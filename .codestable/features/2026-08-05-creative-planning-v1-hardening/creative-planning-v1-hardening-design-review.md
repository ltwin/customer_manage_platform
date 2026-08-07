---
doc_type: feature-design-review
feature: 2026-08-05-creative-planning-v1-hardening
status: passed
review_state: passed
review_reason: ""
reviewer_id: /root
reviewed: 2026-08-05
round: 1
---

# creative-planning-v1-hardening feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-design.md`
- Checklist: `.codestable/features/2026-08-05-creative-planning-v1-hardening/creative-planning-v1-hardening-checklist.yaml`
- Intent / brainstorm: `.codestable/brainstorms/creative-shoot-planning/brainstorm.md`
- Roadmap: `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`
- Related docs: creative-shoot-planning requirement、七份 sibling child designs、tracked v2 prototype、既有 ops backup/restore 文档
- Code facts checked: schema-v1五件套 writer/strict reader、restore preflight/stages、v1 ops tests、planningmedia inventory seam、Makefile 与 frontend scripts

### Independent Review

- Status: interrupted-by-owner
- Detection: independent-agent → owner-capped local closure
- Provider / agent: `/root/hardening_design_review_r1`（已停止，不再恢复）→ `/root` 本地事实合并
- Raw output: 独立agent在长时间只读核验后未返回可合并 verdict；owner明确要求“这一轮审完就不要再审”，主agent停止该agent并禁止第2/3轮
- Merge policy: 仅按已冻结design/checklist、roadmap、sibling contracts和当前ops代码做一次本地material closure；不把实现偏好升级为finding
- Gate effect: owner明确终止后续review；无blocking finding，非阻塞不确定性全部记入Residual Risk
- Final frozen candidate: design `cf8a72d34797c6ced5326f8a526284ce99dfe415615aff58ed4588f25ce876f0`；checklist `cd693cf71daa34769b7ac6c01557b8bb18a0bfca02f6b0e90dd6aa4651e4766c`

## 2. Design Summary

- Goal: 作为只读集成/release evidence coordinator收口首版跨模块E2E、H1/H2、tracked v2、响应式/可访问性、G1–G3证据与发布门；首次拥有production backup schema-v2。
- Key contracts: hardening不暗补上游领域/安全/事务；dependency conformance缺口退回owner；schema-v1五件套兼容、v2七件套双读；restore固定`preflight→stop→replace→migrate→validate→reopen`；JSON/hash/stale fail-closed；不自动生产动作。
- Steps: 8；高风险集中在S1上游准入、S2/S3 package/restore、S4/S5真实跨模块与匿名负向、S6 evidence、S7 rehearsal/release。
- Checks: 24；均为pending且可追溯到roadmap/design/A1–A27。
- Baseline / validation: 当前代码只具备v1五件套与静态prototype runner；长文件与缺失planning E2E命令已明确归因，不用placeholder或N/A绕过。

## 3. Findings

### blocking

none。

### important

- [x] FDR-001 `design §2.1 PlanningRestorePreflightResult、§2.2 restore、A23；checklist S3/preflight check` missing/orphan 的校验对象曾可能被解释为当前待恢复target，而不是staging/source package。
  - Evidence: roadmap要求destructive前校验对象缺失/孤儿；恢复的目标本来可能因损坏而需要修复，若要求target内容先exact，会拒绝最重要的灾难恢复场景。
  - Impact: v2 restore可能在目标卷已有missing/orphan时错误阻塞，失去修复能力；实现者也可能把source package与target runtime两个inventory混为同一oracle。
  - Closure: design已拆成`source_package_inventory`与`target_runtime_inventory`；missing/orphan exact只约束staging/source，target只负责mount/generation/fence/space/replace安全。D5的v1包+非空planning target仍明确拒绝以防时间线混合；checklist同步。

### nit

none。用户要求当前轮收口，未把英文技术名、文件命名或工具选型偏好继续扩成修改项。

### suggestion

none。

### learning

- 灾难恢复preflight必须区分“备份源必须完整”和“待修复目标允许已损坏”；二者都叫inventory时很容易把安全门写成无法恢复的门。
- 最后一个hardening item不能首次提供早于它的stage-2 evidence producer；正确做法是把缺失变为上游conformance blocker，而不是倒置DAG。

### praise

- ownership边界明确：hardening唯一首次拥有的能力是production schema-v2与跨模块release evidence；上游缺口不会被测试协调器暗补。
- schema-v1/v2、old/new reader、source exact preflight、post-stop failure-stop和rollback tool retention形成了可机械验收的闭环。
- A1–A27同时覆盖真实happy path、失败/恢复、H1/H2、prototype、a11y、evidence stale、restore和release非副作用，没有把静态截图或模块fixture冒充production evidence。

## 4. User Review Focus

- Epic统一review需要重点拍板：v2固定七件套；deployment identity typed document；v1包面对非空planning target拒绝；production-shaped rehearsal需独立owner授权。
- implement需要重点遵守：S1任何上游scope缺口都停；missing/orphan校验source package；target损坏不阻止v2修复；进入replace后失败保持writers stopped。
- code review / QA / acceptance需要重点复核：匿名商业字段exact negative、真实browser/PG/双卷/restart、stage-2 producer时序、old reader拒绝、rehearsal与release verifier不执行promotion。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1–A27连续，全部映射S1–S8和证据 | none |
| DoD Contract | pass | E | design与checklist含Design/Impl/Review/QA/Acceptance/Non-Auto、10命令和required artifacts | none |
| Steps and checks traceability | pass | E | 8 steps、24 checks均pending且有roadmap/design/A source | none |
| Roadmap contract compliance | pass | E+C | H1/H2、§4.11、§8、§9、唯一hardening owner与DAG均保持 | none |
| Module/interface design | pass | E+C | stage1/stage2 source分离、planningmedia maintenance窄port、无全库repository | implementation conformance |
| Backup/restore safety | pass | E+C | 当前v1代码事实、v2 exact schema、source/target inventory分离和failure-stop | real rehearsal |
| Independent reviewer completion | residual | H | reviewer已启动但在owner要求停止前未返回verdict；最终为owner-capped local closure | 不再复审；实现/QA提高证据强度 |

Summary: E=6, C=3, H=1；唯一H-only项已显式列为residual risk，未隐藏为独立agent passed。

## 6. Residual Risk

1. stage-2 G3 canonical producer必须在`plan-business-feedback` dispatch前由share observation owner或Goal control artifact真实落地；当前hardening只定义conformance blocker。Goal package必须把这一时序机械化，不能等hardening实现。
2. 七个依赖目前仍是passed draft design而非实现；S1必须以最终generated type、route、maintenance command、accepted artifact与build重新核验，接口名允许按confirmed实现收敛但scope不得缩小。
3. 冻结old reader/tool image必须在rehearsal环境可取得并可验证digest；若只保留源码近似或重建不出上一版binary，A22不能声称old-reader compatibility passed。
4. deployment identity document的control-plane来源和权限由operator环境提供；若实现发现需要远程服务、secret或签名基础设施，须回roadmap/ADR，不得在hardening脚本里临时定义。
5. production-shaped rehearsal依赖owner单独授权环境；Goal authorization、scoped commit或本design确认都不等价于该授权。缺授权时acceptance handoff，不伪造pass。
6. 新v2逻辑若被迫侵入961/543/1941行legacy helpers，v1字符化可能不足以防CLI/signal/cleanup漂移；design已要求停止并另走refactor或先做零行为搬迁证据。
7. 独立reviewer未产出可合并verdict，本轮依据owner明确停止指令做本地closure；因此后续不再花时间做design复审，但code review/QA必须把restore和匿名负向当最高风险面。

## 7. Verdict

- Status: passed
- Next: design保持`draft`，交回`cs-epic` child batch；不启动第2/3轮，不单独请求本feature确认。

## 8. Focused Closure

- FDR-001：source/target inventory语义已在最终冻结candidate闭合；按owner指令不再启动独立复审。
