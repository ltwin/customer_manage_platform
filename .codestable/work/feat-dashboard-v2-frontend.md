---
epic: ../epics/dashboard-v2-redesign.md
type: feat
status: 完成
item: ITEM-5
---

# feat-dashboard-v2-frontend

## 目标

Epic `dashboard-v2-redesign` ITEM-5：`DashboardPage` 接入 `GET /dashboard/v2` 聚合读模型（三层结构），清除原型写死常量；订单表单/详情落地金额与应交日写侧录入；settings 页可编辑交付 SLA 默认值；文案复制弹层复用 Calendar openings 文案。

## 现场

- 前端消费层现状（开工检索）：`DashboardPage.tsx` 为 v1 五块（due/today_slots/unpaid/churn/recent_stats）消费 `GET /dashboard`；`usePrototypeStore` 生态（`PrototypeStore.tsx`/`prototypeStoreContext.ts`/`crm/model.ts`）挂在 AppShell 顶层但已无任何页面消费者；`prototypeData.ts` 被 `scripts/prototype-contract.test.mjs`（v1 hardening）锁定保留。
- 契约（ITEM-1/2/3/4 已定稿，本子项零契约变更）：`Order`/`OrderListItem` 含 `amount_paid`/`outstanding_amount`/`paid_at`/`delivery_due_at`/`delivery_due_is_override`；PATCH body 金额无 null 语义、`delivery_due_at` 仅已到达拍摄且未取消订单接受、显式 null 撤销覆盖；`Settings.delivery_sla_days` 可 PATCH。
- 复用先例：pageReadState 消费模式、`buildCalendarModel`（CalendarPage 未来空档同款算法）、`openingsText` 文案、node --test --experimental-transform-types 测试基建。

## 边界与取舍

- **无新契约变更**：写侧字段契约已由 ITEM-1/2 定稿，未触发 roadmap §4 前置定稿门槛（开工时确认）。
- **咨询转化 / 健康度分层不接**：owner 2026-08-22 拍板基线版下放二期；v2 八块全部接入。
- **「旧五块不回退」的落地解读**：`GET /dashboard` 契约与 client 导出保留（Telegram digest 等服务端消费方不受影响）；DashboardPage 切 v2 后待办行的完成/忽略、待收明细 + 标记收讫（DEC-10 推定）行为保留；v1 churn_alerts 卡随 v2 三层结构移除（原型 v2 同样没有该卡，健康度口径 owner 已下放二期）。
- **瀑布明细弹层只做待收段**：待收明细复用既有 `GET /orders?unpaid_balance=true`（单域过滤查询，非经营指标拼装）；在途/已确认明细 v2 聚合未提供，不跨分页自行拼装（验收标准 2）。
- **利用率卡不做每日热力格**：v2 聚合只含月度汇总数字；每日格子属展示细节且需整月 slots，本版以汇总行 + 空档速览呈现。
- **未来空档（14 天）前端计算**：与 Calendar v2 同源 TS 算法（`buildCalendarModel` + settings availability），DEC-9 双实现并存期由 golden 兜底；dashboard 侧数据为辅助读，失败不阻塞主面板。
- **建单录金额即当下收款**：`amount_paid>0` 时显式带 `paid_at=now`（契约创建不自动写，近 30 天已收现金需落窗锚点）；修正路径在订单详情弹层。
- **PrototypeStore 生态删除**：`usePrototypeStore` 零消费者（grep 全库），删 `PrototypeStore.tsx`/`prototypeStoreContext.ts`/`crm/model.ts` 并解包 AppShell；`prototypeData.ts` 保留（v1 原型契约测试锁定的 foundations 数据）。
- **时区跨日语义**：`localMinutesOfInstant(iso, tz, baseDate?)` 以本地日 serial 差表达跨日分钟（可为负或 ≥1440）；date-only serial 必须与 `Date.UTC` 零点基准一致（12:00Z 锚在 Math.round .5 进位下与次日碰撞，已修并有注释）。

## 证据

- 测试先行 red → green：`scripts/dashboard-v2.test.ts`（14 用例：焦点卡/时间轴/瀑布/交付行/矩阵/空档计算）、`scripts/order-payment.test.ts`（8 用例：hydrate/校验/patch 构造/未变字段省略/覆盖撤销）、`scripts/dashboard-v2-layout.test.mjs`（3 用例：三层结构块 + 430px 断点契约）；`scripts/settings.test.ts` 追加 SLA 用例 2 条（LOAD_START→LOAD_SUCCESS 前置，避免 emptyReminders 默认 SLA=14 掩盖 seq 拒绝）。
- 适配点：`scripts/account-center.test.ts` A1–A4 字段清单纳入 `delivery_sla_days`（reminders 节新增属主字段）+ fixture 补齐 Settings 必填字段。
- make check：r1 EXIT=0（含修复前前端代码）；两处自查修复（`computeUpcomingDates` 与模型 `upcomingLocalDates` 去重、辅助读 effect 补 tick 依赖使 refetch 刷新未来空档）后 r2 唯一失败为 Testcontainers `port "5432/tcp" not found` 偶发（attention.md 记录；customer 包本子项零改动，串行重跑 `go test ./internal/customer/ -count=1 -parallel=1` ok 63.5s）；r3 单轮全绿（见下方验证记录）。
- 验收标准 8 联动：订单卡 overflow「收款 / 应交日」弹层保存 → PATCH amount_paid/paid_at/delivery_due_at → 服务端 DEC-10 推定 outstanding → 瀑布待收段与交付队列 days_left 随之变化；settings 提醒规则节「交付 SLA 天数」→ PATCH delivery_sla_days → 新派生应交日随之变化（历史订单不重写）。

## 验收

- [x] DashboardPage 展示 v2 三层结构（L1 焦点/时间轴/待办/交付、L2 瀑布/利用率/空档速览、L3 渠道矩阵），全部来自 `GET /dashboard/v2`，无原型写死常量
- [x] 前端类型一律引用 schema.d.ts（client.ts `DashboardV2` 族 + 复用 `OrderListItem`/`Settings`），无手写 DTO
- [x] 未来空档复用同一 settings/算法（buildCalendarModel + availability），文案用 `openingsText` 同源输出
- [x] 收入瀑布待收/在途来自真实字段（outstanding_amount/price），无 price/2 类推算
- [x] 订单表单（新建）可录已收金额（amount_paid + paid_at=now）；订单详情弹层可修金额/收款日/应交日（含撤销覆盖 null 路径）
- [x] settings 可编辑 SLA 默认值（提醒规则节，hydrate/edit/build 全链路 + 校验 ≥1）
- [x] 旧 `GET /dashboard` 与 client 契约导出不回退；make check 全绿（r3 单轮）+ make generate 零漂移
- [x] 无 `usePrototypeStore` 残留（生态删除，grep 清零）；375px/移动端 430px 断点契约测试
- [x] 待办完成/忽略、待收明细+标记收讫（v1 行为）不回退

## 状态

完成（change review 两轮闭环终审「可合」，随里程碑提交归档）。

## 验证记录

- r1：`make check` EXIT=0（前端为修复前代码）。
- 自查修复两处（computeUpcomingDates 去重、辅助读 effect 补 tick 依赖）后：r2 唯一失败 `TestCreateValidationRollback`（customer 包 `port "5432/tcp" not found`，Testcontainers 偶发，串行重跑 ok 63.5s）；r3 唯一失败 `TestMultiPlanTimezoneGenerationsContiguous`（reminder 包同款偶发，串行重跑 ok 63.2s）；r4 三用例同款偶发（reminder/digest + shootplanning 两用例）。当日 Docker Desktop 持续抖动（连续三轮不同包偶发、全部环境故障特征、本子项后端零改动）；机器上另有用户项目健康容器在跑，不重启 Docker。
- **r5：最终代码 `make check` 单轮全绿 EXIT=0**（后端 33 包 ok + 前端 22 个 test script 全部 fail 0 + 4 shell 门禁 + make generate 零漂移；日志 /tmp/makecheck-item5-r5.log）。
- review 修复轮定向重跑：tsc 0 错、lint 0 errors、build 过；dashboard-v2 14/14、order-payment 9/9、layout 3/3、settings 8/8、account-center 29/29、prototype 4/4、schedule 59/59。

## change review 记录

- 触发理由：登录后默认落地页整页重写（全站入口传播面广）+ 金额/SLA 写侧录入失败代价重大（直接动财务数字）；沿 epic 前四子项一致质量门槛。
- reviewer 创建：宿主 subagent fresh 同步派发（codex 未装；claude CLI 2.0.76 探针 25s+ 停滞无响应复测确认不可用——异构回落，与前四子项同因）。round 2 沿用同 reviewer follow-up（agent_6643f11e）。
- round 1（冻结 /tmp/item5-review-frozen.diff，SHA-256 d3c7607730dd312107568f39daaef1953be67492060cefa95102517445789b88）：「建议先改再合」，0 Blocking / 2 Important / 7 Nit。I-1 待收明细结算后永久 loading（验收 5 呈现回退）→ settle 成功后弹层内原地重拉；I-2 补录建单 paid_at=now 污染近 30 天已收现金窗 → backfill 不带 paid_at（同根覆盖 N6）+ hint 分支文案。N2 持平中性、N3 100 条截断提示、N5 死 CSS 两条、N7 补 0 重置用例一并修复；N1/N4 residual。
- round 2（冻结 /tmp/item5-review-r2.diff，SHA-256 cbc96db3131e9ad90a2fc478f2be3f35a37aaba871f6dd36be3ae5a07dacc6ba）：终审「可合」——7 项旧 findings 全部 resolved，N1/N4 确认 residual；新 2 Nit 不阻合并（结算后明细重放失败的提示语义、change_ratio=0 中性态测试用例），按 ITEM-4 先例记录随合并闭环不开 round 3。reviewer 两轮均独立重跑定向测试。

## 未决

- 在途/已确认收入明细弹层、利用率每日热力格（CSS 已预留）：数据不在 v2 聚合内，二期或 owner 要求时再议。
- review residual（均 Nit 级）：SLA 默认 14 在 DashboardPage 兜底与 emptyReminders 两处隐性耦合；弹层关闭手势 mouseDown（dashboard 弹层/ConfirmDialog）与 click（订单弹层）并存各有先例；结算成功后明细重放失败时提示+关弹层组合可能让用户误判结算失败（低概率）；change_ratio=0 中性态无防回归用例。

