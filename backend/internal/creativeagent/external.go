package creativeagent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// External agents (epic creative-mcp-server) reach the same tools the built-in
// assistant uses, through MCP. This file is their door: the catalog they may
// see and the one entry that runs a call. Protocol and authorization live
// elsewhere; here an external agent is just a connection acting on its account.

//go:embed instructions/external.md
var externalInstruction string

// ExternalInstructions is the usage guide every host receives on connect.
func ExternalInstructions() string { return strings.TrimSpace(externalInstruction) }

// ExternalPrincipal is who an external call acts as. Scope comes only from the
// connection's account (ADR-001); tool arguments never name an account.
type ExternalPrincipal struct {
	Scope        store.AccountScope
	ConnectionID string
	DisplayName  string
	HostKind     string
}

// ExternalTool is one tool as an external agent sees it.
type ExternalTool struct {
	Name        string
	Title       string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool
	Destructive bool
}

// ExternalImage is an image handed to the external agent's model.
type ExternalImage struct {
	MIME string
	Data []byte
}

// ExternalResult is a finished call: a summary a terminal can show in a few
// lines, the structured result, and images when the tool returns any.
type ExternalResult struct {
	Summary    string
	Structured json.RawMessage
	Images     []ExternalImage
}

// ExternalError is a refusal the external agent reads and acts on. It is a
// tool result, never a protocol failure: hosts treat those as a broken link.
type ExternalError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after_s,omitempty"`
}

func (e *ExternalError) Error() string { return e.Code + ": " + e.Message }

// ExternalConfig wires what only external calls need.
type ExternalConfig struct {
	// BaseURL is PUBLIC_BASE_URL; handoff links point there.
	BaseURL string
	// Watching reports whether the account has a workspace page open in this
	// process (a canvas subscription). Nil means unknown, reported as closed.
	Watching func(accountID string) bool
	// Timezone names the account's IANA zone for dates in summaries.
	Timezone func(ctx context.Context, accountID string) (string, error)
}

// ExternalTools is the external agents' tool catalog and entry.
type ExternalTools struct {
	service  *Service
	base     string
	watching func(string) bool
	timezone func(context.Context, string) (string, error)
	logger   *slog.Logger
}

// ExternalTools binds the external door to this service.
func (s *Service) ExternalTools(cfg ExternalConfig) *ExternalTools {
	return &ExternalTools{service: s, base: strings.TrimRight(cfg.BaseURL, "/"), watching: cfg.Watching, timezone: cfg.Timezone, logger: s.logger}
}

// externalTool is one entry of the external catalog. Tools shared with the
// built-in assistant take their description from the same Info and add only
// what differs outside a run.
type externalTool struct {
	name, title string
	readOnly    bool
	info        func() *schema.ToolInfo
	// appendix is what an external caller must know beyond the shared text.
	appendix string
	// extraParams are arguments only external calls take (canvas_id).
	extraParams map[string]*schema.ParameterInfo
	call        func(*ExternalTools, context.Context, ExternalPrincipal, string) (ExternalResult, error)
}

func externalCatalog() []externalTool {
	return []externalTool{
		{name: getCapabilitiesTool, title: "检查连接", readOnly: true, info: capabilitiesInfo, call: (*ExternalTools).getCapabilities},
		{name: listCanvasesTool, title: "列出画布", readOnly: true, info: listCanvasesInfo, call: (*ExternalTools).listCanvases},
		{name: readCanvasTool, title: "读取画布", readOnly: true, info: func() *schema.ToolInfo { return contextToolInfo(readCanvasTool) },
			appendix:    "外部调用时：必须传 canvas_id（来自 list_canvases）；文字节点的正文开头全部可读；结果另带 canvas_url，把它作为 Markdown 链接交给创作者。",
			extraParams: map[string]*schema.ParameterInfo{"canvas_id": {Type: schema.String, Required: true, Desc: "画布 ID，来自 list_canvases"}},
			call:        (*ExternalTools).readCanvas},
		{name: readAssetTool, title: "读取素材", readOnly: true, info: func() *schema.ToolInfo { return contextToolInfo(readAssetTool) },
			appendix: "外部调用时：可读范围是本账号的全部内容；内容修订 ID 来自 read_canvas 或 search_assets。图片以图片内容随结果返回（长边不超过 1024 像素），原图请让创作者在创意空间里查看。",
			call:     (*ExternalTools).readAsset},
		{name: searchAssetsTool, title: "查找资产库", readOnly: true, info: func() *schema.ToolInfo { return assetToolInfo(searchAssetsTool) },
			call: (*ExternalTools).searchAssets},
		{name: readLibraryTool, title: "读取资产库结构", readOnly: true, info: func() *schema.ToolInfo { return assetToolInfo(readLibraryTool) },
			call: (*ExternalTools).readLibrary},
	}
}

// List returns the tools external agents may call on this deployment.
func (e *ExternalTools) List() ([]ExternalTool, error) {
	out := []ExternalTool{}
	for _, t := range externalCatalog() {
		info := t.info()
		input := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
		if info.ParamsOneOf != nil {
			js, err := info.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("tool %s schema: %w", t.name, err)
			}
			raw, err := json.Marshal(js)
			if err != nil {
				return nil, err
			}
			input = objectSchema(raw)
		}
		if len(t.extraParams) > 0 {
			merged, err := withParams(input, t.extraParams)
			if err != nil {
				return nil, fmt.Errorf("tool %s schema: %w", t.name, err)
			}
			input = merged
		}
		out = append(out, ExternalTool{Name: t.name, Title: t.title, Description: describe(info.Desc, t.appendix), InputSchema: input, ReadOnly: t.readOnly})
	}
	return out, nil
}

func describe(shared, appendix string) string {
	if appendix == "" {
		return shared
	}
	return shared + "\n\n" + appendix
}

// objectSchema makes the schema explicit about being an object, which MCP
// requires of every input schema.
func objectSchema(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if m["type"] == nil {
		m["type"] = "object"
	}
	if m["properties"] == nil {
		m["properties"] = map[string]any{}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// withParams adds external-only arguments to a shared tool's schema.
func withParams(raw json.RawMessage, extra map[string]*schema.ParameterInfo) (json.RawMessage, error) {
	add, err := schema.NewParamsOneOfByParams(extra).ToJSONSchema()
	if err != nil {
		return nil, err
	}
	var base, more struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	addRaw, err := json.Marshal(add)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(addRaw, &more); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	props := map[string]json.RawMessage{}
	for k, v := range base.Properties {
		props[k] = v
	}
	for k, v := range more.Properties {
		props[k] = v
	}
	m["type"] = "object"
	m["properties"] = props
	m["required"] = append(more.Required, base.Required...)
	return json.Marshal(m)
}

// Call runs one external tool call. Refusals come back as *ExternalError;
// anything else is an internal failure the caller reports by reference.
func (e *ExternalTools) Call(ctx context.Context, p ExternalPrincipal, name string, args json.RawMessage) (ExternalResult, error) {
	start := time.Now()
	result, err := e.call(ctx, p, name, args)
	outcome := "ok"
	var refused *ExternalError
	switch {
	case err == nil:
	case errors.As(err, &refused):
		outcome = refused.Code
	default:
		ref := "cxerr_" + uuid.NewString()
		e.logger.ErrorContext(ctx, "external agent tool failed", slog.String("connection_id", p.ConnectionID), slog.String("tool", name),
			slog.String("ref", ref), slog.String("error", err.Error()))
		outcome = "internal"
		err = &ExternalError{Code: "internal", Message: fmt.Sprintf("系统内部错误，原因已记录（编号 %s）。这次调用没有改动任何内容。", ref)}
	}
	e.logger.InfoContext(ctx, "external agent tool call", slog.String("connection_id", p.ConnectionID), slog.String("tool", name),
		slog.String("outcome", outcome), slog.Duration("duration", time.Since(start)))
	return result, err
}

func (e *ExternalTools) call(ctx context.Context, p ExternalPrincipal, name string, args json.RawMessage) (ExternalResult, error) {
	if p.Scope.AccountID() == "" || p.ConnectionID == "" {
		return ExternalResult{}, errors.New("external call without a principal")
	}
	for _, t := range externalCatalog() {
		if t.name != name {
			continue
		}
		if err := requireCreativeRead(ctx, p.Scope); err != nil {
			return ExternalResult{}, externalRefusal(err)
		}
		raw := strings.TrimSpace(string(args))
		if raw == "" || raw == "null" {
			raw = "{}"
		}
		result, err := t.call(e, ctx, p, raw)
		if err != nil {
			return ExternalResult{}, externalRefusal(err)
		}
		return result, nil
	}
	return ExternalResult{}, &ExternalError{Code: "not_found", Message: "没有这个工具：" + name}
}

func requireCreativeRead(ctx context.Context, scope store.AccountScope) error {
	return scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error { return tx.RequireCreativeRead(ctx) })
}

// externalRefusal turns a domain failure into words the external agent acts
// on. Messages the domain wrote itself pass through unchanged, so both doors
// read the same refusal (X-I4); what is missing becomes not_found, whatever
// module said so.
func externalRefusal(err error) error {
	var already *ExternalError
	if errors.As(err, &already) {
		return err
	}
	var refusal *toolRefusal
	var public creativeops.Public
	switch {
	case errors.Is(err, store.ErrCreativeAccessDenied):
		return &ExternalError{Code: "forbidden", Message: "这个账号现在不能使用创意空间"}
	case errors.Is(err, errToolArguments):
		return &ExternalError{Code: "validation_failed", Message: "参数不符合工具说明：须是一个 JSON 对象，且只能使用说明里列出的字段"}
	case errors.Is(err, creativeops.ErrValidation):
		return &ExternalError{Code: "validation_failed", Message: "参数或取值不符合工具说明，请对照说明重写这次调用"}
	case errors.Is(err, creativecanvas.ErrNotFound):
		return &ExternalError{Code: "not_found", Message: "画布或节点不存在"}
	case errors.Is(err, ErrNotFound), errors.Is(err, creativecontent.ErrNotFound), errors.Is(err, creativecontent.ErrMissingRoot),
		errors.Is(err, creativelibrary.ErrNotFound), errors.Is(err, store.ErrNoRows):
		return &ExternalError{Code: "not_found", Message: "内容不存在或已删除，请重新查找"}
	case errors.Is(err, ErrEgressRequired), errors.Is(err, creativecontent.ErrUsageDenied):
		return &ExternalError{Code: "refused", Message: "这项内容已删除、失去引用或不可展示，请重新选择"}
	case errors.Is(err, ErrUnsupportedSegment), errors.Is(err, ErrModelCapability):
		return &ExternalError{Code: "refused", Message: "这类内容不能读取"}
	case errors.As(err, &refusal):
		return &ExternalError{Code: "refused", Message: refusal.PublicMessage()}
	case errors.As(err, &public):
		return &ExternalError{Code: "refused", Message: public.PublicMessage()}
	}
	if mapped := asRefusal(err); errors.As(mapped, &refusal) {
		return &ExternalError{Code: "refused", Message: refusal.PublicMessage()}
	}
	return err
}
