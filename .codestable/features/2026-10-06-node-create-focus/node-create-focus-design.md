---
doc_type: feature-design
feature: 2026-10-06-node-create-focus
status: approved
requirement: V6-006
tags:
  - creative-canvas
  - frontend
---

# V6-006 新建入口、快捷键与编辑焦点 · design

需求：[V6-006](../../requirements/creative-workspace-v6/items/V6-006.md)，PRD CV-50–54。本条只改前端。

## 1. 现状（基线 701052d）

| 项 | 现状 | 目标 |
|---|---|---|
| 统一菜单 | 工具轨、双击空白、右键空白、端口都打开 `addMenuItems` | 不变，补验收 |
| 端口菜单标题 | 输出端口为「添加下游节点」；文本输出端口的「图片」说明为「以上游文字为提示词」 | 「生成下游节点」；「以上游文本为提示词」（CV-51 原文） |
| 快捷键 | T/I/O 在屏幕中心新建；输入框聚焦或按修饰键时不触发 | 不变；A（音频）走同一机制，菜单「音频」项标注 A |
| 新建后焦点 | 只选中新节点，生成面板随之出现，但没有焦点 | 新文本节点立即进入卡片内编辑；新图片/视频/音频节点把焦点交给生成输入框 |
| 新节点与组 | `add_node` 不带 `parent_id`，不会加入组 | 不变，补验收 |
| 双击 | 标题 → 重命名；文本 → 编辑；有内容的媒体 → 全屏；空媒体 → `onEdit`（媒体节点没有内容时直接返回，什么都不做）；空白 → 添加菜单 | 空媒体 → 聚焦生成输入框，不上传、不生成 |

链接（L 快捷键和菜单「链接」项）由 V6-009 交付：新建链接后要进入元信息编辑，这依赖 V6-009 的链接编辑器。本条只提供共用的新建与焦点机制。

## 2. 方案

### 2.1 一个「聚焦生成输入框」请求

- `WorkspacePage` 新增 state `promptFocus: { seq, nodeID }`，作为 prop 传给 `NodeGenerationPanel`。
- `NodeGenerationPanel` 收到 `seq` 变化且 `nodeID` 是本节点时调用 `editor.focus()`。生成面板在待保存期间是 `locked`（`PromptEditor` 的 contentEditable 为 false）。这个请求会一直保留，直到面板解锁、真正拿到焦点，所以本机新建的节点在服务端确认后焦点才落到输入框，期间不会丢。
- `PromptEditorHandle` 增加 `focus()`。

### 2.2 新建后的焦点

`CanvasView` 的 `createNode` 新建并选中节点后，调用新回调 `onCreated(id, kind)`：

- 文本：`WorkspacePage` 等这个节点出现在投影里之后，调用 `editInline`。实现方式是用一个 ref 记下「待编辑的 id」，在 `view` 变化的 effect 里消费。
- 图片/视频/音频：`setPromptFocus({ seq + 1, nodeID: id })`。

工具轨、双击空白、右键空白、端口、T/I/O/A 最终都走 `createNode`，因此行为一致。

### 2.3 双击空媒体

`onNodeDoubleClick` 中，对没有内容的媒体节点不再调用 `onEdit`，改为调用 `onFocusPrompt(id)`，与上面共用 `promptFocus`。上传、资产库按钮和自动生成都不触发。卡片里「上传」「资产库」按钮自身的点击仍各自生效，双击按钮时事件不冒泡。

### 2.4 文案与快捷键

- `addMenuItems`：标题改为「生成下游节点」和「添加上游参考」；图片项说明改为「以上游文本为提示词」；音频项标注快捷键 `A`。
- `WorkspacePage` 的窗口 keydown 加 `a → audio`，守卫与 T/I/O 相同（输入框聚焦或按修饰键时不触发）。
- 帮助文案「按 T / I / O 直接新建」改为「按 T / I / O / A」。

## 3. 验证计划

- mock e2e `creative-node-create.e2e.mjs`：
  - AC-01：四种入口打开同一个菜单，菜单项相同；端口菜单的标题和说明。
  - AC-02：新建文本后出现卡片内编辑器且获得焦点；新建图片后焦点在生成输入框，用一个能确认请求的 mock 让节点经服务端确认后解锁；新节点放在组的范围里时 `parent_id` 仍为 null。
  - AC-03：T/I/O/A 在屏幕中心新建；输入框聚焦或按着 ⌘ 时不触发。
  - AC-04：五种双击行为，其中空媒体只聚焦、不发上传或生成请求。
- 另做回退有效性验证，并跑全部纯 mock e2e（`creative-canvas-interaction` 断言了菜单文本，需要同步修改）。
