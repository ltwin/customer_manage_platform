package creativecontent_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }
func textDraft(text string) creativecontent.Draft {
	return creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &text}, Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}
}
func setup(t *testing.T) (*sql.DB, store.AccountScope, store.AccountScope) {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('content-a','test','active'),('content-b','test','active');INSERT INTO creative_account_capabilities(account_id,read_enabled,manual_write_enabled) VALUES ('content-a',true,true),('content-b',true,true)`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return db, st.ScopeFor(auth.AccountContext{AccountID: "content-a"}), st.ScopeFor(auth.AccountContext{AccountID: "content-b"})
}
func write(t *testing.T, scope store.AccountScope, d creativecontent.Draft, source string, node *string, retain func(store.TxAccountScope, creativecontent.Revision) error) (creativecontent.Revision, error) {
	t.Helper()
	var result creativecontent.Revision
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(t.Context(), "manual_write"); err != nil {
			return err
		}
		var err error
		result, err = creativecontent.WriteAndRetain(t.Context(), tx, d, source, node, func(r creativecontent.Revision) error { return retain(tx, r) })
		return err
	})
	return result, err
}
func asset(ctx context.Context, tx store.TxAccountScope, r creativecontent.Revision) error {
	return tx.Insert(ctx, "creative_assets", []string{"id", "kind", "title", "normalized_title", "content_id", "content_revision_id"}, "ccas_"+uuid.NewString(), r.Kind, "文字参考", "文字参考", r.ContentID, r.ID)
}

func TestCreateRequiresMatchingRootAndCurrentUsage(t *testing.T) {
	db, a, b := setup(t)
	_, err := write(t, a, textDraft("未保留"), "", nil, func(store.TxAccountScope, creativecontent.Revision) error { return nil })
	if !errors.Is(err, creativecontent.ErrMissingRoot) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM creative_contents").Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan survived %d %v", count, err)
	}
	r, err := write(t, a, textDraft("只在本账号可见"), "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(t.Context(), tx, r) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creativecontent.Read(t.Context(), b, r.ID); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatal(err)
	}
	if err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.RequireUsable(t.Context(), tx, r.ID, "generation_reference")
		return err
	}); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='content-a'"); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecontent.Read(t.Context(), a, r.ID); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal(err)
	}
}

func TestTwoNodesForkThenAppendWithoutChangingAssetOrOtherNode(t *testing.T) {
	db, a, _ := setup(t)
	ctx := t.Context()
	original, err := write(t, a, textDraft("共同参考"), "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(ctx, tx, r) })
	if err != nil {
		t.Fatal(err)
	}
	err = a.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		for _, id := range []string{"node-a", "node-b"} {
			if err := tx.Insert(ctx, "creative_projects", []string{"id", "name"}, "project-"+id, id); err != nil {
				return err
			}
			if err := tx.Insert(ctx, "creative_canvases", []string{"id", "project_id", "name"}, "canvas-"+id, "project-"+id, id); err != nil {
				return err
			}
			if err := tx.Insert(ctx, "creative_nodes", []string{"id", "canvas_id", "type_key", "x", "y", "content_id", "content_revision_id"}, id, "canvas-"+id, "core.text", 0, 0, original.ContentID, original.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	nodeID := "node-a"
	bind := func(tx store.TxAccountScope, r creativecontent.Revision) error {
		_, err := tx.Update(ctx, "creative_nodes", "content_id=$2,content_revision_id=$3,data_revision=data_revision+1", "id=$4", r.ContentID, r.ID, nodeID)
		return err
	}
	first, err := write(t, a, textDraft("第一处修改"), original.ID, &nodeID, bind)
	if err != nil {
		t.Fatal(err)
	}
	second, err := write(t, a, textDraft("第二处修改"), first.ID, &nodeID, bind)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentID == original.ContentID || second.ContentID != first.ContentID || second.Sequence != 2 {
		t.Fatalf("bad fork/append: %+v %+v", first, second)
	}
	var other, assetRevision string
	if err := db.QueryRow("SELECT content_revision_id FROM creative_nodes WHERE id='node-b'").Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT content_revision_id FROM creative_assets WHERE account_id='content-a'").Scan(&assetRevision); err != nil {
		t.Fatal(err)
	}
	if other != original.ID || assetRevision != original.ID {
		t.Fatal("shared reference was overwritten")
	}
	read, err := creativecontent.Read(ctx, a, second.ID)
	if err != nil || read.Payload.Body == nil || *read.Payload.Body != "第二处修改" {
		t.Fatalf("reopen %+v %v", read, err)
	}
}

func TestLinkIsStoredWithoutFetchingAndInvalidKindsAreRejected(t *testing.T) {
	_, a, _ := setup(t)
	var requests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer remote.Close()
	link := remote.URL + "/reference"
	d := textDraft("")
	d.Kind = "link"
	d.Payload = creativecontent.Payload{URL: &link}
	r, err := write(t, a, d, "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(t.Context(), tx, r) })
	if err != nil {
		t.Fatal(err)
	}
	got, err := creativecontent.Read(t.Context(), a, r.ID)
	if err != nil || got.Payload.URL == nil || *got.Payload.URL != link {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("link fetched remote content")
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///tmp/file", "https://", "https://name:secret@example.com"} {
		d.Payload.URL = &bad
		if creativecontent.Validate(d) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestWrongContentPairCannotRetainRevision(t *testing.T) {
	_, a, _ := setup(t)
	_, err := write(t, a, textDraft("pair"), "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error {
		r.ContentID = "wrong-content"
		return asset(t.Context(), tx, r)
	})
	if !errors.Is(err, creativecontent.ErrMissingRoot) {
		t.Fatal(err)
	}
}

func TestRevisionSequenceDoesNotRewindAfterUnreferencedRevisionIsRemoved(t *testing.T) {
	_, a, _ := setup(t)
	owner := "former-node"
	retain := func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(t.Context(), tx, r) }
	first, err := write(t, a, textDraft("第一版"), "", &owner, retain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := write(t, a, textDraft("待清理版本"), first.ID, &owner, retain)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a future GC after releasing the only root of the newer revision.
	// The identity remains, as does the older revision's independent asset root.
	if err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		if _, err := tx.Delete(t.Context(), "creative_assets", "content_revision_id=$2", second.ID); err != nil {
			return err
		}
		_, err := tx.Delete(t.Context(), "creative_content_revisions", "id=$2", second.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	third, err := write(t, a, textDraft("继续创作"), first.ID, &owner, retain)
	if err != nil {
		t.Fatal(err)
	}
	if third.ContentID != first.ContentID || third.Sequence != 3 {
		t.Fatalf("sequence reused: %+v", third)
	}
}
