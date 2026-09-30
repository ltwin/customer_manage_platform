package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"

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
	return publishExecution(ctx, scope, target, func(e NodeExecution) bool {
		return s.executors[e.ActionKey] != nil && s.executors[e.ActionKey].Version() == e.ExecutorVersion
	}, nil)
}

// PublishGenerationExecution 复用版本/撤销协议；prelock由生成driver取得Gateway部署/请求锁。
func PublishGenerationExecution(ctx context.Context, scope store.AccountScope, target ExecutionTarget, prelock func(context.Context, store.TxAccountScope) (bool, error)) (creativeops.Receipt, error) {
	if prelock == nil {
		return creativeops.Receipt{}, creativeops.ErrValidation
	}
	return publishExecution(ctx, scope, target, func(e NodeExecution) bool {
		return e.ActionKey == GenerationAction && e.ExecutorVersion == GenerationDriverVersion
	}, prelock)
}
func publishExecution(ctx context.Context, scope store.AccountScope, target ExecutionTarget, registered func(NodeExecution) bool, prelock func(context.Context, store.TxAccountScope) (bool, error)) (creativeops.Receipt, error) {
	saved, err := GetNodeExecution(ctx, scope, target)
	if err != nil {
		return creativeops.Receipt{}, err
	}
	command := creativeops.Command{OperationID: saved.PublishOperation, CreatedAt: saved.CreatedAt, Payload: creativegraph.Canonical(target)}
	return run(ctx, scope, "canvas.publish_execution", command, func(ExecutionTarget) error { return nil }, func(ctx context.Context, tx store.TxAccountScope, v ExecutionTarget) (creativeops.Outcome, error) {
		allowed := true
		if prelock != nil {
			var err error
			allowed, err = prelock(ctx, tx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
		}
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
		eligible := allowed && live && !canvas.Archived && n.ActiveExecutionID != nil && *n.ActiveExecutionID == execution.ID && execution.State == "succeeded" && execution.ApplyState == "pending" && now.Before(execution.Deadline) && registered(execution)
		if eligible && prelock != nil {
			sources, err := generationSourceIdentities(ctx, tx, execution.ID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			for _, source := range sources {
				if _, err := creativecontent.RequireGenerationReference(ctx, tx, source.ID); err != nil {
					eligible = false
					break
				}
			}
		}
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
		if len(outputs) < 1 || len(outputs) > 64 {
			return creativeops.Outcome{}, ErrGraphIntegrity
		}
		refs := []VersionInput{}
		inputRows, err := tx.QueryPage(ctx, "creative_execution_refs", "content_revision_id,role", "execution_id=$2 AND direction='input'", []store.OrderBy{{Column: "ordinal"}}, 64, 0, execution.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		for inputRows.Next() {
			var ref VersionInput
			if err = inputRows.Scan(&ref.RevisionID, &ref.Role); err != nil {
				inputRows.Close()
				return creativeops.Outcome{}, err
			}
			refs = append(refs, ref)
		}
		inputRows.Close()
		if err = inputRows.Err(); err != nil {
			return creativeops.Outcome{}, err
		}
		if len(refs) != len(inputs) {
			return creativeops.Outcome{}, ErrGraphIntegrity
		}
		after := cloneGraph(before)
		// 原ordinal来自执行根；部分成功不能把后续输出重新编号。
		ordinals := map[string]int{}
		rows, err := tx.QueryPage(ctx, "creative_execution_refs", "content_revision_id,ordinal", "execution_id=$2 AND direction='output'", []store.OrderBy{{Column: "ordinal"}}, 64, 0, execution.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		for rows.Next() {
			var id string
			var ordinal int
			if err = rows.Scan(&id, &ordinal); err != nil {
				rows.Close()
				return creativeops.Outcome{}, err
			}
			ordinals[id] = ordinal
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return creativeops.Outcome{}, err
		}
		var lastVersion string
		for _, output := range outputs {
			version, err := makeVersion(ctx, tx, n.ID, output.ID, "generation", creativegraph.Canonical(map[string]any{"prompt": execution.Prompt, "executor_version": execution.ExecutorVersion, "reference_sources": execution.ReferenceSources}), refs)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			ordinal := ordinals[output.ID]
			version.ExecutionID = &execution.ID
			version.Ordinal = &ordinal
			after.Versions[version.ID] = version
			result.RegisteredVersionIDs = append(result.RegisteredVersionIDs, version.ID)
			lastVersion = version.ID
		}
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
			if !mainVersionPinned(n) {
				n.Config = mainVersionConfig(n.Config, false)
				latest := outputs[len(outputs)-1]
				n.ContentID = &latest.ContentID
				n.ContentRevisionID = &latest.ID
				n.SelectedVersionID = &lastVersion
				fitMediaSize(&n, latest)
				after.Nodes[n.ID] = n
			}
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
		if _, err = tx.Update(ctx, "creative_node_executions", "apply_state=$2,change_id=$3", "id=$4", result.ApplyState, change.ChangeID, execution.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_nodes", "active_execution_id=NULL,status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", n.ID, execution.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "node_execution", execution.ID, change.ResultRevision, result)
	})
}
