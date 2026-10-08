package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*serverResource)(nil)
var _ resource.ResourceWithConfigure = (*serverResource)(nil)

type serverResource struct {
	client       *client.Client
	pollInterval time.Duration
}

func (s *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, r *resource.MetadataResponse) {
	r.TypeName = req.ProviderTypeName + "_minecraft_server"
}
func (s *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, r *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		r.Diagnostics.AddError("Invalid provider client", "Expected a Crafty API client.")
		return
	}
	s.client = c
}
func (s *serverResource) Create(ctx context.Context, req resource.CreateRequest, r *resource.CreateResponse) {
	var m serverModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if err := m.validate(); err != nil {
		r.Diagnostics.AddError("Invalid server configuration", err.Error())
		return
	}
	duration, diags := m.Timeouts.Create(ctx, 10*time.Minute)
	r.Diagnostics.Append(diags...)
	if r.Diagnostics.HasError() {
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	created, err := s.client.CreateJavaServer(opCtx, m.createRequest())
	if err != nil {
		r.Diagnostics.AddError("Unable to create Minecraft server", err.Error()+" The POST request was not retried. Its outcome may be uncertain; inspect Crafty before applying again.")
		return
	}
	if created.ID == "" {
		r.Diagnostics.AddError("Missing server ID", "Crafty did not return new_server_id. Check the panel before retrying to avoid duplicate servers.")
		return
	}
	m.ID = types.StringValue(created.ID)
	requested := m
	m.adoptUnknownSettings(serverModel{})
	// Persist identity and a private preparation marker before any further HTTP call.
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Private != nil {
		r.Diagnostics.Append(r.Private.SetKey(ctx, pendingRefreshKey, []byte("true"))...)
	}
	if r.Diagnostics.HasError() {
		return
	}
	pending := func(cause error) {
		r.Diagnostics.AddWarning("Created server configuration pending", fmt.Sprintf("Crafty returned server ID %s, which is saved in state. %s Run terraform plan and apply again after preparation or API recovery to reconcile the configured settings; do not replace the server to retry configuration.", created.ID, cause))
	}
	observed := m
	_, err = s.waitRefresh(opCtx, &observed, true, &client.UpdateServerRequest{Name: m.Name.ValueString()})
	if err != nil {
		pending(err)
		return
	}
	m.adoptUnknownSettings(observed)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	patch := requested.settingsPatch(nil)
	if !emptyPatch(patch) {
		if err := s.client.UpdateServer(opCtx, m.ID.ValueString(), patch); err != nil {
			pending(fmt.Errorf("initial settings PATCH failed and was not retried: %w", err))
			return
		}
		patch.Name = m.Name.ValueString()
		_, err = s.waitRefresh(opCtx, &m, true, &patch)
		if err != nil {
			pending(err)
			return
		}
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Private != nil {
		r.Diagnostics.Append(r.Private.SetKey(ctx, pendingRefreshKey, nil)...)
	}
}

// Crafty returns server objects from both GET endpoints. The provider
// uses the collection to reconcile IDs; missing single GET returns ambiguous
// HTTP 400 NOT_AUTHORIZED rather than a reliable not-found status.
func (s *serverResource) refresh(ctx context.Context, m *serverModel) (bool, error) {
	servers, err := s.client.ListServers(ctx)
	if err != nil {
		return false, err
	}
	for _, remote := range servers {
		if remote.ID == m.ID.ValueString() {
			if remote.Name == nil || remote.AutoStart == nil || remote.MonitoringHost == nil || remote.MonitoringPort == nil || remote.ExecutionCommand == nil {
				return false, errIncompleteServer
			}
			m.Name = types.StringValue(*remote.Name)
			m.AutoStart = types.BoolValue(*remote.AutoStart)
			m.MonitoringHost = types.StringValue(*remote.MonitoringHost)
			m.MonitoringPort = types.Int64Value(*remote.MonitoringPort)
			m.ExecutionCommand = types.StringValue(*remote.ExecutionCommand)
			// Preserve original download inputs; GET does not reconstruct their payload.
			return true, nil
		}
	}
	return false, nil
}
func (s *serverResource) Read(ctx context.Context, req resource.ReadRequest, r *resource.ReadResponse) {
	var m serverModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	duration, diags := m.Timeouts.Read(ctx, 2*time.Minute)
	r.Diagnostics.Append(diags...)
	marker, diags := req.Private.GetKey(ctx, pendingRefreshKey)
	r.Diagnostics.Append(diags...)
	if r.Diagnostics.HasError() {
		return
	}
	pending := string(marker) == "true"
	opCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	found, err := s.waitRefresh(opCtx, &m, pending, nil)
	if err != nil {
		if pending {
			r.Diagnostics.AddWarning("Server refresh pending", fmt.Sprintf("Server ID %s remains in state. %s Run terraform plan again after preparation or API recovery.", m.ID.ValueString(), err))
		} else {
			r.Diagnostics.AddError("Unable to read Minecraft server", err.Error())
		}
		return
	}
	if !found {
		r.Diagnostics.AddError("Minecraft server is not visible", fmt.Sprintf("Server %s remains in Terraform state. Check token permissions and confirm its status in Crafty before changing state.", m.ID.ValueString()))
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Private != nil {
		r.Diagnostics.Append(r.Private.SetKey(ctx, pendingRefreshKey, nil)...)
	}
}
func (s *serverResource) Update(ctx context.Context, req resource.UpdateRequest, r *resource.UpdateResponse) {
	var m serverModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if err := m.validate(); err != nil {
		r.Diagnostics.AddError("Invalid server configuration", err.Error())
		return
	}
	duration, diags := m.Timeouts.Update(ctx, 5*time.Minute)
	r.Diagnostics.Append(diags...)
	var prior serverModel
	r.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if r.Diagnostics.HasError() {
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	m.adoptUnknownSettings(prior)
	patch := m.settingsPatch(&prior)
	if emptyPatch(patch) {
		r.Diagnostics.Append(r.State.Set(ctx, &m)...)
		return
	}
	if err := s.client.UpdateServer(opCtx, m.ID.ValueString(), patch); err != nil {
		r.Diagnostics.AddError("Unable to update Minecraft server", err.Error())
		return
	}
	// Preserve accepted settings and all known computed values if the read fails.
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Private != nil {
		r.Diagnostics.Append(r.Private.SetKey(ctx, pendingRefreshKey, []byte("true"))...)
	}
	if r.Diagnostics.HasError() {
		return
	}
	_, err := s.waitRefresh(opCtx, &m, true, &patch)
	if err != nil {
		r.Diagnostics.AddWarning("Updated server refresh pending", fmt.Sprintf("Crafty accepted the configuration of server %s, which is saved in state. %s Run terraform plan again after API recovery.", m.ID.ValueString(), err))
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	if r.Private != nil {
		r.Diagnostics.Append(r.Private.SetKey(ctx, pendingRefreshKey, nil)...)
	}
}
func (s *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, r *resource.DeleteResponse) {
	var m serverModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	duration, diags := m.Timeouts.Delete(ctx, 5*time.Minute)
	r.Diagnostics.Append(diags...)
	if r.Diagnostics.HasError() {
		return
	}
	opCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	err := s.client.DeleteServer(opCtx, m.ID.ValueString())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		r.Diagnostics.AddError("Unable to delete Minecraft server", err.Error())
		return
	}
	if err := s.waitDeleted(opCtx, &m); err != nil {
		r.Diagnostics.AddError("Unable to confirm server deletion", err.Error())
	}
}

var _ resource.ResourceWithValidateConfig = (*serverResource)(nil)

func (s *serverResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, r *resource.ValidateConfigResponse) {
	var m serverModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	for name, value := range m.Timeouts.Attributes() {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		text := value.(types.String).ValueString()
		duration, err := time.ParseDuration(text)
		if err != nil || duration <= 0 {
			r.Diagnostics.AddError("Invalid operation timeout", fmt.Sprintf("timeouts.%s must be a positive duration, such as 30s or 10m.", name))
		}
	}
	if !m.Name.IsNull() && !m.Name.IsUnknown() && !validServerName(m.Name.ValueString()) {
		r.Diagnostics.AddError("Invalid server name", "Name must contain at least two characters and must not contain slashes, backslashes, or #.")
	}
	for _, v := range []types.String{m.Name, m.Engine, m.Version, m.Host} {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() == "" {
			r.Diagnostics.AddError("Invalid server configuration", "Name, engine, version, and host must not be empty.")
		}
	}
	if !m.Port.IsNull() && !m.Port.IsUnknown() && (m.Port.ValueInt64() < 1 || m.Port.ValueInt64() > 65535) {
		r.Diagnostics.AddError("Invalid server port", "Port must be between 1 and 65535.")
	}
	if !m.MonitoringPort.IsNull() && !m.MonitoringPort.IsUnknown() && (m.MonitoringPort.ValueInt64() < 1 || m.MonitoringPort.ValueInt64() > 65535) {
		r.Diagnostics.AddError("Invalid monitoring port", "monitoring_port must be between 1 and 65535.")
	}
	for _, v := range []types.String{m.MonitoringHost, m.ExecutionCommand} {
		if !v.IsNull() && !v.IsUnknown() && strings.TrimSpace(v.ValueString()) == "" {
			r.Diagnostics.AddError("Invalid server setting", "Explicit monitoring_host and execution_command values must not be blank.")
		}
	}

	if !m.MemMin.IsNull() && !m.MemMin.IsUnknown() && m.MemMin.ValueInt64() < 1 {
		r.Diagnostics.AddError("Invalid minimum memory", "mem_min must be at least 1.")
	}
	if !m.MemMax.IsNull() && !m.MemMax.IsUnknown() && m.MemMax.ValueInt64() < 1 {
		r.Diagnostics.AddError("Invalid maximum memory", "mem_max must be at least 1.")
	}
	if !m.MemMin.IsUnknown() && !m.MemMax.IsUnknown() && !m.MemMin.IsNull() && !m.MemMax.IsNull() && m.MemMax.ValueInt64() < m.MemMin.ValueInt64() {
		r.Diagnostics.AddError("Invalid memory range", "mem_max must be greater than or equal to mem_min.")
	}
}

// NewMinecraftServerResource creates the Minecraft Java resource.
func NewMinecraftServerResource() resource.Resource { return &serverResource{} }

var _ resource.ResourceWithImportState = (*serverResource)(nil)

// ID-only import cannot recover required download metadata from the verified API.
func (s *serverResource) ImportState(_ context.Context, _ resource.ImportStateRequest, r *resource.ImportStateResponse) {
	r.Diagnostics.AddError("Import unavailable for the verified API", "Verified Crafty GET responses do not reconstruct the required engine, version, original memory inputs, and server.properties port. Import by ID is unavailable; the provider will not guess these values from filenames or arbitrary execution commands. No server was modified.")
}
