package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestAccSchedule(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform schedule tests")
	}
	var mu sync.Mutex
	var settings client.ScheduleSettings
	exists := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing authentication")
		}
		path := "/api/v2/servers/abc/tasks"
		if r.URL.Path != path && r.URL.Path != path+"/1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			if !exists {
				w.WriteHeader(500)
				return
			}
			data := struct {
				client.ScheduleSettings
				ID     int64             `json:"schedule_id"`
				Server map[string]string `json:"server_id"`
			}{settings, 1, map[string]string{"server_id": "abc"}}
			if err := json.NewEncoder(w).Encode(data); err != nil {
				t.Error(err)
			}
		case http.MethodPost, http.MethodPatch:
			if r.Method == http.MethodPost && r.URL.Path != path {
				t.Error("provider attempted to run a task")
			}
			if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
				t.Error(err)
			}
			exists = true
			_, _ = w.Write([]byte(`{"status":"ok","data":{"schedule_id":1}}`))
		case http.MethodDelete:
			exists = false
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	}))
	defer api.Close()
	testScheduleLifecycle(t, api.URL, "test-only", "abc", func(id string) error {
		mu.Lock()
		defer mu.Unlock()
		if exists {
			return fmt.Errorf("task %s still exists", id)
		}
		return nil
	})
}

func TestAccScheduleLive(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run live Crafty acceptance tests")
	}
	endpoint, token := os.Getenv("CRAFTY_URL"), os.Getenv("CRAFTY_TOKEN")
	if endpoint == "" || token == "" {
		t.Fatal("CRAFTY_URL and CRAFTY_TOKEN must identify a disposable instance")
	}
	project := os.Getenv("CRAFTY_TEST_COMPOSE_PROJECT")
	if !regexp.MustCompile(`^crafty-provider-ci-[a-z0-9-]+$`).MatchString(project) || endpoint != "http://127.0.0.1:18001" {
		t.Fatal("live schedule verification requires the isolated CI Compose project")
	}
	api := client.New(endpoint, token)
	server := serverModel{Name: types.StringValue("tf-schedule-test"), Engine: types.StringValue("paper"), Version: types.StringValue("1.21.1"), MemMin: types.Int64Value(1), MemMax: types.Int64Value(2), Host: types.StringValue("127.0.0.1"), Port: types.Int64Value(25565)}
	created, err := api.CreateJavaServer(context.Background(), server.createRequest())
	if err != nil || created.ID == "" {
		t.Fatalf("create test server: %v", err)
	}
	t.Cleanup(func() {
		if err := api.DeleteServer(context.Background(), created.ID); err != nil {
			t.Error(err)
		}
	})
	testScheduleLifecycle(t, endpoint, token, created.ID, func(id string) error {
		// A missing-task GET returns an ambiguous 500. The isolated database provides
		// an independent deletion check without interpreting that error as absence.
		taskID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return err
		}
		return checkScheduleDeleted(context.Background(), project, taskID)
	})
}

func checkScheduleDeleted(ctx context.Context, project string, id int64) error {
	command := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", "../../dev/compose.yml", "-f", "../../dev/compose.ci.yml", "exec", "-T", "crafty", "python3", "-c", `import sqlite3,sys; c=sqlite3.connect("file:/crafty/app/config/db/crafty.sqlite?mode=ro",uri=True); assert c.execute("SELECT COUNT(*) FROM schedules WHERE schedule_id = ?",(int(sys.argv[1]),)).fetchone()[0] == 0`, strconv.FormatInt(id, 10))
	if err := command.Run(); err != nil {
		return fmt.Errorf("independent schedule deletion check failed: %w", err)
	}
	return nil
}

func testScheduleLifecycle(t *testing.T, endpoint, token, serverID string, checkDeleted func(string) error) {
	t.Helper()
	t.Setenv("TF_VAR_schedule_test_token", token)
	api := client.New(endpoint, token)
	var id string
	config := func(enabled bool, cron string) string {
		return fmt.Sprintf(`
variable "schedule_test_token" {
 type = string
 sensitive = true
}
provider "crafty" {
 url = %q
 token = var.schedule_test_token
}
resource "crafty_schedule" "test" {
 server_id = %q
 name = "Scheduled command"
 command = "say scheduled-test"
 enabled = %t
 interval = 87600
 interval_type = "hours"
 cron_string = %q
}
`, endpoint, serverID, enabled, cron)
	}
	importStep := func() resource.TestStep {
		return resource.TestStep{ResourceName: "crafty_schedule.test", ImportState: true, ImportStateIdFunc: func(_ *terraform.State) (string, error) { return serverID + "/" + id, nil }, ImportStateVerify: true}
	}
	updatePlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_schedule.test", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy:             func(_ *terraform.State) error { return checkDeleted(id) },
		Steps: []resource.TestStep{
			{Config: config(true, ""), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("crafty_schedule.test", "enabled", "true"), func(state *terraform.State) error {
				id = state.RootModule().Resources["crafty_schedule.test"].Primary.ID
				return nil
			})},
			importStep(),
			{Config: config(false, "0 4 * * mon"), ConfigPlanChecks: updatePlan, Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("crafty_schedule.test", "enabled", "false"), resource.TestCheckResourceAttr("crafty_schedule.test", "cron_string", "0 4 * * mon"))},
			importStep(),
			{PreConfig: func() {
				observed, err := api.GetSchedule(context.Background(), serverID, id)
				if err != nil {
					t.Fatal(err)
				}
				if observed.Enabled == nil || *observed.Enabled || observed.Cron == nil || *observed.Cron != "0 4 * * mon" {
					t.Fatal("disabled cron configuration was not persisted")
				}
				settings := client.ScheduleSettings{Name: "External edit", Enabled: false, Action: "command", Command: "say external", Interval: 87600, IntervalType: "hours", StartTime: "00:00", Cron: "0 4 * * mon"}
				if err := api.UpdateSchedule(context.Background(), serverID, id, settings); err != nil {
					t.Fatal(err)
				}
			}, Config: config(false, "0 4 * * mon"), ConfigPlanChecks: updatePlan, Check: resource.TestCheckResourceAttr("crafty_schedule.test", "command", "say scheduled-test")},
			{Config: config(true, ""), ConfigPlanChecks: updatePlan},
			{Config: config(true, ""), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
}
