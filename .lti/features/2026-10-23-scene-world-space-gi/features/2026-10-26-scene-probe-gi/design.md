# Change 详细设计

## 继承的设计

- **Epic**：`scene-world-space-gi`，基线 v4 加修订 v5 草稿（与本方案一起呈给 owner，见 proposal「Risk Notes」），绑定全部设计条目。S2 落实其中与探针、合成和收口有关的部分：
  - 「领域模型与业务规则」：I1–I9，重点是：
    - I1：探针只替换环境贴图的那一份；
    - I3：每张导出图清空探针；
    - I6：标定只随这一层；
    - I8：探针追代理。
  - 「关键机制」：
    - 「合成的接入点」：全部；
    - 「命中点着色」：间接光读探针；
    - 「探针体」：全部，S2 定下细节（U2）；
    - 「收敛、帧推进与导出」：全部；
    - 「能力与容量」：两份精度同时服务；
    - 「场景与导出」：指纹。
  - 「验证与交付」：参考场景、画质对照、性能用例。
- **契约**：交付 `architecture: 场景模块`、`patterns: 场景着色写法`、`data: 场景文档`（Epic 交付计划，修订 v4）。正文以修订 v5 的批准版本为准，本 Change 的 `contracts.md` 不写局部增量。
- **依据**：
  - S0（`scene-trace-spike`）的 research：
    - `probes.md`：探针原型、收敛、漏光、耗时；
    - `proxy-quality.md`：代理画质；
    - `hit-shading.md`：compute 里的四处限制。
  - S1（`scene-offscreen-reflections`）的 design：底座、命中点着色、反射补追的现状，以及 A1–A13。

  原型的代码在 `features/2026-10-23-scene-trace-spike/discovery/research/prototype/spike/trace/probes.ts`。S2 以它为起点改写，不直接搬运：原型里有测量代码，探针数据放在存储缓冲里手工做双线性。
- **S2 承担**：
  - Epic 的 AC-2、AC-3、AC-4、AC-8（延期）、AC-10；
  - AC-5、AC-6、AC-7、AC-11 的复测；
  - AC-12 的探针部分。
- **方案评审**（owner 2026-10-26 转来，一轮）：结论逐条并入本文，处理结果见「方案评审的处理」。

## 领域模型与业务规则

S2 不触及业务领域。下面是渲染端新增的派生状态和规则。

### 探针体

| 概念 | 内容 | 规则 |
|---|---|---|
| 级 | 第 k 级（k = 0 … 4）：16 × 16 × 16 个探针，间距 `s_k = 0.25 × 2^k` m | 级的盒子对齐到自己的网格，原点是 `s_k` 的整数倍。按场景大小预留前 `n_alloc` 级，每帧启用前 n 级，3 ≤ n ≤ `n_alloc` ≤ 5（见「级联的放置与轮转」） |
| 级的原点 | 这一级的盒子在世界里的位置 | **只在这一级被更新的那一帧移动并锁定**。没轮到的帧，采样、光线内核和命中点都用上次锁定的原点，读到的数据与它代表的位置一致（评审 B3） |
| 探针槽 | 一级里的 4096 个位置之一，按「世界格坐标欧氏取模 16」寻址 | 盒子平移时，槽不动，换的是它代表的世界格（环形寻址）。负坐标用欧氏取模，不用 WGSL 的 `%`（截断取余） |
| 探针状态 | 代表的世界格、重定位偏移（每轴不超过 0.45 `s_k`）、三个年龄、是否停用 | 同时写一份到状态纹理，采样和光线内核都会核对世界格 |
| 数据年龄 | 自数据清空以来被更新的次数 | 只在冷启动或环形重置时归零，决定采样时的成熟度（评审 B2） |
| 编辑年龄 | 自上次编辑以来被更新的次数 | 编辑时归零，决定滞后 α，不影响成熟度 |
| 重定位年龄 | 自重定位窗口打开以来被更新的次数 | 冷启动、环形重置时归零；几何编辑结束后，只对受影响的探针归零（评审 I7） |
| 辐照度 | 8 × 8 的八面体图，存 E × s_env / k（见 P4） | 余弦加权平均辐射度 × π（S0「辐照度的归一化」） |
| 深度矩 | 16 × 16 的八面体图，存平均距离和平均距离² | 按 cos⁵⁰ 加权；距离截到 1.5 倍格对角线（1.5 √3 `s_k`，DDGI 的做法）；命中背面的距离缩短为 0.2 倍 |

**持有者**：
- **视口**：探针体由视口的 `SceneRenderer` 持有，按代理层的身份做键，管线重建时保留（评审 I5）：
  - 景深开关、显示方式、`accumulate` 都会重建管线，底座换档也会；但只要代理层不变，探针就不必冷启动；
  - 底座在两档里都用同一个代理层，所以换档不冷启动；代理层整份重打包、设备丢失时才新建。
- **导出和 `render_view`**：每张图新建，用完释放（Epic I3）。
- 这一处修订了 Epic「数据所有权」里「探针属于每条管线」的写法，只对视口放开，见修订 v5。

### 本次的不变量

- **P1 不构建时只差量化**：底座不是 `ready` 时，管线与 S2 之前的节点相同，只有两处不同：
  - giSplit 在 `ambient` 的 alpha 里写系数 c（见「合成」），不构建时合成只用它判断「是否拆开」，这一项不改变画面；
  - `base` 改为 sRGB 编码的 8 位格式（`rgba8unorm-srgb`），暗色的反照率精度更高。这一项会改变不构建时的画面，但只是量化上的差别，按 R2 的「一级以内」判定（AC-S2-5）。
- **P2 固定的读写顺序**：
  - 每帧先派发所有到期级的光线内核，再派发所有更新内核。光线内核读更新前的探针，屏幕采样和反射命中点读更新后的探针；
  - 一个级的光线不会读到另一个级在本帧刚写的图集；
  - three 的 `updateBefore` 按节点图的固定顺序执行，所以这个顺序每帧、每次导出都一样。
- **P3 结果确定**（Epic I3）：
  - 光线方向的旋转、阴影光线抽哪盏灯、轮转到哪一级，都只取帧序号、探针编号和光线编号。抽灯的随机数带上光线编号，否则一个探针的 64 条光线会抽中同一盏灯；
  - 每个纹素由一个线程独占写入，不用原子操作，求和的顺序固定。
- **P4 存储单位**（评审 B1）：探针存「未曝光值 × s_env」，即写入时乘 s_env / k，读出时乘 k / s_env。
  - k 是本帧的预曝光；s_env 是 `PhysicalLighting` 留在场景上的 `skyLightScale`，其他天空为 1。
  - 不存未曝光值：物理天空下灯光按 lux 计，正午被晒地面上方探针的 E 约 8 × 10⁴，超过 fp16 的 65504；夜里月光的反弹约 10⁻⁴，落进次正规数区间。
  - 乘上 s_env 后，正午和夜晚都落在 fp16 的 0.1–1 量级（`world/exposure.ts` 的 s_env 本来就是为此设计的），探针和天空 LUT 用同一个尺度。
  - 调曝光只改 k，不重置探针。s_env 只在天空变化时改变，这时用一次 compute 把历史和图集乘以 s_new / s_old，再按 `lightingVersion` 重新跟上，成熟度不变。
- **P5 逐级回退**（评审 I2）：
  - 一点上的辐照度，先取覆盖它、且成熟的最细一级；细级不成熟或覆盖不到时，混入更粗一级；
  - 所有启用的级都覆盖不到时，才按环境贴图的辐照度画（`A_env · ao`）；
  - 光线内核在命中点读探针时同理。

### 底座的两份精度（S1 状态机的修改）

S1 的状态机不变（S1 design「底座的状态」），改的是 `ready` 的含义和判档：

| 档 | 反射追 | 探针追 | 就绪条件 |
|---|---|---|---|
| `ready/full`（第 1 档） | 原始层 | 代理层 | 两份都全部就绪，(原始 + 代理) × 1.25 ≤ 预算 |
| `ready/proxy`（第 2 档） | 代理层 | 代理层 | 代理层全部就绪，且装得下 |
| `tooLarge` | — | — | 代理层也装不下 |

- **代理始终构建**：修订 S1 的 D3。代理只有原始几何的 0.4%–2%，第 1 档的显存几乎不变。冷启动多出代理简化的 Worker 耗时，S1 估算为累计 5.8–7.0 s，11 个 Worker 并行约 0.6 s。
- **就绪只有一次**：两份都到齐才算就绪，只到齐一份时不先报 `ready/proxy`，管线只切换一次。
- **换档**：沿用 S1 A4，旧档一直服务到新档全部就绪。
- **探针改追原始几何**：只有 e2e 的 `gpuTestHooks.probeLayer = 'original'` 会这样做，用于 AC-S2-8。

### 编辑的分类

底座在 `sync` 里已经比对场景变化。S2 把会改变光照的变化分成两类，各记一个版本号，交给探针体：

| 版本号 | 何时加一 | 探针的动作 |
|---|---|---|
| `geometryVersion` | 实例增删、可见性变化、变换变化、新几何提交 | 全部探针的编辑年龄归零。同时记下这次改动涉及的实例包围盒（并集）。版本号连续 4 帧不变（编辑结束）之后，包围盒外扩 `s_k` 以内的探针，重定位年龄归零 |
| `lightingVersion` | 材质行变化；灯光行变化（按未曝光的值比较）；`AmbientLight` 之和变化；环境贴图换了纹理，或强度、旋转、s_env 变化 | 全部探针的编辑年龄归零 |

- 编辑只把编辑年龄归零，数据和成熟度都保留：拖动期间画面照常有探针的反弹光，新一轮更新以当前探针为起点（评审 B2）。
- 拖动期间不重开重定位窗口，探针不会随拖动来回挪（评审 I7）。
- 摄像机移动不进版本号，只有环形重置的那部分探针数据年龄归零。

## 模块职责与协作

| 模块 | 职责 | 不负责 |
|---|---|---|
| `render/probes/cascade.ts` | 纯函数：级数、原点、欧氏取模的环形寻址、轮转表、α(编辑年龄)、成熟度、视口收敛帧数（单测） | GPU |
| `render/probes/volume.ts`：`ProbeVolume` | 缓冲与图集（按 `n_alloc` 分配）；`step(frame)`：放置并锁定到期的级、比对版本号、派发内核；s_env 变化时的换算 | 着色、合成 |
| `render/probes/kernels.ts` | TSL compute：光线内核、更新内核（辐照度、深度矩、状态、重定位、图集边框）、换算内核 | 选层：由调用方传入代理层 |
| `render/probes/lookup.ts` | TSL：`probeIrradiance(P, n, V)` 返回 `{E, coverage}`，逐级回退，LOD 一律显式指定 | 写探针 |
| `render/probes/octahedral.ts` | 八面体编码和解码，TSL 和 JS 两份 | — |
| `render/trace/hit.ts` | 加 `radiance(origin, dir)`：探针光线的漫反射着色，投影的灯只抽一盏发阴影光线。`shade()` 的间接光改读探针（有探针时） | 选探针 |
| `render/trace/base.ts` | 两份精度同时服务；`TraceHandle` 改为 `{reflect, probes, tables, tier, key}`；两个版本号；场景包围盒（不含内置地面）；改动的实例包围盒 | — |
| `render/trace/computePatches.ts` | three r186 在 compute 里的限定范围补丁（Epic「技术选型」） | 光栅着色器 |
| `render/sceneRenderer.ts` | 视口持有探针体，跨管线重建保留；就绪后的后台预热；导出的 `warm` | — |
| `render/realtimeGI.ts` | 构建这一层时：屏幕采样 pass、合成；SSGI 的漫反射降噪链不建 | 探针体的生命周期 |

**依赖方向**：
- `sceneRenderer` 和 `realtimeGI` 依赖 `probes/` 和 `trace/`；
- `probes/` 依赖 `trace/` 的层、表和命中点着色；
- `trace/` 只经 `hit.ts` 的参数接收探针的查找函数，不 import `probes/`。将来的路径追踪不用探针，底座不依赖它。

**`ProbeVolume.step(frame)`**：视口每帧和导出预热都只走这一个入口（评审「代码设计」）。调用前，调用方必须已经做完本帧的前置步骤：

```text
前置（SceneRenderer.render 里本来就按这个顺序做，warm 照做）：
  advanceFrame()
  applyExposure(scene, k)                  // 灯光行的预曝光
  base.sync()                              // 两份精度、表（tables.setLights）、版本号、包围盒
  hit.update()                             // 环境贴图换了时改纹理
step(frame)：
  算 n；到期的级（轮转表）移动并锁定原点
  版本号变化 → 编辑年龄归零；几何编辑结束 → 包围盒附近的重定位年龄归零
  s_env 变化 → 换算内核
  派发：所有到期级的光线内核 → 所有到期级的更新内核        // P2
```

**主流程（视口的一帧，构建这一层时）**：

```text
SceneRenderer.render(frame)
  前置（同上）
  handle = base.handle()                         // {reflect, probes, tier}
  probes = 按 handle.probes 的身份取或新建 ProbeVolume
  key 变化 → realtimeGI(..., { trace: handle, probes })   // 管线重建，探针保留
  probes.step(frame)
  pipeline.render():
    光照 pass、预通道、SSGI（只用 AO）、SSR + 补追（命中点的间接光读探针）
    探针采样 pass：每像素 probeIrradiance → vec4(E · k / s_env, coverage)
    合成（world）→ 雾、空气透视、体积 → TRAA
```

**就绪的那一刻**（评审 I5）：
- 底座刚就绪时探针是空的。如果立刻切到 world 管线，画面会先退回环境光、变暗，再慢慢回来。
- 所以视口先在后台预热：底座就绪后，照旧画屏幕空间的管线，同时每帧 `step`，全部启用的级都更新；
- 等所有级的数据年龄都达到 A0 + 14，再切到 world 管线，参考场景约 26 帧；
- 这段时间里视口持续请求帧。每帧多出探针的耗时，只在就绪时发生一次。

**导出与 `render_view`**：

```text
openShooter：挂载 lit → compile → base.ready(15 s)        // 沿用 S1
shoot(framing)：
  g.resetFrames(); picture = new SceneRenderer(...)       // 探针体随这张图新建，全空
  picture.warm(camera, frame, M = 64)                     // 每帧照做前置步骤，再 step，不画光栅
  照常 convergeFrames（12）+ K 帧
  最后一帧 capture
```

预热之后的 12 + K 帧里，探针是继续更新还是冻结，见「导出预热与时限」。

## 关键机制

### 级联的放置与轮转

```text
对第 k 级（W_k = 16 · s_k）：
  前方 f = 摄像机朝向的水平分量（不归一化：俯视时趋近 0，盒子退回以摄像机为中心）
  中心 = 摄像机位置 + f · W_k / 4                 // 前方 3/4，后方 1/4
  原点 = floor((中心 − W_k / 2) / s_k) · s_k       // 竖直方向同样以摄像机为中心
```

- **「摄像机」**：当前在画的视角，包括视口的自由视角、视口里的场景摄像机、导出和 `render_view` 的视点。探针每帧跟着它放置，不需要切到摄像机视角（owner 2026-10-26 确认）。
- **覆盖**：

  | 级 | 间距 | 前方 | 后方 | 上下各 |
  |---|---|---|---|---|
  | 0 | 0.25 m | 3 m | 1 m | 2 m |
  | 1 | 0.5 m | 6 m | 2 m | 4 m |
  | 2 | 1 m | 12 m | 4 m | 8 m |
  | 3 | 2 m | 24 m | 8 m | 16 m |
  | 4 | 4 m | 48 m | 16 m | 32 m |

- **级数**（owner 2026-10-26：按场景大小自动加级，最多五级）：

  ```text
  levels(d) = clamp(3 + ceil(log2(d / 12 m)), 3, 5)  // 第 n−1 级的前方覆盖 ≥ d
  n_alloc   = levels(场景包围盒的对角线长)           // 摄像机在场景里时最远也就这么远
  n         = min(n_alloc, levels(摄像机到包围盒最远角的距离))
  ```

  - 包围盒取参与追踪的网格，不含内置地面 `ground@*`。地面常常远大于场景本身，远处的地面在室外，按天空照亮本来就对；室内的地板、墙和天花板是模型或基础体，照常计入。
  - 参考场景的范围在 10 m 以内，多数用三级，个别摄像机离得远的用四级；压力场景（30 × 20 m 的大厅）用五级。
  - **分配**：缓冲按 `n_alloc` 级分配，三级的场景只占三级的显存（评审「显存」）。场景变大使 `n_alloc` 增加时，扩容一次：已有级的数据拷进新缓冲，新级从冷启动开始。
  - **启用**：n 变大时立即启用，新启用的级从冷启动渐入（P5）；距离降到门槛的 3/4 以下才停用，在边界上不来回切换。导出时 n 只取决于场景和机位，结果仍然确定。
- **后方留四分之一**：反射的命中点常在摄像机背后，例如 `mirror-board` 背后的方块。S0 对照过「居中」和「近面贴摄像机」，`thin-wall` 的区域差在 1–3 个百分点以内（`probes.md`「漏光」），四分之一介于两者之间，在 AC-S2-2 里复核。
- **原点锁定**（评审 B3）：
  - 每一级的原点只在它被更新的那一帧移动；
  - 采样、光线内核和命中点都按锁定的原点寻址；
  - 状态纹理里记着每个槽的世界格。读的时候发现与按锁定原点算出的不一致，就按权重 0、偏移 0 处理。例如本帧刚移动了原点、更新内核还没写到这个槽时，就会出现这种情况。
- **环形重置**：更新内核发现一个槽的世界格变了，就清空它的数据，三个年龄都归零，偏移归零。不需要 CPU 列出哪些探针要重置，也不搬移数据。
- **转动视角**：中心随朝向移动 W_k / 4 的弧。原地转半圈时，第 0 级整体重置，第 2 级移动 6 m，约三分之一重置。重置的地方先用更粗一级的探针（P5），不会退回环境光。
- **轮转**（只在视口）：
  - 第 k 级每 `P_k` 帧更新一次，各级错开帧号，让每帧的负担大致相同；
  - `P` 按顶层的根数分档：

    | 顶层的根数 | P（第 0 … 4 级） |
    |---|---|
    | ≤ 256 | 1, 1, 2, 2, 4 |
    | ≤ 1024 | 2, 2, 4, 4, 8 |
    | 更多 | 4, 4, 8, 8, 16 |

    参考场景都在第一档，压力场景约 2000 个实例，在第三档。
  - 阈值是性能任务的起点，按 AC-S2-6 实测调整，单测守住分档表；
  - 后台预热、冷启动时不轮转，全部启用的级每帧更新；导出也每帧更新全部启用的级。

### 探针光线（`kernels.ts` 光线内核，`hit.ts` `radiance`）

- **分工**：每条光线一个线程，每级 4096 × 64 条；只派发本帧到期的级。
- **方向**：球面 Fibonacci 点集，每帧按 `hash(帧序号)` 生成的旋转整体转动，与 S0 相同。
- **起点**：探针位置加重定位偏移。槽的世界格与锁定原点不一致时，偏移按 0 计。
- **命中**：
  - 经 `wgsl.ts` 的 `traceFirst` 追代理层；
  - 命中背面时，辐射度记 0，距离记负值，供停用和重定位使用。
  - **单面材质的背面**也按背面记，与 S1 的 `shade` 不同：`shade` 遇到单面材质的背面会穿过去（`hit.ts:246`），因为光栅剔除了它。探针按 DDGI 的做法，把一切背面都当作「在物体里面」的证据，这样单面地面下方的探针会停用；代价是单面的卡片背后的探针也可能被挪开或停用。
- **着色**（`radiance`，漫反射，DDGI 的做法）：

  ```text
  L = kd · c / π · (E_direct + E_probe(命中点) · k / s_env + L_amb · r_fill) + emission
  ```

  - `kd`、`c`、`emission` 取命中点的材质，与 S1 的 `surfaceAt` 相同；c 的算法与合成相同（见「合成」）。
  - 宽度由光线锥给出：起点宽度 0，扩散角 √(4π / 64)。宽度不能取 0：WGSL 的 `smoothstep` 在两个边界相等时没有定义（S0 `hit-shading.md` 的 `aaStep`）。
  - `E_direct`：
    - 不投影的灯直接相加，与光栅相同；
    - 投影的灯按各自未遮挡贡献的亮度抽一盏，只对它发阴影光线，结果除以它被抽中的概率；
    - 这是无偏估计，一个命中点只发一条阴影光线，耗时不随灯数增长；噪声由 64 条光线和滞后平均。
  - `E_probe(命中点)`：读更新前的探针（P2），这是多次反弹的来源，逐级回退（P5）。
  - 不计镜面反射：DDGI 的探针只存漫反射，金属在反弹光里没有贡献。这是已知的近似，与 Epic「后续增强」里的「粗糙反射复用探针」是两回事。
- **没命中**：
  - 按光线方向取环境贴图里糙度 0.5 的一级（修订 v5 改写 Epic「命中点着色」的「没命中」一句）；
  - 不取漫反射卷积的那一级：探针本身就在做余弦积分，再取卷积过的值等于卷积两次，天空的明暗分布会被抹平；
  - 也不取最清晰的一级：64 条光线在高频的天空上噪声大。0.5 的波瓣与光线的间距相当。
  - 在 AC-S2-2 的 `physical-noon` 里与逐像素参考对照，参考没命中时取最清晰的一级。
- **写出**：每级一个光线缓冲，不跨级共用。共用一份可以省 17 MB（五级时），但那样就只能一级接一级地「追、更新」，后面的级会读到前面的级刚更新的图集，与 P2 冲突。评审「显存」一条的这一项不采纳。
- **compute 的限制**：
  - PMREM 采样用的 `TextureNode.grad()` 和环境旋转，由 `computePatches.ts` 处理；
  - `Switch` 紧跟 `If` 的问题和库的 `mat4x4f` 类型名，沿用 S1 的写法绕开。

### 更新（`kernels.ts` 更新内核）

- **分工**：每个探针一个工作组，64 个线程。
  1. 先把这个探针的 64 条光线（辐射度与距离，`vec4`）读进工作组共享内存，共 1 KiB，然后 barrier；
  2. 每个线程算 1 个辐照度纹素和 4 个深度矩纹素，各自遍历共享内存里的 64 条光线；
  3. 0 号线程算状态：世界格、背面比例、最近的正面与背面距离及方向、重定位、三个年龄。

  S0 原型每个纹素都从全局缓冲读 64 条光线，深度矩 16 × 16 一项就占了 3 ms。
- **滞后**：

  ```text
  α(编辑年龄) = 编辑年龄 < A0 ? 0 : min(α_max, 1 − 1 / (编辑年龄 − A0 + 2))
  新值 = mix(本帧, 历史, α)
  视口：A0 = 12，α_max = 0.93          // S0「edit」与稳态；owner 2026-10-26 定的缺省
  导出：A0 = 32，α_max = 1（即均值）     // S0「restart32」，M = 64
  ```

  - 导出的两段预热和视口的编辑跟随是同一个公式，参数不同。A0 过后 α 从 0.5 逐帧升到 α_max，跟随期结束时不会突然变稳。
  - 数据年龄为 0 的槽（刚清空）没有历史，α 按 0 计。
- **重定位**：按 RTXGI 的规则，只在重定位年龄小于 8 的更新里进行，之后冻结，探针不随光线旋转来回抖动。
  - 背面比例超过 1/4（探针在物体里）：朝最近的背面方向移过去；
  - 否则，最近的正面近于 0.2 `s_k`：背离它移开；
  - 偏移每轴限制在 0.45 `s_k` 以内。
  - 重定位窗口结束时，背面比例仍超过 1/4 的探针停用（S0 的规则）。
- **写出**：
  - 辐照度和深度矩的历史放在 fp32 的存储缓冲里，每个纹素由一个线程读改写；
  - 同时写进 RGBA16F 的图集，每个探针的图块带 1 个纹素的边框。边框按八面体的折叠复制对应的内部纹素，由算这个内部纹素的线程顺带写出；
  - 状态缓冲同样写一份到状态纹理，内容是世界格、偏移、数据年龄和停用标记；
  - 不轮到的级不派发，它们的图集保持上次的内容。
- **为什么历史放缓冲、采样用纹理**：
  - 采样每像素要取约 16 个探针，硬件双线性比 S0 原型在存储缓冲上手工插值便宜得多；
  - 但 WebGPU 的读写存储纹理只支持 r32 类格式，所以历史放在缓冲里，纹理只写不读；
  - 这样也省掉了 ping-pong 两份图集。
- **换算内核**：s_env 变化时，把每级的历史和图集乘以 s_new / s_old（P4）。

### 采样（`lookup.ts`，屏幕采样 pass）

- **每级**：取周围 8 个探针，与 S0 相同：
  - 采样点沿 `0.2 n + 0.8 V` 偏移 0.3 `s_k`；
  - 权重是以下几项的乘积，并压低小权重：三线性、朝向项 `((n·dir + 1)/2)² + 0.2`、Chebyshev 可见性的三次方、是否启用；
  - 状态纹理里的世界格与按锁定原点算出的不一致时，权重为 0（B3）。
- **成熟度**：`m_i = min(1, 数据年龄_i / A0)`。一级的结果：

  ```text
  E_k        = Σ w·E_i / Σ w
  coverage_k = 盒内淡出 × Σ (w · m_i) / Σ w        // Σ w = 0 时为 0
  ```

- **逐级回退**（P5，评审 I2）：从最粗的启用级往细合成：

  ```text
  E = E_env，c = 0                               // E_env：环境贴图的辐照度，只作最后的退路
  对 k = n−1 … 0：E = mix(E, E_k, coverage_k)，c = max(c, coverage_k)
  ```

  - 合成按 `c` 在探针与 `A_env · ao` 之间混合；
  - 实现时不必每级都采：一点只落在前几级的盒子里，按盒子算出要采的级，再按 `coverage` 提前结束。
  - 混合前先把两边的 NaN 和无穷大换成 0、权重换成 0。S0 发现 `mix(b, a, 0)` 在 `a` 为 NaN 时仍是 NaN。
- **采样的写法**（修订 v5 的 `patterns` 新规则）：
  - 选级和提前结束是按像素分叉的；
  - 图集的采样一律用 `.level(0)` 显式指定 LOD，不依赖隐式导数，所以写在分支里也不会出 NaN。
- **屏幕采样 pass**：
  - 一个全屏的 `convertToTexture` 节点，读预通道的深度和法线，输出 `vec4(E · k / s_env, c)`，RGBA16F；
  - 合成只多读这一张纹理。合成所在的 pass 已接近每阶段 16 张纹理的上限（`realtimeGI.ts` 的注释：加上体积和辅助对象后曾到 17 张）；
  - 先按全分辨率实现，超出预算时按「性能」一节的顺序改为半分辨率。

### 合成（`realtimeGI.ts` `composite`）

**giSplit 的改动**（评审 I1）：
- `ambient.rgb` 不变，仍是 `A_env + A_light`（已乘材质 AO）；
- `ambient.a` 从 1 改为 `c = max(luminance(1 − (Fss·Ess + Fms·Ems)) × 材质 AO, 1e-3)`：
  - 单次与多次散射直接调用 three 的 `this.computeMultiscattering(...)` 求出，参数与 `indirectDiffuse` 相同（`specularColor`、`specularF90`、`this.iridescenceF0Dielectric`，`PhysicalLightingModel.js:766`），不另抄公式；
  - 材质 AO 取 `indirect()` 里 `ambientOcclusion()` 乘的同一个值（`:749`、`:894`），所以 `A_lt` 与 `ambient` 严格一致；
  - 有 sheen 时再乘它的能量补偿。
- **c 不是常数**：光滑电介质在掠射角下 c 约为 0.1，所以下限写成 1e-3，「是否拆开」按 `ambient.a > 0` 判断；
- **标量近似**：`specular_color` 染色时，三个通道的 c 不同，alpha 只存得下一个，取亮度加权值。
- **透明和透射材质**：照旧不写（alpha 0）。

**`base` 改为 sRGB 编码**（评审「次要」）：
- 现在 `D_probe` 承担了主要的间接光。8 位线性的 `kd`，暗色材质有 10–20% 的相对误差，比现状差；现状的 `ambient` 里用的是全精度的 kd。
- `rgba8unorm-srgb` 与 `rgba8unorm` 在 WebGPU 里同样按 8 字节计入每像素 32 字节，不占额外预算。alpha（金属度）在 sRGB 格式里仍是线性的。
- SSR 和合成采样时自动解码，读到的是精度更高的同一个值。

```text
kd      = base.rgb · (1 − base.a)              // base 采样时已解码为线性
c       = ambient.a
split   = (c > 0) · opaque
direct  = output − ambient.rgb − envSpec

不构建（同现在）：
  diffuse = ambient.rgb · ao + kd · gi
构建（world）：
  A_lt    = kd · c · L_amb / π                 // L_amb：本帧 AmbientLight 的颜色 × 强度之和，已乘 k
  A_env   = max(ambient.rgb − A_lt, 0)
  D_probe = kd · c · E_probe / π               // E_probe 来自屏幕采样 pass，已乘 k / s_env
  diffuse = mix(A_env · ao, D_probe, coverage) + A_lt · r_fill · ao

shaded = direct · contact' + diffuse + envSpec · ao · (1 − cover') + reflection · split
```

- **`L_amb`**：由 `tables.ts` 每帧写灯光表时一并交给合成，是一个 uniform，不含 W5 的替身灯。物理天空下没有 `AmbientLight`，它为 0。
- **`D_probe` 带上材质 AO**：c 里乘了材质 AO，`D_probe` 也就带上了烘焙的小尺度遮挡。现在的模型还没有 AO 贴图，等 C5c 的 PBR 贴图交付后才会起作用，那时正好部分填上 S2-U7「小于探针间距的接触阴影」的空缺。
- **`r_fill`**：环境光填充在这一层构建时的重新标定（Epic I6 的 `AMBIENT_FILL_GI`），写成合成里的比值，场景图里的 `AmbientLight` 强度不变：
  - 命中点和探针光线也乘同一个比值；
  - 缺省为 1，按 AC-S2-2 的对照图决定是否改，改动先交 owner 看图。
- **探针不乘 `ao`，`gi` 不叠加**：Epic 修订 v3。`world` 时不构建 SSGI 的漫反射降噪链：
  - 视口：`temporalReproject` 和 `recurrentDenoise`；
  - 导出：`filtered(gi)`。

  SSGI pass 照常运行，只为 AO。`GI_INTENSITY` 在 `world` 时取 0（I6）。fork 的 SSGI 能否在 `giIntensity` 为 0 时跳过取色，在性能任务里按实测决定。
- **启用的各级之外**：画面按 `A_env · ao` 照亮，没有 SSGI 的反弹光，比 S2 之前略暗；最外一级的最后一格内淡出，没有硬边。最多五级时前方覆盖约 48 m，参考场景都在范围内。

### 命中点的间接光（`hit.ts` `shade`，S1 的反射）

- 有探针时，`lightAt` 的环境贴图辐照度那一项换成 `kd · c · E_probe(命中点) · k / s_env / π`，逐级回退，规则同合成；
- `AmbientLight` 那一项乘 `r_fill`；
- 镜子里的墙与画面里的同一面墙因此用同一份间接光（Epic I2）。
- AC-S1-2 的对照（`checks.ts` 的 `hitCheck`）比较的是命中点着色与材质本身，仍按环境贴图算，用一个参数关掉读探针。
- 反射的片元读探针图集要多绑 3 张纹理（辐照度、深度矩、状态），能否放下见未决 S2-U2。

### 收敛与重绘（视口）

- **两个函数分开**（评审 I4）：
  - `convergeFrames(engine)` 不变：12 帧，仍供导出的 `drawnFrames` 和 `AccumulateNode` 使用；探针的收敛由导出预热负责；
  - 视口另用 `viewportSettleFrames(engine, world, n, periods)`：

    ```text
    world：max_{k < n}(P_k) · (A0 + 14)
    其余：convergeFrames(engine)
    ```

  - A0 之后再过约 14 次，α 升到 0.93，稳态闪烁在 0.2%–0.8%（S0），满足 AC-S2-3 的「逐帧变化小于 1%」。
- **帧数**：
  - 参考场景用三到四级，P 最大为 2，`viewportSettleFrames` = 52 帧，在 AC-S2-3 的 64 帧以内；
  - 压力场景用五级，P 最大为 16，要 416 帧。按 owner 2026-10-26 的决定，压力场景只记录、不判定。
- **重新计数**：沿用 `LookView` 现有的规则，不是它自己要的帧、视角变化或场景变化，都让计数归零。底座版本号的变化来自编辑或区段提交，这些本来就会请求一帧，所以照常归零。后台预热期间，视口持续请求帧。
- **不空转**：计数达到之后不再请求帧（Epic AC-4）。

### 导出预热与时限

- **`SceneRenderer.warm(camera, frame, n)`**：
  - 建好管线后，循环 n 次：做完本帧的前置步骤（`advanceFrame`、`applyExposure`、`base.sync`、`hit.update`），再 `probes.step(frame)`；
  - 不画光栅和后处理，所以每帧只有探针的耗时；
  - `host/render.ts` 的 `shoot` 在画第一帧之前调用它，`n = M = 64`；
  - 不构建这一层时什么都不做。
- **预热之后探针更新还是冻结**（评审 I6）：
  - 继续更新：探针的均值再多积累 76 帧，噪声更低；但压力场景每帧要更新五级、20480 个探针，每张 2K 图可能多出好几秒。
  - 冻结：探针在预热末尾已对 32 帧取过均值，之后的帧只画光栅和后处理，耗时最少。
  - 两种都实现成一个常数开关，在性能任务里实测耗时和画面差异，结论连同对照图交 owner。在定下之前，缺省继续更新，这是画质不降的那一边（「画质优先」）。
- **时限**：`estimateMs` 在 `world` 时多两项：

  ```text
  每帧固定项 probeMs(n) × (更新探针的帧数)   // 探针的耗时与分辨率无关，与级数成正比
  每像素每帧 worldNs                          // 反射补追与屏幕采样，S1 的 10 ns 按实测更新
  ```

  - `probeMs` 和 `worldNs` 按参考设备的实测加余量；
  - `shotBudgetMs` 和 `viewBudgetMs` 随之变化（Epic I6：只在构建这一层时生效）；
  - `viewBudgetMs` 在出图前不知道级数，按五级算。
- **`r_max`**：导出仍取 1.0（Epic U3）。接入探针后对照 0.5 与 1.0 的画面和耗时，结论交 owner；没有结论前不改。

### 性能

- **预算**：AC-S2-6 是整层的增量，参考场景 6 ms、压力场景 12 ms。
  - S1 的反射在参考场景占 0.1–0.9 ms，在压力场景占 7.5 ms。
  - 参考场景每帧约更新 10240 个探针（三级加轮转），而 S0 的 8192 个约 10 ms，所以就算按下面的办法优化，仍然可能超。
- **压缩的办法**（设计里已经采用）：
  - 更新：每个探针一个工作组，光线先读进共享内存；
  - 采样：纹理的硬件双线性；
  - 阴影光线：每个命中点只抽一盏灯；
  - SSGI 的漫反射降噪链在 `world` 时不建，省下的时间抵掉一部分；
  - 视口轮转。
- **仍然超出时依次动用**（评审「次要」）。每一项都会降低一点画质或跟随速度，动用前先出对照图交 owner，不自行放宽阈值：
  1. 屏幕采样改为半分辨率，再按深度和法线上采样；
  2. 轮转周期 P 加倍，跟随和收敛的帧数随之加倍；
  3. 每个探针每帧 32 条光线，噪声增大，靠滞后平均。

### 能力与显存

- **设备要求不变**：
  - 光线内核在 compute 阶段绑 7 个存储缓冲：代理层 4 个、顶点色、材质与灯光表、光线缓冲；
  - 更新内核绑 4 个存储缓冲和 3 张存储纹理；
  - 都在 WebGPU 的默认上限以内（每阶段 8 个存储缓冲、4 张存储纹理），所以 `capacity.ts` 的最低要求不变。
- **显存**（每个探针体；视口和一次导出并存时两份）：

  | 项 | 每级 | 三级 | 五级 |
  |---|---|---|---|
  | 辐照度图集，每级 640 × 640，RGBA16F | 3.3 MB | 9.8 MB | 16.4 MB |
  | 深度矩图集，每级 1152 × 1152，RGBA16F | 10.6 MB | 31.9 MB | 53.1 MB |
  | 辐照度和深度矩的历史，fp32 | 12.6 MB | 37.7 MB | 62.9 MB |
  | 光线缓冲，4096 × 64 × 16 B | 4.2 MB | 12.6 MB | 21.0 MB |
  | 状态缓冲与纹理 | < 0.3 MB | < 1 MB | < 1 MB |
  | 合计 | 约 31 MB | 约 92 MB | 约 154 MB |

  - 按 `n_alloc` 分配：三级的场景只占约 92 MB（评审「显存」）；视口的探针体跨管线重建保留，不会因为重建多出一份；
  - 不计入底座的 1 GiB 预算：预算是给场景几何的，探针体的大小只取决于级数，与面数无关；
  - 屏幕采样的纹理随分辨率变化，与 SSGI 的输出同量级。
- **失败**：探针内核的 compute 管线被设备拒绝时，按 S1 A13 的做法处理：底座 `fail(true)`，退回 P1，不重试。
  - `SceneGPU` 现在只包住 `createRenderPipeline`，S2 同样包住 `createComputePipeline`，记下要盯的内核。

### 指纹（`backend/internal/creativescene/fingerprint.go`）

- `fingerprintRender` 加 `WorldTrace int json:"world_trace,omitempty"`。`lit` 的渲染器是 `realtime_gi` 时填 `worldTraceVersion = 1`，其余情况不填；
- 先例是 W1 的 `viewTransform`：不升级版本前缀，只让受影响的那一类导出各过时一次；
- 以后这一层的画面再有明显变化时，版本号加一；
- **部署时的窗口**：指纹在后端放置导出时计算。部署后仍开着的旧标签页导出的图，会拿到新指纹，画面里却没有探针，接受，写进 proposal 的 Risk Notes。

### 提示文字

| 位置 | 现在 | S2 |
|---|---|---|
| 渲染选项卡，`off/compatibility` | 兼容模式：未绘制屏幕外的反射 | 兼容模式：未绘制屏幕外的光照与反射 |
| `off/device` | 当前设备不支持屏幕外的反射 | 当前设备不支持屏幕外的光照与反射 |
| `preparing` | 正在准备屏幕外的反射 | 正在准备屏幕外的光照与反射（后台预热期间同样显示） |
| `ready/proxy` | 场景较大：屏幕外的反射使用简化几何 | 不变：只有反射改用简化几何，探针在两档都追代理 |
| `tooLarge` | 场景过大：未绘制屏幕外的反射 | 场景过大：未绘制屏幕外的光照与反射 |
| `failed` | 屏幕外的反射暂时不可用 | 屏幕外的光照与反射暂时不可用 |
| `TraceNotReady` | 屏幕外的反射还在准备，请稍后重试 | 屏幕外的光照与反射还在准备，请稍后重试 |
| 渲染器说明（`ready`） | 画面外的物体也会出现在反射里；间接光只计算画面里可见的表面 | 画面外的物体也会出现在反射里，也会把光反弹进画面 |

## 局部设计与偏差

| # | 决定 | 依据与取舍 |
|---|---|---|
| D1 | 三到五级，间距 0.25 m 起逐级翻倍，按场景大小自动取（修订 v5；owner 2026-10-26 选定自动加级） | 参考场景的摄像机离主体 2–9 m，两级只覆盖前方约 6 m；封闭的大室内，超出范围处会退回穿墙的环境光，远处变亮。改用两级加大间距会让近处变粗，S0 的薄墙结论建立在 0.25 m 上。不做覆盖全场景的均匀网格：探针数随场景体积增长（30 × 20 × 4 m 的大厅按 0.25 m 要 15 万个）。远处几级的代价由轮转承担；它们的漏光由 AC-S2-2 的加测覆盖 |
| D2 | 盒子前方 3/4、后方 1/4（修订 v5） | 反射命中点要读探针；S0 的「居中」与「近面贴摄像机」差别很小 |
| D3 | 历史放 fp32 存储缓冲，采样用只写的 RGBA16F 图集 | 硬件双线性便宜；WebGPU 的读写存储纹理只支持 r32 类格式；导出的均值累积在 fp32 里不损精度 |
| D4 | 三个年龄分开：数据年龄定成熟度，编辑年龄定滞后，重定位年龄定重定位窗口（评审 B2、I7） | 编辑不让画面失去反弹光；拖动时探针不来回挪；导出预热和视口跟随用同一个滞后公式 |
| D5 | 探针存「未曝光值 × s_env」（评审 B1） | 未曝光值在物理天空下超出 fp16；乘 s_env 后正午和夜晚都在 fp16 的好区间里，调曝光仍不必重新收敛 |
| D6 | 探针光线只算漫反射，投影的灯按贡献抽一盏发阴影光线，随机数带光线编号 | DDGI 的做法；耗时不随灯数增长；不带光线编号时 64 条光线会抽中同一盏灯 |
| D7 | 没命中时取环境贴图糙度 0.5 的一级（修订 v5 改写 Epic 的一句） | 取漫反射卷积的那一级等于卷积两次；取最清晰的一级噪声大 |
| D8 | c（含材质 AO）写在 `ambient.a`，`A_light` 由 uniform 算出（修订 v5，评审 I1） | 光照 pass 已占满默认的每像素 32 字节；直接调用 three 的 `computeMultiscattering`，与材质一致 |
| D9 | 视口按顶层规模分档轮转，导出每帧全部更新 | 守住 AC-S2-6；导出没有每帧预算，只有时限 |
| D10 | `world` 时不建 SSGI 的漫反射降噪链 | 它的结果不再进入合成（增益 0），省下的时间计入这一层的净增量 |
| D11 | 屏幕采样单独一个 pass，输出一张纹理 | 合成所在的 pass 已接近每阶段 16 张纹理的上限；也便于改成半分辨率 |
| D12 | 级的原点只在更新那一帧移动并锁定，状态记世界格（评审 B3） | 轮转时没更新的级不会被按新原点读到别处的数据 |
| D13 | 反射命中点的间接光读探针 | Epic「命中点着色」：S2 起环境贴图那一份取上一帧的探针 |
| D14 | 代理始终构建，第 1 档两份都到齐才就绪，只就绪一次（修订 S1 D3） | 探针在两档都追代理；分两次就绪要多重建一次管线 |
| D15 | 逐级回退，所有级都没有时才退回环境贴图（评审 I2） | 原地转身时最近的区域不会先变亮再变暗；室内的环境光会穿墙 |
| D16 | 视口的探针体由 `SceneRenderer` 持有，按代理层身份做键；就绪后先后台预热再切管线（修订 v5，评审 I5） | 管线重建（景深、换档）和刚就绪时都不会暗一下；导出仍每张图新建，I3 不变 |
| D17 | `base` 改为 sRGB 编码的 8 位格式（评审「次要」） | `D_probe` 用 `kd` 承担主要的间接光，8 位线性在暗色上有 10–20% 的相对误差；不占额外的每像素预算。代价是不构建时的画面不再逐像素相同，按 R2 的「一级以内」判定 |
| D18 | 每级一个光线缓冲，不共用（不采纳评审「显存」的这一项） | 共用就要一级接一级地追和更新，与 P2 冲突；省下的最多 17 MB |

## 方案评审的处理

owner 2026-10-26 转来的方案评审，逐条核对后的处理（核对依据：`world/exposure.ts`、three 的 `PhysicalLightingModel.js:749/766/894`、`engines.ts:73`、`sceneRenderer.ts:606`、`hit.ts:246`、原理化材质的参数表）：

| 条目 | 结论 | 处理 |
|---|---|---|
| B1 未曝光单位溢出 fp16 | 属实：灯光按 `base × k` 写入，物理天空下 base 是 lux | P4、D5：存「未曝光 × s_env」，s_env 变化时换算 |
| B2 编辑把成熟度清零 | 属实 | 「探针体」三个年龄、D4 |
| B3 轮转时原点移动、数据没跟上 | 属实，同一帧里光线内核也会读到旧偏移 | 原点锁定、状态记世界格、不一致时权重 0 偏移 0（D12） |
| I1 c_E 的说法与算法 | 属实：掠射角下约 0.1；`specular_color`、`specular_intensity`、`iridescence` 都会改变它；材质 AO 被漏掉 | 「合成」：调用 `computeMultiscattering`，乘材质 AO，下限 1e-3，标量取亮度 |
| I2 细级未成熟时直接退回环境贴图 | 属实 | P5、D15：逐级回退 |
| I3 远处几级的漏光没有验收覆盖 | 属实 | AC-S2-2 的 `thin-wall` 加两个机位，让墙落在第 1、2 级；S2-U3 |
| I4 `convergeFrames` 一个函数两种用途 | 属实（`engines.ts:73`、`sceneRenderer.ts:606`） | 视口另用 `viewportSettleFrames`，导出的收敛帧保持 12 |
| I5 视口管线重建时探针冷启动 | 属实 | D16：视口持有、按代理层身份做键；就绪后后台预热；Epic 修订 v5 改「数据所有权」的措辞 |
| I6 导出每帧都更新全部探针 | 成立，代价要实测 | 「导出预热与时限」：两种实现成开关，实测后交 owner，定下前缺省继续更新 |
| I7 拖动时每帧重开重定位窗口 | 属实 | 编辑结束后、只对改动包围盒附近重开（D4） |
| P2 的措辞与派发顺序 | 属实，原文自相矛盾 | P2 改写：先全部光线内核，再全部更新内核 |
| 抽灯的随机数带光线编号 | 属实 | P3、D6 |
| 深度矩的最大距离 | 属实 | 截到 1.5 倍格对角线 |
| 环形寻址的负坐标 | 属实 | 欧氏取模；e2e 读回 GPU 算出的槽号，对照 JS |
| specs 的「与只有屏幕空间时相同」 | 属实：级联之外没有 SSGI 的反弹光 | `specs.md` 改写；「合成」写明启用的各级之外的样子 |
| `kd` 来自 8 位线性的 `base` | 属实 | D17：`base` 改为 sRGB 编码 |
| 显存：按需分配 | 采纳 | 按 `n_alloc` 分配 |
| 显存：光线缓冲共用 | 不采纳 | D18 |
| 单面材质的背面 | 属实，原文没写 | 「探针光线」写明 |
| `warm()` 的前置步骤 | 属实 | `step(frame)` 的前置步骤；`warm` 照做 |
| AC-S2-1 只测一个点 | 采纳 | AC-S2-1 加一段扫视的区域均值 |
| 参考场景 6 ms 很紧 | 属实 | 「性能」：依次动用的手段，动用前交 owner |
| 代码设计：两条入口 | 采纳 | 统一为 `step(frame)` |
| 指纹：部署时的旧标签页 | 属实，可以接受 | 「指纹」、proposal Risk Notes |

## 接口与数据契约

- **对外契约**：
  - **API、场景格式、客户端任务**：不变。
  - **指纹**：扩展。实时 GI 的 `lit` 指纹多一个字段，见「指纹」；前缀不变。这是 Epic `data: 场景文档` 的目标正文里已写的行为。
- **交付**（`epic bind --deliver`，正文以修订 v5 的批准版本为准）：
  - `architecture: 场景模块`；
  - `patterns: 场景着色写法`，含修订 v5 新增的「分叉里不放隐式导数的采样」；
  - `data: 场景文档`。
- **本 Change 的 `contracts.md`**：不写局部增量。探针是渲染端的派生状态，不是数据契约（Epic「数据所有权」）。
- **Living Spec**：见 `specs.md`。`creative/scene`：
  - 新增「实时 GI 的屏幕外间接光」；
  - 修改「实时 GI 的屏幕外反射」「渲染器与输出分辨率」「渲染设备不可用与丢失」「导出图提示场景已更新」。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/src/creative-scene/render/probes/cascade.ts` | 新增 | 级数、原点、欧氏取模的环形寻址、轮转表、α(编辑年龄)、成熟度、`viewportSettleFrames` |
| `frontend/src/creative-scene/render/probes/volume.ts` | 新增 | `ProbeVolume`：按 `n_alloc` 分配、`step`、原点锁定、版本号比对、s_env 换算 |
| `frontend/src/creative-scene/render/probes/kernels.ts` | 新增 | 光线内核、更新内核、换算内核 |
| `frontend/src/creative-scene/render/probes/lookup.ts` | 新增 | `probeIrradiance`，逐级回退 |
| `frontend/src/creative-scene/render/probes/octahedral.ts` | 新增 | 八面体编码和解码 |
| `frontend/src/creative-scene/render/trace/computePatches.ts` | 新增 | three r186 compute 补丁（`TextureNode.grad()`、PMREM 的环境旋转） |
| `frontend/src/creative-scene/render/trace/base.ts:TraceBase` | 修改 | 两份精度同时服务、代理始终构建、两个版本号、场景包围盒（不含内置地面）、改动的实例包围盒 |
| `frontend/src/creative-scene/render/trace/base.ts:TraceHandle` | 修改 | `{reflect, probes, tables, tier, key}` |
| `frontend/src/creative-scene/render/trace/base.ts:TraceNotReady` | 修改 | 提示文字 |
| `frontend/src/creative-scene/render/trace/capacity.ts` | 修改 | 两份一起估算与判档 |
| `frontend/src/creative-scene/render/trace/hit.ts:hitShading` | 修改 | `radiance`；`shade` 的间接光读探针；`r_fill` |
| `frontend/src/creative-scene/render/trace/tables.ts:TraceTables.setLights` | 修改 | 交出 `L_amb`；灯光与环境的变化计入 `lightingVersion` |
| `frontend/src/creative-scene/render/trace/checks.ts` | 修改 | 测试专用：逐像素参考、探针区域读回、槽号读回、`hitCheck` 关掉读探针 |
| `frontend/src/creative-scene/render/realtimeGI.ts:realtimeGI` | 修改 | 屏幕采样 pass、`world` 时不建 SSGI 漫反射降噪链、`base` 改 sRGB；`RealtimeGIOptions` 收 `TraceHandle` 和 `ProbeVolume` |
| `frontend/src/creative-scene/render/realtimeGI.ts:composite` | 修改 | `world` 分支；`split` 改为 `ambient.a > 0` |
| `frontend/src/creative-scene/render/giSplit.ts:installGISplit` | 修改 | `ambient.a` 写 c（含材质 AO） |
| `frontend/src/creative-scene/render/engines.ts` | 修改 | `viewportSettleFrames`；`drawnFrames`、`estimateMs`、`shotBudgetMs`、`viewBudgetMs` 加 `world`；`PROBE_WARMUP = 64`；`world` 时 `GI_INTENSITY` 取 0 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.render` | 修改 | 视口持有探针体并 `step`；就绪后的后台预热 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.build` | 修改 | 把 `TraceHandle` 和探针体交给 `realtimeGI`（原来交 `HitShading`） |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.warm` | 新增 | 导出预热 |
| `frontend/src/creative-scene/render/gpu.ts:make` | 修改 | 包住 `createComputePipeline`，`pipelineFailed` 也看 compute 管线 |
| `frontend/src/creative-scene/render/gpu.ts:gpuTestHooks` | 修改 | `probeLayer`、`probeStats`、`probeRegions`、`probeFreeze` |
| `frontend/src/creative-scene/host/render.ts:openShooter` | 修改 | `shoot` 在第一帧前 `warm` |
| `frontend/src/creative-scene/host/exportShots.ts` | 修改 | 时限按 `world` 计入探针 |
| `frontend/src/creative-scene/Viewport.tsx:LookView` | 修改 | `viewportSettleFrames`；后台预热期间持续请求帧 |
| `frontend/src/creative-scene/panels/RenderPanel.tsx` | 修改 | 提示文字与渲染器说明 |
| `frontend/scripts/creative-scene-probes.test.ts` | 新增 | `cascade.ts`、`octahedral.ts` 的单测 |
| `frontend/scripts/creative-scene-trace.test.ts` | 修改 | 两份精度的编排、判档、版本号、包围盒 |
| `frontend/scripts/creative-scene-gi.test.ts` | 修改 | `composite` 的 `world` 分支（纯数值）、时限 |
| `frontend/scripts/creative-scene-shading.test.ts` | 修改 | giSplit 写 c 的核对 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | S2 的 e2e 用例（见下） |
| `backend/internal/creativescene/fingerprint.go:Fingerprint` | 修改 | `world_trace` |
| `backend/internal/creativescene/fingerprint_test.go` | 修改 | 实时 GI 的 `lit` 指纹改变，其余不变 |

## 验证与交付

| 验收 | 怎么验证 | 运行方式 |
|---|---|---|
| AC-S2-1 | `backlit-room` 的 S2 前后并排图（owner 判断）；左墙内侧固定点在 45° 与 100° 下的亮度之比；再从 30° 到 120° 每 15° 一格扫视，左墙内侧区域的均值 | e2e `SCENE_E2E_PROBES=<目录>`，本机 Metal |
| AC-S2-2 | `cornell`、`thin-wall`、`physical-noon` 的区域辐照度，对照逐像素参考（`checks.ts`，每像素 256 条路径、3 次命中、最后一次才读探针）；`thin-wall` 另加两个机位，让墙分别落在第 1、2 级的范围里；图交 owner 看漏光 | 同上 |
| AC-S2-3 | 编辑（拖动物件、关一盏灯、改太阳高度）之后逐帧读回探针区域的平均值，记下回到 10% 以内的帧数；拖动期间读回的 `coverage` 不归零（成熟度不随编辑清零，评审 B2 的回归用例）；停止操作后 `gpuTestHooks.viewportFrames` 停在 `viewportSettleFrames` 以内，最后几帧的逐帧变化小于 1% | 同上 |
| AC-S2-4 | 隐藏标签页里连续两次导出参考场景的多台摄像机，逐像素比较；压力场景只记录 | e2e `SCENE_E2E_TRACE`（S1 用例扩展） |
| AC-S2-5 | 兼容模式、`forceCompatibility`、`traceUnsupported`、`traceDelayMs`（就绪前）下的导出，与 S2 之前的存档对照：giSplit 那一步提交后逐像素相同，`base` 改 sRGB 之后在 R2 的一级以内；提示文字 | 默认 e2e 加 Metal |
| AC-S2-6 | S1 的 `SCENE_E2E_TRACE_PERF` 用例：墙钟，30 帧中位数，重复 3 次；带与不带这一层之差；冷、热着色器缓存分开；导出和 `render_view` 的耗时对照新时限；导出预热之后更新与冻结两种都测 | e2e，本机 Metal |
| AC-S2-7 | S1 的 AC-S1-8 用例照跑，代理始终构建 | 同上 |
| AC-S2-8 | `probeLayer = 'original'` 与缺省（代理）各导出一张，统计差异；并排图交 owner | e2e，本机 Metal |
| AC-S2-9 | 延期（owner 2026-10-26）：验收时按 deferred 记录并写明跟进 | — |
| AC-S2-10 | 后端单测：实时 GI 的 `lit` 指纹与 S2 前不同，工作台的 `lit`、`clay`、`line` 不变；e2e 读画布看到「场景已更新」 | `make check-go PKG=./internal/creativescene/...` 加 e2e |
| AC-S2-11 | `make check-frontend`；verify 时逐条核对三条契约 | 日常门禁 |

- **单测**：
  - `cascade.ts`：级数、原点对齐、欧氏取模（含负坐标）、环形重置的判定、轮转表、α(编辑年龄)、成熟度、`viewportSettleFrames`；
  - `octahedral.ts`：JS 往返；
  - `composite` 的 `world` 分支：在纯数值上，`coverage` 为 0 时等于 `A_env · ao + A_lt · ao`；
  - 底座：两份精度的编排（一次就绪、换档、几何失败）、版本号、包围盒，沿用 S1 的假构建池。
- **GPU 侧的对照**：
  - 探针槽号：在一组含负坐标的位置上，读回 GPU 算出的槽号和世界格，与 `cascade.ts` 的 JS 结果比较；
  - 八面体编码：读回 GPU 编码的结果，与 JS 比较；
  - 都放在 `checks.ts`，只在 e2e 页面动态加载。
- **逐像素参考**：搬进 `checks.ts`。S0 的教训照用：参考在最后一次命中才读探针，否则会继承探针的漏光。
- **证据**：放在 S2 的 `discovery/research/evidence/`，沿用 30 MB 的规则，超出的留在本机并记下重新生成的命令。
- **提交顺序**：
  1. giSplit 和 `split` 的改动单独提交，先过 AC-S2-5 的逐像素对照；
  2. `base` 改 sRGB，过 AC-S2-5 的一级以内；
  3. 底座的两份精度；
  4. 探针体和合成；
  5. 标定、时限和提示；
  6. 指纹（后端）。
- **门禁**：日常 `make check-frontend`、`make check-go PKG=./internal/creativescene/...` 和定向 e2e；合回 develop 用 `make check-merge`。
- **回退**：S1 的开发者开关（`localStorage['scene.trace'] = 'off'`）同时关掉探针，回到 P1。

## 未决事项

| # | 问题 / 假设 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| S2-U1 | three r186 能否在 compute 里生成显式 LOD 的纹理采样（`textureSampleLevel`），以及向 RGBA16F 存储纹理写入（`textureStore`） | 光线内核读图集，更新内核写图集 | 先写最小的 compute 用例。不行就在 `computePatches.ts` 里补；仍不行时，光线内核改用 `textureLoad` 加手工双线性，只有它慢一些 | 探针内核任务开始时 |
| S2-U2 | 反射的片元多绑 3 张探针纹理后，是否超过每阶段 16 张的上限 | 命中点的间接光读探针（D13） | 接入时数实际的绑定；超出时把状态并进辐照度图集的 alpha，少一张 | 命中点接入探针的任务 |
| S2-U3 | 重定位能否把 `thin-wall` 天花板的 +24% 压到 ±15%；墙落在第 1、2 级（0.5、1 m 间距）时能否压住 | AC-S2-2 | 实测；压不住时试探针分类、32 × 32 深度矩，结论和对照图交 owner。Epic AC-3 允许 owner 放宽薄墙场景，不由我决定 | 漏光任务 |
| S2-U4 | 轮转分档能否在参考场景同时守住 AC-S2-6 和 AC-S2-3 | 性能与收敛 | 性能任务实测；超出时按「性能」一节的顺序动用，每一项先交 owner；压力场景只记录收敛帧数（owner 2026-10-26） | 性能任务 |
| S2-U5 | 编辑跟随期间的闪烁是否可以接受 | 观感 | 缺省按 A0 = 12、α = 0 实现（owner 2026-10-26）；录拖动的对照视频，含 α = 0.5 的做法，交 owner 看后定 | 标定任务 |
| S2-U6 | `r_fill` 是否需要不为 1 | 有环境光时的整体亮度 | AC-S2-2 对照逐像素参考；要改时先交 owner 看图（「画质优先」「CG 观感优先」） | 标定任务 |
| S2-U7 | 小于探针间距的接触阴影是否要用短半径的 AO 补（Epic「合成的接入点」） | 墙角和桌腿下的观感 | 做一组对照图交 owner；缺省不加 | 标定任务 |
| S2-U8 | 导出的 `r_max` 是否仍取 1.0（Epic U3） | 导出耗时 | 对照 0.5 与 1.0 的画面和耗时，交 owner；没有结论前不改 | 性能任务 |
| S2-U9 | 导出预热之后，探针继续更新还是冻结 | 导出耗时与画面 | 两种都实测，结论连同对照图交 owner；定下前缺省继续更新 | 性能任务 |
