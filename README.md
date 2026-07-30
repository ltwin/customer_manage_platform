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

## 最短使用路径

首次启动时，空数据库必须通过 `SEED_ADMIN_PASSWORD` 创建唯一的初始账号；登录成功并确认
`/healthz` 为 `{"status":"ok"}` 后立即从运行环境删除该 seed 密码，再以
`--seed-state initialized` 运行生产预检。日常经营的最短路径是：

1. 在「客户」建立客户档案，可补充社交身份、备注与头像。
2. 在「套系」维护报价，再在「订单」关联客户与套系并推进状态、尾款。
3. 在「档期」登记拍摄/占用时段；「经营台」汇总近期提醒、今日档期、待收尾款、流失预警和近 30 天统计。
4. 在「提醒」查看、完成或忽略生日、拍后回访、流失和自定义提醒；「设置」维护 IANA 时区、阈值与每日摘要时间。
5. 在「设置」绑定 Telegram 并下载 reference-only JSON 导出。JSON 只用于查阅，**不是数据库与头像备份**。

服务端会运行 reminder scan 与 Telegram 每日摘要。Telegram long polling 生产环境只允许一个
app replica；`TELEGRAM_BOT_TOKEN` 与 `TELEGRAM_BOT_USERNAME` 必须同时设置或同时为空。
字段、默认值和错误码见 `docs/api/reminders.md` 与 `docs/api/settings.md`。

## 生产部署与预检

最低依赖为 Docker Engine 与 Docker Compose v2.24+。生产配置只经权限为 `0600` 的环境文件
注入；不要 `source` 不可信 env 文件。公网入口必须由 TLS 终止的反向代理承接，并用安全组/
防火墙限制 PostgreSQL 和 Docker socket；应用自身的 `/healthz` 只证明进程与数据库可达，
不证明 TLS、防火墙、磁盘持久性或异地备份已经合格。

### 轨 A：二进制直跑

```bash
make build             # 前端构建 → go:embed → backend/bin/server 单工件
./backend/bin/server   # DATABASE_URL 指向现有 PG 或 compose 起的 PG
```

生产建议 systemd `EnvironmentFile=/etc/crm/env`（chmod 600）注入环境变量。上线前执行：

```bash
./scripts/production-preflight.sh \
  --mode binary \
  --seed-state initialized \
  --env-file /etc/crm/env
```

### 轨 B：全容器

```bash
CRM_ENV_FILE=/etc/crm/env docker compose \
  --env-file /etc/crm/env \
  -f ./docker-compose.yml \
  -p crm-prod \
  up -d --wait
```

compose 内置 PostgreSQL 与外部 PostgreSQL 分别运行：

```bash
./scripts/production-preflight.sh \
  --mode compose-managed-db \
  --seed-state initialized \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod

./scripts/production-preflight.sh \
  --mode compose-external-db \
  --seed-state initialized \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod
```

所有 compose 命令必须显式给出 env、compose file、Docker context 和 project。运维脚本拒绝
`DOCKER_HOST`、`DOCKER_CONTEXT`、TLS selector 环境变量以及 TCP/SSH endpoint，只接受显式
context 解析出的本机绝对 Unix socket；解析后每个 Engine 调用都固定到同一 endpoint，绝不读取
active/default context。`CRM_ENV_FILE=/etc/crm/env` 让 compose interpolation 与 app
`env_file` 使用同一文件，避免暗中回退到仓库 `.env`。

## 环境变量

清单见 `.env.example`（key 全集 + 注释）。凭证红线：真实值只经环境注入，不入库、不入 git。

- **token 轮换**：更换 `AUTH_TOKEN_SECRET` 并重启即吊销全部已发 token（JWT 有效期 30 天）。
- **改密现状**：首版无改密 API；改密 = 清空 accounts 表后用新 `SEED_ADMIN_PASSWORD` 重启 seed（或直接 UPDATE password_hash）。
- **DATABASE_URL / APP_DATABASE_URL / POSTGRES_PASSWORD**：用密码管理器生成并保存；数据库密码轮换需同步连接串并滚动重启。远程 PostgreSQL 必须启用 TLS，不能使用 `sslmode=disable`。
- **AUTH_TOKEN_SECRET**：至少 32 个随机字符；轮换并重启会吊销所有现有 token。
- **SEED_ADMIN_PASSWORD**：只在空数据库首次启动短暂注入，seed 后必须删除；已初始化环境保留非空值会被预检拒绝。
- **TELEGRAM_BOT_TOKEN / TELEGRAM_BOT_USERNAME**：服务端真实读取；BotFather 撤销/轮换 token 后同步环境并重启。禁用时两项都清空。

## 一致备份与恢复

production compose 将头像放在独立 named volume `avatar_data`，容器内固定挂载到
`/var/lib/crm/avatars`，并以 `AVATAR_LOCAL_REQUIRE_MOUNT=true` 启动；缺卷、只落容器层、
不可写或无法从 Linux mount table 证明为独立挂载点时，app 会 fail-fast。二进制直跑可使用
普通目录；`AVATAR_LOCAL_REQUIRE_MOUNT` 未设置时缺省为 `false`，也可显式设置为
`false`。完整配置键见 `.env.example`。

公开 backup/restore 脚本只支持以下目标：`compose-managed-db`、已经 seed、app/postgres 和
`avatar_data`/`pgdata` 都唯一存在、app 精确处于正常 `running` 或 Engine `exited`、本机 Unix
Docker endpoint。binary、外部 PostgreSQL、远程 Engine、app absent 或 created/paused/
restarting/dead 等异常态不在脚本支持范围内，V1 上线前必须由 operator 提供等价的
“停 app → 数据库备份 → 头像目录/卷 → manifest → 恢复验证”演练记录，否则上线清单阻塞。

备份输出必须是尚不存在的新目录；可位于仓库外的加密介质：

```bash
./scripts/backup-compose.sh \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod \
  --output /srv/crm-backups/2026-07-27T020000Z
```

发布包固定且只包含 `database.sql`、`avatar-volume.tgz`、`avatar-manifest.json`、
`metadata.json`、`SHA256SUMS`。前三项在同一 freeze window 取得；metadata v1 同时保存固定表的
逐表数据库行数。脚本在发布前验证四文件 checksum、manifest 完整 schema，并逐个比较 manifest
inventory 与 tar 中实际 regular file 的路径、大小和内容 SHA-256；最终使用“不替换已存在 leaf”的
原子 rename 发布。恢复会**替换目标数据库和头像卷**，必须
逐字输入目标 project：

```bash
./scripts/restore-compose.sh \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod \
  --input /srv/crm-backups/2026-07-27T020000Z \
  --confirm-project crm-prod
```

两脚本以 Docker Engine ID + project 构造物理 target hash，并用 daemon-side immutable-ID lock 与
helper fence 阻止同目标并发操作。只有确认本机旧 owner PID 已消失且无同 generation helper 时，
operator 才可在原命令末尾显式追加 `--break-stale-lock`；绝不能按容器名手工猜删锁。

restore 会先复制私有 staging snapshot，验证五件套、checksum、非空数据库 dump、manifest 完整
schema、manifest↔tar 精确内容、tar traversal/link/device 和 typed project，再停止/替换。数据库
导入与头像 exact-generation verify 后，还会把每张固定表的实际行数逐项与 metadata oracle 比较。
破坏开始后的任一失败（包括 INT/TERM）都执行 failure-stop 并保持 app 为 Engine `exited`，不会伪造 rollback；
operator 应保留日志、检查当前 DB/头像 after-state，修复后从可信包重跑。成功 exact-generation
verify 后，仅当操作前 app 正常 running 才启动并等待 health；原 exited 始终保持 exited。

至少每日备份并定期执行隔离恢复演练；保留一份与 ECS 不同故障域的加密副本，定义 retention 和
可验证销毁。生产上线还必须由 owner 留存 mount/write/fsync/dir-sync probe、ECS 磁盘持久性、TLS、
网络边界和异地介质 attestation；脚本 exit 0 不替代这些人工/云侧证据。

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
