package store_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAttemptLimiterMigrationCreatesVersionedBudgetTable(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate limiter schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open limiter schema: %v", err)
	}
	defer func() { _ = db.Close() }()

	var columns int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'auth_attempt_budgets'
		  AND column_name IN ('action', 'dimension', 'digest', 'window_start', 'attempts')`).Scan(&columns); err != nil {
		t.Fatalf("inspect limiter table: %v", err)
	}
	if columns != 5 {
		t.Fatalf("limiter table column count = %d, want 5", columns)
	}
	if _, err := db.Exec(`
		INSERT INTO auth_attempt_budgets (action, dimension, digest, window_start, attempts)
		VALUES ('unknown', 'subject', 'v1:digest', now(), 1)`); err == nil {
		t.Fatal("unknown auth action must violate limiter schema")
	}
	if _, err := db.Exec(`
		INSERT INTO auth_attempt_budgets (action, dimension, digest, window_start, attempts)
		VALUES ('login', 'raw-ip', '192.0.2.1', now(), 1)`); err == nil {
		t.Fatal("raw source dimension must violate limiter schema")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close before limiter down migration: %v", err)
	}
	for index, label := range []string{
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share assignments", "share feedbacks", "share anonymous projection", "share generations",
		"security attempt budget", "plan crm", "plan ingestion", "planning media", "shoot planning",
		"account profiles", "settings availability", "limiter",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("%s down migration (step %d): %v", label, index+1, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("reopen after limiter down migration: %v", err)
	}
	var limiterTable *string
	if err := db.QueryRow(`SELECT to_regclass('public.auth_attempt_budgets')::text`).Scan(&limiterTable); err != nil {
		t.Fatalf("inspect limiter down migration: %v", err)
	}
	if limiterTable != nil {
		t.Fatalf("limiter down migration left table %q", *limiterTable)
	}
}

func TestAuthReadinessInspectsCurrentLimiterSchemaAndLegacyCutover(t *testing.T) {
	url := startPostgres(t)
	database := openMigrated(t, url)
	ctx := context.Background()

	state, err := database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect current auth readiness: %v", err)
	}
	if state.SchemaVersion != 31 || !state.DatabaseReady || !state.LimiterSchemaReady || !state.LegacyCutoverReady {
		t.Fatalf("current readiness = %#v", state)
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open legacy readiness fixture: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `ALTER TABLE auth_attempt_budgets DROP CONSTRAINT auth_attempt_budgets_pkey`); err != nil {
		t.Fatalf("drop limiter primary key: %v", err)
	}
	state, err = database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect limiter without primary key: %v", err)
	}
	if state.LimiterSchemaReady {
		t.Fatalf("limiter without primary key unexpectedly ready: %#v", state)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE auth_attempt_budgets
		ADD CONSTRAINT auth_attempt_budgets_pkey PRIMARY KEY (action, dimension, digest)`); err != nil {
		t.Fatalf("restore limiter primary key: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE auth_attempt_budgets
		DROP CONSTRAINT auth_attempt_budgets_attempts_check`); err != nil {
		t.Fatalf("drop limiter attempts check: %v", err)
	}
	state, err = database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect limiter without attempts check: %v", err)
	}
	if state.LimiterSchemaReady {
		t.Fatalf("limiter without attempts check unexpectedly ready: %#v", state)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE auth_attempt_budgets
		ADD CONSTRAINT auth_attempt_budgets_attempts_check CHECK (attempts > 0)`); err != nil {
		t.Fatalf("restore limiter attempts check: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		DROP INDEX auth_attempt_budgets_window_idx;
		CREATE INDEX auth_attempt_budgets_window_idx ON auth_attempt_budgets (digest)`); err != nil {
		t.Fatalf("replace limiter index with wrong definition: %v", err)
	}
	state, err = database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect limiter with wrong index: %v", err)
	}
	if state.LimiterSchemaReady {
		t.Fatalf("limiter with wrong index unexpectedly ready: %#v", state)
	}
	if _, err := db.ExecContext(ctx, `
		DROP INDEX auth_attempt_budgets_window_idx;
		CREATE INDEX auth_attempt_budgets_window_idx ON auth_attempt_budgets (window_start)`); err != nil {
		t.Fatalf("restore limiter window index: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO accounts (id, password_hash, status)
		VALUES ('readiness-legacy', 'deprecated-hash', 'legacy_unclaimed')`); err != nil {
		t.Fatalf("insert legacy readiness fixture: %v", err)
	}
	state, err = database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect incomplete legacy cutover: %v", err)
	}
	if state.LegacyCutoverReady {
		t.Fatalf("legacy cutover unexpectedly ready: %#v", state)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM accounts WHERE id = 'readiness-legacy'`); err != nil {
		t.Fatalf("delete legacy readiness fixture: %v", err)
	}
	for index, label := range []string{
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share assignments", "share feedbacks", "share anonymous projection", "share generations",
		"security attempt budget", "plan crm", "plan ingestion", "planning media", "shoot planning",
		"account profiles", "settings availability", "limiter",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("rollback %s schema (step %d): %v", label, index+1, err)
		}
	}
	state, err = database.InspectAuthReadiness(ctx)
	if err != nil {
		t.Fatalf("inspect rolled back limiter schema: %v", err)
	}
	if state.SchemaVersion != 12 || state.LimiterSchemaReady {
		t.Fatalf("rolled back readiness = %#v", state)
	}
}

func TestAttemptLimiterBudgetMatrixAndConcurrentConsume(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate limiter schema: %v", err)
	}
	first, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open first limiter instance: %v", err)
	}
	defer first.Close()
	second, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open second limiter instance: %v", err)
	}
	defer second.Close()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	digest := func(char byte) string { return "v1:" + strings.Repeat(string(char), 43) }

	t.Run("source budget spans distinct subjects", func(t *testing.T) {
		for attempt := 1; attempt <= 21; attempt++ {
			subject := digest(byte('A' + attempt%20))
			retryAfter, allowed, err := first.Consume(
				context.Background(), auth.AuthActionRegister, subject, digest('z'), now,
			)
			if err != nil {
				t.Fatalf("source attempt %d: %v", attempt, err)
			}
			if attempt <= 20 && (!allowed || retryAfter != 0) {
				t.Fatalf("source attempt %d unexpectedly limited: allowed=%v retry=%s", attempt, allowed, retryAfter)
			}
			if attempt == 21 && (allowed || retryAfter != time.Hour) {
				t.Fatalf("source attempt 21 = allowed:%v retry:%s", allowed, retryAfter)
			}
		}
	})

	if err := first.ResetSubject(context.Background(), auth.AuthActionRegister, digest('A')); err != nil {
		t.Fatalf("reset subject: %v", err)
	}
	if err := first.ResetSubject(context.Background(), auth.AuthAction("unknown"), digest('A')); err == nil {
		t.Fatal("unknown action reset must fail closed")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open limiter matrix reset: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), `TRUNCATE auth_attempt_budgets`); err != nil {
		t.Fatalf("reset limiter matrix: %v", err)
	}
	var allowedCount atomic.Int64
	var failures atomic.Int64
	var group sync.WaitGroup
	for attempt := 0; attempt < 20; attempt++ {
		group.Add(1)
		go func(attempt int) {
			defer group.Done()
			limiter := first
			if attempt%2 == 1 {
				limiter = second
			}
			_, allowed, err := limiter.Consume(
				context.Background(), auth.AuthActionLogin, digest('q'), digest('r'), now,
			)
			if err != nil {
				failures.Add(1)
				return
			}
			if allowed {
				allowedCount.Add(1)
			}
		}(attempt)
	}
	group.Wait()
	if failures.Load() != 0 || allowedCount.Load() != 5 {
		t.Fatalf("concurrent budget = allowed:%d failures:%d, want 5/0", allowedCount.Load(), failures.Load())
	}
}

func TestAttemptLimiterPersistsBudgetAcrossInstancesAndHalfOpenWindow(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate limiter schema: %v", err)
	}
	first, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open first limiter instance: %v", err)
	}
	defer first.Close()
	second, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open second limiter instance: %v", err)
	}
	defer second.Close()

	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	subject := "v1:" + strings.Repeat("A", 43)
	source := "v1:" + strings.Repeat("B", 43)
	for attempt := 1; attempt <= 5; attempt++ {
		limiter := first
		if attempt%2 == 0 {
			limiter = second
		}
		retryAfter, allowed, err := limiter.Consume(context.Background(), auth.AuthActionLogin, subject, source, now)
		if err != nil || !allowed || retryAfter != 0 {
			t.Fatalf("attempt %d = allowed:%v retry:%s err:%v", attempt, allowed, retryAfter, err)
		}
	}
	retryAfter, allowed, err := second.Consume(context.Background(), auth.AuthActionLogin, subject, source, now)
	if err != nil || allowed || retryAfter != 15*time.Minute {
		t.Fatalf("limited attempt = allowed:%v retry:%s err:%v", allowed, retryAfter, err)
	}
	retryAfter, allowed, err = first.Consume(context.Background(), auth.AuthActionLogin, subject, source, now.Add(15*time.Minute))
	if err != nil || !allowed || retryAfter != 0 {
		t.Fatalf("half-open window attempt = allowed:%v retry:%s err:%v", allowed, retryAfter, err)
	}
}
