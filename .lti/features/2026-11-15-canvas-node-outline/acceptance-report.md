# 画布节点树技术验收

验收对象：`feat/canvas-node-outline`，实现基线 `c8a3d6a1`，加本轮未提交的滚动修正、浏览器回归及验收资料。在功能分支工作树验证。LTI 的 12 个实施任务均已勾选。

## 规格

下表的「单测」指 `frontend/scripts/creative-canvas-outline.test.ts`，「浏览器」指 Chromium 上的 `frontend/scripts/creative-canvas-outline.e2e.mjs`。补测使用本地同一套 mock API fixture，无真实账号或外部服务调用。

| 验收项 | 结论 | 实现与证据 |
|---|---|---|
| AC-23be937b6caf：三张图片、一个分组及两个子节点，合计六节点 | 满足 | `outline.ts:buildOutline`；单测 groups nest 用确切六节点断言层级及计数，浏览器 structure 核对渲染与折叠。 |
| AC-83f0f47b6d83：预览与类型图标 | 满足 | `NodeOutline.tsx:Thumb`、`KindIcon.tsx:KindIcon`；浏览器 structure 验证图片、角色卡封面及文本图标路径；源码核对音频、链接、场景、分组使用类型图标，视频有缩略图时使用封面。 |
| AC-e257a80f1919：按创建时间倒序，移动和层叠不改顺序 | 满足 | `outline.ts:buildOutline`；单测 creation order 核对位置、层叠、同值 ID 和微秒精度。 |
| AC-4c6279676f57：撤销删除恢复原排序 | 满足 | 后端 `graph_state.go:persistObject` 与前端 `outline.ts:buildOutline`；PostgreSQL 集成测试 `TestNodeCreatedAtSurvivesEditsAndUndoRemove` 对比删除、撤销、重做后的原 UTC 时间；排序单测验证稳定排行。 |
| AC-31393bf1cd1c：未确认先显示，正常确认不跳位 | 满足 | `WorkspacePage.tsx:pendingNodes`、`queue.ts:stageIntent`、`outline.ts:buildOutline`；单测 unconfirmed nodes 对比多批本地投影与服务端确认排序。同批临时 ID 替换、并发新建的例外见 design。 |
| AC-e145eb245828：空画布的空态和控件 | 满足 | `NodeOutline.tsx:NodeOutline`；单测 empty canvas，浏览器补测 0 节点、搜索、筛选及清除后恢复。 |
| AC-3ba02190dda7：远端快照变更自动更新 | 满足 | `useCanvasSnapshot.ts:useCanvasSnapshot` → `WorkspacePage.tsx:view` → `NodeOutline.tsx:NodeOutline`；模拟服务端事件更新快照，树原地反映新增、改标题、移进分组。 |
| AC-1dae35947174：标题搜索、大小写及全半角 | 满足 | `outline.ts:buildOutline` 用 NFKC 归一，仅匹配显示标题；单测 search matches 验证 2 / 13 与 ＤＳＣ / dsc，浏览器 search 核对显示。 |
| AC-168c15022d1a：筛图片保留分组路径、不把路径计入 | 满足 | `outline.ts:buildOutline` 区分命中和路径；单测 type filter 与浏览器 search。 |
| AC-c4e04e74162d：无命中与一键清除 | 满足 | `NodeOutline.tsx:clearFilters` 清空搜索和类型并将焦点还给搜索框；单测 no hits、浏览器 search 和空画布补测。 |
| AC-39342286ed01：搜索清空恢复折叠 | 满足 | `NodeOutline.tsx:toggle` 和 `outline.ts:buildOutline`；浏览器 search 从折叠状态开始，清除筛选后验证折叠保留。 |
| AC-5a5efd949d72：选中并居中，只有放不下时缩小 | 满足 | `CanvasView.tsx:revealRef`、`viewport.ts:revealViewport`；浏览器 reveal 验证视口外节点与组内节点，单测验证保留倍率、缩小下限及面板遮挡。 |
| AC-edb150a3a3f2：框选高亮与自动滚动 | 满足 | `NodeOutline.tsx:NodeOutline` 按首个选中 ID 或列表变更滚动；同一列表结构下曾只高亮不滚动，修正前复现，修正后浏览器 canvas-scroll 通过。canvas-sync 另外验证选中折叠组成员时展开祖先、此后仍能手动折叠。 |
| AC-58eccfd0beb2：修饰键多选不移动视口 | 满足 | `NodeOutline.tsx:click` 与 `WorkspacePage.tsx:onSelection`；浏览器 multi 验证 Cmd/Ctrl、Shift 范围和筛掉锚点的退化行为。 |
| AC-8ec6aa606b6b：改名同步、快捷键撤销、归档只读 | 满足 | `NodeOutline.tsx:startRename`、`InlineNodeTitle`、`WorkspacePage.tsx:graphActions`；浏览器 rename 直接按 Cmd/Ctrl+Z，readonly 验证归档拒绝进入编辑。 |
| AC-731e1e0285fa：两个工具栏入口在同一面板切换 | 满足 | `WorkspacePage.tsx:togglePanel`、`LibraryDrawer.tsx`；浏览器 tabs 验证切标签时抽屉保持打开。 |
| AC-ad3375dd4902：放置、填充和选参考打开资产标签 | 满足 | `WorkspacePage.tsx:openLibraryTask`、`CanvasView.tsx:onOpenLibrary`；浏览器 tabs 验证填充，资产库浏览器回归验证放置，源码核对选参考沿用同一入口。 |
| AC-025923be9235：390 px 无横向滚动 | 满足 | `library.css`、`WorkspacePage.tsx:onSelect`；浏览器 mobile 实测页面滚动宽度和收起抽屉后节点边界。 |

## 设计与契约

- LTI 将 `backend/internal/creativeagent/canvas_tools_test.go` 标为计划外文件：它是 `read_canvas` 新字段的回归断言，随已列明的 `context.go` 改动。自动比对将 `CanvasView.tsx`、`KindIcon.tsx`、`nodeKind.ts` 的简称标为未命中；三个完整路径均在分支差异中。功能、迁移和生成物均在挂载点或其目录范围内。
- 节点初次出现时，`persistGraph` 在数据库事务中取一个时间供同批新对象使用，节点行和身份行写入同一个值；后续更新保持该值，恢复沿用仍在的身份行。撤销字段头未纳入时间，账号作用域及写命令不变。节点树由已有快照和本地投影派生，列表没有专用读取 API；可见的角色卡封面会用已有内容修订接口按需读图。
- LTI 报告 proposal、design 和 contracts 在批准后有修订。验收标准未改；需 owner 关注：迁移从原写的「不重写表」修正为「重写一次」，同批新节点显式共用时间，旧库补种身份保留节点原时间，内外助手的精简 `read_canvas` 同样返回创建时间，未确认排序及 760 px 以下点选时收起面板也已记入设计。验收轮补上实际滚动验证，并把本地排序的契约描述对齐实现。相关差异请 owner 与最终验收一起确认。
- API 和数据契约各有一个适用的 ADD：`CreativeCanvasNode.created_at`（必返、只读、UTC、微秒）与 `creative_graph_identities.created_at`。OpenAPI、Go、TS、工具 schema 均已纳入生成物校验；外部工具和内置助手投影均沿现有鉴权范围输出。无写入参数或错误码变化。
- Living Spec 的 `creative/canvas → 节点树` ADD 适用，不覆盖既有条目。8 个场景按表验证：分组（第 1 项）、新建倒序与撤销（第 3–4 项）、标题搜索（第 8 项）、类型分组路径（第 9 项）、点击定位（第 12 项）、画布选中反映（第 13 项）、可撤销重命名（第 15 项）、无命中（第 10 项）。

## 迁移

- PostgreSQL 容器中的 `TestCreativeGraphIdentityCreatedAtMigration` 在迁移前插入存量、已删除与待补种节点，验证 0084 up 后存量原值、待补种原值及已删除回退时间，先执行 down 并断言列消失。发布环境锁表耗时未实测，也未在生产库执行迁移。
- 新版本要求先跑 up。旧代码读取加列后的库不依赖该列；旧代码若在兼容窗口继续新增对象，身份行和节点行的两个默认时间可能不同，所以切换时应先停止旧写入，迁移成功后再恢复新写入。回退应用后可保留额外列；若要 down，先停止依赖该列的新代码。迁移前已删除且仍可撤销的节点没有原始时间可回填，恢复后使用迁移时刻。

## 检查实录

| 命令或场景 | 结果 |
|---|---|
| `make check-go` | 退出 0；Go 编译、静态检查（0 issues）和全量测试通过。`creativecanvas` 用时 31.047s，`creativeagent` 215.677s，`platform/store` 188.136s。后端验证期间只补了前端滚动代码和文档，Go 实现未改动。 |
| `make check-frontend` | lint（0 errors）与生产构建通过；默认并行单测未通过：`creative-scene-maps.test.ts` 断言结束后进程不退出（在干净 `develop` 上独立复现后终止）；`creative-scene-model.test.ts` 的稠密网格用例 2046 ms，超出 1500 ms 阈值，单独重跑通过（整条用例约 435 ms）。涉及的场景源码与测试在两分支一致。 |
| `cd frontend && node --test --experimental-transform-types --test-concurrency=1 --test-force-exit "scripts/*.test.ts" "scripts/*.test.mjs"` | 退出 0，全量 1133/1133 通过。定向画布、资产、保存队列等 10 个测试文件另跑 79/79 通过。 |
| `make generate-check` | 退出 0；OpenAPI 的 Go、TS、画布工具 schema 等生成物无漂移。 |
| 节点树 / 视口浏览器场景 | 节点树全部 10 个场景通过；视口回归 `ONLY=AC-02` 通过；额外验证了空画布和模拟远端事件刷新。 |
| 资产库浏览器回归 | 原脚本在功能分支和 `develop`（`dd142b03`）同时触发已有 Three.js 预加载断言。临时副本保留请求记录并继续执行，抽屉、批量放置、模型菜单、禁止拖入画布等其余断言均通过（5 个命令、20 次资产查询、无 pageerror）。原脚本未改。 |
| 设计静态扫描 | 严格扫描检出两处基线提示：既有 textarea 的 `resize: none` 由 CSS 选择器提供，扫描只看 JSX；工作台既有滚动条样式只有 WebKit 写法。两处源文件在 `develop` 与功能分支一致。本次沿用现有设计与滚动容器，无新样式。 |
| `git diff --check`；`git merge-tree --write-tree develop HEAD` | 分支差异无空白错误；已提交基线与当前 `develop` 的预合并无冲突，`develop` 新增的是独立 Epic 文档。实际合并门禁留待集成时在主检出执行。 |

`.lti/config.yaml` 未配置检查命令，`lti verify --run` 无命令可运行，以上验证由工作树直接执行。收到 owner 对本快照的验收结论后，再合入两条契约和一条 Living Spec 并结案。
