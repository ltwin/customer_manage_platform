---
doc_type: feature-design-review
feature: 2026-08-05-plan-business-feedback
status: passed
review_state: passed
review_reason: ""
reviewer_id: /root/business_design_review_r3
reviewed: 2026-08-05
round: 3
---

# plan-business-feedback feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-design.md`
- Checklist: `.codestable/features/2026-08-05-plan-business-feedback/plan-business-feedback-checklist.yaml`
- Intent / brainstorm: `.codestable/brainstorms/creative-shoot-planning/brainstorm.md`
- Roadmap: `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`
- Related docs: `.codestable/requirements/creative-shoot-planning.md`、`schedule-calendar.md`、CONTEXT、ADR、core/CRM/reminder passed child designs、tracked v2 prototype
- Code facts checked: Order、Schedule、Settings、idempotency、CalendarPage、ScheduleSlotDialog、OpenAPI现状

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: `/root/business_design_review_r3`
- Raw output: Round 3终局独立建议`passed`：blocking=0、important=0、nit=0、suggestion=0；6项实现期不确定性归为residual risk
- Merge policy: 主agent已逐条核验终局结论与冻结candidate；FDR-007/008均闭合，无未处理material finding
- Gate effect: final design-review passed；本feature达到3轮上限，不启动第4轮
- VerifiedNoWrite: Round 3起止design=`ebeb6aec…`、checklist=`efad0c06…`完全一致；inode/mtime/size与git status基线不变

## 2. Design Summary

- Goal: 为摄影师提供私有经营事实、可解释订单价格草稿和档期时长草稿；生成零目标写，只有摄影师显式确认才修改Order或Schedule。
- Key contracts: unknown不等于0；base与delta分离；服务端target fingerprint；fresh/stale/applied/dismissed；exact acknowledgement；Order审计与Schedule/CRM/reminder同事务；Bearer/AccountScope隔离。
- Steps: 8；高风险集中在S4订单事务、S5档期事务与日历handoff、S6 Settings联合PATCH、S7真实浏览器集成。
- Checks: 21；全部pending且有source，覆盖Settings、locked participant、日历prefill、missing-order、query bound与stage-2 gate。
- Baseline / validation: checklist与items通过官方YAML validator；frontmatter、占位符、U+FFFD、`git diff --check`已核验；implementation仍受stage-2 evidence gate阻止。

## 3. Findings

### blocking

- [x] FDR-001 `design §1.5、§2.1 Bearer detail与Settings；checklist S6/S7、A19` Settings override wire contract无法同时表达保留现状、恢复继承、强制unknown与数值覆盖。（Round 2确认已由完整最终map + CAS闭合）
  - Evidence: 设计把`overrides`称为merge patch，又规定missing=继承、null=unknown、value=覆盖；已有value后，missing只能被实现者解释为preserve或delete之一，无法无歧义恢复平台继承。
  - Impact: A19和Settings三态UI不可实现；并发编辑、canonical body、rule revision/hash和same-value会因实现者选择replacement或merge而漂移。
  - Expected fix scope: 固化完整override-map replacement或closed per-key operation；同步OpenAPI、canonical body、same-value与`value → inherit` API/PG/UI验收。

- [x] FDR-002 `design §2.1 ScheduleDurationParticipant、§2.2 apply existing schedule；CRM design §2.2；reminder design lifecycle contract` 单阶段`ApplyExistingInScope`不能同时满足canonical fence/target锁序和写前stale/ack recheck。（Round 2确认已由staged opaque callback闭合）
  - Evidence: 当前方法在一次调用中完成Schedule写，但business必须在target锁后、Schedule写前锁business rows并重跑stale oracle；接口没有callback或opaque locked capability提供该插入点。
  - Impact: 只能形成`draft → fence/slot`反序/TOCTOU，或先写Schedule再校验draft；A12-A14、PG锁序和whole-transaction rollback均无法结构性保证。
  - Expected fix scope: 改为CRM-owned staged callback/opaque capability；participant先取fence与sorted target locks，再让business在任何Schedule写前做locked recheck，随后由同一capability执行Schedule+CRM+reminder lifecycle。

- [x] FDR-003 `design §2.1 Settings、BusinessRuleProvider；reminder design Settings timezone；backend/internal/settings` Settings联合PATCH缺少caller-owned mutation participant和空行首次CAS线性化点。（Round 2确认AccountSettingsMutationFence与timezone participant顺序成立）
  - Evidence: 无settings行时GET revision=0且不建行；当前代码仍是非锁定Get→Upsert；设计只有只读BusinessRuleProvider，没有定义两个`expected_revision=0`首次写如何串行，也没有定义rule+timezone同请求如何复用reminder fence/work并全回滚。
  - Impact: 可能lost update、两个首次CAS同时成功、timezone/reminder副作用绕过或在business CAS失败后残留，破坏A18/A19和联合请求全回滚承诺。
  - Expected fix scope: Settings-owned caller-transaction participant；timezone字段出现时先取canonical reminder fence；用唯一账号级Settings mutation lock处理空行；同tx校验CAS、写全部字段、为material timezone生成work；增加双连接和fault fixtures。

### important

- [x] FDR-004 `design §2.2 create_new；A9；CalendarPage/ScheduleSlotDialog/ShootOrderFlow` create_new没有落到当前真实Calendar/Dialog typed seam。（Round 2确认PlanningSchedulePrefillV1与普通recovery边界成立）
  - Evidence: Calendar只读`date/slot/schedule_draft`查询参数；Dialog只支持storage-backed scheduleDraftId且普通已有订单create会建立自身recovery journal；不存在order+basis的ephemeral navigation consumer，也没有basis驱动end或精确order/customer解析。
  - Impact: 实现者可能把business draft写入URL/sessionStorage、漏预填/联动、扩大handoff数据，或绕过既有普通create recovery；A9用户路径不可直接验收。
  - Expected fix scope: 固化React Router navigation-state schema、消费/清除/refresh/back行为、Bearer order resolver、start/end联动、普通recovery边界与URL/storage/POST body负向浏览器断言。

- [x] FDR-007 `design A19、Settings same-value与stale oracle；checklist S6/S7` exact same override map同时被定义为0 revision no-op和使fresh draft stale。（Round 3确认same map保持fresh、material map才stale）
  - Evidence: persisted map、revision和canonical hash都不变时，`rule_version_changed`不可能成立；A19却写成“不增revision，只使fresh draft stale”。
  - Impact: backend、frontend与QA对same-value后的effective status有互斥预期，并会引入无版本依据的隐式失效例外。
  - Expected fix scope: same map保持fresh；只有material map change使旧terminal=fresh draft按rule version投影stale；terminal applied/dismissed不改写。

- [x] FDR-008 `design generation unavailable union/decision table；CRM independent/customer-only contract；checklist A9` schedule draft缺少order时没有closed typed结果。（Round 3确认`order_required`、优先级与A24零写闭合）
  - Evidence: plan可合法independent/customer-only，schedule draft却必须携带order；当前schedule union没有`order_required`，`schedule_stage_ineligible`只定义为已有order状态不合格。
  - Impact: OpenAPI/application/frontend可能分别选择404、静默忽略或复用其它reason，破坏typed unavailable和零写保证。
  - Expected fix scope: schedule union增加`order_required`并固定reason优先级；cancelled/其它不合格status归`schedule_stage_ineligible`；增加independent/customer-only零写验收。

### nit

- [x] FDR-005 `design §2.2 create_new` 将路径统一写为`POST /api/v1/schedule/slots`，避免把省略全局前缀的文本当成真实router路径。（已修）

### suggestion

- [x] FDR-006 `design A5；formula truth table` 明确`base_price=null, absolute_target_price=0`，防止OpenAPI/Go/TS truthiness把合法0解释为absent。（已修）

### learning

- 普通Schedule recovery journal与business入站prefill是两个生命周期：前者在用户确认后保障普通create的未知结果恢复，后者必须只在页面内短命存在；二者不能合并，也不能为了business handoff删除现有recovery。
- 跨域participant如果需要“锁目标→调用方校验→owner写入”，单阶段`Apply(command)`不够深；callback或opaque capability应把同一physical tx、锁定目标和允许写入的时点绑定在接口形状中。

### praise

- 金额、unknown/0、base/delta、绝对目标价与canonical target fingerprint已经达到可生成golden test的精度，应在修订中保留。
- Bearer/AccountScope、share/cross-account 404、DTO/query/DOM/bundle/log负向契约完整，符合H1/H2。

## 4. User Review Focus

- Epic统一review需要重点拍板：Settings override采用完整最终map replacement；create_new只携带order+basis的短命prefill，不自动占档；经营信息永不进入匿名面。
- implement需要重点遵守：Settings首次CAS与timezone work同tx；Order/Schedule只通过locked capability写；business与普通Schedule recovery journal分层。
- code review / QA / acceptance需要重点复核：真实PostgreSQL双连接、三断点回滚、scope runtime guard、missing-order零写、calendar refresh/back/storage/URL/POST body、unknown与0的跨语言解码。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1-A24覆盖正常/边界/错误、Settings、missing-order与browser路径 | none |
| DoD Contract | pass | E | 命令、artifact、cleanliness与阶段gate结构完整 | none |
| Steps and checks traceability | pass | E | S1-S8与21 checks均pending且有design/roadmap/A编号source | none |
| Roadmap contract compliance | pass | E+C | 私有/显式确认/无AI、create_new唯一性和stage-2 gate均通过 | none |
| Module interface design | pass | E+C | locked callback + runtime guard、Settings mutation fence与Calendar seam经终局独立核验成立 | implementation conformance |
| Validation and artifacts | pass | E | CMD-001..006与PG/browser/query/compile/evidence artifacts入口完整 | implementation执行 |

Summary: E=6, C=2, H=0；H-only core checks=none。

## 6. Residual Risk

- 上游core/CRM/reminder仍是passed design而非已落地实现；S1必须以真实generated type、`TxAccountScope`、participant wiring和production composition重做conformance，stage-2 gate未通过时implementation diff保持零。
- 201的`generation_id`必须来自已落盘draft；A24全unavailable 409没有draft/generation可引用，OpenAPI golden不得临时生成ID。
- stale oracle实现必须kind-aware，不能让order/schedule的stage reason在错误kind上抢优先级；由stale golden和A13关闭。
- target fingerprint是approved字段值指纹，不是全局revision；A→B→A会回到相同指纹。实现不得私自扩字段/加revision，须以locked recheck、不同key并发、唯一audit和exact acknowledgement验证边界。
- `PlanningSchedulePrefillV1`选择refresh丢弃/back不重开的易失语义；browser fixture必须证明route state立即replace清除，普通recovery只在用户最终确认后启动。
- 工作区已有大量owner modified/untracked产物；implementation派发前必须以当前冻结hash做attribution，不能把并行设计产物计为本feature implementation diff。

## 7. Verdict

- Status: passed
- Next: design保持`draft`，交回`cs-epic` child batch；不单独向用户请求本feature确认。

## 8. Focused Closure

none
