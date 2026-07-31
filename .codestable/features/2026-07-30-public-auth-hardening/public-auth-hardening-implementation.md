---
doc_type: feature-implementation
feature: 2026-07-30-public-auth-hardening
status: completed
updated: 2026-07-31
---

# public-auth-hardening 实现记录

## Owner dependency override

- `email-account-access` 的 roadmap 条目与 canonical review 继续保留真实 `in-progress`／`blocked` 状态，不伪造 `done` 或 `passed`。
- Owner 于 2026-07-31 明确要求先完成剩余 `public-auth-hardening`，以形成注册、验证、登录、刷新、登出、密码恢复／修改和公开认证防护的完整闭环。
- 本次 override 只解除 `public-auth-hardening` implementation admission 的流程阻塞；不授权生产部署、迁移、cutover、root rotation、公开注册开关、commit 或 push，也不替代最终统一收口。

## 第一性原则 pre-pass

- 外部行为：新增密码恢复／修改、全 refresh family 撤销、持久化 subject/source 限速、防枚举 timing、认证 monitor 与 production enable-ready 证据闸门。
- 不可破约束：复用现有 account-auth／PostgreSQL／AuthState／Origin-cookie／root-secret KDF；账号隔离继续只由服务端 `AccountContext`／`AccountScope` 决定；公开响应、日志和证据不得泄露邮箱、IP、密码或 token。
- 最小充分改动：严格按 STEP-001～STEP-007 增量扩展现有 seam，每一步先 RED、后 GREEN，并即时更新 checklist 与证据。
- 必须不写：MFA／OAuth／设备中心／换绑邮箱；access blacklist；in-memory production limiter；生产 fake mail；自动 deploy／cutover／rotation／开关切换；方案外日历与 brainstorm 改动。

## 基线预检

- 后端：`go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1` 通过，3 个目标 package 均为 0 失败。
- 前端：`npm run test:auth` 11/11、`npm run test:api-client` 4/4、`npm run build` 通过；仅有既有 Vite chunk-size warning。
- Dirty scope：实现开始前已有 `.codestable/requirements/VISION.md`、`.codestable/requirements/schedule-calendar.md` 与 calendar／creative-shoot／welcome-login brainstorm、`frontend/image.png` 变更，全部视为 owner/task 外内容，不触碰、不暂存。

## STEP-001 依赖与 limiter 骨架

- 状态：完成。
- Owner override 边界：依赖 child 的代码提交已经存在，但 roadmap／review 状态未伪造；本 step 只补回原 roadmap 已定义而 child 实现缺失的 `ClientMeta`／limiter KDF seam，并记录真实 override。
- RED（limiter digest）：`TestLimiterDigesterMatchesVersionedProtocol` 首次因缺少 `NewLimiterDigester`／`AuthAction` 编译失败；GREEN 后固定 `crm-auth/v1/limiter-hmac` HKDF、versioned HMAC framing、IPv4-mapped `Unmap` 与 action namespace golden vectors。
- RED（schema）：`TestAttemptLimiterMigrationCreatesVersionedBudgetTable` 首次得到 0 个目标列；GREEN 后新增 `0013_auth_attempt_limiter` up/down，表只保存 action、subject/source dimension、`v1:` digest、window 和 attempts，并以约束拒绝未知 action、原始 IP 与非版本化 digest。
- RED（持久化预算）：跨实例测试首次因 `Store.Consume` 不存在编译失败；GREEN 后 PostgreSQL transaction 对 subject/source 依固定顺序行锁，覆盖 5/30/15m、3/20/1h、10/30/15m、half-open 窗口、subject reset、20 并发恰好 5 个 login 放行，以及重启／多实例不清零。
- RED（可信来源）：`TestResolveClientSourceUsesOnlyTrustedXFFChain` 首次因 resolver 不存在编译失败；GREEN 后只读取 XFF，direct peer 不可信时完全忽略，可信时按 1024 bytes／16 hops／strict `netip.ParseAddr` 从右向左剥离；空项、端口、引号、超限整链回退 direct，`Forwarded` 不参与。
- RED（配置）：CIDR 测试首次因缺少 parsed prefixes 和稳定错误类编译失败；GREEN 后 `TRUSTED_PROXY_CIDRS` 在启动时解析 canonical IPv4/IPv6 CIDR，非法值固定归类到该配置键。
- Registration=false：既有 handler gate 仍位于 body parse、application、repository、mail 之前；`TestAuthCapabilitiesAndRegistrationGate` 在本 step 聚合回归中通过，因此 limiter 尚未接入时也保持机械零消费，后续 STEP-003 接入后继续用 spy 固定该顺序。
- VERIFY：`go test ./internal/platform/auth ./internal/platform/store ./internal/platform/httpapi ./internal/platform/config ./cmd/server -count=1 -parallel=1` 全部通过；store 含真实 PostgreSQL migration／并发／跨实例矩阵。
- 迁移兼容窄修：0013 成为最新迁移后，只给既有 account-auth／Telegram／avatar rollback tests 增加一个 limiter down step；未改变旧 migration SQL 或生产业务逻辑。
- 清洁度：`git diff --check` 通过；本 step 新增源码无 debug output、临时 TODO／FIXME／XXX、注释旧代码、完整邮箱／IP、secret 或 in-memory production fallback；owner 的日历与 brainstorm 文件未触碰。

## STEP-002 Password transaction

- 状态：完成。
- RED：真实 PostgreSQL 测试首次因缺少 `BeginPasswordReset`、`ResetPassword`、`ChangePassword` 编译失败；GREEN 后新增 30 分钟 `password_reset` purpose、generic dispatch、purpose-bound proof、credential 更新与 all-family revoke。
- Generic forgot：missing、pending、legacy/state mismatch 不创建 token、不发信但 application 返回同一非错误 dispatch；active verified 才替换旧 reset token并尝试邮件。Provider failure不回滚 token，仍只投影固定 delivery class。
- Token 边界：重发后旧 token、verification purpose、tamper、精确 30 分钟边界、成功后的重复消费均归一为 `invalid_or_expired_token`，credential／session 无副作用；成功删除账号全部 reset token。
- Reset transaction：同一 PostgreSQL transaction 锁定 action token与 active credential，更新 bcrypt、失效 reset token并把账号全部 refresh family置为 revoked；两个既有设备 refresh 均变为 unauthorized，既有 access JWT仍按 ADR-006 保留最多 10 分钟 TTL。
- Change transaction：wrong current password 固定 unauthorized且 refresh family仍可轮换；正确 current password以 expected hash防并发旧写，随后执行与 reset 相同的 credential/token/all-family transaction，并重置 change subject bucket。
- Mail：`ActionPasswordReset` 使用 `/reset-password#token=...`，Resend模板只新增固定“重置密码”主题／动作，不改变 provider retry或脱敏事件。
- VERIFY：`go test -p=1 ./internal/platform/auth/... ./internal/platform/authmail/... ./internal/platform/store/... ./internal/platform/httpapi/... ./cmd/accountctl/... ./cmd/server/... -count=1 -parallel=1` 六组 package 全部通过；核心 transaction 测试使用真实 PostgreSQL。
- 清洁度：`git diff --check` 与目标源码 debug／TODO／FIXME／XXX 扫描通过；测试和实现不输出 password、raw token、完整邮箱／IP、cookie或Authorization，不执行真实邮件或生产动作。

## STEP-003 防枚举与 timing

- 状态：完成。
- Limiter 编排：register／resend／login／forgot／verify／reset／change 全部由 account-auth 在业务动作前 consume；store 错误固定 internal，denied 固定 rate-limited。registration=false 的 HTTP spy 现在连 limiter 调用也计数并证明为零；source 没有 reset seam，成功 login／change 只重置各自 subject，verify／reset 不重置。
- 事务后错误修复：change subject reset 移到密码事务提交之前；reset 失败时 credential transaction 不执行，避免“密码已提交但应用返回 500、客户端不清会话”的不一致结果。成功路径顺序由测试固定为 subject reset → password/session transaction。
- RED/GREEN（登录工作量）：存在账号错密与缺失账号测试先因 timing seam／compare seam 缺失而编译失败；GREEN 后两条路径都选择 real/dummy production-cost bcrypt hash并恰好调用一次 compare，dummy hash在 Service 构造期校验 bcrypt DefaultCost。
- RED/GREEN（响应预算）：公开 mail action 固定 1000ms floor + 0～50ms CSPRNG jitter，login／verify／reset／change 固定 300ms floor + 0～25ms；测试注入 clock／delay／timing random，只推进虚拟时间，不使用 sleep。register bootstrap CLI不套公开响应预算。
- 统计证据：missing、eligible+accepted、eligible+provider failure 各先20次warmup，再各200次采样；所有样本位于1s～1.05s，任意两分支 median delta=0、p95 delta=0、p95 ratio=1.0，满足≤15ms／≤30ms／≤1.25。
- Resend deadline：production默认单次400ms、总850ms、重试间隔25ms、最多2次；机械证明两次尝试与间隔不超过900ms mail floor，保留caller cancellation、临时失败一次重试和整段deadline分类。
- HTTP 429/event：`Retry-After`向上取整且至少1秒；ErrorEnvelope不含dimension／remaining／deadline。`auth.rate_limited`只记录result、failure_class、action与versioned source_digest，不含subject digest、完整email/IP/password。
- VERIFY：`go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1` 全绿（auth 0.97s、store 33.61s、httpapi 28.19s）；`go test -p=1 ./internal/platform/authmail/... ./internal/platform/config/... ./cmd/server/... -count=1 -parallel=1` 全绿。
- 清洁度：`git diff --check` 通过；新增代码无 debug output、临时 TODO／FIXME／XXX、sleep-based timing test、raw subject/source或provider body输出；owner 的日历、brainstorm与image文件未触碰。

## STEP-004 HTTP／OpenAPI／Web／Mail

- 状态：完成。
- HTTP／OpenAPI：Go codegen纳入 `public-auth-hardening` tag并挂载 forgot／reset／change；forgot保持public且无Origin gate，reset为public+exact Origin，change为protected auth+exact Origin。reset／change仅成功204清refresh cookie，missing／wrong Origin、validation、limited、invalid token、wrong current password与internal均不清cookie。
- HTTP matrix：`TestPasswordRoutesOriginCookieAndErrorMatrix` 与 `TestAuthTypedErrorsAndActionPageReferrerPolicy` 覆盖403零application／limiter副作用、400／401／429／500响应、204 cookie清理，以及 `/verify-email`／`/reset-password` 的 `Referrer-Policy: no-referrer`。
- Frontend：新增 forgot／reset／change API与页面；change password的业务401使用one-shot authorized request，不触发refresh／credential mutation replay。登录页、desktop sidebar与Settings均提供找回／修改／退出入口；登出继续复用既有`logoutSession`并立即清内存auth。
- RED／GREEN（Strict Mode fragment）：浏览器首次发现带fragment的reset页面在React Strict Mode第二次layout effect中把已消费token覆盖为missing；先给`auth.test.ts`增加started guard契约并得到10/11、1失败，再给`ResetPasswordPage`加入one-shot ref guard，复测11/11全绿。fresh navigation后地址栏hash为空、token页面保持ready且新密码输入框自动聚焦。
- Browser：1440×900验证desktop forgot／reset布局；375×812验证单列宽360px、无横向溢出、forgot／reset表单可滚动到达、字段focus和错误反馈可见；login页存在“忘记密码？”入口，missing-token固定错误态，浏览器console warning／error为0。浏览器不读取存储；`npm run test:auth`用forbidden localStorage／sessionStorage／IndexedDB spy证明auth/token路径调用为0，源码静态扫描也无storage／console token路径。
- VERIFY：`make generate-check`通过；`go test -p=1 ./internal/platform/auth/... ./internal/platform/authmail/... ./internal/platform/store/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1`全绿；`npm run test:auth` 11/11、`npm run test:api-client` 6/6、`npm run build`通过。Vite仅保留既有chunk-size warning。
- 清洁度：`git diff --check`通过；目标源码debug／TODO／FIXME／XXX与storage／token query／console扫描为0；未读取或修改`.env`，未发送邮件，未触碰owner的日历、brainstorm与image文件。

## STEP-005 Events／monitor

- 状态：完成。
- RED：新增accountctl monitor阈值／adapter／退出码测试后，首轮因`internal/platform/authevent`不存在而setup failed；随后按固定协议补最小共享event模块、parser、evaluator与CLI挂载。
- 事件协议：七类auth event统一使用`authevent.Event`及固定attrs；HTTP补`auth.password_changed`，reset成功记录`action=reset_token`，change成功记录`action=change_password`和脱敏account ref。Resend改为result/action/failure/provider-message allowlist；legacy claim事件与业务报告拆分并强制boolean dry_run。
- Monitor：`accountctl auth monitor`默认cutover off、JSONL stdin，另支持`--file`与`--source=journald`。阈值覆盖reuse任一、5m global 19/20、same source 4/5、mail连续4/5、15m 10样本2失败／3失败、legacy dry-run与off／active severity。
- Degraded：空／告警／损坏／告警+损坏分别exit 0／1／2／3；损坏行后继续发现reuse。stderr只输出line number和`json_syntax`／`event_schema`／`journald_schema`／`input_read`固定类，不输出原始行；普通非auth journald MESSAGE被忽略。
- Event redaction：共享协议测试机械检查七类事件只出现allowlist；HTTP、Resend成功／失败与legacy producer测试分别证明完整email／IP、password、token、cookie、Authorization、provider body、subject digest和malformed canary为零。
- VERIFY：`go test -p=1 ./cmd/accountctl/... -count=1 -parallel=1`、`go test -p=1 ./internal/platform/authevent/... ./internal/platform/authmail/... -count=1 -parallel=1`、`go test -p=1 ./internal/platform/httpapi/... -count=1 -parallel=1`全部通过；monitor矩阵见`public-auth-hardening-monitor-report.md`。
- 清洁度：`git diff --check`通过；目标生产源码debug／TODO／FIXME／XXX／sleep扫描为0。一次既有Docker端口发现瞬态按attention约定包内串行重跑转绿，未跳过core测试。

## STEP-006 Preflight／rotation／rollback

- 状态：完成。
- RED／GREEN（两态闸门）：fixture先要求registration=false输出`secure-baseline-ready`、true缺证据固定失败，旧脚本仍输出`ok`／`public-auth-hardening-not-complete`；最小实现改为false安全基线、true延迟到完整evidence gate。
- RED／GREEN（enable-ready）：full matching synthetic evidence case先因helper和flags不存在而usage失败；新增独立`auth-preflight-evidence.py`后，mail／monitor／security 24h、rollback／rotation 7d、future skew 5m、revision/schema/fingerprint/status/field allowlist全部机械验证。
- RED／GREEN（live checks）：`accountctl auth readiness`测试先因probe/report不存在编译失败；新增embedded build revision attestation与只读Store probe后，当前DB、clean schema 13、limiter表/列/index、legacy cutover必须同时passed。binary显式使用当前accountctl；compose使用当前image内accountctl，不执行migration。
- Root rotation：新增synthetic old/new runner和人工runbook；证明旧access、replay与limiter namespace失败、all-family revoke能力存在且dual-key=false。真实rotation、全局session撤销、deploy、cutover和开关切换均未执行，也没有仓库自动入口。
- Rollback catalog：0013 limiter独立down后再验证0012 legacy-only down；新式账号固定阻断并保留，旧binary边界不伪造兼容。既有legacy harness新增`limiter_schema_rollback=true`。
- VERIFY：`./scripts/test-production-preflight.sh` 48 cases passed；`./scripts/test-auth-rotation-rollback.sh`四组passed；accountctl/store readiness真实PG tests通过；带40位revision的accountctl build、`bash -n`、helper入口和`git diff --check`通过。
- 文档：README改为当前注册／密码／登出状态，补production evidence与独立授权边界；`.env.example`新增非秘密secret version reference和build revision；`docs/ops/auth-root-rotation.md`固定顺序与失败恢复。
- 清洁度：证据输出扫描不含secret、DATABASE_URL、完整recipient、token、cookie、Authorization或provider body；未读取／修改`.env`，未发送邮件，未触碰owner的日历、brainstorm与image文件。

## STEP-007 Security E2E 与全域隔离

- 状态：完成。
- RED／GREEN（真实头像内容）：双账号 E2E 增加“本人头像精确字节可读”后首次返回500；根因是fixture把普通文本标为`image/png`，而下载路径会做真实`image.DecodeConfig`。最小修复仅按项目既有测试模式生成2×2合法PNG，A／B使用不同像素内容；复测`TestAccountAccessHTTPPostgresE2EAndFullIsolation`通过，生产头像逻辑未改。
- 两账号隔离：两个账号都经真实register→mail sink→verify HTTP流程成为active verified账号；A先做refresh rotation与`/me`校验。客户、订单、档期、提醒、设置与导出分别读取自身marker并拒绝对方marker／account ID；本人头像内容200且精确字节匹配，跨账号客户／头像／订单／档期／提醒访问或修改统一404。
- 后台枚举：同一E2E另创建pending注册和legacy-unclaimed账号，`AccountScopes()`只枚举两个active verified账号。Avatar maintenance、reminder scan、Telegram binding/chat resolver、delivery sender与digest snapshot六个真实消费者测试全部通过。
- 安全目录：新增`scripts/test-auth-security-catalog.sh`，用仓库pinned Go／frontend／preflight／rotation harness重放12组证据并固定输出A1～A18；临时日志退出即删除，成功输出扫描完整email、DSN、secret env、Authorization/Bearer/cookie、raw token与provider body，最终声明`synthetic=true`、`production_effect=false`。
- Makefile：`make test`聚合security catalog；catalog内部复用preflight与rotation/rollback，避免把真实deploy、migration、cutover、rotation或开关切换引入门禁。
- CMD-007窄修：`make check`首轮由`errcheck`发现monitor三处fixed diagnostic写入未处理返回值；改为同包best-effort helper显式检查写错误，保持既有degraded报告／退出码不变。窄跑accountctl全测与lint 0 issues后，完整`make check`通过。
- VERIFY：CMD-001～007全部通过；`./scripts/test-auth-security-catalog.sh`输出12组passed、A1～A18全passed；`git diff --check`通过。完整矩阵见security catalog与isolation report。
- TDD exception：security catalog／Makefile是测试编排，不改变生产行为，以脚本首轮全组实际运行和最终`make check`作为替代证据；monitor修复是lint驱动的错误处理清洁度窄修，以accountctl全测与lint作为替代证据。
- 清洁度：新增源码／脚本无debug output、临时TODO／FIXME／XXX、sleep timing、原始认证payload、production fake／memory limiter或自动production effect；未读取／修改`.env`，未发送邮件，owner任务外日历、brainstorm与`frontend/image.png`未触碰。

## 统一 review-fix

- Owner边界：只完成已经启动的一次统一review；以下修复由主agent逐finding本地closure，不再启动第二轮完整review，也不重跑整套DoD。
- REV-001 body/token成本上限：共享`bindStrictJSON`加入4 KiB `MaxBytesReader`；action token固定为32位小写hex selector、单个`.`和43位raw-base64url secret，OpenAPI／Go／TS同步76字符pattern，current password同步12～72字符。
  - RED：`TestPublicAuthRejectsOversizedBodiesBeforeApplication`得到204；`TestMalformedActionTokensDoNotReachLimiterOrRepository`的31／33位、uppercase／非hex selector及短／长secret多格得到204。
  - GREEN：上述请求全部固定400，repository／limiter／cookie零副作用；auth／httpapi目标测试和`make generate-check`通过。
- REV-003 readiness假绿：从仅检查表／列／索引名改为校验PK列序、真实window index结构，并在自动rollback的PostgreSQL transaction内实际执行insert、`ON CONFLICT`和action／dimension／digest／attempts四类CHECK负例。
  - RED：删除`auth_attempt_budgets_pkey`后`LimiterSchemaReady`仍为true。
  - GREEN：drop PK、drop attempts CHECK、同名digest索引三类损坏都返回false；正常schema probe通过且不提交synthetic row。
- REV-004 catalog假绿：每个case成功后写`mktemp` marker；Go正则先以`go test -list`校验预期测试数量；A1～A18逐条声明依赖case，缺marker立即失败；A11额外绑定已有1440×900／375×812 browser evidence。
  - RED：静态检查命中`for scenario in {1..18}`无条件passed循环。
  - GREEN：无条件循环移除；13组case、A1～A18和`production_effect=false`实际运行通过。
- REV-002 disposition：`auth.mail_delivery`只进入受信服务端日志／monitor，事件不含email、account、source或request id，也不进入HTTP；伪造missing账号的provider event会破坏真实provider健康阈值，因此不作为公开枚举blocking。低流量HTTP时间线与内部日志被同一operator掌握时仍有时序关联风险，写入review／QA residual risk。
- Important residual：`auth.password_changed`尚无transactional outbox；login／change subject reset尚未和最终业务mutation组成同一PostgreSQL transaction。两项不影响当前登录／登出／密码闭环的业务可用性，但进入QA／acceptance residual risk，不以交换调用顺序或半套outbox伪装修复。
- VERIFY：`go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1`通过；`./scripts/test-production-preflight.sh` 48 cases；`./scripts/test-auth-security-catalog.sh` 13 case与A1～A18通过；`make generate-check`、`git diff --check`通过。
- 清洁度：没有读取`.env`、发送邮件或触发任何production effect；未修改owner任务外requirements／brainstorm／image路径。

## 最后一轮本地审计

- Checklist：STEP-001～STEP-007均为`done`；`checks`继续保持`pending`，只交acceptance更新。
- 核心命令：CMD-001～CMD-007全部真实运行并通过；全仓Testcontainers按`-p=1 -parallel=1`执行，没有以skip替代核心路径。
- 前端：auth 11/11、api-client 6/6、production build通过；只保留既有Vite chunk-size warning。
- 生成与风格：OpenAPI Go／TypeScript生成零漂移，Go／前端lint通过，`git diff --check`通过。
- 实际交付闭环：注册、邮箱验证、登录、refresh rotation、logout、forgot/reset/change password、all-family revoke、限速、防枚举、事件monitor、preflight和全域账号隔离均有自动化证据。
- 范围与副作用：没有MFA／OAuth／设备管理扩展；没有读取secret、自动commit／push、deploy、production migration／rollback、root rotation、cutover或公开注册开关切换。
- 知识候选：头像下载测试若要证明可读内容，fixture必须是可被图片解码器识别的真实图片字节；仅调用`NewAvatarContent`只能证明大小与媒体类型声明合法。留给acceptance决定是否沉淀。
