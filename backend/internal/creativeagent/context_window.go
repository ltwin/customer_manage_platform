package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The window is counted in tokens and the gateway's hold in bytes. Nothing
// here tokenizes: a conversation's own last request tells how many tokens its
// bytes became, and that ratio estimates the next one (owner 2026-10-14).
const (
	defaultTokensPerByte = 1.0 / 3
	// A ratio outside this range came from a request the bytes do not describe,
	// one made mostly of images for example, and is not used.
	minTokensPerByte = 1.0 / 8
	maxTokensPerByte = 1.0
	// imageInputTokens is what one attached image is estimated at. Images are
	// counted apart from text and never calibrate the ratio.
	imageInputTokens = 1600
	// defaultFixedBytes stands in for the instruction and tool definitions
	// before any request of the conversation has measured them.
	defaultFixedBytes = 48 << 10
	// historyMarginBytes leaves room for inputs expanded during assembly.
	historyMarginBytes = 4096
)

// Context composition parts, in the order the usage view lists them.
const (
	partSystem      = "system"
	partTools       = "tools"
	partSkill       = "skill"
	partSummary     = "summary"
	partMessages    = "messages"
	partToolResults = "tool_results"
	partImages      = "images"
)

var contextParts = []string{partSystem, partTools, partSkill, partSummary, partMessages, partToolResults, partImages}

// requestByteCeiling is the most one request may carry in bytes. The gateway
// prices the first hold at one token per byte, so this is a spending bound,
// derived from the model's token window rather than set beside it.
func requestByteCeiling(limits Limits, model llmgateway.ModelConfig) int {
	if limits.BytesPerWindowToken <= 0 {
		return limits.InputTextBytes // A run created before windows existed.
	}
	return min(limits.InputTextBytes, model.InputWindowTokens()*limits.BytesPerWindowToken)
}

// compactAt is the input size past which a new run compacts first; zero never.
func compactAt(limits Limits, model llmgateway.ModelConfig) int {
	if limits.AutoCompactPercent <= 0 {
		return 0
	}
	return model.InputWindowTokens() * limits.AutoCompactPercent / 100
}

// tokenRatio converts text bytes into estimated tokens.
type tokenRatio float64

func (r tokenRatio) tokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return int(math.Ceil(float64(bytes) * float64(r)))
}

// bytesFor is the inverse: how many text bytes fit in so many tokens.
func (r tokenRatio) bytesFor(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	return int(float64(tokens) / float64(r))
}

// measuredRatio derives a ratio from one request, or reports it unusable.
func measuredRatio(inputTokens, textBytes, images int64) (tokenRatio, bool) {
	text := inputTokens - images*imageInputTokens
	if textBytes <= 0 || text <= 0 {
		return 0, false
	}
	r := float64(text) / float64(textBytes)
	return tokenRatio(r), r >= minTokensPerByte && r <= maxTokensPerByte
}

// contextMeasure is what the latest measured request of a conversation said.
type contextMeasure struct {
	Ratio tokenRatio
	// Fixed is the instruction and tool definitions of that request in bytes,
	// which the next request will almost certainly carry again.
	Fixed int
	// Last is the newest request's own input count and when it was measured.
	// It sees what a byte projection cannot, such as quoted content expanded
	// at dispatch, so it bounds the next estimate from below.
	Last   int64
	LastAt time.Time
}

// conversationMeasureInTx reads the newest calibrated request. A conversation
// that has none uses the defaults.
func conversationMeasureInTx(ctx context.Context, tx store.TxAccountScope, conversationID string) (contextMeasure, error) {
	out := contextMeasure{Ratio: defaultTokensPerByte, Fixed: defaultFixedBytes}
	rows, err := tx.QueryPage(ctx, "creative_agent_steps", "input_tokens,input_text_bytes,input_image_count,context_composition,updated_at",
		"kind='model' AND input_tokens IS NOT NULL AND input_text_bytes IS NOT NULL AND run_id IN (SELECT id FROM creative_agent_runs WHERE account_id=$1 AND conversation_id=$2)",
		[]store.OrderBy{{Column: "updated_at", Desc: true}}, 5, 0, conversationID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	ratioFound, fixedFound := false, false
	for rows.Next() {
		var tokens, bytes int64
		var images *int64
		var raw []byte
		var at time.Time
		if err := rows.Scan(&tokens, &bytes, &images, &raw, &at); err != nil {
			return out, err
		}
		if out.Last == 0 {
			out.Last, out.LastAt = tokens, at
		}
		var count int64
		if images != nil {
			count = *images
		}
		if r, ok := measuredRatio(tokens, bytes, count); ok && !ratioFound {
			out.Ratio, ratioFound = r, true
		}
		var composition contextComposition
		if !fixedFound && len(raw) > 0 && json.Unmarshal(raw, &composition) == nil {
			if fixed := composition.Bytes[partSystem] + composition.Bytes[partTools]; fixed > 0 {
				out.Fixed, fixedFound = fixed, true
			}
		}
		if ratioFound && fixedFound {
			break
		}
	}
	return out, rows.Err()
}

// currentMeasureInTx keeps historical token calibration, but measures fixed
// inputs from this run's consent, model and Skill. An old tool set cannot tell
// whether a protected summary still fits after an authorization change.
func (s *Service) currentMeasureInTx(ctx context.Context, tx store.TxAccountScope, conversationID, consentID string,
	model llmgateway.ModelConfig, skill *creativeskill.Snapshot) (contextMeasure, error) {
	measure, err := conversationMeasureInTx(ctx, tx, conversationID)
	if err != nil {
		return measure, err
	}
	consent, err := readConsentForUpdate(ctx, tx, consentID)
	if err != nil {
		return measure, err
	}
	h := &runtimeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, service: s,
		held: claim{Model: model, Skill: skill}, libraryEnabled: consent.Scope.Mode == ScopeAccountLibrary && consentHas(consent, "media_metadata")}
	current, skillBytes := "", 0
	if skill != nil {
		current, skillBytes = skill.ID, len(skill.Instructions)
	}
	if h.skillIndex, err = skillIndexInTx(ctx, tx, conversationID, current); err != nil {
		return measure, err
	}
	config, handlers, err := h.configureTools(ctx)
	if err != nil {
		return measure, err
	}
	input := &adk.ChatModelAgentContext{Instruction: instructionOf(skill), Tools: config.Tools}
	for _, handler := range handlers {
		if ctx, input, err = handler.BeforeAgent(ctx, input); err != nil {
			return measure, err
		}
	}
	infos := make([]*schema.ToolInfo, 0, len(input.Tools))
	for _, tool := range input.Tools {
		info, err := tool.Info(ctx)
		if err != nil {
			return measure, err
		}
		infos = append(infos, info)
	}
	definitions, err := llmgateway.EinoToolDefinitions(infos)
	if err != nil {
		return measure, err
	}
	// The caller separately budgets the current Skill's instructions.
	measure.Fixed = len(input.Instruction) + perMessageOverhead + toolDefinitionBytes(definitions) - skillBytes
	return measure, nil
}

// contextComposition is one request broken into parts by text bytes, with its
// images counted apart. It is recorded on the model step that sent it.
type contextComposition struct {
	Bytes  map[string]int `json:"bytes"`
	Images int            `json:"images"`
}

func (c contextComposition) textBytes() int {
	total := 0
	for _, part := range contextParts {
		total += c.Bytes[part]
	}
	return total
}

// messageBytes is the gateway's own per-message measure, without images, so a
// composition and the hold it is held against count the same bytes.
func messageBytes(m llmgateway.Message) (int, int) {
	bytes, images := 16, 0
	for _, b := range m.Blocks {
		if b.Kind == llmgateway.BlockText {
			bytes += len(b.Text)
		} else if b.Image != nil {
			images++
		}
	}
	for _, c := range m.ToolCalls {
		bytes += len(c.Arguments) + len(c.Name)
	}
	return bytes + len(m.Reasoning), images
}

// toolDefinitionBytes counts tool definitions the way the gateway does.
func toolDefinitionBytes(tools []llmgateway.ToolDefinition) int {
	total := 0
	for _, t := range tools {
		total += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	return total
}

// composeMessages sorts one request's messages into parts. source and
// messages are index-aligned for len(source); anything bound after them is an
// attachment a tool result asked for.
func composeMessages(source []*schema.Message, messages []llmgateway.Message, skillBytes int, toolNames map[string]string) contextComposition {
	c := contextComposition{Bytes: map[string]int{}}
	for i, m := range messages {
		bytes, images := messageBytes(m)
		c.Images += images
		part := partToolResults
		if i < len(source) {
			part = messagePart(source[i], toolNames)
		}
		if part == partSystem && skillBytes > 0 {
			moved := min(skillBytes, bytes)
			c.Bytes[partSkill] += moved
			bytes -= moved
		}
		c.Bytes[part] += bytes
	}
	return c
}

func messagePart(m *schema.Message, toolNames map[string]string) string {
	switch input, _ := m.Extra[inputKindKey].(string); input {
	case inputCompaction:
		return partSummary
	case inputSkillIndex:
		return partSkill
	}
	switch m.Role {
	case schema.System:
		return partSystem
	case schema.Tool:
		if toolNames[m.ToolCallID] == loadSkillTool {
			return partSkill
		}
		return partToolResults
	default:
		return partMessages
	}
}

// ContextUsage is what the conversation's context looked like at its last
// measured request, for the composer's ring. UsedTokens is the provider's own
// count unless Estimated says it is a projection (after a compaction, before
// the next request has been measured).
type ContextUsage struct {
	ModelKey        string             `json:"model_key"`
	WindowTokens    int                `json:"window_tokens"`
	CompactAtTokens int                `json:"compact_at_tokens"`
	UsedTokens      int64              `json:"used_tokens"`
	Estimated       bool               `json:"estimated"`
	Parts           []ContextUsagePart `json:"parts"`
	MeasuredAt      time.Time          `json:"measured_at"`
}

// ContextUsagePart is always an estimate: providers report one total, which
// the parts share in proportion to their bytes.
type ContextUsagePart struct {
	Key    string `json:"key"`
	Tokens int64  `json:"tokens"`
}

// usageView spreads a total over a composition. Images take their fixed share
// first; text parts divide the rest by bytes, and the last part absorbs the
// rounding so the parts always add up to the total.
func usageView(model llmgateway.ModelConfig, limits Limits, total int64, c contextComposition, estimated bool, at time.Time) ContextUsage {
	view := ContextUsage{ModelKey: model.ModelKey, WindowTokens: model.InputWindowTokens(), CompactAtTokens: compactAt(limits, model),
		UsedTokens: total, Estimated: estimated, Parts: []ContextUsagePart{}, MeasuredAt: at}
	images := min(total, int64(c.Images)*imageInputTokens)
	text := total - images
	bytes := c.textBytes()
	var assigned int64
	last := -1
	for _, part := range contextParts {
		if part == partImages {
			continue
		}
		if c.Bytes[part] <= 0 {
			continue
		}
		share := int64(0)
		if bytes > 0 {
			share = text * int64(c.Bytes[part]) / int64(bytes)
		}
		view.Parts = append(view.Parts, ContextUsagePart{Key: part, Tokens: share})
		assigned += share
		last = len(view.Parts) - 1
	}
	if last >= 0 {
		view.Parts[last].Tokens += text - assigned
	}
	if images > 0 {
		view.Parts = append(view.Parts, ContextUsagePart{Key: partImages, Tokens: images})
	}
	return view
}

// estimatedUsage projects a composition through a ratio, for the moments no
// request has measured it yet.
func estimatedUsage(model llmgateway.ModelConfig, limits Limits, ratio tokenRatio, c contextComposition, at time.Time) ContextUsage {
	total := int64(ratio.tokens(c.textBytes()) + c.Images*imageInputTokens)
	return usageView(model, limits, total, c, true, at)
}

// recordMeasureInTx stores what the provider counted for a model step and
// announces the conversation's new usage. A result without a usable count
// leaves the previous measure standing.
func (h *runtimeHandler) recordMeasureInTx(ctx context.Context, tx store.TxAccountScope, stepID string, usage llmgateway.UsageEvidence) error {
	if stepID == "" || usage.InputTotalTokens == nil || *usage.InputTotalTokens <= 0 {
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, "creative_agent_steps", "context_composition", "id=$2 AND run_id=$3", stepID, h.held.RunID).Scan(&raw)
	if errors.Is(err, store.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "creative_agent_steps", "input_tokens=$3", "id=$2", stepID, *usage.InputTotalTokens); err != nil {
		return err
	}
	var composition contextComposition
	if len(raw) == 0 || json.Unmarshal(raw, &composition) != nil {
		return nil
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	view := usageView(h.held.Model, h.service.limits, *usage.InputTotalTokens, composition, false, now)
	_, err = appendRunEventInTx(ctx, tx, h.held.RunID, "context.usage", view)
	return err
}

// ConversationContextUsage reads what the composer's ring shows. A
// conversation that has never sent a request has none, and the ring is empty
// rather than guessed (owner 2026-10-14).
func (s *Service) conversationUsageInTx(ctx context.Context, tx store.TxAccountScope, conversationID string) (*ContextUsage, error) {
	rows, err := tx.QueryPage(ctx, "creative_agent_steps", "run_id,input_tokens,context_composition,updated_at",
		"kind='model' AND input_tokens IS NOT NULL AND context_composition IS NOT NULL AND run_id IN (SELECT id FROM creative_agent_runs WHERE account_id=$1 AND conversation_id=$2)",
		[]store.OrderBy{{Column: "updated_at", Desc: true}}, 1, 0, conversationID)
	if err != nil {
		return nil, err
	}
	var runID string
	var tokens int64
	var raw []byte
	var at time.Time
	found := rows.Next()
	if found {
		err = rows.Scan(&runID, &tokens, &raw, &at)
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	checkpoint, err := latestCompactionInTx(ctx, tx, conversationID)
	if err != nil {
		return nil, err
	}
	// A checkpoint newer than the measurement changed what the next request
	// will carry; until that request is measured, the ring shows its estimate.
	if checkpoint != nil && (!found || checkpoint.CreatedAt.After(at)) {
		model, err := s.models.Model(checkpoint.ModelKey)
		if err != nil || len(checkpoint.Composition) == 0 {
			return nil, nil
		}
		var composition contextComposition
		if json.Unmarshal(checkpoint.Composition, &composition) != nil {
			return nil, nil
		}
		view := usageView(model, s.limits, checkpoint.TokensAfter, composition, true, checkpoint.CreatedAt)
		return &view, nil
	}
	if !found {
		return nil, nil
	}
	var modelKey string
	if err := tx.QueryRow(ctx, "creative_agent_runs", "model_key", "id=$2", runID).Scan(&modelKey); err != nil {
		return nil, err
	}
	model, err := s.models.Model(modelKey)
	if err != nil {
		return nil, nil // A model withdrawn since has no window to show against.
	}
	var composition contextComposition
	if json.Unmarshal(raw, &composition) != nil {
		return nil, nil
	}
	view := usageView(model, s.limits, tokens, composition, false, at)
	return &view, nil
}
