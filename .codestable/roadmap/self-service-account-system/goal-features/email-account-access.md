---
doc_type: roadmap-goal-feature
roadmap: self-service-account-system
feature: 2026-07-30-email-account-access
roadmap_item: email-account-access
nature: mixed
status: implementing
created: 2026-07-31
---

# email-account-access Goal Feature 规格

## 1. Mapping

- Roadmap item: `email-account-access`
- Feature dir: `.codestable/features/2026-07-30-email-account-access`
- Design: `.codestable/features/2026-07-30-email-account-access/email-account-access-design.md`
- Checklist: `.codestable/features/2026-07-30-email-account-access/email-account-access-checklist.yaml`
- Design review: `.codestable/features/2026-07-30-email-account-access/email-account-access-design-review.md`
- Review: `.codestable/features/2026-07-30-email-account-access/email-account-access-review.md`
- QA: `.codestable/features/2026-07-30-email-account-access/email-account-access-qa.md`
- Acceptance: `.codestable/features/2026-07-30-email-account-access/email-account-access-acceptance.md`
- Dependencies: none
- Nature: mixed
- Goal baseline status: pending

## 2. Deliverable

邮箱注册验证、短期access与可轮换refresh、全Web请求恢复、legacy原地认领和zero-account bootstrap；production公开注册继续锁闭。

## 3. Core Runtime Path

- capabilities/register/真实邮件/verify/session/`/me`/refresh/logout浏览器与API主链。
- JSON、multipart、avatar Blob、export的401 single-flight恢复与Web Storage absence。
- Synthetic legacy claim前后ID/counts/avatar checksum不变。
- Bootstrap dry-run零副作用、atomic guard与public/bootstrap双向并发barrier。
- 两个verified account全业务/后台隔离。

这些是fresh core evidence，必须真实运行；fake/sink不能替代真实accepted receipt，静态grep不能替代PostgreSQL/browser/CLI路径。

## 4. Mandatory Commands

- `cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... ./cmd/accountctl/... -count=1 -parallel=1`
- `cd frontend && npm run test:auth && npm run test:api-client && npm run build`
- `make generate-check`
- `./scripts/test-auth-legacy-cutover.sh`
- `./scripts/test-production-preflight.sh`
- `make check`

命令失败按stage gate恢复；core命令不得因耗时跳过。邮件provider/浏览器/Docker不可用且影响core path时持久化external checkpoint或handoff。

## 5. Feature DoD And Stage Gates

- Design：approved + design-review passed + checklist/Acceptance Coverage/DoD contract可解析。
- Implementation：8 steps全done；scope-gate、dod-runner、evidence-pack passed；core命令、真实mail checkpoint与清洁度通过。
- Review：canonical review passed、独立Task agent、无unresolved blocking，消费evidence/gates。
- QA：canonical QA passed，覆盖主链、并发、Origin/cookie、migration/rollback、两账号隔离与review focus。
- Acceptance：canonical acceptance passed，25 checks全passed，item/requirement/roadmap写回一致。
- Final audit：goal-consistency/goal-audit核验路径、identity、hash、授权与terminal state。

## 6. Gate Inputs And Canonical Artifacts

- `.codestable/features/2026-07-30-email-account-access/email-account-access-evidence-pack.md`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-evidence-pack-results.json`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-gate-results.json`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-dod-results.json`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-dod-contract-results.json`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-review.md`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-qa.md`
- `.codestable/features/2026-07-30-email-account-access/email-account-access-acceptance.md`

这些future artifacts由implementation/review/QA/acceptance依协议生成；相应stage前缺失是预期状态，不得提前伪造。

## 7. External Mail Checkpoint

- Durable state: `goal-state.yaml.external_checkpoints.mail_provider`
- Provider-neutral工作完成后写evidence path并置`awaiting-owner`。
- Owner只提供provider/transport、secret reference和脱敏recipient reference；不得保存secret或完整邮箱。
- 真实adapter配置后置`adapter-configured`；只有accepted receipt + redacted event才置`receipt-verified`。
- Checkpoint未verified时STEP-004、QA core path与acceptance不得通过；resume action固定为state中的命令。

## 8. Acceptance Evidence

- 机器证据：checklist、command logs、gate JSON、evidence pack与input SHA。
- 运行证据：PG transaction/concurrency、API headers、browser desktop/375px、storage inspection、CLI reports、mail receipt。
- Evidence path必须仓库相对且归属本feature；不得写secret、PII、raw token、完整邮箱或provider body。

## 9. Deliverables

- Schema/migration、account-auth、mail/http/OpenAPI、webapp-auth/pages、accountctl与preflight实现。
- Feature design/checklist/design-review/review/QA/acceptance。
- Evidence pack/results、DoD/gate results、migration/rollback/mail/browser报告。
- Requirement/CONTEXT/ADR/roadmap/README writeback或明确N/A。
- Goal-state feature status与roadmap item一致回写。

## 10. Cleanliness

- 无debug输出、临时TODO/FIXME/XXX、注释掉代码、同名runner shim、临时下载包或`__pycache__`。
- 不触碰、stage或commit goal-plan列出的三条protected paths；凭证、完整邮箱、token/cookie/Authorization与provider body不入产物。
- Scoped commit只在`approval-report.md#goal-commits`有效、feature accepted且状态更新持久化后允许；不得push。

## 11. Failure Recovery

- Implementation defect：回implementation修复，再重跑独立review、QA、acceptance。
- Review blocking：回implementation/review-fix并重新独立审查。
- QA implementation failure：回implementation后重跑review/QA/acceptance；纯evidence defect只修对应stage。
- Mail checkpoint awaiting：保存state/evidence并等待owner；不把sink当完成。
- Core环境/evidence不可用、scope变化、reviewer不可用或同项三轮失败：写goal-state handoff reason/next并停止。
- 不得用降级验证、跳过命令、伪造artifact、任务外改动或自动production effect换取通过。
