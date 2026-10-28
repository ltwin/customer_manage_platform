# 实时 GI 连续卡顿诊断（2026-10-27）

结论：已在当前 develop 检出复现，完整世界空间追踪 / 探针 GI 是连续停顿的主要诱因。阻塞主要发生在浏览器主线程生成 TSL/WGSL 着色器时（JS 侧 NodeBuilder.build，不是 GPU 驱动编译）；同一个反射材质在连续几帧重复生成，使一次初始化变成多次接近一秒的冻结。已复现的触发源有三个：

1. 首次进入场景（管线两次切换，各带重复生成）；
2. 编辑使探针范围扩容（如物体移出 30 m，四级变五级）；
3. Agent 的 `render_view(mode:lit)` 回看：每张图都新建 GPU 设备与 SceneRenderer，从零重走 GI 冷启动，单次回看约 0.5 s 预热 + 三次 0.75–1 s 的 render 阻塞（见文末「定向复核 B」）。这与「Agent 改场景时卡」的反馈最直接相关。

普通小范围编辑（增删普通物体、小幅移动、改灯光强度）没有复现同等级卡顿；增删灯光有 100–400 ms 级的独立灯光材质生成成本。

本次为诊断，没有修改产品源代码，没有创建 Git 提交。诊断插桩和单变量实验只在独立测试页面中使用，未接触真实账号或服务端场景。原始数据、CPU profile 与复现脚本保存在 `var/diagnostics/2026-10-27-scene-gi-stalls/`（Git 忽略，仅本机）；下文「本目录」均指该目录。

## 测量方式与边界

- 本机 Apple M4 Pro；Chrome for Testing 145.0.7632.6；Three.js 0.186.1；真实 Metal / WebGPU，非 SwiftShader。
- 当前主检出的 Vite 服务（独立端口 5186）；1280 × 820 浏览器视口；实际 SceneEditor / SceneRenderer，以及仓库自带 Cornell 和 frame-two-lights 样例，API 使用现有 e2e 的内存服务。
- PerformanceObserver 记录主线程 longtask；临时包装 SceneRenderer.render、ProbeVolume.step、TraceBase.sync、NodeBuilder.build；部分运行加 Chrome CPU Profiler；记录 GPUDevice 的着色器和管线创建调用。
- 基线分为 workbench、realtime_gi + traceOff（仍保留屏幕空间 GI）、完整 realtime_gi。`probesOff` 仅不合成探针、仍会计算，不能用来代表“关闭探针计算”，本次没有这样混用。
- 单次主线程任务 >200 ms 的诊断断言在当前代码上失败，得到可重复的红灯。这是问题复现，不是通过验收或修复完成。
- 口径警告：该断言只读 longtask observer，而 observer 在回看路径上漏报了实际的同步 render（见「定向复核 B」：实际阻塞 1006 ms，断言只报 534 ms），漏报原因未查明。因此断言数字是阻塞下限；判定「无卡顿」须同时看 SceneRenderer.render 包装计时（`worstCalls`）或 rAF 帧间隔，后续回归门禁也不能只靠 longtask。
- 这是开发环境和受控样例的测量，不是生产构建的绝对性能承诺，也没有拿到本次反馈所指的具体生产场景。帧间隔包含浏览器/GPU等待；下表只报告可直接归因的主线程 longtask。
- 代码图谱的覆盖元数据停留在 2026-10-24；相关路径被报告为 metadata_changed/not_tracked，因此关键判断均回读当前文件，不以旧图谱做排他结论。

## 对照结果

| 场景与操作 | 配置 | 主线程阻塞证据 |
|---|---|---|
| Cornell 首次进入 | 普通工作台 | 最长 106 ms |
| Cornell 首次进入 | 只保留屏幕空间 GI | 最长 183–187 ms |
| Cornell 首次进入 | 完整 GI | 一轮为 770、252、245、808、806、798 ms；独立运行最高 922 ms |
| frame-two-lights 首次进入 | 只保留屏幕空间 GI | 最长 332 ms |
| frame-two-lights 首次进入 | 完整 GI | 861、254、250、916、947、911 ms 等长任务 |
| Cornell 新增普通方块/球体、小幅移动 0.2 m | 完整 GI | 已执行的这些用例没有 >50 ms 长任务 |
| Cornell 将新物体沿 X 移动 30 m，触发探针 4 → 5 级 | 完整 GI | 593、964、993、998 ms |
| frame-two-lights 新增点光 | 屏幕空间 GI / 完整 GI | 分别 336 / 408 ms，说明灯光编译也有独立开销 |

来源：baseline-workbench.json、baseline-screen.json、baseline-full.json、lighting-*.json、models-*.json、growing-full.json。各轮缓存、JIT 和负载不同，不用不同轮的整段墙钟时间计算加速比。`wallMs` 包含 UI 点击、等待保存、等待收敛和静止确认，并不等同于连续冻结时长。

## 已定位的原因

### 1. 接入经历多次渲染管线切换，首次绘制同步生成大型着色器

`frontend/src/creative-scene/render/sceneRenderer.ts:825–835` 按 trace 就绪和 probes 预热状态选择 look；key 改变时 dispose 旧管线并 build 新管线。实测顺序为：

1. `gi-interactive`：屏幕空间管线。
2. `gi-interactive+trace-full#2+1`：世界空间反射接入，开始探针计算。
3. `gi-interactive+trace-full#2+1+probes-1`：探针热好后接入画面和反射命中点着色。

渲染入口 `sceneRenderer.ts:886` 同步 `built.pipeline.render()`。现有 `SceneRenderer.compile()` 主要处理场景 passes / 灯光材质，不等于这一整条 GI 后处理图和 compute 已异步预编译。

`probes/volume.ts:207–213` 构造内核；`:259–260` 在实际帧内同步 `renderer.compute()`。第一次探针 compute 的 NodeBuilder.build 在 profile 轮花费约 453 ms，生成 384,121 字符的 WGSL。

### 2. 反射节点 setup 在多个消费者中反复发生，重新创建同一材质图

`render/nodes/SSRTraceNode.js:1451–1453` 的 setup 每次重设 `_ssrFn` / `_sharedContext` 并调用 `_buildSSRMaterial()`；`:680–682` 重设 contextNode、fragmentNode、needsUpdate。

调用栈证实，它是在下游不同节点建立自己的 shader graph 时多次被 setup，随后在后续帧再次生成反射 shader。相同材质 UUID 的版本依次 1、4、5；含探针的反射 shader 长度一直为 603,181 字符，却生成三次，分别约 644、720、721 ms。单帧总主线程耗时约 765–843 ms。

此前无探针的世界反射 shader 也生成三次，约 217,311 字符、227–262 ms/次。因此“卡很多次”同时包含阶段切换和同一阶段内的重复生成。

诊断性单变量实验：只在测试页面替换 SSRTraceNode.setup，在 `_ssrFn` 已存在时直接返回现有输出节点。重复生成从三次变成一次；首次进入的两次后续约 800 ms 停顿消失；探针扩容后两次约 1,000 ms 停顿也消失。见 once-full.json，对照 growing-full.json。

这证明重复 setup 是可消除的放大因素。该实验不是可直接发布的补丁：正式修复还需处理共享上下文、合法失效、参数变化和导出，并验证画面一致性。实验仍有约 742/751 ms 的初次接入、547/924 ms 的扩容阻塞，单靠去重尚不能解决全部问题。

### 3. 着色器规模由展开的探针查询和命中点着色放大

`probes/lookup.ts:167` 的 probeIrradiance 用 JS 循环按分配级数展开；`levelAt` 又将每一级八个邻近探针的查询展开。`trace/hit.ts:192` 附近的 surfaceAt 还把整个预设表构成材质 Switch，并做纹理宽度所需的多次模式求值。反射命中点继续查询探针的漫反射和高光，因此图复杂度很高，即使参考场景本身很小也要生成大型 shader。

从四级到五级后，反射 shader 从 603,181 增至 699,342 字符，实测 NodeBuilder.build 为 832–895 ms/次；探针射线内核增至 432,105 字符。

CPU profile 显示主要热点在 TSL 节点 build、节点/缓存遍历和分配，伴随约 613 ms 的 GC 自耗时。所记录的 GPUDevice.createShaderModule 调用本身通常只需几毫秒。不能把全部等待直接叫作显卡驱动编译，明确可见的主因是主线程建图/生成代码；GPU 实际编译、排队与执行的完整时长没有用 GPU timestamp 单独拆分。

### 4. 编辑扩大范围会重启探针初始化

`sceneRenderer.ts:748–762` 的 probesFor 在 outgrown 时创建 growing 体，热好后替换旧体；world.serial 包含在管线 key 中（`:347–348`），替换后重建 GI 管线。

受控实验：新增方块，沿 X 移动 30 m，探针四级变五级；先有新内核的约 593 ms 长任务，切换探针后再出现 964、993、998 ms 的三个长任务。该轮 TraceBase.sync 最长仅约 0.7 ms。它解释了为何一部分编辑后会重复首次进入的卡顿。

仅移动 0.2 m 没触发扩容，也没有复现同量级长任务。因此不能宣称所有编辑都必然重建，具体生产场景里每次编辑的触发条件仍需结合该场景确认。

### 5. 预热工作量和灯光变体是另外两项成本

探针每级 16³=4096 个，每次每探针 64 条射线；四级全量预热约 1,048,576 条射线/帧，五级约 1,310,720 条射线/帧（不含反射、命中点的额外阴影光线）。视口冷启动要求 A0+14=26 次更新（截图路径另按 PICTURE 参数预热 64 帧）；预热时每级每帧更新，扩容期间旧体和 growing 还会同时 step。会加重 GPU 负载，但本次没有以 GPU计时证明其占比，不能将所有长任务归于这一点。

frame-two-lights 增加点光时，关闭世界追踪仍出现 336 ms 长任务，完整 GI 为 408 ms。这部分落在场景光照材质生成上，需独立复核现有灯光预编译覆盖，不能只修反射节点后就认为所有编辑卡顿都解决。

## 修复建议顺序

覆盖视口与 Agent 回看两条路径（回看的证据见文末「定向复核 B」）。

1. 先修复 SSRTraceNode 的多消费者重复初始化，保留必要的上下文和参数失效语义，补一个真实 GI 管线下的构建次数/阻塞回归。去重实验表明它能消掉每轮后两次约 0.8–1 s 的停顿，但首次构建仍有 0.75–1 s。
2. 缩减 TSL 图规模：将重复的探针查询形成可复用 shader 函数，减少 JS 展开和重复命中点表达式；评估预设分支，避免改变动态材质正确性。这是降低单次构建阻塞的根本手段——瓶颈在 JS 侧建图，只有图变小，每次构建才会变快。
3. Agent 回看复用：保留可复用的设备、静态管线、探针内核与 shader，同时正确清空每张图的时域历史（`host/render.ts` 中「Every picture gets a pipeline of its own」是有意为之的语义，复用资源时须保留）。否则即使 1、2 完成，每次回看仍要付一次完整冷启动。
4. 将探针初始准备、扩容、每帧更新纳入时间预算；优先复用资源和 shader，避免每次范围变化重建全部图。预算调度与降采样的画质影响需另验。
5. 异步预编译只作辅助手段：在后台为新 GI 后处理图和探针 compute 预先建图，旧管线继续绘制，就绪后再切换。注意 three r186 的限制：
   - `NodeBuilder.buildAsync()` 只在每个 shader stage 之后 `yieldToMain()`（`three/src/nodes/core/NodeBuilder.js:3282`），一个 600–700 KB 片元着色器的 setup / analyze / generate 各自仍是一整段同步执行。估计每段为数百毫秒量级（未实测），只能把一次大卡顿拆成几次小卡顿，不能保证交互不卡。
   - `compileAsync` 覆盖场景对象，`PassNode.compileAsync` 只编译它自己的场景。后处理管线没有现成的异步编译入口，SSR 材质通过 QuadMesh 渲染，需要自建预编译；仅给 SceneLight 调 compileAsync 也覆盖不到这些路径。
6. 灯光结构变化单独覆盖预编译。交互性能验收以 SceneRenderer.render 包装计时、rAF 帧间隔或 Long Animation Frames 为准，不能只看 longtask observer（会漏报，见「测量方式与边界」），也不能用普通单测通过替代。

临时绕开：选“工作台”可避开整套 GI；若只想隔离世界追踪，可在该站点控制台设置 `localStorage.setItem('scene.trace','off')` 后重新打开场景（最稳妥为刷新页面），保留屏幕空间 GI。恢复用 `localStorage.removeItem('scene.trace')` 后重新打开。它会减少屏幕外反射与间接光照能力，本次未更改真实页面的设置。

## 既有记录与文档

- `.lti/features/2026-10-23-scene-world-space-gi/features/2026-10-26-scene-probe-gi/tasks.md:135` 曾记录接入长任务 946–1134 ms（5.2a 时为 486–872 ms）。
- 同文件 `:172` 明确把性能专项延期，包含这些接入长任务。故已有功能/画面验收不代表首次进入性能已过关。
- 通过 Context7 查阅 Three.js r186 的 Renderer 与 PassNode 文档：compileAsync、compileComputeAsync 用于预编译、避免 shader compilation stutter。原始链接：<https://github.com/mrdoob/three.js/blob/r186/docs/pages/Renderer.html>。

## 复现与证据

本目录 `build.py` 基于当前仓库现有 e2e 生成临时 `frontend/scripts/.gi-stall-diag.mjs`，不改原 e2e。启动独立前端服务后，从仓库根执行：

```sh
python3 var/diagnostics/2026-10-27-scene-gi-stalls/build.py
SCENE_E2E_ONLY='DIAG GI stalls' CREATIVE_SCENE_URL=http://127.0.0.1:5186 DIAG_MODES=full DIAG_MOVE=30 DIAG_LABEL=repro node --experimental-transform-types frontend/scripts/.gi-stall-diag.mjs
```

预期得到“UI blocked >200 ms in stages: open … move …”的失败断言。`DIAG_MODES=screen,full` 做隔离对照；`DIAG_SCENE=frame-two-lights` 使用带模型的场景；`DIAG_PROFILE=1` 输出 Chrome CPU profile；`DIAG_ONCE=1` 只在诊断页面启用上述 setup 去重实验。后者仍有长任务，所以仍预期失败。

完成后删除临时生成的 `frontend/scripts/.gi-stall-diag.mjs`；测试正常结束会清理页面夹具。本次已完成清理，并停止为诊断单独启动的服务。

- `summary.json`：各轮按阶段压缩的数值。
- `lighting-full.json`：完整长任务、TSL 构建和重复 needsUpdate 调用栈。
- `growing-full.json` / `once-full.json`：探针扩容及 setup 去重的单变量对照。
- `profile-full.cpuprofile`：可在 Chrome DevTools Performance 导入的 CPU 采样。
- `models-screen.json` / `models-full.json`：真实 GLB 样例对照。


## 用户补充后的定向复核：手动移动正常，Agent 修改时卡顿

用户补充：移动物体似乎没有问题；Agent 修改场景时可能涉及增删物体和光线，不知道具体内部操作。因此追加两组验证：实际 SceneSession 的远端编辑刷新，以及页面 SceneJobLauncher → SceneExecutor 的 render_view 领取、渲染、上传、完成路径。后端接口由现有 SceneServer 模拟；渲染、GPU、前端调度使用当前真实代码。没有访问或更改用户实际场景，也没有读取那次 Agent run 的工具日志。

### A. 普通远端编辑不会无条件重建整个编辑器

链路：useCanvasSnapshot 的 canvas 事件 → notifyCanvasInvalidate → session.ts 的 openScene 订阅 → SceneSession.invalidate（最多每 500 ms 读取）→ rebase 新快照。SceneObjects 按 object.id 保留节点；Viewport.tsx 的 LookView 创建 SceneRenderer 的 effect 不以 scene 数据为依赖，所以重新取快照不等于重新创建 SceneRenderer。

通过 SceneServer.external(..., {actor: 'agent'}) 更新快照，再发实际 notifyCanvasInvalidate，使用与前次相同 Cornell 场景，测得：

| 操作 | 完整 GI 的 >50 ms 长任务 | GI 管线重新创建 |
| --- | --- | --- |
| 场景没变，仅收到一次通知并重读 | 无 | 无 |
| 范围内增加普通方块 | 无 | 无 |
| 移动此物体 0.2 m | 无 | 无 |
| 删除此物体 | 无 | 无 |
| 原有灯光强度增加 10% | 无 | 无 |
| 增加一盏点光 | 50、109 ms | 无 |
| 给新点光启用阴影 | 159、126 ms | 无 |
| 删除新点光 | 136 ms | 无 |
| 修改环境光强度 | 无 | 无 |

表中「无」同时有两份证据：同一阶段 SceneRenderer.render 包装计时（`worstCalls`）也为空，不只依赖可能漏报的 longtask observer。

这些结果排除了“Agent 每次通知都让整个编辑器从零启动”作为该受控样例的解释，不代表大型模型、超出原范围或更复杂材质的编辑不会卡顿。灯光结构变化仍存在独立的普通材质生成成本。

证据：agent-workbench.json、agent-full.json；生成器 build-agent.py。

### B. Agent 材质光照回看每次都重走 GI 冷启动

重要的新触发源是 render_view(mode:lit)。当前 Agent 工具说明明确建议打光、配材质后调用这种回看（backend/internal/creativeagent/scene_tools.go:128，:185–187）。这种调用由用户页面执行，不是后端独立渲染。

源码链路：

- frontend/src/creative-scene/host/pageExecutor.ts:31 直接在页面调用 renderView。
- host/render.ts:89–103 每次调用 openShooter，完成后 close。
- host/render.ts:121–141 创建隐藏 canvas、React root 和新的 GPU renderer。
- host/render.ts:194–211 每张图创建新的 SceneRenderer，重新 warm 探针，再渲染多帧；没有复用已打开视口的热 GI 管线。
- render.engine=realtime_gi 且 mode=lit 时进入上述 GI；省略 mode 或 mode=clay 时按工作台画，不应把所有 render_view 调用笼统说成软光追。

在页面开启真实 useSceneHost，向模拟接口连续放入两个 render_view(mode:lit, preset:overview) 任务。两次任务均读取同一 revision=1，之间没有编辑。两次领取、渲染、上传和完成均成功，无页面错误。

| 模式与任务 | 同步探针 warm | 后续三个同步 SceneRenderer.render 调用 | 浏览器帧间隔佐证 |
| --- | --- | --- | --- |
| 工作台，回看 1 / 2 | 无探针 | 最大分别 88 / 78 ms | 最大约 107 ms / 未出现 >100 ms |
| 完整 GI，回看 1 | 499 ms | 867、745、850 ms | 867、742、841 ms 等间隔 |
| 完整 GI，回看 2 | 533 ms | 1006、775、800 ms | 1008、767、792 ms 等间隔 |

每次回看都产生新的 SSR 材质 UUID，同一张图内材质版本仍为 1 → 4 → 5；每次生成的反射 shader 都为 603,181 字符。第一次三次 NodeBuilder.build 为 654、650、766 ms；第二次为 702、689、725 ms。对应之前已定位的 SSRTraceNode 重复 setup，重复启动一张图也重复启动这个问题。

预热的 499 / 533 ms 尚未拆分。从时序推断，主要是每次回看都重新构建探针 compute 内核：profile 那一轮中，同一个 384,121 字符的内核构建耗时 427–453 ms。截图路径按 PICTURE 参数（A0=32）预热 `PROBE_WARMUP=64` 帧，与第 5 节视口的 26 次不同。修复第 3 项前应先拆开这段时间，确认复用内核能省多少。

注意测量口径：此处同步调用耗时来自 performance.now 包装，并由 requestAnimationFrame 间隔交叉验证。Chrome 的 longtask observer 在这条 WebGPU 异步回调链上只记录到 warm 的约 0.5 s，漏掉了多个实际同步 render 段；不能仅靠该 observer 的条目数判定无卡顿。阶段 wallMs 含任务领取节流、上传与静止确认，不能当作连续冻结时长。

证据：agent-host-workbench.json、agent-host-full.json（实际页面领取路径）；agent-full.json（直接调用 renderView 的另一轮复现）；生成器 build-agent-host.py。

结论：当前代码至少有两个已复现的主要触发源——首次进入/探针扩容，以及 Agent 的 lit 回看冷启动。普通小范围远端编辑没有复现同等级卡顿。这与用户的观察高度一致，但尚未读取其那次 Agent run，不能断言那次每一次停顿都来自 render_view。

修复范围应同时覆盖视口与截图，已并入上文「修复建议顺序」第 1–3 项。单纯限制 Agent 改动批次、给保存接口节流或只优化拖动，不会消除已复现的截图冻结。若迁移到 Worker 隔离主线程，需要另外评估 React/R3F 场景挂载与资源生命周期，不能靠隐藏 canvas 自动获得隔离。


追加因果对照：仅在诊断页面开启先前的 SSR setup 去重替换，重复跑同一页面领取流程。两次回看的大反射 shader 均从三次生成变成一次，后续两次约 0.8 s 的 render 阻塞消失；仍分别保留 510/521 ms 的探针 warm 和 898/1120 ms 的首次 render。由此可见去重确实消除重复卡顿，但单次冷启动的大阻塞仍需独立修复，不能把实验替换当完整补丁。证据 agent-host-once-full.json。复现：先运行 build-agent-host.py，再在相同命令里加 DIAG_ONCE=1。

本轮诊断结束后已删除临时 runner，页面夹具由测试清理；停止独立 5186 开发服务。诊断脚本与证据保留在忽略目录 var/diagnostics 下，产品源码未修改、未创建提交。聚合数据在 agent-summary.json。
