---
doc_type: feature-design-review
feature: 2026-07-14-dashboard
status: passed
reviewed: 2026-07-14
round: 3
---

# dashboard feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-14-dashboard/dashboard-design.md`
- Checklist: `.codestable/features/2026-07-14-dashboard/dashboard-checklist.yaml`
- Intent / brainstorm: none
- Roadmap: `photographer-private-crm` / item `dashboard`（items.yaml 已 `in-progress` → feature `2026-07-14-dashboard`）
- Related docs: req `dashboard`（draft）、CONTEXT、ADR-001/003、compound `2026-07-09-cross-domain-read-model` / `2026-07-07-openapi-feature-tag-slicing`
- Code facts checked: `DashboardPage.tsx`（原型 price/2、排除 churn）、`App.tsx` 默认落地、`router.go` 未注册 dashboard、`oapi-codegen.yaml` tags、`openapi.yaml` /dashboard、`order/repository.go` unpaid_balance 宽口径、`schedule/repository.go` List/sortSlots、`reminder/clock.go` AccountClock、`settings.Service.TimezoneForAccount`

### Independent Review

- Status: completed
- Detection: native-agent（无 Paseo MCP；同类模型独立上下文，残余风险见下）
- Provider / agent: Cursor Task `code-reviewer-pro` ×3 轮（[R1](9f211c12-fb53-4367-9b63-473db2290a65) → 修 important → [R2](640761f5-3950-4b20-9682-3a3a300b47f9) → 再修 → [R3](0e4a384c-ce12-40a2-a6c9-28eceaa4bbce)）
- Raw output: 三轮回传摘要已本地事实核验后合并
- Merge policy: 已逐条核验；未经代码/文档支撑的外部判断不升为 blocking
- Gate effect: none（三轮后 blocking/important=0）

## 2. Design Summary

- Goal: `GET /dashboard` 服务端五卡聚合 + 替换原型经营台；登录默认落地已存在
- Key contracts: due 近3天含逾期/全类型；today_slots=ScheduleSlotListItem；unpaid=delivered 窄口径且 UI 按笔数；churn 全量 pending；recent_stats 近30天；D8/D9 防漂移；D11 契约权威源顺序
- Steps: 7（契约→共享 clock→提醒→档期→尾款统计→前端→harden）
- Checks: 覆盖名词/流程/挂载/范围/S1–S14
- Baseline / validation: `make check` + `make generate` 零漂移；Docker flake 归因说明已写

## 3. Findings

### blocking

none

### important

none（R1 四条已在 R2/R3 关闭：D10 笔数口径、S11/S12 时区验收、D8+S13 摘要/排序 parity、D9 禁 httpapi + AccountClock 硬下沉）

### nit

- [x] FDR-001 `settings.TimezoneForAccount` 在 scopeFactory 未接时静默回退默认时区 — implement 装配须保证非空（接线注记，不阻塞 design）
- [x] FDR-002 unpaid 排序 `NULLS LAST` 对 delivered 多为防御式 — 可保留

### suggestion

- [x] FDR-003 跨域读模型复用走「导出纯函数 + 契约对拍」模式 — design D8 已采纳；建议日后 compound 沉淀（非本轮阻塞）

### learning

- 禁注入他域 Service（假 seam）与防实现漂移之间的张力，正解是导出包级纯装配函数，而非 service-to-service 依赖。

### praise

- D1 跨域读模型与 compound 对齐；术语表钉死宽窄口径与 TG「今日」窗差异；D6 纠正原型排除 churn；D10 拒绝 price/2 幻觉。

## 4. User Review Focus

- 用户需要重点拍板：**A1** 台上是否保留完成/忽略/标记收讫；**D10** 待收尾款只按笔数展示（不推算尾款金额）；**D6** due 卡含窗内 churn（与流失卡重叠是契约）
- implement 需要重点遵守：D11 roadmap→OpenAPI 顺序；D8/D9 共享单点；DashboardPage 去掉 TODAY/byId/prototype store
- code review / QA / acceptance 需要重点复核：S5 窄 vs 宽 unpaid；S11/S12 时区；S13 摘要+排序 parity；S14 无 price/2

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design §3 S1–S14 + Coverage Matrix | none |
| DoD Contract | pass | E | design DoD + checklist dod.commands | none |
| Steps and checks traceability | pass | E | 7 steps ↔ D/S 场景；checks 均可溯源 | none |
| Roadmap contract compliance | pass | E/C | §4.3 五卡口径遵守；D2/D11 规划 ListItem 细化且权威源先于 OpenAPI | implement 步骤1 执行 update |
| Module interface design | pass | C | HTTP seam + AccountScope 读模型；禁假 seam / 禁 import httpapi | none |
| Validation and artifacts | pass | E | make check / generate；交付物清单可核验 | none |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- 同类模型做独立审查（非异构 Paseo），残余同温层偏差 — 用户整体 review 时重点核对 A1/D6/D10
- ListItem 形尚未物化到 roadmap/OpenAPI — 靠 D11 + CMD-002 关闭
- D9 下沉若意外受阻须补 golden 对拍并记 residual（条件路径）
- 原型库他页残留 — 超出本条范围

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review；确认后将 design `status` 改为 `approved`，再进入 goal-package / 实现
