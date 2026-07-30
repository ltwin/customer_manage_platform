package digest

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

type ReminderScanService interface {
	NeedsScan(context.Context, store.AccountScope, time.Time) (bool, error)
	ScanAndCheckpoint(context.Context, store.AccountScope, time.Time) (reminderdomain.ScanResult, error)
}

type ReminderScanEnsurer struct{ service ReminderScanService }

func NewReminderScanEnsurer(service ReminderScanService) ReminderScanEnsurer {
	return ReminderScanEnsurer{service: service}
}

func (e ReminderScanEnsurer) EnsureScan(
	ctx context.Context,
	account store.ScopedAccount,
	target LocalTarget,
) error {
	needed, err := e.service.NeedsScan(ctx, account.Scope, target.LocalDate)
	if err != nil || !needed {
		return err
	}
	_, err = e.service.ScanAndCheckpoint(ctx, account.Scope, target.LocalDate)
	return err
}

type SettingsRecipientResolver struct{ settings SettingsReader }

func NewSettingsRecipientResolver(settings SettingsReader) SettingsRecipientResolver {
	return SettingsRecipientResolver{settings: settings}
}

func (r SettingsRecipientResolver) ResolveCurrent(
	ctx context.Context,
	account store.ScopedAccount,
) (RecipientOutcome, error) {
	view, err := r.settings.Get(ctx, account.Scope)
	if err != nil {
		return RecipientOutcome{}, err
	}
	if view.TelegramChatID == nil || *view.TelegramChatID == "" {
		return RecipientOutcome{Kind: RecipientMissing}, nil
	}
	return RecipientOutcome{Kind: RecipientCurrent, ChatID: *view.TelegramChatID}, nil
}

var (
	_ ScanEnsurer       = ReminderScanEnsurer{}
	_ RecipientResolver = SettingsRecipientResolver{}
	_ SettingsReader    = (*settings.Service)(nil)
)
