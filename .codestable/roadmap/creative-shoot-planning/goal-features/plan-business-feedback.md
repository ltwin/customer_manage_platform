---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-plan-business-feedback
roadmap_item: plan-business-feedback
nature: mixed
status: pending
created: 2026-08-06
---

# plan-business-feedback Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-plan-business-feedback`
- Design / checklist / design-review：`plan-business-feedback-design.md` / `plan-business-feedback-checklist.yaml` / `plan-business-feedback-design-review.md`
- Review / QA / acceptance：`plan-business-feedback-review.md` / `plan-business-feedback-qa.md` / `plan-business-feedback-acceptance.md`
- Dependencies：`shoot-plan-crm-integration`、`plan-ingestion-capture`、`plan-assignment-reminders` 均严格 `done`
- Pre-dispatch：`stage-2-evidence-go` runner 必须 `passed`
- Nature：mixed，私有经营面

## 2. Deliverable And Core Runtime Path

交付 PlanningBusinessFacts、versioned rule/override、OrderAdjustmentDraft、ScheduleDurationDraft、stale oracle、immutable OrderPriceAdjustment audit、locked Order/Schedule participant 与私有 workbench/settings UI；business 不读取 share/reminder 数据。

核心路径：保存 null/0/value facts → 生成同 generation 双 draft → unknown/formula/warning 解释 → target/plan/facts/CRM/rule 变化投影 stale → exact acknowledgement apply Order 或 future slot end → same-tx audit/generation/ledger；无 slot 只 typed handoff 到普通日历。覆盖 overflow、concurrent CAS、response loss、dismiss、create-new cancel、share/cross-account 404、zero-plan H1 和 bounded query。

## 3. Mandatory Commands

- `python3 .codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py --roadmap .codestable/roadmap/creative-shoot-planning --feature plan-business-feedback --decision stage-2-evidence-go --json`
- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/order ./internal/schedule ./internal/settings ./internal/platform/idempotency ./internal/platform/httpapi ./cmd/server -count=1 -parallel=1`
- `cd frontend && npm run test:shoot-planning`
- `cd frontend && npm run build && npm run lint`
- `make check`
- Supporting：`node --test frontend/scripts/planning-prototype-v2.test.mjs`

## 4. Evidence Dispatch Admission

Runner 只接受 `plan-business-feedback + stage-2-evidence-go`，校验 owner-approved G3 canonical JSON 的 status/path/SHA-256/gate-version/current bytes。当前 decision pending，预期 `needs-human`/exit 2。未产生真实 5 个 eligible shared shoot、hash mismatch、stale finalization 或 rejected 都保持 index 并 handoff；不得用前序 acceptance、fixture 或 Goal 授权代替。

## 5. Feature DoD And Stage Gates

- Design：approved/passed，A1-A24、unknown/0、formula/fingerprint/stale/locked callback、settings map 与 tracked v2 可追踪。
- Implementation：gate passed 后 S1-S8 done；scope/dod/evidence gates passed；无浮点金额、自动 apply、第二 schedule/ledger 或匿名字段。
- Review：独立 review 核验金额/权限/stale/事务/幂等、locked target callback、CRM/reminder generation、share negative、query bound。
- QA：A1-A24、双连接 PG/fault、API negative、formula golden、Settings CAS、1600/1280/375/键盘/200% 和全仓通过。
- Acceptance：摄影师可解释生成并显式确认；Order/Schedule/audit 全成或全败，H1/H2 与原型 conformance 成立。

## 6. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`plan-business-feedback-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 stage-2 runner、formula unknown/zero/absolute truth table、range/overflow/warnings、generation snapshot、unavailable/no-write、target fingerprint/stale priority、PG fault、locked capability、immutable audit、schedule+reminder generation、Settings CAS/timezone、idempotency、route coverage、bounded query、share DTO/DOM negative、calendar typed prefill、prototype/browser、manifest 与 diff summary。

## 7. Deliverables And Cleanliness

- 交付 business/settings/order-audit schema、rules/facts/drafts/application/participants、OpenAPI/routes/UI、测试、Evidence Manifest 与 CONTEXT/roadmap 回写。
- 禁止 debug、临时 marker、注释代码、unused imports、重复 DTO、placeholder、floating point money、anonymous business signal、planshare/reminder/media/AI provider import。
- Accepted 且 commit ref 有效后 scoped commit；不 push。

## 8. Failure Recovery

Gate 非 passed：保持 index 并 handoff，等待真实 evidence/owner disposition。金额、stale、target write、权限或事务缺陷回 implementation 后重跑 review/QA/acceptance；evidence-only 缺口按 stage 修复。若要求自动定价、客户议价或 business 直接写 Order/Schedule 表，立即 handoff 回 design/ADR。
