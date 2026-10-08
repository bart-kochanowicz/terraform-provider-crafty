package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

var _ datasource.DataSource = (*serverDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*serverDataSource)(nil)

type serverDataSource struct{ client *client.Client }

type serverDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	AutoStart        types.Bool   `tfsdk:"auto_start"`
	MonitoringHost   types.String `tfsdk:"monitoring_host"`
	MonitoringPort   types.Int64  `tfsdk:"monitoring_port"`
	ExecutionCommand types.String `tfsdk:"execution_command"`
}

// NewServerDataSource reads an existing server without managing its lifecycle.
func NewServerDataSource() datasource.DataSource { return &serverDataSource{} }

func (s *serverDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, r *datasource.MetadataResponse) {
	r.TypeName = req.ProviderTypeName + "_server"
}

func (s *serverDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, r *datasource.SchemaResponse) {
	r.Schema = schema.Schema{
		Description: "Read an existing Crafty server by ID. Uses the servers visible to the configured token and performs no mutations.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Required: true, Description: "Crafty server ID."},
			"name":              schema.StringAttribute{Computed: true, Description: "Current server name."},
			"auto_start":        schema.BoolAttribute{Computed: true, Description: "Current automatic start setting."},
			"monitoring_host":   schema.StringAttribute{Computed: true, Description: "Current monitoring address."},
			"monitoring_port":   schema.Int64Attribute{Computed: true, Description: "Current monitoring port; distinct from server.properties."},
			"execution_command": schema.StringAttribute{Computed: true, Description: "Current launch command."},
		},
	}
}

func (s *serverDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, r *datasource.ConfigureResponse) {
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

func (s *serverDataSource) Read(ctx context.Context, req datasource.ReadRequest, r *datasource.ReadResponse) {
	var m serverDataSourceModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if m.ID.IsUnknown() || strings.TrimSpace(m.ID.ValueString()) == "" {
		r.Diagnostics.AddError("Invalid server ID", "The server ID must be known and must not be blank.")
		return
	}
	servers, err := s.client.ListServers(ctx)
	if err != nil {
		r.Diagnostics.AddError("Unable to read Crafty server", err.Error())
		return
	}
	var found *client.Server
	for i := range servers {
		if servers[i].ID != m.ID.ValueString() {
			continue
		}
		if found != nil {
			r.Diagnostics.AddError("Ambiguous server response", "Crafty returned multiple servers for the requested ID.")
			return
		}
		found = &servers[i]
	}
	if found == nil {
		r.Diagnostics.AddError("Server unavailable", "The server is absent from the token's visible-server collection. Check the ID and permissions; absence does not prove deletion.")
		return
	}
	if found.Name == nil || found.AutoStart == nil || found.MonitoringHost == nil || found.MonitoringPort == nil || found.ExecutionCommand == nil {
		r.Diagnostics.AddError("Incomplete server response", "Crafty omitted one or more fields required to read the server settings.")
		return
	}
	m.Name = types.StringValue(*found.Name)
	m.AutoStart = types.BoolValue(*found.AutoStart)
	m.MonitoringHost = types.StringValue(*found.MonitoringHost)
	m.MonitoringPort = types.Int64Value(*found.MonitoringPort)
	m.ExecutionCommand = types.StringValue(*found.ExecutionCommand)
	r.Diagnostics.Append(r.State.Set(ctx, &m)...)
}
