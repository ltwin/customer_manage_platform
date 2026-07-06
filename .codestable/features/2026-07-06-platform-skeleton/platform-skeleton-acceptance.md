---
doc_type: feature-acceptance
feature: 2026-07-06-platform-skeleton
status: passed
accepted: 2026-07-06
round: 1
---

# platform-skeleton 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-06
> 关联方案 doc：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] `POST /api/v1/auth/login`：正确密码返回 200 `{token}`；错误密码返回 401 `unauthorized`；缺 password 返回 400 `validation_failed`。证据：`make check`、`go test -count=1 ./internal/platform/httpapi`、QA-002。
- [x] `GET /api/v1/me`：有效 token 返回 `id` / `created_at`，不含 `password_hash`；无 token / 篡改 / 过期 token 均 401。证据：`auth_test.go`、QA-002。
- [x] `GET /healthz`：无鉴权；DB 可达 200 `ok`，DB 不可达 503 `degraded`；不进入 OpenAPI、不套业务 ErrorEnvelope。证据：`router_test.go`、QA-003。
- [x] `AccountScope` 概念签名：业务探针表读写经 scope 强制 `account_id` 隔离。证据：`store/scope*.go` 与真实 PG 容器测试，QA-004。

**名词层“现状 → 变化”逐项核对**：
- [x] `Account`：`accounts` 表为 `id, password_hash, created_at`，不带 `account_id`；符合其作为归属方的设计。
- [x] `AccountContext`：auth 中间件注入，请求日志可读 `account_id`；handler 以下不下穿 `gin.Context`。
- [x] `AccountScope`：落在 `backend/internal/platform/store`；`domain/service` 禁 gin、pgx 仅 store 系包 import，由 golangci-lint/depguard 兜底。
- [x] `ErrorEnvelope`：`/api/v1/**` 非 2xx 统一封套。验收发现 QA residual REV-007 后已修复尾斜杠、`/api`、双斜杠 API 旁路，新增回归测试覆盖。
- [x] `api/openapi.yaml`：作为双端 codegen 输入；`make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` 通过。
- [x] JWT claims：`sub=account_id`、`exp`，secret 经 `AUTH_TOKEN_SECRET` 注入。

**流程图核对**：
- [x] 图中节点均有落点：Gin router、recovery/request log/envelope/auth middleware、login/me/healthz handlers、AccountScope、PostgreSQL、go:embed SPA fallback。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] Go+React 工程骨架与根级 Makefile 已落盘；`make check` 退出码 0。
- [x] OpenAPI 契约与双端 codegen 已落盘；生成物零漂移。
- [x] PostgreSQL 迁移、默认账号 seed、AccountScope 隔离、单账号 JWT 登录、错误封套、健康检查、TG 冒烟脚本、部署双轨工件均已落盘并有 QA 证据。

**明确不做逐项核对**：
- [x] 业务域端点零实现：路由注册仅 `/healthz`、`/api/v1/auth/login`、`/api/v1/me`；`/api/v1/customers` 404 `not_found`。
- [x] 无注册 / 账号管理 / 改密码 API：未发现对应服务端实现或前端入口。
- [x] 无 RLS：迁移 SQL 无 `POLICY` / `ROW LEVEL SECURITY`。
- [x] 无 TG 常驻 / 绑定：服务端无 Telegram 依赖；TG 代码仅 `scripts/telegram-smoke.sh` 与 OpenAPI 后续契约类型。
- [x] 无 UI 组件库：`frontend/package.json` 无 antd / MUI / chakra / element-plus。
- [x] 无 CI / provisioning：无 `.github/workflows`、ansible、terraform。
- [x] 凭证不入库：裸 TG token grep 零命中；`.env` 被 gitignore 且未被 git 跟踪。

**关键决策落地**：
- [x] D1 单仓四顶层布局已落地：`backend/`、`frontend/`、`api/`、根级 Makefile。
- [x] D2 golang-migrate 已落地；ADR-002 仍需 `cs-domain` 补一行“已选定 golang-migrate”的落定记录。
- [x] D3 bcrypt + JWT + env seed 已落地，缺 seed fail-fast 覆盖。
- [x] D4 OpenAPI 全量 + Go auth tag / TS 全量 codegen 已落地。
- [x] D5 AccountScope + depguard + 双账号测试已落地。
- [x] D6 Dockerfile + compose 双轨已落地，QA 已真跑全容器与接现有 PG。
- [x] D7 go:embed SPA fallback 已落地，QA 有二进制直跑证据。

**编排层与流程约束核对**：
- [x] 中间件顺序、启动顺序、契约线、错误语义、幂等、鉴权不变量、可观测点均有代码或测试证据。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点八类均存在：路由、前端路由、迁移、Makefile、OpenAPI/codegen、env key、Docker/compose、TG 冒烟脚本。
- [x] 反向 grep：本 feature 的新增代码均落在 design 2.3 清单内；验收修复的 router 旁路属于“路由注册 / SPA fallback”挂载点内。
- [x] 拔除沙盘推演：删除上述八类挂载点后，平台基座行为与验证入口不再存在；无清单外残留。

## 3. 验收场景核对

- [x] A1 `make check`：通过（build + lint + test + generate-check）。
- [x] A2-A6 登录、`/me` 与鉴权错误路径：通过，见 `auth_test.go` 与 QA-002。
- [x] A7 healthz、未注册 API / 方法不匹配 404：通过；REV-007 旁路已在 acceptance 中修复并由新增测试覆盖。
- [x] A8 panic → 500 internal 且进程存活：通过。
- [x] A9 双账号 AccountScope 隔离：通过，QA 用真实 PG 容器复跑。
- [x] A10 migrate up、seed 幂等、缺 seed fail-fast：通过。
- [x] A11 OpenAPI 端点集合与生成物零漂移：通过。
- [x] A12 TG 冒烟：通过，证据 `evidence/a12-tg-smoke-received.png`。
- [x] A13 浏览器三段路径：通过，证据 `evidence/a13-*.png`。
- [x] A14 部署双轨：通过，QA 已真跑 docker build、compose 全容器、接现有 PG、二进制 go:embed。

**review 报告重点复核**：
- [x] `platform-skeleton-review.md` status=passed，无 blocking / important 未解决。
- [x] Test And QA Focus 已由 `platform-skeleton-qa.md` 覆盖；其中 REV-007 在 acceptance 中补修并复验。
- [x] Residual risk 无核心验收缺口；保留项均为后续 hardening / 合并策略 / owner 部署确认。

**QA 报告重点复核**：
- [x] 验证证据来源：`platform-skeleton-qa.md`，frontmatter status=passed。
- [x] QA Matrix 覆盖 A1-A14 与 review QA focus。
- [x] 功能性核心路径均有运行证据；无 failed / blocked。
- [x] Evidence pack / DoD / Gate results：非 goal/gate 模式，无独立文件；DoD 命令已在 QA 与 final audit 中复核。

## 4. 术语一致性

- `账号（Account）`：已在 `requirements/CONTEXT.md` 定义；代码使用 Account / AccountContext / AccountScope，与禁用“用户”口径不冲突。
- `AccountContext` / `AccountScope` / `ErrorEnvelope`：均为技术实现名词，不进入领域术语表；代码命名一致。
- 防冲突：backend / frontend/src 对“用户”禁用词无命中；设计中的“用户故事”等流程文档用词不属于代码术语冲突。

## 5. 领域影响盘点（提示而非代写）

- [x] `AccountScope`（技术实现名词）：不需要写 CONTEXT；可作为后续 feature 的 code review 检查点。
- [x] `golang-migrate` 选定（ADR-002 既有待落定节点）：建议走 `cs-domain` 更新 ADR-002，不在 accept 里代写。
- [x] D6 部署双轨与 D7 go:embed 单工件：已由 design 记录，若 owner 认为会长期约束部署口径，建议走 `cs-domain` 或 `cs-keep` 沉淀。
- [x] `/api/v1/me` 收编进 roadmap §4：属于 roadmap scope 修订，建议走 `cs-roadmap update`；accept 仅机械回写完成状态。

## 6. requirement delta / clarification 回写

- [x] 方案 frontmatter `requirement: null`。
- [x] 本 feature 是 roadmap 的 greenfield 安全网 / 平台基座，未改变 `customer-profile` 的用户故事、边界或 pitch。
- [x] 无 owner-approved req delta 需要机械应用；不在 accept 阶段自由 backfill 新 requirement。结论：无 requirement 影响，跳过。

## 7. roadmap 回写

- [x] frontmatter `roadmap: photographer-private-crm`、`roadmap_item: platform-skeleton` 均有值。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 中 `platform-skeleton` 已由 `in-progress` 回写为 `done`，feature 匹配 `2026-07-06-platform-skeleton`。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` 第 5 节子 feature 清单已同步为 `状态：done`、`对应 feature：2026-07-06-platform-skeleton`。
- [x] `python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 通过。

## 8. attention.md 候选盘点

- 候选 1：`make check` 已成为后续 roadmap feature 的基线验证入口，建议用 `cs-note` 加到 attention.md 的“测试 / 编译与构建”。
- 候选 2：本地起服务依赖 `.env.example`、Docker / compose PG、env key（`DATABASE_URL`、`AUTH_TOKEN_SECRET`、`SEED_ADMIN_PASSWORD`、`HTTP_ADDR`、`TELEGRAM_BOT_TOKEN`），建议用 `cs-note` 加到“运行与本地起服务 / 环境变量与凭证”。
- 候选 3：TG bot token 只经环境变量注入已在 attention.md 有规则；无需重复。

其他分流：
- `golang-migrate` 落定、部署双轨、go:embed 单工件：建议 `cs-domain` / `cs-keep`。
- README 已覆盖开发与部署段；如要对外指南化，可后续走 `cs-doc-tutorial`。

## 9. 遗留

- 后续优化点：
  - REV-004：`http.Server` 尚无 `IdleTimeout`，归 v1-hardening。
  - R2-05：`AccountScope` 标识符校验暂不支持 `*` / `count(*)` / alias / schema 限定名，后续聚合查询出现时显式扩展。
  - R2-06：compose `env_file.required` 需要 Docker Compose v2.24+，README 可补版本要求。
  - R2-09 / R2-10：前端 request helper 204 空 body 与 guard query/hash 回跳在后续域 feature 前修。
- 已知限制：
  - REV-003 owner 已接受：本地 compose PG 端口 `0.0.0.0:5432` 与弱默认口令回退；真实部署必须改 `.env` 并复核端口暴露。
  - 设计登记的安全残留：无登录限速、明文 HTTP、JWT 30 天窗口、localStorage token XSS 面，归 v1-hardening。
  - R2-07：feat 分支历史含已删除的 1.5MB 二进制 blob；合并 develop 时须 squash/rebase，避免污染共享历史。
- 实现阶段顺手发现：
  - QA residual REV-007 已在 acceptance 中修复，不再作为遗留。

## 10. 最终审计

- 验证证据来源：`platform-skeleton-qa.md` + acceptance 现场复验。
- Evidence sources：非 goal/gate 模式，无 `{slug}-evidence-pack.md` / DoD results / Gate results；截图证据在 `evidence/`。
- Inline Verification Matrix：不适用，已有 QA 报告。
- 聚合命令：
  - `make check` → exit 0。首次普通 sandbox 因外部 worktree `node_modules/.tmp/*.tsbuildinfo` 写入 EPERM 失败，授权外部项目写入后同命令通过。
  - `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` → exit 0，无漂移。
  - `go test -count=1 ./internal/platform/httpapi` → exit 0，覆盖 acceptance 修复的 REV-007 旁路。
  - `git grep -nE '[0-9]{6,}:[A-Za-z0-9_-]{30,}' -- ':!.codestable'` → exit 1，零命中，符合 CMD-005。
  - `git check-ignore -q .env` → exit 0；`git ls-files --error-unmatch .env` → exit 1，符合 CMD-006。
  - `python3 .codestable/tools/validate-yaml.py --file .codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` → exit 0。
- 场景复核：re-verified 10 / trust-prior-verify 4。trust-prior 项为 A12 真机 TG、A13 浏览器截图、A14 Docker/compose/二进制形态与接现有 PG，均来自 QA 报告和 evidence 文件。
- 交付物复核：代码 / 配置 / schema / 路由 / 文档 / requirement / roadmap 均通过；requirement 结论为无影响；roadmap 已回写。
- 完整工作区复核：`git status` 显示本次 acceptance 修改 router、router test、checklist、roadmap、acceptance；`platform-skeleton-qa.md` 仍为未跟踪输入产物，纳入最终交付范围。
- diff 清洁度：通过。源码范围无 `fmt.Println` / `console.*` / TODO / FIXME / XXX；构建产物中的第三方 minified `console.error` 不计入源码残留。
- 知识沉淀出口：attention 候选已分流；ADR / roadmap scope 候选已提示走 `cs-domain` / `cs-roadmap update`；TG 与验证模式可走 `cs-keep`。
- 缺口修复记录：QA residual REV-007 在 acceptance 中修复；新增 `RedirectTrailingSlash=false`、API 前缀识别与非导航 fallback 限制，并补测试。复验通过。
- 结论：通过；无未处理核心验收缺口。
