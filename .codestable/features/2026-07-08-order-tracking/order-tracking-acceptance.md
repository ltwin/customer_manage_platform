---
doc_type: feature-acceptance
feature: 2026-07-08-order-tracking
status: passed
accepted: 2026-07-09
round: 1
---

# order-tracking 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-09
> 关联方案 doc：`.codestable/features/2026-07-08-order-tracking/order-tracking-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：

- [x] `POST /orders` 默认建单：`backend/internal/order/repository.go` + `httpapi/orders.go` 落地；QA-001/后端集成测试覆盖默认 consulting、最小建单、客户/套系引用错误、price 负数。
- [x] `POST /orders` 补录直达：`ApplyCreateInput` + create service 覆盖目标状态、shot_at/delivered_at 必填、closed 结清门禁、archived 套系分叉；QA-001 通过。
- [x] `PATCH /orders/{id}` 状态推进与字段修正：`ApplyUpdateInput`、`canTransition`、D6b 不变量在 domain/service/repository 测试中覆盖；QA-002 通过。
- [x] `GET /orders` 列表：customer/status/unpaid_balance/page 过滤、引用摘要、`created_at DESC, id DESC` 默认排序均在 repository/httpapi/browser QA 覆盖；QA-003 通过。
- [x] `DELETE /orders/{id}`：终态 204、非终态 `409 order_not_terminal`、跨账号/不存在 404；QA-004 覆盖终态删除与聚合即时减少。

**名词层“现状 → 变化”逐项核对**：

- [x] `Order` / `OrderStatus`：OpenAPI、Go 生成物、TS schema 均同步；`make generate` 前后 shasum 一致。
- [x] `backend/internal/order/`：新增 model/service/repository/state_machine 与测试，状态机不在 handler 层。
- [x] `OrderListItem`：列表项携带 `customer_display_name` / `package_name?`，避免前端 N+1 拼装。
- [x] customer/package 聚合字段：从 0/null 桩改为真实订单读数；customer merge 迁移订单，package delete in-use 接通真实 orders 表。

**流程图核对**：

- [x] PATCH 主流程节点均有代码落点：handler 绑定 → service/repository `WithinTx` + `FOR UPDATE` → `ApplyUpdateInput` 先字段后不变量后跃迁 → scoped update → 返回 200/400/409。

结论：接口契约与设计一致，未发现需回填设计或修代码的偏差。

## 2. 行为与决策核对

**需求摘要逐项验证**：

- [x] 后端订单域闭环：建单、补录、跃迁、不变量、收款标记、查询、终态删除均由非缓存后端测试和 QA 报告覆盖。
- [x] 两笔随域生长债：customer/package 聚合真实计算、customer merge 迁移订单、package in-use 校验均已接通并有三域测试。
- [x] 前端订单面：客户详情约单 tab、全局 `/orders` 页、补录模式、unpaid_balance 筛选、移动布局截图均由 QA browser evidence 覆盖。

**明确不做逐项核对**：

- [x] 不碰支付：源码无支付网关/金额流水集成；定金/尾款仅为布尔标记。
- [x] 不做档期联动：本 feature 不注册 schedule 后端；订单状态只由订单操作显式驱动。
- [x] 不生成提醒 / dashboard：相关 tag 未进入后端 include-tags，范围守护测试仍通过。
- [x] 不做批量导入 / 批量操作：补录为逐单模式，无 CSV/批量端点。
- [x] 不任意回退状态：状态机矩阵仅允许 14 条合法边与非终态取消。

**关键决策落地**：

- [x] D3/D11：跨域聚合在各域 repository 内查 orders 表，AccountScope 只通过受控 `ScalarAggregate` 扩展；未抽 order 域 Go 接口。
- [x] D5/D6/D6b：状态机、PATCH 混合语义、字段修正不变量均落 domain/service 层并有测试。
- [x] D8：customer/package 聚合口径与 package delete any-reference 口径分离。
- [x] D15/D16：补录直达与终态物理删除已收编到契约、实现、测试与 UI。

**挂载点反向核对（可卸载性）**：

- [x] `backend/oapi-codegen.yaml` include-tags `orders`：存在，生成 create/list/update/delete server interface。
- [x] `router.go` 与 `RouterDeps.Orders`：四条受保护 orders 路由已注册。
- [x] `0005_orders` migration：up/down 均落盘。
- [x] 前端 `/orders` route + AppShell nav：全局订单页可达。
- [x] 客户详情约单 tab：`CustomerDetailPage` 接入 `OrderWorkspace`。
- [x] 反向核查：`rg orders / OrderWorkspace / createOrder / deleteOrder / ScalarAggregate` 命中均落在契约挂载点、订单域实现、聚合接通、测试或原型旧页面范围内；未发现清单外隐藏挂载点。
- [x] 拔除沙盘推演：按 design §4 逆序删除前端入口、HTTP 路由、include-tags、order 域与迁移后，用户视角订单能力消失；customer/package 聚合与 in-use 可回退为桩态。

## 3. 验收场景核对

对照设计第 3 节 A1-A30 与 QA 报告复核：

- [x] A1-A1h / A9b 建单、补录、引用校验、price：QA-001 pass。
- [x] A2-A9c 状态机、前跳边、时间戳、不变量、PATCH 回滚：QA-002 pass。
- [x] A10-A14 查询、引用摘要、unpaid_balance、排序、跨账号：QA-003 pass。
- [x] A15-A20 / A29-A30 聚合、merge、in-use、终态删除：QA-004 pass。
- [x] A21-A23 前端可见状态：QA browser 证据覆盖桌面与 375px；101+ 客户/套系选择、补录状态不含 consulting 已复核。
- [x] A24-A28 明确不做与范围守护：QA-005 pass。

**review 报告重点复核**：

- [x] review §4 无 blocking / important / nit；suggestion REV-008/REV-009 均为后续规模优化，不阻塞本轮。
- [x] review §5 QA Focus 已覆盖：101+ 客户/套系浏览器场景、补录 consulting 排除、并发锁边界、selected/retouching 筛选均由 QA 或后端测试覆盖。

**QA 报告重点复核**：

- [x] 验证证据来源：`.codestable/features/2026-07-08-order-tracking/order-tracking-qa.md`，frontmatter `status: passed`。
- [x] QA matrix 覆盖 design 关键场景与 review QA focus；feature 性质为 functional，核心路径有运行证据。
- [x] failed / blocked 项为 none。
- [x] residual risk 不承载核心验收缺口：普通 `make check` 的 generate-check 预期差异已用隔离 index 全量 `make check` exit 0 复核。
- [x] Evidence pack：`evidence/*.png` 已落盘；DoD/Gate results 不适用（本 feature 非 goal/gate 包装），CodeStable commit gate 已通过。

## 4. 术语一致性

- 订单 / Order：CONTEXT.md 已有定义；代码、OpenAPI、UI 使用 Order / 订单，客户详情 tab 保留原型「约单」作为 UI 语境，不引入新领域同义词。
- OrderStatus：八态与 roadmap §4.2 一致。
- 定金/尾款标记：代码为 `deposit_paid` / `balance_paid`，无支付集成。
- 补录直达 / 终态删除：design、roadmap、OpenAPI、实现、测试同口径。
- 防冲突：未发现 pay/alipay/wechat_pay、slot/schedule/reminder 的订单实现混入；OpenAPI 中未实现 tag 仍为契约文档与范围守护对象。

## 5. 领域影响盘点（提示而非代写）

- [x] 订单 / Order（术语）：`requirements/CONTEXT.md` 已有定义，不需要本轮 `cs-domain` 补 CONTEXT。
- [x] OrderStatus / 补录直达 / 终态删除（流程约束）：roadmap §4.2/§4.3 与 design 已记录，属于本 feature 契约；暂不需要单独 ADR。
- [x] AccountScope.ScalarAggregate（结构选择）：已有 `.codestable/compound/2026-07-06-accountscope-fail-loud.md` 覆盖 fail-loud 原则；本次仅受控扩展并有测试，不需要新 ADR。
- [x] 跨域聚合读模型落点（结构/约定）：design §2.5 已建议沉淀 convention。它是后续 schedule/reminder/dashboard 可复用模式，建议退出后走 `cs-keep`，不在 accept 里代写。

## 6. requirement delta / clarification 回写

- [x] 判定：`requirement` 原为空 + 本 feature 新增用户可感能力 → 需要 owner-approved backfill。
- [x] 已写 approval：`.codestable/features/2026-07-08-order-tracking/approval-report.md`，owner 回答“按照你的建议”，批准 Option 1。
- [x] 已 backfill：新增 `.codestable/requirements/order-tracking.md`，frontmatter `status: current`。
- [x] 已更新索引：`.codestable/requirements/VISION.md` 将 `order-tracking` 纳入 current，并从待起草能力列表移除「订单记录」。
- [x] 已回写 feature link：design frontmatter `requirement: order-tracking`。

## 7. roadmap 回写

- [x] design frontmatter：`roadmap: photographer-private-crm`、`roadmap_item: order-tracking`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`：`order-tracking` 已从 `in-progress` 改为 `done`，feature 保持 `2026-07-08-order-tracking`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` 第 5 节子 feature 清单：`order-tracking` 已同步为 `状态：done`、`对应 feature：2026-07-08-order-tracking`。
- [x] `validate-yaml.py --file` 对 items.yaml 与 checklist.yaml 均通过。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `attention.md` 的硬启动信息。
- [x] 其他知识出口：design §2.5 的跨域聚合读模型 convention 建议走 `cs-keep`；用户可见订单能力可考虑后续 `cs-doc-tutorial`；公开 API / 组件参考若项目需要对外文档，可走 `cs-doc-api`。

## 9. 遗留

- 后续优化点：
  - REV-008：customer/package 聚合当前按列表逐项查聚合，数据量上来后可批量聚合成页内 map。
  - REV-009：客户/套系增长到数百上千时，订单建单选择器可升级为搜索式选择器。
- 已知限制：
  - 普通 `make check` 在未 staging 的工作区上仍会因生成物相对 HEAD 有预期 diff 而 generate-check 失败；本轮已用隔离 index 证明生成物纳入 index 时全量 `make check` 通过。
  - `.codegraph/.gitignore` 是 CodeGraph 运行期 dot-dir 产物，提交范围应继续排除。
- 实现阶段顺手发现：
  - 补录直达和终态删除已在本 feature 内消解设计观察项；批量导入仍明确不做。

## 10. 最终审计

- 验证证据来源：`.codestable/features/2026-07-08-order-tracking/order-tracking-qa.md`
- Evidence sources：`.codestable/features/2026-07-08-order-tracking/evidence/*.png`；Gate/DoD result files 不适用；CodeStable commit gate 已现场重跑。
- Inline Verification Matrix：不适用，已有独立 QA 报告且 status=passed。
- 聚合命令：
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` → exit 0。
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-07-08-order-tracking/order-tracking-checklist.yaml` → exit 0。
  - `git diff --check` → exit 0。
  - `make generate` + shasum compare `api.gen.go` / `schema.d.ts` → exit 0，无新增漂移。
  - `cd backend && go test -count=1 ./internal/order ./internal/customer ./internal/package ./internal/platform/httpapi ./internal/platform/store` → exit 0。
  - 隔离 index `make check`（临时 index 纳入当前 `api.gen.go` 与 `schema.d.ts`，不改真实 index）→ exit 0。第一次包装脚本因 zsh 只读变量名 `status` 误报 exit 1，改为 `rc` 后同命令通过；项目命令本体无失败。
  - `python3 .codestable/tools/codestable-worktree-gate.py --root . --json commit --unit .codestable/features/2026-07-08-order-tracking` → exit 0，`ok=true`。
- 场景复核：re-verified 10（QA matrix 全部 + final audit 命令组）；trust-prior-verify 3（浏览器截图/DOM 证据沿用 QA 报告：desktop unpaid、101+ options、375px）。
- 交付物复核：
  - 代码：order domain、orders httpapi、迁移、customer/package 聚合、merge/in-use、frontend OrderWorkspace/OrdersPage/nav/route 已落盘。
  - 配置/schema：OpenAPI、oapi-codegen include-tags、Go/TS 生成物已同步。
  - 文档：design/review/QA/acceptance/approval/requirement/evidence 已落盘。
  - requirement：`order-tracking.md` current + VISION current。
  - roadmap：items.yaml done + 主文档 done。
- 完整工作区复核：`git status --short --untracked-files=all` 已复核；本轮 feature 产物均在 dirty/untracked 范围内；`.codegraph/.gitignore` 为无关 dot-dir 运行产物，提交时排除。
- diff 清洁度：通过；源码 targeted grep 未见本 feature 调试输出 / 临时 TODO / FIXME / XXX / 原型桩残留，命中均为 design/QA 文档规则说明。
- 知识沉淀出口：attention 无候选；跨域聚合读模型 convention 建议 `cs-keep`；用户指南/API 参考按 owner 需要后续触发。
- 结论：通过。原始契约满足，验证证据足够且在最终状态仍成立，承诺交付物真实落盘，req 与 roadmap 已闭环。
