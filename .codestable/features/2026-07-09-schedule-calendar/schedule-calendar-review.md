---
doc_type: feature-review
feature: 2026-07-09-schedule-calendar
status: passed
reviewer: subagent
reviewed: 2026-07-10
round: 4
---

# schedule-calendar 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-design.md`
- Checklist: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-checklist.yaml`（8/8 steps done）
- Evidence pack / gate results / DoD results: none（标准 feature 流程）
- Implementation evidence: 多轮 review-fix 对话记录、32 个 schedule helper 测试、前端 build/lint、仓库 `make check`、生成物隔离 index 复核、浏览器深链回归
- Diff basis: 当前 `develop` 的 unstaged tracked diff + 未跟踪 schedule-calendar 新文件；staged diff 为空
- Baseline dirty files: `.codestable/issues/2026-07-10-customer-profile-avatar-stretch/**`、`frontend/scripts/avatar-layout.test.mjs`，以及 `Makefile`、`frontend/package.json`、`frontend/src/index.css` 中明确归因 avatar-layout 的 hunks；不进入本 feature verdict

### Independent Review

- Detection: Paseo MCP 不可用；原生 Codex Task agent 可用；OCR CLI 已安装且 `ocr llm test` 通过
- 环节 A 独立隔离 Task agent: `native-agent` + `completed`
- 环节 B OCR CLI: `skipped-scope-ambiguous`；未提交工作树混有本轮排除的 avatar issue，不能裸扫
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded
- Merge policy: 独立 agent finding 已逐条用当前源码、OpenAPI、design 与测试事实核验、去重；主 agent 的浏览器证据单独合并
- Gate effect: 环节 A 已完成，`reviewer: subagent` 有效；当前无 blocking/important，可以进入 `cs-feat-qa`

## 2. Diff Summary

- review-fix 新增/修改：`frontend/scripts/schedule.test.ts`、`frontend/src/components/schedule/flow.ts`、`timezone.ts`、`ScheduleSlotDialog.tsx`、`ShootOrderFlow.tsx`、`OrderWorkspace.tsx`、`AppShell.tsx`、`shellContext.ts`、`CalendarPage.tsx`、`CustomerDetailPage.tsx`
- round 1 已修复：订单占用深链、backfill 2h/24h 恢复边界、过期人工退出、虚假成功提示、known-resource 错误分类、非法日期 query、跨月/空网格 slot 深链
- 未跟踪 / staged：schedule 组件与测试仍为本 feature 未跟踪文件；staged 为空
- 风险热点：幂等错误码分类、journal 清理与 attempt-key 轮换、customer merge 后候选刷新、原/拟修改区间分离、账号时区 fail-closed 恢复入口

## 3. Adversarial Pass

- 假设的生产 bug：409 被统一当作“明确未绑定失败”，从而把“key 已成功绑定”错误降格成可清 journal、可换 key 的普通校验失败
- 主动攻击过的反例：`idempotency_conflict` × order/slot/backfill phase；PATCH `customer_changed` 连续 merge；`GET /me` 瞬时 500 后 SPA 恢复；上一轮七项反例回归
- 结果：`REV-001`～`REV-011`、`REV-013` 全部 resolved；没有新的 blocking/important

## 4. Findings

### Round 1 Remediation

- [x] `REV-001`：`order_in_use` typed details 已按账号时区生成 `date+slot` 深链，畸形 details 安全降级到日历首页。
- [x] `REV-002`：matching `backfill_order` pending 会保留 2h～24h 的 source draft；不相关 pending 不延长普通 draft 寿命。
- [x] `REV-003`：过期 backfill recovery 已提供客户/订单核对入口和“已人工核对，放弃恢复记录”。
- [x] `REV-004`：backfill order 完成不再触发“档期已保存”；仅 slot 完成回调通知成功。
- [x] `REV-005`：known order/slot 或已满足 status sync 后的刷新/本地收口失败进入结果已知恢复态；status PATCH 自身 5xx 仍为 unknown。
- [x] `REV-006`：Calendar state initializer 使用语义日期校验；浏览器实测 `2026-13-40` 显示可理解提示并回当前月。
- [x] `REV-007`：known-slot recovery link 带账号本地日期；空网格失效 slot 有提示，有效 `date+slot` 会聚焦目标 entry。

### blocking

none

### important

none

### Round 2 Remediation

- [x] `REV-009`：`idempotency_conflict` 已进入专用 classifier/recovery mode；order/slot/backfill 均保留 journal、禁止 replay/reset/key 轮换，24 小时后才允许人工放弃。
- [x] `REV-010`：已加入最新 slot 摘要刷新、candidate reload token、旧候选清理与提交阻断；持久化摘要使用原 slot 区间，拟修改 draft 仅用于后续冲突与 PATCH。
- [x] `REV-011`：AppShell 提供全局 timezone fail-closed 重试；成功后 context 更新并自动恢复 Calendar range/slot 查询。

### Round 3 Remediation

- [x] `REV-013`：`refreshEditedSlotCustomer` 在 id 匹配时固定用原 `slot.start_at/end_at` 定位已回滚记录；自动与手工 retry 均遵守该规则，且只更新 shoot 的 customer/order/status，不覆盖拟改日期、时间和 note。

### nit

none

### suggestion

- [ ] REV-012 可在本轮窄修复中提取一个小型 create-failure classifier，统一 unknown / idempotency-bound conflict / customer_changed / deterministic-unbound / result-known-local-closeout；不要扩大成整体状态机重构。

### learning

- HTTP 409 不是统一的“确定性未绑定失败”：`idempotency_conflict` 恰恰证明 key 已成功绑定，绝不能靠换 key 绕过。
- 幂等恢复安全边界同时包含“5xx 原 key 重放”和“成功绑定冲突禁止换 key”。

### praise

- round 1 的七项编排问题均有代码与验证证据，非法日期、空网格失效 slot、有效 slot 焦点已在真实登录态浏览器复核，控制台无 error。
- 后端账号隔离、customer→order 锁序、一订单一 shoot 部分唯一索引、业务资源与幂等成功响应同事务等边界保持良好。
- OpenAPI、Go codegen 与 `schema.d.ts` 的 union/typed details 保持同源。

## 5. Test And QA Focus

- QA 必须重点复核：跨日期编辑 shoot slot 遇到 `customer_changed` 后，以原区间找回当前 slot，但保留拟改时间/note，再重拉候选与拟修改区间冲突。
- QA 继续回归：order / slot / backfill_order 的 `idempotency_conflict` 不清 journal、不换 key；`/me` 首次失败后显式重试且全程不回退浏览器时区。
- 保留上一轮回归：backfill 2h～24h 硬刷新、known slot 后刷新 5xx、24h 人工放弃、order 成功后取消 slot 无虚假成功、跨月 date+slot、malformed typed details。
- Evidence pack residual risks / gate warnings：无 goal evidence pack；OCR 因 scope 歧义跳过。
- 建议新增或加强的测试：组件测试覆盖原区间与拟修改区间完全不相交的 `customer_changed`；保留 classifier 与 `/me` 重试组件回归。
- 不能靠 review 完全确认：真实 storage SecurityError、10 秒/30 秒计时、多写并发合并、DST 完整交互。

## 6. Residual Risk

- 当前 schedule 测试仍以 helper 为主，组件 effect、错误码分派和网络调用次数主要依赖 build/lint、源码路径审查与浏览器 UAT；QA 需保留动作证据。
- 独立 reviewer 首次 schedule testcontainers 启动出现 `port 5432/tcp not found`，定向与全包 fresh 重跑通过；若 CI 复发需单独归因环境稳定性。
- avatar issue 与本 feature 共用少量 tracked 文件，后续 scoped commit 必须逐 hunk 归因。

## 7. Verdict

- Status: passed
- Next: 进入 `cs-feat-qa`。
