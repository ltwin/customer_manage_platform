---
doc_type: lib-api-ref
entry: orders
category: HTTP API
status: current
source_files:
  - api/openapi.yaml
  - backend/internal/platform/httpapi/orders.go
  - backend/internal/order/model.go
  - backend/internal/order/service.go
  - backend/internal/order/state_machine.go
  - backend/internal/order/repository.go
  - frontend/src/api/client.ts
  - frontend/src/api/schema.d.ts
summary: 订单 endpoint 族，覆盖建单、列表、状态跃迁、字段修正和终态删除。
tags: [orders, order-tracking, http-api]
last_reviewed: 2026-07-09
---

## 概述

`orders` 条目覆盖 `/api/v1/orders` 与 `/api/v1/orders/{id}`。所有接口都需要 Bearer token，由服务端从账号上下文生成 `AccountScope`；客户端不传 `account_id`。

订单接口的公开 TypeScript 类型来自 `frontend/src/api/schema.d.ts`，前端调用封装在 `frontend/src/api/client.ts`。服务端 HTTP handler 只做认证、绑定和错误映射，状态机与字段不变量由 `backend/internal/order` 维护。

## API 参考

### 状态枚举

`OrderStatus` 可取值：

| 值 | 含义 |
|---|---|
| `consulting` | 咨询中 |
| `scheduled` | 已约档 |
| `shot` | 已拍摄 |
| `selected` | 已选片 |
| `retouching` | 精修中 |
| `delivered` | 已交付 |
| `closed` | 已完结 |
| `cancelled` | 已取消 |

合法跃迁由 `state_machine.go` 定义：

| 起点 | 可到达 |
|---|---|
| `consulting` | `scheduled`, `cancelled` |
| `scheduled` | `shot`, `cancelled` |
| `shot` | `selected`, `delivered`, `cancelled` |
| `selected` | `retouching`, `delivered`, `cancelled` |
| `retouching` | `delivered`, `cancelled` |
| `delivered` | `closed`, `cancelled` |
| `closed` | 无出边 |
| `cancelled` | 无出边 |

### 数据结构

`Order`：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `string` | 是 | 服务端生成，只读 |
| `account_id` | `string` | 是 | 服务端账号上下文写入，只读 |
| `created_at` | `date-time` | 是 | 服务端写入，只读 |
| `customer_id` | `string` | 是 | 关联客户 |
| `package_id` | `string` | 否 | 关联套系 |
| `title` | `string` | 否 | 订单标题 |
| `status` | `OrderStatus` | 是 | 当前状态 |
| `price` | `integer` | 否 | 金额，单位分 |
| `deposit_paid` | `boolean` | 是 | 定金是否已收，默认 `false` |
| `balance_paid` | `boolean` | 是 | 尾款是否已结清，默认 `false` |
| `shot_at` | `date-time` | 否 | 到达拍摄后才可写；进入 `shot` 时可由服务端自动写入 |
| `delivered_at` | `date-time` | 否 | 到达交付后才可写；进入 `delivered` 时可由服务端自动写入 |
| `note` | `string` | 否 | 备注，最多 500 字 |

`OrderListItem` 是 `Order` 加列表摘要字段：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `customer_display_name` | `string` | 是 | 关联客户展示名 |
| `package_name` | `string` | 否 | 关联套系名，未关联套系时缺省 |

### `GET /orders`

返回分页订单列表。

| 查询参数 | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `customer_id` | `string` | 无 | 只看某个客户的订单 |
| `status` | `OrderStatus` | 无 | 按状态过滤 |
| `unpaid_balance` | `boolean` | `false` | `true` 表示 `balance_paid=false` 且状态在 `shot`、`selected`、`retouching`、`delivered` |
| `page` | `integer` | `1` | 必须大于 0 |
| `page_size` | `integer` | `20` | 范围 `1..100` |

响应 `200`：

```json
{
  "items": [],
  "total": 0
}
```

列表默认按 `created_at DESC`、`id DESC` 排序，保证分页稳定。

前端封装：

```ts
listOrders(params?: {
  customerId?: string
  status?: OrderStatus
  unpaidBalance?: boolean
  page?: number
  pageSize?: number
}): Promise<OrderListResponse>
```

### `POST /orders`

新建订单。`customer_id` 必填，`status` 不传时默认为 `consulting`。

请求体：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `customer_id` | `string` | 是 | 只能引用当前账号下 active 客户 |
| `package_id` | `string` | 否 | 常规建单只能引用 active 套系；补录建单可引用 archived 套系 |
| `title` | `string` | 否 | 前后空白会被裁剪，空字符串按未传处理 |
| `price` | `integer` | 否 | 单位分，必须大于等于 0 |
| `status` | `OrderStatus` | 否 | 补录直达目标状态 |
| `deposit_paid` | `boolean` | 否 | 默认 `false` |
| `balance_paid` | `boolean` | 否 | 默认 `false` |
| `shot_at` | `date-time` | 否 | 目标状态已到达拍摄时才可传 |
| `delivered_at` | `date-time` | 否 | 目标状态已到达交付时才可传 |
| `note` | `string` | 否 | 最多 500 字 |

响应 `201`：`Order`。

补录规则：

| 目标状态 | 额外要求 |
|---|---|
| `shot`, `selected`, `retouching` | 必须提供 `shot_at` |
| `delivered` | 必须提供 `shot_at` 与 `delivered_at` |
| `closed` | 必须提供 `shot_at`、`delivered_at`，且 `balance_paid=true` |
| `cancelled` | 不要求拍摄或交付时间 |

前端封装：

```ts
createOrder(body: CreateOrderBody): Promise<Order>
```

### `PATCH /orders/{id}`

更新订单字段或推进状态。请求体可同时携带 `status` 与字段修正；服务端在同一事务内计算最终订单，再校验状态机和不变量。

请求体：

| 字段 | 类型 | 说明 |
|---|---|---|
| `status` | `OrderStatus` | 目标状态，必须符合合法跃迁 |
| `deposit_paid` | `boolean` | 终态订单不可修改收款标记 |
| `balance_paid` | `boolean` | 进入或保持 `closed` 时必须为 `true` |
| `shot_at` | `date-time` | 订单已到达拍摄后才可写；显式 `null` 会返回 `400` |
| `delivered_at` | `date-time` | 订单已到达交付后才可写；显式 `null` 会返回 `400` |
| `title` | `string` | 前后空白会被裁剪，空字符串按未传处理 |
| `price` | `integer` | 单位分，必须大于等于 0；运行时也识别 JSON `null` 为清空价格 |
| `note` | `string` | 最多 500 字，空字符串按未传处理 |

响应 `200`：更新后的 `Order`。

自动时间戳：

| 状态变化 | 时间戳行为 |
|---|---|
| 进入 `shot` | 若请求未传 `shot_at` 且订单无 `shot_at`，服务端写入当前 UTC 时间 |
| 进入 `delivered` | 若请求未传 `delivered_at` 且订单无 `delivered_at`，服务端写入当前 UTC 时间 |
| 只修正字段 | 不会重复覆盖已有时间戳 |

前端封装：

```ts
updateOrder(id: string, body: UpdateOrderBody): Promise<Order>
```

### `DELETE /orders/{id}`

物理删除订单。只有 `closed` 或 `cancelled` 终态订单可以删除。

响应 `204`：无响应体。

前端封装：

```ts
deleteOrder(id: string): Promise<void>
```

## 错误响应

所有非 2xx 响应使用统一错误封套：

```json
{
  "error": {
    "code": "validation_failed",
    "message": "错误说明"
  }
}
```

| HTTP | `error.code` | 常见来源 |
|---|---|---|
| `400` | `validation_failed` | 请求体格式错误、分页参数越界、非法状态值、负数价格、备注超过 500 字、时间戳与状态不一致、终态收款标记修改 |
| `401` | `unauthorized` | 未认证或 token 无效 |
| `404` | `not_found` | 订单、客户或套系不存在 |
| `409` | `customer_archived` | 新建订单引用 archived 或 merged 客户 |
| `409` | `invalid_status_transition` | `PATCH` 状态跃迁不在状态机允许范围内 |
| `409` | `unpaid_balance` | 进入或保持 `closed` 时 `balance_paid` 不是 `true` |
| `409` | `order_not_terminal` | 删除非终态订单 |

## 注意事项

- 请求体最大 1 MiB，超过后按请求体格式错误返回 `400 validation_failed`。
- `created_at`、`id`、`account_id` 均由服务端写入，客户端传入无效。
- `listOrders({ unpaidBalance: false })` 不会生成 `unpaid_balance=false` 查询参数；该过滤只在传 `true` 时启用。
- OpenAPI 当前在删除响应里保留了 future-facing 的 `order_in_use` 描述；现有 `httpapi` 错误映射尚未暴露该错误码，schedule 域接入前不应依赖它。

## 相关条目

- `packages`：订单创建可引用套系，套系删除受订单引用影响。
- `customers`：订单创建引用客户；客户列表和详情聚合读取订单统计。
