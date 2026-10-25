# Tasks

## 1. 压力场景与每帧统计

- [x] 1.1 新增 `frontend/scripts/scene-stress.mjs`，提供 `multiPart`、`primitives`、`scaleScene`、`equivalence` 四类场景；同一输入两次生成的 GLB 逐字节相同，全部通过 `inspect()`。统计开发库现有场景里带槽位覆盖的模型实例占比，作为 `scaleScene` 的 `overrides` 默认值（统计不到取 30% 并注明）。单测断言确定性和规模（面数、实例数、网格片数、覆盖比例）
- [x] 1.2 `SceneRenderer.render` 记录 `lastFrame`（draw 数、三角面、CPU 毫秒），取 `render()` 前后的差值，不切换 `info.autoReset`；`gpuTestHooks.lastFrame` 在测试下随帧更新；单测断言两次渲染之间的差值正确
- [ ] 1.3 e2e fixture 支持 `?nomerge=1`（设 `__sceneNoMerge`）；`SCENE_E2E_PERF` 新增压力场景帧时检查：Metal、四种显示模式，记录帧时均值、p95 和每帧 draw 数到 `perf.json`。先在合并前跑一遍，把基线记进 evidence

## 2. 合并网格片

- [x] 2.1 先写单测（红）：`mergeParts` 的分组键（槽位、hint、常量、是否有索引、几何签名含 `gpuType`、变形属性和 `morphTargetsRelative`）、组顺序与组内顺序、网格片首尾相接且覆盖全部、包围球和包围盒与成员几何 `computeBoundingSphere` / `computeBoundingBox` 的结果按位相同、`mergeGeometries` 返回 null 时该组退回不合并、克隆的几何 `piecesOf` 查不到、`__sceneNoMerge` 下与现在相同
- [x] 2.2 实现 `models/merge.ts` 的 `mergeParts` 和 `render/pieces.ts` 的登记与 `piecesOf`，`partsOf` 改为调用它，2.1 转绿；`disposeParts` 先释放范围视图，再释放合并几何
- [x] 2.3 `render/pieces.ts` 补 `extractPiece`、`pieceViews`、`disposePieceViews`、`expandByPieces`；单测断言范围视图共用 attribute 和 index、`drawRange` 与包围体正确、按合并几何缓存、登记为自己那一项
- [x] 2.4 `ModelMesh` 按决策表绘制：半透明或透射的槽位、幽灵模型用范围视图，其余用合并几何；`ModelPreview`、`renderMaterialThumbnail` 同样按决策表
- [x] 2.5 `Outline`、`edgesOf` 有网格片时按网格片生成，外壳登记同样区间的网格片；单测断言外壳三角形和边线集合与「每个网格片各自计算」逐项相等（含贴合堆叠的盒子）
- [x] 2.6 `accelerateModels` 改用 `indirect` BVH，并跳过不在 0 号图层的对象；单测断言建 BVH 后 index 不变、同一组射线对合并几何和范围视图的命中与合并前相同、描边外壳仍是 `noRaycast` 且没有建树
- [x] 2.7 `fitCascades` 有网格片时逐网格片读包围盒；单测：L 形墙加太阳的场景，同一镜头下合并前后每级灯的位置和正交范围逐值相等
- [ ] 2.8 视口取景改用 `expandByPieces`，点选给合并几何用 `raycastPieces`；单测：旋转过的多部件模型取景范围与合并前逐值相等，点选命中的物件和距离与合并前相同；上限模型（30 万面）的合并耗时和点选耗时，合并前后记进 evidence

## 3. 画面等价与交互回归

- [ ] 3.1 默认套件新增等价对照检查（SwiftShader）：`equivalence` 场景在 `?nomerge=1` 与默认下各导出 `clay`、`line`、`lit`（工作台），A 组断言逐像素相同，B 组用 `compareShots` 和 `parityLimits` 比较；截选中描边和线框模式的视口，按 `line` 阈值比较；玻璃区域单独比较；并排图写进 evidence
- [ ] 3.2 `SCENE_E2E_PERF` 的 Metal 检查补实时 GI 的 `lit` 对照，以及多部件场景的 AC-1 断言（帧时均值 ≤ 16.7 ms，白模 draw ≤ 40）；记录合并后的规模场景数字和「确认或修订 Epic AC-3」的建议
- [ ] 3.3 定向回归现有 e2e 中与模型、选择、框选、吸附放置、取景、G/R/S、幽灵、资产预览、缩略图、导出确定性相关的用例（`SCENE_E2E_ONLY`），全部通过

## 4. 工具说明

- [ ] 4.1 `scene_tools_test.go` 先加断言（红），再给 `scene_tools.go` 的 `write_model`、`edit_scene` 说明各加一句成本模型（绿）；同步 `docs/dev/agent-skills-tools.md` 的场景工具表
