# Worktree Override

- reason: 用户明确要求直接在当前 `develop` 分支开发，不使用 linked worktree。
- scope: 本次只覆盖 `customer-core` feature 的契约、后端客户域、受保护客户 API、前端客户建档/列表/详情与对应 CodeStable 证据；不切换分支，不回滚既有未提交改动。
- approval: 2026-07-07 用户确认：“好，允许approved，然后直接在当前分支开发”。
- cleanup: 实现完成后按 CodeStable review / QA / acceptance 流程处理，提交前再次征得用户同意。
