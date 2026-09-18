package store

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestCreativeQueueReadinessRequiresAllMigrations(t *testing.T) {
	s, err := Open(t.Context(), storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runtime, err := s.NewJobRuntime(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err == nil {
		t.Fatal("unmigrated queue reported ready")
	}
	if _, err := s.pool.Exec(t.Context(), "CREATE SCHEMA creative_jobs"); err != nil {
		t.Fatal(err)
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: creativeQueueSchema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(t.Context(), rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{TargetVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err == nil {
		t.Error("partial migration reported ready")
	}
	if err := s.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err != nil {
		t.Fatalf("complete migration not ready: %v", err)
	}
}

func TestCreativeQueueDeferralDoesNotExhaustFailureAttempts(t *testing.T) {
	s, err := Open(t.Context(), storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.pool.Exec(t.Context(), `INSERT INTO accounts(id,password_hash,status) VALUES('defer-account','test','active')`); err != nil {
		t.Fatal(err)
	}
	if err = s.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	runtime, err := s.NewJobRuntime([]JobHandler{{Kind: "test.defer", Work: func(context.Context, AccountScope, jobs.Request) error {
		if calls.Add(1) <= 6 {
			return jobs.Defer(10 * time.Millisecond)
		}
		return nil
	}}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := runtime.(*creativeJobRuntime).client.Subscribe(river.EventKindJobCompleted)
	defer unsubscribe()
	scope := s.ScopeFor(auth.AccountContext{AccountID: "defer-account"})
	var id int64
	if err = scope.WithTxScope(t.Context(), func(tx TxAccountScope) error {
		var err error
		id, err = jobs.EnqueueInTx(t.Context(), tx.Jobs(runtime), jobs.Request{Kind: "test.defer", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{}`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(t.Context(), `UPDATE creative_jobs.river_job SET max_attempts=1 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	select {
	case event := <-events:
		if event == nil || event.Job.ID != id || calls.Load() != 7 || event.Job.Attempt != 1 {
			t.Fatalf("deferral exhausted attempts: calls=%d event=%+v", calls.Load(), event)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("job did not finish after %d calls", calls.Load())
	}
}
