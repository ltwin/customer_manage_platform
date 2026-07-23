---
doc_type: issue-report
issue: 2026-07-22-auth-hardening
status: open
source_feature: 2026-07-22-v1-hardening
owner_decision: accepted-residual
owner_decision_date: 2026-07-22
created: 2026-07-22
tags: [security, authentication, follow-up, open-risk]
---

# auth-hardening 开放风险报告

## 1. 摘要

owner 于 2026-07-22 在 `v1-hardening` 统一设计确认中选择 H1-A：当前 V1 接受四项既有认证 residual，不在首版收口期重写认证模型，并授权把它们持久化为独立的后续入口。

本报告是 canonical open-risk intake，不表示风险已经修复、缓解或验收通过，也不授权直接改动认证代码。

## 2. 已接受的当前风险

| 风险 | 当前事实 | 主要影响 |
|---|---|---|
| 无登录限速或失败锁定 | 登录入口没有基于账号、来源或时间窗的速率限制/失败预算 | 在线猜测、撞库或暴力尝试缺少应用层抑制 |
| JWT 有效期 30 天 | 当前 token TTL 固定为 30 天 | token 泄露后的可利用窗口较长，主要依赖 secret 轮换终止存量 token |
| Bearer token 存于 localStorage | Web 客户端从 localStorage 读取认证 token | 一旦发生可执行脚本注入，token 可能被读取并外带 |
| 无改密 API | 当前没有 owner 自助修改密码的公开 API/UI | 凭证轮换依赖运维或后续能力，日常撤换和事件响应不够直接 |

## 3. Owner 决策

- 决策：接受上述风险作为当前 V1 residual。
- 日期：2026-07-22。
- 来源回答：“按推荐项批准”，对应 `D1-A + H1-A + H2-A`。
- Durable confirmation ID：`47299893-6531-4ce7-91ad-58f47c895ea1`。
- Canonical approval：`.codestable/roadmap/photographer-private-crm/approval-report.md` 的 `v1-hardening-design-confirmation` group。

## 4. 延后理由与边界

1. `v1-hardening` 的当前目标是首版页面状态、375px 移动轻路径、生产运维安全网与全链路回归；认证模型重写会改变公开安全契约、前后端存储/刷新行为和验收面。
2. 登录限速、JWT 生命周期、token 存储和改密能力相互关联，适合独立完成威胁建模、兼容/迁移设计与回滚方案，不应作为收口 feature 的临时中间件或局部补丁混入。
3. 当前批准只接受已列出的四项 residual；不允许据此宣称明文 HTTP、TLS、网络边界、secret 管理或其他安全问题已经解决。
4. `v1-hardening` 实现不得临时新增登录 429/lockout、cookie auth、JWT TTL 配置或改密端点；若需要提前改变这些边界，必须先回到设计流程。

## 5. 建议的后续入口

- 若只处理一个边界清晰、可独立验收的认证增量，使用 `cs-feat` 创建新的 auth-hardening feature。
- 若需要联合设计登录防护、token 生命周期/撤销、浏览器会话存储、改密/重置、审计与部署迁移，使用 `cs-epic` 拆成有依赖顺序的多个子 feature。
- 在任何实现前先形成新的 requirement/design、威胁模型、兼容与迁移策略，并走独立 design review。

## 6. 后续设计至少需要回答

1. 登录限速的 key、时间窗、分布式状态、可信代理/IP 边界、错误响应和误伤恢复策略。
2. JWT access/session 生命周期、撤销与 secret rotation 对已有会话的影响。
3. localStorage Bearer 迁移到更安全会话模型时的 CSRF、XSS、跨标签页、刷新与退出语义。
4. 改密/重置的当前密码验证、会话失效、恢复通道、审计与紧急处置。
5. 对现有 API、OpenAPI、前端 client、部署配置和 owner 操作手册的兼容/回滚计划。
6. 自动化安全回归、浏览器证据、日志脱敏和生产监控/告警门槛。

## 7. 关闭条件

只有新的认证 hardening 工作完成设计批准、实现、独立 code review、QA 与 owner acceptance，并逐项证明上述四个风险已修复或由新的明确决策替代后，本报告才能从 `open` 更新为 terminal 状态。单独更新 README、TLS 说明或部署防火墙不能关闭本报告。

## 8. Non-Automatic Actions

- 本报告不自动创建 feature/epic，不修改认证实现、API、数据库或部署配置。
- 不自动 stage、commit、push、merge、release 或 deploy。
- 不把当前 V1 的风险接受扩张为未来版本的永久豁免。
