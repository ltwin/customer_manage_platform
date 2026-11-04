---
id: 007
title: 旧 Seed 账号原地认领而非新租户搬迁
status: accepted
date: 2026-07-30
relates_to:
  - 001
  - 004
  - 005
  - 006
  - requirements/self-service-account-system
  - roadmap/self-service-account-system
---

# 旧 Seed 账号原地认领而非新租户搬迁

## Context

升级前的部署通过 `SEED_ADMIN_PASSWORD` 创建唯一默认账号，登录只校验最早账号的密码。该账号的稳定 `account_id` 已被客户、订单、档期、套系、提醒、头像 object key、Settings、后台 checkpoint 与导出数据广泛引用。

公开自助注册需要为 owner 建立已验证邮箱身份并退役 password-only 入口。如果先创建新账号再搬迁全库数据，必须同时改写关系表、对象存储引用、运行中 checkpoint 与备份／回滚语义；漏掉任何一条都会造成数据不可见或跨账号归属错误。保留临时双登录自行绑定虽然减少一次性运维步骤，却会延长旧认证面并引入账号抢占和切换时点不确定性。

迁移策略涉及不可逆的数据归属和生产 owner 访问，一旦出错可能锁死既有 CRM 数据；同时存在原地认领、新账号搬迁和双登录过渡等真实备选，满足 ADR 守门三判据。

## Decision

1. 升级 migration 保留原 `accounts.id`，把既有 bcrypt hash 复制到新的密码凭证，并把账号标为 `legacy_unclaimed`；不创建伪邮箱、不自动标记 verified，也不修改任何业务表、头像 object key、Settings、checkpoint 或导出数据的 `account_id`。
2. 旧账号只能通过受信 operator 命令 `accountctl auth claim-legacy` 认领。命令要求恰好一个可认领目标，先支持无写入／不发信的 dry-run，再绑定未验证邮箱身份并发送 purpose 为 `legacy_claim` 的一次性动作令牌；公开注册入口不得认领既有账号。
3. Owner 消费 claim token 后，原账号和 identity 在事务中激活并建立新会话；验收必须证明 `/me` 返回同一 account ID，全部 CRM 资源计数、隔离关系和头像读取保持不变。
4. 切换 preflight 必须机械证明：没有 `legacy_unclaimed` 或 pending claim、password-only route 不存在、旧 JWT 被拒绝、`SEED_ADMIN_PASSWORD` 不再参与启动；任一失败都不得宣布 cutover ready。
5. 新空库不再自动 seed 默认账号。零账号部署由受信 `accountctl auth bootstrap` 复用正常注册／验证应用入口建立首账号，不受公开注册开关和公开 limiter 约束；已有任意账号时固定拒绝。
6. 本 epic 内保留 deprecated／nullable 的旧密码哈希作为旧二进制 operator rollback 兼容；新账号不写旧列。旧二进制无法识别 cutover 后新增的公开账号，这一降级边界必须写入回滚报告。
7. 生产 owner 邮箱绑定、旧认证 cutover、root secret 轮换、迁移执行和 rollback 仍是独立外部授权，不由 roadmap、ADR 或 feature design 批准自动触发。

## Consequences

- 正面：稳定 `account_id` 与所有 CRM 业务数据归属保持不变，不需要跨全库、对象存储和后台状态的大规模租户搬迁。
- 正面：认领只增加认证元数据，可用 dry-run、唯一目标约束、一次性 token 和 cutover report 形成可审计的安全路径；公开用户不能抢占 legacy account。
- 正面：新部署不再依赖共享 seed password，同时在公开注册默认关闭时仍有可信的首账号 bootstrap 路径。
- 负面：升级期间需要显式的 `legacy_unclaimed` 状态、operator 命令、真实邮件投递、回滚兼容列和生产 preflight，发布步骤比自动迁移更长。
- 负面：回滚旧二进制只能恢复原 owner 访问，无法识别切换后新增的公开账号；备份中的历史认证数据也不会因在线认领而自动重写或删除。
- 新约束：认领完成前 legacy 账号不能访问 CRM API，也不能被后台账号枚举选中；认领和 bootstrap 的秘密不得出现在命令行参数、日志或持久化明文中。
- 新约束：任何宣称迁移完成的 acceptance 都必须包含同 account ID、资源计数、跨账号隔离、旧路由／JWT negative probe 与回滚演练证据。

## Alternatives Considered

- **新建公开账号后搬迁全部 CRM 数据**：账号模型干净，但要改写所有业务外键、头像 object key、Settings、后台 checkpoint、导出与备份关联；失败面最大，也直接违背稳定账号键决策。否决。
- **临时保留 password-only 登录，由 owner 登录后自助绑定邮箱**：减少运维 CLI，却延长旧 30 天 JWT 和共享 password-only 入口的可攻击窗口；并发注册、错误账号选择与旧入口退役时点更难机械证明。否决。
- **Migration 自动为旧账号写入预配置邮箱并直接标记 verified**：上线步骤最少，但无法证明邮箱所有权，配置错误会把全部 CRM 数据交给错误身份，也缺少安全可重试的认领证据。否决。
- **继续永久保留单账号 seed 模式，与公开账号并行**：避免 cutover，却形成两套长期认证协议和特权入口，所有安全修复、限速、session 与回归都要重复维护。否决。
