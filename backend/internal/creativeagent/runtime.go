package creativeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	loadSkillTool     = "load_skill"
	readSkillTool     = "read_skill_resource"
	readResultTool    = "read_run_result"
	inlineResultBytes = 8 << 10
	contextRetention  = 90 * 24 * time.Hour
)

var (
	errToolLimit  = errors.New("creative run tool limit exceeded")
	errToolDenied = errors.New("creative run tool is not allowed")
	// errToolFailing is one tool failing, unexplained, often enough that the
	// model would only be retrying blind.
	errToolFailing = errors.New("creative run tool keeps failing")
	// errReadRefused is a read the run's authorisation does not reach. The
	// model is told so in the tool result; it is not a failed run.
	errReadRefused = errors.New("creative run read is not authorised")
)

// runtimeHandler joins framework calls to the run journal. It is per execution,
// never shared between accounts. All durable identities come from model step +
// tool index; framework call IDs are only used to locate that recorded plan.
type runtimeHandler struct {
	libraryEnabled bool
	*adk.BaseChatModelAgentMiddleware
	service   *Service
	scope     store.AccountScope
	held      claim
	revisions []string
	current   *currentStep
	mu        sync.Mutex
	resultErr error
	// pending is the dispatch plan of the answer whose calls are running.
	pending *pendingBatch
	// failures counts unexplained failures per tool in this execution.
	failures map[string]int
	// composition is what the request being bound carries, by part. It is
	// set while binding and taken when the turn is journaled, both on the
	// framework's goroutine for one turn, under mu.
	composition contextComposition
	// measure is the conversation's calibration when the execution started.
	measure contextMeasure
	// skillIndex is the frozen index of skill versions the conversation used
	// before; load_skill may load any of them besides the run's own.
	skillIndex []skillIndexEntry
}

func (h *runtimeHandler) setComposition(c contextComposition) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.composition = c
}

// takeComposition hands the bound request's composition to the step that
// journals it. A turn bound without one is recorded with an empty composition.
func (h *runtimeHandler) takeComposition() contextComposition {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.composition
	h.composition = contextComposition{}
	if c.Bytes == nil {
		c.Bytes = map[string]int{}
	}
	return c
}

var _ adk.ChatModelAgentMiddleware = (*runtimeHandler)(nil)

func (h *runtimeHandler) setResultError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resultErr = err
}
func (h *runtimeHandler) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	step, binding := h.current.get()
	view, err := h.service.gateway.RequestForBinding(ctx, h.scope, callerService, binding)
	if err != nil {
		return ctx, state, err
	}
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var code *string
		if err := tx.QueryRow(ctx, "creative_agent_steps", "error_code", "id=$2 AND run_id=$3", step, h.held.RunID).Scan(&code); err != nil {
			return err
		}
		if code != nil {
			h.mu.Lock()
			rejected := h.resultErr
			h.mu.Unlock()
			if rejected != nil {
				return rejected
			}
			return errCheckpointJournal
		}
		_, err := tx.Update(ctx, "creative_agent_steps", "llm_request_id=$3", "id=$2", step, view.ID)
		return err
	})
	if err != nil {
		return ctx, state, err
	}
	// Provider call IDs may repeat in later rounds. Project each call into the
	// durable plan identity before Eino pairs it with a tool result.
	if len(state.Messages) > 0 {
		last := state.Messages[len(state.Messages)-1]
		for i := range last.ToolCalls {
			last.ToolCalls[i].ID = runtimeCallID(step, i)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return ctx, state, h.resultErr
}
func (h *runtimeHandler) configure(ctx context.Context) (adk.ToolsConfig, []adk.ChatModelAgentMiddleware, error) {
	if !h.held.Model.Capability.ToolCalling {
		return h.configureTools(ctx)
	}
	if err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		consent, err := readConsentForUpdate(ctx, tx, h.held.ConsentID)
		if err != nil {
			return err
		}
		h.libraryEnabled = consent.Scope.Mode == ScopeAccountLibrary && consentHas(consent, "media_metadata")
		return nil
	}); err != nil {
		return adk.ToolsConfig{}, nil, err
	}
	return h.configureTools(ctx)
}

func (h *runtimeHandler) planInTx(ctx context.Context, tx store.TxAccountScope, modelStep string, result llmgateway.Result) error {
	if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
		// Consent revocation is irreversible and is checked only after locking
		// and validating this claim. Keep that paid result consumed for audit,
		// without publishing its text or making its tool plans executable.
		if errors.Is(err, ErrConsentRevoked) {
			h.setResultError(err)
			return nil
		}
		// A stale holder or a failed check must leave the result pending for
		// its successor; it is not a rejected answer we may mark consumed.
		return err
	}
	calls := result.ToolCalls
	resultBytes := len(result.Text)
	for _, c := range calls {
		resultBytes += len(c.ID) + len(c.Name) + len(c.Arguments)
	}
	if resultBytes > h.service.limits.ModelResultBytes || (len(calls) == 0 && len(result.Text) > h.service.limits.MessageBodyBytes) {
		h.setResultError(ErrLimit)
		return nil
	}
	if !result.Consumable() {
		h.setResultError(errEmptyAnswer)
		return nil
	}
	if len(calls) == 0 {
		return nil
	}
	if len(calls) > h.service.limits.ToolCallsPerTurn {
		// Keep the paid result consumed, but do not create executable plans beyond
		// one page. The failed model step records the rejected batch.
		h.setResultError(errToolLimit)
		_, err := tx.Update(ctx, "creative_agent_steps", "error_code='creative_tool_limit'", "id=$2", modelStep)
		return err
	}
	var ordinal int64
	if err := tx.QueryRow(ctx, "creative_agent_runs", "next_step_ordinal", "id=$2", h.held.RunID).Scan(&ordinal); err != nil {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(calls))
	for index, c := range calls {
		state, code := "prepared", ""
		if !h.allowed(c.Name) {
			state, code = "failed", "creative_tool_not_allowed"
			h.setResultError(errToolDenied)
		}
		if c.ID == "" || seen[c.ID] || len(c.Arguments) > h.service.limits.ToolArgumentBytes || !json.Valid(c.Arguments) {
			state, code = "failed", "creative_tool_arguments_invalid"
			h.setResultError(creativeops.ErrValidation)
		}
		seen[c.ID] = true
		id, err := creativeops.NewResourceID("ccst")
		if err != nil {
			return err
		}
		input, err := json.Marshal(c)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(input)
		var finished any
		if state == "failed" {
			finished = now
		}
		// The blocks for these plans are written after the answer's text, in
		// consumeInTx, so the order on screen is the order the model spoke in.
		if err = tx.Insert(ctx, "creative_agent_steps", []string{"id", "run_id", "ordinal", "kind", "state", "execution_epoch", "tool_key", "tool_version", "parent_model_step_id", "tool_call_index", "input_hash", "input", "error_code", "started_at", "finished_at"}, id, h.held.RunID, ordinal+int64(index), "tool", state, h.held.Epoch, c.Name, runtimeToolVersion(c.Name), modelStep, index, hex.EncodeToString(sum[:]), input, nullableText(code), now, finished); err != nil {
			return err
		}
	}
	_, err = tx.Update(ctx, "creative_agent_runs", "next_step_ordinal=$3", "id=$2", h.held.RunID, ordinal+int64(len(calls)))
	return err
}

func (h *runtimeHandler) WrapInvokableToolCall(_ context.Context, next adk.InvokableToolCallEndpoint, info *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		return h.dispatch(ctx, info.Name, info.CallID, args, func(ctx context.Context) (string, error) { return next(ctx, args, opts...) })
	}, nil
}

func (h *runtimeHandler) callTool(ctx context.Context, name, callID, args string, read func(context.Context) (string, error)) (string, error) {
	if !h.allowed(name) {
		return "", errToolDenied
	}
	parent, _ := h.current.get()
	var stepID, state string
	var saved []byte
	var recordedRetry bool
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_agent_steps", "id,state,input,output,tool_call_index,retry_of_step_id", "run_id=$2 AND parent_model_step_id=$3", []store.OrderBy{{Column: "tool_call_index"}}, h.service.limits.ToolCallsPerTurn, 0, h.held.RunID, parent)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, st string
			var raw, out []byte
			var index int
			var retryOf *string
			if err = rows.Scan(&id, &st, &raw, &out, &index, &retryOf); err != nil {
				return err
			}
			var c llmgateway.ToolCall
			if err = json.Unmarshal(raw, &c); err != nil {
				return err
			}
			if runtimeCallID(parent, index) == callID && c.Name == name && string(c.Arguments) == args {
				stepID, state, saved = id, st, out
				recordedRetry = retryOf != nil
			}
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if stepID == "" {
			return ErrRunState
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if state == "succeeded" || state == "failed" {
		var out toolProjection
		if err = json.Unmarshal(saved, &out); err != nil {
			return "", err
		}
		return out.Result, nil
	}
	if state != "prepared" {
		return "", ErrRunState
	}
	// I/O happens without row locks. Mutable permission is checked again before
	// publishing its result to the model, in the same transaction as the journal.
	var result string
	var readErr error
	if readErr == nil {
		result, readErr = read(withToolStep(ctx, stepID))
	}
	code := ""
	var public creativeops.Public
	var parked errParked
	var structured *structuredRefusal
	switch {
	case readErr == nil:
	case errors.As(readErr, &parked):
		// The job's completion writes this step; the run waits for it.
		return "", tool.StatefulInterrupt(ctx, clientJobInfo+parked.jobID, parked.jobID)
	case stopsRun(ctx, readErr):
		return "", readErr
	case errors.As(readErr, &structured):
		code, result = "creative_tool_refused", structured.result
	case errors.As(readErr, &public):
		code = "creative_tool_refused"
		raw, err := json.Marshal(map[string]any{"success": false, "code": code, "message": public.PublicMessage()})
		if err != nil {
			return "", err
		}
		result = string(raw)
	// Each refusal says what is actually missing, so the model can tell the
	// photographer instead of retrying or guessing the content.
	case errors.Is(readErr, errReadRefused):
		code = "creative_tool_not_authorized"
		result = `{"success":false,"code":"creative_tool_not_authorized","message":"这项内容不在本次可读取范围内，或相关授权已撤销"}`
	case errors.Is(readErr, ErrEgressRequired):
		code = "creative_tool_not_authorized"
		result = `{"success":false,"code":"creative_tool_not_authorized","message":"本次对话没有授权发送这类内容"}`
	case errors.Is(readErr, ErrModelCapability):
		code = "creative_tool_model_capability"
		result = `{"success":false,"code":"creative_tool_model_capability","message":"这是一张图片，当前模型不能查看图片内容；如需分析画面，请换用支持图片的模型"}`
	case errors.Is(readErr, ErrNotFound), errors.Is(readErr, creativeops.ErrValidation):
		code = "creative_tool_input_invalid"
		result = `{"success":false,"code":"creative_tool_input_invalid","message":"参数或引用的对象不合法，请核对后再调用"}`
	default:
		code = "creative_tool_failed"
		if result, err = h.unexpected(ctx, name, stepID, readErr); err != nil {
			return "", err
		}
	}
	if !utf8.ValidString(result) || len(result) > h.service.limits.ToolResultBytes {
		return "", ErrLimit
	}
	projection := ""
	err = h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		var err error
		projection, err = h.publishResultInTx(ctx, tx, publishedCall{StepID: stepID, CallID: callID, Name: name, Args: args, Result: result,
			Failed: readErr != nil, Code: code, RecordedRetry: recordedRetry})
		return err
	})
	return projection, err
}

// structuredRefusal is a business "no" whose answer has a structure the model
// acts on, such as a scene conflict with the new baseline. The step fails and
// its result is that structure.
type structuredRefusal struct{ result string }

func (r *structuredRefusal) Error() string { return "creative tool refused: " + r.result }

// publishedCall is one finished call as the journal records it.
type publishedCall struct {
	StepID, CallID, Name, Args string
	Result                     string
	Failed                     bool
	Code                       string
	// RecordedRetry is a recovery with no model call to pair a tool message with.
	RecordedRetry bool
}

// publishResultInTx records a call's result for the model: its context bytes,
// the step, the tool message and the blocks, in that order. callTool runs it
// after the call returns; a client job's completion runs it for a call that
// parked. The caller has admitted the run in tx.
func (h *runtimeHandler) publishResultInTx(ctx context.Context, tx store.TxAccountScope, r publishedCall) (string, error) {
	kind := "tool_result"
	if r.Name == readSkillTool && !r.Failed {
		kind = "skill_resource"
	}
	item, err := h.persistContextInTx(ctx, tx, r.StepID, kind, r.Result)
	if err != nil {
		return "", err
	}
	projection := r.Result
	if len(r.Result) > inlineResultBytes && r.Name != loadSkillTool && r.Name != readResultTool && !libraryWriteTool(r.Name) && r.Name != searchAssetsTool && r.Name != readLibraryTool && r.Name != readLibraryResultTool && h.allowed(readResultTool) {
		raw, err := json.Marshal(map[string]any{"success": true, "item_id": item, "byte_size": len(r.Result), "read_tool": readResultTool, "preview": utf8Prefix(r.Result, 512)})
		if err != nil {
			return "", err
		}
		projection = string(raw)
	}
	output, err := json.Marshal(toolProjection{Result: projection, ItemID: item})
	if err != nil {
		return "", err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return "", err
	}
	terminal := "succeeded"
	if r.Failed {
		terminal = "failed"
	}
	if _, err = tx.Update(ctx, "creative_agent_steps", "state=$3,output=$4,error_code=$5,finished_at=$6,updated_at=$6,execution_epoch=$7", "id=$2 AND state='prepared'", r.StepID, terminal, output, nullableText(r.Code), now, h.held.Epoch); err != nil {
		return "", err
	}
	// A recorded recovery has no model call to pair with a tool message.
	// Its result stays in the step journal; executeRecordedRetry publishes
	// the separate assistant delivery, including after an interrupted retry.
	if !r.RecordedRetry {
		if _, err := h.persistToolMessage(ctx, tx, r.StepID, r.CallID, r.Name, projection, r.Result); err != nil {
			return "", err
		}
	}
	if r.Name == updatePlanTool && !r.Failed {
		plan, err := parsePlan(r.Args)
		if err != nil {
			return "", err
		}
		if _, err = writeBlockInTx(ctx, tx, h.held.RunID, "plan", blockChange{Type: BlockPlan, State: planState(plan.Steps), Set: plan}); err != nil {
			return "", err
		}
	}
	return projection, h.service.writeToolBlockInTx(ctx, tx, h.held.RunID, r.StepID)
}

// stopsRun names the failures that end the run whichever tool met them: the
// run itself cannot go on. Every other failure belongs to the tool and goes
// back to the model as a result.
func stopsRun(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, llmgateway.ErrDeadline) || errors.Is(err, errRunInterrupted) || errors.Is(err, ErrRunState) ||
		errors.Is(err, ErrConsentRevoked) || errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrLimit)
}

// unexpectedFailures is how often one tool may fail for reasons nobody
// explained before the run stops: past that, the model is retrying blind.
const unexpectedFailures = 3

// unexpected answers a failure no layer explained. The model learns which
// call failed, whether anything changed and whether to try again; the error
// itself goes to the log under the step id the photographer can quote.
func (h *runtimeHandler) unexpected(ctx context.Context, name, stepID string, err error) (string, error) {
	h.service.logger.ErrorContext(ctx, "creative agent tool failed unexpectedly",
		slog.String("run_id", h.held.RunID), slog.String("step_id", stepID), slog.String("tool", name), slog.String("error", err.Error()))
	h.mu.Lock()
	if h.failures == nil {
		h.failures = map[string]int{}
	}
	h.failures[name]++
	failures := h.failures[name]
	h.mu.Unlock()
	if failures >= unexpectedFailures {
		return "", fmt.Errorf("%w: %w", errToolFailing, err)
	}
	title, effect := name, "unknown"
	for _, t := range h.service.registeredTools() {
		if t.Key == name {
			title = t.DisplayName
			if t.EffectClass == "read_only" {
				effect = "none"
			}
		}
	}
	message := fmt.Sprintf("「%s」因系统内部错误失败，原因已记录（编号 %s）。", title, stepID)
	if effect == "none" {
		message += "这次调用没有改动任何内容。"
	} else if libraryWriteTool(name) {
		message += "请用 read_library_result 或资产库查询核对后再继续。"
	} else if sceneWriteTool(name) {
		message += "改动可能已生效也可能没有，继续之前请先用 read_scene 核对。"
	} else {
		message += "改动可能已生效也可能没有，继续之前请先读取画布核对。"
	}
	var timeout net.Error
	retryable := store.IsSerializationFailure(err) || errors.As(err, &timeout) && timeout.Timeout()
	raw, jsonErr := json.Marshal(map[string]any{"success": false, "code": "creative_tool_failed", "ref": stepID, "effect": effect, "retryable": retryable, "message": message})
	return string(raw), jsonErr
}

type toolProjection struct {
	Result string `json:"result"`
	ItemID string `json:"item_id"`
}

func runtimeCallID(step string, index int) string { return step + "_" + strconv.Itoa(index) }

func utf8Prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
func strictArguments(raw string, out any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errToolArguments
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errToolArguments
	}
	return nil
}

type runtimeReadTool struct {
	handler *runtimeHandler
	name    string
}

var _ tool.InvokableTool = (*runtimeReadTool)(nil)

func (t *runtimeReadTool) Info(context.Context) (*schema.ToolInfo, error) {
	if t.name == readAssetTool || t.name == readCanvasTool || t.name == readHistoryTool {
		return contextToolInfo(t.name), nil
	}
	params := map[string]*schema.ParameterInfo{}
	desc := "读取本次运行已登记的结果片段；offset/limit 为 UTF-8 字节范围，limit 最多 8192。"
	params["item_id"] = &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "本 run 的结果定位符"}
	params["offset"] = &schema.ParameterInfo{Type: schema.Integer, Required: true, Desc: "起始字节，从 0 开始"}
	params["limit"] = &schema.ParameterInfo{Type: schema.Integer, Required: true, Desc: "最多 8192 字节"}

	return &schema.ToolInfo{Name: t.name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(params)}, nil
}
func (t *runtimeReadTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	switch t.name {
	case readAssetTool:
		return t.handler.readAsset(ctx, raw)
	case readCanvasTool:
		return t.handler.readCanvas(ctx, raw)
	case readHistoryTool:
		return t.handler.readHistory(ctx, raw)
	}
	return t.handler.readRunResult(ctx, raw)
}
func (h *runtimeHandler) readRunResult(ctx context.Context, raw string) (string, error) {
	var args struct {
		ItemID string `json:"item_id"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return "", err
	}
	if args.Offset < 0 || args.Limit < 1 || args.Limit > inlineResultBytes {
		return "", creativeops.ErrValidation
	}
	var body []byte
	var digest string
	var revisions []string
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, h.revisions); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "creative_agent_context_items", "payload,digest", "id=$2 AND run_id=$3 AND retained_until>clock_timestamp()", args.ItemID, h.held.RunID).Scan(&body, &digest); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "creative_context_item_content_refs", "content_revision_id", "item_id=$2", args.ItemID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			revisions = append(revisions, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range revisions {
			readable, err := readableInTx(ctx, tx, h.held.RunID, h.held.ConsentID, id)
			if err != nil {
				return err
			}
			if !readable {
				return errReadRefused
			}
			if _, err := creativecontent.RequireUsable(ctx, tx, id, "display"); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, store.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != digest {
		return "", ErrRunState
	}
	if args.Offset > len(body) || (args.Offset < len(body) && !utf8.RuneStart(body[args.Offset])) {
		return "", creativeops.ErrValidation
	}
	part := utf8Prefix(string(body[args.Offset:]), args.Limit)
	result, err := json.Marshal(map[string]any{"success": true, "content_revision_ids": revisions, "text": part, "next_offset": args.Offset + len(part), "eof": args.Offset+len(part) == len(body)})
	return string(result), err
}

func (h *runtimeHandler) persistContextInTx(ctx context.Context, tx store.TxAccountScope, stepID, kind, payload string) (string, error) {
	id, err := creativeops.NewResourceID("ccci")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(payload))
	var toolInput json.RawMessage
	if err := tx.QueryRow(ctx, "creative_agent_steps", "input", "id=$2 AND run_id=$3", stepID, h.held.RunID).Scan(&toolInput); err != nil {
		return "", err
	}
	var call llmgateway.ToolCall
	if err := json.Unmarshal(toolInput, &call); err != nil {
		return "", err
	}
	revisions := toolContentRevisions(call.Name, payload)
	manifest := map[string]any{"content_revision_ids": revisions, "input_revision_ids": h.revisions, "tool_input": toolInput}
	if call.Name == readSkillTool || call.Name == loadSkillTool {
		var args struct {
			Version string `json:"skill_version_id"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err == nil && args.Version != "" {
			var digest string
			err := tx.QueryRow(ctx, "creative_run_skills", "digest", "run_id=$2 AND skill_version_id=$3", h.held.RunID, args.Version).Scan(&digest)
			if err != nil && !errors.Is(err, store.ErrNoRows) {
				return "", err
			}
			if err == nil {
				manifest["skill_version_id"] = args.Version
				manifest["skill_digest"] = digest
			}
		}
	}
	source, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return "", err
	}
	if err = tx.Insert(ctx, "creative_agent_context_items", []string{"id", "run_id", "kind", "source_step_id_snapshot", "source_manifest", "payload", "digest", "revision", "retained_until"}, id, h.held.RunID, kind, stepID, source, []byte(payload), hex.EncodeToString(sum[:]), 1, now.Add(contextRetention)); err != nil {
		return "", err
	}
	for _, revision := range revisions {
		if err = tx.Insert(ctx, "creative_context_item_content_refs", []string{"item_id", "content_revision_id"}, id, revision); err != nil {
			return "", err
		}
	}
	return id, nil
}
