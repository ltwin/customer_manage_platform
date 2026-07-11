---
doc_type: feature-acceptance
feature: 2026-07-09-schedule-calendar
status: passed
accepted: 2026-07-11
round: 1
---

# schedule-calendar · 档期管理验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-10；owner 终审：2026-07-11
> 关联方案 doc：`.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-design.md`
> 当前状态：技术契约与 A1-A26 均通过；owner 已于 2026-07-11 选择 Option A，requirement、VISION 与 roadmap 的限定机械回写已完成。

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `GET /schedule/slots?from=&to=`：按半开区间返回 `ScheduleSlotListItem`；shoot 分支必返订单/客户/状态摘要，hold/busy 不携带订单引用摘要。证据：OpenAPI、Go/TS 生成物、`schedule_test.go`、`schedule_test.ts` 与浏览器月历摘要。
- [x] `POST /schedule/slots`：支持 shoot/hold/busy，返回 201 `{slot,overlaps}`；可选 `Idempotency-Key` 走 `schedule-slot.create.v1` 协议。证据：`schedule.go`、`idempotency` 协议测试、HTTP/集成测试与 QA 真实 API 重放。
- [x] `PATCH /schedule/slots/{id}`：`order_id`/`note` 实现省略、不改；null、清空；值、替换三态，最终实体重验矩阵和一订单一 shoot。证据：OpenAPI nullable shape、HTTP/服务集成测试。
- [x] `DELETE /schedule/slots/{id}`：只删除档期、返回 204，不联动订单；前端明确说明订单与状态保留。证据：HTTP 测试与 `CalendarPage.confirmDelete`。
- [x] `POST /orders` / `GET /orders` / `DELETE /orders/{id}` 增量：`creation_mode`、可选 Idempotency-Key、`schedulable_at` 和 typed `order_in_use` 均落地，既有无 header/无筛选调用保持兼容。证据：OpenAPI、order/httpapi tests、API client tests。
- [x] `GET /me.timezone` 与 `ApiError.details`：后端 provider 可注入、默认 Asia/Shanghai；前端全局 fail-closed 并可显式重试；typed details 由生成契约透传。证据：auth tests、AppShell、schema/client tests 与 QA 故障注入。

**名词层“现状 → 变化”逐项核对**：

- [x] `ScheduleSlot` / `CreateInput` / `UpdateInput` / `ListFilter` / `CreateResult`：domain、repository、service、OpenAPI 和生成类型字段一致。
- [x] `ScheduleSlotListItem`：使用专用 base + `type` discriminator union；Go/TS 可按 shoot/non-shoot 收窄，repository 批量组装摘要，无 N+1。
- [x] `Operation` / `IdempotencyRecord`：固定 `order.create.v1`、`schedule-slot.create.v1` typed 常量；`0006_idempotency_records` 与 `platform/idempotency.ExecuteCreate` 同口径。
- [x] `schedule_slots`：`0007` migration 包含时间、type/order、note、复合外键、范围索引和一订单一 shoot 部分唯一索引；up/down 均落盘。
- [x] `Account.timezone` 与 `ApiError.details`：OpenAPI、Go、TS、后端 provider、前端 shell context 同源。

**流程图核对**：

- [x] “日历/客户档案入口 → 静态校验与冲突预览 → journal → order/slot 分步创建 → 刷新 slot 摘要 → consulting 状态同步 → unknown/明确失败恢复”均有实际落点：`ScheduleSlotDialog`、`ShootOrderFlow`、`journal.ts`、`flow.ts`、`OrderWorkspace`、API client 与后端 idempotency/order/schedule 服务。
- [x] 后端“PrepareCreate → ExecuteCreate 唯一事务 → TxAccountScope callback → customer→order 锁序 → 资源+成功响应同事务”由当前源码、迁移与 fresh 协议/集成测试共同证明。

结论：第 2.1 节接口、数据库约束、生成契约与实际实现一致，未发现需要修代码或回填 design 的偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 桌面端月历可在一个界面查拍摄、预留、个人占用、订单摘要与重叠；QA 密集日为 682ms，最终抽样重新读到 42 格和两条真实 shoot 摘要。
- [x] 日历与客户档案复用同一 `ScheduleSlotDialog`；客户档案只传固定 shoot/customer，不复制第二套表单。
- [x] 可靠性闭环落地：order、slot、backfill_order 分 phase journal 与 attempt key；unknown、idempotency binding conflict、customer_changed、result-known local closeout 和确定性失败分流。
- [x] 移动首版只交付查阅：375×812 最终抽样为 42 个日期按钮、完整当天摘要、无横向溢出，创建/编辑/删除按钮均不可见。

**明确不做逐项核对**：

- [x] 无 `/schedule/book` 组合端点；仍由前端显式编排 order 与 slot 独立端点。
- [x] 无周视图、拖拽、周期重复、跨月批量操作或第三方日历库。
- [x] schedule 域不直接写 orders；订单状态同步由前端独立调用 order API。
- [x] 移动端没有完整创建、编辑、删除工作台。

**关键决策落地**：

- [x] D1-D3：requirement/roadmap/OpenAPI 同源；schedule、order、platform/idempotency 与 httpapi 边界清楚；列表摘要提供可行动订单落点。
- [x] D4-D7：UTC 半开区间、重叠只提示、未来/历史矩阵、customer→order 锁序和一订单一 shoot 均由纯函数/集成/并发测试覆盖。
- [x] D8-D17：新建订单、已有订单、backfill handoff、结果未知恢复、补偿、order_in_use、删除语义、PATCH 三态、跨日/全天、月历状态和深链均有代码与 QA 场景落点。
- [x] D18：幂等记录 claim/wait/replay/conflict/expiry、同事务成功响应和 commit unknown seam 已实现；handler 不拼 operation 字符串。

**编排层“现状 → 变化”逐项核对**：

- [x] 创建线：HTTP bind/prepare → typed idempotency transaction → schedule/order repository prepared write。
- [x] 查询线：账号时区算 42 日边界 → UTC range 查询 → batch 摘要 → 本地自然日投影与稳定排序。
- [x] 删除线：order 删除先终态门与 order lock，再反查 shoot；slot 删除只删 slot。
- [x] 恢复线：known ids、locked body/key、24 小时边界、人工 abandon 与 status sync fallback 均有实现及测试。

**流程级约束核对**：

- [x] 错误语义：400/401/404/409 typed code/details 与 design 一致；`idempotency_conflict` 不被错误分类为可换 key 的普通 409。
- [x] 幂等与事务：资源和成功 response record 同事务；确定失败回滚；同 hash replay、异 hash conflict；过期单 owner。
- [x] 并发：customer→order 单向锁序与 Delete 的 order-only 门避免反向环；连续 merge 可收敛到 `customer_changed`。
- [x] 账号隔离：slot、order、idempotency 记录都经 AccountScope/TxAccountScope；跨账号按 404/不可见处理。
- [x] 可观测与安全：沿用平台请求日志，无新增敏感日志或 debug 输出；凭证边界未变化。

**挂载点反向核对（可卸载性）**：

- [x] M1 API/生成物：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`api.gen.go`、`schema.d.ts`、API client。
- [x] M2 幂等平台：`0006_idempotency_records`、`platform/idempotency`、store 受控 transaction/claim 原语。
- [x] M3 schedule 域：`0007_schedule_slots`、`backend/internal/schedule`、`httpapi/schedule.go`、router 四条保护路由、server dependency wiring。
- [x] M4 order/platform 增量：creation mode、schedulable filter、tx-aware create、order_in_use、timezone provider 与 typed details。
- [x] M5 前端月历与共享流程：`CalendarPage`、`components/schedule/**`、`useFocusTrap`、样式与 schedule tests。
- [x] M6 双向入口/深链：`AppShell` timezone context、`CustomerDetailPage` 固定客户入口、`OrderWorkspace` backfill/handoff 与 date+slot link。
- [x] 反向核查：先用 CodeGraph 读取当前 on-disk 月历调用面，再以 `rg` 扫描 `ScheduleSlotDialog|ShootOrderFlow|OperationCreate*|schedule:create:pending|AccountTimezoneProvider|schedule routes`；runtime 引用均归入上述六类挂载点，未发现设计清单外的独立运行时入口。
- [x] 拔除沙盘推演：逆向移除 M6→M1 后，运行时回到“无 schedule 路由/表/幂等组合恢复、Calendar 原型不接真实档期”的旧能力；残留只会是 feature/QA/roadmap/requirement 历史记录和截图，不会留下可调用的半套 schedule runtime。共享 store/order 增量须按 M2/M4 成组回退，不能只删 schedule 表。

结论：行为、决策、流程约束和挂载点与 design 一致；无未处理实现偏差。

## 3. 验收场景核对

验证证据来源：`.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-qa.md`（`status: passed`）+ 2026-07-10 acceptance fresh commands/browser sampling。

- [x] **A1 CRUD 基线**：shoot/hold/busy 成功与非法时间、order/type、note 错误均由 schedule service/HTTP fresh tests 覆盖。
- [x] **A2 最终实体矩阵与 customer_changed**：fixed clock、历史/未来/换 order/type、归档/merge 并发由 fresh Go tests覆盖；完全不相交拟修改区间的浏览器恢复证据沿用 QA-002，表单保留且旧候选/冲突清除后重拉。
- [x] **A3 一订单一 shoot**：部分唯一索引、POST/PATCH typed 409、自身排除和并发 1 成功+1 conflict 均由 fresh schedule tests 覆盖。
- [x] **A4 404 与账号隔离**：service/HTTP 双账号 tests fresh 通过。
- [x] **A5 PATCH 三态**：OpenAPI nullable shape 与 HTTP/集成 tests fresh 通过；DELETE 不改订单。
- [x] **A6 order_in_use 与既有 slot 异常状态**：order/httpapi tests fresh 通过；typed details、note-only PATCH 与删 slot 后删订单均有证据。
- [x] **A7 半开区间查询**：domain/service/HTTP tests fresh 通过。
- [x] **A8 discriminator union/摘要/深链**：OpenAPI→Go/TS 重新生成逐字节一致；fresh tests 与浏览器摘要证明 shoot 必填、non-shoot 不带引用、查看订单可行动。
- [x] **A9 严格重叠**：端点相接、包含、部分相交、跨类型与自身排除由 fresh Go/TS tests 覆盖。
- [x] **A10 42 格/跨日/密集日**：fresh `test:schedule` 通过；acceptance 浏览器重新读到周一首列固定 42 格，QA 密集日 5 条/4 冲突/前三条+2/抽屉 5 条证据有效。
- [x] **A11 账号时区与 DST**：fresh tests 覆盖 23/25 小时、不存在/重复时间和浏览器时区隔离；acceptance 浏览器重新显示 `Asia/Shanghai`，QA fail-closed/retry 证据有效。
- [x] **A12 写前冲突与重拉**：fresh schedule tests 覆盖重叠计算/事务快照，多写 settle/聚焦 revalidate 的真实 UI 证据来自 passed QA，无失败项。
- [x] **A13 幂等 typed operation**：fresh `idempotency` 协议测试覆盖同 hash 串并发、异 hash、明确失败清 claim、expiry 单 owner。
- [x] **A14 资源与成功记录同事务**：fresh idempotency/order/schedule tests 覆盖 committed-but-error 与 rolled-back-error，最终 0/1 份。
- [x] **A15 pending journal/attempt key/2h-24h**：fresh `test:schedule` 32/32 覆盖 storage failure、tab 唯一、原 body/key、过期禁 replay、backfill draft/pending 边界；QA 真实 API 三 phase 证据补强。
- [x] **A16 明确失败、补偿与 status_sync**：fresh helper tests 覆盖 action 分流、known slot/customer 分页 fallback 与后续状态满足；完整恢复 UI/补偿证据沿用 passed QA/review。
- [x] **A17 日历新建订单主路径**：fresh build/lint/tests 通过；passed QA 浏览器从新建咨询订单到 shoot→refresh→scheduled 为 21.493s，低于 30 秒，临时资源已清理。
- [x] **A18 客户档案复用入口**：源码/CodeGraph 与 fresh build 证明共用同一 dialog；客户页实操证据沿用 QA/实现期 UAT。
- [x] **A19 候选/backfill handoff**：fresh order tests 与 32 个 schedule tests覆盖矩阵、分页、六态、时间戳裁剪、pending→draft→clear 与零重复网络；完整 handoff UI/真实 API 证据沿用 QA。
- [x] **A20 删除/typed details/深链**：fresh helper tests 通过；acceptance 浏览器重新读到两条“查看订单”可行动链接，QA 非法/失效/跨月深链证据有效。
- [x] **A21 10 秒/30 秒成功标准与加载错误**：passed QA 记录密集日 682ms、完整排期 21.493s、确认到可见 2.456s；acceptance 未修改实现并重新验证正常加载。
- [x] **A22 375px 轻路径**：acceptance fresh viewport 375×812，`clientWidth=scrollWidth=375`、42 格、当天两条完整摘要、可见工作台按钮 `[]`。
- [x] **A23 可访问性**：acceptance fresh Escape 关闭抽屉后 `document.activeElement` 返回 `2026-07-11` 日期按钮；日期 aria-label 可读档期/冲突数，移动无动作入口。
- [x] **A24 范围守护**：scoped `rg` 未发现组合端点、周视图、拖拽、周期重复或第三方日历依赖。
- [x] **A25 域边界/未注册路由**：schedule production repository/service 无 orders 写；router tests 保持 reminder/dashboard/settings/export 404。
- [x] **A26 前端规范**：CalendarPage/schedule production 路径无 prototype store/data import；API DTO 均引用生成 `schema.d.ts`。

**review 报告重点复核**：

- [x] Test And QA Focus 中 `customer_changed` 原事实区间恢复、三 phase `idempotency_conflict`、`/me` fail-closed retry、2h-24h backfill、known-slot、本地收口、跨月深链和 malformed details 均由 QA matrix 覆盖。
- [x] `REV-001`～`REV-011`、`REV-013` 全部 resolved；blocking/important 为 none。
- [x] `REV-012` 仅是小型 classifier 提取 suggestion，不承载行为缺口，不在 acceptance 扩成状态机重构。

**QA 报告重点复核**：

- [x] QA frontmatter 为 `feature-qa / passed / round 1`；Feature type 为 functional，core evidence gate 合理。
- [x] QA-001～QA-012 覆盖 A1-A26、review focus、命令、真实 API、浏览器、375px 与清洁度；failed/blocked 为 none。
- [x] residual risk 仅含可选 classifier、OCR scope 歧义和 Vite chunk warning，没有隐藏核心验收缺口。
- [x] 标准 feature 流程未启用 evidence-pack / DoD Results / Gate Results，报告已明确 none，不构成缺失。

## 4. 术语一致性

- 档期 / `ScheduleSlot`：CONTEXT、design、OpenAPI、后端 package、前端页面与 UI 均使用“档期”，未引入“日程”作为产品主术语。
- shoot / hold / busy：OpenAPI、数据库 CHECK、domain enum、前端分段与样式一致；shoot 必挂 order，hold/busy 禁 order。
- 账号本地自然日 / timezone：统一来自 `/me.timezone`，没有浏览器时区或硬编码 `+08:00` 的生产回退。
- 结果未知 / pending journal / attempt key：journal 与 UI 恢复语义一致；`idempotency_conflict` 被识别为 key 已绑定而非可换 key 失败。
- `customer_changed` / `order_already_scheduled` / `order_in_use`：错误码、typed details、前端行动落点与测试命名一致。
- 防冲突 grep：production scope 未发现 `/schedule/book`、第三方日历、prototype store/data 或 schedule 域 orders 写。

结论：术语与命名一致，无需在验收中修代码。

## 5. 领域影响盘点（提示而非代写）

- [x] 候选：档期 / Schedule Slot（新术语）。结论：`.codestable/requirements/CONTEXT.md` 已定义半开区间、三种类型及订单关联语义，不需要重复写入。
- [x] 候选：跨独立端点组合流程的创建幂等与结果未知恢复（结构性选择 + 流程级约束）。结论：typed operation、唯一事务 owner、资源/响应同事务、原 key/body 重放和 24 小时人工核对属于难回退、非显然、有真实权衡的长期约束；建议验收退出后由用户决定是否走 `cs-domain` 写 ADR，并可用 `cs-keep` 沉淀实现/调试处方。accept 阶段不代写。
- [x] 候选：customer→order 单向锁序（跨域并发约束）。结论：该顺序来自现有 merge 事实并由 barrier tests 固定；建议与上述 ADR 一并评估，或单独走 `cs-domain` 记录锁序约束。accept 阶段不代写。
- [x] 候选：新增 schedule bounded module。结论：roadmap 已定义模块边界，ADR-003 已覆盖薄 handler/单体组织；仅“新增目录”不足以另写 ADR。

## 6. requirement delta / clarification 回写

- [x] 方案 frontmatter 指向 `requirement: schedule-calendar`。
- [x] `.codestable/requirements/schedule-calendar.md` 已由 `status: draft` 机械升级为 `current`；本 feature 是首次实现，原 pitch、用户故事、解决方案与边界正文均保留，并追加 2026-07-10 变更日志。
- [x] `.codestable/requirements/order-tracking.md` 保持 current；“schedule-calendar 增量待验收”的临时注记及将来时变更日志已机械更新为已落地事实。
- [x] feature 目录中没有单独的 `*-req-delta.md` 或 clarification；`approval-report.md` 作为 owner-approved 等价 delta。
- [x] owner 于 2026-07-11 回复 `A`；`approval-report.md` 已更新为 `approved` 并记录限定授权历史。

结论：**passed**。依据 Global Route Governance，owner 已明确批准 Option A；requirement 与 VISION 已在限定范围内完成机械回写，没有扩写新的能力边界。

## 7. roadmap 回写

- [x] 方案 frontmatter 同时包含 `roadmap: photographer-private-crm`、`roadmap_item: schedule-calendar`。
- [x] 回写前 items.yaml 条目为 `slug: schedule-calendar`、`status: in-progress`、`feature: 2026-07-09-schedule-calendar`，满足机械回写前置。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 已将 `schedule-calendar` 更新为 `done`。
- [x] roadmap 主文档条目 7 已同步为 `done`，两份一致。

结论：roadmap 完成状态已机械回写；items.yaml 将在最终状态审计中重新校验。

## 8. attention.md 候选盘点

- [x] 候选 1：dirty/未提交 feature 中，`make check` 尾部 `generate-check` 会把工作树生成物与 Git index 比较，可能在重新生成字节一致时仍 exit 2。建议在 `.codestable/attention.md`“命令与脚本陷阱”记录：先用 `make generate` + 两份生成物逐字节比较确认真实 drift；需要完整门禁时用隔离临时 Git index 预载当前生成物，且不得改真实 staged 状态。
- [x] 该候选是本仓库后续 OpenAPI feature 容易重复踩的工作流事实，适合 `cs-note`；本 acceptance 不直接写 attention。
- [x] 其他知识出口：幂等/结果未知与锁序候选已分流到第 5 节 `cs-domain` / `cs-keep`；用户可见月历、恢复与移动边界可按需走 `cs-doc-tutorial`；公开 REST API 可按需走 `cs-doc-api`。

## 9. 遗留

- 后续优化点：`REV-012` 可提取小型 create-failure classifier；仅做维护性收口，不扩大为状态机重构。
- 已知限制：sessionStorage 覆盖同 tab 硬刷新，不覆盖关闭 tab；unknown 满 24 小时需要人工核对；跨 tab 重叠不实时推送，依赖写后/聚焦重拉；`idempotency_records` 首版没有物理清理任务。
- 性能观察：Vite 单 chunk 约 531kB，仍有 `>500 kB` warning；当前 10 秒/30 秒成功标准与 build 均通过，后续包体优化另起 refactor/feature。
- 审查流程风险：OCR CLI 因工作树混有独立 avatar issue 且共享 tracked 文件而跳过；独立 Task agent review 与 fresh QA/acceptance 证据已覆盖，不构成功能阻塞。
- 提交范围：工作树同时包含独立 avatar issue；后续 scoped commit 必须对 `Makefile`、`frontend/package.json`、`frontend/src/index.css` 逐 hunk 归因，不得把 avatar issue 混入 schedule-calendar。

## 10. 最终审计

- 验证证据来源：`.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-qa.md` + acceptance fresh command/browser verification。
- Evidence sources：标准 feature 流程没有 `{slug}-evidence-pack.md` / gate-results / dod-results；使用 design/checklist/review/QA、实现期 fix notes、`evidence/*.png` 和当前运行输出。
- 聚合命令：
  - `make generate` + 生成前后 `cmp` 两份产物 → exit 0，Go/TS 生成物逐字节一致。
  - 隔离临时 Git index 预载当前生成物后 `make check` → exit 0；Go build、golangci-lint、oxlint、全量 Go tests、前端 4+3+32+1 tests 与 generate-check 全绿；真实 `.git/index` 未改变。
  - `cd backend && go test -count=1 ./internal/platform/idempotency/... ./internal/schedule/... ./internal/order/... ./internal/platform/httpapi/...` → exit 0，四包 fresh 通过。
  - `cd frontend && npm run test:schedule && npm run build && npm run lint` → exit 0，schedule 32/32、TS/Vite build、oxlint 通过；仅保留 Vite chunk warning。
  - 两份 YAML `validate-yaml.py --yaml-only`、`git diff --check`、`git diff --cached --check` → exit 0；真实 staged stat 为空。
- 浏览器复核：
  - Desktop：`/calendar?date=2026-07-11` 加载后显示 `时间按 Asia/Shanghai`、周一首列固定 42 格、7 月 11 日两条 shoot 摘要和可行动订单链接；console warning/error 为空。
  - Mobile：375×812，`innerWidth=clientWidth=scrollWidth=375`、42 个日期按钮、抽屉完整两条摘要、可见创建/编辑/删除按钮 `[]`；Escape 关闭后焦点返回原日期按钮。
- 场景复核：re-verified 20 / trust-prior-verify 6。trust-prior 项为 A2 完全不相交区间 `customer_changed` UI、A16 补偿/status-sync 恢复 UI、A17 30 秒完整创建、A18 客户档案入口实操、A19 backfill 完整 handoff、A21 10 秒/30 秒计时；均来自同日 passed QA，且 QA 后没有 feature 源码变化。比例 23.1%，没有超过 30%。
- 交付物复核：代码、迁移、配置、OpenAPI、Go/TS schema、路由、前端共享组件、测试、截图、design/review/QA/checklist 均真实落盘；2026-07-11 owner approval 后，requirement current 与 roadmap done 已完成限定机械回写。
- 完整工作区复核：`develop`；tracked/unstaged、untracked、staged 全部纳入判断；staged 为空。schedule-calendar 与独立 avatar issue 共存，后者按 review/QA baseline 排除并已明确 scoped-commit 风险。
- diff 清洁度：scoped debug/TODO/FIXME/XXX/prototype/范围外功能查询 0 命中；Go/TS lint 通过；无注释掉临时代码、无无用 import、无手写重复 API DTO、无新增凭证。
- 知识沉淀出口：attention 候选 1 条；ADR/compound 候选 2 类；用户指南/API 参考按需；均已分流且未越权写入。
- 2026-07-11 状态收口：owner 已选择 Option A；approval report、requirement、VISION、roadmap items 与主文档均已按限定范围机械回写，并进入 YAML、状态一致性、diff 与聚合门禁复核。
- 2026-07-11 聚合门禁复验：隔离临时 Git index 预载当前 Go/TS 生成物后执行 `make check` → exit 0；前后端 build、golangci-lint、oxlint、前端 package-price 4/4、api-client 3/3、schedule 32/32、avatar-layout 1/1 与 generate-check 均通过，真实 Git index 保持为空。Go 聚合输出命中缓存，因此另做下述 fresh 数据库包复验。
- 2026-07-11 fresh Go 复验：启动 Docker Desktop 后，`go test -p 1 -count=1` 中 `internal/platform/idempotency`、`internal/schedule`、`internal/order` 依次通过；`internal/platform/httpapi` 首轮在 Testcontainers 连续创建 PostgreSQL 时出现三次基础设施错误 `port "5432/tcp" not found`，均发生在取得连接串之前、未进入业务断言。单跑 `TestCustomerProfileAPIUpdate` 通过，随后 `go test -count=1 ./internal/platform/httpapi/...` 整包通过（14.581s），未改业务代码或测试代码。
- 2026-07-11 状态与格式复核：项目 `validate-yaml.py` 使用 bundled Python fallback parser 校验 items.yaml、approval、acceptance、schedule requirement 与 order requirement，5/5 通过；`git diff --check` 通过；schedule requirement=`current`、VISION 位于 current、items.yaml 与 roadmap 条目 7=`done`、approval=`approved`、acceptance=`passed`，状态一致。
- 结论：**passed**。技术审计通过，owner approval gate 已解除，长期能力与 roadmap 状态已和最终工作区事实对齐。
