---
doc_type: feature-acceptance
feature: 2026-07-07-customer-core
status: passed
accepted: 2026-07-07
round: 1
---

# customer-core 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-07
> 关联方案 doc：`.codestable/features/2026-07-07-customer-core/customer-core-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `POST /api/v1/customers`（`backend/internal/platform/httpapi/customers.go` + `backend/internal/customer/customer.go`）：`display_name/channel/identities[1..N]` 创建客户，返回 201 Customer；`channel=referral` 需要可见介绍人，缺失 400，不存在或跨账号 404。`TestCustomerAPIRoundtrip`、`TestCreateValidationRollback`、`TestReferralVisibility` 覆盖 2 条身份、回滚和 referral 四态。
- [x] `GET /api/v1/customers`：`q/channel/status/page/page_size` 映射到 `customer.ListFilter`；`q` 覆盖 `display_name/real_name/phone/social_identities.handle`；列表项含 `orders_count=0`、`last_shot_at=null`。`TestListFiltersAndPagination` 和 API roundtrip 已覆盖。
- [x] `GET /api/v1/customers/{id}`：返回 Customer + `identities[]` + `notes[]` 空数组 + `referrer?` + `stats{0,0,null}`。`TestCustomerAPIRoundtrip`、`TestReferralVisibility` 已覆盖。

**名词层“现状 → 变化”逐项核对**：
- [x] `Customer`：`customers` 迁移含 `account_id`、姓名类字段、渠道、referrer、status、merge 指针；创建从 `AccountScope` 写入服务端账号。
- [x] `SocialIdentity`：`social_identities` 迁移含 `platform/handle/remark`；创建时 1..N 条同事务写入。
- [x] `CustomerListItem`：OpenAPI/Go/TS 生成物含聚合字段；本轮恒 0/null。
- [x] `CustomerDetail`：OpenAPI/Go/TS 生成物含 identities、notes、referrer、stats。
- [x] `CustomerService / repository`：`backend/internal/customer` 提供 service/repository；该包未 import `gin`。
- [x] AccountScope 事务与受控读模型：`WithinTx`、`QueryPage`、`Count`、`Exists`、`InsertReturningID` 已落地并有回滚/双账号/标识符校验测试。
- [x] 前端客户路由状态：`/customers`、`/customers/new`、`/customers/:id` 已改接 API client，含 empty/loading/error/401/长文本证据。

**流程图核对**（第 2.2 节 mermaid 图）：
- [x] 登录后列表、新建、校验、referrer 查验、创建、详情、筛选、账号隔离搜索、详情读取均有代码落点：`router.go` 挂载三条受保护路由，`customers.go` 做 HTTP 适配，`customer.go` 做校验/事务/查询，前端三页完成导航。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 受保护 Web 内新建客户、列表筛选、详情读取完成；未认证路径 401 清 token 回登录。
- [x] 后端落客户与社交身份数据，`api/openapi.yaml` 与 roadmap §4 增量对齐；Go server interface 只新增 customer-core 三操作，TS schema 全量生成。
- [x] 浏览器证据覆盖“登录后建档（2 个身份）→ 列表搜到 → 详情看到”，`browser-evidence.json` 记录 `elapsedCreateMs:186`。

**明确不做逐项核对**：
- [x] `PATCH /customers/{id}`、建档后 identity 增删、notes、merge 仍 404；`rangeGuardStatuses` 与 API 测试覆盖。
- [x] 没有实现订单 / 套系 / 档期 / 提醒 / dashboard 后端行为；OpenAPI 只同步契约，后端 include-tags 只含 `auth` + `customer-core`。
- [x] 没有重复身份自动去重、merge 建议、聊天软件导入。
- [x] 没有渠道分析、线索池、客户画像。
- [x] `frontend/package.json` 未新增 UI 组件库。

**关键决策落地**：
- [x] D1 customer-core 最小闭环已在 roadmap item 回写为 `done`。
- [x] D2 referral 口径按创建时校验可见 referrer，关系经营留给后续。
- [x] D3 OpenAPI/codegen 切片已执行：全量契约同步，Go 只生成 customer-core 三操作。
- [x] D4 模块归属已执行：客户域在 `backend/internal/customer`，HTTP 只做适配。
- [x] D5 AccountScope 扩展已执行，未绕开账号隔离基座。
- [x] D6 聚合字段从第一天返回，值恒 0/null。
- [x] D7 至少 1 个社交身份为硬条件，校验失败不写半成品。

**流程级约束核对**：
- [x] 错误语义：400/401/404/500 ErrorEnvelope 路径覆盖；panic 500 统一封套由平台测试覆盖。
- [x] 事务一致性：客户与全部社交身份同事务；validation/tx error/panic 回滚测试通过。
- [x] 账号隔离：客户端不传 account_id；scope 从 auth AccountContext 派生；跨账号列表/详情/referrer 不可见。
- [x] 幂等性：POST 非幂等，未引入 idempotency key，未提前做重复身份去重。
- [x] 可观测点：未新增明文手机号/handle 错误日志；结构化启动日志沿用平台。

**挂载点反向核对（可卸载性）**：
- [x] API 契约与 codegen：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`api.gen.go`、`schema.d.ts`。
- [x] 数据库 schema：`backend/internal/platform/store/migrations/0002_customers.*.sql`。
- [x] 受保护 API 路由：`router.go` 挂载 POST/GET `/customers` 与 GET `/customers/:id`。
- [x] 后端域模块：`backend/internal/customer` 接入 Store / AccountScope。
- [x] 前端受保护路由：`App.tsx`、`api/client.ts`、`CustomersPage`、`CustomerNewPage`、`CustomerDetailPage`、`customerLabels.ts`。
- [x] 反向 grep：customer-core 新增引用均落在上述挂载点或其测试/证据内；未发现清单外运行时挂载。
- [x] 拔除沙盘推演：删除上述五类挂载点后将回到“登录后不能建档、不能查客户”的平台骨架；测试和证据文件为验证残留，不影响运行时卸载。

## 3. 验收场景核对

- [x] A1 `make check`：通过。说明：为避免未提交生成物导致 `generate-check` 把当前交付物误判为相对真实 index 的漂移，本轮使用临时 Git index 预载当前 `api.gen.go` / `schema.d.ts`，真实暂存区未改。
- [x] A2 `make generate` + 生成物零漂移：`make check` 内已运行；临时 index 下 `git diff --exit-code -- api.gen.go schema.d.ts` exit 0。
- [x] A3 创建成功：API / service 集成测试覆盖 2 条身份与服务端 account_id。
- [x] A4 校验失败与回滚：缺 display、空 identities、非法 platform、空 handle 均 400/ErrValidation 且无半成品。
- [x] A5 referral：可见 201，缺失 400，不存在/跨账号 404。
- [x] A6 非法分页：`page=-1/0`、`page_size=0` 返回 400 validation_failed。
- [x] A7 默认列表：只返回当前账号 active 客户，`created_at desc, id desc`，聚合零值存在。
- [x] A8 q/channel/分页 total：匹配 phone 与 identity.handle，channel/status/page 正确；同 created_at 跨页无重复丢行。
- [x] A9 详情 shape：identities、notes 空数组、referrer、stats 零值完整。
- [x] A10 双账号隔离：列表、详情、referrer 查验均按当前账号过滤。
- [x] A11 浏览器建档：QA 已逐张目验 6 张截图；review-fix 后未触碰前端，后端行为由测试重证，判定通过。
- [x] A12 UI 状态：空态、错误、401、长文本、375px、focus/keyboard 证据通过；本轮信任 QA 截图证据。
- [x] A13 范围守护：不做端点仍 404，无 UI 入口。
- [x] A14 清洁度：`git diff --check` exit 0；grep 未发现调试输出、TODO/FIXME、注释代码或真实凭证。

**review 报告重点复核**：
- [x] 第 4 节 blocking/important 均 resolved 或 none。
- [x] 第 5 节 QA focus 已由 QA 覆盖；规模压测与 `%/_` LIKE 语义作为非核心 residual。
- [x] 第 6 节 residual risk 已逐项承接到第 9 节。

**QA 报告重点复核**：
- [x] 验证证据来源：`.codestable/features/2026-07-07-customer-core/customer-core-qa.md`，frontmatter `status: passed`。
- [x] QA matrix 覆盖 design A1-A14 与 review QA focus。
- [x] 功能性核心路径均有运行证据；浏览器路径有截图和自动化字段证据。
- [x] failed / blocked 项为 none。
- [x] residual-risk 均非核心验收缺口。
- [x] 非 goal/gate 模式，无 evidence pack / DoD Results / Gate Results。

**design / roadmap review 状态债复核**：
- [x] `customer-core-design-review.md` 与 roadmap review 因 owner 将 `identity` 改为 `identities[1..N]` 标记 `stale`；`worktree-override.md` 记录 owner 已确认“允许 approved，然后直接在当前分支开发”。本轮实现、code review、QA、acceptance 均以当前 approved design 和 checklist 为准重新复验，行为契约无缺口。

## 4. 术语一致性

- 客户 / Customer：代码、OpenAPI、前端页面均使用 Customer / 客户；未用“用户”指代客户。
- 社交身份 / SocialIdentity：后端、OpenAPI、前端标签一致；枚举含 `wechat/qq/telegram/xiaohongshu/douyin/weibo/other`。
- 渠道 / Channel：只用于获客来源，前端文案为“来源渠道”；与平台标签分离。
- 账号 / Account：账号隔离由 `AccountScope` 与 `account_id` 维护；客户端不传 account_id。
- 防冲突 grep：未发现本 feature 新增“粉丝/好友/用户”作为客户同义词；测试中的手机号为假数据用例，不是真实 PII。

## 5. 领域影响盘点（提示而非代写）

- [x] 客户 / 社交身份 / 渠道 / 账号：`requirements/CONTEXT.md` 已有定义；本 feature 未新增领域术语，不需要 `cs-domain` 更新 CONTEXT。
- [x] `CustomerListItem` / `CustomerStats` 聚合字段：属于 API shape / 读模型字段，不是新的业务术语；不需要 `cs-domain`。后续 order-tracking 接真实聚合时若形成稳定口径，再评估是否写领域约束。
- [x] AccountScope 事务与读模型扩展：是 ADR-001 既有隔离原则的实现延展，并已命中 compound《AccountScope 隔离基座要 fail-loud》；不构成新 ADR。
- [x] OpenAPI tag 切片：是可复用工程模式，不是领域模型；建议退出后走 `cs-keep` 沉淀。
- [x] `GET /me` 白名单：仍是 roadmap 语义层候选，已在 roadmap §7 保留后续 `cs-roadmap update` 提示；不在 accept 里代写 ADR/CONTEXT。

## 6. requirement delta / clarification 回写

- `requirement: customer-profile` 当前仍为 `draft`。
- 判定：本 feature 交付的是 `customer-profile` 的最小闭环切片（30 秒建档、列表、详情），但尚未完成该 requirement 的完整边界（建档后身份增删、备注、merge、归档、渐进字段仍在 `customer-profile-complete`）。
- 动作：不在 accept 阶段自由改写 requirement；`customer-profile.md` 保持 draft，`implemented_by` 暂不机械更新为完成。roadmap 已明确 `customer-profile-complete` 落地后再评估 `draft -> current`。
- 结论：无需本轮 req delta 写入；没有长期 requirement 文档变更。

## 7. roadmap 回写

- [x] design frontmatter：`roadmap: photographer-private-crm`，`roadmap_item: customer-core`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`：`customer-core.status` 已由 `in-progress` 改为 `done`，`feature` 保持 `2026-07-07-customer-core`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`：第 5 节子 feature 清单已同步为 `状态：done`。
- [x] roadmap §7 stale 观察项已改为 OpenAPI 同步结果，并保留 `GET /me` 白名单后续处理提示。
- [x] YAML 校验：`python3 .codestable/tools/validate-yaml.py --file ...items.yaml` exit 0。

## 8. attention.md 候选盘点

- [x] 候选 1：`make check` 的 `generate-check` 使用 `git diff --exit-code` 比真实 index；在 feature 未提交且生成物未暂存时会出现流程时序性红灯。建议补入 attention 的“命令与脚本陷阱”：验收未提交生成物时用临时 Git index 或在提交后复跑确认。
- [x] 候选 2：前端首次启动前必须 `cd frontend && npm ci`，否则 `npm run dev` 会报 `sh: vite: command not found`。README 已更新，是否升入 attention 由用户决定。
- [x] 其他知识出口：OpenAPI tag 切片 + 未实现端点只同步契约不注册，建议走 `cs-keep`；README/开发说明已发生变更，可选走 `cs-docs-neat`。

## 9. 遗留

- 后续优化点：
  - review REV-004：`customerMessage` 可改成 `errors.As` 获取 `ValidationError.Message`，降低字符串耦合。
  - review REV-005：`q` 搜索未转义 `%/_`，目前无注入风险，但匹配语义后续可由产品拍板。
  - review REV-006：merge/archive 落地后需明确 referral 可选状态范围。
  - review REV-007/013：前端 `listCustomers` 参数类型和 `page` truthiness 可在后续小重构修。
  - review REV-008：probe 测试表复合 FK 可对齐业务表。
  - review REV-012：rollback 可用 `context.WithoutCancel` + 短超时加固。
  - review REV-011：`page_size` 上限可在后续契约更新中补。
- 已知限制：
  - design-review / roadmap-review 的 `stale` frontmatter 是历史 gate 状态债；本轮依据 owner override 与后续实现 review/QA/acceptance 关闭行为风险，若需要最新设计层独立审查，可另跑 `cs-feat-design-review` / `cs-roadmap-review`。
  - 未做数千客户规模压测；当前目标规模为单摄影师数百客户，DB 侧稳定分页已落地。
  - 浏览器截图取证早于后端 review-fix；因 review-fix 未触碰前端，且后端行为由强制测试重证，验收信任 prior evidence。
  - `customer-profile` requirement 仍为 draft，完整能力完成点在 `customer-profile-complete`。
  - `GET /me` 仍是 platform-skeleton 白名单债，后续走 `cs-roadmap update`。
- 实现阶段顺手发现：
  - OpenAPI 全量契约同步会产生跨域 TS schema diff；Go server include-tags 切片可以控制实际注册面。
  - `make check` 与未提交生成物的 index 语义需要被后续 agent 记住。

## 10. 最终审计

- 验证证据来源：`customer-core-qa.md` + 本轮 accept 复验。
- Evidence sources：非 goal/gate 模式，无 evidence pack / DoD / gate results；浏览器证据来自 `evidence/browser-evidence.json` 与 6 张截图。
- Inline Verification Matrix：不适用，已有 QA 报告 passed。
- 聚合命令：
  - `GIT_INDEX_FILE=<tmp> make check` -> exit 0（临时 index 仅预载当前生成物，真实暂存区未改）
  - `cd backend && go test -count=1 ./...` -> exit 0
  - `cd frontend && npm run build` -> exit 0
  - `git diff --check` -> exit 0
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` -> exit 0
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-07-07-customer-core/customer-core-checklist.yaml` -> exit 0
- 场景复核：re-verified 12 / trust-prior-verify 2（A11/A12 浏览器截图未重截，信任 QA 证据；trust-prior 比例 14.3%）。
- 交付物复核：代码、配置、schema、路由、文档、requirement、roadmap 均已复核；requirement 不改写，roadmap 已 done。
- 完整工作区复核：`git status` 包含本 feature 代码、spec、证据、roadmap、README/OpenAPI 相关改动；未发现无关污染源。
- diff 清洁度：通过；无调试输出、临时 TODO/FIXME、注释掉代码、真实凭证。测试假手机号为用例数据。
- 知识沉淀出口：attention 候选 2 条；`cs-keep` 候选 1 条；`cs-docs-neat` 可选。
- 结论：通过。原始契约满足，验证证据足够，承诺交付物落盘，roadmap 状态已回写；等待用户终审确认后关闭 feature 工作流。
