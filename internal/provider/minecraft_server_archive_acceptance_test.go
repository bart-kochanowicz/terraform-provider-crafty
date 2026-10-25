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
	"time"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccMinecraftServerArchive(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform archive tests")
	}
	var mu sync.Mutex
	name, archiveName := "", ""
	posts, uploads, deletes := 0, 0, 0
	exists := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/v2/servers/import/upload":
			if r.Header.Get("chunkId") == "" {
				archiveName = r.Header.Get("fileName")
				_, _ = fmt.Fprintf(w, `{"status":"ok","data":{"file-id":%q}}`, r.Header.Get("fileId"))
			} else {
				uploads++
				_, _ = w.Write([]byte(`{"status":"completed"}`))
			}
		case r.Method == "POST":
			var body client.CreateJavaServerRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if uploads != 1 || body.Java.CreateType != "import_server" || body.Java.Download != nil || body.Java.Import == nil || body.Java.Import.ArchiveName != archiveName || body.Java.Import.InternalPath != "pack" || body.Java.Import.Executable != "server.jar" {
				t.Error("server creation did not use the uploaded ZIP")
			}
			posts++
			exists = true
			name = body.Name
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
		case r.Method == "GET":
			if exists {
				writeServer(w, name)
			} else {
				_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
			}
		case r.Method == "PATCH":
			var body client.UpdateServerRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			name = body.Name
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == "DELETE":
			if r.URL.RawQuery != "" {
				t.Error("destroy must preserve server files")
			}
			deletes++
			exists = false
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()
	filename, hash := archiveFixture(t, map[string]string{"pack/server.jar": "test", "pack/config/settings.toml": "enabled=true"})
	testArchiveLifecycle(t, api.URL, "test-only", filename, hash, nil, func(*terraform.State) error {
		mu.Lock()
		defer mu.Unlock()
		if exists || posts != 1 || uploads != 1 || deletes != 1 {
			return fmt.Errorf("unexpected archive lifecycle: created=%d uploaded=%d deleted=%d", posts, uploads, deletes)
		}
		return nil
	})
}

func TestAccMinecraftServerArchiveLive(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run live archive tests")
	}
	endpoint, token, project := os.Getenv("CRAFTY_URL"), os.Getenv("CRAFTY_TOKEN"), os.Getenv("CRAFTY_TEST_COMPOSE_PROJECT")
	if token == "" || endpoint != "http://127.0.0.1:18001" || !regexp.MustCompile(`^crafty-provider-ci-[a-z0-9-]+$`).MatchString(project) {
		t.Fatal("archive integration requires the isolated CI Compose instance")
	}
	filename, hash := archiveFixture(t, map[string]string{"pack/server.jar": "server-pack-test", "pack/config/settings.toml": "enabled=true", "pack/payload.bin": strings.Repeat("payload", 80000)})
	api := client.New(endpoint, token)
	id, serverPath := "", ""
	check := func(state *terraform.State) error {
		id = state.RootModule().Resources["crafty_minecraft_server.test"].Primary.ID
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		serverPath, err = replacementServerPath(ctx, endpoint, token, id)
		if err != nil {
			return err
		}
		for {
			command := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", "../../dev/compose.yml", "-f", "../../dev/compose.ci.yml", "exec", "-T", "crafty", "python3", "-c", "from pathlib import Path; import sys; p=Path(sys.argv[1]); assert (p/'server.jar').read_text()=='server-pack-test'; assert (p/'config/settings.toml').read_text()=='enabled=true'; assert (p/'payload.bin').stat().st_size==560000", serverPath)
			if command.Run() == nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("ZIP server pack extraction was not verified")
			case <-time.After(time.Second):
			}
		}
	}
	destroy := func(*terraform.State) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := waitForServerDeletion(ctx, api, id); err != nil {
			return err
		}
		command := exec.CommandContext(ctx, "docker", "compose", "-p", project, "-f", "../../dev/compose.yml", "-f", "../../dev/compose.ci.yml", "exec", "-T", "crafty", "python3", "-c", "from pathlib import Path; import sys; assert (Path(sys.argv[1])/'config/settings.toml').read_text()=='enabled=true'", serverPath)
		if command.Run() != nil {
			return fmt.Errorf("destroy did not preserve the extracted server files")
		}
		return nil
	}
	testArchiveLifecycle(t, endpoint, token, filename, hash, check, destroy)
}

func testArchiveLifecycle(t *testing.T, endpoint, token, filename, hash string, check, destroy resource.TestCheckFunc) {
	t.Helper()
	t.Setenv("TF_VAR_archive_test_token", token)
	config := func(name string) string {
		return fmt.Sprintf(`
variable "archive_test_token" {
 type = string
 sensitive = true
}
provider "crafty" {
 url = %q
 token = var.archive_test_token
}
resource "crafty_minecraft_server" "test" {
 name = %q
 archive_path = %q
 archive_sha256 = %q
 archive_internal_path = "pack"
 executable = "server.jar"
 host = "127.0.0.1"
 port = 25591
 mem_min = 1
 mem_max = 2
}
`, endpoint, name, filename, hash)
	}
	empty := resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}
	checks := []resource.TestCheckFunc{resource.TestCheckResourceAttrSet("crafty_minecraft_server.test", "id")}
	if check != nil {
		checks = append(checks, check)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		CheckDestroy:             destroy,
		Steps: []resource.TestStep{
			{Config: config("archive test"), Check: resource.ComposeTestCheckFunc(checks...)},
			{Config: config("archive test"), PlanOnly: true, ConfigPlanChecks: empty},
			{Config: config("archive renamed"), Check: resource.TestCheckResourceAttr("crafty_minecraft_server.test", "name", "archive renamed")},
			{Config: config("archive renamed"), PlanOnly: true, ConfigPlanChecks: empty},
			{Config: strings.Replace(config("archive renamed"), "mem_max = 2", "mem_max = 3", 1), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction("crafty_minecraft_server.test", plancheck.ResourceActionDestroyBeforeCreate)}}},
			{Config: config("archive renamed"), PlanOnly: true, ConfigPlanChecks: empty},
		},
	})
}
