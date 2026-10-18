package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// admitAll stands in for the agent runtime: every run may write, settling does nothing.
type admitAll struct{}

func (admitAll) Admit(context.Context, store.TxAccountScope, string) (creativescene.RunGate, error) {
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
