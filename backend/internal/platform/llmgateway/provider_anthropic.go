package llmgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// AnthropicMessages adapts the Anthropic Messages protocol. It exists so the
// internal contract is provably independent of the first vendor's wire format;
// enabling a real Anthropic deployment still needs its own admission evidence.
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

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	ToolChoice  map[string]string  `json:"tool_choice,omitempty"`
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

type anthropicUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicResponse struct {
	ID         string           `json:"id"`
	Model      string           `json:"model"`
	StopReason string           `json:"stop_reason"`
	Content    []anthropicBlock `json:"content"`
	Usage      *anthropicUsage  `json:"usage"`
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
		result, err = a.readStream(resp.Body)
	} else {
		result, err = a.readSingle(resp.Body)
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
		Model:       p.Snapshot.RequestModelID,
		MaxTokens:   p.Chat.OutputLimit,
		Stream:      p.Stream,
		Temperature: p.Chat.Temperature,
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
	var system strings.Builder
	for _, m := range p.Chat.Messages {
		if m.Role == RoleSystem {
			for _, b := range m.Blocks {
				system.WriteString(b.Text)
			}
			continue
		}
		encoded, err := encodeAnthropicMessage(ctx, m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, encoded)
	}
	out.System = system.String()
	return json.Marshal(out)
}

func encodeAnthropicMessage(ctx context.Context, m Message) (anthropicMessage, error) {
	out := anthropicMessage{Role: string(m.Role)}
	if m.Role == RoleTool {
		out.Role = "user"
		var text strings.Builder
		for _, b := range m.Blocks {
			text.WriteString(b.Text)
		}
		out.Content = append(out.Content, map[string]any{
			"type": "tool_result", "tool_use_id": m.ToolCallID, "content": text.String(),
		})
		return out, nil
	}
	for _, b := range m.Blocks {
		switch b.Kind {
		case BlockText:
			out.Content = append(out.Content, map[string]any{"type": "text", "text": b.Text})
		case BlockImageInput:
			encoded, err := readImage(ctx, b.Image)
			if err != nil {
				return anthropicMessage{}, err
			}
			out.Content = append(out.Content, map[string]any{
				"type":   "image",
				"source": map[string]any{"type": "base64", "media_type": b.Image.MIME, "data": encoded},
			})
		}
	}
	for _, c := range m.ToolCalls {
		var input any
		if err := json.Unmarshal(c.Arguments, &input); err != nil {
			return anthropicMessage{}, fmt.Errorf("%w: assistant tool call arguments", ErrValidation)
		}
		out.Content = append(out.Content, map[string]any{
			"type": "tool_use", "id": c.ID, "name": c.Name, "input": input,
		})
	}
	return out, nil
}

func (a *AnthropicMessages) readSingle(body io.Reader) (Result, error) {
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
	for i, block := range payload.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			arguments := block.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{ID: block.ID, Index: i, Name: block.Name, Arguments: arguments})
		}
	}
	result.Text = text.String()
	return result, nil
}

type anthropicEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	ContentBlock anthropicBlock     `json:"content_block"`
	Message      *anthropicResponse `json:"message"`
	Usage        *anthropicUsage    `json:"usage"`
}

// readStream reassembles Anthropic events into the same complete result shape.
func (a *AnthropicMessages) readStream(body io.Reader) (Result, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResultBytes)
	var (
		result   Result
		text     strings.Builder
		calls    []*ToolCall
		args     = map[int]*strings.Builder{}
		byIndex  = map[int]*ToolCall{}
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
				result.Usage = mapAnthropicUsage(event.Message.Usage)
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				call := &ToolCall{ID: event.ContentBlock.ID, Index: event.Index, Name: event.ContentBlock.Name}
				byIndex[event.Index], args[event.Index] = call, &strings.Builder{}
				calls = append(calls, call)
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				text.WriteString(event.Delta.Text)
			case "input_json_delta":
				if builder, ok := args[event.Index]; ok {
					builder.WriteString(event.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				result.FinishReason = mapAnthropicStop(event.Delta.StopReason)
				finished = true
			}
			if event.Usage != nil {
				result.Usage = mergeAnthropicUsage(result.Usage, event.Usage)
			}
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
	result.Text = text.String()
	for _, call := range calls {
		complete := *call
		complete.Arguments = json.RawMessage(args[call.Index].String())
		if len(complete.Arguments) == 0 {
			complete.Arguments = json.RawMessage("{}")
		}
		result.ToolCalls = append(result.ToolCalls, complete)
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

func mapAnthropicUsage(u *anthropicUsage) UsageEvidence {
	if u == nil {
		return UsageEvidence{}
	}
	evidence := UsageEvidence{OutputTokens: u.OutputTokens}
	if u.InputTokens != nil {
		total := *u.InputTokens
		if u.CacheReadInputTokens != nil {
			total += *u.CacheReadInputTokens
			cached := *u.CacheReadInputTokens
			evidence.InputCachedTokens = &cached
		}
		if u.CacheCreationInputTokens != nil {
			// Cache writes are billed above ordinary input by this vendor, but
			// the contract only has cached/uncached components today, so they
			// are counted as uncached. Enabling a real Anthropic deployment
			// requires its own price component first, or this undercounts.
			total += *u.CacheCreationInputTokens
		}
		evidence.InputTotalTokens = &total
	}
	return evidence
}

func mergeAnthropicUsage(base UsageEvidence, u *anthropicUsage) UsageEvidence {
	next := mapAnthropicUsage(u)
	if next.InputTotalTokens == nil {
		next.InputTotalTokens = base.InputTotalTokens
	}
	if next.InputCachedTokens == nil {
		next.InputCachedTokens = base.InputCachedTokens
	}
	if next.OutputTokens == nil {
		next.OutputTokens = base.OutputTokens
	}
	return next
}
