// Package provider implements the Crafty Terraform provider.
package provider

import (
	"context"
	"net/url"
	"strings"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*craftyProvider)(nil)

type craftyProvider struct{ version string }
type providerModel struct {
	URL   types.String `tfsdk:"url"`
	Token types.String `tfsdk:"token"`
}

func (p *craftyProvider) Metadata(_ context.Context, _ provider.MetadataRequest, r *provider.MetadataResponse) {
	r.TypeName = "crafty"
	r.Version = p.version
}
func (p *craftyProvider) Schema(_ context.Context, _ provider.SchemaRequest, r *provider.SchemaResponse) {
	r.Schema = schema.Schema{Attributes: map[string]schema.Attribute{
		"url":   schema.StringAttribute{Required: true, Description: "Crafty panel base URL, without /api/v2."},
		"token": schema.StringAttribute{Required: true, Sensitive: true, Description: "Crafty API bearer token."},
	}}
}
func (p *craftyProvider) Configure(ctx context.Context, req provider.ConfigureRequest, r *provider.ConfigureResponse) {
	var m providerModel
	r.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if r.Diagnostics.HasError() {
		return
	}
	if m.URL.IsUnknown() || m.Token.IsUnknown() {
		r.Diagnostics.AddError("Unknown provider configuration", "The URL and token must be known before configuring the provider.")
		return
	}
	u, err := url.Parse(m.URL.ValueString())
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		r.Diagnostics.AddError("Invalid Crafty URL", "Provide an absolute HTTP or HTTPS base URL without credentials, query parameters, or a fragment.")
		return
	}
	if strings.TrimSpace(m.Token.ValueString()) == "" {
		r.Diagnostics.AddError("Invalid Crafty token", "The API token must not be empty.")
		return
	}
	r.ResourceData = client.New(u.String(), m.Token.ValueString())
}
func (p *craftyProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewMinecraftServerResource}
}
func (p *craftyProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// New returns a provider factory for the Terraform plugin server.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &craftyProvider{version: version} }
}
