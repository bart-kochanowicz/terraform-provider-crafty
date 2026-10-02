package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

func (s *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	r.Schema = schema.Schema{
		Description: "A Minecraft Java server downloaded by Crafty. Only name supports in-place updates in the supplied API specification.",
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.BlockAll(ctx),
		},
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, Description: "Crafty server ID."},
			"name":       schema.StringAttribute{Required: true, Description: "Server name; POST name and PATCH server_name."},
			"engine":     schema.StringAttribute{Required: true, PlanModifiers: replaceString, Description: "Download engine identifier, such as paper. Changes replace the server."},
			"version":    schema.StringAttribute{Required: true, PlanModifiers: replaceString, Description: "Minecraft download version. Changes replace the server."},
			"mem_min":    schema.Int64Attribute{Required: true, PlanModifiers: replaceInt, Description: "Minimum Java memory in GiB. Changes replace the server."},
			"mem_max":    schema.Int64Attribute{Required: true, PlanModifiers: replaceInt, Description: "Maximum Java memory in GiB. Changes replace the server."},
			"host":       schema.StringAttribute{Required: true, PlanModifiers: replaceString, Description: "Monitoring host reachable from Crafty. Changes replace the server."},
			"port":       schema.Int64Attribute{Required: true, PlanModifiers: replaceInt, Description: "Monitoring and server.properties port. Changes replace the server."},
			"auto_start": schema.BoolAttribute{Computed: true, Description: "Automatic start setting returned by Crafty; read-only."},
		},
	}
}
