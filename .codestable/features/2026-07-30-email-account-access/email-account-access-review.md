---
doc_type: feature-review
feature: 2026-07-30-email-account-access
status: blocked
reviewer: subagent
reviewed: 2026-07-31
round: 1
lane_a_state: completed
lane_a_ref: "/root/self_service_account_goal_driver/email_access_independent_review"
lane_a_reason: ""
lane_b_state: failed
lane_b_ref: "exec-session:61847"
lane_b_reason: "ocr review 超过 10 分钟仍无任何输出，为避免无限等待已终止（exit 130）"
pending_review_decision: code-review-skip-failed-ocr
pending_review_ref: "exec-session:61847"
pending_review_approval_ref: "approval-report.md#code-review-skip-failed-ocr"
review_fix_state: implementation-verified
closure_state: owner-declined-rereview
handoff_ref: "email-account-access-review.md#8-focused-closure"
---

# email-account-access 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-30-email-account-access/email-account-access-design.md`
- Checklist: `.codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml`
- Evidence pack: `.codestable/features/2026-07-30-email-account-access/email-account-access-evidence-pack.md`
- Gate results: `.codestable/features/2026-07-30-email-account-access/email-account-access-gate-results.json`
- DoD results: `.codestable/features/2026-07-30-email-account-access/email-account-access-dod-results.json`
- Implementation evidence: `.codestable/features/2026-07-30-email-account-access/email-account-access-implementation.md`
- Diff basis: 当前 `feature/userCenter` 分支的 unstaged / untracked diff，实现基线为 `411bbb1fc5bfc35e5617718cd8a0e5f242ad648f`。
- Review mode: initial
- Baseline dirty files: 当前非 ignored 变更均被 scope gate 的 `changed_files` 收录；受保护的用户内容仍隔离在 stash，不属于审查范围。

### Independent Review

- Detection: 宿主可用独立 Task agent；`ocr` CLI 已安装且 `ocr llm test` 连接成功。
- 环节 A 独立隔离 Task agent: independent-agent + completed，ref 为 `/root/self_service_account_goal_driver/email_access_independent_review`；全程只读，已返回完整 findings 与 QA focus。
- 环节 B OCR CLI: failed，ref 为 `exec-session:61847`；执行超过 10 分钟仍无任何输出，已按有界等待策略终止（exit 130），本轮不重启。
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded。
- Merge policy: 独立 reviewer 结论已由主 agent 逐条根据当前源码、调用路径、design 和可复现反例核验；并入 2 个 blocking、2 个 important，另有 1 个本地核验的 blocking。
- Gate effect: 环节 B 已按精确 ref `exec-session:61847` 发起 `RequestSkipFailedLaneB`，对应命名决策 `approval-report.md#code-review-skip-failed-ocr` 仍为 pending；在该决策被机械核验为 approved 之前，报告保持 `blocked`，不伪造 `changes-requested` 或 `passed`。

## 2. Diff Summary

- 新增：账号运维命令、认证/邮件/会话 store、HTTP E2E、迁移、前端认证会话与页面、运维验证脚本及 CodeStable 证据产物。
- 修改：OpenAPI、server/config/router/auth/store、前端 API 与欢迎页、测试 fixture、preflight 与 `Makefile`。
- 删除：旧前端 token helper 及旧登录页路径，由新的 auth session/page 结构替代。
- 未跟踪 / staged：存在 scope 内未跟踪新文件；无 staged diff。
- 风险热点：认证会话线性化、cookie/Origin、refresh replay 密文生命周期、数据迁移与 legacy claim 并发、账号隔离、邮件 provider 脱敏。

## 3. Adversarial Pass

- 假设的生产 bug：认证状态发生跨账号切换时，旧请求或迟到 refresh 会把旧会话操作以新账号凭证重放，或在 logout 后重新建立认证状态。
- 主动攻击过的反例：A 账号写请求迟到 401 + B 账号已登录；refresh 在 logout 完成后迟到；无后续 refresh 流量时 replay ciphertext 是否会由生产 runner 清理；显式默认端口的 `PUBLIC_BASE_URL` 与浏览器 Origin 序列化；空 local/domain 的 ASCII email。
- 结果：前三项升级为 blocking；Origin 默认端口与 AuthState shape 升级为 important；Testcontainers 波动、DoD digest 后同步与生产外部边界保留为 residual risk / QA focus。

## 4. Findings

### blocking

- [ ] REV-001 `frontend/src/auth/session.ts:70` 认证 coordinator 没有稳定的 session epoch，会跨账号重放旧 payload，也会在 logout 后被迟到 refresh 恢复为 authenticated。（来源：independent-agent；维度：spec 合规 + 安全 + 账号隔离 + 并发）
  - Evidence: `authorizedFetch` 只捕获 `requestToken`，迟到 401 时只要全局 `accessToken` 已变为另一个非空值，就用新 token 重放原请求，无法区分同账号 rotation 与新账号登录。`refreshAccessToken` 完成后无条件回写 token/status，`logoutSession` 也没有使已启动 refresh/restore flight 失效。独立 reviewer 的确定性反例已实际观察到 A payload 分别带 `Bearer account-A-access` 与 `Bearer account-B-access` 执行，以及 logout 后最终状态回到 authenticated。
  - Impact: CRM 的客户、订单、档期、提醒等写请求可被错误带入新账号 `AccountScope`，形成跨账号误写；迟到 access JWT 在 refresh family 撤销后仍可有效最多 10 分钟。违反 D7/A9/A10/A16 与 CHK-012/013/019/025。
  - Expected fix scope: 在 webapp-auth 内引入与普通 token rotation 分离的会话 epoch；旧请求只能在同 epoch 重放；refresh/restore 响应在 epoch 变化后必须丢弃；logout 必须使已启动 flight 失效；login/verify 也不得被旧 auth action 覆盖。用确定性前端测试收口，不重跑无关全链。

- [ ] REV-002 `backend/cmd/server/main.go:212` refresh replay ciphertext 的生产 maintenance owner 没有接入 server lifecycle。（来源：independent-agent；维度：spec 合规 + 安全 + 持久化生命周期）
  - Evidence: `auth.Service.SweepExpiredReplayCiphertexts` 和 store SQL 均已实现，但排除测试文件后零生产调用点；server runners 只有 avatar、reminder 与可选 Telegram。现有“无流量 sweep”测试是直接调用 service，只证明方法/SQL，不证明生产 wiring。
  - Impact: 无后续 refresh 流量时，10 秒 grace 已过期的加密 successor bearer 会无限期留在数据库，扩大数据库 + root secret 联合泄漏的历史 token 暴露面，违反 D10/A8/CHK-008。
  - Expected fix scope: 新增小型 auth replay sweep runner，server 启动后立即调用并按固定 cadence 使用有界 batch，共享 server cancellation；错误日志只记固定 event/result/failure class。增加 composition/lifecycle focused test，保留已有真实 PostgreSQL 物理清列测试。

- [ ] REV-003 `backend/internal/platform/auth/service.go:436` 邮箱规范化只检查 ASCII、总长和 `@` 数量，会接受空 local/domain 或含非法空格的地址。（来源：local；维度：spec 合规 + 正确性）
  - Evidence: `normalizeEmail` 对 `ab@`、`@ab`、`a b@example.invalid` 这类非法地址只做 `strings.Count(normalized, "@") == 1` 检查，可直接通过；roadmap §4.2 要求首期只接受“语法合法且总长不超过 254 字节的 ASCII email”，design A4 要求输入边界固定 validation。
  - Impact: public register 或 bootstrap 可以创建永远无法投递/验证的 pending account。在空库 bootstrap 下，该账号会使后续正确 bootstrap 固定命中 `bootstrap_not_empty`，需要人工数据修复，违反首账号安全恢复边界。
  - Expected fix scope: 在 account-auth 深模块内补齐 ASCII addr-spec 语法核验，保留 trim + ASCII lowercase + 254-byte 契约；增加 valid/invalid 表驱动单测试，保证 Register/PlanBootstrap/Resend/Login/claim 共享同一规则。

### important

- [ ] REV-004 `backend/internal/platform/config/config.go:180` `PUBLIC_BASE_URL` 接受显式默认端口，但浏览器 Origin 会省略该端口，导致所有会话端点被拒绝。（来源：independent-agent；维度：spec 合规 + 配置正确性）
  - Evidence: Go canonicalizer 直接保留 `parsed.Host`，production preflight 也直接使用 `parsed.netloc`，因此 `https://app.example.invalid:443` 会通过并保留 `:443`；`requireTrustedOrigin` 做字节级 exact equality，而浏览器 origin serialization 输出 `https://app.example.invalid`。
  - Impact: 一个通过 runtime 和 production preflight 的配置会让 verify/login/refresh/logout 全部固定 403；`http://localhost:80` 有同类问题。
  - Expected fix scope: runtime 与 preflight 使用一致规则去掉 scheme 默认端口，或一致拒绝显式默认端口；补 HTTPS `:443`、loopback HTTP `:80`、非默认端口和 IPv6 focused tests。

- [ ] REV-005 `frontend/src/auth/session.ts:3` 公开 AuthState 没有交付 approved design 中的 account 和 access expiry 契约。（来源：independent-agent；维度：spec 合规 + 模块接口质量）
  - Evidence: design 第 285–288 行将 authenticated state 定义为 `{account: Me, accessToken, expiresAt}`；当前 snapshot 只有 `{status, generation}`。Startup restore 虽请求 `/me`，但只执行 `await response.json()` 后丢弃账号投影，`expires_in` 也没有进入状态。
  - Impact: 下游无法从认证模块取得当前账号，token 与账号/会话之间没有可观测绑定；REV-001 也因此无法识别 token 变化是同账号 rotation 还是新会话。
  - Expected fix scope: 与 REV-001 同一窄修复完成；authenticated snapshot 持有 `Me` 投影、access expiry 和稳定 session epoch，restore/login/verify 通过同一 action 建立完整状态。

### nit

- none

### suggestion

- `backend/internal/platform/httpapi/auth.go:283` 的公开 JSON binder 没有 body size 上限。Login/resend/verify 已公开，建议在下一个 `public-auth-hardening` feature 与 limiter/timing budget 一起加入统一请求体上限，本 feature 不升级为缺陷。
- Trusted logout 的数据库内部错误下，handler 不清 cookie，但前端仍无条件进入 anonymous；瞬时 DB 故障后 reload 可能恢复旧 cookie 会话。A9 未定义 internal-error 分支，建议 QA 或下一 hardening feature 明确语义。

### learning

- 直接调用 service 的“无流量 sweep”测试只证明状态机和 SQL，不证明生产进程拥有该 lifecycle；durable cleanup 同时需要 application test 和 composition/wiring test。
- Refresh single-flight 只解决“同时只发一次 refresh”，不自动解决认证会话线性化；后者需要不随普通 token rotation 变化、但会随 logout/account switch 变化的 session epoch。

### praise

- `RotateRefresh` 使用 generation + family 行锁、固定 grace replay envelope、tamper 后同事务撤销和超窗 reuse 撤销，持久化状态机边界清晰。
- Migration down 在存在新式 null legacy hash 账号时 fail closed，没有伪造 hash 或删除账号。
- Auth/mail 事件未输出完整邮箱、Bearer、cookie、Authorization 或 provider body；Resend adapter 的 attempts、attempt timeout 和 total deadline 均有显式上界。
- `AccountScopes()` 生产查询只枚举 active；测试 fixture 通过真实 claim + verify 激活，没有放宽生产过滤。
- `generate-check` 比较生成前后内容，能在未提交 feature diff 上检查幂等，同时仍会拒绝真实 codegen 漂移。
- v1 ops 新增值只存在 synthetic fixture，未改变生产变量解析语义。
- Scope gate 的三条 TODO/FIXME warning 已核实为 checklist 中“禁止 TODO/FIXME”的规范文字，不是源码施工标记。

## 5. Test And QA Focus

- QA 必须重点复核：A 的迟到 401 在 B 登录后不得用 B token 重放；refresh 晚于 logout/login/verify 返回时必须丢弃；同 epoch 并发 401 仍只有一个 refresh；JSON/multipart/avatar/export 一致遵守 epoch；restore/login/verify 建立完整 account/expiry state；avatar cache 仍随 auth generation 变化清理。
- QA 必须重点复核：Replay runner 启动即 sweep、固定 cadence、有界 batch、cancel 后退出、错误日志脱敏；真实 PostgreSQL 在无后续 refresh 时最终清空过期 `replay_ciphertext/replay_until`。
- QA 必须重点复核：`https://host:443`、非默认 HTTPS port、localhost `http:80`、localhost 非默认 port 和 IPv6 的 runtime/preflight/Origin 一致性；空 local/domain、空格、双点等非法 ASCII email 不得进入 repository。
- Evidence pack residual risks / gate warnings：TODO/FIXME 三条均是 checklist 规范文本；Testcontainers mapped-port 波动保留为测试基础设施 residual risk；DoD checklist digest 是命令完成后对最终 checklist 状态的机械同步，不需因此重跑旧全链。
- 建议新增或加强的测试：上述 session epoch 两个确定性反例、AuthState shape、server replay runner lifecycle、default-port canonicalization/preflight、ASCII email syntax 表驱动单测。
- 不能靠 review 完全确认的点：真实 production proxy/TLS 后 Origin 序列化、真实 down/cutover/root rotation、邮件 provider 外部状态。不再发送真实邮件，复用已有 `receipt-verified` 证据。

## 6. Residual Risk

- Docker Desktop/Testcontainers 的 `port "5432/tcp" not found` 发生于 mapped-port 基础设施阶段，目标测试随后和最终全链均通过；保留为测试基础设施 residual risk，不升级为业务 finding。
- DoD checklist digest 在 core commands 通过后随最终 checklist 状态机械同步。命令退出码和输出一致，不要求重跑已通过全链；review-fix 只运行相关 focused tests 并记录新代码证据。
- `public-auth-hardening` 明确承接 persistent limiter、完整 timing budget、来源解析和生产开放注册；当前公开登录面的 brute-force/大请求风险不误归入本 feature 范围缺陷。
- OCR 环节已绑定 `exec-session:61847` 超时失败；只有同 unit 命名 skip approval 能将它转为 skipped，当前仍为 pending。

## 7. Verdict

- Status: blocked
- Review-fix implementation: verified。REV-001～005 已完成窄修并通过下节所列定向测试、lint/build 与 `git diff --check`。
- Gate truth: 仍为 `blocked`。`approval-report.md#code-review-skip-failed-ocr` 没有收到与该命名决策匹配的 Option A 批准，lane B 继续保持 `failed`；owner 又明确要求修复后不再进行下一轮 review，因此不能把主执行者的实现验证伪造成独立 review `passed`。
- Next: Goal 以 `owner-stop` handoff；不进入 QA、acceptance、scoped commit 或下一 feature。若未来恢复，必须由 owner 明确选择如何恢复当前 review gate；本报告不会从既有 Goal execution/acceptance/commit 授权推断该决定。

## 8. Focused Closure

Closure state: `owner-declined-rereview`。以下是 implementation review-fix 证据，不是第二轮审查，也不改变本报告 `blocked` verdict。

| Finding | 窄修文件 | 实现验证 | 结果 |
|---|---|---|---|
| REV-001 | `frontend/src/auth/session.ts`、`frontend/src/pages/auth/LoginPage.tsx`、`frontend/src/pages/auth/VerifyEmailPage.tsx`、`frontend/scripts/auth.test.ts` | 增加稳定 session epoch；同 epoch 才可 refresh/replay；旧 refresh/restore/auth action 响应失效；cookie-mutating flights 在 logout 与新 auth action 间有序；确定性覆盖 A→B 迟到 401、refresh→logout、迟到 auth action | implementation verified；未复审 |
| REV-002 | `backend/cmd/server/auth_replay_sweep.go`、`backend/cmd/server/main.go`、`backend/cmd/server/main_test.go` | server lifecycle 启动即 sweep、1 分钟 cadence、batch=100、context cancel；错误日志只含固定 event/result/failure_class | implementation verified；未复审 |
| REV-003 | `backend/internal/platform/auth/service.go`、`backend/internal/platform/auth/service_test.go` | 保留 trim/ASCII lowercase/254 bytes；`mail.ParseAddress` 后要求无 display name 且 address exact；表驱动拒绝空 local/domain、空格、双点、comment | implementation verified；未复审 |
| REV-004 | `backend/internal/platform/config/config.go`、`backend/internal/platform/config/config_test.go`、`scripts/production-preflight.sh`、`scripts/test-production-preflight.sh` | runtime/preflight 一致拒绝显式默认端口；保留非默认 HTTPS/localhost HTTP port 与 IPv6 覆盖 | implementation verified；未复审 |
| REV-005 | `frontend/src/auth/session.ts`、两处 auth page、相关前端测试 | authenticated snapshot 交付 account/accessToken/expiresAt/sessionEpoch/generation；restore/login/verify 通过统一 session action 建立完整状态 | implementation verified；未复审 |

定向验证（均为 2026-07-31 当前 diff）：

- `cd frontend && npm run test:auth`：exit 0，11/11。
- `cd frontend && npm run test:api-client`：exit 0，4/4。
- `cd frontend && npm run test:customer-avatar`：exit 0，6/6。
- `cd frontend && npm run test:data-export`：exit 0，8/8。
- `cd frontend && npm run lint`：exit 0。
- `cd frontend && npm run build`：exit 0；仅保留既有大 chunk warning。
- `cd backend && go test ./internal/platform/auth -count=1`：exit 0。
- `cd backend && go test ./cmd/server -count=1`：exit 0。
- `cd backend && go test ./internal/platform/config -count=1`：exit 0。
- `./scripts/test-production-preflight.sh`：exit 0，27 cases。
- `git diff --check`：exit 0。

按 owner 指令未运行：第二轮 reviewer、OCR retry、完整 `make check`、QA、acceptance、commit。此前完整 `make check` 与真实邮件 `receipt-verified` 证据继续保留，未重发真实邮件。
