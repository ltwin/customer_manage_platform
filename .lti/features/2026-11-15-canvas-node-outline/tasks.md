# Tasks

## 1. 节点创建时间（后端与契约）

- [x] 1.1 迁移 0084：`creative_graph_identities.created_at`（`NOT NULL DEFAULT clock_timestamp()`）+ 从 `creative_nodes` 回填，配 `down`；迁移测试覆盖回填值等于节点行时间、`down` 后列消失；迁移回退链测试补上 0084。`check-go PKG=./internal/platform/store/...` 通过
- [x] 1.2 先写红测：画布图集成测试「新建 → 删除 → 撤销 → 重做删除 → 撤销」期间节点 `created_at` 不变，移动 / 改名后不变，复制得到更晚的时间；当前因字段不存在或恢复被重置而失败
- [x] 1.3 `identity.CreatedAt`、`loadGraph` 读身份时间、`persistObject` 恢复节点行时带回；`nodeColumns` / `scanNode` / `Node.CreatedAt` 读出；1.2 转绿，现有 creativecanvas 测试全绿（无 `ErrGraphIntegrity`）
- [x] 1.4 OpenAPI `CreativeCanvasNode.created_at`（required，date-time）；`Node.MarshalJSON` 输出 UTC RFC 3339；`make generate` 更新 `api.gen.go`、`schema.d.ts`、`tool_schemas.gen.go`；前端 `outbox.ts`、`structuralPreview.ts` 本地投影节点补 `created_at`；`check-go PKG=./internal/creativecanvas/...` 与前端类型检查通过

## 2. 节点树派生与定位算法（纯函数）

- [x] 2.1 `nodeKind.ts` + `KindIcon.tsx`：从 `CanvasView.tsx` 移出 `KindIcon`、`nodeKind`、`kindLabel`、`untitled`，`CanvasView` 改为引用；行为不变，前端类型检查与现有画布单测通过
- [x] 2.2 先写 `creative-canvas-outline.test.ts`（红）再实现 `outline.ts:buildOutline`：created_at 倒序 + id 兜底、pending 置顶、分组嵌套与折叠、NFKC + 小写的标题搜索（含 `untitled` 兜底标题）、类型多选与未知类型排除、命中祖先路径强制展开且不改折叠集、`total` / `matched` 计数、空画布；单测全绿
- [x] 2.3 同一测试文件补 `viewport.ts:revealViewport` 用例（能放下时倍率不变并居中、放不下时缩小不低于下限且不放大、insets 生效）并实现；单测全绿

## 3. 面板与节点树界面

- [ ] 3.1 `LibraryDrawer` 标签化：`tab` / `onTab` / `outline` props，头部 `role="tablist"`，资产主体 `hidden` 保持挂载，资产库快捷键只在资产标签生效、画布标签下 Esc 只收起、宽模式只在资产标签；`library.css` 标签样式。无画布的独立资产库视图不显示标签。现有 `creative-library.e2e.mjs` 受影响用例通过
- [ ] 3.2 `NodeOutline.tsx` + `outline.css`：搜索框、类型筛选菜单、树行（缩略图懒加载 / 类型图标、标题、生成中与失败标记）、折叠、底部计数、两种空态与「清除筛选」、`role="tree"` 键盘导航（方向键 / Enter / F2）；前端类型检查与 lint 通过
- [ ] 3.3 `CanvasView` 新增 `revealRef`（世界坐标经 `worldPoint`，insets 读 `#creative-library` 与助手面板，未知 ID 静默返回）；`WorkspacePage` 接线：`panelTab` 状态、工具栏「节点树」按钮的开关 / 切换语义、资产库任务强制资产标签并禁用画布标签、`pendingIDs` 求差、单击选中 + 定位、Cmd/Ctrl 切换、Shift 按可见行区间、画布选中同步高亮 + 展开祖先 + 滚动到可见、双击重命名走 `update_metadata` 并关闭画布侧的重命名

## 4. 验收

- [ ] 4.1 新增 `creative-canvas-outline.e2e.mjs`（mock API，逐个用例跑、换端口）：点击定位不在视口内的节点、画布框选后树内高亮并滚动、Cmd 点击多选不移动视口、双击改名后 Cmd/Ctrl+Z 恢复、只读画布双击不进入编辑、标签切换与资产库任务切到资产标签、搜索 / 筛选 / 无命中清除、390px 宽无横向滚动；全部通过且无 `pageerror`
- [ ] 4.2 回归与门禁：`creative-canvas-viewport.e2e.mjs` 中涉及 `#creative-library` 的用例通过，`make check-frontend` 通过；对照 proposal 验收标准逐条勾核
