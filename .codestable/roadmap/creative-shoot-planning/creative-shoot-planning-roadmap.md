---
doc_type: roadmap
slug: creative-shoot-planning
status: active
created: 2026-08-02
last_reviewed: 2026-08-06
last_updated: 2026-08-06
confirmed_at: 2026-08-05
confirmation_id: "80e741c3-06ba-407e-8d4c-5a9b71b66702"
tags: [shoot-planning, cosplay, creative-workflow, customer-collaboration, v1]
related_requirements:
  - creative-shoot-planning
  - customer-profile
  - order-tracking
  - schedule-calendar
  - package-catalog
  - reminder-engine
  - telegram-digest
related_architecture:
  - 001-account-scoped-data-model
  - 002-postgresql-as-primary-store
  - 003-monolith-first-gin-openapi
  - 004-avatar-object-store-as-binary-adjunct
follow_up_roadmap: creative-shoot-intelligence
related_artifacts:
  - docs/prototypes/creative-shoot-planning/v2/README.md
  - docs/prototypes/creative-shoot-planning/v2/index.html
---

# 创作型拍摄策划首版

## 1. 目标与拆分决定

现有 CRM 已覆盖客户、订单、档期、提醒与经营，但二次元 cosplay 等创作型拍摄从“已定档”到“已拍摄”之间仍缺少可执行的创作准备层。角色理解、场地、妆造、道具、动作、灯光和 shot list 散落在聊天记录、截图、链接与备忘录中，造成准备遗漏、现场返工和协作信息断裂。

本 epic 只交付**不依赖 AI 与知识库也能成立的首版**：摄影师能低成本摄取已有讨论形成策划，在独立 run mode 现场逐镜执行；客户通过免登录、按商业阶段裁剪的链接提供反馈并认领分工；系统在拍摄前提醒摄影师核对认领项；策划复杂度可生成仅摄影师可见的经营草稿；首版媒体进入生产备份恢复。

2026-08-04 已决定把原 16 条路线拆为两个独立 epic：

- 本 roadmap `creative-shoot-planning`：阶段 1–3、观测证据与首版 hardening；
- 后置 roadmap `creative-shoot-intelligence`：项目研究包、后台知识履约、三层知识、风格统计、AI 缺口检查、文本提案和分镜。

拆分后的结果是：本 epic 可独立批准、完成、发布或停止。后置 epic 不是本 DAG 的自动下一批；只有本 epic 完成、真实证据满足 G1–G3，并由 owner 在后置 roadmap 中批准 G4，才可进入后置 child design。

## 2. 不变量、范围与排除项

### 2.1 硬约束

| ID | 不变量 | 可机械验证的表现 |
|---|---|---|
| H1 | 策划是非强制辅助工具，不是流程节点 | 没有策划时订单、客户、档期、报价和提醒零阻塞、零“缺策划”提示；观测指标不出现在摄影师 UI |
| H2 | 客户协作与定价彻底解耦 | 所有 share-token 响应采用独立白名单 DTO，字段集合不含 price/cost/labor/adjustment/draft 及其衍生信号 |
| H3 | 首版完全不依赖 AI/知识库 | 构建、运行、验收和完成信号不需要模型凭证、provider、知识表或 AI 评测 |
| H4 | 客户自动触达不进首版 | reminder recipient 只有摄影师账号所有者；匿名昵称不成为 Telegram/短信/邮件地址 |
| H5 | 原作素材 display-only | official/fan/screenshot 不可绑定 generation_reference；非法组合在上传、绑定和 lease 层 fail-closed |

### 2.2 本 epic 包含

- 独立 `ShootPlan`，`customer_id`、`order_id` 均可空；
- 创作 brief、镜头顺序、可选 canonical tags、readiness、revision 和状态机；
- 摄取聊天文本、参考链接和参考图片，候选确认后原子落盘；
- 独立移动 run mode、执行时间窗、session、逐条 captured/skipped 与可审计纠错；
- 客户/订单/未来拍摄档期弱耦合关联和摘要；
- 参考素材来源、权利、用途、exact generation、binding/lease 与恢复；
- proposal/full 两级匿名分享、整案或逐条反馈、full-only 分工；
- 面向摄影师的客户认领项检查提醒；
- 结构化复杂度事实、订单价格调整草稿和档期时长草稿；
- 内部观测事实、G1–G3 evidence report；
- 首版跨模块 hardening 与 PG+avatar+planning-media 生产备份恢复。

### 2.3 明确排除

- 任何 LLM、图像模型、提示词、AI 生成、AI 缺口检查或 AI 评测；
- 项目、账号或平台知识库及 operator 研究队列；
- 客户账号、客户门户、实时多人编辑和评论审批工作流；
- 向客户自动发送 TG、短信或邮件；
- 自动抓取小红书/网页、OCR、PDF、视频解析；
- 自动定价、自动修改订单/档期、客户侧议价或变更确认；
- 离线可写 run mode；首版断网明确失败/待重试，不伪装同步成功；
- 跨部署媒体归档、模型训练/微调和跨部署中央知识网络。

### 2.4 前端原型参考与权威优先级

受版本控制的首版原型快照位于 [`docs/prototypes/creative-shoot-planning/v2/README.md`](/Users/samson/workspace/my_project/customer_manage_platform/docs/prototypes/creative-shoot-planning/v2/README.md)，入口为 [`index.html`](/Users/samson/workspace/my_project/customer_manage_platform/docs/prototypes/creative-shoot-planning/v2/index.html)。后续 child design、实现、QA 与验收必须引用该快照，不能依赖受 gitignore 管理的 `frontend/proto-design/planning/` 工作目录。

权威优先级固定为：本 roadmap 定义产品边界、权限、生命周期、错误与事务语义；OpenAPI 定义实现期机器契约；经确认的 child feature design 定义单 feature 内部选择；v2 原型定义信息架构、用户语言、视觉层级、交互路径与关键状态参考。出现冲突时必须回到 planning/update 或 feature design 消解，不能让实现按原型静默猜测。原型内的工程评审注释、静态示例数据、控制栏与动画延迟不进入正式产品 UI，也不是 API 或数据库契约。

## 3. 模块边界与所有权

### 3.1 `shootplanning`

拥有 `ShootPlan` 聚合、brief、Shot、`ReadinessItem`、`PublicPlanScale`、执行时间窗、run session、append-only execution event/current projection、摄取候选与 commit、CRM 关联、内部观测、business facts 和经营草稿。它不保存媒体字节、不校验 share token、不投递提醒、不调用模型。

### 3.2 `planningmedia`

拥有媒体对象、exact generation、来源/权利/用途、binding、lease、GC、module inventory 和 typed content port。它不反查 ShootPlan 业务状态；调用方必须提交 typed holder/proof。不得把现有 avatar store 扩成万能 BlobStore，低层安全 primitive 可共享，领域生命周期和 manifest 必须独立。

### 3.3 `planshare`

拥有 token 签发、hash、过期、撤销、轮换、`view_level` 快照、匿名投影、匿名反馈/assignment、限流、幂等和 `SharedAssetAccessRef`。它只能通过稳定的 plan/media query port 读取白名单数据，不能直接查询订单价格或经营草稿。

### 3.4 既有 `reminder` / `digest`

仍拥有提醒调度、状态与账号所有者投递。新能力只注册 `plan_assignment_checklist` kind 和来源重算 adapter；不新增匿名 recipient，不改变现有 Telegram 绑定语义。

### 3.5 `planningbackup` adapter

首版 hardening 拥有 planning media 进入部署备份包的 manifest、strict reader、preflight、restore 顺序和 runbook。账号 JSON export 与部署级备份不是同一能力，本 epic 不承诺可携带媒体账号导出。

### 3.6 所有权矩阵

| 契约 | 唯一 owner | 消费者 | hardening 是否可首次实现 |
|---|---|---|---:|
| ShootPlan/Shot/ReadinessItem/PublicPlanScale 状态与 revision | shoot-plan-core | ingestion、share、business、后置 epic | 否 |
| RunModeSession/execution event/current outcome projection | shoot-plan-core | 观测、后置采用率 | 否 |
| PlanningReminderAccountGeneration / generation work / typed fence | shoot-plan-core（neutral transaction primitive） | CRM、share、reminder/digest | 否 |
| PlanningArchiveCapabilityState deployment singleton / acknowledgement registry / archive reminder participant seam | shoot-plan-core | share policy、reminder adapter、release tooling | 否 |
| 摄取候选、计时与 atomic commit | plan-ingestion-capture | UI、观测 | 否 |
| Source×rights×purpose 与 binding/lease | planning-reference-assets | ingestion、share、后置 AI | 否 |
| CRM association / schedule projection | shoot-plan-crm-integration | share、reminder、business | 否 |
| token/view level/匿名 mutation/ShareAssignment/SharedAssetAccessRef | plan-share-collaboration | 客户页、提醒、观测 | 否 |
| assignment reminder projection / PlanningReminderGenerationResolution | plan-assignment-reminders | CRM/core/settings/temporal participants、reminder/digest | 否 |
| Settings timezone planning participant adapter / Telegram binding revision / temporal invalidation / digest intent | plan-assignment-reminders（settings seam由caller定义） | settings、read/dashboard、digest sender | 否 |
| business facts 与 typed drafts | plan-business-feedback | 订单/档期确认 UI | 否 |
| 生产 backup schema-v2 与跨模块发布证据 | creative-planning-v1-hardening | ops | 是 |

## 4. 关键数据与行为契约

### 4.1 ShootPlan、Shot 与状态

```text
ShootPlan
  id, account_id
  customer_id?, order_id?, linked_order_snapshot?
  title, subject, creative_brief
  status: draft | ready | in_progress | completed | archived
  revision
  execution_window?
  public_scale?
  business_facts?          # 由 plan-business-feedback additive 扩展
  created_at, updated_at, completed_at?

Shot
  id, plan_id, position, title
  scene?, action?, expression?, composition?, lighting_text?, notes?
  framing_tag?
  lighting_direction_tag?
  lighting_quality_tag?
  palette_tag?
  shot_type_tag?
  readiness_item_ids[]     # ShotReadinessLink 的 API projection
  removed_at?              # 逻辑移出当前方案，保留执行审计
  execution_revision
  revision

ReadinessItem
  id, plan_id
  category: styling | location | prop_equipment | other
  title
  requirement: required | optional
  preflight_status: unchecked | checked
  responsibility_hint: photographer | customer | unassigned
  default_preparation_lead_days?
  revision

ShotReadinessLink
  shot_id, readiness_item_id

PublicPlanScale
  planned_look_count?
  planned_scene_count?
```

`ReadinessItem` 是 plan 级准备事实，一项可以服务多个 Shot；`ShotReadinessLink` 只表达关联，不复制状态。`responsibility_hint` 是从讨论中提取、供摄影师整理的备忘，不是客户承诺；只有 `planshare.ShareAssignment` 才是正式认领。`preflight_status=checked` 只表示拍前核对，现场实际缺失必须来自非 voided 的 `preparation_missing` execution event，两类事实不得互相覆盖或自动同步。

`PublicPlanScale` 由 core 拥有，是匿名 proposal/full 可读取的安全摘要：`planned_shot_count` 从当前未移除 Shot 集合派生，look/scene count 只读上面的显式可空字段；公开拍摄日期、时段与展示时长只从 `PlanExecutionWindow.starts_at/ends_at` 投影。它不读取或推断 `PlanningBusinessFacts`，尤其不得把 `estimated_duration_minutes` 当作客户页“计划拍摄时长”。

canonical tag 是可选规范字段，自由文本仍保留。首版固定有限词表并带 `taxonomy_version`：

- `framing_tag`: `extreme_closeup|closeup|medium_closeup|medium|full|wide|extreme_wide|other`；
- `lighting_direction_tag`: `front|side|back|top|bottom|mixed|natural|other`；
- `lighting_quality_tag`: `hard|soft|mixed|natural|other`；
- `palette_tag`: `warm|cool|neutral|monochrome|high_saturation|low_saturation|mixed|other`；
- `shot_type_tag`: `portrait|action|interaction|environment|detail|silhouette|narrative|other`。

未填写统一为 `unknown`，不得从自由文本静默猜测。后置风格统计的“未观察到某类型”只能相对于对应 `taxonomy_version` 的有限全集和明确样本窗计算。

状态规则：

- `draft→ready` 只检查当前 `ReadinessItem.requirement=required` 的项目是否已 checked；optional 项为空或 unchecked 都不提示、不阻塞；
- `ready→in_progress` 在首次 run session 或摄影师显式开始时发生；没有执行时间窗也可进入，但观测记 unknown；
- `in_progress→completed` 需要每个当前、未移除 Shot 的 current outcome 为 captured 或 skipped；skipped 必须有 `skip_reason`；
- completed 的业务编辑必须显式 reopen，产生新 revision；archived 只读；
- 移除有执行记录的 Shot 必须显式确认并逻辑移出当前方案，不能物理删除 Shot 或 execution event；
- 所有 mutation 使用 expected revision，幂等重放返回首次成功结果，异 body 同 key 返回 409。

### 4.2 执行时间窗、run session 与 execution history

```text
PlanExecutionWindow
  source: manual | schedule_slot
  source_ref?
  starts_at, ends_at, timezone
  live_window_starts_at, live_window_ends_at
  revision

RunModeSession
  id, plan_id, opened_at, last_active_at, closed_at?
  execution_window_revision?
  capture_mode: live | backfill | unknown   # server-derived
  idempotency_key

ShotExecutionEvent
  id, shot_id, plan_revision, session_id?, shot_event_seq
  result: captured | skipped | cleared
  skip_reason?: preparation_missing | time_insufficient |
                location_unavailable | subject_unavailable |
                creative_change | technical_failure | other
  notes?
  checked_at
  capture_mode: live | backfill | unknown   # inherited/server-derived
  supersedes_event_id?
  revision

ShotExecutionEventVoid
  id, shot_id, target_event_id, shot_event_seq
  reason, voided_by, voided_at
  revision

PlanFinalizationSnapshot
  plan_id, plan_revision, finalized_at
  current_shot_ids[]
  outcome_event_refs[{shot_id, event_id, result}]
  preparation_missing_event_ids[]
  execution_fact_revision
```

派生规则：

- `live`：session 的 `opened_at` 落在被快照的 `live_window_starts_at..live_window_ends_at` 内；
- `backfill`：存在时间窗且 session 在 `live_window_ends_at` 之后打开，或在普通编辑页补记；
- `unknown`：没有时间窗、时间窗已失效且无法绑定快照、或 session 在 live window 之前用于演练；
- ScheduleSlot 投影默认 live window 为 `[StartAt-2h, EndAt+2h]`，账号未来可配置但必须记录规则版本；手动时间窗直接保存显式 live window；
- 客户端不能提交 capture_mode；窗口后改动不重写历史 session/event；
- `ShotExecutionEvent` 和 `ShotExecutionEventVoid` 都只追加不覆盖；服务端在同一 Shot 的 `execution_revision` CAS 事务内分配严格递增 `shot_event_seq`，并发使用旧 expected execution revision 返回 409；客户端时间戳不参与排序；
- 新 captured/skipped/cleared 在已有 current outcome 时必须以 `supersedes_event_id` 指向该 outcome event，服务端拒绝非当前引用；`cleared` 表示显式撤销当前勾选并让 current outcome 回到未设置；
- Shot 的 effective event 集排除已有 `ShotExecutionEventVoid` 的 target；current outcome 按最大 `shot_event_seq` 确定。void 当前事件后回退到前一个 effective event；没有前一个时回到未设置。void fact 不能撤回，后续纠正只能追加新 result；
- `captured→cleared`、`skipped→captured` 都新增 result event。误点、错误 skip reason 或其他会污染证据的事件必须追加 void fact 和原因，不能原位写 `voided_at` 或物理删除；
- 后续 captured 不会让早先真实的 `preparation_missing` 消失；只有存在 void fact 的误记不参与观测。Shot 后续被逻辑移出当前方案时，既有 result/void facts 仍可审计；
- completed/archived 禁止直接写 result/void；发现误记时必须显式 reopen，追加 void/新结果后再完成。每次 completion 冻结 `PlanFinalizationSnapshot`：当前 Shot 集、每个 current outcome 的 event ref、截至该 revision 的有效 live preparation-missing event IDs 和 execution fact revision；旧 snapshot 永不改写；
- evidence report 必须绑定明确的 finalization revision。样本在 report 生成后 reopen/re-complete 会使旧 sample/report stale；重新生成 canonical JSON 会产生新 hash，旧 `stage-2-evidence-go` hash binding fail-closed，必须由 owner 重新确认，不能沿用旧批准。

G1 判断“现场打开”只看 distinct plan 是否存在 `RunModeSession.capture_mode=live`，不拿某条 execution event 或时间戳聚集代替 session 事实。

### 4.3 内部观测事实

观测属于后台产品证据，不属于摄影师任务。所有时间以 UTC 持久化，按账号 timezone 分组；事实带稳定 dedupe key，重放不增加分子或分母。

```text
PlanBuildObservation
  plan_id, first_ingestion_at, first_ready_at?
  activity_ticks[], active_seconds
  idle_rule_version
  outcome: first_ready | abandoned

RunObservation
  plan_id, execution_window_revision
  first_live_session_at?
  finalization_revision?
  finalized_shot_count
  captured_shot_count, skipped_shot_count
  preparation_missing_shot_count

ShareObservation
  plan_id, shoot_window_id
  token_generation, view_level
  first_opened_at?
  first_interaction_at?
  interaction_kinds[]

ObservationSampleDecision
  gate_version, plan_id, execution_window_ref
  decision: included | excluded
  reason: eligible | non_production | duplicate_shoot |
          no_execution_window | not_completed | other
  decided_by, decided_at, note?
```

技术上的 `IngestionSession` 只管理一次候选预览与 atomic commit；G2 使用 `PlanBuildObservation`。它从第一次 paste/import 到首次 ready 累计服务端收到的去重 activity tick，连续无交互超过 5 分钟时从第 5 分钟起不计；切后台、刷新恢复与多次导入继续归入同一 plan observation，abandon 单列不进入完成时长中位数但必须报告比例。tick 只含 plan/time/dedupe key，不含编辑正文。UI 不显示“用时是否达标”。

真实样本纳入/排除必须形成 `ObservationSampleDecision` 审计，不能为了过线临时删样本；同一实际拍摄的复制计划以 execution window/source_ref 和 owner 复核去重，排除原因随 evidence report 展示。

### 4.4 摄取候选与链接

```text
ShotCandidate
  candidate_id, source_line_refs[], text, target_layer
  proposed_shot_fields, attached_asset_refs[]

ReferenceLinkCandidate
  candidate_id, url, source_hint?, label?
  classification: unresolved | plan_reference | shot_reference

ReadinessCandidate
  candidate_id, source_line_refs[], title
  category, requirement
  responsibility_hint
  default_preparation_lead_days?

DroppedCandidate
  candidate_id, original_excerpt
  reason: blank | explicit_user_drop | duplicate | unsupported | over_limit
```

纯链接必须成为 `ReferenceLinkCandidate`；“不自动抓取”不等于 dropped。`ReadinessCandidate.responsibility_hint` 只是候选备忘，commit 后也不会变成客户认领；`requirement`、category 与可选 lead days 必须可在预览中编辑。任何 dropped 都能在 commit 前查看原因并恢复。commit 输入包含所有保留/丢弃决定、asset bindings 和 expected plan revision，以单事务写入；媒体上传已成功但 plan commit 失败时保持 unattached 并进入安全 GC 宽限期，不产生半条 Shot 或 ReadinessItem。

### 4.5 媒体来源、权利和用途

| source_class | 可接受 rights_basis | moodboard_display | shot_reference_display | generation_reference |
|---|---|---:|---:|---:|
| official / anime_screenshot / setting_book | citation_or_display | 允许 | 允许 | 禁止 |
| fan / unknown_web | citation_or_display | 允许 | 允许 | 禁止 |
| photographer_owned | ownership_attested | 允许 | 允许 | 允许 |
| licensed | license_recorded | 允许 | 允许 | 仅授权范围允许 |
| customer_supplied | display_consent | 允许 | 允许 | 首版禁止；后置需单独 generation consent |

服务端矩阵是唯一权威。`purpose=moodboard_display` 是匿名分享可读的唯一首版媒体 purpose；匿名端永不接收内部 object key、generation 或 tenant asset ID。

### 4.6 CRM 与订单阶段

- ShootPlan 独立存在；关联客户不要求订单，关联订单必须属于同账号且与 customer 一致；
- 一个订单可关联多份策划；同一订单最多一个未来 shoot slot 的现有不变量保持不变；
- `Order.shot_at` 表示进入 shot 状态的历史时间，不是拍摄前 due；
- `ScheduleSlot(type=shoot).StartAt/EndAt` 是未来拍摄时间事实；slot 改期产生新的 execution window revision，不重写历史 session/event；
- 订单取消或删除时保留 linked snapshot，停止 full token 与 reminder；策划本身不被删除；
- customer merge、order delete/in-use、archived reference 均需显式矩阵；
- 任何关联动作都不自动推动订单或档期状态。

### 4.7 proposal/full 签发与匿名权限

Full eligibility 固定为：已关联同账号订单，且订单状态属于 `scheduled|shot|selected|retouching|delivered|closed`。`consulting|cancelled` 或无订单只能 proposal。

| 能力 | proposal | full |
|---|---:|---:|
| 创作意图、moodboard、风格方向 | 允许 | 允许 |
| 公开拍摄日期/时段、`PublicPlanScale` | 允许 | 允许 |
| 整案反馈 | 允许 | 允许 |
| 逐条 Shot/执行参数 | 禁止 | 允许 |
| 逐条反馈 | 禁止 | 允许 |
| assignment 读取/认领/撤销 | 禁止 | 允许 |
| 价格、成本、工时、adjustment/draft | 禁止 | 禁止 |

请求不合资格的 full 返回 `409 full_view_not_eligible`。成单不升级旧 proposal token；摄影师显式重新签发。订单取消后 full token 下一请求 fail-closed，返回统一 404，不降级成 proposal 以免字段缓存混用。

`SharePolicyV1`的直接显式expiry合法闭区间固定为command数据库墙钟的`[now+1h,now+366d]`。proposal与无公开window的full先取`desired=evaluated_at+30d`；有公开window的full取`desired=execution_window.ends_at+24h`；默认规则是`resolved_default=clamp(desired,evaluated_at+1h,evaluated_at+366d)`。Bearer management/detail projection在proposal/full各自view item内返回独立min/max/resolved default与10分钟`default_quote{evaluated_at,valid_until,view_level,window_revision?,policy_version}`，不能用根级单数policy混淆两个view。UI始终提交绝对`expires_at`与`explicit|quoted_default` closed source；quoted default只在quote TTL内且view/window/policy未变、绝对值可按quote完整重算时接受，过期/漂移409刷新，explicit仍按command-time闭区间验证。canonical保存绝对值与quote/source，exact replay不随retry时钟重新计算。由此past-window下界默认能跨正常HTTP延迟提交，又不把任意显式值放宽到旧边界。

摄影师Bearer工作台的feedback与assignment GET都必须是exact paged projection：feedback按`created_at DESC,feedback_id DESC`、assignment按`claimed_at DESC,assignment_id DESC`稳定cursor，limit默认50/最大100，并返回全部disposition/status历史。feedback只含target/content/disposition/revision/deep-link所需字段；assignment只含typed target/content snapshot/lead snapshot/status/revision/revoke actor/deep-link所需字段。两者均不得返回token generation、selector、secret/commitment、receipt、内部account/customer/order/CRM/eligibility/generation identity；assignment read必须提供摄影师revoke使用的current revision。

proposal/full 的公开规模只消费 core `PublicPlanScale` 与当前 Shot 派生数。客户页如展示“计划拍摄时长”，只能由公开 execution window 的 start/end 计算；look/scene 未知时隐藏，不以 0 补齐。匿名 DTO 不得查询 `PlanningBusinessFacts`，也不得从价格、成本、人员、精修数量、经营预估时长或草稿推导公开摘要。

分享 token 不进入 access/error/analytics 日志正文；匿名页面设置 `Referrer-Policy: no-referrer`、敏感响应 `Cache-Control: private, no-store`，CSP 禁止第三方脚本、图片和遥测把完整 URL 带出站。错误页面和前端监控也只能记录 token fingerprint。

匿名媒体接口：

```http
GET /api/v1/shared/plans/{token}/assets/{ref}/content?v={checksum}
```

每次读取校验 token 当前 generation、plan 未 archived、ref 属于该 token snapshot、view level 允许、purpose 为 moodboard_display、binding active、checksum/exact generation 相符。`SharedAssetAccessRef` 随 token rotate 失效，不能复用 Bearer 媒体 endpoint。

### 4.8 分享互动口径

G3 的 eligible shared shoot 必须同时满足：

1. distinct plan 关联了一个有效未来 shoot slot；
2. 拍摄前签发过 full token；
3. full 页面在拍摄窗口结束前至少成功打开一次；
4. plan 最终 completed；
5. 同一 plan/slot 即使轮换多个 token 仍只算一个样本。

分母是满足以上条件的 distinct plan/slot；分子是其中至少发生一次成功、去重后的整案反馈、逐条反馈或 assignment mutation 的样本。摄影师采纳与否是诊断项，不改变“客户是否互动”的事实。反馈 disposition 后的摄影师视图必须能深链回对应 Shot；整案反馈没有 shot target 时回分享反馈分区。

G3 的准备遗漏率在绑定的 `PlanFinalizationSnapshot` 上计算：分母是 `current_shot_ids` 中的 distinct Shot；分子是 `preparation_missing_event_ids` 所属的 distinct Shot。snapshot 只收录完成时已有、无对应 void fact、`capture_mode=live`、`skip_reason=preparation_missing` 的 events。先 skipped 后 captured 仍保留一次真实准备遗漏；误点必须在 completion 前追加 void fact 后才不计入。不得对 event 条数重复计数，不从 notes 推断，也不得因后续 current outcome、Shot 逻辑移除或页面编辑静默抹掉旧 snapshot；完成后的纠错必须按 §4.2 reopen/re-complete 并让旧 evidence binding fail-closed。

### 4.9 分工提醒

```text
ShareAssignment
  id, plan_id
  assignment_kind: readiness | on_site_support
  readiness_item_id?
  content
  claimed_by_display_name
  preparation_lead_days_snapshot?
  lead_rule_version?
  status: active | revoked
  revision
  revoked_at?, revoked_by?: anonymous | photographer
  claim_receipt_commitment
```

`assignment_kind=readiness` 必须引用当前 `ReadinessItem`；`on_site_support` 可不引用准备项，也没有 lead snapshot/due。`responsibility_hint` 与 readiness 默认提前天数都不会自动创建 assignment 或 reminder，匿名昵称也不保存或推断外部投递身份。

full 匿名认领 readiness 的同一事务读取当前 `ReadinessItem.default_preparation_lead_days`；非空时快照该值，为空时解析账号覆盖或平台默认并记录 `lead_rule_version`，形成不可空的 `preparation_lead_days_snapshot`。该 lead snapshot 在 assignment 生命周期内不可变；ReadinessItem 后续修改只影响未来认领，不改写 active assignment。若快照设置错误，必须撤销该 assignment、修改 readiness 默认值后重新认领，形成新的 assignment ID/revision/source；首版不提供 assignment lead update command。reminder projection：

- recipient：摄影师账号所有者；
- due：只为 active readiness assignment 生成，使用 `ScheduleSlot.StartAt - preparation_lead_days_snapshot`；on-site support 不生成拍前提醒；
- source identity：每个 active readiness assignment 以稳定 `(plan_id, assignment_id)` 成为 projection source；adapter 按 `(account_id, plan_id, slot_id, due_date, lead_rule_version)` 聚合清单，并把 source assignment IDs/revisions 纳入可重复material fingerprint；group ID/Reminder dedup key另作不可复用occurrence identity，withdraw后相同事实恢复必须新建occurrence，旧terminal不复活；
- slot 改期、删除、订单取消、账号timezone、策划归档或 assignment 撤销触发幂等 recompute/revoke；projection equality固定为`fingerprint+valid_until+timezone_snapshot`，三者全同才zero-write；material timezone patch与settings write/per-plan work同事务，same IANA zone为no-op，不同zone即使local date/due/valid_until相同也更新snapshot/revision；
- 无未来 slot 时保持 `unscheduled`，不创建错误 due；
- 现有账号 TG digest 可投递给摄影师，绝不直达 `claimed_by_display_name`。

token rotate/revoke 只终止匿名访问，不删除既有 assignment；assignment 是有独立 revision/audit 的 plan 事实，discriminator全链路固定为`assignment_kind`。claim总是创建新assignment ID/revision 1与activation event revision 1；material revoke严格`revision+1`（首次为2），exact replay不加号，re-claim新ID从1开始。数据库CHECK固定为`active→revoked_at/revoked_by均为空`、`revoked→二者均非空`。anonymous claim/self-revoke从sealed ShareTxScope取得fence view；Bearer摄影师撤销从authenticated TxAccountScope经trusted adapter派生view，禁止两类scope/capability互相伪造；两者取得locked token后复用同一mutation kernel。assignment 创建/撤销与对应 source outbox 在同一事务边界，reminder adapter 幂等重算 group membership；不能出现 assignment 已撤销而旧 source 仍 active。摄影师可撤销。匿名认领前由客户浏览器 CSPRNG 生成一次性 `claim_receipt_secret`，请求只提交 SHA-256 commitment；服务端成功响应和幂等 ledger 都不含 secret，前端只在成功后展示仍保存在当前组件内存中的本地 secret。通用ledger当前24h TTL内可exact replay，过期后按当前状态重跑，不承诺永久恢复selector/assignment响应。后续撤销同时要求当前 full token 与 receipt，不能用昵称证明身份。浏览器丢失 secret 后服务端不能恢复，这是“服务端只存 commitment”的明确产品代价；token 失效本身不应让现场承诺静默消失。

assignment reminder 的 freshness 不以 `occurred_at` 或进程内 drain 作为数据库提交前沿。所有可能改变 current reminder 的 assignment activate/revoke、slot create/update/delete/type/order move、order cancel/delete/unlink、account timezone、plan archive 与 clock-driven shoot-start withdrawal，都通过core-owned sealed `FenceTxView.LockCurrentAccount() → LockedFenceTx` two-phase API共享同一 account-scoped PostgreSQL fence，locked recheck后才取得单调 `source_generation`并在同一事务写durable per-plan generation work；multi-plan按plan ID连续reserve，任一失败整个source事务回滚。sealed API对reserve-before-lock、cross-tx/account token与same-fact duplicate runtime fail closed；business-lock-before-fence只承诺在受支持repository/application路径由typed orchestration、depguard/compile fixture、锁序trace与双连接PG证明，不声称侦测任意直接SQL。work `mutation_kind` 是跨模块closed enum：`crm_plan_link_changed|crm_order_lifecycle_changed|crm_schedule_changed|settings_timezone_changed|plan_archived|assignment_activated|assignment_revoked|shoot_started`；只有assignment kinds要求`source_event_id`。`ShareAssignmentSourceEventV1` 必须携带该 generation，event generation unique，并以包含`plan_id`、由planshare migration拥有的两个固定名reciprocal deferred composite FK与work的account/generation/plan/event ID/kind一一对应。planshare down仅允许从未产生assignment event/work的未发布空数据环境，必须是transactional migration并禁止`NoTransaction`；它不能把golang-migrate advisory lock当writer exclusion，而要在任何空检查/DDL前按固定顺序对core generation-work表、planshare source-event表执行`LOCK TABLE ... IN SHARE MODE`，以与application writer的`ROW EXCLUSIVE`冲突并持锁到commit/rollback。取得两把锁后才检查source/work，非空则原子拒绝并要求整体reset/restore；检查通过才drop约束/自己的event表，绝不删除或重建core work。双连接必须证明writer先提交时down等锁后拒绝且表/约束/generation/work/event原样，down先锁/确认空时writer阻塞并在share schema删除后整体rollback、不留core orphan；空库round-trip与reset/restore后re-up也须真实PG证明。duplicate、orphan、cross-plan或错配不能提交。

每个applied work必须有唯一matching `PlanningReminderGenerationResolution`：assignment使用`event_applied|rebuild_verified`并保留专用Inbox；CRM/archive使用`lifecycle_applied`；settings使用`settings_rebuilt`；shoot-start使用`temporal_applied`；非assignment不得伪造Inbox。consumer只有在对应resolution与projection同tx安全落盘后才能MarkApplied并推进contiguous `applied_generation`；CRM/archive若提交出pending/applied但缺resolution的work是invariant corruption。周期 reconciliation 在 fence 下捕获 target generation：core-owned work reader、planshare-owned safe assignment reader和reminder-owned epoch repository分别提供其owner的plan IDs/claim/materialization，由reminder coordinator去重排序组合；planshare adapter不得claim core work或写epoch。worker按kind验证current authoritative fact、写对应resolution，不能拿source-event identity结算非assignment work。跨页 worker、crash/restart、epoch supersede 与 completion 都从数据库事实恢复，普通 keyset cursor不得冒充stable epoch。每个current group还持久`valid_until=ScheduleSlot.StartAt`；final read/intent与每次物理Telegram attempt在所有必需锁与tentative payload之后，用最终guarded SQL即时`clock_timestamp()`检查最早valid_until，禁止`transaction_timestamp()`。fence/delivery/settings锁等待、payload构建、pre-call crash或retry跨过StartAt时fail closed，并以unique temporal marker幂等安排`shoot_started` work；intent必须保存earliest validity与membership fingerprint，不能要求周期runner先运行才阻止stale清单。

read/digest 在捕获 target 后只允许消费到 `applied_generation >= target`且目标内work均有matching resolution。planning Reminder的MarkDone/Dismiss共享account fence但不推进generation，legacy reminder status行为不变。Settings additive持有`telegram_binding_revision`，本期现有bind/rebind导致material chat变化时递增；首版不新增/验收unbind surface，future unbind必须沿用同规则。sender与recipient mutation共同锁序固定为`fence→Settings/recipient row→Delivery claim/rows→current group/reminder`，bind token不得先锁后再取Settings；进程内`RecipientGate`只是优化。首次intent及每次call start都按该顺序锁定，并在同一business Delivery frozen window内用current timezone/current recipient/latest snapshot重建deterministic payload。Delivery持唯一active intent revision和发送状态；authoritative fingerprint完全相同的response-loss retry可复用exact intent，recipient/status/source/time变化则在同一Delivery内supersede旧revision并移动active pointer。每次physical call的唯一入口在authorizing transaction前记录monotonic method-start、在final statement前记录before-final，guarded CAS同时重检current target/watermark/binding revision/chat/claim/pending status/active revision/source/freshness与DB clock，并原子进入`calling`，不存在另一步未定义的authorized→calling转换；planning `start_deadline=min(db_authorized_at+5s,earliest_valid_until)`，legacy-only为`db_authorized_at+5s`。返回时technical remainder=`5s-method全程`，planning business remainder=`(start_deadline-db_authorized_at)-before-final到返回耗时`，取非负最小值为本地倒计时；因此DB锁等待、final SQL、commit-response与本地排队均不会遗漏，StartDeadline只作DB审计，application wall clock/`time.Until`不授权。返回已耗尽、deadline到达或permit superseded均不调用并重跑，mixed digest移除stale planning但保留fresh legacy。

既有business Delivery独占claim/retry/finalize/status，Intent revision与attempt permit不建立第二套结果权威；`delivery_id/source/source_key/target_local_date/timezone_at_enqueue`对该Delivery冻结，planning due/freshness使用当前已提交timezone后再按frozen window筛选，只有独立业务触发才产生新Delivery。material rebind在同一锁序下等待calling attempt完成或deadline失效，仅supersede**同一daily/command Delivery**上的旧recipient intent、移动active pointer并唤醒该Delivery，禁止换source key或创建第二张business Delivery；pending `binding_ack`保留既有专用supersession。rebind提交后不得再开始向旧chat的新调用。revoke/archive/status/time若先于final calling CAS提交，旧事实不得被调用；CAS先提交也只在planning业务deadline与同一monotonic budget内授权紧邻本次调用，不赋予未来retry永久权。Telegram接收成功但本地finalize前崩溃仍可能在facts未变时重复exact intent；最终calling CAS与最后一次本地deadline检查到外部调用真正开始间仍有不可原子消除的极短微窗口，这是与固定5秒post-StartAt授权或分钟级stale retry不同的at-least-once残余风险。corrupt source event必须在projection回滚后以独立、脱敏事务进入durable quarantine；未解决quarantine阻塞相应account/plan freshness，runner使用持久lease/backoff，只有safe current assignment reader驱动的rebuild/verify才能原子resolve quarantine、写assignment Inbox与universal rebuild resolution并推进watermark，禁止静默skip/ack。

archive acknowledgement 是 additive closed union：`planning-share-v1` 的 exact effects 永久保持 `plan_becomes_read_only`、`execution_history_retained`、`active_share_links_become_unavailable`、`share_feedback_retained`、`share_assignments_retained`；reminder 模块启用后，新 archive 请求必须使用 `planning-share-reminder-v1`，在上述全集末尾增加 `active_assignment_reminders_withdrawn`。core-owned `PlanningArchiveCapabilityState` 是无`account_id`的deployment/schema metadata singleton（`singleton_key="planning-archive-v1"`、closed capability、revision、non-null promoted_at），属于ADR-001非业务数据例外。core up migration在同一transaction确定seed `core-v1/revision=1`，`promoted_at`为bootstrap DB time；fresh show/startup/tx reader必须一致，重复migration/restore不得覆盖已提升marker，migration之外缺失/corrupt marker继续fail closed且promoter不得INSERT/upsert修复。application只通过从current physical transaction派生的sealed `ArchiveCapabilityTxView`读取固定singleton，reader以共享行锁保持到request commit；detail/archive/startup与release CLI复用唯一closed decoder，但startup reader不能授权request或转换为通用accountless Store。package依赖固定`store→planningcapability`，后者不import store/pgx，store只注入same-tx fixed-query closure。只有release-only `planningctl archive-capability` promoter可在trusted live-binary/wiring inventory readiness后，以expected revision做相邻CAS并authoritative readback；readiness digest必须绑定environment/deployment identity、schema/content hash、生成/过期时间、current marker/target、live builds、wiring evidence与control-plane provenance/权限假设。marker缺失、stale/non-adjacent/downgrade/low wiring/readback mismatch均0写非零退出。首个core binary起就必须理解三个v1 marker；lower wiring进程读到更高marker时startup/request fail closed，不能用startup cache授权archive；共享锁确保promotion提交后没有按旧marker授权的archive再提交。

promotion顺序固定为additive schema→部署所有理解高marker的兼容binary→确认旧binary退出并生成trusted readiness inventory→`planningctl`原子CAS marker/readback→启用匹配的真实writer/participant；mixed-version fixture必须覆盖promotion前已启动lower-capability进程及在途旧marker archive/shared-lock等待。一旦达到reminder，暂停新生成也不能降到lower policy/旧binary；未来降级需另立离线协议。server single typed registry、detail projection、OpenAPI/generated Go/TS 与前端确认必须展示并提交 exact ordered effects；不得原位扩写旧 version。core以caller-owned `PlanArchiveReminderParticipant`调用reminder真实adapter；archive在 account fence 下于当前事务通过sealed view读取marker，再锁plan并用同一 `TxAccountScope`关闭session、令archived状态失效分享、撤回current assignment reminder、写`lifecycle_applied` resolution并MarkApplied，任一marker/policy/participant/resolution失败则plan/session/reminder/ledger全部回滚。旧 exact 成功只在通用 ledger 24 小时 TTL 内重放；TTL 后按当前 archived state 返回 `archived_read_only`，不承诺永久 replay。

### 4.10 经营事实与 typed drafts

```text
PlanningBusinessFacts
  rented_location_count?
  assistant_count?
  retouched_photo_count?
  estimated_duration_minutes?
  revision

OrderAdjustmentDraft
  id, plan_id, plan_revision
  order_id, order_target_fingerprint
  base_price?
  lines[{kind, label, quantity?, unit_amount?, amount, source_fact}]
  proposed_total?
  rule_version, created_at, expires_at
  status: fresh | stale | applied | dismissed
  stale_reason?

ScheduleDurationDraft
  id, plan_id, plan_revision
  target_mode: update_existing | create_new
  slot_id?, slot_target_fingerprint?
  order_id, order_target_fingerprint
  original_start_at?, original_end_at?
  proposed_end_at?, basis_minutes
  rule_version, status, stale_reason?
```

公开造型数量由 core `PublicPlanScale.planned_look_count` 唯一拥有，business feature 可将它作为显式输入参与规则解释，不在 private facts 中复制另一份 `look_count`。`rented_location_count`、人员、精修数量和 `estimated_duration_minutes` 仍是仅摄影师可见的经营事实，不能进入匿名投影；`planned_scene_count` 也不能从租赁场地数量猜测。

所有 fact 可空；unknown 不等于 0。平台默认规则和账号覆盖规则均版本化，结果逐行解释；`base_price` 为空时 `proposed_total` 也保持 unknown，除非摄影师显式填写绝对目标价格，不能把加价行之和误当订单总价。现有 Order/Schedule 没有公开 revision，因此 draft 使用服务端 canonical target fingerprint：订单覆盖 `id/customer_id/package_id/status/price`，档期覆盖 `id/type/order_id/start_at/end_at`。确认时在同一事务内锁定目标行、重算 fingerprint；不匹配返回 `409 stale_business_draft`，匹配后才写审计型 `OrderPriceAdjustment` 并更新 `Order.price`，或更新 slot end。没有现有 shoot slot 时，`create_new` draft 只把 order 与 `basis_minutes` 带到现有日历创建流程，摄影师选择 start；最终创建仍走现有 schedule 校验和幂等 command，不能凭 duration 自动占档。plan revision、关联、target fingerprint 或规则版本变化都使 draft stale。生成草稿和查看 diff 不写订单/档期，只有摄影师显式确认才 mutation；本 feature 不要求先给现有 Order/Schedule 全局加 revision。

### 4.11 生产备份与恢复

现有 PG+avatar schema-v1 五文件包保持字节和 strict reader 兼容。首版新增 schema-v2：

- PG dump；
- avatar volume + manifest；
- planning media volume + manifest；
- package metadata/digests。

新 reader 接受 v1/v2，旧 reader 必须明确拒绝 v2；禁止部分恢复后报告成功。restore preflight 校验 schema、deployment identity、所有 manifest、digest、对象缺失/孤儿和空间，再按固定顺序进入停写、替换、迁移、校验、开放。回滚旧应用时必须保留能理解 v2 的 restore 工具/镜像。模块级 fixture 不能替代一次真实 production rehearsal。

## 5. 对外接口边界

以下是规划级 seam，child design 可补字段但不得改变权限和事务语义：

```http
# Bearer + AccountScope
POST   /api/v1/shoot-plans
GET    /api/v1/shoot-plans
GET    /api/v1/shoot-plans/{id}
PATCH  /api/v1/shoot-plans/{id}
POST   /api/v1/shoot-plans/{id}/transitions
POST   /api/v1/shoot-plans/{id}/run-sessions
POST   /api/v1/shoot-plans/{id}/shots/{shotId}/capture
POST   /api/v1/shoot-plans/{id}/execution-events/{eventId}/void

POST   /api/v1/shoot-plans/{id}/ingestions
GET    /api/v1/shoot-plans/{id}/ingestions/{sessionId}
POST   /api/v1/shoot-plans/{id}/ingestions/{sessionId}/preview
POST   /api/v1/shoot-plans/{id}/ingestions/{sessionId}/transitions
POST   /api/v1/shoot-plans/{id}/ingestions/{sessionId}/commit

GET    /api/v1/shoot-plans/{id}/assets
POST   /api/v1/shoot-plans/{id}/assets
POST   /api/v1/shoot-plans/{id}/assets/{assetId}/bindings
DELETE /api/v1/shoot-plans/{id}/assets/{assetId}/bindings/{bindingId}
GET    /api/v1/shoot-plans/{id}/assets/{assetId}/content

GET    /api/v1/shoot-plans/{id}/shares
POST   /api/v1/shoot-plans/{id}/shares
POST   /api/v1/shoot-plans/{id}/shares/{shareId}/rotate
DELETE /api/v1/shoot-plans/{id}/shares/{shareId}
GET    /api/v1/shoot-plans/{id}/feedback?cursor={cursor}&limit={1..100}
POST   /api/v1/shoot-plans/{id}/feedback/{feedbackId}/disposition
GET    /api/v1/shoot-plans/{id}/assignments?cursor={cursor}&limit={1..100}
DELETE /api/v1/shoot-plans/{id}/assignments/{assignmentId}
POST   /api/v1/shoot-plans/{id}/assignment-offers
DELETE /api/v1/shoot-plans/{id}/assignment-offers/{offerId}
GET    /api/v1/shoot-plans/{id}/assignment-reminders

POST   /api/v1/shoot-plans/{id}/business-drafts
POST   /api/v1/shoot-plans/{id}/business-drafts/{draftId}/apply

# no Bearer; dedicated share middleware
GET    /api/v1/shared/plans/{token}
POST   /api/v1/shared/plans/{token}/feedback
POST   /api/v1/shared/plans/{token}/shots/{shotRef}/feedback
POST   /api/v1/shared/plans/{token}/assignments
DELETE /api/v1/shared/plans/{token}/assignments/{assignmentRef}  # current full token + claim receipt
GET    /api/v1/shared/plans/{token}/assets/{ref}/content?v={checksum}
```

匿名中间件不构造 `AccountScope`，不伪造 auth `AccountContext`，也不复用“已认证后默认允许”的 handler。全局 selector resolver 只能读取 planshare token 行并做 constant-time commitment 校验；成功后由 store trusted factory 从该已验证数据库行构造 neutral `platform/txcap` 所有、不可由 HTTP/client 创建的 `ValidatedShareContext` 与 sealed `ShareTransactionCapability`。generic `TransactionRunner[planshare.ShareTxScope]` 只开启一次 physical account-filtered transaction，同时给 executor-only `LedgerTxView` 与 callback-only typed `ShareTxScope`；两者共享 tx 但互不可转换。通用 idempotency executor 据此复用唯一 claim→replay-or-callback→store-success 算法；不得向 HTTP 暴露通用 `AccountScope`/`TxAccountScope`/SQL，也不得复制第二套 ledger。proposal 对 full-only route 统一 404。正式 assignment mutation 在该 physical transaction 内还必须先取得 account planning-reminder fence、分配 generation，再写 assignment、observation 与带 generation 的 source event；generation 分配不得由 HTTP/client 输入。

摄影师 Bearer 侧可以读取反馈/assignment、把反馈标为 adopted|ignored、撤销 assignment，并维护 planshare-owned on-site support offer；匿名撤销使用当前 full token + caller-generated `claim_receipt_secret`。所有匿名/摄影师 mutation 带 Idempotency-Key 和 expected resource revision：同 operation/key 且 resource 与 typed canonical body 全相同才返回首次结果，任一 resource/body 差异返回 409；已撤销的同请求重放成功，过期 revision 的新请求返回 `409 assignment_stale|feedback_stale`，跨 plan/账号或 proposal 访问统一 404。匿名 mutation 另有两层限流：所有请求（含 exact replay 和 conflict request）都先计 IP/token-generation outer attempt ceiling，超限 429 且不得访问 ledger；outer/business counter 是 platform-security trusted PostgreSQL 的显式无 AccountScope 业务数据例外，只保存版本化 HMAC digest/window/count，跨实例/重启持久且故障 fail-closed。IP digest按日轮换，但最长1小时grace内trusted resolver提交current/previous opaque candidates，gate在稳定candidate锁后继续消费仍active previous fixed window；previous未结束时不得创建current预算，双active fail closed，所有IP组合维度一致，跨午夜ceiling/Retry-After沿用原window。业务 mutation quota 只有在 admission 与 ledger immutable frame exact match 且未过期时才对 replay 免扣；admission 不在 executor 前抢 winner，只能由 ledger first callback 在同一 physical transaction 写入并复制 ledger expiry。同 key 异 body/target/receipt 继续计费并在未触发 429 时由 ledger 返回 409；并发 first contender 在 winner commit 前均按 attempt 计费，但只有 ledger winner 能形成 admission，不复制第二 ledger。assignment 撤销与带 generation 的 source event/work 在同一事务边界；consumer/reconciliation 必须推进 contiguous applied watermark，不允许 assignment 已撤销但 future reminder 仍被 freshness 判为 current。

`GET /shoot-plans/{id}/shares` 是Bearer管理projection：固定返回proposal/full各一项的latest generation/effective state/reason/fingerprint/expiry/revision，并cursor分页返回on-site support offers；刷新/跨设备后可继续rotate/revoke/close。该query永不返回selector、secret、commitment、完整URL或客户/order详情。

## 6. Feature 路线与 DAG

YAML `depends_on` 是 implementation 顺序和运行依赖权威。标准 `cs-epic` 必须先完成全部 child design batch，因此 G1–G3 不阻止设计；它们在 goal execution 的指定 feature implementation dispatch 前通过 canonical approval ref 停顿。

1. `shoot-plan-core`（done）：稳定聚合、readiness、public scale、run session、append-only execution history/current projection 与观测地基；已完成 implementation、独立 code review、QA 与 acceptance。
2. `planning-reference-assets`（done）：建立参考素材和用途红线；已完成 implementation、3 轮独立 review（含 owner-cap 窄修复 closure）、QA、acceptance，详见 feature implementation/review/QA/acceptance 报告。
3. `plan-ingestion-capture`：依赖前两条，摄取 Shot/Readiness 候选并形成唯一最小闭环。
4. `shoot-plan-crm-integration`：设计可提前完成；implementation 前要求 `approval-report.md#stage-1-evidence-go`。
5. `plan-share-collaboration`：依赖 core、media、CRM。
6. `plan-assignment-reminders`：依赖 full 协作与 CRM 档期事实。
7. `plan-business-feedback`：设计可提前完成；implementation 前要求 `approval-report.md#stage-2-evidence-go`。对 reminders 的依赖只固定实验顺序，不授权 business 读取 share/reminder 数据。
8. `creative-planning-v1-hardening`：汇合全部首版分支，对齐 tracked v2 原型并完成发布和生产恢复门禁。

```text
shoot-plan-core
├── planning-reference-assets ────────────────┐
│                                             └── plan-ingestion-capture [minimal loop] ─────┐
└─────────────────────────────────────────────┴── [stage-1 execution gate] ─ crm-integration ┤

[core + reference-assets + crm] ── plan-share-collaboration ── plan-assignment-reminders ────┤
[ingestion + crm + reminders] ── [stage-2 execution gate] ── plan-business-feedback ──────────┤
                                                                                               └── creative-planning-v1-hardening
```

方括号是 goal execution checkpoint，不是伪 feature，也不参与 child design admission。全部 8 条先按标准批量完成 design/design-review 和统一设计确认；生成 goal package 时，必须把 `planning-evidence-dispatch-gate` 写入本 roadmap 的 `goal-protocol-feature-loop.md` 和 `goal-plan.md`，并交付可执行 required artifact `.codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py`。该 runner 属于 goal workflow control artifact，不是任一 child implementation artifact；goal package 在派发首个 feature 前先用当前 pending decision 做自测，必须得到 `needs-human` 而不是 command-not-found。driver 在开始目标 feature implementation 前执行 gate：

```text
inputs = approval-report.md + versioned evidence report + target feature slug
passed = 对应 named approval 为 approved 且 evidence hash 与批准记录一致
pending/rejected/mismatch = 不运行 implementation；current_feature_index 不变；
                            goal-state 写 handoff + reason/next；输出 needs-human/failed
resume = typed resume 先把 owner 答案写入 canonical approval；approved 时再把
         goal-state handoff→ready-to-dispatch、清空 handoff_reason/next，index 不变，重跑 gate；
         rejected 时保留 handoff，不得绕过
```

固定 CLI 输入为 `--roadmap <path> --feature <slug> --decision <named-decision> --json`。JSON `status` 只允许 `passed|needs-human|failed|blocked`，exit code 分别为 `0|2|3|4`：pending 或 evidence 尚未产生是 `needs-human`；rejected、approved 但 path 缺失、SHA/gate-version mismatch 或 evidence stale 是 `failed`；schema/feature/decision 不可识别是 `blocked`。所有非 passed 结果都保持 `current_feature_index`、写 handoff reason/next 并停止 implementation。approval 的 canonical binding 继续只有 evidence path、SHA-256 和 gate version；evidence JSON 内的 calculation code/build revision 被 SHA 覆盖，不新增一个 approval `build_revision` 字段。

该 checkpoint 使用 goal protocol 已支持的 `passed|failed|needs-human|awaiting|blocked` gate result 和 handoff 恢复，不新增“部分 child design 已确认”的状态，也不改 `codestable-workflow-next` 的 child batch 语义。approval report 是 durable decision surface；goal-state 只缓存 handoff/index，不能反向构造 owner approval。

## 7. Goal Coverage Matrix

| 完成目标 | Owner item | 证据 |
|---|---|---|
| 独立策划和可选执行时间窗不依赖订单 | shoot-plan-core | 领域 fixture、OpenAPI、无订单浏览器路径 |
| 准备项关联、拍前核对、责任提示和正式认领互不混淆 | core + ingestion + share | readiness/link fixture、candidate commit、assignment negative matrix |
| run mode 只做逐镜执行，实时/补录/未知可区分且纠错历史可重放 | shoot-plan-core | 固定时钟 session/result/void/event-seq/finalization fixture、375/coarse pointer 证据 |
| 摄取到现场执行构成最小闭环且建案 active median ≤10 分钟 | plan-ingestion-capture | 候选/atomic rollback tests、样本报告、浏览器证据 |
| 纯链接保留、参考图安全挂载 | ingestion + reference-assets | candidate fixture、rights/purpose negative matrix |
| CRM 关联正确且无策划时既有流程零提示零阻塞 | crm-integration | order/customer/schedule 集成回归、N+1 query evidence |
| proposal/full 商业边界、PublicPlanScale 和匿名媒体读取安全 | plan-share-collaboration | exact response fields、public window duration、business-facts negative query、签发 409、mutation/asset/token matrix |
| 客户认领项在未来拍摄前提醒摄影师，不误发客户 | assignment-reminders | slot recompute/revoke、recipient negative tests、digest fixture |
| 复杂度生成 typed draft，只有摄影师确认才修改订单/档期 | business-feedback | unknown/formula/stale/CAS/404 tests、UI diff |
| planning media 可随生产包 exact restore | v1-hardening | schema-v1/v2 compatibility、真实 restore rehearsal |
| 首版可发布且与 tracked v2 原型的关键状态、组合故障、响应式、可访问性一致 | v1-hardening | prototype conformance matrix、E2E、failure matrix、1600/1280/375、make check |

唯一最小闭环是 `plan-ingestion-capture` 完成时已有的依赖闭包：

```text
shoot-plan-core + planning-reference-assets + plan-ingestion-capture
→ 摄取已有讨论/图片
→ 原子形成 shot list
→ 打开独立 run mode
→ 逐镜 captured/skipped/纠错且保留执行审计
→ 形成可信内部观测
```

## 8. 真实证据门与持久化

### 8.1 G1/G2：阶段 1 evidence gate

样本资格：distinct completed ShootPlan，具有非 unknown 的执行时间窗，且属于真实创作型拍摄；演示、测试和同一拍摄复制的计划排除。累计 5 个样本后生成版本化 evidence report。

| Gate | 计算 | 通过线 | 不通过 |
|---|---|---:|---|
| G1 现场打开 | 有至少一个 live RunModeSession 的 distinct 样本 / 5 | ≥3/5 | goal 不派发 CRM implementation；回到摄取/run mode 形态 |
| G2 建案成本 | eligible plan 的 PlanBuildObservation.active_seconds 中位数；同时报告 abandon 比例 | ≤600 秒 | 重做摄取；不得用熟练度解释 |

Evidence report 必须列出样本 ID、排除原因、事实版本、分子/分母和计算代码版本，但不得进入摄影师 UI。owner 审阅后，在同一 canonical [approval-report.md](/Users/samson/workspace/my_project/customer_manage_platform/.codestable/roadmap/creative-shoot-planning/approval-report.md) 更新 `stage-1-evidence-go`。全部 child design 可在证据产生前完成；只有该 named decision 为 approved，goal driver 才可从同一 current_feature_index 进入 `shoot-plan-crm-integration` implementation。

Canonical evidence 写入本 roadmap 下的 `evidence/stage-1-g1-g2-{window-id}.json` 和同名 Markdown 解读；JSON 是计算权威。批准时把 JSON path、SHA-256、gate version 与 named decision 原子写入 approval report；内容变化使旧批准失效。

真实样本需要阶段能力进入实际使用环境：goal driver 只负责前三条 implementation/review/QA/acceptance 和授权的 scoped commit，随后在 CRM dispatch gate handoff；publish/release/deploy 仍是独立非自动动作，必须由 owner 另行授权。阶段 1 试点部署、采样和批准完成后，typed resume 恢复同一 goal，不重新生成或跳过已 accepted feature。

### 8.2 G3：阶段 2 evidence gate

累计 5 个符合 §4.8 的 eligible shared shoot 后计算：

- 互动率：至少一次去重成功互动的 distinct plan/slot ÷ eligible shared plan/slot，要求 ≥40%；
- 准备缺失率：按 §4.8 在 completion revision 上计算的 `preparation_missing_shot_count ÷ finalized_shot_count`；与 G1 样本基线比较，要求绝对值下降且 evidence 同时报告区间与小样本限制。

G3 不通过时 goal 不派发 `plan-business-feedback` implementation，先判断客户协作是否仍是主线；不能把整案反馈、逐条反馈、认领重复计算成多个样本。owner 结论写入 `approval-report.md#stage-2-evidence-go`，driver 在同一 index 可恢复重检。

G3 canonical JSON 写入 `evidence/stage-2-g3-{window-id}.json`，采用同样的 path/SHA-256/gate-version approval binding。

同理，协作/提醒 feature accepted 后由 owner 单独授权阶段 2 试点部署；goal 在 business dispatch gate handoff等待 5 个 eligible shared shoot。Goal execution authorization、scoped commit authorization 都不能被解释成 deployment 或 production exposure 授权。

G1 cohort 与 G3 eligible-shared cohort 的选择机制不同；evidence report 必须同时给出原始分子/分母、cohort 差异、可比子集和小样本区间。“准备缺失率下降”只作为方向性证据，不表述为分享/认领的因果效果。

### 8.3 G4：后置 epic 启动 gate

本 epic 不持有阶段 4 节点。G4 在 `creative-shoot-intelligence/approval-report.md#upstream-evidence-go` 中批准，输入必须包括：

- 本 epic 八条全部 `done`，或 dropped 项有 owner 具名决定且 coverage 被接管；
- v1 hardening 与生产恢复通过；
- G1、G2、G3 evidence report 和 owner disposition；
- 摄影师仍愿意持续使用 shot list 的定性证据；
- 后置预算、operator 权限与内容政策仍需单独批准。

缺少 G4 approval 时，后置 roadmap 即使已 review passed 也保持 draft，不得开始 child design、建知识表或调用 provider。

## 9. 完成信号、风险与回退

### 9.1 Epic 完成信号

当且仅当：

1. 八条 item 全部 `done`；dropped 只有在 owner 记录、coverage 接管和重新 review 后才可等价；
2. Goal Coverage Matrix 无缺口；
3. G1–G3 都有真实、可重算 evidence 和 owner disposition；
4. 无策划 CRM 零阻塞、客户零价格信号、客户不被自动触达；
5. PG+avatar+planning-media schema-v2 真实恢复成功；
6. 全仓门禁、匿名公网面安全、可访问性和三个断点证据通过。

首版完成不自动启动 `creative-shoot-intelligence`。

### 9.2 Top 风险

1. **建案成本仍高于聊天/备忘录。** 以 active time 和真实摄取 fixture约束；G2 不过即停止扩展。
2. **现场不打开、事后补勾或纠错覆盖污染数据。** 独立 execution-only route、RunModeSession、窗口快照与 append-only event 作为事实；unknown 不冒充 live，误记必须显式 void。
3. **匿名公网面泄漏策划或经营信息。** 独立 DTO、中间件、full eligibility、token-bound assets、限流和 404 fail-closed。
4. **参考图版权/用途被客户端声明绕过。** 服务端 source×rights×purpose allowlist 在四层拒绝。
5. **提醒误发给客户。** recipient 类型固定 account owner，昵称与地址类型隔离；客户通道完全排除。
6. **媒体存在但备份只含数据库引用。** v1 hardening 将 planning volume 纳入生产包，严格恢复前置校验。

### 9.3 回退与发布

- 数据库和 API 只做 additive migration；旧 UI 不因存在 plan 表而改变；
- planshare 公网路由可独立关闭，关闭不影响 Bearer 侧策划；
- reminder kind 可用当前binary进入暂停生成/受控drain，但core durable archive capability达到`planning-share-reminder-v1`后v1不允许回退lower policy或旧binary；暂停期仍保留真实archive participant并要求该acknowledgement。发布严格执行additive schema→部署理解三个marker的兼容binary→确认旧进程退出并归档trusted readiness inventory→release-only planningctl按expected revision相邻CAS/readback→启用真实writer/participant；startup与每次archive transaction复用唯一decoder，request用same-tx sealed shared-lock reader核对marker，promotion前已启动lower wiring进程在高marker下fail closed，在途旧marker archive提交后promoter才可提交。未来若要降低capability必须另立离线drain/migration协议，不删除assignment历史；
- business draft 可关闭生成，已应用 adjustment 保留审计，不逆向自动改价格；
- schema-v2 发布必须与新 restore 工具成对；应用回滚不允许用旧 reader 处理 v2；
- 媒体删除使用 binding/lease liveness 和宽限期，回滚不得直接清空 volume。

## 10. 验证入口与交付物

### 10.1 基线命令

- 全仓：`make check`；
- 契约：`make generate-check`，前端类型只消费 OpenAPI codegen；
- 后端：按实际包名串行运行 shootplanning/planningmedia/planshare/reminder 集成测试；
- 浏览器：1600、1280、375；run mode 另测 coarse pointer、200% zoom、强光对比；
- 匿名页：无账号 cookie 的干净上下文，proposal/full、撤销/轮换、shared asset 和经营端点 negative matrix；
- 原型一致性：以 tracked v2 的五页 page map 建 conformance matrix；覆盖 empty/loading/error/stale/expired、反馈 adopted/ignored 后跳转 Shot、Run Mode execution-only、断网失败/待重试、captured→cleared、真实 skipped→captured、错误 skip reason→append-only void+新结果且历史仍可见；
- 跨 feature 数据路径：验证 core 编辑 `PublicPlanScale`→proposal/full 安全读取→business 只读消费；验证 readiness 默认 lead→正式 assignment snapshot→reminder source/outbox/recompute，未认领 readiness 和 on-site support 均不生成 due；
- 破坏性确认：解除订单关联、归档策划、移除有执行记录的 Shot、轮换/撤销分享、撤销 assignment、应用经营草稿均验证副作用说明与二次确认；归档在 share-only 能力启用后使用 `planning-share-v1` acknowledgement，精确列出策划只读、执行历史保留、active 分享失效、feedback 保留、assignment 保留；reminder 启用后 additive 要求 `planning-share-reminder-v1` 并追加“active assignment reminders withdrawn”，三种variant均按server projection exact展示且旧version不扩写；正式 UI 不出现“评审注释”等工程语言；
- 运维：schema-v1 restore compatibility、schema-v2 exact restore 和缺失/损坏对象 fail-closed rehearsal。

### 10.2 交付落点

- 后端：`backend/internal/shootplanning`、`backend/internal/planningmedia`、`backend/internal/planshare`，以及既有 reminder/digest adapter；
- 契约：`api/openapi.yaml` 与生成物；
- 迁移：`backend/internal/platform/store/migrations/`，所有业务表带 `account_id`；只有明确列出的pre-auth security counter与`PlanningArchiveCapabilityState` deployment metadata singleton是不带`account_id`的非业务例外；
- Web：策划列表/详情/摄取页、AppShell 外独立且只可执行的 run mode、AppShell 外匿名分享页及失效态、摄影师经营草稿；
- 运维：planning media volume、manifest、backup/restore 工具和 runbook；
- 证据：G1–G3 versioned evidence report、tracked v2 conformance matrix、1600/1280/375 screenshots、E2E、restore rehearsal。

### 10.3 长期知识回写

- 已于 2026-08-05 在首条 child design 前用 `cs-req` 建立“创作型拍摄策划” draft requirement；
- core acceptance 后用 `cs-domain` 固化 ShootPlan/Shot/RunModeSession 统一语言；
- media acceptance 后记录 planning binary adjunct 与 avatar 分离决策；
- share acceptance 后记录首个未认证公网面的投影与 token-bound media 决策；
- v1 hardening 后记录部署备份与账号导出边界；
- 稳定的验证命令或本地环境坑按 `cs-note/cs-keep` 规则沉淀。

## 11. 后置 epic 的输入边界

`creative-shoot-intelligence` 可以消费本 epic 的稳定公开 seam：

- ShootPlan/Shot revisioned query；
- canonical tag + taxonomy version；
- `RunInputSnapshot` 所需的 opaque media access permit；
- source×rights×purpose 判定；
- execution provenance 与 live/backfill/unknown；
- G1–G3 evidence；
- production backup schema-v2 的 additive extension point。

后置 epic 不得反向要求本首版安装 provider、创建知识 schema，或让首版 completion 依赖 AI。若后置设计要求改变这些 seam，必须回 `cs-epic planning update` 并重审两个 roadmap，而不是在 child feature 中私改。

## 12. 当前批准状态

- Roadmap 已由 owner 于 2026-08-05 批准并激活为 `active`，confirmation id 为 `80e741c3-06ba-407e-8d4c-5a9b71b66702`；
- `epic-split` 已由 owner 于 2026-08-04 确认；
- `prototype-alignment` 已由 owner 于 2026-08-05 确认，仅授权固化 v2 原型并同步本轮审计契约；
- `creative-shoot-planning` draft requirement 已由 owner 于 2026-08-05 确认并进入需求中心；
- 独立 roadmap review round 3 已通过；当前 owner 批准只允许进入 requirement gate 与连续 child design batch，不授权实现、Goal execution、commit/push、merge 或 deploy；
- `stage-1-evidence-go` 与 `stage-2-evidence-go` 继续 pending，不因 roadmap approval 或未来全量 design approval 自动放行；
- G1/G2、G3 和后置 G4 均使用各自 canonical named approval，不能由 agent 根据数字自动替 owner 作出 go 决定。

## 13. 变更日志

- 2026-08-05：根据 `plan-assignment-reminders` round-1 design review 修复跨feature提交前沿与归档契约：新增core-owned account generation fence/work、share source event generation、CRM lifecycle fence、materialized reconciliation epoch、durable quarantine/repair、immutable digest send intent，以及不改写旧`planning-share-v1`的additive `planning-share-reminder-v1` exact acknowledgement；产品仍是已批准的“归档撤回提醒”，不新增DAG、AI、客户直达或实现授权。

- 2026-08-05：根据 `plan-share-collaboration` round-4 design review，冻结 global security counter 的 platform/store 持久 owner 与 AccountScope 例外；replay admission 改为只由 ledger first callback 同事务写入并复制 ledger expiry，避免 pre-executor admission 与 ledger 并发选出不同 fingerprint winner。

- 2026-08-05：根据 `plan-share-collaboration` round-3 design review 补齐 anonymous mutation 两层限流：always-on outer attempt ceiling 保护 token validation/ledger，业务 quota exemption 只接受绑定 exact canonical frame fingerprint 的未过期 admission；异 canonical 继续计费并由 ledger 409，admission 不得延长 ledger 恢复窗口。

- 2026-08-05：在 `plan-share-collaboration` round-1/2 design review 后同步匿名 dual-view capability、caller-generated claim receipt、分享/offer Bearer管理query与mutation routes、`planning-share-v1`归档副作用；receipt生成方与不可恢复代价进入focused supplemental owner checkpoint，其余不改变DAG或实现授权。

- 2026-08-05：在 `plan-ingestion-capture` child design admission 前补齐已批准“中断恢复”与 `abandon` 观测所需的 Bearer seams：读取单个 ingestion session、显式 session transition；沿用 AccountScope、session revision 与 Idempotency-Key，不新增解析、抓取、AI、匿名访问或产品范围。

- 2026-08-05：在 `planning-reference-assets` child design admission 前补齐已批准素材 gallery 与 plan/shot attach/detach 所需的 Bearer seams：素材列表、binding 创建和 binding 释放；沿用 AccountScope、expected revision、Idempotency-Key 与同一 planningmedia 生命周期，不新增媒体用途、匿名权限或产品范围。

- 2026-08-05：在首条 child design 前补齐已由策划台账和 Web 交付物承诺的 `GET /api/v1/shoot-plans` 列表 seam；不改变权限、事务、范围或 DAG。
- 2026-08-05：owner 确认 `creative-shoot-planning-v1-2026-08-05-r1`，建立创作型拍摄策划 draft requirement 并解除首条 child design 前置门禁。
- 2026-08-05：把前端原型固化为 `docs/prototypes/creative-shoot-planning/v2/` 受版本控制快照，补充权威优先级与 hardening conformance matrix。
- 2026-08-05：补齐 `ReadinessItem`/Shot link/摄取候选，分离责任提示、拍前核对、客户正式认领与现场缺失；默认 lead 只在正式认领时快照进 assignment/reminder source。
- 2026-08-05：把 mutable capture 收敛为 append-only result/void facts + server event sequence + current/finalization projection，明确撤销、skip→capture、误记 void、reopen/re-complete 与 evidence 失效口径。
- 2026-08-05：新增 core-owned `PublicPlanScale`，公开计划时长只来自 execution window；匿名 DTO 禁止读取 private business facts。
- 2026-08-04：按 owner 决定将原阶段 4–6 拆至独立 `creative-shoot-intelligence` roadmap；本 epic 从 16 条收敛为 8 条首版路线。
- 2026-08-04：唯一 minimal loop 从 `shoot-plan-core` 移至 `plan-ingestion-capture`，并补齐 planning media 依赖。
- 2026-08-04：补齐 RunModeSession、执行时间窗、skip_reason、摄取计时、分享互动分母/分子与 G1–G3 evidence/approval gate。
- 2026-08-04：补齐 proposal/full 签发与匿名 mutation 矩阵、token-bound shared media、客户提醒收窄、typed business drafts、canonical style tags 和 source×rights×purpose 矩阵。
- 2026-08-04：新增 `creative-planning-v1-hardening`，把 planning media 生产备份恢复和首版发布门禁从 AI 尾部移回首版。
