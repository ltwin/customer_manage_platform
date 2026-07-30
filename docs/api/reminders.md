---
doc_type: lib-api-ref
entry: reminders
category: HTTP API
status: current
source_files:
  - api/openapi.yaml
  - backend/internal/platform/httpapi/reminders.go
  - backend/internal/platform/httpapi/router.go
  - backend/internal/reminder/model.go
  - backend/internal/reminder/service.go
  - backend/internal/reminder/repository.go
  - backend/internal/reminder/runner.go
  - frontend/src/api/client.ts
  - frontend/src/api/schema.d.ts
summary: Reminder 资源的查询、自定义创建、完成/忽略与手动扫描 HTTP API，以及对应的 TypeScript client。
tags: [reminders, reminder-engine, http-api]
last_reviewed: 2026-07-13
---

## 概述

Reminders API 提供五个需要 Bearer token 的操作：

- 查询提醒列表；
- 创建自定义提醒；
- 将 pending 提醒标记为完成；
- 忽略 pending 提醒；
- 手动执行一次账号级提醒扫描。

HTTP 基础路径为 `/api/v1`。服务端从 Bearer token 解析账号，并在 repository 基座中强制账号隔离；客户端不传 `account_id`。所有非 2xx API 响应使用统一错误封套。

```json
{
  "error": {
    "code": "validation_failed",
    "message": "status 非法"
  }
}
```

## Reminder 资源

| 字段 | 类型 | 必返 | 说明 |
|---|---|---:|---|
| `id` | string | 是 | 服务端生成的提醒 ID，只读 |
| `account_id` | string | 是 | 服务端从账号上下文写入，只读；客户端永不传 |
| `created_at` | RFC 3339 date-time | 是 | 创建时间，只读 |
| `type` | `birthday \| follow_up \| churn \| custom` | 是 | 提醒类型 |
| `customer_id` | string | 否 | 关联客户；无客户时省略 |
| `order_id` | string | 否 | 关联订单；无订单时省略 |
| `due_date` | `YYYY-MM-DD` | 是 | 到期日，按 date-only 语义返回 |
| `content` | string | 是 | 提醒正文 |
| `status` | `pending \| done \| dismissed` | 是 | 当前处理状态 |
| `dedup_key` | string | 是 | 账号内唯一的幂等键，只读 |

自动提醒的 `dedup_key` 由规则生成；custom 提醒使用 `custom:{reminder_id}`。调用方不应构造或修改该字段。

## API 参考

### `GET /api/v1/reminders`

查询当前账号可见的提醒，默认按 `due_date ASC, id ASC` 稳定排序。

#### Query 参数

| 参数 | 类型 | 默认值 | 说明 |
|---|---|---:|---|
| `status` | `pending \| done \| dismissed` | 不过滤 | 按状态过滤；其他值返回 400 |
| `customer_id` | string | 不过滤 | 只返回关联指定客户的提醒 |
| `due_before` | `YYYY-MM-DD` | 不过滤 | 返回 `due_date <= due_before` 的提醒，包含当天 |
| `page` | integer ≥ 1 | 1 | 页码 |
| `page_size` | integer ≥ 1 | 20 | 每页数量；服务端有效上限为 100，超出时按 100 执行 |

#### 200 响应

```json
{
  "items": [
    {
      "id": "rem_123",
      "account_id": "acct_1",
      "created_at": "2026-07-13T08:00:00Z",
      "type": "birthday",
      "customer_id": "cus_1",
      "due_date": "2026-07-15",
      "content": "小林 生日（07-15）",
      "status": "pending",
      "dedup_key": "birthday:cus_1:2026"
    }
  ],
  "total": 1
}
```

`total` 是应用全部过滤条件后的总数，不是当前页的条目数。

#### 示例

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/reminders?status=pending&customer_id=cus_1&due_before=2026-07-31&page=1&page_size=20"
```

### `POST /api/v1/reminders`

创建一条 custom 提醒。

#### 请求体

| 字段 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `type` | `custom` | 是 | 当前创建接口只接受 custom |
| `customer_id` | string | 否 | 关联客户；空白值按未关联处理；非本账号或不存在返回 404 |
| `due_date` | `YYYY-MM-DD` | 是 | date-only 到期日 |
| `content` | string | 是 | 去除首尾空白后不能为空 |

```bash
curl -X POST "http://localhost:8080/api/v1/reminders" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "custom",
    "customer_id": "cus_1",
    "due_date": "2026-07-20",
    "content": "确认外景集合时间"
  }'
```

成功返回 `201` 和新建的 Reminder。初始 `status` 为 `pending`，`dedup_key` 为 `custom:{id}`。

### `POST /api/v1/reminders/{id}/done`

把 pending 提醒标记为 `done`，返回 `200` 和更新后的 Reminder。

- pending → done：成功；
- 已是 done：幂等返回 200；
- 已是 dismissed：返回 400，不允许交叉覆盖；
- 不存在或不属于当前账号：返回 404。

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/reminders/rem_123/done"
```

### `POST /api/v1/reminders/{id}/dismiss`

把 pending 提醒标记为 `dismissed`，返回 `200` 和更新后的 Reminder。

- pending → dismissed：成功；
- 已是 dismissed：幂等返回 200；
- 已是 done：返回 400，不允许交叉覆盖；
- 不存在或不属于当前账号：返回 404。

### `POST /api/v1/admin/reminders/scan`

手动执行当前账号的完整提醒扫描。请求体可以省略；传入空对象与省略请求体等价。

#### 请求体

| 字段 | 类型 | 必填 | 默认值 |
|---|---|---:|---|
| `date` | `YYYY-MM-DD` | 否 | 当前账号时区的今日 |

```bash
curl -X POST "http://localhost:8080/api/v1/admin/reminders/scan" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"date":"2026-07-13"}'
```

#### 200 响应

```json
{
  "created": 3,
  "skipped": 2,
  "auto_dismissed": 1
}
```

| 字段 | 说明 |
|---|---|
| `created` | 本次实际插入的自动提醒数 |
| `skipped` | 幂等冲突和因必要时间戳缺失而跳过的数量 |
| `auto_dismissed` | 本次自动忽略的 pending 提醒数，例如订单已删除或 churn 客户出现非终态订单 |

扫描顺序为 auto-dismiss → birthday → follow_up → churn，并在单账号事务中执行。唯一约束保证重复扫描不会重复插入同一幂等键。手动扫描不推进每日 runner 的 `last_scan_date` 检查点；进程内 runner 仍按账号本地日判断是否需要执行当日自动扫描。

## TypeScript Client

公开类型全部引用 `frontend/src/api/schema.d.ts` 的 OpenAPI 生成结果：

```ts
type Reminder = components['schemas']['Reminder']
type ReminderStatus = components['schemas']['ReminderStatus']
type ReminderType = components['schemas']['ReminderType']
type ReminderListResponse = paths['/reminders']['get']['responses']['200']['content']['application/json']
type CreateReminderBody = paths['/reminders']['post']['requestBody']['content']['application/json']
type ScanRemindersBody = NonNullable<paths['/admin/reminders/scan']['post']['requestBody']>['content']['application/json']
type ScanRemindersResult = paths['/admin/reminders/scan']['post']['responses']['200']['content']['application/json']
```

Client 函数：

```ts
listReminders(params?): Promise<ReminderListResponse>
createReminder(body): Promise<Reminder>
markReminderDone(id): Promise<Reminder>
dismissReminder(id): Promise<Reminder>
scanReminders(body = {}): Promise<ScanRemindersResult>
```

这些函数自动使用 `/api/v1` 前缀、设置 `Content-Type: application/json`，并在已有登录 token 时附加 Bearer header。非 2xx 响应抛出 `ApiError`；收到 401 时 client 会清除本地 token。

## 错误码速查

| HTTP | code | 典型场景 |
|---:|---|---|
| 400 | `validation_failed` | 非法 status/date/page、请求体格式错误、type 非 custom、content/due_date/id 缺失、done/dismiss 交叉覆盖 |
| 401 | `unauthorized` | 缺失、无效或过期的 Bearer token |
| 404 | `not_found` | Reminder 或 customer 不存在、跨账号不可见、运行时未挂载该资源 |
| 500 | `internal` | 数据库、扫描设置或其他内部错误 |

## 注意事项

- `due_date`、`due_before` 和 scan `date` 都使用 `YYYY-MM-DD`，不携带时区偏移；缺省 scan date 才按账号 Settings 中的 timezone 求今日。
- `account_id`、`created_at`、`dedup_key` 与自动提醒类型由服务端管理。
- 列表不支持 `status=all`；要查询全部状态，应省略 `status`。
- 自动扫描的小时 tick 是进程内行为，不是额外的公开 API。

## 相关条目

- [Settings API](settings.md)：账号时区和提醒规则参数。
- 契约权威：`api/openapi.yaml` 的 `reminder-engine` tag。
