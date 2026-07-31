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

启动序：加载配置 → migrate up → HTTP 监听。空数据库允许直接启动，server 不再创建默认账号，也不读取 `SEED_ADMIN_PASSWORD`。首账号必须经下面的受信 `accountctl auth bootstrap` 创建。

### 常用命令

| 命令 | 作用 |
|---|---|
| `make check` | build + lint + test + 契约生成物漂移检查（全 roadmap 的验证入口） |
| `make generate` | OpenAPI → Go 服务端类型（按 tag）+ TS 全量类型 |
| `make db-up` | 起本地 dev 库 |
| `make migrate-up` | 手动执行 schema 迁移（服务启动时也会自动执行） |

## 最短使用路径

首次启动时先让 server 完成 migrate，再在同一受控环境中创建首账号。密码只通过临时环境变量
注入，不能进入 argv、shell 历史或日志；先 dry-run，再正式执行：

```bash
cd backend
export ACCOUNTCTL_AUTH_PASSWORD='由密码管理器临时注入的强密码'
go run ./cmd/accountctl auth bootstrap --email owner@example.com --dry-run
go run ./cmd/accountctl auth bootstrap --email owner@example.com
unset ACCOUNTCTL_AUTH_PASSWORD
```

Dry-run 只返回脱敏计划，不写库、不生成 token、不发邮件；正式命令会在数据库 admission guard
内重新检查零账号条件，并发送验证邮件。投递失败时账号会保持 pending，检查 provider 后调用
`/api/v1/auth/email/resend`，不要重跑 bootstrap。已有任何账号时命令固定拒绝，应改走公开注册或
legacy claim。owner 完成邮件验证并确认 `/healthz` 为 `{"status":"ok"}` 后，日常经营的最短路径是：

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
BUILD_REVISION=<40位Git提交SHA> make build # 同时构建 server 与带 revision attestation 的 accountctl
./backend/bin/server   # DATABASE_URL 指向现有 PG 或 compose 起的 PG
```

生产建议 systemd `EnvironmentFile=/etc/crm/env`（chmod 600）注入环境变量。上线前执行：

```bash
./scripts/production-preflight.sh \
  --mode binary \
  --seed-state initialized \
  --env-file /etc/crm/env \
  --accountctl-bin ./backend/bin/accountctl \
  --build-revision <40位Git提交SHA> \
  --evidence-dir /var/lib/crm/auth-readiness-evidence
```

### 轨 B：全容器

```bash
CRM_ENV_FILE=/etc/crm/env docker compose \
  --env-file /etc/crm/env \
  -f ./docker-compose.yml \
  -p crm-prod \
  build --build-arg BUILD_REVISION=<40位Git提交SHA>

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
  --project-name crm-prod \
  --build-revision <40位Git提交SHA> \
  --evidence-dir /var/lib/crm/auth-readiness-evidence

./scripts/production-preflight.sh \
  --mode compose-external-db \
  --seed-state initialized \
  --env-file /etc/crm/env \
  --compose-file ./docker-compose.yml \
  --docker-context local-production \
  --project-name crm-prod \
  --build-revision <40位Git提交SHA> \
  --evidence-dir /var/lib/crm/auth-readiness-evidence
```

所有 compose 命令必须显式给出 env、compose file、Docker context 和 project。运维脚本拒绝
`DOCKER_HOST`、`DOCKER_CONTEXT`、TLS selector 环境变量以及 TCP/SSH endpoint，只接受显式
context 解析出的本机绝对 Unix socket；解析后每个 Engine 调用都固定到同一 endpoint，绝不读取
active/default context。`CRM_ENV_FILE=/etc/crm/env` 让 compose interpolation 与 app
`env_file` 使用同一文件，避免暗中回退到仓库 `.env`。

`--seed-state empty|initialized` 作为旧运维调用的兼容参数保留，但两种状态都要求
`SEED_ADMIN_PASSWORD` 缺失或为空。`AUTH_PUBLIC_REGISTRATION_ENABLED=false` 时，preflight 完成所有基础配置
检查后只输出 `complete/preflight=secure-baseline-ready`，不声称公开注册已经开启。值为 `true` 时，必须同时满足：

- 当前 `accountctl auth readiness` 真实读取的数据库可达、schema version 13、limiter schema 和 legacy cutover 全绿；
- `mail-accepted.json`、`monitor.json`、`security.json` 在 24 小时内生成；
- `rollback.json`、`rotation.json` 在 7 天内生成；
- 每份 evidence 的 `version=1`、UTC `generated_at`、`build_revision`、`schema_version`、
  `config_fingerprint`、`environment=production`、`status=passed` 和 `evidence_path` 完整，且与本次运行一致；
- evidence 时间不能比当前时间晚超过 5 分钟；任一字段缺失、未知字段、过期、future skew 或 revision/schema/
  fingerprint mismatch 都 fail closed；
- mail receipt 额外只允许脱敏 `recipient_ref`、provider message ID、provider 和
  `AUTH_TOKEN_SECRET_VERSION` 引用，不能包含完整收件地址、secret 或 provider response body。

全部通过才输出 `complete/preflight=enable-ready`。该结果只表示配置与证据具备开启条件；脚本不会修改 env、
切换开关、迁移、启动服务、deploy、cutover 或轮换密钥。真实公开注册仍需 owner 独立授权。证据目录应位于受控运维
存储，不提交真实 recipient、连接串或凭证。开发阶段可继续保持 `false`；Resend 的 `onboarding@resend.dev`
仅适合受控测试，正式开放前应使用已验证的真实发件域名和对应 fresh accepted receipt。

### 旧账号认领与回滚演练

升级前已有 seed 账号使用原地认领，不创建新 account、不改业务表 `account_id` 或头像 object：

```bash
cd backend
go run ./cmd/accountctl auth claim-legacy --email owner@example.com --dry-run
go run ./cmd/accountctl auth claim-legacy --email owner@example.com
```

正式命令只允许恰好一个 `legacy_unclaimed` 目标；同邮箱失败重试会替换旧 claim token。投递失败
时检查 provider 后重跑相同 claim。验证完成后 `/me` 仍返回原 account ID。仓库内的
`./scripts/test-auth-legacy-cutover.sh` 会用 synthetic PostgreSQL fixture 对比客户、订单、档期、
提醒、设置、checkpoint 和头像 checksum，并演练 migration 0012 回滚：legacy-only 数据可以
安全 down；只要存在新式 `password_hash IS NULL` 账号就固定以
`auth_schema_down_blocked_new_accounts` 阻断。仓库不提供生产 down 命令；真实 cutover/rollback
仍需独立授权。旧 binary 无法使用新式账号，这是明确的降级边界。

认证 root secret 的人工轮换顺序、safe point 和失败恢复见
[`docs/ops/auth-root-rotation.md`](docs/ops/auth-root-rotation.md)。仓库内
`./scripts/test-auth-rotation-rollback.sh` 只做 synthetic rehearsal，不触碰生产环境。

## 环境变量

清单见 `.env.example`（key 全集 + 注释）。凭证红线：真实值只经环境注入，不入库、不入 git。

- **token 轮换**：Access JWT 有效期 10 分钟；refresh session 在服务端 rotation。更换 `AUTH_TOKEN_SECRET` 会使旧 access/replay/limiter namespace 失效，但切换前仍须撤销全部 refresh family；真实轮换按 runbook 另行授权。
- **AUTH_TOKEN_SECRET_VERSION**：非秘密的 root secret 版本引用，进入 production config fingerprint；不得填写 secret 值。
- **BUILD_REVISION**：容器构建时嵌入 `accountctl` 的 40 位 Git revision；binary 轨由 `make BUILD_REVISION=... build` 嵌入。preflight 会拒绝运行时 binary 与 evidence revision 不一致。
- **密码与登出**：已提供忘记密码、30 分钟单次 reset token、登录后修改密码和退出登录。reset/change 成功会撤销该账号全部 refresh family并清当前客户端状态；不要通过清空 accounts、恢复 seed 或直接 UPDATE hash 代替。
- **DATABASE_URL / APP_DATABASE_URL / POSTGRES_PASSWORD**：用密码管理器生成并保存；数据库密码轮换需同步连接串并滚动重启。远程 PostgreSQL 必须启用 TLS，不能使用 `sslmode=disable`。
- **AUTH_TOKEN_SECRET**：至少 32 个随机字符；轮换并重启会吊销所有现有 token。
- **ACCOUNTCTL_AUTH_PASSWORD**：仅在 bootstrap 命令进程内临时注入，不能写入 `.env`、argv 或日志；命令结束立即清除。
- **SEED_ADMIN_PASSWORD**：已退役；server 不读取，production preflight 对任何非空值都拒绝。
- **TELEGRAM_BOT_TOKEN / TELEGRAM_BOT_USERNAME**：服务端真实读取；BotFather 撤销/轮换 token 后同步环境并重启。禁用时两项都清空。

## 一致备份与恢复

production compose 将头像放在独立 named volume `avatar_data`，容器内固定挂载到
`/var/lib/crm/avatars`，并以 `AVATAR_LOCAL_REQUIRE_MOUNT=true` 启动；缺卷、只落容器层、
不可写或无法从 Linux mount table 证明为独立挂载点时，app 会 fail-fast。二进制直跑可使用
普通目录；`AVATAR_LOCAL_REQUIRE_MOUNT` 未设置时缺省为 `false`，也可显式设置为
`false`。完整配置键见 `.env.example`。

公开 backup/restore 脚本只支持以下目标：`compose-managed-db`、已经完成账号初始化、app/postgres 和
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
