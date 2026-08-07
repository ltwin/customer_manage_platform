---
doc_type: approval-report
unit: creative-shoot-planning
status: pending
reason: goal-execution-authorization-and-staged-evidence-gates
approvals:
  epic-split: approved
  prototype-alignment: approved
  roadmap-plan: approved
  creative-shoot-planning-requirement: approved
  round-8-share-contract: approved
  review-round-cap-local-closure: approved
  crm-terminal-review-blocking-disposition: approved
  crm-local-revision-final-acceptance: approved
  all-feature-designs: approved
  goal-acceptance: approved
  goal-commits: approved
  stage-1-evidence-go: pending
  stage-2-evidence-go: pending
approval_groups:
  epic-split-2026-08-04:
    status: approved
    confirmation_id: chat-2026-08-04-split-creative-planning-intelligence
    decisions: [epic-split]
  prototype-alignment-2026-08-05:
    status: approved
    confirmation_id: chat-2026-08-05-apply-prototype-alignment-audit
    decisions: [prototype-alignment]
  roadmap-confirmation-2026-08-05:
    status: approved
    confirmation_id: "80e741c3-06ba-407e-8d4c-5a9b71b66702"
    decisions: [roadmap-plan]
  requirement-confirmation-2026-08-05:
    status: approved
    confirmation_id: "62287be4-20b0-4d17-9b50-9c0409a7f5a2"
    decisions: [creative-shoot-planning-requirement]
  round-8-share-contract-2026-08-05:
    status: approved
    confirmation_id: "d83f1556-b56e-440b-b1bd-a45526c21f70"
    decisions: [round-8-share-contract]
  review-round-cap-local-closure-2026-08-05:
    status: approved
    confirmation_id: "1909fda0-52dd-4a74-978a-842de1d7c892"
    decisions: [review-round-cap-local-closure]
  crm-terminal-review-blocking-disposition-2026-08-05:
    status: approved
    confirmation_id: "42fab1d9-3f1c-4106-9d99-44efa7172909"
    decisions: [crm-terminal-review-blocking-disposition]
  crm-local-revision-final-acceptance-2026-08-05:
    status: approved
    confirmation_id: "ff30ebbe-5282-4e87-be21-7bca4669b166"
    decisions: [crm-local-revision-final-acceptance]
    candidate_refs:
      crm_design_sha256: "f9feb4a87fe912ebe67f5d4bd88d3452941379389efe4d8120c3cb1e23a33b7c"
      crm_checklist_sha256: "2c15c5b5739a3e2d04964d989f10649ef5cf339e374925411790ce70f6d4ed4c"
      reminder_design_sha256: "82d0c11636416c45d6a525498071fe3af60d80955633cb9307096e07a53cde1d"
      reminder_checklist_sha256: "282367b0f8048c46f013e8f25ead6711991bb25524474866cb30b402ad0065fe"
  all-feature-designs-confirmation-2026-08-05:
    status: approved
    confirmation_id: "04922f34-3db7-470b-b8eb-b685f2bad8e8"
    decisions: [all-feature-designs]
    candidate_refs:
      manifest_version: "creative-shoot-planning-child-design-batch-v1"
      batch_manifest_sha256: "72016767e2ffbfa9fb059778d0bd844619e90a9b483fc635431c6162a1ddf008"
      child_count: 8
  goal-execution:
    status: approved
    confirmation_id: "0cfee048-fd76-491a-925c-caa8bf9eb434"
    decisions: [goal-acceptance, goal-commits]
    candidate_refs:
      goal_plan_sha256: "ebf83c6e6522fa7dd3a878fe74ce2c59bd02745dbe204534c705e79a1a2a8820"
      goal_state_sha256: "314f15f6c5b7dfb4f3f5083ef80ae606e23d1238e55b9253aaccf3ce91e0f820"
      evidence_runner_sha256: "a4a182006053fbf9ae78a4a7d6886a7619b9c7373c5e40269240b33e1aef25fc"
      acceptance_ref: "approval-report.md#goal-acceptance"
      commit_ref: "approval-report.md#goal-commits"
      workspace_option: "new-worktree-from-develop"
approval_evidence:
  stage-1-evidence-go:
    path: ""
    sha256: ""
    gate_version: ""
  stage-2-evidence-go:
    path: ""
    sha256: ""
    gate_version: ""
created_at: 2026-08-02
updated_at: 2026-08-06
roadmap_confirmed_at: 2026-08-05
selected_options:
  roadmap-confirmation: Option A
  requirement-confirmation: Option A
  round-8-share-contract: Option A
  review-round-cap-local-closure: Option A
  crm-terminal-review-blocking-disposition: Option A
  crm-local-revision-final-acceptance: Option A
  all-feature-designs-confirmation: Option A
  goal-execution: Option A
---

# 创作型拍摄策划首版批准报告

## Decision History

- 2026-08-02：收敛前 11 条混合路线的批准请求已失效；它不得被 runtime 或后续 agent 当成批准证据。
- 2026-08-04：owner 明确同意拆成“首版 epic + AI/知识后置 epic”。已将 `epic-split` 记录为 approved，confirmation id 为 `chat-2026-08-04-split-creative-planning-intelligence`。
- 2026-08-04：原 `void` 状态纠正为历史 superseded 语义；当前 canonical report 全量重建，不继承旧八项 pending decision。
- 2026-08-05：owner 同意按原型审计建立 `docs/prototypes/creative-shoot-planning/v2/` 并同步缺失契约。已将 `prototype-alignment` 记录为 approved，confirmation id 为 `chat-2026-08-05-apply-prototype-alignment-audit`；这不是 `roadmap-plan` 或执行批准。
- 2026-08-05：独立 roadmap review round 3 已通过后，owner 明确“批准 roadmap，现在进入下一步”，选择 Option A。已将 `roadmap-plan` 记录为 approved，confirmation id 为 `80e741c3-06ba-407e-8d4c-5a9b71b66702`，并同步激活 roadmap。该确认只批准首版规划基线与进入 requirement/child design 流程，不批准实现、future evidence go、Goal execution、commit/push、merge 或 deploy。
- 2026-08-05：owner 回复“确认”，精确批准 draft ref `creative-shoot-planning-v1-2026-08-05-r1`。已将 `creative-shoot-planning-requirement` 记录为 approved，confirmation id 为 `62287be4-20b0-4d17-9b50-9c0409a7f5a2`；草案已原样写入需求中心并解除首条 child design 前置门禁。
- 2026-08-05：`plan-share-collaboration` design review 推导出 caller-generated receipt 才能同时满足“服务端只存 commitment”和response-loss exact replay。该变化会新增WebCrypto/CSPRNG前提，并使丢失secret后服务端无法恢复；独立roadmap round 8要求supplemental owner confirmation。已创建`round-8-share-contract=pending`，不得沿用原roadmap confirmation自动批准。
- 2026-08-05：owner 对聚焦的 round-8 share-contract checkpoint 回复“确认”，选择 Option A。已将 `round-8-share-contract` 原子更新为 approved，confirmation id 为 `d83f1556-b56e-440b-b1bd-a45526c21f70`；确认范围仅为 caller-generated receipt、服务端 only-commitment、24 小时幂等恢复窗口及 secret/selector 的不可恢复代价，不重批整个 roadmap，也不授权实现、Goal execution、evidence go、AI/provider 调用、Git 或发布。
- 2026-08-05：owner 要求同一 feature design 后续默认不要超过 3 轮完整复审。该规则已写入 `.codestable/attention.md`；已启动的 core/share/reminder 复审作为各自最后一轮自动复审，不再自动追加 Round 12/11/7。share 与 reminder 的最后 finding 已本地修订但属于实质契约变化，CRM Round 6 与 parent Round 9 尚未实际启动，因此建立 `review-round-cap-local-closure=pending`，等待 owner 一次性选择终局处置。
- 2026-08-05：owner 回复 `A`，批准 `review-round-cap-local-closure` 的混合终局收敛方案，confirmation id 为 `1909fda0-52dd-4a74-978a-842de1d7c892`。share Round 10 后与 reminder Round 6 后的本地闭合获准走 owner-approved local-only lane，不再追加 Round 11/7；CRM Round 6 与 parent roadmap Round 9 各获准且只获准一次终局独立复审。终局复审后的 non-blocking 归 residual risk；仍有 blocking 时停回 owner，不自动再审。本批准不授权实现、QA、Goal execution、AI/provider、Git 或发布。
- 2026-08-05：终局 parent roadmap Round 9 独立复审为 `passed`（0 blocking/important/nit）；不再增加 Round 10，两个非阻塞观察转为 quarantine repair/RTO 与 generation-history query budget residual risk。终局 CRM Round 6 为 `changes-requested`（1 blocking、4 important）；主 agent 已核验 finding 均成立。依 Option A 不自动修订/启动 Round 7，建立 `crm-terminal-review-blocking-disposition=pending` 等待 owner 选择本地修订收敛、重做/缩小范围或显式例外复审。
- 2026-08-05：owner 回复 `A`，批准 `crm-terminal-review-blocking-disposition` 的“一次性本地修订后 owner 终审”，confirmation id 为 `42fab1d9-3f1c-4106-9d99-44efa7172909`。授权范围仅为关闭 CRM Round 6 的 1 blocking/4 important、同步必要 reminder/prototype/checklist 契约并做 owner-approved local review；不启动 Round 7。修订完成后仍须建立新的 final local-only acceptance checkpoint，不能直接进入实现。
- 2026-08-05：已按上述授权完成一次性本地修订与local-only核验：拆分connection/projection revision owner并冻结CAS真值表，闭合direct link与deleted-unlink，补PlanningSummary exact semantics，建立CRM-owned reminder fact union，并绑定tracked v2 conformance。当前CRM design/checklist及必要Reminder同步hash已写入`crm-local-revision-final-acceptance-2026-08-05.candidate_refs`；未启动Round 7，也未把本地结论直接标为passed。
- 2026-08-05：owner 对修订后的最终checkpoint回复`A`，批准`crm-local-revision-final-acceptance`，confirmation id为`ff30ebbe-5282-4e87-be21-7bca4669b166`。批准精确绑定group中的四份candidate hash；CRM Round 6可按owner-approved local-only模式标记passed并交回child design batch。本确认不批准implementation、Goal execution、stage-1/2 evidence、AI/provider、Git或发布。
- 2026-08-05：八条 child feature 均已有 `draft` design、pending checklist 与 `passed` design-review；Epic workflow进入 `all-feature-designs-confirmation`。已建立 `all-feature-designs=pending`，绑定按路径排序的24份design/checklist/design-review SHA-256 manifest（batch SHA-256=`72016767e2ffbfa9fb059778d0bd844619e90a9b483fc635431c6162a1ddf008`）。owner尚未确认前不把任何design改为approved，不生成Goal package，不进入实现。
- 2026-08-06：owner 对统一的 `all-feature-designs` checkpoint 回复 `A`，批准 confirmation id=`04922f34-3db7-470b-b8eb-b685f2bad8e8` 所绑定的 8-design batch；批准前已重新计算 24 份 design/checklist/design-review manifest，SHA-256 仍为 `72016767e2ffbfa9fb059778d0bd844619e90a9b483fc635431c6162a1ddf008`。八份 design 已从 `draft` 同步为 `approved`，并允许进入 Goal package。本确认不授权 Goal execution、implementation、stage-1/2 evidence go、AI/provider 或知识库能力、Git 写操作、publish、release、deploy、promotion 或 production cutover。
- 2026-08-06：已生成 creative-shoot-planning Goal package，8 条 feature 按 DAG 写入 `goal-state.yaml`，两项 Goal 授权保持 pending。`planning-evidence-dispatch-gate.py` 已用当前 stage-1/stage-2 pending decision 自测，均返回 `needs-human`/exit 2；错误 mapping 返回 `blocked`/exit 4。当前工作区仍在 `main` 且有本 Epic 未提交规格，未执行 branch/worktree/stash/cherry-pick/commit；已建立 `approval_groups.goal-execution=pending`，等待 owner 选择执行与 workspace 方案。
- 2026-08-06：owner 明确“批准 Goal execution”，选择 Option A；已用同一次回答原子批准 `goal-acceptance` 与 `goal-commits`，confirmation id=`0cfee048-fd76-491a-925c-caa8bf9eb434`。批准前重新计算 Goal plan、Goal state 与 evidence runner 三个冻结 SHA-256，均与 `approval_groups.goal-execution.candidate_refs` 一致；同时授权从 `develop` 准备 `feat/creative-shoot-planning` 独立 worktree，并只迁移本 Epic 可归因的已批准基线。`stage-1-evidence-go`、`stage-2-evidence-go` 与 production-shaped rehearsal 继续 pending；本确认不授权 remote push、PR、merge、publish、release、deploy、promotion、production cutover、真实试点/样本采集、AI/provider 调用或后置知识库能力。

## Approved Scope: `prototype-alignment`

本次批准只覆盖：把前端原型固化为受版本控制的 v2 参考；修正匿名链接与公开计划时长的歧义文案；在既有 8 条首版路线内补齐 Readiness、可审计执行纠错、`PublicPlanScale`、正式 assignment/reminder 边界与原型 conformance 验收。

它不批准 `roadmap-plan`、`stage-1-evidence-go`、`stage-2-evidence-go`，也不授权激活 roadmap、创建 child feature、进入 design/implementation、调用 provider、commit/push 或 deploy。上述三个 named gate 继续保持 `pending`。

## Decision Recorded

### `roadmap-plan`

已批准。独立 roadmap review round 3 为 `passed`；owner 选择 Option A，当前 8 条首版路线成为 child design 的有效规划基线，roadmap 已由 `draft` 激活为 `active`。批准范围不包含实现或后续 evidence gate。

## Completed Checkpoint: `round-8-share-contract`

Owner 已选择 Option A；`round-8-share-contract` 为 `approved`，confirmation id 为 `d83f1556-b56e-440b-b1bd-a45526c21f70`。本次补充确认只覆盖分享凭证的生成/恢复契约，不重批 8-item roadmap、DAG 或 evidence gate。

Owner 已接受的产品代价：

1. 客户浏览器在assignment claim请求前使用Web Crypto/CSPRNG生成`cr1.<32-byte secret>`；不支持安全随机数时禁用认领，不降级弱随机码；
2. 请求只提交SHA-256 commitment；服务端business DB、成功response与idempotency ledger都不保存/返回明文secret；
3. claim成功后前端只展示当前组件内存中的本地secret一次；通用idempotency ledger当前TTL为24h，TTL内response-loss exact replay返回同业务结果；超过TTL按当前状态重新执行，不承诺永久重放；
4. 分享issue/rotate的新selector也只在首次响应或TTL内exact replay可恢复；若响应丢失超过TTL且新generation已active，只能再次rotate，不能从服务端读取完整URL；
5. receipt secret一旦被用户关闭/丢失，服务端无法找回；客户只能联系摄影师撤销assignment，不能靠昵称恢复身份。

相关但不单独扩张产品范围的技术上下文：anonymous路径使用sealed dual-view transaction capability而不是伪造AccountScope；补齐on-site offer/share管理seam；archive使用`planning-share-v1`精确说明active links失效、feedback与assignment保留。

该 checkpoint 不授权 implementation、Goal execution、stage-1/2 evidence go、AI/provider 调用、commit/push、merge、release 或 deploy。技术设计仍须独立复审通过；owner 的产品取舍确认不能替代 core/share design-review。

## Completed Checkpoint: `review-round-cap-local-closure`

### 确认结果

Owner 已选择 Option A；`review-round-cap-local-closure` 为 `approved`，confirmation id 为 `1909fda0-52dd-4a74-978a-842de1d7c892`。该 checkpoint 用于把历史上已经超过新上限的候选收敛为一次终局决策，不改变未来规则：新 feature design 仍默认最多 3 轮完整独立复审（首轮 + 最多两轮修订复审）；第 3 轮后不自动增加第 4 轮，非阻塞项转 residual risk，仍有 blocking 则停回 owner。

当前事实：

1. `shoot-plan-core` Round 11 已由独立 reviewer 判定 `passed`，当前候选 hash 与冻结 hash 一致，无需再审；
2. `plan-share-collaboration` Round 10 关闭 populated-down TOCTOU 后发现 IP digest 跨午夜额度重置；当前候选已新增 current/previous digest guard 与 rollover continuity fixture，但这属于实质安全契约修订，尚无 Round 11 独立复审；
3. `plan-assignment-reminders` Round 6 发现 final SQL→method return 耗时未从业务余量扣除；当前候选已改为 dual-monotonic budget 并补 100ms/80ms/120ms 边界 fixture，但这属于实质发送授权修订，尚无 Round 7 独立复审；
4. `shoot-plan-crm-integration` Round 6 候选 hash 已冻结，但 reviewer slot 从未实际启动；parent roadmap Round 9 同样尚未实际启动。两者都包含实质 generation/reminder 契约增量，不能伪装成已独立复审通过。

#### Option A — 混合终局收敛（已选）

- 批准 share Round 10 后与 reminder Round 6 后的 local-only closure；两份 design-review 直接以 owner-approved fallback 收敛，不再追加 Round 11/7；
- CRM Round 6 与 parent roadmap Round 9 各允许且只允许一次终局独立复审，因为它们的当前实质候选尚未被 reviewer 看过；
- 终局审查若只剩 non-blocking，写入 residual risk 后收敛；若仍有 blocking，修订后不自动复审，回到 owner checkpoint 决定接受风险、缩小/重做设计或显式授权例外轮次。

#### Option B — 全部再做一次终局独立复审

- share、reminder、CRM 与 parent roadmap 各允许一次终局独立复审；
- 这是最严格但最耗时的选择；任何后续修订都不再自动加轮，按第 3 轮后的同一规则回 owner。

#### Option C — 全部 local-only 收敛

- owner 明确接受 share/reminder 的本地 finding closure，并同时批准 CRM/current parent 候选跳过尚未执行的独立复审；
- 这是最快但风险最高的选择。CRM generation transaction 与 parent cross-feature contract 将只有主 agent 的文档/机械核验，不具备独立 reviewer 证据。

本次已选择 Option A；Option B/C 保留为历史备选，不再构成 pending decision。

#### Non-Automatic Actions

无论选择哪项，都只处理 design/roadmap review gate；不会自动批准全部 child design、进入 implementation/QA/acceptance、生成或派发 Goal execution、调用 AI/provider、使用凭证、commit、push、PR、merge、publish、release、deploy、promotion 或 production cutover。`stage-1-evidence-go` 与 `stage-2-evidence-go` 继续独立保持 `pending`。

## Completed Checkpoint: `crm-terminal-review-blocking-disposition`

### 确认结果

Owner 已选择 Option A；`crm-terminal-review-blocking-disposition` 为 `approved`，confirmation id 为 `42fab1d9-3f1c-4106-9d99-44efa7172909`。CRM Round 6 当前候选无法给 implementation 提供唯一的 revision/CAS oracle，并同时缺 direct link/deleted-unlink、PlanningSummary exact semantics、reminder lifecycle compile seam 与 tracked v2 conformance；本次只授权一次性本地修订和本地核验，不启动 Round 7。

#### Option A — 本地修订后 owner 终审（已选）

- 主 agent 一次性修订 CRM design/checklist，并同步必要的 reminder child/parent/prototype conformance 引用；
- 只做机械检查、契约反向扫描与 owner-approved local review，不再启动完整独立 Round 7；
- 修订结果返回 owner 做一次最终 local-only acceptance；owner 明确确认前仍保持 `draft`，不进入实现。

#### Option B — 缩小或重做 CRM 设计

- 暂停当前 batch，由 owner 指定要后置/删除的 CRM→reminder、summary 或 projection 能力；
- 形成新的范围/接口基线后再决定是否需要例外审查，不沿用当前候选直接通过。

#### Option C — 修订并显式授权一次例外独立复审

- 先关闭 1 blocking/4 important，再额外启动 Round 7；
- 这是最严格也最耗时的选择，构成 review-round cap 的明确例外，不会被 Option A 自动推导。

本次已选择 Option A；Option B/C 保留为历史备选，不再构成 pending decision。当前不允许原样接受 blocking 并进入实现。

#### Non-Automatic Actions

任何选择都不自动批准全部 child design、implementation、QA/acceptance、Goal execution、AI/provider、凭证、Git、PR、merge、publish、release、deploy、promotion 或 production cutover。`stage-1-evidence-go` 与 `stage-2-evidence-go` 继续独立保持 `pending`。

## Completed Checkpoint: `crm-local-revision-final-acceptance`

### `crm-local-revision-final-acceptance`

Owner 已选择 Option A；当前为 `approved`，confirmation id=`ff30ebbe-5282-4e87-be21-7bca4669b166`。这是 CRM Round 6 findings 修订后的最终 local-only 验收，不是新的独立review轮次，也不产生Round 7。

当前冻结候选：

- CRM design SHA-256：`f9feb4a87fe912ebe67f5d4bd88d3452941379389efe4d8120c3cb1e23a33b7c`；
- CRM checklist SHA-256：`2c15c5b5739a3e2d04964d989f10649ef5cf339e374925411790ce70f6d4ed4c`；
- Reminder sibling design SHA-256：`82d0c11636416c45d6a525498071fe3af60d80955633cb9307096e07a53cde1d`；
- Reminder sibling checklist SHA-256：`282367b0f8048c46f013e8f25ead6711991bb25524474866cb30b402ad0065fe`。

#### Option A — 确认 local-only closure（已选）

- 接受上述四份冻结候选已关闭 CRM Round 6 的 1 blocking/4 important；
- 把CRM design-review从`needs-owner-approval`更新为`passed`，仍保持Round 6并明确review mode为owner-approved local-only；
- 交回`cs-epic`继续剩余child design batch，不进入implementation。

#### Option B — 要求继续修改

- 若当时选择，`crm-local-revision-final-acceptance`将保持pending；
- owner指出需要调整的revision、状态转换、summary、reminder seam或prototype契约；
- 修订后仍走本地closure，除非owner另外显式授权review-round-cap例外。

#### Recommendation

推荐 Option A，owner 已采纳。当前本地 Evidence Confidence Ledger 已无unresolved blocking/important；revision/CAS、direct-link/deleted-unlink、summary exact values、closed fact union与tracked v2都有对应check/evidence落点。

#### Non-Automatic Actions

无论选择A或B，都不会自动批准全部child designs、进入implementation/QA/acceptance、生成或派发Goal execution、批准stage-1/2 evidence、调用AI/provider、使用凭证、执行Git写操作、创建PR、merge、publish、release或deploy。

## Completed Checkpoint: `all-feature-designs`

### `all-feature-designs`

当前为 `approved`。Owner 已选择 Option A；confirmation id 为 `04922f34-3db7-470b-b8eb-b685f2bad8e8`。八条 child 已按 DAG 全部完成 design/checklist/design-review，八份 design 已统一从 `draft` 同步为 `approved`，execution lane 仍统一为 `goal`。本次 checkpoint 精确绑定 `creative-shoot-planning-child-design-batch-v1`：对 8 个 feature 目录中的 design/checklist/design-review 共 24 份文件按路径升序组成 `SHA-256␠␠path\n` manifest，其 SHA-256 为 `72016767e2ffbfa9fb059778d0bd844619e90a9b483fc635431c6162a1ddf008`；批准前复算一致。

确认范围：

- 统一批准8份design作为后续Goal package的实现/QA/acceptance契约；
- 允许`cs-epic`把八份design从`draft`同步为`approved`并进入Goal package阶段；
- 接受design-review中已列出的residual risks由implementation/code review/QA/acceptance继续关闭。

不在本确认范围：

- 不批准`stage-1-evidence-go`或`stage-2-evidence-go`；它们继续独立pending并阻止对应implementation dispatch；
- 不授权Goal execution、implementation、commit/push、PR、merge、AI/provider、凭证、publish、release、deploy、promotion或production cutover；
- 不授权hardening的production-shaped rehearsal环境或任何远程/生产动作。

Option A（已选）：确认当前绑定的8份design batch；下一步只持久化design approval并生成Goal package，随后仍需独立的Goal execution authorization。

Option B：指出要修改的具体child/契约；保持全部design draft，不生成Goal package。

Option C：拒绝当前batch；记录rejected并停止本Epic。

Option B/C 保留为历史备选，不再构成 pending decision。

## Decisions Still Needed

### `stage-1-evidence-go`

未来 goal-execution gate，当前为 `pending`。所有 8 条 child design 可以先按标准 batch 完成；只有 shoot-plan-core、planning-reference-assets、plan-ingestion-capture implementation/acceptance 完成并积累 5 个合格真实样本，owner 查看 G1/G2 evidence 后才决定。approved 才允许 goal driver 派发 `shoot-plan-crm-integration` implementation。

批准时必须用一次原子更新同时写 named decision、Decision History 和 `approval_evidence.stage-1-evidence-go` 的 canonical path/SHA-256/gate version；只有状态与 hash binding 同时有效才可放行。

### `stage-2-evidence-go`

未来 goal-execution gate，当前为 `pending`。只有 full 分享/认领/提醒 implementation/acceptance 完成并积累 5 个 eligible shared shoot，owner 查看互动率与准备缺失率 evidence 后才决定。approved 才允许 goal driver 派发 `plan-business-feedback` implementation。

批准时同样原子绑定 stage-2 evidence path/SHA-256/gate version；更新 evidence 内容会使旧批准失效，必须重新确认。

### `goal-acceptance`

当前为 `approved`，与 `goal-commits` 由同一次 owner 回答原子批准，confirmation id=`0cfee048-fd76-491a-925c-caa8bf9eb434`。它只允许 Goal driver 在单个 feature 的 implementation、独立 code review、QA、DoD/gates 和真实核心证据全部通过后，用 `ResumeGoalAcceptance approval-report.md#goal-acceptance` 写 acceptance、把 checklist checks 更新为 passed，并回写对应 roadmap/requirement；不允许跳过 stage-1/stage-2 evidence gate、production-shaped rehearsal、独立 review、QA 或核心运行路径。

### `goal-commits`

当前为 `approved`，与 `goal-acceptance` 绑定同一 confirmation id=`0cfee048-fd76-491a-925c-caa8bf9eb434`。它只允许每个 feature 在 accepted、items/goal-state 已同步且授权仍可机械验证时创建一次 scoped commit；范围可包含当前 feature 代码、spec、evidence、review、QA、acceptance，以及必要的 shared roadmap/goal-state/requirements/docs 回写。首条 scoped commit 还可能纳入迁移到执行 workspace 的本 Epic 已批准但尚未提交的规划/design/prototype baseline；必须先做 scope attribution，不得包含任务外 owner 文件，不得使用 `--no-verify`，也不授权 push。

`goal-acceptance` 与 `goal-commits` 必须由同一次 owner 回答原子批准，并绑定同一非空 confirmation ID；不能只批准一项，也不能用 roadmap/design 的旧 A 代替。

## Completed Checkpoint: `goal-execution`

Owner 已选择 Option A；`approval_groups.goal-execution`、`goal-acceptance` 与 `goal-commits` 已由同一次回答原子批准，confirmation id=`0cfee048-fd76-491a-925c-caa8bf9eb434`。执行 workspace 获准按 GitFlow 从 `develop` 建立独立 worktree，并只迁移本 Epic 可归因的已批准规格；任何冲突、任务外 diff 或无法证明归属的文件都必须 handoff。

Goal package 已生成并通过结构/self-test，准备的 literal command 是：

```text
/goal "执行 CodeStable roadmap 目录 .codestable/roadmap/creative-shoot-planning 下的 goal 执行包。先读取 goal-protocol.md、goal-protocol-feature-loop.md、goal-protocol-gates.md、goal-protocol-audit.md、goal-state.yaml、goal-plan.md；这是已由用户确认 roadmap 和全部 feature design，并在同一次 Goal 启动确认中授权 Goal acceptance 与每个 feature 自动 scoped-commit 的模式，两项 ApprovalRef 仍须分别机械核验。按 goal-state.yaml 的 features 顺序循环：进入 cs-feat implementation、cs-code-review、cs-feat QA；review/QA 失败按协议修复重跑，awaiting/needs-human/blocked 分别等待、请求输入或 handoff。QA passed 后只用 goal-acceptance ApprovalRef 调用 ResumeGoalAcceptance；accept 后先持久化 accepted 状态与新 index，再机械核验 goal-commits ApprovalRef，只有有效时才 scoped-commit 本 feature 的全部状态更新，缺失、不匹配或 rejected 必须 handoff 且不得提交。每个 feature 完成打印 CS_ROADMAP_GOAL_FEATURE_DONE；全部完成后做最终 roadmap 审计。只有出现 CS_ROADMAP_GOAL_COMPLETE，且所有 feature review/QA/acceptance、授权提交和最终审计均通过、没有 CS_ROADMAP_GOAL_HANDOFF，本 goal 才算完成。"
```

### Option A — 授权 Goal execution 并使用独立 worktree（推荐）

一次性批准 `goal-acceptance` 与 `goal-commits`，生成唯一 confirmation ID；同时授权执行前按项目 GitFlow 从 `develop` 建立 `feat/creative-shoot-planning` 独立 worktree，并以可审计方式迁移本 Epic 已批准规格。由于当前 `main` 的 `64e1105` 含规划基线但尚未进入 `develop`，workspace 准备若需要复用该已有 commit 或迁移未提交 Epic 文件，必须只处理已列入本 Epic 的路径并在执行前展示核验结果；任何冲突或任务外 diff 立即 handoff。准备完成后才可派发 literal `/goal`。

### Option B — 只批准执行，不授权 workspace 变更

原子批准两项 Goal decision，但保持 `goal-state.workspace.mode=pending`，不派发 driver；owner 另行把仓库切到符合 GitFlow、包含完整 approved baseline 的执行 workspace 后再恢复。该选项更保守，但会增加一次 workspace checkpoint。

### Option C — 拒绝 Goal execution

把 `approval_groups.goal-execution`、`goal-acceptance` 与 `goal-commits` 原子记录为 rejected，Goal state 进入 handoff，不启动实现或自动 commit。

#### Acceptance / commit 影响范围

- Acceptance：最多 8 次 feature acceptance；每次仍需 implementation、独立 review、QA、DoD/gates 和真实核心证据通过。
- Scoped commits：最多每条 accepted feature 一次；包含该 feature 代码/规格/证据/报告及必要 shared 状态回写。当前未提交的 Epic baseline 只有经 workspace scope attribution 后才可进入首条相关 commit。
- Stage checkpoints：前三条 accepted 后必停在 `stage-1-evidence-go`；协作/提醒 accepted 后必停在 `stage-2-evidence-go`。真实试点部署、采样和 owner evidence disposition 不由本授权自动完成。

#### Non-Automatic Actions

无论选择哪项，都不会自动执行 remote push、PR、merge、publish、release、deploy、promotion、production migration/restore/cutover、试点曝光、真实样本采集、production-shaped rehearsal、AI/provider 调用、知识库建设或后置 `creative-shoot-intelligence`。这些动作仍需各自独立的 owner authorization。

## Why Now

原规划把首版和 AI/知识能力放在同一 16 节点 DAG，人工 G4 无法阻止后置节点提前 ready，且唯一 hardening/生产媒体恢复被拖到 AI 末尾。拆分后本 roadmap 必须作为独立首版重新 review 和批准，不能沿用旧报告。

## Context

当前首版路线包含：

1. `shoot-plan-core`；
2. `planning-reference-assets`；
3. `plan-ingestion-capture`，唯一 minimal loop；
4. `shoot-plan-crm-integration`；
5. `plan-share-collaboration`；
6. `plan-assignment-reminders`；
7. `plan-business-feedback`；
8. `creative-planning-v1-hardening`。

硬约束：策划非强制；客户零价格信号；首版无 AI/知识；提醒只给摄影师；原作素材 display-only；planning media 在首版进入生产恢复。

## Options

### Option A — 批准当前首版 roadmap（已选）

- 将 `roadmap-plan` 原子更新为 approved 并记录 confirmation id；
- roadmap `draft→active`；
- 按标准 `cs-epic` 完成全部 8 条 child design/design-review 和统一设计确认；
- 生成 goal package 时固化两个 pre-implementation evidence gate；`stage-1-evidence-go`、`stage-2-evidence-go` 继续 pending，不能被 roadmap 或全量 design 批准替代。

### Option B — 要求继续修改

- `roadmap-plan` 保持 pending；
- 指定需调整的范围、契约或顺序；
- 修改后重新独立 review。

### Option C — 拒绝首版路线

- 将 `roadmap-plan` 标为 rejected；
- roadmap 保持 draft/archived disposition；
- 不创建 child feature。

## Recommendation

推荐 Option A，owner 已采纳。8 条路线已把最小闭环、匿名公网面、提醒收件人、经营草稿和生产恢复放入明确 owner item，并用两个未来 named approval 保存真实证据停顿。

## Risks And Tradeoffs

- 首版不做离线可写；外景断网可能要求回 planning update。
- proposal/full 和匿名媒体使系统首次拥有未认证公网读写面，安全证据是发布阻断项。
- `stage-1-evidence-go` 与 `stage-2-evidence-go` 会让路线等待真实拍摄，不允许用 fixture 冒充市场证据。
- 客户不会收到自动提醒；首版价值是提醒摄影师核对，不是外部消息平台。
- 订单价格调整引入 additive audit contract，但仍由摄影师显式确认，不自动定价。

## Non-Automatic Actions

批准 roadmap 不自动：

- 批准未来 G1/G2 或 G3；
- 批准或启动 `creative-shoot-intelligence`；
- 创建 requirement/ADR；
- 实现、调用外部服务、使用凭证；
- commit、push、merge 或 deploy；
- 接受 child design/QA 中出现的新 residual risk。

## After You Answer

- Option A 已执行：持久化 `roadmap-plan` approval、激活 roadmap、重跑 workflow/DAG；下一步先完成 requirement gate，再进入连续 child design batch。
- Goal 阶段：全部 child design 统一确认后才生成执行包；driver 完成前三条后在 CRM implementation 前停于 stage-1 gate，完成协作/提醒后在 business implementation 前停于 stage-2 gate。
- Option B：保持 draft，按反馈做 planning update 和独立复审。
- Option C：记录 rejected 与原因，停止本路线。

## Completed Checkpoint: `creative-shoot-planning-requirement`

### Draft Ref

`creative-shoot-planning-v1-2026-08-05-r1`

### 确认结果

Owner 已确认本 draft ref。Roadmap §10.3 的 requirement gate 已闭合；草案已原样写入 `.codestable/requirements/creative-shoot-planning.md`，`VISION.md` 与 roadmap requirement link 已同步。以下内容保留为 durable approval history。

### Requirement 草案

```markdown
---
doc_type: requirement
slug: creative-shoot-planning
pitch: 把散落在聊天、参考图和脑海里的创作灵感，变成拍前能协作、现场能照着执行的拍摄方案
status: draft
last_reviewed: 2026-08-05
implemented_by: []
tags: [shoot-planning, creative-workflow, cosplay, collaboration, run-mode]
---

# 创作型拍摄策划

## 用户故事

- 作为正在筹备 cosplay 等复杂创作拍摄的摄影师，我希望把聊天记录、参考图和零散想法快速整理成场地、妆造、道具、灯光与镜头清单，而不是面对一张空白大表格重新录一遍。
- 作为需要和客户一起理解角色、确认画面与分配准备工作的摄影师，我希望在售前分享创作方向、成单后再开放完整方案与逐项协作，而不是让客户注册账号，或在协作时意外看到价格和成本信息。
- 作为在外景现场一边控光、一边引导动作的摄影师，我希望用手机按镜头勾选完成、记录跳过并纠正误记，随时知道还有哪些画面没拍，而不是在复杂编辑页面和聊天记录之间来回切换。
- 作为需要判断一场复杂拍摄该留多少时间、是否要调整报价的摄影师，我希望策划里的明确事实能形成只给自己看的参考草案，由我确认后再影响订单或档期，而不是靠脑子重算，或让系统替我自动定价和排期。

## 为什么需要

高要求的 cosplay 和创作型拍摄，真正决定成片的准备往往发生在定档之后、开拍之前：角色理解、场地、妆造、道具、动作、灯光和分镜散落在聊天窗口、收藏夹与脑海里。现有 CRM 能记客户、订单和时间，却不能把这些灵感变成一份能共同确认、现场执行和事后复盘的方案；遗漏通常直到现场才暴露，带来返工、浪费档期和创作妥协。

## 怎么解决

提供一套按需使用的拍摄策划工作区：先接住已有讨论和参考素材，再逐步整理创作意图、准备事项与镜头清单；客户通过免登录的专属链接参与合适范围的反馈和分工；拍摄现场切换到简洁的手机执行模式逐镜完成；需要时，摄影师还能从策划复杂度得到私有的报价与排期参考。简单拍摄可以只写几条，复杂拍摄再逐步展开；没有策划案时，原有客户、订单、档期和提醒流程完全照常工作。

## 边界

- 策划是非强制的创作辅助能力，不是订单流程的必填节点；系统不因缺少策划案而阻塞、警告或降低既有 CRM 能力，使用指标也不变成摄影师侧的完成度考核。
- 首版不包含 AI 脚本、AI 分镜、模型调用或知识库维护。冷门作品与角色的知识支持属于独立后置能力，必须在首版真实使用证据成立后另行启动。
- 客户参与采用免登录的专属链接：售前只分享创作方向和规模概览，符合成单条件后才开放完整镜头、逐项反馈与分工；客户侧始终不显示价格、成本、工时或经营草稿。
- 首版不会自动给客户发送 Telegram、短信或邮件；客户认领的拍前事项只提醒摄影师核对。未来若要直达客户，需另行解决身份绑定、同意、退订和投递状态。
- 现场执行首版要求联网；断网时明确提示失败或待重试，不把尚未保存的勾选伪装成同步成功。
- 原作图片、设定集截图和外部参考链接只用于策划展示与沟通，不自动抓取，不进入生成用途；摄影师自有或明确授权的素材才可用于未来生成能力。
- 复杂度只形成摄影师私有的可解释参考，不会自动修改订单价格、占用档期或替摄影师与客户议价；任何经营变更都由摄影师显式确认。
- 首版不建立客户账号，不做实时多人编辑、评论审批流、在线选片或跨部署媒体归档。
```

### 自查结论

- 单一能力：只描述不依赖 AI/知识库也能成立的创作型拍摄策划首版；AI 与知识支持留在后置 epic，没有混入第二份 requirement。
- 用户故事：四条分别来自已确认的摄取建案、客户协作、现场执行与经营回流场景，没有编造无来源角色。
- 人话与颗粒度：删去了聚合名、API、revision、token、outbox、证据阈值和实现 DAG；保留的 proposal/full 含义已改写为“售前方向/成单后完整方案”的用户可理解边界。
- 边界：明确非强制、零价格信号、首版无 AI、客户不自动触达、联网执行、素材用途、人工确认经营变更与不做客户账号。
- Pitch：一句话覆盖“零散灵感 → 可协作 → 可执行”，不承诺尚未进入首版的 AI 生成。

### Options

- Option A（已选）：确认 draft ref `creative-shoot-planning-v1-2026-08-05-r1`；已写入 canonical requirement、更新 `VISION.md` 与 roadmap requirement link，下一步进入 8-child 连续 design/design-review batch。
- Option B：提出具体修改；保持 requirement gate pending，修订后生成新的 draft ref 再确认。
- Option C：拒绝建立该 requirement；child design batch 保持阻塞，等待重新规划或显式修改 roadmap §10.3。

### Non-Automatic Actions

确认该草案只授权落盘这一份 `status: draft` requirement、更新需求索引/roadmap 引用并进入 child design batch；不会授权实现、Goal execution、G1–G3 evidence go、AI/provider 调用、commit、push、PR、merge、publish、release、deploy、promotion 或 production cutover。
