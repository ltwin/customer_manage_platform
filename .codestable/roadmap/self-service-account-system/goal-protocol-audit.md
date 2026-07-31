# Goal Final Audit Protocol

## 1. 启动

所有feature accepted后打印：

```text
CS_ROADMAP_GOAL_AUDIT_START
Roadmap: self-service-account-system
Features to verify: 2
Commands to re-run: <goal-plan去重命令列表>
```

读取roadmap、items、goal-plan/state、approval-report两份授权、goal-features、非dropped item与accepted feature双射，以及每个feature canonical design/checklist/review/QA/acceptance/evidence/gate results。

## 2. 核验

先运行：

```bash
python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/self-service-account-system
```

失败不得打印完成标记；按blocking补证据或回退状态。

```haskell
data AuditOutcome = AuditComplete | RepairAudit | AuditHandoff GoalHandoffReason

auditPassed :: AuditEvidence -> Bool
auditPassed a = and
  [ allItemsTerminal a
  , roadmapFeatureBijection a
  , canonicalFeatureEvidence a
  , goalAuthorizationsValid a
  , allFeatureArtifactsPassed a
  , allChecklistPassed a
  , noCoreResidualRisk a
  , providerRisksExplained a
  , noUnapprovedHOnlyCoreCheck a
  , writebacksCompleteOrNA a
  , consistencyGatePassed a
  , protectedWorkspaceRestored a
  ]

auditOutcome :: AuditEvidence -> AuditOutcome
auditOutcome a
  | auditPassed a                        = AuditComplete
  | not (noUnapprovedHOnlyCoreCheck a)   = AuditHandoff UnapprovedHOnlyCoreCheck
  | coreEnvironmentMissing a             = AuditHandoff CoreEvidenceUnavailable
  | corePathUnverified a                 = AuditHandoff CorePathUnverified
  | sameAuditFailureCount a >= 3         = AuditHandoff RepeatedFailure
  | otherwise                            = RepairAudit
```

## 3. 最终聚合命令

按goal-plan执行final aggregate commands。功能性core命令不能因耗时跳过。外部网络、provider凭证、browser或Docker不可用时，若属于core path则handoff。

必须重新核验：新账号完整主链、legacy/bootstrap、password/all-family revoke、limiter/timing、monitor/preflight、root/rollback catalog和two-account isolation。

## 4. 工作区与清洁度

检查tracked/staged/unstaged/untracked、debug、TODO/FIXME/XXX、注释代码、同名shim、临时runner/download、`__pycache__`及runtime residual。

在最终verdict前恢复goal-state记录的protected local stash并逐path核对goal-plan checksum；冲突、缺失或内容改变必须handoff。Protected user文件恢复后可作为已解释pre-existing dirt保留，但不得出现在任何Goal commit。其余未解释命中阻塞完成。

## 5. 审计报告

写`.codestable/roadmap/self-service-account-system/goal-audit.md`：

```markdown
---
doc_type: roadmap-goal-audit
roadmap: self-service-account-system
status: passed|blocked
audited: YYYY-MM-DD
round: 1
---

# self-service-account-system Goal 最终审计

## 1. Scope
## 2. Roadmap State
## 3. Final Aggregate Commands
## 4. Core Acceptance Paths
## 5. Deliverables And Writebacks
## 6. QA Residual Risk Review
## 7. Provider And E/C/H Evidence Summary
## 8. Workspace And Cleanliness
## 9. Verdict
```

同时写`goal-evidence-summary.md`或在第7节内嵌feature evidence packs、provider warnings、aggregate commands、E/C/H summary与H-only core checks。

## 6. 完成与学习反思

无缺口时先把goal-state status更新为complete，再打印：

```text
CS_ROADMAP_GOAL_AUDIT_COMPLETE
CS_ROADMAP_GOAL_LEARNING_REVIEW
CS_ROADMAP_GOAL_COMPLETE
```

Learning reflection只筛选候选，不自动写`.codestable/compound/`；需owner确认后再运行`cs-keep`。
