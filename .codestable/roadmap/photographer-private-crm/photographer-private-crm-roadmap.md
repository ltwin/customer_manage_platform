---
doc_type: roadmap
slug: photographer-private-crm
status: active
created: 2026-07-05
last_reviewed: 2026-07-08
tags: [crm, photographer, mvp, reminder, scheduling]
related_requirements: [customer-profile]
related_architecture: []
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
- 档期管理：日历视图、订单关联、重叠提示
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
| 为什么不是 single feature | 七个模块、十二条可独立交付的子 feature、跨模块接口契约（实体模型 / API / 提醒规则 / TG 协议 / 导出）、依赖构成 DAG，单 feature 装不下 |
| 为什么不是 brainstorm | 脑暴已完成（2026-07-05），定位 / 形态 / 优先级 / 边界均已拍板，目标与完成信号可写成可证伪条目 |
| roadmap 边界 | 只覆盖首版"先治忘"范围（上表）；画像分析、产品化、选片交付明确不做 |
| 最小闭环 | 第 2 条 `customer-core` 完成后：登录 → 30 秒建档（含渠道、1 个或多个社交身份）→ 列表 / 详情可查——"治忘"的第一块地基端到端可演示 |

## 3. 模块拆分（概设）

```
photographer-private-crm
├── platform   平台基座：认证、账号上下文、HTTP 框架、错误封套、数据访问基座、全量导出
├── customer   客户域：客户聚合（身份 / 渠道 / 转介绍 / 备注 / 渐进字段 / 合并 / 归档）
├── package    套系域：拍摄服务商品的静态定义、上下架与删除
├── order      订单域：订单状态机 + 定金尾款标记
├── schedule   档期域：时间段占用、日历查询、重叠提示
├── reminder   提醒与触达域：规则扫描、幂等提醒生成、TG Bot 绑定与推送
└── webapp     Web 前端：各域页面 + dashboard + 移动轻路径
```

### platform · 平台基座
- **职责**：单账号登录发 token；请求中携带账号上下文；错误封套与校验；数据访问基座强制账号过滤（ADR-001 的执行点）；全量导出（跨域读，归口基座避免各域各写一半）。不含任何业务实体。
- **承载的子 feature**：platform-skeleton、data-export（+ v1-hardening 部分）
- **触碰的现有代码**：无（greenfield 全新）
- **Depth 判断**：deep——账号隔离、鉴权、错误封套全部藏在中间件与 repository 基座内，业务域代码不重复写 `account_id` 过滤；删掉它复杂度会散到所有域，通过 deletion test。

### customer · 客户域
- **职责**：客户聚合根的全生命周期：建档、社交身份增删、渠道、转介绍、备注、渐进字段、合并、归档。不管订单 / 档期 / 提醒（它们反向引用客户）。
- **承载的子 feature**：customer-core、customer-profile-complete、customer-avatar
- **触碰的现有代码**：无
- **Depth 判断**：deep——"合并客户迁移全部关联资源"这类复杂度藏在域内，对外只是一个 merge 端点。

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
- **职责**：时间段（slot）创建 / 查询 / 重叠检测；slot 可关联订单也可为独立忙碌块。不管订单状态（创建 / 删除 slot 不反向改订单）。
- **承载的子 feature**：schedule-calendar
- **触碰的现有代码**：无
- **Depth 判断**：deep——重叠检测与区间查询藏在域内。

### reminder · 提醒与触达域
- **职责**：按规则（生日 / 回访 / 流失）每日扫描客户与订单数据、幂等生成提醒；提醒完成 / 忽略；TG Bot 绑定与每日摘要推送。**刻意不拆独立"通知模块"**——首版单通道（TG），拆出来是 pass-through 假 seam；TG 以 injected port 形式存在于本域内（见 4.5）。
- **承载的子 feature**：reminder-engine、telegram-digest
- **触碰的现有代码**：无
- **Depth 判断**：deep——规则参数、幂等键、扫描窗口全部藏在域内，对外只有 Reminder 资源和摘要推送。

### webapp · Web 前端
- **职责**：所有域的页面 + dashboard 聚合面板 + 移动轻路径（查档期 / 搜客户 / 记备注）。只消费 4.1 定义的 HTTP API，无后端私有耦合。
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
错误封套:  { "error": { "code": string, "message": string } }
错误码:    400 validation_failed | 401 unauthorized | 404 not_found
           409 conflict（子码见各域）| 500 internal
分页:      ?page=1&page_size=20 → { "items": [...], "total": int }
时间:      ISO 8601 UTC（"2026-07-05T09:00:00Z"）；生日特例 "MM-DD" 或 "YYYY-MM-DD"（年份可缺）
时区:      存储一律 UTC；所有 date-only 字段（due_date / birthday / last_shot_at）、"今日 / 当日 / 逾期"判定、
           digest_hour、统计滚动窗口（dashboard recent_stats），一律按 Settings.timezone（IANA，默认 Asia/Shanghai）计算
ID:        string（引擎无关；服务端生成）
```

**约束**：所有资源端点隐式按当前账号过滤，客户端**永不**传 `account_id`（ADR-001：过滤在 repository 基座强制，遗漏视为缺陷）。**Handler 薄层约束**：领域逻辑（状态机 / merge / 提醒规则等）不得 import 路由框架，`gin.Context` 等框架类型不下穿到 service / repository 层——这是将来若微服务化时框架层可低成本置换的前提，进 code review 检查口径。

**Interface 设计检查**：platform 暴露；invariant = 未认证一律 401、跨账号数据不可见、日界判定单一口径（timezone 只在 Settings 一处定义）；seam 放在 HTTP API 层——webapp 与集成测试都穿过它取数；dependency strategy：webapp→API 为 remote-owned（自有服务）；存储为 local-substitutable（repository 接口 + 测试内存/容器替身，production 单 adapter 不算假 seam——替身用于测试面）。

### 4.2 共享实体逻辑模型

所有域共享的持久化 shape（字段级契约；`*` = 必填；所有实体含 `id`、`account_id`、`created_at`，不再重复列）：

```
Customer:        display_name*, real_name?, phone?, birthday?("MM-DD"|"YYYY-MM-DD"),
                 channel*(xiaohongshu|douyin|weibo|referral|other),
                 referrer_customer_id?(channel=referral 时必填),
                 status*(active|merged|archived), merged_into_customer_id?, avatar_url?
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
                 shot_at?, delivered_at?, note?
ScheduleSlot:    start_at*, end_at*(> start_at), type*(shoot|hold|busy),
                 order_id?(type=shoot 时必填), note?
Reminder:        type*(birthday|follow_up|churn|custom), customer_id?, order_id?,
                 due_date*(date), content*, status*(pending|done|dismissed),
                 dedup_key*(unique per account, 见 4.4)
Settings:        timezone*(IANA, 默认 "Asia/Shanghai"),
                 birthday_lead_days*(默认 3), follow_up_after_days*(默认 7),
                 churn_thresholds*: [{shoot_type, days}](默认全类型 180),
                 digest_hour*(0-23, 默认 9, 按 timezone), telegram_chat_id?
```

**订单状态语义与跃迁**：
- 语义：consulting 咨询中 ｜ scheduled 已定档 ｜ shot 已拍摄 ｜ selected 已选片 ｜ retouching 精修中 ｜ delivered 已交付（照片已给客户）｜ closed 完结（服务与收款均完成）｜ cancelled 取消（含坏账，note 写原因）
- 合法跃迁：consulting→scheduled→shot→selected→retouching→delivered→closed；任意非终态→cancelled；不允许跳步回退（回退场景走 cancelled + 新订单）。非法跃迁 → `409 invalid_status_transition`
- **时间戳自动写入**：进入 shot 时服务端自动写 `shot_at`（请求显式提供则用请求值，事后可 PATCH 修正）；进入 delivered 同理写 `delivered_at`。提醒规则依赖这两个字段，此规则是 order-tracking 的硬验收项
- `balance_paid=false` 时进入 closed → `409 unpaid_balance`（坏账场景走 cancelled）
- 创建 / 删除 ScheduleSlot **不**自动变更订单状态；状态只由 `PATCH /orders/{id}` 显式驱动

**客户 merge / 归档语义**：
- merge：source 必须 `status=active`；source 的 SocialIdentity / CustomerNote / Order / Reminder 全部改挂 target，source `status=merged` + `merged_into_customer_id`，不物理删除。**order / reminder 域晚于 merge 落地，其 feature 验收必须各自补"merge 迁移本域实体"用例**（契约随域生长，不静默失效）
- 归档：`PATCH /customers/{id} {status:archived}`；归档客户不参与提醒扫描、不可被新订单引用（`409 customer_archived`）、默认列表隐藏（`?status` 缺省 active，可显式查 archived/all）

**转介绍指针语义**（2026-07-07 拍板，随 customer-profile-complete 落地）：
- merge 时其他客户 `referrer_customer_id` 指向 source 的，同事务批量重定向到 target；重定向后 target 的介绍人若变成自身则清空（介绍链跟人走，不指向 merged 壳）
- 归档**不**清洗既有 referrer 指针——介绍关系是历史事实，与经营状态无关；「仅 `status=active` 客户可被选为介绍人」只约束新写入（建档与 PATCH），不回溯
- `channel` 可 PATCH 修正：改为 `referral` 必须同请求携带 `referrer_customer_id`（否则 400）；从 `referral` 改为其他渠道时服务端自动清空 `referrer_customer_id`

### 4.3 各域资源 API

**方向**：webapp → 各域　**形式**：HTTP API（总约定见 4.1，实体 shape 见 4.2，此处只列端点与特有字段）

```
客户域
  POST   /customers                 {display_name, channel, referrer_customer_id?,
                                     identities:[{platform, handle, remark?}]} → 201 Customer
                                    （30 秒建档端点：display_name/channel/identities[1..N] 为硬性必填；
                                      identities 为空 → 400 validation_failed；
                                      channel=referral 时额外必填 referrer_customer_id）
  GET    /customers?q=&channel=&status=&page=      q 匹配 display_name/real_name/phone/identity.handle
                                    status 缺省 active
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
  PUT    /customers/{id}/avatar     multipart/form-data file            → 200 Customer
  DELETE /customers/{id}/avatar                                          → 200 Customer
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
  POST   /orders                    {customer_id, package_id?, title?, price?} → 201（status=consulting）
                                    引用 merged/archived 客户 → 409 customer_archived
  GET    /orders?customer_id=&status=&unpaid_balance=true&page=
  PATCH  /orders/{id}               {status?|deposit_paid?|balance_paid?|shot_at?|delivered_at?|…}
                                    非法跃迁 → 409 invalid_status_transition
                                    未结清进 closed → 409 unpaid_balance
档期域
  GET    /schedule/slots?from=&to=  区间查询（含跨界 slot）
  POST   /schedule/slots            {start_at, end_at, type, order_id?, note?}
                                    → 201 { slot: Slot, overlaps: [slot_id] }
                                    重叠不阻止，返回 overlaps 由前端提示（hold 双留是真实业务）
  PATCH/DELETE /schedule/slots/{id}
提醒域
  GET    /reminders?status=pending&customer_id=&due_before=&page=
  POST   /reminders                 {type:custom, customer_id?, due_date, content} → 201
  POST   /reminders/{id}/done | /dismiss
  POST   /admin/reminders/scan      {date?("YYYY-MM-DD", 缺省=账号时区今日)}
                                    → { created: int, skipped: int, auto_dismissed: int }
                                    （鉴权同 4.1；每日定时任务与手动触发唯一共用入口）
设置
  GET/PATCH /settings               （shape 见 4.2 Settings）
  POST   /settings/telegram/bind-token → { token, deep_link }   （绑定流程见 4.5）
dashboard
  GET    /dashboard →
         { due_reminders: Reminder[](due_date ≤ 账号时区今日+2 天, pending——近 3 天窗口含逾期，
                          2026-07-06 原型比对拍板),
           today_slots: Slot[](含订单+客户摘要),
           unpaid_orders: { count, items: Order[](status=delivered 且 balance_paid=false；
                            closed 必已结清故不出现) },
           churn_alerts: Reminder[](type=churn, pending),
           recent_stats: { orders_created(按 created_at, 含全部状态),
                           orders_delivered(按 delivered_at, 排除当前 status=cancelled),
                           revenue_confirmed(分, delivered_at 落窗口内且 balance_paid=true
                           且当前非 cancelled 的订单 price 之和) } }
         （recent_stats 窗口 = 账号时区自然日 [今日-29, 今日] 含今日共 30 天；替代原"本月"口径，
           cancelled 归属与 total_order_amount 对齐，2026-07-06 原型比对拍板）
```

**Interface 设计检查**：dashboard 聚合做在服务端（一次请求 vs 前端拼五个列表）——Design-It-Twice 比较过"前端自行组合"（省一个端点但移动端五连击、口径散落前端）与"服务端聚合"（口径单点、移动友好），选后者；depth：聚合口径（何为"待收尾款"）藏在服务端一处。

### 4.4 提醒引擎规则契约

**方向**：reminder 域 ← customer / order 域数据（同进程读模型，in-process 依赖）　**形式**：内部规则 + Reminder 资源

```
扫描:  每日一次（digest_hour 前完成，按账号时区）；唯一手动触发入口 = POST /admin/reminders/scan（4.3）
       全量扫描必须幂等
规则:
  birthday   触发: customer.birthday 距今 ≤ birthday_lead_days 且未过（按账号时区判日界）
             dedup_key = "birthday:{customer_id}:{当年年份}"
  follow_up  触发: order.status=delivered 且 delivered_at + follow_up_after_days ≤ 今日
             dedup_key = "follow_up:{order_id}"
  churn      触发: 客户至少有一单 delivered|closed、当前无非终态订单，且最近一单 shot_at
                   距今 > churn_thresholds[该客户最近套系 shoot_type，缺省 other]
                   （零成交客户不生成 churn——线索跟进规则记二期，防提醒噪音）
             dedup_key = "churn:{customer_id}:{最近一单 order_id}"
             （客户再下单后旧 churn 提醒自动 dismissed）
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
失败:   发送失败重试 3 次（间隔 1/5/30min）；仍失败只记日志——dashboard 是兜底展示（A+D 冗余设计），
        推送失败不得影响 Reminder 生成
凭证:   bot token 只经环境变量注入，不入库、不入 git（规则落 attention.md）
```

**Interface 设计检查**：真实双 adapter（production + test fake），非假 seam；invariant = 摘要生成纯函数（同一数据同一文本），推送与生成分离。

### 4.6 全量导出契约

**方向**：webapp → platform　**形式**：HTTP API

```
GET /export → application/json（Content-Disposition 附件）
{ exported_at, schema_version: 1,
  counts: { customers, social_identities, customer_notes, packages, orders,
            schedule_slots, reminders },
  customers[], social_identities[], customer_notes[], packages[], orders[],
  schedule_slots[], reminders[], settings }        // 各数组 shape 全部按 4.2
```

**约束**：全量无分页；`counts` 必须与各数组长度一致（验收核对点）；含全部 PII，导出文件的存放责任在 owner（见第 7 节拍板包）。

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
   - 所属模块：customer + webapp ｜ 依赖：customer-profile-complete ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：头像不作为建档必填项；完成信号：头像字段/API 与前端客户选择组件联动，上传/替换/移除用例通过，未设置头像 fallback 稳定；设计阶段需明确头像文件存储、大小/格式限制、删除后的对象清理策略
5. **package-catalog** — 套系 CRUD、上下架与删除：类型/定价方式/张数时长底片精修参数；下架后不出现在选择列表，删除受引用完整性保护
   - 所属模块：package + webapp ｜ 依赖：platform-skeleton ｜ 状态：done ｜ 对应 feature：2026-07-08-package-catalog
   - 备注：商品心智（2026-07-08 owner 拍板，§4.2/§4.3 update）——active=上架、archived=下架、DELETE=删除；完成信号：按 4.2 Package shape 建/改/下架/上架各一条通过；?status=active 过滤正确；DELETE 无引用套系 204、被引用套系 409 package_in_use（order 域未落地前无订单可引用、删除恒放行，真实 409 由 order-tracking 接通）
6. **order-tracking** — 订单记录：创建（客户+套系）、八态状态机（非法跃迁 409、时间戳自动写入、未结清禁 closed）、定金/尾款标记、按客户/全局/未收尾款查询
   - 所属模块：order + webapp ｜ 依赖：customer-core, package-catalog ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：依赖理由——订单必须挂客户并引用套系；完成信号：跃迁矩阵测试全过（含 shot_at/delivered_at 自动写入、409 unpaid_balance）、unpaid_balance 筛选正确、**merge 迁移订单用例**（4.2 契约随域生长）、引用归档客户 409、**接通 customers/packages 聚合字段真实计算**（orders_count/last_shot_at/total_order_amount，4.3，2026-07-06 契约更新）、**接通套系删除 in-use 校验**（删除被订单引用的套系返回 409 package_in_use，§4.3 契约随域生长，2026-07-08 update）
7. **schedule-calendar** — 档期：月/周日历视图、slot CRUD、订单关联、重叠返回 overlaps 提示、独立忙碌块
   - 所属模块：schedule + webapp ｜ 依赖：order-tracking ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：依赖理由——type=shoot 的 slot 必须挂订单；完成信号：重叠创建返回 overlaps 且前端提示；建/删 slot 不改订单状态（反向联动禁止用例）；日历页答复"某天有没有档"≤10 秒（演示）；**组合流程拍板（2026-07-06）**：前端「新建拍摄档期」弹窗（含客户档案页「＋新约单」入口）隐式先 POST /orders（选客户+套系）再 POST /schedule/slots 挂 order_id——不新增组合端点，两步失败处理（订单已建、slot 失败时的提示与补救）在本条 feature design 内定义，此流程是 design 硬约束；**design 必答清单**：①保存成功后是否隐式第三步 `PATCH /orders {status:scheduled}`（合法，显式驱动，不违反"slot 不反向改状态"不变量）还是接受"日历有 shoot slot 的 consulting 订单"常态；②slot 失败留下的悬挂 consulting 订单按 4.4 属"非终态订单"会抑制该客户 churn 预警，补救策略须覆盖
8. **reminder-engine** — 提醒引擎：/admin/reminders/scan 幂等生成三类提醒、done/dismiss、参数可配置（含按拍摄类型流失阈值、账号时区）
   - 所属模块：reminder ｜ 依赖：customer-profile-complete, order-tracking ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：依赖理由——生日规则要 birthday 字段（条目 3），回访/流失规则要订单状态时间戳（条目 6）；完成信号：同日双跑扫描零新增；三规则正/反用例（含时区日界、时间戳缺失跳过、零成交不告警）；改阈值后下轮扫描生效；**merge 迁移提醒用例**；GET /reminders 支持 customer_id 过滤（4.3，2026-07-06 契约更新）
9. **telegram-digest** — TG Bot：bind-token 绑定流程、每日摘要推送、/today 命令、失败重试与日志
   - 所属模块：reminder（TelegramPort）｜ 依赖：reminder-engine, schedule-calendar ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：依赖理由——摘要内容 = 提醒（条目 8）+ 当日档期（条目 7）；bot token 已在条目 1 冒烟验证；完成信号：owner 真机绑定并收到含真实数据的摘要（截图）；未绑定时系统全功能正常
10. **dashboard** — 首页面板：待办提醒（近 3 天窗口）、今日档期、待收尾款、流失预警、近 30 天概览（4.3 dashboard 契约）
   - 所属模块：webapp ｜ 依赖：order-tracking, schedule-calendar, reminder-engine ｜ 状态：planned ｜ 对应 feature：未启动
   - 备注：完成信号：五卡片数据与各域列表页交叉一致（核对用例）；登录后默认落地页
11. **data-export** — 全量 JSON 导出（4.6 契约）：一键导出全部实体 + counts 核对
    - 所属模块：platform ｜ 依赖：customer-profile-complete, customer-avatar, package-catalog, order-tracking, schedule-calendar, reminder-engine ｜ 状态：planned ｜ 对应 feature：未启动
    - 备注：依赖理由——导出范围 = 4.2 全部实体，各域落地后才有内容可导；完成信号：counts 与数组长度一致的自动化用例；导出文件手工抽查含 PII 字段完整
12. **v1-hardening** — 首版收口：空态/错误态/加载态清扫、移动轻路径（查档期/搜客户/记备注）、回归清单、README 使用说明
    - 所属模块：跨模块 ｜ 依赖：customer-avatar, telegram-digest, dashboard, data-export ｜ 状态：planned ｜ 对应 feature：未启动
    - 备注：完成信号：375px 宽度下三条轻路径可完成；回归清单逐条打勾归档；README 覆盖部署/备份/凭证操作

**最小闭环**：第 2 条 `customer-core` 做完后，登录 → 30 秒建一个带渠道、1 个或多个社交身份的客户 → 列表搜到、详情看到——端到端最窄路径可演示。

### Goal Coverage Matrix

| Goal / completion signal | Covered by | Verification entry | Evidence type | Core? |
|---|---|---|---|---|
| 客户集中建档、30 秒录入、多平台归一（req customer-profile） | 2, 3, 4 | 多身份建档计时演示 + merge/归档用例测试 + 头像选择面截图 | test + screenshot | yes |
| 渠道归因：每个客户带来源渠道可筛选 | 2 | GET /customers?channel= 用例 | test | yes |
| 再也不忘：三类提醒准确且不重复，主动送达 | 8, 9, 10 | 幂等双跑测试 + 时区日界用例 + TG 真机截图 + dashboard | test + screenshot | yes |
| 档期 10 秒可答、与客户套系关联 | 6, 7 | 日历页演示 + overlaps 用例 | test + screenshot | yes |
| 订单状态与定金尾款不漏 | 6, 10 | 跃迁矩阵测试（含时间戳/unpaid_balance）+ 筛选核对 | test | yes |
| 套系参数有结构化的家 | 5 | CRUD + 上下架过滤 + 删除引用完整性用例 | test | yes |
| 可持续基线：账号隔离 + 全绿验证命令 + 数据可带走 | 1, 11, 12 | make check（或等价）+ 基座过滤测试 + 导出 counts 核对 | command + test | yes |
| 首版整体完成信号 | 全部 | 一条链路演示：建档→套系→订单→档期→标定金→次日 TG 摘要→dashboard 五卡有数 | acceptance report | yes |

## 6. 排期思路与深度规划底稿

**为什么这么拆**：先基座（greenfield 必须先有验证入口和 ADR-001 执行点，并前置杀死 TG 外部依赖风险），再沿"先治忘"价值主线（客户 → 档案完整）铺数据地基（套系 → 订单 → 档期），让提醒引擎在真实数据上运转（引擎 → TG → dashboard），导出与收口断后。1-2 之后，3 与 5 可并行；4 跟随 3 作为客户选择面的识别增强，不阻塞订单/档期主线。

**目标完成信号**（roadmap 级）：上表末行的全链路演示在 owner 真机跑通 + 全部 items done/dropped。"owner 真实使用两周不弃用"是软信号，记观察项由 owner 主观判定，不作为 completed 门槛。

**Top 3 风险与缓解**：
1. **录入成本超 30 秒 → 工具弃用**（产品级最大风险）——缓解：契约 4.3 把"三项必填建档端点"写死；条目 2 验收含计时演示；条目 12 移动轻路径专项。
2. **提醒重复 / 漏发 / 跨日错位 → "治忘"卖点直接失信**——缓解：4.4 幂等键 + 4.1 时区单一口径写进契约；条目 8 硬验收"双跑零新增 + 时区日界用例"；TG 失败不影响生成，dashboard 兜底（A+D 冗余）。
3. **greenfield 无基线 → 后续 feature 无法可信验证**——缓解：条目 1 是安全网条目，交付全绿命令基线 + 账号过滤基座测试 + TG 冒烟，后续每条 feature 的 DoD 挂在这套命令上。

**非显然依赖**：TG bot token 需 owner 向 BotFather 申请（条目 1 前置冒烟，凭证走环境变量，规则落 attention.md）；**条目 1 启动前拍板包**（见第 7 节）：技术栈确认、存储引擎、部署形态与 PII 边界；生日年份可缺（"MM-DD"）导致年龄不可算——契约已按可缺设计。

**关键假设**（review 时可精确反驳）：① ~~技术栈假设~~ 已拍板：Go + React + PostgreSQL（2026-07-05，owner；待 cs-domain 落 ADR-002）；② owner 的 TG 可正常收 bot 消息（条目 1 冒烟即证实/证伪）；③ 默认参数（生日前 3 天、拍后 7 天、流失 180 天、摘要 9 点、时区 Asia/Shanghai）作为初始值合理，均可配置。

**基线与验证入口**：条目 1 交付 `make check`（或等价：build + test + lint 一键）作为全 roadmap 验证入口；UI 类条目另加浏览器手工路径（截图证据）；TG 类条目加真机截图。

**交付物落点**：每条 feature 落在代码 + 测试 + items.yaml 状态回写 + acceptance 报告；条目 3 完成时评估 req customer-profile draft→current；条目 12 落 README 与回归清单文档。

**知识回写点**（acceptance 时触发对应沉淀）：验证命令与本地起服务方式 → attention.md（cs-note）；技术栈与存储引擎落地 → ADR（cs-domain）；TG bot 申请与绑定坑 → compound（cs-keep）；渠道/线索术语 → CONTEXT.md（cs-domain）。

## 7. 观察项

- **条目 1 启动前拍板包**：
  1. ✅ 技术栈：Go + React（2026-07-05 owner 确认）
  2. ✅ 存储引擎：PostgreSQL（2026-07-05 owner 拍板，理由见第 4 节头注；大数据类需求二期按需引入专用存储）；待 cs-domain 落 ADR-002
  3. ✅ 部署形态与 PII 边界：阿里云 ECS 自部署（应用 + PostgreSQL 均自装，2026-07-05 owner 拍板）；备份与导出文件保管策略在 platform-skeleton / v1-hardening 细化（建议 pg_dump 定时 + 异地副本）；TG token 走环境变量已写入 4.5
- ✅ 「渠道」「线索」已补入 CONTEXT.md（2026-07-06，cs-domain；线索定义为"无成交订单的客户"）；技术栈已落 ADR-002（PostgreSQL）与 ADR-003（Gin + JSON/OpenAPI）。
- 剩余四份 req（提醒引擎/档期/订单/套系）尚未起草，建议各条目进 feature-design 时触发 `cs-req draft`。
- 零成交线索的跟进提醒（本版 churn 刻意排除）记二期候选，配合渠道转化分析一起规划。
- **二期候选（2026-07-06 设计原型比对拍板，本版不做）**：①拍摄回顾 / 选片相册缩略图（原型 customer-detail 有此卡片；roadmap §2 已明确在线选片/交付不做，首版无数据来源）；②多层人脉链可视化与转介绍带单金额归因（原型展示"转介绍 2 层 · 合计 ¥3,140"；首版只有 referrer_customer_id 单向引用 + 详情页介绍人摘要，链式聚合与金额归因属渠道转化分析范畴）——两项与渠道转化分析同批规划。
- ✅ **OpenAPI 同步结果**：customer-core 已收编 §4 契约增量（客户/套系聚合字段、q 匹配范围加 phone、reminders customer_id 过滤、Package.note、SocialPlatform 枚举扩展、dashboard 近 3 天 / 近 30 天口径），并按 `customer-core` tag 只注册已实现的客户三操作；`GET /me` 仍为 platform-skeleton 白名单债，后续另走 `cs-roadmap update` 收编到 roadmap 语义层。
- "owner 真实使用两周"作为产品成功软信号，不进验收门槛，由 owner 自行观察后决定二期方向（画像/渠道分析）。

## 8. 变更日志

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
