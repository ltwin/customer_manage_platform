---
type: feat
slug: customer-health-tiers
status: done
branch: feat/customer-health-tiers
base: main (035c063)
created: 2026-08-24
---

# feat-customer-health-tiers：客户资产卡（健康度分层）

## 目标

dashboard v2 L3「客户资产」剩余主体——健康度分层卡：把在册 active 客户按**个人拍摄节奏**分为 活跃/沉睡/高危/已流失/新客 五层，高危层按累计贡献排序引导挽回，每层配语境化私域触达文案（可编辑复制）。来源：原型 `frontend/proto-design/v2/dashboard.html` L3 + `.codestable/audits/dashboard-v2-backend-capability.md` §7（评级 B-，下放二期）；roadmap §7 二期下放清单第 ② 项。

## owner 五项口径拍板（2026-08-24）

1. **D-A 与 churn 口径关系：分离 + 卡内交叉引用**。churn 提醒=行动提醒（固定天数阈值），健康度=资产盘点（个人节奏倍数）；前端高危层标注「其中 N 位已生成唤回提醒」（数据来自 due_reminders 中 type=churn 的 customer_id，零额外请求）。不动 reminder 引擎。
2. **D-B LTV 口径：主口径「已结清」**（非 cancelled ∧ balance_paid=true 的 price 之和，与渠道矩阵同源，排序用它）+ **未结清已收（unsettled_paid）作次要行共存展示**；不做 toggle（owner 体验后再议，数据层两列都下发，展示形态随时可改）。
3. **D-C 阈值/基线参数化进 settings**（不硬编码）：`Settings.health_tiers` 三档 ratio 严格递增 + fallback 基线 30-365；默认值即原型口径（1.2/2/3.5/120）。同日多单 7 天间隔下限不参数化（工程防除零）。
4. **D-D 样本口径**：节奏样本与金额均排除 cancelled；盘点对象仅 status=active 客户（archived/merged 不入）；新客层（0 拍）入卡，CTA=促成首单。
5. **D-E 契约挂载**：扩 `GET /dashboard/v2` 增 `customer_health` 块（同页一次加载），不另开端点。

## 现场

- **roadmap 定稿（开工前门槛）**：§4.2 Settings 增 health_tiers 参数组；§4.3 v2 契约增 customer_health 块（口径/排序/上限/thresholds 回显全定义）；§4.6 导出 schema_version 4→5（Settings.health_tiers required 恒输出，延续 bump 纪律）；§7 第 ② 条收口；§8 变更日志。tier 术语语义权威在 §4.3、仅 dashboard 域消费，不进 CONTEXT.md（沿 ITEM-4 dashboard 派生指标先例）。
- **迁移 0035**（settings_health_tiers）：JSONB NOT NULL DEFAULT 原型口径，存量行由列默认直接获得参数，无 backfill；down 丢列（参数可重录）。0014 availability 先例。
- **后端聚合**：repo 两查（active 客户 + 非 cancelled 订单全量，Go 内存聚合与 buildChannelMatrix 同模式）→ service 折算账号本地 date-only → `buildCustomerHealth` 纯函数（分层/双金额/排序/50 行截断）。settings.Service 增 `HealthTiersForAccount`，V2SettingsReader 扩第三方法。
- **settings 全链路**：model 默认值 + applyPatch 跨字段校验（400）+ EffectiveSettings 存储防御（无效回退默认）+ repository/service 三处 JSON 编解码 + 前端 RemindersDraft（bodyBuilders/kernel/hydrate）+ 设置页「健康度分层」节（四输入 + validateHealthTiersDraft 前端同口径校验）。
- **前端卡**：DashboardPage L3 健康度卡与渠道矩阵成对 two-col（第三处）；分层比例条切换（默认高危）+ 行（渠道徽章/通用基线徽章/节奏文案/双金额/ratio 仪表条）+ 每层 CTA（care/winback/first 三套文案模板，带距上次拍摄/建档天数事实）→ 复用 CopyTextSheet；churn 交叉引用条；「判定口径与基线说明」折叠（引用后端回显 thresholds，参数化后口径与判定快照一致）。
- **schema_version 4→5 波及面**：dataexport service 常量 + openapi enum + 五处测试钉（route_test map/struct 两处、data_export_test 空文档、service_test）+ dataexport fixture HealthTiers 列默认 + **十处迁移测试 down 链/哨兵同步**（store walk-list、settings_availability、auth harness×2、auth_limiter×2（含 readiness 哨兵 34→35）、auth_migration×2、planning_migration、telegram_digest、planning_share 两段链、orders_delivery_due/payment_facts/attribution 顺序 down、0035 专属迁移测试）。

## 边界

- 7 天间隔下限适用于**一切相邻间隔 <7 天**（非仅同日多单）——短间隔（1–6 天）也抬到 7，防基线失真；已写入 roadmap §4.3。
- 非目标：churn 引擎改造、健康度物化读模型表（实时聚合，量级几百客户）、Calendar/其他页消费、文案模板个性化配置。
- 健康度「已流失」≠ churn 提醒触发——语义分离已写入契约描述（排除含义）。
- 同页两卡金额同源：settled_ltv 与 channel_matrix 聚合条件一致。

## 证据

- red → green：后端 settings 7 用例（默认/落位/跨字段校验）+ dashboard 纯函数 5 用例（五层判定/同日下限/双金额/排序/50 截断）先红（build failed / undefined）后绿；前端模型 5 用例（v2HealthRows is not defined 先红）；layout 断言 2 用例（two-col=3、健康度卡缺失先红）后 6/6 绿。
- 集成：dashboard_v2_test 增 customer_health 断言（三人 active、cadence 同日下限兜底、fallback 基线、双金额、tie 排序）；0035 迁移测试（默认值 + down 丢列）；settings round-trip（前端）+ dataexport fixture/route/空文档断言。
- make check：多轮全量，真实失败全清零；残余失败均为 Testcontainers `port "5432/tcp" not found` 偶发（attention.md 记录模式；order/customer/store/auth/idempotency 各包串行重跑均 EXIT=0；当日机器上有用户容器在跑）。前端全套绿（含 account-center A1-A4 白名单补 health_tiers）、lint/build 零错误、make generate 零漂移。

## 验收

- [x] `GET /dashboard/v2` 返回 customer_health 块，五层计数/行字段/排序/50 上限/thresholds 回显与 §4.3 契约一致
- [x] settings health_tiers GET/PATCH 全链路（默认值、跨字段 400、无效存储回退默认）
- [x] 导出 schema_version=5 且 settings 含 health_tiers（同版本同 shape 不变量保持）
- [x] dashboard L3 健康度卡与渠道矩阵成对渲染、点层切换、CTA 文案复制、churn 交叉引用、口径说明引用回显参数
- [x] 迁移 0035 up/down 可逆、存量行默认值正确
- [ ] owner 浏览器实测（分层分布、文案语气、LTV 共存展示观感——owner 明说「先按你的方式去做，回头体验了再看看」）

## 状态与未决

- change review 三轮闭环（2026-08-24，reviewer：宿主 subagent fresh 同一 session follow-up，异构回落沿各 ITEM 先例）：
  - round 1「建议先改再合」1B/1I/5N：blocking=dataexport 私有 settingsColumns 漏 health_tiers 列致导出静默回落默认（违反 §4.6 availability 先例；修复=补列+解码+route/repository 测试改自定义值 1.5/2.5/4/90 双向断言）；important=前端层 hint 硬编码默认区间（修复=v2HealthTierHint 按生效阈值动态拼装+用例）；nit 5 全修（死三元+签名简化、gauge 锚点 /4→lost_ratio、archived 负例、EffectiveSettings 直测、7 天下限落文 §4.3）。
  - round 2「建议先改再合（轻量收尾）」1I/1N：新发现 A=archived 负例种子插在 GetV2 值快照之后断言读旧快照（死钉）；B=三处 healthCopyText 三参调用残留（round 1 脚本 pattern 前缀错误漏改）+1 处旧注释。round 1 全部 resolved。
  - round 3 终审「可合」0/0/0：负例改为种子后二次 GetV2 新快照断言 + **mutation 验证**（临时破坏 status='active' 过滤 → 测试红 total 4 → 恢复绿，负例确证真钉）；三轮 9 项 findings 全 resolved 无遗留。终审快照 SHA-256 f5b6ca3a…2c307。
- change review 已终审「可合」；owner 已授权提交（2026-08-25，attention.md：提交需人工同意），提交信息：`feat(dashboard): 客户资产健康度分层——个人节奏五层盘点、LTV 双口径与 settings 参数组（customer-health-tiers）`。
- 待体验后可能调整：文案模板语气、LTV 次要行展示形态（数据层两列已下发，改展示不动契约）。
