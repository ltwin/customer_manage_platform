# Tasks

## 1. 参考场景与基线

- [x] 1.1 参考场景：`frontend/scripts/fixtures/scene-reference/` 下六个 JSON（`noon`、`dusk`、`room` 与 C5 校准的 `yard`、`room` 内容相同，`materials` 含 15 种材质方块和透射球，`dof` 与 C4 的 `exportScene(true)` 相同，`cornell` 按 design 的盒子间），每组带导出机位；`creative-scene.e2e.mjs` 新增 `referenceScene(server, name)`。完成判据：六个 JSON 都能经 `referenceScene` 载入编辑器，白模和最终渲染无报错
- [x] 1.2 基线用例：`SCENE_E2E_BASELINE=<dir>` 时运行的 `baseline:` 用例（Metal 参数，1600 × 1000，2 倍像素比）导出每组三种图、正交白模截图，写 `baseline.json`；AC-9 性能用例在基线模式下写 `perf.json`。在参考设备上两次运行，输出到 `discovery/research/baseline/`，`baseline.md` 记录设备、版本、耗时和帧时间；证据超过 30 MB 时按 proposal 的规则取舍。定向运行 C5 校准、C4 导出、C5 AC-6 三个现有用例仍通过

## 2. 穿刺页面与环境

- [x] 2.1 穿刺页面骨架：`frontend/spike-webgpu.html` 与 `frontend/spike/webgpu/`，R3F 异步 `gl` 工厂创建 `WebGPURenderer`，`?scene=` 载入参考场景 JSON 并画出基础体（`MeshStandardNodeMaterial`），`?backend=webgl` 走 WebGL2 后端；`vite build` 产物不含该页面。完成判据：开发服务器上打开页面能看到 `noon` 场景，页面显示当前后端
- [x] 2.2 headless 环境（组 7 的一部分）：找到 headless Chromium 能拿到 WebGPU 适配器的启动参数（本机 GPU 一套、SwiftShader 一套），`frontend/spike/webgpu/measure/` 下的测量脚本能打开穿刺页面、读出适配器信息并截图。结论写入 `discovery/research/environment.md`

## 3. 穿刺：接入、材质与画面

- [x] 3.1 组 1 接入与帧推进：视口按需渲染、`GizmoHelper` 二次渲染、离屏根出图；停用内部 rAF 循环、经 `nodeFrame` 显式推进帧序号；隐藏标签页（确认 `document.hidden`）里连续出图两次逐像素比较；`sunShadow.ts` 的做法在节点阴影里是否生效。结论写入 `discovery/research/integration.md`，回答未决 5
- [x] 3.2 组 2 材质、景深与正交：`wood@1`、`marble@1` 改写为 TSL Fn，景深与正交 GTAO 改写为 TSL；与基线图出差异图，记录平均差和超过 8/255 的像素比例。结论写入 `discovery/research/materials.md`
- [x] 3.3 组 3 天空、阴影与描边：fork 的 TSL 天空（Z 轴向上、去云、不画太阳盘）与基线对照；节点阴影的包围盒拟合；`OutlineNode` 与反向外壳两种描边在细小物件和分组上的效果与帧时间。结论写入 `discovery/research/sky-shadow-outline.md`，回答未决 4
- [x] 3.4 组 4 G-buffer 与抗锯齿：单独预通道与单 pass MRT 两种只含不透明几何的做法及帧时间；MSAA 与 TRAA 切换；`compatibility` 特性级别下的 SMAA。结论写入 `discovery/research/gbuffer-aa.md`，回答未决 8 的 G-buffer 部分

## 4. 穿刺：实时 GI 与高分辨率

- [x] 4.1 组 5 实时 GI：在 `cornell` 场景组合 SSGI、SSR、时域重投影、降噪、接触阴影与 TRAA，直接光和间接光分开输出；参考设备上的效果截图、帧时间、收敛帧数；WebGL2 后端的表现。结论写入 `discovery/research/realtime-gi.md`，回答未决 1、3 和 8 的 MRT 布局部分
- [x] 4.2 组 6 高分辨率：4K、8K 的一次渲染与分块渲染，线稿做法，GI 内部分辨率；显存是否够用、耗时、各档 PNG 大小与媒体上限的冲突。结论写入 `discovery/research/high-resolution.md`，回答未决 9 并列出选项与代价
- [x] 4.3 组 7 读回与 Safari：`readRenderTargetPixelsAsync` 的颜色空间、方向与 alpha；隐藏标签页下的节流；Safari 打开组 1 和组 5 的页面。结论补入 `discovery/research/environment.md`

## 5. 收尾

- [x] 5.1 R1 阈值与未决 6：用连拍差异和组 2 的差异推出 `clay`、`line`、`lit` 的阈值；用组 1、2 的原型帧时间与 `perf.json` 对比给出帧率持平的初步判断。写入 `discovery/research/summary.md`，汇总七组结论和未决事项 1、3、4、5、6、8、9 的建议
- [x] 5.2 原型移出：`frontend/spike-webgpu.html` 与 `frontend/spike/` 整体 `git mv` 到 `discovery/research/prototype/`；`git diff develop --stat -- frontend/src` 为空，前端静态检查通过
- [x] 5.3 Epic 修订草稿：在 `develop` 的 Epic 文档上回填 design 的未决事项与相应关键机制，必要时修改 roadmap（拆出 8K），呈给 owner
