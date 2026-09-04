---
epic: ../epics/package-sku-pricing.md
phase: planning
approved_revision: pending
current_item: null
next_action: B1 修订版（hash 9988ead7…）已通过创作侧 3 轮 design review；对本 Epic 自身（套系 SKU / 多订单项 / 订单聚合）做一次增量 design review（新阶段、fresh reviewer），通过后进入 owner confirmation；ITEM-1 可在确认后启动
blocked_by: 本 Epic 修订版待自身增量 design review 与 owner confirmation；ITEM-3 窄例外授权（改创作 legacy 兼容面）待 owner 在创作 Epic 侧明示接受
item_progression: pending
milestone_commit: pending
remote_publish: pending
---

## 子项进度

- [ ] ITEM-1 · 语义权威、API 形状与公式样例
- [ ] ITEM-2 · 套系计价模型与迁移
- [ ] ITEM-3 · 多订单项、报价基准与价格状态机
- [-] ITEM-4 · 创作策划计价划界与陈旧性（已裁决删除 · creative ITEM-1B B1 2026-09-04，编号占位）
- [ ] ITEM-5 · 套系、dashboard、提醒与档期读模型适配
- [ ] ITEM-6 · 前端套系、订单项与排期体验
- [ ] ITEM-7 · 导出、文档与整体验收

## 临时决策与证据

- 2026-09-02：根据产品/技术双维评审修订 proposed Epic；独立 design review 已通过，等待 owner confirmation，`approved_revision` 保持 pending。
- 调查来源：`.codestable/features/2026-07-08-order-tracking/`（正常/补录套系规则、事务锁与订单状态机）、`.codestable/features/2026-08-05-shoot-plan-core/`（Shot 不等于照片）、`.codestable/features/2026-07-31-calendar-v2-redesign/`（订单与档期独立）、`.codestable/work/epic-dashboard-v2-redesign.md`（DEC-10 金额推定与 schema bump 纪律）。
- design review round 1（2026-09-02）：冻结 hash `e4bdc0081f10c6a584cb812ac9532d112abeaabcdd1d97a06a643009d10df7be`，reviewer `/root/package_sku_design_review`（宿主 collaboration subagent，`gpt-5.6-terra` xhigh）结论“建议先改再合”，2 blocking / 1 important。处理：策划 apply 遇既有偏离改为显式 `replace_existing_price_deviation` 替换确认并补状态表；`session.qty/quoted_qty_snapshot` 强制为 1；精修只抑制账号级金额行，数量不一致保留专用告警、更新入口与 acknowledgement，不暗中同步订单。待沿用同 reviewer 复审。
- design review round 2（2026-09-02）：冻结 hash `6c61c350d20abdf608756a0ed0335fed77c55ed838a21525e1ea8082b4caa47c`，同 reviewer 确认 round 1 三项均 resolved；新增 1 important：`session.qty=1` 与 backfill 未知基准整组 NULL 的适用域冲突。处理：将 session 数量约束限定为有计价基准订单；backfill（包括关联已迁为 session 的套系）全部计算字段保持 NULL，客户端传任一计算字段 400，并补 ITEM-3 真库用例。待同 reviewer 最终复审。
- design review round 3（2026-09-02）：冻结 hash `6c213abe3831d1dd6adc7de0e96a5973d064ddfa6b9bc90e000fae0550dae836`，同 reviewer 完整复审确认 round 2 finding resolved，无 blocking、important 或 minor，终局 verdict 为“可进入 owner confirmation”。
- 2026-09-02 owner 追加路线级需求：同一订单允许挂多个不同套系，例如“场照精修 2 张 + 双人精修 1 张”。原单套系设计审查结论随核心领域模型变化失效；Epic 改为 Order 聚合根 + 多个 OrderItem，套系/计价/类型快照与成交分配价下沉到项，订单总价继续作为支付与营收权威，并显式保留整单未分摊调整。代码事实来源：`backend/internal/order/model.go`、`backend/internal/order/repository.go`、`backend/internal/shootplanning/business/evaluator.go`、`backend/internal/dashboard/repository_v2.go`、`backend/internal/reminder/`、`api/openapi.yaml`、`frontend/src/components/schedule/ShootOrderFlow.tsx`；canonical 冲突来源：`.codestable/requirements/CONTEXT.md` 与 `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` 当前仍定义单 `package_id/shoot_type_snapshot`，由 ITEM-1 在实施前回写。
- 多订单项 fresh design review round 1（2026-09-02）：冻结 hash `d68c1e8e986853b3e1c1350b118f5407243a3b6975625316f3edb4965109378b`，reviewer `/root/multi_order_item_design_review`（宿主 collaboration subagent，`gpt-5.6-terra` xhigh）结论“建议先改再合”，5 blocking / 3 important。处理：补齐数量与持久金额非负域；将策划调价改为带 effect key/epoch/supersedes 的 contribution 替换链；引入显式精修模式并把未知/无套系项纳入覆盖判定与 fingerprint；backfill 完整项价自动求和订单总价，dashboard 要求逐单可对账；冻结 items endpoint、order revision、全写幂等和 package 锁全序；dashboard 按格 distinct order；取消含糊兼容投影，旧客户端缺 items-v2 capability fail loud；补复合 FK/RESTRICT/部分唯一索引。待同 reviewer 复审完整当前版本。
- 多订单项 fresh design review round 2（2026-09-02）：冻结 hash `b57c8d3a69ef06dde600511db5570e80f61dec19ec52821240584fd08b3b201b`，同 reviewer 确认 round 1 的 6 项完全解决、2 项需继续收口；新增 2 blocking / 3 important / 1 minor。处理：absolute target 明确为最终订单价并把 presence/value 纳入 effect key；新增 0038 `order_plan_price_contributions` state、v1 audit rebaseline fence 与旧 draft stale；fingerprint 补 order/customer/status，所有聚合写递增 revision 并锁内拒绝终态；NULL 总价改为独立未定价计数；旧 `retouch_count` 安全迁为 mode=none 并保留只读清单，需用户明确启用接管；统一“已提交移除后再次加入”措辞。待同 reviewer 第 3 轮最终复审。
- 多订单项 fresh design review round 3（2026-09-02）：冻结 hash `94984d9501e363fc8d27ea354abdf05376215966930a2ef780863c4b949b14e7`，同 reviewer 确认 round 2 的 5 项解决、absolute 多 plan 优先级仍 blocking，另有 rebaseline NULL/重试 important 与索引漏 0038 minor。处理：absolute 改为排他总价 owner，原子终止其他 contributions；其他 plan 后续 apply 必须 `replace_absolute_target` 并以当前总价 rebaseline；delta 限 signed BIGINT/checked int64；冻结带 revision+Idempotency-Key 的 rebaseline endpoint 与 NULL 基线错误；最终索引补 0038。修订后 Epic hash `d802a77d9db84470c7ccef4146728f9dd6fd4bd1c8222a2e22ad0c2ea4787ca9`。本 design review 已达三轮上限，按门槛停在 owner checkpoint，不自行发起第 4 轮。
- 2026-09-03：owner 将摄影师创作空间重设计设为更高优先级，并明确反对创作流程与计价持续绑定。为形成双向可执行 barrier，本 Epic 的验收 9–11、18、ITEM-4 和 ITEM-6 planner UI 在 `creative-workspace-redesign` ITEM-1B（B1 段）完成逐项 `删除 | legacy-read-only | 由订单端替代` 裁决前不可实施；套系 SKU、多订单项和纯订单计价内容仍保持 proposed，不把旧 planner 设计视为已批准。
- 2026-09-03：创作空间先导 Epic design review round 3 对双向 barrier 完整复核为 `PASS`；冻结 package Epic hash `b61d462194eec4a27aaffbbaf9895f661a3cfd5a402433e19861c2d77c44c64f`。barrier 继续保持，review 通过不等于 planner 内容获批。
- 2026-09-04：`creative-workspace-redesign` ITEM-1B B1 段完成 barrier 裁决，记录见 `.codestable/work/feat-legacy-planner-barrier.md` §2（17 条逐项 `删除 | legacy-read-only | 由订单端替代`，版本绑定 `pilot=creative ITEM-2` / `global=package ITEM-3`）。本 Epic 据此修订：删除验收 9/18、ITEM-4、0038、DEC-9/10 原文与 planner UI；验收 10 改为精修只依赖订单项自身字段；验收 11 改为旧 `order_adjustment` 草稿在删除 `orders.package_id` 后全局 fail-closed（`legacy_planner_retired`）；ITEM-5/6/7 依赖去掉 ITEM-4；遗留风险 6/10 替换。修订后 hash 见下一条 review 记录。套系 SKU 与多订单项内容未变，仍需自身增量 design review 后 owner confirmation。
- 2026-09-04（B1 review round 1 后）：修订版 hash `9cbc210fc5e13a93700c1f600f5aac32b305bbb71f5e9ae8ecce1f0c0f9f54e7`。按创作侧 reviewer 发现补正：验收 11 扩为「fail-closed 且旧只读面 / 旧→旧级联不断」，点名三处去 `package_id` 读取与 settings 写面拒绝，声明两个 `legacy_planner_retired` enum 为 OpenAPI 契约变更；交付索引改为 ITEM-3 对 `shootplanning/business`、`shootplanning/crm`、`settings` 的窄例外授权（范围见 `.codestable/work/feat-legacy-planner-barrier.md` §5）；ITEM-3 验收要点补验收 11；遗留风险 6/10 统一「已裁决删除；替换为」约定。该例外授权是跨 Epic 边界，待 owner 在创作 Epic 侧裁决。
- 2026-09-04（B1 review round 2 后）：修订版 hash `adf47c20a971065cc723f87ad586b3a0d6e98593e3cc343ee8719178ed4fb3aa`。barrier 段改为「三处 SQL 硬读是技术事实，整体退役是 DEC-8 产品裁决」并补 `schedule_duration` 保留前提；验收 11 补两处前端映射与 settings 只读化；交付索引前端行与例外段扩为与 barrier 文件 §5 等价。
- 2026-09-04（B1 review round 3 后，终态）：修订版 hash `9988ead7bcee874229576dcd1f319c7cb0e5ebdc6640886ef6a8b4a84f73d1c6`。settings 拒绝形状定为 400 `validation_failed` + 中文 message（不新增 code、不动 `httpapi/` 手写文件）；交付索引例外段直接枚举三个前端文件。创作侧 B1 审查阶段关闭，barrier 裁决版本绑定生效。

