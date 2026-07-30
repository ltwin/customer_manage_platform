package digest

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type StartHandler interface {
	HandleStart(context.Context, Update) (BindOutcome, error)
}

type ScanEnsurer interface {
	EnsureScan(context.Context, store.ScopedAccount, LocalTarget) error
}

type TargetProvider interface {
	TargetFor(context.Context, store.ScopedAccount, time.Time) (LocalTarget, error)
}

type UpdateHandler struct {
	start    StartHandler
	chats    *ChatAccountResolver
	scan     ScanEnsurer
	targets  TargetProvider
	bindings BindingRepository
	now      func() time.Time
}

func NewUpdateHandler(
	start StartHandler,
	chats *ChatAccountResolver,
	scan ScanEnsurer,
	targets TargetProvider,
	bindings BindingRepository,
) *UpdateHandler {
	return &UpdateHandler{
		start: start, chats: chats, scan: scan, targets: targets, bindings: bindings, now: time.Now,
	}
}

func (h *UpdateHandler) WithClock(now func() time.Time) *UpdateHandler {
	if now != nil {
		h.now = now
	}
	return h
}

func (h *UpdateHandler) HandleUpdate(ctx context.Context, update Update) (bool, error) {
	// Telegram clients may append the bot mention to commands issued in shared
	// contexts (e.g. "/start@bot <token>"); normalize before routing so those
	// forms are not silently misrouted and left unbindable (REV-005).
	command, _ := parseCommand(update.Text)
	switch command {
	case "/start":
		_, err := h.start.HandleStart(ctx, update)
		return err == nil, err
	case "/today":
	default:
		return true, nil
	}
	if update.ChatType != "private" {
		return true, nil
	}
	resolution, err := h.chats.Resolve(ctx, update.ChatID)
	if err != nil {
		return false, err
	}
	if resolution.Kind != ScopeMatched {
		return true, nil
	}
	target, err := h.targets.TargetFor(ctx, resolution.Account, h.now().UTC())
	if err != nil {
		return false, err
	}
	messageKind := MessageKindDigest
	if err := h.scan.EnsureScan(ctx, resolution.Account, target); err != nil {
		messageKind = MessageKindTemporaryUnavailable
	}
	outcome, err := h.bindings.ClaimCommandIfCurrentChat(
		ctx,
		resolution.Account.Scope,
		update.ChatID,
		update.ID,
		target,
		messageKind,
	)
	if err != nil {
		return false, err
	}
	return outcome == ClaimCreated || outcome == ClaimExisting || outcome == ClaimUnauthorized, nil
}

var _ UpdateProcessor = (*UpdateHandler)(nil)
