package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

var _ resource.Resource = (*scheduleResource)(nil)
var _ resource.ResourceWithConfigure = (*scheduleResource)(nil)
var _ resource.ResourceWithImportState = (*scheduleResource)(nil)
var _ resource.ResourceWithValidateConfig = (*scheduleResource)(nil)

type scheduleResource struct{ client *client.Client }
type scheduleModel struct {
	ID           types.String `tfsdk:"id"`
	ServerID     types.String `tfsdk:"server_id"`
	Name         types.String `tfsdk:"name"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	Action       types.String `tfsdk:"action"`
	Command      types.String `tfsdk:"command"`
	Interval     types.Int64  `tfsdk:"interval"`
	IntervalType types.String `tfsdk:"interval_type"`
	StartTime    types.String `tfsdk:"start_time"`
	Cron         types.String `tfsdk:"cron_string"`
}

// NewScheduleResource manages independent recurring server tasks.
func NewScheduleResource() resource.Resource { return &scheduleResource{} }
func (s *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, r *resource.MetadataResponse) {
	r.TypeName = req.ProviderTypeName + "_schedule"
}
func (s *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	r.Schema = schema.Schema{Description: "Manage an independent recurring Crafty task. Requires server SCHEDULE permission. New tasks must be enabled; imported or existing tasks can be disabled.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, Description: "Crafty schedule ID."},
		"server_id":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "Crafty server ID."},
		"name":          schema.StringAttribute{Required: true, Description: "Schedule display name."},
		"command":       schema.StringAttribute{Required: true, Description: "Command queued by Crafty, such as a console command or restart_server."},
		"action":        schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("command"), Description: "Crafty action label. Defaults to command; command determines the queued operation."},
		"enabled":       schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Whether the scheduler runs this task. Must be true when creating."},
		"interval":      schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(1), Description: "Positive interval count. Ignored when cron_string is set."},
		"interval_type": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("hours"), Description: "minutes, hours, or days. Ignored when cron_string is set; defaults to hours."},
		"start_time":    schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("00:00"), Description: "HH:MM in Crafty's host timezone, used for days schedules. Defaults to 00:00."},
		"cron_string":   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Five-field APScheduler cron expression in Crafty's host timezone. Overrides interval settings."},
	}}
}
func (s *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, r *resource.ConfigureResponse) {
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

var scheduleServerID = regexp.MustCompile(`^[a-z0-9-]+$`)
var scheduleStartTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s *scheduleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, r *resource.ValidateConfigResponse) {
	var m scheduleModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if !m.ServerID.IsUnknown() && !scheduleServerID.MatchString(m.ServerID.ValueString()) {
		r.Diagnostics.AddAttributeError(path.Root("server_id"), "Invalid server ID", "Use the Crafty server ID containing lowercase letters, digits and hyphens.")
	}
	for key, value := range map[string]types.String{"name": m.Name, "command": m.Command} {
		if !value.IsUnknown() && strings.TrimSpace(value.ValueString()) == "" {
			r.Diagnostics.AddAttributeError(path.Root(key), "Empty schedule setting", "Provide a nonempty value.")
		}
	}
	if !m.Interval.IsUnknown() && !m.Interval.IsNull() && m.Interval.ValueInt64() < 1 {
		r.Diagnostics.AddAttributeError(path.Root("interval"), "Invalid interval", "Use a positive integer.")
	}
	if !m.StartTime.IsUnknown() && !m.StartTime.IsNull() && !scheduleStartTime.MatchString(m.StartTime.ValueString()) {
		r.Diagnostics.AddAttributeError(path.Root("start_time"), "Invalid start time", "Use HH:MM in the range 00:00 through 23:59.")
	}
	if !m.Cron.IsUnknown() && m.Cron.ValueString() != "" {
		if len(strings.Fields(m.Cron.ValueString())) != 5 {
			r.Diagnostics.AddAttributeError(path.Root("cron_string"), "Invalid cron expression", "Use five fields: minute hour day month weekday.")
		}
	} else if !m.Cron.IsUnknown() && !m.IntervalType.IsUnknown() && !m.IntervalType.IsNull() {
		switch m.IntervalType.ValueString() {
		case "minutes", "hours":
		case "days":
			if !m.Interval.IsUnknown() && m.Interval.ValueInt64() > 31 {
				r.Diagnostics.AddAttributeError(path.Root("interval"), "Invalid day interval", "Crafty uses days of the month; use a count between 1 and 31.")
			}
		default:
			r.Diagnostics.AddAttributeError(path.Root("interval_type"), "Invalid interval type", "Use minutes, hours, or days when cron_string is empty.")
		}
	}
}
func (m scheduleModel) settings() client.ScheduleSettings {
	return client.ScheduleSettings{Name: m.Name.ValueString(), Enabled: m.Enabled.ValueBool(), Action: m.Action.ValueString(), Command: m.Command.ValueString(), Interval: m.Interval.ValueInt64(), IntervalType: m.IntervalType.ValueString(), StartTime: m.StartTime.ValueString(), Cron: m.Cron.ValueString()}
}
func (s *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, r *resource.CreateResponse) {
	var m scheduleModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if !m.Enabled.ValueBool() {
		r.Diagnostics.AddError("Cannot create a disabled schedule", "Crafty does not return an ID for disabled task creation. Import an existing disabled task, or create an enabled task and disable it in a later apply.")
		return
	}
	id, err := s.client.CreateSchedule(ctx, m.ServerID.ValueString(), m.settings())
	if err != nil {
		r.Diagnostics.AddError("Unable to create Crafty schedule", err.Error())
		return
	}
	if id <= 0 {
		r.Diagnostics.AddError("Missing schedule ID", "Crafty did not return a valid schedule ID. Inspect the panel before retrying.")
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *scheduleResource) observe(ctx context.Context, m scheduleModel) (client.Schedule, error) {
	w, err := s.client.GetSchedule(ctx, m.ServerID.ValueString(), m.ID.ValueString())
	if err != nil {
		return w, err
	}
	if w.Name == nil || w.Enabled == nil || w.Action == nil || w.Command == nil || w.Interval == nil || w.IntervalType == nil || w.StartTime == nil || w.Cron == nil || w.OneTime == nil || w.Delay == nil {
		return w, fmt.Errorf("required schedule configuration fields are missing")
	}
	if *w.OneTime || w.Parent != nil || *w.Delay != 0 || (w.ActionID != nil && *w.ActionID != "") {
		return w, fmt.Errorf("only independent recurring tasks without backup references are supported")
	}
	return w, nil
}
func (s *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, r *resource.ReadResponse) {
	var m scheduleModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	w, err := s.observe(ctx, m)
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty schedule", err.Error())
		return
	}
	m.Name, m.Enabled, m.Action, m.Command = types.StringValue(*w.Name), types.BoolValue(*w.Enabled), types.StringValue(*w.Action), types.StringValue(*w.Command)
	m.Interval, m.IntervalType, m.StartTime, m.Cron = types.Int64Value(*w.Interval), types.StringValue(*w.IntervalType), types.StringValue(*w.StartTime), types.StringValue(*w.Cron)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, r *resource.UpdateResponse) {
	var m scheduleModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if _, err := s.observe(ctx, m); err != nil {
		r.Diagnostics.AddError("Unable to read Crafty schedule", err.Error())
		return
	}
	if err := s.client.UpdateSchedule(ctx, m.ServerID.ValueString(), m.ID.ValueString(), m.settings()); err != nil {
		r.Diagnostics.AddError("Unable to update Crafty schedule", err.Error())
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, r *resource.DeleteResponse) {
	var m scheduleModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if _, err := s.observe(ctx, m); err != nil {
		r.Diagnostics.AddError("Unable to read Crafty schedule", err.Error())
		return
	}
	if err := s.client.DeleteSchedule(ctx, m.ServerID.ValueString(), m.ID.ValueString()); err != nil {
		r.Diagnostics.AddError("Unable to delete Crafty schedule", err.Error())
	}
}
func (s *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, r *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || !scheduleServerID.MatchString(parts[0]) {
		r.Diagnostics.AddError("Invalid schedule import ID", "Use SERVER_ID/SCHEDULE_ID.")
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[1] {
		r.Diagnostics.AddError("Invalid schedule import ID", "Use SERVER_ID/SCHEDULE_ID with a positive numeric schedule ID.")
		return
	}
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
