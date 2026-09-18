package creativecanvas

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	ReadCanvasTool        = "read_canvas"
	ReadNodeVersionsTool  = "read_node_versions"
	ReadNodeExecutionTool = "read_node_execution"
)

// NewToolRuntime binds existing application queries to one versioned registry.
// The Agent edge must supply its own scope/egress guard before exposing these
// reads to a model. This registry does not add tools to a running Harness.
func NewToolRuntime(policy map[string]creativeops.ToolPolicy, options ...creativeops.RuntimeOption) (*creativeops.Runtime, error) {
	definitions := []struct {
		key, description, input, output string
		handle                          creativeops.ToolHandler
	}{
		{ReadCanvasTool, "读取画布快照", readCanvasInputSchema, readCanvasOutputSchema, readCanvasTool},
		{ReadNodeVersionsTool, "读取节点作品版本", readNodeVersionsInputSchema, readNodeVersionsOutputSchema, readVersionsTool},
		{ReadNodeExecutionTool, "读取节点执行状态", readNodeExecutionInputSchema, readNodeExecutionOutputSchema, readExecutionTool},
	}
	bindings := make([]creativeops.Binding, 0, len(definitions))
	for _, d := range definitions {
		bindings = append(bindings, creativeops.Binding{Definition: creativeops.Definition{Key: d.key, Category: "canvas", Description: d.description, SchemaVersion: 1, Kind: "query", RequiredCapability: "creative_read", InputSchema: json.RawMessage(d.input), OutputSchema: json.RawMessage(d.output)}, Entrypoints: []creativeops.Entrypoint{creativeops.HTTP, creativeops.Agent}, Handle: d.handle})
	}
	return creativeops.NewRuntime(bindings, policy, options...)
}
func toolData(value any, err error) (creativeops.ToolResult, error) {
	if err != nil {
		return creativeops.ToolResult{}, err
	}
	raw, err := json.Marshal(value)
	return creativeops.ToolResult{Data: raw}, err
}
func readCanvasTool(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.ToolResult, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := creativeops.Decode(command.Payload, &input); err != nil {
		return creativeops.ToolResult{}, err
	}
	return toolData(GetCanvas(ctx, scope, input.ID))
}
func readVersionsTool(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.ToolResult, error) {
	var input struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
	}
	if err := creativeops.Decode(command.Payload, &input); err != nil {
		return creativeops.ToolResult{}, err
	}
	return toolData(ListNodeVersions(ctx, scope, input.ID, input.NodeID))
}
func readExecutionTool(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.ToolResult, error) {
	var input struct {
		ID          string `json:"id"`
		ExecutionID string `json:"execution_id"`
	}
	if err := creativeops.Decode(command.Payload, &input); err != nil {
		return creativeops.ToolResult{}, err
	}
	return toolData(GetNodeExecution(ctx, scope, ExecutionTarget{CanvasID: input.ID, ExecutionID: input.ExecutionID}))
}
