package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// admitAll stands in for the agent runtime: every run may write, settling does nothing.
type admitAll struct{}

func (admitAll) Admit(context.Context, store.TxAccountScope, creativescene.Job) (creativescene.RunGate, error) {
	return struct{}{}, nil
}
func (admitAll) Settle(context.Context, store.TxAccountScope, creativescene.RunGate, creativescene.Job, creativescene.JobResult) error {
	return nil
}
func (admitAll) Abort(context.Context, store.AccountScope, string, error) error { return nil }

func TestCreativeSceneJobsHTTP(t *testing.T) {
	ctx := context.Background()
	creativescene.SetJobWaiterForTest(t, admitAll{})
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `';
INSERT INTO accounts(id,password_hash,status) VALUES('acct-other','test','active')`); err != nil {
		t.Fatal(err)
	}
	token, other := issueToken(t, tokens, testAcctID), issueToken(t, tokens, "acct-other")
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	typ, _ := creativedocument.Lookup(creativescene.TypeKey)
	var doc creativedocument.Document
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInTx(ctx, tx, typ, "中殿", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	newRenderJob := func(step string) string {
		t.Helper()
		var job creativescene.Job
		if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			job, err = creativescene.CreateJob(ctx, tx, creativescene.NewJob{DocumentID: doc.ID, Kind: creativescene.KindRenderView, RunID: "ccrn_http", ToolCallID: step,
				Request: creativescene.RenderRequest{Preset: "overview", Mode: "clay"}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return job.ID
	}
	call := func(method, path, tok, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		return shootPlanningRequest(t, router, method, path, tok, key, body)
	}
	want := func(name string, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && documentErrorCode(t, w) != code {
			t.Fatalf("%s: %d %s, want %d %s", name, w.Code, w.Body.String(), status, code)
		}
	}
	const tab = `"editor_session_id":"tab_0123456789abcdef"`
	claimPath := "/api/v1/creative/scene-jobs/claim"

	want("bad session", call("POST", claimPath, token, "", `{"editor_session_id":"x"}`), 400, "validation_failed")
	want("unknown kind", call("POST", claimPath, token, "", `{`+tab+`,"kinds":["bake"]}`), 400, "validation_failed")
	want("empty queue", call("POST", claimPath, token, "", `{`+tab+`}`), 204, "")
	want("other account's scene", call("POST", claimPath, other, "", `{`+tab+`,"document_id":"`+doc.ID+`"}`), 404, "not_found")

	jobID := newRenderJob("step-1")
	list := call("GET", "/api/v1/creative/documents/"+doc.ID+"/scene-jobs", token, "", "")
	want("list", list, 200, "")
	var listed struct {
		Items []struct {
			ID     string          `json:"id"`
			State  string          `json:"state"`
			Target json.RawMessage `json:"target"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].ID != jobID || string(listed.Items[0].Target) != "null" {
		t.Fatalf("list %s", list.Body.String())
	}
	want("other account's list", call("GET", "/api/v1/creative/documents/"+doc.ID+"/scene-jobs", other, "", ""), 404, "not_found")
	want("other account claims nothing", call("POST", claimPath, other, "", `{`+tab+`}`), 204, "")

	claimed := call("POST", claimPath, token, "", `{`+tab+`,"kinds":["bake_model","render_view"]}`)
	want("claim", claimed, 200, "")
	var claim struct {
		Job struct {
			ID         string `json:"id"`
			DocumentID string `json:"document_id"`
			Kind       string `json:"kind"`
			ClaimToken string `json:"claim_token"`
			Attempt    int    `json:"attempt"`
		} `json:"job"`
	}
	if err := json.Unmarshal(claimed.Body.Bytes(), &claim); err != nil || claim.Job.ID != jobID || claim.Job.DocumentID != doc.ID || claim.Job.Kind != "render_view" || claim.Job.ClaimToken == "" {
		t.Fatalf("claim %s", claimed.Body.String())
	}
	lease := "/api/v1/creative/scene-jobs/" + jobID + "/lease"
	want("lease", call("POST", lease, token, "", `{"claim_token":"`+claim.Job.ClaimToken+`"}`), 200, "")
	want("lease with another token", call("POST", lease, token, "", `{"claim_token":"clm_x"}`), 409, "creative_state_conflict")
	want("lease from another account", call("POST", lease, other, "", `{"claim_token":"`+claim.Job.ClaimToken+`"}`), 404, "not_found")
	want("lease of a missing job", call("POST", "/api/v1/creative/scene-jobs/csjob_missing/lease", token, "", `{"claim_token":"x"}`), 404, "not_found")

	complete := func(id, tok, key string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC().Format(time.RFC3339Nano), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		return call("POST", "/api/v1/creative/scene-jobs/"+id+"/complete", tok, key, string(raw))
	}
	image := stageForHTTP(t, db, scope, "image", creativeglb.Manifest{})
	render := map[string]any{"claim_token": claim.Job.ClaimToken, "outcome": "succeeded", "render": map[string]any{"image_upload_id": image, "scene_revision": "1"}}
	want("complete from another account", complete(jobID, other, uuid.NewString(), render), 404, "not_found")
	want("complete with a missing upload", complete(jobID, token, uuid.NewString(),
		map[string]any{"claim_token": claim.Job.ClaimToken, "outcome": "succeeded", "render": map[string]any{"image_upload_id": "ccup_missing", "scene_revision": "1"}}), 409, "creative_upload_unavailable")
	want("complete with a bad error code", complete(jobID, token, uuid.NewString(),
		map[string]any{"claim_token": claim.Job.ClaimToken, "outcome": "failed", "error": map[string]any{"code": "oops", "message": "x"}}), 400, "validation_failed")
	key := uuid.NewString()
	done := complete(jobID, token, key, render)
	want("complete", done, 200, "")
	var result struct {
		JobID  string `json:"job_id"`
		State  string `json:"state"`
		Result struct {
			ImageUploadID     string `json:"image_upload_id"`
			ContentRevisionID string `json:"content_revision_id"`
			SceneRevision     string `json:"scene_revision"`
		} `json:"result"`
	}
	if err := json.Unmarshal(done.Body.Bytes(), &result); err != nil || result.State != "succeeded" || result.Result.ImageUploadID != image || result.Result.ContentRevisionID == "" || result.Result.SceneRevision != "1" {
		t.Fatalf("complete %s", done.Body.String())
	}
	want("complete again with another key", complete(jobID, token, uuid.NewString(), render), 409, "creative_state_conflict")
	want("lease after completion", call("POST", lease, token, "", `{"claim_token":"`+claim.Job.ClaimToken+`"}`), 409, "creative_state_conflict")
}

func TestCreativeSceneRunChangesHTTP(t *testing.T) {
	ctx := context.Background()
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `';
INSERT INTO accounts(id,password_hash,status) VALUES('acct-other','test','active')`); err != nil {
		t.Fatal(err)
	}
	runID := "ccrn_" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO creative_agent_runs(account_id,id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,
		model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at)
		VALUES($1,$2,'c','v','m','e','k','{}','running','t','v1','{}',now()+interval '1 hour')`, testAcctID, runID); err != nil {
		t.Fatal(err)
	}
	token, other := issueToken(t, tokens, testAcctID), issueToken(t, tokens, "acct-other")
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	typ, _ := creativedocument.Lookup(creativescene.TypeKey)
	var doc creativedocument.Document
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		doc, err = creativedocument.CreateInTx(ctx, tx, typ, "中殿", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The run adds a pillar.
	run := runID
	object := map[string]any{"id": "sobj_000000000000000000001", "kind": "primitive", "name": "柱 A", "parent_id": nil, "order": 10,
		"transform": map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
		"visible":   true, "locked": false, "primitive": map[string]any{"builtin": "box@1"}}
	raw, err := json.Marshal(map[string]any{"document_id": doc.ID, "expected": map[string]any{"objects": "1"},
		"operations": []any{map[string]any{"op": "add_objects", "objects": []any{object}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creativedocument.CommandAs(ctx, scope, creativedocument.Actor{Kind: "agent", RunID: &run},
		creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/creative/documents/" + doc.ID + "/run-changes?run_id="
	call := func(tok, query string) *httptest.ResponseRecorder {
		t.Helper()
		return shootPlanningRequest(t, router, "GET", path+query, tok, "", "")
	}
	ok := call(token, runID)
	if ok.Code != 200 {
		t.Fatalf("run changes: %d %s", ok.Code, ok.Body.String())
	}
	var got struct {
		RunID     string   `json:"run_id"`
		Revisions []string `json:"revisions"`
		Entries   []struct {
			Target  map[string]string `json:"target"`
			Name    string            `json:"name"`
			Change  string            `json:"change"`
			Restore *struct {
				FromRevision string             `json:"from_revision"`
				Units        []string           `json:"units"`
				Expected     map[string]*string `json:"expected"`
			} `json:"restore"`
			Blocked json.RawMessage `json:"blocked"`
		} `json:"entries"`
		UndoAll []json.RawMessage `json:"undo_all"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &got); err != nil || got.RunID != runID || len(got.Revisions) != 1 || got.Revisions[0] != "2" || len(got.Entries) != 1 || len(got.UndoAll) != 1 {
		t.Fatalf("body %s", ok.Body.String())
	}
	e := got.Entries[0]
	if e.Target["kind"] != "object" || e.Name != "柱 A" || e.Change != "added" || e.Restore == nil || e.Restore.FromRevision != "1" ||
		e.Restore.Expected["objects"] == nil || *e.Restore.Expected["objects"] != "2" || string(e.Blocked) != "null" {
		t.Fatalf("entry %s", ok.Body.String())
	}
	for name, tc := range map[string]struct {
		tok, query string
		status     int
	}{
		"missing run id":  {token, "", 400},
		"unknown run":     {token, "ccrn_missing", 404},
		"another account": {other, runID, 404},
	} {
		if w := call(tc.tok, tc.query); w.Code != tc.status {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestCreativeSceneExportJobTransparentCompleteHTTP(t *testing.T) {
	ctx := context.Background()
	creativescene.SetJobWaiterForTest(t, admitAll{})
	creativecanvas.SetExportFreshness(creativescene.Freshness{})
	t.Cleanup(func() { creativecanvas.SetExportFreshness(nil) })

	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`UPDATE accounts SET status='active' WHERE id='` + testAcctID + `'`); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})

	r, err := creativecanvas.CreateProject(ctx, scope, creativeCommand(t, creativecanvas.CreateProjectInput{Name: "任务透明导出"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	canvas, scene := groupedSceneForExport(t, scope, p.CanvasID)
	doc := *scene.DocumentID
	cam := "sobj_C00000000000000000003"
	if _, err := creativedocument.Command(ctx, scope, creativeCommand(t, map[string]any{
		"document_id": doc, "expected": map[string]any{"objects": "1"},
		"operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{
			"id": cam, "kind": "camera", "name": "", "parent_id": nil, "order": 1,
			"transform": map[string]any{"position": []float64{0, 1, 5}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"visible":   true, "locked": false, "camera": map[string]any{},
		}}}},
	})); err != nil {
		t.Fatal(err)
	}

	queueExport := func(tr bool) (creativescene.Job, string) {
		t.Helper()
		var job creativescene.Job
		if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var err error
			job, err = creativescene.CreateExportJobInTx(ctx, tx, creativescene.ExportJobInput{
				DocumentID: doc, CanvasID: p.CanvasID, RunID: "ccrn_http", ToolCallID: "step-" + uuid.NewString(),
				Passes: []string{"clay"}, TransparentBackground: &tr,
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		const tab = `"editor_session_id":"tab_0123456789abcdef"`
		w := shootPlanningRequest(t, router, "POST", "/api/v1/creative/scene-jobs/claim", token, "", `{`+tab+`,"kinds":["export_shots"]}`)
		if w.Code != 200 {
			t.Fatalf("claim = %d %s", w.Code, w.Body.String())
		}
		var out struct {
			Job struct {
				ClaimToken string `json:"claim_token"`
			} `json:"job"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return job, out.Job.ClaimToken
	}
	complete := func(jobID, tok string, exp map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		key := uuid.NewString()
		raw, _ := json.Marshal(map[string]any{
			"operation_id": key, "client_created_at": time.Now().UTC().Format(time.RFC3339Nano),
			"payload": map[string]any{"claim_token": tok, "outcome": "succeeded", "export": exp},
		})
		return shootPlanningRequest(t, router, "POST", "/api/v1/creative/scene-jobs/"+jobID+"/complete", token, key, string(raw))
	}

	// Explicit null / non-bool -> 400 validation_failed (job stays claimed).
	job1, tok1 := queueExport(true)
	up1 := stageForHTTP(t, db, scope, "image", creativeglb.Manifest{})
	shots := []any{map[string]any{"camera_object_id": cam, "images": []any{map[string]any{"pass": "clay", "upload_id": up1}}}}
	for name, bad := range map[string]any{"null": nil, "bool str": "true"} {
		w := complete(job1.ID, tok1, map[string]any{"scene_revision": "2", "effective_transparent_background": bad, "shots": shots})
		if w.Code != 400 || documentErrorCode(t, w) != "validation_failed" {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	// Missing effective_transparent_background on explicit true override -> 200 state=failed invalid_result, zero nodes.
	wMissing := complete(job1.ID, tok1, map[string]any{"scene_revision": "2", "shots": shots})
	if wMissing.Code != 200 {
		t.Fatalf("missing report: %d %s", wMissing.Code, wMissing.Body.String())
	}
	var failOut struct {
		State  string `json:"state"`
		Result struct {
			Code string `json:"code"`
		} `json:"result"`
	}
	if err := json.Unmarshal(wMissing.Body.Bytes(), &failOut); err != nil || failOut.State != "failed" || failOut.Result.Code != "invalid_result" {
		t.Fatalf("missing report body = %s", wMissing.Body.String())
	}
	cAfterFail, _ := creativecanvas.GetCanvas(ctx, scope, p.CanvasID)
	if len(cAfterFail.Nodes) != len(canvas.Nodes) {
		t.Fatalf("failed completion placed nodes: %d", len(cAfterFail.Nodes))
	}

	// Valid retry on fresh job with unconsumed up1 + effective_transparent_background=true succeeds.
	job2, tok2 := queueExport(true)
	wOK := complete(job2.ID, tok2, map[string]any{"scene_revision": "2", "effective_transparent_background": true, "shots": shots})
	if wOK.Code != 200 {
		t.Fatalf("valid completion = %d %s", wOK.Code, wOK.Body.String())
	}
	var okOut struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(wOK.Body.Bytes(), &okOut); err != nil || okOut.State != "succeeded" {
		t.Fatalf("valid completion body = %s", wOK.Body.String())
	}
	assertGroupedExports(t, scope, canvas, *scene.ParentID, 1)
}
