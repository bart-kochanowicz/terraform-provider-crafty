package provider

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

var _ resource.Resource = (*webhookResource)(nil)
var _ resource.ResourceWithConfigure = (*webhookResource)(nil)
var _ resource.ResourceWithImportState = (*webhookResource)(nil)
var _ resource.ResourceWithValidateConfig = (*webhookResource)(nil)

type webhookResource struct{ client *client.Client }
type webhookModel struct {
	ID       types.String `tfsdk:"id"`
	ServerID types.String `tfsdk:"server_id"`
	Type     types.String `tfsdk:"webhook_type"`
	Name     types.String `tfsdk:"name"`
	URL      types.String `tfsdk:"url"`
	BotName  types.String `tfsdk:"bot_name"`
	Triggers types.Set    `tfsdk:"triggers"`
	Body     types.String `tfsdk:"body"`
	Color    types.String `tfsdk:"color"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

// NewWebhookResource manages a server's webhook configuration.
func NewWebhookResource() resource.Resource { return &webhookResource{} }

func (s *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, r *resource.MetadataResponse) {
	r.TypeName = req.ProviderTypeName + "_webhook"
}
func (s *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	r.Schema = schema.Schema{
		Description: "Manage a Crafty server webhook. Requires server CONFIG permission. Refresh only reads configuration.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, Description: "Crafty webhook ID."},
			"server_id":    schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, Description: "Crafty server ID."},
			"webhook_type": schema.StringAttribute{Required: true, Description: "Webhook provider supported by Crafty, such as Discord."},
			"name":         schema.StringAttribute{Required: true, Description: "Webhook name (up to 64 characters)."},
			"url":          schema.StringAttribute{Required: true, Sensitive: true, Description: "HTTP or HTTPS callback URL. Stored in Terraform state."},
			"triggers":     schema.SetAttribute{Required: true, ElementType: types.StringType, Description: "Crafty event names, such as start_server or backup_server."},
			"body":         schema.StringAttribute{Required: true, Description: "Message template interpreted by Crafty."},
			"bot_name":     schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("Crafty Controller"), Description: "Sender display name."},
			"color":        schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("#005cd1"), Description: "Message color."},
			"enabled":      schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Whether Crafty sends notifications for matching events."},
		},
	}
}
func (s *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, r *resource.ConfigureResponse) {
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

var webhookServerID = regexp.MustCompile(`^[a-z0-9-]+$`)

func (s *webhookResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, r *resource.ValidateConfigResponse) {
	var m webhookModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if !m.ServerID.IsUnknown() && !webhookServerID.MatchString(m.ServerID.ValueString()) {
		r.Diagnostics.AddAttributeError(path.Root("server_id"), "Invalid server ID", "Use the Crafty server ID containing lowercase letters, digits and hyphens.")
	}
	if !m.URL.IsUnknown() {
		u, err := url.Parse(m.URL.ValueString())
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			r.Diagnostics.AddAttributeError(path.Root("url"), "Invalid webhook URL", "Provide an absolute HTTP or HTTPS URL without user information or a fragment.")
		}
	}
	if !m.Name.IsUnknown() && (len([]rune(m.Name.ValueString())) == 0 || len([]rune(m.Name.ValueString())) > 64) {
		r.Diagnostics.AddAttributeError(path.Root("name"), "Invalid webhook name", "Use between 1 and 64 characters.")
	}
	for _, value := range m.Triggers.Elements() {
		v := value.(types.String)
		if !v.IsUnknown() && (v.IsNull() || strings.TrimSpace(v.ValueString()) == "" || strings.Contains(v.ValueString(), ",")) {
			r.Diagnostics.AddAttributeError(path.Root("triggers"), "Invalid webhook trigger", "Event names must be nonempty strings without commas.")
		}
	}
}

func (m webhookModel) settings() client.WebhookSettings {
	triggers := make([]string, 0, len(m.Triggers.Elements()))
	for _, value := range m.Triggers.Elements() {
		triggers = append(triggers, value.(types.String).ValueString())
	}
	return client.WebhookSettings{Type: m.Type.ValueString(), Name: m.Name.ValueString(), URL: m.URL.ValueString(), BotName: m.BotName.ValueString(), Triggers: triggers, Body: m.Body.ValueString(), Color: m.Color.ValueString(), Enabled: m.Enabled.ValueBool()}
}
func (s *webhookResource) Create(ctx context.Context, req resource.CreateRequest, r *resource.CreateResponse) {
	var m webhookModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	id, err := s.client.CreateWebhook(ctx, m.ServerID.ValueString(), m.settings())
	if err != nil {
		r.Diagnostics.AddError("Unable to create Crafty webhook", err.Error())
		return
	}
	if id <= 0 {
		r.Diagnostics.AddError("Missing webhook ID", "Crafty accepted the request without a valid webhook ID. Inspect the panel before retrying.")
		return
	}
	m.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *webhookResource) Read(ctx context.Context, req resource.ReadRequest, r *resource.ReadResponse) {
	var m webhookModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	webhooks, err := s.client.ListWebhooks(ctx, m.ServerID.ValueString())
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty webhook", err.Error())
		return
	}
	w, ok := webhooks[m.ID.ValueString()]
	if !ok {
		r.State.RemoveResource(ctx)
		return
	}
	if w.Type == nil || w.Name == nil || w.URL == nil || w.BotName == nil || w.Triggers == nil || w.Body == nil || w.Color == nil || w.Enabled == nil {
		r.Diagnostics.AddError("Incomplete webhook response", "Crafty omitted one or more webhook configuration fields.")
		return
	}
	triggers := strings.Split(strings.TrimSuffix(*w.Triggers, ","), ",")
	if *w.Triggers == "" {
		triggers = []string{}
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, triggers)
	r.Diagnostics.Append(diags...)
	if r.Diagnostics.HasError() {
		return
	}
	m.Type, m.Name, m.URL, m.BotName = types.StringValue(*w.Type), types.StringValue(*w.Name), types.StringValue(*w.URL), types.StringValue(*w.BotName)
	m.Body, m.Color, m.Enabled, m.Triggers = types.StringValue(*w.Body), types.StringValue(*w.Color), types.BoolValue(*w.Enabled), set
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, r *resource.UpdateResponse) {
	var m webhookModel
	r.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	webhooks, err := s.client.ListWebhooks(ctx, m.ServerID.ValueString())
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty webhook", err.Error())
		return
	}
	if _, ok := webhooks[m.ID.ValueString()]; !ok {
		r.Diagnostics.AddError("Webhook unavailable", "The webhook does not belong to this server. Refresh state before retrying.")
		return
	}
	if err := s.client.UpdateWebhook(ctx, m.ServerID.ValueString(), m.ID.ValueString(), m.settings()); err != nil {
		r.Diagnostics.AddError("Unable to update Crafty webhook", err.Error())
		return
	}
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
func (s *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, r *resource.DeleteResponse) {
	var m webhookModel
	r.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	webhooks, err := s.client.ListWebhooks(ctx, m.ServerID.ValueString())
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty webhook", err.Error())
		return
	}
	if _, ok := webhooks[m.ID.ValueString()]; !ok {
		return
	}
	if err := s.client.DeleteWebhook(ctx, m.ServerID.ValueString(), m.ID.ValueString()); err != nil {
		r.Diagnostics.AddError("Unable to delete Crafty webhook", err.Error())
		return
	}
	webhooks, err = s.client.ListWebhooks(ctx, m.ServerID.ValueString())
	if err != nil {
		r.Diagnostics.AddError("Unable to confirm webhook deletion", err.Error())
		return
	}
	if _, ok := webhooks[m.ID.ValueString()]; ok {
		r.Diagnostics.AddError("Webhook still exists", "Crafty accepted deletion but still returns the webhook. Retry after checking the panel.")
	}
}
func (s *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, r *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || !webhookServerID.MatchString(parts[0]) {
		r.Diagnostics.AddError("Invalid webhook import ID", "Use SERVER_ID/WEBHOOK_ID.")
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[1] {
		r.Diagnostics.AddError("Invalid webhook import ID", "Use SERVER_ID/WEBHOOK_ID with a positive numeric webhook ID.")
		return
	}
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("server_id"), parts[0])...)
	r.Diagnostics.Append(r.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
