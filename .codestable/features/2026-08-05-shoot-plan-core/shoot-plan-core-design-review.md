---
doc_type: feature-design-review
feature: 2026-08-05-shoot-plan-core
status: passed
review_state: passed
review_reason: ""
reviewer_id: ""
reviewed: 2026-08-05
round: 11
supersedes_round: 10
review_mode: full-independent
---

# shoot-plan-core feature design 审查报告

> Round 11 完整独立只读复审结论为 `passed`：`blocking=0`、`important=0`、`nit=0`。冻结候选 design SHA-256：`443463f2de22f954b91cdabf69981006b50ba52f52526a010c6be94905aeddea`；checklist SHA-256：`66b4c8577e69c7d8b16d3a9f43982a8a7a711649d80e908d1a7fbc460f302824`，reviewer 确认审查前后 hash 未变且全程只读。Round 10 及此前结论保留为 provenance。

## Round 11 独立复审结论

### Independent Review

- Status：completed；Detection：independent-agent。
- Provider / agent：`/root/core_generation_archive_review`。
- Scope：fresh-install capability bootstrap、`store → planningcapability` 单向 package 依赖与 readiness digest、populated planshare down 的 writer exclusion，以及 Round 10 finding 回归。
- Merge policy：主 agent 已逐条以当前 design/checklist 和 parent roadmap/items 核验；冻结候选 hash 与 reviewer 返回时一致。
- Gate effect：本轮无 blocking / important / nit，允许将当前候选持久化为 `passed`；不替代 Epic 全量 design 的 owner 统一确认。

### findings

- blocking：none。
- important：none。
- nit：none。

### suggestion

- planshare migration 的真实 production ownership 仍属于 `plan-share-collaboration`；core 中跨模块 downgrade 证据在实现时应明确落为 test-only conformance fixture，避免形成第二份生产 migration owner。

### Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Fresh-install capability bootstrap | pass | E | core up migration 同事务 seed 固定 singleton/capability/revision/promoted_at，repeat migrate 不覆盖已提升 marker，缺失或损坏继续 fail closed | implementation 落真实 migration/restore fixture |
| Package dependency and sealed transaction view | pass | E/C | design 冻结 `store → planningcapability`、反向依赖禁止与 same-tx fixed-query closure | depguard / compile-negative 证明 |
| Readiness digest binding | pass | E | digest 输入覆盖 environment/deployment、inventory hash、时间、marker/target、live builds、wiring 与 control-plane provenance | release tooling 产出 canonical digest evidence |
| Populated-down writer exclusion | pass | E/C | transactional migration、固定 SHARE table-lock 顺序及 writer-first/down-first 双连接语义完整 | planshare-owned migration fixture |
| Steps/checks traceability | pass | E | design/checklist 已同步 fresh install、promotion、downgrade 与双连接证据 | execution 保持 pending→done 证据链 |
| Roadmap contract compliance | pass | C | parent roadmap/items 与 core/share owner seam 一致 | share design 继续保持 production owner |

Summary：E=4，C=2，H=0，H-only core checks=`none`。

### residual-risk

- populated-down 的 table lock 可能受线上长事务影响；implementation 必须固定 lock timeout、错误输出与 rollback/runbook，不能无限等待。
- capability promotion 的 fail-closed availability window 仍需 staging/运维演练，静态 design review 不能证明 rollout 可用性。

### Verdict

- Status：`passed`。
- Next：design 保持 `draft`，返回 `cs-epic` 连续 child batch；不进入 implementation、QA、Goal execution 或 Git 操作。

> Round 10 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=1`、`nit=0`、`suggestion=2`。候选 design SHA-256：`53a93ceb36b6d18bd268c1566caca9b3ea8de215553231cca65340f4a54a2489`；checklist SHA-256：`88b2ae5a76e3afe9887465c946c643c00c935519b41be49f59cab64c3f675879`，reviewer 确认审查前后 hash 未变且全程只读。Round 9 及此前结论保留为 provenance。

## Round 10 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R10-001 `PlanningArchiveCapabilityState` 缺 fresh install 的确定性 bootstrap/seed 契约。core up migration 必须在同一migration transaction建立唯一固定初始行，冻结`singleton_key="planning-archive-v1"`、`capability="core-v1"`、initial revision与`promoted_at`语义；重复migrate幂等且不得覆盖已提升marker，非法/重复/unknown marker继续fail closed。S2/A12/check/evidence须覆盖fresh show/startup/tx read、repeat migrate、corrupt/missing以及down/up/reset/restore状态；promoter不得用upsert把损坏marker当首次初始化修复。

### suggestion

- [ ] SUG-R10-001 实现前机械冻结`store → planningcapability`单向package依赖：sealed concrete view在planningcapability，store只注入bound fixed query闭包，planningcapability不得反向import store/pgx，application拿不到generic runner/SQL；加入depguard/compile-negative。
- [ ] SUG-R10-002 readiness digest canonical frame明确绑定environment/deployment identity、inventory schema/content hash、generated/expires time、current singleton state、target、live build set、wiring evidence与control-plane provenance/文件权限或签名信任假设。

### residual-risk

- trusted release inventory真实性、promotion后的短暂fail-closed窗口与sealed view package落点仍需implementation/QA/runbook证据；不影响上述fresh marker必须先闭合。

> Round 9 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=2`、`nit=1`、`suggestion=2`。候选 design SHA-256：`049e883e63f3046897e0b7e7a4e2a2e4f8c902c0fa932ae97a10099869e540bb`；checklist SHA-256：`bb662fa1b9c90ceebc7bbcd4e7537c486767c9cdc16f6a42ed17684893579cba`，reviewer 确认审查前后 hash 未变且全程只读。Round 8 及此前结论保留为 provenance。

## Round 9 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R9-01 `PlanningArchiveCapabilityState` 已冻结 singleton、closed capability、revision CAS 与 mixed-version 顺序，但合法 actor 仍缺可实施接口。须定义从当前 physical transaction 派生且不暴露 raw SQL/Store 的窄 `ArchiveCapabilityTxView.Current`；detail、startup 与 archive callback 复用唯一 decoder/repository，archive callback 必须使用 transaction-bound view。另须给 release-only promoter 固定 package/CLI、相邻单调 CAS、expected revision、readiness、authoritative readback、失败退出与脱敏输出契约，并加入 Required Artifacts、CMD 与 mixed-version command fixture。
- [ ] FDR-R9-02 planshare down 在已有 assignment event/work 时删除 event 表会遗留无法重新关联的 core work，后续 re-up 不能恢复 reciprocal FK。v1 不支持 populated feature downgrade：planshare down 必须在任何 DDL 前检查 assignment source/work，非空则原子 fail closed；fixture 覆盖空库 round-trip、populated down 拒绝且表/约束/generation/event/work 均无部分删除，以及全库 reset/restore 后 re-up。

### nit

- [ ] FDR-R9-03 Route → application coverage 的 `POST /shoot-plans/{id}/transitions` 行须把 archive A12 纳入 Core scenarios，改为 `A3–A5/A10–A12`。

### suggestion

- [ ] FDR-R9-04 固定 marker promotion 与在途 archive transaction 的线性化语义；可选择按 marker read 时刻线性化，或 transaction reader 取共享行锁使 promoter 等待，避免不同 adapter 自行选择。
- [ ] FDR-R9-05 accountless deployment metadata 的极窄例外在 implementation/acceptance 落地后同步至 AccountScope compound；当前 design 阶段不提前改 accepted architecture record。

### residual-risk

- account-wide fence contention、marker promotion 的短暂 fail-closed availability window、caller-owned participant completion proof、reciprocal FK 的永久发布耦合与 Telegram intent 后 at-least-once 仍需实现/QA/production evidence；当前均是设计契约。

> Round 8 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=3`、`nit=2`、`suggestion=1`。候选 design SHA-256：`b0cf4611b2406b4a275a56f0cae7e4b0b0b29a075056356f5439930f8b562795`；checklist SHA-256：`998a67c1aca57ebb130c31da31f78c0a7982a88e38a2fd9bd8b4544cf6899b7b`，reviewer 确认 hash 未变且全程无写入。Round 7 及此前结论保留为 provenance。

## Round 8 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R8-01 `business-lock-before-fence fail closed` 超出了当前 sealed fence API 的机械保证。`FenceTxView → LockedFenceTx` 能阻止未锁 fence 直接 reserve，却不能观察 caller 是否已经通过同一宽 `TxAccountScope` 取得业务行锁。修订必须二选一：引入绑定 physical transaction 的 phase/lock-order tracker 并让所有受支持行锁入口参与；或把承诺收窄为受支持 repository/application 路径由 typed orchestration、depguard、compile fixture 与双连接锁序测试保证，不再声称任意先行业务 SQL 都能 runtime fail closed。
- [ ] FDR-R8-02 `PlanningArchiveCapabilityState` 缺 deployment/account 作用域、schema/CAS、读取时机、promotion 写权限与 mixed-version rolling rollout。须冻结 deployment/schema metadata singleton（或完整 account-scoped 替代）、单调提升约束和发布状态机，覆盖 promotion 前已启动旧进程；只检查新候选启动不足以阻止旧进程按 lower policy 继续 archive。
- [ ] FDR-R8-03 A12 已覆盖 schema/registry、durable capability、participant transaction、composition 与 TTL replay，但 Acceptance Coverage Matrix 只映射 `S1/S5`。须映射到实际 owner `S1/S2/S3/S5/S8`，并列明 migration/composition/transaction fault/mixed-version promotion evidence。

### nit

- [ ] FDR-R8-04 A15 是 core 列表查询上界场景，却在 Coverage Matrix 的 `Core?` 列标为 `no`；改为 `yes`。
- [ ] FDR-R8-05 §2.3/§2.4 人读投影未点名 neutral generation/fence/capability migration 与 archive participant 工作；机械同步 checklist S2/S3，不改变 step 数量。

### suggestion

- [ ] FDR-R8-06 为 planshare-owned reciprocal deferred composite FK 固定 constraint 名、add/drop owner、up/down 顺序以及 core-only/share-enabled schema fixture；planshare 不得删除或重建 core work 表。

### residual-risk

- account-wide fence 的 contention、reciprocal FK migration 耦合、mixed-version capability rollout 与 Telegram at-least-once 仍需真实 PG/双进程/production evidence；当前均只是设计契约。

> Round 7 完整独立只读复审结论为 `changes-requested`：`blocking=1`、`important=7`、`nit=1`、`suggestion=3`。Round 6 `passed` 与此前轮次完整保留为历史 provenance；当前回到 design 修订并继续 fail closed。

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md`
- Checklist: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml`
- Intent / brainstorm: `.codestable/brainstorms/creative-shoot-planning/brainstorm.md`
- Requirement: `.codestable/requirements/creative-shoot-planning.md`
- Roadmap: `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`
- Roadmap items: `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml`
- Prototype: `docs/prototypes/creative-shoot-planning/v2/README.md`、`planning-workspace.html`、`run-mode.html`
- Architecture / decisions: `requirements/CONTEXT.md`、ADR-001～004、AccountScope fail-loud、跨域读模型归展示域、前端工具链 compound
- Code facts checked: `backend/internal/platform/store/scope.go`、`scope_tx.go`、`backend/internal/platform/idempotency/idempotency.go`、migration 0006、order/schedule idempotency callers、`backend/internal/platform/httpapi/router.go`、`backend/cmd/server/main.go`、`frontend/src/App.tsx`、`AppShell.tsx`、`api/client.ts`

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: `/root/creative_roadmap_rereview`
- Raw output: 六轮只读独立审查回传；round 1～3、5 为 `changes-requested`，round 4、6 为 `passed`
- Merge policy: 主 agent 已逐条用 design/checklist、roadmap、prototype 与代码事实核验；只合并有仓库证据的 finding
- Gate effect: round 6 已完整核对 round 5 的 core/share 契约增量与 round 1～4 回归，允许定稿 `passed`

### Review History

| Round | Mode | Result | 主要结论 |
|---|---|---|---|
| 1 | full independent | changes-requested | roadmap schema 漂移、PlanCommand 未闭合、幂等缺 resource frame；另有 replay、placeholder route、Run Mode 验证与 A16 问题 |
| 2 | full independent | changes-requested | 首轮问题基本闭合；发现 ExecuteInScope 共享事务缺口、roadmap projection 字段遗漏、session close/transition payload/旧 wrapper 命令问题 |
| 3 | full independent | changes-requested | 二轮问题闭合；发现 core batch 与 ingestion combined operation/response 混用，以及 resource kind 双重定义 |
| 4 | full independent | passed | 前三轮全部 finding 已关闭；无新 blocking / important |
| 5 | full independent | changes-requested | share 引入 removal guard、planning-share-v1 与 anonymous capability 后，发现 DAG 分工、双视图 capability、archive discovery/policy/TTL 与 negative guard 缺口 |
| 6 | full independent | passed | round 5 的 2 个 blocking、5 个 important 全部关闭；round 1～4 契约无回归 |

## 2. Design Summary

- Goal: 建立可独立存在、AccountScope 隔离的 ShootPlan 核心，支持结构化拍前策划、准备项、公开规模、执行时间窗、Run Mode、append-only 结果/void、双 revision 与 immutable finalization snapshot。
- Key contracts: 12 个 `PlanCommand` variant、5 个 typed transition、7 个当前 core 幂等 operation、plan/Shot/execution-fact 三层 revision、服务端 seq replay、core-only batch 与后续 ingestion combined commit 分离。
- Steps: 8 个 pending step；风险热点集中在 PostgreSQL 并发/事务、通用幂等兼容、event replay、Run Mode 现场可用性与 OpenAPI 双端生成。
- Checks: 12 个 pending check；均已指向 design section / A 场景，不再只用笼统来源。
- Baseline / validation: CMD-001～005；CMD-002 显式覆盖 shootplanning、platform/idempotency、order、schedule、httpapi，另有 PG 双连接、浏览器与 scoped negative guard 证据。

## 3. Findings

### blocking

none

### important

none

### nit

none required for pass

- 可选文字优化：`CommitPlanBatch` 小节把普通入口与 prepared-in-scope 入口称为“同语义”，更精确的表述可在后续无契约文字整理中改为“同一 core mutation 的两种事务执行形态”；当前后文已经明确外层 operation/response ownership，不产生实现歧义。
- R6-N1：最终交付物/挂载点索引未单列 `platform/txcap` 与 `platform/idempotency` extension；详细设计、S3/A2 和 required artifacts 已覆盖，不影响通过，但 implementation inventory 应显式纳入这两个 package。

### suggestion

- 后续 `plan-ingestion-capture` 必须直接引用 `IngestionCommitCanonicalV1` / `IngestionCommitResultV1` 并做逐字段 delta，不得复制后静默演化；任何影响 callback 的新增字段都升级 operation/schema version。
- implementation 可把 operation→constructor→exact frame→stored response 表实现为 platform idempotency 的 table-driven characterization fixture。
- `CreativeBriefPatch` 应使用 presence-aware generated type，明确区分未提交字段与显式 null，不用普通 pointer 猜语义。

### learning

- 幂等协议必须把 operation、resource identity、canonical body 与 stored response 作为一个版本化四元组；只定义 key/hash 不能保证跨资源和跨模块重放安全。
- `ExecuteInScope` 只解决“复用谁的事务”；外层 request/response ownership 仍必须归真正拥有 combined callback 的模块。
- projection-only 字段同样是 roadmap 硬契约；存在 `ShotReadinessLink` 表不自动等于 API 已承诺 `readiness_item_ids[]`。

### praise

- plan revision、Shot execution revision 与 plan execution-fact revision 分离，completion 同时 CAS 两类事实，能避免现场高频写覆盖结构编辑。
- result/void append-only、服务端 seq、latest-effective-event 与 CurrentOutcome 分离、finalization snapshot、reopen/re-complete/stale evidence 构成完整审计链。
- Run Mode 位于 RequireAuth 内、AppShell 外，且只有 2xx 才更新已保存视觉状态；375px/coarse/200% zoom/强光/offline 均进入验收。
- S1 compile harness 与 S5 runtime route registration 分开，明确禁止固定空 2xx、501、panic 与 placeholder handler。

### 已关闭 finding 索引

| ID | 原严重度 | Closure evidence |
|---|---|---|
| R1-B1 | blocking | 完整 roadmap field mapping；required subject、lighting direction、readiness projection、window union/source_ref、session/event fields 进入 A12/checklist |
| R1-B2 | blocking | PlanCommand 12 variant、Transition 5 variant、route→application→A→step 映射与 core batch contract |
| R1-B3 | blocking | versioned canonical frame、resource constructors、operation allowlist、旧 wrapper 兼容与跨资源 fixture |
| R1-I1 | important | latest effective event / CurrentOutcome 纯函数和 cleared/void E1/E2/E3 真值表 |
| R1-I2 | important | runtime route 延后到 S5；stub/501/panic/固定空响应进入清洁度与 route test |
| R1-I3 | important | 200% zoom、强光高对比加入 A14、matrix、checklist、DoD/artifacts |
| R1-I4 | important | A16 改为 request-schema/route/dependency/DOM 固定 allowlist guard |
| R2-B1 | blocking | `ExecuteInScope` 复用同一 claim/replay/store-success 算法，prepare 纯化并事务内重校验 |
| R2-B2 | blocking | required subject、`readiness_item_ids[]`、source/source_ref 与全部 roadmap fields 显式映射 |
| R2-I1 | important | closed_at 只在 complete/archive 服务端派生；原型结束按钮仅导航 |
| R2-I2 | important | Transition generated envelope 与 versioned archive acknowledgement |
| R2-I3 | important | CMD-002 纳入 idempotency/order/schedule/httpapi/shootplanning characterization |
| R3-B1 | blocking | core-only batch 与 ingestion commit 分配不同 operation/canonical/response owner |
| R3-B2 | blocking | Plan/Transition/Batch 等全部引用唯一 constructor 与 exact frame 权威表 |
| R3-I1 | important | CreativeBrief 永远非空；whole null/empty patch 400，逐字段清空保持 empty object |
| R3-I2 | important | test-only account-scoped probe 机械证明 core/probe/ledger rollback，不越界实现 planningmedia |

## 4. User Review Focus

- 用户需要重点拍板：`subject` 按 roadmap 为创建必填；completed 至少有一个当前 Shot；required readiness 未核对不能 ready；ready 被结构编辑破坏 invariant 时自动退 draft；archived 永久只读；页面离开不关闭 RunModeSession，complete/archive 才关闭。
- implement 需要重点遵守：operation/resource/body/response 四元组；PlanCommand/Transition closed union；AccountScope + revision CAS；result/void replay 真值表；core batch 与 ingestion operation ownership；不得提前注册 placeholder route。
- code review / QA / acceptance 需要重点复核：双连接锁序与死锁、同 plan 现场写串行延迟、旧 order/schedule 幂等兼容、probe/shared transaction、A1–A16、200% zoom/强光/offline、A16 scoped negative guard。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design §3.1/§3.3 将 A1–A16 映射到 S1–S8、证据与 CMD | implementation 按矩阵逐项产证 |
| DoD Contract | pass | E | design §3.4 与 checklist `dod` 覆盖 Design/Implementation/Review/QA/Acceptance | 下游不得删减 core command/artifact |
| Steps and checks traceability | pass | E | checklist 8 steps、12 checks 均有 section/A ID 来源与 yes/no exit signal | execution 时保持 pending→done 证据 |
| Roadmap contract compliance | pass | C | roadmap §4/§5 与 design Roadmap 字段映射、closed union、权限/事务语义逐项核对 | CRM/media/ingestion child 复用既有 seam |
| Module interface design | pass | C | application/repository deep module、prepared-in-scope port、operation ownership与代码 AccountScope/idempotency 事实一致 | implementation 验证 deletion/depth 与 probe fixture |
| Validation and artifacts | pass | E | CMD-001～005、evidence_required、cleanliness、A14/A16 证据均明确 | QA 运行真实命令与浏览器动作 |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- capture、void、complete 通过 plan lock 与 plan-level `execution_fact_revision` 串行化同一 plan 的现场写；当前单摄影师低并发假设可接受，但 implementation/QA 必须保留双连接锁顺序、死锁与延迟证据。
- complete/archive 才关闭 session，普通页面离开会留下 open session；后续观测不得把 `closed_at IS NULL` 解释为在线或 duration。
- `IngestionCommitCanonicalV1` 是下游硬 seam，当前 feature 不实现；ingestion design 必须做逐字段核对并复跑 media-only delta/replay fixture。
- test probe 只证明事务机制；真实 planningmedia binding、lease/GC/checksum 由后续 media/ingestion child 复跑 conformance。
- `source=schedule_slot` 当前只有稳定 read type，未来 CRM 写方必须沿用既有 window revision、source_ref 与 session snapshot 规则。
- sealed anonymous capability 当前仍只是设计契约；implementation/acceptance 必须用构造负向测试、跨账号 PG 测试和 depguard 证明 anonymous HTTP/application 不能恢复通用 `AccountScope`。

## 7. Verdict

- Status: passed
- Next: 返回 `cs-epic` 连续 child design batch；design 保持 `draft`，不单独请求用户确认，不进入实现。

## 8. Focused Closure

none；round 4 是实质修订后的完整独立复审，不是 focused closure。

## 9. Round 5 Independent Review（2026-08-05）

- Status：`changes-requested`；reviewer=`/root/creative_roadmap_rereview`；
- Trigger：plan-share-collaboration 对公开 core contract 作了实质增量：`ReadinessRemovalGuard`、`409 readiness_assignment_active`、`planning-share-v1` archive acknowledgement、anonymous `ExecuteInCapability` 复用唯一 ledger 算法；
- Blocking R5-B1：A17把真实planshare claim-vs-remove PG证据压给无share依赖的core，形成DAG前向依赖；已改为core只交fake guard/rollback/wiring contract，真实双连接归share A20。
- Blocking R5-B2：capability存在三套命名且未闭合同一physical tx的ledger/callback双视图；已统一neutral`platform/txcap` canonical types、generic runner、package方向与core test probe/share真实adapter分工。
- Important R5-I1～I5：已补archive policy production fail-closed wiring、detail required acknowledgement discovery、24h TTL内/过期后replay语义、A16结构化唯一例外、PATCH→A17与capability artifact追踪。
- Regression：round1-4 contracts除本轮显式delta外保持；必须round6完整独立复审，不能focused/local closure。

## 10. Round 6 Independent Review（2026-08-05）

- Status：`passed`；reviewer=`/root/creative_roadmap_rereview`；只读完整独立复审，未修改任何文件；
- Closure：round 5 的 R5-B1/R5-B2 与 R5-I1～I5 均已关闭；core 只拥有 fake guard/rollback/composition contract，真实 claim-vs-remove 竞争归 share；canonical `platform/txcap` generic runner、single physical transaction 双视图、archive production fail-closed policy、detail acknowledgement discovery、24h TTL replay/expiry 与 A16 结构化例外均闭合；
- Regression：12 个 PlanCommand、5 个 transition、resource-aware idempotency frame、core/ingestion operation owner、Run Mode、result/void truth table、wrapper compatibility 与 round 1～4 required artifacts 无回归；
- Finding：无 blocking / important；仅保留 R6-N1 的交付物索引文字优化；
- Gate：本 design review 恢复为 `passed`；design 继续保持 `draft` 并返回 `cs-epic`，不单独请求用户确认、不授权实现。

## 11. Round 7 Full Independent Review（2026-08-05）

### Identity and mechanical result

- Reviewer：`/root/core_generation_archive_review`；full-independent、read-only；未修改任何文件。
- Positive closure：0/0 bootstrap 已使用 `INSERT ... ON CONFLICT DO NOTHING` 后 `SELECT ... FOR UPDATE`；share `ShareTxScope` 已可取得bound fence view。
- Verdict：`changes-requested`；blocking=1、important=7、nit=1、suggestion=3。

### Blocking

- [ ] R7-B1 archive participant/DAG ownership 未闭合。
  - Evidence：core禁止实现正式assignment/reminder，却要求真实withdrawal与share/reminder participant rollback；候选没有caller-owned archive participant接口，share又明确archived plan state本身令匿名访问失效。
  - Impact：先行core无法在不越界实现reminder repository的情况下满足checklist；fake与真实表证据归属互相矛盾。
  - Required closure：core定义窄`PlanArchiveReminderParticipant`、named disabled实现与fake/probe rollback；`planning-share-v1`不虚构share participant；真实adapter/current-group withdrawal/whole-tx fault matrix归reminder S3/A9；调整A12/A16/checklist/artifact归属。

### Important

- [ ] R7-I1 core/share/CRM/reminder没有唯一可编译two-phase fence API。须由core neutral package冻结sealed `FenceTxView.LockCurrentAccount() → LockedFenceTx`，locked token拥有`ReserveGeneration/MarkApplied`，并提供跨feature compile/dependency negative。
- [ ] R7-I2 neutral fence没有专属acceptance。须新增A18覆盖并发首次bootstrap、same-value、material reserve、cross-account/tx、三断点rollback、no-gap/orphan和out-of-order applied→contiguous watermark，并映射S2/S3/S8/CMD-002/artifact。
- [ ] R7-I3 multi-plan mutation基数未定义。须选择同outer tx/account fence内按plan ID分配连续per-plan generations，任一plan失败整体rollback，并补2+ plan fixture。
- [ ] R7-I4 archive policy只有升级、无durable downgrade/rollback语义。须引入durable capability或drain gate；reminder→share/share→core不能静默选择lower acknowledgement。
- [ ] R7-I5 core checklist 8个step缺稳定S1–S8 ID，违反`CompleteStep StepId`机器协议。
- [ ] R7-I6 `assignment_kind/offer_kind`与generation work `source_event_id/source_ref`漂移。须统一canonical字段、约束、source event和consumer。
- [ ] R7-I7 focused backend CMD-002未直接覆盖`./cmd/server` composition root，须加入或显式迁移composition tests。

### Nit and suggestions

- [ ] R7-N1 §2.3/Required Artifacts须显式列neutral generation migration/package、`platform/txcap`、idempotency extension及conformance tests。
- [ ] R7-S1 记录脱敏fence lock wait、tx duration、retry/deadlock基线；首版不拆fence。
- [ ] R7-S2 archive effects使用单一typed registry/constructor驱动detail projection与mutation exact-array validator，并保存whole-array JSON golden。
- [ ] R7-S3 冻结versioned `mutation_kind` closed enum与owner边界，core只解释机械work lifecycle，不吸收reminder reducer语义。

### Evidence Confidence Ledger

| Check | Verdict | Class | Required follow-up |
|---|---|---|---|
| Fence bootstrap/no-gap/contiguous | partial | E | A18 + PG fixture |
| Anonymous bound scope | partial-pass | E/C | compile-negative + cross-account + single-tx probe |
| Fence interface conformance | fail | C | canonical interface/package table |
| Archive participant/DAG | fail | C | core fake seam + reminder real adapter分工 |
| Multi-plan cardinality | fail | H | per-plan generation决策 + 2+ plan PG fixture |
| Archive upgrade/TTL | pass | E | implementation golden |
| Archive downgrade | fail | H | durable capability/release gate |
| Exact ordered effects | partial | C | registry/validator + negative whole-array golden |
| Checklist traceability | fail | E | S1–S8 IDs |

### Gate

- Round 7不能恢复`passed`；修复会实质改动core/share/CRM/reminder接口、acceptance、checklist与release policy，必须下一轮完整独立复审。
- Residual risks：account-wide fence contention；OpenAPI数组不自动证明exact tuple；所有capability/participant尚无实现证据；Telegram仍是intent后at-least-once；core work不得向reminder业务语义漂移。
