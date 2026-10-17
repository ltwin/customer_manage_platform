package creativecontent_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
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
	// 可展示的自有素材即可作生成参考；撤回展示授权后两种用途一起被拒。
	generation := func() error {
		return a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			_, err := creativecontent.RequireUsable(t.Context(), tx, r.ID, "generation_reference")
			return err
		})
	}
	if err := generation(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='content-a'"); err != nil {
		t.Fatal(err)
	}
	if err := generation(); !errors.Is(err, creativecontent.ErrUsageDenied) {
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

// A scene is a document snapshot: direct content writes (assets, canvas node
// content, generation) refuse it, and the document path still checks that it
// carries only a JSON object.
func TestSceneKindIsWrittenOnlyThroughADocument(t *testing.T) {
	_, a, _ := setup(t)
	scene := func(raw string) creativecontent.Draft {
		d := textDraft("")
		d.Kind = "scene"
		d.Payload = creativecontent.Payload{Scene: json.RawMessage(raw)}
		return d
	}
	ok := scene(` {"objects":[]}`)
	if creativecontent.Validate(ok) == nil {
		t.Fatal("direct content accepted a scene")
	}
	if _, err := write(t, a, ok, "", nil, func(store.TxAccountScope, creativecontent.Revision) error { return nil }); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("direct write: %v", err)
	}
	errReached := errors.New("reached retain")
	document := func(d creativecontent.Draft) error {
		return a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			if err := tx.RequireCreativeCapability(t.Context(), "manual_write"); err != nil {
				return err
			}
			_, err := creativecontent.WriteDocumentAndRetain(t.Context(), tx, d, func(creativecontent.Revision) error { return errReached })
			return err
		})
	}
	if err := document(ok); !errors.Is(err, errReached) {
		t.Fatalf("document write: %v", err)
	}
	body := "x"
	withBody := scene(`{}`)
	withBody.Payload.Body = &body
	text := textDraft("说明")
	text.Payload.Scene = json.RawMessage(`{}`)
	for name, d := range map[string]creativecontent.Draft{
		"missing":   scene(""),
		"array":     scene(`[]`),
		"malformed": scene(`{"objects":`),
		"too large": scene(`{"pad":"` + strings.Repeat("x", 512<<10) + `"}`),
		"with body": withBody,
		"on text":   text,
	} {
		if err := document(d); !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A model has exactly two trusted writers: the media worker writes the staged
// form, a scene command the complete form (optionally appending to an existing
// model). Every direct content entry refuses it.
func TestModelKindHasTwoTrustedWriters(t *testing.T) {
	db, a, b := setup(t)
	ctx := t.Context()
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at) VALUES
 ('blob-glb','content-a','local','creative-v2/content-a/blobs/glb','v1','sha256-'||repeat('1',64),100,'model/gltf-binary','ready',clock_timestamp()),
 ('blob-png','content-a','local','creative-v2/content-a/blobs/png','v1','sha256-'||repeat('2',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	model := func(raw string) creativecontent.Draft {
		d := textDraft("")
		d.Kind = "model"
		d.Payload = creativecontent.Payload{Model: json.RawMessage(raw)}
		return d
	}
	staged := model(`{"format":"glb","manifest":{}}`)
	complete := model(`{"format":"glb","generator":{},"manifest":{}}`)
	glb := creativecontent.ObjectBinding{Role: "original", BlobID: "blob-glb"}
	png := creativecontent.ObjectBinding{Role: "thumbnail", BlobID: "blob-png"}
	ref := 0
	held := func(tx store.TxAccountScope) func(creativecontent.Revision) error {
		return func(r creativecontent.Revision) error {
			ref++
			return tx.Insert(ctx, "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, "cwdoc_m", int64(ref), r.ID)
		}
	}
	run := func(scope store.AccountScope, f func(tx store.TxAccountScope) (creativecontent.Revision, error)) (creativecontent.Revision, error) {
		var out creativecontent.Revision
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			out, err = f(tx)
			return err
		})
		return out, err
	}

	if creativecontent.Validate(complete) == nil {
		t.Fatal("direct content accepted a model")
	}
	text := textDraft("说明")
	text.Payload.Model = json.RawMessage(`{}`)
	if creativecontent.Validate(text) == nil {
		t.Fatal("model payload accepted on text")
	}
	refused := map[string]func(tx store.TxAccountScope) (creativecontent.Revision, error){
		"direct write": func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			return creativecontent.WriteAndRetain(ctx, tx, complete, "", nil, held(tx))
		},
		"document write": func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			return creativecontent.WriteDocumentAndRetain(ctx, tx, complete, held(tx))
		},
		"model entry with image kind": func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			d := complete
			d.Kind = "image"
			return creativecontent.WriteModelAndRetain(ctx, tx, d, "", []creativecontent.ObjectBinding{glb}, held(tx))
		},
		"model entry without objects": func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			return creativecontent.WriteModelAndRetain(ctx, tx, complete, "", nil, held(tx))
		},
		"model entry with array payload": func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			return creativecontent.WriteModelAndRetain(ctx, tx, model(`[]`), "", []creativecontent.ObjectBinding{glb}, held(tx))
		},
	}
	for name, f := range refused {
		if _, err := run(a, f); !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}

	stagedRev, err := run(a, func(tx store.TxAccountScope) (creativecontent.Revision, error) {
		return creativecontent.WriteMediaAndRetain(ctx, tx, staged, []creativecontent.ObjectBinding{glb}, held(tx))
	})
	if err != nil {
		t.Fatal("staged form:", err)
	}
	if len(stagedRev.Media) != 1 || stagedRev.Media[0].Role != "original" || stagedRev.Media[0].Mime != "model/gltf-binary" {
		t.Fatalf("staged media %+v", stagedRev.Media)
	}
	first, err := run(a, func(tx store.TxAccountScope) (creativecontent.Revision, error) {
		return creativecontent.WriteModelAndRetain(ctx, tx, complete, "", []creativecontent.ObjectBinding{glb, png}, held(tx))
	})
	if err != nil {
		t.Fatal("complete form:", err)
	}
	if first.ContentID == stagedRev.ContentID || first.Sequence != 1 || len(first.Media) != 2 {
		t.Fatalf("complete form %+v", first)
	}
	second, err := run(a, func(tx store.TxAccountScope) (creativecontent.Revision, error) {
		return creativecontent.WriteModelAndRetain(ctx, tx, complete, first.ContentID, []creativecontent.ObjectBinding{glb, png}, held(tx))
	})
	if err != nil || second.ContentID != first.ContentID || second.Sequence != 2 {
		t.Fatalf("append: %+v %v", second, err)
	}
	read, err := run(a, func(tx store.TxAccountScope) (creativecontent.Revision, error) {
		return creativecontent.RequireUsable(ctx, tx, second.ID, "display")
	})
	if err != nil || len(read.Media) != 2 || read.Kind != "model" {
		t.Fatalf("read model: %+v %v", read, err)
	}
	var batch map[string]creativecontent.DisplayResult
	if err := a.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		var err error
		batch, err = creativecontent.ReadDisplayBatch(ctx, tx, []string{second.ID})
		return err
	}); err != nil || batch[second.ID].Err != nil || len(batch[second.ID].Revision.Media) != 2 {
		t.Fatalf("batch read model: %+v %v", batch, err)
	}

	textRev, err := write(t, a, textDraft("不是模型"), "", nil, func(tx store.TxAccountScope, r creativecontent.Revision) error { return asset(ctx, tx, r) })
	if err != nil {
		t.Fatal(err)
	}
	appendTo := func(scope store.AccountScope, base string) error {
		_, err := run(scope, func(tx store.TxAccountScope) (creativecontent.Revision, error) {
			return creativecontent.WriteModelAndRetain(ctx, tx, complete, base, []creativecontent.ObjectBinding{glb, png}, held(tx))
		})
		return err
	}
	if err := appendTo(a, textRev.ContentID); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("append to text: %v", err)
	}
	if err := appendTo(a, "ccnt_missing"); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("append to missing: %v", err)
	}
	if err := appendTo(b, first.ContentID); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("append across accounts: %v", err)
	}
}
