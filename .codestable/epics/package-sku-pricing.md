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

创作策划域曾以 [evaluator.go](../../backend/internal/shootplanning/business/evaluator.go) 的 `AdjustmentLine` / `RuleProfile` 与 append-only `order_price_adjustments` 记录售中加项，其 target 只有单一 `package_id` 和订单总价。`creative-workspace-redesign` ITEM-1B（B1 段，2026-09-04）已裁决：策划不再是订单价格的 writer，旧调价历史作为只读审计保留；本 Epic 因此只处理订单端计价，不再为策划域建任何计价能力。

因此本 Epic 的任务不是重建通用电商，而是：把套系升级为可计算 SKU；让一笔订单包含多个有业务身份的订单项；把套系快照、数量与可归因成交金额放在订单项上；让订单总价、支付事实、档期和下游经营口径继续闭合。

## 目标

用户可以在一笔订单中选择一个或多个不同套系，每个套系独立录入数量、精修数量和成交分配金额，并共享同一订单的客户、状态、档期、收款与交付生命周期。

计价形成“订单项三层金额 + 订单聚合”两级模型：

1. **订单项初始报价 `item.quoted_price`**：该订单项加入订单时按套系快照与初始计价数算出的标准价，永久不变；
2. **订单项当前公式价 `item.calculated_price`**：按同一套快照与该项当前计价数重算的标准价；
3. **订单项成交分配价 `item.price`**：明确归属于该套系项的成交金额，可等于或偏离公式价；
4. **订单初始报价 `order.quoted_price`**：建单时全部初始订单项报价之和，建单后永久不变；
5. **订单当前公式价 `order.calculated_price`**：当前有效订单项公式价之和；
6. **订单成交价 `order.price`**：实际交易总额和全部既有支付/营收下游的唯一权威，可与订单项成交分配价之和存在显式、可解释的整单未分摊调整；订单端是它的唯一 writer。

系统必须分别解释：订单项范围变化、订单项人工偏离、整单未分摊调整和相对初始报价的总变化；不得把一笔多套系订单的总价重复归因到每个套系，也不得用当前套系数据编造历史报价。

## 创作空间重设计 barrier（已裁决）

`.codestable/epics/creative-workspace-redesign.md` 把“创作与计价硬解耦”列为先导产品原则。其 ITEM-1B（B1 段）已于 2026-09-04 对本 Epic 全部策划相关条目作出版本绑定裁决，记录见 `.codestable/work/feat-legacy-planner-barrier.md` §2；本文档按该裁决修订，被删除的条目以“已裁决删除”占位保留编号，不重新编号。裁决要点：

- 策划域不再是订单价格的 writer：不建 `order_plan_price_contributions`、absolute owner、rebaseline、planner effect key / epoch，不建迁移 0038。
- 精修计价只依赖订单项自身字段（`retouch_qty` 与快照模式），不与策划事实核对，不存在“套系接管精修”概念；账号级 `extra_retouch` / `extra_shot` 规则不再参与任何订单计价。
- 旧 `order_adjustment` 草稿的 generate / apply 在本 Epic ITEM-3 删除 `orders.package_id` 时对全部账号退役。技术事实只有一条：三处 SQL 硬读该列，删列必须改代码且无法只对 pilot 关闭；「整体退役」而非「置 nil 继续算价」是 DEC-8 的产品裁决（理由见 barrier 文件 §6）。`dismiss` 与已应用 `order_price_adjustments` 读面保留。`schedule_duration` 草稿与 business facts 对非 pilot 账号保持既有行为——前提是 ITEM-3 一并改掉共用的 `LoadCurrentTargets` SELECT——直至旧兼容面退役另行裁决。
- pilot 账号在创作 Epic ITEM-2 起对全部旧策划写面 `legacy_read_only`，与本 Epic 无关。

本 Epic 的套系 SKU、订单项、多套系建单与订单聚合部分仍为 proposed，等待自身 lineage 的增量 design review 与 owner 确认。

## 范围

- 套系计价参数：`pricing_unit`、`unit_price`、`min_qty`、`retouch_pricing_mode`，以及按模式出现的 `included_retouch_count`、`retouch_unit_price`；旧 `duration_minutes` / `shot_count_max` / `raw_delivery_count` 保留为交付或展示参数。
- 最小订单项模型：一笔订单至少一个有效订单项；正常新业务可含多个不同套系项；订单项持有套系引用、顺序、归因与计价快照、数量、三层金额和移除审计状态。
- 套系公式：`session`、`hour`、`photo` 三种主计量加一条可选精修加价，服务端为唯一计价权威。
- 订单聚合：初始报价、当前公式价、成交价、订单项成交分配小计和整单未分摊调整；既有 `orders.price` 保持权威。
- 正常建单、增加/修改/移除订单项、改量、订单项改价、整单改价、状态推进、客户 merge 的完整价格状态转换与并发边界。
- 旧策划调价路径 fail-closed：删除 `orders.package_id` 后，旧 `order_adjustment` 草稿生成/应用明确失败且零写，`dismiss` 与已应用调整读面保留。
- 排期预填：只有恰好一个有效小时计价项时，才可用该项 `qty`（分钟）建议新档期时长；多小时项不自动相加。
- 下游适配：套系引用保护与经营摘要、订单/档期套系摘要、dashboard 渠道×类型归因、提醒引擎和数据导出。
- 存量套系预检与迁移、存量/补录订单未知基准策略、订单 API items-v2 单次切换、导出 `schema_version` bump、roadmap / CONTEXT / OpenAPI / API 文档回写。

## 非目标

- 不做 SKU 变体矩阵、库存、税费、优惠券、退款流水、分期、发票或通用商品目录。
- 不给订单项建立独立状态机、收款、档期或交付时间；需要分别取消、结算、排期或交付的服务应拆为不同订单。
- 不允许同一订单存在两个同时有效且引用同一 `package_id` 的订单项；同套系追加数量更新既有项，避免事实分配歧义。已提交移除后再次加入会建立新 ID 与新快照项，不把旧项改回有效。
- 不把创作策划 evaluator 提取为独立计价域，也不把策划“镜头数”改义为“照片张数”；不为 items-v2 订单建任何策划调价能力。
- 不为一口价套系做结构化超时计费；临场加价继续走订单总价调整。
- 不自动同步已有档期起止时间；排期仍以 `ScheduleSlot.start_at/end_at` 为事实权威。
- 不改变 DEC-10 支付事实推定：仅改价格不自动重算 `outstanding_amount`，已结清订单仍钉死为 0。
- 不自动把整单优惠按比例摊到套系项；没有用户表达的分摊就是整单未分摊调整，经营分析必须如实单列。
- 不为历史补录或存量订单按套系当前价格编造历史报价；未知报价基准明确保留为 NULL。
- 不做结构化议价原因枚举；朋友价、补差等原因首版仍记录在订单/订单项备注；历史策划加项由既有 append-only `order_price_adjustments` 只读解释。

## 验收标准

1. `session` / `hour` / `photo` 的参数组合由前后端和数据库共同约束：所有 `qty/quoted_qty_snapshot/retouch_qty/quoted_retouch_qty_snapshot≥0`；`session.min_qty=1` 且有基准的 session 订单项 `qty=quoted_qty_snapshot=1`；`hour.min_qty` 按分钟且 ≥1、`unit_price` 为每小时分价；`photo.min_qty` 按张且 ≥1。所有非 NULL 的 `item.price/item.quoted_price/item.calculated_price` 与套系单价均在 `[0, INTEGER_MAX]`，只有派生的整单未分摊调整可为负。非法 POST/PATCH/backfill 均 400，数据库 CHECK 与锁定后领域校验同守该范围。
2. 服务端公式覆盖三主单位、保底、精修、零价、分钟换算、一次向上取整和溢出拒绝；Go 与前端预览消费同一组语言无关 golden cases，逐例一致。
3. `creation_mode=new` 使用 canonical `items` 数组且至少一项：套系订单允许 1–N 个不同 active 套系；每项省略 `qty` 时取自身 `min_qty`、省略 `retouch_qty` 时取自身包含精修数或 0，服务端锁读各套系并生成独立快照。相同 `package_id` 重复出现返回 400；任何套系在锁读时已下架、跨账号或不存在，整笔事务失败且不留下半张订单。
4. 正常套系订单项拥有完整计算基准且 `item.price` 非空；省略项成交价时等于项公式价，显式项成交价时保留。只有全部有效项 `price` 非空时 `allocated_items_price` 才等于其总和，否则为 NULL。省略订单 `price` 时必须能得到该小计并以其为整单成交价；显式订单 `price` 时保留，并仅在小计可知时派生 `unallocated_price_adjustment = order.price - allocated_items_price`，不静默改写各项金额。任何项、聚合或总价超出 PostgreSQL `INTEGER` 范围或使订单总价为负都返回 400。
5. 订单项报价快照、归因快照、初始计价数与 `item.quoted_price` 写入后不可变；`pricing_unit_snapshot` 非空时核心参数快照、两个初始计价数、两个当前计价数、三层项金额与 `retouch_pricing_mode_snapshot` 必须完整。精修模式只允许 `none|included_no_overage|included_with_overage`：`none` 要求包含数/加价单价均 NULL；`included_no_overage` 要求包含数非负且单价 NULL；`included_with_overage` 要求两者均非负非空。套系后改、客户 merge、状态推进均不得重算快照。
6. 订单创建后通过 `POST /orders/{order_id}/items`、`PATCH /orders/{order_id}/items/{item_id}`、`DELETE /orders/{order_id}/items/{item_id}` 在同一订单锁事务中增加、更新或软移除订单项；至少保留一个有效项，且同一套系最多一个有效项。新增项按加入时 active 套系建立新快照；移除保留历史行与快照但不再参与当前聚合；closed/cancelled 禁止增删、改量或再次加入订单项，仅沿用既有终态订单总价/标题/备注/时间戳修正通道。
7. 修改项 `qty/retouch_qty` 总会重算 `item.calculated_price`：若改前 `item.price==item.calculated_price`，项成交分配价跟随；若已偏离则保留。新增、改量、项改价或移除时，若改前订单项成交小计与订单总价均可知，必须保持原整单未分摊调整；若据此得到负总价则拒绝并要求显式给合法订单总价。同一请求显式给订单总价时以该值最终优先。所有入口共用一个锁内领域函数。
8. `item.price_override` 在有计算基准时派生为 `item.price != item.calculated_price`，无基准时为 NULL；`order.price_override` 仅在全部有效项都有计算基准且订单总价非空时派生为 `order.price != order.calculated_price`，否则为 NULL。布尔值均不落库。
9. （已裁决删除 · ITEM-1B B1 2026-09-04）策划 planner contribution、effect key、absolute owner、epoch、rebaseline 全部不建；订单价格唯一 writer 是订单端。
10. 精修计价只依赖订单项自身字段：`retouch_pricing_mode_snapshot=included_with_overage` 时按 DEC-2 以 `item.retouch_qty` 计算超量价，其他模式精修加价为 0；不与策划 `retouched_photo_count` 核对，不生成任何“接管”告警；账号级 `extra_retouch` / `extra_shot` 规则不参与订单项或订单计价。
11. 旧策划 `order_adjustment` 草稿在 `orders.package_id` 删除后全局 fail-closed，且旧只读面与旧→旧级联不断：generate 对 `order_adjustment` kind 返回 `unavailable(reason=legacy_planner_retired)`（按 kind 部分不可用，`schedule_duration` kind 照常生成），`apply_order_adjustment` 返回 stale（`stale_reason=legacy_planner_retired`）且零写，`dismiss` 仍可；旧「经营草稿」只读详情、非 pilot 账号的 `schedule_duration` 草稿、旧计划的订单取消/删除级联继续可用。代码面：`order/business_adjustment.go`、`shootplanning/business/repository.go`（`LoadCurrentTargets`）、`shootplanning/crm/engine.go`（`loadOrderFact`）三处不再读取 `package_id`；`PATCH /settings` 的 `planning_business_rules` 写入返回 400 `validation_failed` + 中文 message「经营规则已随旧策划计价退役」（沿用现有 settings 错误映射，不新增 code）。两个 `legacy_planner_retired` enum 值是 OpenAPI 契约变更，须改 `api/openapi.yaml`、`make generate` 提交 Go/TS 生成物，并补齐两处前端映射（`businessDraftInput.ts` 穷举 Record 与 `BusinessPanel.tsx` 的 `staleLabel` 静默回落表）；`PlanningBusinessRulesSection.tsx` 同步只读化，settings 拒绝只针对请求体出现 `planning_business_rules` 字段、不影响其他设置项。已应用 `order_price_adjustments` 继续可读且 append-only。
12. 新建档期关联订单时，仅当恰好一个有效项为 `hour` 才用其分钟数量预填时长；没有小时项或存在多个小时项时要求用户显式填写，并展示各项时长作为参考但不自动求和。保存后订单项变化不自动修改已有 slot。
13. 迁移前预检列出 `per_photo` 且 `shot_count_min IS NULL OR shot_count_min < 1` 的套系并阻止迁移；合法数据中 `per_photo` 精确迁移，`fixed/per_duration` 迁为 `session` 保持基础报价恒等。旧 `retouch_count` 从未表达套系精修计价，统一把新模式设为 `none`，原值保存在 `legacy_retouch_count` 只读迁移字段和待确认清单；只有用户在新 UI 明确选择精修模式并确认包含数后，未来订单项才快照新模式；迁移本身不改变非 pilot 账号既有旧策划 `extra_retouch` 行为（该行为在 items-v2 切换时随验收 11 退役）。每笔存量订单生成一个 legacy 订单项，原 `package_id`、`shoot_type_snapshot` 与可归属的原 `price` 原样搬入该项，`package_name_snapshot` 保持 NULL，全部公式基准与报价字段为 NULL，订单 `price` 一字不改。
14. `creation_mode=backfill` 可记录一个或多个 active/archived 套系引用，但不快照套系当前计价参数、不自动计价；各项计算字段必须为 NULL，非空项成交分配价仍必须非负。若所有有效项都显式提供成交分配价而省略订单总价，服务端以其 checked sum 写入 `order.price`；若至少一项金额未知，省略订单总价则保持 `order.price=NULL`，显式合法总价则保存，系统不得自动平均、按当前价格分摊或把未知当 0。正常无套系订单继续支持自由总价，并使用一个无基准项承载显示与导出。
15. 订单级 `channel_snapshot` 保持不可变；`shoot_type_snapshot` 与 `package_name_snapshot` 下沉到订单项。客户总金额、在途、待收和收款仍读 `orders.price`。套系摘要按非取消订单的有效项返回 distinct order 数、有效项数、未知基准项数、已分配成交均价及 `Σ(item.price-item.calculated_price)/Σ(item.calculated_price)`，分母为 0 时为 NULL，整单调整不归入任一套系。dashboard 渠道×类型的每格订单数为 `COUNT(DISTINCT order_id)`、金额按项成交分配价聚合；只有订单总价非空、全部有效项价格已分配且未分摊调整可计算时才进入类型项与调整桶，并逐订单满足“类型项金额 + 调整桶 = orders.price”。价格非空但分配不完整的订单整笔进入带金额的“未分配订单”桶；`order.price=NULL` 只进入独立的未定价计数/列表，不进入任何货币合计，也不当作 0。提醒引擎的多类型最新订单采用相关类型阈值最大值，无类型时用 `other`；回访标题优先订单标题，否则展示套系摘要。
16. `POST /orders` 以 `items` 为唯一 canonical shape，Order 响应带每次项或价格写均递增的聚合 `revision`。所有订单读写请求必须携带 `X-Order-Shape-Version: 2`；缺失或不匹配返回 426 `order_shape_upgrade_required`，旧请求字段 `package_id` 另返回 400 `legacy_order_shape_unsupported`。旧响应字段 `package_id/package_name/shoot_type_snapshot` 在维护窗口切换后移除，因此旧客户端不能把多套系静默误读成无套系。所有订单项与价格写请求携带 `expected_order_revision`；服务端锁订单后不匹配则返回 409 `order_revision_conflict` 及当前 revision。POST order/item 与 item PATCH/DELETE、整单价格 PATCH 均接受 `Idempotency-Key`，第一方 UI 必传；typed operation + account + key 唯一，同 canonical request 重放原 2xx，不同请求返回 409 `idempotency_conflict`。初始多套系锁按 package ID 全序；订单项写先锁 order，再锁单个 package。订单列表和档期使用批量 `package_summary`，不得 N+1。导出 `schema_version` 统一升 5→6并新增 `order_items`；前后端同一维护窗口切换，不支持旧客户端或新旧二进制混跑。
17. `order_items` 带 `account_id`，以 `(account_id, order_id)` 复合外键 `ON DELETE CASCADE` 归属订单，以 nullable `(account_id, package_id)` 复合外键 `ON DELETE RESTRICT` 保护套系历史引用；部分唯一索引 `(account_id, order_id, package_id) WHERE removed_at IS NULL AND package_id IS NOT NULL` 兜底单一有效套系项。迁移验证 SQL、并发双写、响应丢失重放、陈旧 revision、反序 package 输入无死锁和 package 删除/归档边界均通过。
18. （已裁决删除 · ITEM-1B B1 2026-09-04）不建 `order_plan_price_contributions`、`absolute_price_owner_plan_id`、rebaseline endpoint 与迁移 0038；`order_price_adjustments` 仅作历史只读审计。
## 共享语言与概念边界

沿用 `.codestable/requirements/CONTEXT.md` 中账号、客户、套系、订单、档期、镜头等 canonical 术语；ITEM-1 会把已确认的多订单项关系回写 canonical 文档。本 Epic 先冻结以下局部单义定义：

| 术语 | 本 Epic 中的单义定义 | 不包括 / 关键关系 |
|---|---|---|
| 订单 `Order` | 同一客户的一次交易与共同履约容器，拥有一个状态、收款事实、档期关系和成交总价 | 不是单个套系；需要分别取消、收款、排期或交付的服务必须拆单 |
| 订单项 `OrderItem` | 订单内一条有身份、有顺序的购买明细；可引用一个套系，并持有该次购买的快照、数量与成交分配价 | 不是普通 many-to-many join，也没有独立状态/支付/档期；同一订单同一套系最多一个有效项 |
| 有效订单项 | 尚未被软移除、参与当前公式价和成交分配小计的订单项 | 被移除项保留审计和历史引用，不参与当前聚合 |
| 计价单位 `pricing_unit` | `session`（次）、`hour`（按小时展示和定价）、`photo`（最终照片张数） | 有基准时 session 数量恒为 1；hour 数量以分钟存储；photo 不等于策划 Shot |
| 当前计价数 `item.qty` | 某订单项当前约定并用于计价的主数量；hour/photo 可修正，session 恒为 1 | 不宣称是不可变的物理交付事实；不直接作为档期事实 |
| 精修计价数 `item.retouch_qty` | 某订单项当前约定并用于该项精修加价判断的张数 | 只服务本项公式；不与策划事实核对 |
| 精修计价模式 | `none`（不含精修计价）、`included_no_overage`（包含但不自动计算超量价）、`included_with_overage`（包含且按单价计算超量） | 不再用 nullable 单价猜测模式；与策划域无关 |
| 订单项初始报价 | `item.quoted_price`，按该项加入时快照与初始计价数计算，写入后不可变 | 不等于订单建单时总报价；后加项也有自己的项初始报价 |
| 订单初始报价 | `order.quoted_price`，仅取建单事务中的初始项报价之和，写入后不可变 | 后加/移除订单项不改写它；当前范围看 `order.calculated_price` |
| 当前公式价 | 项级由快照与当前数量计算；订单级仅在全部有效项有基准时等于各项公式价之和 | 不含项人工偏离、整单调整或策划加项 |
| 成交分配价 `item.price` | 用户明确归属于某套系项的成交金额 | 不等于已收款；不得自动吸收整单优惠或策划加项 |
| 成交总价 `order.price` | 一笔订单实际交易总额及支付/营收下游唯一权威 | 可偏离项成交分配小计；不自动改支付事实 |
| 订单项成交分配小计 | `allocated_items_price`；仅当每个有效项 `price` 都非空时等于其总和，否则为 NULL | 不把未知项当 0，不是订单成交总价 |
| 整单未分摊调整 | `order.price - allocated_items_price`，仅在两侧均可知时派生 | 不自动分摊给套系；包含整单优惠与无法归项的人工调整 |

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

- **DEC-9 · 订单价格唯一 writer 是订单端**（原“策划贡献按身份替换”已由 creative ITEM-1B B1 裁决删除）：不建 contribution state、effect key、epoch、absolute owner 与 rebaseline。旧 `order_price_adjustments` 只作历史审计读面；删除 `orders.package_id` 后旧 `order_adjustment` 草稿路径全局 fail-closed（验收 11）。任何“从创作域写订单价格”的需求回 `creative-workspace-redesign` 讨论，不在本 Epic 内扩张。

- **DEC-10 · 精修计价只依赖订单项自身**（原“多项精修必须证明覆盖完整”已由 creative ITEM-1B B1 裁决删除）：每个订单项按自身 `retouch_pricing_mode_snapshot` 与 `retouch_qty` 独立计算精修加价；不存在跨项覆盖判定、不与策划总精修事实核对、不生成分配告警。账号级 `extra_retouch` / `extra_shot` 规则退出订单计价。

- **DEC-11 · 支付事实保持既有 DEC-10**：任何自动或手工项价/总价变化都不单独触发待收金额推定。价格变化后前端提示复核收款，已结清订单 `outstanding_amount=0` 保持不变。

- **DEC-12 · 小时项只在单义时预填档期**：恰好一个有效小时项才自动建议时长；多个小时项可能并行、重叠或属于不同内容，系统不能假定顺序相加。档期保存后与订单项独立演进。

- **DEC-13 · 类型归因属于订单项**：`channel_snapshot` 仍属于订单，`shoot_type_snapshot` 下沉到项。dashboard 类型金额只消费项成交分配价，整单未分摊调整进入独立桶；提醒引擎面对最新多类型订单使用最大流失阈值，优先避免过早误报。

- **DEC-14 · 迁移为 forward-only 聚合与 API items-v2 切换**：0036 迁套系参数并把旧精修能力默认隔离为 `none`，0037 建 `order_items` 并迁 legacy 项，再删除订单级 `package_id/shoot_type_snapshot` 权威与旧 API shape（原 0038 planner contribution 迁移已由 creative ITEM-1B B1 裁决删除）。生产采用单实例维护窗口：停写、备份、预检、迁移、同步部署前后端、烟测；不支持旧客户端或新旧二进制混跑，回退依赖备份恢复。

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
  - 验收要点：验收标准 3–8、11、13、14、16、17；多套系创建原子性、账号隔离、数量/金额上下界、相同套系部分唯一索引、软移除/重加新快照、至少一项、revision 冲突、所有项写幂等重放、package 锁全序、终态限制、legacy/backfill NULL 组与金额求和、客户 merge、复合 FK/套系删除保护和旧 planner 路径 fail-closed 且旧只读面/旧→旧级联不断（验收 11）回归均有真库用例。

- **ITEM-4 · 创作策划计价划界与陈旧性**（已裁决删除 · creative ITEM-1B B1 2026-09-04）
  - 状态：整项删除，编号保留占位；不建 0038、contribution state、absolute owner、rebaseline 与 evaluator 接管上下文。旧策划路径的 fail-closed 由 ITEM-3 承担。

- **ITEM-5 · 套系、dashboard、提醒与档期读模型适配**（owning: `cs-feat`）
  - 可交付：Package pricing summary；订单/档期批量 `package_summary`；dashboard 渠道×类型项级归因与整单调整桶；提醒引擎多项标题和最大 churn threshold；小时项预填判定。
  - 依赖：ITEM-3。
  - 验收要点：验收标准 12、15–17；无 N+1；每格 `COUNT(DISTINCT order_id)` 不因多项重复；完整分配订单逐单对账、部分未知订单只进未分配桶；零项不可出现；removed/cancelled/未知基准/跨账号/多小时项均有真库或领域用例。

- **ITEM-6 · 前端套系、订单项与排期体验**（owning: `cs-feat`）
  - 可交付：`PackagesPage` 单位化表单、legacy 精修待确认清单和经营摘要；`OrderWorkspace` / `ShootOrderFlow` 多项编辑器、逐项数量/公式价/成交分配价、整单小计/调整/总价、已提交移除后的重新加入提示和收款复核；小时档期预填。订单页不新增任何策划计价入口（planner rebaseline / 策划精修分配 UI 已由 creative ITEM-1B B1 裁决删除）。
  - 依赖：ITEM-2、ITEM-3、ITEM-5。
  - 验收要点：建单添加“场照精修 2 + 双人精修 1”无需离开表单；服务端响应覆盖前端预览；相同套系引导改量；整单改价不伪装成项分摊；所有项/价格写携带 revision 与 Idempotency-Key，冲突保留草稿并提供重载；375px、200% zoom、键盘、错误保留和结果未知恢复通过；API 类型只引用 `schema.d.ts`。

- **ITEM-7 · 导出、文档与整体验收**（owning: `cs-roadmap update`）
  - 可交付：导出 `order_items` 与全部新增字段、`schema_version` 5→6；回写 roadmap §4.6/§5/§8、CONTEXT、items.yaml、`docs/api/` 和维护窗口说明；核对 OpenAPI/生成物/实现一致。
  - 依赖：ITEM-2、ITEM-3、ITEM-5、ITEM-6。
  - 验收要点：验收标准 16–17；同一 schema version 对应唯一 shape；旧 shape 请求明确失败且不会误读多套系；旧 retouch_count 上线前后不改变账号级计价，只有用户确认后新订单生效；迁移/恢复演练、分层门禁与最终 `make check` 证据完整。

## 最终交付索引

- 语义权威：roadmap §4.2/§4.3/§4.6/§5/§8、`.codestable/requirements/CONTEXT.md`。
- 后端：`backend/internal/package/`、`backend/internal/order/`（含 `business_adjustment.go` 去 `package_id` 依赖）、`backend/internal/dashboard/`、`backend/internal/reminder/`、`backend/internal/schedule/`、迁移 0036/0037。`backend/internal/shootplanning/business/` 与 `shootplanning/crm/` 作为包归 `creative-workspace-redesign` 的 legacy 兼容面管理，本 Epic ITEM-3 只持有**窄例外授权**（范围以 `.codestable/work/feat-legacy-planner-barrier.md` §5 第 3 条为准，两处等价）：后端 `business/repository.go`、`business/application.go`、`crm/engine.go`、`settings/service.go`，契约 `api/openapi.yaml` 两个 enum 与生成物，以及前端 `frontend/src/planning/businessDraftInput.ts`、`frontend/src/planning/panels/BusinessPanel.tsx`、`frontend/src/account/settings/PlanningBusinessRulesSection.tsx`；限于去 `package_id` 读取、产出并中文化 `legacy_planner_retired`、以 `validation_failed` + 中文 message 拒绝 `planning_business_rules` 写入与设置面只读化，不得扩展 evaluator、新增策划写面或修改 `internal/platform/httpapi/` 手写文件。
- 契约：`api/openapi.yaml`、生成的 Go/TS 类型、语言无关 pricing golden cases。
- 前端：`PackagesPage.tsx`、`OrderWorkspace.tsx`、`ShootOrderFlow.tsx` / `ScheduleSlotDialog.tsx`、dashboard 与日历套系摘要；窄例外授权范围内的 `frontend/src/planning/businessDraftInput.ts`、`frontend/src/planning/panels/BusinessPanel.tsx`（`staleLabel` 中文化）与 `frontend/src/account/settings/PlanningBusinessRulesSection.tsx`（只读化）。
- 运维与文档：data export schema v6、`docs/api/`、维护窗口迁移/备份/恢复说明。

## 整体验收

以验收标准 1–8、10–17、各 ITEM 验收证据、迁移预检与维护窗口演练、OpenAPI 零漂移、分层门禁和最终 `make check` 为自动化门槛。另由 owner 进行桌面与 375px 真浏览器主链验收：确认旧精修套系 → 建两个套系 → 一单添加多个套系项 → 逐项改量/改价 → 整单优惠且不自动分摊 → revision 冲突恢复 → 单/多小时项排期 → 收款复核 → 套系/dashboard 对账 → 导出恢复。最终由 fresh acceptance reviewer 核对批准版本与实现，review 通过不代替 owner 接受。

## 遗留风险

1. **录入成本上升**：多项编辑比单选套系复杂；UI 必须默认一项、支持快速追加，并持续显示逐项小计与总价，不能让用户在多个折叠层中找金额。
2. **整单调整无法精确归因**：不自动分摊是为了避免伪造套系成交事实；套系分析只能消费明确的项成交分配价，整单调整需单列，用户若要精确归因必须逐项改价。
3. **历史多套系分配未知**：存量订单只能精确迁成原单项；backfill 可记录多个套系引用，但若没有逐项金额就不能进入套系成交均价或偏离率。
4. **精修数量按项人工录入**：多个套系项的精修数量依赖人工逐项表达，系统不做跨项核对，不创造照片资产与自动归属模型。旧套系默认 `none`，在 owner 确认前不产生套系精修加价。
5. **存量时长套系降级**：`per_duration` 为保报价恒等迁成 `session`，owner 需按迁移清单手工决定哪些套系改为 `hour` 及其每小时价格。
6. （原“两层计价能力并存”已裁决删除 · creative ITEM-1B B1 2026-09-04；替换为）**旧 evaluator 代码与 `planning_business_*` 表作为只读兼容资产保留**：它们不再参与订单计价，但在旧兼容面退役前仍在代码库中；任何“顺手复用 evaluator 算订单项”都属重新引入耦合，必须回 Epic 讨论。
7. **支付事实可能滞后于成交价**：这是沿用 DEC-10 的有意行为；UI 提示只能降低漏收风险，未来需引入 outstanding 来源状态才能安全自动联动。
8. **forward-only 迁移依赖运维纪律**：维护窗口若缺少备份、预检或烟测，删除旧订单级套系权威后的回退只能恢复备份。
9. **API 切换需要停机协同**：为避免旧客户端把多套系误读成无套系，本 Epic 选择一次性移除旧订单 shape；维护窗口必须同步部署前后端并在开放写入前完成 items-v2 烟测。
10. （原“旧策划单需要一次 rebaseline”已裁决删除 · creative ITEM-1B B1 2026-09-04；替换为）**有历史策划调价的订单失去自动重算路径**：`order_price_adjustments` 只读保留，items-v2 之后这些订单的价格只能由订单端人工调整；这是解耦的有意成本，不建补偿机制。
