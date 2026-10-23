package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/oauth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

const (
	base     = "https://crm.example.com"
	resource = base + "/mcp"
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	callback = "http://127.0.0.1:3000/callback"
)

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time             { return c.at }
func (c *clock) advance(d time.Duration)    { c.at = c.at.Add(d) }
func (c *clock) set(at time.Time) time.Time { c.at = at; return at }

type fixture struct {
	svc   *oauth.Service
	st    *store.Store
	clock *clock
	url   string
}

func setup(t *testing.T, client *http.Client) fixture {
	t.Helper()
	name, url := storetest.NewDatabase(t)
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	storetest.ExecSQL(t, name, `INSERT INTO accounts (id, status, password_hash) VALUES ('acct', 'active', 'h'), ('acct2', 'active', 'h');
INSERT INTO accounts (id, status, password_hash) VALUES ('frozen', 'pending_verification', 'h')`)
	c := &clock{}
	c.set(time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC))
	svc, err := oauth.New(oauth.Config{BaseURL: base, Secret: strings.Repeat("s", 32), Store: st, HTTPClient: client, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{svc: svc, st: st, clock: c, url: url}
}

func (f fixture) register(t *testing.T, body string) oauth.RegistrationResponse {
	t.Helper()
	resp, err := f.svc.Register(context.Background(), "203.0.113.9", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func authorizeQuery(clientID, redirect string) url.Values {
	return url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "state": {"st-1"}, "resource": {resource}}
}

// grant runs authorize and "allow", returning the code from the callback.
func (f fixture) grant(t *testing.T, account, clientID, redirect string) string {
	t.Helper()
	ctx := context.Background()
	got, err := f.svc.Authorize(ctx, authorizeQuery(clientID, redirect))
	if err != nil || got.Request == "" {
		t.Fatalf("authorize: %+v %v", got, err)
	}
	to, err := f.svc.Decide(ctx, account, got.Request, true)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(to)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "st-1" || q.Get("iss") != base || q.Get("code") == "" {
		t.Fatalf("callback %s", to)
	}
	return q.Get("code")
}

func oauthCode(err error) string {
	var e *oauth.Error
	if errors.As(err, &e) {
		return e.Code
	}
	if err == nil {
		return ""
	}
	return "go:" + err.Error()
}

func TestAuthorizationCodeFlowRotationAndReplay(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Claude Code (creative)","redirect_uris":["`+callback+`"],"software_id":"x"}`)
	if client.ClientSecret != "" || client.AuthMethod != "none" {
		t.Fatalf("public client got %+v", client)
	}
	// A native client may come back on another loopback port.
	code := f.grant(t, "acct", client.ClientID, "http://127.0.0.1:51234/callback")
	tokens, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code,
		RedirectURI: "http://127.0.0.1:51234/callback", CodeVerifier: verifier, Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	if tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 || !strings.HasPrefix(tokens.RefreshToken, "cxr_") || tokens.Scope != "creative" {
		t.Fatalf("tokens %+v", tokens)
	}
	conn, exp, err := f.svc.VerifyAccess(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if conn.AccountID != "acct" || conn.HostKind != "claude_code" || conn.DisplayName != "Claude Code" || !exp.Equal(f.clock.at.Add(time.Hour)) {
		t.Fatalf("connection %+v exp %v", conn, exp)
	}
	if conn.LastUsedAt == nil || !conn.Active(f.clock.at) {
		t.Fatal("first call did not record use")
	}

	// The access token expires; refresh rotates the chain.
	f.clock.advance(61 * time.Minute)
	if _, _, err := f.svc.VerifyAccess(ctx, tokens.AccessToken); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatal("expired access token accepted", err)
	}
	next, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "refresh_token", ClientID: client.ClientID, RefreshToken: tokens.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if next.RefreshToken == tokens.RefreshToken {
		t.Fatal("refresh token not rotated")
	}
	if _, _, err := f.svc.VerifyAccess(ctx, next.AccessToken); err != nil {
		t.Fatal(err)
	}

	// Replaying the rotated token disconnects the whole connection.
	if _, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "refresh_token", ClientID: client.ClientID, RefreshToken: tokens.RefreshToken}); oauthCode(err) != "invalid_grant" {
		t.Fatal("rotated refresh token accepted", err)
	}
	if _, _, err := f.svc.VerifyAccess(ctx, next.AccessToken); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatal("access token survived a refresh replay", err)
	}
	if _, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "refresh_token", ClientID: client.ClientID, RefreshToken: next.RefreshToken}); oauthCode(err) != "invalid_grant" {
		t.Fatal("current refresh token survived a replay", err)
	}
	conns, err := f.svc.ListConnections(ctx, "acct", true)
	if err != nil || len(conns) != 1 || conns[0].RevokedReason != "refresh_replay" {
		t.Fatalf("connections %+v %v", conns, err)
	}
}

func TestCodeIsOneTimeAndBound(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	other := f.register(t, `{"client_name":"Other","redirect_uris":["`+callback+`"]}`)
	exchange := func(code string, mutate func(*oauth.TokenRequest)) error {
		req := oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: callback, CodeVerifier: verifier, Resource: resource}
		if mutate != nil {
			mutate(&req)
		}
		_, err := f.svc.Token(ctx, req)
		return err
	}
	for _, c := range []struct {
		name   string
		mutate func(*oauth.TokenRequest)
		want   string
	}{
		{"wrong verifier", func(r *oauth.TokenRequest) { r.CodeVerifier = strings.Repeat("a", 43) }, "invalid_grant"},
		{"wrong callback", func(r *oauth.TokenRequest) { r.RedirectURI = "http://127.0.0.1:3000/other" }, "invalid_grant"},
		{"other client", func(r *oauth.TokenRequest) { r.ClientID = other.ClientID }, "invalid_client"},
		{"other resource", func(r *oauth.TokenRequest) { r.Resource = "https://crm.example.com/api" }, "invalid_target"},
		{"unknown client", func(r *oauth.TokenRequest) { r.ClientID = "oclient_missing" }, "invalid_client"},
		{"unknown code", func(r *oauth.TokenRequest) { r.Code = "nope" }, "invalid_grant"},
		{"password grant", func(r *oauth.TokenRequest) { r.GrantType = "password" }, "unsupported_grant_type"},
	} {
		code := f.grant(t, "acct", client.ClientID, callback)
		if got := oauthCode(exchange(code, c.mutate)); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}

	code := f.grant(t, "acct", client.ClientID, callback)
	f.clock.advance(61 * time.Second)
	if got := oauthCode(exchange(code, nil)); got != "invalid_grant" {
		t.Fatal("expired code accepted", got)
	}

	code = f.grant(t, "acct", client.ClientID, callback)
	if err := exchange(code, nil); err != nil {
		t.Fatal(err)
	}
	if got := oauthCode(exchange(code, nil)); got != "invalid_grant" {
		t.Fatal("code used twice", got)
	}
	all, err := f.svc.ListConnections(ctx, "acct", true)
	if err != nil {
		t.Fatal(err)
	}
	replayed := 0
	for _, c := range all {
		if c.RevokedReason == "refresh_replay" {
			replayed++
		}
	}
	if replayed != 1 {
		t.Fatalf("replayed code disconnected %d connections", replayed)
	}
}

func TestAuthorizeRefusals(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	local := func(q url.Values) string {
		got, err := f.svc.Authorize(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if got.Redirect != "" || got.Request != "" {
			t.Fatalf("expected a local error, got %+v", got)
		}
		return got.LocalError
	}
	q := authorizeQuery("oclient_missing", callback)
	if got := local(q); got != "invalid_client" {
		t.Fatal(got)
	}
	q = authorizeQuery(client.ClientID, "https://evil.example/cb")
	if got := local(q); got != "invalid_redirect_uri" {
		t.Fatal(got)
	}
	for _, c := range []struct {
		name string
		edit func(url.Values)
		want string
	}{
		{"no pkce", func(q url.Values) { q.Del("code_challenge") }, "invalid_request"},
		{"plain pkce", func(q url.Values) { q.Set("code_challenge_method", "plain") }, "invalid_request"},
		{"token response", func(q url.Values) { q.Set("response_type", "token") }, "unsupported_response_type"},
		{"other resource", func(q url.Values) { q.Set("resource", "https://other.example/mcp") }, "invalid_target"},
		{"other scope", func(q url.Values) { q.Set("scope", "crm") }, "invalid_scope"},
	} {
		q := authorizeQuery(client.ClientID, callback)
		c.edit(q)
		got, err := f.svc.Authorize(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(got.Redirect)
		if got.Redirect == "" || u.Query().Get("error") != c.want || u.Query().Get("state") != "st-1" || u.Query().Get("iss") != base {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	// Leaving the resource out means the only one there is.
	q = authorizeQuery(client.ClientID, callback)
	q.Del("resource")
	if got, err := f.svc.Authorize(ctx, q); err != nil || got.Request == "" {
		t.Fatalf("missing resource refused: %+v %v", got, err)
	}
}

func TestConsentRequestIsSignedAndExpires(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	got, err := f.svc.Authorize(ctx, authorizeQuery(client.ClientID, callback))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(got.Request, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var shown oauth.ConsentRequest
	if err := json.Unmarshal(payload, &shown); err != nil || shown.ClientName != "Codex" || shown.DisplayName != "Codex" {
		t.Fatalf("payload %s", payload)
	}
	forged := strings.Replace(string(payload), callback, "https://evil.example/cb", 1)
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(forged)) + "." + parts[2]
	if _, err := f.svc.Decide(ctx, "acct", tampered, true); !errors.Is(err, oauth.ErrRequestInvalid) {
		t.Fatal("tampered request accepted", err)
	}
	to, err := f.svc.Decide(ctx, "acct", got.Request, false)
	if err != nil || !strings.Contains(to, "error=access_denied") || !strings.HasPrefix(to, callback) {
		t.Fatalf("deny: %s %v", to, err)
	}
	if _, err := f.svc.Decide(ctx, "frozen", got.Request, true); !errors.Is(err, oauth.ErrAccountInactive) {
		t.Fatal("inactive account granted", err)
	}
	f.clock.advance(11 * time.Minute)
	if _, err := f.svc.Decide(ctx, "acct", got.Request, true); !errors.Is(err, oauth.ErrRequestInvalid) {
		t.Fatal("expired request accepted", err)
	}
}

func TestConfidentialClientAuthentication(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"claude.ai","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"token_endpoint_auth_method":"client_secret_post"}`)
	if client.ClientSecret == "" || client.ClientSecretExpiresAt == nil || *client.ClientSecretExpiresAt != 0 {
		t.Fatalf("confidential client %+v", client)
	}
	try := func(secret string, basic bool) string {
		code := f.grant(t, "acct", client.ClientID, "https://claude.ai/api/mcp/auth_callback")
		_, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, ClientSecret: secret, BasicAuth: basic,
			Code: code, RedirectURI: "https://claude.ai/api/mcp/auth_callback", CodeVerifier: verifier})
		return oauthCode(err)
	}
	if got := try("", false); got != "invalid_client" {
		t.Fatal("no secret:", got)
	}
	if got := try("wrong", false); got != "invalid_client" {
		t.Fatal("wrong secret:", got)
	}
	if got := try(client.ClientSecret, true); got != "invalid_client" {
		t.Fatal("secret by the wrong method:", got)
	}
	if got := try(client.ClientSecret, false); got != "" {
		t.Fatal("right secret:", got)
	}
	conns, _ := f.svc.ListConnections(ctx, "acct", false)
	if len(conns) == 0 || conns[0].HostKind != "claude" || conns[0].DisplayName != "Claude" {
		t.Fatalf("connections %+v", conns)
	}
}

func TestRegistrationValidationAndThrottle(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	for _, c := range []struct{ body, want string }{
		{`{"client_name":"x","redirect_uris":["http://example.com/cb"]}`, "invalid_redirect_uri"},
		{`{"client_name":"x","redirect_uris":[]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":["` + callback + `"]}`, "invalid_client_metadata"},
		{`{"client_name":"x","redirect_uris":["` + callback + `"],"token_endpoint_auth_method":"private_key_jwt"}`, "invalid_client_metadata"},
		{`{"client_name":"x","redirect_uris":["` + callback + `"],"grant_types":["implicit"]}`, "invalid_client_metadata"},
		{`[]`, "invalid_client_metadata"},
	} {
		if _, err := f.svc.Register(ctx, "198.51.100.1", []byte(c.body)); oauthCode(err) != c.want {
			t.Errorf("%s: got %s, want %s", c.body, oauthCode(err), c.want)
		}
	}
	body := []byte(`{"client_name":"x","redirect_uris":["` + callback + `"]}`)
	for i := range 20 {
		if _, err := f.svc.Register(ctx, "198.51.100.2", body); err != nil {
			t.Fatal(i, err)
		}
	}
	_, err := f.svc.Register(ctx, "198.51.100.2", body)
	var throttled *oauth.Error
	if !errors.As(err, &throttled) || throttled.Code != "too_many_requests" || throttled.RetryAfter <= 0 {
		t.Fatal("21st registration allowed", err)
	}
	if _, err := f.svc.Register(ctx, "198.51.100.3", body); err != nil {
		t.Fatal("another IP throttled", err)
	}
}

func TestRefreshIdleExpiry(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	code := f.grant(t, "acct", client.ClientID, callback)
	tokens, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: callback, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(31 * 24 * time.Hour)
	if _, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "refresh_token", ClientID: client.ClientID, RefreshToken: tokens.RefreshToken}); oauthCode(err) != "invalid_grant" {
		t.Fatal("idle refresh token accepted", err)
	}
}

func TestClientMetadataDocument(t *testing.T) {
	var fetches atomic.Int32
	var docID string
	mismatch := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		id := docID
		if mismatch {
			id = "https://elsewhere.example/client.json"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": id, "client_name": "Claude", "redirect_uris": []string{callback}})
	}))
	defer server.Close()
	docID = server.URL + "/client.json"
	f := setup(t, server.Client())
	ctx := context.Background()
	f.grant(t, "acct", docID, callback)
	f.grant(t, "acct", docID, callback)
	if n := fetches.Load(); n != 1 {
		t.Fatalf("metadata fetched %d times within a day", n)
	}
	f.clock.advance(25 * time.Hour)
	mismatch = true
	got, err := f.svc.Authorize(ctx, authorizeQuery(docID, callback))
	if err != nil || got.LocalError != "invalid_client" {
		t.Fatalf("mismatched document accepted: %+v %v", got, err)
	}
	// The document's own client_id must be an https URL with a path.
	got, err = f.svc.Authorize(ctx, authorizeQuery("https://crm.example.com", callback))
	if err != nil || got.LocalError != "invalid_client" {
		t.Fatalf("pathless client id accepted: %+v %v", got, err)
	}
}

func TestRevokeIsPerAccountAndImmediate(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	code := f.grant(t, "acct", client.ClientID, callback)
	tokens, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: callback, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := f.svc.VerifyAccess(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Revoke(ctx, "acct2", conn.ID); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatal("another account revoked the connection", err)
	}
	if list, _ := f.svc.ListConnections(ctx, "acct2", true); len(list) != 0 {
		t.Fatal("another account sees the connection")
	}
	revoked, err := f.svc.Revoke(ctx, "acct", conn.ID)
	if err != nil || revoked.RevokedAt == nil || revoked.RevokedReason != "user" {
		t.Fatalf("revoke %+v %v", revoked, err)
	}
	f.clock.advance(time.Minute)
	again, err := f.svc.Revoke(ctx, "acct", conn.ID)
	if err != nil || !again.RevokedAt.Equal(*revoked.RevokedAt) {
		t.Fatal("second revoke changed the connection", err)
	}
	if _, _, err := f.svc.VerifyAccess(ctx, tokens.AccessToken); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatal("revoked connection still answers", err)
	}
	if _, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "refresh_token", ClientID: client.ClientID, RefreshToken: tokens.RefreshToken}); oauthCode(err) != "invalid_grant" {
		t.Fatal("revoked connection refreshed", err)
	}
	if list, _ := f.svc.ListConnections(ctx, "acct", false); len(list) != 0 {
		t.Fatal("revoked connection listed as valid")
	}
}

// A token signed for another purpose or audience never verifies.
func TestAccessTokenIsBoundToTheMCPEndpoint(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	other, err := oauth.New(oauth.Config{BaseURL: "https://other.example", Secret: strings.Repeat("s", 32), Store: f.st, Now: f.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	code := f.grant(t, "acct", client.ClientID, callback)
	tokens, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: callback, CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := other.VerifyAccess(ctx, tokens.AccessToken); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatal("token for another resource accepted", err)
	}
	for _, junk := range []string{"", "abc", tokens.AccessToken + "x"} {
		if _, _, err := f.svc.VerifyAccess(ctx, junk); !errors.Is(err, oauth.ErrInvalidToken) {
			t.Fatalf("%q accepted", junk)
		}
	}
}

// Two exchanges of one code race on its row lock: exactly one gets tokens.
func TestConcurrentCodeExchangeIssuesOnce(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	client := f.register(t, `{"client_name":"Codex","redirect_uris":["`+callback+`"]}`)
	code := f.grant(t, "acct", client.ClientID, callback)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.svc.Token(ctx, oauth.TokenRequest{GrantType: "authorization_code", ClientID: client.ClientID, Code: code, RedirectURI: callback, CodeVerifier: verifier})
			results <- err
		}()
	}
	issued := 0
	for range 2 {
		if err := <-results; err == nil {
			issued++
		} else if oauthCode(err) != "invalid_grant" {
			t.Fatal(err)
		}
	}
	if issued != 1 {
		t.Fatalf("one code issued tokens %d times", issued)
	}
}
