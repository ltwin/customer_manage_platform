package digest

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type captureSnapshotRepository struct {
	window Window
}

func (r *captureSnapshotRepository) Load(_ context.Context, _ store.AccountScope, window Window, _ int) (DigestSnapshot, error) {
	r.window = window
	return DigestSnapshot{LocalDate: window.LocalDate, Timezone: window.Timezone}, nil
}

func TestDigestMessageBuilderUsesDeliveryFrozenTargetAcrossMidnightAndTimezoneChange(t *testing.T) {
	repo := &captureSnapshotRepository{}
	builder := NewDigestMessageBuilder(repo, NewRenderer())
	localDate := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	text, err := builder.Build(context.Background(), store.AccountScope{}, Delivery{
		MessageKind:       MessageKindDigest,
		TargetLocalDate:   &localDate,
		TimezoneAtEnqueue: "Asia/Shanghai",
		NextAttemptAt:     time.Date(2026, 7, 16, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if repo.window.LocalDate.Format("2006-01-02") != "2026-07-15" || repo.window.Timezone != "Asia/Shanghai" {
		t.Fatalf("retry target drifted to send-time state: %+v", repo.window)
	}
	if text[:len("7月15日经营摘要")] != "7月15日经营摘要" {
		t.Fatalf("renderer title drifted: %s", text)
	}
}

func TestDigestMessageBuilderUsesFixedSafeTextForAckAndUnavailable(t *testing.T) {
	builder := NewDigestMessageBuilder(&captureSnapshotRepository{}, NewRenderer())
	ack, err := builder.Build(context.Background(), store.AccountScope{}, Delivery{MessageKind: MessageKindBindingAck})
	if err != nil || ack != "Telegram 绑定成功。你将从这里收到每日经营摘要。" {
		t.Fatalf("binding ack: text=%q err=%v", ack, err)
	}
	unavailable, err := builder.Build(context.Background(), store.AccountScope{}, Delivery{MessageKind: MessageKindTemporaryUnavailable})
	if err != nil || unavailable != "摘要暂时不可用，请稍后重试。" {
		t.Fatalf("temporary unavailable: text=%q err=%v", unavailable, err)
	}
}
