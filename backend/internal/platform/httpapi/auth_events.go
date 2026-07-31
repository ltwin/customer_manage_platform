package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func (h *handlers) logAuthSessionEvent(ctx context.Context, event string, session auth.Session, err error) {
	if h.logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("event", event),
		slog.String("result", "success"),
	}
	if err != nil {
		attrs[1] = slog.String("result", "failure")
		attrs = append(attrs, slog.String("failure_class", authFailureClass(err)))
	} else {
		attrs = append(attrs,
			slog.String("account_ref", redactedAuthRef("account", session.AccountID)),
			slog.String("session_ref", redactedAuthRef("session", session.RefreshSessionID)),
		)
	}
	h.logger.LogAttrs(ctx, slog.LevelInfo, "auth event", attrs...)
}

func (h *handlers) logRefreshReuseEvent(ctx context.Context, err error) {
	if h.logger == nil || !errors.Is(err, auth.ErrRefreshReuse) {
		return
	}
	h.logger.LogAttrs(ctx, slog.LevelWarn, "auth event",
		slog.String("event", "auth.refresh_reuse"),
		slog.String("result", "failure"),
		slog.String("failure_class", "reuse"),
	)
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
	default:
		return string(auth.AuthErrorInternal)
	}
}

func redactedAuthRef(kind, raw string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + raw))
	return kind + ":" + hex.EncodeToString(digest[:6])
}
