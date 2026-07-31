---
doc_type: feature-acceptance
feature: 2026-07-30-public-auth-hardening
status: passed
audit_state: completed
audit_reason: ""
auditor_id: ""
acceptance_authorization_ref: "approval-report.md#goal-acceptance"
accepted: 2026-07-31
round: 1
---

# public-auth-hardening 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-31
> 关联方案 doc：`.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
> Goal 授权：`.codestable/roadmap/self-service-account-system/approval-report.md#goal-acceptance`

## 1. 接口契约核对

对照方案第 2.1 节名词层、OpenAPI、生成物与最终源码逐项核对：

- [x] `BeginPasswordReset(ctx, email, ClientMeta)`：eligible 账号创建 30 分钟 `password_reset` token并尝试投递；missing、pending、legacy/state mismatch 与 delivery failure 对外均投影为相同 `202 {"status":"accepted"}`。代码入口为 `backend/internal/platform/auth/service.go`，HTTP 薄投影为 `backend/internal/platform/httpapi/auth.go`。
- [x] `ResetPassword(ctx, rawActionToken, newPassword, ClientMeta)`：固定 76 字符 token wire grammar，先消费 limiter，再由 PostgreSQL transaction 消费 token、更新 credential、失效 reset tokens 并撤销全部 refresh family；成功才返回 204 并清 cookie。
- [x] `ChangePassword(ctx, AccountContext, currentPassword, newPassword, ClientMeta)`：客户端不提交 `account_id`；错误当前密码统一 401，成功执行同一 password/session transaction 语义并使前端回到 anonymous/login。
- [x] `AttemptLimiter.Consume/ResetSubject`：account-auth 拥有编排，production adapter 为 PostgreSQL；HTTP 只提供规范化 `ClientMeta`，没有直接访问 limiter/store。
- [x] operation 投影与设计表一致：forgot generic 202；reset invalid 统一 400；change wrong-current 统一 401；limited 统一 429 + `Retry-After`；limiter/store error fail closed 500。
- [x] OpenAPI、Go server interface、TypeScript schema 与 router 均存在 `forgotPassword`、`resetPassword`、`changePassword`，tag 保持 `public-auth-hardening`，`make generate-check` 无漂移。

流程图节点在最终代码中的落点：

- [x] exact Origin gate：`handlers.requireTrustedOrigin` 位于 reset/change、login/refresh/logout 等 cookie mutation 入口的 application 调用之前。
- [x] account-auth normalize/digest/limiter：`auth.Service.consumeAttempt` 与 PostgreSQL `Store.Consume`。
- [x] credential + token + all-family transaction：`Store.ResetPassword`、`Store.ChangePassword` 与 `applyPasswordChange`。
- [x] allowlisted events/monitor：`internal/platform/authevent` 与 `accountctl auth monitor`。
- [x] preflight/security evidence：`scripts/production-preflight.sh`、`accountctl auth readiness` 与 `scripts/test-auth-security-catalog.sh`。

结论：接口示例、名词层变化与流程图均有真实代码落点，无未处理偏差。

## 2. 行为与决策核对

### 需求摘要与关键决策

- [x] S1 / D2：forgot/reset/change、30 分钟 token、密码更新和全部 refresh family 撤销由真实 PostgreSQL application/HTTP E2E 证明；已签 access JWT 仍只保留 ADR-006 明确接受的最多 10 分钟风险窗。
- [x] S2 / D3：register/resend/login/forgot/verify/reset/change 均接入持久化 subject/source limiter；预算、half-open window、并发、重启和跨实例由 Testcontainers 证明。
- [x] S3 / D5：公开 response shape、status、`Retry-After`、dummy bcrypt、fixed response budget 与事件字段不暴露完整邮箱、IP、token、cookie或触发维度。
- [x] S4 / D7：七类事件、monitor 阈值、JSONL/journald degraded 语义和退出码 0/1/2/3 已落地并通过 fixture。
- [x] S5 / D8：production preflight 对 mail/config/proxy/cookie/limiter/cutover/evidence 任一 unknown、missing、stale 或 mismatch 均 fail closed；通过只表示 enable-ready。
- [x] S6 / D9：root rotation、migration/rollback 与 security catalog 只使用 synthetic 数据和仓库内 runner，没有真实 production effect。
- [x] S7 / ADR-001：两个 verified account 的客户、订单、档期、提醒、头像、设置、导出及后台 `AccountScopes` 消费者保持全域隔离。

### 明确不做与副作用边界

- [x] 未实现 MFA、passkey、OAuth/SSO、手机号、设备管理、邮箱换绑、账号删除或 access blacklist。
- [x] 未保存完整邮箱、原始 IP、密码、raw token、cookie、Authorization、provider body 或损坏日志原文。
- [x] 未执行 deploy、production migration/rollback、root-secret rotation、cutover、公开注册开关切换或 push。
- [x] 未以 fake/sink/in-memory limiter 作为 production readiness 证据；测试替身只存在于 deterministic tests。

### 流程级约束、挂载点与可卸载性

- [x] Origin pre-app、registration=false pre-parse/pre-limiter、limiter fail closed、credential/session transaction、事件 allowlist 和 evidence fail closed 均由测试与源码固定。
- [x] 挂载点 1（account-auth + PostgreSQL）：auth service/types、store transaction、limiter/readiness 与 migration 0013。
- [x] 挂载点 2（OpenAPI/http/router）：OpenAPI、Go/TS生成物、auth handlers、client source resolver 与 router tag。
- [x] 挂载点 3（webapp-auth/pages）：session actions、API client/transport、Forgot/Reset/Change 页面、Login/Settings/AppShell 入口。
- [x] 挂载点 4（auth-ops/events）：authevent、accountctl monitor/readiness、preflight、rotation/rollback/security catalog。
- [x] 挂载点 5（production config/README）：非秘密配置示例、server/compose wiring、README 与 root rotation runbook。
- [x] 反向核查：CodeGraph call-path 探索与定向 `rg` 未发现清单外的平行 auth store、handler 状态机、客户端 token transport或 production limiter fallback。
- [x] 拔除沙盘：逆序移除 password pages/client → HTTP/OpenAPI tag → ops/evidence → limiter/migration → account-auth password contract，可恢复到 `email-account-access` 基线；共享文件中的挂载点均已在上述五组内登记。

结论：设计决策与最终实现一致；没有用验收报告掩盖接口或编排偏差。

## 3. 验收场景核对

验证证据来源：`public-auth-hardening-qa.md`（独立只读 runner，`status: passed`）、`public-auth-hardening-evidence-pack.md`、`public-auth-hardening-dod-results.json`、`public-auth-hardening-gate-results.json`、实现期 CMD-001～CMD-007 与 review-fix 后的目标复验。

- [x] **A1**：forgot 的 eligible/missing/pending/legacy/state mismatch/provider failure 对外 202 shape/header/timing一致；只有eligible创建token并实际尝试mail。证据：QA-001、QA-004、security catalog `password-session` / `timing-enumeration`。
- [x] **A2**：reset旧token、30m边界、wrong purpose、tamper与重复消费统一400且无credential/session副作用。证据：PostgreSQL application matrix。
- [x] **A3**：reset成功原子更新bcrypt、消费/失效reset token并撤销全部refresh family；当前及其他设备旧refresh均401+clear。证据：QA runner定向闭环与catalog。
- [x] **A4**：change错误current password统一401；成功执行全family撤销并使客户端anonymous。证据：QA-001、frontend auth/API client tests。
- [x] **A5**：reset/change不可信Origin固定403且零app/limiter/cookie副作用；只有可信成功204清cookie。证据：QA-002与HTTP machine matrix。
- [x] **A6**：全部action预算、窗口、并发、重启和跨实例语义固定；source不重置，login/change仅重置subject。证据：QA-004与PG limiter tests。
- [x] **A7**：registration=false在parse/limiter/DB/mail前固定503/no-store/零消费，已有恢复入口不受阻。证据：HTTP gate tests与security catalog。
- [x] **A8**：429向上取整且至少1秒，正文/事件不泄露维度、次数或subject。证据：HTTP tests与event allowlist。
- [x] **A9**：limiter HKDF/HMAC framing、XFF-only trusted-hop算法、非法链回退与敏感信息零落盘成立。证据：crypto/proxy/limiter tests。
- [x] **A10**：login存在/不存在/错密都恰好一次production-cost bcrypt；mail分支timing统计满足设计阈值。证据：QA-004与timing report。
- [x] **A11**：OpenAPI/Go/router/TS一致；fragment首读即删、no-referrer、无Web Storage和375px/focus/keyboard状态已验证。自动测试与实现期真实浏览均通过；未保留截图artifact，列入证据质量残余风险。
- [x] **A12**：logout/reset/change成功后前端立即anonymous/login，迟到refresh不能恢复旧认证状态。证据：frontend auth 11/11与E2E logout。
- [x] **A13**：七类事件只含allowlist，敏感canary扫描为零。证据：QA-005、event-redaction catalog。
- [x] **A14**：reuse、rate-limit、mail failure和legacy claim阈值的刚低于/刚达到边界正确。证据：accountctl monitor tests。
- [x] **A15**：空/正常/损坏/告警/组合分别满足exit 0/0/2/1/3，损坏后继续且不输出原始行。证据：monitor report与catalog。
- [x] **A16**：registration=true任一production evidence缺失/错误/stale均fail closed；false只报告secure-baseline-ready。证据：48-case preflight与readiness负例。
- [x] **A17**：synthetic rotation证明旧access/refresh/replay/limiter namespace失效，且无dual-key；rollback/security catalog可重放。证据：rotation/rollback reports与catalog。
- [x] **A18**：两个verified account全域隔离，客户端不提交`account_id`；没有自动生产动作或范围外认证能力。证据：QA-008、two-account E2E、`AccountScopes` consumers与实现期`make check`。

Review/QA 重点：

- [x] review 第 5 节的完整认证闭环、body/token边界、readiness负例、catalog marker可信度及三项残余风险均已由QA覆盖。
- [x] QA无failed/blocked项；功能性核心路径有真实PostgreSQL/HTTP/frontend/script运行证据，没有把核心缺口降格为residual risk。
- [x] blocking DoD均有pass evidence；review-fix后核心package、48-case preflight、13-case/A1～A18 catalog和generate-check再次通过。
- [x] 真实Resend accepted receipt复用既有受控非生产checkpoint；本轮未重复发信，也未读取`.env`。

## 4. 术语一致性

- [x] `Account`、`Account Identity`、`Password Credential`、`Auth Action Token`、`Refresh Session Family`与`Legacy Account Claim`继续使用`requirements/CONTEXT.md`既有定义。
- [x] `AccountContext`和`AccountScope`仍以稳定`account_id`作为CRM隔离边界；客户端无`account_id`输入。
- [x] password reset/change、limiter、monitor等实现未把“客户”误称为账号，也未把账号写成禁用术语“用户”。
- [x] 新增的password/limiter/monitor术语属于本feature内部和运维协议，不改变项目级领域词汇边界。

结论：无需在本次acceptance直接改写`CONTEXT.md`。

## 5. 领域影响盘点（提示而非代写）

- [x] 新名词候选：无。账号认证核心词已在`CONTEXT.md`定义；subject/source budget与degraded monitor是实现/运维协议，不需要提升为业务领域术语。
- [x] 结构性决策候选：无新增。身份/凭证分离、短期access+rotating refresh、legacy原地认领继续由ADR-005～007约束；本feature未改变其边界。
- [x] 流程级约束候选：当前roadmap第4节和设计D1～D9已充分记录；若未来实现transactional outbox或把limiter reset并入业务事务，需要独立feature/ADR，不能在本验收中提前固化。

结论：本轮没有必须转交`cs-domain`才能完成验收的候选。

## 6. requirement delta / clarification 回写

- Requirement：`.codestable/requirements/self-service-account-system.md`，当前为`draft`。
- 本feature严格实现已批准roadmap中的第二条hardening边界，没有新增或改变用户故事、pitch、能力范围或公开契约，因此不存在新的feature-level requirement delta。
- 2026-07-30的roadmap/requirement/ADR owner批准已记录在同roadmap的`approval-report.md`；本轮不自由重写长期requirement。
- Owner明确要求`email-account-access`继续保持真实`implementing`状态，因此不能把覆盖两个child的整体requirement伪造成`current`。`draft → current`与`implemented_by`机械回写留到两个child均accepted后的roadmap final audit。
- 结论：Requirement unchanged at feature level；无新delta需要申请，不修改受保护的`requirements/VISION.md`或任务外requirement。

## 7. roadmap 回写

- [x] Design frontmatter中的`roadmap`与`roadmap_item`均为`self-service-account-system` / `public-auth-hardening`。
- [x] `.codestable/roadmap/self-service-account-system/self-service-account-system-items.yaml`中该item的feature指针唯一匹配当前目录。
- [x] `public-auth-hardening`由`in-progress`机械更新为`done`；roadmap主文档第3节对应状态由`planned`同步为`done`。
- [x] `goal-state.yaml`中仅把`public-auth-hardening`更新为`accepted`，并把`current_feature_index`指回仍待完成的`email-account-access`。
- [x] `email-account-access`继续保持`in-progress` / `implementing`；Goal顶层继续`running`，不运行最终roadmap audit、不打印Goal complete。
- [x] 该顺序依赖既有owner override `public-auth-hardening-dependency-admission`；它只允许先完成本feature，不授权伪造依赖完成或触发production动作。

## 8. attention.md 候选盘点

- [x] 本feature未暴露每个后续feature都会再次遇到的通用环境/命令陷阱；现有Docker/Testcontainers串行建议已在`attention.md`。
- 可复用但较窄的测试经验：头像下载E2E若要证明真实内容可读，fixture必须使用能被图片解码器识别的真实图片字节；仅有媒体类型和大小不足。该项保留为后续`cs-keep`候选，不在acceptance中擅自写attention。
- API/用户指南变化已经由README、OpenAPI、生成客户端与运维runbook承载；是否进一步拆分tutorial/API参考不阻塞本feature。

## 9. 遗留

后续优化与已知限制：

1. `auth.password_changed`尚无transactional outbox；数据库commit到服务端日志写入之间的极端进程崩溃窗口可能丢失审计事件。
2. login/change subject limiter reset尚未与session create/password mutation处于同一PostgreSQL transaction；正确凭证后的后置内部失败可能已经清空subject预算。
3. 同时掌握低流量HTTP时间线和内部auth日志的受信operator仍可能通过真实`auth.mail_delivery`时序推断eligible状态；必须维持严格日志访问控制。
4. timing benchmark使用injected clock/fake provider证明算法，不替代真实PostgreSQL、bcrypt、Resend网络和production runtime调度采样。
5. 浏览器已在实现期实际验证1440×900与375×812、focus/keyboard/fragment/StrictMode，但没有保留截图artifact。
6. 真实production sender/DNS、proxy拓扑、DB、artifact freshness与root rotation维护窗尚未执行；production preflight必须继续fail closed，真实动作需owner独立授权。

上述项均不承载当前注册、验证、登录、refresh、登出、找回/重置/修改密码和全family撤销的核心验收缺口；不得描述为已修复。

## 10. 最终审计

- 验证证据来源：`public-auth-hardening-qa.md`（独立runner passed）与实现/review-fix后的目标验证。
- Evidence sources：`public-auth-hardening-evidence-pack.md`、`public-auth-hardening-dod-results.json`、`public-auth-hardening-gate-results.json`、preflight/rotation/rollback/monitor/isolation/security reports。
- Goal authorization：`goal-state.yaml`与`approval-report.md#goal-acceptance`均为approved且confirmation ID一致；提交另用独立`approval-report.md#goal-commits`。
- 独立auditor判定：本feature已有独立code reviewer和独立只读QA runner；Goal gate不强制额外acceptance auditor。本轮按`AuditLocally`完成最终仓库审计，没有启动第二轮review。
- 聚合命令：
  - `python3 .../validate-yaml.py public-auth-hardening-checklist.yaml self-service-account-system-items.yaml goal-state.yaml` → acceptance写回后exit 0。
  - `make generate-check` → acceptance final audit重跑，exit 0。
  - `git diff --check` → acceptance final audit重跑，exit 0。
  - `go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1` → trust-prior-verify，review-fix后exit 0。
  - `./scripts/test-production-preflight.sh` → trust-prior-verify，review-fix后48 cases passed。
  - `./scripts/test-auth-security-catalog.sh` → trust-prior-verify，review-fix后13 cases与A1～A18 passed，`production_effect=false`。
  - 完整CMD-001～CMD-007 / `make check` → trust-prior-verify，实现结束时全部exit 0；按owner“本轮收束，不再重复耗时门禁”要求未在acceptance再次运行。
- 场景复核：re-verified 14 / trust-prior-verify 4。A1～A10、A12～A15由review-fix后独立QA/目标runner重新取得运行证据；A11浏览器肉眼、A16/A17完整production-shaped组合和A18全仓`make check`复用实现期证据。trust-prior比例超过30%的命令维度主要来自未重复跑完整DoD；核心逻辑仍有review-fix后运行证据，缺失截图artifact已明确保留。
- 交付物复核：代码、配置、migration/schema、OpenAPI/Go/TS、routes/pages、monitor/preflight/rotation/rollback、README/runbook、review/QA/evidence与roadmap机械状态均真实存在。
- 完整工作区复核：tracked/unstaged/untracked均已盘点；任务外requirements、calendar/creative-shoot/welcome-login brainstorm和`frontend/image.png`保持排除，`.env`未读取、未暂存。
- diff清洁度：无debug、临时TODO/FIXME/XXX、注释旧实现、敏感payload或production effect；checklist文字中的“TODO”只是禁止项描述。
- 知识沉淀出口：无attention/domain硬前置；真实图片fixture经验登记为可选`cs-keep`候选。
- 结论：通过。23项acceptance checks均有运行或可核验证据；没有未处理的核心缺口。顶层Goal仍因`email-account-access`未accepted而保持running。
