# Tasks

先做不确定性最大的实时 GI 管线（局部未决 L1–L3），再接场景格式、选项卡和出图。分支上各任务之间允许功能暂时不完整（例如 GI 管线先只能经测试参数打开），但每个任务结束时 `tsc -b`、`oxlint`、相关单测和 `go test` 都通过。

## 1. 实时 GI 管线

- [x] 1.1 `render/nodes/SSGINode.js`（+ `.d.ts`，黄金比例采样、直接输出 `vec4(GI, AO)`）、`render/giSplit.ts`、`render/realtimeGI.ts`，`SceneRenderer` 的 `Frame.engine` / `quality` 与 `keyOf` 扩展（此时只经测试参数打开）。先写单测：SSGI fork 守护（换回上游块后逐字相同）、`giSplit` 守护（无标记时着色代码与不包装时逐字相同）、合成代数（常量纹理）。完成判据：盒子间的 `lit`、`clay` 实时 GI 出图有渗色和反射、没有均匀提亮；结清 L1（MRT 读 property、三附件建管线，Metal 与 `forceWebGL` 都试）与 L3（递归降噪接法），不成立时按 design 的退路实现并记在本任务下
  - **L1 成立**：MRT 读得到光照模型里赋值的 property；GI 光照 pass 的 `output` 加三个附件在 Metal 和 `forceWebGL`（WebGL2）下都能建管线、出图不报错。采用方案不变，不用 `builtinGIContext` 退路。
  - **L3 成立**：盒子间 2048 × 1365，`lit`，16 帧加 TRAA，后墙上部残余噪点（R0 度量）不降噪 14.4、递归降噪 5.9；渗色保留：立方体左侧面红绿比工作台 0.95 → 实时 GI 1.44，红墙旁地面 1.00 → 1.21。剩余噪点留给 K 帧累积（4.1）和标定（5.1）。
  - 实施中的发现：
    - three 会把不透明材质的 alpha 强制设为 1，fork 若用 `colorNode` 输出 `vec4(GI, AO)`，AO 会整张变成 1；改用 `fragmentNode`（fork 块已注明）。
    - 拆出的三个附件按材质的混合方式写入：透明、透射材质写 alpha 0，附件保留背后物体的值，两个后端一致（WebGL2 只能全局混合）。合成的 `split`（是否拆过、且预通道有表面）取 `ambient.a × (深度 < 1)`，玻璃和背景处画面原样输出。
    - SSR 命中覆盖率先取每帧的原始命中（光线长度 < 1000），期望值上无偏，噪点靠 TRAA 和累积；是否需要时域平滑在 1.2 看图定（L2）。
    - 灯光预编译（`SceneRenderer.compile`）还没带上 GI 光照 pass 的上下文，这个 pass 的材质在第一帧编译；放到 1.2 或性能任务（7.2）处理。
    - 雾：`output` 是加雾后的，三个间接光通道没有加雾，远处 AO、GI 会略偏；参考场景没有雾，记为近似。
    - 实时 GI 白模平均亮度高于工作台（盒子间 119.6 对 106.0，`lit` 126.5 对 91.2），由 1.2 的环境光补偿标定。
    - 离屏出图暂时按场景里的 `render.engine` 画实时 GI、固定预热 16 帧（`engines.ts` 的 `GI_WARMUP_FRAMES`），4.1 换成 K 帧累积。
  - 单测：`scripts/creative-scene-gi.test.ts`（在 node 里用 WebGPU 后端的节点构建器生成 WGSL 比对；去掉标记判断的变异会被抓到）。
- [x] 1.2 SSR 命中覆盖率（结清 L2）、接触阴影的主光源选择、`final` 档关掉 `flickerSuppression`、实时 GI 的环境光补偿（`litLook.ts`、`Lighting.tsx`，`room` 与盒子间平均亮度与工作台相差 ≤ 10%）；GI 管线释放。完成判据：连续导出 48 张实时 GI 图后 GPU 纹理数不增长（e2e，计数同 R1 的灯光用例）；`forceWebGL` 下盒子间与 WebGPU 在 AC-R1-1 阈值内
  - **L2 结清**：命中覆盖率取每帧的原始命中（光线长度 < 1000），合成在期望值上无偏，噪点交给 TRAA 和累积；不 fork `SSRNode`。盒子间金属球的反射在 16 帧时还有颗粒，4.1 累积后再看。
  - **接触阴影**：`sceneRenderer.ts` 的 `mainLight`：太阳（`Lighting.tsx` 给白模和材质光照的太阳加 `userData.sun`，落到地平线下强度为 0 时不算）；否则取投影的平行光（按强度）或聚光灯（按强度除以到目标距离的平方）中照度最大的；点光没有单一方向，不参与。主光源变化时重建管线（键里带它的 uuid）。
  - **GI 强度**：three 缺省的 `giIntensity = 10` 让室外场景（`materials`）阳光照到的墙边阴影被冲淡；按 SSGI 的积分，能量守恒约在 2.5，但盒子间天花板会全黑。暂定 5（`engines.ts` 的 `GI_INTENSITY`），5.1 标定和 AC-R2-10 重绘对照时连同对照图交 owner。
  - **环境光补偿**：`AMBIENT_FILL_GI = 0.35`（`ambientFill(engine)`）。`giIntensity` 5 时，材质光照 room +5.8%、noon −3.0%（0.25–0.5 都在 10% 内，取最对称的值）；白模不另设补偿：盒子间 −6%、room −4%、noon +1%。盒子间材质光照 +27%，它的环境光只有 0.05，亮出来的全是反弹光，补偿压不下去，按 design 的标准不达标，交 owner 看图决定。
  - **释放**：Metal 上起初每张图多留 7 张纹理、3 个渲染目标、70 个 uniform buffer、1 个着色程序，原因有三，均已修：
    - `convertToTexture` 在 TRAA、AO 降噪输入、SSR 的重投影里隐式建的 RTT 节点不随节点释放：改为显式建立并纳入释放清单（SSR 改传 `getTextureNode()`）；
    - TRAA 的 1×1 占位深度纹理，`dispose()` 不释放：单独释放；
    - three 按 MRT 节点的 id 区分渲染上下文，每张图新建 MRT 节点就会给场景每个网格在两个 pass 各留一套渲染对象（场景材质不随管线释放）：两个 MRT 改为模块级共享。GI 光照 pass 的上下文也改为每个渲染器上下文一个（`giSplit.ts` 的 `GILightingPass` / `splitContext`），图与图之间复用渲染对象。
    - 结果：Metal、SwiftShader 连续 48 张，计数从第 1 张到第 48 张不变；12 张 512 px 的耗时 4.8 s → 1.7 s。e2e「real-time GI: 48 pictures in a row leave nothing behind on the device」（SwiftShader 下约 155 s）。
  - **`forceWebGL`**（盒子间，16 帧）：白模平均差 0.32、差 > 8 的像素 0.04%，达标；材质光照平均差 1.18 达标，差 > 8 的像素 3.5% 超标。两张图各模糊 2 px 后降到 0.15%，差异是像素级噪点、不是画面差别。4.1 做完 K 帧累积后重测（已写进 4.1 的完成判据）。
  - 清单外改动：`host/ShotScene.tsx` 把场景的渲染器传给 `Lighting`（补偿随渲染器）。视口在 3.2 接入有效渲染器后同样传入。
  - 仍未做：灯光预编译没带 GI 光照 pass 的上下文（`compileAsync` 晚些才建材质，那时的上下文不对），挪到 7.2 处理。

## 2. 场景格式与后端

- [x] 2.1 `creativescene`：`Render` / `renderPatch` 新字段、`decodeScene` 补缺省、`completedScene` 补齐 `render`、`newScene`、`Validate`、`set_render` 提示、`fingerprintRender.Engine`；`creativemedia` 的 `MaxImagePixels`。先改测试：`TestRenderMergesByKey` 的期望、老场景（SQL 剥字段）读取补齐且写入成功、固定 v1 指纹不变、`engine` 只影响 `clay` / `lit`、`resolution` 不影响、`path_tracing` / 未知档位 / `null` 被拒。完成判据：`make check-go PKG=./internal/creativescene/...`、`./internal/creativemedia/...` 绿
  - 两个包的 `make check-go` 都绿。补缺省的函数叫 `completeRenderSettings`，因为 `completeRender` 已是 `render_view` 任务完成的函数名；完整读取判断 `render` 是否齐全用 `view.go` 的 `storedRender.complete()`（design 写的 `renderComplete`）。
  - `path_tracing` 单独提示「还不能选路径追踪，可选 workbench 或 realtime_gi」；空串、`null`、未知值都被拒。
  - `model_test.go` 的 `validScene` 写全 `render`，并加了四个 render 校验用例；`creativemedia/config_internal_test.go` 固定 8192² 可收、8192 × 8193 不收。
- [x] 2.2 `api/openapi.yaml` 两个 schema 加字段，`make generate` 提交 Go 与 TS 生成物；前端读 `render` 的地方去掉对缺字段的猜测，改用 `engines.ts` 的取值函数。完成判据：`make generate-check`、`tsc -b` 绿
  - `engines.ts` 的 `Engine` 取自生成的 `CreativeSceneRender`，`engineOf`、`litLook.ts` 的 `exposureOf` 和属性区的曝光不再兜底缺字段；对应单测改为「服务端读出 0」。
  - e2e 的内存服务端新建场景、参考场景 fixture（六个）补齐 `engine: workbench`、`resolution: 2k`，与真实后端读出一致；画面不变。定向 e2e「reference scenes: each opens and draws」绿；场景单测 227 个全绿。

## 3. 渲染选项卡与有效渲染器

- [x] 3.1 属性区三个选项卡（`SceneEditor.tsx`、`sceneStore.ts`、`scene.css`）与 `panels/RenderPanel.tsx`；`EnvironmentPanel` 去掉曝光；后端提示读 `SceneGPU`。完成判据：e2e「渲染选项卡」用例绿（内容、路径追踪禁用、自动切换、只读预览禁用），现有曝光用例改到新位置后仍绿
  - e2e「render tab: …」绿；「AC-5: exposure」（先点「渲染」选项卡）、「AC-5: with nothing selected the environment shows」、「history lists revisions」仍绿。`tabAfterSelection` 有单测（`creative-scene-interaction.test.ts`）。
  - 渲染后端由视口建好渲染器后写进编辑器 UI 状态（`EditorUI.backend`，`Viewport.tsx` 一行），「渲染」选项卡读它。
  - 「物件」选项卡没有选中时沿用 `Properties` 现有的「选中物件后在这里调整」，没有改成 design 写的「未选中物件」：现有文字更能说明怎么用，也免得改清单外的 `Properties.tsx`。
- [x] 3.2 `render/engines.ts` 的 `effectiveEngine` 接入视口（实时 GI 时 1× 像素比、正交角标）和离屏渲染端（`render_view`、导出）；视口收敛（`LookView` 区分自己续的帧）。先写 `engines.ts` 决策表单测。完成判据：e2e 用例绿——换渲染器后视口、`render_view`、`clay` / `lit` 导出一致，撤销、两个标签页同步、过时提示三种情况（AC-R2-2）；线框、材质预览、`line` 与工作台逐像素相同、正交角标（AC-R2-4）；实时 GI 视口帧计数停在 N、工作台停在 1
  - e2e「renderer in effect: …」绿（SwiftShader）：最终渲染、白模在实时 GI 下帧计数停在 16、工作台停在 1；线框、材质预览换渲染器前后逐像素相同；正交视图角标且停在 1；另一个标签页读到新渲染器、撤销后跟随；离屏出图 `line` 两种渲染器哈希相同，`clay`、`lit` 不同。`effectiveEngine` 决策表有单测。原有「lights: … warms」「AC-1: lights」「export: two cameras」「7 then 5」仍绿，场景单测 229 个全绿。
  - 过时提示三种情况由服务端指纹负责（e2e 的内存服务端不算指纹）：`creativescene` 的 `TestEngineFingerprintsOnlyClayAndLit`（换渲染器只让 `clay`、`lit` 过时，改输出分辨率不过时）与 `TestExposureFingerprintsOnlyLit`。
  - 收敛计数：自己没请求的帧、视角（相机矩阵、尺寸、像素比）变化、场景或显示变化都清零；计数写进测试钩子 `viewportFrames`。视口的像素比改取 R3F 的 `viewport.dpr`（渲染器上的值会被实时 GI 的 1× 改掉），`SceneRenderer` 也核对渲染器当前的像素比。

## 4. 累积与输出分辨率

- [x] 4.1 `gpu.settle()`、异步 `shoot` 与 K 帧累积（`SceneRenderer` 的累积节点、`host/render.ts`）、`exportShots.ts` / `executor.ts` 逐张 await 与按估算的时限（`engines.ts`）。先写单测：时限公式、帧之间超时、`ManualTicker` 跟随新时限。完成判据：隐藏标签页里实时 GI 的多摄像机三种方式连拍两次逐像素相同、与前台一致（`creative-scene-agent-background.e2e.mjs`）；8K 实时 GI 导出期间续租照常（日志或钩子计数）
  - 已完成：`gpu.settle()`（WebGPU 用 `onSubmittedWorkDone`，WebGL2 用 `fenceSync` 加后台计时器轮询）；`SceneRenderer` 的累积节点（景深之后、色调映射之前，半精度两张目标轮换）；`shoot` 异步，前 K − 1 帧之间 `settle`、查丢设备和截止时间（`ShotTimeout` 放在 `gpu.ts`，免得 `exportShots.ts` 连带加载 `.tsx`）；`engines.ts` 的 `framesFor`（暂定 K = 64、K_view = 16）、`estimateMs`、`shotBudgetMs`、`viewBudgetMs`、`jobRenderCapMs`；`renderShots` 逐张 await，每张按估算定时限，整批为 60 s 加各张之和；执行器 `render_view` 用 `viewBudgetMs`（仍是 60 s），助手导出外层上限改为 25 分钟。单测 231 个全绿（新增时限公式、截止时间与帧之间超时）；e2e「real-time GI: 48」「renderer in effect」「export: two cameras」仍绿（前两个的离屏出图改用 `render_view` 的 16 帧，SwiftShader 上省时间）。
  - **确定性（owner 2026-10-21 定，AC-R2-5 已修订）**：实时 GI 改为容差比对（每通道差 ≤ 1，差 > 8 的像素 ≤ 0.01%），工作台仍逐像素相同。隐藏标签页用例先原样跑完 R1 的工作台流程，再回到前台跑实时 GI 一轮、转后台连拍两次、再在 WebGL2 后端连拍两次（避免实时 GI 的长导出让工作台那轮的材质被释放）；fixture 的 `__differ` 在页面里解码比对。本机实测这次连最低位差都没有（隐藏/前台、两次之间最大差 0），前台 9 张实时 GI 约 12 s。用例在 `creative-scene.e2e.mjs`，不改后台助手 e2e（design 的挂载点已同步删去）。原先的观察：`SCENE_E2E_HIDDEN` 的隐藏标签页用例加了实时 GI 一轮。隐藏标签页第一次与前台一致，但连拍两次不完全相同。前台同一页面连续导出实测两种来源：
    - 导出之间材质被释放（引用归零后 1 秒）又重建，three 第二次构建同一材质生成的代码不同，结果有最低位差，线稿个别像素翻转（1 个像素差 16）。工作台导出快，下一次导出总能赶在释放前复用材质，所以 R1 没暴露；实时 GI 的白模和线稿要画好几秒，切到材质光照时材质已释放。把释放延迟临时改长后这一类消失。
    - 另有偶发的最低位差：四次导出中一次，某张实时 GI 材质光照约 1.8 万像素差 1，来源未定位（白模从未出现）。
  - **续租**：e2e「real-time GI: the page keeps its heartbeat through an 8K picture」（`SCENE_E2E_GPU_8K=1`，本机 Metal）：盒子间 8192×5461 实时 GI 白模 40.4 s，后台计时器每秒一次的心跳 38 次，最长间隔 2.1 s（最后一帧加编码），续租每 10 s 一次、租约 30 s。同一张图存成 PNG 有 47.2 MB，超过媒体上传的 25 MB，由 4.2 的 JPEG 解决（4.2 量 JPEG 大小确认）。
  - `forceWebGL` 的 1.2 遗留项挪到 7.1 一起做（届时 K 帧累积和标定都已就位）。
- [x] 4.2 输出分辨率：`framing.ts` 长边按档位、`imageTypeOf` 与上传文件名 / MIME、纹理上限检查、4K / 8K 丢设备以 `host_error` 结束且手动导出提示、`ExportDialog` 显示档位。完成判据：e2e 四档 × 1:1 / 3:2 / 竖幅的尺寸与格式、8K 1:1 能上传成画布节点、压低纹理上限的失败、4K 丢设备不回队（AC-R2-6）
  - 已完成：`engines.ts` 的 `longSideOf`、`imageTypeOf`（4K / 8K 的白模和材质光照为 JPEG 0.95，线稿与 1K / 2K 为 PNG）、`extensionOf`；`framing.ts` 去掉固定的 `exportLongSide`，长边跟随 `render.resolution`；导出逐张按自己的格式编码，上传文件名 `.jpg` / `.png` 与 MIME 对应；`Shooter.maxTextureSize`（`gpuTestHooks.textureLimit` 可压低）不够时出图前就失败，提示「当前设备最大只能导出长边 N px 的图，请选更低的输出分辨率」；≥ 4096 时出图中丢设备提示「渲染设备在按 4K 出图时丢失……可以换低一档再导出」。两种失败带 `hostError`，执行器记 `host_error`、不交回队列（单测）；2K 及以下丢设备照旧回队。`ExportDialog` 显示「长边 X（XK）」。
  - e2e「output resolution: 1K to 8K……」绿：四档 × 1:1 / 3:2 / 2:3 的尺寸与格式；编辑器 8K 1:1 导出上传成画布节点（白模 JPEG 0.6 MB、线稿 PNG 1.3 MB，单方块场景）；压低纹理上限的失败与 4K 丢设备不再放置。8K 部分在本机 Metal 浏览器里跑：SwiftShader 在 8K 会丢上下文（软件渲染的限制，不是产品行为）。「export: two cameras」「device loss: a manual export」「device loss: render_view and export_shots」仍绿。
  - 盒子间、材质场景的 8K 实时 GI JPEG 体积留到 7.2（L4）一起量，4.1 量过同图 PNG 47.2 MB。
  - `host/ShotScene.tsx` 不在挂载点内（3.2 已记），本任务未再动挂载点外的文件。

## 5. 标定

- [x] 5.1 在参考设备（Metal）上标定 N、K（导出与 `render_view`）、`final` 质量档、TRAA 的 `flickerReduction`、累积目标精度，方法见 design「收敛」「累积与出图」；常量写进 `engines.ts`，曲线与图存进 `discovery/research/calibration`。完成判据：残余噪点阈值写进 AC-R2-5 的 e2e 并通过；2K 实时 GI 一张的耗时记录在案
  - 记录见 `discovery/research/calibration.md` 与 `calibration/` 下的对比图。**N = 12**（四组冷启动相邻帧差都 < 0.2 的最小值）。`final` 档保持 2 × 8：3 × 16 噪点低 13%、耗时多 44%。TRAA 的 `flickerReduction` 不影响亮部，保留、不 fork。累积用半精度：K64 时与单精度没有像素差超过 2 级。
  - **修正**：累积改为先预热 N 帧再平均（`AccumulateNode` 的 warm-up，`engines.ts` 的 `drawnFrames`，出图时限按 N + K 算）。时域节点第一帧的 HDR 值极大，原先 K16 的材质光照平均亮度 180，收敛值约 117。
  - **owner 决定（2026-10-21）**：出图的 GI 只做空间滤波（design 的退路，视口不变），K = 64、K_view = 16。design 的 K 规则因噪点按 1/√K 一直下降而达不到。代价是盒子间后墙上部偏亮 25%，图上有低频斑驳，渗色变弱。design「局部设计与偏差」已补记。质量档改为建管线时固定（进入 `Look` 的缓存键，去掉 `setQuality`）；SSGI 的 slice / step 原先一直是 three 的默认 1 / 12，现在两档都按 design 设为 2 / 8。
  - 验证：e2e「real-time GI: a 2K export of the box room is within its residual noise」新增并通过（本机 Metal：材质光照 1.232、阈值 1.5、3.4 s；白模 0.189、阈值 0.3、1.5 s）。「renderer in effect」（帧数改为 12）、「real-time GI: 48」、`SCENE_E2E_HIDDEN` 确定性（隐藏与前台最大差 0，两次之间最大差 1）仍绿。单测 65 个全绿。

## 6. 助手

- [x] 6.1 `scene_tools.go` 三个工具的说明、`instructions/base.md`、`scene_tools_test.go`；`docs/dev/agent-skills-tools.md` 工具表与排空后升级记录、`docs/dev/creative-scene-agent.md`。完成判据：`make check-go PKG=./internal/creativeagent/...` 绿；助手脚本化回合 e2e「换成实时 GI，按 4K 导出」绿（AC-R2-7）
  - 三个工具的说明与 `base.md` 的场景段落按 design 改；`TestSceneToolsDescribeRendererAndResolution` 固定新写法。场景工具说明增长约 0.9 KB，压缩回归用例 `TestCompactionSummaryFallsBackWhenCurrentToolsOutgrowWindow` 的写死上限 40000 超出 129，改为与同文件其他用例一样取 `requestByteCeiling`（C5 同样处理过）。
  - AC-R2-7 分两半：Go 脚本化回合 `TestTheAssistantSwitchesToRealtimeGIAndExportsIn4K`（`set_render` 写入 `engine`、`resolution`，按键合并保留 `mode`；`render_view` lit 截图进入下一步；三种导出放上画布）；页面一半在 `creative-scene-agent.e2e.mjs`「after 「换成实时 GI，按 4K 导出」…」（Metal：实时 GI 的 `render_view` 与工作台不同，导出 clay、lit 为 4096 × 2731 JPEG、线稿 PNG，3 张约 15 s）。`duskDoc` 补齐 `engine`、`resolution`。
  - `make check-go PKG=./internal/creativeagent/...` 绿（新增用例前跑；新增两个用例单独跑绿）。`docs/dev/creative-scene-agent.md` 另补了一条 R1 的渲染器说明：R1 没更新这份文档，渲染端仍写着 WebGL 的 `EffectComposer` 链。

## 7. 验收证据

- [x] 7.1 AC-R2-3 效果用例：盒子间的区域测量（红墙旁地面红绿比、金属球与工作台的差、接触处亮度）写进 e2e，图存进 evidence；切回工作台对 R1 冻结参考图；SwiftShader 的 WebGL2 下实时 GI 出图不报错（AC-R2-8 后半）；`forceWebGL` 下盒子间的实时 GI 白模、材质光照与 WebGPU 在 AC-R1-1 阈值内（AC-R2-8 前半，1.2 遗留）。完成判据：用例绿，evidence 齐
  - 证据在 `discovery/research/effects/`（M4 Pro、Metal），e2e 带 `SCENE_E2E_EVIDENCE=<dir>` 时重新生成。
  - **区域测量**（「real-time GI: the box room bleeds red …」，2K，工作台 → 实时 GI）：材质光照红墙旁地面红绿比 1.005 → 1.105，立方体朝红墙的侧面 0.951 → 1.606，金属球区域与工作台平均差 22.2，光滑地板靠绿墙处绿红比 0.992 → 1.149，立方体正面下部（接地面反弹光）190 → 235；白模立方体脚下地面 133 → 122（更暗），正面下部 100 → 106。断言取这些值留余量（地面红绿比 +0.07、侧面 +0.3、金属球 ≥ 10），图与数值见 `cornell-*.jpg`、`cornell-regions.json`。
  - **交 owner 看的两点**：① 地面红绿比 1.105 低于 1.1 时用递归降噪测到的 1.21，就是 5.1 预料的「出图改空间滤波后渗色变弱」；立方体侧面的渗色反而更强（1.44 → 1.61）。② 材质光照下立方体脚下的地面被聚光灯照到饱和白（两种渲染器都是 247 以上），工作台的阴影本来就是纯黑，实时 GI 只会加反弹光，所以「接触处比工作台更暗」在材质光照里量不出来，只记录不断言；白模里成立。白模没有反射由 `realtimeGI.ts` 保证（`reflections` 只给 `lit`）。
  - **切回工作台对 R1**（`SCENE_E2E_WORKBENCH=<dir>`）：R1 没有存参考图，用 develop（R1 合入后的代码）在同一台机器、同一浏览器先导出六个参考场景的 27 张图（三种导出加正交白模），再用本分支导出比对：27 张全部逐像素相同（平均差 0，差 > 8 的像素 0），`workbench-vs-r1.json`。参考图本身留在本机，不入库。
  - **WebGL2**：SwiftShader（没有 WebGPU，走 WebGL2 后端）下盒子间实时 GI 的白模和材质光照出图无报错；Metal 上强制 WebGL2，2K 导出与 WebGPU 比：白模平均差 0.07、差 > 8 的像素 0%，材质光照 0.50、0.13%，在 AC-R1-1 的限值（1.5 / 1%）内，也在 R1 收紧后的阈值内。1.2 时材质光照的 3.5% 来自 16 帧的像素级噪点，K 帧累积后消失。
- [x] 7.2 AC-R2-9 性能：实时 GI 视口帧时间（盒子间、R0 性能场景，1× 像素比）、停止操作到停止重绘的时间、四档两种渲染器的导出耗时、工作台视口帧时间对 R1。完成判据：数据存进 `discovery/research/perf`，不满足的逐项列出交 owner；结清 L4（8K 实时 GI）
  - 数据与结论见 `discovery/research/perf.md`（e2e「R2 performance」，`SCENE_E2E_PERF_R2=<dir>`）：五项都满足。实时 GI 视口最终渲染在 R0 性能场景平均 10.6、p95 16.8 ms；停止操作后再画 12 帧（约 0.1 s）停下；2K 实时 GI 材质光照 3.4 s、白模 1.5 s；工作台视口帧时间与 R1 相同。
  - **L4**：本机 8K 实时 GI 白模 43.7 s、材质光照 95.3 s，JPEG 1.3、3.5 MB，没有丢设备；峰值内存与低配设备未测。按 design 交 owner 确认是否保留 8K 档位。
  - **灯光预编译（1.2 遗留）**：实时 GI 的最终渲染里加灯，视口卡 5 s，且每种材质报一次「Color target has no corresponding fragment stage output」。原因是 three r186 的 `compileAsync` 晚一两个任务才创建替身的 render object、构建材质，读到的渲染目标、MRT 和上下文已被帧重置：材质按空目标生成，没有 MRT 输出，被 WebGPU 拒绝；预通道和光照 pass 的替身又落在同一个缓存键上（缓存按材质和上下文，不含 MRT），光照 pass 的真实网格只好在下一帧同步编译。`sceneRenderer.ts` 的 `asItsPass` 只对替身网格，在 render object 创建和材质开始构建这两个同步调用期间设上所属 pass 的目标（1×1 克隆）、MRT 和上下文（光照 pass 带拆分标记），调用完立即还原。修正后加灯最长一帧 116 ms、无报错；新增回归用例「real-time GI: a light added in the rendered view…」。工作台同走这条路径，R1 的 AC-9 复跑加灯 117 ms、删灯 300 ms（R1 150、266 ms）。
- [x] 7.3 AC-R2-10 重绘对照：同机位工作台与实时 GI 各导出一组，用 gpt-image-2.5 重绘（凭证只经环境变量，拿不到时交 owner）；AC-R2-11 Safari 人工检查。完成判据：样例与记录存进 evidence，等 owner 结论
  - **owner 2026-10-21 决定延后**：重绘对照的效果和 Safari 检查都由 owner 后续验证，Safari 暂不作为重点支持；本次只交付原图与记录模板（`discovery/research/gi-repaint.md`），结论不在本次验收范围内，验收时记为后续安排。
  - 样例与记录：`discovery/research/gi-repaint.md`（C5 AC-10 的棚拍布景和三台机位，工作台与实时 GI 各导出白模、线稿、材质光照；重绘组合、提示词和结果表）。本机没有图像模型凭证，gpt-image-2.5 重绘与 Safari 检查交 owner（见上，已延后）。
  - **生成样例时发现的缺陷（已修）**：实时 GI 的材质光照图里，背对太阳的背景墙满是颗粒、偏灰，右上方有一条和画框平行的直边，灯罩上有锯齿暗斑。逐项关掉 GI、AO、接触阴影、反射对照，只有关掉接触阴影的那张干净；再改 `SSSNode` 的厚度、追踪长度、质量对照，确定是自遮挡：墙背对主光，朝主光的光线一出发就在墙内，被判为遮挡；合成又把接触阴影乘在整个直接光上，聚光灯和落地灯照在墙上的光也被压暗；噪点来自每帧的随机偏移，直边是光线走出画面后不再判定。`realtimeGI.ts` 改为接触阴影按法线与主光方向的点积淡入（N·L ≤ 0 时为 1，×4 后夹到 0–1），只作用于朝向主光的表面；同时 `SSSNode` 的光线方向（灯位置减目标位置）不做单位化，太阳离目标 60 m 时追踪长度会是 6 m 而不是 0.1 m，改为用一个每帧对齐主光方向、距原点单位长度的代理光源。盒子间（主光是自上而下的聚光）的区域测量、残余噪点数值与修正前相同；7.1 的结论不变。design「局部设计与偏差」补一条。
- [x] 7.4 owner 验收前发现的两个问题（2026-10-21）：① 实时 GI 的最终渲染里，摄像机、空物体等辅助对象参与了渲染；② 渲染器应只作用于最终渲染视口和最终导出，白模、线框、材质预览不受影响。完成判据：辅助对象不进 AO 与实时 GI 的预通道（e2e 红→绿）；白模、`clay` 导出、`render_view` 的 `clay` 总按工作台；规格、契约与助手说明同步
  - **辅助对象**：灯光图标、摄像机视锥（含点选用的隐形盒）、空物体十字、分组坐标轴都在 layer 0，进了实时 GI 的预通道深度，SSGI 和接触阴影把它们当遮挡物，在周围地面画出大片颗粒状黑斑（owner 截图里树干旁那团）；工作台的 AO 预通道也算上了它们。改为放进 aside 层（与网格、选中描边同层）：照常画进图里，不进预通道；视口点选的射线打开 aside 层，点选行为不变。新增 e2e「real-time GI: camera frustums and empties are drawn in the rendered view but neither shade nor occlude」（本机 Metal，地面上 0.15 m 一台摄像机、0.1 m 一个空物体，与没有它们的同一视图比）：修正前 1.76% 的像素不同（两团黑斑），修正后 0.05%（只剩视锥和十字的线条）。SwiftShader 下修正前也只差 0.05%，所以用例跑在 Metal 上。
  - **渲染器作用范围**：`effectiveEngine` 只对最终渲染（`rendered`）和 `lit` 返回场景的渲染器，白模（`solid`、`clay`）改回工作台；正交角标只在最终渲染时出现；渲染选项卡在非最终渲染时提示「渲染器只作用于「最终渲染」和材质光照导出」。白模那一支实时 GI 没有调用方了，删去 `Frame.reflections` 与 `realtimeGI` 的 `reflections` 选项，SSR 常开；材质光照的残余噪声仍是 1.232，与改动前相同。指纹的 `engine` 只计入 `lit`（`TestEngineFingerprintsOnlyLit`），换渲染器后已有白模图不再提示过时。助手的 `set_render`、`render_view`、`export_shots` 说明和 `base.md` 改为「只作用于材质光照」。
  - **测试随之调整**：「renderer in effect」里白模与线框、材质预览一样逐像素等于工作台，离屏 `clay` 两种渲染器哈希相同；残余噪声、区域测量、WebGL2 两条、性能和 8K 心跳只测 `lit`。7.1 的白模接触测量（133 → 122）和 7.2 的「实时 GI 白模」数据是改动前的记录，白模现在就是工作台；`cornell-realtime_gi-clay.jpg` 等旧图留作历史。AC-R2-3 的「接触处比工作台更暗」原先只在白模里成立，盒子间的材质光照里量不出来（见 7.1），交 owner 看图。
  - **重绘样例**：实时 GI 的白模样例删去，组 2 改为白模 + 线稿 + 实时 GI 材质光照（`gi-repaint.md`）。
  - 验证：上面的 e2e 红→绿；「renderer in effect」「a 2K export of the box room is within its residual noise」绿；`effectiveEngine` 单测、`tsc`、`oxlint` 绿；`go test ./internal/creativescene -run Fingerprint`、`./internal/creativeagent -run 'TestSceneTools|Compaction|TestTheAssistantSwitches'` 绿。
  - 偏离 Epic 已批准的「渲染器同时作用于白模和最终渲染」（Epic proposal 修订记录 2026-10-21、design「渲染器与显示模式」、contracts `scene.render.engine`）：Epic 修订待 owner 批准；本子项的 proposal（AC-R2-2/3/4/8）、specs、design 已按新口径改。

- [x] 7.5 代码审计逐项处理（2026-10-21）。完成判据：确认的问题修正并有回归检查，不改的写明理由
  - **雾**：光照 pass 带雾，合成用带雾的 `output` 减去不带雾的间接光，再加回不带雾的 AO、GI、反射和接触阴影。改为光照 pass 不画雾（`GILightingPass` 暂时摘掉场景的线性雾，其他种类的雾留给材质），合成后按预通道深度统一叠雾。新增 e2e「real-time GI: the fog covers the indirect light as it covers the rest」（本机 Metal，雾止于 0.02 m，放大 4 倍的白盒子对红盒子）：修正前 1.09% 的像素不同（盒子在全雾里透出来，地面有暖色渗色），修正后 0.00%。
  - **SSGI fork**：`// fork: end` 和 `this._material.needsUpdate = true;` 写在了同一行，后者被注释掉；`dispose()` 的文档注释也被 fork 块切开。按行拆开后，unfork 对照仍等于上游；对照测试新增一条：`fork: was` / `fork: end` 必须独占一行（旧文件上红，修正后绿）。
  - **契约措辞**：`api/openapi.yaml` 里 `render.engine` 的说明改为「只作用于材质光照……白模、线稿、线框、材质预览和正交视图总按 workbench」，`make generate` 更新 `api.gen.go`、`schema.d.ts`。
  - **主光**：主光不再进管线的键，接触阴影每帧现查主光，没有主光时不追踪、合成里权重为 0。太阳落山、换天空、两盏聚光强弱交替都不再重建整条管线和时域历史。聚光的距离改按世界坐标算（单测补上「挂在分组下」一例，红→绿）。主光仍是每画一帧遍历一次场景，数百个物件只要微秒级；视口只在交互和收敛时出帧，不再另做场景变化的通知。
  - **辅助对象**：只排除出预通道还不够，它们仍画在光照 pass 里，SSGI、SSR 会把它们的颜色当成背后表面的光。改为视口里单独一个辅助对象 pass（8 位颜色，导出不建），雾之后、TRAA 之前按深度叠上（深度不大于预通道深度处可见，与同一 pass 的深度测试相同；代价是玻璃后面的辅助对象也画在玻璃上面）。审计说的「网格线处 split 降到约 0.5」不成立：网格在 `ambient` 等附件里写的是 alpha 0，按画面的混合方式保留底下的值（玻璃同理，见 `realtimeGI.ts`「Blended as the picture is」），现在网格也不进光照 pass 了。
  - **低优先级**：`exportShots.ts` 的 `budgetOf` 去掉对线稿的特判（`effectiveEngine` 已返回工作台）；截止时间本来就和 `host/render.ts` 一样用 `performance.now()`，注入的 `now` 只计导出耗时，补注释说明。不改的几项：`gpu.ts` 引用 `host/ticker.ts`，ticker 是不依赖其他模块的计时工具，搬位置要改 5 处引用，收益只是目录上的分层；`fixedNoise()` 每次建管线生成 3 张 64×64 噪声纹理，耗时约 1 ms，按设备缓存要另管释放；8 位打包法线、`installGISplit` / `asItsPass` 对 three 内部的依赖由单测钉住，升级 three 时对照。
  - **未验证、交 owner**：8K 的峰值内存（R0 记录约 9 GB），以及 Safari 的画布尺寸上限：8K 出图直接把画布设到 8192 × 5461，Safari 若超出画布上限可能静默出空图，纹理上限检查管不到。AC-R2-11 的 Safari 检查加上 4K、8K 材质光照导出（`gi-repaint.md`）。导出计划内复用渲染器、只重置时域状态，是后续优化，不在本次范围。
  - 验证：上面两条红→绿；「camera frustums and empties…」0.05%、「renderer in effect」「48 pictures in a row」「a 2K export … residual noise」（仍是 1.232）「a light added in the rendered view」（最长一帧 108 ms）都绿；GI 单测 11 条、导出相关单测、`tsc`、`oxlint` 绿。视口帧时间没有重测（多一个只画辅助对象的 pass）。
