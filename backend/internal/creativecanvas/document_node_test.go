package creativecanvas_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// sheet is a one-unit document type; Empty takes {"text": ...} options.
type sheet struct {
	key       string
	creatable bool
}

func (s sheet) Key() string         { return s.key }
func (s sheet) NodeTypeKey() string { return s.key }
func (sheet) ContentKind() string   { return "text" }
func (s sheet) Creatable() bool     { return s.creatable }

func (sheet) Empty(_ context.Context, _ store.TxAccountScope, _ string, opts json.RawMessage) (creativedocument.Snapshot, error) {
	var o struct {
		Text string `json:"text"`
	}
	if len(opts) > 0 {
		if err := creativeops.Decode(opts, &o); err != nil {
			return creativedocument.Snapshot{}, err
		}
	}
	if o.Text == "" {
		o.Text = "空白"
	}
	return creativedocument.Snapshot{Payload: creativecontent.Payload{Body: &o.Text}}, nil
}
func (sheet) Units(creativecontent.Payload) ([]string, error) { return []string{"text"}, nil }
func (sheet) Touched(context.Context, store.TxAccountScope, creativedocument.Current, creativedocument.Actor, []json.RawMessage) ([]string, error) {
	return nil, creativeops.ErrValidation
}
func (sheet) Apply(context.Context, store.TxAccountScope, creativedocument.Current, creativedocument.Actor, creativedocument.Retain, []json.RawMessage) (creativedocument.Snapshot, creativedocument.Changes, error) {
	return creativedocument.Snapshot{}, creativedocument.Changes{}, creativeops.ErrValidation
}
func (sheet) RestoreTouched(_ context.Context, _ store.TxAccountScope, _, _ creativedocument.Current, units []string) ([]string, error) {
	return units, nil
}
func (sheet) RestoreUnits(_ context.Context, _ store.TxAccountScope, old, _ creativedocument.Current, _ creativedocument.Retain, _ []string) (creativedocument.Snapshot, creativedocument.Changes, error) {
	return creativedocument.Snapshot{Payload: old.Content.Payload}, creativedocument.Changes{Changed: []string{"text"}}, nil
}
func (sheet) View(_ context.Context, _ store.ReadTxAccountScope, cur creativedocument.Current, _ []string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"text": *cur.Content.Payload.Body})
}
func (sheet) Summary(_ context.Context, _ store.ReadTxAccountScope, cur creativedocument.Current) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"text": *cur.Content.Payload.Body})
}

func tryGraph(t *testing.T, scope store.AccountScope, canvas string, actions ...creativecanvas.Action) error {
	t.Helper()
	snapshot, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	_, err = creativecanvas.ApplyCommands(t.Context(), scope, command(t, creativecanvas.BatchInput{CanvasID: canvas, ExpectedTopologyRevision: snapshot.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(snapshot), Actions: actions}))
	return err
}

type documentNodeView struct {
	Data struct {
		DocumentID *string `json:"document_id"`
	} `json:"data"`
	Status struct {
		ContentState string `json:"content_state"`
	} `json:"status"`
	Content         *creativecontent.Revision `json:"content"`
	DocumentSummary json.RawMessage           `json:"document_summary"`
	DocumentTrashed *bool                     `json:"document_trashed"`
}

func nodeView(t *testing.T, scope store.AccountScope, canvas, id string) documentNodeView {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(findNode(t, c, id))
	if err != nil {
		t.Fatal(err)
	}
	var v documentNodeView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func newNodeID() string { return "cwnode_" + uuid.NewString() }

func TestDocumentNodes(t *testing.T) {
	db, scope, other := setup(t)
	creativedocument.RegisterForTest(t, sheet{key: "test.sheet", creatable: true})
	p := project(t, scope)
	documents := func() int {
		return countRows(t, db, `SELECT count(*) FROM creative_documents`)
	}

	defs := map[string]creativecanvas.NodeDefinition{}
	for _, d := range creativecanvas.NodeDefinitions() {
		defs[d.TypeKey] = d
	}
	if !defs["test.sheet"].CreationSupported || !defs["test.sheet"].Maximize || defs[creativedocument.InternalTypeKey].CreationSupported || !defs["core.text"].CreationSupported {
		t.Fatalf("definitions %+v", defs)
	}

	// Creating puts the document, its first revision and the node in one change.
	id := newNodeID()
	added := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: id, TypeKey: "test.sheet", Title: "分镜", DocumentOptions: json.RawMessage(`{"text":"起始"}`)})
	v := nodeView(t, scope, p.CanvasID, id)
	if v.Data.DocumentID == nil || v.Status.ContentState != "ready" || string(v.DocumentSummary) != `{"text":"起始"}` || v.DocumentTrashed == nil || *v.DocumentTrashed {
		t.Fatalf("new document node %+v", v)
	}
	docID := *v.Data.DocumentID
	raw, err := creativedocument.Read(t.Context(), scope, docID)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Title    string `json:"title"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Title != "分镜" || doc.Revision != "1" {
		t.Fatalf("document %s (%v)", raw, err)
	}
	if n := countRows(t, db, `SELECT count(*) FROM creative_document_revisions WHERE document_id=$1 AND actor_kind='photographer'`, docID); n != 1 {
		t.Fatalf("%d revisions", n)
	}

	// Undo removes only the node.
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if _, err := creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: added.ChangeID, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if len(c.Nodes) != 0 {
		t.Fatalf("undo left %d nodes", len(c.Nodes))
	}
	if _, err := creativedocument.Read(t.Context(), scope, docID); err != nil {
		t.Fatal("undo took the document", err)
	}

	// The document goes back on the canvas by id.
	back := newNodeID()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: back, TypeKey: "test.sheet", DocumentID: docID})
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if n := findNode(t, c, back); n.DocumentID == nil || *n.DocumentID != docID || n.Title != "分镜" {
		t.Fatalf("placed node %+v", n)
	}

	otherProject := project(t, other)
	before := documents()
	for name, tc := range map[string]struct {
		scope   store.AccountScope
		canvas  string
		actions []creativecanvas.Action
		want    error
	}{
		"type not creatable":         {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: creativedocument.InternalTypeKey}}, creativeops.ErrValidation},
		"type not registered":        {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.missing"}}, creativeops.ErrValidation},
		"document with content":      {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet", Content: func() *creativecontent.Draft { d := draft("正文"); return &d }()}}, creativeops.ErrValidation},
		"document on a core node":    {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "core.text", DocumentID: docID}}, creativeops.ErrValidation},
		"options when placing":       {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet", DocumentID: docID, DocumentOptions: json.RawMessage(`{"text":"x"}`)}}, creativeops.ErrValidation},
		"wrong node type":            {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: creativedocument.InternalTypeKey, DocumentID: docID}}, creativeops.ErrValidation},
		"bad options":                {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet", DocumentOptions: json.RawMessage(`{"bogus":1}`)}}, creativeops.ErrValidation},
		"later action fails":         {scope, p.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet"}, {Type: "add_node", NodeID: "bad", TypeKey: "core.text"}}, creativeops.ErrValidation},
		"another account's document": {other, otherProject.CanvasID, []creativecanvas.Action{{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet", DocumentID: docID}}, creativedocument.ErrNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tryGraph(t, tc.scope, tc.canvas, tc.actions...); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if got := documents(); got != before {
				t.Fatalf("%d documents, want %d", got, before)
			}
		})
	}

	target := func() creativeops.Command { return command(t, map[string]string{"document_id": docID}) }
	if _, err := creativedocument.Trash(t.Context(), scope, target()); err != nil {
		t.Fatal(err)
	}
	if v := nodeView(t, scope, p.CanvasID, back); v.DocumentTrashed == nil || !*v.DocumentTrashed || string(v.DocumentSummary) != `{"text":"起始"}` || v.Status.ContentState != "ready" {
		t.Fatalf("node of a trashed document %+v", v)
	}
	if err := tryGraph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: newNodeID(), TypeKey: "test.sheet", DocumentID: docID}); !errors.Is(err, creativedocument.ErrTrashed) {
		t.Fatalf("placing a trashed document: %v", err)
	}
	if _, err := creativedocument.Purge(t.Context(), scope, target()); err != nil {
		t.Fatal(err)
	}
	if v := nodeView(t, scope, p.CanvasID, back); v.Status.ContentState != "unavailable" || string(v.DocumentSummary) != "null" || v.DocumentTrashed == nil || *v.DocumentTrashed {
		t.Fatalf("node of a purged document %+v", v)
	}

	gone := newNodeID()
	t.Run("type later unregistered", func(t *testing.T) {
		creativedocument.RegisterForTest(t, sheet{key: "test.gone", creatable: true})
		graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: gone, TypeKey: "test.gone"})
	})
	if v := nodeView(t, scope, p.CanvasID, gone); v.Status.ContentState != "unavailable" || v.Data.DocumentID == nil {
		t.Fatalf("node of an unregistered type %+v", v)
	}
}

func TestInternalDocumentNodeKeepsContentPreview(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	r, err := creativecanvas.CreateInternalDocument(t.Context(), scope, command(t, creativecanvas.CreateDocumentInput{Title: "独立文档", Content: draft("文档的正文")}))
	if err != nil {
		t.Fatal(err)
	}
	var d creativecanvas.Document
	if err := json.Unmarshal(r.Outcome.Response, &d); err != nil {
		t.Fatal(err)
	}
	id := newNodeID()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: id, TypeKey: creativedocument.InternalTypeKey, DocumentID: d.ID})
	v := nodeView(t, scope, p.CanvasID, id)
	var summary map[string]string
	if err := json.Unmarshal(v.DocumentSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if v.Content == nil || v.Content.Payload.Body == nil || *v.Content.Payload.Body != "文档的正文" || !reflect.DeepEqual(summary, map[string]string{"preview": "文档的正文"}) {
		t.Fatalf("internal document node %+v", v)
	}
}

func TestDocumentOnTwoCanvases(t *testing.T) {
	_, scope, _ := setup(t)
	first, second := project(t, scope), project(t, scope)
	r, err := creativecanvas.CreateInternalDocument(t.Context(), scope, command(t, creativecanvas.CreateDocumentInput{Title: "共享文档", Content: draft("初稿")}))
	if err != nil {
		t.Fatal(err)
	}
	var d creativecanvas.Document
	if err := json.Unmarshal(r.Outcome.Response, &d); err != nil {
		t.Fatal(err)
	}
	a, b := newNodeID(), newNodeID()
	graph(t, scope, first.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: a, TypeKey: creativedocument.InternalTypeKey, DocumentID: d.ID})
	graph(t, scope, second.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: b, TypeKey: creativedocument.InternalTypeKey, DocumentID: d.ID})
	edit, err := json.Marshal(map[string]any{"document_id": d.ID, "expected": map[string]string{"body": "1"}, "operations": []any{map[string]string{"op": "set_body", "text": "定稿"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creativedocument.Command(t.Context(), scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: edit}); err != nil {
		t.Fatal(err)
	}
	for canvas, id := range map[string]string{first.CanvasID: a, second.CanvasID: b} {
		if v := nodeView(t, scope, canvas, id); v.Content == nil || *v.Content.Payload.Body != "定稿" {
			t.Fatalf("node %s on %s %+v", id, canvas, v)
		}
	}
	graph(t, scope, first.CanvasID, creativecanvas.Action{Type: "remove_nodes", NodeIDs: []string{a}})
	if v := nodeView(t, scope, second.CanvasID, b); v.Status.ContentState != "ready" || *v.Content.Payload.Body != "定稿" {
		t.Fatalf("remaining node %+v", v)
	}
	if _, err := creativedocument.Read(t.Context(), scope, d.ID); err != nil {
		t.Fatal("removing a node took the document", err)
	}
}
