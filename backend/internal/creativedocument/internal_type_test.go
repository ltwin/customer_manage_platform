package creativedocument_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func textDraft(body string) creativecontent.Draft {
	return creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body},
		Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}
}

func createInternal(t *testing.T, scope store.AccountScope, title string, draft creativecontent.Draft) (creativedocument.Document, error) {
	t.Helper()
	var d creativedocument.Document
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		d, err = creativedocument.CreateInternalInTx(t.Context(), tx, title, draft, creativedocument.Photographer)
		return err
	})
	return d, err
}

type internalView struct {
	TypeKey           string                   `json:"type_key"`
	Revision          creativeops.Revision     `json:"revision"`
	ContentRevisionID string                   `json:"content_revision_id"`
	UnitRevs          map[string]string        `json:"unit_revs"`
	Content           creativecontent.Revision `json:"content"`
}

func readInternal(t *testing.T, scope store.AccountScope, id string) internalView {
	t.Helper()
	raw, err := creativedocument.Read(t.Context(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var v internalView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func body(v internalView) string {
	if v.Content.Payload.Body == nil {
		return ""
	}
	return *v.Content.Payload.Body
}

func TestInternalDocument(t *testing.T) {
	f := setup(t)
	d, err := createInternal(t, f.a, "光线与空间", textDraft("侧逆光"))
	if err != nil {
		t.Fatal(err)
	}
	if d.TypeKey != creativedocument.InternalTypeKey || !reflect.DeepEqual(d.UnitRevs, map[string]int64{"meta": 1, "body": 1}) {
		t.Fatalf("created %+v", d)
	}
	// The read keeps the original content field the frontend renders.
	v := readInternal(t, f.a, d.ID)
	if v.TypeKey != creativedocument.InternalTypeKey || v.Content.ID != v.ContentRevisionID || body(v) != "侧逆光" {
		t.Fatalf("read %+v", v)
	}

	setBody := func(expected, text string) batch {
		return batch{DocumentID: d.ID, Expected: map[string]any{"body": expected}, Operations: []any{op{"op": "set_body", "text": text}}}
	}
	if res := mustSend(t, f.a, setBody("1", "顶光")); res.Revision != 2 || !reflect.DeepEqual(res.ChangedUnits, []string{"body"}) {
		t.Fatalf("set_body %+v", res)
	}
	if v := readInternal(t, f.a, d.ID); body(v) != "顶光" || v.UnitRevs["body"] != "2" {
		t.Fatalf("after set_body %+v", v)
	}
	for name, b := range map[string]batch{
		"empty body":   setBody("2", ""),
		"twice":        {DocumentID: d.ID, Expected: map[string]any{"body": "2"}, Operations: []any{op{"op": "set_body", "text": "a"}, op{"op": "set_body", "text": "b"}}},
		"unknown op":   {DocumentID: d.ID, Expected: map[string]any{"body": "2"}, Operations: []any{op{"op": "add_items", "ids": []string{"a"}}}},
		"unknown unit": {DocumentID: d.ID, Expected: map[string]any{"note": nil}, Operations: []any{op{"op": "restore_units", "from_revision": "1", "units": []string{"note"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}

	raw, err := creativedocument.ReadRevision(t.Context(), f.a, d.ID, 1, []string{"body"})
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		View map[string]string `json:"view"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || !reflect.DeepEqual(old.View, map[string]string{"body": "侧逆光"}) {
		t.Fatalf("revision 1 %s (%v)", raw, err)
	}
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"body": "2"}, Operations: []any{op{"op": "restore_units", "from_revision": "1", "units": []string{"body"}}}}) // 3
	if got := body(readInternal(t, f.a, d.ID)); got != "侧逆光" {
		t.Fatalf("after restore_units %q", got)
	}
	mustSend(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "3", Operations: []any{op{"op": "restore", "revision": "2"}}}) // 4
	if v := readInternal(t, f.a, d.ID); body(v) != "顶光" || v.UnitRevs["body"] != "4" {
		t.Fatalf("after restore %+v", v)
	}

	long := strings.Repeat("光", 250)
	mustSend(t, f.a, setBody("4", long))
	page, err := creativedocument.List(t.Context(), f.a, creativedocument.ListQuery{Type: creativedocument.InternalTypeKey})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list %+v (%v)", page, err)
	}
	var summary struct {
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(page.Items[0].Summary, &summary); err != nil || summary.Preview != strings.Repeat("光", 200) {
		t.Fatalf("summary %s (%v)", page.Items[0].Summary, err)
	}

	link := "https://example.com"
	_, err = createInternal(t, f.a, "链接", creativecontent.Draft{Kind: "link", Payload: creativecontent.Payload{URL: &link}})
	wantErr(t, err, creativeops.ErrValidation)
}
