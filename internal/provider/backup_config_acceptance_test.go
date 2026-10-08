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
	"strings"
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

func TestAccBackupConfig(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform backup policy tests")
	}
	var mu sync.Mutex
	policy := map[string]any{"backup_id": "backup", "server_id": "abc", "backup_name": "Existing policy", "backup_location": "/backups", "max_backups": 0, "compress": false, "shutdown": false, "before": "", "after": "", "excluded_dirs": nil}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing authentication")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/servers/abc/backups":
			if err := json.NewEncoder(w).Encode(map[string]any{"backup": policy}); err != nil {
				t.Error(err)
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/servers/abc/backups/backup/backup":
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Error(err)
			}
			for key, value := range patch {
				if key == "excluded_dirs" {
					values := value.([]any)
					dirs := make([]string, 0, len(values))
					for _, v := range values {
						dirs = append(dirs, v.(string))
					}
					value = strings.Join(dirs, ",")
				}
				policy[key] = value
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected backup operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(405)
		}
	}))
	defer api.Close()
	testBackupConfigLifecycle(t, api.URL, "test-only", "abc", "backup", nil)
}

func TestAccBackupConfigLive(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run live Crafty acceptance tests")
	}
	endpoint, token, project := os.Getenv("CRAFTY_URL"), os.Getenv("CRAFTY_TOKEN"), os.Getenv("CRAFTY_TEST_COMPOSE_PROJECT")
	if token == "" || endpoint != "http://127.0.0.1:18001" || !regexp.MustCompile(`^crafty-provider-ci-[a-z0-9-]+$`).MatchString(project) {
		t.Fatal("live backup verification requires the isolated CI Compose project")
	}
	api := client.New(endpoint, token)
	server := serverModel{Name: types.StringValue("tf-backup-test"), Engine: types.StringValue("paper"), Version: types.StringValue("1.21.1"), MemMin: types.Int64Value(1), MemMax: types.Int64Value(2), Host: types.StringValue("127.0.0.1"), Port: types.Int64Value(25565)}
	created, err := api.CreateJavaServer(context.Background(), server.createRequest())
	if err != nil || created.ID == "" {
		t.Fatalf("create test server: %v", err)
	}
	t.Cleanup(func() {
		if err := api.DeleteServer(context.Background(), created.ID); err != nil {
			t.Error(err)
		}
	})
	policies, err := api.ListBackupConfigs(context.Background(), created.ID)
	if err != nil || len(policies) != 1 {
		t.Fatalf("expected the test server's single default policy: %v", err)
	}
	var id, location string
	for key, policy := range policies {
		if policy.Location == nil {
			t.Fatal("missing backup location")
		}
		id, location = key, *policy.Location
	}
	// This fixture resembles an existing archive; no backup operation is invoked.
	archive := func(check bool) error {
		code := `import pathlib,sys; p=pathlib.Path(sys.argv[1])/sys.argv[2]/"terraform-preservation.zip"; p.parent.mkdir(parents=True,exist_ok=True); p.write_bytes(b"test-archive")`
		if check {
			code = `import pathlib,sys; p=pathlib.Path(sys.argv[1])/sys.argv[2]/"terraform-preservation.zip"; assert p.read_bytes()==b"test-archive"`
		}
		command := exec.CommandContext(context.Background(), "docker", "compose", "-p", project, "-f", "../../dev/compose.yml", "-f", "../../dev/compose.ci.yml", "exec", "-T", "crafty", "python3", "-c", code, location, id)
		if command.Run() != nil {
			return fmt.Errorf("archive preservation check failed")
		}
		return nil
	}
	if err := archive(false); err != nil {
		t.Fatal(err)
	}
	testBackupConfigLifecycle(t, endpoint, token, created.ID, id, func() error { return archive(true) })
}

func testBackupConfigLifecycle(t *testing.T, endpoint, token, serverID, backupID string, checkArchive func() error) {
	t.Helper()
	t.Setenv("TF_VAR_backup_test_token", token)
	api := client.New(endpoint, token)
	config := func(settings string) string {
		return fmt.Sprintf(`
variable "backup_test_token" {
 type = string
 sensitive = true
}
provider "crafty" {
 url = %q
 token = var.backup_test_token
}
resource "crafty_backup_config" "test" {
 server_id = %q
 backup_id = %q
 %s
}
`, endpoint, serverID, backupID, settings)
	}
	policies, err := api.ListBackupConfigs(context.Background(), serverID)
	if err != nil || policies[backupID].Location == nil {
		t.Fatalf("read initial backup location: %v", err)
	}
	location := *policies[backupID].Location + "/terraform-policy-test"
	initial := fmt.Sprintf(`name = "Managed policy"
 backup_location = %q
 max_backups = 4
 compress = true
 shutdown = true
 before = "save-all"
 after = "say backup-complete"
 excluded_dirs = ["logs", "cache"]`, location)
	updated := `name = "Managed policy"
 max_backups = 0
 compress = false
 shutdown = false
 before = ""
 after = ""
 excluded_dirs = []`
	check := resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("crafty_backup_config.test", "backup_location", location), resource.TestCheckResourceAttr("crafty_backup_config.test", "max_backups", "0"), resource.TestCheckResourceAttr("crafty_backup_config.test", "compress", "false"), resource.TestCheckResourceAttr("crafty_backup_config.test", "shutdown", "false"), resource.TestCheckResourceAttr("crafty_backup_config.test", "excluded_dirs.#", "0"))
	updatePlan := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_backup_config.test", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy: func(_ *terraform.State) error {
			policies, err := api.ListBackupConfigs(context.Background(), serverID)
			if err != nil {
				return err
			}
			policy, ok := policies[backupID]
			if !ok || policy.Name == nil || *policy.Name != "External policy" {
				return fmt.Errorf("destroy removed or reset the backup policy")
			}
			if checkArchive != nil {
				return checkArchive()
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: config(""), Check: resource.TestCheckResourceAttr("crafty_backup_config.test", "id", backupID)},
			{ResourceName: "crafty_backup_config.test", ImportState: true, ImportStateId: serverID + "/" + backupID, ImportStateVerify: true},
			{Config: config(initial), ConfigPlanChecks: updatePlan, Check: resource.TestCheckResourceAttr("crafty_backup_config.test", "excluded_dirs.#", "2")},
			{Config: config(updated), ConfigPlanChecks: updatePlan, Check: check},
			{PreConfig: func() {
				v := int64(9)
				if err := api.UpdateBackupConfig(context.Background(), serverID, backupID, client.BackupConfigPatch{MaxBackups: &v}); err != nil {
					t.Fatal(err)
				}
			}, Config: config(updated), ConfigPlanChecks: updatePlan, Check: check},
			{Config: config(""), Check: check},
			{PreConfig: func() {
				name, before := "External policy", "external-command"
				if err := api.UpdateBackupConfig(context.Background(), serverID, backupID, client.BackupConfigPatch{Name: &name, Before: &before}); err != nil {
					t.Fatal(err)
				}
			}, Config: config(""), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: config(""), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("crafty_backup_config.test", "name", "External policy"), resource.TestCheckResourceAttr("crafty_backup_config.test", "before", "external-command"))},
		},
	})
}
