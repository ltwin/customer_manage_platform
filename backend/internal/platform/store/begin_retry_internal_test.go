package store

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func refusingPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), "postgres://probe@127.0.0.1:1/probe?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// An unreachable database is waited out, and the caller's own deadline or
// cancellation is what the error then reports.
func TestBeginWaitsOutUnreachableDatabaseUntilCallerGivesUp(t *testing.T) {
	pool := refusingPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 900*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := beginTx(ctx, pool, pgx.TxOptions{})
	if waited := time.Since(started); waited < 700*time.Millisecond {
		t.Fatalf("gave up after %v without waiting out the outage", waited)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline lost: %v", err)
	}

	ctx, cancel = context.WithCancel(t.Context())
	time.AfterFunc(300*time.Millisecond, cancel)
	if _, err = beginTx(ctx, pool, pgx.TxOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation lost: %v", err)
	}
}

// The budget covers connecting as well as waiting, and running out of it is an
// outage, not a deadline the caller set.
func TestBeginBudgetCoversSlowConnects(t *testing.T) {
	budget, backoff := beginBudget, beginBackoff
	beginBudget, beginBackoff = time.Second, []time.Duration{50 * time.Millisecond}
	t.Cleanup(func() { beginBudget, beginBackoff = budget, backoff })

	config, err := pgxpool.ParseConfig("postgres://probe@127.0.0.1:1/probe?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	unreachable := errors.New("network is unreachable")
	config.ConnConfig.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case <-time.After(300 * time.Millisecond):
			return nil, unreachable
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	started := time.Now()
	_, err = beginTx(t.Context(), pool, pgx.TxOptions{})
	if waited := time.Since(started); waited > beginBudget+200*time.Millisecond {
		t.Fatalf("slow connects stretched the wait to %v", waited)
	}
	if !errors.Is(err, unreachable) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the outage, got %v", err)
	}
}

// A refusal the server gives, or a pool that is closed, will not pass by
// waiting and is answered at once.
func TestBeginDoesNotRetryPermanentRefusals(t *testing.T) {
	missing, err := pgxpool.New(t.Context(), storetest.URLFor("no_such_database_probe"))
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	started := time.Now()
	_, err = beginTx(t.Context(), missing, pgx.TxOptions{})
	var refused *pgconn.PgError
	if !errors.As(err, &refused) || refused.Code != "3D000" {
		t.Fatalf("want 3D000, got %v", err)
	}
	if waited := time.Since(started); waited > 200*time.Millisecond {
		t.Fatalf("retried a missing database for %v", waited)
	}

	closed := refusingPool(t)
	closed.Close()
	started = time.Now()
	if _, err = beginTx(t.Context(), closed, pgx.TxOptions{}); err == nil {
		t.Fatal("begin on a closed pool succeeded")
	}
	if waited := time.Since(started); waited > 200*time.Millisecond {
		t.Fatalf("retried a closed pool for %v", waited)
	}
}
