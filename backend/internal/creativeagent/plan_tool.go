package creativeagent

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// updatePlanTool lets the model show the photographer its plan. It is a
// process tool: its call is hidden and its effect is the run's one plan block.
const updatePlanTool = "update_plan"

// parsePlan is the whole contract of update_plan. The arguments are the
// complete plan block payload, never a diff, so a retried call cannot apply
// twice.
func parsePlan(raw string) (planPayload, error) {
	var args planPayload
	if err := strictArguments(raw, &args); err != nil {
		return planPayload{}, err
	}
	if err := validPlan(args.Steps); err != nil {
		return planPayload{}, err
	}
	return args, nil
}

type planTool struct{}

var _ tool.InvokableTool = planTool{}

func (planTool) Info(context.Context) (*schema.ToolInfo, error) {
	step := map[string]*schema.ParameterInfo{
		"key":   {Type: schema.String, Required: true, Desc: "本步骤稳定标识，更新时保持不变"},
		"title": {Type: schema.String, Required: true, Desc: "给创作者看的一句话步骤名"},
		"state": {Type: schema.String, Required: true, Desc: "步骤状态", Enum: []string{PlanTodo, PlanDoing, PlanDone, PlanSkipped}},
	}
	return &schema.ToolInfo{
		Name: updatePlanTool,
		Desc: "多步任务开始时给出计划，进展时用完整列表更新每步状态。只用于展示进度，不执行任何操作。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"steps": {Type: schema.Array, Required: true, Desc: "完整计划，1 到 20 步", ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: step}},
		}),
	}, nil
}

// InvokableRun only validates; the plan block is written in the transaction
// that records the step's result, so the two cannot disagree.
func (planTool) InvokableRun(_ context.Context, raw string, _ ...tool.Option) (string, error) {
	if _, err := parsePlan(raw); err != nil {
		return "", err
	}
	return `{"success":true}`, nil
}
