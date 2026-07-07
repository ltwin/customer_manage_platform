---
doc_type: roadmap-review
roadmap: photographer-private-crm
status: passed
reviewed: 2026-07-06
round: 3
---

# photographer-private-crm roadmap 审查报告（round 3 · 2026-07-06 update）

> 本轮针对 2026-07-06 update（设计原型比对后的契约增量），非全量首轮审查；round 1/2（2026-07-05，changes-requested → passed，Codex 异构独立审查）结论对未触碰部分继续有效。

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`（§4.1/§4.2/§4.3/§4.5/§5/§7/§8 本次变更部分）
- Items: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`
- Related docs: `.codestable/attention.md`、`requirements/CONTEXT.md`、ADR-001/003、compound `2026-07-06-openapi-roadmap-bidirectional-check.md`、`2026-07-06-accountscope-fail-loud.md`
- Code facts checked: `api/openapi.yaml`（month_stats L1045、reminders 参数 L777、SocialPlatform schema——漂移声明属实）、`backend/internal/platform/httpapi/`（router.go / auth.go / api.gen.go——platform-skeleton 已交付端点仅 healthz/login/me）、`backend/oapi-codegen.yaml`（include-tags 过滤）、`Makefile`（前端全量 codegen）
- 变更依据素材：Open Design 高保真原型 5 页面（dashboard / customers / customer-detail / calendar / packages + js/data.js 共享示例数据，实体模型自称对齐 §4.2）

### Independent Review

- Status: completed
- Detection: native-agent（本轮无 Paseo 工具，用宿主原生 Task agent；同类 agent 降级已记录，残余风险 = 非异构视角，见第 6 节）
- Provider / agent: Claude general-purpose subagent（只读审查）
- Raw output: 已回传结构化审查（0 blocking / 3 important / 3 nit / 2 suggestion / 2 residual-risk / 1 learning / 3 praise）
- Merge policy: 主 agent 逐条对照 roadmap/items/openapi/代码事实核验后合并；全部 important/nit/suggestion 判定成立并已在本轮修复或采纳
- Gate effect: none（reviewer 已 completed，findings 已处置）

## 2. Roadmap Summary

- Goal completion signal: 未变——全链路演示（建档→套系→订单→档期→定金→TG 摘要→dashboard 五卡）+ 全部 items done/dropped
- Module split: 未变（7 模块）；本次为 §4 契约增量 + 拍板记录，不动模块边界
- Interface contracts: 增量七项——客户/套系聚合字段（口径含 cancelled 排除规则与读模型归属）、q 匹配加 phone、reminders customer_id 过滤、Package.note、SocialIdentity 平台枚举扩展、dashboard 近 3 天待办窗口 + recent_stats 近 30 天口径（窗口边界与 cancelled 归属已闭合）、schedule-calendar 组合流程拍板（含 design 必答清单）
- Items: 11 条不增不减；minimal_loop 仍为 customer-core；依赖边零变化
- Dependency shape: DAG 无环（独立 reviewer 复核 items.yaml 与 §5 完全一致）

## 3. Findings

### blocking

none

### important（本轮发现，已全部修复）

- [x] RMR-301 `roadmap.md#4.3 dashboard` recent_stats 口径不可测试（窗口边界与 cancelled 归属未定义）
  - Evidence: 修复前仅写"近 30 天滚动，按账号时区"；§4.2 允许 delivered→cancelled，delivered_at 落窗的 cancelled 订单归属两可；与 total_order_amount 的"非 cancelled"口径不对齐
  - Impact: dashboard 条目完成信号"五卡片交叉一致核对"写不出唯一期望值
  - Resolution: 窗口 = 账号时区自然日 [今日-29, 今日] 含今日；orders_delivered/revenue_confirmed 排除当前 cancelled；orders_created 含全部状态——已写入 §4.3
- [x] RMR-302 `roadmap.md#4.3` 聚合字段 orders_count / last_shot_at 口径未到可执行级
  - Evidence: 修复前未定义是否含 cancelled；last_shot_at 标 date 而 Order.shot_at 是 date-time，截断时区未挂到 §4.1 date-only 枚举
  - Impact: customer-core 收编 OpenAPI 时写得出 shape 写不出语义；order-tracking 接通真实计算会产生实现分歧
  - Resolution: 统一"非 cancelled"口径；last_shot_at = 非 cancelled 订单 max(shot_at) 按账号时区截断，并补入 §4.1 date-only 枚举——已写入
- [x] RMR-303 `roadmap.md#5 条目6` 组合流程后订单 consulting→scheduled 跃迁责任悬空
  - Evidence: 拍板只归属"两步失败处理"给 design；POST /orders 落 consulting，走完两步订单状态与日历事实不符；悬挂 consulting 订单会抑制 churn 预警（4.4"当前无非终态订单"条件）
  - Impact: 状态与日历长期不符、流失预警被静默抑制
  - Resolution: 条目 6 备注（roadmap + items.yaml 双侧）补 design 必答清单：①是否隐式第三步 PATCH scheduled；②悬挂 consulting 订单对 churn 的抑制须被补救策略覆盖——决策权留给 feature design，roadmap 不越位拍板

### nit（已修复）

- [x] RMR-304 §5 条目 5/7 完成信号与 items.yaml notes 单侧承载——主文档已同步聚合接通与 customer_id 过滤
- [x] RMR-305 §7 OpenAPI 待办枚举漏"q 匹配加 phone"——已补全
- [x] RMR-306 §4.5 TG 摘要与 dashboard 待办窗口分叉未注明有意——已加"有意保持今日口径"说明

### suggestion（已采纳）

- [x] RMR-307 聚合读的架构归属：§4.3 已引用 4.4 同措辞（同进程读模型，repository/service 层完成，不在 handler 拼装）
- [x] RMR-308 收编 OpenAPI 时聚合字段与 detail.stats 标 required（恒 0/null 阶段即返回，接通后 shape 不变）——已写入 §7 待办

### learning

- include-tags 策略（`backend/oapi-codegen.yaml`）使 openapi.yaml 可安全领先实现：收编契约增量不触碰后端生成物、不破坏 `make check` 的 generate-check。候选沉淀 compound，建议 customer-core acceptance 时落盘。

### praise

- platform-skeleton 影响评估经代码核验无水分（已交付端点仅 healthz/login/me，均不受本次增量影响；api.gen.go 因 include-tags 不含受影响域）。
- OpenAPI 漂移处理规范：不静默改机器契约，观察项待办 + 收编时机 + compound 双向核对机制齐全。
- 「聚合字段自始存在恒 0/null」与「不新增组合端点」两个拍板符合项目反预支复杂度的一贯口径。

## 4. User Review Focus

- 用户需要重点拍板：①recent_stats 的 cancelled 排除与 [今日-29, 今日] 窗口定义（review 按"与 total_order_amount 对齐"的最小惊讶原则代拟，需 owner 确认）；②orders_created 含全部状态的口径；③二期候选记录措辞（拍摄回顾 / 人脉链金额归因）是否符合预期
- 后续 feature-design 需要重点复核：schedule-calendar 的 design 必答清单两项；customer-core 收编 OpenAPI 时按 §7 待办清单逐项核对 + compound 双向核对；聚合字段 required 策略
- 不能靠 roadmap review 完全确认的点：见第 6 节

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Granularity Gate | pass | E | §2 表未变，本次 update 不触碰 | none |
| Goal Coverage Matrix | pass | E | 八行覆盖未受影响；dashboard 行验证入口在 RMR-301 闭合后可执行 | none |
| DAG and minimal loop | pass | E | items.yaml 依赖边零变化，独立 reviewer 复核无环；customer-core 因"恒 0/null"约定不新增对 order 域依赖 | none |
| Interface contract usability | pass | E | 增量七项全部到字段/口径/时区级；RMR-301/302 修复后可直接作 OpenAPI 收编依据 | customer-core 收编时双向核对 |
| Module interface depth | pass | C | 聚合归属读模型措辞与 4.4 一致；无新 seam / 无假 adapter；组合流程拒绝 BFF 端点 | none |
| platform-skeleton 影响评估 | pass | C | router.go / auth.go / api.gen.go 代码事实核验 | none |
| OpenAPI 漂移声明 | pass | E | openapi.yaml L1045 month_stats、L777 reminders 参数与 §4 新文不一致，漂移属实且已记待办 | customer-core 启动时收编 |

Summary: E=5, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- **聚合 join 的账号隔离**：orders_count / last_shot_at / recent_stats 全是跨表聚合，子查询漏 account_id 过滤不报错、单账号阶段测不出——order-tracking / dashboard 的 code review 检查口径须显式包含"join/子查询逐个核对 AccountScope 覆盖"（compound `accountscope-fail-loud` 的基座对 join 场景的覆盖在 design 时确认）。
- **schema.d.ts 跨域大 diff**：前端 codegen 全量生成（Makefile 无 include-tags 过滤），customer-core 收编后其 PR 携带无关域 schema 变更——review 噪音，已在 §7 待办预告，customer-core design 里再明示。
- **本轮独立 review 为同类 agent**（native Claude subagent，非 round 1/2 的 Codex 异构 provider）——本轮为契约增量复审、全部核心检查有 E/C 级证据，残余风险有限；下次全量 review 建议恢复异构。

## 7. Verdict

- Status: passed
- Next: 交给用户 review（重点拍板项见第 4 节）；用户确认后更新主文档 `last_reviewed: 2026-07-06`
