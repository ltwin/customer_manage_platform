---
doc_type: roadmap-goal-feature
roadmap: photographer-private-crm
feature: 2026-07-08-order-tracking
roadmap_item: order-tracking
nature: functional
status: accepted-baseline
created: 2026-07-22
---

# order-tracking Goal Feature 规格

## 1. Mapping

- Roadmap item: `order-tracking`
- Feature dir: `.codestable/features/2026-07-08-order-tracking`
- Design: `.codestable/features/2026-07-08-order-tracking/order-tracking-design.md`
- Checklist: `.codestable/features/2026-07-08-order-tracking/order-tracking-checklist.yaml`
- Design review: `.codestable/features/2026-07-08-order-tracking/order-tracking-design-review.md`
- Review: `.codestable/features/2026-07-08-order-tracking/order-tracking-review.md`
- QA: `.codestable/features/2026-07-08-order-tracking/order-tracking-qa.md`
- Acceptance: `.codestable/features/2026-07-08-order-tracking/order-tracking-acceptance.md`
- Dependencies: `customer-core`, `package-catalog`
- Nature: `functional`
- Goal baseline status: `accepted-baseline`

## 2. Deliverable

八态订单、定金/尾款标记、全局/客户/待收查询与引用完整性。

## 3. Core Runtime Path

客户+套系建单 → 合法状态推进 → 标定金/尾款 → 查询列表/详情 → 非法跃迁和未结清 closed 被拒。

该 feature 在 GoalPackage baseline 前已经 acceptance passed；driver 不重新进入 implementation/review/QA/acceptance。最终审计仍须核验 canonical identity、既有 terminal artifacts、聚合命令和现代 gate evidence。

## 4. Mandatory Commands

- `make check`
- `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts`
- `cd backend && go test ./...`
- `cd frontend && npm run build`

命令失败时按对应 stage gate 恢复；core 命令不得因耗时跳过。外部环境缺失且影响 core path 时持久化 handoff。

## 5. Feature DoD And Stage Gates

- Baseline admission：design approved、review/QA/acceptance passed、checklist steps done/checks passed 已机械确认。
- Execution：保持 goal-state status=accepted，不改历史 feature 代码或 terminal checklist；只在 final audit 对真实既有输入补 modern evidence。
- Review：canonical review `status: passed`、独立 Task agent、无 unresolved blocking，并消费 evidence/gates。
- QA：canonical QA `status: passed`，覆盖 core path、DoD、review focus 与 residual risks。
- Acceptance：canonical acceptance `status: passed`，checks 全 passed，roadmap/item/writeback 与证据一致。
- Final audit：goal-consistency/goal-audit 对路径、identity、hash、authorization 与 terminal state 机械核验。

## 6. Gate Inputs And Canonical Artifacts

- `.codestable/features/2026-07-08-order-tracking/order-tracking-evidence-pack.md`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-evidence-pack-results.json`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-gate-results.json`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-dod-results.json`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-dod-contract-results.json`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-review.md`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-qa.md`
- `.codestable/features/2026-07-08-order-tracking/order-tracking-acceptance.md`

Package creation baseline missing modern/future artifacts: `order-tracking-evidence-pack.md`, `order-tracking-evidence-pack-results.json`, `order-tracking-gate-results.json`, `order-tracking-dod-results.json`, `order-tracking-dod-contract-results.json`.
历史缺口只能由当前官方 gate 对真实 design/checklist/report/command evidence 生成或由 audit 明确阻断；禁止占位文件、复制别的 feature 结果或手写 passed JSON。

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

- 既有 terminal artifact identity/status 不一致：final audit repair；若反映真实实现缺口则 handoff，不静默重开或改写历史 acceptance。
- Review blocking：回 implementation/review-fix，重新独立审查。
- QA implementation failure：回 implementation 后重跑 review/QA/acceptance；纯 evidence defect 只修对应 stage。
- Core environment/evidence 不可用、scope 变化、reviewer 不可用或同项三轮失败：写 goal-state handoff reason/next 并停止。
- 不得用降级验证、跳过命令、伪造 artifact 或范围外改动换取通过。
