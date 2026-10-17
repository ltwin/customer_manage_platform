# 验证记录：场景文档与编辑器外壳（C1）

日期：2026-10-16（2026-10-17 补 owner 第二轮测试的修正）　分支：`feat/scene-document-shell`（worktree `.worktrees/scene-document-shell`，基线 `2a883f4`）

## 门禁结果

| 检查 | 结果 |
|---|---|
| `go test -count=1`：`creativescene`、`creativecontent`、`creativedocument`、`creativecanvas`、`platform/store`、`platform/httpapi` | 通过 |
| `golangci-lint run`：上述包与 `cmd/server`、`cmd/creative-worker` | 0 issues |
| `make generate-check`（OpenAPI 两份生成物、`builtins.gen.json`） | 通过，无漂移 |
| `make check-frontend`（oxlint、`tsc -b` + vite build、589 个 node 测试） | 通过；lint 只有既有警告，没有本次新增的 |
| `frontend/scripts/creative-scene.e2e.mjs`（Playwright Chromium，mock 路由） | 22/22 通过（2026-10-17 修订增长优化后重跑）；夹具与 `main.tsx` 一样用 StrictMode 渲染 |
| `make check`（合回 `develop` 前的门禁） | 未跑，合并点执行一次 |

e2e 的服务端是脚本里的内存实现：冲突检查、修订号、回收站、修订列表和整份恢复都按契约模拟，操作语义复用前端的 `state/apply.ts`。服务端真实语义由 Go 测试负责，e2e 验证的是编辑器在真实浏览器里的交互、请求次数和请求内容。运行方式：在 `frontend/` 起 Vite（`npx vite --port 5179`），再执行 `node scripts/creative-scene.e2e.mjs`；`SCENE_E2E_ONLY=<名称片段>` 只跑一项，`SCENE_E2E_SHOTS=<目录>` 保存关键步骤截图。

测试顺序说明：后端 2.x 的部分测试写在实现之后。补做了变异检查：把 `restore.go` 的顺序规则和父对象规则改坏，恢复测试都会失败。浏览器验证中新增的两条会话单测（撤销回执即时推进基线、大回执推进涉及单元）在旧逻辑下确认失败，修复后通过。

## 浏览器验证中发现并修正的问题

| 问题 | 修正 | 回归测试 |
|---|---|---|
| 撤销 / 重做回执到达后 base 不推进、要等重读，期间紧接的编辑带旧修订号，和自己的回执冲突 | 回执连续时就地推进 base；撤销与 `reset_required` 另补一次重读校正 | 单测「an undo receipt advances the base at once…」「a receipt too large…」；e2e「undo and redo」 |
| 自由移动的平面法线取「枢轴→相机」，透视下横向拖动带出竖直漂移 | 改为平行于屏幕的视平面（法线为相机朝向，同 Blender） | e2e「keyboard flow」断言横拖后高度不变 |
| 新物件落在指针移向菜单项途中的位置，而不是按 Shift + A 时的位置 | 记下打开菜单时的位置；从菜单栏打开时放在视图中心 | e2e「new objects go where the menu opened」 |
| 添加菜单第一项是地面，回车会误加一块 40 m 的未锁定地面 | 地面放到菜单末尾；新加的地面锁定并放在原点 | 单测「a new ground goes to the origin…」；e2e 断言首项是方块 |
| 选中描边浅黄、1 px，在陶土色上几乎看不出，球体只描折角边时完全没有描边 | 实体用外轮廓，扁平体用加粗边线，颜色取 Blender 选中橙 / 活动浅橙 | 截图目检 |
| 新建修订与随后两分钟内的编辑并成一组，回不到刚建好时 | 新建单独成组，标为「新建场景」 | 单测「history groups…」；e2e「history」 |
| 属性栏只在回车 / 失焦时提交，与 AC-2「停下 300ms 后提交」不符；聚焦再离开会写回舍入值 | 300ms 防抖 + 回车 / 失焦立即提交，只提交改动过的文本；属性栏按物件重建 | e2e「properties commit 300 ms…」 |
| 字段内回车后焦点落到页面 body，快捷键全部失效 | 焦点交还编辑器根元素；点击视口也取回焦点 | e2e「key boundaries」 |
| owner 测试：新建方块后退出编辑器再进来，方块不见了（数据库里只有第 1 版） | 开发模式的 StrictMode 挂载后会多跑一轮 effect 清理：会话的 `attach` 写在 `useMemo`、`detach` 写在清理里，模拟卸载时立刻判定没人使用并销毁会话，之后的编辑只在本地显示、再也不发送。撤销栈同理会断开监听。改为 effect 里成对 `attach` / `detach`、按计数并推迟一拍判定；撤销栈可重连。生产构建不受影响，但写法本身不符合 React 的 effect 约定。原 e2e 夹具没开 StrictMode，所以没测出来 | 单测「StrictMode detaches and attaches…」（改回立即判定时失败）、「the undo stack reconnects…」；e2e「closing the editor right after adding…」修复前失败 |
| owner 测试：触控板双指滑动、捏合后视图要点一下才更新 | 视口按需渲染，滚轮 / 手势调用 camera-controls 时不带过渡、不发事件，没有触发重绘；每次程序化移动相机后显式重绘 | e2e「trackpad swipes and pinches…」修复前失败 |
| owner 测试：坐标轴与 Blender 不一致（`G Z` 是前后） | 编辑器层换成 Blender 坐标（Z 向上），文档数据仍 Y 向上，见 design「局部设计与偏差」 | 单测「the editor speaks Blender…」等 4 条；e2e `G Z 2` 抬高、属性栏按 Blender 轴、数字键与坐标轴部件 |
| 点击坐标轴部件会取消选中；drei 的转视角动画转到顶视后画面歪 90° | 控制器自己命中轴头，走数字键的视图切换，拦下这次点击 | e2e「number keys and the axis widget…」 |
| 冲突提示压住坐标轴部件；助手面板变底部抽屉（≤ 760px）时编辑器仍让出右侧 | 调整右边距与断点 | 截图目检；e2e「makes room for the assistant panel」 |
| owner 测试（2026-10-17）：新建方块后按 S 缩放，Cmd+Z 把方块一起撤掉 | 只读查了 owner 那份场景：只有第 2 版（新建，缩放仍为 1），缩放和撤销都没到服务端。在真实路由（httptest + 容器库，临时探针，已删）上直接操作、加 1.5～3 秒保存延迟都复现不了「一次撤掉两步」，但找到了能精确产生这个现象的代码路径：新建在排队等前一个保存时，紧接的缩放被并进新建（原合并规则：待发的新增吸收之后对它的修改），Cmd+Z 撤掉的是整个待发新增。改为只在同一次连续编辑（合并键）和 Shift + D 复制后移动时合并，见 design「合并」 | 单测「adding while a save is slow, then scaling…」「commits of one field fold…」，修复前失败；e2e「slow saves…」第一次 Cmd+Z 只去掉缩放，修复前失败 |
| 排查时发现：任何撤销提示（「这一步之后物件已被修改」「正在保存」）都会让编辑器无限重渲染并崩溃，整页空白 | 每次渲染都把 `undo.notice` 写进界面状态，写入又触发渲染。改为撤销栈逐条推送提示 | e2e「slow saves…」保存中连按两次 Cmd+Z 编辑器仍在、提示正确；修复前在真实路由上复现为 `Maximum update depth exceeded` |
| 保存中按 Cmd+Z 只提示「稍后再撤销」，什么都不做；连远程库时保存慢，撤销像失灵 | 记下这次撤销，回执到达后执行；期间有新编辑或修改被拒绝就取消 | 单测「undo while the last edit is saving…」、改写后的「undoing an edit that has not been sent…」；e2e「slow saves…」放行后先存新建、再撤销新建 |
| owner 测试（2026-10-17）：触控板双指滑动环绕方向是反的 | 环绕改为画面跟着手指走，与 Shift 平移、Blender 默认一致 | 单测「a two-finger swipe turns the view the way the fingers go…」直接用 camera-controls 断言，旧符号下失败 |
| owner 需求（2026-10-17）：再次进入时视窗要和离开时一样 | 相机、选择、工具、面板开合按账号和场景存本机，见 design「视窗状态」 | 单测「the view is kept per account and scene…」；e2e「leaving and coming back, even after a reload…」（关闭再打开、刷新页面；正交顶视与任意透视角度对比截图），修复前失败 |
| 验证视窗恢复时发现：正交视图里场景缩小几十倍；7 之后立刻 5 得不到顶视 | 正交相机设 zoom 后漏了更新投影矩阵；切换时取了过渡中途的相机位置。分别补上更新、改取 camera-controls 的目标终点 | e2e「7 then 5 at once gives a top orthographic view…」，修复前 85.8% 像素不同 |

## 代码审计发现的问题（2026-10-17）

审计报告列了 3 个 P2，逐项核对都成立；核对第 3 项时又找到一个同源问题。每项先写测试确认在修复前失败，修复后通过。

| 问题 | 原因与修正 | 回归测试 |
|---|---|---|
| 删除全部物件后改渲染设置，再用 `restore_units` 撤销设置，快照存成 `"objects": null`，编辑器遍历时报错 | 恢复时用 `append([]Object(nil), …)` 复制，空切片变成 nil。改用 `slices.Clone`；编码快照时再兜底，nil 一律写成 `[]` | `creativescene` 「TestRestoringSettingsOfAnEmptySceneKeepsObjectsAnArray」 |
| `position: [null, 0, 0]` 返回 200，物件被移到原点，修订递增 | `requireFields` 对任何 `null` 都放行，也不检查数值数组的元素；Go 把 `null` 解码成零值。改为只有契约标为可空的字段接受 `null`，数组和映射的元素一并检查 | 「TestInvalidCommandsLeaveNoRevision」新增 11 个 `null` 用例（坐标、旋转、`visible`、`name`、`order`、更新里的可选字段、`params`、`objects`、`ambient`、删除 ID、参数值），断言 `validation_failed` 且不产生修订 |
| `POST /creative/assets` 提交 `kind: "scene"` 返回 500 | 资产入口复用共享内容校验，C1 加了 `scene` 分支后能通过校验，进写事务才撞上 `creative_assets_kind_check`。在共享层把 `scene` 列为文档专用类型：`Validate`、`WriteAndRetain` 拒绝，文档层改用 `WriteDocumentAndRetain` | `creativelibrary` 「TestCreateAssetRefusesSceneContent」：返回校验错误，资产和内容修订都没有写入 |
| （核对第 3 项时发现）画布 `replace_content` 能把一份未经场景 schema 校验的内容挂到 `core.scene` 节点上 | 同一根因：`replace_content` 把节点类型去掉 `core.` 前缀当作内容类型，场景节点得到 `scene`，再走共享写入。随第 3 项一起修正 | `creativescene` 「TestNewSceneFromCanvas」末尾：带授权信息的 `replace_content` 返回校验错误，内容修订数不变 |

门禁：`golangci-lint`（`creativecontent`、`creativedocument`、`creativescene`、`creativelibrary`、`creativecanvas`、`creativegeneration`、`creativeagent`、`platform/httpapi`）0 issues；上述 8 个包 `go test -count=1` 全部通过。`make check-go` 传多个包时 `test-go` 只测第一个（`PKG` 往下传没加引号），这次直接用同样参数跑 `go test`。前端发出的请求只在 `parent_id` 上写 `null`，不受新检查影响。

## 修订增长的检查与优化（2026-10-17）

owner 问快速操作是否会产生大量修订、有没有性能风险。核对结论：

- **修订数**：每个独立操作一个修订（同时是一个撤销步骤），连续操作合成一个（拖动松手提交、属性输入停 300 ms 提交、复制后移动）。操作快不会让修订数翻倍。历史面板按 2 分钟分组、每页 50 条，不会刷屏；撤销和重做是恢复，每次单独一条。
- **前端**：同一时间只发一个请求。模拟保存卡住时意图积压：100 个物件积压 200 个，新操作 0.4 ms；1000 个物件积压 500 个，新操作 12 ms。
- **请求与读取**：每个编辑器最多一个请求在途。每次保存引起的重读（画布页一次、编辑器 500 ms 内最多一次、历史面板开着时第一页一次）都有合并或节流。读当前场景只读最新一份，历史列表按索引分页，修订再多也不变慢。
- **存储（真正的风险）**：每次保存存一份完整快照，旧修订不清理。本地 PG17 实测压缩后：每个物件约 90 B / 修订；100 个物件约 9.6 KB，300 个约 27 KB，1000 个约 90 KB（物件位置、尺寸各不相同；AC-11 的 5.2 KB 是相同方块）。1000 次操作在 100 个物件的场景上约 10 MB，只增不减。epic 设计已把历史清理列为后续工作，尚无子项承接，待 owner 决定。
- **白写的修订**：服务端不识别无变化的命令。Alt+G / R / S 不检查物件是否已是默认值；编辑器也不忽略按键重复，按住 Alt+G 一秒写十几到几十份快照。

修正：

| 问题 | 修正 | 回归测试 |
|---|---|---|
| 无变化的修改照样保存 | `SceneSession.edit` 入队前经 `withoutNoops` 去掉不改变任何值的字段、更新和操作，全部无变化则不入队；`clearOp` 跳过已是默认值的物件，标签只数真正改动的物件 | `creative-scene.test.ts`「an edit that changes nothing is not saved…」；`creative-scene-interaction.test.ts`「hide, hide unselected and reveal…」补两条清除断言 |
| 按住快捷键重复执行 | `handleKey` 遇到 `event.repeat` 仍拦下按键，但只有撤销、重做执行 | `creative-scene-interaction.test.ts`「a held key acts once and stays claimed…」；e2e「a held key acts once, and clearing what is already clear saves nothing」 |

三条单测和这条 e2e 都在修复前确认失败（e2e 按住 Alt+G 再补按两次，写了 6 条修改，应为 1 条），修复后通过。门禁：`make check-frontend` 通过（589 个测试）；e2e 22/22。

## 验收对照

简称：`scene` = `backend/internal/creativescene/scene_test.go`，`model` / `restore` / `concurrency` 同目录对应文件；`http` = `backend/internal/platform/httpapi/creative_documents_test.go`；`unit` = `frontend/scripts/creative-scene.test.ts`，`interaction` = `frontend/scripts/creative-scene-interaction.test.ts`；`e2e` = `frontend/scripts/creative-scene.e2e.mjs`。

| 验收 | 证据 | 结论 |
|---|---|---|
| AC-1 新建与打开 | `scene` `TestNewSceneFromCanvas`（`add_node core.scene` 同事务产生文档、修订 1，快照只有锁定的地面）；画布创建菜单、卡片、最大化入口靠代码走查和 `tsc`；编辑视图渲染地面和网格见 e2e 截图 | 后端满足；**画布侧未在浏览器验证**：现有画布 e2e 需要真实账号，请 owner 在真实画布上走一遍（新建「3D 场景」、卡片物件数、双击打开、助手面板仍可打开） |
| AC-2 放置与变换 | e2e「keyboard flow」：Shift + A 加方块并选中，`G Z 2 Enter` 状态栏显示「移动 · Z 轴 · …2」、只 1 个命令且 Z 恰好 +2，`R` 右键取消无请求，最终「已保存」；e2e「move tool drags…」gizmo 拖动 1 个命令；e2e「properties commit 300 ms」宽度改 3 m 1 个命令；Ctrl 5° 吸附见 `interaction`「R turns around the view axis…」 | 满足 |
| AC-2a 导航 | `interaction`：鼠标 / 触控板滚轮映射、`ctrlKey` 一律推拉、中键与 Alt + 左键、设备推测与手动选择、触控板环绕方向（camera-controls 实测）；e2e 数字键与坐标轴部件、7 之后立刻 5 的正交顶视、触控板滑动与捏合无需点击即重绘 | **部分满足**：`classifyWheel` 仍是假设，等 4.1 真机录制校准（6.2 未勾） |
| AC-2b 多选 | e2e「selection edits」：框选两只方块且不含锁定地面，H / Alt + H 各 1 个命令，Shift + D 复制并移动合计 1 个 `add_objects`，X 确认后 1 个删除；e2e「keyboard flow」A 全选后 G 只更新未锁定物件；`interaction` Shift + 单击三态、只变换最上层；`unit` 多物件一次确认一个撤销步骤 | 满足 |
| AC-2c 上手辅助与按键边界 | e2e「key boundaries」：状态栏提示、菜单项标注快捷键、名称框里打 `grsx` 只产生重命名、Esc 三次不关视图只清选择、Cmd/Ctrl + S 提示自动保存、`?` 打开速查、按键不会到达编辑器之外的监听；`interaction` 按 `event.code` 分发、组字中不分发 | 满足 |
| AC-3 重开一致 | e2e「reopening」：新页面重新读取后属性栏数值与服务端一致；e2e「leaving and coming back…」：关闭再打开、刷新页面后工具、选择、视口与离开时一致（截图差异 < 0.5%），且不产生命令；`interaction` 视窗状态的校验与节流写入 | 满足。视窗状态只存在本机浏览器 |
| AC-4 只撤销自己 | e2e「undo and redo」：撤销移动、撤销添加、重做添加（同一 ID 回来）、重做移动，均为 `restore_units`；e2e「slow saves…」：慢保存下新建再缩放，Cmd+Z 依次去掉缩放、待发的新建，保存中的再撤销等回执后执行；`unit` 保存中撤销的等待与取消；`unit` 过期规则（别人改过即跳过并提示、重新移动不复活、别人改层级不过期）；`restore` 150 个物件嵌套分组一次撤销、单元冲突整体失败、层级复原 | 满足 |
| AC-5 单元并发 | `concurrency` 摆放与内容并发都成功、同一单元恰一方成功（各 20 轮）；e2e「someone else's edit」409 后提示「重新应用我的修改」，重发后落在最新版本上；`unit` 删除只撞 `objects` 时提示确认、新增只撞 `objects` 时自动变基、被拒意图的后续修改一起进入冲突提示 | 满足 |
| AC-6 历史版本 | e2e「history」：列表分组、新建单独成组、只读预览（编辑菜单禁用）、确认后整份恢复产生新修订（`restored_from = 1`）；`unit` 分组与描述 | 满足 |
| AC-7 多标签页同步 | e2e：另一页收到画布失效通知后显示新位置；回收站中提交得到 409 后切只读，移出回收站后恢复可编辑；`unit` 拖动中推迟重读 | 满足。e2e 直接调用 `notifyCanvasInvalidate` 代替真实 SSE；SSE → 失效通知这一段沿用画布既有链路 |
| AC-7a 离开提示 | e2e「leaving with unsaved edits」：保存中关闭页面弹出 beforeunload，保存后不再拦截；e2e 冲突时点「还原节点」先确认；`unit` 离开提示跟随未保存状态 | 满足 |
| AC-8 校验与上限 | `model` `TestValidate`（S1–S8、灯光、摄像机、分组、材质槽）；`scene` `TestInvalidCommandsLeaveNoRevision`；`http` 33 台摄像机 413 | 满足 |
| AC-9 账号隔离与回收站 | `http` 跨账号 404、回收站 409 `creative_state_conflict`；e2e 回收站中只读并提示先恢复 | 满足 |
| AC-10 按需加载 | 构建产物：`WebGLRenderer` 只在 `SceneEditor` 块（约 1.1 MB，gzip 305 KB），画布入口块大小不变；e2e「opening the canvas page loads no three.js」；e2e「without WebGL2」显示说明 | 满足 |
| AC-11 写入频率实测 | 本机 `CREATIVE_SCENE_BASELINE=1 go test -run Baseline`：300 个物件快照约 94 KB；连续 100 次 `update_objects` 服务端 p50 26 ms / p95 34 ms；每修订存储约 5.2 KB；10 个场景节点的画布读快照 p50 约 50 ms | 达标（设计假设 p95 < 150 ms、画布 < 500 ms），不提前做摘要缓存 |

## 未完成与待 owner 决定

1. **4.1 浏览器输入穿刺 / 6.2 导航**：需要 owner 在 macOS Chrome / Safari / Edge（触控板和鼠标）与 Windows Chrome（鼠标）上录制。录制页在本次会话的 scratchpad（不入库）。录制结果存为 `frontend/scripts/fixtures/scene-input/*.json` 后，再校准 `classifyWheel`、补夹具测试、勾选 6.2。也可以由 owner 决定延期，作为后续安排记录。
2. ~~坐标轴与 Blender 的差异~~：**已处理**（owner 2026-10-16 要求与 Blender 一致）。编辑器层采用 Z 向上，文档数据保持契约的 Y 向上；摄像机 / 灯光朝向的显示差别留到 C4。
3. **AC-1 画布侧**：请在真实画布上手动确认一次（见上表）。
4. **proposal 风险第 7 条不成立**：原以为 `add_node` 枚举加 `core.scene` 会改变助手 `edit_canvas` 的工具 schema，需要排空后升级。实际助手的节点类型是手写枚举（`backend/internal/creativeagent/canvas_tools.go:nodeTypes`，只有文字、图片、视频、音频、链接），`tool_schemas.gen.go` 也没有变化。所以本次上线不需要排空升级，助手在 C1 也不能新建场景，要到 C3 才接入。design 的挂载点已注明。
5. **历史清理**：每次保存一份完整快照、只增不减（见「修订增长」）。建议作为 epic 的后续子项：近期修订全部保留，更早的按历史面板的 2 分钟分组只留每组最后一版，恢复点和新建保留；保留期限由 owner 定。
