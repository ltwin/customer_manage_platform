package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
)

func TestShootPlanningBusinessHTTPVerticalSlice(t *testing.T) {
	router, database, issuer, _ := newCustomerAPIRouterWithContainer(t)
	token := issueToken(t, issuer, testAcctID)

	created := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", token, "business-http-create", `{"title":"经营接口","subject":"角色 B"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create business plan: %d %s", created.Code, created.Body.String())
	}
	var plan struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil || plan.ID == "" || plan.Revision != 1 {
		t.Fatalf("decode business plan: err=%v body=%s", err, created.Body.String())
	}
	initialDetail := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+plan.ID, token, "", "")
	if initialDetail.Code != http.StatusOK || !strings.Contains(initialDetail.Body.String(), `"business"`) {
		t.Fatalf("business detail projection: %d %s", initialDetail.Code, initialDetail.Body.String())
	}

	unauthorized := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts", "", "business-http-unauthorized", `{"expected_plan_revision":1,"expected_business_facts_revision":0,"draft_kinds":["order_adjustment"]}`)
	requirePlanningError(t, unauthorized, http.StatusUnauthorized, httpapi.CodeUnauthorized)
	badFacts := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+plan.ID, token, "business-http-bad-facts", `{"expected_revision":1,"operation":"set_business_facts","expected_business_facts_revision":0,"facts":{"rented_location_count":0,"assistant_count":0,"retouched_photo_count":0}}`)
	requirePlanningError(t, badFacts, http.StatusBadRequest, httpapi.CodeValidationFailed)
	rangeFacts := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+plan.ID, token, "business-http-range-facts", `{"expected_revision":1,"operation":"set_business_facts","expected_business_facts_revision":0,"facts":{"rented_location_count":101,"assistant_count":0,"retouched_photo_count":0,"estimated_duration_minutes":420}}`)
	requirePlanningError(t, rangeFacts, http.StatusBadRequest, httpapi.CodeValidationFailed)

	facts := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+plan.ID, token, "business-http-facts", `{"expected_revision":1,"operation":"set_business_facts","expected_business_facts_revision":0,"facts":{"rented_location_count":1,"assistant_count":0,"retouched_photo_count":18,"estimated_duration_minutes":420}}`)
	if facts.Code != http.StatusOK || !strings.Contains(facts.Body.String(), `"revision":2`) {
		t.Fatalf("set business facts: %d %s", facts.Code, facts.Body.String())
	}

	missingOrder := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts", token, "business-http-missing-order", `{"expected_plan_revision":2,"expected_business_facts_revision":1,"draft_kinds":["order_adjustment","schedule_duration"]}`)
	requirePlanningError(t, missingOrder, http.StatusConflict, "business_draft_unavailable")
	var unavailable struct {
		Error struct {
			Details struct {
				OrderAdjustment  struct{ State, Reason string } `json:"order_adjustment"`
				ScheduleDuration struct{ State, Reason string } `json:"schedule_duration"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(missingOrder.Body.Bytes(), &unavailable); err != nil ||
		unavailable.Error.Details.OrderAdjustment.Reason != "order_required" ||
		unavailable.Error.Details.ScheduleDuration.Reason != "order_required" {
		t.Fatalf("typed unavailable details missing: err=%v body=%s", err, missingOrder.Body.String())
	}
	scope := database.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	draftCount, err := scope.Count(t.Context(), "planning_business_drafts", "plan_id = $2", plan.ID)
	if err != nil || draftCount != 0 {
		t.Fatalf("all-unavailable generation wrote drafts: count=%d err=%v", draftCount, err)
	}
	badKinds := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts", token, "business-http-bad-kinds", `{"expected_plan_revision":2,"expected_business_facts_revision":1,"draft_kinds":["order_adjustment","order_adjustment"]}`)
	requirePlanningError(t, badKinds, http.StatusBadRequest, httpapi.CodeValidationFailed)

	customers := customerdomain.NewService(customerdomain.NewPostgresRepository())
	customer, err := customers.Create(t.Context(), scope, customerdomain.CreateInput{
		DisplayName: "经营接口客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformOther, Handle: "business-http-customer"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	basePrice := 268000
	orderTitle := "经营接口订单"
	createdOrder, err := orderdomain.NewService(orderdomain.NewPostgresRepository()).Create(t.Context(), scope, orderdomain.CreateInput{
		CreationMode: orderdomain.CreationModeNew,
		CustomerID:   customer.ID,
		Title:        &orderTitle,
		Price:        &basePrice,
	})
	if err != nil {
		t.Fatal(err)
	}
	linked := shootPlanningRequest(t, router, http.MethodPatch, "/api/v1/shoot-plans/"+plan.ID, token, "business-http-link-order", `{"expected_revision":2,"operation":"link_order","order_id":"`+createdOrder.ID+`"}`)
	if linked.Code != http.StatusOK || !strings.Contains(linked.Body.String(), `"revision":2`) {
		t.Fatalf("link business order: %d %s", linked.Code, linked.Body.String())
	}

	generatedResponse := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts", token, "business-http-generate", `{"expected_plan_revision":2,"expected_business_facts_revision":1,"draft_kinds":["order_adjustment","schedule_duration"]}`)
	if generatedResponse.Code != http.StatusCreated {
		t.Fatalf("generate business drafts: %d %s", generatedResponse.Code, generatedResponse.Body.String())
	}
	type draftProjection struct {
		ID                      string `json:"id"`
		Revision                int64  `json:"revision"`
		RequiredAcknowledgement *struct {
			Version string   `json:"version"`
			Effects []string `json:"effects"`
		} `json:"required_acknowledgement"`
	}
	var generated struct {
		OrderAdjustment struct {
			State      string           `json:"state"`
			OrderDraft *draftProjection `json:"order_draft"`
		} `json:"order_adjustment"`
		ScheduleDuration struct {
			State         string `json:"state"`
			ScheduleDraft *struct {
				draftProjection
				TargetMode   string `json:"target_mode"`
				BasisMinutes int    `json:"basis_minutes"`
			} `json:"schedule_draft"`
		} `json:"schedule_duration"`
	}
	if err := json.Unmarshal(generatedResponse.Body.Bytes(), &generated); err != nil || generated.OrderAdjustment.OrderDraft == nil || generated.ScheduleDuration.ScheduleDraft == nil || generated.ScheduleDuration.ScheduleDraft.TargetMode != "create_new" {
		t.Fatalf("decode generated business drafts: err=%v body=%s", err, generatedResponse.Body.String())
	}
	orderDraft := generated.OrderAdjustment.OrderDraft
	wrongAck := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts/"+orderDraft.ID+"/apply", token, "business-http-wrong-ack", `{"expected_draft_revision":1,"decision":"apply_order_adjustment","acknowledgement":{"version":"order-adjustment-v1","effects":[]}}`)
	requirePlanningError(t, wrongAck, http.StatusBadRequest, httpapi.CodeValidationFailed)

	if _, err := scope.Update(t.Context(), "orders", "price = $2", "id = $3", 269000, createdOrder.ID); err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := json.Marshal(orderDraft.RequiredAcknowledgement)
	if err != nil {
		t.Fatal(err)
	}
	staleApply := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts/"+orderDraft.ID+"/apply", token, "business-http-stale-apply", `{"expected_draft_revision":1,"decision":"apply_order_adjustment","acknowledgement":`+string(acknowledgement)+`}`)
	requirePlanningError(t, staleApply, http.StatusConflict, "stale_business_draft")
	var stale struct {
		Error struct {
			Details struct {
				DraftID  string `json:"draft_id"`
				Kind     string `json:"kind"`
				Status   string `json:"status"`
				Reason   string `json:"stale_reason"`
				Revision int64  `json:"revision"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(staleApply.Body.Bytes(), &stale); err != nil || stale.Error.Details.DraftID != orderDraft.ID || stale.Error.Details.Reason != "order_target_changed" || stale.Error.Details.Status != "stale" {
		t.Fatalf("typed stale details missing: err=%v body=%s", err, staleApply.Body.String())
	}

	scheduleDraft := generated.ScheduleDuration.ScheduleDraft
	createNewApply := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts/"+scheduleDraft.ID+"/apply", token, "business-http-create-new-apply", `{"expected_draft_revision":1,"decision":"apply_schedule_duration"}`)
	requirePlanningError(t, createNewApply, http.StatusConflict, "business_draft_requires_schedule_create")

	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(t.Context(), "business-http-other", hash); err != nil {
		t.Fatal(err)
	}
	otherToken := issueToken(t, issuer, "business-http-other")
	crossDetail := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/shoot-plans/"+plan.ID, otherToken, "", "")
	requirePlanningError(t, crossDetail, http.StatusNotFound, httpapi.CodeNotFound)
	crossDecision := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans/"+plan.ID+"/business-drafts/"+orderDraft.ID+"/apply", otherToken, "business-http-cross-decision", `{"expected_draft_revision":1,"decision":"dismiss"}`)
	requirePlanningError(t, crossDecision, http.StatusNotFound, httpapi.CodeNotFound)
	shareBusiness := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shared/plans/not-a-token/business-drafts", "", "business-http-share", `{}`)
	requirePlanningError(t, shareBusiness, http.StatusNotFound, httpapi.CodeNotFound)
}

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
