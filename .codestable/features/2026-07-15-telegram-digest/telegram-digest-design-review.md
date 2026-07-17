---
doc_type: feature-design-review
feature: 2026-07-15-telegram-digest
status: passed
reviewed: 2026-07-15
round: 8
---

# telegram-digest feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md`
- Checklist: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-checklist.yaml`
- Intent / brainstorm: none
- Requirement: `.codestable/requirements/telegram-digest.md`
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` + `photographer-private-crm-items.yaml`
- Architecture / compound: CONTEXT、ADR-001/002/003、AccountScope fail-loud、cross-domain read model、OpenAPI/Telegram 相关沉淀
- Code facts checked: Store AccountScopes/ScopedAccount/TxAccountScope、Settings、Reminder scan/runner、Schedule ListItem、OpenAPI/router/config/server、SettingsPage/client、Makefile/compose

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: `/root/telegram_digest_design_review_r8`
- Raw output: verdict `passed`；0 blocking、0 important、2 nit、1 suggestion；全程只读，未修改文件
- Attempt history: Round 4 首个 reviewer 外部文档查询卡住且未给 verdict，已明确中止；Round 4 retry 与 Round 5～8 均由新的独立只读 agent 完成
- Merge policy: 主 agent 已逐条用 design/checklist/roadmap/代码事实核验；两个 Round 8 nit 已按 reviewer 给出的精确机械边界修正，不改变名词层、编排拓扑或 owner 范围
- Gate effect: Round 8 design-review passed；owner 已于 2026-07-15 接受设计内容与 A1～A11，可把 design 从 draft 切换为 approved

## 2. Design Summary

- Goal: 安全绑定摄影师自己的 Telegram 私聊，按账号时区发送每日经营摘要并支持 `/today`；Telegram 故障不影响 Web、Reminder 或 dashboard。
- Binding: 43-char opaque Bind token、AccountScopeEnumerator scoped match、recipient gate 内原子 ConsumeAndBind、旧 pending ack superseded、Settings 列级 mutation、chat 全局唯一。
- Inbound: getUpdates 按 update_id 串行；durable terminal outcome 后才提高 offset；rollback/commit-unknown 不确认连续后缀。
- Delivery: 单 DeliverySender、128-bit claim identity、30秒 lease、20秒 attempt、call-boundary fence、claim-id/status CAS、1/5/30 failure budget、current-binding recipient、recipient 5分钟退避。
- External error: 八值 TelegramError 与 operation×kind 唯一策略源；无 Retryable 第二真相源。
- Steps / checks: 8 个稳定 step、56 个稳定 check，全部 pending；S1～S30 与 N1～N7 均有证据入口。
- Validation: `make check`、双端 codegen drift、后端定向测试、前端 `test:telegram-digest`、`git diff --check`、synthetic 真 Bot 脱敏证据。

## 3. Finding Resolution History

| Round | Verdict | Findings | Resolution |
|---|---|---|---|
| 1 | changes-requested | 4 blocking、7 important、4 nit | 区分 Bot/Bind token；定义绑定事务 owner、Settings 列级 mutation、scoped chat resolver、durable handling→offset ack、Delivery 冻结语义、稳定 ID、验证命令、synthetic 证据与单 replica 假设。 |
| 2 | changes-requested | 2 blocking、1 important | Bind token 改为完全 opaque/scoped enumeration；`/today` 固定 resolver→EnsureScan→message_kind→claim；invalid_auth 不烧 Delivery budget。 |
| 3 | changes-requested | 1 blocking、1 important、1 nit | TelegramError 改为八值穷尽 operation×kind；S24 直接覆盖 rollback/commit-unknown；synthetic roadmap delta 明示。 |
| 4 | passed | 0 blocking、0 important、2 nit、1 suggestion | 关闭 ScopedAccount/roadmap 状态漂移；owner 接受 current-binding 接收者方向。 |
| 5 | changes-requested | 1 blocking、2 important、1 nit、1 suggestion | 增 account-keyed recipient gate；旧 ack superseded；recipient missing/integrity 5分钟退避、日志限流、账号公平、rebind 唤醒。 |
| 6 | changes-requested | 1 blocking、1 important、1 nit | 增 claim identity/lease/result CAS；唯一 DeliverySender；result 持 gate 写回；gate 可取消/rebind 优先；四个 source+kind 组合固定。 |
| 7 | changes-requested | 1 blocking、1 important、2 nit | 增 call-boundary fence、取消阶段表、2秒 cleanup/repair、attempts=failure budget、Recreate/no-overlap 部署约束。 |
| 8 | passed | 0 blocking、0 important、2 nit、1 suggestion | CHK-007 trace 收敛到 S23；callDeadline 机械化为 `min(attemptDeadline,leaseUntil)-12s`；所有 blocking/important 关闭。 |

## 4. Current Findings

### blocking

none。

### important

none。

### nit

none。Round 8 两项机械 nit 已关闭：CHK-007 直接 trace S23；call-boundary 二次检查使用明确 `callDeadline` 公式。

### suggestion

- implementation 将 claim/result transaction uncertainty 固化为 typed outcome/error，如 `not_committed|committed|commit_unknown|stale`，不得解析 error string；用 PostgreSQL table-driven seam 覆盖。该项不阻塞 design。

### learning

- claim lease 负责 crash recovery；阻止过期 owner 产生 true-external 副作用仍需要 current claim/status fence、剩余时间预算与紧邻外部调用的 context/clock 检查。
- `attempts` 是 consumed failure budget，不是 Telegram 调用总数；取消、结果未知、凭证暂停与 recipient 异常不得被误算成确定失败。
- AccountScope 合规不仅要求 SQL 带 account_id，用来选择 scope 的信息也必须来自认证上下文或服务端可信枚举。

### praise

- cancellation/recovery 已分开 pre-send、send-started、send timeout、result unknown 与 stale lease，避免一个 defer ReleaseClaim 覆盖所有阶段。
- binding/rebind、recipient authorization、attempt reservation、poll offset acknowledgement 四个线性化问题被分别建模，没有用“幂等”一词掩盖。
- roadmap §4.5、主文档条目 9 与 items.yaml 已统一为总 attempt=4、in-progress/feature 指向和 synthetic 脱敏证据。

## 5. Owner Decisions And Downstream Focus

owner 已于 2026-07-15 接受 A1～A11：private chat、空摘要仍发、digest_hour 后补发、`replicas=1 + Recreate/no-overlap`、允许 rebind/无解绑、总 failure budget=4、chat 全局唯一、冻结日期时区、Bind link possession risk、synthetic 真机证据，以及 current-binding recipient/旧 ack supersede/recipient 退避语义。

implement 重点遵守：

- account gate→scope 固定锁序；不得持 DB transaction 跨 Telegram 网络；Settings PATCH 不取得 recipient gate。
- poll/daily/command 只入队；唯一 DeliverySender 执行 claim→gate→fence→send→result CAS。
- `callDeadline=min(attemptDeadline,leaseUntil)-12s`；cleanup/repair 为独立2秒 DB-only context，并计入10秒 shutdown。
- stale writer 不覆盖 sent/failed/superseded/new retry；commit-unknown 先重读/幂等写，不立即重发。
- typed transaction outcome、凭证/URL/正文日志脱敏、AccountScope fail-loud。

code review / QA / acceptance 重点复核：S23 barrier 并发、S24 offset/commit-unknown、S25 error table、跨午夜/时区、单 replica/no-overlap、无 webhook、synthetic 真机证据、cleanup 与 shutdown 实测。

## 6. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | S1～S30、N1～N7 全部映射 step/证据/command；S23～S25 独立覆盖核心状态机 | none |
| DoD Contract | pass | E | 五阶段 DoD、CMD-001～005、Required Artifacts 完整 | implementation/QA 产出真实证据 |
| Steps and checks traceability | pass | E | 8 steps、56 checks、稳定唯一 ID、全 pending；CHK-007/049 直接 trace S23 | none |
| Roadmap contract compliance | pass | C | §4.5、条目9、items、ADR/compound 一致 | acceptance 回写 related requirement/done/stable constraints |
| Module/interface design | pass | C | AccountScope/Settings/Reminder/Schedule/OpenAPI/router/config/server 代码事实支撑 interfaces/seams | typed commit outcome 在实现固化 |
| Validation and artifacts | pass | E | backend/frontend/codegen/full check、barrier fake、PostgreSQL concurrency、synthetic 真机/runbook 均有入口 | 执行时留证 |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 7. Residual Risk

- call-boundary fence 到 Telegram 真正接受请求之间仍有不可原子化的小窗口；Bot API 无调用方 idempotency key，本能力为 at-least-once。
- Telegram 已接受而本地 result CAS/commit 最终无法确认时，lease 后重领可能重复投递。
- 已在 rebind 线性化前提交给 Telegram 的旧 chat 请求，可能因通道延迟在 rebind commit 后才显示。
- 2秒 cleanup 若遇到 shutdown 剩余预算不足只能失败并依赖30秒 lease 自愈；implementation 必须用真实 WaitGroup/lifecycle 测试。
- 进程内 RecipientGate 依赖部署真实遵守 `replicas=1 + Recreate/no-overlap`。
- Telegram update 保留窗口、Bind link possession capability、eventual snapshot 与 Telegram 外部 PII 边界继续成立。

## 8. Verdict

- Status: passed
- Next: owner 已接受设计内容；主 agent 可把 design 标记为 approved，然后进入 GoalPackage。实现开始前必须按 attention 让 owner 选择当前 branch 或 feature worktree。
