---
doc_type: feature-design-review
feature: 2026-07-30-email-account-access
status: passed
review_state: passed
review_reason: owner-authorized-no-further-review-round
reviewer_id: /root/round3_email_account_access
reviewed: 2026-07-30
round: 3
---

# email-account-access feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-30-email-account-access/email-account-access-design.md`
- Checklist: `.codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml`
- Roadmap: `.codestable/roadmap/self-service-account-system/self-service-account-system-roadmap.md`、`.codestable/roadmap/self-service-account-system/self-service-account-system-items.yaml`
- Related docs: self-service requirement、CONTEXT、ADR-005～007
- Code facts checked: auth/store/httpapi/server、migration、OpenAPI/codegen、frontend auth/client/pages/tests、Makefile

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: `/root/round3_email_account_access`
- Raw output: 第三轮独立 reviewer 回传 1 条 blocking、4 条 important、1 条 nit，并判定修订原本需要完整复审；reviewer 只读，没有写仓库文件。
- VerifiedNoWrite: reviewer 启动前后全工作树内容摘要均为 `7e1cf822c2e83fead915d1d4ea4e7054c470ea0c6218a1977c84224afef73012`。
- Merge policy: 主 agent 逐条核验后确认全部有效，并同步修订 design/checklist。
- Owner override: owner 明确要求“不要一直做 review，设计阶段时间太长”。因此本轮修订后不再启动 round 4；改做逐 finding focused closure、YAML/追踪/占位符/diff 机械校验。该裁剪只结束设计审查循环，不自动批准 design，也不取消 Epic 两份 child design 的统一人工确认。

### Review History

| Round | Reviewer | Result | Closure |
|---|---|---|---|
| 1 | `/root/review_email_account_access` | changes-requested | 补 CurrentAccount seam、typed error、TTL、rollback、mail checkpoint、move-only gate 与追踪 |
| 2 | `/root/rereview_email_account_access` | changes-requested | 补原子 admission、outcome/error、KDF/AEAD、durable checkpoint、logout/Origin、rollback harness、旧 login shape 与 no-store |
| 3 | `/root/round3_email_account_access` | changes-requested → owner-authorized closure | 一次性修完 dry-run seam、混合 admission、错误优先级、KDF/envelope/AAD/sweep、cookie absolute expiry 与 A11 trace；按 owner 指令不再启动 round 4 |

## 2. Design Summary

- Goal: 在测试／封闭环境交付邮箱注册验证、短期 access + 可轮换 refresh、全 Web 请求恢复、legacy 原地认领和零账号 bootstrap；production 注册继续锁闭。
- Key contracts: Account/Identity/Credential 分离、CurrentAccount(AccountContext)、PlanBootstrap 只读预检、全 account creation 共用 PG admission guard、10m access、14d idle、30d absolute、10s replay、固定 HKDF/AES-GCM 字节协议、true-external mail、内存 AuthState、item2 404。
- Steps: 8 个，全部 pending；没有提前进入 implementation。
- Checks: 25 个，全部 pending；A1～A18 与 S1～S6 均有 step/check/command evidence。
- Baseline / validation: 6 条最终态 core commands；feature-created 命令与当前 baseline 已区分。

## 3. Findings

### blocking

- [x] FDR-R3-B1 — bootstrap dry-run 缺少可实现的 application seam。
  - Closure: 新增受信只读 `PlanBootstrap(ctx,email,password)`；复用正式规范化、验证和 eligibility，返回脱敏快照，零写／零 token／零邮件；正式操作仍用原签名 `Register(bootstrap_first_account)` 并在 guard 内重查。已进入 D2、interface、A14、STEP-003/006、CHK-002/017、CMD-001/004。

### important

- [x] FDR-R3-I1 — 验收只证明 bootstrap-vs-bootstrap，不能证明 public creation 共用 guard。
  - Closure: 增加 public 持 guard → bootstrap wait/loser，以及 bootstrap 通过零账号检查 → public wait 的双向 PostgreSQL barrier；两端都穿 application interface，进入流程约束、A14、STEP-003、CHK-017、CMD-001。
- [x] FDR-R3-I2 — Register/Resend Attempted 状态与 context error 分类存在歧义。
  - Closure: outcome 表按 operation/state 拆行；Register 新邮箱与 Resend pending 新 token 后为 Attempted=true，冲突/no-target/non-pending 为 false；分类顺序固定 `errors.Is(cancel/deadline)` → typed class → temporary fallback，并要求每格断言 result/error/HTTP/CLI/event。
- [x] FDR-R3-I3 — KDF/AEAD 字节协议与无流量 cleanup owner 不唯一。
  - Closure: 固定 HKDF IKM/salt/info/output、`0x01|nonce[12]|ciphertext+tag`、versioned length-prefixed AAD 和 UTC epoch seconds；parse/tamper/wrong-key 固定 401 + 同事务 revoke；新增 account-auth `SweepExpiredReplayCiphertexts`，由有界 maintenance cadence 在无请求时清空物理列。
- [x] FDR-R3-I4 — refresh cookie absolute-expiry 与 clear 语义没有机械验收。
  - Closure: verify/login/refresh 的 Max-Age/Expires 不晚于 `RefreshExpiresAt` 和 family absolute deadline；接近 deadline 不延长；logout/invalid refresh 固定 Max-Age=0 + past Expires；进入 Origin/cookie matrix、A8/A9、CHK-009、STEP-003/006、CMD-001 与 production preflight。

### nit

- [x] FDR-R3-N1 — A11 的 32-byte action secret/hash 缺少后端直接追踪。
  - Closure: CHK-006 增加 A11 trace；coverage matrix 把后端 token/hash 交 CMD-001，把 fragment/referrer/storage 交 CMD-001/002 + browser evidence。

### suggestion

- [x] 核心 coverage rows 已展开到 A3、A8、A9、A11、A14，避免跨表反向推导。

## 4. User Review Focus

- 仍需 owner 在 Epic 统一 gate 确认两份 child design；本报告不把 design 从 draft 自动改成 approved。
- 实现前仍必须选择当前 branch 或 worktree/branch；当前没有创建分支、提交、部署或执行生产动作。
- 实现期外部 checkpoint 仍包括邮件 provider/transport、非生产 secret reference、脱敏 recipient reference 与 accepted receipt。
- 核心实现风险是 mixed admission 线性化、refresh 密钥协议与 absolute expiry、全请求路径 bearer secret 隔离。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A3/A8/A9/A11/A14 已独立映射 step/command/evidence | 实现期按矩阵产证据 |
| DoD Contract | pass | E | 8 steps、25 checks、6 core commands 与 artifacts 齐全 | 保持全部 pending 到实现 |
| Steps and checks traceability | pass | E | S1～S6/A1～A18 均有直接或窄范围 trace | 运行 YAML/ID 机械校验 |
| Roadmap contract compliance | pass-with-owner-process-override | C | roadmap 行为缺口已修；仅省略实质修订后的第四轮独立复审 | Epic 统一人工确认仍保留 |
| Module interface design | pass | C | dry-run、CurrentAccount、maintenance sweep 都在 account-auth seam，caller 不读 store | code review 核对无旁路 |
| Validation and artifacts | pass | C | mixed barrier、KDF bytes、cookie headers、rollback、mail checkpoint 都有 command owner | Goal package 生成后持久化 checkpoint |

Summary: E=3, C=3, H=0, H-only core checks=none。

## 6. Residual Risk

- 第四轮独立复审按 owner 明确指令省略；风险由本轮逐 finding closure、机械校验和 Epic 统一人工确认接管，不能把该裁剪解释为实现已被审查。
- Provider/transport、secret、DNS/发件域、测试收件方仍是外部状态；accepted receipt 前 STEP-004/acceptance 不得完成。
- Refresh family 撤销后 access JWT 最多继续 10 分钟，是 ADR-006 已接受窗口。
- Production rollback/cutover/root rotation/deploy 继续需要独立授权；synthetic rehearsal 不授权真实执行。
- Item1 仍不交付 persistent limiter/timing budget/monitor，production 公开注册必须继续被 preflight 锁闭，直到 `public-auth-hardening` 完成。

## 7. Verdict

- Status: passed
- Review basis: 三轮独立审查 + owner 授权的最终 focused closure。
- Next: 保持 design `status: draft`、checklist 全 pending，运行 feature workflow hook 后交回 Epic 批量设计，继续 `public-auth-hardening`；不单独请求第一份 child design 确认。

## 8. Focused Closure

- Closure owner: `/root`
- Authorization: owner 于 2026-07-30 明确要求停止反复 review、缩短设计阶段。
- Scope: 只关闭 round 3 已知 findings；没有改变 Epic 产品范围、requirement/ADR、production release boundary 或实施授权。
- Validation: YAML、frontmatter/status、ID/trace、placeholder scan、diff check 与 workflow hook 均须通过后才交回 Epic。
