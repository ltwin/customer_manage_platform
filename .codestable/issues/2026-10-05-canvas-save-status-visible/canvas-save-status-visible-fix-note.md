---
doc_type: issue-fix
issue: 2026-10-05-canvas-save-status-visible
status: confirmed
path: fast-track
fix_date: 2026-10-05
requirement: V6-059
tags:
  - frontend
  - creative-canvas
  - save-queue
---

# 画布保存状态可见修复记录

## 1. 问题描述

对照 PRD CV-A4（需求 [V6-059](../../requirements/creative-workspace-v6/items/V6-059.md)）：保存队列、断网草稿和冲突处理已经有了，但正常编辑时看不到「保存中 / 已保存」。只有离线、出错、待恢复时才出现提示块，平时只有画布上的内部属性 `data-sync` 在变。

## 2. 根因

`WorkspacePage` 只在异常时渲染 `.cc-save-status` 提示块，没有给正常保存流程做展示。`data-sync` 只区分「outbox 有未确认改动」和「空闲」，不区分离线和冲突，而且不可见。

## 3. 修复方案

1. 新增 `editor/saveStatus.ts`：一个纯函数，从本画布的 outbox、单通道 job、网络状态、节点内容对比弹窗推出状态。判断顺序：
   - 冲突：正在对比另一窗口改过的节点内容，或画布命令因 `creative_revision_conflict` 被拒；
   - 离线：断网时有待保存修改；
   - 被拒绝：画布命令因其他原因被拒；
   - 待确认：请求结果不明，且当前没有在发送；
   - 保存中：outbox 有未确认的修改，或画布命令正在发送；
   - 已保存：本会话在这张画布上至少拿到过一次服务端回执，且之后没有待保存修改；
   - 其余情况不显示。打开画布后还没编辑过时不显示「已保存」。
2. `Job` 增加可选字段 `conflict`。`SaveQueue` 只在请求被拒、错误码为 `creative_revision_conflict` 时写入它；旧 journal 没有这个字段，不影响读取校验。
3. 项目名旁边显示一行只读文字（`role="status"`、`pointer-events: none`）。离线和待确认用 warning 色，冲突和被拒绝用 danger 色。原有的详细提示和恢复按钮不变。
4. `data-sync` 保持原来的含义。`creative-media`、`creative-text-canvas`、`creative-canvas-commands` 这几个 e2e 靠它等待 outbox 清空。

状态只读 React state，不写 journal，不进撤销栈。不改后端、OpenAPI，也不改保存队列的发送和恢复逻辑。

原型用 localStorage，没有保存状态的位置和文案（PRD CV-A4 备注里写了「按基础需求实现」），所以位置和文案是本条定的：放在左上浮块项目名右侧，文案是「保存中 / 已保存 / 离线 · 修改保留在本机 / 保存冲突 · 待处理 / 保存被拒绝 / 保存待确认」。

## 4. 改动文件清单

- `frontend/src/creative-canvas/editor/saveStatus.ts`：新增，状态推导和文案。
- `frontend/src/creative-canvas/editor/WorkspacePage.tsx`：记录本会话拿到过回执的画布；在头部渲染状态。
- `frontend/src/creative-canvas/editor/journal.ts`、`queue.ts`：`Job.conflict`。
- `frontend/src/creative-canvas/editor/workspace.css`：`.cc-save-state`。
- `frontend/scripts/creative-save-status.test.ts`：新增，状态推导和冲突标记的单测。
- `frontend/scripts/creative-save-status.e2e.mjs`：新增，mock e2e，每条 AC 用一个新的 browser context。命令请求可以被挂起、放行（返回成功回执）或返回 409。

## 5. 验证结果

- `npm run lint`：退出码 0，改动文件没有 warning。
- `npm run build`：通过。
- `make test-frontend`：396/396 通过（新增 5 项）。
- `node scripts/creative-save-status.e2e.mjs`（Vite dev server + 全量 mock API）：AC-01–04 全部通过。
- 回归有效性：`WorkspacePage.tsx` 换回 HEAD 版本后，同一 e2e 的 AC-01–04 全部失败（等不到状态文字）；换回修复后通过。冲突标记由单测覆盖（`archived_read_only` 被拒时不标冲突）。
- 同时通过：`creative-canvas-interaction`、`creative-canvas-baseline`、`creative-composer-baseline`、`creative-library`、`creative-library-baseline`、`creative-generation` e2e。

未验证：

- `creative-text-canvas`、`creative-media`、`creative-canvas-commands` e2e 需要真实后端和账号，本次未运行。它们依赖的 `data-sync` 没有改。
- 真实双窗口冲突没有在浏览器里跑；e2e 用 mock 的 409 `creative_revision_conflict` 模拟另一窗口先保存。
- e2e 保存了头部截图（`/tmp/v6-059-saving.png`、`/tmp/v6-059-saved.png`），但还没人看过，视觉走查留给 owner。

## 6. 验收对应

| AC | 证据 |
|---|---|
| AC-01 有未确认修改时显示「保存中」，服务端确认后才显示「已保存」 | e2e AC-01：打开后不显示状态；移动节点后显示「保存中」，请求挂起 600ms 仍是「保存中」；放行回执后才变成「已保存」；第二次编辑重新走一遍。单测：较早的回执不覆盖之后的待保存修改 |
| AC-02 断网显示离线并保留本机草稿，恢复后自动提交并回到正常状态 | e2e AC-02：断网后编辑显示「离线 · 修改保留在本机」，期间没有发出请求；恢复网络后自动提交的正是这次移动，经过「保存中」到「已保存」，位置保持不变。刷新后草稿仍在由既有 journal 保证（`creative-text-canvas` e2e），本次未改 |
| AC-03 多窗口冲突时双方内容都保留，状态说明冲突而不是已保存 | e2e AC-03：先保存成功显示「已保存」，再编辑正文并收到 409 `creative_revision_conflict`，状态变成「保存冲突 · 待处理」，400ms 后也没有回到「已保存」；出现「关闭被拒绝的请求，保留草稿」，被拒的请求不会自动重发。服务端保留另一窗口的内容；本窗口的正文留在本机草稿里。节点内容对比弹窗打开时同样显示冲突（单测） |
| AC-04 不干扰画布操作，不进撤销栈 | e2e AC-04：状态元素 `pointer-events: none`，不获取焦点；一次编辑只留一步撤销，撤销以后撤销栈恢复为空；状态变化不产生撤销步骤 |
