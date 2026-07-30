package digest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var ErrTelegramIntegrationSuspended = errors.New("telegram integration suspended")

// IntegrationState 在 poll、binding 与 sender 间共享 invalid_auth 暂停窗口。
type IntegrationState struct {
	mu             sync.Mutex
	suspendedUntil time.Time
	now            func() time.Time
	logger         *slog.Logger
}

func NewIntegrationState(logger *slog.Logger) *IntegrationState {
	if logger == nil {
		logger = slog.Default()
	}
	return &IntegrationState{now: time.Now, logger: logger}
}

func (s *IntegrationState) WithClock(now func() time.Time) *IntegrationState {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *IntegrationState) Suspend(reason TelegramErrorKind, delay time.Duration) {
	if delay <= 0 {
		return
	}
	now := s.now().UTC()
	until := now.Add(delay)
	s.mu.Lock()
	if !until.After(s.suspendedUntil) {
		s.mu.Unlock()
		return
	}
	s.suspendedUntil = until
	s.mu.Unlock()
	s.logger.Warn("telegram integration suspended",
		slog.String("status", "suspended"),
		slog.String("reason", string(reason)),
		slog.Time("retry_at", until),
	)
}

func (s *IntegrationState) RetryAfter() time.Duration {
	s.mu.Lock()
	until := s.suspendedUntil
	s.mu.Unlock()
	delay := until.Sub(s.now().UTC())
	if delay <= 0 {
		return 0
	}
	return delay
}

func (s *IntegrationState) Suspended() bool { return s.RetryAfter() > 0 }

type BindTokenIssuer interface {
	IssueBindToken(context.Context, store.AccountScope) (BindLink, error)
}

type GuardedBindingIssuer struct {
	issuer BindTokenIssuer
	state  *IntegrationState
}

func NewGuardedBindingIssuer(issuer BindTokenIssuer, state *IntegrationState) *GuardedBindingIssuer {
	return &GuardedBindingIssuer{issuer: issuer, state: state}
}

func (i *GuardedBindingIssuer) IssueBindToken(
	ctx context.Context,
	scope store.AccountScope,
) (BindLink, error) {
	if i.state != nil && i.state.Suspended() {
		return BindLink{}, ErrTelegramIntegrationSuspended
	}
	return i.issuer.IssueBindToken(ctx, scope)
}
