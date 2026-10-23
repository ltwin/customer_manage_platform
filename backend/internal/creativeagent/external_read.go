package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Tools only external agents have: a run is given its canvas and its account,
// an external agent has to ask.
const (
	getCapabilitiesTool = "get_capabilities"
	listCanvasesTool    = "list_canvases"
)

const (
	// externalImageSide and externalImageBytes bound an image handed to an
	// external model: vision models downscale to about this size themselves.
	externalImageSide  = 1024
	externalImageBytes = 1 << 20
	// skillCountCeiling stops counting skills after a few catalog pages.
	skillCountCeiling = 200
)

func capabilitiesInfo() *schema.ToolInfo {
	return &schema.ToolInfo{Name: getCapabilitiesTool, Desc: "先调用这个工具：确认连接的创意空间账号和署名，列出当前可用的生成模型、本月剩余额度、可用 Skill 数量，以及创作者现在有没有打开创意空间页面（近似判断，只作提示）。hints 是需要先告诉创作者的事。"}
}

func listCanvasesInfo() *schema.ToolInfo {
	return &schema.ToolInfo{Name: listCanvasesTool, Desc: "按最近编辑倒序列出创作者的画布（不含已归档），每项带 canvas_id 和在创意空间打开它的 canvas_url。query 按标题包含匹配。next_cursor 为空字符串表示没有下一页。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":  {Type: schema.String, Desc: "标题包含的文字，最多 100 字"},
			"limit":  {Type: schema.Integer, Desc: "1 到 50，缺省 20"},
			"cursor": {Type: schema.String, Desc: "上一页的 next_cursor，query 保持不变"},
		})}
}

func (e *ExternalTools) canvasURL(id string) string { return e.base + "/creative/canvases/" + id }

// location is the account's zone for dates in summaries; UTC when unknown.
func (e *ExternalTools) location(ctx context.Context, accountID string) *time.Location {
	if e.timezone == nil {
		return time.UTC
	}
	name, err := e.timezone(ctx, accountID)
	if err != nil || name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func shortDate(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	label := fmt.Sprintf("%d 月 %d 日", int(t.Month()), t.Day())
	if loc == time.UTC {
		label += "（UTC）"
	}
	return label
}

func structured(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	return raw, err
}

type capabilityModel struct {
	Key               string `json:"key"`
	DisplayName       string `json:"display_name"`
	NodeType          string `json:"node_type"`
	ParametersSummary string `json:"parameters_summary"`
}

type capabilityBudget struct {
	Remaining string    `json:"remaining"`
	Unit      string    `json:"unit"`
	PeriodEnd time.Time `json:"period_end"`
}

type capabilities struct {
	Account struct {
		DisplayName *string `json:"display_name"`
	} `json:"account"`
	Connection struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		HostKind    string `json:"host_kind"`
	} `json:"connection"`
	Generation struct {
		Models []capabilityModel `json:"models"`
		Budget *capabilityBudget `json:"budget"`
	} `json:"generation"`
	WorkspaceOpen bool `json:"workspace_open"`
	Skills        struct {
		Count int `json:"count"`
	} `json:"skills"`
	Hints []string `json:"hints"`
}

func (e *ExternalTools) getCapabilities(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	if err := strictArguments(raw, &struct{}{}); err != nil {
		return ExternalResult{}, err
	}
	var out capabilities
	out.Connection.ID, out.Connection.DisplayName, out.Connection.HostKind = p.ConnectionID, p.DisplayName, p.HostKind
	var name *string
	err := p.Scope.QueryRow(ctx, "account_profiles", "display_name", "").Scan(&name)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return ExternalResult{}, err
	}
	out.Account.DisplayName = name
	out.Generation.Models = []capabilityModel{}
	for _, m := range e.service.generationModels() {
		keys := make([]string, 0, len(m.Parameters))
		for _, param := range m.Parameters {
			keys = append(keys, param.Key)
		}
		out.Generation.Models = append(out.Generation.Models, capabilityModel{Key: m.Key, DisplayName: m.Label, NodeType: m.Kind, ParametersSummary: strings.Join(keys, ", ")})
	}
	if e.service.gateway != nil {
		b, err := e.service.gateway.MonthBudget(ctx, p.Scope)
		if err != nil {
			return ExternalResult{}, err
		}
		out.Generation.Budget = &capabilityBudget{Remaining: fmt.Sprintf("%.2f", float64(b.RemainingMicros)/1e6), Unit: b.Currency, PeriodEnd: b.PeriodEnd}
	}
	out.WorkspaceOpen = e.watching != nil && e.watching(p.Scope.AccountID())
	count, err := e.countSkills(ctx, p.Scope)
	if err != nil {
		return ExternalResult{}, err
	}
	out.Skills.Count = count
	out.Hints = []string{}
	if !out.WorkspaceOpen {
		out.Hints = append(out.Hints, "创作者现在没有打开创意空间页面；把画布链接交给创作者打开，就能实时看到你的改动。")
	}
	if len(out.Generation.Models) == 0 {
		out.Hints = append(out.Hints, "当前部署没有可用的生成模型。")
	}

	lines := []string{}
	if name != nil && *name != "" {
		lines = append(lines, fmt.Sprintf("已连接创意空间账号「%s」，署名 %s。", *name, p.DisplayName))
	} else {
		lines = append(lines, fmt.Sprintf("已连接创意空间账号，署名 %s。", p.DisplayName))
	}
	gen := fmt.Sprintf("可用生成模型 %d 个", len(out.Generation.Models))
	if b := out.Generation.Budget; b != nil {
		gen += fmt.Sprintf("；本月剩余额度 %s %s", b.Remaining, b.Unit)
	}
	lines = append(lines, gen+"。")
	if out.WorkspaceOpen {
		lines = append(lines, "创作者已打开创意空间页面。")
	} else {
		lines = append(lines, "创作者现在没有打开创意空间页面。")
	}
	lines = append(lines, "下一步：用 list_canvases 找到要处理的画布。")
	body, err := structured(out)
	return ExternalResult{Summary: strings.Join(lines, "\n"), Structured: body}, err
}

func (e *ExternalTools) countSkills(ctx context.Context, scope store.AccountScope) (int, error) {
	if e.service.skills == nil {
		return 0, nil
	}
	count, cursor := 0, ""
	for count < skillCountCeiling {
		page, err := e.service.skills.ListAccessibleSkills(ctx, scope, "", cursor, 50)
		if err != nil {
			return 0, err
		}
		count += len(page.Items)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return count, nil
}

type canvasListItem struct {
	CanvasID  string    `json:"canvas_id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	CanvasURL string    `json:"canvas_url"`
}

func (e *ExternalTools) listCanvases(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	var args struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return ExternalResult{}, err
	}
	if args.Limit == 0 {
		args.Limit = 20
	}
	if args.Limit < 1 || args.Limit > 50 || utf8.RuneCountInString(args.Query) > 100 {
		return ExternalResult{}, creativeops.ErrValidation
	}
	page, err := creativecanvas.ListProjects(ctx, p.Scope, creativecanvas.ProjectQuery{Q: args.Query, Limit: args.Limit, Cursor: args.Cursor})
	if err != nil {
		return ExternalResult{}, err
	}
	out := struct {
		Items      []canvasListItem `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}{Items: []canvasListItem{}, NextCursor: page.NextCursor}
	loc := e.location(ctx, p.Scope.AccountID())
	lines := []string{}
	for i, project := range page.Items {
		if project.DefaultCanvasID == "" {
			continue
		}
		item := canvasListItem{CanvasID: project.DefaultCanvasID, Title: project.Name, UpdatedAt: project.EditedAt, CanvasURL: e.canvasURL(project.DefaultCanvasID)}
		out.Items = append(out.Items, item)
		lines = append(lines, fmt.Sprintf("%d. [%s](%s) · %s编辑", i+1, markdownText(item.Title), item.CanvasURL, shortDate(item.UpdatedAt, loc)))
	}
	head := fmt.Sprintf("最近编辑的 %d 张画布：", len(out.Items))
	switch {
	case len(out.Items) == 0 && args.Query != "":
		head = fmt.Sprintf("没有标题包含「%s」的画布。", args.Query)
	case len(out.Items) == 0:
		head = "还没有画布。请创作者先在创意空间新建一个项目。"
	}
	summary := append([]string{head}, lines...)
	if len(out.Items) > 0 {
		next := "下一步：用 read_canvas(canvas_id) 读取内容。"
		if out.NextCursor != "" {
			next += "还有更早的画布，传 cursor 继续列出。"
		}
		summary = append(summary, next)
	}
	body, err := structured(out)
	return ExternalResult{Summary: strings.Join(summary, "\n"), Structured: body}, err
}

// markdownText keeps a title from breaking the link it labels.
func markdownText(s string) string {
	return strings.NewReplacer("[", "［", "]", "］", "\n", " ").Replace(s)
}

func (e *ExternalTools) readCanvas(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	var args struct {
		CanvasID string `json:"canvas_id"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return ExternalResult{}, err
	}
	if args.CanvasID == "" || args.Offset < 0 || args.Limit < 1 || args.Limit > 50 {
		return ExternalResult{}, creativeops.ErrValidation
	}
	view, err := readCanvasView(ctx, p.Scope, externalReadAccess{}, args.CanvasID, args.Offset, args.Limit)
	if errors.Is(err, creativecanvas.ErrNotFound) || errors.Is(err, store.ErrNoRows) {
		return ExternalResult{}, &ExternalError{Code: "not_found", Message: "画布不存在"}
	}
	if err != nil {
		return ExternalResult{}, err
	}
	url := e.canvasURL(view.ID)
	body, err := json.Marshal(struct {
		canvasContext
		CanvasURL string `json:"canvas_url"`
	}{view, url})
	if err != nil {
		return ExternalResult{}, err
	}
	lines := []string{fmt.Sprintf("画布《%s》共 %d 个节点，这一页 %d 个。", view.Name, view.NodeCount, len(view.Nodes)), fmt.Sprintf("[在创意空间打开](%s)", url)}
	for i, n := range view.Nodes {
		if i == 4 {
			lines = append(lines, fmt.Sprintf("……另有 %d 个节点见结构化结果。", len(view.Nodes)-4))
			break
		}
		title := n.Title
		if title == "" {
			title = "未命名"
		}
		lines = append(lines, fmt.Sprintf("- %s（%s）", title, n.Type))
	}
	next := "下一步：图片和完整正文用 read_asset(content_revision_id) 读取。"
	if view.NextOffset != nil {
		next = fmt.Sprintf("下一步：还有节点，用 offset=%d 继续读；图片和完整正文用 read_asset(content_revision_id)。", *view.NextOffset)
	}
	lines = append(lines, next)
	return ExternalResult{Summary: strings.Join(lines, "\n"), Structured: body}, nil
}

func (e *ExternalTools) readAsset(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	var args struct {
		RevisionID string `json:"content_revision_id"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return ExternalResult{}, err
	}
	result, err := e.service.readAssetResult(ctx, p.Scope, externalReadAccess{}, args.RevisionID, args.Offset, args.Limit)
	if err != nil {
		return ExternalResult{}, err
	}
	out := ExternalResult{}
	var summary []string
	if id, _ := result["image_revision_id"].(string); id != "" {
		img, err := e.image(ctx, p.Scope, id)
		var limit *creativemedia.ImagePreviewLimitError
		switch {
		case errors.As(err, &limit), errors.Is(err, creativemedia.ErrUnsupported):
			delete(result, "image_revision_id")
			result["image_omitted"] = "这张图片无法压缩到可发送的大小或格式"
			summary = append(summary, "这张图片无法压缩到可发送的大小或格式，没有附上。")
		case err != nil:
			return ExternalResult{}, err
		default:
			out.Images = []ExternalImage{img}
		}
	}
	switch {
	case result["model"] != nil:
		if len(out.Images) > 0 {
			summary = append([]string{"模型的尺寸、材质和参数见结构化结果，已附上缩略图。"}, summary...)
		} else {
			summary = append([]string{"模型的尺寸、材质和参数见结构化结果。"}, summary...)
		}
	case len(out.Images) > 0:
		summary = append([]string{"已附上图片。"}, summary...)
	case result["text"] != nil:
		head := "文字内容见结构化结果的 text。"
		if more, _ := result["has_more"].(bool); more {
			head += fmt.Sprintf("还有后续，用 offset=%v 继续读。", result["next_offset"])
		}
		summary = append([]string{head}, summary...)
	}
	body, err := structured(result)
	out.Summary, out.Structured = strings.Join(summary, "\n"), body
	return out, err
}

// image reads the display rendition and bounds it for an external model.
func (e *ExternalTools) image(ctx context.Context, scope store.AccountScope, id string) (ExternalImage, error) {
	if e.service.images == nil {
		return ExternalImage{}, ErrModelCapability
	}
	handle, err := e.service.images.AgentImage(ctx, scope, id, e.service.limits.ImagePreviewBytes)
	if err != nil {
		return ExternalImage{}, err
	}
	reader, err := handle.Open(ctx)
	if err != nil {
		return ExternalImage{}, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, handle.ByteSize+1))
	if err != nil {
		return ExternalImage{}, err
	}
	fitted, mime, err := creativemedia.FitPreview(data, handle.MIME, externalImageSide, externalImageBytes)
	if err != nil {
		return ExternalImage{}, err
	}
	return ExternalImage{MIME: mime, Data: fitted}, nil
}

func (e *ExternalTools) searchAssets(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	page, err := e.service.searchAssets(ctx, p.Scope, externalReadAccess{}, raw)
	if err != nil {
		return ExternalResult{}, err
	}
	return externalPageSummary(page, "素材", "下一步：要看图片或完整信息，用 read_asset(content_revision_id) 逐项读取。")
}

func (e *ExternalTools) readLibrary(ctx context.Context, p ExternalPrincipal, raw string) (ExternalResult, error) {
	page, err := e.service.readLibrary(ctx, p.Scope, externalReadAccess{}, raw)
	if err != nil {
		return ExternalResult{}, err
	}
	return externalPageSummary(page, "项", "下一步：用 search_assets 按分组或标签查找素材。")
}

func externalPageSummary(page, noun, next string) (ExternalResult, error) {
	var head struct {
		Items      []json.RawMessage `json:"items"`
		TotalCount *int              `json:"total_count"`
		HasMore    bool              `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(page), &head); err != nil {
		return ExternalResult{}, err
	}
	first := fmt.Sprintf("这一页 %d %s", len(head.Items), noun)
	if head.TotalCount != nil {
		first = fmt.Sprintf("共 %d %s，这一页 %d %s", *head.TotalCount, noun, len(head.Items), noun)
	}
	if len(head.Items) == 0 {
		first = "没有找到符合条件的" + strings.TrimPrefix(noun, "项")
		if noun == "项" {
			first = "没有符合条件的分组、分类或标签"
		}
	}
	lines := []string{first + "。"}
	if head.HasMore {
		lines = append(lines, "还有下一页，传 next_cursor 继续读取。")
	}
	if len(head.Items) > 0 {
		lines = append(lines, next)
	}
	return ExternalResult{Summary: strings.Join(lines, "\n"), Structured: json.RawMessage(page)}, nil
}
