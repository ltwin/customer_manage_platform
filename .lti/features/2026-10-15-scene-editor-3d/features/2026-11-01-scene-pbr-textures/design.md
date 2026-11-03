# C5c1 场景图片与材质贴图：详细设计

## 继承的设计

- Epic `scene-editor-3d` 修订 v16：
  - design「材质与光照（C5）」的「贴图与 HDRI」一项：贴图是原理化 BSDF 同名参数的图片输入；场景引用图片是通用能力；世界空间层完整采样；材质资产按复制语义处理。
  - 不变量 I1（只引用不可变内容、GC 持有）、I7（资产库只复制）、I8（账号隔离）。
  - 未决事项 10：本设计负责解决它。
- 项目约定：
  - `patterns: 场景着色写法`：只用 TSL；分叉里不放隐式导数的采样；分叉之前先存成变量；光追只经 `render/trace/`。
  - `data: 场景文档`、`data: 模型资产`、`api: 提交场景命令`、`api: 读取场景`。
- 探索与调研：
  - `discovery/brainstorm.md`：owner 的 6 项决定，其中阴影上限 8 张是过渡做法。
  - `discovery/research/texture-pipeline.md`：纹理预算、材质构建、图片缓存、世界空间层、后端数据、媒体，下文简称「调研」。
- 与其他 Epic 的关系：
  - `scene-world-space-gi` 的 U6（命中点怎样采样贴图）由本设计给出方案。归档时协调修订那个 Epic，把 U6 标为已决。
  - `cg-composite` 的人片卡片和原片背景复用本次的场景图片层（服务端的图片引用与校验、前端的图片缓存与出图前等图）。

## 领域模型与业务规则

### 贴图属于材质槽

- 贴图是材质槽分配的一部分，只用于原理化 BSDF（`principled@1`）。程序化预设不接贴图，它们的外观由预设函数决定，已发布的外观不变。
- 一个槽位最多五种贴图：`base_color`、`normal`、`roughness`、`metalness`、`ao`。每种贴图引用一个**图片内容修订**（`content_revision_id`），与模型引用一样指向不可变的修订（I1）。
- 一个槽位只有一套映射（`mapping`），五张贴图共用：
  - `triplanar`：三平面投影，默认。按物件空间的米铺开，物件缩放时不拉伸，与现有预设相同。
  - `uv`：使用模型的 `TEXCOORD_0`。
  - 这与 Blender 里多个 Image Texture 节点共用一个 Mapping 节点的常见做法相同；每张图各自映射的需求出现时再加。
- 贴图与常数参数相乘，语义同 three.js 和 glTF：
  - 基础色 = `color` × 贴图（sRGB）；
  - 糙度 = `roughness` × 贴图通道；
  - 金属度 = `metalness` × 贴图通道；
  - AO 只压暗间接光；
  - 法线贴图是切线空间，按 OpenGL 约定（+Y 朝上，与 glTF、Blender 相同），强度可调。
- 糙度、金属度、AO 各自读取一个通道（缺省 `r`）。所以 glTF 的打包 ORM 图可以给三项都指定同一张图，再分别取 `r`、`g`、`b`；分开的灰度图直接用缺省值。
- **透明方式**（owner 2026-11-02，对应 glTF 的 OPAQUE / MASK / BLEND）：
  - `opaque`：缺省值，忽略基础色贴图的透明通道；常数 `opacity` 和 `transmission` 的行为与现在完全相同。已有场景不受影响。
  - `clip`：透明度（`opacity` × 贴图透明通道）低于 `alpha_cutoff`（缺省 0.5）的部分直接挖掉，留下的部分完全不透明（同 glTF MASK），边缘清晰，用于树叶、铁丝网、镂花。物件按不透明物件参与深度、AO 和世界空间层，挖掉的部分不投影。
  - `blend`：贴图透明通道乘到 `opacity` 上，走现有的半透明路径，用于薄纱、脏玻璃。即使 `opacity = 1`、贴图也全不透明，仍按半透明物件处理。
  - 物件归哪一类（透明队列、深度写入、AO、世界空间层）由「材质渲染策略」统一决定，见下表。
  - 与 Blender 一致，白模、线稿和线框不看材质，所以不体现镂空（owner 2026-11-02）。镂空效果在材质预览、最终渲染和 `lit` 导出里体现，AI 重绘时可以参考 `lit` 图。
  - 透明裁切的着色逻辑写成通用的，`cg-composite` 的人片卡片复用。

### 材质渲染策略

现在「是否半透明」由 `principled.ts:seeThrough` 只看常数 `opacity < 1 || transmission > 0` 决定，`principledProps` 的 `transparent`/`depthWrite`、`SlotMesh` 的分片绘制、AO 预渲染和世界空间层的收集（`render/trace/collect.ts`）都依赖它。加了透明方式后，改由一个函数 `renderPolicy(slot)` 统一给出，以上各处只读它的结果：

| `alpha_mode` | 透明队列（`transparent`、分片绘制） | 深度写入 | AO 预渲染 | 世界空间层 | 透明测试 |
|---|---|---|---|---|---|
| `opaque` | `opacity < 1` 或 `transmission > 0`（现状） | 与左列相反 | 与左列相反 | 与左列相反 | 无 |
| `clip` | 只看 `transmission > 0` | 是 | 参与 | 参与，命中点按裁切处理 | `opacity × a < alpha_cutoff` 挖掉 |
| `blend` | 总是 | 否 | 不参与 | 不参与 | 无 |

- `clip` 不带基础色贴图时，`a = 1`，整个表面按 `opacity` 与阈值比较，要么全留、要么全挖。编辑器在面板上提示「没有透明贴图，裁切只看不透明度」。
- `clip` 加 `transmission > 0`（带镂空的玻璃）按透明队列处理，和现在的玻璃一样不进世界空间层。
- 缓存键 `keyOf` 包含整个槽位分配：`maps`、`mapping`、`alpha_mode`、`alpha_cutoff`。只切换透明方式也会得到新的材质实例。

### 场景引用图片（通用）

- 场景快照里出现的图片内容修订，必须属于本账号、种类为 `image`、状态 `ready` 且可展示。校验规则与模型引用相同，在写入事务里完成（I8）。
- 被引用的图片登记进 `creative_document_content_refs`，与模型引用同表，成为 GC 持有方（I1）。历史修订照常持有，与现在的模型相同。
- 单个场景最多引用 64 张不同的图片。这是假设值，实测后调整，与 `maxSceneModels` 并列。
- 「引用」的收集只看场景格式里定义为图片引用的字段。本次只有 `material_slots.*.maps.*`；`cg-composite` 增加物件类型时，在同一个收集函数里加入口。

### 内容里引用的内容（新的持有方）

- 资产库里的模型副本（`model.materials`）和材质资产（`material`），都会在内容修订的载荷里引用图片。
- 新增通用的持有关系：「修订 A 持有修订 B」。它在写入 A 的同一事务里写入，之后不再修改，因为修订不可变。只要 A 的状态不是已删除，B 就不会被 GC 回收。
- 这个关系不递归判断：A 本身由谁持有，交给 A 自己的 GC 判定；A 被回收后，下一轮 GC 再回收 B。

### 材质资产

- 新增内容种类和资产种类 `material`：
  - 载荷包括一个槽位分配（`preset`、`params`、`maps`、`mapping`）和来源说明；
  - 绑定一张缩略图：前端渲染的材质球 PNG，256 px，与模型缩略图相同，作为 blob 对象角色 `thumbnail`。
- 存入资产库时，复制出一份只属于资产库的修订（I7）。用到槽位时，把载荷复制进物件的 `material_slots`，之后与资产没有关联。
- 存入时如果资产库里已有相同的材质，就复用那份资产。判断依据是载荷完全相同，与模型资产的 `FindModelAssetInTx` 相同。

### 表面阴影上限（过渡做法）

- 普通表面最多使用 8 张阴影贴图。成本按张计：投影平行光 4 张（级联），聚光、点光、面光各 1 张。
- 选取顺序固定，与相机无关，所以拖动视角不会改变选取结果：
  1. 天空太阳（程序化或物理天空，投影时 4 张）。
  2. 文档里打开了投影的灯，按物件列表的顺序，放得下就选，放不下就跳到下一盏。
- 落选的灯照常照亮，只是不投影：编辑器在这盏灯上显示「阴影超出上限」，助手读场景时这盏灯标为 `shadow: "over_budget"`。
- 这是过渡做法（owner 2026-11-02），最终方案另立 change。之所以不按调研里「按贡献排序」来选：贡献随视角变化，选中的灯一变，所有材质就要重新编译；而按物件列表顺序，摄影师可以自己调整，结果也可以预期。

## 模块职责与协作

### 写入（服务端）

```
编辑器 / 助手 ──update_objects(material_slots.maps)──▶ creativescene.Apply
   ├─ validateSlots：外形（maps 只在 principled@1；键、通道、映射字段与范围）
   ├─ 执行操作，得到修改后的场景
   ├─ verifyImages：新出现的图片修订 → RequireUsable(display) + Kind=="image"；已持有的批量复核
   └─ encodeScene：Refs = modelRefs ∪ imageRefs
creativedocument.persist ─▶ 新修订 + creative_document_content_refs
```

- 助手写入前，`creativeagent` 先确认每个新的图片 ID 对本次运行可读，做法同 `save_library`。不可读时，工具结果写明原因。「新的」指当前修订还没有引用的图片；在同一场景里把一个槽位的贴图复制到另一个槽位不需要再确认。
- **经持有方可读**：图片可能只被材质资产或模型库副本持有（原图已经从画布和资产库删掉），这时直接判断图片会不可读，助手就用不了这份材质。所以 `readableRevisionInTx` 加最后一个分支：
  1. 先查图片本身是否被撤销授权，撤销的直接拒绝（与现在相同，撤销优先）。
  2. 原有分支（直接授权、画布、消息、资产库）任一可读即可。
  3. 新增：`creative_content_revision_refs` 里存在某个持有方修订持有这张图片（`role = texture`），而这个持有方修订按同一函数可读（它自己的撤销检查照常生效）。只查一层，不递归。
- 这一条只放宽「经由已授权的材质或模型使用其贴图」，不放宽单独读取像素：`read_asset` 读材质时只返回贴图的图片 ID、尺寸和缩略图，与读模型时只给事实相同。

### 渲染（前端）

```
场景快照 ─▶ resolveSlot ─▶ materialFor(key 含图片修订 ID 与映射)
                              ├─ images.ts：按修订取 Texture（页面级缓存，引用计数）
                              │    └─ ORM：按(三张源图, 三个通道)合成一张
                              └─ build：MeshPhysicalNodeMaterial
                                   colorNode / roughnessNode / metalnessNode / aoNode / normalNode
                                   ← maps.ts 的映射函数（三平面 / UV，光栅传隐式导数采样）
世界空间层 ─▶ restyle(material) ─▶ TraceTables 行 + TraceMaps（三张纹理数组，每设备一份）
                                   └─ hit.ts：maps.ts 同一映射函数，传 .grad() 采样
离屏出图 / 材质缩略图 / 3D 预览 ─▶ 先 loadSceneImages(scene)，再编译、计帧
```

- 图片、贴图、数组都只在前端处理；服务端不碰像素。
- 材质的映射与法线解码只写一份（`materials/maps.ts`），光栅和命中点各自传入采样方式。这和预设函数通过「宽度函数」区分光栅与命中点是同一个思路，可以避免两份实现在外观上逐渐漂移。

## 关键机制

### 光栅材质的构建

- 有贴图的原理化材质用显式 TSL 节点，不用 `map` 这类内置字段，因为内置字段只走 UV（调研 §2）：
  - `colorNode = color × sample(base_color)`
  - `roughnessNode = roughness × sample(orm).g`
  - `metalnessNode = metalness × sample(orm).b`
  - `aoNode = sample(orm).r`
  - `normalNode = perturb(normal map)`
- 三平面投影：
  - 坐标用现有的 `positionLocal × modelScale`（米）和 `normalLocal / modelScale`（`materials/instances.ts`），权重用 `triWeights`（`materials/patterns.ts`）。
  - 每个轴平面的坐标为 `planeUV / size`，再按 `rotation` 旋转。
  - **三个轴无条件采样，再按权重混合**，不加 `If (w > ε)`（约定「分叉里不放隐式导数的采样」）。
- 三平面法线：
  - 每个轴把切线空间法线换算成高度梯度 `(−nx/nz, −ny/nz) × strength`。这个梯度在旋转后的贴图坐标里，要先按 `−rotation` 转回投影平面的两个轴，再按负向轴翻转符号，然后按权重混合。
  - 混合后作为协向量变换到视空间，投影到表面：`N' = normalize(N − (g − (g·N)N))`。
  - 这与 `bumpNormal` 的表面梯度做法一致，实时 GI 的预渲染和阴影偏移都会自动用上（调研 §2）。
- UV 模式：
  - 不用 three 的 `normalMap`：它的切线来自几何体的 `tangent` 属性，或者对原始 `uv()` 求导（`TangentUtils.js`），不知道贴图坐标被旋转和缩放过，旋转 90° 后凹凸方向会错。
  - 改为在 `maps.ts` 里按变换后的坐标 `uv' = rotate(uv) × repeat` 求切线框架：`T`、`B` 由 `dFdx/dFdy(positionView)` 和 `dFdx/dFdy(uv')` 求出（同 three 的无切线做法，只是换成 `uv'`），法线 = `normalize(TBN × (n.xy × strength, n.z))`。几何体自带的 `tangent` 属性不用，光栅与命中点因此是同一个约定。
  - 命中点用三角形三个顶点的 `uv'` 求同样的 `T`、`B`（见世界空间层）。
  - 几何体没有 `TEXCOORD_0` 时按三平面投影渲染。编辑器的材质面板和助手的 `read_scene` 都会提示「这个模型没有 UV，按三平面显示」。
- 透明方式（队列、深度写入等由「材质渲染策略」决定）：
  - `clip`：`opacityNode = opacity × a`，`alphaTestNode = alpha_cutoff`；留下的部分按不透明绘制。
  - `blend`：`opacityNode = opacity × a`，`transparent = true`，与现有半透明材质相同（不参与 AO）。
  - 阴影：投影的深度渲染也要按 `clip` 挖空。three r186 的阴影 pass 是否自动沿用材质的 alpha test，在第一项任务里验证；不沿用时，给材质设置 `castShadowNode` 或 `maskShadowNode`，采样同一张基础色贴图。
- 缓存键 `keyOf` 加入整个槽位分配：`maps`（各图片修订 ID 和通道）、`mapping`、`alpha_mode`、`alpha_cutoff`：
  - 只换图片：只重建 CPU 端的节点，GPU 程序复用（按 WGSL 文本缓存），不重新编译。
  - 增减某种贴图、切换映射方式：重新编译，与新增灯光相同，走预编译。
- 没有贴图的原理化材质和全部预设：构建路径与现在完全相同，现有外观不变。

### 图片缓存（`materials/images.ts`）

两层身份，源图与纹理分开：

| 层 | 键 | 内容 | 共享范围 |
|---|---|---|---|
| 源图 | 图片修订 ID | 解码后的 `ImageBitmap` | 同一修订无论用作什么都只解码一次 |
| 纹理 | `TextureKey` | three 的 `Texture`（带 `colorSpace`） | 页面级，所有 GPU 设备共用一个对象 |

`TextureKey` 有三种，写成规范化的字符串，同时用作光栅缓存、世界空间层的层号分配、引用计数和确定性排序的键：

- `color:<revision>`：基础色，sRGB。
- `normal:<revision>`：法线，线性。
- `orm:<ao rev>.<ch>|<roughness rev>.<ch>|<metalness rev>.<ch>`：ORM，线性；缺的一项写 `-`。三项都指向同一张图、通道依次为 `r`、`g`、`b`（标准 ORM）时，直接用源图上传，不合成，但键仍是这个组合键。

同一张图同时用作基础色和糙度时，得到 `color:` 和 `orm:` 两个纹理，各自按正确的颜色空间解码，互不影响，也与加载顺序无关。

```
acquire(key) ─▶ 命中：refs++
               未命中：建 Texture（Repeat、mipmap、colorSpace 由键的种类决定）
                        → 每张源图 acquireSource(revision)：
                            mediaTicket(revision,'original','display') → fetch
                            → createImageBitmap(imageOrientation:'from-image', 长边 ≤ 2048)
                        → color / normal：texture.image = bitmap
                          orm：三张源图缩放到相同尺寸，合成一张 RGBA（缺的通道填 1）
                        → needsUpdate
release(key) ─▶ refs--；为 0 时延迟 1 s：texture.dispose()，并释放它用到的源图
releaseSource ─▶ 源图 refs 为 0 时 bitmap.close()
ready(keys) ─▶ Promise；任一张失败 → reject，原因写明是哪张图片
```

- **Texture 对象一旦建立就不再更换**，图片晚到时只更新 `version`。换对象会导致节点重建（`objects/Lighting.tsx` 里换环境贴图的注释说的就是这个）。材质实例也是页面级共享的，所以没法为每个设备单独换 `.value`。
- 占位图：每种纹理共用一个 1×1 对象。基础色用白、法线用 (0.5, 0.5, 1)、ORM 用 (1, 1, 1)，所以图片到达之前，画面等于常数参数的效果。
- 原图一律读 `original`：展示图是有损 JPEG，还会丢透明通道（调研 §5）。
- 加载并发：同时最多解码 4 张，其余排队，避免一次打开几十张大图卡住主线程和网络。

### 显存预算

- 估算：长边 2048 的 RGBA 带 mip 约 21 MiB。64 张源图光是光栅纹理就约 1.33 GiB；ORM 合成是额外的纹理，再加上世界空间层的纹理数组和渲染目标。three 按 renderer 分别上传，视口和离屏出图同时存在时还要翻倍。只限制张数不够。
- 第一项任务先实测，再定数值：用参考场景（64 张 2048 图片、全部开启世界空间层）在本机测视口、离屏出图同时存在时的显存峰值，据此确定每个设备的纹理字节预算，回写本文件。
- 超出预算时的处理，不悄悄降低画质（[[render-quality-over-performance]]）：
  - 光栅：按字节预算而不是张数做准入。超出的纹理保持占位图，编辑器显示「贴图过多，部分未加载」并列出是哪些；助手的 `read_scene` 同样告知。
  - 离屏出图：超出预算或分配失败时，这张图失败，失败码 `SCENE_IMAGES_FAILED`，说明原因，不出缺贴图的图。
  - 离屏出图的设备在空闲时释放它上传的纹理（three 的 `Textures` 按 renderer 管理），不常驻第二份。
- 后端的 64 张是源图张数，前端的预算按字节和派生纹理计，两者分开统计。

### 等图片就绪

- 离屏出图（`render_view`、`lit` 导出）：`openShooter` 在挂载前调用 `loadSceneImages(scene)`，与 `loadNightAssets` 并列。任一张失败，这张图就失败，并说明哪张图片不可用，不出缺贴图的图。失败码沿用夜空资源的做法，新增 `SCENE_IMAGES_FAILED`。
- 材质缩略图（`library/thumbnail.ts`）和 3D 预览（`preview/ModelPreview.tsx`）同样先等图片。预览在等待期间继续显示静态缩略图。
- 视口不等：先用占位图画，图片到了再重绘（`invalidate`）。

### 世界空间层（解决 U6）

方案完整记在调研 §4，下面只列采纳的决定。

- **数据**：
  - 新增 `render/trace/maps.ts` 的 `TraceMaps`，每个设备持有三张 `texture_2d_array`：基础色 `rgba8unorm-srgb`、法线（RG，运行时重建 z）、ORM；带完整 mip，平铺方式为 repeat。
  - 每层的尺寸：视口 512²，导出 1024²。
  - 层号按 `TextureKey` 分配和引用计数（与光栅的图片缓存同一个键），三张数组各自独立：`color:` 键进基础色数组，`normal:` 键进法线数组，`orm:` 键进 ORM 数组。空出的层号放进空闲列表复用。所以通道配置不同的 ORM 占不同的层，不会串材质。
  - 容量按 1.25 倍增长：拷贝到新数组，再换 `node.value`，不重新编译。
- **写入层**：先把源 Texture 重采样到中间渲染目标并生成 mip，再用 `copyTextureToTexture` 逐层、逐 mip 拷贝。这样 CPU 端不用常驻每层 4 MiB 的像素，也避免每加一层就重算整个数组的 mip。
- **材质行**：`MATERIAL_ROW` 由 8 改为 10 个 vec4。
  - 第 6 个：三个层号和映射方式，`−1` 表示没有这张贴图或还没就绪。
  - 第 7 个：两个方向的尺寸倒数、旋转、法线强度。
  - 第 8 个：UV 模式的 repeat。
- **几何**：UV 合并进现有的顶点颜色缓冲，改名为「顶点附加」缓冲：每个顶点 3 个 u32，分别是 RGBA8 颜色、u（f32）、v（f32）。这样存储缓冲的数量不增加，探针核仍是 7/8（调研 §4）。
  - `GeometryData`、`Segment`、代理简化（`simplify`）都要带上 UV。
  - **代理简化要保住 UV**：现在的 `simplify` 只约束位置误差，保留剩余顶点的 UV 并不能让插值结果不变（审查用相同参数实测：平面从 32 个三角形简化到 14 个，几何误差约 0.000137 m，中心点的 UV 却从 (0.8, 0.5) 变成 (0.5, 0.5)）。这会让探针采错颜色，也会移动 `clip` 的镂空位置。
    - 带 UV 的几何体改用 `MeshoptSimplifier.simplifyWithAttributes`，把 UV 作为属性传入并给足权重。
    - 简化后检查 UV 误差：超过上限（按 UV 空间 1/512，即视口每层 512² 时的一个纹素）就整组保留原几何，与现在「简化不到误差内就保持原样」的规则（I8）一致。
    - 三平面投影不依赖 UV，不受影响；不带 UV 的几何体保持现在的简化方式。
  - `cache.ts` 的 `BUILDER_VERSION` 加 1，让旧缓存失效。
- **命中点**：
  - 在 `Switch` 之后、单独的 `If(行里有贴图)` 里采样，输入在分叉之前存成变量。
  - 一律用 `.grad()`：梯度来自已有的光线锥微分 `dpx/dpy`。三平面按投影平面取分量；UV 模式用三角形的 UV 雅可比矩阵换算。
  - UV 模式的 TBN 由三角形的三个顶点和变换后的 `uv'` 求出，不存切线，与光栅的约定相同（见「光栅材质的构建」）。三平面的梯度同样先按 `−rotation` 转回投影平面。
  - AO 只乘到间接项（`lightAt` 和探针反弹）。
  - **透明裁切**：求交库只返回最近的交点，没有 any-hit 回调。所以命中 `clip` 材质时，先采样基础色贴图的透明通道；被裁掉的话，从交点稍往前偏移，继续追同一条光线，最多再追 4 次。反射光线、探针光线和命中点朝灯光发出的阴影光线都按这个规则处理。
  - 「确认没有命中」和「次数用完」分开处理。次数用完时按保守的方式处理，不当作没有命中，否则多层叶片后面的实墙会被看穿成天空，阴影光线也会误判灯光可见：
    - 阴影光线：按被遮挡处理。
    - 反射光线和探针光线：把最后一次命中的面当作不透明，按它的材质着色。
    - 结果是密集叶丛偏暗、颜色偏向叶片，而不会漏光。调试统计里记录「次数用完」的光线数，供排查。
  - `blend` 材质沿用现有规则：透明材质不进世界空间层（`render/trace/collect.ts`）。
- **就绪与确定性**：
  - 贴图上传完成之前，材质行的层号写 −1，按常数参数着色。上传完成后改写材质行，`lightingVersion` 加 1，探针重新采集。
  - `base.ready()` 要包含图片层就绪，导出等它就绪后才计帧。
  - 导出时按 `TextureKey` 字符串排序分配层号，所以两次导出逐像素相同。
- **容量**：
  - `gpu.ts` 申请适配器支持的 `maxTextureArrayLayers`（默认 256）。
  - 超出时，多出的图片在世界空间层里退回常数参数，并明确提示（不悄悄降低画质，`scene-world-space-gi` I8）。
  - 光栅不受这个上限影响。
- **约定修订**：`patterns: 场景着色写法` 里「命中点不做凹凸」改为「程序化预设的凹凸命中点不做；贴图的法线在命中点照做（每个三角形求 TBN，`.grad()` 采样）」。

### 表面阴影上限

```
lights（文档顺序） + sky.sun
  ─▶ selectShadowCasters(budget = 8)：
       sun (4) → 文档灯依次：cost = directional ? 4 : 1；放得下就选，否则落选
  ─▶ 选中的灯 castShadow = true；落选的灯 castShadow = false，标记 over_budget
```

- 函数放在 `objects/shadowBudget.ts`，`objects/lights.ts` 和 `objects/Lighting.tsx` 按结果设置 `castShadow`。体积光的 `selectLights` 保持独立，各自的上限不变。
- 只有选中的集合变化时才重新编译：增删灯、开关投影、改物件列表顺序。拖动灯光不改变集合，因此不会触发编译。
- 预留的 8 张纹理：DFG 1、环境 1、玻璃 1、LTC 2、贴图 3。加上阴影 8 张，合计正好 16（调研 §1）。
- 编辑器：属性面板中灯光的「投影」开关旁显示「阴影超出上限」，悬停说明规则。助手：`read_scene` 的灯光带 `shadow: "on" | "off" | "over_budget"`。

### 材质资产的存入与使用

- 存入：
  - 编辑器在槽位菜单里点「存为材质资产」，渲染材质球缩略图并上传到暂存，再调用新接口。
  - 服务端按 `SaveModelToLibrary` 的顺序处理：校验目标 → 读取当前修订 → 检查物件内容单元的预期版本 → 取槽位 → 锁定暂存的缩略图 → 写材质修订（同一事务写入持有关系）→ 建资产 → 消费暂存。
- 使用：
  - 编辑器：从资产面板把材质拖到物件槽位上，或者在槽位菜单里选材质资产。前端把载荷复制进 `material_slots`，走普通的 `update_objects`。
  - 助手：用 `search_assets kind:material` 找到材质，用 `read_asset` 看载荷，把载荷写进 `edit_scene`。做法同模型资产。载荷里的图片即使原图已从画布和资产库删掉，也经材质资产这个持有方可读（见「写入（服务端）」）。
- 资产库界面：卡片显示材质球缩略图，查看浮窗用 3D 材质球预览。材质资产不能放到画布上，与模型资产相同。

## 局部设计与偏差

| 决定 | 依据 | 取舍 |
|---|---|---|
| 贴图只用于原理化 BSDF | 预设外观发布后不变；参数名本来就对齐 three.js 和 glTF | 想给石砖预设叠一张贴图做不到，只能改用原理化材质 |
| 一个槽位一套映射，糙度、金属度、AO 各自选通道 | 符合常见做法，界面简单；能直接用 glTF 的 ORM 图 | 每张图各自映射的需求以后再加 |
| 每套材质固定最多 3 个绑定（基础色、法线、ORM） | 在 16 张纹理的上限下余量可控（调研 §1） | 分开上传的糙度、金属度、AO 要由前端合成 ORM |
| 原图读 `original`，客户端缩到 2048 | 展示图是有损 JPEG 且丢透明通道 | 首次加载大图较慢；需要时以后加专用的贴图格式 |
| 世界空间层用纹理数组，视口每层 512²、导出每层 1024² | WebGPU 没有 bindless；显存可控（调研 §4） | 反射里的贴图比光栅稍糊，靠光线锥选 mip 弥补；owner 看对照图验收 |
| 阴影上限按物件列表顺序选取 | 结果稳定、可以预期，避免随视角重新编译 | 不一定是视觉上最重要的灯；摄影师可以自己调整顺序。过渡做法 |
| 透明方式做 `clip` 和 `blend`，白模和线稿不体现 | 与 glTF、Blender 一致；镂空的效果在 `lit` 图里体现（owner 2026-11-02） | 世界空间层要为 `clip` 补「被裁掉就继续追」，每条光线最多多追 4 次 |
| 透明队列、深度写入、AO、世界空间层的归属由 `renderPolicy` 统一决定 | 原来分散在 `seeThrough` 的各个调用方，加透明方式后容易不一致 | 现有调用方都要改成读策略；`opaque` 的结果与现在逐项相同 |
| 纹理键区分用途（`color:`/`normal:`/`orm:`），源图按修订共享 | `colorSpace` 是 Texture 的属性，同一张图用作基础色和糙度要两份；ORM 是组合，没有单一修订 ID | 同一张图两种用途时多占一份显存 |
| UV 模式自己按 `uv'` 求切线框架，不用 three 的 `normalMap` | three 只按原始 `uv()` 或几何体切线求 TBN，旋转贴图后凹凸方向会错 | 不使用 GLB 自带的切线，接缝处与 Blender 按切线烘焙的结果可能略有差别 |
| 裁切追踪次数用完时按保守方式处理 | 当作没有命中会漏光，改变遮挡关系 | 密集叶丛偏暗 |
| 带 UV 的几何体按属性感知简化，UV 误差超限保留原几何 | 只约束位置误差会移动贴图和镂空位置 | 带 UV 的复杂模型代理面数更多 |
| 助手可以经已授权的持有方使用其贴图 | 原图删掉后，材质资产仍是可用的；否则助手用不了材质 | 只查一层，撤销授权优先 |
| 新增通用表「修订持有修订」，不按资产范围建表 | 库副本放进场景后，资产即使被彻底删除，副本修订里的图片引用仍然有效 | 多一张表，要登记进 GC 的持有方条件和守卫测试 |

假设（实施中验证）：
- 单场景最多 64 张图片；长边 2048；数组层 512² 和 1024²。实测显存和加载时间后再调整。
- 三平面法线的表面梯度混合，在 45° 斜面上没有明显接缝。用测试场景对照 Blender 的 Box 投影（blend 0.2）。

后续增强（不在本次）：
- 每张贴图各自的映射。
- 程序化预设叠加贴图。
- 置换贴图。
- 阴影的最终方案。

## 接口与数据契约

契约状态：**扩展**（兼容）。完整定义（字段表、错误、请求与响应示例）以 Epic `contracts.md` 修订 v17 为准，由 C5c1 交付；下面只列要点。写进 Epic 时做了三处对齐：图片超过 64 张改为 413 `creative_size_limit`（与物件、摄像机上限相同）；「材质存入资产库」的请求改用 `target`，错误码改用 `creative_document_conflict`、`creative_state_conflict`（与「模型存入资产库」相同）；读取场景的 `images` 不带名称（图片内容没有统一的名称字段，助手看到图片 ID 和尺寸）。另外补了 `patterns: 场景着色写法` 和 `architecture: 场景模块` 两条修改。

### `data: 场景文档`（MODIFY）

`objects[].material_slots` 的值新增两个可选字段：

| 字段 | 类型 | 必填 | 约束与缺省 |
|---|---|---|---|
| `maps` | object | 否 | 只在 `preset = principled@1` 时允许，其他预设写了返回 400。键为 `base_color`、`normal`、`roughness`、`metalness`、`ao`，至少一个 |
| `maps.<kind>.content_revision_id` | string（`ccrv_…`） | 是 | 必须是本账号 `image` 类型、`ready` 且可展示的内容修订；否则返回 400，指出字段 |
| `maps.<kind>.channel` | `"r"`\|`"g"`\|`"b"`\|`"a"` | 否 | 只对 `roughness`、`metalness`、`ao` 有效，缺省 `r`；写在 `base_color`、`normal` 上返回 400 |
| `mapping` | object | 否 | 只在有 `maps` 时允许；缺省 `{kind:"triplanar", size_m:1, rotation_deg:0, normal_strength:1}`，存储时补齐 |
| `mapping.kind` | `"triplanar"`\|`"uv"` | 否 | 缺省 `triplanar` |
| `mapping.size_m` | number 0.01…100 | 否 | 三平面投影时一块贴图的宽度（米），高度按图片宽高比；`uv` 时不允许 |
| `mapping.repeat` | `[u, v]`，各 0.01…1000 | 否 | 只在 `uv` 时允许，缺省 `[1,1]` |
| `mapping.rotation_deg` | number 0…360 | 否 | 缺省 0 |
| `mapping.normal_strength` | number 0…4 | 否 | 缺省 1；没有法线贴图时也允许保存 |
| `alpha_mode` | `"opaque"`\|`"clip"`\|`"blend"` | 否 | 只在 `principled@1` 时允许；缺省 `opaque`，存储时补齐（有 `maps` 时）。不带基础色贴图也允许，此时透明度只来自 `opacity` |
| `alpha_cutoff` | number 0…1 | 否 | 只在 `alpha_mode = clip` 时允许，缺省 0.5 |

- 引用表 `creative_document_content_refs` 同时记录场景引用的图片修订。单个场景最多 64 张不同的图片，超出返回 413 `creative_size_limit`。
- 修改 `maps` 或 `mapping` 只涉及 `object:<id>:content` 单元，与其他材质字段相同。渲染指纹已经整体计入 `material_slots`，贴图一变，`lit` 导出图就会提示过时。
- 按键合并：`update_objects` 里的 `material_slots` 按槽位整体替换，这与现有规则相同，`maps` 和 `mapping` 不单独合并。

### `api: 提交场景命令`（MODIFY）

- 引用的图片不存在、不属于本账号、不是 `image`，或者不可展示：返回 400，错误形态与模型引用不可用相同，错误码为 `creative_image_unavailable`，`details` 里指出字段路径。
- 超过 64 张图片：返回 413 `creative_size_limit`，`details.limit = 64`。

### `api: 读取场景`（MODIFY）

响应新增必返的 `images: {<content_revision_id>: {usable, width, height, mime}}`，与 `models` 并列，供编辑器选择缩放尺寸、显示缺图提示。只列当前修订引用的图片。

### `data: 模型资产`（MODIFY）

- 库副本的 `model.materials` 里每个槽位的结构与场景的 `material_slots` 相同，可以带 `maps`、`mapping`。
- 库副本引用的图片，写入时登记进 `creative_content_revision_refs`（见下）。

### `data: 材质资产`（ADD）

- 内容种类 `material`；资产种类 `material`。迁移会把 `material` 加进 `creative_contents` 和 `creative_assets` 的种类检查。
- 载荷 `material`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `slot` | `{preset, params, maps?, mapping?}` | 结构与场景的材质槽相同 |
| `source` | `{document_id, object_id, slot_id, revision}` | 来源说明，只供展示，不构成引用 |

- 对象角色：`thumbnail`，256 px 的材质球 PNG，必须有。
- 持有关系：新表 `creative_content_revision_refs(account_id, holder_revision_id, content_revision_id, role)`，主键为前三列。在写入持有方修订的同一事务里写入，之后不修改。它加入 `creativereference.LiveOwnerCondition`：只要持有方修订的状态不是 `deleted`，被引用的修订就是活的。`role` 取 `texture`。
- GC：`material` 加入可回收种类。资产被彻底删除且没有其他持有方时，按现有规则回收它的修订和缩略图，再下一轮回收不再被持有的图片。
- 去重：资产库已有 `slot` 完全相同的材质资产时直接复用，不新建。

### `api: 材质存入资产库`（ADD）

`POST /creative/assets/from-scene-material`

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `document_id` | string | 是 | 场景 |
| `object_id` | string | 是 | 物件 |
| `slot_id` | string | 是 | 物件的有效槽位 |
| `expected_object_rev` | CreativeRevision | 是 | 物件内容单元的预期版本 |
| `target` | `CreativeUploadAssetTarget` | 是 | 标题（必填）、描述、分组、标签、收藏，同 `from-scene-model` |
| `thumbnail_upload_id` | string | 是 | 暂存的 PNG 缩略图 |

- 成功返回 201 和资产（与 `from-scene-model` 相同）；复用已有资产时返回 200。
- 错误：
  - 槽位没有分配材质：400 `creative_material_slot_empty`；
  - 物件内容单元版本不符：409 `creative_document_conflict`；
  - 场景在回收站：409 `creative_state_conflict`；
  - 暂存不可用：409 `creative_upload_unavailable`；
  - 图片已不可用：400 `creative_image_unavailable`。

### `patterns: 场景着色写法`（MODIFY）

- 「命中点不做凹凸」改为：「程序化预设的凹凸命中点不做；贴图的法线在命中点照做，TBN 按三角形求，采样一律带 `.grad()`」。
- 补一条：材质贴图在光栅和命中点共用 `materials/maps.ts` 的映射函数。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/platform/store/migrations/0081_creative_material_textures.{up,down}.sql` | 新增 | `material` 种类；`creative_content_revision_refs`；down 在存在 material 行时拒绝回滚 |
| `backend/internal/platform/store/*_migration_test.go`、`store_test.go` | 修改 | 迁移标签与回滚链 |
| `backend/internal/creativescene/model.go:MaterialSlot`、`validateSlots`、`encodeScene` | 修改 | `maps`/`mapping` 外形与补齐；`Refs = modelRefs ∪ imageRefs`；`maxSceneImages` |
| `backend/internal/creativescene/images.go` | 新增 | `imageRefs`、`verifyImages`（参照 `verifyExisting`）、`readImages`、`ErrImageUnavailable` |
| `backend/internal/creativescene/type.go:Apply` | 修改 | 执行操作后调用 `verifyImages` |
| `backend/internal/creativescene/view.go` | 修改 | 读取场景时返回 `images` |
| `backend/internal/creativescene/materials.go:checkMaterial`、`resolveSlots` | 修改 | 助手写入时校验 `maps` 与 `mapping` 的键 |
| `backend/internal/creativescene/agent.go` | 修改 | `read_scene` 带贴图、映射、无 UV 提示和灯的 `shadow` 状态；`AgentEditInTx` 透传 |
| `backend/internal/creativescene/library.go` | 修改 | `SaveMaterialToLibrary`；模型库副本写持有关系 |
| `backend/internal/creativecontent/content.go`、`model_facts.go` | 修改 | `Payload.Material`、`validate`、`WriteMaterialAndRetain`、持有关系写入、材质简介 |
| `backend/internal/creativereference/projection.go:LiveOwnerCondition`、`projection_roles_test.go` | 修改 | 新持有方及守卫登记 |
| `backend/internal/creativemedia/gc.go`、`agent.go:AgentImage` | 修改 | `material` 可回收；助手看材质用缩略图 |
| `backend/internal/creativelibrary/list.go`、`discovery.go`、`service.go` | 修改 | 种类清单、简介、去重 |
| `backend/internal/creativeagent/scene_tools.go`、`context.go`、`asset_tool_info.go` | 修改 | 工具说明；写贴图前确认可读；`readableRevisionInTx` 加「经持有方可读」分支；`read_asset` 支持材质 |
| `backend/internal/platform/httpapi/creative_media.go` | 修改 | `from-scene-material` 路由 |
| `api/openapi.yaml` | 修改 | 材质槽字段、`images`、材质载荷、新接口、种类枚举；`make generate` |
| `frontend/src/creative-scene/materials/images.ts` | 新增 | 图片缓存、ORM 合成、`loadSceneImages` |
| `frontend/src/creative-scene/materials/maps.ts` | 新增 | 三平面与 UV 映射、法线混合，光栅与命中点共用 |
| `frontend/src/creative-scene/materials/instances.ts:keyOf`、`build` | 修改 | 贴图进键；有贴图时构建节点 |
| `frontend/src/creative-scene/materials/resolve.ts`、`principled.ts`、`catalog.ts`、`slotEdits.ts` | 修改 | 槽位带 `maps`/`mapping`/`alpha_mode`；`renderPolicy` 取代 `seeThrough`；编辑器写入 |
| `frontend/src/creative-scene/objects/SlotMesh.tsx`、`render/trace/collect.ts`、`render/sceneRenderer.ts` | 修改 | 分片绘制、世界空间层收集、AO 预渲染改读 `renderPolicy` |
| `frontend/src/creative-scene/panels/MaterialSlots.tsx`、`Properties.tsx`、`AssetPanel.tsx`、`ContextMenu.tsx` | 修改 | 贴图输入、映射设置、存为材质资产、拖放材质、灯光阴影状态 |
| `frontend/src/creative-scene/objects/shadowBudget.ts` | 新增 | `selectShadowCasters` |
| `frontend/src/creative-scene/objects/lights.ts`、`Lighting.tsx` | 修改 | 按选取结果设置 `castShadow` |
| `frontend/src/creative-scene/host/render.ts:openShooter` | 修改 | 出图前等图片；`SCENE_IMAGES_FAILED` |
| `frontend/src/creative-scene/library/thumbnail.ts` | 修改 | 材质球缩略图；等图片 |
| `frontend/src/creative-scene/preview/ModelPreview.tsx` | 修改 | 等图片；材质预览 |
| `frontend/src/creative-scene/SceneEditor.tsx`、`api.ts` | 修改 | 存为材质资产、拖放材质 |
| `frontend/src/creative-scene/render/trace/maps.ts` | 新增 | `TraceMaps` 纹理数组 |
| `frontend/src/creative-scene/render/trace/tables.ts`、`hit.ts`、`base.ts`、`layer.ts`、`pool.ts`、`build.worker.ts`、`cache.ts`、`checks.ts` | 修改 | 材质行 10 个 vec4；顶点附加缓冲（颜色加 UV）；命中点采样；就绪；e2e 对照 |
| `frontend/src/creative-scene/render/gpu.ts` | 修改 | 申请 `maxTextureArrayLayers` |
| `frontend/src/creative-canvas/assets/*`、`editor/assetPlacement.ts`、`editor/LibraryTiles.tsx`、`editor/MediaView.tsx`、`viewer/MediaViewer.tsx`、`viewer/AssetInfoPanel.tsx` | 修改 | 材质资产卡片、筛选、查看浮窗；不能放到画布上 |
| `.lti/contracts/patterns.md` | 修改（归档时随 delta） | 命中点贴图法线 |

## 验证与交付

- **后端**：`make check-go`，覆盖 `creativescene`、`creativecontent`、`creativelibrary`、`creativereference`、`creativemedia`、`creativeagent`、`platform/store`。
  - 校验：`maps` 用在预设上、非法键、`channel` 写在基础色上、`size_m` 越界、`uv` 模式带 `size_m`，都返回 400 并指出字段。
  - 超过 64 张图片返回 413 `creative_size_limit`。
  - 归属：引用别的账号的图片、视频或模型，都返回 400 `creative_image_unavailable`（不用 404，编辑器会把 404 当作文档已不存在）。
  - GC：
    - 场景引用的图片，原节点和资产都删除后不被回收；场景彻底删除后被回收。
    - 材质资产彻底删除后，缩略图和不再被持有的图片在后续轮次被回收。
    - 守卫测试登记新列。
  - 迁移：前进、回滚；存在 material 行时拒绝回滚。
  - 助手：
    - 写入不可读的图片时，工具结果写明原因；
    - 原图从画布和资产库删掉、只被材质资产持有时，助手读到材质后仍能把它用到槽位上；撤销这张图片或这个材质的授权后，写入被拒绝；
    - `read_scene` 带贴图和 `shadow` 状态；
    - `search_assets kind:material` 能找到材质。
- **前端单元**：
  - `images.ts` 的引用计数与释放、ORM 合成与「标准 ORM 直接复用」；
  - 同一张图同时用作基础色和糙度：得到两个纹理，颜色空间各自正确；交换加载顺序结果相同；
  - 同一组源图、通道配置不同的两个 ORM 得到不同的 `TextureKey` 和不同的层；
  - `renderPolicy` 的决策表逐行覆盖，`opaque` 的结果与原 `seeThrough` 逐项相同；
  - `keyOf` 只换图片时结构键不变；只切 `alpha_mode` 时键改变；
  - UV 模式旋转 90° 时，法线贴图的凹凸方向随贴图旋转（光栅与命中点各验一次）；
  - 代理简化：非线性 UV 的平面简化后 UV 误差不超上限，超限时保留原几何；
  - `selectShadowCasters` 的选取顺序与成本；
  - 材质行 10 个 vec4 的写入。
- **e2e**（逐个运行，换端口，见 [[targeted-verification-only]]）：
  - 五种贴图在材质预览、最终渲染、`render_view`、`lit` 导出中都生效；白模、线稿与上线前逐像素相同。
  - 透明裁切：树叶面片在最终渲染里镂空，投下的阴影也镂空，在反射和探针里同样镂空；`blend` 的透明度随贴图变化；白模、线稿不受影响。
  - 裁切次数用完：六层镂空面片后面放一面实墙，反射、探针和阴影都不漏光（不出现天空色，灯光判为被遮挡）。
  - `blend` 且 `opacity = 1` 的物件不进世界空间层；`clip` 且 `opacity < 1` 的物件按不透明进入。
  - 三平面投影：物件缩放后贴图尺寸不变。UV 模式：没有 UV 的模型显示提示。
  - 离屏出图时图片加载失败，这张图失败并写明原因。
  - 最坏场景（太阳 + 选满的投影灯 + 面光 + 玻璃 + 三张贴图）：在本机 Metal 上统计材质片元 WGSL 的 `texture_*` 声明 ≤ 16，画面不黑。第 4 盏投影平行光落选，不再黑屏。
  - 世界空间层：反射命中点逐像素对照光栅（`checks.ts`）；两次导出逐像素相同；层号用完时有提示。
  - 材质资产：存入、复用、拖到另一个场景的槽位，渲染一致。
- **owner 验收**：反射与探针 GI 里贴图的效果，看对照图（镜面地板反射贴图墙、探针照亮的彩色贴图房间）。
- **合并**：主检出 `git merge --no-commit` 后跑 `make check-merge`。迁移 0081 接在 0080 之后。如果 `cg-composite` 或其他 Epic 先合入了迁移，编号顺延。

## 未决事项

| # | 问题 / 假设 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| 1 | 三平面法线的表面梯度混合在斜面上的接缝 | 光栅与命中点的法线 | 任务开头用测试场景对照 Blender 的 Box 投影 | 法线任务开始前 |
| 2 | 每个设备的纹理字节预算；图片上限 64、长边 2048、数组每层 512²/1024² 的显存与加载时间 | 视口、导出 | 第一项任务实测参考场景（视口与离屏同时存在）的峰值，定下字节预算并回写本文件 | 贴图加载任务开始前 |
| 3 | 阴影的最终方案（阴影图集或虚拟阴影贴图） | 表面阴影 | 另立 change，owner 决定开工时机 | 不阻塞本次 |
| 4 | 世界空间 GI Epic 的 U6 修订 | 那个 Epic 的设计记录 | 本设计批准后，起草那个 Epic 的修订，指向本设计 | C5c1 实施前 |
| 5 | three r186 的阴影 pass 是否沿用材质的 alpha test | `clip` 的阴影 | 第一项任务在 WebGPU 和 WebGL2 两个后端上验证，不支持就用 `castShadowNode` 补 | 透明任务开始前 |
