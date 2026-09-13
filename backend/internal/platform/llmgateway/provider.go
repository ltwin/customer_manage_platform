package llmgateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

// Outcome classifies what a real attempt proved about provider acceptance.
// It is the only input allowed to decide whether a retry may ever happen.
type Outcome string

const (
	// OutcomeUnaccepted means the provider demonstrably did not accept the
	// request and cannot have charged for it.
	OutcomeUnaccepted Outcome = "unaccepted"
	// OutcomeRejected means a definitive, non-retryable refusal.
	OutcomeRejected Outcome = "rejected"
	// OutcomeUnknown means acceptance cannot be proven either way.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeProtocol means the provider answered outside the fixed contract.
	OutcomeProtocol Outcome = "protocol"
)

// ProviderError carries the adapter's evidence. A bare 429 or 5xx is never
// promoted to "unaccepted" without the adapter proving it.
type ProviderError struct {
	Outcome    Outcome
	Class      string
	StatusCode int
	Detail     string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %s (%s, status %d): %s", e.Outcome, e.Class, e.StatusCode, e.Detail)
}

// Is lets callers match the gateway's coarse sentinels.
func (e *ProviderError) Is(target error) bool {
	switch target {
	case ErrUnknown:
		return e.Outcome == OutcomeUnknown
	case ErrProtocol:
		return e.Outcome == OutcomeProtocol
	}
	return false
}

// Retryable reports whether this evidence may re-admit the same request.
func (e *ProviderError) Retryable() bool { return e.Outcome == OutcomeUnaccepted }

// ProviderRequest is one dispatch. The credential is resolved at send time and
// never persisted, logged or hashed.
type ProviderRequest struct {
	Model      ModelConfig
	Snapshot   ModelSnapshot
	Chat       ChatRequest
	Credential string
	Stream     bool
}

// Provider adapts one wire protocol. It performs exactly one attempt and must
// not retry, fail over or switch models internally. It must honor the supplied
// context deadline for all local I/O; recovery depends on this hard transport
// bound before reclaiming an orphaned slot.
type Provider interface {
	Key() ProviderKey
	Invoke(context.Context, ProviderRequest) (Result, error)
}

// NewHTTPClient builds a client with every implicit retry disabled, so each
// real attempt is created and counted by the gateway alone.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Go retries idempotent requests on a reused, half-broken connection.
	// A model call may already have been billed, so that is forbidden here.
	transport.DisableKeepAlives = true
	return &http.Client{Timeout: timeout, Transport: transport}
}

// classifyStatus maps a complete non-2xx response, received before any content
// was streamed, onto acceptance evidence. Only statuses the deployment was
// verified to answer before processing may be treated as unaccepted; every
// other status stays unknown rather than licensing a possibly duplicate charge.
func classifyStatus(model ModelConfig, status int, detail string) *ProviderError {
	if slices.Contains(model.UnacceptedStatuses, status) {
		return &ProviderError{Outcome: OutcomeUnaccepted, Class: "admission_refused", StatusCode: status, Detail: detail}
	}
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusUnprocessableEntity:
		return &ProviderError{Outcome: OutcomeRejected, Class: "invalid_request", StatusCode: status, Detail: detail}
	case http.StatusPaymentRequired:
		return &ProviderError{Outcome: OutcomeRejected, Class: "insufficient_balance", StatusCode: status, Detail: detail}
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// The request may already have been processed and billed.
		return &ProviderError{Outcome: OutcomeUnknown, Class: "server_error", StatusCode: status, Detail: detail}
	default:
		return &ProviderError{Outcome: OutcomeUnknown, Class: "unexpected_status", StatusCode: status, Detail: detail}
	}
}

// classifyTransport maps a transport failure. A Go client error cannot prove
// that no byte reached the provider — the request may have been accepted and
// billed while the answer was lost — so every transport failure stays unknown
// and keeps its hold. Proven non-delivery only comes from evidence the provider
// itself gives, through a declared pre-processing status or a verified
// rejection.
func classifyTransport(err error) *ProviderError {
	if errors.Is(err, context.Canceled) {
		return &ProviderError{Outcome: OutcomeUnknown, Class: "cancelled_in_flight", Detail: err.Error()}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ProviderError{Outcome: OutcomeUnknown, Class: "timeout", Detail: err.Error()}
	}
	return &ProviderError{Outcome: OutcomeUnknown, Class: "transport", Detail: err.Error()}
}

// readImage loads a pinned media handle at send time. The gateway never
// dereferences an asset id or an arbitrary URL.
func readImage(ctx context.Context, in *ImageInput) (string, error) {
	reader, err := in.Open(ctx)
	if err != nil {
		return "", fmt.Errorf("open image input %s: %w", in.RevisionID, err)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(io.LimitReader(reader, in.ByteSize))
	if err != nil {
		return "", fmt.Errorf("read image input %s: %w", in.RevisionID, err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// validateResult applies the contract rules every adapter must satisfy.
func validateResult(r Result, snapshot ModelSnapshot) error {
	if !snapshot.AcceptsModelID(r.Model) {
		return &ProviderError{
			Outcome: OutcomeProtocol,
			Class:   "model_routing_changed",
			Detail:  fmt.Sprintf("provider answered with model %q", r.Model),
		}
	}
	switch r.FinishReason {
	case FinishStop, FinishToolCalls, FinishLength, FinishRefused, FinishError:
	default:
		return &ProviderError{Outcome: OutcomeProtocol, Class: "finish_reason", Detail: string(r.FinishReason)}
	}
	seen := make(map[string]struct{}, len(r.ToolCalls))
	for _, c := range r.ToolCalls {
		if c.ID == "" || !toolNamePattern(c.Name) {
			return &ProviderError{Outcome: OutcomeProtocol, Class: "tool_call_identity", Detail: c.ID}
		}
		if _, dup := seen[c.ID]; dup {
			return &ProviderError{Outcome: OutcomeProtocol, Class: "duplicate_tool_call", Detail: c.ID}
		}
		seen[c.ID] = struct{}{}
		if len(c.Arguments) > maxToolArgsBytes {
			return &ProviderError{Outcome: OutcomeProtocol, Class: "tool_arguments_too_large", Detail: c.ID}
		}
		// Truncated or failed answers can still carry locally parseable JSON;
		// those arguments must never become an executable tool step.
		if r.Consumable() && !json.Valid(c.Arguments) {
			return &ProviderError{Outcome: OutcomeProtocol, Class: "tool_arguments_not_json", Detail: c.ID}
		}
	}
	if r.FinishReason == FinishToolCalls && len(r.ToolCalls) == 0 {
		return &ProviderError{Outcome: OutcomeProtocol, Class: "missing_tool_calls", Detail: "finish_reason=tool_calls"}
	}
	return nil
}
