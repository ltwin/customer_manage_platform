package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackPlatformSkillOwner is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackPlatformSkillOwner(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-skill-runtime", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-platform-skill-owner", err)
	}
}

// A deployment that filed its platform catalog under a configured account gets
// it moved to the reserved owner, versions, resources, imports and the owner
// recorded on messages and runs included. That account's own skills stay put.
func TestPlatformSkillOwnerMigrationMovesOnlyThePlatformCatalog(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	rollbackPlatformSkillOwner(t, url)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	const (
		platformSkill = "ccsk_00000000-0000-0000-0000-000000000001"
		ownSkill      = "ccsk_00000000-0000-0000-0000-000000000002"
		version       = "ccsv_00000000-0000-0000-0000-000000000001"
		digest        = "0000000000000000000000000000000000000000000000000000000000000000"
	)
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('legacy-platform','test','active');
 INSERT INTO creative_skills(id,account_id,origin,slug,display_name,description,current_version_id)
 VALUES('` + platformSkill + `','legacy-platform','platform','shared','共享','述','` + version + `'),
       ('` + ownSkill + `','legacy-platform','account','own','自己','述',NULL);
 INSERT INTO creative_skill_versions(id,account_id,skill_id,version_number,schema_version,display_name_snapshot,description_snapshot,instructions,manifest,digest)
 VALUES('` + version + `','legacy-platform','` + platformSkill + `',1,1,'共享','述','正文','{}','` + digest + `');
 INSERT INTO creative_skill_version_resources(account_id,skill_version_id,path,mime,byte_size,sha256,storage_driver,bucket,object_key,object_version)
 VALUES('legacy-platform','` + version + `','a.md','text/markdown',1,'` + digest + `','local','','creative-skills/legacy-platform/imports/x/a.md','v1');
 INSERT INTO creative_skill_imports(id,account_id,operation_id,request_hash,target_skill_id,state,verified_manifest,expires_at)
 VALUES('ccsi_00000000-0000-0000-0000-000000000001','legacy-platform','op-platform','` + digest + `','` + platformSkill + `','preparing','{"origin":"platform"}',now()),
       ('ccsi_00000000-0000-0000-0000-000000000002','legacy-platform','op-own','` + digest + `','` + ownSkill + `','preparing','{"origin":"account"}',now());
 INSERT INTO creative_message_skill_refs(account_id,message_id,segment_ordinal,skill_id,skill_version_id,skill_owner_account_id,digest)
 VALUES('reader','m',0,'` + platformSkill + `','` + version + `','legacy-platform','` + digest + `');
 INSERT INTO creative_run_skill_refs(account_id,run_id,segment_ordinal,skill_id,skill_version_id,skill_owner_account_id,digest)
 VALUES('reader','r',0,'` + platformSkill + `','` + version + `','legacy-platform','` + digest + `')`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ query, want string }{
		{`SELECT account_id FROM creative_skills WHERE id='` + platformSkill + `'`, "platform"},
		{`SELECT account_id FROM creative_skills WHERE id='` + ownSkill + `'`, "legacy-platform"},
		{`SELECT account_id FROM creative_skill_versions WHERE id='` + version + `'`, "platform"},
		{`SELECT account_id FROM creative_skill_version_resources WHERE skill_version_id='` + version + `'`, "platform"},
		{`SELECT account_id FROM creative_skill_imports WHERE operation_id='op-platform'`, "platform"},
		{`SELECT account_id FROM creative_skill_imports WHERE operation_id='op-own'`, "legacy-platform"},
		{`SELECT skill_owner_account_id FROM creative_message_skill_refs WHERE message_id='m'`, "platform"},
		{`SELECT skill_owner_account_id FROM creative_run_skill_refs WHERE run_id='r'`, "platform"},
		// The reader's own key never moves.
		{`SELECT account_id FROM creative_run_skill_refs WHERE run_id='r'`, "reader"},
	} {
		var got string
		if err := db.QueryRow(check.query).Scan(&got); err != nil {
			t.Fatal(check.query, err)
		}
		if got != check.want {
			t.Fatalf("%s = %q, want %q", check.query, got, check.want)
		}
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-skill-runtime", err)
	}
	// Nothing records which account held the catalog before, so going back
	// would be a guess.
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "before rollback") {
		t.Fatal("lossy rollback", err)
	}
}

func TestPlatformSkillOwnerEmptyMigrationCanRollbackAndReapply(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	rollbackPlatformSkillOwner(t, url)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
