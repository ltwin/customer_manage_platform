# Design

## 设计上下文与关键决策

依据：`proposal.md`（2026-11-15 批准）与 owner 拍板的三项取舍——左侧面板「画布 | 资产」两个标签、只搜标题、按创建时间倒序。无 discovery 文档。

| 决定 | 采用 | 理由 / 备选 |
|---|---|---|
| 排序依据 | 服务端节点创建时间 `created_at` | 节点 ID 是随机 UUID，`z_order` 会被置顶层叠改变，都不能表示创建先后；`creative_nodes.created_at` 已有但快照没读出。 |
| 撤销删除后保留创建时间 | 在 `creative_graph_identities` 加 `created_at`，恢复节点行时用它 | 撤销删除走「删行再插行」（`graph_state.go:persistObject`），节点行的时间会被重置；身份行在删除后保留，是唯一跨越删除的载体。备选①把时间放进撤销链的字段头（`creativegraph.NodeFields`）：会改变全部已有字段头的哈希，`planChanges` 的完整性校验会对所有存量节点报 `ErrGraphIntegrity`，否决。备选②不保留，撤销后排最前：违背已批准验收，否决。 |
| 面板形态 | `LibraryDrawer` 继续做外壳，加标签；节点树是新组件放进它的主体 | 资产库抽屉已有宽度、收起、`#creative-library` 的视口避让（`viewport.ts:panelInsets`、`CanvasView` 的 `centerRef`）。复用外壳，这些行为两个标签自动一致。备选：抽出独立 `SidePanel` 外壳再把资产库降为主体——要动资产库 500+ 行的键盘与宽度逻辑，收益只是结构更整齐，本次不做。 |
| 节点树数据 | 纯函数从当前画布视图派生，不新增请求 | 画布快照已含全部节点（上限 500，`service.go:maxNodes`），规模下全量重算 O(n log n) 可忽略。 |
| 列表渲染 | 不做虚拟滚动 | 500 行以内的扁平 DOM 列表成本可接受；缩略图按可见性懒加载（复用 `MediaView.tsx:useNearViewport`）。若实测掉帧再加，不预支。 |

本次 proposal 的修订：原写「数据库不变」，调查撤销链后需要一列 + 回填，风险面加 `data-migration`，已 `lti revise` 并随本 design 一并请 owner 确认。

## 领域模型与业务规则

- **节点创建时间**：画布节点作为对象第一次出现在画布上的时刻。一个节点 ID 一生只有一个创建时间。
  - 不变：移动、缩放、改名、换内容、选版本、移入移出分组、删除后撤销恢复、重做。
  - 新时间：新建、复制 / 粘贴 / 派生出的新节点（新 ID）。
- **节点树行**（前端派生读模型，不持久化）：

```ts
type OutlineRow = {
  id: string
  depth: number            // 0 = 顶层
  node: CanvasNode
  isGroup: boolean
  expanded: boolean        // 仅分组有意义
  match: boolean           // 自身命中搜索/筛选；作为路径保留的分组为 false
}
type Outline = { rows: OutlineRow[]; total: number; matched: number | null } // matched=null 表示无搜索/筛选
```

- **排序**：同一父级下，未确认节点（只在本地投影中、不在最近一次服务端快照中）在最前；未确认与已确认节点各自按 `created_at` 倒序（精确到微秒，不经 `Date.parse` 截到毫秒），同值按 `id` 升序。分组与普通节点混排，同规则。
- **显示标题**：`metadata.title`，空时用画布同一兜底 `CanvasView.tsx:untitled(typeKey)`（「未命名图片」等）。搜索针对显示标题。
- **搜索匹配**：查询与标题都做 `NFKC` 归一后转小写，再做子串包含；查询去首尾空白后为空视为未搜索。
- **类型筛选**：可选值固定为 8 个已知类型（`core.image/video/audio/text/link/scene/character/group`），多选取并集；选了任意类型后，未知类型节点不显示。
- **可见集**：无搜索、无筛选时 = 全部节点（受折叠影响）。有任一条件时，命中节点 = 同时满足搜索与筛选的节点；可见 = 命中节点 ∪ 它们的全部祖先分组；祖先分组在此时一律展开（不改写用户的折叠状态）。
- **计数**：`total` = 画布全部节点数（含分组，不含不可见规则影响）；`matched` = 命中节点数（作为路径的分组不计）。底部文案「共 {total} 节点」或「{matched} / 共 {total} 节点」。
- **折叠状态**：组件内 `Set<groupId>`，按画布 ID 隔离，默认全部展开，不持久化。搜索/筛选期间不读写它，清空后原样生效（满足「清空后恢复折叠」）。

## 模块职责与协作

| 模块 | 职责 / 非职责 | 关键接口 | 代码位置 |
|---|---|---|---|
| 迁移 | 加列、回填 | — | `backend/internal/platform/store/migrations/0084_*` |
| 画布图状态 | 读写身份行的创建时间；恢复节点时带回 | `identity.CreatedAt`、`loadGraph`、`persistObject` | `backend/internal/creativecanvas/graph_state.go` |
| 节点读出 | 节点行多读 `created_at`，序列化到 API | `nodeColumns`、`scanNode`、`Node.CreatedAt`、`Node.MarshalJSON` | `graph_model.go`、`service.go:Node`、`node_view.go` |
| 契约 | `CreativeCanvasNode.created_at` | OpenAPI → Go/TS/工具 schema 生成 | `api/openapi.yaml` + 生成物 |
| 节点树派生 | 排序、搜索、筛选、计数、扁平化；纯函数，无 React | `buildOutline(nodes, opts)` | 新 `frontend/src/creative-canvas/editor/outline.ts` |
| 节点树界面 | 搜索框、类型筛选、行渲染、折叠、多选、重命名、空态、计数 | `<NodeOutline …/>` | 新 `NodeOutline.tsx`、`outline.css` |
| 面板外壳 | 标签切换；资产主体保持挂载（隐藏而非卸载）；资产库键盘逻辑只在资产标签生效 | `LibraryDrawer` 新增 `tab`、`onTab`、`outline` | `LibraryDrawer.tsx` |
| 工作区编排 | 面板开关与当前标签、工具栏「节点树」按钮、把选中/重命名/定位接到节点树 | `panelTab` 状态 | `WorkspacePage.tsx` |
| 画布定位 | 暴露「把视口带到节点」 | `revealRef`（同 `centerRef` 模式）；`viewport.ts:revealViewport` | `CanvasView.tsx`、`viewport.ts` |

依赖方向：`NodeOutline` → `outline.ts`、`api.ts` 类型；`WorkspacePage` 连接 `NodeOutline` 与 `CanvasView`；`NodeOutline` 不 import `CanvasView`（`KindIcon`、`untitled` 搬到共享文件，见挂载点）。

主流程：

```
快照/本地投影 view.nodes ──► buildOutline(nodes,{query,types,collapsed,pendingIDs}) ──► 行列表
点击行 ──► onSelection([id]) ──► 画布选中
        └► revealRef.current(id) ──► revealViewport ──► flow.setViewport(…, 240ms)
双击行 ──► InlineNodeTitle ──► onActions([update_metadata]) ──► 与画布改名同一命令与撤销链
画布选中变化 ──► 节点树高亮；首个选中行在折叠分组内则展开其祖先；scrollIntoView({block:'nearest'})
```

## 关键机制

### 恢复节点时保留创建时间（后端）

```
loadGraph:   身份行多读 created_at → identity.CreatedAt
persistObject(kind=node, live):
  if 节点行不存在:  INSERT … created_at = i.CreatedAt      // 首次创建、撤销恢复、重做新建都走这里
  UPDATE …（不触碰 created_at）
新身份：同一次变更里第一次需要时取一次 tx.CreativeNow()（数据库 clock_timestamp），
        该变更新建的所有对象共用；身份行 INSERT 显式写入同一值
```

- `persistObject` 在同一事务里先写节点行、后写身份行；两者若各取默认值会相差约 1–2 ms，撤销恢复后的时间就和最初读到的不相等（实施时测试抓到），所以显式共用一个时间。
- 重做「新建节点」= 身份已存在、节点行不存在，同样取身份时间——重做后仍是原来的创建时间，符合「一个 ID 一个创建时间」。
- `scanNode` 被画布各处共用；多读一列只影响 `Node` 结构，`nodeStateJSON` 显式列字段、不含 `created_at`，撤销链字段头与哈希不变。

### 排序与未确认节点（前端）

- `pendingIDs` = 当前视图节点 ID − 最近一次服务端确认快照的节点 ID，由 `WorkspacePage` 从已有的确认快照与本地投影求差（具体变量在任务 1 定位）。
- 本机意图入队时记 `stagedAt`（`queue.ts:stageIntent`）；本地投影构造节点处（`outbox.ts`、`structuralPreview.ts`）用 `localCreatedAt(intent)` 填 `created_at`，同一次修改建的节点共用一个时间（旧草稿没有 `stagedAt` 时取 1970 年）。服务端给同一次变更的节点同一时间，所以一批节点在确认前后都按 `id` 升序，不跳位；不同批之间新的在前。
- 确认后该节点离开 pending，按服务端时间排序；它通常是最新的，位置不变。复制、编组的预览 ID 在收到回执后换成服务端 ID，同批内按新 ID 重排，这一次换序发生在回执时、属于已知限制。若确认前另一处（助手 / 其他标签页）恰好新建了更晚的节点，确认后会排在其后——这是正确顺序，不视为跳动缺陷。

### 视口定位

`revealViewport(rect, stage, insets, current): Viewport`（纯函数，`viewport.ts`）：

| 条件 | 结果 |
|---|---|
| 节点在当前倍率下能完整放进可见区（去掉 insets） | 倍率不变，平移使节点中心落在可见区中心 |
| 放不下 | 倍率 = `min(可见宽/节点宽, 可见高/节点高)`，下限 `zoomLimits.min`，且不大于当前倍率；居中 |

- 节点矩形：`viewport.ts:nodeRect`，世界坐标用 `graph.ts:worldPoint`（分组内节点是相对坐标），上方加标签高度 28px（与 `nodeBounds` 一致）。
- insets 沿用 `panelInsets`，读 `#creative-library`（外壳 id 不变）与助手面板，所以面板开着时节点不会被盖住；适应画布与定位共用 `CanvasView.tsx:coveredInsets` 的测量。
- 窄屏（≤ 760px，`viewport.ts:narrowLayout`）的抽屉是盖住大半画布的底部浮层：单击一行先收起抽屉再定位；抽屉只隐藏不卸载，搜索与筛选保留。
- Cmd/Ctrl/Shift 点击只改选中，不定位；Shift 的锚点被搜索或筛选隐藏时只选中这一行，同样不定位。节点 ID 不在画布上（刚被删除）时 `revealRef` 静默返回。

### 选择语义

- 单击：`onSelection([id])` + 定位。
- Cmd/Ctrl 单击：在当前选中里切换该 ID，锚点设为它。
- Shift 单击：从锚点到它，按**当前可见行顺序**取区间（与资产库 `assets/selection.ts` 的区间语义一致）。
- 选中分组行 = 选中分组节点本身（与画布点分组框一致），不连带子节点。

### 重命名

- 双击行标题且画布可编辑、节点 `capabilities.actions` 含 `rename` 时，行内换成 `InlineNodeTitle`（`InlineNodeEditor.tsx`），保存走 `update_metadata`（与 `CanvasView.tsx:saveTitle` 同一动作和撤销链），Esc / 失焦行为沿用该组件。
- 同一节点在画布上也处于重命名时，`InlineNodeTitle` 失焦即保存并关闭，所以焦点进入节点树的输入框时画布那一处自然结束，不需要额外协调。

### 面板与标签

- `WorkspacePage` 新增 `panelTab: 'canvas' | 'assets'`，与现有 `libraryOpen` 组合：
  - 工具栏「节点树」：未开 → 打开并切 `canvas`；已开且在 `canvas` → 关闭；已开在 `assets` → 切 `canvas`。「资产库」按钮对称。
  - 由节点操作唤起的资产库任务（`openLibraryTask`：填充 / 选参考）强制切 `assets`。任务进行中页眉照旧只显示任务标题（如「选择图片」），不显示标签，因而不能切到节点树；任务结束恢复标签。工具轨点「节点树」会先取消任务再切过去。
  - 无画布（资产库独立视图，`is-library`）时不显示标签，行为同现状。
- `LibraryDrawer`：头部原 `<h2>{title}</h2>` 处换成标签组（`role="tablist"`），`assets` 时其余头部按钮照旧；`canvas` 时只保留收起按钮、宽模式强制关闭。资产主体用 `hidden` 隐藏而非卸载，切回时筛选与滚动位置保留。根元素 `onKeyDown` 的资产库快捷键（Esc 清选、⌘A、数字评分）只在 `assets` 标签生效；`canvas` 标签下 Esc 只收起面板。
- 打开节点树时焦点放到搜索框；方向键在行间移动焦点，Enter 等同单击，F2 等同双击（无障碍：`role="tree"` / `treeitem`、`aria-expanded`、`aria-selected`、`aria-level`）。

### 行内容

- 缩略图：图片节点用 `MediaView.tsx:useMediaURL(revision, media, 'thumbnail', near)`，`near` 来自 `useNearViewport`；视频有海报渲染时同法取，没有则图标；角色卡取 `node.character.cover_revision_id`（没有时取卡片第一张参考图）的内容修订作封面，经 `versionCache.ts:useContentRevision` 读取——不可变内容按 id 缓存、并发合并，滚到附近才读；其余类型 `KindIcon`。取图失败退回图标，不显示错误。
- 状态标记：`labelMeta.ts:generationPhase` 非空 → 「生成中」圆点；`labelMeta.ts:generationFailure` 非空 → 「失败」标记。与画布卡片同一判定。
- 空态：画布无节点「画布上还没有节点」；有条件无命中「没有匹配的节点」+「清除筛选」按钮（清空搜索与类型）。

## 接口与数据契约

- 契约状态：**扩展**（只增字段）。
- 变更：见本 Feature `contracts.md`——`api: ADD 画布节点的创建时间`、`data: ADD 画布对象的创建时间`。OpenAPI 改动后 `make generate`，提交 Go / TS 两份生成物与 `tool_schemas.gen.go`。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/platform/store/migrations/0084_creative_graph_identity_created_at.up.sql` | 新增 | 加列 + 回填 |
| `backend/internal/platform/store/migrations/0084_creative_graph_identity_created_at.down.sql` | 新增 | 删列 |
| `backend/internal/platform/store/` 迁移回退链测试 | 修改 | 回退链补上 0084（同 0082、0083 的做法） |
| `backend/internal/platform/store/creative_graph_migration.go:seedCreativeGraphIdentities` | 修改 | 补建身份行时写入节点行的 `created_at` |
| `backend/internal/creativeagent/context.go:canvasNodeContext` | 修改 | 助手 `read_canvas` 节点投影加 `created_at` |
| `backend/internal/creativecanvas/graph_state.go:identity` | 修改 | 加 `CreatedAt` |
| `backend/internal/creativecanvas/graph_state.go:loadGraph` | 修改 | 身份查询多读 `created_at` |
| `backend/internal/creativecanvas/graph_state.go:persistObject` | 修改 | 恢复节点行时带回创建时间 |
| `backend/internal/creativecanvas/graph_model.go:nodeColumns` | 修改 | 加 `created_at` |
| `backend/internal/creativecanvas/graph_model.go:scanNode` | 修改 | 扫描 `created_at` |
| `backend/internal/creativecanvas/service.go:Node` | 修改 | 加 `CreatedAt time.Time`（`json:"-"`，由 `MarshalJSON` 输出） |
| `backend/internal/creativecanvas/node_view.go:MarshalJSON` | 修改 | 输出 `created_at`（UTC RFC 3339） |
| `backend/internal/creativecanvas/tool_schemas.gen.go` | 生成 | 随 OpenAPI 再生成 |
| `api/openapi.yaml` | 修改 | `CreativeCanvasNode.created_at`（required） |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `frontend/src/creative-canvas/editor/outbox.ts` | 修改 | `Intent.stagedAt`；本地投影节点填 `created_at` |
| `frontend/src/creative-canvas/editor/structuralPreview.ts` | 修改 | `localCreatedAt`；编组、复制预览节点填 `created_at` |
| `frontend/src/creative-canvas/editor/queue.ts:stageIntent` | 修改 | 入队时记 `stagedAt` |
| `frontend/src/creative-canvas/editor/versionCache.ts:useContentRevision` | 新增 | 按 id 缓存读取内容修订（角色卡封面） |
| `frontend/src/creative-canvas/editor/outline.ts` | 新增 | `buildOutline` 纯函数 |
| `frontend/src/creative-canvas/editor/NodeOutline.tsx` | 新增 | 节点树组件 |
| `frontend/src/creative-canvas/editor/outline.css` | 新增 | 节点树样式 |
| `frontend/src/creative-canvas/editor/nodeKind.ts` | 新增 | 从 `CanvasView.tsx` 移出 `nodeKind`、`untitled`、`kindLabel`，两处共用 |
| `frontend/src/creative-canvas/editor/KindIcon.tsx` | 新增 | 从 `CanvasView.tsx` 移出 `KindIcon`（组件单独成文件，满足 fast-refresh 规则） |
| `frontend/src/creative-canvas/editor/CanvasView.tsx` | 修改 | 改为引用 `nodeKind.ts`、`KindIcon.tsx`；新增 `revealRef` prop |
| `frontend/src/creative-canvas/editor/viewport.ts:revealViewport` | 新增 | 定位算法 |
| `frontend/src/creative-canvas/editor/viewport.ts:nodeRect` | 新增 | 单个节点的世界坐标矩形（含标签） |
| `frontend/src/creative-canvas/editor/viewport.ts:narrowLayout` | 新增 | 窄屏断点常量，`panelInsets` 与窄屏收起抽屉共用 |
| `frontend/src/creative-canvas/editor/LibraryDrawer.tsx` | 修改 | 标签头、`tab`/`onTab`/`outline` props、按标签分派键盘与主体 |
| `frontend/src/creative-canvas/editor/library.css` | 修改 | 标签样式 |
| `frontend/src/creative-canvas/editor/WorkspacePage.tsx` | 修改 | `panelTab`、工具栏「节点树」按钮、连接选中/重命名/定位、`pendingIDs` |
| `frontend/scripts/creative-canvas-outline.test.ts` | 新增 | `buildOutline`、`revealViewport` 单测 |
| `frontend/scripts/creative-canvas-outline.e2e.mjs` | 新增 | 节点树浏览器场景 |
| `backend/internal/creativecanvas/` 撤销恢复测试（现有 graph 测试文件内） | 修改 | 新增「删除后撤销保持创建时间」用例 |

## 验证与交付

| 验收 / 不变量 | 方法 | 完成判据 |
|---|---|---|
| 创建时间读出、撤销删除与重做后不变、移动/改名不变 | Go 集成测试（`check-go PKG=./internal/creativecanvas/...`） | 新建 → 删除 → 撤销 → 重做删除 → 撤销，`created_at` 全程相等；移动、改名后相等；复制得到更晚的新时间 |
| 迁移回填 | 迁移测试（`internal/platform/store`） | 回填后存量节点身份行时间 = 节点行时间；`down` 后列消失 |
| 字段头哈希未变 | 现有 graph / undo 测试全过 | 不出现 `ErrGraphIntegrity` |
| 排序、搜索（NFKC/大小写）、类型筛选、祖先路径、计数、pending 置顶、空态 | `creative-canvas-outline.test.ts` | proposal 中对应 AC 的输入输出逐条有用例 |
| 定位算法 | 同上单测 | 能放下时倍率不变且居中；放不下时缩小且不低于下限；insets 生效 |
| 点击定位、画布框选高亮并滚动、Cmd 多选不移动视口、双击改名 + 撤销、标签切换、资产库任务强制资产标签、390px 无横向滚动 | `creative-canvas-outline.e2e.mjs`（mock API，单用例逐个跑，换端口） | 各断言通过，无 `pageerror`；长列表中用真实框选手势确认首个选中行进入可见区 |
| 资产库回归 | 现有 `creative-library.e2e.mjs`、`creative-canvas-viewport.e2e.mjs` 中涉及 `#creative-library` 的用例 | 通过；只跑这两个文件里受影响的用例 |
| 前端门禁 | `make check-frontend` | 通过 |

交付顺序：后端迁移与字段 + OpenAPI 生成 → 前端纯函数与单测 → 面板与节点树界面 → e2e。全部在 `feat/canvas-node-outline` 分支，单 feature 一次合入 `develop`，按 `check-merge` 门禁。回滚：前端回滚即隐藏功能；新列保留无害，必要时执行 `down`。
