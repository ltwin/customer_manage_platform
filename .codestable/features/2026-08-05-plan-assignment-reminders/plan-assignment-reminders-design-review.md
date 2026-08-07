---
doc_type: feature-design-review
feature: 2026-08-05-plan-assignment-reminders
status: passed
review_state: passed
review_reason: ""
reviewer_id: ""
reviewed: 2026-08-05
round: 6
supersedes_round: 5
review_mode: owner-approved-local-only
---

# plan-assignment-reminders feature design 审查报告

> Round 6 完整独立只读复审已完成。冻结候选 design SHA-256：`da54e5957a27127c1f2dffa58ce3f8e9c48ecd5674f934630557d454aabdbec2`；checklist SHA-256：`2df7c355bb75e47f8c635aa53d2b6d91f111b91748160de8bc2abb91831239fc`；结论为 `changes-requested`，`blocking=1`、`important=0`。reviewer 确认 Round 5 的 Delivery identity、route/术语及 atomic calling CAS 方向已关闭，但原 monotonic budget 公式没有扣除 final SQL 到 method return 的耗时。主 agent 已按 finding 修订候选；当前 design SHA-256：`d73d3bee451c2842c055c6f70bb3f05e11ea9cb8284648e9c497777019bc82f7`，checklist SHA-256：`23da6a3af10850488b875f73269a2592c3d935a12cc585dc995d3e0e3ea76cfa`。Owner 已用 confirmation id `1909fda0-52dd-4a74-978a-842de1d7c892` 批准 local-only closure；本轮不增加 Round 7，当前候选收敛为 `passed`。

## Round 6 独立复审与审查上限后本地修订

### Independent Review

- Status：completed；Detection：independent-agent。
- Provider / agent：`/root/media_design_reviewer`。
- Frozen candidate verdict：`changes-requested`；blocking=1、important=0、nit=0。
- Closed regression：Round 5 的同一 business Delivery identity、intent revision supersession、`GET /api/v1/shoot-plans/{id}/assignment-reminders` route、项目术语以及 final guarded CAS 直接创建 `calling` attempt 均已关闭。
- Gate effect：monotonic budget finding 使冻结候选不能通过；之后修订改变物理 Telegram call-start 的授权时间计算，正常应再次完整独立复审。

### blocking

- [x] FDR-R6-B01 原公式仅取 `min(database remaining, 5s - caller elapsed)`，但没有定义 database remaining 的参考时点；final SQL、commit 和 response round-trip 的耗时可能未从业务有效期扣除。
  - Evidence：若 `earliest_valid_until = db_authorized_at + 100ms`，而 final statement 到 method return 消耗 80ms，旧公式仍可能返回 100ms，真实业务余量只剩约 20ms；若耗时 120ms，仍可能错误调用 Telegram。
  - Impact：在 `StartAt` / intent validity 边界后启动物理调用，破坏“过期不发送”的产品和安全契约。
  - Local correction：authorizing method 进入前记录 monotonic `method_started`，final guarded statement 前记录 `before_final_statement`；SQL 返回 `db_authorized_at` 与 `business_remaining_at_authorization = start_deadline - db_authorized_at`。method return 时计算 `technical_remaining = 5s - monotonic_elapsed(method_started, method_returned)`；planning intent 的 `business_remaining = business_remaining_at_authorization - monotonic_elapsed(before_final_statement, method_returned)`，legacy-only 为 `+infinity`；最终 `monotonic_start_budget = max(0, min(technical_remaining, business_remaining))`。
  - Boundary：`before_final_statement` 早于 DB clock 取样，所以保守扣除 final statement、commit 与 response round-trip；`method_started` 扣除全部锁等待并维护完整 5 秒技术上限。`CallStartPermit.MonotonicStartBudget` 以 `method_returned` 为本地倒计时起点，之后 sender 队列等待继续扣减；`StartDeadline` 只作 DB-time 审计。禁止 application wall clock、`time.Until(StartDeadline)` 或 response 后重启 5 秒。
  - Evidence added：A21 覆盖 100ms 业务余量/80ms round-trip 得到 `budget <= 20ms`，以及 120ms round-trip 得到 `budget=0` 且不调用 Telegram。

### Local closure classification

- Delta type：实质发送授权契约修订，不满足 focused closure；主 agent 不能自行把本项改成 `passed`。
- Review cap：owner 已要求同一 feature design 默认不超过 3 轮完整复审；本 feature 已超过该上限，因此不自动增加 Round 7。
- Owner approval：Option A 已批准上述 local-only closure，approval ref 为 `approval-report.md#review-round-cap-local-closure`，confirmation id 为 `1909fda0-52dd-4a74-978a-842de1d7c892`；本 feature 不再增加 Round 7。

### residual-risk

- 数据库 final CAS 与真正 Telegram network call 无法原子化；当前预算只把不可消除窗口压缩为 method return 后的本地 monotonic 倒计时，仍需 sender-boundary fixture 和延迟注入证明。
- monotonic clock 的进程内语义、method return 后排队扣减以及 0-budget fast-fail 需要实现期单元/集成测试；静态 design 不能证明 scheduler 和 transport adapter 严格消费 permit。
- Telegram response-loss duplicate、跨账号 quarantine 与未来 package 组合仍按既有 residual risk 处理。

### Verdict

- Status：`passed`。
- Review state：`passed`；lane=`owner-approved-local-only`。
- Next：design 保持 `draft`，返回 `cs-epic` 连续 child batch；不启动 Round 7，不进入 implementation、QA、Goal execution 或 Git 操作。

> Round 5 完整独立只读复审结论为 `changes-requested`：`blocking=1`、`important=1`、`nit=2`。候选 design SHA-256：`bbbe332e0f9078c89d521baaa8c8082b562e7e2d04a02a950cb74d337929df6`；checklist SHA-256：`a0566d2319b9267122327a948345cdb91505d688ee7cb70a2492d1036855c801`，reviewer 确认审查前后 hash 未变且全程只读。Round 4 及此前结论保留为 provenance。

## Round 5 独立复审 findings

### blocking

- [ ] FDR-R5-B01 固定5秒attempt-start permit仍允许guard在StartAt前极短通过、sender在StartAt后但5秒内开始调用；这是可消除的授权窗口，不是数据库到Telegram不可原子的微窗口。planning permit的start deadline必须至少cap到`earliest_valid_until`，本地monotonic预算从授权请求前开始或扣除往返；`authorized→calling`最终DB transition须校验active intent、current binding/status/freshness与DB clock并定义CAS/失败语义。补guard-before-boundary→delayed-call、commit-response pause、mark-calling pause/rebind fixture。

### important

- [ ] FDR-R5-I01 material rebind应保留同一业务Delivery identity/source key，只supersede旧active intent revision并释放/唤醒该Delivery；不得把业务Delivery本身supersede/re-enqueue。只有既有pending `binding_ack`按已通过契约superseded。同步D8/D14/send flow/A23-A24/roadmap/items。

### nit

- [ ] FDR-R5-N01 plan reminder公开route path parameter统一为roadmap/core路径族使用的`{id}`，不要单独写`{planId}`。
- [ ] FDR-R5-N02 清理设计正文中项目禁词“用户”，改为摄影师账号、界面状态或账号所有者动作。

### residual-risk

- 即使deadline/CAS闭合，最终DB transition后到Telegram真正call-start仍有不可原子微窗口；response-loss duplicate、account-wide quarantine与未来package组合继续需要实现/QA证据。

> Round 4 完整独立只读复审结论为 `changes-requested`：`blocking=1`、`important=3`、`nit=1`、`suggestion=1`。候选 design SHA-256：`380c1f0780b709d8a737eed8e10543fc9b63e3c985f7c4039f618315c5bcf9d6`；checklist SHA-256：`d5b2dbcf8008ec628c90bb2c4ce6c80042c4160bb9cb3e69bdb450ea727be734`，reviewer 确认审查前后 hash 未变且全程只读。Round 3 及此前结论保留为 provenance。

## Round 4 独立复审 findings

### blocking

- [ ] FDR-R4-B01 final `clock_timestamp()` guard 只阻止 StartAt 后创建 intent，已提交 intent 的首次外部调用或 retry 仍可在 StartAt 后发送旧 planning 清单。若维持“StartAt 后绝不发送”的产品承诺，intent 必须保存可验证的 temporal 上界，每次物理 Telegram attempt 在 call boundary 用数据库 `clock_timestamp()` 重检；过期时不得调用 Telegram，由 Delivery 唯一状态机 supersede/cancel 并处理混合 legacy+planning digest。增加 pre-call crash、transient retry、response-loss duplicate 与 mixed digest 跨界 fixture；仅 Telegram 调用紧邻处不可原子消除的微窗口列 residual risk。

### important

- [ ] FDR-R4-I01 immutable intent 冻结旧 recipient/payload，静默改变已通过 Telegram digest 的 current-recipient 与 retry-freshness 契约。默认保持既有安全语义：material rebind/unbind 在共同 fence→Settings→Delivery→reminder 锁序下 supersede 尚未开始或需 retry 的旧-recipient intent/Delivery，并用 current recipient/fresh projection 重新 enqueue；若要反向选择，必须作为 requirement、passed design、roadmap 与 owner approval 的显式 supersession。补 pre-call crash/rebind、transient failure/rebind、unbind 与 mixed digest fixture。
- [ ] FDR-R4-I02 A23/checklist 强制验收 unbind，但当前系统和本 design 没有 command/endpoint。若本 feature 不新增 unbind，则当前 DoD 只覆盖现有 bind/rebind，保留 future rule 但删除不可执行验收；若要交付 unbind，须补完整 Bearer surface、授权、幂等、重复 unbind、Settings/UI projection、Delivery/intent 处置、错误与证据。
- [ ] FDR-R4-I03 `AssignmentReminderSourceReader` 同时 claim core work、读取 planshare source、物化 reminder epoch，跨越三位 owner。须拆为 core-owned generation work claim/view、planshare-owned safe source reader/plan-ID enumerator、reminder-owned epoch repository/materializer，由 reminder coordinator 在自身事务中组合；同步 typed interface、S2、check 与 depguard evidence。

### nit

- [ ] FDR-R4-N01 CMD-002 加入 `./internal/platform/store/... ./internal/dataexport/...`，覆盖本 feature 的 migration、Settings additive column、cascade 与 export/redaction。

### suggestion

- [ ] FDR-R4-S01 增加 old temporal marker 提交后同 slot 被重新排到 future StartAt 的双连接 fixture；旧 work 只结算 matching old validity，不得撤回新 group，并以 universal resolution 推进 watermark。

### residual-risk

- Telegram 外部调用与数据库不能原子提交，response-loss duplicate 仍为 at-least-once；account-wide quarantine、未来 package 组合、PG 多实例 runner 和跨 feature 候选状态仍需实现/QA证据。

> Round 3 完整独立只读复审结论为 `changes-requested`：`blocking=1`、`important=3`、`nit=1`、`suggestion=1`。候选 design SHA-256：`35922ddb3d847571a850d3af3f4c47d7cc02769ebf29e808d39089b99d247b66`；checklist SHA-256：`3bca57dca2aa5e4b5a610cd53dd40d35abd917fefd25252f57dbb96b0660f27a`，reviewer 确认 hash 未变且全程无写入。Round 2 及此前结论保留为 provenance。

## Round 3 独立复审 findings

### blocking

- [ ] FDR-R3-B01 temporal 校验使用 PostgreSQL transaction-start time，却把 intent commit 当发送线性化点。事务可在 `StartAt` 前启动并在 fence/delivery/recipient 锁等待或 payload 构建中跨界，旧 `transaction_timestamp()` 仍会放行过期内容。须在取得全部锁后用权威当前 DB wall clock（例如 `clock_timestamp()`）在最终 guarded snapshot/intent SQL 中即时重检最早 `valid_until`，并分别冻结 source/status/recipient 与 temporal decision 的线性化点；A21 增加 fence、delivery、recipient、payload 四种跨界 fixture。

### important

- [ ] FDR-R3-I01 `recipient_binding_revision` 尚无权威字段；现有 rebind 为 bind token→settings→deliveries，sender 为 fence→delivery→settings，跨实例可死锁且进程内 `RecipientGate` 不能承担正确性。须定义 Settings additive revision 或独立 binding row、bind/rebind/unbind 单调递增、sender/rebind 共用完整锁序与 zero-generation account fence 语义，并补双连接 fixture。
- [ ] FDR-R3-I02 `PlanAssignmentReminderInbox` 强制 assignment event identity，却要求为所有 pending CRM/settings/archive/temporal work 写 `rebuild_verified` Inbox。须按 mutation kind 分派 settlement：assignment 用 Inbox；非 assignment 使用 `(account_id,generation)` 通用 resolution/evidence 或 work resolution 字段；settings/time/CRM/archive 分别冻结 applied/repair/invariant-corruption 规则，同步 D6/A7/S2/checklist/artifact。
- [ ] FDR-R3-I03 不同 IANA timezone 但同 local date/offset 时，fingerprint+valid_until 相同却 `timezone_snapshot` 变化，和 zero-write no-op 冲突。须明确 projection equality 包含 `timezone_snapshot`（或把它改为 immutable activation metadata）；只 metadata 变化保留 occurrence/status并递增 metadata/validity revision，A20 增加 different-zone/same-date fixture。

### nit

- [ ] FDR-R3-N01 CMD-002 加入 `./internal/settings/...`，覆盖本 feature 对 Settings transaction/binding/revision 的实质改造。

### suggestion

- [ ] FDR-R3-S01 明确既有 Delivery 的 frozen `target_local_date + timezone_at_enqueue` 继续拥有 delivery window；planning projection 使用当前已提交 timezone。补 enqueue 后 timezone 变化、intent 前/retry fixture；如要 supersede 旧 Delivery，应另立契约。

### residual-risk

- account-wide quarantine、Telegram response-loss at-least-once、未来 package 尚未实现仍需后续真实 PG/网络/组合证据；不以 prose 视为已生产成立。

> Round 2 完整独立只读复审结论为 `changes-requested`：`blocking=2`、`important=3`、`nit=1`、`suggestion=1`。round 1 findings 与下方原始报告完整保留为 provenance；当前回到 design 修订，继续 fail closed，不授权统一确认或实现。

## 1. Scope And Inputs

- Design：`.codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-design.md`
- Checklist：`.codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-checklist.yaml`
- Intent / brainstorm：由 parent creative-shoot-planning brainstorm、requirement 与 roadmap 承接；本目录无独立 intent
- Roadmap：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md` 与 items.yaml
- Related docs：creative-shoot-planning requirement、passed core/CRM/share designs、tracked v2 prototype、AccountScope/跨域 read-model compound
- Code facts checked：既有 reminder model/service/repository/runner、digest snapshot/daily/sender/composition、schedule/order transaction seams、OpenAPI Reminder 与 Reminders/Dashboard UI

### Independent Review

- Status：completed
- Detection：independent-agent
- Provider / agent：`/root/media_design_reviewer`
- Raw output：完整、独立、只读审查；`blocking=2`、`important=1`、`nit=1`、`suggestion=1`；reviewer 未修改 design、checklist 或其他文件
- Merge policy：主 agent 已逐条以 roadmap、passed upstream designs、prototype 与代码事实核验；以下 finding 均有直接仓库证据
- Gate effect：存在 blocking 与未关闭 important，round 1 不能通过；必须回 design 修订并重新完整独立复审

## 2. Design Summary

- Goal：把 formal active readiness assignment 按未来 shoot slot 投影为只面向摄影师账号 owner 的可重算提醒
- Key contracts：稳定 source、date-only due、immutable group fingerprint、新事实新 reminder 实例、event + participant + reconciliation、digest freshness fail-closed
- Steps：6；风险热点为跨实例 source 收敛、CRM 全局锁序、terminal no-resurrection 与 recipient 负向边界
- Checks：20；均为 pending 并指向 roadmap/design/A 场景
- Baseline / validation：YAML 与 items 可解析；placeholder 扫描通过；本轮发现 archive acknowledgement、freshness/linearization 与 corrupt recovery 三个实质契约缺口

## 3. Findings

### blocking

- [ ] FDR-B01 `plan-assignment-reminders-design.md §2.2/A9；shoot-plan-core-design.md archive policy` 归档新增了撤回 assignment reminder 的真实副作用，却复用已冻结的 `planning-share-v1` acknowledgement。
  - Evidence：候选要求 plan archive 撤回 current reminder groups；上游 core 已把 `planning-share-v1` 的 exact effects 冻结为 plan read-only、execution history retained、active share links unavailable、share feedback retained、share assignments retained，并明确后续 reminder 副作用必须新增下一 version。
  - Impact：服务端若按候选执行，会超出用户提交的 exact acknowledgement、OpenAPI closed union 与 canonical idempotency body；若不执行，则 A9/DoD 无法成立。
  - Expected fix scope：新增 additive `planning-share-reminder-v1`，其 exact effects 在 `planning-share-v1` 全集上增加 `active_assignment_reminders_withdrawn`；同步 roadmap、core/reminder design 与 checklist、policy/version registry、detail projection、OpenAPI/generated Go/TS、前端 exact-effect 确认、transition canonical/replay、24 小时 ledger TTL 与 archive whole-transaction rollback；受影响 core、roadmap、reminder 均须复审。

- [ ] FDR-B02 `plan-assignment-reminders-design.md D7/§2.2/A15；ShareAssignmentSourceEventV1；backend/internal/reminder/digest/*` freshness 没有数据库可证明的提交前沿，sender 也没有跨实例发送决定的线性化协议。
  - Evidence：候选只使用 `time.Time cutoff/source_cutoff` 与“查询未消费事件直到为空”；上游 source envelope 没有 account-scoped commit sequence/high-water mark；reconciliation checklist 声称稳定 epoch，但 design 未定义 epoch token/snapshot boundary；现有 sender 的最终 EXISTS 与进程内 mutex 不能串行化多实例 sender、revoke、CRM lifecycle 与 archive writer。
  - Impact：并发 commit 可能在 drain/ensure 边界漏入，跨页 scan 可能漏计划，revoke/archive 也可能在 final validation 与 Telegram call 之间提交，击穿“不发送已撤销清单”的核心承诺。
  - Expected fix scope：冻结 account-scoped monotonic generation/fence，所有 assignment/slot/order/plan/time mutation 在同一 DB fence 下推进；source event 带 generation，consumer 维护 applied generation；reconciliation 物化 durable stable epoch/work rows；sender 在共享 fence 内捕获 target、ensure、构建并提交 immutable send intent/payload，再做外部调用，明确 send-intent-before-revoke 与 revoke-before-send-intent 的线性化语义；同步 share/reminder design/checklist并完整复审。

### important

- [ ] FDR-I01 `plan-assignment-reminders-design.md D4/assignment event consume/A6` “标记 corrupt 后整个事务回滚”无法留下 durable 标记，也没有安全恢复协议。
  - Evidence：schema 只有 inbox 与 reconcile state；projection transaction 回滚会同时回滚 corrupt marker，不可变坏事件会持续未消费并被多实例热重试。
  - Impact：账号 freshness 可永久 fail closed、后台持续重试与重复日志；若实现者静默 ack/skip，又会绕过安全承诺。
  - Expected fix scope：新增脱敏 durable quarantine；projection rollback 后独立事务持久化、lease/backoff/health surface；未解决 quarantine 使相关 account/plan freshness fail closed；只允许 safe current assignment reader 驱动受控 rebuild/verify，repair transaction 原子推进 inbox/watermark 并 resolve，禁止静默 skip。

### nit

- [ ] FDR-N01 `PlanAssignmentReminderView.groups[].status` 同时可能被理解为 group lifecycle 与 Reminder 用户状态；改为 `reminder_status: pending | done | dismissed`，历史 group lifecycle 若将来公开则另用 `group_state`。

### suggestion

- [ ] FDR-S01 冻结 exact current-group 数据库约束：至少 `UNIQUE(account_id, group_fingerprint)`，并对 logical bucket `(account_id, plan_id, slot_id, due_date, lead_rule_version)` 的 `state=current` 建 partial unique/统一锁序；补漏锁与双实例 negative fixture。

### learning

- immutable reminder instance 与 terminal no-resurrection 是正确建模边界：同 fingerprint 保留 pending/done/dismissed，material change 新建实例而不是覆盖旧 Reminder。

### praise

- formal readiness eligibility、date-only due、DST 日历日规则与 account-owner-only recipient 的正负边界清楚；`Order.shot_at`、customer delivery、price/business draft 均被明确排除。
- projection module 的 due、fingerprint、desired-group diff、withdraw/terminal policy 与 typed ports 具有足够 interface depth；6 steps、20 checks、A1-A18 的基础 traceability 良好。

## 4. User Review Focus

- 当前不进入 owner review；先关闭 archive acknowledgement、generation/epoch/send-intent 与 durable quarantine。
- epic child batch 内本 feature 修订并通过复审后仍保持 design `draft`，不单项确认、不进入实现。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | fail | E/C | A1-A18 可追踪，但 A6/A9/A15 缺 durable recovery、versioned archive 与 commit frontier | round 2 核对新增矩阵 |
| DoD Contract | warn | E | 五阶段 DoD 与命令已落盘，关键并发/恢复证据尚未冻结 | 同步 required artifacts |
| Steps and checks traceability | pass | E | 6 steps/20 checks 均 pending 且来源基本完整 | 修订后保持 exact source |
| Roadmap contract compliance | fail | E/C | reminder archive 副作用与 frozen `planning-share-v1` 冲突 | parent/core 一起修订 |
| Module interface design | warn | E/C | projection seam 成立，但 generation/fence/epoch/send-intent/quarantine interfaces 不完整 | round 2 核对 depth |
| Validation and artifacts | warn | E/C | YAML/placeholder 通过，关键双实例与恢复 fixture 不足 | 补 artifact 与命令映射 |

Summary：E=3，C=3，H=0，H-only core checks=none。

## 6. Residual Risk

- Telegram 外部投递仍是 at-least-once：Telegram 接收成功但本地 finalize 前崩溃可能重复投递。该风险与“发送 stale revoked content”不同；本轮只要求用 immutable send intent 固定合法内容与线性化顺序，不虚构外部 exactly-once。

## 7. Verdict

- Status：changes-requested
- Next：回 `cs-feat` design 阶段同步 parent/core/share/reminder 契约，校验后启动 round 2 完整独立 design review；不得进入用户确认或实现。

## 8. Focused Closure

none

## 9. Round 2 Full Independent Review

### Review identity and mechanical checks

- Reviewer：`/root/media_design_reviewer`
- Mode：full-independent、read-only
- Candidate design SHA-256：`8834e791cdfc877e38920dbce1e6b48c71592ab608189213f8c3718669015fc2`
- Candidate checklist SHA-256：`c586ba8af886db51f4934d7103c636af29d0758b2df7d2af341bbf84a0bbf48b`
- Reviewer write audit：未修改任何文件；上述 SHA 与 reviewer 启动前主代理基线一致
- Mechanical result：frontmatter/feature/roadmap item 合法；checklist YAML 可解析（6 steps、22 checks、6 commands、23 evidence artifacts）；placeholder 扫描仅命中“禁止 TODO/FIXME”的规则文本；`git diff --check` 无输出
- Closed from round 1：additive `planning-share-reminder-v1`、durable quarantine/repair、stable reconciliation epoch completion、`reminder_status` 与 current logical-bucket partial unique 均已被 reviewer 认可；刚补充的 epoch-plan `rebuild_verified`/work settlement 不再列 finding

### Blocking findings

- [ ] FDR-B01 material fingerprint 被错误地同时当作 current-state 比较值与永久 group/reminder instance identity。
  - Evidence：candidate 以 material facts 计算 `group_fingerprint`，同时要求 `UNIQUE(account_id,group_fingerprint)`、Reminder dedup 只由该 fingerprint 导出、group 只可 current→withdrawn 且不可复活。
  - Counterexample：slot `shoot→non-shoot→shoot`、改期再改回、unlink/relink 或 timezone 改走再改回会让同一 material fingerprint 合法复现；旧实例不能复活，全历史 unique 又禁止新实例。
  - Impact：合法 recurrence 永久漏提醒，或被迫违反 terminal no-resurrection。
  - Required closure：拆分 repeatable material-state fingerprint 与 immutable occurrence identity；只对 current exact fingerprint no-op；withdrawn 后同 fingerprint 分配新 group/reminder identity；保留 current logical-bucket partial unique；补 pending/done/dismissed recurrence、slot/date/link 往返与双实例 PG fixture。

- [ ] FDR-B02 generation frontier 没有覆盖 reducer 的全部可变/外生输入：account timezone 与 `now`。
  - Evidence：due 使用 `LocalDate(StartAt, account.timezone)`，但 Settings timezone PATCH 当前只是 Get→Upsert，未进入 fence/work；`StartAt>now` 依赖 runner 事后生成 `shoot_started`，final read/send 只校验 target/applied，不校验持久 temporal validity。
  - Counterexample：timezone 跨日期修改后 target 不变仍读旧 due；runner 延迟/崩溃且时间越过 StartAt 后 target 仍不变，已开拍 group 可进入 read/digest。
  - Impact：watermark 被宣称 fresh 时仍可能显示/发送错误日期或拍后清单。
  - Required closure：material timezone patch 与 settings write、account fence、durable account-wide per-plan work同一事务，same-zone不推进；group持久化 temporal validity，最终 read/send 在 fence 下对 DB time fail closed并幂等安排 durable withdrawal，不能依赖 runner 先创造frontier；同步 roadmap/items/settings seam；补 timezone rollback/restart与StartAt双实例/read/send fixture。

### Important findings

- [ ] FDR-I01 canonical typed fence API 只有 `ReserveGeneration`，无法表达必须先于业务锁的 `LockCurrentAccount`。
  - Required closure：core-owned sealed `FenceTxView` + `LockCurrentAccount` 返回 same-tx/current-account opaque locked token；`ReserveGeneration` 只接受该token并在locked recheck后调用；wrong-order、cross-tx/account、duplicate reserve、same-value no-reserve negative fixture。

- [ ] FDR-I02 单 plan generation work 与 multi-plan order cancel/delete/slot move 不一致。
  - Required closure：冻结同一 outer transaction、同一account fence内按 `plan_id ASC` 连续分配 per-plan generation/work；任一plan失败整体rollback，watermark不越过缺口；补2+ plans、old/new order move、second-plan failure与restart fixture。

- [ ] FDR-I03 send-intent 线性化未覆盖 planning Reminder done/dismiss 与 recipient/callability。
  - Evidence：现有 MarkDone/Dismiss 是无fence直接 CAS；sender当前先Build，再 ReloadClaim/ResolveCurrent/AssertCallableClaim。
  - Required closure：仅新 planning reminder 的用户terminal动作与自动dismiss共享account fence/row lock，legacy保持零回归；首次intent只在同一 final tx内确认current delivery claim、current callable recipient和fresh projection后提交；无recipient不得冻结payload；明确 intent→Telegram→Delivery finalize 顺序，补status-vs-intent与missing→revoke→bind fixture。

### Nit and suggestion

- [ ] FDR-N01 `PlanAssignmentReminderDigestIntent` 不应与既有 `Delivery` 重复拥有 ready/sent/retryable 状态；intent保持immutable payload-only，Delivery独占claim/retry/finalize/status authority，或必须给出完整双状态恢复不变量。
- [ ] FDR-S01 冻结 intent payload 数据生命周期：account-scoped FK、长度上限、retry/recovery horizon、terminal cleanup/正文清除、account export/delete与日志脱敏；重试窗口内不得丢失exact payload。

### Evidence Confidence Ledger

| Check | Verdict | Class | Basis / follow-up |
|---|---|---|---|
| Acceptance Coverage Matrix | fail | C | A1-A18 缺 recurrence/timezone/delayed-runner/status/recipient/multi-plan反例；须新增可证伪fixture |
| DoD Contract | warn | E | 五层DoD、6 commands、23 artifacts存在；required evidence须补新边界 |
| Steps/checks traceability | warn | E | 6/22均pending且有source，但部分check固化错误global unique并遗漏frontier输入 |
| Roadmap contract compliance | warn | C | 产品/隐私主边界一致；roadmap §4.9本身遗漏timezone/status frontier，必须parent同步 |
| Module interface design | fail | C | lock seam、multi-plan cardinality与send decision未闭合 |
| Validation/artifacts | warn | E | YAML/placeholder/diff-check通过；需新增真实PG/network并发证据 |

Summary：`E=3`、`C=3`、`H=0`；H-only core checks=`none`。

### Round 2 verdict and gate

- Verdict：`changes-requested`
- Counts：blocking=2、important=3、nit=1、suggestion=1
- Residual risk：低 generation quarantine 仍会阻塞全账号freshness，需health/alert/backoff/repair SLO；Telegram response loss仍可能重复同intent，不能宣称exactly-once；core/share/CRM/roadmap独立复审gate仍未全部放行。
- Next：回 design 修订；由于schema identity、frontier、typed port与send protocol均为实质变化，修后必须启动下一轮完整独立复审，而不是用主代理自查或focused closure替代。
