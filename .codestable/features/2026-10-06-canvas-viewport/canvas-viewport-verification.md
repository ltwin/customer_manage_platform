---
doc_type: feature-verification
feature: 2026-10-06-canvas-viewport
requirement: V6-002
status: passed
date: 2026-10-06
---

# V6-002 验证记录

设计：[canvas-viewport-design.md](canvas-viewport-design.md)。本条只改了前端。

## 实现中发现的既有缺陷

点画布空白处后，焦点停在 `<body>` 上，所以挂在 `.cc-flow` 上的画布快捷键（⇧1、⌘G、⌘A、Esc 等）都收不到按键，必须先点一下节点才能用。现在给 `.cc-flow` 加了 `tabIndex={-1}`，点空白处时焦点会落到画布上，并去掉了焦点描边。新增 e2e 的 AC-01（⌘0）和 AC-02（打开资产库后按 ⇧1）都在先点空白的情况下用快捷键，这一点由它们覆盖。

## 门禁

- `npm run lint`：退出码 0。
- `npm run build`：通过。
- `make test-frontend`：404/404 通过（新增 `creative-canvas-viewport.test.ts` 4 项）。
- mock e2e：新增的 `creative-canvas-viewport` 6 项全部通过（AC-01、02、03、03-invalid、03-garbage、04）。另外 9 个纯 mock 的创意空间 e2e 也全部通过：node-resize、node-label、save-status、canvas-interaction、canvas-baseline、composer-baseline、library、library-baseline、generation。
- 回归有效性：把 `CanvasView.tsx`、`workspace.css`、`WorkspacePage.tsx` 换回 HEAD 后，AC-01、AC-02、AC-04 失败。AC-03 系列仍然通过：恢复视口和非法值回退在改动前就成立（原来由 `fitView` 属性兜底），本条只是把它们统一到新的适应规则上，并补上验收。

修改的既有断言：`creative-canvas-baseline` AC-05 的拖线落点。按新规则适应画布后，画布放大到 110% 上限，源节点靠近右边缘，原来固定的 +160 偏移会落到窗口外。现在把落点限制在窗口内，断言本身不变。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 缩放 10%–400%；指针缩放；⌘± 以屏幕中心 1.2 倍 | e2e AC-01：⌘0 回到 100%；⌘+ 后倍率为 1.2，屏幕中心对应的世界点不动；⌘- 回到 1；连续放大停在 400%，连续缩小停在 10%；按住 ⌘ 滚动滚轮时以指针为中心缩放，指针下的世界点不动。单测 `zoom range is 10%–400%` |
| AC-02 适应画布含标题区、避开面板、20%–110% | e2e AC-02：⇧1 后倍率在 20%–110%，节点在左右边距内，标签区没有被顶部遮住；打开资产库后，⇧1 和缩放条「适应全部节点」都让节点落在抽屉右侧，倍率不超过 110%。单测覆盖 28px 标题区、只计根节点、倍率上下限、左右面板让位和窄屏边距 |
| AC-03 按账号/画布恢复；非法值回退适应；不进撤销栈 | e2e AC-03：没有存储的视口时执行适应；⌘+ 加滚轮平移后撤销按钮仍不可用；刷新后视口恢复。AC-03-invalid（存储 zoom=9）和 AC-03-garbage（存储值无法解析）都回退为适应画布 |
| AC-04 四种平移；单选有内容的节点时 Space 全屏；视口变化关闭菜单 | e2e AC-04：中键拖动、平移工具下左键拖动、未选中时按住 Space 拖动、滚轮滚动都能平移；滚轮滚动时添加菜单关闭；选中有内容的图片后按 Space 打开媒体预览，视口不动；选中文本节点后按 Space 不打开任何东西；平移不产生撤销步骤 |

## 未验证

- 助手面板打开时的右侧让位只有单测覆盖。打开助手需要 5179 端口的 QA 页面（`creative-agent.e2e.mjs`），本次没有在浏览器里验证。
- 触控板捏合：Playwright 没有原生的捏合手势，只验证了 ⌘ 加滚轮。xyflow 把捏合当作带 ctrlKey 的滚轮事件处理，与这条走同一条路径。
- 真实后端相关的 e2e 没有运行。
