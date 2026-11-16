package store_test

import (
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackCharacterCards undoes 0083 and then 0082.
func rollbackCharacterCards(t *testing.T, url string) {
	t.Helper()
	rollbackEgressAccountConsent(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-character-cards", err)
	}
}

func TestCreativeCharacterCardsMigration(t *testing.T) {
	url, db := openMaterialTextures(t)
	rollbackCharacterCards(t, url)

	const insertCard = `INSERT INTO creative_contents(id,account_id,kind,created_by_kind)
VALUES('ccnt_card','acct','character','photographer')`
	if _, err := db.Exec(insertCard); err == nil {
		t.Fatal("character content accepted before the migration")
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insertCard); err != nil {
		t.Fatal(err)
	}
	rollbackEgressAccountConsent(t, url)
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "character content, assets or references exist") {
		t.Fatalf("lossy rollback accepted: %v", err)
	}
}
