package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var ErrActionUnavailable = errors.New("node action is not registered")

type NodeExecutor interface {
	ActionKey() string
	Version() string
	Step(context.Context, store.AccountScope, ExecutionStep) (ExecutionObservation, error)
}
type ExecutionService struct{ executors map[string]NodeExecutor }

func NewExecutionService(executors []NodeExecutor) (*ExecutionService, error) {
	s := &ExecutionService{executors: map[string]NodeExecutor{}}
	for _, e := range executors {
		if e == nil || e.ActionKey() == "" || e.Version() == "" || s.executors[e.ActionKey()] != nil {
			return nil, creativeops.ErrValidation
		}
		s.executors[e.ActionKey()] = e
	}
	return s, nil
}

type textCompose struct{}

func InternalTextCompose() NodeExecutor { return textCompose{} }
func (textCompose) ActionKey() string   { return "internal.text-compose" }
func (textCompose) Version() string     { return "1" }
func (textCompose) Step(ctx context.Context, _ store.AccountScope, step ExecutionStep) (ExecutionObservation, error) {
	inputs := step.Inputs
	if err := ctx.Err(); err != nil {
		return ExecutionObservation{}, err
	}
	if len(inputs) < 2 {
		return ExecutionObservation{}, creativeops.ErrValidation
	}
	parts := []string{}
	for _, r := range inputs {
		if r.Kind != "text" || r.Payload.Body == nil {
			return ExecutionObservation{}, creativeops.ErrValidation
		}
		parts = append(parts, *r.Payload.Body)
	}
	body := strings.Join(parts, "\n\n")
	if utf8.RuneCountInString(body) > 100000 {
		return ExecutionObservation{}, creativeops.ErrValidation
	}
	return ExecutionObservation{State: ExecutionCompleted, Payload: &creativecontent.Payload{Body: &body}}, nil
}

type RequestExecutionInput struct {
	CanvasID             string               `json:"canvas_id"`
	NodeID               string               `json:"node_id"`
	ActionKey            string               `json:"action_key"`
	ExpectedDataRevision creativeops.Revision `json:"expected_data_revision"`
	DraftRevision        creativeops.Revision `json:"draft_revision"`
	ReadSet              []ObjectRead         `json:"read_set"`
}
type ExecutionTarget struct {
	CanvasID    string `json:"canvas_id"`
	ExecutionID string `json:"execution_id"`
}
type NodeExecution struct {
	ResumeToken      string                   `json:"-"`
	NextStepAt       *time.Time               `json:"-"`
	ResultPayload    *creativecontent.Payload `json:"-"`
	ExecutorVersion  string                   `json:"executor_version"`
	ID               string                   `json:"execution_id"`
	NodeID           string                   `json:"node_id"`
	ActionKey        string                   `json:"action_key"`
	State            string                   `json:"state"`
	ApplyState       string                   `json:"apply_state"`
	Epoch            int64                    `json:"execution_epoch"`
	Deadline         time.Time                `json:"deadline"`
	ChangeID         *string                  `json:"change_id"`
	Error            string                   `json:"error"`
	CanvasID         string                   `json:"canvas_id"`
	CreatedAt        time.Time                `json:"created_at"`
	TargetData       creativeops.Revision     `json:"-"`
	Selected         *string                  `json:"-"`
	Prompt           NodePrompt               `json:"-"`
	ReadSet          []ObjectRead             `json:"-"`
	PublishOperation string                   `json:"-"`
	ResultOperation  string                   `json:"-"`
	Lease            *time.Time               `json:"-"`
}

func (s *ExecutionService) Request(ctx context.Context, scope store.AccountScope, c creativeops.Command, runtime jobs.Runtime) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.request_execution", c, func(v RequestExecutionInput) error {
		if s.executors[v.ActionKey] == nil {
			return ErrActionUnavailable
		}
		if runtime == nil || v.CanvasID == "" || v.NodeID == "" || v.ExpectedDataRevision < 1 || v.DraftRevision < 1 {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v RequestExecutionInput) (creativeops.Outcome, error) {
		canvas, err := lockCanvas(ctx, tx, v.CanvasID, true)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		g, ids, err := loadGraph(ctx, tx, canvas.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		n, ok := g.Nodes[v.NodeID]
		if !ok {
			return creativeops.Outcome{}, ErrNotFound
		}
		if n.TypeKey != "core.text" || n.TypeVersion != 1 || n.DataRevision != v.ExpectedDataRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		if n.ActiveExecutionID != nil {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		p, err := readPrompt(ctx, tx, canvas.ID, n.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if p == nil || p.Revision != v.DraftRevision || p.ActionKey != v.ActionKey {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		type reference struct {
			rank, ordinal            int
			slot, id, revision, role string
		}
		ordered := []reference{}
		required := map[string]map[string]bool{n.ID: {"data": true}}
		for _, edge := range g.Edges {
			if edge.TargetNodeID != n.ID {
				continue
			}
			source := g.Nodes[edge.SourceNodeID]
			if source.ContentRevisionID == nil {
				return creativeops.Outcome{}, creativeops.ErrValidation
			}
			ordered = append(ordered, reference{rank: 2, ordinal: edge.Ordinal, slot: edge.TargetPort, id: edge.ID, revision: *source.ContentRevisionID, role: edge.Role})
			required[source.ID] = map[string]bool{"data": true}
		}
		for _, input := range g.Inputs {
			if input.NodeID != n.ID {
				continue
			}
			rank := 1
			if input.DraftID != nil {
				rank = 0
			}
			revision := input.ContentRevisionID
			if input.SourceNodeID != nil {
				source := g.Nodes[*input.SourceNodeID]
				revision = source.ContentRevisionID
				required[source.ID] = map[string]bool{"data": true}
			}
			if revision == nil {
				return creativeops.Outcome{}, creativeops.ErrValidation
			}
			ordered = append(ordered, reference{rank: rank, ordinal: input.Ordinal, slot: input.Slot, id: input.ID, revision: *revision, role: input.Role})
		}
		// Explicit draft order, explicit node slots, then visible edges. A duplicate
		// revision keeps its first position; contradictory roles are rejected.
		sort.Slice(ordered, func(i, j int) bool {
			a, b := ordered[i], ordered[j]
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
		inputIDs := []string{}
		roles := map[string]string{}
		for _, ref := range ordered {
			if role, exists := roles[ref.revision]; exists {
				if role != ref.role {
					return creativeops.Outcome{}, creativeops.ErrValidation
				}
				continue
			}
			roles[ref.revision] = ref.role
			inputIDs = append(inputIDs, ref.revision)
		}
		if len(inputIDs) < 2 || len(inputIDs) > 50 {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		if err = validateReadSet(v.ReadSet, required, ids); err != nil {
			return creativeops.Outcome{}, err
		}
		executionReads := []ObjectRead{}
		for id := range required {
			executionReads = append(executionReads, ObjectRead{Kind: "node", ID: id, DataRevision: ids[id].D})
		}
		sort.Slice(executionReads, func(i, j int) bool { return executionReads[i].ID < executionReads[j].ID })
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		execution := NodeExecution{ExecutorVersion: s.executors[v.ActionKey].Version(), ID: "cwexec_" + uuid.NewString(), NodeID: n.ID, CanvasID: canvas.ID, ActionKey: v.ActionKey, State: "queued", ApplyState: "pending", Deadline: now.Add(30 * time.Second), CreatedAt: now, PublishOperation: uuid.NewString(), ResultOperation: uuid.NewString()}
		if err = tx.Insert(ctx, "creative_node_executions", []string{"id", "canvas_id", "node_id", "action_key", "executor_version", "operation_id", "request_hash", "state", "deadline", "target_data_revision", "selected_version_id", "draft_revision", "prompt_snapshot", "read_set", "caller_kind", "publish_operation_id", "result_operation_id"}, execution.ID, canvas.ID, n.ID, v.ActionKey, s.executors[v.ActionKey].Version(), c.OperationID, creativegraph.Hash(c.Payload), "queued", execution.Deadline, int64(n.DataRevision), n.SelectedVersionID, int64(p.Revision), creativegraph.Canonical(p), creativegraph.Canonical(executionReads), "photographer", execution.PublishOperation, execution.ResultOperation); err != nil {
			return creativeops.Outcome{}, err
		}
		for ordinal, id := range inputIDs {
			r, err := creativecontent.RequireUsable(ctx, tx, id, "display")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if r.Kind != "text" {
				return creativeops.Outcome{}, creativeops.ErrValidation
			}
			if err = tx.Insert(ctx, "creative_execution_refs", []string{"execution_id", "direction", "ordinal", "content_revision_id", "role"}, execution.ID, "input", ordinal, id, "reference"); err != nil {
				return creativeops.Outcome{}, err
			}
		}
		if _, err = tx.Update(ctx, "creative_nodes", "active_execution_id=$2,latest_execution_id=$2,status_revision=status_revision+1", "id=$3 AND canvas_id=$4", execution.ID, n.ID, canvas.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = advanceCanvas(ctx, tx, canvas, false); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = jobs.EnqueueInTx(ctx, tx.Jobs(runtime), jobs.Request{Kind: "canvas.node_execution", OperationID: c.OperationID, CreatedAt: now, Payload: creativegraph.Canonical(ExecutionTarget{CanvasID: canvas.ID, ExecutionID: execution.ID})}); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(202, "node_execution", execution.ID, canvas.Revision+1, execution)
	})
}
func readExecution(ctx context.Context, tx store.TxAccountScope, target ExecutionTarget) (NodeExecution, error) {
	var e NodeExecution
	var data int64
	var prompt, reads, result json.RawMessage
	err := tx.QueryRowForUpdate(ctx, "creative_node_executions", "id,node_id,action_key,state,apply_state,execution_epoch,deadline,change_id,error,canvas_id,created_at,target_data_revision,selected_version_id,prompt_snapshot,read_set,publish_operation_id,result_operation_id,lease_until,executor_version,resume_token,next_step_at,result_payload", "id=$2 AND canvas_id=$3", target.ExecutionID, target.CanvasID).Scan(&e.ID, &e.NodeID, &e.ActionKey, &e.State, &e.ApplyState, &e.Epoch, &e.Deadline, &e.ChangeID, &e.Error, &e.CanvasID, &e.CreatedAt, &data, &e.Selected, &prompt, &reads, &e.PublishOperation, &e.ResultOperation, &e.Lease, &e.ExecutorVersion, &e.ResumeToken, &e.NextStepAt, &result)
	if err != nil {
		return e, notFound(err)
	}
	if len(result) > 0 {
		if err = json.Unmarshal(result, &e.ResultPayload); err != nil {
			return e, err
		}
	}
	e.TargetData = creativeops.Revision(data)
	if err = json.Unmarshal(prompt, &e.Prompt); err != nil {
		return e, err
	}
	err = json.Unmarshal(reads, &e.ReadSet)
	return e, err
}
func GetNodeExecution(ctx context.Context, scope store.AccountScope, target ExecutionTarget) (NodeExecution, error) {
	var e NodeExecution
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		e, err = readExecution(ctx, tx, target)
		return err
	})
	return e, err
}
func executionRefs(ctx context.Context, tx store.TxAccountScope, id, direction string) ([]creativecontent.Revision, error) {
	rows, err := tx.QueryPage(ctx, "creative_execution_refs", "content_revision_id", "execution_id=$2 AND direction=$3", []store.OrderBy{{Column: "ordinal"}}, 51, 0, id, direction)
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
	refs := []creativecontent.Revision{}
	for _, id := range ids {
		r, err := creativecontent.RequireUsable(ctx, tx, id, "display")
		if err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, nil
}
func (s *ExecutionService) Handler() store.JobHandler {
	return store.JobHandler{Kind: "canvas.node_execution", Work: s.Work}
}
func (s *ExecutionService) Work(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	var target ExecutionTarget
	if err := creativeops.Decode(job.Payload, &target); err != nil {
		return err
	}
	var execution NodeExecution
	var inputs []creativecontent.Revision
	claimed := false
	var deferFor time.Duration
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		canvas, err := lockCanvas(ctx, tx, target.CanvasID, false)
		if err != nil {
			return err
		}
		execution, err = readExecution(ctx, tx, target)
		if err != nil {
			return err
		}
		if execution.State == "succeeded" || execution.State == "cancelled" || execution.State == "failed" {
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if canvas.Archived || !now.Before(execution.Deadline) {
			return finishExecution(ctx, tx, canvas, execution, "cancelled", "expired", "")
		}
		if execution.Lease != nil && now.Before(*execution.Lease) {
			deferFor = min(execution.Lease.Sub(now), execution.Deadline.Sub(now))
			return nil
		}
		if execution.NextStepAt != nil && now.Before(*execution.NextStepAt) {
			deferFor = min(execution.NextStepAt.Sub(now), execution.Deadline.Sub(now))
			return nil
		}
		if s.executors[execution.ActionKey] == nil || s.executors[execution.ActionKey].Version() != execution.ExecutorVersion {
			return ErrActionUnavailable
		}
		inputs, err = executionRefs(ctx, tx, execution.ID, "input")
		if err != nil {
			return err
		}
		execution.Epoch++
		lease := minTime(now.Add(15*time.Second), execution.Deadline)
		execution.Lease = &lease
		state := "running"
		if execution.State == "reconciling" {
			state = execution.State
		}
		if _, err = tx.Update(ctx, "creative_node_executions", "state=$2,execution_epoch=$3,lease_until=$4", "id=$5", state, execution.Epoch, lease, execution.ID); err != nil {
			return err
		}
		if _, err = tx.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", execution.NodeID, execution.ID); err != nil {
			return err
		}
		claimed = true
		return advanceCanvas(ctx, tx, canvas, false)
	})
	if err != nil {
		return err
	}
	if deferFor > 0 {
		return jobs.Defer(deferFor)
	}
	if !claimed {
		if execution.State == "succeeded" && execution.ApplyState == "pending" {
			_, err = s.Publish(ctx, scope, target)
		}
		return err
	}
	payload := execution.ResultPayload
	if payload == nil {
		stepCtx, cancel := context.WithDeadline(ctx, *execution.Lease)
		observation, stepErr := s.executors[execution.ActionKey].Step(stepCtx, scope, ExecutionStep{ExecutionID: execution.ID, Epoch: execution.Epoch, Deadline: execution.Deadline, Prompt: execution.Prompt, Inputs: inputs, ResumeToken: execution.ResumeToken, Reconciling: execution.State == "reconciling"})
		cancel()
		if stepErr != nil {
			return s.fail(ctx, scope, target, execution.Epoch, stepErr)
		}
		if err = observation.validate(execution.ResumeToken); err != nil {
			return s.fail(ctx, scope, target, execution.Epoch, err)
		}
		if err = s.checkpoint(ctx, scope, target, execution, observation); err != nil {
			return err
		}
		payload = observation.Payload
	}

	resultCommand := creativeops.Command{OperationID: execution.ResultOperation, CreatedAt: execution.CreatedAt, Payload: creativegraph.Canonical(target)}
	_, err = run(ctx, scope, "canvas.execution_result", resultCommand, func(ExecutionTarget) error { return nil }, func(ctx context.Context, tx store.TxAccountScope, v ExecutionTarget) (creativeops.Outcome, error) {
		canvas, err := lockCanvas(ctx, tx, v.CanvasID, false)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		current, err := readExecution(ctx, tx, v)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if (current.State != "running" && current.State != "reconciling") || current.Epoch != execution.Epoch {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if canvas.Archived || !now.Before(current.Deadline) || current.Lease == nil || !now.Before(*current.Lease) {
			if err = finishExecution(ctx, tx, canvas, current, "cancelled", "expired", ""); err != nil {
				return creativeops.Outcome{}, err
			}
			current.State = "cancelled"
			current.ApplyState = "expired"
			return outcome(200, "node_execution", current.ID, canvas.Revision+1, current)
		}
		sources, err := executionRefs(ctx, tx, current.ID, "input")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if len(sources) < 2 {
			return creativeops.Outcome{}, ErrGraphIntegrity
		}
		draft := creativecontent.Draft{Kind: "text", Payload: *payload}
		output, err := creativecontent.WriteAndRetain(ctx, tx, draft, sources[0].ID, &current.NodeID, func(r creativecontent.Revision) error {
			return tx.Insert(ctx, "creative_execution_refs", []string{"execution_id", "direction", "ordinal", "content_revision_id", "role"}, current.ID, "output", 0, r.ID, "result")
		})
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = creativecontent.RetainDerivativeRights(ctx, tx, output.ID, sources); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_node_executions", "state='succeeded',lease_until=NULL", "id=$2", current.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_nodes", "status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", current.NodeID, current.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = advanceCanvas(ctx, tx, canvas, false); err != nil {
			return creativeops.Outcome{}, err
		}
		current.State = "succeeded"
		return outcome(200, "node_execution", current.ID, canvas.Revision+1, current)
	})
	if err != nil {
		return err
	}
	_, err = s.Publish(ctx, scope, target)
	return err
}
func finishExecution(ctx context.Context, tx store.TxAccountScope, canvas Canvas, e NodeExecution, state, apply, message string) error {
	if _, err := tx.Update(ctx, "creative_node_executions", "state=$2,apply_state=$3,error=$4,execution_epoch=execution_epoch+1,lease_until=NULL,next_step_at=NULL", "id=$5", state, apply, message, e.ID); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "creative_nodes", "active_execution_id=NULL,status_revision=status_revision+1", "id=$2 AND active_execution_id=$3", e.NodeID, e.ID); err != nil {
		return err
	}
	return advanceCanvas(ctx, tx, canvas, false)
}
func (s *ExecutionService) fail(ctx context.Context, scope store.AccountScope, target ExecutionTarget, epoch int64, cause error) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
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
		if e.Epoch != epoch || (e.State != "running" && e.State != "reconciling") {
			return nil
		}
		return finishExecution(ctx, tx, canvas, e, "failed", "discarded", cause.Error())
	})
}
func CancelNodeExecution(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.cancel_execution", c, func(v ExecutionTarget) error {
		if v.CanvasID == "" || v.ExecutionID == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v ExecutionTarget) (creativeops.Outcome, error) {
		canvas, err := lockCanvas(ctx, tx, v.CanvasID, false)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		e, err := readExecution(ctx, tx, v)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		resultRevision := canvas.Revision
		if e.ApplyState == "pending" {
			resultRevision++
			if err = finishExecution(ctx, tx, canvas, e, "cancelled", "discarded", ""); err != nil {
				return creativeops.Outcome{}, err
			}
			e.State = "cancelled"
			e.ApplyState = "discarded"
		}
		return outcome(200, "node_execution", e.ID, resultRevision, e)
	})
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
