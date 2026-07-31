---
doc_type: approval-report
unit: self-service-account-system
status: approved
reason: interview
approvals:
  primary-registration-identity: approved
  email-verification-admission: approved
  browser-session-model: approved
  initial-account-security-scope: approved
  legacy-seed-account-migration: approved
  brainstorm-next-step: approved
approval_groups: {}
created_at: 2026-07-30
---

# Approval Report

## Decision History

### 2026-07-30 — primary-registration-identity

- Owner 选择 A“邮箱 + 密码”作为首期 canonical identity。
- 手机号同样属于目标能力，但首期不实现；后续作为绑定身份与新增登录通道接入。
- 结果：`primary-registration-identity: approved`。

### 2026-07-30 — email-verification-admission

- Owner 选择 A“验证后才能进入经营台”。
- 注册后账号处于 `pending_verification`；完成邮箱验证后才能激活并访问 CRM 业务 API。
- 未验证账号不获得受限经营台权限，避免把双状态授权扩散到各业务域。
- 结果：`email-verification-admission: approved`。

### 2026-07-30 — browser-session-model

- Owner 选择 B“短期 access JWT + 可轮换 refresh session”。
- access JWT 用于 API 短期授权；refresh token 通过 HttpOnly cookie 持有，并由服务端保存、轮换和撤销 refresh session。
- 正式方案不再以 `localStorage + 30 天单 JWT` 作为浏览器长期会话模型。
- 该方向为未来移动端／开放客户端保留扩展能力，同时接受双 token 生命周期和重放防护的额外复杂度。
- 结果：`browser-session-model: approved`。

### 2026-07-30 — initial-account-security-scope

- Owner 选择 A“公开可用的最小安全闭环”。
- 首期包含邮箱注册与验证、邮箱密码登录、access/refresh 生命周期、当前设备退出、忘记／重置密码、登录后改密、改密／重置后撤销全部 refresh session，以及登录限速。
- 设备／会话列表、逐设备管理、修改邮箱、账号注销／删除和安全审计中心明确延后。
- 结果：`initial-account-security-scope: approved`。

### 2026-07-30 — legacy-seed-account-migration

- Owner 选择 A“原地认领旧账号”。
- 保留旧 `account_id` 与全部 CRM 数据，通过一次性运维认领流程绑定并验证 owner 邮箱。
- 认领完成后关闭 password-only 旧登录入口；未来空库不再自动 seed 默认账号，直接进入公开注册流程。
- 不创建新租户再搬迁全库业务数据和头像对象。
- 结果：`legacy-seed-account-migration: approved`。

### 2026-07-30 — brainstorm-next-step

- Owner 选择 A“现在进入 `cs-epic` planning”。
- 规划约束：feature 不得拆得过小过细；优先聚合成可独立交付、可端到端验收的业务闭环，控制跨 feature 协调成本与开发效率损耗。
- ADR 候选保留为实现前 follow-up，不因进入 roadmap 而省略。
- 结果：`brainstorm-next-step: approved`。

## Decision Needed

无。Brainstorm 的全部 owner 决策已完成，下一步已批准进入 `cs-epic` planning。

## Why Now

Owner 已批准把本 brainstorm 交给 `cs-epic` planning，并额外要求 feature 保持足够粗粒度，避免为流程拆成过多小条目而损失开发效率。

## Context

- 已确认方向已写入 `.codestable/brainstorms/self-service-account-system/brainstorm.md`。
- 这是公开自助注册的多用户账号系统，不应塞入一个单 feature。
- 可复用现有 `AccountScope`、业务数据隔离与薄 handler 约束；主要变化集中在账号身份、认证会话、邮件 token、安全闭环、迁移与前端接入。
- 结构性 ADR 候选至少包括：账号与登录身份的稳定模型、access/refresh 会话与撤销模型、旧 seed 账号原地认领策略。
- 本轮未选择邮件服务供应商，也未确定 token 时长、cookie/CSRF 细节或 API shape；这些属于 epic/ADR/design 的后续工作。

## Options

### A. 现在进入 `cs-epic` planning（已选择）

把 brainstorm 作为输入，拆解账号身份与注册验证、登录与 session rotation、密码恢复与安全防护、旧账号认领、前端接入及上线迁移等子 feature，并梳理依赖、契约和最小闭环。ADR 候选作为 roadmap 架构输入保留，在实现前再由 `cs-domain` 正式落盘。

### B. 先进入 `cs-domain` 落 ADR，再做 epic planning

先把账号／身份分离、access/refresh 会话和旧账号原地认领写成正式 ADR，再返回 `cs-epic` 拆 roadmap。架构证据最早固化，但 ADR 的具体 Context/Decision 可能还需要 epic planning 补充接口和迁移细节。

### C. 暂停在 brainstorm

只保存当前讨论，不启动下游流程。之后再次进入 `cs-brainstorm` 或 `cs-epic` 时，从现有文档恢复，不需要重新回答本轮问题。

## Recommendation

Owner 已选择 A。Epic planning 必须以可独立交付的业务闭环为 feature 边界，避免按表、端点或技术层机械拆分；只有存在真实依赖、独立风险或独立验收价值时才拆成不同 feature。

## Risks And Tradeoffs

- A 最快进入正确的多 feature 拆解，但必须把 ADR 作为实现前的显式 follow-up，不能因 roadmap 已写就省略结构性决策记录。
- B 先得到正式决策证据，但在模块边界和接口尚未展开时，ADR 可能需要较快修订；适合 owner 更看重先锁架构的情况。
- C 没有实施风险，但公开账号系统继续停留在讨论态，现有 password-only 认证 residual 不会发生变化。

## Non-Automatic Actions

本次批准只允许进入 `cs-epic` planning，不自动实现、提交代码、执行数据库迁移、绑定真实邮箱、失效旧 token、修改环境变量、采购邮件服务或接受安全 residual。

## After You Answer

加载 `cs-epic` planning，传递 brainstorm 路径、5 项已确认方向、ADR 候选与“feature 不过度细拆”的规划约束。
