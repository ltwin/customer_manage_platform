---
doc_type: issue-fix
issue: 2026-10-05-canvas-baseline-regression
status: confirmed
path: fast-track
fix_date: 2026-10-05
requirement: V6-055
tags:
  - frontend
  - creative-canvas
  - baseline-regression
---

# 画布基础交互基线回归验收与修复记录

## 1. 范围

[V6-055](../../requirements/creative-workspace-v6/items/V6-055.md) 是验证项，覆盖差距分析中未列为 GAP 的 PRD 编号：SH-06、CV-20/22/24/33/34/36/37/39、CV-A1。先用浏览器回归逐条验收 8 条 AC，实际不符合的再就地修复。

## 2. 首轮验收结果

新增 `frontend/scripts/creative-canvas-baseline.e2e.mjs`：全量 mock API、页面加载后离线，所有改动只留在本机，撤销走本地取消。每条 AC 使用新的 browser context，失败不会连锁影响后续 AC。

| AC | 首轮 | 结论 |
|---|---|---|
| AC-01 缩放条置灰、点百分比回 100% 且中心点不动 | 通过 | — |
| AC-02 选择规则 | 失败 | 真实缺口：在多选状态下单击其中一个节点的标题，多选被打散成单选 |
| AC-03 组标题选中、⌘A、Esc 逐层 | 失败 | 真实缺口：画布没有 ⌘A；端口菜单关闭后焦点留在端口按钮上，第二次 Esc 不清空选中 |
| AC-04 方向键移动 | 通过 | — |
| AC-05 端口加节点、拖到空白的虚线 | 通过 | 首轮失败是测试自身的定位问题，修正后通过，代码无需改动 |
| AC-06 连线规则 | 失败 | 真实缺口：引用环先画出来，等服务端返回通用的 `validation_failed` 才被拒，没有说明原因；重复连线、自环、连到组时静默不连，也没有提示 |
| AC-07 三种断线方式与双击提示 | 失败 | 真实缺口：双击连线断开时没有「⌘Z 可撤销」提示 |
| AC-08 删除节点子树 | 通过 | — |

## 3. 根因与修复

全部改动在前端 `CanvasView.tsx`，`WorkspacePage.tsx` 只接了一个提示回调。后端、OpenAPI、保存队列都没有改。

1. **AC-02**：节点标题的 `onClick` 固定调用 `selectNode`，会把选区重置为单个节点。修复后，如果被点的节点已经在多选集合里，只切换焦点节点，保留多选。修饰键的切换仍由 React Flow 处理。
2. **AC-03 ⌘A**：画布键盘处理器里没有这个快捷键。现在 ⌘/Ctrl+A 选中全部顶层节点和组，子节点随所在组计入，同时清掉选中的连线和打开的菜单。
3. **AC-03 Esc**：键盘处理器遇到焦点在按钮上就整体返回，所以焦点停在端口等按钮上时 Esc 失效。另外，旧逻辑一次 Esc 会同时关闭菜单并清空选中。现在按钮上只放行 Esc，并且分层处理：有浮层时先关浮层，没有浮层时再清空选中。
4. **AC-06**：新增 `connectionProblem` 本机预检，拦截自环、以组为端点、重复连线和引用环（沿已有连线做 DFS），通过画布提示说明原因，不再发出必然被服务端拒绝的请求。拖线松手在自己或组的标题上时，`connectionTarget` 不会命中它们，这种情况也补了原因提示。服务端校验保持不变。
5. **AC-07**：`onEdgeDoubleClick` 断线后提示「已断开连线，⌘Z 可撤销」。在中点双击时，第一下选中连线并出现剪刀按钮，第二下落在剪刀上；这种情况用 `event.detail >= 2` 识别为双击，给出同样的提示。

提示复用工作台已有的 `copyNotice` 区域（`aria-live="polite"`，带「知道了」按钮），没有新增 toast 组件。

## 4. 改动文件清单

- `frontend/src/creative-canvas/editor/CanvasView.tsx`：多选内单击、⌘A、Esc 逐层、连线预检与提示、双击断线提示；新增 `onNotice` prop。
- `frontend/src/creative-canvas/editor/WorkspacePage.tsx`：把 `onNotice` 接到 `setCopyNotice`。
- `frontend/scripts/creative-canvas-baseline.e2e.mjs`：新增 V6-055 的 8 条 AC 浏览器回归；`ONLY=AC-0x` 可以只跑指定 AC；有失败时退出码非 0。

## 5. 验证结果

- `npx tsc -b --noEmit`：通过。`npm run lint`：0 error，新文件没有新增 warning。`npm run build`：通过。
- `make test-frontend`：391/391 通过。
- `node scripts/creative-canvas-baseline.e2e.mjs`：8/8 通过，连续两次。
- 同时回归 `creative-canvas-interaction`、`creative-library`、`creative-generation` e2e，全部通过。
- 回归有效性：把 `CanvasView.tsx` 和 `WorkspacePage.tsx` 换回修复前的版本跑同一个 e2e，AC-02、03、06、07 失败，AC-01、04、05、08 通过，与首轮结论一致。

未验证：

- 撤销只验证了离线时的本机取消，没有覆盖已发出改动的服务端撤销。这条路径由已有的 `creative-canvas-commands` e2e（需要真实后端）覆盖，本次未运行。
- 拒绝「指向链接」的连线不在本条修复范围内，由 V6-009 统一处理链接节点不接收输入。本条 AC-06 只验证了文本作为下游、环、自环、重复、组端点。
- macOS 上 Ctrl+单击是系统的右键手势，e2e 在 darwin 上只检查 Shift/⌘，其他平台会额外检查 Ctrl。
- 没有做视觉走查。

## 6. 验收对应

| AC | 证据 |
|---|---|
| AC-01 | baseline e2e AC-01：撤销/重做空栈置灰；放大两次后点百分比，缩放回到 1，屏幕中心对应的画布点误差 <2px |
| AC-02 | baseline e2e AC-02：单击选中；Shift/⌘ 单击切换加选与取消；单击多选中的节点不打散；单击空白清空；选中连线清空节点，选中节点清空连线 |
| AC-03 | baseline e2e AC-03：点组内空白不选中组，点组标题选中组；⌘A 选中 a/b/c/g；第一次 Esc 只关端口菜单，第二次清空选中 |
| AC-04 | baseline e2e AC-04：→ 移 1px，Shift+↓ 移 10px；标题输入框内的方向键不移动节点；两次移动分别撤销 |
| AC-05 | baseline e2e AC-05：点击端口新建的节点在源节点右侧 120、垂直居中并自动连线；Enter/Space 打开菜单；拖到空白后虚线保留，取消则消失，选择类型后卡片左侧中点和新连线终点都落在松手点 |
| AC-06 | baseline e2e AC-06：文本节点有输入端口；环、重复、组端点、自环都不新增连线，并提示对应原因 |
| AC-07 | baseline e2e AC-07：剪刀按钮、Backspace、Delete、双击四种方式断线都能撤销，双击后提示「⌘Z 可撤销」 |
| AC-08 | baseline e2e AC-08：Delete 删除节点及相关连线，不弹确认，可撤销；Backspace 删除组时连同子节点删除，可撤销 |
