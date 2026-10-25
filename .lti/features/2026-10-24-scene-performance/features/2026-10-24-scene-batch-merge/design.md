# Change 详细设计

## 继承的设计

- **Epic**：`scene-performance` 基线 v1。适用条目：
  - design：「设计上下文」「领域模型与业务规则」（网格片、渲染批次、I1–I5）、「架构设计」「技术选型」「关键机制」（合并 part、每帧统计）、「已知限制」「验证与交付」；
  - 交付 `patterns: 场景渲染批次`。
- **依据**：排查数据见 Epic 的 `discovery/research/drawcall-diagnosis.md`。
- **本次边界**：合并、统计、压力场景、等价对照、工具说明。共享基础体几何和实例化（P4，owner 2026-10-25）、负载估算、阴影缓存不在本次。

## 领域模型与业务规则

沿用 Epic 的「网格片」「渲染批次」。本次新增的内部类型：

```ts
// render/pieces.ts
/** 一个网格片在合并几何里的位置：有索引时是 index 的区间，没有索引时是顶点区间；包围体在模型空间。 */
export type Piece = { start: number; count: number; sphere: Sphere; box: Box3 }
/** 登记过的几何（合并几何、范围视图、按网格片拼成的描边外壳）返回它的网格片；其他几何，包括它们的克隆，返回 undefined，按整个几何处理。 */
export function piecesOf(geometry: BufferGeometry): readonly Piece[] | undefined
```

- **pieces 只存一份**：在 `render/pieces.ts` 模块内的 `WeakMap<BufferGeometry, readonly Piece[]>` 里登记。`ModelMeshPart` 不加字段，仍是 `{geometry, slot, hint, constants}`。
  - 不挂 `geometry.userData`：`BufferGeometry.copy` 共享 `userData` 的引用（three r186 `BufferGeometry.js:1456`），克隆出来的几何会带着已经不对的区间。用 WeakMap，克隆查不到，自然退回按整个几何处理。
- **pieces 的规则**：按拼接顺序排列，区间首尾相接，覆盖整个几何。只有一个成员的组不登记，按整个几何处理。
- **包围体的算法不能换**：合并前，在每个成员几何上调用 three 的 `computeBoundingSphere()`、`computeBoundingBox()`（包围盒中心加最大距离），直接取这两个对象。three 给透明物体排序用的是 `geometry.boundingSphere.center`（r186 `Renderer.js:3298-3305`），换一种算法排序就可能变。单测按位比较。
- **范围视图**：给某个网格片用的 `BufferGeometry`。
  - 与合并几何共用同一组 attribute 和 index 对象，所以也是同一份 GPU 缓冲；
  - `drawRange` 设为该网格片的区间；
  - `boundingSphere` 和 `boundingBox` 直接取该网格片的包围体，不重算：重算会覆盖全部顶点；
  - 登记为只含它自己那一项（区间不变）。
- **范围视图的生命周期**：按合并几何懒建并缓存（`WeakMap<BufferGeometry, BufferGeometry[]>`），只跟随模型缓存释放：先释放范围视图，再释放合并几何，从不单独释放。原因是单独释放可能带走共用的 GPU 缓冲。
- **什么时候按网格片画**（决策表）：

  | 情况 | 画什么 |
  |---|---|
  | 白模，或解析出的材质不透明 | 合并几何，每个槽位一次 draw |
  | 解析出的材质半透明或透射（`materials/instances.ts:isSeeThrough`） | 每个网格片的范围视图 |
  | 幽灵模型（`ghostMaterial`） | 每个网格片的范围视图 |
  | 线框模式 | 合并几何（不绘制、只供拾取），边线按网格片生成 |

  决策由调用方做（它们本来就知道材质和是否幽灵），`render/pieces.ts` 只提供 `pieceViews(geometry)`。

## 模块职责与协作

主流程（视口；离屏截图和导出相同）：

1. `ModelCache.load(revision)` → `partsOf(parseGLB)` → `mergeParts`，得到合并后的 part，网格片登记在 `render/pieces.ts`；
2. `ModelMesh` 按决策表决定每组画合并几何还是范围视图；
3. `SceneRenderer.render` 画各个 pass，记录 `lastFrame`；
4. e2e 经 `gpuTestHooks.lastFrame` 读取统计。

| 模块 | 本次职责 |
|---|---|
| 新模块 `models/merge.ts` | 纯函数：分组键、`mergeGeometries`、合并失败时回退、算包围体并登记网格片 |
| 新模块 `render/pieces.ts` | 只依赖 three：`Piece`、登记与 `piecesOf`、`extractPiece`、`pieceViews` / `disposePieceViews`、`expandByPieces` |
| `models/cache.ts` | 下载、解析、缓存不变；`partsOf` 调 `mergeParts`；`disposeParts` 的释放顺序 |
| `objects/ModelMesh.tsx`、`preview/ModelPreview.tsx`、`library/thumbnail.ts` | 按决策表画合并几何或 `pieceViews` |
| `objects/Outline.tsx`、`objects/wireframe.ts` | 有网格片时按网格片取出、分别计算再拼接 |
| `placement/raycast.ts` | 吸附用 indirect BVH、跳过辅助图层；点选按网格片剔除（`raycastPieces`） |
| `render/sunShadow.ts` | 级联拟合逐网格片读包围盒 |
| `Viewport.tsx` | 取景用 `expandByPieces`；测试钩子写 `lastFrame` |
| `render/sceneRenderer.ts` | `lastFrame` |

`render/` 目前不依赖 `models/`。网格片登记放在 `render/pieces.ts`，级联拟合不必反向引用模型模块；`models/merge.ts` 依赖它登记。

## 关键机制

### 合并（`mergeParts`）

```
meshes ← partsOf 遍历 GLB 场景树里的 Mesh（现有）
for each mesh: 烘焙世界变换、镜像时翻转绕序（现有），算出 constants
groups ← 按分组键分组，保持首次出现顺序
for each group:
  if 只有 1 个成员: 原几何，不登记
  else:
    每个成员 computeBoundingSphere / computeBoundingBox，记下 start/count（按拼接后的偏移）
    merged = mergeGeometries(成员几何, false)
    if merged 为 null: 这一组退回不合并，每个成员各自成一个 part
    else: 登记网格片；释放成员几何
return { meshes: groups 映射为 ModelMeshPart }
```

- **分组键**：槽位、hint、constants 全部字段、是否有索引，加上几何签名。几何签名按名称排序后拼接：
  - 每个属性的 `name:itemSize:normalized:数组类型:gpuType`；
  - 每个变形属性的名称、个数和同样的属性签名；
  - `morphTargetsRelative`。

  这些都是 r186 `mergeGeometries` / `mergeAttributes` 会校验的项，签名不同就分到不同组。交错属性不用单独分组：`mergeAttributes` 会把它们拆开拷贝（`BufferGeometryUtils.js:385`）。
- **失败回退**：签名已经覆盖校验项，正常不会失败。万一 `mergeGeometries` 返回 null（以后外部 GLB 导入也走这里），这一组退回不合并，画面照旧，只是少了合并的收益。
- **只给 e2e 用的开关**：`globalThis.__sceneNoMerge === true` 时每个网格片单独成组，与现在的行为相同。只有 e2e fixture 页面会设它（`?nomerge=1`），产品里没有入口。

### 范围视图和网格片的消费方

three 的绘制、`Mesh.raycast` 都按 `drawRange` 处理，`three-mesh-bvh` 构建时也按 `drawRange`（`geometryUtils.js:63`）。要单独处理的有两类。

读整个几何的计算，先按区间取出网格片再算：

| 消费方 | 做法 |
|---|---|
| `Outline`（外壳几何由 `objects/wireframe.ts:shellOf` 生成，便于单测） | `piecesOf(geometry)` 为空时照旧处理。否则对每个网格片用 `extractPiece` 取出独立几何（共用 attribute，index 取该区间的拷贝；无索引时拷贝顶点区间），分别 `toCreasedNormals(·, π)`，再 `mergeGeometries` 成一个外壳，并登记网格片（外壳自己的区间，原网格片的包围盒）。结果与合并前「每个网格片各一个外壳」的三角形完全相同。无索引几何只传位置给 `toCreasedNormals`：它会把法线写回传入的几何，原来的写法会改掉模型自身的法线 |
| `edgesOf`（线框） | 同上，每个网格片分别 `EdgesGeometry(·, 30°)`，再拼接成一个 `BufferGeometry`。贴合网格片之间的边线照常显示 |
| `accelerateModels` | `new MeshBVH(geometry, { indirect: true })`：indirect 模式排的是自己的间接缓冲，既不重排 index，也不补 index（`GeometryBVH.js:145-154`）。同时跳过不在 0 号图层上的对象（选中描边外壳、边线），它们原本是 `noRaycast`，现在会被换成 `acceleratedRaycast` 并单独建树 |

原来逐 mesh 读包围体或三角形的地方，改为逐网格片：

| 消费方 | 做法 |
|---|---|
| `fitCascades`（`render/sunShadow.ts:125-131`） | 有网格片时把每个网格片的 `box` 放进投影物列表，否则照旧用几何包围盒。列表与合并前逐 mesh 的盒子相同，级联的尺寸、texel、normalBias 和深度范围也就相同。遍历次数和现在一样 |
| 取景（`Viewport.tsx` 的 `frame`） | `expandByPieces(bounds, object)`：与 `Box3.expandByObject` 的非精确模式相同，只是有网格片的几何逐个网格片取盒。物件旋转时，合并盒变换后的轴对齐盒比各网格片的并集大，不这样做取景会偏松 |
| 点选（`Viewport.tsx` 的 `objectAt`） | 模型的 mesh 都用 `raycastPieces`（经 `SlotMesh`、`WireMesh` 新增的可选 `raycast` 属性传入；没有网格片的几何等同 three 默认）：逐网格片用 three 默认的 `Mesh.raycast` 测它的范围视图（先测包围球、包围盒，再测区间内的三角形），命中的 `object` 改回原 mesh。开销与合并前相同，不需要先建 BVH。吸附建好 BVH 后照旧换成 `acceleratedRaycast` |

`SceneRenderer.compile` 的替身按「顶点布局 + 材质 + 标志」区分种类，合并几何和范围视图的顶点布局不变，不用改。

### 每帧统计

`SceneRenderer.render` 在 `built.pipeline.render()` 前后取 `renderer.info.render.drawCalls` 和 `triangles` 的差值，用 `performance.now()` 计时，写入只读的 `lastFrame: {draws, triangles, cpuMs}`。

不用切换 `info.autoReset`：r186 只在 Animation 循环每次 rAF 开头重置统计（`Animation.js:81`），`render()` 中途不会清零。

`Viewport.tsx` 的 `LookView` 在 `sceneTest` 下把它写到 `gpuTestHooks.lastFrame`。

### 压力场景生成器

新文件 `frontend/scripts/scene-stress.mjs`，在 Node 里运行：
- 用 three 的 `GLTFExporter` 加最小的 `FileReader` 补丁写 GLB，再用 `sandbox/inspect.ts` 的 `inspect()` 算 manifest，与排查时的做法相同。
- 生成是确定性的：固定种子，同样的输入得到逐字节相同的 GLB。
- 提供四类场景：
  - `multiPart({parts, slots})`：网格片铺在网格上，各用自己的几何；
  - `primitives(n)`；
  - `scaleScene({overrides})`：
    - 30 个模型，每个 20–300 个网格片，形状由盒子、圆柱、球、车削体混合；
    - 2000 个实例，其中 `overrides` 比例的实例带槽位材质覆盖；
    - 300 个基础体；
    - 总面数不超过 500 万；
    - 投影灯：太阳、2 盏聚光、1 盏点光。
  - `equivalence()`：对照场景，分两组。
    - A 组没有任何两个表面处在同一深度：零件之间、零件与地面之间都不接触、不穿插，相隔 1–2 cm。包括同一槽位互相重叠的三块玻璃板（`principled@1`，opacity 0.4）、缩放 2.5 倍的程序化材质模型、受太阳光照的 L 形墙（两段墙同一槽位，合并盒会盖住内角的空地）；
    - B 组是会出现同深度表面的情况：贴合堆叠、立在地面上的盒子，边对边的地砖，穿过方块的圆杆。两个表面深度相同时谁显示，合并前取决于逐物件的深度排序，合并后取决于拼接顺序。
    - 实施中的发现：两段墙互相穿插时，交线上两个零件深度相同，线稿导出差了 2 个像素（均值 0.00018，在 `line` 阈值内）。穿插和贴地与共面接触是同一种情况，所以都放进 B 组。
- `overrides` 的默认值：实施时统计开发库现有场景里带槽位覆盖的模型实例占比，写进 evidence。统计不到时取 30%，并在 evidence 里注明是假设。

## 局部设计与偏差

- **范围视图用 `drawRange`，不用 index 切片**：与 Epic 契约一致，显存零增量。代价是描边、线框边线要先取出网格片，吸附加速要用 indirect。
- **吸附加速一律改用 `indirect` BVH**：未合并的几何也受影响，但只是不再重排 index，命中结果相同。
- **点选不走 BVH**：评审建议点选时也按需加速。`accelerateModels` 遍历整个场景建树，第一次点选会卡住，所以改为按网格片剔除，开销回到合并前的水平。
- **不加** Epic 未定义的东西：网格片到命名部件（`part:<id>`）的映射，留给按部件点选落地时。

## 接口与数据契约

- **状态**：扩展。交付 `patterns: 场景渲染批次`（Epic 基线 v1）。
- 场景文档、模型内容、GLB、工具入参和结果都不变。
- 工具说明加的句子（定稿以实现为准，要点不变）：
  - `write_model`：「视口流畅度取决于场景里的物件数和每个模型的材质槽数，与三角面关系不大：同一槽位的零件渲染时会自动合并，不必为了性能减少分段数或零件。」
  - `edit_scene`：「成批的相同小件（栅栏、砖、书本）写成一个代码模型（lib.array/radial），比逐个加基础体流畅得多；重复出现的东西放同一模型的多个实例。」

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/src/creative-scene/models/merge.ts` | 新增 | `mergeParts`：分组键、合并、失败回退、登记网格片；e2e 开关 |
| `frontend/src/creative-scene/models/cache.ts:partsOf` | 修改 | 调 `mergeParts` |
| `frontend/src/creative-scene/models/cache.ts:disposeParts` | 修改 | 先释放范围视图，再释放合并几何 |
| `frontend/src/creative-scene/render/pieces.ts` | 新增 | `Piece`、`piecesOf`、`extractPiece`、`pieceViews`、`disposePieceViews`、`expandByPieces` |
| `frontend/src/creative-scene/objects/ModelMesh.tsx:ModelMesh` | 修改 | 按决策表画合并几何或范围视图（材质槽位路径和幽灵路径） |
| `frontend/src/creative-scene/objects/Outline.tsx:Outline` | 修改 | 外壳改用 `shellOf` |
| `frontend/src/creative-scene/objects/SlotMesh.tsx:SlotMesh` | 修改（清单外补充） | 可选的 `raycast` 属性，模型的 mesh 按网格片点选 |
| `frontend/src/creative-scene/objects/WireMesh.tsx:WireMesh` | 修改（清单外补充） | 同上，线框模式下的点选 |
| `frontend/src/creative-scene/objects/wireframe.ts:edgesOf` | 修改 | 有网格片时按网格片生成边线 |
| `frontend/src/creative-scene/objects/wireframe.ts:shellOf` | 新增 | 选中描边外壳的几何，按网格片生成 |
| `frontend/src/creative-scene/placement/raycast.ts:accelerateModels` | 修改 | `indirect: true`；跳过不在 0 号图层的对象 |
| `frontend/src/creative-scene/placement/raycast.ts:raycastPieces` | 新增 | 点选按网格片剔除 |
| `frontend/src/creative-scene/render/sunShadow.ts:fitCascades` | 修改 | 逐网格片读包围盒 |
| `frontend/src/creative-scene/Viewport.tsx` | 修改 | `frame` 用 `expandByPieces`；`LookView` 测试下写 `gpuTestHooks.lastFrame` |
| `frontend/src/creative-scene/preview/ModelPreview.tsx:Meshes` | 修改 | 按决策表画 |
| `frontend/src/creative-scene/library/thumbnail.ts:renderMaterialThumbnail` | 修改 | 按决策表画 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.render` | 修改 | `lastFrame` |
| `frontend/src/creative-scene/render/gpu.ts:gpuTestHooks` | 修改 | 加 `lastFrame` |
| `frontend/scripts/scene-stress.mjs` | 新增 | 压力场景、对照场景与上限模型（`capModel`）生成器 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | fixture 支持 `?nomerge=1`；等价对照检查；`SCENE_E2E_PERF` 的压力场景帧时检查 |
| `frontend/scripts/creative-scene-model.test.ts` | 修改 | 合并分组、顺序、网格片、范围视图、描边和边线的等价性单测 |
| `frontend/scripts/creative-scene-shadow.test.ts` | 修改 | 级联逐值相等（L 形墙） |
| `frontend/scripts/creative-scene-snap.test.ts` | 修改 | 点选和吸附命中与合并前相同、描边外壳不被加速 |
| `frontend/scripts/creative-scene-gpu.test.ts` | 修改 | three 只在动画循环开头重置统计、`measure` 的差值 |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | `write_model`、`edit_scene` 说明各加一句 |
| `backend/internal/creativeagent/scene_tools_test.go` | 修改 | 断言新句子 |
| `docs/dev/agent-skills-tools.md` | 修改 | 场景工具表注明 P1 的说明变化 |

## 验证与交付

- **AC-1 帧时**：
  - 在 `SCENE_E2E_PERF` 下用 Metal 跑多部件场景的四种显示模式（实时 GI 另起一个场景页面），断言帧时均值不超过 16.7 ms、白模每帧 draw 数不超过 40。
  - 用 `?nomerge=1` 再跑一遍，作为同一次运行里的基线。
  - 数字取热着色器缓存，记入 evidence 的 `perf.json`。
- **AC-2 画面等价**：
  - 默认套件里新增一条检查（SwiftShader）：对照场景在 `?nomerge=1` 和默认下各导出一次 `clay`、`line`、`lit`（工作台）。A 组断言逐像素相同；B 组用 `compareShots` 和 `parityLimits` 比较。同一构建、同一状态导出两次逐像素相同（已核实 SwiftShader 确定），所以 A 组的任何差异都来自合并。视口的选中描边和线框模式截图按 `line` 阈值比较。玻璃区域裁出来单独比。
  - 单测：L 形墙加太阳的场景，同一镜头下 `fitCascades` 在合并前后算出的每级灯位置和正交范围逐值相等。
  - `SCENE_E2E_PERF` 的 Metal 检查里，补实时 GI 的 `lit` 对照（阈值）。
  - 并排图存进 evidence，交 owner 看。
- **AC-3 交互**：
  - 定向跑现有 e2e 里与模型、选择、放置、幽灵相关的用例（`SCENE_E2E_ONLY` 过滤）；
  - 单测：`raycastPieces` 和范围视图对同一组射线的命中与合并前相同；吸附在 indirect BVH 下的命中与现在相同；描边外壳在吸附加速后仍是 `noRaycast`；
  - 单测：旋转过的多部件模型，`expandByPieces` 的结果与合并前 `expandByObject` 逐值相等。
- **AC-4 确定性**：沿用现有的「两次导出逐像素相同」用例。
- **AC-5 基线**：
  - 规模场景合并前后的数字记入 evidence，在归档时附上「确认或修订 Epic AC-3」的建议；
  - 上限模型（30 万面）的合并耗时和一次点选的耗时，合并前后都记入 evidence。
- **AC-6 工具说明**：`go test -run` 跑 `scene_tools_test.go` 里相关的用例。
- **回滚**：纯前端加工具说明的文字，回退代码即可，没有数据残留。
- **发布**：工具说明改了检查点注册摘要，按「排空后升级」。
- **跑哪些测试**：按 `targeted-verification`，只跑上面列出的定向用例；合并回 `develop` 时跑 `make check-merge`。

## 未决事项

无。规模场景的阈值（Epic U5）由本次的基线提供依据，在 P4 开始前确认，不阻塞本次交付。
