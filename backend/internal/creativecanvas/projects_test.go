package creativecanvas_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func namedProject(t *testing.T, a store.AccountScope, name string) creativecanvas.ProjectResult {
	t.Helper()
	r, err := creativecanvas.CreateProject(t.Context(), a, command(t, creativecanvas.CreateProjectInput{Name: name}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func listProjects(t *testing.T, a store.AccountScope, q creativecanvas.ProjectQuery) creativecanvas.ProjectPage {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 30
	}
	page, err := creativecanvas.ListProjects(t.Context(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}
func names(page creativecanvas.ProjectPage) []string {
	out := []string{}
	for _, p := range page.Items {
		out = append(out, p.Name)
	}
	return out
}
func sameList(a, b []string) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// insertRun stands in for a run of any state; status only reads state and canvas.
func insertRun(t *testing.T, db *sql.DB, account, canvas, state string) string {
	t.Helper()
	id := "ccrn_" + uuid.NewString()
	finished := "NULL"
	if state == "succeeded" {
		finished = "clock_timestamp()"
	}
	if _, err := db.Exec(`INSERT INTO creative_agent_runs(id,account_id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at,finished_at)
VALUES($1,$2,'conv',$3,$4,'consent','m','{}',$5,'claim','1','{}',clock_timestamp()+interval '1 hour',`+finished+`)`, id, account, canvas, uuid.NewString(), state); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProjectListSearchSortAndCursor(t *testing.T) {
	_, a, b := setup(t)
	namedProject(t, a, "Beta 外景")
	namedProject(t, a, "alpha 婚纱")
	namedProject(t, a, "ＧＡＭＭＡ 棚拍")
	namedProject(t, a, "100% 写真")
	namedProject(t, b, "alpha 别人的")

	byName := listProjects(t, a, creativecanvas.ProjectQuery{Sort: creativecanvas.SortName})
	if want := []string{"100% 写真", "alpha 婚纱", "Beta 外景", "ＧＡＭＭＡ 棚拍"}; !sameList(names(byName), want) {
		t.Fatalf("name order %v", names(byName))
	}
	// Full-width and case are folded on both sides; % is a literal.
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{Q: "gamma"})); !sameList(got, []string{"ＧＡＭＭＡ 棚拍"}) {
		t.Fatalf("search gamma %v", got)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{Q: "０%"})); !sameList(got, []string{"100% 写真"}) {
		t.Fatalf("search %% %v", got)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{Q: "%"})); !sameList(got, []string{"100% 写真"}) {
		t.Fatalf("percent is not a wildcard %v", got)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{Q: "ALPHA"})); !sameList(got, []string{"alpha 婚纱"}) {
		t.Fatalf("other account leaked %v", got)
	}

	// Paging by one across the name order visits every project once.
	seen := []string{}
	q := creativecanvas.ProjectQuery{Sort: creativecanvas.SortName, Limit: 1}
	for i := 0; i < 5; i++ {
		page := listProjects(t, a, q)
		seen = append(seen, names(page)...)
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if !sameList(seen, names(byName)) {
		t.Fatalf("paged %v", seen)
	}
	first := listProjects(t, a, creativecanvas.ProjectQuery{Sort: creativecanvas.SortName, Limit: 1})
	for _, changed := range []creativecanvas.ProjectQuery{
		{Sort: creativecanvas.SortEditedDesc, Limit: 1, Cursor: first.NextCursor},
		{Sort: creativecanvas.SortName, Q: "a", Limit: 1, Cursor: first.NextCursor},
	} {
		if _, err := creativecanvas.ListProjects(t.Context(), a, changed); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("cursor reused across queries: %v", err)
		}
	}
	if _, err := creativecanvas.ListProjects(t.Context(), b, creativecanvas.ProjectQuery{Sort: creativecanvas.SortName, Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("cursor crossed accounts: %v", err)
	}
	if _, err := creativecanvas.ListProjects(t.Context(), a, creativecanvas.ProjectQuery{Sort: "random", Limit: 1}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("unknown sort: %v", err)
	}
}

// Recent means recently edited: a canvas commit or a rename moves a project
// up, archiving and restoring do not.
func TestProjectEditedAtFollowsCanvasAndRename(t *testing.T) {
	db, a, _ := setup(t)
	older := namedProject(t, a, "先建的")
	newer := namedProject(t, a, "后建的")
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{})); !sameList(got, []string{"后建的", "先建的"}) {
		t.Fatalf("initial %v", got)
	}
	add(t, a, older, asset(t, a))
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{})); !sameList(got, []string{"先建的", "后建的"}) {
		t.Fatalf("canvas edit did not count %v", got)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{Sort: creativecanvas.SortEditedAsc})); !sameList(got, []string{"后建的", "先建的"}) {
		t.Fatalf("oldest first %v", got)
	}
	if _, err := creativecanvas.ArchiveProject(t.Context(), a, command(t, creativecanvas.ProjectStateInput{ProjectID: newer.ProjectID, ExpectedRevision: 1})); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.RestoreProject(t.Context(), a, command(t, creativecanvas.ProjectStateInput{ProjectID: newer.ProjectID, ExpectedRevision: 2})); err != nil {
		t.Fatal(err)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{})); !sameList(got, []string{"先建的", "后建的"}) {
		t.Fatalf("archive and restore counted as edits %v", got)
	}
	if _, err := creativecanvas.RenameProject(t.Context(), a, command(t, creativecanvas.RenameProjectInput{ProjectID: newer.ProjectID, ExpectedRevision: 3, Name: "改过名"})); err != nil {
		t.Fatal(err)
	}
	if got := names(listProjects(t, a, creativecanvas.ProjectQuery{})); !sameList(got, []string{"改过名", "先建的"}) {
		t.Fatalf("rename did not count %v", got)
	}
	var normalized string
	if err := db.QueryRow(`SELECT normalized_name FROM creative_projects WHERE id=$1`, newer.ProjectID).Scan(&normalized); err != nil || normalized != "改过名" {
		t.Fatalf("normalized %q %v", normalized, err)
	}
}

func TestProjectCoverIsABoundedSketch(t *testing.T) {
	db, a, b := setup(t)
	empty := namedProject(t, a, "空项目")
	small := namedProject(t, a, "小画布")
	large := namedProject(t, a, "大画布")
	node, _ := add(t, a, small, asset(t, a))
	other := "cwnode_" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y) VALUES($1,'canvas-a',$2,'core.image',400,0)`, other, small.CanvasID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_edges(id,account_id,canvas_id,source_node_id,target_node_id,source_port,target_port,role) VALUES($1,'canvas-a',$2,$3,$4,'out','in','reference')`, "cwedge_"+uuid.NewString(), small.CanvasID, node.NodeID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y)
SELECT 'cwnode_'||gen_random_uuid(),'canvas-a',$1,'core.image',g*10,g*5 FROM generate_series(1,3000) g`, large.CanvasID); err != nil {
		t.Fatal(err)
	}
	// Another account's work never shows in this account's covers.
	foreign := namedProject(t, b, "别人的")
	if _, err := db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y) VALUES($1,'canvas-b',$2,'core.text',0,0)`, "cwnode_"+uuid.NewString(), foreign.CanvasID); err != nil {
		t.Fatal(err)
	}

	covers := map[string]creativecanvas.ProjectCover{}
	for _, p := range listProjects(t, a, creativecanvas.ProjectQuery{}).Items {
		covers[p.Name] = p.Cover
	}
	if c := covers["空项目"]; len(c.Nodes) != 0 || len(c.Edges) != 0 || c.More {
		t.Fatalf("empty %+v", c)
	}
	kinds := map[string]string{}
	for _, n := range covers["小画布"].Nodes {
		kinds[n.Kind] = n.ContentRevisionID
	}
	// Text never carries a revision (the cover has no text); an image without
	// content is drawn as an empty frame.
	if c := covers["小画布"]; len(c.Nodes) != 2 || len(kinds) != 2 || kinds["text"] != "" || len(c.Edges) != 1 || c.More {
		t.Fatalf("small %+v", c)
	}
	if _, ok := kinds["empty"]; !ok {
		t.Fatalf("small kinds %v", kinds)
	}
	c := covers["大画布"]
	if len(c.Nodes) != 60 || !c.More || c.Nodes[0].Kind != "empty" {
		t.Fatalf("large nodes=%d more=%v kind=%s", len(c.Nodes), c.More, c.Nodes[0].Kind)
	}
	_ = empty

	// The cover read stops at the sample: the plan reads at most 61 index rows
	// however large the canvas is (V6-063 AC-03).
	var plan string
	if err := db.QueryRow(`EXPLAIN (ANALYZE, FORMAT JSON) SELECT id FROM creative_nodes WHERE account_id='canvas-a' AND canvas_id=$1 ORDER BY id LIMIT 61`, large.CanvasID).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var parsed []struct {
		Plan struct {
			Plans []struct {
				NodeType   string  `json:"Node Type"`
				ActualRows float64 `json:"Actual Rows"`
			} `json:"Plans"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil || len(parsed) != 1 || len(parsed[0].Plan.Plans) != 1 {
		t.Fatalf("plan %s %v", plan, err)
	}
	if scan := parsed[0].Plan.Plans[0]; scan.ActualRows > 61 {
		t.Fatalf("cover read grows with the canvas: %+v", scan)
	}
}

func TestProjectActivityAndArchiveGuard(t *testing.T) {
	db, a, b := setup(t)
	busy := namedProject(t, a, "在跑")
	waiting := namedProject(t, a, "等你")
	idle := namedProject(t, a, "空闲")
	foreign := namedProject(t, b, "别人在跑")
	running := insertRun(t, db, "canvas-a", busy.CanvasID, "running")
	insertRun(t, db, "canvas-a", waiting.CanvasID, "waiting_input")
	insertRun(t, db, "canvas-a", idle.CanvasID, "succeeded")
	insertRun(t, db, "canvas-b", foreign.CanvasID, "running")

	page := listProjects(t, a, creativecanvas.ProjectQuery{})
	state := map[string][2]bool{}
	for _, p := range page.Items {
		state[p.Name] = [2]bool{p.Running, p.Waiting}
	}
	if state["在跑"] != [2]bool{true, false} || state["等你"] != [2]bool{false, true} || state["空闲"] != [2]bool{false, false} {
		t.Fatalf("activity %v", state)
	}
	if page.ActivityCounts != (creativecanvas.ActivityCounts{Running: 1, Waiting: 1}) {
		t.Fatalf("counts %+v", page.ActivityCounts)
	}

	archive := func(p creativecanvas.ProjectResult, revision creativeops.Revision) error {
		_, err := creativecanvas.ArchiveProject(t.Context(), a, command(t, creativecanvas.ProjectStateInput{ProjectID: p.ProjectID, ExpectedRevision: revision}))
		return err
	}
	if err := archive(busy, 1); !errors.Is(err, creativecanvas.ErrProjectBusy) {
		t.Fatalf("archived with a running run: %v", err)
	}
	// Waiting alone may be archived; it then drops out of the counts.
	if err := archive(waiting, 1); err != nil {
		t.Fatal(err)
	}
	if counts := listProjects(t, a, creativecanvas.ProjectQuery{}).ActivityCounts; counts != (creativecanvas.ActivityCounts{Running: 1}) {
		t.Fatalf("archived project counted %+v", counts)
	}
	// A running generation blocks archiving just like a run.
	if _, err := db.Exec(`UPDATE creative_agent_runs SET state='succeeded',finished_at=clock_timestamp() WHERE id=$1`, running); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_node_executions(id,account_id,canvas_id,node_id,action_key,executor_version,operation_id,request_hash,state,deadline,target_data_revision,draft_revision,prompt_snapshot,read_set,caller_kind,publish_operation_id,result_operation_id)
VALUES($1,'canvas-a',$2,'n','gen','1',$3,'h','queued',clock_timestamp()+interval '1 hour',1,1,'{}','{}','photographer',$4,$5)`, "cwex_"+uuid.NewString(), busy.CanvasID, uuid.NewString(), uuid.NewString(), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := archive(busy, 1); !errors.Is(err, creativecanvas.ErrProjectBusy) {
		t.Fatalf("archived with a queued generation: %v", err)
	}
	if _, err := db.Exec(`UPDATE creative_node_executions SET state='succeeded' WHERE canvas_id=$1`, busy.CanvasID); err != nil {
		t.Fatal(err)
	}
	if err := archive(busy, 1); err != nil {
		t.Fatal(err)
	}

	// The entry guard every run activation takes.
	err := a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return creativecanvas.LockProjectForWorkInTx(t.Context(), tx, busy.CanvasID)
	})
	if !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatalf("archived project admitted work: %v", err)
	}
	err = a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return creativecanvas.LockProjectForWorkInTx(t.Context(), tx, idle.CanvasID)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Children store positions relative to their group. The cover returns world
// coordinates, also when the groups above a node fell outside the sample.
func TestProjectCoverUsesWorldCoordinates(t *testing.T) {
	db, a, _ := setup(t)
	insert := func(canvas, id, typeKey string, x, y float64, parent any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,parent_id) VALUES($1,'canvas-a',$2,$3,$4,$5,$6)`, id, canvas, typeKey, x, y, parent); err != nil {
			t.Fatal(err)
		}
	}
	nested := namedProject(t, a, "嵌套")
	insert(nested.CanvasID, "cwnode_a-outer", "core.group", 100, 50, nil)
	insert(nested.CanvasID, "cwnode_b-inner", "core.group", 20, 10, "cwnode_a-outer")
	insert(nested.CanvasID, "cwnode_c-leaf", "core.text", 5, 5, "cwnode_b-inner")

	// The leaf sorts first; 60 fillers push both groups out of the sample.
	unsampled := namedProject(t, a, "父组不在样本里")
	insert(unsampled.CanvasID, "cwnode_0-leaf", "core.text", 5, 5, "cwnode_z-inner")
	for i := 0; i < 60; i++ {
		insert(unsampled.CanvasID, fmt.Sprintf("cwnode_1-%02d", i), "core.text", 0, 0, nil)
	}
	insert(unsampled.CanvasID, "cwnode_z-outer", "core.group", 100, 50, nil)
	insert(unsampled.CanvasID, "cwnode_z-inner", "core.group", 20, 10, "cwnode_z-outer")

	at := map[string][2]float64{}
	for _, p := range listProjects(t, a, creativecanvas.ProjectQuery{}).Items {
		for _, n := range p.Cover.Nodes {
			at[n.ID] = [2]float64{n.X, n.Y}
		}
	}
	for id, want := range map[string][2]float64{
		"cwnode_a-outer": {100, 50}, "cwnode_b-inner": {120, 60}, "cwnode_c-leaf": {125, 65},
		"cwnode_0-leaf": {125, 65},
	} {
		if at[id] != want {
			t.Fatalf("%s at %v, want %v", id, at[id], want)
		}
	}
	if _, ok := at["cwnode_z-outer"]; ok {
		t.Fatal("groups outside the sample were drawn")
	}
}

// Every cursor the server hands out must be accepted back, even for the
// longest name NFKC can produce (U+FDFA expands to 33 bytes).
func TestProjectCursorFromLongestNameIsAccepted(t *testing.T) {
	_, a, _ := setup(t)
	long := strings.Repeat("ﷺ", 200)
	namedProject(t, a, long)
	namedProject(t, a, "㍿"+strings.Repeat("㍿", 199))
	namedProject(t, a, "短")
	for _, sort := range []string{creativecanvas.SortName, creativecanvas.SortEditedDesc, creativecanvas.SortEditedAsc} {
		seen := 0
		q := creativecanvas.ProjectQuery{Sort: sort, Limit: 1}
		for i := 0; i < 4; i++ {
			page, err := creativecanvas.ListProjects(t.Context(), a, q)
			if err != nil {
				t.Fatalf("%s page %d: %v (cursor %d bytes)", sort, i, err, len(q.Cursor))
			}
			seen += len(page.Items)
			if page.NextCursor == "" {
				break
			}
			q.Cursor = page.NextCursor
		}
		if seen != 3 {
			t.Fatalf("%s saw %d", sort, seen)
		}
	}
}

// Listing reads a snapshot and never takes the account's creative write lock,
// so it cannot hold up canvas writes or run entries.
func TestProjectListDoesNotTakeTheWriteLock(t *testing.T) {
	_, a, _ := setup(t)
	namedProject(t, a, "项目")
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			if err := tx.LockCreativeWrite(t.Context()); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	// Released on every exit, so a failing assertion cannot leave the lock
	// holder (and the test database) waiting forever.
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	listed := make(chan error, 1)
	go func() {
		_, err := creativecanvas.ListProjects(t.Context(), a, creativecanvas.ProjectQuery{Limit: 30})
		listed <- err
	}()
	select {
	case err := <-listed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("project list waited for the account write lock")
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
