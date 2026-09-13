// Package llmgateway owns model admission, one-shot dispatch identity, complete
// results and the spend ledger for every server-side model call. Business
// packages never receive a provider client, credential or raw provider payload.
package llmgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// ContractVersion is the business-stable shape of a request and its result.
// Providers are adapted to it; it is never widened to fit one provider.
const ContractVersion = "v1"

var (
	ErrValidation   = errors.New("invalid gateway request")
	ErrCapability   = errors.New("model does not support this request")
	ErrNotFound     = errors.New("gateway record not found")
	ErrState        = errors.New("gateway state does not allow this action")
	ErrBudget       = errors.New("budget exceeded")
	ErrConflict     = errors.New("gateway identity conflict")
	ErrProtocol     = errors.New("provider violated the model contract")
	ErrUnknown      = errors.New("model result unknown")
	ErrUnavailable  = errors.New("model deployment unavailable")
	ErrRateLimited  = errors.New("provider admission unavailable")
	ErrDeadline     = errors.New("gateway deadline exceeded")
	ErrCancelled    = errors.New("gateway request cancelled")
	ErrAttemptsUsed = errors.New("gateway attempts exhausted")
)

// Role is the closed message role set. Provider-specific roles are adapted.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// BlockKind is the closed content block set for one message.
type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockImageInput BlockKind = "image_input"
)

// ImageInput is a bounded media handle the caller already authorized and pinned.
// The adapter only reads the stream this handle opens; it never resolves an
// asset ID or an arbitrary HTTP URL, and the transient read never enters the
// persisted request hash.
type ImageInput struct {
	RevisionID string
	Digest     string
	MIME       string
	ByteSize   int64
	Open       func(context.Context) (io.ReadCloser, error) `json:"-"`
}

// Block is one content part of a message.
type Block struct {
	Kind  BlockKind
	Text  string
	Image *ImageInput
}

// ToolCall is a validated, complete structured call produced by the model.
type ToolCall struct {
	ID        string
	Index     int
	Name      string
	Arguments json.RawMessage
}

// Message is one contract-level conversation entry.
type Message struct {
	Role       Role
	Blocks     []Block
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolDefinition is the versioned schema the model may call. Harness validates
// arguments again against this same definition before any effect.
type ToolDefinition struct {
	Name          string
	Description   string
	InputSchema   json.RawMessage
	SchemaVersion int
}

// ChatRequest is the only request shape. Identity, deadline, budget and tracing
// travel in the control envelope, never inside model messages.
type ChatRequest struct {
	ContractVersion string
	ModelKey        string
	Messages        []Message
	Tools           []ToolDefinition
	ToolChoice      string
	OutputLimit     int
	Temperature     *float64
	ResponseFormat  string
}

// FinishReason is the closed completion classification.
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
	FinishLength    FinishReason = "length"
	FinishRefused   FinishReason = "refused"
	FinishError     FinishReason = "error"
)

// UsageEvidence carries measured dimensions. A missing dimension stays nil;
// it is never filled with zero.
type UsageEvidence struct {
	InputTotalTokens  *int64
	InputCachedTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
}

// Complete reports whether every billable dimension was reported.
func (u UsageEvidence) Complete() bool {
	return u.InputTotalTokens != nil && u.InputCachedTokens != nil && u.OutputTokens != nil
}

// Result is the single complete assistant answer. Stream fragments are never a
// result; only a reassembled, validated answer is persisted.
type Result struct {
	Text              string
	ToolCalls         []ToolCall
	FinishReason      FinishReason
	Usage             UsageEvidence
	Model             string
	ProviderRequestID string
}

// Consumable reports whether tool calls from this result may be executed.
// length/error truncate arguments, so locally valid JSON is still refused.
func (r Result) Consumable() bool {
	return r.FinishReason == FinishStop || r.FinishReason == FinishToolCalls
}

const (
	maxMessages      = 400
	maxTools         = 64
	maxTextBytes     = 1 << 20
	maxToolArgsBytes = 256 << 10
	maxOutputLimit   = 384000
)

var toolNamePattern = func(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Validate enforces the contract before any model capability check.
func (r ChatRequest) Validate() error {
	if r.ContractVersion != ContractVersion || r.ModelKey == "" {
		return fmt.Errorf("%w: contract version or model key", ErrValidation)
	}
	if len(r.Messages) == 0 || len(r.Messages) > maxMessages {
		return fmt.Errorf("%w: message count", ErrValidation)
	}
	if r.OutputLimit <= 0 || r.OutputLimit > maxOutputLimit {
		return fmt.Errorf("%w: output limit", ErrValidation)
	}
	if r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 2) {
		return fmt.Errorf("%w: temperature", ErrValidation)
	}
	switch r.ResponseFormat {
	case "", "text", "json_object":
	default:
		return fmt.Errorf("%w: response format", ErrValidation)
	}
	switch r.ToolChoice {
	case "", "auto", "none", "required":
	default:
		return fmt.Errorf("%w: tool choice", ErrValidation)
	}
	if len(r.Tools) > maxTools {
		return fmt.Errorf("%w: tool count", ErrValidation)
	}
	names := make(map[string]struct{}, len(r.Tools))
	for _, t := range r.Tools {
		if !toolNamePattern(t.Name) || t.Description == "" || t.SchemaVersion < 1 {
			return fmt.Errorf("%w: tool definition %q", ErrValidation, t.Name)
		}
		if _, dup := names[t.Name]; dup {
			return fmt.Errorf("%w: duplicate tool %q", ErrValidation, t.Name)
		}
		names[t.Name] = struct{}{}
		if err := validateToolSchema(t.InputSchema); err != nil {
			return fmt.Errorf("%w: tool %q schema: %w", ErrValidation, t.Name, err)
		}
	}
	seenCalls := make(map[string]struct{})
	for i, m := range r.Messages {
		if err := m.validate(seenCalls); err != nil {
			return fmt.Errorf("%w: message %d: %w", ErrValidation, i, err)
		}
	}
	return nil
}

func (m Message) validate(seenCalls map[string]struct{}) error {
	switch m.Role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
	default:
		return errors.New("unknown role")
	}
	if m.Role == RoleTool {
		if m.ToolCallID == "" {
			return errors.New("tool message without call id")
		}
		if _, ok := seenCalls[m.ToolCallID]; !ok {
			return errors.New("tool message references an unverified call id")
		}
	} else if m.ToolCallID != "" {
		return errors.New("only tool messages carry a call id")
	}
	if m.Role != RoleAssistant && len(m.ToolCalls) > 0 {
		return errors.New("only assistant messages carry tool calls")
	}
	for _, c := range m.ToolCalls {
		if c.ID == "" || !toolNamePattern(c.Name) || len(c.Arguments) > maxToolArgsBytes {
			return errors.New("invalid assistant tool call")
		}
		if !json.Valid(c.Arguments) {
			return errors.New("assistant tool call arguments are not json")
		}
		if _, dup := seenCalls[c.ID]; dup {
			return errors.New("duplicate tool call id")
		}
		seenCalls[c.ID] = struct{}{}
	}
	if len(m.Blocks) == 0 && len(m.ToolCalls) == 0 {
		return errors.New("empty message")
	}
	for _, b := range m.Blocks {
		switch b.Kind {
		case BlockText:
			if b.Image != nil || b.Text == "" || len(b.Text) > maxTextBytes || !utf8.ValidString(b.Text) {
				return errors.New("invalid text block")
			}
		case BlockImageInput:
			if b.Image == nil || b.Text != "" {
				return errors.New("invalid image block")
			}
			if b.Image.RevisionID == "" || b.Image.MIME == "" || b.Image.Open == nil {
				return errors.New("image block is not a bound media handle")
			}
			if !strings.HasPrefix(b.Image.Digest, "sha256-") || b.Image.ByteSize <= 0 {
				return errors.New("image block lacks a fixed digest")
			}
		default:
			return errors.New("unknown block kind")
		}
	}
	return nil
}

// validateToolSchema accepts only the tested JSON Schema subset. An unsupported
// keyword is refused instead of being silently dropped before dispatch.
func validateToolSchema(raw json.RawMessage) error {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(raw, &node); err != nil {
		return errors.New("schema is not an object")
	}
	return validateSchemaNode(node, true, 0)
}

func validateSchemaNode(node map[string]json.RawMessage, root bool, depth int) error {
	if depth > 8 {
		return errors.New("schema nested too deeply")
	}
	var kind string
	if raw, ok := node["type"]; !ok {
		return errors.New("schema node needs a type")
	} else if err := json.Unmarshal(raw, &kind); err != nil {
		return errors.New("schema type must be a string")
	}
	allowed := map[string]struct{}{"type": {}, "description": {}, "enum": {}}
	switch kind {
	case "object":
		for _, k := range []string{"properties", "required", "additionalProperties"} {
			allowed[k] = struct{}{}
		}
	case "array":
		for _, k := range []string{"items", "minItems", "maxItems"} {
			allowed[k] = struct{}{}
		}
	case "string":
		for _, k := range []string{"minLength", "maxLength", "format"} {
			allowed[k] = struct{}{}
		}
	case "integer", "number":
		for _, k := range []string{"minimum", "maximum"} {
			allowed[k] = struct{}{}
		}
	case "boolean":
	default:
		return fmt.Errorf("unsupported schema type %q", kind)
	}
	for k := range node {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("unsupported schema keyword %q", k)
		}
	}
	if root && kind != "object" {
		return errors.New("tool schema root must be an object")
	}
	if kind == "object" {
		var additional bool
		if raw, ok := node["additionalProperties"]; !ok {
			return errors.New("object schema must set additionalProperties:false")
		} else if err := json.Unmarshal(raw, &additional); err != nil || additional {
			return errors.New("object schema must set additionalProperties:false")
		}
		if raw, ok := node["properties"]; ok {
			var props map[string]map[string]json.RawMessage
			if err := json.Unmarshal(raw, &props); err != nil {
				return errors.New("properties must be an object")
			}
			for name, child := range props {
				if err := validateSchemaNode(child, false, depth+1); err != nil {
					return fmt.Errorf("property %q: %w", name, err)
				}
			}
		}
	}
	if kind == "array" {
		raw, ok := node["items"]
		if !ok {
			return errors.New("array schema needs items")
		}
		var child map[string]json.RawMessage
		if err := json.Unmarshal(raw, &child); err != nil {
			return errors.New("items must be an object")
		}
		if err := validateSchemaNode(child, false, depth+1); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	return nil
}

// hashableRequest is the canonical projection that identifies one request.
// It deliberately excludes epoch, transient URLs and tracing.
type hashableRequest struct {
	Contract    string            `json:"contract_version"`
	ModelKey    string            `json:"model_key"`
	Catalog     string            `json:"catalog_version"`
	Deployment  string            `json:"deployment_key"`
	PriceVer    string            `json:"price_version"`
	OutputLimit int               `json:"output_limit"`
	ToolChoice  string            `json:"tool_choice"`
	Format      string            `json:"response_format"`
	Temperature *float64          `json:"temperature,omitempty"`
	Tools       []hashableTool    `json:"tools"`
	Messages    []hashableMessage `json:"messages"`
}

type hashableTool struct {
	Name    string          `json:"name"`
	Desc    string          `json:"description"`
	Version int             `json:"schema_version"`
	Schema  json.RawMessage `json:"input_schema"`
}

type hashableMessage struct {
	Role      string          `json:"role"`
	CallID    string          `json:"tool_call_id,omitempty"`
	Blocks    []hashableBlock `json:"blocks"`
	ToolCalls []hashableCall  `json:"tool_calls,omitempty"`
}

type hashableBlock struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Digest string `json:"media_digest,omitempty"`
	MIME   string `json:"media_mime,omitempty"`
}

type hashableCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

// RequestHash covers the exact instruction material, the fixed model and price
// snapshot, the output limit and media content digests. A price change can
// never rewrite the hash of an existing request.
func RequestHash(r ChatRequest, m ModelSnapshot) (string, error) {
	h := hashableRequest{
		Contract:    r.ContractVersion,
		ModelKey:    r.ModelKey,
		Catalog:     m.CatalogVersion,
		Deployment:  m.DeploymentKey,
		PriceVer:    m.PriceVersion,
		OutputLimit: r.OutputLimit,
		ToolChoice:  r.ToolChoice,
		Format:      r.ResponseFormat,
		Temperature: r.Temperature,
		Tools:       make([]hashableTool, 0, len(r.Tools)),
		Messages:    make([]hashableMessage, 0, len(r.Messages)),
	}
	tools := append([]ToolDefinition(nil), r.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	for _, t := range tools {
		canonical, err := canonicalJSON(t.InputSchema)
		if err != nil {
			return "", err
		}
		h.Tools = append(h.Tools, hashableTool{Name: t.Name, Desc: t.Description, Version: t.SchemaVersion, Schema: canonical})
	}
	for _, msg := range r.Messages {
		entry := hashableMessage{Role: string(msg.Role), CallID: msg.ToolCallID, Blocks: make([]hashableBlock, 0, len(msg.Blocks))}
		for _, b := range msg.Blocks {
			block := hashableBlock{Kind: string(b.Kind), Text: b.Text}
			if b.Image != nil {
				block.Digest, block.MIME = b.Image.Digest, b.Image.MIME
			}
			entry.Blocks = append(entry.Blocks, block)
		}
		for _, c := range msg.ToolCalls {
			canonical, err := canonicalJSON(c.Arguments)
			if err != nil {
				return "", err
			}
			entry.ToolCalls = append(entry.ToolCalls, hashableCall{ID: c.ID, Name: c.Name, Args: canonical})
		}
		h.Messages = append(h.Messages, entry)
	}
	encoded, err := json.Marshal(h)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

// ResultHash identifies one complete persisted result.
func ResultHash(r Result) (string, error) {
	calls := make([]hashableCall, 0, len(r.ToolCalls))
	for _, c := range r.ToolCalls {
		canonical, err := canonicalJSON(c.Arguments)
		if err != nil {
			return "", err
		}
		calls = append(calls, hashableCall{ID: c.ID, Name: c.Name, Args: canonical})
	}
	encoded, err := json.Marshal(struct {
		Text   string         `json:"text"`
		Calls  []hashableCall `json:"tool_calls"`
		Finish string         `json:"finish_reason"`
		Model  string         `json:"model"`
	}{r.Text, calls, string(r.FinishReason), r.Model})
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256-" + hex.EncodeToString(sum[:])
}

// canonicalJSON re-encodes a document so that key order never changes a hash.
func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%w: json payload", ErrValidation)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
