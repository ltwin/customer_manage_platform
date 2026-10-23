# Change 详细设计

## 继承的设计

- Epic `scene-world-environment` 批准版本 v9：
  - 「领域模型与业务规则」：体积物件的概念与边界，E2 单一来源，E3 导出确定，E5 三种雾共存。
  - 「架构设计」：`objects/Volume.tsx` 与体积渲染的职责，`SceneRenderer` 的合成位置。
  - 「技术选型」：体积光照用逐像素光线步进。
  - 「关键机制」：「合成顺序」（修订 v9：体积在空气透视之外，按体积深度分段）、「体积物件（W5）」全节、「渲染指纹」的清除规则。
- 契约：`data: ADD 体积物件`、`api: MODIFY 提交场景命令`，本次完整交付。
- 穿刺：`discovery/research/volume-lighting.md`，原型 `discovery/research/prototype/src/vol.ts` 是本次着色代码的出发点。原型里的区间、灯光衰减、纹理绑定和射线起点按本文改写，不照搬。
- 本次承担 roadmap W5 的全部范围：数据、渲染、编辑界面、助手说明和文档。

## 领域模型与业务规则

- **对象：**
  - 体积物件是普通对象（`kind = "volume"`），身份、层级、摆放、选择、撤销和改挂都沿用现有规则。
  - 内容单元 `object:<id>:content` 只放 `volume`。
- **介质：**
  - 形状是单位立方体（边长 1 m）或单位球（直径 1 m），由 `transform` 放进世界，非均匀缩放得到长方体和椭球。
  - 系数（每米，RGB，按线性颜色）：
    - σs = `scatter_density` × `scatter_color`；
    - σa = `absorption_density` × (1 − `absorption_color`)；
    - σt = σs + σa。
  - 密度 = 满值 × smoothstep(0, `edge_falloff`, e)。e 是归一化的离边距离：立方体为 1 − 2·max(|x|,|y|,|z|)，球为 1 − 2·|p|，都在局部坐标里取，边界为 0、中心为 1。`edge_falloff = 0` 为硬边。
  - 重叠处各体积的系数相加；相函数按各自的 `anisotropy`，按 σs 加权。
- **可见性：**
  - `visible = false` 或祖先不可见的体积不参与任何绘制。
  - 介质只在 `lit` 里绘制：最终渲染的视口、`lit` 导出、`render_view` 的 `lit`。是否在 `lit` 由 `LitLighting` 登记决定（见「登记与出图」），不由 `<Volume>` 自己判断。
  - 视口里任何显示模式都画范围线框（辅助对象，导出和 `render_view` 不画）。
- **投射阴影：**
  - `cast_shadow = true` 的体积，挡住平行光（太阳、月光、文档里的平行光）、聚光、点光射向表面的光。
  - 只在灯本身投影、表面接收阴影时生效，这是 three 的阴影节点本来就有的条件。
- **上限**：8 个，计入 2000 个对象上限。服务端在 `validateScene` 里计数，与摄像机的 32 台同处。

## 模块职责与协作

体积渲染放在 `render/volumes/` 下，按职责拆开。介质函数不依赖光栅化状态，留给将来的路径追踪器调用（Epic「为远期预留」）。

| 模块 | 职责 | 不负责 |
|---|---|---|
| `backend/internal/creativescene/model.go` | `KindVolume`、`Volume` 结构、`completeVolume`、`validateVolume`、`mergeVolume`、`volumePatch`；内容字段与类型匹配；8 个的上限 | — |
| `backend/internal/creativescene/ops.go` | `update_objects` 的 `volume` 补丁按键合并 | — |
| `backend/internal/creativescene/fingerprint.go` | `clay`、`line` 跳过体积物件 | — |
| `backend/internal/creativeagent/scene_tools.go` | 工具说明 | — |
| `render/volumes/medium.ts`（新） | 纯函数：JS 侧的系数换算（颜色 → σs、σa、σt，线性）；TSL Fn `hitVolume`（局部坐标求交）、`volumeDensity`（含边缘柔化）、`chordThrough`（解析弦长，按边缘柔化折算） | 灯光、步进、合成 |
| `render/volumes/state.ts`（新） | 体积的 uniform（逆世界矩阵、系数、形状、数量、投影列表）与 `prepareVolumes` / `finishVolumes`；灯的选择与结构签名；纹理节点每帧换图 | 着色 |
| `render/volumes/lights.ts`（新） | 体积受光：各类灯的照度 × 可见性，复用 three 的距离衰减、聚光锥衰减和 CSM 的级联选法；天空光照项 | 积分 |
| `render/volumes/march.ts`（新） | `VolumeNode`：区间合并与步数分配、积分、两次模糊；输出散射光、透射率和体积深度 | 合成 |
| `render/volumes/composite.ts`（新） | `applyVolumes(color, depth, camera)`：全分辨率覆盖掩码、按深度上采样、按 Epic「合成顺序」的分段公式与空气透视合成 | — |
| `render/volumes/shadow.ts`（新） | 表面阴影系数 `volumeShadow`；`VolumeShadowNode`、`VolumePointShadowNode`；给 `SunCascades` 用的系数 | — |
| `world/atmosphere/aerial.ts` | 新增 `aerialAt(viewPosition)`：返回到该点的累计散射 S（已乘 `relative`）与透射 T，与 `applyAerial` 共用查表代码 | — |
| `objects/Volume.tsx`（新） | 一个体积对象：范围线框（辅助层，按屏幕像素阈值拾取）；在场景的体积登记表里登记自己（对象 ID、Object3D、字段） | 着色、是否在 `lit` |
| `objects/Lighting.tsx` | `LitLighting` 在场景上登记「体积可画」（`scene.userData.volumes`），卸载时清除，与 `scene.userData.aerial` 相同 | — |
| `render/sceneRenderer.ts` | 工作台路径：在 `aerialBehind` 之后、景深之前接 `applyVolumes`；出图前后调 `prepareVolumes` / `finishVolumes`；`standIn` 克隆灯时装同类的阴影节点 | — |
| `render/realtimeGI.ts` | GI 路径：在 `aerialBehind` 之后、`aside` 之前接 `applyVolumes` | — |
| `render/sunShadow.ts:SunCascades` | `setup` 的结果乘以 `volumeShadow`；提供给替身灯装同类节点的方法 | — |
| `objects/lights.ts` | 聚光、点光在 `buildLight` 时装上 `VolumeShadowNode`、`VolumePointShadowNode`；`disposeLight` 释放它们 | — |
| `objects/SceneObjects.tsx:ObjectNode` | `kind === 'volume'` 时渲染 `<Volume>` | — |
| `editing.ts`、`SceneEditor.tsx` | `newVolume`、添加菜单两项、属性栏挂 `VolumeFields` | — |
| `panels/VolumeFields.tsx`（新） | 体积属性：形状、颜色、密度（对数滑杆，最左一档为 0）、各向异性、边缘柔化、投射阴影 | — |
| `panels/Outliner.tsx` | 体积的图标（`Cloud`） | — |
| `Viewport.tsx` | `centrePoint` 跳过体积物件，新物件不落到体积的线框上 | — |
| `state/apply.ts` | `contentKeys`、`updateKeys` 加 `volume`；`mergeVolume`；新体积补缺省值 | — |

文件路径都相对 `frontend/src/creative-scene/`（后端除外）。

主流程（视口一帧，最终渲染）：

```
SceneRenderer.render(camera, frame)
  ├─ applyExposure / prepareAerial（不变）
  ├─ prepareVolumes(scene, camera)
  │    ├─ 不可画（没有 LitLighting 登记）或没有可见体积 → on = 0，返回
  │    ├─ 按文档顺序取前 8 个可见体积，当场读 matrixWorld：写逆矩阵、系数、形状、数量、投影列表
  │    └─ 选灯（按贡献取前几盏）→ 结构签名；变了 → 标记步进材质重建
  └─ pipeline.render()
       ├─ 场景 pass（材质的阴影节点里乘 volumeShadow；three 在这里建好或释放阴影图）
       ├─ VolumeNode.updateBefore（依赖场景 pass 的深度，排在它之后）
       │    ├─ on = 0 → 直接返回
       │    ├─ 把阴影图、环境贴图换进纹理节点（缺图用 1×1 占位）
       │    └─ 步进 pass（CSS 像素的一半）→ 横向模糊 → 纵向模糊
       └─ 合成：表面 → 全局雾 → 空气透视 → applyVolumes → 景深 → …（不变）
  finishVolumes()：on = 0
```

## 关键机制

### 登记与出图

- `<Volume>` 在 `scene.userData.volumeObjects`（`Map<对象 ID, { object, volume }>`）里登记，卸载时删除。登记的是 Object3D，不存世界矩阵：拖动变换时 `prepareVolumes` 当场读 `matrixWorld`，不会过时。
- `LitLighting` 挂载时置 `scene.userData.volumes = true`，卸载时清除。白模、线框、材质预览都不挂 `LitLighting`，所以 `prepareVolumes` 见不到标记，`on = 0`，白模导出不会带雾（AC-W5-6）。
- `prepareVolumes` 与 `prepareAerial` 一样在 `pipeline.render()` 前同步调用，`finishVolumes` 在 `finally` 里调用，离屏出图和视口共用一套 uniform。
- 体积按对象在文档里的顺序排列，跨会话、跨机器结果相同。超过 8 个只可能出现在服务端拒绝之前的瞬间，取前 8 个，不报错。

### 选灯与编译

- **参与的灯**：太阳、月光、文档里的平行光、聚光、点光；`AmbientLight` 和 `HemisphereLight` 只进天空光照项。
- **上限**：最多 6 盏，阴影贴图最多 12 张（平行光每盏 4 张级联，聚光 1 张，点光 1 张立方体图）。加上深度和环境贴图共 14 张，在 WebGPU 缺省每阶段 16 张之内，`gpu.ts` 不调高上限。
- **排序**：投影的太阳或月光总在前；其余按对体积的估计照度排序：灯的强度 × 到所有体积包围球最近点的距离衰减（平行光按强度）。超出上限的灯只照亮表面。
- **结构签名**：每个灯位的类型与是否投影、有没有环境贴图。灯的位置、颜色、强度、锥角走 uniform，不进签名。
- **重建**：签名变了只重建步进材质，不重建管线（重建管线会清掉 TRAA 和降噪的历史，视口会闪）。
- **首次编译**：场景里第一次出现体积时才建步进材质。视口先异步编译，编好之前这一帧不画体积，与新灯的预热相同（`SceneLights.tsx`）；导出在出图前等它编好。
- **步进用平面网格绘制**（实施时调整）：`QuadMesh` 绘制时按顶点序号生成全屏三角形，`compileAsync` 编出的顶点着色器与之不同，预编译不命中；步进改用平面网格加正交摄像机。
- **实时 GI 的体积合成单独一个 pass**（实施时调整）：实时 GI 视口的最终合成原本把 GI 合成、雾、空气透视、体积与辅助对象叠加放在同一个全屏 pass，加上体积的两张输出后片元阶段采样 17 张纹理，超过 WebGPU 每阶段 16 张，管线创建失败，视口全黑（出图没有辅助对象 pass，15 张，不受影响）。改为体积合成之前的画面先转成一张中间纹理（`convertToTexture`），体积合成与辅助对象叠加在下一个 pass；视口多一次全屏读写。无体积时中间纹理与 TRAA 输入同为半浮点、在像素中心采样，按理逐像素不变（未做逐字节对比；实时 GI 出图的相关 e2e 均通过）。回归检查：e2e「W5 real-time GI viewport」（Metal 下才红，SwiftShader 上限更高）。

### 纹理绑定

- 阴影图和环境贴图都通过 `texture()` 节点绑定，签名只记结构，不记哪张纹理。
- 每帧在 `VolumeNode.updateBefore` 里，即场景 pass 之后，把当前的 `light.shadow.map.depthTexture`、`scene.environment` 换进 `.value`；缺图时用 1×1 占位（阴影为全亮，环境为黑）。
- 原因：
  - 程序化天空和物理天空的环境贴图最快每 200 ms 新建一次，旧的被释放（`Lighting.tsx`）；
  - 关掉一盏灯的投影时，three 在场景 pass 构建材质的过程中释放它的阴影图。
- **顺序的保证**：`VolumeNode` 以场景 pass 的深度纹理节点为输入，three 按依赖先执行 pass 的 `updateBefore`，再执行 `VolumeNode` 的。e2e「关掉灯的投影后立即导出」检验这一点。

### 步进（`VolumeNode`）

- 按 three 自带显示节点（`GTAONode` 等）的写法写一个 `TempNode`：
  - `updateType = FRAME`，`updateBefore` 里画自己的全屏四边形。输出两张半精度目标：L.rgb 加视深，T.rgb 加按透射率加权的体积深度 d_v。
  - `setSize` 按 `renderer.getPixelRatio()` 定步进尺寸：宽 = 画面宽 × 0.5 / pixelRatio。视口在 DPR 2 时为画面的 1/4，导出（pixelRatio 1）为一半。
- **射线**：起点取该像素在近平面上的点，终点取不透明表面，都由 `getViewPosition` 重建，方向为两者之差。这样透视和正交两种投影都对（正交视图下工作台也画 `lit`，`engines.ts`）。天空像素的终点取视深 10 km。
- **视深**：步进、模糊和上采样都用同一个函数求视深，天空统一取 1e4（原型里步进存的是 1e4·cosθ，上采样用的是 1e4，两边不一致）。
- **区间与步数**：
  1. 与每个体积在局部坐标里求交（立方体用 slab，球用二次方程），裁到 [近平面, 表面]，得到至多 8 个区间和「这条线穿过哪些体积」的位掩码。
  2. 区间按入点排序（8 个以内用插入排序），合并重叠段。
  3. 总步数 48，每段保底 4 步，其余按段长比例分配。
  4. 每段内起点按固定种子的交错梯度噪声抖动。
- **每步**：
  - 只对掩码里的体积求密度和系数；
  - 每盏参与的灯求照度 × 可见性，乘上本体积的自遮挡 e^(−σt·弦长)；
  - 加上天空光照项；
  - 按 Hillaire 2015 的写法积分，同时累计 d_v = Σ(t · ΔT) / Σ ΔT。
- **光源**：
  - **平行光**：按 `CSMShadowNode` 的选法，`viewZToOrthographicDepth(z, near, min(maxFar, far))` 对照 `_cascades` 区间选级联，比较采样该级联的阴影图。
  - **聚光**：three 的 `getDistanceAttenuation`（`decay`、`distance` 截断）乘聚光锥衰减（与 `SpotLightNode` 相同），比较采样 2D 阴影图。
  - **点光**：three 的 `getDistanceAttenuation`，比较采样立方体阴影图（深度按 `viewZToPerspectiveDepth`）。
  - 不投影的灯可见性为 1。
- **天空光照项**：
  - 环境贴图在 6 个轴向上按糙度 1 采样取平均，乘 `environmentIntensity`；
  - 再加上 `AmbientLight` 的 color × intensity / π、`HemisphereLight` 的 (sky + ground) × intensity / 2π；
  - 每个像素算一次，不放在步进循环里。
- **弦长的边缘柔化**：自遮挡和表面投影用的解析弦长乘 (1 − `edge_falloff` / 2)，即沿弦从边到中心的平均密度，宽柔化的烟团不再偏暗。
- **数值**：L 写入半精度目标前夹到 6e4 以内；合成里用 `select` 代替 `mix`，NaN 不会漏进画面。

### 模糊与合成

- **模糊**：横、纵各 7 个采样，高斯权重 × 1 / (1 + |Δz| / (0.02·z))。
- **上采样**：取 4 个低分辨率纹素，按双线性份额 × 深度相近度加权。
- **覆盖掩码**：合成时在全分辨率对每个体积做一次解析求交（只求交，不求光照），得到这个像素的视线是否穿过体积。不穿过的像素直接取 AP(C)，与没有体积时逐像素一致；低分辨率和模糊不会让轮廓向外渗（AC-W5-3）。
- **与空气透视合成**（Epic「合成顺序」，修订 v9）：

  ```
  (S_v, T_v) = aerialAt(视线上 d_v 处)          // aerial 关闭时 S_v = 0，T_v = 1
  out = covered ? S_v·(1 − T_vol) + T_v·L_vol + T_vol·AP(C) : AP(C)
  ```

  AP(C) 是已做完空气透视的画面（背景像素为原色）。`aerialAt` 与 `applyAerial` 共用切片查表，没有新的 LUT。
- 实时 GI 路径里同样用固定噪声，不随帧变化，所以导出不依赖累积帧数。交给 TRAA 的帧间抖动留作后续调优。

### 表面阴影系数

```
volumeShadow(position, toLight, maxT):     // 在材质的阴影节点里，每盏灯各一份
  τ = 0
  for k in 0..castCount-1:                 // 只走打开投射阴影的体积
    i = castList[k]
    (a, b) = hitVolume(i, position, toLight)
    τ += σt_i · chordFactor_i · max(min(b, maxT) − max(a, 0), 0)
  return exp(−τ)                           // RGB，乘在 three 的阴影值上
```

- `castList`（8 个下标）和 `castCount` 是 uniform：切换开关、增删体积都不重新编译材质；没有打开投射阴影的体积时循环 0 次。
- **平行光（太阳、月光、文档里的平行光）**：`SunCascades` 重写 `setup`，返回 `super.setup(builder).mul(volumeShadow(...))`，toLight 取自它已有的光照方向。
- **聚光和点光**：`lights.ts` 在 `buildLight` 时装上 `ShadowNode`、`PointShadowNode` 的子类，子类同样重写 `setup`。three 只采用一盏灯第一次构建的阴影节点，所以只在建灯时装一次（与 `cascadeSunShadow` 相同）。`disposeLight` 释放它们（`BuiltLight.stop` 现在只管平行光）。
- **替身灯**：`SceneRenderer.compile` 用 `standIn` 克隆的灯预编译，three 的 `LightShadow.copy` 不复制 `shadowNode`，替身灯会用默认节点，生成的着色器与真实的灯不同，真实材质在下一帧全部重新编译。`standIn` 克隆灯时按原灯装同类的阴影节点，`disposeStandIn` 一并释放。体积的 uniform 是全局的，所以替身与真实的灯生成同一份代码。这也修好了现有的 `SunCascades` 错配。
- **实施时的发现**：develop 上新加一盏投影灯后，之后的帧仍会重新编译材质（WebGPU 与 WebGL2 都是），替身与真实材质的着色器只差生成的变量编号和 uniform 顺序，来自 three 的构建顺序，与阴影节点无关，不在本次范围。本次保证有体积时不比没有体积时多，即不比 develop 多（AC-W5-11）。

### 数据与服务端

- **`model.go`：**
  - `KindVolume = "volume"`，`maxVolumes = 8`；
  - `Object.Volume *Volume`；
  - `Volume{Shape *string; ScatterColor *string; ScatterDensity *float64; Anisotropy *float64; AbsorptionColor *string; AbsorptionDensity *float64; EdgeFalloff *float64; CastShadow *bool}`。
  - `completeVolume` 补缺省值，`validateVolume` 按契约的范围校验，越界的提示带字段路径 `volume.<field>`。
  - 内容字段表加 `"volume"`，`KindVolume` 只允许 `volume`。
- **`ops.go`：**`objectPatch.Volume *volumePatch`，`mergeVolume` 按键合并后再走 `completeVolume` 和 `validateVolume`。
- **上限：**`validateScene` 计数，超过 8 个返回 `ErrSizeLimit`「体积物件不能超过 8 个」。
- **`fingerprint.go`：**`o.Kind == KindVolume && pass != PassLit` 时跳过，与灯光相同。

### 编辑与拾取

- **新体积**：大小 2 m（`transform.scale = [2, 2, 2]`），在视口中心对着的地面点 g 上，底面贴地。文档坐标 Y 向上（`space.ts`），位置为 `[g.x, g.y + 1, g.z]`。
- **拾取**：线框的 `raycast` 按屏幕像素阈值（6 CSS px，按命中距离换成世界单位）判断边线，不用 three 默认的 `Line.threshold = 1`（世界单位 1 m）；盖住整间屋子的体积不会抢走墙角附近的点击。`centrePoint` 跳过体积物件。
- **密度滑杆**：对数刻度 0.001…100，最左一档为 0。吸收密度的缺省值就是 0。

## 局部设计与偏差

- **步数 48、每段保底 4 步、模糊 7 采样、深度权重 0.02：** 取穿刺的值。实施时用 AC-W5-1 的光束样例请 owner 看图，可以微调，只改常量（roadmap W5 的剩余不确定性）。
- **灯数上限 6 盏、阴影贴图 12 张：** 按 WebGPU 缺省的纹理上限定，不调高设备上限。
- **新体积的缺省大小 2 m：** 字段缺省值不变，大小只是 `transform.scale`。
- **天空光照取 PMREM 的 6 个方向平均：** 与材质用的是同一张图，同源（E2）。不读回 CPU，物理天空和程序化天空都适用。
- **GI 路径不用帧间抖动：** 先求确定和简单。TRAA 下的帧间抖动是后续调优，不在本次。Epic 允许用帧间抖动，本次不用。
- **GI 导出不缓存步进结果：** 每帧仍步进一次。TRAA 每帧的子像素抖动让结果不完全相同，4K 下约多 1 s，可接受。
- **介质里的点光：** 均匀步进下 1/d² 的奇点可能出亮斑或条带。先在样张里加一张点光图，有问题再改 equi-angular 采样。

## 接口与数据契约

- **契约状态：** 扩展，由本次完整交付。`data: ADD 体积物件` 和 `api: MODIFY 提交场景命令` 都按 Epic v9 的定义，不复制正文。
- **OpenAPI：**
  - 新增 `CreativeSceneVolume`，字段与 `data: 体积物件` 一一对应；
  - `CreativeSceneObject.kind` 的枚举加 `volume`，`CreativeSceneObject` 加 `volume`；
  - `update_objects` 的更新项加 `volume`，用 `CreativeSceneVolume` 的全可选版本。
  - 重新生成 Go 和 TS 两份生成物。
- **兼容：** 旧客户端不会写出体积物件。回滚到不认识 `volume` 的后端时，含体积的场景读取失败，只做前向修复（契约已写明）。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `api/openapi.yaml` | 修改 | `CreativeSceneVolume`，对象和更新项加 `volume` |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `backend/internal/creativescene/model.go` | 修改 | `KindVolume`、`Volume`、补齐、校验、合并、上限、内容字段表 |
| `backend/internal/creativescene/ops.go` | 修改 | `objectPatch.Volume` |
| `backend/internal/creativescene/fingerprint.go:Fingerprint` | 修改 | `clay`、`line` 跳过体积 |
| `backend/internal/creativescene/model_test.go` | 修改 | 补齐、校验、合并、上限 |
| `backend/internal/creativescene/fingerprint_internal_test.go` | 修改 | 三种导出方式的清除 |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | 工具说明 |
| `frontend/src/creative-scene/render/volumes/medium.ts` | 新增 | 系数换算、求交、密度、弦长 |
| `frontend/src/creative-scene/render/volumes/state.ts` | 新增 | uniform、`prepareVolumes` / `finishVolumes`、选灯、签名、纹理换图 |
| `frontend/src/creative-scene/render/volumes/lights.ts` | 新增 | 体积受光、天空光照项 |
| `frontend/src/creative-scene/render/volumes/march.ts` | 新增 | `VolumeNode`：区间、步进、模糊 |
| `frontend/src/creative-scene/render/volumes/composite.ts` | 新增 | 覆盖掩码、上采样、`applyVolumes` |
| `frontend/src/creative-scene/render/volumes/shadow.ts` | 新增 | `volumeShadow`、两个阴影节点子类 |
| `frontend/src/creative-scene/world/atmosphere/aerial.ts` | 修改 | `aerialAt` |
| `frontend/src/creative-scene/objects/Volume.tsx` | 新增 | 线框、拾取、登记 |
| `frontend/src/creative-scene/objects/Lighting.tsx:LitLighting` | 修改 | 登记「体积可画」 |
| `frontend/src/creative-scene/panels/VolumeFields.tsx` | 新增 | 属性栏 |
| `frontend/src/creative-scene/render/sceneRenderer.ts` | 修改 | 工作台路径接入、`prepareVolumes` / `finishVolumes`、`standIn` 的阴影节点 |
| `frontend/src/creative-scene/render/realtimeGI.ts` | 修改 | GI 路径接入 |
| `frontend/src/creative-scene/render/sunShadow.ts:SunCascades` | 修改 | `setup` 乘 `volumeShadow`；替身灯装同类节点 |
| `frontend/src/creative-scene/objects/lights.ts:buildLight` | 修改 | 聚光、点光装带体积系数的阴影节点；`disposeLight` 释放 |
| `frontend/src/creative-scene/objects/SceneObjects.tsx:ObjectNode` | 修改 | 渲染 `<Volume>` |
| `frontend/src/creative-scene/editing.ts` | 修改 | `newVolume` |
| `frontend/src/creative-scene/SceneEditor.tsx` | 修改 | 添加菜单、属性栏 |
| `frontend/src/creative-scene/panels/Outliner.tsx` | 修改 | 图标 |
| `frontend/src/creative-scene/Viewport.tsx` | 修改 | `centrePoint` 跳过体积 |
| `frontend/src/creative-scene/state/apply.ts` | 修改 | `contentKeys`、`updateKeys`、`mergeVolume` |
| `frontend/scripts/creative-scene-volumes.test.ts` | 新增 | JS 侧逻辑的单测 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | W5 的 e2e 检查 |
| `docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md` | 修改 | 体积物件 |

## 验证与交付

- **后端单测**（`check-go PKG=./internal/creativescene/...`、`./internal/creativeagent/...`）：
  - 补齐缺省值；
  - 越界和类型不符返回 400 并写明字段；
  - 第 9 个体积返回 413；
  - 只改一个字段的合并；
  - 指纹：`lit` 随体积变化，`clay`、`line` 不变（AC-W5-8）。
- **前端单测**，只测 JS 侧真正运行的逻辑，不另写一份着色器的 CPU 副本：
  - 颜色换算成系数（线性颜色）；
  - 选灯的排序与上限、结构签名；
  - 投影列表；
  - `mergeVolume`、`newVolume` 的落点。
- **着色器**由 e2e 在 GPU 上读回小块画面检验。
- **e2e**（端口 5293，`SCENE_E2E_ONLY` 过滤，只跑 W5 与回归项）：
  - W5 window：屋子加太阳，光束内外的亮度比不低于阈值，窗棂暗带；视口和单帧工作台导出各一次（AC-W5-1）。
  - W5 spot：锥内外（AC-W5-2）。
  - W5 range：移动、缩放后范围外像素不变，包括轮廓附近（AC-W5-3）。
  - W5 params：调密度、吸收颜色、各向异性后的变化方向（AC-W5-4）。
  - W5 cast shadow：地面变暗；关闭后与无体积一致（AC-W5-5）。
  - W5 clay：白模和线稿导出与无体积逐像素一致（AC-W5-6）。
  - W5 backends：SwiftShader 与 WebGPU 的差异（AC-W5-7）。
  - W5 twice：连拍两次逐像素一致（AC-W5-9）。
  - W5 ortho：正交视图的 `lit` 下体积位置正确。
  - W5 aerial：物理天空、空气透视加大 `distance_scale`，一团烟跨在地平线上，上下两侧没有接缝。
  - W5 shadow off：关掉一盏灯的投影后立即导出，画面正确，没有读到已释放的纹理。
  - W5 many lights：超过上限的投影灯，步进管线照常建出，超出的灯只照亮表面。
  - W5 no recompile：加一盏投影的聚光或平行光后，帧里重新编译的程序数与没有体积时相同，步进材质不在其中（实施时调整，见「表面阴影系数」）。
  - 回归：W3 的「day unchanged」、「sun shadows」、W4 的 far 和 GI，确认没有体积时画面不变。
- **助手**（AC-W5-10）：按工具说明在开发环境里请助手做一次，存截图到 evidence。
- **性能**（AC-W5-11）：在参考设备上用穿刺的计时方法对产品视口实测（WebGPU，1280 × 720 CSS 像素、DPR 2），记入 evidence：
  - 1 个、8 个、8 个都投影三组；
  - 没有体积的场景：帧时间与 develop 对照；加一盏投影灯时的材质编译时间与 develop 对照。
- **owner 看图**：窗口光束、聚光光锥、浓烟投影、介质里的点光四张样张，存进 evidence。
- **回退**：体积是新对象类型。出问题时前端可以不绘制介质，只显示线框，数据不受影响。

## 未决事项

无。步数和模糊强度的微调见「局部设计与偏差」，在 owner 看样张时定，不阻塞实施。AC-W5-1 的亮度比阈值在实施时由穿刺样张实测后写进 e2e。
