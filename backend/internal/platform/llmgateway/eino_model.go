package llmgateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ModelSession binds one Eino model adapter to a single account, caller group
// and budget ceiling. Summarization and other auxiliary calls use the same
// session, so they can never become a second, unmetered model exit.
type ModelSession struct {
	CallSession
	Stream bool
	// TurnKey names the caller's own durable step for the turn about to run. The
	// framework drives Generate without any notion of resumption, so this is the
	// only place a persisted step identity can enter; there is deliberately no
	// default, because a per-process one would make every resume pay again.
	TurnKey func(context.Context, ChatRequest) (string, error)
	// Consume joins the caller's own persistence to the transaction that marks
	// the result consumed. A run that records steps must set it; a call with no
	// business row of its own may leave it nil.
	Consume func(store.TxAccountScope, Result) error
}

func (s ModelSession) validate() error {
	if err := s.CallSession.validate(); err != nil {
		return err
	}
	if s.TurnKey == nil {
		return fmt.Errorf("%w: model session needs a durable turn key", ErrValidation)
	}
	return nil
}

// GatewayModelAdapter implements the fixed Eino chat model interface on top of
// the gateway's admission, dispatch and accounting protocol. Framework retries,
// failover and per-skill model overrides stay disabled by construction.
type GatewayModelAdapter struct {
	gateway *Service
	session ModelSession
	tools   []*schema.ToolInfo
}

// NewEinoModel binds a session. Each run builds its own adapter.
func (s *Service) NewEinoModel(session ModelSession) (*GatewayModelAdapter, error) {
	if err := session.validate(); err != nil {
		return nil, err
	}
	if _, err := s.catalog.Model(session.ModelKey); err != nil {
		return nil, err
	}
	return &GatewayModelAdapter{gateway: s, session: session}, nil
}

// WithTools returns an isolated configuration instead of mutating the shared
// instance, as the Eino contract requires.
func (a *GatewayModelAdapter) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	next := &GatewayModelAdapter{gateway: a.gateway, session: a.session, tools: append([]*schema.ToolInfo(nil), tools...)}
	if _, err := next.toolDefinitions(); err != nil {
		return nil, err
	}
	return next, nil
}

// Generate runs one complete gateway turn: admission, one-shot dispatch,
// persisted complete result, then accounting.
func (a *GatewayModelAdapter) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	result, err := a.call(ctx, input, a.session.Stream)
	if err != nil {
		return nil, err
	}
	return toEinoMessage(result), nil
}

// Stream reassembles provider fragments inside the gateway and hands back the
// single validated result. Fragment-level product streaming is the Harness's
// job; a fragment is never a persisted result here.
func (a *GatewayModelAdapter) Stream(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := a.call(ctx, input, true)
	if err != nil {
		return nil, err
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	writer.Send(toEinoMessage(result), nil)
	writer.Close()
	return reader, nil
}

func (a *GatewayModelAdapter) call(ctx context.Context, input []*schema.Message, stream bool) (Result, error) {
	messages, err := fromEinoMessages(input)
	if err != nil {
		return Result{}, err
	}
	tools, err := a.toolDefinitions()
	if err != nil {
		return Result{}, err
	}
	chat := ChatRequest{
		ContractVersion: ContractVersion,
		ModelKey:        a.session.ModelKey,
		Messages:        messages,
		Tools:           tools,
		OutputLimit:     a.session.OutputLimit,
	}
	// The framework has no notion of resumption, so the durable step identity is
	// asked for here and the coordinator does the rest: a replayed turn returns
	// the result the earlier run already paid for.
	bindingKey, err := a.session.TurnKey(ctx, chat)
	if err != nil {
		return Result{}, err
	}
	outcome, err := a.gateway.Call(ctx, a.session.CallSession, CallInput{
		BindingKey: bindingKey,
		Chat:       chat,
		Stream:     stream,
		Consume:    a.session.Consume,
	})
	if err != nil {
		return Result{}, err
	}
	return outcome.Result, nil
}

func (a *GatewayModelAdapter) toolDefinitions() ([]ToolDefinition, error) {
	definitions := make([]ToolDefinition, 0, len(a.tools))
	for _, t := range a.tools {
		if t == nil || t.ParamsOneOf == nil {
			return nil, fmt.Errorf("%w: tool %q has no parameter schema", ErrValidation, safeToolName(t))
		}
		jsonSchema, err := t.ToJSONSchema()
		if err != nil {
			return nil, fmt.Errorf("%w: tool %q schema: %w", ErrValidation, t.Name, err)
		}
		raw, err := json.Marshal(jsonSchema)
		if err != nil {
			return nil, err
		}
		normalized, err := closeObjectSchemas(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: tool %q schema: %w", ErrValidation, t.Name, err)
		}
		definitions = append(definitions, ToolDefinition{
			Name:          t.Name,
			Description:   t.Desc,
			InputSchema:   normalized,
			SchemaVersion: 1,
		})
	}
	return definitions, nil
}

// closeObjectSchemas tightens a framework-produced schema to the tested subset.
// Eino's parameter model cannot express additionalProperties at all, so closing
// every object is a faithful projection of "these are the parameters" rather
// than an override of a caller's choice. Anything else stays untouched and is
// still rejected by the contract validator if it is outside the subset.
func closeObjectSchemas(raw json.RawMessage) (json.RawMessage, error) {
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, err
	}
	closeObjects(node)
	return json.Marshal(node)
}

func closeObjects(node any) {
	object, ok := node.(map[string]any)
	if !ok {
		return
	}
	if kind, _ := object["type"].(string); kind == "object" {
		if _, set := object["additionalProperties"]; !set {
			object["additionalProperties"] = false
		}
	}
	for _, key := range []string{"properties", "items"} {
		switch child := object[key].(type) {
		case map[string]any:
			if key == "properties" {
				for _, value := range child {
					closeObjects(value)
				}
				continue
			}
			closeObjects(child)
		}
	}
}

func safeToolName(t *schema.ToolInfo) string {
	if t == nil {
		return ""
	}
	return t.Name
}

func fromEinoMessages(input []*schema.Message) ([]Message, error) {
	messages := make([]Message, 0, len(input))
	for _, m := range input {
		if m == nil {
			return nil, fmt.Errorf("%w: nil message", ErrValidation)
		}
		converted := Message{ToolCallID: m.ToolCallID}
		switch m.Role {
		case schema.System:
			converted.Role = RoleSystem
		case schema.User:
			converted.Role = RoleUser
		case schema.Assistant:
			converted.Role = RoleAssistant
		case schema.Tool:
			converted.Role = RoleTool
		default:
			return nil, fmt.Errorf("%w: unsupported role %q", ErrValidation, m.Role)
		}
		if len(m.UserInputMultiContent) > 0 || len(m.MultiContent) > 0 {
			// Media must arrive as an authorized, pinned handle through the
			// caller's own context builder, never as framework state.
			return nil, fmt.Errorf("%w: multimodal parts must be bound by the caller", ErrCapability)
		}
		if m.Content != "" {
			converted.Blocks = append(converted.Blocks, Block{Kind: BlockText, Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			index := 0
			if c.Index != nil {
				index = *c.Index
			}
			arguments := json.RawMessage(c.Function.Arguments)
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			converted.ToolCalls = append(converted.ToolCalls, ToolCall{
				ID: c.ID, Index: index, Name: c.Function.Name, Arguments: arguments,
			})
		}
		messages = append(messages, converted)
	}
	return messages, nil
}

func toEinoMessage(r Result) *schema.Message {
	message := &schema.Message{
		Role:    schema.Assistant,
		Content: r.Text,
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: string(r.FinishReason),
		},
	}
	if r.Usage.InputTotalTokens != nil && r.Usage.OutputTokens != nil {
		usage := &schema.TokenUsage{
			PromptTokens:     int(*r.Usage.InputTotalTokens),
			CompletionTokens: int(*r.Usage.OutputTokens),
			TotalTokens:      int(*r.Usage.InputTotalTokens + *r.Usage.OutputTokens),
		}
		message.ResponseMeta.Usage = usage
	}
	if !r.Consumable() {
		// length/error truncates arguments, so the framework must never see a
		// tool call it could decide to execute.
		return message
	}
	for i := range r.ToolCalls {
		call := r.ToolCalls[i]
		index := call.Index
		message.ToolCalls = append(message.ToolCalls, schema.ToolCall{
			Index: &index,
			ID:    call.ID,
			Type:  "function",
			Function: schema.FunctionCall{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return message
}

// EstimateInputTokens is the built-in conservative bound over everything that
// really enters the prompt: messages, tool schemas and bounded media. It
// deliberately over-counts, because an under-estimated hold would admit a call
// the budget cannot actually cover.
func EstimateInputTokens(r ChatRequest) int64 {
	var bytes int64
	for _, t := range r.Tools {
		// Tool definitions are sent on every turn and are part of the input.
		bytes += int64(len(t.Name) + len(t.Description) + len(t.InputSchema))
	}
	for _, m := range r.Messages {
		bytes += 16
		for _, b := range m.Blocks {
			if b.Kind == BlockText {
				bytes += int64(len(b.Text))
				continue
			}
			if b.Image != nil {
				// No measured byte-to-token mapping exists yet, so image input
				// stays disabled in the catalog. This bound is only a floor for
				// deployments that enable it with their own verified mapping.
				bytes += 4096
			}
		}
		for _, c := range m.ToolCalls {
			bytes += int64(len(c.Arguments)) + int64(len(c.Name))
		}
	}
	// One token per byte is an upper bound for UTF-8 text in every language.
	return bytes
}
