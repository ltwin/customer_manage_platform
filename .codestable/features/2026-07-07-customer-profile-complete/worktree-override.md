# worktree-override

- feature: 2026-07-07-customer-profile-complete
- date: 2026-07-07

## reason

单 feature 串行开发，无并行开发需求。按 `.codestable/attention.md`「单feature开发倾向于不开worktree」的约定，在主工作区从 `develop` 拉 `feat/customer-profile-complete` 分支开发，不创建 linked worktree。

## scope

仅本 feature（customer-profile-complete）的实现周期：分支 `feat/customer-profile-complete`，改动范围以 `customer-profile-complete-design.md` 挂载点清单为准。后续 feature 开工时重新走 gate 判定。

## approval

用户于 2026-07-07 在实现启动问答中选择「当前仓库拉 feat/ 分支 + override」，明确批准本次不开 worktree。
