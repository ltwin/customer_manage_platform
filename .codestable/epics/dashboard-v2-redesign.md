---
status: active
created: 2026-08-22
work: ../work/epic-dashboard-v2-redesign.md
---
# Dashboard v2 经营台重构

## 起点

旧版 Dashboard 五块（`GET /dashboard`）已在生产稳定运行，口径单点在服务端，是本次重构的基座而非重写对象。设计原型 `frontend/proto-design/v2/dashboard.html` 展示的经营台愿景（今日驾驶舱左列、经营脉搏、客户资产三层次）超过旧五块能力。审计 `.codestable/audits/dashboard-v2-backend-capability.md`（2026-08-22）逐模块核对后结论：旧五块可直接复用，但收入瀑布的真实尾款/现金、交付 SLA、渠道×类型归因快照依赖当前**未记录的数据事实**，必须先行补模型；咨询转化与健康度分层是更高成本、更低价值且与既有术语/提醒口径冲突的模块，下放二期。

## 目标

把经营台从"旧五块"升级为 v2 原型的分层仪表盘：**今日驾驶舱左列（下一场/时间轴/待办/交付队列）+ 未来空档 + 收入构成瀑布 + 渠道×类型收入 + 档期利用率统一**。所有数字由服务端聚合、口径单点，原型里的写死常量（09:00–21:00、90 分钟、+14 天、TODAY-30）一律替换为 settings 或服务端事实。

## 范围

- 今日档期 / 时间轴 / 下一场（复用 `today_slots`；`next_shoot` 为今日及之后最早的 shoot 投影，见共享语言，今日无档期时跨到未来日）
- 今日待办（复用 `due_reminders`，补客户摘要投影以还原 v2 行样式）
- 后期交付队列（新增 `delivery_due_at` = `shot_at + 账号级 SLA`，订单级可覆盖）
- 未来空档（复用 Calendar 的 openings 模型 + settings availability）
- 收入构成瀑布（已确认 / 待收 / 在途三段 + 已收现金 KPI + 环比 + 客单价 + 90 天复购占比 + 待收账龄，基于支付字段）
- 渠道 × 类型收入矩阵（订单归因快照）
- 档期利用率（与 Calendar v2 同口径，只做展示派生）
- 触达文案复制弹层（纯前端，复用 Calendar openings 文案）
- 金额与 SLA 写侧录入（订单表单/详情录入已收金额与应交日覆盖；settings 编辑交付 SLA 默认值）——瀑布与交付队列数据持续进入的前提
- 服务端聚合端点 `GET /dashboard/v2`（一次返回上述读模型）

## 非目标

- 咨询转化（`consulted_at` / 转化漏斗 / 跟进线索）——二期，随 roadmap §7 观察项中的「渠道转化分析」二期候选同批规划
- 客户健康度分层（个人节奏 ratio / tier / LTV 读模型）——二期，需先解决与 reminder churn 口径关系
- 独立 Lead/Inquiry 实体——CONTEXT.md 已把「线索」定义为客户状态视图，不新建实体
- 独立支付流水表、退款、部分退款——本版只用订单最小金额字段
- 触达发送闭环（消息模板 / 已触达已回复统计 / 真实发送）——reminder `done` 不代表已发送
- 看板自定义布局、图表钻取、拖拽、本页导出

## 验收标准

1. 登录后默认落地页展示 v2 三层结构，左列 / 空档 / 瀑布 / 矩阵 / 利用率全部为真实 API 数据，无原型写死常量。
2. `GET /dashboard/v2` 一次返回全部读模型；口径单点在服务端，前端不跨分页接口自行拼装经营指标。
3. 利用率与 Calendar v2 同一月视图数值一致（parity 契约测试）；未来空档复用同一 settings/算法。
4. 收入瀑布的待收/在途来自 `amount_paid`/`outstanding_amount`/`paid_at` 真实字段，待收账龄锚 `delivered_at`（见共享语言），无 `price/2` 类推算；「在途」的精确口径以 ITEM-4 设计冻结时的定义为准。
5. 交付队列的应交日由账号 SLA + 订单覆盖落库，改 SLA 不重写历史订单。
6. 渠道×类型矩阵按订单下单时快照归因，客户渠道/套系类型后改不影响历史归因。
7. 旧 `GET /dashboard` 与 `DashboardPage` 行为不回退；`make check` 全绿、OpenAPI `make generate` 零漂移。
8. 订单表单/详情可录入已收金额与应交日覆盖，settings 可编辑 SLA 默认值；录入后瀑布与交付队列数字随之变化。

## 共享语言与概念边界

沿用 `.codestable/requirements/CONTEXT.md` 既有单义术语（账号 / 客户 / 社交身份 / 渠道 / 档期 / 套系 / 订单 / 提醒 / 线索）。本 Epic 只新增局部边界：

| 术语 | 本 Epic 边界 | 来源 |
|---|---|---|
| 已确认收入 | delivered_at 落窗 ∧ balance_paid=true ∧ 非 cancelled 的 price 之和 | roadmap §4.3 既有 `revenue_confirmed` |
| 已收现金 | 新增 `amount_paid` 字段之和 | 本 Epic DEC-4 |
| 待收尾款 | `outstanding_amount`（余额未清部分），非报价推算 | 本 Epic DEC-4 |
| 在途 | 已交付未结清 vs 已拍摄未交付的边界，ITEM-4 设计冻结时定稿并回写本表 | 本 Epic |
| 客单价 | 窗口内已确认收入 ÷ 窗口内计入已确认收入的订单笔数 | 本 Epic |
| 90 天复购占比 | 近 90 天窗口内按 `shot_at` 拍摄 ≥2 次的客户占比；精确口径 ITEM-4 设计冻结 | 审计 §4 |
| 待收账龄 | 今日 − 最早一笔未结清已交付订单的 `delivered_at` | 本 Epic |
| 环比 | 与上一个相邻 30 天窗口的同口径比较 | 本 Epic |
| 归因快照 | 订单创建时固化的 `channel_snapshot`/`shoot_type_snapshot` | 本 Epic DEC-5 |
| 线索（Lead） | 客户状态视图（无 delivered/closed 订单），**非独立实体** | CONTEXT.md |
| 下一场（next_shoot） | 今日及之后最早的 shoot 档期，跨日/取消口径见 ITEM-4 | 本 Epic |

## 关键决策

- **DEC-1 · 待办窗口统一**：沿用正式口径 `status=pending ∧ due_date ≤ 账号时区今日+2`（含逾期、含全部 type）。废弃原型"仅 pending 无上界"。与 v1 `dashboard-design` 术语一致。
- **DEC-2 · 利用率口径统一**：Dashboard 只做展示派生，不新造算法；统一采用 Calendar v2 的"工作窗口内 shoot+hold 占用分钟占比"口径。废弃原型"有拍摄的天数"。
- **DEC-3 · 空档/时段以 settings 为准**：删除原型 09:00–21:00 / ≥90 分钟常量；复用 `GET /settings` availability（工作日窗口、最小可约时长、转场缓冲）与 Calendar openings 算法。
- **DEC-4 · 金额语义四态**：已确认（`revenue_confirmed` 既有口径）/ 已收现金（`amount_paid`）/ 待收（`outstanding_amount`）/ 在途（待定，见遗留风险）。支付事实 = 订单最小字段，不做支付流水表。已收现金在 v2 UI 作为瀑布卡 KPI 行辅助指标呈现，不是瀑布三段之一。
- **DEC-5 · 归因快照**：订单表固化 `channel_snapshot`/`shoot_type_snapshot`，下单时取客户当前渠道与套系拍摄类型。历史订单不回写快照（遗留风险说明）。
- **DEC-6 · 交付 SLA 来源**：`delivery_due_at = shot_at + 账号级默认 SLA 天数`，订单级可覆盖；SLA 默认值进 settings（与 availability 同级），不写死 14 天。
- **DEC-7 · 端点版本策略**：保留 `GET /dashboard`（旧五块）不动，新增 `GET /dashboard/v2`。两端点并存，deprecate 时机见遗留风险，不在本 Epic 内合并。
- **DEC-8 · 聚合读模型归属**：`dashboard/v2` 读模型落在 dashboard 域，经 `AccountScope` 直查 orders/slots/reminders/customers/packages，复用 `schedule.AssembleListItems` 摘要装配，不注入他域 service（compound cross-domain-read-model）。
- **DEC-9 · openings/利用率单一权威实现**：当前算法只存在于前端 TS（`frontend/src/pages/calendar/model.ts`、`openings.ts`）；ITEM-4 在服务端（Go）落**权威实现**，Go/TS 以共享 golden fixtures 契约测试对拍（归 ITEM-4）；Calendar 前端切换为消费服务端结果下放二期，双实现并存期漂移记遗留风险。
- **DEC-10 · 金额联动推定**：任何写路径（含既有 v1「标记收讫」`PATCH {balance_paid:true}` 与 POST 补录建单）在置 `balance_paid=true` 且未显式提供金额时，推定 `amount_paid=COALESCE(price, 既有 amount_paid, 0)`（推定不降低既有 `amount_paid`）、`outstanding_amount=0`；未结清情形——录入/修正 `amount_paid` 而未显式提供 `outstanding_amount` 时，推定 `outstanding_amount = max(price − amount_paid, 0)`（`price IS NULL` 时为 NULL 且不计入待收合计）。显式金额始终优先。不变量为**单向**：`balance_paid=true ⇒ outstanding_amount=0`（已收讫订单 outstanding 钉死 0，后改 price 不重算）；反向不要求——录满全款而未点收讫的订单 outstanding 为 0 但仍留在待收尾款卡，提示摄影师显式确认收讫（2026-08-23 owner 裁决）。保证 v1 行为不回退。

## 子项契约

- **ITEM-1 · 交付 SLA 与交付队列事实**（owning: cs-feat）
  可交付：`delivery_due_at` 字段 + 账号级 SLA 默认值（settings）+ 订单级覆盖；`shot_at` 首次落值时自动写入（含补录创建即带 shot_at 的订单）；存量已有 `shot_at` 的订单 backfill（含 closed；cancelled 不回填——不进入交付队列）。
  依赖：none。
  验收要点：SLA 默认值进 settings；订单覆盖生效；shot_at 变更后 due 重算；改 SLA 不改历史已落库订单；存量 backfill 已执行且覆盖面正确（含 closed、排除 cancelled）；导出 schema 兼容（统一决策见 ITEM-6）。
- **ITEM-2 · 支付事实字段**（owning: cs-feat）
  可交付：订单 `amount_paid`/`outstanding_amount`/`paid_at` 字段 + migration + POST（含 backfill 建单）/PATCH 双写路径扩展 + 金额一致不变量（DEC-10 推定）。
  依赖：none。
  验收要点：金额字段经创建与修正两条路径可写；单向不变量 `balance_paid=true ⇒ outstanding_amount=0` 恒成立（DEC-10）且 closed 恒结清不变量不破；v1「标记收讫」`PATCH {balance_paid:true}` 行为不回退（DEC-10 推定生效，推定不降低既有 amount_paid）；部分收款联动——录入定金/部分款后 `outstanding_amount` 按 DEC-10 推定随之变化（支撑验收标准 8 待收段）；存量 backfill 规则——已结清单推定 `amount_paid=COALESCE(price,0)`、`outstanding=0`，未结清单 `amount_paid=0`、`outstanding=price`（`price IS NULL` 时 outstanding 为 NULL 且不计入待收合计）；无 `price/2` 推算；导出 schema 兼容（统一决策见 ITEM-6）。
- **ITEM-3 · 归因快照**（owning: cs-feat）
  可交付：订单 `channel_snapshot`/`shoot_type_snapshot` 字段 + migration + 创建时固化。
  依赖：none。
  验收要点：新建订单固化下单时渠道/拍摄类型；客户渠道或套系类型后改不改变历史订单归因；无套系订单 `shoot_type_snapshot` 为 NULL、矩阵归「未归因」桶（展示细节归 ITEM-4）；存量订单 best-effort 回填（记遗留风险）；导出 schema 兼容（统一决策见 ITEM-6）。
- **ITEM-4 · `/dashboard/v2` 聚合读模型**（owning: cs-feat）
  可交付：OpenAPI `GET /dashboard/v2` 契约 + dashboard 域聚合实现 + router 手工注册 + codegen 切片。
  依赖：ITEM-1, ITEM-2, ITEM-3。
  验收要点：一次返回 next_shoot / today_slots / today_openings / delivery_queue / revenue_waterfall（含环比）/ schedule_utilization / channel_matrix / due_reminders 行样式；复用 schedule 摘要装配无 N+1；openings/利用率的 Go 权威实现与前端 TS 以共享 golden fixtures 对拍（DEC-9）；「在途」定义在本子项设计冻结时定稿并回写共享语言表；渠道矩阵含未归因桶；时区/日界同源；跨日 shoot、取消 slot、无 availability 边界明确。
- **ITEM-5 · 前端接入与写侧录入**（owning: cs-feat）
  可交付：`DashboardPage` 接 `GET /dashboard/v2`；原型写死常量替换为 settings/API；文案复制弹层复用 openings 文案；订单表单/详情金额录入（amount_paid/paid_at、应交日覆盖）；settings 页 SLA 默认值编辑。
  依赖：ITEM-4（dashboard 面）；写侧录入部分仅依赖 ITEM-1, ITEM-2。
  验收要点：前端类型引用 `schema.d.ts` 禁手写 DTO；旧五块不回退；无 `usePrototypeStore` 残留；375px/移动端不崩；金额/SLA 录入后瀑布与交付队列数字随之变化（验收标准 8）。
- **ITEM-6 · 契约与文档回写**（owning: cs-roadmap update）
  可交付：roadmap 新增 dashboard-v2 条目及 §4.3 契约；CONTEXT.md 若有新术语同步；items.yaml 状态回写。
  依赖：每个含契约变更的子项（ITEM-1/2/3/4）implement 启动前，其 §4 语义措辞先经 `cs-roadmap update` 定稿（沿用 v1 dashboard D11 先例，避免机器形式领先语义权威源）；acceptance 阶段整体定稿。
  验收要点：机器契约与 roadmap 权威源一致；变更走 `cs-roadmap update` 不绕开；§4.6 导出契约对 ITEM-1/2/3 全部新增字段统一一次决策（schema_version bump 或可选字段向后兼容），不逐子项各自为政。

## 最终交付索引

- 后端：`backend/internal/dashboard/` 扩展 v2 聚合；订单域三组字段迁移（delivery_due_at / 金额 / 快照）。
- 契约：`api/openapi.yaml` + `api.gen.go` + `schema.d.ts` 再生成。
- 前端：`DashboardPage.tsx` 接 v2；订单表单/详情与 settings 页的金额/SLA 写侧录入；文案弹层复用 openings。
- 文档：roadmap §4.3、items.yaml、CONTEXT.md（如需）、docs/api。

## 整体验收

（见「验收标准」1–8；以 `make check` 全绿 + 各子项验收要点证据 + parity 契约测试为门槛，最终 owner gate 另行发起 final acceptance review。）

## 遗留风险

1. **在途金额定义未定**：已交付未结清、已拍摄未交付、已交付已结清之外的"在途"窗口边界需在 ITEM-4 设计阶段定死，否则瀑布三段对不上。
2. **`/dashboard` 与 `/dashboard/v2` 并存**：旧接口不 deprecate 前两套读模型并存，有口径漂移风险；合并时机留待二期或 owner 再拍板。
3. **退款不在本版**：金额字段无退款语义，退单只能改 `amount_paid`/`outstanding_amount` 手工修正，无流水可追溯。
4. **存量订单归因快照 best-effort**：历史订单按当前客户渠道/套系类型回填，可能与真实下单时不一致，仅作展示不做精确承诺。
5. **健康度与 churn 口径冲突未解决**：二期健康度分层与 reminder 固定阈值 churn 是两套"流失"结论，需先决策共用或分离，本 Epic 不碰。
6. **openings/利用率 Go/TS 双实现并存期漂移**：权威实现在服务端（DEC-9），但 Calendar 前端切换为消费服务端结果下放二期，期间两套实现靠共享 golden fixtures 对拍兜底，仍有漂移窗口。
7. **存量金额 backfill 系统性误差**：历史定金已收但金额从未被记录，backfill 按 `amount_paid=0` 处理会低估已收现金、虚高待收；可逐单手工修正，不承诺历史金额精确。
