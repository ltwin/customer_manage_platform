package llmgateway_test

import (
	"context"
	"errors"
	"testing"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestGenerationHandoffAllOutputsAndRollback(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}}
	r, p, _ := generationRuntime(t, f, a)
	v := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
		t.Fatal(err)
	}
	outputs, err := r.ImportableOutputs(t.Context(), f.a, v.ID, "creative_agent", "canvas:g3")
	if err != nil || len(outputs) != 1 || outputs[0].Output.Media.FetchURL == "" {
		t.Fatal(outputs, err)
	}
	output := outputs[0]
	injected := errors.New("handoff rollback")
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return r.HandoffOutputInTx(t.Context(), tx, output, "creative_agent", "canvas:g3", func() (string, error) { return "", injected })
	})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	if queryCount(t, f.db, `SELECT count(*) FROM llm_result_consumers WHERE state='pending'`) != 1 {
		t.Fatal("rollback released consumer")
	}
	calls := 0
	for range 2 {
		err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			return r.HandoffOutputInTx(t.Context(), tx, output, "creative_agent", "canvas:g3", func() (string, error) { calls++; return "retained-revision", nil })
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || queryCount(t, f.db, `SELECT count(*) FROM llm_result_consumers WHERE state='consumed'`) != 1 {
		t.Fatal("handoff did not replay", calls)
	}
	if _, err = r.ImportableOutputs(t.Context(), f.b, v.ID, "creative_agent", "canvas:g3"); !errors.Is(err, gw.ErrNotFound) {
		t.Fatal("cross account", err)
	}
}

func TestGenerationHandoffDistinguishesUsageAndOutputConflict(t *testing.T) {
	for _, resultConflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "usage", true: "output"}[resultConflict], func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
				return generationCompleted(f), nil
			}}
			r, p, _ := generationRuntime(t, f, a)
			v := admitRuntime(t, f, r, p)
			if _, err := r.AdvanceGeneration(t.Context(), f.a, v.ID); err != nil {
				t.Fatal(err)
			}
			c := gw.GenerationClaim{RequestID: v.ID}
			if err := f.db.QueryRow(`SELECT attempt_id,epoch FROM llm_generation_tasks WHERE request_id=$1`, v.ID).Scan(&c.AttemptID, &c.Epoch); err != nil {
				t.Fatal(err)
			}
			event := generationCompleted(f)
			event.Usage = map[string]int64{"output_ms": 2000}
			if resultConflict {
				event.Observation.Outputs[0].Media.ByteSize++
			}
			err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				return r.RecordGenerationObservationInTx(t.Context(), tx, c, event)
			})
			if err != nil {
				t.Fatal(err)
			}
			progress, err := r.GetGeneration(t.Context(), f.a, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !progress.VerificationRequired || progress.ResultVerificationRequired != resultConflict {
				t.Fatalf("wrong isolation: %+v", progress)
			}
			_, err = r.ImportableOutputs(t.Context(), f.a, v.ID, "creative_agent", "canvas:g3")
			if resultConflict && !errors.Is(err, gw.ErrState) || !resultConflict && err != nil {
				t.Fatal("wrong handoff isolation", err)
			}
		})
	}
}

func TestGenerationHandoffNewTaskCollisionMustIsolateResult(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	a := &generationAdapter{submit: func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		return generationCompleted(f), nil
	}}
	r, p, _ := generationRuntime(t, f, a)
	first := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, first.ID); err != nil {
		t.Fatal(err)
	}
	a.submit = func(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
		e := generationCompleted(f)
		e.Observation.TaskID = ""
		return e, nil
	}
	second := admitRuntime(t, f, r, p)
	if _, err := r.AdvanceGeneration(t.Context(), f.a, second.ID); err != nil {
		t.Fatal(err)
	}
	c := gw.GenerationClaim{RequestID: second.ID}
	if err := f.db.QueryRow(`SELECT attempt_id,epoch FROM llm_generation_tasks WHERE request_id=$1`, second.ID).Scan(&c.AttemptID, &c.Epoch); err != nil {
		t.Fatal(err)
	}
	e := generationCompleted(f)
	e.EventKey = "late-task-binding"
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error { return r.RecordGenerationObservationInTx(t.Context(), tx, c, e) }); err != nil {
		t.Fatal(err)
	}
	progress, err := r.GetGeneration(t.Context(), f.a, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := r.ImportableOutputs(t.Context(), f.a, second.ID, "creative_agent", "canvas:g3")
	t.Logf("verify=%v result_verify=%v outputs=%d import_err=%v", progress.VerificationRequired, progress.ResultVerificationRequired, len(outputs), err)
	if !progress.ResultVerificationRequired || !errors.Is(err, gw.ErrState) {
		t.Fatal("colliding task identity incorrectly classified as billing-only conflict")
	}
}
