---
doc_type: brainstorm
slug: calendar-v2-redesign
created: 2026-07-31
status: archived
summary: 档期页 v2 设计阶段的后端能力差距记录；必要增量已由 2026-07-31-calendar-v2-redesign 实现并验收
tags: [schedule, calendar, availability, backend-gap]
---

# 档期页 v2：后端变更清单

> 原型：`frontend/proto-design/v2/calendar.html`（+ `calendar.css` / `calendar.js` / `calendar-data.js`）
> 归档状态：本文是设计阶段的分析记录，不再作为待办或接口权威来源。必要增量已由 `2026-07-31-calendar-v2-redesign` 实现并于 2026-08-01 验收；最终契约以 `api/openapi.yaml`、正式 feature design 与 acceptance 为准。

## 结论先说

设计阶段确认，原型里约 90% 的优化**不需要新增后端端点**——周视图、非模态侧栏、点事件条直接编辑、键盘导航、年月跳转、撞单块级高亮、类型筛选、周末着色、取消订单降级、骨架屏简化、移动端写操作，均可由现有数据在前端完成。

当时唯一必须补齐的后端能力是**账号的「可约时段」定义**。该能力现已完成；「什么时候有空」「利用率」「可约 N 天」继续由前端消费 Settings 与 slots 后纯计算。

| # | 变更 | 必要性 | 阻塞谁 |
|---|---|---|---|
| 1 | `settings` 增 `availability` | **已完成** | migration 0014、Settings API/UI、data export schema v2 |
| 2 | `GET /schedule/openings` | 未实现，保持可选 | 无（前端本地计算） |
| 3 | `GET /schedule/overview` | 未实现，保持可选 | 无（前端本地计算） |
| 4 | slot 的 `order_status` | 已有并沿用 | — |

---

## 1. 账号可约时段偏好（已完成）

### 为什么

设计阶段的系统里没有「几点到几点算可以接单」这个概念。摄影师被问「下周什么时候有空」时，系统只能显示"这些时段已被占用"，不能回答"这些时段还空着"。二者不是同一件事——凌晨 3 点没被占用，但它不是可约时段。

三个能力全部依赖它：

- **空档速览 / 可粘贴文案**：工作窗口 − 已占用 = 可约空档；没有工作窗口就没有减数
- **利用率**：已排时长 ÷ 可约总时长；分母来自工作窗口
- **周视图非工作时段压暗**：不压暗的话时间轴是 0:00–24:00 的稀疏空白

### 数据结构

最终实现沿用本记录的判断：`settings` 表增加 JSONB 列，不新建表，因为它是账号级偏好，和 `digest_hour`、`churn_thresholds` 同类：

```sql
-- backend/internal/platform/store/migrations/0014_settings_availability.up.sql
ALTER TABLE settings
  ADD COLUMN availability JSONB NOT NULL DEFAULT '{
    "weekly": {
      "1": {"start": "10:00", "end": "19:00"},
      "2": {"start": "10:00", "end": "19:00"},
      "3": {"start": "10:00", "end": "19:00"},
      "4": {"start": "10:00", "end": "19:00"},
      "5": {"start": "10:00", "end": "19:00"},
      "6": {"start": "09:00", "end": "20:00"},
      "7": {"start": "09:00", "end": "20:00"}
    },
    "min_opening_minutes": 120,
    "turnaround_minutes": 60
  }'::jsonb;
```

字段语义：

| 字段 | 含义 | 约束 |
|---|---|---|
| `weekly` | ISO 星期（`"1"`=周一 … `"7"`=周日）→ 当天可约窗口 | 值为 `null` 表示当天整天不接单；`end > start`；`HH:MM` 24 小时制 |
| `min_opening_minutes` | 短于此值的碎片不对外报为「可约」 | ≥ 15，≤ 480 |
| `turnaround_minutes` | 两场之间低于此间隔给**软提醒**（不是冲突） | ≥ 0，≤ 240 |

时区沿用 `settings.timezone`，不在 `availability` 内重复存——单一事实源，避免两处不一致。

**为什么用 ISO 1–7 而不是 0–6**：`churn_thresholds` 已是 JSONB 数组，本项目 JSON 里没有星期先例；ISO 8601 的 1=Monday 是标准，Go 的 `time.Weekday` 是 0=Sunday，前端 `Date.getDay()` 也是 0=Sunday——三方都不一致时选标准而非任一实现的默认，转换点集中在一处。原型里用的 0=周一 是 UI 列序，映射时要换算。

### 接口

下方是设计阶段草案。最终 OpenAPI 没有使用宽泛的 `additionalProperties` weekday map，而是用 `ScheduleAvailabilityWeekly` 显式声明 `"1"`～`"7"` 七个 required、nullable 属性，并设置 `additionalProperties: false`；运行时由局部 strict decoder 拒绝缺键、额外键和未知嵌套字段。

`GET /settings` 响应增字段，`PATCH /settings` 支持整体替换 `availability`（不做字段级 patch——嵌套 patch 语义混乱，前端本来就是整表单提交）：

```yaml
# components/schemas/Settings 增
availability:
  $ref: "#/components/schemas/ScheduleAvailability"

ScheduleAvailability:
  type: object
  required: [weekly, min_opening_minutes, turnaround_minutes]
  properties:
    weekly:
      type: object
      description: ISO 星期 "1"(周一)…"7"(周日) → 可约窗口；null 表示当天不接单
      additionalProperties:
        nullable: true
        type: object
        required: [start, end]
        properties:
          start: { type: string, pattern: "^([01][0-9]|2[0-3]):[0-5][0-9]$" }
          end:   { type: string, pattern: "^([01][0-9]|2[0-3]):[0-5][0-9]$" }
    min_opening_minutes:
      type: integer
      minimum: 15
      maximum: 480
      default: 120
    turnaround_minutes:
      type: integer
      minimum: 0
      maximum: 240
      default: 60
```

校验（`400 validation_failed`）：`weekly` 键必须是 `"1"`–`"7"` 全集；`end > start`；范围越界。

**注意**：`GET /me` 返回的 `Account` 不加这个字段。账号身份和账号偏好是两件事（ADR-005 已经把 identity 和 credential 分开了，同理），`availability` 属于 `Settings`。前端拿时区已经在读 `/settings`，不增加请求。

### 前端配套

设置页「可约时段」编辑区已经交付：7 行星期 ×（开关 + 起止时间），以及最小空档和转场缓冲两个数字输入；失败保留草稿、保存 pending 冻结和成功按服务端响应回填均有测试与浏览器证据。

---

## 2. 空档扫描接口（可选，P2）

```
GET /schedule/openings?from=&to=&min_minutes=
→ [{ date, start_at, end_at }]
```

**先不做。** 前端已有 `GET /schedule/slots?from&to` 的完整区间数据 + `availability`，本地做区间减法即可，逻辑不到 30 行（原型 `calendar-data.js:openingsOfDay`）。只有两种情况才值得上服务端：

- 跨月长窗口扫描（比如"未来 3 个月哪天有整天空档"）——一次拉 90 天 slots 数据量偏大
- 未来做对外分享链接（客户自己看可约时段）——那时前端不在自己人手里，逻辑必须在服务端

在这两个场景出现之前，加这个接口是提前抽象。

## 3. 月度概览聚合（可选，P3）

```
GET /schedule/overview?month=
→ { shoot_count, hold_days, conflict_days, open_days, revenue, utilization }
```

**先不做。** 顶栏那句「7月 · 12 场拍摄 · 利用率 64% · 可约 9 天」的所有输入都在当月 slots 里，前端聚合即可。只有要做跨月趋势图（近 6 个月利用率曲线）才需要服务端算。

## 4. 取消订单降级（无需改动）

`GET /schedule/slots` 的 shoot 项已带 `order_status`。月视图给已取消订单的档期加删除线 + 降透明度，纯前端。冲突判定要排除已取消档期，也是前端逻辑。

---

## 派生的软能力：转场间隔提醒

原型当时提出了一条既有系统没有、现已落地的判定：两场拍摄之间间隔小于 `turnaround_minutes` 时给**软提醒**（不是冲突）。

`12:00 结束 + 12:15 开始` 在现有逻辑里判定为「无冲突」，但实际上摄影师收器材、转场、吃饭都来不及。这是真实翻车场景，值得提示。

它是 `availability.turnaround_minutes` 的直接产物，不需要额外后端字段。最终判定逻辑位于前端日历纯模型。语义边界保持为：**软提醒不阻止保存，也不进冲突计数**——`schedule-calendar.md` 的「重叠只提示、不阻止」是关于硬重叠的，转场提醒比它更弱一档。

---

## 与现有需求文档的关系

`.codestable/requirements/schedule-calendar.md` 的「边界」一节写着：

> 首版只做月视图，不做周视图、拖拽改期、周期重复或跨月批量操作。

本次原型当时**突破了这条边界**（加了周视图）。该冲突已在实施前通过 owner 批准的 requirement/roadmap delta 解决；`schedule-calendar.md` 已更新为月、周双视图与移动端完整 CRUD，没有在 feature 内绕开权威契约。

拖拽改期仍在边界外，原型也没做。

## 最终实现与证据

- Feature design：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
- Acceptance：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-acceptance.md`
- Settings migration：`backend/internal/platform/store/migrations/0014_settings_availability.up.sql`
- Settings domain/strict decode：`backend/internal/settings/availability.go`
- OpenAPI：`api/openapi.yaml` 中 `ScheduleAvailabilityWeekly`、`ScheduleAvailability` 与 `Settings.availability`
- 前端纯计算：`frontend/src/pages/calendar/`；Settings 编辑：`frontend/src/pages/settings/`
- 明确未新增：`GET /schedule/openings`、`GET /schedule/overview`、book、自助预约、主动消息、拖拽、重复规则与多工作窗口。
