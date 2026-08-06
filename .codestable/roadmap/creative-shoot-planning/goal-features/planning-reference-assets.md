---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-planning-reference-assets
roadmap_item: planning-reference-assets
nature: mixed
status: accepted
created: 2026-08-06
---

# planning-reference-assets Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-planning-reference-assets`
- Design / checklist / design-review：`planning-reference-assets-design.md` / `planning-reference-assets-checklist.yaml` / `planning-reference-assets-design-review.md`
- Review / QA / acceptance：`planning-reference-assets-review.md` / `planning-reference-assets-qa.md` / `planning-reference-assets-acceptance.md`
- Dependencies：`shoot-plan-core` 必须严格 `done`
- Nature：mixed

- Acceptance：已通过；3 轮 review cap 已消费，REV-016 由 owner-cap 窄修复和独立 QA closure 验证；accepted 状态与 scoped commit 在本次 feature 收尾中同批持久化。
- Residual：REV-017、真实 HTTP multipart negative fixture、projection boundary、浏览器极端尺寸证据与外部 production checkpoints 仍按 acceptance 报告保留。

## 2. Deliverable And Core Runtime Path

交付 planningmedia typed local store、source×rights×purpose 服务端矩阵、original/display rendition、Plan/Shot binding、lease、read pin、opaque access ref、GC、inventory/manifest 与模块恢复，且不回归 avatar 生命周期。

核心路径：合法/非法图片上传与 adversarial corpus → 服务端 rights 判定 → plan/shot attach/detach → RunInputSnapshot read-pin → 鉴权内容读取 → stale/released/corrupt/late-delete/GC → empty-target module restore。必须用真实 DB/object bytes/HTTP headers 和并发故障证据；公开静态目录或 object key 暴露均为 blocking。

## 3. Mandatory Commands

- `make generate-check`
- `cd backend && go test -p=1 ./internal/planningmedia ./internal/platform/immutablefs ./internal/avatarmedia ./internal/accountprofile ./internal/shootplanning ./internal/platform/idempotency ./internal/platform/httpapi -count=1 -parallel=1`
- `cd backend && go test -p=1 ./internal/planningmedia -run 'TestPostgres|TestSharedTx|TestGC|TestRestore|TestUploadFaults' -count=1 -parallel=1`
- `cd frontend && npm run test:planning-media`
- `cd frontend && npm run build && npm run lint`
- `make check`

## 4. Feature DoD And Stage Gates

- Design：approved/passed，rights matrix、typed keys、store/DB ownership、restore 与 A1-A16 可追踪。
- Implementation：S1-S8 done；scope/dod/evidence gates passed；bounded spool、single claim、shared-tx、read pin 和 route composition 走真实实现。
- Review：独立 review 核验服务端权利矩阵、AccountScope、bytes/metadata 事务、GC/lease/pin、avatar non-regression 与 object identity 隐藏。
- QA：A1-A16、adversarial bytes、PG/object fault、query bound、browser 断点、module restore、全仓通过。
- Acceptance：checks 全 passed；planningmedia 术语/ownership 回写；生产 schema-v2 只留给 hardening。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`planning-reference-assets-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 rights matrix、adversarial corpus、object fault injection、PG concurrency、idempotency、inventory/restore manifest、avatar characterization、API/header/query matrix、shared-tx ingestion probe、scope negative guard、1600/1280/375 browser 与 diff summary。

## 6. Deliverables And Cleanliness

- 交付 planningmedia schema/repository/store/inventory/restore seam、OpenAPI/生成物/handlers、工作台 gallery/binding 与 Run Mode reference sheet。
- 禁止 debug/临时 marker/注释代码/重复 DTO/placeholder/public static media route/object-key exposure；不新增 avatar 复用 hack、AI/provider 或通用文件仓库。
- 只在 Goal commit 授权有效且 accepted 后 scoped commit；不 push。

## 7. Failure Recovery

行为缺陷回 implementation 并重跑 review/QA/acceptance；evidence-only 缺口只修相应 stage。若实现要求外部对象存储、密钥管理或改变生产 backup schema，属于 scope/ADR 变化，立即 handoff。
