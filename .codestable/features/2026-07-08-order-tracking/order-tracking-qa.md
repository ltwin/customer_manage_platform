---
doc_type: feature-qa
feature: 2026-07-08-order-tracking
status: passed
tested: 2026-07-09
round: 1
---

# order-tracking QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-08-order-tracking/order-tracking-design.md`
- Checklist: `.codestable/features/2026-07-08-order-tracking/order-tracking-checklist.yaml`
- Review: `.codestable/features/2026-07-08-order-tracking/order-tracking-review.md`（status=passed，round=3，blocking/important/nit 均 none）
- Evidence pack: `.codestable/features/2026-07-08-order-tracking/evidence/*.png`，本轮新增 `qa-orders-unpaid-desktop.png`、`qa-create-dialog-101-options-desktop.png`、`qa-orders-unpaid-375.png`
- Gate results: none
- DoD results: none
- Diff basis: `git status --short` 显示 order-tracking 实现、OpenAPI/生成物、roadmap/spec/evidence 均在当前未 staged diff；`git diff --cached --name-only` 为空
- Baseline dirty files: `.codegraph/` 为 CodeGraph 运行期索引目录，按 dot-dir 规则排除；其余 dirty/untracked 文件可归因于本 feature 或本轮 QA 证据
- Feature type: functional
- Core evidence gate: 后端订单 API、状态机/不变量、查询/聚合/merge/in-use/删除、跨账号隔离、HTTP 错误语义、前端订单页/约单弹窗/移动布局均需要运行证据；本轮用 Testcontainers 集成测试、HTTP 集成测试、全量 build/lint/test/generate-check、真实 Chrome headless 浏览器截图与 DOM 抽取覆盖

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | design A1-A1h/A9b | core-functional | 建单、最小建单、补录直达、客户/套系引用校验、price 负数 | integration/httpapi | `cd backend && go test -count=1 ./internal/order ./internal/platform/httpapi` | 201/400/404/409 口径与契约一致 | pass |
| QA-002 | design A2-A9c | core-functional | 14 条状态边、前跳边、时间戳自动写入、D6b 不变量、PATCH 回滚 | unit/integration | `cd backend && go test -count=1 ./internal/order` | 合法跃迁通过，非法跃迁/不变量破坏拒绝且字段不落库 | pass |
| QA-003 | design A10-A14 | core-functional | `GET /orders` 三维过滤、unpaid_balance 收窄口径、摘要、稳定分页、跨账号隔离 | integration/browser | 后端目标测试 + Chrome `/orders` 未收尾款筛选 | consulting/scheduled/cancelled 未结清不出现在 unpaid；跨账号不可见 | pass |
| QA-004 | design A15-A20/A29-A30 | core-functional | customer/package 聚合真实值、merge 订单迁移、package in-use 双口径、终态删除 | integration | `cd backend && go test -count=1 ./internal/customer ./internal/package ./internal/order` | cancelled 不计聚合但阻止 package delete；删除终态即时反映 | pass |
| QA-005 | design A27/A24-A28 | supporting | 路由范围守护与明确不做 | integration/diff | `cd backend && go test -count=1 ./internal/platform/httpapi` + diff/grep | 未实现域仍 404；无支付/档期/提醒/批量导入实现 | pass |
| QA-006 | review §5 | core-functional | 建单并发 customer merge / package archive / package delete 不留下脏引用 | integration | `cd backend && go test -count=1 ./internal/order` | `FOR UPDATE` 锁等待用例通过，无 merged/archived 脏引用或 500 | pass |
| QA-007 | review §5/design A21-A23 | core-functional | 全局建单第 101+ 客户/套系可选；补录状态不出现 consulting；移动端未收尾款布局 | browser | Chrome CDP + 临时 QA DB 105 customers/packages + screenshots | select 拉全页 total；backfill 状态只有 7 项且无「咨询」；375px 不重叠 | pass |
| QA-008 | design DoD/3.y | supporting | OpenAPI 生成物无新增漂移，orders Go interface/TS schema 同步 | generate/typecheck | `make generate` 前后 shasum 对比；隔离 index `make check` | 重生成不改变当前生成物；全量 build/lint/test/generate-check 通过 | pass |
| QA-009 | attention/design 清洁度 | non-functional | 调试输出、临时 TODO/FIXME、注释代码、无用 import、原型桩残留 | diff/grep/lint | `git diff --check`、targeted `rg`、`npm run lint`、`golangci-lint` | 无本 feature 源码清洁度问题 | pass |
| QA-010 | checklist steps / review basis | supporting | review 是否基于当前 diff，CodeStable worktree gate 是否允许进入 acceptance | gate/diff | `python3 .codestable/tools/codestable-worktree-gate.py --root . --json commit --unit .codestable/features/2026-07-08-order-tracking` | gate ok，staged 为空，required review 指向当前 review | pass |

## 3. Command Results

- `make check` → exit 2：build、lint、backend `go test ./...`、frontend `test:package-price` 均已通过；最后 `generate-check` 因当前 feature 的 `api.gen.go` / `schema.d.ts` 仍是未提交工作区 diff，`git diff --exit-code -- ...` 按 HEAD 比较而报差异。该失败不是业务断言或生成漂移，已用 QA-008 的隔离 index 复跑覆盖。
- `tmp_index=$(mktemp ...) && GIT_INDEX_FILE="$tmp_index" git add -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts && GIT_INDEX_FILE="$tmp_index" make check` → exit 0：不改真实 git index，只把当前生成物放入临时 index 后，全量 build/lint/test/generate-check 通过。
- `cd backend && go test -count=1 ./internal/order ./internal/customer ./internal/package ./internal/platform/httpapi ./internal/platform/store` → exit 0：order 18.030s、customer 31.406s、package 12.707s、httpapi 21.089s、store 19.585s。
- `git diff --check` → exit 0：无 whitespace/error marker。
- `shasum backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts > before && make generate && shasum ... > after && diff -u before after` → exit 0：重生成前后生成物哈希一致。
- `python3 .codestable/tools/codestable-worktree-gate.py --root . --json commit --unit .codestable/features/2026-07-08-order-tracking` → exit 0：`ok=true`，`findings=[]`，`warnings=[]`，staged files 为空。
- Browser QA setup/action → exit 0：临时 QA DB + 本地后端/前端 + Google Chrome headless CDP；创建 105 个客户、105 个套系、1 个 delivered 未收尾款单、1 个 consulting 单；验证后已停止进程并 drop QA DB。

## 4. Scenario Results

- [x] QA-001 建单与补录直达：pass
  - Evidence: `backend/internal/order` 与 `backend/internal/platform/httpapi` 非缓存测试通过，覆盖默认 consulting、补录 delivered/closed/cancelled、缺时间戳 400、closed 未结清 409、套系 active/archived 分叉。
  - Notes: 归因本 feature。
- [x] QA-002 状态机与字段修正不变量：pass
  - Evidence: `backend/internal/order` 非缓存测试通过；review-fix 后并发锁边界测试同包覆盖。
  - Notes: D6b 三条不变量在创建/跃迁/字段修正路径均有测试证据。
- [x] QA-003 查询与 unpaid_balance：pass
  - Evidence: Chrome `/orders` 点击「未收尾款」后 desktop/mobile 均只显示 `QA未收尾款单`，DOM 抽取 `hasDeliveredUnpaid=true`、`hasConsultingOrder=false`；截图：`qa-orders-unpaid-desktop.png`、`qa-orders-unpaid-375.png`。
  - Notes: 与后端集成测试的收窄口径互相印证。
- [x] QA-004 聚合/merge/in-use/delete：pass
  - Evidence: `backend/internal/customer`、`backend/internal/package`、`backend/internal/order` 非缓存测试通过。
  - Notes: list count 与 delete in-use 双口径由 package 测试钉死。
- [x] QA-005 HTTP 路由与范围守护：pass
  - Evidence: `backend/internal/platform/httpapi` 非缓存测试通过；router scope guard 测试覆盖未实现域端点。
  - Notes: 未发现 schedule/reminder/dashboard/settings 被意外注册。
- [x] QA-006 review 并发焦点：pass
  - Evidence: `backend/internal/order` 非缓存测试通过；review 记录的 create-vs-customer-merge、create-vs-package-archive/delete、create-then-package-delete 用例均在该包内执行。
  - Notes: `pg_stat_activity` 观察 `FOR UPDATE` lock wait，避免固定 sleep 假阳性。
- [x] QA-007 前端 101+ 与补录状态：pass
  - Evidence: Chrome DOM 抽取：客户 select `count=106` 且含 `QA客户105`；套系 select `count=106` 且含 `QA套系105`；补录状态 select `count=7`，options=`定档/已拍摄/已选片/精修中/已交付/完结/取消`，`hasConsultingStatus=false`。截图：`qa-create-dialog-101-options-desktop.png`。
  - Notes: 106 = 105 条真实数据 + 1 个占位选项；验证了 review §5 指出的 101+ 分页选项风险。
- [x] QA-008 生成物与全量门禁：pass
  - Evidence: 隔离 index `make check` exit 0；`make generate` 前后 shasum 一致。
  - Notes: 真实 index 未 staging，故普通 `make check` 的 generate-check 退出 2 作为 residual risk 记录。
- [x] QA-009 清洁度：pass
  - Evidence: `git diff --check` exit 0；目标源码 grep 无 `fmt.Println`/`console.log`/`debugger`/临时 TODO/FIXME/XXX/订单 UI 原型桩 import；Go/TS lint 在 `make check` 中通过。
  - Notes: broad grep 命中的 `prototypeData` 均为既有 prototype 模块或设计文档说明，不是本轮订单 UI 残留。

## 5. Findings

### failed

none

### blocked

none

### residual-risk

- 普通 `make check` 在当前未 staged feature diff 上仍会于 `generate-check` exit 2，因为生成物相对 HEAD 有预期 diff；本轮用临时 `GIT_INDEX_FILE` 验证“当前生成物已纳入 index 时”全量 `make check` 可通过，并用 shasum 证明 `make generate` 不再改变生成物。进入 commit/acceptance 前，真实 staging 范围需要包含 `api.gen.go` 与 `schema.d.ts` 后再跑普通 `make check`。
- `.codegraph/` 仍是运行期 dot-dir 索引目录；后续 staging/commit 需继续排除，避免混入无关运行产物。
- Browser QA 使用临时 QA DB + Chrome CDP 手工脚本取得运行证据，没有新增持久化 e2e 测试文件；如后续把全量选择器升级为搜索式选择器，需要补对应 UI 自动化。

## 6. Cleanliness

- Debug output: pass
- Temporary TODO/FIXME/XXX: pass
- Commented-out code: pass
- Unused imports / dead code from this feature: pass
- Out-of-scope files: pass（`.codegraph/` 需提交前排除；其余 dirty 可归因于 order-tracking）

## 7. Verdict

- Status: passed
- Next: `cs-feat-accept`
