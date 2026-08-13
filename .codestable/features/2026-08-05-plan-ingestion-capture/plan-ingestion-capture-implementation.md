---
doc_type: feature-implementation
feature: 2026-08-05-plan-ingestion-capture
status: in-progress
execution_lane: goal
current_step: S8
updated: 2026-08-06
---

# plan-ingestion-capture 实现记录

## 基线

- 工作区：`.worktrees/creative-shoot-planning`，分支 `feat/creative-shoot-planning`；
- 依赖 `shoot-plan-core`、`planning-reference-assets` 均为 `done`；
- 基线预检：`cd backend && go test -p=1 ./internal/shootplanning/... ./internal/planningmedia ./internal/platform/idempotency ./internal/platform/httpapi -count=1 -parallel=1` 通过；
- 本 feature 的 review 上限独立为 3 轮；当前尚未进入代码 review。

## S1 — parser v1 与候选 reconciliation

- 退出信号：golden 覆盖 parser/candidate ID、URL、speaker、blank、readiness cue、duplicate、reparse、reference source-change 与 0/1/N readiness link provenance；同输入 canonical JSON byte-stable。
- TDD：以 URL/speaker/readiness、时间/协议、请求/候选上限、LCS reparse、duplicate provenance、reference URL 变化、readiness link 0/1/N 与外置 golden corpus形成 RED/GREEN/VERIFY 微循环。
- 主要交付：
  - `backend/internal/shootplanning/ingestion/parser.go`：纯 parser、frame_v1/hash、closed candidate schema、limits、显式 readiness link builder；
  - `backend/internal/shootplanning/ingestion/reparse.go`：固定 tie-break LCS、equal-gap 配对、source_missing/reappear、reference 独立对齐、link 失效确认；
  - `backend/internal/shootplanning/ingestion/testdata/parser_v1_corpus.json` 与测试文件：版本化 golden corpus。
- 验证：`cd backend && go test ./internal/shootplanning/ingestion -count=1`、`go vet ./internal/shootplanning/ingestion` 通过。
- 清洁度：无网络客户端、AI/provider/OCR/crawler 依赖；无调试输出、TODO/FIXME、原文日志或 placeholder handler。

## S2 — session/reference/observation 持久层

- 退出信号：PostgreSQL partial unique 保证每 plan 一份 editing session；repository 支持 account scope、session CAS、terminal lifecycle、30 日 source redaction；observation 共用 server-time accumulator，replay tick 不累计，连续 301 秒只接受 300 秒。
- 主要交付：
  - `backend/internal/platform/store/migrations/0018_plan_ingestion.up.sql` / `.down.sql`：session、reference link、observation、activity tick 及幂等 operation allowlist；
  - `backend/internal/shootplanning/ingestion/repository.go`：session create/get/lock/preview/transition/redaction；
  - `backend/internal/shootplanning/ingestion/observation.go`：deduplicated tick、300 秒 cap、first-ready/abandon terminal、post-terminal count；
  - `repository_integration_test.go`：真实 PostgreSQL 的账号隔离、唯一 editing、CAS 与 fake clock 证据。
- 验证：`cd backend && go test ./internal/shootplanning/ingestion -run TestSessionRevisionScopeAndObservationClock -count=1 -parallel=1` 通过；`go test ./internal/platform/store -run TestShootPlanningCoreMigrationSeedIsStableAndDownIsComplete -count=1 -parallel=1` 通过。
- 锁序边界：repository 只通过 `AccountScope`/`TxAccountScope`，不暴露裸 pgx；S3 combined path 将按 session→plan→asset/reference 固定顺序编排。

## S3 — application、幂等与 HTTP seam

- 退出信号：create/get/preview/transition 可恢复；create 并发唯一会话与同 key replay 返回原 session；preview/transition 使用 expected session revision，terminal session 只读；OpenAPI 与路由错误矩阵已落盘。
- 主要交付：
  - `backend/internal/shootplanning/ingestion/application.go`：create/preview/transition application，解析与 reparse 在 session CAS 内执行；
  - `backend/internal/platform/httpapi/planning_ingestion.go`：Bearer 保护的会话 API 路由；
  - `backend/internal/platform/idempotency/idempotency.go`：create/preview/transition/commit operation 与 resource identity；
  - `api/openapi.yaml`、`frontend/src/api/schema.d.ts`、`backend/oapi-codegen.yaml` 生成契约同步；
  - `router.go` / `cmd/server/main.go`：application composition 与 route registration。
- 验证：`cd backend && go test ./internal/shootplanning/ingestion -run TestApplicationCreatePreviewReplayAndAbandon -count=1 -parallel=1` 通过；HTTP、server、shootplanning contract 包编译通过；`make generate` 完成。
- 明确边界：S3 只管理 editing session 和候选预览；尚未写入 Shot/Readiness/PlanReferenceLink/media binding，下一步 S4 只落 pure commit preparation 与 typed reference target seam。

## S4 — commit preparation 与 reference target capability

- 退出信号：`IngestionCommitCanonicalV1` 逐 candidate 校验 keep/discard、unknown/duplicate 拒绝；core kept 分支生成 upstream `PreparedPlanBatch`；core 0 candidate + reference-only 保持 `Core=nil`；reference URL/scheme/target 独立校验。
- 主要交付：
  - `backend/internal/shootplanning/ingestion/commit.go`：v1 canonical/prepare、shot/readiness/link 映射、reference-only 分支、`PlanTargetProof`、`PlanReferenceLink` participant；
  - `commit_test.go`：core+reference、all-discard、reference-only、ftp negative fixture。
- capability 隔离：`PlanTargetProof` 只带 `reference_link_create` operation 与 plan/target proof，不接受或转换 planningmedia `HolderProof`；后续 combined commit 必须再次在 tx 内签发。
- 验证：`cd backend && go test ./internal/shootplanning/ingestion -run TestPrepareIngestionCommitCoreAndReferenceBranches -count=1` 通过。
- 下一步 S5：在同一 AccountScope 事务里接 core prepared batch、planningmedia prepared binding、reference participant、observation sink 与幂等 ledger，先覆盖全成/全败/response replay。

## S5 — combined commit

- 退出信号：session→core→reference proof→media binding→observation→session terminal 只在一条 AccountScope transaction 内完成；core/reference failure 全回滚；same idempotency key replay 不再次执行 callback。
- 主要交付：
  - `application.go`：`Commit` 统一编排 kept core、reference-only、prepared media、activity tick 与 session commit；
  - `planningmedia/application.go`：`BindPreparedAssetsInScope` 狭窄 post-core binding seam，重新执行 HolderAuthorizer；
  - `application_integration_test.go`：reference-only exact replay、core batch、invalid reference target 导致 core rollback 的真实 PostgreSQL 证据。
- 锁序：combined path 先锁 ingestion session，再由 core/reference/media 各自沿既有 owner seam 取得 plan/asset 锁；不把 media `HolderProof` 转成 `PlanTargetProof`。
- 验证：`cd backend && go test ./internal/shootplanning/ingestion -run TestCombinedCommitReferenceOnlyAndCoreRollbackBoundary -count=1 -parallel=1` 通过。
- 待 acceptance 补齐：真实 staged media binding fault injection、object/DB 双失败、跨账号/cross-plan asset、response-loss 并发 callback=0 矩阵；这些属于 S8 evidence，不在当前实现步伐中静默替代。

## S6 — ready observation sink 与 stage-1 evidence projector

- core 在 `shootplanning` parent 定义 `PlanReadyObservationSink` 窄 transaction interface；默认使用显式 Noop，但生产 composition 在摄取路由启用时强制要求真实 sink，Noop wiring 会 fail closed。
- ingestion `PlanReadyObservationAdapter` 只把 plan-ready fact 转成 observation accumulator；无 observation 是正常 0-row no-op。ready/abandon 使用稳定 tick id，复用 server-time 300 秒 cap，幂等 replay 不再次执行 callback 或 active delta。
- create/有实质变化的 preview/abandon 现在写服务端 activity tick；preview 校验 expected session revision，并对 same-body canonical snapshot 做无 revision/no-tick no-op。
- `evidence.go` 提供 `ProjectStage1Evidence` 与 `GenerateStage1EvidenceFromStore`：按 owner-reviewed sample decisions、live RunModeSession 与 PlanBuildObservation 生成 `planning-evidence-stage1-v1` canonical JSON、G1 distinct live plan、G2 successful active median、abandon ratio、排除理由、rule versions、build revision 与 stable SHA-256；报告只带事实 status，不修改 `approval-report.md` 或任何 gate。
- 测试：固定五样本 fixture 验证 G1=3/5、G2 median=540 秒、abandon ratio=0.2 与 hash 对输入顺序稳定；排除样本保持 `insufficient`；PG integration 验证 abandon/ready terminal final delta 与同 key replay 不累计；route-enabled + Noop sink composition negative test 通过。
- 验证：`go test ./internal/shootplanning/ingestion -run 'TestProjectStage1|TestApplicationCreatePreviewReplayAndAbandon|TestCombinedCommitReferenceOnlyAndCoreRollbackBoundary|TestSessionRevisionScopeAndObservationClock' -count=1 -parallel=1`、`go vet ./internal/shootplanning/... ./cmd/server/...` 通过。

## S7 — 三步摄取页与最小闭环

- `frontend/src/planning/ShootPlanIngestionPage.tsx` 接入 `/shoot-plans/:id/ingestions/:sessionId`，`new` 路由先加载策划并创建会话，成功后 replace 到真实 session URL；支持刷新恢复 editing/terminal session。
- 左栏粘贴原文与参考图上传/暂存，右栏按 Shot/Readiness/ReferenceLink/Dropped 展示候选；候选可编辑标题、改类、合并、丢弃/恢复；准备项链接只按用户显式选择 0..N 镜头；参考链接只保存 URL 本身。
- commit summary 显示核心候选、参考链接、素材绑定数量；同一 Idempotency-Key 调用原子 commit，失败保留当前编辑和错误提示；成功后回到策划工作台。样式覆盖 1600/1280/768/430、键盘 focus-visible、粗指针与窄屏布局。
- `api/openapi.yaml` 将 ingestion snapshot/decision/result 收紧为可生成的契约类型，`frontend/src/planning/api.ts` 仅引用生成 schema；新增 `test:plan-ingestion` contract test，未引入 AI/provider/OCR/crawler 或客户端计时。
- 验证：`make generate-check`、`npm run build`、`npm run lint`、`npm run test:plan-ingestion`、`npm run test:shoot-planning` 通过。

## S8 — 实现审计与真实最小闭环

- 修复 `repository.go` 的跨层 `pgx` import 与幂等资源 gofmt；补齐新增 0014 migration 后旧 avatar/Telegram migration up/down 测试的回滚序列；
- 修复真实浏览器首次暴露的空候选数组 `null` 崩溃：后端 snapshot decode/encode 与前端 session/commit response 均保持数组契约，并加入空集合单测；
- `make generate-check`、目标后端包回归、ingestion focused tests、前端 ingestion/shoot-planning tests、build/lint 与全仓 `make check` 均通过；
- scope gate、DoD runner、evidence pack 均在 `implementation.before_review` 通过，机器结果见同目录 `*-gate-results.json`、`*-dod-results.json`、`*-evidence-pack-results.json`；
- 本地真实 API 完成 login → plan → ingestion session → atomic commit → ready → Run Mode；浏览器完成粘贴 → 解析候选 → 保存摘要 → 确认保存，并检查 1280/430 断点与窄视口无横向溢出；
- 详细审计与 residual risks：`plan-ingestion-capture-s8-audit.md`。source-change ack contract、真实 staged media 双故障和 stage-1 owner gate 保持明确后置风险，不伪造为已闭合。

下一步：进入本 feature 独立 code review；按照 owner 约束 review 总次数不超过 3 轮。
