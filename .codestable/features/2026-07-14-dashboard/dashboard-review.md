---
doc_type: feature-review
feature: 2026-07-14-dashboard
status: passed
reviewer: subagent
reviewed: 2026-07-14
round: 1
---

# dashboard 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-14-dashboard/dashboard-design.md`（`status: approved`，与目录一致）
- Checklist: `.codestable/features/2026-07-14-dashboard/dashboard-checklist.yaml`（7 个 steps 全 `done`；24 条 checks 全 `pending`——acceptance 阶段回写，符合 gate 前置）
- Evidence pack: `.codestable/features/2026-07-14-dashboard/dashboard-impl-evidence.md`
- Gate results: none（goal 模式，无独立 gate-results.json）
- DoD results: none（checklist 内联 dod 段，CMD-001 `make check` / CMD-002 `make generate` 幂等）
- Implementation evidence: `dashboard-impl-evidence.md`（含各 step RED→GREEN→VERIFY 与命令结果表）
- Diff basis: `git status --short` + `git diff`（分支 `feat/dashboard`，全部未提交；含 untracked 新包）
- Baseline dirty files: none（工作区 dirty 全部可归因于本 feature；`.codestable/requirements/VISION.md`、roadmap items/roadmap.md 属本 feature 契约权威源更新）

### Independent Review

- Detection: 本报告由父 agent 派发的**独立隔离 Task agent**（`code-reviewer-pro` 子代理，非本轮实现者）产出；主实现 agent 未参与本审查。
- 环节 A 独立隔离 Task agent: native-agent + completed
- 环节 B OCR CLI: not-available（本次调用未提供 ocr CLI 通道，跳过行级 OCR 扫描）
- OCR severity mapping: 不适用（未运行 OCR）
- Merge policy: 环节 A 结论均经本地对源码/测试/store 基座逐条事实核验后落定；无未返回的已启动环节。
- Gate effect: 独立 Task agent（环节 A）已完成，可作为下游质量 gate 放行锚点；OCR 缺失不阻塞（`subagent` 已满足默认 gate 要求）。

## 2. Diff Summary

- 新增：
  - `backend/internal/dashboard/{model,service,repository,dashboard_test}.go`（只读聚合域包）
  - `backend/internal/platform/clock/clock.go`（下沉共享 date-only clock，D9）
  - `backend/internal/platform/httpapi/{dashboard,dashboard_test}.go`（薄 handler + 端点/parity 测试）
  - `.codestable/requirements/dashboard.md`、feature 目录产物
- 修改：
  - 契约/生成物：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts`
  - 装配/编排：`backend/internal/schedule/repository.go`（导出 `AssembleListItems`，D8）、`backend/internal/reminder/clock.go`（转发到共享 clock，D9）、`backend/cmd/server/main.go`、`httpapi/{router,auth}.go`
  - 前端：`frontend/src/api/client.ts`、`frontend/src/pages/DashboardPage.tsx`
  - 回归修正：`httpapi/{router_test,packages_test,customers_test}.go`
  - 契约权威源：roadmap items/roadmap.md、VISION.md
- 删除：无
- 未跟踪 / staged：全部改动均未跟踪或未 staged（goal 模式不 commit）
- 风险热点：跨域只读读模型（reminders/schedule/orders 交叉口径）、账号隔离、时区日界、公共 API 契约、用户可见 UI

## 3. Adversarial Pass

- 假设的生产 bug：`recent_stats.revenue_confirmed` 在「无匹配行」或「匹配行 price 全 NULL」时 SUM 返回 NULL，Scan 进 int64 触发 500。
  - 核验结果：`store.AccountScope.ScalarAggregate` 对 `AggregateSum` 使用 `COALESCE(sum(%s), 0)`（`scope.go:326`），NULL/空集恒回 0；空 dashboard 测试 `TestDashboardEndpointReturnsFiveKeys` 断言 `revenue_confirmed==0` 并 200 通过 → 反例不成立。
- 攻击过的其它反例：
  - 账号隔离：dashboard 全部读经 `scope.Query/Count/ScalarAggregate`，`$1=account_id` 由基座强制（`scope.go`），cond 占位符从 `$2` 起，客户端无法传 `account_id` → 无跨账号泄漏。
  - due 窗边界（逾期/今日/+2/+3/done/churn 双卡）：`TestDashboardGetAggregatesFiveBlocks` 精确断言集合与排序，手工复算一致。
  - recent_stats 口径（created 含全状态、delivered 排除 cancelled、revenue 含 NULL price 按 0）：手工按 seed 复算 created=5 / delivered=3 / revenue=50000，与断言一致。
  - 时区日界 + 失败不瞎算：`TestDashboardDayBoundaryFollowsTimezone`（上海 vs 纽约同一 UTC 瞬时不同本地今日）与失败/非法 IANA 两条 error 断言覆盖 S11/S12。
  - D8 摘要漂移：`today_slots` 与 `GET /schedule/slots` 共用 `schedule.AssembleListItems`，parity 测试逐字节比对同窗口输出 → 无双实现。
- 结果：无升级为 findings 的反例；余量见 §6 residual risk。

## 4. Findings

### blocking

- none

### important

- none

### nit

- [ ] REV-001 `frontend/src/pages/DashboardPage.tsx:313-314` `ReminderRow` 的「已逾期」标签用浏览器时钟 + 账号时区在前端本地推算 `reminder.due_date < today`，与后端账号时区日界在极端跨日/DST 瞬间可能短暂不一致（仅影响红字提示，不影响 due 卡归属与计数）。可接受为展示层近似，不阻塞。

### suggestion

- [ ] REV-002 `backend/internal/dashboard/repository.go:101-148` `loadUnpaidOrders` 自行装配 `orderdomain.ListItem`（客户名 + 套餐名批量查找），与 order 域列表的同类装配逻辑存在潜在重复。design D8 只对 `today_slots` 强制 parity，`OrderListItem` 无 parity 契约、字段简单、`toAPIOrderListItem` 已复用，故不阻塞；若后续 order 列表装配演进（如客户合并/缺失显示口径），建议比照 D8 抽一个 order 侧共享装配函数以防漂移。

### learning

- `clock.NewAccountClock("")` 对空串回退 `Asia/Shanghai`（`clock.go:20-24`）。这是 settings 的既定默认值语义（非「读取失败」），与 D3/S12「读取失败 → error 不瞎算」不冲突；失败路径由 `TimezoneForAccount` 返回 error 触发，测试已覆盖。reminder 经 alias 转发到同一函数，行为与下沉前一致，无回归。

### praise

- D8 落地干净：`schedule.AssembleListItems` 签名只吃 `store.AccountScope`（非注入 `schedule.Service`），dashboard 与 schedule 列表单一来源，并以逐字节 parity 测试钉死，是「跨域只读读模型」compound 的范式实现。
- D9 共享 clock 下沉 + reminder 别名转发，既消除同构分叉又保住 reminder 既有日界测试绿；S11/S12 时区测试对抗性强（跨日 + 非法 IANA + 读取失败三态）。

## 5. Test And QA Focus

- QA 必须重点复核（运行时/浏览器证据，代码审查无法替代）：
  - S1 登录无 `from` 落地 `/dashboard` 且五卡渲染；S7 完成/忽略提醒后 refetch 计数下降且与提醒页一致；S8 标记尾款收讫后该单移除且订单详情 `balance_paid=true`；S4 今日档期行跳转（客户档案 / `/calendar`）；S14 尾款卡以笔数为主指标、无 `price/2`。
  - impl-evidence 未附浏览器截图（checklist `evidence_required` 含 `browser_screenshot`）；这些在 acceptance 阶段补齐即可。
- Evidence pack residual risks / gate warnings：`make check` 的 `generate-check` 段在 goal「不 commit」下与 HEAD 存在预期 diff（内容即 Step 1 契约/生成物），`make generate` 幂等（shasum 不变、无漂移）——非缺陷，交由后续 commit 收口。
- 建议新增或加强的测试：现有 Go 单测/集成测试对后端口径覆盖充分（S2/S3/S5/S6/S9/S11/S12/S13 均有自动化证据）；前端无自动化行为测试，依赖 acceptance 手工/浏览器验证（符合 design 对前端场景的证据类型约定）。
- 不能靠 review 完全确认的点：前端交互后端到端一致性（S7/S8 写后重拉）、真实浏览器落地路由（S1）。

## 6. Residual Risk

- 前端台上写动作（done/dismiss/PATCH balance_paid）后 refetch 的端到端一致性，代码逻辑正确但需 acceptance 浏览器实测（S7/S8）。
- 登录默认落地 `/dashboard`（S1）为路由行为，需运行时确认。
- `loadUnpaidOrders` 与 order 域装配的重复（REV-002）：当前无 parity 契约，属长期演进的低概率漂移风险，acceptance 无需处理，记录备查。
- generate-check 与 HEAD 的 diff 属 goal 模式预期例外，commit 时随契约/生成物一并落盘即消除。

## 7. Verdict

- Status: passed
- Next: 按「进入来源」表进入 `cs-feat` QA / acceptance 阶段——重点补齐 §5 列出的浏览器/端到端证据（S1/S4/S7/S8/S14），并在 acceptance 回写 checklist `checks`、items.yaml `dashboard → done`、req `dashboard → current`。无 blocking，无需 review-fix。
