package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (s *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, r *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replaceInt := []planmodifier.Int64{int64planmodifier.RequiresReplace()}
	replaceMemory := []planmodifier.Float64{float64planmodifier.RequiresReplaceIf(func(ctx context.Context, req planmodifier.Float64Request, resp *float64planmodifier.RequiresReplaceIfFuncResponse) {
		var managed types.Bool
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("manage_memory"), &managed)...)
		resp.RequiresReplace = !managed.ValueBool()
	}, "Memory changes replace the server unless manage_memory is enabled.", "Memory changes replace the server unless `manage_memory` is enabled.")}
	r.Schema = schema.Schema{
		Description: "A Minecraft Java server downloaded by Crafty or installed from a ZIP server pack. Name, automatic start, monitoring settings, and execution command support in-place updates; installation inputs require replacement.",
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.BlockAll(ctx),
		},
		Attributes: map[string]schema.Attribute{
			"id":                    schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, Description: "Crafty server ID."},
			"name":                  schema.StringAttribute{Required: true, Description: "Server name; at least two characters, excluding slashes, backslashes, and #. POST name and PATCH server_name."},
			"engine":                schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Download engine identifier, such as paper. Required with version unless archive_path is set. Changes replace the server."},
			"version":               schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Minecraft download version. Required with engine unless archive_path is set. Changes replace the server."},
			"archive_path":          schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Local ZIP server pack available to the machine running Terraform. Mutually exclusive with engine and version. Changes replace the server."},
			"archive_sha256":        schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Expected SHA-256 of archive_path, checked before upload. Required for ZIP installation. Changes replace the server."},
			"archive_internal_path": schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Relative directory inside the ZIP containing the server files; omit for the archive root. Changes replace the server."},
			"executable":            schema.StringAttribute{Optional: true, PlanModifiers: replaceString, Description: "Path to the server executable relative to archive_internal_path. Required for ZIP installation. Changes replace the server."},
			"mem_min":               schema.Float64Attribute{Required: true, PlanModifiers: replaceMemory, Description: "Minimum Java memory in Crafty units (1000 JVM MiB per unit). Creation-only unless manage_memory is enabled. Managed values may be fractional in whole-MiB increments."},
			"mem_max":               schema.Float64Attribute{Required: true, PlanModifiers: replaceMemory, Description: "Maximum Java memory in Crafty units (1000 JVM MiB per unit). Creation-only unless manage_memory is enabled. Managed values may be fractional in whole-MiB increments."},
			"manage_memory":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Manage mem_min and mem_max through inline JVM -Xms/-Xmx flags without replacement. Default false preserves creation-only behavior. Reads visible flags for drift; does not inspect argument files or restart Minecraft. Explicit execution_command must contain matching heap flags."},
			"host":                  schema.StringAttribute{Required: true, PlanModifiers: replaceString, Description: "Monitoring host reachable from Crafty. Changes replace the server."},
			"port":                  schema.Int64Attribute{Required: true, PlanModifiers: replaceInt, Description: "Monitoring port and generated server.properties port. An imported server.properties file retains its own port. Changes replace the server."},
			"auto_start":            schema.BoolAttribute{Optional: true, Computed: true, Description: "Automatic start setting. Omit to observe Crafty; explicit true or false is managed through PATCH."},
			"monitoring_host":       schema.StringAttribute{Optional: true, Computed: true, Description: "Current monitoring address. Defaults to the host creation input; PATCH updates monitoring only."},
			"monitoring_port":       schema.Int64Attribute{Optional: true, Computed: true, Description: "Current monitoring port, 1–65535. Defaults to the port creation input; PATCH does not change server.properties."},
			"execution_command":     schema.StringAttribute{Optional: true, Computed: true, Description: "Current launch command. Omit to observe Crafty's generated command; explicit values override it through PATCH, including its JVM memory flags. Does not restart the server."},
		},
	}
}
