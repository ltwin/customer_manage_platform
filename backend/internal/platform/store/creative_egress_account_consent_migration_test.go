package store_test

import (
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackEgressAccountConsent undoes 0084 and then 0083.
func rollbackEgressAccountConsent(t *testing.T, url string) {
	t.Helper()
	rollbackAgentToolTasks(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-egress-account-consent", err)
	}
}

const (
	accountConsentA = `INSERT INTO creative_egress_consents(id,account_id,vendor_key,purpose,scope,policy_version)
VALUES('ccec_00000000-0000-0000-0000-00000000000a','acct','platform','creative_assistance',
 '{"mode":"account_library","data_classes":["text"]}','egress-2026-11-1')`
	accountConsentB = `INSERT INTO creative_egress_consents(id,account_id,vendor_key,purpose,scope,policy_version)
VALUES('ccec_00000000-0000-0000-0000-00000000000b','acct','platform','creative_assistance',
 '{"mode":"selected_revisions","data_classes":["text"]}','egress-2026-11-1')`
	accountConsentState = `INSERT INTO creative_egress_consent_states(account_id,current_consent_id)
VALUES('acct','ccec_00000000-0000-0000-0000-00000000000a')`
)

func TestCreativeEgressAccountConsentMigration(t *testing.T) {
	url, db := openMaterialTextures(t)
	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackEgressAccountConsent(t, url)
	if _, err := db.Exec(`SELECT 1 FROM creative_egress_consent_states`); err == nil {
		t.Fatal("state table survived rollback")
	}
	if _, err := db.Exec(`SELECT superseded_at FROM creative_egress_consents`); err == nil {
		t.Fatal("superseded_at survived rollback")
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{accountConsentA, accountConsentState} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	// A second unstamped account-level row is refused by the partial index.
	if _, err := db.Exec(accountConsentB); err == nil {
		t.Fatal("second current account-level consent accepted")
	}
	if _, err := db.Exec(`UPDATE creative_egress_consents SET superseded_at=now() WHERE id='ccec_00000000-0000-0000-0000-00000000000a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(accountConsentB); err != nil {
		t.Fatal("account-level consent refused after the old one was superseded", err)
	}
	// Conversation-level history is outside the index.
	for i := range 2 {
		q := `INSERT INTO creative_egress_consents(id,account_id,vendor_key,purpose,conversation_id,scope,policy_version)
VALUES($1,'acct','platform','creative_assistance','ccco_x','{"mode":"account_library","data_classes":["text"]}','egress-2026-10-2')`
		if _, err := db.Exec(q, "ccec_00000000-0000-0000-0000-00000000010"+string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO creative_egress_consent_states(account_id,current_consent_id) VALUES('other','bad')`); err == nil {
		t.Fatal("malformed current_consent_id accepted")
	}

	rollbackAgentToolTasks(t, url)
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "account-level egress consents exist") {
		t.Fatalf("lossy rollback accepted: %v", err)
	}
}
