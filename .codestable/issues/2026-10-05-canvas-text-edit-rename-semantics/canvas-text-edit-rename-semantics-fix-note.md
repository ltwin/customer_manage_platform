---
doc_type: issue-fix
issue: 2026-10-05-canvas-text-edit-rename-semantics
status: confirmed
path: fast-track
fix_date: 2026-10-05
requirement: V6-001
tags:
  - frontend
  - creative-canvas
  - inline-edit
---

# 画布文字编辑与节点重命名语义修复记录

## 1. 问题描述

对照 PRD CV-60/61/62（需求 [V6-001](../../requirements/creative-workspace-v6/items/V6-001.md)），画布上有四处行为与需求不一致：

1. 文字节点在卡片内编辑时按 Esc 会丢弃改动；PRD 要求 Esc 退出并保存（CV-61，偏离原型的「按此为准」项）。
2. 节点和组标题清空后按 Enter 或失焦，会把空标题提交为新名称；PRD 要求空名称不提交。
3. 编辑文字期间节点工具条仍然显示；PRD 要求编辑期间隐藏。
4. 文本节点工具条没有「编辑」入口；PRD 列出双击、Enter、工具条「编辑」、右键菜单四个入口。

## 2. 根因

- `InlineNodeText` 的 Esc 分支直接调用 `onCancel(original)`，走的是放弃草稿的路径，而不是 `save()`。
- `InlineNodeTitle.finish` 只比较了 `value.trim() !== title`，没有排除 trim 后为空的情况。
- `CanvasView` 只按 `active` 渲染选区工具条，没有考虑内联编辑状态。
- 工具条的按钮列表里没有文本节点的编辑项。

## 3. 修复方案

1. `InlineNodeText`：Esc 改为调用 `save()`，与点击空白处一致。内容未改动时 `save()` 本来就只退出、不写入；链接校验不通过时保留编辑器并显示错误，与失焦一致。
2. `InlineNodeTitle`：trim 后为空或与原名相同都不调用 `onSave`，只关闭输入框。节点标题和组标题共用这个组件，一处修复同时覆盖两者。
3. `CanvasView`：存在内联编辑时不渲染选区工具条，编辑结束后恢复。
4. `CanvasView`：单选可编辑的文本节点时，工具条增加「编辑」按钮，调用的入口与双击、Enter、右键菜单相同（`onEdit`）。工具条其余按钮的整体对齐留给 V6-007。

不改后端、OpenAPI、保存队列或草稿机制。

## 4. 改动文件清单

- `frontend/src/creative-canvas/editor/InlineNodeEditor.tsx`：Esc 保存；空标题不提交。
- `frontend/src/creative-canvas/editor/CanvasView.tsx`：编辑期间隐藏工具条；文本节点工具条增加「编辑」。
- `frontend/scripts/creative-canvas-interaction.e2e.mjs`：原来断言「Esc 不保存正文」，现改为 Esc 保存且可撤销。新增断言：空名在 Enter 和失焦时都不提交、未改动不留撤销步骤、编辑期间工具条隐藏且结束后恢复、未改动按 Esc 不留撤销步骤、工具条「编辑」、右键菜单与 Enter 都能进入卡片内编辑器。

## 5. 验证结果

- `npm run lint`：0 error；剩余 warning 均为改动前已有。
- `npm run build`：通过。
- `make test-frontend`：391/391 通过。
- `node scripts/creative-canvas-interaction.e2e.mjs`（Vite dev server + 全量 mock API）：连续 3 次通过；`creative-library`、`creative-generation` e2e 同时通过。
- 回归有效性：三处修复分别临时回退后跑同一 e2e，各自在对应断言处失败（空标题保留旧名、Esc 保存正文、编辑期间工具条隐藏），恢复修复后通过。

未验证：

- `creative-agent.e2e.mjs` 需要另起 5179 端口的 QA 页面，在修复前、后都因连接被拒而无法运行，与本次改动无关。
- `creative-text-canvas.e2e.mjs` 需要真实后端和账号，本次未运行。它里面的 Esc 用法都发生在正文未改动时，按新语义仍然只是退出。
- 未做视觉走查。

## 6. 验收对应

| AC | 证据 |
|---|---|
| AC-01 文字编辑按 Esc 或失焦后保存有效改动，重开仍在 | interaction e2e：Esc 保存后卡片显示新正文，并可一步撤销；失焦保存沿用原有断言。重开后仍在由既有保存队列和本机草稿保证（`creative-text-canvas` e2e 的离线、刷新场景），本次未改这条路径 |
| AC-02 名称 Enter/失焦提交、Esc 取消，空名和未修改不写入、不留撤销步骤 | interaction e2e：Enter 与失焦提交、Esc 取消沿用原有断言；空名在 Enter 和失焦时保留旧名，未修改 Enter 不改变撤销/重做栈 |
| AC-03 输入法组字期间 Enter/Esc 不误提交，保存失败保留草稿 | interaction e2e：标题与正文的组字 Enter 不提交（原有断言）；Esc 组字分支与 Enter 共用同一个 `isComposing` 早退。保存失败保留草稿由既有保存队列保证（`creative-text-canvas` e2e 的冲突草稿场景），本次未改这条路径 |
| AC-04 四个入口一致；有变化才记撤销；编辑期间隐藏工具条 | interaction e2e：双击、Enter、工具条「编辑」、右键「编辑节点」都打开同一个卡片内编辑器；未改动按 Esc 不留撤销步骤；编辑期间工具条隐藏、结束后恢复 |

右键菜单项目前叫「编辑节点」，PRD CV-80 写的是「编辑文字（仅文本）」。菜单文案按 V6-007 统一调整，本条不改，已在 V6-007「当前基础」登记。
