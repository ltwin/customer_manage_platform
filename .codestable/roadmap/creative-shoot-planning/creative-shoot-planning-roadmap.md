---
doc_type: roadmap
slug: creative-shoot-planning
status: draft
created: 2026-08-02
last_reviewed: 2026-08-04
last_updated: 2026-08-04
tags: [shoot-planning, cosplay, creative-workflow, customer-collaboration, v1]
related_requirements:
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
- 独立移动 run mode、执行时间窗、session 与逐条 captured/skipped；
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

## 3. 模块边界与所有权

### 3.1 `shootplanning`

拥有 `ShootPlan` 聚合、brief、Shot、readiness、执行时间窗、run session、capture、摄取候选与 commit、CRM 关联、内部观测、business facts 和经营草稿。它不保存媒体字节、不校验 share token、不投递提醒、不调用模型。

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
| ShootPlan/Shot 状态、revision、canonical tags | shoot-plan-core | ingestion、share、business、后置 epic | 否 |
| RunModeSession/capture_mode/skip_reason | shoot-plan-core | 观测、后置采用率 | 否 |
| 摄取候选、计时与 atomic commit | plan-ingestion-capture | UI、观测 | 否 |
| Source×rights×purpose 与 binding/lease | planning-reference-assets | ingestion、share、后置 AI | 否 |
| CRM association / schedule projection | shoot-plan-crm-integration | share、reminder、business | 否 |
| token/view level/匿名 mutation/SharedAssetAccessRef | plan-share-collaboration | 客户页、提醒、观测 | 否 |
| assignment reminder projection | plan-assignment-reminders | reminder/digest | 否 |
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
  required_readiness_ids[]
  revision
```

canonical tag 是可选规范字段，自由文本仍保留。首版固定有限词表并带 `taxonomy_version`：

- `framing_tag`: `extreme_closeup|closeup|medium_closeup|medium|full|wide|extreme_wide|other`；
- `lighting_direction_tag`: `front|side|back|top|bottom|mixed|natural|other`；
- `lighting_quality_tag`: `hard|soft|mixed|natural|other`；
- `palette_tag`: `warm|cool|neutral|monochrome|high_saturation|low_saturation|mixed|other`；
- `shot_type_tag`: `portrait|action|interaction|environment|detail|silhouette|narrative|other`。

未填写统一为 `unknown`，不得从自由文本静默猜测。后置风格统计的“未观察到某类型”只能相对于对应 `taxonomy_version` 的有限全集和明确样本窗计算。

状态规则：

- `draft→ready` 只检查摄影师显式标记为 required 的 readiness；空的可选层级不提示、不阻塞；
- `ready→in_progress` 在首次 run session 或摄影师显式开始时发生；没有执行时间窗也可进入，但观测记 unknown；
- `in_progress→completed` 需要每个当前 Shot 为 captured 或 skipped；skipped 必须有 `skip_reason`；
- completed 的业务编辑必须显式 reopen，产生新 revision；archived 只读；
- 所有 mutation 使用 expected revision，幂等重放返回首次成功结果，异 body 同 key 返回 409。

### 4.2 执行时间窗、run session 与 capture

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

ShotCapture
  shot_id, plan_revision, session_id?
  status: captured | skipped
  skip_reason?: preparation_missing | time_insufficient |
                location_unavailable | subject_unavailable |
                creative_change | technical_failure | other
  notes?
  checked_at
  capture_mode: live | backfill | unknown   # inherited/server-derived
```

派生规则：

- `live`：session 的 `opened_at` 落在被快照的 `live_window_starts_at..live_window_ends_at` 内；
- `backfill`：存在时间窗且 session 在 `live_window_ends_at` 之后打开，或在普通编辑页补记；
- `unknown`：没有时间窗、时间窗已失效且无法绑定快照、或 session 在 live window 之前用于演练；
- ScheduleSlot 投影默认 live window 为 `[StartAt-2h, EndAt+2h]`，账号未来可配置但必须记录规则版本；手动时间窗直接保存显式 live window；
- 客户端不能提交 capture_mode；窗口后改动不重写历史 session/capture。

G1 判断“现场打开”只看 distinct plan 是否存在 `RunModeSession.capture_mode=live`，不拿某条 capture 或时间戳聚集代替 session 事实。

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
  captured_count, skipped_count
  preparation_missing_count

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

DroppedCandidate
  candidate_id, original_excerpt
  reason: blank | explicit_user_drop | duplicate | unsupported | over_limit
```

纯链接必须成为 `ReferenceLinkCandidate`；“不自动抓取”不等于 dropped。任何 dropped 都能在 commit 前查看原因并恢复。commit 输入包含所有保留/丢弃决定、asset bindings 和 expected plan revision，以单事务写入；媒体上传已成功但 plan commit 失败时保持 unattached 并进入安全 GC 宽限期，不产生半条 Shot。

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
- `ScheduleSlot(type=shoot).StartAt/EndAt` 是未来拍摄时间事实；slot 改期产生新的 execution window revision，不重写历史 capture；
- 订单取消或删除时保留 linked snapshot，停止 full token 与 reminder；策划本身不被删除；
- customer merge、order delete/in-use、archived reference 均需显式矩阵；
- 任何关联动作都不自动推动订单或档期状态。

### 4.7 proposal/full 签发与匿名权限

Full eligibility 固定为：已关联同账号订单，且订单状态属于 `scheduled|shot|selected|retouching|delivered|closed`。`consulting|cancelled` 或无订单只能 proposal。

| 能力 | proposal | full |
|---|---:|---:|
| 创作意图、moodboard、风格方向、镜头规模概览 | 允许 | 允许 |
| 整案反馈 | 允许 | 允许 |
| 逐条 Shot/执行参数 | 禁止 | 允许 |
| 逐条反馈 | 禁止 | 允许 |
| assignment 读取/认领/撤销 | 禁止 | 允许 |
| 价格、成本、工时、adjustment/draft | 禁止 | 禁止 |

请求不合资格的 full 返回 `409 full_view_not_eligible`。成单不升级旧 proposal token；摄影师显式重新签发。订单取消后 full token 下一请求 fail-closed，返回统一 404，不降级成 proposal 以免字段缓存混用。

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

分母是满足以上条件的 distinct plan/slot；分子是其中至少发生一次成功、去重后的整案反馈、逐条反馈或 assignment mutation 的样本。摄影师采纳与否是诊断项，不改变“客户是否互动”的事实。G3 的准备遗漏率为 `preparation_missing captures / finalized shots`，而不是在自由文本 notes 上做推断。

### 4.9 分工提醒

`ShareAssignment` 保存 `claimed_by_display_name`、内容、可选 `preparation_lead_days` 和状态，但不保存或推断外部投递身份。reminder projection：

- recipient：摄影师账号所有者；
- due：`ScheduleSlot.StartAt - preparation_lead_days`，缺省规则版本化；
- grouping：同 plan/slot/due date 聚合为客户认领项检查清单；
- slot 改期、删除、订单取消、策划归档或 assignment 撤销触发幂等 recompute/revoke；
- 无未来 slot 时保持 `unscheduled`，不创建错误 due；
- 现有账号 TG digest 可投递给摄影师，绝不直达 `claimed_by_display_name`。

token rotate/revoke 只终止匿名访问，不删除既有 assignment；assignment 是有独立 revision/audit 的 plan 事实。摄影师可撤销；匿名认领成功时返回一次性 `claim_receipt_secret`（服务端只存 hash），后续撤销同时要求当前 full token 与 receipt，不能用昵称证明身份。token 失效本身不应让现场承诺静默消失。

### 4.10 经营事实与 typed drafts

```text
PlanningBusinessFacts
  look_count?
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
GET    /api/v1/shoot-plans/{id}
PATCH  /api/v1/shoot-plans/{id}
POST   /api/v1/shoot-plans/{id}/transitions
POST   /api/v1/shoot-plans/{id}/run-sessions
POST   /api/v1/shoot-plans/{id}/shots/{shotId}/capture

POST   /api/v1/shoot-plans/{id}/ingestions
POST   /api/v1/shoot-plans/{id}/ingestions/{sessionId}/preview
POST   /api/v1/shoot-plans/{id}/ingestions/{sessionId}/commit

POST   /api/v1/shoot-plans/{id}/assets
GET    /api/v1/shoot-plans/{id}/assets/{assetId}/content

POST   /api/v1/shoot-plans/{id}/shares
POST   /api/v1/shoot-plans/{id}/shares/{shareId}/rotate
DELETE /api/v1/shoot-plans/{id}/shares/{shareId}
GET    /api/v1/shoot-plans/{id}/feedback
POST   /api/v1/shoot-plans/{id}/feedback/{feedbackId}/disposition
GET    /api/v1/shoot-plans/{id}/assignments
DELETE /api/v1/shoot-plans/{id}/assignments/{assignmentId}

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

匿名中间件不构造 AccountScope，不复用“已认证后默认允许”的 handler；它只能得到 `ValidatedShareContext` 和白名单 query port。proposal 对 full-only route 统一 404，避免暴露资源是否存在。

摄影师 Bearer 侧可以读取反馈/assignment、把反馈标为 adopted|ignored、撤销 assignment；匿名撤销使用当前 full token + `claim_receipt_secret`。所有匿名/摄影师 mutation 带 Idempotency-Key 和 expected resource revision：同 key 同 body 返回首次结果，异 body 返回 409；已撤销的同请求重放成功，过期 revision 的新请求返回 `409 assignment_stale|feedback_stale`，跨 plan/账号或 proposal 访问统一 404。assignment 撤销与 reminder recompute/revoke 在同一事务/outbox 边界，不允许 assignment 已撤销但未来提醒仍 active。

## 6. Feature 路线与 DAG

YAML `depends_on` 是 implementation 顺序和运行依赖权威。标准 `cs-epic` 必须先完成全部 child design batch，因此 G1–G3 不阻止设计；它们在 goal execution 的指定 feature implementation dispatch 前通过 canonical approval ref 停顿。

1. `shoot-plan-core`：稳定聚合、run session、capture 与观测地基。
2. `planning-reference-assets`：建立参考素材和用途红线。
3. `plan-ingestion-capture`：依赖前两条，形成唯一最小闭环。
4. `shoot-plan-crm-integration`：设计可提前完成；implementation 前要求 `approval-report.md#stage-1-evidence-go`。
5. `plan-share-collaboration`：依赖 core、media、CRM。
6. `plan-assignment-reminders`：依赖 full 协作与 CRM 档期事实。
7. `plan-business-feedback`：设计可提前完成；implementation 前要求 `approval-report.md#stage-2-evidence-go`。对 reminders 的依赖只固定实验顺序，不授权 business 读取 share/reminder 数据。
8. `creative-planning-v1-hardening`：汇合全部首版分支，完成发布和生产恢复门禁。

```text
shoot-plan-core
├── planning-reference-assets ────────────────┐
│                                             └── plan-ingestion-capture [minimal loop] ─────┐
└─────────────────────────────────────────────┴── [stage-1 execution gate] ─ crm-integration ┤

[core + reference-assets + crm] ── plan-share-collaboration ── plan-assignment-reminders ────┤
[ingestion + crm + reminders] ── [stage-2 execution gate] ── plan-business-feedback ──────────┤
                                                                                               └── creative-planning-v1-hardening
```

方括号是 goal execution checkpoint，不是伪 feature，也不参与 child design admission。全部 8 条先按标准批量完成 design/design-review 和统一设计确认；生成 goal package 时，必须把 `planning-evidence-dispatch-gate` 写入本 roadmap 的 `goal-protocol-feature-loop.md` 和 `goal-plan.md`。driver 在开始目标 feature implementation 前执行 gate：

```text
inputs = approval-report.md + versioned evidence report + target feature slug
passed = 对应 named approval 为 approved 且 evidence hash 与批准记录一致
pending/rejected/mismatch = 不运行 implementation；current_feature_index 不变；
                            goal-state 写 handoff + reason/next；输出 needs-human/failed
resume = typed resume 先把 owner 答案写入 canonical approval；approved 时再把
         goal-state handoff→ready-to-dispatch、清空 handoff_reason/next，index 不变，重跑 gate；
         rejected 时保留 handoff，不得绕过
```

该 checkpoint 使用 goal protocol 已支持的 `passed|failed|needs-human|awaiting|blocked` gate result 和 handoff 恢复，不新增“部分 child design 已确认”的状态，也不改 `codestable-workflow-next` 的 child batch 语义。approval report 是 durable decision surface；goal-state 只缓存 handoff/index，不能反向构造 owner approval。

## 7. Goal Coverage Matrix

| 完成目标 | Owner item | 证据 |
|---|---|---|
| 独立策划和可选执行时间窗不依赖订单 | shoot-plan-core | 领域 fixture、OpenAPI、无订单浏览器路径 |
| run mode 逐镜操作且实时/补录/未知可区分 | shoot-plan-core | 固定时钟 session/capture fixture、375/coarse pointer 证据 |
| 摄取到现场执行构成最小闭环且建案 active median ≤10 分钟 | plan-ingestion-capture | 候选/atomic rollback tests、样本报告、浏览器证据 |
| 纯链接保留、参考图安全挂载 | ingestion + reference-assets | candidate fixture、rights/purpose negative matrix |
| CRM 关联正确且无策划时既有流程零提示零阻塞 | crm-integration | order/customer/schedule 集成回归、N+1 query evidence |
| proposal/full 商业边界和匿名媒体读取安全 | plan-share-collaboration | exact response fields、签发 409、mutation/asset/token matrix |
| 客户认领项在未来拍摄前提醒摄影师，不误发客户 | assignment-reminders | slot recompute/revoke、recipient negative tests、digest fixture |
| 复杂度生成 typed draft，只有摄影师确认才修改订单/档期 | business-feedback | unknown/formula/stale/CAS/404 tests、UI diff |
| planning media 可随生产包 exact restore | v1-hardening | schema-v1/v2 compatibility、真实 restore rehearsal |
| 首版可发布且组合故障、响应式、可访问性通过 | v1-hardening | E2E、failure matrix、1600/1280/375、make check |

唯一最小闭环是 `plan-ingestion-capture` 完成时已有的依赖闭包：

```text
shoot-plan-core + planning-reference-assets + plan-ingestion-capture
→ 摄取已有讨论/图片
→ 原子形成 shot list
→ 打开独立 run mode
→ 逐镜 captured/skipped
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
- 准备缺失率：`skip_reason=preparation_missing` 的 finalized Shot ÷ 全部 finalized Shot；与 G1 样本基线比较，要求绝对值下降且 evidence 同时报告区间与小样本限制。

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
2. **现场不打开或事后补勾污染数据。** 独立 route、RunModeSession 与窗口快照作为事实；unknown 不冒充 live。
3. **匿名公网面泄漏策划或经营信息。** 独立 DTO、中间件、full eligibility、token-bound assets、限流和 404 fail-closed。
4. **参考图版权/用途被客户端声明绕过。** 服务端 source×rights×purpose allowlist 在四层拒绝。
5. **提醒误发给客户。** recipient 类型固定 account owner，昵称与地址类型隔离；客户通道完全排除。
6. **媒体存在但备份只含数据库引用。** v1 hardening 将 planning volume 纳入生产包，严格恢复前置校验。

### 9.3 回退与发布

- 数据库和 API 只做 additive migration；旧 UI 不因存在 plan 表而改变；
- planshare 公网路由可独立关闭，关闭不影响 Bearer 侧策划；
- reminder kind 可停止生成并撤回未来提醒，不删除 assignment 历史；
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
- 运维：schema-v1 restore compatibility、schema-v2 exact restore 和缺失/损坏对象 fail-closed rehearsal。

### 10.2 交付落点

- 后端：`backend/internal/shootplanning`、`backend/internal/planningmedia`、`backend/internal/planshare`，以及既有 reminder/digest adapter；
- 契约：`api/openapi.yaml` 与生成物；
- 迁移：`backend/internal/platform/store/migrations/`，所有业务表带 `account_id`；
- Web：策划列表/详情/摄取页、AppShell 外独立 run mode、AppShell 外匿名分享页、摄影师经营草稿；
- 运维：planning media volume、manifest、backup/restore 工具和 runbook；
- 证据：G1–G3 versioned evidence report、E2E、screenshots、restore rehearsal。

### 10.3 长期知识回写

- 首条 child design 前用 `cs-req` 建立“创作型拍摄策划首版” requirement；
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
- capture provenance 与 live/backfill/unknown；
- G1–G3 evidence；
- production backup schema-v2 的 additive extension point。

后置 epic 不得反向要求本首版安装 provider、创建知识 schema，或让首版 completion 依赖 AI。若后置设计要求改变这些 seam，必须回 `cs-epic planning update` 并重审两个 roadmap，而不是在 child feature 中私改。

## 12. 当前批准状态

- Roadmap 状态保持 `draft`；
- `epic-split` 已由 owner 于 2026-08-04 确认；
- 修订完成后须重新通过独立 roadmap review；
- review passed 只表示可执行，不等于 owner 批准本路线；
- owner 批准前不创建 child feature、不实现、不激活 roadmap、不 commit/push；
- G1/G2、G3 和后置 G4 均使用各自 canonical named approval，不能由 agent 根据数字自动替 owner 作出 go 决定。

## 13. 变更日志

- 2026-08-04：按 owner 决定将原阶段 4–6 拆至独立 `creative-shoot-intelligence` roadmap；本 epic 从 16 条收敛为 8 条首版路线。
- 2026-08-04：唯一 minimal loop 从 `shoot-plan-core` 移至 `plan-ingestion-capture`，并补齐 planning media 依赖。
- 2026-08-04：补齐 RunModeSession、执行时间窗、skip_reason、摄取计时、分享互动分母/分子与 G1–G3 evidence/approval gate。
- 2026-08-04：补齐 proposal/full 签发与匿名 mutation 矩阵、token-bound shared media、客户提醒收窄、typed business drafts、canonical style tags 和 source×rights×purpose 矩阵。
- 2026-08-04：新增 `creative-planning-v1-hardening`，把 planning media 生产备份恢复和首版发布门禁从 AI 尾部移回首版。
