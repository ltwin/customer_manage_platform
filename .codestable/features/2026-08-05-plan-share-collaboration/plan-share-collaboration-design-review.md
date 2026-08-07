---
doc_type: feature-design-review
feature: 2026-08-05-plan-share-collaboration
status: passed
review_state: passed
review_reason: ""
reviewer_id: ""
reviewed: 2026-08-05
round: 10
supersedes_round: 9
review_mode: owner-approved-local-only
---

# plan-share-collaboration feature design 审查报告

> Round 10 完整独立只读复审已完成。冻结候选 design SHA-256：`b18947defc8dd8346d0a18fcff8050386679ec9323e5eaad9aa85694cec6e206`；checklist SHA-256：`1bdbbcbd7f189e8e5526a78f84a2a6c5dfdb6deb74183b1bcd5b281ff40f06b9`；结论为 `changes-requested`，`blocking=0`、`important=1`。reviewer 确认 Round 9 的 populated-down TOCTOU finding 已关闭，但发现 IP HMAC 每日轮换会使跨午夜的 active fixed window 获得新 counter identity。主 agent 已按 finding 修订候选；当前 design SHA-256：`a5eb429955695e7fe7954db60c916ed061795436203c381d9227da3ad763a431`，checklist SHA-256：`fe327cf139ac7d0316d533cb10f36dc12e0f6d3207f3e06545696640543da480`。Owner 已用 confirmation id `1909fda0-52dd-4a74-978a-842de1d7c892` 批准 local-only closure；本轮不增加 Round 11，当前候选收敛为 `passed`。

## Round 10 独立复审与审查上限后本地修订

### Independent Review

- Status：completed；Detection：independent-agent。
- Provider / agent：`/root/share_generation_review`。
- Frozen candidate verdict：`changes-requested`；blocking=0、important=1、nit=0。
- Closed regression：Round 9 的 populated-down check→DDL TOCTOU 已由 transactional migration、固定 SHARE table-lock 顺序以及 writer-first/down-first 双连接语义关闭。
- Gate effect：IP digest rollover finding 导致冻结候选不能通过；之后的契约修订属于实质安全语义变化，正常应再次完整独立复审。

### important

- [x] FDR-R10-I01 IP HMAC 每日轮换会改变 counter identity；若 23:59 创建的 10 分钟 outer-attempt 或 1 小时 business-quota 窗口在 00:00 后仍 active，新的 digest 可能建立第二个窗口并重置额度。
  - Evidence：Round 10 冻结候选按 daily current digest 单独寻址 counter，没有把前一日 active window 与新一日 digest 线性化到同一 guard identity。
  - Impact：攻击者或高频调用者可利用午夜边界额外获得一份配额，且 `Retry-After` 可能被新窗口缩短。
  - Local correction：当前候选新增 `SecurityAttemptBudgetGuardV1(policy_version, action, dimension, digest_version, hmac_digest, last_used_at)`；trusted resolver 仅在内存生成 current/previous 两个 domain-separated digest candidate，最长 1 小时 rollover grace 内同时提交；PG gate 按全局稳定顺序插入并 `FOR UPDATE` 锁定全部 guard rows，再选择/写入 counter。previous window 尚 active 时继续消费其剩余额度并保留原 `Retry-After`；current/previous 同时 active 视为 invariant corruption，返回 503 fail closed；原窗口结束后才允许 current-key 新预算。raw IP/HMAC key 不进入 application/store API 或持久层。
  - Evidence added：A22 与 checklist 的 `global_security_counter_daily_digest_rollover_continuity_fixture` 覆盖 23:59 首次消费、跨午夜提交、00:00 current/previous 并发、10m/1h ceiling、原窗口结束后的新预算和 cleanup。

### Local closure classification

- Delta type：实质安全契约修订，不满足 focused closure；主 agent 不能自行把本项改成 `passed`。
- Review cap：owner 已要求同一 feature design 默认不超过 3 轮完整复审；本 feature 已远超该上限，因此不自动增加 Round 11。
- Owner approval：Option A 已批准上述 local-only closure，approval ref 为 `approval-report.md#review-round-cap-local-closure`，confirmation id 为 `1909fda0-52dd-4a74-978a-842de1d7c892`；本 feature 不再增加 Round 11。

### residual-risk

- current/previous candidate 的 rollover grace、guard/counter cleanup 与 clock-boundary 并发仍需真实 PostgreSQL 固定时钟/双连接证据；local correction 目前只是 design/checklist 契约。
- global digest guard 会增加写放大和锁竞争；容量、statement timeout、索引及 cleanup runbook 留待 implementation/code review/QA。
- raw source IP 的可信解析仍依赖反向代理 allowlist 与部署配置；设计已禁止其越过 resolver，但静态文档不能证明生产 wiring。

### Verdict

- Status：`passed`。
- Review state：`passed`；lane=`owner-approved-local-only`。
- Next：design 保持 `draft`，返回 `cs-epic` 连续 child batch；不启动 Round 11，不进入 implementation、QA、Goal execution 或 Git 操作。

> Round 9 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=1`、`nit=0`。候选 design SHA-256：`48a57f8c6b9cc539ad2b9c46bc33993c3d6baf77d1fffdd1500f6f84358614ac`；checklist SHA-256：`7e2334f86e086cbc8501d90da805272b110bb7888bbad42724b8ea0dd56d715f`，reviewer 确认审查前后 hash 未变且全程只读。Round 8 及此前结论保留为 provenance。

## Round 9 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R9-001 populated down 的空检查未与在线 assignment writer 互斥，存在check→DDL TOCTOU。down transaction必须在空检查前按稳定顺序取得会与writer `ROW EXCLUSIVE`冲突并保持到commit/rollback的core work表和planshare source-event表锁，或验证不可由普通应用伪造的offline write fence；不能把golang-migrate advisory lock当业务writer exclusion。双连接fixture覆盖writer先提交→down见非空原子拒绝，以及down先锁空表→writer阻塞、down完成后旧writer整tx失败且无orphan。

### residual-risk

- default quote无MAC只允许最多10分钟的较短TTL且不扩大权限；Bearer keyset列表不承诺point-in-time snapshot；planshare多数seam仍需implementation证据。当前唯一门禁项是down writer exclusion。

> Round 8 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=3`、`nit=0`。候选 design SHA-256：`52db3e4e0783d63f66983536df1f95c1e11aa2c1728cb98e78acc14792cb77fb`；checklist SHA-256：`bd29f184df8452298b70870a013070df42f0341713bfed33ecf9e23c3ec02364`，reviewer 确认审查前后 hash 未变且全程只读。Round 7 及此前结论保留为 provenance。

## Round 8 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R8-001 `ShareManagementProjectionV1` 的 exact response 未把 expiry policy 绑定到 `proposal/full` view。须在结构中冻结 per-view cardinality，例如每个 `share_views[]` item 内含对应 policy，或固定键 `expiry_policies{proposal,full}`；management/detail projection 全部一致，并以同一 plan 的 proposal/full 默认值不同 fixture 验证。
- [ ] FDR-R8-002 past-window full 默认值 clamp 到动态 `now+1h` 后，在 projection→mutation 的正常网络延迟下必然低于新的 command-time 下界。须冻结可跨请求稳定的 quote/reference 或明确安全余量，并定义过期/刷新错误；继续保持 UI 提交绝对 `expires_at`、canonical 保存绝对值、exact replay 不重算，以及直接 command-time equality 合法。fixture 必须使用递增双时钟。
- [ ] FDR-R8-003 摄影师侧 `GET /feedback` 与 `GET /assignments` 只有路由、没有 exact read contract。须分别冻结 allowlist DTO、status/disposition/revision/target/deep-link 等必要字段、稳定排序、cursor、默认/最大 limit、active/history 规则，以及 receipt/commitment/selector/secret/internal account/CRM identity 排除；同步 OpenAPI golden、S5/S6、check 与 A24 coverage。

### nit

none。

### residual-risk

- `platform/txcap`、generic capability executor、planshare package、reciprocal FK、path sanitizer、uniform-404 timing、rate counter、media release/GC 与新的前端测试命令都仍需实现期证据；当前 design gate 不能用预期实现替代上述三项唯一契约修复。

> Round 7 完整独立只读复审结论为 `changes-requested`：`blocking=0`、`important=1`、`nit=1`、`suggestion=1`。候选 design SHA-256：`e9b69b4f6a880a1fd14138b6fb170c466b5d5933a18098b194c1e28a98906a2e`；checklist SHA-256：`54405436683de34e7b2f169392e3a8ee9a427c36748dcb977f6874e1bcbda448`，reviewer 确认 hash 未变且全程无写入。Round 6 及此前结论保留为 provenance。

## Round 7 独立复审 findings

### blocking

none。

### important

- [ ] FDR-R7-001 full token 的 window-derived 默认 expiry 与合法输入区间冲突。当前同时规定 `expires_at ∈ [now+1h, now+366d]` 与默认 `execution_window.ends_at+24h`；已结束超过 24h 或远期超过一年仍可能是合法 full issuance。须在 `SharePolicyV1` 冻结唯一 clamp/fallback 规则，并同步 issue/rotate、OpenAPI/UI 默认值、canonical request；A2/A3/S2/checklist 覆盖过去 window、远期 window 与上下边界 equality。

### nit

- [ ] FDR-R7-002 `ShareAssignment.revoked_by` 应为可空，并以 DB CHECK 保证 active 时 `revoked_at/revoked_by` 均空、revoked 时二者均非空；并入 A19/A20 revision fixture。

### suggestion

- [ ] FDR-R7-003 可将 `plan_id` 加入 work/event reciprocal composite FK，使 plan mismatch 在 commit 时报错；若不采用，A23 必须明确 plan mismatch 由 consumer quarantine 而非 FK 拒绝。

### residual-risk

- selector miss 的 IP-only read abuse、反向代理/监控 raw-token canary、跨 feature package/composition 与真实 PG 约束仍交 hardening/implementation 证明；不改变本轮窄修订范围。

> Round 6 完整独立只读复审结论为 `changes-requested`：`blocking=1`、`important=4`、`nit=1`、`suggestion=2`。Round 5 `passed` 及旧轮次保留为 provenance；当前回到 design 修订并继续 fail closed。

## 1. Scope And Inputs

- Design：`.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-design.md`；
- Checklist：`.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-checklist.yaml`；
- Requirement / roadmap / v2 prototype / passed core-media-CRM designs / ADR-compound / router-middleware-idempotency-auth-transport facts 已列入审查输入。

### Independent Review

- Status：completed；Detection：independent-agent；
- Provider / agent：`/root/media_design_reviewer`；
- Raw output：round 1～4 均为 `changes-requested`；round 5 为 `passed`，blocking=0、important=0、nit=3；
- Independence：reviewer只读，没有修改 design、checklist、roadmap、core 或 prototype；
- Merge policy：主agent已逐条以 roadmap、passed child designs、compound、代码事实与 prototype 核验；
- Gate effect：round 5 已完整关闭 FDR-I10/I11/N03/N04，round 1～4 其余契约无回归；主 agent 已核验文档、代码先例、parent/checklist 与 focused closure 增量，design gate 通过。

## 2. Design Summary

- Goal：proposal/full匿名分享、token-bound moodboard、feedback与full-only assignment；
- Key contracts：client-generated secret commitment、immutable view level、live safe projection、eligibility epoch、exact DTO、uniform404、receipt与formal assignment；
- Steps：8；Checks：26；
- Baseline / validation：round 1 发现 5 blocking、5 important、1 nit；round 2 发现 1 blocking、3 important；round 3 新增 1 important；round 4 新增 2 important；round 5 以 blocking=0、important=0 通过。最终设计已补 platform-security global PG fixed-window counter、ledger-winner-only admission、唯一 canonical frame 与双实例/双连接 evidence。

## 3. Findings

当前开放的 blocking/important/nit 均为 none。以下 B01～B05、I01～I05、N01 是 round 1 历史 finding，均已在后续轮次关闭；保留本表用于 provenance，不表示当前仍开放。Round 5 的 N05（窗口术语）、N06（canonical string escaping）与 N07（compound approval 状态）已在同一修订链做 focused closure。

### blocking

| ID | Finding | Required closure |
|---|---|---|
| FDR-B01 | anonymous token 验证后写“建立 AccountScope”，与 roadmap/compound 的 authenticated-only `AccountScope` 构造规则冲突，也未说明如何复用唯一 idempotency ledger 算法 | 冻结 `ValidatedShareContext → sealed ShareTransactionCapability/ShareTxScope`；HTTP 不得到通用 `AccountScope`，不伪造 `AccountContext`；store trusted factory 开 account-filtered tx；复用唯一 claim/replay/store-success 算法 |
| FDR-B02 | caller-generated receipt 与 parent roadmap “服务端成功响应返回 claim_receipt_secret”冲突 | 同步 roadmap、items 与 prototype intent：浏览器请求前生成 secret，只提交 commitment；成功后 UI 展示本地 secret；服务端 response/ledger 不含明文 |
| FDR-B03 | mutation 幂等只写概括，未冻结 operation/resource/canonical/stored-response 四元组 | 为全部 Bearer/anonymous mutation 建权威表，覆盖 commitment、token generation、expected revision、receipt hash、response-loss 与跨 operation/target 行为 |
| FDR-B04 | active assignment 阻止 `remove_readiness` 修改了 passed core contract，但缺 participant seam、锁后重检、生产 wiring 与 error union | 在 core 定义 `ReadinessRemovalGuard`，只接受 caller `TxAccountScope`，锁后调用，生产 fail-closed，映射 `409 readiness_assignment_active`，补双连接测试与完整独立复审 |
| FDR-B05 | share 让 archive 产生真实副作用，但 core 仍只有 `core-v1` acknowledgement | 新增 `planning-share-v1` exact effect set、选择规则、canonical/replay/UI 确认与 additive OpenAPI variant；同步 core checklist 并完整独立复审 |

### important

| ID | Finding | Required closure |
|---|---|---|
| FDR-I01 | share 写 `lock plan → read CRM eligibility`，与 CRM 的 `customer → order → slot → plan` 全局锁序可能反转 | issue/full mutation 先无锁 pre-read，再按 CRM 顺序锁 known order/slot，最后锁 plan 并重检 sidecar revision/epoch/status；定义 bounded retry 与 read linearization；补取消/解除/重关联竞态矩阵 |
| FDR-I02 | design routes 使用 `/share-feedback`、`/share-assignments`、POST revoke 和 `/{offerId}/claim`，漂移 parent seam | 对齐 parent feedback/assignment routes；claim body 使用 closed target union；仅为 planshare-owned on-site offer 在 parent 显式增加 Bearer routes |
| FDR-I03 | assignment source outbox 未定义 versioned schema/unique/order/privacy；流程图把 feedback 也写成 outbox | 冻结 `ShareAssignmentSourceEventV1`、唯一键与 revision ordering；排除 nickname/receipt/token/customer recipient；feedback 只写 observation，assignment claim/revoke 同 tx 写 observation+source event |
| FDR-I04 | anonymous moodboard `display_name/caption` 没有 owner，filename 又不可恢复 | v1 DTO 删除二者，只保留 `ref/checksum`；原型卡片视为视觉参考而非字段契约 |
| FDR-I05 | checklist `source` 使用笼统名词，无法机械追踪 | 所有 check 改为稳定 `design:§...+A...` 或 `roadmap:§...` 引用 |

### nit

- FDR-N01：prototype 文案暗示 proposal 被 full 取代、撤销 full 会让所有分享失效，与 proposal/full 可同时 active 冲突；design 必须写 prototype conformance override，正式 UI 按实际 view generation 状态展示。

### suggestion

- 集中定义 `SharePolicyV1`，让 expiry/rate/default/receipt/token 参数只有一个 owner。

### learning

- 匿名 capability 不能通过“已验证后恢复 AccountScope”偷渡认证语义；应把 grant 的来源与能力面一起密封。
- only-hash 与 response-loss replay 同时成立时，secret 必须在 caller 侧预先生成，ledger 只重放非 secret 业务响应。
- 后续 feature 给已通过 core 引入真实删除/归档副作用时，必须升级 core 的 guard 与 acknowledgement 公开契约。

### praise

- proposal/full exact projection、link epoch 防复活、token-bound media、receipt commitment 与 business zero-query 的安全方向成立。
- assignment 与 token 生命周期分离、readiness lead snapshot 不原位更新，给下一 reminder child 提供了稳定 source。

## 4. User Review Focus

- 当前不请求 owner 单项确认；本 feature 位于 epic child batch，完成 round 5 独立复审后仍保持 `draft`，交回 `cs-epic` 继续后续 child design。
- caller-generated one-time secret 的不可恢复产品代价已由 owner 在 `approval-report.md#round-8-share-contract` 选择 Option A（confirmation id `d83f1556-b56e-440b-b1bd-a45526c21f70`）；统一 designs review 仍需整体查看 proposal/full 可同时 active、archive 副作用与两层匿名限流，但不重复询问同一 supplemental checkpoint。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1-A24；A22含双实例/restart、双连接winner、计数、rollback、TTL及canonical特殊字符golden | implementation按精确计数落证据 |
| DoD Contract | pass | E | Design/Implementation/Review/QA/Acceptance DoD、6 commands、22 artifacts与cleanliness齐全 | none |
| Steps and checks traceability | pass | E | 8 steps/26 checks全pending，source稳定指向design/roadmap/A场景 | none |
| Roadmap contract compliance | pass | E/C | roadmap §5、items与design一致；passed core/media/CRM支撑 | none |
| Module interface design | pass | E/C | securitybudget/store/planshare ownership、sealed capability、claim-bound writer与typed ports边界成立 | code review验证隐藏bridge |
| Validation and artifacts | pass | E/C | 两个rate专用artifact加PG/HTTP/log/browser/depguard证据；现有auth limiter/proxy提供可行先例 | 实现期验证key rotation与容量边界 |

Summary：E=3，C=3，H=0，H-only core checks=none；6项全部通过。

## 6. Residual Risk

- `SecurityAttemptBudgetV1` 与 claim-bound bridge 尚未实现；implementation/code review 必须证明 current claim 不可伪造、没有复制通用 ledger、没有形成第二个通用 scope。
- 日轮换 HMAC 跨午夜 active window 的旧/新 key 选择仍需时钟 fixture，避免静默重置或双扣；fixed-window 的边界 burst 是已明确的模型代价。
- global digest 表可能产生高基数与 DB 写放大；48h cleanup、索引、statement timeout、pool 隔离和容量监控需要在 security artifact/code review 核验。
- uniform 404 的统计 guard 只能发现明显早返回，不证明公网绝对等时；G3 多代 token 去重仍由 hardening consumer 做跨代 plan/slot 聚合验证。

## 7. Verdict

- Status：passed。
- Next：design 保持 `draft`，返回 `cs-epic` 连续 child batch；不请求本 feature 单项确认，不进入实现。

## 8. Focused Closure

- Closed findings：FDR-N05、FDR-N06、FDR-N07。
- Attributed delta：design 仅把错误的“滑动窗口”改为 anchored fixed-window，并把 canonical JSON string escaping 指定到唯一encoder字节规则/A22 golden；checklist同步同一证据；compound只同步已存在的owner confirmation与core/share review provenance。
- Verification：design/checklist/compound定向diff、YAML/frontmatter parse、`git diff --check`、非法字符扫描；canonical escaping已机械覆盖控制字符、`/`、`<>&`、U+2028/U+2029和非ASCII。
- Classification：不改变token权限、公开API、winner线性化、架构边界、验收范围或DAG；只消除术语/encoder歧义并同步已发生的approval/review事实，因此不增加round、不启动新reviewer。

## 9. Round 2 Independent Review（2026-08-05）

- Status：`changes-requested`；reviewer=`/root/media_design_reviewer`；
- Trigger：round 1 的 FDR-B01～B05、FDR-I01～I05、FDR-N01 已按 finding 回写 design/checklist/parent/core/compound/prototype intent；
- Round1 closure：B01-B03/I01-I05/N01 contract-level closed；B04/B05等待core独立复审。
- Blocking FDR-B06：anonymous GET被称为read-only却需写ref/observation；已改为单个read-write repeatable-read transaction，冻结ref唯一键/upsert/serialization retry/full-open write与并发线性化。
- Important FDR-I06：Bearer full issue/rotate pre-read可能阻断exact replay；已改为ledger先行，eligibility pre-read/排序锁/recheck只在first callback，anonymous replay前current auth另行保留。
- Important FDR-I07：缺分享/offer管理query；已新增并同步parent `GET .../shares`、exact `ShareManagementProjectionV1`、排序分页/reason/revision/secret负向契约。
- Important FDR-I08：observation缺policy version与exact dedupe；已冻结full-open/feedback/assignment constructors及多代token→distinct plan/slot聚合层次。
- Gate：上述均属实质修订，必须round3完整独立复审。

## 10. Round 3 Independent Review（2026-08-05）

- Status：`changes-requested`；reviewer=`/root/media_design_reviewer`；完整、独立、只读复审，未修改文件；
- Closed：round 2 的 FDR-B06、FDR-I06～I08 均关闭；anonymous GET 的 read-write repeatable-read/ref upsert、Bearer ledger-first replay、management projection、observation policy/dedupe 与 canonical capability 均成立；
- Important FDR-I09：旧 rate admission 只按 `(token_generation,operation,Idempotency-Key)` 识别 replay，未绑定 exact canonical frame，同 key 异 body/target 可免扣业务配额并反复打 ledger；即使 exact replay 也缺 always-on request attempt ceiling，可能形成无上限 ledger 负载；
- Required closure：所有匿名 mutation 含 replay/conflict 都先计 IP/token-generation outer attempt，超限 429 且 ledger-call=0；业务 quota admission 绑定 operation+exact resource+typed canonical fingerprint，只有未过期 exact replay 免扣，异 fingerprint 继续计费并在获准后由 ledger 409；admission TTL 不长于 ledger TTL；A22/checklist/artifact 覆盖 exact 高频、异 body/target/receipt、429/409/ledger-call 与 expiry 边界；
- Nit FDR-N02：复审时 core round 6 尚在运行；现已由独立 reviewer裁决 `passed` 并持久化，依赖状态文字事实闭合；
- External gate：`round-8-share-contract` 已由 owner 选择 Option A 并记录 confirmation id；该批准不替代本轮技术 review。

## 11. Round 4 Independent Review（2026-08-05）

- Status：`changes-requested`；reviewer=`/root/media_design_reviewer`；完整、独立、只读复审，未修改文件；
- Closed：FDR-I09 的 always-on outer attempt、exact-frame binding、429/409/ledger-call precedence、admission≤ledger TTL、顺序 adversarial fixture 与 parent/items/checklist 同步成立；round 1～3 已关闭项无回归；
- Important FDR-I10：resolver 前 IP counter 缺少持久 owner/AccountScope 例外，可能被实现为进程内 limiter 或无 owner 的 accountless 业务表；要求 platform-security trusted PG counter、跨实例/重启 fail-closed 与 cleanup evidence；
- Important FDR-I11：pre-executor admission 与 generic ledger 是两个 winner 线性化点，并发 A/B frame 可能 admission/ledger 各选不同 fingerprint，重新打开 conflict-frame 免费 bypass；要求 admission 与 ledger immutable frame 共用 winner、锁序/rollback 与双连接计数 fixture；
- Nit FDR-N03/N04：§1.8 未同步完整 rate policy；`version || operation || resource || body` framing 不机械唯一；均须随修订闭合；
- Gate：global counter storage/transaction/acceptance 属实质变化，必须 round 5 完整独立复审。

## 12. Round 5 Independent Review（2026-08-05）

- Status：`passed`；reviewer=`/root/media_design_reviewer`；完整、独立、只读复审，未修改文件；blocking=0、important=0、nit=3；
- Closed：platform-security `SecurityAttemptBudgetV1` 是无 AccountScope 业务数据的显式 trusted-store PG 例外，按固定维度 row-lock、跨实例/重启共享、store error 503 fail-closed；`CanonicalAnonymousMutationFrameV1Bytes` 唯一构造同时供 ledger/admission hash；admission 不再 pre-executor 选 winner，只由 ledger first callback 同一 physical tx 写入并复制 ledger expiry；FDR-I10/I11/N03/N04全部关闭；
- Concurrency semantics：winner commit 前并发 exact/different frame 均保守扣 business quota；ledger callback 仅一次，只有 ledger winner 可形成 admission，conflict/rollback不能写 admission；A22/checklist/artifact 已加入跨实例/restart 与双连接 quota/admission/ledger/callback/429/409/rollback 断言；
- Scope：完整核对 FDR-I10/I11/N03/N04 closure、parent/items/checklist/compound 对齐与 round 1～4 回归；未发现回归；
- Nits：N05窗口术语、N06 canonical string escaping、N07 compound owner-confirmation状态均已按§8 focused closure关闭；
- Gate：主 agent 已用 design/checklist/roadmap/items/approval/compound与当前auth limiter/proxy/idempotency代码事实逐条核验并合并，round 5裁决持久化为`passed`。

## 13. Round 6 Full Independent Review（2026-08-05）

### Identity and already-closed deltas

- Reviewer：`/root/share_generation_review`；full-independent、read-only；未修改任何文件。
- Reviewer确认已关闭：`ShareTxScope.PlanningReminderFence()` accessor；work统一`source_event_id`与closed kind；event generation unique + reciprocal deferred composite FK；A23 duplicate/orphan/mismatch/rollback；旧锁序简写删除；core canonical type-state fence API已出现。
- Verdict：`changes-requested`；blocking=1、important=4、nit=1、suggestion=2。

### Blocking

- [ ] FDR-B01 把所有assignment claim/revoke错误地限制到anonymous `ShareTxScope`，导致Bearer摄影师撤销没有合法事务入口。
  - Required closure：anonymous claim/self-revoke走`ExecuteInCapability[ShareTxScope] → scope.PlanningReminderFence()`；Bearer photographer-revoke走authenticated `Execute/ExecuteInScope → TxAccountScope → trusted adapter FenceTxView`。两者取得`LockedFenceTx`和actor-specific stores后复用同一未导出mutation kernel与唯一锁序/generation/event算法；anonymous不能恢复AccountScope，Bearer不能伪造share capability，Bearer无anonymous admission/rate。同步D8/§2.1/§2.2/A23/S6/checklist与两入口whole-tx/compile-negative fixture。

### Important

- [ ] FDR-I01 share/reminder仍未完全统一到core canonical `FenceTxView.LockCurrentAccount() → LockedFenceTx.ReserveGeneration/MarkApplied` type-state API；须删除旧`PlanningReminderMutationFence/PlanningReminderFenceTxView`名和直接reserve形状，补wrong-order/cross-tx/account/exact replay/same-value negative。
- [ ] FDR-I02 assignment revision状态机未冻结。新claim必须新ID/revision=1，activation event=1；material revoke原子+1且首次revoke event=2；exact replay callback=0不递增；re-claim新ID/rev1；token/feedback/failure不改revision。补A17/A19/A23/S6/checklist。
- [ ] FDR-I03 `ShareAssignment.offer_kind`与全局canonical `assignment_kind`漂移。须统一model、active unique、CHECK、event mapping、DTO/consumer/fixture；readiness无offer row。
- [ ] FDR-I04 CMD-003 `test:plan-share`与CMD-006 `planning-prototype-v2.test.mjs`当前不存在且无producer/baseline attribution。S7须创建两runner并在exit后变mandatory；实现前缺失记expected implementation gap，S7后缺失fix-or-block；Required Artifacts同步。

### Nit and suggestions

- [ ] FDR-N01 将“用户统一review”改为“owner/设计人工review”，避免与匿名客户角色混淆。
- [ ] FDR-S01 为高风险evidence建立manifest：producing step、command/selector、output path/schema、required assertions/counters、build/config fingerprint。
- [ ] FDR-S02 保存account fence lock wait、tx duration、retry/serialization/deadlock脱敏基线；首版不拆fence。

### Evidence Confidence Ledger

| Check | Verdict | Class | Follow-up |
|---|---|---|---|
| Acceptance Coverage Matrix | fail | C | A23区分两entrypoint并补revision状态机 |
| DoD Contract | warn | E | 明确未来runner创建step/baseline |
| Steps/checks traceability | pass | E | 8 steps/26 checks/A1-A24均可追踪 |
| Roadmap compliance | pass | C | 依赖最终canonical契约后重跑 |
| Module interface design | fail | C | actor-specific entrypoint + type-state同步 |
| Validation/artifacts | warn | E | runner ownership + evidence manifest |

Summary：`E=3`、`C=3`、`H=0`；H-only core checks=`none`。

### Gate

- Round 6不能恢复`passed`；actor entrypoint、公开mutation orchestration、跨模块types与验收语义会实质变化，修后须完整独立复审。
- Residual risks：upstream core/CRM/reminder候选仍在变化；reciprocal deferred FK migration顺序需PG验证；account-wide fence可能形成contention；后置reminder才证明最终投递freshness；future frontend runners在S7前不可执行。
