---
doc_type: roadmap-goal-plan
roadmap: creative-shoot-planning
status: awaiting-authorization
created: 2026-08-06
baseline_ref: 64e1105c0e08b474851431fa80f2ec3d595a85a5
---

# creative-shoot-planning Goal 执行计划

## 1. Scope And Inputs

- Roadmap：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`
- Items：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml`
- Approval：`.codestable/roadmap/creative-shoot-planning/approval-report.md`
- Goal state：`.codestable/roadmap/creative-shoot-planning/goal-state.yaml`
- Runtime protocols：`goal-protocol.md`、`goal-protocol-feature-loop.md`、`goal-protocol-gates.md`、`goal-protocol-audit.md`
- Feature specs：`.codestable/roadmap/creative-shoot-planning/goal-features/*.md`
- Dispatch evidence runner：`.codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py`
- Git baseline：`64e1105c0e08b474851431fa80f2ec3d595a85a5`
- Content confirmation：roadmap 已确认，8 份 child design 已统一批准，8 份 design-review 均为 passed。
- Execution authorization：尚未批准；`goal-acceptance` 与 `goal-commits` 均为 pending。
- Workspace：尚未选择。当前工作区位于 `main` 且含本 Epic 未提交规格；项目规则要求实现分支从 `develop` 建立，Goal 启动前必须由 owner 明确选择 worktree/branch 迁移方案。

本 Goal 只执行首版拍摄策划；AI/provider、AI 脚本/分镜生成、领域知识库维护和后置 `creative-shoot-intelligence` Epic 全部不在范围内。

## 2. Feature Order

| # | Feature | 性质 | 一句话交付物 |
|---:|---|---|---|
| 1 | `shoot-plan-core` | mixed | 独立 ShootPlan/Shot/Readiness、公开规模、执行时间窗、Run Mode 与 append-only 执行审计地基 |
| 2 | `planning-reference-assets` | mixed | 参考素材 rights/purpose 矩阵、typed storage、binding/lease/permit/GC 与模块恢复 |
| 3 | `plan-ingestion-capture` | functional | 聊天/链接/图片低成本摄取为可编辑候选并原子形成可现场执行 shot list 的唯一最小闭环 |
| 4 | `shoot-plan-crm-integration` | mixed | 通过 stage-1 真实证据门后，弱耦合关联客户/订单/拍摄档期并提供无 N+1 摘要 |
| 5 | `plan-share-collaboration` | mixed | proposal/full 免登录分享、token-bound 素材、反馈、认领和严格零价格匿名投影 |
| 6 | `plan-assignment-reminders` | mixed | 将正式 readiness assignment 按未来 shoot slot 投影成只发给摄影师账号所有者的提醒 |
| 7 | `plan-business-feedback` | mixed | 通过 stage-2 真实证据门后生成仅摄影师可见的成本、订单调价和档期时长 typed draft |
| 8 | `creative-planning-v1-hardening` | mixed | 跨模块 E2E、H1/H2、五页原型、a11y、证据、schema-v2 恢复与发布/回退 readiness 收口 |

顺序由 items.yaml 的 DAG 固定。Implementation 只接受依赖严格 `done`；`dropped` 或 design-review passed 不满足实现准入。

## 3. Roadmap Core Acceptance Paths

### 3.1 最小创作执行闭环

在真实 PostgreSQL、真实 API、受控文件卷和浏览器中执行：摄取既有讨论/链接/已上传图片 → 编辑 Shot/Readiness 候选 → 单事务 commit → 打开独立 execution-only Run Mode → captured/skipped/cleared/void/reopen/re-complete → 审计历史与当前 outcome 一致。断网、revision conflict、媒体 participant 失败不得产生半写或伪成功。

### 3.2 客户与 CRM 协作闭环

- 独立策划可选关联 customer/order/future shoot slot；无策划时既有 CRM 页面零提示、零阻塞、零额外 planning mutation。
- proposal/full 根据订单资格签发不可变视图；匿名读取、反馈、认领、撤销、过期/轮换/归档与 shared asset 都走 exact allowlist、统一 404、限流和幂等。
- 任何匿名 API/DTO/query/DOM/bundle 均不出现价格、成本、工时、经营草稿或可推导信号。

### 3.3 Assignment → Reminder 路径

正式 active readiness assignment 才能依据同账号同订单未来 `type=shoot` slot 和冻结 lead snapshot 生成 due；改期、timezone、订单/计划/assignment 生命周期通过 account generation fence 原子重算或撤回。收件人固定为摄影师账号所有者，昵称不是身份，客户不会被自动触达；shoot start 边界通过 DB clock + monotonic budget fail closed。

### 3.4 私有经营草稿路径

摄影师录入可空复杂度事实后，`planning-business-v1` 生成可解释 typed draft；unknown 与 0 严格区分。只有摄影师对 fresh draft 做 exact acknowledgement 后，才在同一事务修改 Order.price 或已有 shoot slot end 并写审计；目标/plan/facts/CRM/rule 漂移均 stale、零目标写。无 slot 只进入普通日历选择开始时间，不自动占档。

### 3.5 生产恢复与 UI 收口

- 五页 tracked v2 在 1600/1280/375、coarse pointer、200% zoom、键盘、读屏、强光对比以及 empty/loading/error/stale/expired 状态下与正式权限/状态机一致。
- schema-v1 五件套保持兼容；schema-v2 七件套加入 planning media，固定 `preflight→stop→replace→migrate→validate→reopen`，missing/orphan/digest/identity/space/failure-stop 全部 fail closed。
- production-shaped rehearsal 只在 owner 提供的受控非生产环境和独立授权下执行；通过 readiness verifier 不等于自动发布或切换生产。

## 4. Evidence Dispatch Gates

Goal execution authorization 不能替代以下真实使用证据：

| Gate | 发生位置 | 当前状态 | 放行条件 |
|---|---|---|---|
| `stage-1-evidence-go` | `shoot-plan-crm-integration` implementation 前 | pending | 5 个合格真实 ShootPlan 的 G1/G2 canonical JSON，由 owner 原子批准 path/SHA-256/gate-version |
| `stage-2-evidence-go` | `plan-business-feedback` implementation 前 | pending | 5 个 eligible shared shoot 的 G3 canonical JSON，由 owner 原子批准 path/SHA-256/gate-version |

固定 runner CLI：

```bash
python3 .codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py \
  --roadmap .codestable/roadmap/creative-shoot-planning \
  --feature <shoot-plan-crm-integration|plan-business-feedback> \
  --decision <stage-1-evidence-go|stage-2-evidence-go> \
  --json
```

`passed|needs-human|failed|blocked` 对应 exit `0|2|3|4`。任何非 passed 都保持 index，写 handoff 并停止；不得用 fixture、设计批准或手工 state 跳过。

## 5. Key Assumptions

1. Go、Node、Docker/Testcontainers、PostgreSQL、浏览器 runner 与本地双文件卷在执行 workspace 可用；核心环境缺失不能用 grep、静态截图或 mock 冒充真实运行。
2. OpenAPI 是机器契约，Go/TS 类型只从 `api/openapi.yaml` 生成；业务 DTO 不手写复制。
3. 所有业务表遵守 AccountScope；只有 roadmap 已明确批准的 pre-auth security counter 与 deployment metadata singleton 属于无 `account_id` 例外。
4. 当前规划相关未提交文件属于待确认的 Goal baseline；是否以及如何迁移到 `develop` 派生分支，必须在启动 checkpoint 由 owner 选择，不能自动 stash/cherry-pick/commit。
5. 真机拍摄样本、外部试点部署、production-shaped 配置、真实凭证和 control-plane 权限不会由 Goal execution 自动获得。

## 6. Top 3 Risks And Mitigations

1. **首批实现规模过大，跨模块 transaction/revision 契约漂移。**
   - 缓解：严格按 DAG 和 approved design/checklist 推进；每条 feature 先 characterise/compile-negative，再真实 PG 故障/并发；实质 scope 变化立刻 handoff，不能在 implementation 私改 roadmap。
2. **匿名公网面或提醒泄露商业/客户身份信息。**
   - 缓解：proposal/full exact DTO、无 cookie 干净上下文、token-bound media、uniform 404、两层限流、recipient negative guard、H2 query/DTO/DOM/bundle 全矩阵；核心缺口不能降为 residual risk。
3. **真实使用/生产恢复证据被 fixture 或本地绿灯替代。**
   - 缓解：两道 dispatch runner 绑定 owner approval + canonical evidence hash；hardening 的 production-shaped rehearsal 单独 owner 授权；证据 bytes/build/config/finalization 变化使旧批准 fail closed。

## 7. Mandatory Validation Commands

各 feature 的完整命令与 evidence list 以对应 `goal-features/*.md` 和 checklist 为准。核心入口汇总如下：

- `shoot-plan-core`：`make generate-check`；shootplanning/platform/backend 串行测试；`npm run test:shoot-planning`；frontend build/lint；`make check`。
- `planning-reference-assets`：planningmedia/immutablefs/avatar/shared-tx/restore 串行测试；`npm run test:planning-media`；生成、build/lint、全仓。
- `plan-ingestion-capture`：ingestion golden/combined commit/observation/evidence；`npm run test:plan-ingestion`；生成、build/lint、全仓。
- `shoot-plan-crm-integration`：stage-1 runner 必须先 passed；CRM/customer/order/schedule PG 测试；`npm run test:shoot-plan-crm`；生成、build/lint、全仓。
- `plan-share-collaboration`：backend 全仓串行、`npm run test:plan-share`、OpenAPI generated diff、prototype v2 supporting runner、`make check`。
- `plan-assignment-reminders`：reminder/planshare/shootplanning/order/schedule/settings/store/dataexport 串行测试、专属 frontend runner、prototype v2 supporting runner、全仓。
- `plan-business-feedback`：stage-2 runner 必须先 passed；business/order/schedule/settings/idempotency 测试、shoot-planning frontend、prototype v2、生成、build/lint、全仓。
- `creative-planning-v1-hardening`：跨模块 backend、shoot-planning unit/e2e、prototype/build/lint、planningbackup selftest、安全脚本、release readiness verifier、`make check`；production-shaped rehearsal 另需 owner 授权。

命令或 package 尚未由上游 feature 真实交付时，按 dependency conformance failure 处理；禁止新增空 script、同名 shim 或假 JSON 使其变绿。

## 8. Final Aggregate Commands

Roadmap 完成前在最终 build 去重重跑：

```bash
make generate-check
cd backend && go test -p=1 ./internal/shootplanning/... ./internal/planningmedia/... ./internal/planshare/... ./internal/reminder/... ./internal/customer ./internal/order ./internal/schedule ./internal/settings/... ./internal/platform/idempotency ./internal/platform/planningcapability ./internal/platform/store/... ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1
cd frontend && npm run test:shoot-planning && npm run test:planning-media && npm run test:plan-ingestion && npm run test:shoot-plan-crm && npm run test:plan-share && npm run test:plan-assignment-reminders && npm run test:shoot-planning:e2e
cd frontend && npm run test:prototype && npm run build && npm run lint
python3 scripts/lib/planningbackup/selftest.py
bash scripts/test-planning-ops-backup-restore-safety.sh
./scripts/verify-planning-v1-release-readiness.sh --evidence <evidence-manifest> --json
make check
git diff --check
python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/creative-shoot-planning
```

功能性核心命令不得因耗时跳过。CMD-009 production-shaped rehearsal 不在自动命令集合中；缺 owner 环境/授权时 hardening acceptance 必须 handoff。

## 9. Preflight Strategy

1. 每次恢复先运行 Epic workflow hook，以 repo facts 决定 next stage；不得靠对话记忆跳转。
2. Goal 启动前解决 workspace checkpoint：禁止在 `main` 写实现；新 workspace 必须基于 `develop` 且能完整、可审计地携带本 Epic 已批准规格。任何 stash/worktree/branch/cherry-pick/commit 都需 owner 所选方案授权。
3. 在选定 workspace 重新确认 baseline、branch、tracked/staged/unstaged/untracked，并区分本 Epic baseline 与任务外 owner 文件；不 reset/clean，不使用 `--no-verify`。
4. 每个 feature 前运行 implementation-ready hook；CRM/business 额外运行对应 evidence dispatch gate。
5. 检查 CodeStable gate scripts、Go/Node/Docker/browser 和卷/磁盘；缺 runner 只能更新依赖/lockfile/既有配置或重装 CodeStable。
6. 凭证仅经 owner-controlled 环境变量/secret reference 注入；不写报告、日志、JSON、截图或仓库。

## 10. Missing Tool Recovery

- 只能补真实测试依赖、锁文件、既有 runner 配置或更新/重装 CodeStable skill 包。
- 禁止新增 `go`、`docker`、`jest`、`pytest.py` 等同名 shim，禁止空脚本、skip flag 或伪造结果。
- Core 环境缺失必须 `needs-human`/handoff；静态扫描只能作为 supporting evidence。
- archguard/meta-cc/provider unavailable 记录 fallback，不自动阻塞基础流程；但影响核心路径的 warning 必须由 review/QA/audit解释或提升 blocking。

## 11. DoD Policy

- Design：approved design + passed design-review + parseable checklist + Acceptance Coverage Matrix + DoD Contract。
- Implementation：当前 feature 全部 steps done；scope-gate、dod-runner、evidence-pack passed；core 命令有真实日志；无未解释范围外 diff。
- Review：独立 Task agent review passed、无 unresolved blocking、消费 evidence/gates 并给 QA focus；遵守同一 feature review 最多 3 轮的项目规则。
- QA：passed；覆盖关键场景、DoD、review focus 与真实 API/browser/CLI/PG/volume 路径，不把核心缺口降为 residual。
- Acceptance：`goal-acceptance` 命名授权有效；checks 全 passed；canonical artifacts/gates/results 完整；items/roadmap/requirements 按设计回写。
- Scoped commit：`goal-commits` 命名授权有效；只包含当前 feature 代码/spec/evidence/review/QA/acceptance、必要 shared roadmap/goal-state 更新；不 push、不绕过 hooks。
- Final audit：features 全 accepted、items terminal、聚合命令/core paths、两份授权、goal consistency 与 goal audit gate 全部通过。

## 12. Gate Policy

- Runtime 权威：`.codestable/roadmap/creative-shoot-planning/goal-protocol-gates.md`。
- implementation.before_review：scope-gate + dod-runner + evidence-pack。
- review.before_pass：review-evidence-gate。
- qa.before_acceptance：qa-evidence-gate。
- acceptance.before_done：acceptance-dod-gate。
- roadmap_audit.before_complete：goal-consistency-gate + goal-audit-gate。
- stage-1/stage-2 dispatch gate 是 implementation admission 的额外 executable gate，不替代上述通用 gates。
- 同一 blocking 三轮仍失败时持久化 handoff，不循环伪修；design review 本轮不再重开。

## 13. Provider Policy

- 本首版不引入 AI/provider 或知识库；任何相关依赖、调用、prompt、模型配置或 provider credential 都是 scope violation。
- CodeStable archguard/meta-cc unavailable 记录为 provider unavailable/fallback，不自动阻塞；warning 必须由独立 review、QA 或 final audit解释。
- meta-cc 首批只消费已有摘要，不为满足 provider 字段伪造输出。
- 最终审计聚合 goal-evidence-summary、provider warnings、E/C/H summary 与 H-only core checks；未批准的 H-only core check必须 handoff。

## 14. Final Audit Deliverables

最终审计至少核验：

- roadmap/items/goal-state/approval-report identity、状态、8-feature 双射和两份 Goal 授权；
- 每个 feature 的 approved design、checklist、review、QA、acceptance、evidence pack/results、scope/DoD/gate JSON；
- 最小摄取/Run Mode、CRM/H1、proposal/full/H2、assignment/reminder recipient、business stale/apply、五页原型/a11y、G1-G3 stale、schema-v1/v2 restore/release readiness 核心路径；
- stage-1/stage-2 evidence decision、path/SHA-256/gate-version 与当前 bytes 一致；
- requirement/CONTEXT/ADR/roadmap/docs/runbook 回写或明确 N/A；
- workspace 已在 owner 选定分支/worktree，tracked/staged/unstaged/untracked、debug/TODO/FIXME/XXX、shim/package/`__pycache__` 与 secret/PII 均清洁；
- `.codestable/roadmap/creative-shoot-planning/goal-audit.md`、goal-evidence-summary 与 consistency gate 实际通过。

## 15. Goal Authorization Policy

- `goal-acceptance`：允许 driver 在 implementation、独立 review、QA、DoD/gates 和真实证据全部通过后，以 `ResumeGoalAcceptance approval-report.md#goal-acceptance` 完成每个 feature acceptance；当前 pending。
- `goal-commits`：允许每个 feature accepted、items/goal-state 持久化且授权仍可机械验证后创建一次 scoped commit；当前 pending。
- 两项必须在 `approval_groups.goal-execution` 中由同一次 owner answer 原子批准，使用同一非空 confirmation ID；不能只批准一项。
- Goal 启动 checkpoint 还必须确定符合 GitFlow 的 workspace；当前 main 不允许直接实现。
- Non-Automatic Actions：remote push、PR、merge、publish、release、deploy、promotion、production migration/restore/cutover、试点曝光、真实采样、production-shaped rehearsal、后置 AI/知识 Epic 均不自动执行，仍需对应流程的独立 owner authorization。
