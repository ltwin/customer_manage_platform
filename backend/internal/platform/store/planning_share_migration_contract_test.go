package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const installShareAssignmentFixture = `
CREATE TABLE share_assignment_source_event_v1 (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    generation BIGINT NOT NULL,
    plan_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('assignment_activated','assignment_revoked')),
    PRIMARY KEY (account_id, event_id),
    UNIQUE (account_id, generation, plan_id, event_id, mutation_kind),
    CONSTRAINT fk_planshare_assignment_event_to_planning_reminder_work
      FOREIGN KEY (account_id, generation, plan_id, event_id, mutation_kind)
      REFERENCES planning_reminder_generation_work
        (account_id, generation, plan_id, source_event_id, mutation_kind)
      DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE planning_reminder_generation_work
  ADD CONSTRAINT fk_planning_reminder_assignment_work_to_planshare_event
  FOREIGN KEY (account_id, generation, plan_id, source_event_id, mutation_kind)
  REFERENCES share_assignment_source_event_v1
    (account_id, generation, plan_id, event_id, mutation_kind)
  DEFERRABLE INITIALLY DEFERRED;`

func TestPlanningShareReciprocalFKAndPopulatedDownWriterExclusion(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts(id,password_hash) VALUES('share-fk-acct','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO shoot_plans(id,account_id,title,subject) VALUES('share-fk-plan','share-fk-acct','计划','主体')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, installShareAssignmentFixture); err != nil {
		t.Fatalf("install reciprocal constraints: %v", err)
	}

	pairTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairTx.ExecContext(ctx, `INSERT INTO planning_reminder_generation_work(account_id,generation,plan_id,mutation_kind,source_event_id) VALUES('share-fk-acct',1,'share-fk-plan','assignment_activated','event-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pairTx.ExecContext(ctx, `INSERT INTO share_assignment_source_event_v1(account_id,generation,plan_id,event_id,mutation_kind) VALUES('share-fk-acct',1,'share-fk-plan','event-1','assignment_activated')`); err != nil {
		t.Fatal(err)
	}
	if err := pairTx.Commit(); err != nil {
		t.Fatalf("commit reciprocal pair: %v", err)
	}

	// Writer-first: the down protocol acquires work then event SHARE locks, sees
	// populated data, and rolls back without touching either relation/constraint.
	downTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := downTx.ExecContext(ctx, `LOCK TABLE planning_reminder_generation_work IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	if _, err := downTx.ExecContext(ctx, `LOCK TABLE share_assignment_source_event_v1 IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	var populated bool
	if err := downTx.QueryRowContext(ctx, `SELECT EXISTS(
      SELECT 1 FROM share_assignment_source_event_v1
      UNION ALL
      SELECT 1 FROM planning_reminder_generation_work
      WHERE mutation_kind IN ('assignment_activated','assignment_revoked'))`).Scan(&populated); err != nil {
		t.Fatal(err)
	}
	if !populated {
		t.Fatal("populated down fixture unexpectedly empty")
	}
	if err := downTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var pairCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM share_assignment_source_event_v1`).Scan(&pairCount); err != nil || pairCount != 1 {
		t.Fatalf("writer-first rejection lost source event: count=%d err=%v", pairCount, err)
	}

	// Return to the only supported down precondition: an unpublished empty pair.
	if _, err := db.ExecContext(ctx, `DELETE FROM share_assignment_source_event_v1; DELETE FROM planning_reminder_generation_work WHERE mutation_kind IN ('assignment_activated','assignment_revoked')`); err != nil {
		t.Fatal(err)
	}
	downTx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := downTx.ExecContext(ctx, `LOCK TABLE planning_reminder_generation_work IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	if _, err := downTx.ExecContext(ctx, `LOCK TABLE share_assignment_source_event_v1 IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}

	writerStarted := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			writerDone <- err
			return
		}
		defer func() { _ = writerTx.Rollback() }()
		close(writerStarted)
		if _, err := writerTx.ExecContext(ctx, `INSERT INTO planning_reminder_generation_work(account_id,generation,plan_id,mutation_kind,source_event_id) VALUES('share-fk-acct',2,'share-fk-plan','assignment_activated','event-2')`); err != nil {
			writerDone <- err
			return
		}
		if _, err := writerTx.ExecContext(ctx, `INSERT INTO share_assignment_source_event_v1(account_id,generation,plan_id,event_id,mutation_kind) VALUES('share-fk-acct',2,'share-fk-plan','event-2','assignment_activated')`); err != nil {
			writerDone <- err
			return
		}
		writerDone <- writerTx.Commit()
	}()
	<-writerStarted
	select {
	case err := <-writerDone:
		t.Fatalf("writer bypassed down SHARE locks: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := downTx.ExecContext(ctx, `
ALTER TABLE planning_reminder_generation_work
  DROP CONSTRAINT fk_planning_reminder_assignment_work_to_planshare_event;
ALTER TABLE share_assignment_source_event_v1
  DROP CONSTRAINT fk_planshare_assignment_event_to_planning_reminder_work;
DROP TABLE share_assignment_source_event_v1;`); err != nil {
		t.Fatalf("empty down DDL: %v", err)
	}
	if err := downTx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-writerDone; err == nil {
		t.Fatal("old writer path unexpectedly committed after share schema down")
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM planning_reminder_generation_work WHERE source_event_id='event-2'`).Scan(&pairCount); err != nil {
		t.Fatal(err)
	}
	if pairCount != 0 {
		t.Fatalf("down-first writer rollback left %d core work orphan", pairCount)
	}
}

func TestPlanningShareReciprocalFKRejectsCrossPlanIdentity(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `
INSERT INTO accounts(id,password_hash) VALUES('cross-plan-acct','hash');
INSERT INTO shoot_plans(id,account_id,title,subject) VALUES
 ('cross-plan-a','cross-plan-acct','A','主体'),('cross-plan-b','cross-plan-acct','B','主体');`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, installShareAssignmentFixture); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO planning_reminder_generation_work(account_id,generation,plan_id,mutation_kind,source_event_id) VALUES('cross-plan-acct',1,'cross-plan-a','assignment_activated','cross-event')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO share_assignment_source_event_v1(account_id,generation,plan_id,event_id,mutation_kind) VALUES('cross-plan-acct',1,'cross-plan-b','cross-event','assignment_activated')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("cross-plan reciprocal identity unexpectedly committed")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}
