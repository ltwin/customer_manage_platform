package store_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestTelegramDigestMigrationUpDownAndConstraints(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	for _, accountID := range []string{"acc_tg_a", "acc_tg_b"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ($1, 'hash')`, accountID); err != nil {
			t.Fatalf("insert account %s: %v", accountID, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings (account_id, telegram_chat_id) VALUES ('acc_tg_a', 'chat-1')`); err != nil {
		t.Fatalf("bind first chat: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings (account_id, telegram_chat_id) VALUES ('acc_tg_b', 'chat-1')`); err == nil {
		t.Fatal("one Telegram chat must not bind to two accounts")
	}

	hash := make([]byte, 32)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_bind_tokens (account_id, token_hash, expires_at)
		VALUES ('acc_tg_a', $1, now() + interval '10 minutes')`, hash); err != nil {
		t.Fatalf("insert bind token: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_bind_tokens (account_id, token_hash, expires_at)
		VALUES ('acc_tg_a', $1, now() + interval '10 minutes')`, hash); err == nil {
		t.Fatal("bind token state must be account-unique")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_bind_tokens (account_id, token_hash, expires_at)
		VALUES ('acc_tg_b', decode('00', 'hex'), now() + interval '10 minutes')`); err == nil {
		t.Fatal("bind token hash must be exactly 32 bytes")
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_deliveries (
			id, account_id, source, source_key, message_kind, status,
			target_local_date, timezone_at_enqueue, next_attempt_at
		) VALUES (
			'del_tg_1', 'acc_tg_a', 'daily', '2026-07-15', 'digest', 'pending',
			DATE '2026-07-15', 'Asia/Shanghai', now()
		)`); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_deliveries (
			id, account_id, source, source_key, message_kind, status, next_attempt_at
		) VALUES ('del_tg_2', 'acc_tg_a', 'daily', '2026-07-15', 'digest', 'pending', now())`); err == nil {
		t.Fatal("delivery source key must be unique within an account")
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO telegram_deliveries (
			id, account_id, source, source_key, message_kind, status, attempts, next_attempt_at
		) VALUES ('del_tg_bad', 'acc_tg_b', 'daily', 'bad', 'digest', 'pending', 5, now())`); err == nil {
		t.Fatal("delivery attempts must remain within the four-attempt failure budget")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close before down migration: %v", err)
	}
	for index, label := range []string{
		"agent-conversations", "llm-gateway",
		"creative-canvas-events",
		"creative-media",
		"creative-canvas-commands",
		"creative-library-organization",
		"creative-text-canvas",
		"creative-foundation",
		"creative-workspace",
		"settings-health-tiers",
		"orders-attribution-snapshot",
		"orders-payment-facts",
		"orders-delivery-due",
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share-assignments", "share-feedbacks", "share-anonymous", "share-generations",
		"security-attempt-budget", "plan-crm", "plan-ingestion", "planning-media", "shoot-planning",
		"account-profiles", "settings-availability", "auth-attempt-limiter", "account-auth", "Telegram digest",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("migrate down %s (step %d): %v", label, index+1, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after down: %v", err)
	}
	defer func() { _ = db.Close() }()
	var relations int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('telegram_bind_tokens', 'telegram_deliveries')`).Scan(&relations); err != nil {
		t.Fatalf("inspect down migration: %v", err)
	}
	if relations != 0 {
		t.Fatalf("down migration left %d Telegram digest tables", relations)
	}
}
