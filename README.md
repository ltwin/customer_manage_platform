# 摄影师私域客户经营系统（CRM）

Go + Gin + React + PostgreSQL 单体，阿里云 ECS 自部署。规格与流程见 `.codestable/`（规划权威源：`roadmap §4` 契约 + `api/openapi.yaml` 机器形式）。

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
cd frontend && npm run dev   # 打开 http://localhost:5173
```

启动序：加载配置 → migrate up → ensure 默认账号 → HTTP 监听。首次启动（accounts 为空）必须提供 `SEED_ADMIN_PASSWORD`，否则 fail-fast；**seed 完成后可从环境移除该变量**。

### 常用命令

| 命令 | 作用 |
|---|---|
| `make check` | build + lint + test + 契约生成物漂移检查（全 roadmap 的验证入口） |
| `make generate` | OpenAPI → Go 服务端类型（按 tag）+ TS 全量类型 |
| `make db-up` | 起本地 dev 库 |
| `make migrate-up` | 手动执行 schema 迁移（服务启动时也会自动执行） |

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

## 备份建议（v1-hardening 前的手动方案）

```bash
docker compose exec postgres pg_dump -U crm crm > backup-$(date +%F).sql
```

建议每日 cron + 异地留存；脚本化与恢复演练归 v1-hardening。

## 目录

```
api/        OpenAPI 契约（roadmap §4 的机器形式，双端 codegen 输入）
backend/    Go 单体（cmd/server 入口；internal/platform = auth / config / httpapi / store / webui）
frontend/   Vite + React + TS（dev 走 Vite proxy；构建产物 go:embed 进二进制）
scripts/    运维脚本（TG 冒烟）
docs/       Go 编码规范 checklist 等
```
