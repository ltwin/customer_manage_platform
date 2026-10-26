# Tasks

## 1. 参考场景与基线

- [ ] 1.1 参考场景：在 `frontend/scripts/fixtures/scene-reference/` 下新增 `frame-two-lights.json` 和 `occluder-wall.json`，内容按 design「参考场景」：`frame-two-lights` 复制文档 `cwdoc_98de9f98-…`，只读开发库，两个模型实例作为代码模型放进固定数据，走不通时改存 GLB。在 `creative-scene.e2e.mjs` 里新增 `lightReferenceNames`，并入「每个参考场景都能打开」的检查和 `SCENE_E2E_BASELINE` 用例。完成判据：定向运行这项检查通过（AC-S0-1）；`frame-two-lights` 的 `lit` 导出与原场景在编辑器里的画面一致。
- [ ] 1.2 基线：在参考设备上运行 `SCENE_E2E_BASELINE`，把两个新场景在 `workbench` 和 `realtime_gi` 下的 `lit` 导出存到 `discovery/research/baseline/`，`baseline.md` 记录设备和版本（AC-S0-2）。

## 2. 穿刺页面与参考解

- [ ] 2.1 页面骨架：新增 `frontend/spike-light.html` 和 `frontend/spike/light/`，design「穿刺页面」里的参数都能用。原型的有尺寸灯光子类经 `addLight` 注册，`PhysicalLightingModel` 的包装只在页面里装。完成判据：两个场景和 `area-studio` 都能显示；`vite build` 的产物不包含这个页面。
- [ ] 2.2 第 1 组，参考解：按 design「参考解」实现，覆盖球、圆盘、矩形和椭圆，输出 `direct`、`directUnshadowed` 和 `visibility`。完成判据：第 1024 帧与第 2048 帧的差异每通道不超过 1/255（AC-S0-3）。结论写进 `reference.md`。

## 3. 软阴影

- [ ] 3.1 第 2 组，候选做法：`pcf-radius`、`pcss`、`smtrace`、`vsm-pcss`、`msm-pcss`、`mip-pcss`，以及导出时的 `jitter` 对照。每种都按 design「测量与判定」测量：
  - 误差和半影宽度，在 `occluder-wall` 的三块板上分别测，覆盖多层遮挡和大半影；
  - `workbench` 和 `realtime_gi` 下的颗粒，核的旋转随帧序号变化；
  - 级联边界和立方体面接缝处是否连续；
  - 接收面偏置下的条纹和漏光；
  - 每盏灯的耗时，热缓存和冷缓存分开。

  结论写进 `soft-shadow-candidates.md`，附 `frame-two-lights` 和 `occluder-wall` 上与参考解的并排图集（AC-S0-5）。
- [ ] 3.2 第 3 组，读取原始深度（U7）：在 WebGL2 和 WebGPU 上，验证同一张设了 `compareFunction` 的阴影深度图能否在一个着色器里同时做比较采样和原始读取。不行时，验证阴影 pass 额外输出线性深度的备选方案。结论写进 `depth-read-backends.md`。
- [ ] 3.3 第 4 组，接法：
  - `PhysicalLightingModel` 新入口的包装方式，以及 clearcoat 和 sheen 的处理；
  - 四个滤波接入点：2D、立方体（`dp` 线性化）、`SunCascades` 逐级设置、standIn 编出同一个着色器；
  - 尺寸为 0 时按 uniform 分支走原来的 PCF，与基线的差异不超过 1/255，且不把比较采样放进按像素分叉的分支。

  结论写进 `integration.md`。

## 4. 面光源、接触阴影、近似与对照

- [ ] 4.1 第 5 组，面光源阴影（U3、U5）：
  - 比较中心处的立方体贴图和正面半球的透视贴图；
  - 测单视点的误差；
  - 阴影相机的轴与面光源的轴不对齐时，滤波范围怎么算；
  - 半影上限随尺寸怎样增长。

  结论写进 `area-shadow.md`。
- [ ] 4.2 第 6、7 组：
  - 实时 GI 的接触阴影：按半影宽度缩短追踪长度，或超过阈值就关掉（U4）；
  - 球形光源的地平线裁剪：Snyder 和代表点两种做法，都必须做出包裹，以及 sheen 近似的误差（U6）。

  结论分别写进 `contact-shadow.md`、`sphere-horizon-sheen.md`。
- [ ] 4.3 第 8 组，Blender Cycles 对照的做法（后续验收项）：写 `blender-cycles.md`，包括 `occluder-wall` 的搭建步骤、Cycles 渲染设置、要读的相对量和读法，以及参考解给出的预期值；同时写明 U8 的核对步骤。本机不渲染（owner 2026-10-26）。
- [ ] 4.4 第 9 组，缺省值的观感：在 `frame-two-lights` 上看半径 0.1 m 的效果，在 `area-studio` 上看 10000 cd/m² 的面光源效果。图交 owner 看，结论写进 `defaults-look.md`。

## 5. 结论与收尾

- [ ] 5.1 第 10 组，阈值：提出 AC-1、AC-3（包裹）、AC-4、AC-5、AC-8 的阈值，附依据，写进 `thresholds.md`。U1–U8 每项给出建议（AC-S0-4）。对比图集和阈值交 owner 选定。
- [ ] 5.2 原型收尾：用 `git mv` 把 `frontend/spike-light.html` 和 `frontend/spike/light/` 移进 `discovery/research/prototype/`。完成判据：`git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空，`frontend/spike*` 不存在，前端静态检查通过（AC-S0-6）。
- [ ] 5.3 Epic 修订：在主检出的 `develop` 上，把 owner 的选择和阈值写回 Epic 的 design 和 proposal，呈给 owner。批准后执行 `lti epic revise`（AC-S0-7）。
