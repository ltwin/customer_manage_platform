<!-- 已批准的目标契约，尚未实现。项目 api 契约里目前没有「创建助手运行」的章节。本节只覆盖这次扩展的部分：scene_ref 片段、scene_ref 消息块、scene_context.selected_object_ids 的弃用。其余字段以 api/openapi.yaml 为准。 -->

## api: ADD 助手指令中的场景引用

### 用途与调用
- **用途**：摄影师在助手消息里按位置引用 3D 场景，也可以引用场景里的部分物件。服务端在创建运行时校验这些引用，把场景标题和物件名冻结成消息快照，并据此告诉助手每处引用指的是什么。
- **消费者**：创意空间助手面板（`AgentPanel`）。外部助手（MCP）不使用。
- **调用**：`POST /api/v1/creative/conversations/{id}/runs`。请求体是创意写操作的通用信封 `{operation_id, client_created_at, payload}`，`payload` 为 `CreativeAgentRunPayload`。本节只说明 `payload.instruction.instruction_segments[]` 中新增的 `scene_ref` 片段，以及 `scene_context` 的变化。
- **权限**：已登录，账号具备创意空间能力；会话属于当前账号；本会话的外发授权 `egress_consent_id` 必须包含 `canvas_metadata`。引用的场景必须属于当前账号。

### 请求：`scene_ref` 片段
| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `type` | string | 是 | 固定为 `"scene_ref"` |
| `document_id` | string | 是 | 场景文档 ID（`type_key = scene`）。不能为空 |
| `object_ids` | string[] | 否 | 引用的物件 ID，1…50 个，不能为空串，不能重复。上限按单个片段计算，同一场景的多个片段各自计。省略表示引用整个场景；传空数组算格式错误 |

- `scene_ref` 不能带 `text`、`skill_id`、`skill_version_id`、`content_revision_id`、`command`；其他类型的片段不能带 `document_id`、`object_ids`。
- 片段按出现位置组成一条消息。同一个场景可以出现多次，每次是一个独立的引用。一次提交里不同的 `document_id` 最多 5 个。
- 不同提交之间不去重，也不合并。

### 请求：`scene_context`（变更）
- `selected_object_ids` 标为 **deprecated**：仍然必传、仍然接受，最多 50 个。新前端固定传 `[]`，选中的物件改由 `scene_ref` 表达。
- 传入非空的 `selected_object_ids` 时，服务端按原规则处理：在消息末尾追加 `scene_selection` 块，运行输入里写明选中了哪些物件。
- `document_id` 的含义不变：表示「正在编辑的场景」，不计入 5 个场景的上限。

### 成功响应
202，响应体与原来相同。消息体中，每个 `scene_ref` 片段在原位置变成一个 `scene_ref` 消息块：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `type` | string | 是 | 否 | `"scene_ref"` |
| `scene.document_id` | string | 是 | 否 | 场景文档 ID |
| `scene.title` | string | 是 | 否 | 发送时的场景标题快照，最长 200 字；场景没有标题时为空字符串 |
| `scene.revision` | integer | 否 | 否 | 发送时场景的版本号。`scene_ref` 块必返；旧的 `scene_selection` 块不返 |
| `scene.objects` | array | 是 | 否 | 引用整个场景时为 `[]`；否则按请求顺序列出，0…50 项。`scene_selection` 块仍至少 1 项 |
| `scene.objects[].id` | string | 是 | 否 | 物件 ID |
| `scene.objects[].name` | string | 是 | 否 | 发送时的物件名，没有名称为空字符串；`missing` 为 true 时也为空字符串 |
| `scene.objects[].missing` | boolean | 否 | 否 | 为 true 表示发送时这个物件已经不在场景里；省略即 false |

- 运行的冻结输入里，每个 `scene_ref` 会在原位置写入一行说明，告诉助手引用了哪个场景、哪些物件。已删除的物件只给出物件 ID 和「已不在场景里」，因为服务端只读当前场景，拿不到它的原名。之后重试，使用的是同一份冻结输入。
- 本轮输入预算按补齐后的引用说明计算，包括名称、ID、已删除提示和每条消息的开销。
- 被引用的场景也会加入「自上次写入后的变化」摘要的候选。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | `scene_ref` 缺少 `document_id`；`object_ids` 是空数组、含空串、有重复或超过 50 个；带了其他类型片段的字段；其他类型的片段带了 `document_id` / `object_ids` | 视为客户端缺陷，草稿保留 |
| 413 | `creative_context_limit` | 一次提交中不同的场景超过 5 个；或者补齐标题、物件名和引用说明后，整条消息超过消息长度上限（32 KiB，与现有的消息体上限相同） | 提示「一条消息最多引用 5 个场景」，或提示消息过长 |
| 404 | `not_found` | 场景不存在、属于其他账号、不是场景类型，或已进入回收站 | 提示「引用的场景已不可用，请重新选择」，草稿保留 |
| 403 | 现有外发授权错误码 | 授权不含 `canvas_metadata` | 走现有的外发授权流程 |

以上失败都不会创建运行、不占用槽位、不写入消息。用同一个 `operation_id` 重放时，返回第一次的回执。

### 调用示例
```http
POST /api/v1/creative/conversations/ccnv_1/runs
Content-Type: application/json
```
```json
{
  "operation_id": "6d2f0b8e-5c3a-4f1e-9b7d-2a1c0e9f8b7a",
  "client_created_at": "2026-11-09T10:00:00Z",
  "payload": {
    "model_key": "default",
    "egress_consent_id": "ccns_1",
    "instruction": {
      "schema_version": 1,
      "instruction_segments": [
        {"type": "text", "text": "把"},
        {"type": "scene_ref", "document_id": "cwdoc_living", "object_ids": ["sobj_lamp", "sobj_key"]},
        {"type": "text", "text": "改成金属，"},
        {"type": "scene_ref", "document_id": "cwdoc_living", "object_ids": ["sobj_sofa", "sobj_table"]},
        {"type": "text", "text": "改成胡桃木"}
      ]
    },
    "scene_context": {"document_id": "cwdoc_living", "selected_object_ids": []}
  }
}
```
消息体中对应的块（摘录，`sobj_table` 在发送前已被删除）：
```json
{"type": "scene_ref", "scene": {"document_id": "cwdoc_living", "title": "客厅", "revision": 12,
  "objects": [{"id": "sobj_sofa", "name": "沙发"}, {"id": "sobj_table", "name": "", "missing": true}]}}
```
失败示例：
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```

### 兼容与变更
- 只扩展，不破坏：片段和块的 `type` 枚举新增 `scene_ref`；`CreativeAgentMessageScene` 新增可选的 `revision` 和 `objects[].missing`，`objects.minItems` 从 1 放宽到 0（只有 `scene_ref` 块可能为空）。
- 旧前端：遇到 `scene_ref` 块会按未知块显示成通用引用 chip，不会崩溃；它提交的非空 `selected_object_ids` 照常处理。
- 前后端同一版本上线。弃用的 `selected_object_ids` 等旧页面自然淘汰后另开 change 删除。
