# 验证记录：场景文档与编辑器外壳（C1）

日期：2026-10-16　分支：`feat/scene-document-shell`（worktree `.worktrees/scene-document-shell`，基线 `2a883f4`）

## 门禁结果

| 检查 | 结果 |
|---|---|
| `go test -count=1`：`creativescene`、`creativecontent`、`creativedocument`、`creativecanvas`、`platform/store`、`platform/httpapi` | 通过 |
| `golangci-lint run`：上述包与 `cmd/server`、`cmd/creative-worker` | 0 issues |
| `make generate-check`（OpenAPI 两份生成物、`builtins.gen.json`） | 通过，无漂移 |
| `make check-frontend`（oxlint、`tsc -b` + vite build、577 个 node 测试） | 通过；lint 只有既有警告，没有本次新增的 |
| `frontend/scripts/creative-scene.e2e.mjs`（Playwright Chromium，mock 路由） | 15/15 通过，连跑两遍稳定 |
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
| 冲突提示压住坐标轴部件；助手面板变底部抽屉（≤ 760px）时编辑器仍让出右侧 | 调整右边距与断点 | 截图目检；e2e「makes room for the assistant panel」 |

## 验收对照

简称：`scene` = `backend/internal/creativescene/scene_test.go`，`model` / `restore` / `concurrency` 同目录对应文件；`http` = `backend/internal/platform/httpapi/creative_documents_test.go`；`unit` = `frontend/scripts/creative-scene.test.ts`，`interaction` = `frontend/scripts/creative-scene-interaction.test.ts`；`e2e` = `frontend/scripts/creative-scene.e2e.mjs`。

| 验收 | 证据 | 结论 |
|---|---|---|
| AC-1 新建与打开 | `scene` `TestNewSceneFromCanvas`（`add_node core.scene` 同事务产生文档、修订 1，快照只有锁定的地面）；画布创建菜单、卡片、最大化入口靠代码走查和 `tsc`；编辑视图渲染地面和网格见 e2e 截图 | 后端满足；**画布侧未在浏览器验证**：现有画布 e2e 需要真实账号，请 owner 在真实画布上走一遍（新建「3D 场景」、卡片物件数、双击打开、助手面板仍可打开） |
| AC-2 放置与变换 | e2e「keyboard flow」：Shift + A 加方块并选中，`G Z 2 Enter` 状态栏显示「移动 · Z 轴 · …2」、只 1 个命令且 Z 恰好 +2，`R` 右键取消无请求，最终「已保存」；e2e「move tool drags…」gizmo 拖动 1 个命令；e2e「properties commit 300 ms」宽度改 3 m 1 个命令；Ctrl 5° 吸附见 `interaction`「R turns around the view axis…」 | 满足 |
| AC-2a 导航 | `interaction`：鼠标 / 触控板滚轮映射、`ctrlKey` 一律推拉、中键与 Alt + 左键、设备推测与手动选择；1 / 3 / 7 / 5、`.`、Home 已接线，未做浏览器断言 | **部分满足**：`classifyWheel` 仍是假设，等 4.1 真机录制校准（6.2 未勾） |
| AC-2b 多选 | e2e「selection edits」：框选两只方块且不含锁定地面，H / Alt + H 各 1 个命令，Shift + D 复制并移动合计 1 个 `add_objects`，X 确认后 1 个删除；e2e「keyboard flow」A 全选后 G 只更新未锁定物件；`interaction` Shift + 单击三态、只变换最上层；`unit` 多物件一次确认一个撤销步骤 | 满足 |
| AC-2c 上手辅助与按键边界 | e2e「key boundaries」：状态栏提示、菜单项标注快捷键、名称框里打 `grsx` 只产生重命名、Esc 三次不关视图只清选择、Cmd/Ctrl + S 提示自动保存、`?` 打开速查、按键不会到达编辑器之外的监听；`interaction` 按 `event.code` 分发、组字中不分发 | 满足 |
| AC-3 重开一致 | e2e「reopening」：新页面重新读取后属性栏数值与服务端一致 | 满足 |
| AC-4 只撤销自己 | e2e「undo and redo」：撤销移动、撤销添加、重做添加（同一 ID 回来）、重做移动，均为 `restore_units`；`unit` 过期规则（别人改过即跳过并提示、重新移动不复活、别人改层级不过期）；`restore` 150 个物件嵌套分组一次撤销、单元冲突整体失败、层级复原 | 满足 |
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
2. **坐标轴与 Blender 的差异**：数据契约规定 Y 轴向上、正面朝 +Z（epic `patterns: 几何模块契约`），所以 `G Z` 是前后移动；Blender 的 `G Z` 是上下移动。习惯 Blender 的摄影师会按错。可选：保持现状并在速查里说明；或者存储仍是 Y 向上，界面和锁轴键按 Blender 的叫法映射（X 右、Y 纵深、Z 向上）。
3. **AC-1 画布侧**：请在真实画布上手动确认一次（见上表）。
