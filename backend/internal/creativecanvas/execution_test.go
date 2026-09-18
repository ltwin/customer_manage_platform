package creativecanvas_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func executionFixture(t *testing.T) (*sql.DB, store.AccountScope, jobs.Runtime, *creativecanvas.ExecutionService, creativecanvas.ProjectResult, []string) {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('executor-account','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err = st.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc, err := creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{creativecanvas.InternalTextCompose()})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := st.NewJobRuntime([]store.JobHandler{svc.Handler()}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	scope := st.ScopeFor(auth.AccountContext{AccountID: "executor-account"})
	p := project(t, scope)
	ids := []string{"cwnode_" + uuid.NewString(), "cwnode_" + uuid.NewString(), "cwnode_" + uuid.NewString()}
	a, b := draft("窗边自然光"), draft("柔和的人像")
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: ids[0], TypeKey: "core.text", Content: &a}, creativecanvas.Action{Type: "add_node", NodeID: ids[1], TypeKey: "core.text", Content: &b}, creativecanvas.Action{Type: "add_node", NodeID: ids[2], TypeKey: "core.text"})
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "connect_reference", SourceNodeID: ids[0], TargetNodeID: ids[2], SourcePort: "output", TargetPort: "reference", Role: "reference"}, creativecanvas.Action{Type: "connect_reference", SourceNodeID: ids[1], TargetNodeID: ids[2], SourcePort: "output", TargetPort: "reference", Role: "reference"})
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	_, err = creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: ids[2], ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), ActionKey: "internal.text-compose", Text: "合并参考", Parameters: json.RawMessage(`{}`)}))
	if err != nil {
		t.Fatal(err)
	}
	return db, scope, runtime, svc, p, ids
}
func requestExecution(t *testing.T, scope store.AccountScope, runtime jobs.Runtime, svc *creativecanvas.ExecutionService, p creativecanvas.ProjectResult, id string) (creativecanvas.NodeExecution, creativeops.Command) {
	t.Helper()
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	var n creativecanvas.Node
	for _, node := range c.Nodes {
		if node.ID == id {
			n = node
		}
	}
	cmd := command(t, creativecanvas.RequestExecutionInput{CanvasID: p.CanvasID, NodeID: id, ActionKey: "internal.text-compose", ExpectedDataRevision: n.DataRevision, DraftRevision: n.Prompt.Revision, ReadSet: creativecanvas.ReadSetFor(c)})
	r, err := svc.Request(t.Context(), scope, cmd, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome.HTTPStatus != 202 {
		t.Fatal("execution not accepted")
	}
	var e creativecanvas.NodeExecution
	if err = json.Unmarshal(r.Outcome.Response, &e); err != nil {
		t.Fatal(err)
	}
	return e, cmd
}
func TestInternalExecutionPersistentResultVersionAndUndo(t *testing.T) {
	db, scope, runtime, svc, p, ids := executionFixture(t)
	e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
	replay, err := svc.Request(t.Context(), scope, cmd, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if string(replay.Outcome.Response) == "" {
		t.Fatal("missing receipt")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&count); err != nil || count != 1 {
		t.Fatal("acceptance should enqueue once", count, err)
	}
	target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
	raw, _ := json.Marshal(target)
	job := jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}
	before, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	var data creativeops.Revision
	for _, n := range before.Nodes {
		if n.ID == ids[2] {
			data = n.DataRevision
			if n.Status.GenerationState != "queued" {
				t.Fatal("status not projected")
			}
		}
	}
	if err = svc.Work(t.Context(), scope, job); err != nil {
		t.Fatal(err)
	}
	if err = svc.Work(t.Context(), scope, job); err != nil {
		t.Fatal("duplicate worker", err)
	}
	finished, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
	if err != nil || finished.State != "succeeded" || finished.ApplyState != "applied" {
		t.Fatal("missing durable apply", finished, err)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 1 || versions.SelectedVersionID == nil {
		t.Fatal("missing version", versions, err)
	}
	after, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	for _, n := range after.Nodes {
		if n.ID == ids[2] {
			if n.DataRevision != data+1 || n.Content == nil || n.Content.Payload.Body == nil {
				t.Fatal("status polluted data revision", n)
			}
		}
	}
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: *finished.ChangeID, ReadSet: creativecanvas.ReadSetFor(after)})); err != nil {
		t.Fatal("published version not reversible", err)
	}
	versions, err = creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 0 || versions.SelectedVersionID != nil {
		t.Fatal("version apply undo failed", versions, err)
	}
}
func TestExecutionCancellationAndPromptCAS(t *testing.T) {
	_, scope, runtime, svc, p, ids := executionFixture(t)
	e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
	target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
	if _, err := svc.Request(t.Context(), scope, command(t, creativecanvas.RequestExecutionInput{CanvasID: p.CanvasID, NodeID: ids[2], ActionKey: "not-registered", ExpectedDataRevision: 1, DraftRevision: 1}), runtime); !errors.Is(err, creativecanvas.ErrActionUnavailable) {
		t.Fatal("unknown action accepted", err)
	}
	if _, err := creativecanvas.CancelNodeExecution(t.Context(), scope, command(t, target)); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(target)
	if err := svc.Work(t.Context(), scope, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 0 {
		t.Fatal("cancelled execution published", err)
	}
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	input := creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: ids[2], ExpectedTopologyRevision: c.TopologyRevision, ActionKey: "internal.text-compose", Parameters: json.RawMessage(`{}`)}
	if _, err = creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, input)); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatal("missing prompt CAS accepted", err)
	}
}

func TestExecutionPreservesEditsAndContributorRights(t *testing.T) {
	for _, editSource := range []bool{false, true} {
		t.Run(fmt.Sprint("source=", editSource), func(t *testing.T) {
			db, scope, runtime, svc, p, ids := executionFixture(t)
			e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
			changed := ids[2]
			if editSource {
				changed = ids[0]
			}
			manual := draft("生成期间继续编辑")
			action := creativecanvas.Action{Type: "replace_content", NodeID: changed, Payload: &manual.Payload}
			if !editSource {
				action.Rights = &manual.Rights
			}
			graph(t, scope, p.CanvasID, action)
			target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
			raw, _ := json.Marshal(target)
			if err := svc.Work(t.Context(), scope, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}); err != nil {
				t.Fatal(err)
			}
			finished, err := creativecanvas.GetNodeExecution(t.Context(), scope, target)
			if err != nil || finished.ApplyState != "conflicted" {
				t.Fatal("late result adopted", finished, err)
			}
			versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
			if err != nil || len(versions.Items) == 0 {
				t.Fatal("conflicted result not retained", err)
			}
			var generated creativecanvas.NodeVersion
			for _, version := range versions.Items {
				if version.Origin == "generation" {
					generated = version
				}
			}
			if generated.ID == "" {
				t.Fatal("missing generated version")
			}
			if _, err = creativecontent.Read(t.Context(), scope, generated.ContentRevisionID); err != nil {
				t.Fatal(err)
			}
			// The second contributing declaration must still govern the composed output.
			c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
			if err != nil {
				t.Fatal(err)
			}
			source := findNode(t, c, ids[1])
			if _, err = db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='executor-account' AND declaration_id=$1`, source.Content.DeclarationID); err != nil {
				t.Fatal(err)
			}
			if _, err = creativecontent.Read(t.Context(), scope, generated.ContentRevisionID); !errors.Is(err, creativecontent.ErrUsageDenied) {
				t.Fatal("derived output laundered contributor rights", err)
			}
		})
	}
}

type pausedComposer struct {
	creativecanvas.NodeExecutor
	entered, release chan struct{}
}

func (p pausedComposer) Step(ctx context.Context, scope store.AccountScope, step creativecanvas.ExecutionStep) (creativecanvas.ExecutionObservation, error) {
	close(p.entered)
	select {
	case <-p.release:
		return p.NodeExecutor.Step(ctx, scope, step)
	case <-ctx.Done():
		return creativecanvas.ExecutionObservation{}, ctx.Err()
	}
}
func TestCancellationDuringComposeCannotPublish(t *testing.T) {
	_, scope, runtime, svc, p, ids := executionFixture(t)
	e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
	executor := pausedComposer{NodeExecutor: creativecanvas.InternalTextCompose(), entered: make(chan struct{}), release: make(chan struct{})}
	worker, err := creativecanvas.NewExecutionService([]creativecanvas.NodeExecutor{executor})
	if err != nil {
		t.Fatal(err)
	}
	target := creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID}
	raw, _ := json.Marshal(target)
	job := jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}
	done := make(chan error, 1)
	go func() { done <- worker.Work(t.Context(), scope, job) }()
	select {
	case <-executor.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker never entered composer")
	}
	if _, err = creativecanvas.CancelNodeExecution(t.Context(), scope, command(t, target)); err != nil {
		close(executor.release)
		t.Fatal(err)
	}
	close(executor.release)
	if err = <-done; err != nil && !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatal(err)
	}
	if err = worker.Work(t.Context(), scope, job); err != nil {
		t.Fatal("cancelled retry", err)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 0 {
		t.Fatal("cancelled output published", versions, err)
	}
}

func TestVersionReuseSelectionCopyAndDeletionUndo(t *testing.T) {
	_, scope, runtime, svc, p, ids := executionFixture(t)
	e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
	raw, _ := json.Marshal(creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID})
	if err := svc.Work(t.Context(), scope, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 1 {
		t.Fatal(versions, err)
	}
	v := versions.Items[0]
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	n := findNode(t, c, ids[2])
	if _, err = creativecanvas.ReuseVersionPrompt(t.Context(), scope, command(t, creativecanvas.ReuseVersionInput{CanvasID: p.CanvasID, NodeID: ids[2], VersionID: v.ID, ExpectedDraftRevision: &n.Prompt.Revision, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	n = findNode(t, c, ids[2])
	if len(n.Prompt.References) != 2 {
		t.Fatal("version prompt lost fixed inputs", n.Prompt)
	}
	for _, ref := range n.Prompt.References {
		if ref.ContentRevisionID == nil || ref.SourceNodeID != nil {
			t.Fatal("reuse captured live inputs", ref)
		}
	}
	copy := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{ids[2]}, DX: 36, DY: 36})
	copied, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, copy.IDMapping[ids[2]])
	if err != nil || len(copied.Items) != 1 || copied.Items[0].ID == v.ID || copied.Items[0].Origin != "copy" {
		t.Fatal("copy shares mutable version ownership", copied, err)
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "clear_content", NodeID: ids[2]})
	deleted := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "delete_version", NodeID: ids[2], VersionID: v.ID})
	versions, err = creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 0 {
		t.Fatal("deleted version remained", versions, err)
	}
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if _, err = creativecanvas.Undo(t.Context(), scope, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: deleted.ChangeID, ReadSet: creativecanvas.ReadSetFor(c)})); err != nil {
		t.Fatal(err)
	}
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "select_version", NodeID: ids[2], VersionID: v.ID})
	versions, err = creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || versions.SelectedVersionID == nil || *versions.SelectedVersionID != v.ID || versions.Items[0].Number != v.Number || versions.Items[0].Revision <= v.Revision {
		t.Fatal("version restore reset identity or physical guard", versions, err)
	}
}
