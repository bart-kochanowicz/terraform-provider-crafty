package provider

import (
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

// Terraform verifies that a rejected settings PATCH after POST keeps an untainted
// ID, exposes actual drift on refresh, and retries only configuration on apply.
func TestAccMinecraftServerInitialSettingsRecovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform recovery acceptance tests")
	}
	var mu sync.Mutex
	created, deleted, reject, autoStart := false, false, true, false
	posts, patches := 0, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "POST":
			posts++
			created = true
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "PATCH":
			patches++
			if reject {
				w.WriteHeader(503)
				return
			}
			var update struct {
				AutoStart bool `json:"auto_start"`
			}
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Error(err)
			}
			autoStart = update.AutoStart
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "DELETE":
			deleted = true
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "GET":
			if !created || deleted {
				_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": "original", "auto_start": autoStart, "server_ip": "127.0.0.1", "server_port": 25565, "execution_command": "java -Xms1000M -Xmx2000M -jar paper.jar nogui"}}})
		}
	}))
	defer api.Close()
	config := fmt.Sprintf(`provider "crafty" {
 url = %q
 token = "test-only"
}
resource "crafty_minecraft_server" "test" {
 name = "original"
 engine = "paper"
 version = "1.21.1"
 mem_min = 1
 mem_max = 2
 host = "127.0.0.1"
 port = 25565
 auto_start = true
}`, api.URL)
	check := func(expectedPatches int) resource.TestCheckFunc {
		return func(state *terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			saved := state.RootModule().Resources["crafty_minecraft_server.test"]
			if saved == nil || saved.Primary == nil || saved.Primary.ID != "abc" || saved.Primary.Tainted || posts != 1 || patches != expectedPatches {
				return fmt.Errorf("identity lost, tainted, or replayed: POST=%d PATCH=%d", posts, patches)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(recoveryTestProvider{Provider: New("test")()})},
		CheckDestroy: func(*terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if !deleted || posts != 1 {
				return fmt.Errorf("cleanup failed or server duplicated")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config, Check: check(1), ExpectNonEmptyPlan: true},
			{PreConfig: func() { mu.Lock(); reject = false; mu.Unlock() }, Config: config, Check: resource.ComposeTestCheckFunc(check(2), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "auto_start", "true")), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionUpdate)}}},
			{Config: config, PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}
