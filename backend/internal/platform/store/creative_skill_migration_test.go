package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestSkillFoundationMigrationShapeAndLossyRollback pins the 0045 shape and
// proves a rollback can never silently drop a frozen skill version, which is
// the only copy of what a run was told to do.
func TestSkillFoundationMigrationShapeAndLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	tables := []string{
		"creative_skills", "creative_skill_versions", "creative_skill_version_resources",
		"creative_skill_imports", "creative_message_skill_refs",
	}
	// A skill is business data even when the platform is its author, so every
	// one of these tables carries an explicit account key.
	for _, table := range tables {
		var scoped bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns
			 WHERE table_schema='public' AND table_name=$1 AND column_name='account_id')`,
			table).Scan(&scoped); err != nil || !scoped {
			t.Fatalf("table %s is missing or unscoped: %v", table, err)
		}
	}

	// The empty rollback runs first: a refused one leaves the schema version
	// dirty, so the recoverable case has to be proved before the refusal.
	// 0046 (agent runs) sits above this migration; step past it first.
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-context migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback with no versions must succeed: %v", err)
	}
	for _, table := range tables {
		var exists bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
			table).Scan(&exists); err != nil || exists {
			t.Fatalf("table %s survived the rollback: %v", table, err)
		}
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}

	const skillID = "ccsk_00000000-0000-4000-8000-000000000001"
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('mig-skill','test','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_skills(id,account_id,origin,slug,display_name,description)
		VALUES($1,'mig-skill','platform','reference-direction','参考整理','说明')`, skillID); err != nil {
		t.Fatal(err)
	}
	// A marketplace pointer answers a different question than the author's own
	// current version, and no marketplace exists yet: the column is reserved,
	// not usable.
	if _, err := db.Exec(`UPDATE creative_skills SET marketplace_version_id=$1 WHERE id=$1`, skillID); err == nil {
		t.Fatal("the reserved marketplace pointer must stay empty until a marketplace publishes")
	}
	// One author cannot own the same discovery name twice.
	if _, err := db.Exec(`INSERT INTO creative_skills(id,account_id,origin,slug,display_name,description)
		VALUES('ccsk_00000000-0000-4000-8000-000000000002','mig-skill','platform','reference-direction','别名','说明')`); err == nil {
		t.Fatal("a slug must be unique per author")
	}

	version := func(id, number string) error {
		_, err := db.Exec(`INSERT INTO creative_skill_versions
			(id,account_id,skill_id,version_number,schema_version,display_name_snapshot,
			 description_snapshot,instructions,manifest,digest)
			VALUES($1,'mig-skill',$2,$3,1,'参考整理','说明','正文','{}'::jsonb,repeat('a',64))`,
			id, skillID, number)
		return err
	}
	if err := version("ccsv_00000000-0000-4000-8000-000000000001", "1"); err != nil {
		t.Fatal(err)
	}
	if err := version("ccsv_00000000-0000-4000-8000-000000000002", "1"); err == nil {
		t.Fatal("two versions must never share one number in a skill")
	}

	// Execution control and the reason for it move together: an active version
	// carrying a withdrawal reason would make the catalog unreadable.
	if _, err := db.Exec(`UPDATE creative_skill_versions SET disabled_reason='内容有误'
		WHERE id='ccsv_00000000-0000-4000-8000-000000000001'`); err == nil {
		t.Fatal("an active version must not carry a withdrawal reason")
	}
	if _, err := db.Exec(`UPDATE creative_skill_versions
		SET execution_status='disabled', disabled_at=clock_timestamp(), disabled_reason='内容有误'
		WHERE id='ccsv_00000000-0000-4000-8000-000000000001'`); err != nil {
		t.Fatalf("withdrawing a version must stay possible: %v", err)
	}

	// A finalised import must name the version it produced; anything else makes
	// "did this operation already publish?" unanswerable after a crash.
	if _, err := db.Exec(`INSERT INTO creative_skill_imports
		(id,account_id,operation_id,request_hash,target_skill_id,state,expires_at)
		VALUES('ccsi_00000000-0000-4000-8000-000000000001','mig-skill','op-1',repeat('b',64),$1,
		       'finalized',clock_timestamp())`, skillID); err == nil {
		t.Fatal("a finalized import must record its resulting version")
	}
	if _, err := db.Exec(`INSERT INTO creative_skill_imports
		(id,account_id,operation_id,request_hash,target_skill_id,state,expires_at)
		VALUES('ccsi_00000000-0000-4000-8000-000000000001','mig-skill','op-1',repeat('b',64),$1,
		       'preparing',clock_timestamp())`, skillID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_skill_imports
		(id,account_id,operation_id,request_hash,target_skill_id,state,expires_at)
		VALUES('ccsi_00000000-0000-4000-8000-000000000002','mig-skill','op-1',repeat('c',64),$1,
		       'preparing',clock_timestamp())`, skillID); err == nil {
		t.Fatal("one operation id must identify one import")
	}

	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-context migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback agent-runs migration: %v", err)
	}
	if err := store.MigrateDownOneForTest(url); err == nil {
		t.Fatal("a rollback that would drop frozen skill versions must be refused")
	} else if !strings.Contains(err.Error(), "export") {
		t.Fatalf("the refusal must say what to do first: %v", err)
	}
}
