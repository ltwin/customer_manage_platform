# telegram-digest goal protocol

## 执行 loop

1. 读取 `telegram-digest-design.md`（approved）、`telegram-digest-checklist.yaml`、`goal-plan.md`、`goal-state.yaml`。
2. 确认 owner 已选择当前 branch 或 feature worktree；未确认则停止，不写业务代码、不启动 driver。
3. 进入 `cs-feat` implementation，按 STEP-001～008 完成实现；代码行为默认 RED→GREEN→VERIFY，例外必须写 `TDD exception` 与替代证据。
4. 每完成一个 step，立即在 `goal-state.yaml.ledger` 写 step id、状态、证据/commit 范围；续跑以 ledger + `git log` + 仓库事实为准，不重复完成 step。
5. 运行 CMD-001～005，生成 implementation evidence pack、gate results、DoD results；失败必须 fix-or-block。
6. 把 state 写为 `review/ready`，进入独立 `cs-code-review`；blocking 时写 `review/fixing`，修复后回 `review/ready` 重跑。
7. review passed 后写 `qa/ready`，进入 `cs-feat` QA；QA failed/blocked 写 `qa/fixing`，修复后回 `review/ready`，重跑 review 和 QA。
8. QA passed 后写 `acceptance/ready`，进入 `cs-feat` acceptance；更新 checklist、requirement、roadmap/related requirements/长期约束与最终证据。
9. review、QA、acceptance 全 passed 且无 handoff 后，先写 `stage: complete` / `status: passed`，再打印 `CS_FEATURE_GOAL_COMPLETE`。

## Goal 模式接管

- 普通 implementation/review/QA/acceptance checkpoint 改为写报告、state 与 evidence；仅 handoff 条件触发停机。
- attention.md 例外优先：开发前必须确认 branch/worktree；commit 必须 owner 明确同意；禁止自动 commit、`--no-verify` 与未授权 push。
- driver 不得绕过 TDD；行为 step 缺 RED/GREEN/VERIFY 且无 TDD exception 时 implementation gate 不通过。
- 每个 stage/status 变化立即写回 state；complete/passed 与 handoff/blocked 是终态，优先于残留 driver metadata。
- poll/daily/command 只入队，唯一 DeliverySender 执行发送；任何实现若需改变 approved concurrency/runtime contract 必须 handoff，不能临场简化。

## Review / QA 特别门槛

- code review 分开报告 spec 合规与代码质量，两者均 passed 才能进入 QA。
- S23 必须用 PostgreSQL concurrency + barrier fake + fake clock/log 证明 claim/call fence/gate/cancel/rebind/ack/recipient 状态机。
- S24 必须证明 offset 只在 durable terminal outcome 后推进，rollback/commit-unknown 不丢连续后缀。
- S25 必须 table-drive 八值 TelegramError 的 getUpdates/sendMessage 全函数。
- 真机只允许 synthetic fixture；截图裁剪/脱敏；Bot token/Bind token/chat_id/真实客户信息不得入证据。

## Handoff

命中以下任一条件，先写 `stage: handoff` / `status: blocked` / `handoff_reason` / `handoff_next`，再输出：

```text
CS_FEATURE_GOAL_HANDOFF
Reason: <具体阻塞>
Next: <建议动作>
```

- 需要改变 approved design、feature 范围、公开契约、A1～A11 或 roadmap item。
- 独立 reviewer pending/failed/blocked，且没有 owner 明确降级。
- 同一失败项三轮修复仍不通过。
- Telegram 凭证、Docker/PostgreSQL 或真机环境缺失，导致核心行为无法判断。
- owner 主动暂停、改方向或终止。

## Literal Goal Command

```text
/goal "执行 CodeStable feature 目录 .codestable/features/2026-07-15-telegram-digest 下的 goal 执行包。先读取 goal-protocol.md、goal-state.yaml、goal-plan.md、telegram-digest-design.md、telegram-digest-checklist.yaml；这是已由用户确认 design 后的 goal 模式。按 goal-protocol.md 连续执行 cs-feat implementation、cs-code-review、cs-feat QA、cs-feat acceptance；implementation 的代码行为 step 默认用 TDD micro-loop，必须留下 RED/GREEN/VERIFY evidence，不能 TDD 时写 TDD exception 和替代证据；review blocking 时做 review-fix 并重跑 review；QA failed / blocked 时做 qa-fix 并重跑 review 和 QA。只有当 CS_FEATURE_GOAL_COMPLETE 出现在 transcript 中，且 review passed、QA passed、acceptance passed、没有 CS_FEATURE_GOAL_HANDOFF，本 goal 才算完成。"
```
