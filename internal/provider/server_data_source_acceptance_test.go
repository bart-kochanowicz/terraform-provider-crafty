package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccServerDataSource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("Set TF_ACC=1 to run Terraform data source tests")
	}
	var name atomic.Value
	name.Store("existing")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/servers" {
			t.Errorf("data source sent unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeServer(w, name.Load().(string))
	}))
	defer api.Close()
	config := func(id string) string {
		return fmt.Sprintf(`
provider "crafty" {
 url = %q
 token = "test-only"
}
data "crafty_server" "existing" {
 id = %q
}
output "server_name" {
 value = data.crafty_server.existing.name
}
`, api.URL, id)
	}
	check := func(expectedName string) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttr("data.crafty_server.existing", "id", "abc"),
			resource.TestCheckResourceAttr("data.crafty_server.existing", "name", expectedName),
			resource.TestCheckResourceAttr("data.crafty_server.existing", "auto_start", "false"),
			resource.TestCheckResourceAttr("data.crafty_server.existing", "monitoring_host", "127.0.0.1"),
			resource.TestCheckResourceAttr("data.crafty_server.existing", "monitoring_port", "25565"),
			resource.TestCheckResourceAttr("data.crafty_server.existing", "execution_command", "java -Xms1000M -Xmx2000M -jar paper.jar nogui"),
		)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"crafty": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: config("abc"), Check: check("existing")},
			{PreConfig: func() { name.Store("renamed") }, Config: config("abc"), Check: check("renamed")},
			{Config: config("missing"), ExpectError: regexp.MustCompile("Server unavailable")},
		},
	})
}
