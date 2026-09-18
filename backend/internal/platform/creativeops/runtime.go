package creativeops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrToolUnavailable = errors.New("creative tool unavailable")
	ErrToolVersion     = errors.New("creative tool version mismatch")
	ErrToolResult      = errors.New("creative tool returned an invalid result")
)

type Entrypoint string

const (
	HTTP  Entrypoint = "http"
	Agent Entrypoint = "agent"
)

type ToolResult struct {
	Data    json.RawMessage
	Receipt *Receipt
}

func (r ToolResult) Response() json.RawMessage {
	if r.Receipt != nil {
		return r.Receipt.Outcome.Response
	}
	return r.Data
}

// Handler owns domain transactions and resource-level checks. The runtime's
// discovery and coarse capability check never replace that last authority check.
type ToolHandler func(context.Context, store.AccountScope, Command) (ToolResult, error)
type Binding struct {
	Definition  Definition
	Entrypoints []Entrypoint
	Handle      ToolHandler
}

// Policy can only narrow a compiled binding. It cannot replace the handler,
// schema, required capability, source exposure, or effect classification.
type ToolPolicy struct {
	DisabledReason string
	MaxInputBytes  int
}
type ToolRef struct {
	Key     string
	Version int
}
type ToolView struct {
	Definition Definition
	Available  bool
	Reason     string
}
type AgentAuthority func(context.Context, string, json.RawMessage) error

type Runtime struct {
	catalog  *Catalog
	handlers map[string]ToolHandler
	exposure map[string][]Entrypoint
	policies map[string]ToolPolicy
	observer func(context.Context, ToolObservation)
}

// ToolObservation has bounded labels and no arguments, credentials or output.
// Observers must be nonblocking; failures must not alter committed effects.
type ToolObservation struct {
	Key        string
	Version    int
	Entrypoint Entrypoint
	Duration   time.Duration
	Outcome    string
}
type RuntimeOption func(*Runtime)

func WithObserver(observer func(context.Context, ToolObservation)) RuntimeOption {
	return func(r *Runtime) { r.observer = observer }
}
func (r *Runtime) observe(ctx context.Context, event ToolObservation) {
	if r.observer == nil {
		return
	}
	// A telemetry plugin cannot turn a committed command into a retryable error.
	defer func() { _ = recover() }()
	r.observer(ctx, event)
}

func NewRuntime(bindings []Binding, policies map[string]ToolPolicy, options ...RuntimeOption) (*Runtime, error) {
	defs := make([]Definition, 0, len(bindings))
	r := &Runtime{handlers: map[string]ToolHandler{}, exposure: map[string][]Entrypoint{}, policies: map[string]ToolPolicy{}}
	for _, b := range bindings {
		if b.Handle == nil || b.Definition.Category == "" || b.Definition.Kind == "query" && (b.Definition.MayCharge || b.Definition.Undoable) || len(b.Entrypoints) == 0 {
			return nil, ErrValidation
		}
		for i, entry := range b.Entrypoints {
			if entry != HTTP && entry != Agent || slices.Contains(b.Entrypoints[:i], entry) {
				return nil, ErrValidation
			}
			// Writes need the Harness transaction-bound authority protocol (FND-08).
			// This slice exposes only read tools to Agent, even if a caller misregisters.
			if entry == Agent && b.Definition.Kind != "query" {
				return nil, ErrValidation
			}
		}
		defs = append(defs, b.Definition)
		r.handlers[b.Definition.Key] = b.Handle
		r.exposure[b.Definition.Key] = append([]Entrypoint(nil), b.Entrypoints...)
	}
	var err error
	r.catalog, err = NewCatalog(defs)
	if err != nil {
		return nil, err
	}
	for key, p := range policies {
		if r.handlers[key] == nil || p.MaxInputBytes < 0 || p.MaxInputBytes > 1<<20 {
			return nil, ErrValidation
		}
		r.policies[key] = p
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r, nil
}

type ToolSession struct {
	runtime   *Runtime
	scope     store.AccountScope
	entry     Entrypoint
	allowed   map[string]int
	authority AgentAuthority
}

func (r *Runtime) ForHTTP(scope store.AccountScope) *ToolSession {
	return &ToolSession{runtime: r, scope: scope, entry: HTTP}
}
func (r *Runtime) ForAgent(scope store.AccountScope, allowed []ToolRef, authority AgentAuthority) (*ToolSession, error) {
	if authority == nil || scope.AccountID() == "" {
		return nil, ErrValidation
	}
	session := &ToolSession{runtime: r, scope: scope, entry: Agent, authority: authority, allowed: map[string]int{}}
	for _, ref := range allowed {
		d, ok := r.catalog.definitions[ref.Key]
		if !ok || d.SchemaVersion != ref.Version || !slices.Contains(r.exposure[ref.Key], Agent) || session.allowed[ref.Key] != 0 {
			return nil, ErrValidation
		}
		session.allowed[ref.Key] = ref.Version
	}
	return session, nil
}
func (s *ToolSession) Entrypoint() Entrypoint { return s.entry }

func (s *ToolSession) visible(d Definition) bool {
	return slices.Contains(s.runtime.exposure[d.Key], s.entry) && (s.entry != Agent || s.allowed[d.Key] == d.SchemaVersion)
}
func (s *ToolSession) require(ctx context.Context, capability string) error {
	if s.scope.AccountID() == "" {
		return store.ErrCreativeAccessDenied
	}
	return s.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error { return tx.RequireCreativeCapability(ctx, capability) })
}
func (s *ToolSession) Describe(ctx context.Context, key string, version int) (Definition, error) {
	if err := s.require(ctx, "creative_read"); err != nil {
		return Definition{}, err
	}
	d, ok := s.runtime.catalog.definitions[key]
	if !ok || !s.visible(d) {
		return Definition{}, ErrToolUnavailable
	}
	if d.SchemaVersion != version {
		return Definition{}, ErrToolVersion
	}
	return copyDefinition(d), nil
}
func (s *ToolSession) List(ctx context.Context) ([]ToolView, error) {
	if err := s.require(ctx, "creative_read"); err != nil {
		return nil, err
	}
	views := []ToolView{}
	for _, d := range s.runtime.catalog.List() {
		if !s.visible(d) {
			continue
		}
		reason := s.runtime.policies[d.Key].DisabledReason
		if err := s.require(ctx, d.RequiredCapability); err != nil {
			if !errors.Is(err, store.ErrCreativeAccessDenied) {
				return nil, err
			}
			reason = "账号能力未开放"
		}
		views = append(views, ToolView{Definition: d, Available: reason == "", Reason: reason})
	}
	return views, nil
}
func (s *ToolSession) Invoke(ctx context.Context, key string, version int, command Command) (result ToolResult, err error) {
	started := time.Now()
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		if errors.Is(err, ErrValidation) || errors.Is(err, ErrToolUnavailable) || errors.Is(err, ErrToolVersion) || errors.Is(err, store.ErrCreativeAccessDenied) {
			outcome = "refused"
		}
		s.runtime.observe(ctx, ToolObservation{Key: key, Version: version, Entrypoint: s.entry, Duration: time.Since(started), Outcome: outcome})
	}()
	d, err := s.Describe(ctx, key, version)
	if err != nil {
		return ToolResult{}, err
	}
	p := s.runtime.policies[key]
	if p.DisabledReason != "" {
		return ToolResult{}, ErrToolUnavailable
	}
	if d.RequiredCapability != "creative_read" {
		if err = s.require(ctx, d.RequiredCapability); err != nil {
			return ToolResult{}, err
		}
	}
	if d.Kind != "query" && (!ValidOperationID(command.OperationID) || command.CreatedAt.IsZero()) {
		return ToolResult{}, ErrValidation
	}
	limit := p.MaxInputBytes
	if limit == 0 {
		limit = 1 << 20
	}
	if len(command.Payload) > limit {
		return ToolResult{}, ErrValidation
	}
	var input map[string]any
	if err = Decode(command.Payload, &input); err != nil {
		return ToolResult{}, err
	}
	if err = s.runtime.catalog.inputs[key].VisitJSON(input); err != nil {
		return ToolResult{}, ErrValidation
	}
	// Give the authority and handler their own bytes: neither may mutate the
	// command checked for the other, or the retry identity retained by the caller.
	if s.authority != nil {
		if err = s.authority(ctx, key, append(json.RawMessage(nil), command.Payload...)); err != nil {
			return ToolResult{}, err
		}
	}
	command.Payload = append(json.RawMessage(nil), command.Payload...)
	result, err = s.runtime.handlers[key](ctx, s.scope, command)
	if err != nil {
		return result, err
	}
	if d.Kind == "query" {
		if result.Receipt != nil {
			return result, ErrToolResult
		}
	} else {
		if result.Receipt == nil || len(result.Data) > 0 || result.Receipt.OperationID != command.OperationID {
			return result, ErrToolResult
		}
		outcome := result.Receipt.Outcome
		if err = validateOutcome(outcome); err != nil {
			return result, ErrToolResult
		}
		if d.Kind == "execution" && (outcome.HTTPStatus != 202 || outcome.ResultID == nil || *outcome.ResultID == "" || outcome.ResultKind == "") || d.Kind == "command" && outcome.HTTPStatus == 202 {
			return result, ErrToolResult
		}
	}
	// Domain responses add wrappers around accepted input. Check complete JSON
	// here without reapplying the command input depth limit to stored data.
	if !json.Valid(result.Response()) {
		return result, ErrToolResult
	}
	var output any
	// Responses may exceed the command input bound; domain pagination owns their
	// size. UseNumber preserves exact integer bytes and existing response semantics.
	decoder := json.NewDecoder(bytes.NewReader(result.Response()))
	decoder.UseNumber()
	if err = decoder.Decode(&output); err != nil {
		return result, ErrToolResult
	}
	if err = s.runtime.catalog.outputs[key].VisitJSON(output); err != nil {
		return result, fmt.Errorf("%w: %s", ErrToolResult, key)
	}
	return result, nil
}
func copyDefinition(d Definition) Definition {
	d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
	d.OutputSchema = append(json.RawMessage(nil), d.OutputSchema...)
	return d
}
