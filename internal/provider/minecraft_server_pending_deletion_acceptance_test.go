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

// A disappeared pending ID must survive refresh/plan without a duplicate create,
// even when the test API knows it was actually deleted outside Terraform.
func TestAccMinecraftServerPendingExternalDeletion(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform pending deletion tests")
	}
	for _, phase := range []string{"create", "update"} {
		t.Run(phase, func(t *testing.T) {
			var mu sync.Mutex
			created, deleted, failedRefresh := false, false, false
			posts, patches, deletes, missingReads := 0, 0, 0, 0
			name := "original"
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case "POST":
					posts++
					created = true
					failedRefresh = phase == "create"
					_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
				case "PATCH":
					var payload struct {
						Name string `json:"server_name"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					patches++
					name = payload.Name
					failedRefresh = true
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				case "GET":
					if !created || deleted {
						if deleted {
							missingReads++
						}
						_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
						return
					}
					if failedRefresh {
						w.WriteHeader(403)
						return
					}
					writeServer(w, name)
				case "DELETE":
					deletes++
					if !deleted {
						t.Error("external deletion was not exercised")
					}
					w.WriteHeader(404)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
				}
			}))
			defer api.Close()
			config := func(name string) string {
				return fmt.Sprintf(`
provider "crafty" {
 url = %q
 token = "test-only"
}
resource "crafty_minecraft_server" "test" {
 name = %q
 engine = "paper"
 version = "1.21.1"
 mem_min = 1
 mem_max = 2
 host = "127.0.0.1"
 port = 25565
 timeouts {
  create = "100ms"
  read = "100ms"
  update = "100ms"
  delete = "1s"
 }
}
`, api.URL, name)
			}
			expectedPatches := 0
			if phase == "update" {
				expectedPatches = 1
			}
			check := func(state *terraform.State) error {
				mu.Lock()
				defer mu.Unlock()
				stored := state.RootModule().Resources["crafty_minecraft_server.test"]
				if stored == nil || stored.Primary == nil || stored.Primary.ID != "abc" || stored.Primary.Tainted {
					return fmt.Errorf("pending identity was removed or tainted")
				}
				if posts != 1 || patches != expectedPatches {
					return fmt.Errorf("pending mutation replayed: POST=%d PATCH=%d", posts, patches)
				}
				return nil
			}
			steps := []resource.TestStep{{Config: config("original")}}
			targetName := "original"
			if phase == "update" {
				targetName = "renamed"
				steps = append(steps, resource.TestStep{Config: config(targetName), Check: check})
			} else {
				steps[0].Check = check
			}
			steps = append(steps,
				resource.TestStep{PreConfig: func() { mu.Lock(); deleted = true; mu.Unlock() }, RefreshState: true, Check: check},
				resource.TestStep{Config: config(targetName), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
				resource.TestStep{RefreshState: true, Check: check},
			)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(recoveryTestProvider{Provider: New("test")()})},
				Steps:                    steps,
				CheckDestroy: func(*terraform.State) error {
					mu.Lock()
					defer mu.Unlock()
					if posts != 1 || patches != expectedPatches || deletes != 1 || missingReads < 3 {
						return fmt.Errorf("pending deletion scenario incomplete: POST=%d PATCH=%d DELETE=%d missing reads=%d", posts, patches, deletes, missingReads)
					}
					return nil
				},
			})
		})
	}
}
