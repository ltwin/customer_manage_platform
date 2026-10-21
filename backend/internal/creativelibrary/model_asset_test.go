package creativelibrary_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const modelCode = "export default function build() {}"

// saveModel writes the complete form of a model the way a scene command does
// and files it into the library.
func saveModel(t *testing.T, a store.AccountScope, title string) (creativelibrary.AssetResult, creativecontent.Revision) {
	t.Helper()
	d := textDraft("")
	d.Kind, d.Payload = "model", creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","name_hint":"柱","manifest":{"triangles":12},
 "generator":{"language":"three-module","code":"` + modelCode + `","code_sha256":"sha256-x","mount":"floor","params":{}}}`)}
	var (
		result   creativelibrary.AssetResult
		revision creativecontent.Revision
	)
	err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		revision, err = creativecontent.WriteModelAndRetain(t.Context(), tx, d, "", []creativecontent.ObjectBinding{{Role: "original", BlobID: "blob-glb"}, {Role: "thumbnail", BlobID: "blob-png"}},
			func(r creativecontent.Revision) error {
				result, err = creativelibrary.SaveRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: title})
				return err
			})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, revision
}

func TestModelAssetsListFilterAndTrashWithoutCode(t *testing.T) {
	db, a, _ := setup(t)
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at) VALUES
 ('blob-glb','library-a','local','creative-v2/library-a/blobs/glb','v1','sha256-'||repeat('1',64),100,'model/gltf-binary','ready',clock_timestamp()),
 ('blob-png','library-a','local','creative-v2/library-a/blobs/png','v1','sha256-'||repeat('2',64),50,'image/png','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(creativelibrary.AssetKinds(), ","), "model") {
		t.Fatal("model is not an asset kind")
	}
	long := strings.Repeat("长", 2500)
	r, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "长文", Content: textDraft(long)}))
	text := decode[creativelibrary.AssetResult](t, r, err)
	model, revision := saveModel(t, a, "哥特束柱")

	p := search(t, a, creativelibrary.Search{Kind: "model", IncludeFacets: true})
	if p.TotalCount != 1 || p.Items[0].ID != model.ID || p.Items[0].Kind != "model" {
		t.Fatalf("model filter %+v", p.Items)
	}
	if p.Facets.Kinds["model"] != 1 || p.Facets.Kinds["text"] != 1 {
		t.Fatalf("facets %+v", p.Facets.Kinds)
	}
	noCode := func(where string, c *creativecontent.Revision) {
		t.Helper()
		if c == nil || c.Kind != "model" || !strings.Contains(string(c.Payload.Model), `"code_sha256"`) {
			t.Fatalf("%s: model content missing: %+v", where, c)
		}
		if strings.Contains(string(c.Payload.Model), modelCode) || strings.Contains(string(c.Payload.Model), `"code":`) {
			t.Fatalf("%s: code sent: %s", where, c.Payload.Model)
		}
	}
	noCode("list", p.Items[0].Content)
	got, err := creativelibrary.GetAsset(t.Context(), a, model.ID)
	if err != nil {
		t.Fatal(err)
	}
	noCode("read", got.Content)
	full, err := creativecontent.Read(t.Context(), a, revision.ID)
	if err != nil || !strings.Contains(string(full.Payload.Model), modelCode) {
		t.Fatalf("content revision read lost the code: %v", err)
	}
	if got, err = creativelibrary.GetAsset(t.Context(), a, text.ID); err != nil || got.Content.Truncated || *got.Content.Payload.Body != long {
		t.Fatalf("text read is no longer complete: %v", err)
	}

	// Saving the same revision again reuses the asset.
	again := saveModelRevision(t, a, revision)
	if again.ID != model.ID {
		t.Fatalf("same revision made a second asset %s", again.ID)
	}

	r, err = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: model.ID, Revision: again.Revision}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if p := search(t, a, creativelibrary.Search{Kind: "model"}); p.TotalCount != 0 {
		t.Fatal("trashed model still listed")
	}
	if p := search(t, a, creativelibrary.Search{Kind: "model", View: "trash"}); p.TotalCount != 1 {
		t.Fatal("trashed model missing from the trash")
	}
	r, err = creativelibrary.Restore(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: model.ID, Revision: again.Revision + 1}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if p := search(t, a, creativelibrary.Search{Kind: "model"}); p.TotalCount != 1 {
		t.Fatal("restored model not listed")
	}
}

func saveModelRevision(t *testing.T, a store.AccountScope, r creativecontent.Revision) creativelibrary.AssetResult {
	t.Helper()
	var result creativelibrary.AssetResult
	err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		result, err = creativelibrary.SaveRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: "再存"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
