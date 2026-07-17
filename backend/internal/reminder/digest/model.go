// Package digest implements the account-owned Telegram digest application.
package digest

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type DeliverySource string

const (
	DeliverySourceDaily      DeliverySource = "daily"
	DeliverySourceCommand    DeliverySource = "command"
	DeliverySourceBindingAck DeliverySource = "binding_ack"
)

type MessageKind string

const (
	MessageKindDigest               MessageKind = "digest"
	MessageKindTemporaryUnavailable MessageKind = "temporary_unavailable"
	MessageKindBindingAck           MessageKind = "binding_ack"
)

type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "pending"
	DeliveryStatusSent       DeliveryStatus = "sent"
	DeliveryStatusFailed     DeliveryStatus = "failed"
	DeliveryStatusSuperseded DeliveryStatus = "superseded"
)

type Delivery struct {
	ID                string
	Source            DeliverySource
	SourceKey         string
	MessageKind       MessageKind
	TargetLocalDate   *time.Time
	TimezoneAtEnqueue string
	Status            DeliveryStatus
	Attempts          int
	NextAttemptAt     time.Time
	SentAt            *time.Time
	LastErrorCode     string
	ClaimID           string
	LeaseUntil        *time.Time
}

type AttemptClaim struct {
	Delivery   Delivery
	ClaimID    string
	LeaseUntil time.Time
}

type ClaimDueOutcome string

const (
	ClaimDueClaimed ClaimDueOutcome = "claimed"
	ClaimDueNone    ClaimDueOutcome = "none"
)

type FinalizeOutcome string

const (
	FinalizeApplied FinalizeOutcome = "applied"
	FinalizeStale   FinalizeOutcome = "stale"
)

type AttemptResult struct {
	Sent       bool
	ErrorKind  TelegramErrorKind
	RetryAfter time.Duration
}

type ClaimRelease struct {
	ErrorCode     string
	NextAttemptAt time.Time
}

type DeliveryRepository interface {
	ClaimDueAttempt(context.Context, store.AccountScope, time.Time, time.Time) (AttemptClaim, ClaimDueOutcome, error)
	ReloadClaim(context.Context, store.AccountScope, string, string) (Delivery, error)
	AssertCallableClaim(context.Context, store.AccountScope, AttemptClaim, time.Time, time.Time) (bool, error)
	FinalizeAttempt(context.Context, store.AccountScope, AttemptClaim, AttemptResult, time.Time) (FinalizeOutcome, error)
	ReleaseClaim(context.Context, store.AccountScope, AttemptClaim, ClaimRelease, time.Time) (FinalizeOutcome, error)
}

type RecipientKind string

const (
	RecipientCurrent   RecipientKind = "current"
	RecipientMissing   RecipientKind = "binding_missing"
	RecipientIntegrity RecipientKind = "integrity_conflict"
)

type RecipientOutcome struct {
	Kind   RecipientKind
	ChatID string
}

type RecipientResolver interface {
	ResolveCurrent(context.Context, store.ScopedAccount) (RecipientOutcome, error)
}

type MessageBuilder interface {
	Build(context.Context, store.AccountScope, Delivery) (string, error)
}
