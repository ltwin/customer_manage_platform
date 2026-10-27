# Change 详细设计

## 继承的设计

- **依据**：Epic `scene-light-size` 批准基线 v5（v4 加上面光源缩放按世界缩放）。本子项绑定全部 design 条目，交付 `data: 面光源`。以下都以 Epic design 为准，本文只写落到代码上的做法和 Epic 没写到的局部决定：
  - 面光源的光源模型和不变量 I1、I4、I6；
  - LTC 选型：矩形沿用 three，椭圆用 Heitz & Hill 2017；
  - 中心的立方体阴影和矩形核（U3），半影上限与采样档（U5）；
  - 体积的钳位；
  - 编辑器、缺省值（D3）和指纹（D4：不加版本号）。
- **S0 结论**（子项 `scene-light-size-spike` 的 `discovery/research/`）：
  - `area-shadow.md`：立方体对参考解、矩形核、U5 的档位；
  - `thresholds.md`：AC-5 的阈值；椭圆不能借矩形的 LTC（亮度差 1.32 倍）；
  - `defaults-look.md`：缺省 0.25 m 方形、10000 cd/m² 的观感；
  - 原型 `prototype/spike/light/area.ts`（给 `RectAreaLight` 挂 `PointLightShadow` 加 `pointShadow` 节点）和 `soft.ts` 的 `pointPCSS(rect)` 的 `rectAxes`（矩形核）。S2 只搬矩形核，接收面仍用产品里 `cubePCSS` 的几何法线。
- **S1 的实际做法**（与 Epic 架构图的差别）：
  - 直接光不是 `directSphere` 这类独立入口。`render/lightModel/direct.ts` 的 `installDirect` 包住 `PhysicalLightingModel.prototype.direct`，按光源节点交来的 `sized` 分层着色。S2 照这个做法包住 `directRectArea`。
  - 灯光表格式已定为每行 5 个 vec4（`render/trace/tables.ts`），S2 只填面光源的槽位。
- **经验**：
  - `tsl-loop-fn-hoisted-in-branch`：每份阴影新建带 `Loop` 的 `Fn`；
  - `three-rebuild-materials-by-lights`：材质只在灯光组合的缓存键变化时重建；
  - `webgpu-perf-warm-shader-cache`：冷启动测法；
  - `three-info-memory-leak-trace`：新增的阴影和 LTC 资源，拆灯时要确认计数回落。

## 领域模型与业务规则

不新增身份或聚合。面光源是 `kind = light` 的对象，字段在 `object:<id>:content` 单元内，与其他灯光一样由场景命令写入（`data: 面光源`）。

| 规则 | 落点 |
|---|---|
| 只有正面发光，沿局部 −Z | LTC 本身单面（three `LTC_Evaluate` 的 `lightNormal·(P−p0) ≥ 0`）；椭圆照同一判断；命中点、体积都按单面 |
| 实际尺寸 = `width`、`height` 乘世界缩放（含父级） | 渲染端从灯的世界矩阵列向量取；服务端按父级链算世界矩阵（见「缩放」） |
| 等效发光强度 = 亮度 × 实际面积。面积按两条世界半轴向量算：矩形 `4·|hw × hh|`，椭圆 `π·|hw × hh|`。父级非均匀缩放加子级旋转时发光面是平行四边形，这样算才和 LTC 的积分一致 | 属性面板的显示、换类型的换算、体积和命中点的强度 |
| 换类型时按等效发光强度换算（Epic I6） | 服务端 `mergeLight`；编辑器显式写入同样的结果 |
| 强度上限按类型 | 面光源 0…100000 cd/m²，其他类型 0…1000 |
| 面光源总有尺寸 | 宽高下限 0.01 m：投影时总挂 PCSS，`workbench` 出图总做时域累积 |

## 模块职责与协作

主流程（新建一盏面光源到出图）：

```
Shift+A「面光源」 editing.ts newLight('area')
  → add_objects（服务端 completeLight 补齐 shape/width/height、强度 10000）
  → SceneLights.tsx SceneLight：lightKey = 'area+rectangle' | 'area+ellipse'
      buildLight('area') → SceneAreaLight（无参构造，自带 SizedAreaShadow，near 0.05 m）
      applyLight：颜色、亮度、宽高、形状；投影时 attachSoftShadow(light, final)，按类型选矩形或椭圆核
  → SceneAreaLight.updateMatrixWorld()：从世界矩阵列向量算两条世界半轴向量，写到灯和阴影上
  → three LightsNode 按构造函数找到 AreaLightNode（installLightModel 注册）
      update()：把灯上的世界半轴向量转到视空间，交给 three 的 halfWidth、halfHeight
      setupShadowNode()：pointShadow（体积时由 volumeShadowNode 换成 VolumePointShadowNode）
      setupDirectRectArea()：交出 three 的参数 + ellipse 标记
  → PhysicalLightingModel.directRectArea（installAreaDirect 包装）
      矩形：three 原样 + sheen；椭圆：ltcEllipse 替代 LTC_Evaluate，clearcoat、sheen 同样
  → 阴影：cubePCSS 的矩形核，采样档按 final 选
  → 命中点：collect.ts 收集 → tables.ts 写入 kind 3 → hit.ts 的 lightAt、directAt 里单独的面光源分支（不走 tableLight）
  → 体积：state.ts 收集为 'area' 槽位 → lights.ts 按单面余弦光、钳位
```

| 模块 | 职责 | 不负责 |
|---|---|---|
| `backend/internal/creativescene` | 校验、补齐、换类型换算 | 着色 |
| `render/lightModel/lights.ts` | `SceneAreaLight`（含世界半轴的计算）、`SizedAreaShadow` 的 `copy()`、`hasSize` | 尺寸来源之外的场景状态 |
| `render/lightModel/nodes.ts` | `AreaLightNode` 注册，把灯上的半轴转到视空间，LTC 表初始化一次 | 光照公式、半轴的计算 |
| `render/lightModel/ltc.ts`（新） | 椭圆 LTC 的形状因子、抄自 three 的 `LTC_ClippedSphereFormFactor`、面光源的照度 π·L·FF；光栅和命中点共用（I4） | 纹理加载 |
| `render/lightModel/direct.ts` | 包住 `directRectArea`：椭圆分支、sheen | 光源节点 |
| `render/lightModel/shadowFilter.ts` | 立方体 PCSS 的矩形核、出图档 | 阴影图本身 |
| `render/trace/` | 面光源行、命中点着色、阴影光线 | 光栅 |
| 编辑器 | 菜单、面板、图标、按类型的缺省值 | 换算以外的校验（服务端负责） |

依赖方向不变：`render/lightModel/` 不依赖编辑器外壳；`render/trace/` 读取它的公式。

## 关键机制

### 灯光子类与光源节点

- **`SceneAreaLight extends RectAreaLight`**（`render/lightModel/lights.ts`）：
  - 无参构造。three 的 `RectAreaLight` 没有阴影（`RectAreaLight.js:10`），构造时给它一个 `SizedAreaShadow`，`castShadow` 由 `applyLight` 设；
  - 字段 `shape: 'rectangle' | 'ellipse'`；`width`、`height` 沿用 three 的字段，存文档的值，不乘缩放；
  - **世界半轴**：覆写 `updateMatrixWorld()`，先调父类，再从 `matrixWorld` 的前两列乘 `width/2`、`height/2` 得到世界空间的半宽、半高向量 `halfX`、`halfY`，同时写到阴影上（单位轴 `axisX`、`axisY` 和长度 `half`）。
    - 渲染器每帧先更新矩阵再渲染，所以着色器读到的总是这一帧的值；
    - standIn 的副本各自更新矩阵，自己算，不依赖光源节点的 `update` 和 `renderGroup` uniform 的先后次序。
  - `copy()` 带上 `shape` 和阴影（standIn 的 `clone(false)` 要编出同一个着色器）。
- **`SizedAreaShadow`**：`sized(PointLightShadow)` 再派生一层。
  - 构造函数里初始化 `axisX`、`axisY`、`half`，并把相机的 near 设为 0.05 m。three 的缺省是 0.5 m（`PointLightShadow.js:16`），柔光箱前不到 0.5 m 的遮光板、格栅会不投影；S0 测阈值时用的就是 0.05（`area.ts:49`）。体积读的是同一个相机的 near（`volumes/state.ts:325`），自动一致；
  - `copy()` 带上 `axisX`、`axisY`、`half`。
- **`sizeOf` 与 `hasSize`**：
  - `sizeOf` 有几处直接参与计算（`trace/tables.ts:219-228`，`volumes/state.ts:265/281/295`，`realtimeGI.ts:275`），面光源不收进去，它仍只表示球半径和太阳角径；
  - 新增 `hasSize(light)`：面光源恒为真，其他灯为 `sizeOf > 0`。`lightKey`、`softShadowsIn` 这类只问「有没有尺寸」的地方用它。
  - 文档层的 `state/apply.ts:lightSize` 同理，对面光源不返回数值，调用方按类型判断。
- **`AreaLightNode extends RectAreaLightNode`**（`render/lightModel/nodes.ts`），经 `library.addLight(AreaLightNode, SceneAreaLight)` 注册。three 按构造函数精确匹配，资产预览等处用的普通 `RectAreaLight` 不受影响。
  - `update(frame)`：不调用父类（父类用 `extractRotation`，丢掉了缩放），把灯上的 `halfX`、`halfY` 乘视矩阵的旋转部分，写进 three 的 `halfWidth`、`halfHeight`。
  - `setupShadowNode()`：返回 `pointShadow(light)`；`shadow.shadowNode` 已被体积换成 `VolumePointShadowNode` 时，three 优先用它（`AnalyticLightNode.setupShadow`）。
  - `setupDirectRectArea(builder)`：父类的返回值加 `ellipse: true | false`（按 `light.shape`，构建时定，所以形状进 `lightKey`，切换形状时重建一次，代价与新增一盏灯相同）。
- **LTC 表**：`RectAreaLightNode.setLTC(RectAreaLightTexturesLib.init())` 在 `installLightModel` 里调用一次。

### 缩放

- **渲染端**：尺寸取世界矩阵列向量，包含物件和所有父级的缩放，也就是视口里网格物体所用的同一个矩阵。图标画在物件的 group 里，自然一致（AC-S2-1 的「轮廓和发光面一致」）。没有父级、或父级不缩放时，就是 `width × |scale.x|`。
- **服务端**：`lookat.go` 已有按父级链计算的 `worldMatrix`（`lens.go` 也在用），换类型时同样按世界缩放算面积，面板、助手和服务端的结果一致。
- **缩放恒为正**：服务端只接受 0.001…1000 的缩放（`model.go:796`），不存在负缩放，不为它另写规则。
- **平行四边形**：父级非均匀缩放加子级旋转时，两条半轴不再垂直，发光面是平行四边形。LTC 按四个角点积分，结果就是这个平行四边形；椭圆同理是它的内切椭圆。面积按 `|hw × hh|` 算（见「领域模型」），与积分一致。

### 直接光：矩形与椭圆（`installAreaDirect`）

在 `direct.ts` 里照 `installDirect` 的方式，包住 `PhysicalLightingModel.prototype.directRectArea` 一次：

```
directRectArea(input):
  先另存（aside）：
    if !input.ellipse: three 原样（漫反射、高光、clearcoat）
    else:             同一套代码，LTC_Evaluate(p0..p3) 换成 ltcEllipse(N, V, P, mInv, centre, halfW, halfH)
  FF、avgDir = 单位矩阵下的向量形状因子（矩形或椭圆）
  补上 three 的 direct 有、directRectArea 没有的几项（N2）：
    F = F_Schlick(specularColor, specularF90, dot(V, normalize(avgDir + V)))
    directDiffuse  += aside.diffuse × (1 − F) [× sheenEnergyComp(avgDir)，有 sheen 时]
    directSpecular += aside.specular × multiScatteringCompensation
  sheen（两种形状都补）：
    sheenSpecularDirect += π × lightColor × FF × BRDF_Sheen(avgDir)
```

- **椭圆分支**本质上是把 three 的 `directRectArea` 函数体复制一份，只换形状因子。`direct.ts` 和 `ltc.ts` 都加进 `render/lightModel/nodes.ts` 的 three 升级核对清单。
- **椭圆的形状因子**（`render/lightModel/ltc.ts`，文件头注明 Heitz & Hill 2017「Real-Time Line- and Disk-Light Shading with Linearly Transformed Cosines」）：
  1. 中心和两条半轴向量变换到 LTC 空间：C、V1、V2 各乘 `mInv`（相对着色点 P，并转到 three 的 LTC 坐标系，与 `LTC_Evaluate` 相同，包括它取负的 T2 轴）；
  2. 背面判断：`dot(cross(V1, V2), C) < 0` 时为 0（单面）；
  3. 求变换后椭圆的主轴：由 `d11 = V1·V1`、`d22 = V2·V2`、`d12 = V1·V2` 求两个特征值和主轴方向，得到椭圆在其平面内的标准形；
  4. 由椭圆和着色点构成的椭圆锥，写出它的 3×3 矩阵，解三次方程（Blinn 的稳健解法）求特征值，得到等效球冠的两个半轴参数 L1、L2 和平均方向 `avgDir`，形状因子 `FF = L1·L2 / √((1+L1²)(1+L2²))`；
  5. 向量形状因子 `FF × avgDir` 做地平线裁剪，与矩形的最后一步相同。three/tsl 没有导出 `LTC_ClippedSphereFormFactor`（`LTC.js:175` 只导出了 `LTC_Evaluate`、`LTC_Evaluate_Volume`、`LTC_Uv`），在 `ltc.ts` 里抄一份。
  - 漫反射用单位矩阵的 `mInv`，高光和 clearcoat 用各自 LTC 表查出的 `mInv`，Fresnel 用同一张 `ltc_2`。
  - **单测**（`scripts/creative-scene-gi.test.ts` 同类的 node 测试，纯数学部分在 JS 里有一份对照实现）：
    - 正对：圆盘中心在法线上，与解析解 `E = π·L·R²/(R²+d²)` 对照；
    - 不对称的视角：椭圆偏离法线、绕法线转一个角度，与数值积分对照，用来发现 T2 轴取负有没有照搬对。
- **sheen**：three 的 `directRectArea` 没有 sheen（Epic design「直接光的入口」表）。
  - three 的面光源漫反射写的是 `L × albedo × FF`，没有 Lambert 的 1/π，所以照度是 `E = π·L·FF`。sheen 照 three 点光的写法（照度 × `BRDF_Sheen`），要乘 π，否则暗约 3.14 倍。
  - 方向用单位矩阵下向量形状因子的平均方向，不用中心方向。大光源贴近表面时，中心方向可能已经在地平线下面。
  - 不做定量验收，与 S1 的 sheen 处理一致。
- **能量的 4% 偏差（N2，已查清）**：不是归一化问题，是 three r186 的 `directRectArea` 比它自己的 `direct` 少了几项：
  - 漫反射没有乘 `1 − F`（`PhysicalLightingModel.js:673` 对点光有，`:714` 对面光源没有）。F0 = 0.04 时正好约 4%，与尺寸无关；
  - 高光没有乘 `multiScatteringCompensation`，有 sheen 时漫反射也没有乘 sheen 的能量系数。
  - 参考解（`light-check/reference.ts`）和命中点（`hit.ts:316`）都按 `direct` 的写法乘了 `1 − F`。
  - 三方对照（任务 1.1）：光栅与解析解的漫反射一致，参考解是解析解的 0.959 倍；墙面上光栅 / 参考解 1×1 m 为 1.040，2×2 m 为 1.043。临时补上 `1 − F` 后为 1.0016 和 1.0044。
  - 做法：上面伪代码里的「补上」一段。F 的方向用向量形状因子的平均方向，远处退化为点光的写法，AC-S2-2 的 2% 保持不变。

### 软阴影：矩形核与采样档

- **按类型显式选核**：`cubePCSS({ kernel, search, filter, maxTexels })`，`attachSoftShadow` 按灯的类型和形状传入 `kernel: 'disc' | 'rect' | 'ellipse'`，不在构建时看阴影上有没有某个字段（`axisX` 由 `updateMatrixWorld` 写入，但它在构造函数里就已初始化，见「灯光子类」）。点光仍是 `'disc'`，不变。
- **滤波核**：现在的圆盘核 `toward(v, k)` 用 `tangent`、`bitangent` 和半径 `r`。面光源把单位轴 `axisX`、`axisY` 投影到垂直于 `bd3D` 的平面上，分别乘 `half.x`、`half.y`，再乘系数 c，作为两条核轴：
  - 矩形 c = 2/√3：让 Vogel 圆盘采样的二阶矩等于均匀矩形；
  - 椭圆 c = 1：均匀椭圆本身就是 Vogel 圆盘按两条半轴拉伸。乘 2/√3 会让椭圆的半影宽 15%。
- **搜索核**：矩形的搜索阶段用外接椭圆，c = √2，盖住矩形的四个角，只有从角上看得到的遮挡物也不会漏。滤波阶段仍用 2/√3。椭圆的搜索和滤波都用 1。
- **上限换算**：`radius` 取实际最长的核轴长度，即 `c × max(half.x, half.y)`，不是 `max(half)`。否则矩形的半影最宽会到上限的 1.15 倍，出图档约 148 texel。
- **采样档**（U5）：

  | 档 | 搜索 + 滤波 | 上限 | 用在 |
  |---|---|---|---|
  | 视口 | 16 + 32 | 48 texel | 视口的灯 |
  | 出图 | 32 + 64 | 128 texel | 导出、light-check（与 S0 测阈值时相同） |

  - 视口和导出各自建灯（导出的 `ShotScene` 是另一棵树，`SceneLight` 的 `warm` 为 false）。`attachSoftShadow(light, final)` 按 `final = !warm` 在构建时选档，滤波函数各是各的。点光、聚光、平行光仍只有一档，不变。
  - `SEARCH`、`FILTER`、`MAX_TEXELS` 从模块常量改为 `cubePCSS` 的参数，缺省值不变。
- **standIn 与体积**：`volumeShadowNode` 和 `shadowLike` 现在按 `isPointLight` 选立方体节点，改为「点光或面光源」。

### 命中点（AC-S2-3）

- **收集**：`collect.ts` 的 `collectLights` 加 `isRectAreaLight`（`SceneAreaLight`）。
- **灯光表**：`LIGHT.area = 3`，填 S1 定下的槽位：

  | vec4 | area |
  |---|---|
  | 0 | (3, casts, shape：0 矩形 / 1 椭圆, 0) |
  | 1 | (中心, 0) |
  | 2 | (颜色 × 亮度, 0) |
  | 3 | (世界半宽向量, 0) |
  | 4 | (世界半高向量, 0) |

  法线取 `normalize(cross(半宽, 半高))` 的反向（−Z）。这与 S1 设计表里预留的写法（第 1、2 行末位放半宽、半高标量）不同，改为放两条带长度的向量：命中点要的是轴向量，格式本身（每行 5 个 vec4）不变。
- **不走 `tableLight`**：`tableLight` 返回「方向 + 颜色」，LTC 给出的是积分后的结果，这个接口表达不了。而且它会把面光源当成别的灯：
  - 第 240 行 `kind.x > spot − 0.5` 当成聚光；
  - 第 258 行 `sized.and(kind.x > point − 0.5)` 当成球光；
  - 第 222 行从第 4 个 vec4 的 `.x` 读尺寸，面光源这里是半高向量的 x 分量，`sized`、`sinHalf` 会是随机值。
- **`lightAt` 和 `directAt` 里单独分支**：
  - 先判断 `kind.x > spot + 0.5`，面光源走自己的分支，其余灯照旧调用 `tableLight`；
  - 漫反射：照度 `E = π·L·FF`，FF 用单位矩阵的 LTC（矩形用 three 的 `LTC_Evaluate`，椭圆用 `ltc.ts`），与光栅同一份公式（I4）；与点光的写法（`hit.ts:316`）和光栅一样乘 `1 − F`，F 的方向取平均方向 `avgDir`；
  - 高光：同光栅，用 LTC 表查 `mInv`，LTC 表与光栅共用；
  - `directAt` 的蓄水池：同一个照度 E 既当权重，又当 `picked`。估计量 `picked × total / luminance(picked)` 因此无偏，也不需要另一套近似公式；
  - 阴影光线：在发光面上均匀取点（矩形按两条半轴均匀，椭圆按面积均匀的同心圆映射），`reach` 停在取样点前 1 mm；随机数沿用现有的 PCG，种子取帧序号和像素。

### 体积

- `render/volumes/state.ts`：
  - `SlotKind` 加 `'area'`；第 260 行的收集过滤加 `isRectAreaLight`；
  - 第 171 行 `structure` 的 `['directional', 'spot', 'point'].indexOf` 加上 `'area'`，否则返回 −1，排序和结构签名会乱；
  - 第 154 行的估计用 `Math.pow(…, c.decay)`，`RectAreaLight` 没有 `decay`，会得到 NaN。面光源显式用 2，强度用等效发光强度（亮度 × 面积），距离钳在 `max(d², A/π)`；
  - 面光源不用 `sizeOf`，半径一项填 0，钳位单独传面积。
- `render/volumes/lights.ts`：
  - 光强 = 等效发光强度 × `max(dot(−Z, 指向样本点的方向), 0)`（单面余弦）；
  - 衰减 `1 / max(d², A/π)`；
  - 阴影读立方体图，与点光同一条路径（`maps` 按 `kind === 'point' || kind === 'area'` 选立方体）。

### 编辑器

- **新建菜单**：`SceneEditor.tsx` 的「灯光」一节加 `area`，`newLight` 用视图朝向（同聚光），缺省 0.25 m 方形，强度取 `NEW_LIGHT_INTENSITY.area = 10000`。
- **属性面板**（`panels/LightFields.tsx`）：
  - 面光源：强度标签「亮度（cd/m²）」，上限 100000，旁边显示「≈ N cd」（亮度 × 按世界半轴算的面积）；
  - 「形状」下拉（矩形 / 椭圆），「宽（m）」「高（m）」；
  - 半径、距离、衰减只对点光和聚光显示；
  - 切换类型时，一次写入 `{ type, intensity }`，强度按 I6 和世界缩放换算并截断，结果与服务端相同（Epic 修订 v5）。
- **图标**（`objects/SceneObjects.tsx` 的 `LightIcon`）：面光源画矩形或椭圆的线框，在物件局部 XY 平面上、随物件缩放，加一条沿 −Z、长 0.2 m 的朝向短线。`RadiusRing` 不画给面光源。拾取仍靠图标中心的小球。
- **缺省值**：`state/apply.ts` 的 `lightDefaults` 拆成按类型的表，面光源强度 10000，其余仍为 1；`lightFields` 加 `area` 一行；本地 `mergeLight` 照服务端加换算。`lightSize` 不收面光源（见「`sizeOf` 与 `hasSize`」）：`lightKey` 对面光源取 `area+<shape>`，`render/engines.ts` 的 `softShadowsIn` 对投影的面光源恒为真，`RadiusRing` 不画给面光源。

### 服务端

- `model.go`：
  - `Light`、`lightPatch` 加 `Shape`、`Width`、`Height`；`lightFields` 加 `area`，`lightNames` 加「面光源」；
  - `completeLight`：强度缺省按类型（面光源 10000）；面光源补 `shape`、`width`、`height`；
  - `validateLight`：强度范围按类型；`shape ∈ {rectangle, ellipse}`；宽高 0.01…20；
  - `mergeLight(cur, p, before, after)`：类型改变且本次没写强度时按 I6 换算再截断。`before`、`after` 是物件在这次更新前后的世界缩放（只用 X、Y 两轴列向量），由调用方按父级链算好：
    - 原面积（面光源 → 点光或聚光）用 `before` 和原宽高、形状；
    - 新面积（点光或聚光 → 面光源）用 `after` 和本次写入或补齐后的宽高、形状；
    - 同一次更新里同时改了缩放和类型时，原面积仍按改之前的缩放算。现在 `ops.go:323` 先写 transform 再合并灯光，所以 `updateObjectsOp.apply` 要在写 `o.Transform` 之前先算 `before`。
  - 世界缩放复用 `lookat.go` 的父级链做法：抽一个只按 `s.Objects` 计算世界矩阵的函数，`lookAtScene.worldMatrix` 和换算共用。面积按两条世界半轴的叉积算，与渲染端一致。
- `fingerprint.go`：不改。面光源随灯光对象进 `lit` 指纹，光源模型版本仍为 `lights-v2`（D4）。
- `api/openapi.yaml`：`CreativeSceneLight`、`CreativeSceneLightPatch` 的 `type` 加 `area`，加 `shape`、`width`、`height`，强度上限改为 100000 并写明按类型；Patch 的说明加换算规则。`make generate` 后提交 Go 和 TS 两份生成物。
- `creativeagent/scene_tools.go`：灯光字段一句加面光源的字段和单位；「打光」指引加一句柔光箱的用法（例如 0.6 × 0.9 m、亮度量级）。

## 局部设计与偏差

- **sized 入口沿用 S1 的包装做法**：Epic 架构图写的是新增 `directEllipse` 入口，S1 已经改为包住 three 的 `direct`。S2 照此包住 `directRectArea`，椭圆是它的一个分支。效果相同，少一套与 three 并行的入口。
- **灯光表的面光源槽位**：改放两条带长度的轴向量（见「命中点」），格式不变。
- **采样档在构建时选**：不用 uniform 控制循环次数。视口和导出本来就是两份灯，各编各的滤波函数，没有额外的编译。
- **sheen 的近似**沿用 Epic 的写法，不做定量验收；按评审补上 π，方向改用向量形状因子的平均方向。
- **命中点的面光源单独分支**：不经过 `tableLight`，同一个照度既当蓄水池权重又当 `picked`（见「命中点」）。
- **补提交参考场景**：Epic roadmap 写 S0 提交了面光源棚拍场景，实际是运行时合成的（S0 `spike/light/main.tsx`），仓库里没有。S2 把它提交为 `scripts/fixtures/scene-reference/area-studio`：`occluder-wall` 的布局，灯换成面光源，参数对齐 S0。

## 接口与数据契约

- **契约状态**：扩展。交付 Epic 的 `data: 面光源`（绑定引用，不复制正文）；场景命令外形不变；OpenAPI 只加枚举值和字段。
- 绑定的是 Epic 修订 v5 的 `data: 面光源`：尺寸按世界缩放（含父级），服务端换类型的换算同样按世界缩放。
- 本子项的 `contracts.md` 不写局部增量。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativescene/model.go:Light` | 修改 | 加 `Shape`、`Width`、`Height`，同步 `lightPatch` |
| `backend/internal/creativescene/model.go:lightFields` | 修改 | 加 `area`；`lightNames` 加「面光源」 |
| `backend/internal/creativescene/model.go:completeLight` | 修改 | 按类型的强度缺省；面光源字段补齐 |
| `backend/internal/creativescene/model.go:validateLight` | 修改 | 按类型的强度范围；形状与宽高校验 |
| `backend/internal/creativescene/model.go:mergeLight` | 修改 | 加缩放参数与 I6 换算 |
| `backend/internal/creativescene/ops.go:updateObjectsOp.apply` | 修改 | 写 transform 之前先算更新前的世界缩放，合并灯光时传入前后两份 |
| `backend/internal/creativescene/lookat.go:worldMatrix` | 修改 | 抽出只按 `s.Objects` 计算世界矩阵的函数，与换算共用 |
| `backend/internal/creativescene/light_size_test.go` | 修改 | 面光源的补齐、范围、换算用例（或新文件 `area_light_test.go`） |
| `backend/internal/creativescene/scene_test.go` | 修改 | 换类型与 400 字段的用例 |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | 灯光字段与打光指引 |
| `backend/internal/creativeagent/scene_tools_test.go` | 修改 | 说明里必须出现的面光源字段 |
| `api/openapi.yaml` | 修改 | `CreativeSceneLight`、`CreativeSceneLightPatch` |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `frontend/src/creative-scene/state/apply.ts` | 修改 | `lightFields`、按类型的 `lightDefaults`、`lightSize`、`mergeLight` 换算 |
| `frontend/src/creative-scene/render/lightModel/lights.ts` | 修改 | `SceneAreaLight`（`updateMatrixWorld` 算世界半轴）、`SizedAreaShadow`（near 0.05）、`hasSize`；`sizeOf` 不收面光源 |
| `frontend/src/creative-scene/render/lightModel/nodes.ts` | 修改 | `AreaLightNode`、注册、LTC 初始化；升级核对清单加本文件的绕法 |
| `frontend/src/creative-scene/render/lightModel/ltc.ts` | 新增 | 椭圆 LTC 形状因子、`LTC_ClippedSphereFormFactor` 的副本、面光源照度 |
| `frontend/src/creative-scene/render/lightModel/direct.ts` | 修改 | `installAreaDirect`：椭圆分支与 sheen |
| `frontend/src/creative-scene/render/lightModel/shadowFilter.ts` | 修改 | `cubePCSS({ kernel, search, filter, maxTexels })`：矩形、椭圆核与搜索核，上限按实际核轴换算；`attachSoftShadow(light, final)` 按类型选核 |
| `frontend/src/creative-scene/objects/lights.ts` | 修改 | `buildLight('area')`、`applyLight`、`lightKey` 含形状 |
| `frontend/src/creative-scene/objects/SceneLights.tsx` | 修改 | 把 `final = !warm` 交给 `applyLight` |
| `frontend/src/creative-scene/render/volumes/shadow.ts` | 修改 | `volumeShadowNode`、`shadowLike` 对面光源用立方体节点 |
| `frontend/src/creative-scene/render/volumes/state.ts` | 修改 | `'area'` 槽位与收集（第 260 行），`structure`（第 171 行），估计里显式用 2 和等效发光强度（第 154 行） |
| `frontend/src/creative-scene/render/volumes/lights.ts` | 修改 | 单面余弦光、钳位、立方体阴影 |
| `frontend/src/creative-scene/render/trace/collect.ts:collectLights` | 修改 | 收集面光源 |
| `frontend/src/creative-scene/render/trace/tables.ts` | 修改 | `LIGHT.area`、`writeLight` 的面光源行 |
| `frontend/src/creative-scene/render/trace/hit.ts` | 修改 | `lightAt`、`directAt` 里面光源的单独分支（LTC 照度、阴影光线），`tableLight` 不处理面光源 |
| `frontend/src/creative-scene/render/litLook.ts:NEW_LIGHT_INTENSITY` | 修改 | 加 `area: 10000` |
| `frontend/src/creative-scene/render/engines.ts:softShadowsIn` | 修改 | 投影的面光源恒为有尺寸 |
| `frontend/src/creative-scene/editing.ts` | 修改 | `lightLabels`、`newLight` |
| `frontend/src/creative-scene/SceneEditor.tsx` | 修改 | 新建菜单加面光源 |
| `frontend/src/creative-scene/panels/LightFields.tsx` | 修改 | 形状、宽高、亮度与等效 cd、切换类型写强度 |
| `frontend/src/creative-scene/objects/SceneObjects.tsx:LightIcon` | 修改 | 矩形或椭圆轮廓、朝向短线；`RadiusRing` 排除面光源 |
| `frontend/scripts/fixtures/scene-reference/area-studio` | 新增 | 棚拍参考场景 |
| `frontend/scripts/light-check/` | 修改 | 加载 `area-studio`、形状从灯读取、`texelsAt` 的面光源分支、不给面光源写 `radius_m` |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | AC-S2-1…6 的检查；`sizeless()` 不给面光源写 `radius_m` |
| `frontend/scripts/creative-scene.test.ts` | 修改 | 本地 `mergeLight` 的换算用例 |
| `frontend/scripts/creative-scene-trace.test.ts` | 修改 | 灯光表面光源行的用例 |
| `frontend/scripts/creative-scene-ltc.test.ts` | 新增 | 椭圆形状因子：正对与解析解对照，不对称视角与数值积分对照 |

## 验证与交付

| 验收 | 怎么验 |
|---|---|
| AC-S2-1 | light-check 在 `area-studio` 上对参考解：1×1 m 矩形、1×1 m 椭圆、2×0.5 m 和 2×2 m 矩形，各测能量、全图阴影误差和板剖面；另一个 e2e 缩放物件，比较轮廓与发光面的投影范围 |
| AC-S2-2 | light-check 读法线方向上 10 倍边长处的照度，与 `L·A/d²` 比；近处读几处，照度不超过 π·L |
| AC-S2-3 | 照 AC-S1-6 的做法，`SCENE_E2E_TRACE` 下逐网格对照 |
| AC-S2-4 | `SCENE_E2E_LIGHT_PERF` 加面光源的场景，冷启动，与 S1 的尺寸为 0 对照场景比；结果写进 verify |
| AC-S2-5 | 后端单测（补齐、范围、400 字段、换算与截断、显式强度优先、父级带缩放、同一次改缩放和类型）；前端单测（本地 `mergeLight` 与服务端结果相同）；e2e：菜单新建、面板改字段与撤销、切换类型 |
| AC-S2-6 | 两个后端各一条着色 e2e；投影体积 e2e；`workbench` 连续两次逐像素相同；重跑 AC-S1-2 基线和 AC-S1-10；后端 `TestLightModelStalesLitOnce` 不变 |
| AC-S2-7 | 出图交 owner |

- 按「只跑定向测试」：e2e 用 `SCENE_E2E_ONLY` 过滤，worktree 的 Vite 换端口，避开 owner 的 5173、5179。
- 拆灯时资源要回落：加面光源再删除，视口 `renderTargets` 回到原数（S2-6 顺带检查，见经验 `three-info-memory-leak-trace`）。
- 回滚：只做前向修复（Epic「回滚」），含面光源的快照在旧版本上读取失败。

## 未决事项

| # | 问题 | 影响 | 解决方式 | 最晚 |
|---|---|---|---|---|
| N2（已解决，任务 1.1） | S0 测得矩形面光源整体比参考解亮约 4% | AC-S2-1、AC-S2-2 | 光栅一边的问题：three 的 `directRectArea` 漫反射少乘 `1 − F`，见「直接光」。参考解不改，S1 不受影响 | — |

N1（缩放取哪一层）由 owner 2026-10-27 定为世界缩放，经 Epic 修订 v5 写进契约；服务端同样按世界缩放换算。
