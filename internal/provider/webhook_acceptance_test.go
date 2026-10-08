package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestAccWebhook(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform webhook tests")
	}
	var mu sync.Mutex
	settings := map[string]client.WebhookSettings{}
	var nextID int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing authentication")
		}
		if r.URL.Path != "/api/v2/servers/abc/webhook" && !strings.HasPrefix(r.URL.Path, "/api/v2/servers/abc/webhook/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v2/servers/abc/webhook/")
		switch r.Method {
		case http.MethodGet:
			data := map[string]any{}
			for id, item := range settings {
				data[id] = map[string]any{"webhook_type": item.Type, "name": item.Name, "url": item.URL, "bot_name": item.BotName, "trigger": strings.Join(item.Triggers, ",") + ",", "body": item.Body, "color": item.Color, "enabled": item.Enabled}
				if len(item.Triggers) == 0 {
					data[id].(map[string]any)["trigger"] = ""
				}
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": data}); err != nil {
				t.Error(err)
			}
		case http.MethodPost, http.MethodPatch:
			var item client.WebhookSettings
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				t.Error(err)
			}
			if r.Method == http.MethodPost {
				if r.URL.Path != "/api/v2/servers/abc/webhook" {
					t.Error("provider attempted to send a test notification")
				}
				nextID++
				id = fmt.Sprint(nextID)
			}
			settings[id] = item
			if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": map[string]any{"webhook_id": nextID}}); err != nil {
				t.Error(err)
			}
		case http.MethodDelete:
			delete(settings, id)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	}))
	defer api.Close()
	testWebhookLifecycle(t, api.URL, "test-only", "abc")
}

func TestAccWebhookLive(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run live Crafty acceptance tests")
	}
	endpoint, token := os.Getenv("CRAFTY_URL"), os.Getenv("CRAFTY_TOKEN")
	if endpoint == "" || token == "" {
		t.Fatal("CRAFTY_URL and CRAFTY_TOKEN must identify a disposable Crafty instance")
	}
	api := client.New(endpoint, token)
	created, err := api.CreateJavaServer(context.Background(), client.CreateJavaServerRequest{
		Name: "tf-webhook-test", MonitoringType: "minecraft_java", CreateType: "minecraft_java",
		Monitoring: client.JavaMonitoring{Host: "127.0.0.1", Port: 25565},
		Java:       client.JavaCreateData{CreateType: "download_jar", Download: client.JavaDownload{Category: "mc_java_servers", Engine: "paper", Version: "1.21.1", MemMin: 1, MemMax: 2, Port: 25565}},
	})
	if err != nil || created.ID == "" {
		t.Fatalf("create test server: %v", err)
	}
	t.Cleanup(func() {
		if err := api.DeleteServer(context.Background(), created.ID); err != nil {
			t.Error(err)
		}
	})
	testWebhookLifecycle(t, endpoint, token, created.ID)
}

func testWebhookLifecycle(t *testing.T, endpoint, token, serverID string) {
	t.Helper()
	t.Setenv("TF_VAR_webhook_test_token", token)
	api := client.New(endpoint, token)
	var id string
	config := func(name string, enabled bool, triggers string) string {
		return fmt.Sprintf(`
variable "webhook_test_token" {
 type = string
 sensitive = true
}
provider "crafty" {
 url = %q
 token = var.webhook_test_token
}
resource "crafty_webhook" "test" {
 server_id = %q
 webhook_type = "Discord"
 name = %q
 url = "https://example.com/webhook/test-only"
 triggers = %s
 body = ""
 enabled = %t
}
`, endpoint, serverID, name, triggers, enabled)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(_ *terraform.State) error {
			items, err := api.ListWebhooks(context.Background(), serverID)
			if err != nil {
				return err
			}
			if len(items) != 0 {
				return fmt.Errorf("webhooks remain after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config("initial", false, `["stop_server", "start_server"]`), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("crafty_webhook.test", "enabled", "false"),
				resource.TestCheckResourceAttr("crafty_webhook.test", "triggers.#", "2"),
				resource.TestCheckResourceAttr("crafty_webhook.test", "bot_name", "Crafty Controller"),
				func(state *terraform.State) error {
					id = state.RootModule().Resources["crafty_webhook.test"].Primary.ID
					return nil
				},
			)},
			{ResourceName: "crafty_webhook.test", ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return serverID + "/" + id, nil }, ImportStateVerify: true},
			{Config: config("updated", true, `[]`), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_webhook.test", plancheck.ResourceActionUpdate)}}, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("crafty_webhook.test", "name", "updated"),
				resource.TestCheckResourceAttr("crafty_webhook.test", "enabled", "true"),
				resource.TestCheckResourceAttr("crafty_webhook.test", "triggers.#", "0"),
			)},
			{PreConfig: func() {
				items, err := api.ListWebhooks(context.Background(), serverID)
				if err != nil {
					t.Fatal(err)
				}
				if items[id].Name == nil || *items[id].Name != "updated" {
					t.Fatal("update was not persisted")
				}
				err = api.UpdateWebhook(context.Background(), serverID, id, client.WebhookSettings{Type: "Discord", Name: "external", URL: "https://example.com/webhook/test-only", BotName: "Crafty Controller", Triggers: []string{}, Body: "", Color: "#005cd1", Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
			}, Config: config("updated", true, `[]`), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_webhook.test", plancheck.ResourceActionUpdate)}}},
			{Config: config("updated", true, `[]`), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{PreConfig: func() {
				if err := api.DeleteWebhook(context.Background(), serverID, id); err != nil {
					t.Fatal(err)
				}
			}, Config: config("updated", true, `[]`), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_webhook.test", plancheck.ResourceActionCreate)}}},
		},
	})
}
