---
doc_type: feature-qa
feature: 2026-07-08-package-catalog
status: passed
tested: 2026-07-09
round: 1
---

# package-catalog QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-08-package-catalog/package-catalog-design.md`
- Checklist: `.codestable/features/2026-07-08-package-catalog/package-catalog-checklist.yaml`
- Review: `.codestable/features/2026-07-08-package-catalog/package-catalog-review.md`（status: passed，round: 6）
- Evidence pack: none；同目录已有实现期截图，本轮新增 QA 截图：
  - `.codestable/features/2026-07-08-package-catalog/qa-packages-created-desktop.png`
  - `.codestable/features/2026-07-08-package-catalog/qa-packages-edited-desktop.png`
  - `.codestable/features/2026-07-08-package-catalog/qa-packages-archived-desktop.png`
  - `.codestable/features/2026-07-08-package-catalog/qa-packages-mobile-375.png`
- Gate results: none
- DoD results: none
- Diff basis: `git status --short` 显示当前 dirty / untracked 均为 package-catalog 实现、生成物、需求 / roadmap 回写、CodeStable 产物和 QA 截图；`api.gen.go` 与 `schema.d.ts` 仍为 staged，源码 / 文档 / 截图仍 unstaged。
- Baseline dirty files: none（未发现可归因为 feature 外的既有 dirty 文件）。提交前仍需整体 stage，避免生成物与源码错位。
- Feature type: functional
- Core evidence gate: API CRUD + 校验 + 账号隔离、OpenAPI 生成物、PackagesPage 真实 API 迁移、价格元/分换算与两位小数输入、上架/下架/删除确认、移动端和错误状态均需要运行证据。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | A1 / CMD-001 | core-functional | 项目基线 | command | `make check` | build/lint/test/generate-check 全部 0 | pass |
| QA-002 | A2 / CMD-002 | core-functional | OpenAPI 生成物零漂移 | command + diff | `make generate`；`git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` | 退出码 0 | pass |
| QA-003 | A3 / A4 | core-functional | API 创建、负价格拒绝、0 元合法、未认证拒绝 | API + browser fetch | Playwright 页面内 fetch + 网络响应 | 400 validation_failed、201 base_price=0、401、临时记录可删除 | pass |
| QA-004 | A5 / review focus | core-functional | 基础价逐字输入与保存，不允许超过两位小数，不静默四舍五入 | unit + browser | `npm run test:package-price`；Playwright 输入 `680.`→`680.5`→`680.55`、`12.345`、`0.004`、保存 `0.05` | helper 拒绝超精度；UI 第三位小数不进入值；0.05 保存为 5 分；680.55 保存为 68055 分 | pass |
| QA-005 | A5 | core-functional | PATCH 只改价、交付参数不丢 | integration + browser | `go test ./...` + 浏览器编辑保存 | PATCH 请求体含 base_price=68055，卡片显示 ¥680.55，其他字段保持 | pass |
| QA-006 | A6 / A7 / A8 | core-functional | active/archived/all 过滤与下架/上架语义 | integration + browser | 浏览器下架后 active 消失、archived 可见；测试覆盖 status 过滤与非法分页 | 下架无 in-use 拦截，archived tab 可见，active 不混旧数据 | pass |
| QA-007 | A9 / A10 | core-functional | not_found 与账号隔离 | integration | `cd backend && go test ./...` | 跨账号列表不可见，PATCH/DELETE 对方套系 404 | pass |
| QA-008 | A11 | core-functional | 浏览器完整流程 | browser | 新建→列表可见→编辑改价→下架→删除二次确认 | 请求/响应与 UI 一致，删除后列表移除 | pass |
| QA-009 | A12 / review residual | core-functional | 错误状态、401、移动端 375px、删除弹窗键盘取消 | browser + API | Playwright 强制 archived GET 500、无 token fetch、375px 截图、Escape 关闭删除弹窗 | 失败不展示旧 filter 卡片；401；无横向溢出；Escape 关闭 | pass |
| QA-010 | A13 / A15 | core-functional | 删除 in-use 域生长与范围守护 | test + source check + API | `go test ./...`；grep/source 复核 repository | 无 orders 表删除 204；orders 表存在且引用时 package_in_use；无订单/档期行为实现 | pass |
| QA-011 | A14 | supporting | 清洁度 | grep + diff | `git diff --check`；`git diff --cached --check`；grep debug/TODO/原型桩/UI 库 | 无空白错误、调试输出、TODO/FIXME/XXX、原型桩 import、新 UI 库 | pass |
| QA-012 | review residual | supporting | 混合 staged/unstaged/untracked 提交风险 | git status | `git status --short` | QA 记录为提交前风险，不影响运行验收 | pass |

## 3. Command Results

- `cd frontend && npm run test:package-price` -> exit 0：4 tests pass；覆盖 `680.`、`680.5`、`680.55`、`0.05`、`12.345`、`0.004`、超 int4 分值与不四舍五入。
- `cd frontend && npm run build` -> exit 0：TypeScript build + Vite build pass。
- `cd backend && go test ./...` -> exit 0：package domain、HTTP API、store/customer/platform 测试 pass。
- `make generate` -> exit 0：Go server types + TS schema 生成成功。
- `git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` -> exit 0：生成物相对工作树零漂移。
- `make check` -> exit 0：frontend build、webui sync、backend build、golangci-lint、oxlint、backend tests、package price tests、generate-check 全绿。
- `git diff --check` -> exit 0：unstaged diff 无 whitespace error。
- `git diff --cached --check` -> exit 0：staged 生成物 diff 无 whitespace error。
- `make db-up` -> exit 0：本地 Postgres healthy；浏览器 QA 使用 `http://localhost:5173` + 8080 现有本地后端 + 本地 Postgres。

## 4. Scenario Results

- [x] QA-003 API 创建/校验：pass
  - Evidence: 页面内 fetch：未认证 GET `/api/v1/packages` -> 401；POST `base_price:-1` -> 400 `validation_failed`；POST `base_price:0` -> 201、`base_price=0`、`status=active`；随后 DELETE 临时 0 元套系 -> 204。
  - Notes: 覆盖 A3/A4 的真实 API 行为；缺字段/非法枚举由 backend HTTP/domain 测试覆盖。

- [x] QA-004 价格输入：pass
  - Evidence: 浏览器输入 `12.345` / `0.004` 后 input 值为空（`type=number` + React guard 未接受超精度值）；逐字输入 `680.` 后 DOM value 读作 `680`，继续输入 `5` 得 `680.5`，再输入 `5` 得 `680.55`；保存 PATCH 请求体 `base_price=68055`。
  - Evidence: 创建时输入 `0.05`，POST 请求体 `base_price=5`，响应 `base_price=5`，卡片显示 `¥0.05 /1.5 小时`。
  - Notes: `input[type=number]` 的 DOM value 会规范化尾随点，但真实逐字路径可继续得到 `680.5` / `680.55`；用户输入第三位小数不会进入受控值，也不会被保存成四舍五入后的金额。

- [x] QA-005 PATCH 与字段保持：pass
  - Evidence: 编辑 `QA 套系稳定 1783565686576`，PATCH 请求体含 `base_price:68055`，duration / shot range / raw delivery / retouch / note 保持原值；卡片显示 `¥680.55`、90 分钟、60-80 张、50 张底片、8 张精修。

- [x] QA-006 状态过滤：pass
  - Evidence: 下架 PATCH 请求体 `{ "status": "archived" }`，响应 200；active tab 中 QA 套系消失；archived tab 中卡片可见并带「已下架」与「重新上架」按钮。

- [x] QA-008 完整浏览器流程：pass
  - Evidence: Playwright 完成新建 -> 列表卡片可见 -> 编辑改价 -> 下架 -> archived 可见 -> 删除确认；DELETE 响应 204；删除后 archived 列表不再含 QA 套系。
  - Screenshots: `qa-packages-created-desktop.png`、`qa-packages-edited-desktop.png`、`qa-packages-archived-desktop.png`。

- [x] QA-009 UI 错误与移动端：pass
  - Evidence: 强制 archived GET 500 时页面显示错误，且不显示上一个 active filter 的卡片，也不显示旧 QA archived 卡片；删除弹窗包含套系名，按 Escape 可关闭；375px viewport `scrollWidth=375`、`innerWidth=375`，无横向溢出。
  - Screenshots: `qa-packages-mobile-375.png`。

- [x] QA-010 删除 in-use 与范围守护：pass
  - Evidence: `backend/internal/package/package_test.go` 覆盖无引用删除 204/二次删除 not_found、跨账号删除 not_found、orders 表存在且 scheduled/cancelled 引用时 `ErrPackageInUse`；repository `Delete` 在事务内 `findPackageForUpdate` 后 `countOrderReferences`，无 orders 表返回 0 放行。
  - Notes: 本 feature 不实现订单/档期行为；真实 order 域接通后的 409 已由当前领域方法和测试表模拟覆盖。

## 5. Findings

### failed

- none

### blocked

- none

### residual-risk

- 提交前仍需整体 stage：当前 `api.gen.go` 与 `schema.d.ts` 已 staged，源码 / 文档 / QA 截图仍 unstaged / untracked；这是提交流程风险，不影响本轮 QA verdict。
- Browser Bridge 的 Chrome 扩展没有连接 tab，本轮浏览器证据改用 headless Chromium Playwright。它覆盖真实 DOM、真实 Vite、真实后端 API 与本地 Postgres，但没有覆盖用户本机 Chrome 扩展路径；该差异不阻塞本 feature。

## 6. Cleanliness

- Debug output: pass（feature 相关源码 grep 未命中 `console.log` / `fmt.Println`）
- Temporary TODO/FIXME/XXX: pass（feature 相关源码 grep 未命中）
- Commented-out code: pass（未发现临时注释代码）
- Unused imports / dead code from this feature: pass（`make check`、golangci-lint、oxlint 通过）
- Out-of-scope files: pass（dirty 文件均归属 package-catalog 实现、需求/roadmap 回写、CodeStable 产物或生成物；未发现订单/档期行为实现）

## 7. Verdict

- Status: passed
- Next: `cs-feat-accept`
