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

func TestWebhookReadAndMutationScope(t *testing.T) {
	for _, test := range []struct {
		name, data         string
		status             int
		wantError, removed bool
	}{
		{"missing", `{}`, 200, false, true},
		{"different webhook", `{"2":{}}`, 200, false, true},
		{"incomplete", `{"1":{"name":"test"}}`, 200, true, false},
		{"denied", `"secret-response"`, 400, true, false},
		{"missing endpoint", `{}`, 404, true, false},
		{"null data", `null`, 200, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/servers/abc/webhook" {
					t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"status":"ok","data":` + test.data + `}`))
			}))
			defer api.Close()
			ctx := context.Background()
			s := &webhookResource{client: client.New(api.URL, "secret-token")}
			var schema resource.SchemaResponse
			s.Schema(ctx, resource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			if d := state.Set(ctx, &webhookModel{ID: types.StringValue("1"), ServerID: types.StringValue("abc"), Triggers: types.SetNull(types.StringType)}); d.HasError() {
				t.Fatal(d)
			}
			read := resource.ReadResponse{State: state}
			s.Read(ctx, resource.ReadRequest{State: state}, &read)
			if read.Diagnostics.HasError() != test.wantError || read.State.Raw.IsNull() != test.removed {
				t.Fatalf("unexpected read result: %v", read.Diagnostics)
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail(), "secret") {
					t.Fatal("diagnostic disclosed secrets")
				}
			}
			if test.name == "incomplete" {
				return
			}
			update := resource.UpdateResponse{State: state}
			s.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw}}, &update)
			if !update.Diagnostics.HasError() {
				t.Fatal("update accepted an ID outside the server collection")
			}
			deleted := resource.DeleteResponse{State: state}
			s.Delete(ctx, resource.DeleteRequest{State: state}, &deleted)
			if deleted.Diagnostics.HasError() != test.wantError {
				t.Fatal(deleted.Diagnostics)
			}
		})
	}
}
