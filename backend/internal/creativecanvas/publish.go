package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type PublishResult struct {
	ExecutionID          string        `json:"execution_id"`
	ApplyState           string        `json:"apply_state"`
	RegisteredVersionIDs []string      `json:"registered_version_ids"`
	Change               *ChangeResult `json:"change,omitempty"`
}

func (s *ExecutionService) Publish(ctx context.Context, scope store.AccountScope, target ExecutionTarget) (creativeops.Receipt, error) {
	saved, err := GetNodeExecution(ctx, scope, target)
	if err != nil {
		return creativeops.Receipt{}, err
	}
	command := creativeops.Command{OperationID: saved.PublishOperation, CreatedAt: saved.CreatedAt, Payload: creativegraph.Canonical(target)}
	return run(ctx, scope, "canvas.publish_execution", command, func(ExecutionTarget) error { return nil }, func(ctx context.Context, tx store.TxAccountScope, v ExecutionTarget) (creativeops.Outcome, error) {
		canvas, err := lockCanvas(ctx, tx, v.CanvasID, false)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		execution, err := readExecution(ctx, tx, v)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		before, ids, err := loadGraph(ctx, tx, canvas.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		n, live := before.Nodes[execution.NodeID]
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		result := PublishResult{ExecutionID: execution.ID, ApplyState: "discarded", RegisteredVersionIDs: []string{}}
		eligible := live && !canvas.Archived && n.ActiveExecutionID != nil && *n.ActiveExecutionID == execution.ID && execution.State == "succeeded" && execution.ApplyState == "pending" && now.Before(execution.Deadline) && s.executors[execution.ActionKey] != nil && s.executors[execution.ActionKey].Version() == execution.ExecutorVersion
		if !eligible {
			revision := canvas.Revision
			if execution.State == "queued" || execution.State == "running" || execution.State == "reconciling" {
				return creativeops.Outcome{}, ErrVersionConflict
			}
			if execution.ApplyState == "pending" {
				apply := "discarded"
				if !now.Before(execution.Deadline) {
					apply = "expired"
				}
				if err = finishExecution(ctx, tx, canvas, execution, execution.State, apply, ""); err != nil {
					return creativeops.Outcome{}, err
				}
				result.ApplyState = apply
				revision++
			} else {
				result.ApplyState = execution.ApplyState
			}
			return outcome(200, "node_execution", execution.ID, revision, result)
		}
		outputs, err := executionRefs(ctx, tx, execution.ID, "output")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		inputs, err := executionRefs(ctx, tx, execution.ID, "input")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if len(outputs) != 1 {
			return creativeops.Outcome{}, ErrGraphIntegrity
		}
		refs := []VersionInput{}
		for _, r := range inputs {
			refs = append(refs, VersionInput{RevisionID: r.ID, Role: "reference"})
		}
		version, err := makeVersion(ctx, tx, n.ID, outputs[0].ID, "generation", creativegraph.Canonical(map[string]any{"prompt": execution.Prompt, "executor_version": execution.ExecutorVersion}), refs)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		ordinal := 0
		version.ExecutionID = &execution.ID
		version.Ordinal = &ordinal
		after := cloneGraph(before)
		after.Versions[version.ID] = version
		applied := n.DataRevision == execution.TargetData && equalValue(creativegraph.Canonical(n.SelectedVersionID), creativegraph.Canonical(execution.Selected))
		for _, r := range execution.ReadSet {
			if r.Kind != "node" || r.DataRevision < 1 {
				continue
			}
			i, ok := ids[r.ID]
			if !ok || !i.Live || i.D != r.DataRevision {
				applied = false
			}
		}
		if applied {
			n.ContentID = &outputs[0].ContentID
			n.ContentRevisionID = &outputs[0].ID
			n.SelectedVersionID = &version.ID
			after.Nodes[n.ID] = n
			result.ApplyState = "applied"
		} else {
			result.ApplyState = "conflicted"
		}
		changes, err := planChanges(before, after, ids)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		change, err := persistGraph(ctx, tx, canvas, before, after, ids, changes, command.OperationID, newChangeID(), "", "", nil, nil)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		result.Change = &change
		result.RegisteredVersionIDs = append(result.RegisteredVersionIDs, version.ID)
		if _, err = tx.Update(ctx, "creative_node_executions", "apply_state=$2,change_id=$3", "id=$4", result.ApplyState, change.ChangeID, execution.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_nodes", "active_execution_id=NULL,status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", n.ID, execution.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "node_execution", execution.ID, change.ResultRevision, result)
	})
}
