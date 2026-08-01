---
doc_type: feature-approval-report
feature: 2026-07-31-calendar-v2-redesign
updated: 2026-08-01
---

# Calendar v2 redesign 审批记录

## code-review-local-only

- status: approved
- decision_id: `code-review-local-only`
- decided_at: 2026-08-01
- scope: 终止 Round 11 及后续重复代码审查，不再启动新的 reviewer；以已完成的独立审查历史、REV-023 修复、canonical gates 与真实浏览器证据做本地收口。
- owner_instruction: “不要一直review了，已经跑了一整天了你知道吗？”
- boundary: 本批准只终止追加 review 循环，不等同于 Goal acceptance authorization，不授权 commit、push、PR、merge 或 deploy。

## goal-acceptance-calendar-v2

- status: approved
- decision_id: `goal-acceptance-calendar-v2`
- confirmation_id: `f127b48a-5b54-4b5c-8bae-8b3aeb6a4f58`
- decided_at: 2026-08-01
- authorization_ref: `approval-report.md#goal-acceptance-calendar-v2`
- owner_instruction: “那么现在你可以提交代码，并合入main分支，另外我注意到主工作区中还有一些改动，你得注意一下是否有冲突”
- scope: 在实现、QA、canonical gates 和浏览器矩阵均已通过的当前 Calendar v2 diff 上完成验收，并允许创建一个 scoped feature commit。
- boundary: 不恢复追加 review；不授权 push、deploy 或修改主工作区 `feature/userCenter` 的未提交内容。

## commit-merge-calendar-v2

- status: approved
- decision_id: `commit-merge-calendar-v2`
- confirmation_id: `f127b48a-5b54-4b5c-8bae-8b3aeb6a4f58`
- decided_at: 2026-08-01
- scope: 将 `feat/calendar-v2-redesign` 的 scoped commit 合入本地 `main`。
- isolation: 在独立干净的 `main` worktree 中执行冲突预检与合并；不得切换、暂存、提交或清理主工作区 `feature/userCenter`。
- boundary: 不授权 push、PR、deploy，也不改变 parent Goal 中 `v1-hardening` 的 handoff/current index 语义。
