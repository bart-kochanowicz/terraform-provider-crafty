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
	"time"

	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

type recoveryTestProvider struct{ frameworkprovider.Provider }

func (p recoveryTestProvider) Resources(context.Context) []func() frameworkresource.Resource {
	return []func() frameworkresource.Resource{func() frameworkresource.Resource { return &serverResource{pollInterval: time.Millisecond} }}
}

// Real Terraform verifies that warnings and private state survive RPC boundaries:
// a server hidden after POST must not be tainted, removed, or created a second time.
func TestAccMinecraftServerPostCreateRecovery(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform recovery acceptance tests")
	}
	for _, postCreateFault := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(postCreateFault), func(t *testing.T) {
			var mu sync.Mutex
			created, deleted, fault, posts, patches := false, false, 0, 0, 0
			name := "original"
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case "POST":
					posts++
					created = true
					fault = postCreateFault
					_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
				case "PATCH":
					var update struct {
						Name string `json:"server_name"`
					}
					if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					patches++
					name = update.Name
					fault = 503
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				case "DELETE":
					deleted = true
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				case "GET":
					if !created || deleted || fault == -1 {
						_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
						return
					}
					if fault != 0 {
						w.WriteHeader(fault)
						return
					}
					writeServer(w, name)
				default:
					t.Errorf("unexpected method %s", r.Method)
					w.WriteHeader(405)
				}
			}))
			defer api.Close()
			config := func(serverName string) string {
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
`, api.URL, serverName)
			}
			setFault := func(value int) func() { return func() { mu.Lock(); fault = value; mu.Unlock() } }
			check := func(expectedPatches int) resource.TestCheckFunc {
				return func(state *terraform.State) error {
					mu.Lock()
					defer mu.Unlock()
					stored := state.RootModule().Resources["crafty_minecraft_server.test"]
					if stored == nil || stored.Primary == nil || stored.Primary.ID != "abc" || stored.Primary.Tainted {
						return fmt.Errorf("existing server was lost or tainted")
					}
					if posts != 1 || patches != expectedPatches {
						return fmt.Errorf("unexpected mutations: POST=%d PATCH=%d", posts, patches)
					}
					return nil
				}
			}
			empty := resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(recoveryTestProvider{Provider: New("test")()})},
				CheckDestroy: func(*terraform.State) error {
					mu.Lock()
					defer mu.Unlock()
					if !deleted || posts != 1 {
						return fmt.Errorf("server was not destroyed or was duplicated")
					}
					return nil
				},
				Steps: []resource.TestStep{
					{Config: config("original"), Check: check(0)},
					{PreConfig: setFault(-1), Config: config("original"), PlanOnly: true, ConfigPlanChecks: empty},
					{PreConfig: setFault(0), Config: config("original"), Check: check(0)},
					{Config: config("renamed"), Check: check(1)},
					{PreConfig: setFault(0), Config: config("renamed"), PlanOnly: true, ConfigPlanChecks: empty, Check: check(1)},
				},
			})
		})
	}
}
