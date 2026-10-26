# Change 详细设计

## 继承的设计

- **Epic** `scene-light-size` 批准版本 v1（2026-10-26）。本次用到的条目：
  - 「领域模型与业务规则」的 I1–I4：判定标准的依据；
  - 「架构设计」「技术选型」「关键机制」：穿刺验证的对象；
  - 「未决事项」U1–U8：S0 要回答的问题；
  - roadmap 的 S0 行和 10 组穿刺清单。
- **research**：Epic 的 `discovery/research/light-size-current-and-blender.md`。
- **沿用 GI-S0（`scene-trace-spike`）的做法**：
  - 参考场景 JSON 和 `referenceScene`；
  - 基线用例 `SCENE_E2E_BASELINE` 和 `exportReference`；
  - 只在开发服务器里存在的穿刺页面；
  - 原型结束时 `git mv` 进 discovery；
  - 证据超过 30 MB 时取舍的规则。
- **复用的产品模块**（只 import，不修改）：
  - GI-S1 的光追底座 `render/trace/`：`base.ts`、`hit.ts` 的命中点着色、`wgsl.ts` 的 `traceFirst` / `traceWithin`；
  - `render/trace/checks.ts` 的 `hitCheck`：逐像素对照光栅和命中点的 `direct` / `directUnshadowed`。
- **lesson**：
  - `webgpu-perf-warm-shader-cache`：热、冷着色器缓存分开测；
  - `three-rebuild-materials-by-lights`：灯的组合不变时，three 不重建材质；
  - `epic-revise-sync-planning-root`：Epic 修订在主检出的 `develop` 上做。

本次不涉及领域模型，不改产品代码、契约、场景格式和依赖。

## 关键机制

### 参考场景

两个场景都只用现有字段，产品编辑器能直接打开，地面都是 `ground@1`。

- **`frame-two-lights`**：复制 owner 2026-10-26 反馈的原始场景，即开发库画布 `cccv_3fa2de7c-8ccc-4f79-8cb1-84a91f0bafe0` 上的场景节点「参考还原｜工业科幻反应舱」，文档 `cwdoc_98de9f98-d5ca-4943-a05e-60a084801d34`。
  - **原样复制**：环境（纯色天空、环境光 0.65）、渲染设置（`realtime_gi`、曝光 +0.5）、14 个基础体、摄像机「参考构图｜低机位」，以及两盏灯：
    - 「左上冷白主光」：**平行光**，强度 2.5，投影。所以它本来就不衰减，与 Blender 的 Sun 相同；
    - 「右下橙色反光」：点光，强度 38 cd，`decay` 2，`distance` 15 m，投影。
  - **两个模型实例**：「管线、环形舱门与桁架」和「高墙延伸与前景斜撑」，是挡出明暗带的主要几何。取出它们内容修订里的几何代码，作为代码模型放进固定数据，走 e2e 现有的代码模型路径（staged upload → committed revision → GLB）载入。这条路走不通的话，退回存成 GLB 固定数据，在任务 1.1 里定。
- **`occluder-wall`**：
  - 一面 6 m 宽的白墙；
  - 三块 0.4 m 见方的遮挡板，离墙分别 0.05、0.5、3 m；
  - 墙前 1 m 处有一个直径 0.5 m 的球体，用来看包裹效果；
  - 三盏灯，聚光、点光和平行光，各自独立开关，由穿刺页面用 URL 参数选择；
  - 中性灰材质，粗糙度 0.6；另有一块粗糙度 0.15 的板，用来看高光。

### 穿刺页面

`frontend/spike-light.html` 加 `frontend/spike/light/`，只在开发服务器上存在，`vite build` 的产物不包含它。

| 参数 | 取值 | 作用 |
|---|---|---|
| `scene` | `frame-two-lights` / `occluder-wall` / `area-studio` | 载入参考场景；`area-studio` 是页面里写死的面光源用例（见「局部设计与偏差」） |
| `size` | 数值 | 光源半径（m），或平行光的角径（°） |
| `method` | `pcf` / `pcf-radius` / `pcss` / `smtrace` / `vsm-pcss` / `msm-pcss` / `mip-pcss` / `jitter` / `reference` | 候选的软阴影做法，加上参考解 |
| `engine` | `workbench` / `realtime_gi` | 同产品的两种渲染器 |
| `backend` | `webgpu` / `webgl` | 第 3 组的后端对照 |
| `cold` | `1` | 冷缓存测量：给着色器加一个每次加载都不同的常量 |

- **有尺寸的灯光**：原型用页面自己的灯光子类和光源节点，对应 Epic 架构图里的 `SceneSphereLight` 等。它们经 `renderer.library.addLight` 注册，`PhysicalLightingModel` 的包装也只在页面里装，不改 `render/`。
- **切换做法时整页重载**，避免 three 沿用旧着色器（lesson `three-rebuild-materials-by-lights`）。

### 参考解

- **主光线**：每个像素从摄像机发一条主光线，用 `traceFirst` 求出命中点，材质从材质表读取。
- **直接光**：在每盏光源的表面上均匀采样一个点：
  - 球：在可见的球冠上均匀取点；
  - 圆盘：在角径范围内的圆锥里均匀取方向；
  - 矩形、椭圆：在发光面上均匀取点。

  每个采样点按 `hit.ts` 的 BRDF 和光源的辐亮度算出贡献，再用 `traceWithin` 追一条阴影光线判定是否可见。
- **累积**：随机数取帧序号，逐帧做算术平均，累积 1024 帧。AC-S0-3 用第 1024 帧和第 2048 帧的差异来判断是否收敛。
- **输出**：
  - `direct`：带阴影；
  - `directUnshadowed`：不带阴影；
  - `visibility`：每盏灯可见的比例，只用来测量半影。

  与光栅比较时，光栅那边也只画直接光（`hitCheck` 的 `direct` 模式），把环境光和 GI 排除在外。
- **辐亮度换算**：
  - 球：`L = I / (π r²)`，所以远处的照度是 `I/d²`（I1 的第 1 种变化）；
  - 圆盘：按照度 / 立体角换算；
  - 面光源：直接用亮度。
- **适用范围**：参考解只在 WebGPU 上能跑。第 3 组的 WebGL2 对照只比候选做法自己，不比参考解。

### 测量与判定

- **半影宽度**：在 `occluder-wall` 的墙上，沿垂直于阴影边缘的扫描线读取 `visibility`，取 10% 到 90% 之间的距离（米）。三块遮挡板分开记录。
- **误差**：在受光区域的掩码内，对 `direct` 的亮度求平均绝对误差、95 分位和最大误差。掩码去掉主光线没有命中的像素，以及法线背向全部光源的像素。
- **包裹**：在球体上测明暗交界线的位置，即 `directUnshadowed` 降到峰值 1% 处的极角，分别与参考解和 Blender 对照。
- **高光**：在 0.15 粗糙度的板上，测高光区域的面积和总能量（亮度之和），与参考解对照（AC-4）。
- **颗粒**：同一帧，在半影区域里求每像素的局部方差。`workbench` 测单帧；`realtime_gi` 测收敛后的帧。
- **耗时**：每盏灯的阴影滤波耗时，用加上该做法和不加时的 GPU 时间差来算。
- **判定**：每种做法给出「可行 / 有条件可行 / 不可行」和对照数据，阈值由 S0 提出、owner 看图确认。一种做法只要在 `workbench` 下颗粒明显，或在多层遮挡下误差明显偏大，就标为「有条件可行」，并写明条件。

### Blender Cycles 对照（后续验收项）

本机没有 Blender。owner 2026-10-26 决定：这一组记为后续验收项，由 owner 以后在另一台机器上做；这一轮由 owner 自己看效果。S0 只交付对照的做法和预期值，不阻塞归档。U8 先按 Epic 暂定的「点光和聚光的半径不随缩放」推进，后续验收发现不一致时，经 Epic 修订调整。

- 在 Blender 4.x 里照 `occluder-wall` 搭同样的几何和材质，三盏灯分别渲染。
- 只比较相对量，不换算绝对单位：
  - 同一盏点光，半径取 0、0.1、0.5 m 时，墙上远处的照度比；
  - 半影宽度；
  - 球体上明暗交界线的位置；
  - 高光的面积比。

  这样不受 Blender W 与 three cd 换算的影响。
- 同时核对 U8：把点光物件缩放到 2 倍，看半影宽度是否变化。
- S0 交付 `blender-cycles.md`，内容包括：
  - 场景的几何、材质和灯光参数；
  - 搭建步骤；
  - Cycles 的渲染设置；
  - 要读的量和读法；
  - 参考解给出的预期值。
- owner 渲染后，把 `.blend` 文件和渲染图补进 `discovery/research/blender/`。
- Epic 整体验收时，这一项按 `lti epic verify --method deferred` 记为后续跟进。

### 结论与 Epic 修订

- 每组写一篇 research，放在 `discovery/research/`：
  - `reference.md`（第 1 组）；
  - `soft-shadow-candidates.md`（第 2 组，附对比图集，交 owner 看图）；
  - `depth-read-backends.md`（第 3 组）；
  - `integration.md`（第 4 组）；
  - `area-shadow.md`（第 5 组）；
  - `contact-shadow.md`（第 6 组）；
  - `sphere-horizon-sheen.md`（第 7 组）；
  - `blender-cycles.md`（第 8 组）；
  - `defaults-look.md`（第 9 组）；
  - `thresholds.md`（第 10 组）。
- 修订草稿写回 Epic 的 design（U1–U8、关键机制、技术选型）和 proposal（AC 阈值）。在主检出的 `develop` 上执行 `lti epic revise`。

## 局部设计与偏差

1. **面光源棚拍场景只写成穿刺页面里的用例**：Epic roadmap 写的是 S0 提交三个参考场景。但现有格式还没有 `area` 类型，存成固定数据的话，产品编辑器会拒绝它。所以 S0 只提交两个，面光源棚拍场景在 S2 有了类型后再存成固定数据。这是 S0 内部的取舍，不改 Epic 的交付边界。
2. **新场景单独列为 `lightReferenceNames`**：放进 `referenceNames` 的话，R0 和 W1 要与存档基线逐张比对，就得重跑。
3. **参考解只在 WebGPU 上跑**：光追底座只在 WebGPU 上运行（`scene-world-space-gi` 的非目标）。
4. **Blender 对照改为后续验收项**（owner 2026-10-26）：Epic roadmap 把它放在 S0 第 8 组。S0 只交付对照的做法和预期值，S0 结束时的 Epic 修订把它移进整体验收的后续跟进。

## 接口与数据契约

不变。本次不交付 Epic 契约，不新增局部契约。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/scripts/creative-scene.e2e.mjs:lightReferenceNames` | 新增 | `frame-two-lights`、`occluder-wall`；「每个参考场景都能打开」的检查和 `SCENE_E2E_BASELINE` 用例也遍历它 |
| `frontend/scripts/fixtures/scene-reference/` | 新增 | `frame-two-lights.json`、`occluder-wall.json` |
| `frontend/spike-light.html` | 临时新增 | 穿刺页面入口，S0 结束时移进 discovery |
| `frontend/spike/light/` | 临时新增 | 原型、参考解和测量脚本，S0 结束时移进 discovery |

## 验证与交付

- **AC-S0-1**：定向运行 `SCENE_E2E_ONLY="reference scenes"`，换端口，避开 owner 的 Vite；两个新场景在两种显示下都没有报错。
- **AC-S0-2**：在参考设备上运行 `SCENE_E2E_ONLY=baseline: SCENE_E2E_BASELINE=<S0>/discovery/research/baseline/`。
- **AC-S0-3**：穿刺页面的测量脚本输出第 1024 帧和第 2048 帧的差异；Blender 的对照写进 `blender-cycles.md`。
- **AC-S0-4、AC-S0-5**：逐组核对 research 是否有结论、数据和图；对比图集交 owner 看图。
- **AC-S0-6**：最后一个实现提交后满足以下各项：
  - `git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空；
  - `frontend/spike*` 不存在；
  - 前端静态检查通过。
- **合回 `develop`**：只涉及 e2e 脚本、固定数据和文档，按 `check-merge` 的判定表只需前端静态检查。

## 未决事项

无。两项都已由 owner 在 2026-10-26 回答：
- O1：原始场景在画布 `cccv_3fa2de7c-8ccc-4f79-8cb1-84a91f0bafe0` 上，见「参考场景」。
- O2：本机不装 Blender，对照记为后续验收项，见「Blender Cycles 对照」。
