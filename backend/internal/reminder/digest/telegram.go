package digest

import (
	"context"
	"fmt"
	"time"
)

type TelegramErrorKind string

const (
	TelegramErrorNetwork         TelegramErrorKind = "network"
	TelegramErrorTimeout         TelegramErrorKind = "timeout"
	TelegramErrorServer          TelegramErrorKind = "server_error"
	TelegramErrorRateLimited     TelegramErrorKind = "rate_limited"
	TelegramErrorWebhookConflict TelegramErrorKind = "webhook_conflict"
	TelegramErrorInvalidAuth     TelegramErrorKind = "invalid_auth"
	TelegramErrorClient          TelegramErrorKind = "client_error"
	TelegramErrorProtocol        TelegramErrorKind = "protocol_error"
)

func TelegramErrorKinds() []TelegramErrorKind {
	return []TelegramErrorKind{
		TelegramErrorNetwork,
		TelegramErrorTimeout,
		TelegramErrorServer,
		TelegramErrorRateLimited,
		TelegramErrorWebhookConflict,
		TelegramErrorInvalidAuth,
		TelegramErrorClient,
		TelegramErrorProtocol,
	}
}

type TelegramError struct {
	Kind       TelegramErrorKind
	Code       string
	RetryAfter time.Duration
}

func (e *TelegramError) Error() string {
	if e == nil {
		return "telegram error"
	}
	return fmt.Sprintf("telegram %s (%s)", e.Kind, e.Code)
}

type MessageRef struct {
	MessageID int64
}

type TelegramSender interface {
	SendMessage(context.Context, string, string) (MessageRef, error)
}

type Update struct {
	ID       int64
	ChatID   string
	ChatType string
	Text     string
}

type TelegramUpdateSource interface {
	GetUpdates(context.Context, int64, time.Duration) ([]Update, error)
}
