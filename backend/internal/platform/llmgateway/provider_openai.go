package llmgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// OpenAICompatible adapts the OpenAI-shaped chat completions protocol used by
// DeepSeek and other compatible deployments.
type OpenAICompatible struct{ client *http.Client }

// NewOpenAICompatible builds the adapter with implicit retries disabled.
func NewOpenAICompatible(client *http.Client) *OpenAICompatible {
	return &OpenAICompatible{client: client}
}

func (a *OpenAICompatible) Key() ProviderKey { return ProviderOpenAICompatible }

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolRef `json:"tool_calls,omitempty"`
}

type openAIToolRef struct {
	ID       string `json:"id"`
	Index    *int   `json:"index,omitempty"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens"`
	Stream         bool            `json:"stream,omitempty"`
	StreamOptions  *streamOptions  `json:"stream_options,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	Tools          []openAITool    `json:"tools,omitempty"`
	ToolChoice     string          `json:"tool_choice,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Thinking       *thinkingOption `json:"thinking,omitempty"`
	N              int             `json:"n"`
}

// thinkingOption carries the deployment's fixed reasoning mode.
type thinkingOption struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openAIUsage struct {
	PromptTokens          *int64 `json:"prompt_tokens"`
	CompletionTokens      *int64 `json:"completion_tokens"`
	PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
	CompletionDetails     *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string          `json:"content"`
			ToolCalls []openAIToolRef `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content   string          `json:"content"`
			ToolCalls []openAIToolRef `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// Invoke performs exactly one attempt against the deployment.
func (a *OpenAICompatible) Invoke(ctx context.Context, p ProviderRequest) (Result, error) {
	body, err := a.encode(ctx, p)
	if err != nil {
		return Result{}, err
	}
	endpoint := strings.TrimSuffix(p.Model.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: build request: %w", ErrValidation, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", map[bool]string{true: "text/event-stream", false: "application/json"}[p.Stream])
	req.Header.Set("Authorization", "Bearer "+p.Credential)

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

func (a *OpenAICompatible) encode(ctx context.Context, p ProviderRequest) ([]byte, error) {
	out := openAIRequest{
		Model:       p.Snapshot.RequestModelID,
		MaxTokens:   p.Chat.OutputLimit,
		Stream:      p.Stream,
		Temperature: p.Chat.Temperature,
		ToolChoice:  p.Chat.ToolChoice,
		N:           1,
	}
	if p.Stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if p.Model.Thinking != "" {
		out.Thinking = &thinkingOption{Type: p.Model.Thinking}
	}
	if p.Chat.ResponseFormat == "json_object" {
		out.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	for _, t := range p.Chat.Tools {
		tool := openAITool{Type: "function"}
		tool.Function.Name, tool.Function.Description, tool.Function.Parameters = t.Name, t.Description, t.InputSchema
		out.Tools = append(out.Tools, tool)
	}
	for _, m := range p.Chat.Messages {
		encoded, err := encodeOpenAIMessage(ctx, m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, encoded)
	}
	return json.Marshal(out)
}

func encodeOpenAIMessage(ctx context.Context, m Message) (openAIMessage, error) {
	out := openAIMessage{Role: string(m.Role), ToolCallID: m.ToolCallID}
	for _, c := range m.ToolCalls {
		ref := openAIToolRef{ID: c.ID, Type: "function"}
		ref.Function.Name, ref.Function.Arguments = c.Name, string(c.Arguments)
		out.ToolCalls = append(out.ToolCalls, ref)
	}
	onlyText := true
	for _, b := range m.Blocks {
		if b.Kind != BlockText {
			onlyText = false
		}
	}
	if onlyText {
		var text strings.Builder
		for _, b := range m.Blocks {
			text.WriteString(b.Text)
		}
		if text.Len() > 0 || len(out.ToolCalls) == 0 {
			out.Content = text.String()
		}
		return out, nil
	}
	parts := make([]map[string]any, 0, len(m.Blocks))
	for _, b := range m.Blocks {
		switch b.Kind {
		case BlockText:
			parts = append(parts, map[string]any{"type": "text", "text": b.Text})
		case BlockImageInput:
			encoded, err := readImage(ctx, b.Image)
			if err != nil {
				return openAIMessage{}, err
			}
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "data:" + b.Image.MIME + ";base64," + encoded},
			})
		}
	}
	out.Content = parts
	return out, nil
}

func (a *OpenAICompatible) readSingle(body io.Reader) (Result, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxResultBytes))
	if err != nil {
		return Result{}, classifyTransport(err)
	}
	var payload openAIResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_body", Detail: err.Error()}
	}
	if len(payload.Choices) == 0 {
		return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "no_choice", Detail: payload.ID}
	}
	choice := payload.Choices[0]
	result := Result{
		Text:              choice.Message.Content,
		FinishReason:      mapOpenAIFinish(choice.FinishReason),
		Model:             payload.Model,
		ProviderRequestID: payload.ID,
		Usage:             mapOpenAIUsage(payload.Usage),
	}
	for i, c := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        c.ID,
			Index:     i,
			Name:      c.Function.Name,
			Arguments: json.RawMessage(c.Function.Arguments),
		})
	}
	return result, nil
}

const maxResultBytes = 256 << 10

// readStream reassembles fragments by choice and tool index. A fragment stream
// is never a result: only a fully terminated, validated answer is returned.
func (a *OpenAICompatible) readStream(body io.Reader) (Result, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResultBytes)
	var (
		text      strings.Builder
		result    Result
		calls     = map[int]*ToolCall{}
		args      = map[int]*strings.Builder{}
		names     = map[int]*strings.Builder{}
		finished  bool
		sawChunk  bool
		doneToken bool
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			doneToken = true
			break
		}
		var chunk openAIResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_chunk", Detail: err.Error()}
		}
		sawChunk = true
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.ID != "" {
			result.ProviderRequestID = chunk.ID
		}
		if chunk.Usage != nil {
			result.Usage = mapOpenAIUsage(chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				// n=1 is fixed by the contract; extra choices are a protocol break.
				return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "unexpected_choice", Detail: payload}
			}
			text.WriteString(choice.Delta.Content)
			for _, fragment := range choice.Delta.ToolCalls {
				index := 0
				if fragment.Index != nil {
					index = *fragment.Index
				}
				call, ok := calls[index]
				if !ok {
					call = &ToolCall{Index: index}
					calls[index] = call
					args[index], names[index] = &strings.Builder{}, &strings.Builder{}
				}
				if fragment.ID != "" {
					call.ID = fragment.ID
				}
				// Name and argument deltas are concatenated, never replaced:
				// a provider may split either across chunks.
				names[index].WriteString(fragment.Function.Name)
				args[index].WriteString(fragment.Function.Arguments)
			}
			if choice.FinishReason != "" {
				result.FinishReason = mapOpenAIFinish(choice.FinishReason)
				finished = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, classifyTransport(err)
	}
	if !sawChunk || !finished || !doneToken {
		// A stream that stopped early proves nothing about billing.
		return Result{}, &ProviderError{Outcome: OutcomeUnknown, Class: "incomplete_stream", Detail: "stream ended before a terminal chunk"}
	}
	result.Text = text.String()
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := *calls[index]
		call.Name = names[index].String()
		call.Arguments = json.RawMessage(args[index].String())
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		result.ToolCalls = append(result.ToolCalls, call)
	}
	return result, nil
}

func mapOpenAIFinish(reason string) FinishReason {
	switch reason {
	case "stop":
		return FinishStop
	case "tool_calls", "function_call":
		return FinishToolCalls
	case "length", "max_tokens":
		return FinishLength
	case "content_filter":
		return FinishRefused
	default:
		return FinishError
	}
}

func mapOpenAIUsage(u *openAIUsage) UsageEvidence {
	if u == nil {
		return UsageEvidence{}
	}
	evidence := UsageEvidence{InputTotalTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
	if u.PromptCacheHitTokens != nil {
		evidence.InputCachedTokens = u.PromptCacheHitTokens
	}
	if u.CompletionDetails != nil {
		evidence.ReasoningTokens = u.CompletionDetails.ReasoningTokens
	}
	return evidence
}

func summarizeDetail(raw []byte) string {
	detail := strings.TrimSpace(string(raw))
	if len(detail) > 500 {
		detail = detail[:500]
	}
	return detail
}
