package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGetShootPlanAssignmentRemindersEmptyAndNotFound(t *testing.T) {
	router, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)

	missing := authenticatedRequest(t, router, http.MethodGet,
		"/api/v1/shoot-plans/plan_missing/assignment-reminders", token, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing plan: want 404, got %d body=%s", missing.Code, missing.Body.String())
	}

	create := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/shoot-plans", token, "s5-create-plan",
		`{"title":"认领提醒空态","subject":"角色"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create plan: want 201, got %d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode created plan: %v body=%s", err, create.Body.String())
	}

	rec := authenticatedRequest(t, router, http.MethodGet,
		"/api/v1/shoot-plans/"+created.ID+"/assignment-reminders", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty assignment reminders: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		PlanID                 string `json:"plan_id"`
		UnscheduledSourceCount int    `json:"unscheduled_source_count"`
		Groups                 []any  `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode view: %v body=%s", err, rec.Body.String())
	}
	if body.PlanID != created.ID {
		t.Fatalf("plan_id=%q want %q", body.PlanID, created.ID)
	}
	if body.UnscheduledSourceCount != 0 {
		t.Fatalf("unscheduled_source_count=%d want 0", body.UnscheduledSourceCount)
	}
	if body.Groups == nil || len(body.Groups) != 0 {
		t.Fatalf("groups must be empty array, got %#v", body.Groups)
	}

	unauthorized := authenticatedRequest(t, router, http.MethodGet,
		"/api/v1/shoot-plans/"+created.ID+"/assignment-reminders", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: want 401, got %d", unauthorized.Code)
	}
}
