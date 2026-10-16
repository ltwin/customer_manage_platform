package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func rollbackProviderLimits(t *testing.T, url string) {
	t.Helper()
	rollbackDocumentFoundation(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback provider quota retirement", err)
	}
}

func TestProviderQuotaRetirementPreservesNewAndLegacyAttemptsOnRollback(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	const insert = `INSERT INTO llm_attempts(id,account_id,request_id,attempt_number,reservation_id,reservation_generation,dispatch_epoch,permit_digest,limit_key)
 VALUES($1,'a',$1,1,'reservation',1,1,$2,$3)`
	for i, key := range []string{"", "runninghub/images"} {
		id := "llma_" + uuid.NewString()
		hash := "sha256-" + strings.Repeat(string(rune('a'+i)), 64)
		if _, err = db.Exec(insert, id, hash, key); err != nil {
			t.Fatal(err)
		}
	}
	rollbackProviderLimits(t, url)
	var total int
	if err = db.QueryRow(`SELECT count(*) FROM llm_attempts`).Scan(&total); err != nil || total != 2 {
		t.Fatalf("rollback lost attempts: %d %v", total, err)
	}
	if _, err = db.Exec(`UPDATE llm_attempts SET error_class='recovery' WHERE limit_key=''`); err != nil {
		t.Fatal("new-format attempt cannot finish after rollback", err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
