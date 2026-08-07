---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-plan-assignment-reminders
roadmap_item: plan-assignment-reminders
nature: mixed
status: pending
created: 2026-08-06
---

# plan-assignment-reminders Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-plan-assignment-reminders`
- Design / checklist / design-review：`plan-assignment-reminders-design.md` / `plan-assignment-reminders-checklist.yaml` / `plan-assignment-reminders-design-review.md`
- Review / QA / acceptance：`plan-assignment-reminders-review.md` / `plan-assignment-reminders-qa.md` / `plan-assignment-reminders-acceptance.md`
- Dependencies：`plan-share-collaboration`、`shoot-plan-crm-integration` 均严格 `done`
- Nature：mixed，高风险跨模块 freshness/外部投递

## 2. Deliverable And Core Runtime Path

交付 `plan_assignment_checklist` projection、generation/source event/work/resolution、stable reconciliation epoch、quarantine/repair、date-only due、assignment Inbox、Settings binding revision、Delivery intent revision 和 final DB-clock/monotonic send authorization；收件人只允许摄影师账号所有者。

核心路径：formal active readiness assignment + future shoot slot → due/group/reminder → digest intent → final calling CAS → Telegram account owner；随后改期、timezone、rebind、assignment revoke、order cancel、plan archive、shoot-start 触发重算/撤回。覆盖 cross-instance/restart、generation gap/corrupt event、lock wait、payload build/pre-call crash/response loss、same local date timezone 和 mixed legacy/planning delivery；on-site support/未认领 readiness/昵称不得生成 due 或客户投递。

## 3. Mandatory Commands

- `make generate-check`
- `cd backend && go test -p=1 ./internal/reminder/... ./internal/planshare/... ./internal/shootplanning/... ./internal/order ./internal/schedule ./internal/settings/... ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi ./cmd/server -count=1 -parallel=1`
- `cd frontend && npm run test:plan-assignment-reminders`
- `cd frontend && npm run lint && npm run build`
- Supporting：`node --test frontend/scripts/planning-prototype-v2.test.mjs`
- `make check`

## 4. Feature DoD And Stage Gates

- Design：approved/passed；eligibility/due/materiality、closed lifecycle fact、generation frontier、delivery authority 与 residual at-least-once window 已冻结。
- Implementation：S1-S6 done；scope/dod/evidence gates passed；无 in-memory production fallback、Noop participant 或客户 channel。
- Review：独立 review 核验 account fence/全局锁序、contiguous resolution、reconciliation owner、DB clock + dual monotonic budget、rebind 与 business Delivery identity。
- QA：PG 并发/故障/restart、fixed-clock boundary、quarantine repair、recipient network negative、H1/H4/shot_at/business guard、browser 和全仓通过。
- Acceptance：只对 formal readiness 生成提醒，只发摄影师；撤销/改期/archive/shoot-start 后 freshness fail closed，legacy reminder 不回归。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`plan-assignment-reminders-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 group fingerprint/occurrence truth table、due/start matrix、source corruption、full epoch、generation frontier/resolution、fence type/ownership、timezone/temporal fault/restart、quarantine、CRM lifecycle/closed union/direct-link transaction、archive capability、digest intent/attempt/deadline/monotonic/rebind/retention、recipient negative、no-plan regression、OpenAPI/browser 和 roadmap writeback。

## 6. Deliverables And Cleanliness

- 交付 reminder/source/group schema、core/share/CRM/settings adapters、worker/reconciliation/sender、OpenAPI/read model/UI、测试与证据。
- 禁止 debug、临时 TODO/FIXME、注释代码、unused imports、手写 DTO、placeholder、Noop/in-memory production fallback、customer delivery、raw nickname/token/receipt log、`Order.shot_at` 或 business fact dependency。
- Accepted 且 commit ref 有效后 scoped commit；不 push。

## 7. Failure Recovery

Freshness、recipient、generation 或 external call authorization 缺陷是 implementation defect，修复后必须重跑 review/QA/acceptance；evidence-only 缺口按 stage 修复。若需客户直达、unbind surface、exact-once Telegram 或新的业务 Delivery identity，属于 scope change，立即 handoff。
