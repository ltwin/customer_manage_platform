---
id: 006
title: 短期 Access JWT 配合可轮换服务端 Refresh Session
status: accepted
date: 2026-07-30
relates_to:
  - 001
  - 002
  - 005
  - requirements/self-service-account-system
  - roadmap/self-service-account-system
---

# 短期 Access JWT 配合可轮换服务端 Refresh Session

## Context

现有浏览器会话是写入 `localStorage` 的 30 天 HS256 Bearer JWT。它实现简单且业务请求无需查库，但 bearer secret 长期暴露给脚本环境，服务端无法撤销单次登录；改密、密码重置、退出和疑似重放都不能立即终止长期会话。

公开账号系统既需要浏览器安全边界和服务端撤销能力，又希望保留短期 JWT 对 API 的低成本授权，并为后续原生客户端或其他 API consumer 留出独立 transport 的空间。多标签页并发 refresh、响应丢失和 token reuse 使“只轮换但不处理 grace/replay”的简单方案会误伤正常用户或无法可靠识别盗用。

会话模型一旦发布会固化前端状态机、cookie、数据库、JWT claims、密钥轮换与密码安全事件，切换成本高；候选包括长期 JWT、服务端 opaque cookie session 和 access/refresh 双 token，满足 ADR 守门三判据。

## Decision

1. API 授权使用短期 Access JWT，默认 TTL 10 分钟；至少固定校验算法、issuer、audience、版本、`sub=account_id`、refresh family `sid`、唯一 `jti` 及标准时间 claims。
2. 浏览器只在内存保存 access token，不写入 `localStorage`、`sessionStorage`、IndexedDB、URL 或日志。前端启动通过 refresh 恢复会话，认证 API client 对明确的 auth 401 只做一次全局 single-flight refresh 和单次请求重放。
3. 长期会话由服务端持久化的刷新会话族承载。Refresh token 是高熵 bearer secret，浏览器通过同源 `HttpOnly; Secure; SameSite=Strict` cookie 持有，数据库只保存摘要；默认 idle TTL 14 天、family absolute TTL 30 天。
4. 每次 refresh 在数据库事务中锁定当前 generation，标记已轮换并创建唯一 successor。正常退出撤销当前 family；改密或密码重置撤销该账号全部 refresh family。
5. 为处理多标签页和响应丢失，同一 generation 在默认 10 秒 bounded concurrency grace 内返回同一 successor；服务端只在 grace 内以用途派生的 AEAD key 保存 successor 密文，之后清除。
6. grace 之外再次使用已轮换 token 视为 reuse，原子撤销整个 family、清除 cookie 并返回统一未授权结果；安全日志不得记录 token、密码或完整邮箱。
7. 业务 middleware 不为每个请求查询 refresh session，接受已签发 access JWT 最多存活 10 分钟的剩余风险窗。旧版 30 天 JWT 在新 verifier 切换时立即失效。

## Consequences

- 正面：业务 API 保留短期 JWT 的无状态校验成本；服务端同时获得长期会话轮换、当前设备退出、改密／重置全撤销和 refresh reuse 检测能力。
- 正面：refresh bearer secret 不暴露给普通页面脚本；前端认证状态、cookie transport 和 API 请求重放集中在 `webapp-auth`，不再由每个页面各自处理 401。
- 正面：有界 grace 明确区分正常并发／丢包重试与超窗 reuse，避免单靠前端协调造成竞态误撤销。
- 负面：数据库新增会话族与 generation 状态，refresh 需要事务锁、密文短暂重放、密钥派生和清理逻辑；测试矩阵显著大于长期单 JWT。
- 负面：access JWT 在撤销后仍可能存活至 10 分钟 TTL；若未来要求即时撤销每个 API 请求，需要另立决策引入 blacklist、token introspection 或纯服务端 session。
- 新约束：所有设置、轮换或清除 refresh cookie 的端点必须验证同源 `Origin`；SameSite 只作纵深防御。跨源或原生客户端不得通过放宽本 Web cookie 契约接入。
- 新约束：auth root secret 轮换会使既有 refresh replay 密文和旧 JWT 失效，必须由 preflight/runbook 明确执行顺序与恢复边界。

## Alternatives Considered

- **维持 `localStorage + 30 天 Bearer JWT`**：实现和扩缩容最简单，但 XSS 暴露面、无法服务端撤销、改密后长期有效和旧 token cutover 风险不适合公开注册。否决。
- **纯服务端 opaque cookie session**：撤销和浏览器安全模型直接，但每个 API 请求都依赖 session store，且未来非浏览器 API transport 需要另建授权模型。它是可行方案，但 owner 选择保留短期 JWT 的 API 边界。未选。
- **短期 Access JWT + 不轮换的长期 Refresh Token**：减少 generation 复杂度，却无法检测 refresh 重放，泄露 token 在整个绝对生命周期内持续有效。否决。
- **每次轮换都立即判定旧 token reuse、没有并发 grace**：安全规则简单，但多标签页并发和响应丢失会频繁误撤销正常 family；只靠前端 single-flight 不能覆盖跨标签页与网络竞态。否决。
