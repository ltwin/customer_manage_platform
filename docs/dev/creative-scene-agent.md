# 场景助手开发说明（C3）

助手用五个场景工具读场景、改场景、写几何代码生成模型、截图回看、导出分镜。编译模型、截图和导出要用浏览器（沙箱、WebGL），所以服务端把这几类工作派成**场景客户端任务**，由打开着的创意空间页面领取执行，运行在此期间挂起。本文记实现事实、调试方法与已知边界；设计权威是 epic `scene-editor-3d` 与 C3 的 design（`.lti/features/2026-10-15-scene-editor-3d/features/2026-10-17-scene-agent-tools/`），工具登记见 [Agent Skills 与 Tools 运行契约](agent-skills-tools.md#场景工具c32026-10-18)。

## 模块分工

| 位置 | 负责 |
|---|---|
| `backend/internal/creativescene/jobs.go` | 任务表的读写：建任务（同 `(run_id, tool_call_id)` 返回已有任务）、认领、续租、列出、取消；状态迁移日志 |
| `creativescene/jobs_complete.go` | 完成任务：准入复查、烘焙结果在保存点里写入场景、截图只校验不消费候选、冲突与不合规结果 |
| `creativescene/jobs_sweep.go` | 清扫：租约回收、期限过期、删除旧任务；creative-worker 每 15 秒一轮 |
| `creativescene/agent.go`、`run_changes.go` | 工具的领域入口（读、改、撤回、冲突描述）与「本轮改动」清单 |
| `backend/internal/creativeagent/scene_tools.go` | 四个工具的参数、结果与挂起 |
| `creativeagent/scene_waiter.go` | `JobWaiter` 的实现：`Admit`、`Settle`、`Abort` |
| `frontend/src/creative-scene/host/` | 渲染端：认领循环、执行器、离屏截图、Worker 节拍、关闭提醒 |
| `frontend/src/creative-scene/agentActivity.ts` | 编辑视图里助手工作的高亮、占位、状态栏 |
| `frontend/src/creative-canvas/editor/AgentScene.tsx`、`agentScene.ts` | 助手面板的截图与本轮改动清单 |

依赖方向是 `creativeagent → creativescene`：完成任务时需要回到运行，经 `creativescene.JobWaiter` 端口调用，server 和 creative-worker 启动时用 `SetJobWaiter` 注册。

## 任务的生命周期

```
queued ──认领──> claimed ──完成──> succeeded / failed
   │               │ 租约过期（第 1、2 次）回 queued；第 3 次转 failed（host_error）
   └── 期限到（30 分钟）──> expired        运行结束时 ──> cancelled
```

- 种类：`bake_model`（编译代码模型、上传 GLB 与缩略图、写入场景）、`render_view`（截一张图）、`export_shots`（C4：按摄像机出 clay 和线稿，节点在完成事务里放到画布上；C5 起可含 `lit`）。`render_view` 的请求带 `mode`（`clay` / `lit`，C5）。R2 起两种任务按场景的 `render.engine` 出图，导出长边按 `render.resolution`。
- 租约 30 秒，页面每 10 秒续一次；续租不会超过期限。
- 新建与进入终态会触发画布通知（账号级），认领和续租不通知。页面靠通知知道有新任务。
- 终态满 30 天的任务由清扫删除。

### 完成接口的结果

| 情况 | 响应 | 页面怎么做 |
|---|---|---|
| 成功写入，或任务失败（编译错误、冲突、结果不合规、导出无法放置） | 200，`state` 为 `succeeded` / `failed` | 保留上传的产物：冲突的烘焙要靠它重试 |
| 租约已丢（被别的页面接手、任务结束） | 409 / 404 | 丢弃产物 |
| 暂存上传已失效 | 409 `creative_upload_unavailable` | 重新上传一次再交 |
| 网络错误、5xx | — | 用同一个 `operation_id` 重试 3 次，仍不行就放手，等租约过期回队 |
| 成功的结果被 400 拒绝 | 400 | 改报 `host_error`，避免反复回队 |

失败码：`compile_error`（语法、运行或模块不合规，行列号写在 message 开头「第 N 行第 M 列：」）、`compile_timeout`、`limit_exceeded`、`render_error`（摄像机已删等请求本身的问题）、`host_error`（沙箱没启动、WebGL、缩略图、上传、截图超时、执行器代码块加载失败）。

## 运行的挂起与恢复

- `write_model` / `render_view` / `export_shots` 在运行的写事务里建任务，返回 `errParked`；`callTool` 把它变成 Eino 的 interrupt，步骤保持 `prepared`。同一批里其他调用照常执行，挂起不停批。
- 本次执行结束时，`completeAttempt` 看任务是否都已终态：是就直接入队继续，否则运行进入 `waiting_client`，`wait_reason = client_job`。等待期间不占写入槽位，补充输入不会唤醒它。
- 任务完成的事务里，`Settle` 按工具成功分支的顺序写步骤结果，再取槽位入队；取不到槽位时运行留在 `waiting_client`、`wait_reason = slot_busy`，清扫在槽位空出后恢复它。
- `Admit` 在完成事务里复查运行（能力、期限、未取消、Skill 未停用、consent 未撤回），锁序为项目 → 槽位 → 网关组 → 运行 → Skill → consent。复查失败时整笔回滚，另开事务 `Abort` 结束运行，任务随运行取消，场景不会多出修订。
- 崩溃后从检查点重跑时，工具按 `(run_id, tool_call_id)` 找到已建的任务，不会重复建。

模型读到的结果：烘焙成功 `{status: "succeeded", content_revision_id, object_ids, written_revision, …尺寸、三角面}`；截图 `{image_revision_id, content_revision_id, scene_revision, width, height}`，图片随下一次请求作为图片输入；导出 `{status: "succeeded", scene_revision, change_id, nodes:[{camera_object_id, pass, node_id}]}`，不带图片；失败 `{status: "failed", error: {code, message}}`；冲突带 `retry_job_id` 与变化说明；过期提示助手转告创作者打开页面。

## 渲染端（前端）

### 认领循环

- 每个页面一个，由 `WorkspacePage` 的 `useSceneHost(account)` 启动（`host/launcher.ts`，不含 three）。场景编辑视图不另开循环，只 `prefer(documentID)` 让本场景的任务先领。
- 触发：挂载、画布失效通知（500 ms 节流）、回到前台、恢复联网；做完一个任务立即再领。认领遇到网络错误不重试，等下一次触发。
- 第一次认领前探测设备：WebGL2、`Worker`、`(pointer: fine)`、窗口宽度 ≥ 1024。不满足就不认领，避免平板上的画布页领走任务再失败。
- 执行器（`host/pageExecutor.ts`，含沙箱、three、截图）在认领到第一个任务时 `import()`，加载期间循环自己续租。代码块加载失败时本页标记为不能执行、不再认领，并把这个任务以 `host_error` 报回。
- 页面持有任务时，头部常驻「助手正在用这个页面……，关闭页面会中断」；本页是浏览器里唯一的创意空间标签页时，关闭会弹确认（`host/leaveGuard.ts`，标签页之间用 BroadcastChannel `creative-workspace` 互相计数）。

### 执行器与截图

- 计时（续租、上传轮询、沙箱超时）都用 `host/ticker.ts` 的 Worker 节拍，隐藏标签页里的页面计时器会被浏览器降频。
- 截图（`host/render.ts`）：React Three Fiber 的 `createRoot` 配一个不挂到页面的 canvas，`frameloop: 'never'`，组件提交后手动渲染一帧，导出长边 1024 的 JPEG（与缩略图一样用 `canvasBlob` 同步编码，不用 `toBlob`），再 `unmount` 释放 WebGL 上下文。单独的 root 必须先 `extend(THREE)`，否则灯光等元素报错、提交永远不来。截图不画灯光和摄像机图标。整体 60 秒上限，超时报 `host_error`。
- 取景（`host/framing.ts`）：视场角按画面长边对应传感器长边（`cameraOptics.verticalFov`，C4 起；与 Blender 的 Auto 适配相同），默认传感器下 3:2 横幅与原先一样，竖幅与横幅同焦距拍到的范围一样大；`viewpoint` 是全画幅 4:3；预设（总览、俯视、正视、侧视）按可见物件（不含地面、灯光、摄像机）的包围盒取景，留 1.1 倍边距。摄像机的取景还带光学参数：对焦物件存在时按它原点沿视线的深度对焦，否则按 `focus`，夹在 near/far 之间。
- 导出与景深回看（C4，`host/render.ts` 的 `openShooter`）：一次导出共用一个离屏根和 WebGL 上下文，模型只加载一次，逐镜头移动同一台相机；结束后 `forceContextLoss`。`clay` 是 `EffectComposer`（4 倍 MSAA）→ `RenderPass` → `GTAOPass` →（景深）→ `OutputPass`；景深在 `render/dofPass.ts`，按物理弥散圆（与 `cameraOptics.blurPixels` 同一公式），先算 16 像素瓦片的最大弥散圆，再在半分辨率按 Vogel 圆盘聚合，只收自身弥散圆覆盖到中心的样本，位于中心之后的样本最多影响到中心自己的弥散圆。线稿在 `render/linePass.ts`，2 倍分辨率的视空间法线与深度做边缘检测。导出长边 2048 PNG（R2 起按 `render.resolution`，见下）；`render_view` 也走 clay（长边 1024 JPEG），`depth_of_field` 只加虚化。编辑器视口用同一个 `SceneRenderer`（`render/sceneRenderer.ts`） 出每一帧（`Viewport.tsx:LookView`，`useFrame` 优先级 1）：网格不进 AO 的法线深度，变换手柄在整条链之后单独画；相机视图打开「景深预览」时加虚化，弥散圆按取景框长边换算。编码仍用 `canvasBlob`：助手的导出多在后台标签页执行。
- 材质光照（C5）：`openShooter(scene, models, look)` 按 `clay` 或 `lit` 挂载 `ShotScene`（`lit` 即视口的「最终渲染」：材质、灯光物件、天空当环境光照、阴影与 AO），挂载后 `gl.compileAsync` 预编译材质；`setLook` 换一种再编译。`lit` 出图前把 `toneMappingExposure` 设为 `2^render.exposure_ev`，出完复原。导出先出全部摄像机的 clay 与线稿，再切一次 `lit` 出全部材质光照图，切换超过 60 秒按失败处理；`render_view` 按请求的 `mode` 直接以那种方式打开。
- 视口的显示模式（C5，`Viewport.tsx:LookView`）：线框直接 `gl.render`；白模、材质预览、最终渲染都走 `SceneRenderer`，只换挂载的光照（`objects/Lighting.tsx`）与材质。最终渲染里新增灯光、或灯光开关投射阴影时，three.js 要把所有受光材质重编译一遍（AC-9 场景约 2 秒）；`SceneLight` 先让这盏灯不亮，用 `compileAsync` 在驱动线程上编好再显示，卡顿降到 0.1 秒左右。截图不需要：出图前已整体编译。
- 渲染器（R1 起，`render/gpu.ts`、`render/sceneRenderer.ts`）：上面的 WebGL 链已换成 `WebGPURenderer` + TSL 的 `RenderPipeline`，WebGPU 不可用时退回它的 WebGL2 后端；画面与原先在阈值内一致，组成以 `sceneRenderer.ts` 头注释为准。
- 渲染设置（R2，`render/engines.ts`）：场景的 `render.engine` 决定最终渲染和材质光照用工作台还是实时 GI（`render/realtimeGI.ts`：SSGI、SSR、接触阴影、TRAA）；白模、线框、材质预览、线稿和正交视图总是工作台（`effectiveEngine`）。灯光图标、摄像机视锥、空物体等辅助对象在 aside 层，画进视口但不进 AO 与实时 GI 的 prepass。实时 GI 的一张图先画 12 帧让时域节点稳定，再平均 K 帧（导出 64、`render_view` 16；工作台 1 帧，`drawnFrames`）；出图（`final`）的 GI 只做空间滤波，视口用递归降噪。帧之间 `gpu.settle()` 等 GPU 做完，查设备丢失和截止时间；单张时限按估算的 8 倍、至少 30 秒，`render_view` 至少 60 秒，助手导出整批以 25 分钟为上限。导出长边按 `render.resolution`（1K–8K），4K、8K 的 clay 与 lit 编成 JPEG 0.95，其余 PNG；长边超过设备纹理上限时出图前失败，4K、8K 出图时设备丢失以 `host_error` 结束、不回队列。
- 白模与色调映射（W1，scene-display-look）：白模（视口、正交视图、`render_view` 与导出的 `clay`、资产库预览的「白模」）用固定的工作室光（`render/studio.ts`），与 Blender 实体模式的默认一致：一盏平行主光从视角左上方照来，`SceneRenderer.render` 画之前用 `aimStudio` 按摄像机转好（资产库预览在 `useFrame` 里调用），加半球补光和一点环境光，不投影，保留 AO，背景是中性灰 `STUDIO_BACKGROUND`；不读场景的天空、太阳、环境光、雾和灯光物件。所以 `clay` 的渲染指纹不计 `environment`，`clay`、`lit` 另带常量键 `view_transform: "agx"`（`creativescene/fingerprint.go`）：W1 之前的白模和材质光照导出各显示一次过时，线稿不受影响。输出节点除线框外都按 AgX 色调映射（线框保持 ACES Filmic，画面不变），资产库预览在 `onCreated` 设 AgX；最终渲染的常量在 AgX 下重新标定（`render/litLook.ts`）。
- 物理天空与手动曝光（W2，scene-physical-sky）：
  - 天空：`objects/Lighting.tsx:PhysicalLighting` 用 Hillaire 2020 的三张 LUT（`world/atmosphere/luts.ts`：透射率 256 × 64、多次散射 32 × 32、天空视图 384 × 216，RGBA16F）画背景，用不带太阳盘的同一节点生成天空光照（PMREM，200 ms 节流）。CPU 上另有一份同样的大气（`world/atmosphere/cpu.ts`）算太阳透射率和水平照度 E；两份只共享 `constants.ts`，e2e「W2 atmosphere drift」把 GPU 的 LUT 和 `sampleSkyView` 的方向采样与 CPU 对照。太阳位置由 `world/astro.ts`（`astronomy-engine`）按地点、日期、当地时刻算，换算 UTC 用 Temporal 的 compatible 规则。
  - 单位：太阳约 12.8 万 lux（大气顶）× 亮度 × 色温，灯光坎德拉，自发光 cd/m²。天空视图和天空光照按 s_env（由 E 定的环境曝光）存进半精度，与摄像机无关。
  - 预曝光：每次出图前 `SceneRenderer.render` 调 `world/exposure.ts:applyExposure(scene, k)`，灯光写 `userData.baseIntensity × k`、天空光照 `k / s_env`、雾 `雾色 × E / π × k`、全局 uniform `preExposure = k`（天空背景、太阳盘、自发光读它），画完把 `preExposure` 写回 1。k 由 `render/litLook.ts:frameExposure` 求：物理天空下 `1 / (1.2 · 2^EV100)`，场景摄像机的 EV100 由 ISO、快门、光圈得出（`host/framing.ts:cameraEV100`），临时视点、预设取景和视口自由视角用 `render.exposure_ev100`，`toneMappingExposure` 为 1；其他天空 k = 1，照旧按 `2^exposure_ev`。`render/gpu.ts` 在模块加载时给所有节点材质的输出钳到 60 000（SwiftShader 溢出会写 Inf）。
  - 太阳参考：`creativescene/sun.go` 用 NOAA 公式在服务端算一份，物理天空的地点模式下随 `read_scene` 和写了环境的 `edit_scene` 结果给助手（`sun`），不存储、不进渲染指纹；口径与环境选项卡显示的一致（日出日落按 −0.833°，黄金时刻 −4°…6°，蓝调时刻 −6°…−4°）。

### 编辑视图

`SceneEditor` 打开时和画布通知后（1 秒节流）读任务列表和最近修订（先读最近 20 条，之后只读 `since_revision` 之后的）：

- 描边（`look.ts` 的 `agentEdge`）：进行中烘焙要替换的物件，以及 60 秒内助手修订改过的物件；物件列表行尾标「助手正在修改」。选中、编辑这些物件时状态栏提示「助手正在修改此物件，你的修改可能让它重做」，不阻止操作。
- 助手新增的模型在目标位置显示 1 m 的半透明占位，挂在目标父物件下，不可拾取。
- 状态栏：「助手正在生成「名称」…」。
- 编辑器把场景和选中写进 `creative-scene/context.ts`，面板发消息时作为 `scene_context` 带上；场景在回收站或已删除时不提供。

### 助手面板

- `render_view` 的工具行下显示截图：从工具详情的结果取 `content_revision_id`，签 `original` 角色的展示票据（截图长边 1024，没有展示图）。截图是暂存上传，过期后显示「截图已不可用」。
- 运行结束后，从 `edit_scene` / `write_model` 的参数里收集场景，逐个读「本轮改动」：每处可「恢复这一处」，整体「撤销本轮」。提交走通用命令接口，写入者是创作者。撤销本轮分多批时按顺序提交，遇到冲突就停下并重读清单，提示撤销了一部分。

## 接口

| 接口 | 用途 |
|---|---|
| `POST /creative/scene-jobs/claim` | 认领最早的任务；带 `document_id` 时只领该场景的；`kinds` 过滤；没有时 204 |
| `POST /creative/scene-jobs/{id}/lease` | 续租 |
| `POST /creative/scene-jobs/{id}/complete` | 完成，`Idempotency-Key` 等于 `operation_id` |
| `GET /creative/documents/{id}/scene-jobs` | 场景未结束的任务（最多 50 条，不含代码与认领信息），供编辑视图 |
| `GET /creative/documents/{id}/run-changes?run_id=` | 一次运行对场景的改动与恢复命令 |
| `GET /creative/documents/{id}/revisions?run_id=&since_revision=` | 修订列表的过滤（接口原有，C3 起前端使用） |
| `POST /creative/documents/{id}/scene-exports` | 摄影师手动导出（C4）：放置导出图节点；助手导出不经这里 |

## 调试

### 日志

- 每次状态迁移一条 `creative scene job`：`job_id`、`document_id`、`kind`、`from`、`to`、`attempt`；有运行时带 `run_id`，有标签页持有时带 `editor_session_id`，失败带 `code`；`in_state` 是在离开的那个状态里待了多久（`queued` 是等页面领取，`claimed` 是页面执行），`since_created` 是从建任务起的总时长。按 `job_id` 能跟住一个任务在几个标签页之间的往返。
- 每轮清扫一条 `creative scene job sweep`：`requeued`、`host_errors`、`expired`、`deleted`、`aborted`；出错时是同名的 Warn 日志带 `error`。

### 查库（只读）

```sql
-- 一个场景最近的任务
-- editor_session_id 是认领它的标签页（每个页面一个随机 ID）
SELECT id, kind, state, attempt, run_id, editor_session_id, lease_until, deadline, created_at
FROM creative_scene_jobs WHERE document_id = $1 ORDER BY created_at DESC LIMIT 20;

-- 在等页面的运行和原因
SELECT id, state, wait_reason, deadline_at FROM creative_agent_runs WHERE state = 'waiting_client';
```

### 常见现象

| 现象 | 先看 |
|---|---|
| 运行一直「等待页面执行」 | 有没有开着的创意空间页面；页面是否通过设备探测（窄窗口、触屏、没有 WebGL2 不认领）；任务是否在 `claimed` 反复回队（日志里 `attempt` 增长） |
| 任务第 3 次以 `host_error` 失败 | 页面在执行中关闭或崩溃了三次；看浏览器控制台与 `code` |
| 运行停在 `waiting_client` / `slot_busy` | 同一会话里另一个运行占着写入槽位（槽位按会话划分），它结束后清扫会恢复 |
| 截图显示「截图已不可用」 | 暂存上传已过期回收，属预期 |

浏览器里：认领到第一个任务时 Network 面板会出现 `pageExecutor` 代码块；之前页面不应加载 three（C2 的 e2e「opening the canvas page loads no three.js」守着）。

## 测试

- 单测：`node --test --experimental-transform-types scripts/creative-scene-agent.test.ts`（执行器、取景、认领循环、关闭提醒、编辑视图的活动、面板的清单逻辑）。
- 浏览器：先在 `frontend/` 起 `npx vite --port 5179 --strictPort`，再跑
  - `node scripts/creative-scene-agent.e2e.mjs`：截图、画布页按需执行、关闭提醒、设备探测、面板截图与本轮改动、`scene_context`（`SCENE_AGENT_E2E_ONLY=<名称片段>` 只跑匹配项）；
  - `node scripts/creative-scene.e2e.mjs`：编辑器，含高亮、占位、优先认领本场景、编辑器上下文（`SCENE_E2E_ONLY=`）；C5 起含最终渲染的标定检查（`calibration`）；W2 的检查都以 `W2 ` 开头（`SCENE_E2E_ONLY='W2 '`），`SCENE_E2E_W2_METAL=1` 让防漂移对照跑在本机 GPU 上，`SCENE_E2E_W2_PICTURES=<目录>` 导出物理天空参考场景的图，`SCENE_E2E_W2_SKIES=<基线目录>` 逐像素核对其他天空不变；
  - 帧时间（AC-9，只在点名时跑，需要本机 GPU）：`SCENE_E2E_PERF=1 SCENE_E2E_ONLY=AC-9 node scripts/creative-scene.e2e.mjs`，用 Metal 后端的无头 Chromium、1600 × 1000、2 倍像素比，打印四种显示模式的平均与 p95 帧时间和加灯、删灯的最长一帧。
- 后端：`make check-go PKG=./internal/creativescene/...`、`PKG=./internal/creativeagent/...`。
- 实测（只在点名时跑，经 `TestCreativeEditorBrowser` 起真实 API、媒体流水线和 PostgreSQL；测试端点 `/__test/scene-job` 像工具那样建任务，替身运行记下结果交回的时间）：
  - 时延：`CREATIVE_EDITOR_URL=http://localhost:5179 CREATIVE_EDITOR_SCRIPT=creative-scene-agent-latency.e2e.mjs go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser -v`（`SCENE_AGENT_LATENCY_RUNS` 调样本数）；
  - 后台标签页：`CREATIVE_EDITOR_URL=http://localhost:5181 CREATIVE_EDITOR_SCRIPT=creative-scene-agent-background.e2e.mjs go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser -v -timeout 30m`。脚本自己在 5181 起 Vite 并启动本机 Chrome（窗口在屏幕外）：Playwright 会让它控制的页面一直可见，测不出后台。

## 已知边界

- 后台标签页：真实 Chrome 隐藏 6.5 分钟后，烘焙与截图的耗时和前台相当（2026-10-18 实测 1.05 倍、1.00 倍）。前提是所有计时走 Worker 节拍、图片同步编码（`canvasBlob`）：Chrome 后台会把页面主线程的计时器和 `toBlob` 的空闲编码推迟到每秒乃至每分钟一次，新增等待时不要用页面计时器。
- 「本轮改动」不能单独恢复层级调整（`hierarchy_not_restorable`）：C5 起助手可以编组、改挂，被改挂的物件和它进出的分组都不能单独恢复，按前后快照判断；同一物件被运行写过两次、中间有别人改过时按保守处理，记为别人改过。运行写出的修订超过 500 个时接口返回 400，改用历史版本。
- 五个工具的定义每次模型请求都会带上；C4 加长了 `edit_scene` 的说明（摄像机参数、景深与 `look_at`），C5 又加了材质目录、打光与曝光建议（约 2.8 KB）；W2 加物理天空的字段、预设、太阳参考与两段曝光建议，`edit_scene` 从 7.7 KB 到 11.0 KB（约 1,900 字），`read_scene`、`render_view` 各加约 0.1 KB。`compaction_regression_test.go` 里最小的测试窗口随之从 8000 调到 9000 token。
- 相机视图不跟随摄像机的滚转（视口保持 Z 朝上）；用 `look_at`、「添加摄像机」或 Ctrl+Alt+0 摆的机位没有滚转。
- 编辑视图不区分任务排队与执行中（认领不发通知），两种状态显示一样。
