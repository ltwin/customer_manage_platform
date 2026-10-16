# 验证记录：通用文档底座

日期：2026-10-16　分支：`feat/creative-document-foundation`（worktree `.worktrees/creative-document-foundation`）

## 门禁结果

| 检查 | 结果 |
|---|---|
| `make test-go`（后端全部包，`-p=4 -count=1`） | 通过，无失败 |
| `golangci-lint run`：`creativedocument`、`creativecanvas`、`platform/httpapi`、`creativereference`、`platform/store` | 0 issues |
| `make generate-check` | 通过，生成物无漂移 |
| `make check-frontend`（oxlint、`tsc -b` + vite build、521 个 node 测试） | 通过 |
| Playwright e2e（`frontend/scripts/*.e2e.mjs`） | **未跑**：需要运行中的前端和一对真实账号，只能手动跑 |
| `make check`（合回 `develop` 前的门禁） | 未跑，合并前执行 |

测试顺序说明：2.x 的测试写在实现之后，没有经过先红阶段。补做了变异检查：把 `command.go` 里 7 处关键判断（null 预期、恢复引用只查历史、`AfterRestore` 读旧修订、整份恢复的修订号比较、回执预算、预登记引用必须在快照里、类型诚实性）逐个改坏，对应测试都失败；类型诚实性一项原先没有覆盖，已补测试。3.1、4.2 的测试先红后绿。

## 验收对照

测试文件简称：`doc` = `backend/internal/creativedocument/document_test.go`，`internal` = 同目录 `internal_type_test.go`，`canvas` = `backend/internal/creativecanvas/document_node_test.go`，`http` = `backend/internal/platform/httpapi/creative_documents_test.go`，`migration` = `backend/internal/platform/store/creative_document_foundation_migration_test.go`，`holder` = `backend/internal/creativereference/document_holder_test.go`。

### 新建与读取

| 验收 | 证据 | 结论 |
|---|---|---|
| `add_node` 以可新建类型同事务建文档、修订、节点；任一步失败全无 | `canvas` `TestDocumentNodes`：新建后文档修订号 1、修订行 1 条、节点挂 `document_id`；「later action fails」「bad options」子测试文档数不变 | 满足 |
| 撤销只移除节点，文档可读、可放回 | `canvas` `TestDocumentNodes`：撤销后节点 0 个、`Read` 成功、`document_id` 放回成功 | 满足 |
| 未注册或不可新建的类型 400，不产生行 | `canvas` 子测试「type not creatable」「type not registered」：`ErrValidation`，文档数不变 | 满足 |
| `internal.document` 读取保留原字段并加新字段，前端无需改行为 | `http` 逐个检查 `id`、`title`、`type_key`、`content_revision_id`、`revision`、`content`、`unit_revs`、`trashed_at`、`created_at`、`updated_at`；`internal` `TestInternalDocument`；前端 `tsc -b` 通过 | 满足（e2e 未跑） |
| 其他账号读取 404 | `doc` `TestCreateAndRead`；`http`「read another account's」 | 满足 |

### 命令与并发

| 验收 | 证据 | 结论 |
|---|---|---|
| expected 一致时产生 N+1，被改单元 N+1，修订行记写入者、单元、运行 ID | `doc` `TestExpectedExistence`、`TestHistory`（助手写入带 `run_id`）；迁移 CHECK 保证 agent 必带 `run_id`（`migration`） | 满足 |
| 改不同单元两者都成功，修订号连续 | `doc` `TestWritesToDifferentUnitsBothLand`，屏障并发 20 轮 | 满足 |
| 改同一单元恰一方成功，另一方 409 带冲突明细，不产生修订 | `doc` `TestWritesToSameUnitExactlyOneWins` 20 轮；`http`「stale」检查错误码与 `details.conflicts` | 满足 |
| 非法操作、单元重复、缺少 expected → 400 整批不生效 | `doc` `TestExpectedExistence` 子测试；`internal`「twice」「empty body」 | 满足 |
| 整份恢复：所有单元 N+1，旧修订号再提交冲突，记 `restored_from` | `doc` `TestWholeRestore` | 满足 |
| `restore` / `restore_units` 与其他操作同批 → 400 | `doc` `TestWholeRestore`「batched」、`TestRestoreUnits`「batched」 | 满足 |
| 只改正文的并发写入让整份恢复 409（`*`） | `doc` `TestWholeRestore` 第一段 | 满足 |
| `restore_units` 恢复已删除单元；新增→删除后原请求因集合单元冲突 | `doc` `TestRestoreUnits`、`TestExpectedExistence`（stale add 只在 `items` 冲突） | 满足 |
| 带回授权已撤销的引用照常恢复；普通编辑新增它 403 | `doc` `TestRestoreReferences`（`restore_units` 和整份恢复都成功，`add_ref` 返回 `ErrUsageDenied`） | 底座部分满足；「读取时标为不可用」由各类型的 `View` 负责，本 change 只有 `internal.document`，它不写引用 |
| `restore_units` 只动指定单元 | `doc` `TestRestoreUnits`（`item:b` 修订号保持 2） | 满足 |
| 幂等重放返回原回执；同 key 不同请求 409 | `http` 命令重放、「same key, other payload」；`doc` `TestReceiptStaysBounded` 重放 | 满足 |
| 2000 单元整份恢复回执 < 64 KiB、`reset_required`、重放一致 | `doc` `TestReceiptStaysBounded`；长 ID 批量删除触发 `reset_required` | 满足 |
| 类型在命令里新建内容不报缺少持有方；之后失败整体回滚无残留 | `doc` `TestContentCreatedInsideWrite`（`fail_after_create`、`create_orphan` 后内容修订数和引用行不变；`create_ref` 成功） | 满足 |

### 历史、回收站与 GC

| 验收 | 证据 | 结论 |
|---|---|---|
| 删除单元在历史中可见（审计 P2） | `doc` `TestExpectedExistence`、`TestRestoreUnits`：修订列表和读旧修订的 `removed_units`；`http` 检查字段存在 | 满足 |
| 摘要批量读取查询数不随文档数增长（审计 P2） | `doc` `TestSummariesReadReferencesOnce`：1、5、20 份文档引用表扫描都为 1 次，摘要里的引用数正确；`List` 走同一个 `summarize` | 满足（`List` 自开事务，没法同事务计数，靠共用实现保证） |
| 恢复没有历史的旧修订（审计 P3） | `doc` `TestRestoreWithoutHistory`：两种恢复返回 400，读旧修订返回 `ErrRevisionNotFound`；`http`「修订不存在」 | 满足 |
| 按 `run_id` 过滤、倒序、游标稳定 | `doc` `TestHistory`（分页 [5,4]/[3,2]/[1]，换条件用旧游标 400） | 满足 |
| `since_revision` 只返回之后的修订，带写入者、运行 ID、改动单元 | `doc` `TestHistory`；`http`「since 1」 | 满足 |
| 读旧修订指定 `units` 只返回这些单元 | `doc` `TestHistory`、`internal`、`http`「old revision」 | 满足 |
| 回收站中可读不可写（409），默认列表不显示，恢复后可写 | `doc` `TestTrashAndPurge`、`TestList`；`http` trash 段 | 满足 |
| 彻底删除不在回收站的文档 409 | `doc` `TestTrashAndPurge`；`http`「purge before trash」 | 满足 |
| 彻底删除丢响应重试返回同一回执 | `doc` `TestTrashAndPurge`；`http` purge 重放 | 满足 |
| 彻底删除清掉四类行，节点显示不可用，引用不再被保留 | `doc` `TestTrashAndPurge`、`TestConversation`（四张表计数为 0）；`canvas`（purge 后 `content_state=unavailable`）；`holder`（删行后不再持有） | 满足 |
| 只被历史修订引用的内容不被 GC 回收；守卫测试登记新表 | `holder`；`projection_roles_test.go` 登记两列；`make test-go` 含 `creativemedia` GC 测试 | 满足 |

### 画布与会话

| 验收 | 证据 | 结论 |
|---|---|---|
| 同一文档放两张画布，修改两处可见，删一个节点不影响另一个和文档 | `canvas` `TestDocumentOnTwoCanvases` | 满足 |
| 会话指针指向已删除会话时返回空 | `doc` `TestConversation` | 满足 |

### 迁移

| 验收 | 证据 | 结论 |
|---|---|---|
| 存量文档回填一条 system 修订，`unit_revs` 一致，`internal.document` 含 `body` | `migration` 回填测试 | 满足 |
| 有非 system 修订或回收站文档时拒绝回滚 | `migration` 拒绝回滚测试（每种情况各用一个库） | 满足 |

## 实施偏差

见 `design.md`「实施记录（2026-10-16）」和「实施审计处置」。需要 owner 确认的契约补充：修订列表项加 `annotated_units`、`removed_units`，读旧修订加 `annotated_units`、`removed_units` 和 `unit_revs`（只加字段，不改已有字段）；读旧修订的 404 区分「文档不存在」和「修订不存在」，恢复没有历史的修订返回 400。审计处置后重跑 `make test-go`、lint、`make generate-check`、`make check-frontend`，全部通过。
