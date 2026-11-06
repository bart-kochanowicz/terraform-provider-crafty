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

func TestAccMinecraftServerManagedMemory(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform memory update tests")
	}
	var mu sync.Mutex
	created, posts, patches, deletes := false, 0, 0, 0
	const original = "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xmx12G @libraries/unix_args.txt nogui"
	command := original
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "POST":
			posts++
			created = true
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case "GET":
			servers := []any{}
			if created {
				servers = []any{map[string]any{"server_id": "abc", "server_name": "managed memory", "auto_start": false, "server_ip": "127.0.0.1", "server_port": 25565, "execution_command": command}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": servers})
		case "PATCH":
			patches++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.URL.Path != "/api/v2/servers/abc" || len(body) != 1 {
				t.Errorf("memory update changed unrelated settings: %v", body)
			}
			value, ok := body["execution_command"].(string)
			if !ok {
				t.Errorf("memory PATCH did not use execution_command: %v", body)
			}
			command = value
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "DELETE":
			deletes++
			created = false
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer api.Close()
	config := func(managed bool, max float64, protect bool) string {
		return fmt.Sprintf(`
provider "crafty" {
 url = %q
 token = "test-only"
}
resource "crafty_minecraft_server" "test" {
 name = "managed memory"
 engine = "paper"
 version = "1.21.1"
 mem_min = 4
 mem_max = %g
 manage_memory = %t
 host = "127.0.0.1"
 port = 25565
 lifecycle { prevent_destroy = %t }
}
`, api.URL, max, managed, protect)
	}
	check := func(max string, wantPatches int) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "id", "abc"),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "mem_min", "4"),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "mem_max", max),
			func(*terraform.State) error {
				mu.Lock()
				defer mu.Unlock()
				if posts != 1 || deletes != 0 || patches != wantPatches {
					return fmt.Errorf("unexpected mutation counts POST=%d PATCH=%d DELETE=%d", posts, patches, deletes)
				}
				return nil
			})
	}
	update := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config(false, 12, true), Check: check("12", 0)},
			{Config: config(true, 20, true), ConfigPlanChecks: update, Check: resource.ComposeTestCheckFunc(check("20", 1), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "execution_command", "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xms4000M -Xmx20000M @libraries/unix_args.txt nogui"))},
			{PreConfig: func() {
				mu.Lock()
				command = "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xms4G -Xmx12G @libraries/unix_args.txt nogui"
				mu.Unlock()
			}, Config: config(true, 20, true), ConfigPlanChecks: update, Check: check("20", 2)},
			{Config: config(true, 20.48, true), ConfigPlanChecks: update, Check: check("20.48", 3)},
			{PreConfig: func() {
				mu.Lock()
				command = "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xmx20480M @libraries/unix_args.txt nogui"
				mu.Unlock()
			}, Config: config(true, 20.48, true), ConfigPlanChecks: update, Check: check("20.48", 4)},
			// Remove only the test's deletion safeguard for fixture cleanup.
			{Config: config(true, 20.48, false), Check: check("20.48", 4), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
		CheckDestroy: func(*terraform.State) error {
			mu.Lock()
			defer mu.Unlock()
			if created || posts != 1 || deletes != 1 || patches != 4 {
				return fmt.Errorf("unexpected lifecycle POST=%d PATCH=%d DELETE=%d", posts, patches, deletes)
			}
			return nil
		},
	})
}
