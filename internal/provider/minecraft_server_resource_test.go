package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRefreshPreservesDownloadInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v2/servers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": "changed", "auto_start": true}}})
	}))
	defer server.Close()
	s := &serverResource{client: client.New(server.URL, "")}
	m := serverModel{ID: types.StringValue("abc"), Engine: types.StringValue("paper"), MemMax: types.Int64Value(2)}
	found, err := s.refresh(context.Background(), &m)
	if err != nil || !found || m.Name.ValueString() != "changed" || !m.AutoStart.ValueBool() || m.Engine.ValueString() != "paper" || m.MemMax.ValueInt64() != 2 {
		t.Fatalf("unexpected refresh: %+v, %v", m, err)
	}
	m.ID = types.StringValue("missing")
	found, err = s.refresh(context.Background(), &m)
	if err != nil || found {
		t.Fatalf("expected missing resource: %v, %v", found, err)
	}
}
func TestResourceCRUD(t *testing.T) {
	ctx := context.Background()
	name := "original"
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.Method]++
		switch r.Method {
		case "POST":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			java, ok := body["minecraft_java_create_data"].(map[string]any)
			if !ok {
				t.Error("missing Java create data")
				return
			}
			download := java["download_jar_create_data"].(map[string]any)
			if download["mem_max"] != float64(2) || download["type"] != "paper" || body["name"] != name {
				t.Errorf("unexpected POST payload: %v", body)
			}
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": name, "auto_start": false}}})
		case "PATCH":
			if r.URL.Path != "/api/v2/servers/abc" {
				t.Error("unexpected PATCH path")
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body) != 1 || body["server_name"] != "renamed" {
				t.Errorf("unexpected PATCH payload: %v", body)
			}
			name = body["server_name"]
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "DELETE":
			if r.URL.Path != "/api/v2/servers/abc" {
				t.Error("unexpected DELETE path")
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	s := &serverResource{client: client.New(server.URL, "secret")}
	var schemaResponse resource.SchemaResponse
	s.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	plan := tfsdk.Plan{Schema: schemaResponse.Schema}
	m := serverModel{ID: types.StringUnknown(), Name: types.StringValue(name), Engine: types.StringValue("paper"), Version: types.StringValue("1.21.1"), MemMin: types.Int64Value(1), MemMax: types.Int64Value(2), Host: types.StringValue("127.0.0.1"), Port: types.Int64Value(25565), AutoStart: types.BoolUnknown()}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	created := resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	s.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	if d := created.State.Get(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	if m.ID.ValueString() != "abc" || m.AutoStart.ValueBool() {
		t.Fatalf("unexpected created state: %+v", m)
	}
	read := resource.ReadResponse{State: created.State}
	s.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	m.Name = types.StringValue("renamed")
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatal(d)
	}
	updated := resource.UpdateResponse{State: created.State}
	s.Update(ctx, resource.UpdateRequest{Plan: plan, State: created.State}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: updated.State}
	s.Delete(ctx, resource.DeleteRequest{State: updated.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	if calls["POST"] != 1 || calls["PATCH"] != 1 || calls["DELETE"] != 1 || calls["GET"] != 3 {
		t.Fatalf("unexpected CRUD calls: %v", calls)
	}
}
