# Change 详细设计

## 继承的设计

- Epic `scene-webgpu-renderer` 批准版本 v1（2026-10-21）。本次用到的条目：
  - 「验证与交付」：参考场景集、参考设备、基线和 e2e 的 WebGPU 环境；
  - 「技术选型」和「关键机制」中帧推进、渲染管线组成、实时 GI、收敛与累积、输出分辨率、读回各节：穿刺验证的对象；
  - 「未决事项」1、3、4、5、6、8、9：R0 要回答的问题；
  - roadmap 的 R0 行和 7 组穿刺清单。
- 现有场景 e2e 的做法沿用：`frontend/scripts/creative-scene.e2e.mjs` 的 `SceneServer` 模拟后端，`runExport` 走导出对话框取上传的 PNG，C5 的 AC-9 性能用例 `orbitFrames` / `frameStats` 测帧时间，`SCENE_E2E_ONLY` 过滤用例。
- discovery 的先例：C5 的 `discovery/research/lit-repaint.md` 加同名目录存图和脚本。
- 本子项没有 brainstorm 和 research 前置。

本次不涉及领域模型，不改产品代码、契约和场景格式。

## 关键机制

### 参考场景

六组参考场景存成完整的场景文档 JSON（`data: 场景文档` 的 `environment`、`render`、`objects`），放在 `frontend/scripts/fixtures/scene-reference/`：

| 文件 | 内容 | 导出机位 | 主要服务 |
|---|---|---|---|
| `noon.json` | C5 校准的 `yard`：白箱、混凝土墙、木箱、金属球；太阳高度 55°，EV 0 | 一台 50mm 平视机位 | AC-1 |
| `dusk.json` | 同上，太阳高度 5°，EV +2 | 同上 | AC-1 |
| `room.json` | C5 校准的 `room`：6 × 4 m 房间，两盏 40 cd 壁灯，深色天空 | 一台 28mm 机位 | AC-1 |
| `materials.json` | 15 个方块：14 种预设和默认参数的原理化材质，各用默认参数；再加一个 `transmission = 1` 的原理化球 | C5 AC-2 用例的俯视「样张」机位 | AC-1，材质迁移 |
| `dof.json` | C4 的 `exportScene(true)`：8 m 外对焦的方块、40 m 外的塔，F1.4 | 机位 A（景深）、机位 B（2:3 竖幅） | AC-1，景深 |
| `cornell.json` | 盒子间：4 × 3 × 3 m，左墙红色、右墙绿色的 `paint@1`，白地面，`principled@1` 光滑地板（粗糙度 0.1），金属球（`metal@1`，`rust = 0`），白箱；顶部一盏面向下的聚光，深色天空 | 一台 35mm 正对开口的机位 | AC-4 实时 GI |

- 前四组的内容照搬现有用例的构造代码，第一次由脚本生成 JSON 后手工核对，之后 JSON 是唯一来源。现有用例继续用自己的构造函数，不改。
- e2e 新增 `referenceScene(server, name)`：读 JSON，替换 `server.scene`，为每个物件登记单元修订号，与现有 `seed` 的做法相同。
- 穿刺页面直接 `import` 同一份 JSON，所以两条管线画的是同一份场景。

### 基线用例

新增 `SCENE_E2E_BASELINE=<dir>` 时运行的用例，浏览器参数与 AC-9 性能用例相同（`--use-angle=metal --ignore-gpu-blocklist --enable-gpu`），视口 1600 × 1000，2 倍像素比：

1. 对每组场景：用 `?export=1` 打开，切到最终渲染（导出对话框因此勾选三种图），走 `runExport` 的同一路径导出，记录从点击「导出」到上传完成的耗时，存 `<dir>/<场景>-<机位>-<pass>.png`。
2. 每组再切白模、按 `1` 进入前视正交，截视口 `<dir>/<场景>-ortho-clay.png`，作为正交 AO 的基线。
3. 写 `<dir>/baseline.json`：设备型号、操作系统、Chromium 版本、`UNMASKED_RENDERER_WEBGL`、每张图的尺寸和导出耗时。
4. AC-9 性能用例在设了 `SCENE_E2E_BASELINE` 时，把四种显示模式的帧时间和加灯卡顿写进 `<dir>/perf.json`。`SCENE_E2E_ONLY` 按用例名的子串过滤，所以基线和性能分两次运行。

导出路径是用户实际使用的路径，所以耗时包含离屏根的创建、着色器编译和读回。

### 穿刺页面

- 入口 `frontend/spike-webgpu.html`，源码在 `frontend/spike/webgpu/`。仓库已有同样做法的 `frontend/creative-lab.html`：`vite.config.ts` 没有配置多页面的构建入口，这类页面只在开发服务器上存在，不进产品构建。
- 页面用 `?case=<组>&scene=<参考场景>` 选择穿刺项和场景。用 R3F 9.8.1 的异步 `gl` 工厂创建 `WebGPURenderer`（`new WebGPURenderer(props)` 后 `await renderer.init()`）；`?backend=webgl` 时传 `forceWebGL: true`，用来测 WebGL2 后端。
- 原型可以引用 `frontend/src` 里与渲染器无关的模块（场景格式、几何构建、材质参数解析），但不修改它们。
- 测量脚本（Playwright）放在 `frontend/spike/webgpu/measure/`，输出写到 R0 的 `discovery/research/<组>/`。
- R0 结束时，`frontend/spike-webgpu.html` 和 `frontend/spike/` 整体 `git mv` 到 `discovery/research/prototype/`，作为最后一个实现提交。`frontend` 恢复原样，AC-R0-4 由 `git diff develop --stat -- frontend/src` 为空来验证。

### 判定标准

每组的结果写成「可行 / 有条件可行 / 不可行」，条件写明。判定标准：

| 组 | 做法 | 可行的标准 | 回答 |
|---|---|---|---|
| 1 接入与帧推进 | R3F 视口按需渲染、离屏根（`createRoot` + `configure`）出图、`GizmoHelper` 二次渲染；停用渲染器内部的 rAF 循环（`Animation.js`），出图前经私有接口 `renderer._nodes.nodeFrame` 推进帧序号 | 视口交互和按需刷新正常；离屏根出图正确；用 `bringToFront` 切走标签页，确认 `document.hidden` 为真后连续出图两次，逐像素一致 | 未决 5 |
| 2 材质、景深与正交 | `wood@1`（三平面投影）和 `marble@1`（噪声）改写为 TSL Fn；景深和正交 GTAO 改写为 TSL | 与基线图的差异图人眼看不出区别，并记录平均差和超过 8/255 的像素比例 | 未决 5（`sunShadow` 除外）的前提；R1 阈值 |
| 3 天空、阴影与描边 | fork 的 TSL 天空：Z 轴向上、去掉云、不画太阳盘；节点阴影下的包围盒拟合；`OutlineNode` 和反向外壳两种描边 | 天空与基线的地平线、天顶颜色一致；阴影边缘清晰度与基线一致；描边在细小物件和分组上都正确，记录两种做法的帧时间 | 未决 4、5 |
| 4 G-buffer 与抗锯齿 | 单独预通道和单 pass MRT 两种只含不透明几何的做法；MSAA 与 TRAA 切换；`featureLevel: 'compatibility'` 下的 SMAA | 透明物体和隐藏物体不进 AO；切换无残影、不泄漏资源；记录两种做法的帧时间 | 未决 8 |
| 5 实时 GI | SSGI、SSR、时域重投影、降噪、接触阴影、TRAA，直接光和间接光分开输出 | `cornell.json` 里 AC-4 的三个效果可见；参考设备上 1600 × 1000、2 倍像素比的平均帧时间不超过 33 ms；记录收敛帧数；WebGL2 后端单独记录效果和错误 | 未决 1、3、8 |
| 6 高分辨率 | 4K、8K 的离屏出图：一次渲染和分块渲染；线稿的做法；GI 内部分辨率 | 记录显存峰值（以 `device.lost` 和出图是否成功为准）、耗时、各档 PNG 文件大小；给出各档可行的组合 | 未决 9 |
| 7 读回与环境 | `readRenderTargetPixelsAsync` 读回；隐藏标签页下的节流；Safari；headless Chromium 的 WebGPU 启动参数 | 读回的颜色空间、方向、alpha 与 Epic「读回」一节一致；Safari 能跑通组 1 和组 5；找到 headless 下能拿到 WebGPU 适配器的参数（GPU 和 SwiftShader 各一套） | 未决 5；e2e 环境 |

未决 6（帧率持平）由组 1、2 的原型和 `perf.json` 对比给出初步判断，最终在 R1 验收。

### R1 对照阈值

阈值从两组数据推出：同一管线同一设备连拍两次的差异（应为 0），以及组 2 原型与基线之间的差异。R0 给出 `clay`、`line`、`lit` 各自的「平均差」和「超过 8/255 的像素比例」上限，供 R1 的 e2e 防回退；AC-1 是否通过仍由 owner 看并排图判断。

### 结论与 Epic 修订

- research 文件：`discovery/research/baseline.md`，以及每组一篇 `discovery/research/<组>.md`，图、数据和脚本放在同名目录。
- 结论汇总进 Epic 修订草稿：design 的未决事项表逐项改成采用的方案，相应关键机制章节同步修改；需要 owner 选择的项写明选项和代价。8K 不可行或工作量过大时，roadmap 同时拆出后续子项。
- 修订草稿在 `develop` 上修改 Epic 文档（按 lesson `epic-revise-sync-planning-root`，Epic 的规划工作区是主检出），owner 批准后 `lti epic revise`，再提交到 `develop`。

## 局部设计与偏差

- 参考场景用 JSON 固定数据，而不是 e2e 里的构造函数：穿刺页面、R1、R2 都要用同一份场景，JSON 与渲染器无关，也不依赖 e2e 的辅助函数。代价是前四组与现有用例的构造代码重复一份，现有用例不改，所以不会互相影响。
- 原型放在开发服务器专用页面，而不是在编辑器里加开关：不碰产品代码，结束时整体移走，不留开关和分支。
- 基线用例按需运行：依赖本机 GPU，不能进日常 e2e，与现有性能用例一致。

## 接口与数据契约

不变。本次不交付 Epic 契约，不新增局部契约。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | 新增 `referenceScene`、基线用例；AC-9 性能用例在基线模式下写 `perf.json` |
| `frontend/scripts/fixtures/scene-reference/` | 新增 | 六组参考场景 JSON |
| `frontend/spike-webgpu.html` | 临时新增 | 穿刺页面入口，R0 结束时移进 discovery |
| `frontend/spike/webgpu/` | 临时新增 | 原型和测量脚本，R0 结束时移进 discovery |

## 验证与交付

- AC-R0-1、AC-R0-2：在参考设备上先后运行 `SCENE_E2E_ONLY=baseline SCENE_E2E_BASELINE=<dir>` 和 `SCENE_E2E_ONLY=AC-9 SCENE_E2E_PERF=1 SCENE_E2E_BASELINE=<dir>`，`<dir>` 为 `discovery/research/baseline/`。检查六组场景都无报错，三种图、正交截图、`baseline.json`、`perf.json` 齐全。
- 现有用例不回归：定向运行 C5 校准、C4 导出、C5 AC-6 三个用例（它们与新增代码在同一文件），按项目约定换端口，不跑全量。
- AC-R0-3：逐组核对 research 是否有结论、数据和截图，未决事项是否逐项回答。
- AC-R0-4：最后一个实现提交后，`git diff develop --stat -- frontend/src` 为空，`frontend/spike*` 不存在。
- 合回 `develop` 只涉及 e2e 脚本和文档，按 `check-merge` 的判定表只需前端静态检查。

## 未决事项

无。参考设备为本机 Mac16,8（Apple M4 Pro），owner 2026-10-21 确认。
