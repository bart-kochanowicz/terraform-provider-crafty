package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

var _ resource.Resource = (*backupConfigResource)(nil)
var _ resource.ResourceWithConfigure = (*backupConfigResource)(nil)
var _ resource.ResourceWithImportState = (*backupConfigResource)(nil)
var _ resource.ResourceWithValidateConfig = (*backupConfigResource)(nil)

type backupConfigResource struct{ client *client.Client }
type backupConfigModel struct {
	ID           types.String `tfsdk:"id"`
	ServerID     types.String `tfsdk:"server_id"`
	BackupID     types.String `tfsdk:"backup_id"`
	Name         types.String `tfsdk:"name"`
	Location     types.String `tfsdk:"backup_location"`
	MaxBackups   types.Int64  `tfsdk:"max_backups"`
	Compress     types.Bool   `tfsdk:"compress"`
	Shutdown     types.Bool   `tfsdk:"shutdown"`
	Before       types.String `tfsdk:"before"`
	After        types.String `tfsdk:"after"`
	ExcludedDirs types.Set    `tfsdk:"excluded_dirs"`
}

// NewBackupConfigResource manages an existing policy without owning its deletion.
func NewBackupConfigResource() resource.Resource { return &backupConfigResource{} }
func (s *backupConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, r *resource.MetadataResponse) {
	r.TypeName = req.ProviderTypeName + "_backup_config"
}
func (s *backupConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	r.Schema = schema.Schema{Description: "Manage an existing Crafty backup policy by ID. Only configured settings are updated. Destroy leaves the policy, settings and archives intact. Requires server BACKUP permission.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, Description: "Managed backup policy ID."},
		"server_id":       schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "Crafty server ID."},
		"backup_id":       schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "ID of the existing backup policy to manage."},
		"name":            schema.StringAttribute{Optional: true, Computed: true, Description: "Backup policy name, at least three characters."},
		"backup_location": schema.StringAttribute{Optional: true, Computed: true, Description: "Backup directory on the Crafty host, using forward slashes. Changing it does not move archives. Requires a superuser."},
		"max_backups":     schema.Int64Attribute{Optional: true, Computed: true, Description: "Maximum retained backups; zero disables automatic pruning."},
		"compress":        schema.BoolAttribute{Optional: true, Computed: true, Description: "Whether to compress backups."},
		"shutdown":        schema.BoolAttribute{Optional: true, Computed: true, Description: "Whether Crafty stops the server while taking a backup."},
		"before":          schema.StringAttribute{Optional: true, Computed: true, Description: "Server command sent before a backup; empty disables it."},
		"after":           schema.StringAttribute{Optional: true, Computed: true, Description: "Server command sent after a backup; empty disables it."},
		"excluded_dirs":   schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Description: "Directory exclusions interpreted by Crafty; entries cannot contain commas."},
	}}
}
func (s *backupConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, r *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	api, ok := req.ProviderData.(*client.Client)
	if !ok {
		r.Diagnostics.AddError("Invalid provider client", "Expected a Crafty API client.")
		return
	}
	s.client = api
}

var backupConfigID = regexp.MustCompile(`^[a-z0-9-]+$`)

func (s *backupConfigResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, r *resource.ValidateConfigResponse) {
	var m backupConfigModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	for key, value := range map[string]types.String{"server_id": m.ServerID, "backup_id": m.BackupID} {
		if !value.IsUnknown() && !backupConfigID.MatchString(value.ValueString()) {
			r.Diagnostics.AddAttributeError(path.Root(key), "Invalid Crafty ID", "Use an ID containing lowercase letters, digits and hyphens.")
		}
	}
	if !m.Name.IsNull() && !m.Name.IsUnknown() && len([]rune(m.Name.ValueString())) < 3 {
		r.Diagnostics.AddAttributeError(path.Root("name"), "Invalid backup name", "Use at least three characters.")
	}
	if !m.Location.IsNull() && !m.Location.IsUnknown() && (strings.TrimSpace(m.Location.ValueString()) == "" || strings.Contains(m.Location.ValueString(), `\`)) {
		r.Diagnostics.AddAttributeError(path.Root("backup_location"), "Invalid backup location", "Provide a nonempty Crafty host path using forward slashes.")
	}
	if !m.MaxBackups.IsNull() && !m.MaxBackups.IsUnknown() && m.MaxBackups.ValueInt64() < 0 {
		r.Diagnostics.AddAttributeError(path.Root("max_backups"), "Invalid backup retention", "Use zero or a positive integer.")
	}
	for _, value := range m.ExcludedDirs.Elements() {
		v := value.(types.String)
		if !v.IsUnknown() && (v.IsNull() || strings.TrimSpace(v.ValueString()) == "" || strings.Contains(v.ValueString(), ",")) {
			r.Diagnostics.AddAttributeError(path.Root("excluded_dirs"), "Invalid directory exclusion", "Use nonempty entries without commas.")
		}
	}
}
func (s *backupConfigResource) observe(ctx context.Context, m backupConfigModel) (backupConfigModel, error) {
	configs, err := s.client.ListBackupConfigs(ctx, m.ServerID.ValueString())
	if err != nil {
		return m, err
	}
	w, ok := configs[m.BackupID.ValueString()]
	if !ok {
		return m, fmt.Errorf("backup policy is unavailable; check its ID and permissions")
	}
	if w.ID != m.BackupID.ValueString() || w.ServerID != m.ServerID.ValueString() {
		return m, fmt.Errorf("backup policy response does not match the requested server and policy")
	}
	if w.Name == nil || w.Location == nil || w.MaxBackups == nil || w.Compress == nil || w.Shutdown == nil || w.Before == nil || w.After == nil || len(w.ExcludedDirs) == 0 {
		return m, fmt.Errorf("required backup policy configuration fields are missing")
	}
	var excluded string
	if err := json.Unmarshal(w.ExcludedDirs, &excluded); err != nil {
		return m, fmt.Errorf("invalid backup directory exclusions")
	}
	dirs := []string{}
	if excluded != "" {
		dirs = strings.Split(excluded, ",")
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, dirs)
	if diags.HasError() {
		return m, fmt.Errorf("invalid backup directory exclusions")
	}
	m.ID = types.StringValue(w.ID)
	m.Name, m.Location, m.Before, m.After = types.StringValue(*w.Name), types.StringValue(*w.Location), types.StringValue(*w.Before), types.StringValue(*w.After)
	m.MaxBackups, m.Compress, m.Shutdown, m.ExcludedDirs = types.Int64Value(*w.MaxBackups), types.BoolValue(*w.Compress), types.BoolValue(*w.Shutdown), set
	return m, nil
}
func (m backupConfigModel) patch(plan backupConfigModel) client.BackupConfigPatch {
	patch := client.BackupConfigPatch{}
	if !m.Name.IsNull() {
		v := plan.Name.ValueString()
		patch.Name = &v
	}
	if !m.Location.IsNull() {
		v := plan.Location.ValueString()
		patch.Location = &v
	}
	if !m.MaxBackups.IsNull() {
		v := plan.MaxBackups.ValueInt64()
		patch.MaxBackups = &v
	}
	if !m.Compress.IsNull() {
		v := plan.Compress.ValueBool()
		patch.Compress = &v
	}
	if !m.Shutdown.IsNull() {
		v := plan.Shutdown.ValueBool()
		patch.Shutdown = &v
	}
	if !m.Before.IsNull() {
		v := plan.Before.ValueString()
		patch.Before = &v
	}
	if !m.After.IsNull() {
		v := plan.After.ValueString()
		patch.After = &v
	}
	if !m.ExcludedDirs.IsNull() {
		dirs := make([]string, 0, len(plan.ExcludedDirs.Elements()))
		for _, value := range plan.ExcludedDirs.Elements() {
			dirs = append(dirs, value.(types.String).ValueString())
		}
		patch.ExcludedDirs = &dirs
	}
	return patch
}
func (m *backupConfigModel) adoptUnknown(observed backupConfigModel) {
	if m.Name.IsUnknown() || m.Name.IsNull() {
		m.Name = observed.Name
	}
	if m.Location.IsUnknown() || m.Location.IsNull() {
		m.Location = observed.Location
	}
	if m.MaxBackups.IsUnknown() || m.MaxBackups.IsNull() {
		m.MaxBackups = observed.MaxBackups
	}
	if m.Compress.IsUnknown() || m.Compress.IsNull() {
		m.Compress = observed.Compress
	}
	if m.Shutdown.IsUnknown() || m.Shutdown.IsNull() {
		m.Shutdown = observed.Shutdown
	}
	if m.Before.IsUnknown() || m.Before.IsNull() {
		m.Before = observed.Before
	}
	if m.After.IsUnknown() || m.After.IsNull() {
		m.After = observed.After
	}
	if m.ExcludedDirs.IsUnknown() || m.ExcludedDirs.IsNull() {
		m.ExcludedDirs = observed.ExcludedDirs
	}
}
func (s *backupConfigResource) apply(ctx context.Context, plan, config backupConfigModel) (backupConfigModel, error) {
	observed, err := s.observe(ctx, plan)
	if err != nil {
		return plan, err
	}
	patch := config.patch(plan)
	plan.ID = observed.ID
	plan.adoptUnknown(observed)
	if patch.Name != nil || patch.Location != nil || patch.MaxBackups != nil || patch.Compress != nil || patch.Shutdown != nil || patch.Before != nil || patch.After != nil || patch.ExcludedDirs != nil {
		if err := s.client.UpdateBackupConfig(ctx, plan.ServerID.ValueString(), plan.BackupID.ValueString(), patch); err != nil {
			return plan, err
		}
	}
	return plan, nil
}
func (s *backupConfigResource) Create(ctx context.Context, req resource.CreateRequest, r *resource.CreateResponse) {
	var plan, config backupConfigModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	r.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if r.Diagnostics.HasError() {
		return
	}
	m, err := s.apply(ctx, plan, config)
	if err != nil {
		r.Diagnostics.AddError("Unable to manage Crafty backup policy", err.Error())
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *backupConfigResource) Read(ctx context.Context, req resource.ReadRequest, r *resource.ReadResponse) {
	var m backupConfigModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	observed, err := s.observe(ctx, m)
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty backup policy", err.Error())
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &observed)...)
}
func (s *backupConfigResource) Update(ctx context.Context, req resource.UpdateRequest, r *resource.UpdateResponse) {
	var plan, config backupConfigModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	r.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if r.Diagnostics.HasError() {
		return
	}
	m, err := s.apply(ctx, plan, config)
	if err != nil {
		r.Diagnostics.AddError("Unable to update Crafty backup policy", err.Error())
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *backupConfigResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
func (s *backupConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, r *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || !backupConfigID.MatchString(parts[0]) || !backupConfigID.MatchString(parts[1]) {
		r.Diagnostics.AddError("Invalid backup policy import ID", "Use SERVER_ID/BACKUP_ID.")
		return
	}
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("backup_id"), parts[1])...)
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
