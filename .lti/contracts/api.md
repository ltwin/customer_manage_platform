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
| `models` | object | 是 | 否 | 以 `content_revision_id` 为键，覆盖快照引用的全部模型；没有模型实例时为 `{}` |
| `models.*.content_id` | string | 是 | 否 | 模型内容 ID |
| `models.*.usable` | boolean | 是 | 否 | false 时 `manifest`、`generator` 为 null，编辑器显示「模型不可用」占位 |
| `models.*.manifest` | object | 是 | 是 | 见几何模块契约「产物」；取服务端从 GLB 算出的值 |
| `models.*.generator` | object | 是 | 是 | `{language, contract_version, lib_version, code_sha256, params_schema, params, mount}`，**不含代码** |
| `models.*.name_hint` | string | 是 | 是 | 模型名称提示 |

- **GLB 与缩略图**：响应不含地址。编辑器按模型修订调用现有的 `POST /creative/media-access-tickets`（`content_revision_id`，`role` 为 `original` 取 GLB、`thumbnail` 取缩略图，`purpose` 为 `display`），按修订缓存，与画布图片的做法一致。
- **代码**：通过 `GET /creative/content-revisions/{content_revision_id}` 单独读取（载荷的 `model.generator.code`），避免每次重读快照都传几十 KB 代码。
- **性能**：模型摘要在同一个读快照事务里批量读取，查询次数不随模型数增长。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 401 | 现有认证错误码 | 未登录或会话过期 | 走现有续期 |
| 404 | `not_found` | 不存在或不属于当前账号 | 关闭编辑视图，回到画布 |

### 交互规则
只读，没有分页。重复调用没有副作用。

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
  "revision": "6",
  "content_revision_id": "ccrv_3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7",
  "unit_revs": {"meta": "1", "environment": "3", "render": "1", "objects": "6",
                "object:sobj_V1StGXR8_Z5jdHi6B-myT": "1", "object:sobj_V1StGXR8_Z5jdHi6B-myT:content": "1",
                "object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "6", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "6"},
  "trashed_at": null,
  "created_at": "2026-10-20T08:00:00Z",
  "updated_at": "2026-10-22T10:00:00Z",
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
      {"id": "sobj_Z1x2C3v4B5n6M7a8S9d0f", "kind": "model_instance", "name": "左侧柱 1", "parent_id": null, "order": 3,
       "transform": {"position": [-3, 0, -2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": false,
       "model": {"content_revision_id": "ccrv_9e2a0d4c-1b3f-4a5e-8c7d-6f5e4d3c2b1a"}, "material_slots": {}}
    ]
  },
  "models": {
    "ccrv_9e2a0d4c-1b3f-4a5e-8c7d-6f5e4d3c2b1a": {
      "content_id": "ccnt_51c0e8a2-7d6b-4c3a-9f1e-2d4c6b8a0e1f",
      "usable": true,
      "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                   "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []},
      "generator": {"language": "three-module", "contract_version": 1, "lib_version": 1,
                    "code_sha256": "sha256-6f1ed002ab5595859014ebf0951522d9f2b8d1c5e8a6a7c3b1d0e9f8a7b6c5d4",
                    "params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                    "params": {"height": 6}, "mount": "floor"},
      "name_hint": "哥特束柱"
    }
  }
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
- 在通用读取接口上新增 `scene` 分支，其他类型不受影响。
- C2 修改：`models.*` 去掉 `glb_url`、`thumbnail_url`（C1 期间 `models` 恒为空，没有调用方用过这两个字段），加 `name_hint` 和 `generator.code_sha256`；去掉 503（读取不再签发票据）。

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
| `add_objects` | `objects`（1–2000 个完整对象，见数据契约；`kind` 本条目允许 `primitive`/`light`/`camera`/`group`/`volume`；`primitive.params` 缺省的键补成默认值；`light`、`camera`、`volume` 缺省的字段补成数据契约里的默认值） | `objects`（一次；新对象的两个单元以 N+1 出现在 `changed_units` 中） |
| `update_objects` | `updates`（1–2000 项）；每项 `object_id`，以及 `name`/`transform`/`visible`/`locked`/`primitive.params`/`material_slots`/`light`/`camera`/`volume` 中任选，至少一项（省略即不改；`primitive.params`、`light`、`camera`、`volume` 按键合并，`light.type` 改变时去掉新类型没有的字段、缺少的补默认值；`material_slots` 整体替换；字段必须与对象的 `kind` 相符）；同一物件只能出现一次 | 每项：只改摆放字段时为 `object:<id>`；只改内容字段时为 `object:<id>:content`；两类都改时两个单元都涉及 |
| `remove_objects` | `object_ids`（1–2000；分组连同后代一起删除） | `objects`，以及每个被删对象的 `object:<id>` 和 `object:<id>:content` |
| `reparent_objects` | `moves`（1–2000 项 `{object_id, parent_id（null 为根）, order, transform?}`）；可选 `create_groups`（1–2000 个新分组，外形同 `add_objects` 里 `kind=group` 的对象）和 `remove_groups`（1–2000 个分组 ID）。按顺序执行：先建 `create_groups`；再按 `moves` 改挂，`parent_id` 可以是本次新建的分组，带 `transform` 时同时替换该物件的局部变换（调用方据此保持世界位置，服务端照写）；最后删除 `remove_groups`，删除时它们必须没有子物件。同一物件在三处合计只能出现一次（修订 v10） | `objects`（一次）；带 `transform` 的移动另涉及 `object:<id>`；新建的分组的两个单元以 N+1 出现在 `changed_units` 中；删除的分组涉及 `object:<id>` 和 `object:<id>:content` |
| `set_environment` | `environment`（整体替换） | `environment` |
| `set_render` | `render`（按键合并，只改写到的字段；修订 v10） | `render` |
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
| 400 | `validation_failed` | 外形非法、单元重复、`expected` 缺单元、对象违反数据契约（父子成环、未知内置体、数值越界、ID 与当前对象重复、体积物件带了 `material_slots` 等）、引用的物件不存在、本次写到的 `focus_object_id` 不可对焦（不存在、是摄像机自己、灯光或摄像机）、`reparent_objects` 里同一物件出现多次、`create_groups` 不是分组、`remove_groups` 不是分组或删除时仍有子物件、`restore_units` 列了 `objects` 或只列了存在性会变化的对象的一个单元 | 修正，不可原样重试 |
| 404 | `not_found` | 场景不存在或不属于当前账号 | 关闭编辑视图 |
| 409 | `creative_document_conflict` | 任一单元的 `rev` 与 `expected` 不符。`details.conflicts` 为数组：`[{unit, expected_rev, current_rev}]` | 重读，按冲突界面处理后再提交 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 409 | `idempotency_conflict` | 同一 key 用于不同请求体 | 换新 key 前先查原回执 |
| 413 | `creative_size_limit` | 请求体超限，或应用后快照 > 512 KiB / 对象 > 2000 / 摄像机 > 32 / 体积物件 > 8 | 缩小改动 |

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
编组示例（修订 v10，Ctrl + G：新建分组，把两个分属不同父级的物件挂进去，并写入保持世界位置的局部变换；一个修订，撤销是另一个 `reparent_objects`）：
```json
{"operation_id": "8a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", "client_created_at": "2026-10-20T09:40:00Z",
 "payload": {
   "expected": {"objects": "5", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a": "5"},
   "operations": [{"op": "reparent_objects",
     "create_groups": [{"id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "kind": "group", "name": "石台组", "parent_id": null, "order": 3,
       "transform": {"position": [0.75, 0, 2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}, "visible": true, "locked": false}],
     "moves": [
       {"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 0,
        "transform": {"position": [0.75, 1.4, 5], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}},
       {"object_id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 1,
        "transform": {"position": [-0.75, 0, -5], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}}]}]}}
```
```json
{"revision": "6", "changed_units": ["objects", "object:sobj_G1h2J3k4L5z6X7c8V9b0n", "object:sobj_G1h2J3k4L5z6X7c8V9b0n:content", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
取消编组时，`moves` 把子物件挂回分组原来的父级并写回局部变换，`remove_groups` 删除这个分组。

冲突示例（助手已先改过这台摄像机）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
  "details": {"conflicts": [{"unit": "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "expected_rev": "4", "current_rev": "6"}]}}}
```

### 兼容与变更
在通用命令接口上新增 `scene` 类型的操作集合，其他类型不受影响。`api: 场景模型命令` 会在此基础上增加操作，并放开 `add_objects` 的 `model_instance`，属于兼容扩展。旧客户端不会发送新操作。

`scene-world-environment` W5：`add_objects`、`update_objects` 放开 `kind = volume` 和它的内容字段 `volume`（见 `data: 体积物件`），`volume` 与 `light`、`camera` 一样按键合并，属于兼容扩展；旧客户端不会写出体积物件。物理天空、夜空、空气透视和摄像机曝光的新字段走已有的 `set_environment`（整体替换）、`update_objects` 的 `camera`（按键合并）和 `set_render`（按键合并，默认曝光值），本接口的外形不变，只是按数据契约校验新字段。

修订 v10（C5 起草时）：`set_render` 由整体替换改为按键合并（`render` 新增 `exposure_ev` 后，只写 `mode` 的旧写法不能把曝光重置为 0；旧客户端总是写全部字段，不受影响）。`reparent_objects` 增加可选的 `moves[].transform`、`create_groups` 和 `remove_groups`，编组和取消编组因此能在一次提交里完成：同一批里每个单元只能被一个操作涉及，「新建分组 + 改挂」或「改挂 + 删除分组」若拆成两个操作，会两次涉及 `objects`。旧请求不受影响；`remove_objects` 删除分组时仍连同后代一起删除。

修订 v7：`update_objects` 里的 `light`、`camera` 由整体替换改为按键合并（字段允许省略后，整体替换会把没写的字段悄悄重置成默认值；three.js 里改 `light.intensity` 也只改这一项）；`add_objects` 可省略它们的字段。字段改名见 `data: 场景文档`。编辑器同一版本更新，旧字段名返回 400。

### 补充说明
新建场景不经本接口，而是走画布命令 `add_node`（`type_key = "core.scene"`）。底座的文档类型端口负责在同一事务里创建文档；场景类型提供初始快照（一块内置地面 `ground@1`，摄像机随 C4 的放置 UI 出现）和卡片摘要。

## 场景模型命令

### 用途与调用
- **用途**：把烘焙好的模型提交为 `model` 内容，并在场景中新增实例或替换实例的模型。
- **消费者**：编辑器代码面板（C2）；客户端任务完成时由服务端内部调用（C3，经领域层）。
- **调用**：同 `api: 提交场景命令` 的端点与外层，增加以下操作：

| op | 字段 | 涉及单元 |
|---|---|---|
| `commit_model` | `ref`（本批内的临时名，`^[a-z0-9_]{1,32}$`）、`glb_upload_id`、`thumbnail_upload_id`（均为就绪且未消费的暂存上传）、`generator`（完整的 `model.generator`，含代码）、`manifest`、`name_hint`、`base_content_id`（可选，有则作为该模型内容的新修订，否则新建模型内容） | 无场景单元（不改快照） |
| `add_objects` 放开 `kind=model_instance` | `objects[].model` 写 `{content_revision_id}` 或 `{ref}`（引用本批的 `commit_model`） | `objects` |
| `set_instance_model` | `object_id`、`model`（`{content_revision_id}` 或 `{ref}`）、`scope`（`"this"` 或 `"all_instances"`，缺省 `"this"`）、`object_ids`（`all_instances` 时必填，含 `object_id`，列出调用方看到的全部共用实例；每个都必须批前引用同一旧修订） | `object:<id>:content`（只动内容单元，所以摄影师同时移动这个物件不会冲突）；`all_instances` 时为 `object_ids` 中每个对象的内容单元。由调用方列出而不是服务端展开，避免并发新增的共用实例让请求缺少预期单元 |

- **分叉规则（I4）**：
  - `base_content_id` 被其他实例或资产引用时，`commit_model` 仍然在该内容上追加修订。是否让其他实例跟随，由 `set_instance_model` 的 `scope` 决定。
  - 资产持有的旧修订不受影响，因为资产引用的是修订而不是内容的最新版。
  - 编辑器和助手在 `scope = "this"` 且共享时，应传入不带 `base_content_id` 的 `commit_model`，即新建内容（分叉）。
- **持有方交接**：`commit_model` 在场景类型的 `Apply` 中新建模型内容，使用底座传入的 `Retain` 回调预登记引用（底座 `patterns: 文档类型端口`）。
- **校验**：暂存上传属于本账号且未消费，GLB 上传的声明类型为 `model`、缩略图为 PNG 图片；manifest 与暂存形态中服务端算出的一致；`code_sha256` 正确；参数符合 schema（缺省的键补默认值）；`commit_model` 必须出现在引用它的操作之前。消费后两个暂存候选标为已被文档采用。
- **成功响应**：在 `api: 提交场景命令` 的响应上增加 `models: {<ref>: {content_id, content_revision_id}}`，经底座的类型专属回执字段返回（`patterns: 文档类型端口`），随幂等回执重放。
- **失败响应**（在基础接口之上增加）：

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 409 | `creative_upload_unavailable` | 暂存上传不存在、属于其他账号、已过期、已消费或尚未就绪 | 重新上传 |
| 400 | `validation_failed` | manifest 与 GLB 不符、哈希不符、参数越界、上传类型不符、`ref` 未定义 / 重复 / 未被本批引用、单批 `commit_model` 超过 16 个 | 修正 |
| 400 | `creative_model_unavailable` | `content_revision_id` 不存在、不是完整形态的 `model` 或不可展示 | 重读后重新选择 |

不使用 404 和 `creative_state_conflict`：编辑器把命令返回的 404 当作文档不存在、把 `creative_state_conflict` 当作文档进了回收站。

- **示例**：
```json
{
  "operation_id": "0b7e2c1d-3f4a-4b5c-8d9e-0f1a2b3c4d5e",
  "client_created_at": "2026-10-22T10:00:00Z",
  "payload": {
    "expected": {"objects": "5"},
    "operations": [
      {"op": "commit_model", "ref": "pillar", "glb_upload_id": "cupl_aa11", "thumbnail_upload_id": "cupl_bb22",
       "name_hint": "哥特束柱",
       "generator": {"language": "three-module", "contract_version": 1, "lib_version": 1,
                     "code": "export const params={height:{type:'number',default:6,min:2,max:20}};export const mount='floor';export default function build({params,THREE}){const g=new THREE.Group();g.add(new THREE.Mesh(new THREE.CylinderGeometry(0.4,0.5,params.height,16),new THREE.MeshStandardMaterial({name:'slot:stone'})));return g}",
                     "code_sha256": "sha256-6f1ed002ab5595859014ebf0951522d9f2b8d1c5e8a6a7c3b1d0e9f8a7b6c5d4",
                     "params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                     "params": {"height": 6}, "mount": "floor"},
       "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                    "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []}},
      {"op": "add_objects", "objects": [{
        "id": "sobj_Z1x2C3v4B5n6M7a8S9d0f", "kind": "model_instance", "name": "左侧柱 1", "parent_id": null, "order": 3,
        "transform": {"position": [-3, 0, -2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
        "visible": true, "locked": false, "model": {"ref": "pillar"}, "material_slots": {}}]}
    ]
  }
}
```
```json
{"revision": "6", "changed_units": ["objects", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
 "annotated_units": [], "removed_units": [], "reset_required": false,
 "models": {"pillar": {"content_id": "ccnt_51c0", "content_revision_id": "ccrev_9e2a"}}}
```
- **兼容**：在 `api: 提交场景命令` 上做兼容扩展。

## 认领场景客户端任务

### 用途与调用
- **用途**：渲染端认领一个待执行任务。指定场景时只取该场景最早的任务，否则取账号里最早的任务。
- **消费者**：前端渲染端：场景编辑视图（带 `document_id`）和画布页的后台渲染端（不带）。收到失效通知、连接或回到前台、完成上一个任务后调用。
- **调用**：`POST /api/v1/creative/scene-jobs/claim`，`application/json`。认领本身不需要幂等键：重复认领只会拿到下一个任务。
- **权限**：任务属于当前账号；指定的场景属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `editor_session_id` | body | string，`^[A-Za-z0-9_-]{16,64}$` | 是 | 标签页随机生成的会话 ID |
| `document_id` | body | string | 否 | 只认领该场景的任务 |
| `kinds` | body | string[] | 否 | 本端能执行的任务类型；缺省为全部已知类型 |

### 成功响应
- 200，认领成功：`{job: {id, document_id, kind, request, attempt, claim_token, lease_until, deadline}}`。`request` 的结构见 `data: 场景客户端任务`。
- 204，没有可认领的任务：无响应体。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 会话 ID 非法、`kinds` 含未知值 | 修正 |
| 404 | `not_found` | 指定的文档不存在、不属于当前账号或不是场景 | 停止认领该场景 |

### 交互规则
- 用 `FOR UPDATE SKIP LOCKED` 保证每个任务最多被一个会话认领。同一会话可以连续认领多个任务，按顺序执行。
- 没有任务时只读索引、不加锁，可以在每次失效通知时调用。
- 租约 30 秒，续租见 `api: 续租场景客户端任务`。

### 调用示例
```http
POST /api/v1/creative/scene-jobs/claim
Authorization: Bearer <token>
Content-Type: application/json

{"editor_session_id": "tab_Vx9q2LmN7pR4sT8w", "kinds": ["bake_model", "render_view"]}
```
```json
{"job": {"id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10",
  "kind": "render_view", "request": {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "mode": "clay"},
  "attempt": 0, "claim_token": "clm_Q2w3E4r5T6y7U8i9", "lease_until": "2026-10-22T10:05:30Z", "deadline": "2026-10-22T10:35:00Z"}}
```
失败示例：
```json
{"error": {"code": "validation_failed", "message": "editor_session_id 格式不正确"}}
```

### 兼容与变更
首次定义。修订 v6 起为账号级路径（此前未交付）。

## 续租场景客户端任务

### 用途与调用
- **用途**：执行中的编辑器延长租约，表明自己仍在工作。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/lease`，`application/json`，不需要幂等键（续租本身幂等）。
- **权限**：任务属于当前账号，且 `claim_token` 匹配。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `job_id` | path | string | 是 | 任务 ID |
| `claim_token` | body | string | 是 | 认领时拿到的令牌 |

### 成功响应
200：`{lease_until}`，延长到当前时间 + 30 秒，但不超过 `deadline`。续租不触发任务通知。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 任务不存在或不属于当前账号 | 放弃执行 |
| 409 | `creative_state_conflict` | 任务已不是 `claimed`，或令牌不匹配（租约已过期被回收、任务已过期或取消） | 立即停止执行并丢弃产物 |

### 调用示例
```json
{"claim_token": "clm_Q2w3E4r5T6y7U8i9"}
```
```json
{"lease_until": "2026-10-22T10:06:00Z"}
```
```json
{"error": {"code": "creative_state_conflict", "message": "任务已被回收"}}
```

### 兼容与变更
首次定义。

## 完成场景客户端任务

### 用途与调用
- **用途**：渲染端提交任务结果（成功或失败）。服务端在同一事务里落结果、改终态、让关联运行继续。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/complete`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：任务属于当前账号，`claim_token` 匹配，且租约未过期。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `claim_token` | string | 是 | — |
| `outcome` | `"succeeded"` \| `"failed"` | 是 | — |
| `error` | `{code, message}` | 失败时 | `code` 取值：`compile_error`、`compile_timeout`、`limit_exceeded`、`render_error`、`host_error`（宿主侧失败：媒体校验失败、WebGL 上下文丢失、代码加载失败等）；`message` ≤ 4000 字符，会原样交给助手 |
| `bake` | object | `kind=bake_model` 且成功时 | `{glb_upload_id, thumbnail_upload_id, manifest, module}`。`module` 为 `{params_schema, params, mount}`：沙箱回传、宿主按服务端规则校验过的规范化模块信息。服务端用任务 `request` 里的代码计算 `code_sha256`、组装 `generator`，按 `api: 场景模型命令` 的规则校验，再组装 `commit_model` 和 `add_objects` / `set_instance_model`，经场景命令写入，写入者记为助手的 `run_id`；替换目标的预期版本取任务 `request.expected` |
| `render` | object | `kind=render_view` 且成功时 | `{image_upload_id, scene_revision}`：暂存图片（JPEG，长边 1024），以及渲染端实际渲染的场景修订（不得大于当前修订） |
| `export` | object | `kind=export_shots` 且成功时 | `{scene_revision, shots}`：渲染端实际渲染的场景修订，以及图片清单。`shots` 的结构与 `api: 导出分镜` 相同，必须正好覆盖任务 `request` 的摄像机和导出方式。服务端在保存点内按 `api: 导出分镜` 的规则放置节点（位置、标题、`based_on`），放到任务 `request` 的画布和场景节点旁，节点按 `request.camera_object_ids` 的顺序排行，写入者记为助手的 `run_id` |

### 成功响应
200：`{job_id, state, result}`，`result` 结构见 `data: 场景客户端任务`。`state` 为 `succeeded` 或 `failed`。下列情况任务记为 `failed` 并照常返回 200，渲染端不重试：
- 写入场景时单元冲突，例如替换目标在烘焙期间被摄影师改过或删除（`result.code = "conflict"`，带冲突信息和烘焙产物）；
- 结果内容不合规：manifest 与服务端从 GLB 算出的不符、参数不符合定义、物件超出上限、父级不存在（`result.code = "invalid_result"`）；
- 导出无法放置：画布已归档、场景节点已不在画布上或不指向这个场景、场景在回收站或已删除、摄像机在所报修订里不存在、`scene_revision` 晚于当前修订、画布节点超出上限（`result.code = "invalid_result"`，`message` 说明原因）。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 请求外形不对：缺少与 kind 对应的结果字段、错误码非法、`export.shots` 与任务请求的摄像机和导出方式不一致。任务不变 | 修正 |
| 404 | `not_found` | 任务不存在或不属于当前账号 | 放弃 |
| 409 | `creative_state_conflict` | 任务已不是 `claimed`、令牌不匹配或租约已失效；或关联运行的复查不通过（运行已结束，任务已取消） | 丢弃产物；任务会被重新认领、已过期或已取消 |
| 409 | `creative_upload_unavailable` | 暂存上传不存在、已过期或已被消费 | 租约仍有效时重新上传，用新的 operation id 重试 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
- **原子性**：结果落库、任务终态、运行继续在同一事务里。场景写入和导出的画布放置在保存点内执行，冲突、结果不合规或无法放置时回滚到保存点，任务记为 `failed`（带原因），运行照常继续，助手据此重做。导出的节点只在任务成功时创建。
- **幂等**：沿用公共约定。网络中断后用同一 key 重试或查回执。

### 调用示例
```json
{"operation_id": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", "client_created_at": "2026-10-22T10:05:12Z",
 "payload": {"claim_token": "clm_Q2w3E4r5T6y7U8i9", "outcome": "succeeded", "render": {"image_upload_id": "ccup_cc33", "scene_revision": "12"}}}
```
```json
{"job_id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "state": "succeeded",
 "result": {"image_upload_id": "ccup_cc33", "content_revision_id": "ccrv_77aa", "scene_revision": "12"}}
```
烘焙成功示例（请求）：
```json
{"operation_id": "5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f", "client_created_at": "2026-10-22T10:06:40Z",
 "payload": {"claim_token": "clm_Z9y8X7w6V5u4T3s2", "outcome": "succeeded",
             "bake": {"glb_upload_id": "ccup_g1", "thumbnail_upload_id": "ccup_t1",
                      "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                                   "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []},
                      "module": {"params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                                 "params": {"height": 6}, "mount": "floor"}}}}
```
编译失败示例（请求）：
```json
{"operation_id": "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f", "client_created_at": "2026-10-22T10:07:00Z",
 "payload": {"claim_token": "clm_Z9y8X7w6V5u4T3s2", "outcome": "failed",
             "error": {"code": "compile_error", "message": "ReferenceError: archCount is not defined (line 12)"}}}
```
导出成功示例（请求，修订 v8）：
```json
{"operation_id": "7b8c9d0e-1f2a-4b3c-8d4e-5f6a7b8c9d0e", "client_created_at": "2026-10-25T14:02:30Z",
 "payload": {"claim_token": "clm_K1j2H3g4F5d6S7a8", "outcome": "succeeded",
             "export": {"scene_revision": "18",
                        "shots": [{"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
                                   "images": [{"pass": "clay", "upload_id": "ccup_dd44"}, {"pass": "line", "upload_id": "ccup_ee55"}]}]}}}
```

### 兼容与变更
首次定义。修订 v6：`bake` 增加 `module`；`render` 增加 `scene_revision`；错误码增加 `host_error`；暂存上传不可用改用 `creative_upload_unavailable`（与 `api: 场景模型命令` 一致）；冲突与结果不合规记为任务 `failed` 并返回 200（`creativeops` 只提交 2xx 回执）；完成时复查关联运行。修订 v8（C4 起草时）：`export` 由 `{export_operation_id}` 改为 `{scene_revision, shots}`，导出节点在完成事务里创建，不再经 `api: 导出分镜`。C3 只做了占位（服务端对 `export` 结果直接拒绝，没有调用方），不需要兼容。

## 列出场景客户端任务

### 用途与调用
- **用途**：列出某个场景当前未结束的任务，供编辑器高亮「助手正在修改」的物件、在烘焙目标处显示占位。
- **消费者**：场景编辑视图。打开时、收到失效通知时（节流）调用。
- **调用**：`GET /api/v1/creative/documents/{id}/scene-jobs`，只读，无请求体。
- **权限**：场景属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景 ID |

### 成功响应
200：`{items: [{id, kind, state, run_id, created_at, target}]}`，只含 `queued` 和 `claimed`，按创建时间排序，最多 50 条。
- `target`：`bake_model` 为 `{op:"add", object:{name, parent_id, transform}}` 或 `{op:"replace", object_ids}`；其他 kind 为 null。
- 不返回代码、参数和认领信息。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 文档不存在、不属于当前账号或不是场景 | 停止 |

### 交互规则
只读，没有分页；重复调用没有副作用。

### 调用示例
```json
{"items": [{"id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "kind": "bake_model", "state": "claimed",
  "run_id": "crun_8c7b6a59", "created_at": "2026-10-22T10:05:00Z",
  "target": {"op": "replace", "object_ids": ["sobj_Z1x2C3v4B5n6M7a8S9d0f"]}}]}
```

### 兼容与变更
首次定义（修订 v6）。

## 读取运行的场景改动

### 用途与调用
- **用途**：列出某次助手运行对一个场景改了哪些物件和设置，并给出每一处恢复到运行之前所需的 `restore_units`；已被别人改过的标为不可恢复。「恢复这一处」「撤销本轮」和助手撤回自己的改动共用这一份计算。
- **消费者**：助手面板的本轮改动清单；`edit_scene` 的撤回操作在领域层调用同一函数，不经 HTTP。
- **调用**：`GET /api/v1/creative/documents/{id}/run-changes?run_id=<run_id>`，只读。
- **权限**：场景和运行都属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景 ID |
| `run_id` | query | string | 是 | 助手运行 ID |

### 成功响应
200：

| 字段 | 类型 | 可为 null | 说明 |
|---|---|---|---|
| `run_id`、`document_id` | string | 否 | — |
| `revisions` | string[] | 否 | 这次运行写出的修订号，升序；为空表示没有改过这个场景 |
| `entries` | array | 否 | 每个改动过的物件一条，`environment` / `render` / `meta` 各一条；运行只调整过层级或顺序时（只动了 `objects` 单元）另有一条 `{kind:"unit", unit:"objects"}` |
| `entries[].target` | `{kind:"object", object_id}` \| `{kind:"unit", unit}` | 否 | — |
| `entries[].name` | string | 否 | 当前名称；已删除的取运行之前的名称 |
| `entries[].change` | `"added"` \| `"removed"` \| `"modified"` | 否 | 相对运行之前 |
| `entries[].restore` | `{from_revision, units, expected}` | 是 | 恢复这一处的命令内容（`restore_units` 的参数加上预期版本）；不可恢复时为 null |
| `entries[].blocked` | `{reason, revision, actor_kind}` | 是 | 不可恢复的原因：`changed_by_others`（运行之后别人改过这一处）、`restored_whole`（之后整份恢复过）或 `hierarchy_not_restorable`（运行调整过层级或顺序；按单元恢复不改层级，只能在历史版本列表整份恢复。修订 v10 起助手可以编组和改挂：某条修订改了物件的父级或顺序时，这些物件的条目同样以此原因阻止恢复，否则只恢复局部变换、不恢复父级，物件会跳位；判断按该修订前后快照里的 `parent_id`、`order`，不按单元） |
| `undo_all` | array | 否 | 撤销本轮的命令内容，通常 1 批；只包含可恢复的条目；全部不可恢复时为空数组 |

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 缺少 `run_id`；运行写出的修订超过 500 个；运行第一次写入之后的修订（含别人的）超过 5000 个 | 改用历史版本列表 |
| 404 | `not_found` | 文档或运行不存在、不属于当前账号，或文档不是场景 | 停止 |

### 交互规则
- 某一处的旧值取它在本次运行第一次被写之前那一版。一批 `restore_units` 只能有一个来源修订，这些来源修订之间若没有别人改过，就合成一批；否则按来源修订分批。
- 同一物件的摆放和内容两个单元若来自不同的来源修订，且两者之间有别人改过，这一条不拆成两批，整条标为 `changed_by_others`（`blocked.revision` 为那次修改），留给历史版本列表。
- 返回的 `expected` 是读取时的当前版本；提交后若有人刚好又改了，照常冲突。
- 只读，没有副作用。

### 调用示例
```json
{"run_id": "crun_8c7b6a59", "document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10", "revisions": ["7", "8", "9"],
 "entries": [
   {"target": {"kind": "object", "object_id": "sobj_Z1x2C3v4B5n6M7a8S9d0f"}, "name": "左侧柱 1", "change": "added",
    "restore": {"from_revision": "6", "units": ["object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
                "expected": {"object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "7", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "9", "objects": "7"}},
    "blocked": null},
   {"target": {"kind": "object", "object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ"}, "name": "主机位", "change": "modified",
    "restore": null, "blocked": {"reason": "changed_by_others", "revision": "10", "actor_kind": "photographer"}}],
 "undo_all": [{"from_revision": "6", "units": ["object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
               "expected": {"object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "7", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "9", "objects": "7"}}]}
```

### 兼容与变更
首次定义（修订 v6）。修订 v7 补记 C3 实现时确定的第三种不可恢复原因、5000 条历史上限和多来源规则，与已交付的实现一致，接口外形不变。修订 v10（C5）：助手可以编组和改挂，`hierarchy_not_restorable` 扩展到改了父级或顺序的物件条目，接口外形不变。

## 导出分镜

### 用途与调用
- **用途**：把渲染端出的导出图落成画布图片节点，并记录 `based_on`。
- **消费者**：前端编辑视图（摄影师点导出）。助手导出不调用本接口，图片清单随 `api: 完成场景客户端任务` 提交，放置规则与本接口相同。
- **调用**：`POST /api/v1/creative/documents/{id}/scene-exports`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：场景和目标画布都属于当前账号，画布未归档，且场景节点在该画布上、指向这个场景。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `canvas_id` | string | 是 | 场景节点所在画布 |
| `scene_node_id` | string | 是 | 场景节点；新节点放在它的右侧，与它同一父级 |
| `scene_revision` | CreativeRevision | 是 | 出图时的场景修订；必须等于或早于当前修订，并且仍存在 |
| `shots` | array，1–32 | 是 | 每台摄像机一项，摄像机不重复；节点按这个顺序逐行排列 |
| `shots[].camera_object_id` | string | 是 | 在 `scene_revision` 里存在且 `kind=camera` |
| `shots[].images` | array，1–3 | 是 | `{pass:"clay"\|"line"\|"lit", upload_id}`，同一镜头内 `pass` 不重复；暂存图片就绪、未消费、类型为图片。全部镜头合计 ≤ 48 张 |

### 成功响应
200：`{change_id, scene_revision, nodes:[{camera_object_id, pass, node_id, content_revision_id, lens}]}`。节点经画布命令层在一次修改里创建，可以在画布上一次撤销。`lens` 与节点 `based_on.lens` 相同（修订 v9）。
- 位置：从场景节点右侧 80 起，每台摄像机一行、每种方式一列（按 `clay`、`line`、`lit` 的顺序），节点宽 300、高按摄像机画幅；整块与同一父级下的已有节点相交时，下移到相交节点下方。
- 标题：摄像机 `label` 非空时用它，否则用物件名，接「 · clay」「 · 线稿」或「 · 材质光照」。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、`pass` 或摄像机重复、摄像机不存在于该修订、`scene_revision` 晚于当前修订或不存在 | 修正 |
| 404 | `not_found` | 场景、画布或场景节点不存在，不属于当前账号，或场景节点不指向这个场景 | 停止 |
| 409 | `archived_read_only` | 画布已归档（沿用画布现有错误码） | 恢复画布后重新导出 |
| 409 | `creative_state_conflict` | 场景在回收站 | 丢弃暂存图片 |
| 409 | `creative_upload_unavailable` | 暂存图片不存在、未就绪、已过期、已被消费或不是图片 | 重新上传后用新的 operation id 重试 |
| 413 | `creative_size_limit` | 图片合计超过 48 张，或画布节点数超过上限 | 分次导出 |

### 交互规则
- **原子性**：全部节点一起创建或都不创建。
- **幂等**：沿用公共约定。网络中断、超时或 5xx 时，用同一 operation id 和 `client_created_at` 重试，得到同一组节点；只有服务端明确拒绝（4xx）时才丢弃暂存图片。
- **过时**：后续场景修改不影响已创建的节点；过时提示由节点视图计算（`data: 导出图来源`）。

### 调用示例
```json
{"operation_id": "4d5e6f70-8192-4a3b-9c4d-5e6f708192a3", "client_created_at": "2026-10-25T14:00:00Z",
 "payload": {"canvas_id": "ccv_12ab", "scene_node_id": "cnode_34cd", "scene_revision": "18",
   "shots": [{"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
              "images": [{"pass": "clay", "upload_id": "cupl_dd44"}, {"pass": "line", "upload_id": "cupl_ee55"}]}]}}
```
```json
{"change_id": "cchg_56ef", "scene_revision": "18", "nodes": [
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "clay", "node_id": "cnode_78a1", "content_revision_id": "ccrev_a1b2"},
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "line", "node_id": "cnode_78a2", "content_revision_id": "ccrev_a1b3"}]}
```
```json
{"error": {"code": "validation_failed", "message": "摄像机 sobj_x 在修订 18 中不存在"}}
```

### 兼容与变更
首次定义。图片节点 `config` 新增 `based_on`，节点快照新增可选的 `based_on_state`，都属于兼容扩展；旧读取端忽略这些字段。修订 v8（C4 起草时）：错误码对齐画布与暂存的现有实现（归档为 `archived_read_only`，暂存不可用为 `creative_upload_unavailable`）；合计 48 张上限（画布一次修改最多牵动 100 个对象，每张图占节点和版本两个）；响应增加 `scene_revision`；只服务手动导出，去掉 `job_id`，助手导出改随完成任务提交；`based_on` 增加 `content_revision_id` 和 `fingerprint`；写明位置、标题和重试规则。修订 v9（C4 验收中）：`based_on` 和响应的节点增加镜头快照 `lens`，兼容扩展。

## 模型存入资产库

### 用途与调用
- **用途**：把场景里某个实例的模型和材质分配复制成库副本，存入资产库（复制语义，见 I7 与 `data: 模型资产`，修订 v12）。
- **消费者**：编辑器对象菜单「存入资产库」；后续的助手库整理工具。
- **调用**：`POST /api/v1/creative/assets/from-scene-model`，`Idempotency-Key` 必填，外层为公共约定（`{operation_id, client_created_at, payload}`，`Idempotency-Key` 等于 `operation_id`）。成功响应复用 `CreativeAssetCreateResult`（`api/openapi.yaml`）。
- **权限**：场景属于当前账号，账号有创意空间写入权限（同 `/creative/assets/from-canvas-node`）。
- **只读场景**：不改场景，不产生场景修订；只写库副本和资产库。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `document_id` | string | 是 | 场景文档 ID |
| `object_id` | string | 是 | `kind=model_instance` 的对象 |
| `expected_object_rev` | CreativeRevision | 是 | 该对象**内容单元** `object:<id>:content` 的当前 `rev`。模型引用和材质分配都在这个单元里（修订 v11、v12），防止存入摄影师没看到的模型或材质 |
| `thumbnail_upload_id` | string | 是 | 缩略图的暂存上传（修订 v12）：声明类型 `image` 的 PNG，与 `commit_model` 的缩略图同一套暂存上传。编辑器按实例当前的材质渲染，256 × 256、取景与烘焙缩略图相同。新建库副本时消费；复用已有资产时不消费，按暂存规则过期回收 |
| `target` | `CreativeUploadAssetTarget` | 是 | 沿用现有结构：`title` 必填（1–200），`description`（≤ 1000 码点）、`group_ids`、`tag_ids`、`new_tags`、`is_favorite` 可选 |

### 成功响应
- 201：已新建库副本和资产，`reused = false`；`content_revision_id` 是库副本修订。
- 200：来源修订相同、材质分配相同的模型资产已存在且不在回收站（修订 v12），`reused = true`，只追加所选分组和标签；`title`、`description` 是已有资产的值，本次传入的标题、描述和收藏不生效。

两者的语义都与 `/creative/assets/from-canvas-node` 一致，随幂等回执重放。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 对象不是模型实例；`target` 字段不合法；缩略图上传不是 PNG 图片 | 修正 |
| 400 | `creative_model_unavailable` | 实例引用的模型修订不存在、不是完整形态或不可展示（修订 v11：与场景模型命令一致，不用 404 和 `creative_state_conflict`） | 提示不能存入 |
| 404 | `not_found` | 场景不存在或不属于当前账号；对象不在场景当前快照里；`target` 引用的分组或标签不存在（同 `/creative/assets/from-canvas-node`） | 重读 |
| 409 | `creative_document_conflict` | `expected_object_rev` 与当前不符；`details.conflicts` 为 `[{unit: "object:<id>:content", expected_rev, current_rev}]`，与提交场景命令相同 | 重读，确认看到的是要存的模型后重试 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 409 | `creative_upload_unavailable` | 需要新建库副本时，缩略图上传不存在、属于其他账号、已过期、已消费或尚未就绪（修订 v12，与 `commit_model` 相同） | 重新渲染并上传缩略图后重试 |
| 409 | `idempotency_conflict` 等 | 公共约定 | — |

### 调用示例
```json
{"operation_id": "6e7f8091-a2b3-4c4d-8e5f-60718293a4b5", "client_created_at": "2026-10-28T09:00:00Z",
 "payload": {"document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10", "object_id": "sobj_Z1x2C3v4B5n6M7a8S9d0f",
             "expected_object_rev": "6", "thumbnail_upload_id": "cupl_5d0c",
             "target": {"title": "哥特束柱", "description": "可调高度的束柱", "group_ids": ["cgrp_31aa"]}}}
```
201：
```json
{"asset_id": "cast_90ab", "revision": "1", "content_revision_id": "ccrev_9e2a", "library_revision": "211", "tag_mapping": {}, "reused": false, "title": "哥特束柱", "description": "可调高度的束柱"}
```
409（摄影师看到的是修订 6，之后助手改了参数或材质）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
           "details": {"conflicts": [{"unit": "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content", "expected_rev": "6", "current_rev": "9"}]}}}
```

### 兼容与变更
首次定义。**从资产库放入场景**不新增接口：编辑器用资产的 `content_revision_id` 调 `add_objects`（`kind=model_instance`），`material_slots` 写资产的 `materials`。之后编辑总是分叉（`data: 模型资产`「库副本只属于资产库」），资产不受影响。修订 v12 新增必填的 `thumbnail_upload_id`；接口尚未上线，不保留旧形态。`CreativeAssetCreateResult` 已于 C6 起草时（2026-10-20）核对，示例与之一致。资产相关接口的 `kind` 枚举（资产列表筛选、`CreativeLibraryAsset.kind`、`CreativeAssetFacets.kinds`）加 `model`，是兼容扩展。

## OAuth 授权服务器

### 用途与调用
- **用途**：让外部助手按 MCP 授权规范取得、续期访问令牌。成功的标志是宿主拿到受众为 `<PUBLIC_BASE_URL>/mcp` 的访问令牌和 refresh token。
- **消费者**：四个首批宿主的 MCP 客户端（Codex CLI、Claude Code 在本机发起；claude.ai 连接器、ChatGPT 在各自服务端发起）；`/oauth/authorize` 的确认页（前端）。
- **调用**：
  - `GET /.well-known/oauth-protected-resource/mcp`：受保护资源元数据（RFC 9728）。
  - `GET /.well-known/oauth-authorization-server`：授权服务器元数据（RFC 8414）。
  - `POST /oauth/register`：动态客户端注册（RFC 7591，兼容用途）。
  - `GET /oauth/authorize`：授权请求，由前端页面承接。
  - `POST /api/v1/oauth/authorize/decision`：确认页提交「允许」或「拒绝」（网页端鉴权）。
  - `POST /oauth/token`：授权码换令牌、refresh。
- **格式**：元数据、注册、令牌接口为 `application/json`；令牌请求体为 `application/x-www-form-urlencoded`（OAuth 规定）。错误体使用 OAuth 标准错误格式 `{error, error_description}`，不使用项目 `ErrorEnvelope`，因为调用方是遵循 OAuth 的通用客户端。`decision` 接口是网页端接口，沿用 `ErrorEnvelope`。
- **权限**：元数据、注册、授权请求、令牌接口无需鉴权（OAuth 协议本身的公开端点），访问边界由 PKCE、回调地址校验和一次性授权码保证。`decision` 需要网页端 Bearer（已登录的摄影师），连接建在该账号下。

### 受保护资源元数据
`GET /.well-known/oauth-protected-resource/mcp`，无请求参数。200：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `resource` | string | 是 | `<PUBLIC_BASE_URL>/mcp` |
| `authorization_servers` | string[] | 是 | `[<PUBLIC_BASE_URL>]` |
| `scopes_supported` | string[] | 是 | `["creative"]` |
| `bearer_methods_supported` | string[] | 是 | `["header"]` |
| `resource_name` | string | 是 | 「创意空间」 |

### 授权服务器元数据
`GET /.well-known/oauth-authorization-server`，无请求参数。200：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `issuer` | string | 是 | `<PUBLIC_BASE_URL>` |
| `authorization_endpoint` | string | 是 | `<PUBLIC_BASE_URL>/oauth/authorize` |
| `token_endpoint` | string | 是 | `<PUBLIC_BASE_URL>/oauth/token` |
| `registration_endpoint` | string | DCR 保留时 | `<PUBLIC_BASE_URL>/oauth/register`（见 design U2） |
| `response_types_supported` | string[] | 是 | `["code"]` |
| `grant_types_supported` | string[] | 是 | `["authorization_code", "refresh_token"]` |
| `code_challenge_methods_supported` | string[] | 是 | `["S256"]` |
| `token_endpoint_auth_methods_supported` | string[] | 是 | `["none", "client_secret_post", "client_secret_basic"]` |
| `scopes_supported` | string[] | 是 | `["creative"]` |
| `client_id_metadata_document_supported` | boolean | 是 | `true` |
| `authorization_response_iss_parameter_supported` | boolean | 是 | `true` |

### 动态客户端注册
`POST /oauth/register`，body JSON：

| 字段 | 类型 | 必传 | 约束 |
|---|---|---|---|
| `client_name` | string | 是 | 1–100 字符 |
| `redirect_uris` | string[] | 是 | 1–5 个，回环地址或 https |
| `grant_types` | string[] | 否 | 缺省 `["authorization_code","refresh_token"]`；只能是这两个的子集 |
| `token_endpoint_auth_method` | string | 否 | 缺省 `none`；可为 `client_secret_post`、`client_secret_basic`；其他值拒绝 |
| 其他 RFC 7591 字段 | — | 否 | 原样存入 `metadata`，不影响行为 |

201：`{client_id, client_id_issued_at, client_name, redirect_uris, grant_types, token_endpoint_auth_method, client_secret, client_secret_expires_at}`。`client_secret` 只在机密客户端时返回（只返回这一次），`client_secret_expires_at` 为 `0`（不过期）；公共客户端不含这两个字段。

失败（400）：`invalid_redirect_uri`（不是回环或 https、超过 5 个）；`invalid_client_metadata`（缺 `client_name`、不支持的认证方式或授权类型）。同一 IP 每小时注册 ≤ 20 次，超出 429，带 `Retry-After`。

### 授权请求
`GET /oauth/authorize`，query：`response_type=code`、`client_id`、`redirect_uri`、`code_challenge`、`code_challenge_method=S256`、`state`、`resource`（必须等于 `<PUBLIC_BASE_URL>/mcp`）、`scope`（可选，只能是 `creative`）。

- 服务端先校验客户端：`client_id` 是 https URL 时按 CIMD 拉取元数据（只允许 https、公网地址、≤ 5 KB、3 秒超时，元数据里的 `client_id` 必须等于该 URL）；否则必须是已注册的 DCR 客户端。`redirect_uri` 必须精确匹配（回环地址允许端口不同）。
- 客户端或回调地址不合法时，**不重定向**，直接显示错误页（防止开放重定向）。
- 其他参数错误（缺 PKCE、`resource` 不符、`scope` 不支持）时，重定向回 `redirect_uri`，带 `error=invalid_request|invalid_scope|invalid_target`、`state`、`iss`。
- 合法时返回前端确认页。前端未登录则走现有登录，登录后回到确认页。确认页显示 `client_name`、回调地址主机、能做的事和数据去向（design「授权与 consent」）。

### 确认授权
`POST /api/v1/oauth/authorize/decision`，网页端 Bearer，body JSON：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `request` | string | 是 | 授权请求页面拿到的签名请求包（服务端校验过的 authorize 参数，10 分钟有效，防篡改） |
| `decision` | string | 是 | `allow` / `deny` |

200：`{redirect_to}`，前端跳转。`allow` 时建连接和授权码，`redirect_to` 带 `code`、`state`、`iss`；`deny` 时 `redirect_to` 带 `error=access_denied`、`state`、`iss`。

失败：400 `validation_failed`（请求包过期或签名不符，前端提示重新从外部助手发起）；401 沿用网页端处理；403 `forbidden`（账号没有创意空间能力）。

### 令牌
`POST /oauth/token`，form：

| 字段 | 必传 | 说明 |
|---|---|---|
| `grant_type` | 是 | `authorization_code` 或 `refresh_token` |
| `client_id` | 是 | 必须与授权码或 refresh token 所属连接的客户端一致；机密客户端按注册的认证方式另带密钥（表单 `client_secret` 或 HTTP Basic），不符时 `invalid_client` |
| `code`、`redirect_uri`、`code_verifier` | `authorization_code` 时 | `redirect_uri` 与授权请求一致；`code_verifier` 经 S256 后等于 `code_challenge` |
| `refresh_token` | `refresh_token` 时 | — |
| `resource` | 是 | 必须等于 `<PUBLIC_BASE_URL>/mcp` |

200（`Cache-Control: no-store`）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `access_token` | string | JWT，`aud=<PUBLIC_BASE_URL>/mcp`、`sub=<connection_id>`、`acct=<account_id>`、`scope=creative`，HS256，签名密钥按用途从网页 JWT 密钥派生 |
| `token_type` | string | `Bearer` |
| `expires_in` | integer | 3600 |
| `refresh_token` | string | 新的 refresh token；每次 refresh 都轮换 |
| `scope` | string | `creative` |

失败（400，OAuth 错误格式）：
- `invalid_grant`：授权码无效、过期、已用（已用时同时断开连接）；PKCE 不符；`redirect_uri` 不符；refresh token 无效、过期、已轮换（已轮换时断开连接）；连接已断开。宿主需要重新授权。
- `invalid_client`：`client_id` 不符。
- `invalid_target`：`resource` 不符。
- `unsupported_grant_type`：其他授权类型。

### 调用示例
```http
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&client_id=oclient_3b2c1d0e-9f8a-4b7c-8d6e-5f4a3b2c1d0e&code=Qm9n7kYp2sV1xZ4c&redirect_uri=http%3A%2F%2F127.0.0.1%3A43117%2Fcallback&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&resource=https%3A%2F%2Fcrm.example.com%2Fmcp
```
```json
{"access_token": "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJodHRwczovL2NybS5leGFtcGxlLmNvbS9tY3AifQ.sig", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "r1.8f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c", "scope": "creative"}
```
```json
{"error": "invalid_grant", "error_description": "授权码已使用"}
```

### 兼容与变更
首次定义，无已有消费者。DCR 按 MCP 规范已弃用，保留为兼容；若 C1 实测四个宿主都走 CIMD，则不提供 `/oauth/register`，元数据不返回 `registration_endpoint`（只收窄，不影响其他字段）。

## MCP 端点

### 用途与调用
- **用途**：外部助手调用创意空间工具的唯一入口。
- **消费者**：ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页）的 MCP 客户端，经 Codex 插件、Claude 连接器、ChatGPT 应用或本机手动配置接入。
- **调用**：`<PUBLIC_BASE_URL>/mcp`，MCP streamable HTTP 传输，JSON-RPC 2.0。协议版本由官方 Go SDK 协商，服务端按 MCP 2026-07-28 实现，兼容 SDK 支持的旧版本。
- **权限**：`Authorization: Bearer <access_token>`，令牌来自 `api: OAuth 授权服务器`。校验顺序：签名 → `aud` 等于 `<PUBLIC_BASE_URL>/mcp` → 未过期 → 连接未断开。账号范围只来自令牌所属连接（ADR-001）。

### 请求
- MCP 方法：工具发现（`tools/list` 或新版协议的发现方法）与 `tools/call`；客户端声明 MCP Apps 扩展时另有 `resources/read`，只用于读取界面资源（见 `api: MCP 生成与任务卡`）。不提供 prompts、sampling；除界面资源外不提供 resources。
- `tools/call` 的 `name` 与 `arguments` 按 `api: MCP 只读工具`、`api: MCP 写入与生成工具`、`api: MCP 场景与 Skill 工具` 定义。工具清单随子项交付逐步增加，未交付的工具不出现在发现结果里。

### 成功响应
- 连接建立时（`initialize` 或新版协议的发现方法）下发服务端 `instructions`：所有宿主共用的使用说明与运行规则（design「服务端 instructions 与自检」），≤ 4000 字符。
- 工具发现：返回当前部署可用的外部工具，每个带 `name`、`title`（中文显示名，宿主的工具行和许可提示会显示它，例如「列出画布」「写提示词」「生成」「回看」）、`description`（与内置助手同一份说明加外部差异）、`inputSchema` 和 `annotations`（只读工具 `readOnlyHint: true`；`trash_assets` 等删除类工具 `destructiveHint: true`）。部署条件与内置相同：没有生成模型时不列 `set_node_prompt`、`generate_node`；没有压缩器时不列 `compress_image`。客户端声明 MCP Apps 扩展时，挂卡片的工具另带 `_meta.ui`（见两个卡片契约），其余工具带 `_meta.ui.visibility: ["model"]`，卡片不能调用它们。
- `tools/call`：
  - `content` 的第一项是文字，分两段。前一段是摘要：第一行是结论（终端宿主往往只显示前 3–4 行），随后是关键字段、Markdown 格式的画布链接和给外部助手的下一步提示，不超过 8 行。后一段是结构化结果的 JSON 文本，供不读 `structuredContent` 的宿主使用。两段之间空一行。之后是可选的图片（`ImageContent`，长边 ≤ 1024，单次 ≤ 4 张）。
  - `structuredContent` 为同一结构化结果。
  - 只给卡片用的数据（例如带签名的缩略图地址）放在 `_meta.creative`，只在客户端声明 MCP Apps 扩展时返回；外部助手的模型看不到 `_meta`。
  - 摘要由 `creativeagent` 的外部工具门面按工具生成，和工具说明一起维护；不同宿主拿到的文字相同。

### 失败响应
| 信号 | 触发条件 | 调用方处理 |
|---|---|---|
| HTTP 401 + `WWW-Authenticate: Bearer resource_metadata="<PUBLIC_BASE_URL>/.well-known/oauth-protected-resource/mcp", scope="creative"` | 缺令牌、签名或受众不符、过期、连接已断开 | 宿主先 refresh，失败则重新授权 |
| HTTP 403（同上 `WWW-Authenticate`，`error="insufficient_scope"`） | 以后的细粒度授权；当前不会出现 | — |
| JSON-RPC 错误 `-32602` | 工具不存在、参数不符合 `inputSchema` | 修正调用 |
| 工具结果 `isError: true`，文字内容为 `{"code": "<领域错误码>", "message": "<中文说明>", "retry_after_s": <可选>}` | 领域拒绝：`not_found`、`validation_failed`、`conflict`、`forbidden`（账号无创意能力）、`budget_exhausted`（生成额度不足）、`rate_limited`（连接限流，带 `retry_after_s`）、以及各工具已有的错误码 | 按错误码处理；与内置助手看到的错误一致 |

### 交互规则
- **限流**：每个连接每分钟工具调用 ≤ 120 次，同时等待中的任务句柄 ≤ 8 个（初始值，上线后调整，不作承诺）。
- **超时**：单次 `tools/call` 服务端最长处理 45 秒；会超过的工作改为任务句柄（见 `api: MCP 写入与生成工具` 的 `wait_tasks`）。
- **重复调用**：MCP 没有幂等键。宿主重试同一调用可能重复执行写工具；工具说明提示「写入失败后先读再决定是否重试」。领域层已有的版本检查（`draft_revision`、场景 `base_revision` 等）照常拦截过时写入。
- **活动状态**：成功通过鉴权的调用节流更新连接的 `last_used_at`（≤ 1 次/分钟）。

### 调用示例
```http
POST /mcp
Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJodHRwczovL2NybS5leGFtcGxlLmNvbS9tY3AifQ.sig
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": {"name": "list_canvases", "arguments": {"limit": 2}}}
```
```json
{"jsonrpc": "2.0", "id": 7, "result": {"content": [{"type": "text", "text": "最近编辑的 1 张画布：\n1. [教堂中殿分镜](https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f) · 10 月 21 日编辑 · `ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f`\n下一步：用 read_canvas(canvas_id) 读取内容。\n\n{\"items\":[{\"canvas_id\":\"ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f\",\"title\":\"教堂中殿分镜\",\"updated_at\":\"2026-10-21T08:10:00Z\",\"canvas_url\":\"https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f\"}],\"next_cursor\":\"\"}"}], "structuredContent": {"items": [{"canvas_id": "ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f", "title": "教堂中殿分镜", "updated_at": "2026-10-21T08:10:00Z", "canvas_url": "https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"}], "next_cursor": ""}}}
```
```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer resource_metadata="https://crm.example.com/.well-known/oauth-protected-resource/mcp", scope="creative"
```

### 兼容与变更
首次定义。工具的增加是兼容扩展；工具 schema 跟随内置工具的 `ToolEntry.Version`，破坏性变化（删字段、改语义）时在工具说明里写明版本，并在本契约记录。

## 外部助手连接列表与断开

### 用途与调用
- **用途**：接入页列出和断开外部助手连接，并拿到接入文字要用的 MCP 地址；创意空间顶栏显示「外部助手已连接」。
- **消费者**：前端「外部助手」接入页、创意空间顶栏。
- **调用**：`GET /api/v1/creative/agent-connections`；`POST /api/v1/creative/agent-connections/{id}/revoke`。
- **格式**：`application/json`，错误沿用 `ErrorEnvelope`。
- **权限**：网页端 Bearer；列表要 `creative_read`，断开要 `manual_write`；只能看到和断开本账号的连接。

### 请求
列表 query：

| 字段 | 类型 | 必传 | 缺省 | 说明 |
|---|---|---|---|---|
| `active` | boolean | 否 | 不过滤 | `true` 只返回有效且 5 分钟内用过的连接（顶栏用） |
| `include_revoked` | boolean | 否 | `false` | `true` 时一并返回已断开的连接 |

不分页：一个账号的连接数量很小；返回最近创建的 50 个，超出部分不返回（接入页提示「只显示最近 50 个」）。

断开：path `id`，无请求体。

### 成功响应
列表 200：`{mcp_url, items:[…]}`。`mcp_url`（string，必返）为 `<PUBLIC_BASE_URL>/mcp`，接入页据此生成各宿主的接入文字，不在前端写死。`items` 按 `created_at` 倒序：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `id` | string | 是 | 否 | `cxcon_…` |
| `display_name` | string | 是 | 否 | 「Claude」「Claude Code」「Codex」「ChatGPT」或客户端名 |
| `host_kind` | string | 是 | 否 | `claude` / `claude_code` / `codex` / `chatgpt` / `other` |
| `created_at` | string(date-time) | 是 | 否 | 授权时间 |
| `last_used_at` | string(date-time) | 是 | 是 | 从未调用过工具时为 null |
| `active` | boolean | 是 | 否 | 有效且 `last_used_at` 在 5 分钟内 |
| `revoked_at` | string(date-time) | 是 | 是 | 有效时为 null |

断开 200：返回该连接的上述对象（`revoked_at` 非空）。重复断开已断开的连接返回 200 与原对象，不改变 `revoked_at`。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | query 参数不是布尔值 | 修正 |
| 404 | `not_found` | 连接不存在或不属于本账号 | 刷新列表 |
| 403 | 现有 `CodeForbidden` | 账号没有创意空间能力 | 沿用现有处理 |

### 交互规则
- 断开的副作用见 `data: 外部助手连接与 OAuth` 的「断开」，在一个事务里完成；提交后发账号失效通知，顶栏和接入页重读列表。
- 连接由不活动变为活动时也发账号失效通知；变为不活动不通知，前端按 `last_used_at + 5 分钟` 自行到期隐藏。

### 调用示例
```http
GET /api/v1/creative/agent-connections?active=true
Authorization: Bearer <token>
```
```json
{"mcp_url": "https://crm.example.com/mcp", "items": [{"id": "cxcon_7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d", "display_name": "Claude Code", "host_kind": "claude_code", "created_at": "2026-10-21T07:58:12Z", "last_used_at": "2026-10-21T08:10:03Z", "active": true, "revoked_at": null}]}
```
```http
POST /api/v1/creative/agent-connections/cxcon_0000aaaa-1111-4bbb-8ccc-2222dddd3333/revoke
Authorization: Bearer <token>
```
```json
{"error": {"code": "not_found", "message": "连接不存在"}}
```

### 兼容与变更
首次定义，无已有消费者。

## MCP 只读工具

### 用途与调用
- **用途**：外部助手自检、找到画布、读取画布与引用素材、查找和浏览资产库。
- **调用**：经 `api: MCP 端点` 的 `tools/call`。
- **权限**：连接有效；账号 `creative_read`。授权即账号级 consent（design「授权与 consent」）：本账号未进回收站的画布、内容和资产都可读，不再按对话判定可读范围。

### 工具
| 工具 | 来源 | 与内置工具的差异 |
|---|---|---|
| `get_capabilities` | 外部专用 | 见下 |
| `list_canvases` | 外部专用 | 见下 |
| `read_canvas` | 内置同名 | 新增必传 `canvas_id`（string，`list_canvases` 返回的 ID）；文字节点全部可读；结果另带 `canvas_url` |
| `read_asset` | 内置同名 | 入参不变；可读范围为整个账号；图片结果以 `ImageContent` 返回，长边 ≤ 1024；账号模型能力不再参与判定（外部助手自己决定能否看图） |
| `search_assets` | 内置同名 | 入参与结果不变；不需要资产库 consent |
| `read_library` | 内置同名 | 入参与结果不变；不需要资产库 consent |

#### get_capabilities
- 入参：无。
- 结果：
  - `account`：`{display_name}`，授权账号的显示名。
  - `connection`：`{id, display_name, host_kind}`。
  - `generation`：`{models:[{key, display_name, node_type, parameters_summary}], budget}`。`budget` 为 `{remaining, unit, period_end}`，沿用网关的生成预算；积分落地后 `unit` 改为积分，字段不变。部署没有生成模型时 `models` 为空数组。
  - `workspace_open`：boolean，当前有没有打开的创意空间页面。这是账号级实时通道是否有活动订阅的近似判断，只作提示，不作保证。
  - `skills`：`{count}`，可用 Skill 数量（平台加账号自己的）。
  - `hints`：string[]，给外部助手的简短提醒，例如没有打开页面时提示场景任务需要页面。
- 不返回部署配置、版本号或内部地址。
- 错误：只有 `api: MCP 端点` 的通用错误。

#### list_canvases
- 入参：`query`（string，可选，按标题包含匹配，≤ 100 字符）、`limit`（integer，可选，1–50，缺省 20）、`cursor`（string，可选，上一页的 `next_cursor`）。
- 结果：`{items:[{canvas_id, title, updated_at, canvas_url}], next_cursor}`，按 `updated_at` 倒序；`canvas_url` 是创意空间里打开该画布的链接；`next_cursor` 为空字符串表示末页。不含已归档画布。
- 错误：`validation_failed`（参数越界、游标非法）。

### 失败响应
各工具沿用内置工具的错误码；另有 `api: MCP 端点` 的通用错误。`read_canvas` 的 `canvas_id` 不存在或不属于本账号时为 `not_found`。

### 调用示例
```json
{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": {"name": "read_canvas", "arguments": {"canvas_id": "ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"}}}
```
成功时结构化结果与内置 `read_canvas` 相同。失败示例：
```json
{"jsonrpc": "2.0", "id": 8, "result": {"isError": true, "content": [{"type": "text", "text": "{\"code\":\"not_found\",\"message\":\"画布不存在\"}"}]}}
```

### 兼容与变更
首次定义。内置工具的行为不变。
