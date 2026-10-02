package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestUpdateSettingsWaitsForFullConvergence(t *testing.T) {
	gets := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 4 || body["auto_start"] != false || body["server_ip"] != "127.0.0.2" || body["server_port"] != float64(25566) || body["execution_command"] != "custom launch command" {
				t.Errorf("unexpected PATCH: %v", body)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		gets++
		port := 25565
		if gets > 1 {
			port = 25566
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": "original", "auto_start": false, "server_ip": "127.0.0.2", "server_port": port, "execution_command": "custom launch command"}}})
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	prior := reliabilityModel()
	prior.AutoStart = types.BoolValue(true)
	state := reliabilityState(t, s, prior)
	desired := prior
	desired.AutoStart = types.BoolValue(false)
	desired.MonitoringHost = types.StringValue("127.0.0.2")
	desired.MonitoringPort = types.Int64Value(25566)
	desired.ExecutionCommand = types.StringValue("custom launch command")
	desired.Timeouts = testTimeouts(map[string]string{"update": "1s"})
	response := resource.UpdateResponse{State: state}
	s.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(reliabilityState(t, s, desired))}, &response)
	actual := assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 0 || gets != 2 || !actual.MonitoringPort.Equal(desired.MonitoringPort) {
		t.Fatalf("incomplete convergence: %+v %v GET=%d", actual, response.Diagnostics, gets)
	}
}

func TestCreateSettingsFailureKeepsIDWithoutMutationReplay(t *testing.T) {
	posts, patches := 0, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			posts++
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "PATCH":
			patches++
			w.WriteHeader(503)
		default:
			writeServer(w, "original")
		}
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	desired := reliabilityModel()
	desired.AutoStart = types.BoolValue(true)
	desired.MonitoringHost = types.StringValue("127.0.0.2")
	state := reliabilityState(t, s, desired)
	response := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	s.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &response)
	actual := assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 1 || posts != 1 || patches != 1 || !actual.AutoStart.ValueBool() || actual.ExecutionCommand.IsUnknown() {
		t.Fatalf("failed initial configuration lost identity or replayed: %+v %v POST=%d PATCH=%d", actual, response.Diagnostics, posts, patches)
	}
}

func TestUpdateWithOnlyTimeoutChangesDoesNotPatch(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.Method)
		w.WriteHeader(500)
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, "")}
	model := reliabilityModel()
	state := reliabilityState(t, s, model)
	model.Timeouts = testTimeouts(map[string]string{"update": "1m"})
	response := resource.UpdateResponse{State: state}
	s.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(reliabilityState(t, s, model))}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	assertServerID(t, response.State)
}
