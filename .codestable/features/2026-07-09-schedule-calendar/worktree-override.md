# Worktree Override

## reason

本次 `schedule-calendar` 单 feature 按 owner 明确授权，直接在当前 `develop` 检出实现，不创建 linked worktree；保留并基于当前工作区尚未提交的需求、roadmap、OpenAPI、design 与 checklist 契约改动继续推进。

## scope

仅覆盖 `.codestable/features/2026-07-09-schedule-calendar` 已批准 design/checklist 声明的契约生成、幂等基础设施、schedule 与 order 后端、月历与共享排期前端、自动化测试和实现证据。不得回退当前工作区既有改动，不自动 commit、merge 或 push。

## approval

用户于 2026-07-10 在实现启动问答中明确批准："允许就在当前develop上实现"。
