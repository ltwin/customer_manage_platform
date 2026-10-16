package creativedocument_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) {
	creativedocument.Register(board{})
	storetest.Main(m, store.MigrateUp)
}

type fixture struct {
	db   *sql.DB
	a, b store.AccountScope
}

func setup(t *testing.T) fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('doc-a','test','active'),('doc-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return fixture{db: db, a: st.ScopeFor(auth.AccountContext{AccountID: "doc-a"}), b: st.ScopeFor(auth.AccountContext{AccountID: "doc-b"})}
}

func createBoard(t *testing.T, scope store.AccountScope, title string) creativedocument.Document {
	t.Helper()
	var d creativedocument.Document
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		d, err = creativedocument.CreateInTx(t.Context(), tx, board{}, title, nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

// batch is the command payload; expected values are revision strings or nil
// for "does not exist".
type batch struct {
	DocumentID       string         `json:"document_id"`
	Expected         map[string]any `json:"expected,omitempty"`
	ExpectedRevision string         `json:"expected_revision,omitempty"`
	Operations       []any          `json:"operations"`
}

type op = map[string]any

func r(n int64) string { return strconv.FormatInt(n, 10) }

func command(t *testing.T, payload any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}

// run never calls t.Fatal so goroutines may use it.
func run(t *testing.T, scope store.AccountScope, actor creativedocument.Actor, c creativeops.Command) (creativedocument.CommandResult, []byte, error) {
	receipt, err := creativedocument.CommandAs(t.Context(), scope, actor, c)
	if err != nil {
		return creativedocument.CommandResult{}, nil, err
	}
	var out creativedocument.CommandResult
	err = json.Unmarshal(receipt.Outcome.Response, &out)
	return out, receipt.Outcome.Response, err
}

func send(t *testing.T, scope store.AccountScope, b batch) (creativedocument.CommandResult, error) {
	out, _, err := run(t, scope, creativedocument.Photographer, command(t, b))
	return out, err
}

func mustSend(t *testing.T, scope store.AccountScope, b batch) creativedocument.CommandResult {
	t.Helper()
	out, err := send(t, scope, b)
	if err != nil {
		t.Fatalf("command %+v: %v", b.Operations, err)
	}
	return out
}

func agent(runID string) creativedocument.Actor {
	return creativedocument.Actor{Kind: "agent", RunID: &runID}
}

type boardView struct {
	Title     string               `json:"title"`
	Revision  creativeops.Revision `json:"revision"`
	UnitRevs  map[string]string    `json:"unit_revs"`
	TrashedAt *time.Time           `json:"trashed_at"`
	Board     boardState           `json:"board"`
}

func readBoard(t *testing.T, scope store.AccountScope, id string) boardView {
	t.Helper()
	raw, err := creativedocument.Read(t.Context(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var v boardView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func conflicts(t *testing.T, err error) string {
	t.Helper()
	c, ok := creativedocument.IsPublicConflict(err)
	if !ok || !errors.Is(err, creativedocument.ErrConflict) {
		t.Fatalf("want a unit conflict, got %v", err)
	}
	raw, err := json.Marshal(c.Conflicts)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

func revisionsOf(page creativedocument.RevisionPage) []int64 {
	out := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, int64(item.Revision))
	}
	return out
}

func listRevisions(t *testing.T, scope store.AccountScope, id string, q creativedocument.RevisionQuery) creativedocument.RevisionPage {
	t.Helper()
	page, err := creativedocument.ListRevisions(t.Context(), scope, id, q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// sameJSON compares receipts semantically; a replay comes back from JSONB.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type renamed struct {
	board
	key, node string
}

func (r renamed) Key() string         { return r.key }
func (r renamed) NodeTypeKey() string { return r.node }

func TestRegistry(t *testing.T) {
	if typ, ok := creativedocument.Lookup(creativedocument.InternalTypeKey); !ok || typ.Creatable() {
		t.Fatalf("internal type: %v %v", typ, ok)
	}
	if typ, ok := creativedocument.ForNode("test.board"); !ok || typ.Key() != "test.board" {
		t.Fatalf("board by node type: %v %v", typ, ok)
	}
	t.Run("register for test", func(t *testing.T) {
		creativedocument.RegisterForTest(t, renamed{key: "test.registry", node: "test.registry.node"})
		if typ, ok := creativedocument.ForNode("test.registry.node"); !ok || typ.Key() != "test.registry" {
			t.Fatalf("registered type: %v %v", typ, ok)
		}
		keys := map[string]bool{}
		for _, typ := range creativedocument.NodeTypes() {
			keys[typ.NodeTypeKey()] = true
		}
		if !keys["test.registry.node"] || !keys["test.board"] || !keys[creativedocument.InternalTypeKey] {
			t.Fatalf("node types: %v", keys)
		}
	})
	if _, ok := creativedocument.Lookup("test.registry"); ok {
		t.Fatal("RegisterForTest left its type behind")
	}
	for name, typ := range map[string]creativedocument.Type{
		"same key":       renamed{key: "test.board", node: "test.other"},
		"same node type": renamed{key: "test.other", node: "test.board"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a repeated registration did not panic")
				}
				_, byKey := creativedocument.Lookup("test.other")
				_, byNode := creativedocument.ForNode("test.other")
				if byKey || byNode {
					t.Fatal("a rejected registration left half of itself behind")
				}
			}()
			creativedocument.Register(typ)
		})
	}
}

func TestCreateAndRead(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "  ")
	want := map[string]int64{"meta": 1, "items": 1, "note": 1, "refs": 1}
	if d.Revision != 1 || d.Title != "未命名" || !reflect.DeepEqual(d.UnitRevs, want) {
		t.Fatalf("created %+v", d)
	}
	v := readBoard(t, f.a, d.ID)
	if v.Revision != 1 || v.Board.Note != "空白" || v.UnitRevs["note"] != "1" || v.TrashedAt != nil {
		t.Fatalf("read %+v", v)
	}
	raw, err := creativedocument.ReadRevision(t.Context(), f.a, d.ID, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		ActorKind    string            `json:"actor_kind"`
		ChangedUnits []string          `json:"changed_units"`
		UnitRevs     map[string]string `json:"unit_revs"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if old.ActorKind != "photographer" || !reflect.DeepEqual(old.ChangedUnits, []string{"*"}) || old.UnitRevs["meta"] != "1" {
		t.Fatalf("first revision %s", raw)
	}
	_, err = creativedocument.Read(t.Context(), f.b, d.ID)
	wantErr(t, err, creativedocument.ErrNotFound)

	internal, _ := creativedocument.Lookup(creativedocument.InternalTypeKey)
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativedocument.CreateInTx(t.Context(), tx, internal, "", nil, creativedocument.Photographer)
		return err
	})
	wantErr(t, err, creativeops.ErrValidation)
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativedocument.CreateInTx(t.Context(), tx, board{}, strings.Repeat("长", 201), nil, creativedocument.Photographer)
		return err
	})
	wantErr(t, err, creativeops.ErrValidation)
}

// addItems adds ids to a fresh board and returns the revision they landed in.
func addItems(t *testing.T, scope store.AccountScope, d creativedocument.Document, ids ...string) int64 {
	t.Helper()
	expected := map[string]any{"items": r(d.UnitRevs["items"])}
	for _, id := range ids {
		expected["item:"+id] = nil
	}
	return int64(mustSend(t, scope, batch{DocumentID: d.ID, Expected: expected, Operations: []any{op{"op": "add_items", "ids": ids}}}).Revision)
}

func race(t *testing.T, scope store.AccountScope, batches ...batch) ([]creativedocument.CommandResult, []error) {
	t.Helper()
	commands := make([]creativeops.Command, len(batches))
	for i, b := range batches {
		commands[i] = command(t, b)
	}
	results, errs := make([]creativedocument.CommandResult, len(batches)), make([]error, len(batches))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], _, errs[i] = run(t, scope, creativedocument.Photographer, commands[i])
		}()
	}
	close(start)
	wg.Wait()
	return results, errs
}

func TestWritesToDifferentUnitsBothLand(t *testing.T) {
	f := setup(t)
	for range 20 {
		d := createBoard(t, f.a, "")
		base := addItems(t, f.a, d, "a", "b")
		results, errs := race(t, f.a,
			batch{DocumentID: d.ID, Expected: map[string]any{"item:a": r(base)}, Operations: []any{op{"op": "set_item", "id": "a", "value": 5}}},
			batch{DocumentID: d.ID, Expected: map[string]any{"item:b": r(base)}, Operations: []any{op{"op": "set_item", "id": "b", "value": 6}}})
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("writes to different units: %v %v", errs[0], errs[1])
		}
		got := []int64{int64(results[0].Revision), int64(results[1].Revision)}
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		if got[0] != base+1 || got[1] != base+2 {
			t.Fatalf("revisions %v after %d", got, base)
		}
		v := readBoard(t, f.a, d.ID)
		if v.UnitRevs["item:a"] != r(int64(results[0].Revision)) || v.UnitRevs["item:b"] != r(int64(results[1].Revision)) || v.Board.Items["a"] != 5 || v.Board.Items["b"] != 6 {
			t.Fatalf("board %+v after %+v", v, results)
		}
		if page := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{}); !reflect.DeepEqual(revisionsOf(page), []int64{base + 2, base + 1, base, 1}) {
			t.Fatalf("history %v", revisionsOf(page))
		}
	}
}

func TestWritesToSameUnitExactlyOneWins(t *testing.T) {
	f := setup(t)
	for range 20 {
		d := createBoard(t, f.a, "")
		base := addItems(t, f.a, d, "a")
		results, errs := race(t, f.a,
			batch{DocumentID: d.ID, Expected: map[string]any{"item:a": r(base)}, Operations: []any{op{"op": "set_item", "id": "a", "value": 5}}},
			batch{DocumentID: d.ID, Expected: map[string]any{"item:a": r(base)}, Operations: []any{op{"op": "set_item", "id": "a", "value": 6}}})
		winner, loser := 0, 1
		if errs[0] != nil {
			winner, loser = 1, 0
		}
		if errs[winner] != nil || errs[loser] == nil {
			t.Fatalf("want exactly one winner: %v %v", errs[0], errs[1])
		}
		if got, want := conflicts(t, errs[loser]), fmt.Sprintf(`[{"unit":"item:a","expected_rev":"%d","current_rev":"%d"}]`, base, base+1); got != want {
			t.Fatalf("conflict %s, want %s", got, want)
		}
		v := readBoard(t, f.a, d.ID)
		if int64(results[winner].Revision) != base+1 || v.Revision != creativeops.Revision(base+1) || v.Board.Items["a"] != 5+winner {
			t.Fatalf("board %+v, winner %d", v, winner)
		}
	}
}

func TestExpectedExistence(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	add := batch{DocumentID: d.ID, Expected: map[string]any{"items": "1", "item:x": nil}, Operations: []any{op{"op": "add_items", "ids": []string{"x"}}}}
	res := mustSend(t, f.a, add)
	if res.Revision != 2 || !reflect.DeepEqual(res.ChangedUnits, []string{"item:x", "items"}) || res.ResetRequired {
		t.Fatalf("add %+v", res)
	}
	_, err := send(t, f.a, add)
	if got := conflicts(t, err); got != `[{"unit":"item:x","expected_rev":null,"current_rev":"2"},{"unit":"items","expected_rev":"1","current_rev":"2"}]` {
		t.Fatalf("adding twice: %s", got)
	}
	res = mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"items": "2", "item:x": "2"}, Operations: []any{op{"op": "remove_items", "ids": []string{"x"}}}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{"items"}) || !reflect.DeepEqual(res.RemovedUnits, []string{"item:x"}) {
		t.Fatalf("remove %+v", res)
	}
	if _, ok := readBoard(t, f.a, d.ID).UnitRevs["item:x"]; ok {
		t.Fatal("removed unit still has a revision")
	}
	// History keeps what was removed, not only the collection unit.
	if item := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 1}).Items[0]; !reflect.DeepEqual(item.ChangedUnits, []string{"items"}) || !reflect.DeepEqual(item.RemovedUnits, []string{"item:x"}) {
		t.Fatalf("history of a removal %+v", item)
	}
	raw, err := creativedocument.ReadRevision(t.Context(), f.a, d.ID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	var removal struct {
		RemovedUnits []string `json:"removed_units"`
	}
	if err := json.Unmarshal(raw, &removal); err != nil || !reflect.DeepEqual(removal.RemovedUnits, []string{"item:x"}) {
		t.Fatalf("revision 3 %s (%v)", raw, err)
	}
	// x is absent again, so only the collection unit catches the stale add.
	_, err = send(t, f.a, add)
	if got := conflicts(t, err); got != `[{"unit":"items","expected_rev":"1","current_rev":"3"}]` {
		t.Fatalf("stale add: %s", got)
	}
	_, err = send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:x": "2"}, Operations: []any{op{"op": "set_item", "id": "x", "value": 1}}})
	if got := conflicts(t, err); got != `[{"unit":"item:x","expected_rev":"2","current_rev":null}]` {
		t.Fatalf("stale edit of a removed unit: %s", got)
	}

	for name, b := range map[string]batch{
		"missing expected unit": {DocumentID: d.ID, Expected: map[string]any{"items": "3"}, Operations: []any{op{"op": "set_note", "text": "x"}}},
		"unit touched twice":    {DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}, op{"op": "set_note", "text": "y"}}},
		"bad unit name":         {DocumentID: d.ID, Expected: map[string]any{"note": "1", "bad unit": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}},
		"no expected":           {DocumentID: d.ID, Operations: []any{op{"op": "set_note", "text": "x"}}},
		"unknown op":            {DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "explode"}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
	_, err = send(t, f.b, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}})
	wantErr(t, err, creativedocument.ErrNotFound)
	_, _, err = run(t, f.a, creativedocument.Actor{Kind: "agent"}, command(t, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}}))
	wantErr(t, err, creativeops.ErrValidation)
}

func TestAnnotationKeepsUnitRevision(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	base := addItems(t, f.a, d, "a")
	res := mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:a": r(base)}, Operations: []any{op{"op": "confirm", "id": "a"}}})
	if res.Revision != creativeops.Revision(base+1) || len(res.ChangedUnits) != 0 || !reflect.DeepEqual(res.AnnotatedUnits, []string{"item:a"}) {
		t.Fatalf("confirm %+v", res)
	}
	v := readBoard(t, f.a, d.ID)
	if v.UnitRevs["item:a"] != r(base) || v.Board.Confirmed["a"] != base {
		t.Fatalf("board after confirm %+v", v)
	}
	item := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 1}).Items[0]
	if len(item.ChangedUnits) != 0 || !reflect.DeepEqual(item.AnnotatedUnits, []string{"item:a"}) {
		t.Fatalf("history %+v", item)
	}
	// A write prepared before the confirmation still lands.
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:a": r(base)}, Operations: []any{op{"op": "set_item", "id": "a", "value": 2}}})
}

func TestWholeRestore(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	addItems(t, f.a, d, "a", "b")                                                                                                                                                        // 2
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:a": "2", "item:b": "2"}, Operations: []any{op{"op": "confirm", "id": "a"}, op{"op": "confirm", "id": "b"}}}) // 3
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:b": "2"}, Operations: []any{op{"op": "set_item", "id": "b", "value": 9}}})                                   // 4
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"meta": "1"}, Operations: []any{op{"op": "rename", "title": "改名"}}})                                               // 5
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "改"}}})                                               // 6

	// A write that only touched the note still makes a whole restore stale.
	_, err := send(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "5", Operations: []any{op{"op": "restore", "revision": "3"}}})
	if got := conflicts(t, err); got != `[{"unit":"*","expected_rev":"5","current_rev":"6"}]` {
		t.Fatalf("stale restore: %s", got)
	}
	for name, b := range map[string]batch{
		"with expected":          {DocumentID: d.ID, ExpectedRevision: "6", Expected: map[string]any{"note": "6"}, Operations: []any{op{"op": "restore", "revision": "3"}}},
		"without revision":       {DocumentID: d.ID, Operations: []any{op{"op": "restore", "revision": "3"}}},
		"batched":                {DocumentID: d.ID, ExpectedRevision: "6", Operations: []any{op{"op": "restore", "revision": "3"}, op{"op": "set_note", "text": "x"}}},
		"revision on plain edit": {DocumentID: d.ID, ExpectedRevision: "6", Expected: map[string]any{"note": "6"}, Operations: []any{op{"op": "set_note", "text": "x"}}},
		"current revision":       {DocumentID: d.ID, ExpectedRevision: "6", Operations: []any{op{"op": "restore", "revision": "6"}}},
		"zero revision":          {DocumentID: d.ID, ExpectedRevision: "6", Operations: []any{op{"op": "restore", "revision": "0"}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}

	res := mustSend(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "6", Operations: []any{op{"op": "restore", "revision": "3"}}})
	if res.Revision != 7 || !res.ResetRequired || len(res.ChangedUnits)+len(res.AnnotatedUnits)+len(res.RemovedUnits) != 0 {
		t.Fatalf("restore %+v", res)
	}
	v := readBoard(t, f.a, d.ID)
	for unit, got := range v.UnitRevs {
		if got != "7" {
			t.Fatalf("unit %s at %s after restore", unit, got)
		}
	}
	// Unit revisions in revision 3 were item:b=2, matching b's confirmation;
	// the head had already moved b to 4.
	if len(v.UnitRevs) != 6 || v.Title != "未命名" || v.Board.Note != "空白" || v.Board.Items["b"] != 1 || !reflect.DeepEqual(v.Board.Confirmed, map[string]int64{"a": 7, "b": 7}) {
		t.Fatalf("restored board %+v", v)
	}
	item := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 1}).Items[0]
	if item.RestoredFrom == nil || *item.RestoredFrom != 3 || !reflect.DeepEqual(item.ChangedUnits, []string{"*"}) {
		t.Fatalf("restore history %+v", item)
	}
	// In revision 4, b had moved past its confirmation, so it is dropped.
	mustSend(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "7", Operations: []any{op{"op": "restore", "revision": "4"}}})
	if got := readBoard(t, f.a, d.ID).Board.Confirmed; !reflect.DeepEqual(got, map[string]int64{"a": 8}) {
		t.Fatalf("confirmations after restoring revision 4: %v", got)
	}
}

func TestRestoreUnits(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	addItems(t, f.a, d, "a", "b")                                                                                                                                       // 2
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:a": "2"}, Operations: []any{op{"op": "set_item", "id": "a", "value": 5}}})                  // 3
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"items": "2", "item:a": "3"}, Operations: []any{op{"op": "remove_items", "ids": []string{"a"}}}}) // 4
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"meta": "1"}, Operations: []any{op{"op": "rename", "title": "新"}}})                               // 5

	restore := func(from string, units ...string) op {
		return op{"op": "restore_units", "from_revision": from, "units": units}
	}
	res := mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:a": nil, "items": "4"}, Operations: []any{restore("3", "item:a")}})
	if res.Revision != 6 || !reflect.DeepEqual(res.ChangedUnits, []string{"item:a", "items"}) || len(res.RemovedUnits) != 0 {
		t.Fatalf("restore a removed unit %+v", res)
	}
	v := readBoard(t, f.a, d.ID)
	if v.Board.Items["a"] != 5 || v.UnitRevs["item:a"] != "6" || v.UnitRevs["items"] != "6" || v.UnitRevs["item:b"] != "2" {
		t.Fatalf("board %+v", v)
	}
	if item := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 1}).Items[0]; item.RestoredFrom == nil || *item.RestoredFrom != 3 {
		t.Fatalf("history %+v", item)
	}

	_, err := send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:b": "2", "items": "4"}, Operations: []any{restore("1", "item:b")}})
	if got := conflicts(t, err); got != `[{"unit":"items","expected_rev":"4","current_rev":"6"}]` {
		t.Fatalf("stale restore_units: %s", got)
	}
	res = mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"item:b": "2", "items": "6"}, Operations: []any{restore("1", "item:b")}})
	if !reflect.DeepEqual(res.ChangedUnits, []string{"items"}) || !reflect.DeepEqual(res.RemovedUnits, []string{"item:b"}) {
		t.Fatalf("restore to absent %+v", res)
	}
	if item := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 1}).Items[0]; !reflect.DeepEqual(item.RemovedUnits, []string{"item:b"}) {
		t.Fatalf("history of restoring to absent %+v", item)
	}
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"meta": "5"}, Operations: []any{restore("1", "meta")}})
	if v := readBoard(t, f.a, d.ID); v.Title != "未命名" || len(v.Board.Items) != 1 {
		t.Fatalf("after restoring meta %+v", v)
	}

	for name, b := range map[string]batch{
		"batched":           {DocumentID: d.ID, Expected: map[string]any{"meta": "8", "note": "1"}, Operations: []any{restore("1", "meta"), op{"op": "set_note", "text": "x"}}},
		"expected revision": {DocumentID: d.ID, ExpectedRevision: "8", Operations: []any{restore("1", "meta")}},
		"repeated unit":     {DocumentID: d.ID, Expected: map[string]any{"meta": "8"}, Operations: []any{restore("1", "meta", "meta")}},
		"no units":          {DocumentID: d.ID, Expected: map[string]any{"meta": "8"}, Operations: []any{restore("1")}},
		"current revision":  {DocumentID: d.ID, Expected: map[string]any{"meta": "8"}, Operations: []any{restore("8", "meta")}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, b)
			wantErr(t, err, creativeops.ErrValidation)
		})
	}
}

func TestRestoreReferences(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"refs": "1"}, Operations: []any{op{"op": "create_ref", "text": "参考图"}}}) // 2
	refs := readBoard(t, f.a, d.ID).Board.Refs
	if len(refs) != 1 {
		t.Fatalf("refs %v", refs)
	}
	ref := refs[0]
	restoreRefs := func(from, expected string) batch {
		return batch{DocumentID: d.ID, Expected: map[string]any{"refs": expected}, Operations: []any{op{"op": "restore_units", "from_revision": from, "units": []string{"refs"}}}}
	}
	mustSend(t, f.a, restoreRefs("1", "2")) // 3: no refs
	if _, err := f.db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='doc-a'
 AND declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE account_id='doc-a' AND id=$1)`, ref); err != nil {
		t.Fatal(err)
	}
	// A plain write checks display authorization...
	_, err := send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"refs": "3"}, Operations: []any{op{"op": "add_ref", "ref": ref}}})
	wantErr(t, err, creativecontent.ErrUsageDenied)
	// ...but a restore only needs the reference to be on record in that revision.
	mustSend(t, f.a, restoreRefs("2", "3")) // 4
	if got := readBoard(t, f.a, d.ID).Board.Refs; !reflect.DeepEqual(got, []string{ref}) {
		t.Fatalf("refs after restore_units %v", got)
	}
	mustSend(t, f.a, restoreRefs("1", "4"))                                                                                   // 5
	mustSend(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "5", Operations: []any{op{"op": "restore", "revision": "2"}}}) // 6
	if got := readBoard(t, f.a, d.ID).Board.Refs; !reflect.DeepEqual(got, []string{ref}) {
		t.Fatalf("refs after a whole restore %v", got)
	}

	// Usable content that revision 7 never recorded is still refused.
	other := createBoard(t, f.a, "别处")
	mustSend(t, f.a, batch{DocumentID: other.ID, Expected: map[string]any{"refs": "1"}, Operations: []any{op{"op": "create_ref", "text": "别处的图"}}})
	leak := readBoard(t, f.a, other.ID).Board.Refs[0]
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "6"}, Operations: []any{op{"op": "plant_leak", "ref": leak}}}) // 7
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "7"}, Operations: []any{op{"op": "set_note", "text": "x"}}})   // 8
	_, err = send(t, f.a, restoreRefs("7", "6"))
	wantErr(t, err, creativeops.ErrValidation)
}

func TestReceiptStaysBounded(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	ids := make([]string, 2000)
	for i := range ids {
		ids[i] = fmt.Sprintf("n%04d", i)
	}
	if rev := addItems(t, f.a, d, ids...); rev != 2 {
		t.Fatalf("add landed in %d", rev)
	}
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}}) // 3
	restore := command(t, batch{DocumentID: d.ID, ExpectedRevision: "3", Operations: []any{op{"op": "restore", "revision": "2"}}})
	res, body, err := run(t, f.a, creativedocument.Photographer, restore)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ResetRequired || len(body) >= 64<<10 {
		t.Fatalf("restore receipt of %d bytes: %+v", len(body), res)
	}
	_, replay, err := run(t, f.a, creativedocument.Photographer, restore)
	if err != nil || !sameJSON(t, replay, body) {
		t.Fatalf("replay %s (%v), first %s", replay, err, body)
	}
	if v := readBoard(t, f.a, d.ID); v.Revision != 4 || len(v.UnitRevs) != 2004 {
		t.Fatalf("after restore: revision %d, %d units", v.Revision, len(v.UnitRevs))
	}

	long := createBoard(t, f.a, "")
	ids = make([]string, 600)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s%04d", strings.Repeat("x", 106), i)
	}
	addItems(t, f.a, long, ids...)
	expected := map[string]any{"items": "2"}
	for _, id := range ids {
		expected["item:"+id] = "2"
	}
	res = mustSend(t, f.a, batch{DocumentID: long.ID, Expected: expected, Operations: []any{op{"op": "remove_items", "ids": ids}}})
	if !res.ResetRequired || len(res.ChangedUnits)+len(res.RemovedUnits) != 0 {
		t.Fatalf("bulk remove receipt %+v", res)
	}
	if v := readBoard(t, f.a, long.ID); len(v.UnitRevs) != 4 {
		t.Fatalf("units after bulk remove %v", len(v.UnitRevs))
	}
}

func TestContentCreatedInsideWrite(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	contents := func() int {
		return count(t, f.db, `SELECT count(*) FROM creative_content_revisions WHERE account_id='doc-a'`)
	}
	before := contents()
	for name, tc := range map[string]struct {
		op   string
		want error
	}{
		"type fails after creating": {"fail_after_create", errBoardFail},
		"created content left out":  {"create_orphan", creativedocument.ErrTypeContract},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"refs": "1"}, Operations: []any{op{"op": tc.op, "text": "草图"}}})
			wantErr(t, err, tc.want)
			if got := contents(); got != before {
				t.Fatalf("content revisions %d, want %d", got, before)
			}
			if n := count(t, f.db, `SELECT count(*) FROM creative_document_content_refs WHERE document_id=$1`, d.ID); n != 0 {
				t.Fatalf("%d reference rows left behind", n)
			}
			if v := readBoard(t, f.a, d.ID); v.Revision != 1 {
				t.Fatalf("revision moved to %d", v.Revision)
			}
		})
	}
	res := mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"refs": "1"}, Operations: []any{op{"op": "create_ref", "text": "草图"}}})
	ref := readBoard(t, f.a, d.ID).Board.Refs[0]
	if got := contents(); got != before+2 {
		t.Fatalf("content revisions %d, want the reference and the snapshot", got)
	}
	if n := count(t, f.db, `SELECT count(*) FROM creative_document_content_refs WHERE document_id=$1 AND revision=$2 AND content_revision_id=$3`, d.ID, int64(res.Revision), ref); n != 1 {
		t.Fatalf("reference rows %d", n)
	}
}

// Documents that existed before the migration only have their current
// revision on record; restoring an older number is a bad request, not a
// missing document.
func TestRestoreWithoutHistory(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	addItems(t, f.a, d, "a")                                                                                                               // 2
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "改"}}}) // 3
	for _, table := range []string{"creative_document_content_refs", "creative_document_revisions"} {
		if _, err := f.db.Exec(`DELETE FROM `+table+` WHERE document_id=$1 AND revision<3`, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err := send(t, f.a, batch{DocumentID: d.ID, ExpectedRevision: "3", Operations: []any{op{"op": "restore", "revision": "1"}}})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "3"}, Operations: []any{op{"op": "restore_units", "from_revision": "1", "units": []string{"note"}}}})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = creativedocument.ReadRevision(t.Context(), f.a, d.ID, 1, nil)
	wantErr(t, err, creativedocument.ErrRevisionNotFound)
	if v := readBoard(t, f.a, d.ID); v.Revision != 3 {
		t.Fatalf("document moved to %d", v.Revision)
	}
}

// Canvas snapshots and document lists summarize many documents at once; their
// references must come in one query however many documents there are.
func TestSummariesReadReferencesOnce(t *testing.T) {
	f := setup(t)
	// pg_stat_xact_user_tables counts this transaction's scans; the view adds
	// the account column every scoped read requires.
	if _, err := f.db.Exec(`CREATE VIEW test_ref_scans AS SELECT 'doc-a'::text AS account_id, (coalesce(seq_scan,0)+coalesce(idx_scan,0))::bigint AS scans
 FROM pg_stat_xact_user_tables WHERE relname='creative_document_content_refs'`); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 20)
	for i := range ids {
		d := createBoard(t, f.a, "")
		ids[i] = d.ID
		if i%7 == 0 {
			mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"refs": "1"}, Operations: []any{op{"op": "create_ref", "text": "参考"}}})
		}
	}
	// Never t.Fatal inside the transaction: the connection would stay checked
	// out and closing the store would wait for it forever.
	scans := func(tx store.ReadTxAccountScope) (int64, error) {
		var v int64
		err := tx.QueryRow(t.Context(), "test_ref_scans", "scans", "").Scan(&v)
		return v, err
	}
	for _, n := range []int{1, 5, 20} {
		var delta int64
		var got map[string]creativedocument.NodeSummary
		err := f.a.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
			before, err := scans(tx)
			if err != nil {
				return err
			}
			if got, err = creativedocument.SummaryBatch(t.Context(), tx, ids[:n]); err != nil {
				return err
			}
			after, err := scans(tx)
			delta = after - before
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if delta != 1 {
			t.Fatalf("%d documents read references in %d queries", n, delta)
		}
		for i, id := range ids[:n] {
			want := `{"items":0,"refs":0}`
			if i%7 == 0 {
				want = `{"items":0,"refs":1}`
			}
			if s := got[id]; !s.Usable || string(s.Summary) != want {
				t.Fatalf("summary of document %d: %+v", i, s)
			}
		}
	}
}

func TestTypeMustReportOnlyCheckedUnits(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	_, err := send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "sneak"}}})
	wantErr(t, err, creativedocument.ErrTypeContract)
	if v := readBoard(t, f.a, d.ID); v.Revision != 1 {
		t.Fatalf("revision moved to %d", v.Revision)
	}
}

func TestHistory(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	note := func(actor creativedocument.Actor, expected, text string) {
		t.Helper()
		if _, _, err := run(t, f.a, actor, command(t, batch{DocumentID: d.ID, Expected: map[string]any{"note": expected}, Operations: []any{op{"op": "set_note", "text": text}}})); err != nil {
			t.Fatal(err)
		}
	}
	note(agent("run-1"), "1", "助手改")               // 2
	note(creativedocument.Photographer, "2", "我改") // 3
	note(agent("run-1"), "3", "助手又改")              // 4
	note(agent("run-2"), "4", "另一轮")               // 5

	all := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{})
	if !reflect.DeepEqual(revisionsOf(all), []int64{5, 4, 3, 2, 1}) || all.NextCursor != nil {
		t.Fatalf("history %v", revisionsOf(all))
	}
	if item := all.Items[3]; item.ActorKind != "agent" || item.RunID == nil || *item.RunID != "run-1" || !reflect.DeepEqual(item.ChangedUnits, []string{"note"}) {
		t.Fatalf("agent revision %+v", item)
	}
	if got := revisionsOf(listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{RunID: "run-1"})); !reflect.DeepEqual(got, []int64{4, 2}) {
		t.Fatalf("run-1 %v", got)
	}
	if got := revisionsOf(listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{SinceRevision: 3})); !reflect.DeepEqual(got, []int64{5, 4}) {
		t.Fatalf("since 3 %v", got)
	}
	var pages [][]int64
	cursor := ""
	for {
		page := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 2, Cursor: cursor})
		pages = append(pages, revisionsOf(page))
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !reflect.DeepEqual(pages, [][]int64{{5, 4}, {3, 2}, {1}}) {
		t.Fatalf("pages %v", pages)
	}
	first := listRevisions(t, f.a, d.ID, creativedocument.RevisionQuery{Limit: 2})
	_, err := creativedocument.ListRevisions(t.Context(), f.a, d.ID, creativedocument.RevisionQuery{Limit: 2, RunID: "run-1", Cursor: *first.NextCursor})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = creativedocument.ListRevisions(t.Context(), f.a, d.ID, creativedocument.RevisionQuery{Limit: 101})
	wantErr(t, err, creativeops.ErrValidation)

	raw, err := creativedocument.ReadRevision(t.Context(), f.a, d.ID, 2, []string{"note"})
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		RunID    *string           `json:"run_id"`
		UnitRevs map[string]string `json:"unit_revs"`
		View     map[string]any    `json:"view"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if old.RunID == nil || *old.RunID != "run-1" || old.UnitRevs["note"] != "2" || !reflect.DeepEqual(old.View, map[string]any{"note": "助手改"}) {
		t.Fatalf("revision 2 %s", raw)
	}
	_, err = creativedocument.ReadRevision(t.Context(), f.a, d.ID, 9, nil)
	wantErr(t, err, creativedocument.ErrRevisionNotFound)
	_, err = creativedocument.ReadRevision(t.Context(), f.a, d.ID, 2, []string{"bad unit"})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = creativedocument.ListRevisions(t.Context(), f.b, d.ID, creativedocument.RevisionQuery{})
	wantErr(t, err, creativedocument.ErrNotFound)
	_, err = creativedocument.ReadRevision(t.Context(), f.b, d.ID, 1, nil)
	wantErr(t, err, creativedocument.ErrNotFound)
}

func target(t *testing.T, id string) creativeops.Command {
	return command(t, map[string]string{"document_id": id})
}

type targetFunc func(context.Context, store.AccountScope, creativeops.Command) (creativeops.Receipt, error)

// trashedAt runs a trash or untrash and returns the document's trashed_at.
func trashedAt(t *testing.T, act targetFunc, scope store.AccountScope, id string) *time.Time {
	t.Helper()
	receipt, err := act(t.Context(), scope, target(t, id))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		TrashedAt *time.Time `json:"trashed_at"`
	}
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return out.TrashedAt
}

func TestTrashAndPurge(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	ctx := t.Context()
	first := trashedAt(t, creativedocument.Trash, f.a, d.ID)
	if first == nil || readBoard(t, f.a, d.ID).TrashedAt == nil {
		t.Fatal("trash did not stick")
	}
	_, err := send(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}})
	wantErr(t, err, creativedocument.ErrTrashed)
	if again := trashedAt(t, creativedocument.Trash, f.a, d.ID); again == nil || !again.Equal(*first) {
		t.Fatalf("trashing twice moved the time: %v %v", first, again)
	}
	if got := trashedAt(t, creativedocument.Untrash, f.a, d.ID); got != nil {
		t.Fatalf("untrash left %v", got)
	}
	mustSend(t, f.a, batch{DocumentID: d.ID, Expected: map[string]any{"note": "1"}, Operations: []any{op{"op": "set_note", "text": "x"}}})
	_, err = creativedocument.Purge(ctx, f.a, target(t, d.ID))
	wantErr(t, err, creativedocument.ErrNotTrashed)
	_, err = creativedocument.Trash(ctx, f.b, target(t, d.ID))
	wantErr(t, err, creativedocument.ErrNotFound)

	trashedAt(t, creativedocument.Trash, f.a, d.ID)
	_, err = creativedocument.Purge(ctx, f.b, target(t, d.ID))
	wantErr(t, err, creativedocument.ErrNotFound)
	purge := target(t, d.ID)
	receipt, err := creativedocument.Purge(ctx, f.a, purge)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"creative_documents", "creative_document_revisions", "creative_document_content_refs", "creative_document_conversations"} {
		column := "document_id"
		if table == "creative_documents" {
			column = "id"
		}
		if n := count(t, f.db, "SELECT count(*) FROM "+table+" WHERE "+column+"=$1", d.ID); n != 0 {
			t.Fatalf("%s keeps %d rows", table, n)
		}
	}
	_, err = creativedocument.Read(ctx, f.a, d.ID)
	wantErr(t, err, creativedocument.ErrNotFound)
	// A retry after a lost response gets the same answer.
	replay, err := creativedocument.Purge(ctx, f.a, purge)
	if err != nil || !sameJSON(t, replay.Outcome.Response, receipt.Outcome.Response) {
		t.Fatalf("purge replay %s (%v)", replay.Outcome.Response, err)
	}
	_, err = creativedocument.Purge(ctx, f.a, target(t, d.ID))
	wantErr(t, err, creativedocument.ErrNotFound)
}

func titles(page creativedocument.ListPage) []string {
	out := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		out = append(out, item.Title)
	}
	return out
}

func TestList(t *testing.T) {
	f := setup(t)
	createBoard(t, f.a, "灯光 50%")
	createBoard(t, f.a, "灯光_A")
	portrait := createBoard(t, f.a, "人像")
	list := func(q creativedocument.ListQuery) creativedocument.ListPage {
		t.Helper()
		page, err := creativedocument.List(t.Context(), f.a, q)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := list(creativedocument.ListQuery{Limit: 2})
	if !reflect.DeepEqual(titles(first), []string{"人像", "灯光_A"}) || first.NextCursor == nil || string(first.Items[0].Summary) != `{"items":0,"refs":0}` {
		t.Fatalf("first page %+v", first)
	}
	if next := list(creativedocument.ListQuery{Limit: 2, Cursor: *first.NextCursor}); !reflect.DeepEqual(titles(next), []string{"灯光 50%"}) || next.NextCursor != nil {
		t.Fatalf("second page %+v", next)
	}
	_, err := creativedocument.List(t.Context(), f.a, creativedocument.ListQuery{Limit: 2, Q: "灯光", Cursor: *first.NextCursor})
	wantErr(t, err, creativeops.ErrValidation)
	_, err = creativedocument.List(t.Context(), f.a, creativedocument.ListQuery{Limit: 51})
	wantErr(t, err, creativeops.ErrValidation)
	for q, want := range map[string][]string{"50%": {"灯光 50%"}, "_": {"灯光_A"}, "灯光": {"灯光_A", "灯光 50%"}} {
		if got := titles(list(creativedocument.ListQuery{Q: q})); !reflect.DeepEqual(got, want) {
			t.Fatalf("q %q: %v", q, got)
		}
	}
	if got := list(creativedocument.ListQuery{Type: creativedocument.InternalTypeKey}); len(got.Items) != 0 {
		t.Fatalf("internal documents %v", titles(got))
	}
	trashedAt(t, creativedocument.Trash, f.a, portrait.ID)
	if got := titles(list(creativedocument.ListQuery{Type: "test.board"})); !reflect.DeepEqual(got, []string{"灯光_A", "灯光 50%"}) {
		t.Fatalf("live documents %v", got)
	}
	if got := list(creativedocument.ListQuery{Trashed: true}); !reflect.DeepEqual(titles(got), []string{"人像"}) || got.Items[0].TrashedAt == nil {
		t.Fatalf("trash %+v", got)
	}
	other, err := creativedocument.List(t.Context(), f.b, creativedocument.ListQuery{})
	if err != nil || len(other.Items) != 0 {
		t.Fatalf("another account sees %v (%v)", titles(other), err)
	}
}

func TestConversation(t *testing.T) {
	f := setup(t)
	d := createBoard(t, f.a, "")
	conv1, conv2 := "ccco_"+uuid.NewString(), "ccco_"+uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_projects(id,account_id,name) VALUES ('doc-pa','doc-a','项目'),('doc-pb','doc-b','项目');
 INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES ('doc-ca','doc-a','doc-pa','画布'),('doc-cb','doc-b','doc-pb','画布')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title) VALUES ($1,'doc-a','doc-pa','doc-ca','对话'),($2,'doc-a','doc-pa','doc-ca','对话')`, conv1, conv2); err != nil {
		t.Fatal(err)
	}
	read := func(scope store.AccountScope, canvas string) (*string, error) {
		return creativedocument.ReadConversation(t.Context(), scope, d.ID, canvas)
	}
	set := func(conv string) {
		t.Helper()
		if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			return creativedocument.SetConversation(t.Context(), tx, d.ID, "doc-ca", conv)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := read(f.a, "doc-ca"); err != nil || got != nil {
		t.Fatalf("before any conversation: %v %v", got, err)
	}
	set(conv1)
	set(conv2)
	if got, err := read(f.a, "doc-ca"); err != nil || got == nil || *got != conv2 {
		t.Fatalf("conversation %v %v", got, err)
	}
	_, err := read(f.a, "doc-cb")
	wantErr(t, err, creativedocument.ErrNotFound)
	_, err = read(f.a, "")
	wantErr(t, err, creativeops.ErrValidation)
	_, err = read(f.b, "doc-cb")
	wantErr(t, err, creativedocument.ErrNotFound)
	if _, err := f.db.Exec(`DELETE FROM creative_agent_conversations WHERE id=$1`, conv2); err != nil {
		t.Fatal(err)
	}
	if got, err := read(f.a, "doc-ca"); err != nil || got != nil {
		t.Fatalf("pointer to a deleted conversation: %v %v", got, err)
	}

	trashedAt(t, creativedocument.Trash, f.a, d.ID)
	if _, err := creativedocument.Purge(t.Context(), f.a, target(t, d.ID)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.db, `SELECT count(*) FROM creative_document_conversations WHERE document_id=$1`, d.ID); n != 0 {
		t.Fatalf("purge kept %d conversation pointers", n)
	}
}
