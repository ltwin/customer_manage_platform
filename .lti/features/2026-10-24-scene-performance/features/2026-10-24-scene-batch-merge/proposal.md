---
risk:
  factors:
    - broad-impact
    - perf-sensitive
behavior: changed
reads:
  - creative/scene
  - creative/library
decisionsPending: false
---
# 本次交付

## Why

本子项是 Epic `scene-performance` 基线 v1 的 P1，也是第一个子项。它承担 Epic 的 AC-1（多部件模型）和 AC-2（画质不降）中合并的部分，并为 AC-3 提供压力场景和基线。

- **先做的理由**（roadmap「排序原则」）：
  - 改动小，收益是数量级的：排查时 4000 个网格片的场景，在 GLB 里预先合并后，实时 GI 从 84 ms 降到 8.7 ms。
  - 现有场景不用改任何东西就能受益。
  - 后面所有子项都要用到这里的压力场景和每帧统计。
- **画质优先**（owner 2026-10-24）：合并按「本应相同」验收，不留例外。为此在 Epic 设计里加了网格片范围视图：半透明槽位、幽灵模型、选中描边、线框边线都保持合并前的网格片粒度。

## What Changes

按 Epic design「关键机制」实现，本次交付 `patterns: 场景渲染批次`。

1. **合并网格片**（design「合并 part」）：
   - `models/cache.ts` 的 `partsOf` 按分组键合并，记录每个网格片的范围和包围球。
   - 视口、离屏截图、导出、资产缩略图、资产 3D 预览都经由它，一起生效。
2. **按网格片绘制和读取的地方**：
   - 半透明或透射的槽位、幽灵模型，按网格片绘制范围视图，排序和合并前相同。
   - 选中描边的外壳、线框边线按网格片生成再拼接，接缝处与合并前相同。
   - 按包围盒工作的地方逐个网格片取盒：太阳级联拟合（`render/sunShadow.ts:fitCascades`）、视口取景（`Viewport.tsx` 的 `frame`）。合并后一个槽位只剩一个大包围盒，不这样做，级联会变大、阴影变软，取景也会偏松。
   - 点选先按网格片的包围体剔除，再测区间内的三角形，开销与合并前相同。吸附的结果与合并前相同。
   - 顺手修一个现有问题：吸附加速会把选中描边外壳的 `noRaycast` 换成 BVH 拾取，并为它单独建树。改为跳过辅助图层上的对象。
3. **每帧统计**：
   - `SceneRenderer` 记录 `lastFrame`：本帧的 draw 数、三角面、`render()` 的 CPU 毫秒。
   - 这一项经 e2e 测试钩子读取，P2 的统计叠加层也用它。
4. **压力场景与性能门禁**：
   - 用 Node 里的确定性生成器写 GLB，场景包括：
     - 多部件：4 个实例，每个 1000 个网格片、2 个槽位；
     - 多基础体：1000 个，P1 只记基线（共享几何和实例化在 P4）；
     - 规模场景：AC-3。一部分实例带材质覆盖，比例取自现有场景的统计（P4 的实例化分组会被覆盖打散，比例不真实，AC-3 就失去意义）。
   - 可选开启的帧时门禁 `SCENE_E2E_PERF` 加入这三个场景，记录每个显示模式的帧时均值、p95 和每帧 draw 数。
   - 记录合并前的基线和合并后的结果。
5. **画面等价对照**：
   - 只给 e2e fixture 用的开关可以关掉合并，同一份构建里出「合并前」和「合并后」两组图。
   - 不透明路径合并前后画的是同样的三角形、同样的着色器，SwiftShader 下导出又是确定的，所以先断言逐像素相同，阈值只做兜底。
   - 导出对照：`clay`、`line`、`lit`（工作台和实时 GI）。
   - 视口对照：选中描边、线框模式。
   - 对照场景要包括同一槽位互相重叠的玻璃、缩放过的程序化材质物件、受太阳光照的 L 形墙。
6. **工具说明**：`write_model` 和 `edit_scene` 的说明各加一句成本模型（design「接口设计」），同步 `docs/dev/agent-skills-tools.md`。

**不做**：
- 共享基础体几何、自动实例化（P4，owner 2026-10-25）；
- 负载估算和统计叠加层（P2）；
- 阴影缓存（P5）；
- 任何渲染 pass 或着色的改动。

## Acceptance Criteria

参考设备为 Apple M4 Pro，视口 1600 × 1000、2× 像素，Metal 后端。

- **AC-1 多部件模型**：
  - **WHEN** 场景里有 4 个代码模型实例，每个 1000 个网格片、2 个槽位，镜头运动时，**THEN** 白模、材质预览、最终渲染（工作台和实时 GI）的视口帧时都不超过 16.7 ms，并且每帧 draw 数与网格片数无关（白模不超过 40 次）。
  - 改前基线：白模 16 ms、工作台最终渲染 63 ms、实时 GI 84 ms。
- **AC-2 画面等价**：
  - **WHEN** 对照场景 A 组（没有任何两个表面处在同一深度：零件之间、零件与地面之间不接触、不穿插）合并前后各导出一次 `clay`、`line`、工作台 `lit`，**THEN** 逐像素相同。
  - **WHEN** B 组（贴合堆叠的盒子、边对边的地砖、穿插的零件）做同样对照，以及两组在实时 GI 下导出 `lit`，**THEN** 差异在迁移等价阈值内：
    - `line`：平均差 ≤ 0.1，差 > 8 的像素 ≤ 0.05%；
    - `clay`、`lit`：平均差 ≤ 1.5，差 > 8 的像素 ≤ 1%。
  - B 组只可能在同深度的表面处有差别（贴合、穿插的交线、贴地）：两个面深度相同时谁在上，合并前取决于逐物件的深度排序，合并后取决于拼接顺序。
  - **WHEN** 对比合并前后的视口截图，**THEN** 选中描边和线框模式在 `line` 阈值内。
  - **WHEN** 同一场景、同一镜头，**THEN** 合并前后每级太阳级联灯的位置和正交范围逐值相等（单测，场景含 L 形墙）。
  - 场景里有同一槽位互相重叠的玻璃，玻璃区域单独统计，同样满足上面的判据。
  - owner 看并排图，确认肉眼看不出差别。
- **AC-3 交互不变**：点选、框选、吸附放置、G/R/S、取景、选中描边、线框模式、助手高亮、拖入资产的幽灵，在多部件模型和玻璃槽位上的行为与合并前相同；取景框住的范围逐值相同（单测）。现有的场景 e2e 中，与模型、选择、放置相关的用例都要通过。
- **AC-4 确定性**：同一场景两次导出逐像素相同（沿用现有检查）。
- **AC-5 基线**：
  - 规模场景（Epic AC-3）在合并前后的帧时和每帧 draw 数都记录进 evidence，作为确认或修订 Epic AC-3 阈值的依据（Epic U5）。
  - 上限模型（30 万面）的合并耗时（主线程）和一次点选的耗时，合并前后都记录进 evidence。
- **AC-6 工具说明**：`write_model` 和 `edit_scene` 的说明包含成本模型那一句（有单测断言），`docs/dev/agent-skills-tools.md` 同步。

## Impact

- **前端**（`frontend/src/creative-scene/`）：
  - 新增 `models/merge.ts`（分组合并）、`render/pieces.ts`（网格片登记、取出、范围视图、包围盒）；`models/cache.ts` 只改调用和释放顺序。
  - `objects/`：`ModelMesh.tsx`、`Outline.tsx`、`wireframe.ts`。
  - `placement/raycast.ts`：吸附用 indirect BVH、跳过辅助图层；点选按网格片剔除。
  - `library/thumbnail.ts`、`preview/ModelPreview.tsx`：与视口用同一条「按网格片还是合并」的规则。
  - `render/sunShadow.ts`：级联拟合读网格片包围盒。
  - `Viewport.tsx`：取景读网格片包围盒；测试钩子写 `lastFrame`。
  - `render/sceneRenderer.ts`：`lastFrame`。
  - `render/gpu.ts`：测试钩子。
- **测试**：
  - `frontend/scripts/` 新增压力场景生成器；
  - `creative-scene.e2e.mjs` 加对照和性能检查；
  - 单测加进 `creative-scene-model.test.ts` 等现有文件。
- **后端**：`backend/internal/creativeagent/scene_tools.go` 两段说明，`scene_tools_test.go` 加断言。
- **契约**：交付 `patterns: 场景渲染批次`；规格 `creative/scene` 新增一条需求（见 specs）。

## Risk Notes

- **广影响**：所有模型的绘制都改走合并路径，包括导出、缩略图和资产预览。AC-2 的对照和 AC-3 的现有 e2e 是主要防线；关掉合并的测试开关只给 e2e 用，产品里没有入口。
- **性能证据**：
  - 帧时要在 Metal 上实测，SwiftShader 只用来跑等价对照。
  - 按 lesson `webgpu-perf-warm-shader-cache`，帧时取热缓存数字，并注明是哪一组。
- **工具说明改动会改变检查点注册摘要**（`creativeagent/checkpoint.go` 把工具定义算进摘要）：发布后，活动中的运行不能跨这次发布续跑。沿用「排空后升级」，与以往改工具说明时相同。
- **一项局部实现决定**（不改 Epic 契约）：范围视图共用合并几何的 attribute 和 index，只设 `drawRange` 和包围体。three 的绘制、默认拾取和 `MeshBVH` 的构建都按 `drawRange` 处理；但 `EdgesGeometry`、`toCreasedNormals` 不看 `drawRange`，所以描边和线框先按范围取出网格片再计算。`MeshBVH` 的非 indirect 模式会就地重排 index、打乱区间，所以吸附加速改用 indirect（design「范围视图的消费方」）。
- **按包围盒工作的地方**：太阳级联拟合和视口取景原来逐 mesh 读几何包围盒，合并后必须改为逐网格片读，否则画面和取景会变（评审 B1）。这类消费方在 design 里逐一列出，并有逐值相等的单测。
