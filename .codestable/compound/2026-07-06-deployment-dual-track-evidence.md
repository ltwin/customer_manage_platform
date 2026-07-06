# 部署双轨与 go:embed 证据链

## 背景

platform-skeleton 同时支持二进制直跑与 Docker 镜像运行；PostgreSQL 既可由 compose 自动部署，也可接现有服务。review 阶段曾出现“静态看着对，但运行形态没真跑”的风险，go:embed 的 SPA 托管也不能只靠一张截图证明。

## 结论

涉及部署形态的 feature，不要只做静态审查。至少分开验证三条轨：

- `docker build -t crm:local .`：镜像可构建。
- `docker compose up -d --wait` + `curl /healthz`：全容器模式可启动，app 真连 compose postgres。
- 外部 PG + `APP_DATABASE_URL` + `docker compose up -d --no-deps --wait app`：接现有 PG 时不拉起 compose postgres，app 真连外部库。

go:embed / SPA fallback 的强证据优先用证据链，而不是只用截图：

- served asset md5 与 `frontend/dist`、`backend/internal/platform/webui/dist` 三方一致。
- 二进制 `strings` 能看到 embed 资产路径。
- 请求日志显示由二进制服务返回。
- Vite dev server 无监听，排除访问到 dev server 的误判。

## 证据

- 部署工件：`Dockerfile`、`docker-compose.yml`、`.env.example`
- go:embed 落点：`backend/internal/platform/webui/`
- 证据文件：`.codestable/features/2026-07-06-platform-skeleton/evidence/a14-binary-embed-evidence.md`
- QA 记录：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-qa.md` QA-010 到 QA-013
