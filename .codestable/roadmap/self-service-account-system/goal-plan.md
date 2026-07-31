---
doc_type: roadmap-goal-plan
roadmap: self-service-account-system
status: active
created: 2026-07-31
baseline_ref: 411bbb1fc5bfc35e5617718cd8a0e5f242ad648f
---

# self-service-account-system Goal 执行计划

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/self-service-account-system/self-service-account-system-roadmap.md`
- Items: `.codestable/roadmap/self-service-account-system/self-service-account-system-items.yaml`
- Goal state: `.codestable/roadmap/self-service-account-system/goal-state.yaml`
- Runtime protocols: `goal-protocol.md`、`goal-protocol-feature-loop.md`、`goal-protocol-gates.md`、`goal-protocol-audit.md`
- Feature specs: `.codestable/roadmap/self-service-account-system/goal-features/*.md`
- Git baseline: `411bbb1fc5bfc35e5617718cd8a0e5f242ad648f`
- Workspace: owner 已选择当前 branch `feature/userCenter`；不创建 worktree 或新 branch
- Content confirmation: roadmap 已确认；两份 child design 已统一批准；对应 design-review 均为 passed
- Execution authorization: approved；driver 已派发，acceptance 与 scoped commit 仍分别机械核验各自 ApprovalRef

## 2. Feature Order

| # | Feature | 性质 | 一句话交付物 |
|---:|---|---|---|
| 1 | email-account-access | mixed | 邮箱注册验证、10m access + rotating refresh、全 Web 请求恢复、legacy 原地认领与 zero-account bootstrap |
| 2 | public-auth-hardening | mixed | 密码恢复/修改、全 refresh family 撤销、持久化限速、防枚举、monitor 与 production enable-ready preflight |

拓扑顺序由 items.yaml 固定。第二条 implementation 只有在第一条 item 严格 `done` 后才可进入；design-review passed 或 `dropped` 不满足 implementation dependency。

## 3. Roadmap Core Acceptance Paths

### 3.1 新账号访问主链

在真实 PostgreSQL、deterministic clock 与真实事务邮件 adapter 的受控非生产环境中执行：

1. capabilities=true → register generic 202；
2. 收到 verification mail accepted receipt，浏览器从 fragment 消费 token；
3. verify 后取得 10m access + HttpOnly refresh，`/me` 返回稳定 account ID/email/timezone；
4. reload 通过 refresh restore，JSON/multipart/avatar/export 的 auth 401 最多 single-flight refresh + replay 一次；
5. rotation 在 10s grace 返回同一 successor，grace 外 reuse 撤销 family；
6. logout 对 missing/expired/revoked/valid 均按契约清 cookie，旧 refresh 固定失败。

### 3.2 Legacy 与首账号运维路径

- Synthetic full legacy fixture：claim dry-run 零写零邮件；正式认领后 account ID、客户/订单/档期/提醒/头像/设置/导出 counts 与 checksum 不变。
- Zero-account fixture：bootstrap dry-run 只读脱敏；正式执行复用 atomic admission guard；bootstrap-vs-bootstrap 与 public-vs-bootstrap 双向 barrier 只有合法线性化结果。
- Repo-contained rollback rehearsal：legacy-only down 成功，新式账号 fail closed，旧 binary 降级边界写入脱敏报告。

### 3.3 Password、限速与防枚举路径

- Forgot 对 active/missing/pending/legacy/provider failure 保持相同公开 202 shape/header/timing；30m reset token 单次、purpose 隔离。
- Reset/change 成功在同一事务更新 credential、失效 reset token并撤销账号全部 refresh family；所有设备旧 refresh 失败，access residual 不超过 10m。
- PostgreSQL subject/source limiter覆盖 login/change、register/resend/forgot、verify/reset 的预算、Retry-After、并发、窗口、restart与multi-instance。
- Dummy bcrypt、mail/login/token response floor+jitter与20 warmup/200交错样本的median/p95/ratio阈值通过。

### 3.4 Monitor 与 production readiness

- JSONL/journald fixture覆盖 refresh reuse、rate limit、mail failure、legacy dry-run/cutover mode与exit 0/1/2/3；损坏行继续扫描且不输出原文。
- Production preflight对真实mail receipt、secret/issuer/KDF、HTTPS base、proxy、cookie、limiter、legacy cutover、security/rollback evidence逐项fail closed；全部匹配revision/schema/config fingerprint和freshness才enable-ready。
- Root rotation只做synthetic rehearsal/runbook；不自动切换registration、deploy、migration、rotation或cutover。

### 3.5 两账号隔离

用两个真实verified account重跑客户、订单、档期、提醒、头像、设置、导出与后台`AccountScopes()`消费者；A token对B数据不可见，客户端不提交account_id。

## 4. Key Assumptions

1. 同源Web SPA仍是首期唯一客户端；Origin/cookie契约不为原生或跨源客户端放宽。
2. 真实邮件provider能在受控环境返回accepted receipt，且hardening阶段能满足≤900ms total deadline；不满足时必须handoff或回design。
3. Docker/Testcontainers、Go、Node与浏览器runner在当前workspace可用；core环境缺失不能用静态grep替代。
4. 当前`feature/userCenter`分支的related planning/spec变更属于本Goal；任务外creative/welcome brainstorm与`frontend/image.png`必须保护。
5. Production DNS、credential、owner邮箱、deploy/cutover/rotation均需owner独立授权，不由Goal execution授权覆盖。

## 5. Top 3 Risks And Mitigations

1. **Refresh/改密竞态留下可用长期会话。**
   - 缓解：真实PG行锁/transaction、deterministic clock、grace/reuse/all-family revoke矩阵与多设备证据；review/QA不得把核心缺口降为residual。
2. **Generic outcome仍经timing、limiter或事件泄露账号。**
   - 缓解：固定digest/proxy/预算/Retry-After、dummy bcrypt、floor+jitter、统计阈值和event allowlist；security catalog重放。
3. **Dirty current branch导致任务外文件被提交或feature commit不可复核。**
   - 缓解：Goal启动后在写代码前核对三条protected path checksum，创建仅包含这些path的local stash；每个feature scoped commit禁止包含它们，最终audit后或任何handoff前恢复stash。stash失败/冲突必须handoff，不能丢弃用户文件。

Protected baseline checksums：

| Path | SHA-256 aggregate |
|---|---|
| `.codestable/brainstorms/creative-shoot-planning/` | `7d7d8b90c1e5b5d7599eeb4fb7e1b2df2bc5cb0926664bfba829916c3bfa1d01` |
| `.codestable/brainstorms/welcome-login-design/` | `035445109a50ca2c8503a53968d20b69ec4d0fa317a67c8efb1d7d791786b0fd` |
| `frontend/image.png` | `263514ca4ccd1512e7c2e2d98d2ba25414e89429cc59553aba784d6e4ed85d0c` |

## 6. Mandatory Validation Commands

### 6.1 email-account-access

```bash
cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... ./cmd/accountctl/... -count=1 -parallel=1
cd frontend && npm run test:auth && npm run test:api-client && npm run build
make generate-check
./scripts/test-auth-legacy-cutover.sh
./scripts/test-production-preflight.sh
make check
```

### 6.2 public-auth-hardening

```bash
cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1
cd backend && go test -p=1 ./cmd/accountctl/... -count=1 -parallel=1
cd frontend && npm run test:auth && npm run test:api-client && npm run build
make generate-check
./scripts/test-production-preflight.sh
./scripts/test-auth-security-catalog.sh
make check
```

## 7. Final Aggregate Commands

Roadmap完成前去重重跑：

```bash
make check
make generate-check
cd backend && go test ./... -count=1 -parallel=1
cd frontend && npm run lint && npm run test:auth && npm run test:api-client && npm run test:prototype && npm run build
./scripts/test-auth-legacy-cutover.sh
./scripts/test-production-preflight.sh
./scripts/test-auth-security-catalog.sh
git diff --check
python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/self-service-account-system
```

功能性核心命令不得因耗时跳过。外部provider/browser/Docker不可用且影响core path时必须needs-human/handoff；只有非核心命令可带明确理由trust-prior。

## 8. Preflight Strategy

1. 每次恢复先运行`codestable-workflow-next.py epic --roadmap ... --json`，以repo facts决定next stage。
2. Goal execution批准后先确认仍在`feature/userCenter`；若branch变化则handoff，不自行切换。
3. 写业务代码前核对protected paths checksum并创建path-scoped local stash，记录stash ref到goal-state；不stash related self-service artifacts。最终audit后或handoff前恢复并复核checksum。
4. 每个feature进入前运行feature workflow hook的`--require-implementation-ready`；第二条依赖第一条严格done。
5. 记录baseline/branch/tracked/staged/unstaged/untracked；禁止destructive git命令，不使用`--no-verify`。
6. 检查Go/Node/Docker/Testcontainers/browser与CodeStable gate scripts；缺runner按Missing Tool Recovery处理。
7. 真实凭证只经owner-controlled环境变量/secret reference注入，不写仓库、日志、JSON或截图。

## 9. Missing Tool Recovery

- 只能安装/修复真实测试依赖、lockfile、既有runner配置或更新/重装CodeStable skill包。
- 禁止新增`go`、`docker`、`jest`、`pytest.py`等同名shim，禁止空脚本、跳过测试或伪造JSON。
- Core环境缺失必须needs-human/handoff；不能用静态grep冒充运行证据。
- Provider unavailable记录fallback，不自动阻塞基础流程；但核心provider warning必须由review/QA/audit解释。

## 10. DoD Policy

- Design：approved design + passed design-review + parseable checklist + Acceptance Coverage Matrix + DoD Contract。
- Implementation：当前feature全部steps done；scope-gate、dod-runner、evidence-pack passed；core命令有真实日志；无未解释范围外diff。
- Review：独立Task agent review passed、无unresolved blocking、消费evidence/gates并给QA focus。
- QA：passed；覆盖关键场景、DoD、review focus与真实API/browser/CLI/PG core paths，不把核心缺口降为residual。
- Acceptance：`goal-acceptance`命名授权有效；checks全passed；canonical artifacts/gates/results完整；items/roadmap回写done。
- Scoped commit：`goal-commits`命名授权有效；只包含当前feature代码/spec/evidence/review/QA/acceptance、shared roadmap/goal-state更新；不得包含protected paths，不得push。
- Final audit：全部features accepted、items terminal、聚合命令/core paths通过、授权有效、goal consistency与goal audit gates passed。

## 11. Gate Policy

- Runtime权威：`.codestable/roadmap/self-service-account-system/goal-protocol-gates.md`。
- implementation.before_review：scope-gate + dod-runner + evidence-pack。
- review.before_pass：review-evidence-gate。
- qa.before_acceptance：qa-evidence-gate。
- acceptance.before_done：acceptance-dod-gate。
- roadmap_audit.before_complete：goal-consistency-gate + goal-audit-gate。
- `protocol-only`由对应stage消费evidence；不得当成缺少executable脚本。
- 同一blocking三轮仍失败时持久化handoff，不循环伪修。

## 12. Provider Policy

- archguard/meta-cc unavailable记录为provider unavailable/fallback，不自动阻塞。
- provider warning必须由独立review、QA或final audit解释、修复或提升为blocking。
- meta-cc首批只消费已有摘要；不得为满足provider字段伪造输出。
- 最终审计聚合goal-evidence-summary、provider warnings、E/C/H summary与H-only core checks；未批准的H-only core check必须handoff。

## 13. Final Audit Deliverables

最终审计至少核验：

- roadmap/items/goal-state/approval-report的状态、identity、两份授权与feature双射；
- 每个feature的approved design、checklist、review、QA、acceptance、evidence pack/results、scope/DoD/gate JSON；
- 新账号主链、legacy/bootstrap、password/all-family revoke、limiter/timing、monitor/preflight、root/rollback和two-account isolation；
- requirement/CONTEXT/ADR/roadmap/README writeback或明确N/A；
- protected local stash已恢复且checksum匹配；tracked/staged/unstaged/untracked、debug/TODO/FIXME/XXX、临时shim/package/`__pycache__`与runtime residual；
- `.codestable/roadmap/self-service-account-system/goal-audit.md`及goal-evidence-summary；
- 必跑`codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/self-service-account-system`。

## 14. Goal Authorization Policy

- `goal-acceptance`：允许driver在implementation、独立review、QA、DoD/gates与真实证据全部通过后，以`ResumeGoalAcceptance approval-report.md#goal-acceptance`完成每个feature acceptance；当前approved。
- `goal-commits`：允许每个feature accepted且状态已持久化后自动创建一次scoped commit；当前approved。
- 两项必须在`approval_groups.goal-execution`中由同一次owner answer原子批准，使用同一非空confirmation ID；不能只批准一项。
- Goal在当前branch执行时，还会按第8节创建/恢复仅含三条protected path的local stash；该动作不stage、不commit、不push，任何失败立即handoff。
- Approval refs固定为`approval-report.md#goal-acceptance`与`approval-report.md#goal-commits`，互不替代。
- Scoped commit不授权remote push、PR、merge、publish、release、deploy、promotion、production migration/rotation/cutover或registration开关变更。
