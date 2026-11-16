package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

type idleProvider struct{}

func (idleProvider) Key() llmgateway.ProviderKey { return llmgateway.ProviderOpenAICompatible }
func (idleProvider) Invoke(context.Context, llmgateway.ProviderRequest) (llmgateway.Result, error) {
	return llmgateway.Result{}, context.Canceled
}

type egressFixture struct {
	server       *httptest.Server
	db           *sql.DB
	web, other   string
	conversation string
	model        string
}

// newEgressFixture serves the real router with a model this deployment can
// reach, so a run refused here is refused by the consent and nothing before it.
func newEgressFixture(t *testing.T) egressFixture {
	t.Helper()
	url := storetest.NewURL(t)
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ($1,'h','active'),('egress-other','h','active'),('egress-platform','h','active')`, testAcctID); err != nil {
		t.Fatal(err)
	}
	// One provider's credential, served by a provider that is never called:
	// no worker runs here, so a run is only admitted and queued.
	credential := func(env string) (string, bool) { return "test-credential", env == "DEEPSEEK_API_KEY" }
	models, err := llmgateway.DefaultCatalog(credential)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, Credential: credential,
		Providers:     map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: idleProvider{}},
		DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := versionedfs.NewLocal(t.TempDir(), func(string, string, int, time.Time) versionedfs.PartAuthorization {
		return versionedfs.PartAuthorization{}
	})
	if err != nil {
		t.Fatal(err)
	}
	skills, err := creativeskill.NewService(objects, s.ScopeFor(auth.AccountContext{AccountID: "egress-platform"}))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := creativeagent.NewService(gateway, models, skills)
	if err != nil {
		t.Fatal(err)
	}
	// A queue for the run to be put on; nothing works it here.
	if err := s.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	queue, err := s.NewJobRuntime(agent.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	agent.SetRuntime(queue)
	tokens := auth.NewTokenIssuer(testSecret)
	router := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: s, ScopeFactory: s,
		Auth: auth.NewService(s, tokens), CreativeAgent: agent})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	receipt, err := creativecanvas.CreateProject(t.Context(), scope,
		creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"name":"九月，向海而行"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var project creativecanvas.ProjectResult
	if err := json.Unmarshal(receipt.Outcome.Response, &project); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"canvas_id": project.CanvasID, "title": "参考整理"})
	receipt, err = agent.CreateConversation(t.Context(), scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	var conversation creativeagent.Conversation
	if err := json.Unmarshal(receipt.Outcome.Response, &conversation); err != nil {
		t.Fatal(err)
	}
	var model string
	for _, m := range models.List() {
		if m.Available {
			model = m.ModelKey
			break
		}
	}
	if model == "" {
		t.Fatal("no model is reachable in the test catalog")
	}
	return egressFixture{server: server, db: db, web: issueToken(t, tokens, testAcctID), other: issueToken(t, tokens, "egress-other"),
		conversation: conversation.ID, model: model}
}

// call sends a request and decodes its JSON answer; a body that is not JSON
// fails the test rather than reading as an empty object.
func (f egressFixture) call(t *testing.T, method, path, bearer string, payload any) (int, map[string]any) {
	t.Helper()
	status, raw := f.do(t, method, path, bearer, payload)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s %s answered %d with a body that is not JSON: %q", method, path, status, raw)
	}
	return status, out
}

func (f egressFixture) do(t *testing.T, method, path, bearer string, payload any) (int, []byte) {
	t.Helper()
	var body io.Reader
	key := uuid.NewString()
	if payload != nil {
		raw, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC().Format(time.RFC3339), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+"/api/v1"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func egressError(body map[string]any) (code, message string) {
	e, _ := body["error"].(map[string]any)
	code, _ = e["code"].(string)
	message, _ = e["message"].(string)
	return code, message
}

// The consent routes are wired to the service, and a run is gated by the
// account's consent with no authorisation in the request. Statuses alone
// would not tell an unregistered route from a refusal, so the service's own
// codes and messages are asserted.
func TestTheAccountConsentGatesARunOverHTTP(t *testing.T) {
	f := newEgressFixture(t)
	run := map[string]any{"model_key": f.model, "instruction": map[string]any{"schema_version": 1,
		"instruction_segments": []map[string]any{{"type": "text", "text": "帮我理一理。"}}}}

	status, state := f.call(t, "GET", "/creative/egress-consent", f.web, nil)
	version, _ := state["policy_version"].(string)
	if status != 200 || state["effective"] != false || state["revision"] != "0" || state["consent"] != nil || version == "" {
		t.Fatalf("never answered: %d %v", status, state)
	}

	status, body := f.call(t, "POST", "/creative/conversations/"+f.conversation+"/runs", f.web, run)
	if code, message := egressError(body); status != 403 || code != "creative_egress_required" || !strings.Contains(message, "发送前说明") {
		t.Fatalf("a run without consent: %d %v", status, body)
	}
	var runs, holds int
	if err := f.db.QueryRow(`SELECT (SELECT count(*) FROM creative_agent_runs), (SELECT count(*) FROM llm_usage_reservations)`).Scan(&runs, &holds); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || holds != 0 {
		t.Fatalf("a refused run left runs=%d holds=%d", runs, holds)
	}

	status, body = f.call(t, "POST", "/creative/egress-consent", f.web, map[string]any{"policy_version": "egress-2000-01-1", "library_access": true, "expected_revision": "0"})
	if code, _ := egressError(body); status != 409 || code != "creative_egress_policy_changed" {
		t.Fatalf("an outdated notice: %d %v", status, body)
	}
	status, body = f.call(t, "POST", "/creative/egress-consent", f.web, map[string]any{"policy_version": version, "library_access": true, "expected_revision": "3"})
	if code, _ := egressError(body); status != 409 || code != "creative_revision_conflict" {
		t.Fatalf("an agreement on a state never shown: %d %v", status, body)
	}
	status, state = f.call(t, "POST", "/creative/egress-consent", f.web, map[string]any{"policy_version": version, "library_access": true, "expected_revision": "0"})
	consent, _ := state["consent"].(map[string]any)
	if status != 200 || state["effective"] != true || consent == nil || consent["library_access"] != true {
		t.Fatalf("agreeing: %d %v", status, state)
	}
	status, state = f.call(t, "GET", "/creative/egress-consent", f.other, nil)
	if status != 200 || state["effective"] != false || state["consent"] != nil {
		t.Fatalf("another account read this one's consent: %d %v", status, state)
	}

	// The request no longer carries an authorisation; one that does is malformed.
	withID := map[string]any{"model_key": f.model, "egress_consent_id": consent["id"], "instruction": run["instruction"]}
	if status, body = f.call(t, "POST", "/creative/conversations/"+f.conversation+"/runs", f.web, withID); status != 400 {
		t.Fatalf("a run naming an authorisation: %d %v", status, body)
	}
	status, body = f.call(t, "POST", "/creative/conversations/"+f.conversation+"/runs", f.web, run)
	if status != 202 || body["egress_consent_id"] != consent["id"] {
		t.Fatalf("a run under the consent: %d %v", status, body)
	}

	status, state = f.call(t, "POST", "/creative/egress-consent/revoke", f.web, map[string]any{})
	if status != 200 || state["effective"] != false || state["consent"] != nil || state["revoked_at"] == nil {
		t.Fatalf("withdrawing: %d %v", status, state)
	}

	// The per-conversation routes are gone.
	if status, raw := f.do(t, "POST", "/creative/conversations/"+f.conversation+"/egress-consents", f.web, map[string]any{}); status != 404 {
		t.Fatalf("the old grant route answered: %d %s", status, raw)
	}
}
