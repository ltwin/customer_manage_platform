# data-export goal protocol

## 执行 loop

1. 读取 `data-export-design.md`（approved）、`data-export-checklist.yaml`、`data-export-design-review.md`、`goal-plan.md`、`goal-state.yaml`。
2. 确认 owner 已选择当前工作区 feature branch 或新 worktree；未确认则保持 `implementation/ready-to-dispatch` 并停止，不写业务代码、不启动 driver。
3. 进入 `cs-feat` implementation，按 STEP-001～007 完成实现；代码行为默认 RED→GREEN→VERIFY，例外必须写 `TDD exception` 与替代证据。
4. 每完成一个 step，立即在 `goal-state.yaml.ledger` 写 step id、状态、证据/commit 范围；续跑以 ledger + `git log` + 仓库事实为准，不重复完成 step。
5. 运行 CMD-001～004，生成 implementation evidence pack、gate results、DoD results；失败必须 fix-or-block。
6. 把 state 写为 `review/ready`，进入独立 `cs-code-review`；blocking 时写 `review/fixing`，修复后回 `review/ready` 重跑。
7. review 的 spec 合规与代码质量均 passed 后写 `qa/ready`，进入 `cs-feat` QA；QA failed/blocked 写 `qa/fixing`，修复后回 `review/ready`，重跑 review 和 QA。
8. QA passed 后写 `acceptance/ready`，进入 `cs-feat` acceptance；更新 checklist、roadmap/related docs 与最终证据。
9. review、QA、acceptance 全 passed 且无 handoff 后，先写 `stage: complete` / `status: passed`，再打印 `CS_FEATURE_GOAL_COMPLETE`。

## Goal 模式接管

- 普通 implementation/review/QA/acceptance checkpoint 改为写报告、state 与 evidence；仅 handoff 条件触发停机。
- `.codestable/attention.md` 优先：开发前必须确认 branch/worktree；commit 必须 owner 明确同意；禁止自动 commit、`--no-verify` 与未授权 push。
- driver 不得绕过 TDD；行为 step 缺 RED/GREEN/VERIFY 且无 TDD exception 时 implementation gate 不通过。
- 每个 stage/status 变化立即写回 state；complete/passed 与 handoff/blocked 是终态，优先于残留 driver metadata。
- Owner 已批准的 reference-only 边界不可在实现中扩张为媒体包、import/restore、streaming/async 或跨部署头像恢复；需要改变时必须 handoff。
- R3-NIT-001 执行解释：STEP-005 先证明 client/body/download 协作失败时不产出下载；STEP-006 再完成 DataExportCard 的可重试错误呈现、SettingsPage 挂载与浏览器验收，不把该责任重叠当成省略任一证据的许可。

## Review / QA 特别门槛

- code review 必须分开报告 spec 合规与代码质量，两者均 passed 才能进入 QA。
- A2/CMD-002 必须证明 `/export` 引用生成的 `ExportDocument` / `ExportCounts`，required/additionalProperties/minimum 正确，且后端没有重复手写顶层 DTO。
- A8 与 A18 必须分别证明 snapshot 一致性、同 session 的 `repeatable read` / `transaction_read_only=on`，并有 `ReadTxAccountScope` 无写面断言；不能用其中一个替代另一个。
- A6/A7 必须用解析后结构键 allowlist + 唯一哨兵值证明内部头像 generation、凭证与运行状态未导出；不得对备注正文做普通关键词误报扫描。
- A17 必须证明七类表与 settings 各至多一次批量读、无逐实体 query/N+1、counts 不另查 `count(*)`。
- A9 后端 writer 与 A12/A13 前端 body/blob rejection 是 transport 边界的两侧证据：headers 后不得二次渲染，读取失败不得创建 URL 或下载。
- A12 额外覆盖 Content-Type 缺失、语法错误、合法参数、错误 base type、substring 与 `+json`；文件名必须 whole-string 匹配固定 regex。
- A14 浏览器证据使用虚构 fixture，覆盖桌面、375px、键盘、PII 保管与头像不可便携文案；API JSON/evidence 不得包含 owner 真实数据。

## Handoff

命中以下任一条件，先写 `stage: handoff` / `status: blocked` / `handoff_reason` / `handoff_next`，再输出：

```text
CS_FEATURE_GOAL_HANDOFF
Reason: <具体阻塞>
Next: <建议动作>
```

- 需要改变 approved design、reference-only 范围、公开契约、A1～A18 或 roadmap item。
- 独立 reviewer pending/failed/blocked，且没有 owner 明确降级。
- 同一失败项三轮修复仍不通过。
- Docker/PostgreSQL、浏览器或其他核心环境缺失，导致关键行为无法判断。
- owner 主动暂停、改方向或终止。

## Literal Goal Command

```text
/goal "执行 CodeStable feature 目录 .codestable/features/2026-07-21-data-export 下的 goal 执行包。先读取 goal-protocol.md、goal-state.yaml、goal-plan.md、data-export-design.md、data-export-checklist.yaml；这是已由用户确认 design 后的 goal 模式。按 goal-protocol.md 连续执行 cs-feat implementation、cs-code-review、cs-feat QA、cs-feat acceptance；implementation 的代码行为 step 默认用 TDD micro-loop，必须留下 RED/GREEN/VERIFY evidence，不能 TDD 时写 TDD exception 和替代证据；review blocking 时做 review-fix 并重跑 review；QA failed / blocked 时做 qa-fix 并重跑 review 和 QA。只有当 CS_FEATURE_GOAL_COMPLETE 出现在 transcript 中，且 review passed、QA passed、acceptance passed、没有 CS_FEATURE_GOAL_HANDOFF，本 goal 才算完成。"
```
