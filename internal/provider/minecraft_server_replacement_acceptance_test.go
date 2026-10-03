package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Plan-only changes verify every replacement input without additional downloads.
func TestAccMinecraftServerReplacementPlans(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform replacement plan tests")
	}
	var mu sync.Mutex
	created, posts, deletes := false, 0, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "POST":
			posts++
			created = true
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "GET":
			if created {
				writeServer(w, "replacement plans")
			} else {
				_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
			}
		case "DELETE":
			deletes++
			created = false
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("plan-only scenario unexpectedly sent %s", r.Method)
			w.WriteHeader(405)
		}
	}))
	defer api.Close()
	defaults := map[string]string{"engine": `"paper"`, "version": `"1.21.1"`, "mem_min": "1", "mem_max": "2", "host": `"127.0.0.1"`, "port": "25565"}
	config := func(field, value string) string {
		values := make(map[string]string)
		for key, value := range defaults {
			values[key] = value
		}
		if field != "" {
			values[field] = value
		}
		return fmt.Sprintf(`
provider "crafty" {
 url = %q
 token = "test-only"
}
resource "crafty_minecraft_server" "test" {
 name = "replacement plans"
 engine = %s
 version = %s
 mem_min = %s
 mem_max = %s
 host = %s
 port = %s
}
`, api.URL, values["engine"], values["version"], values["mem_min"], values["mem_max"], values["host"], values["port"])
	}
	steps := []resource.TestStep{{Config: config("", ""), Check: resource.TestCheckResourceAttr("crafty_minecraft_server.test", "id", "abc")}}
	for _, change := range []struct{ field, value string }{{"engine", `"vanilla"`}, {"version", `"1.21.2"`}, {"mem_min", "2"}, {"mem_max", "3"}, {"host", `"127.0.0.2"`}, {"port", "25566"}} {
		steps = append(steps, resource.TestStep{Config: config(change.field, change.value), PlanOnly: true, ExpectNonEmptyPlan: true,
			ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionDestroyBeforeCreate)}}})
	}
	steps = append(steps, resource.TestStep{Config: config("", ""), PlanOnly: true,
		ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		Steps:                    steps,
		CheckDestroy: func(*terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if created || posts != 1 || deletes != 1 {
				return fmt.Errorf("plan-only checks mutated resource: POST=%d DELETE=%d", posts, deletes)
			}
			return nil
		},
	})
}

// Filesystem assertions are opt-in only for the fresh CI project configured by ci.py.
func replacementServerPath(ctx context.Context, endpoint, token, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/v2/servers/"+id, nil)
	if err != nil {
		return "", fmt.Errorf("could not prepare replacement evidence request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not read replacement evidence")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("replacement evidence returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if json.NewDecoder(response.Body).Decode(&result) != nil || result.Status != "ok" || result.Data.Path == "" {
		return "", fmt.Errorf("replacement evidence lacks server directory")
	}
	return result.Data.Path, nil
}
