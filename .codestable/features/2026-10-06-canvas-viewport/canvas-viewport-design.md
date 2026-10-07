---
doc_type: feature-design
feature: 2026-10-06-canvas-viewport
status: approved
requirement: V6-002
tags:
  - creative-canvas
  - frontend
---

# V6-002 视口缩放、平移与面板避让 · design

需求：[V6-002](../../requirements/creative-workspace-v6/items/V6-002.md)，PRD CV-01/02/03/04、AL-02。本条只改前端，不改后端、OpenAPI。

## 1. 现状（基线 688929a）

| 项 | 现状 | 目标 |
|---|---|---|
| 缩放范围 | `minZoom 0.15 / maxZoom 2`；恢复视口时也只接受 0.15–2 | 10%–400% |
| ⌘+/⌘- | 没有键盘缩放；缩放条按钮用 xyflow 的 `zoomIn/zoomOut`（以屏幕中心、1.2 倍缩放） | ⌘= / ⌘+ / ⌘- 以屏幕中心按 1.2 倍缩放，和按钮共用同一个调用 |
| 指针缩放 | `zoomActivationKeyCode=[Control, Meta]` 加滚轮、触控板捏合，都以指针为中心 | 不变，补上验收 |
| 适应画布 | 有 4 处，都调用 `fitView({ padding: 0.25, maxZoom: 1 })`：⇧1、缩放条、右键菜单、首次打开 | 一个 `fitCanvas()`：包围盒顶部加 28px 标题区，倍率限制在 20%–110%，避开资产库和助手面板 |
| 视口恢复 | localStorage 按 `account:canvas` 存取，数值非法时不恢复；首次打开由 `fitView` 属性兜底 | 不变；数值非法或不存在时调用 `fitCanvas()` |
| Space | `panActivationKeyCode="Space"`，有没有选中都会平移 | 没有选中时按住平移；单选一个有内容的媒体节点时打开全屏预览；其他情况不处理 |
| 菜单 | `onMoveStart` 关闭添加菜单和右键菜单 | 不变，补上验收（滚轮平移也会触发） |

## 2. 方案

### 2.1 视口工具 `viewport.ts`（纯函数）

```ts
export const zoomLimits = { min: 0.1, max: 4 }
export const fitZoomLimits = { min: 0.2, max: 1.1 }
// stage 是画布在屏幕上的矩形，insets 是被面板挡住的部分，bounds 是世界坐标包围盒。
export function fitViewport(bounds, stage, insets): { x, y, zoom }
export function nodeBounds(nodes): Rect | null   // 世界坐标；只算根节点，顶部额外加 28px 标题区
export function validViewport(v): v is Viewport  // 数值有限，且缩放在 zoomLimits 内
```

`fitViewport` 先在去掉 insets 后的可见区里按包围盒算出缩放倍率，再限制到 20%–110%，最后居中，规则与原型的 `fitView` 一致。

### 2.2 面板避让

`CanvasView` 在调用时读取实际 DOM，不额外维护面板状态：

- 左侧：如果 `#creative-library` 可见，左侧让出 `它的 right − stage.left + 16`；否则让出 80，给工具轨留位置。
- 右侧：如果 `.cc-agent-panel` 存在，右侧让出 `stage.right − 它的 left + 16`；否则让出 40。
- 上下各让出 90，给顶部浮块和底部缩放条留位置。
- 窄屏（≤760px）：左右各 16，上 72，下 80，与原型一致。

### 2.3 接入点

- `minZoom / maxZoom` 改为 `zoomLimits`。
- 去掉 `fitView` 属性和 `fitViewOptions`。改为在 `onInit` 时读取存储的视口：合法就恢复，否则在节点测量完成后调用一次 `fitCanvas()`。
- ⇧1、缩放条的「适应」按钮、右键菜单的「适应全部节点」都调用 `fitCanvas()`。资产库定位节点（`focusRequest`）不改，它只是聚焦到选中的几个节点，不是适应画布。
- 键盘：在画布的 keydown 里加 ⌘= / ⌘+ / ⌘- / ⌘0（⌘0 回到 100%），调用 `flow.zoomIn / zoomOut / zoomTo(1)`，都以屏幕中心缩放。浏览器默认的页面缩放要 `preventDefault`，只在焦点位于画布内时拦截。
- Space：
  - `panActivationKeyCode` 改为 `selection.length ? null : 'Space'`，`panHeld` 同样只在没有选中时生效。
  - 单选一个有内容、且支持 maximize 的媒体节点时，按 Space 调用 `onPreview`，与双击、工具条「放大预览」走同一个入口。
  - 焦点在按钮或输入框上时不处理 Space，沿用现有的 keydown 守卫。
- 帮助文案「按住空格拖动」改为「未选中时按住空格拖动」，并新增一行「全屏查看 · Space」。

### 2.4 撤销与保存

视口变化只写 localStorage，不进 outbox 或撤销栈，这一点不变。验收时断言缩放、平移之后撤销按钮仍然不可用。

## 3. 验证计划

- 前端单测：`fitViewport`（左右面板让位、倍率限制在 20%–110%、标题区 28px）、`validViewport`、`nodeBounds`（只算根节点，包含子节点不重复计入）。
- mock e2e `creative-canvas-viewport.e2e.mjs`：
  - AC-01：⌘+ 和 ⌘- 以屏幕中心 1.2 倍缩放，缩放卡在 10% 和 400%。
  - AC-02：打开资产库或助手后，适应画布时节点落在面板外，倍率在 20%–110%。
  - AC-03：刷新后视口恢复；存储值非法时执行适应；视口变化不进撤销栈。
  - AC-04：Space 有选中和没选中两种分支；中键和平移工具都能平移；滚轮关闭菜单。
- 另做回退有效性验证，并跑全部纯 mock e2e（`creative-canvas-baseline` 的 AC-01 里有 100% 与适应画布的断言）。
