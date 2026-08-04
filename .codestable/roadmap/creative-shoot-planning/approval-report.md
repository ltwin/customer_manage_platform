---
doc_type: approval-report
unit: creative-shoot-planning
status: pending
reason: roadmap-owner-approval
approvals:
  epic-split: approved
  roadmap-plan: pending
  stage-1-evidence-go: pending
  stage-2-evidence-go: pending
approval_groups:
  epic-split-2026-08-04:
    status: approved
    confirmation_id: chat-2026-08-04-split-creative-planning-intelligence
    decisions: [epic-split]
approval_evidence:
  stage-1-evidence-go:
    path: ""
    sha256: ""
    gate_version: ""
  stage-2-evidence-go:
    path: ""
    sha256: ""
    gate_version: ""
created_at: 2026-08-02
updated_at: 2026-08-04
---

# 创作型拍摄策划首版批准报告

## Decision History

- 2026-08-02：收敛前 11 条混合路线的批准请求已失效；它不得被 runtime 或后续 agent 当成批准证据。
- 2026-08-04：owner 明确同意拆成“首版 epic + AI/知识后置 epic”。已将 `epic-split` 记录为 approved，confirmation id 为 `chat-2026-08-04-split-creative-planning-intelligence`。
- 2026-08-04：原 `void` 状态纠正为历史 superseded 语义；当前 canonical report 全量重建，不继承旧八项 pending decision。

## Decision Needed

### `roadmap-plan`

待独立 roadmap review 通过后，由 owner 决定是否批准当前 8 条首版路线。当前为 `pending`，不能激活 roadmap 或创建 child feature。

### `stage-1-evidence-go`

未来 goal-execution gate，当前为 `pending`。所有 8 条 child design 可以先按标准 batch 完成；只有 shoot-plan-core、planning-reference-assets、plan-ingestion-capture implementation/acceptance 完成并积累 5 个合格真实样本，owner 查看 G1/G2 evidence 后才决定。approved 才允许 goal driver 派发 `shoot-plan-crm-integration` implementation。

批准时必须用一次原子更新同时写 named decision、Decision History 和 `approval_evidence.stage-1-evidence-go` 的 canonical path/SHA-256/gate version；只有状态与 hash binding 同时有效才可放行。

### `stage-2-evidence-go`

未来 goal-execution gate，当前为 `pending`。只有 full 分享/认领/提醒 implementation/acceptance 完成并积累 5 个 eligible shared shoot，owner 查看互动率与准备缺失率 evidence 后才决定。approved 才允许 goal driver 派发 `plan-business-feedback` implementation。

批准时同样原子绑定 stage-2 evidence path/SHA-256/gate version；更新 evidence 内容会使旧批准失效，必须重新确认。

## Why Now

原规划把首版和 AI/知识能力放在同一 16 节点 DAG，人工 G4 无法阻止后置节点提前 ready，且唯一 hardening/生产媒体恢复被拖到 AI 末尾。拆分后本 roadmap 必须作为独立首版重新 review 和批准，不能沿用旧报告。

## Context

当前首版路线包含：

1. `shoot-plan-core`；
2. `planning-reference-assets`；
3. `plan-ingestion-capture`，唯一 minimal loop；
4. `shoot-plan-crm-integration`；
5. `plan-share-collaboration`；
6. `plan-assignment-reminders`；
7. `plan-business-feedback`；
8. `creative-planning-v1-hardening`。

硬约束：策划非强制；客户零价格信号；首版无 AI/知识；提醒只给摄影师；原作素材 display-only；planning media 在首版进入生产恢复。

## Options

### Option A — 批准当前首版 roadmap（review passed 后推荐）

- 将 `roadmap-plan` 原子更新为 approved 并记录 confirmation id；
- roadmap `draft→active`；
- 按标准 `cs-epic` 完成全部 8 条 child design/design-review 和统一设计确认；
- 生成 goal package 时固化两个 pre-implementation evidence gate；`stage-1-evidence-go`、`stage-2-evidence-go` 继续 pending，不能被 roadmap 或全量 design 批准替代。

### Option B — 要求继续修改

- `roadmap-plan` 保持 pending；
- 指定需调整的范围、契约或顺序；
- 修改后重新独立 review。

### Option C — 拒绝首版路线

- 将 `roadmap-plan` 标为 rejected；
- roadmap 保持 draft/archived disposition；
- 不创建 child feature。

## Recommendation

在独立 review 没有 blocking finding 后选择 Option A。8 条路线已把最小闭环、匿名公网面、提醒收件人、经营草稿和生产恢复放入明确 owner item，并用两个未来 named approval 保存真实证据停顿。

## Risks And Tradeoffs

- 首版不做离线可写；外景断网可能要求回 planning update。
- proposal/full 和匿名媒体使系统首次拥有未认证公网读写面，安全证据是发布阻断项。
- `stage-1-evidence-go` 与 `stage-2-evidence-go` 会让路线等待真实拍摄，不允许用 fixture 冒充市场证据。
- 客户不会收到自动提醒；首版价值是提醒摄影师核对，不是外部消息平台。
- 订单价格调整引入 additive audit contract，但仍由摄影师显式确认，不自动定价。

## Non-Automatic Actions

批准 roadmap 不自动：

- 批准未来 G1/G2 或 G3；
- 批准或启动 `creative-shoot-intelligence`；
- 创建 requirement/ADR；
- 实现、调用外部服务、使用凭证；
- commit、push、merge 或 deploy；
- 接受 child design/QA 中出现的新 residual risk。

## After You Answer

- Option A：持久化 `roadmap-plan` approval、激活 roadmap、重跑 workflow/DAG；先完成 requirement gate，再按第一批依赖进入 child design。
- Goal 阶段：全部 child design 统一确认后才生成执行包；driver 完成前三条后在 CRM implementation 前停于 stage-1 gate，完成协作/提醒后在 business implementation 前停于 stage-2 gate。
- Option B：保持 draft，按反馈做 planning update 和独立复审。
- Option C：记录 rejected 与原因，停止本路线。
