# reminder-engine goal protocol

## 执行 loop

1. 读取 `reminder-engine-design.md`（approved）、`reminder-engine-checklist.yaml`、`goal-plan.md`、`goal-state.yaml`。
2. 进入 `cs-feat` implementation 阶段完成 checklist 8 个 steps（S1→S8 依赖序）；代码行为 step 默认 TDD micro-loop（RED → GREEN → VERIFY），不能 TDD 时写 `TDD exception` 和替代证据（S1/S8 例外口径见 goal-plan）。
3. 运行 implementation gates（CMD-001/002 必绿），生成 evidence pack / gate results / DoD results。
4. 进入 `cs-code-review`；有 blocking 就 review-fix 后重跑 review（state: review/fixing → review/ready）。
5. review passed 后进入 `cs-feat` QA；QA failed / blocked 就 qa-fix 后重跑 review 和 QA（state: qa/fixing → review/ready）。
6. QA passed 后进入 `cs-feat` acceptance：更新 checklist checks、items.yaml（status: done）、req reminder-engine draft→current（cs-req update）、契约回写班车（cs-roadmap update：§4.3 排序 A3、§4.4 dedup 年份 A4 + custom 键 D7）、roadmap §3/items notes 补 settings 包归属（D1）、观察项①②③记录、评估 cs-keep 沉淀（settings 有效值模式 / runner 形状约定）。
7. 全部通过后先写 `stage: complete` / `status: passed`，再打印 `CS_FEATURE_GOAL_COMPLETE`。

## Goal 模式接管

- 普通流程中各阶段停等用户确认的 checkpoint，在 goal 模式下改为写入报告、状态和证据记录；只有命中 handoff 条件才停。
- **例外（attention.md 硬约束，优先于接管规则）：git commit 必须经人工同意，不得自动执行**——实现完成后把建议的 commit 切分与 message 写进报告，等 owner 执行或明确授权；禁 `git commit --no-verify`。
- Goal driver 不得绕过 TDD policy；行为代码 step 缺 RED/GREEN/VERIFY evidence 且无 `TDD exception` 时，implementation gate 不通过。
- 每个阶段 gate 通过后按 goal 协议状态机更新 `goal-state.yaml` 的 `stage`/`status`；driver 中断后按仓库事实（ledger + `git log`）续跑，不重复已完成 step。
- design-review 的 residual-risk（churn 静默、digest 空窗、timezone 西移）在 QA 阶段按 design §4 观察项口径记录，不升级为阻塞。

## Handoff

命中以下条件先写 `stage: handoff` / `status: blocked` / `handoff_reason` / `handoff_next`，再输出：

```text
CS_FEATURE_GOAL_HANDOFF
Reason: <具体阻塞>
Next: <建议动作>
```

条件：

- 需要改变 approved design、feature 范围、公开契约或 roadmap item（注意：goal-plan 已声明的 acceptance 回写班车是计划内动作，不算）。
- 独立 Task agent reviewer pending / failed / blocked，且没有用户明确降级。
- 同一失败项三轮修复仍不通过。
- 外部凭证或环境缺失导致核心行为无法判断（如 Docker/testcontainers 不可用且无法归因）。
- 用户主动要求暂停、改方向或终止。
