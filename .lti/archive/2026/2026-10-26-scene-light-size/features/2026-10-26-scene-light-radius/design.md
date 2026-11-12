# Change 详细设计

## 继承的设计

- **依据**：Epic `scene-light-size` 批准基线 v3（v2 加上 U8 的结论）。本子项绑定全部 design 条目，交付 `data: 光源尺寸`。光源模型、不变量 I1–I6、`PhysicalLightingModel` 的扩展方式、PCSS 的参数与四个接入点、原始深度读取（U7）、尺寸为 0 不挂滤波、`workbench` 累积（U2）、接触阴影（U4）、灯光表扩容、天空角径、体积的钳位、编辑器和指纹（D4），都以 Epic design 为准，本文只写落到代码上的做法和 Epic 没写到的局部决定。
- **S0 结论**：子项 `scene-light-size-spike` 的 `discovery/research/`。
  - `integration.md`「给 S1 的清单」；
  - `depth-read-backends.md`：两个后端的写法和两个 three 缺陷；
  - `soft-shadow-candidates.md`「反应舱」：`Loop` 的坑、冷启动测法；
  - `thresholds.md`。
  - 原型代码在 `discovery/research/prototype/spike/light/`：`soft.ts` 的 `mapPCSS`、`pointPCSS`、`perShadow`、`receiverPlane`，以及 `sphere.ts` 的 `hybrid`、`horizon`。S1 照搬算法，去掉 URL 参数、调试输出、`unroll` 和 `smtrace`。
- **经验**：
  - `tsl-loop-fn-hoisted-in-branch`：每份阴影新建带 `Loop` 的 `Fn`；
  - `three-rebuild-materials-by-lights`：材质只在灯光组合的缓存键变化时重建；
  - `webgpu-perf-warm-shader-cache`：冷启动测法。
- **本次边界**：点光、聚光、平行光，以及天空的太阳和月亮。面光源（`type = area`、LTC、矩形核、`lightAxisX/Y` 等）属于 S2，本次只在灯光表里留出位置。

## 领域模型与业务规则

沿用 Epic「领域模型与业务规则」。灯光对象新增两个字段，归 `object:<id>:content` 单元，不新增身份或聚合。本次落到代码上的规则：

| 规则 | 服务端（权威） | 前端 |
|---|---|---|
| 字段归属：`radius_m` 只属于 `point`、`spot`，`angular_diameter_deg` 只属于 `directional` | `lightFields` 加入两个字段；写错类型由 `checkLightFields` 返回 400 并指出字段 | `state/apply.ts` 的 `lightFields` 同步，`mergeLight` 对该类型没有的字段返回 null |
| 缺省值：`radius_m` 0.1，`angular_diameter_deg` 0.545 | `completeLight` 补齐；`decodeScene` 对每盏灯补齐 | `lightDefaults` 改为按类型分开的表 |
| 取值范围：0…10 m，0…30° | `validateLight`，非有限值返回 400 | 属性面板 `NumberField` 的 min、max |
| 存量 | 读取时补齐；`view.go` 的 `completedScene` 发现灯光缺字段时，返回补齐后的场景 | 无 |
| 指纹 | `lit` 指纹在「有灯光对象，或天空为 `procedural`、`physical`」时计入 `lights-v2` | 无 |

渲染端的派生量（代表点、半影、滤波半径）不落库，按帧从灯光对象上读取。

- **光源尺寸**：渲染端只用一个数。灯光子类上的 `size` 字段，球形光源是半径（m），平行光是角径（°）。
  - 文档灯光：取自 `radius_m` 和 `angular_diameter_deg`。
  - 天空的太阳和月亮：取自 `sky.sun_size_deg`，或常数 0.52°、0.545°。
- **U8**：半径不乘物件缩放，与 Blender 一致（owner 2026-10-27 核对，Epic 修订 v3）。取半径的地方只有 `objects/lights.ts` 的 `applyLight` 和 `LightIcon` 两处。

## 模块职责与协作

```
服务端 creativescene               写入、补齐、校验、指纹（权威）
   │  场景 JSON
   ▼
objects/SceneLights.tsx            按 (type, 尺寸是否为 0) 建灯；两者之一变化时重建（见「0 与非 0 的切换」）
objects/lights.ts                  buildLight → SceneSphereLight / SceneSpotLight / SceneSunLight
                                   applyLight → light.size；size > 0 时 attachSoftShadow
objects/Lighting.tsx               程序化天空、物理天空的太阳和月亮也用 SceneSunLight
   │
   ▼
render/lightModel/  （新）
   ├─ lights.ts       灯光子类与阴影子类（无参构造，覆写 copy()），sizeOf(light)
   ├─ nodes.ts        SphereLightNode / SizedSpotLightNode / SunDiscLightNode；installLightModel(renderer)
   ├─ direct.ts       包装 PhysicalLightingModel.direct：hybrid 高光 + Snyder 漫反射
   ├─ shadowFilter.ts PCSS：mapPCSS(spotGeometry | sunGeometry)、cubePCSS；rawDepth()；attachSoftShadow()
   └─ formulas.ts     有效距离衰减、hybrid、Snyder、光源表面采样；光栅节点和 trace/hit.ts 共用（I4）
   │
   ├──► render/sunShadow.ts         级联随 copy() 带上滤波函数；ownNames 后缀按序号
   ├──► render/volumes/             体积按中心点：衰减钳 max(d², r²)；有尺寸的灯的阴影图手动比较
   ├──► render/sceneRenderer.ts     standIn 经 copy() 带上滤波函数；workbench 出图累积；mainLight 的接触阴影
   ├──► render/realtimeGI.ts        主光有尺寸时 contactOn = 0
   └──► render/trace/tables.ts、hit.ts   灯光行 5 个 vec4；命中点同一套公式，阴影光线在光源表面采样
```

- **依赖方向**：`render/lightModel/` 不依赖 `objects/`、编辑器外壳和 `render/trace/`。`objects/` 和 `render/trace/` 依赖它。
- **注册**：`render/gpu.ts` 在 `new WebGPURenderer` 之后调用一次 `installLightModel(renderer)`。它做两件事：
  - 为三个灯光子类调用 `renderer.library.addLight`；
  - 在 `PhysicalLightingModel.prototype` 上装一次 `direct` 的包装，做法同 `installGISplit`。
  - 白模的工作室光、缩略图和资产预览用的是 three 原生的灯光类，按构造函数精确匹配，不受影响。
- **服务端**：
  - `backend/internal/creativescene` 负责字段、补齐、校验和指纹；
  - `backend/internal/creativeagent/scene_tools.go` 负责工具说明。
  - 外部助手（MCP）现在只开放读取工具（`external.go` 的 `externalCatalog`），不带场景工具，所以本次不涉及 MCP。Epic 架构里「外部助手也用这份说明」的说法以现状为准，等 MCP 开放场景写入时再生效。

## 关键机制

### 灯光与阴影子类

| 类 | 继承 | 节点 | 阴影子类 |
|---|---|---|---|
| `SceneSphereLight` | `PointLight` | `SphereLightNode`（继承 `PointLightNode`） | `SizedPointShadow`（继承 `PointLightShadow`） |
| `SceneSpotLight` | `SpotLight` | `SizedSpotLightNode`（继承 `SpotLightNode`） | `SizedSpotShadow`（继承 `SpotLightShadow`） |
| `SceneSunLight` | `DirectionalLight` | `SunDiscLightNode`（继承 `DirectionalLightNode`） | `SizedSunShadow`（继承 `DirectionalLightShadow`） |

- **构造与复制**：
  - 灯光子类无参构造，构造时把 `this.shadow` 换成对应的阴影子类；
  - `copy()` 复制 `size`；
  - 阴影子类的 `copy()` 复制 `filterNode` 和 `lightSize`。
  - 这样 `clone(false)`（standIn）和 `shadow.clone()`（`CSMShadowNode` 的级联）都会带上滤波函数。
- **节点的 `setupDirect`**：
  - **构建时尺寸为 0**：原样返回父类的结果，不加 `sized`，也不乘衰减比值。着色器与修复条纹后的基线相同，不会出现 r = 0 时 Snyder 的 1/0、∞·0（AC-S1-2）。构建时读到的尺寸是准确的，因为 0 和非 0 之间的切换会重建灯（见「0 与非 0 的切换」）。
  - **尺寸大于 0**：先调用父类，再在返回值上加 `sized: { lightVector, sinHalf }`：
    - 球：`sinHalf = r / d`，钳在 [10⁻³, 0.9999]，避免极小的 r 让 Snyder 的 sin² 下溢；
    - 圆盘：`sinHalf = sin(θ/2)`，`lightVector` 是朝向光源的单位向量。
  - `lightColor` 乘有效距离的比值 `max(d^decay, 0.01) / max(max(d, r)^decay, 0.01)`。S0 的教训：不要用 `getDistanceAttenuation` 再算一遍。
  - `size` 用 `reference('size', 'float', light).setGroup(renderGroup)` 按帧读取，非 0 之间改尺寸不重编。
- **光照模型**：`direct.ts` 包装 `PhysicalLightingModel.direct`。
  - 输入里没有 `sized` 时，原样调用。
  - 有 `sized` 时分三份，基础层和 clearcoat 各按自己的粗糙度算 `hybrid`：
    - **基础层高光**：按基础层粗糙度的 `hybrid` 改写方向、粗糙度和能量系数，调用一次原来的 `direct`，只取 specular，这次关掉 clearcoat 和 sheen；
    - **clearcoat**：按 clearcoat 粗糙度另算一遍 `hybrid`，得到自己的代表点方向、加宽后的粗糙度和能量系数，直接用 `BRDF_GGX` 累加到 `clearcoatSpecularDirect`。照度项照抄 three r186 `PhysicalLightingModel.direct` 里 clearcoat 那几行（用 `clearcoatNormalView`），并注明版本；
    - **漫反射**：以 Snyder 的弯折方向再调用一次 `direct`，只取 diffuse，这次关掉 clearcoat，sheen 随它走。
  - 原型让 clearcoat 跟着基础层的方向和能量走。基础层粗糙时，基础层的 s 都用在加宽上，代表点不平移；clearcoat 只换了粗糙度，却跟着中心方向，结果仍是点光那样的小亮点（评审例：基础层 0.5、clearcoat 0.05、s = 0.15）。所以分开算，AC-S1-4 另加一块 clearcoat 高光板。
  - 球和圆盘用同一个式子，区别只在 `sinHalf` 的来源。
- **衰减**：平行光没有衰减，只有球形光源乘有效距离的比值。

### 0 与非 0 的切换

- three 只保留一盏灯第一次构建的阴影节点；材质只在灯光组合的缓存键（灯的 id 与 `castShadow`）变化时重建（经验 `three-rebuild-materials-by-lights`）。
- 所以尺寸在 0 和非 0 之间切换时，`SceneLights.tsx` 像换类型一样重建这盏灯：`useMemo` 的键从 `light.type` 改为 `light.type` 加上「尺寸是否大于 0」。
  - 新灯的 id 不同，材质重编一次，代价与新增一盏灯相同（Epic「尺寸为 0」）；
  - 非 0 之间的变化只更新 `size`，不重编。
- 天空的太阳也按同样的键重建。程序化天空恒为 0.545°，不会切换；物理天空的 `sun_size_deg` 本来就大于 0。

### 软阴影的挂载（`attachSoftShadow`）

`applyLight` 在 `size > 0` 且 `castShadow` 时调用，必须在第一次构建之前完成。平行光（文档灯光和天空的太阳都是）要在 `cascadeSunShadow` 之前调用：`CSMShadowNode` 第一次构建时 clone 的就是这时的 `light.shadow`。

1. `shadow.lightSize = size`。逐帧更新，滤波函数用 `reference` 读取。
2. `shadow.filterNode`：
   - 聚光用 `mapPCSS(spotGeometry)`；
   - 平行光用 `mapPCSS(sunGeometry)`；
   - 点光用 `cubePCSS()`。
3. **原始深度**：滤波函数第一次构建时，对 `depthTexture` 设 `compareFunction = null`、`minFilter = magFilter = NearestFilter`。两个后端用同一套写法（U7），全部读原始值、手动比较。
   - 立方体贴图的读取，WebGPU 用 `level(expression('0u', 'float'))`，WebGL 用隐式 level 的 `texture()`。
   - 两处都写注释，说明绕开的是 three r186 的哪个缺陷，并各配一条 e2e。
4. **`Loop` 的坑**：滤波函数用 `perShadow` 包一层。每次构建都新建内层带 `Loop` 的 `Fn`，防止级联共用时被提前声明。
5. **参数**：16 次搜索加 32 次滤波，半影上限 48 texel，接收面偏置的坡度上限 20 倍。核按 `interleavedGradientNoise(screen + frameId × 5.588238)` 随帧旋转。
6. **尺寸为 0**：不做上面任何一步，阴影图保留比较模式和 three 的 PCF。

### 尺寸为 0 时的条纹

- **现状**：聚光和点光的 `bias`、`normalBias` 都是 0，斜照的墙面有自阴影条纹。级联用 1.5 texel 的 `normalBias`，没有条纹。
- **采用**：给尺寸为 0 的聚光和点光加法线偏置，量是接收点处 1.5 个 texel，与级联同一口径。
  - 聚光一个 texel 是 `2 d tan(fov/2) / mapSize`，点光是 `2 d / mapSize`，d 是接收点到灯的距离。
  - 做法：在 `VolumeShadowNode` 和 `VolumePointShadowNode` 里覆写 `setupShadowPosition`。这两个类本来就是这两种灯的阴影节点。
    - three r186 的 `ShadowBaseNode.setupShadowPosition` 只有一行：`shadowPositionWorld.assign(material.receivedShadowPositionNode || context.shadowPositionWorld || positionWorld)`。
    - 覆写为同一个来源加上 `normalWorld × 1.5 · texel(d)`。`shadow.normalBias` 保持 0，`setupShadow` 里那一项为 0，不需要改。
    - 只有几行，注明 three 的版本。
  - 注释写明两个前提：
    - `renderer.highPrecision` 必须是关的。打开时 `ShadowNode.setupShadow` 走 `positionLocal` 分支，忽略 `shadowPositionWorld`，偏置失效。项目现在没有打开。
    - 同一个类也服务带尺寸的灯，所以按 `shadow.filterNode` 有没有设分支：带尺寸时不加这项，只用滤波函数里的接收面偏置。
- **备选**：覆写行不通时，退回固定的 `normalBias = 0.05 m`。S0 实测这个值在 0.1–3 m 的板上干净，但小物件的影子可能离开物体。
- **基线**：两种做法都要出 `occluder-wall` 和 `frame-two-lights` 的新基线图，交 owner 看过后再作为 AC-S1-2 的基线提交（`SCENE_E2E_LIGHT_BASELINE`）。
- 带尺寸的灯用接收面偏置，不加法线偏置，与 S0 测量时相同。

### 级联与 standIn

- **级联**：
  - `cascadeSunShadow` 不变。`CSMShadowNode` 只在第一次构建时 `light.shadow.clone()` 一次（`CSMShadowNode.js:175`），建出每一级的阴影；`SizedSunShadow.copy()` 把滤波函数带过去，每级读自己的相机。
  - 每一级读的是自己那份副本上的 `lightSize`。灯在非 0 尺寸之间变化时不重建，所以 `fitCascades` 每帧把原灯的 `lightSize` 抄到每一级。这一步和每级的 `normalBias` 写在同一个循环里（`sunShadow.ts:211`）。会碰到的情况：
    - 物理天空调 `sun_size_deg`，也就是 AC-S1-5 本身；
    - 文档平行光改角径；
    - 物理天空太阳和月亮互换：投影的始终是 `parts.sun` 这一盏，它的尺寸在 `sun_size_deg` 和 0.52 之间切换。
  - 同一个循环里，带尺寸时 `normalBias` 设为 0（S0 的级联 PCSS 只用接收面偏置），尺寸为 0 时照旧 1.5 texel。
- **`ownNames` 的后缀**：从 `csm${this.id}` 改为 `csm${k}`，k 是这盏平行光在本次构建的 `builder.lightsNode.getLights()` 里、所有投影平行光中的序号。
  - three 按 `Object3D.id` 给灯排序（`LightsNode.js:36`）。standIn 的副本按遍历顺序新建，id 的先后与遍历顺序一致；只有原灯的 id 顺序也与遍历顺序一致时，序号才一致。
  - 不一致时只是 standIn 编出的代码对不上、第一帧多编一次，与现状相同，不影响画面。
  - 两盏投影平行光的命名冲突仍然避开：同一着色器里序号不同。
- **standIn**：
  - `shadowLike` 照旧按原灯的阴影节点种类给副本建同类节点；
  - 滤波函数和 `lightSize` 随 `clone(false)` → `copy()` 带过去，不需要另外接线。

### 体积

体积按中心点计算（Epic「体积」）。本次要处理两件事：

- **衰减**：`render/volumes/lights.ts` 的 `spot`、`point` 用有效距离，与 `formulas.ts` 共用同一个函数。槽位 uniform 增加一个 `radius`，由 `state.ts` 的 `writeSlot` 写入；候选灯的排序（`state.ts` 的 `Candidate`）也用 `max(d, r)`。
- **阴影图**：Epic 没有写到这一点。带尺寸的灯的阴影图去掉了比较模式，体积里的 `shadow2D` 和点光的 `.compare()` 会编译失败：WGSL 拿到普通 sampler，WebGL 的 uniform 类型不符。
  - 槽位按灯是否带尺寸选读法：带尺寸时读原始深度、手动比较，否则照旧比较采样。
  - **march 的重建**：march 只在签名变化时重建，签名现在只由类型和是否投影组成；阴影图每帧换进同一个纹理节点（`bindVolumeTextures`）。所以「灯重建了」不会让 march 重建。两盏同类型的聚光（一盏带尺寸、一盏不带）互换槽位，也会让读法和贴图对不上。
    - 做法：`Slot`、`Candidate`、`structure` 和 `signatureOf` 都加上「是否带尺寸」。
    - 签名里带尺寸的槽位写成 `spot+s` 这样的形式。尺寸在 0 和非 0 之间切换，或者带尺寸与不带尺寸的灯互换槽位时，march 都会重建。
  - **边缘的观感**：尺寸为 0 时，PCF 模式下阴影图是线性过滤（`ShadowNode.js:366-369`），硬件比较采样等于 2×2 的 PCF。单样本的最近点比较会在体积光束边缘出现 texel 台阶。太阳总带尺寸，所以所有体积场景都会受影响。
    - 做法：手动做 2×2 双线性比较。2D 贴图读四个相邻 texel，按小数部分插值比较结果；立方体贴图在面内取同样的 2×2 邻域。这样保持现状的观感。
  - 立方体贴图同样要绕开 U7 的两个缺陷。

### workbench 的出图累积（U2）

- **什么时候累积**：`workbench` 的 `lit` 画面，并且场景里有带尺寸的投影灯（`hasSizedCaster(scene)`，天空的太阳也算），才画 `WORKBENCH_FRAMES = 16` 帧并平均。其余情况仍是 1 帧：
  - `clay` 和 `line` 不读灯光；
  - 尺寸都为 0 时 PCF 的噪声不随帧变化，多帧没有意义；
  - 视口（交互）不累积。
- **导出与 `render_view`**：导出（`use: 'export'`）和助手的回看（`use: 'view'`）都累积。回看的分辨率小，16 帧的代价有限；助手要根据回看判断阴影，颗粒会误导它。
- **代价**：S0 的「导出 +30%」是在 1 帧的 `workbench` 上测的，「每帧约 2 ms」是视口的数。导出每帧都会重画全部阴影图、跑体积的 march，8K 的像素是参考分辨率的 12 倍。所以：
  - **第 2–16 帧冻结阴影图**：导出时摄像机不动，级联只取决于摄像机，16 帧里阴影图相同，只有核在转。第 1 帧之后对所有投影灯设 `shadow.autoUpdate = false`，导出结束后恢复；
  - 体积的 march 照常每帧跑，它的抖动是静态 IGN，帧间相同；
  - AC-S1-7 加测 4K、8K 和含体积的场景。导出时长超过 +50% 时，先查哪一项占大头，再交 owner。
- **代码**：
  - `render/engines.ts`：`framesFor`、`drawnFrames` 增加「是否有带尺寸的投影灯」这个输入，`workbench` 时返回 16 或 1。导出耗时的估算随之变化。
  - `host/render.ts`：按 `hasSizedCaster(three)` 和 `options.kind === 'lit'` 决定帧数；负责冻结和恢复阴影图。
  - `render/sceneRenderer.ts`：`accumulate` 不再限于 `gi`。`workbench` 的画面接 `AccumulateNode(picture, 0)`，没有预热帧。
- **逐像素相同**：两次导出逐像素相同（AC-S1-10）。核的旋转只取决于 `frameId` 和像素；`frameId` 由 `gpu.ts` 推进，`resetFrames()` 让每次导出从 0 开始。

### 接触阴影（U4）

`realtimeGI.ts` 的 `shadow.updateBefore` 里，`contactOn` 改为「有主光，并且 `sizeOf(main) === 0`」。不满足时与没有主光一样直接返回，不再跑屏幕空间追踪，合成按权重 0 处理。`mainLight` 不变。

- 主光就是天空的太阳时，它的角径总大于 0，所以有太阳的场景一律不再追屏幕空间接触阴影；
- 只有平行光或聚光尺寸写成 0 时才保留。

这是 U4 的直接结果：S0 测得接触阴影在有尺寸时只叠出硬暗带。

### 灯光表与命中点（AC-S1-6）

**灯光表**（`render/trace/tables.ts`）：`LIGHT_ROW` 从 4 改为 5。这是一次定下的格式，S2 不再改格式，只填面光源的槽位。

| vec4 | directional | point | spot | area（S2 填） |
|---|---|---|---|---|
| 0 | (type, casts, 0, 0) | (type, casts, distance, decay) | 同点光 | (type, casts, shape, 0) |
| 1 | (朝向光源的方向, 0) | (位置, 0) | (位置, cos 光锥) | (中心, 半宽) |
| 2 | (颜色 × 强度, 0) | (颜色 × 强度, 0) | (颜色 × 强度, cos 半影起点) | (颜色 × 亮度, 半高) |
| 3 | 0 | 0 | (照射方向, 0) | (法线 −Z, 0) |
| 4 | (sin(θ/2), 0, 0, 0) | (半径, 0, 0, 0) | (半径, 0, 0, 0) | (局部 X 轴, 0) |

- `writeLight` 填第 4 行时要换算单位：球形光源写半径（m）；平行光的 `sizeOf` 是角径（°），写入 `sin(θ/2)`。`relit` 比较的范围随 `LIGHT_ROW` 自动变化。
- **着色**（`hit.ts` 的 `lightAt` 和蓄水池采样里的直接光）：
  - 照度乘有效距离的比值；
  - 漫反射用 Snyder 的弯折方向；
  - 高光用 `hybrid` 改写后的方向、粗糙度和能量系数；
  - 两处都调用 `formulas.ts`，不再各自内联 `getDistanceAttenuation`。
- **用弯折后的方向判断受光**：
  - `lightAt` 里 `dotNL > 0` 的判断（`hit.ts:260`）和蓄水池的权重（`hit.ts:429` 的 `irradiance`）都改用 Snyder 弯折后的方向和照度系数 F。
  - 否则明暗交界线外侧的包裹光在命中点上会被截掉，蓄水池抽中各盏灯的概率也与实际照度不一致。
- **阴影光线**：
  - 指向光源表面上的随机点：球是对中心张角 `asin(r/d)` 的圆锥均匀采样，平行光是半角 θ/2 的圆锥；
  - 球形光源的 `reach` 停在采样到的光源表面点前 1 mm，而不是追到球心。否则包住灯的灯泡模型会自遮挡；
  - 随机数用 `hit.ts` 现有的 PCG，种子取帧序号和像素，靠实时 GI 的时域累积收敛；
  - 尺寸为 0 时退回中心方向和现在的 `reach`，与现状相同。

### 天空的太阳和月亮

- **程序化天空**：`Lighting.tsx` 的 `Sun` 从 JSX 的 `<directionalLight>` 改为 `<primitive object={sun}>`，`sun` 是 `useMemo` 建的 `SceneSunLight`。`size` 取 0.545，`attachSoftShadow` 在 `cascadeSunShadow` 之前调用。
- **物理天空**：`sun` 和 `aside` 两盏灯改为 `SceneSunLight`。
  - 太阳的 `size` 取 `sky.sun_size_deg`，月亮取 0.52；
  - 太阳和月亮互换时（351–359 行），`size` 跟着换，级联靠 `fitCascades` 每帧同步（见「级联与 standIn」）；
  - 只有投影的 `sun` 在 `cascadeSunShadow` 之前挂滤波函数；
  - `aside` 不投影，只用 `size` 算圆盘的高光和漫反射。

### 编辑器

- **属性面板**（`panels/LightFields.tsx`）：
  - 点光和聚光加「半径（m）」，0–10，步长 0.01；
  - 平行光加「角径（°）」，0–30，不设步长（`step="any"`）。缺省值 0.545 不在 0.05 的步进格上，`type="number"` 会判为 `stepMismatch`；
  - 合并键为 `${id}:light.radius_m`、`${id}:light.angular_diameter_deg`。
  - 换类型照旧只写 `{type}`，由服务端（以及 e2e 内存服务器所用的 `apply.ts`）按字段表保留或补齐。
- **图标**（`objects/SceneObjects.tsx` 的 `LightIcon`）：点光和聚光在半径大于 0 时加一个朝向摄像机的圆环（Blender 的画法），世界空间里的半径等于 `radius_m`。
  - **位置、朝向、大小**：在 `useFrame` 里每帧直接写圆环的 `matrixWorld`，`matrixAutoUpdate = false`。矩阵由三部分组成：灯的世界位置、摄像机的朝向、半径。
    - 着色用的是世界空间里的半径，所以要绕开全部父级缩放，包括外层分组和非均匀缩放，不能只抵消物件自己的缩放；
    - 朝向也在这里逐帧更新。
  - **可见性**：缺省半径 0.1 m 的圆环落在 0.15 m 的实心球里面（`SceneObjects.tsx:206`）。圆环用 `depthTest = false`，并给一个较大的 `renderOrder`，画在实心球之上。
  - 现有 0.15 m 的实心球、光锥和光束不变，选取目标不变。
- **新建菜单**（`editing.ts` 的 `newLight`）：不写尺寸，由服务端补缺省值。

## 局部设计与偏差

| # | 决定 | 依据与取舍 |
|---|---|---|
| L1 | 0 与非 0 的切换靠重建灯实现，键为 (type, 尺寸 > 0) | three 只在灯光组合的缓存键变化时重建材质（经验）。另一种做法是加一盏占位灯，但那样会改变所有材质的缓存键；重建这一盏灯与「换类型」的现有路径相同 |
| L2 | 条纹修复：按接收点处 texel 换算的法线偏置，覆写只有一行的 `setupShadowPosition`；固定 0.05 m 作备选 | 固定值与场景尺度有关：小物件的影子会离开物体，大场景偏置又不够。前提是 `highPrecision` 关闭、按有没有 `filterNode` 分支，写进注释。基线图由 owner 看过再定 |
| L3 | 体积在带尺寸时读原始深度，手动做 2×2 双线性比较；march 的签名加上「是否带尺寸」 | Epic 没有写到，不做的话体积着色器编译失败。2×2 双线性保持现状的光束边缘；签名不加尺寸，march 就不会随切换重建。仍按中心点计算，不改 Epic 的语义 |
| L4 | `workbench` 的 `lit` 画面在有带尺寸的投影灯时累积 16 帧，导出和回看都是；第 2–16 帧冻结阴影图 | U2 的落地。`clay`、`line` 不读灯光；没有带尺寸的灯时多帧没有意义；冻结阴影图控制高分辨率导出的代价 |
| L5 | `ownNames` 后缀按投影平行光的序号 | S0 的建议。序号一致的前提是原灯的 id 顺序与遍历顺序一致；不一致时只是 standIn 多编一次，与现状相同 |
| L9 | clearcoat 单独算 `hybrid`，直接累加 `BRDF_GGX` | 原型让 clearcoat 跟着基础层的方向和能量走，基础层粗糙时 clearcoat 仍是小亮点 |
| L10 | 尺寸为 0 的光源节点原样返回父类 | 着色器与基线相同，避开 r = 0 时 Snyder 的除零 |
| L11 | 级联的 `lightSize` 由 `fitCascades` 每帧从原灯抄到每一级 | `CSMShadowNode` 只 clone 一次，非 0 之间改尺寸不重建 |
| L6 | 太阳在场时接触阴影一律关掉 | U4 的直接结果：太阳的角径恒大于 0 |
| L7 | 外部助手不在本次范围 | MCP 现在不带场景工具，与 Epic 架构里的说法不同，以现状为准 |
| L8 | 圆盘的高光和漫反射与球共用式子，`sinHalf = sin(θ/2)` | 无限远的圆盘张角恒定，等价于 r/d 恒定的球。S0 只在球上测过；平行光的高光按 AC-S1-4 在 0.545° 和 5° 上复核 |

**待验证假设**：
- 混合模型的参数在粗糙度 0.4 上是否仍达标（AC-S1-4）；
- 4 盏灯的性能（AC-S1-7）；
- 级联在平行光大角径（例如 10°）时，最远一级的半影上限。S0 测过 10°，宽半影最多偏窄 19%，在 AC-1 以内。

## 接口与数据契约

- **契约状态**：扩展，向后兼容。交付 Epic 的 `data: 光源尺寸`，正文见 Epic `contracts.md`，不复制。本子项的 `contracts.md` 留空。
- **OpenAPI**：
  - `CreativeSceneLight` 和 `CreativeSceneLightPatch` 加 `radius_m`、`angular_diameter_deg`，描述写明适用类型、范围和缺省值；
  - 运行 `make generate`，提交 `backend/internal/platform/httpapi/api.gen.go` 和 `frontend/src/api/schema.d.ts`。
- **工具说明**：`scene_tools.go` 第 83 行的灯光字段说明里补上：
  - point 和 spot 有 `radius_m`（光源半径 0…10 m，缺省 0.1，0 为硬影子）；
  - directional 有 `angular_diameter_deg`（0…30°，缺省 0.545）。
- **指纹**：
  - `fingerprint.go` 加常量 `lightModelVersion = "lights-v2"`；
  - 计入方式：在被序列化的结构上加 `light_model`（omitempty）字段，只在 `pass == lit`，且有灯光对象或天空为 `procedural`、`physical` 时填写；
  - 其余场景和 `clay`、`line` 的指纹字节不变。
- **Living Spec**：见 `specs.md`。ADD「光源尺寸与软阴影」，MODIFY「导出图提示场景已更新」，加一段说明和一个场景。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativescene/model.go:Light` | 修改 | 加 `RadiusM`、`AngularDiameterDeg`，指针，omitempty |
| `backend/internal/creativescene/model.go:lightPatch` | 修改 | 同上 |
| `backend/internal/creativescene/model.go:lightFields` | 修改 | 按类型加入新字段 |
| `backend/internal/creativescene/model.go:(*Light).fields` | 修改 | 新字段的 set 和 clear |
| `backend/internal/creativescene/model.go:completeLight` | 修改 | 补缺省值 |
| `backend/internal/creativescene/model.go:mergeLight` | 修改 | keep 新字段 |
| `backend/internal/creativescene/model.go:validateLight` | 修改 | 范围与有限值 |
| `backend/internal/creativescene/model.go:decodeScene` | 修改 | 对每盏灯 `completeLight` |
| `backend/internal/creativescene/view.go:completedScene` | 修改 | 灯光缺字段时返回补齐后的场景 |
| `backend/internal/creativescene/fingerprint.go:Fingerprint` | 修改 | `lightModelVersion` 与 `light_model` 字段 |
| `backend/internal/creativescene/scene_test.go` | 修改 | 字段列表、缺省值、换类型、错类型 400 |
| `backend/internal/creativescene/fingerprint_internal_test.go` | 修改 | 固定指纹更新；新增「加入光源尺寸后提示一次」的用例 |
| `backend/internal/creativescene/light_size_test.go` | 新增 | 旧快照读取补齐（仿 `camera_dof_test.go:TestOldCameraIsCompletedOnRead`）、范围 |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | 灯光字段说明 |
| `backend/internal/creativeagent/scene_tools_test.go:TestEditSceneDescribesThreejsFields` | 修改 | 断言新字段出现 |
| `api/openapi.yaml` | 修改 | `CreativeSceneLight`、`CreativeSceneLightPatch` |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `frontend/src/creative-scene/render/lightModel/lights.ts` | 新增 | 灯光与阴影子类、`sizeOf` |
| `frontend/src/creative-scene/render/lightModel/nodes.ts` | 新增 | 三个光源节点、`installLightModel` |
| `frontend/src/creative-scene/render/lightModel/direct.ts` | 新增 | `PhysicalLightingModel.direct` 的包装 |
| `frontend/src/creative-scene/render/lightModel/shadowFilter.ts` | 新增 | PCSS、原始深度、`attachSoftShadow` |
| `frontend/src/creative-scene/render/lightModel/formulas.ts` | 新增 | 衰减、`hybrid`、Snyder、光源表面采样 |
| `frontend/src/creative-scene/render/gpu.ts` | 修改 | 建渲染器后 `installLightModel` |
| `frontend/src/creative-scene/objects/lights.ts:buildLight` | 修改 | 用灯光子类 |
| `frontend/src/creative-scene/objects/lights.ts:applyLight` | 修改 | 写 `size`，挂软阴影 |
| `frontend/src/creative-scene/objects/SceneLights.tsx` | 修改 | 重建键加「尺寸 > 0」 |
| `frontend/src/creative-scene/objects/Lighting.tsx` | 修改 | `Sun` 与物理天空的太阳和月亮改用 `SceneSunLight` |
| `frontend/src/creative-scene/render/sunShadow.ts:SunCascades.setup` | 修改 | `ownNames` 后缀按序号 |
| `frontend/src/creative-scene/render/sunShadow.ts:fitCascades` | 修改 | 每级同步 `lightSize`；带尺寸时 `normalBias = 0` |
| `frontend/src/creative-scene/render/volumes/shadow.ts:VolumeShadowNode` | 修改 | 尺寸为 0 的法线偏置（覆写 `setupShadowPosition`） |
| `frontend/src/creative-scene/render/volumes/shadow.ts:VolumePointShadowNode` | 修改 | 同上 |
| `frontend/src/creative-scene/render/volumes/lights.ts` | 修改 | 有效距离；带尺寸时手动 2×2 双线性比较 |
| `frontend/src/creative-scene/render/volumes/state.ts` | 修改 | 槽位的半径；`Slot`、`Candidate`、`structure`、`signatureOf` 加「是否带尺寸」；候选排序 |
| `frontend/src/creative-scene/render/realtimeGI.ts` | 修改 | `contactOn` 看主光尺寸，带尺寸时跳过追踪 |
| `frontend/src/creative-scene/render/sceneRenderer.ts` | 修改 | `accumulate` 不限于 GI；`workbench` 接 `AccumulateNode` |
| `frontend/src/creative-scene/render/engines.ts` | 修改 | `workbench` 的帧数看有没有带尺寸的投影灯；耗时估算 |
| `frontend/src/creative-scene/host/render.ts` | 修改 | 只有 `lit` 用多帧；第 2–16 帧冻结阴影图，结束后恢复 |
| `frontend/src/creative-scene/render/trace/tables.ts` | 修改 | `LIGHT_ROW = 5`、`writeLight` |
| `frontend/src/creative-scene/render/trace/hit.ts` | 修改 | 两处直接光改用 `formulas.ts`；阴影光线在光源表面采样 |
| `frontend/src/creative-scene/state/apply.ts` | 修改 | `lightFields`、按类型的 `lightDefaults` |
| `frontend/src/creative-scene/panels/LightFields.tsx` | 修改 | 两个新字段 |
| `frontend/src/creative-scene/objects/SceneObjects.tsx:LightIcon` | 修改 | 半径圆环，抵消缩放 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | 新增的断言，见下 |
| `frontend/scripts/fixtures/scene-reference/` | 修改 | 修复条纹后的基线图和 `light-baseline` 数据 |

## 验证与交付

**e2e**：都写在 `frontend/scripts/creative-scene.e2e.mjs`，按记忆「只跑定向测试」用 `SCENE_E2E_ONLY` 过滤，用 `CREATIVE_SCENE_URL` 换端口，避开 owner 的 5173 和 5179。

| 验收 | 断言 |
|---|---|
| AC-S1-1 | `occluder-wall`：聚光、点光、平行光分别带尺寸，三块板的剖面与参考解比较。参考解用 S0 的方法（`traceWithin` 在光源表面采样）现算，帧数取 8192 |
| AC-S1-2 | 尺寸全部为 0，与修复条纹后的基线比较，每通道差 ≤ 1/255，两种渲染器都测 |
| AC-S1-3 | 照度探针：d ≥ 10r 处与 `I/d²` 相差 ≤ 2%；d < r 时为有限值；交界线 ≤ 2° |
| AC-S1-4 | 高光板，粗糙度 0.15 和 0.4，半径 0.1、0.3、1 m；平行光 0.545° 和 5°；另加一块 clearcoat 板（基础层 0.5、clearcoat 0.05），只比较 clearcoat 层 |
| AC-S1-5 | 程序化天空和物理天空的柱子影子，在最近一级级联里量半影；`sun_size_deg = 5` 时变宽 |
| AC-S1-6 | 按 GI-S1 AC-S1-2 的逐网格对照，加入带尺寸的灯 |
| AC-S1-10 | WebGPU 和 `forceWebGL` 各一条，检查带尺寸的灯所在之处着色不缺失、不全黑；级联 1–3 有阴影；`workbench` 的 `lit` 连续两次导出逐像素相同；物理天空把 `sun_size_deg` 从 0.545° 调到 5° 后，最近一级的半影随之变宽（级联的 `lightSize` 同步）；一盏聚光在 0 和非 0 之间切换、旁边有投影的体积时，体积着色不缺失、不全黑（march 重建） |

**服务端**：只跑 `make check-go PKG=./internal/creativescene/...` 和 `PKG=./internal/creativeagent/...`。

**前端**：`make check-frontend`（类型检查、lint，以及涉及 `state/apply.ts` 的单测）。

**性能（AC-S1-7）**：
- 参考设备，冷启动。按 Epic「验证与交付」，每次加载给材质输出乘一个 1 + k·2⁻²³ 的随机系数，S0 的 `?cold=1` 移进 `gpuTestHooks`。
- 4 盏灯的场景：`frame-two-lights`（已有一盏平行光、一盏点光）加一盏聚光，天空设为程序化天空，太阳投影。聚光、点光、平行光、太阳各一，作为 e2e 的固定数据。
- 测视口帧时、第一次构建和导出时长，以及 0 与非 0 切换的停顿。
- 导出时长另测 4K、8K，以及加一个投影体积的同一场景。

**观感（AC-S1-11）**：用 owner 反馈的场景出改前改后的对比图，`workbench` 和 `realtime_gi` 各一组，交 owner 看。条纹修复后的新基线图也在这一轮一起看。

**合并**：以当时的 develop 为底，在主检出 `git merge --no-ff --no-commit` 后跑 `make check-merge`（Epic「合并顺序」）。

**回滚**：只做前向修复（Epic D2）。旧后端读不懂新字段时会读取失败，同 `render.engine` 的先例。

## 未决事项

| 问题 | 影响 | 解决方式 | 最晚解决 |
|---|---|---|---|
| 条纹修复用哪种偏置 | AC-S1-2 的基线图 | 实施时出两种做法的图，交 owner 看 | 提交基线图之前 |
