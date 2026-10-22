// Package creativemcp serves the creative tools to external agents over MCP
// (epic creative-mcp-server). It only adapts protocols: tools, their wording
// and their rules belong to creativeagent, tokens to platform/oauth. It is a
// plain http.Handler and never imports a router framework (ADR-003).
package creativemcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/samson/customer-manage-platform/backend/internal/creativeagent"
)

// ErrUnauthorized is what Verify returns for a token the endpoint must answer
// with 401, so the host refreshes it or asks the photographer again.
var ErrUnauthorized = errors.New("mcp request is not authorized")

// Scope is the one OAuth scope the endpoint requires.
const Scope = "creative"

const principalKey = "creative_principal"

// Options assemble the endpoint.
type Options struct {
	Tools *creativeagent.ExternalTools
	// Verify checks a bearer token and names the connection it acts for.
	Verify func(ctx context.Context, token string) (creativeagent.ExternalPrincipal, time.Time, error)
	// ResourceMetadataURL goes into every 401's WWW-Authenticate.
	ResourceMetadataURL string
	Version             string
	Logger              *slog.Logger
	// CallsPerMinute bounds one connection's tool calls; zero takes the default.
	CallsPerMinute int
	Now            func() time.Time
}

// New builds the /mcp handler.
func New(opts Options) (http.Handler, error) {
	if opts.Tools == nil || opts.Verify == nil || opts.ResourceMetadataURL == "" {
		return nil, errors.New("creativemcp: tools, verifier and resource metadata URL are required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Version == "" {
		opts.Version = "1"
	}
	tools, err := opts.Tools.List()
	if err != nil {
		return nil, err
	}
	limit := newLimiter(opts.CallsPerMinute, opts.Now)
	server := mcp.NewServer(&mcp.Implementation{Name: "creative-space", Title: "创意空间", Version: opts.Version},
		&mcp.ServerOptions{Instructions: creativeagent.ExternalInstructions(), Logger: opts.Logger})
	for _, t := range tools {
		server.AddTool(sdkTool(t), toolHandler(opts.Tools, limit, t.Name))
	}
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		// Behind the reverse proxy every request arrives from loopback with the
		// public Host, which the SDK's DNS-rebinding guard would refuse. That
		// guard protects unauthenticated local servers; this one requires an
		// OAuth token on every request.
		DisableLocalhostProtection: true,
		Logger:                     opts.Logger,
	})
	verify := func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		p, exp, err := opts.Verify(ctx, token)
		if errors.Is(err, ErrUnauthorized) {
			return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
		}
		if err != nil {
			return nil, err
		}
		// UserID ties the MCP session to its connection: another connection
		// presenting the same session id is refused by the SDK.
		return &auth.TokenInfo{UserID: p.ConnectionID, Scopes: []string{Scope}, Expiration: exp, Extra: map[string]any{principalKey: p}}, nil
	}
	return auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: opts.ResourceMetadataURL, Scopes: []string{Scope}})(stream), nil
}

func sdkTool(t creativeagent.ExternalTool) *mcp.Tool {
	annotations := &mcp.ToolAnnotations{Title: t.Title, ReadOnlyHint: t.ReadOnly, IdempotentHint: t.ReadOnly}
	if !t.ReadOnly {
		destructive := t.Destructive
		annotations.DestructiveHint = &destructive
	}
	closed := false
	annotations.OpenWorldHint = &closed
	return &mcp.Tool{Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: json.RawMessage(t.InputSchema), Annotations: annotations}
}

func toolHandler(tools *creativeagent.ExternalTools, limit *limiter, name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var p creativeagent.ExternalPrincipal
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			p, _ = req.Extra.TokenInfo.Extra[principalKey].(creativeagent.ExternalPrincipal)
		}
		if p.ConnectionID == "" {
			return nil, errors.New("creativemcp: tool call without a verified connection")
		}
		if wait, ok := limit.allow(p.ConnectionID); !ok {
			return refusal(&creativeagent.ExternalError{Code: "rate_limited", Message: "调用太频繁，请稍后再试", RetryAfter: wait}), nil
		}
		result, err := tools.Call(ctx, p, name, req.Params.Arguments)
		var refused *creativeagent.ExternalError
		if errors.As(err, &refused) {
			return refusal(refused), nil
		}
		if err != nil {
			return nil, err
		}
		return success(result), nil
	}
}

// success writes the summary first, so a terminal showing a few lines still
// reads the conclusion, then the structured result for hosts that do not read
// structuredContent. A result carrying images leaves structuredContent out:
// some hosts drop the text when both are present (U9), and the JSON is already
// in the text.
func success(r creativeagent.ExternalResult) *mcp.CallToolResult {
	text := r.Summary
	if len(r.Structured) > 0 {
		text += "\n\n" + string(r.Structured)
	}
	out := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	for _, img := range r.Images {
		out.Content = append(out.Content, &mcp.ImageContent{Data: img.Data, MIMEType: img.MIME})
	}
	if len(r.Images) == 0 && len(r.Structured) > 0 {
		out.StructuredContent = json.RawMessage(r.Structured)
	}
	return out
}

// refusal is a domain "no" as a tool result: hosts treat JSON-RPC errors as a
// broken connection, while the model reads and acts on this.
func refusal(e *creativeagent.ExternalError) *mcp.CallToolResult {
	raw, err := json.Marshal(e)
	if err != nil {
		raw = []byte(`{"code":"internal","message":"系统内部错误"}`)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
}
