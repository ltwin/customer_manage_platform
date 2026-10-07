---
doc_type: feature-verification
feature: 2026-10-06-node-create-focus
requirement: V6-006
status: passed
date: 2026-10-06
---

# V6-006 验证记录

设计：[node-create-focus-design.md](node-create-focus-design.md)。本条只改前端。

## 门禁

- `npm run lint`：退出码 0。
- `npm run build`：通过。
- `make test-frontend`：404/404。
- mock e2e：新增的 `creative-node-create` AC-01–04 全部通过。另外 10 个纯 mock 的创意空间 e2e 也全部通过：canvas-viewport、node-resize、node-label、save-status、canvas-interaction、canvas-baseline、composer-baseline、library、library-baseline、generation。
- 回归有效性：把 `CanvasView`、`WorkspacePage`、`NodeGenerationPanel`、`PromptEditor` 换回 HEAD 后，AC-01–04 全部失败。AC-03 在 T 那一步就失败了，后面的 A 没有跑到，所以 A 另外单独验证：HEAD 版本的 `WorkspacePage` 按 A 不会新建节点。

修改的既有断言：新文本节点现在会直接进入编辑，所以 ⌘Z 在编辑器里只撤销输入的文字，不撤销画布操作。下面三处原来是「新建文本后马上 ⌘Z 撤销创建」，现在改为先按 Esc 退出编辑（内容没改，不会留下撤销步骤），再 ⌘Z：
- `creative-canvas-interaction`：右键新建文字、T 快捷键。
- `creative-canvas-baseline` AC-05：拖线到空白处新建文字。

另外，`creative-canvas-interaction` 里添加菜单的「音频」项多了快捷键 `A`，断言同步更新。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 四个入口打开同一个菜单；端口菜单标题；文本上游说明 | e2e AC-01：工具轨、双击空白、右键空白打开的菜单项完全相同（右键菜单以同样的 6 项开头）；文本节点输出端口的菜单标题是「生成下游节点」，「图片」项说明是「以上游文本为提示词」；输入端口的菜单标题是「添加上游参考」，没有这条说明 |
| AC-02 只选中新节点；文本立即编辑；媒体焦点交给生成输入框；不自动入组 | e2e AC-02：新建文字后只选中这一个节点，焦点在「正文」编辑器里；新建图片后，等服务端确认、生成面板解锁，焦点落到「提示词」输入框；在组框内部新建视频，发出的 `add_node` 不带 `parent_id` |
| AC-03 T/I/O 在屏幕中心新建；输入聚焦或按修饰键时不触发；A/L 用同一机制 | e2e AC-03：T/I/O/A 分别新建 text/image/video/audio 节点，卡片中心与屏幕中心重合；⌘I、⌥T 不会新建；在提示词输入框里打「tioa」也不会新建。L（链接）由 V6-009 AC-05 验收 |
| AC-04 双击按位置区分 | e2e AC-04：双击标题进入重命名；双击文本进入编辑；双击有内容的图片打开全屏预览；双击空图片只把焦点交给提示词输入框，不发命令、不调生成接口，也不打开文件选择；双击空白打开添加菜单 |

## 交接

- V6-009：菜单里的「链接 L」和 L 快捷键在 V6-009 交付。新建链接后进入元信息编辑，可以直接复用本条的 `onCreated` 回调。
- V6-010：A 快捷键已经接入。V6-010 AC-05 只需要验收「新建音频后焦点交给生成输入框」，本条的 e2e AC-03 已经覆盖了按 A 新建音频节点。

## 未验证

- 真实后端相关的 e2e 没有运行。
- 焦点切换的视觉表现（光标、焦点环）由 owner 走查。
