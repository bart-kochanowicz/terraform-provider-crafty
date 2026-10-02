package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func contractFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/crafty-4.10.4/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRecordedCraftyContract(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			var got, want map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			if err := json.Unmarshal(contractFixture(t, "create-request"), &want); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Error("typed create request differs from the recorded live request")
			}
			w.WriteHeader(201)
			_, _ = w.Write(contractFixture(t, "create-response"))
		case r.Method == "PATCH":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(got, map[string]any{"server_name": "renamed"}) {
				t.Errorf("provider PATCH must only change name: %v", got)
			}
			_, _ = w.Write(contractFixture(t, "patch-response"))
		case r.URL.Path == "/api/v2/servers":
			_, _ = w.Write(contractFixture(t, "list-response"))
		default:
			_, _ = w.Write(contractFixture(t, "server-response"))
		}
	}))
	defer server.Close()
	api := New(server.URL, "test-only")
	var create CreateJavaServerRequest
	if err := json.Unmarshal(contractFixture(t, "create-request"), &create); err != nil {
		t.Fatal(err)
	}
	created, err := api.CreateJavaServer(ctx, create)
	if err != nil || created.ID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("create fixture incompatible: %+v %v", created, err)
	}
	servers, err := api.ListServers(ctx)
	if err != nil || len(servers) != 1 || servers[0].ID != created.ID || servers[0].Name == nil || *servers[0].Name != "contract-test" || servers[0].AutoStart == nil || *servers[0].AutoStart {
		t.Fatalf("collection fixture incompatible: %+v %v", servers, err)
	}
	if servers[0].MonitoringHost == nil || *servers[0].MonitoringHost != "127.0.0.1" || servers[0].MonitoringPort == nil || *servers[0].MonitoringPort != 25576 || servers[0].ExecutionCommand == nil || *servers[0].ExecutionCommand != "java -Xms1000M -Xmx2000M -jar paper.jar nogui" {
		t.Fatalf("managed settings incompatible with recorded GET: %+v", servers[0])
	}
	var single Server
	if err := api.request(ctx, "GET", "/api/v2/servers/"+created.ID, nil, &single); err != nil || !reflect.DeepEqual(single, servers[0]) {
		t.Fatalf("single GET differs from collection model: %+v %v", single, err)
	}
	if err := api.UpdateServer(ctx, created.ID, UpdateServerRequest{Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedMissingServerIsNotAuthoritativeNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write(contractFixture(t, "missing-server-response"))
	}))
	defer server.Close()
	err := New(server.URL, "test-only").request(context.Background(), "GET", "/api/v2/servers/missing", nil, nil)
	if err == nil || IsNotFound(err) {
		t.Fatalf("ambiguous NOT_AUTHORIZED must not be treated as deletion: %v", err)
	}
}
