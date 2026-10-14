package creativeagent

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const libraryCallTable = "creative_agent_library_calls"

var errLibraryFenced = errors.New("library call no longer executing")

type libraryPhaseState struct {
	Message         string               `json:"message,omitempty"`
	CurrentRevision creativeops.Revision `json:"current_revision,omitempty"`
	Status          string               `json:"status"`
	Attempt         int                  `json:"attempt"`
	Command         *creativeops.Command `json:"command,omitempty"`
	Result          json.RawMessage      `json:"result,omitempty"`
}
type libraryCall struct {
	ID, Step, Run, Conversation, Tool, Hash, State, CursorKey string
	Created, Retained                                         time.Time
	Plan                                                      libraryPlan
	Progress                                                  [][]libraryPhaseState
	Summary                                                   json.RawMessage
	Details                                                   []map[string]any
}

const libraryCallColumns = "id,tool_step_id,run_id,conversation_id,tool_name,arguments_hash,state,cursor_key,created_at,retained_until,plan,progress,summary,details"

func scanLibraryCall(row store.Row) (libraryCall, error) {
	var c libraryCall
	var plan, progress, details []byte
	err := row.Scan(&c.ID, &c.Step, &c.Run, &c.Conversation, &c.Tool, &c.Hash, &c.State, &c.CursorKey, &c.Created, &c.Retained, &plan, &progress, &c.Summary, &details)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(plan, &c.Plan); err != nil {
		return c, err
	}
	if err = json.Unmarshal(progress, &c.Progress); err != nil {
		return c, err
	}
	err = json.Unmarshal(details, &c.Details)
	return c, err
}
func (h *runtimeHandler) loadLibraryCall(ctx context.Context, id string) (libraryCall, error) {
	return scanLibraryCall(h.scope.QueryRow(ctx, libraryCallTable, libraryCallColumns, "id=$2 AND conversation_id=$3", id, h.held.ConversationID))
}
func (h *runtimeHandler) changeLibraryCall(ctx context.Context, id string, change func(*libraryCall) error) (libraryCall, error) {
	var c libraryCall
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		c, err = h.changeLibraryCallInTx(ctx, tx, id, change)
		return err
	})
	return c, err
}
func (h *runtimeHandler) changeLibraryCallInTx(ctx context.Context, tx store.TxAccountScope, id string, change func(*libraryCall) error) (libraryCall, error) {
	c, err := scanLibraryCall(tx.QueryRowForUpdate(ctx, libraryCallTable, libraryCallColumns, "id=$2 AND conversation_id=$3", id, h.held.ConversationID))
	if err != nil {
		return c, err
	}
	if !time.Now().Before(c.Retained) {
		return c, creativeops.ErrExpired
	}
	if c.State == "complete" {
		return c, nil
	}
	if err := change(&c); err != nil {
		return c, err
	}
	progress, err := json.Marshal(c.Progress)
	if err != nil {
		return c, err
	}
	details, err := json.Marshal(c.Details)
	if err != nil {
		return c, err
	}
	_, err = tx.Update(ctx, libraryCallTable, "state=$3,progress=$4,summary=$5,details=$6", "id=$2", id, c.State, progress, c.Summary, details)
	return c, err
}

func (h *runtimeHandler) startLibraryCall(ctx context.Context, name, raw string) (libraryCall, error) {
	step := toolStepFrom(ctx)
	if step == "" {
		return libraryCall{}, ErrRunState
	}
	id := toolOperation(h.held.RunID + "/" + step + "/library-result")
	hash := digestBytes([]byte(raw))
	c, err := h.loadLibraryCall(ctx, id)
	if err == nil {
		if c.Hash != hash || c.Tool != name || c.Run != h.held.RunID {
			return c, creativeops.ErrConflict
		}
		if !time.Now().Before(c.Retained) {
			return c, creativeops.ErrExpired
		}
		return c, nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return c, err
	}
	plan, err := h.prepareLibraryPlan(ctx, name, raw)
	if err != nil {
		return c, err
	}
	c = libraryCall{ID: id, Step: step, Run: h.held.RunID, Conversation: h.held.ConversationID, Tool: name, Hash: hash, State: "executing", Created: time.Now().UTC(), Plan: plan, Details: []map[string]any{}}
	c.Retained = c.Created.Add(contextRetention)
	for _, item := range plan.Items {
		states := []libraryPhaseState{}
		for range item.Phases {
			states = append(states, libraryPhaseState{Status: "pending"})
		}
		c.Progress = append(c.Progress, states)
	}
	planRaw, err := json.Marshal(plan)
	if err != nil {
		return c, err
	}
	// Plan text can occur in the plan, frozen command and before/after detail;
	// a fourth copy reserves JSONB spacing and record envelopes. LibraryResult
	// receipts are bounded scalar facts (including at most one 80-rune tag),
	// so reserve 8 KiB twice per phase. Saving can return multiple tag facts and
	// legacy descriptions, and reserves the executor's full 64 KiB twice.
	reserved := len(planRaw)*4 + len(plan.Items)*1024
	for _, item := range plan.Items {
		for _, p := range item.Phases {
			receiptBudget := 8 << 10
			if p.Name == saveLibraryTool {
				receiptBudget = 64 << 10
			}
			reserved += 2 * receiptBudget
		}
	}
	if reserved > 8<<20 {
		return c, refuseTool("整理计划超过保存预算，请缩小批次")
	}
	worst := librarySummary(c, true)
	encoded, err := json.Marshal(worst)
	if err != nil {
		return c, err
	}
	if len(encoded) > h.service.limits.ToolResultBytes {
		return c, refuseTool("整理摘要超过返回预算，请缩小批次")
	}
	progress, err := json.Marshal(c.Progress)
	if err != nil {
		return c, err
	}
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeOperation(ctx, id); err != nil {
			return err
		}
		existing, e := scanLibraryCall(tx.QueryRow(ctx, libraryCallTable, libraryCallColumns, "id=$2", id))
		if e == nil {
			if existing.Hash != hash || existing.Tool != name {
				return creativeops.ErrConflict
			}
			c = existing
			return nil
		}
		if !errors.Is(e, store.ErrNoRows) {
			return e
		}
		return tx.Insert(ctx, libraryCallTable, []string{"id", "tool_step_id", "run_id", "conversation_id", "tool_name", "protocol_version", "arguments_hash", "created_at", "retained_until", "state", "plan", "progress"}, id, step, c.Run, c.Conversation, name, 1, hash, c.Created, c.Retained, "executing", planRaw, progress)
	})
	if err != nil {
		return c, err
	}
	return h.loadLibraryCall(ctx, id)
}
func librarySummary(c libraryCall, worst bool) map[string]any {
	items := []map[string]any{}
	stopped := false
	for i, item := range c.Plan.Items {
		status := item.Status
		revision := item.Revision
		id := item.ID
		applied := 0
		states := map[string]string{}
		counts := map[string]int{"tags_added": 0, "tags_removed": 0, "groups_added": 0, "groups_removed": 0}
		result := map[string]any{"index": i, "id": id}
		if id == "" {
			result["id"] = nil
		}
		if c.Tool == saveLibraryTool {
			result["node_id"] = id
			result["id"] = nil
		}
		for j, p := range item.Phases {
			s := c.Progress[i][j]
			states[p.Name] = s.Status
			if s.CurrentRevision > 0 {
				result["current_revision"] = s.CurrentRevision
			}
			if s.Status == "applied" {
				applied++
				var body map[string]any
				if json.Unmarshal(s.Result, &body) == nil {
					if v, ok := body["revision"].(string); ok {
						_ = json.Unmarshal([]byte(fmt.Sprintf("%q", v)), &revision)
					}
					for _, k := range []string{"asset_id", "id", "affected_asset_count", "affected_tag_count", "existing", "reused", "content_revision_id"} {
						if v, ok := body[k]; ok {
							result[k] = v
							if k == "asset_id" {
								result["id"] = v
							}
						}
					}
				}
				if p.Name == "organize" {
					for _, change := range item.Changes {
						k, _ := change["kind"].(string)
						v, _ := change["change"].(string)
						counts[k+"s_"+v]++
					}
				}
			} else if s.Status != "pending" {
				status = s.Status
			}
		}
		if len(item.Phases) > 0 {
			switch {
			case applied == len(item.Phases):
				status = "applied"
			case applied > 0:
				status = "partial"
			case status == "":
				status = "not_run"
			}
		}
		if status == "conflict" && applied == 0 && item.Status == "" {
			if current, ok := result["current_revision"].(creativeops.Revision); ok {
				revision = current
			} else {
				revision = 0
				result["revision"] = nil
			}
		}
		if status == "partial" {
			if _, ok := result["current_revision"]; !ok {
				result["current_revision"] = nil
			}
		}
		if c.Tool == manageLibraryTool {
			if stopped {
				status = "not_run"
			}
			if status != "applied" && status != "unchanged" {
				stopped = true
			}
		}
		if worst {
			status = "partial"
			revision = creativeops.Revision(9223372036854775807)
			result["id"] = strings.Repeat("x", 100)
			result["current_revision"] = revision
			result["affected_asset_count"] = int64(9223372036854775807)
			result["affected_tag_count"] = int64(9223372036854775807)
			result["existing"] = false
			result["reused"] = false
			result["asset_id"] = strings.Repeat("x", 100)
			result["content_revision_id"] = strings.Repeat("x", 100)
			for key := range counts {
				counts[key] = 100
			}
		}
		result["status"] = status
		result["stages"] = states
		result["counts"] = counts
		if item.Action != "" {
			result["action"] = item.Action
		}
		if revision > 0 {
			result["revision"] = revision
		}
		if c.Tool != manageLibraryTool && c.Tool != saveLibraryTool {
			result["asset_id"] = item.ID
		}
		items = append(items, result)
	}
	return map[string]any{"result_id": c.ID, "retained_until": c.Retained, "state": c.State, "items": items, "details_available": c.State == "complete", "retention_days": c.Plan.RetentionDays}
}
func buildLibraryDetails(c libraryCall) []map[string]any {
	details := []map[string]any{}
	summary := librarySummary(c, false)
	items, _ := summary["items"].([]map[string]any)
	for i, item := range c.Plan.Items {
		details = append(details, map[string]any{"type": "item_summary", "item_index": i, "value": items[i]})
		for j, p := range item.Phases {
			state := c.Progress[i][j]
			if state.Message != "" {
				details = append(details, map[string]any{"type": "action_result", "item_index": i, "phase": p.Name, "status": state.Status, "message": state.Message})
			}
			if state.Status != "applied" {
				continue
			}
			switch p.Name {
			case "metadata":
				var in creativelibrary.MetadataInput
				if json.Unmarshal(p.Payload, &in) != nil {
					continue
				}
				value := map[string]any{}
				if in.Title != nil {
					value["title_before"] = item.Before["title"]
					value["title_after"] = *in.Title
				}
				if in.Description != nil {
					value["description_before"] = item.Before["description"]
					value["description_after"] = *in.Description
				}
				details = append(details, map[string]any{"type": "metadata_change", "item_index": i, "value": value})
			case "organize":
				for _, change := range item.Changes {
					details = append(details, map[string]any{"type": "relation_change", "item_index": i, "value": change})
				}
			default:
				details = append(details, map[string]any{"type": "action_result", "item_index": i, "value": state.Result})
			}
		}
	}
	return details
}
func finishLibraryCall(c *libraryCall) error {
	for _, item := range c.Progress {
		for _, phase := range item {
			if phase.Status == "pending" {
				return errLibraryFenced
			}
		}
	}
	c.State = "complete"
	c.Details = buildLibraryDetails(*c)
	summary, err := json.Marshal(librarySummary(*c, false))
	c.Summary = summary
	return err
}
func (h *runtimeHandler) libraryGuard(id string, i, j int) creativeops.CommandGuard {
	return func(ctx context.Context, tx store.TxAccountScope, key string, command creativeops.Command) error {
		// Receipt replay precedes this guard. New effects hold the run and
		// consent locks through commit, before journal/library/canvas locks.
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		c, err := scanLibraryCall(tx.QueryRowForUpdate(ctx, libraryCallTable, libraryCallColumns, "id=$2 AND conversation_id=$3", id, h.held.ConversationID))
		if err != nil {
			return err
		}
		if c.State != "executing" || !time.Now().Before(c.Retained) || i >= len(c.Progress) || j >= len(c.Progress[i]) {
			return errLibraryFenced
		}
		state := c.Progress[i][j]
		saved := state.Command
		if state.Status != "pending" || saved == nil || saved.OperationID != command.OperationID || !saved.CreatedAt.Equal(command.CreatedAt) || !sameLibraryPayload(saved.Payload, command.Payload) || libraryOperationKey(c.Plan.Items[i].Phases[j].Name) != key {
			return errLibraryFenced
		}
		return nil
	}
}
func sameLibraryPayload(a, b []byte) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	ar, _ := json.Marshal(av)
	br, _ := json.Marshal(bv)
	return bytes.Equal(ar, br)
}
func libraryOperationKey(name string) string {
	keys := map[string]string{"metadata": "library.asset_metadata", "organize": "library.batch_organize", trashAssetsTool: "library.asset_trash", restoreAssetsTool: "library.asset_restore", saveLibraryTool: "library.create_asset_from_node", "create_group": "library.group_create", "rename_group": "library.group_rename", "move_group": "library.group_move", "delete_group": "library.group_delete", "create_category": "library.category_create", "rename_category": "library.category_edit", "delete_category": "library.category_delete", "create_tag": "library.tag_create", "edit_tag": "library.tag_edit", "delete_tag": "library.tag_delete"}
	return keys[name]
}
func executeLibraryPhase(ctx context.Context, sc store.AccountScope, name string, c creativeops.Command) (creativeops.Receipt, error) {
	commands := map[string]func(context.Context, store.AccountScope, creativeops.Command) (creativeops.Receipt, error){"metadata": creativelibrary.Metadata, "organize": creativelibrary.Organize, trashAssetsTool: creativelibrary.BatchTrash, restoreAssetsTool: creativelibrary.BatchRestore, saveLibraryTool: creativecanvas.SaveNodeToLibrary, "create_group": creativelibrary.CreateGroup, "rename_group": creativelibrary.RenameGroup, "move_group": creativelibrary.MoveGroup, "delete_group": creativelibrary.DeleteGroup, "create_category": creativelibrary.CreateCategory, "rename_category": creativelibrary.EditCategory, "delete_category": creativelibrary.DeleteCategory, "create_tag": creativelibrary.CreateTag, "edit_tag": creativelibrary.EditTag, "delete_tag": creativelibrary.DeleteTag}
	fn, ok := commands[name]
	if !ok {
		return creativeops.Receipt{}, errToolArguments
	}
	return fn(ctx, sc, c)
}
func (h *runtimeHandler) freezeLibraryCommand(ctx context.Context, c libraryCall, i, j int) (libraryCall, error) {
	p := c.Plan.Items[i].Phases[j]
	payload := p.Payload
	if c.Tool == manageLibraryTool {
		settings, err := creativelibrary.GetSettings(ctx, h.scope)
		if err != nil {
			return c, err
		}
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			return c, err
		}
		body["hierarchy_revision"] = settings.HierarchyRevision
		payload, err = json.Marshal(body)
		if err != nil {
			return c, err
		}
	}
	return h.changeLibraryCall(ctx, c.ID, func(current *libraryCall) error {
		if current.State != "executing" {
			return errLibraryFenced
		}
		state := &current.Progress[i][j]
		if state.Status != "pending" || state.Command != nil {
			return nil
		}
		operation := toolOperation(current.Run + "/" + current.Step + fmt.Sprintf("/%d/%s/%d", i, p.Name, state.Attempt))
		state.Command = &creativeops.Command{OperationID: operation, CreatedAt: current.Created, Payload: payload}
		return nil
	})
}

// A result belongs to the exact frozen attempt that produced it. An old
// executor must never mark a newer pending command failed (or not_run).
func (h *runtimeHandler) setLibraryPhase(ctx context.Context, observed libraryCall, i, j int, status string, response json.RawMessage, message string, revision creativeops.Revision) (libraryCall, error) {
	c := observed
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		previous := observed.Progress[i][j]
		if previous.Command != nil {
			// A previous business failure is only an observation. Another
			// executor may have since committed the same attempt. Serialize
			// with execution and prefer its receipt before closing the phase.
			receipt, err := (creativeops.Executor{}).LookupCommandInTx(ctx, tx, libraryOperationKey(observed.Plan.Items[i].Phases[j].Name), *previous.Command)
			if err == nil {
				status, response, message, revision = "applied", receipt.Outcome.Response, "", 0
			} else if !errors.Is(err, creativeops.ErrNotFound) {
				return err
			} else if status == "applied" {
				return errLibraryFenced
			}
		}
		var err error
		c, err = h.changeLibraryCallInTx(ctx, tx, observed.ID, func(c *libraryCall) error {
			state := &c.Progress[i][j]
			if state.Status != "pending" {
				return nil
			}
			if state.Attempt != previous.Attempt || !sameLibraryCommand(state.Command, previous.Command) {
				return errLibraryFenced
			}
			state.Status = status
			state.Result = response
			state.Message = message
			state.CurrentRevision = revision
			return nil
		})
		return err
	})
	return c, err
}

func sameLibraryCommand(a, b *creativeops.Command) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.OperationID == b.OperationID && a.CreatedAt.Equal(b.CreatedAt) && sameLibraryPayload(a.Payload, b.Payload)
}
func (h *runtimeHandler) settleLibraryCall(ctx context.Context, c libraryCall) (libraryCall, error) {
	c, err := h.changeLibraryCall(ctx, c.ID, func(c *libraryCall) error { c.State = "settling"; return nil })
	if err != nil {
		return c, err
	}
	if c.State == "complete" {
		return c, nil
	}
	for i, item := range c.Plan.Items {
		for j := range item.Phases {
			state := c.Progress[i][j]
			if state.Status != "pending" {
				continue
			}
			status := "not_run"
			var response json.RawMessage
			if state.Command != nil {
				wait, cancel := context.WithTimeout(ctx, 5*time.Second)
				r, err := (creativeops.Executor{}).LookupCommandLocked(wait, h.scope, libraryOperationKey(item.Phases[j].Name), *state.Command)
				cancel()
				if err == nil {
					status = "applied"
					response = r.Outcome.Response
				} else if !errors.Is(err, creativeops.ErrNotFound) {
					return c, err
				}
			}
			c, err = h.setLibraryPhase(ctx, c, i, j, status, response, "", 0)
			if err != nil {
				return c, err
			}
		}
	}
	return h.changeLibraryCall(ctx, c.ID, finishLibraryCall)
}
func (h *runtimeHandler) unknownLibraryCall(ctx context.Context, c libraryCall, cause error) (string, error) {
	// If closing cannot be persisted, leave the tool prepared; ordinary unknown
	// output must never seal a step whose future writes are not fenced.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	c, err := h.changeLibraryCall(cleanup, c.ID, func(c *libraryCall) error { c.State = "settling"; return nil })
	if err != nil {
		return "", fmt.Errorf("%w: library closing: %v", errRunInterrupted, err)
	}
	if c.State == "complete" {
		return string(c.Summary), nil
	}
	// A competing executor changed the attempt or finished the call. Fence
	// first, then reconcile the current command under its operation lock.
	if errors.Is(cause, errLibraryFenced) {
		if resolved, reconcileErr := h.settleLibraryCall(cleanup, c); reconcileErr == nil {
			return string(resolved.Summary), nil
		}
	}
	h.service.logger.ErrorContext(ctx, "library call awaiting reconciliation", "result_id", c.ID, "error", cause.Error())
	result := map[string]any{"success": false, "effect": "unknown", "retryable": false, "state": "settling", "result_id": c.ID, "retained_until": c.Retained, "read_tool": readLibraryResultTool, "message": "提交结果待核对；剩余写入已停止。用 read_library_result 核对，不要重发写入。"}
	raw, err := json.Marshal(result)
	return string(raw), err
}
func (h *runtimeHandler) runLibraryWrite(ctx context.Context, name, raw string) (string, error) {
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	c, err := h.startLibraryCall(ctx, name, raw)
	if err != nil {
		return "", asRefusal(err)
	}
	if c.State == "complete" {
		return string(c.Summary), nil
	}
	if c.State == "settling" {
		resolved, err := h.settleLibraryCall(ctx, c)
		if err != nil {
			return h.unknownLibraryCall(ctx, c, err)
		}
		return string(resolved.Summary), nil
	}
	stopped := false
	for i, item := range c.Plan.Items {
		if c.Tool == manageLibraryTool && item.Status != "" {
			stopped = true
		}
		failed := false
		for j := range item.Phases {
			state := c.Progress[i][j]
			if state.Status != "pending" {
				if state.Status != "applied" {
					failed = true
					if c.Tool == manageLibraryTool {
						stopped = true
					}
				}
				continue
			}
			if stopped || failed {
				c, err = h.setLibraryPhase(ctx, c, i, j, "not_run", nil, "", 0)
				if err != nil {
					return h.unknownLibraryCall(ctx, c, err)
				}
				continue
			}
			if state.Command == nil {
				c, err = h.freezeLibraryCommand(ctx, c, i, j)
				if err != nil {
					return h.unknownLibraryCall(ctx, c, err)
				}
				state = c.Progress[i][j]
			}
			if c.State != "executing" {
				return h.unknownLibraryCall(ctx, c, errLibraryFenced)
			}

			// Executor first checks the receipt, including on replay after a write
			// changed the original asset revision. The immutable plan is never rebuilt.
			var receipt creativeops.Receipt
			var runErr error
			c, receipt, runErr = h.runLibraryPhase(ctx, c, i, j)
			status := "applied"
			if runErr != nil {
				status = libraryFailure(runErr)
				if status == "" {
					return h.unknownLibraryCall(ctx, c, runErr)
				}
			}
			var observed creativeops.Revision
			message := ""
			if runErr != nil {
				message = runErr.Error()
				if c.Tool != manageLibraryTool && c.Tool != saveLibraryTool {
					_ = h.scope.QueryRow(ctx, "creative_assets", "revision", "id=$2", item.ID).Scan(&observed) // Best-effort observed revision, never used to retry.
				}
			}
			c, err = h.setLibraryPhase(ctx, c, i, j, status, receipt.Outcome.Response, message, observed)
			if err != nil {
				return h.unknownLibraryCall(ctx, c, err)
			}
			failed = c.Progress[i][j].Status != "applied"
			if c.Tool == manageLibraryTool && failed {
				stopped = true
			}
		}
	}
	c, err = h.changeLibraryCall(ctx, c.ID, finishLibraryCall)
	if err != nil {
		return h.unknownLibraryCall(ctx, c, err)
	}
	return string(c.Summary), nil
}

type libraryResultCursor struct {
	Result string `json:"result"`
	Digest string `json:"digest"`
	Offset int    `json:"offset"`
}

func librarySummaryDigest(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	return digestBytes(canonical), err
}
func resultCursor(c libraryCall, offset int) (string, error) {
	digest, err := librarySummaryDigest(c.Summary)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(libraryResultCursor{Result: c.ID, Digest: digest, Offset: offset})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(c.CursorKey))
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func resultOffset(c libraryCall, encoded string) (int, error) {
	if encoded == "" {
		return 0, nil
	}
	if len(encoded) > 2048 {
		return 0, errToolArguments
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return 0, errToolArguments
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, errToolArguments
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, errToolArguments
	}
	mac := hmac.New(sha256.New, []byte(c.CursorKey))
	_, _ = mac.Write(raw)
	var v libraryResultCursor
	digest, err := librarySummaryDigest(c.Summary)
	if err != nil {
		return 0, err
	}
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(raw, &v) != nil || v.Result != c.ID || v.Digest != digest || v.Offset < 0 || v.Offset > len(c.Details) {
		return 0, errToolArguments
	}
	return v.Offset, nil
}
func (h *runtimeHandler) readLibraryResult(ctx context.Context, raw string) (string, error) {
	var args struct {
		ID     string `json:"result_id"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	if args.Limit == 0 {
		args.Limit = 50
	}
	if !validLibraryID(args.ID) || args.Limit < 1 || args.Limit > 50 {
		return "", errToolArguments
	}
	if err := h.requireLibrary(ctx, true); err != nil {
		return "", err
	}
	c, err := h.loadLibraryCall(ctx, args.ID)
	if errors.Is(err, store.ErrNoRows) {
		return "", refuseTool("result_not_found：本会话没有该整理结果")
	}
	if err != nil {
		return "", err
	}
	if !time.Now().Before(c.Retained) {
		return "", refuseTool("result_expired：完整整理结果已超过 90 天保留窗口")
	}
	if c.State == "executing" {
		var finished *time.Time
		err := h.scope.QueryRow(ctx, "creative_agent_runs", "finished_at", "id=$2", c.Run).Scan(&finished)
		if err != nil && !errors.Is(err, store.ErrNoRows) {
			return "", err
		}
		if finished != nil || errors.Is(err, store.ErrNoRows) {
			c, err = h.changeLibraryCall(ctx, c.ID, func(c *libraryCall) error { c.State = "settling"; return nil })
			if err != nil {
				return "", err
			}
		}
	}
	if c.State == "settling" {
		resolved, e := h.settleLibraryCall(ctx, c)
		if e == nil {
			c = resolved
		} else if errors.Is(e, creativeops.ErrConflict) {
			return "", refuseTool("result_incompatible：回执与原命令不一致，不能重放或声称已完成，请检查执行记录")
		} else {
			c.State = "settling"
		}
	}
	if c.State != "complete" {
		out, err := json.Marshal(map[string]any{"result_id": c.ID, "state": "in_progress", "details_available": false})
		return string(out), err
	}
	offset, err := resultOffset(c, args.Cursor)
	if err != nil {
		return "", err
	}
	for n := min(args.Limit, len(c.Details)-offset); n >= 0; n-- {
		if n == 0 && offset < len(c.Details) {
			return "", refuseTool("单条完整明细超过读取预算")
		}
		more := offset+n < len(c.Details)
		next := ""
		if more {
			next, err = resultCursor(c, offset+n)
			if err != nil {
				return "", err
			}
		}
		out, err := json.Marshal(map[string]any{"result_id": c.ID, "state": "complete", "summary": c.Summary, "items": c.Details[offset : offset+n], "total_count": len(c.Details), "has_more": more, "next_cursor": next, "retained_until": c.Retained})
		if err != nil {
			return "", err
		}
		if len(out) <= h.service.limits.ToolResultBytes {
			return string(out), nil
		}
	}
	return "", refuseTool("结果超过读取预算")
}
