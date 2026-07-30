---
doc_type: feature-acceptance
feature: 2026-07-08-package-catalog
status: passed
accepted: 2026-07-09
round: 1
---

# package-catalog · 套系目录验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-09
> 关联方案 doc：`.codestable/features/2026-07-08-package-catalog/package-catalog-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `POST /api/v1/packages`：合法必填 + 可选交付参数返回 201 Package；服务端写 `account_id`，默认 `status=active`。证据：`TestPackageAPIRoundtrip`、`TestCreateListAndValidation`、QA-003、2026-07-09 browser smoke。
- [x] `POST /api/v1/packages` 错误路径：缺 `base_price`、缺 name、非法 enum、负数 / 超界整数返回 400 `validation_failed` 且不写入；`base_price=0` 合法。证据：`packages_test.go`、`package_test.go`、QA-003。
- [x] `GET /api/v1/packages?status=`：缺省 active，支持 archived / all，分页与 total 可核对；列表项含 `orders_count=0`。证据：`TestListStatusPaginationAndIsolation`、`TestPackageAPIRoundtrip`。
- [x] `PATCH /api/v1/packages/{id}`：只传 `base_price` 只改价；交付参数传 null 清空；`note` 可改 / 清空；`status` 可 active ↔ archived。证据：`TestUpdatePartialAndClearableFields`、`TestPackageAPIRoundtrip`、浏览器改价为 `¥680.55`。
- [x] `DELETE /api/v1/packages/{id}`：无引用 204 物理删除；不存在 / 跨账号 404；有订单表且任一订单引用时 409 `package_in_use`。证据：`TestUpdateStatusAndDeleteIsolation`、`TestDeletePackageInUseWhenOrdersTableExists`、`TestPackageAPIErrorPathsAndScope`。

**名词层“现状 → 变化”逐项核对**：
- [x] `Package`：`packages` 表、`pkgcatalog.Package`、OpenAPI / Go / TS 类型均包含设计字段；`account_id` 从 AccountScope 写入。
- [x] `PackageListItem`：`orders_count` 从第一天返回；本轮恒 0，order-tracking 接通真实计算。
- [x] `CreateInput` / `UpdateInput`：创建必填字段、PATCH 部分更新、clearable nullable int、`note` 同步已落地。
- [x] `PackageService` / repository：`backend/internal/package` 新增 service/repository；service/repository 不 import `gin`，handler 只做 HTTP 适配。
- [x] OpenAPI PATCH note + DELETE 端点：`api/openapi.yaml`、`api.gen.go`、`schema.d.ts` 已同步；`backend/oapi-codegen.yaml` include-tags 含 packages。
- [x] 前端 client / PackagesPage：`listPackages` / `createPackage` / `updatePackage` / `deletePackage` 类型均来自 `schema.d.ts`；PackagesPage 已从原型桩迁移到真 API。

**流程图核对**：
- [x] 图中“已登录 → 套系列表 / 表单 → 校验 → 创建/更新 → 上下架 → 删除 → status 过滤 → AccountScope 查询”的节点均有落点：`router.go` 注册四条受保护路由，`packages.go` 做 HTTP 适配，`service.go` 做校验，`repository.go` 做 AccountScope 持久化，`PackagesPage.tsx` 做 UI 编排。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 受保护 Web 内可新建 / 编辑 / 上架 / 下架 / 删除套系：API 集成测试、QA 浏览器报告、2026-07-09 browser smoke 均通过。
- [x] 后端落套系持久化且 API shape 与 roadmap §4 对齐：迁移、OpenAPI、生成物、HTTP handler、service/repository 均落盘。
- [x] PackagesPage 从原型内存桩迁移真 API：`rg` 未命中 `usePrototypeStore` / `prototypeStoreContext`；浏览器 smoke 覆盖新建、改价、下架、删除。

**明确不做逐项核对**：
- [x] 不实现订单 / 档期域行为：`/api/v1/orders` 仍 404；本 feature 仅保留 OpenAPI 契约与测试模拟表，未注册 order/schedule 路由。
- [x] 删除 in-use 本轮不查真实订单表：真实 orders 表未落地；领域方法对无表返回 0 放行；测试用临时 orders 表验证未来 409 seam。
- [x] 下架无 in-use 校验：`PATCH status=archived` 不调用 `countOrderReferences`，测试与浏览器均可直接下架。
- [x] 不做促销 / 折扣 / 阶梯定价 / 套系复制 / 排序拖拽：源码 grep 未命中促销、折扣、阶梯字段或入口。
- [x] 不引入 UI 组件库：`frontend/package.json` 未新增 antd / MUI / chakra 等 UI 库。

**关键决策落地**：
- [x] D1 推进条目：items.yaml 中 `package-catalog` 初始为 `in-progress` 且 feature 指向本目录；owner approval A 后已回写为 `done`。
- [x] D2 模块归属：新增 `backend/internal/package`，Go 包名 `pkgcatalog`，handler 在 `platform/httpapi/packages.go`。
- [x] D3 OpenAPI/codegen：PATCH note + DELETE endpoint + packages tag 均落地，`make generate` 零漂移。
- [x] D4/D9 上下架与删除语义：`active|archived` 是持久状态；删除是操作；in-use seam 可运行非空桩。
- [x] D5 AccountScope 复用：未扩展 store 基座，所有 package 读写经 `store.AccountScope`。
- [x] D6 聚合字段：`orders_count=0` 从列表返回。
- [x] D7 PATCH 部分更新：动态 setClause + nullable int 清空语义有单测。
- [x] D8 前端原型桩迁移：真 API client 与页面接线已通过 build / lint / browser smoke。

**编排层“现状 → 变化”逐项核对**：
- [x] 契约线：OpenAPI → Go server types / TS schema 生成链路通过。
- [x] 写入线：HTTP adapter → service 校验 → repository AccountScope Insert/Update。
- [x] 读取线：status 过滤、分页、created_at desc + id desc 排序、orders_count shape。
- [x] 删除线：WithinTx 先锁 package，再计算引用数，再删除；无表放行，有表引用 409。
- [x] 前端线：列表 loading/error/empty、status filter、dialog、单位换算、delete confirm 均落地。

**流程级约束核对**：
- [x] 错误语义：400/401/404/409 均走 ErrorEnvelope；`package_in_use` code 已加入 envelope。
- [x] 事务与一致性：更新与删除均使用 `scope.WithinTx`；删除 in-use 检查在 tx 内执行。
- [x] 账号隔离：packages 表带 `account_id`；跨账号列表不可见、PATCH/DELETE 404。
- [x] 幂等性：POST 非幂等；DELETE 后重复删除 404，与 design 一致。
- [x] 可观测点：沿用 platform 请求日志；无新增敏感日志。

**挂载点反向核对（可卸载性）**：
- [x] API 契约与 codegen：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、`api.gen.go`、`schema.d.ts`。
- [x] 数据库 schema：`0004_packages.up.sql` / `.down.sql`。
- [x] 受保护 API 路由：`router.go` + `packages.go` 四条 packages route。
- [x] 后端域模块：`backend/internal/package/{model,service,repository,test}.go`。
- [x] 前端套系数据源：`client.ts` packages 方法、`PackagesPage.tsx`、`packagePrice.ts`、`package-price.test.ts`。
- [x] 反向 grep：package 相关命中 325 行，均落在上述挂载点、feature spec、roadmap/req 或测试证据内；未发现清单外 runtime 挂入点。
- [x] 拔除沙盘推演：逆向移除上述五类挂载点后，系统回到“后端无 packages route / 前端不能真 API 管套系”的骨架；残留只会是 roadmap/req/spec 记录与截图证据，不影响 runtime。

## 3. 验收场景核对

对照方案第 3 节关键场景清单：

- [x] A1 `make check`：2026-07-09 重跑 exit 0。
- [x] A2 `make generate && git diff --exit-code -- api.gen.go schema.d.ts`：2026-07-09 重跑 exit 0。
- [x] A3 创建套系：API / service 测试与 browser smoke 通过。
- [x] A4 创建校验：缺字段、非法 enum、负数 / 超界、0 元合法均覆盖。
- [x] A5 PATCH 部分更新：只改价、null 清空、note 清空均覆盖；browser smoke 验证 `0.05 → 680.55`。
- [x] A6 上架 / 下架：PATCH status=archived / active 覆盖；下架无 in-use。
- [x] A7 默认 active 列表：账号内 active、created_at desc、orders_count=0 覆盖。
- [x] A8 archived/all 与非法分页：service + HTTP 测试覆盖。
- [x] A9 不存在 / 跨账号 id 404：service + HTTP 测试覆盖。
- [x] A10 双账号隔离：service + HTTP 测试覆盖。
- [x] A11 浏览器完整流程：2026-07-09 in-app browser smoke 新建 `验收套系 ...` → 显示 `¥0.05` → 编辑为 `¥680.55` → 下架 active 消失 / archived 可见 → 删除确认 → 删除后列表移除。
- [x] A12 UI 状态与 polish：QA 截图覆盖 created/edited/archived/mobile；2026-07-09 375px viewport 复核 `innerWidth=375`、`scrollWidth=360`，无横向溢出。
- [x] A13 范围守护：grep 无原型桩 import；无订单 / 档期行为实现；删除 in-use seam 非空桩。
- [x] A14 清洁度：`git diff --check`、`git diff --cached --check` exit 0；feature 相关源码 grep 无 `console.log` / `fmt.Println` / TODO / FIXME / XXX / debugger。
- [x] A15 DELETE in-use 语义：无引用 204；测试临时 orders 表引用返回 409 `package_in_use`；order-tracking 仍负责真实订单表接通。

**功能性前端验证**：
- [x] Desktop browser：2026-07-09 in-app browser smoke 覆盖新建 / 改价 / 下架 / 删除主路径。
- [x] Mobile browser：2026-07-09 375px viewport 复核无横向溢出；QA 报告保留 `qa-packages-mobile-375.png`。

**review 报告重点复核**：
- [x] 价格输入 review focus：`package-price.test.ts` + browser smoke 覆盖两位小数与不静默四舍五入。
- [x] active / archived / all 切换旧数据风险：QA-006 / QA-009 覆盖。
- [x] mutation 与 load-more 并发：QA 已覆盖，accept 未发现反证。
- [x] 删除被订单引用 409：service / HTTP 测试临时 orders 表覆盖。
- [x] residual risk“提交前整体 stage”：仍是提交流程注意，不影响验收证据；最终 commit 前必须统一 stage。

**QA 报告重点复核**：
- [x] 验证证据来源：`.codestable/features/2026-07-08-package-catalog/package-catalog-qa.md`，frontmatter `status=passed`。
- [x] QA matrix 覆盖 A1-A15、review QA focus、命令、API、浏览器、移动端与清洁度。
- [x] failed / blocked：none。
- [x] residual-risk：Browser Bridge 未连接 Chrome tab，已用 headless Chromium Playwright；accept 另用 in-app browser smoke 复核主路径，风险不承载核心缺口。
- [x] Evidence pack / Gate / DoD：未启用 goal/gate evidence pack；standalone QA 报告 + accept 命令 / browser evidence 足够。

## 4. 术语一致性

- 套系 / Package / pkgcatalog：feature runtime 与测试相关 grep 325 行，命中均在 package 域、packages HTTP adapter、OpenAPI/生成物、前端 PackagesPage/client/test 和 spec/roadmap/req 内。
- `ShootType` / `PricingMode` / `PackageStatus`：枚举与 OpenAPI `portrait|cosplay|other`、`per_duration|per_photo|fixed`、`active|archived` 一致。
- 上架 / 下架 / 删除：UI 使用商品语言，底层持久态仍为 `active|archived`，删除不是状态。
- 防冲突：feature runtime grep 未命中 `usePrototypeStore` / `prototypeStoreContext` / `discount` / `promotion` / `tier` / 新 UI 库；没有把「用户」作为领域术语引入 package 域。

## 5. 领域影响盘点（提示而非代写）

- [x] 候选：上架 / 下架 / 删除（术语 / 流程约束）。结论：roadmap §4.2/§4.3 已记录商品心智，CONTEXT.md 尚未单列“上架/下架”。建议后续可走 `cs-domain` 将“上架 / 下架 / 删除”作为 Package 的长期语义补入 CONTEXT；accept 阶段不直接改。
- [x] 候选：引用完整性校验接口先行、订单域后接通（结构性选择）。结论：design-review learning 与 roadmap order-tracking 备注均已登记；若 order-tracking 落地后平滑接通，建议走 `cs-keep` 沉淀为跨域前向依赖模式。
- [x] 候选：新增 package 域模块。结论：roadmap §3 已定义 package bounded module，ADR-003 已覆盖 handler 薄层；不需要新 ADR。

## 6. requirement delta / clarification 回写

- [x] 方案 frontmatter `requirement: package-catalog`。
- [x] 初始状态：`.codestable/requirements/package-catalog.md` 为 `status: draft`，本 feature 是首次实现。
- [x] 初始缺口：feature 目录内无 `*-req-delta.md`、clarification 或 approved req delta，因此先写 `approval-report.md` 停在 requirement gate。
- [x] Owner approval：2026-07-09 owner 回复 `A`，批准 `.codestable/features/2026-07-08-package-catalog/approval-report.md` 作为等价 req delta。
- [x] 已机械回写：`.codestable/requirements/package-catalog.md` 升级为 `current`，`implemented_by` 追加 `2026-07-08-package-catalog`，保留愿景正文并追加变更日志；`.codestable/requirements/VISION.md` 已把 package-catalog 移入 current。

结论：通过。requirement gate 已由 owner approval A 解除，req / VISION 已按批准范围机械回写。

## 7. roadmap 回写

- [x] 方案 frontmatter `roadmap: photographer-private-crm`、`roadmap_item: package-catalog` 均存在。
- [x] items.yaml 初始条目：`slug: package-catalog`、`status: in-progress`、`feature: 2026-07-08-package-catalog`，符合可回写前置。
- [x] 已回写：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 中 `package-catalog` 改为 `status: done`。
- [x] 已同步主文档：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` §5 package-catalog 状态改为 `done`。
- [x] YAML 校验通过：`python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`。

结论：通过。roadmap items.yaml 与主文档已同步 `done`。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的新启动必读内容。
- [x] 可复用知识分流：引用完整性校验接口先行模式建议在 order-tracking 验证后走 `cs-keep`，现在先作为领域影响候选登记。
- [x] 指南 / API 参考分流：新增 packages API 与 PackagesPage 用户可见行为，若 owner 需要外部说明，accept 后可走 `cs-doc-tutorial` / `cs-doc-api`。

## 9. 遗留

- 后续优化点：`order-tracking` 接通真实 orders 表后，必须把 packages `orders_count` 真实计算与 DELETE 被引用 409 纳入验收。
- 已知限制：order-tracking 接通前 `orders_count` 恒为 0，真实订单引用删除 409 依赖后续订单域落地；当前已用临时 orders 表测试 seam。
- 实现阶段顺手发现：提交前需要统一 stage 当前混合 staged/unstaged/untracked 工作区，避免 OpenAPI 源、生成物、helper/test 或 CodeStable 产物错位。

## 10. 最终审计

- 验证证据来源：`.codestable/features/2026-07-08-package-catalog/package-catalog-qa.md` + accept-inline browser smoke。
- Evidence sources：同目录实现期 / QA 截图；未使用 goal/gate evidence pack。
- 聚合命令：
  - `make check` -> exit 0；frontend build、webui sync、backend build、golangci-lint、oxlint、backend tests、package price tests、generate-check 全绿。
  - `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` -> exit 0。
  - `cd backend && go test ./...` -> exit 0。
  - `cd frontend && npm run build` -> exit 0。
  - `git diff --check` / `git diff --cached --check` -> exit 0。
  - `python3 .codestable/tools/validate-yaml.py --file package-catalog-checklist.yaml` -> exit 0。
- 浏览器复核：
  - Desktop：in-app browser `http://localhost:5173/packages`，新建 `验收套系 ...`、`¥0.05`、编辑 `¥680.55`、下架、archived 可见、删除确认、删除后不可见 -> pass。
  - Mobile：viewport 375px，`innerWidth=375`、`scrollWidth=360`、无横向溢出 -> pass。
- 场景复核：re-verified 13 / trust-prior-verify 2。trust-prior 项为 QA 的强制 500 错误态与 Escape 键取消删除弹窗截图 / 记录；不承载核心 CRUD 验收。
- 交付物复核：代码 / 配置 / schema / 路由 / 文档 / requirement current / roadmap done 均存在。
- 完整工作区复核：`git status --short --branch --untracked-files=all` 已纳入判断；dirty / untracked 文件均属于 package-catalog 实现、生成物、需求 / roadmap 回写、CodeStable 产物或截图证据；生成物当前 staged，其他文件仍未统一 stage。
- diff 清洁度：通过；未发现调试输出、临时 TODO/FIXME、注释掉代码、无用 import 或真实凭证新增。
- 知识沉淀出口：CONTEXT 候选（上架/下架/删除）与 cs-keep 候选（跨域前向依赖模式）已分流；attention 无候选。
- 结论：通过。owner approval A 已解除 requirement gate；req / VISION / roadmap 已回写，功能契约、验证证据、交付物与知识分流均闭环。
