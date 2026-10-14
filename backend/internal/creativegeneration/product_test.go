package creativegeneration_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestProductGenerationReplayDoesNotPrepareAgain(t *testing.T) {
	f := setup(t, gw.GenerationText)
	var inputCommand creativecanvas.RequestExecutionInput
	must(t, json.Unmarshal(f.command.Payload, &inputCommand))
	inputCommand.ConfigurationRevision = "1"
	f.command.Payload, _ = json.Marshal(inputCommand)
	var prepares atomic.Int64
	p, err := creativegeneration.NewProduct(f.svc, []creativegeneration.Registration{{Model: creativegeneration.Model{ConfigurationRevision: "1", Key: "test-model", Label: "受控文字模型", Kind: "text"}, PrepareInTx: func(context.Context, store.TxAccountScope, creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
		prepares.Add(1)
		return f.admission, 1, nil
	}}})
	must(t, err)
	catalog, err := p.Catalog(t.Context(), f.scope)
	must(t, err)
	if len(catalog.Models) != 1 || catalog.DisabledReason != "" {
		t.Fatal(catalog)
	}
	// 账号不可用时不列出任何模型。
	_, err = f.db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('g4-pending','test','pending_verification')`)
	must(t, err)
	pending := f.st.ScopeFor(auth.AccountContext{AccountID: "g4-pending"})
	if c, err := p.Catalog(t.Context(), pending); err != nil || len(c.Models) != 0 || c.DisabledReason != "账号当前不可用" {
		t.Fatalf("inactive account catalog: %+v %v", c, err)
	}
	first, err := p.Request(t.Context(), f.scope, f.command)
	must(t, err)
	var e creativecanvas.NodeExecution
	must(t, json.Unmarshal(first.Outcome.Response, &e))
	f.target.ExecutionID = e.ID
	for range 5 {
		f.step(t)
	}
	// 已成功后，配置撤掉且草稿变化，旧操作仍回放原执行。
	_, err = f.db.Exec("UPDATE creative_node_prompt_drafts SET revision=revision+1,text='changed' WHERE account_id='g4'")
	must(t, err)
	empty, err := creativegeneration.NewProduct(nil, nil)
	must(t, err)
	second, err := empty.Request(t.Context(), f.scope, f.command)
	must(t, err)
	var replay creativecanvas.NodeExecution
	must(t, json.Unmarshal(second.Outcome.Response, &replay))
	if replay.ID != e.ID || replay.CreatedAt != e.CreatedAt || prepares.Load() != 1 || f.a.calls.Load() != 1 {
		t.Fatalf("replay prepared=%d submitted=%d", prepares.Load(), f.a.calls.Load())
	}
	changed := f.command
	var input creativecanvas.RequestExecutionInput
	must(t, json.Unmarshal(changed.Payload, &input))
	input.DraftRevision++
	changed.Payload, _ = json.Marshal(input)
	if _, err = empty.Request(t.Context(), f.scope, changed); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatalf("changed intent: %v", err)
	}
}

func TestProductCatalogWithoutModelsExplainsWhy(t *testing.T) {
	f := setup(t, gw.GenerationText)
	p, err := creativegeneration.NewProduct(nil, nil)
	must(t, err)
	c, err := p.Catalog(t.Context(), f.scope)
	must(t, err)
	if len(c.Models) != 0 || c.DisabledReason != "尚未配置生成模型" {
		t.Fatal(c)
	}
	if _, err = p.Request(t.Context(), f.scope, f.command); !errors.Is(err, creativecanvas.ErrActionUnavailable) {
		t.Fatal(err)
	}
	var count int
	must(t, f.db.QueryRow("SELECT count(*) FROM creative_operation_receipts WHERE operation_id=$1", f.command.OperationID).Scan(&count))
	if count != 0 {
		t.Fatal("unavailable request persisted")
	}
}

func TestProductRejectsStaleConfigurationAndRollsBackPreparation(t *testing.T) {
	f := setup(t, gw.GenerationText)
	var in creativecanvas.RequestExecutionInput
	must(t, json.Unmarshal(f.command.Payload, &in))
	in.ConfigurationRevision = "old"
	f.command.Payload, _ = json.Marshal(in)
	var calls atomic.Int64
	injected := errors.New("injected preparation failure")
	p, err := creativegeneration.NewProduct(f.svc, []creativegeneration.Registration{{Model: creativegeneration.Model{ConfigurationRevision: "1", Key: "test-model", Label: "受控文字模型", Kind: "text"}, PrepareInTx: func(ctx context.Context, tx store.TxAccountScope, _ creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
		calls.Add(1)
		_, e := tx.Update(ctx, "creative_node_prompt_drafts", "text='should roll back'", "node_id=$2", f.node)
		if e != nil {
			return gw.GenerationAdmission{}, 0, e
		}
		return gw.GenerationAdmission{}, 0, injected
	}}})
	must(t, err)
	if _, err = p.Request(t.Context(), f.scope, f.command); !errors.Is(err, gw.ErrConflict) || calls.Load() != 0 {
		t.Fatalf("stale configuration: %v calls=%d", err, calls.Load())
	}
	in.ConfigurationRevision = "1"
	f.command.Payload, _ = json.Marshal(in)
	if _, err = p.Request(t.Context(), f.scope, f.command); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	var prompt string
	must(t, f.db.QueryRow("SELECT text FROM creative_node_prompt_drafts WHERE account_id='g4' AND node_id=$1", f.node).Scan(&prompt))
	if prompt != "生成作品" {
		t.Fatal("preparation did not roll back")
	}
	var requests int
	must(t, f.db.QueryRow("SELECT count(*) FROM llm_requests WHERE account_id='g4'").Scan(&requests))
	if requests != 0 || f.a.calls.Load() != 0 {
		t.Fatal("failed preparation admitted request")
	}
}

func TestProductModelKindsAreExact(t *testing.T) {
	f := setup(t, gw.GenerationText)
	for _, kind := range []string{"text|image", "image|video", "TEXT", "3d", ""} {
		_, err := creativegeneration.NewProduct(f.svc, []creativegeneration.Registration{{Model: creativegeneration.Model{ConfigurationRevision: "1", Key: "test-model", Label: "测试", Kind: kind}, PrepareInTx: func(context.Context, store.TxAccountScope, creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
			return f.admission, 1, nil
		}}})
		if !errors.Is(err, gw.ErrValidation) {
			t.Errorf("accepted kind %q: %v", kind, err)
		}
	}
}

// 助手的复核在受理事务里：拒绝时什么都不落库，也就不会派发；放行时照常受理。
func TestAgentGenerationAdmitGuardsAcceptance(t *testing.T) {
	f := setup(t, gw.GenerationText)
	p, err := creativegeneration.NewProduct(f.svc, []creativegeneration.Registration{{Model: creativegeneration.Model{ConfigurationRevision: "1", Key: "test-model", Label: "受控文字模型", Kind: "text"}, PrepareInTx: func(context.Context, store.TxAccountScope, creativecanvas.RequestExecutionInput) (gw.GenerationAdmission, int64, error) {
		return f.admission, 1, nil
	}}})
	must(t, err)
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.scope, f.target.CanvasID)
	must(t, err)
	in := creativegeneration.AgentGeneration{Seed: "run/step-stopped", CanvasID: f.target.CanvasID, NodeID: f.node, DraftRevision: int64(canvas.Nodes[0].Prompt.Revision)}
	stopped := errors.New("run stopped")
	in.Admit = func(context.Context, store.TxAccountScope) error { return stopped }
	if _, err = p.GenerateNode(t.Context(), f.scope, in); !errors.Is(err, stopped) {
		t.Fatalf("refused admission: %v", err)
	}
	for _, table := range []string{"llm_requests", "creative_node_executions", "creative_generation_bindings"} {
		if count(t, f.db, "SELECT count(*) FROM "+table) != 0 {
			t.Fatal("refused generation recorded", table)
		}
	}
	var admitted atomic.Int64
	in.Seed = "run/step-running"
	in.Admit = func(context.Context, store.TxAccountScope) error { admitted.Add(1); return nil }
	result, err := p.GenerateNode(t.Context(), f.scope, in)
	must(t, err)
	if admitted.Load() != 1 || result.State != "running" || count(t, f.db, "SELECT count(*) FROM llm_requests WHERE state='prepared'") != 1 {
		t.Fatalf("admitted generation %+v", result)
	}
}
