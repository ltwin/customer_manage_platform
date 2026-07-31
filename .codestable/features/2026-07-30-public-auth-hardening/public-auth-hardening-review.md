---
doc_type: feature-review
feature: 2026-07-30-public-auth-hardening
status: passed
reviewer: subagent
reviewed: 2026-07-31
round: 1
lane_a_state: completed
lane_a_ref: "/root/final_public_auth_review"
lane_a_reason: "独立reviewer完成一次统一收尾审查；结果已逐条按当前仓库事实核验"
lane_b_state: skipped
lane_b_ref: ""
lane_b_reason: "skipped-scope-ambiguous: workspace包含owner任务外dirty paths，按协议改做scope-gate changed_files本地行级审查"
---

# public-auth-hardening 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
- Checklist: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml`
- Evidence pack: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-evidence-pack.md`
- Gate results: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-gate-results.json`
- DoD results: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-dod-results.json`
- Implementation evidence: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-implementation.md`
- Diff basis: 当前未提交workspace diff；实现范围以scope-gate的`changed_files`为准，review-fix增量由主agent逐hunk归因。
- Review mode: initial + owner-directed local closure。
- Baseline dirty files: `.codestable/requirements/VISION.md`、`.codestable/requirements/schedule-calendar.md`、三个任务外brainstorm目录与`frontend/image.png`由owner持有，排除在本轮审查外。

### Independent Review

- Detection: 独立隔离Task agent可用且已完成；OCR CLI可连接，但workspace含owner任务外dirty paths，禁止裸workspace扫描。
- 环节 A 独立隔离 Task agent: independent-agent + completed，ref=`/root/final_public_auth_review`。
- 环节 B OCR CLI: skipped-scope-ambiguous；主agent改做scope-gate `changed_files`本地行级审查。
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded。
- Merge policy: 独立reviewer的每条finding均以当前源码、可达路径和实际契约核验后合并；没有把未证实猜测直接升级为blocking。
- Gate effect: 无；三个成立的blocking已定点修复并通过目标验证，一个finding经本地事实核验不构成公开枚举阻塞。
- Owner constraint: owner明确要求本轮是唯一一次统一review；修复后不再启动第二轮完整review。由于本轮修复包含生产行为，标准协议原本会要求完整复审；本报告如实记录为owner-directed local closure，不伪造第二个reviewer。

## 2. Diff Summary

- 新增：password hardening、auth monitor/readiness、limiter migration、preflight/rotation/security catalog、密码页面与证据文件。
- 修改：account-auth/store/HTTP/OpenAPI/frontend auth state、配置、Makefile与运维入口；review-fix另收紧认证body/token grammar、readiness live probe与A1～A18映射。
- 删除：none。
- 未跟踪 / staged：本feature新增文件均未跟踪；staged diff为空。
- 风险热点：认证安全、持久化迁移、跨账号隔离、Origin/cookie、timing、日志脱敏、发布闸门、前端会话时序与证据可信度。

## 3. Adversarial Pass

- 假设的生产 bug：未认证请求用超大body／selector耗尽资源；残缺limiter schema被readiness误判；catalog在具体场景未运行时仍输出passed；数据库commit与事件／limiter reset的顺序出现crash gap。
- 主动攻击过的反例：8 KiB尾随body、31/33位与非小写hex selector、42/44位secret、drop limiter PK、drop attempts CHECK、同名错误索引、测试正则空匹配、账号状态与mail event观察边界、session-create／password transaction后置失败。
- 结果：前三类升级为blocking并已修复；mail event观察边界不构成公开可利用枚举路径但保留residual risk；事件持久化与subject reset顺序保留为important residual risk。

## 4. Findings

### blocking

none。原独立review的四项处置如下：

- REV-001 公开认证body/token无界：成立，已关闭。
- REV-002 mail event数量依赖eligible状态：不作为blocking。`DeliveryOutcome`与`auth.mail_delivery`只进入受信服务端日志／monitor，不序列化到HTTP；事件无email、account、source、request id，外部请求者没有观察面。真实provider事件必须只表示真实投递，若为missing账号伪造provider事件会破坏D7健康阈值。低流量下同时掌握HTTP时间线与内部日志的受信operator仍可能做时间关联，列入Residual Risk并要求日志访问控制。
- REV-003 limiter readiness可在缺主键时假绿：成立，已关闭。
- REV-004 A1～A18无条件打印passed：成立，已关闭。

### important

- [~] REV-005 `backend/internal/platform/store/auth_account.go:157`、`backend/internal/platform/httpapi/auth.go:238` password transaction commit后才写`auth.password_changed`，进程在commit→log之间崩溃时可能永久缺审计事件。
  - Evidence: credential/token/all-family事务先commit；HTTP随后写slog，当前没有transactional outbox。
  - Impact: 不影响账号注册、登录、登出、改密和session撤销的业务正确性，但会降低极端进程崩溃下的安全审计完整性。
  - Disposition: 本feature不临时拼装半套outbox；按owner“先完成闭环且不再重复review”指令进入residual risk，后续应独立设计持久化outbox与可靠publisher。
- [~] REV-006 `backend/internal/platform/auth/service.go:363`、`backend/internal/platform/auth/service.go:426` login/change在最终业务commit前重置subject bucket，后续session create／password transaction失败时预算可能已被清空。
  - Evidence: 当前测试明确固定reset→commit顺序，用于避免“业务已commit但limiter reset失败导致客户端误认为失败”的反向不一致。
  - Impact: 需要已经掌握正确密码且同时命中后续内部错误／并发loser，不能绕过密码校验；仍会造成limiter状态与客户端结果不完全一致。
  - Disposition: 保留为residual risk；要完全修复需把limiter reset与session/password mutation交给同一PostgreSQL transaction owner，不能简单交换两行调用顺序。

### nit

none。

### suggestion

none。

### learning

- `Origin`不是认证边界；公开认证入口仍需独立限制request body与token wire grammar。
- readiness必须验证生产SQL真正依赖的主键、约束和冲突更新，而不是只比对表／列／索引名称。
- 安全catalog的场景passed必须由显式case依赖产生，且测试正则必须拒绝空匹配。

### praise

- PostgreSQL limiter以固定subject→source顺序原子消费，half-open窗口、跨实例和并发语义清晰。
- credential更新、reset token清理和全部refresh family撤销由同一事务收口；Origin/cookie错误矩阵和两账号全域隔离证据完整。
- XFF默认不信任direct peer，非法链整体回退；frontend change password使用one-shot authorized request避免401 refresh后重放credential mutation。

## 5. Test And QA Focus

- QA 必须重点复核：注册→验证→登录→refresh rotation/replay→logout→forgot→reset→全部设备refresh 401/clear→新密码登录→change→全部设备退出的完整闭环；两个verified账号全域隔离保持不变。
- Request/token边界：超大body、31/33位／非小写hex selector、短／长secret、多dot必须固定4xx且limiter/repository零调用。
- Readiness负例：drop PK、缺CHECK、同名错误索引、dirty schema均不得enable-ready；正常探针事务不得留下synthetic row。
- Catalog可信度：删除／改名一个绑定测试时对应case必须因匹配数量不符失败；缺任一case marker时对应A场景不得输出passed。
- Residual risk复核：session create／change repository失败后的subject budget；commit后进程终止时`password_changed`事件缺口；mail event／HTTP时间线的内部访问控制。
- Evidence pack warning：scope gate命中checklist规范文字中的`TODO`，不是源码临时TODO；已解释，不阻塞。
- 不能靠review完全确认：真实Resend receipt／DNS、production proxy拓扑、production DB、artifact freshness与真实root rotation维护窗。

## 6. Residual Risk

- `auth.password_changed`尚无transactional outbox，极端commit→log崩溃窗口可能丢事件。
- login/change subject reset尚未与最终业务mutation组成同一数据库事务；正确凭证后的内部失败可能清空subject预算。
- 同时拥有低流量HTTP时间线与内部auth日志的受信operator可能通过真实mail event时序推断eligible状态；事件本身不含subject/email/request id，需继续限制日志权限与关联能力。
- timing benchmark使用injected clock／fake provider证明算法，不替代真实PostgreSQL、bcrypt、Resend网络和runtime调度的生产采样。
- 浏览器证据记录在implementation report，未保留可复核截图artifact；QA需重放375px、键盘/focus、fragment history与StrictMode场景。

## 7. Verdict

- Status: passed。
- Next: 按Goal lane进入`cs-feat` QA；不启动第二轮完整review。若QA暴露真实业务失败，只做对应QA-fix并如实记录owner对重复review的限制。

## 8. Focused Closure

- Closed findings: REV-001、REV-003、REV-004；REV-002经本地信任边界核验不成立为blocking。
- REV-001 attributed delta: `backend/internal/platform/httpapi/auth.go`共享4 KiB `MaxBytesReader`；`backend/internal/platform/auth/crypto.go`固定76字符wire grammar；`api/openapi.yaml`及Go／TS生成物同步长度／pattern；`auth_test.go`新增body/token边界测试。
  - RED: 超大尾随body与31/33位、uppercase／非hex selector当前均返回204并进入业务。
  - GREEN: 目标测试固定400且repository／limiter／cookie零副作用；`make generate-check`通过。
- REV-003 attributed delta: `backend/internal/platform/store/auth_readiness.go`校验PK／实际window index，并在自动rollback事务内探测insert、`ON CONFLICT`与action/dimension/digest/attempts四类CHECK；`auth_limiter_test.go`新增drop-PK、drop-CHECK和同名错误索引负例。
  - RED: drop PK后`LimiterSchemaReady=true`。
  - GREEN: 三类损坏均false；正常schema通过且探针事务回滚。
- REV-004 attributed delta: `scripts/test-auth-security-catalog.sh`为每个case写临时成功marker、校验Go测试匹配数量，并用18条显式`require_scenario`映射生成A1～A18；A11另绑定已有browser evidence。
  - RED: 静态检查命中`for scenario in {1..18}`无条件循环。
  - GREEN: unconditional loop消失；13个case、A1～A18及`production_effect=false`实际通过。
- Targeted verification:
  - `go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1`：passed。
  - `./scripts/test-production-preflight.sh`：48 cases passed。
  - `./scripts/test-auth-security-catalog.sh`：13 cases、A1～A18 passed，production_effect=false。
  - `make generate-check`、`git diff --check`：passed。
- Classification: 修复包含安全生产行为，按标准协议不属于普通focused-closure；owner明确要求唯一一次review后不再启动下一轮，因此由主agent做逐finding本地closure并保留首轮`reviewer: subagent`锚点。本报告没有声称发生第二次独立review。
