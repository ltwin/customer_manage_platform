---
doc_type: roadmap-review
roadmap: photographer-private-crm
status: passed
reviewed: 2026-07-10
round: 7
---

# photographer-private-crm roadmap 审查报告

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`
- Items: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`
- Related docs: schedule-calendar / order-tracking requirements、CONTEXT、ADR-001/003、相关 compound、schedule design/checklist
- Code facts checked: customer Merge/Update、order Create/Delete、AccountScope、OrderWorkspace 日期/补录状态、OpenAPI 双端临时生成

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: 宿主原生 Codex subagent `schedule_roadmap_review_r6`
- Raw output: round 6 `changes-requested`（1 blocking / 3 important / 1 nit），修订后 round 7 多次聚焦复核，最终 `blocking: none / important: none / passed`
- Merge policy: 主 agent 逐条核验代码和契约事实；成立项同步 roadmap/items/requirements/OpenAPI/design/checklist，并让同一独立 reviewer 复核最终增量
- Gate effect: none

## 2. Roadmap Summary

- Goal completion signal: 全链路可演示且全部 item done/dropped；本轮只强化 schedule-calendar 的可靠排期闭环。
- Module split: 既有 7 模块不变；schedule-calendar 合理跨 platform + order + schedule + webapp。
- Interface contracts: Idempotency-Key、typed operation、creation_mode、schedulable_at、timezone、ScheduleSlotListItem union、typed conflict details、nullable PATCH。
- Items: 12 条；唯一 minimal loop 为 customer-core；schedule-calendar 为 in-progress。
- Dependency shape: DAG，无未知依赖、无环。

## 3. Findings

### blocking

- [x] RMR-R6-001 `schedule create/PATCH × customer archive/merge` 缺少统一并发锁协议。
  - Resolution: 固化 customer→order 锁序、customer_id 复核和一次自动重试；连续变化返回 customer_changed。两种提交顺序线性化且保留既有 slot 历史事实。

### important

- [x] RMR-R6-002 幂等 operation 是持久化命名空间却可由 handler 自由命名。
  - Resolution: 固定 typed `order.create.v1` / `schedule-slot.create.v1`，由 contract test 守值。
- [x] RMR-R6-003 shoot 摘要“必返”未进入 OpenAPI 机器契约。
  - Resolution: ScheduleSlotListItem 改为 discriminator union；shoot 必填订单/客户/状态摘要，non-shoot 无引用字段。
- [x] RMR-R6-004 merge 后恢复仍可能使用过期 known_customer_id。
  - Resolution: post-slot refresh 回写最新 customer_id；未命中先重拉 slot，再用无 customer 稳定分页兜底。
- [x] RMR-R7-001 handoff 可补录不可排期状态、使用浏览器时区，且成功后的本地写序不完整。
  - Resolution: schedule_draft 限定历史六态，默认 shot/账号时区预填；pending→draft→clear 分阶段恢复；unknown 与明确结果使用不同退出规则。
- [x] RMR-R7-002 默认 shot 后状态切换会发送隐藏时间戳。
  - Resolution: canonical body 按最终状态裁剪；A19/step 7 覆盖 shot→scheduled、delivered→shot。

### nit

- [x] RMR-R6-005 密集月历的冲突数和同时间排序不可复现；已定义 display_start/id 稳定排序及“当日参与重叠的唯一 slot 数”。

### suggestion

- [x] 将 creation_mode 矩阵与 slot 时间矩阵收敛为 design 权威表，降低跨文档漂移。
- [x] schedule_draft 默认 shot 并预填账号本地开始日。

### learning

- 先提交者线性化比“归档竞态一律 409”更符合既有产品规则：shoot 先提交后归档时 slot 应保留并显示 archived 警示。
- 稳定 offset 分页不是快照，只能作为极窄恢复兜底；规模增长后应补按订单 ID 查询或 cursor。

### praise

- `new/backfill`、未来/历史 shoot 矩阵、consulting-first 与 post-slot status_sync 已形成同一条经营事实链。
- roadmap 没有用组合后端端点掩盖失败恢复，而是让独立端点保持可复用并用幂等/journal 收口。

## 4. User Review Focus

- 用户需要重点确认：只做月视图、移动端只读、重叠不阻止、历史六态补录和可保留异常现状结束。
- 后续 feature-design / implementation 重点：锁序、typed operation、双端 union、账号时区和 pending 恢复写序。
- 不能靠 roadmap review 完全确认：DST 控件可用性、关闭 tab 恢复、服务端 TTL 时钟边界、idempotency 物理清理。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Granularity Gate | pass | E | schedule-calendar 跨四层且含独立验收闭环 | none |
| Goal Coverage Matrix | pass | E | 条目 7 完成信号与 design A1-A26 对齐 | acceptance 取证 |
| DAG and minimal loop | pass | E | 12 items 无未知依赖/无环；唯一 minimal loop=customer-core | none |
| Interface contract usability | pass | E+C | roadmap §4 + OpenAPI + 临时 codegen 可执行 | 实现后零漂移 |
| Module interface depth | pass | E+C | idempotency、timezone、跨域锁与读模型 seam 有代码事实 | code review 复核 |

Summary: E=3，E+C=2，H=0；H-only core checks=none。

## 6. Residual Risk

- sessionStorage 关闭 tab 后丢失；当前承诺只覆盖硬刷新，unknown 关闭 tab 需人工核对。
- 客户端 24 小时与服务端 TTL 可能因时钟漂移出现边界差异。
- 跨 tab 冲突只在写后和重新聚焦时收敛，不是实时推送。
- DST 重复/不存在时间需成熟 IANA/Temporal 实现；验收已定义，具体引擎留实现期选择。

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review；schedule-calendar 可在用户确认 design 后进入实现
