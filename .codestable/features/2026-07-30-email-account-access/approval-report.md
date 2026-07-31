---
doc_type: approval-report
unit: .codestable/features/2026-07-30-email-account-access
status: pending
reason: review-authorization
approvals:
  code-review-skip-failed-ocr: pending
approval_groups: {}
created_at: 2026-07-31
---

# email-account-access 审查恢复决策

## Decision Needed

是否跳过本轮已超时失败的可选 OCR 行级扫描，继续以唯一独立 Task agent 审查和主 agent 本地行级核验收口代码审查。

## Why Now

- OCR 执行 ref 为 `exec-session:61847`。
- `ocr llm test` 连接自检成功，但正式 `ocr review` 运行超过 10 分钟仍没有任何输出。
- 为避免可选扫描无限阻塞 Goal execution，主 agent 已终止该执行，进程退出码为 130，本轮不会自动重启。
- gate 必需的独立 Task agent reviewer `/root/self_service_account_goal_driver/email_access_independent_review` 仍在正常运行，且已产生可由仓库事实核验的 findings。

## Context

- Review report: `.codestable/features/2026-07-30-email-account-access/email-account-access-review.md`
- Failed lane: `lane_b`
- Failed ref: `exec-session:61847`
- Named decision: `code-review-skip-failed-ocr`
- OCR 是可选行级补充，不能代替且不会削弱独立 Task agent gate。

## Options

### Option A — 跳过本轮失败 OCR（推荐）

将 `approvals.code-review-skip-failed-ocr` 记为 `approved`，仅把精确失败 ref `exec-session:61847` 对应的 lane B 转为 `skipped`。唯一独立 Task agent 必须完成，所有 findings 仍需本地事实核验，blocking 仍必须修复。

### Option B — 保持阻塞

将 `approvals.code-review-skip-failed-ocr` 记为 `rejected`，保留 lane B `failed`。代码审查不能放行；后续只能在工具配置或容量变化后，对同一失败 ref 走 typed retry。

## Recommendation

推荐 Option A。OCR 已经超过有界等待且没有产生可消费结果；继续等待或立即重试只会延长阶段时间。保留必需的独立 Task agent + 主 agent 本地行级审查，仍能对 spec 合规和代码质量做出可核验结论。

## Risks And Tradeoffs

- 跳过 OCR 会少一个机器行级视角，未发现的窄行级模式风险需由主 agent 行级审查与 QA 关注补位。
- 这个 skip 只对 `exec-session:61847` 有效，不是全局关闭 OCR，也不是接受任何 code finding 或 residual risk。
- 独立 reviewer 未完成前，review 仍不能定稿。

## Non-Automatic Actions

本决策不会自动：

- 把 review 标记为 passed；
- 忽略或接受 blocking / important findings；
- 跳过 QA 或 acceptance；
- 创建 commit、push、PR、merge 或 deploy；
- 执行生产 migration、cutover、secret rotation 或切换注册开关。

## After You Answer

- 若批准 Option A：将命名决策记为 `approved`，以 `ResumeSkipFailedLaneB exec-session:61847 approval-report.md#code-review-skip-failed-ocr` 恢复，把 lane B 记为 `skipped`，继续等待并合并唯一独立 reviewer。
- 若拒绝 Option B：保留 lane B `failed`，代码审查阻塞，不进入 QA。

## Decision History

- 2026-07-31：owner 批准 Goal execution，但没有对本命名决策选择 Option A 或 Option B；不得把 Goal approval 机械复用为 OCR skip approval。
- 2026-07-31：owner 要求当前 findings 修复后不再继续下一轮 review。该 stop 指令终止 Goal review loop，但不等于批准 `ResumeSkipFailedLaneB`；本报告与命名决策继续保持 `pending`，lane B 保持真实 `failed` 历史，Goal 转为 owner-stop handoff。
