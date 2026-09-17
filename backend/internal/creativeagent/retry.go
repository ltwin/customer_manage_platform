package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var ErrRecoveryEvidence = errors.New("creative step cannot be safely recovered")

// RetryRun restores recorded read-only resource steps, never a fresh Query.
// Model requests require Gateway's original retry permit; terminal requests
// cannot get a new billing identity through this endpoint. Canvas effects arrive
// with FND-08's operation-receipt adapters, not through a generic replay switch.
func (s *Service) RetryRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var in struct {
		RunID     string   `json:"run_id"`
		StepIDs   []string `json:"step_ids"`
		ConsentID string   `json:"egress_consent_id"`
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.retry_run", Capability: "agent_start", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.RunID == "" || in.ConsentID == "" || len(in.StepIDs) != 1 {
			return creativeops.ErrValidation
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		var source controlledRun
		peek, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", in.RunID))
		if errors.Is(err, store.ErrNoRows) {
			return creativeops.Outcome{}, ErrNotFound
		}
		if err != nil {
			return creativeops.Outcome{}, err
		}
		id, err := creativeops.NewResourceID("ccrn")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		token, err := randomClaimToken()
		if err != nil {
			return creativeops.Outcome{}, err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = acquireSlotInTx(ctx, tx, id, token, now); err != nil {
			return creativeops.Outcome{}, err
		}
		source.requests, err = s.gateway.LockCallerGroupInTx(ctx, tx, callerService, in.RunID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		model, err := s.models.Model(peek.ModelKey)
		if err != nil {
			return creativeops.Outcome{}, translateGateway(err)
		}
		reservation, err := s.reserveInTx(ctx, tx, id, model, now.Add(s.limits.RunDuration()))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		source.Run, err = scanRun(tx.QueryRowForUpdate(ctx, "creative_agent_runs", runColumns, "id=$2", in.RunID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !terminalRun(source.State) || source.FinishedAt == nil || !now.Before(source.FinishedAt.Add(contextRetention)) || source.unsettled() {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		var kind, state, hash string
		var keyValue *string
		var raw, output []byte
		var original *string
		err = tx.QueryRow(ctx, "creative_agent_steps", "kind,state,tool_key,input_hash,input,output,retry_of_step_id", "id=$2 AND run_id=$3", in.StepIDs[0], source.ID).Scan(&kind, &state, &keyValue, &hash, &raw, &output, &original)
		if errors.Is(err, store.ErrNoRows) {
			return creativeops.Outcome{}, ErrNotFound
		}
		if err != nil {
			return creativeops.Outcome{}, err
		}
		key := ""
		if keyValue != nil {
			key = *keyValue
		}
		// These registered tools are pure reads of a fixed Skill package. ReadRunResult
		// contains a source-run-local item and is not silently rebound to another run.
		if kind != "tool" || state != "failed" || len(output) != 0 || key != readSkillTool {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		root := in.StepIDs[0]
		if original != nil {
			root = *original
		}
		exists, err := tx.Exists(ctx, "creative_agent_steps", "id=$2", root)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !exists {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		recovered, err := tx.Exists(ctx, "creative_agent_steps", "retry_of_step_id=$2 AND state='succeeded'", root)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if recovered {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		var snapshot []byte
		if err = tx.QueryRow(ctx, "creative_agent_runs", "skill_snapshot", "id=$2", source.ID).Scan(&snapshot); err != nil {
			return creativeops.Outcome{}, err
		}
		var skill creativeskill.Snapshot
		if len(snapshot) == 0 || json.Unmarshal(snapshot, &skill) != nil {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		var call llmgateway.ToolCall
		if json.Unmarshal(raw, &call) != nil || call.Name != readSkillTool {
			return creativeops.Outcome{}, ErrRecoveryEvidence
		}
		consent, err := readConsentForUpdate(ctx, tx, in.ConsentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = coversRun(consent, source.ConversationID, model.VendorKey, nil); err != nil {
			return creativeops.Outcome{}, err
		}
		body := Body{Blocks: []Block{{Type: "text", Text: "恢复已记录的 Skill 资源读取步骤；不重新规划原任务。"}}}
		message, err := s.appendMessageInTx(ctx, tx, source.ConversationID, newMessage{Role: "user", Status: "complete", RunID: id, Body: body})
		if err != nil {
			return creativeops.Outcome{}, err
		}
		run, err := s.insertRunInTx(ctx, tx, runInsert{ID: id, ConversationID: source.ConversationID, CanvasID: source.CanvasID, TriggerMessageID: message.ID, ConsentID: consent.ID, Model: model, Skill: &ResolvedSkill{Snapshot: skill}, ClaimToken: token, ReservationID: reservation.ID, Deadline: now.Add(s.limits.RunDuration()), Now: now})
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = s.freezeInputsInTx(ctx, tx, id, nil, body.Blocks); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = tx.Insert(ctx, "creative_run_skill_refs", []string{"run_id", "segment_ordinal", "skill_id", "skill_version_id", "skill_owner_account_id", "digest"}, id, 0, skill.SkillID, skill.ID, skill.OwnerAccountID, skill.Digest); err != nil {
			return creativeops.Outcome{}, err
		}
		summary, _ := json.Marshal(map[string]any{"source_run_id": source.ID, "step_ids": in.StepIDs, "restores": "read_skill_resource"})
		if _, err = tx.Update(ctx, "creative_agent_runs", "source_run_id=$3,source_summary=$4,next_step_ordinal=3", "id=$2", id, source.ID, summary); err != nil {
			return creativeops.Outcome{}, err
		}
		parent, err := creativeops.NewResourceID("ccst")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		step, err := creativeops.NewResourceID("ccst")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = tx.Insert(ctx, "creative_agent_steps", []string{"id", "run_id", "ordinal", "kind", "state", "execution_epoch", "input_hash", "input"}, parent, id, 1, "check", "succeeded", 1, digestBytes(summary), summary); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = tx.Insert(ctx, "creative_agent_steps", []string{"id", "run_id", "ordinal", "kind", "state", "execution_epoch", "tool_key", "tool_version", "parent_model_step_id", "tool_call_index", "input_hash", "input", "retry_of_step_id"}, step, id, 2, "tool", "prepared", 1, key, 1, parent, 0, hash, raw, root); err != nil {
			return creativeops.Outcome{}, err
		}
		c := controlledRun{Run: run, token: token, epoch: 1}
		if err = s.enqueueControlledRun(ctx, tx, c, now); err != nil {
			return creativeops.Outcome{}, err
		}
		run, err = scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", id))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return runOutcome(run, 202)
	}}, command)
}

func (s *Service) executeRecordedRetry(ctx context.Context, scope store.AccountScope, held claim) (turnOutcome, error) {
	e, err := s.newRunExecution(ctx, scope, held)
	if err != nil {
		return turnOutcome{}, err
	}
	var id, parent string
	var raw []byte
	var index int
	err = scope.QueryRow(ctx, "creative_agent_steps", "id,parent_model_step_id,tool_call_index,input", "run_id=$2 AND retry_of_step_id IS NOT NULL", held.RunID).Scan(&id, &parent, &index, &raw)
	if err != nil {
		return turnOutcome{}, err
	}
	var call llmgateway.ToolCall
	if err = json.Unmarshal(raw, &call); err != nil {
		return turnOutcome{}, err
	}
	if call.Name != readSkillTool {
		return turnOutcome{}, ErrRecoveryEvidence
	}
	e.runtime.current.set(parent, "")
	result, err := e.runtime.callTool(ctx, call.Name, runtimeCallID(parent, index), string(call.Arguments), func() (string, error) { return e.runtime.readSkillResource(ctx, string(call.Arguments)) })
	if err != nil {
		return turnOutcome{StepID: id}, err
	}
	delivered := false
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := s.admitDispatchInTx(ctx, tx, held, nil); err != nil {
			return err
		}
		var state string
		if err := tx.QueryRow(ctx, "creative_agent_steps", "state", "id=$2", id).Scan(&state); err != nil {
			return err
		}
		if state != "succeeded" {
			return ErrRecoveryEvidence
		}
		exists, err := tx.Exists(ctx, "creative_agent_messages", "run_id=$2 AND source_step_id=$3", held.RunID, id)
		if err != nil {
			return err
		}
		if exists {
			delivered = true
			return nil
		}
		text := "已恢复原步骤的资源读取。\n" + strings.TrimSpace(result)
		if len(text) > s.limits.MessageBodyBytes {
			text = "已恢复原步骤的资源读取；完整结果保留在运行记录中。"
		}
		_, err = s.appendMessageInTx(ctx, tx, held.ConversationID, newMessage{Role: "assistant", Status: "complete", RunID: held.RunID, SourceStepID: id, Body: Body{Blocks: []Block{{Type: "text", Text: text}}}})
		delivered = err == nil
		return err
	})
	return turnOutcome{StepID: id, Delivered: delivered}, err
}
