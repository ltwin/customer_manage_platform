package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (s *Service) waitForInput(ctx context.Context, tx store.TxAccountScope, c controlledRun, reason string, now time.Time) error {
	token, err := randomClaimToken()
	if err != nil {
		return err
	}
	if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=$3,wait_reason=$4", "id=$2", c.ID, token, reason); err != nil {
		return err
	}
	if err = s.moveControlledRun(ctx, tx, c, RunWaitingInput, "", now); err != nil {
		return err
	}
	return releaseSlotInTx(ctx, tx, c.ID, c.token, now)
}

// Supplements are photographer messages under the original consent. Model,
// Skill, deadline and existing input batches remain fixed.
func (s *Service) SupplementRun(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	var in struct {
		RunID        string               `json:"run_id"`
		Revision     creativeops.Revision `json:"expected_revision"`
		WaitingToken string               `json:"waiting_token"`
		Text         string               `json:"text"`
	}
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "agent.supplement_run", Capability: "agent_start", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.RunID == "" || in.WaitingToken == "" || in.Revision < 1 || strings.TrimSpace(in.Text) == "" {
			return creativeops.ErrValidation
		}
		if len(in.Text) > s.limits.MessageBodyBytes {
			return ErrLimit
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		c, err := s.lockControlledRun(ctx, tx, in.RunID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if c.State != RunWaitingInput || c.cancelled != nil {
			return creativeops.Outcome{}, ErrRunState
		}
		if c.Revision != in.Revision || c.WaitingToken != in.WaitingToken {
			return creativeops.Outcome{}, ErrRevisionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !now.Before(c.DeadlineAt) {
			return creativeops.Outcome{}, creativeops.ErrExpired
		}
		var batches int
		rows, err := tx.QueryPage(ctx, "creative_run_inputs", "batch_ordinal", "run_id=$2", []store.OrderBy{{Column: "batch_ordinal", Desc: true}}, 1, 0, c.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if rows.Next() {
			err = rows.Scan(&batches)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if rowErr != nil {
			return creativeops.Outcome{}, rowErr
		}
		if batches >= s.limits.ToolCalls {
			return creativeops.Outcome{}, ErrLimit
		}
		model, err := s.models.Model(c.ModelKey)
		if err != nil {
			return creativeops.Outcome{}, translateGateway(err)
		}
		consent, err := readConsentForUpdate(ctx, tx, c.EgressConsentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err = coversRun(consent, c.ConversationID, model.VendorKey, nil); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = acquireSlotInTx(ctx, tx, c.ID, c.token, now); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = s.appendMessageInTx(ctx, tx, c.ConversationID, newMessage{Role: "user", Status: "complete", RunID: c.ID, Body: Body{Blocks: []Block{{Type: "text", Text: in.Text}}}}); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = tx.Insert(ctx, "creative_run_inputs", []string{"run_id", "batch_ordinal", "ordinal", "input_role", "read_level", "body"}, c.ID, batches+1, 0, "instruction", "text", in.Text); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err = tx.Update(ctx, "creative_agent_runs", "waiting_token=NULL,wait_reason=NULL", "id=$2", c.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if err = s.enqueueControlledRun(ctx, tx, c, now); err != nil {
			return creativeops.Outcome{}, err
		}
		run, err := scanRun(tx.QueryRow(ctx, "creative_agent_runs", runColumns, "id=$2", c.ID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return runOutcome(run, 202)
	}}, command)
}

// Checkpoints before a supplement remain replayable. Message metadata marks
// which immutable batches are already in Eino state; it grants no authority.
func (h *runtimeHandler) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	seen := map[string]bool{}
	for _, message := range state.Messages {
		if key, ok := message.Extra["creative_supplement"].(string); ok {
			seen[key] = true
		}
	}
	rows, err := h.scope.QueryPage(ctx, "creative_run_inputs", "batch_ordinal,body", "run_id=$2 AND batch_ordinal>0", []store.OrderBy{{Column: "batch_ordinal"}, {Column: "ordinal"}}, h.service.limits.ToolCalls+1, 0, h.held.RunID)
	if err != nil {
		return ctx, state, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > h.service.limits.ToolCalls {
			return ctx, state, ErrLimit
		}
		var batch int
		var body string
		if err = rows.Scan(&batch, &body); err != nil {
			return ctx, state, err
		}
		key := strconv.Itoa(batch)
		if !seen[key] {
			m := schema.UserMessage(body)
			m.Extra = map[string]any{"creative_supplement": key}
			state.Messages = append(state.Messages, m)
		}
	}
	if err = rows.Err(); err != nil && !errors.Is(err, store.ErrNoRows) {
		return ctx, state, err
	}
	return ctx, state, nil
}
