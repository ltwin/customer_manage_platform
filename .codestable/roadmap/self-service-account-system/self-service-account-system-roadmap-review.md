---
doc_type: roadmap-review
roadmap: self-service-account-system
status: passed
review_state: passed
review_reason: ""
reviewer_id: /root/auth_roadmap_reviewer
reviewed: 2026-07-30
round: 3
---

# self-service-account-system roadmap 审查报告

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/self-service-account-system/self-service-account-system-roadmap.md`
- Items: `.codestable/roadmap/self-service-account-system/self-service-account-system-items.yaml`
- Related docs: brainstorm／approval、auth-hardening issue、CONTEXT、ADR-001/002/003、OpenAPI feature tag slicing compound、platform-skeleton auth design
- Code facts checked: `backend/oapi-codegen.yaml`、auth/httpapi/store/server、frontend auth/API client/cache、Makefile/OpenAPI

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: `/root/auth_roadmap_reviewer`
- Round 1: `changes-requested`，发现 1 blocking + 6 important；两条粗粒度 feature 形态本身通过
- Round 2: round 1 七项均 resolved；新增 1 important `R2-001`（公开注册开关缺运行时 HTTP/UI 契约）
- Round 3: `passed`；`R2-001` 与 monitor 损坏行 advisory 闭合，无新 blocking／important／advisory
- Merge policy: 主 agent 逐项用 roadmap、items、现有 codegen 约定与 reviewer evidence 复核；未把 reviewer 偏好当作 owner 决策
- File policy: reviewer 全程只读，未修改文件

## 2. Roadmap Summary

- Goal completion signal: 公开邮箱注册、验证准入、access/refresh、恢复／改密／限速、旧账号原地认领与上线监控完成，旧认证 residual 退役
- Module split: account-auth deep module + auth-http／auth-mail／auth-ops adapters + webapp-auth state machine
- Interface contracts: identity/schema、应用接口、action token/mail、HTTP/OpenAPI、session rotation、前端状态、limiter、legacy cutover、AccountScope、config/release gate、observability
- Items: 仅 2 条粗粒度全栈 feature；`email-account-access` 为测试／封闭环境 minimal loop，`public-auth-hardening` 为公开上线安全闭环
- Dependency shape: `email-account-access → public-auth-hardening`；DAG 无环，恰一 `minimal_loop: true`
- Efficiency boundary: schema、mail、OpenAPI、backend、frontend、migration、tests 与六个执行 waves 均为 feature 内部 checklist，不提升成独立 roadmap item

## 3. Findings Closure

### Round 1

- [x] **RMR-001 · blocking**：邮件投递失败契约统一为 public generic 202；delivery failure 仅作为内部 outcome，可信 legacy CLI 可显式失败。
- [x] **RMR-002 · important**：新增 operation/tag/owner matrix；TypeScript 全量、Go 按 feature tag 切片，item1 期间 item2 endpoint 保持 404 且无 stub／501。
- [x] **RMR-003 · important**：item1 明确只可在测试／封闭环境验收；production preflight 在 hardening 前拒绝开启公开注册。
- [x] **RMR-004 · important**：account-auth 只提供 `InspectLegacyState`；auth-ops 组合 DB、route/OpenAPI、旧 JWT 与配置检查生成 cutover report。
- [x] **RMR-005 · important**：定义 dispatch、delivery、legacy claim/state 与 cutover check/report 的最小跨模块 shape 和敏感字段边界。
- [x] **RMR-006 · important**：固定认证事件、允许／禁止字段、阈值、monitor 命令、redaction 与验收入口；未新增 security-center feature。
- [x] **RMR-007 · important**：ADR／CONTEXT 时点前移到 roadmap owner 批准后、首个 child implementation 前。

### Round 2

- [x] **R2-001 · important**：`AUTH_PUBLIC_REGISTRATION_ENABLED=false` 形成 backend-authoritative runtime contract：
  - `GET /auth/capabilities` 返回唯一公开 capability；
  - `POST /auth/register` 对任意 body 固定 `503 registration_disabled`，在解析／存储／limiter／mail 前返回且零副作用；
  - WelcomePage CTA 与 `/register` 在 capability unknown／failure／false 时 fail closed；
  - resend／verify／login／refresh／logout／`/me`／密码流程／legacy CLI 不受该开关影响；
  - `getAuthCapabilities` 归 item1 tag owner；Goal Coverage Matrix 含 API/UI negative evidence。

### Round 3 Regression

- RMR-001～007：全部 pass，无回归。
- `R2-001`：resolved。
- Monitor 损坏行：固定“隔离后继续扫描 + degraded”、redacted stderr 与 `0/1/2/3` 退出码，resolved。
- 新 blocking：none。
- 新 important：none。
- 新 advisory：none。

## 4. Granularity And Execution Review

- 两条 feature 都有独立、可证伪的业务结果：
  - `email-account-access`：注册→验证→session→`/me`→refresh→logout + legacy 原地认领；只允许测试／封闭环境闭环。
  - `public-auth-hardening`：找回／重置／改密、全 session 撤销、限速／防枚举、monitor、安全回归与 production preflight；完成后才具备公开启用条件。
- 六个 waves 没有独立 slug、depends_on、status、feature 或 minimal_loop，不构成第二层 roadmap DAG。
- OpenAPI／codegen 按 operation tag 切片，避免粗粒度 feature 因未实现端点而无法编译；内部 checklist 保留执行顺序，避免再拆小 feature 带来的协调成本。
- YAML 已成功校验；无未知依赖、自指、循环或 slug 冲突。

## 5. User Review Focus

- 是否批准只保留上述 2 条粗粒度 feature，并把技术层／执行 waves 留在各 feature 内部。
- 是否接受 item1 可工程验收但不能公开生产启用，必须等待 item2 安全闭环。
- 是否接受 roadmap 批准后、首个 child implementation 前先用 `cs-domain` 固化身份、session 与 legacy claim ADR／CONTEXT。
- 事务邮件 provider、发件域名/DNS 与凭证仍未选择；真实 adapter 与送达证据是 item1 acceptance 的外部 checkpoint。

## 6. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---:|---|---|
| Granularity Gate | pass | E | 2 个业务闭环；六 waves 明确只为内部 checklist | none |
| DAG and minimal loop | pass | E | 两节点单向依赖、无环、恰一 minimal loop | none |
| Goal Coverage Matrix | pass | E | 所有 core goal 均有 item、验证入口与 evidence type | acceptance 产出真实证据 |
| Generic 202 anti-enumeration | pass | E | register/resend/forgot 对账号态与 delivery failure 保持统一 public outcome | implementation tests |
| Disabled registration runtime | pass | E | capability、固定 503、零副作用、UI fail-closed 与 regression evidence 均明确 | implementation tests + screenshot |
| OpenAPI/codegen ownership | pass | E+C | operation/tag matrix 与现有 include-tags 约定一致 | `make generate-check` |
| Interface contract usability | pass | E+C | application result、HTTP、session、cutover、config 与 monitor 契约可执行 | child design 细化内部实现 |
| Module interface depth | pass | E+C | account-auth 深模块与 HTTP/mail/ops adapters ownership 清晰 | none |
| Legacy in-place claim safety | pass | E+C | stable account ID、dry-run、synthetic counts/avatar 与 composite cutover report | migration/rollback evidence |
| Observability | pass | E+C | 固定事件、redaction、thresholds、parser 策略和退出码 | fixture tests |
| ADR timing | pass | E+C | owner 批准后、首个 implementation 前写 ADR/CONTEXT | workflow admission gate |
| Validation strategy | pass | E | Testcontainers、generate-check、browser、preflight、monitor、migration/rollback 均有入口 | feature QA/acceptance |

Summary: 所有核心检查均有 E 或 E+C 证据；H-only core checks=none。

## 7. Residual Risk

- 真实邮件 provider、域名/DNS、provider 凭证和真实送达尚不能由 planning review 证明；item1 acceptance 必须通过 true-external checkpoint。
- 实际 production deployment/cutover、owner 邮箱绑定与 root secret rotation 仍需 owner 单独授权。
- 当前工作树有 owner 的欢迎页／登录页未提交改动；未来实现必须先选择当前 branch 或新 worktree，并保留这些变更。

## 8. Verdict

- Status: passed
- Review gate: cleared
- Next: 写 roadmap canonical approval report，进入 owner 人工 review；owner 批准前 roadmap 保持 `draft`，不启动 child design／implementation
