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
		desc = "任务需要素材、灵感或背景知识时按需查找资产库。按文字和归类精确检索，不按画面相似度；无文字信息的素材可换条件或按分组、标签浏览。查不到就如实说明，不编造素材。结果不含图片；需要查看时逐项用 read_asset 读取。返回完整标题、描述和素材 revision。按条数与字节分页；has_more 为 true 时可继续。relations_complete 为 false 时按 relations_query 调 read_library 读取完整关系。cursor_stale 表示库已变化，重新从首页查询。回收站只读元数据，不因此获得内容读取权限。"
		p = map[string]*schema.ParameterInfo{"asset_id": str("精确素材 ID"), "untagged": {Type: schema.Boolean, Desc: "只查未打标签的素材"}, "query": str("关键词，匹配标题、描述、正文和标签名"), "kind": {Type: schema.String, Enum: nodeTypes}, "group_id": str("read_library 返回的分组 ID"), "include_subgroups": {Type: schema.Boolean, Desc: "包含子分组"}, "tag_ids": list("最多 50 个已有标签 ID"), "tag_mode": {Type: schema.String, Enum: []string{"all", "any"}}, "min_rating": {Type: schema.Number}, "unrated": {Type: schema.Boolean}, "color": str("十六进制主色，如 #FF0000"), "shape": {Type: schema.String, Enum: []string{"landscape", "square", "portrait"}}, "view": {Type: schema.String, Enum: []string{"all", "recent", "favorites", "unclassified", "trash"}}, "sort": {Type: schema.String, Enum: []string{"recent", "oldest", "name", "rating"}}, "limit": limit, "cursor": str("上一页 next_cursor，筛选条件保持不变")}
	case readLibraryTool:
		desc = "筛选并分页读取资产库分组、分类、标签及 revision，返回平铺 items。delete_impact 为完整数据的删除影响预览：asset_count 含回收站、分组只计直接成员，分类 tag_count 计标签；promoted_group_count 为删除后上提的子组。has_more 时继续传 cursor；库变化报 cursor_stale。asset_relations 分页返回某素材全部标签/分组，首请求传搜索所得库版本。"
		p = map[string]*schema.ParameterInfo{"kind": {Type: schema.String, Enum: []string{"all", "group", "category", "tag", "asset_relations"}}, "id": str("结构 ID 精确筛选"), "query": str("名称关键词，最多 200 字"), "parent_id": str("只用于 group；null 表示顶层，缺省所有层级"), "category_id": str("只用于 tag；null 表示未分类，缺省所有分类"), "asset_id": str("asset_relations 必填，不与结构筛选混用"), "expected_library_revision": str("asset_relations 第一页必填，来自 relations_query"), "limit": {Type: schema.Integer, Desc: "默认 50，1 到 100"}, "cursor": str("上一页 next_cursor；其余筛选保持不变")}
	case readLibraryResultTool:
		desc = "分页读取本会话 90 天内的整理结果，包含完整前后值、关系和动作结果。跨运行可读；不重做业务写入。settling 只核对回执，未核清返回 in_progress，过期返回 result_expired。"
		p = map[string]*schema.ParameterInfo{"result_id": {Type: schema.String, Required: true}, "limit": {Type: schema.Integer, Desc: "默认 50，1 到 50"}, "cursor": str("上一页 next_cursor")}
	case organizeAssetsTool, trashAssetsTool, restoreAssetsTool:
		fields := map[string]*schema.ParameterInfo{"asset_id": {Type: schema.String, Required: true}, "expected_revision": {Type: schema.String, Required: true, Desc: "搜索返回的素材 revision"}}
		desc = "逐项移入回收站，items 为 1 到 50 个不重复素材。结果含每项状态和 revision、result_id；完整结果用 read_library_result。冲突不覆盖。"
		if name == restoreAssetsTool {
			desc = "逐项从回收站恢复，items 为 1 到 50 个不重复素材。ID 和 revision 来自 trash 搜索；不存在或不在回收站会明确返回。结果明细用 read_library_result。"
		}
		if name == organizeAssetsTool {
			desc = "逐项整理 1 到 50 个不重复素材，每项至少一个修改字段。文字和归类分别执行，可能 partial；返回全部项状态、revision、阶段和变更数量，完整前后值与关系通过 result_id 调 read_library_result。冲突不覆盖，不重发原调用。"
			fields["title"] = str("可选，1 到 200 字")
			fields["description"] = str("可选，0 到 1000 个 Unicode 码点，空串清空")
			for _, key := range []string{"add_tag_ids", "remove_tag_ids", "add_group_ids", "remove_group_ids"} {
				fields[key] = list("最多 50 个已有 ID；同一个 ID 不能同时增加和移除")
			}
		}
		p = map[string]*schema.ParameterInfo{"items": {Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: fields}}}
	case manageLibraryTool:
		desc = "按顺序执行 1 到 20 个结构动作，首个失败后剩余 not_run。已有对象要求 expected_revision，不自动刷新对象版本。同批新建 ID 须下一次调用才能使用。create_tag 同名复用且保留颜色分类。删除标签/分类不可恢复；分组删除上提子组，页面没有本次撤销入口。返回实际 affected_asset_count/affected_tag_count；0 与不适用区分。完整结果用 read_library_result。"
		p = map[string]*schema.ParameterInfo{"actions": {Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{"action": {Type: schema.String, Required: true, Enum: []string{"create_group", "rename_group", "move_group", "delete_group", "create_category", "rename_category", "delete_category", "create_tag", "edit_tag", "delete_tag"}}, "group_id": str("分组动作目标，非创建必填"), "tag_id": str("标签动作目标，非创建必填"), "category_id": str("分类动作目标，或标签的分类；标签传 null 移出分类"), "expected_revision": str("修改/删除已有对象必填，来自 read_library"), "name": str("创建/改名必填，1 到 80 字；edit_tag 可选"), "parent_id": str("create_group 可选，move_group 必填；null 为顶层"), "position": {Type: schema.Integer, Desc: "创建/移动分组的位置，非负"}}}}}
	case conversationAssetsTool:
		desc = "找回本对话出现过的内容，包括创作者发的附件和助手读过或成功采用的生成结果。不跨对话；每个修订只返回符合筛选条件的最新一次。优先按 kind、source 筛选，query 匹配消息正文（工具正文可能是 JSON）。需要内容时用 read_asset。翻页同时传回两个 next_before 字段，筛选条件保持不变。"
		p = map[string]*schema.ParameterInfo{"kind": {Type: schema.String, Enum: nodeTypes}, "source": {Type: schema.String, Enum: []string{"photographer", "assistant"}}, "query": str("消息文字，不区分大小写，最多 200 字"), "before_ordinal": {Type: schema.Integer, Required: true, Desc: "首次 0，之后 next_before_ordinal"}, "before_revision_id": str("之后传 next_before_revision_id，避免同条消息漏项"), "limit": limit}
	case saveLibraryTool:
		desc = "把画布节点当前内容存入资产库；同一修订已在库中时复用，只追加归类。返回实际资产 ID、revision 和是否复用；完整实际文字与标签创建/复用事实通过 read_library_result。支持最多 10 个 new_tags，同名复用保持原分类颜色。"
		p = map[string]*schema.ParameterInfo{"node_id": {Type: schema.String, Required: true}, "title": {Type: schema.String, Required: true, Desc: "最多 200 字"}, "description": {Type: schema.String, Required: true, Desc: "1 到 1000 个 Unicode 码点，不得为空白"}, "new_tags": {Type: schema.Array, Desc: "最多 10 个新标签，同名复用", ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{"name": {Type: schema.String, Required: true, Desc: "1 到 80 字"}, "category_id": str("已有分类 ID，可选")}}}, "group_ids": list("已有分组 ID"), "tag_ids": list("已有标签 ID")}
	}
	return &schema.ToolInfo{Name: name, Desc: desc, ParamsOneOf: schema.NewParamsOneOfByParams(p)}
}
