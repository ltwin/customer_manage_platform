package digest

import (
	"context"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	claimLease       = 30 * time.Second
	attemptTimeout   = 20 * time.Second
	telegramTimeout  = 10 * time.Second
	resultMargin     = 2 * time.Second
	cleanupTimeout   = 2 * time.Second
	recipientBackoff = 5 * time.Minute
)

var errCallBoundary = errors.New("delivery call boundary closed")

type CallAuthorizer interface {
	BeginCurrentCall(ctx context.Context, scope store.AccountScope, claim AttemptClaim) (CallStartPermit, error)
}

type DeliverySender struct {
	repository  DeliveryRepository
	gate        RecipientGate
	recipients  RecipientResolver
	messages    MessageBuilder
	telegram    TelegramSender
	calls       CallAuthorizer
	integration *IntegrationState
	now         func() time.Time
	monoNow     func() time.Time
}

func NewDeliverySender(
	repository DeliveryRepository,
	gate RecipientGate,
	recipients RecipientResolver,
	messages MessageBuilder,
	telegram TelegramSender,
) *DeliverySender {
	return &DeliverySender{
		repository: repository,
		gate:       gate,
		recipients: recipients,
		messages:   messages,
		telegram:   telegram,
		now:        time.Now,
		monoNow:    time.Now,
	}
}

func (s *DeliverySender) WithClock(now func() time.Time) *DeliverySender {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *DeliverySender) WithMonoClock(now func() time.Time) *DeliverySender {
	if now != nil {
		s.monoNow = now
	}
	return s
}

func (s *DeliverySender) WithCallAuthorizer(calls CallAuthorizer) *DeliverySender {
	s.calls = calls
	return s
}

// WithIntentService wires the production DigestIntentService as the sole call authorizer.
func (s *DeliverySender) WithIntentService(intent *DigestIntentService) *DeliverySender {
	s.calls = intent
	return s
}

func (s *DeliverySender) WithIntegrationState(state *IntegrationState) *DeliverySender {
	s.integration = state
	return s
}

func (s *DeliverySender) SendNext(ctx context.Context, account store.ScopedAccount) (bool, error) {
	start := s.now().UTC()
	claim, outcome, err := s.repository.ClaimDueAttempt(ctx, account.Scope, start, start.Add(claimLease))
	if err != nil || outcome == ClaimDueNone {
		return false, err
	}
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	attemptDeadline := start.Add(attemptTimeout)
	sendStarted := false
	claimReleased := false

	err = s.gate.WithSend(attemptCtx, account.AccountID, func(gateCtx context.Context) error {
		current, err := s.repository.ReloadClaim(gateCtx, account.Scope, claim.Delivery.ID, claim.ClaimID)
		if err != nil {
			return err
		}
		if current.Status != DeliveryStatusPending || current.ClaimID != claim.ClaimID || current.LeaseUntil == nil {
			return nil
		}
		claim.Delivery = current
		claim.LeaseUntil = *current.LeaseUntil

		if s.calls == nil {
			return errors.New("digest call authorizer required for physical send")
		}

		permit, err := s.calls.BeginCurrentCall(gateCtx, account.Scope, claim)
		if err != nil {
			if errors.Is(err, errRecipientMissingIntent) {
				if err := s.release(account.Scope, claim, ClaimRelease{
					ErrorCode: "recipient_missing", NextAttemptAt: s.now().UTC().Add(recipientBackoff),
				}); err != nil {
					return err
				}
				claimReleased = true
				return nil
			}
			if errors.Is(err, errCallBudgetExhausted) || errors.Is(err, store.ErrDigestCallNotAuthorized) ||
				errors.Is(err, errIntentGuardFailed) {
				return errCallBoundary
			}
			return err
		}

		// Final local monotonic check before the physical Telegram call.
		if permit.MonotonicStartBudget <= 0 {
			return errCallBoundary
		}
		now := s.now().UTC()
		callDeadline := minTime(attemptDeadline, claim.LeaseUntil).Add(-(telegramTimeout + resultMargin))
		if gateCtx.Err() != nil || now.After(callDeadline) {
			return errCallBoundary
		}
		callable, err := s.repository.AssertCallableClaim(
			gateCtx,
			account.Scope,
			claim,
			now,
			now.Add(telegramTimeout+resultMargin),
		)
		if err != nil {
			return err
		}
		now = s.now().UTC()
		if !callable || gateCtx.Err() != nil || now.After(callDeadline) {
			return errCallBoundary
		}

		sendCtx, sendCancel := context.WithTimeout(gateCtx, minDuration(telegramTimeout, permit.MonotonicStartBudget))
		sendStarted = true
		_, sendErr := s.telegram.SendMessage(sendCtx, permit.RecipientChatID, permit.PayloadText)
		sendCancel()
		if sendErr == nil {
			_ = s.finalizePermit(account.Scope, claim, permit, "finalized")
			return s.finalize(account.Scope, claim, AttemptResult{Sent: true})
		}
		var telegramErr *TelegramError
		if errors.As(sendErr, &telegramErr) {
			if telegramErr.Kind == TelegramErrorInvalidAuth {
				if s.integration != nil {
					s.integration.Suspend(telegramErr.Kind, recipientBackoff)
				}
				_ = s.finalizePermit(account.Scope, claim, permit, "definite_failure")
				if err := s.release(account.Scope, claim, ClaimRelease{
					ErrorCode: string(telegramErr.Kind), NextAttemptAt: s.now().UTC().Add(recipientBackoff),
				}); err != nil {
					return err
				}
				claimReleased = true
				return nil
			}
			_ = s.finalizePermit(account.Scope, claim, permit, "definite_failure")
			return s.finalize(account.Scope, claim, AttemptResult{ErrorKind: telegramErr.Kind, RetryAfter: telegramErr.RetryAfter})
		}
		if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
			_ = s.finalizePermit(account.Scope, claim, permit, "response_unknown")
			return sendErr
		}
		_ = s.finalizePermit(account.Scope, claim, permit, "definite_failure")
		return s.finalize(account.Scope, claim, AttemptResult{ErrorKind: TelegramErrorNetwork})
	})
	if err == nil {
		return true, nil
	}
	if !sendStarted && !claimReleased {
		if cleanupErr := s.release(account.Scope, claim, ClaimRelease{NextAttemptAt: s.now().UTC()}); cleanupErr != nil {
			return true, cleanupErr
		}
		if errors.Is(err, errCallBoundary) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true, nil
		}
	}
	if sendStarted && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return true, nil
	}
	return true, err
}

func (s *DeliverySender) finalizePermit(scope store.AccountScope, claim AttemptClaim, permit CallStartPermit, outcome string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_, err := scope.Update(ctx, "delivery_send_attempt_permits",
		"outcome = $2, finalized_at = clock_timestamp()",
		"delivery_id = $3 AND attempt_id = $4 AND outcome = $5",
		outcome, claim.Delivery.ID, permit.AttemptID, "calling")
	return err
}

func (s *DeliverySender) finalize(scope store.AccountScope, claim AttemptClaim, result AttemptResult) error {
	resultCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	now := s.now().UTC()
	outcome, err := s.repository.FinalizeAttempt(resultCtx, scope, claim, result, now)
	if err == nil && outcome == FinalizeApplied {
		return nil
	}
	current, reloadErr := s.repository.ReloadClaim(resultCtx, scope, claim.Delivery.ID, claim.ClaimID)
	if reloadErr != nil {
		return errors.Join(err, reloadErr)
	}
	if current.Status != DeliveryStatusPending || current.ClaimID != claim.ClaimID {
		return nil
	}
	if current.LeaseUntil == nil || !current.LeaseUntil.After(now) {
		return err
	}
	retryOutcome, retryErr := s.repository.FinalizeAttempt(resultCtx, scope, claim, result, now)
	if retryErr == nil && retryOutcome != FinalizeApplied {
		return nil
	}
	return retryErr
}

func (s *DeliverySender) release(scope store.AccountScope, claim AttemptClaim, release ClaimRelease) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_, err := s.repository.ReleaseClaim(cleanupCtx, scope, claim, release, s.now().UTC())
	return err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
