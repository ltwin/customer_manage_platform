---
doc_type: feature-design
feature: 2026-08-05-creative-planning-v1-hardening
requirement: creative-shoot-planning
roadmap: creative-shoot-planning
roadmap_item: creative-planning-v1-hardening
execution_lane: goal
status: approved
summary: 以只读集成证据协调器和 production backup schema-v2 收口创作型拍摄策划首版的跨模块发布与恢复门禁
tags: [shoot-planning, hardening, e2e, evidence, backup, restore, release]
---

# creative-planning-v1-hardening Feature Design

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| 首版一致性矩阵 / `PlanningV1ConformanceMatrix` | 把 roadmap、OpenAPI、已确认 child design 与 tracked v2 prototype 映射到可执行场景、证据和 owner 的版本化矩阵 | 不是新的产品规格；冲突仍按既定权威优先级解决 |
| 端到端场景 / `PlanningV1E2EScenario` | 跨 core、media、ingestion、CRM、share、reminder、business 的真实用户路径定义 | 不复制各模块内部状态机，只通过公开 API、稳定 maintenance seam 和用户可见 UI 观察 |
| 失败场景 / `PlanningV1FailureScenario` | 对 DB、对象卷、网络、重启、过期、并发和 stale 的可重复故障脚本及预期终局 | 不允许 hardening 新造上游补偿规则 |
| 证据窗口 / `EvidenceWindow` | 固定 cohort、事实截止点、计算版本、build/config 和窗口时间的只读计算边界 | 不是任意日期筛选；窗口关闭后不静默纳入新样本 |
| 样本决定 / `ObservationSampleDecision` | 对候选 shoot 纳入或排除及其理由、事实引用、revision 和审阅来源的审计记录 | 不从 notes、日志或昵称推断资格；不能只保留最终入选列表 |
| 首版证据索引 / `PlanningV1EvidenceReport` | 对既有 stage-1 G1/G2 JSON、stage-2 G3 JSON、样本决定、hash 和 stale 状态的跨阶段权威索引 | stage JSON 仍是各自计算权威；Markdown 只做人读解释；本索引不批准 gate |
| 生产备份包 v2 / `PlanningBackupPackageV2` | 在既有 PostgreSQL、avatar 包内容之外加入 planning media volume 与 manifest 的严格部署级备份包 | 不等于账号 JSON export，也不改变 planningmedia 的领域 ownership |
| planning media 备份清单 / `PlanningMediaBackupManifest` | 由 planningmedia maintenance adapter 对生产卷生成并验证的稳定排序 exact object 清单 | 复用上游 `PlanningMediaManifestV1` 事实，不读取 media 私有表拼另一份清单 |
| 恢复预检 / `PlanningRestorePreflightResult` | destructive mutation 前对 package、deployment identity、manifest、digest、对象差异和空间的完整判定 | 任一 required check 失败即零目标写；不能把 warning 当 pass |
| 恢复演练 / `PlanningRestoreRehearsal` | 在真实 PostgreSQL、avatar volume、planning-media volume 和生产形态镜像上完成的备份—破坏—恢复—验证证据 | 模块 empty-volume fixture 或 mock object store 不能冒充本演练 |
| 发布就绪证据 / `PlanningReleaseReadinessEvidence` | 汇总 contract conformance、E2E、H1/H2、原型、可访问性、evidence、backup/restore、archive capability 和 rollback 的 fail-closed 报告 | 只判定是否具备发布条件；不会执行 release、deploy、promotion 或 production cutover |

代码与文档检索未发现上述 hardening 名词的现存实现。当前仓库已有 `scripts/backup-compose.sh`、`scripts/restore-compose.sh` 和 strict schema-v1 五件套；已有 `PlanningMediaManifestV1`/module restore 只属于 planningmedia 模块级能力。二者不得混称为 production schema-v2。

## 1. 决策与约束

### 1.1 需求摘要与成功标准

本 feature 是首版的集成与发布证据协调器，并首次拥有 production backup schema-v2。它把七个上游 child 已落地的公开 seam 组合成真实浏览器、真实 PostgreSQL 和真实对象卷场景，证明“摄取已有讨论/图片 → atomic commit → shot list → live Run Mode → 客户 full 协作与认领 → 摄影师提醒 → 私有经营草稿”可完整工作，且失败后终局可解释、可恢复、不泄露商业信息。

成功标准：

1. 上游每个核心行为都能映射到 owner、公开 seam、accepted artifact 和场景证据；发现缺口时 fail-closed 返回对应上游 scope defect。
2. H1 在没有任何策划的账号上保持客户、订单、档期、报价、设置和 reminder 零提示、零阻塞、零额外业务 mutation；内部观测指标不进入摄影师 UI。
3. H2 对 proposal/full 的 read/mutation、shared asset、query、DTO、DOM、bundle 和日志投影做 exact negative，价格、成本、business facts、经营时长、adjustment line、business draft 均不可表达。
4. tracked v2 五页 page map 的信息架构、关键状态、纠错、失效、破坏性确认和用户语言都有 conformance row；原型静态样例不覆盖 roadmap/OpenAPI/confirmed design。
5. 1600、1280、375、200% zoom、keyboard、screen reader、coarse pointer、强光/对比、focus return 与 44px touch target 都有可重复浏览器证据，Run Mode 在移动端保持 execution-only。
6. G1/G2 与 G3 的 stage JSON、样本决定、cohort 差异、hash 和 stale 状态可机械验证；JSON 为权威，Markdown 由 JSON 派生，approval hash mismatch fail-closed。
7. 既有 schema-v1 五件套 writer/reader characterization 零漂移；新 reader 同时接受 v1/v2，冻结的旧 reader 对 v2 明确非零拒绝。
8. schema-v2 在 destructive restore 前完成全部 preflight，并按 `preflight → stop writes → replace → migrate → validate → reopen` 固定顺序 exact restore PostgreSQL、avatar 与 planning media；失败不报告部分成功。
9. release evidence 证明 additive schema、兼容 binary、旧进程退出、archive capability 相邻 CAS/readback、真实 writer/participant 启用和 rollback tool retention 的顺序；不自动执行任何生产动作。

### 1.2 明确不做

- 不首次定义或修改 ShootPlan、Shot、Readiness、RunModeSession、execution event/void/finalization 的领域规则。
- 不首次定义 media rights、binding、lease、permit、GC 或 object-key 语义；hardening 只消费 planningmedia inventory/verify maintenance seam。
- 不首次定义 share token、匿名限流、receipt、feedback、assignment、archive effects 或 reminder generation/fence/resolution；发现缺失必须退回原 owner。
- 不首次定义 PlanningBusinessFacts、规则、draft、Order/Schedule apply 或 Settings CAS；不读取 business 私有表构造匿名 negative 的“捷径”。
- 不把 hardening 做成新的业务 bounded context，不新增摄影师侧“完成率”“G1/G2/G3”“发布就绪”页面或徽章。
- 不调用 AI/provider，不新增知识库、OCR、链接抓取或 generation reference consumer。
- 不让账号 JSON export 携带媒体；deployment backup 与 account export 始终是两种能力。
- 不修改 `planning-share-v1` 的 exact ordered effects，不新造 archive marker，不在 reminder enabled 后绕过 `planning-share-reminder-v1`。
- 不执行 schema down、自动降低 capability、自动 push/merge/publish/release/deploy/promotion/cutover，也不把本地 synthetic rehearsal 写成 production rehearsal。
- 不因 hardening 测试方便而直接查询其它模块私有表、绕过 AccountScope、调用匿名内部 service 或构造不可伪造 capability。

### 1.3 复杂度档位与方案深度

本 feature 走长期业务资产的高正确性档位。恢复错误可能造成数据库与两个对象卷不一致，H2 失败会泄露商业数据，跨模块 E2E 的假阳性会把未闭合能力推进发布，因此选择：

- 真实 PostgreSQL、真实 volume/object 文件、真实迁移与真实应用镜像；不使用内存 repository 代替关键证据。
- 真实浏览器运行已构建前端并访问真实 HTTP API；DOM 字符串 grep 只作为补充 guard，不替代用户路径。
- 版本化 canonical JSON + SHA-256；Markdown、截图和视频只作为解释，不替代机器报告。
- schema-v1/v2 分支严格 decoder；不使用“存在某文件就猜版本”或宽松 unknown-field 忽略。
- destructive restore 全阶段 fail-closed；不提供 `--skip-manifest`、`--force-space`、`--ignore-orphan` 等绕过开关。

唯一简化是 release/cutover 仍由 operator 在外部 control plane 执行；本 feature 只产出可核验证据和 runbook。这不是临时替身，而是权限边界。

### 1.4 所有权与模块放置

- `planninghardening` 是测试/maintenance/release evidence coordinator，不拥有业务数据表。它消费稳定接口、调用公开 route，并把结果写到受版本控制或 operator 指定的 evidence 目录。
- `planningbackup` adapter 拥有 deployment package schema-v2、dual-reader dispatch、preflight、restore orchestration 与 production rehearsal schema；这是本 feature 唯一首次引入的系统能力。
- `shootplanning` 继续拥有 plan/run/finalization/observation facts；`planningmedia` 继续拥有 media inventory 与 exact volume verification；`planshare` 继续拥有匿名投影和互动事实；`reminder`、`order`、`schedule`、`settings` 各自拥有 mutation。
- stage-1 G1/G2 生产器继续由 ingestion/core 已承诺的 `planning-evidence-stage1-v1` 提供。stage-2 G3 生产器必须在 business dispatch 前由 share observation 所在 owner 或 Goal control artifact 落地；hardening 只校验并汇总，不能在最后一个 item 中倒置 DAG 补做。
- `planningctl archive-capability` 继续唯一拥有 capability readiness/promote/readback；hardening release verifier只读取其结构化输出，不直接 UPDATE marker。

该边界已由 roadmap §3.5/§3.6 批准，不需要为“evidence coordinator不是业务域”新增 ADR。若实现要求跨服务备份、外部对象存储、加密密钥封装或远程控制面写入，必须回到 roadmap/ADR，不能在本 feature 内扩权。

### 1.5 上游 contract conformance admission

hardening implementation 开始前建立 `PlanningV1DependencyConformanceV1`。每一项必须绑定 implemented build revision、accepted feature artifact、public seam/version、positive probe 和 negative probe；状态只允许 `conformant | blocked_upstream_scope_defect | blocked_not_implemented`。

| Owner | 必须已存在的 seam/事实 | hardening 只允许的消费方式 |
|---|---|---|
| shoot-plan-core | ShootPlan/Shot/Readiness、RunModeSession、result/void/current projection、PlanFinalizationSnapshot、PublicPlanScale、archive capability reader | Bearer API、accepted query/maintenance port、finalization/evidence fixture；不查私表重建 current |
| planning-reference-assets | `PlanningMediaManifestV1`、Inventory、exact verify/module restore、display access permit | maintenance adapter + volume mount；不复制 rights/binding/lease 规则 |
| plan-ingestion-capture | candidate preview/transition/atomic commit、PlanBuildObservation、stage-1 report | 公开 API 与 canonical report；不读取 candidate 私表模拟 commit |
| CRM integration | link/unlink、future shoot slot projection、H1 summary/query contract | Bearer API 与真实 CRM 页面；不直接写 connection/projection |
| share collaboration | proposal/full DTO、token失效、asset ref、feedback/assignment、ShareInteractionObservation | 匿名/Bearer API 与 safe evidence port；不持有原 token hash、receipt commitment 或私表 |
| assignment reminders | generation work/resolution/freshness、站内/TG intent、archive reminder effects | 用户可见 reminder API、maintenance freshness probe；不伪造 source generation |
| business feedback | facts/drafts/stale/apply、private-only DTO、Calendar prefill | Bearer API 与真实 UI；不把 draft字段塞进 share probe |
| Goal control | stage-1/stage-2 approval binding gate、canonical stage JSON path/hash/version | 只读 validator；不写 owner approval，不前移 current feature index |

准入额外规则：

1. `stage-1-evidence-go` / `stage-2-evidence-go` 的 pending 不阻止当前 design；未来 Goal 执行中，hardening 作为最后一项只有在所有依赖严格 `done` 后才可 dispatch。
2. 若 stage-2 canonical JSON producer 到 business dispatch 时不存在，这是上游 scope defect，不得等到 hardening 实现后再补；Goal 必须保持原 index handoff。
3. 任何 accepted design 与真实 generated type 不一致，以 approved scope change 流程处理；hardening 不建立兼容 shim 把错误接口伪装成通过。
4. dependency matrix 任一 required row 非 conformant，S2–S8 不得产生 implementation diff。

### 1.6 关键假设与冻结选择

- D1：production schema-v2 的部署 identity 来自 operator 提供、权限受控且不含 secret 的 typed identity document，至少含 `environment_id`、`deployment_id`、`compose_project` 和 storage-layout version；backup 写入，restore 必须 exact match。
- D2：v2 包固定七个 regular files：`database.sql`、`avatar-volume.tgz`、`avatar-manifest.json`、`planning-media-volume.tgz`、`planning-media-manifest.json`、`metadata.json`、`SHA256SUMS`。顶层多/少成员、symlink、device、hardlink、tar traversal 一律拒绝。
- D3：v1 包仍是当前五件套，metadata `schema_version=1`、成员集合、payload/digest 和 strict manifest 行为保持 characterization；v2 `metadata.schema_version=2` 且使用独立 exact field set。新 reader先最小严格解析 version再分派，不在同一 decoder 中堆 optional fields。
- D4：schema-v2 writer 在 planning media capability 启用的发布中是默认生产 writer；保留显式 legacy v1 生成只用于兼容演练/受控回退，不允许在已产生 planning media 的生产部署用 v1 包声称完整备份。
- D5：恢复可接受 v1 或 v2；v1 恢复后 planning media target 必须是不存在/空且 deployment inventory 明确声明 v1时代无 planning media。非空 target 使用 v1 包恢复时 fail-closed，避免静默保留新卷形成时间线混合。
- D6：available-space preflight 使用未压缩数据库估算、两个 tar 展开上界、staging/替换冗余和固定安全余量；无法取得可靠 free-space 或 size oracle 时 blocked，不允许猜测 pass。
- D7：production rehearsal 在 owner 批准的非生产、production-shaped 隔离部署上使用真实镜像、真实 PostgreSQL 与两个 named volume；报告可脱敏但必须含 deployment identity hash、镜像 digest、工具 digest、包 hash、阶段时间和验证结果。
- D8：H2 bundle guard 对构建产物扫描受禁 schema/property 名称和匿名 chunk imports，但不把字符串 absence 当唯一安全证据；服务端 exact DTO/query 负向和真实匿名浏览器仍是 required。
- D9：截图分辨率是 viewport CSS pixels；200% zoom 独立记录。375 viewport + coarse pointer 是 Run Mode 核心门，不能用缩放桌面截图替代。
- D10：所有证据报告以 UTC、stable key ordering、duplicate-key rejection 和 canonical UTF-8 JSON 编码；report hash 不包含同名 Markdown。

这些假设在 Epic 全量 designs 统一确认时整体拍板。实现阶段不能私自改成六/八文件包、宽松 v2 reader、v1 对非空 planning volume 覆盖或自动生产演练。

### 1.7 Top 风险、依赖与证据计划

| 风险 | 缓解 | 主要证据 |
|---|---|---|
| hardening 暗补上游事务/安全能力，��成 ownership 和 DAG 倒置 | S1 dependency conformance fail-closed；场景只经公开 seam；scope defect 回原 owner | conformance matrix、dependency/import guard、route-to-owner trace |
| schema-v2 部分恢复或 v1/v2误判导致 PG/volume 时间线混合 | exact members/decoder、全部 preflight、固定阶段状态机、failure-stop、v1非空planning target拒绝 | package golden/adversarial corpus、真实 restore fault matrix、post-restore inventories |
| E2E/原型/evidence 只测 happy path或静态 DOM，形成发布假阳性 | 真实浏览器+真实API+真实PG/object/restart，失败矩阵、hash/stale validator、Evidence Manifest | browser recordings、DB/object oracles、canonical JSON、release verifier |

非显然依赖：七个 child 必须先 implementation/review/QA/acceptance `done`；stage evidence gates必须在各自 dispatch 前已由 Goal 协议闭合；production-shaped rehearsal 需要 owner 单独批准环境与 operator，不能由 Goal execution authorization 推导；旧 reader v2拒绝证据需要冻结的上一版工具/镜像 artifact。

实现基线：当前 `make check` 包含串行 Go 测试、前端现有 runners、v1 ops selftest/safety/contract；当前 `frontend/package.json` 只有 `test:prototype`，尚无 `test:shoot-planning`/真实浏览器 runner；当前 `v1-ops-package.py`、`v1-ops-common.sh`、`v1-ops-smoke-runner.py` 分别约 961/543/1941 行；v1 restore 无 planning volume、dual schema、available-space 或 planning missing/orphan preflight。S1 必须按依赖实际落地后的命令重新建立基线，缺命令是 blocked upstream/required artifact，不得静默跳过。

最终交付物类别：dependency/conformance schema 与 runner；真实跨模块 browser E2E/failure suite；H1/H2/prototype/responsive/a11y evidence；G1–G3 evidence validator/index；schema-v2 writer/dual reader/preflight/restore tools与 adversarial tests；production-shaped rehearsal runbook/report；release/rollback readiness verifier；Evidence Manifest；design/review/QA/acceptance与限定文档回写。

清洁度：禁止 placeholder route、fake pass、硬编码样例 token/secret、生产数据拷入 fixture、宽松 JSON、skip/force preflight、debug dump、TODO/FIXME、注释掉代码、无用 import、手写重复 OpenAPI DTO、正式 UI 的“契约/评审注释/G1/G2/G3”、匿名商业字段、未经脱敏的路径/token/receipt/feedback/价格日志。

## 2. 名词与编排

### 2.1 名词层

#### 现状

- 当前业务代码尚无 shoot-planning 实现；七份 sibling child design-review 已 passed 但 design 均为 draft，等待 Epic 统一确认。hardening 不能把 design seam 当作已实现事实。
- 当前 production ops package 是 schema-v1 五件套。`v1-ops-package.py` exact `PACKAGE_FILES`，metadata 只接受 `schema_version=1`；`restore-compose.sh` 在 package snapshot/strict validate 后停止 app、替换 DB/avatar、验证 counts/manifest并恢复服务。
- v1 preflight 已覆盖 project、container/volume identity、checksum、manifest/tar安全、数据库非空和目标 generation fence，但不认识 planning media，不校验 v2 deployment identity、planning missing/orphan 或两个卷展开空间。
- planning-reference-assets design 已承诺 `PlanningMediaManifestV1`、`Inventory` 和 empty target module restore fixture，并明确 production schema-v2/rehearsal归 hardening。
- tracked v2 prototype 是五页静态参考；当前 `prototype-contract.test.mjs` 只验证快照结构，不能证明正式应用状态、API权限或响应式可访问性。

#### 变化

`PlanningV1ConformanceMatrix` 使用版本化 JSON/YAML schema：

```text
PlanningV1ConformanceMatrix
  schema: planning-v1-conformance-matrix-v1
  roadmap_sha256, openapi_sha256
  child_design_refs[{feature,path,sha256,accepted_ref}]
  prototype_ref{path,version,sha256}
  generated_at, build_revision, config_fingerprint
  rows[]

ConformanceRow
  id, category, requirement_ref, owner_feature
  public_seam, scenario_ids[], evidence_ids[]
  required_viewports[], required_failure_ids[]
  status: pending | passed | failed | blocked
  reason?
```

`accepted_ref` 必须来自 Epic 统一确认后的 durable artifact；当前 design 阶段不伪造。矩阵状态只能由 runner结果汇总，不能人工把 failed 改 passed。

```text
PlanningV1E2EScenario
  id, schema_version, title
  preconditions[], actors[], clock_mode
  steps[{actor,action,public_seam,expected_observation}]
  required_oracles[], cleanup_policy

PlanningV1FailureScenario
  id, injection_point, pre_mutation_or_post_mutation
  expected_http_or_cli_error
  expected_db_oracle, expected_object_oracle
  recovery_action, final_oracle

ScenarioResult
  scenario_id, runner_version, build_revision, started_at, finished_at
  status: passed | failed | blocked
  assertions[], artifact_refs[], sanitized_trace_ref?
```

场景定义与结果分离；修改 expected observation 必须推进 scenario schema/hash，不能只重跑后覆盖历史报告。

`ObservationSampleDecision` 和 evidence index：

```text
ObservationSampleDecision
  schema: planning-observation-sample-decision-v1
  decision_id, decision_revision
  cohort: stage_1_g1_g2 | stage_2_g3
  candidate_ref, canonical_shoot_key
  decision: eligible | excluded
  reason: eligible | non_production | not_creative | not_completed |
          unknown_execution_window | duplicate_shoot | not_shared_eligible |
          outside_window | superseded_finalization | invalid_source_fact
  source_fact_refs[{kind,id,revision,sha256?}]
  decided_at, reviewer_ref

EvidenceWindow
  window_id, cohort_schema, opened_at, closed_at
  source_high_watermarks[], calculation_version
  build_revision, config_fingerprint

PlanningV1EvidenceReport
  schema: planning-v1-evidence-index-v1
  window_refs[]
  stage_1{json_path,sha256,gate_version,stale,status}
  stage_2{json_path,sha256,gate_version,stale,status}
  sample_decision_digest
  cohort_comparison{shared_keys[],stage_1_only[],stage_2_only[],limitations[]}
  generated_at, validator_version
  overall_status: passed | failed | stale | blocked
```

`reviewer_ref` 是内部审阅身份引用，不包含 secret。candidate 必须恰有一个 current decision；重复 canonical shoot 在同 cohort 只能有一个 eligible。stage-1 与 stage-2 的 cohort 不能合并成同一分母。

G1/G2 validator 固定：

- G1 分母是窗口内五个 eligible distinct completed ShootPlan；分子是其中存在至少一个 `capture_mode=live` RunModeSession 的 distinct plan。
- G2 只对 eligible completed PlanBuildObservation 的 `active_seconds` 求中位数，同时报告 abandoned/全部 eligible candidate 比例；不把 abandon 塞进 duration median。
- time tick、idle cap、live 判定只验证 owner 已落地规则；hardening 不从浏览器时间或 event 时间聚集重算另一口径。

G3 validator 固定：

- eligible shared shoot 资格沿用 roadmap §4.8/§8.2，按 distinct `(plan_id,slot_id)`；多代 token、多条 feedback/assignment 不增加分母。
- 互动分子是至少一个去重成功互动事实的 distinct eligible key。
- 准备遗漏分母是绑定 `PlanFinalizationSnapshot.current_shot_ids` 的 distinct Shot；分子是 `preparation_missing_event_ids` 所属 distinct Shot，按 Shot 去重，不按 event 条数累计。
- notes、feedback正文、current outcome 或页面状态都不是 preparation-missing 来源。
- completed 后纠错必须 reopen/re-complete；新的 finalization revision 使绑定旧 snapshot 的 stage-2 report stale。旧 report文件保留，不原位改写。

备份包 exact schema：

```text
PlanningBackupPackageV1
  database.sql
  avatar-volume.tgz
  avatar-manifest.json
  metadata.json                    # schema_version=1，既有 exact field set
  SHA256SUMS

PlanningBackupPackageV2
  database.sql
  avatar-volume.tgz
  avatar-manifest.json
  planning-media-volume.tgz
  planning-media-manifest.json
  metadata.json                    # schema_version=2，独立 exact field set
  SHA256SUMS
```

v2 metadata 固定包含：

```text
schema_version: 2
created_at
source_deployment_identity{environment_id,deployment_id,compose_project,storage_layout_version}
compose_file_sha256
images{app,postgres,restore_tool}
postgres_server_major
app_was_running
database_counts
payloads{
  database.sql, avatar-volume.tgz, avatar-manifest.json,
  planning-media-volume.tgz, planning-media-manifest.json
}
manifest_schemas{avatar,planning_media}
package_writer_version
```

所有 object 字段集合 exact，unknown/duplicate key拒绝；payload record固定 `filename/sha256/size_bytes`。`SHA256SUMS`覆盖除自身外全部六个文件；metadata 的 payload digests不包含 metadata本身，避免自引用，顶层 checksum负责 metadata。manifest schema name必须与各自文件内 version exact一致。

`PlanningMediaBackupManifest` 直接封装 planningmedia maintenance 输出的 `PlanningMediaManifestV1` canonical bytes/digest；package adapter不解析成第二套领域清单后重写。tar成员必须与 manifest exact一一对应：missing、orphan、checksum/size mismatch、非regular文件、link/device/traversal全部失败。

```text
PlanningRestorePreflightResult
  schema: planning-restore-preflight-v1
  package_schema: 1 | 2
  package_sha256, deployment_identity_sha256
  checks[{id,status:passed|failed|blocked,reason_key}]
  required_bytes, available_bytes
  source_package_inventory{database,avatar,planning_media}
  target_runtime_inventory{database,avatar,planning_media}
  destructive_authorized: bool       # 仅全部required passed时为true

PlanningRestoreRehearsal
  schema: planning-restore-rehearsal-v1
  rehearsal_id, environment_class: production_shaped_non_production
  deployment_identity_sha256
  source_image_digests, restore_tool_digest, package_sha256
  stage_results[preflight,stop_writes,replace,migrate,validate,reopen]
  database_oracle, avatar_oracle, planning_media_oracle
  failure_cases[], started_at, finished_at
  status: passed | failed | blocked
```

preflight required checks固定：schema/version、顶层成员与文件类型、duplicate/unknown fields、deployment identity、全部 digests、两个 manifests、tar安全、DB dump、PG major兼容策略、target generation/fence、**staging/source package** 的 missing objects 与 orphan objects、available space、restore tool/image兼容性。当前待恢复 target 的内容差异只用于容量、挂载、generation/fence与替换安全判断；v2 restore不能因为target已有missing/orphan就拒绝修复。唯一额外内容门是D5：v1包面对非空planning target必须拒绝，避免跨时代时间线混合。任何 required check unknown 归blocked/failed，不允许pass-with-warning。

`PlanningReleaseReadinessEvidence`：

```text
schema: planning-v1-release-readiness-v1
build_revision, openapi_sha256, migration_head
dependency_conformance_sha256
e2e_result_sha256, failure_matrix_sha256
h1_h2_result_sha256, prototype_a11y_result_sha256
planning_evidence_index_sha256
backup_rehearsal_sha256
archive_readiness{
  current_marker, target_marker, expected_revision,
  trusted_inventory_sha256, planningctl_readback_sha256
}
rollback{
  app_image_digest, understands_active_marker,
  restore_tool_digest, reads_schema_v2,
  schema_down_forbidden
}
required_checks[]
status: passed | failed | stale | blocked
```

任何引用 artifact 内容变化、build/config不匹配、过期、stage report stale、approval hash mismatch 或 rehearsal非真实环境都会令 readiness 非 passed。

#### 接口深度与 dependency strategy

hardening 不建立一个能读全库的 `Repository`。稳定 seam 按职责拆开：

```go
// planningmedia maintenance composition 实现；只供受信 maintenance binary。
type PlanningMediaBackupMaintenance interface {
    GenerateManifest(ctx context.Context) (PlanningMediaManifestV1, error)
    VerifyMountedVolume(ctx context.Context, manifest PlanningMediaManifestV1) error
}

// stage-1 owner实现；不暴露业务mutation或裸Tx。
type PlanningStage1EvidenceSourceV1 interface {
    ExportStage1Facts(ctx context.Context, window EvidenceWindow) (Stage1FactSetV1, error)
}

// stage-2 owner独立实现；hardening coordinator组合两者但不把ownership合并。
type PlanningStage2EvidenceSourceV1 interface {
    ExportStage2Facts(ctx context.Context, window EvidenceWindow) (Stage2FactSetV1, error)
}
```

如果 stage owners 已选择 maintenance CLI 而不是 Go interface，hardening 通过 canonical JSON adapter消费；不得同时保留两个权威来源。接口必须表达版本、窗口、高水位和 closed error，不接受任意 SQL/query string。测试从接口观察 exact fact set、missing/stale和deterministic bytes；匿名安全仍通过公开 route证明，不把 maintenance source当匿名 actor。

### 2.2 编排层

#### 现状

- 当前 v1 backup 顺序为 stop app（若运行）→ pg dump/counts → avatar tar → avatar manifest/metadata/checksums → validate → restart/health → no-replace publish。
- 当前 v1 restore 顺序为 snapshot/validate → stop app → drop/create/restore DB → replace avatar root → avatar/count verify → start/health；destructive failure保持 app stopped。
- 当前原型测试只验证静态快照；跨模块真实 E2E、stage evidence聚合、release readiness和production-shaped rehearsal尚不存在。

#### 变化：实施准入

S1 先执行依赖矩阵：

```text
scan accepted artifacts + implemented generated types + routes + maintenance commands
→ run owner-provided positive/negative conformance probes
→ bind build/openapi/migration/config hashes
→ all conformant ? allow S2 : write blocked_upstream_scope_defect and stop
```

准入工具只写 evidence，不修改业务状态；需要 probe mutation 时使用隔离测试账号/数据库并按场景清理。不能为了通过矩阵加兼容 adapter 到业务代码。

#### 变化：跨模块真实 E2E

主场景 `E2E-PLANNING-V1-001` 固定为：

```text
seed未来shoot slot与consulting/scheduled order
→ 摄取已有讨论文本与已上传图片
→ preview编辑、丢弃/恢复candidate
→ atomic commit形成Shot/Readiness/binding
→ 工作台读取shot list与PublicPlanScale
→ 在真实窗口打开live RunModeSession
→ captured / preparation_missing skipped / captured→cleared / void→新result
→ 签发full share并匿名打开shared asset
→ 客户整案/逐Shot feedback与readiness assignment认领
→ 摄影师看到assignment reminder并处理feedback后深链Shot
→ 保存私有business facts、生成draft、展示stale与显式apply
→ 读取execution history、feedback、reminder、business audit完成最终oracle
```

场景跨 actor 切换 photographer Bearer、anonymous token 和 maintenance observer。每步保存公开 request/response schema hash、用户可见截图/可访问树和数据库/对象的脱敏 oracle；token、receipt secret、feedback正文、价格明细不进入通用日志。

失败矩阵至少包含：

- ingestion core commit成功前 media binding故障：Shot/Readiness/binding/ledger全回滚，candidate可重试；
- Run Mode断网：客户端明确失败/待重试，服务端无伪成功；恢复后同key只形成一次result；
- captured→cleared、真实 skipped→captured、错误skip reason→void+新result：历史均可见，current确定；completed后未reopen纠错拒绝；
- share token expired/revoked/rotated、计划archived：匿名统一404且历史feedback/assignment保留；
- shared asset DB/object/restart/late-delete：exact generation、pin/lease/binding owner结果保持一致，不泄露 object key；
- reminder generation/fence/resolution故障：源 mutation whole-tx回滚或freshness fail-closed，不产生错误recipient；
- business target/rule/plan变化：旧draft stale且Order/Schedule零写，显式再生成后才可apply；
- backup package/volume/manifest损坏：destructive前拒绝；restore replace/migrate/validate故障：保持writes stopped且不报告成功。

failure injection只调用各 owner 已提供的 test seam/fixture或基础设施故障点。若某断点不存在，属于对应 owner evidence缺口，不在 hardening生产代码增加“测试后门”。

#### 变化：H1/H2 组合矩阵

H1 建立同一 build 的 paired baseline：账号 A 永不创建 plan，账号 B 走完整 plan。账号 A 逐一执行客户创建/合并、订单创建/报价/状态流转、schedule create/update/delete、Settings patch、普通 reminder/digest。比较用户可见 response/DOM、允许的 planning_summary 空投影、DB mutation集合和通知 recipient；不得出现“缺策划”、禁用按钮、阻断错误、planning reminder、business draft或观测指标。性能/query预算沿用 CRM owner证据，不把内部空summary batch query误报为业务行为。

H2 采用 exact allowlist + forbidden semantic set双门：

| Surface | required positive | required negative |
|---|---|---|
| proposal read/mutation | proposal创作摘要、moodboard、公开scale、整案feedback | Shot detail、assignment、price/cost/labor/business |
| full read/mutation | full允许的Shot、feedback、assignment | price/cost/business facts/estimated business duration/adjustment/draft |
| shared asset | exact display bytes与安全headers | object key、asset id、business字段、跨plan generation |
| PublicPlanScale链 | core编辑 → share读取 → business只读消费 | share query business=0；business不成为scale owner |
| API/DTO/query | OpenAPI exact union、generated types、SQL owner trace | anonymous handler/import/query不可达business/order price |
| DOM/bundle/log | 用户创作文案与公开窗口时长 | forbidden property/label/chunk import、工程注释、token/receipt/价格日志 |

forbidden semantic set包括同义字段和衍生信号，不能只扫字面 `price`。公开“计划拍摄时长”只能从 execution window `ends_at-starts_at`计算；`estimated_duration_minutes` 即使数值相同也不得成为来源。

#### 变化：prototype、响应式与可访问性 conformance

矩阵按 README page map建立五组：index/workspace/ingestion/run/shared。每个 row记录 prototype intent、上位契约、正式 route/组件、状态、viewport、操作、预期与证据。

必测状态：empty/loading/error/stale/expired、断网失败/待重试、anonymous失效、business draft stale、media unavailable、reminder freshness blocked。Run Mode 只允许执行、参考速查和收尾，不出现Shot结构编辑、价格或经营表单。

破坏性操作均在 mutation 前显示由服务端 current state派生的副作用并二次确认：unlink order、archive plan、移除有执行记录的 Shot、rotate/revoke share、revoke assignment、apply business draft。确认 dialog关闭后焦点返回触发器；server revision变化时旧确认失败并刷新副作用，不能拿静态 prototype 文案当 acknowledgement。

正式 UI 不出现“评审注释”“契约”“fail-closed”“G1/G2/G3”等工程文本；planning list不显示business price badge，business只在detail私有tab。

浏览器矩阵：

- 1600/1280：工作台主层级、六分区、dialog/drawer、长文本和高密度shot list；
- 375 + coarse pointer：Run Mode主门，所有主操作≥44×44 CSS px，无依赖hover，无双向滚动阻断；
- 200% zoom：内容reflow，不丢失操作/错误/label，不出现遮挡；
- keyboard：完整tab顺序、skip link、dialog focus trap/return、Escape、无键盘陷阱；
- screen reader：landmark、heading、form name/description/error、状态变化 live region、shot状态和历史不只靠颜色；
- 强光/对比：正文、focus、disabled、error、captured/skipped/void状态达到项目对比门；截图之外保留计算/人工核验记录。

#### 变化：evidence 生成、验证与 stale

stage producer 在 repeatable read/等价稳定快照中输出 canonical JSON与同名Markdown。hardening validator：

1. strict decode schema、duplicate key、closed enum、stable ordering；
2. 重算 stage JSON 内全部 counts/median/rates、decision digest和canonical hash；
3. 通过 versioned source high-watermark/revision验证事实仍匹配；
4. stage-2逐个读取 current finalization revision，任何reopen/re-complete或binding mismatch标 stale；
5. 比较 approval-report中path/SHA-256/gate version；pending只报告 pending，不伪造failed approval，approved mismatch则failed；
6. 生成 `PlanningV1EvidenceReport` JSON与由它派生的Markdown，不修改 stage报告或approval。

新样本落在已关闭window之外不令历史report stale；修改已引用事实/decision、finalization revision、report bytes或validator contract才stale。要纳入新样本必须新建window/report，不覆盖旧文件。

#### 变化：schema-v2 backup writer

```text
pin endpoint / typed deployment identity / lock / generation fence
→ resolve exact app, postgres, avatar volume, planning-media volume and restore-tool image
→ stop writes and verify old app exited
→ database counts + pg_dump
→ avatar tar + avatar manifest
→ planning-media tar + canonical PlanningMediaManifestV1
→ v2 metadata + SHA256SUMS
→ new-reader validate package, both tar↔manifest exact
→ restore prior app state and health
→ no-replace/fsync publish
```

任何阶段失败不发布 package；若原 app运行，失败清理后尽力恢复原状态，但不能在manifest/volume不可信时把 package标有效。production capability已存在 planning media 时，writer缺卷或maintenance adapter直接失败。

v1 compatibility采用三轨证据：当前五文件 golden 经新 reader通过；冻结上一版 writer生成包经新 reader通过；新 v2包经冻结上一版 reader以stable unsupported-schema/package-membership错误非零拒绝。不能为了“旧 reader友好”让 v2伪装schema=1。

#### 变化：restore preflight 与状态机

```text
snapshot package to private staging
→ strict schema dispatch and deployment identity match
→ verify all digests/manifests/tar members/db dump/PG major/tool compatibility
→ verify staging/source DB/avatar/planning inventories exact；inspect target runtime mounts/generation and calculate required/free space
→ emit preflight result; only all required passed yields destructive_authorized
→ stop writes and prove app/writers exited
→ replace DB, avatar root, planning-media root as one restore attempt
→ run forward migrations with restore-capable current tooling
→ validate DB counts/invariants + avatar exact manifest + planning exact manifest + missing/orphan=0
→ reopen prior service state and health
```

preflight前或失败时目标零写。进入replace后任一失败，app/writers保持stopped，terminal只允许`failed_restore_stopped`，operator按runbook继续修复或重新完整restore；禁止跳到reopen或输出`verified`。不提供自动回滚到混合旧状态，因为跨三个资源无法在未验证快照上声称原子回退。

v1 restore：目标 planning media inventory必须empty/absent；流程仍显式经过planning target check。v2 restore：先替换两个卷，再migrate/validate。validate时对象missing/orphan均为0，checksum/size exact；late-delete/GC worker保持停止直到reopen后由正常reconciliation恢复。

#### 变化：production rehearsal、发布与回退

production-shaped rehearsal由owner另行授权，在隔离deployment：创建已知PG/avatar/planning fixtures → v2 backup → 对三资源做可观察破坏 → restore → migrations → API/browser/manifest验证 → 重启后再次读取 → 运行一个preflight拒绝case和一个destructive failure-stop case。报告只含脱敏ID/hash，不含生产数据。

release verifier按固定顺序核验而不执行：

```text
additive schema available
→ candidate/rollback binaries understand all three archive markers
→ old processes confirmed exited
→ trusted readiness inventory archived and fresh
→ release-only planningctl adjacent CAS + authoritative readback evidence
→ real share/reminder writer and participant enabled
→ schema-v2 writer paired with v2-capable restore tool/image
```

回退不做schema down、不降低marker、不恢复旧reader。应用可回退到明确理解当前marker/DB additive schema的兼容镜像；v2 restore tool作为独立sidecar/maintenance image继续保留并可调用。若旧应用镜像不理解active marker或需要旧reader，rollback证据直接blocked。

### 2.3 挂载点

| 挂载点 | 作用 | 删除后 feature 是否消失 |
|---|---|---|
| production backup/restore public CLI + `planningbackup` strict package modules | schema-v2 writer、dual reader、preflight、restore状态机 | 是 |
| planningmedia maintenance composition | 生成/验证 canonical manifest 与mounted volume | 是，v2无法 exact restore |
| repository browser E2E/conformance runner | 跨模块、H1/H2、prototype、responsive/a11y场景 | 是，发布门缺失 |
| roadmap `evidence/` canonical report validator/index | G1–G3 hash/stale/cohort审计 | 是，evidence无法机械复核 |
| release readiness CLI/runbook | 汇总发布/rollback/rehearsal并fail-closed | 是，不能形成首版发布判定 |

内部测试 helper、page object、fixture factory 和 screenshot目录不是独立挂载点。

### 2.4 推进策略

1. **上游准入与基线**：建立 dependency conformance、命令/route/generated type矩阵；任何缺口先阻塞并归属原 owner。
2. **package schema与dual reader**：先冻结v1 characterization，再实现独立v2 model/writer/strict reader和adversarial corpus。
3. **restore状态机**：实现完整preflight、space/object差异、固定阶段、failure-stop与真实三资源fixture。
4. **跨模块E2E与失败矩阵**：在真实API/PG/volume上完成主路径、断网/并发/重启/late-delete/stale。
5. **前端一致性与可访问性**：把五页原型意图映射到正式应用并运行viewport/input/assistive matrix。
6. **G1–G3 evidence validator**：严格重算stage报告、sample decisions、cohort和stale/approval binding，生成首版索引。
7. **发布/回退与production rehearsal**：产出operator runbook、真实演练证据和release readiness；任何外部动作停在owner authorization。
8. **全仓门禁与交付审计**：重跑全部命令、场景与Evidence Manifest，反查无越权scope、placeholder或遗漏artifact。

每步证据必须写入 Evidence Manifest：producer step、command/test selector、输入schema/hash、输出path/schema、required assertions/counters、build revision、config/deployment fingerprint、sanitization policy。只贴终端“passed”不够。

### 2.5 结构健康度与微重构

文件级事实：`scripts/lib/v1-ops-package.py`约961行、`v1-ops-common.sh`约543行、`v1-ops-smoke-runner.py`约1941行，已混合schema decode、tar/manifest、runtime、fault harness和evidence组装。继续把v2字段、planning manifest、space preflight和release evidence直接追加进去会扩大既有职责混合。

目录级事实：`scripts/lib/`当前已有多个v1 ops/auth helper；hardening至少新增package model、preflight、manifest adapter、rehearsal result、release evidence和测试fixture，若全部摊平会形成命名前缀目录。

结论：不在本 feature 前先重构既有 v1 文件，以免在生产恢复路径上制造纯搬迁风险；同时禁止继续向三个胖文件堆v2主体。新增代码放入职责化相邻目录（建议 `scripts/lib/planningbackup/` 与 `scripts/lib/planninghardening/`），现有 public shell只增加薄的schema dispatch/stage调用；v1 legacy decoder通过characterization adapter复用。真实浏览器场景放 `frontend/e2e/shoot-planning/` 或仓库既有E2E convention，不堆入单个两千行script。

这不是“先搬再改”的微重构，因此 checklist不增加独立搬迁step。若实现发现必须修改v1 helper内部结构才能双读，只允许先做可单独验证的函数抽取且五件套golden、错误key、CLI stdout/stderr/exit code、signal/cleanup和old package restore全部零漂移；否则停止并另走 `cs-refactor`，不能夹带语义改写。

建议在 implementation通过后沉淀目录 convention：部署级package schema采用独立version decoder，legacy characterization不可与新schema optional字段合并。

## 3. 验收契约

### 3.1 可观察场景

| ID | 输入 / 触发 | 期望可观察结果 | 证据 |
|---|---|---|---|
| A1 | hardening开始时扫描七个依赖、Goal gate、generated types和maintenance seams | 每项为conformant并绑定accepted/build/hash；缺实现或scope缺口时S2–S8零diff并指出owner | dependency matrix + import/route/command probes |
| A2 | 真实账号执行完整摄取→live run→full协作→reminder→business闭环 | 所有actor按权限成功，Shot/history/feedback/assignment/reminder/draft/audit终局一致 | browser recording + API/PG/object oracles |
| A3 | ingestion atomic commit在media binding或core batch断点失败后重试 | 首次Shot/Readiness/binding/ledger无部分写，candidate可恢复；同key重试只成功一次 | fault injection + PG/object diff |
| A4 | Run Mode断网提交result，再恢复相同/不同请求 | UI明确失败/待重试；相同key只一个result，不同frame conflict；不伪装captured | offline browser + API/event oracle |
| A5 | captured→cleared、preparation_missing skipped→captured、错误skip→void+新result | current outcome确定，全部历史仍可见；错误reason不删除；completed未reopen纠错409 | event/finalization fixture + browser |
| A6 | 无plan账号执行客户、订单、报价、schedule、settings、普通reminder路径 | 零“缺策划”提示/阻塞/额外planning mutation/客户触达；指标不在摄影师UI | paired H1 response/DOM/DB/recipient diff |
| A7 | proposal read/feedback与full read/feedback/assignment mutation | proposal/full按各自exact allowlist；所有匿名响应零商业字段/衍生信号 | OpenAPI/API exact key golden + query guard |
| A8 | shared asset读取、跨plan generation、DB/object重启与late-delete | 合法exact display可读；非法统一拒绝且无object key；pin/lease/GC终局符合media owner | HTTP headers/body + volume/DB/restart oracle |
| A9 | core修改PublicPlanScale后share读取、business生成解释 | share读取公开scale，business只读消费；share business query=0，business不复制scale owner | three-module trace + SQL/import guard |
| A10 | full token过期、撤销、轮换、plan archive后进行read/mutation | 统一404；匿名DOM不泄露原因/商业数据；feedback/assignment历史保留 | API/browser + PG safe projection |
| A11 | 摄影师adopt/ignore整案与Shot feedback | Shot feedback disposition后深链目标Shot并正确focus；整案回反馈分区 | browser navigation/focus recording |
| A12 | readiness assignment创建/撤销、slot改期、timezone和shoot-start边界 | reminder只到摄影师，generation/resolution contiguous/fresh；撤销/过期后不误发 | PG/freshness/recipient/digest fixture |
| A13 | business facts生成fresh draft，随后plan/rule/target变化并尝试apply | 旧draft stale且Order/Schedule零写；新draft二次确认后owner mutation与audit全成或全败 | browser + PG fault/CAS oracle |
| A14 | 五页page map逐row运行empty/loading/error/stale/expired与正常态 | 每row有正式route/component和证据；冲突按roadmap→OpenAPI→confirmed design→prototype | conformance JSON + screenshots |
| A15 | unlink/archive/remove executed Shot/rotate-revoke/revoke assignment/apply draft | mutation前显示current副作用并二次确认；revision漂移拒绝旧确认；focus return正确 | browser/API acknowledgement matrix |
| A16 | 扫描正式planning UI/list/detail和构建bundle | 无评审注释/契约/G指标；list无business price badge，business仅私有detail tab | DOM/bundle/component guard |
| A17 | 1600、1280、375/coarse、200% zoom运行主场景 | 无关键遮挡/双向阻断；Run Mode execution-only；主触控≥44px且不依赖hover | screenshots/video + computed geometry |
| A18 | keyboard/screen reader/强光对比执行dialog、drawer、forms、status/history | 无键盘陷阱，语义/错误/live status可读，focus trap/return正确，状态不只靠颜色 | accessibility tree + manual/automated audit |
| A19 | 输入stage-1五个eligible样本、duplicate/excluded与abandon | G1 distinct live分子/5、G2 successful median与abandon ratio重算一致；全部candidate有decision | canonical JSON validator golden |
| A20 | 输入stage-2多token/多互动、多个preparation events同Shot与不同cohort | 互动按distinct plan/slot；遗漏按snapshot distinct Shot；event不重复计数；cohort差异单列 | canonical JSON + finalization golden |
| A21 | completed后reopen/re-complete、decision revision变化、stage JSON bytes或approval hash变化 | 旧report保留但stale/failed；未批准只显示pending；不得修改approval或静默重新绑定 | stale/hash/approval validator fixture |
| A22 | 当前/冻结writer产生schema-v1包，新reader读取；v2包交冻结old reader | v1成员/metadata/checksum/error/restore characterization零漂移；old reader明确拒绝v2 | golden packages + old/new reader matrix |
| A23 | v2包含合法PG/avatar/planning；分别注入unknown field、坏digest、missing/orphan/link/traversal/空间不足/identity mismatch | 全部在destructive前failed/blocked，目标PG/两卷/app状态零变化 | adversarial corpus + preflight mutation oracle |
| A24 | production-shaped部署执行v2 backup、破坏三资源、restore | 阶段顺序固定；migrate后DB/两个manifest exact，missing/orphan=0，reopen健康且重启可读 | signed rehearsal JSON + package/tool/image hashes |
| A25 | restore在DB replace、avatar replace、planning replace、migrate、validate断点失败 | 不输出verified/complete，writers保持stopped；下一次必须从完整preflight/restore恢复 | destructive failure-stop matrix |
| A26 | 生成release readiness，输入旧进程未退出、非相邻marker、旧rollback image或旧restore tool | 任一情况非passed且无promotion/write；全满足时只产出passed evidence，不执行发布 | release verifier + planningctl structured outputs |
| A27 | 最终build运行全部核心命令和Evidence Manifest审计 | `make check`等全绿，artifact hash/build/config一致，无placeholder/secret/匿名商业信号 | command logs + manifest verifier + diff audit |

### 3.2 Acceptance Coverage Matrix

| 核心能力 | 场景 | Checklist step | 证据类型 | blocking |
|---|---|---|---|---|
| 上游ownership与准入 | A1 | S1 | contract/import/command matrix | yes |
| 全链路与失败恢复 | A2–A5、A8、A10–A13 | S4 | browser + API + PG/object/restart | yes |
| H1/H2 | A6–A10、A16 | S4/S5 | exact DTO/query/DOM/bundle/DB | yes |
| tracked v2一致性 | A14–A18 | S5 | conformance + viewport/a11y | yes |
| G1–G3与stale | A19–A21 | S6 | canonical JSON/hash validator | yes |
| schema-v1/v2兼容 | A22–A23 | S2/S3 | golden/adversarial/preflight | yes |
| exact restore与failure-stop | A24–A25 | S3/S7 | production-shaped rehearsal | yes |
| 发布/回退门 | A26 | S7 | release/readiness structured evidence | yes |
| 全仓DoD | A27 | S8 | commands/manifest/diff | yes |

### 3.3 DoD Contract

| ID | 层级 | 完成条件 | 证据 | 失败处置 |
|---|---|---|---|---|
| DOD-DESIGN-001 | Design | 本design、checklist、独立design-review passed且仍为draft等待Epic统一确认 | artifacts/frontmatter/hashes | blocking |
| DOD-IMPL-001 | Implementation | 依赖严格done后S1–S8全部完成；无上游暗补、placeholder或跳过preflight | checklist/evidence/import diff | blocking |
| DOD-REVIEW-001 | Code Review | 独立review核验ownership、匿名负向、strict decoders、restore状态机与release无副作用 | review findings closure | blocking |
| DOD-QA-001 | QA | A1–A27、真实browser/PG/volumes/restart/fault/old-reader均有可重放证据 | QA + Evidence Manifest | blocking |
| DOD-ACCEPT-001 | Acceptance | production-shaped rehearsal、G1–G3 index、release/rollback readiness与全仓命令全部匹配最终build | acceptance + hashes | blocking |
| DOD-NON-AUTO-001 | Authorization | 没有自动remote push/merge/publish/release/deploy/promotion/cutover；外部rehearsal和生产动作均有独立owner授权 | action log/approval refs | blocking |

### 3.4 必跑验证命令

| ID | 命令 | 用途 | 基线说明 |
|---|---|---|---|
| CMD-001 | `make generate-check` | OpenAPI Go/TS生成物漂移 | 当前存在 |
| CMD-002 | `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/planningmedia/... ./internal/planshare/... ./internal/reminder/... ./internal/order ./internal/schedule ./internal/settings ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1` | owner模块、composition与maintenance | 依赖落地后应存在；缺包按实际accepted结构更新但不得缩scope |
| CMD-003 | `cd frontend && npm run test:shoot-planning` | planning unit/contract runners | 当前不存在，前序child必须形成或S1阻塞 |
| CMD-004 | `cd frontend && npm run test:shoot-planning:e2e` | 真实浏览器E2E/H1/H2/prototype/a11y | 当前不存在，本feature交付 |
| CMD-005 | `cd frontend && npm run test:prototype && npm run build && npm run lint` | tracked快照、构建、lint | 当前除planning正式runner外可用 |
| CMD-006 | `python3 scripts/lib/planningbackup/selftest.py` | v1/v2 strict package与adversarial corpus | 本feature交付 |
| CMD-007 | `bash scripts/test-planning-ops-backup-restore-safety.sh` | signal/cleanup/preflight/阶段/failure-stop | 本feature交付 |
| CMD-008 | `make check` | 全仓最终门 | 当前存在，须纳入新增runners |
| CMD-009 | `./scripts/rehearse-planning-backup-restore.sh --config <owner-approved-production-shaped-config> --evidence <private-evidence-dir>` | 真实production-shaped rehearsal | 非自动；需owner环境/操作授权，缺授权时acceptance handoff而非伪pass |
| CMD-010 | `./scripts/verify-planning-v1-release-readiness.sh --evidence <evidence-manifest> --json` | 最终release/rollback fail-closed判定 | 本feature交付；只读不promotion |

基线预检在S1执行。已有红灯按build revision、命令、错误和owner单独记录；不得删除断言、降级命令或把未实现runner标N/A来获得绿灯。

### 3.5 Required Artifacts

- `planning_v1_dependency_conformance.json`
- `planning_v1_conformance_matrix.json`
- `planning_v1_e2e_results.json` 与 failure matrix
- `planning_v1_h1_h2_negative_matrix.json`
- `planning_v1_prototype_responsive_a11y_matrix.json`
- 1600/1280/375/200%/keyboard/screen-reader/coarse-pointer证据
- stage-1/stage-2 canonical JSON与Markdown、`ObservationSampleDecision` digest、`planning_v1_evidence_index.json`
- schema-v1 old/new writer-reader golden matrix
- schema-v2 package golden、adversarial corpus、preflight mutation oracle
- restore stage/failure-stop matrix
- `planning_restore_rehearsal.json` 与 operator runbook
- `planning_v1_release_readiness.json`、archive readiness/readback与rollback tool retention evidence
- Evidence Manifest、sanitization report、diff summary
- design-review、implementation step evidence、code review、QA、acceptance、roadmap/CONTEXT/docs限定回写

## 4. 文档与架构回写预判

- `docs/prototypes/creative-shoot-planning/v2/README.md` 继续是冻结UI参考，不写运行结果；conformance结果放本feature evidence。
- 新增/更新 deployment runbook，明确schema-v1/v2成员、deployment identity、preflight、固定restore顺序、failure-stop、production-shaped rehearsal、rollback保留v2 reader。账号export文档明确不包含media。
- `.codestable/requirements/CONTEXT.md` 在acceptance后只补 `planningbackup`、`PlanningV1EvidenceReport` 与 release evidence术语；不把hardening登记为业务域。
- roadmap item只有全部验收通过后才从`in-progress`改`done`；G1–G3 approval disposition和production动作仍按canonical approval/workflow独立记录。
- 现有ADR-001/ADR-004继续约束AccountScope例外、durable adjunct与备份原则。若deployment identity来源、restore sidecar保留或外部对象存储引入新的长期结构决策，再走`cs-domain`；当前design不提前创建ADR。

Non-Automatic Actions：本design及未来Goal授权均不自动执行remote push、merge、publish、release、deploy、promotion或production cutover；CMD-009、真实环境采样和任何control-plane动作都需要owner另行授权。
