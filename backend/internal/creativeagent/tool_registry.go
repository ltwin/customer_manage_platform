package creativeagent

import (
	"context"
	"fmt"
	"net/url"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// A definition owns all projections of an Agent capability. Domain commands
// retain their own transactions, authorization and receipts.
type agentToolDefinition struct {
	entry     ToolEntry
	bind      func(*runtimeHandler) tool.InvokableTool
	deployed  func(*Service) bool
	allowed   func(*runtimeHandler) bool
	resources func(string, map[string][]string, bool) []resource
}

func agentToolDefinitions() []agentToolDefinition {
	read := func(name string) func(*runtimeHandler) tool.InvokableTool {
		return func(h *runtimeHandler) tool.InvokableTool { return &runtimeReadTool{handler: h, name: name} }
	}
	library := func(name string) func(*runtimeHandler) tool.InvokableTool {
		return func(h *runtimeHandler) tool.InvokableTool { return &libraryTool{handler: h, name: name} }
	}
	canvas := func(name string) func(*runtimeHandler) tool.InvokableTool {
		return func(h *runtimeHandler) tool.InvokableTool { return &canvasTool{handler: h, name: name} }
	}
	scene := func(name string) func(*runtimeHandler) tool.InvokableTool {
		return func(h *runtimeHandler) tool.InvokableTool { return &sceneTool{handler: h, name: name} }
	}
	skill := func(name string) func(*runtimeHandler) tool.InvokableTool {
		return func(h *runtimeHandler) tool.InvokableTool { return &skillTool{handler: h, name: name} }
	}
	fixed := func(rs ...resource) func(string, map[string][]string, bool) []resource {
		return func(string, map[string][]string, bool) []resource { return append([]resource(nil), rs...) }
	}
	libraryAllowed := func(h *runtimeHandler) bool { return h.libraryEnabled }
	generationEnabled := func(s *Service) bool { return len(s.generationModels()) > 0 }
	webEnabled := func(s *Service) bool { return s.fetcher != nil }
	definitions := []agentToolDefinition{
		{entry: ToolEntry{Key: loadSkillTool, Version: 2, DisplayName: "加载 Skill", EffectClass: "read_only"}, bind: skill(loadSkillTool), resources: fixed(resource{"skills", true})},
		{entry: ToolEntry{Key: readSkillTool, Version: 2, DisplayName: "读取 Skill 资源", EffectClass: "read_only"}, bind: skill(readSkillTool), resources: fixed(resource{"skills", false})},
		{entry: ToolEntry{Key: searchSkillsTool, Version: 1, DisplayName: "查找 Skill", EffectClass: "read_only"}, bind: skill(searchSkillsTool), resources: fixed(resource{"skills", true})},
		{entry: ToolEntry{Key: readResultTool, Version: 1, DisplayName: "读取运行结果", EffectClass: "read_only"}, bind: read(readResultTool), resources: fixed(resource{keyCanvas, false}, resource{keyNodes, false}, resource{keyLibrary, false})},
		{entry: ToolEntry{Key: searchAssetsTool, Version: 2, DisplayName: "查找资产库", EffectClass: "read_only"}, bind: library(searchAssetsTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, false})},
		{entry: ToolEntry{Key: readLibraryTool, Version: 2, DisplayName: "读取资产库结构", EffectClass: "read_only"}, bind: library(readLibraryTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, false})},
		{entry: ToolEntry{Key: organizeAssetsTool, Version: 1, DisplayName: "整理素材", EffectClass: "write", MaxImpact: 50}, bind: library(organizeAssetsTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, true})},
		{entry: ToolEntry{Key: manageLibraryTool, Version: 1, DisplayName: "管理资产库结构", EffectClass: "write", MaxImpact: 20}, bind: library(manageLibraryTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, true})},
		{entry: ToolEntry{Key: trashAssetsTool, Version: 1, DisplayName: "移入回收站", EffectClass: "write", MaxImpact: 50}, bind: library(trashAssetsTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, true})},
		{entry: ToolEntry{Key: restoreAssetsTool, Version: 1, DisplayName: "从回收站恢复", EffectClass: "write", MaxImpact: 50}, bind: library(restoreAssetsTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, true})},
		{entry: ToolEntry{Key: readLibraryResultTool, Version: 1, DisplayName: "读取整理结果", EffectClass: "read_only"}, bind: library(readLibraryResultTool), allowed: libraryAllowed, resources: fixed(resource{keyLibrary, false})},
		{entry: ToolEntry{Key: conversationAssetsTool, Version: 1, DisplayName: "找回对话内容", EffectClass: "read_only"}, bind: library(conversationAssetsTool)},
		{entry: ToolEntry{Key: saveLibraryTool, Version: 2, DisplayName: "存入资产库", EffectClass: "write", MaxImpact: 1}, bind: canvas(saveLibraryTool), allowed: libraryAllowed, resources: func(args string, _ map[string][]string, _ bool) []resource {
			node := stringArgument(args, "node_id")
			if node == "" {
				return wholeRun()
			}
			return []resource{{keyLibrary, true}, {keyCanvas, false}, {nodeKey(node), false}}
		}},
		{entry: ToolEntry{Key: readAssetTool, Version: 1, DisplayName: "读取引用素材", EffectClass: "read_only"}, bind: read(readAssetTool), resources: fixed(resource{keyCanvas, false}, resource{keyNodes, false}, resource{keyLibrary, false})},
		{entry: ToolEntry{Key: readCanvasTool, Version: 1, DisplayName: "读取画布", EffectClass: "read_only"}, bind: read(readCanvasTool), resources: fixed(resource{keyCanvas, false}, resource{keyNodes, false})},
		{entry: ToolEntry{Key: readHistoryTool, Version: 1, DisplayName: "读取对话历史", EffectClass: "read_only"}, bind: read(readHistoryTool), resources: fixed()},
		{entry: ToolEntry{Key: updatePlanTool, Version: 1, DisplayName: "更新计划", EffectClass: "read_only"}, bind: func(*runtimeHandler) tool.InvokableTool { return planTool{} }, resources: fixed(resource{keyPlan, true})},
		{entry: ToolEntry{Key: editCanvasTool, Version: 1, DisplayName: "修改画布", EffectClass: "write", MaxImpact: 100}, bind: canvas(editCanvasTool), resources: fixed(resource{keyCanvas, true}, resource{keyLibrary, false})},
		{entry: ToolEntry{Key: setNodePromptTool, Version: 1, DisplayName: "写入提示词", EffectClass: "write", MaxImpact: 1}, bind: canvas(setNodePromptTool), deployed: generationEnabled, resources: fixed(resource{keyCanvas, true})},
		{entry: ToolEntry{Key: generateNodeTool, Version: 1, DisplayName: "生成", EffectClass: "generate", MaxImpact: 1}, bind: canvas(generateNodeTool), deployed: generationEnabled, resources: generationResources},
		{entry: ToolEntry{Key: compressImageTool, Version: 1, DisplayName: "压缩图片", EffectClass: "write", MaxImpact: 1}, bind: canvas(compressImageTool), deployed: func(s *Service) bool { return s.compressor != nil }, resources: func(args string, _ map[string][]string, _ bool) []resource {
			node := stringArgument(args, "node_id")
			if node == "" {
				return wholeRun()
			}
			return []resource{{keyCanvas, false}, {nodeKey(node), true}}
		}},
		{entry: ToolEntry{Key: readSceneTool, Version: 1, DisplayName: "读取场景", EffectClass: "read_only"}, bind: scene(readSceneTool), resources: sceneResources(false)},
		{entry: ToolEntry{Key: editSceneTool, Version: 1, DisplayName: "修改场景", EffectClass: "write", MaxImpact: 2000}, bind: scene(editSceneTool), resources: sceneResources(true)},
		{entry: ToolEntry{Key: writeModelTool, Version: 1, DisplayName: "生成模型", EffectClass: "write", MaxImpact: 1}, bind: scene(writeModelTool), resources: sceneResources(false)},
		{entry: ToolEntry{Key: renderViewTool, Version: 1, DisplayName: "回看场景", EffectClass: "read_only"}, bind: scene(renderViewTool),
			allowed: func(h *runtimeHandler) bool { return h.held.Model.Capability.ImageInput }, resources: sceneResources(false)},
		{entry: ToolEntry{Key: exportShotsTool, Version: 1, DisplayName: "导出分镜", EffectClass: "write", MaxImpact: 48}, bind: scene(exportShotsTool),
			resources: func(args string, _ map[string][]string, _ bool) []resource {
				doc := stringArgument(args, "document_id")
				if doc == "" {
					return wholeRun()
				}
				return []resource{{"scene:" + doc, false}, {keyCanvas, true}}
			}},
		{entry: ToolEntry{Key: searchWebTool, Version: 1, DisplayName: "搜索网页", EffectClass: "read_only"}, bind: func(h *runtimeHandler) tool.InvokableTool { return &searchTool{handler: h} }, deployed: webEnabled, resources: func(args string, _ map[string][]string, _ bool) []resource {
			q := searchKey(stringArgument(args, "query"))
			if q == "" {
				return wholeRun()
			}
			return []resource{{"search:" + q, true}}
		}},
		{entry: ToolEntry{Key: readWebpageTool, Version: 1, DisplayName: "读取网页", EffectClass: "read_only"}, bind: func(h *runtimeHandler) tool.InvokableTool { return &webpageTool{handler: h} }, deployed: webEnabled, resources: func(args string, _ map[string][]string, _ bool) []resource {
			raw := stringArgument(args, "url")
			u, err := url.Parse(raw)
			if raw == "" || err != nil {
				return wholeRun()
			}
			return []resource{{"webpage:" + webpageKey(u), true}}
		}},
	}
	return definitions
}

func wholeRun() []resource { return []resource{{keyRun, true}} }
func generationResources(args string, upstream map[string][]string, rewired bool) []resource {
	node := stringArgument(args, "node_id")
	sources, ok := upstream[node]
	if node == "" || !ok || rewired {
		return wholeRun()
	}
	out := []resource{{keyCanvas, false}, {nodeKey(node), true}}
	for _, source := range sources {
		out = append(out, resource{nodeKey(source), false})
	}
	return out
}
func validateToolDefinitions(definitions []agentToolDefinition) error {
	seen := map[string]bool{}
	for _, d := range definitions {
		if seen[d.entry.Key] || d.entry.Key == "" || d.entry.Version < 1 || d.bind == nil {
			return fmt.Errorf("invalid or duplicate agent tool: %s", d.entry.Key)
		}
		seen[d.entry.Key] = true
	}
	return nil
}
func runtimeToolEntries() []ToolEntry { return deployedToolEntries(&Service{}) }
func deployedToolEntries(s *Service) []ToolEntry {
	out := []ToolEntry{}
	for _, d := range agentToolDefinitions() {
		if d.deployed == nil || d.deployed(s) {
			out = append(out, d.entry)
		}
	}
	return out
}
func (h *runtimeHandler) allowed(name string) bool {
	for _, d := range agentToolDefinitions() {
		if d.entry.Key == name {
			return (d.deployed == nil || d.deployed(h.service)) && (d.allowed == nil || d.allowed(h))
		}
	}
	return false
}
func (h *runtimeHandler) configureTools(_ context.Context) (adk.ToolsConfig, []adk.ChatModelAgentMiddleware, error) {
	config := adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{ExecuteSequentially: false}}
	if h.held.Model.Capability.ToolCalling {
		for _, d := range agentToolDefinitions() {
			if h.allowed(d.entry.Key) {
				config.Tools = append(config.Tools, d.bind(h))
			}
		}
	}
	return config, []adk.ChatModelAgentMiddleware{h}, nil
}
func runtimeToolVersion(name string) int {
	for _, d := range agentToolDefinitions() {
		if d.entry.Key == name {
			return d.entry.Version
		}
	}
	return 0
}
func toolResources(name, args string, upstream map[string][]string, rewired bool) []resource {
	for _, d := range agentToolDefinitions() {
		if d.entry.Key == name && d.resources != nil {
			return d.resources(args, upstream, rewired)
		}
	}
	return wholeRun()
}
