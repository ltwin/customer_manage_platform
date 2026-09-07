package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cw "github.com/samson/customer-manage-platform/backend/internal/creativeworkspace"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestCreativeWorkspaceHTTPAndLegacyBarrier(t *testing.T) {
	router, db, tokens := newCustomerAPIRouter(t)
	token := issueToken(t, tokens, testAcctID)
	req := func(method, path, key, body string) *httptest.ResponseRecorder {
		return shootPlanningRequest(t, router, method, "/api/v1"+path, token, key, body)
	}
	requirePlanningError(t, req("POST", "/creative-workspaces", "create-space-1", "{}"), 409, "pilot_required")
	pre := req("GET", "/creative-pilot/preflight", "", "")
	if pre.Code != 200 {
		t.Fatal(pre.Body.String())
	}
	sc := db.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	n, err := sc.Count(t.Context(), "creative_pilot_accounts", "")
	if err != nil || n != 0 {
		t.Fatalf("preflight wrote capability: %d %v", n, err)
	}
	enrolled := req("POST", "/creative-pilot/enroll", "", "{}")
	if enrolled.Code != 200 {
		t.Fatalf("enroll %d %s", enrolled.Code, enrolled.Body)
	}
	for _, tt := range []struct{ method, path, body string }{
		{"POST", "/shoot-plans", `{"title":"旧计划","subject":"角色"}`},
		{"PATCH", "/shoot-plans/old", `{"expected_revision":1,"operation":"link_customer","customer_id":"nope"}`},
		{"POST", "/shoot-plans/old/business-drafts", `{"expected_plan_revision":1,"expected_business_facts_revision":1,"draft_kinds":["order_adjustment"]}`},
		{"PATCH", "/settings", `{"planning_business_rules":null}`},
	} {
		requirePlanningError(t, req(tt.method, tt.path, "legacy-blocked-key", tt.body), 409, "legacy_read_only")
	}
	if v := req("PATCH", "/settings", "", `{"birthday_lead_days":4}`); v.Code != 200 {
		t.Fatalf("unrelated settings blocked: %d %s", v.Code, v.Body)
	}
	created := req("POST", "/creative-workspaces", "create-space-2", `{"name":"窗边人像"}`)
	if created.Code != 201 {
		t.Fatalf("create %d %s", created.Code, created.Body)
	}
	again := req("POST", "/creative-workspaces", "create-space-2", `{"name":"窗边人像"}`)
	if again.Code != 201 || again.Body.String() != created.Body.String() {
		t.Fatal("create replay changed response")
	}
	requirePlanningError(t, req("POST", "/creative-workspaces", "create-space-2", `{"name":"changed"}`), 409, "idempotency_conflict")
	var ws cw.Workspace
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	path := "/creative-workspaces/" + ws.ID
	imported := req("POST", path+"/cards", "import-space-1", `{"raw_text":"第一句\nhttps://example.com\n第三句","asset_ids":[]}`)
	if imported.Code != 201 {
		t.Fatalf("import %d %s", imported.Code, imported.Body)
	}
	var cards cw.ImportCardsResult
	if err := json.Unmarshal(imported.Body.Bytes(), &cards); err != nil {
		t.Fatal(err)
	}
	retry := req("POST", path+"/cards", "import-space-1", `{"raw_text":"第一句\nhttps://example.com\n第三句","asset_ids":[]}`)
	if retry.Code != 201 || retry.Body.String() != imported.Body.String() {
		t.Fatal("import replay duplicated")
	}
	shoot := req("POST", path+"/shoot-items", "create-shots-1", `{"card_ids":["`+cards.Cards[0].ID+`"],"merge":false}`)
	if shoot.Code != 201 {
		t.Fatalf("shot %d %s", shoot.Code, shoot.Body)
	}
	var items []cw.ShootItem
	if err := json.Unmarshal(shoot.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	done := req("PATCH", path, "", `{"operation":"update_shoot_item","id":"`+items[0].ID+`","expected_revision":1,"result":"done"}`)
	if done.Code != 200 || !strings.Contains(done.Body.String(), `"result":"done"`) {
		t.Fatalf("done %d %s", done.Code, done.Body)
	}
	requirePlanningError(t, req("PATCH", path, "", `{"operation":"update_shoot_item","id":"`+items[0].ID+`","expected_revision":1,"result":"skipped"}`), 409, "workspace_revision_conflict")
	requirePlanningError(t, req("POST", path+"/observations", "", `{"event_id":"event-12345","session_id":"session-12345","kind":"live_open","client_at":"2026-09-07T00:00:00Z","live":true}`), 400, "validation_failed")
	event := `{"event_id":"event-12345","session_id":"session-12345","kind":"live_unverified","client_at":"2026-09-07T00:00:00Z"}`
	for range 2 {
		v := req("POST", path+"/observations", "", event)
		if v.Code != 204 {
			t.Fatalf("observe %d %s", v.Code, v.Body)
		}
	}
	n, err = sc.Count(t.Context(), "creative_observation_events", "")
	if err != nil || n != 1 {
		t.Fatalf("observation replay: %d %v", n, err)
	}
	if err := db.CreateAccount(t.Context(), "creative-other", "hash"); err != nil {
		t.Fatal(err)
	}
	other := shootPlanningRequest(t, router, "GET", "/api/v1"+path, issueToken(t, tokens, "creative-other"), "", "")
	requirePlanningError(t, other, 404, "not_found")
	denied := shootPlanningRequest(t, router, "POST", "/api/v1/creative-pilot/enroll", issueToken(t, tokens, "creative-other"), "", "{}")
	requirePlanningError(t, denied, 403, "forbidden")
	stopped := req("POST", "/creative-pilot/stop", "", "")
	if stopped.Code != 200 {
		t.Fatal(stopped.Body.String())
	}
	requirePlanningError(t, req("POST", path+"/cards", "stopped-import-1", `{"raw_text":"不会写入","asset_ids":[]}`), 409, "pilot_required")
	if v := req("GET", path, "", ""); v.Code != 200 {
		t.Fatal(v.Body.String())
	}
}

// Opt-in browser fixture. Uses the same shared, port-ready test container and
// an isolated database; no production account or credentials are involved.
func TestCreativeBrowserPreview(t *testing.T) {
	dir := os.Getenv("CREATIVE_BROWSER_FIXTURE")
	if dir == "" {
		t.Skip("set CREATIVE_BROWSER_FIXTURE for browser QA")
	}
	router, db, tokens := newCustomerAPIRouter(t)
	sc := db.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if err := sc.Insert(t.Context(), "customers", []string{"id", "display_name", "channel"}, "cus_preview", "阿宁", "other"); err != nil {
		t.Fatal(err)
	}
	if err := sc.Insert(t.Context(), "orders", []string{"id", "customer_id", "title"}, "ord_preview", "cus_preview", "窗边人像"); err != nil {
		t.Fatal(err)
	}
	fixture := struct {
		Token string `json:"token"`
	}{Token: issueToken(t, tokens, testAcctID)}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", router)
	end := make(chan struct{}, 1)
	mux.HandleFunc("/__end", func(w http.ResponseWriter, r *http.Request) {
		select {
		case end <- struct{}{}:
		default:
		}
		w.WriteHeader(204)
	})
	server := &http.Server{Addr: "127.0.0.1:18089", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	select {
	case <-end:
	case err := <-errors:
		if err != http.ErrServerClosed {
			t.Fatal(err)
		}
	case <-t.Context().Done():
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
