# Dashboard v2 原型与现有后端能力调查

- 调查日期：2026-08-22
- 调查对象：`frontend/proto-design/v2/dashboard.html` 及其 `dashboard.js` / `dashboard-data.js`
- 判断标准：能否依靠当前正式后端契约稳定上线，而不是能否在演示数据量下由前端临时拼出画面
- 范围：只读调查；本报告未修改业务代码

## 结论摘要

**当前后端可以完整支撑旧版 Dashboard 的五块核心能力，但不能直接支撑 `v2/dashboard.html` 的完整设计。**

已经具备正式后端契约、服务端聚合、OpenAPI、前端真实消费和测试的范围是：

1. 近三天（含逾期）的 pending 提醒；
2. 今日档期及订单/客户摘要；
3. 已交付且未结清的订单；
4. 全量 pending 流失预警；
5. 近 30 个账号本地自然日的新建订单数、交付数和确认收入；
6. 上述提醒的完成/忽略，以及待收订单的尾款已收标记。

这些能力由 `GET /api/v1/dashboard` 一次聚合返回，当前生产首页 `frontend/src/pages/DashboardPage.tsx` 已经接入该接口。正式契约只承诺这五块，而不是 v2 原型展示的全部分析模块：

- 五块响应契约：`api/openapi.yaml:1625-1683`
- 五块聚合模型：`backend/internal/dashboard/model.go:11-39`
- Dashboard handler：`backend/internal/platform/httpapi/dashboard.go:12-57`
- 前端真实消费：`frontend/src/pages/DashboardPage.tsx:182-307`
- 需求边界：`.codestable/requirements/dashboard.md:24-34`

v2 原型新增的“后期交付、收入瀑布、档期利用率、咨询转化、客户健康度、渠道×类型收入”等模块，大多能找到部分原始字段，但当前没有对应的稳定后端读模型、历史事件、金额事实或统一统计口径。因此不能把它们视为“后端已经支持”。

## 支持级别定义

| 级别 | 含义 |
|---|---|
| A | 当前正式接口已经提供所需语义；前端只做展示性派生即可。 |
| B | 原始字段或相关接口存在，但需要跨接口拼装、前端自行定义口径、硬编码常量，或存在历史/金额语义风险；可做探索，不宜直接承诺为稳定经营指标。 |
| C | 关键业务事实没有被记录，无法从当前数据可靠反推。 |

## 模块逐项核对

### 1. 下一场拍摄与今日时间轴：B（时间轴主体接近 A）

**原型需要**

- 今日拍摄按开始时间排序；
- 开始/结束时间、时长、客户、订单、套系和备注；
- 预留、个人占用、当前时间线；
- 09:00–21:00 内不少于 90 分钟的空档；
- 下一场倒计时；
- 客户头像、平台/账号、健康标签、累计 LTV、历史拍摄次数。

原型的时间轴和空档算法见：

- 页面结构：`frontend/proto-design/v2/dashboard.html:42-60`
- 时间轴渲染：`frontend/proto-design/v2/dashboard.js:41-158`
- 演示数据字段和算法：`frontend/proto-design/v2/dashboard-data.js:87-100`、`:291-310`

**当前已有能力**

`GET /dashboard.today_slots` 已按账号本地今日半开日界返回相交档期；`shoot` 项有订单/客户摘要，`hold`/`busy` 项有备注；返回结果与 `GET /schedule/slots` 共用 `schedule.AssembleListItems`，并按 `start_at ASC, id ASC` 排序：

- Dashboard 今日档期窗口：`backend/internal/dashboard/service.go:59-73`、`backend/internal/dashboard/repository.go:40-47`
- 档期摘要装配与排序：`backend/internal/schedule/repository.go:153-185`、`:542-549`
- shoot 摘要字段：`backend/internal/platform/httpapi/api.gen.go:973-1017`
- 非 shoot 字段：`backend/internal/platform/httpapi/api.gen.go:710-724`

因此，**今日档期列表、时间轴事件和“下一场”的基础信息可以复用**。

**缺口**

档期摘要没有主社交身份、头像、健康层级、拍摄次数或已收 LTV。客户真实模型是多 `identities[]`，也没有“主身份”字段；客户详情的 `total_order_amount` 是非取消订单的报价总额，不等于 v2 所称的已收 LTV：

- 客户详情摘要契约：`api/openapi.yaml:2217-2288`
- 客户领域模型：`backend/internal/customer/model.go:54-66`
- 原型焦点卡：`frontend/proto-design/v2/dashboard.js:61-100`

若要完整还原焦点卡，需要服务端增加客户/焦点卡摘要，或接受逐客户/逐订单补请求及相应 N+1 风险；还要明确“进行中的拍摄、跨日拍摄、已取消订单引用的 slot”如何进入“下一场”。

### 2. 今日待办：A-（核心能力 A，完整 v2 行样式 B）

**当前已有能力**

`due_reminders` 的正式口径是 `status=pending` 且 `due_date <= 账号时区今日+2`，含逾期和全部类型；一条 churn 提醒可同时出现在待办和流失预警中。提醒可通过既有端点完成/忽略：

- 待办契约：`api/openapi.yaml:1640-1644`
- 查询实现：`backend/internal/dashboard/repository.go:65-80`
- 完成/忽略端点：`frontend/src/api/client.ts:532-538`
- 原型待办展示：`frontend/proto-design/v2/dashboard.js:160-217`

**差异与缺口**

v2 原型的 `dueReminders()` 只筛 `pending`，没有上界，和正式“今日+2 天”的口径并不一致：

- 原型：`frontend/proto-design/v2/dashboard-data.js:326-327`
- 正式需求：`.codestable/features/2026-07-14-dashboard/dashboard-design.md:15-23`

正式 `Reminder` 有类型、客户 ID、订单 ID、日期和内容，但没有客户名称、头像、渠道或原型里的 `copy` 模板类型：

- `Reminder` schema：`api/openapi.yaml:2574-2604`

因此核心待办闭环可以直接支撑；要完整还原 v2 行样式，需要客户摘要/主身份投影和文案模板上下文，且先统一待办时间窗。

### 3. 后期交付队列：B

**原型口径**

- 只选 `shot`、`selected`、`retouching`；
- 拍摄后固定 14 天交付 SLA；
- `due_date = shot_at + 14 天`；
- 按应交日排序并展示阶段、剩余/逾期天数和风险条。

证据：`frontend/proto-design/v2/dashboard-data.js:329-355`、`frontend/proto-design/v2/dashboard.js:219-258`。

**当前已有字段**

订单有八态状态、`shot_at`、`delivered_at`，所以在小数据量下可以用订单列表拼出一份近似队列：

- 订单契约：`api/openapi.yaml:2393-2432`
- 订单领域模型：`backend/internal/order/model.go:46-60`

**不能直接承诺的原因**

- `/orders` 只支持单个 `status`，没有交付队列或状态集合接口；
- 14 天只是 v2 JavaScript 常量，不是账号、套系或订单级契约；
- 没有 `delivery_due_at`、承诺交付日、SLA 来源或延期记录；
- 订单列表有分页，完整队列需要跨页拉取；
- 若未来修改 SLA，历史订单的应交日会被重新解释。

建议至少定义 `delivery_due_at` 或“账号默认 SLA + 订单覆盖”的来源和落库规则，再提供服务端队列读模型。

### 4. 收入构成/瀑布：B-

**当前可复用**

正式 Dashboard 已提供近 30 天 `revenue_confirmed`，且 `unpaid_orders` 提供已交付未结清订单；后端口径是 `delivered_at` 落窗、`balance_paid=true`、当前非 cancelled 的 `price` 总和：

- OpenAPI：`api/openapi.yaml:1650-1679`
- 实现：`backend/internal/dashboard/repository.go:149-173`
- 原型瀑布算法：`frontend/proto-design/v2/dashboard-data.js:205-235`

**缺口**

v2 还需要：

- 上一个 30 天确认收入和环比；
- 待收尾款金额；
- 在途金额；
- 客单价、90 天复购占比、最长待收账龄；
- 三段明细的稳定集合和统一窗口。

当前没有这些聚合字段或日期范围订单接口。订单只有报价 `price`、`deposit_paid` 和 `balance_paid` 两个布尔值，没有定金金额、已收金额、尾款金额、支付时间、退款或支付流水：

- 订单表字段：`backend/internal/platform/store/migrations/0005_orders.up.sql:1-30`
- 正式设计明确禁止用报价推算尾款：`.codestable/features/2026-07-14-dashboard/dashboard-design.md:50-53`

因此当前最多展示“订单报价/待收订单笔数”和“按交付日期确认的报价总和”，不能把它包装成精确尾款或现金到账。若要做真实财务语义，需支付事实模型，至少包括 `amount_paid`、`outstanding_amount`、`paid_at` 和退款处理；更稳妥的是支付流水。

另一个口径冲突是：正式后端近 30 天窗口为账号本地 `[今日-29日 00:00, 次日 00:00)`，而原型的已确认收入使用 `TODAY-30` 起点和 fallback anchor：

- 后端窗口：`backend/internal/dashboard/service.go:63-73`
- 原型窗口/anchor：`frontend/proto-design/v2/dashboard-data.js:128-135`、`:205-235`

必须先统一窗口和“确认收入/已收现金/应收/在途”的定义。

### 5. 档期利用率与未来空档：空档 A；利用率 B

**当前已有能力**

该项不能再按旧原型注释判断为“后端没有工作时段”：当前 settings 已正式保存账号级 `availability`，包含每周工作窗口、最小可约时长和转场缓冲；日历前端已经用 `GET /settings` + `GET /schedule/slots` 计算未来空档和月度概览：

- Settings OpenAPI：`api/openapi.yaml:2615-2730`
- 数据库迁移：`backend/internal/platform/store/migrations/0014_settings_availability.up.sql:1-13`
- 日历加载档期和 availability：`frontend/src/pages/CalendarPage.tsx:194-242`、`:324-358`
- 空档计算：`frontend/src/pages/calendar/model.ts:115-164`、`:402-463`
- 月度利用率计算：`frontend/src/pages/calendar/model.ts:166-214`

所以，若 Dashboard 复用日历的模型和 settings，未来空档可以支撑；不需要新增“是否有 availability”的基础能力。

**当前缺口**

两个页面的利用率口径不一致：

- v2 Dashboard 原型：`有拍摄的天数 / 非全天休假天数`，见 `frontend/proto-design/v2/dashboard-data.js:260-288`；
- 现行 Calendar v2：`shoot + hold 占用分钟 / 工作窗口分钟`，取消档期和 busy 的处理也不同，见 `frontend/src/pages/calendar/model.ts:166-214`。

还需统一：hold、busy、全天占用、取消拍摄、重叠、过去日期、无工作窗口、DST 日界和工作窗口时区如何计入分子/分母。否则 Dashboard 和 Calendar 会给摄影师不同的利用率。

### 6. 咨询转化：C

**原型口径**

- 近 30 天创建且未取消的订单都算 leads；
- 当前状态不是 `consulting` 就算 converted；
- 平均周期用 `created_at -> shot_at`；
- 当前仍为 `consulting` 的订单作为跟进线索。

证据：`frontend/proto-design/v2/dashboard-data.js:357-384`。

**为什么当前后端不能可靠支撑**

订单允许创建时直接进入后续状态，历史补录也可以直接写后续状态，因此 `created_at` 不是“咨询发生时间”，当前非 `consulting` 也不能证明订单经历过咨询：

- 新建订单状态/补录语义：`api/openapi.yaml:1043-1060`
- 订单状态字段：`backend/internal/order/model.go:46-60`

系统没有咨询实体，也没有 `consulted_at`、`scheduled_at`、`converted_at`、`lost_at` 或状态变更事件历史。`orders_created` 只统计全部状态，不能代替咨询数：

- Dashboard recent stats：`api/openapi.yaml:1667-1679`
- Dashboard 实现：`backend/internal/dashboard/repository.go:149-173`

这是最明确的 C 级缺口。要实现可靠转化率，需要先决定使用独立 Inquiry/Lead 实体，还是订单状态事件表，并明确 cohort、取消是否入分母、补录是否排除、什么时刻算成交。

### 7. 客户健康度分层：B-

**原型口径**

- `距上次拍摄天数 / 历史平均拍摄间隔`；
- 只有一次拍摄时回退 120 天基线；
- 阈值：`<=1.2` 活跃、`<=2` 沉睡、`<=3.5` 高危、`>3.5` 已流失；
- 无拍摄为新客；
- 高危/流失按累计已收 LTV 排序。

证据：`frontend/proto-design/v2/dashboard.html:139-146`、`frontend/proto-design/v2/dashboard-data.js:150-203`、`frontend/proto-design/v2/dashboard.js:418-512`。

**当前已有字段**

订单的 `shot_at` 可以提供历史拍摄日期；客户列表有 `orders_count`/`last_shot_at`，客户详情有订单报价总额：

- 客户列表/详情契约：`api/openapi.yaml:2217-2288`
- 订单字段：`backend/internal/order/model.go:46-60`

**缺口**

当前没有健康度、平均节奏、ratio、baseline、tier 或 LTV 读模型。若逐客户请求订单再计算，会产生 N+1，并且难以保证拉全历史。提醒引擎的 churn 规则是按拍摄类型的固定天数阈值，不是个人节奏模型：

- churn 规则：`backend/internal/reminder/service.go:455-517`
- settings churn thresholds：`api/openapi.yaml:2606-2613`

还需明确同日多单、取消/补录、客户 merge、账号时区、只有一次拍摄和 LTV 的收入语义。建议形成服务端 `CustomerHealth` 读模型，并与 reminder churn 规则明确关系，避免两套“流失”结论互相冲突。

### 8. 渠道 × 类型收入矩阵：B-

**当前已有字段**

客户有来源 `channel`，订单有客户/套系/报价/尾款状态，套系有 `shoot_type`，所以技术上可以跨接口取数后计算：

- Customer channel：`api/openapi.yaml:2094-2102`
- Package shoot type：`api/openapi.yaml:2131-2137`
- 原型聚合：`frontend/proto-design/v2/dashboard-data.js:237-252`

**缺口与风险**

- `OrderListItem` 没有 `shoot_type`，仍需额外读取套系；
- 当前没有按渠道/类型聚合的服务端接口；
- 客户渠道和套系类型都可以后改，历史订单会随当前主数据变化而重写历史归因；
- `balance_paid=true` 仅是状态布尔值，不是实际已收金额；
- 原型没有明确排除 cancelled，和正式“确认收入”语义可能不一致。

需要先决定矩阵按当前维度还是下单时快照，并统一取消、退款、客户 merge、窗口和“累计已收”的定义。若要作为长期经营指标，建议保存下单时渠道/拍摄类型快照或分析事件。

### 9. 触达文案、复制和弹层：复制 A；个性化 B；发送闭环 C

弹层、编辑 textarea 和剪贴板复制是纯前端行为，当前不需要后端；现行 Calendar 也已经用真实空档生成可复制文案：

- 原型弹层/复制：`frontend/proto-design/v2/dashboard.js:547-647`、`:702-728`
- 现行空档文案：`frontend/src/pages/calendar/openings.ts:1-28`
- Calendar 消费：`frontend/src/pages/CalendarPage.tsx:620-626`

但 Reminder 契约没有原型 `copy` 类型，只有 `type/customer_id/order_id/content/status`：`api/openapi.yaml:2574-2604`。如果只是前端模板生成和复制，可以保留在 UI；如果需要统计“已触达/已回复”、模板版本、避免重复唤回或真正发送，就需要独立的 message template/contact event 契约，不能把 reminder 的 `done` 当作“消息已发送”。

## 现有后端已支撑范围的调用链

旧版五块不是规划中的空壳，而是已经落地：

```text
GET /api/v1/dashboard
  -> httpapi.GetDashboard
  -> dashboard.Service.Get
  -> 账号时区与自然日窗口
  -> dashboard.Repository.LoadDashboard
  -> AccountScope 查询 reminders / schedule_slots / orders
  -> JSON 五块响应
```

对应代码：

- 路由：`backend/internal/platform/httpapi/router.go:159-162`
- handler：`backend/internal/platform/httpapi/dashboard.go:33-57`
- 时区与窗口：`backend/internal/dashboard/service.go:52-75`
- 五块查询：`backend/internal/dashboard/repository.go:27-63`
- 生产组装：`backend/cmd/server/main.go:147-157`、`:190-212`
- 前端 API client：`frontend/src/api/client.ts:73-78`、`:482-484`

账号隔离由 `AccountScope` 强制加入账号条件，Dashboard 不接受客户端 `account_id`：

- handler 取账号上下文：`backend/internal/platform/httpapi/dashboard.go:35-47`
- scope 基座：`backend/internal/platform/store/scope.go:51-77`、`:87-119`

## 关键语义不一致

### A. v2 待办与正式 Dashboard 时间窗不同

v2 原型只筛 `pending`；正式契约是 `pending && due_date <= 今日+2`，并包含历史逾期。必须先选定“今日待办”的唯一口径。

### B. Dashboard v2 与 Calendar v2 利用率不同

Dashboard v2 按“有拍摄的天数”计算；Calendar v2 按“工作窗口内 shoot/hold 占用分钟”计算。两者即使使用同一档期数据，也会给出不同百分比。

### C. 原型固定 09:00–21:00/90 分钟与现行 availability 不同

Dashboard 原型写死 09:00–21:00、空档至少 90 分钟：`frontend/proto-design/v2/dashboard-data.js:386-405`。现行 Settings availability 默认工作窗口为工作日 10:00–19:00、周末 09:00–20:00，最小空档 120 分钟：`backend/internal/platform/store/migrations/0014_settings_availability.up.sql:1-13`。真实实现必须复用账号配置，而不是继续写死原型常量。

### D. 原型“尾款金额”不是可由当前模型准确得出的事实

当前只有报价和两个收款布尔值，没有定金金额或支付流水。可以展示报价和订单笔数，但不能从 `price/2` 或其他比例推算尾款。正式设计已明确这一限制：`.codestable/features/2026-07-14-dashboard/dashboard-design.md:50-53`。

### E. 原型“咨询转化”不是当前订单数据的可靠推导

当前订单状态允许直接创建/补录到后续状态，没有咨询发生和转化事件。继续使用 `created_at + current status` 会混入直接建单、历史补录和真实咨询转化。

## 建议的补齐顺序

### P0：先冻结产品口径

1. 统一 Dashboard 与 Calendar 的 availability、时区、DST、空档最小时长和利用率定义；
2. 统一待办窗口（全部 pending、截至今日，或近三天含逾期）；
3. 明确确认收入、已收现金、应收、在途四种金额语义；
4. 明确咨询转化是订单状态快照还是独立线索生命周期；
5. 明确健康度是否与 reminder 的固定阈值共用或分离。

### P1：优先补事实模型

1. 咨询/线索实体或订单状态事件历史；
2. 订单交付承诺日/SLA 来源；
3. 支付流水或至少应收/已收/支付时间/退款字段；
4. 渠道与拍摄类型的订单发生时快照；
5. 客户健康度读模型（历史拍摄节奏、基线、tier、LTV）。

### P2：再补 Dashboard v2 聚合读模型

建议扩展版本化 Dashboard 读模型（或新增 `/dashboard/v2`），由服务端一次返回：

- `next_shoot`、`today_slots`、`today_openings`；
- `delivery_queue`；
- `revenue_waterfall`、上一窗口和明细；
- `schedule_utilization`、`upcoming_openings`；
- `conversion_stats`、`consulting_leads`；
- `health_cohorts`；
- `channel_matrix`。

不要让前端通过客户、订单、套系、档期多个分页接口自行拼装这些经营指标；正式 Dashboard 的设计原则已经明确服务端聚合和口径单点：`.codestable/features/2026-07-14-dashboard/dashboard-design.md:24-53`。

## 最终判定表

| 原型模块 | 当前判定 | 结论 |
|---|---:|---|
| 今日档期 / 时间轴基础信息 | B（主体可复用） | 今日 slot 和摘要已有；焦点卡的健康/LTV/主联系方式缺少投影。 |
| 今日待办与完成/忽略 | A- | 核心闭环已有；v2 行样式和时间窗需统一。 |
| 后期交付队列 | B | 订单字段够做近似，但 SLA/交付队列契约不存在。 |
| 收入瀑布 | B- | 近 30 天确认报价总和已有；在途、环比、真实尾款和支付事实不足。 |
| 档期利用率 | B | 档期和 availability 已有，但口径与 Calendar 不一致。 |
| 未来空档 | A（复用 Calendar 模型） | Settings availability + slot 区间已有；需复用而非照抄原型常量。 |
| 咨询转化 | C | 缺少咨询发生、转化和状态历史事实。 |
| 客户健康度 | B- | shot_at 可算原始节奏，但没有服务端读模型且与 churn 口径未统一。 |
| 渠道×类型收入 | B- | 维度字段存在，但没有聚合、支付事实和历史归因快照。 |
| 文案复制弹层 | A（纯前端复制） | 只复制无需后端；发送/触达闭环需要新契约。 |

**建议结论：不要直接把 v2 原型作为“当前后端可支撑的设计”实现。应先把它拆成“现有五块可直接接入”和“v2 分析扩展”两阶段；其中咨询转化、金额事实、交付 SLA、归因快照是扩展阶段的前置依赖。**
