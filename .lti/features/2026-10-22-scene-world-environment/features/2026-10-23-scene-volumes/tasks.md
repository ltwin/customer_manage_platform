# Tasks

## 1. 数据

- [x] 1.1 后端：先改 `model_test.go`（部分字段补齐、越界与类型不符返回 400 并带 `volume.<field>`、体积带 `material_slots` 返回 400、第 9 个体积返回 413、`update_objects` 只改一个字段时其余不变）与 `fingerprint_internal_test.go`（改体积 `lit` 指纹变、`clay`/`line` 不变）；再改 `model.go`、`ops.go`、`fingerprint.go`；`api/openapi.yaml` 加 `CreativeSceneVolume`，运行生成器。完成判据：测试先红后绿，`go test ./internal/creativescene/` 通过，两份生成物已更新
  - 体积的内容补丁直接用 `Volume`（字段全可选），与 `camera` 相同，不另设 `volumePatch`；OpenAPI 的更新项同样引用 `CreativeSceneVolume`。
  - 设计挂载点外另改 `view.go`：读取运行改动时内容单元带上 `volume`，否则助手读不到体积字段。
  - DB 级用例写在 `scene_test.go`（补齐与按键合并、带 `material_slots` 与第 9 个），`model_test.go` 写 `Validate` 的范围与上限。全包测试通过。
- [x] 1.2 前端：`state/apply.ts` 的 `contentKeys`、`updateKeys`、`mergeVolume` 与缺省值；`render/volumes/medium.ts` 的 JS 侧系数换算；单测 `creative-scene-volumes.test.ts`（系数换算、`mergeVolume`）。完成判据：单测通过，`tsc -b` 通过
  - `completeObject` 同时补齐体积的缺省值；单测 3 项通过。

## 2. 渲染

- [x] 2.1 `render/volumes/state.ts`：体积登记表、`prepareVolumes` / `finishVolumes`、选灯排序与上限、结构签名、投影列表；`LitLighting` 登记「体积可画」。单测补选灯、签名、投影列表。完成判据：单测通过，`tsc -b` 通过
  - 登记「体积可画」放在 `Lighting.tsx` 的 `useVolumesDrawn`，`LitLighting` 和 `PhysicalLighting` 都调用：物理天空下的最终渲染不经 `LitLighting`。
  - 投影的灯在阴影图建好之前按不投影计入签名（阴影图在材质第一次构建时才有），建好后下一帧重建一次步进材质；不用 1×1 占位深度纹理。
  - 单测 6 项通过（另有区间排序网络对 8 个输入的全部 0/1 排列）。
- [x] 2.2 `medium.ts` 的 TSL 求交、密度、弦长；`lights.ts`；`march.ts` 的 `VolumeNode`（区间合并、步数分配、积分、两次模糊、纹理换图）；`composite.ts` 的覆盖掩码、上采样与分段合成；`aerial.ts:aerialAt`。接入 `sceneRenderer.ts` 工作台路径与 `realtimeGI.ts`；`objects/Volume.tsx` 的线框与登记，`SceneObjects.tsx` 渲染它。完成判据：e2e「W5 window」（视口与单帧导出）、「W5 spot」、「W5 range」、「W5 clay」、「W5 ortho」、「W5 aerial」通过；W3「day unchanged」、W4 far 与 GI 仍绿
  - 每个步进节点各自持有阴影图和环境贴图的纹理节点，每帧换图（视口和离屏出图的管线各自绑定）。
  - e2e（Metal 与 SwiftShader 结果一致）：W5 window 导出光束内外 15.1 / 6.1、窗棂暗带 6.1，视口 14.4 / 6.3 / 6.1，阈值定为 2 倍与 0.6；W5 spot 锥内 203.8、锥外 0.9，GI 路径 191.4 / −1.3；W5 range 两种摆放下轮廓外 0 个像素变化，白模、线稿与无体积哈希相同，lit 连拍两次相同；W5 aerial 地平线上下 20.9 / 20.9，旁边的远处地面 165.3；W5 ortho 通过。
  - 「W3 day unchanged」需要 develop 的基线，在 2.4 之后一起跑（2.4 改所有投影灯的阴影节点）。
- [x] 2.3 首次编译与重建：步进材质在第一个体积出现时才建，视口异步编译、编好前不画，导出等编好；签名变化只重建步进材质。完成判据：e2e「W5 many lights」通过；没有体积时不建步进材质
  - `SceneRenderer` 增加 `redraw` 参数（视口传 `invalidate`），步进节点编译期间 `ready = 0`，合成不画体积；`gpu.ts` 的测试钩子加 `marches` 计数（挂载点外，只加一个计数字段）。
  - W5 many lights（9 盏投影灯，只在 Metal 上跑，SwiftShader 过慢）：步进材质 1 个，体积照常画出；「没有体积不建步进材质」放进 W5 ortho，SwiftShader 上也跑。
- [x] 2.4 表面阴影：`shadow.ts` 的 `volumeShadow` 与两个阴影节点子类；`SunCascades.setup` 乘系数；`lights.ts:buildLight` 装节点、`disposeLight` 释放；`standIn` 按原灯装同类阴影节点。完成判据：e2e「W5 cast shadow」「W5 shadow off」「W5 no recompile」通过；「sun shadows」仍绿
  - W5 cast shadow：浓烟投影处的地面 162 → 78，旁边受光的地面不变；关闭投射阴影后轮廓外 0 个像素变化。
  - **替身灯没有消除新灯后的重新编译**：develop 上加一盏投影的聚光，之后的帧仍重新编译 9 个材质程序（WebGPU 与 WebGL2 都一样），本分支没有体积时也是 9 个。对比着色器代码，替身编译与真实材质的差别只在生成的变量编号和 uniform 顺序，是 three 构建顺序造成的，不是阴影节点。阴影节点同类仍然装上（否则代码结构就不同），按 id 排序替身灯试过无效，已撤回。
  - 「W5 no recompile」改为：同一场景有体积和没有体积时，加一盏投影的聚光或平行光后帧里重新编译的程序数相同（都是 9），且步进材质不在其中；关掉投影后画面照常。满足 AC-W5-11「不比 develop 多」。
  - 步进材质的预编译最初没有命中：`QuadMesh` 绘制时按顶点序号生成全屏三角形，`compileAsync` 编出的顶点着色器不同。步进改用平面网格加正交摄像机绘制，预编译与绘制一致。
  - 「shadow off」并入「W5 no recompile」：关掉两盏聚光的投影后画面照常、无报错。
  - 回归：「sun shadows」「W4 far」「W4 real-time GI」「AC-1」「AC-4」通过。「W3 day unchanged」与「lights: the rendered view warms…」在 develop 上同样失败（环境原因，非本次引入）；改用逐像素对照：用 develop 与本分支分别出 W3 基线的 4 张图（正午与手动天空的导出和视口），逐像素完全相同。

## 3. 编辑界面与助手

- [x] 3.1 `editing.ts:newVolume`（落点单测）、`SceneEditor.tsx` 添加菜单两项与属性栏、`panels/VolumeFields.tsx`、`Outliner.tsx` 图标、`Volume.tsx` 的像素阈值拾取、`Viewport.tsx:centrePoint` 跳过体积。完成判据：e2e 走一遍添加、改密度、Cmd+Z 撤回；「W5 params」通过
  - W5 editing：Shift+A 添加立方体体积（2 m，底面落在视口中心对着的物件上），数字框与对数滑杆改散射密度（最左一档为 0），打开投射阴影后 Cmd+Z 撤回，范围里面点击选中后面的物件而不是体积。e2e 的测试服务器 `restoreUnits` 补上 `volume`（测试夹具，不是产品代码）。
  - W5 params：光束处散射密度 0.1 → 0.3 亮度增量 11.2 → 20.0，各向异性 0 → 0.6（逆光）11.2 → 15.1；透过吸收体看白盒，红蓝比 0.79，保留红色时 0.91，保留蓝色时 0.50。AgX 先混合通道再过曲线，被保留的通道也会略暗，所以按色相方向判断。
- [x] 3.2 `creativeagent/scene_tools.go` 的 `edit_scene` 说明与测试关键句；`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md`。完成判据：`go test ./internal/creativeagent/` 通过
  - 说明加体积物件的字段、范围、上限、密度提示和「窗前的雾」样例（约 1 KB）；`TestEditSceneDescribesThreejsFields` 的合并规则改为「light、camera、volume 按键合并」；`compaction_regression_test.go` 的 14000 token 窗口不够，按 W4 的做法调到 15000（注释写明原因）。`go test ./internal/creativeagent/` 全部通过。

## 4. 验收

- [x] 4.1 e2e「W5 twice」「W5 backends」；AC-W5-1 亮度比阈值按样张实测写进 e2e；owner 样张四张（窗口光束、聚光光锥、浓烟投影、介质里的点光）存进 `discovery/research/evidence/`。完成判据：`SCENE_E2E_ONLY=W5` 全绿，样张交 owner
  - 连拍两次逐像素一致放在「W5 range」里（lit 两次哈希相同）。
  - W5 backends（`SCENE_E2E_W5_METAL`，`evidence/w5-parity.json`）：窗口屋子 0.249 / 0.121%，在 Epic lit 阈值内；Cornell 聚光 0.761 / 1.107%，比同一张图没有体积时更接近。
  - 样张 4 张见 `evidence/README.md`（e2e「W5 samples」，`SCENE_E2E_W5_SAMPLES` 生成）。点光图里墙上的方块亮斑与环纹没有体积时也有，是现有的点光阴影现象。
  - `SCENE_E2E_ONLY="W5 "` 在 SwiftShader 上 9 项全绿；Metal 上另有 many lights、backends、samples、performance。
- [x] 4.2 性能（AC-W5-11）：1 个、8 个、8 个都投影三组，以及没有体积时帧时间和加灯编译时间与 develop 对照，记入 evidence。完成判据：数据齐全，未达标项写明原因
  - 视口测量加了测试钩子 `gpuTestHooks.redrawViewport`（`Viewport.tsx` 只在 e2e 页面上设置，挂载点外）；develop 的对照在临时 worktree 里打同样的钩子。
  - 测量中发现天空光照项在循环里每一步都采样（节点在循环外创建、第一次在循环里用），提前求值后 1 个体积从 +3.7 ms 降到 +3.0 ms。
  - 结果（`evidence/README.md`）：1 个 +2.98–3.01 ms、8 个 +4.42–4.51 ms、8 个都投影再 +2.04–2.25 ms，都在目标上（前两项余量为零）；没有体积时帧时间比 develop 多约 0.1 ms（测量波动内）。
  - **未达标**：没有体积时加一盏投影的聚光，预编译 217–240 ms，develop 166–182 ms，多约 50 ms。来自每盏投影灯阴影里的体积透射率循环（打桩成 1 时 186–188 ms）。试过把它做成着色器函数（three 复用函数代码时不声明其中引用的 uniform 和缓冲，不可行）和无分支版本（无改善），都已撤回。要做到不多，只能在场景里出现投影体积之后才把这段代码放进材质，代价是第一次打开投射阴影时所有受光材质重新编译一次，与 design「切换开关不重新编译」冲突，交 owner 决定。
- [ ] 4.3 助手走一次（AC-W5-10）：请助手「在窗前放一团能看到光束的雾」，存截图。需要开发环境里的真实模型，本次没有跑。
