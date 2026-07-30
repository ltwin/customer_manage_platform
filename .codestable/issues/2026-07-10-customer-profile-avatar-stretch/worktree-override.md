# Worktree Override

## reason

本次客户详情头像拉伸是单点 UI 缺陷，owner 已明确批准直接在当前 `develop` 检出修复，不创建 linked worktree；当前工作区同时保留其他尚未提交的功能开发改动。

## scope

仅覆盖客户详情头像 flex 布局回归测试、`frontend/src/index.css` 中的最小选择器修复，以及 `.codestable/issues/2026-07-10-customer-profile-avatar-stretch/` 下的修复记录。不得回退或改写当前工作区既有改动，不自动 commit、merge 或 push。

## approval

用户于 2026-07-10 在快速通道确认中回复 `ok`，批准按已说明的根因与方案在当前 `develop` 检出直接修复。
