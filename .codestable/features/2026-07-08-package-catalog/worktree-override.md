---
doc_type: worktree-override
unit: 2026-07-08-package-catalog
status: approved
created_at: 2026-07-08
---

# Worktree Override

## Reason

本仓库当前按 owner 既有偏好在 `develop` 检出继续单 feature 实现，不创建 linked worktree。

## Scope

仅限 `.codestable/features/2026-07-08-package-catalog/package-catalog-design.md` 对应的 package-catalog 实现：OpenAPI / codegen、后端套系域与路由、前端 PackagesPage 真 API 迁移、测试与本 feature checklist 状态回写。

## Approval

沿用本仓库已记录的 owner 偏好：「在当前 develop 分支上继续开发，不使用 worktree。」本次用户在当前 checkout 直接触发 `cs-feat-impl` 执行该 feature。

## Guardrails

- 不做 commit / merge / push，除非 owner 明确要求。
- 不回退当前工作区已有的 `.codestable` 规划与需求改动。
- 每个 checklist step 完成立即验证并回写 status。
