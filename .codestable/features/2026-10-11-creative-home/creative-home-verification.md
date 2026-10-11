---
doc_type: feature-verification
feature: 2026-10-11-creative-home
requirement: V6-061、V6-062、V6-063、V6-064（首页与资产库改版第 1 期）
status: in-progress
date: 2026-10-11
---

# 创意空间首页第 1 期验证记录

设计：[creative-home-design.md](creative-home-design.md)。按 §8 的顺序逐条追加，每条一节。

## V6-061 侧栏与路由

### 实现

- 新目录 `frontend/src/creative-canvas/home/`：`AppSidebar.tsx` + `sidebar.css`（侧栏），`HomePage.tsx` + `home.css`（首页外壳），`projectList.ts`（最近项目读取、状态展示规则、画布链接、缩略图取第一张图片）。
- 路由：`/creative` → `HomePage`；`/creative/projects` → `ProjectsPage`（从 `editor/` 搬来）；`/creative?view=archived` 跳转到 `/creative/projects?view=archived`。画布页的「返回创意空间」和「所有项目」仍然指向 `/creative`。
- 首页只有侧栏和「继续创作」：取同一次 `GET /projects?limit=5` 的前 4 个，显示名称和状态标签，封面先是空白点阵。一句话开始（V6-062）、封面缩影与轮询（V6-063）在后续提交里补上。
- 项目页去掉了营销式标题、头部导航和页脚，只保留列表区块，标题改为 `h1`。列表本身（新建占位卡、标签、加载更多）由 V6-064 重写。

### 实现偏差

- 名称提示的横坐标取侧栏右边缘加 8px，而不是原型里的条目右边缘加 8px。折叠后条目右边缘在侧栏内 10px，按原型算提示会盖住侧栏边线 3px。
- 侧栏切回标签页时也会重读最近项目（只在首页以外的页面；首页的重读由页面负责）。设计只写了首页和项目页的重读，侧栏顺带做了，避免状态点停在旧值。
- 资产库页面仍然是画布外框，没有接侧栏。它由 V6-067 重做，届时接入。

### 门禁

- `make check-frontend`：lint 通过、build 通过、单测 431/431。
- mock e2e：新增 `scripts/creative-home.e2e.mjs`，AC-01–05 全部通过。
- 回归有效性（变异）：
  - 让侧栏在首页也自己读一次：AC-04「首页与侧栏共用一次读取」失败。
  - 把展示规则改成运行中优先：AC-04 状态点断言失败。
  - 恢复后全部通过。
- 改了三个需要真实后端的 e2e 入口（`creative-media`、`creative-text-canvas`、`creative-canvas-commands`）：从 `/creative` 改为 `/creative/projects`，因为「开启新的创作」占位卡现在在项目页。本次没有运行这三个脚本（需要真实后端和账号）。

### 验收对应

| AC | 证据 |
|---|---|
| AC-01 展开 216 / 折叠 56，图标位置不变，偏好记在本机，存储不可用时默认展开 | e2e AC-01：宽度 216 → 56；导航图标、缩略图和返回图标的横坐标在折叠前后完全相同；`creative-side-collapsed` 写入 `1`；进入项目页后仍是 56；展开后写回 `0`。`Storage.prototype.getItem/setItem` 都抛错时默认 216，仍能切换，页面没有报错 |
| AC-02 折叠时悬停或聚焦显示名称提示，不被裁切 | e2e AC-02：悬停导航「项目」、最近打开的长名称项目，以及键盘聚焦「资产库」，都出现 `role=tooltip`，文字正确，左边缘在侧栏右边缘之外，宽度完整；移开后消失；展开状态下不出现 |
| AC-03 761–1100 自动折叠、隐藏切换按钮、不改偏好；≤760 顶栏；无横向滚动 | e2e AC-03：1280、1000、700、390 四种宽度下，首页和项目页的文档和主区 `scrollWidth ≤ clientWidth`。1000 宽时 56px，切换按钮不可见。700、390 宽时侧栏是高度不到 64px 的整行顶栏，品牌和三个入口可见，返回 CRM 和最近打开隐藏。偏好始终保持 `0` |
| AC-04 当前页高亮带 `aria-current`；最近打开 5 个，按最近编辑排序，状态点，点击进入画布 | e2e AC-04：首页「首页」、项目页「项目」带 `aria-current=page`，另一项没有。首页只完成一次 `/projects?limit=5` 读取（侧栏与「继续创作」共用）。5 个条目顺序和链接与接口返回一致。状态点：仅运行中是琥珀色 `rgb(226,164,83)`，等你确认是白色，两者都有时显示白点（D3），空闲没有点。只有带图片的项目有缩略图。点击条目进入 `/creative/canvases/c4`。`edited_at` 排序本身由第 1 步的 storetest `TestProjectListSearchSortAndCursor` 覆盖。真实的白点要等 V6-031、V6-033 产生等待中的运行，本条只用 mock 验证渲染 |
| AC-05 导航加条目时最近打开下移，只在自己的区域滚动；底部是返回 CRM 和账号区 | e2e AC-05：720 高的窗口里，把导航加到 12 项后，最近打开的顶边下移且在导航之下；它自己的列表 `scrollHeight > clientHeight`，侧栏整体不溢出；底部返回 CRM 和账号区（头像首字 S + Samson，不显示点数）仍在屏幕内 |

### 浏览器证据

截图在 `/tmp/creative-home-qa/`：

- `side-expanded-1280.png`、`side-collapsed-1280.png`：展开和折叠。
- `side-tip.png`：折叠时聚焦「资产库」的提示。
- `side-1000.png`：强制折叠。
- `side-700.png`、`side-390.png`：顶栏。
- `side-crowded.png`：导航 12 项时最近打开的区域内滚动。

已逐张看过 1280 展开、折叠提示、390 顶栏三张，布局与原型一致。

### 未验证

- 需要真实后端的三个 e2e 脚本，本次没有运行。
- 没有在真实账号数据上走查。
