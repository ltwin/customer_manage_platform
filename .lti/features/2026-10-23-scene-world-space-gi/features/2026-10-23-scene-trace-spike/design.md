# Change 详细设计

## 继承的设计

- **Epic**：`scene-world-space-gi` 批准版本 v2（2026-10-23）。本次用到的条目：
  - 「设计上下文」里的待验证假设和规模目标（去重后约 2000 万面、约 2000 个实例）；
  - 「技术选型」和「`three-mesh-bvh/webgpu` 的使用要点」：穿刺验证的对象；
  - 「关键机制」中以下各节：穿刺验证的对象。
    - 合成的接入点；
    - 屏幕外反射；
    - 命中点着色（宽度函数）；
    - 场景同步；
    - 一次性开销与缓存；
    - 精度分档；
    - 收敛、帧推进与导出；
    - 能力与容量。
  - 不变量 I2、I3、I7、I8、I9：判定标准的依据；
  - 「未决事项」U1、U2、U3、U5、U9：S0 要回答的问题；
  - roadmap 的 S0 行和 7 组穿刺清单。
- **research**：Epic 的 `discovery/research/world-space-tracing-options.md` 和 `complex-scene-scaling.md`。其中的推测数字由本次实测确认或推翻。
- **沿用 R0 的做法**（`scene-webgpu-renderer` 的 `scene-webgpu-spike`）：
  - 参考场景 JSON 和 `referenceScene`；
  - 基线用例 `SCENE_E2E_BASELINE` 和 `exportReference(gpu, name, { forceWebGL, edit })`；
  - 开发服务器专用的穿刺页面；
  - headless 下拿到 WebGPU 的启动参数 `metal`；
  - 原型结束时 `git mv` 进 discovery。
- **lesson**：
  - `webgpu-perf-warm-shader-cache`：热、冷着色器缓存分开测；
  - `epic-revise-sync-planning-root`：Epic 修订在主检出的 `develop` 上做。

本次不涉及领域模型，不改产品代码、契约、场景格式和依赖。

## 关键机制

### 新参考场景

三个场景存成完整的场景文档 JSON（`environment`、`render`、`objects`），格式与现有的 `cornell.json` 相同，只用内置基础体。它们另列在 `traceReferenceNames` 里，不加入 `referenceNames`：后者在 W1、R1 的用例里要和存档的基线图逐张比对，新场景没有存档。「每个参考场景都能打开」的检查同时遍历两份列表：

| 文件 | 内容 | 导出机位 | 服务 |
|---|---|---|---|
| `backlit-room.json` | 进深 6 m、宽 5 m、高 3 m 的房间，没有天花板。物理天空，手动太阳角度：高度 35°，方位 180°。摄像机朝向房间深处；摄像机背后约 1 m 处有一面被阳光直射的白墙，墙前有一个红色和一个蓝色的 `paint@1` 方块。画面里有白地面、灰色家具（方块和圆柱），全部在远墙的阴影里 | 一台 24mm 机位（手动曝光，EV100 约 13），背后的墙和彩色方块都不在画面里 | AC-1、AC-2 |
| `thin-wall.json` | 4 × 4 × 3 m 的封闭房间，四面墙和天花板都是 2 cm 厚的 `box@1`，左墙有一扇窗洞。窗外阳光高度 40°，室内只有窗洞透进的光 | 一台 24mm 机位（手动曝光，EV100 约 12），看窗洞所在的墙面和旁边的墙角 | AC-3 漏光 |
| `mirror-board.json` | 光滑地板（`principled@1`，糙度 0.05），镜面柜（`principled@1` 方块，金属度 1、糙度 0），金属球（`metal@1`，`rust = 0`）。摄像机背后有红、绿、黄三个方块和一根圆柱 | 一台 35mm 机位（手动曝光，EV100 约 15），正对镜面柜，背后的物件不在画面里 | AC-1、AC-12 的反射部分 |

现有的 `noon`、`room`、`materials`、`cornell` 继续作为对照场景，`cornell` 就是 Epic 里说的盒子间。

### 压力场景生成器

`frontend/scripts/fixtures/scene-stress/` 下有以下文件：

- **`spec.json`**：
  - 种子；
  - 几何列表：每项是类型、参数和目标面数；
  - 实例布局：网格摆放规则、缩放和朝向的随机范围。
- **`generate.mjs`**：纯 ES 模块，不依赖 three 和 DOM，浏览器和 Node 都能 `import`。
  - `generate(spec, { only })` 返回 `{geometries: [{id, type, positions: Float32Array, normals: Float32Array, indices: Uint32Array}], instances: [{geometry, matrix: number[16]}]}`，矩阵按列存储（同 three 的 `Matrix4.elements`）。`only` 只生成列出的几何，实例不受影响。
  - `counts(spec)` 不生成几何，直接算出面数、几何数和实例数。
  - 每个几何用独立的随机数流，所以单独生成和全量生成的结果相同。
  - 随机数用固定种子的 `mulberry32`。

**几何类型**：位移细分球、扭结环面、带凹槽的柱体和起伏地块。这几类都有平滑曲面和薄结构，用来检验代理的画质。

**规格**：

- 57 个约 30 万面的几何，另加 2 个 150 万面的，再加 1 个房间外壳用的立方体，去重后共 2011 万面；
- 实例 2000 个，摆成 30 × 20 m 的室内布局：
  - 地面和四面墙共 5 个实例，都用那个立方体；
  - 两个高面数几何放大 2.5 倍，放在摄像机前方两侧；
  - 其余 1993 个按网格排列，位置加随机偏移，缩放和朝向也随机。

**确定性**：AC-S0-1 由 `generate.test.mjs` 检查。同一个种子生成两次，几何的哈希相同；去重后的面数和实例数在规格的 ±5% 以内。

S0 的穿刺页面直接用 `generate()` 的结果建 `BufferGeometry`，不经过场景文档。GLB 写出和编辑器载入留给 S1（AC-11）。

### 实时 GI 基线用例

新增 `SCENE_E2E_GI_BASELINE=<dir>` 时运行的用例。浏览器参数用 `metal`，视口 1600 × 1000、2 倍像素比，沿用 R0 的基线用例：

1. **导出**：每个参考场景（三个新场景加 `noon`、`room`、`materials`、`cornell`）用 `exportReference(gpu, name, { edit })` 导出。`edit` 把 `render.engine` 设为 `realtime_gi`，只导出 `lit`（2K）。再用 `forceWebGL: true` 导出一套，作为 AC-6 的对照。存成 `<dir>/<场景>-<后端>-lit.png`。
2. **视口帧时间**：每个场景在最终渲染下，按现有性能用例 `orbitFrames` 和 `frameStats` 的测法，记录平均、p95 和最大值。
3. **区域亮度**：对导出图的固定区域做 `lumaStats`，区域在 JSON 里按场景定义：
   - `backlit-room`：地面、家具朝向背后墙的那一面；
   - `thin-wall`：墙角、远墙；
   - `cornell`：红墙旁的地面。
4. **固定点的转动对照**（`pointAcrossViews`）：取 `backlit-room` 左墙内侧一个没有直射光的点，把场景摄像机原地转到两个角度，用夹具页面的出图测这个点所在小区域的亮度。两个角度是 45°（亮墙完全在画面外）和 100°（亮墙进入画面），两者之比作为 AC-2 的依据。
   - 最初的方案是原地转一圈截视口、记录整图平均亮度。实测发现它主要反映「画面里有没有亮墙」，不能反映间接光，所以改成现在的固定点（实施时调整，见 research `gi-baseline.md`）。
5. **汇总**：写 `<dir>/gi-baseline.json`，内容包括设备、版本、每张图的尺寸、导出耗时、帧时间、区域亮度和固定点的转动对照。
   - 无头浏览器里按 rAF 测的视口帧时间，两次运行之间相差可达两倍，只作参考；AC-7 的性能基线由组 2 用 GPU 时间戳测量。

### 穿刺页面

- **入口**：`frontend/spike-trace.html`，源码在 `frontend/spike/trace/`。与 R0 的 `spike-webgpu.html` 和现有的 `creative-lab.html` 一样，`vite.config.ts` 没有多页面的构建入口，这个页面只在开发服务器上存在。
- **选择**：用 `?case=<组>&scene=<参考场景或 stress>` 选择穿刺项和场景。
  - `?limits=default` 时，不额外请求存储上限；缺省时，按适配器上限请求。
  - `?cold=1` 时，往一处 TSL 着色里注入一个随机常量，用来测冷缓存（lesson）。
- **渲染器**：用 three 0.186.1 的 `WebGPURenderer` 直接创建，不走 R3F。原型要直接控制 compute pass 和帧推进，场景按参考场景 JSON 用 `frontend/src` 里的几何构建和材质解析搭出来。
- **引用 `frontend/src`**：可以引用与渲染器无关的模块，也可以复制其中的节点做 fork 原型（例如 `render/nodes/SSGINode.js`、three 的 `SSRNode.js`、`materials/patterns.ts` 的两个预设），但不修改 `frontend/src`。
- **测量脚本**（Playwright）：放在 `frontend/spike/trace/measure/`，输出写到 S0 的 `discovery/research/<组>/`。
- **收尾**：`frontend/spike-trace.html` 和 `frontend/spike/` 整体 `git mv` 到 `discovery/research/prototype/`，附一份 README，写明原位置和重新运行的方法（同 R0）。

### 判定标准

每组的结果写成「可行 / 有条件可行 / 不可行」，条件写明：

| 组 | 做法 | 可行的标准 | 回答 |
|---|---|---|---|
| 1 接入 | compute pass 接进 `RenderPipeline`，与 MRT 和时域节点共用帧序号。用 `BVHComputeData` 打包参考场景：自己收集网格，按图层和辅助对象过滤。每像素发一条主光线，求交结果画成深度和法线。材质索引写进变换槽的对齐字段（覆写 `writeTransformData`） | 与光栅的深度、法线对照，除几何边缘外不一致的像素 < 0.1%；命中点读回的材质索引全部正确；两次运行逐像素相同 | 接入可行性；U9 的打包部分 |
| 2 吞吐与上限 | 参考场景和压力场景分别测三类光线：相干的主光线、不相干的半球光线、反射光线，每类在追原始几何和追代理两种情况下都测。另测以下几项：<br>- `ParallelMeshBVHWorker` 的构建耗时和 `meshoptimizer` 的简化耗时；<br>- 用原型级 IndexedDB 缓存测冷、热就绪时间；<br>- 显存占用；<br>- 添加或拖动一个物件时的上传量和最长帧；<br>- 增量追加写入能否通过扩展 `BVHComputeData` 实现；<br>- BVH 深度与栈深 60 的余量。<br>默认 limits 和最大 limits 各测一遍 | 给出每帧光线预算（每毫秒能追的光线数，按类型）、每种 limits 下的容量上限，以及压力场景在每种 limits 下落在哪一档；增量打包给出「扩展即可」或「需要 fork」及范围 | U1；U9 的打包与缓存部分；AC-7、AC-11 的阈值 |
| 3 命中点着色 | 把 `wood@1`、`marble@1` 的副本改成宽度函数输入，光栅路径传 `fwidth`。在 compute 里对命中点调用它们，比较宽度的两种取法：按光线锥估计，或取 0 靠累积抗锯齿。命中点采样太阳阴影贴图和场景环境贴图。同一表面上，光栅与命中点着色对照。另测 14 个预设全部内联后的编译耗时，冷、热分开 | 光栅路径逐像素与改动前相同（生成的 WGSL 相同，或像素差为 0）；命中点与光栅在同一表面上的平均差 ≤ 2/255；冷编译 ≤ `lookBudgetMs` 的一半 | I2、I7 的可行性；宽度函数的取法；编译预算 |
| 4 反射补追 | fork `SSRNode`，比较两种做法：<br>- A：在片元着色里直接求交；<br>- B：输出没命中采样的方向和权重，由 compute 追完后在重投影前合并。<br>两种都覆盖屏幕边缘淡出带。在 `mirror-board` 上看摄像机背后物件的反射，转动视角检查接缝。同时确认片元阶段在 `core` 和 `compatibility` 两种功能级别下能否读存储缓冲 | `mirror-board` 的镜面柜、金属球和地板映出背后的物件；物件离开画面时没有接缝和边缘变暗；记录两种做法的帧时间和限制 | U3（含 `r_max` 的建议值） |
| 6 探针原型 | 在 `cornell` 和 `thin-wall` 上跑两级级联的 DDGI：八面体辐照度加深度矩，探针停用，读上一帧探针实现多次反弹。按 Epic「合成的接入点」替换 `A_env`，粗调 `GI_INTENSITY` | `thin-wall` 的墙角和墙外没有明显漏光；记录收敛帧数、两级之间的过渡、每帧耗时，以及红墙旁地面的亮度与基线之差 | U2 的方向；U5 的 `M`；AC-3、AC-4 的阈值 |
| 5 代理画质 | 对 ε ∈ {1 mm, 3 mm, 1 cm, 3 cm} 生成代理（`ErrorAbsolute`、`LockBorder`，按槽位分别简化）。用组 6 的探针，分别追代理和原始几何，在 `backlit-room`、`thin-wall` 和压力场景上出并排图和差异图；在 `mirror-board` 上出反射追代理的并排图 | 每个 ε 都有：面数压缩比、实际误差分布、整图与区域的平均差、超过 2/255 的像素比例；owner 看图选定 ε_probe 和分档门槛 | U9 的 ε；AC-12 的阈值 |
| 7 阈值 | 汇总基线和各组数据，提出 AC-2、AC-3、AC-4、AC-7、AC-11、AC-12 的阈值 | 每个阈值都写明依据（基线波动、实测余量） | U5 |

组 5 用的是组 6 的探针，所以任务按 6 → 5 的顺序执行，组号保持与 roadmap 一致。

### 结论与 Epic 修订

- **research 文件**：`discovery/research/gi-baseline.md`，以及每组一篇 `discovery/research/<组>.md`；图、数据和脚本放在同名目录。`summary.md` 汇总七组结论、U1、U2、U3、U5、U9 的建议和阈值表。
- **Epic 修订草稿**：在主检出的 `develop` 上修改 Epic 文档（lesson `epic-revise-sync-planning-root`）：
  - design 的未决事项逐项改成采用的方案，相应关键机制同步修改；
  - proposal 的「S0 后定」改为具体阈值；
  - 结论如果改变了 S1 或 S2 的范围，同时改 roadmap。

  修订呈给 owner，批准后 `lti epic revise`，再提交到 `develop`。

## 局部设计与偏差

- **压力场景提交生成器**：去重后约 2000 万面的几何有数百 MB，不能进 git。生成器是确定性的，S1 的 AC-11 用例可以在 Node 里用同一个生成器写出 GLB。代价是 S1 还要补一个 GLB 写出函数。
- **穿刺页面直接用 `WebGPURenderer`，不用 R3F**：原型的重点是 compute pass、帧推进和缓冲管理，不需要编辑器外壳。R0 已经证明 R3F 能接住 `WebGPURenderer`，S1 产品化时再接回 `SceneRenderer`。
- **`meshoptimizer` 用现有的间接依赖**：原型只在 S0 分支上存在，不改 `package.json`。改成直接依赖并锁定版本由 S1 交付（Epic 技术选型）。
- **基线用例按需运行**：依赖本机 GPU，不能进日常 e2e，与现有的性能和基线用例一致。
- **实施时的调整**（2026-10-23）：
  - 新场景另列 `traceReferenceNames`，不加入 `referenceNames`；
  - 转动序列改为固定点在两个视角下的对照；
  - 基线用例写在页面夹具 `.scene-gi-qa.html` 之后，因为固定点要用夹具的出图。
  - 发现并修复了实时 GI 导出天空发黑的回归，修复在 develop 上（`73067304`），见 research `gi-baseline.md`。
  - 压力场景生成器：扭结环的 p、q 改为互质（组 2 发现）。原来约三分之一的扭结环 p、q 不互质，曲线把同一条路走好几遍，管面重叠，meshoptimizer 因重合顶点一个面也简化不掉。随机数的消耗次数不变，其他几何不受影响，面数和实例数不变。
  - 组 3：宽度函数副本做了全部 14 个预设，而不只 `wood@1` 和 `marble@1`，因为测内联编译耗时需要全部预设都能在 compute 里调用。光栅与命中点的对照放在 `materials` 场景（14 个预设齐全），受光颜色按同一曝光系数换算后比较，否则过曝的像素会掩盖差异。另补测了朝太阳的阴影光线，因为产品的太阳阴影已改为按视锥拟合的级联阴影（见 research `hit-shading.md`）。
  - 组 4：管线只取实时 GI 的反射项（产品的预通道、拆分光照 pass 和合成公式），去掉时域重投影和降噪，每张图取 SSR 64 帧的平均，这样比较的只是补追本身；打包用组 2 的 `TraceLayer`，参考场景的 BVH 在主线程上建；压力场景没有测帧时间，由组 2 的吞吐估算。
  - 组 2 的 Worker 构建：用多个普通 Worker 并行处理不同几何，不用 `ParallelMeshBVHWorker`。后者依赖 `SharedArrayBuffer`，页面需要跨源隔离，产品页面没有这个设置。两者的对比见 research `throughput.md`。
  - 组 6：
    - 加测了 `backlit-room`（亮墙在摄像机背后），用来看大房间和第 0 级覆盖全画面的情况。
    - 光照去掉了半球光，所以数字是同一套光照下的辐照度，不与 `gi-baseline` 的产品亮度直接对照。
    - 参考改为每像素的多次反弹路径（3 次命中，只有最后一次读探针）。第一版在第一次命中就读探针，继承了探针的漏光。
    - 除任务写的合成外，另比较了不乘 AO 和乘一半 AO 两种合成、八种更新方式和六组配置，因为 Epic 的合成与预热在实测下不成立，需要给出替代方案的依据（见 research `probes.md`）。
  - 组 5：
    - 两个参考场景全由盒子组成，`LockBorder` 下简化不掉任何面，代理与原始相同，所以画质结论主要来自压力场景。
    - 压力场景没有测量区域，只给整图指标。
    - 实测误差另按表面到表面的最近距离双向取点测量，因为 meshoptimizer 报告的误差只是估计值。
    - 出图在页面里逐像素比较未压缩的图；证据只保留半尺寸的并排图和差异图（30 MB 的规则）。

## 接口与数据契约

不变。本次不交付 Epic 契约，不新增局部契约。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/scripts/creative-scene.e2e.mjs:traceReferenceNames` | 新增 | `backlit-room`、`thin-wall`、`mirror-board`；「每个参考场景都能打开」的检查同时遍历它 |
| `frontend/scripts/creative-scene.e2e.mjs:exportReference` | 修改 | 新增可选的 `litOnly`，缺省行为不变 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | 新增 `SCENE_E2E_GI_BASELINE` 用例、区域定义的读取、`shooterScene`、`pointAcrossViews`；`three` 的导入加上 `Vector3` |
| `frontend/scripts/fixtures/scene-reference/` | 新增 | `backlit-room.json`、`thin-wall.json`、`mirror-board.json`，以及区域定义 `gi-regions.json` |
| `frontend/scripts/fixtures/scene-stress/` | 新增 | `spec.json`、`generate.mjs`、`generate.test.mjs` |
| `frontend/spike-trace.html` | 临时新增 | 穿刺页面入口，S0 结束时移进 discovery |
| `frontend/spike/trace/` | 临时新增 | 原型和测量脚本，S0 结束时移进 discovery |

## 验证与交付

- **AC-S0-1**：
  - 新参考场景：定向运行 `SCENE_E2E_ONLY=baseline: SCENE_E2E_BASELINE=<临时目录>`，九个参考场景都能导出且没有报错。
  - 压力场景：运行 `node --test frontend/scripts/fixtures/scene-stress/generate.test.mjs`。
- **AC-S0-2**：在参考设备上运行 `SCENE_E2E_ONLY=gi-baseline SCENE_E2E_GI_BASELINE=<S0>/discovery/research/gi-baseline/`，检查两个后端的导出图、`gi-baseline.json` 和转动序列是否齐全。
- **现有用例不回归**：参考场景列表变长会影响引用 `referenceNames` 的用例。定向运行 R0 基线和 W1 的 `SCENE_E2E_LOOKS` 两个用例，按项目约定换端口，不跑全量。
- **AC-S0-3、AC-S0-4**：逐组核对 research 是否有结论、数据和截图；U 项是否逐项回答；组 5 的对照图是否齐全。
- **AC-S0-5**：最后一个实现提交后满足以下各项：
  - `git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空；
  - `frontend/spike*` 不存在；
  - 前端静态检查通过。
- **合回 `develop`**：只涉及 e2e 脚本、固定数据和文档，按 `check-merge` 的判定表只需前端静态检查。

## 未决事项

无。参考设备沿用 R0 的本机 Mac16,8（Apple M4 Pro）。
