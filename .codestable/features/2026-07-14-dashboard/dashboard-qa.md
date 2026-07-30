---
doc_type: feature-qa
feature: 2026-07-14-dashboard
status: passed
tested: 2026-07-14
round: 1
---

# dashboard QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-14-dashboard/dashboard-design.md`（`status: approved`）
- Checklist: `.codestable/features/2026-07-14-dashboard/dashboard-checklist.yaml`（7 steps 全 `done`；checks 仍 `pending`，QA 未改）
- Review: `.codestable/features/2026-07-14-dashboard/dashboard-review.md`（round 1，`status: passed`，blocking=none）
- Evidence pack: `dashboard-impl-evidence.md`（替代独立 gate-results.json / dod-results.json）
- Gate results: none（goal 模式；CMD 结果写入本报告 §3）
- DoD results: none（checklist 内联 dod；见 §3）
- Diff basis: 分支 `feat/dashboard`；相对 baseline `19d5161c…` 的全部未提交实现；QA 只读验证，未改生产代码
- Baseline dirty files: 无（VISION / roadmap / OpenAPI / 前后端实现均可归因本 feature）
- Review freshness: review round 1 基于当前实现 diff；QA 期间无代码改动，review 未过期
- Feature type: `functional`
- Core evidence gate: design S1–S14 均为核心验收。后端 S2/S3/S5/S6/S9/S11/S12/S13 以定向/全量 Go 测试为运行证据；浏览器核心路径 S1/S4/S7/S8/S14 以真实 Vite+API 截图与 API 交叉为证据；S10 以 404 curl + 前端 diff 为证据

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | CMD-001 | core-functional | build/lint/test | command | `make check`（build/lint/test） | 绿 | pass（generate-check 见 QA-002） |
| QA-002 | CMD-002 | core-functional | codegen 零漂移 | command | `make generate` 前后 shasum | 生成物幂等 | pass（相对 HEAD 有预期 diff） |
| QA-003 | S1 | core-functional | 登录/根路径落地经营台 | browser | login → `/dashboard`；访问 `/` → `/dashboard` | 五卡渲染 | pass |
| QA-004 | S2/S3 | core-functional | due 窗 / churn 双卡 | unit | `go test ./internal/dashboard` | 窗内含逾期；churn 双卡 | pass |
| QA-005 | S4 | core-functional | 今日档期展示+跳转日历 | browser + API | 今日 hold 行；「打开日历」 | `/calendar` 见同 slot | pass |
| QA-006 | S5/S6/S14 | core-functional | 窄 unpaid + 笔数 + stats | API + browser + unit | unpaid count 口径；UI「N 笔」；禁 price/2 | 一致 | pass |
| QA-007 | S7 | core-functional | 台上完成提醒后 refetch | browser + API | 点「完成」 | due 1→0；pending 列表空 | pass |
| QA-008 | S8 | core-functional | 标记收讫后 refetch | browser + API | 点「标记收讫」 | unpaid 0；订单 balance_paid=true；revenue↑ | pass |
| QA-009 | S9 | core-functional | 无 token | API | curl GET /dashboard | 401 | pass |
| QA-010 | S10 | core-functional | 范围守护 | API + diff | telegram bind / export 404；无五请求拼页 | 404；Dashboard 仅 fetchDashboard | pass |
| QA-011 | S11/S12 | core-functional | 非默认时区 / 时区失败 | unit | DayBoundary / fail paths | 日界随 IANA；失败 500 | pass |
| QA-012 | S13 | core-functional | today_slots vs schedule list | integration | `TestDashboardTodaySlotsParity…` | 摘要+排序一致 | pass |
| QA-013 | review §5 | supporting | REV-001/002 residual | diff | 展示层逾期标签；order 装配重复 | 不阻塞 | pass（residual） |
| QA-014 | cleanliness | supporting | 调试/原型残留 | diff | DashboardPage 扫描 | 无 prototype/price/2/console.log | pass |

## 3. Command Results

- `make build` → exit 0；`make lint` → exit 0（golangci 0 issues；oxlint 通过）。
- `make check` → build/lint/`go test ./...`/前端脚本全绿；`generate-check` exit 1：**相对 HEAD 的契约/生成物 diff**（恰为 Step1 OpenAPI+codegen），属 goal「不 commit」预期例外。
- `make generate` 幂等：`api.gen.go` / `schema.d.ts` 重跑前后 shasum 不变（`98411361…` / `03c68dee…`）。
- `go test ./internal/dashboard/... ./internal/platform/httpapi/... -count=1 -parallel=1 -run 'Dashboard|Unregistered'` → exit 0。
- 真实 API（`:8080`）+ Vite（`:5173`）：
  - 无 token → 401（`qa-screenshots/s9-curl.txt`）
  - 登录后 GET `/dashboard` 五键齐全（`qa-screenshots/dashboard-api.json`）
  - POST telegram bind-token / GET export → 404
  - 动作后 unpaid=0、revenue_confirmed=196000（`dashboard-api-after-actions.json`）
  - 订单列表同 id：`balance_paid=True status=delivered price=128000`（`s8-order-list.txt`）
- 浏览器证据目录：`qa-screenshots/`（S1/S4/S7/S8/S14 截图）

## 4. Scenario Results

- [x] QA-001 门禁 build/lint/test：pass
- [x] QA-002 codegen 幂等 / generate-check 预期 diff：pass（goal 例外，非缺陷）
- [x] QA-003 S1 登录与 `/` 落地 `/dashboard` 五卡：pass — `s1-dashboard-landing.png`
- [x] QA-004 S2/S3 due/churn：pass — dashboard 包单测（API 手动创建 churn 被校验拒绝「type 仅支持 custom」，引擎生成 churn 路径由单测覆盖）
- [x] QA-005 S4 今日档期 + 打开日历：pass — hold 行可见；跳转 `/calendar` 同日「QA-dashboard today hold」（`s4-calendar-nav.png`）
- [x] QA-006 S5/S6/S14：pass — 窄 unpaid API；UI「N 笔」+「报价」；`price/2` grep 无匹配；stats 单测
- [x] QA-007 S7 完成提醒：pass — due 卡 1→0；pending reminders=0（`s7-after-done.png`）
- [x] QA-008 S8 标记收讫：pass — unpaid 1→0；revenue ¥680→¥1,960；列表 `balance_paid=true`（`s8-after-balance-paid.png`）
- [x] QA-009 S9 401：pass
- [x] QA-010 S10 范围：pass — TG/export 404；前端无五连击拼口径
- [x] QA-011 S11/S12：pass — 时区日界与失败路径单测
- [x] QA-012 S13 parity：pass — httpapi Dashboard parity 测试
- [x] QA-013 review residual：pass（不升级）
- [x] QA-014 清洁度：pass

## 5. Findings

### failed

- none

### blocked

- none

### residual-risk

- REV-001：前端「已逾期」标签用浏览器时钟近似，极端跨日瞬间可能与后端日界短暂不一致（仅红字提示）。
- REV-002：`loadUnpaidOrders` 与 order 域装配潜在重复，无 parity 契约，长期低概率漂移。
- generate-check 相对 HEAD 的 diff 待 owner commit 后消除。
- design-review residual（ListItem 物化措辞、D9 条件路径）：实现已落地 D8/D9；acceptance 再钉 §4.3 一致即可。

## 6. Cleanliness

- Debug output: pass
- Temporary TODO/FIXME/XXX: pass（本 feature 生产 diff 未引入）
- Commented-out code: pass
- Unused imports / dead code from this feature: pass
- Out-of-scope files: pass（DashboardPage 已脱离 prototype；他页仍可引用原型库，符合 design「不强制清原型库」）

## 7. Verdict

- Status: passed
- Next: `cs-feat` acceptance 阶段（回写 checklist checks、items.yaml `dashboard → done`、req `dashboard → current`、确认 §4.3 与 OpenAPI 一致；**不得自动 git commit**）
