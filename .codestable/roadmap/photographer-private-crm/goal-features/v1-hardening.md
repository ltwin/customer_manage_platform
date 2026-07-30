---
doc_type: roadmap-goal-feature
roadmap: photographer-private-crm
feature: 2026-07-22-v1-hardening
roadmap_item: v1-hardening
nature: mixed
status: pending
created: 2026-07-22
---

# v1-hardening Goal Feature 规格

## 1. Mapping

- Roadmap item: `v1-hardening`
- Feature dir: `.codestable/features/2026-07-22-v1-hardening`
- Design: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-design.md`
- Checklist: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-checklist.yaml`
- Design review: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-design-review.md`
- Review: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-review.md`
- QA: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-qa.md`
- Acceptance: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-acceptance.md`
- Dependencies: `customer-avatar`, `telegram-digest`, `dashboard`, `data-export`
- Nature: `mixed`
- Goal baseline status: `pending`

## 2. Deliverable

全路由状态、375px 轻路径、生产预检/一致备份恢复、README 与 V1 全链路证据。

## 3. Core Runtime Path

逐 route 状态矩阵；375px 查档期/搜客户/记备注；三部署轨 preflight；真实 synthetic backup/destructive restore/race smoke；A24 主链与 A27 export。

该路径属于本次 goal 的 fresh core evidence，必须真实运行；不能以静态 grep、历史截图或自报 JSON 替代。

## 4. Mandatory Commands

- `make check`
- `cd frontend && npm run test:v1-hardening`
- `bash -n scripts/*.sh`
- `./scripts/test-v1-ops-contract.sh && docker build -t crm:v1-hardening .`
- `./scripts/v1-ops-smoke.sh`
- `git diff --check`

命令失败时按对应 stage gate 恢复；core 命令不得因耗时跳过。外部环境缺失且影响 core path 时持久化 handoff。

## 5. Feature DoD And Stage Gates

- Design：approved + independent design-review passed + checklist/Acceptance Coverage/DoD contract 可解析。
- Implementation：全部 steps done；scope-gate、dod-runner、evidence-pack passed；命令日志与清洁度通过。
- Review：canonical review `status: passed`、独立 Task agent、无 unresolved blocking，并消费 evidence/gates。
- QA：canonical QA `status: passed`，覆盖 core path、DoD、review focus 与 residual risks。
- Acceptance：canonical acceptance `status: passed`，checks 全 passed，roadmap/item/writeback 与证据一致。
- Final audit：goal-consistency/goal-audit 对路径、identity、hash、authorization 与 terminal state 机械核验。

## 6. Gate Inputs And Canonical Artifacts

- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-evidence-pack.md`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-evidence-pack-results.json`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-gate-results.json`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-dod-results.json`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-dod-contract-results.json`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-review.md`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-qa.md`
- `.codestable/features/2026-07-22-v1-hardening/v1-hardening-acceptance.md`

Package creation baseline missing modern/future artifacts: `v1-hardening-review.md`, `v1-hardening-qa.md`, `v1-hardening-acceptance.md`, `v1-hardening-evidence-pack.md`, `v1-hardening-evidence-pack-results.json`, `v1-hardening-gate-results.json`, `v1-hardening-dod-results.json`, `v1-hardening-dod-contract-results.json`.
这些文件由本次 implementation/review/QA/acceptance 依协议生成；在相应 stage 前缺失是预期状态，不能提前伪造。

## 7. Acceptance Evidence

- 机器证据：checklist、command logs、gate JSON、evidence pack 与输入 SHA。
- 运行证据：API/后端/前端/browser/e2e/smoke 中与该 feature 性质匹配的 core path。
- 人工证据：只有设计或 roadmap 明确要求的 owner/environment attestation；H-only core check 未批准时必须 handoff。
- Evidence path 必须仓库相对、归属本 feature；不得写 secret、PII、raw token、用户机器绝对路径或未经脱敏的外部响应。

## 8. Deliverables

- feature design/checklist/design-review/review/QA/acceptance
- 实现代码与注册/挂载点（历史 baseline feature 只核验，不重新生成）
- command logs、evidence pack/results、DoD/gate results
- requirement/architecture/roadmap/README writeback 或明确 N/A
- goal-state feature status 与 roadmap item 的一致回写

## 9. Cleanliness

- 无 debug 输出、临时 TODO/FIXME/XXX、注释掉代码、同名 runner shim、临时下载包或 `__pycache__`。
- 不触碰、stage 或 commit 任务外 `.workflow/` 与 `install-cpamp.sh`。
- 不提交凭证、PII、Docker temp resources、备份半包或 secret-bearing evidence。
- scoped commit 仅在 `approval-report.md#goal-commits` 机械有效、feature accepted 且全部状态更新持久化后允许；不得 push。

## 10. Failure Recovery

- Implementation defect：回 implementation 修复，再重跑 independent review、QA、acceptance。
- Review blocking：回 implementation/review-fix，重新独立审查。
- QA implementation failure：回 implementation 后重跑 review/QA/acceptance；纯 evidence defect 只修对应 stage。
- Core environment/evidence 不可用、scope 变化、reviewer 不可用或同项三轮失败：写 goal-state handoff reason/next 并停止。
- 不得用降级验证、跳过命令、伪造 artifact 或范围外改动换取通过。
