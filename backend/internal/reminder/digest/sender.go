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

type DeliverySender struct {
	repository  DeliveryRepository
	gate        RecipientGate
	recipients  RecipientResolver
	messages    MessageBuilder
	telegram    TelegramSender
	integration *IntegrationState
	now         func() time.Time
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
	}
}

func (s *DeliverySender) WithClock(now func() time.Time) *DeliverySender {
	if now != nil {
		s.now = now
	}
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

	// Build the message text before entering the recipient gate. It is
	// deterministic from the delivery's enqueue-frozen fields (message kind,
	// target local date, timezone) and independent of the current recipient,
	// so keeping it out of the critical section holds the gate order to
	// design D7 (reload → recipient → call-boundary → ≤10s send → short CAS)
	// and stops rebind latency from being bound to read-model performance
	// (REV-004).
	text, err := s.messages.Build(attemptCtx, account.Scope, claim.Delivery)
	if err != nil {
		if cleanupErr := s.release(account.Scope, claim, ClaimRelease{NextAttemptAt: s.now().UTC()}); cleanupErr != nil {
			return true, cleanupErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true, nil
		}
		return true, err
	}

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

		recipient, err := s.recipients.ResolveCurrent(gateCtx, account)
		if err != nil {
			return err
		}
		switch recipient.Kind {
		case RecipientMissing, RecipientIntegrity:
			code := "recipient_missing"
			if recipient.Kind == RecipientIntegrity {
				code = "recipient_integrity"
			}
			if err := s.release(account.Scope, claim, ClaimRelease{ErrorCode: code, NextAttemptAt: s.now().UTC().Add(recipientBackoff)}); err != nil {
				return err
			}
			claimReleased = true
			return nil
		case RecipientCurrent:
		default:
			return errors.New("unknown recipient outcome")
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

		sendCtx, sendCancel := context.WithTimeout(gateCtx, telegramTimeout)
		sendStarted = true
		_, sendErr := s.telegram.SendMessage(sendCtx, recipient.ChatID, text)
		sendCancel()
		if sendErr == nil {
			return s.finalize(account.Scope, claim, AttemptResult{Sent: true})
		}
		var telegramErr *TelegramError
		if errors.As(sendErr, &telegramErr) {
			if telegramErr.Kind == TelegramErrorInvalidAuth {
				if s.integration != nil {
					s.integration.Suspend(telegramErr.Kind, recipientBackoff)
				}
				if err := s.release(account.Scope, claim, ClaimRelease{
					ErrorCode: string(telegramErr.Kind), NextAttemptAt: s.now().UTC().Add(recipientBackoff),
				}); err != nil {
					return err
				}
				claimReleased = true
				return nil
			}
			return s.finalize(account.Scope, claim, AttemptResult{ErrorKind: telegramErr.Kind, RetryAfter: telegramErr.RetryAfter})
		}
		if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
			return sendErr
		}
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

func (s *DeliverySender) finalize(scope store.AccountScope, claim AttemptClaim, result AttemptResult) error {
	resultCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	now := s.now().UTC()
	outcome, err := s.repository.FinalizeAttempt(resultCtx, scope, claim, result, now)
	if err == nil && outcome == FinalizeApplied {
		return nil
	}

	// Either the write failed (commit outcome unknown) or the CAS matched no
	// row (FinalizeStale). A stale outcome must not be mistaken for success:
	// reload by claim identity and, only while our claim is still the current
	// pending owner with a live lease, retry the same idempotent result CAS
	// without resending Telegram (REV-006).
	current, reloadErr := s.repository.ReloadClaim(resultCtx, scope, claim.Delivery.ID, claim.ClaimID)
	if reloadErr != nil {
		return errors.Join(err, reloadErr)
	}
	if current.Status != DeliveryStatusPending || current.ClaimID != claim.ClaimID {
		// A terminal state or another claimant already owns the result; this
		// attempt is consumed and must not be resent.
		return nil
	}
	if current.LeaseUntil == nil || !current.LeaseUntil.After(now) {
		// Our claim lapsed; a reclaimant retries. Do not rewrite an expired claim.
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
