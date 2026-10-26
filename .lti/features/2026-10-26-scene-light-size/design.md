# Epic 技术方案

> **子项编号**：本文的 S0、S1、S2 指本 Epic 的子项。`scene-world-space-gi` 的子项写作 GI-S1、GI-S2，其中 GI-S1 交付了光追底座。

## 设计上下文

- **需求**：见 proposal.md。现状与 Blender 的做法见 `discovery/research/light-size-current-and-blender.md`。
- **采用方案**：
  - 光源模型照 Blender：点光和聚光是球，平行光是有角径的圆盘，面光源是矩形或椭圆。
  - 强度沿用 three.js 的物理单位。
  - 着色：
    - 扩展 `PhysicalLightingModel`，为有尺寸的光源加专门的直接光入口：球和圆盘用代表点，面光源用 LTC；
    - 阴影仍用阴影贴图，滤波按光源尺寸和遮挡物距离算半影；
    - 光追命中点用同一套光源模型，阴影光线在光源表面上采样。
- **待验证假设**：S0 出图、owner 看图后再定（U1–U3、U5–U7），本文只定各方案共用的接法和不变量。待定的有：
  - 软阴影用 PCSS、阴影图追踪还是可预滤波的变体；
  - 样本数和降噪；
  - 两个后端能否读取原始阴影深度。
- **后续增强**：
  - Blender 功率单位的导入导出换算；
  - 面光源的 `spread`、IES 配光；
  - 体积里的软阴影；
  - 在视口里拖拽点光的半径。
- **相关 Epic**：
  - `scene-world-space-gi`：GI-S1 的光追底座，其中 `render/trace/` 正在被 GI-S2 修改。
  - `scene-world-environment`：物理天空的太阳和月亮、体积的阴影节点。

## 领域模型与业务规则

- **光源尺寸**：属于灯光内容，和颜色、强度一样归 `object:<id>:content` 单元，由场景命令写入。不新增身份或聚合。

| 光源 | 形状 | 尺寸来源 | 对应 Blender |
|---|---|---|---|
| `point`、`spot` | 球 | `radius_m`，不受物件缩放影响（U8） | Point / Spot 的 Radius。聚光的另外两个参数：Blender 的 Spot Size 是整个光锥的角度，对应 `angle × 2`（`angle` 沿用 three 的半角）；Blend 对应 `penumbra`，只管从光锥边缘到中心的羽化 |
| `directional` | 无限远处的圆盘 | `angular_diameter_deg` | Sun 的 Angle |
| 程序化天空的太阳 | 同上 | 常数 0.545° | — |
| 物理天空的太阳 | 同上 | `sky.sun_size_deg`，与画出的太阳盘一致 | Nishita 天空的 Sun Size |
| 物理天空的月亮 | 同上 | 常数 0.52° | — |
| `area` | 矩形或椭圆，单面发光 | `width × |scale.x|`、`height × |scale.y|`、`shape`：随物件缩放，与 Blender 一致 | Area 的 Size、Size Y、Shape |

- **不变量**：
  - **I1 强度与尺寸无关**：
    - 点光和聚光的 `intensity` 是发光强度（cd）。和 Blender 一样，改半径时总功率不变，会出现四种变化（owner 2026-10-26 确认）：
      1. 远处的照度不变：球面完全露在受光面地平线以上时，照度精确等于 `I/d²`；
      2. 光源表面和高光的单位面积亮度按 r² 下降，高光变大变暗，总能量不变；
      3. 受光范围变大：法线背向中心、但还能看到部分球面的表面也会受光，明暗交界线随半径外移，形成包裹；
      4. 靠近光源处照度封顶，不像点光源那样发散。
    - 面光源的 `intensity` 是亮度（cd/m²），法线方向上的等效发光强度是亮度 × 发光面积，改面积会改变总光量（决定 D3）。
  - **I2 距离衰减**：把有效距离 `d_eff = max(d, r)` 代入现有的 `getDistanceAttenuation`，`decay` 和钳位照旧，即 `1 / max(d_eff^decay, 0.01)`。`distance` 的截断窗口仍按真实距离 d 计算。
    - 缺省 `decay = 2`、`distance = 0` 时，结果是 `I / max(d², r², 0.01)`：法线朝光源、球面完全在地平线以上时，这就是精确解，在光源以外按平方反比衰减，在光源以内照度有限。
    - `decay ≠ 2` 或 `distance > 0` 是非物理设置，照样代入同一个式子，不保证精确，AC-3 也不覆盖。
  - **I3 尺寸为 0 时退回点光源**：r = 0 时 I2 退化成现状的 `max(d^decay, 0.01)`。代表点和归一化系数在 r = 0 时恒等于点光源。阴影滤波按 uniform 分支，尺寸为 0 时走原来的 PCF（见「软阴影的接入」）。
  - **I4 一份光源模型**：光栅、光追命中点和将来的路径追踪，用同一套光源形状、强度和衰减的定义。近似方法可以不同，但都以同一个解析真值为准。
  - **I5 聚光的 `penumbra` 只管光锥边缘**，与 Blender 的 Blend 一样。
  - **I6 换类型保持等效亮度**：在点光、聚光和面光源之间换类型时，按等效发光强度换算强度，再截断到目标类型的上限。与平行光之间换类型时，数值原样保留，再截断。由服务端在合并时完成，规则见 `data: 面光源`。

## 架构设计

只描述增量，场景模块的分层沿用 `architecture: 场景模块`。

- **服务端** `backend/internal/creativescene`：
  - 灯光字段表加入新字段和 `area` 类型；
  - 负责校验、缺省值补齐和换类型时的强度换算（I6）；
  - `lit` 指纹计入光源模型版本。
  不参与着色。
- **助手** `backend/internal/creativeagent`：更新场景工具说明里的灯光字段。外部助手（MCP）也用这份说明。
- **前端渲染端** `frontend/src/creative-scene/`：

```
objects/lights.ts                    文档字段 → 场景灯光子类
   │                                   SceneSphereLight / SceneSpotLight / SceneSunLight / SceneAreaLight
   │                                   构造函数无参数，覆写 copy() 复制尺寸
   ▼
render/lightModel/  （新）
   ├─ lightingModel.ts                 扩展 PhysicalLightingModel（先例：render/giSplit.ts 包装 indirect）：
   │                                     新增 directSphere / directDisk / directEllipse
   │                                     矩形沿用 three 的 directRectArea
   ├─ 光源节点                          经 renderer.library.addLight 按类精确注册；
   │                                     setupDirect 交给上面的新入口，传形状参数，不再只传一个 lightDirection
   ├─ shadowFilter.ts                  两套滤波：2D（聚光、级联、面光源）和立方体（点光，先把 dp 线性化）
   │                                     尺寸为 0 时按 uniform 分支走原来的 PCF
   └─ 公式                              尺寸、衰减和代表点的公式，光栅节点和 render/trace/hit.ts 共用（I4）
   ▼
render/sunShadow.ts（SunCascades）     初始化后给每一级级联的 shadow 设 filterNode，按该级的 texel 尺寸换算
render/volumes/shadow.ts              VolumeShadowNode / VolumePointShadowNode 的 super.setup 通过 filterNode 用上软阴影
render/sceneRenderer.ts（standIn）     shadowLike 扩展到新的子类和 filterNode，替身编出同一个着色器
render/trace/tables.ts, hit.ts        灯光行带上形状和尺寸；命中点按同一套公式着色，阴影光线在光源表面采样
render/volumes/                       有尺寸的光源在体积里按中心点计算，衰减按 I2；面光源另有钳位（见「体积」）
render/realtimeGI.ts                  接触阴影按光源尺寸处理（U4）
objects/Lighting.tsx                  程序化天空和物理天空的太阳、月亮改用 SceneSunLight
panels/LightFields.tsx、objects/SceneObjects.tsx（LightIcon）、新建菜单、state/apply.ts（按类型分开的缺省值表）
```

- **直接光的入口**：
  - three 的光源节点只能交出一个 `lightDirection`，漫反射、GGX、clearcoat 和 sheen 共用它（`PhysicalLightingModel.direct`）。代表点要求高光和漫反射用不同的方向，椭圆 LTC 也没有现成入口。所以要在光照模型上加新入口，光源节点只负责把形状参数交给它。
  - 包装方式和 `installGISplit` 相同：在 `PhysicalLightingModel.prototype` 上装一次，没有传入形状参数时不改变任何东西。
  - 各分量的处理：

| 分量 | 球 / 圆盘 | 矩形 / 椭圆 |
|---|---|---|
| 漫反射 | 中心方向，衰减按 I2 | LTC 漫反射项 |
| GGX 高光 | 代表点方向，乘 `(α/α')²` | LTC 高光项 |
| clearcoat | 用 clearcoat 粗糙度另算一个代表点 | three 的 `directRectArea` 已有 clearcoat；椭圆照它的写法 |
| sheen | 中心方向（近似） | 中心方向，按 LTC 漫反射项的形状因子加权（近似）；three 的 `directRectArea` 没有 sheen，在新入口里补上 |

  sheen 的近似误差由 S0 对照参考解测量（U6）。
- **注册方式**：
  - 场景灯光用自己的子类。three 按构造函数精确匹配光源节点，所以白模的工作室光、模型缩略图和资产预览的灯光都不受影响。
  - 节点在渲染器创建入口里注册一次。
  - standIn 用 `clone(false)` 复制灯光，这会丢掉 `filterNode`，替身编出的着色器就和实际不同。所以要扩展 `shadowLike`，并要求子类构造函数无参数、覆写 `copy()`，防止落回「下一帧重编」。
- **依赖方向**：
  - `render/lightModel/` 不依赖编辑器外壳；
  - `render/trace/` 读取它的公式，不反向依赖。
- **失败与降级**：
  - 软阴影要读原始阴影深度，而阴影深度纹理设了 `compareFunction`。在 WebGL2 上，同一张纹理在一个着色器里既做比较采样又读原始值，结果没有定义；WebGPU 上 sampler 的匹配也还要实测。
  - 能不能在两个后端上原地读取，由 S0 实测（U7）。不行时，阴影 pass 额外输出一张线性深度供搜索用。在 S0 结论出来之前，不能说两个后端都支持。
  - 只有某个后端做不到时，按「只支持 WebGPU 的统一规则」经 Epic 修订处理，不悄悄降级。

## 技术选型

| 问题 | 采用 | 依据与边界 |
|---|---|---|
| 字段与单位 | three.js 物理单位：cd，面光源 cd/m²；尺寸字段按命名原则加单位后缀，three.js 有同名字段的用原名（`width`、`height`） | `data: 场景文档`「命名原则」 |
| 球和圆盘的高光 | Karis 2013 的代表点，加 `(α/α')²` 能量归一化，在光照模型的新入口里实现 | 业界通用；r = 0 时严格退回点光源（I3） |
| 球形光源的漫反射与衰减 | 照度按 I2。球面部分落到地平线以下时，用 Snyder 1996 的地平线裁剪，或代表点，由 S0 定（U6） | 满足 I2、I3 |
| 面光源 | 矩形：three r186 的 `RectAreaLightNode` 的 LTC 表和 `directRectArea`（Heitz 2016），尺寸改为从变换矩阵的列向量取；椭圆：LTC 圆盘（Heitz & Hill 2017），在文件头注明论文 | three 有矩形的实现；椭圆自写 |
| 软阴影 | 阴影贴图加按尺寸算半影的滤波。候选（U1）：PCSS；仿 EEVEE 的阴影图追踪；可预滤波的变体：基于 VSM 或 MSM 的 PCSS，或在阴影图的 mip 上做 PCSS | 估算：r = 0.1、d_b = 1、d_r = 3 时 w = 0.4 m。3 m 处立方体贴图一个 texel 约 6 mm，滤波核直径约 65 texel。只靠少量逐点样本，没有时域累积时必然出颗粒，所以必须比较可预滤波的做法。three 已支持 VSM |
| 阴影偏置 | 接收面深度偏置（receiver-plane depth bias），随滤波核大小变化 | 固定的 1.5 texel `normalBias` 在大核和斜受光面上会出条纹或漏光 |
| 噪声与收敛 | 核的旋转随渲染器创建入口提供的帧序号变化，光栅和命中点都这样做 | three 的 PCF 用 IGN，只取决于屏幕坐标，每帧同一个像素的旋转相同，TRAA 平均不掉。`workbench` 没有时域累积，单独处理（U2） |
| 参考解 | 用 GI-S1 的光追底座在光源表面采样，多帧累积 | S0 和各子项的 e2e 都拿它当真值 |

**不采用**：
- 只调大 `shadow.radius`：模糊均匀，不随距离变化，不满足 AC-1。
- Blender 式 Jitter，即每帧抖动光源、重画阴影：视口的瓶颈在 draw call（`scene-performance`），代价太高。只作为 S0 的导出对照。

## 关键机制

### 半影宽度

Δ 是接收点到遮挡物的距离。

- **球形光源**：w = 2r · (d_r − d_b) / d_b，按光源相机的投影换算成 texel。立方体贴图的 `dp` 是逐面的透视深度，要先线性化再代入。
- **平行光**：w = 2Δ · tan(θ/2)。每级级联按自己的世界尺寸 / texel 换算，所以跨过级联边界时半影宽度连续。
- **面光源**：只在阴影相机的轴和面光源的局部轴对齐时，宽和高才能分别代入，得到椭圆的滤波范围。不对齐的情况（例如立方体贴图的各个面），以及单视点阴影贴图表示不了的「只有边缘看得到的遮挡」，都由 S0 测量误差后定方案（U3）。
- **上限**：半影宽度有上限，由 U1 定，超出的截断，截断误差计入 AC-1。

### 软阴影的接入

- **聚光**、**面光源**（如果用透视贴图）：设在 `light.shadow.filterNode` 上，用 2D 的函数签名 `{ depthTexture, shadowCoord, shadow, depthLayer }`。
- **点光**：`PointShadowNode` 调用滤波函数时传的是 `{ depthTexture, bd3D, dp, shadow }`，所以要单独写一套立方体滤波。
- **级联**：`CSMShadowNode` 用 `light.shadow.clone()` 给每一级建 shadow，而 `LightShadow.copy` 不复制 `filterNode`。所以设在 `light.shadow` 上不会生效，要在 `SunCascades` 初始化之后给每一级单独设。
- **尺寸为 0**：
  - three 只保留第一次构建的阴影节点（`objects/lights.ts`）。如果在 JS 构建时按尺寸选滤波函数，半径在 0 和非 0 之间改动时不会重编着色器。
  - 所以两条路径都编进着色器，按 uniform 分支选择。分支条件只取 uniform，在 WGSL 里是一致控制流。
  - 注意项目已有的教训：比较采样不能放在按像素分叉的分支里，否则导数会变成 NaN（`render/volumes/lights.ts` 的注释，owner 2026-10-26）。
- **偏置**：接收面深度偏置随核的大小变化，取代固定的 normalBias。
- **体积阴影**：`VolumeShadowNode` 等的 `super.setup` 会用到同一个 `filterNode`，体积的透射率照旧乘在软阴影结果上。

### 光栅与命中点一致

- 公式只写一份，放在 `render/lightModel/`。
- **灯光表**（`render/trace/tables.ts`）每行现在是 4 个 vec4，要扩到能放下形状、尺寸，以及面光源的两条边向量（至少 5 个 vec4）。S1 一次把格式定好，S2 不再改格式。
- **命中点**：直接光用和光栅一样的近似；阴影光线在光源表面上随机取点，随机数取帧序号，靠实时 GI 的时域累积收敛。

### 天空的太阳和月亮

- **程序化天空**的太阳（`objects/Lighting.tsx` 的 `Sun`）和**物理天空**的太阳、月亮，都改用 `SceneSunLight`，级联照旧由 `SunCascades` 排布。
- 角径：
  - 物理天空的太阳取 `sky.sun_size_deg`，所以太阳盘调大时，阴影也一起变软，两者一致；
  - 月亮取常数 0.52°；
  - 程序化天空没有尺寸字段，取常数 0.545°，与物理天空 `sun_size_deg` 的缺省值相同。
- 平行光的缺省值也取 0.545°。这样全项目里「太阳的视直径」只有一个数。

### 体积

- 有尺寸的光源在体积散射和体积对表面的投影里，按中心点计算，衰减按 I2 钳在 `max(d², r²)`。
- 面光源按中心处、朝 −Z、单面余弦分布的光计算，强度按亮度 × 面积，衰减钳在 `max(d², A/π)`，避免大面光源在中心附近的雾里出现热点。

### 编辑器

- **属性面板**：
  - 点光和聚光：「半径（m）」；
  - 平行光：「角径（°）」；
  - 面光源：「形状 / 宽 / 高」，强度旁边显示等效发光强度（cd）；
  - 切换类型时，面板显式写入按 I6 换算后的强度。
- **灯光图标**：
  - 点光和聚光画半径为 `radius_m` 的球轮廓。图标所在的 group 带缩放，所以这个球要抵消缩放；
  - 平行光照旧画一条光束；
  - 面光源画矩形或椭圆轮廓，和物件缩放一致，加一条沿 −Z 的朝向短线。
- **新建菜单**加「面光源」，缺省 0.25 m 方形。
- **缺省值**：`state/apply.ts` 的 `lightDefaults` 现在是跨类型的一张表，要改成按类型分开（面光源的强度缺省与其他类型不同，见 D3）。

## 数据与存储设计

字段见 contracts.md 的 `data: 光源尺寸`、`data: 面光源`。存储外形不变，`schema_version` 仍为 1。

- **D1 字段名**：
  - `radius_m`：three.js 没有对应字段；不叫 `radius`，避免和 `shadow.radius`（单位是 texel）混淆。
  - `angular_diameter_deg`：聚光的 `angle` 已经表示光锥半角。
  - `width`、`height`：与 three.js `RectAreaLight` 同名。
  - `shape`：`rectangle` / `ellipse`。
- **D2 缺省值与存量**：
  - 缺省值：
    - `radius_m` 0.1；
    - `angular_diameter_deg` 0.545（与 `sky.sun_size_deg` 的缺省值一致）；
    - 面光源 0.25 m 方形。
  - 不迁移：读取时补齐。已有场景的灯会变软，这正是要修的问题；需要硬阴影的，把尺寸写成 0。
- **D3 面光源的强度**：
  - 用 three.js 的亮度（cd/m²），上限 100000。
  - 缺省 10000 cd/m²，约为常见 LED 灯板的亮度量级，0.25 m 方形时等效约 625 cd。这里偏离了「缺省取 three.js 默认值」：three 的缺省 1 cd/m² 在缺省尺寸下只有 0.0625 cd，助手省略强度时会得到一盏看不见的灯。
  - 不采用 Blender 的「改尺寸总功率不变」，原因是命名原则。
  - 已知限制：点光上限 1000 cd，而面光源 20 × 20 m 时等效可达 4×10⁷ cd，两者不对称。等以后修订灯光的取值范围时一并处理。
- **D4 渲染指纹**：
  - 什么时候计入：`lit` 的场景里有灯光对象，或者天空的太阳会投影（程序化天空、物理天空）时，指纹计入光源模型版本号。所以这些场景已有的 `lit` 导出各提示一次「场景已更新」。HDRI 或纯色天空、又没有灯光对象的场景不提示。
  - 谁负责加一：版本号是 `backend/internal/creativescene/fingerprint.go` 里的常量，反映的是前端的渲染语义。哪个子项改变了已有文档的灯光画面，就由它在同一次提交里加一，并写进该子项的验收清单。本 Epic 只在 S1 加一次；S2 只新增类型，不改已有画面，不加。
- **回滚**：回滚到不认识新字段或 `area` 的后端，读取会失败，只做前向修复（同 `render.engine` 的先例）。

## 验证与交付

- **e2e** 用 S0 提交的参考场景，按记忆「只跑定向测试」用 ONLY 过滤，换端口避开 owner 的 Vite：
  - AC-1、AC-4、AC-5：与光追参考解比较；
  - AC-3：读取照度；
  - AC-2：尺寸为 0 时与基线图比较，每通道差异不超过 1/255；
  - AC-7：按 GI-S1 的 AC-S1-2 逐网格对照。
- **性能**（AC-8）在参考设备上测。
- **观感**（AC-11 和 S2 的棚拍场景）由 owner 看图判断。
- **合并顺序**：S1、S2 和 GI-S2 都要改 `render/trace/hit.ts`、`tables.ts`。各自合回 `develop` 前以当时的 `develop` 为底，按 `make check-merge` 处理。

## 未决事项

| # | 问题 | 影响 | 解决方式 | 最晚解决 |
|---|---|---|---|---|
| U1 | 软阴影的做法（PCSS、阴影图追踪，或基于 VSM / MSM / mip 的预滤波 PCSS）、样本数、半影上限 | S1、S2 | S0 用参考解出对比图，owner 看图后选定，经 Epic 修订回填 | S1 实施前 |
| U2 | `workbench` 没有时域累积：用预滤波压住颗粒，还是给 `workbench` 的最终渲染加时域累积 | S1 | S0 测两种做法的颗粒和开销 | S1 实施前 |
| U3 | 面光源阴影：立方体贴图还是正面半球的透视贴图；单视点的误差；阴影相机的轴与面光源的轴不对齐时，滤波范围怎么算 | S2 | S0 原型和参考解对照 | S2 实施前 |
| U4 | 实时 GI 的屏幕空间接触阴影是硬的：按半影宽度缩短追踪长度，还是超过阈值就关掉 | S1 | S0 对照 | S1 实施前 |
| U5 | 尺寸上限，暂定 `radius_m` ≤ 10、`angular_diameter_deg` ≤ 30、`width`/`height` 0.01…20。超出半影上限后画面能否接受，以及面光源单视点阴影误差随尺寸的增长 | S1、S2 的契约 | S0 测量，必要时经 Epic 修订调整 | S1 实施前 |
| U6 | 球形光源部分落到地平线以下时的漫反射近似（Snyder 还是代表点）。选出的做法必须做出包裹，即 I1 的第 3 种变化，不能只是简单截断；sheen 近似的误差 | S1 | S0 与参考解和 Blender Cycles 对照 | S1 实施前 |
| U7 | 设了 `compareFunction` 的阴影深度纹理，能否在 WebGL2 和 WebGPU 上同时做比较采样和原始读取；不行时，阴影 pass 额外输出线性深度 | S1、S2 | S0 在两个后端实测 | S1 实施前 |
| U8 | 点光和聚光的半径是否随物件缩放：Blender 的实际行为待核对；本文暂定不随缩放 | S1 | S0 的 Blender Cycles 对照时核对 | S1 实施前 |
