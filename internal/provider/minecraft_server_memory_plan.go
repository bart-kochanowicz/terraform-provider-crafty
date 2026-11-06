package provider

import (
	"context"
	"fmt"
	"math"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func (m serverModel) validateMemory() error {
	for _, value := range []types.Float64{m.MemMin, m.MemMax} {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		if m.ManageMemory.ValueBool() {
			if _, err := memoryMiB(value.ValueFloat64()); err != nil {
				return err
			}
		} else if n := value.ValueFloat64(); math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || math.Trunc(n) != n {
			return fmt.Errorf("creation-only memory inputs must be whole numbers of at least 1; enable manage_memory for runtime heap settings")
		}
	}
	return nil
}

func (m serverModel) validateHeapCommand() error {
	if !m.ManageMemory.ValueBool() || m.ExecutionCommand.IsNull() || m.ExecutionCommand.IsUnknown() || m.MemMin.IsUnknown() || m.MemMax.IsUnknown() || m.MemMin.IsNull() || m.MemMax.IsNull() {
		return nil
	}
	min, max, err := heapMemory(m.ExecutionCommand.ValueString())
	if err != nil {
		return err
	}
	if min == nil || max == nil || *min != m.MemMin.ValueFloat64() || *max != m.MemMax.ValueFloat64() {
		return fmt.Errorf("explicit execution_command must contain inline -Xms/-Xmx matching mem_min/mem_max (1000 MiB per unit); omit execution_command to let the provider edit the observed Java command")
	}
	return nil
}

var _ resource.ResourceWithModifyPlan = (*serverResource)(nil)

func (s *serverResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var planned, config serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || !planned.ManageMemory.ValueBool() || planned.MemMin.IsUnknown() || planned.MemMax.IsUnknown() {
		return
	}
	if !config.ExecutionCommand.IsNull() {
		if err := planned.validateHeapCommand(); err != nil {
			resp.Diagnostics.AddError("Conflicting managed memory command", err.Error())
		}
		return
	}
	if req.State.Raw.IsNull() {
		return // Crafty's generated command is available only after creation.
	}
	var prior serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || prior.ExecutionCommand.IsNull() || prior.ExecutionCommand.IsUnknown() {
		return
	}
	command, err := rewriteHeapMemory(prior.ExecutionCommand.ValueString(), planned.MemMin.ValueFloat64(), planned.MemMax.ValueFloat64())
	if err != nil {
		resp.Diagnostics.AddError("Unable to plan managed Java memory", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("execution_command"), types.StringValue(command))...)
}
