# Dashboard 实现证据（feat/dashboard）

功能：2026-07-14-dashboard · 分支：feat/dashboard · 模式：goal（不 commit）
设计权威：`.codestable/features/2026-07-14-dashboard/dashboard-design.md`

## 汇总结论

- `make check` 中 **build / lint / test 全绿**；`generate-check` 仅因 goal 模式「不 commit」而与 HEAD 存在差异——其 diff 恰为 Step 1 契约与生成物变更，且 `make generate` 幂等（重跑前后 `api.gen.go`、`schema.d.ts` 的 shasum 完全一致，无漂移）。这是 goal 模式下的预期例外，非缺陷。
- 硬规则守护：
  - ADR-001：dashboard 全部查询走 `store.AccountScope`，`account_id` 从不来自客户端。
  - ADR-003：`dashboard` 包不 import gin / httpapi；handler 为薄适配。
  - D1：dashboard repo 经 `AccountScope` 直查 reminders/slots/orders，未注入他域 Go 服务作假 seam。
  - D8：`schedule.AssembleListItems` 导出，dashboard 今日档期与 `GET /schedule/slots` 共用同一装配。
  - D9：`AccountClock` 及 date-only 工具下沉 `backend/internal/platform/clock`，reminder / dashboard 同源引用；reminder 既有日界测试保持绿。
  - D10：前端以 `unpaid_orders.count` 为主指标，无任何 `price/2` 推算。
  - 术语：全程 账号 / 客户，无「用户」。

---

## Step 1：契约权威源 + 机器形式（父 agent 已完成，本次校验）

- exit_signal：roadmap §4.3 与 OpenAPI 一致，生成物含 `GetDashboard` 与 `ScheduleSlotListItem`/`OrderListItem` 引用 ✅
- 校验：`make generate` 幂等，`api.gen.go` / `schema.d.ts` 重跑 shasum 不变。
- 文件：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts`、roadmap 文档。

## Step 2：编排骨架 + 日界共享

- exit_signal：共享 clock 被 reminder/dashboard 引用且 reminder 日界测试绿；鉴权 GET 200 五键空值；dashboard 不 import httpapi ✅
- TDD RED→GREEN：
  - RED：新增 `TestDashboardEndpointReturnsFiveKeys`（未认证 401；认证 200 且 5 键存在）——stub 前构建断裂（`GetDashboard` 未实现 `ServerInterface`）。
  - GREEN：stub Service/Repository + handler + 路由注册后 `go test ./internal/platform/httpapi/... -run Dashboard` → PASS。
- VERIFY：`go test ./internal/reminder/...` → `ok`（日界回归绿）。
- 文件：`backend/internal/platform/clock/clock.go`（新）、`backend/internal/reminder/clock.go`（转发）、`backend/internal/dashboard/{model,service,repository}.go`（新）、`backend/internal/platform/httpapi/{dashboard.go,router.go,auth.go}`、`backend/cmd/server/main.go`、`backend/internal/platform/httpapi/customers_test.go`（fixture 扩展 Dashboard）。

## Step 3：计算节点 · 提醒（due + churn）

- exit_signal：窗内/窗外/逾期/同条 churn 双卡出现单测通过 ✅
- 口径：due = `status=pending ∧ due_date ≤ 今日+2`（date-only 串比较，含逾期、含全部 type、含 churn）；churn = `type=churn ∧ pending`（无日期窗）。排序 `due_date ASC, id ASC`。
- TDD：`TestDashboardGetAggregatesFiveBlocks` 播种逾期/窗内/窗外/churn 提醒，断言 due 命中且 churn 同条在两卡重叠。
- VERIFY：`go test ./internal/dashboard/...` → `ok`。
- 文件：`backend/internal/dashboard/repository.go`、`dashboard_test.go`。

## Step 4：计算节点 · 今日档期

- exit_signal：跨日相交通过；S13 摘要字段与排序与 `GET /schedule/slots` 一致 ✅
- 口径：与账号本地今日半开日界 `[today 00:00, tomorrow 00:00)` 相交；经 `schedule.AssembleListItems`（D8）装配 shoot 摘要。排序 `start_at ASC, id ASC`。
- TDD：`TestDashboardTodaySlotsParityWithScheduleList` 对同一窗口逐字节比对 dashboard `today_slots` 与 `GET /schedule/slots`（含 URL 编码修复 `+` 时区偏移）。
- VERIFY：`go test ./internal/platform/httpapi/... -run Dashboard` → PASS。
- 文件：`backend/internal/schedule/repository.go`（导出 `AssembleListItems`）、`backend/internal/dashboard/repository.go`、`dashboard_test.go`。

## Step 5：计算节点 · 尾款 + recent_stats

- exit_signal：窄口径 unpaid、cancelled、窗口边界、NULL price 单测通过 ✅
- 口径：unpaid = `status=delivered ∧ balance_paid=false`（窄于 unpaid_balance），`OrderListItem` 含 `customer_display_name`，`count==len(items)` 不截断，排序 `delivered_at ASC NULLS LAST, id ASC`；recent_stats 窗口 `[今日-29, 今日]` 本地日：orders_created 含全状态、orders_delivered 排除 cancelled、revenue_confirmed = delivered∧balance_paid∧≠cancelled 的 price 之和（NULL→0）。
- TDD：`TestDashboardGetAggregatesFiveBlocks` 覆盖 NULL price、cancelled 排除、窄口径笔数。
- VERIFY：`go test ./internal/dashboard/...` → `ok`。
- 文件：`backend/internal/dashboard/repository.go`、`dashboard_test.go`。

## Step 6：前端接真

- exit_signal：五卡为真实 API、动作后交叉一致、无 price/2、档期可跳转 ✅
- 实现：`client.ts` 新增 `Dashboard`/`DashboardSlot`/`DashboardUnpaidOrder`/`DashboardReminder`（均由 `paths['/dashboard']['get']` 派生，无手写 DTO）与 `fetchDashboard`；`DashboardPage.tsx` 重写脱离 `usePrototypeStore`/`TODAY`/`byId`/`price/2`，完成/忽略提醒与 PATCH `balance_paid` 后 refetch，档期行按 `customer_id` 跳客户或 `/calendar`，尾款以 `count` 为主指标。
- VERIFY：`npx tsc -b` → 无错误；`npm run lint`（oxlint）→ 无告警。
- 文件：`frontend/src/api/client.ts`、`frontend/src/pages/DashboardPage.tsx`。

## Step 7：polish / harden

- exit_signal：S1–S14 有证据且 `make check` 绿 ✅（generate-check 为 goal 模式预期例外，见汇总）
- 实现：加载 / 错误（重试）/ 空态；401 跳登录；时区读取或解析失败即返回 error → handler 落 500，绝不回退默认/浏览器时区（design D3/S12）；非默认时区日界测试 `TestDashboardDayBoundaryFollowsTimezone`（Shanghai vs New York 同一 UTC 瞬时不同本地今日）。
- 回归修复：`/dashboard` 现为真实路由，更新两处过期断言——
  - `router_test.go` `TestUnregisteredAPIPathsReturn404Envelope`：移除 `/api/v1/dashboard`（已注册，不再属未注册路径）。
  - `packages_test.go` `TestPackageAPIErrorPathsAndScope`：移除「dashboard 仍 404」断言（已实现，行为归 `dashboard_test.go`）。
- VERIFY：`make check` → build ✅ / lint ✅ / test ✅（`go test ./...` 全绿）/ generate-check 幂等无漂移。
- 文件：`backend/internal/platform/httpapi/{router_test.go,packages_test.go}`。

---

## 命令与结果

| 命令 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go test ./internal/dashboard/... ./internal/reminder/... ./internal/platform/clock/...` | dashboard `ok` / reminder `ok` / clock 无测试文件 |
| `go test ./internal/platform/httpapi/... -run Dashboard -v` | `TestDashboardEndpointReturnsFiveKeys` PASS、`TestDashboardTodaySlotsParityWithScheduleList` PASS |
| `go test ./...`（make check test 段） | 全部 `ok` |
| `golangci-lint run ./...` | 0 issues |
| `npx tsc -b` / `npm run lint` | 无错误 / oxlint 无告警 |
| `make generate` 幂等 | 重跑前后 shasum 一致，无漂移 |

## 已知例外 / 阻塞

- `make check` 的 `generate-check` 段在 goal「不 commit」下必然与 HEAD 存在 diff（diff 内容即 Step 1 契约/生成物），生成幂等无漂移，交由后续 commit 收口。无功能性阻塞。
