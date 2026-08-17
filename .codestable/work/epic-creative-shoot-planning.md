---
epic: ../requirements/creative-shoot-planning.md
phase: executing
approved_revision: 91bf0c2b47e2b9d117339803ae3254c731420db0be634d81e9dd66722a579a67
current_item: ITEM-8
next_action: 实现 `creative-planning-v1-hardening`，复用已有 v1 ops 能力并补齐 planning-media schema-v2、strict reader/writer、restore state machine 与 release readiness
blocked_by: null
item_progression: continuous
milestone_commit: authorized
remote_publish: manual
---

## 子项进度

- [x] ITEM-1 · `shoot-plan-core`
  - commit: `876768c7b877f51385698e20ed7050574883c762`
  - evidence: legacy `.codestable/features/2026-08-05-shoot-plan-core/`
- [x] ITEM-2 · `planning-reference-assets`
  - commit: `becb30ad49b62e4a972484dc08f247d6b758288d`
  - evidence: legacy `.codestable/features/2026-08-05-planning-reference-assets/`
- [x] ITEM-3 · `plan-ingestion-capture`
  - commit: `87372301d4b8f3035404bea7132d1d2fd5685357`
  - verification: `make check` passed（2026-08-12）
  - review_closure: 第 3 轮 blocking findings 已通过定向修复处理，修复后自动化验证通过；根据 owner 指令未启动第 4 轮独立 review。验证通过不等于新增 review 签署。
  - evidence: legacy `.codestable/features/2026-08-05-plan-ingestion-capture/`
- [x] ITEM-4 · `shoot-plan-crm-integration`
  - status: implemented
  - commit: `be182a6f58333fc10798505bb94858e07eec58f4`
  - owner_dispatch: 2026-08-13 授权在 stage-1 canonical JSON 缺失时先实现；不把 gate 标为 passed
  - verification: `make generate-check`；`go test -p=1 ./internal/shootplanning/... ./internal/customer ./internal/order ./internal/schedule ./internal/platform/httpapi -count=1 -parallel=1`；`npm run test:shoot-plan-crm`；`npm run lint`；`npm run build`。CMD-001 / stage-1 仍未机械通过。本地 worktree PG `schema_migrations` dirty version 16，未 force，浏览器烟马未跑。
- [x] ITEM-5 · `plan-share-collaboration`
  - status: implemented
  - verification: `make generate-check` 与 `make check` 通过（2026-08-14）
  - evidence: `.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-s8-evidence.md`
  - checklist: S1/S3/S5/S8=`done`；S2/S4/S6/S7=`pending`（cancel/delete 双连接、content↔GC 双连接、claim↔remove/archive HTTP exact、真实浏览器截图等 residual）
  - note: 按 owner 授权提交本条里程碑后进入 ITEM-6。stage-1/stage-2 仍未标 passed；未 push。
- [x] ITEM-6 · `plan-assignment-reminders`
  - status: implemented
  - verification: 父代理复核 `make generate-check`、`go test -p=1 ./cmd/server ./internal/reminder/ ./internal/reminder/digest/ -count=1 -parallel=1`、`npm run test:plan-assignment-reminders` 均 exit 0；S6 证据记录 CMD-001～006（含 `make check`）exit 0。
  - evidence: `.codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-s6-evidence.md`
  - checklist: S1–S6=`done`；真实浏览器截图 residual；stage-1/stage-2 仍未标 passed
  - note: 按 owner 授权提交本条里程碑。生产走真实 archive/CRM/timezone/freshness 与 PG lease runner，扩展既有 Telegram digest，不向客户投递。未 push。
- [x] ITEM-7 · `plan-business-feedback`
  - status: implemented
  - verification: `npm run test:shoot-planning` 24/24；`npm run test:schedule` 59/59；`npm run test:settings` 6/6；`npm run test:package-price` 4/4；`npm run build`；`npm run lint`；`make generate-check`；`go test ./internal/shootplanning/business ./internal/platform/httpapi ./internal/settings -count=1 -parallel=1`。
  - browser: synthetic 隔离账号完成经营草稿生成、确认应用、时长更新、规则过期与普通档期投影；1600/1280/375/640px（200% 等效）无水平溢出，窄屏事实表单单列可达。
  - review: 独立 change review 经 3 轮闭环，最终 `PASS / 可合`；blocking/important 为 0。
  - residual: `stage-2-evidence-go` 仍为 `pending`，真实 5 个 eligible shared shoots 的 G3 owner approval 留到 Epic 最终人工验收，不以 synthetic 浏览器证据替代。
- [ ] ITEM-8 · `creative-planning-v1-hardening`

## 临时决策与证据

- Legacy Goal execution approval `0cfee048-fd76-491a-925c-caa8bf9eb434` 映射为：`item_progression: continuous`、`milestone_commit: authorized`、`remote_publish: manual`。
- `stage-1-evidence-go` 与 `stage-2-evidence-go` 仍为 `pending`；fixture、设计批准、agent 判断或绿色自动化测试都不能替代真实样本证据及 owner 批准。
- 2026-08-13：owner 在会话中回复「我批准」，意图批准 `stage-1-evidence-go`。同日 runner 结果为 `needs-human` / exit 2（named decision 仍 pending；`approval_evidence.stage-1-evidence-go` 的 path/SHA-256/gate_version 为空；roadmap 下不存在 `evidence/*.json`）。口头批准未写入 `approval-report.md`，避免无绑定地把 decision 标成 approved 后变成 `failed`。
- 2026-08-13 本地 worktree PG 计数（不含 PII）：`shoot_plans=4`，`first_ready=1`，`live` run session=0。G1 要求纳入 5 份且至少 3 份 live，G2 要求 5 份 first_ready 且 active 中位数 ≤600s；当前本地库不够生成 `status=passed` 的 canonical JSON。
- 2026-08-13：owner 表示功能大致过完暂无问题，授权先跑后续开发。解释为 **implementation dispatch 延期 stage-1**，不是把 `stage-1-evidence-go` 写成 approved/passed，也不补空 SHA 绑定。ITEM-4 进入 implementing；stage-1 仍是未满足的真实使用证据，stage-2 在 ITEM-7 前继续暂停。工作区沿用已有 `.worktrees/creative-shoot-planning` / `feat/creative-shoot-planning`。
- `.codestable/roadmap/creative-shoot-planning/` 与对应 `.codestable/features/` 路径作为 legacy v1 冻结证据输入保留，不再承担活动进度的 canonical owner，也不在本次更新中改写。
- 分支基线：`feat/creative-shoot-planning` 已包含本地最新 `main`，当前三个 Epic 里程碑提交位于其上；安全 stash 保留，未应用、未删除。
- 2026-08-14：ITEM-4 implementation 收口。自动化验证通过；stage-1 仍 deferred；本地 PG dirty v16 未 force。按 owner 授权提交本条里程碑后进入 ITEM-5。
- 2026-08-14：ITEM-5 S8 全矩阵验证后按授权提交里程碑。`make generate-check` / `make check` 通过；补 `expiry_quote_stale`、archive 投影 404、IP previous-day grace、Bearer feedback allowlist golden；修 auth-legacy tip 钉 `fullVersion==24`。S2/S4/S6/S7 与若干 A 残差记入 evidence，不阻塞本条里程碑。stage-1/stage-2 未标 passed；未 push。
- 2026-08-14：ITEM-6 S1–S6 实现后按授权提交里程碑。生产接线为真实 archive/CRM/timezone/freshness 与 PG lease runner；扩展既有 Telegram digest，不向客户投递。真实浏览器截图 residual；stage-1/stage-2 未标 passed；未 push。ITEM-7 仍被 `stage-2-evidence-go` 挡住。
- 2026-08-17：恢复执行时重跑 `plan-business-feedback + stage-2-evidence-go`，runner 返回 `needs-human` / exit 2；`approval-report.md` 的 named decision 仍为 `pending`，canonical evidence path/SHA-256/gate version 均为空。ITEM-7 保持未开工，ITEM-8 因依赖 ITEM-7 亦不可调度。
- 2026-08-17：owner 指示只有必须人工验证的部分才提醒，优先功能开发，并在整个 Epic 完成后集中列出人工验收项；自动验收尽量使用浏览器模拟。该指令解释为允许 ITEM-7 implementation dispatch 延后 stage-2 真实样本验收，但不把 named decision 或 gate 伪造为 approved/passed；真实 5 个 eligible shared shoot 的 G3 evidence 留到 Epic 最终 owner gate。
- 2026-08-18：ITEM-7 实现与独立审查收口。普通档期 API payload 不携带经营字段；经营时长依据随 `SlotDraft` 保存，手动改结束时间后断开联动；移动端经营事实表单改为真实单列 grid。浏览器使用 synthetic 隔离账号验证生成、应用、过期和响应式布局；纯状态测试补足原生 date/time 输入无法由浏览器控制层触发 React 受控事件的自动验收缺口。
- 2026-08-18：ITEM-7 权威定向验证、前端 build/lint 与 `make generate-check` 通过。全量 `make check` 中 HTTP 测试曾在并发锁等待处超时，HTTP/reminder 首次独立重跑曾出现 Testcontainers `port "5432/tcp" not found`；受影响包使用 `-count=1 -parallel=1` 再次重跑均通过，符合 `.codestable/attention.md` 已记录的本机 Docker 波动，未观察到业务断言回归。
