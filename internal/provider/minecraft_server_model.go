package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

type serverModel struct {
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
	ID        types.String   `tfsdk:"id"`
	Name      types.String   `tfsdk:"name"`
	Engine    types.String   `tfsdk:"engine"`
	Version   types.String   `tfsdk:"version"`
	MemMin    types.Int64    `tfsdk:"mem_min"`
	MemMax    types.Int64    `tfsdk:"mem_max"`
	Host      types.String   `tfsdk:"host"`
	Port      types.Int64    `tfsdk:"port"`
	AutoStart types.Bool     `tfsdk:"auto_start"`
}

func (m serverModel) validate() error {
	if m.Name.ValueString() == "" || m.Engine.ValueString() == "" || m.Version.ValueString() == "" || m.Host.ValueString() == "" {
		return fmt.Errorf("name, engine, version, and host must not be empty")
	}
	if m.MemMin.ValueInt64() < 1 || m.MemMax.ValueInt64() < m.MemMin.ValueInt64() {
		return fmt.Errorf("memory values must satisfy 1 <= mem_min <= mem_max")
	}
	if m.Port.ValueInt64() < 1 || m.Port.ValueInt64() > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func (m serverModel) createRequest() client.CreateJavaServerRequest {
	return client.CreateJavaServerRequest{
		Name: m.Name.ValueString(), MonitoringType: "minecraft_java", CreateType: "minecraft_java",
		Monitoring: client.JavaMonitoring{Host: m.Host.ValueString(), Port: m.Port.ValueInt64()},
		Java: client.JavaCreateData{CreateType: "download_jar", Download: client.JavaDownload{
			Category: "mc_java_servers", Engine: m.Engine.ValueString(), Version: m.Version.ValueString(),
			MemMin: m.MemMin.ValueInt64(), MemMax: m.MemMax.ValueInt64(), Port: m.Port.ValueInt64(),
		}},
	}
}
