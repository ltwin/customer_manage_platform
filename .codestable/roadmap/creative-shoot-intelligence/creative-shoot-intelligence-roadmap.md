---
doc_type: roadmap
slug: creative-shoot-intelligence
status: draft
created: 2026-08-04
last_reviewed: 2026-08-04
last_updated: 2026-08-04
tags: [shoot-planning, cosplay, ai, knowledge-base, operator, creative-intelligence]
related_requirements:
  - customer-profile
  - order-tracking
  - schedule-calendar
related_architecture:
  - 001-account-scoped-data-model
  - 002-postgresql-as-primary-store
  - 003-monolith-first-gin-openapi
  - 004-avatar-object-store-as-binary-adjunct
upstream_roadmap: creative-shoot-planning
required_requirement_before_confirmation: creative-shoot-intelligence
---

# 创作型拍摄 AI 与垂类知识增强

## 1. 目标、前提与非自动启动

二次元 cosplay 存在大量冷门作品、角色版本和垂类视觉约束。通用 LLM 或图像模型很可能没有可靠训练数据：它们可能“不认识角色”，却以流畅语气生成泛二次元方案。单纯换模型或堆提示词不能解决知识缺失，产品必须维护可追溯、可撤回、按项目/账号/平台分层的模型外知识，并为后台人员提供持续响应缺口的履约系统。

本 epic 的产品目标不是“一键生成漂亮方案”，而是让 AI 成为有依据、可停机、可追溯、可采用或拒绝的创作副驾驶：先用项目知识做缺口检查，再沉淀账号私有知识和平台公共事实，统计摄影师自然形成的风格，最后才开放文本提案与分镜候选。

本 roadmap 与首版 `creative-shoot-planning` 已拆分。它不是首版 DAG 的自动后续，任何 item 进入 child design 前必须同时满足：

1. 首版 roadmap 已完成并通过 hardening/生产恢复；
2. G1–G3 有真实、可重算 evidence 和 owner disposition；
3. 本 roadmap 独立 review 为 passed，且 `.codestable/requirements/creative-shoot-intelligence.md` 已创建并由 frontmatter 关联；
4. 本 [approval-report.md](/Users/samson/workspace/my_project/customer_manage_platform/.codestable/roadmap/creative-shoot-intelligence/approval-report.md) 的 `g4-roadmap-start` group 用同一次 owner 确认将 `roadmap-plan` 与 `upstream-evidence-go` 原子置为 approved；
5. ADR-001 operator/platform 窄例外、内容政策、live provider 凭证和费用仍在各自 gate 单独批准。

在 G4 前即使独立 review 已 passed，`roadmap-plan` 也有意保持 pending，roadmap 保持 `draft`，不触发标准 ConfirmRoadmap。缺少任一条件时不得创建 child feature、知识表、读取租户提交、使用 provider 凭证或调用 live 模型。

## 2. 产品与安全不变量

| ID | 不变量 | 可机械验证的表现 |
|---|---|---|
| I1 | 检查类先于生成类 | 最小闭环止于 gap check；proposal 必须依赖 gap、治理和风格统计 |
| I2 | 知识不足明确停机 | blocking gap/rights/consent 产生 `needs_knowledge|blocked_rights`，不能回退到通用模型盲生成 |
| I3 | 摄影师保持最终控制 | AI 只产 immutable proposal；apply/reject 显式、revision CAS、可追溯，不自动修改 ShootPlan |
| I4 | 摄影转译经验不进 platform | kind×scope 服务端 allowlist，三层写路径都拒绝 forbidden combination |
| I5 | 原作图只展示、不进生成 | 首版 source×rights×purpose 矩阵在 reserve/dispatch/publish/open 重检 |
| I6 | operator 只读显式 submission | capability + immutable submission bundle；不得浏览租户任意项目/知识 |
| I7 | 风格来自真实历史，不要求填表 | 只统计 canonical shot tags；unknown 单列，样本不足返回 unavailable |
| I8 | AI 可整体关闭 | provider 不可用或本 epic 未部署时，首版策划、分享、提醒和经营能力完全可用 |
| I9 | 指标不外化 | 采用率、评测和成本只进内部运营面，不成为摄影师完成度、排行榜或强提醒 |

## 3. 范围与排除项

### 3.1 包含

- 项目研究包、来源版本、权利、冲突、缺口和撤回；
- 后台人员的 submission-bound 研究队列与摄影师复核回写；
- AI 缺口检查和 `needs_knowledge` 停机；
- 账号私有知识晋升与平台公共事实/视觉目录；
- operator capability、consent、审核、lineage 和 quarantine；
- 从首版 Shot canonical tags 统计的账号风格画像；
- 异步文本 ProposalSet、显式 apply/reject 和 proposal→final Shot provenance；
- 单镜头 AI 分镜候选、AI 标签和 planning media 生命周期；
- 版本化评测、成本、provider/model/prompt 漂移；
- 账号 export metadata 扩展；
- 部署 backup schema-v3、retention、GC 和全路线 hardening。

### 3.2 排除

- 预建所有动漫作品的完整百科；
- 后台人员任意浏览或直接改写租户知识；
- 训练/微调基础模型、运营跨部署中央知识网络；
- 自动网页抓取、绕过站点条款的采集、PDF/OCR/视频理解；
- 把同人解释当官方事实、把冲突来源静默合并；
- 把客户隐私、未采用 AI 输出、原始照片像素自动用于学习；
- 让 official/fan/screenshot 图片进入图像生成 reference；
- AI 自动 apply、自动改价、自动改档期、自动向客户发布；
- 把 A/B 主观分数作为唯一价值信号；
- 账号导出包含 provider secret、token secret、运行快照或媒体二进制。

## 4. 模块边界与所有权

### 4.1 `creativeknowledge`

拥有 project/account/platform 三 scope 的 source、version、claim、conflict、promotion、consent、submission 和 context assembler。tenant 与 platform repository 物理分路；普通 AccountScope 永不获得 unscoped repository。

### 4.2 `knowledgeops`

拥有 operator capability boundary、submission-bound queue、claim lease、研究履约、审核与 audit。它只接受显式 frozen submission，不提供“按账号浏览”入口。账号管理员不等于 knowledge.operator，普通账号不能自授予 capability。

### 4.3 `planningai`

拥有 Run、RunInputSnapshot、KnowledgeUseRef、provider ports、ProposalSet、apply receipt/provenance、gap output、storyboard output 和调用预算。它不直接查业务表或媒体对象，只通过 revisioned query 与 opaque access permit。

### 4.4 `shootplanning` / `planningmedia` 上游 seam

本 epic 只消费首版稳定接口：revisioned ShootPlan/Shot、canonical tags、capture provenance、source×rights×purpose、binding/lease 和 backup extension point。改变这些 seam 必须回首版 planning update 并重审两个 roadmap。

### 4.5 `planningeval` 与 ops

评测拥有 corpus/rubric/report，不拥有生产 run 权限。账号导出和部署 backup/retention 是两个独立 item：前者是租户 portability 的诚实元数据边界，后者是部署恢复与数据生命周期。

### 4.6 所有权矩阵

| 契约 | 唯一 owner | 消费者 | hardening 是否可首次实现 |
|---|---|---|---:|
| project source/version/claim/conflict/request | project-knowledge-packs | research ops、assembler、gap | 否 |
| operator submission/lease/review/audit | project-research-operations | project pack、platform catalog | 否 |
| Run/Snapshot/KnowledgeUseRef/gap lifecycle | ai-gap-check | proposals、storyboard、eval | 否 |
| account promotion 与私有 generation | account-knowledge-curation | assembler、export | 否 |
| platform catalog/consent/kind allowlist | platform-knowledge-catalog | assembler、AI、export/ops | 否 |
| StyleProfile 统计 | photographer-style-statistics | proposal assembler、export | 否 |
| ProposalSet/apply/provenance/adoption | ai-planning-proposals | ShootPlan、eval、export | 否 |
| AI image generation/binding | ai-storyboard-generation | planningmedia、eval、ops | 否 |
| corpus/rubric/provider reports | ai-evaluation-operations | release/owner | 否 |
| 账号 export schema | creative-intelligence-account-export | account owner | 否 |
| backup-v3/retention/GC | creative-intelligence-ops-retention | deployment ops | 否 |
| 跨模块发布证据 | creative-intelligence-release-hardening | owner | 是 |

## 5. 知识领域契约

### 5.1 核心实体

```text
KnowledgeSource
  id, scope, account_id?
  source_class: official | secondary_summary | fan_interpretation |
                photographer_experience | customer_contribution | operator_research
  title, locator?, citation, rights_basis, effective_uses[]
  work, character?, canon_period?, language?
  created_by, created_at, withdrawn_at?, withdraw_generation

KnowledgeVersion
  id, source_id, generation, content_hash
  status: draft | effective | superseded | withdrawn | quarantined
  valid_from?, valid_to?, created_at

KnowledgeClaim
  id, version_id
  kind: canon_fact | visual_identity | photography_guidance |
        style_preference | private_production_experience
  subject_key, field_key, value, confidence_label
  source_excerpt_ref, applicability, created_at

KnowledgeConflict
  id, subject_key, field_key, claim_ids[]
  state: unresolved | contextualized | superseded
  resolution_note?, resolver?, resolved_at?
```

事实可信度来自来源和适用范围，不用单一数值把“官方设定”和“同人解释”混为一谈。不同剧情时期、动画/游戏版本或同人解释可同时存在；assembler 必须把冲突和引用交给 AI/摄影师，不得静默选一个当真。

### 5.2 Scope × kind allowlist

| knowledge kind | project | account | platform |
|---|---:|---:|---:|
| canon_fact | 允许 | 允许 | 允许 |
| visual_identity | 允许 | 允许 | 允许 |
| photography_guidance | 允许 | 允许 | 禁止 |
| style_preference | 允许 | 允许 | 禁止 |
| private_production_experience | 允许 | 允许 | 禁止 |

三层防线都必须执行同一 policy library：

1. submission/promotion command validation；
2. operator approve/publish validation；
3. repository insert/update constraint。

非法组合返回 `422 forbidden_kind_for_scope`；历史非法行迁移到 `quarantined`，不进入检索或 context snapshot。不能只在运营 UI 隐藏按钮。

知识文本用途同样由服务端判定，不能只保护图片：

| source_class | project_display | ai_check_context | ai_generation_text_context | platform_publish | eval_fixture |
|---|---:|---:|---:|---:|---:|
| official / secondary_summary | 按引用政策 | 按引用政策 | 按内容政策 | 需 rights 审核 | 默认禁止 |
| fan_interpretation | 明示为同人解释 | 可用但保留标签 | 默认禁止，专项批准后才可 | 禁止 | 默认禁止 |
| photographer_experience | 允许 | 允许 | 允许 | 因 kind allowlist 禁止 | 需账号 opt-in |
| customer_contribution | 项目 consent 内允许 | 项目 consent 内允许 | 默认禁止 | 禁止 | 禁止 |
| operator_research | 继承底层来源最窄用途 | 继承 | 继承 | 继承并审核 | 仅 operator-curated 专用 fixture |

`effective_uses` 是来源权利、贡献 consent、scope/kind policy 和当前请求 purpose 的交集；任一撤回都增加 generation 并使新 snapshot fail-closed。AI 输出不能自动作为 KnowledgeSource 晋升。

### 5.3 冲突与检索优先级

检索必须先做 account/scope/rights/lifecycle 过滤，再做排序。默认上下文优先级是：

```text
项目明确适用 claim > 账号私有 claim > 平台公共 claim
```

它表达“本次拍摄约束优先”，不表达“高层一定更真”。同 field 存在互斥事实时，snapshot 保存所有有效 claim、来源和 conflict ID；AI 必须输出“存在冲突/需要摄影师选择”，不能让排序静默消除冲突。

### 5.4 Project research 与后台履约

```text
ResearchRequest
  id, project_id, requested_by
  subject, gap_kind, question, selected_context_refs[]
  status: draft | submitted | claimed | researching |
          awaiting_photographer | accepted | rejected | withdrawn

ResearchSubmission
  id, request_id, immutable_bundle_hash
  consent_generation, allowed_fields, allowed_asset_refs[]
  submitted_at, withdrawn_at?

OperatorLease
  request_id, operator_id, lease_until, revision

ResearchDraft
  request_id, source/version/claim candidates[]
  operator_notes, created_at
```

履约流程：摄影师提交 frozen bundle → capability 校验 → operator claim lease → 研究并附来源 → 摄影师预览 → 摄影师 accept 后复制到 project generation。operator 不能绕过 accept 直接写租户知识。撤销 submission consent 后阻止新的 operator read/publish；已产生 draft 保留 audit，但内容按 retention/policy 处理。已经由摄影师采纳的 project generation 不因 operator 访问同意撤销而失效，只有对应 KnowledgeSource rights 被撤回时才停止新使用。

`knowledge.operator` 由部署管理员通过受信 accountctl/ops seam 授予和撤销，下一请求生效。所有 read 记录 submission ID、operator ID、purpose 和时间，不记录无关租户正文。

### 5.5 晋升与三层 assembler

- 同一语义 claim 在三个不同 project 重复出现，仅创建 account promotion candidate；摄影师显式确认后复制为 account generation；
- account→platform 必须是 allowlisted kind、version-bound consent、operator 审核和独立 platform generation；
- operator 可从公开来源直接建立 `PlatformCatalogDraft`，但 draft 不得引用未提交的租户内容，必须带 source/rights，且经过与 tenant promotion 相同的 allowlist、审核、审计和 withdraw 生命周期；
- platform 只保存 published catalog，不以 `account_id=NULL` 偷建“全局租户表”；
- tenant asset 晋升 platform 时复制允许用途的 exact generation，不复用私有对象；
- consent/rights withdraw 增 generation；新 snapshot fail-closed，旧 run 按 retention 保留审计但不能重新打开已撤回资产；
- assembler 公开签名保持 `AssembleContext(account, project, planRevision, purpose)`，输出 immutable refs 和 snapshot hash。

## 6. AI 运行与生成契约

### 6.1 RunInputSnapshot 与状态机

```text
PlanningAIRun
  id, account_id, plan_id, run_kind
  status: queued | dispatched | needs_knowledge | blocked_rights |
          succeeded | failed | cancelled | outcome_unknown
  provider, model, prompt_version, budget_policy_version
  input_snapshot_id, result_ref?, error_class?
  idempotency_key, created_at, terminal_at?

RunInputSnapshot
  id, plan_id, plan_revision
  shot_revisions[]
  knowledge_use_refs[]
  asset_access_refs[]
  style_profile_revision?
  assembler_version, prompt_hash, snapshot_hash

KnowledgeUseRef
  scope, source_id, version_id, claim_id
  rights_generation, consent_generation?, citation
```

创建 snapshot、reserve assets 和创建 run 在一个可恢复事务协议内。dispatch、retry、result publish 和 output open 都复核 lifecycle/rights/consent/binding。provider 未知结果不得自动重发产生双份费用；只有持久化 acknowledgement 后才创建新的 replacement run。

日志禁止知识正文、prompt 全文、客户信息、token 和 provider secret；只保留 IDs、hash、计量和脱敏错误分类。

### 6.2 Gap check

Gap check 只比较结构化 shot list、assignment/readiness、场地/时间限制和知识 claims，输出：

```text
GapFinding
  type: missing_signature_action | visual_identity_conflict |
        infeasible_lighting | unassigned_prop | missing_source | other
  severity: info | important | blocking_knowledge
  affected_refs[]
  citations[]
  explanation
  suggested_action: ignore | edit_plan | request_research
```

`blocking_knowledge` 只阻止本次 AI run，不阻止 ShootPlan ready/in_progress/completed。摄影师可忽略普通 finding；标记“无资料/不符合设定”可生成 ResearchRequest 草稿，但提交给 operator 仍需显式确认。

### 6.3 风格统计

StyleProfile 只读首版的 completed Shot canonical tags 和 taxonomy version。unknown 单列；跨 taxonomy 需要显式 mapping version，否则分桶统计。配置固定最小样本数、时间窗和 live/completed inclusion policy；不足返回 `insufficient_samples`。

`unobserved_in_sample` 表达“在当前有限全集与样本窗未出现”，不得命名成永久的“从不拍”。摄影师可 dismiss/restore 一个 facet；dismiss 是账号私有偏好，不晋升 platform。

### 6.4 ProposalSet、apply 与采用率 lineage

```text
ProposalSet
  id, run_id, plan_id, base_plan_revision
  proposals[], created_at
  status: open | applied | rejected | stale

Proposal
  id, entity_type, proposed_patch
  citations[], constraints[], confidence_label

ProposalApplyReceipt
  proposal_set_id, selection_hash
  base_revision, resulting_plan_revision
  edges[{proposal_id, resulting_entity_type, resulting_entity_id, resulting_path}]
  applied_at
```

apply 使用同事务 plan CAS；成功 receipt 可幂等重放，异 selection 同 key 冲突。人工后续编辑保留 provenance edge；删除 resulting entity 使其不再计 adopted；复制生成新 entity 必须新增 lineage edge，不能继承为同一个采用事件。

采用率：

```text
distinct displayed proposal whose resulting Shot still exists and has live captured
-------------------------------------------------------------------------------
all distinct Shot proposals that reached an open ProposalSet visible to the photographer
```

backfill/unknown capture 不进分子；已展示后 rejected/stale 的 proposal 仍留在分母。rights-blocked/needs_knowledge 在 ProposalSet 形成前停机，单列为 run 诊断而不是伪造 proposal。该指标只进内部观测，不对摄影师显示完成度。

### 6.5 素材用途与分镜

AI reserve 只接受首版 media policy 判定 `generation_reference=true` 的 opaque refs。official、fan、setting book、anime screenshot 即使出现在 moodboard 也必须在 reserve 阶段返回 `blocked_rights`；不能把图片转 base64 后绕过 typed permit。

AI 分镜输出是新的 generation，保存 provider/model/prompt/context hashes、AI 标签、rights policy version、binding 和 lease。它是策划候选，不是客户作品，不自动进入 share/full，更不覆盖 Shot。

## 7. 评测、导出与运维

### 7.1 评测策略

版本化 corpus 至少覆盖：

- 热门角色基础样本；
- 冷门角色且 project knowledge 充分；
- 冷门角色知识缺失，应 needs_knowledge；
- 不同剧情时期/来源冲突；
- official image display-only；
- 场地、预算、器材和安全限制导致不可执行；
- provider timeout、429、内容拒绝和 outcome_unknown。

Rubric 包含引用正确性、角色忠实度、约束遵循、现场可执行性、多样性、真实采用率、失败率、延迟和成本。知识有/无或 provider A/B 是诊断，不作为唯一产品完成信号。自动门禁使用 deterministic/fault adapters；live checkpoint 单独获得 owner 凭证/费用批准。

### 7.2 账号导出

账号 export 是 reference-only：包含租户自有 project/account knowledge、source/consent/lineage、style stats 和 proposal/apply provenance；不包含 platform media、provider secret、token secret、RunInputSnapshot 或媒体 bytes。schema 明确：

```text
media_portability=references_only
binary_included=false
platform_citations_not_self_contained=true
run_input_snapshot_included=false
replayable=false
```

它在既有 account-scoped 单事务 snapshot 中完成，不跨事务拼装“看似完整”导出。

### 7.3 部署备份、retention 与 GC

首版 schema-v2 已覆盖 PG+avatar+planning media。本 epic additive 引入 schema-v3，包含 tenant/platform generation、AI output、operator audit 和相关 manifests；新 reader 继续读 v1/v2/v3，旧 reader 拒绝未知版本。retention policy 区分：research submission/draft、RunInputSnapshot、provider result、AI media、eval corpus、audit、withdraw/quarantine。binding、lease、lineage、consent、policy hold 共同决定 liveness，任何 GC 都必须 dry-run、scope counts 和宽限期。

账号 export 不可用于 deployment restore；数据库引用存在但二进制未恢复时必须 fail-closed，不能报告成功。

## 8. 接口边界

规划级 seam：

```http
# 摄影师 Bearer + AccountScope
POST /api/v1/shoot-plans/{id}/research-requests
POST /api/v1/research-requests/{id}/submit
POST /api/v1/research-requests/{id}/accept
POST /api/v1/shoot-plans/{id}/ai/gap-checks
POST /api/v1/shoot-plans/{id}/ai/proposals
POST /api/v1/shoot-plans/{id}/ai/proposal-sets/{setId}/apply
POST /api/v1/shoot-plans/{id}/shots/{shotId}/storyboards
GET  /api/v1/account/knowledge
GET  /api/v1/account/style-profile

# knowledge.operator capability + submission-bound context
GET  /api/v1/operator/research-submissions
POST /api/v1/operator/research-submissions/{id}/claim
POST /api/v1/operator/research-submissions/{id}/draft
POST /api/v1/operator/platform-drafts
POST /api/v1/operator/platform-submissions/{id}/decision
```

operator handler 接收 `OperatorSubmissionContext`，不是 AccountScope 或 unscoped DB handle。所有 content open 需要 submission proof；列表只返回明确提交的 bundle metadata。

## 9. Feature 路线与 DAG

1. `project-knowledge-packs`：建立项目知识、来源、冲突和缺口。
2. `project-research-operations`：建立后台持续履约 seam。
3. `ai-gap-check`：形成本 epic 唯一最小闭环。
4. `account-knowledge-curation`：项目重复知识显式晋升账号层。
5. `platform-knowledge-catalog`：allowlisted 公共事实与视觉目录。
6. `photographer-style-statistics`：独立从首版历史统计风格，不依赖 proposal。
7. `ai-planning-proposals`：在 gap、治理、风格稳定后生成文本提案。
8. `ai-storyboard-generation`：最后开放图像候选。
9. `ai-evaluation-operations`：持续比较质量、成本和漂移。
10. `creative-intelligence-account-export`：独立收口账号 portability。
11. `creative-intelligence-ops-retention`：独立收口 deployment restore/retention/GC。
12. `creative-intelligence-release-hardening`：最终集成和发布。

```text
project-knowledge-packs ── project-research-operations ─┐
          └─────────────────────────────────────────────┴─ ai-gap-check [minimal loop]
                                                            ├─ account-knowledge-curation ─ platform-knowledge-catalog ─┐
                                                            └─ photographer-style-statistics ──────────────────────────┤
                                                                                                                        └─ ai-planning-proposals
                                                                                                                              └─ ai-storyboard-generation
                                                                                                                                      └─ ai-evaluation-operations

[account curation + platform catalog + proposals + style] ─ creative-intelligence-account-export ─┐
[research ops + platform catalog + storyboard + eval] ──── creative-intelligence-ops-retention ───┴─ creative-intelligence-release-hardening
```

YAML `depends_on` 是本 epic 内技术依赖权威；全图之外仍有 `roadmap-plan` 和 `upstream-evidence-go` 两个全局 dispatch barrier。没有两个 approved ref 时，任何根节点都不得派发。

## 10. Goal Coverage Matrix

| 目标 | Owner item | 证据 |
|---|---|---|
| 冷门角色可建有来源、版本、冲突和权利的项目研究包 | project-knowledge-packs | schema/fixture、scope/withdraw/conflict tests |
| 后台人员可持续响应明确缺口但不能浏览租户任意数据 | project-research-operations | capability/submission/lease/audit/withdraw matrix |
| 缺口检查有引用、可停机、失败可恢复且不阻塞策划 | ai-gap-check | deterministic/fault runs、needs_knowledge、snapshot evidence |
| 项目知识可显式沉淀账号私有层 | account-knowledge-curation | promotion/lineage/conflict/withdraw tests |
| 平台层只含非竞争性事实/视觉知识 | platform-knowledge-catalog | kind×scope 三层拒绝、ADR/consent/operator tests |
| 风格画像由历史 Shot 统计且可重算 | photographer-style-statistics | taxonomy/unknown/insufficient/dismiss fixtures |
| 文本提案显式 apply/reject 且采用率可自动计算 | ai-planning-proposals | CAS/receipt/provenance/live-adoption report |
| 分镜使用合法素材、带 AI 标签且不自动覆盖 | ai-storyboard-generation | rights/reserve/provider/binding/UI evidence |
| AI 质量、成本和漂移可持续评估 | ai-evaluation-operations | corpus/rubric/versioned reports |
| 账号导出诚实表达不可携带边界 | creative-intelligence-account-export | schema/transaction/reference-only fixture |
| knowledge/AI media 可 exact restore 且按政策 retention | creative-intelligence-ops-retention | backup-v3/GC/restore rehearsal |
| AI 整体关闭时首版仍完整可用 | creative-intelligence-release-hardening | disabled-provider E2E、首版 regression |

唯一最小闭环是 `ai-gap-check` 及其依赖闭包：

```text
摄影师标记冷门角色知识缺口
→ 显式提交给后台研究队列
→ operator 附来源形成 draft
→ 摄影师采纳进入项目研究包
→ gap check 对照 shot list 给出有引用检查
→ 摄影师忽略、修改或再次请求知识
```

它先证明“知识支持能降低错误”，不依赖文本生成或图片生成。

## 11. G4 启动批准与后续 checkpoints

### 11.1 `upstream-evidence-go`

Owner 批准 G4 前必须看到：

- 首版 roadmap 八条最终状态与独立验收；
- G1：5 个样本中 live run 打开数；
- G2：摄取 active duration 中位数；
- G3：eligible shared shoots 的互动率和 preparation_missing 变化；
- 首版生产恢复、匿名安全和 H1/H2 回归；
- 对小样本不确定性的说明；
- 本 epic 预计 operator 成本、provider 成本和内容政策责任。

上述输入汇总为 `evidence/upstream-g4-{window-id}.json` 和同名 Markdown；JSON 引用首版 item 状态、G1–G3 canonical evidence path/hash、restore evidence 和 requirement hash。`g4-roadmap-start` 批准时把 bundle path/SHA-256/gate version 与两项 named decision 原子写入 approval report；bundle 变化使旧批准失效。

不通过时，本 roadmap 继续 draft，不建知识库、不调用 AI。通过只允许进入 child design，不等于批准 ADR、运营越权、live provider 花费或生产开放。

### 11.2 独立 checkpoints

- `project-research-operations` 实现前：ADR-001 submission-bound operator 窄例外；
- `platform-knowledge-catalog` 实现前：platform published catalog、operator审核和 published media ADR；
- 首次 live provider 前：模型/区域/凭证/预算/内容政策批准；
- tenant 数据进入 eval corpus 前：逐用途 opt-in、脱敏和 retention 批准；
- platform catalog 生产开放前：内容/版权政策与运营责任批准。

标准 epic 会在所有 child design 统一确认后生成一个 goal package，不支持靠任意 per-item prose 在 driver 中安全停顿。因此四项实现/副作用前提必须在写入或授权 goal package前形成 durable evidence，并由 canonical `approval-report.md` 的 `approval_groups.intelligence-execution-prerequisites` 同一次确认；下游分别机械核验四个 `approvals` key：

- `operator-adr` 与 `platform-catalog-adr` 已 approved，ADR 文件存在且被相关 design 引用；
- `live-provider` 已明确 provider/model/region、凭证来源、预算上限、未知结果成本和是否允许真实调用；
- `production-content-policy` 已明确来源/版权、operator 责任、平台发布和撤回处置；
- 任一 pending/rejected 时，goal package 不得标 `ready-to-dispatch`，不得请求标准 Goal execution authorization。

本规则允许全部 child design 先完成，因为决策材料需要具体 design；但执行授权一次性前置，不要求 goal driver 中途理解自定义 checkpoint。若 owner 不批准 live/provider 或 production policy，必须 drop/拆出相应 live-dependent item、更新 coverage 并重跑 review，不能在已授权 goal 中临时降级。

## 12. 完成信号、风险与回退

### 12.1 完成信号

当且仅当 12 条 item 全部 `done`、coverage 无缺口，并在真实冷门角色案例中完成最小闭环和后续 proposal/storyboard 路径；operator 访问 submission-bound、kind×scope 与素材用途矩阵全部 fail-closed；采用率 provenance 可重算；账号 export 边界诚实；schema-v3 exact restore；provider disabled 时首版无回归。

单纯生成一张漂亮图片不构成完成。A/B 评分提升也不能替代真实采用率和可执行性证据。

### 12.2 Top 风险

1. **后台知识运营成本失控。** 只响应真实 ResearchRequest，不预建百科；队列记录耗时、复用和放弃原因。
2. **AI 流畅胡说。** needs_knowledge、冲突暴露、引用覆盖和结构化 gap 先行；生成不能绕过 blocking。
3. **跨账号/operator 泄漏。** 物理存储分路、submission bundle、capability、next-request revoke 和 audit。
4. **版权与素材用途越界。** typed permit 在 reserve/dispatch/publish/open 重检；display bytes 不能转码绕过。
5. **供应商费用和能力漂移。** provider ports、预算、未知结果 acknowledgement、版本化 eval 和 live checkpoint。
6. **风格画像把稀疏数据当偏好。** unknown、最小样本、taxonomy version 和 unobserved_in_sample 语义。
7. **删除/撤回导致历史不可审计或媒体误删。** immutable refs、withdraw generation、binding/lease/lineage 和 retention hold。

### 12.3 回退

- AI/provider feature flag 可整体关闭，首版服务不依赖其表或路由；
- platform catalog 可停止发布/读取，新 project/account 知识仍隔离可用；
- operator capability 撤销从下一请求生效，active lease 不能继续读取正文；
- outcome_unknown 不自动重发；人工 acknowledgement 后才 replacement；
- schema-v3 发布与 restore 工具成对，应用回滚仍保留能读 v3 的 ops 镜像；
- withdraw/quarantine 不物理改写历史 run，禁止新使用并按 retention 到期清除。

## 13. 验证入口与长期回写

- 全仓：`make check`、`make generate-check`；
- 自动 AI：deterministic/fault adapters，不把随机 live 输出塞进普通单元测试；
- 安全：三 scope×五 kind、source×rights×purpose、operator submission、withdraw/consent、跨账号矩阵；
- 浏览器：研究包、operator queue、gap/proposal/storyboard、style profile 在 1600/1280/375 与键盘/读屏下验证；
- 运维：账号 export schema fixture、backup-v1/v2/v3 strict dispatch、schema-v3 exact restore/GC rehearsal；
- live：owner 授权后记录 provider/model/prompt/context、预算与人工 rubric，不把凭证写入 artifact。

知识回写：

- G4/ConfirmRoadmap 前用 `cs-req` 建立 `.codestable/requirements/creative-shoot-intelligence.md`，并把 slug 加入本 frontmatter `related_requirements`；缺文件时不允许确认 roadmap；
- project research ops 前用 `cs-domain` 记录 operator submission narrow exception；
- platform catalog 前记录 platform scope、kind allowlist、consent 与 published media ADR；
- gap/proposal acceptance 后记录 RunInputSnapshot、provider port 和 proposal provenance；
- ops retention 后记录账号 export 与部署恢复差异及 binding/lease/lineage GC 不变量。

## 14. 当前批准状态

- roadmap 为 `draft`；
- epic 拆分方向已由 owner 于 2026-08-04 确认；
- 本路线本身、G4、ADR、live provider 费用和生产内容政策均未被自动批准；
- 独立 roadmap review 可以现在完成，但 `roadmap-plan` 必须继续 pending；直到首版 evidence 和 durable requirement 就绪，才用 `g4-roadmap-start` 原子确认路线与启动；
- owner 未批准 `roadmap-plan` 与 `upstream-evidence-go` 前，不创建 child feature、不实现、不调用模型、不 commit/push。
