# 接口约定

记录已交付接口的全局约定与具体契约。Epic 的目标 API 在 Epic contracts.md 定义，子 Change 引用批准版本；独立 Change 在自己的 contracts.md 提 delta。归档后才合入本文件。

起草要求（文档责任，不代表 CLI 已逐字段校验）：
- **必填**：用途与消费者、调用方式、鉴权与资源权限、请求字段、成功响应、失败语义、调用示例、兼容性。无参数或无响应体须明确写“无”；公开接口须写“无需鉴权”及访问边界。
- **按场景必填**：列表接口的分页/过滤/排序；写操作的副作用/原子性/重复提交；并发更新的冲突处理；异步任务/回调/流式接口的生命周期；有配额或执行期限时的限流/超时。未涉及的机制可以不设计，但要说明“不适用 + 原因”或“不支持 + 调用方影响”。
- **可选**：时序图、选型背景、SDK 调用示例、额外性能目标；它们不能替代字段和行为定义。
- 这里的“必填”指文档必须说明；字段表中的“必传/必返”和“可为 null”分别定义协议行为，不能混为一谈。
- 允许引用已有公共约定或权威 schema，须写准确文件/章节或版本、适用字段及本接口差异；不复制第二套定义，也不能只写“同上”“标准响应”“object”“正常返回”。
- 正文用中文说明语义，协议字段保留真实名称；一项 API 一个二级标题，内部用三级及更低标题。MODIFY 必须给出完整的新章节。
- 未决项写出待决问题、影响的接口/子项和解决时点；不得用 TODO、空表、占位符或编造默认值冒充已完成设计。实施依赖的决定须先明确。

<!-- 以下是单接口写作骨架，起草时替换占位内容，不把本注释作为已完成契约。
项目正文标题：## <接口中文名称>
Epic/Change delta 标题：## api: ADD <接口中文名称>（修改为 MODIFY，删除为 REMOVE）
REMOVE 使用精简退役说明：原接口引用、删除原因、受影响消费者、替代接口或无替代说明、迁移/下线安排；无需重写已删除接口的请求响应。

## <接口中文名称>

### 用途与调用【必填】
- 用途：解决什么业务操作，什么结果表示成功。
- 消费者：具体客户端/服务或目标使用者。
- 调用：HTTP 方法 + 完整路径；非 HTTP 写协议、操作名和消息方向。
- 格式：请求/响应媒体类型、协议版本；明确引用适用的公共约定。
- 权限：身份如何传递、角色/权限要求、租户或资源归属边界；公开访问也须明确。

### 请求【必填】
按 path/query/header/body 标注每个业务相关字段；无需逐项列出通用传输头。

| 字段路径 | 位置 | 类型/格式 | 必传 | 可为 null | 缺省行为 | 约束与业务含义 |
|---|---|---|---|---|---|---|
| <例如 items[].quantity> | body | integer | 是 | 否 | 缺失报错 | <数值范围、单位、业务含义> |

嵌套对象展开到调用方需要填写的字段，或准确引用其 schema；数组写元素类型、数量限制，枚举列出合法值及含义。
区分缺失、null、空字符串、空数组；条件必传写触发条件。PATCH 尤其要说明省略与清空的差异。
时间写格式和时区，金额写单位与精度；没有请求参数明确写“无”，不要保留空表。

### 成功响应【必填】
- HTTP 状态码（非 HTTP 写成功信号），必要响应头；多种成功结果分别说明。

| 字段路径 | 类型/格式 | 必返或出现条件 | 可为 null | 业务含义与约束 |
|---|---|---|---|---|
| <实际字段> | <具体类型> | <条件> | <是/否> | <说明> |

展开嵌套结构或引用准确 schema；无响应体明确写“无响应体”。异步受理不等于业务完成。

### 失败响应【必填】
- 错误体结构：逐字段定义，或引用公共错误 schema 的准确位置和版本。

| HTTP 状态码/失败信号 | 业务错误码 | 触发条件 | 调用方处理/是否可重试 |
|---|---|---|---|
| <实际值> | <稳定错误码> | <具体条件> | <如何修正；重试条件或不可重试> |

覆盖适用的参数错误、身份/资源权限失败、不存在、状态冲突及业务拒绝；不适用者说明原因。
通用服务异常可引用公共约定。不能只列“400/500”；有错误码就说明含义，没有独立业务错误码则明确写“无，按状态码区分”。

### 交互规则【按场景必填】
- 列表：分页参数及默认/最大值、游标/页码含义、排序与稳定次序、过滤语义、空结果及末页信号。
- 写操作：修改哪些资源；成功/失败后可观察的状态，是否可能部分成功；重复调用是否重复生效。
  支持幂等时写 key 的位置、作用域、有效期和相同 key 不同请求的处理；不支持则说明重复调用风险。
- 并发更新：版本/条件更新要求、冲突响应或覆盖规则。
- 异步/回调/流式：受理、进度、完成、失败的信号，结果取得方式；涉及取消、重放、顺序和去重时说明规则。
- 存在配额/期限：限流边界、超时行为、重试提示；未知数值标为待决，不编造承诺。
未触发的项可合并说明不适用原因，不为填模板新增分页、幂等、限流等功能。

### 调用示例【必填】
给出一组能对照上述字段表阅读的完整请求与成功响应，以及至少一组适用的失败响应（标明触发请求/条件）。
HTTP 示例包含方法、路径、必要头、实际状态码及完整 body；无 body 如实省略。使用虚构业务数据，不放真实密钥。
JSON 示例必须合法，不用省略号替代字段；所有值、字段和错误码与定义一致。真正无失败分支的协议需说明原因。

### 兼容与变更【必填】
- 新增/兼容扩展/破坏性变更，以及判断依据；不能仅因“新增字段”就断言兼容。
- 已有消费者是否受影响，默认行为是否改变；破坏性变更补迁移、过渡期或版本安排。
- 新接口可写“首次定义，无已有消费者迁移”；沿用项目版本策略须准确引用。

### 补充说明【可选】
时序图、设计取舍、SDK 示例、性能目标等；没有则删掉本节。

-->

## 读取文档

### 用途与调用
- **用途**：读取文档的当前状态；按 `type_key` 附加类型字段。
- **消费者**：前端文档视图（现有 `internal.document`；以后的策划、场景编辑器）。
- **调用**：`GET /api/v1/creative/documents/{id}`，响应 `application/json`。
- **权限**：`creative_read`；文档属于当前账号。回收站中的文档也可读。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 文档 ID |

无查询参数和请求体。

### 成功响应
200。通用字段：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `id` | string | 是 | 否 | — |
| `type_key` | string | 是 | 否 | 判别字段 |
| `title` | string | 是 | 否 | ≤ 200 |
| `revision` | CreativeRevision | 是 | 否 | 当前修订号 |
| `content_revision_id` | string | 是 | 否 | 当前快照 |
| `unit_revs` | object（单元 → CreativeRevision） | 是 | 否 | 当前单元修订号 |
| `trashed_at` | string(date-time) | 是 | 是 | — |
| `created_at` / `updated_at` | string(date-time) | 是 | 否 | — |

类型分支（`oneOf`，按 `type_key` 判别）：
- `internal.document`：另有 `content`（`CreativeContentRevision`，与现状相同）。
- 其他类型由各自的契约定义（策划：V6-079；场景：epic `scene-editor-3d` 的 `api: 读取场景`）。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 不存在、不属于当前账号、类型未注册 | 关闭视图 |
| 403 | `creative_usage_denied` | 快照内容的展示授权已撤销（沿用现有映射） | 显示不可用 |
| 403 | 现有 `CodeForbidden` | 账号没有创意空间能力 | 沿用现有处理 |

### 调用示例
```http
GET /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b
Authorization: Bearer <token>
```
```json
{"id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "type_key": "internal.document", "title": "参考说明",
 "revision": "3", "content_revision_id": "ccrv_5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d",
 "unit_revs": {"meta": "1", "body": "3"}, "trashed_at": null,
 "created_at": "2026-10-16T08:00:00Z", "updated_at": "2026-10-16T09:12:00Z",
 "content": {"id": "ccrv_5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d", "content_id": "ccnt_1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
             "kind": "text", "sequence": "1", "payload": {"text": "灯光以侧逆光为主。"}}}
```
（`content` 的完整字段以现有 `CreativeContentRevision` schema 为准，示例只列出主要字段。）
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```

### 兼容与变更
在现有路由上扩展：新增 `unit_revs`、`trashed_at`、`created_at`、`updated_at`，schema 改为按 `type_key` 判别的 `oneOf`。`internal.document` 的原字段全部保留。TS 消费者需要按 `type_key` 收窄（`DocumentView.tsx`、`WorkspacePage.tsx`）。

## 提交文档命令

### 用途与调用
- **用途**：对文档做一批原子编辑，成功时产生一个新修订。
- **消费者**：各类型的编辑器；助手工具经领域入口 `creativedocument.Command`（写入者为助手和 `run_id`），不经 HTTP。
- **调用**：`POST /api/v1/creative/documents/{id}/commands`，`Idempotency-Key` 必填。
- **权限**：`manual_write`；文档属于当前账号且不在回收站。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `expected` | object（单元 → CreativeRevision 或 null；schema `CreativeDocumentExpectedUnits`） | 非 `restore` 批次必传 | 必须包含本批涉及的每个单元。值为修订号：预期该单元存在且修订号相同；值为 null：预期该单元当前不存在。可以多给，多给的不检查。`restore` 批次不得出现 |
| `expected_revision` | CreativeRevision | `restore` 批次必传 | 预期的文档当前修订号。只用于 `restore`，其他批次出现时返回 400 |
| `operations` | array，1–200 项 | 是 | 每项必有 `op`；通用操作如下，其余由类型定义 |

通用操作：

| op | 字段 | 涉及单元 |
|---|---|---|
| `rename` | `title`（1–200 字符，去首尾空白后非空） | `meta` |
| `restore` | `revision`（1 ≤ revision < 当前修订）；**必须单独成批** | 全部单元。在文档行锁内比较 `expected_revision` 与文档当前修订号，不使用 `expected` |
| `restore_units` | `from_revision`（同上）、`units`（1–4096 个单元名，可含 `meta`）；**必须单独成批** | 列出的单元。当前不存在、或在 `from_revision` 中不存在的单元会改变存在性，此时类型同时声明集合单元；当前不存在的单元在 `expected` 中写 null |

同一批里每个单元最多被一个操作涉及；请求体 ≤ 1 MiB。

**引用校验**：只校验新快照相对当前快照**新增**的内容修订引用，已在当前快照中的引用不复查。
- 普通批次（`rename` 与类型操作）新增的引用：必须存在且展示授权有效。不存在返回 404 `not_found`，授权无效返回 403 `creative_usage_denied`。
- `restore` / `restore_units` 从修订 k 带回的引用：必须登记在本文档修订 k 的引用表中，否则返回 400 `validation_failed`。这一条不检查展示授权：授权已撤销的引用照常恢复，读取时由类型显示为不可用占位（沿用 V6-079 §3.3）。

### 成功响应
200（同一 operation 回放原回执）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `revision` | CreativeRevision | 是 | 新修订号。`changed_units` 里每个单元的新修订号都等于它，所以回执不再单独返回单元修订号映射 |
| `changed_units` | string[] | 是 | 本次推进了修订号的单元；`reset_required = true` 时为空数组 |
| `annotated_units` | string[] | 是 | 本次只做标注的单元；`reset_required = true` 时为空数组 |
| `removed_units` | string[] | 是 | 本次被删除的单元；`reset_required = true` 时为空数组 |
| `reset_required` | boolean | 是 | `true` 表示改动范围太大，回执不列明细，调用方应重新读取文档。整份 `restore` 恒为 `true`；其他批次在三个列表序列化后合计超过 48 KiB 时为 `true` |

回执始终小于执行器的 64 KiB 上限；重放返回首次提交时保存的同一份回执（`creativeops` 原样存储）。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法；单元重复；`expected` 缺少涉及单元；`restore` 或 `restore_units` 未单独成批；`restore` 缺少 `expected_revision`，或非 `restore` 批次带了它；恢复的修订号越界，或该修订没有保存历史（迁移只回填了当前修订）；恢复带回的引用不在该修订的引用登记中；类型校验失败 | 修正，不可原样重试 |
| 403 | `creative_usage_denied` | 普通批次新增的引用没有有效的展示授权（沿用现有映射） | 换用其他内容 |
| 404 | `not_found` | 文档不存在或不属于当前账号；普通批次新增的引用不存在 | 关闭视图或修正引用 |
| 409 | `creative_document_conflict` | 任一涉及单元的当前状态与 `expected` 不符（修订号不同；预期存在但已不存在；预期不存在但已存在），或 `restore` 的 `expected_revision` ≠ 当前文档修订号。`details`（`CreativeDocumentConflictDetails`）为 `{conflicts:[{unit, expected_rev, current_rev}]}`：`expected_rev` / `current_rev` 为 null 表示预期 / 当前不存在；`restore` 冲突时 `unit` 为 `"*"`，两个值都是文档修订号 | 重读文档，在冲突界面处理后再提交 |
| 409 | `creative_state_conflict` | 文档在回收站 | 先恢复文档 |
| 409 | `idempotency_conflict` | 公共约定 | — |
| 413 | `creative_size_limit` | 请求体 > 1 MiB，或新快照 > 512 KiB，或单元 > 4096 | 缩小改动 |

### 交互规则
- **原子**：整批成功或都不生效；冲突在任何写入之前判定。
- **幂等**：公共约定。
- **并发**：普通批次按单元乐观并发；整份恢复按文档修订号检查。规则见 design。
- 不适用：分页、异步、限流（沿用全局请求体上限）。

### 调用示例
```http
POST /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/commands
Authorization: Bearer <token>
Idempotency-Key: 3c2b1a09-8f7e-4d6c-9b5a-4e3d2c1b0a99
Content-Type: application/json
```
```json
{"operation_id": "3c2b1a09-8f7e-4d6c-9b5a-4e3d2c1b0a99", "client_created_at": "2026-10-16T09:20:00Z",
 "payload": {"expected": {"meta": "1", "body": "3"},
             "operations": [{"op": "rename", "title": "灯光参考说明"},
                            {"op": "set_body", "text": "灯光以侧逆光为主，补一盏低位冷色。"}]}}
```
```json
{"revision": "4", "changed_units": ["meta", "body"], "annotated_units": [], "removed_units": [], "reset_required": false}
```
冲突：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
           "details": {"conflicts": [{"unit": "body", "expected_rev": "3", "current_rev": "5"}]}}}
```

整份恢复（摄影师基于修订 4 恢复到 2，但助手已写出修订 5）：
```json
{"operation_id": "8e9f0a1b-2c3d-4e4f-9a5b-6c7d8e9f0a1b", "client_created_at": "2026-10-16T09:40:00Z",
 "payload": {"expected_revision": "4", "operations": [{"op": "restore", "revision": "2"}]}}
```
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
           "details": {"conflicts": [{"unit": "*", "expected_rev": "4", "current_rev": "5"}]}}}
```
整份恢复成功时的回执：
```json
{"revision": "6", "changed_units": [], "annotated_units": [], "removed_units": [], "reset_required": true}
```

### 兼容与变更
首次定义。`set_body` 是 `internal.document` 的类型操作，只用于内部视图和测试。

2026-10-16（`scene-document-shell`）：`restore_units` 的单元上限从 200 放宽到 4096，与单元容量一致，使删除大分组后的撤销能在一个请求里原子完成；请求体 1 MiB 限制不变。只放宽，旧客户端不受影响。

## 文档进回收站

- **用途**：把文档放进回收站，之后只读。消费者是前端文档菜单。
- **调用**：`POST /api/v1/creative/documents/{id}/trash`，`Idempotency-Key` 必填，`payload` 为 `{}`。
- **权限**：`manual_write`；只有摄影师经 HTTP 操作，助手没有对应工具。
- **成功响应**：200，`{id, trashed_at, updated_at}`。已在回收站时同样返回 200，`trashed_at` 保持原值。
- **失败响应**：
  - 404 `not_found`：文档不存在或不属于当前账号。
  - 409 `idempotency_conflict`：公共约定。
- **交互**：不产生修订；挂它的节点读取时 `document_trashed = true`。
- **示例**：
```json
{"operation_id": "7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d", "client_created_at": "2026-10-16T10:00:00Z", "payload": {}}
```
```json
{"id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "trashed_at": "2026-10-16T10:00:01Z", "updated_at": "2026-10-16T10:00:01Z"}
```
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```
- **兼容**：首次定义。

## 文档移出回收站

- **用途**：恢复回收站中的文档，恢复后可以照常写。
- **调用**：`POST /api/v1/creative/documents/{id}/untrash`，`Idempotency-Key` 必填，`payload` 为 `{}`。
- **权限**：同「文档进回收站」。
- **成功响应**：200，`{id, trashed_at: null, updated_at}`。不在回收站时同样返回 200。
- **失败响应**：同「文档进回收站」。
- **示例**：
```json
{"operation_id": "1b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e", "client_created_at": "2026-10-16T10:05:00Z", "payload": {}}
```
```json
{"id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "trashed_at": null, "updated_at": "2026-10-16T10:05:01Z"}
```
- **兼容**：首次定义。

## 彻底删除文档

- **用途**：不可恢复地删除回收站中的文档及其全部历史。
- **调用**：`POST /api/v1/creative/documents/{id}/purge`，`Idempotency-Key` 必填，标准命令封套，`payload` 为 `{}`。与创意模块现有的 `POST /creative/library/empty-trash`、`POST /creative/assets/{id}/trash` 一致：创意接口没有 `DELETE` 路由，全部走 `creativeops` 执行器，首次执行和重放是同一条路径。
- **权限**：`manual_write`；只有摄影师经 HTTP 操作，助手没有对应工具。
- **成功响应**：200，`{id, purged: true}`。同一 key 重放返回同一份回执。
- **失败响应**：

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 文档不存在、不属于当前账号或已被删除（非重放） | 刷新列表 |
| 409 | `creative_state_conflict` | 文档不在回收站 | 先进回收站 |
| 409 | `idempotency_conflict` | 公共约定 | — |

- **交互**：同一事务删除会话指针、引用行、修订行和文档行；挂它的节点之后读取为不可用（`status.content_state = "unavailable"`）；快照内容的物理保留见 `data: 通用文档`。
- **示例**：
```http
POST /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/purge
Idempotency-Key: 9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a
Content-Type: application/json

{"operation_id": "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a", "client_created_at": "2026-10-16T11:00:00Z", "payload": {}}
```
```json
{"id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "purged": true}
```
```json
{"error": {"code": "creative_state_conflict", "message": "只能彻底删除回收站中的文档"}}
```
- **兼容**：首次定义。

## 列出文档

### 用途与调用
- **用途**：按类型列出本账号的文档，供「引用已有文档」选择框和回收站页使用。
- **调用**：`GET /api/v1/creative/documents`。
- **权限**：`creative_read`。

### 请求（query）
| 字段 | 类型 | 必传 | 缺省 | 说明 |
|---|---|---|---|---|
| `type` | string | 否 | 全部类型 | 精确匹配 `type_key` |
| `q` | string ≤ 100 | 否 | 无 | 标题包含，大小写不敏感 |
| `trashed` | boolean | 否 | `false` | `true` 只列回收站中的，`false` 只列不在回收站的 |
| `limit` | integer 1–50 | 否 | 20 | — |
| `cursor` | string | 否 | 无 | 上一页的 `next_cursor`；绑定账号与过滤条件，条件变化后使用返回 400 |

### 成功响应
200：`{items:[{id, type_key, title, revision, trashed_at, updated_at, summary}], next_cursor}`。
- 按 `updated_at` 倒序，同时间按 `id` 倒序。
- `summary` 为类型摘要（JSON 对象）；类型未注册时为 null。
- `next_cursor` 为 null 表示末页；没有结果时 `items` 为空数组。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 参数越界，或游标非法、与条件不符 | 去掉游标重新开始 |

### 调用示例
```http
GET /api/v1/creative/documents?type=internal.document&limit=2
```
```json
{"items": [{"id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "type_key": "internal.document", "title": "灯光参考说明",
            "revision": "4", "trashed_at": null, "updated_at": "2026-10-16T09:20:01Z", "summary": {"preview": "灯光以侧逆光为主，补一盏低位冷色。"}}],
 "next_cursor": null}
```
```json
{"error": {"code": "validation_failed", "message": "limit 必须在 1 到 50 之间"}}
```

### 兼容与变更
首次定义。

## 列出文档修订

- **用途**：历史版本列表、「本轮改动」清单，以及告诉助手「自你上次写入后发生了什么」。
- **调用**：`GET /api/v1/creative/documents/{id}/revisions`。
- **权限**：`creative_read`。回收站中的文档也可以读。
- **请求（query）**：
  - `run_id`（可选，只列该运行写入的修订）
  - `since_revision`（可选，CreativeRevision，只列修订号大于它的修订；可与 `run_id` 同时使用）
  - `limit`（1–100，缺省 50）
  - `cursor`（上一页的 `next_cursor`，绑定文档、`run_id` 与 `since_revision`）
- **成功响应**：200，`{items:[{revision, actor_kind, run_id, restored_from, changed_units, annotated_units, removed_units, title, created_at}], next_cursor}`。按 `revision` 倒序；`run_id` 和 `restored_from` 可为 null；不含快照内容。`annotated_units` 是这个修订只做了标注、修订号没变的单元；`removed_units` 是这个修订删除的单元，删除时 `changed_units` 只含集合单元，具体删了什么要看它；整份恢复的 `removed_units` 为空，由 `changed_units = ["*"]` 表示全部重置（这两个字段实施时补充，「本轮改动」清单和告知助手需要区分改动、确认和删除）。
- **失败响应**：
  - 400 `validation_failed`：参数或游标非法。
  - 404 `not_found`：文档不存在或不属于当前账号。
- **示例**：
```http
GET /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/revisions?limit=2
```
```json
{"items": [
  {"revision": "4", "actor_kind": "photographer", "run_id": null, "restored_from": null, "changed_units": ["meta", "body"], "title": "灯光参考说明", "created_at": "2026-10-16T09:20:01Z"},
  {"revision": "3", "actor_kind": "agent", "run_id": "crun_6f5e4d3c-2b1a-4f9e-8d7c-6b5a4f3e2d1c", "restored_from": null, "changed_units": ["body"], "title": "参考说明", "created_at": "2026-10-16T09:01:12Z"}],
 "next_cursor": "eyJyIjoiMyJ9"}
```
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```
- **兼容**：首次定义。

## 读取文档旧修订

- **用途**：查看某个旧版本，或者为对照取基准内容。
- **调用**：`GET /api/v1/creative/documents/{id}/revisions/{revision}`，query 可选 `units`（逗号分隔，最多 200 个，只返回这些单元的内容）。
- **权限**：`creative_read`。
- **成功响应**：200。
  - 字段：`{revision, title, actor_kind, run_id, restored_from, changed_units, annotated_units, removed_units, unit_revs, created_at, view}`。
  - `unit_revs` 是该修订提交时的单元修订号（实施时补充，按单元恢复前判断哪些单元在此之后被改过要用到）。
  - `view` 是类型的读取分支，结构与「读取文档」的类型字段相同，按该修订的快照计算。
  - 给了 `units` 时，`view` 只包含这些单元；`units` 的裁剪由类型实现，不支持裁剪的类型返回全部。
- **失败响应**：
  - 400 `validation_failed`：`revision` 非法，或 `units` 超过 200 个。
  - 404 `not_found`：文档不存在（「文档不存在」），或文档存在但没有这个修订（「修订不存在」，例如迁移前的旧修订号）。
- **示例**：
```http
GET /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/revisions/3?units=body
```
```json
{"revision": "3", "title": "参考说明", "actor_kind": "agent", "run_id": "crun_6f5e4d3c-2b1a-4f9e-8d7c-6b5a4f3e2d1c", "restored_from": null,
 "changed_units": ["body"], "created_at": "2026-10-16T09:01:12Z", "view": {"body": "灯光以侧逆光为主。"}}
```
```json
{"error": {"code": "not_found", "message": "修订不存在"}}
```
- **兼容**：首次定义。

## 读取文档工作会话

- **用途**：编辑视图打开助手面板时，找到这份文档在当前画布上的工作会话。
- **调用**：`GET /api/v1/creative/documents/{id}/conversation?canvas_id=<id>`（`canvas_id` 必传）。
- **权限**：`creative_read`；文档和画布都属于当前账号。
- **成功响应**：200，`{conversation_id}`。没有指针，或指向的会话已被删除时为 `{"conversation_id": null}`。
- **写入**：不开放 HTTP。由发送消息、类型的助手工具在同一事务里调用 `creativedocument.SetConversation` 顺带写入（V6-079 §7.6）。
- **失败响应**：
  - 400 `validation_failed`：缺少 `canvas_id`。
  - 404 `not_found`：文档或画布不存在。
- **示例**：
```http
GET /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/conversation?canvas_id=ccv_2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d6e
```
```json
{"conversation_id": "cconv_8e7d6c5b-4a3f-4e2d-9c1b-0a9f8e7d6c5b"}
```
```json
{"error": {"code": "validation_failed", "message": "缺少 canvas_id"}}
```
- **兼容**：首次定义。

## 画布新建与放回文档节点

- **用途**：在画布上新建一份文档并放置其节点，或把已有文档放到画布上。
- **调用**：现有画布命令 `POST /api/v1/creative/canvases/{id}/commands` 的 `add_node` 动作（请求外形和回执不变），新增字段：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `type_key` | string | 是（现有） | 节点类型。新增允许值：已注册文档类型声明的节点类型（如 `core.scene`），新建时该文档类型必须可新建。放回时必须等于文档类型注册的节点类型 |
| `document_id` | string | 否 | 有值表示放回已有文档；与 `content`、`asset`、`content_revision_id` 互斥 |
| `document_options` | object | 否 | 新建时交给 `Type.Empty`，结构由类型定义；放回时不得出现 |

- **新建**：同一事务创建文档（修订号 1）、修订行和节点；节点标题取文档标题。撤销画布改动只移除节点。
- **放回**：文档必须存在、属于当前账号且不在回收站。
- **节点快照扩展**：带 `document_id` 的节点新增 `document_summary`（类型摘要或 null）、`document_trashed`（boolean）；文档已彻底删除、类型未注册或快照内容不可展示时，节点 `status.content_state = "unavailable"`（沿用现有的不可用表示）、`document_summary` 为 null。两个新字段只出现在文档节点上。`internal.document` 继续返回现有的 `content` 预览。
- **失败响应**（在画布命令现有错误之上）：

| 状态码 | 错误码 | 触发条件 |
|---|---|---|
| 400 | `validation_failed` | 类型未注册或不可新建；`document_id` 与内容字段同时出现；放回时类型不符；`document_options` 不合法 |
| 404 | `not_found` | `document_id` 不存在或不属于当前账号 |
| 409 | `creative_state_conflict` | 放回的文档在回收站 |

- **示例**（`add_node` 动作片段）：
```json
{"type": "add_node", "node_id": "cwnode_4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8", "type_key": "internal.document",
 "document_id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "x": 120, "y": 80}
```
- **兼容**：扩展现有动作，旧请求不受影响；`internal.document` 仍不可新建，但可以放回（替代测试夹具 `ReferenceInternalDocument` 的生产路径）。

## 读取场景

### 用途与调用
- **用途**：编辑器打开或重读场景时，取得当前快照和渲染所需的模型信息。
- **消费者**：前端场景编辑器；助手工具 `read_scene` 走领域层，不经 HTTP。
- **调用**：`GET /api/v1/creative/documents/{id}`，响应为 `application/json`。这是通用读取接口在 `type_key = "scene"` 时的分支：通用字段（`id`、`type_key`、`title`、`revision`、`trashed_at`）由底座的 `api: 读取文档` 定义，本条目定义 `scene` 分支增加的字段。
- **权限**：已登录且账号具备创意空间能力；场景必须属于当前账号，否则返回 404。回收站中的场景可以读取，响应标明 `trashed_at`。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景文档 ID |

无查询参数和请求体。

### 成功响应
200：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `id`、`type_key`、`title`、`revision`、`content_revision_id`、`unit_revs`、`trashed_at`、`created_at`、`updated_at` | — | 是 | — | 通用字段，见底座 `api: 读取文档`；`type_key` 为 `"scene"`；场景的单元命名见 `data: 场景文档` |
| `scene` | object | 是 | 否 | 快照里的 `scene`，结构见 `data: 场景文档` |
| `models` | object | 是 | 否 | 以 `content_revision_id` 为键，覆盖快照引用的全部模型；C2 之前恒为 `{}` |
| `models.*.content_id` | string | 是 | 否 | 模型内容 ID |
| `models.*.usable` | boolean | 是 | 否 | false 时其余字段为 null，编辑器显示「模型不可用」占位 |
| `models.*.manifest` | object | 是 | 是 | 见几何模块契约「产物」 |
| `models.*.generator` | object | 是 | 是 | `{language, contract_version, lib_version, params_schema, params, mount}`，**不含代码** |
| `models.*.glb_url` | string | 是 | 是 | 10 分钟有效的读取票据 URL |
| `models.*.thumbnail_url` | string | 是 | 是 | 同上 |

代码通过助手工具或代码面板单独读取（C2 局部设计），避免每次重读快照都传几十 KB 代码。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 401 | 现有认证错误码 | 未登录或会话过期 | 走现有续期 |
| 404 | `not_found` | 不存在或不属于当前账号 | 关闭编辑视图，回到画布 |
| 503 | `creative_dependency_unavailable` | 媒体票据服务不可用 | 退避重试 |

### 交互规则
只读，没有分页。重复调用没有副作用。读取票据每次签发新的。

### 调用示例
```http
GET /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10
Authorization: Bearer <token>
```
```json
{
  "id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10",
  "type_key": "scene",
  "title": "黄昏教堂中殿",
  "revision": "4",
  "content_revision_id": "ccrv_3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7",
  "unit_revs": {"meta": "1", "environment": "3", "render": "1", "objects": "4",
                "object:sobj_V1StGXR8_Z5jdHi6B-myT": "1", "object:sobj_V1StGXR8_Z5jdHi6B-myT:content": "1",
                "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ:content": "2"},
  "trashed_at": null,
  "created_at": "2026-10-20T08:00:00Z",
  "updated_at": "2026-10-20T09:10:00Z",
  "scene": {
    "environment": {
      "sky": {"kind": "procedural", "sun_elevation_deg": 12, "sun_azimuth_deg": 240, "turbidity": 4},
      "ambient": {"color": "#ffffff", "intensity": 0.4},
      "fog": null
    },
    "render": {"mode": "clay"},
    "objects": [
      {"id": "sobj_V1StGXR8_Z5jdHi6B-myT", "kind": "primitive", "name": "地面", "parent_id": null, "order": 0,
       "transform": {"position": [0, 0, 0], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": true,
       "primitive": {"builtin": "ground@1", "params": {"size": 40}}, "material_slots": {}},
      {"id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "kind": "camera", "name": "主机位", "parent_id": null, "order": 1,
       "transform": {"position": [0, 1.6, 8], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": false,
       "camera": {"focal_mm": 35, "sensor_mm": [36, 24], "aspect": "3:2", "near_m": 0.1, "far_m": 1000, "label": "镜头 1"}}
    ]
  },
  "models": {}
}
```
失败示例：
```http
GET /api/v1/creative/documents/cwdoc_not_mine
```
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```

### 兼容与变更
在通用读取接口上新增 `scene` 分支，其他类型不受影响。`models` 在 C2 之前恒为空对象，C2 起才填充，属于兼容扩展。

## 提交场景命令

### 用途与调用
- **用途**：对场景做一组原子编辑，成功时产生一个新修订。
- **消费者**：前端场景编辑器（outbox）；助手工具 `edit_scene` 经同一领域命令（不经 HTTP，写入者记为助手和 `run_id`）。
- **调用**：`POST /api/v1/creative/documents/{id}/commands`，`Idempotency-Key` 必填，`application/json`。这是通用命令接口在 `scene` 类型上的操作集合：外壳（`expected` 为单元到修订号的映射、`operations` 数组、整批原子、单元乐观并发）由底座的 `api: 提交文档命令` 定义，本条目定义场景的单元和操作。
- **权限**：场景属于当前账号，且不在回收站。

### 请求
外层 `{operation_id, client_created_at, payload}`（公共约定）。`payload`（外壳见底座，以下是场景的约束）：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `expected` | object（单元 → CreativeRevision 或 null，见底座） | 非 `restore` 批次必传 | 本批涉及的每个单元及其预期 `rev`，null 表示预期该单元不存在；缺少涉及单元时返回 400。`restore` 批次改用底座的 `expected_revision` |
| `operations` | array，1–200 项 | 是 | 按顺序应用；每个单元最多出现一次（同一对象的多处改动合并成 `update_objects` 里的一项）。批量操作的条目上限与物件上限一致（2000），一次多选操作因此始终是一个操作、一个修订 |

`operations[]` 按 `op` 区分：

| op | 字段 | 涉及单元 |
|---|---|---|
| `rename` | `title`（1–200） | `meta` |
| `add_objects` | `objects`（1–2000 个完整对象，见数据契约；`kind` 本条目允许 `primitive`/`light`/`camera`/`group`；`primitive.params` 缺省的键补成默认值） | `objects`（一次；新对象的两个单元以 N+1 出现在 `changed_units` 中） |
| `update_objects` | `updates`（1–2000 项）；每项 `object_id`，以及 `name`/`transform`/`visible`/`locked`/`primitive.params`/`material_slots`/`light`/`camera` 中任选，至少一项（省略即不改；`primitive.params` 按键合并；`material_slots`、`light`、`camera` 整体替换；字段必须与对象的 `kind` 相符）；同一物件只能出现一次 | 每项：只改摆放字段时为 `object:<id>`；只改内容字段时为 `object:<id>:content`；两类都改时两个单元都涉及 |
| `remove_objects` | `object_ids`（1–2000；分组连同后代一起删除） | `objects`，以及每个被删对象的 `object:<id>` 和 `object:<id>:content` |
| `reparent_objects` | `moves`（1–2000 项 `{object_id, parent_id（null 为根）, order}`） | `objects`（一次） |
| `set_environment` | `environment`（整体替换） | `environment` |
| `set_render` | `render`（整体替换） | `render` |
| `restore` | `revision`（旧修订号）；**必须单独成批**，带 `expected_revision` | 全部单元；按文档修订号检查（底座规则） |
| `restore_units` | `units`（1–4096 个单元名，随底座放宽；不能含 `objects`）、`from_revision` | 列出的单元；**必须单独成批**，删除大分组后的撤销也是一个请求，整体成功或整体失败。对象两边都存在时只换列出单元的字段，`parent_id`、`order` 保持现状。存在性会变化时，该对象的两个单元都必须列出，并额外涉及 `objects`，当前不存在的单元在 `expected` 中写 null。服务端先算出恢复后的完整对象集合，再统一解析层级：每个对象沿来源快照（加回的取旧修订，保留的取当前）的祖先链，挂到第一个仍在最终集合中且是分组的祖先下，没有就放到根；父对象同在本次加回的集合里时层级保持不变；旧修订中不存在的对象被删除时，它的后代按同一规则上移，不连带删除。顺序：父对象没变的保留原 `order`，其余按来源快照的层级和顺序依次放入，原 `order` 被占用时排到末尾。结果与 `units` 的排列顺序无关 |

### 成功响应
200（同一 operation 回放原回执）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
回执外形由底座 `api: 提交文档命令` 定义：`revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`。场景不增加字段（`api: 场景模型命令` 另加 `models`）。删除大分组或整份恢复时 `reset_required = true`，编辑器重新读取场景。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、单元重复、`expected` 缺单元、对象违反数据契约（父子成环、未知内置体、数值越界、ID 与当前对象重复等）、引用的物件不存在、`restore_units` 列了 `objects` 或只列了存在性会变化的对象的一个单元 | 修正，不可原样重试 |
| 404 | `not_found` | 场景不存在或不属于当前账号 | 关闭编辑视图 |
| 409 | `creative_document_conflict` | 任一单元的 `rev` 与 `expected` 不符。`details.conflicts` 为数组：`[{unit, expected_rev, current_rev}]` | 重读，按冲突界面处理后再提交 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 409 | `idempotency_conflict` | 同一 key 用于不同请求体 | 换新 key 前先查原回执 |
| 413 | `creative_size_limit` | 请求体超限，或应用后快照 > 512 KiB / 对象 > 2000 / 摄像机 > 32 | 缩小改动 |

### 交互规则
- **原子性**：全部成功或全部不生效。
- **幂等**：沿用公共约定。
- **并发**：按单元乐观并发，规则见 design「单元版本、写入与撤销」。
- 不适用：分页、异步、限流（沿用全局请求体上限）。

### 调用示例
```http
POST /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10/commands
Authorization: Bearer <token>
Idempotency-Key: 5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60
Content-Type: application/json
```
```json
{
  "operation_id": "5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60",
  "client_created_at": "2026-10-20T09:30:00Z",
  "payload": {
    "expected": {"object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "objects": "4"},
    "operations": [
      {"op": "update_objects", "updates": [{"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
       "transform": {"position": [1.5, 1.4, 7], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}}]},
      {"op": "add_objects", "objects": [{
        "id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "kind": "primitive", "name": "石台", "parent_id": null, "order": 2,
        "transform": {"position": [0, 0, -3], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
        "visible": true, "locked": false,
        "primitive": {"builtin": "box@1", "params": {"width": 2, "height": 0.8, "depth": 1}}, "material_slots": {}}]}
    ]
  }
}
```
```json
{"revision": "5", "changed_units": ["object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "objects", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a:content"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
冲突示例（助手已先改过这台摄像机）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
  "details": {"conflicts": [{"unit": "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "expected_rev": "4", "current_rev": "6"}]}}}
```

### 兼容与变更
在通用命令接口上新增 `scene` 类型的操作集合，其他类型不受影响。`api: 场景模型命令` 会在此基础上增加操作，并放开 `add_objects` 的 `model_instance`，属于兼容扩展。旧客户端不会发送新操作。

### 补充说明
新建场景不经本接口，而是走画布命令 `add_node`（`type_key = "core.scene"`）。底座的文档类型端口负责在同一事务里创建文档；场景类型提供初始快照（一块内置地面 `ground@1`，摄像机随 C4 的放置 UI 出现）和卡片摘要。
