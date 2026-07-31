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
advance Acceptance (Failed StageEvidenceDefect)        = Remediate Acceptance Acceptance
advance stage (Awaiting ref)                            = WaitFor stage ref
advance stage (NeedsHuman reason)                       = RequestHuman stage reason
advance stage (Blocked reason)                          = HandoffStage stage reason
```

## 1. 进入 Feature

读取 goal-feature、feature design/checklist、roadmap item和当前代码上下文。

进入实现前运行：

```bash
python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-workflow-next.py feature --feature <feature-dir> --require-implementation-ready --json
```

从items.yaml机械核验当前item的全部`depends_on`严格为`done`。`dropped`或仅design-review passed不满足；gate非ok时持久化handoff并停止。

首次feature前还必须执行goal-plan workspace preflight：确认branch为`feature/userCenter`，核对三条protected path checksum，创建path-scoped local stash并把ref写回goal-state。失败时不得写代码。

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

只有implementationReady为真时，才把当前feature状态改为`implementing`。

## 2. 实现阶段

必须显式进入`cs-feat` implementation阶段，开始前打印：

```text
CS_STAGE_START feature=<feature-slug> stage=implementation skill=cs-feat
```

如果不能加载`cs-feat`主入口，必须停下，不得降级为普通实现。

- 先做基线预检。
- 按checklist steps顺序实现；每步完成只把该step从pending改done。
- 不修改checks；checks只由acceptance更新。
- 每步留下命令、手工、API、浏览器或diff证据并执行清洁度检查。
- 实现结束运行`implementation.before_review` gates。

## 3. Code Review 阶段

按`cs-code-review`执行：读取design/checklist/evidence/gates/git diff，只读审查，写`<feature-slug>-review.md`。Review必须解释gate/provider warnings并把Test And QA Focus交给QA。

Review失败返回implementation/review-fix，打印`CS_ROADMAP_GOAL_REVIEW_FIX`，修复后重新独立审查。

## 4. QA 阶段

按`cs-feat` QA阶段执行：只读验证，写`<feature-slug>-qa.md`，覆盖design场景、DoD、review focus和residual risks。功能性core path必须真实运行；mixed/non-functional部分需给替代证据理由。

QA失败回implementation后重跑review/QA/acceptance并打印`CS_ROADMAP_GOAL_QA_FIX`。Awaiting只等待已启动工作；NeedsHuman请求输入；Blocked直接handoff。退出前写goal-state handoff reason/next并恢复protected stash。

## 5. Acceptance 阶段

按`cs-feat` acceptance阶段执行：

- 从goal-state读取`acceptance_authorization_ref`，只以`ResumeGoalAcceptance ApprovalRef`进入；缺失/不匹配/rejected必须handoff。
- 确认review/QA passed且无核心缺口。
- 复核evidence pack、DoD Results、Gate Results。
- 写`<feature-slug>-acceptance.md`，把checks从pending改passed。
- 按design处理reference/architecture/requirement写回，并回写items/roadmap。

Acceptance失败先归因：实现行为不满足为ImplementationDefect，回implementation并重跑review/QA/acceptance；仅报告/checklist/evidence缺口为StageEvidenceDefect，只修acceptance后重验。

## 6. Feature 完成

打印`CS_ROADMAP_GOAL_FEATURE_VERIFY`，列出Implementation/Review/QA/Acceptance/Commands/Deliverables/Cleanliness/Roadmap item。

全部通过后：

- 当前feature状态改为`accepted`；`current_feature_index`加1，和items/roadmap写回一起持久化。
- 重跑epic workflow hook；只有`awaiting`/`dispatch_goal`且两份ref同时有效，`commitAuthorized`才成立。
- 授权有效时scoped-commit当前feature代码/spec/evidence/review/QA/acceptance、roadmap与goal-state更新；禁止包含protected paths、凭证或任务外文件，禁止push。
- Commit后检查工作树；related state必须干净，protected paths继续在local stash中。只有满足goal-plan cleanliness才进入下一条。
- 打印`CS_ROADMAP_GOAL_FEATURE_DONE`。

全部feature accepted后进入final audit；audit通过后恢复protected stash并核对checksum。若恢复失败或冲突，先handoff，不得宣告Goal完成。
