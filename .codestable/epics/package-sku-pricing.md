---
status: proposed
created: 2026-09-01
work: ../work/epic-package-sku-pricing.md
---
# 套系 SKU 化、多订单项与订单计价

## 起点

套系（`packages`）当前的 `pricing_mode(per_duration|per_photo|fixed)` 只参与分类展示，不参与服务端算价：三种模式共用一组 nullable 字段（`duration_minutes` / `shot_count_min|max` / `raw_delivery_count` / `retouch_count`），[service.go](../../backend/internal/package/service.go) 只校验枚举和单字段范围，不校验模式与参数组合；`base_price` 与计量参数之间没有领域公式。

订单当前只有一个 nullable `package_id`：[model.go](../../backend/internal/order/model.go)、[repository.go](../../backend/internal/order/repository.go)、OpenAPI 与 `ShootOrderFlow` 都以“一笔订单最多一个套系”为前提。这无法表达同一次交易中购买多个不同服务，例如“场照精修 2 张 + 双人精修 1 张”；若强迫用户拆成多笔订单，会把本应共同管理的收款、状态、档期和交付拆散，若为每个组合新建一个套系又会造成商品目录组合爆炸。

前端还存在计价语义不一致：[PackagesPage.tsx](../../frontend/src/pages/PackagesPage.tsx) 把 `per_photo.base_price` 展示成每张单价，却把 `per_duration.base_price` 展示成整段时长总价。订单侧 `price` 完全自由填写，没有套系级可复现基准，系统无法区分订单项范围变化、套系项人工改价、整单调整和创作策划加项。

创作策划域已有按规则生成可解释调价行的能力：[evaluator.go](../../backend/internal/shootplanning/business/evaluator.go) 的 `AdjustmentLine` / `RuleProfile` 与 append-only `order_price_adjustments` 记录售中加项，但其 target 仍只有单一 `package_id` 和订单总价。其 `current_shot_count` 统计的是策划中的 **Shot（镜头执行单元）**，按 `.codestable/requirements/CONTEXT.md` 的 canonical 定义，一条镜头不等于一张最终照片，不能替代订单项的照片计价数。

因此本 Epic 的任务不是重建通用电商，而是：把套系升级为可计算 SKU；让一笔订单包含多个有业务身份的订单项；把套系快照、数量与可归因成交金额放在订单项上；让订单总价、支付事实、策划调整、档期和下游经营口径继续闭合。

## 目标

用户可以在一笔订单中选择一个或多个不同套系，每个套系独立录入数量、精修数量和成交分配金额，并共享同一订单的客户、状态、档期、收款与交付生命周期。

计价形成“订单项三层金额 + 订单聚合”两级模型：

1. **订单项初始报价 `item.quoted_price`**：该订单项加入订单时按套系快照与初始计价数算出的标准价，永久不变；
2. **订单项当前公式价 `item.calculated_price`**：按同一套快照与该项当前计价数重算的标准价；
3. **订单项成交分配价 `item.price`**：明确归属于该套系项的成交金额，可等于或偏离公式价；
4. **订单初始报价 `order.quoted_price`**：建单时全部初始订单项报价之和，建单后永久不变；
5. **订单当前公式价 `order.calculated_price`**：当前有效订单项公式价之和；
6. **订单成交价 `order.price`**：实际交易总额和全部既有支付/营收下游的唯一权威，可与订单项成交分配价之和存在显式、可解释的整单未分摊调整。

系统必须分别解释：订单项范围变化、订单项人工偏离、整单未分摊调整和相对初始报价的总变化；不得把一笔多套系订单的总价重复归因到每个套系，也不得用当前套系数据编造历史报价。

## 创作空间重设计前置阻断

`.codestable/epics/creative-workspace-redesign.md` 已把“创作与计价硬解耦”列为先导产品原则。为防止本 Epic 在新产品边界冻结前继续固化旧策划 evaluator，所有策划相关内容当前都是**不可执行候选**：范围中的策划调价/精修划界，验收标准 9–11/18，DEC-9/10，DEC-14 的 0038 planner 部分，ITEM-3 验收中的 `DEC-10` planner 回归，ITEM-4，ITEM-5 对 ITEM-4 的依赖，ITEM-6 中 planner rebaseline / 策划精修分配 UI，ITEM-7 的 AC18/rebaseline 内容，整体验收主链与遗留风险中的 planner 表述，以及最终交付索引中的 `shootplanning/business`、planner 契约和迁移 0038。

这些部分只有在创作空间 Epic ITEM-1B（B1 段）同时更新两份 Epic，并对上列每项作出 `删除 | legacy-read-only | 由订单端替代` 的版本绑定裁决后，才可重新进行 design review；当前文本仅作为冲突清单保留，不构成实施授权。该裁决只处置旧策划与计价的既有耦合，**不以创作空间 Epic 的 `prototype-shape-go` 为前置条件**，因此本 Epic 的解冻不需要等待创作先导证据。套系 SKU、订单项、多套系建单和不依赖策划的订单聚合仍可独立讨论，但不得创建 `order_plan_price_contributions`、absolute owner、rebaseline、新 business draft 或任何从创作域写订单价格的能力。

## 范围

- 套系计价参数：`pricing_unit`、`unit_price`、`min_qty`、`retouch_pricing_mode`，以及按模式出现的 `included_retouch_count`、`retouch_unit_price`；旧 `duration_minutes` / `shot_count_max` / `raw_delivery_count` 保留为交付或展示参数。
- 最小订单项模型：一笔订单至少一个有效订单项；正常新业务可含多个不同套系项；订单项持有套系引用、顺序、归因与计价快照、数量、三层金额和移除审计状态。
- 套系公式：`session`、`hour`、`photo` 三种主计量加一条可选精修加价，服务端为唯一计价权威。
- 订单聚合：初始报价、当前公式价、成交价、订单项成交分配小计和整单未分摊调整；既有 `orders.price` 保持权威。
- 正常建单、增加/修改/移除订单项、改量、订单项改价、整单改价、策划调价、状态推进、客户 merge 的完整价格状态转换与并发边界。
- 创作策划划界：按全部有效订单项判断精修是否由套系完整接管；无法把订单级精修事实可靠分配到多个订单项时显式要求人工分配，不自动猜测。
- 排期预填：只有恰好一个有效小时计价项时，才可用该项 `qty`（分钟）建议新档期时长；多小时项不自动相加。
- 下游适配：套系引用保护与经营摘要、订单/档期套系摘要、dashboard 渠道×类型归因、提醒引擎和数据导出。
- 存量套系预检与迁移、存量/补录订单未知基准策略、订单 API items-v2 单次切换、导出 `schema_version` bump、roadmap / CONTEXT / OpenAPI / API 文档回写。

## 非目标

- 不做 SKU 变体矩阵、库存、税费、优惠券、退款流水、分期、发票或通用商品目录。
- 不给订单项建立独立状态机、收款、档期或交付时间；需要分别取消、结算、排期或交付的服务应拆为不同订单。
- 不允许同一订单存在两个同时有效且引用同一 `package_id` 的订单项；同套系追加数量更新既有项，避免事实分配歧义。已提交移除后再次加入会建立新 ID 与新快照项，不把旧项改回有效。
- 不把创作策划 evaluator 提取为独立计价域，也不把策划“镜头数”改义为“照片张数”。
- 不为一口价套系做结构化超时计费；临场加价继续走订单总价调整。
- 不自动同步已有档期起止时间；排期仍以 `ScheduleSlot.start_at/end_at` 为事实权威。
- 不改变 DEC-10 支付事实推定：仅改价格不自动重算 `outstanding_amount`，已结清订单仍钉死为 0。
- 不自动把整单优惠或策划加价按比例摊到套系项；没有用户表达的分摊就是整单未分摊调整，经营分析必须如实单列。
- 不为历史补录或存量订单按套系当前价格编造历史报价；未知报价基准明确保留为 NULL。
- 不做结构化议价原因枚举；朋友价、补差等原因首版仍记录在订单/订单项备注，策划加项由 append-only 调价记录解释。

## 验收标准

1. `session` / `hour` / `photo` 的参数组合由前后端和数据库共同约束：所有 `qty/quoted_qty_snapshot/retouch_qty/quoted_retouch_qty_snapshot≥0`；`session.min_qty=1` 且有基准的 session 订单项 `qty=quoted_qty_snapshot=1`；`hour.min_qty` 按分钟且 ≥1、`unit_price` 为每小时分价；`photo.min_qty` 按张且 ≥1。所有非 NULL 的 `item.price/item.quoted_price/item.calculated_price` 与套系单价均在 `[0, INTEGER_MAX]`，只有派生的整单未分摊调整可为负。非法 POST/PATCH/backfill 均 400，数据库 CHECK 与锁定后领域校验同守该范围。
2. 服务端公式覆盖三主单位、保底、精修、零价、分钟换算、一次向上取整和溢出拒绝；Go 与前端预览消费同一组语言无关 golden cases，逐例一致。
3. `creation_mode=new` 使用 canonical `items` 数组且至少一项：套系订单允许 1–N 个不同 active 套系；每项省略 `qty` 时取自身 `min_qty`、省略 `retouch_qty` 时取自身包含精修数或 0，服务端锁读各套系并生成独立快照。相同 `package_id` 重复出现返回 400；任何套系在锁读时已下架、跨账号或不存在，整笔事务失败且不留下半张订单。
4. 正常套系订单项拥有完整计算基准且 `item.price` 非空；省略项成交价时等于项公式价，显式项成交价时保留。只有全部有效项 `price` 非空时 `allocated_items_price` 才等于其总和，否则为 NULL。省略订单 `price` 时必须能得到该小计并以其为整单成交价；显式订单 `price` 时保留，并仅在小计可知时派生 `unallocated_price_adjustment = order.price - allocated_items_price`，不静默改写各项金额。任何项、聚合或总价超出 PostgreSQL `INTEGER` 范围或使订单总价为负都返回 400。
5. 订单项报价快照、归因快照、初始计价数与 `item.quoted_price` 写入后不可变；`pricing_unit_snapshot` 非空时核心参数快照、两个初始计价数、两个当前计价数、三层项金额与 `retouch_pricing_mode_snapshot` 必须完整。精修模式只允许 `none|included_no_overage|included_with_overage`：`none` 要求包含数/加价单价均 NULL；`included_no_overage` 要求包含数非负且单价 NULL；`included_with_overage` 要求两者均非负非空。套系后改、客户 merge、状态推进、策划调价均不得重算快照。
6. 订单创建后通过 `POST /orders/{order_id}/items`、`PATCH /orders/{order_id}/items/{item_id}`、`DELETE /orders/{order_id}/items/{item_id}` 在同一订单锁事务中增加、更新或软移除订单项；至少保留一个有效项，且同一套系最多一个有效项。新增项按加入时 active 套系建立新快照；移除保留历史行与快照但不再参与当前聚合；closed/cancelled 禁止增删、改量或再次加入订单项，仅沿用既有终态订单总价/标题/备注/时间戳修正通道。
7. 修改项 `qty/retouch_qty` 总会重算 `item.calculated_price`：若改前 `item.price==item.calculated_price`，项成交分配价跟随；若已偏离则保留。新增、改量、项改价或移除时，若改前订单项成交小计与订单总价均可知，必须保持原整单未分摊调整；若据此得到负总价则拒绝并要求显式给合法订单总价。同一请求显式给订单总价时以该值最终优先。所有入口共用一个锁内领域函数。
8. `item.price_override` 在有计算基准时派生为 `item.price != item.calculated_price`，无基准时为 NULL；`order.price_override` 仅在全部有效项都有计算基准且订单总价非空时派生为 `order.price != order.calculated_price`，否则为 NULL。布尔值均不落库。
9. 策划为每个 `order_id+plan_id` 维护至多一个 active planner contribution。每次评估产出 `planner_delta` 与 `planner_effect_key = hash(order_id, plan_id, business_facts_fingerprint, rule_version, calculation_mode, absolute_target_presence_and_value, planner_epoch)`。规则增量模式以全基准订单的 `order.calculated_price`、或无完整基准订单扣除该 plan 旧 contribution 后的订单价计算 `new_delta`；apply 原子执行 `after_price = before_price - previous_plan_delta + new_delta`。absolute 模式表示排他的最终订单目标价：确认 `replace_order_total` 后，先终止其他全部 active contributions 并递增其 epoch，再令 `new_delta = absolute_target - (before_price - Σ被终止 delta - 本 plan previous_delta)`，写入订单唯一 `absolute_price_owner_plan_id`，最终 `after_price=absolute_target`。absolute owner 存在时，其他 plan 不得 apply；必须显式确认 `replace_absolute_target`，终止 absolute contribution、清 owner、以当前总价 rebaseline 后才能写入新贡献。所有 delta 使用 checked `int64` 计算并持久化为范围 `[-INTEGER_MAX, INTEGER_MAX]` 的 BIGINT，最终订单价仍须在 `[0, INTEGER_MAX]`。同 effect key 重放返回原 2xx/no-material-change。手工整单改价若存在 active contribution，同样必须确认 `replace_planner_contributions` 并终止旧贡献、清 absolute owner、递增 epoch。
10. `extra_shot` 永不因按张套系消失。只有每个有效订单项都有完整基准且 `retouch_pricing_mode_snapshot != none` 时，套系才完整接管精修并抑制账号级金额行：恰好一个接管项时比较其 `retouch_qty`；多个接管项时比较策划总精修事实与这些项 `retouch_qty` 之和。任何无套系、未知基准、`none` 项、数量不一致或事实未知都返回 `package_retouch_allocation_required`，不生成自动精修金额，UI 展示逐项分配/手工整单调整入口；未解决却继续应用其他行必须确认，apply 不暗中修改任何项。所有有效项均为 `none` 时保持账号级规则。
11. 策划草稿 order target fingerprint 升版并按 item ID 的 canonical 顺序覆盖订单 ID、customer ID、status、revision、总价/聚合、全部订单项 ID/`removed_at`、套系引用、`qty`、`retouch_qty`、项公式价/成交价、`retouch_pricing_mode_snapshot`、包含精修数/精修单价快照及 contribution state。纯展示顺序不入 fingerprint；新增、移除、再次加入、改量或只改 `retouch_qty` 即使未改变总金额也必须使旧草稿 stale。订单任意写、状态推进、客户 merge 与 contribution state 变化都递增 revision；apply 锁内再次拒绝 closed/cancelled。
12. 新建档期关联订单时，仅当恰好一个有效项为 `hour` 才用其分钟数量预填时长；没有小时项或存在多个小时项时要求用户显式填写，并展示各项时长作为参考但不自动求和。保存后订单项变化不自动修改已有 slot。
13. 迁移前预检列出 `per_photo` 且 `shot_count_min IS NULL OR shot_count_min < 1` 的套系并阻止迁移；合法数据中 `per_photo` 精确迁移，`fixed/per_duration` 迁为 `session` 保持基础报价恒等。旧 `retouch_count` 从未表达计价接管，统一把新模式设为 `none`，原值保存在 `legacy_retouch_count` 只读迁移字段和待确认清单；只有用户在新 UI 明确选择接管模式并确认包含数后，未来订单项才快照新模式，迁移本身不得改变账号级 `extra_retouch` 行为。每笔存量订单生成一个 legacy 订单项，原 `package_id`、`shoot_type_snapshot` 与可归属的原 `price` 原样搬入该项，`package_name_snapshot` 保持 NULL，全部公式基准与报价字段为 NULL，订单 `price` 一字不改。
14. `creation_mode=backfill` 可记录一个或多个 active/archived 套系引用，但不快照套系当前计价参数、不自动计价；各项计算字段必须为 NULL，非空项成交分配价仍必须非负。若所有有效项都显式提供成交分配价而省略订单总价，服务端以其 checked sum 写入 `order.price`；若至少一项金额未知，省略订单总价则保持 `order.price=NULL`，显式合法总价则保存，系统不得自动平均、按当前价格分摊或把未知当 0。正常无套系订单继续支持自由总价，并使用一个无基准项承载显示与导出。
15. 订单级 `channel_snapshot` 保持不可变；`shoot_type_snapshot` 与 `package_name_snapshot` 下沉到订单项。客户总金额、在途、待收和收款仍读 `orders.price`。套系摘要按非取消订单的有效项返回 distinct order 数、有效项数、未知基准项数、已分配成交均价及 `Σ(item.price-item.calculated_price)/Σ(item.calculated_price)`，分母为 0 时为 NULL，整单调整不归入任一套系。dashboard 渠道×类型的每格订单数为 `COUNT(DISTINCT order_id)`、金额按项成交分配价聚合；只有订单总价非空、全部有效项价格已分配且未分摊调整可计算时才进入类型项与调整桶，并逐订单满足“类型项金额 + 调整桶 = orders.price”。价格非空但分配不完整的订单整笔进入带金额的“未分配订单”桶；`order.price=NULL` 只进入独立的未定价计数/列表，不进入任何货币合计，也不当作 0。提醒引擎的多类型最新订单采用相关类型阈值最大值，无类型时用 `other`；回访标题优先订单标题，否则展示套系摘要。
16. `POST /orders` 以 `items` 为唯一 canonical shape，Order 响应带每次项或价格写均递增的聚合 `revision`。所有订单读写请求必须携带 `X-Order-Shape-Version: 2`；缺失或不匹配返回 426 `order_shape_upgrade_required`，旧请求字段 `package_id` 另返回 400 `legacy_order_shape_unsupported`。旧响应字段 `package_id/package_name/shoot_type_snapshot` 在维护窗口切换后移除，因此旧客户端不能把多套系静默误读成无套系。所有订单项与价格写请求携带 `expected_order_revision`；服务端锁订单后不匹配则返回 409 `order_revision_conflict` 及当前 revision。POST order/item 与 item PATCH/DELETE、整单价格 PATCH 均接受 `Idempotency-Key`，第一方 UI 必传；typed operation + account + key 唯一，同 canonical request 重放原 2xx，不同请求返回 409 `idempotency_conflict`。初始多套系锁按 package ID 全序；订单项写先锁 order，再锁单个 package。订单列表和档期使用批量 `package_summary`，不得 N+1。导出 `schema_version` 统一升 5→6并新增 `order_items`；前后端同一维护窗口切换，不支持旧客户端或新旧二进制混跑。
17. `order_items` 带 `account_id`，以 `(account_id, order_id)` 复合外键 `ON DELETE CASCADE` 归属订单，以 nullable `(account_id, package_id)` 复合外键 `ON DELETE RESTRICT` 保护套系历史引用；部分唯一索引 `(account_id, order_id, package_id) WHERE removed_at IS NULL AND package_id IS NOT NULL` 兜底单一有效套系项。迁移验证 SQL、并发双写、响应丢失重放、陈旧 revision、反序 package 输入无死锁和 package 删除/归档边界均通过。
18. 新增 `order_plan_price_contributions` 可变投影，以 `(account_id, order_id, plan_id)` 为主键，保存 `active_effect_key/active_delta/planner_epoch/last_adjustment_id/rebaseline_required`，订单保存 nullable `absolute_price_owner_plan_id` 并以外键保证它指向本订单 active absolute contribution；`order_price_adjustments` 继续只追加 supersede/reset/rebaseline 事件。升级时所有旧策划 draft 直接 stale；发现既有 v1 策划审计的 order+plan 置 `rebaseline_required=true`，不得生成可 apply 新贡献。rebaseline 使用 `POST /orders/{order_id}/plans/{plan_id}/pricing-rebaseline`：请求必须带 `expected_order_revision` 与 `Idempotency-Key`；当前 `order.price` 非空时以它为基线，为 NULL 时必须在同一请求用 `replace_order_total` 提供合法非负基线，否则 400 `price_required_for_rebaseline`；事务写审计、清 flag、`active_delta=0`、递增 epoch/revision，同 canonical request 重放原 2xx。没有旧审计的组合从零 contribution 懒创建。0038 迁移、唯一/外键约束、NULL 基线、响应丢失重放、历史重复收费反例与恢复演练进入门禁。

## 共享语言与概念边界

沿用 `.codestable/requirements/CONTEXT.md` 中账号、客户、套系、订单、档期、镜头等 canonical 术语；ITEM-1 会把已确认的多订单项关系回写 canonical 文档。本 Epic 先冻结以下局部单义定义：

| 术语 | 本 Epic 中的单义定义 | 不包括 / 关键关系 |
|---|---|---|
| 订单 `Order` | 同一客户的一次交易与共同履约容器，拥有一个状态、收款事实、档期关系和成交总价 | 不是单个套系；需要分别取消、收款、排期或交付的服务必须拆单 |
| 订单项 `OrderItem` | 订单内一条有身份、有顺序的购买明细；可引用一个套系，并持有该次购买的快照、数量与成交分配价 | 不是普通 many-to-many join，也没有独立状态/支付/档期；同一订单同一套系最多一个有效项 |
| 有效订单项 | 尚未被软移除、参与当前公式价和成交分配小计的订单项 | 被移除项保留审计和历史引用，不参与当前聚合 |
| 计价单位 `pricing_unit` | `session`（次）、`hour`（按小时展示和定价）、`photo`（最终照片张数） | 有基准时 session 数量恒为 1；hour 数量以分钟存储；photo 不等于策划 Shot |
| 当前计价数 `item.qty` | 某订单项当前约定并用于计价的主数量；hour/photo 可修正，session 恒为 1 | 不宣称是不可变的物理交付事实；不直接作为档期事实 |
| 精修计价数 `item.retouch_qty` | 某订单项当前约定并用于该项精修加价判断的张数 | 多项之和可与策划总精修事实核对，但策划不会自动决定项间分配 |
| 精修计价模式 | `none`（套系不接管）、`included_no_overage`（包含但不自动计算超量价）、`included_with_overage`（包含且按单价计算超量） | 只有后两者表示接管；不再用 nullable 单价猜测是否接管 |
| 订单项初始报价 | `item.quoted_price`，按该项加入时快照与初始计价数计算，写入后不可变 | 不等于订单建单时总报价；后加项也有自己的项初始报价 |
| 订单初始报价 | `order.quoted_price`，仅取建单事务中的初始项报价之和，写入后不可变 | 后加/移除订单项不改写它；当前范围看 `order.calculated_price` |
| 当前公式价 | 项级由快照与当前数量计算；订单级仅在全部有效项有基准时等于各项公式价之和 | 不含项人工偏离、整单调整或策划加项 |
| 成交分配价 `item.price` | 用户明确归属于某套系项的成交金额 | 不等于已收款；不得自动吸收整单优惠或策划加项 |
| 成交总价 `order.price` | 一笔订单实际交易总额及支付/营收下游唯一权威 | 可偏离项成交分配小计；不自动改支付事实 |
| 订单项成交分配小计 | `allocated_items_price`；仅当每个有效项 `price` 都非空时等于其总和，否则为 NULL | 不把未知项当 0，不是订单成交总价 |
| 整单未分摊调整 | `order.price - allocated_items_price`，仅在两侧均可知时派生 | 不自动分摊给套系；包含整单优惠、无法归项的人工调整与策划 apply 结果 |
| 策划贡献 | 某 order+plan 当前生效的可替换 `planner_delta`，由独立 state 投影定位、由 append-only adjustment 记录历史 | 不是从订单当前总价猜出的差额；旧审计未 rebaseline 前没有可安全复用的 active delta |
| absolute 总价所有者 | 当前唯一有权把订单价钉为最终目标值的 plan；存在时其他 plan contribution 不可直接 apply | 不是普通增量贡献；替换它必须显式确认并先终止旧 absolute contribution |
| 套系接管精修 | 有完整基准的有效项，其 `retouch_pricing_mode_snapshot` 为 `included_no_overage` 或 `included_with_overage` | 任何无套系、未知基准或 `none` 项都会使自动接管不完整；永不接管 extra_shot |

边界场景：用户建单时添加“场照精修”项 `qty=2`、项公式/成交分配价 400 元，再添加“双人精修”项 `qty=1`、项公式/成交分配价 300 元。订单初始报价、当前公式价和默认成交总价均为 700 元。摄影师把整单总价改为 650 元而不改各项，则两项仍分别归因 400/300 元，整单未分摊调整为 -50 元；系统不会擅自把 -50 元按比例分到两个套系。后来把场照改为 3 张且该项此前未偏离，该项公式/成交分配价随之变化，订单总价按相同差额变化并继续保留 -50 元整单调整。

## 关键决策

- **DEC-1 · 多套系建模为订单项而非 ID 数组**：`order_items` 是带快照、数量、金额和审计状态的领域实体，不是 `orders.package_ids[]` 或无属性 join。订单继续是交易与履约聚合根，所有项写入先锁订单并在同一事务更新聚合；项公式事实是 source of truth，`orders.calculated_price` 是同事务维护的聚合投影，禁止绕过订单域直接写 item，真库并发测试负责钉住不漂移。

- **DEC-2 · 统一参数形状、公式明确分支**：

  ```text
  billable_qty = max(item.qty, min_qty_snapshot)

  primary_amount =
    session: unit_price_snapshot
    hour:   ceil(unit_price_snapshot × billable_qty_minutes / 60)
    photo:  unit_price_snapshot × billable_qty_photos

  retouch_amount =
    mode in {none, included_no_overage}: 0
    mode = included_with_overage:
      retouch_unit_price_snapshot × max(item.retouch_qty - included_retouch_count_snapshot, 0)

  item.calculated_price = checked(primary_amount + retouch_amount)
  ```

  金额以分存储，使用 `int64` 中间值，末尾一次舍入；超出 PostgreSQL `INTEGER` 范围返回 validation error，不截断、不饱和。

- **DEC-3 · 订单项数量可修正但不是物理交付事实**：正常加项默认取套系起订量以保持直接保存；`0 <= qty < min_qty` 合法，公式只向上取保底，不改写用户输入；`retouch_qty` 同样不得为负。有基准的 session 项数量恒为 1。所有持久金额非负，负数只存在于派生的整单未分摊调整。

- **DEC-4 · 项级归因与整单权威并存**：项 `price` 回答“这部分成交金额属于哪个套系”，订单 `price` 回答“客户这笔交易总共多少钱”。默认总价取项成交分配小计；用户显式整单改价不会反向猜分摊，差额成为可见的整单未分摊调整。客户金额、支付、在途和营收继续只认订单总价。

- **DEC-5 · 订单初始报价不可变，后续范围由当前公式价解释**：建单时 `order.quoted_price=Σ初始 item.quoted_price`；后续加项、移除或改量不重写该值。每个后加项仍保留自己的不可变项报价，因此既可回答原始整单报价，也可追踪何时加入了什么范围。

- **DEC-6 · 改项时同时保留项偏离与整单调整**：锁内先计算旧项成交小计与旧整单未分摊调整，再应用项变化。项未偏离时成交分配价跟随新公式价，已偏离时保留；随后以新项小计加旧整单调整得到新订单总价。显式项价覆盖该项结果，显式订单总价最后覆盖整单结果。未知历史无法计算差额时不自动动订单总价。

- **DEC-7 · 快照即项报价版本，未知历史保持未知**：正常套系项在锁读 active 套系的同一事务写完整快照；套系后改不影响订单项。存量、backfill 与无套系项可以有引用或成交事实但没有可验证公式基准，相关计算字段整组 NULL；不建立套系版本表，也不拿当前套系值回填历史。

- **DEC-8 · 移除是审计状态，不是删除历史**：订单项使用 `removed_at`（或等价单义状态）退出当前聚合；套系引用保护查看全部历史订单项，套系经营当前摘要只看有效项。再次加入已移除套系创建新 item ID 和新快照，不复活旧快照。

- **DEC-9 · 策划贡献按身份替换，absolute target 排他拥有最终总价**：独立 contribution state 保存 active delta/epoch/最后审计，append-only adjustment 保存 `effect_key/delta/supersedes` 事件；同 effect key 由数据库唯一约束保证并发重放幂等。普通规则增量以 `new_delta-old_delta` 替换该 plan 旧贡献。absolute apply 原子终止其他 active contributions、写唯一 absolute owner 并把最终订单价钉到 target；其他 plan 后续 apply 必须确认 `replace_absolute_target`，先终止它并以当前成交价 rebaseline，因而不能静默破坏目标。手工整单改价确认后重置全部贡献/owner 并递增 epoch。旧 v1 审计不反推 delta，必须人工 rebaseline；NULL 总价必须在同一幂等命令提供合法基线。任何 apply 都不修改或自动分配 item.price。

- **DEC-10 · 多项精修必须证明覆盖完整**：每个有效订单项都有完整基准且显式模式为 `included_no_overage|included_with_overage` 时，才抑制账号级 `extra_retouch`；多项用 `Σitem.retouch_qty` 与策划总精修事实核对。无套系/未知/`none` 混入、数量不一致或事实未知都不自动猜分配，也不生成可能重复收费的精修金额；返回专用告警与逐项编辑/手工整单调整入口，用户可确认后只应用其他明确行。全部有效项均为 `none` 时按账号级规则计价；`extra_shot` 永远保持既有规则。

- **DEC-11 · 支付事实保持既有 DEC-10**：任何自动或手工项价/总价变化都不单独触发待收金额推定。价格变化后前端提示复核收款，已结清订单 `outstanding_amount=0` 保持不变。

- **DEC-12 · 小时项只在单义时预填档期**：恰好一个有效小时项才自动建议时长；多个小时项可能并行、重叠或属于不同内容，系统不能假定顺序相加。档期保存后与订单项独立演进。

- **DEC-13 · 类型归因属于订单项**：`channel_snapshot` 仍属于订单，`shoot_type_snapshot` 下沉到项。dashboard 类型金额只消费项成交分配价，整单未分摊调整进入独立桶；提醒引擎面对最新多类型订单使用最大流失阈值，优先避免过早误报。

- **DEC-14 · 迁移为 forward-only 聚合与 API items-v2 切换**：0036 迁套系参数并把旧精修能力默认隔离为 `none`，0037 建 `order_items` 并迁 legacy 项，0038 建 planner contribution state 与历史 rebaseline fence，再删除订单级 `package_id/shoot_type_snapshot` 权威与旧 API shape。生产采用单实例维护窗口：停写、备份、预检、迁移、同步部署前后端、烟测；不支持旧客户端或新旧二进制混跑，回退依赖备份恢复。

## 子项契约

- **ITEM-1 · 语义权威、API 形状与公式样例**（owning: `cs-roadmap update`）
  - 可交付：先回写 roadmap §4.2/§4.3 与 CONTEXT 的 Order/OrderItem/Package、项级三层金额、订单聚合、归因和正常/backfill 分支；冻结 canonical `items` 请求/响应与语言无关 pricing golden cases。
  - 依赖：none。
  - 验收要点：订单/订单项、项成交分配/整单调整、镜头/照片、计价数/档期时长全文单义；样例至少覆盖“场照 2 + 双人 1”、同套系重复拒绝、整单 -50 调整、项改量后保留整单调整和未知历史；后续代码不得领先权威源。

- **ITEM-2 · 套系计价模型与迁移**（owning: `cs-feat`）
  - 可交付：迁移 0036、package 模型/服务/repository、OpenAPI 与生成类型；预检非法 `per_photo`；完成旧字段迁移、最终状态校验、数据库 CHECK 和服务端纯公式。
  - 依赖：ITEM-1。
  - 验收要点：验收标准 1、2、13；PATCH 锁行后校验最终组合；迁移前后报价恒等证据与维护窗口回退说明齐全。

- **ITEM-3 · 多订单项、报价基准与价格状态机**（owning: `cs-feat`）
  - 可交付：迁移 0037 与 `order_items`；旧订单单 legacy 项迁移；items-v2 订单创建/读取/项增改移 API；项快照与三层金额；订单聚合、revision、幂等、派生 flags 和锁内 DEC-6 状态机。
  - 依赖：ITEM-1、ITEM-2。
  - 验收要点：验收标准 3–8、13、14、16、17；多套系创建原子性、账号隔离、数量/金额上下界、相同套系部分唯一索引、软移除/重加新快照、至少一项、revision 冲突、所有项写幂等重放、package 锁全序、终态限制、legacy/backfill NULL 组与金额求和、客户 merge、复合 FK/套系删除保护和 DEC-10 回归均有真库用例。

- **ITEM-4 · 创作策划计价划界与陈旧性**（owning: `cs-feat`）
  - 状态：被 `creative-workspace-redesign` ITEM-1B（B1 段）阻断；当前内容仅作待裁决冲突清单，不可实施。
  - 可交付：0038 contribution state、absolute owner 与 v1 rebaseline fence；order target/fingerprint 升版并携带 canonical 项集合与 active contributions；装配层选择聚合/扣旧贡献 base；evaluator 增显式精修模式与完整/部分接管上下文；business adjustment adapter 实现普通增量替换、排他 absolute、effect key、epoch、rebaseline 与手工总价 reset acknowledgement。
  - 依赖：ITEM-3。
  - 验收要点：验收标准 9–11、18；覆盖单项、多项全部接管、无套系/未知/none 混合、零加价接管、数量不一致、只改 retouch_qty stale、无基准同事实重复 apply、事实/规则变化替换旧 delta、不同 absolute target、已有人工/其他 plan contribution 下 absolute 最终值、absolute 后其他 plan 未确认拒绝/确认后替换、delta 正负边界、手工总价 reset 后重应用、旧 v1 审计 rebaseline、NULL 基线、rebaseline 响应丢失重放、旧 draft stale、生成后取消/关闭/merge、并发同 effect key；任何 apply 都不改 item 行。

- **ITEM-5 · 套系、dashboard、提醒与档期读模型适配**（owning: `cs-feat`）
  - 可交付：Package pricing summary；订单/档期批量 `package_summary`；dashboard 渠道×类型项级归因与整单调整桶；提醒引擎多项标题和最大 churn threshold；小时项预填判定。
  - 依赖：ITEM-3、ITEM-4。
  - 验收要点：验收标准 12、15–17；无 N+1；每格 `COUNT(DISTINCT order_id)` 不因多项重复；完整分配订单逐单对账、部分未知订单只进未分配桶；零项不可出现；removed/cancelled/未知基准/跨账号/多小时项均有真库或领域用例。

- **ITEM-6 · 前端套系、订单项与排期体验**（owning: `cs-feat`）
  - 可交付：`PackagesPage` 单位化表单、legacy 精修待确认清单和经营摘要；`OrderWorkspace` / `ShootOrderFlow` 多项编辑器、逐项数量/公式价/成交分配价、整单小计/调整/总价、已提交移除后的重新加入提示和收款复核；小时档期预填。原 planner rebaseline / 策划精修分配 UI 被 `creative-workspace-redesign` ITEM-1B（B1 段）阻断，未经双 Epic 裁决不得实施。
  - 依赖：ITEM-2、ITEM-3、ITEM-4、ITEM-5。
  - 验收要点：建单添加“场照精修 2 + 双人精修 1”无需离开表单；服务端响应覆盖前端预览；相同套系引导改量；整单改价不伪装成项分摊；所有项/价格写携带 revision 与 Idempotency-Key，冲突保留草稿并提供重载；375px、200% zoom、键盘、错误保留和结果未知恢复通过；API 类型只引用 `schema.d.ts`。

- **ITEM-7 · 导出、文档与整体验收**（owning: `cs-roadmap update`）
  - 可交付：导出 `order_items` 与全部新增字段、`schema_version` 5→6；回写 roadmap §4.6/§5/§8、CONTEXT、items.yaml、`docs/api/` 和维护窗口说明；核对 OpenAPI/生成物/实现一致。
  - 依赖：ITEM-2、ITEM-3、ITEM-4、ITEM-5、ITEM-6。
  - 验收要点：验收标准 16–18；同一 schema version 对应唯一 shape；旧 shape 请求明确失败且不会误读多套系；旧 retouch_count 上线前后不改变账号级计价，只有用户确认后新订单生效；迁移/rebaseline/恢复演练、分层门禁与最终 `make check` 证据完整。

## 最终交付索引

- 语义权威：roadmap §4.2/§4.3/§4.6/§5/§8、`.codestable/requirements/CONTEXT.md`。
- 后端：`backend/internal/package/`、`backend/internal/order/`、`backend/internal/shootplanning/business/`、`backend/internal/dashboard/`、`backend/internal/reminder/`、`backend/internal/schedule/`、迁移 0036/0037/0038。
- 契约：`api/openapi.yaml`、生成的 Go/TS 类型、语言无关 pricing golden cases。
- 前端：`PackagesPage.tsx`、`OrderWorkspace.tsx`、`ShootOrderFlow.tsx` / `ScheduleSlotDialog.tsx`、dashboard 与日历套系摘要。
- 运维与文档：data export schema v6、`docs/api/`、维护窗口迁移/备份/恢复说明。

## 整体验收

以验收标准 1–18、各 ITEM 验收证据、迁移预检与维护窗口演练、OpenAPI 零漂移、分层门禁和最终 `make check` 为自动化门槛。另由 owner 进行桌面与 375px 真浏览器主链验收：确认旧精修套系 → 建两个套系 → 一单添加多个套系项 → 逐项改量/改价 → 整单优惠且不自动分摊 → 策划 rebaseline/精修分配/增量与 absolute 调价 → revision 冲突恢复 → 单/多小时项排期 → 收款复核 → 套系/dashboard 对账 → 导出恢复。最终由 fresh acceptance reviewer 核对批准版本与实现，review 通过不代替 owner 接受。

## 遗留风险

1. **录入成本上升**：多项编辑比单选套系复杂；UI 必须默认一项、支持快速追加，并持续显示逐项小计与总价，不能让用户在多个折叠层中找金额。
2. **整单调整无法精确归因**：不自动分摊是为了避免伪造套系成交事实；套系分析只能消费明确的项成交分配价，整单调整需单列，用户若要精确归因必须逐项改价。
3. **历史多套系分配未知**：存量订单只能精确迁成原单项；backfill 可记录多个套系引用，但若没有逐项金额就不能进入套系成交均价或偏离率。
4. **精修事实仍是订单级**：多个套系项的精修分配依赖人工表达；首版只做总量核对和显式分配，不创造照片资产与自动归属模型。旧套系默认 `none` 会在 owner 确认前保留账号级计价，安全但不会立即享受套系接管。
5. **存量时长套系降级**：`per_duration` 为保报价恒等迁成 `session`，owner 需按迁移清单手工决定哪些套系改为 `hour` 及其每小时价格。
6. **两层计价能力并存**：套系项公式和策划 evaluator 靠明确 base、接管判定与 fingerprint 保持一致；新增重叠维度时必须同时修改划界契约。
7. **支付事实可能滞后于成交价**：这是沿用 DEC-10 的有意行为；UI 提示只能降低漏收风险，未来需引入 outstanding 来源状态才能安全自动联动。
8. **forward-only 迁移依赖运维纪律**：维护窗口若缺少备份、预检或烟测，删除旧订单级套系权威后的回退只能恢复备份。
9. **API 切换需要停机协同**：为避免旧客户端把多套系误读成无套系，本 Epic 选择一次性移除旧订单 shape；维护窗口必须同步部署前后端并在开放写入前完成 items-v2 烟测。
10. **旧策划单需要一次 rebaseline**：安全 fence 拒绝猜测旧 adjustment 的活跃贡献，会让有历史策划调价的订单首次生成新版草稿前多一步确认；这是避免重复收费的有意成本。
