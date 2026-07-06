# CLAUDE.md

摄影师私域客户经营系统（CRM）：Go + Gin + React + PostgreSQL，阿里云 ECS 自部署。截至 2026-07-06 规划层已完成（roadmap active），业务代码尚未开工。

## 工作流

- 本项目用 CodeStable 管理规格与流程；任何 CodeStable 技能启动前先读 `.codestable/attention.md`。
- 功能实现从 roadmap 取条目：`.codestable/roadmap/photographer-private-crm/`（11 条子 feature，依赖序）。

## 硬规则（写代码必须遵守）

1. **账号隔离（ADR-001）**：任何业务表必须带 `account_id`；任何查询必须限定账号范围；客户端永不传 `account_id`——过滤在 repository 基座强制。
2. **Handler 薄层（ADR-003）**：领域逻辑（状态机 / merge / 提醒规则）不得 import 路由框架；`gin.Context` 不下穿 service / repository 层。
3. **契约先行**：API 以 roadmap §4 为语义权威源、OpenAPI 文件为机器形式；契约不合理回 `cs-roadmap update` 改，不得在 feature 里绕开。
4. **凭证红线**：TG bot token 等一切凭证只经环境变量注入，不入库、不入 git。
5. **术语**：按 `.codestable/requirements/CONTEXT.md`；「用户」是禁用词（说「账号」= 摄影师，「客户」= 拍摄对象）。

## 文档索引

| 要找什么 | 位置 |
|---|---|
| 启动必读注意事项 | `.codestable/attention.md` |
| 领域术语表 | `.codestable/requirements/CONTEXT.md` |
| 已拍板决策（ADR 001-003） | `.codestable/requirements/adrs/` |
| 能力愿景索引 | `.codestable/requirements/VISION.md` |
| 规划与接口契约（硬约束） | `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` |
| 需求讨论原始记录 | `.codestable/brainstorms/photographer-private-crm/brainstorm.md` |
