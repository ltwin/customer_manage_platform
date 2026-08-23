---
epic: dashboard-v2-redesign
item: ITEM-1
slug: feat-delivery-sla
status: implemented
---

# ITEM-1 · 交付 SLA 与交付队列事实

## 目标

给订单一个可查询的**应交付日** `delivery_due_at`，来源 = 账号级默认 SLA 天数（settings）+ 订单级覆盖，供 ITEM-4 的后期交付队列直接读取，不在前端用 `+14 天` JS 常量推算。

Epic 契约：`.codestable/epics/dashboard-v2-redesign.md` ITEM-1 与 DEC-6（approved_revision a35402254f6f...）。

## 现场（已核实）

- `settings` 表标量字段模式：列 + CHECK + `PatchInput` 指针 + `DefaultSettings()`（`0009_settings.up.sql`、`settings/model.go:44-86`）。`availability` 那套 JSONB 严格解码不适用于单个整数，不引入。
- 订单时间戳写入唯一挂载点：`ApplyCreateInput`（`state_machine.go:40-78`）与 `ApplyUpdateInput`（`:80-150`）。`shot_at` 首次落值有两条路径——状态跃迁到 shot 自动写 now（`:136-139`），或显式传入（`:113-120`）。
- `clock.AccountClock` 已下沉 platform，reminder/dashboard 同源（`platform/clock/clock.go`），提供 `LocalDate`/`AddDays`/`DateOnly`。SLA 加天数必须走它。
- 迁移序号已到 0031，本次 0032。
- 导出订单列固定在 `dataexport/repository.go:25` `orderColumns`，新增字段须同步（Epic ITEM-6 统一决策导出 schema）。
- 订单表 `orders` 无 `updated_at`；`price` 可空；存在 `CreationModeBackfill` 可直接建 delivered/closed 单。

## 边界

### 已定（Epic 契约 + 本设计）

1. **存储类型 = `DATE`（date-only）**，不是 timestamptz。理由：SLA 语义是「拍摄后 N 天交付」，摄影师看日期不看时刻；date-only 在跨时区/DST 下无歧义；与 `reminders.due_date` 既有模式一致。
2. **来源三态**：
   - `delivery_due_at IS NULL` 且订单无 `shot_at` → 未进入交付链条，队列不显示。
   - 自动派生：`shot_at` 首次落值时写入 `LocalDate(shot_at) + settings.delivery_sla_days`（账号时区）。
   - 订单级覆盖：显式传入 `delivery_due_at` 优先，写入后不再被自动派生覆盖。
3. **改 SLA 不重写历史**：`delivery_due_at` 是**落库事实**，settings 改动只影响此后新落值的订单（Epic 验收标准 5）。
4. **`shot_at` 变更后重算**：仅当该订单 `delivery_due_at` 为自动派生（非订单级覆盖）时才随 `shot_at` 重算。需要区分「派生」与「覆盖」——见待决 D-1。
5. **backfill 范围**：存量已有 `shot_at` 的订单全部回填（含 closed），`cancelled` 不回填（不进入交付队列）。回填值 = `LocalDate(shot_at) + 默认 SLA`（迁移时账号 settings 的有效值）。
6. **SLA 默认值**：`delivery_sla_days`，默认 14，范围 1–180（CHECK）。放 settings 表标量列，照 `digest_hour` 模式。
7. **覆盖日期无下界**（round 2 复审 N-D 确认为故意）：覆盖值允许早于 `LocalDate(shot_at)`——补录早已逾期的应交日是真实场景；消费方（ITEM-4 交付队列）不得假设 due ≥ LocalDate(shot_at) 的单调性。

### 待决（本设计需定，不上升 Epic）

- **D-1 派生 vs 覆盖如何区分**：方案 A 加布尔列 `delivery_due_override`；方案 B 不区分，`shot_at` 变更一律重算（覆盖会被冲掉）；方案 C 不区分，`delivery_due_at` 一旦有值就不再自动重算（Epic 验收「shot_at 变更后 due 重算」不满足）。
  → **选 A**：Epic 验收同时要求「订单覆盖生效」与「shot_at 变更后 due 重算」，只有显式标记能同时满足两者。列名 `delivery_due_is_override BOOLEAN NOT NULL DEFAULT false`。
- **D-2 `cancelled` 订单已有 due 时如何处理**：保留落库值不清除（历史事实），队列查询侧排除 cancelled。理由：清除是破坏性的，且 cancel→其他状态不可逆（状态机无反向边），无需回滚。

## 交付物

1. `0032_orders_delivery_due.up/down.sql`：orders 加 `delivery_due_at DATE` + `delivery_due_is_override BOOLEAN NOT NULL DEFAULT false`；settings 加 `delivery_sla_days INTEGER NOT NULL DEFAULT 14 CHECK (1..180)`；backfill 存量。
2. `settings`：model 加 `DeliverySLADays` + PatchInput + 默认值 + 校验；repository 列同步。
3. `order`：model 加 `DeliveryDueAt`/`DeliveryDueIsOverride`；state_machine 派生逻辑；repository 读写列；service 校验。
4. `api/openapi.yaml`：Settings/Order/CreateOrder/UpdateOrder shape；`make generate` 双端。
5. roadmap §4.2/§4.3 措辞（D11：implement 前定稿）。
6. `dataexport` orderColumns 同步。

## 验收（Epic ITEM-1 验收要点逐条）

| ID | 场景 | 期望 |
|---|---|---|
| S1 | settings PATCH `delivery_sla_days` | 生效；越界 400 |
| S2 | 订单跃迁到 shot（不传 delivery_due_at） | 自动 = LocalDate(shot_at)+SLA，`is_override=false` |
| S3 | 创建/更新显式传 delivery_due_at | 覆盖生效，`is_override=true` |
| S4 | 已派生订单改 shot_at | due 重算 |
| S5 | 已覆盖订单改 shot_at | due 不变 |
| S6 | 改 settings SLA 后查历史订单 | 历史 due 不变 |
| S7 | 补录建单直接带 shot_at | 建单即落 due |
| S8 | migration backfill | 存量有 shot_at 的订单（含 closed）有 due；cancelled 无 |
| S9 | 非账号订单 | AccountScope 隔离不泄漏 |
| S10 | 导出 | orderColumns 含新字段，schema 决策见 ITEM-6 |
| S11 | PATCH 显式传 null | 撤销订单级覆盖，回到自动派生；未传该字段保持现状（三态） |

## 证据

实现完成。关键证据：

- **S1 settings SLA**（round 2 补齐）：`settings/service_test.go` `TestServicePatchDeliverySLADays`（1/30/180 生效、0/-1/181 拒绝）+ `httpapi/settings_availability_test.go` `TestSettingsDeliverySLADaysPatchAppliesAndRejectsOutOfRange`（PATCH 30 生效、越界 400、拒绝不改存量）。
- **S2–S8 领域用例**：`backend/internal/order/delivery_due_test.go`（15 例，含上海/洛杉矶日界、SLA 14→30 重算、覆盖不被冲、改 SLA 不重写历史、显式 null 撤销五态族）。
- **S7 接线回归**：`backend/internal/platform/httpapi/orders_test.go` `TestDeliveryDueDerivedOnBothCreatePaths`——带/不带 `Idempotency-Key` 两条建单分支都必须派生。该用例经 red→green 验证：把 handler 改回 `PrepareCreate` 即失败。
- **S8 迁移 backfill**：`backend/internal/platform/store/orders_delivery_due_migration_test.go`——东京/洛杉矶各按本地日界、无 settings 行回落 `Asia/Shanghai`+14、closed 回填、cancelled 与无 shot_at 不回填、down 可逆。注：backfill 对存量 settings 行恒见默认 14（列在本迁移内以 DEFAULT 14 创建），`COALESCE` 只兜 LEFT JOIN 未命中的无 settings 行账号——不存在「存量行带非默认 SLA 参与回填」的路径。
- **S10 导出**：`dataexport` 订单与 settings 列均取真值，fixture 用非默认值（SLA 30、订单级覆盖 2026-07-25）验证不回落默认。

**门禁说明（纠正先前声明）**：先前「make check 全绿」不成立——分支 planning 提交遗留两处门禁破坏：`account-center.test.ts` 媒体查询正则被块内新增的 `.main > .topbar` 规则截断（已放宽为跨规则懒匹配），auth harness 哨兵 `fullVersion == 29` 未随 0030/0031/0032 升（已改 32）。修复后各环节组件全绿：build/lint、后端 33 包 go test、全部前端 npm 套件、planning-prototype-v2、4 个 shell 门禁脚本、generate-check 零漂移。期间本机 Docker Testcontainers 多次偶发 `port "5432/tcp" not found`（attention.md 已记录的已知环境故障），受影响包串行重跑全部通过。

## Review findings 处置（change review round 1）

| 编号 | 处置 |
|---|---|
| B1 幂等建单分支不派生（`PrepareCreateInScope` 死代码） | 已修：`orders.go:80` 改调 `PrepareCreateInScope`；补 handler 层 red→green 回归；测试 router 补齐 `WithDeliveryPolicyProvider`（此前与生产接线不一致，是 B1 得以溜过的结构性原因） |
| I2 backfill 无迁移测试 | 已补（见上 S8） |
| I3 cancelled 语义不一致 | 已在 roadmap §4.2 写明：`delivery_due_at IS NOT NULL` **不等价于**「在交付队列中」，消费方必须联合 status 过滤，队列集合口径由 ITEM-4 单点定义；补 `TestDeliveryDueRetainedOnCancelledOrder` |
| I4 覆盖值无法撤销 | **已修（owner 2026-08-23 选方案 b：完整支持撤销）**：`UpdateInput.DeliveryDueAt` 改 `nullable.Nullable[time.Time]`；OpenAPI PATCH body 标 `nullable: true`（codegen 随之产出三态类型，handler 只需 `domainNullableDateFromAPI` 转换，无需手工解析原始 JSON）；`applyDeliveryDue` 显式 null → 清 override 并按当前 shot_at 重新派生（无 shot_at 则清空）。建单侧保持二态（无撤销语义），经 `nullableFromPointer` 提升 |
| N5 死分支 | 已合并进守卫条件并补注释说明前置不变量 |
| N6 契约描述漏「或变更」 | 已修 OpenAPI 三处，并补「仅已到达拍摄且未取消的订单接受该字段」 |
| N7 显式覆盖不过校验 | 已加 `validateDeliveryDue`：未拍摄 / 已取消订单拒绝 `delivery_due_at`，补两条用例 |
| N8 每次 PATCH 多一次 settings 查询 | 未做，非热点路径，记录备查 |

## Change review round 2（follow-up 复审，2026-08-24）

- **reviewer 创建**：宿主 subagent（继承主会话线路，无 model 覆盖）。异构候选均不可用：codex CLI 未安装；claude CLI 上游 503 `model_not_found`。原 round 1 reviewer session 跨会话不可恢复，按协议创建 fresh reviewer（原因记录于案）。
- **结论「可合」**：round 1 全部 findings 逐项核销 resolved（N8 判「取舍可接受，记录备查」）；无 blocking；两个门禁修复（account-center 正则放宽、harness 哨兵 29→32）被判正确且必要。
- **I-1（important）已修**：S1 场景补 settings 服务层 + httpapi 层双测试（见证据节）；work 文档证据表述同步纠正（10 例 → 15 例）。
- **N-A 已修**：删除仅剩测试调用的 `PrepareCreate` 公共接缝；`order_test.go` 改用 `PrepareCreateInScope`。
- **N-B 已修**：`dataexport` 的 `delivery_due_at` 投影改 `clock.DateOnly` 归一化，与 order/dashboard 投影统一。
- **N-C 处置（对 reviewer 建议的纠正）**：「存量行带非默认 SLA 参与 backfill」是不存在路径——`delivery_sla_days` 在本迁移内以 DEFAULT 14 创建；`COALESCE` 只兜 LEFT JOIN 未命中（due-nosettings 已覆盖）。删除死字段 `sla` 并补注释说明，不为不可达分支造测试。
- **N-D 已记**：覆盖日期无下界是故意（补录逾期应交日合法），见边界第 7 条。

## Change review round 3（终审，2026-08-24）

同 reviewer follow-up。结论**「可合」**：round 1/2/3 全部 findings 核销，blocking 与 important 双清零；round 2 的 N-C 建议被实现方驳回且裁决成立（「存量行带非默认 SLA 参与 backfill」物理不可构造——列在 0032 内以 DEFAULT 14 创建，reviewer 确认原建议属审查失误）；唯一非正式跟进（本节状态滞后）随本条更新闭环。非阻塞项按 attention.md 三轮上限正式转为残余风险（N8 settings 查询、导出 schema_version 待 ITEM-6、down 迁移丢新列数据、backfill 后改 shot_at 按当时 SLA 重算）。

## 未决

- **导出 schema_version**：本条新增字段已进导出面，按 Epic ITEM-6「不逐子项各自为政」不在此 bump，由 ITEM-6 统一决策。
- **N8 每次 PATCH 多一次 settings 查询**：非热点路径，未优化，记录备查。

## 顺带修正（owner 2026-08-23 同意）

- roadmap §4.6 `schema_version` 由 `2` 校正为 `3`，与实现一致（漂移由先前 creative-planning 系列引入，非本条）。

## 状态

**完成**。change review 三轮闭环（round 1 修复 9 条 → round 2 可合+1 important/4 nit → round 3 终审可合、双清零）；分支遗留门禁破坏（account-center 正则、auth harness 哨兵）已随本子项修复；里程碑提交见 git 历史（feat(order) · dashboard-v2 ITEM-1）。
