---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-plan-ingestion-capture
roadmap_item: plan-ingestion-capture
nature: functional
status: pending
created: 2026-08-06
---

# plan-ingestion-capture Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-plan-ingestion-capture`
- Design / checklist / design-review：`plan-ingestion-capture-design.md` / `plan-ingestion-capture-checklist.yaml` / `plan-ingestion-capture-design-review.md`
- Review / QA / acceptance：`plan-ingestion-capture-review.md` / `plan-ingestion-capture-qa.md` / `plan-ingestion-capture-acceptance.md`
- Dependencies：`shoot-plan-core`、`planning-reference-assets` 均严格 `done`
- Nature：functional；本 roadmap 唯一 minimal loop

## 2. Deliverable And Core Runtime Path

交付 deterministic parser/session/reparse/候选编辑、ReferenceLink/Readiness 候选、combined atomic commit、PlanBuildObservation 与 stage-1 evidence projector；不做 AI/OCR/crawler。

Fresh core path：粘贴聊天/纯链接/已上传图片 → URL-first 确定性拆条 → 编辑/合并/归类/丢弃/恢复/逐项 source-change ack → 同一 physical transaction 写 Shot/Readiness/link/media binding → 工作台 → ready → Run Mode capture。覆盖刷新恢复、多 tab revision、0-core media-only、participant failure、response loss/idempotent replay 和 active-time 300 秒 idle cap。

## 3. Mandatory Commands

- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/planningmedia ./internal/platform/idempotency ./internal/platform/httpapi -count=1 -parallel=1`
- `cd backend && go test -p=1 ./internal/shootplanning/ingestion -run 'TestParserGolden|TestCombinedCommit|TestObservation|TestEvidence' -count=1 -parallel=1`
- `cd frontend && npm run test:plan-ingestion`
- `cd frontend && npm run build && npm run lint`
- `make check`

## 4. Feature DoD And Stage Gates

- Design：approved/passed；parser v1、frame IDs、reparse reconciliation、commit delta、observation/retention 与 A1-A17 明确。
- Implementation：S1-S8 done；scope/dod/evidence gates passed；无 network parser、placeholder、第二 ledger 或浏览器计时。
- Review：独立 review 核验 determinism/XSS/SSRF negative、provenance、CAS/lock、callback replay、tick 偏差与 evidence gate 权限。
- QA：A1-A17、golden corpus、PG 故障/并发、fake clock、1600/1280/375/键盘/200%、最小闭环录屏和全仓通过。
- Acceptance：可低成本形成并原子保存可执行清单；fixed median≤600 秒只作为产品 fixture；真实 stage-1 仍 pending、不得自动派发 CRM。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`plan-ingestion-capture-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 parser golden/canonical diff、session API revision、combined PG fault、callback counts、activity clock、query/lock order、stage-1 report dry-run/hash、browser screenshots、minimal-loop recording、scope negative guard 与 diff summary。Stage evidence 必须是 canonical JSON + 派生 Markdown，不修改 approval。

## 6. Deliverables And Cleanliness

- 交付 ingestion schema/domain/application/repository/parser/report projector、OpenAPI/routes、三步摄取 UI、测试和 evidence。
- 禁止 raw ingestion log、crawler/provider/OCR/AI dependency、debug、临时 marker、注释代码、重复 DTO、placeholder。
- Accepted 后且 commit ref 有效才 scoped commit；不 push。

## 7. Failure Recovery

Parser/transaction/UI 行为缺陷回 implementation 并重跑后续 stages；报告落盘缺口只修 evidence/acceptance。真实 stage-1 尚无 5 样本是后续 CRM dispatch 的 expected handoff，不得将本 feature 标失败或伪造样本。
