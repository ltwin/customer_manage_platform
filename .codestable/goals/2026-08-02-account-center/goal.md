---
doc_type: goal
goal: account-center
status: complete
roadmap: account-center
branch: feat/account-center
---

# Goal · 摄影师账号中心

## Objective

交付完整账号中心产品面：全局头像按钮 + 独立 `/account/*` 工作区（资料／隐私与安全／系统设置），含账号头像不可变代次与备份恢复安全网，并通过 hardening 收口旧入口与回归证据。

## Starting Point

- Roadmap `account-center` 已 ConfirmRoadmap（active）。
- 五份子 design 已 design-review passed，并经 owner「开始实现」隐含 ConfirmAllChildDesign → **approved**。
- 工作分支：`feat/account-center`（普通分支，不用 worktree）；由 `main` tip 拉出（`develop` 当时落后于 `main`）。
- 代码现状：无 `account_profiles`、无 AccountMenu、无 `/account` 路由；客户头像与 Settings／auth／export 已存在。

## Acceptance Criteria

对齐 roadmap Goal Coverage 的 core 行：

1. 全局头像按钮可识别当前账号并进入三个板块 + 退出。
2. 可维护展示名称与账号头像；刷新／重登／冲突后按服务端版本显示。
3. 隐私与安全集中且保持既有 auth／export 语义。
4. Settings 五区 owned-field 与 dirty/stale 保护。
5. 账号资料／头像／设置／导出跨账号隔离。
6. 旧深链 redirect、移动端／缩放／粗指针可用；v1／v2 restore 与 `make check` 绿。

## Non-Goals

团队成员、工作室双资料、设备中心、换绑邮箱、登录历史、账号删除、公开主页、扩展资料字段、改写客户头像对象键／API。

## Decisions And Assumptions

- 执行序：`avatar-media-safety-net` → `account-profile-center`（最小闭环）→ privacy ∥ settings → hardening。
- 提交需人工同意（attention）；每条 feature 倾向独立 commit，不自动 push。
- 实现严格按已 approved design／checklist；发现契约缺口回 roadmap／design，不临场发明协议。
- ADR-004 补充已在工作区有未提交改动，safety-net STEP-001 先对齐再继续。

## Current State

- `status: complete`；`current_iteration: 5`。
- 功能验收：`functional-acceptance.md` **pass**（Task agent `e6d8ccea-a35e-4729-82a9-15ef4ad7c3c6`）。
- Owner residual：**1A 2A 3A**（见 `approval-report.md`）。

## Next Action

无（goal 完成）。Follow-Up：资料双账号断言、compose-identity issue、Docker 后补 check／截图、owner 同意后 commit。
