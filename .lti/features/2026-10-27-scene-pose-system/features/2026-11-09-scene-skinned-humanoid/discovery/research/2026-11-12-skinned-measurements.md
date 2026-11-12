# 带骨骼人形的实测与截图（2026-11-12，任务 6.3）

本文记录 AC-8 的重新收敛耗时、接近上限的导入角色的加载与静态化耗时，以及交 owner 看图的 AC-1、AC-2、AC-6 截图。截图与原始计时在 `skinned-6.3/`。

- **机器**：Apple M4 Pro（Mac16,8），darwin 26.5，Chromium 145.0.7632.6（Playwright，Metal 后端）。
- **测量脚本**：`frontend/scripts/creative-scene.e2e.mjs` 的「skinned 6.3 evidence…」。只在有素体文件且设了 `SCENE_E2E_EVIDENCE` 时运行：
  `SCENE_E2E_ONLY='skinned 6.3' SCENE_E2E_EVIDENCE=<目录> node scripts/creative-scene.e2e.mjs`
- 计时原始数据：`skinned-6.3/skinned-timings.json`。

## AC-8：实时 GI 下换姿态

场景：实时 GI、程序化天空、地面、身后一面镜子；女素体与男素体各一个，从「机位」看（28 mm）。只给女素体换姿态，用 `__scenePose.set`。

| 姿态 | 进入追踪层 | 收敛到可导出 | 说明 |
|---|---|---|---|
| 双臂放下（arms-down） | 67 ms | 418 ms | 首次出现的姿态 |
| 左臂前伸（reach） | 50 ms | 425 ms | 首次出现的姿态 |
| 再次双臂放下 | 17 ms | 425 ms | 命中静态化缓存 |

- **「进入追踪层」**：从调用 `__scenePose.set` 到追踪层的 `geometryVersion` 增加且没有待建段。它包括 CPU 蒙皮、静态化几何、worker 里这一段的重建和上传，其他段不动。
- **「收敛到可导出」**：从调用到视口累积满 52 帧（`viewportSettle`），也就是 `render_view` 开始累积所需的状态。时间主要花在累积帧上，与姿态无关。
- **分项（主线程，9 次的中位数）**：

  | 项目 | 耗时 |
  |---|---|
  | 求姿态（标准骨骼 → 骨骼矩阵） | 0.1 ms |
  | CPU 蒙皮与静态化几何（8,844 顶点、65 关节） | 0.7 ms |
  | 拾取用 BVH | 2.7 ms |

  BVH 只在第一次拾取时才建（`placement/raycast.ts`），不在换姿态的路径上。
- 一次 `lit` 导出（1024×683）：静止姿势 3.26 s，左臂前伸 3.28 s，两者相同。

结论：素体换姿态时，追踪层在 4 帧左右换上新姿态，之后的耗时就是正常的 GI 累积。换姿态本身不是瓶颈。

## 接近上限的导入角色

把女素体的身体网格复制 148 份，作为一个蒙皮网格，用导入的 `writeGLB` 写出，带 52 根标准骨骼的映射：

- 文件 105.7 MB（上限 200 MB）。
- 1,896,176 个三角形（上限 200 万），1,091,648 个顶点，65 个关节。

| 项目 | 耗时 |
|---|---|
| 写出 GLB（导入的最后一步） | 557 ms |
| `ModelCache` 加载（解析、分出骨架模板） | 317 ms |
| 求姿态 | 0.1 ms |
| CPU 蒙皮与静态化几何 | 96 ms |
| 拾取用 BVH（第一次拾取时） | 468 ms |

- 这一档的角色每换一次姿态，主线程约花 0.1 s 做静态化；画面上能感觉到一下停顿，但不会卡住。
- 追踪层的段重建在 worker 里进行，没有单独测。它用 SAH 建树，估计不少于上面 BVH 的 0.5 s。重建期间视口按 Epic 的规则把这个角色从追踪层剔除，导出则等它完成。
- 子项 4 做拖动调姿时，如果要支持这种体量，可以只做 refit，或者在拖动中跳过静态化。这一点留给子项 4 的设计。

## 本次的优化：静态化几何

第一次测量时，素体的静态化需要 2.9 ms，大部分时间花在逐顶点调用 three 的 `Matrix4` 和 `getComponent` 上。`rig/skin.ts:staticGeometry` 改为：

- 第一次用到一个几何时，把位置、法线、切线、关节、权重读成普通数组并缓存；
- 关节矩阵放进一个 `Float32Array`；
- 逐顶点直接混合 3×4 矩阵；
- 法线用混合矩阵的余子式（即逆转置乘以行列式），行列式为负时翻转，然后归一化。

与旧版逐顶点对照：位置和切线完全一致，法线最多差 0.022°（浮点舍入）。

- 素体：2.9 ms → 0.7 ms。
- 接近上限的角色：96 ms。旧版按比例估算约 0.4 s，没有实测。
- 单测 `rig-skin`、`import-rig` 和 e2e「skinned AC-6」都通过。

## 截图

### AC-1：内置素体（`body-*`）

| 文件 | 内容 |
|---|---|
| `body-preview.png` | 材质预览 |
| `body-rendered-rest.png` | 实时 GI，静止姿势；身后镜中可见两个素体的背面 |
| `body-rendered-arms-down.png` | 实时 GI，女素体双臂放下 |
| `body-rendered-reach.png` | 实时 GI，女素体左臂前伸；镜中和地面阴影随之改变 |
| `body-lit-rest.jpg` | `lit` 导出，静止姿势 |
| `body-lit-reach.jpg` | `lit` 导出，左臂前伸；与视口一致 |

### AC-2：导入 VRM 1.0 样例（`vrm1-*`）

| 文件 | 内容 |
|---|---|
| `vrm1-thumbnail.png` | 资产库缩略图，带贴图 |
| `vrm1-material-preview.png` | 放进场景后的材质预览，贴地站立 |
| `vrm1-白模.png` | 白模显示 |
| `vrm1-线框.png` | 线框显示 |

### AC-6：三处的手部位置一致（`ac6-*`）

这是 e2e 用来比对位置的标记画面，不是好看的图：

- 场景全黑，手上挂一个自发光的红色方块；
- 按方块在屏幕上的像素位置，比对 `lit` 导出（a）、实时 GI 镜中（b）和视口。

| 姿态 | 导出 | 镜中 | 视口 |
|---|---|---|---|
| 静止 | `ac6-skinned-rest-a.jpg` | `ac6-skinned-rest-b.jpg` | `ac6-skinned-rest-viewport.png` |
| 抬手 | `ac6-skinned-raised-a.jpg` | `ac6-skinned-raised-b.jpg` | `ac6-skinned-raised-viewport.png` |

两种姿态下，三处的位置在 e2e 容差内一致。抬手后，方块在导出中从 (569, 340) 移到 (449, 345)，在视口中落在期望位置 (351, 285)。
