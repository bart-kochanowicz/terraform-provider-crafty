package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestManagedMemoryRefreshReadsCommandDrift(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": "original", "auto_start": false, "server_ip": "127.0.0.1", "server_port": 25565, "execution_command": "java -Xms4G -Xmx12G -jar app.jar"}}})
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, "")}
	m := reliabilityModel()
	m.ManageMemory = types.BoolValue(true)
	m.MemMin, m.MemMax = types.Float64Value(4), types.Float64Value(20)
	found, err := s.refresh(context.Background(), &m)
	if err != nil || !found || m.MemMin.ValueFloat64() != 4.096 || m.MemMax.ValueFloat64() != 12.288 || m.ID.ValueString() != "abc" {
		t.Fatalf("refresh did not expose actual heap settings: %+v %v", m, err)
	}
}

func TestManagedMemoryCreateReconcilesGeneratedCommand(t *testing.T) {
	posts, patches := 0, 0
	command := "java -Xms1000M -Xmx2000M -jar paper.jar nogui"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			posts++
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "PATCH":
			patches++
			var body client.UpdateServerRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.ExecutionCommand == nil {
				t.Error("missing memory command")
			} else {
				command = *body.ExecutionCommand
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": "original", "auto_start": false, "server_ip": "127.0.0.1", "server_port": 25565, "execution_command": command}}})
		}
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	m := reliabilityModel()
	m.ManageMemory = types.BoolValue(true)
	m.MemMin, m.MemMax = types.Float64Value(4), types.Float64Value(20)
	response := reliabilityCreate(t, s, m)
	actual := assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 0 || posts != 1 || patches != 1 || actual.MemMin.ValueFloat64() != 4 || actual.MemMax.ValueFloat64() != 20 || command != "java -Xms4000M -Xmx20000M -jar paper.jar nogui" {
		t.Fatalf("memory setup did not converge: %+v %v POST=%d PATCH=%d", actual, response.Diagnostics, posts, patches)
	}
}

func TestManagedMemoryValidatesExplicitCommand(t *testing.T) {
	m := reliabilityModel()
	m.ManageMemory = types.BoolValue(true)
	m.MemMin, m.MemMax = types.Float64Value(4), types.Float64Value(20)
	for _, tc := range []struct {
		command string
		valid   bool
	}{
		{"java -Xms4000M -Xmx20000M -jar app.jar", true},
		{"java -Xms4G -Xmx20G -jar app.jar", false},
		{"java @args.txt", false},
		{"bash start.sh", false},
	} {
		m.ExecutionCommand = types.StringValue(tc.command)
		if err := m.validate(); (err == nil) != tc.valid {
			t.Errorf("command %q: %v", tc.command, err)
		}
	}
}
