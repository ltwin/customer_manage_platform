package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRedirectURIRules(t *testing.T) {
	for _, c := range []struct {
		uri  string
		want bool
	}{
		{"http://127.0.0.1:43117/callback", true},
		{"http://localhost:8976/oauth/callback", true},
		{"http://[::1]:9000/cb", true},
		{"https://claude.ai/api/mcp/auth_callback", true},
		{"https://chatgpt.com/connector_platform_oauth_redirect", true},
		{"http://example.com/callback", false},
		{"http://127.0.0.2.example.com/cb", false},
		{"myapp://callback", false},
		{"https://a.example/cb#frag", false},
		{"https://user:pw@a.example/cb", false},
		{"/relative", false},
	} {
		if got := validRedirectURI(c.uri); got != c.want {
			t.Errorf("validRedirectURI(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

func TestRedirectURIMatchIgnoresOnlyLoopbackPort(t *testing.T) {
	registered := []string{"http://127.0.0.1:3000/callback", "https://claude.ai/api/mcp/auth_callback"}
	for _, c := range []struct {
		uri  string
		want bool
	}{
		{"http://127.0.0.1:3000/callback", true},
		{"http://127.0.0.1:51234/callback", true},
		{"http://127.0.0.1:51234/other", false},
		{"http://localhost:3000/callback", false},
		{"https://claude.ai/api/mcp/auth_callback", true},
		{"https://claude.ai:8443/api/mcp/auth_callback", false},
		{"https://claude.ai/api/mcp/auth_callback?x=1", false},
	} {
		if got := matchRedirectURI(registered, c.uri); got != c.want {
			t.Errorf("matchRedirectURI(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

func TestIdentifyHost(t *testing.T) {
	for _, c := range []struct {
		client   Client
		redirect string
		kind     string
		display  string
	}{
		{Client{ID: "oclient_1", Registration: "dcr", Name: "claude.ai"}, "https://claude.ai/api/mcp/auth_callback", HostClaude, "Claude"},
		{Client{ID: "oclient_2", Registration: "dcr", Name: "ChatGPT"}, "https://chatgpt.com/connector_platform_oauth_redirect", HostChatGPT, "ChatGPT"},
		{Client{ID: "oclient_3", Registration: "dcr", Name: "Claude Code (creative)"}, "http://localhost:51234/callback", HostClaudeCode, "Claude Code"},
		{Client{ID: "https://claude.ai/oauth/claude-code-client-metadata", Registration: "cimd", Name: "Claude"}, "http://localhost:51234/callback", HostClaudeCode, "Claude Code"},
		{Client{ID: "oclient_4", Registration: "dcr", Name: "Codex"}, "http://127.0.0.1:1455/callback", HostCodex, "Codex"},
		{Client{ID: "oclient_5", Registration: "dcr", Name: "Cursor"}, "http://127.0.0.1:1455/callback", HostOther, "Cursor"},
	} {
		kind, display := identifyHost(c.client, c.redirect)
		if kind != c.kind || display != c.display {
			t.Errorf("%s via %s: got %s/%s, want %s/%s", c.client.Name, c.redirect, kind, display, c.kind, c.display)
		}
	}
}

func TestPublicAddress(t *testing.T) {
	for _, c := range []struct {
		addr string
		want bool
	}{
		{"93.184.216.34", true},
		{"2606:2800:220:1:248:1893:25c8:1946", true},
		{"127.0.0.1", false},
		{"10.0.0.8", false},
		{"172.16.3.4", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"100.100.100.200", false},
		{"0.0.0.0", false},
		{"::1", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"::ffff:127.0.0.1", false},
		{"224.0.0.1", false},
	} {
		if got := publicAddress(netip.MustParseAddr(c.addr)); got != c.want {
			t.Errorf("publicAddress(%s) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// The default metadata client must refuse to reach an internal service even
// when it is asked to directly.
func TestGuardedClientRefusesLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := guardedClient().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("loopback request allowed")
	}
}
