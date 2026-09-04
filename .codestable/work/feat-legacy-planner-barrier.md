---
epic: creative-workspace-redesign
item: ITEM-1B
segment: B1
status: review-closed-round-3
created: 2026-09-04
base_commit: 5535816
creative_epic_revision: ac6b32090a67fff970213fffb135c8bd2ea9f87eacfc27b267a4dd14bd017159
package_epic_revision_before: b61d462194eec4a27aaffbbaf9895f661a3cfd5a402433e19861c2d77c44c64f
---

# ITEM-1B · B1 · 旧策划计价 barrier 裁决、迁移/路由矩阵与 preflight 只读 inventory

本文件是 `creative-workspace-redesign` ITEM-1B 的 B1 段交付物：对 `package-sku-pricing` 全部策划相关条目逐条作出版本绑定裁决，把创作 Epic 迁移矩阵落到表、路由与操作级别，并冻结 pilot preflight 的只读 inventory 与逐类清场清单。B1 只处置旧世界耦合，不依赖 `prototype-shape-go`；B2（canonical requirement 回写）仍等 gate。

裁决三值定义：

- **删除**：该条目从 `package-sku-pricing` 移除，不再是任何 Epic 的交付；对应旧能力不新建。
- **legacy-read-only**：旧能力的历史数据与读面保留，写面在指定版本点关闭；不迁移、不重建。
- **由订单端替代**：该需求仍成立，但由订单/套系域自身字段满足，与策划域零耦合。

## 1. 代码事实（裁决依据）

| 事实 | 位置 | 对裁决的影响 |
|---|---|---|
| 三处 SQL 硬读 `orders.package_id` | `backend/internal/shootplanning/business/repository.go:204-208`（`LoadCurrentTargets`，generate / apply / 旧只读详情共用，且在按草稿 kind 分支之前无条件执行）；`backend/internal/order/business_adjustment.go:39-42`（apply 参与者锁读）；`backend/internal/shootplanning/crm/engine.go:592-597`（`loadOrderFact`，位于订单取消/删除级联路径 `order/repository.go:225 → crm/engine.go applyOrderLifecycle`） | package ITEM-3 删除该列后三处查询必然报错，因此**改代码不可避免**，也**不可能只对 pilot 关闭**。注意：`OrderTarget.PackageID` 是 `*string`（`evaluator.go:375`），删列后置 nil 指纹仍可计算（旧草稿只会变 `order_target_changed` stale），所以「整体退役 `order_adjustment`」不是技术必然，而是 DEC-8 的产品裁决（见 §6）。`schedule_duration` 草稿与旧只读详情语义上不依赖该列，但共用 `LoadCurrentTargets`，要继续可用必须一并改这条 SELECT；CRM 级联要「旧→旧继续生效」也必须改 `loadOrderFact` |
| 草稿决策要求计划处于可变状态 | `backend/internal/shootplanning/business/application.go:837-848`（`mutablePlanStatus`：`completed → reopen_required`、`archived → archived_read_only`；dismiss 走同一守卫 `:381-386`） | **completed 与 archived 计划上的 fresh 草稿都无法 dismiss**，且完成/归档路径都不触碰 `planning_business_drafts`（`execution.go:638-661`、`command_engine.go:764-789`）。因此清场必须在「完成或归档」之前 dismiss；preflight 只把非终态计划上的 fresh 草稿判为阻断，completed / archived 上的残留判为诊断（completed 仍有 reopen → dismiss → 重新完成的出路，作为可选路径写入清单） |
| dismiss 不经 `LoadCurrentTargets` | `backend/internal/shootplanning/business/application.go:381-403` | 删列后 dismiss 仍可用，「dismiss 仍可」的承诺在代码上成立 |
| 归档计划自动关闭未关闭 run session | `backend/internal/shootplanning/command_engine.go:779` | 「未关闭现场」的清场路径可与「归档计划」合并 |
| 计划完成路径也关闭 run session | `backend/internal/shootplanning/execution.go:645` | 同上 |
| 草稿 TTL 24h，但过期不改 `terminal_status`，仍为 `fresh` | `backend/internal/shootplanning/business/model.go:12`；迁移 0029 `planning_business_drafts.terminal_status` | preflight 以 `terminal_status='fresh'` 为唯一谓词，不看 `expires_at` |
| `order_price_adjustments` 由触发器保证 append-only | 迁移 0029 `order_price_adjustments_append_only` | 已应用调整天然只读，无需新增保护 |
| 账号级规则 `planning_business_rule_overrides` 在计价上只被 order evaluator 消费 | `backend/internal/settings/service.go:273-276,340-345`；`frontend/src/account/settings/PlanningBusinessRulesSection.tsx`；`application.go:663-665`（`rules.RuleVersion` 仍是 `schedule_duration` 草稿的 stale 输入） | 订单草稿退役后该设置面在**计价上**无消费者；改规则仍会让活跃档期草稿变 stale，因此写面关闭是产品裁决（避免无意义的 stale 噪音），归 package ITEM-3 例外授权范围（§5） |
| 旧 CRM 关联引擎带 epoch / 双 revision / 事件 / 订单生命周期参与者 | 迁移 0019；`backend/internal/order/repository.go:18-22`（`PlanningOrderLifecycleParticipant`） | 旧计划的订单取消/删除级联继续对旧对象生效（旧→旧），新 `WorkspaceLink` 不接入 |
| 订单/客户页读取旧策划摘要 | `frontend/src/components/orders/OrderWorkspace.tsx:1022`、`frontend/src/pages/CustomerDetailPage.tsx:255`（`PlanningSummaryLink`） | 属 legacy 读面，保留；新关联的反向入口与之并存但层级不同 |
| 现有旧策划路由 | `api/openapi.yaml` 2198-3436 行；`backend/internal/shootplanning/httpcontract/api.gen.go` | 见 §3 路由矩阵；`PATCH /shoot-plans/{id}`（`applyShootPlanCommand`，2301-2333）是旧计划主体（shots / readiness / CRM link）的主写入口 |
| `stale_reason` 与 `OrderBusinessDraftUnavailableReason` 是封闭 enum | `api/openapi.yaml:3838-3841, 3886-3895`（`additionalProperties: false`）；前端映射两处失败模式相反：`frontend/src/planning/businessDraftInput.ts:16`（`Record<UnavailableReason, string>` 穷举，enum 加值后 `tsc` 直接失败，有构建门禁兜底）与 `frontend/src/planning/panels/BusinessPanel.tsx:403-413`（`staleLabel` 为 `Record<string,string>` + `?? value` 静默回落，不补映射就会把 `legacy_planner_retired` 英文原样显示，违反创作 Epic 验收 17） | 新值 `legacy_planner_retired` 是 OpenAPI 契约变更：需改 `api/openapi.yaml`、`make generate` 提交 Go/TS 生成物、**两处**前端映射都补中文。相反 `error.code` 为自由字符串（`:3823-3824`），409 子码 `legacy_read_only` 不需要契约改动 |
| `PATCH /settings` 的 `planning_business_rules` 是隔离字段 | `frontend/src/account/settings/PlanningBusinessRulesSection.tsx:57-84`（payload 只含该一个字段，全仓无第二处发送）；`settings/model.go:35-41` + `httpapi/settings.go:112-124`（`ValidationError` 一律映射为 400 `validation_failed` 并透传 message） | 拒绝依据是**请求体出现该字段**，不影响时区、提醒阈值等其他 settings PATCH；沿用 `validation_failed` + 中文 message，不新增 code（否则要改不在例外授权内的 `httpapi/settings.go`）；该 section 在关闭后仍渲染可点「保存」，需只读化（归属见 §5） |

## 2. `package-sku-pricing` 逐条裁决（版本绑定）

版本绑定含义：`pilot=creative ITEM-2` 指 pilot 账号在创作 Epic ITEM-2 的 capability 启用点生效；`global=package ITEM-3` 指全部账号在 package Epic ITEM-3（items-v2 切换 / 删除 `orders.package_id`）生效；两者取先到者。

| # | package Epic 条目 | 裁决 | 版本绑定 | 说明 |
|---|---|---|---|---|
| 1 | 范围「…策划调价…的完整价格状态转换」 | 删除 | — | 策划不再是订单价格 writer；价格状态机只有订单端入口 |
| 2 | 范围「创作策划划界：按全部有效订单项判断精修是否由套系完整接管」 | 由订单端替代 | package ITEM-3 | 精修计价只由 `item.retouch_qty` × 快照模式决定；不再与策划 `retouched_photo_count` 核对，不存在「接管」概念 |
| 3 | 验收 9（planner contribution / effect key / absolute owner / epoch / rebaseline） | 删除 | — | 不建 `order_plan_price_contributions`，不建 absolute owner，不建 rebaseline endpoint |
| 4 | 验收 10（`extra_shot` / 精修接管 / `package_retouch_allocation_required`） | 删除；精修部分由订单端替代 | package ITEM-3 | 账号级 `extra_retouch` / `extra_shot` 规则不再参与任何订单计价；套系精修超量只按 DEC-2 公式 |
| 5 | 验收 11（策划草稿 order target fingerprint 升版覆盖订单项） | 删除；旧草稿路径 legacy-read-only | global=package ITEM-3 | 不为 items-v2 订单生成新 `order_adjustment` 草稿；切换后 generate 对 `order_adjustment` 这一 kind 返回 `unavailable(reason=legacy_planner_retired)`（按 kind 部分不可用，`schedule_duration` kind 照常；代码已支持 per-kind unavailable，`application.go:238-285`），apply 返回 stale（`stale_reason=legacy_planner_retired`）且零写，dismiss 仍可；两个新 enum 值属 OpenAPI 契约变更；`order_price_adjustments` 读面与旧工作台历史 tab 保留 |
| 6 | 验收 18（0038 迁移、contribution 投影、rebaseline） | 删除 | — | 0038 不建；package 迁移序列止于 0037 |
| 7 | DEC-9（策划贡献按身份替换 / absolute 排他） | 删除 | — | 由新 DEC-9′ 替代：「策划调价历史只读，订单价格唯一 writer 是订单端」 |
| 8 | DEC-10（多项精修覆盖完整性证明） | 删除 | — | 由新 DEC-10′ 替代：「精修计价只依赖订单项自身字段」 |
| 9 | DEC-14 的 0038 planner 部分 | 删除 | — | 0036 / 0037 与 items-v2 切换保留；DEC-14 文本移除 0038 |
| 10 | ITEM-3 验收中的「DEC-10 planner 回归」 | 由订单端替代 | package ITEM-3 | 回归改为「旧 planner 路径 fail-closed 且旧只读面不断」：删除 `orders.package_id` 后，`order_adjustment` generate/apply 明确失败且零写、dismiss 可用、已应用调整可读；`schedule_duration` 草稿与旧「经营草稿」只读详情继续可用；旧计划的订单取消/删除级联继续生效。代码面（package ITEM-3 例外授权，见 §5）：`order/business_adjustment.go:39-42`、`shootplanning/business/repository.go:204-208`、`shootplanning/crm/engine.go:592-597` 三处去 `package_id` 读取；`shootplanning/business/application.go` 产出两个 `legacy_planner_retired` 值；`settings/service.go` 拒绝 `planning_business_rules` 写入；`api/openapi.yaml` 两个 enum 加值并 `make generate` |
| 11 | ITEM-4 整项 | 删除 | — | 子项 ID 保留为已裁决删除的占位，不重新编号 |
| 12 | ITEM-5 对 ITEM-4 的依赖 | 删除 | — | ITEM-5 只依赖 ITEM-3 |
| 13 | ITEM-6 中 planner rebaseline / 策划精修分配 UI | 删除 | — | 订单页不新增任何策划计价入口；旧工作台「经营草稿」tab 按 §3 只读化 |
| 14 | ITEM-7 的 AC18 / rebaseline 内容 | 删除 | — | ITEM-7 验收要点收敛为 16–17；整体验收收敛为 1–8、10–17（9、18 为占位） |
| 15 | 整体验收主链中的「策划 rebaseline / 精修分配 / 增量与 absolute 调价」 | 删除 | — | owner 主链不含任何策划步骤 |
| 16 | 遗留风险 6（两层计价能力并存）、10（旧策划单需 rebaseline） | 删除；新增风险 | — | 新增：「旧 evaluator 代码与 `planning_business_*` 表作为只读兼容资产保留至旧兼容面退役」 |
| 17 | 最终交付索引中的 `shootplanning/business`、planner 契约、迁移 0038 | 删除；保留一处窄例外 | — | `shootplanning/business` 作为包归创作 Epic 的 legacy 兼容面管理，但 package ITEM-3 获得**窄例外授权**：只允许修改 §2 第 10 行列出的去 `package_id` 读取、fail-closed 返回值与 settings 写面拒绝，不得扩展 evaluator 或新增策划写面 |

**pilot 账号的额外停写（creative ITEM-2，不属于 package Epic）**：`POST /shoot-plans/{id}/business-drafts`（两种 kind）、`decision=apply_order_adjustment | apply_schedule_duration`、`shoot-plan.business-facts.v1` 写入、`settings.planning_business_rules` 写入，对 pilot 账号一律 409 `legacy_read_only`；`decision=dismiss` 允许。非 pilot 账号的 `schedule_duration` 草稿与 business facts 写入在 package ITEM-3 后仍保持 legacy-write——前提是 ITEM-3 按 §2 第 10 行改掉 `LoadCurrentTargets` 的 SELECT（语义不依赖 `package_id`，但 SQL 共用），直到旧兼容面退役另行裁决。

**交叉态优先级**：pilot 且已过 package ITEM-3 时，pilot capability 检查先于全局退役判定——`POST /shoot-plans/{id}/business-drafts` 对 pilot 返回 409 `legacy_read_only`，不返回 200 + `unavailable`；两条时间线只收窄写面、不新开写入口，`order_price_adjustments` 由触发器 append-only，因此无双写与悬空。

## 3. 迁移 / 路由矩阵（表与操作级冻结）

对创作 Epic「迁移、灰度与回退矩阵」的落地映射。`non-pilot` 列为 `legacy-write` 账号，`pilot` 列为通过 preflight 后的账号。

### 3.1 表

| 表（迁移） | 处置 | pilot 写 | 说明 |
|---|---|---|---|
| `shoot_plans`、`shoot_plan_shots`、`shoot_plan_readiness_items`、`shoot_plan_shot_readiness_links`、`shoot_plan_execution_windows`（0016） | legacy-only | 禁止 | 只读；不迁成空间/卡片/备忘 |
| `shoot_plan_run_sessions`、`shoot_plan_execution_events`、`shoot_plan_execution_event_voids`、`shoot_plan_finalization_snapshots`（0016） | legacy-only | 禁止 | 新拍摄项执行事实走新表 |
| `planning_reminder_account_generations`、`planning_reminder_generation_work`、`planning_reminder_generation_resolutions`（0016/0019） | legacy-only | 系统内部 | 旧计划的既有世代继续由系统收敛；pilot 不产生新 mutation |
| `planning_archive_capability_state`（0016） | 不变 | — | 与 pilot capability 无关 |
| `planning_media_*`（0017、0031） | compatibility-read + 新 holder | 允许（新 holder） | 媒体身份与权利矩阵复用；ITEM-3 新增创作空间 holder 类型或独立生命周期，不经旧 `plan_id` 路径 |
| `shoot_plan_ingestion_sessions`、`shoot_plan_reference_links`、`shoot_plan_build_*`（0018） | legacy-only | 禁止 | 新批量搬入不经摄取会话 |
| `plan_crm_connections`、`plan_schedule_projections`、`plan_crm_connection_events`（0019） | legacy-only | 系统内部 | 旧计划的订单取消/删除级联继续生效（旧→旧），前提是 package ITEM-3 改掉 `crm/engine.go:592-597` 对 `package_id` 的读取；新 `WorkspaceLink` 不写这些表 |
| `share_generations`、`share_assignment_offers`（0021）、`share_asset_access_refs`、`share_interaction_observations`（0022）、`share_feedbacks`、`share_replay_admissions`（0023）、`share_assignments`、`share_assignment_source_event_v1`（0024） | legacy-only | 禁止（匿名端按 token 生命周期） | 准入前 token 必须过期/撤销，终态链接返回既有失效语义 |
| `plan_assignment_reminder_*`（0025–0028）、`reminders.plan_id` | legacy-only | 系统内部 | 不为新空间生成策划提醒 |
| `planning_business_facts`、`planning_business_drafts`（0029） | legacy-only | 禁止 | pilot 不新增事实、不生成草稿；dismiss 允许 |
| `order_price_adjustments`（0029） | compatibility-read | 不可变（触发器） | 订单审计保留；不撤销、不重算 |
| `settings.planning_business_rule_*`（0029） | legacy-read-only | 禁止 | 全局在 package ITEM-3 后计价上无消费者，写面随之关闭（package ITEM-3 例外授权范围） |

### 3.2 路由

| 路由前缀 / 操作 | non-pilot | pilot | 备注 |
|---|---|---|---|
| `GET /shoot-plans`、`GET /shoot-plans/{id}` 及所有 GET 子资源 | 不变 | legacy-read | 入口在「旧策划记录」历史层级 |
| `POST /shoot-plans`（新建） | 不变 | 409 `legacy_read_only` | pilot 不再新建旧计划；`stop` 后可恢复 |
| `PATCH /shoot-plans/{id}`（`applyShootPlanCommand`：`ShootPlanMutationRequest` 的**全部** operation，不按数量枚举——含 `set_business_facts`、`set_execution_window`、`set_public_scale`、shots / readiness 命令，以及写 `plan_crm_connections` 的 5 条 CRM 命令 `link_customer` / `link_order` / `unlink_order` / `unlink_customer` / `adopt_schedule_projection`） | 不变 | 409 `legacy_read_only` | 旧计划主体的主写入口，pilot 一律 blanket 拒绝，不得按 operation 白名单实现；§2 点名的 business-facts 停写与 DEC-15 防「顺手复用旧 CRM 引擎」均由本行覆盖 |
| `/shoot-plans/{id}/transitions`、`/shots/{shotId}/capture`、`/run-sessions`、`/execution-events/{eventId}/void`、`/ingestion-sessions*`、`/assets*`（写） | 不变 | 409 `legacy_read_only` | 归档/完成属清场动作，在准入前完成 |
| `/shoot-plans/{id}/shares*`、`/feedback*`、`/assignments*`、`/assignment-offers*`（写） | 不变 | 409 `legacy_read_only` | 撤销/终结属清场动作 |
| `/shoot-plans/{id}/business-drafts`（generate） | 不变；package ITEM-3 后 `order_adjustment` kind 返回 `unavailable(legacy_planner_retired)`，`schedule_duration` kind 照常 | 409 `legacy_read_only`（先于全局判定） | |
| `/shoot-plans/{id}/business-drafts/{draftId}/apply`（decision） | 不变；package ITEM-3 后 `apply_order_adjustment` 返回 stale `legacy_planner_retired` | 仅 `dismiss` 允许 | |
| `/shared/plans/{token}*`（匿名） | 不变 | 按 token 生命周期返回既有终态语义 | 不转换为新版分享 |
| `PATCH /settings`（`planning_business_rules` 字段） | 不变；package ITEM-3 后 400 `validation_failed` + 中文 message「经营规则已随旧策划计价退役」（沿用 `abortSettingsError` 现有映射，`httpapi/settings.go:112-124`，不新增 error code、不动 `httpapi/` 包） | 409 `legacy_read_only` | 拒绝依据是请求体出现该字段，不影响其他设置项的 PATCH；`PlanningBusinessRulesSection.tsx` 同步只读化（显示同一文案，去掉保存）；归 package ITEM-3 例外授权 |
| 订单/客户详情的 `planning_summary` | 不变 | legacy-read | 与新 `WorkspaceLink` 反向入口并存，层级为历史 |
| 前端 `/shoot-plans*` 路由与「策划」导航 | 不变 | 导航项降级为「旧策划记录」历史入口，路由保留只读 | 不静默重定向 |

## 4. preflight 只读 inventory 与逐类清场清单（冻结）

preflight 是只读检查：在一个只读事务内按下列谓词逐类计数，任一类非零即拒绝准入，并返回逐类清单；不执行任何清场动作、不写任何表。所有谓词以 `account_id = $1` 限定。

| 类别（清单标题，摄影师语言） | 只读谓词 | 阻断？ | 自行解除路径（顺序敏感） |
|---|---|---|---|
| 未结束的旧策划 | `shoot_plans WHERE status IN ('draft','ready','in_progress')` | 是 | 先处理下方草稿、分享与认领，再对每个计划执行「完成」或「归档」（`POST /shoot-plans/{id}/transitions`）；归档会自动关闭现场会话。完成/归档会预留提醒世代，随后短暂命中「系统仍在整理提醒」，属正常现象 |
| 未关闭的现场会话 | `shoot_plan_run_sessions WHERE closed_at IS NULL` | 是 | 完成或归档所属计划即自动关闭 |
| 仍有效的分享链接 | `share_generations WHERE state='active' AND expires_at > now()` | 是 | 撤销该分享（`DELETE /shoot-plans/{id}/shares/{shareId}`）或等待过期 |
| 未处理的客户反馈 | `share_feedbacks WHERE disposition='pending'` | 是 | 逐条标记「采纳」或「忽略」（`POST .../feedback/{feedbackId}/disposition`） |
| 进行中的客户认领 | `share_assignments WHERE status='active'` | 是 | 摄影师撤销认领（`DELETE .../assignments/{assignmentId}`）；撤销后客户自撤凭证随之失效 |
| 仍开放的现场协助邀请 | `share_assignment_offers WHERE state='open'` | 是 | 关闭邀请（`DELETE .../assignment-offers/{offerId}`）；有进行中认领时先撤销认领 |
| 未处理的策划提醒 | `reminders WHERE type='plan_assignment_checklist' AND status='pending'` | 是 | 在提醒页标记完成或忽略 |
| 系统仍在整理提醒 | `planning_reminder_account_generations WHERE applied_generation < target_generation`，或 `planning_reminder_generation_work WHERE state <> 'applied'`，或 `plan_assignment_reminder_quarantines WHERE resolved_at IS NULL`，或 `plan_assignment_reminder_reconcile_epochs WHERE status='open'` | 是 | 无需操作，稍后重试。**这一类通常是上面清场动作（完成/归档计划、撤销认领）的正常产物**，清单文案不得表述为故障；持续数分钟不消失时联系运维（quarantine 需人工处置）。`plan_assignment_reminder_temporal_invalidations WHERE state='pending'` 不单列：其插入与 `ReserveGeneration(shoot_started)` 同事务（`reminder/temporal_invalidation.go:48-71`），被 `generation_work state <> 'applied'` 传递覆盖；任一侧改动须重验该覆盖 |
| 未决定的经营草稿 | `planning_business_drafts d JOIN shoot_plans p ON d.account_id = p.account_id AND d.plan_id = p.id WHERE d.terminal_status='fresh' AND p.status IN ('draft','ready','in_progress')` | 是 | 在旧策划「经营草稿」中逐条「忽略」；**必须在完成或归档计划之前**完成——完成与归档后都无法再忽略（`mutablePlanStatus`）。注：本行计划集是「未结束的旧策划」行的真子集，它不会成为唯一阻断项；保留它的作用是承载「先 dismiss 再终结」这条顺序约束并在清单中单独呈现，实现时不得视为冗余删除 |
| 已完成 / 已归档计划上残留的经营草稿 | `… WHERE d.terminal_status='fresh' AND p.status IN ('completed','archived')` | 否（诊断） | 无可执行写路径，不构成活跃履约责任，只计数入报告。若摄影师希望清掉 completed 计划上的残留，可选路径为「重新打开 → 忽略草稿 → 再次完成」（`transitions: reopen`，pre-admission 账号仍为 legacy-write）；archived 无出路 |
| 编辑中的摄取会话 | `shoot_plan_ingestion_sessions WHERE state='editing'` | 否（诊断） | 随所属计划归档而失活；只计数入报告 |

补充约束：

- 清单只在账号主动申请准入且被拒时返回；不做提醒、红点或推销入口（创作 Epic 遗留风险 9）。
- 谓词与 API 操作在实现时必须与本表一致；新增或放宽类别属于契约变化，回 Epic 讨论。
- 通过 preflight 后的切换是同一事务内「再次执行全部谓词 + 写 pilot capability」，中间不允许旧写入穿插（ITEM-2 实现细节）。
- 「completed / archived 计划上残留草稿」判为诊断而非阻断，是对创作 Epic 验收 9「未终态 business draft」的一处细化：这类草稿没有任何可执行写路径（`mutablePlanStatus` 对两者都拒绝 dismiss），不构成活跃履约责任；按字面阻断会制造无解阻断。若 owner 希望严格按字面阻断，则需在 ITEM-2 为终态计划开放 dismiss，属新增写面，本 B1 不推荐。

## 5. 顺序与解冻

1. 本文件与 `package-sku-pricing` 修订同时冻结；package Epic 的 planner 相关文本按 §2 移除/替换，其余套系 SKU 与多订单项内容保持 proposed，等待其自身 lineage 的增量 design review 与 owner 确认。
2. 创作 Epic 正文不改（已批准 hash 有效）；barrier 已裁决的事实记入创作 work 游标，指向本文件。若 owner 希望创作 Epic「目标用户与优先级 §1」文字反映已裁决状态，属 hash 替换级改动，需另行确认。
3. `creative ITEM-2` 实现 §3.2 的 pilot 列与 §4 全部谓词；`package ITEM-3` 实现 §2 第 5、10 行的全局退役。**归属例外（与 package Epic 交付索引等价）**：package ITEM-3 获得对后端 `shootplanning/business/{repository.go, application.go}`、`shootplanning/crm/engine.go`、`settings/service.go`，契约 `api/openapi.yaml` 两个 enum 及 `make generate` 生成物，前端 `frontend/src/planning/businessDraftInput.ts`、`frontend/src/planning/panels/BusinessPanel.tsx`（`staleLabel`）、`frontend/src/account/settings/PlanningBusinessRulesSection.tsx`（只读化）的窄修改授权，范围限于去 `package_id` 读取、产出并中文化 `legacy_planner_retired`、以 `validation_failed` + 中文 message 拒绝 `planning_business_rules` 写入与对应设置面只读化；不得扩展 evaluator、不得新增策划写面、不得触碰 `internal/platform/httpapi/` 手写文件（生成物除外）。两者先到者先生效，互不阻塞——前提是 package ITEM-3 一并改掉 `LoadCurrentTargets` 与 `loadOrderFact` 两条 SELECT，否则先到 ITEM-3 时非 pilot 的档期草稿、旧只读详情与 CRM 级联会随删列一起失败。

## 6. 需 owner 知晓的裁决点

- **全局退役而非仅 pilot**：`order_adjustment` 草稿的 generate/apply 在 package ITEM-3 对全部账号关闭。代码层面确定的只有一点：三处 SELECT 硬读 `orders.package_id`，删列后必须改代码，且无法只对 pilot 关闭。改法有二：(a) 把 `OrderTarget.PackageID` 恒置 nil 继续算价（指纹仍可计算，旧草稿自然变 `order_target_changed` stale，不需新 enum）；(b) 整体退役 `order_adjustment`。选 (b) 是 DEC-8 / 新 DEC-9′ 的**产品裁决**：(a) 会让旧 evaluator 以「单一套系」语义继续给多订单项订单写价，正是本轮要拆的耦合。「只退役 apply、保留 generate 做只读预览」同样保留创作域算订单价的引力，不采纳。
- **`schedule_duration` 草稿对非 pilot 保留**：语义不依赖套系列，且创作 Epic 未要求中断非 pilot 账号的旧流程；代价是 package ITEM-3 必须改 `LoadCurrentTargets` 的 SELECT（§5 例外授权）。
- **completed / archived 计划残留草稿判诊断**：见 §4 补充约束末条；completed 有 reopen 出路但不强制。**这是对已批准创作 Epic 验收 9 与迁移矩阵「任一 fresh/stale 但未 dismissed/applied 的草稿使 preflight 失败」字面的实质收窄**，ITEM-2 开工前需 owner 明示接受；不接受则按字面阻断并在 ITEM-2 为终态计划开放 dismiss 写面。
- **归属例外**：package ITEM-3 对创作 legacy 兼容面的窄修改授权（§5 第 3 条）是本 B1 新引入的跨 Epic 边界，若 owner 不接受，替代方案是创作 Epic 另开一个只做去 `package_id` 依赖的子项并成为 package ITEM-3 的前置依赖。
