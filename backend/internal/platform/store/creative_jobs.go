package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
)

const creativeQueueSchema = "creative_jobs"

// JobHandler is trusted application composition; task input cannot register code.
// Work must use the request's operation identity for durable effect deduplication.
type JobHandler struct {
	Kind string
	Work func(context.Context, AccountScope, jobs.Request) error
}

type queuedCreativeJob struct {
	AccountID     string       `json:"account_id"`
	SchemaVersion int          `json:"schema_version"`
	Request       jobs.Request `json:"request"`
}

func (queuedCreativeJob) Kind() string { return "creative_dispatch_v1" }

type creativeJobRuntime struct {
	store    *Store
	client   *river.Client[pgx.Tx]
	handlers map[string]func(context.Context, AccountScope, jobs.Request) error
}

// NewJobRuntime shares the store's connection pool; no second pool or raw tx is
// given to domain code. An empty registry is insert/start-disabled, not a no-op worker.
func (s *Store) NewJobRuntime(handlers []JobHandler, logger *slog.Logger) (jobs.Runtime, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("store is not open")
	}
	runtime := &creativeJobRuntime{store: s, handlers: make(map[string]func(context.Context, AccountScope, jobs.Request) error)}
	for _, h := range handlers {
		if h.Kind == "" || h.Work == nil {
			return nil, jobs.ErrInvalidTask
		}
		if _, exists := runtime.handlers[h.Kind]; exists {
			return nil, jobs.ErrInvalidTask
		}
		runtime.handlers[h.Kind] = h.Work
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &creativeDispatcher{runtime: runtime})
	client, err := river.NewClient(riverpgxv5.New(s.pool), &river.Config{
		Schema: creativeQueueSchema, Workers: workers, Logger: logger,
		Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
	})
	if err != nil {
		return nil, fmt.Errorf("create creative jobs: %w", err)
	}
	runtime.client = client
	return runtime, nil
}
func (r *creativeJobRuntime) Start(ctx context.Context) error {
	if len(r.handlers) == 0 {
		return jobs.ErrNoHandlers
	}
	if err := r.Check(ctx); err != nil {
		return err
	}
	return r.client.Start(ctx)
}
func (r *creativeJobRuntime) Stop(ctx context.Context) error { return r.client.Stop(ctx) }
func (r *creativeJobRuntime) Check(ctx context.Context) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(r.store.pool), &rivermigrate.Config{Schema: creativeQueueSchema})
	if err != nil {
		return fmt.Errorf("prepare queue readiness: %w", err)
	}
	result, err := migrator.Validate(ctx, nil)
	if err != nil {
		return fmt.Errorf("validate queue migrations: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("creative jobs migrations incomplete: %v", result.Messages)
	}
	return nil
}

func (sc TxAccountScope) Jobs(runtime jobs.Runtime) jobs.TxEnqueuer {
	return jobs.BindTrustedTxEnqueuer(func(ctx context.Context, request jobs.Request) (int64, error) {
		r, ok := runtime.(*creativeJobRuntime)
		tx, transactional := sc.scope.runner.(pgx.Tx)
		if !ok || !transactional || sc.AccountID() == "" || sc.scope.pool != r.store.pool {
			return 0, jobs.ErrInvalidTask
		}
		if _, registered := r.handlers[request.Kind]; !registered {
			return 0, jobs.ErrInvalidTask
		}
		result, err := r.client.InsertTx(ctx, tx, queuedCreativeJob{AccountID: sc.AccountID(), SchemaVersion: 1, Request: request}, &river.InsertOpts{MaxAttempts: 5})
		if err != nil {
			return 0, fmt.Errorf("enqueue creative task: %w", err)
		}
		return result.Job.ID, nil
	})
}

type creativeDispatcher struct {
	river.WorkerDefaults[queuedCreativeJob]
	runtime *creativeJobRuntime
}

func (w *creativeDispatcher) Timeout(*river.Job[queuedCreativeJob]) time.Duration {
	return 2 * time.Minute
}
func (w *creativeDispatcher) Work(ctx context.Context, job *river.Job[queuedCreativeJob]) error {
	a := job.Args
	if a.SchemaVersion != 1 || a.AccountID == "" {
		return river.JobCancel(jobs.ErrInvalidTask)
	}
	if err := a.Request.Validate(); err != nil {
		return river.JobCancel(err)
	}
	handler, ok := w.runtime.handlers[a.Request.Kind]
	if !ok {
		return river.JobCancel(jobs.ErrInvalidTask)
	}
	var status string
	err := w.runtime.store.pool.QueryRow(ctx, "SELECT status FROM accounts WHERE id = $1", a.AccountID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return river.JobCancel(ErrCreativeAccessDenied)
	}
	if err != nil {
		return err
	}
	// The account comes from the trusted queue envelope, not request payload. The
	// command executor still rechecks capability and active account at effect time.
	scope := w.runtime.store.ScopeFor(auth.AccountContext{AccountID: a.AccountID})
	a.Request.Payload = append(json.RawMessage(nil), a.Request.Payload...)
	return handler(ctx, scope, a.Request)
}

// MigrateCreativeJobs is explicit and separate from application migrations.
func (s *Store) MigrateCreativeJobs(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended('creative-jobs-migrations', 0))"); err != nil {
			_ = conn.Conn().Close(cleanup) // Never return a session carrying a migration lock to the pool.
		}
		conn.Release()
	}()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended('creative-jobs-migrations', 0))"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS creative_jobs"); err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: creativeQueueSchema})
	if err != nil {
		return err
	}
	// River must commit individual migrations: later migrations use newly added
	// enum values, which PostgreSQL forbids within the transaction adding them.
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

var _ jobs.Runtime = (*creativeJobRuntime)(nil)
