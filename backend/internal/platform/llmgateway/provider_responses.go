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

// OpenAIResponses adapts the OpenAI Responses protocol. Reasoning models are
// served through it rather than chat completions because only here can their
// reasoning travel back to later turns: chat completions drops it, which the
// vendor documents as degrading multi-step tool use.
//
// Every request is stateless (store=false) and asks for the encrypted
// reasoning items, which come back verbatim on the next turn of the same model.
type OpenAIResponses struct{ client *http.Client }

// NewOpenAIResponses builds the adapter with implicit retries disabled.
func NewOpenAIResponses(client *http.Client) *OpenAIResponses {
	return &OpenAIResponses{client: client}
}

func (a *OpenAIResponses) Key() ProviderKey { return ProviderOpenAIResponses }

// responsesReasoningFormat marks reasoning this adapter produced. Reasoning is
// opaque model context; only the adapter that wrote it can read it back, and
// only to the deployment and model that produced it: the sealed items are bound
// to the upstream organisation, so the same model behind another vendor would
// refuse them with a 400 that may not be failed over.
const responsesReasoningFormat = "openai_responses"

type responsesReasoning struct {
	Format     string            `json:"format"`
	Deployment string            `json:"deployment"`
	Model      string            `json:"model"`
	Items      []json.RawMessage `json:"items"`
}

// decodeResponsesReasoning reads reasoning back. Anything else — another
// vendor's plain text, or nothing — is not this adapter's to send.
func decodeResponsesReasoning(raw string) (responsesReasoning, bool) {
	if !strings.HasPrefix(raw, "{") {
		return responsesReasoning{}, false
	}
	var r responsesReasoning
	if json.Unmarshal([]byte(raw), &r) != nil || r.Format != responsesReasoningFormat || len(r.Items) == 0 {
		return responsesReasoning{}, false
	}
	return r, true
}

type responsesRequest struct {
	Model string `json:"model"`
	// Instructions carries the leading system messages. A relay may fill an
	// absent one with its own prompt — AiCodeWith injects a ~4,300-token
	// coding-agent prompt that makes the model introduce itself as Codex
	// (measured 2026-10-09) — so the caller's system text must travel here.
	Instructions    string             `json:"instructions,omitempty"`
	Input           []any              `json:"input"`
	MaxOutputTokens int                `json:"max_output_tokens"`
	Stream          bool               `json:"stream,omitempty"`
	Store           bool               `json:"store"`
	Include         []string           `json:"include,omitempty"`
	Reasoning       *responsesThinking `json:"reasoning,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	Tools           []responsesTool    `json:"tools,omitempty"`
	ToolChoice      string             `json:"tool_choice,omitempty"`
	Text            *responsesText     `json:"text,omitempty"`
}

type responsesThinking struct {
	Effort string `json:"effort"`
	// Context asks for every earlier turn's reasoning to stay in view, which
	// is the point of handing it back at all.
	Context string `json:"context"`
}

type responsesText struct {
	Format responseFormat `json:"format"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type responsesUsage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	InputDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responsesItem struct {
	Type    string `json:"type"`
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Args    string `json:"arguments"`
	Content []struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Refusal string `json:"refusal"`
	} `json:"content"`
}

type responsesBody struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	Status     string            `json:"status"`
	Output     []json.RawMessage `json:"output"`
	Usage      *responsesUsage   `json:"usage"`
	Incomplete *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Invoke performs exactly one attempt against the deployment.
func (a *OpenAIResponses) Invoke(ctx context.Context, p ProviderRequest) (Result, error) {
	body, err := a.encode(ctx, p)
	if err != nil {
		return Result{}, err
	}
	endpoint := strings.TrimSuffix(p.Model.BaseURL, "/") + "/responses"
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
	var final responsesBody
	if p.Stream {
		final, err = readResponsesStream(resp.Body, p.Sink)
	} else {
		final, err = readResponsesSingle(resp.Body)
	}
	if err != nil {
		return Result{}, err
	}
	result, err := responsesResult(final, p.Model.DeploymentKey)
	if err != nil {
		return Result{}, err
	}
	if err := validateResult(result, p.Snapshot); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (a *OpenAIResponses) encode(ctx context.Context, p ProviderRequest) ([]byte, error) {
	out := responsesRequest{
		Model:           p.Snapshot.RequestModelID,
		MaxOutputTokens: p.Chat.OutputLimit,
		Stream:          p.Stream,
		Temperature:     p.Chat.Temperature,
		ToolChoice:      p.Chat.ToolChoice,
	}
	if p.Model.ReasoningEffort != "" {
		out.Include = []string{"reasoning.encrypted_content"}
		out.Reasoning = &responsesThinking{Effort: p.Model.ReasoningEffort, Context: "all_turns"}
	}
	if p.Chat.ResponseFormat == "json_object" {
		out.Text = &responsesText{Format: responseFormat{Type: "json_object"}}
	}
	for _, t := range p.Chat.Tools {
		out.Tools = append(out.Tools, responsesTool{
			Type: "function", Name: t.Name, Description: t.Description, Parameters: t.InputSchema,
		})
	}
	messages := p.Chat.Messages
	var instructions []string
	for len(messages) > 0 && messages[0].Role == RoleSystem && onlyText(messages[0].Blocks) {
		var text strings.Builder
		for _, b := range messages[0].Blocks {
			text.WriteString(b.Text)
		}
		instructions = append(instructions, text.String())
		messages = messages[1:]
	}
	out.Instructions = strings.Join(instructions, "\n\n")
	// A system message later in the conversation keeps its place in input.
	for _, m := range messages {
		items, err := encodeResponsesMessage(ctx, m, p.Model.DeploymentKey, p.Snapshot.RequestModelID)
		if err != nil {
			return nil, err
		}
		out.Input = append(out.Input, items...)
	}
	return json.Marshal(out)
}

// encodeResponsesMessage turns one contract message into input items. An
// assistant turn is its reasoning, then its words, then its calls — the order
// the model produced them in.
func encodeResponsesMessage(ctx context.Context, m Message, deployment, model string) ([]any, error) {
	switch m.Role {
	case RoleAssistant:
		var items []any
		if r, ok := decodeResponsesReasoning(m.Reasoning); ok && r.Deployment == deployment && r.Model == model {
			for _, item := range r.Items {
				items = append(items, item)
			}
		}
		var text strings.Builder
		for _, b := range m.Blocks {
			text.WriteString(b.Text)
		}
		if text.Len() > 0 {
			items = append(items, map[string]any{
				"type": "message", "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": text.String()}},
			})
		}
		for _, c := range m.ToolCalls {
			items = append(items, map[string]any{
				"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": string(c.Arguments),
			})
		}
		return items, nil
	case RoleTool:
		parts, onlyText, err := responsesParts(ctx, m.Blocks)
		if err != nil {
			return nil, err
		}
		var output any = parts
		if onlyText {
			var text strings.Builder
			for _, b := range m.Blocks {
				text.WriteString(b.Text)
			}
			output = text.String()
		}
		return []any{map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": output}}, nil
	default:
		parts, _, err := responsesParts(ctx, m.Blocks)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"role": string(m.Role), "content": parts}}, nil
	}
}

func onlyText(blocks []Block) bool {
	for _, b := range blocks {
		if b.Kind != BlockText {
			return false
		}
	}
	return true
}

func responsesParts(ctx context.Context, blocks []Block) ([]map[string]any, bool, error) {
	parts := make([]map[string]any, 0, len(blocks))
	onlyText := true
	for _, b := range blocks {
		switch b.Kind {
		case BlockText:
			parts = append(parts, map[string]any{"type": "input_text", "text": b.Text})
		case BlockImageInput:
			onlyText = false
			encoded, err := readImage(ctx, b.Image)
			if err != nil {
				return nil, false, err
			}
			parts = append(parts, map[string]any{
				"type": "input_image", "image_url": "data:" + b.Image.MIME + ";base64," + encoded,
			})
		}
	}
	return parts, onlyText, nil
}

// maxResponsesBytes bounds one complete answer. It is wider than chat
// completions because encrypted reasoning rides along with the text.
const maxResponsesBytes = 4 << 20

func readResponsesSingle(body io.Reader) (responsesBody, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxResponsesBytes))
	if err != nil {
		return responsesBody{}, classifyTransport(err)
	}
	var payload responsesBody
	if err := json.Unmarshal(raw, &payload); err != nil {
		return responsesBody{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_body", Detail: err.Error()}
	}
	return payload, nil
}

// readResponsesStream forwards text deltas while the answer streams, but the
// result is only ever the terminal event's complete response object.
func readResponsesStream(body io.Reader, sink Sink) (responsesBody, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResponsesBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event struct {
			Type     string        `json:"type"`
			Delta    string        `json:"delta"`
			Response responsesBody `json:"response"`
			Message  string        `json:"message"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return responsesBody{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_chunk", Detail: err.Error()}
		}
		switch event.Type {
		case "response.output_text.delta":
			emit(sink, DeltaText, event.Delta)
		case "response.completed", "response.incomplete", "response.failed":
			return event.Response, nil
		case "error":
			// A mid-stream error may come after the request was processed.
			return responsesBody{}, &ProviderError{Outcome: OutcomeUnknown, Class: "stream_error", Detail: summarizeDetail([]byte(event.Message))}
		}
	}
	if err := scanner.Err(); err != nil {
		return responsesBody{}, classifyTransport(err)
	}
	// A stream that stopped early proves nothing about billing.
	return responsesBody{}, &ProviderError{Outcome: OutcomeUnknown, Class: "incomplete_stream", Detail: "stream ended before a terminal event"}
}

func responsesResult(body responsesBody, deployment string) (Result, error) {
	result := Result{Model: body.Model, ProviderRequestID: body.ID, Usage: mapResponsesUsage(body.Usage)}
	var text strings.Builder
	var reasoning []json.RawMessage
	refused := false
	for _, raw := range body.Output {
		var item responsesItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "malformed_item", Detail: err.Error()}
		}
		switch item.Type {
		case "reasoning":
			reasoning = append(reasoning, raw)
		case "message":
			for _, c := range item.Content {
				text.WriteString(c.Text)
				if c.Type == "refusal" {
					refused = true
				}
			}
		case "function_call":
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID: item.CallID, Index: len(result.ToolCalls), Name: item.Name, Arguments: json.RawMessage(item.Args),
			})
		}
	}
	result.Text = text.String()
	if len(reasoning) > 0 {
		encoded, err := json.Marshal(responsesReasoning{Format: responsesReasoningFormat, Deployment: deployment, Model: body.Model, Items: reasoning})
		if err != nil {
			return Result{}, fmt.Errorf("encode reasoning: %w", err)
		}
		result.Reasoning = string(encoded)
	}
	switch {
	case body.Status == "failed":
		detail := ""
		if body.Error != nil {
			detail = body.Error.Code + ": " + body.Error.Message
		}
		return Result{}, &ProviderError{Outcome: OutcomeUnknown, Class: "response_failed", Detail: summarizeDetail([]byte(detail))}
	case body.Status == "incomplete":
		result.FinishReason = FinishError
		if body.Incomplete != nil {
			switch body.Incomplete.Reason {
			case "max_output_tokens":
				result.FinishReason = FinishLength
			case "content_filter":
				result.FinishReason = FinishRefused
			}
		}
	case body.Status != "completed":
		return Result{}, &ProviderError{Outcome: OutcomeProtocol, Class: "response_status", Detail: body.Status}
	case refused:
		result.FinishReason = FinishRefused
	case len(result.ToolCalls) > 0:
		result.FinishReason = FinishToolCalls
	default:
		result.FinishReason = FinishStop
	}
	return result, nil
}

func mapResponsesUsage(u *responsesUsage) UsageEvidence {
	if u == nil {
		return UsageEvidence{}
	}
	evidence := UsageEvidence{InputTotalTokens: u.InputTokens, OutputTokens: u.OutputTokens}
	if u.InputDetails != nil {
		evidence.InputCachedTokens = u.InputDetails.CachedTokens
	}
	if u.OutputDetails != nil {
		evidence.ReasoningTokens = u.OutputDetails.ReasoningTokens
	}
	return evidence
}
