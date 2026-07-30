---
doc_type: feature-design-review
feature: 2026-07-09-schedule-calendar
status: passed
reviewed: 2026-07-10
round: 5
---

# schedule-calendar feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-design.md`
- Checklist: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-checklist.yaml`
- Intent / brainstorm: none
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` + items.yaml
- Related docs: `requirements/schedule-calendar.md`、`requirements/order-tracking.md`、`requirements/CONTEXT.md`、ADR-001/003、cross-domain-read-model / AccountScope fail-loud compound
- Code facts checked: customer Merge/Update 锁序、order Create/Delete 事务、AccountScope 事务接口、OrderWorkspace backfill 状态与日期转换、OpenAPI Go/TS 临时生成结果

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: 宿主原生 Codex subagent `schedule_design_review_r4`
- Raw output: round 5 全量复核及多次聚焦复核；先后发现 non-shoot union 继承 order_id、明确失败无法退出 pending、handoff 状态切换脏时间戳、known_order_id 首写失败语义，修订后最终 `blocking: none / important: none / passed`
- Merge policy: 主 agent 逐条以 design/checklist/OpenAPI/代码事实和临时生成结果核验，成立项全部修订并让 reviewer 复核
- Gate effect: none

## 2. Design Summary

- Goal: 桌面端可靠月历 CRUD + 客户档案共用排期入口；移动端只交付查档期轻路径。
- Key contracts: 账号时区 42 格月历、跨日/全天投影、重叠只提示、一订单一 shoot、type 判别列表摘要、订单删除反向拦截、创建幂等与结果未知恢复。
- Steps: 8 步；高风险面为幂等事务、customer/merge 锁序、历史 backfill handoff、status_sync 恢复和双端契约生成。
- Checks: A1-A26 共 26 条，全部 pending 且可追溯。
- Baseline / validation: make check / generate drift / 目标 Go 测试 / test:schedule / build / lint / YAML / diff cleanliness。

## 3. Findings

### blocking

- none

### important

- [x] FDR-R5-001 `OpenAPI ScheduleSlotListItem` non-shoot 分支继承通用 ScheduleSlot 后仍含可选 order_id。
  - Resolution: 抽出不含 order_id 的 `ScheduleSlotListItemBase`；shoot/non-shoot 构成 discriminator union。Go/TS 临时生成均证明 shoot 摘要必填、non-shoot 无订单引用字段。
- [x] FDR-R5-002 `D10/A15/A16` 明确失败与结果未知共用 pending 阻塞，可能把 tab 锁满 24 小时。
  - Resolution: 只有 unknown 不可清理；路径 B 明确失败、路径 A 保留 consulting/补偿、status_sync 明确失败均有安全终止动作，清 pending 但保留异常文本。
- [x] FDR-R5-003 `D17/A19` schedule_draft handoff 允许不可排期状态、浏览器时区和隐藏时间戳穿透。
  - Resolution: handoff 只允许历史可排期六态，默认 shot 并按账号时区预填；canonical body 按最终状态裁剪时间戳，覆盖 shot→scheduled 与 delivered→shot。
- [x] FDR-R5-004 `D17/A19` pending→draft→clear 对第一个本地写点失败作了不可能的零网络承诺。
  - Resolution: known id 写入 pending 失败时保留旧 key/body 与内存 id；刷新后用原 key/body 幂等确认。known id 持久化后，draft/clear 失败才保证纯本地重试。

### nit

- [x] FDR-R5-N01 `test:schedule` 创建阶段在 design step 8 与 checklist step 3 重复；已将 step 8 改为重跑终验。

### suggestion

- [x] schedule_draft 默认 shot，并按 draft.start_at 的账号本地日期预填 shot_at，减少重复输入。
- [ ] 实现阶段采用成熟 IANA/Temporal 时区能力，避免手写 DST 模糊时间解析。

### learning

- customer→order 锁序来自现有 Merge 代码事实，不是抽象偏好；反向 order→customer 会制造死锁环。
- 无 customer 的 offset 分页只作连续 merge 后的罕见恢复兜底，不是并发快照；优先使用刷新后的 slot 摘要。

### praise

- 幂等从请求头补丁提升为可验证的事务协议：typed operation、唯一事务 owner、canonical input、同事务成功响应和 commit unknown 测试 seam。
- 密集月历已钉死固定格高、稳定排序、前三条、唯一 slot 冲突计数与跨日归属，可直接落纯函数和浏览器验收。

## 4. User Review Focus

- 用户需要重点确认：首版只月视图；桌面完整 CRUD、375px 只读轻路径；重叠只提示；历史补录六态与“放弃本次排期”分流。
- implement 需要重点遵守：typed operation、customer→order 锁序、discriminator union、账号时区、unknown 与明确失败的不同退出规则。
- code review / QA / acceptance 重点复核：A2、A8、A10-A19，尤其响应丢失、硬刷新、merge、状态切换和本地存储失败点。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E+C | A1-A26 + requirement/roadmap/代码事实 | QA 按核心场景取证 |
| DoD Contract | pass | E | Design/Implementation/Review/QA/Acceptance、commands、artifacts 齐全 | none |
| Steps and checks traceability | pass | E | 8 steps、26 checks 均 pending 且来源明确 | none |
| Roadmap contract compliance | pass | E+C | roadmap §4/条目 7、items、OpenAPI 同口径 | none |
| Module interface design | pass | E+C | ExecuteCreate、TxAccountScope、PrepareCreate、timezone provider 与代码事实相容 | 实现期按 seam 测试 |
| Validation and artifacts | pass | E | YAML、Go/TS 临时生成、diff cleanliness 通过 | 实现后跑完整命令 |

Summary: E=3，E+C=3，H=0；H-only core checks=none。

## 6. Residual Risk

- sessionStorage 覆盖硬刷新但不覆盖关闭 tab；首版承诺已收窄，unknown 时关闭 tab 需人工核对。
- 客户端 24 小时与服务端 TTL 独立计时；实现应留安全余量，后续可增加服务端权威 expires_at。
- idempotency_records 尚未定义物理清理任务；首版监控表增长，后续补周期清理。
- 跨 tab 冲突不是实时推送；写后/聚焦重拉是当前收敛机制。

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review；用户确认后 design `draft → approved`，再进入 `cs-feat-impl`
