package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestScheduleReadErrorsPreserveIdentity(t *testing.T) {
	const valid = `"schedule_id":1,"server_id":{"server_id":"abc"},"name":"test","enabled":false,"action":"command","command":"say test","interval":24,"interval_type":"hours","start_time":"00:00","cron_string":"","delay":0`
	for _, test := range []struct {
		name, data string
		status     int
	}{
		{"missing or transient failure", `"secret-response"`, 500},
		{"denied", `"secret-response"`, 400},
		{"different server", `{"schedule_id":1,"server_id":{"server_id":"other"}}`, 200},
		{"different task", `{"schedule_id":2,"server_id":{"server_id":"abc"}}`, 200},
		{"incomplete", `{"schedule_id":1,"server_id":{"server_id":"abc"}}`, 200},
		{"one time", `{` + valid + `,"one_time":true,"parent":null}`, 200},
		{"chain", `{` + valid + `,"one_time":false,"parent":2}`, 200},
		{"backup reference", `{` + valid + `,"one_time":false,"parent":null,"action_id":"backup-id"}`, 200},
		{"null", `null`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/servers/abc/tasks/1" {
					t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.data))
			}))
			defer api.Close()
			ctx := context.Background()
			s := &scheduleResource{client: client.New(api.URL, "secret-token")}
			var schema resource.SchemaResponse
			s.Schema(ctx, resource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &scheduleModel{ID: types.StringValue("1"), ServerID: types.StringValue("abc")}); d.HasError() {
				t.Fatal(d)
			}
			read := resource.ReadResponse{State: state}
			s.Read(ctx, resource.ReadRequest{State: state}, &read)
			if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
				t.Fatal("read error discarded identity or state")
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail(), "secret") {
					t.Fatal("diagnostics disclosed secrets")
				}
			}
			update := resource.UpdateResponse{State: state}
			s.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw}}, &update)
			deleted := resource.DeleteResponse{State: state}
			s.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
			if !update.Diagnostics.HasError() || !deleted.Diagnostics.HasError() {
				t.Fatal("mutation proceeded without a matching supported task")
			}
		})
	}
}

func TestScheduleCreationRequiresIdentity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled rejected before POST", true: "missing ID not replayed"}[enabled], func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				_, _ = w.Write([]byte(`{"status":"ok","data":{"schedule_id":null}}`))
			}))
			defer api.Close()
			ctx := context.Background()
			s := &scheduleResource{client: client.New(api.URL, "test-only")}
			var schema resource.SchemaResponse
			s.Schema(ctx, resource.SchemaRequest{}, &schema)
			plan := tfsdk.Plan{Schema: schema.Schema}
			if d := plan.Set(ctx, &scheduleModel{ServerID: types.StringValue("abc"), Enabled: types.BoolValue(enabled)}); d.HasError() {
				t.Fatal(d)
			}
			response := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
			s.Create(ctx, resource.CreateRequest{Plan: plan}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("creation succeeded without an ID")
			}
			want := 0
			if enabled {
				want = 1
			}
			if calls != want {
				t.Fatalf("expected %d API calls, got %d", want, calls)
			}
		})
	}
}
