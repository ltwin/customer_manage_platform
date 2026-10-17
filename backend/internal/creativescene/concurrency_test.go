package creativescene_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// race starts every batch at once behind a barrier.
func race(t *testing.T, scope store.AccountScope, batches ...batch) []error {
	t.Helper()
	errs := make([]error, len(batches))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, b := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = send(t, scope, b)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

func TestPlacementAndContentDoNotConflict(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a := objectID(1)
	addObjects(t, f.a, doc, box(a, nil, 1))
	for round := range 20 {
		v := readScene(t, f.a, doc)
		move := batch{DocumentID: doc, Expected: v.expect(placement(a)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "transform": at(float64(round), 0, 0)}}}}}
		resize := batch{DocumentID: doc, Expected: v.expect(content(a)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "primitive": map[string]any{"params": map[string]any{"width": float64(round + 1)}}}}}}}
		for i, err := range race(t, f.a, move, resize) {
			if err != nil {
				t.Fatalf("round %d writer %d: %v", round, i, err)
			}
		}
		o := readScene(t, f.a, doc).object(t, a)
		if o.Transform.Position[0] != float64(round) || o.Primitive.Params["width"] != float64(round+1) {
			t.Fatalf("round %d lost a write: %+v", round, o)
		}
	}
}

func TestSameUnitExactlyOneWins(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	a := objectID(1)
	addObjects(t, f.a, doc, box(a, nil, 1))
	for round := range 20 {
		v := readScene(t, f.a, doc)
		moveTo := func(x float64) batch {
			return batch{DocumentID: doc, Expected: v.expect(placement(a)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": a, "transform": at(x, 0, 0)}}}}}
		}
		var won, lost int
		for _, err := range race(t, f.a, moveTo(1), moveTo(2)) {
			c, ok := creativedocument.IsPublicConflict(err)
			switch {
			case err == nil:
				won++
			case ok && errors.Is(err, creativedocument.ErrConflict) && len(c.Conflicts) == 1 && c.Conflicts[0].Unit == placement(a):
				lost++
			default:
				t.Fatalf("round %d: unexpected %v", round, err)
			}
		}
		if won != 1 || lost != 1 {
			t.Fatalf("round %d: %d won, %d lost", round, won, lost)
		}
	}
}

// TestBaseline measures AC-11 on 300 objects: command latency, bytes each
// revision adds, and a canvas read with ten scene nodes. It only logs; the
// numbers go into the verification record.
func TestBaseline(t *testing.T) {
	if os.Getenv("CREATIVE_SCENE_BASELINE") != "1" {
		t.Skip("set CREATIVE_SCENE_BASELINE=1 to measure the 300-object baseline")
	}
	f := setup(t)
	fill := func(doc string) []string {
		objects := make([]map[string]any, 300)
		ids := make([]string, 300)
		for i := range objects {
			ids[i] = objectID(i + 1)
			objects[i] = box(ids[i], nil, i+1)
			objects[i]["name"] = fmt.Sprintf("物件 %d", i+1)
		}
		addObjects(t, f.a, doc, objects...)
		return ids
	}
	doc := createScene(t, f.a)
	ids := fill(doc)
	var size int
	if err := f.db.QueryRow(`SELECT length(r.payload::text) FROM creative_documents d JOIN creative_content_revisions r ON r.id=d.content_revision_id WHERE d.id=$1`, doc).Scan(&size); err != nil {
		t.Fatal(err)
	}
	bytesBefore := count(t, f, `SELECT coalesce(sum(pg_column_size(payload)),0) FROM creative_content_revisions`)
	const runs = 200
	durations := make([]time.Duration, 0, runs)
	for i := range runs {
		v := readScene(t, f.a, doc)
		id := ids[i%len(ids)]
		b := batch{DocumentID: doc, Expected: v.expect(placement(id)), Operations: []any{op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "transform": at(float64(i), 0, 0)}}}}}
		started := time.Now()
		mustSend(t, f.a, b)
		durations = append(durations, time.Since(started))
	}
	bytesAfter := count(t, f, `SELECT coalesce(sum(pg_column_size(payload)),0) FROM creative_content_revisions`)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("300-object snapshot %d bytes; command p50 %v p95 %v; %d bytes stored per revision",
		size, durations[runs/2], durations[runs*95/100], (bytesAfter-bytesBefore)/runs)

	r, err := creativecanvas.CreateProject(t.Context(), f.a, command(t, creativecanvas.CreateProjectInput{Name: "基线"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		canvas, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		node := "cwnode_" + uuid.NewString()
		add := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas),
			Actions: []creativecanvas.Action{{Type: "add_node", NodeID: node, TypeKey: creativescene.NodeTypeKey}}}
		if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, add)); err != nil {
			t.Fatal(err)
		}
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range canvas.Nodes {
		raw, _ := json.Marshal(n)
		var v struct {
			Data struct {
				DocumentID string `json:"document_id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &v)
		fill(v.Data.DocumentID)
	}
	reads := make([]time.Duration, 0, 20)
	for range 20 {
		started := time.Now()
		if _, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID); err != nil {
			t.Fatal(err)
		}
		reads = append(reads, time.Since(started))
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
	t.Logf("canvas with 10 scenes of 300 objects: read p50 %v max %v", reads[len(reads)/2], reads[len(reads)-1])
}

func count(t *testing.T, f fixture, query string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
