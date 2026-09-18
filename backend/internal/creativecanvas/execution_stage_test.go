package creativecanvas_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type stagedExecutor struct {
	creativecanvas.NodeExecutor
	calls int
	steps []creativecanvas.ExecutionStep
}

func (e *stagedExecutor) Step(ctx context.Context, scope store.AccountScope, step creativecanvas.ExecutionStep) (creativecanvas.ExecutionObservation, error) {
	e.calls++
	e.steps = append(e.steps, step)
	if step.ResumeToken == "" {
		return creativecanvas.ExecutionObservation{State: creativecanvas.ExecutionWaiting, ResumeToken: "gateway-request-test", RetryAfter: time.Second}, nil
	}
	return e.NodeExecutor.Step(ctx, scope, step)
}
func TestExecutionStageResumesAndRetainsCompletedResult(t *testing.T) {
	db, scope, queue, _, p, ids := executionFixture(t)
	adapter := &stagedExecutor{NodeExecutor: creativecanvas.InternalTextCompose()}
	service, err := creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{adapter})
	if err != nil {
		t.Fatal(err)
	}
	e, cmd := requestExecution(t, scope, queue, service, p, ids[2])
	target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
	raw, _ := json.Marshal(target)
	job := jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}
	var deferred *jobs.DeferredError
	if err = service.Work(t.Context(), scope, job); !errors.As(err, &deferred) {
		t.Fatalf("waiting must defer: %v", err)
	}
	if err = service.Work(t.Context(), scope, job); !errors.As(err, &deferred) || adapter.calls != 1 {
		t.Fatalf("early delivery: calls=%d err=%v", adapter.calls, err)
	}
	if _, err = db.Exec(`UPDATE creative_node_executions SET next_step_at=clock_timestamp()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	service, err = creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{adapter})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE FUNCTION reject_stage_output() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.direction='output' THEN RAISE EXCEPTION 'test output unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_stage_output BEFORE INSERT ON creative_execution_refs FOR EACH ROW EXECUTE FUNCTION reject_stage_output()`); err != nil {
		t.Fatal(err)
	}
	if err = service.Work(t.Context(), scope, job); err == nil {
		t.Fatal("injected handoff failure missing")
	}
	if adapter.calls != 2 || adapter.steps[0].ExecutionID != e.ID || adapter.steps[1].ExecutionID != e.ID || adapter.steps[1].ResumeToken != "gateway-request-test" {
		t.Fatalf("lost continuation: %+v", adapter.steps)
	}
	if _, err = db.Exec(`DROP TRIGGER reject_stage_output ON creative_execution_refs`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE creative_node_executions SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Work(t.Context(), scope, job); err != nil {
		t.Fatal(err)
	}
	if adapter.calls != 2 {
		t.Fatal("handoff retry ran executor again")
	}
	if err = service.Work(t.Context(), scope, job); err != nil {
		t.Fatal(err)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	finished, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
	if err != nil || finished.State != "succeeded" || finished.ApplyState != "applied" {
		t.Fatalf("finished=%+v err=%v", finished, err)
	}
}

type observationExecutor struct {
	creativecanvas.NodeExecutor
	observe func(creativecanvas.ExecutionStep) creativecanvas.ExecutionObservation
}

func (e observationExecutor) Step(_ context.Context, _ store.AccountScope, step creativecanvas.ExecutionStep) (creativecanvas.ExecutionObservation, error) {
	return e.observe(step), nil
}

func TestExecutionStageUnknownAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume_unknown", true: "cancel_waiting"}[cancelled], func(t *testing.T) {
			db, scope, queue, _, p, ids := executionFixture(t)
			calls := 0
			executor := observationExecutor{NodeExecutor: creativecanvas.InternalTextCompose(), observe: func(step creativecanvas.ExecutionStep) creativecanvas.ExecutionObservation {
				calls++
				if calls == 1 {
					return creativecanvas.ExecutionObservation{State: creativecanvas.ExecutionReconciling, RetryAfter: time.Second}
				}
				var state string
				if err := db.QueryRow(`SELECT state FROM creative_node_executions WHERE id=$1`, step.ExecutionID).Scan(&state); err != nil || state != "reconciling" {
					t.Fatalf("claim erased recovery state: %s %v", state, err)
				}
				if !step.Reconciling || step.ResumeToken != "" {
					t.Fatalf("lost unknown state: %+v", step)
				}
				body := "已核实的结果"
				return creativecanvas.ExecutionObservation{State: creativecanvas.ExecutionCompleted, Payload: &creativecontent.Payload{Body: &body}}
			}}
			service, err := creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{executor})
			if err != nil {
				t.Fatal(err)
			}
			e, cmd := requestExecution(t, scope, queue, service, p, ids[2])
			target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
			raw, _ := json.Marshal(target)
			job := jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}
			var deferred *jobs.DeferredError
			if err = service.Work(t.Context(), scope, job); !errors.As(err, &deferred) {
				t.Fatal(err)
			}
			current, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
			if err != nil || current.State != "reconciling" {
				t.Fatalf("unknown not durable: %+v %v", current, err)
			}
			if _, err = service.Publish(t.Context(), scope, target); !errors.Is(err, creativecanvas.ErrVersionConflict) {
				t.Fatalf("published unknown: %v", err)
			}
			if cancelled {
				if _, err = creativecanvas.CancelNodeExecution(t.Context(), scope, command(t, target)); err != nil {
					t.Fatal(err)
				}
			} else if _, err = db.Exec(`UPDATE creative_node_executions SET next_step_at=clock_timestamp()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
				t.Fatal(err)
			}
			if err = service.Work(t.Context(), scope, job); err != nil {
				t.Fatal(err)
			}
			versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
			if err != nil {
				t.Fatal(err)
			}
			if cancelled && (calls != 1 || len(versions.Items) != 0) || !cancelled && (calls != 2 || len(versions.Items) != 1) {
				t.Fatalf("calls=%d versions=%d", calls, len(versions.Items))
			}
		})
	}
}

func TestExecutionStageRefusesChangedContinuationAndExpiredWait(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed_binding", true: "expired"}[expire], func(t *testing.T) {
			db, scope, queue, _, p, ids := executionFixture(t)
			calls := 0
			executor := observationExecutor{NodeExecutor: creativecanvas.InternalTextCompose(), observe: func(creativecanvas.ExecutionStep) creativecanvas.ExecutionObservation {
				calls++
				token := "first-request"
				if calls > 1 {
					token = "different-request"
				}
				return creativecanvas.ExecutionObservation{State: creativecanvas.ExecutionWaiting, ResumeToken: token, RetryAfter: time.Second}
			}}
			service, err := creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{executor})
			if err != nil {
				t.Fatal(err)
			}
			e, cmd := requestExecution(t, scope, queue, service, p, ids[2])
			target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
			raw, _ := json.Marshal(target)
			job := jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}
			var deferred *jobs.DeferredError
			if err = service.Work(t.Context(), scope, job); !errors.As(err, &deferred) {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE creative_node_executions SET next_step_at=clock_timestamp()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
				t.Fatal(err)
			}
			if expire {
				if _, err = db.Exec(`UPDATE creative_node_executions SET deadline=clock_timestamp()-interval '1 second' WHERE id=$1`, e.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = service.Work(t.Context(), scope, job); err != nil {
				t.Fatal(err)
			}
			current, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
			if err != nil {
				t.Fatal(err)
			}
			if expire && (current.State != "cancelled" || calls != 1) || !expire && (current.State != "failed" || calls != 2) {
				t.Fatalf("state=%s calls=%d", current.State, calls)
			}
			var token string
			if err = db.QueryRow(`SELECT resume_token FROM creative_node_executions WHERE id=$1`, e.ID).Scan(&token); err != nil || token != "first-request" {
				t.Fatalf("binding changed: %q %v", token, err)
			}
		})
	}
}
