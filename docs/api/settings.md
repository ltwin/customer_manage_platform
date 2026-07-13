---
doc_type: lib-api-ref
entry: settings
category: HTTP API
status: current
source_files:
  - api/openapi.yaml
  - backend/internal/platform/httpapi/settings.go
  - backend/internal/platform/httpapi/auth.go
  - backend/internal/platform/httpapi/router.go
  - backend/internal/settings/model.go
  - backend/internal/settings/service.go
  - backend/internal/settings/repository.go
  - frontend/src/api/client.ts
  - frontend/src/api/schema.d.ts
summary: 账号级 Settings 的读取与部分更新 API、默认值和字段校验，以及对应的 TypeScript client。
tags: [settings, reminder-engine, timezone, http-api]
last_reviewed: 2026-07-13
---

## 概述

Settings API 读取和更新当前账号的业务设置，包括账号时区、生日提醒提前天数、交付后回访天数、按拍摄类型配置的流失阈值，以及每日摘要小时。

HTTP 基础路径为 `/api/v1`，两个操作都需要 Bearer token。Settings 行可以不存在：读取时服务端返回完整默认值，但不会为了 GET 隐式创建数据库行；首次 PATCH 时才执行 upsert。

Settings 是账号级业务数据，不是部署环境变量。客户端不传 `account_id`。

## Settings 资源

| 字段 | 类型 | 必返 | 默认值 | 说明 |
|---|---|---:|---:|---|
| `timezone` | string | 是 | `Asia/Shanghai` | IANA 时区；所有 date-only 业务判断使用该时区 |
| `birthday_lead_days` | integer | 是 | 3 | 生日提前提醒天数，必须 ≥ 1 |
| `follow_up_after_days` | integer | 是 | 7 | 交付后回访天数，必须 ≥ 1 |
| `churn_thresholds` | ChurnThreshold[] | 是 | 三种类型均 180 | 按最近套系拍摄类型选取流失阈值 |
| `digest_hour` | integer | 是 | 9 | 按 `timezone` 解释的小时，范围 0–23 |
| `telegram_chat_id` | string | 否 | 无 | 当前绑定标识；GET 可返回，PATCH /settings 不接受该字段 |

默认 `churn_thresholds`：

```json
[
  {"shoot_type":"portrait","days":180},
  {"shoot_type":"cosplay","days":180},
  {"shoot_type":"other","days":180}
]
```

`shoot_type` 只接受 `portrait`、`cosplay`、`other`；`days` 必须 ≥ 1。

## API 参考

### `GET /api/v1/settings`

返回当前账号的有效 Settings。

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/settings"
```

#### 200 响应

```json
{
  "timezone": "Asia/Shanghai",
  "birthday_lead_days": 3,
  "follow_up_after_days": 7,
  "churn_thresholds": [
    {"shoot_type":"portrait","days":180},
    {"shoot_type":"cosplay","days":180},
    {"shoot_type":"other","days":180}
  ],
  "digest_hour": 9
}
```

如果数据库没有 Settings 行，响应仍是上述完整默认 shape，且 GET 不写数据库。如果已有行缺少有效的 churn 类型条目，响应会用默认条目补齐。

### `PATCH /api/v1/settings`

部分更新当前账号的 Settings；省略的普通字段保留当前有效值。请求体必须是 JSON 对象。

#### 请求字段

| 字段 | 类型 | 校验 |
|---|---|---|
| `timezone` | string | 去除首尾空白后必须是可加载的 IANA 时区，且不能为空 |
| `birthday_lead_days` | integer | ≥ 1 |
| `follow_up_after_days` | integer | ≥ 1 |
| `churn_thresholds` | ChurnThreshold[] | shoot_type 只允许三种枚举、不得重复；days ≥ 1 |
| `digest_hour` | integer | 0–23 |

```bash
curl -X PATCH "http://localhost:8080/api/v1/settings" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "timezone": "Asia/Shanghai",
    "birthday_lead_days": 5,
    "churn_thresholds": [
      {"shoot_type":"portrait","days":120}
    ]
  }'
```

成功返回 `200` 和更新后的完整 Settings。

#### churn_thresholds 的 entry-level overlay

当请求包含 `churn_thresholds` 时，服务端以默认三类型、每类 180 天为底，再用请求中的条目覆盖。请求未包含的拍摄类型回到默认 180，而不是保留该类型以前的自定义值。

例如只提交：

```json
{
  "churn_thresholds": [
    {"shoot_type":"portrait","days":90}
  ]
}
```

响应中的有效数组为：

```json
[
  {"shoot_type":"portrait","days":90},
  {"shoot_type":"cosplay","days":180},
  {"shoot_type":"other","days":180}
]
```

如果请求完全省略 `churn_thresholds`，则保留当前有效数组。

## timezone 的影响

更新 `timezone` 后：

- `GET /api/v1/me` 的 `timezone` 会返回新值；
- Reminder 扫描缺省 date 按新时区求“今日”；
- reminder runner 的每日检查点判断按新时区的本地日期执行；
- 其他依赖账号自然日的功能应使用 `/me` 返回的 timezone，而不是浏览器时区。

`timezone` 必须是 IANA 名称，例如 `Asia/Shanghai` 或 `America/Los_Angeles`。`UTC+8`、空字符串或不存在的区域名会返回 400。

## TypeScript Client

公开类型来自 OpenAPI 生成结果：

```ts
type Settings = components['schemas']['Settings']
type ChurnThreshold = components['schemas']['ChurnThreshold']
type UpdateSettingsBody = paths['/settings']['patch']['requestBody']['content']['application/json']
```

Client 函数：

```ts
getSettings(): Promise<Settings>
updateSettings(body: UpdateSettingsBody): Promise<Settings>
```

Client 自动使用 `/api/v1` 前缀、JSON Content-Type 和当前 Bearer token。非 2xx 响应抛出 `ApiError`；收到 401 时会清除本地 token。

## 错误码速查

| HTTP | code | 典型场景 |
|---:|---|---|
| 400 | `validation_failed` | 请求体格式错误、非法/空 IANA timezone、天数小于 1、digest_hour 越界、shoot_type 非法或重复 |
| 401 | `unauthorized` | 缺失、无效或过期的 Bearer token |
| 404 | `not_found` | 运行时没有挂载 Settings service；正常“无数据库行”不会返回 404 |
| 500 | `internal` | 数据库、账号上下文或其他内部错误 |

## 注意事项

- PATCH /settings 不接受 `telegram_chat_id`；Telegram 绑定属于独立的后续接口。
- 服务端返回的是“叠加默认后的有效 Settings”，不保证与数据库原始行逐字段相同。
- `digest_hour` 当前是 Settings 的公开字段，但实际摘要推送由后续 Telegram 能力消费。
- 前端 API 类型必须继续引用生成的 `schema.d.ts`，不要手写重复 DTO。

## 相关条目

- [Reminders API](reminders.md)：使用 Settings 中的时区和规则参数生成提醒。
- `GET /api/v1/me`：返回当前有效 `timezone`。
- 契约权威：`api/openapi.yaml` 的 `reminder-engine` tag。
