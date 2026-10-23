package httpapi_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

const (
	mcpBase     = "http://localhost:8080"
	mcpVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	mcpCallback = "http://127.0.0.1:3000/callback"
)

type mcpFixture struct {
	server   *httptest.Server
	web      string // the photographer's web token
	otherWeb string // another account's web token
	canvasID string
	st       *store.Store
}

// newMCPFixture serves the real router with the external agent endpoint on,
// for one active account owning one canvas, and a second account owning another.
func newMCPFixture(t *testing.T, enabled bool) mcpFixture {
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
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ($1,'h','active'),('mcp-other','h','active'),('mcp-platform','h','active')`, testAcctID); err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokenIssuer(testSecret)
	models, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := versionedfs.NewLocal(t.TempDir(), func(string, string, int, time.Time) versionedfs.PartAuthorization {
		return versionedfs.PartAuthorization{}
	})
	if err != nil {
		t.Fatal(err)
	}
	skills, err := creativeskill.NewService(objects, s.ScopeFor(auth.AccountContext{AccountID: "mcp-platform"}))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := creativeagent.NewService(gateway, models, skills)
	if err != nil {
		t.Fatal(err)
	}
	var authServer *oauth.Service
	if enabled {
		if authServer, err = oauth.New(oauth.Config{BaseURL: mcpBase, Secret: testSecret, Store: s}); err != nil {
			t.Fatal(err)
		}
	}
	router := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: s, ScopeFactory: s,
		Auth: auth.NewService(s, tokens), PublicBaseURL: mcpBase, CreativeAgent: agent, OAuth: authServer})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	canvas := func(account, name string) string {
		receipt, err := creativecanvas.CreateProject(t.Context(), s.ScopeFor(auth.AccountContext{AccountID: account}),
			creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"name":"` + name + `"}`)})
		if err != nil {
			t.Fatal(err)
		}
		var project creativecanvas.ProjectResult
		if err := json.Unmarshal(receipt.Outcome.Response, &project); err != nil {
			t.Fatal(err)
		}
		return project.CanvasID
	}
	mine := canvas(testAcctID, "九月，向海而行")
	canvas("mcp-other", "别人的项目")
	return mcpFixture{server: server, web: issueToken(t, tokens, testAcctID), otherWeb: issueToken(t, tokens, "mcp-other"), canvasID: mine, st: s}
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (f mcpFixture) do(t *testing.T, method, path, bearer, contentType, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

// connect runs the whole authorization a command-line host goes through and
// returns its tokens.
func (f mcpFixture) connect(t *testing.T) oauth.TokenResponse {
	t.Helper()
	return f.connectAs(t, f.web)
}

// connectAs authorizes as the account signed in with web.
func (f mcpFixture) connectAs(t *testing.T, web string) oauth.TokenResponse {
	t.Helper()
	reg := f.do(t, http.MethodPost, "/oauth/register", "", "application/json", `{"client_name":"Claude Code (creative)","redirect_uris":["`+mcpCallback+`"]}`)
	if reg.StatusCode != http.StatusCreated {
		t.Fatalf("register status %d", reg.StatusCode)
	}
	var client oauth.RegistrationResponse
	decodeBody(t, reg, &client)
	sum := sha256.Sum256([]byte(mcpVerifier))
	q := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"http://127.0.0.1:61234/callback"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {"s1"}, "resource": {mcpBase + "/mcp"}}
	authz := f.do(t, http.MethodGet, "/oauth/authorize?"+q.Encode(), "", "", "")
	consent, err := url.Parse(authz.Header.Get("Location"))
	if authz.StatusCode != http.StatusFound || err != nil || consent.Path != "/oauth/consent" || consent.Query().Get("request") == "" {
		t.Fatalf("authorize %d %s", authz.StatusCode, authz.Header.Get("Location"))
	}
	body, _ := json.Marshal(map[string]string{"request": consent.Query().Get("request"), "decision": "allow"})
	decision := f.do(t, http.MethodPost, "/api/v1/oauth/authorize/decision", web, "application/json", string(body))
	if decision.StatusCode != http.StatusOK {
		t.Fatalf("decision status %d", decision.StatusCode)
	}
	var to struct {
		RedirectTo string `json:"redirect_to"`
	}
	decodeBody(t, decision, &to)
	back, err := url.Parse(to.RedirectTo)
	if err != nil || back.Host != "127.0.0.1:61234" || back.Query().Get("state") != "s1" || back.Query().Get("iss") != mcpBase {
		t.Fatalf("redirect %s", to.RedirectTo)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "code": {back.Query().Get("code")},
		"redirect_uri": {"http://127.0.0.1:61234/callback"}, "code_verifier": {mcpVerifier}, "resource": {mcpBase + "/mcp"}}
	tok := f.do(t, http.MethodPost, "/oauth/token", "", "application/x-www-form-urlencoded", form.Encode())
	if tok.StatusCode != http.StatusOK || tok.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("token status %d", tok.StatusCode)
	}
	var tokens oauth.TokenResponse
	decodeBody(t, tok, &tokens)
	return tokens
}

type bearer struct{ token string }

// swappable is a host's token store: re-authorizing replaces the token while
// the MCP session it already holds stays open.
type swappable struct {
	mu    sync.Mutex
	token string
}

func (b *swappable) set(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.token = token
}

func (b *swappable) RoundTrip(r *http.Request) (*http.Response, error) {
	b.mu.Lock()
	token := b.token
	b.mu.Unlock()
	return bearer{token}.RoundTrip(r)
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (f mcpFixture) session(t *testing.T, token string) (*mcp.ClientSession, error) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil)
	return client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}, MaxRetries: -1}, nil)
}

func toolText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("empty tool result")
	}
	text, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first content is %T", r.Content[0])
	}
	return text.Text
}

func TestMCPEndpointEndToEnd(t *testing.T) {
	f := newMCPFixture(t, true)

	// An anonymous request learns where to authorize.
	anon := f.do(t, http.MethodPost, "/mcp", "", "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if anon.StatusCode != http.StatusUnauthorized || !strings.Contains(anon.Header.Get("WWW-Authenticate"), `resource_metadata="`+mcpBase+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("anonymous %d %q", anon.StatusCode, anon.Header.Get("WWW-Authenticate"))
	}
	var resource oauth.ProtectedResourceMetadata
	decodeBody(t, f.do(t, http.MethodGet, "/.well-known/oauth-protected-resource/mcp", "", "", ""), &resource)
	if resource.Resource != mcpBase+"/mcp" || resource.AuthorizationServers[0] != mcpBase {
		t.Fatalf("resource metadata %+v", resource)
	}
	var server oauth.AuthorizationServerMetadata
	decodeBody(t, f.do(t, http.MethodGet, "/.well-known/oauth-authorization-server", "", "", ""), &server)
	if server.TokenEndpoint != mcpBase+"/oauth/token" || !server.ClientIDMetadataDocumentSupported {
		t.Fatalf("server metadata %+v", server)
	}

	tokens := f.connect(t)
	session, err := f.session(t, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if !strings.Contains(session.InitializeResult().Instructions, "get_capabilities") {
		t.Fatal("instructions not sent on initialize")
	}
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		if tool.Title == "" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Meta["ui"] != nil {
			t.Errorf("tool %s: %+v", tool.Name, tool)
		}
	}
	if strings.Join(names, ",") != "get_capabilities,list_canvases,read_asset,read_canvas,read_library,search_assets" {
		t.Fatalf("tools %v", names)
	}

	canvases, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_canvases", Arguments: map[string]any{}})
	if err != nil || canvases.IsError {
		t.Fatalf("list_canvases %v %+v", err, canvases)
	}
	text := toolText(t, canvases)
	if !strings.HasPrefix(text, "最近编辑的 1 张画布：") || !strings.Contains(text, "("+mcpBase+"/creative/canvases/"+f.canvasID+")") || canvases.StructuredContent == nil {
		t.Fatalf("list_canvases text %s", text)
	}
	if strings.Contains(text, "别人的项目") {
		t.Fatal("another account's canvas listed")
	}
	other, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "read_canvas", Arguments: map[string]any{"canvas_id": "cccv_other", "offset": 0, "limit": 10}})
	if err != nil || !other.IsError || !strings.Contains(toolText(t, other), `"code":"not_found"`) {
		t.Fatalf("read_canvas of a missing canvas %v %+v", err, other)
	}
	caps, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_capabilities", Arguments: map[string]any{}})
	if err != nil || caps.IsError || !strings.Contains(toolText(t, caps), "署名 Claude Code") {
		t.Fatalf("get_capabilities %v %+v", err, caps)
	}

	// Tokens of the two doors never open the other one.
	if r := f.do(t, http.MethodPost, "/mcp", f.web, "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("web token on /mcp: %d", r.StatusCode)
	}
	if r := f.do(t, http.MethodGet, "/api/v1/me", tokens.AccessToken, "", ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mcp token on the web API: %d", r.StatusCode)
	}

	// The workspace sees the connection, the photographer disconnects it.
	var list struct {
		McpURL string `json:"mcp_url"`
		Items  []struct {
			ID          string     `json:"id"`
			DisplayName string     `json:"display_name"`
			HostKind    string     `json:"host_kind"`
			Active      bool       `json:"active"`
			LastUsedAt  *time.Time `json:"last_used_at"`
			RevokedAt   *time.Time `json:"revoked_at"`
		} `json:"items"`
	}
	decodeBody(t, f.do(t, http.MethodGet, "/api/v1/creative/agent-connections?active=true", f.web, "", ""), &list)
	if list.McpURL != mcpBase+"/mcp" || len(list.Items) != 1 || !list.Items[0].Active || list.Items[0].DisplayName != "Claude Code" || list.Items[0].HostKind != "claude_code" {
		t.Fatalf("connections %+v", list)
	}
	if r := f.do(t, http.MethodGet, "/api/v1/creative/agent-connections?active=maybe", f.web, "", ""); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad query %d", r.StatusCode)
	}
	if r := f.do(t, http.MethodPost, "/api/v1/creative/agent-connections/cxcon_missing/revoke", f.web, "", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke missing %d", r.StatusCode)
	}
	revoke := f.do(t, http.MethodPost, "/api/v1/creative/agent-connections/"+list.Items[0].ID+"/revoke", f.web, "", "")
	if revoke.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d", revoke.StatusCode)
	}
	if r := f.do(t, http.MethodPost, "/mcp", tokens.AccessToken, "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token on /mcp: %d", r.StatusCode)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {"x"}, "refresh_token": {tokens.RefreshToken}}
	if r := f.do(t, http.MethodPost, "/oauth/token", "", "application/x-www-form-urlencoded", form.Encode()); r.StatusCode != http.StatusUnauthorized && r.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh after revoke %d", r.StatusCode)
	}
	var all struct {
		Items []struct {
			RevokedAt *time.Time `json:"revoked_at"`
		} `json:"items"`
	}
	decodeBody(t, f.do(t, http.MethodGet, "/api/v1/creative/agent-connections?include_revoked=true", f.web, "", ""), &all)
	if len(all.Items) != 1 || all.Items[0].RevokedAt == nil {
		t.Fatalf("revoked list %+v", all)
	}
}

func TestMCPRefusesALocalErrorRatherThanRedirecting(t *testing.T) {
	f := newMCPFixture(t, true)
	q := url.Values{"response_type": {"code"}, "client_id": {"oclient_missing"}, "redirect_uri": {"https://evil.example/cb"}}
	r := f.do(t, http.MethodGet, "/oauth/authorize?"+q.Encode(), "", "", "")
	if r.StatusCode != http.StatusFound || r.Header.Get("Location") != "/oauth/consent?error=invalid_client" {
		t.Fatalf("authorize %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	bad := f.do(t, http.MethodPost, "/api/v1/oauth/authorize/decision", f.web, "application/json", `{"request":"v1.x.y","decision":"allow"}`)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged request %d", bad.StatusCode)
	}
}

func TestMCPSwitchedOffIsNotThere(t *testing.T) {
	f := newMCPFixture(t, false)
	for _, path := range []string{"/mcp", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/oauth/authorize", "/oauth/token"} {
		if r := f.do(t, http.MethodGet, path, "", "", ""); r.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", path, r.StatusCode)
		}
	}
	if r := f.do(t, http.MethodGet, "/api/v1/creative/agent-connections", f.web, "", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("connections with MCP off: %d", r.StatusCode)
	}
}

// A host that signs in again gets a new connection but may keep the MCP
// session it opened before (Claude Code does). The session belongs to the
// account, so the next call works and is attributed to the new connection;
// another account presenting the session is still refused.
func TestMCPSessionSurvivesReauthorization(t *testing.T) {
	f := newMCPFixture(t, true)
	tokens := &swappable{token: f.connect(t).AccessToken}
	client := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: tokens}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	tokens.set(f.connect(t).AccessToken)
	got, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_capabilities", Arguments: map[string]any{}})
	if err != nil || got.IsError {
		t.Fatalf("call after signing in again: %v %+v", err, got)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decodeBody(t, f.do(t, http.MethodGet, "/api/v1/creative/agent-connections", f.web, "", ""), &list)
	var caps struct {
		Connection struct {
			ID string `json:"id"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(toolText(t, got), "\n\n", 2)[1]), &caps); err != nil || len(list.Items) != 2 || caps.Connection.ID != list.Items[0].ID {
		t.Fatalf("attributed to %q, connections %+v (%v)", caps.Connection.ID, list.Items, err)
	}

	other := f.connectAs(t, f.otherWeb)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.server.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+other.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", session.ID())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("another account on this session: %d", resp.StatusCode)
	}
}
