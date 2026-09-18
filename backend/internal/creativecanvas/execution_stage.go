package creativecanvas

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ExecutionStep is a durable business continuation, not a provider request.
// External drivers must use ExecutionID as their Gateway binding and perform
// final admission there. An empty token never proves that a submission is safe:
// the driver must recover its durable Gateway binding after a lost checkpoint.
// Provider adapters must not be registered directly as NodeExecutors.
type ExecutionStep struct {
	ExecutionID string
	Epoch       int64
	Deadline    time.Time
	Prompt      NodePrompt
	Inputs      []creativecontent.Revision
	ResumeToken string
	Reconciling bool
}
type ExecutionStage string

const (
	ExecutionCompleted   ExecutionStage = "completed"
	ExecutionWaiting     ExecutionStage = "waiting"
	ExecutionReconciling ExecutionStage = "reconciling"
)

// ResumeToken is a non-secret stable Gateway reference, never a signed URL or
// credential. Payload is the bounded text result supported by this first slice.
// Step must do one bounded action. It may not poll internally or blindly retry
// external submission. Uncertain acceptance is Reconciling, not an error that
// licenses a fresh submission. Errors mean a definitive local failure.
type ExecutionObservation struct {
	State       ExecutionStage
	ResumeToken string
	RetryAfter  time.Duration
	Payload     *creativecontent.Payload
}

func (o ExecutionObservation) validate(previous string) error {
	if len(o.ResumeToken) > 200 || previous != "" && o.ResumeToken != "" && o.ResumeToken != previous {
		return creativeops.ErrValidation
	}
	switch o.State {
	case ExecutionCompleted:
		if o.Payload == nil || o.RetryAfter != 0 {
			return creativeops.ErrValidation
		}
		return creativecontent.Validate(creativecontent.Draft{Kind: "text", Payload: *o.Payload})
	case ExecutionWaiting, ExecutionReconciling:
		if o.Payload != nil || o.RetryAfter < time.Second || o.RetryAfter > time.Minute || o.State == ExecutionWaiting && o.ResumeToken == "" {
			return creativeops.ErrValidation
		}
	default:
		return creativeops.ErrValidation
	}
	return nil
}

func (s *ExecutionService) checkpoint(ctx context.Context, scope store.AccountScope, target ExecutionTarget, execution NodeExecution, observation ExecutionObservation) error {
	var delay time.Duration
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		canvas, err := lockCanvas(ctx, tx, target.CanvasID, false)
		if err != nil {
			return err
		}
		current, err := readExecution(ctx, tx, target)
		if err != nil {
			return err
		}
		if (current.State != "running" && current.State != "reconciling") || current.Epoch != execution.Epoch {
			return ErrVersionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if canvas.Archived || !now.Before(current.Deadline) || current.Lease == nil || !now.Before(*current.Lease) {
			return ErrVersionConflict
		}
		token := observation.ResumeToken
		if token == "" {
			token = current.ResumeToken
		}
		if observation.State == ExecutionCompleted {
			_, err = tx.Update(ctx, "creative_node_executions", "result_payload=$2,resume_token=$3,next_step_at=NULL", "id=$4", observation.Payload, token, current.ID)
			return err
		}
		state := "running"
		if observation.State == ExecutionReconciling {
			state = "reconciling"
		}
		delay = min(observation.RetryAfter, current.Deadline.Sub(now))
		_, err = tx.Update(ctx, "creative_node_executions", "state=$2,resume_token=$3,next_step_at=$4,lease_until=NULL", "id=$5", state, token, now.Add(delay), current.ID)
		if err != nil {
			return err
		}
		if _, err = tx.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", current.NodeID, current.ID); err != nil {
			return err
		}
		return advanceCanvas(ctx, tx, canvas, false)
	})
	if err != nil {
		return err
	}
	if delay > 0 {
		return jobs.Defer(delay)
	}
	return nil
}
