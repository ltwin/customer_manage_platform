---
doc_type: roadmap
slug: self-service-account-system
status: active
created: 2026-07-30
last_reviewed: 2026-07-30
tags: [authentication, registration, account, session, security, migration, frontend]
related_requirements: [self-service-account-system]
related_architecture: [001-account-scoped-data-model, 002-postgresql-as-primary-store, 003-monolith-first-gin-openapi, 005-separate-account-identity-and-credential, 006-short-lived-access-and-rotating-refresh-sessions, 007-claim-legacy-seed-account-in-place]
---

# 公开自助注册与多用户认证系统路线

## 1. 背景

系统已经有一套可运行的单账号认证基座：首次启动以 `SEED_ADMIN_PASSWORD` 创建默认账号，`POST /api/v1/auth/login` 只接收密码，后端固定校验最早创建的账号并签发 30 天 HS256 JWT，前端把 Bearer token 保存在 `localStorage`。全部 CRM 业务数据已经通过稳定的 `account_id` 和 `AccountScope` 强制隔离，因此数据归属地基无需推翻。

这套基座服务于“单账号私有部署”的首版边界，不支持公开产品所需的账号标识、自助注册、邮箱所有权验证、密码恢复、服务端会话撤销和登录防爆破。Owner 已在 `.codestable/brainstorms/self-service-account-system/brainstorm.md` 确认把产品升级为公开自助注册、多用户、多租户系统：首期使用邮箱 + 密码，验证后才准入；会话采用短期 access JWT + HttpOnly refresh token + 服务端 refresh session；现有 seed 账号原地认领，保留原 `account_id` 与全部 CRM 数据。

本 epic 以两条粗粒度、全栈垂直 feature 交付。Schema、邮件 adapter、OpenAPI、后端、前端和测试不按技术层拆成独立 feature；只有“核心账号访问闭环”和“公开上线安全闭环”两个可独立验收的业务结果，以减少跨 feature 协调和重复 design 成本。

## 2. 范围与明确不做

### 本 roadmap 覆盖

- 邮箱 + 密码自助注册，邮箱规范化、唯一性和账号生命周期状态；
- 注册后 `pending_verification`，验证邮箱后才成为 active 账号；
- 验证邮件重发与不泄露账号存在性的统一响应；
- 邮箱密码登录、短期 access JWT、HttpOnly refresh cookie、服务端 refresh session、轮换、并发重试和重放撤销；
- 前端注册、待验证、验证结果、登录、会话恢复、透明刷新、路由守卫和退出；
- 忘记／重置密码、登录后改密、改密／重置后撤销全部 refresh session；
- 注册、验证重发、登录和找回入口的持久化限速与账号枚举防护；
- 现有 seed 账号原地认领，旧 password-only 登录、旧 JWT 和 `SEED_ADMIN_PASSWORD` 退役；
- OpenAPI／双端 codegen、跨账号隔离回归、安全用例、迁移／回滚演练和运维说明。

### 明确不做

- 手机号绑定、短信验证码注册／登录——长期需要，首期只通过 `account_identities.kind` 预留稳定扩展点，不引入短信供应商；
- 第三方 OAuth／SSO、API Key、原生 App token transport——本 epic 只交付同源 Web SPA；
- 设备／会话管理中心、逐设备命名与手工下线——首期支持当前设备退出及安全事件下的全 refresh session 撤销；
- 修改／换绑邮箱——需要重新验证、通知旧邮箱与账号接管防护，后续独立规划；
- 账号注销／删除——涉及 CRM 数据保留、导出、头像对象和备份销毁，不混入认证 epic；
- 登录历史、安全事件中心、风控评分、CAPTCHA、计费和运营后台；
- 申请／邀请制开通、内测码等中间形态——Owner 已确认 item2 验收后直接全量公开注册，不建设中间形态；
- 服务条款／隐私政策同意采集与版本记录——公开放量前由 owner 按目标法域单独决策；若确认需要，`accounts` 增加同意版本列即可，不阻塞本 epic；
- `pending_verification` 账号的 TTL 清理与垃圾账号回收——resend 幂等保证真实主人不被占位阻塞，清理策略后续独立规划；
- 把账号隔离升级为 PostgreSQL RLS——继续复用已验证的 `AccountScope` 结构性强制；
- 更换 PostgreSQL、Gin、OpenAPI/codegen 或 bcrypt；密码哈希参数演进可在 feature design 内做兼容升级，但不改技术栈。

### Granularity Gate

| 判断项 | 结论 |
|---|---|
| 为什么不是 single feature | 身份／凭证、一次性动作 token、外部邮件、access/refresh 会话、前端状态机、恢复与限速、旧账号迁移构成跨模块协议和依赖顺序；一次 feature 会把核心迁移与上线硬化混成不可审查的大改动 |
| 为什么不是 brainstorm | 5 个 owner checkpoint 已确认产品模型、首期身份、验证准入、会话模型、安全范围与旧账号迁移；成功标准和明确不做均可证伪 |
| roadmap 边界 | 只覆盖邮箱首期与公开上线安全闭环；手机号、设备中心、换邮箱、账号删除和审计中心明确延后 |
| 最小闭环 | 第 1 条 `email-account-access` 完成后，新用户可“注册→收信→验证→自动建立会话→访问 `/me`→刷新→退出”，旧 seed 账号可原地认领后用邮箱登录且全部 CRM 数据仍归原 `account_id` |
| 粒度效率约束 | 全 roadmap 只拆 2 条全栈 feature；不按表、端点、前后端或测试层机械细拆。第 2 条只因公开安全完成信号与第 1 条存在清晰依赖、独立风险和独立验收价值而单列 |

## 3. 模块拆分（概设）

```text
self-service-account-system
├── account-auth      深模块：账号生命周期、登录身份、密码凭证、一次性动作与会话安全
├── auth-http         Gin/OpenAPI 薄适配：JSON、cookie、Origin、错误封套与 AccountContext 注入
├── auth-mail         真外部边界：验证／重置邮件投递 adapter；模板语义由 account-auth 拥有
├── auth-ops          一次性旧账号认领、迁移检查与生产预检命令
└── webapp-auth       React 认证状态机、公开页面、路由守卫与 API client 会话恢复
```

### account-auth · 账号与认证深模块

- **职责**：承载账号生命周期、可扩展登录身份、密码凭证、邮箱验证／密码重置 token、access token 签发解析、refresh session 轮换／撤销／重放检测，以及认证入口的限速决策。对外只暴露 4.2 的应用操作；不感知 Gin、React 或邮件供应商 SDK。
- **承载的子 feature**：email-account-access、public-auth-hardening。
- **触碰的现有代码／模块**：`backend/internal/platform/auth`、`backend/internal/platform/store` 的账号认证仓储面、数据库 migrations、server composition root；现有 `AccountContext` 保留。
- **Depth 判断**：deep。调用方只表达“注册／验证／登录／刷新／恢复”等意图，token 哈希、轮换事务、身份规范化和状态不变量全部藏在模块内；删掉该模块会把安全规则散回 HTTP、CLI、邮件 worker 和测试。

### auth-http · HTTP 与浏览器凭证适配

- **职责**：忠实实现 4.4 HTTP 契约；解析 JSON／Bearer／refresh cookie／Origin，设置或清除 cookie，把 `account-auth` 结果映射成统一错误封套，并继续通过既有 auth middleware 注入 `AccountContext`。不包含密码、token 或账号状态领域判断。
- **承载的子 feature**：email-account-access、public-auth-hardening。
- **触碰的现有代码／模块**：`api/openapi.yaml`、codegen 产物、`backend/internal/platform/httpapi`、router 与 auth middleware。
- **Depth 判断**：刻意保持 shallow adapter；它的价值是隔离 Gin／HTTP 语义，不再抽一层 pass-through service。

### auth-mail · 认证邮件投递 adapter

- **职责**：实现 `AuthMailSender` 真外部 adapter，把 account-auth 已决定的 verification／reset 语义交给事务邮件服务；返回可观测 delivery receipt。它不决定 token 生命周期、账号状态或邮件是否应该发送。
- **承载的子 feature**：email-account-access（验证／认领邮件）、public-auth-hardening（重置邮件）。
- **触碰的现有代码／模块**：新增基础设施 adapter、config 与 server composition；不进入 customer/reminder 的 Telegram 触达域。
- **Depth 判断**：真实 seam，不是独立业务域。邮件供应商是 true external，production adapter 与 deterministic test adapter 都穿过同一 port；不为单个供应商再造一层转发接口。

### auth-ops · 认证迁移与运维命令

- **职责**：提供一次性、失败安全的旧 seed 账号认领、全新零账号部署的首账号 bootstrap、dry-run、迁移完成检查与生产 preflight；只调用 account-auth 的受控应用接口，不直接拼 SQL 绕过状态机。
- **承载的子 feature**：email-account-access（认领与 bootstrap 命令）、public-auth-hardening（最终 preflight／回滚演练）。
- **触碰的现有代码／模块**：新增 `backend/cmd/accountctl`（或同等单一运维二进制）、生产预检脚本、README／部署配置。
- **Depth 判断**：薄操作入口但并非假 seam；可信本地 operator 与公开 HTTP 的权限边界不同，删除它会迫使系统暴露危险的公开 legacy-claim API 或人工改库。

### webapp-auth · Web 认证体验

- **职责**：公开注册／验证／登录／找回页面，内存 access token、启动时 refresh 恢复、401 单次刷新重放、路由守卫与退出。只消费 4.4 HTTP API；不解析 JWT、不读取 refresh cookie、不保存认证 token 到 Web Storage。
- **承载的子 feature**：email-account-access、public-auth-hardening。
- **触碰的现有代码／模块**：`frontend/src/auth`、API client、`App.tsx`、现有 LoginPage／WelcomePage、新增注册／验证／找回／重置／改密页面。
- **Depth 判断**：认证状态机是 deep 前端模块；页面只消费 `anonymous | restoring | authenticated` 与操作结果，避免每个页面各写一次 401／refresh／跳转逻辑。

## 4. 模块间接口契约／共享协议（架构层详设）

本节是两个子 feature 的硬约束。内部文件拆分、SQL 写法和具体事务邮件供应商由 feature design 决定，但不得改变这里定义的状态、HTTP shape、安全不变量和模块 seam；要改先回 `cs-epic` planning。

### 4.1 账号、身份、凭证与认证状态

最终逻辑模型如下；列名可在 feature design 中做不改变语义的调整，约束和状态不可弱化：

```text
accounts
  id                  text primary key                 # 稳定租户键，现有业务表继续引用
  status              pending_verification | active | legacy_unclaimed
  activated_at        timestamptz nullable
  created_at          timestamptz not null

account_identities
  id                  text primary key
  account_id          text not null references accounts(id)
  kind                email                            # 首期唯一允许值；phone 为后续扩展
  normalized_value    text not null
  verified_at         timestamptz nullable
  created_at          timestamptz not null
  unique(kind, normalized_value)
  unique(account_id, kind)                             # 首期每账号每种身份最多一个

password_credentials
  account_id          text primary key references accounts(id)
  password_hash       text not null                    # bcrypt；永不出现在 API／日志
  updated_at          timestamptz not null

auth_action_tokens
  id                  text primary key                 # wire token 的 selector
  account_id          text not null references accounts(id)
  identity_id         text nullable references account_identities(id)
  purpose             email_verification | password_reset | legacy_claim
  secret_hash         text not null                    # 只存高熵 secret 的摘要
  expires_at          timestamptz not null
  consumed_at         timestamptz nullable
  invalidated_at      timestamptz nullable
  created_at          timestamptz not null

auth_refresh_sessions
  id                  text primary key                 # 单次 refresh generation
  family_id           text not null                    # 一次设备登录的 session family
  parent_id           text nullable
  account_id          text not null references accounts(id)
  token_hash          text not null                    # raw refresh token 永不入库／日志
  expires_at          timestamptz not null
  rotated_at          timestamptz nullable
  revoked_at          timestamptz nullable
  revoke_reason       text nullable
  replay_ciphertext   bytea nullable                    # 仅 grace 内保存 successor refresh 的 AEAD 密文
  replay_until        timestamptz nullable
  created_at          timestamptz not null
  last_seen_at        timestamptz nullable
```

状态不变量：

- 新公开注册原子创建 `accounts(pending_verification)`、email identity（未验证）与 password credential；账号 ID 由服务端生成。
- 只有有效、未消费的 `email_verification` 或 `legacy_claim` token 能在事务中把 identity 标记 verified、把账号置 active 并写 `activated_at`。
- 只有 active 账号可以登录、签发／刷新 session 或构造 HTTP `AccountContext`；`pending_verification` 与 `legacy_unclaimed` 不能访问 CRM 业务 API。
- `AccountScope` 与所有业务表的 `account_id` 不改变；客户端永不提交 `account_id`。
- `AccountScopes()` 等后台账号枚举只返回 active 账号；未验证／未认领期间不运行提醒、头像维护或摘要任务。
- 邮箱规范化固定为 trim + Unicode-independent ASCII lowercase；首期只接受语法合法且总长不超过 254 字节的 ASCII email，不做 Gmail 点号／plus-address 特殊折叠。
- 密码不 trim，首期要求 12–72 UTF-8 字节；错误响应不得回显密码规则之外的秘密信息。现有 bcrypt hash 复制到 `password_credentials` 后仍可直接验证。
- 旧 `accounts.password_hash` 在本 epic 内保留为 deprecated／nullable 回滚兼容列，新账号不写该列；其删除另行规划，避免把破坏性 schema cleanup 混入认证切换。

**Interface 设计检查**：账号与登录身份从首期就分表，而不是把 email 直接焊在 accounts 上。Owner 已确认手机号是后续真实需求；identity 表让 phone 以后通过新增 `kind` 和验证器接入，而 `account_id`、业务数据和 session 不迁移。该 shared state 归 account-auth 所有，其他模块不得直接写。

### 4.2 account-auth 应用接口

HTTP handler、auth-ops 与测试只通过下列语义入口调用认证模块；它们不依赖 Gin 类型：

```go
type ClientMeta struct {
    SourceIP     netip.Addr // 只在 account-auth 内派生 HMAC digest；持久层不存原始 IP
    UserAgent    string
}

type PublicDispatchStatus string // verification_required | accepted

type DeliveryOutcome struct {
    Attempted         bool
    Accepted          bool
    ProviderMessageID string
    FailureClass      string
}

type VerificationDispatch struct {
    PublicStatus PublicDispatchStatus
    Delivery     DeliveryOutcome
}

type ResetDispatch struct {
    PublicStatus PublicDispatchStatus
    Delivery     DeliveryOutcome
}

type LegacyClaimState string // ready | pending_same_email | already_claimed | conflict | no_target

type LegacyClaimResult struct {
    DryRun            bool
    State             LegacyClaimState
    AccountIDRedacted string
    EmailRedacted     string
    LegacyCount       int
    PendingClaimCount int
    Delivery          DeliveryOutcome
}

type LegacyAuthState struct {
    LegacyUnclaimedCount int
    PendingClaimCount    int
    ActiveClaimedCount   int
}

type SessionGrant struct {
    AccountID       string
    AccessToken     string
    AccessExpiresIn time.Duration
    RefreshToken    string // 只交给 HTTP adapter 写 HttpOnly cookie；不得序列化进 JSON／日志
    RefreshExpiresAt time.Time
}

Register(ctx context.Context, email, password string, meta ClientMeta) (VerificationDispatch, error)
ResendVerification(ctx context.Context, email string, meta ClientMeta) (VerificationDispatch, error)
VerifyEmail(ctx context.Context, rawActionToken string, meta ClientMeta) (SessionGrant, error)
Login(ctx context.Context, email, password string, meta ClientMeta) (SessionGrant, error)
Refresh(ctx context.Context, rawRefreshToken string, meta ClientMeta) (SessionGrant, error)
Logout(ctx context.Context, rawRefreshToken string) error

BeginPasswordReset(ctx context.Context, email string, meta ClientMeta) (ResetDispatch, error)
ResetPassword(ctx context.Context, rawActionToken, newPassword string, meta ClientMeta) error
ChangePassword(ctx context.Context, account AccountContext, currentPassword, newPassword string, meta ClientMeta) error

BeginLegacyClaim(ctx context.Context, email string, dryRun bool) (LegacyClaimResult, error)
InspectLegacyState(ctx context.Context) (LegacyAuthState, error)
```

`DeliveryOutcome` 与 `LegacyClaimResult` 仅供服务端日志、测试和可信 CLI 使用，不得直接序列化进公开 202 响应；`FailureClass` 只使用固定枚举，不携带 provider body、完整邮箱或 secret。跨数据库、HTTP 路由、旧 JWT 与启动配置的 `LegacyCutoverReport` 属于 auth-ops，不属于 account-auth；其契约见 4.8。

共同语义：

- Register／ResendVerification／BeginPasswordReset 对“账号不存在、已存在、状态不匹配”返回相同 public outcome；是否实际发信只在服务端决定，防止邮箱枚举。
- 邮件调用失败时账号／token 保持安全可重试状态并写结构化 delivery outcome；公开 Register／ResendVerification／BeginPasswordReset 仍返回与不存在邮箱相同的 generic accepted outcome，避免以投递成功／失败形成账号枚举侧信道。用户可从 resend／forgot 重新触发；受信 legacy-claim CLI 可以显式返回投递失败。
- VerifyEmail 消费 token、激活账号并创建首个 session 是一个有明确 transaction owner 的流程；重复消费返回同一 generic invalid/expired 结果，不二次创建 session。
- Login 只在密码校验成功后区分 `pending_verification`，返回 `email_verification_required`；不存在账号与错误密码统一为 `unauthorized`，且对不存在账号执行同成本 dummy hash 校验（见 4.7），不留下 timing 枚举侧信道。
- ResetPassword／ChangePassword 成功后在同一事务语义中更新 bcrypt hash、失效同 purpose token、撤销该账号全部 refresh session；HTTP 清除当前 refresh cookie。已签发 access JWT 最多继续存活到 10 分钟 TTL，不引入每请求 blacklist 查询。
- `ClientMeta` 由 account-auth 在 password reset/change 内派生 source digest并调用统一 limiter；HTTP 不直接访问 limiter store。Reset 使用 action token selector 作为 subject，Change 使用 `AccountContext.AccountID` 的 canonical bytes，均不持久化 raw subject。
- Logout 幂等：缺 cookie、已撤销或已过期均视为完成；绝不把 refresh token 放进错误或日志。
- BeginLegacyClaim 只允许可信本地命令调用，只认领 `legacy_unclaimed` 账号；相同 email 的 pending 重试幂等，不允许另一个公开账号抢占同一 identity。

**Interface 设计检查**：winner 是少量“用户意图”入口，而不是向 handler 暴露 IdentityRepository、TokenRepository、SessionRepository 让其自行编排。seam 位于 account-auth application 边界；内部 PostgreSQL 是 local-substitutable，测试穿过同一入口使用 Testcontainers。auth-ops 复用该 seam，不直接改表。

### 4.3 一次性动作 token 与邮件投递 port

Action token wire shape 固定为不可解释的 `<selector>.<32-byte-random-secret>`；数据库以 selector 定位行，只比较 secret 摘要。不同 purpose 不可互换，消费使用行锁或等价原子更新保证单次性。

默认生命周期：

| Purpose | TTL | 重发／重置行为 |
|---|---:|---|
| email_verification | 24 小时 | 新 token 生成时失效该 identity 旧 verification token |
| legacy_claim | 24 小时 | 仅 operator 可重发；相同 email 幂等，换 email 必须显式 replace 并审计 |
| password_reset | 30 分钟 | 新 token 生成时失效该账号旧 reset token；成功改密后全部 reset token 失效 |

外部邮件 port：

```go
type AuthMailKind string // email_verification | legacy_claim | password_reset

type AuthMail struct {
    Kind       AuthMailKind
    Recipient  string
    ActionURL  string
    ExpiresAt  time.Time
}

type DeliveryReceipt struct {
    ProviderMessageID string
    AcceptedAt        time.Time
}

type AuthMailSender interface {
    Send(ctx context.Context, mail AuthMail) (DeliveryReceipt, error)
}
```

- account-auth 拥有邮件用途、动作 URL 与发送时机；adapter 只负责投递。
- production 必须配置真实事务邮件 adapter；测试使用 deterministic fake，开发可使用明确标记的 mail sink。fake／sink 不得成为生产默认值。
- production adapter 必须配置显式超时与有限重试预算；超时或失败一律按投递失败处理——账号与 token 保持安全可重试状态、记录结构化 delivery outcome、对外仍返回 generic 202，handler 不得无限等待 provider。
- `PUBLIC_BASE_URL` 是 action URL 唯一 origin；token 只放在验证／重置页面 URL fragment（例如 `/verify-email#token=...`，fragment 不发送给 HTTP server），前端首次读取后立刻从 address bar/history 移除，再通过 POST body 消费；页面统一设置 `Referrer-Policy: no-referrer`。
- 首期采用“提交安全状态后同步调用邮件 port + 结构化 delivery outcome + generic public 202 + resend 恢复”，不引入存储 bearer secret 明文的 outbox。真实 provider 未接受投递时不得记录为 sent；若未来需要自动重试，必须先设计不在 outbox 明文持久化 action token 的方案。

**Interface 设计检查**：邮件供应商是 true external，port 放在 account-auth 与 provider adapter 之间；production/test 确有两种 adapter。port 隐藏供应商 SDK、错误与 message ID，但不把业务模板决策下放给基础设施。

### 4.4 最终 HTTP／OpenAPI 契约

所有端点位于 `/api/v1`。除明确标注 Bearer 的端点外均 `security: []`；所有非 2xx 响应继续使用现有 `ErrorEnvelope`。

```text
GET /auth/capabilities
  out: 200 { public_registration_enabled: boolean }
  note: 只暴露公开能力开关，不暴露内部配置、provider、secret 或账号状态；webapp 以它驱动注册入口

POST /auth/register
  in:  { email: string, password: string }
  out: 202 { status: "verification_required" }
       400 validation_failed | 429 rate_limited | 503 registration_disabled
  note: 邮箱已存在／状态不匹配／真实投递暂时失败仍返回相同 202 shape；不得据此枚举账号
        AUTH_PUBLIC_REGISTRATION_ENABLED=false 时，在解析／验证 body、访问账号存储、消耗 limiter 或调用邮件前，
        对任意 body 固定返回相同 503 ErrorEnvelope，不创建任何账号／identity／token

POST /auth/email/resend
  in:  { email: string }
  out: 202 { status: "verification_required" }
       400 validation_failed | 429 rate_limited

POST /auth/email/verify
  in:  { token: string }
  out: 200 AccessTokenResponse + Set-Cookie refresh
       400 invalid_or_expired_token | 429 rate_limited

POST /auth/login
  in:  { email: string, password: string }
  out: 200 AccessTokenResponse + Set-Cookie refresh
       400 validation_failed | 401 unauthorized
       403 email_verification_required | 429 rate_limited

POST /auth/refresh
  in:  HttpOnly refresh cookie; empty JSON/body
  out: 200 AccessTokenResponse + rotated Set-Cookie refresh
       401 unauthorized（同时清 cookie） | 403 forbidden（Origin 不可信）

POST /auth/logout
  in:  HttpOnly refresh cookie
  out: 204（幂等，同时清 cookie） | 403 forbidden（Origin 不可信）

POST /auth/password/forgot
  in:  { email: string }
  out: 202 { status: "accepted" }
       400 validation_failed | 429 rate_limited
  note: 邮箱不存在／账号状态不匹配／真实投递暂时失败仍返回相同 202 shape

POST /auth/password/reset
  in:  { token: string, new_password: string }
  out: 204（撤销该账号全部 refresh session 并清 cookie）
       400 validation_failed | 400 invalid_or_expired_token | 429 rate_limited

POST /auth/password/change                  [Bearer access JWT]
  in:  { current_password: string, new_password: string }
  out: 204（撤销该账号全部 refresh session 并清 cookie）
       400 validation_failed | 401 unauthorized | 429 rate_limited

GET /me                                     [Bearer access JWT]
  out: 200 { id, email, created_at, timezone }
       401 unauthorized

AccessTokenResponse:
  { access_token: string, token_type: "Bearer", expires_in: 600 }
```

新增公共错误码：

- `email_verification_required`（403）：只有密码已正确但邮箱未验证时返回；
- `forbidden`（403）：cookie-mutating endpoint 的 Origin／同站约束失败；
- `rate_limited`（429）：同时发送 `Retry-After`，不得暴露是 email 维度还是 source 维度触发；
- `invalid_or_expired_token`（400）：verification／reset／claim token 的不存在、purpose 错误、过期、已消费或已失效统一响应；
- `registration_disabled`（503）：仅表示 operator 当前未开放新账号创建；response 不包含开启时间、环境或 remediation 细节，不得因 email／body 内容变化；

refresh cookie 的生产不变量：名为 `__Host-crm_refresh`，`HttpOnly; Secure; SameSite=Strict; Path=/`，不设置 Domain；Max-Age 不超过 refresh absolute expiry。localhost 开发可以使用显式 dev cookie 配置，但生产 preflight 必须拒绝关闭 Secure／HttpOnly、宽 Domain 或非 Strict SameSite。

所有会设置、轮换或清除 refresh cookie 的 endpoint 必须验证浏览器 `Origin` 与 `PUBLIC_BASE_URL` 完全一致；`Origin` 缺失或不一致一律 fail closed 返回 403 `forbidden`，SameSite=Strict 只作纵深防御、不替代该校验。CORS 默认不开启。未来跨源／原生客户端需要独立扩展 token transport，不放宽本 epic 的 Web cookie 契约。

#### OpenAPI／codegen 分片所有权

| Operations | OpenAPI tag | Owner item |
|---|---|---|
| `getAuthCapabilities`、`register`、`resendVerification`、`verifyEmail`、`login`、`refresh`、`logout`、`getMe` | `email-account-access` | email-account-access |
| `forgotPassword`、`resetPassword`、`changePassword` | `public-auth-hardening` | public-auth-hardening |

- `api/openapi.yaml` 与 TypeScript schema 保持完整契约；Go server 继续按 `backend/oapi-codegen.yaml` 的 feature tag 切片，router 只手工暴露已实现 operation。
- 第 1 条只把 `email-account-access` 加入 Go `include-tags`；第 2 条完成对应 handler 时才加入 `public-auth-hardening`。不得继续使用会一次拉入两个切片的通用 `auth` tag。
- 第 1 条期间，第 2 条所属 endpoint 必须保持 404；不得生成待实现的 `ServerInterface` 方法，也不得用 stub／501 伪装已接入。
- 每条 feature 的 contract gate 都运行 `make generate-check`，并检查 router 暴露面与当前 tag owner 一致。
- 第 2 条完成后评估把两个 feature tag 收敛为单一 `auth` 领域 tag，并同步调整 include-tags 与 router 暴露面检查；收敛完成前不得回退使用通用 `auth` tag。

### 4.5 Access JWT 与 refresh rotation 协议

Access JWT 默认 TTL 10 分钟，claims 至少包含：

```text
ver = 2
iss = configured issuer
aud = crm-api
sub = account_id
sid = refresh family_id
jti = unique token id
iat, exp
```

- middleware 只接受固定算法、issuer、audience、`ver=2` 和完整 claims；旧版仅有 `sub/iat/exp` 的 30 天 JWT 在切换时立即失效。
- access token 只保存在前端内存；不得写入 localStorage、sessionStorage、IndexedDB、URL 或日志。
- refresh token 是至少 32 字节 CSPRNG bearer secret；数据库只保存摘要。默认 idle TTL 14 天、family absolute TTL 30 天，每次轮换的 expiry 不得超过 absolute deadline。
- Refresh 在数据库事务中锁定当前 generation：有效且未旋转 → 标记 rotated 并创建唯一 child；已撤销／过期 → 401。
- 同一 refresh token 在 10 秒 bounded concurrency grace 内因多 tab／丢失响应重试时返回同一 successor：首次 rotation 用从 auth root secret 按用途派生的 AEAD key 把 successor raw token 加密到 parent row 的 `replay_ciphertext/replay_until`；grace 内只解密重放同一 successor，grace 后清除密文。密文、nonce 与 key rotation 行为必须有 deterministic test，raw bearer secret 不得进入日志或普通明文列。
- grace 之外再次使用已旋转 token视为 reuse，原子撤销该 family 全部 generation，清 cookie 并返回 401；结构化安全日志只记 family/session ID 与原因，不记 token、密码或完整 email。
- 前端仍必须以 single-flight 协调同 tab refresh，并用浏览器跨 tab 协调能力减少并发；服务端 bounded replay 是网络丢包与竞态的最终兜底，不能只靠前端。

**Interface 设计检查**：access JWT 给 API caller 低成本短期授权，refresh session 把长期状态、撤销和重放复杂度集中在 account-auth。dependency strategy 为 PostgreSQL local-substitutable；middleware 不为每个业务请求查 refresh 表，接受最多 10 分钟 access 风险窗。

### 4.6 前端认证状态与请求协议

`webapp-auth` 对页面暴露：

```ts
type AuthState =
  | { status: 'anonymous' }
  | { status: 'restoring' }
  | { status: 'authenticated'; account: Me; accessToken: string; expiresAt: number }

type AuthActions = {
  register(email: string, password: string): Promise<'verification_required'>
  resendVerification(email: string): Promise<'verification_required'>
  verifyEmail(token: string): Promise<void>
  login(email: string, password: string): Promise<void>
  logout(): Promise<void>
  forgotPassword(email: string): Promise<'accepted'>
  resetPassword(token: string, newPassword: string): Promise<void>
  changePassword(currentPassword: string, newPassword: string): Promise<void>
}
```

- App 启动先进入 `restoring` 并调用 `/auth/refresh`；成功后再取 `/me`，失败清本地内存进入 anonymous。恢复完成前受保护页面不闪现、不冒充 anonymous。
- API client 用内存 access token 设置 Bearer。收到明确的 auth middleware 401 时触发全局 single-flight refresh，成功后只重放原请求一次；网络错误、429、403、5xx 和已经重放过的请求不得自动循环。
- token transport 迁移必须覆盖 `frontend/src/api/client.ts` 的普通 JSON、avatar/media、export 与 media JSON 全部请求路径，并保留 `customerAvatarMedia.ts` 基于 auth generation 失效缓存的语义；不得只替换主 JSON helper 后宣称已移除 localStorage token。每条路径都要有 401 refresh／单次重放与 storage inspection 证据。
- 401 发生在 handler 前，重放非幂等业务请求不会出现“业务已执行又重放”；仍保留现有 Idempotency-Key 契约，不把认证刷新当业务幂等替代品。
- logout 即使服务端不可达也清空内存并进入 anonymous；服务端可达时撤销当前 refresh family。改密／重置成功后同样回 anonymous，要求重新登录。
- 登录返回 `email_verification_required` 时，UI 在同页提供重发验证邮件入口（`resendVerification`），不要求用户重新注册。
- `/` 未登录继续显示 WelcomePage；新增 `/register`、`/verify-email`、`/forgot-password`、`/reset-password`；`/login` 改为 email + password。已认证访问这些公开 auth 页面时跳转 `/dashboard`。
- token fragment 首次读取后立即 `history.replaceState` 移除；错误 UI 不能回显 token 或判断邮箱是否已注册。

### 4.7 限速与防枚举协议

限速必须经统一 port，production 使用 PostgreSQL 持久化实现，确保进程重启或多实例不清零；测试使用 deterministic clock adapter：

```go
type AuthAction string // register | resend_verification | login | forgot_password | verify_token | reset_token | change_password

type AttemptLimiter interface {
    Consume(ctx context.Context, action AuthAction, subjectDigest, sourceDigest string, now time.Time) (RetryAfter time.Duration, allowed bool, err error)
    ResetSubject(ctx context.Context, action AuthAction, subjectDigest string) error
}
```

默认预算：

| Action | subject 预算 | source 预算 | window |
|---|---:|---:|---:|
| login／change password attempt | 5 次 | 30 次 | 15 分钟 |
| register | 3 次 | 20 次 | 1 小时 |
| resend verification | 3 次 | 20 次 | 1 小时 |
| forgot password | 3 次 | 20 次 | 1 小时 |
| verify／reset token attempt | 10 次 | 30 次 | 15 分钟 |

- subjectDigest／sourceDigest 使用从现有 auth root secret 按用途派生的 HMAC key；数据库不保存完整邮箱或原始 IP。可信代理来源必须来自显式 allowlist，不能无条件信任 `X-Forwarded-For`。
- 邮箱不做点号／plus-address 折叠（§4.1），register／resend／forgot 的 subject 预算可被地址变体稀释，只作弱信号；真实防滥用闸门是 source 预算与邮件发送成本。该残余已接受，首期不引入地址折叠。
- 所有 action 在业务动作前原子 `Consume`，因此成功与失败都消耗 source budget；source 不因成功重置。成功 login 重置该 subject 的 login bucket，成功 change 重置该 account subject 的 change bucket；成功 verify/reset 的 selector 已单次消费，不执行 bucket reset。失败原因（不存在、错密码）对外统一 401，内部结构化计数可区分但不得记录密码或完整邮箱。
- login／register／resend／forgot 对“账号不存在”与“账号存在”保持相同状态码、response shape 与响应时间预算：login 对不存在账号必须以预置 dummy bcrypt hash 执行同成本校验，邮件类入口在“真实发信”与“不发信”两条路径上保持可比工作量；timing 阈值是 public-auth-hardening security tests 的硬验收项，不是可选项。
- `Retry-After` 向上取整到秒；错误正文不暴露触发维度或剩余次数。

### 4.8 旧 seed 账号原地认领与切换协议

数据库 migration 对升级前已有账号执行：

1. 保留原 `accounts.id`；把原 bcrypt hash 复制到 `password_credentials`；
2. 标记 `accounts.status=legacy_unclaimed`，不创建伪邮箱、不标记 verified；
3. 业务表、头像 object key、后台 checkpoint、Settings 与导出数据完全不改 `account_id`；
4. 新空库允许零账号启动，不再运行 `EnsureDefaultAccount`，`SEED_ADMIN_PASSWORD` 在新配置／生产预检中必须缺失或为空；全新部署的首账号经 `accountctl auth bootstrap` 创建（见本节末）。

受信 operator 命令：

```text
accountctl auth claim-legacy --email <owner-email> [--dry-run]
```

- `--dry-run` 输出 legacy_unclaimed 数量、目标 account ID 的脱敏表示、邮箱是否冲突和将执行的步骤，不写库、不发信。
- 正式执行要求恰好一个可认领目标；创建／绑定未验证 email identity，生成 `legacy_claim` token 并经真实 AuthMailSender 发信。数据库或投递失败均返回非零退出码与可重试 remediation。
- owner 点击链接后复用 `/auth/email/verify` 消费 claim token，原账号置 active、创建 session；`GET /me` 返回同一 account ID，全部 CRM 资源计数和头像读取与认领前一致。
- account-auth 的 `InspectLegacyState` 只报告数据库内的 `legacy_unclaimed`、pending claim 与 active claimed 数量；它不探测 HTTP、OpenAPI、旧二进制或环境变量。
- auth-ops 的 production preflight 组合该数据库报告、route／OpenAPI probe、旧 JWT negative request 与启动配置检查，生成下列运维报告并在任一检查失败时非零退出：

```go
type CutoverCheckStatus string // passed | failed

type CutoverCheck struct {
    Name     string
    Status   CutoverCheckStatus
    Evidence string
}

type LegacyCutoverReport struct {
    Ready  bool
    Checks []CutoverCheck
}
```

- `LegacyCutoverReport` 必须证明：`legacy_unclaimed=0`、不存在未验证 claim、旧 password-only 路由不存在、旧 JWT 被拒绝、`SEED_ADMIN_PASSWORD` 不再参与启动。`Evidence` 只保存脱敏摘要／检查名，不保存 token、密码、完整邮箱或环境变量值。
- 认领是认证元数据增量，不宣称修改／删除历史备份；回滚时保留原 deprecated password hash，使旧二进制在明确的 operator 回滚流程中仍能恢复 owner 访问。已完成公开注册的新账号在旧二进制中不可用，这是回滚报告必须显式说明的降级边界。

全新零账号部署的首账号由可信 CLI 创建，不依赖公开注册开关：

```text
accountctl auth bootstrap --email <owner-email> [--dry-run]
```

- 仅当库中零账号时可执行；已有任何账号时固定拒绝，提示走公开注册或 legacy claim。
- 复用 account-auth 的 `Register` 应用入口（可信 CLI 不受公开注册闸门与公开 limiter 约束），创建 `pending_verification` 账号并触发验证邮件；owner 复用 `/auth/email/verify` 完成激活，不引入新 token purpose。
- 密码经交互式 prompt 或环境变量注入，不出现在命令行参数、shell 历史或日志中。
- `--dry-run` 输出将执行的步骤，不写库、不发信；数据库或投递失败返回非零退出码且可安全重试。

### 4.9 AccountContext／AccountScope 不变量

既有 contract 保持：

```go
accountID, err := AccessTokenVerifier.Verify(bearerToken)
ctx := auth.WithAccountContext(request.Context(), auth.AccountContext{AccountID: accountID})
scope := store.ScopeFor(auth.AccountContext{AccountID: accountID})
```

- 只有 auth middleware 可从已验证 access JWT 构造 `AccountContext`；注册、登录、refresh、公开 token 端点不能自行构造业务 `AccountScope`。
- service／repository 不 import Gin；业务查询继续只能经 AccountScope，客户端永不传 account_id。
- 所有既有双账号隔离测试必须在两个自助注册并验证的账号上重跑；账号 A 的 token 对账号 B 的客户、订单、档期、提醒、头像、设置和导出均不可见。

### 4.10 配置、密钥与公开注册发布闸门

共享配置契约：

| 配置 | 语义与硬约束 |
|---|---|
| `AUTH_TOKEN_SECRET` | auth root secret；production 必填且满足现有强度 preflight，不直接用于多个用途 |
| `AUTH_TOKEN_ISSUER` | access JWT 的固定 issuer；签发与校验必须一致 |
| `PUBLIC_BASE_URL` | 邮件 action URL 与 cookie-mutating endpoint Origin 校验的唯一公开 origin |
| `AUTH_PUBLIC_REGISTRATION_ENABLED` | 默认 `false`；公开注册的显式发布开关，不影响受信 legacy claim／bootstrap CLI |
| `TRUSTED_PROXY_CIDRS` | 可为空的可信反向代理 allowlist；仅来自该范围时才读取 forwarded source IP |
| `AUTH_MAIL_DRIVER` | production 必须指向真实 adapter；`fake`／`sink` 只允许测试或显式开发环境 |

- 从 `AUTH_TOKEN_SECRET` 以固定 label 派生相互隔离的 `access-signing`、`refresh-replay-aead`、`limiter-hmac` 子密钥；代码和运维文档必须固定 label 与派生版本，禁止复制同一 raw key 到三个用途。
- root secret 轮换是全会话失效事件：先关闭公开注册并记录 maintenance window，撤销全部 refresh session，再切换签发／校验与派生子密钥，验证旧 access／refresh 均失败，最后按 preflight 恢复入口。失败恢复顺序与旧 key 是否可临时回装必须在 runbook 中明确；不得以同时长期接受两套 refresh replay key 的方式静默降级。
- backend 是公开注册开关的最终权威：`GET /auth/capabilities` 与 `POST /auth/register` 必须读取同一运行时配置快照并返回 `Cache-Control: no-store`。开关为 false 时，register 在任何 body／email 下都固定返回 `503 registration_disabled`，且账号、identity、action token、limiter 和 mail adapter 均无副作用；开关为 true 时才进入 4.4 的正常注册契约。
- webapp 启动和进入 `/register` 前读取 capabilities；结果未知或请求失败时 fail closed，不展示可提交表单。false 时 WelcomePage 注册 CTA 显示“注册暂未开放”并保持 disabled，直接访问 `/register` 显示同一不可用状态且保留返回登录／欢迎页入口；true 时才启用 CTA 与表单。UI 不能用独立 build-time boolean 覆盖 backend 判定。
- 该开关只控制“创建新账号”：`resendVerification`、`verifyEmail`、`login`、`refresh`、`logout`、`getMe`、第 2 条密码流程和可信 legacy claim／bootstrap CLI 继续按各自契约工作，保证已有 pending／active／legacy owner 不因暂停新注册而失去恢复入口。
- `email-account-access` 可以独立完成 design、implementation、review、QA 与 acceptance，但它只是测试／封闭环境的中间闭环。该条的测试与封闭验收可显式设置 `AUTH_PUBLIC_REGISTRATION_ENABLED=true`；其 production preflight 必须在该值为 true 时失败，并返回固定 remediation `public-auth-hardening-not-complete`。
- `public-auth-hardening` acceptance 才把 production preflight 更新为：仅当邮件、密钥、issuer、public base URL、proxy、cookie、限速、legacy cutover 与安全回归全部通过时允许公开注册开关为 true。即使 preflight 通过，真实部署、owner 邮箱绑定、provider 凭证配置和生产 cutover 仍需 owner 单独授权。
- 公开注册形态已由 Owner 拍板为二元：不建设申请／邀请制、内测码等中间形态；item1 完成至 item2 验收之间生产注册保持关闭，item2 验收后直接全量开放，`GET /auth/capabilities` 因此只表达布尔开关。

### 4.11 生产认证可观测性最小契约

本 epic 不建设安全中心 UI，但必须输出固定的结构化事件：

- `auth.login`
- `auth.email_verified`
- `auth.rate_limited`
- `auth.refresh_reuse`
- `auth.mail_delivery`
- `auth.password_changed`
- `auth.legacy_claim`

事件只允许记录时间、结果、固定 failure class、脱敏 account/session/family ID、action、source digest、provider message ID，以及仅供 `auth.legacy_claim` 使用的布尔 `dry_run`；禁止记录完整 email／IP、密码、action／access／refresh token、cookie、Authorization header 或 provider response body。`auth.email_verified` 同时服务注册漏斗分析与“未收到验证邮件”类工单排查，不只是安全监测。

`public-auth-hardening` 必须交付 `accountctl auth monitor --cutover-mode=off|active`（默认 `off`），读取结构化 JSONL／journald 并按固定窗口非零告警：

- 任一 `auth.refresh_reuse`：high；
- 5 分钟内 `auth.rate_limited` 全局不少于 20 次，或同一 source digest 不少于 5 次：warning；
- `auth.mail_delivery` 连续失败不少于 5 次，或 15 分钟内样本不少于 10 且失败率大于 20%：warning；
- 任一非 dry-run `auth.legacy_claim` 失败：`cutover-mode=off` 为 warning，`cutover-mode=active` 为 high；dry-run 失败不触发本条。

acceptance 必须覆盖每个阈值的正／负边界、空输入／损坏行行为、退出码和敏感字段 redaction；该命令与日志契约属于第 2 条内部上线 checklist，不新增独立 observability feature。

monitor 对损坏行采用“隔离后继续扫描 + degraded”策略：不把原始行写到 stderr，只输出行号与固定 parse failure class；退出码 `0`=无告警且无损坏、`1`=有告警无损坏、`2`=无告警但有损坏、`3`=同时有告警与损坏。这样既不因单行损坏丢弃后续真实告警，也不静默忽略输入完整性问题。

## 5. 子 feature 清单

1. **email-account-access** — 交付邮箱注册、验证后准入、access/refresh 会话和全套 Web 接入，并让现有 seed 账号在保留原 account_id 与 CRM 数据的前提下完成一次性邮箱认领
   - 所属模块：account-auth、auth-http、auth-mail、auth-ops、webapp-auth
   - 依赖：无
   - 状态：planned
   - 对应 feature：未启动
   - 备注：这是一个粗粒度全栈闭环；Schema、邮件 adapter、OpenAPI/codegen、后端、前端、迁移与测试不得再各拆一条 feature。完成信号：新账号“注册→收信→验证→自动会话→`/me`→refresh rotation→退出”端到端通过；synthetic legacy fixture 经 `accountctl claim-legacy` 后 account ID、各业务 counts、头像读取均不变；全新空库经 `accountctl auth bootstrap` 创建首账号并完成邮箱验证；旧 password-only 登录与旧 JWT 在新运行态不可用；access token 不进入任何 Web Storage。内部执行只按 checklist waves 推进：schema／migration safety → identity／action／session 深模块 → mail／OpenAPI／HTTP → webapp 全请求路径 → legacy claim／preflight → E2E／全域隔离；这些 wave 不是子 feature。
   - 发布边界：本条可以独立设计、实现、复审、QA 和验收，但只形成测试／封闭环境闭环。`AUTH_PUBLIC_REGISTRATION_ENABLED` 默认 false；本条必须证明 false 时 capabilities／register／WelcomePage／`/register` 全部 fail closed 且无注册副作用；production preflight 对 true 固定失败为 `public-auth-hardening-not-complete`，直到第 2 条验收完成。

2. **public-auth-hardening** — 在邮箱账号访问闭环上补齐密码恢复／修改、全 refresh session 撤销、持久化限速／防枚举和公开上线安全回归
   - 所属模块：account-auth、auth-http、auth-mail、auth-ops、webapp-auth
   - 依赖：email-account-access；因为恢复、撤销与限速复用其 identity、action token、refresh family、邮件 port 和前端 AuthState 契约
   - 状态：done
   - 对应 feature：未启动
   - 备注：同样保持全栈闭环，不另拆“找回密码”“限速”“可观测性”“安全测试”小 feature。完成信号：忘记／重置／改密全路径通过且撤销全部 refresh session；login/register/resend/forgot/token 预算边界与 `Retry-After` 可重复验证；账号枚举、rotation race/reuse、cookie/Origin、旧 JWT、跨账号数据与 migration/rollback case catalog 全绿；固定认证事件、monitor 阈值和 redaction 验收通过；production preflight 和 README 覆盖邮件、auth secret/issuer、PUBLIC_BASE_URL、proxy、公开注册闸门、SEED 退役与 cutover。

**最小闭环**：第 1 条 `email-account-access` 完成后，新用户和原 seed owner 都能通过已验证邮箱进入同一个基于 `AccountContext`／`AccountScope` 的 CRM；第 2 条把该闭环提升到允许公开上线的安全完成定义。

### Goal Coverage Matrix

| Goal／completion signal | Covered by item(s) | Verification entry | Evidence type | Core? |
|---|---|---|---|---|
| 新用户自助注册、收到验证邮件、验证后自动进入经营台 | email-account-access | Testcontainers API roundtrip + 浏览器 `/register → /verify-email → /dashboard` | test + API + screenshot | yes |
| 未验证账号不能取得 session 或访问任一 CRM 业务 API | email-account-access | pending account 对 `/auth/login` 与 representative protected endpoints 的状态矩阵 | test + API | yes |
| access 10m、refresh rotation／bounded replay／reuse revoke、当前设备退出 | email-account-access, public-auth-hardening | deterministic clock + 并发 refresh／丢响应／reuse integration matrix | test + evidence report | yes |
| access token 不进 Web Storage，刷新恢复与 401 单次重放无循环 | email-account-access | frontend auth client tests + 浏览器 reload／双 tab 检查 + storage inspection | test + screenshot | yes |
| 原 seed 账号认领后 account_id、业务数据和头像引用不变 | email-account-access | synthetic full fixture `accountctl --dry-run/claim` 前后 counts、ID、avatar checksum 比对 | command + test + evidence report | yes |
| 全新空库部署可创建并验证首账号，不依赖公开注册开关 | email-account-access | zero-account fixture：`accountctl auth bootstrap --dry-run`／正式执行 + 验证邮件 roundtrip + 非空库固定拒绝 | command + test | yes |
| 忘记／重置／修改密码可用且安全事件撤销全部 refresh session | public-auth-hardening | token 单次／过期／purpose 隔离 + 多 session 撤销矩阵 + UI 流程 | test + API + screenshot | yes |
| 公开认证入口限速且不泄露邮箱是否存在 | public-auth-hardening | 每 action subject/source 边界、Retry-After、响应 shape 与 timing budget（含 login 对不存在账号的 dummy-hash 同成本校验）检查 | test + security review | yes |
| 旧 password-only、旧 JWT、localStorage token 与 SEED 启动路径退役 | email-account-access, public-auth-hardening | route/OpenAPI grep、旧 token negative test、storage inspection、production preflight | test + command | yes |
| 现有账号隔离在真实多账号注册后仍成立 | email-account-access, public-auth-hardening | 两个 verified accounts 全域 cross-account regression + `make check` | test + command | yes |
| 第 1 条不能误开生产公开注册，第 2 条通过全部安全门禁后才可解锁 | email-account-access, public-auth-hardening | 两阶段 production preflight：`public-auth-hardening-not-complete` negative case + secure config/cutover positive case | command + acceptance report | yes |
| 公开注册开关为 false 时 API 与 UI 都 fail closed 且已有账号入口不受影响 | email-account-access | capabilities false + 任意 register body 固定 503／零副作用 + CTA／direct-route unavailable + login/resend/verify regression | API + test + screenshot | yes |
| 关键认证事件可监测、阈值可执行且日志无敏感字段 | public-auth-hardening | JSONL／journald fixture 的阈值正负边界、退出码与 redaction 检查 | command + test + evidence report | yes |
| 公开上线与回滚边界可由 operator 重放 | public-auth-hardening | synthetic migration／rollback case catalog、README/preflight dry-run | command + acceptance report | yes |

## 6. 排期思路与深度规划底稿

### 为什么只拆两条

第一条沿用户价值垂直切开：不做“纯 Schema foundation”或“纯后端 API”中间态，而是一次得到可演示的新账号访问闭环，并把旧账号认领放在同一条里，避免认证切换后 owner 无法访问已有数据。第二条只承载公开上线前必须独立审查的安全闭环；它依赖第一条稳定的 identity/session/token seam，完成信号与风险面也独立。继续细拆会让每条 design 重复协商相同 OpenAPI、token、邮件和前端状态；继续合并则会把核心迁移与安全 hardening 塞进一个无法有效 review 的超大 feature。

### 目标完成信号

本 epic 完成时，任意新摄影师可以在真实邮件 adapter 下完成注册、验证、登录、刷新、退出、找回和改密；未验证／错误凭证／滥用请求按统一安全契约失败。原 seed owner 认领后仍使用同一个 account ID，全部 CRM 数据与头像不迁移、不串号。旧 JWT、password-only 登录、`localStorage` token 和 seed 启动路径退役；两账号全域隔离、迁移／回滚、安全矩阵与 `make check` 全绿。

### Top 3 风险与缓解

1. **Refresh rotation 在并发／丢响应下误封或被重放**：4.5 先固定“grace 内同一 successor、grace 外 family revoke”的可观察语义；第一条必须有 deterministic clock、事务并发和双 tab 场景，第二条再做攻击矩阵与 residual review。
2. **邮件 token 与统一响应仍泄露账号或留下不可恢复 pending 状态**：action token 只存 hash、purpose 隔离、单次消费；公开注册／重发／找回在账号不存在、状态不匹配或真实 provider 投递失败时都返回同一 generic 202，真实 delivery outcome 只留在内部事件；resend／forgot 是正式恢复路径，不用 fake 伪装生产完成。
3. **旧 seed 账号切换导致数据孤儿或 owner 锁死**：保留原 account ID 和 deprecated hash，先 dry-run 再认领；synthetic full fixture 比对全域 counts／头像 checksum；新二进制只改认证元数据，回滚报告明确新账号在旧 binary 下不可用的边界。

### 非显然依赖

- 公开验证与找回需要真实事务邮件服务、发件域名／DNS、`PUBLIC_BASE_URL` 与可观测 message ID；供应商采购／凭证由 owner 另行授权，不能用测试 sink 代替上线证据。
- 当前 `v1-hardening` 仍有进行中产物，且生产 preflight／README 紧耦合 `AUTH_TOKEN_SECRET` 与 `SEED_ADMIN_PASSWORD`；实现前需基于其最终状态更新，而不是覆盖现有运维改动。
- 旧账号认领需要 owner 可接收的真实邮箱；实际绑定、生产 cutover 和 secret 轮换属于外部状态变更，必须另行授权。
- Access／refresh 双 token 仍使用现有 `github.com/golang-jwt/jwt/v5` access token 基座与 PostgreSQL 主存储；若 feature design 想更换算法／库，须回 roadmap／ADR，不作为顺手重写。

### 关键假设

- 首期唯一客户端是同源 Web SPA；未来移动端会复用 account/session 语义，但 token transport 另行设计。
- 单账号可同时有多个 refresh family；首期无 UI 管理它们，但改密／重置可以全部撤销。
- SMTP／HTTP 邮件供应商可通过 `AuthMailSender` 同步返回 accepted receipt；若真实 provider 只支持异步 webhook，本 roadmap 的 public HTTP shape不变，adapter 内处理 provider 差异。
- 当前业务表的 account_id 和对象 key 已足够稳定，认领只增认证元数据，不需要搬迁任何业务数据。

### 基线与验证入口

- 全量门禁：`make check`；后端 Testcontainers 串行口径继续使用 `go test -p=1 ./... -count=1 -parallel=1`。
- 契约门禁：`make generate-check`，OpenAPI → Go／TS 生成物零漂移。
- 定向后端：auth／httpapi／store／accountctl 包测试，覆盖 deterministic clock、真实 PostgreSQL transaction 与双账号隔离。
- 定向前端：新增 `test:auth` 脚本并纳入 Makefile；浏览器证据覆盖注册、验证、reload refresh、退出、找回、重置、改密和 375px 关键路径。
- 运维：production-preflight 与 synthetic legacy fixture migration／rollback case catalog；不在 roadmap 起草阶段运行完整门禁。

### 交付物落点

- `api/openapi.yaml` 与 Go／TS 生成物；
- `backend/internal/platform/auth` 深模块、认证 migrations／repository、httpapi adapter、真实 mail adapter、`backend/cmd/accountctl`；
- `frontend/src/auth` 状态机／client 与公开 auth 页面；
- config、production preflight、迁移／回滚脚本或 case catalog、README；
- 两条 feature 各自的 design／review／QA／acceptance 证据与 roadmap 状态回写。

### 知识回写点

- roadmap 经 owner 批准后、第一条 child feature 开始实现前，先通过 `cs-domain` 正式记录身份模型、access/refresh 会话与旧账号原地认领 ADR，并更新 `requirements/CONTEXT.md` 区分“Account 数据归属根”与“登录身份”；acceptance 只补充实现中发现的后果，不得把结构性决策拖到实现结束后；
- 邮件 provider、cookie／proxy、refresh concurrency 或 bcrypt 迁移若暴露长期坑，通过 `cs-keep` 沉淀；
- 生产必须知道的 auth 配置、测试命令或 preflight 陷阱用 `cs-note` 追加 attention；
- 公开 API 与用户操作变化由 acceptance 后提示 `cs-docs`／`cs-docs-neat` 同步。

## 7. 观察项

- `requirements/CONTEXT.md` 仍写“账号首版单账号运行”，现状上没有错；roadmap 获 owner 批准后、首条 child feature 实现前需由 `cs-domain` 更新为多账号产品化语义，并区分“账号（数据归属）”与“登录身份（email／phone）”。planning 阶段不直接改 requirements／ADR。
- ADR-001 明确预留产品化且说“开多账号只是放开注册”，方向兼容；但账号／登录身份分表、access/refresh session 和原地认领是新的结构性决策，roadmap 批准后应由 `cs-domain` 在实现前评估分别记录或合并 ADR。
- `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md` 的四项 residual（不限速、30 天 JWT、localStorage、无改密）只有在第 2 条 acceptance 全部给出证据后才能关闭，不因 roadmap 创建而视为已解决。
- 原 `photographer-private-crm` roadmap 把多账号注册列为明确不做；本 roadmap 是后续产品化 epic，不回写或重开已完成的 platform-skeleton feature。完成后再做文档索引整理。
- 事务邮件供应商尚未选择。它不改变 `AuthMailSender` 与 HTTP 契约，但真实采购、域名验证、secret 注入和送达证据是第一条实现／验收的外部 checkpoint。
- 实现前按 `.codestable/attention.md` 让 owner 选择当前 branch 或新 worktree；当前工作树已有欢迎页／登录页相关未提交改动，认证 feature 必须保留并基于这些事实继续，不能覆盖。
- 服务条款／隐私政策同意采集未纳入首期；公开放量前 owner 需按目标法域确认是否需要，若需要则 `accounts` 增加同意版本列——该决策在 ADR 固化阶段确认成本最低，拖到公开上线后回填更贵。
