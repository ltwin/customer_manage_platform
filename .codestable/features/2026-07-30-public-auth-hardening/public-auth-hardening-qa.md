---
doc_type: feature-qa
feature: 2026-07-30-public-auth-hardening
status: passed
runner_state: completed
runner_reason: ""
runner_id: "/root/public_auth_qa_runner"
tested: 2026-07-31
round: 1
---

# public-auth-hardening QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
- Checklist: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml`
- Review: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-review.md`
- Evidence pack: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-evidence-pack.md`
- Gate results: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-gate-results.json`
- DoD results: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-dod-results.json`
- Implementation evidence: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-implementation.md`
- Runner: `/root/public_auth_qa_runner`，独立只读；未修改文件、未读取`.env`、未触发外部或生产动作。
- Diff basis: 当前未提交workspace diff；scope以implementation gate `changed_files`及review-fix归因增量为准。
- Baseline dirty files: owner任务外requirements、calendar／creative-shoot／welcome-login brainstorm与`frontend/image.png`全部排除。
- Feature type: functional（认证API、UI、session、持久化、邮件adapter、monitor与production readiness均有运行行为）。
- Core evidence gate: 注册→验证→登录→refresh→logout→forgot／reset／change→all-family revoke；公开认证body/token、Origin/cookie、limiter／readiness、两账号全域隔离、frontend anonymous状态和A1～A18 security catalog。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | S1／A1～A4 | core-functional | register→verify→login→refresh／logout与forgot→reset→new login→change→all-family revoke | PostgreSQL application + HTTP E2E | runner定向Go矩阵；security catalog `password-session`／`two-account-isolation` | 全闭环状态机成立，旧refresh固定401/clear | pass |
| QA-002 | S2／A5 | core-functional | reset/change Origin、cookie及400／401／403／429／500 | HTTP integration | `TestPasswordRoutesOriginCookieAndErrorMatrix`（catalog） | 只有可信Origin成功204清cookie，其他结果零cookie mutation | pass |
| QA-003 | review REV-001 | core-functional | 8 KiB body、异常selector／secret grammar | HTTP integration | `TestPublicAuthRejectsOversizedBodiesBeforeApplication`、`TestMalformedActionTokensDoNotReachLimiterOrRepository` | 固定4xx，limiter／repository／cookie零副作用 | pass |
| QA-004 | S3／A6～A10 | core-functional | subject/source预算、proxy、dummy bcrypt、timing与Retry-After | PG concurrency + function + HTTP | security catalog `limiter-proxy`／`timing-enumeration` | 预算、窗口、跨实例、XFF、同成本与timing阈值固定 | pass |
| QA-005 | S4／A13～A15 | supporting | 七事件allowlist、redaction、monitor阈值与exit0/1/2/3 | unit／CLI fixture | security catalog `event-redaction`／`monitor` | 无敏感字段，阈值和degraded语义固定 | pass |
| QA-006 | review REV-003／A16 | core-functional | limiter schema正常、drop PK、缺CHECK、同名错误索引、dirty/down | PostgreSQL integration + preflight fixture | runner readiness测试；`test-production-preflight.sh` | 任一损坏fail closed，probe回滚无残留 | pass |
| QA-007 | review REV-004／A1～A18 | core-functional | 13 case到18场景显式映射与空匹配防护 | executable catalog | 主agent完整`test-auth-security-catalog.sh` | 每个A场景依赖真实case marker，最终production_effect=false | pass |
| QA-008 | S7／A18 | core-functional | 两verified账号CRM全域与后台AccountScopes消费者隔离 | HTTP E2E + PG integration | runner隔离E2E；catalog `account-scope-consumers` | 只读写本账号，pending／legacy不进入active枚举 | pass |
| QA-009 | A11～A12 | core-functional | fragment首读即删、no-referrer、无storage、logout/password后anonymous、375px与focus | frontend test + production build + manual browser | catalog `frontend-auth`／`browser-evidence`；implementation browser记录 | 状态、路由、fragment与小屏可用 | pass（截图artifact缺失列为residual） |
| QA-010 | mail external checkpoint | supporting | Resend HTTPS adapter与真实非生产accepted receipt | external receipt + adapter tests | `email-account-access-mail-checkpoint.md` | HTTP 2xx、provider message id/accepted time allowlist、无secret | pass |
| QA-011 | D8～D9 | supporting | preflight／rotation／rollback不触发生产副作用 | CLI/script fixture | 48-case preflight、rotation-rollback、security catalog | synthetic证据全绿，真实生产动作仍需独立授权 | pass |
| QA-012 | review residual | non-functional risk | event crash gap、subject reset顺序、内部mail时序关联 | fault-model + existing tests | review第5/6/8节与runner复核 | 不伪造已消除；不阻塞当前功能闭环，带入acceptance | pass with residual risk |

## 3. Command Results

- Runner：`go test -p=1 ./internal/platform/store ./internal/platform/httpapi -run '^(TestAccountAuthApplicationPostgres|TestPasswordResetAndChangeRevokeAllRefreshFamilies|TestAuthReadinessInspectsCurrentLimiterSchemaAndLegacyCutover|TestAccountAccessHTTPPostgresE2EAndFullIsolation|TestPublicAuthRejectsOversizedBodiesBeforeApplication|TestMalformedActionTokensDoNotReachLimiterOrRepository)$' -count=1 -parallel=1 -v` → exit 0；store 7.899s、httpapi 2.370s，所有Testcontainers已停止。
- 主agent：`go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1` → exit 0；review-fix后的三个核心package全绿。
- 主agent：`./scripts/test-production-preflight.sh` → exit 0，48 cases passed。
- 主agent：`./scripts/test-auth-security-catalog.sh` → exit 0，13 case、A1～A18 passed，`synthetic=true`、`production_effect=false`。
- 主agent：`make generate-check` → exit 0；OpenAPI Go／TS生成物零漂移。
- 主agent：`git diff --check`与目标源码`TODO|FIXME|XXX|debug output`扫描 → exit 0／零命中。
- Runner自身的catalog重放在最终exit前按owner“立即收束”指令中断，未计入pass；QA只采用主agent修复后已完整取得的exit 0证据。
- 未重复运行`make check`／完整CMD-001～007 DoD：实现完成时已通过；review-fix后按owner要求只跑核心package、preflight、catalog、generate-check和清洁度，均覆盖本轮增量。
- 未执行production live preflight、deploy、migration／rollback、root rotation、cutover或registration开关：均需owner另行授权，且不是开发期QA应产生的副作用。

## 4. Scenario Results

- [x] QA-001 完整认证闭环：pass。
  - Evidence: `TestAccountAuthApplicationPostgres`含registration/verification/current-account与refresh状态机；`TestPasswordResetAndChangeRevokeAllRefreshFamilies`证明reset/change更新credential并撤销全部family；隔离E2E末尾logout 204+clear，旧refresh固定401+clear。
- [x] QA-002 Origin/cookie machine：pass。
  - Evidence: password routes matrix完整通过，错误路径无`Set-Cookie`。
- [x] QA-003 body/token成本边界：pass。
  - Evidence: oversized body和6类malformed token定向测试通过，应用边界零调用。
- [x] QA-004 limiter／anti-enumeration：pass。
  - Evidence: catalog `limiter-proxy`、`timing-enumeration`通过；provider accepted/failure/missing分支timing统计满足design阈值。
- [x] QA-005 events／monitor：pass。
  - Evidence: 七事件allowlist/redaction、JSONL/journald、阈值边界与exit0/1/2/3通过。
- [x] QA-006 live readiness负例：pass。
  - Evidence: drop PK、drop attempts CHECK、同名错误索引均`LimiterSchemaReady=false`；正常probe transaction rollback。
- [x] QA-007 security catalog可信映射：pass。
  - Evidence: Go正则预期数量、case marker、18条`require_scenario`均实际执行；无无条件A循环。
- [x] QA-008 账号隔离：pass。
  - Evidence: 两账号HTTP E2E与六类后台消费者通过；客户、订单、档期、提醒、头像、设置、导出无串号。
- [x] QA-009 Web状态：pass。
  - Evidence: frontend auth 11/11、api-client 6/6、production build；实现阶段已实际浏览1440×900与375×812并验证focus、无横向溢出、fragment/StrictMode与console零错误。没有保留截图artifact，列为残余证据质量风险，不等于核心路径未运行。
- [x] QA-010 真实邮件checkpoint：pass。
  - Evidence: 已有受控非生产Resend HTTP 2xx，provider message ID与accepted time仅按allowlist保留；本轮没有重复发信。
- [x] QA-011 副作用边界：pass。
  - Evidence: 所有QA均为Testcontainers／synthetic fixture／只读报告；没有生产动作。

## 5. Findings

### failed

none。

### blocked

none。真实production enable-ready、DNS／sender domain、proxy拓扑、production DB与真实rotation维护窗需要发布时独立授权和当次fresh evidence；它们不是开发期QA阻塞，也没有被伪报为已完成。

### residual-risk

- `auth.password_changed`尚无transactional outbox；数据库commit后、日志写入前进程崩溃可能永久缺事件。
- login/change subject reset尚未与最终session-create／password mutation处于同一PostgreSQL transaction；正确凭证后的后置内部失败可能已清空subject budget。
- 同时拥有低流量HTTP时间线与内部auth日志的受信operator，仍可能通过真实`auth.mail_delivery`时序推断eligible状态；事件无email/account/source/request id，仍需严格日志访问控制。
- timing benchmark使用injected clock／fake provider，不替代真实PostgreSQL、bcrypt、Resend网络和production runtime采样。
- 完整闭环由HTTP E2E、PostgreSQL application/session matrix和frontend tests组合证明；没有单一保留截图的浏览器one-shot artifact。实现阶段实际浏览已完成，但截图可复核性不足。
- 真实production Resend sender/DNS、proxy链、DB、evidence freshness和root rotation维护窗未在开发QA执行；production preflight必须在发布环境继续fail closed。

## 6. Cleanliness

- Debug output: pass；目标生产源码无新增调试输出。
- Temporary TODO/FIXME/XXX: pass；目标源码零命中。scope gate唯一`TODO` warning来自checklist规范文字“禁止TODO”。
- Commented-out code: pass。
- Unused imports / dead code from this feature: pass；Go核心package与frontend production build通过。
- Out-of-scope files: pass；owner的requirements、brainstorm与`frontend/image.png`未修改、未纳入QA或后续commit范围。
- Secret/effect boundary: pass；未读取`.env`，未发新邮件，未触发production migration／deploy／rollback／rotation／cutover／开关切换。

## 7. Verdict

- Status: passed。
- Next: 进入`cs-feat` acceptance阶段，逐条更新23个acceptance checks、确认residual risk与交付物；不再启动新的code review。
