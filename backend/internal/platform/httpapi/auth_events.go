package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

func (h *handlers) logAuthSessionEvent(ctx context.Context, event string, session auth.Session, err error) {
	if h.logger == nil {
		return
	}
	record := authevent.Event{Name: authevent.Name(event), Result: authevent.ResultSuccess}
	if err != nil {
		record.Result = authevent.ResultFailure
		record.FailureClass = authFailureClass(err)
	} else {
		record.AccountRef = redactedAuthRef("account", session.AccountID)
		record.SessionRef = redactedAuthRef("session", session.RefreshSessionID)
	}
	h.logger.LogAttrs(ctx, slog.LevelInfo, "auth event", record.Attrs()...)
}

func (h *handlers) logRefreshReuseEvent(ctx context.Context, err error) {
	if h.logger == nil || !errors.Is(err, auth.ErrRefreshReuse) {
		return
	}
	record := authevent.Event{
		Name: authevent.RefreshReuse, Result: authevent.ResultFailure, FailureClass: "reuse",
	}
	h.logger.LogAttrs(ctx, slog.LevelWarn, "auth event", record.Attrs()...)
}

func (h *handlers) logRateLimitedEvent(ctx context.Context, err error) {
	if h.logger == nil {
		return
	}
	action, sourceDigest, ok := auth.AuthRateLimitDetails(err)
	if !ok {
		return
	}
	record := authevent.Event{
		Name: authevent.RateLimited, Result: authevent.ResultFailure,
		FailureClass: string(auth.AuthErrorRateLimited), Action: string(action), SourceDigest: sourceDigest,
	}
	h.logger.LogAttrs(ctx, slog.LevelWarn, "auth event", record.Attrs()...)
}

func (h *handlers) logPasswordChangedEvent(ctx context.Context, action auth.AuthAction, accountID string) {
	if h.logger == nil {
		return
	}
	record := authevent.Event{
		Name: authevent.PasswordChanged, Result: authevent.ResultSuccess, Action: string(action),
	}
	if accountID != "" {
		record.AccountRef = redactedAuthRef("account", accountID)
	}
	h.logger.LogAttrs(ctx, slog.LevelInfo, "auth event", record.Attrs()...)
}

func authFailureClass(err error) string {
	switch {
	case auth.IsAuthError(err, auth.AuthErrorValidation):
		return string(auth.AuthErrorValidation)
	case auth.IsAuthError(err, auth.AuthErrorUnauthorized):
		return string(auth.AuthErrorUnauthorized)
	case auth.IsAuthError(err, auth.AuthErrorEmailVerificationRequired):
		return string(auth.AuthErrorEmailVerificationRequired)
	case auth.IsAuthError(err, auth.AuthErrorInvalidOrExpiredToken):
		return string(auth.AuthErrorInvalidOrExpiredToken)
	case auth.IsAuthError(err, auth.AuthErrorRateLimited):
		return string(auth.AuthErrorRateLimited)
	default:
		return string(auth.AuthErrorInternal)
	}
}

func redactedAuthRef(kind, raw string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + raw))
	return kind + ":" + hex.EncodeToString(digest[:6])
}
