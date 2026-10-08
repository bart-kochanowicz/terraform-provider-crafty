package provider

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
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
	var id, previousID, previousPath string
	project := os.Getenv("CRAFTY_TEST_COMPOSE_PROJECT")
	if project != "" && (!regexp.MustCompile(`^crafty-provider-ci-[a-z0-9-]+$`).MatchString(project) || endpoint != "http://127.0.0.1:18001") {
		t.Fatal("filesystem checks require the isolated CI project and endpoint")
	}

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
			if server.ID != id && server.ID != previousID && (server.Name == nil || (*server.Name != name && *server.Name != renamed)) {
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
	config := func(serverName string, settings ...string) string {
		extra := ""
		if len(settings) > 0 {
			extra = settings[0]
		}
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
 %s
}
data "crafty_server" "lookup" {
 id = crafty_minecraft_server.test.id
}
`, endpoint, serverName, engine, version, extra)
	}
	check := func(expectedName string) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttrPair("data.crafty_server.lookup", "id", "crafty_minecraft_server.test", "id"),
			resource.TestCheckResourceAttr("data.crafty_server.lookup", "name", expectedName),
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
						attrs := stored.Primary.Attributes
						if server.AutoStart == nil || fmt.Sprint(*server.AutoStart) != attrs["auto_start"] || server.MonitoringHost == nil || *server.MonitoringHost != attrs["monitoring_host"] || server.MonitoringPort == nil || fmt.Sprint(*server.MonitoringPort) != attrs["monitoring_port"] || server.ExecutionCommand == nil || *server.ExecutionCommand != attrs["execution_command"] {
							return fmt.Errorf("managed settings differ between Terraform and Crafty")
						}
						return nil
					}
				}
				return fmt.Errorf("server %s with name %s not found in Crafty", id, expectedName)
			},
		)
	}
	initial := `auto_start = true
 monitoring_host = "127.0.0.2"
 monitoring_port = 25566
 execution_command = "java -Xms1500M -Xmx2500M -jar paper.jar nogui"`
	updatedSettings := `auto_start = false
 monitoring_host = "127.0.0.3"
 monitoring_port = 25567
 execution_command = "java -Xms2000M -Xmx3000M -jar paper.jar nogui"`
	updatePlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionUpdate)}}
	emptyPlan := resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	settingsCheck := resource.ComposeTestCheckFunc(check(renamed),
		resource.TestCheckResourceAttr("crafty_minecraft_server.test", "auto_start", "false"),
		resource.TestCheckResourceAttr("crafty_minecraft_server.test", "monitoring_host", "127.0.0.3"),
		resource.TestCheckResourceAttr("crafty_minecraft_server.test", "monitoring_port", "25567"),
		resource.TestCheckResourceAttr("crafty_minecraft_server.test", "execution_command", "java -Xms2000M -Xmx3000M -jar paper.jar nogui"))

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
			if previousID != "" {
				if err := waitForServerDeletion(ctx, api, previousID); err != nil {
					return err
				}
			}
			return waitForServerDeletion(ctx, api, id)
		},
		Steps: []resource.TestStep{
			{Config: config(name, initial), Check: resource.ComposeTestCheckFunc(check(name), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "auto_start", "true"), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "monitoring_port", "25566"))},
			{RefreshState: true, Check: check(name)},
			{Config: config(renamed, initial), Check: check(renamed), ConfigPlanChecks: updatePlan},
			{Config: config(renamed, updatedSettings), Check: settingsCheck, ConfigPlanChecks: updatePlan},
			{RefreshState: true, Check: settingsCheck},
			{Config: config(renamed, updatedSettings), PlanOnly: true, ConfigPlanChecks: emptyPlan},
			{PreConfig: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				drift := true
				port := int64(25568)
				if err := api.UpdateServer(ctx, id, client.UpdateServerRequest{AutoStart: &drift, MonitoringPort: &port}); err != nil {
					t.Fatal(err)
				}
			}, Config: config(renamed, updatedSettings), Check: settingsCheck, ConfigPlanChecks: updatePlan},
			{Config: config(renamed), Check: settingsCheck},
			{Config: config(renamed), PlanOnly: true, ConfigPlanChecks: emptyPlan},
			{PreConfig: func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				unmanaged := true
				port := int64(25568)
				if err := api.UpdateServer(ctx, id, client.UpdateServerRequest{AutoStart: &unmanaged, MonitoringPort: &port}); err != nil {
					t.Fatal(err)
				}
			}, Config: config(renamed), PlanOnly: true, ConfigPlanChecks: emptyPlan},
			{Config: config(renamed), Check: resource.ComposeTestCheckFunc(check(renamed), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "auto_start", "true"), resource.TestCheckResourceAttr("crafty_minecraft_server.test", "monitoring_port", "25568"))},
			{ResourceName: "crafty_minecraft_server.test", ImportState: true, ExpectError: regexp.MustCompile("Import unavailable for the verified API")},
			{Config: config(renamed), PlanOnly: true, ConfigPlanChecks: emptyPlan},

			{PreConfig: func() {
				previousID = id
				if project != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					var err error
					previousPath, err = replacementServerPath(ctx, endpoint, token, previousID)
					if err != nil {
						t.Fatal(err)
					}
				}
			}, Config: strings.Replace(config(renamed), "mem_max = 2", "mem_max = 3", 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionDestroyBeforeCreate)}},
				Check: func(state *terraform.State) error {
					stored := state.RootModule().Resources["crafty_minecraft_server.test"]
					if stored == nil || stored.Primary == nil || stored.Primary.ID == "" || stored.Primary.ID == previousID {
						return fmt.Errorf("replacement did not produce a new ID")
					}
					id = stored.Primary.ID
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					servers, err := api.ListServers(ctx)
					if err != nil {
						return err
					}
					matching := 0
					newIDFound := false
					for _, server := range servers {
						if server.ID == previousID {
							return fmt.Errorf("old server still exists after replacement")
						}
						if server.Name != nil && *server.Name == renamed {
							matching++
						}
						if server.ID == id {
							newIDFound = true
						}
						if server.ID == id && (server.ExecutionCommand == nil || !strings.Contains(*server.ExecutionCommand, "-Xmx3000M")) {
							return fmt.Errorf("replacement did not use updated memory input")
						}
					}
					if !newIDFound {
						return fmt.Errorf("new ID is absent from Crafty after replacement")
					}
					if matching != 1 {
						return fmt.Errorf("replacement left %d matching servers", matching)
					}
					if project != "" {
						newPath, err := replacementServerPath(ctx, endpoint, token, id)
						if err != nil {
							return err
						}
						if newPath == previousPath {
							return fmt.Errorf("replacement reused old directory")
						}
						// Pass directories as arguments, never include them in logs or artifacts.
						command := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", "../../dev/compose.yml", "-f", "../../dev/compose.ci.yml", "exec", "-T", "crafty", "python3", "-c", "import pathlib,sys; assert all(pathlib.Path(p).is_dir() for p in sys.argv[1:])", previousPath, newPath)
						if command.Run() != nil {
							return fmt.Errorf("replacement directory retention check failed")
						}
					}
					return nil
				}},
			{Config: strings.Replace(config(renamed), "mem_max = 2", "mem_max = 3", 1), PlanOnly: true, ConfigPlanChecks: emptyPlan},
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
