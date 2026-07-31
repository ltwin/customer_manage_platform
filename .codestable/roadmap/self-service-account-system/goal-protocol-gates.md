# Goal Gate Policy

## 1. 通用 Gate Result

```haskell
data GateKind = Executable | ProtocolOnly
data GateStatus = Passed | Failed | NeedsHuman | Awaiting | Blocked | Skipped
data Gate = Design | Implementation | Review | QA | Acceptance | RoadmapAudit
data Recovery = ReviseDesign | FixImplementation | FixReview | FixQAThenReview | FixAcceptance | FixAudit

recover :: Gate -> Recovery
recover Design         = ReviseDesign
recover Implementation = FixImplementation
recover Review         = FixReview
recover QA             = FixQAThenReview
recover Acceptance     = FixAcceptance
recover RoadmapAudit   = FixAudit

maySkip :: Bool -> Maybe Reason -> Bool
maySkip core reason = not core && isJust reason
```

每个gate输出：

```json
{
  "gate_id": "gate-name",
  "feature": "YYYY-MM-DD-feature-slug",
  "inputs": {"design|checklist|feature_dir|out": "canonical repo-relative path"},
  "input_digests": {"design|checklist|out": "sha256"},
  "stage": "stage.name",
  "kind": "executable|protocol-only",
  "status": "passed|failed|needs-human|awaiting|blocked|skipped",
  "blocking": [],
  "warnings": [],
  "evidence": [],
  "providers": {}
}
```

`Failed`是可修复blocking；`NeedsHuman`缺owner输入/能力；`Awaiting`表示外部工作已启动；`Blocked`是终止态；`Skipped`受`maySkip`约束。`protocol-only`由对应stage执行，不是缺失脚本。旧result缺`kind`只能归一为`executable`。

## 2. feature_design.before_approve

必须有design-review passed、checklist YAML可解析、Acceptance Coverage Matrix与DoD Contract。失败回design；Goal不接管未approved design。

## 3. implementation.before_review

必须运行当前`cs-onboard` skill包的：

- `scope-gate`
- `dod-runner`
- `evidence-pack`

缺脚本说明CodeStable安装不完整，应更新/重装，不能当passed。

检查steps全done、diff无未解释范围外文件、清洁度通过、core命令有证据、evidence pack包含Scope/DoD/Validation/Cleanliness/Residual Risks。失败回implementation；缺evidence先补证据。

## 4. review.before_pass

必须运行`review-evidence-gate`。Review需基于当前diff、status passed、由独立Task agent完成、无unresolved blocking并消费evidence/gates。`reviewer: ocr|self`只能作为owner显式降级fallback。高风险provider warning需解释或交QA。

失败回review/implementation；独立reviewer不可用则handoff或重新启动，不静默降级。

## 5. qa.before_acceptance

必须运行`qa-evidence-gate`。QA需status passed，覆盖design关键场景、DoD、review focus、evidence residual risks；core path有实际运行证据，不把核心缺口写成residual。高风险feature建议独立QA Task agent，主流程核验后写正式报告。

失败回QA/implementation。

## 6. acceptance.before_done

必须运行`acceptance-dod-gate`。检查acceptance passed、checks全passed、blocking DoD有pass evidence、roadmap item回写、residual不含core gap。StageEvidenceDefect只修acceptance；ImplementationDefect回implementation后重跑review/QA/acceptance。

## 7. roadmap_audit.before_complete

必须运行：

- `goal-consistency-gate`
- `goal-audit-gate`

检查：

- Goal-state全部features accepted，两份authorization机械有效；items均done或有理由dropped。
- 每个非dropped item与feature一一对应，feature pointer/canonical identity匹配。
- Feature dir、design、checklist、review、QA、acceptance路径与frontmatter归属正确。
- 四份executable gate JSON只接受passed，gate/feature/inputs/input SHA/stage identity匹配。
- Approved design、review、QA、acceptance、evidence pack、gate/DoD results存在且passed；steps done、checks passed。
- Final aggregate commands已重跑或有非核心trust-prior；provider warnings已解释。
- Protected local stash已恢复且checksum匹配，任务外文件未stage/commit；goal-audit.md status passed。

失败回audit；同项三轮仍失败则handoff。

## 8. Provider Policy

- Provider unavailable不阻塞基础流程。
- Provider warning必须由review/QA/audit解释；未解释的核心风险可阻塞。
- archguard/meta-cc不可用记录fallback；meta-cc首批只读已有摘要。
