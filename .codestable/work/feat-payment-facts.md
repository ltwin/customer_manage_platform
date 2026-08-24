---
epic: ../epics/dashboard-v2-redesign.md
item: ITEM-2
slug: payment-facts
status: in-progress
created: 2026-08-24
review_stage: change-review
review_rounds: 0
---

# feat-payment-facts · 支付事实字段（ITEM-2）

## 目标

订单落支付事实：`amount_paid`（已收现金，分，恒有值默认 0）/ `outstanding_amount`（待收余额，分，NULL=price 未定价不计待收）/ `paid_at`（收款时刻）。POST（含补录建单）与 PATCH 双写路径可写；DEC-10 金额联动推定与单向不变量 `balance_paid=true ⇒ outstanding_amount=0` 恒成立；存量 backfill（已结清推定 COALESCE(price,0)+0，未结清 0+price）；无 price/2 推算。语义权威：roadmap §4.2 支付事实块（本项开工前已定稿，2026-08-24）。

## 现场

- 迁移基线 0032（ITEM-1）；walk-list 钉在 7 个 store 测试 + harness 哨兵 `fullVersion == 32`。
- order 域写路径：`ApplyCreateInput`/`ApplyUpdateInput`（state_machine.go）+ `applyDeliveryDue`（delivery_due.go 先例）；repository 显式列 INSERT/UPDATE。
- 绕过 order service 的直写路径仅一条：`order/business_adjustment.go`（shootplanning 商务调价，只写 price）。DEC-10 字面下 price 变更不是推定触发器，该路径无需改动。
- 导出投影复用 httpapi `toAPIOrder`；dashboard/导出各自维护镜像列清单。
- OpenAPI：Order schema + POST/PATCH body + api.gen.go + schema.d.ts 再生成（make generate）。

## 边界

- 推定触发器按 DEC-10 冻结字面：①置结清未给金额→结清推定（不降低既有）；②录入/修正 amount_paid（含创建，建单视为 0 已录入）未给 outstanding→max(price−amount,0)。**price 单独变更不触发 outstanding 重算**（含 business_adjustment 直写），未结清订单 outstanding 可能滞后于 price——边界接受，消费方（ITEM-4）按落库值聚合。
- `paid_at` 自动写仅限 PATCH 中 balance_paid false→true 实际跃迁且未显式提供；创建（含补录结清单）不自动写（不为历史发明时刻）；已写入可修正不可置空。
- 金额字段无 null 语义：POST/PATCH 显式 null → 400；终态金额可修正（对齐 price，退款/坏账手工通道），收款标记终态不可变不变量保持。
- backfill 含 cancelled（按 epic 字面「未结清单」不排除）；paid_at 一律不回填。消费方联合 status 过滤（同 delivery_due 模式）。
- 单向不变量上 DB 表级 CHECK（NOT balance_paid OR outstanding_amount=0），schema/迁移路径触发独立 change review。
- 导出 schema_version 保持 3（ITEM-6 统一决策，新字段可选向后兼容）。
- 取消结清（终态外 balance_paid true→false）不重算 outstanding（留 0=「录满未点收讫」合法态）。

## 证据

- 领域级（`backend/internal/order/payment_test.go`，19 例 red→green）：建单推定（price→outstanding、price NULL→NULL）、结清单补录推定（COALESCE(price,0)+0，paid_at 不发明）、显式优先、v1 标记收讫推定+paid_at=now、不降低既有（1200/1000 保持 1200）、部分收款联动（300→700、超收→0、price NULL→NULL）、显式 outstanding 优先、结清态 outstanding≠0/null/负值 400、结清后改价/改金额 outstanding 钉死 0、取消结清保持金额、再结清落新 paid_at、显式 paid_at 优先、终态金额可修/cancelled 记坏账、未触碰字段保持现状、price 单独变更不重算。
- 迁移（`backend/internal/platform/store/orders_payment_facts_migration_test.go`，真 Postgres）：MigrateStepsForTest(32) 造存量 → up 后逐单断言 backfill 值（含 cancelled 不排除、price NULL→NULL、paid_at 全 NULL）；表级 CHECK 三连击（结清+outstanding>0 拒、负 amount、负 outstanding）；down 后三列消失。
- httpapi（`orders_test.go` TestOrderPaymentFactsLifecycle）：建单 1000→outstanding 1000 → PATCH 300→700 → 标记收讫→1000/0/paid_at 非空 → 改价 800→outstanding 仍 0 → 结清态显式 outstanding 400 → 三字段显式 null 各 400 → 负值 400 → 补录结清单 500/0/paid_at NULL。
- 导出（`dataexport/repository_test.go`）：order-a 部分收款 300/700、order-f 结清 8000/0+paid_at 原样带出；`canonicalSnapshotTimes` 补 paid_at 归一化；schema_version 保持 3（§4.6 注记，ITEM-6 统一决策）。
- walk-list 钉：7 个 store 测试文件 down 步进表加 `orders-payment-facts`；harness 哨兵 `fullVersion == 32 → 33`；planning_share 契约测试两处插 0033 down 步。
- OpenAPI：Order schema（amount_paid required）+ POST/PATCH body 三字段 + api.gen.go + schema.d.ts 再生成。
- 迁移 CHECK 采用 `NOT balance_paid OR COALESCE(outstanding_amount,0)=0`：把 settled+NULL 显式按 0 语义求值（两式对 NULL 行均放行；应用路径结清分支恒写 0，实际产不出 settled+NULL，无需要堵的洞——round 1 review N-1 纠正了先前「堵洞」的错误表述）。

## 验收

- [x] 金额字段经 POST（new+backfill）与 PATCH 可写（领域+httpapi 用例）
- [x] 单向不变量恒成立（服务层 validatePaymentFacts + 迁移表级 CHECK）；closed 恒结清不变量不破（既有 validateFinalState 未动）
- [x] v1 标记收讫行为不回退：推定生效、不降低既有 amount_paid、自动落 paid_at
- [x] 部分收款联动：录入 amount_paid 后 outstanding 按推定变化
- [x] 存量 backfill 规则正确（含 price NULL→outstanding NULL；cancelled 不排除；paid_at 不回填）
- [x] 无 price/2 推算；显式 null/negative → 400；结清态显式 outstanding≠0 → 400
- [x] 导出含新字段且 schema_version=3；OpenAPI 再生成；make check 全绿（见状态）

## 状态

- 完成（2026-08-24 开工并完成：roadmap §4.2/§4.3/§4.6 措辞定稿 → 实现 → change review 两轮闭环终审可合 → make check 单轮全绿；语义权威 roadmap §4.2 支付事实块）

### change review 记录

- **round 1（2026-08-24）**：reviewer 创建按序回退——codex CLI 未安装、claude CLI 上游 503（`model_not_found` 无可用渠道，与 ITEM-1 时一致），回落宿主 subagent fresh reviewer 单轮执行 cs-review。冻结目标：staged diff（29 文件 +1141/−29）。结论**可合**：0 blocking / 1 important / 3 nit。
  - I-1（important）：POST 金额字段显式 null 被 *int 绑定吞成缺省、未按 §4.2 返 400，且注释宣称「契约层即非法」与运行时不符 → 已修（bindCreateOrderBody + rejectExplicitNullAmounts + e2e 三例）。
  - N-1：「COALESCE 堵洞」表述不成立（两式对 settled+NULL 均放行，应用层恒写 0，无实际洞）→ 已修文档表述，迁移不动。
  - N-2：work 文档用例计数 17→19 → 已修。
  - N-3：§4.6「可选形式」与 amount_paid required/恒输出不一致 → 已修措辞。
  - 审查同时确认：迁移 backfill 与 epic 逐条对齐、walk-list 钉零缺漏、DEC-10 全写路径成立（含 business_adjustment 直写 price 不触发重算）、三处列投影对齐、OpenAPI 三方一致、安全面无恙。
- **round 2（2026-08-24）**：同 reviewer follow-up 复审修复增量（重新冻结 b8c374df19868c19，29 文件 +1182/−33）。结论**可合**：I-1/N-1/N-2/N-3 全部 resolved，无 unresolved，无新 blocking/important；reviewer 独立重跑 httpapi/order 全包测试通过。剩余 1 条可选 nit（POST「不接受 null」与 PATCH「不可置空」错误文案不一致，均 400 validation_failed 语义一致）——按审查意见接受不阻塞，不另行统一（改动会触碰领域层既有文案）。审查阶段关闭（2 轮，未达 3 轮上限）。

### 验证记录

- r1 完整 make check：3 个真实失败（0033 适配点：导出期望文档缺 amount_paid、readiness 版本钉 32、0032 测试 down 少退一步）→ 全部修复。
- r2 完整 make check：仅 customer 包 Testcontainers `port "5432/tcp" not found` 偶发（与 diff 无关），串行重跑通过。
- r3 完整 make check：**EXIT=0 单轮全绿**（33 后端包 + 前端全套 + 4 shell 门禁 + generate-check 零漂移）。

## 未决

- 无阻塞未决。非阻塞备忘：未结清订单 outstanding 可滞后于 price（DEC-10 字面，price 变更非触发器，ITEM-4 消费按落库值聚合）；导出 schema_version 停 3 待 ITEM-6 统一决策；down 迁移丢弃三列数据（与 0032 同取舍）。
