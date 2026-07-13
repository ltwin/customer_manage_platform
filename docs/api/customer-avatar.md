---
doc_type: lib-api-ref
entry: customer-avatar
category: HTTP API
status: current
source_files:
  - api/openapi.yaml
  - backend/internal/platform/httpapi/customer_avatar.go
  - backend/internal/platform/httpapi/router.go
  - backend/internal/customer/avatar_application.go
  - backend/internal/customer/avatar_store.go
  - backend/internal/customer/model.go
  - frontend/src/api/client.ts
  - frontend/src/api/schema.d.ts
summary: 客户头像条件 PUT/DELETE 与强版本鉴权 GET，以及 Customer 投影中的 avatar_revision/version/url 字段。
tags: [customers, customer-avatar, http-api]
last_reviewed: 2026-07-13
---

## 概述

`customer-avatar` 覆盖：

- `PUT /api/v1/customers/{id}/avatar`
- `DELETE /api/v1/customers/{id}/avatar`
- `GET /api/v1/customers/{id}/avatar/content`

以及列表/详情等响应里 **始终存在的** `avatar_revision` 与 **成对可选的** `avatar_version` / `avatar_url`。

全部需要 Bearer token。服务端从账号上下文建立 `AccountScope`；客户端**永不传** `account_id`。跨账号或不可见客户对三方法均 `404`；无/失效 token 为 `401`。

前端类型来自 `frontend/src/api/schema.d.ts`；封装在 `frontend/src/api/client.ts`。Handler 只做协议适配，领域规则在 `backend/internal/customer`。

## 字段与 token

### `avatar_revision`

| 属性 | 值 |
|---|---|
| 类型 | `string`，只读 |
| 模式 | `^ar-(0\|[1-9][0-9]*)$`，例 `ar-0` |
| 用途 | PUT/DELETE 的写并发 token；CustomerAvatar 在同 URL 下 revision 变化时必须重取媒体 |
| 语义 | 初始 `ar-0`；pointer 每次成功变化 +1；same-content no-op / already-none 不增 |

### `avatar_version` / `avatar_url`

| 字段 | 说明 |
|---|---|
| `avatar_version` | `sha256-` + 64 位小写 hex；仅当前有头像时存在 |
| `avatar_url` | 同源相对路径 `/api/v1/customers/{id}/avatar/content?v={avatar_version}`；仅当前有头像时存在 |

二者成对缺省。`avatar_object_id`、本地路径、bucket、object key **不下发**。

### `Customer` / `CustomerListItem` / `CustomerSummary`

- `avatar_revision` 始终 required。
- `CustomerSummary` 保留 `id` / `display_name` / `channel` / `status`，只增上述头像字段（转介绍等嵌套摘要可直接显示头像）。

## `PUT /customers/{id}/avatar`

条件设置或替换头像。

| 项 | 说明 |
|---|---|
| Header | `If-Match: "ar-N"`（quoted revision，必填） |
| Body | `multipart/form-data`，字段名 `file`（binary） |
| 接受格式 | JPEG / PNG / WebP；原始 ≤5 MiB；解码宽高各 ≤4096 |
| 成功 | `200` + `Customer`（含新 revision/version/url） |
| same-content | 当前 generation 完整时 no-op：`200` 且 revision 不变 |
| `400` | 校验失败（格式/尺寸/缺 If-Match 等）；**不改**当前头像 |
| `409` | `customer_merged`（含 same-content）或 `avatar_revision_conflict` |
| `404` | 客户不存在或跨账号 |
| `500` | 存储/内部错误；旧 pointer 保持 |

状态：`active` / `archived` 可写；`merged` 恒 `409 customer_merged`。

前端：

```ts
putCustomerAvatar(id: string, file: File, avatarRevision: string): Promise<Customer>
// If-Match: `"${avatarRevision}"`
```

## `DELETE /customers/{id}/avatar`

条件移除头像。

| 项 | 说明 |
|---|---|
| Header | `If-Match: "ar-N"` |
| 成功 | `200` + `Customer`（pointer 清空，revision +1） |
| already-none | `200` no-op（不要求 revision 匹配语义上仍安全重放） |
| `409` | `avatar_revision_conflict` |
| merged | **唯一**允许的 cleanup-only 写：匹配 revision 时清 pointer 入 GC，不恢复其它编辑能力 |

前端：

```ts
deleteCustomerAvatar(id: string, avatarRevision: string): Promise<Customer>
```

## `GET /customers/{id}/avatar/content`

鉴权读取**当前**强版本内容。

| 项 | 说明 |
|---|---|
| Query | `v` 必填，须等于当前 `avatar_version` |
| Header | 可选 `If-None-Match: "sha256-..."`（quoted） |
| `200` | 完整性已验证的 image/* 字节 |
| `304` | 完整性通过且 ETag 命中；无 body |
| 响应头 | `ETag`（quoted version）、`Cache-Control: private, no-cache`、`Vary: Authorization`、`X-Content-Type-Options: nosniff` |
| `400` | 缺/非法 `v` |
| `404` | 无头像或跨账号/不存在 |
| `409` | `avatar_version_stale`（旧 v，**不返回**新字节） |
| `500` | pointer 宣称有对象但缺失/损坏（integrity） |

前端媒体读取：

```ts
fetchAvatarBlob(url: string, signal: AbortSignal): Promise<Blob>
// url 使用 Customer.avatar_url；携带 Bearer，不把 token 放进 query
```

## 错误码速查

| code | 典型场景 |
|---|---|
| `validation_failed` | 非法文件、缺/非法 If-Match 或 v |
| `unauthorized` | 无 token |
| `not_found` | 无客户 / 跨账号 / 无头像 GET |
| `customer_merged` | merged 上 PUT |
| `avatar_revision_conflict` | If-Match revision 失配 |
| `avatar_version_stale` | GET v 非当前 |
| `internal` | 完整性失败或存储故障 |

## 相关

- 用户操作：[docs/user/customer-avatar.md](../user/customer-avatar.md)
- 开发集成：[docs/dev/customer-avatar.md](../dev/customer-avatar.md)
- 运维备份：根目录 `README.md`「客户头像持久卷与一致备份」
- 契约权威：`api/openapi.yaml` tag `customer-avatar`
