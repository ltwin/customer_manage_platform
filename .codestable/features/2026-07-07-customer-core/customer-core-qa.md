---
doc_type: feature-qa
feature: 2026-07-07-customer-core
status: passed
tested: 2026-07-07
round: 1
---

# customer-core QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-core/customer-core-design.md`（approved）
- Checklist: `.codestable/features/2026-07-07-customer-core/customer-core-checklist.yaml`（steps 全 done）
- Review: `.codestable/features/2026-07-07-customer-core/customer-core-review.md`（round 2 passed，reviewer: subagent+ocr，基于当前 diff——review-fix 后无新改动）
- Evidence pack / Gate results / DoD results: none（非 goal/gate 模式）
- Diff basis: `develop` 工作区未提交改动（同 review round 2 基线；QA 期间 `make generate` 幂等验证未产生新 diff）
- Baseline dirty files: none（roadmap/README 修改归因本 feature 状态回写）
- Feature type: **functional**（新增受保护 API×3、持久化 schema、前端三页面）
- Core evidence gate: A3-A10（API 行为，integration 测试）、A11-A12（浏览器路径，截图+自动化 checks）、A13（范围守护）、CMD-001~004（命令基线）

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | design A1/CMD-001 | core-functional | 命令基线 | command | `make build` + `make lint` + `make test` | exit 0 | pass |
| QA-002 | design A2/CMD-002 | core-functional | 生成物与契约同步 | command | `make generate` 幂等验证（md5 前后比对） | 零漂移 | pass* |
| QA-003 | design A3/CMD-003 | core-functional | 创建客户+2 身份 | integration | `go test ./...`（TestCustomerAPIRoundtrip 等） | 201+同事务 | pass |
| QA-004 | design A4 | core-functional | 校验失败无半成品 | integration | TestCreateValidationRollback（4 用例 before/after Count） | 400+回滚 | pass |
| QA-005 | design A5 | core-functional | referral 四态 | integration | TestReferralVisibility | 201/400/404/404 | pass |
| QA-006 | design A6 + review focus 1 | core-functional | 非法分页含 page=0 | integration | customers_test（-1/0/page_size=0 → 400） | 400 validation_failed | pass |
| QA-007 | design A7-A8 | core-functional | 列表过滤/聚合零值/created_at desc | integration | TestListFiltersAndPagination | 按契约 | pass |
| QA-008 | review focus 2 | core-functional | 同 created_at 跨页无重复丢行 | integration | TestListPaginationStableAcrossPages | 3 页并集=全集 | pass |
| QA-009 | design A9 | core-functional | 详情完整 shape | integration | Roundtrip detail 断言 | identities/notes/stats | pass |
| QA-010 | design A10 | core-functional | 双账号隔离 | integration | 双账号列表/详情/referrer 测试 | 跨账号 404 | pass |
| QA-011 | design A11 | core-functional | 浏览器 30 秒多身份建档 | browser+screenshot | evidence 截图×6 + browser-evidence.json 逐项核对 | <30s、可搜可查 | pass |
| QA-012 | design A12 | core-functional | 空态/错误/401/长文本/375px/focus | browser+screenshot | 同上（本轮已逐张目验） | 状态可见不溢出 | pass |
| QA-013 | design A13 | core-functional | 范围守护端点 404 | API+integration | PATCH 404 测试 + rangeGuardStatuses 全 404 | 404 | pass |
| QA-014 | design A14 | supporting | 清洁度 | grep/diff | 两轮 review grep（无 debug/TODO/PII） | 零命中 | pass |
| QA-015 | review focus 5 | supporting | 500 封套不泄内部文本 | integration | router_test.go:125 panic→500 internal 封套 | 统一封套 | pass |
| QA-016 | review focus 3 | supporting | 数千客户规模耗时 | manual | 未运行（见第 3 节） | — | residual |
| QA-017 | review focus 4 / REV-005 | supporting | q 含 %/_ 通配符 | manual | 未运行（已知匹配语义瑕疵，nit 在案） | — | residual |
| QA-018 | design CMD-004 | core-functional | 前端类型与构建 | build | `make build`（含 `npm run build`） | exit 0 | pass |

## 3. Command Results

- `make build` → exit 0：前端 vite build + webui-sync + `go build ./...` + server 二进制，全通过（CMD-004 含在内）
- `make lint` → exit 0：golangci-lint `0 issues` + oxlint 通过
- `cd backend && go test ./...` → exit 0：customer / httpapi / store 三包 ok（CMD-003；含 review-fix 新增的 panic 回滚、跨页一致性、page=0 三组测试）
- `make generate` 幂等验证 → 生成前后 `api.gen.go` / `schema.d.ts` md5 完全一致（ZERO-DRIFT）
- **`make check` 的 `generate-check` 子目标 → 红**（`git diff --exit-code` 比对工作区 vs HEAD）：生成物本身是本 feature 的未提交交付物，该子目标在 pre-commit 状态下必然失败，**归因为流程时序而非缺陷**。CMD-002 的语义（生成物与契约同步、零漂移）由幂等性验证等价证明；commit 后该目标将自然转绿，acceptance 可复跑确认
- 未运行：数千客户规模压测（QA-016）→ 需构造批量数据，成本高于当前收益；REV-001 修复后列表已走 DB 索引分页，规模风险已从「全量内存加载」降级为「深分页 OFFSET 成本」（review R6 在案），目标规模（单摄影师私域数百客户）下不阻塞
- 未运行：`q` 通配符手工验证（QA-017）→ 已知 nit（REV-005），行为确定（`%`/`_` 参与 LIKE 匹配），无需实测确认存在性；产品口径留给后续 feature

## 4. Scenario Results

- [x] QA-001~010、013、015、018：pass——证据为本轮实跑命令输出（第 3 节），integration 测试直接落在真实 PG 容器上，覆盖 A3-A10 全部 API 行为与错误路径
- [x] QA-011 浏览器建档：pass
  - Evidence: `customers-search-desktop.png`（建档后列表可搜到长昵称客户）、`customer-detail-desktop.png`（详情 2 个私域账号、聚合零值、referrer 无）、`browser-evidence.json`（`elapsedCreateMs:186`、`identityItemCount:2`、`searchFindsCreatedCustomer:true`、`formInputPreservedOnError:true`）
  - Notes: 截图取证于 review-fix 之前，但 review-fix 未触碰任何前端文件（仅 scope.go/customer.go/customers.go 后端），列表/详情行为语义由 round 2 整改后的 integration 测试重新证明，浏览器证据仍有效
- [x] QA-012 UI 状态：pass
  - Evidence: `customers-empty-desktop.png`（空态+计数 0）、`customer-new-error-desktop.png`（「介绍人必填」表单错误+输入保留）、`customer-detail-mobile-375.png`（375px 长昵称折行不溢出、底部 tab 导航正常）、`customers-unauth-mobile.png`（未认证回登录页）；`initialFocusId:displayName`、`focusAfterTab:channel`、`mobileNoGlobalOverflow:true`
  - Notes: 6 张截图本轮逐张目验，与 browser-evidence.json 自报字段互相印证
- [x] QA-013 范围守护：pass——PATCH/identities/notes/merge 全 404（integration + rangeGuardStatuses 双证据）；前端无对应 UI 入口（截图核对：详情页只有「返回列表」）

## 5. Findings

### failed

- none

### blocked

- none

### residual-risk

- QA-016 数千客户规模未压测：目标规模数百客户，DB 索引分页已落地，深分页 OFFSET 成本（review R6）留列表增长后评估；非核心路径
- QA-017 `q` 通配符 `%`/`_` 匹配语义（REV-005 nit 在案）：行为确定，产品口径未拍板，不影响核心搜索路径
- `generate-check` 子目标 pre-commit 必红（时序性），commit 后 acceptance 复跑确认转绿
- 浏览器截图取证早于 review-fix：前端零改动 + 后端行为有 round 2 测试重证，判定仍有效；acceptance 若要求可低成本重截
- review R2~R4 承接：Count/QueryPage 非同快照（可接受）、referrer 404 放大（order/merge 后回归）、design-review stale 状态债（owner 已口头豁免，acceptance 需记录）

## 6. Cleanliness

- Debug output: pass（两轮 grep 零命中）
- Temporary TODO/FIXME/XXX: pass
- Commented-out code: pass
- Unused imports / dead code: pass（golangci-lint + oxlint 0 issues）
- Out-of-scope files: pass（diff 全部归因本 feature；无订单/套系/档期/提醒实现）

## 7. Verdict

- Status: **passed**
- 全部功能性核心路径（A1-A14）有实际运行证据：API 行为走真实 PG 容器 integration 测试，浏览器路径有 6 张截图+自动化 checks 逐项核对，命令基线 build/lint/test/generate 幂等全绿
- Next: `cs-feat-accept`（提醒：① `generate-check` 在 commit 后复跑确认；② design-review stale 豁免需在 acceptance 记录；③ 提交需人工同意——attention.md 约定）
