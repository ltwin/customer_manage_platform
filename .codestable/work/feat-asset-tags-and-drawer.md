---
type: feat
status: verified
commit_authorized: true
---

# 资产库：标签管理与画布抽屉重建

## 目标

owner 2026-10-12 提出两个问题：

1. 资产库没有完善的标签管理，至少要能在左栏新增、删除标签，并支持层级。
2. 画布里的资产库抽屉还是旧原型的交互，和独立资产库页差距较大。

继续使用 worktree `.worktrees/creative-workspace-redesign` 与分支 `feat/creative-workspace-redesign`。已在本分支提交，提交信息「feat: 资产库标签管理与画布抽屉复用独立页组件」。

## 拍板（owner 2026-10-12）

- **层级沿用「分类 → 标签」两层**，不做标签嵌套，父标签筛选与嵌套层数随之不再讨论。调研过 Eagle：它也是「标签群组 → 标签」的平铺模型，另有独立的标签管理页；我们暂不做独立管理页、多群组归属、合并、加星和排除筛选。
- **被引用的标签允许删除**：后端早已支持删除时从所有素材上摘掉，界面只需在删除前弹确认，说明后果。
- **抽屉复用独立页组件重建**，只额外保留画布相关能力。

已回写 PRD §11.3 #15、#16 与需求总表 D-07；V6-057 的 AC-02、03、04、08 按新交互修订后重新验收。

## 改动

### 标签管理（独立页左栏）

- 后端：`GET /creative/tags` 的每个标签多一个 `asset_count`，表示引用它的素材数（含回收站，正是删除会影响的范围）。契约与 Go/TS 生成物同步。删除行为不变。
- 前端新增 `assets/TagTree.tsx`，替换左栏原来只读的标签区：
  - 「标签」标题旁的 + 可以新建标签或新建分类。
  - 分类可以折叠，右键可新建标签、重命名、删除（标签归入「未分类」）。
  - 标签右键可重命名、改颜色、移到分类、删除；也可以把标签拖到分类上。
  - 名称在本机先查重，重名时留在输入框里提示。
- 删除确认写清后果：「N 项素材（含回收站）会去掉这个标签，素材本身保留。删除后无法撤销。」如果删掉的标签正在筛选条件里，会顺带移除这个条件。
- 命令构造集中在 `assets/tagCommands.ts`，有单测覆盖。

### 抽屉重建

- 新的 `editor/LibraryDrawer.tsx` 替换 1500 行的 `LibraryPanel.tsx`，与独立页共用以下部分：
  - 浏览区 `AssetBrowser`（新增 `drawer` 模式：不显示布局、缩放和检查器开关）
  - 卡片
  - 筛选：类型、颜色、画幅、评分、标签
  - 右键菜单 `assetMenu.tsx`
  - 查看浮窗 `AssetViewer`
  - 写入：`useActions` / `useOrganization`
  - 导入 `useAssetImport`
  - 资料库树：`LibraryTree`，加上从独立页抽出的 `useTreeCommands.ts`
- 交互：
  - 344px 的窄抽屉用顶部「视图」下拉切换视图和分组；加宽到 760px 后左侧显示资料库树，分组管理在树里进行。
  - 单击选中，双击、空格或回车打开浮窗。
  - Esc 依次清空关键词、清空选择、收起抽屉。
  - ⌘A 全选，数字键评分。
- 画布专属能力：
  - 卡片可以拖进画布；
  - 右键菜单第一项和多选底栏都有「放入画布」；
  - 填充模式（只列同类，点一份即填）和选参考模式保留；
  - 节点拖到树里的分组上，存入该组；拖到抽屉其他位置，存入当前分组。
- 删除的旧代码：
  - 组件：`LibraryPanel`、`LibraryEditor`、`OrganizationPicker`、`LibraryViewer`；
  - 交互：分组平铺预览、悬停大图、快速查看；
  - 「标签与分类」弹窗；
  - 画布页上的「新增资产」表单（由导入菜单承接）、分组撤销状态、抽屉加载期间阻塞画布的逻辑。
- 发现并修复的冲突：
  - 导入 hook 在 document 上监听粘贴，嵌进画布页会抢走画布自己的 ⌘V。现在新增 `paste` 开关，抽屉里关闭。
  - 删除分组的确认按钮从「确认」改为「删除分组」，和标签、分类的删除一致。

## 证据

- `make check-go PKG=./internal/creativelibrary/...`：build、lint、包测试通过，DB 测试断言了 `asset_count`。
- `make check-frontend` 退出 0，共 475 项单测；`make generate-check` 退出 0。
- 浏览器回归，都是纯 mock，跑在 Vite 5173 上：
  - `creative-assets.e2e.mjs`：40 组全部通过，其中新增 `tag-tree-manage`。删除分组的确认按钮名改为「删除分组」后，V6-018 两组重新跑通过。
  - `creative-library.e2e.mjs`：按新抽屉重写，11 步，连跑 4 次全部通过。
  - `creative-library-baseline.e2e.mjs`（V6-057）：按修订后的 AC 改写，10 项全部通过。
  - `creative-canvas-interaction`、`creative-canvas-viewport`、`creative-generation`、`creative-node-toolbar`、`creative-file-import`、`creative-projects`、`creative-home` 全部通过。
  - 其中 `creative-home` 的 mock 素材原先缺必填的 `revision`，抽屉在画布页加载这些素材时会报错，已补齐。
- 截图：`/tmp/creative-assets-qa/tag-tree.png`、`tag-delete-confirm.png`，`/tmp/creative-library-drawer.png`、`creative-library-drawer-wide.png`。

## 未覆盖

- `creative-media.e2e.mjs` 和 `creative-text-canvas.e2e.mjs` 需要真实后端，本轮没有跑。两份在独立资产库页上线（d169b20）之后就没再维护，`/creative/library` 部分本来就已经过时。本轮只把 `creative-media` 里抽屉卡片的选择器 `.cl-tile` 换成了 `.al-tile`。
- 删除标签目前没有撤销；以后要做「合并标签」或可撤销的删除，再单独讨论设计。
