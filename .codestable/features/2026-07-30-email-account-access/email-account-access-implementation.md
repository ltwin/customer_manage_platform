---
doc_type: feature-implementation
feature: 2026-07-30-email-account-access
status: completed
updated: 2026-07-31
---

# email-account-access 实现记录

## 基线预检

- `npm run test:api-client`：4/4 通过。
- `npm run build`：通过；仅保留既有 chunk size warning。
- `npm run test:prototype`：基线已有 1 项失败，断言仍要求 `CustomerNewPage` 直接调用 `listCustomers`，而当前代码已改由 `CustomerPicker` 承担；本 feature 未修改该范围，后续以“不新增失败”隔离。

## STEP-001 认证前端结构微重构

- Gate A：新增长期 `test:auth` characterization，并接入 `package.json` 与 `Makefile`；覆盖 password-only `/auth/login` body、`crm_token` localStorage、`/login` route 与成功导航。
- Gate B：仅把 `LoginPage.tsx`/CSS 移入 `pages/auth/` 并更新 import；`test:auth`、`test:api-client`、build 通过，prototype 保持同一项既有失败。
- Gate C：仅把 `ErrorEnvelope`、`ApiErrorDetails`、`ApiError` 与 JSON `request` 移入 `api/transport.ts`，由 `client.ts` re-export；未移动或去重 `apiErrorFromResponse`。
- 首次 Gate C 验证因 Node ESM 需要 `.ts` 扩展名失败；窄修复仅补齐 transport import/re-export 扩展名，随后 `test:auth` 2/2、`test:api-client` 4/4、build 全绿。
- TDD exception：本 step 是 approved design 指定的 characterization + 纯移动，无产品行为变化；替代证据为三个 gate 的相同公开请求、storage、route、navigation 与 build 结果。
- 清洁度：无 debug 输出、临时 TODO/FIXME、注释旧代码或方案外实现。

## STEP-002 Schema／migration safety

- RED：新增真实 PostgreSQL migration tests 后，因缺 `password_credentials` 与 `accounts.status` 分别以 `42P01`、`42703` 失败。
- GREEN：新增 `0012_account_auth` up/down，建立账号状态、email identity、password credential、action token、refresh family/generation；旧 `accounts.password_hash` 保留但允许新式账号为 null。
- 迁移不变量：旧账号自动标为 `legacy_unclaimed` 并复制 credential，原 account ID 与 customer 外键不变；重复 up 通过；email 全局唯一与 32-byte token hash constraint 已由真实 PG 证伪。
- Rollback：legacy-only 数据 down 成功并移除五张认证表；存在任一 null legacy hash 新式账号时以 `auth_schema_down_blocked_new_accounts` 固定 fail closed，不填造 hash、不删账号。
- VERIFY：`go test ./internal/platform/store -run 'TestAccountAuth(MigrationPreservesLegacyAccountAndRollsBack|DownMigrationRejectsNewStyleAccount)$' -count=1 -parallel=1` 通过。
- 清洁度：迁移与测试无凭证、临时 schema、debug 输出或方案外数据改写。

## STEP-003 Identity／action／session 深模块

- RED（编译兼容）：认证深模块替换旧接口后，`store/account.go` 的 `auth.AccountReader` 编译断言失败；移除失效断言，并让旧兼容账号创建在同一 transaction 内同步写入 `password_credentials` 后恢复编译。
- RED（协议单元）：新增 KDF／AEAD／Bearer／JWT／delivery／validation tests，先后证伪 deterministic JWT random seam 缺失、`iat`／单一 audience 未强制、delivery error 未暴露 provider-neutral interface、repository failure 未归一为 `internal`、非法 admission mode 与过大 sweep batch 未 fail closed。
- GREEN（密码与 token）：邮箱固定 trim + ASCII lowercase，密码按未 trim 的 12–72 UTF-8 bytes 校验；action／refresh wire 为 selector + 32-byte CSPRNG secret，数据库只存 SHA-256；access JWT 为 HS256、`ver=2`、固定 iss/aud 与 `sub/sid/jti/iat/exp`，TTL 10 分钟并拒绝旧 shape、未来 `iat`、缺 claim、附加 audience 和 synthetic root rotation。
- GREEN（KDF／AEAD）：HKDF-SHA-256 对两个版本化 label 各派生 32 bytes；AES-256-GCM envelope 固定 `0x01 | nonce[12] | ciphertext+tag`，AAD 使用 length-prefixed parent/family ID + UTC epoch seconds；单元测试覆盖 nonce 不复用、未知版本、短包、nonce/cipher/AAD tamper、wrong key 和 root rotation negative。
- GREEN（真实 PostgreSQL 生命周期）：覆盖 bootstrap dry-run 零账号／零 identity／零 token／零邮件，register generic duplicate、resend 替换旧 token且投递失败不回滚安全状态、24 小时 action 边界、pending/active login、purpose-neutral verification/legacy activation、`CurrentAccount` 与 active-only `AccountScopes`。
- GREEN（legacy 可恢复）：首次 legacy claim 投递失败后，同邮箱重试复用原 identity 并替换 action token；旧 token 失效、新 token 可激活，最终账号数量为 1 且原 account ID 完全保留。
- GREEN（refresh 状态机）：真实行锁覆盖首次 rotation、10 秒边界内同 successor、刚过窗口 reuse 整族撤销、successor post-revoke 失败、tamper/wrong replay key 同 transaction 撤销、无后续流量有界 sweep 物理清空、14 天 idle／30 天 absolute 不延长，以及 missing/valid/already-revoked/post-logout 语义。
- GREEN（admission barrier）：bootstrap-vs-bootstrap 并发只有一个赢家；public 持 production admission lock 时 bootstrap 在 `pg_stat_activity` 中等待并在 public commit 后得到 `bootstrap_not_empty`；bootstrap 已通过零账号检查并持锁时 public 等待，bootstrap commit 后 public 才继续。两端均通过 `Service.Register`，测试未直接拼账号创建 transaction。
- 兼容回归：新增 0012 后同步修正既有 avatar／Telegram migration tests 的固定 down-step 偏移，没有放宽 schema 断言。
- VERIFY：`go test ./internal/platform/auth ./internal/platform/store -count=1 -parallel=1` 通过；auth 与 store 均 0 失败，store 包含真实 PostgreSQL migration、并发与事务矩阵。
- 清洁度：测试失败信息不输出 access/refresh/action token；源码、SQL 与 evidence 未记录真实 root secret、完整真实邮箱、provider body、cookie 或 Authorization；无临时 debug／TODO／FIXME。

## STEP-004 Mail／OpenAPI／HTTP（provider-neutral 子阶段）

- OpenAPI／codegen：item1 的 capabilities、register、resend、verify、login、refresh、logout 与 `/me` 已使用 `email-account-access` tag 进入 Go server interface；item2 的 forgot/reset/change password 只进入 OpenAPI 与 TypeScript schema，未进入 Go interface、未挂载 route，HTTP 测试固定断言三条路径为 404。
- HTTP adapter：registration disabled 在解析 body、调用 application/store/mail 前固定返回 503；register/resend 对外保持 generic 202；verify/login/refresh/logout 在解析 body 或调用 application 前执行 exact Origin gate；cookie 固定 `__Host-crm_refresh`、HttpOnly、Secure、SameSite=Strict、Path=/、无 Domain，并覆盖过期／注销清除语义；`/verify-email` SPA fallback 固定 `Referrer-Policy: no-referrer`。
- Mail port／开发 sink：account-auth 只向 adapter 传 purpose、recipient、action URL 与 expiry；缺少 public base URL 时吸收为 `misconfigured`，不泄露裸 action token；显式 localhost sink 日志只含 fixed purpose 与 synthetic message ID，不含 recipient、action URL 或 bearer，且不作为 production 默认或真实 receipt evidence。
- 配置／composition root：`AUTH_PUBLIC_REGISTRATION_ENABLED` 默认 false，`AUTH_MAIL_DRIVER` 默认 `unavailable`；sink 仅允许 loopback public base，未知／真实但未实现的 driver fail closed；issuer、canonical public base URL 与 trusted proxy 配置由 composition root 注入。
- VERIFY：`go test -p=1 ./internal/platform/auth/... ./internal/platform/authmail/... ./internal/platform/store/... ./internal/platform/httpapi/... ./internal/platform/config/... ./cmd/server/... -count=1 -parallel=1` 共 6 个包通过，退出码 0；其中 `httpapi` 全包测试覆盖 item2 404、Origin 零副作用、cookie、capability、generic outcome 与 no-referrer。
- Codegen 幂等：重复执行 `make generate` 退出码 0，`api/openapi.yaml`、Go generated server 与 TypeScript schema 三个 SHA-256 前后完全一致；`git diff --check` 通过。
- 范围核验：Go generated server 中不存在 `ForgotPassword`／`ResetPassword`／`ChangePassword`；TypeScript schema 中存在 `forgotPassword`／`resetPassword`／`changePassword`；默认配置没有启用 fake/sink。
- TDD：HTTP、sink 与配置行为均由新增测试先行约束并在本子阶段 GREEN；真实 adapter 尚未选择，不能进入其 RED/GREEN loop。
- 清洁度：provider-neutral 代码与 evidence 不含 credential、完整真实邮箱、provider response body、Bearer token、cookie 或 Authorization；无 production test adapter 默认。
- 外部 checkpoint：`goal-state.yaml.external_checkpoints.mail_provider.status` 已置为 `awaiting-owner`。真实 adapter、bounded deadline、accepted receipt 与 redacted event 尚缺 owner 输入，因此 STEP-004 保持 `pending`，不进入 STEP-005。

### STEP-004 Resend 真实 Adapter 子阶段

- Context7／官方契约：Resend 使用 `POST https://api.resend.com/emails`、Bearer auth、JSON、显式 User-Agent；成功响应只消费 `id`，并使用 24 小时有效的 Idempotency-Key 防止 retry 重复投递。
- RED：`resend_test.go` 与 config tests 先因缺 `Resend`、`ResendAPIKey`、`AuthMailFrom` 和 `ErrResendAPIKeyMissing` 编译失败。
- GREEN：新增 Resend HTTPS adapter、config fail-fast 与 composition wiring；固定 4 秒 attempt timeout、5 秒 total deadline、最多 2 attempts，只有 transport／429／5xx retry；401／403、其他 4xx 与 temporary failure 分别映射为 typed class。
- 脱敏：accepted event 只记录固定事件字段、provider、purpose、status、provider message ID 和 accepted time；failure event 只记录固定 failure class。API key、完整 recipient、action URL、response body 与 bearer token 不进入日志或 evidence。
- 本地 VERIFY：`go test ./internal/platform/authmail ./internal/platform/config ./cmd/server -count=1` 通过，0 失败；覆盖成功 receipt、幂等键、日志脱敏、有限 retry、failure mapping、caller cancellation 与 whole-operation deadline。
- 测试稳定性窄修复：聚合验证暴露 `token_test.go` 的 claims inspection 使用真实墙钟，固定时间 token 在当天稍后会被判过期；只为 inspection parser 注入同一 deterministic clock，目标测试恢复通过，不改变生产 token 行为。
- 真实外部 VERIFY：受控请求返回 HTTP 403，typed class=`misconfigured`，未取得 provider message ID 或 accepted time。未保存 provider response body；脱敏证据见 `email-account-access-mail-checkpoint.md`。
- Checkpoint 结论：需要 owner-approved verified sender reference，或把 recipient reference 对齐为 Resend onboarding sender 允许的账号测试收件人。若涉及 domain／DNS，必须由 owner 在外部完成。STEP-004 继续保持 `pending`，不进入 STEP-005。

#### STEP-004 External Checkpoint Closure

- Owner 完成 permitted recipient alignment 并以固定 resume action 恢复；Goal 重新保护三条任务外 paths 后，只执行一次受控 live receipt。
- 最终结果：HTTP 2xx accepted；provider message ID=`f646ede0-b7c5-47cc-8d3c-db76bca67c41`，accepted at=`2026-07-31T04:09:32Z`。
- Live runner 同时核验 redacted `auth.mail_delivery` accepted event 存在，且不含 API key、完整 recipient、action URL 或 action bearer；provider response body 未保存。
- `goal-state.yaml.external_checkpoints.mail_provider.status` 已置为 `receipt-verified`；Resend adapter、bounded deadline、finite attempts、typed failure、accepted receipt 与 redacted event 的退出信号全部满足，STEP-004 标为 `done`。

## STEP-005 Webapp 全请求路径

- RED（认证 coordinator）：最终 `test:auth` 先因缺少 `RegisterPage.tsx` 真实失败；补齐首版后又以“较慢旧 generation 的 401 在首个 refresh 已结束后返回”用例证伪 naive single-flight，真实得到 refresh 2 次而期望 1 次。
- GREEN（内存 AuthState）：新增 `anonymous | restoring | authenticated` 稳定 snapshot，以 `useSyncExternalStore` 订阅；access token 只保留在模块内存。App reload 先 refresh，再带新 access 调 `/me`；普通路由在 restoring 结束前只显示恢复状态，受保护页面不闪现。
- GREEN（统一恢复）：JSON `request`、multipart avatar、avatar Blob 与 export 全部穿过 `authorizedFetch`。只有首个 auth 401 才进入 refresh；并发请求共享 flight，迟到的旧 generation 401 直接以当前 generation 重放；原请求最多重放一次，replayed 401 进入 anonymous，网络／403／429／5xx 不触发循环。
- GREEN（页面与 capability）：Login 改为 email/password 且无 phone/SMS UI；`/register` 自行读取 backend capability，loading／false／error 均不渲染可提交表单，true 才开放；Welcome 同样 fail closed，只有 true 才链到 `/register`；`/verify-email` 用 layout effect 首次消费 fragment 并在发请求前同步 `history.replaceState`。
- 认证缓存联动：头像媒体缓存 key 继续包含 auth generation，认证状态变化会撤销旧 object URL；旧 `auth/token.ts`、`crm_token` 与直接 401 clear-token 契约已退役。
- TDD VERIFY：`npm run test:auth` 8/8、`npm run test:api-client` 4/4、`npm run test:customer-avatar` 6/6、`npm run test:data-export` 8/8，共 26/26 通过；`npm run lint` 与 `npm run build` 均退出 0，build 只保留既有 chunk size warning。
- 浏览器 VERIFY：1280×720 登录页双栏完整、邮箱默认聚焦、无横向溢出；375×812 单栏无横向溢出，表单位于同一可滚动页面；欢迎页 `ArrowRight` 将选中态和焦点从“客户档案”移到“档期日历”；registration=false 时 `/register` 无 form；验证 URL 从含 action fragment 的入口变为无 fragment 的 `/verify-email`；浏览器日志 18 条中 Bearer／Authorization／cookie／action-token 敏感模式匹配为 0。
- Storage／URL 残留：`test:auth` 在加载认证模块前把 localStorage、sessionStorage 与 IndexedDB 全部替换为调用即失败的陷阱，8 个场景结束计数为 0；`rg` 确认 `frontend/src/auth` 无 Web Storage 引用，前端源码／测试无旧 token helper，仅测试自身保留 `crm_token` negative matcher。浏览器只读验证面不暴露 Storage API，因此不读取或保存任何浏览器 storage value。
- 基线风险：`npm run test:prototype` 仍为 3/4，通过外的唯一失败仍是旧契约要求 `CustomerNewPage` 直接调用 `listCustomers`，当前真实实现已委托 `CustomerPicker`；本 step 未新增 prototype failure，最终聚合门禁前按最窄范围同步该测试契约。
- 清洁度：`git diff --check` 通过；认证／页面／transport diff 无 debug 输出、TODO／FIXME／XXX、注释旧代码、Web Storage bearer、URL bearer 或 production 注册默认开启。

## STEP-006 Legacy claim／bootstrap／preflight

- 第一性原则边界：本 step 只改变 legacy 认领与首账号 bootstrap 的受信运维入口、退役 server seed 路径并建立可重放的 preflight／rollback 证据；不执行生产 migration／down／cutover，不切换生产注册开关，不旋转真实 root secret，也不让 CLI 绕过 account-auth application seam。
- RED（legacy application contract）：dry-run 零副作用测试先因缺少 `LegacyClaimRecord`、`LegacyClaimCommand` 与 `LegacyAuthState` 编译失败；随后以失败即报错的 random reader 固定证明 dry-run 在随机数、selector、token、identity ID、数据库 mutation 与邮件之前返回。
- GREEN（claim 状态机）：repository seam 分为只读 `PlanLegacyClaim`、事务 `BeginLegacyClaim` 与聚合 `InspectLegacyState`；结果只暴露 `ready`、`pending_same_email`、`already_claimed`、`conflict`、`no_target` 五种固定状态，以及 SHA-256 截断的 account／email reference。正式认领覆盖唯一 target、同邮箱 pending retry 替换旧 token、已完成、冲突与无 target；完成后状态为 legacy=0、pending=0、active=1。
- RED／GREEN（`accountctl`）：CLI tests 先因缺 `run`、`commandDeps`、exit code 与 application seam 编译失败；随后交付 `accountctl auth claim-legacy --email <owner-email> [--dry-run]` 与 `accountctl auth bootstrap --email <owner-email> [--dry-run]`。密码只读 `ACCOUNTCTL_AUTH_PASSWORD`，不接受 `--password`；CLI 不执行 migration、不直接读业务 SQL，dry-run 不触碰 avatar 文件探针。
- CLI 结果契约：退出码固定为 0=成功／ready dry-run、1=内部／配置／store 失败、2=usage／validation／缺密码环境变量、3=non-ready、4=邮件投递失败。单行 JSON 只含固定事件、状态、计数、脱敏引用与 remediation；legacy 事件固定 `auth.legacy_claim`。投递失败只引导 resend 或相同 claim 重试，不输出密码、完整邮箱、action URL／token、数据库 URL、Bearer、Authorization 或 cookie。
- Server seed 退役：composition root 已删除 `EnsureDefaultAccount`、`SeedAdminPassword` 与 `account-seed` 启动路径；机械 negative test 禁止这些标识重新进入 server。新增 `LoadAccountAuth` 供 server／accountctl 共用认证配置，且不创建／探测 avatar 目录。旧 `auth/seed.go` 仅保留 deprecated rollback／历史契约，生产 server 不可达。
- Production preflight：`--seed-state empty|initialized` 仅保留参数兼容，两种状态都要求 `SEED_ADMIN_PASSWORD` 缺失或为空；任意非空值固定 exit 4／`remove-retired-seed-secret`。公开注册 true 固定 exit 4／`public-auth-hardening-not-complete`；同时核验稳定 issuer、canonical HTTPS origin、Resend 生产配置与 `__Host-crm_refresh` 的 HttpOnly／Secure／Strict／Path=/／无 Domain／absolute-expiry clamp／clear profile。`./scripts/test-production-preflight.sh` 24/24 cases 通过。
- Repo-pinned cutover／rollback harness：真实 PostgreSQL fixture 证明 dry-run 零写／零邮件，正式 claim + verify 保留相同 account ID、客户／订单／档期／提醒／设置 counts 与 avatar object checksum；legacy-only 0012 down 从 schema 12 回到 11 且数据保留，新式 null legacy hash 账号以 `auth_schema_down_blocked_new_accounts` fail closed，旧 binary 边界固定为 `new_style_accounts_unavailable_to_old_binary`。
- 脱敏报告：`auth_legacy_cutover` 仅含固定状态、计数、checksum 与 `account:<digest>`／`email:<digest>`；`auth_legacy_rollback` 仅含 migration checksum、schema version、布尔结果与固定 marker。两份报告均由 `backend/internal/platform/store/auth_legacy_cutover_harness_test.go` 在 synthetic 数据上生成，不含 raw account ID、完整邮箱、credential、Bearer、cookie、Authorization 或 provider body。
- CMD-004：`./scripts/test-auth-legacy-cutover.sh` 共 6/6 checks、0 失败，覆盖 accountctl、旧 access JWT、同一路由 password-only body 400／`validation_failed`／无 Set-Cookie、server seed retired、真实 PG admission／concurrency 与 rollback harness；脚本已纳入 `make test`。
- 聚合 VERIFY：`go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... ./internal/platform/config/... ./cmd/accountctl/... ./cmd/server/... -count=1 -parallel=1` 六组 package 全部通过、0 失败；`bash ./scripts/test-v1-ops-common.sh` 退出 0，`./scripts/test-v1-ops-contract.sh` 的 catalog／results 均 148 cases 通过，negative corpus 被正确拒绝。
- 清洁度：`git diff --check` 通过；旧 `BeginLegacyClaim(context.Context, string, string)` 签名不存在；server／config 不引用 retired seed；README 中 `SEED_ADMIN_PASSWORD` 只用于“已退役／必须为空”的运维说明；目标代码未发现 debug 输出、临时 TODO／FIXME／XXX。25 条 acceptance checks 未提前修改，继续保持 `pending`。

## STEP-007 E2E／全域隔离 harden

- 第一性原则边界：本 step 只补齐两个真实 verified account 的 HTTP 主链、业务域隔离、active-only 后台枚举与结构化认证事件证据；不新增密码找回／修改、phone／SMS、OAuth／SSO、device center 或其他认证入口，也不执行生产 migration、cutover、注册开关或 secret 操作。
- RED／GREEN（结构化认证事件）：新增 allowlist/redaction 测试首先以 `auth event count=0 want=3` 真实失败，证明 `auth.login`、`auth.email_verified` 与 `auth.refresh_reuse` 缺失；最小实现新增 `AccountID` session 投影和集中 event helper。三个事件只允许 `event`、`result`、`failure_class`、`account_ref`、`session_ref`，引用固定为不可逆截断摘要；reuse 仅在 typed `ErrRefreshReuse` 时记录。目标测试随后 1/1 通过，并验证不含完整邮箱、账号原 ID、密码、access／refresh／action token、cookie 或 Authorization。
- GREEN（真实 PostgreSQL HTTP E2E）：`TestAccountAccessHTTPPostgresE2EAndFullIsolation` 使用 Testcontainers PostgreSQL、真实 migration/store/auth service/router 与 synthetic mail，连续完成两个账号的 register → action fragment → verify → refresh cookie → `/me`；账号 A 再完成 refresh rotation、使用新 access 访问 `/me`、logout，以及旧 refresh 在 logout 后固定 401 + clear cookie。
- 全域隔离：同一 E2E 为两个账号分别建立客户、订单、档期、提醒、头像、设置与导出数据，并核验 avatar reconciliation checkpoint；账号 A 的 export 必须含 A marker，且不得含 B marker、B timezone 或 B account ID。A 对 B 的 customer detail、avatar content、order patch、schedule patch、reminder done 五条越权路径全部固定 404。
- Active-only：fixture 同时包含 2 个 verified active、1 个 pending registration 与 1 个 `legacy_unclaimed`；`AccountScopes()` 只返回两个 active。既有 reminder／avatar maintenance／Telegram digest 的 active-only 消费者证据由 store 聚合回归共同重放，业务读写继续由 `AccountScope` 隔离。
- 异常／并发／Origin：STEP-003／004／006 已建立 admission concurrency、refresh reuse、logout matrix、Origin 零调用／零 cookie mutation 与 legacy negative；本 step 的真实 HTTP 主链和三包聚合测试重新重放这些证据，未发现新增失败。
- 范围守护：`TestAuthTypedErrorsItem2ScopeAndVerifyReferrerPolicy` 通过，forgot／reset／change 三条 item2 HTTP route 仍为 404；源码扫描中的 `phone` 仅来自既有 CRM 客户档案字段与测试数据，`reset` 仅来自普通 UI state 或未挂载的生成 schema，未发现 SMS／OAuth／SSO／device 认证入口。
- 前端 VERIFY：`npm run test:auth` 8/8、`npm run test:api-client` 4/4、`npm run lint` 与 `npm run build` 均退出 0；build 仅保留既有大 chunk warning。
- 后端 VERIFY：结构化事件目标测试 1/1、真实 E2E 1/1、item2 route 守护 1/1、两个既有 customer-profile 隔离测试 2/2 均通过。首次 auth/store/httpapi 聚合运行有两个旧 customer-profile tests 因 Docker Desktop/Testcontainers `port "5432/tcp" not found` 基础设施波动未启动；分别重跑后通过，随后 `go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1` 三包全部通过、0 失败。
- 清洁度：认证源码、HTTP E2E 与前端 auth 范围未发现新增 debug 输出、临时 TODO／FIXME／XXX；E2E log 扫描不含 synthetic 完整邮箱、密码、access／refresh、Authorization 或 cookie 名；`git diff --check` 通过。25 条 acceptance checks 未提前修改，继续保持 `pending`。

## STEP-008 完整门禁与证据包

- Prototype 契约窄修：基线 `npm run test:prototype` 的唯一失败仍要求 `CustomerNewPage` 直接调用 `listCustomers`；只更新静态测试，使其验证当前 `CustomerPicker` 委托、`candidateStatuses={['active']}` 与非手输 ID，产品代码不变，结果从 3/4 恢复为 4/4。
- Generate gate RED／GREEN：原 `make generate-check` 把合法未提交 generated diff 与 Git index 比较，导致契约已同步时仍固定失败；改为生成前后临时快照比较，继续拒绝真实漂移且允许 feature 提交前验证。重跑 `make generate-check` 退出 0。
- Lint 收口：`make check` 首次发现 4 项——HTTP E2E 越过 store 边界 import pgx、两个未处理 Close、一个可改 tagged switch；分别改用 `store.ErrNoRows`、显式处理 close、tagged switch 后，受影响 authmail/store/httpapi 包全绿，`golangci-lint` 为 `0 issues`。
- Active-only fixture 收口：全仓测试首次重放发现 customer avatar maintenance、reminder runner 与 Telegram digest 的旧 `CreateAccount` fixture 在 0012 后正确成为 `legacy_unclaimed`，因此不再被后台枚举。生产 `AccountScopes` 未放宽；三个测试域统一通过真实 legacy claim + verify application seam 激活 synthetic 账号，customer、reminder、reminder/digest 包随后全部通过。
- 旧 v1 ops fixture 收口：common preflight fixture 缺少新增账号认证配置矩阵而静默 exit 4；只补 synthetic issuer、canonical origin、registration=false、Resend driver/from/key。没有读取或保存 owner 环境值；common 15/15、package selftest、backup/restore safety、catalog/results 148/148 全部通过。
- CMD-001：auth/store/httpapi/accountctl 四组 package 串行通过，0 失败。
- CMD-002：`test:auth` 8/8、`test:api-client` 4/4、build 通过；只保留既有 Vite 大 chunk warning。
- CMD-003：OpenAPI Go/TypeScript 生成前后内容一致，退出 0。
- CMD-004：legacy cutover 6/6，脱敏 cutover/rollback 聚合报告保持 ready，退出 0。
- CMD-005：production preflight 24/24，退出 0。
- CMD-006：最终 `make check` 原样完整退出 0，覆盖全 Go 包、全部前端测试、双端 lint/build、cutover/preflight、v1 ops 与 generate-check。期间一次 digest Testcontainers 用例在 mapped-port 获取阶段出现已知 `port "5432/tcp" not found`，未进入断言；目标测试单独重跑通过，最终全链重跑也通过。
- 外部邮件证据：复用已持久化的 `receipt-verified` checkpoint；按 owner 指示未再次发送真实邮件，也未读取／输出 `.env` 值。
- Implementation gates：scope-gate `passed`、DoD runner CMD-001～006 全部 exit 0、evidence-pack `passed`。Scope 的三条 warning 仅来自 checklist 中“禁止 TODO／FIXME”的规范文字，不是源码施工痕迹；archguard/meta-cc 按 Provider Policy 记为 disabled/skipped，不构成核心缺口。
- 最终清洁度：临时 `scripts/lib/__pycache__` 已清理；`git diff --check`、源码 debug／TODO／FIXME／XXX 扫描与敏感字段守护通过；无 production test adapter 默认、真实 credential、完整真实邮箱、provider body、Bearer、cookie 或 Authorization evidence。25 条 acceptance checks 仍全部保持 `pending`，等待 acceptance 阶段裁决。

## 下一步

进入唯一一次独立 code review；review 通过后按 Goal 协议继续 QA 与 acceptance，不再追加设计 review。
