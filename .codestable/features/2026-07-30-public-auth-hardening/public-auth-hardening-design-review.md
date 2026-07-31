---
doc_type: feature-design-review
feature: 2026-07-30-public-auth-hardening
status: passed
review_state: passed
review_reason: owner-authorized-single-review-focused-closure
reviewer_id: /root/review_public_auth_hardening
reviewed: 2026-07-30
round: 1
---

# public-auth-hardening feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
- Checklist: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml`
- Roadmap/items: `.codestable/roadmap/self-service-account-system/`
- Dependency: `.codestable/features/2026-07-30-email-account-access/` design/checklist/passed review
- Related docs: self-service requirement、CONTEXT、ADR-005～007

### Independent Review

- Status: completed
- Detection: independent-agent
- Provider / agent: `/root/review_public_auth_hardening`
- Result before closure: changes-requested；2 blocking + 3 important，无措辞/格式/suggestion扩展。
- VerifiedNoWrite: reviewer启动前后全工作树内容摘要均为 `d24190f3e4aac15d0b2b199e7b60b7bc2a2b75fa370551d59cdb69c55278fff8`。
- Owner instruction: owner明确要求停止反复review、缩短设计阶段。按此指令只做一次独立审查；下述修订由主agent逐finding focused closure，不再启动第二轮。该裁剪不自动批准design，Epic统一人工确认仍保留。

## 2. Design Summary

- Goal: 补齐password forgot/reset/change、all-refresh-family revoke、persistent limiter/anti-enumeration、auth monitor、production enable-ready preflight与security/rollback catalog。
- Steps: 7，全部pending。
- Checks: 23，全部pending。
- Core commands: 7，覆盖backend/ops/frontend/codegen/preflight/security catalog/full check。
- Production effects: deploy/migration/cutover/rotation/registration toggle/commit/push均不自动执行。

## 3. Findings And Closure

### blocking

- [x] FDR-R1-B1 — reset/change限速缺合法seam，changePassword 429无action/预算。
  - Closure: parent roadmap与design统一把`ClientMeta`加入ResetPassword/ChangePassword；limiter由account-auth拥有，HTTP只传meta。新增`change_password` action；login/change为5 subject/30 source/15m，verify/reset为10/30/15m。所有attempt消费source，成功login/change只reset各自subject，成功token action不reset。Reset subject=selector，Change subject=canonical account ID。已进入D1/D3/D4、interface、A6、STEP-001、CHK-004～010、CMD-001。
- [x] FDR-R1-B2 — legacy monitor无法判断dry-run/cutover phase/severity。
  - Closure: event allowlist仅为legacy_claim增加boolean`dry_run`；命令固定`--cutover-mode=off|active`默认off。非dry-run失败off=warning、active=high，dry-run不触发。已同步roadmap D7、A14、STEP-005、CHK-016/017、CMD-002。

### important

- [x] FDR-R1-I1 — reset/change可信Origin下cookie结果矩阵含糊。
  - Closure: 新增operation×outcome×status×app/limiter×Set-Cookie表。只有success 204清cookie；validation/429/reset-invalid/change-wrong/500均无Set-Cookie；untrusted Origin 403且零app/limiter/cookie副作用。已进入D2/D6、A5、STEP-004、CHK-013、CMD-001。
- [x] FDR-R1-I2 — trusted proxy header与多hop算法未固定。
  - Closure: 只读X-Forwarded-For并忽略Forwarded；direct peer必须trusted；限制1024bytes/16hops；strict ParseAddr+Unmap；从右向左剥离trusted hops取首个untrusted，全trusted取最左；任一非法整链回退direct。已进入D4、A9、STEP-001、CHK-009、CMD-001与preflight。
- [x] FDR-R1-I3 — preflight stale evidence不可机械判断。
  - Closure: 固定versioned envelope字段、current build/schema/config fingerprint绑定、non-secret fingerprint组成、mail provider/secret-ref/recipient-ref绑定；mail/monitor/security 24h、rollback/rotation 7d、future skew 5m，live config/limiter/cutover每次重查。已进入D8、A16、STEP-006、CHK-019、CMD-005/006。

## 4. User Review Focus

- Timing hard contract：mail action 1000ms floor + 0～50ms jitter，provider deadline≤900ms；login/token 300ms + 0～25ms；统计阈值见D5/H2～H4。
- Limiter所有attempt都会消耗source budget；成功只重置login/change subject，不重置source。
- Production preflight通过只表示enable-ready，不修改开关或执行任何生产动作。
- Root rotation会使既有session/replay/limiter namespace失效，真实执行必须独立维护窗授权。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Roadmap/design/checklist alignment | pass-with-owner-process-override | C | 5项缺口已同步roadmap/design/checklist；省略修订后第二轮独立复审 | Epic统一人工确认 |
| Password/session transaction | pass | C | 30m token + credential/reset tokens/all families同事务，CMD-001 | implementation evidence |
| Limiter/anti-enumeration | pass | E | seam/action/budget/digest/proxy/timing/Retry-After都有直接check | PG/timing report |
| Monitor/preflight | pass | E | mode、threshold、degraded exits与freshness envelope均可机械重放 | ops reports |
| Security/rollback/isolation | pass | E | CMD-006/007与A17/A18覆盖 | QA evidence pack |

Summary: E=3, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- 修订后的第二轮独立复审按owner明确指令省略；风险由逐finding closure、机械校验和Epic统一人工确认接管。
- 真实provider能否满足≤900ms total deadline仍需实现期external checkpoint证明；不满足时不能降低timing contract。
- Persistent limiter/timing会增加认证延迟和PostgreSQL写入量，QA必须保存并发与benchmark证据。
- Access JWT在全family revoke后仍最多有效10分钟，是ADR-006已接受风险。
- Real mail/DNS/credential/owner email/production env仍是外部状态；preflight evidence不授权真实变更。

## 7. Verdict

- Status: passed
- Review basis: 一次独立审查 + owner授权的逐finding focused closure。
- Next: design保持draft、30个step/check保持pending；运行feature/epic workflow hooks后进入两份child design统一人工确认。

## 8. Focused Closure

- Closure owner: `/root`
- Authorization: owner于2026-07-30要求停止多轮review并缩短design阶段。
- Scope: 只关闭本轮2 blocking+3 important，同时修正parent roadmap内部interface/monitor矛盾；不扩产品范围、不授权implementation或production effect。
- Required validation: YAML、status/count、trace/placeholder、diff check、feature hook、epic hook。
