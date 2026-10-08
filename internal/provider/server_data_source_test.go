package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestServerDataSourceRead(t *testing.T) {
	const server = `{"server_id":"abc","server_name":"existing","auto_start":false,"server_ip":"","server_port":0,"execution_command":""}`
	for _, test := range []struct {
		name, data, errorSummary string
		id                       types.String
		status                   int
	}{
		{"found", `[{"server_id":"other"},` + server + `]`, "", types.StringValue("abc"), 200},
		{"empty collection", `[]`, "Server unavailable", types.StringValue("abc"), 200},
		{"filtered collection", `[{"server_id":"other"}]`, "Server unavailable", types.StringValue("abc"), 200},
		{"duplicate ID", `[` + server + `,` + server + `]`, "Ambiguous server response", types.StringValue("abc"), 200},
		{"incomplete response", `[{"server_id":"abc","server_name":"existing"}]`, "Incomplete server response", types.StringValue("abc"), 200},
		{"denied", `"secret-body"`, "Unable to read Crafty server", types.StringValue("abc"), 403},
		{"empty ID", `[]`, "Invalid server ID", types.StringValue(""), 200},
		{"blank ID", `[]`, "Invalid server ID", types.StringValue(" \t"), 200},
		{"unknown ID", `[]`, "Invalid server ID", types.StringUnknown(), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/servers" || r.Header.Get("Authorization") != "Bearer secret-token" {
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"status":"ok","data":` + test.data + `}`))
			}))
			defer api.Close()
			ctx := context.Background()
			s := &serverDataSource{client: client.New(api.URL, "secret-token")}
			var schema datasource.SchemaResponse
			s.Schema(ctx, datasource.SchemaRequest{}, &schema)
			plan := tfsdk.Plan{Schema: schema.Schema}
			if d := plan.Set(ctx, &serverDataSourceModel{ID: test.id}); d.HasError() {
				t.Fatal(d)
			}
			response := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
			s.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw}}, &response)
			if test.errorSummary != "" {
				if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != test.errorSummary {
					t.Fatalf("unexpected diagnostics: %v", response.Diagnostics)
				}
				for _, diagnostic := range response.Diagnostics {
					if strings.Contains(diagnostic.Detail(), "secret") {
						t.Fatal("diagnostic disclosed credentials or response body")
					}
				}
			} else {
				if response.Diagnostics.HasError() {
					t.Fatal(response.Diagnostics)
				}
				var actual serverDataSourceModel
				if d := response.State.Get(ctx, &actual); d.HasError() {
					t.Fatal(d)
				}
				if actual.ID.ValueString() != "abc" || actual.Name.ValueString() != "existing" || actual.AutoStart.IsNull() || actual.AutoStart.ValueBool() || actual.MonitoringPort.IsNull() || actual.MonitoringPort.ValueInt64() != 0 || actual.MonitoringHost.IsNull() || actual.ExecutionCommand.IsNull() {
					t.Fatalf("server values or false/zero values were lost: %+v", actual)
				}
			}
			wantCalls := int32(1)
			if test.errorSummary == "Invalid server ID" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("expected %d API calls, got %d", wantCalls, calls.Load())
			}
		})
	}
}
