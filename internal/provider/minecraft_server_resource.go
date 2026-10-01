package provider

import (
	"context"
	"fmt"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*serverResource)(nil)
var _ resource.ResourceWithConfigure = (*serverResource)(nil)

type serverResource struct{ client *client.Client }

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
	created, err := s.client.CreateJavaServer(ctx, m.createRequest())
	if err != nil {
		r.Diagnostics.AddError("Unable to create Minecraft server", err.Error())
		return
	}
	if created.ID == "" {
		r.Diagnostics.AddError("Missing server ID", "Crafty did not return new_server_id. Check the panel before retrying to avoid duplicate servers.")
		return
	}
	m.ID = types.StringValue(created.ID)
	m.AutoStart = types.BoolNull()
	// Persist the ID before refreshing so a refresh failure does not orphan the server.
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
	found, err := s.refresh(ctx, &m)
	if err != nil {
		r.Diagnostics.AddError("Unable to read created server", err.Error())
		return
	}
	if !found {
		r.Diagnostics.AddError("Created server not visible", "The server ID is saved. Run terraform refresh after Crafty finishes preparing the server.")
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}

// The single-server GET schema incorrectly describes a role. The documented
// collection GET provides the Server schema and is used for authoritative reads.
func (s *serverResource) refresh(ctx context.Context, m *serverModel) (bool, error) {
	servers, err := s.client.ListServers(ctx)
	if err != nil {
		return false, err
	}
	for _, remote := range servers {
		if remote.ID == m.ID.ValueString() {
			if remote.Name == nil || remote.AutoStart == nil {
				return false, fmt.Errorf("server response is missing server_name or auto_start")
			}
			m.Name = types.StringValue(*remote.Name)
			m.AutoStart = types.BoolValue(*remote.AutoStart)
			// Download inputs are not returned by the Server schema. Preserve state.
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
	found, err := s.refresh(ctx, &m)
	if err != nil {
		r.Diagnostics.AddError("Unable to read Minecraft server", err.Error())
		return
	}
	if !found {
		r.State.RemoveResource(ctx)
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
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
	if err := s.client.UpdateServer(ctx, m.ID.ValueString(), client.UpdateServerRequest{Name: m.Name.ValueString()}); err != nil {
		r.Diagnostics.AddError("Unable to update Minecraft server", err.Error())
		return
	}
	found, err := s.refresh(ctx, &m)
	if err != nil {
		r.Diagnostics.AddError("Unable to read updated server", err.Error())
		return
	}
	if !found {
		r.Diagnostics.AddError("Updated server not visible", "Crafty no longer lists the server. Run terraform plan to reconcile state.")
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, r *resource.DeleteResponse) {
	var m serverModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	err := s.client.DeleteServer(ctx, m.ID.ValueString())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		r.Diagnostics.AddError("Unable to delete Minecraft server", err.Error())
	}
}

var _ resource.ResourceWithValidateConfig = (*serverResource)(nil)

func (s *serverResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, r *resource.ValidateConfigResponse) {
	var m serverModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	for _, v := range []types.String{m.Name, m.Engine, m.Version, m.Host} {
		if !v.IsNull() && !v.IsUnknown() && v.ValueString() == "" {
			r.Diagnostics.AddError("Invalid server configuration", "Name, engine, version, and host must not be empty.")
		}
	}
	if !m.Port.IsNull() && !m.Port.IsUnknown() && (m.Port.ValueInt64() < 1 || m.Port.ValueInt64() > 65535) {
		r.Diagnostics.AddError("Invalid server port", "Port must be between 1 and 65535.")
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
