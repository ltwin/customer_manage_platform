---
doc_type: feature-qa
feature: 2026-07-07-customer-profile-complete
status: passed
tested: 2026-07-08
round: 1
---

# customer-profile-complete QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-design.md` (status=approved)
- Checklist: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-checklist.yaml` (9 steps 全 done)
- Review: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-review.md` (status=passed, round=2, reviewer=subagent+ocr)
- Evidence pack: none（实现证据在对话 + checklist steps 内）
- Gate results: none
- DoD results: CMD-001~004 全绿（本轮重跑确认）
- Diff basis: 本轮工作区与 review round 2 一致（staged=4 契约/生成物文件；unstaged=customer 域三文件+5测试+5前端组件+两页改造+迁移+scope.go+路由+httpapi）
- Baseline dirty files: `.codestable/features/2026-07-08-identity-delete-button/` 及 `IdentitySection.tsx` class + `index.css` 样式增量（范围外 unit，review R-2 已记录；提交时须独立拆分）
- Feature type: functional（改变 5 个 API 端点行为、前端可见 UI、持久化 customer_notes 表、状态机语义）
- Core evidence gate: A3-A12 须有实际运行证据；A13-A15 UI/browser 须有浏览器或等价端到端证据

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | DoD CMD-001 | core-functional | build+lint+test+generate-check 一键基线 | command | `make check` | exit 0 | **pass** |
| QA-002 | DoD CMD-002 | core-functional | 生成物零漂移（Go server/TS schema）| command+diff | `go tool oapi-codegen … && git diff --exit-code -- api.gen.go` | exit 0 (ZERO_DRIFT_OK) | **pass** |
| QA-003 | DoD CMD-003 | core-functional | 域/HTTP/事务集成测试 | integration | `go test -count=1 ./internal/customer/...` | all ok | **pass** |
| QA-004 | DoD CMD-004 | core-functional | 前端 TS 类型检查与构建 | typecheck+build | `npm run build` | exit 0 | **pass** |
| QA-005 | design A3/A4/A5 | core-functional | 渐进字段/null清空/channel联动/状态矩阵 HTTP 面 | integration | `go test -count=1 ./internal/platform/httpapi/...` TestCustomerProfileAPIUpdate | 400/200/409 各路径正确 | **pass** |
| QA-006 | design A6 | core-functional | 归档后缺省隐藏/显式查回 | integration | TestCustomerProfileAPIArchiveListFilter | total 计数正确 | **pass** |
| QA-007 | design A7/A8 | core-functional | 身份增删+末位守护 409 | integration | TestCustomerProfileAPIIdentitiesAndNotes + TestDeleteIdentityAndLastGuard | 204/404/409 | **pass** |
| QA-008 | design A9 | core-functional | 备注 201/400/倒序 | integration | TestCustomerProfileAPIIdentitiesAndNotes + TestAddNoteAndDetailOrdering | 201/400/倒序断言 | **pass** |
| QA-009 | design A10/A11 | core-functional | merge 迁移+错误矩阵+指针重定向 | integration | TestCustomerProfileAPIMerge + TestMergeMigratesEverything + TestMergeErrorMatrix | 200/409/404 | **pass** |
| QA-010 | design A12 | core-functional | 双账号跨账号全部写操作 404 | integration | TestCustomerProfileAPICrossAccountIsolation + TestUpdateCrossAccountIsolation | 全 404 无泄漏 | **pass** |
| QA-011 | design A4b | core-functional | 建档 referrer 收紧 active (D8) | integration | TestCreateReferrerMustBeActive | archived/merged → 404 | **pass** |
| QA-012 | review REV-001 | core-functional | merge×PATCH 并发 TOCTOU 守护 | integration | TestMergeConcurrentWriteGuard (8轮) | 无非法写入到 merged | **pass** |
| QA-013 | review R-2/D3 | core-functional | 并发删穿末位身份守护 | integration | TestDeleteIdentityConcurrentGuard | 恰好 1 次 last_identity | **pass** |
| QA-014 | design A10 | core-functional | merge 事务失败整体回滚 | integration | TestMergeRollbackOnMidTransactionFailure | source/target 均保持原状 | **pass** |
| QA-015 | design A13/A14 | core-functional | 浏览器：字段编辑/身份增删/归档/merge/≤2步备注 | browser | 实现阶段 checklist S7/S8/S9 浏览器演示 | 操作成功+即时刷新 | **pass**（见注1） |
| QA-016 | design A15 | supporting | UI 空态/加载/错误/375px/keyboard | browser | 实现阶段 S9 harden polish | 状态可见不破版 | **pass**（见注1） |
| QA-017 | design A16 | core-functional | 范围守护：无 Order/Reminder 迁移/无自动去重/无 UI 库/无 DELETE /customers | diff+grep | grep orders/reminders in customer/; grep DELETE /customers in router; grep UI libs in package.json | 全部无命中 | **pass** |
| QA-018 | review R-2 | supporting | 清洁度：无调试输出/TODO/FIXME/注释代码 | grep | grep TODO/FIXME/console.log in src/ | 无命中 | **pass** |
| QA-019 | DoD CMD-002 | core-functional | 前端 schema.d.ts 零漂移 | command+diff | `npm run generate && git diff --exit-code -- frontend/src/api/schema.d.ts` | exit 0（make check 内已覆盖）| **pass** |
| QA-020 | review residual R-3 | supporting | requireActiveReferrer 无锁 Exists TOCTOU | residual | 既有行为非本轮引入；功能路径已测；并发写路径有 FOR UPDATE 覆盖 | residual-risk（见第5节） | n/a |

---

> **注1（QA-015/016 浏览器证据）**：本次 QA 轮无法启动 live 服务器（compose postgres 凭证仅容器内可用，宿主无 psql；详见「未运行项」）。浏览器证据来源于 review 报告确认的 checklist S7/S8/S9 演示（round 2 review 已接受该证据）。build + Testcontainers 集成测试构成 API 行为闭环。

## 3. Command Results

- `make check` → exit 0：build + golangci-lint 0 issues + oxlint + go test all ok (cached) + generate-check 零漂移
- `go tool oapi-codegen … && git diff --exit-code -- api.gen.go` → exit 0 (ZERO_DRIFT_OK)
- `cd frontend && npm run build` → exit 0：vite 45 modules, 298 kB JS
- `go test -count=1 ./internal/customer/...` → exit 0：ok 24.830s（含所有新测试）
- `go test -count=1 ./internal/platform/httpapi/...` → exit 0：ok 10.588s（含5组 profile API 集成测试）
- `grep -rn "orders|reminders" backend/internal/customer/` → 无命中（exit 1）
- `grep -rn "DELETE /customers" router.go openapi.yaml` → 无命中（exit 1）
- `grep -rn "TODO|FIXME" frontend/src/ backend/internal/customer/ httpapi/` → 无命中（exit 1）
- `grep -rn "console.log|debug" frontend/src/` → 无命中（exit 1）
- package.json dependencies → react/react-dom/react-router-dom 三个，无 UI 组件库
- **未运行：live server smoke test** → 宿主无 psql 命令，compose postgres 凭证（POSTGRES_USER=crm / crm-dev-password）在宿主端认证失败（socket auth 无 crm role；TCP 连接同样拒绝）。API 行为已由 Testcontainers 集成测试完整覆盖，**不阻塞**

## 4. Scenario Results

- [x] QA-001 make check 基线：pass — exit 0，build/lint/test/generate 全绿
- [x] QA-002 生成物零漂移：pass — ZERO_DRIFT_OK
- [x] QA-003 域集成测试：pass — ok 24.830s，含 update/identity/note/merge/concurrent 全套
- [x] QA-004 前端构建：pass — tsc -b + vite exit 0
- [x] QA-005 渐进字段/联动/状态矩阵 HTTP：pass — TestCustomerProfileAPIUpdate 覆盖 A3/A4/A5 各状态码路径
- [x] QA-006 归档列表语义：pass — TestCustomerProfileAPIArchiveListFilter total 1/1/2
- [x] QA-007 身份增删末位守护：pass — 201/204/404/409 last_identity 全路径
- [x] QA-008 备注边界与倒序：pass — 201/400/倒序断言 (created_at DESC, id DESC)
- [x] QA-009 merge 迁移+错误矩阵：pass — 正常迁移/重放409/source merged+指针/target active 全路径
- [x] QA-010 双账号隔离：pass — 全 404 无任何跨账号写入，A 客户完好
- [x] QA-011 建档 referrer active (D8)：pass — archived/merged referrer → 404
- [x] QA-012 merge×PATCH 并发守护：pass — 8 轮无非法写入到 merged 客户
- [x] QA-013 并发删穿末位身份：pass — 恰好 1 次 last_identity，1 身份存活
- [x] QA-014 merge 事务回滚：pass — 注入 CHECK 约束后 source/target 全部保持原状
- [x] QA-015 浏览器档案编辑/merge/备注：pass（review round 2 确认，实现阶段演示）
- [x] QA-016 UI polish/375px/keyboard：pass（review round 2 确认）
- [x] QA-017 范围守护 grep：pass — 无 Order/Reminder/DELETE /customers/UI 库
- [x] QA-018 清洁度：pass — 无 TODO/FIXME/debug 输出/注释代码
- [x] QA-019 schema.d.ts 零漂移：pass（make check 内 generate-check 已覆盖）

## 5. Findings

### failed

- none

### blocked

- none

### residual-risk

- **R-1（继承 review）** `requireActiveReferrer` 校验介绍人 active 状态为无锁 `Exists`，与并发 merge/归档介绍人有窄 TOCTOU 窗口：既有行为，非本轮引入；核心写路径已有 FOR UPDATE 覆盖；acceptance 可评估是否纳入 compound
- **R-2（继承 review）** 浏览器真实用户路径（A13/A14/A15）依赖实现阶段演示证据，本轮无 live 服务器重跑；Testcontainers HTTP 集成测试提供 API 层闭环覆盖，UI 渲染路径靠 tsc + build 保证类型安全
- **R-3（继承 review）** IME 组合态行为（`isComposing`）依赖浏览器/输入法组合，review 静态确认后尚未真机复核
- **R-4（继承 review）** `page_size` 契约无 maximum，服务端不设上限（customer-core 既有代码），顺手发现，留后续 issue
- **R-5（提交约束）** 工作树含范围外 unit `2026-07-08-identity-delete-button`（spec 目录 + IdentitySection class 变更 + index.css 样式）——提交时**必须拆为独立 commit**，不可混入本 feature

## 6. Cleanliness

- Debug output: **pass** — 无 `fmt.Println` / `console.log`（grep 无命中）
- Temporary TODO/FIXME/XXX: **pass** — grep 无命中
- Commented-out code: **pass** — 代码注释均为说明性注释（契约/设计决策/WHY），无注释掉的代码块
- Unused imports / dead code from this feature: **pass** — golangci-lint 0 issues；tsc -b 0 errors
- Out-of-scope files: **pass（需手工动作）** — `.codestable/features/2026-07-08-identity-delete-button/` 及相关改动已在工作区，须提交时独立拆分（R-5）

## 7. Verdict

- Status: **passed**
- Next: 进入 `cs-feat-accept`；提交前按 R-5 把范围外 unit 拆为独立 commit；residual-risk R-1/R-3 acceptance 时评估是否纳入 compound
