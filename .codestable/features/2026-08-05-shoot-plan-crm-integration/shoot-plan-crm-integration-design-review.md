---
doc_type: feature-design-review
feature: 2026-08-05-shoot-plan-crm-integration
status: passed
review_state: passed
review_reason: ""
reviewer_id: ""
reviewed: 2026-08-05
round: 6
supersedes_round: 5
review_mode: owner-approved-local-only
---

# shoot-plan-crm-integration feature design 审查报告

> Round 6 终局完整独立复审原候选 verdict 为 `changes-requested`：`blocking=1`、`important=4`、`nit=0`。Owner 随后以 Option A 明确批准一次性本地修订，confirmation id=`42fab1d9-3f1c-4106-9d99-44efa7172909`；修订后又以confirmation id=`ff30ebbe-5282-4e87-be21-7bca4669b166`确认最终local-only closure。全部 finding 已关闭；没有启动 Round 7，也不把本地核验伪装成新独立复审。当前Round 6按owner-approved local-only模式为`passed`。

## Round 6 owner-approved local-only 闭合候选

### 授权、冻结基线与当前候选

- Round 6 independent reviewer：`/root/crm_terminal_design_review`；原结果=`changes-requested`，blocking=1、important=4、nit=0、suggestion=1。
- Round 6 frozen design SHA-256：`2be0f20e471cf19c7db28760c457ea47d059928e0a506b0e335fc13a27db44a7`；checklist SHA-256：`376c8974290d9b43febf956ccabd1f3a199b16310c1d3a2557ffb77d982960b7`。
- Local revision authorization：`.codestable/roadmap/creative-shoot-planning/approval-report.md#crm-terminal-review-blocking-disposition`，status=`approved`，confirmation id=`42fab1d9-3f1c-4106-9d99-44efa7172909`。
- Final local-only acceptance：`.codestable/roadmap/creative-shoot-planning/approval-report.md#crm-local-revision-final-acceptance`，status=`approved`，confirmation id=`ff30ebbe-5282-4e87-be21-7bca4669b166`；精确绑定下列四份candidate hash。
- Current CRM design SHA-256：`f9feb4a87fe912ebe67f5d4bd88d3452941379389efe4d8120c3cb1e23a33b7c`；checklist SHA-256：`2c15c5b5739a3e2d04964d989f10649ef5cf339e374925411790ce70f6d4ed4c`。
- Necessary reminder sync SHA-256：design=`82d0c11636416c45d6a525498071fe3af60d80955633cb9307096e07a53cde1d`；checklist=`282367b0f8048c46f013e8f25ead6711991bb25524474866cb30b402ad0065fe`。
- Review lane：owner-approved local-only；主 agent只做契约核验、反向扫描与机械验证；Round保持6，不启动Round 7。

### Findings closure

- [x] FDR-R6-B01 revision/CAS owner 已闭合。
  - Closure：`PlanCRMConnection.connection_revision`与`PlanScheduleProjection.projection_revision`分别由各自行唯一拥有；plan创建/首次projection初始化、connection-only/projection-only/both/window-only/no-op推进规则及event after correlation均有真值表。adopt只CAS当前projection，不匹配稳定返回`409 projection_revision_conflict`且0写；A10/A12与checklist新增concurrent-adopt/revision-owner fixture。
- [x] FDR-R6-I01 command状态转换已闭合。
  - Closure：independent direct `link_order`从order权威customer派生；same customer成功、different customer/order分别稳定409。`order_deleted→unlink_customer`转independent，清current epoch/snapshot与projection source、置`inactive_unlinked`/suppression=false，delete/unlink event均保留被清snapshot；relink必须新epoch。A1/A3/A7与专门truth-table evidence同步。
- [x] FDR-R6-I02 PlanningSummary exact semantics 已闭合。
  - Closure：customer/order/schedule current membership、archived排除、completed计数、active集合、primary排序、primary-owned window/warning及zero/archived-only absent均已冻结；0/10/100 IDs必须同时断言query `0/1/1`与exact DTO values。
- [x] FDR-R6-I03 CRM→Reminder compile seam 已闭合。
  - Closure：CRM package拥有sealed `CRMReminderLifecycleFactV1`三variant及单一participant；link/unlink/relink、order cancel/delete、schedule lifecycle映射exact fact。Reminder只实现adapter，CRM不import reminder；archive保留core-owned独立participant。每plan generation、matching lifecycle resolution、MarkApplied、disabled/real wiring、compile-negative与whole-transaction fixture已同步到两份design/checklist。
- [x] FDR-R6-I04 tracked v2 conformance 已闭合。
  - Closure：CRM design与checklist直接引用`docs/prototypes/creative-shoot-planning/v2/README.md`和`docs/prototypes/creative-shoot-planning/v2/planning-workspace.html`；matrix覆盖CRM card层级、customer/order/slot、projection/manual来源、unlink的full立即失效/提醒撤回/订单档期无副作用、cancelled/deleted/suppressed/adopt。原型未覆盖的三域summary明确由child exact contract拥有。
- FDR-R6-S01 仍是非阻塞实现切片建议；当前S2/S3已分别承接revision/reducer与closed fact/whole-tx证据，不额外改变feature范围或step数量。

### Local Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1/A3/A7/A9/A10/A12/A13/A15与S2-S5逐项承接五个finding | owner final acceptance |
| DoD Contract | pass | E | Design DoD明确local-only lane；Implementation/Review/QA列revision、union、exact summary、prototype证据 | 实现期执行 |
| Steps and checks traceability | pass | E | CRM 7 steps/16 checks/23 evidence；Reminder 6 steps/26 checks/42 evidence，新增项均有source | 实现期留证 |
| Roadmap contract compliance | pass | E/C | 不改parent产品/DAG/gate；补齐tracked v2与既有generation/archive边界 | parent Round 9保持有效 |
| Module interface design | pass | E/C | revision owner唯一；CRM-owned sealed union、Reminder adapter、core archive seam依赖方向闭合 | compile/depguard验证 |
| Validation and artifacts | pass | E | checklist YAML、frontmatter、fence、placeholder、乱码/冲突/尾空白与反向扫描可机械复跑 | 最终checkpoint前重跑 |

Summary：E=4，C=2，H=0，H-only core checks=`none`。

### Candidate Verdict And Final Gate

- Final local verdict：`passed`；unresolved blocking=0、important=0、nit=0。
- Persisted workflow status：`passed`；Round仍为6，review mode仍为`owner-approved-local-only`，没有Round 7。
- Next：交回`cs-epic`继续child design batch；design保持`draft`，等待全部child designs统一确认，不进入implementation。
- Non-automatic：本checkpoint不批准实现、QA/acceptance、Goal execution、stage-1/2 evidence、AI/provider、凭证、Git、PR、merge、发布或部署。

> 以下 §1～§8 保留 Round 5 及此前的完整审查 provenance；当前权威是上述Round 6 independent finding与owner-approved local closure candidate。

## 1. Scope And Inputs

- Design：`.codestable/features/2026-08-05-shoot-plan-crm-integration/shoot-plan-crm-integration-design.md`；
- Checklist：`.codestable/features/2026-08-05-shoot-plan-crm-integration/shoot-plan-crm-integration-checklist.yaml`；
- Requirement：`.codestable/requirements/creative-shoot-planning.md`；
- Roadmap：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md` 与 items；
- Related docs：passed core design、cross-domain read-model compound、parent evidence gate contract；
- Code facts checked：customer merge、order cancel/update/delete、schedule create/update/delete、HTTP/OpenAPI error contract 与现有 transaction scope。

### Independent Review

- Status：completed；Detection：independent-agent；
- Provider / agent：`/root/creative_roadmap_focus_review`；
- Raw output：round 5 完整独立复审完成，0 blocking、0 important、2 nit，建议 `pass`；
- Merge policy：逐条以 design、checklist、roadmap 与既有 source endpoint 契约核验；两个 nit 均在同一修订链做 focused closure；
- Gate effect：独立 reviewer gate 已满足，可交回 `cs-epic` 批次。

## 2. Design Summary

- Goal：让 ShootPlan 可选关联 customer/order，把真实 shoot slot 投影为 execution window，同时保持零策划 CRM 流程无侵入。
- Key contracts：shootplanning-owned connection/projection sidecar、per-link epoch immutable snapshot、独立 `crm_revision`、冻结的 `PlanMutationResult`、CRM 专属 request operation/canonical、live source reduction 与 `apply_suppressed` 三分支。
- Transaction topology：有 key 的 CRM/manual mutation 由 `Execute` 统一 claim/replay；无 key 的 customer/order/schedule source mutation 保持本域 transaction ownership，并按 endpoint 既有拓扑执行 lock-first 或 pre-read/recheck。
- Steps：7 步；风险热点集中在跨域锁序/rollback、source error wire compatibility、fixed-clock projection、summary query delta 与 scoped guard。
- Checks：A1-A19 均可追踪到 S0-S6；checklist 同时覆盖 exact error、12 个 core golden、0/1/1 query delta 与 G1–G3 gate 约束。
- Baseline / validation：`make check`、Go/前端测试、OpenAPI golden、真实 PostgreSQL 双连接、故障注入、固定时钟与 query-count fixture。

## 3. Findings

### blocking

- none。

### important

- none。

### nit

- [x] FDR5-001 成功标准中的 `pre-read → sorted locks → locked recheck` 容易被误读为所有 source mutation 的统一拓扑。
  - Closure：已收窄为仅适用于需要发现 old/new refs 的路径；source IDs 已知的 merge/order 路径直接按全局顺序 lock-first。
- [x] FDR5-002 推进策略把三域 characterization 写成统一 retry 形态，弱化了 endpoint 差异。
  - Closure：已按 endpoint topology 改写为 CRM/manual `Execute` callback/retry、merge/order lock-first、schedule create/update/delete 各自既定 attempt/recheck。

### suggestion

- none。

### learning

- 跨域一致性不应强行统一 wire error：事务内部 retry sentinel 可以按编排复用，但对外必须保持各 source domain 已有错误契约。

### praise

- connection epoch、projection status 与 suppression 分离，使 order delete、relink 和 missing/past→future 三分支都能用稳定状态解释。
- material no-op fingerprint、single frozen response 与 exact replay bytes 把并发、重试和 core 兼容性放在同一可验证契约里。

## 4. User Review Focus

- 用户需要重点拍板：link epoch snapshot 保留策略、`apply_suppressed` 只有 adopt/unlink/new epoch 清除、completed/archived 只更新 sidecar、stage-1 evidence gate 在 implementation 前仍为 pending。
- implement 需要重点遵守：customer→order→slot→plan 全局锁序；CRM/manual 与 source-domain 两类 transaction shell；schedule exact error mapping；`PlanMutationResult` 和 12 个 core request/response golden 不得漂移。
- code review / QA / acceptance 需要重点复核：真实 PG 双连接死锁与回滚、claim replay callback/query=0、source domain transaction rollback/no-ledger、fixed clock、OpenAPI DELETE golden、PlanningSummary query delta。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1-A19 显式映射 S0-S6 与证据类型 | acceptance 按矩阵逐项收证 |
| DoD Contract | pass | E | Design/Implementation/Review/QA/Acceptance、命令与产物均已列明 | goal package 后执行 |
| Steps and checks traceability | pass | E | 7 steps 与 checklist checks 对应 exact error、PG、clock、query、golden | none |
| Roadmap contract compliance | pass | E/C | sidecar、single response、zero-plan absence、stage-1 gate 与 parent contract 一致 | implementation 前执行 gate runner |
| Module interface design | pass | E/C | CRM command/response、typed transaction participant、read-model batch seam 的 invariant/order/error mode 明确 | 以实现测试核验 adapter locality |
| Validation and artifacts | pass | E/C | 必跑命令、PG fixture、OpenAPI golden、query delta、gate report 均有落点 | 保存版本化证据 |

Summary：E=4，C=2，H=0，H-only core checks=none。

## 6. Residual Risk

- PostgreSQL 双连接下的真实锁等待/死锁矩阵只能由实现后的并发 fixture 证明。
- `Execute` claim 在 retry sentinel 回滚后必须不留 ledger；成功 replay 必须 callback/source query=0。
- customer/order/schedule 的 caller-owned transaction 失败必须连同 planning participant 一起回滚，且不得写 CRM idempotency ledger。
- schedule delete 新增 `409 customer_changed` 后必须同步 OpenAPI DELETE response 与 generated golden。
- `end_at-ε`、`end_at`、`end_at+2h` 和 RunModeSession backfill 依赖固定时钟测试。
- PlanningSummary 只承诺新增 planning query delta 0/1/1，不能把既有 customer orderStats N+1 误归因或误宣称已修复。
- future goal package 仍须交付并自测 `planning-evidence-dispatch-gate.py`；本 design 阶段不生成 runner。
- `stage-1-evidence-go` 仍为 pending；它不阻塞 design，但必须在本 feature implementation dispatch 前 approved。

## 7. Verdict

- Status：passed。
- Next：交回 `cs-epic` child-design batch，继续 `plan-share-collaboration`；本 design 保持 `draft`，不单独请求用户确认，也不进入 implementation。

## 8. Focused Closure

- Closed findings：FDR5-001、FDR5-002。
- Attributed delta：design §1.1 成功标准与 §2.4 推进策略的文字收窄；未改 command/response、transaction ownership、error mapping、验收场景或 checklist step 边界。
- Verification：design frontmatter validation passed；checklist validation passed；Ruby YAML parse passed；`git diff --check` passed。
- Classification：两处只消除“所有路径同一 topology”的歧义，保持 round 5 已审行为、公开契约、架构边界、验收语义和范围不变，因此不增加 review round。
