---
doc_type: feature-review
feature: 2026-08-05-shoot-plan-core
status: passed
reviewer: subagent
reviewed: 2026-08-06
round: 2
lane_a_state: completed
lane_a_ref: "/root/creative_shoot_planning_goal_driver/shoot_plan_core_rereview"
lane_a_reason: "Round 2 全新独立 Task agent 已完成只读完整复审；0 blocking、0 important"
lane_b_state: skipped
lane_b_ref: ""
lane_b_reason: "skipped-scope-ambiguous：未提交 Epic baseline 超出 review-fix current_scope_files；按协议改为本地行级核验明确修复文件，不重复裸扫 workspace"
---

# shoot-plan-core 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md`
- Checklist: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml`
- Evidence pack: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-evidence-pack.md`
- Gate results: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-gate-results.json`
- DoD results: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-dod-results.json`
- Implementation evidence: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-implementation.md`
- Diff basis: Goal baseline `64e1105c0e08b474851431fa80f2ec3d595a85a5` 到当前 unstaged/untracked Epic worktree；scope-gate 固定 allowlist 已通过，staged 为 none。
- Review mode: full-rereview
- Baseline dirty files: 首条 feature 获准包含已做 scope attribution 的 roadmap/design/prototype baseline；finding 只落当前 core 人写实现与契约。

### Independent Review

- Round 1 环节 A：独立 Task agent `/root/creative_shoot_planning_goal_driver/shoot_plan_core_review` 已完成。
- Round 1 环节 B：OCR CLI 已完成，审查 70 个文件并输出 39 条评论；High 经仓库事实核验后进入 blocking/important，Medium 仅进入 nit/suggestion，Low 丢弃。
- Round 2 环节 A：全新独立 Task agent `/root/creative_shoot_planning_goal_driver/shoot_plan_core_rereview` 已完成，结论为 PASS；未发现 review-fix 引入的新 production bug。
- Round 2 环节 B：未重复裸跑 OCR。当前 dirty tree 包含整个未提交 Epic baseline，scope 无法安全归因；按 `skipped-scope-ambiguous` 处理，并由主线程对明确 review-fix 文件做本地行级核验。
- Merge policy: 已丢弃 Round 1 OCR 对 `.codestable/`、dot directory、后置 feature 原型和禁止 surface 的命中；Round 2 结论已逐条与当前源码、测试和 fresh gate 证据核验。
- Gate effect: 本轮满足 `reviewer: subagent` 独立审查锚点；可进入 Goal lane 的 review evidence gate 和 QA。

## 2. Diff Summary

- 新增：shootplanning domain/application/repository/migration/contract，planningcapability/txcap/planningctl，HTTP adapter/tests，planning frontend/pages/tests，feature evidence。
- 修改：OpenAPI/codegen、idempotency/store transaction seams、server/router/envelope、AppShell/App routes、Makefile/package scripts及批准的 roadmap/requirements/architecture baseline。
- 删除：仅删除已被真实 HTTP adapter compile assertion 取代的 S1 临时 compile harness。
- 未跟踪 / staged：批准 Epic baseline 与 core 实现多为 untracked；staged 为 none。
- 风险热点：账号隔离、migration、幂等同事务、顺序数据完整性、双 revision、append-only replay、archive capability promotion、typed error contract、Run Mode offline/UI。

## 3. Adversarial Pass

- 假设的生产 bug：软移出后位置推导错误、非法 reorder 污染顺序、公开响应与 ledger 快照不一致、归档确认 stale 时客户端无法恢复。
- 主动攻击过的反例：移除非末尾镜头后尾插、重复/缺失/额外/cross-plan reorder、create 后修改再原 key replay、cleared/void 后 UI 移除、stale archive acknowledgement、跨账号 fingerprint、生成子命令失败传播、不可构造 TS discriminated union。
- 结果：Round 1 的九项 material finding 已全部关闭或有经批准契约支持的驳回；Round 2 没有发现新的 blocking/important。

## 4. Findings

### blocking

none。

### important

none。

### nit

- [ ] NIT-001 `backend/internal/shootplanning/execution.go:207,385` capture/void ledger 保存 `Status: 200`，公开 HTTP 首次与重放固定返回 201。
  - Evidence: 应用层当前只解码 ledger body，handler 固定 `http.StatusCreated`，所以客户端首次与重放仍一致；内部 ledger status 尚未成为用户可见错误。
  - Impact: ledger 元数据不忠实表达公开响应；未来通用 replay/audit adapter 若采用 stored status，可能形成差异。
  - Disposition: 不阻塞本轮；QA 显式检查 ledger status/body 与公开响应，并作为后续 hygiene 修复候选。此处不改生产代码，避免在 Round 2 通过后触发额外完整复审。

- [ ] NIT-002 `frontend/src/planning/panels/ShotsPanel.tsx:142`、`ReadinessPanel.tsx:100`、`ShootPlanWorkspacePage.tsx:207` 仍有三个 `as PlanCommand/PlanTransition`。
  - Evidence: 当前 select/input 来源受控，未找到可构造非法生产请求的路径；但断言会削弱未来 OpenAPI enum/schema 漂移的编译期保护。
  - Impact: standards hygiene 与长期类型安全风险，不影响 REV-004 所针对的冲突 discriminator closure。
  - Disposition: 不阻塞本轮；记录为 QA/后续类型卫生项，不追加 Round 3。

### suggestion

- SUG-001：在真实 PostgreSQL 回归中补全 reorder 的 missing、extra、cross-plan 三类矩阵；当前 duplicate 已覆盖，算法在写 position 前已统一拒绝四类非法集合。
- SUG-002：明确 populated planning ledger 上的 down-migration 政策。当前恢复旧 operation CHECK 会 fail closed，不会静默删数据，但会阻止直接回滚。

### learning

- 带软删除的有序集合应以 current `MAX(position)+1` 表达尾插，不应以 current count 推导物理位置。
- 幂等 ledger 的 stored body 必须是公开边界真正返回的权威快照；create 已改为在同一事务内组装并保存完整 201 body。
- execution fact、per-shot revision、plan-level fact revision 与 finalization snapshot 分层清晰，CAS 与 plan/shot 锁形成可解释的失败恢复路径。

### praise

- `REV-001` 同时由 position 算法、partial unique index 与真实 PG 回归关闭，而非仅调整 UI 排序。
- `REV-003` 在业务事务内完成 detail/capability 投影和 ledger 持久化，后续 mutation 不会改变 create replay body。
- `REV-007` 使用 account/operation/raw-key 的 NUL 分隔 domain frame，并由直接读取 PostgreSQL BYTEA 的回归验证。
- `REV-009` 采用 additive typed details union，保留 order/schedule 既有冲突结构兼容性。
- core 没有引入 AI/provider/知识库、匿名分享、媒体资产、CRM 关联或经营数据 surface。

### Round 1 Closure

| Finding | Round 2 结论 | 核验依据 |
|---|---|---|
| REV-001 | closed | 无锚点新增使用 current `MAX(position)+1`；PG 覆盖 reorder→移出非末尾→再尾插。 |
| REV-002 | closed | `sameStringSet` 双侧唯一且精确集合；验证发生在 position 写入前，并校验逐 ID affected rows。 |
| REV-003 | closed | create 在同一幂等事务内保存完整 `PlanDetail` 201 body；HTTP exact replay 测试通过。 |
| REV-004 | closed | link/unlink 与 mark-ready/start/reopen 使用可构造独立 schema；Go/TS 已 canonical codegen。 |
| REV-005 | rejected-valid | 批准 design 将 trusted control-plane inventory 定义为 identity/provenance 信任源；新增外部 identity 会改变已批准模型。 |
| REV-006 | closed | UI 以 retained `execution_history[].shot_id` 判断是否要求移除确认。 |
| REV-007 | closed | fingerprint 绑定 `account_id NUL operation NUL key`；数据库 exact-byte 回归通过。 |
| REV-008 | closed | `generate-check` 使用 `set -eu`；`MAKE=false` 失败可传播。 |
| REV-009 | closed | stale archive acknowledgement 返回 authoritative typed 409，且兼容 order/schedule details。 |

## 5. Test And QA Focus

- Reorder fail-closed：duplicate、missing、extra、cross-plan 均返回 validation，plan revision、Shot 顺序和 position 不变。
- Capture/void 幂等：首次与重放均 201 且 body 精确一致；直接检查 ledger body/status；网络超时重试不新增第二条事实。
- Create 幂等：首次 201 后修改 plan，再用原 key/body 重放，仍逐字节返回首次 body。
- 执行并发：同一 Shot 相同 expected execution revision 只能一个提交；complete 与 capture/void 并发保持一致 snapshot。
- 历史保留：captured→cleared/void 后 UI 移除仍提交 acknowledgement=true。
- Archive 原子性：participant 失败整事务回滚；capability 中途提升 fail closed；stale effects 返回 authoritative typed 409。
- Migration：fresh up/down/re-up；populated ledger down 行为与正式 rollback policy 一致。
- 前端：合法 generated union 可直接构造；375px、200% zoom、coarse pointer、离线恢复、多 Shot Run Mode pending-key 重试。
- 详情并发：capture/void 高频写入时观察 READ COMMITTED 多语句投影是否出现用户可见短暂混合；确认 409 后刷新可恢复。
- Fresh evidence：scope-gate、DoD runner、evidence-pack 均 passed；`git diff --check` passed。scope 的 4 个 warning 只来自后续 checklist 中禁止 TODO/FIXME 的验收文本；Vite chunk 提示为非阻塞。

## 6. Residual Risk

- `GetPlan` 在默认 READ COMMITTED 下用多条查询组装详情；并发 capture/void 时可能出现短暂混合投影。CAS 可防数据损坏，QA 需验证用户可见影响和刷新恢复。
- deployment identity 依赖已批准的 trusted control-plane inventory；错误配对 DATABASE_URL/inventory 属于 runbook/运维信任边界。
- reorder 的完整非法输入矩阵尚未全部由独立 PG case 锁定；当前实现逻辑已统一 fail closed。
- archguard binary 与 meta-cc realtime summary 不可用；按 Goal provider policy 为 non-blocking，QA/acceptance 继续显式记录。

## 7. Verdict

- Status: passed
- Next: 运行 `review-evidence-gate`，通过后进入 `cs-feat` Goal lane QA。

## 8. Focused Closure

none；本轮 review-fix 包含生产行为与公开契约变化，已完成 Round 2 全新独立完整复审，未走 focused closure。
