# dashboard goal plan

## 路径

- feature 目录：`.codestable/features/2026-07-14-dashboard/`
- design：`dashboard-design.md`（status: approved）
- checklist：`dashboard-checklist.yaml`（7 steps / checks 覆盖 S1–S14 + DoD）
- design-review：`dashboard-design-review.md`（status: passed, round 3）

## 用户确认依据

- 2026-07-14 owner 在会话中对 design 整体 **approve**（design-review 已 passed；重点拍板项 A1 台上动作、D6 due 含 churn、D10 待收尾款按笔数均按 design 原文采纳）。
- 分支：`feat/dashboard`（本工作区从 `develop` 拉出，2026-07-14 owner 确认）。

## 必跑验证命令（机读权威源 = checklist dod.commands）

| ID | 命令 | 失败处理 |
|---|---|---|
| CMD-001 | `make check` | fix-or-block |
| CMD-002 | `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` | fix-or-block |

基线风险预检（非 DoD 硬门，见 attention.md）：Docker 下 `go test ./... -count=1 -parallel=1` 可归因端口 flake。

## Implementation TDD policy

- 代码行为 step（checklist 步骤 2–5：骨架/提醒/档期/尾款与 stats）默认 RED → GREEN → VERIFY micro-loop，留 evidence。
- 步骤 1（契约权威源 + OpenAPI + codegen）可写 `TDD exception`：替代证据 = roadmap §4.3 已更新 + 生成物 diff + 鉴权 GET 可编译引用。
- 步骤 6–7（前端接真 / polish）UI 态可写 `TDD exception`：替代证据 = 浏览器截图 + 动作后交叉一致；聚合口径仍以后端单测为主，前端禁手写 DTO、禁 `price/2`。

## 核心验收路径

1. `GET /dashboard` 五键齐全；登录默认落地 `/dashboard`（S1）。
2. due 近 3 天窗含逾期与全类型；同条 churn 可双卡出现（S2/S3）。
3. today_slots 跨日相交 + D8/S13 与 `GET /schedule/slots` 摘要字段及排序 parity（S4/S13）。
4. unpaid 窄口径（delivered ∧ 未结清）+ 笔数展示禁 price/2；recent_stats 含 NULL price=0（S5/S6/S14）。
5. 台上完成/忽略/标记收讫后重拉一致（S7/S8）。
6. 401、时区失败 500、非默认时区日界（S9/S11/S12）；范围守护不做 TG/宽 unpaid 等（S10）。

## DoD / gate policy 摘要

- design DoD + checklist dod.commands 为 blocking；Acceptance Coverage Matrix S1–S14 须有证据。
- 清洁度：无调试打印 / 临时 TODO / 注释掉代码 / 无用 import；前端禁 `console.log`；DashboardPage 不残留原型 `TODAY`/`byId`/`usePrototypeStore`。
- 硬规则：账号隔离（ADR-001）、薄 handler（ADR-003）、术语（不用「用户」）、Uber Go / 前端 checklist；D1 禁假 seam、D9 禁 dashboard import httpapi、D11 roadmap→OpenAPI 顺序。
- commit 纪律（attention.md）：倾向一个 feature 一个提交，**提交必须经人工同意，不得自动 commit**；禁 `--no-verify`。

## Handoff 条件

见 `goal-protocol.md`。D11 要求的 implement 启动前 `cs-roadmap update` §4.3 ListItem 措辞是 design 已声明的计划内动作，不算「改公开契约」handoff。若实现中发现契约本身不合理需实质变更，才 handoff。
