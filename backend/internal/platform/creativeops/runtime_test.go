package creativeops_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops/einoadapter"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func queryBinding(calls *int) creativeops.Binding {
	return creativeops.Binding{
		Definition:  creativeops.Definition{Key: "read_test", Category: "canvas", Description: "读取测试", SchemaVersion: 1, Kind: "query", RequiredCapability: "creative_read", InputSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string","minLength":1}},"additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}},"additionalProperties":false}`)},
		Entrypoints: []creativeops.Entrypoint{creativeops.HTTP, creativeops.Agent},
		Handle: func(_ context.Context, _ store.AccountScope, c creativeops.Command) (creativeops.ToolResult, error) {
			*calls++
			return creativeops.ToolResult{Data: c.Payload}, nil
		},
	}
}
func TestToolRuntimeRejectsInputBeforeHandlerAndSharesAdapters(t *testing.T) {
	f := setup(t)
	calls := 0
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{queryBinding(&calls)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	http := runtime.ForHTTP(f.a)
	for _, raw := range []string{`{}`, `{"id":2}`, `{"id":""}`, `{"id":"x","account_id":"creative-b"}`, `{"id":"x","id":"y"}`} {
		if _, err = http.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(raw)}); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached handler")
	}
	agent, err := runtime.ForAgent(f.a, []creativeops.ToolRef{{Key: "read_test", Version: 1}}, func(context.Context, string, json.RawMessage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	tool, err := einoadapter.Bind(agent, "read_test", 1)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"one"}`
	got, err := http.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(raw)})
	if err != nil {
		t.Fatal(err)
	}
	agentResult, err := tool.InvokableRun(t.Context(), raw)
	if err != nil || string(got.Response()) != agentResult {
		t.Fatalf("http=%s agent=%s err=%v", got.Response(), agentResult, err)
	}
}

func TestToolRuntimeRejectsIncompleteOrInvalidOutput(t *testing.T) {
	f := setup(t)
	for _, raw := range []string{``, `{"id":`, `{"id":"one"} {"id":"two"}`, `{"id":2}`, `null`} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			binding := queryBinding(&calls)
			binding.Handle = func(context.Context, store.AccountScope, creativeops.Command) (creativeops.ToolResult, error) {
				return creativeops.ToolResult{Data: json.RawMessage(raw)}, nil
			}
			runtime, err := creativeops.NewRuntime([]creativeops.Binding{binding}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"one"}`)}); !errors.Is(err, creativeops.ErrToolResult) {
				t.Fatalf("invalid output accepted: %v", err)
			}
		})
	}
}

func TestToolRuntimeExposureAuthorityAndImmutablePolicy(t *testing.T) {
	f := setup(t)
	calls := 0
	b := queryBinding(&calls)
	if _, err := creativeops.NewRuntime([]creativeops.Binding{{Definition: b.Definition}}, nil); err == nil {
		t.Fatal("unbound tool accepted")
	}
	policy := map[string]creativeops.ToolPolicy{"read_test": {DisabledReason: "maintenance"}}
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{b}, policy)
	if err != nil {
		t.Fatal(err)
	}
	delete(policy, "read_test")
	if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"one"}`)}); !errors.Is(err, creativeops.ErrToolUnavailable) {
		t.Fatal(err)
	}
	runtime, err = creativeops.NewRuntime([]creativeops.Binding{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.ForAgent(f.a, nil, nil); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("agent without authority")
	}
	agent, err := runtime.ForAgent(f.a, []creativeops.ToolRef{{Key: "read_test", Version: 1}}, func(context.Context, string, json.RawMessage) error { return store.ErrCreativeAccessDenied })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agent.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"one"}`)}); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatal(err)
	}
	if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), "read_test", 2, creativeops.Command{Payload: json.RawMessage(`{"id":"one"}`)}); !errors.Is(err, creativeops.ErrToolVersion) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("guard bypassed")
	}
	b.Entrypoints = []creativeops.Entrypoint{creativeops.Agent}
	runtime, err = creativeops.NewRuntime([]creativeops.Binding{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"one"}`)}); !errors.Is(err, creativeops.ErrToolUnavailable) {
		t.Fatal(err)
	}
}
func TestToolCommandReplayUsesDomainReceipt(t *testing.T) {
	f := setup(t)
	op := operation()
	b := queryBinding(new(int))
	b.Definition.Key = op.Key
	b.Definition.Kind = "command"
	b.Definition.RequiredCapability = "manual_write"
	b.Entrypoints = []creativeops.Entrypoint{creativeops.HTTP}
	b.Definition.InputSchema = json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string"}},"required":["marker"],"additionalProperties":false}`)
	b.Definition.OutputSchema = json.RawMessage(`{"type":"object","properties":{"saved":{"type":"boolean"}},"required":["saved"]}`)
	b.Handle = func(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.ToolResult, error) {
		r, err := (creativeops.Executor{}).Run(ctx, scope, op, c)
		return creativeops.ToolResult{Receipt: &r}, err
	}
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{b}, nil, creativeops.WithObserver(func(context.Context, creativeops.ToolObservation) { panic("telemetry unavailable") }))
	if err != nil {
		t.Fatal(err)
	}
	cmd := command("effect")
	for range 2 {
		if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), op.Key, 1, cmd); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, f, "creative_test_effects") != 1 {
		t.Fatal("runtime duplicated effect")
	}
	cmd.Payload = json.RawMessage(`{"marker":"changed"}`)
	if _, err = runtime.ForHTTP(f.a).Invoke(t.Context(), op.Key, 1, cmd); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatal(err)
	}
}

func TestToolSessionCannotBeReboundToWeakerAuthority(t *testing.T) {
	f := setup(t)
	calls := 0
	b := queryBinding(&calls)
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = einoadapter.Bind(runtime.ForHTTP(f.a), "read_test", 1); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("HTTP authority used for Agent")
	}
	b.Definition.Kind = "command"
	b.Definition.RequiredCapability = "manual_write"
	if _, err = creativeops.NewRuntime([]creativeops.Binding{b}, nil); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("Agent writes enabled without transactional Harness authority")
	}
	b = queryBinding(&calls)
	b.Definition.MayCharge = true
	if _, err = creativeops.NewRuntime([]creativeops.Binding{b}, nil); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("charging query has no command identity")
	}
}
func TestToolDiscoveryAndInvocationUseSamePolicy(t *testing.T) {
	f := setup(t)
	calls := 0
	b := queryBinding(&calls)
	policy := map[string]creativeops.ToolPolicy{"read_test": {MaxInputBytes: 12}}
	runtime, err := creativeops.NewRuntime([]creativeops.Binding{b}, policy)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []creativeops.ToolRef{{Key: "read_test", Version: 1}}
	agent, err := runtime.ForAgent(f.a, allowed, func(context.Context, string, json.RawMessage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	allowed[0].Key = "tampered"
	views, err := agent.List(t.Context())
	if err != nil || len(views) != 1 || !views[0].Available {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	views[0].Definition.InputSchema[0] = 'x'
	if _, err = agent.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"this is too long"}`)}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("input policy bypassed")
	}
	if _, err = agent.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"a"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='creative-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err = agent.List(t.Context()); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatal(err)
	}
	if _, err = agent.Invoke(t.Context(), "read_test", 1, creativeops.Command{Payload: json.RawMessage(`{"id":"a"}`)}); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("revoked tool invoked")
	}
}
