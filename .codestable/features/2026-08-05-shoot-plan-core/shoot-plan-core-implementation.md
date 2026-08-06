---
doc_type: feature-implementation
feature: 2026-08-05-shoot-plan-core
status: passed
updated: 2026-08-06
---

# shoot-plan-core 实现证据

## 基线与第一性原则 pre-pass

- Workspace：独立 worktree `.worktrees/creative-shoot-planning`；branch `feat/creative-shoot-planning`；Goal baseline `64e1105c0e08b474851431fa80f2ec3d595a85a5`。
- 外部行为：摄影师可在不关联客户、订单或档期的情况下创建 ShootPlan，维护 brief、Shot、Readiness、公开规模和手工执行时间窗，并在独立 Run Mode 中记录、撤销和完成现场执行。
- 不可破约束：所有业务数据经 AccountScope；Plan revision 与 execution fact revision 分离；执行事实 append-only；请求不接收 `account_id` / `capture_mode`；OpenAPI 生成类型是双端唯一 DTO 来源；archive capability 与 reminder fence 必须 fail closed。
- 最小充分改动：独立 `shootplanning` 深模块、一次 migration、OpenAPI/生成物、薄 HTTP adapter、release-only planningctl、策划工作台与独立 Run Mode，以及对应 PG/API/browser/静态负向证据。
- 必须不写：CRM 强关联、媒体/摄取、匿名分享、反馈/认领、经营定价、AI/provider/知识库、客户端账号作用域、客户端 capture mode、placeholder route。
- 方案深度：ShootPlan 是长期维护的核心业务资产且含数据/并发/审计正确性要求，因此采用真实 PostgreSQL、真实 HTTP、真实浏览器和 append-only replay；仅后置跨 feature participant 使用 sealed fake probe，并写明由后续 feature 转正。

## S1 — 契约与领域骨架

- 退出信号：双端 codegen 零漂移；closed union、状态机和 E1/E2/E3 replay truth table 通过；未注册 placeholder route。
- 影响面：`api/openapi.yaml`；`backend/internal/shootplanning/model.go:195` 的 `PlanCommand`；`state_machine.go:28` 的 `TransitionPlanState`；`replay.go:14` 的 `ReplayCurrentOutcome`；生成的 `httpcontract/api.gen.go` 和 `frontend/src/api/schema.d.ts`。
- TDD：RED 先固定 command/transition canonical shape、nullable patch 与 event JSON；GREEN 落领域类型、状态机和纯 replay；VERIFY 为 `command_engine_test.go`、`state_machine_test.go`、`replay_test.go` 与 CMD-001/002。
- 清洁度：generated interface 的生产 adapter compile assertion 位于 `httpapi/shoot_planning.go:29`；S1 临时 Gin compile harness 在真实 adapter 落地后已删除。
- 结果：通过。

## S2 — 账号隔离与 neutral 基础持久化

- 退出信号：真实 PG 覆盖 create/list/detail、隔离、CAS、稳定分页、软移出、capability bootstrap/promotion/shared-lock 与 neutral generation/fence；migration up/down 与 reciprocal FK fixture 通过。
- 影响面：`0012_shoot_planning_core.up/down.sql`；`repository.go:71` 的 `PostgresRepository`、`:142` 的固定两查询 List、`:194` 的 Detail；`platform/store/planning_capability.go`、`planning_reminder_fence.go`、`planning_share_migration_contract_test.go`；`platform/planningcapability` 与 `platform/txcap`。
- TDD：RED 先落真实容器 fixture；GREEN 依次实现 account-scoped schema/repository、deployment singleton capability、sealed transaction reader/promoter 和 generation fence；VERIFY 为 repository/store/planning migration/双连接测试。
- 查询上界：List 固定一次 `Count` + 一次 `QueryPage`，循环只扫描当前 rows，不按 item 加载 Shot/Readiness；真实 PG stable-list fixture 验证分页与 tie-break。Detail 对单 plan 使用固定查询序列，不随集合大小 fan-out。
- 清洁度：业务包不直接 import pgx；`planReadScope` 使用 `store.Row/Rows` 边界类型。
- 结果：通过。

## S3 — 聚合命令与生命周期

- 退出信号：每个 command/transition variant 接通；三个 executor 入口共享唯一 ledger 算法和 physical transaction；remove readiness guard、archive acknowledgement/capability 与 reminder participant rollback 通过。
- 影响面：`application.go:122` 的 `Application`、`:131` 的 `NewApplication`、`:192` 的 `CreatePlan`；`batch.go:191` 的 `CommitPlanBatch`、`:222` 的 `CommitPreparedPlanBatchInScope`；`command_engine.go` 的结构命令节点；`platform/idempotency` 的 typed frame 与 capability execution。
- TDD：RED 覆盖 patch presence、noop、batch、guard/policy/wiring 与三断点回滚；GREEN 实现 command engine、prepared-in-scope port、single-ledger orchestration；VERIFY 为 command/unit、repository integration 和 idempotency capability probe。
- S8 窄修复：事务 probe 不再自行建立 pgxpool/临时表，而以现有 account-scoped readiness projection 证明失败全回滚、成功只执行一次和 replay callback=0。
- 结果：通过。

## S4 — 执行历史与完成快照

- 退出信号：session/captured/skipped/cleared/void/server seq/current projection/finalization/reopen 可重放；并发旧 revision 只有一个成功；旧 snapshot 保留。
- 影响面：`execution.go:50` 的 `OpenRunSession`、`:183` 的 `AppendShotResult`、`:360` 的 `VoidExecutionEvent`、`:568` 的 `completePlanInScope`；`replay.go:14` 的纯重放。
- TDD：RED 固定 captured→cleared、recapture、void truth table、并发 capture/complete 和 reopen/re-complete；GREEN 只追加事实并由 replay 派生 current outcome；VERIFY 为 replay 与真实 PG 并发 fixture。
- 结果：通过。

## S5 — HTTP 与 composition harden

- 退出信号：全部 route 进入指定 application method；Bearer/AccountScope、严格 JSON、400/401/404/409/500、跨账号与 schema negatives 通过；无空 2xx/501/panic/placeholder。
- 影响面：`httpapi/shoot_planning.go:31`～`:186` 八个 handler、`:374` 的 command decoder、`:534` 的 transition decoder、`:721` 的 route registration；`cmd/server/main.go:177` 的 composition；`router.go`、统一 envelope 和 OpenAPI route。
- TDD：RED 先令未注册 route、缺 key、未知/混合字段、`account_id`、`capture_mode` 与错误 effects 被拒绝；GREEN 在真实 adapter 注册 route；VERIFY 为 `TestShootPlanningHTTPVerticalSlice`、server composition fail-closed 与 route/application 覆盖。
- 结果：通过。

## S6 — 策划台账与工作台

- 退出信号：真实 API 支持台账、brief、Shot、Readiness、公开规模、时间窗和历史；empty/loading/error/stale、冲突恢复、确认 dialog 与 1600/1280/375 可观察；没有后置假入口。
- 影响面：`ShootPlansPage.tsx:24`、`ShootPlanWorkspacePage.tsx:36`、四个 panel、`planning/api.ts:25`～`:115`、`presentation.ts`、`StatusBadge.tsx` 与 `planning.css`。
- TDD：RED/GREEN 专项源码 contract 固定 generated types、路由、dirty draft、幂等 key、四个 core 分区和 forbidden surface；真实浏览器验证 create/edit/conflict/retry/Shot/Readiness/archive dialog 与响应式布局。
- 浏览器证据：`evidence/s6-ledger-empty-1600.png`、`s6-workspace-readiness-1280.png`、`s6-workspace-readiness-375.png`。浏览器还发现 `readiness_item_ids: null` 契约违例；先加 HTTP RED，再在 repository 规范化为空数组，相关测试 GREEN。
- 结果：通过。

## S7 — 独立 Run Mode

- 退出信号：登录保护但脱离 AppShell；375px/coarse pointer/200% zoom/高对比下逐镜可执行；目标至少 44px；未收 2xx 不显示已保存；DOM 无结构编辑。
- 影响面：`ShootPlanRunPage.tsx:37`、`runState.ts:3`、`run.css`；`planning/api.ts:77` 的 open 与 `:89` 的 append；`App.tsx:55` 的独立受保护 route。
- TDD：RED/GREEN 固定 success-only state update、same-key retry、execution-only DOM、coarse/focus/light contracts；真实断网流程证明失败时保持 `0/1 + 待执行`，恢复后同动作 retry 仅在 2xx 后变为 `1/1 + 已拍摄`。
- 浏览器证据：`evidence/s7-run-mode-375.png`；DOM 几何无横向溢出、无 `.app-shell`，关键控件 48～62px；原生 button/select 保留键盘语义，focus-visible 4px。
- 结果：通过。

## S8 — 全面验证与交付审计

- 退出信号：CMD-001～005 全绿；A1～A18 有自动化/PG/API/browser/静态证据；请求不接收账号/capture mode；无后置 surface、stub、调试或临时产物。
- 第一次 CMD-005 失败：depguard 拒绝 domain 直接 Gin/pgx，staticcheck 报布尔常量比较；按窄修复删除已被真实 adapter 取代的 compile harness、改用 `store.Row/Rows`、测试复用 account-scoped projection，并用 `!After(...)` 表达校验。
- 定向恢复：`golangci-lint run ./internal/shootplanning/... ./internal/platform/httpapi/...` 为 `0 issues`；`TestCommitPreparedPlanBatchInScopeSharesLedgerAndProbeTransaction` 通过。
- Fresh canonical DoD：`shoot-plan-core-dod-results.json` 中 CMD-001～005 全部 exit 0；CMD-002 的所有指定包通过；CMD-003 为 10/10；CMD-004 build/lint 通过；CMD-005 全仓回归通过。唯一 build warning 是既有 Vite 500 KiB chunk 提示，不在本 feature 扩 scope 做 code splitting。
- Scope gate：`shoot-plan-core-gate-results.json` 为 passed。warning 仅来自后续 feature checklist 中描述“禁止 TODO/FIXME”的验收文本；当前 core 源码清洁度扫描无命中。
- DoD contract gate：设计已有完整 Required Artifacts，修正全角冒号为 gate 可识别的 ASCII 格式后 `shoot-plan-core-dod-contract-results.json` 为 passed；仅格式闭合，无契约变化、无新增 design review。
- 结果：通过。

## A1～A18 证据索引

| 场景 | 主要证据 |
|---|---|
| A1 | `TestPostgresRepositoryAccountIsolationAndStableList`、`TestShootPlanningHTTPVerticalSlice`：title+subject 建案、分页、跨账号 404、响应无 account_id、Shot IDs 数组契约 |
| A2 | `TestTypedExecuteFramesResourceAndOperation`、`TestExecuteInScopeAndCapabilityUseOnePhysicalTransaction`、prepared batch PG probe、既有 order/schedule characterization |
| A3 | `TestApplicationPlanCommandsAndLifecycle`：required/optional/checked readiness 与 ready gate |
| A4 | lifecycle integration + `TestStructuralMutationDemotesInvalidReadyPlan`：ready 回 draft、in_progress 新 Shot、completed write gate |
| A5 | Run lifecycle integration/HTTP：ready 原子 start、状态 admission、session close |
| A6 | fixed-clock run session fixture + HTTP `client_capture_mode` negative：server 派生 live/backfill/unknown |
| A7 | replay truth table、run lifecycle、`TestConcurrentShotResultsWithSameExecutionRevisionAllowOneWinner` |
| A8 | `TestReplayCurrentOutcomeTruthTable`、void HTTP/PG lifecycle、invalid/repeated void negatives |
| A9 | soft-remove/history/finalization PG fixture：当前结构移出但历史保留 |
| A10 | completion integration 与 `TestConcurrentCaptureAndCompleteCannotCreateSnapshotGap` |
| A11 | reopen→void/new result→re-complete integration，旧/新 finalization revision 并存 |
| A12 | command/OpenAPI fixtures、archive policy/composition rollback、fresh-install marker、shared-lock promotion、planningctl readiness digest/CAS |
| A13 | frontend 10-case contract + S6 1600/1280/375 真实浏览器截图 |
| A14 | frontend offline/success-only contract + S7 375px/coarse/focus/high-contrast/断网重试真实证据 |
| A15 | List 固定一次 Count + 一次 QueryPage 的结构上界、PG stable-list/page fixture；Detail 为单 plan 固定查询序列 |
| A16 | strict HTTP subtests（account_id/capture_mode/unknown/mixed/trailing）、generated DTO contract、depguard、frontend forbidden surface/route/DOM tests |
| A17 | `TestApplicationRemovalGuardAndReminderArchiveRollback`、`TestApplicationArchiveCompositionFailsClosed`：lock→guard→remove、active/error rollback、wiring fail closed |
| A18 | fence unit/PG tests、two-connection migration/FK tests、rollback no-gap、contiguous watermark、typed capability/depguard compile conformance |

## 实现完成汇报

### 动了哪些文件

- 契约/生成：`api/openapi.yaml`、shootplanning Go contract、前端 `schema.d.ts`。
- 后端：migration；`internal/shootplanning`；platform idempotency/planningcapability/store/txcap；HTTP adapter/router/envelope；server composition；planningctl。
- 前端：`src/planning/**`、`App.tsx`、`AppShell.tsx`、专项测试和 package script。
- 规格/证据：本 feature design/checklist/design-review/implementation/gates/screenshots，以及经 owner 批准迁入执行 worktree 的 Epic roadmap、全部 child design 与 tracked v2 prototype baseline。
- 当前无 staged file、commit、push、PR、merge 或 deploy。

### 是否触碰方案外文件或引入新概念

否。共享 platform/HTTP/store 文件均由 approved design 的 AccountScope、typed transaction、capability/fence、route/composition、旧 wrapper 回归或 codegen 挂载点直接推出。没有新增设计术语、AI/provider/知识库、媒体、CRM、匿名协作或经营能力。

### 代码质量反射检查

- planning 前端进入独立领域目录，没有继续把 API/页面摊平到既有胖文件。
- HTTP handler 保持薄 adapter；生成接口 assertion 移到真实 adapter，删除临时 harness。
- repository 不直接依赖 pgx；测试不建立绕过 store 的连接。
- 未增加万能 helper、客户端 scope/capture mode、特殊账号分支或后置抽象。
- `main.go` / `router.go` 的更大 composition 拆分仍是独立 refactor 候选，本 feature 未顺手处理。

### TDD 与失败恢复

- S1～S5 与 S6/S7 可自动观察行为均留下 RED→GREEN→VERIFY；纯 codegen/迁移/CSS/浏览器与 S8 verification-only 项使用明确替代证据。
- S6 浏览器发现 null array 后补 HTTP RED 再修 repository；S8 CMD-005 首次失败后只修四个 lint 阻塞并重跑定向/全仓命令。
- 没有同一 failure 达到三轮，也没有扩 scope 或 owner-stop 条件。

### 清洁度与 residual risks

- `git diff --check`、Go/TS lint、build、targeted tests、全仓 `make check`、scope/DoD contract/DoD runner 均通过。
- 当前 core 源码无 console/fmt/log 调试输出、TODO/FIXME/XXX、placeholder handler、注释掉旧代码或重复 DTO。
- Raw `account_id` 命中只存在于既有非 planning OpenAPI 响应和 negative tests；planning 请求/响应 contract 均拒绝/隐藏。`capture_mode` 仅存在于服务端派生的响应事实，不存在于 planning 请求/API wrapper 输入。
- Archive effects 中的 share/reminder 名称是批准的三个 closed acknowledgement variants 与六个 exact effects；不是 share/reminder 业务 surface。
- Vite 500 KiB warning 属全局既有 bundle 提示；当前功能正确性与 lint 不受影响，未来 code splitting 应独立治理。
- Scope gate 的 TODO/FIXME warning 来自后续 checklist 的禁词测试描述，不是实现临时标记。

### 知识回写候选

- `make generate-check` 已改为“生成前后临时副本逐文件 cmp”，避免未 stage 的合法生成物被 `git diff --exit-code` 误判；acceptance 时评估是否写入 attention/compound。
- Gin 与 pgx depguard 同样覆盖 `_test.go`；集成测试应经 store seam 或既有 account-scoped projection，而不是给测试目录放宽生产架构边界。

### 下一步

所有 8 个 implementation steps 已完成。生成 evidence pack 并确认 implementation.before_review 三个 executable gate 全部 passed 后，进入一次独立 `cs-code-review`；不增加 design review 轮次。

## Round 1 Review Fix

### 范围

- 来源：`shoot-plan-core-review.md` Round 1 `changes-requested`。
- 只处理 REV-001～REV-009；不处理 OCR Medium/Low、Vite chunk warning 或后置 feature surface。
- REV-005 经批准 design/design-review 证据核验为超出当前 trusted inventory 信任模型的假设，记录驳回而不改生产契约。

### REV 修复证据

| REV | RED | GREEN / 改动 | Targeted verify |
|---|---|---|---|
| REV-001 | 真实 PG 中移出首位后无锚点新增，既有 Shot 被移到新 Shot 之后 | `insertionPosition` 对非空 current set 使用 `MAX(position)+1`，保留尾插语义 | `TestApplicationPatchNoopWindowAndBatchSemantics` passed |
| REV-002 | `[A,B] → [A,A]` 原先返回 nil 且可写坏 position | 双侧唯一集合校验；逐 ID update 必须 affected row=1 | 同一 PG test 断言 validation、revision/顺序不变后 passed |
| REV-003 | HTTP 创建后新增 Shot，再用原 key/body 重放会返回 revision 2/含 Shot 的实时详情 | create callback 同一事务读取 capability、组装并 ledger 保存公开 `PlanDetail`；handler 移除 ledger 外 GET | `TestShootPlanningHTTPVerticalSlice` exact body replay passed |
| REV-004 | 生成 TS 出现 `link & unlink`、`mark_ready & start/reopen` 不可能 intersection | OpenAPI 改为各自基于无冲突 base/显式 schema，重生成 Go/TS；caller 去除相关 `as` | `make generate-check`、`npm run build` passed |
| REV-005 | OCR 假设 wrong inventory/DB 组合需要第二身份源 | 驳回：design/design-review 已批准 trusted inventory identity/provenance + environment/deployment digest binding，并禁止新增运行时环境变量 | 文档/源码交叉核验；无生产改动 |
| REV-006 | retained history 存在但 current outcome unset 时 UI 推断 false | 新增纯函数 `shotHasExecutionHistory`，按 `execution_history[].shot_id` 判断 | 新增 Node 行为 test，11/11 passed；frontend build passed |
| REV-007 | DB fingerprint 等于 `sha256(key)`，与批准 frame 不同 | 改为 `sha256(account_id NUL shoot-plan.run-session.open.v1 NUL key)` | PG 直接读取 fingerprint 的 RED→GREEN passed |
| REV-008 | `make -s generate-check MAKE=false` 原先 exit 0 | recipe 开头 `set -eu`，任一 copy/generate/cmp 非零立即传播 | 注入失败得到 Make error；正常 `make generate-check` passed |
| REV-009 | stale/wrong archive effects 409 没有 details | domain typed error 携 authoritative acknowledgement；OpenAPI `ErrorDetails` additive union；handler 输出 generated typed details | HTTP 断言 `required_archive_acknowledgement` exact core variant passed；既有 schedule/order details 兼容测试 passed |

### TDD 记录

- RED 均在生产改动前真实出现，失败原因分别为目标行为：重复 reorder 被接受、尾插顺序颠倒、create replay 漂移、历史推断缺失、fingerprint frame 不符、generate 失败被吞、409 details 缺失。
- REV-004 为类型系统行为：以生成物中的不可能 intersection 作为 RED，重生成后通过去除 caller 断言与 `tsc -b` 证明 GREEN。
- REV-005 没有进入 TDD，因为本地事实核验否定了 finding 的前提，强行实现会改变批准的 release trust contract。

### 清洁度

- 所有代码修改均在 core review finding 的直接影响面；未实现 OCR 非阻塞建议。
- codegen 只通过 canonical `make generate` 更新，Go 文件已 gofmt。
- `git diff --check` passed；源码扫描没有 debug output、TODO/FIXME/XXX 或 placeholder handler，`placeholder=` 命中仅为 HTML 输入提示属性。

### Fresh Full Verification

- `scope-gate`: passed；blocking 0；4 个 warning 仍只来自后续 feature checklist 中“禁止 TODO/FIXME”的验收文本。
- `dod-runner`: passed；CMD-001～CMD-005 全部 exit 0；CMD-003 现为 11/11。
- CMD-002：shootplanning、idempotency、planningcapability、store、order、schedule、httpapi、server、planningctl 串行测试全部 passed。
- CMD-004：frontend `tsc -b`、Vite build、oxlint passed；仅保留既有 500 KiB chunk warning。
- CMD-005：全仓 `make check` passed，包含 build/lint、串行 Go tests、全部前端专项、148-case ops contract 与 codegen。
- `evidence-pack`: passed；archguard binary 与 meta-cc realtime summary 不可用，按 Goal provider policy 作为 non-blocking provider 状态记录。
- 下一步只做一次必要的完整独立代码复审；不进入 QA 直到 Round 2 `review.before_pass` 放行。
