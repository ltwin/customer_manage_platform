package creativecanvas_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops/einoadapter"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCanvasQueriesUseSameToolContractAcrossEntrypoints(t *testing.T) {
	_, alice, bob := setup(t)
	p := project(t, alice)
	v := asset(t, alice)
	node, _ := add(t, alice, p, v)
	runtime, err := creativecanvas.NewToolRuntime(nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []creativeops.ToolRef{{Key: creativecanvas.ReadCanvasTool, Version: 1}, {Key: creativecanvas.ReadNodeVersionsTool, Version: 1}, {Key: creativecanvas.ReadNodeExecutionTool, Version: 1}}
	agent, err := runtime.ForAgent(alice, allowed, func(context.Context, string, json.RawMessage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key   string
		input any
	}{
		{creativecanvas.ReadCanvasTool, map[string]string{"id": p.CanvasID}},
		{creativecanvas.ReadNodeVersionsTool, map[string]string{"id": p.CanvasID, "node_id": node.NodeID}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			raw, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := runtime.ForHTTP(alice).Invoke(t.Context(), tc.key, 1, creativeops.Command{Payload: raw})
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := einoadapter.Bind(agent, tc.key, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = adapter.Info(t.Context()); err != nil {
				t.Fatal(err)
			}
			text, err := adapter.InvokableRun(t.Context(), string(raw))
			if err != nil || text != string(result.Response()) {
				t.Fatalf("query mismatch: %v", err)
			}
			if _, err = runtime.ForHTTP(bob).Invoke(t.Context(), tc.key, 1, creativeops.Command{Payload: raw}); !errors.Is(err, creativecanvas.ErrNotFound) {
				t.Fatalf("cross account: %v", err)
			}
		})
	}
}

func TestExecutionToolKeepsAcceptedIdentityAndReadsPersistedState(t *testing.T) {
	db, scope, jobs, service, project, nodes := executionFixture(t)
	execution, command := requestExecution(t, scope, jobs, service, project, nodes[2])
	binding := creativeops.Binding{
		Definition:  creativeops.Definition{Key: "request_test_execution", Category: "canvas", Description: "执行测试", SchemaVersion: 1, Kind: "execution", RequiredCapability: "manual_write", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)},
		Entrypoints: []creativeops.Entrypoint{creativeops.HTTP},
		Handle: func(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.ToolResult, error) {
			receipt, err := service.Request(ctx, scope, c, jobs)
			return creativeops.ToolResult{Receipt: &receipt}, err
		},
	}
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{binding}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.ForHTTP(scope).Invoke(t.Context(), binding.Definition.Key, 1, command)
	if err != nil || result.Receipt == nil || result.Receipt.Outcome.HTTPStatus != 202 || *result.Receipt.Outcome.ResultID != execution.ID {
		t.Fatalf("accepted identity: %+v %v", result, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay queued jobs=%d err=%v", count, err)
	}
	runtime, err = creativecanvas.NewToolRuntime(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"id": project.CanvasID, "execution_id": execution.ID})
	result, err = runtime.ForHTTP(scope).Invoke(t.Context(), creativecanvas.ReadNodeExecutionTool, 1, creativeops.Command{Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	var stored creativecanvas.NodeExecution
	if err = json.Unmarshal(result.Response(), &stored); err != nil || stored.ID != execution.ID || stored.State != "queued" {
		t.Fatalf("persisted query=%+v err=%v", stored, err)
	}
}
