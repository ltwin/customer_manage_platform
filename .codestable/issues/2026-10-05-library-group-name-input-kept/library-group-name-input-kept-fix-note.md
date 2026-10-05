---
doc_type: issue-fix
issue: 2026-10-05-library-group-name-input-kept
status: confirmed
path: fast-track
fix_date: 2026-10-05
requirement: V6-017
tags:
  - frontend
  - creative-library
  - asset-group
---

# 资产分组新建/改名校验失败丢失输入修复记录

## 1. 问题描述

资产库就地新建或重命名分组时，名称与同层分组重名、超过 80 字或被服务端拒绝，输入框会直接消失，已输入的名称丢失，摄影师只能重新点「新建分组」再输一遍（PRD AL-63；需求 [V6-017](../../requirements/creative-workspace-v6/items/V6-017.md)）。

## 2. 根因

`frontend/src/creative-canvas/editor/LibraryPanel.tsx` 的 `commitName` 第一步就 `setNameEdit(null)` 关闭编辑态，之后才做重名、长度校验并发请求。校验失败或请求被拒时输入框已经卸载，uncontrolled input 的值随之丢失。

## 3. 修复方案

1. 只在成功、空名称（视为取消）或名称未变时关闭编辑态；重名、超长、服务端拒绝都保留输入框和已输入内容，并给出提示。
2. 由 Enter 触发的失败把焦点留在输入框，方便直接修改；由失焦触发的失败不抢回焦点，输入框留在原位。
3. 增加提交中锁 `naming`：请求在途时再按 Enter 或失焦不重复发请求；成功后锁一直保持到编辑态切换，避免输入框卸载时补发的失焦再提交一次。
4. 服务端拒绝仍走现有保存队列的「被拒绝请求」提示；关闭提示后可在原输入框改名重试。不改后端、OpenAPI 或保存队列。

## 4. 改动文件清单

- `frontend/src/creative-canvas/editor/LibraryPanel.tsx`：`commitName` 改为失败保留编辑态，新增提交中锁。
- `frontend/scripts/creative-library.e2e.mjs`：场景 7 增加重名保留输入与焦点、失焦失败保留、服务端 `creative_group_name_taken` 拒绝保留输入、连按两次 Enter 只发一次、Esc 取消、输入法组字 Enter 不提交、失焦提交一次；mock 增加服务端占用名。

## 5. 验证结果

- `npm run lint`：0 error；剩余 warning 均为改动前已有。
- `npm run build`：通过。
- `make test-frontend`：391/391 通过。
- `node scripts/creative-library.e2e.mjs`（Vite dev server + 全量 mock API）：连续 3 次通过。
- 回归有效性：把 `LibraryPanel.tsx` 临时换回修复前版本跑同一 e2e，在「重名保留输入」处失败（输入框已卸载），恢复修复后通过。

未验证：真实后端与多窗口并发下的服务端拒绝路径只用 mock 409 覆盖；未做视觉走查。

## 6. 验收对应

| AC | 证据 |
|---|---|
| AC-01 同层重名、超长或服务端拒绝时保留输入及编辑位置 | e2e 场景 7：本机重名（Enter/失焦）与服务端拒绝均保留输入，Enter 失败后焦点仍在输入框。超长由 `maxLength=80` 在输入层限制，并保留超长校验分支 |
| AC-02 Enter/失焦提交，Esc 取消，输入法不误触发 | e2e 场景 7：Enter 提交、失焦提交、Esc 取消不发请求、组字中 Enter 不提交 |
| AC-03 成功只创建/改名一次，不重复请求或产生空分组 | e2e 场景 7：连按两次 Enter 只发一次请求，成功后输入框关闭；空名称视为取消不发请求 |
