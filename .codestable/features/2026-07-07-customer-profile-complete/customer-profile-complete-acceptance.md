---
doc_type: feature-acceptance
feature: 2026-07-07-customer-profile-complete
status: passed
accepted: 2026-07-08
round: 1
---

# customer-profile-complete · 客户档案补全验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-08
> 关联方案 doc：`.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `PATCH /api/v1/customers/{id}` 渐进字段与 null 清空：live API smoke `patch-null-channel 200`，断言 `real_name` / `birthday` 生效、`phone=null`、从 referral 改走后 `referrer_customer_id=null`。
- [x] `PATCH {channel: referral}` 缺 referrer：live API smoke `patch-referral-missing 400`。
- [x] `PATCH {channel: referral, referrer_customer_id: self}`：live API smoke `patch-self-referrer 400`。
- [x] `PATCH {status: archived}` / `{status: active}`：live API smoke `archive 200`、`restore 200`。
- [x] merged 客户 PATCH：live API smoke `merged-readonly 409`，错误语义由测试覆盖 `customer_merged`。
- [x] `DELETE /identities/{identity_id}`：live API smoke `delete-identity 204`；删最后一个身份 `delete-last-guard 409`。
- [x] `POST /notes` 与详情倒序：live API smoke `add-note 201`、`detail-note-order 200`，断言最新备注为首条。
- [x] `POST /merge`：live API smoke `merge 200`，随后 `source-detail 200` 断言 source `status=merged`、`merged_into_customer_id=target`、身份数为 0。

**名词层“现状 → 变化”逐项核对**：

- [x] `CustomerNote`：`backend/internal/platform/store/migrations/0003_customer_notes.up.sql` 新增表与 `(account_id, customer_id, created_at DESC, id DESC)` 索引；`backend/internal/customer/repository.go` 写入 / 查询 notes；`identity_note_test.go` 与 live smoke 覆盖。
- [x] `UpdateInput`：`backend/internal/customer/model.go` 使用 `nullable.Nullable[string]` 表达 `real_name` / `phone` / `birthday` 三态；`backend/internal/platform/httpapi/api.gen.go` 由 codegen 生成 nullable 类型；未手写重复 DTO。
- [x] `CreateInput` referrer active 校验：`TestCreateReferrerMustBeActive` 覆盖 archived / merged referrer → 404。
- [x] `MergeResult`：merge API 返回 target Customer；迁移计数未暴露到 API，仅测试断言迁移效果。
- [x] `Service` 接口扩展：`Update` / `AddIdentity` / `DeleteIdentity` / `AddNote` / `Merge` 均落在 customer service / repository，HTTP 层保持薄适配。
- [x] 文件结构拆分：`customer.go` 删除，`model.go` / `service.go` / `repository.go` 承载同包职责。
- [x] 前端客户档案组件：`frontend/src/components/customers/` 承载档案表单、身份、备注、merge 对话框、快捷备注。

**流程图核对**：

- [x] `D → PATCH → 校验 → AccountScope 更新`：`customers_profile.go` + `repository.Update` + `update_test.go` / `customers_profile_test.go` 覆盖。
- [x] `身份增删 → last_identity`：`DeleteIdentity` 同事务查数与删除；`TestDeleteIdentityAndLastGuard` / live smoke 覆盖。
- [x] `备注 ≤500`：`AddNote` 验证空 / 超长，详情倒序；`identity_note_test.go` 覆盖。
- [x] `merge → WithinTx 迁身份/备注 + referrer 重定向 + source merged`：`repository.Merge` 与 `merge_test.go` / `customers_profile_test.go` 覆盖。
- [x] `列表 status 筛选`：live API smoke `archived-hidden 200` / `archived-visible 200` 断言 active 缺省隐藏、archived 显式可查。

## 2. 行为与决策核对

对照方案第 1 节 + 第 2.2 节：

**需求摘要逐项验证**：

- [x] 渐进字段 PATCH：命令、HTTP 集成测试和 live smoke 均覆盖。
- [x] 建档后身份增删：API 测试与 live smoke 覆盖，末位守护返回 409。
- [x] 随手备注：详情页截图显示备注 tab 与保存入口；live smoke 验证备注创建与详情读取。
- [x] 归档与恢复：live smoke 验证归档隐藏、archived 查回、恢复。
- [x] 客户合并：HTTP / domain 测试和 live smoke 验证 source merged、身份迁移、source 0 身份。

**明确不做逐项核对**：

- [x] 不做 Order / Reminder merge 迁移与归档联动：`rg "orders|reminders" backend/internal/customer backend/internal/platform/httpapi/customers_profile.go` 无命中。
- [x] 不做重复身份自动去重 / 合并建议 / 相似客户提示：相关 grep 仅命中既有 reminder `dedup_key` schema 与分页测试的英文 `duplicated`，未命中客户域自动去重或 UI 建议入口。
- [x] 不做人脉链可视化与金额归因：相关 grep 无 feature 实现命中。
- [x] 不物理删除客户：router 仅有 `DELETE /customers/:id/identities/:identity_id`，无 `DELETE /customers/{id}`。
- [x] 不引入 UI 组件库：`frontend/package.json` dependencies 仅 `react` / `react-dom` / `react-router-dom`。

**关键决策落地**：

- [x] D1 转介绍指针语义：merge 测试覆盖 source 指针重定向与自指清空；target 可保留 referral+空 referrer 历史态。
- [x] D2 channel 与 referrer 联动：live smoke 覆盖缺 referrer、自指、从 referral 改走清空；测试覆盖非 active / 跨账号 404。
- [x] D3 末位身份守护：live smoke 与并发测试覆盖 `last_identity`。
- [x] D8 Create referrer active 收紧：`TestCreateReferrerMustBeActive` 覆盖。
- [x] D4 merged 只读、archived 可编辑：HTTP / domain 测试覆盖，live smoke 覆盖 merged 409 与 archived 恢复。
- [x] D5 merge 前置校验：`TestMergeErrorMatrix` 与 HTTP 测试覆盖 source/target 非 active、source==target、不存在/跨账号。
- [x] D6 契约 tag / 错误矩阵：OpenAPI 5 操作带 `customer-profile-complete` tag，409 描述与 nullable / status 子集已 codegen。
- [x] D7 前端组件落点：详情页只装配，新组件在 `components/customers/`。
- [x] D9 PATCH null 三态：Go 和生成类型均使用 `nullable.Nullable`。

**流程级约束核对**：

- [x] 错误语义：`envelope.go` 定义 `customer_merged` / `last_identity` / `merge_conflict`；HTTP 测试覆盖封套。
- [x] 事务与一致性：merge 使用 `WithinTx`；`QueryRowForUpdate` 串行化写路径；并发 / 回滚测试通过。
- [x] 幂等性：重复 merge 走 source 非 active → `merge_conflict`，测试覆盖。
- [x] 账号隔离：全部写路径经 `AccountScope`；跨账号 HTTP 测试覆盖全 404。
- [x] 可观测点：未发现 handle / 手机号明文日志；清洁度 grep 无调试输出。

**挂载点反向核对（可卸载性）**：

- [x] API 契约与 codegen：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`api.gen.go`、`schema.d.ts`。
- [x] 数据库 schema：`0003_customer_notes.up.sql` / `.down.sql`。
- [x] 受保护 API 路由：router 注册 5 条 profile 操作。
- [x] 前端 UI 注入点：`CustomerDetailPage.tsx`、`CustomersPage.tsx`、`components/customers/`、`index.css`。
- [x] 反向核查：本 feature 引用集中在上述挂载点、customer domain、HTTP adapter、AccountScope 扩展、测试与生成物中；范围外 `2026-07-08-identity-delete-button`、`2026-07-08-referrer-select` 与 `frontend/scripts/prototype-contract.test.mjs` 增量已作为提交拆分风险登记。
- [x] 拔除沙盘推演：移除 OpenAPI tag/include、0003 migration、router 5 条、customer 五操作与前端组件注入后，系统回到 customer-core 只读档案状态；`customer_notes` 表需随 down migration 一并移除。

## 3. 验收场景核对

对照方案第 3 节关键场景清单：

- [x] A1 `make check`：exit 0；build + lint + test + generate-check 全绿。
- [x] A2 `make generate && git diff --exit-code -- api.gen.go schema.d.ts`：exit 0，生成物零漂移。
- [x] A3 渐进字段 / null / birthday：HTTP 集成测试 + live smoke pass。
- [x] A4 channel 联动：HTTP 集成测试 + live smoke pass。
- [x] A4b Create referrer active 收紧：`TestCreateReferrerMustBeActive` pass。
- [x] A5 状态矩阵：HTTP 集成测试 + live smoke pass。
- [x] A6 归档列表：HTTP 集成测试 + live smoke pass。
- [x] A7/A8 身份增删：HTTP / domain / 并发测试 + live smoke pass。
- [x] A9 备注：HTTP / domain 测试 + live smoke pass。
- [x] A10/A11 merge：HTTP / domain / 回滚 / 并发测试 + live smoke pass。
- [x] A12 双账号：HTTP / domain 测试 pass。
- [x] A13 浏览器详情页：Playwright 截图通过，见 `evidence/accept-detail-desktop.png` 与 `evidence/accept-detail-mobile-375.png`。
- [x] A14 ≤2 步备注：QA 继承实现阶段演示，acceptance live API 与详情页截图复核备注入口可见；未重新做交互计步录屏。
- [x] A15 UI polish：375px 截图复核无明显破版；IME 真机输入未复跑，继承 review residual。
- [x] A16 范围守护 / 清洁度：grep 与 diff review pass。

**review 报告重点复核**：

- [x] Review 第 4 节 blocking 为 none；important REV-001/002/003 均 resolved。
- [x] Review 第 5 节 QA Focus 已由 QA 报告 QA-005~QA-019 与本轮 live smoke / screenshots 覆盖。
- [x] Review residual R-2（范围外 identity-delete-button）仍存在；本轮最终状态另有 `referrer-select` worktree override 与 prototype contract test 增量，提交时必须拆分；不阻断当前功能行为验收。

**QA 报告重点复核**：

- [x] 验证证据来源：`customer-profile-complete-qa.md` status=passed + 本轮 accept 复验。
- [x] QA Matrix 覆盖 design A1-A16 与 review QA focus。
- [x] feature 性质为 functional；核心 API 路径已有运行证据，本轮又补 live API smoke。
- [x] failed / blocked 项为 none。
- [x] residual-risk 已登记，未承载核心验收缺口；requirement governance 已由 owner 批准的 approval A 解除。
- [x] Evidence pack / DoD / Gate：本 feature 无独立 evidence pack / gate JSON；DoD 命令本轮复跑通过。

## 4. 术语一致性

对照方案第 0 节 + 第 2.1 节命名 grep 代码：

- 归档 / archive：OpenAPI status、service/repository、前端按钮与筛选一致。
- 合并 / merge：API `/merge`、service/repository、merge dialog、错误码 `merge_conflict` 一致。
- 转介绍 / referral：channel/referrer 命名一致；`referrer_customer_id` 沿用权威契约。
- 渐进字段：PATCH 字段名与 schema / codegen / frontend 表单一致。
- 末位身份守护：错误码 `last_identity`、service/repository、测试一致。
- 防冲突：禁用词“用户”在本 feature 代码 / 文案新增面未作为业务对象使用；“客户 / 账号 / 社交身份”沿用 CONTEXT.md。

## 5. 领域影响盘点（提示而非代写）

对照方案第 4 节 + 实际实现：

- [x] 候选 1（术语 / 流程约束）：“末位身份守护”是本次新增不变量，建议走 `cs-domain` 评估是否写入 `requirements/CONTEXT.md` 或 ADR。accept 阶段已登记，未代写。
- [x] 候选 2（结构选择 / 流程约束）：merge 迁移面“随域生长”（order/reminder 后续 feature 自觉补迁移用例）已写在 roadmap / design，建议后续 `cs-keep` 沉淀为流程经验；不需要在 acceptance 直接写 ADR。
- [x] 候选 3（流程约束）：AccountScope 写路径锁面与 `QueryRowForUpdate` 的使用约定已由 review residual R-3 暴露，建议 `cs-keep` 或后续 `cs-domain` 判断是否升级为长期约束；accept 不代写。

## 6. requirement delta / clarification 回写

状态：**passed**。

判据：design frontmatter `requirement: customer-profile`，本 feature 实现了该 req 的全部用户故事。owner 已批准 approval A，允许把 `.codestable/requirements/customer-profile.md` 从 `draft` 机械升级为 `current`，回填 `implemented_by`，并同步 `VISION.md`。

已执行回写：`.codestable/requirements/customer-profile.md`、`.codestable/requirements/VISION.md`。

## 7. roadmap 回写

状态：已执行。

- [x] design frontmatter 同时存在 `roadmap: photographer-private-crm` 与 `roadmap_item: customer-profile-complete`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 中对应条目已改为 `status: done`、`feature: 2026-07-07-customer-profile-complete`。
- [x] `photographer-private-crm-roadmap.md` 第 5 节子 feature 清单已同步为 `done`。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露必须补入 `.codestable/attention.md` 的稳定项目入口规则。

分流候选：

- `cs-keep` 候选：merge 迁移面“随域生长”的执行模式；AccountScope 写路径锁面约定；OpenAPI 错误矩阵逐端点核对经验。
- `cs-doc-tutorial` 候选：客户详情页新增编辑 / 归档 / merge / 备注操作，后续面向使用说明时应更新。
- `cs-doc-api` 候选：5 个 customer-profile API 操作已激活，若项目开始维护 API 参考，需要同步。

## 9. 遗留

- 后续优化点：`page_size` 契约无 maximum、服务端不设上限（customer-core 既有发现，建议后续 issue）。
- 已知限制：IME 组合态未做真机输入复核；acceptance 已通过静态修复 + build + 截图验证 UI 渲染。
- 已知限制：`requireActiveReferrer` active 校验仍为无锁 Exists，窄并发窗口属既有 / 后续增强候选，核心写路径本轮已加 FOR UPDATE。
- 提交注意：工作树含范围外 unit `2026-07-08-identity-delete-button`、`2026-07-08-referrer-select`、`frontend/scripts/prototype-contract.test.mjs` 增量，以及 `IdentitySection.tsx` / `index.css` 的相关样式风险，scoped commit 时必须拆分。
- 流程阻塞：none。

## 10. 最终审计

- 验证证据来源：`customer-profile-complete-qa.md` + accept-inline 复验。
- Evidence sources：无独立 evidence pack / gate JSON；DoD 命令、live API smoke、Playwright 截图、最终 diff 复核。
- Inline Verification Matrix：
  - CMD-001：`make check` → exit 0。
  - CMD-002：`make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` → exit 0。
  - CMD-003：`cd backend && go test ./...` → exit 0。
  - CMD-004：`cd frontend && npm run build` → exit 0。
  - LIVE-API：登录 token + local Postgres + backend `:18080`，覆盖 create/referrer、PATCH null/channel、notes、identity delete、last_identity、archive/list/restore、merge、merged readonly → `LIVE_API_SMOKE_OK`。
  - BROWSER：Playwright Chromium 截图详情页桌面 / 375px 移动 → `evidence/accept-detail-desktop.png`、`evidence/accept-detail-mobile-375.png`。
- 聚合命令：全部 exit 0；`make check` 含 build/lint/test/generate-check。
- 场景复核：re-verified 14 / trust-prior-verify 2（A14 计步演示、IME 真机输入）。
- 交付物复核：代码 / 配置 / schema / 路由 / 迁移 / 前端 UI / 测试均通过；requirement / roadmap 已回写。
- 完整工作区复核：`git status --short` 显示 feature 代码、feature spec、生成物、roadmap 既有改动，以及范围外 `identity-delete-button` / `referrer-select` / prototype contract test 增量；已纳入判断。
- diff 清洁度：无 debug / TODO / FIXME；无 UI 组件库；无客户物理删除；无 Order/Reminder 迁移实现。
- 知识沉淀出口：候选已分流到第 8 节；未写 attention。
- 结论：**passed**。功能实现、验收场景、requirement 回写与 roadmap 回写均完成；剩余小 UI unit 按独立 commit 拆分。
