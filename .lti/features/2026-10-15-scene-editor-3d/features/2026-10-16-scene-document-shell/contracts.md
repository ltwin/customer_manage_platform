<!-- 本 change 的局部契约增量。epic 目标契约（architecture: 场景模块、data: 场景文档、api: 读取场景、api: 提交场景命令）经 epic bind --deliver 交付，正文不复制到这里。 -->

## data: ADD 场景内置基础体

### 用途与存储角色
- **用途**：服务端校验 `objects[].primitive` 的依据，也是前端几何生成和参数面板的参数来源。
- **存储**：代码仓库中的一份 JSON，`backend/internal/creativescene/builtins.json`，经 Go `embed` 读入。不进数据库。
- **派生**：`make generate` 原样复制到 `frontend/src/creative-scene/builtins.gen.json`，`make generate-check` 比对漂移。前端不得手写另一份。

### 结构与身份
- 每项的身份是 `<name>@<version>`。版本一经发布，参数和几何语义就不再改变；需要改动时发布新版本（如 `box@2`），旧版本继续可读、可渲染。
- 每项字段：`builtin`、`label`、`mount`（`floor` / `wall` / `ceiling` / `free`）、`params`。`params` 的格式同 `patterns: 几何模块契约` 的参数定义：`number` 必带 `default`、`min`、`max`，可带 `step`、`unit`；`integer` 同 `number`；还有 `boolean`、`enum`、`color`；每项可带 `label`。
- 首批清单（单位为米，Y 轴向上，正面朝 +Z）：

| builtin | mount | 参数（默认值，范围） |
|---|---|---|
| `ground@1` | floor | `size`（40，1…1000） |
| `box@1` | floor | `width`、`height`、`depth`（各 1，0.01…1000） |
| `cylinder@1` | floor | `radius_top`（0.5，0…500）、`radius_bottom`（0.5，0.001…500）、`height`（1，0.01…1000）、`radial_segments`（integer 32，3…128） |
| `sphere@1` | floor | `radius`（0.5，0.01…500）、`segments`（integer 32，8…128） |
| `plane@1` | wall | `width`、`height`（各 2，0.01…1000） |
| `stairs@1` | floor | `width`（1.2，0.1…100）、`step_count`（integer 8，1…100）、`step_height`（0.18，0.01…2）、`step_depth`（0.28，0.01…5） |
| `arch@1` | floor | `width`（2，0.2…100）、`height`（3，0.2…100）、`depth`（0.5，0.05…20）、`opening_width`（1.2，0.1…100）、`opening_height`（2.2，0.1…100） |

### 访问模式与保证
- **服务端校验**：`builtin` 必须在清单中；`params` 的键必须属于该项，类型和单项范围必须符合；缺省的键在写入时补成默认值，所以快照里的参数总是完整的。跨参数约束（如门洞开口不超过外框）不由服务端校验，由几何代码收缩处理。
- **前端**：每个清单项都有一个几何函数。前端测试断言清单与几何函数一一对应。

### 生命周期与演进
- 新增基础体或新版本只改这份 JSON 和前端几何代码，不需要迁移。
- 删除或修改已发布版本属于破坏性变更，不允许。

## api: MODIFY 提交文档命令

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
