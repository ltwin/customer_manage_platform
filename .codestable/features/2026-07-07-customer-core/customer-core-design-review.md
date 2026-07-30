---
doc_type: feature-design-review
feature: 2026-07-07-customer-core
status: stale
reviewed: 2026-07-07
round: 1
stale_reason: 2026-07-07 owner review changed POST /customers from identity to identities[1..N]; rerun design review before approval/implementation
---

# customer-core feature design 审查报告

> 2026-07-07 变更备注：本报告审查的是“建档只创建首个社交身份”的旧版 design。owner review 后，roadmap/design/checklist 已改为 `POST /customers` 接收 `identities[1..N]`，并要求客户与全部身份同事务写入。本报告保留历史结论，但不再作为当前 design 的 passed gate；实现前需重新跑 design review。

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-core/customer-core-design.md`
- Checklist: `.codestable/features/2026-07-07-customer-core/customer-core-checklist.yaml`
- Intent / brainstorm: feature 目录内无；相关原始讨论为 `.codestable/brainstorms/photographer-private-crm/brainstorm.md`
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`、`photographer-private-crm-items.yaml`
- Related docs: `requirements/customer-profile.md`、`requirements/CONTEXT.md`、ADR-001、ADR-003、compound `2026-07-06-accountscope-fail-loud`、`2026-07-06-openapi-roadmap-bidirectional-check`
- Code facts checked: `api/openapi.yaml`、`backend/oapi-codegen.yaml`、`backend/internal/platform/httpapi/router.go`、`backend/internal/platform/store/scope.go`、`frontend/src/App.tsx`、`frontend/src/api/client.ts`

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: codeg_mcp delegated `codex` task `df164914-24b1-432c-a886-15870c0d7c43` +复审 task `1b5a838b-e569-426b-8569-01cd0ada0c87`
- Raw output: 首轮建议 `changes-requested`，指出 OpenAPI §7 同步范围、AccountScope 事务 seam、`POST /customers` 404、A4 原子性四项；主 agent 已修 design/checklist。复审建议 `passed`，无 blocking / important。
- Merge policy: 已逐条用 design / checklist / roadmap / code facts 核验；首轮四项已修复，复审 nit/suggestion 作为非阻塞记录。
- Gate effect: none

## 2. Design Summary

- Goal: 交付 `customer-core` 最小闭环：30 秒建档、客户列表搜索、客户详情页。
- Key contracts: OpenAPI 先收编 roadmap §7 全量机器契约漂移；Go server 仅按 customer-core operation tag 生成三条客户 API；客户读写必须走 tx-aware AccountScope；列表 / 详情聚合字段本轮恒 0/null。
- Steps: 7 步，从契约同步 → AccountScope 扩展 → 客户域持久化 → HTTP 垂直切片 → 前端页面 → 状态接入 → harden/终验。
- Checks: 34 条，覆盖名词契约、编排骨架、流程约束、挂载点、范围守护和 A1-A14 验收场景。
- Baseline / validation: `make check`、`make generate && git diff ...`、`cd backend && go test ./...`、`cd frontend && npm run build`；浏览器计时 / 截图作为 UI 证据。

## 3. Findings

### blocking

none

### important

none

### nit

- [ ] FDR-001 `api/openapi.yaml` 顶部 tag 描述仍有旧名（如 `customer-merge`、`package-price`、`dashboard-today`）。
  - Evidence: 当前 OpenAPI tag 描述与 roadmap item 名不完全一致；design S1 已要求收编 §7 漂移。
  - Impact: 读者噪音，不影响 design 可执行性。
  - Expected fix scope: implementation 的 S1 契约同步里顺手更新描述即可，不扩大业务范围。

### suggestion

- [ ] FDR-002 implementation evidence 建议附一段 “roadmap §7 drift checklist” 摘要。
  - Evidence: design 已把 §7 全量同步列为 S1；当前 `api/openapi.yaml` 漂移分散在客户、套系、提醒、dashboard 多处。
  - Impact: 降低 code review 漏看跨域契约同步的概率。

### learning

- 契约同步和业务实现范围分离是本 feature 的关键：OpenAPI 全量追 roadmap，Go server 只按 operation tag 生成 customer-core 入口，未实现域不注册路由。

### praise

- design 已主动引用 AccountScope 与 OpenAPI 两条历史沉淀，且把首轮 review 的四个阻塞点补进 design/checklist。
- Acceptance Coverage Matrix 覆盖 A1-A14，每个核心场景都有 step、证据类型和命令 / 动作。

## 4. User Review Focus

- 用户需要重点拍板：`channel=referral` 是否在 customer-core 就要求选择介绍人；重复社交身份本轮暂不拦截是否接受；`GET /me` 白名单继续另走 roadmap update 是否接受。
- implement 需要重点遵守：S1 全量 OpenAPI 漂移同步但只注册 customer-core 三条 API；S2 tx-aware AccountScope 不能绕开；本报告原审查的是“客户创建与首个身份同事务”，当前 design 已改为“客户创建与全部身份同事务”，需复审后执行。
- code review / QA / acceptance 需要重点复核：AccountScope 事务与复杂读模型、跨账号不可见、OpenAPI §7 checklist、30 秒浏览器演示、明确不做端点仍 404。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design A1-A14 与 matrix 已覆盖命令、API、UI、范围守护、清洁度 | QA 复核浏览器证据 |
| DoD Contract | pass | E | design 3.y + checklist `dod.commands` 字段完整 | implementation 保持命令一致 |
| Steps and checks traceability | pass | E | checklist steps 7 条，checks 来源覆盖 design 各节 | none |
| Roadmap contract compliance | pass | C | roadmap §4/§7 已读；design S1 收编 §7 全量漂移，不绕开 §4 | `/me` 白名单后续 roadmap update |
| Module interface design | pass | C | Interface 检查覆盖 customer service、HTTP seam、tx-aware AccountScope、dependency strategy | code review 重点复核 S2 |
| Validation and artifacts | pass | E | 必跑命令、截图/录屏、API 响应、diff summary 已写入 design/checklist | none |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- 当前通过的是 design gate，不代表 S1/S2 实现风险低；`api/openapi.yaml` 仍有真实漂移，`scope.go` 当前仍只有 pool-backed CRUD。下游必须重点复核 OpenAPI 大 diff 和 AccountScope 事务 / 复杂查询扩展。
- 30 秒建档的真实可用性只能靠实现后的浏览器计时与截图确认，QA 不得只看自动化测试。

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review；用户确认后把 design `status` 改为 `approved`，进入 `cs-feat-impl`。
