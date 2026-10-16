# Tasks

## 1. 存储与 GC 持有方

- [ ] 1.1 迁移测试先行：在 `backend/internal/platform/store/creative_document_foundation_migration_test.go` 写回填断言（每份存量文档恰有一条 `system` 修订、文档头与修订行的 `unit_revs` 一致、`internal.document` 含 `body`）和拒绝回滚断言（存在非 `system` 修订或回收站文档时回滚失败）；新链头 `rollbackDocumentFoundation` 先回滚 0069 再调 `rollbackPlatformSkillOwner`；同步 `creative_graph_migration_test.go`、`creative_library_migration_test.go` 的标签列表。测试先红。
- [ ] 1.2 写 `0069_creative_document_foundation.up/down.sql`：`creative_documents` 补 `created_at`、`updated_at`、`trashed_at`、`unit_revs` 及两个列表索引；新建 `creative_document_revisions`（含 `title`、`annotated_units`、`unit_revs`、写入者 CHECK、`agent` 必带 `run_id`）、`creative_document_content_refs`、`creative_document_conversations`；回填与拒绝回滚逻辑。1.1 转绿，store 包全部迁移测试通过。
- [ ] 1.3 `creativereference.LiveOwnerCondition` 加修订表 `content_revision_id` 与引用表两行，`projection_roles_test.go` 登记持有角色；守卫测试与 `creativemedia` GC 测试全部通过，新增「只被历史修订引用的图片不被回收、文档删除后可回收」用例。

## 2. creativedocument 核心

- [ ] 2.1 新建包 `backend/internal/creativedocument`：`Type` 端口（含 `NodeTypeKey`、`RestoreTouched`、`Retain`、`Changes.Annotated`）、包级注册表（重复 key 或重复节点类型 panic、`RegisterForTest` 自动注销）、文档读写基础（读当前、读旧修订含 `unit_revs`）。单测覆盖注册冲突与映射查找。
- [ ] 2.2 并发与版本测试先行（真实 PostgreSQL，测试类型含集合单元、标注操作、可新建内容）：改不同单元都成功且修订号连续、改同一单元恰一方 409、`expected` 为 null 的存在性检查、「不存在 → 新增 → 删除」后旧请求冲突、标注不推进修订号；屏障并发各跑 20 次。测试先红。
- [ ] 2.3 实现 `Command`：外壳校验（单独成批规则、`expected` / `expected_revision` 互斥）、行锁、`Touched` / `RestoreTouched` 两阶段、版本检查、`Apply` 与 `Retain` 预登记、引用校验两条路径、写快照与修订行与引用行、文档头更新、有界回执（48 KiB 预算与 `reset_required`）。2.2 转绿。
- [ ] 2.4 恢复测试与实现：整份恢复按 `expected_revision` 检查（只改正文的并发写入被拒）、恢复后所有单元修订号为 N+1、`restore_units` 恢复已删除单元、带回失效授权的引用照常恢复、越界引用 400、`RestoreHook` 拿到旧修订 `unit_revs`；2000 单元文档整份恢复回执 < 64 KiB 且重放一致。
- [ ] 2.5 持有方交接测试与实现：类型 `Apply` 新建内容后报错 → 整体回滚无残留；新建内容未出现在最终快照 → 回滚；正常路径不出现 `ErrMissingRoot`。
- [ ] 2.6 历史、回收站、列表、会话：修订列表（`run_id`、`since_revision`、游标绑定条件）、读旧修订指定 `units`、`trash` / `untrash` / `purge`（只删回收站中的、同事务删四类行）、文档列表（`type`、`q` 转义、`trashed`、游标）、工作会话读取与 `SetConversation`；每项含跨账号 404 测试。
- [ ] 2.7 内置类型 `internal.document`（单元 `body`、操作 `set_body`、不可新建、`View` 保持原 `content` 字段、摘要为预览），并在 `backend/cmd/server/main.go`、`backend/cmd/creative-worker/main.go` 启动时注册；`document_fixture.go` 改为经 `creativedocument` 创建，原集成测试仍绿。

## 3. 画布接入

- [ ] 3.1 `creativecanvas` 测试先行：`add_node` 以注册节点类型新建文档（文档、修订、节点同事务，失败全无）、撤销只移除节点、`document_id` 放回（类型须匹配、回收站文档 409、与内容字段互斥 400）、不可新建类型 400；节点快照对文档节点返回 `document_summary` / `document_trashed`，已删除或未注册为 `unavailable`。测试先红。
- [ ] 3.2 实现：`graph_model.go` 的 `Action` 加 `DocumentID`、`DocumentOptions`；`graph_commands.go` 的 `editGraph.add` 经注册表映射节点类型并调 `creativedocument.CreateInTx` / `RequireReadable`；`definitions.go` 的 `NodeDefinitions` 合并注册表提供的文档节点定义；`node_snapshot.go`、`node_view.go` 改为对所有文档节点调 `SummaryBatch`。3.1 转绿，`creativecanvas` 全包测试通过。

## 4. HTTP、OpenAPI 与前端类型

- [ ] 4.1 `api/openapi.yaml`：`CreativeDocument` 改为按 `type_key` 判别的 `oneOf`（`internal.document` 保留原字段）、新增命令 / 回收站 / purge / 列表 / 修订 / 旧修订 / 会话接口与 `CreativeDocumentExpectedUnits`、`CreativeDocumentConflictDetails`（并入 `ErrorDetails`）、画布 `add_node` 新字段与节点快照字段；`make generate` 后 `make generate-check` 通过。
- [ ] 4.2 `backend/internal/platform/httpapi/creative_documents.go` 路由与错误映射（`manual_write`、`creative_document_conflict`、`creative_state_conflict`、`creative_usage_denied`、`creative_size_limit`），`GetCreativeDocument` 改调 `creativedocument.Read`；httpapi 测试覆盖每个接口的成功、失败、跨账号、幂等重放（含 purge 丢响应重试）与去掉 `manual_write` 后 403。
- [ ] 4.3 前端：`frontend/src/creative-canvas/editor/api.ts`、`DocumentView.tsx`、`WorkspacePage.tsx` 按 `type_key === 'internal.document'` 收窄，行为不变；`make check-frontend` 与现有文档相关 e2e 通过。

## 5. 收尾验证

- [ ] 5.1 逐条对照 proposal 验收与 specs 场景写验证记录 `.lti/features/2026-10-16-creative-document-foundation/verification.md`；`make check-go` 相关包、`make check-frontend`、`make generate-check` 结果如实记录；合回 `develop` 前跑 `make check`。
