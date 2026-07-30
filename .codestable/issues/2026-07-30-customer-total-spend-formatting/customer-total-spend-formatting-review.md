---
doc_type: issue-review
issue: 2026-07-30-customer-total-spend-formatting
status: passed
reviewer: subagent
reviewed: 2026-07-30
round: 1
lane_a_state: completed
lane_a_ref: "/root/money_format_review"
lane_a_reason: "独立只读审查完成；spec 合规与代码质量均通过，无 blocking/important/nit"
lane_b_state: skipped
lane_b_ref: ""
lane_b_reason: "skipped-scope-ambiguous：工作区含本轮范围外 dirty/untracked 文件，按协议改为主 agent 对 current_scope_files 做本地行级审查"
---

# customer-total-spend-formatting 代码审查报告

## 1. Scope And Inputs

- Issue report: `.codestable/issues/2026-07-30-customer-total-spend-formatting/customer-total-spend-formatting-report.md`
- Fast-path approval: `.codestable/issues/2026-07-30-customer-total-spend-formatting/approval-report.md#issue-fast-path`
- Fix note: `.codestable/issues/2026-07-30-customer-total-spend-formatting/customer-total-spend-formatting-fix-note.md`
- Implementation evidence: fix-note 第 4、5 节与本轮对话中的测试、构建和浏览器验证结果。
- Diff basis: 当前 unstaged/untracked 工作区；本轮可归因范围为 `Makefile`、`frontend/package.json` 的测试脚本行、`CustomerDetailPage.tsx` 的格式化 import/调用、`customerDetailMoney.ts`、`customer-money.test.ts`。
- Review mode: initial。
- Baseline dirty files: 当前分支另有 `frontend/package-lock.json`、多个组件/页面/CSS、图片和工具目录等既有改动；结论不归因这些范围外变更。

### Independent Review

- Detection: 独立隔离 Task agent 可用；OCR CLI v1.x 可用且连接测试成功。
- 环节 A 独立隔离 Task agent: independent-agent + completed，ref `/root/money_format_review`；完成后核对工作区，没有发现该 agent 写入文件。
- 环节 B OCR CLI: skipped；未提交工作区含大量本轮范围外变更，禁止裸 workspace 扫描，改为主 agent 本地行级审查本轮文件。
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded。
- Merge policy: 独立 reviewer findings 已逐条用 OpenAPI、生产代码、测试、CSS 与运行结果核验，并与主 agent 本地行级审查合并。
- Gate effect: 环节 A 已完成；`reviewer: subagent` 满足 issue review gate。

## 2. Diff Summary

- 新增：`frontend/src/pages/customerDetailMoney.ts`、`frontend/scripts/customer-money.test.ts`。
- 修改：`frontend/src/pages/CustomerDetailPage.tsx`、`frontend/package.json`、`Makefile`。
- 删除：none。
- 未跟踪 / staged：上述两个新增前端文件与 issue 目录未跟踪；本轮没有 staged 文件。
- 风险热点：用户可见金额显示、分到元的精度、locale 输出、测试门禁接线；无 API、后端数据、权限、并发或账号隔离变更。

## 3. Adversarial Pass

- 假设的生产 bug：边界分值或 locale 行为可能导致金额精度、符号、千分位或响应式布局与预期不一致。
- 主动攻击过的反例：`0`、`1`、`5`、`10`、`99`、`101`、`113800`、PostgreSQL 单价 `INTEGER` 上界、`Number.MAX_SAFE_INTEGER`；Node 与浏览器 locale；测试门禁接线；320/584/1081/1440px 布局；dirty diff 归因。
- 结果：已批准业务样例及常见整数边界正确；1081/1440px 一行显示且无溢出，320px 有意按现有 `overflow-wrap:anywhere` 分为两行但无裁切；极端聚合总额的浮点精度进入 residual risk，不阻塞当前业务修复。

## 4. Findings

### blocking

none。

### important

none。

### nit

none。

### suggestion

- REV-S01 `frontend/scripts/customer-money.test.ts:4-10`：后续增加页面接线级 component/integration 测试；当前纯函数测试不会在页面将来绕过 helper 时失败。本轮已用真实浏览器复现补足实现证据，不阻塞。
- REV-S02 `frontend/scripts/customer-money.test.ts:6-10`：后续可改为表驱动用例，并补 `1`、`99`、`101` 与 `2147483647`，提高进位和大额回归的诊断精度。

### learning

- `total_order_amount` 是 required integer 且订单价格受非负约束；本轮无需为负值、`NaN`、`Infinity` 或退款语义泛化 helper。若领域契约将来引入负消费或可空统计，应先更新 API/领域语义。

### praise

- OpenAPI 的“分”契约、生成 TS 类型、生产 helper、页面调用和测试引用形成同一条可追溯链；测试没有复制生产算法。
- `package.json` 与 `Makefile` 均已接线，回归测试会进入项目聚合门禁。
- 可归因生产改动保持在一个 import 与一个渲染调用，没有扩成全站金额重构。

## 5. Test And QA Focus

- QA 必须重点复核：客户详情样例 `113800 → ¥1,138.00`、无订单 `0 → ¥0.00`、分/角进位、1080/1081px 响应式临界点、移动端两行金额的可读性。
- Evidence pack residual risks / gate warnings：全量额外测试发现一个与本轮无关的既有 `prototype-contract` 失败，已记录在 fix-note 遗留事项。
- 建议新增或加强的测试：后续补页面接线级 component/integration 测试；helper 边界用例可改为表驱动。
- 不能靠 review 完全确认的点：Safari 的 `toLocaleString('zh-CN')` 输出未在本轮实机复核；当前 Chrome 与 Node 输出一致。

## 6. Residual Risk

- `frontend/src/pages/customerDetailMoney.ts:2` 先执行浮点除法。对 `Number.MAX_SAFE_INTEGER` 分会显示 `¥90,071,992,547,409.90`，而整数末两位为 `91`；约需数百万笔接近 PostgreSQL `INTEGER` 上界的订单才可能触达，当前业务概率极低。若未来允许这种总额，应为 API 定义业务上限，或用整数拆分元/分；超过安全整数还需先解决 JSON `number` 契约。
- 320px 下 `¥1,138.00` 按现有移动端 `overflow-wrap:anywhere` 显示为两行但无溢出；584/1081/1440px 均为一行无溢出。若产品要求货币值永不换行，应另行确认 CSS 范围，不在本次逻辑修复中偷改。
- 工作区存在大量范围外 dirty 文件；尤其 `frontend/package.json` 和 `CustomerDetailPage.tsx` 同时包含用户既有改动。若后续提交，必须按 hunk 核对 staged diff，避免把未审变更错误归入本 issue。
- `prototype-contract.test.mjs` 有一个与本轮无关的既有失败；已在 fix-note 记录，不能把本 review 的 passed 解读为全仓测试完全绿色。

## 7. Verdict

- Status: passed。
- Next: 返回 `cs-issue` fix 阶段，持久化修复完成确认点，等待 owner 验收；未获单独授权前不 commit。

## 8. Focused Closure

none。
