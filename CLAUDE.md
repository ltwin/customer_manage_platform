# CLAUDE.md

摄影师私域客户经营系统（CRM）：Go + Gin + React + PostgreSQL，阿里云 ECS 自部署。项目处于首版实现期；已完成能力与待办状态以 roadmap items 为准。

## 工作流

- 本项目用 CodeStable 管理规格与流程；任何 CodeStable 技能启动前先读 `.codestable/attention.md`。
- 功能实现从 roadmap 取条目：`.codestable/roadmap/photographer-private-crm/`（13 条子 feature，依赖序）。
- 当前执行状态以 `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 为准；不要从旧 feature 报告或对话推断进度。
- 聚合验证入口是 `make check`；OpenAPI 变更后运行 `make generate` 并提交 Go/TS 两份生成物。

## 硬规则（写代码必须遵守）

1. **账号隔离（ADR-001）**：任何业务表必须带 `account_id`；任何查询必须限定账号范围；客户端永不传 `account_id`——过滤在 repository 基座强制。
2. **Handler 薄层（ADR-003）**：领域逻辑（状态机 / merge / 提醒规则）不得 import 路由框架；`gin.Context` 不下穿 service / repository 层。
3. **契约先行**：API 以 roadmap §4 为语义权威源、OpenAPI 文件为机器形式；契约不合理回 `cs-roadmap update` 改，不得在 feature 里绕开。
4. **凭证红线**：TG bot token 等一切凭证只经环境变量注入，不入库、不入 git。
5. **术语**：按 `.codestable/requirements/CONTEXT.md`；「用户」是禁用词（说「账号」= 摄影师，「客户」= 拍摄对象）。
6. **Go 编码规范（2026-07-06 拍板）**：Uber Go Style Guide（中文参考 <https://github.com/xxjwxc/uber_go_guide_cn>）；code review 按 `docs/go-style-checklist.md` 逐项检查，工具可查项交给 gofmt / golangci-lint。
7. **前端编码规范（2026-07-07 拍板）**：工具链优先——TS `strict: true` + typescript-eslint strict + react-hooks + Prettier；code review 按 `docs/frontend-style-checklist.md` 逐项检查；API 类型一律引用 openapi-typescript 生成的 `schema.d.ts`，禁手写重复 DTO。

## Git 约定（GitFlow，2026-07-06 拍板）

- 长期分支：`main` = 生产（受 CodeStable branch-guard 保护，只收 `release/*` / `hotfix/*` 合并）；`develop` = 集成基线，日常工作分支从这里拉出、合回。
- 工作分支沿用 CodeStable 类型前缀：`feat/*`（新功能，≈ GitFlow feature）、`fix/*`（缺陷修复）、`refactor/*`（重构）——不要用 `feature/*` 命名，branch-guard 认的是 `feat/`。
- `release/*` / `hotfix/*` 保留给发布节奏出现之后；首版迭代期 `develop` → `main` 直接走发布合并即可，不为不存在的发布流程预支分支。
- `develop` 是当前集成基线；工作分支完成后按 scoped commit 合回，不在 `main` 上直接开发。

## 文档索引

| 要找什么 | 位置 |
|---|---|
| 启动必读注意事项 | `.codestable/attention.md` |
| 领域术语表 | `.codestable/requirements/CONTEXT.md` |
| 已拍板决策（ADR 001-004） | `.codestable/requirements/adrs/` |
| 能力愿景索引 | `.codestable/requirements/VISION.md` |
| 提醒引擎能力与边界 | `.codestable/requirements/reminder-engine.md`（实现细节与已知边界见 roadmap 的 reminder 章节） |
| 规划与接口契约（硬约束） | `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` |
| Roadmap 当前执行状态 | `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` |
| 需求讨论原始记录 | `.codestable/brainstorms/photographer-private-crm/brainstorm.md` |
| HTTP API 参考清单 | `docs/api/manifest.yaml`（已有条目见 `docs/api/`，机器契约仍以 `api/openapi.yaml` 为准） |
| Reminder / Settings API 参考 | `docs/api/reminders.md`、`docs/api/settings.md` |
| 客户头像用户/开发/API 指南 | `docs/user/customer-avatar.md`、`docs/dev/customer-avatar.md`、`docs/api/customer-avatar.md` |
| 对象存储驱动与 OSS bucket 初始化 | `docs/dev/object-storage.md` |
| 头像 generation 模式沉淀 | `.codestable/compound/2026-07-13-avatar-immutable-generation-pattern.md` |
| Go 编码规范 checklist | `docs/go-style-checklist.md`（决策：`.codestable/compound/2026-07-06-decision-go-uber-style-guide.md`） |
| 前端编码规范 checklist | `docs/frontend-style-checklist.md`（决策：`.codestable/compound/2026-07-07-decision-frontend-toolchain-first-standard.md`） |
