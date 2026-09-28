package creativecanvas

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const GenerationAction = "gateway.generation"
const GenerationDriverVersion = "1"

// GenerationSpec 是driver已预检的输入，不携带模型价格或供应商定位。
type GenerationSpec struct {
	ParameterDefaults map[string]json.RawMessage
	Kind              string
	Prompt            string
	ModelKey          string
	Parameters        json.RawMessage
	References        []GenerationReference
}
type GenerationReference struct{ RevisionID, Role string }

// AdmitGenerationInTx 由driver在Gateway预锁之后调用；输入、执行和预算一起回滚。
func AdmitGenerationInTx(ctx context.Context, tx store.TxAccountScope, in RequestExecutionInput, spec GenerationSpec, executionID, operation, requestHash string, deadline time.Time) (NodeExecution, error) {
	if in.ActionKey != GenerationAction || executionID == "" || in.ExpectedDataRevision < 1 || in.DraftRevision < 1 {
		return NodeExecution{}, creativeops.ErrValidation
	}
	if err := tx.RequireCreativeCapability(ctx, "generation_apply"); err != nil {
		return NodeExecution{}, err
	}
	canvas, err := lockCanvas(ctx, tx, in.CanvasID, true)
	if err != nil {
		return NodeExecution{}, err
	}
	g, ids, err := loadGraph(ctx, tx, canvas.ID)
	if err != nil {
		return NodeExecution{}, err
	}
	n, ok := g.Nodes[in.NodeID]
	if !ok {
		return NodeExecution{}, ErrNotFound
	}
	if n.TypeVersion != 1 || n.TypeKey != "core."+spec.Kind || n.DataRevision != in.ExpectedDataRevision || n.ActiveExecutionID != nil {
		return NodeExecution{}, ErrVersionConflict
	}
	p, err := readPrompt(ctx, tx, canvas.ID, n.ID)
	if err != nil {
		return NodeExecution{}, err
	}
	if p == nil || p.Revision != in.DraftRevision || p.ActionKey != GenerationAction || p.Text != spec.Prompt || p.ModelKey != spec.ModelKey || !sameParameters(p.Parameters, spec.Parameters, spec.ParameterDefaults) {
		return NodeExecution{}, ErrVersionConflict
	}
	type orderedRef struct {
		rank, ordinal            int
		slot, id, revision, role string
	}
	var refs []orderedRef
	required := map[string]map[string]bool{n.ID: {"data": true}}
	for _, v := range g.Inputs {
		if v.NodeID != n.ID {
			continue
		}
		rank := 1
		if v.DraftID != nil {
			rank = 0
		}
		revision := v.ContentRevisionID
		if v.SourceNodeID != nil {
			source, ok := g.Nodes[*v.SourceNodeID]
			if !ok {
				return NodeExecution{}, ErrGraphIntegrity
			}
			revision = source.ContentRevisionID
			required[source.ID] = map[string]bool{"data": true}
		}
		if revision == nil {
			return NodeExecution{}, ErrGraphIntegrity
		}
		refs = append(refs, orderedRef{rank, v.Ordinal, v.Slot, v.ID, *revision, v.Role})
	}
	for _, v := range g.Edges {
		if v.TargetNodeID != n.ID {
			continue
		}
		source := g.Nodes[v.SourceNodeID]
		if source.ContentRevisionID == nil {
			return NodeExecution{}, ErrGraphIntegrity
		}
		required[source.ID] = map[string]bool{"data": true}
		refs = append(refs, orderedRef{2, v.Ordinal, v.TargetPort, v.ID, *source.ContentRevisionID, v.Role})
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.slot != b.slot {
			return a.slot < b.slot
		}
		if a.ordinal != b.ordinal {
			return a.ordinal < b.ordinal
		}
		return a.id < b.id
	})
	if len(refs) != len(spec.References) || len(refs) > 50 {
		return NodeExecution{}, ErrVersionConflict
	}
	for i, v := range refs {
		if spec.References[i].RevisionID != v.revision || spec.References[i].Role != v.role {
			return NodeExecution{}, ErrVersionConflict
		}
	}
	if err = validateReadSet(in.ReadSet, required, ids); err != nil {
		return NodeExecution{}, err
	}
	reads := []ObjectRead{}
	for id := range required {
		reads = append(reads, ObjectRead{Kind: "node", ID: id, DataRevision: ids[id].D})
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].ID < reads[j].ID })
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return NodeExecution{}, err
	}
	if !deadline.After(now) || deadline.After(now.Add(24*time.Hour)) {
		return NodeExecution{}, creativeops.ErrValidation
	}
	e := NodeExecution{ID: executionID, CanvasID: canvas.ID, NodeID: n.ID, ActionKey: GenerationAction, ExecutorVersion: GenerationDriverVersion, State: "queued", ApplyState: "pending", CreatedAt: now, Deadline: deadline, PublishOperation: uuid.NewString(), ResultOperation: uuid.NewString()}
	err = tx.Insert(ctx, "creative_node_executions", []string{"id", "canvas_id", "node_id", "action_key", "executor_version", "operation_id", "request_hash", "state", "deadline", "target_data_revision", "selected_version_id", "draft_revision", "prompt_snapshot", "read_set", "caller_kind", "publish_operation_id", "result_operation_id"}, e.ID, canvas.ID, n.ID, e.ActionKey, e.ExecutorVersion, operation, requestHash, e.State, deadline, int64(n.DataRevision), n.SelectedVersionID, int64(p.Revision), creativegraph.Canonical(p), creativegraph.Canonical(reads), "photographer", e.PublishOperation, e.ResultOperation)
	if err != nil {
		return NodeExecution{}, err
	}
	// 内容锁按修订ID稳定排序；输出列表保持原顺序与重复条目。
	sorted := append([]orderedRef(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].revision < sorted[j].revision })
	for _, v := range sorted {
		if _, err = creativecontent.RequireGenerationReference(ctx, tx, v.revision); err != nil {
			return NodeExecution{}, err
		}
	}
	for i, v := range refs {
		if err = tx.Insert(ctx, "creative_execution_refs", []string{"execution_id", "direction", "ordinal", "content_revision_id", "role"}, e.ID, "input", i, v.revision, v.role); err != nil {
			return NodeExecution{}, err
		}
	}
	if _, err = tx.Update(ctx, "creative_nodes", "active_execution_id=$2,latest_execution_id=$2,status_revision=status_revision+1", "id=$3", e.ID, n.ID); err != nil {
		return NodeExecution{}, err
	}
	return e, advanceCanvas(ctx, tx, canvas, false)
}
func sameParameters(a, b json.RawMessage, defaults map[string]json.RawMessage) bool {
	var av, bv map[string]any
	decode := func(raw json.RawMessage, dst any) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		return d.Decode(dst)
	}
	if decode(a, &av) != nil || decode(b, &bv) != nil || av == nil || bv == nil {
		return false
	}
	for key, raw := range defaults {
		if _, ok := av[key]; !ok {
			var v any
			if decode(raw, &v) != nil {
				return false
			}
			av[key] = v
		}
	}
	return equalValue(creativegraph.Canonical(av), creativegraph.Canonical(bv))
}

// GuardGenerationInTx 在最终派发的请求锁之后复核当前资格与用途授权。
func GuardGenerationInTx(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget) error {
	if err := tx.RequireCreativeCapability(ctx, "node_generate"); err != nil {
		return err
	}
	if err := tx.RequireCreativeCapability(ctx, "generation_apply"); err != nil {
		return err
	}
	canvas, err := lockCanvas(ctx, tx, target.CanvasID, false)
	if err != nil {
		return err
	}
	e, err := readExecution(ctx, tx, target)
	if err != nil {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	n, err := scanNode(tx.QueryRow(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", e.NodeID, e.CanvasID))
	if err != nil {
		return err
	}
	if canvas.Archived || !now.Before(e.Deadline) || e.ActionKey != GenerationAction || e.State != "queued" && e.State != "running" && e.State != "reconciling" || n.ActiveExecutionID == nil || *n.ActiveExecutionID != e.ID || n.DataRevision != e.TargetData {
		return ErrVersionConflict
	}
	for _, ref := range e.ReadSet {
		if ref.Kind != "node" {
			return ErrGraphIntegrity
		}
		var revision int64
		if err = tx.QueryRow(ctx, "creative_nodes", "data_revision", "id=$2 AND canvas_id=$3", ref.ID, e.CanvasID).Scan(&revision); err != nil {
			return err
		}
		if revision != int64(ref.DataRevision) {
			return ErrVersionConflict
		}
	}
	rows, err := tx.QueryPage(ctx, "creative_execution_refs", "content_revision_id", "execution_id=$2 AND direction='input'", []store.OrderBy{{Column: "content_revision_id"}}, 51, 0, e.ID)
	if err != nil {
		return err
	}
	var refs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range refs {
		if _, err = creativecontent.RequireGenerationReference(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// RetainGenerationOutputInTx 是候选保留：即使执行被取消，已付费结果仍保留90天。
// 它不授予发布资格，也不改变节点内容。
func RetainGenerationOutputInTx(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget, ordinal int, r creativecontent.Revision) error {
	e, err := readExecution(ctx, tx, target)
	if err != nil {
		return err
	}
	if e.ActionKey != GenerationAction || ordinal < 0 || ordinal > 63 {
		return creativeops.ErrValidation
	}
	var previous string
	err = tx.QueryRow(ctx, "creative_execution_refs", "content_revision_id", "execution_id=$2 AND direction='output' AND ordinal=$3", e.ID, ordinal).Scan(&previous)
	if err == nil {
		if previous != r.ID {
			return ErrVersionConflict
		}
		return nil
	}
	if err != store.ErrNoRows {
		return err
	}
	if err = tx.Insert(ctx, "creative_execution_refs", []string{"execution_id", "direction", "ordinal", "content_revision_id", "role"}, e.ID, "output", ordinal, r.ID, "result"); err != nil {
		return err
	}
	// 到期的执行也可接住迟到作品，新的保留窗口从交接时刻起算。
	if _, err = tx.Update(ctx, "creative_node_executions", "retained_until=GREATEST(retained_until,clock_timestamp()+interval '90 days')", "id=$2", e.ID); err != nil {
		return err
	}
	inputs, err := generationSourceIdentities(ctx, tx, e.ID)
	if err != nil {
		return err
	}
	return creativecontent.RetainDerivativeRights(ctx, tx, r.ID, inputs)
}

func CompleteGenerationInTx(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget, failed bool) error {
	canvas, err := lockCanvas(ctx, tx, target.CanvasID, false)
	if err != nil {
		return err
	}
	e, err := readExecution(ctx, tx, target)
	if err != nil {
		return err
	}
	if e.ActionKey != GenerationAction {
		return creativeops.ErrValidation
	}
	if e.State == "cancelled" || e.State == "succeeded" || e.State == "failed" {
		return nil
	}
	has, err := tx.Exists(ctx, "creative_execution_refs", "execution_id=$2 AND direction='output'", e.ID)
	if err != nil {
		return err
	}
	if !has {
		if failed {
			return finishExecution(ctx, tx, canvas, e, "failed", "discarded", "generation_failed")
		}
		return ErrVersionConflict
	}
	if _, err = tx.Update(ctx, "creative_node_executions", "state='succeeded',lease_until=NULL,next_step_at=NULL,error=$3", "id=$2", e.ID, map[bool]string{true: "partial_import", false: ""}[failed]); err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", e.NodeID, e.ID); err != nil {
		return err
	}
	return advanceCanvas(ctx, tx, canvas, false)
}

// LockGenerationHandoffInTx 在内容写入前固定业务锁顺序，取消不阻止候选保留。
func LockGenerationHandoffInTx(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget) error {
	if _, err := lockCanvas(ctx, tx, target.CanvasID, false); err != nil {
		return err
	}
	e, err := readExecution(ctx, tx, target)
	if err != nil {
		return err
	}
	if e.ActionKey != GenerationAction {
		return creativeops.ErrValidation
	}
	return nil
}

// 保留衍生权利只读来源身份，不把撤销后的授权当作继续发布资格。
func generationSourceIdentities(ctx context.Context, tx store.TxAccountScope, id string) ([]creativecontent.Revision, error) {
	rows, err := tx.QueryPage(ctx, "creative_execution_refs", "content_revision_id", "execution_id=$2 AND direction='input'", []store.OrderBy{{Column: "content_revision_id"}}, 51, 0, id)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var sources []creativecontent.Revision
	for _, id := range ids {
		r := creativecontent.Revision{ID: id}
		if err = tx.QueryRow(ctx, "creative_content_revisions", "rights_declaration_id", "id=$2", id).Scan(&r.DeclarationID); err != nil {
			return nil, err
		}
		sources = append(sources, r)
	}
	return sources, nil
}

// MarkGenerationVerificationInTx 标记仍非终态的执行；不释放输入根或当前执行资格。
func MarkGenerationVerificationInTx(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget) error {
	canvas, err := lockCanvas(ctx, tx, target.CanvasID, false)
	if err != nil {
		return err
	}
	e, err := readExecution(ctx, tx, target)
	if err != nil {
		return err
	}
	if e.State != "queued" && e.State != "running" && e.State != "reconciling" {
		return nil
	}
	if _, err = tx.Update(ctx, "creative_node_executions", "state='reconciling',error='generation_verification_required'", "id=$2", e.ID); err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", e.NodeID, e.ID); err != nil {
		return err
	}
	return advanceCanvas(ctx, tx, canvas, false)
}
