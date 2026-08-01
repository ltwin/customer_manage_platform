---
doc_type: feature-design-review
feature: 2026-07-31-calendar-v2-redesign
status: passed
review_state: passed
review_reason: ""
reviewer_id: "/root/calendar_v2_design_review_round4"
reviewed: 2026-07-31
round: 4
---

# calendar-v2-redesign feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
- Checklist: `.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-checklist.yaml`
- Intent / brainstorm: feature 目录内无 intent/brainstorm；原型与源工作区 brainstorm 只作为需求证据，不成为运行时依赖
- Requirement: `.codestable/requirements/VISION.md`、`.codestable/requirements/schedule-calendar.md`
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`、`photographer-private-crm-items.yaml`、`goal-state.yaml`
- Related docs: `.codestable/requirements/CONTEXT.md`、`.codestable/compound/2026-07-09-cross-domain-read-model.md`、已完成 schedule-calendar / data-export 设计及项目 execution/shared/solution-depth 约定
- Code facts checked: Settings、schedule、dataexport、dashboard repositories 与 HTTP/OpenAPI；`SettingsPage.tsx`、`CalendarPage.tsx`、`ScheduleSlotDialog.tsx`、calendar model/timezone、`frontend/package.json`、`Makefile` 与响应式样式

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: 第一轮 `/root/calendar_v2_design_review`；第二轮 `/root/calendar_v2_design_review_round2`；第三轮 `/root/calendar_v2_design_review_round3`；当前完整复审 `/root/calendar_v2_design_review_round4`
- Raw output: 第四轮从 design/checklist、requirement、roadmap/goal-state 与真实代码重新核验，实际运行 workflow resolver、YAML/命令与 git 清洁度检查，结论为 `passed`，无 blocking / important；一个基线表述 nit 已由 focused closure 收紧
- Merge policy: 独立 reviewer 的 ownership、Settings seam、代码现状、命令与 residual risk 均由主 agent 对照仓库事实复核；reviewer 全程只读，未写 design/checklist/review
- Gate effect: design-review gate 已通过；本 feature 是 roadmap-owned Goal child，后续必须回交 parent `cs-epic`，本报告不单独授权绕开 parent handoff 进入实现

## 2. Design Summary

- Goal: 保留现有可靠排期写流程，补齐账号可约偏好和批量展示摘要，把档期页升级为月/周双视图、空档回答、转场提醒及桌面/移动完整操作工作台。
- Key contracts: `Settings.availability` 为真实工作窗口；shoot slot 批量附加价格、收款和拍摄类型摘要；openings/overview/布局在前端纯计算；data export 同步有效 Settings 并升 `schema_version=2`；详情按 workspace content-box 宽度切换非模态侧栏或模态底部抽屉。
- Interface seams: Settings HTTP/OpenAPI 与局部 strict decoder；纯 `AvailabilitySettingsDraft` hydrate/validator/serializer；Calendar pure model；主窗口/openings/conflict preview 各自的 range key + generation；schedule repository 的同库批量展示读模型。
- Steps: 8 步；风险热点是 OpenAPI 数字星期 codegen、Settings migration/dataexport parity、跨域批量摘要、DST、三类请求竞态、容器驱动布局和既有 `ScheduleSlotDialog` 可靠性回归。
- Checks: 15 项，覆盖名词契约、编排、挂载点、范围守护、A1–A17、Settings 自动测试 seam 与 DoD；全部保持 pending，供 Goal ledger 执行。
- Baseline / validation: 当前可运行 pre-implementation baseline 与未来 S7 新增的 `npm run test:settings` 已区分；最终门禁为 `make generate-check`、focused Go tests、schedule/settings 前端专项测试、build/lint 与 `make check`。

## 3. Findings

### blocking

none。

### important

none。

### nit

none。第四轮 `FDR4-N01` 已在本轮 focused closure 关闭。

### suggestion

- S7 同时包含 Settings availability 切片和 Calendar 视觉/可访问性收尾。当前仍有可证伪 exit signal，不阻塞执行；Goal ledger 若需要更细失败归因，可在不改变范围的前提下拆成两个实现 unit，但不得扩大为 SettingsPage 整页重构。
- 实现 S7 时必须同时新增 `test:settings` package script 并注册进 canonical `make check`；该建议已收编为 S7 的明确交付边界。

### learning

- 本仓库已有“纯 TypeScript module + Node `--test --experimental-transform-types`”测试模式；Settings availability 不需要为自动证据引入 React Testing Library、jsdom 或整页 form 架构。
- 扩展公共 Settings 必须扫描 dataexport 这类显式列清单消费者；仅改 Settings repository 会静默丢失非默认导出值。
- 响应式一旦改变 modal/non-modal 和 focus trap 语义，就必须有可观察的 workspace layout-mode seam，不能仅依赖 viewport media query。
- 复用可靠写对话框仍需保护已有异步竞态；conflict preview 的 generation 修复可以保持 journal/Idempotency-Key/unknown recovery 边界不动。

### praise

- Settings seam 边界准确：`fromSettings`、validator、serializer 是纯函数，首次 GET/PATCH 成功才 rehydrate，失败只保留错误与草稿，不要求重构 SettingsPage 其他区域。
- Roadmap 硬契约没有被绕开：availability、默认值、Temporal compatible、批量摘要与 export v2 已同步到 requirement/roadmap/items/goal-state，旧 schedule-calendar/data-export done 历史保持不变。
- schedule 摘要沿用 `AccountScope` 内同库批量装配，避免 service-to-service 假 adapter 和 N+1。
- 原型的周/月、冲突泳道、详情侧栏/底部抽屉、空档复制和移动 CRUD 被迁移为生产契约，同时明确排除 openings/overview/book endpoint、主动消息、拖拽、重复规则与多窗口。

## 4. User Review Focus

- 用户已拍板：新 roadmap 增量、export schema v2、默认周视图、工作窗口默认值、14/8/5 空档口径、利用率口径、Temporal `compatible`、单日单窗口和明确不做范围。
- implement 必须遵守：先固定 OpenAPI 数字星期 codegen 结果；Settings 局部 strict decode 与损坏 JSON fail-closed；dataexport 非默认 parity；schedule 摘要批量装配；所有 range/generation latest-wins；Settings 失败不伪造可约；继续复用 `ScheduleSlotDialog` 的 journal、幂等、unknown recovery、历史补录与删除语义。
- code review / QA / acceptance 重点复核：DST gap/fold、跨日/取消/利用率、迟到响应、1600/1280/375 的实际 workspace 宽度与 mode、DOM role/aria/focus、页面级 overflow、移动写操作、Settings 保存失败保草稿与成功按服务端响应 rehydrate。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design A1–A17、反向范围守护及对应 S1–S8 / 命令动作完整 | implementation 按场景留证 |
| DoD Contract | pass | E | Design / Implementation / Review / QA / Acceptance DoD、四条 core command 与 required artifacts 齐全 | none |
| Steps and checks traceability | pass | E | 8 steps/15 checks 均能回到名词、编排、挂载点、范围或验收来源 | S7 可选拆 unit，不改契约 |
| Roadmap contract compliance | pass | E+C | roadmap §4.2/§4.3/§4.6、items 与 goal-state 唯一认领；resolver 无 owner error | 回交 parent epic |
| Module interface design | pass | C | Settings/Schedule/Calendar 代码事实支撑 HTTP、draft、pure model、generation 与批量摘要 seam | implementation 先做 codegen spike |
| Validation and artifacts | pass | E+C | design/checklist CMD-001–004 一致；S7 明确创建并注册 `test:settings` | 记录现有依赖环境红灯 |

Summary: E=3，C=1，E+C=2，H=0；H-only core checks=none。

## 6. Residual Risk

- OpenAPI 数字属性 `"1"`–`"7"` 经 oapi-codegen/openapi-typescript 后的 Go/TS 字段名与 nullable shape 尚无真实生成证据；S1 必须先 spike/codegen，不得用手写 DTO 绕过。
- 当前 worktree 的 `npm run test:schedule` 因缺少已声明的 `@js-temporal/polyfill` 运行依赖而失败；开工前应恢复/核对依赖并把环境红灯与 feature 代码红灯分开记录。
- 1600/1280/375 的 workspace width、role/aria/focus/overflow 仍依赖真实浏览器证据；纯 Node 测试不能替代 DOM/UAT。
- Parent roadmap 当前仍为 `status: handoff`、`current_feature_index: 11`，指向实施中的 `v1-hardening`；calendar-v2 是 pending child。即使本 design review passed，也必须让 parent `cs-epic` 状态机决定能否并行推进，不能私自改 current index 或清除 handoff。

## 7. Verdict

- Status: passed
- Next: design-review 已通过；用户批准已记录。作为 roadmap-owned Goal child，运行 workflow resolver 并回交 `cs-epic`；若 parent handoff 阻塞，保持仓库状态并请求 owner 决定等待或显式解耦，不得自行降级为 Standard。

## 8. Focused Closure

- Closed findings: `FDR4-N01`；同时收编 reviewer suggestion `FDR4-S02`。
- Attributed delta: design §1.8 区分当前 pre-implementation baseline 与 S7 才新增的 `test:settings`；design/checklist S7 明确创建 package script 并注册进 `make check`。
- Verification: checklist/roadmap items/goal-state YAML validator passed；`git diff --check` passed；U+FFFD 与精确 placeholder token 扫描无命中；feature resolver 不再报告 roadmap ownership error。
- Classification: 仅澄清命令生命周期并补齐既定测试脚本的 canonical 注册位置，不改变行为、公开契约、架构边界、验收语义或 feature 范围，因此按 focused closure 关闭，无需第五轮独立复审。
