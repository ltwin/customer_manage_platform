# worktree override

- reason: current-checkout-implementation
- scope: `.codestable/features/2026-07-08-order-tracking` order-tracking implementation on the existing `develop` checkout.
- approval: Project memory records owner preference: "在当前 develop 分支上继续开发，不使用 worktree"; `.codestable/attention.md` also notes single-feature development tends not to use a worktree.
- risk_control: Do not switch branches, do not revert unrelated existing changes, and keep implementation evidence in the feature checklist/report.
