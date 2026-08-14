package reminder_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func TestS4MarkDoneLegacyZeroDrift(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s4_status")
	ctx := context.Background()
	svc := reminder.NewService(reminder.NewPostgresRepository(), nil, nil)
	created, err := svc.CreateCustom(ctx, scope, reminder.CreateCustomInput{
		DueDate: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		Content: "legacy custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.MarkDone(ctx, scope, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != reminder.StatusDone || updated.Type != reminder.TypeCustom {
		t.Fatalf("unexpected: %+v", updated)
	}
}

func TestS4BindingRevisionMaterialOnly(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s4_bind")
	ctx := context.Background()
	now := time.Now().UTC()
	repo := digest.NewPostgresBindingRepository()

	bind := func(chat string, updateID int64, tokenByte byte) {
		t.Helper()
		hash := make([]byte, 32)
		for i := range hash {
			hash[i] = tokenByte
		}
		if err := repo.IssueBindToken(ctx, scope, hash, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		outcome, err := repo.ConsumeAndBind(ctx, scope, hash, chat, updateID, now)
		if err != nil || outcome != digest.BindApplied {
			t.Fatalf("bind chat=%s: outcome=%v err=%v", chat, outcome, err)
		}
	}

	bind("1001", 1, 1)
	settingsSvc := settings.NewService(settings.NewPostgresRepository())
	view, err := settingsSvc.Get(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if view.TelegramBindingRevision != 1 {
		t.Fatalf("first bind revision=%d want 1", view.TelegramBindingRevision)
	}

	bind("1001", 2, 2)
	view, err = settingsSvc.Get(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if view.TelegramBindingRevision != 1 {
		t.Fatalf("same chat revision=%d want 1", view.TelegramBindingRevision)
	}

	bind("2002", 3, 3)
	view, err = settingsSvc.Get(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if view.TelegramBindingRevision != 2 {
		t.Fatalf("material rebind revision=%d want 2", view.TelegramBindingRevision)
	}
	if view.TelegramChatID == nil || *view.TelegramChatID != "2002" {
		t.Fatalf("chat=%v", view.TelegramChatID)
	}
}

func TestS4CallingCASDualSenderOneWinner(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s4_cas")
	ctx := context.Background()
	now := time.Now().UTC()
	seedBoundSettings(t, scope, "chat-winner", 1)
	insertPendingDigestDelivery(t, scope, "del_cas", now)

	intent := digest.NewDigestIntentService(digest.NewDigestMessageBuilder(
		digest.NewPostgresSnapshotRepository(), digest.NewRenderer()), nil)
	claim := claimDelivery(t, scope, now)

	var (
		mu      sync.Mutex
		winners int
		fails   int
	)
	run := func() {
		_, err := intent.BeginCurrentCall(ctx, scope, claim)
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			winners++
		} else {
			fails++
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run() }()
	go func() { defer wg.Done(); run() }()
	wg.Wait()
	if winners != 1 || fails != 1 {
		t.Fatalf("winners=%d fails=%d want 1/1", winners, fails)
	}
}

func TestS4MonotonicBudgetNearBoundary(t *testing.T) {
	// S6 strengthens this path through BeginCurrentCall + clock_timestamp CAS
	// in TestS6MonotonicBudgetThroughBeginCurrentCall. Keep a pure-math sanity check.
	cases := []struct {
		name     string
		pause    time.Duration
		wantMax  time.Duration
		wantZero bool
	}{
		{name: "80ms_from_100ms", pause: 80 * time.Millisecond, wantMax: 20 * time.Millisecond},
		{name: "120ms_from_100ms", pause: 120 * time.Millisecond, wantZero: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const businessRemainingAtAuth = 100 * time.Millisecond
			remaining := businessRemainingAtAuth - tc.pause
			if remaining < 0 {
				remaining = 0
			}
			if tc.wantZero {
				if remaining != 0 {
					t.Fatalf("remaining=%v want 0", remaining)
				}
				return
			}
			if remaining > tc.wantMax {
				t.Fatalf("remaining=%v want <= %v", remaining, tc.wantMax)
			}
		})
	}
}

func TestS4CreateReminderCustomOnly(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s4_custom")
	svc := reminder.NewService(reminder.NewPostgresRepository(), nil, nil)
	created, err := svc.CreateCustom(context.Background(), scope, reminder.CreateCustomInput{
		DueDate: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC),
		Content: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Type != reminder.TypeCustom {
		t.Fatalf("type=%s", created.Type)
	}
}

func TestS4H4NoCustomerDeliverySchema(t *testing.T) {
	_, url := startAssignmentReminderPostgresURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`
SELECT count(*) FROM information_schema.tables
WHERE table_schema='public' AND (
  table_name ILIKE '%customer%telegram%'
  OR table_name ILIKE '%customer%sms%'
  OR table_name ILIKE '%opt_in%'
  OR table_name ILIKE '%unsubscribe%'
)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unexpected customer delivery tables: %d", n)
	}
}

func TestS4IntentSameFingerprintReuse(t *testing.T) {
	s, _ := startAssignmentReminderPostgresURL(t)
	scope := activateAssignmentTestAccount(t, s, "acc_s4_intent")
	ctx := context.Background()
	now := time.Now().UTC()
	seedBoundSettings(t, scope, "chat-intent", 1)
	insertPendingDigestDelivery(t, scope, "del_intent", now)
	intentSvc := digest.NewDigestIntentService(digest.NewDigestMessageBuilder(
		digest.NewPostgresSnapshotRepository(), digest.NewRenderer()), nil)
	claim := claimDelivery(t, scope, now)

	var first, second digest.ImmutableDigestIntent
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		first, err = intentSvc.PrepareCurrentIntentInScope(ctx, tx, locked, claim)
		return err
	})
	if err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		second, err = intentSvc.PrepareCurrentIntentInScope(ctx, tx, locked, claim)
		return err
	})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if first.IntentRevision != second.IntentRevision {
		t.Fatalf("same fingerprint should reuse revision: %d vs %d", first.IntentRevision, second.IntentRevision)
	}
}

func seedBoundSettings(t *testing.T, scope store.AccountScope, chatID string, rev int64) {
	t.Helper()
	ctx := context.Background()
	svc := settings.NewService(settings.NewPostgresRepository())
	if _, err := svc.Patch(ctx, scope, settings.PatchInput{}); err != nil {
		t.Fatalf("seed default settings: %v", err)
	}
	if err := scope.Upsert(ctx, "settings",
		[]string{"telegram_chat_id", "telegram_binding_revision", "updated_at"},
		[]string{"account_id"},
		[]string{"telegram_chat_id", "telegram_binding_revision", "updated_at"},
		chatID, rev, time.Now().UTC(),
	); err != nil {
		t.Fatalf("seed telegram binding: %v", err)
	}
}

func insertPendingDigestDelivery(t *testing.T, scope store.AccountScope, id string, now time.Time) {
	t.Helper()
	local := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	if err := scope.Insert(context.Background(), "telegram_deliveries", []string{
		"id", "source", "source_key", "message_kind", "target_local_date", "timezone_at_enqueue",
		"status", "next_attempt_at", "updated_at",
	}, id, "daily", "daily:"+local.Format("2006-01-02"), "digest", local, "Asia/Shanghai",
		"pending", now, now); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
}

func claimDelivery(t *testing.T, scope store.AccountScope, now time.Time) digest.AttemptClaim {
	t.Helper()
	claim, outcome, err := digest.NewPostgresDeliveryRepository().ClaimDueAttempt(
		context.Background(), scope, now, now.Add(30*time.Second))
	if err != nil || outcome != digest.ClaimDueClaimed {
		t.Fatalf("claim: outcome=%v err=%v", outcome, err)
	}
	return claim
}
