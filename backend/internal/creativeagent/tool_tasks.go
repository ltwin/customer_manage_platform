package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// toolTaskNamespace derives a task handle from its tool step, so a replayed
// call finds the task it already has (data: 助手耗时工具任务与完成通知).
var toolTaskNamespace = uuid.MustParse("6f1d7c2e-3b9a-4d5e-8f60-71a2b3c4d5e6")

// toolTaskID is the stable handle of the task behind a tool step.
func toolTaskID(stepID string) string {
	return "cctask_" + uuid.NewSHA1(toolTaskNamespace, []byte(stepID)).String()
}

// newToolTask is what a tool registers when it accepts long-running work.
type newToolTask struct {
	StepID   string
	ToolKey  string
	SourceID string
	Mode     string
	// Due marks the task for the finisher at once, which covers a source
	// that changed before the task row existed.
	Due bool
}

// registerToolTaskInTx records the task behind a tool step in the
// transaction that created or replayed its source. A replay keeps the row it
// already has.
func (h *runtimeHandler) registerToolTaskInTx(ctx context.Context, tx store.TxAccountScope, in newToolTask) error {
	var canvasID string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "canvas_id", "id=$2", h.held.RunID).Scan(&canvasID); err != nil {
		return err
	}
	var skill any
	if h.held.Skill != nil {
		skill = h.held.Skill.ID
	}
	var due any
	if in.Due {
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		due = now
	}
	var id string
	err := tx.InsertOnConflictDoNothingReturning(ctx, "creative_agent_tool_tasks",
		[]string{"id", "conversation_id", "canvas_id", "source_run_id", "tool_step_id", "tool_key", "source_id", "mode", "model_key", "skill_version_id", "next_check_at"},
		[]string{"account_id", "tool_step_id"}, []string{"id"},
		toolTaskID(in.StepID), h.held.ConversationID, canvasID, h.held.RunID, in.StepID, in.ToolKey, in.SourceID, in.Mode, h.held.Model.ModelKey, skill, due).Scan(&id)
	if errors.Is(err, store.ErrNoRows) {
		return nil
	}
	return err
}

// sceneToolKey is the tool that creates a job of kind.
func sceneToolKey(kind creativescene.JobKind) (string, error) {
	switch kind {
	case creativescene.KindBakeModel:
		return "write_model", nil
	case creativescene.KindRenderView:
		return "render_view", nil
	case creativescene.KindExportShots:
		return "export_shots", nil
	}
	return "", fmt.Errorf("creative agent: no tool for scene job kind %q", kind)
}

// sceneTaskState is the task state a finished job ends its task in.
func sceneTaskState(jobState string) string {
	switch jobState {
	case creativescene.JobSucceeded:
		return taskSucceeded
	case creativescene.JobExpired:
		return taskExpired
	case creativescene.JobCancelled:
		return taskCancelled
	}
	return taskFailed
}

const (
	backgroundParamDesc = "true 时放到后台运行：立即返回 task_handle，不等结果，可以接着做别的；完成后结果会以系统通知送来，届时再如实告诉创作者。省略为 false：等到结果再返回"
	backgroundToolNote  = "耗时较长、不需要马上看结果时，可以传 run_in_background:true 放到后台；放到后台后不要为了等结果重复调用或查询。"
)

// offerBackground adds run_in_background to a long-running tool's
// definition (generate_node, compress_image, write_model, export_shots;
// render_view is foreground only, review R9), unless the run keeps the definitions of an older checkpoint.
func (h *runtimeHandler) offerBackground(desc string, params map[string]*schema.ParameterInfo, notes ...string) string {
	// A definition read without a run (tests, catalogs) is the current one.
	if h != nil && h.legacyToolSchemas {
		return desc
	}
	params["run_in_background"] = &schema.ParameterInfo{Type: schema.Boolean, Desc: backgroundParamDesc}
	return desc + backgroundToolNote + strings.Join(notes, "")
}

// splitBackground takes run_in_background off a call's arguments: it must be
// a boolean when present. Arguments that are not a JSON object are left for
// the tool's own parser to refuse.
func splitBackground(raw string) (string, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return raw, false, nil
	}
	value, ok := fields["run_in_background"]
	if !ok {
		return raw, false, nil
	}
	var background bool
	switch string(value) {
	case "true":
		background = true
	case "false":
	default:
		return raw, false, errToolArguments
	}
	delete(fields, "run_in_background")
	out, err := json.Marshal(fields)
	if err != nil {
		return raw, false, err
	}
	return string(out), background, nil
}

// backgroundArgument is splitBackground for this run: a run on the old
// definitions was never offered the field and refuses it like any unknown
// argument.
func (h *runtimeHandler) backgroundArgument(raw string) (string, bool, error) {
	args, background, err := splitBackground(raw)
	if err == nil && h.legacyToolSchemas && args != raw {
		return raw, false, errToolArguments
	}
	return args, background, err
}

// acceptBackground records a background task behind the run's admission and
// answers with its handle; the run goes on to its next model call. The task
// is due at once, so a source that already moved is not missed.
func (h *runtimeHandler) acceptBackground(ctx context.Context, task newToolTask) (any, error) {
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		return h.registerToolTaskInTx(ctx, tx, task)
	})
	if err != nil {
		return nil, err
	}
	return acceptedInBackground(task.StepID), nil
}

// acceptedInBackground is the answer of a call accepted in the background.
// A replay of the call answers the same.
func acceptedInBackground(step string) any {
	return struct {
		TaskHandle string `json:"task_handle"`
		State      string `json:"state"`
	}{toolTaskID(step), taskRunning}
}

// stepArgs reads the arguments a tool step was called with, without
// run_in_background.
func stepArgs(ctx context.Context, tx store.TxAccountScope, stepID string) (string, error) {
	var input []byte
	if err := tx.QueryRow(ctx, "creative_agent_steps", "input", "id=$2 AND kind='tool'", stepID).Scan(&input); err != nil {
		return "", err
	}
	var call struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(input, &call); err != nil {
		return "", err
	}
	args, _, err := splitBackground(string(call.Arguments))
	return args, err
}

// finishSceneTaskInTx ends the task behind a finished job with the tool
// result its call gets, when there is a task and it is still running. result
// is that answer when the caller already built it, empty otherwise. The
// caller holds the job; the task row is locked last.
func finishSceneTaskInTx(ctx context.Context, tx store.TxAccountScope, job creativescene.Job, res creativescene.JobResult, result string, failed bool) error {
	tool, err := sceneToolKey(job.Kind)
	if err != nil {
		return err
	}
	task, err := scanToolTask(tx.QueryRowForUpdate(ctx, "creative_agent_tool_tasks", toolTaskColumns, "tool_key=$2 AND source_id=$3", tool, job.ID))
	if errors.Is(err, store.ErrNoRows) {
		// A job from before tool tasks: the old path is all it has.
		return nil
	}
	if err != nil || task.State != taskRunning {
		return err
	}
	if result == "" {
		args, err := stepArgs(ctx, tx, task.ToolStepID)
		if err != nil {
			return err
		}
		if result, failed, err = sceneToolResult(ctx, tx, job, res, args); err != nil {
			return err
		}
	}
	state := sceneTaskState(job.State)
	if state == taskSucceeded && failed {
		state = taskFailed
	}
	return finishTaskRow(ctx, tx, task, toolTaskOutcome{Done: true, State: state, Result: result, Failed: failed})
}

// sceneTaskSource finishes scene tasks the scene completion did not: a job
// cancelled with its run, expired, or one in the background.
type sceneTaskSource struct{}

func (sceneTaskSource) settle(ctx context.Context, tx store.TxAccountScope, task toolTask) (toolTaskOutcome, error) {
	jobs, err := creativescene.LockJobs(ctx, tx, []string{task.SourceID})
	if err != nil {
		return toolTaskOutcome{}, err
	}
	if len(jobs) == 0 {
		return toolTaskOutcome{Done: true, State: taskFailed, Failed: true,
			Result: `{"success":false,"code":"creative_tool_failed","message":"场景任务已不存在"}`}, nil
	}
	job := jobs[0]
	if !creativescene.Terminal(job.State) {
		return toolTaskOutcome{RecheckAt: job.Deadline}, nil
	}
	res, err := creativescene.ResultOf(job)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	args, err := stepArgs(ctx, tx, task.ToolStepID)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	result, failed, err := sceneToolResult(ctx, tx, job, res, args)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	state := sceneTaskState(job.State)
	if state == taskSucceeded && failed {
		state = taskFailed
	}
	return toolTaskOutcome{Done: true, State: state, Result: result, Failed: failed}, nil
}

// ToolTask is a long-running tool's task as the panel sees it
// (contracts: CreativeAgentToolTask).
type ToolTask struct {
	Handle     string               `json:"handle"`
	Mode       string               `json:"mode"`
	State      string               `json:"state"`
	Revision   creativeops.Revision `json:"revision"`
	Result     json.RawMessage      `json:"result,omitempty"`
	FinishedAt *time.Time           `json:"finished_at,omitempty"`
	ToolKey    string               `json:"tool_key"`
	RunID      string               `json:"run_id"`
	ToolStepID string               `json:"tool_step_id"`
}

func readToolTaskInTx(ctx context.Context, tx store.TxAccountScope, id string) (ToolTask, error) {
	var t ToolTask
	var revision int64
	var result []byte
	err := tx.QueryRow(ctx, "creative_agent_tool_tasks", "id,mode,state,revision,result,finished_at,tool_key,source_run_id,tool_step_id", "id=$2", id).
		Scan(&t.Handle, &t.Mode, &t.State, &revision, &result, &t.FinishedAt, &t.ToolKey, &t.RunID, &t.ToolStepID)
	if errors.Is(err, store.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Revision, t.Result = creativeops.Revision(revision), result
	return t, err
}

func toolTaskReceipt(ctx context.Context, tx store.TxAccountScope, id string) (creativeops.Outcome, error) {
	t, err := readToolTaskInTx(ctx, tx, id)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	return creativeops.Outcome{HTTPStatus: 200, Response: raw, ResultKind: "agent_tool_task", ResultID: &t.Handle, ResultRevision: &t.Revision}, nil
}

// peekToolTask reads a task by its handle without locking it: the task row
// is locked last (design: 锁顺序).
func peekToolTask(ctx context.Context, tx store.TxAccountScope, handle string) (toolTask, error) {
	task, err := scanToolTask(tx.QueryRow(ctx, "creative_agent_tool_tasks", toolTaskColumns, "id=$2", handle))
	if errors.Is(err, store.ErrNoRows) {
		return task, ErrNotFound
	}
	return task, err
}

func emptyPayload(raw json.RawMessage) error {
	var payload struct{}
	return creativeops.Decode(raw, &payload)
}

// DetachToolTask ends the assistant's foreground wait on a long-running
// tool: the call answers "moved to the background" and the run goes on; the
// task's result later arrives as a notice. A source that already finished
// answers the call with its real result instead. A stopped run refuses.
// Locks: operation, project, run (admitRun), source, task.
func (s *Service) DetachToolTask(ctx context.Context, scope store.AccountScope, handle string, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.detach_tool_task", Capability: "creative_read", Validate: emptyPayload,
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			task, err := peekToolTask(ctx, tx, handle)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if task.State != taskRunning || task.Mode != modeForeground {
				return toolTaskReceipt(ctx, tx, handle)
			}
			g, err := sceneWaiter{s: s}.admitRun(ctx, tx, task.SourceRunID)
			if errors.Is(err, creativescene.ErrRunGate) {
				if errors.Is(err, store.ErrCreativeAccessDenied) {
					return creativeops.Outcome{}, store.ErrCreativeAccessDenied
				}
				return creativeops.Outcome{}, ErrRunState
			}
			if err != nil {
				return creativeops.Outcome{}, err
			}
			source, ok := s.toolSources[task.ToolKey]
			if !ok {
				return creativeops.Outcome{}, fmt.Errorf("creative agent: no source for tool %s", task.ToolKey)
			}
			outcome, err := source.settle(ctx, tx, task)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			row, err := scanToolTask(tx.QueryRowForUpdate(ctx, "creative_agent_tool_tasks", toolTaskColumns, "id=$2", handle))
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if row.State != taskRunning || row.Mode != modeForeground {
				return toolTaskReceipt(ctx, tx, handle)
			}
			if outcome.Done {
				// Finished first: the call gets its real answer.
				if err := finishTaskRow(ctx, tx, row, outcome); err != nil {
					return creativeops.Outcome{}, err
				}
				if err := s.publishForegroundInTx(ctx, tx, g, row, outcome); err != nil {
					return creativeops.Outcome{}, err
				}
				return toolTaskReceipt(ctx, tx, handle)
			}
			if _, err := tx.Update(ctx, "creative_agent_tool_tasks", "mode='background',next_check_at=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp()",
				"id=$2 AND state='running' AND mode='foreground'", handle); err != nil {
				return creativeops.Outcome{}, err
			}
			detached, err := json.Marshal(struct {
				TaskHandle string `json:"task_handle"`
				State      string `json:"state"`
				Detached   bool   `json:"detached"`
			}{handle, taskRunning, true})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err := s.publishForegroundInTx(ctx, tx, g, row, toolTaskOutcome{Result: string(detached)}); err != nil {
				return creativeops.Outcome{}, err
			}
			return toolTaskReceipt(ctx, tx, handle)
		}}, command)
}

// CancelToolTask cancels a background task from its card. A task still in
// the foreground has no cancel (move it to the background first); one that
// already ended keeps its result. Locks: operation, project, source, task.
func (s *Service) CancelToolTask(ctx context.Context, scope store.AccountScope, handle string, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.cancel_tool_task", Capability: "manual_write", Validate: emptyPayload,
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			task, err := peekToolTask(ctx, tx, handle)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if task.State != taskRunning {
				return toolTaskReceipt(ctx, tx, handle)
			}
			if task.Mode != modeBackground {
				return creativeops.Outcome{}, ErrRunState
			}
			if err := creativecanvas.LockProjectForWorkInTx(ctx, tx, task.CanvasID); err != nil {
				return creativeops.Outcome{}, err
			}
			outcome, err := s.cancelSourceInTx(ctx, tx, task)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			row, err := scanToolTask(tx.QueryRowForUpdate(ctx, "creative_agent_tool_tasks", toolTaskColumns, "id=$2", handle))
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if row.State != taskRunning {
				return toolTaskReceipt(ctx, tx, handle)
			}
			if _, err := tx.Update(ctx, "creative_agent_tool_tasks", "stop_requested_at=clock_timestamp()", "id=$2", handle); err != nil {
				return creativeops.Outcome{}, err
			}
			if err := s.finishTaskInTx(ctx, tx, row, outcome); err != nil {
				return creativeops.Outcome{}, err
			}
			return toolTaskReceipt(ctx, tx, handle)
		}}, command)
}

// cancelSourceInTx cancels a background task's source in its domain and
// reads where it ended: cancelled, or the result it reached first.
func (s *Service) cancelSourceInTx(ctx context.Context, tx store.TxAccountScope, task toolTask) (toolTaskOutcome, error) {
	args, err := stepArgs(ctx, tx, task.ToolStepID)
	if err != nil {
		return toolTaskOutcome{}, err
	}
	switch task.ToolKey {
	case generateNodeTool:
		if s.generator == nil {
			return toolTaskOutcome{}, errors.New("creative agent: generation is not bound")
		}
		if err := s.generator.CancelAgentGenerationInTx(ctx, tx, task.CanvasID, task.SourceID); err != nil {
			return toolTaskOutcome{}, err
		}
		var node struct {
			NodeID string `json:"node_id"`
		}
		if err := json.Unmarshal([]byte(args), &node); err != nil {
			return toolTaskOutcome{}, err
		}
		result, done, err := s.generator.AgentGenerationInTx(ctx, tx, task.CanvasID, node.NodeID, task.SourceID)
		if err != nil {
			return toolTaskOutcome{}, err
		}
		if !done {
			result.State, result.ApplyState = "cancelled", "discarded"
		}
		return generationOutcome(result)
	case compressImageTool:
		if s.compressor == nil {
			return toolTaskOutcome{}, errors.New("creative agent: image editing is not bound")
		}
		in, err := compressInput(task.CanvasID, task.SourceRunID+"/"+task.ToolStepID, args)
		if err != nil {
			return toolTaskOutcome{}, err
		}
		result, err := s.compressor.DiscardAgentCompressInTx(ctx, tx, in, task.SourceID)
		if err != nil {
			return toolTaskOutcome{}, err
		}
		return compressOutcome(result, false)
	case writeModelTool, exportShotsTool:
		if err := creativescene.CancelJobInTx(ctx, tx, task.SourceID); err != nil {
			return toolTaskOutcome{}, err
		}
		return sceneTaskSource{}.settle(ctx, tx, task)
	}
	return toolTaskOutcome{}, fmt.Errorf("creative agent: %s cannot run in the background", task.ToolKey)
}
