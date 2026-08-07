# Goal Feature Loop

```haskell
data FeatureStage = Implementation | Review | QA | Acceptance
data FailureCause = ImplementationDefect | StageEvidenceDefect
data StageResult
  = Passed | Failed FailureCause | Awaiting ExternalRef
  | NeedsHuman Reason | Blocked Reason
data FeatureStep
  = Run FeatureStage | Remediate FeatureStage FeatureStage
  | WaitFor FeatureStage ExternalRef | RequestHuman FeatureStage Reason
  | HandoffStage FeatureStage Reason | Accepted

implementationReady :: RoadmapItems -> RoadmapItem -> Bool
implementationReady items item = all ((== Done) . statusIn items) (dependsOn item)

commitAuthorized :: GoalState -> Bool
commitAuthorized s = approvalArtifactApproved s (commitAuthorizationRef s) "goal-commits"

advance :: FeatureStage -> StageResult -> FeatureStep
advance Implementation Passed = Run Review
advance Review Passed         = Run QA
advance QA Passed             = Run Acceptance
advance Acceptance Passed     = Accepted
advance Implementation (Failed _)                       = Remediate Implementation Implementation
advance Review (Failed _)                               = Remediate Implementation Review
advance QA (Failed _)                                   = Remediate Implementation Review
advance Acceptance (Failed ImplementationDefect)        = Remediate Implementation Review
advance Acceptance (Failed StageEvidenceDefect)         = Remediate Acceptance Acceptance
advance stage (Awaiting ref)                            = WaitFor stage ref
advance stage (NeedsHuman reason)                       = RequestHuman stage reason
advance stage (Blocked reason)                          = HandoffStage stage reason
```

## 1. 进入 Feature

读取：

- `goal-features/<feature-slug>.md`
- feature design
- feature checklist
- roadmap item
- 当前代码上下文

进入实现前运行 `codestable-workflow-next.py feature --feature <feature-dir> --require-implementation-ready --json`，从 items.yaml 机械核验当前 item 的全部 `depends_on` 都严格为 `done`。`dropped` 或仅 design-review `passed` 只曾满足 child batch 的 design admission，不满足本阶段；gate 非 `ok` 时持久化 handoff 并停止，不把当前 feature 改为 `implementing`。

### 1.1 创作策划真实证据派发门

在通用 implementation-ready 检查之后、把 feature 标为 `implementing` 之前，按下表执行 roadmap 自带 runner：

| 目标 feature | Named decision | 命令 |
|---|---|---|
| `shoot-plan-crm-integration` | `stage-1-evidence-go` | `python3 .codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py --roadmap .codestable/roadmap/creative-shoot-planning --feature shoot-plan-crm-integration --decision stage-1-evidence-go --json` |
| `plan-business-feedback` | `stage-2-evidence-go` | `python3 .codestable/roadmap/creative-shoot-planning/goal-tools/planning-evidence-dispatch-gate.py --roadmap .codestable/roadmap/creative-shoot-planning --feature plan-business-feedback --decision stage-2-evidence-go --json` |

- exit `0` / `status=passed` 才可继续；
- exit `2` / `needs-human`、exit `3` / `failed`、exit `4` / `blocked` 均保持 `current_feature_index` 不变，将 `goal-state.yaml` 写成 `handoff`，记录 runner JSON 中的 reason/next 后停止；
- 不允许用 roadmap/design/Goal execution批准、fixed fixture、缺失 runner、手工改 state 或空 evidence 替代 named decision + path/SHA-256/gate-version 绑定；
- typed resume 必须先更新 canonical `approval-report.md`，再把 handoff 恢复为 `ready-to-dispatch`、清空 reason/next并重跑同一命令。

打印：

```text
CS_ROADMAP_GOAL_FEATURE_START
Feature: <N>/<总数> <feature-slug>
Design: <路径>
Checklist: <路径>
Depends on: <依赖|none>
Mandatory commands: <命令列表>
Evidence required: <证据列表>
```

只有 `implementationReady` 和适用的真实证据派发门均通过时，才把 `goal-state.yaml` 当前 feature 状态改为 `implementing`。

## 2. 实现阶段

必须显式进入 `cs-feat` implementation 阶段执行；禁止仅凭本协议摘要替代主入口协议。开始写代码前先打印：

```text
CS_STAGE_START feature=<feature-slug> stage=implementation skill=cs-feat
```

如果不能加载 `cs-feat` 主入口，必须停下说明原因，不得降级为普通实现。

按 `cs-feat` implementation 阶段执行：

- 先做基线预检。
- 按 checklist steps 顺序实现。
- 每步完成后只把该 step 的 `status` 从 `pending` 改为 `done`。
- 不修改 `checks`；checks 只由 acceptance 更新。
- 每步留下命令、手工、API、浏览器或 diff 证据。
- 每步执行清洁度检查。

实现结束后运行 `implementation.before_review` gates。

## 3. Code Review 阶段

按 `cs-code-review` 执行：

- 读取 design、checklist、evidence pack、gate results、git diff。
- 只读审查，不直接修代码。
- 写 `{feature-slug}-review.md`。
- review 必须解释 gate warnings 和 provider warnings。
- review 的 Test And QA Focus 交给 QA。

`advance Review` 返回 `Remediate Implementation Review` 时打印 `CS_ROADMAP_GOAL_REVIEW_FIX`。

## 4. QA 阶段

按 `cs-feat` QA 阶段执行：

- 读取 design、checklist、review、evidence pack、gate results、git diff。
- 只读运行验证，不直接修代码。
- 写 `{feature-slug}-qa.md`。
- 覆盖 design 关键场景、DoD commands、review QA focus、evidence pack residual risks。
- 功能性核心路径必须有实际运行证据。
- 非功能性 feature 必须写明替代证据理由。

`advance QA (Failed _)` 返回 `Remediate Implementation Review` 时打印 `CS_ROADMAP_GOAL_QA_FIX`；`Awaiting` 只等待已启动工作，`NeedsHuman` 请求缺失输入，`Blocked` 直接持久化 handoff；三者都不进入 review-fix 循环。`WaitFor` 保留 external ref；`RequestHuman` / `HandoffStage` 退出前写 `goal-state.yaml` 的 `status: handoff`、reason 与 next。

## 5. Acceptance 阶段

按 `cs-feat` acceptance 阶段执行：

- 从 goal-state 读取 `acceptance_authorization_ref`，以 `ResumeGoalAcceptance ApprovalRef` 显式进入；缺失、不匹配或 rejected 时持久化 handoff，不把 Goal driver 运行当作 owner 批准。
- 确认 review passed 且无 unresolved blocking。
- 确认 QA passed 且无 unresolved failed / blocked。
- 复核 evidence pack、DoD Results、Gate Results。
- 填 `{feature-slug}-acceptance.md`。
- 把 checklist checks 从 `pending` 改为 `passed`。
- 按 design 第 4 节处理 reference / architecture / requirement 回写。
- 按 roadmap / roadmap_item 回写 items.yaml 和 roadmap 主文档。

Acceptance 失败必须先归因：实现行为不满足验收是 `ImplementationDefect`，回 implementation 修复后重跑 review / QA / acceptance；仅报告、checklist 或证据落盘缺口是 `StageEvidenceDefect`，只修 acceptance 后重验。

## 6. Feature 完成

打印 `CS_ROADMAP_GOAL_FEATURE_VERIFY`，列出 Implementation / Review / QA / Acceptance / Commands / Deliverables / Cleanliness / Roadmap item。

全部通过后：

- 当前 feature 状态改为 `accepted`。
- `current_feature_index` 加 1，并把 accepted 状态、roadmap/items 回写和新 index 一起持久化；这些状态更新必须进入本 feature 的 scoped commit。
- 再次运行 `python3 <cs-onboard skill 目录>/tools/codestable-workflow-next.py epic --roadmap <roadmap-dir> --json`；只有结果为 `awaiting` / `dispatch_goal` 且 evidence 同时返回 `approval-report.md#goal-acceptance` 与 `approval-report.md#goal-commits`，`commitAuthorized` 才成立。其他结果先把 feature 保持 `accepted`、持久化 handoff，并停止，不得执行 `git commit`。
- 授权仍有效时，scoped-commit 本 feature 的代码、spec、review、QA、acceptance、roadmap 回写和全部 goal-state 更新；commit 成功后再运行 `git status --short`，只有所有状态更新之后工作树仍干净，才能进入下一条。
- 打印 `CS_ROADMAP_GOAL_FEATURE_DONE`。
