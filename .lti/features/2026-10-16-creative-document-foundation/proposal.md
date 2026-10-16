---
risk:
  factors: [data-migration, concurrency, trust-boundary, irreversible, broad-impact]
behavior: changed
reads: []
decisionsPending: false
---

## Why

策划文档（V6-079）和 3D 场景（epic `scene-editor-3d`）都是「画布节点背后挂一份可多次修改的独立文档」，都需要修订历史、按单元的并发控制、引用登记、回收站和画布接入。两者各写一套会重复、也会分叉，所以先把这部分做成通用文档底座，两个功能都在它上面注册自己的文档类型（owner 2026-10-16）。

## What Changes

- **存储**：
  - `creative_documents` 补 `created_at`、`updated_at`、`trashed_at`。
  - 新增修订表：每个修订一行，记录对应快照、写入者（摄影师 / 助手）、运行 ID、改动单元、恢复来源。
  - 新增引用表：每个修订引用了哪些内容修订。
  - 新增工作会话指针表：文档在某张画布上对应的助手会话。
  - 引用表和历史快照登记为 GC 持有方。
  - 带回滚链和迁移测试。
- **文档类型端口**：
  - 各类型在组合根注册：空白载荷、读取分支、命令应用（声明涉及单元、应用、返回引用）、按单元恢复、卡片摘要。
  - `creativecanvas` 不依赖任何具体类型。
  - 现有 `internal.document` 改为通过端口注册，作为第一个类型和验收载体。
- **通用读取**：`GET /creative/documents/{id}` 返回通用字段，再加上该类型的读取分支。`internal.document` 现有字段保持不变。
- **通用命令**：`POST /creative/documents/{id}/commands` 的外壳：
  - 请求由 `expected`（单元到修订号的映射）和 `operations` 组成。
  - 整批原子；按单元乐观并发，冲突返回 409 `creative_document_conflict`。
  - 通用操作 `rename`、`restore`、`restore_units`。整份恢复带 `expected_revision`，按文档修订号检查；预期值允许 null，表示预期该单元不存在。
  - 助手经同一领域入口写入，写入者记为助手和运行 ID。
- **修订历史**：修订列表（分页，可按运行 ID 过滤）；读取旧修订，可只取指定单元。
- **回收站与列表**：
  - 进回收站、恢复、彻底删除（只能删回收站里的，删除后挂它的节点变为不可用、引用释放）。
  - 文档列表，支持按类型、标题搜索、回收站筛选，游标分页。
- **画布接入**：
  - `add_node` 对已注册且允许新建的文档类型，在同一事务里建文档和第 1 个修订。
  - `add_node` 带 `document_id` 时放回已有文档；删除节点不删除文档。
  - 读取某张画布上的工作会话指针。

## Acceptance Criteria

### 新建与读取

- WHEN 以已注册且允许新建的文档类型调用画布 `add_node` THEN 同一事务里出现文档行（修订号 1）、第 1 个修订行、类型给出的空白快照和画布节点；任一步失败则全部不存在。
- WHEN 撤销这次 `add_node` THEN 只移除节点，文档仍可通过 `GET /creative/documents/{id}` 读到，也能用 `add_node` 带 `document_id` 放回。
- WHEN 以未注册的类型，或注册了但不允许新建的类型（如 `internal.document`）调用 `add_node` 新建 THEN 返回 400 `validation_failed`，不产生任何行。
- WHEN 读取 `internal.document` THEN 响应仍包含原有的 `id`、`title`、`type_key`、`content_revision_id`、`revision`、`content`，另加 `trashed_at`、`created_at`、`updated_at`；现有前端文档视图无需修改行为就能工作。
- WHEN 读取其他账号的文档 THEN 返回 404 `not_found`，与不存在的文档无法区分。

### 命令与并发

- WHEN 提交的 `expected` 中每个涉及单元的修订号都与当前一致 THEN 产生修订 N+1，被改单元的修订号设为 N+1，其余不变；修订行记录写入者、改动单元，助手写入时还记录运行 ID。
- WHEN 摄影师和助手几乎同时提交，改动**不同**单元 THEN 两者都成功，修订号连续（N+1、N+2），最终快照同时包含两处改动。
- WHEN 两方基于同一版本改**同一**单元 THEN 先到者成功，后到者返回 409 `creative_document_conflict`，`details.conflicts` 列出单元、预期修订号和当前修订号，并且不产生修订。
- WHEN 一批操作中任一操作非法，或同一单元出现两次，或缺少涉及单元的 `expected` THEN 返回 400 `validation_failed`，整批不生效。
- WHEN 带当前文档修订号作为 `expected_revision` 提交 `restore` 恢复到旧修订 k THEN 产生修订 N+1，快照等于 k 的内容，所有单元的修订号都变为 N+1，此前持有的旧修订号再提交都会冲突；修订行记录 `restored_from = k`。
- WHEN `restore` 或 `restore_units` 和其他操作放在同一批 THEN 返回 400。
- WHEN 摄影师基于修订 N 提交整份恢复，而在此期间助手只改了正文、标题没变 THEN 返回 409 `creative_document_conflict`（`unit` 为 `"*"`），当前内容不被覆盖。
- WHEN 用 `restore_units` 恢复一个当前已删除的单元，`expected` 中该单元写 null、集合单元写当前修订号 THEN 恢复成功；WHEN 该单元在此期间经历过「新增 → 删除」后再用原请求提交 THEN 因集合单元修订号变化而返回 409。
- WHEN 恢复的旧修订引用了一张展示授权已撤销的图片 THEN 恢复成功，读取时这张图标为不可用；WHEN 普通编辑新增这张图 THEN 返回 403 `creative_usage_denied`。
- WHEN `restore_units` 恢复部分单元 THEN 只有这些单元回到 k 的内容，其余单元的内容和修订号不变。
- WHEN 同一 `Idempotency-Key` 重放相同请求 THEN 返回原回执，不产生新修订；同一 key 用于不同请求体时返回 409 `idempotency_conflict`。
- WHEN 对一份含 2000 个单元的文档做整份恢复 THEN 提交成功，回执小于 64 KiB 且 `reset_required = true`，重放返回同一份回执。
- WHEN 类型在一次命令里新建内容（如场景提交模型）THEN 不出现「缺少持有方」错误；WHEN 新建内容之后的步骤失败 THEN 整体回滚，没有残留的内容和引用。

### 历史、回收站与 GC

- WHEN 列出修订并按 `run_id` 过滤 THEN 只返回该运行写入的修订，按修订号倒序，游标翻页稳定。
- WHEN 列出修订带 `since_revision = r` THEN 只返回修订号大于 r 的修订，每项带写入者、运行 ID 和改动单元（供助手了解自它上次写入后的变化）。
- WHEN 读取旧修订并指定 `units` THEN 只返回这些单元的内容。
- WHEN 文档在回收站 THEN 可以读，写命令返回 409 `creative_state_conflict`，默认列表不显示，加回收站筛选才显示；恢复后可以照常写。
- WHEN 彻底删除不在回收站的文档 THEN 返回 409 `creative_state_conflict`。
- WHEN 彻底删除成功但响应丢失，客户端用同一 `Idempotency-Key` 重试 THEN 返回同一份 200 回执，没有额外副作用。
- WHEN 彻底删除回收站中的文档 THEN 文档行、修订行、引用行、工作会话指针都删除，挂它的画布节点读取时显示为不可用；之后这些修订引用的内容不再因这份文档被 GC 保留。
- WHEN 某个内容修订只被一份文档的**历史**修订引用 THEN 媒体 GC 不回收它；守卫测试要求新表登记持有关系。

### 画布与会话

- WHEN 同一份文档被放到两张画布上 THEN 两个节点指向同一文档，任一处的修改在另一处读取时都能看到；删除其中一个节点不影响文档和另一个节点。
- WHEN 读取某文档在某张画布上的工作会话，而指向的会话已被删除 THEN 返回空，不报错。

## Impact

- **代码**：
  - 新包：`backend/internal/creativedocument`（通用层：存储、端口、命令外壳、历史、回收站）。
  - 改动：`backend/internal/creativecanvas`（`add_node` 建档与放回，`internal.document` 改为通过端口注册，节点不可用判定）、`backend/internal/creativereference`（持有方登记）、`backend/internal/platform/httpapi`（新路由）、`backend/internal/platform/store/migrations`（新迁移与回滚链）、组合根（类型注册）。
  - 前端只更新生成的类型，以及因响应 schema 改为按类型区分而需要的类型收窄，不做新界面。
- **契约**：
  - `GET /creative/documents/{id}`：增加字段，schema 改为按 `type_key` 区分。
  - 新增：`POST /creative/documents/{id}/commands`、`/trash`、`/untrash`、`POST /creative/documents/{id}/purge`、`GET /creative/documents`、`GET /creative/documents/{id}/revisions`、`GET /creative/documents/{id}/revisions/{revision}`、`GET /creative/documents/{id}/conversation`。
  - 画布 `add_node`：开放已注册的文档类型，新增 `document_id` 参数。
  - 运行 `make generate`，提交 Go / TS 生成物。
- **下游**：
  - V6-079 design（`.codestable/features/2026-10-15-shoot-plan-document/shoot-plan-document-design.md`）的 §3、§5.2、§5.4、§5.5、§9 中的通用部分改为复用本 change。该文件的修改先给 owner 过目，不在本 change 内直接改。
  - epic `scene-editor-3d` 的 C1 依赖本 change 归档。
- **外部**：无。

## Risk Notes

- **data-migration**：
  - 给已有表加列，新建三张表。迁移为每份存量文档回填一条写入者为 `system` 的修订（快照和标题取当前值），并设置单元修订号。
  - 回滚时，若存在 `system` 以外写入者的修订，或存在回收站中的文档，拒绝回滚，前向修复。
  - 迁移编号接在 `0068_agent_skill_runtime` 之后。V6-079 原计划用的 0068 已被占用，本 change 取下一个编号。
- **concurrency**：按单元的乐观并发和文档行锁是正确性的核心。验收要用真实 PostgreSQL 的并发测试证明：改不同单元都成功、改同一单元只有一方成功、修订号不重复不跳号。
- **trust-boundary**：新增写接口和列表接口，必须按账号限定（ADR-001）并检查现有能力 `creative_read` / `manual_write`（不新造能力名）；类型命令的载荷由类型端口校验，通用层校验外壳和大小上限。
- **irreversible**：彻底删除会不可恢复地删掉历史修订，所以只允许删除回收站中的文档，并且只有摄影师能操作（助手没有对应工具）。快照内容（非媒体类型）不被现有媒体 GC 物理回收，首版保留为无入口数据。确认文案属于前端，由使用方功能负责。
- **broad-impact**：GC 持有方条件 `creativereference.LiveOwnerCondition` 被媒体 GC、资产库保留检查等多处使用。改动必须让守卫测试和现有 GC 测试全部通过，只能扩大「被持有」的范围，不能缩小。
- **契约兼容**：`GET /creative/documents/{id}` 改为按类型区分的 schema 后，TS 生成类型从单一对象变为联合类型，现有前端（`DocumentView.tsx`、`WorkspacePage.tsx`）需要按 `type_key` 收窄。字段和语义不变，所以不标记 contract-break，但要在验收中确认前端编译和现有 e2e 通过。
- **范围外**：策划和场景的类型实现、编辑界面、助手工具、回收站的前端入口，都由各自的功能负责。
