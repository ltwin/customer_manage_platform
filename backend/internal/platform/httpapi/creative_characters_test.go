package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCreativeCharacterHTTP(t *testing.T) {
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
VALUES('card-http-png',$1,'local','character','v1','sha256-'||repeat('1',64),50,'image/png','ready',clock_timestamp())`, testAcctID); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	var image creativecontent.Revision
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		var err error
		image, err = creativecontent.WriteMediaAndRetain(ctx, tx, creativecontent.Draft{Kind: "image"},
			[]creativecontent.ObjectBinding{{Role: "original", BlobID: "card-http-png"}}, func(r creativecontent.Revision) error {
				_, err := creativelibrary.CreateFromRevisionInTx(ctx, tx, r, creativelibrary.ImportTarget{Title: "立绘"})
				return err
			})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	post := func(path string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		id := uuid.NewString()
		body, err := json.Marshal(map[string]any{"operation_id": id, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return shootPlanningRequest(t, router, "POST", path, token, id, string(body))
	}
	card := creativecontent.CharacterPayload{
		Name: "云", Appearance: "青绿色短发", IP: &creativecontent.CharacterIP{Name: "Genshin"},
		Views:   []creativecontent.CharacterView{{ContentRevisionID: "ccrv_missing", View: "front"}},
		Sources: []creativecontent.CharacterSource{},
	}
	const createPath = "/api/v1/creative/assets/characters"
	missing := post(createPath, creativelibrary.CreateCharacterInput{Character: card})
	if missing.Code != 400 || documentErrorCode(t, missing) != "creative_image_unavailable" ||
		!strings.Contains(missing.Body.String(), `"content_revision_ids":["ccrv_missing"]`) {
		t.Fatalf("missing image: %d %s", missing.Code, missing.Body.String())
	}
	card.Views[0].ContentRevisionID = image.ID
	created := post(createPath, creativelibrary.CreateCharacterInput{Character: card})
	var asset creativelibrary.AssetResult
	if err := json.Unmarshal(created.Body.Bytes(), &asset); err != nil || created.Code != 201 || asset.ID == "" ||
		!strings.Contains(created.Body.String(), `"reused":false`) {
		t.Fatalf("create: %d %s (%v)", created.Code, created.Body.String(), err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		return shootPlanningRequest(t, router, "GET", path, token, "", "")
	}
	page := get("/api/v1/creative/assets?kind=character&character_ip=Genshin")
	if page.Code != 200 || !strings.Contains(page.Body.String(), `"view_count":1`) {
		t.Fatalf("list: %d %s", page.Code, page.Body.String())
	}
	ips := get("/api/v1/creative/character-ips?query=Genshin")
	if ips.Code != 200 || !strings.Contains(ips.Body.String(), `"character_count":1`) {
		t.Fatalf("IP suggestions: %d %s", ips.Code, ips.Body.String())
	}
	path := "/api/v1/creative/assets/" + asset.ID + "/character-revisions"
	card.Appearance = "青绿色短发，银色胸针"
	saved := post(path, map[string]any{"character": card, "expected_revision": "1"})
	if saved.Code != 200 || !strings.Contains(saved.Body.String(), `"revision":"2"`) {
		t.Fatalf("save: %d %s", saved.Code, saved.Body.String())
	}
	stale := post(path, map[string]any{"character": card, "expected_revision": "1"})
	if stale.Code != 409 || documentErrorCode(t, stale) != "creative_revision_conflict" {
		t.Fatalf("stale: %d %s", stale.Code, stale.Body.String())
	}
}
