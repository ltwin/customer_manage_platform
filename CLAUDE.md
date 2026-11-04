# CLAUDE.md

摄影师私域客户经营系统（CRM）：Go + Gin + React + PostgreSQL，阿里云 ECS 自部署。需求、设计、进度在 Multica 项目「光合工坊」；仓库 `docs/` 只保存长期知识。

## 工作流

- 需求、设计、进度、评审都在 Multica 项目「光合工坊」的 issue 里走，按项目 workflow 推进；仓库只保存长期知识（`docs/`），进度和讨论不落仓库。
- 一个 issue 对应一个 PR；完整轨的设计记录写 `docs/design/GUAN-<n>.md`，和代码同一个 PR。
- 验证按影响面选门禁，判定表见 [AGENTS.md 「验证范式」](AGENTS.md#验证范式)：日常用 `make check-frontend` / `check-go PKG=...` / `check-ops`，合回 `develop` 用 `make check-merge`（在主检出 `git merge --no-commit` 后跑，后端靠测试缓存只重跑受影响的包），`make check` 留给发布前；都不是每次编辑的默认动作。
- OpenAPI 变更后运行 `make generate` 并提交 Go/TS 两份生成物。
- 语言：`docs/` 与 issue 产出的正文用中文；机器状态（YAML / JSON / frontmatter 字段）保持机读格式。
- `.lti/` 是过渡期遗留，新需求不使用；`scene-editor-3d` 收尾之前，它的 contracts 和 lessons 仍以 `.lti/` 为准。
- 已退役的 CodeStable 目录归档在 tag `archive/codestable-final`：历史引用写成 `archive/codestable-final:.codestable/<path>`，用 `git show archive/codestable-final:.codestable/<path>` 打开；带 `#<locator>` 的先去掉 `#` 及之后的部分再打开，用 `grep` 定位：`ref='archive/codestable-final:.codestable/<path>#<locator>'; git show "${ref%%#*}" | grep -n -- "${ref#*#}"`。

## 硬规则（写代码必须遵守）

硬规则优先级最高；Go / 前端 checklist 是风格与惯用法层，和硬规则冲突时硬规则赢。

1. **账号隔离（ADR-001）**：任何业务表必须带 `account_id`；任何查询必须限定账号范围；客户端永不传 `account_id`——过滤在 repository 基座强制。
2. **Handler 薄层（ADR-003）**：领域逻辑（状态机 / merge / 提醒规则）不得 import 路由框架；`gin.Context` 不下穿 service / repository 层。
3. **契约先行**：API 契约以 `api/openapi.yaml` 为准；契约不合理时改契约本身，改契约走完整轨、经过 Owner「方案确认」，不得在实现里绕开。
4. **凭证红线**：TG bot token 等一切凭证只经环境变量注入，不入库、不入 git。
5. **术语**：按 `docs/domain/glossary.md`；「用户」是禁用词（说「账号」= 摄影师，「客户」= 拍摄对象）。
6. **Go 编码规范（2026-07-06 拍板）**：Uber Go Style Guide（中文参考 <https://github.com/xxjwxc/uber_go_guide_cn>）；code review 按 `docs/go-style-checklist.md` 逐项检查，工具可查项交给 gofmt / golangci-lint。
7. **前端编码规范（2026-07-07 拍板）**：工具链优先——TS `strict: true` + typescript-eslint strict + react-hooks + Prettier；code review 按 `docs/frontend-style-checklist.md` 逐项检查；API 类型一律引用 openapi-typescript 生成的 `schema.d.ts`，禁手写重复 DTO。

## Git 约定（GitFlow）

- 长期分支：`main` = 生产，只收发布合并；`develop` = 集成基线，日常工作分支从这里拉出、合回。
- 工作分支：`feat|fix|refactor/GUAN-<n>-<slug>`，从 `origin/develop` 拉；Epic 集成分支 `epic/GUAN-<n>`。不要用 `feature/*`、`perf/*` 等其他前缀。
- PR 标题以 `GUAN-<n>:` 开头，目标 `develop`（Epic 子 issue → `epic/GUAN-<父 n>`），不向 `main` 开 PR。
- agent 可以在自己的分支上提交和推送；合入 `develop` / `main` 只能由 Owner 完成。
- `release/*` / `hotfix/*` 保留给发布节奏出现之后；首版迭代期 `develop` → `main` 直接走发布合并即可。
- lint 门禁：clone 后先跑 `make hooks` 启用 pre-commit（`core.hooksPath=scripts/githooks`，写在仓库级 `.git/config`，所有 worktree 共用）。pre-commit 按暂存路径分派：动了 `backend/` 的 Go 文件或 lint 配置跑后端 lint，动了 `frontend/` 跑前端 lint（缺 `frontend/node_modules` 时拒绝提交，先 `make frontend-install`），只改文档 / 脚本时跳过。lint 不过不许提交。
- **任何 agent 禁止 `git commit --no-verify`**；环境坏了需要绕过 hook，只能由 Owner 手动操作并说明原因。

## 文档索引

| 要找什么 | 位置 |
|---|---|
| 验证范式与门禁判定表 | `AGENTS.md`「验证范式」 |
| 领域术语表 | `docs/domain/glossary.md` |
| 已拍板架构决策（ADR 001-009） | `docs/adr/` |
| 已拍板工程决策与经验沉淀 | `docs/decisions/` |
| 能力愿景索引 | `docs/product/vision.md` |
| 能力需求文档 | `docs/product/capabilities/` |
| 提醒引擎能力与边界 | `docs/product/capabilities/reminder-engine.md` |
| 单需求设计记录 | `docs/design/GUAN-<n>.md` |
| HTTP API 契约 | `api/openapi.yaml`（参考清单 `docs/api/manifest.yaml`，已有条目见 `docs/api/`） |
| Reminder / Settings API 参考 | `docs/api/reminders.md`、`docs/api/settings.md` |
| 客户头像用户/开发/API 指南 | `docs/user/customer-avatar.md`、`docs/dev/customer-avatar.md`、`docs/api/customer-avatar.md` |
| 对象存储驱动与 OSS bucket 初始化 | `docs/dev/object-storage.md` |
| Skill 受信导入与回收 CLI | `docs/dev/creative-skill-import.md` |
| 外部助手（MCP 端点、OAuth、本机接入与排错） | `docs/dev/creative-mcp.md` |
| 运维指南与 ops 输入 | `docs/ops/`（v1-hardening 的 ops catalog 等脚本输入在 `docs/ops/v1-hardening/`；手动跑的 ops / rehearsal 脚本证据默认写到 gitignore 掉的 `var/evidence/<topic>/`，要保留的结论贴进 issue 评论） |
| 头像 generation 模式沉淀 | `docs/decisions/2026-07-13-avatar-immutable-generation-pattern.md` |
| Go 编码规范 checklist | `docs/go-style-checklist.md`（决策：`docs/decisions/2026-07-06-decision-go-uber-style-guide.md`） |
| 前端编码规范 checklist | `docs/frontend-style-checklist.md`（决策：`docs/decisions/2026-07-07-decision-frontend-toolchain-first-standard.md`） |
| 文档链接检查 | `python3 scripts/check-doc-links.py --verify-archive` |
