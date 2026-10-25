# P1 合并 part：验收证据

2026-10-25，分支 `feat/scene-batch-merge`。

- **参考设备**：Mac16,8（Apple M4 Pro），macOS 26.5，Chromium 145（Playwright 自带），Metal 后端。视口 1600 × 1000，2× 像素。
- **测法**：
  - 帧时取热着色器缓存：每种显示模式先转 1 秒视角，再测 4 秒（lesson `webgpu-perf-warm-shader-cache`）。
  - 每帧 draw 数、三角面和 CPU 毫秒来自 `SceneRenderer.lastFrame`。
  - 「合并前」是同一构建加 `?nomerge=1`，每个网格片单独画，与合并前的行为相同。

原始数据：`perf/p1-perf.json`、`parity/p1-parity.json`。并排图（合并前、合并后、差值 ×8）在 `parity/` 和 `perf/` 下。

## AC-1 多部件模型

场景：4 个实例，每个 1000 个网格片、2 个槽位（`scene-stress.mjs:multiPart`）。

| 显示模式 | 合并前：帧时均值 / p95 | 合并前：draw/帧 | 合并后：帧时均值 / p95 | 合并后：draw/帧 |
|---|---|---|---|---|
| 白模 | 16.2 / 18.5 ms | 8005 | 8.3 / 10.1 ms | 21 |
| 材质预览 | 24.0 / 26.7 ms | 8005 | 8.3 / 9.9 ms | 21 |
| 最终渲染（工作台） | 63.4 / 74.7 ms | 20006 | 8.3 / 10.0 ms | 46 |
| 最终渲染（实时 GI） | 81.7 / 85.0 ms | 20016 | 9.3 / 16.6 ms | 56 |

四种模式都在 16.7 ms 以内，白模每帧 21 次 draw，满足 AC-1。8.3 ms 是显示器 120 Hz 的上限。合并前的数字与排查时一致（白模 16 ms、工作台 63 ms、GI 84 ms）。

## AC-2 画面等价

对照场景是 `scene-stress.mjs:equivalence`：

- **A 组**：没有任何两个表面处在同一深度。包括同一槽位重叠的三块玻璃、缩放 2.5 倍的大理石程序化材质、受太阳光照的 L 形墙。
- **B 组**：贴合堆叠、立在地面上的盒子，边对边的地砖，穿过方块的圆杆。

| 对照 | 后端 | 判据 | 结果 |
|---|---|---|---|
| A 组导出 `clay`、`line`、`lit`（工作台） | SwiftShader | 逐像素相同 | 3/3 相同 |
| B 组导出 `clay`、`line`、`lit`（工作台） | SwiftShader | 迁移等价阈值 | 3/3 逐像素相同 |
| 视口：A 组选中描边（白模）、线框；B 组同上 | SwiftShader | `line` 阈值 | 4/4 逐像素相同 |
| A、B 组导出 `lit`（实时 GI） | Metal | `lit` 阈值 | 2/2 逐像素相同 |
| A 组玻璃区域（实时 GI，101802 像素） | Metal | `lit` 阈值 | 逐像素相同 |
| AC-4：合并后同一场景再导出一次，6 张 | SwiftShader | 逐像素相同 | 6/6 相同 |

- **实施中的发现**：第一版对照场景里，L 形墙的两段墙体互相穿插。线稿导出在转角交线上差了 2 个像素（均值 0.00018，差 > 8 的占 0.0003%，在 `line` 阈值内）。
  - **原因**：交线上两个零件的深度相同，谁显示取决于绘制顺序，合并前后顺序不同。
  - **核实**：同一构建、同一状态导出两次逐像素相同，所以差异来自合并，不是 SwiftShader 的不确定。
  - **处理**：穿插、贴地与共面接触同属「同深度表面」，归入 B 组；A 组改为互不接触。判据没有放宽（`lti revise` 已记录）。B 组这次也逐像素相同：深度恰好相等的采样很少，碰上的概率取决于镜头。
- **待 owner 看图**：`parity/p1-*.jpg` 每张依次是合并前、合并后、差值 ×8。

## AC-5 基线与规模场景（Epic AC-3）

规模场景（`scaleScene`）的组成：
- 30 个代码模型，每个 20–300 个网格片、1–3 个槽位；
- 2000 个实例，其中 43% 带槽位覆盖；
- 300 个基础体；
- 实例共 411 万面；
- 投影灯：太阳、2 盏聚光、1 盏点光。

按 Epic 的估算规则，共 4060 个单元。

| 显示模式 | 合并前：帧时均值 | 合并前：draw/帧 | 合并后：帧时均值 / p95 | 合并后：draw/帧 | 合并后：CPU |
|---|---|---|---|---|---|
| 白模 | 260 ms | 59913 | 12.6 / 18.0 ms | 2650 | 12.4 ms |
| 材质预览 | 393 ms | 59913 | 15.6 / 18.2 ms | 2666 | 15.2 ms |
| 最终渲染（工作台） | 10.5 s | 636347 | 157 / 167 ms | 16275 | 152 ms |
| 最终渲染（实时 GI） | 14.0 s | 637560 | 215 / 277 ms | 16285 | 208 ms |

多基础体场景（1000 个相同方块）合并前后相同（以下写作「合并前 / 合并后」）：白模 8.3 / 8.3 ms，工作台 17.9 / 18.5 ms，实时 GI 23.7 / 24.4 ms。基础体不参与合并，共享基础体几何已挪到 P4。

**合并后工作台每帧 152 ms 花在哪**（`perf/scale-workbench-profile.txt`，3 秒 CPU profile）：
- `fitCascades` 占约 30%，约 55 ms/帧，主要是逐个网格片变换包围盒（`Box3.applyMatrix4`），场景里约 20 万个网格片。合并前也要处理同样多的盒子，开销相同，只是以前淹没在每帧 10 秒里。
- 其余是阴影通道里逐个对象的投影和绘制：4 级级联、2 盏聚光、点光 6 个面。

**对 Epic AC-3 阈值的建议**（Epic U5）：
- 白模 ≤ 16.7 ms 在 P1 之后就已达到（12.6 ms），比 Epic 估算的约 40 ms 好。原因是视锥剔除，每帧实际只画 2650 次。
- 最终渲染 ≤ 33 ms 还差得多（157 / 215 ms）。
- 建议阈值暂不修订，待 P4、P5 实测后再定。可以落到这两期的手段：
  1. **P5**：灯光方向和物件矩阵不变时，复用上一帧变换好的网格片包围盒。结果逐值相同，不需要 owner 看图准入，可省约 55 ms。它与 P5「投影物签名」用的是同一次遍历。
  2. **P4**：实例化，减少阴影通道里的 draw 和逐对象开销。
  3. **P5**：阴影缓存。静止或只转镜头时，聚光和点光的阴影不重画。

## 上限模型（任务 2.8）

模型：28.8 万面，2000 个球，2 个槽位（`capModel`）。在 Node 里测（与页面主线程是同一个 V8），取 20 次的中位数。

| | 合并前 | 合并后 |
|---|---|---|
| `partsOf` 耗时 | 26.3 ms | 37.2 ms（多出的约 11 ms 是一次性的合并） |
| part 数 | 2000 | 2 |
| 一次点选（吸附建 BVH 之前） | 0.14 ms | 0.10 ms（按网格片剔除） |
| 一次点选，不按网格片剔除 | — | 10.4 ms |

评审提的风险确实存在：合并后直接用 three 默认拾取，一次点选要测整个槽位的三角形，要 10.4 ms。按网格片剔除后回到合并前的水平。

## 规模场景的材质覆盖比例

2026-10-25 从开发库统计：15 个场景、127 个模型实例，其中 55 个带槽位覆盖（43%）；201 个基础体里 68 个设了材质。这是 owner 自用的数据，样本小。P4 设计分组时，如果有种子摄影师的数据，应重新统计。

## AC-3 交互回归（任务 3.3）

只跑与模型、选择、放置、幽灵、预览、缩略图和导出相关的现有用例（SwiftShader，`SCENE_E2E_ONLY` 逐条过滤）。

| 用例 | 结果 |
|---|---|
| C6 save to library（带材质的卡片缩略图） | 通过 |
| asset panel: Shift+A 从资产库添加… | 通过 |
| C6 drag in（拖入资产的幽灵，吸附到方块顶面） | 通过 |
| C6 copy semantics | 通过 |
| selection edits: box select, H / Alt+H, Shift+D, X | 通过 |
| the move tool drags the selection | 通过 |
| a code model: 代码模型… then 生成 | 通过 |
| a parameter change rebakes into a new revision | 通过 |
| a model that cannot be shown is a grey block | 通过 |
| AC-1: Z switches the shading, Shift+Z goes to wireframe | 通过 |
| export: two cameras × clay and line | 通过 |
| device loss: the asset card thumbnail | 通过 |
| 资产页 `C6-model-preview`（资产 3D 预览，`creative-assets.e2e.mjs`） | 通过 |
| the assistant’s changes are outlined and labelled for a minute | **失败，develop 上同样失败** |
| a model the assistant is adding stands as a placeholder | **失败，develop 上同样失败** |

最后两条在 `develop`（`a342fa90`）的临时 worktree 上用同样方式重跑，失败位置和信息相同：
- 「the chair is outlined in the assistant’s colour」；
- 「the placeholder is drawn (0 px)」。

它们用的是基础体方块和加载中的占位框，不经过模型合并，所以是本分支之前就有的问题，与 P1 无关，留给单独的修复。
