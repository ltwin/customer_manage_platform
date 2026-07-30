package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fixedTargetProvider struct{ target LocalTarget }

func (p fixedTargetProvider) TargetFor(context.Context, store.ScopedAccount, time.Time) (LocalTarget, error) {
	return p.target, nil
}

type toggleScanEnsurer struct{ err error }

func (e *toggleScanEnsurer) EnsureScan(context.Context, store.ScopedAccount, LocalTarget) error {
	return e.err
}

type noStartHandler struct{}

func (noStartHandler) HandleStart(context.Context, Update) (BindOutcome, error) {
	return BindInvalid, nil
}

func TestUpdateHandlerTodayFreezesFirstMessageKindAcrossReplay(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	if err := account.Scope.Upsert(context.Background(), "settings",
		[]string{"telegram_chat_id", "updated_at"}, []string{"account_id"},
		[]string{"telegram_chat_id", "updated_at"}, "chat-today", now); err != nil {
		t.Fatalf("bind chat: %v", err)
	}
	repo := NewPostgresBindingRepository()
	scan := &toggleScanEnsurer{err: errors.New("scan unavailable")}
	handler := NewUpdateHandler(
		noStartHandler{},
		NewChatAccountResolver(accountEnumerator{db: db}, repo),
		scan,
		fixedTargetProvider{target: LocalTarget{LocalDate: now, Timezone: "Asia/Shanghai"}},
		repo,
	).WithClock(func() time.Time { return now })

	terminal, err := handler.HandleUpdate(context.Background(), Update{
		ID: 501, ChatID: "chat-today", ChatType: "private", Text: "/today",
	})
	if err != nil || !terminal {
		t.Fatalf("scan failure command must still become durable: terminal=%v err=%v", terminal, err)
	}
	delivery := loadCommandDelivery(t, account.Scope, "501")
	if delivery.MessageKind != MessageKindTemporaryUnavailable {
		t.Fatalf("scan failure kind=%s", delivery.MessageKind)
	}

	scan.err = nil
	terminal, err = handler.HandleUpdate(context.Background(), Update{
		ID: 501, ChatID: "chat-today", ChatType: "private", Text: "/today",
	})
	if err != nil || !terminal {
		t.Fatalf("replay existing command: terminal=%v err=%v", terminal, err)
	}
	delivery = loadCommandDelivery(t, account.Scope, "501")
	if delivery.MessageKind != MessageKindTemporaryUnavailable {
		t.Fatalf("replay stole first message kind: %s", delivery.MessageKind)
	}

	terminal, err = handler.HandleUpdate(context.Background(), Update{
		ID: 502, ChatID: "chat-today", ChatType: "private", Text: "/today",
	})
	if err != nil || !terminal {
		t.Fatalf("new command: terminal=%v err=%v", terminal, err)
	}
	if got := loadCommandDelivery(t, account.Scope, "502").MessageKind; got != MessageKindDigest {
		t.Fatalf("new command after scan recovery kind=%s", got)
	}
}

func loadCommandDelivery(t *testing.T, scope store.AccountScope, sourceKey string) Delivery {
	t.Helper()
	return mustScanDelivery(t, scope.QueryRow(context.Background(), "telegram_deliveries", deliveryColumns,
		"source = $2 AND source_key = $3", string(DeliverySourceCommand), sourceKey))
}

func mustScanDelivery(t *testing.T, row rowScanner) Delivery {
	t.Helper()
	delivery, err := scanDelivery(row)
	if err != nil {
		t.Fatalf("scan delivery: %v", err)
	}
	return delivery
}
