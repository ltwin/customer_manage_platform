---
epic: ../epics/dashboard-v2-redesign.md
phase: executing
approved_revision: a35402254f6f4265e86cb0b58631504ee9f798567ef265f2ef5af32a17d51827
current_item: ITEM-3
next_action: 执行 ITEM-3（cs-feat：归因快照 channel_snapshot/shoot_type_snapshot），分支 feat/dashboard-v2-redesign 单分支串行继续
blocked_by: null
item_progression: continuous
milestone_commit: authorized
remote_publish: manual
---
## 子项进度
- [x] ITEM-1 交付 SLA 与交付队列事实（2026-08-24 完成：三轮 change review 闭环终审可合，完整 make check 单轮全绿；记录见 feat-delivery-sla.md，语义权威 roadmap §4.2）
- [x] ITEM-2 支付事实字段（2026-08-24 完成：两轮 change review 闭环终审可合，完整 make check 单轮全绿；记录见 feat-payment-facts.md，语义权威 roadmap §4.2 支付事实块）
- [ ] ITEM-3 归因快照
- [ ] ITEM-4 /dashboard/v2 聚合读模型
- [ ] ITEM-5 前端接入与原型常量清除
- [ ] ITEM-6 契约与文档回写

## 临时决策与证据

- 调查来源：`.codestable/audits/dashboard-v2-backend-capability.md`（2026-08-22）；核对：`backend/internal/order/model.go`（订单仅 price + deposit_paid/balance_paid 布尔，无金额字段）、`0005_orders.up.sql`。
- owner 拍板（2026-08-22）：本版只做基线（咨询转化/健康度下放二期）；支付事实走订单最小字段（amount_paid/outstanding_amount/paid_at）。
- compound 命中：`2026-07-09-cross-domain-read-model`、`2026-07-07-openapi-feature-tag-slicing`、`2026-07-14-dashboard`（D1/D10）。
- 术语权威：CONTEXT.md 已定义「线索=客户状态视图，非独立实体」；roadmap §7 已记「渠道转化分析」为二期候选。
- owner gate（2026-08-22）：批准 proposed 文档；item_progression=continuous、milestone_commit=authorized、remote_publish=manual；approved_revision 待 design review 通过后写入（目标 hash 56928209e1168da54a27700907cd1a21eaa072cc31cf601bd121d01accc7dc73）。
- design review 首轮创建记录：宿主 subagent（fable）因 API 402 余额不足终止无报告，按规则该轮失败不计轮次；codex CLI（maestro delegate）同因上游 402 额度用尽失败不计轮次；最终由宿主 subagent（继承主会话线路，无 model 覆盖）完成首轮终态报告。
- design review round 1（2026-08-23）：结论「建议先改再合」，2 blocking / 7 important / 4 nit。B1（ITEM-1 backfill 对象写反→改为存量已有 shot_at 订单）、B2（金额/SLA 写侧 UI 无归属→并入 ITEM-5）已修复；I1→DEC-9+遗留风险 6、I2→ITEM-2 backfill 规则+遗留风险 7、I3→三子项导出兼容+ITEM-6 统一决策、I4→ITEM-6 沿用 D11 先例、I5→验收标准 4 注明在途口径归 ITEM-4 冻结、I6→ITEM-2 扩 POST 双写路径、I7→DEC-10 结清推定；nit N1-N4 已吸收。修复后 hash：14c99c14f56b191d3e16484c07827b28be36c64956e1110c25f33ebd6a8a7155。
- design review round 2（2026-08-23）：结论「有条件可合」，前轮 13 条全部 resolved、无 blocking；新发现 NEW-1（整体验收漏引标准 8→已改 1–8）、NEW-2（部分收款 outstanding 联动缺失→DEC-10 扩为「金额联动推定」+ ITEM-2 验收补联动条款）与 NIT-1/2/3 均已修复。修复后 hash：2d045bf3f31068e7aa0566b9b230b2acbd3049e8e3814abbe989ef314abc8d76。
- design review round 3（2026-08-23）：结论「建议先改再合」，round 1/2 全部核销无回退；R3-1 blocking（DEC-10 联动与双向不变量互斥）达轮次上限交 owner 裁决——owner 选 ② 单向不变量（balance_paid=true ⇒ outstanding=0，反向不要求；已收讫订单 outstanding 钉死 0），并采纳「推定不降低既有 amount_paid」覆盖语义；R3-2 owner 选补 ITEM-1 backfill 验收项；NIT-5 顺手清理（范围行补已收现金 KPI）。审查阶段关闭。
- owner gate 完成（2026-08-23）：proposed→active，approved_revision=a35402254f6f4265e86cb0b58631504ee9f798567ef265f2ef5af32a17d51827，phase=executing，current_item=ITEM-1。B2 写侧 UI 并入 ITEM-5 与 DEC-10 金额推定均经 owner 裁决环节确认。
- ITEM-1 完成（2026-08-24）：change review 三轮闭环（round 1 修 9 条；round 2 可合 + I-1/NA-ND；round 3 终审「可合」blocking/important 双清零，N-C 经裁决确认 backfill 非 14 SLA 分支物理不可构造）。reviewer：宿主 subagent（codex 未装、claude CLI 上游 503，异构回落；round 1 reviewer session 跨会话不可恢复故 round 2 起 fresh）。随项修复分支遗留门禁破坏：account-center 媒体查询正则、auth harness 哨兵 29→32（planning 提交漏升）。验证：完整 make check 单轮全绿（EXIT=0，33 包 + 前端全套 + 4 shell 门禁 + 零漂移）；期间多次 Testcontainers `port 5432 not found` 偶发环境故障，串行重跑均过。残余风险（非阻塞，终审转正）：N8 PATCH 多一次 settings 查询、导出 schema_version 停 3 待 ITEM-6 统一决策、down 迁移丢新列数据、backfill 后改 shot_at 按当时 SLA 重算。
- ITEM-2 完成（2026-08-24）：开工前经 roadmap 定稿门槛（§4.2 支付事实块 + §4.3 POST/PATCH 枚举 + §4.6 导出注记，沿用 ITEM-1/ITEM-6 D11 先例）。迁移 0033 三列 + 表级单向不变量 CHECK + 存量 backfill（含 cancelled 不排除、paid_at 不回填）；order 域 payment.go DEC-10 推定（建单视为 0 已录入、结清推定不降低既有、price 变更非触发器、paid_at 仅 false→true 跃迁自动写）；金额无 null 语义 POST/PATCH 双侧 400；walk-list 钉与哨兵 33、readiness 钉同步。change review 两轮闭环（round 1 可合 + I-1/3 nit → 修复；round 2 终审「可合」全 resolved 无新增，剩余 1 文案 nit 接受不阻塞）；reviewer：宿主 subagent fresh（codex 未装、claude CLI 上游 503 复测确认，异构回落；round 2 沿用同 reviewer follow-up）。验证：完整 make check 单轮全绿（r3 EXIT=0；r1 的 3 个真实失败为 0033 适配点已修，r2 的 customer 包 Testcontainers 偶发串行重跑过）。记录：feat-payment-facts.md。非阻塞备忘：未结清 outstanding 可滞后于 price（DEC-10 字面）、schema_version 停 3 待 ITEM-6、down 丢三列数据。
