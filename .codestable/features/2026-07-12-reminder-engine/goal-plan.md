# reminder-engine goal plan

## 路径

- feature 目录：`.codestable/features/2026-07-12-reminder-engine/`
- design：`reminder-engine-design.md`（status: approved）
- checklist：`reminder-engine-checklist.yaml`（8 steps / 24 checks / dod 3 命令）
- design-review：`reminder-engine-design-review.md`（status: passed, round 1）

## 用户确认依据

- 2026-07-12 owner 在会话中逐项认可全部假设/裁定（A1 02-29→02-28、A2 digest 空窗留 telegram-digest、A3/A4/D7 补齐型裁定 + acceptance 回写班车、A5 scan date 不限、D5 order_id 无外键、D6 merge 双 pending 接受），并追加拍板：per-customer 提醒开关不进首版（design §4 观察项④）。design 已改 `approved`。
- 分支：`feat/reminder-engine`（从 develop 拉出，2026-07-12）。

## 必跑验证命令（机读权威源 = checklist dod.commands）

| ID | 命令 | 失败处理 |
|---|---|---|
| CMD-001 | `make check` | fix-or-block |
| CMD-002 | `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` | fix-or-block |
| CMD-003 | `cd backend && go test ./... -count=1 -parallel=1` | document-baseline（testcontainers flake 归因预检，见 attention.md） |

## Implementation TDD policy

- 代码行为 step（S2-S7）默认 RED → GREEN → VERIFY micro-loop，留 evidence。
- S1（契约切片/codegen）与 S8 前端样式收尾部分可写 `TDD exception`：S1 的替代证据 = 生成物 diff + 404 测试；S8 UI 三态的替代证据 = 浏览器截图。前端交互逻辑（done/dismiss/校验）仍应有组件级或最少 e2e 断言。

## 核心验收路径

1. 同日双跑扫描零新增（场景 1）——roadmap 硬验收。
2. 三规则正/反/边界矩阵 + 时区 DST 注入（场景 3-13）。
3. merge 迁移提醒（场景 14）、已删订单 auto-dismiss（场景 15）。
4. Settings 校验 + PATCH timezone 后 /me 跟随（场景 19）。
5. 调度注入 clock：每本地日恰一次 + 重启补扫 + SIGTERM 有界退出（场景 20）。
6. 前端三面浏览器路径截图（场景 21-23）。

## DoD / gate policy 摘要

- design §3.4 DoD Contract 五级全 blocking；Acceptance Coverage Matrix 全部 Core=yes。
- 清洁度：slog 仅限三类（扫描跳过/扫描完成摘要/runner 错误）；无 TODO/FIXME/console.log/死 import。
- 硬规则：账号隔离（ADR-001，扫描查询全经 AccountScope）、薄 handler（ADR-003）、术语（不用「用户」）、Uber Go / 前端 checklist。
- commit 纪律（attention.md）：倾向一个 feature 一个提交，**提交必须经人工同意，不得自动 commit**；禁 `--no-verify`。

## Handoff 条件

见 `goal-protocol.md`；特别注意：A3/A4/D7 契约回写班车属 acceptance 阶段动作（cs-roadmap update），不构成"改公开契约"的 handoff——它是 design 已声明并经用户确认的计划内回写。若实现中发现契约本身不合理需实质变更，才 handoff。
