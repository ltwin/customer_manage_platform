---
doc_type: feature-review
feature: 2026-07-08-order-tracking
status: passed
reviewer: subagent
reviewed: 2026-07-09
round: 3
---

# order-tracking 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-08-order-tracking/order-tracking-design.md`
- Checklist: `.codestable/features/2026-07-08-order-tracking/order-tracking-checklist.yaml`（steps 全 done；checks 仍 pending，留给 QA/acceptance）
- Evidence pack: `.codestable/features/2026-07-08-order-tracking/evidence/*.png`
- Gate results: none
- DoD results: none
- Implementation evidence: 当前对话中的 review-fix 修复记录、native-agent 复审结果、本地主验证命令
- Diff basis: `git status --short --untracked-files=all` 显示 order-tracking 实现、OpenAPI/生成物、roadmap/spec、截图证据均未 staged；`git diff --cached --name-only` 为空
- Baseline dirty files: `.codegraph/.gitignore` 属 CodeGraph 运行期索引目录，按 dot-dir 规则排除；其余可归因于本轮 order-tracking

### Independent Review

- Detection: 未暴露 `mcp__paseo__create_agent`；当前宿主 native subagent 可用并已完成；`ocr` CLI 可用且 `ocr llm test` 成功
- 环节 A 独立隔离 Task agent: `native-agent` + `completed`（最终复审 verdict: passed；blocking/important/nit 均 none）
- 环节 B OCR CLI: `skipped-scope-ambiguous`（当前未提交 diff 混入 `.codegraph/.gitignore` 与 `.codestable/` spec/evidence，不能用裸 workspace scope 扫）
- OCR severity mapping: High->blocking/important, Medium->nit/suggestion, Low->discarded
- Merge policy: native subagent findings 已逐条本地核验；OCR 跳过后由本地行级审查补足
- Gate effect: 无 blocking / important，允许进入 feature QA

## 2. Diff Summary

- 新增：`backend/internal/order/*`、`backend/internal/platform/httpapi/orders.go`、`orders_test.go`、`0005_orders` 迁移、`frontend/src/components/orders/OrderWorkspace.tsx`、`frontend/src/pages/OrdersPage.tsx`、feature design/checklist/design-review/worktree override 与截图证据
- 修改：`api/openapi.yaml`、`api.gen.go`、`schema.d.ts`、`oapi-codegen.yaml`、router/server wiring、customer/package 聚合与测试、`AccountScope.ScalarAggregate`、前端 nav/routes/API client/CSS/CustomerDetailPage
- 删除：none
- 未跟踪 / staged：新增实现与 spec/evidence 均未跟踪；staged none
- 风险热点：状态机不变量、PATCH nullable 语义、订单时间线、引用状态并发、跨域聚合、终态删除、前端建单下拉规模

## 3. Adversarial Pass

- 假设的生产 bug：订单创建与 customer merge / package archive/delete 并发时仍可能留下脏引用，或者测试只是 sleep 假阳性。
- 主动攻击过的反例：建单同时 merge source customer；建单同时 archive package；建单同时 delete package；建单后删除 package；`scheduled -> shot` 带 `shot_at:null`；`shot -> delivered` 带 `delivered_at:null`；全局页超过 100 个客户/套系；补录模式选择 consulting + archived package。
- 结果：REV-005 已通过创建事务内 `QueryRowForUpdate` 锁读 customer/package 修复；并发测试改用 `pg_stat_activity` 观察实际 `FOR UPDATE` Lock wait，避免固定 sleep 假阳性，并补 package delete 竞态覆盖。REV-006 已按 total 翻页拉全客户/套系选项。REV-007 已在补录状态下拉排除 consulting，并保留防御校验。

## 4. Findings

### blocking

none

### important

none

### nit

none

### suggestion

- [ ] REV-008 customer/package 聚合当前按列表逐项查聚合，v0 可接受，后续数据量上来时可批量聚合成页内 map（来源：native-agent + local）
- [ ] REV-009 `frontend/src/components/orders/OrderWorkspace.tsx:752` 全量分页拉取客户/套系选项已修复前 100 条问题；后续如果客户/套系增长到数百上千，建议升级为搜索式选择器，避免每次打开/刷新订单工作台都拉全量。

### learning

- 引用状态不变量不能只靠外键：FK 保护存在性和 key，不保护 active/merged/archived 这类业务状态。创建路径需要和状态改变路径共享锁边界。
- 并发测试应证明受测查询实际等待目标行锁；`backend/internal/order/order_test.go:635` 通过 `pg_stat_activity` 观察 `FOR UPDATE` Lock wait，比固定等待窗口更可信。
- oapi-codegen 可选字段生成 `*time.Time` 后，`orders.go` 用 raw JSON 恢复了 null 语义；这个信息已经保留到 domain 自动时间戳分支，round 1 的 PATCH null 问题仍保持解除。

### praise

- `backend/internal/order/repository.go:191`、`:206` 已在 create 事务内锁读 customer/package，使建单和 merge/archive/delete 参与同一行锁序列。
- `backend/internal/order/order_test.go:168` 起覆盖 create-vs-customer-merge、package archive/delete 竞态，以及 create 后 package delete `package_in_use`。
- `frontend/src/components/orders/OrderWorkspace.tsx:71` 排除补录 consulting，`:739` 保留防御性校验；UI 与后端 `isBackfillCreate` 语义对齐。
- `fetchAllPages` 已按 `total` 拉完整分页，解决全局建单只看到前 100 个客户/套系的问题。

## 5. Test And QA Focus

- QA 必须重点复核：真实浏览器里全局建单第 101+ 个客户/套系可选；补录模式不出现 consulting；建单同时 merge/archive/delete 不留下 merged customer 脏引用、archived package 新业务引用或 500；selected/retouching 筛选保留。
- Evidence pack residual risks / gate warnings: OCR 因 scope 混入 dot-dir 与 `.codestable/` 产物而跳过；`make check` 在本机 Docker/Testcontainers 层偶发失败（见 Residual Risk），但目标包和单包重跑已通过。
- 建议新增或加强的测试：none for current review-fix；后续若改搜索式选择器，再补 101+ 选项 UI 自动化。
- 不能靠 review 完全确认的点：未做新的浏览器截图；101+ 真实 UI 场景交给 QA gate。

本地验证：

- `cd backend && go test -count=1 ./internal/order ./internal/customer ./internal/package ./internal/platform/httpapi ./internal/platform/store` -> passed
- `cd frontend && npm run build` -> passed
- `cd frontend && npm run lint` -> passed
- `git diff --check` -> passed
- `python3 .codestable/tools/codestable-worktree-gate.py --root . --json commit --unit .codestable/features/2026-07-08-order-tracking` -> passed
- `make generate` 后对 `backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts` 的 diff before/after 一致 -> generated drift not introduced
- `TESTCONTAINERS_RYUK_DISABLED=true go test -count=1 ./internal/platform/httpapi` -> passed
- `TESTCONTAINERS_RYUK_DISABLED=true go test -count=1 ./internal/customer` -> passed

## 6. Residual Risk

- `make check` 在本机 Docker/Testcontainers 阶段两次失败，失败形态为容器连接串 / Ryuk reaper 启动层错误（如 `port "5432/tcp" not found`、`reaper: ... could not start container`），不是业务断言失败；目标包命令与相关单包重跑已通过。QA gate 若环境稳定，应重新跑完整 `make check`。
- `.codegraph/.gitignore` 仍是 dot-dir 运行期文件，提交前需按 scope 过滤，避免混入无关索引产物。
- customer/package 聚合逐项查询仍作为 v0 suggestion 保留；不阻塞本轮 QA。

## 7. Verdict

- Status: passed
- Next: 进入 `cs-feat-qa`；QA gate 需优先补跑完整 `make check`（若 Testcontainers 环境稳定）与 101+ 客户/套系选择场景。
