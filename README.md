# 摄影师私域客户经营系统（CRM）

Go + Gin + React + PostgreSQL 单体，阿里云 ECS 自部署。规格与流程见 `.codestable/`（规划权威源：`roadmap §4` 契约 + `api/openapi.yaml` 机器形式）。

当前已落地客户档案（含可选头像）、套系、订单、月历档期、提醒引擎与账号级提醒设置；能力现状见 `.codestable/requirements/VISION.md`，roadmap 执行状态见 `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`。

客户头像操作与开发接入见 `docs/user/customer-avatar.md`、`docs/dev/customer-avatar.md`；HTTP 参考清单见 `docs/api/manifest.yaml`，已同步的 Reminder 与 Settings 参考见 `docs/api/reminders.md`、`docs/api/settings.md`。

## 开发

### 前置依赖

- Go 1.25+、Node 24+（npm）、Docker（本地 dev 库 / 测试容器 / 镜像构建）
- golangci-lint 2.x

### 起步

```bash
cp .env.example .env   # 占位值即本地可跑 dev 值（禁止用于生产）
make db-up             # 起本地 dev PostgreSQL（compose postgres 服务）
make check             # build + lint + test + 契约漂移检查，一键全绿 = 基线健康
```

### 本地起服务（dev 双进程）

```bash
# 终端 1：后端（读 .env 手动导出，或用你习惯的 env 注入方式）
set -a && source .env && set +a
cd backend && go run ./cmd/server

# 终端 2：前端（Vite dev server，/api 代理到 :8080）
cd frontend
npm ci                       # 首次启动或依赖缺失时先安装；否则会出现 sh: vite: command not found
npm run dev                  # 打开 http://localhost:5173
```

启动序：加载配置 → migrate up → ensure 默认账号 → HTTP 监听。首次启动（accounts 为空）必须提供 `SEED_ADMIN_PASSWORD`，否则 fail-fast；**seed 完成后可从环境移除该变量**。

### 常用命令

| 命令 | 作用 |
|---|---|
| `make check` | build + lint + test + 契约生成物漂移检查（全 roadmap 的验证入口） |
| `make generate` | OpenAPI → Go 服务端类型（按 tag）+ TS 全量类型 |
| `make db-up` | 起本地 dev 库 |
| `make migrate-up` | 手动执行 schema 迁移（服务启动时也会自动执行） |

## 提醒与账号设置

- `/reminders` 提供生日、拍后回访、流失与自定义提醒的查看、完成、忽略和手动扫描；客户详情的提醒区域可直接创建关联当前客户的自定义提醒。
- `/settings` 配置 IANA 账号时区、生日提前天数、交付后回访天数、按拍摄类型区分的流失阈值与摘要小时。日期边界按账号时区解释，不按浏览器时区兜底。
- 服务启动后，进程内 reminder runner 会立即检查一次，之后每小时检查；同一账号在同一本地自然日只自动扫描一次。手动扫描依靠幂等键避免重复提醒，且不推进自动扫描检查点。
- 当前能力只生成和管理提醒，不会自动联系客户；Telegram 每日摘要与 dashboard 今日待办仍是后续 roadmap 条目。

字段、默认值、过滤条件、错误码和手动扫描请求见 `docs/api/reminders.md` 与 `docs/api/settings.md`。

## 部署（双轨，配置只经环境变量）

### 轨 A：二进制直跑

```bash
make build             # 前端构建 → go:embed → backend/bin/server 单工件
./backend/bin/server   # DATABASE_URL 指向现有 PG 或 compose 起的 PG
```

生产建议 systemd `EnvironmentFile=/etc/crm/env`（chmod 600）注入环境变量。

### 轨 B：全容器

```bash
cp .env.example .env   # 生产环境改为真实值
docker compose up -d --wait   # postgres + app；app 就绪以 healthz 为准
```

只接现有 PG 时：在 `.env` 设 `APP_DATABASE_URL` 指向现有库，然后 `docker compose up -d --no-deps --wait app`（`--no-deps` 避免把 compose 内 postgres 一并拉起）。

## 环境变量

清单见 `.env.example`（key 全集 + 注释）。凭证红线：真实值只经环境注入，不入库、不入 git。

- **token 轮换**：更换 `AUTH_TOKEN_SECRET` 并重启即吊销全部已发 token（JWT 有效期 30 天）。
- **改密现状**：首版无改密 API；改密 = 清空 accounts 表后用新 `SEED_ADMIN_PASSWORD` 重启 seed（或直接 UPDATE password_hash）。
- **TELEGRAM_BOT_TOKEN** 仅 `scripts/telegram-smoke.sh` 使用，服务端不读取。

## 客户头像持久卷与一致备份

production compose 将头像放在独立 named volume `avatar_data`，容器内固定挂载到
`/var/lib/crm/avatars`，并以 `AVATAR_LOCAL_REQUIRE_MOUNT=true` 启动；缺卷、只落容器层、
不可写或无法从 Linux mount table 证明为独立挂载点时，app 会 fail-fast。二进制直跑可使用
普通目录；`AVATAR_LOCAL_REQUIRE_MOUNT` 未设置时缺省为 `false`，也可显式设置为
`false`。完整配置键见 `.env.example`。

头像备份必须同时包含 PostgreSQL、整个头像 volume 和 exact-generation manifest。为冻结 API 与
maintenance runner，先停 app；备份或恢复期间不要开放写流量：

```bash
set -eu
umask 077
BACKUP_DIR="backup-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir "$BACKUP_DIR"

docker compose stop app
docker compose run --rm --no-deps \
  --entrypoint /usr/local/bin/avatar-manifest app generate \
  > "$BACKUP_DIR/avatar-manifest.json"
docker compose exec -T postgres \
  pg_dump -U "${POSTGRES_USER:-crm}" "${POSTGRES_DB:-crm}" \
  > "$BACKUP_DIR/database.sql"
docker compose run --rm --no-deps --entrypoint tar app \
  -C /var/lib/crm/avatars -czf - . \
  > "$BACKUP_DIR/avatar-volume.tgz"
docker compose start app
```

manifest 会逐个 current pointer 记录 `account_id/customer_id/avatar_version/avatar_object_id/key/
media_type/size/actual_sha256`，并保存全部物理 generation 的 key、count 和 checksum 汇总。
仅比较对象数量或内容 checksum 不够：同内容但不同 `avatar_object_id` 是不同物理代次。

恢复会替换目标数据库和头像 volume。先停 app，确认备份来源与保留策略，再执行；最后一条 verify
成功前不得启动 app：

```bash
set -eu
BACKUP_DIR=backup-YYYYMMDDTHHMMSSZ

docker compose stop app
docker compose exec -T postgres \
  dropdb -U "${POSTGRES_USER:-crm}" --if-exists --force "${POSTGRES_DB:-crm}"
docker compose exec -T postgres \
  createdb -U "${POSTGRES_USER:-crm}" "${POSTGRES_DB:-crm}"
docker compose exec -T postgres \
  psql -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-crm}" "${POSTGRES_DB:-crm}" \
  < "$BACKUP_DIR/database.sql"
docker compose run --rm --no-deps \
  -v "$PWD/$BACKUP_DIR:/backup:ro" --entrypoint sh app -c \
  'find /var/lib/crm/avatars -mindepth 1 -maxdepth 1 -exec rm -rf {} + && tar -xzf /backup/avatar-volume.tgz -C /var/lib/crm/avatars'
docker compose run --rm --no-deps \
  -v "$PWD/$BACKUP_DIR:/backup:ro" \
  --entrypoint /usr/local/bin/avatar-manifest app \
  --manifest /backup/avatar-manifest.json verify
docker compose start app
```

备份目录含数据库凭证派生数据和客户头像 PII：保持 `0700/0600` 权限，使用受控账号、加密介质和
异地加密传输，并按独立 retention/销毁策略管理。在线“移除头像”（包括 merged 客户的隐私清理）
只清理当前 pointer 与活动存储中的对象，不会追溯擦除已经生成的历史备份；恢复旧备份可能重新带回
头像 PII，因此恢复前必须评估备份年龄、合法保留范围与是否应先销毁该备份。UI 不承诺跨历史备份的
彻底擦除。

## 目录

```
api/        OpenAPI 契约（roadmap §4 的机器形式，双端 codegen 输入）
backend/    Go 单体（cmd/server 入口；internal 下含 customer / package / order / schedule / reminder / settings 领域与 platform 基座）
frontend/   Vite + React + TS（dev 走 Vite proxy；API 类型由 OpenAPI 生成；构建产物 go:embed 进二进制）
scripts/    运维脚本（TG 冒烟）
docs/       用户/开发指南、编码规范 checklist 与 HTTP API 参考（清单见 docs/api/manifest.yaml）
```
