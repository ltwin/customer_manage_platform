---
doc_type: feature-design
feature: 2026-10-06-node-manual-resize
status: approved
requirement: V6-004
tags:
  - creative-canvas
  - frontend
  - backend
---

# V6-004 节点手动缩放与媒体比例 · design

需求：[V6-004](../../requirements/creative-workspace-v6/items/V6-004.md)，PRD CV-15/17/18。

## 1. 拍板（owner 2026-10-06）

- 尺寸约束定为最小 160×100、最大宽 1200，作为正式值，不再是 PRD 暂值。
- 越界处理：拖动时前端卡在边界；后端对越界的 `resize_node` 按校验错误拒绝。已有节点不迁移，下次缩放时才受新约束。

## 2. 现状（基线 c513835）

| 项 | 现状 | 目标 |
|---|---|---|
| 手柄可见 | 只要选中且有 `resize` 能力就显示，多选时每个节点都显示；组有 `resize` 能力 | 只有单选的普通节点显示；组没有 `resize` 能力 |
| 约束 | 前端 160×100，没有最大值；后端只拦小于 80 | 两端都是宽 160–1200；非媒体高度 ≥ 100 |
| 尺寸提示 | 没有 | 拖动中实时显示「宽 × 高」 |
| 媒体比例 | 后端 `fitMediaSize` 在生成、上传、填充、选版本、缩放后都按当前宽度重算高度；前端预览在读不到宽高时退回 1:1 | 读不到宽高时保持当前高度 |

组框由子节点自动撑开，已由 `editGraph` 的自底向上扩展实现（`graph_commands.go` 中计算 24/52/24 内边距的那一段）；本条只去掉手动缩放，撑开的细节由 V6-005 验收。

## 3. 方案

### 3.1 后端

- `definitions.go`：`core.group` 的 Actions 去掉 `resize`。
- `resize_node`：
  - 组直接拒绝（`ErrValidation`）。
  - 宽度不在 160–1200 之间时拒绝。
  - 非媒体节点（文本、链接、音频、文档）以及空的图片/视频节点，高度小于 100 时拒绝。
  - 有内容的图片/视频只校验宽度，高度由 `fitMediaSize` 按内容比例重算，客户端传的高度只是预览值。这样极宽的图片缩到 160 宽时不会因为高度小于 100 被拒。
- 常量 `minNodeWidth / maxNodeWidth / minNodeHeight` 与前端的 `nodeSizeLimits` 取值相同。

### 3.2 前端

- `outbox.ts` 导出 `nodeSizeLimits = { minWidth: 160, maxWidth: 1200, minHeight: 100 }`。
- `NodeResizer`：
  - 只在单选、非组、可编辑并且有 `resize` 能力时显示，`isVisible = selected && solo && …`。
  - `minWidth / maxWidth` 来自 `nodeSizeLimits`。`minHeight` 对等比的媒体取 0，由宽度约束兜住；其余类型取 100。
  - `keepAspectRatio` 规则不变：图片、视频为 true，音频为 false。
  - `onResize` 把四舍五入后的宽高写进卡片内的局部 state，在卡片右下角外侧显示「W × H」。`onResizeEnd` 清掉提示，并照常发出 `move_node + resize_node` 一个 intent。
- `resize()` 删掉组的分支（组不能再缩放）。
- `fitMediaHeight` 增加 `fallback` 参数：读不到宽高时返回 fallback。画布卡片传入当前 `metadata.height`，对应「尺寸未读到时保持原高度」；本机新建预览仍然默认 1:1。

### 3.3 撤销与保存

一次拖动松手只发一个 intent（move + resize），对应一步撤销，沿用现有机制。连续两次拖动在 pending 期间会被 `coalesce` 合并；拖动之间一般隔得比较久，第一次早已发出，所以通常是两步。验收时按「松手后等发送完成再拖下一次」来验证。尺寸随画布保存，刷新后保持，由 outbox 和快照保证。

### 3.4 比例 chip

AC-05 的最后一句（空媒体节点改比例 chip 时按当前宽度重算高度）依赖比例 chip。比例 chip 目前没有实现，由 V6-021 交付，V6-021 AC-03 的要求与此相同。本条不做这一部分，由 V6-021 AC-03 验收，并在验证记录里登记。

## 4. 验证计划

- Go：组的 `resize_node` 被拒；宽度 159、1201 被拒，160、1200 通过；文本高度 99 被拒；有内容的图片缩放后高度按比例重算，即使客户端传了一个不合理的高度。
- 前端单测：`fitMediaHeight` 的 fallback；`nodeSizeLimits` 与后端取值一致（两边的常量都有测试）。
- mock e2e `creative-node-resize.e2e.mjs`：
  - AC-01：手柄只在单选非组节点上出现。
  - AC-02：图片等比缩放、文本自由缩放、拖动中显示尺寸。
  - AC-03：一次拖动一步撤销。
  - AC-04：拖到边界时卡住。
  - AC-05：读不到宽高时保持当前高度。
  - 另做回退有效性验证。
- 门禁：`make check-go PKG=./internal/creativecanvas/...`、前端 lint/build/test、全部纯 mock e2e。
