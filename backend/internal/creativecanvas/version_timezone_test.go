package creativecanvas_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	beijing   = time.FixedZone("CST", 8*3600)
	elsewhere = time.FixedZone("PDT", -7*3600)
)

// inZone runs the process in a time zone; pgx renders timestamps in time.Local.
func inZone(t *testing.T, zone *time.Location) {
	t.Helper()
	previous := time.Local
	time.Local = zone
	t.Cleanup(func() { time.Local = previous })
}

// executedVersion leaves one generated version on the canvas and returns the
// change that published it.
func executedVersion(t *testing.T) (*sql.DB, store.AccountScope, creativecanvas.ProjectResult, string) {
	t.Helper()
	db, scope, runtime, svc, p, ids := executionFixture(t)
	e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
	target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
	raw, _ := json.Marshal(target)
	if err := svc.Work(t.Context(), scope, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	finished, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
	if err != nil || finished.ChangeID == nil {
		t.Fatal("execution not applied", finished, err)
	}
	return db, scope, p, *finished.ChangeID
}

func addNode(t *testing.T, scope store.AccountScope, canvas string) error {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	input := creativecanvas.BatchInput{CanvasID: canvas, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{{Type: "add_node", NodeID: "cwnode_" + uuid.NewString(), TypeKey: "core.text"}}}
	_, err = creativecanvas.ApplyCommands(t.Context(), scope, command(t, input))
	return err
}

func undo(t *testing.T, scope store.AccountScope, canvas, change string) error {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, canvas)
	if err != nil {
		t.Fatal(err)
	}
	_, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: canvas, ChangeID: change, ReadSet: creativecanvas.ReadSetFor(c)}))
	return err
}

// A version created by a process in one time zone stays editable, and its
// publication undoable, from a process in another.
func TestVersionHeadsIgnoreProcessTimeZone(t *testing.T) {
	inZone(t, beijing)
	_, scope, p, change := executedVersion(t)
	time.Local = elsewhere
	if err := addNode(t, scope, p.CanvasID); err != nil {
		t.Fatal("edit refused across time zones", err)
	}
	if err := undo(t, scope, p.CanvasID, change); err != nil {
		t.Fatal("undo refused across time zones", err)
	}
}

// Heads written before versions were hashed in UTC carry the writer's offset,
// both on the version and in the change log. They are still trusted when they
// match their own hash; a head that does not is still corruption.
func TestLegacyVersionHeadsStayEditable(t *testing.T) {
	inZone(t, elsewhere)
	db, scope, p, change := executedVersion(t)
	rewriteVersionHeads(t, db, func(h creativegraph.Head) creativegraph.Head { return beijingHead(t, h) })
	if err := addNode(t, scope, p.CanvasID); err != nil {
		t.Fatal("legacy head refused", err)
	}
	if err := undo(t, scope, p.CanvasID, change); err != nil {
		t.Fatal("legacy change log refused", err)
	}

	db, scope, p, _ = executedVersion(t)
	rewriteVersionHeads(t, db, func(h creativegraph.Head) creativegraph.Head {
		h.Value = beijingHead(t, h).Value
		return h
	})
	if err := addNode(t, scope, p.CanvasID); !errors.Is(err, creativecanvas.ErrGraphIntegrity) {
		t.Fatal("a head that disagrees with its hash was accepted", err)
	}
}

// beijingHead renders a version head the way a +08:00 process used to.
func beijingHead(t *testing.T, h creativegraph.Head) creativegraph.Head {
	t.Helper()
	if string(h.Value) == "null" {
		return h
	}
	var v map[string]any
	decoder := json.NewDecoder(bytes.NewReader(h.Value))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, v["created_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	v["created_at"] = at.In(beijing).Format(time.RFC3339Nano)
	value := creativegraph.Canonical(v)
	return creativegraph.Head{Token: h.Token, Hash: creativegraph.Hash(value), Value: value}
}

// rewriteVersionHeads rewrites every version data head, on identities and in
// the change log alike.
func rewriteVersionHeads(t *testing.T, db *sql.DB, rewrite func(creativegraph.Head) creativegraph.Head) {
	t.Helper()
	rows, err := db.Query(`SELECT id, effect_heads FROM creative_graph_identities WHERE kind='version'`)
	if err != nil {
		t.Fatal(err)
	}
	updates := map[string][]byte{}
	for rows.Next() {
		var id string
		var raw []byte
		var heads map[string]creativegraph.Head
		if err = rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &heads); err != nil {
			t.Fatal(err)
		}
		heads["data"] = rewrite(heads["data"])
		updates[id], _ = json.Marshal(heads)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if len(updates) == 0 {
		t.Fatal("no version heads")
	}
	for id, raw := range updates {
		if _, err = db.Exec(`UPDATE creative_graph_identities SET effect_heads=$1 WHERE id=$2`, raw, id); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = db.Query(`SELECT id, before_after FROM creative_changes`)
	if err != nil {
		t.Fatal(err)
	}
	updates = map[string][]byte{}
	for rows.Next() {
		var id string
		var raw []byte
		var detail struct {
			SchemaVersion int `json:"schema_version"`
			Fields        []struct {
				ID     string             `json:"id"`
				Kind   string             `json:"kind"`
				Field  string             `json:"field"`
				Before creativegraph.Head `json:"before"`
				After  creativegraph.Head `json:"after"`
			} `json:"fields"`
		}
		if err = rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &detail); err != nil {
			t.Fatal(err)
		}
		for i, f := range detail.Fields {
			if f.Kind == "version" && f.Field == "data" {
				detail.Fields[i].Before = rewrite(f.Before)
				detail.Fields[i].After = rewrite(f.After)
			}
		}
		updates[id], _ = json.Marshal(detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for id, raw := range updates {
		if _, err = db.Exec(`UPDATE creative_changes SET before_after=$1 WHERE id=$2`, raw, id); err != nil {
			t.Fatal(err)
		}
	}
}
