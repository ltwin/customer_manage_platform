---
id: 005
title: 稳定账号与登录身份、密码凭证分离建模
status: accepted
date: 2026-07-30
relates_to:
  - 001
  - 002
  - requirements/self-service-account-system
  - roadmap/self-service-account-system
---

# 稳定账号与登录身份、密码凭证分离建模

## Context

ADR-001 已把 `account_id` 定义为全部 CRM 业务数据的稳定归属维度，但首版认证基座仍把“最早创建的账号 + `accounts.password_hash`”当成唯一登录入口，没有可验证的登录身份，也默认全系统只有一个使用者。

公开自助注册要求邮箱全局唯一、验证后才能准入，并允许多个账号共享同一套隔离模型。Owner 同时确认手机号是后续真实身份渠道。如果把邮箱和密码继续直接焊在 `accounts` 上，新增手机号、身份换绑或多登录渠道时将迫使账号主表、业务引用和认证流程一起迁移；如果把每个登录身份都当成独立账号，又会破坏稳定 `account_id` 与业务数据归属的语义。

该决策难以回退：身份唯一性、账号状态和凭证归属一旦进入生产数据与认证路径，改模需要跨账号表、令牌、会话、OpenAPI 和全部业务隔离回归。它也存在真实备选，因此满足 ADR 守门三判据。

## Decision

1. `accounts.id` 继续作为稳定账号键和所有 CRM 数据的唯一归属键；业务表、`AccountContext` 与 `AccountScope` 不改用邮箱或 identity ID。
2. 登录身份由独立的 `account_identities` 模型承载，至少包含所属账号、`kind`、规范化后的值和验证状态。首期唯一启用的 `kind` 是 `email`，但模型从第一天允许后续增加 `phone`；同一 `kind + normalized_value` 全局唯一，首期每账号每种 kind 最多一个。
3. 密码凭证由独立的 `password_credentials` 模型承载并归属于账号，不把原始密码、密码哈希或验证细节放入登录身份。首期继续兼容既有 bcrypt hash。
4. 账号生命周期至少区分 `pending_verification`、`active` 与 `legacy_unclaimed`。只有 `active` 账号能获得业务 `AccountContext`、建立或刷新会话，并被后台账号枚举选中。
5. 邮箱所有权验证、密码重置和旧账号认领使用 purpose 隔离的认证动作令牌；identity、credential、action token 与 refresh session 的共享状态由 `account-auth` 模块拥有，其他模块不得直接写。
6. 旧 `accounts.password_hash` 在本 epic 内只作为 deprecated／nullable 回滚兼容列保留；新账号不写该列，其最终删除另行规划。

## Consequences

- 正面：`account_id`、全部 CRM 外键和账号隔离测试保持稳定；增加手机号等新登录渠道不要求搬迁业务数据或重签既有账号主键。
- 正面：登录身份验证、密码凭证和账号生命周期各有单一语义，公开 API 不需要把邮箱当作租户键，也不会把客户的社交身份与系统账号身份混为一谈。
- 正面：未验证和未认领状态在 account-auth 边界被统一挡住，不需要把双状态授权扩散到客户、订单、档期、提醒、头像或导出模块。
- 负面：注册、登录、验证、认领和改密会跨多张认证表，需要清晰的事务 owner、唯一约束和并发测试；模型比把 email/password 直接放进账号表更复杂。
- 新约束：客户端永不提交 `account_id`；只有已验证 access token 能构造 `AccountContext`；后台任务只能枚举 active 账号。
- 新约束：邮箱规范化、identity 唯一冲突和公开防枚举响应必须由 account-auth 统一处理，不能由各 handler 自行实现。

## Alternatives Considered

- **把 email 与 password_hash 直接放在 `accounts`**：首期表更少，但手机号、多个登录身份、换绑和验证状态都会持续膨胀账号主表；身份变化与稳定租户键强耦合。否决。
- **每个登录身份建立一个独立账号**：模型表面简单，却会让同一经营主体的邮箱／手机号形成多个 CRM 数据空间，或迫使后续做账号合并与业务数据搬迁。否决。
- **仅保留外部身份提供商 ID，完全委托第三方认证**：能减少密码与验证实现，但本 epic 已确认邮箱密码、旧 bcrypt 兼容、受信 legacy claim 和自有 refresh 撤销语义；同时会把核心账号可用性绑定到尚未选择的供应商。否决。
- **维持单账号 password-only 基座**：无需迁移，但无法交付公开自助注册、多账号隔离使用和可扩展身份渠道，违背已批准的产品方向。否决。
