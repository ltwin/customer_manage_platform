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

	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestSecurityAttemptBudgetMigrationRejectsSensitiveColumns(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate security budget schema: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open security budget schema: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, table := range []string{
		"security_attempt_budgets_v1",
		"security_attempt_budget_guards_v1",
	} {
		rows, err := db.Query(`
			SELECT column_name
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1
			ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatalf("list %s columns: %v", table, err)
		}
		var columns []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan %s column: %v", table, err)
			}
			columns = append(columns, name)
		}
		_ = rows.Close()
		joined := strings.Join(columns, ",")
		for _, forbidden := range []string{
			"account_id", "raw_ip", "ip", "token", "receipt", "body", "idempotency",
		} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("%s columns %v unexpectedly contain %q", table, columns, forbidden)
			}
		}
	}

	digest := bytesRepeat(0x11, 32)
	if _, err := db.Exec(`
		INSERT INTO security_attempt_budgets_v1 (
			policy_version, action, dimension, digest_version, hmac_digest, window_start, attempts
		) VALUES ('v1', 'login', 'ip', 'd1', $1, now(), 1)`, digest); err == nil {
		t.Fatal("auth login action must not enter security attempt budgets")
	}
	if _, err := db.Exec(`
		INSERT INTO security_attempt_budgets_v1 (
			policy_version, action, dimension, digest_version, hmac_digest, window_start, attempts
		) VALUES ('v1', 'planshare_anonymous_mutation_outer_v1', 'raw-ip', 'd1', $1, now(), 1)`, digest); err == nil {
		t.Fatal("raw-ip dimension must violate security budget schema")
	}
}

func TestSecurityAttemptBudgetCrossInstancePersistAndFailClosed(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate security budget: %v", err)
	}
	first, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	defer first.Close()
	second, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer second.Close()

	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	candidates := securitybudget.DigestCandidates{
		Current: securitybudget.DigestIdentity{
			Version: securitybudget.DigestVersionV1,
			HMAC:    bytesRepeat(0x42, 32),
		},
	}
	window := 10 * time.Minute
	limit := 3

	for attempt := 1; attempt <= 3; attempt++ {
		retry, allowed, err := first.ConsumeSecurityAttemptBudget(
			context.Background(),
			securitybudget.PolicyV1,
			securitybudget.ActionAnonymousMutationOuter,
			securitybudget.DimensionIP,
			candidates,
			window,
			limit,
			now,
		)
		if err != nil || !allowed || retry != 0 {
			t.Fatalf("attempt %d: allowed=%v retry=%s err=%v", attempt, allowed, retry, err)
		}
	}
	retry, allowed, err := second.ConsumeSecurityAttemptBudget(
		context.Background(),
		securitybudget.PolicyV1,
		securitybudget.ActionAnonymousMutationOuter,
		securitybudget.DimensionIP,
		candidates,
		window,
		limit,
		now,
	)
	if err != nil || allowed || retry != window {
		t.Fatalf("cross-instance ceiling: allowed=%v retry=%s err=%v", allowed, retry, err)
	}

	var failures atomic.Int32
	var allowedCount atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, consumeErr := first.ConsumeSecurityAttemptBudget(
				context.Background(),
				securitybudget.PolicyV1,
				securitybudget.ActionAnonymousReadOuter,
				securitybudget.DimensionTokenGeneration,
				securitybudget.DigestCandidates{
					Current: securitybudget.DigestIdentity{
						Version: securitybudget.DigestVersionV1,
						HMAC:    bytesRepeat(0x77, 32),
					},
				},
				time.Hour,
				5,
				now,
			)
			if consumeErr != nil {
				failures.Add(1)
				return
			}
			if ok {
				allowedCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 || allowedCount.Load() != 5 {
		t.Fatalf("concurrent budget allowed=%d failures=%d", allowedCount.Load(), failures.Load())
	}

	closed := &store.Store{}
	if _, _, err := closed.ConsumeSecurityAttemptBudget(
		context.Background(),
		securitybudget.PolicyV1,
		securitybudget.ActionAnonymousMutationBusiness,
		securitybudget.DimensionTokenGenerationIP,
		candidates,
		window,
		limit,
		now,
	); err == nil {
		t.Fatal("nil store must fail closed")
	}
}

func TestSecurityAttemptBudgetPreviousDigestGraceContinuesWindow(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate security budget: %v", err)
	}
	db, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	windowStart := time.Date(2026, 8, 14, 23, 55, 0, 0, time.UTC)
	afterMidnight := time.Date(2026, 8, 15, 0, 2, 0, 0, time.UTC)
	previous := securitybudget.DigestIdentity{
		Version: securitybudget.DigestVersionV1,
		HMAC:    bytesRepeat(0x31, 32),
	}
	current := securitybudget.DigestIdentity{
		Version: securitybudget.DigestVersionV1,
		HMAC:    bytesRepeat(0x32, 32),
	}
	window := 10 * time.Minute
	limit := 2

	for attempt := 1; attempt <= 2; attempt++ {
		retry, allowed, err := db.ConsumeSecurityAttemptBudget(
			context.Background(),
			securitybudget.PolicyV1,
			securitybudget.ActionAnonymousMutationOuter,
			securitybudget.DimensionIP,
			securitybudget.DigestCandidates{Current: previous},
			window,
			limit,
			windowStart,
		)
		if err != nil || !allowed || retry != 0 {
			t.Fatalf("pre-midnight attempt %d: allowed=%v retry=%s err=%v", attempt, allowed, retry, err)
		}
	}

	grace := securitybudget.DigestCandidates{
		Current:            current,
		Previous:           &previous,
		RolloverGraceUntil: afterMidnight.Add(30 * time.Minute),
	}
	retry, allowed, err := db.ConsumeSecurityAttemptBudget(
		context.Background(),
		securitybudget.PolicyV1,
		securitybudget.ActionAnonymousMutationOuter,
		securitybudget.DimensionIP,
		grace,
		window,
		limit,
		afterMidnight,
	)
	if err != nil || allowed {
		t.Fatalf("post-midnight must continue previous active window: allowed=%v retry=%s err=%v", allowed, retry, err)
	}
	wantRetry := windowStart.Add(window).Sub(afterMidnight)
	if retry != wantRetry {
		t.Fatalf("Retry-After want %s got %s", wantRetry, retry)
	}

	// After previous window ends, current digest may open a fresh budget.
	afterWindow := windowStart.Add(window).Add(time.Second)
	retry, allowed, err = db.ConsumeSecurityAttemptBudget(
		context.Background(),
		securitybudget.PolicyV1,
		securitybudget.ActionAnonymousMutationOuter,
		securitybudget.DimensionIP,
		grace,
		window,
		limit,
		afterWindow,
	)
	if err != nil || !allowed || retry != 0 {
		t.Fatalf("after previous window: allowed=%v retry=%s err=%v", allowed, retry, err)
	}
}

func bytesRepeat(value byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
