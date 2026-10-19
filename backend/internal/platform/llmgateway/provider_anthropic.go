package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// AnthropicMessages adapts the Anthropic Messages protocol. Claude models are
// served through it; a thinking deployment hands its signed thinking blocks
// back on the turns of the same tool loop.
//
// Every request asks for automatic prompt caching: an agent loop resends the
// whole window each step, and a cache read costs a twentieth of fresh input.
// Cache writes cost more than fresh input, which the deployment's uncached
// rate has to cover (see mapAnthropicUsage).
type AnthropicMessages struct {
	client  *http.Client
	version string
}

// NewAnthropicMessages builds the adapter with implicit retries disabled.
func NewAnthropicMessages(client *http.Client, apiVersion string) *AnthropicMessages {
	if apiVersion == "" {
		apiVersion = "2023-06-01"
	}
	return &AnthropicMessages{client: client, version: apiVersion}
}

func (a *AnthropicMessages) Key() ProviderKey { return ProviderAnthropicMessage }

// anthropicReasoningFormat marks thinking this adapter recorded. The record is
// the assistant turn's whole content array, verbatim: a thinking block is only
// valid next to the exact text and tool calls it was produced with, and only
// for the model that produced it.
const anthropicReasoningFormat = "anthropic_messages"

type anthropicReasoning struct {
	Format     string            `json:"format"`
	Deployment string            `json:"deployment"`
	Model      string            `json:"model"`
	Content    []json.RawMessage `json:"content"`
}

func decodeAnthropicReasoning(raw string) (anthropicReasoning, bool) {
	if !strings.HasPrefix(raw, "{") {
		return anthropicReasoning{}, false
	}
	var r anthropicReasoning
	if json.Unmarshal([]byte(raw), &r) != nil || r.Format != anthropicReasoningFormat || len(r.Content) == 0 {
		return anthropicReasoning{}, false
	}
	return r, true
}

// matches reports whether the recorded content is still the message the caller
// hands back. Anything else would replay a signature beside words it was not
// produced with.
func (r anthropicReasoning) matches(m Message) bool {
	var text strings.Builder
	var ids []string
	for _, raw := range r.Content {
		var b anthropicBlock
		if json.Unmarshal(raw, &b) != nil {
			return false
		}
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			ids = append(ids, b.ID)
		}
	}
	var want strings.Builder
	for _, b := range m.Blocks {
		want.WriteString(b.Text)
	}
	wantIDs := make([]string, len(m.ToolCalls))
	for i, c := range m.ToolCalls {
		wantIDs[i] = c.ID
	}
	return text.String() == want.String() && slices.Equal(ids, wantIDs)
}

type anthropicRequest struct {
	Model        string                `json:"model"`
	System       string                `json:"system,omitempty"`
	Messages     []anthropicMessage    `json:"messages"`
	MaxTokens    int                   `json:"max_tokens"`
	Stream       bool                  `json:"stream,omitempty"`
	Temperature  *float64              `json:"temperature,omitempty"`
	Tools        []anthropicTool       `json:"tools,omitempty"`
	ToolChoice   map[string]string     `json:"tool_choice,omitempty"`
	Thinking     *anthropicThinking    `json:"thinking,omitempty"`
	OutputConfig *anthropicOutput      `json:"output_config,omitempty"`
	CacheControl *anthropicCacheMarker `json:"cache_control,omitempty"`
}

type anthropicThinking struct {
	Type string `json:"type"`
}

type anthropicOutput struct {
	Effort string `json:"effort"`
}

type anthropicCacheMarker struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicUsage reads both shapes the relay answers with: the official one,
// with cache reads and writes, and a converted channel's, which reports only
// input_tokens_details.cached_tokens (measured 2026-10-16).
type anthropicUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	InputDetails             *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails *struct {
		ThinkingTokens *int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

// merge lets a later report overwrite only the fields it actually carries; a
// stream's closing usage may omit the cache counts its opening one had.
func (u *anthropicUsage) merge(next *anthropicUsage) {
	if next == nil {
		return
	}
	pick := func(dst **int64, src *int64) {
		if src != nil {
			*dst = src
		}
	}
	pick(&u.InputTokens, next.InputTokens)
	pick(&u.OutputTokens, next.OutputTokens)
	pick(&u.CacheReadInputTokens, next.CacheReadInputTokens)
	pick(&u.CacheCreationInputTokens, next.CacheCreationInputTokens)
	if next.InputDetails != nil {
		u.InputDetails = next.InputDetails
	}
	if next.OutputDetails != nil {
		u.OutputDetails = next.OutputDetails
	}
}

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	Data      string          `json:"data"`
}

type anthropicResponse struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Content    []json.RawMessage `json:"content"`
	Usage      *anthropicUsage   `json:"usage"`
}

// Invoke performs exactly one attempt against the deployment.
func (a *AnthropicMessages) Invoke(ctx context.Context, p ProviderRequest) (Result, error) {
	body, err := a.encode(ctx, p)
	if err != nil {
		return Result{}, err
	}
	endpoint := strings.TrimSuffix(p.Model.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: build request: %w", ErrValidation, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.Credential)
	req.Header.Set("anthropic-version", a.version)

	resp, err := a.client.Do(req)
	if err != nil {
		return Result{}, classifyTransport(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return Result{}, classifyStatus(p.Model, resp.StatusCode, summarizeDetail(detail))
	}
	var result Result
	if p.Stream {
		result, err = a.readStream(resp.Body, p.Sink, p.Model.DeploymentKey)
	} else {
		result, err = a.readSingle(resp.Body, p.Model.DeploymentKey)
	}
	if err != nil {
		return Result{}, err
	}
	if err := validateResult(result, p.Snapshot); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (a *AnthropicMessages) encode(ctx context.Context, p ProviderRequest) ([]byte, error) {
	out := anthropicRequest{
		Model:        p.Snapshot.RequestModelID,
		MaxTokens:    p.Chat.OutputLimit,
		Stream:       p.Stream,
		Temperature:  p.Chat.Temperature,
		CacheControl: &anthropicCacheMarker{Type: "ephemeral"},
	}
	if p.Model.ReasoningEffort != "" {
		// Claude 5.5 models cannot turn thinking off; effort is the control.
		out.Thinking = &anthropicThinking{Type: "adaptive"}
		out.OutputConfig = &anthropicOutput{Effort: p.Model.ReasoningEffort}
	}
	switch p.Chat.ToolChoice {
	case "auto", "none":
		out.ToolChoice = map[string]string{"type": p.Chat.ToolChoice}
	case "required":
		out.ToolChoice = map[string]string{"type": "any"}
	}
	for _, t := range p.Chat.Tools {
		out.Tools = append(out.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	// Thinking travels back only within the current tool loop: the turns after
	// the last thing the creator said. A block is valid only while everything
	// before it is byte-identical to when it was produced, and history ahead of
	// the loop is trimmed and compacted between runs; inside one loop the
	// history only grows. Earlier turns lose their thinking the same way on
	// every request, so the prefix the loop's blocks were produced with holds.
	loopStart := -1
	for i, m := range p.Chat.Messages {
		if m.Role == RoleUser {
			loopStart = i
		}
	}
	var system strings.Builder
	for i, m := range p.Chat.Messages {
		switch m.Role {
		case RoleSystem:
			for _, b := range m.Blocks {
				system.WriteString(b.Text)
			}
		case RoleAssistant:
			replay := i > loopStart && p.Model.ReasoningEffort != ""
			encoded, err := encodeAnthropicAssistant(m, replay, p.Model.DeploymentKey, p.Snapshot.RequestModelID)
			if err != nil {
				return nil, err
			}
			if len(encoded.Content) > 0 {
				out.Messages = append(out.Messages, encoded)
			}
		case RoleTool:
			result, err := encodeAnthropicToolResult(ctx, m)
			if err != nil {
				return nil, err
			}
			// Every result of one parallel step goes back in a single message;
			// splitting them teaches the model to stop calling in parallel.
			if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == "user" && isToolResults(out.Messages[n-1]) {
				out.Messages[n-1].Content = append(out.Messages[n-1].Content, result)
				continue
			}
			out.Messages = append(out.Messages, anthropicMessage{Role: "user", Content: []any{result}})
		default:
			content, err := anthropicContent(ctx, m.Blocks)
			if err != nil {
				return nil, err
			}
			out.Messages = append(out.Messages, anthropicMessage{Role: string(m.Role), Content: content})
		}
	}
	out.System = system.String()
	return json.Marshal(out)
}

func isToolResults(m anthropicMessage) bool {
	for _, c := range m.Content {
		block, ok := c.(map[string]any)
		if !ok || block["type"] != "tool_result" {
			return false
		}
	}
	return len(m.Content) > 0
}

// encodeAnthropicAssistant replays the recorded content when it belongs to this
// deployment and model and still matches the message; otherwise the turn is
// rebuilt from its words and calls, without thinking.
func encodeAnthropicAssistant(m Message, replay bool, deployment, model string) (anthropicMessage, error) {
	out := anthropicMessage{Role: "assistant"}
	if replay {
		if r, ok := decodeAnthropicReasoning(m.Reasoning); ok && r.Deployment == deployment && r.Model == model && r.matches(m) {
			for _, block := range r.Content {
				out.Content = append(out.Content, block)
			}
			return out, nil
		}
	}
	var text strings.Builder
	for _, b := range m.Blocks {
		text.WriteString(b.Text)
	}
	if text.Len() > 0 {
		out.Content = append(out.Content, map[string]any{"type": "text", "text": text.String()})
	}
	for _, c := range m.ToolCalls {
		input := c.Arguments
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		// The arguments go back as the model wrote them; a decode and
		// re-encode would reorder their keys and change the prompt.
		if !json.Valid(input) {
			return anthropicMessage{}, fmt.Errorf("%w: assistant tool call arguments", ErrValidation)
		}
		out.Content = append(out.Content, map[string]any{
			"type": "tool_use", "id": c.ID, "name": c.Name, "input": input,
		})
	}
	return out, nil
}

func encodeAnthropicToolResult(ctx context.Context, m Message) (map[string]any, error) {
	result := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID}
	if onlyText(m.Blocks) {
		var text strings.Builder
		for _, b := range m.Blocks {
			text.WriteString(b.Text)
		}
		result["content"] = text.String()
		return result, nil
	}
	content, err := anthropicContent(ctx, m.Blocks)
	if err != nil {
		return nil, err
	}
	result["content"] = content
	return result, nil
}

func anthropicContent(ctx context.Context, blocks []Block) ([]any, error) {
	content := make([]any, 0, len(blocks))
	for _, b := range blocks {
		switch b.Kind {
		case BlockText:
			content = append(content, map[string]any{"type": "text", "text": b.Text})
		case BlockImageInput:
			encoded, err := readImage(ctx, b.Image)
			if err != nil {
				return nil, err
			}
			content = append(content, map[string]any{
				"type":   "image",
				"source": map[string]any{"type": "base64", "media_type": b.Image.MIME, "data": encoded},
			})
		}
	}
	return content, nil
}

func (a *AnthropicMessages) readSingle(body io.Reader, deployment string) (Result, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxResultBytes))
	if err != nil {
		return Result{}, classifyTransport(err)
	}
	var payload anthropicResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_body", Detail: err.Error()}
	}
	result := Result{
		FinishReason:      mapAnthropicStop(payload.StopReason),
		Model:             payload.Model,
		ProviderRequestID: payload.ID,
		Usage:             mapAnthropicUsage(payload.Usage),
	}
	var text strings.Builder
	thought := false
	for i, raw := range payload.Content {
		var block anthropicBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_block", Detail: err.Error()}
		}
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			arguments := block.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: block.ID, Index: i, Name: block.Name, Arguments: arguments})
		case "thinking", "redacted_thinking":
			thought = true
		}
	}
	result.Text = text.String()
	if thought {
		result.Reasoning = recordAnthropicReasoning(deployment, payload.Model, payload.Content)
	}
	return result, nil
}

// recordAnthropicReasoning seals a turn's content for its own later replay. A
// record too large to travel back as a message is dropped: the next turn loses
// this step's thinking, which the model tolerates, instead of failing.
func recordAnthropicReasoning(deployment, model string, content []json.RawMessage) string {
	encoded, err := json.Marshal(anthropicReasoning{Format: anthropicReasoningFormat, Deployment: deployment, Model: model, Content: content})
	if err != nil || len(encoded) > maxTextBytes {
		return ""
	}
	return string(encoded)
}

type anthropicEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	ContentBlock anthropicBlock     `json:"content_block"`
	Message      *anthropicResponse `json:"message"`
	Usage        *anthropicUsage    `json:"usage"`
}

// streamBlock is one content block being reassembled from its deltas.
type streamBlock struct {
	block    anthropicBlock
	text     strings.Builder
	thinking strings.Builder
	args     strings.Builder
}

// raw renders the reassembled block the way a single response would carry it.
func (s *streamBlock) raw() (json.RawMessage, bool) {
	var v any
	switch s.block.Type {
	case "text":
		v = map[string]any{"type": "text", "text": s.text.String()}
	case "thinking":
		v = map[string]any{"type": "thinking", "thinking": s.thinking.String(), "signature": s.block.Signature}
	case "redacted_thinking":
		v = map[string]any{"type": "redacted_thinking", "data": s.block.Data}
	case "tool_use":
		input := json.RawMessage(s.args.String())
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		if !json.Valid(input) {
			return nil, false
		}
		v = map[string]any{"type": "tool_use", "id": s.block.ID, "name": s.block.Name, "input": input}
	default:
		return nil, false
	}
	encoded, err := json.Marshal(v)
	return encoded, err == nil
}

// readStream reassembles Anthropic events into the same complete result shape.
func (a *AnthropicMessages) readStream(body io.Reader, sink Sink, deployment string) (Result, error) {
	scanner := eventLines(body, maxResultBytes)
	var (
		result   Result
		usage    anthropicUsage
		blocks   []*streamBlock
		byIndex  = map[int]*streamBlock{}
		finished bool
		stopped  bool
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event anthropicEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_chunk", Detail: err.Error()}
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				result.Model, result.ProviderRequestID = event.Message.Model, event.Message.ID
				usage.merge(event.Message.Usage)
			}
		case "content_block_start":
			block := &streamBlock{block: event.ContentBlock}
			block.text.WriteString(event.ContentBlock.Text)
			block.thinking.WriteString(event.ContentBlock.Thinking)
			byIndex[event.Index] = block
			blocks = append(blocks, block)
		case "content_block_delta":
			block, ok := byIndex[event.Index]
			if !ok {
				return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "orphan_delta", Detail: line}
			}
			switch event.Delta.Type {
			case "text_delta":
				block.text.WriteString(event.Delta.Text)
				emit(sink, DeltaText, event.Delta.Text)
			case "input_json_delta":
				block.args.WriteString(event.Delta.PartialJSON)
			case "thinking_delta":
				block.thinking.WriteString(event.Delta.Thinking)
			case "signature_delta":
				block.block.Signature += event.Delta.Signature
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				result.FinishReason = mapAnthropicStop(event.Delta.StopReason)
				finished = true
			}
			usage.merge(event.Usage)
		case "message_stop":
			stopped = true
		case "error":
			return Result{}, &ProviderError{Outcome: OutcomeUnknown, Class: "stream_error", Detail: line}
		}
		if stopped {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, classifyTransport(err)
	}
	if !finished || !stopped {
		return Result{}, &ProviderError{Outcome: OutcomeUnknown, Class: "incomplete_stream", Detail: "stream ended before message_stop"}
	}
	result.Usage = mapAnthropicUsage(&usage)
	var text strings.Builder
	thought := false
	for i, block := range blocks {
		switch block.block.Type {
		case "text":
			text.WriteString(block.text.String())
		case "tool_use":
			arguments := json.RawMessage(block.args.String())
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: block.block.ID, Index: i, Name: block.block.Name, Arguments: arguments})
		case "thinking", "redacted_thinking":
			thought = true
		}
	}
	result.Text = text.String()
	if thought {
		content := make([]json.RawMessage, 0, len(blocks))
		for _, block := range blocks {
			raw, ok := block.raw()
			if !ok {
				// Truncated arguments cannot be replayed; neither can the
				// thinking that led to them.
				content = nil
				break
			}
			content = append(content, raw)
		}
		if content != nil {
			result.Reasoning = recordAnthropicReasoning(deployment, result.Model, content)
		}
	}
	return result, nil
}

func mapAnthropicStop(reason string) FinishReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return FinishStop
	case "tool_use":
		return FinishToolCalls
	case "max_tokens":
		return FinishLength
	case "refusal":
		return FinishRefused
	default:
		return FinishError
	}
}

// mapAnthropicUsage converts usage into the contract's disjoint parts. Here
// input_tokens excludes cache reads and writes, so both are added back.
//
// Cache writes are billed above fresh input (1.25x for the five-minute cache)
// but the contract has only cached and uncached input, so they count as
// uncached: a deployment of this adapter must set its uncached rate to the
// cache-write rate, or every write is undercharged. Fresh input is then booked
// a quarter high — an upper bound, as the relay bills less anyway.
//
// A converted relay channel reports only input_tokens_details.cached_tokens.
// It is read the same way, as excluded from input_tokens, which can only
// overstate the input if the channel meant it otherwise.
func mapAnthropicUsage(u *anthropicUsage) UsageEvidence {
	if u == nil {
		return UsageEvidence{}
	}
	evidence := UsageEvidence{OutputTokens: u.OutputTokens}
	if u.OutputDetails != nil {
		evidence.ReasoningTokens = u.OutputDetails.ThinkingTokens
	}
	if u.InputTokens == nil {
		return evidence
	}
	total := *u.InputTokens
	var cached *int64
	switch {
	case u.CacheReadInputTokens != nil:
		cached = u.CacheReadInputTokens
	case u.InputDetails != nil:
		cached = u.InputDetails.CachedTokens
	}
	if cached != nil {
		read := *cached
		total += read
		evidence.InputCachedTokens = &read
	}
	if u.CacheCreationInputTokens != nil {
		total += *u.CacheCreationInputTokens
	}
	evidence.InputTotalTokens = &total
	return evidence
}
