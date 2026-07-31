---
doc_type: roadmap-goal-feature
roadmap: self-service-account-system
feature: 2026-07-30-public-auth-hardening
roadmap_item: public-auth-hardening
nature: mixed
status: pending
created: 2026-07-31
---

# public-auth-hardening Goal Feature 规格

## 1. Mapping

- Roadmap item: `public-auth-hardening`
- Feature dir: `.codestable/features/2026-07-30-public-auth-hardening`
- Design: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design.md`
- Checklist: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-checklist.yaml`
- Design review: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-design-review.md`
- Review: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-review.md`
- QA: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-qa.md`
- Acceptance: `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-acceptance.md`
- Dependencies: `email-account-access`
- Nature: mixed
- Goal baseline status: pending

## 2. Deliverable

Password forgot/reset/change、全refresh family撤销、PostgreSQL限速、防枚举timing、认证monitor与production enable-ready preflight。

## 3. Core Runtime Path

- Forgot/reset/change的generic outcome、30m token、credential更新与多设备all-family revoke。
- Subject/source预算、Retry-After、trusted proxy、dummy bcrypt、floor+jitter和统计timing阈值。
- Reset fragment/no-referrer/storage absence与password pages/AuthState状态。
- JSONL/journald阈值、legacy dry-run/cutover mode、损坏行degraded与exit 0/1/2/3。
- Production preflight evidence envelope/freshness、root rotation与rollback/security catalog。
- 两个verified account全域隔离。

这些是production-public readiness的fresh core evidence；不能用in-memory limiter、fake mail、静态grep或历史报告替代。

## 4. Mandatory Commands

- `cd backend && go test -p=1 ./internal/platform/auth/... ./internal/platform/store/... ./internal/platform/httpapi/... -count=1 -parallel=1`
- `cd backend && go test -p=1 ./cmd/accountctl/... -count=1 -parallel=1`
- `cd frontend && npm run test:auth && npm run test:api-client && npm run build`
- `make generate-check`
- `./scripts/test-production-preflight.sh`
- `./scripts/test-auth-security-catalog.sh`
- `make check`

进入前必须由feature hook证明`email-account-access` item严格done。命令失败按stage gate恢复；provider/browser/PG core环境缺失必须handoff。

## 5. Feature DoD And Stage Gates

- Design：approved + design-review passed + checklist/Acceptance Coverage/DoD contract可解析。
- Implementation：7 steps全done；scope-gate、dod-runner、evidence-pack passed；core命令、timing/monitor/preflight与清洁度通过。
- Review：canonical review passed、独立Task agent、无unresolved blocking并消费evidence/gates。
- QA：canonical QA passed，覆盖A1～A18、DoD、review focus与真实PG/API/browser/CLI core paths。
- Acceptance：canonical acceptance passed，23 checks全passed，item/requirement/roadmap/README写回一致。
- Final audit：goal-consistency/goal-audit核验路径、identity、hash、authorization与terminal state。

## 6. Gate Inputs And Canonical Artifacts

- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-evidence-pack.md`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-evidence-pack-results.json`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-gate-results.json`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-dod-results.json`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-dod-contract-results.json`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-review.md`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-qa.md`
- `.codestable/features/2026-07-30-public-auth-hardening/public-auth-hardening-acceptance.md`

这些future artifacts由本次stages生成；相应stage前缺失是预期状态，不得提前伪造。

## 7. Acceptance Evidence

- 机器证据：checklist、command logs、gate JSON、evidence pack与input SHA。
- 运行证据：PG limiter/transaction、API headers、timing report、browser/storage、monitor exit matrix、preflight/rotation/rollback catalog。
- 人工证据：只限design明确要求的真实mail/provider/production环境输入；H-only core check未批准时必须handoff。
- Evidence path必须仓库相对且归属本feature；不得写secret、PII、raw token、完整邮箱/IP或损坏日志原文。

## 8. Deliverables

- Password account-auth/http/mail/frontend实现、PostgreSQL limiter、events/monitor、preflight/runbook/security catalog。
- Feature design/checklist/design-review/review/QA/acceptance。
- Evidence pack/results、DoD/gate results、timing/monitor/preflight/rotation/rollback/isolation报告。
- Requirement/CONTEXT/ADR/roadmap/README writeback或明确N/A。
- Goal-state feature status与roadmap item一致回写。

## 9. Cleanliness

- 无debug输出、临时TODO/FIXME/XXX、注释掉代码、sleep-based timing test、同名runner shim、临时下载包或`__pycache__`。
- 不触碰、stage或commitgoal-plan列出的protected paths；不提交凭证、PII、secret-bearing evidence、fake/sink/in-memory production默认或自动production effect。
- Scoped commit只在`approval-report.md#goal-commits`有效、feature accepted且状态更新持久化后允许；不得push。

## 10. Failure Recovery

- Implementation defect：回implementation修复，再重跑独立review、QA、acceptance。
- Review blocking：回implementation/review-fix并重新独立审查。
- QA implementation failure：回implementation后重跑review/QA/acceptance；纯evidence defect只修对应stage。
- Timing/provider/preflight core evidence不可用、scope变化、reviewer不可用或同项三轮失败：写goal-state handoff reason/next并停止。
- 不得用降级验证、跳过命令、伪造artifact、任务外改动或自动deploy/cutover/rotation换取通过。
