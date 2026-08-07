---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-shoot-plan-core
roadmap_item: shoot-plan-core
nature: mixed
status: pending
created: 2026-08-06
---

# shoot-plan-core Goal Feature 规格

## 1. Mapping

- Roadmap item：`shoot-plan-core`
- Feature dir：`.codestable/features/2026-08-05-shoot-plan-core`
- Design / checklist / design-review：`shoot-plan-core-design.md` / `shoot-plan-core-checklist.yaml` / `shoot-plan-core-design-review.md`
- Review / QA / acceptance：`shoot-plan-core-review.md` / `shoot-plan-core-qa.md` / `shoot-plan-core-acceptance.md`
- Dependencies：none
- Nature：mixed

## 2. Deliverable And Core Runtime Path

交付独立 ShootPlan/Shot/Readiness/PublicPlanScale/PlanExecutionWindow、RunModeSession、append-only result/void history/current projection、finalization/reopen，以及 neutral planning reminder fence 与 archive capability metadata。

Fresh core evidence 必须真实运行：无 customer/order 建案 → 编辑 brief/Shot/Readiness/window → ready → 独立 375px Run Mode → captured/skipped/cleared/void → completion snapshot → reopen/re-complete；覆盖跨账号、revision/idempotency、双连接 fence、archive marker/promotion 和断网失败。静态 DTO 或 mock repository 不能替代 API/PG/browser/CLI 路径。

## 3. Mandatory Commands

- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/platform/idempotency ./internal/platform/planningcapability ./internal/platform/store/... ./internal/order ./internal/schedule ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1`
- `cd frontend && npm run test:shoot-planning`
- `cd frontend && npm run build && npm run lint`
- `make check`

全部为 core；缺包/runner 是 dependency 或实现缺口，不得跳过。

## 4. Feature DoD And Stage Gates

- Design：approved + design-review passed；A1-A18、Coverage Matrix、DoD Contract 与 checklist 可解析。
- Implementation：S1-S8 全 done；scope/dod/evidence gates passed；真实 PostgreSQL、OpenAPI、route wiring、Run Mode 与 archive/fence 证据齐全。
- Review：独立 review passed，重点检查 AccountScope、单 physical transaction、revision/event replay、archive capability、无 placeholder 与范围负向。
- QA：A1-A18、PG 并发/故障、API matrix、1600/1280/375/coarse/200%/断网和全部 core 命令通过。
- Acceptance：checks 全 passed；领域/requirements/roadmap 回写；无 CRM/media/share/business/AI 越界。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`shoot-plan-core-evidence-pack.md`、`shoot-plan-core-evidence-pack-results.json`、`shoot-plan-core-gate-results.json`、`shoot-plan-core-dod-results.json`、`shoot-plan-core-dod-contract-results.json`、review/QA/acceptance 报告。

Evidence 至少包含 command output、API response matrix、PG concurrency、route→application coverage、cross-resource idempotency、ExecuteInScope/Capability 单事务 probe、readiness removal guard、archive acknowledgement/capability/promotion/fresh-install/restore fixtures、planning reminder generation/fence、reciprocal FK/up-down、浏览器截图与 diff summary。路径必须仓库相对并归属本 feature；future artifact 不得提前伪造。

## 6. Deliverables And Cleanliness

- 交付 migration、shootplanning/domain/application/repository、OpenAPI+生成物、server/planningctl wiring、工作台和独立 Run Mode、测试与证据。
- 禁止 debug output、临时 task marker、注释掉代码、重复 OpenAPI DTO、placeholder handler、AI/provider/匿名/经营表面。
- Scoped commit 仅在 `approval-report.md#goal-commits` 有效且 feature accepted 后允许，不 push、不使用 `--no-verify`。

## 7. Failure Recovery

Implementation defect 回 implementation；review/QA 行为缺陷修复后重跑独立 review→QA→acceptance；纯 evidence 缺口只修对应 stage。Scope 变化、核心环境缺失、reviewer 不可用或同项三轮失败时写 goal-state handoff，禁止降级验证或暗补后置模块。
