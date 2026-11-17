# Agent 后台工具与自动唤醒：实施与验收证据

工作区：`feat/agent-tool-background`。项目已有批准的 proposal、design 和契约，验收沿用该版本。

| 检查 | 实现与证据 |
| --- | --- |
| 前台停靠、结果和停止边界 | `creativeagent/tool_tasks.go`、`tool_task_finisher.go`、`waiting.go`。`TestForegroundGenerationParksUntilItsExecutionConcludes` 用持久化任务模拟长生成和等待采用；`TestStoppedRunsGenerationTaskStillRecordsItsAnswer`、`TestCancelledGenerationIsCancelledForTheModel`、`TestRerunAfterACrashParksOnTheSameJobs`、`TestSceneToolTaskEndsWithItsJob` 验证停止、取消、重放和场景原事务。 |
| 后台受理与控制 | `TestGenerationInBackgroundOutlivesItsRun`、`TestBackgroundArgument`、`TestRenderViewIsForegroundOnly` 验证受理、工具参数和页面截图限制；`TestDetachContinuesTheRun`、`TestDetachAfterTheSourceFinished`、`TestStopAndDetach` 和 `TestCancelBackgroundGeneration` 验证完成与转后台顺序、重复请求和生成取消。既有 `search_web` 的超时路径未改。 |
| 通知、派发与自动继续 | `TestBackgroundResultBecomesOneNotice`、`TestExpiredBackgroundSceneIsNoticed`、`TestNoticeOfADeletedConversationIsSuppressed` 验证终态通知及历史卡片；`TestNoticeArrivingMidRunIsAnsweredByTheRun`、`TestNextRunCarriesThePendingNotice`、`TestRefusedDispatchLeavesTheNoticePending` 用网关请求与数据库送达记录核对。 |
| 唤醒、并发与计费准入 | `TestWakeTimeOfAGroup` 使用指定完成时间验证 2 秒静默、10 秒上限；`TestDueNoticeOpensAnAutomaticTurn` 验证真实建运行与重复扫描；`TestAutomaticTurnsKeepTheirOwnNoticeConfiguration` 验证不同模型依次开轮，第一轮不提前送达另一组；`TestAutomaticTurnsStopAfterFiveInARow` 验证连锁上限和摄影师输入重置；`TestNoticesThatDoNotWake` 验证已停止（含 partial）、占用、撤回同意和回退开关。积分、模型与 Skill 沿用 `openWakeRunInTx` 的准入及事务回滚。 |
| API 契约与客户端兼容 | `TestCreativeAgentToolTaskRoutesAndProjections` 用真实 router + PostgreSQL 验证两个路由、跨账号 404、回执重放、换句柄冲突、任务快照和 SSE；新增字段为可选，事件信封仍为版本 1。 |
| 浏览器 | `agent-queue.e2e.mjs` 验证转后台即时反馈、拒绝恢复、408 后回读、历史卡片更新、刷新、跨对话隔离、生成取消扣费提示、通知在自动轮之前，以及队首输给自动轮后保留顺序和草稿。 `agent-stop.e2e.mjs` 验证停止；`creative-agent-blocks.e2e.mjs` 验证 SSE 断线、重连和快照恢复。浏览器使用模拟 API，HTTP、任务与计费事务另用真实 PostgreSQL 集成验证。 |

与挂载点的差异：运行快照投影落在 `run_stream.go`（设计列了 `run_read.go`）；前端对话窗口的系统消息保留在 `agentThread.ts`，控制请求的超时沿用 `api.ts` 的请求层。其余附加文件主要是测试和存储扫描。工具注册、生成与压缩的受理实现沿各自的工具入口，未另改设计列出的 service 入口。数据库迁移使用原有 `0084_creative_graph_identity_created_at`（从 develop 同步）后接 `0085_creative_agent_tool_tasks`；全链路反向走查包含两步。完成通知数据有反向迁移保护，操作方式见开发文档。

本地浏览器使用真实前端代码与可控响应；没有调用真实生成供应商，也没有部署到生产。摄影师的实际使用验收由 owner 完成。

- 迁移：`TestCreativeAgentToolTasksMigration` 在真实 PostgreSQL 验证源表触发、旧源状态、账号隔离、空库可回退和有任务数据时拒绝回退。旧库保持原场景任务路径；新任务存在时使用支持任务表的 worker 排空，不能用旧二进制接手未完任务。未运行生产迁移。
- 信任边界：`registerCreativeAgent` 走既有认证路由，`creativeWriteTargets` 验证信封和幂等头，`bindToolTaskCommand` 拒绝非空公开 payload、验证句柄并将其纳入幂等指纹。任务查找经 `TxAccountScope`；转后台复查运行执行权和 `agent_start`，取消复查 `manual_write` 与项目写保护。本地 HTTP 用例分别覆盖 401、跨账号 404、非法句柄 400 和任务冲突 409。
- 并发边界：源行更新后才标记任务；收尾器发布实时事件前按项目、来源运行、源行、任务行取锁，来源运行日志已清理时仍可独立收尾。相同操作号由 creativeops 锁定；通知与任务终态同事务写入，唤醒与手动发送共用会话槽。重跑命令：`cd backend && go test ./internal/creativeagent -run 'Test(StopAndDetach|DetachAfterTheSourceFinished|CancelBackgroundGeneration|ToolTaskFinishersDoNotSettleOneTaskTwice|AutomaticTurnsKeepTheirOwnNoticeConfiguration)$' -count=3`。

最终本机门禁：`make generate-check`、`make check-frontend`（1121 个测试）、`make check-go GOTEST_P=2` 全部通过。此前四并行的 PostgreSQL 容器启动超时，减少同时启动的测试进程后原失败包在全量中通过；检查范围未减。独立存储包完整回归通过，HTTP 与并发指定用例连续三轮通过。浏览器队列与后台任务 19 项、停止 10 项、运行事件流场景均通过。

## 审计回归复验

- 停止先于采用时保留原图。`TestStopWinsBetweenCompressionReadinessAndAdoption` 把停止插在收尾读到 ready 与采用之间，`TestAgentCompressStoppedLeavesTheNodeAlone` 在 PostgreSQL 验证拒绝采用后候选丢弃、图片修订保持原值。
- 快速压缩转后台发一条通知。`TestFastCompressDetachedBeforeAdoptionPublishesNotice` 在同步采用前转后台，核对工具应答、完成通知和实际送达。
- 失败的 `partial` 不主动开轮。`TestNoticeFromPartialFailureWaitsForPhotographer` 核对 `source_failed` 和摄影师手动运行送达。
- 等锁时通知已送达则不重复开轮。`TestWakeRechecksNoticeAfterWaitingForProject` 用 PostgreSQL 锁等待验证无额外运行和积分预留。
- `make check-go GOTEST_P=1 PKG='./internal/creativeagent/... ./internal/creativeimageedit/...'` 通过全仓 Go 构建、限定包 lint 和测试。完整测试曾触发一处并行工具调用的顺序假设，改为按节点核对后重跑通过。
