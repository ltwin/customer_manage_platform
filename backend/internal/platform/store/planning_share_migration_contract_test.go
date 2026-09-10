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
	seedShareAssignmentPair(t, db, ctx, "share-fk-acct", "share-fk-plan", "asgn-1", "event-1", 1)

	pairTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairTx.ExecContext(ctx, `INSERT INTO planning_reminder_generation_work(account_id,generation,plan_id,mutation_kind,source_event_id) VALUES('share-fk-acct',1,'share-fk-plan','assignment_activated','event-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pairTx.ExecContext(ctx, `
INSERT INTO share_assignment_source_event_v1(
  event_id, account_id, plan_id, assignment_id, assignment_revision,
  account_source_generation, event_kind, assignment_kind, readiness_item_id,
  preparation_lead_days_snapshot, lead_rule_version, content_fingerprint
) VALUES (
  'event-1','share-fk-acct','share-fk-plan','asgn-1',1,
  1,'assignment_activated','readiness','ready-1',
  3,'platform-default-v1','fp1'
)`); err != nil {
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
	if _, err := db.ExecContext(ctx, `
DELETE FROM share_assignment_source_event_v1;
DELETE FROM planning_reminder_generation_work WHERE mutation_kind IN ('assignment_activated','assignment_revoked');
DELETE FROM share_assignments;
DELETE FROM share_generations;
`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0039 creative library organization down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0038 creative text canvas down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0037 creative foundation down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0036 creative workspace down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0035 settings health tiers down before 0034: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0034 orders attribution snapshot down before 0033: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0033 orders payment facts down before 0032: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0032 orders delivery due down before 0031: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0031 media gallery down before 0030: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0030 list enrichment down before 0029: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0029 business feedback down before 0028: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0028 digest intent down before 0027: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0027 temporal down before 0026: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0026 down before 0024 lock protocol: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0025 down before 0024 lock protocol: %v", err)
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
		if _, err := writerTx.ExecContext(ctx, `
INSERT INTO share_assignment_source_event_v1(
  event_id, account_id, plan_id, assignment_id, assignment_revision,
  account_source_generation, event_kind, assignment_kind, readiness_item_id,
  preparation_lead_days_snapshot, lead_rule_version, content_fingerprint
) VALUES (
  'event-2','share-fk-acct','share-fk-plan','asgn-1',1,
  2,'assignment_activated','readiness','ready-1',
  3,'platform-default-v1','fp1'
)`); err != nil {
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
	seedShareAssignmentPair(t, db, ctx, "cross-plan-acct", "cross-plan-a", "asgn-cross", "cross-event", 1)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO planning_reminder_generation_work(account_id,generation,plan_id,mutation_kind,source_event_id) VALUES('cross-plan-acct',1,'cross-plan-a','assignment_activated','cross-event')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO share_assignment_source_event_v1(
  event_id, account_id, plan_id, assignment_id, assignment_revision,
  account_source_generation, event_kind, assignment_kind, readiness_item_id,
  preparation_lead_days_snapshot, lead_rule_version, content_fingerprint
) VALUES (
  'cross-event','cross-plan-acct','cross-plan-b','asgn-cross',1,
  1,'assignment_activated','readiness','ready-1',
  3,'platform-default-v1','fp1'
)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("cross-plan reciprocal identity unexpectedly committed")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}

func TestPlanningShareEmptyMigrationRoundTrip(t *testing.T) {
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
	var exists bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_name = 'share_assignment_source_event_v1'
)`).Scan(&exists); err != nil || !exists {
		t.Fatalf("0024 event table missing: exists=%v err=%v", exists, err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0039 creative library organization down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0038 creative text canvas down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0037 creative foundation down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0036 creative workspace down: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0035 settings health tiers down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0034 orders attribution snapshot down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0033 orders payment facts down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0032 orders delivery due down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0031 media gallery down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0030 list enrichment down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0029 business feedback down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0028 digest intent down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0027 temporal down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0026 down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty 0025 down failed: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("empty down failed: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_name = 'share_assignment_source_event_v1'
)`).Scan(&exists); err != nil || exists {
		t.Fatalf("0024 event table still present after empty down: exists=%v err=%v", exists, err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("re-up after empty down failed: %v", err)
	}
}

func seedShareAssignmentPair(t *testing.T, db *sql.DB, ctx context.Context, accountID, planID, assignmentID, _ string, _ int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO share_generations(
  id, account_id, plan_id, view_level, generation, selector, secret_commitment, fingerprint,
  state, expires_at, issued_at, revision
) VALUES (
  $1, $2, $3, 'proposal', 1, $4, decode('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','hex'),
  $5, 'active', now() + interval '1 day', now(), 1
)`, "gen-"+assignmentID, accountID, planID, "sel"+assignmentID, "fp"+assignmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO share_assignments(
  id, account_id, plan_id, token_generation_id, assignment_kind, readiness_item_id,
  content_snapshot, claimed_by_display_name, preparation_lead_days_snapshot, lead_rule_version,
  status, claim_receipt_commitment, revision
) VALUES (
  $1, $2, $3, $4, 'readiness', 'ready-1',
  '准备内容', '匿名', 3, 'platform-default-v1',
  'active', decode('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','hex'), 1
)`, assignmentID, accountID, planID, "gen-"+assignmentID); err != nil {
		t.Fatal(err)
	}
}
