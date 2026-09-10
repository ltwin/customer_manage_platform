package creativecanvas_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }
func setup(t *testing.T) (*sql.DB, store.AccountScope, store.AccountScope) {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('canvas-a','test','active'),('canvas-b','test','active'); INSERT INTO creative_account_capabilities(account_id,read_enabled,manual_write_enabled) VALUES ('canvas-a',true,true),('canvas-b',true,true)`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return db, st.ScopeFor(auth.AccountContext{AccountID: "canvas-a"}), st.ScopeFor(auth.AccountContext{AccountID: "canvas-b"})
}
func command(t *testing.T, input any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}
func draft(body string) creativecontent.Draft {
	return creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}, Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}
}
func project(t *testing.T, a store.AccountScope) creativecanvas.ProjectResult {
	t.Helper()
	r, err := creativecanvas.CreateProject(t.Context(), a, command(t, creativecanvas.CreateProjectInput{Name: "创作项目"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func asset(t *testing.T, a store.AccountScope) creativelibrary.AssetResult {
	t.Helper()
	r, err := creativelibrary.CreateAsset(t.Context(), a, command(t, creativelibrary.CreateAssetInput{Title: "原始灵感", Content: draft("共同参考")}))
	if err != nil {
		t.Fatal(err)
	}
	var v creativelibrary.AssetResult
	if err := json.Unmarshal(r.Outcome.Response, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func add(t *testing.T, a store.AccountScope, p creativecanvas.ProjectResult, v creativelibrary.AssetResult) (creativecanvas.NodeResult, creativeops.Command) {
	t.Helper()
	input := creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: "cwnode_" + uuid.NewString(), TypeKey: "core.text", ExpectedTopologyRevision: 1, Asset: &creativelibrary.AssetReference{ID: v.ID, Revision: v.Revision, ContentRevisionID: v.ContentRevisionID}, X: 20, Y: 30}
	c := command(t, input)
	r, err := creativecanvas.AddNode(t.Context(), a, c)
	if err != nil {
		t.Fatal(err)
	}
	var n creativecanvas.NodeResult
	if err := json.Unmarshal(r.Outcome.Response, &n); err != nil {
		t.Fatal(err)
	}
	return n, c
}

func TestAssetTwoCanvasesEditMoveReopenAndReplay(t *testing.T) {
	_, a, b := setup(t)
	v := asset(t, a)
	p, q := project(t, a), project(t, a)
	n, retry := add(t, a, p, v)
	_, _ = add(t, a, q, v)
	if _, err := creativecanvas.AddNode(t.Context(), a, retry); err != nil {
		t.Fatal(err)
	}
	body := "第一处修改"
	c := command(t, creativecanvas.ReplaceContentInput{CanvasID: p.CanvasID, NodeID: n.NodeID, ExpectedDataRevision: n.DataRevision, ExpectedContentRevisionID: &v.ContentRevisionID, Payload: creativecontent.Payload{Body: &body}})
	if _, err := creativecanvas.ReplaceContent(t.Context(), a, c); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.MoveNode(t.Context(), a, command(t, creativecanvas.MoveNodeInput{CanvasID: p.CanvasID, NodeID: n.NodeID, ExpectedPlacementRevision: n.PlacementRevision, X: 140, Y: 180})); err != nil {
		t.Fatal(err)
	}
	left, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	right, err := creativecanvas.GetCanvas(t.Context(), a, q.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := creativelibrary.GetAsset(t.Context(), a, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Nodes) != 1 || len(right.Nodes) != 1 || *left.Nodes[0].Content.Payload.Body != body || *right.Nodes[0].Content.Payload.Body != "共同参考" || *original.Content.Payload.Body != "共同参考" || left.Nodes[0].X != 140 || left.Nodes[0].DataRevision != 2 {
		t.Fatalf("bad isolation/reopen %+v %+v %+v", left, right, original)
	}
	if _, err := creativecanvas.GetCanvas(t.Context(), b, p.CanvasID); !errors.Is(err, creativecanvas.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := creativelibrary.GetAsset(t.Context(), b, v.ID); !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatal(err)
	}
	// The same operation cannot be retargeted to a different canvas.
	var altered creativecanvas.AddNodeInput
	_ = json.Unmarshal(retry.Payload, &altered)
	altered.CanvasID = q.CanvasID
	retry.Payload, _ = json.Marshal(altered)
	if _, err := creativecanvas.AddNode(t.Context(), a, retry); !errors.Is(err, creativeops.ErrConflict) {
		t.Fatal(err)
	}
}

func TestArchiveRestoreAndConcurrentVersionConflict(t *testing.T) {
	_, a, _ := setup(t)
	v := asset(t, a)
	p := project(t, a)
	n, retry := add(t, a, p, v)
	archive := command(t, creativecanvas.ProjectStateInput{ProjectID: p.ProjectID, ExpectedRevision: 1})
	if _, err := creativecanvas.ArchiveProject(t.Context(), a, archive); err != nil {
		t.Fatal(err)
	}
	move := creativecanvas.MoveNodeInput{CanvasID: p.CanvasID, NodeID: n.NodeID, ExpectedPlacementRevision: 1, X: 100, Y: 100}
	if _, err := creativecanvas.MoveNode(t.Context(), a, command(t, move)); !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatal(err)
	}
	if _, err := creativecanvas.AddNode(t.Context(), a, retry); err != nil {
		t.Fatal("committed replay must remain readable", err)
	}
	if _, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.RestoreProject(t.Context(), a, command(t, creativecanvas.ProjectStateInput{ProjectID: p.ProjectID, ExpectedRevision: 2})); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		c := command(t, move)
		wg.Go(func() { _, err := creativecanvas.MoveNode(t.Context(), a, c); errs <- err })
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, creativecanvas.ErrVersionConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestStaleAssetInvalidCommandAndRightsRollback(t *testing.T) {
	db, a, b := setup(t)
	v := asset(t, a)
	p := project(t, a)
	input := creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: "cwnode_" + uuid.NewString(), TypeKey: "core.text", ExpectedTopologyRevision: 1, Asset: &creativelibrary.AssetReference{ID: v.ID, Revision: 2, ContentRevisionID: v.ContentRevisionID}}
	if _, err := creativecanvas.AddNode(t.Context(), a, command(t, input)); !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal(err)
	}
	input.Asset.Revision = 1
	if _, err := creativecanvas.AddNode(t.Context(), b, command(t, input)); err == nil {
		t.Fatal("cross account accepted")
	}
	c := command(t, input)
	c.Payload = append(c.Payload[:len(c.Payload)-1], []byte(`,"account_id":"canvas-b"}`)...)
	if _, err := creativecanvas.AddNode(t.Context(), a, c); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='canvas-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.AddNode(t.Context(), a, command(t, input)); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal(err)
	}
	snapshot, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
	if err != nil || len(snapshot.Nodes) != 0 || snapshot.Revision != 1 {
		t.Fatalf("partial write %+v %v", snapshot, err)
	}
}

func TestEmptyNodeFirstContentAndStaleReplacement(t *testing.T) {
	_, a, _ := setup(t)
	p := project(t, a)
	id := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.AddNode(t.Context(), a, command(t, creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: id, TypeKey: "core.text", ExpectedTopologyRevision: 1})); err != nil {
		t.Fatal(err)
	}
	d := draft("首次正文")
	input := creativecanvas.ReplaceContentInput{CanvasID: p.CanvasID, NodeID: id, ExpectedDataRevision: 1, Payload: d.Payload, ContentRights: &d.Rights}
	if _, err := creativecanvas.ReplaceContent(t.Context(), a, command(t, input)); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.ReplaceContent(t.Context(), a, command(t, input)); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatalf("stale first write: %v", err)
	}
	c, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
	if err != nil || len(c.Nodes) != 1 || c.Nodes[0].Content == nil || *c.Nodes[0].Content.Payload.Body != "首次正文" {
		t.Fatalf("first write: %+v %v", c, err)
	}
}

func TestListsPaginationAccountAndArchiveFilters(t *testing.T) {
	_, a, b := setup(t)
	for range 3 {
		asset(t, a)
		project(t, a)
	}
	first, err := creativelibrary.ListAssets(t.Context(), a, 2, "")
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" || first.TotalCount != 3 || first.LibraryRevision != 4 {
		t.Fatalf("first assets %+v %v", first, err)
	}
	second, err := creativelibrary.ListAssets(t.Context(), a, 2, first.NextCursor)
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second assets %+v %v", second, err)
	}
	if _, err := creativelibrary.ListAssets(t.Context(), b, 2, first.NextCursor); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("cross account cursor: %v", err)
	}
	projects, err := creativecanvas.ListProjects(t.Context(), a, false, 2, "")
	if err != nil || len(projects.Items) != 2 || projects.NextCursor == "" {
		t.Fatalf("projects %+v %v", projects, err)
	}
	if _, err := creativecanvas.ListProjects(t.Context(), a, true, 2, projects.NextCursor); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("changed filter cursor %v", err)
	}
	if _, err := creativecanvas.ArchiveProject(t.Context(), a, command(t, creativecanvas.ProjectStateInput{ProjectID: projects.Items[0].ID, ExpectedRevision: 1})); err != nil {
		t.Fatal(err)
	}
	archived, err := creativecanvas.ListProjects(t.Context(), a, true, 30, "")
	if err != nil || len(archived.Items) != 1 || !archived.Items[0].Archived || archived.Items[0].DefaultCanvasID == "" {
		t.Fatalf("archived %+v %v", archived, err)
	}
	empty, err := creativelibrary.ListAssets(t.Context(), b, 30, "")
	if err != nil || len(empty.Items) != 0 || empty.TotalCount != 0 {
		t.Fatalf("isolated %+v %v", empty, err)
	}
}

func TestCanvasPreviewDoesNotReplaceFullRevision(t *testing.T) {
	_, a, _ := setup(t)
	p := project(t, a)
	body := strings.Repeat("长", 2100)
	d := draft(body)
	id := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.AddNode(t.Context(), a, command(t, creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: id, TypeKey: "core.text", ExpectedTopologyRevision: 1, Content: &d})); err != nil {
		t.Fatal(err)
	}
	c, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Nodes[0].Content.Truncated || len([]rune(*c.Nodes[0].Content.Payload.Body)) != 2000 {
		t.Fatal("unbounded or unmarked preview")
	}
	full, err := creativecontent.Read(t.Context(), a, *c.Nodes[0].ContentRevisionID)
	if err != nil || full.Truncated || *full.Payload.Body != body {
		t.Fatalf("full revision lost: %v", err)
	}

	nextBody := "另一个窗口已保存"
	if _, err := creativecanvas.ReplaceContent(t.Context(), a, command(t, creativecanvas.ReplaceContentInput{CanvasID: p.CanvasID, NodeID: id, ExpectedDataRevision: 1, ExpectedContentRevisionID: &full.ID, Payload: creativecontent.Payload{Body: &nextBody}})); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecontent.Read(t.Context(), a, full.ID); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("stale truncated read %v", err)
	}
}

func TestActiveAccountUsesCanvasWithoutEnrollment(t *testing.T) {
	db, a, _ := setup(t)
	if _, err := db.Exec(`DELETE FROM creative_account_capabilities WHERE account_id='canvas-a'`); err != nil {
		t.Fatal(err)
	}
	p := project(t, a)
	if _, err := creativecanvas.GetCanvas(t.Context(), a, p.CanvasID); err != nil {
		t.Fatal(err)
	}
	// Even a stale disabled rollout row no longer controls product access.
	if _, err := db.Exec(`INSERT INTO creative_account_capabilities(account_id,read_enabled,manual_write_enabled) VALUES ('canvas-a',false,false)`); err != nil {
		t.Fatal(err)
	}
	project(t, a)
	if _, err := db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id='canvas-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := creativecanvas.CreateProject(t.Context(), a, command(t, creativecanvas.CreateProjectInput{Name: "inactive"})); !errors.Is(err, store.ErrCreativeAccessDenied) {
		t.Fatalf("inactive account: %v", err)
	}
}

func TestCreativeReferenceCheckDetectsWrongRootIdentity(t *testing.T) {
	db, a, b := setup(t)
	v := asset(t, a)
	p := project(t, a)
	add(t, a, p, v)
	if err := a.CheckCreativeTextReferences(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_assets SET content_id='wrong-content' WHERE account_id='canvas-a'`); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckCreativeTextReferences(t.Context()); err == nil {
		t.Fatal("broken root accepted")
	}
	if err := b.CheckCreativeTextReferences(t.Context()); err != nil {
		t.Fatal("another account affected", err)
	}
}
