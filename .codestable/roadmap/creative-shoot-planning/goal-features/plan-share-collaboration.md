---
doc_type: roadmap-goal-feature
roadmap: creative-shoot-planning
feature: 2026-08-05-plan-share-collaboration
roadmap_item: plan-share-collaboration
nature: mixed
status: pending
created: 2026-08-06
---

# plan-share-collaboration Goal Feature 规格

## 1. Mapping

- Feature dir：`.codestable/features/2026-08-05-plan-share-collaboration`
- Design / checklist / design-review：`plan-share-collaboration-design.md` / `plan-share-collaboration-checklist.yaml` / `plan-share-collaboration-design-review.md`
- Review / QA / acceptance：`plan-share-collaboration-review.md` / `plan-share-collaboration-qa.md` / `plan-share-collaboration-acceptance.md`
- Dependencies：`shoot-plan-core`、`planning-reference-assets`、`shoot-plan-crm-integration` 均严格 `done`
- Nature：mixed，高风险匿名公网面

## 2. Deliverable And Core Runtime Path

交付 proposal/full 分层 token generation、Bearer 管理、匿名 exact projection、token-bound shared media、feedback、readiness/on-site assignment、caller-generated receipt commitment、撤销与 archive seam，以及跨实例持久限流/幂等。

核心路径：Bearer issue/rotate/revoke/management → 无 cookie 匿名 proposal/full read → shared asset → plan/Shot feedback → readiness/on-site claim → 一次性 secret 本地展示 → token+receipt revoke → 摄影师 disposition/revoke。覆盖 eligibility/expiry quote、response loss、outer/business quota、跨午夜 digest rollover、过期/撤销/轮换/归档统一 404、DB/object/restart 和 populated-down writer exclusion。匿名响应必须逐 key 验证零价格/成本/工时/内部身份。

## 3. Mandatory Commands

- `cd backend && go test -p=1 ./... -count=1 -parallel=1`
- `cd frontend && npm run lint && npm run build`
- `cd frontend && npm run test:plan-share`
- `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts`
- `make check`
- Supporting：`node --test frontend/scripts/planning-prototype-v2.test.mjs`

Core 命令失败必须修复或阻塞；supporting 命令只有非核心且有明确理由时才能 trust-prior，不能跳过匿名核心路径。

## 4. Feature DoD And Stage Gates

- Design：approved/passed，owner 已确认 caller-generated receipt/only-commitment/24h replay/不可恢复代价；不再重开 design review。
- Implementation：S1-S8 done；scope/dod/evidence gates passed；所有匿名 route、ledger、counter、media 与 source event 走真实 wiring。
- Review：独立 review 核验 exact DTO、token/receipt 不落明文、sealed scope/单 ledger、两层限流、IP rollover、CRM 锁序、assignment generation/reciprocal FK、archive/down。
- QA：proposal/full/API/PG/media/security/browser/A 矩阵、query/DTO/DOM/bundle/log negative、响应式/a11y 和全仓通过。
- Acceptance：客户可安全查看/反馈/认领，摄影师可管理；H2、统一 404、history retention 与无客户账号/自动消息成立。

## 5. Gate Inputs And Acceptance Evidence

Canonical future artifacts：`plan-share-collaboration-evidence-pack.md`、对应 results/gate/dod/dod-contract JSON、review/QA/acceptance。

Evidence 至少包含 PostgreSQL/media fault、exact DTO/idempotency、token/receipt/log canary、sealed capability、rate/admission/replay、cross-instance counter/daily rollover、CRM lock order、readiness guard/archive、expiry quote/replay、Bearer pagination golden、source event/FK/down、assignment generation/revision、interaction observation、security headers、专属 frontend/prototype runners、high-risk manifest、浏览器与 roadmap writeback。

## 6. Deliverables And Cleanliness

- 交付 planshare schema/domain/application/repository/security counter、OpenAPI/routes、匿名页/工作台 UI、测试与 security evidence。
- 禁止 debug、临时 TODO/FIXME、注释代码、unused imports、手写 DTO、placeholder/static fixture、raw token/receipt/free text log、Web Storage secret、匿名第三方资源、AI/provider 或客户 delivery dependency。
- Accepted 且 Goal commit 授权有效后 scoped commit；不 push。

## 7. Failure Recovery

任何匿名权限/密钥/限流/事务核心缺陷回 implementation 并重跑独立 review/QA/acceptance；仅 artifact 落盘缺口按 stage 修复。需要客户账号、实时协同、服务器恢复 secret 或客户消息通道均属于 scope change，立即 handoff。
