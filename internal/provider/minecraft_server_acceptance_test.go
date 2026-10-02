package provider

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestAccMinecraftServerLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run live Crafty acceptance tests")
	}
	token := os.Getenv("CRAFTY_TOKEN")
	if token == "" {
		t.Fatal("CRAFTY_TOKEN must contain a disposable Crafty instance API token")
	}
	endpoint := os.Getenv("CRAFTY_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:18000"
	}
	engine, version := os.Getenv("CRAFTY_TEST_ENGINE"), os.Getenv("CRAFTY_TEST_VERSION")
	if engine == "" {
		engine = "paper"
	}
	if version == "" {
		version = "1.21.1"
	}
	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("tf-acc-%x", suffix)
	renamed := name + "-renamed"
	t.Setenv("TF_VAR_crafty_acceptance_token", token)
	api := client.New(endpoint, token)
	var id string

	// Registered before resource.Test so fallback cleanup also runs after a failed
	// apply, including a create response lost before its ID reached Terraform state.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		servers, err := api.ListServers(ctx)
		if err != nil {
			t.Errorf("Fallback cleanup could not list servers: %v", err)
			return
		}
		for _, server := range servers {
			if server.ID != id && (server.Name == nil || (*server.Name != name && *server.Name != renamed)) {
				continue
			}
			if err := api.DeleteServer(ctx, server.ID); err != nil && !client.IsNotFound(err) {
				t.Errorf("Fallback cleanup failed for %s: %v", server.ID, err)
				continue
			}
			if err := waitForServerDeletion(ctx, api, server.ID); err != nil {
				t.Error(err)
			}
		}
	})
	config := func(serverName string) string {
		return fmt.Sprintf(`
variable "crafty_acceptance_token" {
 type = string
 sensitive = true
}
provider "crafty" {
 url = %q
 token = var.crafty_acceptance_token
}
resource "crafty_minecraft_server" "test" {
 name = %q
 engine = %q
 version = %q
 mem_min = 1
 mem_max = 2
 host = "127.0.0.1"
 port = 25565
}
`, endpoint, serverName, engine, version)
	}
	check := func(expectedName string) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "name", expectedName),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "engine", engine),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "version", version),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "mem_min", "1"),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "mem_max", "2"),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "host", "127.0.0.1"),
			resource.TestCheckResourceAttr("crafty_minecraft_server.test", "port", "25565"),
			resource.TestCheckResourceAttrSet("crafty_minecraft_server.test", "auto_start"),
			func(state *terraform.State) error {
				stored, ok := state.RootModule().Resources["crafty_minecraft_server.test"]
				if !ok || stored.Primary == nil || stored.Primary.ID == "" {
					return fmt.Errorf("server ID missing from Terraform state")
				}
				if id != "" && id != stored.Primary.ID {
					return fmt.Errorf("rename or refresh replaced the server: %s -> %s", id, stored.Primary.ID)
				}
				id = stored.Primary.ID
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				servers, err := api.ListServers(ctx)
				if err != nil {
					return err
				}
				for _, server := range servers {
					if server.ID == id && server.Name != nil && *server.Name == expectedName {
						return nil
					}
				}
				return fmt.Errorf("server %s with name %s not found in Crafty", id, expectedName)
			},
		)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		PreCheck: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := api.ListServers(ctx); err != nil {
				t.Fatalf("Crafty precheck failed: %v", err)
			}
		},
		CheckDestroy: func(_ *terraform.State) error {
			if id == "" {
				return fmt.Errorf("no server ID captured; destroy cannot be verified")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			return waitForServerDeletion(ctx, api, id)
		},
		Steps: []resource.TestStep{
			{Config: config(name), Check: check(name)},
			{RefreshState: true, Check: check(name)},
			{Config: config(renamed), Check: check(renamed), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionUpdate)}}},
			{Config: config(renamed), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	}) // resource.Test always runs Terraform destroy and CheckDestroy at the end.
}

func waitForServerDeletion(ctx context.Context, api *client.Client, id string) error {
	for {
		servers, err := api.ListServers(ctx)
		if err != nil {
			return fmt.Errorf("verify deletion of %s: %w", id, err)
		}
		found := false
		for _, server := range servers {
			if server.ID == id {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("server %s still exists: %w", id, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}
