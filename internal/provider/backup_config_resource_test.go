package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestBackupConfigFailuresPreserveState(t *testing.T) {
	const valid = `"backup_id":"backup","server_id":"abc","backup_name":"Existing","backup_location":"/backups","max_backups":0,"compress":false,"shutdown":false,"before":"","after":"","excluded_dirs":null`
	for _, test := range []struct {
		name, data string
		status     int
	}{
		{"missing", `{}`, 200},
		{"wrong server", `{"backup":{"backup_id":"backup","server_id":"other"}}`, 200},
		{"wrong policy", `{"backup":{"backup_id":"other","server_id":"abc"}}`, 200},
		{"incomplete", `{"backup":{"backup_id":"backup","server_id":"abc"}}`, 200},
		{"invalid exclusions", `{"backup":{` + strings.Replace(valid, `"excluded_dirs":null`, `"excluded_dirs":[]`, 1) + `}}`, 200},
		{"denied", `"secret-response"`, 400},
		{"server error", `"secret-response"`, 500},
		{"null collection", `null`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/servers/abc/backups" {
					t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.data))
			}))
			defer api.Close()
			ctx := context.Background()
			s := &backupConfigResource{client: client.New(api.URL, "secret-token")}
			var schema resource.SchemaResponse
			s.Schema(ctx, resource.SchemaRequest{}, &schema)
			state := tfsdk.State{Schema: schema.Schema}
			m := backupConfigModel{ID: types.StringValue("backup"), BackupID: types.StringValue("backup"), ServerID: types.StringValue("abc"), ExcludedDirs: types.SetNull(types.StringType)}
			if d := state.Set(ctx, &m); d.HasError() {
				t.Fatal(d)
			}
			read := resource.ReadResponse{State: state}
			s.Read(ctx, resource.ReadRequest{State: state}, &read)
			if !read.Diagnostics.HasError() || !read.State.Raw.Equal(state.Raw) {
				t.Fatal("read failure discarded identity or state")
			}
			for _, d := range read.Diagnostics {
				if strings.Contains(d.Detail(), "secret") {
					t.Fatal("diagnostic disclosed secrets")
				}
			}
			config := tfsdk.Config{Schema: schema.Schema, Raw: state.Raw}
			plan := tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw}
			created := resource.CreateResponse{State: state}
			s.Create(ctx, resource.CreateRequest{Plan: plan, Config: config}, &created)
			updated := resource.UpdateResponse{State: state}
			s.Update(ctx, resource.UpdateRequest{Plan: plan, Config: config}, &updated)
			if !created.Diagnostics.HasError() || !updated.Diagnostics.HasError() {
				t.Fatal("management proceeded without matching complete policy data")
			}
		})
	}
}

func TestBackupConfigOnlyPatchesConfiguredSettings(t *testing.T) {
	const policy = `{"backup":{"backup_id":"backup","server_id":"abc","backup_name":"Existing","backup_location":"/backups","max_backups":5,"compress":true,"shutdown":true,"before":"save-all","after":"say done","excluded_dirs":"logs"}}`
	var patch map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(policy))
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected backup operation: %s", r.Method)
		}
	}))
	defer api.Close()
	ctx := context.Background()
	s := &backupConfigResource{client: client.New(api.URL, "test-only")}
	var schema resource.SchemaResponse
	s.Schema(ctx, resource.SchemaRequest{}, &schema)
	config := backupConfigModel{ServerID: types.StringValue("abc"), BackupID: types.StringValue("backup"), MaxBackups: types.Int64Value(0), Compress: types.BoolValue(false), ExcludedDirs: types.SetValueMust(types.StringType, nil)}
	plan := config
	plan.Name = types.StringUnknown()
	plan.Location = types.StringUnknown()
	plan.Shutdown = types.BoolUnknown()
	plan.Before = types.StringUnknown()
	plan.After = types.StringUnknown()
	c, p := tfsdk.Plan{Schema: schema.Schema}, tfsdk.Plan{Schema: schema.Schema}
	if d := c.Set(ctx, &config); d.HasError() {
		t.Fatal(d)
	}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	response := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	s.Create(ctx, resource.CreateRequest{Plan: p, Config: tfsdk.Config{Schema: schema.Schema, Raw: c.Raw}}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	if len(patch) != 3 || patch["max_backups"] != float64(0) || patch["compress"] != false || len(patch["excluded_dirs"].([]any)) != 0 {
		t.Fatalf("unconfigured fields were changed or false/zero/empty values lost: %v", patch)
	}
	var stored backupConfigModel
	if d := response.State.Get(ctx, &stored); d.HasError() {
		t.Fatal(d)
	}
	if stored.Name.ValueString() != "Existing" || !stored.Shutdown.ValueBool() || stored.Before.ValueString() != "save-all" {
		t.Fatal("unconfigured values were not adopted")
	}
	deleted := resource.DeleteResponse{State: response.State}
	s.Delete(ctx, resource.DeleteRequest{State: response.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}
