---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-creative-planning-v1-hardening
roadmap_item: creative-planning-v1-hardening
nature: mixed
status: pending
created: 2026-08-06
---

# creative-planning-v1-hardening Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-creative-planning-v1-hardening`
- Design / checklist / design-review：`creative-planning-v1-hardening-design.md` / `creative-planning-v1-hardening-checklist.yaml` / `creative-planning-v1-hardening-design-review.md`
- Review / QA / acceptance：`creative-planning-v1-hardening-review.md` / `creative-planning-v1-hardening-qa.md` / `creative-planning-v1-hardening-acceptance.md`
- Dependencies：ingestion、CRM、reference-assets、share、reminders、business 六项全部严格 `done`
- Nature：mixed，跨模块/运维/发布 readiness 收口

## 2. Deliverable And Core Runtime Path

交付 PlanningV1 conformance/evidence index、真实跨模块 E2E 与 failure matrix、H1/H2、五页 prototype responsive/a11y、schema-v1/v2 strict backup reader/writer、restore preflight/state machine、production-shaped rehearsal evidence、release/rollback readiness；不首次补上游核心规则。

核心路径：真实账号完成摄取→shot list→live Run Mode纠错→full分享/asset/feedback/assignment→摄影师 reminder→私有 business draft，并覆盖 DB/object/network/restart/late-delete/stale/fence。随后用 v2 七文件包破坏并恢复 PG/avatar/planning-media，固定阶段和 failure-stop；旧 reader 拒绝 v2、新 reader兼容 v1/v2。五页 UI 在 1600/1280/375/coarse/200%/键盘/读屏/强光下跑正常与全部关键失败态。

## 3. Mandatory Commands

- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/planningmedia/... ./internal/planshare/... ./internal/reminder/... ./internal/order ./internal/schedule ./internal/settings ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1`
- `cd frontend && npm run test:shoot-planning`
- `cd frontend && npm run test:shoot-planning:e2e`
- `cd frontend && npm run test:prototype && npm run build && npm run lint`
- `python3 scripts/lib/planningbackup/selftest.py`
- `bash scripts/test-planning-ops-backup-restore-safety.sh`
- `make check`
- H-only / non-automatic：`./scripts/rehearse-planning-backup-restore.sh --config <owner-approved-production-shaped-config> --evidence <private-evidence-dir>`
- `./scripts/verify-planning-v1-release-readiness.sh --evidence <evidence-manifest> --json`

CMD-009 需要 owner 提供环境和独立操作授权；缺授权时 acceptance handoff，不能用本地 mock/fixture 代替。

## 4. Feature DoD And Stage Gates

- Design：approved；owner-capped local closure 已 passed，本 feature 不再启动 design review。
- Implementation：S1-S8 done；依赖 conformance 无缺口；scope/dod/evidence gates passed；hardening 不私建补偿表/规则绕过上游。
- Review：独立 code review 核验 ownership、匿名负向、strict decoders、restore state machine、evidence stale 与 release 无副作用；遵守最多三轮。
- QA：A1-A27、真实 browser/API/PG/volumes/restart/fault/old-reader、G1-G3 validator、生产形态 rehearsal 与全部核心命令有可重放证据。
- Acceptance：rehearsal、evidence index、release/rollback readiness、sanitization、最终 build/hash/config 和 roadmap/docs 回写全部一致。
- Authorization：任何 remote/publish/release/deploy/promotion/cutover 或 production restore 都未由 Goal 自动授权。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`creative-planning-v1-hardening-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance，以及 `planning_v1_dependency_conformance.json`、conformance/E2E/failure/H1-H2/prototype matrices、stage evidence/index、schema golden/adversarial corpus、restore preflight/stage/rehearsal、release readiness、Evidence Manifest 与 sanitization report。

G1/G2/G3 必须按 current sample decision/window/finalization 重算；report/decision/build/config bytes 变化使旧 index/approval stale。Restore 必须验证 source package missing/orphan；v2 不因当前 target 损坏而拒绝修复，v1 面对非空 planning target 必须 fail closed。

## 6. Deliverables And Cleanliness

- 交付 test/e2e/conformance runners、planningbackup v2 工具与 safety scripts、operator runbook、截图/录屏/a11y、evidence/readiness JSON 和限定文档回写。
- 禁止 debug、临时 marker、注释代码、unused imports、重复 DTO、placeholder/fake pass、skip/force preflight、secret/production data fixture、anonymous business signal、automatic release/cutover。
- Accepted 且 commit ref 有效后 scoped commit；不 push。真实 rehearsal/private evidence 不得误提交敏感内容。

## 7. Failure Recovery

上游 conformance 缺口：S2-S8 零 diff，归属原 owner feature并 handoff/回退，不在 hardening 暗补。E2E/restore/readiness 实现缺陷回 implementation并重跑 review/QA/acceptance；artifact-only 缺口按 stage 修复。缺 production-shaped 环境/授权、核心路径不可验证或同项三轮失败时持久化 handoff，绝不打印完成标记。
