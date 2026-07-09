---
doc_type: feature-design-review
feature: 2026-07-08-order-tracking
status: changes-applied
reviewed: 2026-07-09
reviewer: codex (独立 Task agent，异构 provider) + 主 agent 本地事实核验合并（round 1）；主 agent PM 视角评审（round 2，owner 指示按最优方案落地）
round: 2
---

# order-tracking · design review（round 1）

## 1. 输入与范围

审查对象：`order-tracking-design.md`（355 行，§0–§4）+ `order-tracking-checklist.yaml`（7 steps / 24 checks）。
独立 reviewer：Codex（异构 provider，只读审查，未改文件、未写报告）；主 agent 逐条本地核验其 findings 后合并，不照抄。
已核输入：attention、design、checklist、roadmap 主文档（§4 契约）、items.yaml、ADR-001/003、compound（accountscope-fail-loud / openapi-feature-tag-slicing）、design 引用的关键代码（package/customer repository、scope.go、router.go、packages.go、migrations 0002/0004、oapi-codegen.yaml、api/openapi.yaml、package_test.go）。

## 2. blocking findings

- **FDR-001（CONFIRMED）`last_shot_at` UTC 截断绕开 §4 硬契约**
  证据：`roadmap.md:108/126-127` §4 为硬约束、date-only 字段按 `Settings.timezone`（默认 Asia/Shanghai）；design D8（`:72`）与"明确不做 #7"（`:关键约束`）改为本轮 UTC。
  影响：聚合测试可绿，但 reminder / dashboard 后续按账号时区计算会与 last_shot_at 对不上，属硬契约漂移。
  修复边界：本轮改为**硬编码 `Asia/Shanghai`（roadmap 契约默认值）截断**，不用 UTC——用契约默认值即零漂移；Settings 表落地后改为读 `Settings.timezone`。同步 A15/A16 与 checkitem。
  **处置：已修**（见 §5）。

- **FDR-002（CONFIRMED）package delete in-use 与 list orders_count 口径冲突**
  证据：roadmap `:218-219` delete「被任一订单引用」即 409；`:224-225` list orders_count =「非 cancelled 计数」；现有 `package_test.go:310-320` 显式断言 cancelled 订单也阻止删除；design §2.2（`:178`）却写"把 `countOrderReferences` 已有逻辑喂给列表项"。
  影响：复用同一 helper 必二选一违约——列表把 cancelled 算进（违 §4.3），或删除放行仅 cancelled 引用的套系（违现有测试 + roadmap）。
  修复边界：拆两个口径——delete in-use 用 any-reference（含 cancelled，保留现有 helper 语义）、package list orders_count 用 `status != cancelled` 的独立聚合；两套测试分别覆盖。
  **处置：已修**（见 §5）。

## 3. important findings

- **FDR-003（CONFIRMED）D9 archived 套系建单策略未收敛，A1e 不可证伪**
  证据：design `:73` 同写"含 archived"与"倾向只允许 active"；A1e `:246` 写"默认拒绝/或放行"；checklist `:34-36` 写"按 D9 拍板口径"。roadmap `:216`「GET ?status=active 供下单选择」是强信号。
  修复边界：收敛为单一规则——**新建订单只允许引用 active 套系；archived → `400 validation_failed`**（对齐 roadmap 下单选择口径；存量订单继续引用是历史保留，非新建）。同步 A1e / checklist。
  **处置：已修**（见 §5）。

- **FDR-004（CONFIRMED）D11 AccountScope 聚合扩展 API 太抽象**
  证据：`scope.go:103-220/289-326` 无 max/sum 标量聚合；compound `accountscope-fail-loud.md:9-15` 要求显式扩展 + 测试；design 只写 `ScalarQuery/Aggregate` 方向。
  影响：账号隔离基座结构性改动,API 不定容易放开任意 SQL 或 N+1。
  修复边界：design/checklist 固化最小 API——受控标量聚合方法（固定 op 白名单 `count/max/sum` + 列名字面量校验 + cond 占位符 + null 行为约定），不支持任意表达式；补基座单测矩阵。
  **处置：已修**（见 §5）。

- **FDR-005（CONFIRMED）D6「先字段后跃迁」缺失败回滚验收**
  证据：design `:157-164/173-174`（编排）先应用字段再校验；A7/A8 只测正向；checklist `:49-51` 只覆盖成功/门禁。
  影响：若 409 时字段已部分落库会污染订单。
  修复边界：明确「字段先在内存生效、最终同一 WithinTx 提交；任何 409 整体回滚不持久化」；补「混合 PATCH 触发 409 后字段不变」验收 + check。
  **处置：已修**（见 §5）。

## 4. nit / suggestion / learning / praise / residual-risk

- **FDR-006（nit，已修）** `status == current` 对角线语义钉死：`canTransition` 对角线返回 false；service 层 `status` 缺省或 == 当前时不进跃迁校验、只做字段修正。区分二者写进 D5/D6。
- **FDR-007（nit，已修）** price 非负加 service 层校验 → `400 validation_failed`（不靠 DB CHECK 变 500）；补 A9b 负数用例 + check。
- **FDR-008（nit，已修）** D1 stale：改为"已由 owner 启动、items.yaml 已回写 in-progress"，删"从 planned 选中"措辞。
- **suggestion（采纳）** D3「各域 repository 直接查 orders 表」符合 roadmap `:199-202` 同进程读模型；已在 §2.5 留 `cs-keep` convention 钩子供 implement 后沉淀。时间戳自动写入建议用可注入 clock——已加入 §2.2 与 suggestion，implement 可选。
- **learning** D12 复合外键前提成立（customers `0002:13-15`、packages `0004:16` 均有 `(account_id,id)` 唯一键）；OpenAPI/运行面切片判断正确（`api/openapi.yaml:559-690` 三端点已固化、`oapi-codegen.yaml:11-15` 未含 orders、`router.go:66-80` 未注册）——与主 agent 本地核验一致。
- **praise** 明确不做反向验收 A24-A28 完整；挂载点把聚合/merge/in-use 归为"接管既有桩"而非独立挂载点,卸载说明清晰。
- **residual-risk** 若 owner 后续要真账号时区，last_shot_at 从 Asia/Shanghai 硬编码切到 Settings.timezone 需同步 reminder/dashboard；本轮为文件事实审查,未跑测试/生成命令（留 implement 预检）。

## 5. 修订摘要（本轮已应用到 design + checklist）

| finding | 修订 |
|---|---|
| FDR-001 | D8 + 明确不做 #7 + A15/A16 + check：UTC → 硬编码 Asia/Shanghai（契约默认值）截断 |
| FDR-002 | §2.2 + D8 + A17/A19 + check：delete in-use（any-ref）与 list orders_count（非 cancelled）拆两个口径两套测试 |
| FDR-003 | D9 + A1e + checklist step3/check：新建只允许 active 套系，archived → 400 |
| FDR-004 | D11 + checklist step3：固化受控标量聚合 API（op 白名单 + 列名校验 + 单测矩阵） |
| FDR-005 | D6 + §2.2 + 新增 A8b + check：同事务任何 409 整体回滚,补失败字段不变验收 |
| FDR-006 | D5/D6：status==current 不进跃迁校验 |
| FDR-007 | D9 + 新增 A9b + check：price 负数 service 层 400 |
| FDR-008 | D1：措辞改为已启动/已回写 |

## 6. 下游需要注意

- last_shot_at 时区口径是跨 feature 债（Asia/Shanghai 硬编码 → Settings.timezone），reminder-engine / dashboard 落地时须一并接通,否则口径分叉。
- package 域两个 count 口径（delete any-ref / list 非 cancelled）是 order-tracking 引入的永久区分,后续勿再合并。

## 7. Verdict

- Status: **changes-requested → 修订已应用,待用户整体 review**
- 独立 reviewer（Codex）findings 全部本地核验通过、无驳回,已全部修订进 design + checklist。
- Next: 交给用户整体 review（Phase 6）；用户放行后 `status: draft → approved`。

---

# order-tracking · design review（round 2 · PM 视角，2026-07-09）

## 1. 输入与范围

审查视角：资深产品经理（状态机设计 + 需求描述的产品体验与完整性），刻意避开 round 1 已覆盖的技术项。已核输入：design（round-1 修订版）、roadmap §4.1-§4.3、OpenAPI（Order/OrderListItem/listOrders 实测核对）、brainstorm 原始诉求、前端原型 Order 桩。owner 指示"按最优方案优化"，本轮 findings 已全部落地（roadmap 契约 update + OpenAPI 收编 + design/checklist 修订），处置见 §3。

## 2. findings

- **PMR-001（blocking，已修）状态机不变量只在跃迁时刻保证，字段修正路径可绕过**：closed 订单 PATCH `balance_paid:false` 走字段修正路径不触发任何门禁 → "closed 但未结清"矛盾态；`nullable` 支持把已 shot 订单的 shot_at 置空 → 破坏 last_shot_at 聚合与 churn 数据基础；consulting 订单可预写 shot_at 污染聚合。终态订单允许改哪些字段（cancelled 补 note 是真实需求）设计未着一字，验收零覆盖。**处置**：§4.2 新增「字段修正不变量」三条（恒结清/时间戳一致/终态修正面）；design 新增 D6b + A6c/A7b/A9c 验收 + checklist 独立 check。
- **PMR-002（high，已修）全局订单页无客户名/套系名数据来源**：Order shape 只有 id 引用，`GET /orders` 列表无摘要字段——照原契约实现的 A22 页面不可用，前端被迫 N+1 或拉全量客户。round-1 的"本 feature 无需改契约"结论不成立。**处置**：roadmap §4.3 列表项附 `customer_display_name`/`package_name?`；OpenAPI 新增 `OrderListItem`（allOf 模式同 CustomerListItem）并已 `make generate` 同步；design 新增 D13（join 或页内 IN 批量，禁 N+1）+ A10b。
- **PMR-003（high，已修）两个口径问题**：① 强制线性八态与 Package `retouch_count=0`/`raw_delivery_count` 商品形态自相矛盾——直出单被迫伪造 selected/retouching 假状态；② unpaid_balance 宽口径（仅排除 cancelled）把 consulting/scheduled 常态未结清单混入"未收尾款"，名实不符。**处置**：§4.2 跃迁表加前跳边 `shot→delivered`、`selected→delivered`（合法边 12→14，仍禁回退与其余跳步）；§4.3 unpaid_balance 收窄为 `status ∈ {shot..delivered}`（"已进入交付链条且未结清"）；D5/D10/A2b/A12 同步。
- **PMR-004（high，已修）列表排序未定义**：无稳定排序则分页遍历本身不可靠（翻页重复/漏单）。**处置**：§4.3 钉死缺省 `created_at DESC, id DESC`，不加 sort 参数；design 新增 D14 + A13 排序断言。
- **PMR-005（medium，已修）UI 面缺口打包**：套系下拉未限定 active（选中即撞 400）、推进 shot/delivered 静默写 now（次日补记时间系统性偏移）、409 不应是用户可达错误（只暴露合法动作）、cancel 无确认无 note 引导（cancelled 语义过载：真实取消/坏账/误操作不可区分）、无标题订单无兜底、定金与定档零关联未显式决策、price null 推进无提示（统计低估）。**处置**：design §2.2 新增「交互契约」7 项 + A21/A22/A23 扩展 + 明确不做 #8（定金不设门禁为显式决策）+ D7 前端配合。
- **PMR-006（observation → 已由 owner 拍板消解，见 §3b）**：① 历史订单补录缺位——consulting 起步 + 逐跳推进使补录笨重，老客户聚合为空将致 reminder-engine 上线误报；② 误操作 cancelled 单不可物理删 + delete in-use any-ref 口径 → 一次误触永久锁死套系删除。两条初判为跨 feature 观察，owner 于 2026-07-09 当日直接拍板解决（补录直达 + 终态删除），处置见 §3b。

## 3b. round-2 增补（owner 拍板，2026-07-09）

review 汇报后 owner 追加两项决策，直接消解 PMR-006 的两条观察，均为契约级变更、已走 roadmap update（§8 第二条 2026-07-09 条目）并同步 OpenAPI/design/checklist：

- **补录直达（→ design D15）**：`POST /orders` 扩全 shape，status 可直达八态任意值、不必逐级跃迁。设计取舍：创建复用 D6b 同一套不变量校验器（建单 = 从"无"直达目标状态的特殊跃迁）；补录时间戳必须显式（≥shot 须 shot_at、≥delivered 须 delivered_at，缺省 now 必错 → 400 fail loud，与 PATCH 跃迁 auto-now 有意不同——跃迁贴实时、补录必为过去）；closed 须已结清；补录允许引用 archived 套系（历史真实性优先），常规建单仍只许 active（FDR-003 规则不变）；批量导入仍不做。新增验收 A1f/A1g/A1h + UI 补录模式（交互契约 8）。
- **终态物理删除（→ design D16）**：新增 `DELETE /orders/{id}`，仅 closed/cancelled → 204，非终态 → 409 order_not_terminal。设计取舍：聚合为实时查询，删除即时反映（删 closed 单减少统计，UI 确认明示）；slot 引用拦截 409 order_in_use 随 schedule-calendar 生长（本轮不写探测代码），reminder 引用不拦截（引擎自动 dismiss，随其 design 细化）；原"订单只 cancel 不物理删"（旧 A28）推翻，A28 改写为"不做批量操作/导入"反向核对。新增验收 A29/A30（含"删垃圾 cancelled 单解锁套系删除"链路用例）+ UI 删除入口（交互契约 9）。

## 3. 修订落点摘要

| finding | roadmap | OpenAPI | design | checklist |
|---|---|---|---|---|
| PMR-001 | §4.2 字段修正不变量 | PATCH 409 描述 | D6b、A6c/A7b/A9c、mermaid、DoD | step2/3 + 不变量 check |
| PMR-002 | §4.3 列表引用摘要 | OrderListItem + listOrders 响应 | D13、§2.1 shape、A10b/A22 | step3 + 摘要排序 check |
| PMR-003 | §4.2 前跳边、§4.3 口径 | unpaid_balance 参数描述 | D5/D10、A2b/A3/A12、术语表 | 跃迁/口径 check |
| PMR-004 | §4.3 默认排序 | listOrders summary | D14、A13 | 摘要排序 check |
| PMR-005 | — | — | §2.2 交互契约、明确不做 #8、D7、A21-A23 | step6/7 + A23 check |
| PMR-006① 补录 | §4.2 补录直达、§4.3 POST 全 shape | POST body/409 描述 | D15、A1f/A1g/A1h、明确不做 #9 改写 | step1/3/6 + 补录 check |
| PMR-006② 删除 | §4.3 DELETE 端点、§5 条目 7/8 备注 | DELETE /orders/{id} | D16、A29/A30、A28 改写、挂载点/卸载四路由 | step1/3/5/6 + 删除 check |

roadmap §8 已记 2026-07-09 变更条目（五项契约 update + 动因 + 受影响条目）；`make generate` 已跑，`schema.d.ts` 同步（api.gen.go 不受影响，orders 未进 include-tags）；checklist 顺手清理了 round-1 遗留的重复 check（A15/A16 的 UTC 旧版与 Asia/Shanghai 修订版并存）。

## 4. 下游需要注意（round 2 增量）

- 前跳边使 reminder-engine 的 follow_up/churn 输入面不变（仍只依赖 delivered_at/shot_at），但 selected/retouching 不再是"必经态"——后续若有按状态驻留时长的分析需求，注意直出单无这两态。
- unpaid_balance 订单页口径（shot..delivered）与 dashboard unpaid_orders 卡口径（delivered-only）是有意的宽窄分工，dashboard 条目落地时勿对齐成同一个。
- **订单可物理删除的跨域涟漪（2026-07-09 拍板）**：schedule-calendar 落地时须接通"被 shoot slot 引用的终态订单 DELETE → 409 order_in_use"（条目 7 备注已记，验收补用例）；reminder-engine 须定义"引用已删订单的 pending reminder 自动 dismiss/跳过"（条目 8 备注已记）；data-export 无需改动（已删订单自然不在导出集）。
- 补录直达已消解"reminder-engine 上线前老客户 churn 基线"风险；但补录数据质量依赖 owner 录入的时间戳真实性，无系统校验（信任单人 owner）。

## 5. Verdict（round 2）

- Status: **changes-applied**——PMR-001~005 全部落地进 roadmap/OpenAPI/design/checklist；PMR-006 两条观察由 owner 当日追加拍板（补录直达 + 终态删除，§3b）一并落地；design 维持 `status: approved`（owner 直接指示落地）。
- Next: implement 按修订后 design + checklist 执行；S1 预检时 `make generate` 应零漂移（契约收编已在 design 阶段完成，含 POST 全 shape 与 DELETE 端点）。
