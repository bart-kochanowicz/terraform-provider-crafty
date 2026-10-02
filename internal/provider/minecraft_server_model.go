package provider

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

type serverModel struct {
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
	ID               types.String   `tfsdk:"id"`
	Name             types.String   `tfsdk:"name"`
	Engine           types.String   `tfsdk:"engine"`
	Version          types.String   `tfsdk:"version"`
	MemMin           types.Int64    `tfsdk:"mem_min"`
	MemMax           types.Int64    `tfsdk:"mem_max"`
	Host             types.String   `tfsdk:"host"`
	Port             types.Int64    `tfsdk:"port"`
	AutoStart        types.Bool     `tfsdk:"auto_start"`
	MonitoringHost   types.String   `tfsdk:"monitoring_host"`
	MonitoringPort   types.Int64    `tfsdk:"monitoring_port"`
	ExecutionCommand types.String   `tfsdk:"execution_command"`
}

func (m serverModel) validate() error {
	if m.Name.ValueString() == "" || m.Engine.ValueString() == "" || m.Version.ValueString() == "" || m.Host.ValueString() == "" {
		return fmt.Errorf("name, engine, version, and host must not be empty")
	}
	if !validServerName(m.Name.ValueString()) {
		return fmt.Errorf("name must contain at least two characters and must not contain slashes, backslashes, or #")
	}
	if m.MemMin.ValueInt64() < 1 || m.MemMax.ValueInt64() < m.MemMin.ValueInt64() {
		return fmt.Errorf("memory values must satisfy 1 <= mem_min <= mem_max")
	}
	if m.Port.ValueInt64() < 1 || m.Port.ValueInt64() > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if !m.MonitoringPort.IsNull() && !m.MonitoringPort.IsUnknown() && (m.MonitoringPort.ValueInt64() < 1 || m.MonitoringPort.ValueInt64() > 65535) {
		return fmt.Errorf("monitoring_port must be between 1 and 65535")
	}
	for _, v := range []types.String{m.MonitoringHost, m.ExecutionCommand} {
		if !v.IsNull() && !v.IsUnknown() && strings.TrimSpace(v.ValueString()) == "" {
			return fmt.Errorf("explicit monitoring_host and execution_command must not be blank")
		}
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

// Crafty 4.10.4 validates the same name constraints on create and PATCH.
func validServerName(name string) bool {
	return utf8.RuneCountInString(name) >= 2 && !strings.ContainsAny(name, "/\\#")
}

// settingsPatch excludes unknown computed values and unchanged settings.
func (m serverModel) settingsPatch(prior *serverModel) client.UpdateServerRequest {
	p := client.UpdateServerRequest{}
	if prior != nil && !m.Name.Equal(prior.Name) {
		p.Name = m.Name.ValueString()
	}
	if !m.AutoStart.IsNull() && !m.AutoStart.IsUnknown() && (prior == nil || !m.AutoStart.Equal(prior.AutoStart)) {
		v := m.AutoStart.ValueBool()
		p.AutoStart = &v
	}
	if !m.MonitoringHost.IsNull() && !m.MonitoringHost.IsUnknown() && (prior == nil || !m.MonitoringHost.Equal(prior.MonitoringHost)) {
		v := m.MonitoringHost.ValueString()
		p.MonitoringHost = &v
	}
	if !m.MonitoringPort.IsNull() && !m.MonitoringPort.IsUnknown() && (prior == nil || !m.MonitoringPort.Equal(prior.MonitoringPort)) {
		v := m.MonitoringPort.ValueInt64()
		p.MonitoringPort = &v
	}
	if !m.ExecutionCommand.IsNull() && !m.ExecutionCommand.IsUnknown() && (prior == nil || !m.ExecutionCommand.Equal(prior.ExecutionCommand)) {
		v := m.ExecutionCommand.ValueString()
		p.ExecutionCommand = &v
	}
	return p
}

func emptyPatch(p client.UpdateServerRequest) bool {
	return p.Name == "" && p.AutoStart == nil && p.MonitoringHost == nil && p.MonitoringPort == nil && p.ExecutionCommand == nil
}

func (m serverModel) matchesPatch(p *client.UpdateServerRequest) bool {
	return p == nil || ((p.Name == "" || m.Name.ValueString() == p.Name) &&
		(p.AutoStart == nil || (!m.AutoStart.IsNull() && m.AutoStart.ValueBool() == *p.AutoStart)) &&
		(p.MonitoringHost == nil || (!m.MonitoringHost.IsNull() && m.MonitoringHost.ValueString() == *p.MonitoringHost)) &&
		(p.MonitoringPort == nil || (!m.MonitoringPort.IsNull() && m.MonitoringPort.ValueInt64() == *p.MonitoringPort)) &&
		(p.ExecutionCommand == nil || (!m.ExecutionCommand.IsNull() && m.ExecutionCommand.ValueString() == *p.ExecutionCommand)))
}

// Unconfigured Optional+Computed settings adopt the observed values. Configured
// settings keep the plan until GET confirms the accepted PATCH, including false.
func (m *serverModel) adoptUnknownSettings(observed serverModel) {
	if m.AutoStart.IsUnknown() || m.AutoStart.IsNull() {
		m.AutoStart = observed.AutoStart
	}
	if m.MonitoringHost.IsUnknown() || m.MonitoringHost.IsNull() {
		m.MonitoringHost = observed.MonitoringHost
	}
	if m.MonitoringPort.IsUnknown() || m.MonitoringPort.IsNull() {
		m.MonitoringPort = observed.MonitoringPort
	}
	if m.ExecutionCommand.IsUnknown() || m.ExecutionCommand.IsNull() {
		m.ExecutionCommand = observed.ExecutionCommand
	}
}
