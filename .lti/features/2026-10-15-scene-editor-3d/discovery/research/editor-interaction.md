# 3D 编辑器交互调研：以 Blender 为蓝本，在浏览器里怎么落地

调研日期：2026-10-16。触发：C1 开工前，owner 希望先调研 3D 编辑器交互的最佳实践，个人倾向模仿 Blender。

## 1. 问题

1. Blender 的交互由哪些部分组成，哪些是核心习惯？
2. 搬到浏览器里，哪些能照搬，哪些受限？
3. 目标用户是 cosplay 摄影师，多数不会 Blender、常用 MacBook 触控板。照搬 Blender 时，怎样降低上手门槛？
4. 对 C1 的设计和后续子项有什么影响？

## 2. 来源

| 来源 | 内容 | 可信度 |
|---|---|---|
| Blender 手册源文件（projects.blender.org/blender/blender-manual，main 分支）：`interface/keymap/blender_default`、`editors/3dview/navigate/navigation`、`.../transform/control/axis_locking`、`precision`、`numeric_input`、`editors/preferences/input`、`getting_started/configuration/hardware`、`interface/keymap/industry_compatible`、`interface/window_system/status_bar` | 默认键位、导航、变换模态、触控板手势、模拟中键、状态栏 | 一手 |
| Blender 源码 `scripts/presets/keyconfig/keymap_data/blender_default.py`（github.com/blender/blender，main） | `km_object_mode`、`km_view3d`、`km_transform_modal_map`、`_template_items_transform_actions` 的实际绑定 | 一手 |
| Spline 文档 `docs.spline.design/cameras/working-with-cameras` | 网页 3D 设计工具的视口导航（鼠标 / 触控板分列） | 一手 |
| `yomotsu/camera-controls` README 与示例（context7） | drei `CameraControls` 底层库的按键与手势映射能力 | 一手 |
| MDN `WheelEvent` / `ctrlKey`；WebKit bug 145214、230545；W3C public-webapps 2016-01 邮件；Firefox bug 1203059 | 浏览器滚轮、捏合、保留快捷键 | 一手，但部分较旧，需实测 |

`docs.blender.org` 对自动访问有人机验证，所以改读手册源文件，内容与线上手册相同。

## 3. Blender 交互的组成

以 2.80 之后的默认键位（左键选择）为准。

### 3.1 视口导航

| 操作 | 鼠标 | 触控板（系统级手势） | 键盘 |
|---|---|---|---|
| 环绕 | 中键拖动 | 双指拖动 | Numpad 2/4/6/8 步进 |
| 平移 | Shift + 中键拖动；Shift / Ctrl + 滚轮 | Shift + 双指拖动 | Ctrl + Numpad 2/4/6/8 |
| 缩放 | 滚轮；Ctrl + 中键拖动 | 捏合；Ctrl（macOS 为 Cmd）+ 双指拖动 | Numpad +/- |
| 标准视图 | — | — | Numpad 1/3/7 前/右/顶，加 Ctrl 为反方向；Numpad 5 正交/透视；Numpad 0 相机视图 |
| 取景 | — | — | Numpad . 框选中物件；Home 框全部 |
| 导航饼菜单 | — | — | `（反引号） |

- **没有中键时**：可开启「模拟三键鼠标」，用 Alt + 左键代替中键，Shift / Ctrl 组合照旧。Blender 默认不开，因为 Alt + 左键另有用途（批量改值等）。
- **没有小键盘时**：可开启「模拟小键盘」，用数字行代替。Blender 默认不开，因为数字行在编辑模式里切换点线面。
- 中键或缩放进行中，右键取消。

### 3.2 选择

- 左键单击选中，Shift + 单击加选或取消，在空白处单击取消选择。
- A 全选，Alt + A 或双击 A 取消全选，Ctrl + I 反选。
- 默认工具是「框选」：左键拖动一律拉出选框，在物件上拖动也一样；Shift + 拖动加选，Ctrl + 拖动减选。工具键位优先于物件模式键位（`km_3d_view_tool_select_box` 把左键拖动绑给 `view3d.select_box`）。
- 「调整（Tweak）」工具：单击选择，拖动物件就是移动。W 在框选和调整等选择工具之间切换。

### 3.3 变换：Blender 最有辨识度的部分

- **快捷键进入模态**：G 移动、R 旋转、S 缩放。按下后不用按住鼠标，物件跟随鼠标移动。
  - 确认：左键、回车、空格。
  - 取消：右键、Esc，物件回到原位。
- **轴向锁定**：模态中按 X / Y / Z 锁定到该轴；再按一次切到另一种坐标系（全局 / 局部），第三次解除。Shift + X/Y/Z 锁定到「排除该轴的平面」。按住中键可以按鼠标方向自动选轴。
- **精度**：
  - 按住 Ctrl 吸附：移动按 1 单位，旋转按 5°，缩放按 0.1；
  - 按住 Shift 慢速：1/10 速度；
  - Ctrl + Shift 细吸附：0.1 单位、1°、0.01。
- **数字输入**：模态中直接键入数字。例如 `G Z 2 回车` 是沿 Z 移动 2；`S 2 回车` 是放大 2 倍。小数点、负号（Minus）、Backspace 回退、Tab 切下一个轴。
- **拖动即移动**：物件模式键位把左键拖动绑给了移动（`transform.translate` 绑在选择键的 CLICK_DRAG 上），但默认的框选工具会先接住左键拖动，所以只有在「调整」工具下拖动物件才是移动。
- **gizmo（操作手柄）**：只有在工具栏选了移动 / 旋转 / 缩放工具时才显示；拖动 gizmo 时同样可以用 Ctrl / Shift。
- **清除变换**：Alt + G / R / S 分别清除位置、旋转、缩放。

### 3.4 编辑与界面

| 操作 | 键位 |
|---|---|
| 添加菜单 | Shift + A（在鼠标处弹出） |
| 复制并立即移动 | Shift + D；Alt + D 是关联复制（共享网格数据） |
| 删除 | X（带确认）/ Delete（不确认） |
| 隐藏 / 隐藏未选 / 全部显示 | H / Shift + H / Alt + H |
| 父子关系 | Ctrl + P 设置父级，Alt + P 清除 |
| 集合 | Ctrl + G 新建集合，M 移到集合 |
| 重命名 | F2 |
| 菜单搜索 | F3 |
| 调整上一步操作的参数 | F9 |
| 撤销 / 重做 | Ctrl + Z / Ctrl + Shift + Z |
| 工具栏 / 侧边栏 | T / N |
| 右键 | 上下文菜单（左键选择模式下） |

- macOS 上 Cmd 可以代替 Ctrl，与系统冲突的少数组合除外。
- **悬停在数值框上**：Ctrl + C / V 复制粘贴值，Backspace 恢复默认值，Minus 取反，Ctrl + 滚轮步进。拖动数值框可以连续调值。
- **状态栏**：左侧显示当前鼠标按键和工具的快捷键提示；模态操作时中间显示当前数值（如移动量）。这是 Blender 2.8 之后降低学习门槛的关键设计。
- Blender 另外内置一套「Industry Compatible」键位（Alt + 左/中/右键导航，W/E/R 切换移动/旋转/缩放，Ctrl + D 复制），给 Maya 等软件的用户用。

## 4. 网页端的参照：Spline

Spline 是面向设计师的网页 3D 工具，走的是 Figma 式交互，不是 Blender 式：

| 操作 | 鼠标 | 触控板 |
|---|---|---|
| 环绕 | Option/Alt + 拖动 | Option/Alt + 轻点拖动 |
| 平移 | 空格 + 拖动 | 双指拖动 |
| 缩放 | 滚轮，或 Cmd/Ctrl + 滚轮 | 捏合，或 Cmd/Ctrl + 双指滑动 |

另有视口底部的视图助手（点轴切换标准视图、重置、等轴测），M 切换正交 / 透视。

**对比**：Spline 的触控板双指是**平移**，Blender 是**环绕**。设计工具用户（Figma、Lightroom、Photoshop）习惯双指平移；3D 软件用户习惯双指环绕。

## 5. 搬到浏览器的限制

| 限制 | 影响 | 处理 |
|---|---|---|
| **触控板双指滑动和鼠标滚轮都是 `wheel` 事件**，浏览器不区分来源。Blender 能区分，是因为它直接拿系统的 TRACKPADPAN 事件 | 照搬 Blender 时，双指 = 环绕，但鼠标滚轮也会变成环绕而不是缩放 | 需要「导航设备：鼠标 / 触控板」设置。可以先自动推测（触控板事件通常有横向分量和非整数小增量，鼠标滚轮多是 ±100/120 这类整格），并允许手动切换、本机记住。推测规则需实测 |
| **捏合**：Chrome / Edge / Firefox 在 macOS 上把捏合映射成带 `ctrlKey` 的 `wheel`；Safari 历史上用非标准的 `gesturechange` | 与真实的 Ctrl + 滚轮无法完全区分；Safari 行为要单独测 | 两种事件都处理；`ctrlKey` 的 wheel 一律当缩放（两者含义都是缩放，不冲突）；必须 `preventDefault`，防止整页缩放 |
| **保留快捷键**：Ctrl/Cmd + N、T、W 等由浏览器界面占用，网页拦不住；Cmd + Q、Cmd + H 由 macOS 占用 | Blender 的 Ctrl + N（新文件）等不可用；Cmd + H 不能当隐藏 | 编辑器不用这些组合。H、Alt + H 用 `event.code` 判断，避免 macOS 上 Option 产生特殊字符 |
| **F 键**：F1 帮助、F3 查找、F5 刷新、F11 全屏、F12 开发者工具 | Blender 的 F3、F9 用起来有风险 | 只用 F2（重命名）；菜单搜索以后另定快捷键 |
| **右键**默认弹出浏览器菜单；**中键**在 Windows 上触发自动滚动 | 右键取消、中键环绕会被浏览器抢走 | 视口内对 `contextmenu` 和中键 `pointerdown` 调用 `preventDefault` |
| **光标环绕**：Blender 在变换时会把光标从屏幕一边绕回另一边。浏览器只能用 Pointer Lock，需要用户手势；会显示「按 Esc 显示光标」横幅；Esc 会先被浏览器拿去解锁 | 与「Esc 取消变换」冲突 | 首版不用 Pointer Lock，变换范围受屏幕边界限制 |
| **中文输入法**：焦点在可输入元素时，字母键进入输入法 | 属性面板里打字时误触发 G / R / S | 焦点在输入框、`isComposing` 为真时一律不处理快捷键；视口获得焦点时才处理 |
| **macOS 修饰键** | Blender 在 macOS 上用 Cmd 代替 Ctrl | 撤销、重做、吸附等组合在 macOS 上认 `metaKey`，其他平台认 `ctrlKey` |

**`camera-controls` 的能力**（drei `CameraControls` 的底层）：
- 左、中、右键和滚轮都可以单独映射为环绕、平移、推拉、缩放或无；
- 可以按修饰键动态切换，官方示例就是 Shift 切平移、Ctrl 切推拉；
- 滚轮也可以映射为环绕（macOS 上滚轮事件带上下左右四个方向）。

所以 Blender 式导航（中键环绕、Shift + 中键平移、滚轮缩放）和触控板模式（双指环绕、Shift + 双指平移、捏合缩放）都能配出来，不需要自己写相机控制。G / R / S 模态变换 drei 没有现成组件，要自己实现，代码量中等：射线投影、轴向约束、数字输入缓冲、吸附。

## 6. 结论与建议

**总体**：采用 Blender 2.8+ 默认键位（左键选择）作为唯一键位，不做键位切换。在其上叠加三层上手辅助：状态栏提示、带快捷键标注的菜单和工具栏、快捷键速查。这正是 Blender 自 2.8 起降低门槛的做法，不另造一套交互。

**与 Blender 的必要偏差**（浏览器限制或产品差异）：
1. **导航设备设置**：鼠标或触控板，自动推测加手动切换。触控板模式按 Blender 的手势：双指环绕、Shift + 双指平移、捏合或 Cmd + 双指缩放。
2. **中键与 Alt + 左键并存**：中键照常使用；Alt + 左键（含 Shift、Ctrl 组合）作为没有中键时的替代，始终可用。编辑器里 Alt + 左键没有别的用途，所以不必像 Blender 那样做成开关。
3. **始终开启数字行模拟小键盘**：物件模式下数字行没有别的用途；模态变换里的数字输入只在模态期间生效，不冲突。笔记本用户因此可以直接按 1 / 3 / 7 切视图。同时提供可点击的视图导航 gizmo（drei `GizmoViewport`）。
4. **不做 3D 游标**：Shift + A 添加的物件落在鼠标所指的地面位置；鼠标不在视口时落在视图中心对应的地面。3D 游标概念对目标用户成本高、收益小。
5. **Esc 不关闭编辑视图**：Blender 用户习惯连按 Esc 取消，Esc 只取消模态和选择。关闭视图走顶栏按钮。
6. **Ctrl/Cmd + S**：拦下浏览器的保存网页，提示「场景会自动保存」。
7. **H 隐藏写入文档**：Blender 的 H 只在视口隐藏；本项目直接改物件的 `visible`，因此会保存，导出时也按此生效。
8. **F3 / F9 暂不提供**：浏览器占用 F3；菜单搜索和「调整上一步」以后再定快捷键。
9. **不做光标环绕**：首版不用 Pointer Lock。

**上手辅助**：
- **状态栏**（视口底部）：
  - 空闲时显示「左键选择 · 拖动框选 · 中键环绕 · G 移动 · Shift+A 添加」；
  - 选中物件时显示 G/R/S；
  - 模态中显示当前轴向和数值，如「移动 · Z 轴 · 2.000 m」，以及可用按键「X/Y/Z 锁轴 · Ctrl 吸附 · Shift 精细 · 输入数字 · 左键确认 · 右键取消」。
- **顶部菜单**：「添加」「物件」「视图」，每项右侧标注快捷键。
- **左侧工具栏**：框选（默认）、调整、移动、旋转、缩放。选移动 / 旋转 / 缩放时显示 gizmo，与 Blender 一致。
- **快捷键速查**：问号按钮或 `?` 键打开。
- **首次打开**：一条可关闭的提示，说明导航设备设置和三个最常用操作。

**不在 C1 做、但已有归属**：
- Ctrl + P / Ctrl + G 编组：C5。
- Numpad 0 相机视图、Ctrl + Alt + Numpad 0 相机对齐视图：C4。
- Alt + D 关联复制（共享模型）：C2。

## 7. 局限与待实测

- 触控板 / 鼠标的自动推测规则、Safari 的捏合事件、各浏览器能否拦住 Cmd + D 等组合，都要在 C1 实施初期用真机穿刺：macOS Chrome / Safari / Edge 加触控板和鼠标，Windows Chrome 加鼠标。
- 以上键位的「好用」与否没有用户测试证据。Blender 键位对新手的学习成本是公认的，本方案靠状态栏和菜单缓解，效果要 owner 试用后判断。

## 8. Owner 决定（2026-10-16）

1. **触控板用 Blender 式**：双指环绕，Shift + 双指平移，捏合或 Cmd + 双指缩放。
2. **多选放进 C1**：Shift + 单击加选或取消、A 全选、Alt + A 取消全选、框选；G / R / S 和删除、复制、隐藏都作用于全部选中物件。不再随编组放到 C5。
3. **默认工具照 Blender**：打开编辑器时是「框选」工具，不显示 gizmo；工具栏可切到调整、移动、旋转、缩放。
4. **中键和 Alt + 左键并存**：保留中键操作，Alt + 左键作为替代。

采纳的结论写入 epic `design.md`「编辑器交互约定」和 C1 `design.md`。

