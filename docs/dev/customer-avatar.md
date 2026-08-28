---
doc_type: dev-guide
slug: customer-avatar
component: customer-avatar
status: current
summary: 开发者如何接入客户头像契约、本地卷配置、AvatarObjectStore 边界与前端 CustomerAvatar/CustomerPicker。
tags: [customer, avatar, dev-guide]
last_reviewed: 2026-07-13
---

# 客户头像（开发者）

## 概述

头像是 Customer 档案的可选属性。PostgreSQL 保存 **写 revision + 五字段 current pointer**；二进制落在 **AvatarObjectStore**（支持 local 持久卷或私有 OSS）。公开读取始终走同源鉴权 `GET /api/v1/customers/{id}/avatar/content?v={avatar_version}`，不暴露本地路径、object key 或预签名 URL。

机器契约以 `api/openapi.yaml` 为准；人类可读 endpoint 参考见 [docs/api/customer-avatar.md](../api/customer-avatar.md)。

## 前置依赖

- 已完成 `customer-profile-complete`、`order-tracking`、`schedule-calendar`（选择面注入依赖既有 UI）。
- 环境变量（见 `.env.example`）：
  - `AVATAR_STORAGE_DRIVER=local|oss`；完整 OSS 配置与 bucket runbook 见 [object-storage.md](object-storage.md)
  - local 模式：`AVATAR_LOCAL_ROOT`、`AVATAR_LOCAL_REQUIRE_MOUNT`
  - oss 模式：`OSS_REGION`、`OSS_BUCKET`，可选 `OSS_ENDPOINT` / `OSS_USE_CNAME`
- Migration：`backend/internal/platform/store/migrations/0008_customer_avatar.*.sql`

## 快速上手

### 本地开发

```bash
cp .env.example .env   # 已含 AVATAR_* 本地默认
make db-up
set -a && source .env && set +a
cd backend && go run ./cmd/server
```

前端 Vite 代理下，详情页上传会走 multipart + `If-Match: "ar-N"`。

### 条件写（curl 示意）

```bash
# 假设 token 与客户 id 已就绪，当前 revision 为 ar-0
curl -sS -X PUT "http://localhost:8080/api/v1/customers/cus_x/avatar" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'If-Match: "ar-0"' \
  -F "file=@portrait.jpg;type=image/jpeg"
```

成功响应为完整 `Customer`：`avatar_revision` 变为 `ar-1`，并带 `avatar_version` / `avatar_url`。

### 鉴权读图

```bash
curl -sS -D- \
  -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/customers/cus_x/avatar/content?v=$AVATAR_VERSION" \
  -o /tmp/avatar.bin
```

期望：`200` + `ETag`（quoted version）+ `Cache-Control: private, no-cache` + `Vary: Authorization` + `X-Content-Type-Options: nosniff`。匹配 `If-None-Match` 时先校验字节完整性再 `304`。

## 架构要点

| 层 | 职责 | 路径 |
|---|---|---|
| HTTP | multipart / header / 状态码适配；不直接调 store | `httpapi/customer_avatar.go` |
| Application | revision CAS、generation 发布、READ 完整性 | `customer/avatar_application.go` |
| Store port | PutImmutable / Open / Stat / List / Delete | `customer/avatar_store.go` |
| Local adapter | durable I/O、路径 hardening、错误脱敏 | `customer/avatarstore/` |
| Image | 格式/尺寸/方向/去元数据/确定性 checksum | `customer/avatarimage/` |
| Maintenance | inventory + pointer audit + due GC | `customer/avatar_maintenance.go` |
| Frontend | `CustomerAvatar` 鉴权 Blob；`CustomerPicker` 选择面 | `frontend/src/components/customers/` |

三分离模型（version / revision / object_id）与「进入 GC 即烧毁」见 compound：  
`.codestable/compound/2026-07-13-avatar-immutable-generation-pattern.md`。

## 前端接入

- **展示**：只传 `customerId` / `displayName` / `avatarRevision` / `avatarUrl`，不要复制 Blob 逻辑。
- **写操作**：始终用当前 `avatar_revision` 组 `If-Match`；`409 avatar_revision_conflict` 时重拉详情。
- **选择面**：`CustomerPicker` 由 caller 显式传 `candidateStatuses`（如 `['active']` 或 `['active','archived']`），merged 永不作为新候选。
- **类型**：只用 `frontend/src/api/schema.d.ts`；封装见 `putCustomerAvatar` / `deleteCustomerAvatar` / `fetchAvatarBlob`。

## 运维与备份

production 卷与 `avatar-manifest` 一致备份步骤见根目录 `README.md`「客户头像持久卷与一致备份」。要点：

- 停 app 再 dump DB + volume + generate manifest；
- 恢复后 `avatar-manifest verify` 通过再启动写流量；
- 错误 `object_id` 必须被拒绝（不能只比 content checksum）。

## 测试入口

```bash
cd backend && go test ./internal/customer ./internal/customer/avatarstore ./internal/platform/httpapi ./cmd/server -count=1 -parallel=1
cd frontend && npm run test:customer-avatar && npm run test:avatar-layout
make check   # 全仓门禁；本机 Testcontainers 高并行偶发失败时见 .codestable/attention.md「测试」
```

## 明确不做

- 预签名直读、客户端直传
- 建档表单里头像字段
- 裁剪 / 人脸 / 相册 / 原图 / GIF·SVG·HEIC
- JSON data-export 内嵌头像二进制（data-export 另有 owner gate）
