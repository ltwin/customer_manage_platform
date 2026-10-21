---
slug: epic-revise-sync-planning-root
status: observed
scope: lti：子项实施期间（worktree）修订 Epic 后重新批准子项
createdAt: 2026-10-21T03:25:02.946Z
source: 07c86d41-6dbb-4054-88aa-e92384e4d8bc
---
worktree 里 lti epic revise、epic bind 都只改 worktree 的 .lti，但 lti approve 会再对照 planningRoot（主检出、集成分支）上的 Epic 记录；那里还没有新版本时报 epicDesignStale「missing approved version」。做法：revise 之后把 Epic 目录下的 meta.json、proposal.md、design.md、contracts.md、roadmap.md 复制到主检出并在集成分支提交（先核对两边已提交的 Epic 文件一致），再在 worktree 里 approve。先例：v9（037cd63b）、v12（66e95441）。
