---
doc_type: goal-functional-acceptance
goal: account-center
status: pass
reviewer_id: e6d8ccea-a35e-4729-82a9-15ef4ad7c3c6
reviewer_role: Acceptance
final_iteration: iterations/005.md
branch: feat/account-center
updated: 2026-08-02
---

# Functional Acceptance · account-center

## Reviewer

- Task agent：[Account-center functional accept](e6d8ccea-a35e-4729-82a9-15ef4ad7c3c6)
- 角色：Acceptance（只读）；消费后关闭（本报告为结论落盘）
- Owner residual 裁决：`approval-report.md` → **1A 2A 3A**

## Acceptance Checks

| # | 准则 | 结论 |
|---|---|---|
| 1 | 全局头像按钮／菜单六动作 | pass |
| 2 | 展示名称／头像可维护＋回退 | pass |
| 3 | privacy 最终 IA（auth／export 语义） | pass |
| 4 | settings 五区 owned-field／dirty/stale | pass |
| 5 | 跨账号隔离（基座强制） | pass（资料专属双账号断言见 Follow-Up） |
| 6 | redirect／导航收口；restore／截图／全量 make check | redirect pass；其余按 1A/2A/3A waived |

## Functional Evidence

- 路由：`/account`、`/profile`、`security*`、`settings` 真实页面，无占位
- redirect：`App.tsx` 保护组内 `/settings`、`/change-password` → `Navigate replace`；旧页已删；HTTP `/settings` API 保留
- 本会话／验收复跑：`test:account-center` **29 pass**；`golangci-lint ./...` **0 issues**；accountprofile／avatarmedia／dataexport／httpapi 包测绿（含导出 v3、双账号 HTTP 隔离）
- iterations 001–004 + hardening evidence（D11／defect-routing／breakpoint 清单）

## Verdict

**pass**

## Residual Risks

1. CMD-002 全量 restore（compose-identity）— **waived 1A**
2. 断点截图包 — **waived 2A**
3. 依赖 Docker 的全量 `make check` — **waived 3A**
4. `account_profiles` 专属双账号 E2E 断言尚未落盘（隔离由 AccountScope 强制）

## Delivery Record

avatarmedia 安全网；accountprofile＋migration 0015＋GC；OpenAPI profile＊＋codegen；export v3；AccountCenter Context／Menu／Layout／四页；privacy IA；settings 五区；hardening redirect／nav／D11／键盘循环。

## Follow-Up

1. 补 account_profiles 双账号 E2E 断言  
2. 另开 issue：restore-compose compose-identity  
3. Docker 就绪后补 `make check`＋断点截图  
4. owner 同意后按 feature 粒度 commit → develop  
