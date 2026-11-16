package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
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

// stepArgs reads the arguments a tool step was called with.
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
	return string(call.Arguments), nil
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
