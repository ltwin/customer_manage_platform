---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-shoot-plan-crm-integration
roadmap_item: shoot-plan-crm-integration
nature: mixed
status: pending
created: 2026-08-06
---

# shoot-plan-crm-integration Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-shoot-plan-crm-integration`
- Design / checklist / design-review：`shoot-plan-crm-integration-design.md` / `shoot-plan-crm-integration-checklist.yaml` / `shoot-plan-crm-integration-design-review.md`
- Review / QA / acceptance：`shoot-plan-crm-integration-review.md` / `shoot-plan-crm-integration-qa.md` / `shoot-plan-crm-integration-acceptance.md`
- Dependencies：`shoot-plan-core`、`plan-ingestion-capture` 均严格 `done`
- Pre-dispatch：`stage-1-evidence-go` runner 必须 `passed`
- Nature：mixed

## 2. Deliverable And Core Runtime Path

交付 ShootPlan 与 customer/order/future shoot slot 的可空弱关联、link epoch snapshot、独立 connection/projection revisions、manual/schedule window reducer、CRM lifecycle transaction participants、batch planning summary 和 tracked v2 CRM UI；不自动改订单状态/档期。

核心路径：independent→customer/order link、direct link、unlink/deleted-unlink/relink、customer merge、order cancel/delete、slot create/update/delete/move、manual override/adopt、multiple plans、零 plan CRM 路径和 0/10/100 summary query。必须证明 account fence→sorted business locks→locked recheck→per-plan generation/projection/resolution 全事务，且 `Order.shot_at` 从 planning 路径排除。

## 3. Mandatory Commands

- `python3 .codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py --roadmap .codestable/roadmap/creative-shoot-planning --feature shoot-plan-crm-integration --decision stage-1-evidence-go --json`
- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/customer ./internal/order ./internal/schedule ./internal/platform/httpapi -count=1 -parallel=1`
- `cd frontend && npm run test:shoot-plan-crm`
- `cd frontend && npm run build && npm run lint`
- `make check`

全部 core；第一条非 0 时不创建 implementation artifact、不推进 index。

## 4. Evidence Dispatch Admission

Runner 只接受 `shoot-plan-crm-integration + stage-1-evidence-go`，并机械核验 canonical approval 状态与 `evidence/stage-1-*.json` 的 path/SHA-256/gate-version/current passed 状态。当前 decision 为 pending，预期结果 `needs-human`/exit 2。Typed resume 必须先写 owner approval，再恢复 state 并重跑；Goal/design/fixed fixture 均不能替代。

## 5. Feature DoD And Stage Gates

- Design：approved，owner-approved local-only design closure 已有 durable evidence，不重开 design review。
- Implementation：gate passed 后 S1-S6 done；scope/dod/evidence gates passed；无 Noop production participant/gate bypass。
- Review：独立 code review 核验 lock/fence、whole-tx generation、revision/CAS owner、epoch privacy、closed CRM fact union、material no-op、shot_at guard、summary exact values。
- QA：A1-A19、真实 PG 并发/断点、fixed clock、request union、三断点 UI、query exact matrix 和全仓通过。
- Acceptance：弱关联与 slot 事实正确，零 plan CRM 零影响；pending gate 从未被绕过。

## 6. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`shoot-plan-crm-integration-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 stage-1 runner JSON/exit、revision owner truth table、direct-link/deleted-unlink reducer、sorted-lock PG、participant fault、generation rollback/resolution、closed fact compile-negative、material no-op、shot_at guard、summary query/exact values、zero-plan API/DOM、OpenAPI golden、retry/ledger negative、slot boundary、prototype conformance、screenshots 与 diff summary。

## 7. Deliverables And Cleanliness

- 交付 sidecar migrations/reducer/commands/participants/summary/OpenAPI/routes/UI 和全部测试证据；runner 属于 Goal 包，不算 CRM implementation artifact。
- 禁止 debug/临时 marker/注释代码/重复 DTO/placeholder/Noop production participant/gate bypass/匿名或经营字段。
- Accepted 且 commit ref 有效后 scoped commit；不 push。

## 8. Failure Recovery

Gate needs-human/failed/blocked：保持 index，goal-state handoff，等待 canonical evidence/owner disposition；不得进入 implementation。实现或 QA 缺陷按 feature loop 回退；若需要异步 bus/trigger/跨服务、匿名摘要或修改 approval schema，属于 scope change，立即 handoff。
