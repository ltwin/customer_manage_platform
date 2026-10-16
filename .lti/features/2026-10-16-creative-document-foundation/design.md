# 通用文档底座设计

需求与验收见 `proposal.md`，行为规格见 `specs.md`，接口与数据定义见 `contracts.md`。形状来源是 V6-079 design（`.codestable/features/2026-10-15-shoot-plan-document/shoot-plan-document-design.md` §3–§5、§9）中与策划无关的部分。本文只写抽成通用层时新增或改变的决定。

## 现状

- `creative_documents`（迁移 0040）只有 `id`、`account_id`、`type_key`、`title`、`content_revision_id`、`revision`，账号级失效触发器已存在（0042 的 `creative_documents_notify`）。
- 文档行的当前内容修订已经是 GC 持有方（`backend/internal/creativereference/projection.go` 的 `LiveOwnerCondition` 中 `creative_documents` 一行）。
- 唯一的类型 `internal.document` 只能由集成测试夹具创建（`backend/internal/creativecanvas/document_fixture.go` 的 `CreateInternalDocument`、`ReferenceInternalDocument`）；生产环境的 `add_node` 明确拒绝它（`graph_commands.go` 的 `editGraph.add`）。
- 读取 `GET /creative/documents/{id}`（`httpapi/creative_canvas.go` 的 `GetCreativeDocument` → `creativecanvas.ReadDocument`）返回 `CreativeDocument`；前端 `DocumentView.tsx` 用它显示最大化的 `internal.document` 节点。
- 节点快照只对 `internal.document` 读文档：文档不存在时标为不可用（`node_snapshot.go`）。
- 内容层每次 `WriteAndRetain` 都要求同事务里已有持有方（`creativecontent.requireRoot`），只有同一节点来源才追加 sequence；文档的每次保存因此写一条**新内容**，历史顺序由修订表承担（同 V6-079 §3.2）。

## 领域模型与业务规则

### 概念与身份

| 概念 | 身份 | 说明 |
|---|---|---|
| 文档 | `cwdoc_<uuid>`，账号内唯一 | 一行 `creative_documents`；`type_key` 决定由哪个类型解释载荷 |
| 文档修订 | `(document_id, revision)` | 修订号从 1 开始，单调递增、永不复用；每个修订对应一条不可变的内容修订（快照） |
| 单元 | 类型定义的字符串，如 `meta`、`body`、`section:lighting`、`object:sobj_x` | 并发控制的最小粒度；`meta` 由通用层保留，表示标题 |
| 单元修订号 | 单元 → 修订号 | 等于该单元最后一次被改动时的文档修订号 |
| 写入者 | `photographer` / `agent` / `system` | `agent` 时带 `run_id`；`system` 只用于迁移回填 |
| 工作会话指针 | `(document_id, canvas_id)` | 指向一个助手会话；只是指针，不进快照 |

### 不变量

- **D-I1 修订完整**：每份文档的当前修订号都有一行修订记录，其 `content_revision_id` 等于文档行的当前内容修订。迁移为存量文档回填一行（写入者 `system`）。
- **D-I2 单元版本**：单元修订号 ≤ 文档修订号；成功写入后，被改单元的修订号等于新文档修订号，未改单元不变；单元被删除后从映射中移除，再出现时取新修订号。
- **D-I3 原子**：一次命令只产生 0 或 1 个修订；产生修订时，快照、修订行、引用行、文档头一起提交。
- **D-I4 回收站只读**：`trashed_at` 不为空的文档拒绝任何写命令；只有它们可以被彻底删除。
- **D-I5 历史引用受保护**：任一修订快照本身，以及修订引用表里登记的内容修订，都是 GC 持有方；文档彻底删除后才释放。
- **D-I6 账号隔离**：所有表带 `account_id`，查询经 `store.TxAccountScope` 限定账号（ADR-001）。

### 聚合与一致性边界

文档是一个聚合：一次写入在 `creative_documents` 行锁内完成。画布节点和文档是两个聚合，只在 `add_node` 新建时同事务创建；之后节点只持有 `document_id`，文档的修改和删除不改节点行。

## 模块职责与协作

```mermaid
flowchart LR
  HTTP[httpapi<br/>文档路由] --> DOC[creativedocument 新包<br/>存储·命令外壳·历史·回收站·列表·会话·类型注册表]
  CANVAS[creativecanvas<br/>add_node·节点快照] --> DOC
  DOC --> CONTENT[creativecontent]
  DOC --> STORE[platform/store]
  REF[creativereference<br/>LiveOwnerCondition] -. 读修订与引用表 .-> DOC
  TYPES[类型实现<br/>internal.document（本包内）<br/>photo.shoot_plan（V6-079）<br/>scene（epic C1）] -->|Register| DOC
  ROOT[类型所在包的 init()] -->|进程导入时注册| TYPES
```

- **`backend/internal/creativedocument`（新）**：
  - 职责：表的读写、类型注册表、命令外壳（版本检查、通用操作、写修订）、修订历史、回收站与彻底删除、列表、工作会话指针，以及内置的 `internal.document` 类型。
  - 非职责：具体类型的载荷、校验与操作；渲染卡片；画布拓扑。
  - 不 import gin（ADR-003），不 import `creativecanvas`。
- **`creativecanvas`**：
  - `add_node` 对已注册且可新建的类型调 `creativedocument.CreateInTx`；带 `document_id` 时调 `creativedocument.RequirePlaceable`（检查存在、节点类型匹配、不在回收站）后放回。
  - 节点快照对所有带 `document_id` 的节点调 `creativedocument.SummaryBatch`，取摘要和回收站状态。
  - 夹具 `CreateInternalDocument` 改为经 `creativedocument` 创建。
- **`creativereference`**：`LiveOwnerCondition` 增加修订表和引用表两行；`projection_roles_test.go` 登记新表的持有角色。
- **`httpapi`**：新增文档路由，`GetCreativeDocument` 改为调 `creativedocument.Read`。
- **类型注册表**：`creativedocument.Register(Type)` 是包级注册表，在类型所在包的 `init()` 中调用（内置的 `internal.document` 就在 `creativedocument` 包里注册，任何导入该包的进程都能看到），重复注册同一 key 或同一节点类型直接 panic；测试用 `RegisterForTest` 注册并在清理时注销。不用注入：`creativecanvas` 的命令都是包级函数，沿用这种风格。

### 类型端口

```go
// creativedocument.Type 由各文档类型实现；通用层不理解载荷。
type Type interface {
    Key() string                 // 文档类型："internal.document" / "photo.shoot_plan" / "scene"
    NodeTypeKey() string         // 对应的画布节点类型："internal.document" / "photo.shoot_plan" / "core.scene"；双向唯一
    ContentKind() string         // 快照内容的类型："text" / "plan" / "scene"
    Creatable() bool             // add_node 能否新建
    // 新建时的初始快照；opts 来自 add_node 的 document_options（类型自定义，可为空）
    Empty(ctx context.Context, tx store.TxAccountScope, title string, opts json.RawMessage) (Snapshot, error)
    // 载荷当前包含的全部单元（不含 meta）；用于恢复后重置版本、校验 expected
    Units(payload creativecontent.Payload) ([]string, error)
    // 第一阶段：只读地判定每个操作涉及哪些单元，并做外形校验；不得写库。
    // 单元存在性变化时必须同时返回集合单元（如 objects、shots）
    Touched(ctx context.Context, tx store.TxAccountScope, cur Current, actor Actor, ops []json.RawMessage) ([]string, error)
    // 第二阶段：版本检查通过后应用；可以在 tx 内写类型自己的附属数据（如场景提交模型）
    Apply(ctx context.Context, tx store.TxAccountScope, cur Current, actor Actor, retain Retain, ops []json.RawMessage) (Snapshot, Changes, error)
    // restore_units 涉及的单元：列出的单元，加上存在性变化时的集合单元；只读
    RestoreTouched(ctx context.Context, tx store.TxAccountScope, old, cur Current, units []string) ([]string, error)
    // restore_units：用旧快照的这些单元替换当前快照对应部分；返回的 Refs 中新增的部分视为「带回的引用」
    RestoreUnits(ctx context.Context, tx store.TxAccountScope, old, cur Current, retain Retain, units []string) (Snapshot, Changes, error)
    // 读取分支：附加在通用字段旁的类型字段（JSON 对象）
    // units 为 nil 时返回完整读取分支；读旧修订指定 units 时由类型裁剪
    View(ctx context.Context, tx store.ReadTxAccountScope, cur Current, units []string) (json.RawMessage, error)
    // 卡片摘要；批量读取时逐个调用
    Summary(ctx context.Context, tx store.ReadTxAccountScope, cur Current) (json.RawMessage, error)
}

type Snapshot struct { Payload creativecontent.Payload; Refs []string } // Refs：本快照引用的内容修订
type Changes  struct { Changed, Annotated, Removed []string }    // 推进修订号的单元、只做标注的单元、被删除的单元
// Retain 由通用层提供：把类型在事务内新建的内容修订预登记到修订 N+1 的引用行，
// 直接作为 creativecontent.WriteAndRetain 的 retain 参数使用
type Retain   func(creativecontent.Revision) error
type Current  struct { Document Document; Revision int64; Title string; Content creativecontent.Revision; UnitRevs map[string]int64; Refs []string } // 旧修订的各字段取自修订行
type Actor    struct { Kind string; RunID *string }
```

- `restore` 恢复整份文档时不调用类型，直接取旧快照；但会调用可选接口 `RestoreHook`（例如策划要在恢复后保留「已确认」）。
- 标题是通用字段：`rename` 由通用层处理，单元为 `meta`，类型不感知。

## 关键机制

### 一次命令的处理顺序

```text
CommandDocument(id, expected, expected_revision, operations, actor):
  1 校验外壳：operations 1–200；expected 的键 ≤ 4096；请求体 ≤ 1 MiB
    - restore / restore_units 必须单独成批，否则 400
    - restore 批次必须带 expected_revision、不得带 expected；其他批次不得带 expected_revision，否则 400
  2 锁文档行（FOR UPDATE）；不存在 → 404；trashed → 409 creative_state_conflict
  3 restore：expected_revision ≠ 文档当前修订号 → 409（unit "*"）；相等则跳到第 6 步
  4 计算涉及单元：rename → meta；restore_units → Type.RestoreTouched；类型操作 → Type.Touched（都只读）
    - 同一单元被两个操作涉及 → 400
    - 任一涉及单元不在 expected 中 → 400
  5 版本检查，对每个涉及单元 u：
      expected[u] = null 且 u 当前不存在       → 通过
      expected[u] = r    且 unit_revs[u] = r    → 通过
      其余                                      → 冲突
    - 有冲突 → 409 creative_document_conflict，details 列出全部冲突单元
  6 应用：rename 改标题；类型操作调 Type.Apply；restore 取修订 k 的快照与标题；restore_units 调 Type.RestoreUnits
    - Apply / RestoreUnits 收到 Retain 回调：类型新建的内容先预登记到修订 N+1 的引用行（持有方交接）
  7 校验新快照：≤ 512 KiB；新增引用 = 新快照引用 − 当前快照引用
    - restore / restore_units：新增引用必须 ⊆ 修订 k 在引用表中的登记，否则 400；不查展示授权
    - 普通批次：每个新增引用 RequireUsable(display)，否则 404 / 403；经 Retain 预登记的、本事务新建的内容视为已通过
    - 经 Retain 预登记的引用必须全部出现在最终快照引用里，否则回滚（类型错误，按 500 处理并记日志）
  8 写入：新内容修订（WriteAndRetain，retain 回调里插入修订行，作为持有方）
         → 引用行（补齐未预登记的）→ 修订行写入 unit_revs 与 annotated_units → 更新文档头（revision、content_revision_id、title、unit_revs、updated_at）
    - Changed 的单元推进到 N+1；Annotated 的单元不推进
  9 回执：{revision, changed_units, annotated_units, removed_units, reset_required}
    - 整份 restore 恒为 reset_required = true，列表为空；其他批次三个列表合计 > 48 KiB 时同样处理
```

- 第 4、5 步在任何写入之前完成，所以冲突一定比校验错误先报，且不会产生半截副作用。
- 第 6 步之后的任何错误都使整个事务回滚（包括类型在 `Apply` 里写的附属数据）。
- 幂等沿用 `creativeops.Executor`：同一 operation id 回放原回执。

### 恢复

| 操作 | 结果 |
|---|---|
| `restore {revision: k}` | 前置：`expected_revision` 等于当前文档修订号。读修订 k 的快照 → 新修订 N+1，载荷等于 k，标题恢复为 k 时的标题；`unit_revs` 重建为 `Units(k 的载荷)` ∪ `{meta}`，全部设为 N+1；修订行 `restored_from = k`；`changed_units` 记为 `["*"]` |
| `restore_units {from_revision: k, units}` | 单独成批；涉及单元由 `Type.RestoreTouched` 给出，当前不存在的单元在 `expected` 中写 null。读修订 k 的快照 → `Type.RestoreUnits`；`meta` 由通用层直接恢复标题；被改单元设为 N+1，被删除单元移出映射；修订行 `restored_from = k` |

k 不存在或大于等于 N 时返回 400。

### 新建与放回

- **新建**：`add_node` 的节点 `type_key` 经注册表映射到可新建的文档类型、可选的 `title` 和 `document_options`。`creativedocument.CreateInTx` 调 `Type.Empty` → 写内容修订 → 文档行（修订号 1、`unit_revs` 全为 1）→ 修订行（写入者为调用者）→ 引用行；画布再写节点（`document_id` 指向它）。撤销这次画布改动只移除节点。
- **放回**：`add_node` 带 `document_id`（与内容、素材互斥），节点类型必须等于该文档类型注册的 `NodeTypeKey`；回收站中的文档不能放回（409）。
- **节点快照**：对所有带 `document_id` 的节点，批量读文档头，调 `Type.Summary`。
  - 文档不存在（已彻底删除）→ 节点 `status.content_state = "unavailable"`。
  - 在回收站 → `document_trashed = true`，摘要照常返回。
  - 类型未注册（例如回滚到旧版本）→ 不可用。
  - `internal.document` 的摘要保持现在的 `content` 预览字段，前端不变。

### 回收站与彻底删除

- `trash` / `untrash` 只改 `trashed_at`，不产生修订，文档头 `updated_at` 更新（触发失效通知）。
- 彻底删除：只允许 `trashed_at` 不为空的文档；同一事务删除工作会话指针、引用行、修订行、文档行。快照内容修订属于非媒体类型，不在现有媒体 GC 的回收范围内（`creativemedia/gc.go` 的 `unheldMediaRevision` 只处理 image / video / audio）。首版保留为无入口数据，不承诺物理回收，后续需要时再加文档快照清理任务。快照引用的媒体在失去全部持有方后，照常由媒体 GC 回收。节点保留 `document_id`，之后读快照时显示不可用。

### 列表与历史分页

- **文档列表**：游标为 `(updated_at, id)` 倒序。过滤 `type`（精确）、`q`（标题包含，大小写不敏感，转义 `%` `_`）、`trashed`（缺省 false）。`limit` 缺省 20、最大 50。游标绑定账号和过滤条件，换条件后旧游标返回 400（沿用画布列表游标的做法）。
- **修订列表**：游标为 `revision` 倒序，可按 `run_id`、`since_revision` 过滤，`limit` 缺省 50、最大 100。每项不含快照，只含元数据。

## 接口与数据

契约变化全部是**扩展**，定义见 `contracts.md`：

- **architecture**：ADD 通用文档底座。
- **patterns**：ADD 文档类型端口。
- **data**：ADD 通用文档。
- **api**：ADD 读取文档（在现有路由上扩展字段）、提交文档命令、文档进回收站、文档移出回收站、彻底删除文档、列出文档、列出文档修订、读取文档旧修订、读取文档工作会话、画布新建与放回文档节点。

兼容性：`CreativeDocument` 改为按 `type_key` 区分的 `oneOf`。`internal.document` 分支保留原有全部字段，所以已有响应字节只多出新字段。TS 生成类型变为联合类型，`DocumentView.tsx` 和 `WorkspacePage.tsx` 按 `type_key === 'internal.document'` 收窄，行为不变。

## 关键决策

| 决策 | 采用 | 理由 / 备选 |
|---|---|---|
| 单元修订号存哪里 | `creative_documents.unit_revs JSONB`（当前映射），随文档头在同一行锁内更新 | 通用层要能做版本检查和恢复重置，不能放在类型载荷里；备选「每单元一行」的表在 4096 单元内没有收益，还多一次查询 |
| 每次保存写新内容还是追加 sequence | 写新内容（新 `content_id`） | 内容层只有同一节点来源才追加（`creativecontent.write`）；历史顺序由修订表承担，与 V6-079 §3.2 一致 |
| 类型操作两阶段 | `Touched`（只读）→ 版本检查 → `Apply` | 冲突在任何写入之前报出；场景的 `commit_model` 等附属写入只在检查通过后发生 |
| 整份恢复的版本检查 | 独立的 `expected_revision`，比较文档修订号 | `meta` 只代表标题版本，用它做整体检查会漏掉只改正文的并发写入；与 V6-079 原方案一致（评审 #1） |
| 不存在单元的预期值 | `expected` 的值允许 null，表示预期不存在；存在性变化时同时检查集合单元 | `CreativeRevision` 只接受正整数，原设计的「视为 0」无法表达；集合单元让「不存在 → 新增 → 删除」之后的旧请求冲突（评审 #2） |
| 恢复带回的引用 | 只校验它登记在本文档修订 k 的引用表中，不查展示授权；读取时照常检查 | 历史版本里授权已撤销的图片仍可恢复并显示占位（V6-079 §3.3）；客户端无法借恢复引入新的引用（评审 #3） |
| 撤销（Cmd+Z）不由底座提供 | 撤销栈在前端，只含本次打开期间摄影师自己的操作；撤销以反向操作的普通命令提交，按单元检查版本 | 协同原则（owner 2026-10-16）：Cmd+Z 只管自己的操作；历史列表和本轮清单可以明确地回退；助手总会被告知。底座只需提供修订历史（含 `since_revision`）和恢复，不需要记录「撤销了哪个修订」 |
| 写权限 | HTTP 写入检查现有 `manual_write`；助手写入沿用画布工具已有的运行授权检查 | 现有能力枚举（`store/creative_foundation.go` 的 `RequireCreativeCapability`）没有 `creative_write`，新造名称会让正常账号的写请求全部被拒（审计 #1） |
| 彻底删除的外形 | `POST /documents/{id}/purge`，标准命令封套，200 JSON 回执 | 执行器要求非零 `client_created_at`，只接受 200/201/202 的 JSON 对象回执（`creativeops/executor.go`）；创意接口没有 `DELETE` 路由，`empty-trash` 也是 POST（审计 #2） |
| 回执有界 | 去掉单元修订号映射（可由 `revision` + `changed_units` 推出）；整份恢复和超大批次只回 `reset_required` | 执行器回执上限 64 KiB；场景 2000 个对象整份恢复时，列出全部单元会超过上限（审计 #3） |
| 类型内新建内容 | 通用层传 `Retain` 回调，预登记到修订 N+1 的引用行 | 内容层 `WriteAndRetain` 返回前要求已有持有方（`creativecontent.write` 末尾的 `requireRoot`），通用层在 `Apply` 之后才写引用行会得到 `ErrMissingRoot`（审计 #4） |
| 历史单元版本 | 修订行保存写入后的完整 `unit_revs`，以及 `annotated_units` | 恢复钩子要判断旧版本「当时是否已确认」（V6-079 §4.4）；确认类操作只标注、不推进修订号（审计建议） |
| 节点类型映射 | 端口声明 `NodeTypeKey`，双向唯一 | 场景是 `core.scene` 节点对应 `scene` 文档，画布不能写死特例（审计建议） |
| `restore_units` 单独成批 | 是 | 「带回的引用」和「普通新增的引用」不必按操作归因，两条校验路径清楚 |
| 注册表形式 | 包级注册表，组合根启动时注册 | `creativecanvas` 的命令都是包级函数，注入需要大改；重复注册直接 panic，防止配置错误静默生效 |
| `internal.document` | 迁入 `creativedocument` 作为内置类型（单元 `body`，操作 `set_body`），不可新建 | 有现成数据和前端视图，作为验收载体不需要另造测试类型；保持不对生产开放 |
| 存量数据 | 迁移回填修订行（写入者 `system`）和 `unit_revs` | 保证 D-I1，代码里不需要「没有修订行」的分支 |

## 挂载点

- `backend/internal/platform/store/migrations/0069_creative_document_foundation.up.sql`
- `backend/internal/platform/store/migrations/0069_creative_document_foundation.down.sql`
- `backend/internal/platform/store/creative_document_foundation_migration_test.go`（新回滚链头 `rollbackDocumentFoundation`，先回滚 0069 再调 `rollbackPlatformSkillOwner`）
- `backend/internal/platform/store/creative_graph_migration_test.go`、`backend/internal/platform/store/creative_library_migration_test.go`（迁移标签列表）
- 同目录下其余带回滚标签列表的迁移测试：`auth_legacy_cutover_harness_test.go`、`auth_limiter_test.go`、`auth_migration_test.go`、`creative_platform_skill_owner_migration_test.go`、`planning_migration_test.go`、`settings_availability_migration_test.go`、`store_test.go`、`telegram_digest_migration_test.go`（实施时发现，回滚链加了 0069 就要同步）
- `backend/internal/creativedocument/`（新包：`document.go`、`registry.go`、`command.go`（含恢复）、`history.go`、`trash.go`、`list.go`、`conversation.go`、`internal_type.go` 及测试）
- `backend/internal/creativecanvas/graph_commands.go`：`editGraph.add`
- `backend/internal/creativecanvas/graph_model.go`：`Action`（新增 `DocumentID`、`DocumentOptions`）
- `backend/internal/creativecanvas/node_snapshot.go`
- `backend/internal/creativecanvas/node_view.go`（`document_summary`、`document_trashed`）
- `backend/internal/creativecanvas/service.go`：`Node` 加两个只读展示字段（实施时发现，结构体定义在这里）
- `backend/internal/creativecanvas/definitions.go`：`NodeDefinitions`（文档类型节点由注册表提供定义）
- `backend/internal/creativecanvas/document_fixture.go`
- `backend/internal/creativereference/projection.go`：`LiveOwnerCondition`
- `backend/internal/creativereference/projection_roles_test.go`、`document_holder_test.go`（历史引用持有测试）
- `backend/internal/platform/httpapi/creative_canvas.go`：`GetCreativeDocument`
- `backend/internal/platform/httpapi/creative_documents.go`（新路由）
- `api/openapi.yaml` 及生成物 `frontend/src/api/schema.d.ts`、后端生成代码（含 `creativecanvas/tool_schemas.gen.go`）
- `frontend/src/creative-canvas/editor/DocumentView.tsx`（类型收窄）。`api.ts`、`WorkspacePage.tsx` 不用改：`CreativeDocument` 目前只有一个分支，`WorkspacePage` 打开视图前本来就按 `type_key` 判断

## 验证与交付

| 验收（proposal） | 方法 | 完成判据 |
|---|---|---|
| 新建、撤销、放回、不可新建的类型 | `creativecanvas` 真实库测试，用测试类型（`RegisterForTest`）和 `internal.document` | 行的出现与回滚符合预期；撤销后文档可读、可放回 |
| 读取兼容 | httpapi 测试比对 `internal.document` 响应字段；前端 `make check-frontend` | 原字段全部存在；前端编译与现有 e2e 通过 |
| 改不同单元都成功、改同一单元一方冲突 | `creativedocument` 真实 PostgreSQL 并发测试（两个事务用屏障同时提交，各跑 20 次） | 不同单元两个都成功且修订号连续；同一单元恰好一个 409，没有多余修订 |
| 非法批次、restore 单独成批、幂等 | 单元测试 + httpapi 测试 | 状态码与错误码一致，无修订产生 |
| 整份恢复使旧令牌失效、按单元恢复只动指定单元 | 真实库测试 | 恢复后用旧修订号提交得到 409；未恢复单元的内容和修订号不变 |
| 整份恢复检测并发修改 | 真实库测试：摄影师读到修订 N → 助手只改正文 → 摄影师以 `expected_revision = N` 恢复 | 409，`unit` 为 `*`，当前内容未被覆盖 |
| 恢复已删除的单元、阻止旧请求重放 | 测试类型含集合单元：删除对象 X → 以 `X = null`、集合单元为当前修订号恢复 X；另做「不存在 → 新增 → 删除」后重放原请求 | 恢复成功，X 回到旧内容；重放因集合单元修订号变化而 409 |
| 写权限 | httpapi 测试：只有 `creative_read` + `manual_write` 的正常账号能写；去掉 `manual_write` 后 403 | 状态码正确 |
| 彻底删除丢响应后重试 | httpapi 测试：首次成功但丢弃响应，同 key 重放 | 返回同一份 200 回执，无副作用 |
| 回执有界 | 测试类型构造 2000 个对象的文档：整份恢复；删除 2000 个单元的批次；重放 | 都成功提交，回执 < 64 KiB，`reset_required = true`；重放返回同一份回执 |
| 类型内新建内容 | 测试类型的 `Apply` 先新建内容再报错；另一例新建内容但最终快照不引用它 | 两例都整体回滚，没有残留内容与引用行；正常路径不出现 `ErrMissingRoot` |
| 标注不推进版本 | 测试类型的标注操作；恢复到标注后的旧版本 | 标注单元修订号不变；旧修订行的 `unit_revs` 可供恢复钩子读取 |
| 恢复含失效引用的旧版本 | 旧修订引用图片 A，当前已移除，撤销 A 的展示授权后恢复；普通操作新增 A；测试类型的 `RestoreUnits` 故意带回一个不在登记中的引用 | 恢复成功，读取时 A 标为不可用；新增 A 返回 403；越界引用返回 400 |
| 历史按运行过滤、读旧修订指定单元 | 真实库测试 | 结果顺序与过滤正确，游标翻页无重复无遗漏 |
| 回收站只读、只能删回收站中的、删除后节点不可用 | 真实库测试 + 节点快照测试 | 各错误码正确；删除后节点 `unavailable` |
| 历史引用受 GC 保护 | `creativereference` 守卫测试 + 媒体 GC 测试（只被旧修订引用的图片不回收，删除文档后可回收） | 守卫测试列出新表；GC 行为符合 |
| 账号隔离 | 每个新接口的跨账号测试 | 一律 404，列表不出现 |
| 迁移与回滚 | store 迁移测试：回填后 D-I1 成立；有修订历史时回滚被拒绝 | 新链头接入，所有迁移测试通过 |

门禁：`make check-go PKG=./internal/creativedocument/...` 等按包运行；改动 OpenAPI 后运行 `make generate`、`make generate-check`；合回 `develop` 前跑 `make check`。

部署：只加列、加表、回填，不改已有列的语义，可以先迁移再发布。没有 Agent 工具变化，不需要排空运行。

GC 部署顺序：`LiveOwnerCondition` 编译进 server 和 creative-worker 两个进程。本 change 只有 `internal.document`，它不写引用，新的持有方登记暂时只覆盖文本快照（本来就不被 GC 回收），所以不受影响。之后写引用的类型（策划、场景）上线时，必须先让所有 creative-worker 运行包含新持有方条件的版本，再开放引用写入，避免旧版 GC 漏认新表而回收只被历史引用的媒体。这条写进各类型的上线步骤。

## 未决事项

| 问题 | 影响 | 解决方式 | 最晚时点 |
|---|---|---|---|
| 组合根的实际文件（server 和 worker 是否都需要注册类型） | 挂载点 | 实施第一步读 `backend/cmd/` 确认；worker 若不读文档就不注册 | 实施开始时 |
| `q` 标题搜索是否需要索引 | 列表性能 | 首版用 `ILIKE`，账号内文档量小；超过 1 万份再加 trigram 索引 | 不阻塞 |

## 实施记录（2026-10-16）

实施中与本设计原文不同的地方，都已改回正文；这里集中列出，供 verify 对照。

| 项 | 原设计 | 实施 | 理由 |
|---|---|---|---|
| 类型注册 | 组合根 `cmd/server`、`cmd/creative-worker` 启动时注册 | 类型所在包的 `init()` 注册 | 任何导入包的进程都看得到，测试和两个进程不会漏注册；两个 `main.go` 不用改 |
| 端口签名 | `Touched` 无 `actor`；`View` 无 `units`；载荷为 `json.RawMessage` | `Touched` 带 `actor`；`View` 带 `units`；载荷为 `creativecontent.Payload` | 助手和摄影师可涉及的单元可能不同；读旧修订按单元裁剪由类型做；载荷直接复用内容层类型 |
| 放回检查 | `RequireReadable` | `RequirePlaceable` | 同时检查节点类型匹配和回收站，名字按用途取 |
| 内部文档夹具 | 经 `creativedocument` 创建 | 新增 `CreateInternalInTx`，保留草稿自带的权利声明；`ReferenceInternalDocument` 也改用 `RequirePlaceable` | `internal.document` 不可新建，`CreateInTx` 走不通；夹具原来就带权利声明 |
| `add_node` 写入者 | 写入者为调用者 | 记为摄影师 | 画布命令没有执行者信息；助手的画布工具在 Go 里自己构造动作，目前走不到新建文档的路径。助手需要新建文档时再传入执行者 |
| 文档节点尺寸 | 未规定 | 380×260 | 沿用夹具原来的 `internal.document` 节点尺寸 |
| 节点快照 | `internal.document` 继续返回 `content` 预览 | 只有 `internal.document` 返回 `content` 预览，其他类型只给 `document_summary` | 其他类型的快照可能很大（场景），卡片用摘要 |
| 单元名 | `[a-z0-9_:.-]` | `[A-Za-z0-9_:.-]` | 场景物件 ID 是含大写的 nanoid |
| 修订列表 / 旧修订 | 不含 `annotated_units`、`removed_units`；旧修订不含 `unit_revs` | 都补上，修订表加 `removed_units` 列 | 「本轮改动」清单要区分改动、确认和删除；按单元恢复前要知道该修订的单元修订号。契约同步补充，需 owner 确认 |
| 能力检查测试 | 去掉 `manual_write` 后 403 | 账号非 active 时读写都 403 | 创意能力现在只由账号状态决定，`creative_account_capabilities` 不再参与，无法单独关闭写能力 |
| `add_node` 的 `type_key` | 开放已注册文档类型 | OpenAPI 枚举加 `internal.document`；以后的类型在各自 change 里加 | 保持枚举，前端类型不变宽 |
| 修订缺失 | 未区分 | 新增 `ErrRevisionNotFound`：读旧修订返回 404「修订不存在」，两种恢复返回 400 | 迁移只回填当前修订，旧文档的更早修订号不存在，不能报成「文档不存在」 |
| 摘要批量读取 | 未规定 | `SummaryBatch` 与 `List` 共用 `summarize`，内容和引用各一次批量查询 | 画布刷新和文档列表不能随文档数线性增加查询 |
| 测试顺序 | 2.2 测试先行 | 2.x 的测试写在实现之后 | 补做了变异检查：7 处关键判断改坏后对应测试都失败，另补了类型诚实性检查的测试 |

## 实施审计处置（2026-10-16）

| 问题 | 结论 | 处理 |
|---|---|---|
| P2 修订历史丢失被删除单元 | 成立，已复现：删除 `item:a` 后历史只记 `items` | 修订表加 `removed_units` 列（0069 尚未合并，直接改迁移），持久化并在修订列表、读旧修订返回；补删除和按单元恢复到不存在两个场景的测试 |
| P2 摘要批量读取 N+1 | 成立，已复现：5 份文档 5 次引用查询 | 抽出 `summarize` / `currentRefs`，按 `(document_id, revision)` 一次取回；守卫测试用 `pg_stat_xact_user_tables` 在同一事务内数引用表扫描次数，1、5、20 份文档都为 1 |
| P3 恢复缺失历史误报文档不存在 | 成立，已复现 | 区分 `ErrRevisionNotFound`：恢复入口转为 400「第 k 版没有保存历史」，读旧修订为 404「修订不存在」 |

## 评审处置（2026-10-16 第 1 轮）

| # | 结论 | 处置 |
|---|---|---|
| 1 | 阻塞：整份恢复只检查 `meta`，会覆盖只改了正文的并发写入 | 采纳。新增 `expected_revision`，按文档修订号检查（§一次命令的处理顺序第 3 步，contracts） |
| 2 | 阻塞：恢复已删除单元没有合法的预期值 | 采纳。`expected` 的值允许 null；存在性变化时同时检查集合单元（端口新增 `RestoreTouched`） |
| 3 | 高风险：恢复带回的历史引用走展示授权检查，会阻断恢复 | 采纳，沿用 V6-079「失效引用仍可恢复、显示占位」的语义（待 owner 确认）。带回的引用改为校验历史登记 |
| 4 | 建议：彻底删除的物理保留边界 | 采纳。写明快照不被现有媒体 GC 回收，首版保留为无入口数据 |
| 5 | 建议：统一迁移口径 | 采纳。proposal 改为「回填一条 system 修订；存在非 system 修订或回收站状态时拒绝回滚」 |
| 6 | 演进备注：GC 进程先升级再开放引用写入 | 采纳，写入部署说明 |

## 方案审计处置（2026-10-16）

| # | 结论 | 处置 |
|---|---|---|
| 1 | 阻塞：写权限名 `creative_write` 不存在 | 采纳。改用现有 `manual_write`；助手沿用运行授权检查 |
| 2 | 阻塞：`DELETE` + 204 无法接入幂等执行器 | 采纳。改为 `POST /purge`，标准封套，200 JSON 回执（外形待 owner 确认） |
| 3 | 阻塞：整份恢复的回执可能超过 64 KiB | 采纳。去掉单元修订号映射，加 `reset_required`（外形待 owner 确认） |
| 4 | 高风险：类型内新建内容缺少持有方交接 | 采纳。`Retain` 回调预登记，写入前核对 |
| 5 | 建议：历史单元版本的归宿 | 采纳。修订行保存 `unit_revs`、`annotated_units`；端口区分推进与标注 |
| 6 | 建议：节点类型与文档类型映射 | 采纳。端口新增 `NodeTypeKey` |

按「文档 1 轮评审、阻塞清零即关闭」处理，不再发起第 2 轮。
