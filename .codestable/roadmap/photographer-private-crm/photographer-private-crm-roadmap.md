---
doc_type: roadmap
slug: photographer-private-crm
status: active
created: 2026-07-05
last_reviewed: 2026-07-31
tags: [crm, photographer, mvp, reminder, scheduling]
related_requirements: [customer-profile, package-catalog, order-tracking, schedule-calendar, reminder-engine]
related_architecture: [001-account-scoped-data-model, 002-postgresql-as-primary-store, 003-monolith-first-gin-openapi]
---

# 摄影师私域客户经营系统 · 首版路线

## 1. 背景

Owner 是摄影师，客户全部来自私域（微信 / QQ / Telegram），客户信息、约单、档期、套系分别散落在聊天软件、系统日历和备忘录里，遗忘和流失严重。2026-07-05 脑暴（`.codestable/brainstorms/photographer-private-crm/brainstorm.md`）敲定首版走"先治忘"路线：客户档案 + 提醒引擎做厚，档期中等，订单与套系轻量。定位为自用起步、预留产品化（ADR-001：全量数据带账号维度）。产品形态为响应式 Web（桌面优先），提醒触达 = Telegram Bot 推送 + Web dashboard 组合。

本 roadmap 把首版拆成可依次交付、可独立验证的子 feature 序列，并定下各 feature 共同遵守的接口契约。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- 平台基座：单账号登录、账号维度数据隔离、前后端脚手架与验证基线（greenfield，从零建）
- 客户档案：30 秒建档、多平台身份、渠道归因、转介绍、偏好备注、归档与合并（对应 req `customer-profile`）
- 套系管理：商品定义（类型 / 定价方式 / 交付参数）+ 上下架 + 删除（引用完整性保护）
- 订单记录：状态流转 + 定金 / 尾款标记（轻量，不碰支付）
- 档期管理：月/周双视图、客户 / 订单关联、跨日与全天占用、重叠与转场提示、可约空档回答、移动端完整操作和异常恢复
- 提醒引擎：生日 / 拍后回访 / 流失预警，幂等生成，参数可配置
- 触达：Telegram Bot 绑定与每日摘要 + dashboard 今日待办
- 数据资产可带走：全量 JSON 导出（4.6 契约）
- 首版收口：空态 / 错误态、移动轻路径、回归与文档

### 明确不做（本版）

- 支付集成——收款继续走支付宝 / 微信，系统只记定金 / 尾款布尔标记
- 在线选片 / 云盘交付——另一个产品的体量，订单状态里只记"已选片 / 已交付"
- 微信 / QQ 好友自动同步或任何微信自动化——无开放 API，违规自动化有封号风险
- 客户画像 / 渠道转化分析——二期视图，本版只保证数据链路完整不堵死
- 零成交线索的跟进提醒规则——二期（防首版提醒噪音，见 4.4 churn 规则）
- 多账号注册、计费、运营后台——ADR-001 只要求数据模型预留，不要求实现
- 原生 App / 小程序——响应式 Web 覆盖移动轻路径
- 客户侧入口——客户不登录系统

### Granularity Gate

| 判断项 | 结论 |
|---|---|
| 为什么不是 single feature | 七个模块、十三条可独立交付的子 feature、跨模块接口契约（实体模型 / API / 提醒规则 / TG 协议 / 导出）、依赖构成 DAG，单 feature 装不下 |
| 为什么不是 brainstorm | 脑暴已完成（2026-07-05），定位 / 形态 / 优先级 / 边界均已拍板，目标与完成信号可写成可证伪条目 |
| roadmap 边界 | 只覆盖首版"先治忘"范围（上表）；画像分析、产品化、选片交付明确不做 |
| 最小闭环 | 第 2 条 `customer-core` 完成后：登录 → 30 秒建档（含渠道、1 个或多个社交身份）→ 列表 / 详情可查——"治忘"的第一块地基端到端可演示 |

## 3. 模块拆分（概设）

```
photographer-private-crm
├── platform   平台基座：认证、账号上下文、HTTP 框架、错误封套、数据/对象访问基座、全量导出
├── customer   客户域：客户聚合（身份 / 渠道 / 转介绍 / 备注 / 渐进字段 / 合并 / 归档）
├── package    套系域：拍摄服务商品的静态定义、上下架与删除
├── order      订单域：订单状态机 + 定金尾款标记
├── schedule   档期域：时间段占用、日历查询、重叠提示与展示读模型
├── reminder   提醒与触达域：规则扫描、幂等提醒生成、TG Bot 绑定与推送
└── webapp     Web 前端：各域页面 + dashboard + 响应式完整操作路径
```

### platform · 平台基座
- **职责**：单账号登录发 token；请求中携带账号上下文；错误封套与校验；数据访问基座强制账号过滤（ADR-001 的执行点）；提供业务域拥有的对象存储 port 所需的基础设施 adapter、配置与后台维护装配；全量导出（跨域读，归口基座避免各域各写一半）。不含任何业务实体，也不决定头像业务生命周期。
- **承载的子 feature**：platform-skeleton、customer-avatar（基础设施增量）、data-export（+ v1-hardening 部分）
- **触碰的现有代码**：platform config/httpapi/store、server composition root、Docker/README；customer-avatar 只在此落 provider adapter、后台维护和部署装配，不下放业务 pointer 规则。
- **Depth 判断**：deep——账号隔离、鉴权、错误封套与 provider I/O 装配藏在中间件、repository 基座和 adapter 内，业务域代码不重复写 `account_id` 过滤或本地/OSS 调用；删掉它复杂度会散到所有域，通过 deletion test。

### customer · 客户域
- **职责**：客户聚合根的全生命周期：建档、社交身份增删、渠道、转介绍、备注、渐进字段、头像元数据与生命周期、合并、归档。不管订单 / 档期 / 提醒（它们反向引用客户）；头像二进制经 injected `AvatarObjectStore` port 持久化，客户域不感知本地路径或 OSS SDK。
- **承载的子 feature**：customer-core、customer-profile-complete、customer-avatar
- **触碰的现有代码**：backend customer model/service/repository、客户 schema migration 与 customer HTTP 契约；customer-avatar 增 current pointer、条件写、GC 生命周期编排。
- **Depth 判断**：deep——"合并客户迁移全部关联资源"与“不可变头像对象 + 当前 pointer + 条件写/延迟回收”藏在域内，对外只是稳定的 Customer 与 merge/avatar 端点。

### package · 套系域
- **职责**：套系静态定义（拍摄类型 / 定价方式 / 张数时长底片精修参数）、上下架（active/archived）与删除（带引用完整性保护）。商品心智：上架供下单、下架停售存量仍引用、删除仅限无引用套系。不含库存、不含订单逻辑。
- **承载的子 feature**：package-catalog
- **触碰的现有代码**：无
- **Depth 判断**：浅但独立成域合理——它是订单与流失阈值（按拍摄类型）的引用源，不是 pass-through（有自己的校验、上下架与删除引用完整性语义）。

### order · 订单域
- **职责**：订单创建、八态状态机（含非法跃迁拒绝、状态时间戳自动写入）、定金 / 尾款标记、按客户与全局查询。不碰支付、不管档期时间（引用 slot 由档期域管）。
- **承载的子 feature**：order-tracking
- **触碰的现有代码**：无
- **Depth 判断**：deep——状态机合法跃迁与时间戳规则藏在域内，callers 只调 PATCH 并处理 409。

### schedule · 档期域
- **职责**：时间段（slot）创建 / 查询 / 重叠检测；slot 可关联订单也可为独立忙碌块，支持跨日与全天占用并按账号时区落到相交自然日；为档期展示批量装配订单、客户与套系摘要。不管订单状态（创建 / 删除 slot 不反向改订单），可约空档与周/月布局由 webapp 基于 slots + Settings 纯派生。
- **承载的子 feature**：schedule-calendar、calendar-v2-redesign
- **触碰的现有代码**：无
- **Depth 判断**：deep——重叠检测与区间查询藏在域内。

### reminder · 提醒与触达域
- **职责**：按规则（生日 / 回访 / 流失）每日扫描客户与订单数据、幂等生成提醒；提醒完成 / 忽略；账号级 Settings（时区、提醒参数、digest_hour、telegram_chat_id、可约作息与转场缓冲）归 `backend/internal/settings` 域包，作为 reminder、`GET /me`、calendar 与 telegram-digest 的配置来源；TG Bot 绑定与每日摘要推送仍由后续条目实现。**刻意不拆独立"通知模块"**——首版单通道（TG），拆出来是 pass-through 假 seam；TG 以 injected port 形式存在于本域内（见 4.5）。
- **承载的子 feature**：reminder-engine、telegram-digest
- **触碰的现有代码**：backend reminder/settings 域包、platform store/httpapi 与 server composition root、customer merge、webapp 提醒/设置页及客户档案提醒 tab
- **Depth 判断**：deep——规则参数、幂等键、扫描窗口全部藏在域内，对外只有 Reminder 资源和摘要推送。

### webapp · Web 前端
- **职责**：所有域的页面 + dashboard 聚合面板 + 响应式操作路径；档期页在桌面与移动端都支持查询和 CRUD，周/月布局、可约空档、利用率与转场提示由生成 API DTO 纯派生；CustomerAvatar/CustomerPicker 收口跨页面的鉴权头像与客户选择体验。只消费 4.1 定义的 HTTP API，无后端私有耦合。
- **承载的子 feature**：跨条目（每条子 feature 交付各自的 UI 垂直切片），dashboard 单列一条
- **Depth 判断**：不适用（展示层）；约束是"只经 API seam 取数"。

## 4. 模块间接口契约 / 共享协议（架构层详设）

> 本节是所有子 feature 的硬约束。发现不合理回 `cs-roadmap update` 改，不许在 feature 里绕开。
> 技术栈已拍板（2026-07-05，owner）：Go 后端 + React 前端 + PostgreSQL。选 PG 的理由：merge 是事务型操作、外键引用完整性是系统骨架、账号隔离可升级 RLS、dashboard/二期分析是聚合查询主场；半结构化字段用 JSONB。本节契约为逻辑模型与 HTTP 协议级，不绑定引擎细节。
> 契约固化与代码组织（2026-07-05 拍板）：本节契约自条目 1 起固化为 OpenAPI 文件（§4 保持语义权威源，OpenAPI 是其机器可执行形式），双端 codegen（Go: oapi-codegen / TS: openapi-typescript）保证前后端类型对齐；后端组织为 Gin + 轻量 DDD——domain 包（实体+领域服务）、repository 接口、handler 薄适配层；不引入 CQRS / 领域事件总线等重仪式（需要时另行 ADR）。对外契约维持 JSON/REST 不用 proto——唯一消费者是浏览器，服务间 gRPC 接口待真实拆分时以 proto 新生。

### 4.1 HTTP API 总约定

**方向**：webapp → 所有后端域　**形式**：HTTP + JSON

```
Base:      /api/v1
认证:      Authorization: Bearer {token}（POST /api/v1/auth/login {password} → {token}；首版单账号）
错误封套:  { "error": { "code": string, "message": string, "details"?: object } }
错误码:    400 validation_failed | 401 unauthorized | 404 not_found
           409 conflict（子码见各域）| 500 internal
分页:      ?page=1&page_size=20 → { "items": [...], "total": int }
时间:      ISO 8601 UTC（"2026-07-05T09:00:00Z"）；生日特例 "MM-DD" 或 "YYYY-MM-DD"（年份可缺）
时区:      存储一律 UTC；所有 date-only 字段（due_date / birthday / last_shot_at）、"今日 / 当日 / 逾期"判定、
           digest_hour、统计滚动窗口（dashboard recent_stats），一律按 Settings.timezone（IANA，默认 Asia/Shanghai）计算
ID:        string（引擎无关；服务端生成）
头像媒体:  Customer.avatar_url 固定为同源应用相对 URL
           /api/v1/customers/{id}/avatar/content?v={avatar_version}，不是本地路径、对象存储 key 或厂商 URL；
           读取仍须 Bearer 鉴权。前端统一通过带鉴权的媒体 client 获取 Blob 后展示，禁止把 token 放
           query string，也不开放匿名头像目录；v 是当前版本的强校验条件，不是“忽略即可”的装饰缓存键。
幂等:      POST /orders 与 POST /schedule/slots 接受 Idempotency-Key；组合流程及从档期跳转的历史订单补录必须传。
           静态校验先于 claim；仅成功 2xx 持久化。24 小时内同账号+同操作+同 key+
           同规范化请求重放返回首次成功结果；成功绑定后的同 key 异请求 → 409
           idempotency_conflict。事务结果明确的 4xx/业务409/callback error 回滚且不绑定 key；
           客户端收到带 key 创建的任意 5xx 均按结果未知，用原 body/key 重放；未传 key 仍按原非幂等行为。
```

**约束**：所有资源端点隐式按当前账号过滤，客户端**永不**传 `account_id`（ADR-001：过滤在 repository 基座强制，遗漏视为缺陷）。**Handler 薄层约束**：领域逻辑（状态机 / merge / 提醒规则等）不得 import 路由框架，`gin.Context` 等框架类型不下穿到 service / repository 层——这是将来若微服务化时框架层可低成本置换的前提，进 code review 检查口径。

**幂等执行接口（schedule-calendar 增量）**：`platform/idempotency.ExecuteCreate(ctx, scope, operation, key, normalizedRequest, func(txScope store.TxAccountScope) (StoredResponse, error)) (StoredResponse, error)` 是唯一事务 owner。`operation` 为持久化协议类型，首版固定 `order.create.v1` / `schedule-slot.create.v1` 两个 typed 常量；不得使用路由名、函数名或 handler 临时字符串，新增语义版本只能新增常量并由 migration/contract test 固定。`store.TxAccountScope` 只暴露账号限定的读写能力，不暴露 `WithinTx`；`AccountScope.WithTxScope` 是唯一构造入口，避免 callback 误开第二个事务。store 扩展受控、自动注入 account_id 且校验标识符的 `InsertOnConflictDoNothingReturning`：首次 claim 插入成功；冲突请求等待后返回未插入，再以 `QueryRowForUpdate` 完成同 hash replay、异 hash 409 或过期接管，禁止用普通 INSERT 的唯一键错误把事务置为 aborted。order/schedule service 先 `PrepareCreate`，同一 normalized input 同时用于 canonical hash 与 callback；repository 提供消费 `TxAccountScope` 的 `CreatePreparedInScope`，handler 不复制领域校验，原无 key 路径通过 `WithTxScope` 复用同一节点。`idempotency.TxRunner` 是内部 commit seam，production adapter 调 `WithTxScope`，测试 adapter 可脚本化 committed-but-error / rolled-back-error。首个请求写业务资源与已序列化成功响应记录后一起 commit；事务结果明确的 4xx/业务409/callback error 回滚且不绑定 key；commit 结果未知时原 key/body 安全重试；过期行由 `SELECT FOR UPDATE` 单 owner 接管。只缓存 2xx；HTTP 5xx 不缓存，但客户端无法由此证明事务回滚，必须原 key/body 确认。

**时区读取接口（schedule-calendar 增量）**：`GET /me` 返回 `timezone`。Settings 未落地前由单一 `AccountTimezoneProvider` 返回 Asia/Shanghai；日历只读该值，不使用浏览器时区。provider 可注入 IANA 时区用于 DST 测试，后续 Settings 落地时替换实现而不改前端契约。

**Interface 设计检查**：platform 暴露；invariant = 未认证一律 401、跨账号数据不可见、日界判定单一口径、带 key 的创建只有一个事务 owner；seam 放在 HTTP API 与 platform helper 层。webapp→API 为 remote-owned；存储为 local-substitutable。幂等 callback 只暴露不含事务启动能力的 `TxAccountScope`，不暴露 pgx/Gin；order/schedule repository 需提供消费该 scope 的内部写路径。

### 4.2 共享实体逻辑模型

所有域共享的持久化 shape（字段级契约；`*` = 必填；所有实体含 `id`、`account_id`、`created_at`，不再重复列）：

```
Customer:        display_name*, real_name?, phone?, birthday?("MM-DD"|"YYYY-MM-DD"),
                 channel*(xiaohongshu|douyin|weibo|referral|other),
                 referrer_customer_id?(channel=referral 时必填),
                 status*(active|merged|archived), merged_into_customer_id?,
                 avatar_revision*(readOnly, "ar-" + 非负十进制 bigint),
                 avatar_version?(readOnly), avatar_url?(readOnly)
                 （avatar_revision 始终返回，初始 ar-0，每次成功切换或清空 pointer 后原子 +1；no-op 不变，
                   专供 If-Match 防 ABA。无头像时 avatar_version/avatar_url 缺省；持久化 current pointer 保存
                   storage-neutral 的 avatar_version / avatar_object_id / avatar_media_type / avatar_size /
                   avatar_updated_at 五字段。avatar_object_id 是内部 128-bit 随机代际、永不下发；对象 key
                   按 account/customer/avatar_version/avatar_object_id 派生，永不保存本地绝对路径、bucket
                   或 OSS 签名 URL）
CustomerSummary:  id*, display_name*, channel*, status*, avatar_revision*(readOnly),
                  avatar_version?(readOnly), avatar_url?(readOnly)
                 （保留既有 id/display_name/channel/status，只增头像投影；用于 referrer 等嵌套客户摘要，
                   与 Customer 使用同一头像投影规则，使既有 archived/merged 关系也能显示当前头像，
                   不要求前端额外 GET detail）
SocialIdentity:  customer_id*, platform*(wechat|qq|telegram|xiaohongshu|douyin|weibo|other),
                 handle*, remark?
                 （枚举含来源平台：小红书/抖音/微博账号也是真实私域身份，2026-07-06 原型比对拍板）
CustomerNote:    customer_id*, content*(≤500字)
Package:         name*, shoot_type*(portrait|cosplay|other),
                 pricing_mode*(per_duration|per_photo|fixed), base_price*(分, int),
                 duration_minutes?, shot_count_min?, shot_count_max?,
                 raw_delivery_count?, retouch_count?(0=不含精修), note?(≤500字),
                 status*(active|archived)
                 （套系是商品心智：active=上架中、archived=下架停售；两态为持久状态。
                   "删除"是破坏性操作不是状态，见 §4.3 DELETE；套系无 merged 态，区别于 Customer）
Order:           customer_id*, package_id?, title?,
                 status*(consulting|scheduled|shot|selected|retouching|delivered|closed|cancelled),
                 price?(分), deposit_paid*(bool, 默认 false), balance_paid*(bool, 默认 false),
                 shot_at?, delivered_at?, note?,
                 delivery_due_at?(date), delivery_due_is_override*(bool, 默认 false)
                 （应交付日语义，dashboard-v2-redesign ITEM-1 增量：date-only，非时刻。
                   shot_at 首次落值或变更时自动派生 = 账号时区 LocalDate(shot_at) + Settings.delivery_sla_days，
                   写入即为落库事实；其余写路径（改备注/改价/推进状态）不重算既有值。
                   显式传 delivery_due_at 为订单级覆盖并置 is_override=true，仅已到达拍摄
                   且未取消的订单接受该字段；覆盖后不再随 shot_at 变更重算。PATCH 显式传 null
                   撤销覆盖（is_override 回 false）并按当前 shot_at 重新派生，无 shot_at 时清空；
                   未传该字段保持现状。改 Settings.delivery_sla_days
                   只影响此后新落值的订单，不重写历史。cancelled 订单保留既有落库值——
                   因此 delivery_due_at IS NOT NULL 不等价于「在交付队列中」，
                   消费方必须联合 status 过滤，队列集合口径由 ITEM-4 单点定义）
                 amount_paid*(分, int, 默认 0), outstanding_amount?(分), paid_at?
                 （支付事实语义，dashboard-v2-redesign ITEM-2 增量：
                   amount_paid=已收现金，outstanding_amount=待收余额（NULL=price 未定价，
                   不计入待收合计），paid_at=收款时刻。DEC-10 金额联动推定：显式金额始终优先；
                   置 balance_paid=true 且未显式给金额时推定 amount_paid=max(既有, COALESCE(price,0))
                   （不降低既有）、outstanding_amount=0；未结清且录入/修正 amount_paid（含创建——
                   建单即视为 0 已录入）而未显式给 outstanding_amount 时推定
                   max(price−amount_paid, 0)。单向不变量恒成立：balance_paid=true ⇒
                   outstanding_amount=0，已收讫订单钉死 0，后改 price/金额不重算；反向不要求——
                   录满全款而未点收讫的订单 outstanding=0 但 balance_paid 仍 false，留在待收尾款卡
                   提示显式确认。paid_at 显式提供始终优先；仅 balance_paid 由 false 实际跃迁为
                   true 且未显式提供时自动写服务端 now（v1「标记收讫」路径），创建（含补录结清单）
                   不自动写——不为历史发明收款时刻；已写入可修正、不可置空。金额字段无 null 语义：
                   POST/PATCH 显式 null → 400；终态订单金额可修正（对齐 price，退款/坏账的
                   手工修正通道），deposit_paid/balance_paid 终态不可变不变）
                 channel_snapshot*(渠道枚举，同 customers.channel), shoot_type_snapshot?(portrait|cosplay|other)
                 （归因快照语义，dashboard-v2-redesign ITEM-3 增量：
                   创建事务内固化下单时客户当前渠道与所选套系当前拍摄类型，写入即为落库事实；
                   此后不可变——客户渠道/套系拍摄类型后改、客户 merge 改挂订单、任何 PATCH/状态
                   推进都不重算（两字段非请求字段，POST/PATCH 均不接受；换套系路径不存在——
                   package_id 创建后不可改）。shoot_type_snapshot NULL = 无套系订单，渠道×类型
                   矩阵归「未归因」桶（展示口径 ITEM-4）。channel_snapshot 恒有值、取值域与
                   customers.channel 同步演进（直写 SQL 缺省归 'other' 仅兜底，生产写路径
                   恒显式提供）。存量订单 best-effort 回填：按当前客户渠道/套系类型写入，
                   可能与真实下单时不一致，仅作展示不做精确承诺）
ScheduleSlot:    start_at*, end_at*(> start_at), type*(shoot|hold|busy),
                 order_id?(type=shoot 时必填), note?
Reminder:        type*(birthday|follow_up|churn|custom), customer_id?, order_id?,
                 due_date*(date), content*, status*(pending|done|dismissed),
                 dedup_key*(unique per account, 见 4.4)
Settings:        timezone*(IANA, 默认 "Asia/Shanghai"),
                 birthday_lead_days*(默认 3), follow_up_after_days*(默认 7),
                 churn_thresholds*: [{shoot_type, days}](默认全类型 180),
                 digest_hour*(0-23, 默认 9, 按 timezone), telegram_chat_id?,
                 delivery_sla_days*(1..180, 默认 14)
                 （账号级默认交付 SLA 天数，dashboard-v2-redesign ITEM-1 增量：
                   供 Order.delivery_due_at 自动派生使用；订单级覆盖优先，
                   修改本值不重写历史订单已落库的应交付日），
                 availability*: {
                   weekly*: {"1".."7": {start*(HH:MM), end*(HH:MM)} | null},
                   min_opening_minutes*(15..480, 默认 120),
                   turnaround_minutes*(0..240, 默认 60)
                 }
```

**可约偏好语义（calendar-v2-redesign 增量）**：`weekly` 使用 ISO 星期 `"1"`（周一）到 `"7"`（周日）的七个完整必填 key，禁止额外 key；每天只有一个本地时间窗口或 `null`，`end > start`。默认周一至周五 `10:00–19:00`、周六周日 `09:00–20:00`，时区只读同一 Settings.timezone。可约空档 = 工作窗口减去未取消的 shoot/hold/busy；短于 `min_opening_minutes` 的碎片不对外报告。`turnaround_minutes` 只产生软提醒，不阻止保存、不计硬冲突。recurring 本地时间按 Temporal `compatible`（fold earlier、gap 向后平移）解释；解析后 `end <= start` 的自然日 fail-closed，不生成空档或利用率。

**订单状态语义与跃迁**：
- 语义：consulting 咨询中 ｜ scheduled 已定档 ｜ shot 已拍摄 ｜ selected 已选片 ｜ retouching 精修中 ｜ delivered 已交付（照片已给客户）｜ closed 完结（服务与收款均完成）｜ cancelled 取消（含坏账，note 写原因）
- 合法跃迁：consulting→scheduled→shot→selected→retouching→delivered→closed；**前跳边 shot→delivered、selected→delivered**（跳过选片/精修——直出底片、`retouch_count=0` 套系的单不必伪造中间态，对齐 Package 商品形态，2026-07-09 拍板）；任意非终态→cancelled；除上述前跳边外不允许跳步、一律不允许回退（回退场景走 cancelled + 新订单）。非法跃迁 → `409 invalid_status_transition`
- **补录直达**（2026-07-09 拍板）：`POST /orders` 可带 `status` 直接以八态任意值建单（历史订单补录不必逐级跃迁），规则见 §4.3；创建路径与跃迁/字段修正同守下方不变量
- **时间戳自动写入**：进入 shot 时服务端自动写 `shot_at`（请求显式提供则用请求值，事后可 PATCH 修正）；进入 delivered 同理写 `delivered_at`。提醒规则依赖这两个字段，此规则是 order-tracking 的硬验收项
- `balance_paid=false` 时进入 closed → `409 unpaid_balance`（坏账场景走 cancelled）
- **字段修正不变量**（2026-07-09 拍板，与跃迁门禁同为硬验收——状态机不变量必须在创建（补录直达）、跃迁、字段修正三条路径同样成立，不能只守进门）：
  1. closed 订单恒 `balance_paid=true`：任何使 closed 订单 `balance_paid` 变 false 的 PATCH → `409 unpaid_balance`（进门门禁推广为恒成立不变量）；
  2. `shot_at`/`delivered_at` 仅在订单已到达对应状态（含随本次请求进入）时可写；已写入后可修正、不可置空；未到达时传入 → `400 validation_failed`（保护 last_shot_at 聚合与 follow_up/churn 规则的数据基础）；
  3. 终态订单（closed/cancelled）仅可修正 `note`/`title`/`price`/时间戳（受规则 2 约束）；`deposit_paid`/`balance_paid` 在终态不可变更（cancelled 的标记留作坏账证据）→ `400 validation_failed`
- 创建 / 删除 ScheduleSlot **不**自动变更订单状态；状态只由 `PATCH /orders/{id}` 显式驱动

**客户 merge / 归档语义**：
- merge：source 必须 `status=active`；source 的 SocialIdentity / CustomerNote / Order / Reminder 全部改挂 target，source `status=merged` + `merged_into_customer_id`，不物理删除。**order / reminder 域晚于 merge 落地，其 feature 验收必须各自补"merge 迁移本域实体"用例**（契约随域生长，不静默失效）
- avatar 是 Customer 自身属性，不在 merge 迁移面：target 保留自身头像，source 默认保留原头像；merged source 禁止设置/替换头像，但 owner 可在合并后显式执行 cleanup-only DELETE 清空其头像，避免 PII 永久不可删除。该例外只允许 object→none，不恢复 merged 档案的其他编辑能力；本 feature 不做“无头像 target 自动继承 source 头像”等隐式选择。
- 归档：`PATCH /customers/{id} {status:archived}`；归档客户不参与提醒扫描、不可被 `creation_mode=new` 新业务引用（`409 customer_archived`）、默认列表隐藏（`?status` 缺省 active，可显式查 archived/all）；`creation_mode=backfill` 为保留历史真实性可引用 archived 客户，merged 永远拒绝

**转介绍指针语义**（2026-07-07 拍板，随 customer-profile-complete 落地）：
- merge 时其他客户 `referrer_customer_id` 指向 source 的，同事务批量重定向到 target；重定向后 target 的介绍人若变成自身则清空（介绍链跟人走，不指向 merged 壳）
- 归档**不**清洗既有 referrer 指针——介绍关系是历史事实，与经营状态无关；「仅 `status=active` 客户可被选为介绍人」只约束新写入（建档与 PATCH），不回溯
- `channel` 可 PATCH 修正：改为 `referral` 必须同请求携带 `referrer_customer_id`（否则 400）；从 `referral` 改为其他渠道时服务端自动清空 `referrer_customer_id`
- 编辑页若既有 referrer 后来变为 archived/merged，前端可把它作为 pinned 当前值显示并原样保留，但不得混入 active 新候选；若要清除，必须同请求把 `channel` 改为非 referral（由服务端联动清空），否则 required 校验阻止保存，不得放宽 `channel=referral` 必有 referrer 的不变量

### 4.3 各域资源 API

**方向**：webapp → 各域　**形式**：HTTP API（总约定见 4.1，实体 shape 见 4.2，此处只列端点与特有字段）

```
平台
  GET    /me                        → Account{id, created_at, timezone}
                                    timezone 为 IANA；Settings 未落地前固定 Asia/Shanghai
客户域
  POST   /customers                 {display_name, channel, referrer_customer_id?,
                                     identities:[{platform, handle, remark?}]} → 201 Customer
                                    （30 秒建档端点：display_name/channel/identities[1..N] 为硬性必填；
                                      identities 为空 → 400 validation_failed；
                                      channel=referral 时额外必填 referrer_customer_id）
  GET    /customers?q=&channel=&status=&page=      q 匹配 display_name/real_name/phone/identity.handle
                                    status 缺省 active；兼容单值并扩为逗号分隔集合
                                      active|archived|merged（或 all），服务端先按完整 status 集合+q
                                      过滤再做稳定分页，禁止前端取一页后排除 merged；重复/非法值 400
                                    列表项附聚合: orders_count(int, 非 cancelled 订单计数),
                                    last_shot_at?(date, 非 cancelled 订单 max(shot_at) 按账号时区截断)
                                    （order 域未落地前恒为 0/null，字段自始存在防契约破坏性变更；
                                      聚合同 4.4 为同进程读模型，在 repository/service 层完成，不在 handler 拼装）
  GET    /customers/{id}            → Customer + identities[] + notes[](倒序) + referrer摘要
                                    + stats{ orders_count(非 cancelled), total_order_amount(分, 非 cancelled
                                      订单 price 之和), last_shot_at?(口径同列表) }（客龄由 created_at 前端推导）
  PATCH  /customers/{id}            渐进补全任意字段；{status:archived} 即归档（语义见 4.2）
  POST   /customers/{id}/identities {platform, handle, remark?}        → 201
  DELETE /customers/{id}/identities/{identity_id}
  POST   /customers/{id}/notes      {content}                          → 201
  POST   /customers/{id}/merge      {source_customer_id}               → 200 target Customer
                                    409 merge_conflict（source 非 active）
  PUT    /customers/{id}/avatar     Header: If-Match: "{avatar_revision}"；
                                    multipart/form-data file            → 200 Customer
                                    active/archived 可设置或替换，merged → 409 customer_merged；
                                    仅接收 JPEG/PNG/WebP，原始文件 ≤5 MiB、解码后宽高各 ≤4096；
                                    服务端校正方向、去元数据并生成最长边 ≤512、编码后 ≤5 MiB 的非动画栅格图；
                                    MIME/魔数/解码/尺寸任一不符或 If-Match 缺失/格式错误
                                      → 400 validation_failed；
                                    规范化字节 checksum 已等于当前 avatar_version 且当前物理 generation
                                      通过完整字节校验 → 直接 200（结果未知重放，不改 revision）；
                                    同 checksum 但对象缺失/损坏 → 发布 fresh generation 修复；
                                    其余需要改变 pointer 且当前 avatar_revision 与 If-Match 不同
                                      → 409 avatar_revision_conflict
  GET    /customers/{id}/avatar/content?v={avatar_version}               → 200 image/* 二进制
                                    标准 Bearer 鉴权 + 账号隔离；v 缺失/格式错 → 400 validation_failed；
                                    DB 无当前头像 → 404 not_found；v 与当前 pointer 不同
                                      → 409 avatar_version_stale，绝不返回新版本字节；
                                    DB pointer 对应对象缺失或 checksum/size/media type 不符 → 500 internal
                                      并记录 integrity failure；If-None-Match 命中 → 304；成功响应固定：
                                      ETag="{avatar_version}"、Cache-Control="private, no-cache"、
                                      Vary=Authorization、X-Content-Type-Options=nosniff
  DELETE /customers/{id}/avatar    Header: If-Match: "{avatar_revision}" → 200 Customer
                                    active/archived 可正常移除；merged 仅允许 cleanup-only 移除既有头像，
                                      不因此获得 PUT 或其他档案编辑能力；
                                    header 缺失/格式错 → 400 validation_failed；当前已无头像直接 200；
                                    当前有头像且 revision 不同 → 409 avatar_revision_conflict；成功只原子清当前
                                    pointer、avatar_revision +1 并登记延迟 GC，不在 DB commit 前物理删对象
套系域
  POST/GET/PATCH/DELETE /packages…  GET ?status=active 供下单选择（上架中）
                                    下架（停售）：PATCH {status:archived}——无 in-use 校验，存量订单继续引用
                                      历史套系，仅从下单选择列表消失；恢复上架 PATCH {status:active}
                                    删除：DELETE /packages/{id} → 204；带 in-use 校验——被任一订单引用时
                                      拒绝，返回 409 package_in_use（保护 Order.package_id 引用完整性，见 §4.2 头注）；
                                      无引用才物理删除。删除是破坏性操作，非持久状态（区别于下架 archived）
                                      （in-use 校验语义随域生长：order 域未落地前无订单可引用，删除恒放行；
                                       真实 409 package_in_use 由 order-tracking 接通订单表引用检查，
                                       其 feature 验收须补"删除被引用套系 409"用例，同 orders_count 真实计数一并接通）
                                    GET 列表项附聚合: orders_count(int, 引用本套系的非 cancelled 订单计数；
                                    order 域未落地前恒为 0)
订单域
  POST   /orders                    Header: Idempotency-Key?；Body: {creation_mode?=new,
                                    customer_id, package_id?, title?, price?, status?, deposit_paid?,
                                     balance_paid?, shot_at?, delivered_at?, amount_paid?,
                                     outstanding_amount?, paid_at?, note?}
                                    → 201；creation_mode=new 缺省 status=consulting
                                    new：status 只允许 consulting/scheduled，客户与套系都只允许 active；
                                      merged/archived 客户 → 409 customer_archived；
                                      schedule-calendar 路径 A 用 new+consulting，slot 成功并刷新日历后
                                      再显式 PATCH scheduled；候选加载后套系下架仍由服务端拒绝，
                                      前端过滤不是安全边界。new+scheduled 保留为一般 API 能力
                                    backfill：status 可为八态任意值，建单即直达目标
                                      状态、不必逐级跃迁；创建与跃迁/字段修正同守 §4.2 不变量——
                                      目标状态 ≥ shot 必须显式给 shot_at、≥ delivered（含 closed）必须
                                      显式给 delivered_at（补录是历史事实，缺省 now 必错 → 400 fail loud）；
                                      status=closed 必须 balance_paid=true（否则 409 unpaid_balance）；
                                      cancelled 可直建（时间戳可选，建议 note 写原因）
                                    历史引用规则：backfill 允许 active/archived 客户与套系，merged 客户
                                      仍 → 409 customer_archived；引用仍须同账号存在，不存在/跨账号→404
                                    响应与列表项随 §4.2 Order shape 携带创建时固化的
                                      channel_snapshot/shoot_type_snapshot（非请求字段，PATCH 亦不可写）
  GET    /orders?customer_id=&status=&unpaid_balance=true&schedulable_at=&page=
                                    列表项附引用摘要: customer_display_name(string),
                                      package_name?(string, 引用套系时返回)——全局订单页可读性依赖，
                                      同"列表项附聚合"既有模式（2026-07-09 update）
                                    unpaid_balance=true 口径 = balance_paid=false 且
                                      status ∈ {shot, selected, retouching, delivered}（"已进入交付链条
                                      且未结清"；consulting/scheduled 未到收款环节、cancelled 已取消、
                                      closed 恒已结清，均不出现——比 dashboard unpaid_orders 的
                                      delivered-only 口径宽，2026-07-09 拍板）
                                    默认排序 created_at DESC（同值按 id DESC，保证分页稳定；
                                      不提供 sort 参数，待真实需求另扩）
                                    schedulable_at=<slot_end_at>：按该时刻相对服务端 now 应用与
                                      schedule POST/PATCH 相同的订单+客户未来/历史矩阵：未来客户须
                                      active，历史可 active/archived，merged 永拒；并排除已有 shoot
                                      slot；可与 customer_id 组合，服务端完整分页过滤，前端不得只取
                                      第一页自行筛选
  PATCH  /orders/{id}               {status?|deposit_paid?|balance_paid?|shot_at?|delivered_at?|
                                    amount_paid?|outstanding_amount?|paid_at?|…}
                                    非法跃迁 → 409 invalid_status_transition
                                    未结清进 closed / closed 试图取消结清标记 → 409 unpaid_balance
                                    status 等于当前状态视为幂等字段修正/no-op → 200，供结果未知重放
                                    字段修正不变量违反（时间戳预写/置空、终态改标记）→ 400（见 §4.2）
                                    金额字段显式 null → 400；balance_paid=true 时显式 outstanding≠0 →
                                    400（单向不变量）；标记收讫 {balance_paid:true} 自动落 paid_at=now
                                    并按推定补齐金额（支付事实语义见 §4.2，dashboard-v2-redesign
                                    ITEM-2）
  DELETE /orders/{id}               仅终态（closed/cancelled）可物理删除 → 204（2026-07-09 拍板）
                                    非终态 → 409 order_not_terminal（进行中订单先 cancel）
                                    删除即从实时聚合/统计消失（删 closed 单会减少 orders_count 与
                                      营收类统计，UI 删除确认须明示）；破坏性操作，与套系 DELETE 同级
                                    引用拦截随域生长：被 type=shoot 的 slot 引用 → 409 order_in_use
                                      （schedule-calendar 落地时接通，落地前无 slot 可引用、只受终态
                                      门禁约束），error.details 必返 schedule_slot_id/schedule_start_at
                                      供订单页直达关联日期；reminder 引用不拦截删除——引用已删订单的 pending
                                      提醒由 reminder-engine 定义自动 dismiss/跳过（其 design 细化）
档期域
  GET    /schedule/slots?from=&to=  区间查询（半开区间 [from,to)，含跨界 slot）；月历传完整
                                    6 周可见网格的账号本地日界再转 UTC，不只查自然月
                                    列表项为以 type 判别的 ScheduleSlotListItem union：shoot variant
                                      必返 order_id/customer_id/customer_display_name/customer_status/
                                      order_status/order_deposit_paid/order_balance_paid，
                                      order_title/order_price/package_name/package_shoot_type 因源字段
                                      可空而可选；
                                      hold/busy variant 无引用摘要（repository batch 组装，禁 N+1）
  POST   /schedule/slots            Header: Idempotency-Key?；Body:
                                    {start_at, end_at, type, order_id?, note?}
                                    → 201 { slot: ScheduleSlot, overlaps: [slot_id] }
                                    重叠不阻止；响应 overlaps 是事务内快照，不承诺捕获并发另一事务。
                                    单次成功后前端立即重拉；多个在途写全部完成后统一重拉，并在窗口
                                    重新聚焦时 revalidate，按刷新结果给当前冲突提示；同 key
                                    重放返回首次成功结果。跨日合法，前端把 slot 显示在每个相交本地自然日；
                                    「全天」是账号本地 [00:00, 次日00:00) 的输入快捷方式
                                    type=shoot 引用规则：
                                    - end_at > 请求时刻（未来/进行中）只允许 consulting/scheduled；
                                    - end_at <= 请求时刻（历史补录）允许
                                      scheduled/shot/selected/retouching/delivered/closed；
                                    - 未来客户须 active，历史可 active/archived，merged 永不允许；
                                      归档先提交的未来写 → 409 customer_archived；shoot 先提交则
                                      后续归档合法，列表保留 slot 并返回 archived 警示；
                                    - cancelled 永不允许；同一订单最多一条 shoot slot，冲突 →
                                      409 order_already_scheduled，error.details 必返现有 slot id/start_at
                                    create/改挂 shoot 统一先读 order.customer_id，再按 customer→order
                                      加行锁并复核；merge 改挂则回滚并以新 id 自动重试一次，再次
                                      变化 → 409 customer_changed 并由前端重拉候选。归档/merge
                                      先提交时写入重验并拒绝或改挂，shoot 先提交时后续客户状态变化
                                      合法且列表显示最新状态；任一顺序不得死锁或产生悬空引用
  PATCH  /schedule/slots/{id}       start_at?/end_at?/type?/order_id?/note?；order_id 与 note
                                    支持显式 null 清空，省略表示不改；时间/type/order 变化重跑矩阵
                                      与唯一性校验，note-only 不因客户/订单后来变态而拒绝
  DELETE /schedule/slots/{id}       → 204；只删档期，不改变或删除订单，UI 必须明示
提醒域
  GET    /reminders?status=pending&customer_id=&due_before=&page=
                                    默认稳定排序 due_date ASC, id ASC
  POST   /reminders                 {type:custom, customer_id?, due_date, content} → 201
  POST   /reminders/{id}/done | /dismiss
  POST   /admin/reminders/scan      {date?("YYYY-MM-DD", 缺省=账号时区今日)}
                                    → { created: int, skipped: int, auto_dismissed: int }
                                    （鉴权同 4.1；每日定时任务与手动触发唯一共用入口）
设置
  GET/PATCH /settings               （shape 见 4.2 Settings）；GET 对无存储行返回含完整
                                    availability 的服务端有效默认值。PATCH 的 availability 是整体
                                    替换；weekly 缺键、额外键、坏 HH:MM、end<=start 或阈值越界
                                    → 400 validation_failed 且旧值不变。Settings handler 对该嵌套
                                    对象使用局部 strict decode/原始 JSON 校验，不依赖普通 binding
                                    静默丢弃未知键；损坏的持久化 JSON fail-closed → 500，不回落默认。
  POST   /settings/telegram/bind-token → { token, deep_link }   （绑定流程见 4.5）
dashboard
  GET    /dashboard →
         { due_reminders: Reminder[](due_date ≤ 账号时区今日+2 天, pending——近 3 天窗口含逾期，
                          含全部 type（含 churn）；2026-07-06 原型比对拍板),
           today_slots: ScheduleSlotListItem[](与本地今日半开日界相交；shoot 含订单+客户摘要，
                          摘要语义与 GET /schedule/slots 一致),
           unpaid_orders: { count, items: OrderListItem[](status=delivered 且 balance_paid=false；
                            closed 必已结清故不出现；count == len(items)；窄于 orders?unpaid_balance) },
           churn_alerts: Reminder[](type=churn, pending；无日期窗口，可与 due_reminders 重叠),
           recent_stats: { orders_created(按 created_at, 含全部状态),
                           orders_delivered(按 delivered_at, 排除当前 status=cancelled),
                           revenue_confirmed(分, delivered_at 落窗口内且 balance_paid=true
                           且当前非 cancelled 的订单 price 之和；price IS NULL 按 0) } }
         （recent_stats 窗口 = 账号时区自然日 [今日-29, 今日] 含今日共 30 天；替代原"本月"口径，
           cancelled 归属与 total_order_amount 对齐，2026-07-06 原型比对拍板；
           ListItem 形由 dashboard feature 2026-07-14 钉死，与 OpenAPI 一致）
```


**Interface 设计检查**：dashboard 聚合做在服务端（一次请求 vs 前端拼五个列表）——Design-It-Twice 比较过"前端自行组合"（省一个端点但移动端五连击、口径散落前端）与"服务端聚合"（口径单点、移动友好），选后者；depth：聚合口径（何为"待收尾款"）藏在服务端一处。

### 4.3a 客户头像存储与交付协议

**方向**：customer 应用编排 → storage adapter　**形式**：injected port；首版本地文件，后续可替换私有 OSS

```
AvatarObjectStore
  PutImmutable(ctx, object_key, replayable_body, expected_meta) → PutResult{meta, created}
  Open(ctx, object_key)                             → ObjectStream{body, meta}
  Stat(ctx, object_key)                             → ObjectMeta
  List(ctx, prefix, cursor, limit)                  → ObjectPage{items, next_cursor?, done}
  Delete(ctx, object_key)                           → error

ObjectMeta = { media_type, size, checksum, modified_at }
PutResult  = { meta:ObjectMeta, created:bool }
ObjectItem = { key, meta:ObjectMeta }
ObjectRef  = { avatar_version, avatar_object_id }
replayable_body = 最终规范化的只读精确字节（≤5 MiB）；caller 持有，adapter 不保留/不关闭
avatar_version   = "sha256-" + sha256(最终规范化字节)的 64 位小写十六进制
avatar_object_id = 服务端 crypto-random 128-bit 的 32 位小写十六进制；一次发布生成一次，离开 current 后永不复用
object_key = avatars/{account_id}/customers/{customer_id}/{avatar_version}/{avatar_object_id}
```

**不变量**：

- `avatar_version` 与 `ObjectMeta.checksum` 是最终规范化编码后精确字节的 SHA-256；它是 API/ETag 内容版本。物理对象还带不可复用 `avatar_object_id`：同内容再次发布可有相同 version，但必须使用新 object_id/key；对象一旦成功写入就不可覆盖。`PutImmutable.created=true` 表示本次首次发布，已存在且完整性相同则返回 `created=false`；只有同一请求在结果未知后的内部重试可接受 false，fresh ID 首次调用得到 false 必按随机碰撞处理并换新 ID。
- `avatar_revision` 是 Customer 行上的独立写 revision，数据库保存非负 bigint，API 编码为 `ar-{decimal}`。它初始为 0，每次 current pointer 从 none→object、object→object 或 object→none 成功提交时原子 +1；desired=current / already-none no-op 不变。PUT/DELETE 的 `If-Match` 只比较 revision，内容 URL 与 ETag 仍只使用 checksum version，从而同时避免 ABA 与 provider identity 泄漏。
- 图片规范化不得写入时间戳、随机数等非确定 metadata；同一构建对同一输入必须生成相同精确字节与 checksum，保证 storage timeout / commit unknown 的原请求重放可收敛。
- `object_key` 只由应用从已鉴权账号、已查得客户与 ObjectRef(avatar_version+avatar_object_id) 派生，请求不能传；adapter 必须拒绝路径逃逸。数据库与 API 不出现本地绝对路径、bucket、endpoint、SDK 类型或临时签名参数。
- PostgreSQL 是 Customer 与 current pointer 的 system of record；local/OSS 是 binary durable adjunct。DB 保存 `avatar_version/avatar_object_id/avatar_media_type/avatar_size/avatar_updated_at`，当前物理 key 可纯派生；API 不暴露 object_id。
- port 错误至少归类 `ErrObjectNotFound`、`ErrInvalidObjectKey`、`ErrIntegrityMismatch`、`ErrTemporary` 与其余 internal I/O error。规范化输出上限 5 MiB，`PutImmutable` 的 replayable_body 必须能在同一应用操作内以完全相同字节重复读取，caller 保持所有权，adapter 不保留也不负责 close。`Delete` 对不存在对象归一为成功，但成功返回的后置条件必须是同一 adapter 随后的 `Stat(key)` 已稳定为 not-found，不能在 provider 仍可能异步完成删除时提前确认；无法建立该 barrier 时返回 Temporary。`ObjectStream.body` 必须可关闭，每个直接调用 `Open` 的 application caller（鉴权 READ、same-content 完整性检查、current-pointer audit）都必须在成功、错误、取消路径 close；HTTP handler 不直接调用 store。
- `GET /avatar/content` 的 customer application read service 始终先用 AccountScope 读取 DB 当前 pointer，再严格校验 v，随后 `Stat/Open`。由于规范化头像上限仍为 5 MiB，该 service 必须完整读取（超上限即失败）、重新识别 media type、计算实际 size/checksum 并与 DB pointer/ObjectMeta 核对，成功后才向 HTTP 层返回 ≤5 MiB 的 validated bytes + media metadata。HTTP handler 只做鉴权上下文、query/header 与 binary/JSON 响应适配：在 service 验证成功前不得发送任何 200/304 header；If-None-Match 命中也只能基于已验证结果返回 304，不得跳过字节完整性校验。旧版本 URL 永不返回新字节。首轮迁移 OSS 后仍走此鉴权端点，所以业务层、前端和 HTTP 契约不变；预签名 URL 是后续媒体交付优化，不是替换 adapter 的前提。
- `List` 的 reconciliation 扫描域固定为账号级 prefix `avatars/{account_id}/customers/`，在该 prefix 内按完整 key 升序稳定返回 `ObjectItem`；cursor 是该账号排序域内由 adapter 生成的 opaque exclusive cursor，limit 必须受配置上限约束。对扫描期间不变的对象集不得重复/跳过；并发插入到已越过 key 区间的对象允许留到下一完整 cycle，但不能永久饥饿。

**PUT 状态机**：

1. 先校验 If-Match 存在且是合法 quoted `avatar_revision`，并用 AccountScope 非锁定读取确认 Customer 属于当前账号；不存在/跨账号先返回 404，不能为任意 URL id 写 orphan。
2. 在客户锁外限流、解码、方向校正、规范化并计算 checksum。
3. 若非锁定快照显示 desired checksum 已是 current，先开短事务锁 Customer，按账号重验存在性与状态；merged 必须先返回 `409 customer_merged`，不得借 no-op 快路径绕过写状态矩阵。active/archived 再重验 checksum，并在 5 MiB 上限内 `Stat/Open` 完整读取当前 ObjectRef，重识别 media type、size、checksum。实际 generation 完整时不写对象、直接 200（commit unknown 重放快路径，允许旧 revision 因为没有状态变化）；对象缺失/损坏时不得返回 200，If-Match revision 失配先返回 409，匹配则退出短事务并进入 fresh generation 修复流程。
4. 其余情况生成新的、永不复用的 avatar_object_id，以完整 generation key 调 `PutImmutable`；单次请求内 storage timeout 可用同 object_id 重试，跨 HTTP 重试可生成新 id，旧 generation 由 reconciliation 回收。
5. 唯一 transaction owner 为 `AccountScope.WithTxScope`；callback 用 TxAccountScope 锁 Customer，再锁**新 object_id** 对应 GC row。只要该 row 已存在，就把 generation 视为已进入回收生命周期：不得取消、不得晋升 current，回滚并用**全新 object_id** bounded 重做 publish。无 GC row 才做 final Stat/完整性核对；对象缺失也回滚并换全新 object_id。物理 object_id 绝不复用，目标 key 已存在但不是本次内部重试时按碰撞处理并换新 ID。
6. 锁内若并发请求已把相同 desired checksum 设为 current，必须先对该 current ObjectRef 做与第 3 步相同的完整字节校验；有效时把本次未引用 generation 入 pending GC 并返回 200。若 current 缺失/损坏，则本次新 generation 只能在 If-Match revision 仍匹配时用于修复；其余需要改变 pointer 的路径都校验 If-Match revision，失配 generation 由 reconciliation 入队并返回 `409 avatar_revision_conflict`。
7. 原子切换 current pointer 的 version+object_id+metadata，并令 avatar_revision +1；旧 ObjectRef 同事务首次入 pending GC。DB 失败最多留下可发现 orphan，旧 current 与旧 revision 完好。

**DELETE 状态机**：

1. 校验 quoted avatar_revision 格式后开事务，按账号锁定 Customer，并重验状态、current pointer 与 revision。
2. active/archived 执行正常移除；merged 只开放本 DELETE 作为 cleanup-only PII 清理例外，PUT 与其他档案 mutator 仍保持拒绝。当前已无头像直接 200（no-op，不改 revision）。
3. 当前存在且 If-Match revision 失配返回 `409 avatar_revision_conflict`；匹配时同一事务清空 current pointer、avatar_revision +1 并登记旧 ObjectRef 的 `avatar_object_gc`，commit 后返回 200。禁止在 commit 前物理删除当前对象。

**GC / reconciliation**：

- `avatar_object_gc` 是 account-scoped pending 队列，对 `(account_id, customer_id, avatar_object_id)` 唯一，并保存 avatar_version、not_before、next_attempt_at、attempts、last_error；初始 grace period 为 24 小时。
- pointer 替换/清空首次入队取 `not_before=now+grace`；reconciliation 首次发现 orphan 也取 `now+grace`。重复 enqueue 使用 `ON CONFLICT DO NOTHING`（或等价“保留已有最早 not_before 与全部 retry metadata”），不得每小时把 due time 向后推，也不得缩短既有 grace；失败退避只更新 `next_attempt_at/attempts/last_error`。
- GC worker 对每个对象使用独立事务，锁 Customer row + GC row 后复核 current pointer：若 `avatar_object_id` 等于待删 generation，说明 PUT/reconciliation 竞态留下了 stale queue row，只删该 row 并 commit，绝不调用 storage Delete；不等时才对精确 generation key 调有超时的幂等 Delete，成功删 row并 commit。Delete 失败先 rollback 主事务，再以同一 GC row identity 在独立短事务条件更新 `next_attempt_at/attempts/last_error`，row 已不存在时不得重建；commit unknown 则保留 row 并由下轮幂等重试。即使 DB/session 丢失后旧 Delete 迟到，它也只能删除不可复用的旧 object_id；同 checksum 新 current 使用另一 key，存储侧代际保证安全。
- reconciliation 只从 platform 提供的可信 server-side `AccountScopeEnumerator` 获取账号并构造与 HTTP 相同 fail-loud 的 AccountScope；客户端永不传 account_id。`avatar_reconciliation_checkpoint` 对每账号分别保存 object inventory 的 opaque exclusive cursor/cycle 与 current-pointer audit 的 customer-id exclusive cursor/cycle。object 方向按账号级 key prefix 扫一页，把未被五字段 pointer 引用的 generation 入 pending；pending insert 与 object cursor advance 同一 DB 事务，崩溃后可重放。pointer 方向按 Customer id 稳定分页一页，对每个非空 ObjectRef 执行与 GET 相同的 ≤5 MiB 完整 Stat/Open/media/size/checksum 校验；缺失/损坏是已完成的 integrity finding，结构化记录后允许 pointer cursor 前进，但不得清 pointer或入 GC；单项 Temporary/internal I/O 做本 tick 有限重试，仍失败则结构化记录 audit-temporary 并推进 cursor，下一完整 cycle 再访，不能让一个 key 永久饿死后页。两个方向到 done 才各自清 cursor开下一 cycle。reconciliation 只入队或报告，**不得直接物理删除或静默改成“无头像”**。
- composition root 挂载唯一 `AvatarMaintenanceRunner`：依赖迁移、storage readiness 与 mount attestation 成功后启动；立即跑一轮，之后每小时 single-flight tick。首版固定每账号每 tick 最多一页 object inventory + 一页 current-pointer audit + 100 条 due GC，重叠 tick 合并/跳过；clock/ticker 可注入。现有 server 使用 `context.Background()` 且未做 graceful shutdown，本 feature 必须引入 OS signal-aware root context：signal 后停止新 tick、取消在途 storage I/O、有界等待 runner，并完成 HTTP graceful shutdown 后退出，不能把该生产装配继续推迟到 v1-hardening。GC row 的 Delete 失败按 `next_attempt_at` 退避重试；reconciliation 单项失败按上一条 cursor/cycle 语义记录并推进或留待下一完整 cycle 再访，不为 audit 另造退避 row；后台暂时失败不关闭 HTTP 服务。多实例重复 runner 由 Customer/GC row 锁、队列唯一约束、幂等 Delete 与不可复用 generation key 保证正确性。
- local adapter 在最终 generation 同父目录创建自有 temp：写 content 与 metadata、分别 file fsync、temp directory fsync、原子 rename 到完整 avatar_version/avatar_object_id key、再 parent directory fsync；只清理自有命名且超过 1 小时的 stale temp，不碰最终对象或无关文件。

**部署、备份与未来 OSS 迁移**：

- production adapter = 配置根目录下的 local filesystem；test adapter = in-memory fake；未来 OSS adapter 实现同一不可变 key、metadata、List 与幂等 Delete 语义。配置新增 `AVATAR_LOCAL_REQUIRE_MOUNT`：二进制直跑缺省 false；容器/production compose 固定 true。为 true 时，应用在可写性检查前解析真实路径并通过 Linux mount table 验证 `AVATAR_LOCAL_ROOT` 是独立 mountpoint；缺 mount、平台不支持验证或身份不符均启动失败。镜像可预建目录，但不能仅靠“目录存在且可写”声称卷已挂载。
- 本地一致备份须：freeze 所有头像 mutator（API PUT/DELETE、GC/reconciliation；首版优先直接停 app）→ `pg_dump` → 归档头像 volume → 生成 exact-generation manifest。manifest 对每个 current pointer 至少记录 account_id、customer_id、avatar_version、avatar_object_id、derived object key、media_type、size、实际 SHA-256，并另列全部物理 generation 的 key/count/checksum 汇总；恢复须同时恢复 DB+volume，逐 current 精确 key 与实际字节核验通过后才重新开放写入。只比 object count/checksum 不足以区分同 checksum 的多个 object_id。头像目录与备份按 PII 处理，使用最小文件权限、访问控制与加密。cleanup-only DELETE 只清在线 pointer并按 24h+runner 节奏回收活动存储，不会追溯擦除已经生成的历史备份；恢复旧备份可能重新带回该 PII，备份 retention/销毁策略须在运维说明中明确，UI/文档不得宣称“从所有备份彻底删除”。
- 首轮 local→OSS 迁移另起 feature，倾向短时 write freeze：freeze → copy → exact key/object_id/count/checksum 核对 → 切 adapter → smoke → unfreeze。若 unfreeze 后回滚，必须把 OSS 期间新增写入反向同步回 local；“保留本地副本”本身不是完整 rollback。也可在届时另选 dual-write 或增量补拷，但不得只改 driver 配置。

**Interface 设计检查**：port 由 customer 侧拥有，caller 只知道 immutable ObjectRef 语义；avatar_revision 是 provider-neutral 写 CAS，checksum 是公开内容版本，object_id 是内部物理 generation，三者分工关闭 ABA/stale Delete 且不泄漏 provider generation/versionId。pointer/条件写/生命周期由 customer application 编排，本地/OSS I/O 集中在 platform adapter；删除 seam 会让路径、完整性、物理代次隔离与 OSS 调用散回 service/handler。local + in-memory 为 local-substitutable，未来 OSS 为 true external；Stat/List/runner 都有当前用途。

### 4.4 提醒引擎规则契约

**方向**：reminder 域 ← customer / order 域数据（同进程读模型，in-process 依赖）　**形式**：内部规则 + Reminder 资源

```
扫描:  每日一次（digest_hour 前完成，按账号时区）；唯一手动触发入口 = POST /admin/reminders/scan（4.3）
       全量扫描必须幂等
规则:
  birthday   触发: customer.birthday 距今 ≤ birthday_lead_days 且未过（按账号时区判日界）
             dedup_key = "birthday:{customer_id}:{生日发生日年份}"
  follow_up  触发: order.status=delivered 且 delivered_at + follow_up_after_days ≤ 今日
             dedup_key = "follow_up:{order_id}"
  churn      触发: 客户至少有一单 delivered|closed、当前无非终态订单，且最近一单 shot_at
                   距今 > churn_thresholds[该客户最近套系 shoot_type，缺省 other]
                   （零成交客户不生成 churn——线索跟进规则记二期，防提醒噪音）
             dedup_key = "churn:{customer_id}:{最近一单 order_id}"
             （客户再下单后旧 churn 提醒自动 dismissed）
  custom     手工创建；dedup_key = "custom:{reminder_id}"
```

**约束**：dedup_key 唯一冲突 = 静默跳过（不报错不重复）；重复扫描零新增是 reminder-engine 的硬验收；status=merged/archived 客户不参与扫描；follow_up/churn 扫描遇 `delivered_at`/`shot_at` 缺失（历史数据）→ 跳过该条并记日志，不报错。

**Interface 设计检查**：规则参数与幂等全部藏在 reminder 域内；seam = Reminder 资源（dashboard 与 TG 摘要都只读 Reminder，不各自实现规则）；测试面 = "构造客户/订单数据 → 调 /admin/reminders/scan → 断言 Reminder 集合与计数"。

### 4.5 Telegram 推送协议

**方向**：reminder 域 → Telegram Bot API　**形式**：true external，injected port

```
Port:   TelegramPort { sendMessage(chat_id, text) error }
        production adapter = Telegram Bot API (long polling 收 /start、/today)
        test adapter = in-memory fake（记录发送内容供断言）
绑定:   POST /settings/telegram/bind-token → {token(10min 有效), deep_link:"https://t.me/{bot}?start={token}"}
        用户点 deep_link → bot 收 /start {token} → 校验 → 存 chat_id 到 Settings → 回执消息
命令:   /today → 即时发送当日摘要（与每日推送同一生成函数）
摘要:   ① 今日+逾期 pending 提醒（按 due_date 升序，≤20 条，超出计数）
        （摘要窗口有意保持"今日"口径，不随 dashboard due_reminders 的近 3 天窗口——推送只推当日可行动项，2026-07-06 确认）
        ② 今日档期（时间+客户+套系）
        ③ 待收尾款订单计数
失败:   初次发送失败后再重试 3 次（间隔 1/5/30min，总 attempt=4）；仍失败只记日志——dashboard 是兜底展示（A+D 冗余设计），
        推送失败不得影响 Reminder 生成
凭证:   bot token 只经环境变量注入，不入库、不入 git（规则落 attention.md）
```

**Interface 设计检查**：真实双 adapter（production + test fake），非假 seam；invariant = 摘要生成纯函数（同一数据同一文本），推送与生成分离。

### 4.6 全量导出契约

**方向**：webapp → platform　**形式**：HTTP API

```
GET /export → application/json（Content-Disposition 附件）
{ exported_at, schema_version: 3,
  counts: { customers, social_identities, customer_notes, packages, orders,
            schedule_slots, reminders },
  customers[], social_identities[], customer_notes[], packages[], orders[],
  schedule_slots[], reminders[], settings }        // 各数组 shape 全部按 4.2
```

**约束**：全量无分页；`counts` 必须与各数组长度一致（验收核对点）；含全部 PII，导出文件的存放责任在 owner（见第 7 节拍板包）。`calendar-v2-redesign` 因 `Settings.availability` 成为 required 字段把导出 `schema_version` 从 1 升为 2，creative-planning 系列续升为 3（本行 2026-08-23 校正为与实现一致）；dataexport 的显式列、JSON 解码与 API 投影必须返回和 `GET /settings` 相同的非默认 availability，禁止静默回落默认值。v1 的实体数组、counts 与 reference-only 头像边界不变。`dashboard-v2-redesign` ITEM-1/2/3 新增订单字段随 §4.2 Order shape 进入导出（amount_paid/channel_snapshot 恒输出，其余可缺省），`schema_version` 是否 bump 由该 epic ITEM-6 统一决策（此前保持 3）。

### 4.x 共享数据结构 / 状态

实体逻辑模型即 4.2；除 Settings 外无其他全局状态。前端无跨页共享协议约束（页面各自取数 + dashboard 聚合端点）。

## 5. 子 feature 清单

1. **platform-skeleton** — Go+React 脚手架、单账号登录、账号上下文与 repository 基座（强制账号过滤）、错误封套、健康检查、build/test/lint 命令基线、§4 契约固化为 OpenAPI + 双端 codegen
   - 所属模块：platform ｜ 依赖：无 ｜ 状态：done ｜ 对应 feature：2026-07-06-platform-skeleton
   - 备注：greenfield 安全网条目——建立后续全部 feature 的验证入口（`make check` 或等价全绿）；完成信号：登录取 token → `GET /api/v1/me` 返回账号；跨账号过滤有基座级测试；OpenAPI 文件与 §4 一致且 codegen 可跑。代码组织按 Gin + 轻量 DDD（见 §4 头注）。**外部依赖前置验证**：本条内完成 TG bot 申请 + `sendMessage` 冒烟（脚本级发一条测试消息即可），提前杀死条目 9 的外部依赖风险；token 凭证规则落 attention.md
2. **customer-core** — 30 秒建档最小闭环：POST /customers（昵称+至少 1 个社交身份+渠道必填，建档时可录入多个身份）、列表搜索（昵称/handle）、详情页
   - 所属模块：customer + webapp ｜ 依赖：platform-skeleton ｜ 状态：done ｜ 对应 feature：2026-07-07-customer-core
   - 备注：**最小闭环**；完成信号：新建到保存 ≤30 秒（计时演示，覆盖 2 个社交身份输入）；无社交身份创建返回 400；列表按 q/channel 过滤正确
3. **customer-profile-complete** — 档案补全：建档后身份增删、客户合并、归档、生日/手机/真名渐进字段、随手备注、转介绍关联
   - 所属模块：customer + webapp ｜ 依赖：customer-core ｜ 状态：done ｜ 对应 feature：2026-07-07-customer-profile-complete
   - 备注：merge / 归档语义按 4.2；design 阶段按 merge、归档、notes、referral、渐进字段分片验收（防单片过大）；完成信号：合并后 source 状态 merged 且身份/备注归并；归档客户默认列表隐藏；任意含客户的页面 ≤2 步追加备注；落地后评估 req customer-profile draft→current
4. **customer-avatar** — 客户头像：可选设置 / 替换 / 移除头像；列表、详情、转介绍下拉、merge 对话框等客户选择面显示头像缩略图，未设置时使用默认首字头像
   - 所属模块：customer + platform + webapp ｜ 依赖：customer-profile-complete, order-tracking, schedule-calendar ｜ 状态：done ｜ 对应 feature：2026-07-11-customer-avatar
   - 备注：依赖理由——头像属于档案增强，而“订单/档期客户选择面必须带头像”是本条硬范围，故待其既有 UI 落地后再做兼容增量；已 done 的 customer/order/schedule 条目状态不回退，由 customer-avatar 自己承担 caller 回归。头像不进入 30 秒建档表单。2026-07-11 owner 拍板首版使用 ECS/local persistent volume，经 `AvatarObjectStore` 隔离并预留后续 OSS adapter；2026-07-13 owner 拍板 merged 禁止 PUT 但允许 cleanup-only DELETE，并接受 24h grace、每小时、每账号每 tick 各一页 inventory/audit +100 due 的 GC 默认值。完成信号：① avatar_revision If-Match 的 PUT/DELETE、强内容版本鉴权 GET、跨账号/merged cleanup/非法文件与缓存矩阵通过；② 不可复用 object_id generation + PostgreSQL revision/五字段 pointer 在 rollback/commit unknown/ABA/并发写下不覆盖新头像；③ generation 一旦进入 GC 就不再晋升，历史与 pre-current 精确 generation Delete 在 DB/session 丢失、迟到和同 checksum 重发时仍不伤新 current，pending enqueue 不推迟 due，signal-aware MaintenanceRunner/reconciliation 可回收并报告 integrity；④ 全客户展示/选择/固定摘要统一头像或 fallback；⑤ production require-mount 能证明缺卷启动失败，volume recreate 与停 app 一致备份/恢复+exact-generation manifest 可核对；⑥ `make check` 全绿。OSS adapter、存量搬迁与预签名直读不在本条实现。
5. **package-catalog** — 套系 CRUD、上下架与删除：类型/定价方式/张数时长底片精修参数；下架后不出现在选择列表，删除受引用完整性保护
   - 所属模块：package + webapp ｜ 依赖：platform-skeleton ｜ 状态：done ｜ 对应 feature：2026-07-08-package-catalog
   - 备注：商品心智（2026-07-08 owner 拍板，§4.2/§4.3 update）——active=上架、archived=下架、DELETE=删除；完成信号：按 4.2 Package shape 建/改/下架/上架各一条通过；?status=active 过滤正确；DELETE 无引用套系 204、被引用套系 409 package_in_use（order 域未落地前无订单可引用、删除恒放行，真实 409 由 order-tracking 接通）
6. **order-tracking** — 订单记录：创建（客户+套系）、八态状态机（非法跃迁 409、时间戳自动写入、未结清禁 closed）、定金/尾款标记、按客户/全局/未收尾款查询
   - 所属模块：order + webapp ｜ 依赖：customer-core, package-catalog ｜ 状态：done ｜ 对应 feature：2026-07-08-order-tracking
   - 备注：依赖理由——订单必须挂客户并引用套系；完成信号：跃迁矩阵测试全过（含前跳边、shot_at/delivered_at 自动写入、409 unpaid_balance、字段修正不变量）、unpaid_balance 筛选正确（收窄口径）、列表附引用摘要（customer_display_name/package_name）与默认排序稳定、**merge 迁移订单用例**（4.2 契约随域生长）、引用归档客户 409、**接通 customers/packages 聚合字段真实计算**（orders_count/last_shot_at/total_order_amount，4.3，2026-07-06 契约更新）、**接通套系删除 in-use 校验**（删除被订单引用的套系返回 409 package_in_use，§4.3 契约随域生长，2026-07-08 update）；**2026-07-09 契约 update**（design PM review + owner 追加拍板，见 §8）：前跳边 / 字段修正不变量 / 列表引用摘要 / unpaid_balance 口径 / 默认排序 / POST 补录直达 / DELETE 终态物理删除
7. **schedule-calendar** — 档期：月历、跨日/全天 slot CRUD、订单关联、可行动的重叠提示、独立忙碌块与异常恢复
   - 所属模块：platform + order + schedule + webapp ｜ 依赖：order-tracking ｜ 状态：done ｜ 对应 feature：2026-07-09-schedule-calendar
   - 备注：依赖理由——type=shoot 的 slot 必须挂订单。完成信号：①日历页与客户档案页共用「新建拍摄档期」流程，客户档案入口预选当前客户；②桌面端从客户档案到建单+挂档 ≤30 秒，月历查指定日期安排与重叠 ≤10 秒；③月历周一首列并查询固定 6 周网格，跨日/全天 slot 在每个相交本地自然日可见，密集日以 display_start/id 稳定排序且冲突数按当日参与重叠的唯一 slot 计；④保存前展示具体重叠对象但允许继续，成功后重拉给当前冲突提示，多在途写全部完成后统一重拉；⑤组合流程使用 128-bit flow_id 的 per-step attempt Idempotency-Key 和 session flow journal，服务端 operation 固定为 typed `order.create.v1`/`schedule-slot.create.v1`，重复点击/响应丢失/5xx/硬刷新不产生重复订单或 slot，24 小时过期后禁自动重放；历史无候选跳转 backfill 时，schedule_draft 只允许历史可排期六态，默认 shot 并按账号时区预填档期开始日，POST order 前升级为同一 pending journal 的 `backfill_order` phase，结果未知不得重复补录；只有 unknown 结果持续阻塞且不可清理，明确失败或资源已知时可保留现状/补偿后结束；⑥新建订单先落 consulting，所有 consulting 订单都在 slot 成功并刷新日历后再显式推进 scheduled，刷新返回的 ScheduleSlotListItem.customer_id 覆盖 journal 旧值后才进入状态同步，状态同步 unknown 保留恢复入口，明确失败可重试、删除 slot 或保留异常现状并结束，不产生无档期的已定档订单；⑦shoot 写与归档/merge 共用 customer→order 锁序，按先提交者线性化且无死锁/悬空引用；连续 merge 返回 customer_changed 时保留表单与已知订单、清旧候选后重拉确认；ScheduleSlotListItem 为 type 判别 union，shoot 必返订单/客户/状态摘要；⑧建/删 slot 不自动改订单状态，删 slot 明示订单保留并可直达客户档案订单 tab；⑨接通被 shoot slot 引用订单 DELETE → 409 order_in_use，并用 typed details 直达关联档期；⑩creation_mode 与 schedulable_at 服务端复用订单+客户时间矩阵，未来要求 active 客户、历史允许 active/archived、merged 永拒，缺 header、缺候选过滤的既有订单调用/排序/total 不变；⑪GET /me timezone 驱动日界，含 schedule_draft OrderWorkspace 历史时间、非默认 DST 用例，加载失败禁用写入而不回退浏览器时区；⑫首版仅月视图，移动端只验收查档期轻路径，不做周视图/拖拽/重复档期。组合流程仍是前端显式调用订单与档期两个独立端点，不新增聚合端点。
8. **reminder-engine** — 提醒引擎：/admin/reminders/scan 幂等生成三类提醒、done/dismiss、参数可配置（含按拍摄类型流失阈值、账号时区）
   - 所属模块：reminder + webapp ｜ 依赖：customer-profile-complete, order-tracking ｜ 状态：done ｜ 对应 feature：2026-07-12-reminder-engine
   - 备注：依赖理由——生日规则要 birthday 字段（条目 3），回访/流失规则要订单状态时间戳（条目 6）。Settings 独立归 `backend/internal/settings` 域包，提供有效默认值、提醒参数与账号时区，供 reminder、`GET /me` 和后续 telegram-digest 消费。完成证据：同日双跑零新增；三规则正/反/边界、时区日界、时间戳缺失跳过、零成交不告警、改阈值生效、merge 迁移、已删订单 auto-dismiss、customer_id 过滤、runner 每本地日一次与前端三路径均已通过 review/QA。
9. **telegram-digest** — TG Bot：bind-token 绑定流程、每日摘要推送、/today 命令、失败重试与日志
   - 所属模块：reminder（TelegramPort）｜ 依赖：reminder-engine, schedule-calendar ｜ 状态：done ｜ 对应 feature：2026-07-15-telegram-digest
   - 备注：依赖理由——摘要内容 = 提醒（条目 8）+ 当日档期（条目 7）；bot token 已在条目 1 冒烟验证；完成信号：owner 真机绑定并收到基于 synthetic fixture 的真实端到端摘要（截图裁剪/脱敏），未绑定时系统全功能正常。2026-07-17 done：owner 确认真机 binding/daily//today 通过（S19 owner-attested，无截图归档）；code review round 2 passed、QA passed、CMD-001~005 全绿；入站失败安全提示（REV-002）延后为单独设计决策。
10. **dashboard** — 首页面板：待办提醒（近 3 天窗口）、今日档期、待收尾款、流失预警、近 30 天概览（4.3 dashboard 契约）
   - 所属模块：webapp + dashboard ｜ 依赖：order-tracking, schedule-calendar, reminder-engine ｜ 状态：done ｜ 对应 feature：2026-07-14-dashboard
   - 备注：完成信号：五卡片数据与各域列表页交叉一致（核对用例）；登录后默认落地页。§4.3 ListItem 形与 OpenAPI 已由本 feature 钉死；台上写复用 reminders done/dismiss 与 PATCH order balance_paid。
11. **data-export** — 全量 JSON 导出（4.6 契约）：一键导出全部实体 + counts 核对
    - 所属模块：platform ｜ 依赖：customer-profile-complete, customer-avatar, package-catalog, order-tracking, schedule-calendar, reminder-engine ｜ 状态：done ｜ 对应 feature：2026-07-21-data-export
    - 备注：依赖理由——导出范围 = 4.2 全部实体，各域落地后才有内容可导。2026-07-21 owner 已选择 §4.6 reference-only JSON：在同一账号一致快照中导出七类实体与有效 Settings；counts 必须逐项等于最终数组长度；内部字段、凭证、头像二进制/object_id/object key/GC/reconciliation 均排除；设置页必须常驻 PII 保管与头像只含当前部署引用、不可便携恢复说明。exact-generation manifest 仅属于媒体便携包分支，本 feature 不交付且完成核对为 N/A。
12. **v1-hardening** — 首版收口：空态/错误态/加载态清扫、移动轻路径（查档期/搜客户/记备注）、回归清单、README 使用说明
    - 所属模块：跨模块 ｜ 依赖：customer-avatar, telegram-digest, dashboard, data-export ｜ 状态：in-progress ｜ 对应 feature：2026-07-22-v1-hardening
    - 备注：完成信号：375px 宽度下三条轻路径可完成；回归清单逐条打勾归档；README 覆盖部署/备份/凭证操作
13. **calendar-v2-redesign** — 档期工作台增量：月/周双视图、真实可约空档、转场提醒、订单款项/套系摘要和移动端完整 CRUD
    - 所属模块：platform + reminder/settings + schedule + webapp ｜ 依赖：schedule-calendar, reminder-engine, dashboard, data-export ｜ 状态：done ｜ 对应 feature：2026-07-31-calendar-v2-redesign
    - 备注：本条是新增量，不回退或改写已 done 的 `schedule-calendar` / `data-export` 历史。完成信号：Settings availability 迁移、严格 PATCH、有效默认与 schema-v2 导出 parity 通过；shoot slot 在 schedule repository 内批量装配价格/收款/套系拍摄类型且 dashboard 回归无 N+1；月/周视图、未来 14 天空档（最多展示 8 天、复制前 5 天）、shoot+hold 利用率、转场软提醒、取消降级和桌面/移动 CRUD 可用；周日默认 09:00–20:00、单日单窗口、Temporal compatible DST 语义有确定性测试；复用 ScheduleSlotDialog 的 journal/幂等/unknown recovery，并以 conflict preview generation 保证确认范围与保存范围一致；1600/1280/375 记录实际 workspace 宽度、layout mode、DOM/focus 与 overflow 证据；不新增 openings/overview/book、拖拽、重复规则、多窗口、自助预约或主动消息发送。

**最小闭环**：第 2 条 `customer-core` 做完后，登录 → 30 秒建一个带渠道、1 个或多个社交身份的客户 → 列表搜到、详情看到——端到端最窄路径可演示。

### Goal Coverage Matrix

| Goal / completion signal | Covered by | Verification entry | Evidence type | Core? |
|---|---|---|---|---|
| 客户集中建档、30 秒录入、多平台归一（req customer-profile） | 2, 3, 4 | 多身份建档计时演示 + merge/归档测试 + 头像条件写/并发/恢复 API 证据 + 全选择面截图 | test + API + screenshot | yes |
| 渠道归因：每个客户带来源渠道可筛选 | 2 | GET /customers?channel= 用例 | test | yes |
| 再也不忘：三类提醒准确且不重复，主动送达 | 8, 9, 10 | 幂等双跑测试 + 时区日界用例 + TG 真机截图 + dashboard | test + screenshot | yes |
| 档期一眼可答、30 秒可靠排期、与客户套系关联 | 6, 7, 13 | 月历/周历/空档文案与客户档案两入口演示 + 跨日/全天 + overlaps/转场明细 + 幂等重放/结果未知恢复用例 | test + API + screenshot | yes |
| 订单状态与定金尾款不漏 | 6, 10 | 跃迁矩阵测试（含时间戳/unpaid_balance）+ 筛选核对 | test | yes |
| 套系参数有结构化的家 | 5 | CRUD + 上下架过滤 + 删除引用完整性用例 | test | yes |
| 可持续基线：账号隔离 + 全绿验证命令 + 数据可带走 | 1, 11, 12, 13 | make check + 基座过滤测试 + schema-v2 Settings 导出 parity；reference-only 核对 JSON counts/边界 | command + test + decision | yes |
| 首版整体完成信号 | 全部 | 一条链路演示：建档→套系→订单→档期→标定金→次日 TG 摘要→dashboard 五卡有数 | acceptance report | yes |

## 6. 排期思路与深度规划底稿

**为什么这么拆**：先基座（greenfield 必须先有验证入口和 ADR-001 执行点，并前置杀死 TG 外部依赖风险），再沿"先治忘"价值主线（客户 → 档案完整）铺数据地基（套系 → 订单 → 档期），让提醒引擎在真实数据上运转（引擎 → TG → dashboard），导出与收口断后。1-2 之后，3 与 5 可并行；头像不阻塞订单/档期业务能力，待 6/7 的真实客户选择面落地后由条目 4 一次承担 customer/order/schedule UI 兼容增量。

**目标完成信号**（roadmap 级）：上表末行的全链路演示在 owner 真机跑通 + 全部 items done/dropped。"owner 真实使用两周不弃用"是软信号，记观察项由 owner 主观判定，不作为 completed 门槛。

**Top 3 风险与缓解**：
1. **录入成本超 30 秒 → 工具弃用**（产品级最大风险）——缓解：契约 4.3 把"三项必填建档端点"写死；条目 2 验收含计时演示；条目 12 移动轻路径专项。
2. **提醒重复 / 漏发 / 跨日错位 → "治忘"卖点直接失信**——缓解：4.4 幂等键 + 4.1 时区单一口径写进契约；条目 8 硬验收"双跑零新增 + 时区日界用例"；TG 失败不影响生成，dashboard 兜底（A+D 冗余）。
3. **greenfield 无基线 → 后续 feature 无法可信验证**——缓解：条目 1 是安全网条目，交付全绿命令基线 + 账号过滤基座测试 + TG 冒烟，后续每条 feature 的 DoD 挂在这套命令上。

**非显然依赖**：TG bot token 需 owner 向 BotFather 申请（条目 1 前置冒烟，凭证走环境变量，规则落 attention.md）；**条目 1 启动前拍板包**（见第 7 节）：技术栈确认、存储引擎、部署形态与 PII 边界；生日年份可缺（"MM-DD"）导致年龄不可算——契约已按可缺设计。customer-avatar 增加 PostgreSQL 之外的持久化卷：一致备份必须在头像写 freeze/停 app 下同时取得 `pg_dump`、头像 volume archive 与逐 current ObjectRef 的 exact-generation manifest，恢复后先按精确 key/实际字节核验再开放写；未来切 OSS 是单独迁移，不得直接切配置丢失本地对象。

**关键假设**（review 时可精确反驳）：① 技术栈已拍板并落 ADR：Go + React + PostgreSQL（2026-07-05，owner；ADR-002/003）；② owner 的 TG 可正常收 bot 消息（条目 1 冒烟即证实/证伪）；③ 默认参数（生日前 3 天、拍后 7 天、流失 180 天、摘要 9 点、时区 Asia/Shanghai）作为初始值合理，均可配置。

**基线与验证入口**：条目 1 交付 `make check`（或等价：build + test + lint 一键）作为全 roadmap 验证入口；UI 类条目另加浏览器手工路径（截图证据）；TG 类条目加真机截图。

**交付物落点**：每条 feature 落在代码 + 测试 + items.yaml 状态回写 + acceptance 报告；条目 3 完成时评估 req customer-profile draft→current；条目 12 落 README 与回归清单文档。

**知识回写点**（acceptance 时触发对应沉淀）：验证命令与本地起服务方式 → attention.md（cs-note）；技术栈与存储引擎落地 → ADR（cs-domain）；TG bot 申请与绑定坑 → compound（cs-keep）；渠道/线索术语 → CONTEXT.md（cs-domain）。

## 7. 观察项

- **条目 1 启动前拍板包**：
  1. ✅ 技术栈：Go + React（2026-07-05 owner 确认）
  2. ✅ 存储引擎：PostgreSQL（2026-07-05 owner 拍板，理由见第 4 节头注；已落 ADR-002；大数据类需求二期按需引入专用存储）
  3. ✅ 部署形态与 PII 边界：阿里云 ECS 自部署（应用 + PostgreSQL 均自装，2026-07-05 owner 拍板）；备份与导出文件保管策略在 platform-skeleton / v1-hardening 细化（建议 pg_dump 定时 + 异地副本）；TG token 走环境变量已写入 4.5
- ✅ 「渠道」「线索」已补入 CONTEXT.md（2026-07-06，cs-domain；线索定义为"无成交订单的客户"）；技术栈已落 ADR-002（PostgreSQL）与 ADR-003（Gin + JSON/OpenAPI）。
- 剩余未起草 req 仅提醒引擎；档期主体已由 `2026-07-09-schedule-calendar` 落地并升级为 current，2026-07-31 owner 另批准 `calendar-v2-redesign` 增量，不改写首版完成历史。
- 零成交线索的跟进提醒（本版 churn 刻意排除）记二期候选，配合渠道转化分析一起规划。
- **reminder-engine 已知边界（2026-07-13 acceptance）**：① `digest_hour` 早于每日 runner 首次跨日扫描完成时刻时可能出现摘要空窗，telegram-digest 应在推送前顺带触发幂等扫描；② 复购触发旧 churn 自动 dismissed 后若新订单再取消，既有 churn dedup 行不会回到 pending，可能静默到产生新的最近成交单；③ 账号时区向西修改可能让检查点暂时领先本地日期，后续自然日推进后自愈。三项均不改变本 feature 已验收边界，后续消费/迭代需显式读取。
- **二期候选（2026-07-06 设计原型比对拍板，本版不做）**：①拍摄回顾 / 选片相册缩略图（原型 customer-detail 有此卡片；roadmap §2 已明确在线选片/交付不做，首版无数据来源）；②多层人脉链可视化与转介绍带单金额归因（原型展示"转介绍 2 层 · 合计 ¥3,140"；首版只有 referrer_customer_id 单向引用 + 详情页介绍人摘要，链式聚合与金额归因属渠道转化分析范畴）——两项与渠道转化分析同批规划。
- ✅ **OpenAPI 同步结果**：customer-core 已收编 §4 契约增量；2026-07-10 schedule-calendar update 同时把 `GET /me` 收编为平台契约并增加 timezone，消解原白名单债。
- **头像与全量导出决策 gate**：`customer-avatar` 只保证 Customer JSON 带可用 `avatar_revision/avatar_version/avatar_url` 与本地卷可做一致备份；当前 §4.6 仍是实体 JSON。`data-export` design 启动前必须由 owner 二选一：reference-only JSON（明确不承诺头像便携恢复），或先把 §4.6 update 为媒体文件 + exact-generation manifest/key/count/checksum 的便携包。未拍板不得启动/完成该条；不得把鉴权 URL 冒充可携带资产。**2026-07-21 resolved**：owner 已选择 reference-only JSON；只导出公开头像引用元数据，不含头像二进制、内部 object_id 或 manifest，不承诺跨环境便携恢复；§4.6 JSON 契约保持不变，启动 gate 已解除。
- "owner 真实使用两周"作为产品成功软信号，不进验收门槛，由 owner 自行观察后决定二期方向（画像/渠道分析）。

## 8. 变更日志

- 2026-07-31（calendar-v2-redesign owner 批准）：新增条目 13 作为 roadmap-owned 增量，不回退旧 `schedule-calendar` / `data-export` done 状态。§2/§3 收编月/周双视图、可约空档回答、转场软提醒和移动端完整 CRUD；§4.2 给 Settings 增严格 ISO weekday availability（周日默认 09:00–20:00、单日单窗口、最小空档 120 分钟、转场 60 分钟、Temporal compatible DST）；§4.3 扩 shoot slot 批量摘要为价格/定金/尾款/套系拍摄类型并要求 Settings 局部 strict decode；§4.6 因 required availability 把导出 schema_version 升到 2 并要求非默认 Settings parity。openings/overview 继续由 webapp 对短窗口纯计算，不新增聚合端点；旧可靠写流程保留并补 conflict preview generation。
- 2026-07-13（reminder-engine acceptance）：条目 8 完成；Settings 明确归 `backend/internal/settings`，`GET /reminders` 固定 `due_date ASC,id ASC`，birthday dedup 年份明确为生日发生日年份，custom dedup 固定为 `custom:{reminder_id}`；记录 digest 空窗、复购取消后 churn 静默和时区西移检查点自愈三项已知边界。
- 2026-07-13（customer-avatar owner 选择）：merged source 默认保留合并时的头像且 GET 可读，但 PUT 继续 `409 customer_merged`，DELETE 改为唯一 cleanup-only PII 清理例外；它只能清 pointer 并进入精确代次 GC，不恢复 merged 档案其他编辑能力。owner 同时接受首版 GC 默认值：24h grace、每小时 runner、每账号每 tick 各一页 object inventory/current-pointer audit +100 due。
- 2026-07-13（customer-avatar roadmap round 10 收敛）：generation 模型继续补齐四个边界。① 新增独立 `avatar_revision=ar-{非负 bigint}`，PUT/DELETE If-Match 用 revision，content v/ETag 继续用 checksum，关闭 A→B→A ABA；② pre-current generation 只要出现 GC row 就永久烧毁并换 fresh object_id，避免 DB session 丢失后的在途 Delete 与原 PUT 重新晋升同一 key；③ desired=current 只有完整验证实际 generation 后才 no-op 200，缺失/损坏走 revision CAS 的 fresh generation 修复；④ 一致备份/OSS copy 改为 exact-generation manifest/key/object_id 核验，并把 signal-aware root context、HTTP graceful shutdown 与 runner 有界退出纳入本 feature。
- 2026-07-13（customer-avatar roadmap round 9 修正）：独立 review 证明 transaction-scoped advisory lock 会在 PostgreSQL session 丢失时提前释放，而已发出的 filesystem/OSS Delete 仍可能迟到，故移除 `AvatarObjectFence`。改为公开 checksum version + 永不复用的随机 `avatar_object_id` 双层模型，五字段 current pointer 指向完整 generation，GC 只删精确旧 object_id；同 checksum 再发布必用新 key，因此迟到 Delete 在存储侧天然隔离。保留 pending enqueue、MaintenanceRunner cadence/single-flight/shutdown、require-mount attestation 与固定摘要 CustomerAvatar 约束。
- 2026-07-12（customer-avatar 独立 roadmap review 后收敛）：保留 owner 拍板的“首版 ECS/local persistent volume、后期 OSS adapter”方向，但将覆盖式 current key 修订为 checksum 不可变对象 + PostgreSQL current pointer；补 `PutImmutable/Open/Stat/List/Delete`、If-Match 条件写、强版本 GET/ETag/304、延迟 GC/reconciliation、local fsync/atomic rename、一致备份与 future OSS freeze-copy-verify-switch 边界。原因：DDD 隔离能稳定上层接口，但不会自动解决 PostgreSQL 与 filesystem/OSS 间的分布式一致性。`customer-avatar` 模块归属改为 customer+platform+webapp，显式依赖已完成的 order/schedule 并承担其 UI 兼容增量，不回退已 done 状态；data-export 增 owner 决策 gate；acceptance 提示为 PostgreSQL pointer + 二进制 durable adjunct 补充 ADR，不直接改 ADR-002。
- 2026-07-11（customer-avatar design 启动，owner 拍板本地优先 / OSS 可替换）：补齐头像媒体读取与存储 seam。`Customer.avatar_url` 固定为同源鉴权媒体 URL，新增 `GET /customers/{id}/avatar/content`；上传限制为 JPEG/PNG/WebP、≤5 MiB、解码边界 4096、规范化最长边 ≤512 并去元数据。首版 local persistent volume + in-memory fake，未来 OSS adapter 保持应用层与前端契约不变，存量搬迁另起 migration。merge 不迁移头像；data-export 的二进制便携性留条目 11 启动前回 roadmap 决策。本条最初使用覆盖式 stable key，已被 2026-07-12 收敛记录取代。
- 2026-07-10（schedule-calendar 产品评审与独立 review 后 owner 授权优化）：档期契约与条目 7 同步 update。首版从「月/周」收窄为周一首列的固定 6 周月视图；补客户档案页共用入口、跨日/全天、稳定排序与唯一 slot 冲突计数、保存前重叠明细、成功后刷新确认、失败/过期恢复和删除引导；POST /orders 与 POST /schedule/slots 增可选 Idempotency-Key，固化 typed operation 常量、唯一事务 owner、只缓存 2xx、128-bit flow/per-step attempt 与 24 小时重放边界，历史排期跳转 backfill 的订单创建也纳入同一 pending journal；POST /orders 增 creation_mode 显式区分新业务/历史补录；GET /orders 增时间感知 schedulable_at；路径 A 新建 consulting，所有 consulting 订单都先成功建 slot 并刷新日历再同步 scheduled，且使用刷新后 slot 摘要的 customer_id 修正 merge 竞态；shoot 写与归档/merge 统一 customer→order 锁序；GET /me 增 timezone 且加载失败禁止按浏览器时区写入；GET slot 列表升级为 type 判别 union，shoot 必返订单/客户/状态摘要并落到客户档案订单 tab；shoot 按订单+客户未来/历史矩阵分流并限制一订单一 shoot；PATCH 明确 nullable 三态；order_in_use/order_already_scheduled details 可直达现有 slot。受影响：已完成 order-tracking 由条目 7 承担兼容增量，无 header、无 creation_mode、无 schedulable_at 的既有新业务调用保持默认行为/列表排序/total；现有补录 UI 同步显式传 backfill。旧 roadmap/design review 因实质变化失效并重跑。
- 2026-07-09（order-tracking design round-2 review 后 owner 追加拍板）：订单域契约再 update 两项，§4.2/§4.3 同步：
  - **§4.3 POST /orders 补录直达**：POST 扩为全 shape（status/deposit_paid/balance_paid/shot_at/delivered_at/note 均可选），status 可直达八态任意值、不必逐级跃迁——历史订单补录是上线刚需（老客户 last_shot_at/churn 基线，否则 reminder-engine 上线即误报），逐级跳既伪造流程又笨重。创建与跃迁/字段修正三条路径同守 §4.2 不变量：目标状态 ≥shot 须显式 shot_at、≥delivered 须显式 delivered_at（补录缺省 now 必错，fail loud 400）、closed 须已结清（409 unpaid_balance）；补录（status≠consulting）允许引用 archived 套系（历史真实性优先），新业务建单仍只允许 active（原 FDR-003 规则不变）。原「reminder-engine 启动前决策补录」观察项就此消解。
  - **§4.3 新增 DELETE /orders/{id} 终态物理删除**：仅 closed/cancelled 可删 → 204；非终态 → 409 order_not_terminal（先 cancel）。动因：误操作产生的 cancelled 垃圾单若不可清理，会永久阻塞套系删除（delete in-use 为 any-reference 口径）且污染订单列表；owner 单人工具，删除权在 owner。删除即从实时聚合消失（删 closed 单会减少 orders_count/营收类统计，UI 确认须明示）；被 shoot slot 引用 → 409 order_in_use 随 schedule-calendar 生长接通；reminder 引用不拦截（引用已删订单的 pending 提醒由 reminder-engine 自动 dismiss/跳过，其 design 细化）。原「订单只 cancel 不物理删」的 order-tracking design 约束同步推翻。
  - **受影响条目**：`order-tracking`（本 feature 落地 POST 补录与 DELETE，OpenAPI 已同步收编）；`schedule-calendar`（接通 order_in_use 拦截，验收补用例）；`reminder-engine`（已删订单的 reminder 处理细则）。
- 2026-07-09（order-tracking design PM 视角评审后 owner 拍板）：订单域契约 update 五项，§4.2/§4.3 同步：
  - **§4.2 跃迁表加前跳边**：`shot→delivered`、`selected→delivered`（跳过选片/精修）。动因：Package 契约本就支持 `retouch_count=0`（不含精修）与 `raw_delivery_count`（底片直出）的商品形态，强制线性会迫使直出单伪造 selected/retouching 假状态；仍禁一切回退与其余跳步。
  - **§4.2 字段修正不变量**：状态机不变量从"跃迁时刻检查"升级为"恒成立"——closed 恒结清（PATCH `balance_paid=false` 于 closed 订单 → 409 unpaid_balance）；shot_at/delivered_at 仅已到达对应状态可写、写入后不可置空（未到达传入 → 400）；终态仅可修正 note/title/price/时间戳，付款标记不可变更。堵住"字段修正路径绕过门禁产生矛盾态（closed 未结清、shot 无 shot_at、consulting 带 shot_at 污染 last_shot_at）"的漏洞。
  - **§4.3 GET /orders 列表项附引用摘要**：`customer_display_name`（必返）、`package_name?`（引用套系时）——Order shape 只有 id 引用，无摘要则全局订单页不可读，前端将被迫 N+1 或拉全量客户；沿用客户/套系"列表项附聚合"既有模式。
  - **§4.3 unpaid_balance 口径收窄**：`balance_paid=false AND status ∈ {shot, selected, retouching, delivered}`（"已进入交付链条且未结清"）。原设计假设的宽口径（仅排除 cancelled）会把 consulting/scheduled 常态未结清单混入"未收尾款"视图，名实不符、信号噪音大。
  - **§4.3 默认排序钉死**：`created_at DESC, id DESC`（稳定分页前提）；不加 sort 参数。
  - **受影响条目**：`order-tracking`（本次 design 即按新契约执行，OpenAPI 于 design 阶段同步收编：新增 `OrderListItem` schema、listOrders 响应与参数描述）。其余条目不受影响；dashboard `unpaid_orders` 卡仍为 delivered-only 窄口径，两口径分工不变。
- 2026-07-08（package-catalog design 启动时 owner 拍板）：套系从"静态定义 + 归档"改为**商品心智的上架/下架/删除**三操作，§4.2/§4.3 接口契约同步 update：
  - **§4.2**：Package `status(active|archived)` 语义明确为 active=上架中、archived=下架停售（两态为持久状态）；套系无 merged 态（区别于 Customer）。
  - **§4.3**：套系域端点从 `POST/GET/PATCH` 扩为 `POST/GET/PATCH/DELETE`。下架 = PATCH {status:archived}（无 in-use 校验，存量订单继续引用，语义不变，仅措辞对齐商品心智），恢复上架 = PATCH {status:active}；**新增删除** DELETE /packages/{id} → 204，**带 in-use 校验**：被任一订单引用时拒绝返回 `409 package_in_use`（保护 Order.package_id 引用完整性），无引用才物理删除。删除是破坏性操作、非持久状态。
  - **契约随域生长**：in-use 校验需查订单表，但 orders 表在 order-tracking 才建；package-catalog 落地时无订单可引用、删除恒放行，真实 `409 package_in_use` 由 order-tracking 接通订单引用检查（其验收补"删除被引用套系 409"用例，与 orders_count 真实计数一并接通）——与 orders_count 恒 0→真实计算同一模式，字段/校验从第一天存在不破坏 shape。
  - **受影响条目**：`package-catalog`（本次落地删除操作与上下架语义）；`order-tracking`（补删除 in-use 接通用例）。§4.2/§4.3 其余契约不变。
- 2026-07-08：根据 owner 反馈新增 `customer-avatar` planned 子 feature：客户头像可选设置/替换/移除，用于列表、详情、转介绍下拉、merge 对话框等客户选择面辅助识别；同时明确客户选择展示不拼社交账号/来源渠道，短 UID 负责消歧，头像作为后续增强。

- 2026-07-07（customer-profile-complete design 启动时拍板）：§4.2 补「转介绍指针语义」——merge 时 referrer 指向 source 的批量重定向 target（自指清空）；归档不清洗既有 referrer；介绍人候选仅 active（只约束新写入）；channel 可 PATCH 修正且与 referrer 联动（改为 referral 必带介绍人、改走 referral 自动清空）。承接 customer-core review 遗留 REV-006。

- 2026-07-07：根据 owner review 调整 `customer-core` 建档契约：`POST /customers` 从单个 `identity` 改为 `identities[1..N]`，30 秒建档硬性要求至少 1 个社交身份、允许同次录入多个私域账号；无社交身份创建必须 400，不允许生成无身份客户；建档后的身份增删仍归 `customer-profile-complete`。

- 2026-07-06：依据「影约 CRM」设计原型（Open Design 高保真原型，5 页面）与 roadmap 全量比对后的 update（owner 三项拍板：A 契约补齐 / B 超范围项只记录 / C 组合流程拍板）。
  - **接口契约变化（§4.1 / §4.2 / §4.3）**：
    1. `GET /customers` 列表项附聚合字段 `orders_count` / `last_shot_at?`；`q` 匹配范围加 `phone`；`GET /customers/{id}` 附 `stats{orders_count, total_order_amount, last_shot_at?}`（客龄由 created_at 前端推导）。聚合字段在 order 域未落地前恒 0/null，字段自始存在避免破坏性变更
    2. `GET /packages` 列表项附聚合 `orders_count`
    3. `GET /reminders` 新增 `customer_id=` 过滤（客户档案页「提醒」tab 数据源）
    4. `Package` 增加 `note?(≤500字)` 字段（棚租说明 / 加拍规则等）
    5. `SocialIdentity.platform` 枚举扩展：加 `xiaohongshu|douyin|weibo`（来源平台账号是真实私域身份）
    6. dashboard 口径对齐原型：`due_reminders` 窗口 = 账号时区今日+2 天（近 3 天含逾期）；`month_stats` 改为 `recent_stats`（近 30 天滚动窗口，orders_created 按 created_at、orders_delivered / revenue_confirmed 按 delivered_at 判窗）；§4.1 时区条目同步措辞
  - **范围记录（§7 观察项）**：拍摄回顾 / 选片相册、多层人脉链与转介绍金额归因——超出首版，记二期候选（与渠道转化分析同批），原型中对应卡片首版不实现
  - **组合流程拍板（§5 条目 7 备注）**：日历新建拍摄档期 = 前端隐式先 `POST /orders` 再 `POST /schedule/slots`，不新增组合端点，两步失败处理归 schedule-calendar feature design
  - **受影响的已启动 / 完成 feature**：platform-skeleton（done）——其交付的登录 / me / healthz 端点不受本次变化影响，无需返工；但 `api/openapi.yaml` 为 §4 全量固化，本次增量产生文档级漂移，收编责任落 customer-core 启动时（见 §7「OpenAPI 同步待办」）。其余条目均 planned，直接按新契约执行
  - **roadmap-review（round 3）复核后补充的口径闭合**：recent_stats 窗口定义为账号时区自然日 [今日-29, 今日]、orders_delivered/revenue_confirmed 排除当前 cancelled（与 total_order_amount 对齐）、orders_created 含全部状态；聚合字段 orders_count/last_shot_at 统一"非 cancelled"口径，last_shot_at 补入 §4.1 date-only 时区枚举；聚合明确为同进程读模型（repository/service 层完成）；§4.5 注明 TG 摘要有意保持"今日"窗口不随 dashboard 近 3 天；条目 7 备注补 design 必答清单（隐式第三步 PATCH scheduled 与悬挂 consulting 订单对 churn 的抑制）
