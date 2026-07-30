---
doc_type: feature-acceptance
feature: 2026-07-14-dashboard
status: passed
accepted: 2026-07-14
round: 1
---

# dashboard 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-14
> 关联方案 doc：`.codestable/features/2026-07-14-dashboard/dashboard-design.md`（`status: approved`）
> 工作区：`/Users/samson/workspace/my_project/customer_manage_platform`，分支 `feat/dashboard`，基线 `19d5161c…`

## 1. 接口契约核对

对照方案第 2.1 节。

**接口示例逐项核对**：

- [x] `GET /api/v1/dashboard` → 200 五键
  → `httpapi.GetDashboard` + `dashboard.Service.Get`；OpenAPI `getDashboard`。
  实测：QA API JSON 五键齐全；无 token 401；单测 `TestDashboardEndpointReturnsFiveKeys`。
- [x] `today_slots: ScheduleSlotListItem[]` / `unpaid_orders.items: OrderListItem[]`
  → OpenAPI 与 §4.3 措辞一致；生成物 `schema.d.ts` / `api.gen.go` 可引用。
- [x] 台上写：`POST /reminders/{id}/done|dismiss`、`PATCH /orders/{id} {balance_paid:true}`
  → DashboardPage 复用既有 client；QA S7/S8 浏览器 + API 交叉通过。

**名词层「现状 → 变化」**：

- [x] 新增 `backend/internal/dashboard` 只读聚合包 → 存在且不 import httpapi/gin。
- [x] 契约摘要形修正 → roadmap §4.3 + OpenAPI 均 ListItem。
- [x] `fetchDashboard` paths 派生类型 → `client.ts`；禁手写 DTO。
- [x] `DashboardPage` 脱离原型 → 无 `usePrototypeStore`/`TODAY`/`byId`/`price/2`。
- [x] `oapi-codegen.yaml` include-tags 含 `dashboard`；router 手工注册。

**流程图核对（2.2）**：

- [x] UI→GET→Service→Timezone→Repo→AccountScope→五块组装 → 代码落点齐全。
- [x] 时区失败 → error → 500，不瞎算 → 单测 S12。
- [x] 写后重拉 → QA S7/S8。

**偏差**：无未处理偏差。

## 2. 行为与决策核对

**需求摘要**：

- [x] 服务端聚合五卡 + 前端接真 + 登录默认落地 → QA/浏览器/API 通过。
- [x] 成功标准（五卡交叉一致 + 默认落地）→ items.yaml 完成信号满足。

**明确不做**：

- [x] 无 TG bind / export 实现 → 仍 404（S10）。
- [x] 无前端五请求拼页 → DashboardPage 仅 `fetchDashboard`。
- [x] 无看板自定义/图表/本页导出。
- [x] unpaid 保持 delivered-only 窄口径（S5 单测 + API）。
- [x] 无 v1-hardening 全站收口。
- [x] 无新写契约。

**关键决策**：

- [x] D1 只读域包 + AccountScope 直查，无他域服务假 seam。
- [x] D2/D11 ListItem 形；§4.3 先于/与 OpenAPI 一致。
- [x] D3/D9 时区注入 + `platform/clock` 同源；失败 500。
- [x] D4 台上动作范围 + 重拉。
- [x] D5 数组不截断，`count==len(items)`。
- [x] D6 due 含全部 type（含 churn）。
- [x] D7 tag 切片 + 手工路由。
- [x] D8 `schedule.AssembleListItems` + S13 parity。
- [x] D10 笔数主指标，禁 price/2。

**挂载点反向核对（2.3）**：

| 清单 | 代码落点 | 反向 grep |
|---|---|---|
| HTTP `GET /dashboard` | `router.go:129`、`dashboard.go` | 仅 protected 一组 |
| OpenAPI / codegen tag | `openapi.yaml`、`oapi-codegen.yaml` | include-tags 含 dashboard |
| `fetchDashboard` | `client.ts` | DashboardPage 唯一消费 |
| `DashboardPage` 接真 | `DashboardPage.tsx` | 无 prototype |
| req + VISION | `dashboard.md` current；VISION 已移入 current | — |
| 额外落点（composition） | `main.go` 注入 dashboard.Service(settings) | 属挂载，可卸载 |

**拔除沙盘**：去掉 router 注册 + DashboardPage 取数 → 经营台消失；去掉 OpenAPI tag → 生成面与契约漂移。无清单外业务挂载。

## 3. 验收场景核对

- [x] **S1** 登录/`/` → `/dashboard` 五卡 — 浏览器 `qa-screenshots/s1-dashboard-landing.png`
- [x] **S2/S3** due 窗 / churn 双卡 — `go test ./internal/dashboard`
- [x] **S4** 今日档期 + 日历跳转 — `s4-calendar-nav.png` + schedule API 同 slot
- [x] **S5/S6/S14** 窄 unpaid / stats / 笔数 UI — 单测 + `s14-grep.txt` + 浏览器
- [x] **S7** 完成提醒 — `s7-after-done.png`；pending=0
- [x] **S8** 标记收讫 — `s8-after-balance-paid.png`；`balance_paid=true`；revenue↑
- [x] **S9** 401 — `s9-curl.txt`
- [x] **S10** 范围守护 — 404 + diff
- [x] **S11/S12** 时区 — DayBoundary / fail 单测
- [x] **S13** parity — httpapi Dashboard parity 测试

**review §5 / residual**：REV-001/002 保留为遗留，不阻塞。
**QA**：`dashboard-qa.md` round 1 `passed`；failed/blocked=none。

## 4. 术语一致性

- [x] 经营台 / dashboard / 账号 / 客户 — 本 feature 代码与文案一致。
- [x] 禁用词「用户」：`dashboard` 包与 `DashboardPage` grep 无命中。

## 5. 领域影响盘点

- [x] 新名词「经营台」已在 design 第 0 节定义；CONTEXT 未强制补条——可选后续 `cs-domain`，不阻塞。
- [x] 结构性选择（跨域只读读模型、D8 导出装配、D9 clock 下沉）遵循既有 compound / ADR-001/003，**无需新 ADR**。
- [x] 流程约束（时区失败 500、窄 unpaid）已在契约/测试钉死，非新 ADR。

## 6. requirement delta / clarification 回写

- Requirement：`.codestable/requirements/dashboard.md`（design 阶段起草 draft）。
- 分支：goal 包与 owner 已批准 design（含本 req 愿景正文）；本条为首次实现，pitch/用户故事/边界相对 approved design **未改**。
- [x] 机械升级：`status: current`；`implemented_by: [2026-07-14-dashboard]`；变更日志追加。
- [x] `VISION.md`：dashboard 从 draft 移入 current。

## 7. roadmap 回写

- [x] `photographer-private-crm-items.yaml`：`dashboard` `in-progress` → **`done`**（feature 指向 `2026-07-14-dashboard`）。
- [x] `photographer-private-crm-roadmap.md` §5 条目 10 → **done**。
- [x] §4.3 ListItem 措辞与 OpenAPI 一致（implement Step1 已钉；acceptance 复核通过）。
- [x] `validate-yaml.py` 通过。

## 8. attention.md 候选盘点

- [x] 无新的「每个后续 feature 必撞」环境坑；Docker 串行测已在 attention。
- 分流：
  - D8 `AssembleListItems` 跨域只读装配模式 → 建议 `cs-keep`（可与既有 cross-domain-read-model compound 对照）
  - 公开 `GET /dashboard` → 可选 `cs-docs` api
  - 经营台使用说明 → 可选 `cs-docs` tutorial（非阻塞；v1-hardening 亦可收口）

## 9. 遗留

- REV-001 前端逾期红字近似日界。
- REV-002 unpaid OrderListItem 装配与 order 域潜在重复。
- goal 模式未 commit：`generate-check` 相对 HEAD 预期 diff，待 owner 授权一次 scoped commit。
- telegram-digest / data-export / v1-hardening 仍 planned，不在本条。

## 10. 最终审计

- 验证证据来源：`dashboard-qa.md` passed + 本轮 accept 重跑
- Evidence pack：`dashboard-impl-evidence.md`；无独立 gate/dod JSON
- 聚合命令（re-verified）：
  - `make build` → 0
  - `make lint` → 0
  - `go test ./internal/dashboard/... ./internal/platform/httpapi/... ./internal/reminder/... -count=1 -parallel=1` → 0
  - `make generate` 幂等（shasum 不变）
  - `make check` generate-check 相对 HEAD 失败 = goal 不 commit 预期例外
- 场景复核：`re-verified` ≈ 10（S2/S3/S5/S6/S9/S11/S12/S13 + 门禁 + API）；`trust-prior-verify` ≈ 4（S1/S4/S7/S8/S14 浏览器截图，本轮 accept 未重复点击但文件仍在 `qa-screenshots/`，QA 同日完成）
- trust-prior 比例约 29%（≤30%）；建议 owner 终审时打开 `/dashboard` 扫一眼五卡即可
- 交付物：dashboard 包、clock、handler、OpenAPI、前端、req current、roadmap done、QA/acceptance 报告 → 均在工作区
- 完整工作区：全部未提交属本 feature 交付；0 staged；**不得自动 commit**
- diff 清洁度：pass
- 知识沉淀出口：已分流建议（cs-keep / cs-docs），未擅自写入
- **结论：通过**

---

## 验收结论

- checklist 全部 `checks`：**passed**（steps 仍为 done）
- design / design-review / code-review / QA：**均 passed**
- roadmap `dashboard`：**done**
- requirement `dashboard`：**current**
- **status: passed**

建议 commit message（待 owner 授权，禁 `--no-verify`）：

```
feat(dashboard): 实现经营台 GET /dashboard 五卡聚合与接真首页

服务端只读聚合 + 前端脱离原型；roadmap dashboard → done；req → current。
```

待用户终审确认后，cs-feat / goal 工作流可关闭。后续 BUG 走 `cs-issue`。
