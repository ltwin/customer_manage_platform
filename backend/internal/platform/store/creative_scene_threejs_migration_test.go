package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackSceneThreejs is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackSceneThreejs(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-scene-threejs-fields", err)
	}
}

// A scene in the format before v7: fog, a camera and one light of each type.
const oldScene = `{"environment":{"sky":{"kind":"color","color":"#203040"},"ambient":{"color":"#ffffff","intensity":0.4},
 "fog":{"color":"#808080","near_m":5,"far_m":50}},
 "render":{"mode":"clay"},
 "objects":[
  {"id":"sobj_000000000000000000001","kind":"camera","camera":{"focal_mm":35,"sensor_mm":[36,24],"aspect":"3:2","near_m":0.1,"far_m":200,"label":"A"}},
  {"id":"sobj_000000000000000000002","kind":"light","light":{"type":"directional","color":"#ffffff","intensity":3,"range_m":null,"angle_deg":null,"penumbra":null,"cast_shadow":true}},
  {"id":"sobj_000000000000000000003","kind":"light","light":{"type":"point","color":"#ff963d","intensity":45,"range_m":7,"angle_deg":null,"penumbra":null,"cast_shadow":false}},
  {"id":"sobj_000000000000000000004","kind":"light","light":{"type":"point","color":"#ffffff","intensity":10,"range_m":null,"angle_deg":null,"penumbra":null,"cast_shadow":false}},
  {"id":"sobj_000000000000000000005","kind":"light","light":{"type":"spot","color":"#ffeedd","intensity":50,"range_m":20,"angle_deg":30,"penumbra":0.2,"cast_shadow":false}},
  {"id":"sobj_000000000000000000006","kind":"primitive","primitive":{"builtin":"box@1","params":{"width":1,"height":1,"depth":1}}}]}`

// The same scene after 0077; the spot's 30° full cone is a 15° half-angle.
const newScene = `{"environment":{"sky":{"kind":"color","color":"#203040"},"ambient":{"color":"#ffffff","intensity":0.4},
 "fog":{"color":"#808080","near":5,"far":50}},
 "render":{"mode":"clay"},
 "objects":[
  {"id":"sobj_000000000000000000001","kind":"camera","camera":{"focal_mm":35,"sensor_mm":[36,24],"aspect":"3:2","near":0.1,"far":200,"label":"A"}},
  {"id":"sobj_000000000000000000002","kind":"light","light":{"type":"directional","color":"#ffffff","intensity":3,"cast_shadow":true}},
  {"id":"sobj_000000000000000000003","kind":"light","light":{"type":"point","color":"#ff963d","intensity":45,"cast_shadow":false,"distance":7,"decay":2}},
  {"id":"sobj_000000000000000000004","kind":"light","light":{"type":"point","color":"#ffffff","intensity":10,"cast_shadow":false,"distance":0,"decay":2}},
  {"id":"sobj_000000000000000000005","kind":"light","light":{"type":"spot","color":"#ffeedd","intensity":50,"cast_shadow":false,"distance":20,"decay":2,"angle":0.2617993877991494,"penumbra":0.2}},
  {"id":"sobj_000000000000000000006","kind":"primitive","primitive":{"builtin":"box@1","params":{"width":1,"height":1,"depth":1}}}]}`

func TestSceneThreejsMigrationRenamesFieldsAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// The clean rollback runs first: a refused one leaves the version dirty.
	rollbackSceneThreejs(t, url)
	insert := func(content, kind, revision, payload string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO creative_contents(id,account_id,kind,created_by_kind) VALUES($1,'acct',$2,'photographer') ON CONFLICT DO NOTHING`, content, kind); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id)
VALUES($1,'acct',$2,(SELECT count(*)+1 FROM creative_content_revisions WHERE content_id=$2),1,$3,'decl')`, revision, content, payload); err != nil {
			t.Fatal(err)
		}
	}
	insert("ccnt_scene", "scene", "ccrev_old", `{"scene":`+oldScene+`}`)
	insert("ccnt_scene", "scene", "ccrev_bare", `{"scene":{"environment":{"sky":{"kind":"color","color":"#000000"},"ambient":{"color":"#ffffff","intensity":1},"fog":null},"render":{"mode":"clay"},"objects":[]}}`)
	// Only scene revisions are rewritten.
	insert("ccnt_text", "text", "ccrev_text", `{"text":"near_m","scene":{"objects":[{"camera":{"near_m":1}}]}}`)
	text := `{"text":"near_m","scene":{"objects":[{"camera":{"near_m":1}}]}}`
	scene := func(revision, want string) {
		t.Helper()
		var same bool
		var got string
		if err := db.QueryRow(`SELECT payload->'scene' = $2::jsonb, payload->>'scene' FROM creative_content_revisions WHERE id=$1`, revision, want).Scan(&same, &got); err != nil {
			t.Fatal(err)
		}
		if !same {
			t.Fatalf("%s scene\n got %s\nwant %s", revision, got, want)
		}
	}
	textUntouched := func() {
		t.Helper()
		var same bool
		if err := db.QueryRow(`SELECT payload = $1::jsonb FROM creative_content_revisions WHERE id='ccrev_text'`, text).Scan(&same); err != nil || !same {
			t.Fatal("a text revision was rewritten", err)
		}
	}
	bare := `{"environment":{"sky":{"kind":"color","color":"#000000"},"ambient":{"color":"#ffffff","intensity":1},"fog":null},"render":{"mode":"clay"},"objects":[]}`

	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	scene("ccrev_old", newScene)
	scene("ccrev_bare", bare)
	textUntouched()

	rollbackSceneThreejs(t, url)
	scene("ccrev_old", oldScene)
	scene("ccrev_bare", bare)
	textUntouched()

	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE creative_content_revisions SET payload = jsonb_set(payload, '{scene,objects,2,light,decay}', '3') WHERE id='ccrev_old'`); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "decay other than 2") {
		t.Fatal("lossy rollback allowed", err)
	}
}
