---
doc_type: approval-report
unit: .codestable/roadmap/self-service-account-system
status: approved
reason: goal-execution-authorized
approvals:
  roadmap-plan: approved
  all-child-designs: approved
  goal-acceptance: approved
  goal-commits: approved
approval_groups:
  goal-execution:
    status: approved
    confirmation_id: "a2f3d9ac-c7b1-4da7-b6a4-2a245f576741"
    decisions:
      - goal-acceptance
      - goal-commits
created_at: 2026-07-30
designs_answered_at: 2026-07-31
goal_execution_answered_at: 2026-07-31
selected_options:
  goal-execution: Option A
implementation_workspace: current-branch
implementation_branch: feature/userCenter
---

# Self-Service Account System Approval Report

## Decision History

- 2026-07-31：owner回答“批准 Goal execution”，按推荐Option A以一次owner answer原子批准`goal-acceptance`与`goal-commits`；Goal execution confirmation ID为`a2f3d9ac-c7b1-4da7-b6a4-2a245f576741`。
- 2026-07-30：owner批准Option A，两条粗粒度全栈feature roadmap；`roadmap-plan: approved`，roadmap进入active。
- 2026-07-30：requirement、CONTEXT与ADR-005～007获批并固化。
- 2026-07-30：owner要求停止反复design review、缩短设计阶段；两份child design对已知finding做owner-authorized focused closure，不再继续review循环。
- 2026-07-31：owner回答“确认，然后使用当前分支吧”，统一批准两份child design，并选择`feature/userCenter`当前分支，不创建worktree或新分支。
- Roadmap与child design批准不替代本次独立Goal execution授权；本次授权也不自动允许push、PR、merge、deploy或任何生产操作。

## Decision Recorded

Goal package已生成并通过初步结构校验。owner已用一次回答原子批准：

1. `goal-acceptance`：允许Goal driver在单个feature的implementation、独立code review、QA、DoD/gates与真实证据全部通过后，使用`ResumeGoalAcceptance approval-report.md#goal-acceptance`完成acceptance、更新checks与roadmap，不再逐feature等待相同确认。
2. `goal-commits`：允许Goal driver在单个feature accepted、items/goal-state状态已持久化且授权仍可机械验证后，创建一次该feature的scoped commit。

两项已由同一次owner answer一起批准，并绑定同一非空confirmation ID；本记录没有用此前roadmap或design确认替代Goal execution授权。

## Why Now

- Roadmap与两份child design都已批准。
- Goal state使用Git baseline`411bbb1fc5bfc35e5617718cd8a0e5f242ad648f`，feature顺序为`email-account-access → public-auth-hardening`，index为0。
- 当前branch已确认`feature/userCenter`。
- Goal package、两份goal-feature spec与四份runtime protocol已落盘。
- canonical approval group与两份named decision已批准；`goal-state.yaml` projection必须使用同一confirmation ID并分别机械核验两份ApprovalRef后，状态机才允许派发。

## Context

- Goal plan：`.codestable/roadmap/self-service-account-system/goal-plan.md`
- Goal state：`.codestable/roadmap/self-service-account-system/goal-state.yaml`
- Runtime protocol：`goal-protocol.md`、`goal-protocol-feature-loop.md`、`goal-protocol-gates.md`、`goal-protocol-audit.md`
- Feature specs：`.codestable/roadmap/self-service-account-system/goal-features/*.md`
- Current branch：`feature/userCenter`
- Pending features：`email-account-access`、`public-auth-hardening`
- Protected task-external paths：`.codestable/brainstorms/creative-shoot-planning/`、`.codestable/brainstorms/welcome-login-design/`、`frontend/image.png`

当前branch存在上述用户任务外未提交内容。若Goal execution获批，driver在写业务代码前会核对goal-plan中的三份checksum，创建只包含这三条path的local stash并记录ref；每个scoped commit都禁止包含它们。Final audit后或任一handoff前恢复stash并复核checksum。Stash失败/冲突立即handoff，禁止丢弃用户文件。

准备派发的literal command：

```text
/goal "执行 CodeStable roadmap 目录 .codestable/roadmap/self-service-account-system 下的 goal 执行包。先读取 goal-protocol.md、goal-protocol-feature-loop.md、goal-protocol-gates.md、goal-protocol-audit.md、goal-state.yaml、goal-plan.md；这是已由用户确认 roadmap 和全部 feature design，并在同一次 Goal 启动确认中授权 Goal acceptance 与每个 feature 自动 scoped-commit 的模式，两项 ApprovalRef 仍须分别机械核验。按 goal-state.yaml 的 features 顺序循环：进入 cs-feat implementation、cs-code-review、cs-feat QA；review/QA 失败按协议修复重跑，awaiting/needs-human/blocked 分别等待、请求输入或 handoff。QA passed 后只用 goal-acceptance ApprovalRef 调用 ResumeGoalAcceptance；accept 后先持久化 accepted 状态与新 index，再机械核验 goal-commits ApprovalRef，只有有效时才 scoped-commit 本 feature 的全部状态更新，缺失、不匹配或 rejected 必须 handoff 且不得提交。每个 feature 完成打印 CS_ROADMAP_GOAL_FEATURE_DONE；全部完成后做最终 roadmap 审计。只有出现 CS_ROADMAP_GOAL_COMPLETE，且所有 feature review/QA/acceptance、授权提交和最终审计均通过、没有 CS_ROADMAP_GOAL_HANDOFF，本 goal 才算完成。"
```

## Options

### Option A — 原子授权Goal execution（推荐）

一次性批准：

- `approval_groups.goal-execution.status: approved`；
- 新的非空`confirmation_id`；
- `approvals.goal-acceptance: approved`；
- `approvals.goal-commits: approved`；
- Goal state中相同confirmation ID、两项approved projection与`status: ready-to-dispatch`。

授权后主流程立即重跑状态机；只有返回`dispatch_goal`且两份ref有效，才尝试派发可见Goal driver。

### Option B — 拒绝Goal execution

把group和两项named decision原子记录为rejected，将goal-state写为handoff，不派发、不实现、不commit。Design与Goal package保留，之后需新的明确owner决策恢复。

## Recommendation

推荐Option A。Roadmap、design、branch与执行包都已明确；原子checkpoint能让acceptance和scoped commit各自保留独立机器证据，又避免逐feature重复询问。

## Risks And Tradeoffs

- `goal-acceptance`允许driver在真实gates通过后自动写acceptance、把email feature 25个checks与hardening 23个checks更新为passed并回写roadmap；不允许跳过独立review、QA、core path或外部checkpoint。
- `goal-commits`允许最多每个feature一个scoped commit；可能包含该feature代码/spec/evidence/review/QA/acceptance以及shared requirement/ADR/roadmap/goal-state更新。
- 当前branch的related planning/spec是未提交状态，第一条feature scoped commit会纳入其必要依赖；三条protected user path绝不纳入。
- True-external mail、browser/Docker/PG、production-shaped preflight等core证据不可用时必须needs-human/handoff，不能静态降级或伪造passed。
- 自动commit不会使用`--no-verify`；pre-commit/lint失败必须修复或handoff。

## Non-Automatic Actions

即使Option A获批，下列动作也不会自动发生：

- remote push；
- 创建或更新PR；
- merge；
- publish/package release；
- deploy、promotion或production cutover；
- 执行production migration/rollback/root-secret rotation；
- 修改registration生产开关、DNS、邮件provider credential、云网络或真实生产数据；
- 把任务外protected paths stage或commit；
- 自动关闭未由真实acceptance证明的residual risk。

这些动作仍需各自流程和owner独立授权。

## After You Answer

- 若批准Option A：生成Goal execution confirmation ID；先原子更新本report的group和两项decision形成durable commit point，再幂等同步goal-state；重跑状态机并按agent conventions派发同一literal `/goal`。
- 若选择Option B：原子记录rejected，写goal-state handoff reason/next并停止。
- 若回答含糊或只批准一项：保持全部pending，不修改goal-state，不派发。
