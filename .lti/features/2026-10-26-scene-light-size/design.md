# Epic 技术方案

> **子项编号**：本文的 S0、S1、S2 指本 Epic 的子项。`scene-world-space-gi` 的子项写作 GI-S1、GI-S2，其中 GI-S1 交付了光追底座。

## 设计上下文

- **需求**：见 proposal.md。现状与 Blender 的做法见 `discovery/research/light-size-current-and-blender.md`。
- **采用方案**：
  - 光源模型照 Blender：点光和聚光是球，平行光是有角径的圆盘，面光源是矩形或椭圆。
  - 强度沿用 three.js 的物理单位。
  - 着色：
    - 扩展 `PhysicalLightingModel`，为有尺寸的光源加专门的直接光入口：球和圆盘用代表点，面光源用 LTC；
    - 阴影仍用阴影贴图，滤波用 PCSS，按光源尺寸和遮挡物距离算半影；
    - 光追命中点用同一套光源模型，阴影光线在光源表面上采样。
  - S0 的对比图和测量见子项 `scene-light-size-spike` 的 `discovery/research/`，owner 2026-10-26 按其建议选定（修订 v2）。
- **待验证假设**：
  - U8：点光和聚光的半径是否随物件缩放，等 Blender 对照；
  - 高光混合模型的两个参数（1.5、0.6）只在 0.15 粗糙度上拟合过，S1 在另一档粗糙度上复核 AC-4；
  - 性能只在两盏灯的场景上测过，S1 在 4 盏灯的场景上复测 AC-8。
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
  - **I3 尺寸为 0 时退回点光源**：r = 0 时 I2 退化成现状的 `max(d^decay, 0.01)`。高光和漫反射的近似在 r = 0 时恒等于点光源。尺寸为 0 的灯不挂软阴影滤波，阴影走原来的 PCF（见「软阴影的接入」）。
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
   ├─ shadowFilter.ts                  PCSS 两套滤波：2D（聚光、级联）和立方体（点光、面光源）
   │                                     参数一律从所过滤的 shadow 读取；尺寸为 0 的灯不挂
   └─ 公式                              尺寸、衰减和代表点的公式，光栅节点和 render/trace/hit.ts 共用（I4）
   ▼
render/sunShadow.ts（SunCascades）     每一级 clone() 时经阴影子类的 copy() 带上 filterNode，每级读自己的相机；
                                      ownNames 的后缀改为按灯的序号，让 standIn 编出同样的代码
render/volumes/shadow.ts              VolumeShadowNode / VolumePointShadowNode 的 super.setup 通过 filterNode 用上软阴影
render/sceneRenderer.ts（standIn）     shadowLike 扩展到新的子类和 filterNode，替身编出同一个着色器
render/trace/tables.ts, hit.ts        灯光行带上形状和尺寸；命中点按同一套公式着色，阴影光线在光源表面采样
render/volumes/                       有尺寸的光源在体积里按中心点计算，衰减按 I2；面光源另有钳位（见「体积」）
render/realtimeGI.ts                  主光有尺寸时关掉屏幕空间接触阴影（U4）
render/sceneRenderer.ts（workbench）   最终导出加时域累积，逐帧旋转核（U2）
objects/Lighting.tsx                  程序化天空和物理天空的太阳、月亮改用 SceneSunLight
panels/LightFields.tsx、objects/SceneObjects.tsx（LightIcon）、新建菜单、state/apply.ts（按类型分开的缺省值表）
```

- **直接光的入口**：
  - three 的光源节点只能交出一个 `lightDirection`，漫反射、GGX、clearcoat 和 sheen 共用它（`PhysicalLightingModel.direct`）。代表点要求高光和漫反射用不同的方向，椭圆 LTC 也没有现成入口。所以要在光照模型上加新入口，光源节点只负责把形状参数交给它。
  - 包装方式和 `installGISplit` 相同：在 `PhysicalLightingModel.prototype` 上装一次，没有传入形状参数时不改变任何东西。
  - 各分量的处理：

| 分量 | 球 / 圆盘 | 矩形 / 椭圆 |
|---|---|---|
| 漫反射 | 中心方向，照度用 Snyder 1996 的精确解：球面完全在地平线以上时即 I2，部分落到地平线以下时做出包裹（U6） | LTC 漫反射项 |
| GGX 高光 | 混合模型：光源角半径 s = r/d 中，不超过 1.5α 的部分按平方和加宽粗糙度，超出的部分交给代表点平移，平移部分按 Karis 的 `(α/α')²` 归一化 | LTC 高光项 |
| clearcoat | 与 GGX 高光同一套，用 clearcoat 自己的粗糙度 | three 的 `directRectArea` 已有 clearcoat；椭圆照它的写法 |
| sheen | 随漫反射走（Snyder 照度），不做定量验收 | 中心方向，按 LTC 漫反射项的形状因子加权（近似）；three 的 `directRectArea` 没有 sheen，在新入口里补上 |

  S0 的测量：混合模型在半径 0.1–1 m 时能量 0.92–0.99、面积 0.97–1.15、峰值低 3%–20%；单用 Karis 代表点时，小半径的高光面积大约 2 倍。Snyder 的明暗交界线与参考解相差 ≤ 1°，代表点包裹过头 7–10°。
- **注册方式**：
  - 场景灯光用自己的子类。three 按构造函数精确匹配光源节点，所以白模的工作室光、模型缩略图和资产预览的灯光都不受影响。
  - 节点在渲染器创建入口里注册一次。
  - three 的 `LightShadow.copy` 不复制 `filterNode`。standIn 用 `clone(false)` 复制灯光、级联用 `clone()` 复制阴影，都会丢掉它。所以阴影也用子类，覆写 `copy()` 带上 `filterNode` 和光源尺寸；灯光子类构造函数无参数、覆写 `copy()`，并扩展 `shadowLike`。
  - standIn 和真实着色器的编号本来就对不上（`creative-scene.e2e.mjs`「W5 no recompile」），这在 PCF 下已经存在。S0 实测带上滤波函数后，聚光和点光那一帧的冷编译反而更快；平行光要把 `ownNames` 的后缀改为按序号才能对上。
- **依赖方向**：
  - `render/lightModel/` 不依赖编辑器外壳；
  - `render/trace/` 读取它的公式，不反向依赖。
- **失败与降级**：
  - **原始深度（U7）**：两个后端统一一套写法。带尺寸的灯，阴影深度纹理去掉 `compareFunction`、改为最近点过滤，搜索和滤波都读原始值、手动比较；不需要额外输出线性深度。WebGPU 和 WebGL2 各要绕开一个 three r186 的缺陷，各有一个 e2e 检查带尺寸的灯所在之处着色正确，而不是缺失或全黑（S0 `depth-read-backends.md`）。
  - 尺寸为 0 的灯保留比较模式和硬件 PCF，着色器与现状完全相同。

## 技术选型

| 问题 | 采用 | 依据与边界 |
|---|---|---|
| 字段与单位 | three.js 物理单位：cd，面光源 cd/m²；尺寸字段按命名原则加单位后缀，three.js 有同名字段的用原名（`width`、`height`） | `data: 场景文档`「命名原则」 |
| 球和圆盘的高光 | 混合模型：小角半径部分按平方和加宽粗糙度，超出 1.5α 的部分用 Karis 2013 的代表点平移并归一化能量（见「直接光的入口」） | S0 对照参考解：纯代表点在小半径时高光面积大约 2 倍；纯加宽在 1 m 时能量只剩 0.66。r = 0 时严格退回点光源（I3） |
| 球形光源的漫反射与衰减 | 照度按 I2；球面部分落到地平线以下时用 Snyder 1996 的精确照度（U6） | 交界线与参考解相差 ≤ 1°；代表点包裹过头 7–10° |
| 面光源 | 矩形：three r186 的 `RectAreaLightNode` 的 LTC 表和 `directRectArea`（Heitz 2016），尺寸改为从变换矩阵的列向量取；椭圆：LTC 圆盘（Heitz & Hill 2017），在文件头注明论文 | three 有矩形的实现；椭圆自写。S0 测得矩形比参考解整体亮约 4%，不随尺寸变化，像是 three 的 LTC 拟合带来的固定偏差。椭圆不能借用矩形的 LTC（S0 中两者亮度差 1.32 倍，约等于面积比 4/π 再乘 1.04） |
| 软阴影（U1） | PCSS：先搜索遮挡物，再按半影宽度做可变半径的滤波。点光、聚光、平行光：16 次搜索 + 32 次滤波，半影上限 48 texel；面光源见「半影宽度」。采样循环用 TSL 的 `Loop`（见「软阴影的接入」） | S0 在四种光源上与参考解比：宽半影偏差 −19% 到 +11%，剖面均值 ≤ 0.016。阴影图追踪（`smtrace`）多层遮挡更准，但单帧颗粒和黑点明显、耗时是 PCSS 的 2.5 倍；可预滤波的变体也不做 |
| 阴影偏置 | 接收面深度偏置（receiver-plane depth bias），随滤波核大小变化；上限按坡度表达，≤ 20 倍（约 87°） | 固定的 1.5 texel `normalBias` 在大核和斜受光面上会出条纹或漏光 |
| 噪声与收敛 | 核的旋转随帧序号变化，光栅和命中点都这样做。`workbench` 的最终导出加时域累积（例如 16 帧，逐帧旋转核），不做预滤波（U2） | three 的 PCF 用 IGN，只取决于屏幕坐标，TRAA 平均不掉。S0：多帧平均让 3 m 板剖面最大差从 0.193 降到 0.080；`workbench` 每帧约 2 ms，16 帧只多几十毫秒 |
| 参考解 | 用 GI-S1 的光追底座在光源表面采样，多帧累积 | S0 和各子项的 e2e 都拿它当真值 |

**不采用**：
- 只调大 `shadow.radius`：模糊均匀，不随距离变化，不满足 AC-1。
- Blender 式 Jitter，即每帧抖动光源、重画阴影：视口的瓶颈在 draw call（`scene-performance`），代价太高。只作为 S0 的导出对照。
- 阴影图追踪（`smtrace`）、基于 VSM 或 MSM 的 PCSS、在 mip 上做 PCSS：S0 比较后不采用（U1）。

## 关键机制

### 半影宽度

Δ 是接收点到遮挡物的距离。

- **球形光源**：w = 2r · (d_r − d_b) / d_b，按光源相机的投影换算成 texel。立方体贴图的 `dp` 是逐面的透视深度，要先线性化再代入。
- **平行光**：w = 2Δ · tan(θ/2)。每级级联按自己的世界尺寸 / texel 换算，所以跨过级联边界时半影宽度连续。
- **面光源（U3）**：用中心处的立方体阴影图，接法与点光相同。滤波核与面光源的两条轴对齐：把光源的 x、y 轴投影到垂直于「灯到接收点」方向的平面上，分别乘以半宽和半高，再乘以 2/√3，使圆盘采样的二阶矩与均匀矩形相同。S0 中立方体与参考解几乎一致，没有看出单视点误差；前半球 150° 透视图的半影只有一半，不采用。细长光源（2 × 0.5 m）上，矩形核让 0.5 m 板的误差降到圆盘核的五分之一。
- **上限（U5）**：超出的截断，半影偏窄但画面连续，截断误差计入 AC-1。
  - 点光、聚光、平行光：48 texel。
  - 面光源：出图（`quality: 'final'`）128 texel、32 次搜索 + 64 次滤波；视口 48 texel、16 + 32 次。上限放宽时采样要一起加密，否则小半影处误差变大；放到 256 反而变差。
  - S0 中受上限影响最大的是平行光 10° 和 2 m 面光源，宽半影最多偏窄 19%。

### 软阴影的接入

滤波函数只从它所过滤的 `shadow` 里读参数：相机的 near、far、fov 和边界，点光的位置，光源尺寸，都用 `reference('camera.near', 'float', shadow)` 这种带点路径按帧读取。这样同一个函数可以给任意一份阴影用，四个接入点不需要逐处接线。

- **聚光**：设在 `light.shadow.filterNode` 上，2D 签名 `{ depthTexture, shadowCoord, shadow, depthLayer }`。
- **点光**、**面光源**：`PointShadowNode` 传的是 `{ depthTexture, bd3D, dp, shadow }`，单独写一套立方体滤波；接收点位置用 `positionWorld − camera.position` 还原。
- **级联**：`CSMShadowNode` 第一次构建时用 `light.shadow.clone()` 给每一级建阴影，滤波函数随阴影子类的 `copy()` 带过去，每级读自己的相机，跨级联边界连续。
- **standIn**：同样随 `copy()` 带过去（见「注册方式」）。
- **采样循环**：用 TSL 的 `Loop`，不逐次展开。展开时每个材质有约 250 段采样代码，导出变慢约 2–3 倍；用 `Loop` 后冷启动导出 `workbench` +30%、`realtime_gi` +14%。
  - 坑：three 的 `VarNode` 遇到在分支里调用、内含 `Loop` 的无 layout `Fn` 时，会把结果变量提到着色器开头，而这个标记是按 `Fn` 记的。四级级联共用一个 `Fn` 时，第 1–3 级被提到 `shadowPositionWorld` 赋值之前，整级全亮。所以每份阴影新建一个带循环的 `Fn`，并加一个 e2e 检查级联 1–3 仍有阴影（S0 `soft-shadow-candidates.md`「反应舱」）。
- **尺寸为 0**：不挂滤波函数，阴影图保留比较模式，着色器与现状完全相同（AC-2）。
  - 不用 uniform 分支：带尺寸的灯关掉了比较模式（「失败与降级」），同一个着色器里没有硬件 PCF，手动比较模仿不到 1/255 以内。
  - 代价：尺寸在 0 和非 0 之间切换时要换滤波函数。three 只保留第一次构建的阴影节点（`objects/lights.ts`），S1 要让这次切换触发阴影节点重建，材质重编一次，代价与新增一盏灯相同；S1 在视口里实测这次停顿。
  - 聚光和点光现有的自阴影条纹（`bias` 为 0）在 S1 一起修，AC-2 的基线随之改为修复后的画面。
- **偏置**：接收面深度偏置随核的大小变化，取代固定的 normalBias，上限见「技术选型」。
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
  - AC-2：尺寸为 0 时与修复条纹后的基线图比较，每通道差异不超过 1/255；
  - AC-7：按 GI-S1 的 AC-S1-2 逐网格对照；
  - 防回退：WebGPU 和 WebGL2 上带尺寸的灯着色正确（U7 的两个 three 缺陷）；平行光级联 1–3 仍有阴影（`Loop` 的坑）。
- **性能**（AC-8）在参考设备上测。导出耗时要冷启动测量：系统会跨进程缓存编译过的着色器，换新的浏览器配置也不能保证冷启动，S0 的做法是每次加载给材质输出乘一个随机的 1 + k·2⁻²³（S0 `soft-shadow-candidates.md`「反应舱」）。
- **观感**（AC-11 和 S2 的棚拍场景）由 owner 看图判断。
- **合并顺序**：S1、S2 和 GI-S2 都要改 `render/trace/hit.ts`、`tables.ts`。各自合回 `develop` 前以当时的 `develop` 为底，按 `make check-merge` 处理。

## 未决事项

U1–U7 已由 S0 出图、owner 2026-10-26 选定（修订 v2），结论写进上文各节，依据见 S0 的 `discovery/research/thresholds.md`：

| # | 问题 | 结论 | 正文位置 |
|---|---|---|---|
| U1 | 软阴影的做法、样本数、半影上限 | PCSS，16 + 32 次，48 texel | 技术选型「软阴影」；关键机制「半影宽度」 |
| U2 | `workbench` 的颗粒 | 最终导出加时域累积，不做预滤波 | 技术选型「噪声与收敛」 |
| U3 | 面光源阴影 | 中心的立方体图，矩形核 | 关键机制「半影宽度」 |
| U4 | 屏幕空间接触阴影 | 主光有尺寸时关掉 | 架构设计 `realtimeGI.ts` |
| U5 | 尺寸上限与半影上限 | 字段上限维持暂定；面光源 128 texel | 关键机制「半影宽度」；`contracts.md` |
| U6 | 地平线以下的漫反射、sheen | Snyder；sheen 随漫反射，不定量验收 | 架构设计「直接光的入口」 |
| U7 | 原始深度读取 | 两个后端统一去掉比较、手动比较 | 架构设计「失败与降级」 |

仍未决：

| # | 问题 | 影响 | 解决方式 | 最晚解决 |
|---|---|---|---|---|
| U8 | 点光和聚光的半径是否随物件缩放：Blender 的实际行为待核对；本文暂定不随缩放 | S1 | owner 在另一台装有 Blender 的机器上按 S0 `blender-cycles.md` 对照。结果是「随缩放」时经 Epic 修订改为「半径乘以物件缩放」 | S1 实施前 |
