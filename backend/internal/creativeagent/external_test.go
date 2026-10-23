package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func (f *fixture) external(watching bool) *ExternalTools {
	return f.service.ExternalTools(ExternalConfig{BaseURL: "https://crm.example.com/", Watching: func(string) bool { return watching }})
}

func (f *fixture) principal(scope any) ExternalPrincipal {
	p := ExternalPrincipal{Scope: f.alice, ConnectionID: "cxcon_alice", DisplayName: "Claude Code", HostKind: "claude_code"}
	if scope == "bob" {
		p.Scope, p.ConnectionID = f.bob, "cxcon_bob"
	}
	return p
}

func externalCode(err error) string {
	var refused *ExternalError
	if errors.As(err, &refused) {
		return refused.Code
	}
	if err == nil {
		return ""
	}
	return "go:" + err.Error()
}

func TestExternalCatalogIsTheReadToolsWithTitles(t *testing.T) {
	f := setup(t)
	tools, err := f.external(false).List()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
		if tool.Title == "" || !tool.ReadOnly || tool.Destructive || tool.Description == "" {
			t.Errorf("%s: %+v", tool.Name, tool)
		}
		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema.Type != "object" || schema.Properties == nil {
			t.Errorf("%s schema %s", tool.Name, tool.InputSchema)
		}
		if tool.Name == readCanvasTool && (!slices.Contains(schema.Required, "canvas_id") || schema.Properties["canvas_id"] == nil || schema.Properties["offset"] == nil) {
			t.Errorf("read_canvas schema %s", tool.InputSchema)
		}
	}
	want := []string{"get_capabilities", "list_canvases", "read_canvas", "read_asset", "search_assets", "read_library"}
	if !slices.Equal(names, want) {
		t.Fatalf("catalog %v, want %v", names, want)
	}
	if !strings.Contains(ExternalInstructions(), "get_capabilities") || len([]rune(ExternalInstructions())) > 4000 {
		t.Fatal("instructions missing or over 4000 characters")
	}
}

// X-I1: every identifier from another account is simply not there.
func TestExternalCallsStayInTheirAccount(t *testing.T) {
	f := setup(t)
	e := f.external(false)
	ctx := t.Context()
	alice, bob := f.principal("alice"), f.principal("bob")
	mine := f.revision(f.alice, "海边黄昏的参考说明")
	theirs := f.revision(f.bob, "别人的笔记")

	list, err := e.Call(ctx, alice, listCanvasesTool, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var canvases struct {
		Items []canvasListItem `json:"items"`
	}
	if err := json.Unmarshal(list.Structured, &canvases); err != nil {
		t.Fatal(err)
	}
	if len(canvases.Items) != 1 || canvases.Items[0].CanvasID != "cccv_a" || canvases.Items[0].CanvasURL != "https://crm.example.com/creative/canvases/cccv_a" {
		t.Fatalf("canvases %+v", canvases.Items)
	}
	if first := strings.SplitN(list.Summary, "\n", 2)[0]; first != "最近编辑的 1 张画布：" {
		t.Fatalf("summary first line %q", first)
	}
	if !strings.Contains(list.Summary, "[项目](https://crm.example.com/creative/canvases/cccv_a)") {
		t.Fatalf("summary has no canvas link: %s", list.Summary)
	}

	for _, c := range []struct {
		name, tool, args string
	}{
		{"other canvas", readCanvasTool, `{"canvas_id":"cccv_b","offset":0,"limit":10}`},
		{"other revision", readAssetTool, `{"content_revision_id":"` + theirs + `","offset":0,"limit":100}`},
		{"no such revision", readAssetTool, `{"content_revision_id":"ccrv_missing","offset":0,"limit":100}`},
	} {
		if _, err := e.Call(ctx, alice, c.tool, json.RawMessage(c.args)); externalCode(err) != "not_found" {
			t.Errorf("%s: %s", c.name, externalCode(err))
		}
	}
	got, err := e.Call(ctx, alice, readAssetTool, json.RawMessage(`{"content_revision_id":"`+mine+`","offset":0,"limit":8192}`))
	if err != nil {
		t.Fatal(err)
	}
	var asset map[string]any
	if err := json.Unmarshal(got.Structured, &asset); err != nil {
		t.Fatal(err)
	}
	if text, _ := asset["text"].(string); !strings.Contains(text, "海边黄昏的参考说明") || len(got.Images) != 0 {
		t.Fatalf("read_asset %s", got.Structured)
	}

	found, err := e.Call(ctx, bob, searchAssetsTool, json.RawMessage(`{"limit":20}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(found.Structured), mine) || !strings.Contains(string(found.Structured), theirs) {
		t.Fatalf("bob's search %s", found.Structured)
	}
	if _, err := e.Call(ctx, bob, readLibraryTool, json.RawMessage(`{"kind":"all"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestExternalReadCanvasNeedsItsCanvas(t *testing.T) {
	f := setup(t)
	e := f.external(false)
	ctx := t.Context()
	got, err := e.Call(ctx, f.principal("alice"), readCanvasTool, json.RawMessage(`{"canvas_id":"cccv_a","offset":0,"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		ID        string `json:"canvas_id"`
		CanvasURL string `json:"canvas_url"`
		NodeCount int    `json:"node_count"`
	}
	if err := json.Unmarshal(got.Structured, &view); err != nil || view.ID != "cccv_a" || view.CanvasURL != "https://crm.example.com/creative/canvases/cccv_a" {
		t.Fatalf("read_canvas %s %v", got.Structured, err)
	}
	if !strings.HasPrefix(got.Summary, "画布《项目》共 0 个节点") || !strings.Contains(got.Summary, "](https://crm.example.com/creative/canvases/cccv_a)") {
		t.Fatalf("summary %s", got.Summary)
	}
	for _, args := range []string{`{"offset":0,"limit":10}`, `{"canvas_id":"cccv_a","offset":0,"limit":10,"conversation_id":"x"}`, `{"canvas_id":"cccv_a","offset":0,"limit":0}`, `[1]`} {
		if _, err := e.Call(ctx, f.principal("alice"), readCanvasTool, json.RawMessage(args)); externalCode(err) != "validation_failed" {
			t.Errorf("%s: %s", args, externalCode(err))
		}
	}
	if _, err := e.Call(ctx, f.principal("alice"), "read_history", json.RawMessage(`{}`)); externalCode(err) != "not_found" {
		t.Fatal("an internal tool is reachable from outside", err)
	}
}

func TestExternalCapabilities(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, open := range []bool{false, true} {
		got, err := f.external(open).Call(ctx, f.principal("alice"), getCapabilitiesTool, nil)
		if err != nil {
			t.Fatal(err)
		}
		var caps capabilities
		if err := json.Unmarshal(got.Structured, &caps); err != nil {
			t.Fatal(err)
		}
		if caps.Connection.ID != "cxcon_alice" || caps.Connection.DisplayName != "Claude Code" || caps.WorkspaceOpen != open {
			t.Fatalf("caps %s", got.Structured)
		}
		if caps.Generation.Budget == nil || caps.Generation.Budget.Remaining != "20.00" || caps.Generation.Budget.PeriodEnd.Day() != 1 {
			t.Fatalf("budget %+v", caps.Generation.Budget)
		}
		// No generator is bound in this fixture: the deployment has no models.
		if len(caps.Generation.Models) != 0 || !slices.Contains(caps.Hints, "当前部署没有可用的生成模型。") {
			t.Fatalf("models %+v hints %v", caps.Generation.Models, caps.Hints)
		}
		if strings.Contains(strings.Join(caps.Hints, ""), "没有打开") == open {
			t.Fatalf("workspace hint with open=%v: %v", open, caps.Hints)
		}
		if first := strings.SplitN(got.Summary, "\n", 2)[0]; first != "已连接创意空间账号，署名 Claude Code。" {
			t.Fatalf("summary %q", first)
		}
	}
	if _, err := f.external(false).Call(ctx, f.principal("alice"), getCapabilitiesTool, json.RawMessage(`{"probe":1}`)); externalCode(err) != "validation_failed" {
		t.Fatal("unknown argument accepted", err)
	}
}
