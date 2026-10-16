package creativereference_test

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

// A revision referenced only by a document's history stays held — media GC
// selects by the same condition — and is released once that history is gone.
func TestDocumentHistoryHoldsReferencedRevisions(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('holder','test','active');
 INSERT INTO creative_account_capabilities(account_id,read_enabled,manual_write_enabled) VALUES ('holder',true,true)`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	scope := st.ScopeFor(auth.AccountContext{AccountID: "holder"})
	text := "侧逆光"
	draft := creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &text},
		Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}

	var referenced, snapshot creativecontent.Revision
	err = scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		// Each write returns only after its holder exists; the document rows are that holder.
		referenced, err = creativecontent.WriteAndRetain(t.Context(), tx, draft, "", nil, func(r creativecontent.Revision) error {
			return tx.Insert(t.Context(), "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, "cwdoc_h", int64(1), r.ID)
		})
		if err != nil {
			return err
		}
		snapshot, err = creativecontent.WriteAndRetain(t.Context(), tx, draft, "", nil, func(r creativecontent.Revision) error {
			return tx.Insert(t.Context(), "creative_document_revisions", []string{"document_id", "revision", "content_revision_id", "title", "actor_kind", "unit_revs"}, "cwdoc_h", int64(1), r.ID, "策划", "photographer", `{"meta":1}`)
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	live := func() map[string]bool {
		var out map[string]bool
		if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			out, err = creativereference.Live(t.Context(), tx, []string{referenced.ID, snapshot.ID})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := live(); !got[referenced.ID] || !got[snapshot.ID] {
		t.Fatalf("document history does not hold its revisions: %v", got)
	}
	if _, err := db.Exec(`DELETE FROM creative_document_content_refs; DELETE FROM creative_document_revisions`); err != nil {
		t.Fatal(err)
	}
	if got := live(); got[referenced.ID] || got[snapshot.ID] {
		t.Fatalf("purged history still holds revisions: %v", got)
	}
}
