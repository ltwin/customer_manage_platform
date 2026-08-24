---
epic: ../epics/dashboard-v2-redesign.md
item: ITEM-3
status: implementing
---

# feat: 归因快照 channel_snapshot / shoot_type_snapshot

## 目标

订单固化下单时归因（客户渠道 + 套系拍摄类型），支撑渠道×类型收入矩阵（ITEM-4 消费）：
- 订单 `channel_snapshot`/`shoot_type_snapshot` 字段 + migration（0034）+ 创建时固化
- 客户渠道/套系类型后改不改变历史订单归因；无套系订单 `shoot_type_snapshot` NULL（矩阵「未归因」桶）
- 存量订单 best-effort 回填（epic 遗留风险 4）；导出 schema 兼容（schema_version 停 3，ITEM-6 统一决策）

## 现场

- `customers.channel` NOT NULL 枚举（xiaohongshu/douyin/weibo/referral/other，0002）；`packages.shoot_type` NOT NULL 枚举（portrait/cosplay/other，0004）；orders 复合 FK 保证 customer 恒存在、package 可空（0005）。
- 订单唯一生产 INSERT 在 `order/repository.go CreatePreparedInScope`；该函数已用 `requireUsableCustomer/requireUsablePackage` 在创建事务内 FOR UPDATE 锁定 customer/package——快照取值落同一缝隙（同事务一致性）。幂等建单与档期建单都经此路径。
- `UpdateInput` 无 PackageID：套系创建后不可换，快照不可变性天然成立。
- 大量测试用 `scope.Insert(ctx, "orders", …)` 显式列直插（19 处/16 文件）——`NOT NULL` 无默认会打断无关套件；沿用 ITEM-2 `amount_paid DEFAULT 0` 先例取 `channel_snapshot DEFAULT 'other'`（渠道枚举兜底桶；生产写路径恒显式提供）。
- 导出投影复用 httpapi `toAPIOrder`（data_export.go），加字段自动跟进。

## 边界

- 快照非请求字段：POST/PATCH body 均不接受（roadmap §4.2/§4.3 定稿措辞）。
- 客户 merge 改挂订单、客户渠道后改、套系 shoot_type 后改、订单任何 PATCH/状态推进均不重算快照。
- `business_adjustment.go` 直写价格与快照无关。
- dashboard 域 orderColumns 未加快照列——v1 五块无消费者，ITEM-4 建 v2 读模型时按需扩展。
- 存量 backfill 按当前客户渠道/套系类型写入，可能与真实下单时不一致，仅作展示不做精确承诺（epic 遗留风险 4）；epic DEC-5 括注「历史订单不回写快照」为 proposed 稿残留，与同文档验收要点/遗留风险 4 冲突，以验收要点（best-effort 回填）为准，措辞清理归 ITEM-6。
- down 迁移丢两列快照数据（与 ITEM-1/2 down 语义一致）。

## 证据

- 领域固化与不可变：`backend/internal/order/attribution_test.go` TestCreateFreezesAttributionSnapshot（创建固化 douyin/cosplay；SQL 改渠道/改类型/改挂客户后 Update 不重算；无套系 → NULL）。
- 迁移与约束：`backend/internal/platform/store/orders_attribution_snapshot_migration_test.go`（backfill、枚举 CHECK、DEFAULT 'other' 兜底、down 可逆）。
- API 端到端：`orders_test.go` TestOrderAttributionSnapshotLifecycle（建单响应快照、客户/套系后改 + PATCH 后不变、列表项携带、无套系缺省、后改后新单固化新值）。
- walk-list 钉 0034：7 个 store 测试文件；哨兵 `fullVersion == 34`、readiness `SchemaVersion != 34`；0032/0033 down 段与 planning_share 两处 down-walk 各叠加一层 0034 回退。
- 导出兼容：dataexport repository_test fixtures 显式快照值 + route 测试期望 `channel_snapshot` 恒输出、`shoot_type_snapshot` 随套系输出。

## 验收

- [x] 新建订单固化下单时渠道/拍摄类型（领域 + e2e）
- [x] 客户渠道或套系类型后改不改变历史订单归因（领域 + e2e）
- [x] 无套系订单 shoot_type_snapshot 为 NULL（「未归因」桶展示归 ITEM-4）
- [x] 存量订单 best-effort 回填（记遗留风险）
- [x] 导出 schema 兼容（schema_version 停 3，ITEM-6 统一决策）

## 状态

完成（2026-08-24）

## 验证记录

- make check r1（nit 修复前）：单轮全绿 EXIT=0（33 后端包 + 前端全套 + 4 shell 门禁 + generate 零漂移），/tmp/makecheck-item3-r1.log。
- make check r2（nit 修复后重跑）：单轮全绿 EXIT=0（33 包），/tmp/makecheck-item3-r2.log。
- 三个新测试（领域/迁移/e2e）与受影响套件（order/dataexport/httpapi/store 串行）均通过。

## change review 记录

- 触发依据：持久化 schema/迁移 + 多消费者契约（OpenAPI required 字段）变更。
- reviewer 创建：宿主 subagent fresh（codex 未装、claude CLI 上游 503 model_not_found 本会话复测确认，异构回落；宿主 Agent 工具无 model 参数，继承主会话线路）。前两次异步派发均因「模型流停滞 600s 无事件」失败无终态报告，按规则不计轮次；第三次同步精简执行成功。
- round 1（目标 e754299e…ca72c，26 文件 +585/−47）：结论「可合」，0 blocking / 0 important / 2 nit——N-1 attribution_test 条件风格不一；N-2 幂等建单路径快照未覆盖。两条均已修复（N-2 用既有 authenticatedOrderCreate helper 补幂等固化断言 + 同键重放存储响应一致，对齐交付日双路径先例）。
- round 2 follow-up（同 reviewer 同 session；目标 52c4a669…2cd58，26 文件 +610/−47）：终审「可合」，N-1/N-2 resolved，无 new findings。阶段 2 轮关闭。

## 未决

- 无阻塞项；DEC-5 括注措辞与验收要点的冲突清理归 ITEM-6。
