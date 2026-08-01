---
doc_type: approval-report
unit: .codestable/roadmap/photographer-private-crm
status: approved
reason: other
approvals:
  all-feature-designs: approved
  calendar-v2-design: approved
  v1-hardening-auth-residual: approved
  v1-hardening-telegram-evidence: approved
  goal-acceptance: approved
  goal-commits: approved
approval_groups:
  calendar-v2-design-confirmation:
    status: approved
    confirmation_id: "bcf1b5e3-2884-475f-83e1-89543b1dad50"
    decisions:
      - calendar-v2-design
  v1-hardening-design-confirmation:
    status: approved
    confirmation_id: "47299893-6531-4ce7-91ad-58f47c895ea1"
    decisions:
      - all-feature-designs
      - v1-hardening-auth-residual
      - v1-hardening-telegram-evidence
  goal-execution:
    status: approved
    confirmation_id: "89e1d9c5-4956-4ee3-9fd7-0cdb7a19f995"
    decisions:
      - goal-acceptance
      - goal-commits
created_at: 2026-07-22
answered_at: 2026-07-23
calendar_v2_design_answered_at: 2026-07-31
selected_options:
  all-feature-designs: D1-A
  calendar-v2-design: Option A
  v1-hardening-auth-residual: H1-A
  v1-hardening-telegram-evidence: H2-A
  goal-execution: Option A
---

# Photographer Private CRM Approval Report

## Decision History

- 2026-07-31：owner 明确回答“批准 Option A，确认更新后的 13 个 child design baseline；仅将 calendar-v2-redesign design 标记为 approved。”本次确认 ID 为 `bcf1b5e3-2884-475f-83e1-89543b1dad50`；calendar v2 design 已机械升级为 `approved`，既有 Goal execution、commit 与 parent handoff 未扩权或改写。
- 2026-07-31：新增第 13 个 `calendar-v2-redesign` 后，第四轮独立 design review 已通过；parent resolver 返回 `all-feature-designs-confirmation`。此前 12 个 child 的批准保持原样，本报告新增 `calendar-v2-design` pending decision，等待 owner 对更新后的 13-child design baseline 做统一确认。
- 2026-07-23：owner 回答“批准”，按推荐 Option A 以一次 owner answer 原子批准 `goal-acceptance` 与 `goal-commits`；Goal execution confirmation ID 为 `89e1d9c5-4956-4ee3-9fd7-0cdb7a19f995`。
- 2026-07-22：owner 回答“按推荐项批准”，一次性批准 `D1-A + H1-A + H2-A`；confirmation ID 为 `47299893-6531-4ce7-91ad-58f47c895ea1`。
- D1-A：完整 `v1-hardening` design/checklist 已批准，design 已从 `draft` 机械升级为 `approved`。
- H1-A：当前 V1 接受无登录限速、30 天 JWT、localStorage Bearer、无改密 API 四项 residual；canonical `auth-hardening-report.md` 已创建为 `open`。
- H2-A：复用 2026-07-17 owner-attested TG true-external transport/binding；本轮仍 fresh 验证同一 synthetic fixture 的次日 digest payload、调度关联与完整 A24 主链。
- 上述设计确认不会自动授权 Goal acceptance、自动 commit、push、merge 或部署；本节作为 durable history 保留。

## Calendar V2 Design Confirmation

### Decision Recorded

owner 已选择 Option A：统一确认更新后的 13 个 child design baseline。前 12 个已批准 design 保持不变，仅把通过第四轮独立评审的 `calendar-v2-redesign` 从 `draft` 机械升级为 `approved`。

### Why Now

- `.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design-review.md` 已为 round 4 `passed`，无 unresolved blocking / important finding。
- `codestable-workflow-next.py feature --epic-child-batch` 返回 `return-to-cs-epic-batch-loop`。
- Parent `codestable-workflow-next.py epic` 返回唯一 user gate：`all-feature-designs-confirmation`。
- 旧 `all-feature-designs` durable history 只覆盖当时的 12 个 child，不能自动扩大到 2026-07-31 新增的第 13 个 child。

### Confirmation Scope

- 批准 `.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md` 与 checklist 的最终范围：Settings availability、export schema v2、shoot 批量摘要、月/周双视图、空档/概览纯计算、移动 CRUD、冲突预览 generation 与响应式工作区。
- 保持前 12 个 child 的 approved/accepted 历史不变；不回退或重写旧 `schedule-calendar`、`data-export`、`v1-hardening` 证据。
- 仅把 `calendar-v2-redesign` design 从 `draft` 改为 `approved`，并把 `approvals.calendar-v2-design` 与对应 group 记为 `approved`。
- 保留 parent `status: handoff`、`current_feature_index: 11` 与 `v1-hardening` implementing 状态；由后续 workflow resolver 决定 GoalPackage/implementation 是否可推进。

### Options

#### Option A — 批准更新后的全部 child design（推荐）

批准当前 calendar v2 design，并将更新后的 13-child design baseline 记为统一确认；随后重跑 parent resolver。

#### Option B — 要求修改 calendar v2 design

保持 `calendar-v2-design` pending、design 为 `draft`，回 `cs-feat` design 阶段按 owner 指定内容修订并重新评审。

### Non-Automatic Actions

本设计确认不会自动授权或执行：

- 修改 parent handoff/current feature index；
- Goal acceptance 或 scoped commit 对第 13 个 child 的扩权；
- 创建 commit、push、PR 或 merge；
- deploy、migration 执行、promotion 或 production cutover；
- 主动发送消息、修改云资源或真实生产数据。

### Applied Result

- `approvals.calendar-v2-design: approved`。
- `approval_groups.calendar-v2-design-confirmation.status: approved`，confirmation ID 为 `bcf1b5e3-2884-475f-83e1-89543b1dad50`。
- calendar v2 design frontmatter 为 `status: approved`。
- 未修改 parent handoff、current feature index、既有 Goal authorization projection 或任何业务代码；下一动作由 workflow resolver 决定。

## Decision Recorded

GoalPackage 已生成并通过初步结构自查。owner 已对同一次 Goal execution 启动原子批准以下两项命名授权：

1. `goal-acceptance`：允许 Goal driver 在每个 feature 的实现、独立 code review、QA、DoD/gates 和真实证据全部通过后，以 `ResumeGoalAcceptance approval-report.md#goal-acceptance` 完成 acceptance，不再逐 feature 等待相同内容的人工批准。
2. `goal-commits`：允许 Goal driver 在单个 feature accepted、roadmap/goal-state 状态已持久化且授权仍可机械验证后，自动创建 scoped commit。

两项已由同一次 owner answer 一起批准；本记录没有用 D1/H1/H2 设计批准替代 Goal execution 授权。

## Why Now

Epic 状态机已从 `all-feature-designs-confirmation` 推进到 GoalPackage：

- 前 11 个 feature 已是历史 `done/accepted` baseline；
- 第 12 个 `v1-hardening` 是唯一 `pending` feature；
- `goal-state.yaml` 使用 `current_feature_index: 11`；
- Git baseline 为 `f27d4296edfc86e8f4cceef0553ba4f1dd41a0c9`；
- Goal state 为 `awaiting-authorization`；
- 两份 ApprovalRef 已固定，但 named decisions 仍为 `pending`；
- 未取得本 checkpoint 前，状态机不得返回 `dispatch_goal`。

## Context

- Goal plan：`.codestable/roadmap/photographer-private-crm/goal-plan.md`
- Goal state：`.codestable/roadmap/photographer-private-crm/goal-state.yaml`
- Runtime protocol：`goal-protocol.md`、`goal-protocol-feature-loop.md`、`goal-protocol-gates.md`、`goal-protocol-audit.md`
- Feature specs：`.codestable/roadmap/photographer-private-crm/goal-features/*.md`
- Current branch：`develop`
- Pending implementation：`v1-hardening`
- Historical accepted projection：platform-skeleton、customer-core、package-catalog、customer-profile-complete、order-tracking、schedule-calendar、reminder-engine、customer-avatar、telegram-digest、dashboard、data-export
- Task-external untracked files：`.workflow/`、`install-cpamp.sh`；必须保持未触碰、未 stage、未 commit
- H1 follow-up：`.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`，`status: open`

准备派发的 literal command 为：

```text
/goal "执行 CodeStable roadmap 目录 .codestable/roadmap/photographer-private-crm 下的 goal 执行包。先读取 goal-protocol.md、goal-protocol-feature-loop.md、goal-protocol-gates.md、goal-protocol-audit.md、goal-state.yaml、goal-plan.md；这是已由用户确认 roadmap 和全部 feature design，并在同一次 Goal 启动确认中授权 Goal acceptance 与每个 feature 自动 scoped-commit 的模式，两项 ApprovalRef 仍须分别机械核验。按 goal-state.yaml 的 features 顺序循环：进入 cs-feat implementation、cs-code-review、cs-feat QA；review/QA 失败按协议修复重跑，awaiting/needs-human/blocked 分别等待、请求输入或 handoff。QA passed 后只用 goal-acceptance ApprovalRef 调用 ResumeGoalAcceptance；accept 后先持久化 accepted 状态与新 index，再机械核验 goal-commits ApprovalRef，只有有效时才 scoped-commit 本 feature 的全部状态更新，缺失、不匹配或 rejected 必须 handoff 且不得提交。每个 feature 完成打印 CS_ROADMAP_GOAL_FEATURE_DONE；全部完成后做最终 roadmap 审计。只有出现 CS_ROADMAP_GOAL_COMPLETE，且所有 feature review/QA/acceptance、授权提交和最终审计均通过、没有 CS_ROADMAP_GOAL_HANDOFF，本 goal 才算完成。"
```

## Options

### Option A — 原子授权 Goal execution（推荐）

一次性批准：

- `approval_groups.goal-execution.status: approved`；
- 一个新的非空 `confirmation_id`；
- `approvals.goal-acceptance: approved`；
- `approvals.goal-commits: approved`；
- `goal-state.yaml` 中相同 confirmation ID、两项 approved projection 与 `status: ready-to-dispatch`。

授权后主流程立即重跑状态机；只有它返回 `dispatch_goal` 且两份 ref 均有效，才尝试派发可见 Goal driver。

### Option B — 拒绝 Goal execution

把 `goal-execution` group 和两项 named decisions 原子记录为 `rejected`，将 goal-state 持久化为 `handoff`，不派发 driver、不实现、不 commit。设计与 GoalPackage 保留，之后需要新的明确 owner 决策才能恢复。

不支持“只批准其中一项并启动”的第三种状态。

## Recommendation

推荐 Option A。

理由：设计、H1/H2、依赖状态与 GoalPackage 已完整落盘；一个原子 checkpoint 可以同时保证 acceptance 和 scoped-commit 各自有独立机器证据，避免 driver 在 acceptance 后卡在未授权提交或把启动授权误当成提交授权。

## Risks And Tradeoffs

- `goal-acceptance` 会让 driver 在真实 gates 通过后自动写 acceptance、把 46 个 checks 更新为 passed，并回写 roadmap；它不允许跳过独立 review、QA、A1～A27 或 owner/environment attestation。
- `goal-commits` 允许本 roadmap 自动创建 scoped commits。当前实际 pending feature 只有 `v1-hardening`；前 11 个历史 accepted feature 的代码不会重新提交。
- scoped commit 可包含 `v1-hardening` 代码、spec、命令/浏览器/ops evidence、review、QA、acceptance、roadmap/items/goal-state/audit 更新，以及由官方 gate 基于真实历史产物生成的必要 canonical evidence backfill。
- 任何历史 evidence 无法真实证明时必须 handoff；授权不允许创建占位报告、复制其他 feature JSON 或伪造 passed 状态。
- Docker/浏览器/ECS/TG owner attestation 等 core 环境不可用时，driver 必须 needs-human/handoff，不能静态降级。
- 当前 develop 工作区已有本任务的未提交设计/GoalPackage 文件。实现前仍必须单独遵守 `.codestable/attention.md` 的 branch/worktree 选择；本授权不替代该环境选择。
- 自动 commit 可能把一个 feature 的大量证据与状态一起提交，但不得包含任务外 `.workflow/`、`install-cpamp.sh`、凭证、PII、备份半包或临时 Docker 资源。

## Non-Automatic Actions

即使 Option A 获批，下列动作也不会自动发生：

- remote push；
- 创建或更新 PR；
- merge；
- publish/package release；
- deploy、promotion 或 production cutover；
- 修改云防火墙、TLS、域名、ECS、Telegram BotFather 或真实生产数据；
- 自动关闭 `auth-hardening` open risk；
- 未经选择直接在当前 branch 或新 worktree 开发。

这些动作仍需各自流程和 owner 的独立授权。

## After You Answer

- 若批准 Option A：生成新的 Goal execution confirmation ID；先原子更新本 approval report 的 group 与两项 named decisions，形成 durable commit point；再幂等同步 goal-state；重跑状态机并按共享 agent conventions 尝试派发同一条 literal `/goal`。
- 若选择 Option B：原子记录 rejected，写 goal-state handoff reason/next，并停止。
- 若回答含糊或只批准一项：保持全部 pending，不修改 goal-state，不派发。
