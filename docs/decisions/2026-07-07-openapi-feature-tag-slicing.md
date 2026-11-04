# OpenAPI 全量同步与 Go tag 切片

## 背景

`customer-core` 需要一次收编 roadmap §7 里跨域的 OpenAPI 漂移：客户聚合字段、套系聚合字段、提醒按客户过滤、Package.note、社交平台枚举、dashboard 近 3 天 / 近 30 天口径等。TS 前端类型必须全量生成，才不会继续拖着旧契约；但 Go 后端如果也全量生成并注册所有端点，就会把尚未实现的套系、订单、提醒、dashboard 等入口提前暴露出来。

项目里已有 `.codestable/compound/2026-07-06-openapi-roadmap-bidirectional-check.md` 记录“roadmap 与 OpenAPI 双向核对”。这次补充的是实现侧的切片口径：契约可以全量同步，运行时注册面必须按 feature 控制。

## 结论

后续 feature 同步 OpenAPI 时采用这个模式：

- `api/openapi.yaml` 按 roadmap §4/§7 全量同步机器契约，不因为某个域尚未实现就让 OpenAPI 继续漂移。
- `frontend/src/api/schema.d.ts` 继续全量生成，前端可以提前拿到完整契约类型。
- Go server codegen 用 operation-level tag 切片：只把当前已实现 feature 的 tag 加进 `backend/oapi-codegen.yaml` 的 `include-tags`。
- 后端路由仍手工挂到受保护 group；不要直接用生成物里的 `RegisterHandlers` 注册全部生成路由。
- 未实现域只允许存在于 OpenAPI 和 TS 类型里，不允许出现在运行时路由注册里；范围守护用 404 测试兜住。

一句话：**OpenAPI 是全量契约，Go include-tags 是当前实现面，router.go 是真实暴露面。**

## 证据

- `api/openapi.yaml`：`customer-core` 同步期间收编了 customer/package/reminder/dashboard 的契约增量，同时给已实现的三条客户操作加 `customer-core` tag。
- `backend/oapi-codegen.yaml`：`include-tags` 当前只有 `auth` 和 `customer-core`，因此 Go server interface 只新增 `CreateCustomer` / `ListCustomers` / `GetCustomer`。
- `backend/internal/platform/httpapi/router.go`：只手工注册受保护的 `GET /customers`、`POST /customers`、`GET /customers/:id`；未注册套系、订单、提醒、dashboard 后端入口。
- `frontend/src/api/schema.d.ts`：由 openapi-typescript 全量生成，包含未实现域的类型，这是前端契约层允许的“提前可见”。
- `.codestable/features/2026-07-07-customer-core/customer-core-acceptance.md`：第 2、7、10 节记录了切片验收结论，范围守护确认未实现端点仍 404。
- 验证命令：验收时 `make check` 通过，`make generate` 后 `api.gen.go` / `schema.d.ts` 零漂移。
