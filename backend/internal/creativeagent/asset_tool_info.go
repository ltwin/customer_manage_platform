package creativeagent

import "github.com/cloudwego/eino/schema"

func assetToolInfo(name string) *schema.ToolInfo {
	str := func(desc string) *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.String, Desc: desc} }
	list := func(desc string) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Array, Desc: desc, ElemInfo: &schema.ParameterInfo{Type: schema.String}}
	}
	limit := &schema.ParameterInfo{Type: schema.Integer, Required: true, Desc: "每页 1 到 20 项"}
	p := map[string]*schema.ParameterInfo{}
	desc := ""
	switch name {
	case searchAssetsTool:
		desc = "任务需要素材、灵感或背景知识时按需查找资产库。按文字和归类精确检索，不按画面相似度；无文字信息的素材可换条件或按分组、标签浏览。查不到就如实说明，不编造素材。结果不含图片；需要查看时逐项用 read_asset 读取。大结果会省略部分归类，tags_omitted、groups_omitted 表示省略数量；素材与翻页游标保持完整。"
		p = map[string]*schema.ParameterInfo{"query": str("关键词，匹配标题、描述、正文和标签名"), "kind": {Type: schema.String, Enum: nodeTypes}, "group_id": str("read_library 返回的分组 ID"), "include_subgroups": {Type: schema.Boolean, Desc: "包含子分组"}, "tag_ids": list("最多 50 个已有标签 ID"), "tag_mode": {Type: schema.String, Enum: []string{"all", "any"}}, "min_rating": {Type: schema.Number}, "unrated": {Type: schema.Boolean}, "color": str("十六进制主色，如 #FF0000"), "shape": {Type: schema.String, Enum: []string{"landscape", "square", "portrait"}}, "view": {Type: schema.String, Enum: []string{"all", "recent", "favorites", "unclassified"}}, "sort": {Type: schema.String, Enum: []string{"recent", "oldest", "name", "rating"}}, "limit": limit, "cursor": str("上一页 next_cursor，筛选条件保持不变")}
	case readLibraryTool:
		desc = "读取资产库分组树、分类与标签的 ID、名称和素材数，最多 500 项；truncated 表示有省略。用于查找素材或选择已有归类。"
	case conversationAssetsTool:
		desc = "找回本对话出现过的内容，包括摄影师发的附件和助手读过或成功采用的生成结果。不跨对话；每个修订只返回符合筛选条件的最新一次。优先按 kind、source 筛选，query 匹配消息正文（工具正文可能是 JSON）。需要内容时用 read_asset。翻页同时传回两个 next_before 字段，筛选条件保持不变。"
		p = map[string]*schema.ParameterInfo{"kind": {Type: schema.String, Enum: nodeTypes}, "source": {Type: schema.String, Enum: []string{"photographer", "assistant"}}, "query": str("消息文字，不区分大小写，最多 200 字"), "before_ordinal": {Type: schema.Integer, Required: true, Desc: "首次 0，之后 next_before_ordinal"}, "before_revision_id": str("之后传 next_before_revision_id，避免同条消息漏项"), "limit": limit}
	case saveLibraryTool:
		desc = "把画布节点当前内容存入资产库；同一修订已在库中时复用，只追加归类。只能使用已有分组和标签，新建标签属于整理资产库。"
		p = map[string]*schema.ParameterInfo{"node_id": {Type: schema.String, Required: true}, "title": {Type: schema.String, Required: true, Desc: "最多 200 字"}, "description": str("素材描述"), "group_ids": list("已有分组 ID"), "tag_ids": list("已有标签 ID")}
	}
	return &schema.ToolInfo{Name: name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(p)}
}
