package oauth

import (
	"net/url"
	"strings"
)

// Host kinds name the products an external agent runs in; the display name is
// how history and the workspace sign its work.
const (
	HostClaude     = "claude"
	HostClaudeCode = "claude_code"
	HostCodex      = "codex"
	HostChatGPT    = "chatgpt"
	HostOther      = "other"
)

var hostDisplayNames = map[string]string{
	HostClaude: "Claude", HostClaudeCode: "Claude Code", HostCodex: "Codex", HostChatGPT: "ChatGPT",
}

// identifyHost recognizes the host from what the client declared at
// authorization: its callback host, its name, its metadata URL. The first rule
// that matches wins. Unknown hosts are signed with the name they gave.
func identifyHost(client Client, redirectURI string) (kind, display string) {
	callback := ""
	if u, err := url.Parse(redirectURI); err == nil {
		callback = strings.ToLower(u.Hostname())
	}
	name := strings.ToLower(client.Name)
	metadataHost := ""
	if client.Registration == "cimd" {
		if u, err := url.Parse(client.ID); err == nil {
			metadataHost = strings.ToLower(u.Hostname())
		}
	}
	switch {
	case underDomain(callback, "claude.ai", "claude.com"):
		kind = HostClaude
	case underDomain(callback, "chatgpt.com", "openai.com"):
		kind = HostChatGPT
	case strings.Contains(name, "claude code"), underDomain(metadataHost, "claude.ai", "claude.com", "anthropic.com") && loopbackHost(callback):
		kind = HostClaudeCode
	case strings.Contains(name, "codex"):
		kind = HostCodex
	default:
		return HostOther, client.Name
	}
	return kind, hostDisplayNames[kind]
}

func underDomain(host string, domains ...string) bool {
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}
