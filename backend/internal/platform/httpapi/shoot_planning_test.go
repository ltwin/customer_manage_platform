package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
)

func shootPlanningRequest(
	t *testing.T,
	h http.Handler,
	method, path, token, key, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	return recorder
}

func requirePlanningError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("want status %d, got %d body=%s", status, recorder.Code, recorder.Body.String())
	}
	var envelope httpapi.ErrorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, recorder.Body.String())
	}
	if envelope.Error.Code != code {
		t.Fatalf("want error code %q, got %q body=%s", code, envelope.Error.Code, recorder.Body.String())
	}
}

func TestShootPlanningHTTPVerticalSlice(t *testing.T) {
	router, database, issuer, _ := newCustomerAPIRouterWithContainer(t)
	accountAToken := issueToken(t, issuer, testAcctID)

	unauthorized := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", "", "create-unauthorized", `{"title":"夜景","subject":"角色 A"}`)
	requirePlanningError(t, unauthorized, http.StatusUnauthorized, httpapi.CodeUnauthorized)

	createBody := `{"title":"夜景创作","subject":"冷门角色 A"}`
	created := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "create-plan-1", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create plan: want 201, got %d body=%s", created.Code, created.Body.String())
	}
	var createdPlan struct {
		ID                             string `json:"id"`
		Revision                       int64  `json:"revision"`
		ExecutionFactRevision          int64  `json:"execution_fact_revision"`
		RequiredArchiveAcknowledgement struct {
			Version string `json:"version"`
		} `json:"required_archive_acknowledgement"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdPlan); err != nil {
		t.Fatalf("decode created plan: %v body=%s", err, created.Body.String())
	}
	if createdPlan.ID == "" || createdPlan.Revision != 1 || createdPlan.RequiredArchiveAcknowledgement.Version != "core-v1" {
		t.Fatalf("created detail is incomplete: %s", created.Body.String())
	}
	if strings.Contains(created.Body.String(), "account_id") {
		t.Fatalf("shoot planning response must not expose account_id: %s", created.Body.String())
	}

	replayed := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "create-plan-1", createBody)
	if replayed.Code != http.StatusCreated || !strings.Contains(replayed.Body.String(), createdPlan.ID) {
		t.Fatalf("create replay must return the same plan: %d %s", replayed.Code, replayed.Body.String())
	}
	conflict := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "create-plan-1", `{"title":"不同标题","subject":"冷门角色 A"}`)
	requirePlanningError(t, conflict, http.StatusConflict, httpapi.CodeIdempotencyConflict)

	list := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans?page=1&page_size=20", accountAToken, "", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"items"`) || !strings.Contains(list.Body.String(), createdPlan.ID) {
		t.Fatalf("list plans: %d %s", list.Code, list.Body.String())
	}
	badQuery := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans?account_id=acct-other", accountAToken, "", "")
	requirePlanningError(t, badQuery, http.StatusBadRequest, httpapi.CodeValidationFailed)

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash account B password: %v", err)
	}
	if err := database.CreateAccount(t.Context(), "acct-2", hash); err != nil {
		t.Fatalf("create account B: %v", err)
	}
	accountBToken := issueToken(t, issuer, "acct-2")
	crossAccount := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+createdPlan.ID, accountBToken, "", "")
	requirePlanningError(t, crossAccount, http.StatusNotFound, httpapi.CodeNotFound)

	strictBodies := []struct {
		name, method, path, key, body string
	}{
		{"create account_id", http.MethodPost, "/api/v1/shoot-plans", "strict-create-account", `{"title":"x","subject":"y","account_id":"acct-1"}`},
		{"unknown command", http.MethodPatch, "/api/v1/shoot-plans/" + createdPlan.ID, "strict-unknown-command", `{"expected_revision":1,"operation":"unknown"}`},
		{"mixed command fields", http.MethodPatch, "/api/v1/shoot-plans/" + createdPlan.ID, "strict-mixed-command", `{"expected_revision":1,"operation":"clear_execution_window","shot_id":"shot-x"}`},
		{"nonnull patch field", http.MethodPatch, "/api/v1/shoot-plans/" + createdPlan.ID, "strict-null-title", `{"expected_revision":1,"operation":"update_brief","title":null,"subject":"仍有效"}`},
		{"client capture mode", http.MethodPost, "/api/v1/shoot-plans/" + createdPlan.ID + "/shots/shot-x/capture", "strict-capture-mode", `{"expected_execution_revision":0,"result":"captured","capture_mode":"live"}`},
		{"nonnull skip reason", http.MethodPost, "/api/v1/shoot-plans/" + createdPlan.ID + "/shots/shot-x/capture", "strict-null-skip", `{"expected_execution_revision":0,"result":"captured","skip_reason":null}`},
		{"trailing json", http.MethodPost, "/api/v1/shoot-plans", "strict-trailing", `{"title":"x","subject":"y"}{}`},
		{"missing capture revision", http.MethodPost, "/api/v1/shoot-plans/" + createdPlan.ID + "/shots/shot-x/capture", "strict-missing-revision", `{"result":"captured"}`},
	}
	for _, test := range strictBodies {
		t.Run(test.name, func(t *testing.T) {
			response := shootPlanningRequest(t, router, test.method, test.path, accountAToken, test.key, test.body)
			requirePlanningError(t, response, http.StatusBadRequest, httpapi.CodeValidationFailed)
		})
	}

	missingKey := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "", createBody)
	requirePlanningError(t, missingKey, http.StatusBadRequest, httpapi.CodeValidationFailed)
	missingBody := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "strict-missing-body", "")
	requirePlanningError(t, missingBody, http.StatusBadRequest, httpapi.CodeValidationFailed)

	unknownArchiveField := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+createdPlan.ID+"/transitions", accountAToken, "strict-archive-field", `{"expected_revision":1,"transition":"archive","payload":{"version":"core-v1","effects":["plan_becomes_read_only","execution_history_retained"],"account_id":"acct-1"}}`)
	requirePlanningError(t, unknownArchiveField, http.StatusBadRequest, httpapi.CodeValidationFailed)
	wrongArchiveEffects := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+createdPlan.ID+"/transitions", accountAToken, "archive-wrong-effects", `{"expected_revision":1,"transition":"archive","payload":{"version":"core-v1","effects":["plan_becomes_read_only"]}}`)
	requirePlanningError(t, wrongArchiveEffects, http.StatusConflict, httpapi.CodeArchiveAcknowledgementRequired)
	var archiveConflict struct {
		Error struct {
			Details *struct {
				RequiredArchiveAcknowledgement struct {
					Version string   `json:"version"`
					Effects []string `json:"effects"`
				} `json:"required_archive_acknowledgement"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(wrongArchiveEffects.Body.Bytes(), &archiveConflict); err != nil || archiveConflict.Error.Details == nil ||
		archiveConflict.Error.Details.RequiredArchiveAcknowledgement.Version != "core-v1" ||
		len(archiveConflict.Error.Details.RequiredArchiveAcknowledgement.Effects) != 2 {
		t.Fatalf("archive conflict missing authoritative acknowledgement: err=%v body=%s", err, wrongArchiveEffects.Body.String())
	}

	upsertShot := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+createdPlan.ID, accountAToken, "command-upsert-shot", `{"expected_revision":1,"operation":"upsert_shot","shot":{"title":"主镜头"}}`)
	if upsertShot.Code != http.StatusOK {
		t.Fatalf("upsert shot: %d %s", upsertShot.Code, upsertShot.Body.String())
	}
	var mutation struct {
		Revision          int64 `json:"revision"`
		ChangedProjection struct {
			ShotID string `json:"shot_id"`
		} `json:"changed_projection"`
	}
	if err := json.Unmarshal(upsertShot.Body.Bytes(), &mutation); err != nil || mutation.Revision != 2 || mutation.ChangedProjection.ShotID == "" {
		t.Fatalf("decode upsert shot result: err=%v body=%s", err, upsertShot.Body.String())
	}
	replayedAfterMutation := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", accountAToken, "create-plan-1", createBody)
	if replayedAfterMutation.Code != http.StatusCreated || replayedAfterMutation.Body.String() != created.Body.String() {
		t.Fatalf("create replay changed after later mutation: first=%s replay=%s", created.Body.String(), replayedAfterMutation.Body.String())
	}
	shotDetail := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+createdPlan.ID, accountAToken, "", "")
	var shotProjection struct {
		Shots []struct {
			ReadinessItemIDs []string `json:"readiness_item_ids"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(shotDetail.Body.Bytes(), &shotProjection); err != nil || len(shotProjection.Shots) != 1 {
		t.Fatalf("decode shot detail: err=%v body=%s", err, shotDetail.Body.String())
	}
	if shotProjection.Shots[0].ReadinessItemIDs == nil {
		t.Fatalf("shot readiness_item_ids must be an empty JSON array, got null: %s", shotDetail.Body.String())
	}

	staleCommand := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+createdPlan.ID, accountAToken, "command-stale", `{"expected_revision":1,"operation":"clear_execution_window"}`)
	requirePlanningError(t, staleCommand, http.StatusConflict, httpapi.CodePlanRevisionConflict)

	markReady := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+createdPlan.ID+"/transitions", accountAToken, "transition-ready", `{"expected_revision":2,"transition":"mark_ready","payload":{}}`)
	if markReady.Code != http.StatusOK {
		t.Fatalf("mark ready: %d %s", markReady.Code, markReady.Body.String())
	}

	openSession := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+createdPlan.ID+"/run-sessions", accountAToken, "run-open", `{"expected_revision":3}`)
	if openSession.Code != http.StatusCreated {
		t.Fatalf("open run session: %d %s", openSession.Code, openSession.Body.String())
	}
	var opened struct {
		PlanRevision int64 `json:"plan_revision"`
		Session      struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(openSession.Body.Bytes(), &opened); err != nil || opened.PlanRevision != 4 || opened.Session.ID == "" {
		t.Fatalf("decode open session: err=%v body=%s", err, openSession.Body.String())
	}

	capturePath := "/api/v1/shoot-plans/" + createdPlan.ID + "/shots/" + mutation.ChangedProjection.ShotID + "/capture"
	captured := shootPlanningRequest(t, router, http.MethodPost, capturePath, accountAToken, "capture-1", `{"expected_execution_revision":0,"session_id":"`+opened.Session.ID+`","result":"captured"}`)
	if captured.Code != http.StatusCreated {
		t.Fatalf("capture shot: %d %s", captured.Code, captured.Body.String())
	}
	var captureResult struct {
		ExecutionFactRevision int64 `json:"execution_fact_revision"`
		ExecutionRevision     int64 `json:"execution_revision"`
		Event                 struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(captured.Body.Bytes(), &captureResult); err != nil || captureResult.Event.ID == "" || captureResult.ExecutionRevision != 1 {
		t.Fatalf("decode capture: err=%v body=%s", err, captured.Body.String())
	}
	if strings.Contains(captured.Body.String(), `"skip_reason"`) {
		t.Fatalf("captured result must omit non-applicable skip_reason: %s", captured.Body.String())
	}

	voidPath := "/api/v1/shoot-plans/" + createdPlan.ID + "/execution-events/" + captureResult.Event.ID + "/void"
	voided := shootPlanningRequest(t, router, http.MethodPost, voidPath, accountAToken, "void-key-1", `{"expected_execution_revision":1,"reason":"误触"}`)
	if voided.Code != http.StatusCreated {
		t.Fatalf("void execution event: %d %s; path=%s capture=%s", voided.Code, voided.Body.String(), voidPath, captured.Body.String())
	}

	recaptured := shootPlanningRequest(t, router, http.MethodPost, capturePath, accountAToken, "capture-2", `{"expected_execution_revision":2,"session_id":"`+opened.Session.ID+`","result":"captured"}`)
	if recaptured.Code != http.StatusCreated {
		t.Fatalf("recapture shot: %d %s", recaptured.Code, recaptured.Body.String())
	}
	var recaptureResult struct {
		ExecutionFactRevision int64 `json:"execution_fact_revision"`
	}
	if err := json.Unmarshal(recaptured.Body.Bytes(), &recaptureResult); err != nil || recaptureResult.ExecutionFactRevision != 3 {
		t.Fatalf("decode recapture: err=%v body=%s", err, recaptured.Body.String())
	}

	complete := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+createdPlan.ID+"/transitions", accountAToken, "transition-complete", `{"expected_revision":4,"transition":"complete","payload":{"expected_execution_fact_revision":3}}`)
	if complete.Code != http.StatusOK || !strings.Contains(complete.Body.String(), `"status":"completed"`) {
		t.Fatalf("complete plan: %d %s", complete.Code, complete.Body.String())
	}

	detail := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+createdPlan.ID+"?include=execution_history", accountAToken, "", "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"execution_history"`) || !strings.Contains(detail.Body.String(), `"finalizations"`) {
		t.Fatalf("detail with history: %d %s", detail.Code, detail.Body.String())
	}

	// 模拟 marker 已由 release-only control plane 提升、旧 core binary 尚未退出：
	// 下一次 request 必须 fail closed 为通用 500，且不能泄露 wiring 自由文本。
	currentCapability, err := database.ArchiveCapabilityPromoter().Show(t.Context())
	if err != nil {
		t.Fatalf("show archive capability: %v", err)
	}
	now := time.Now().UTC()
	_, err = database.ArchiveCapabilityPromoter().Promote(t.Context(), currentCapability.Revision, planningcapability.ArchiveCapabilityPlanningShare, planningcapability.ReleaseReadinessEvidence{
		Digest: "test-readiness", Environment: "test", Deployment: "httpapi-test",
		Target: planningcapability.ArchiveCapabilityPlanningShare, CurrentRevision: currentCapability.Revision,
		GeneratedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		LiveBuildsCompatible: true, TargetWiringReady: true,
	})
	if err != nil {
		t.Fatalf("promote archive capability: %v", err)
	}
	failClosed := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+createdPlan.ID, accountAToken, "", "")
	requirePlanningError(t, failClosed, http.StatusInternalServerError, httpapi.CodeInternal)
	if strings.Contains(failClosed.Body.String(), "wiring") || strings.Contains(failClosed.Body.String(), "capability") {
		t.Fatalf("unexpected errors must not leak internal text: %s", failClosed.Body.String())
	}
}
