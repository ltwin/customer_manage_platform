# Tasks

## 1. 参考场景与基线

- [x] 1.1 参考场景：在 `frontend/scripts/fixtures/scene-reference/` 下新增 `frame-two-lights.json` 和 `occluder-wall.json`，内容按 design「参考场景」：`frame-two-lights` 复制文档 `cwdoc_98de9f98-…`，只读开发库，两个模型实例作为代码模型放进固定数据，走不通时改存 GLB。在 `creative-scene.e2e.mjs` 里新增 `lightReferenceNames`，并入「每个参考场景都能打开」的检查和 `SCENE_E2E_BASELINE` 用例。完成判据：定向运行这项检查通过（AC-S0-1）；`frame-two-lights` 的 `lit` 导出与原场景在编辑器里的画面一致。
  - 结果：11 个参考场景都通过（207 s）；`frame-two-lights` 的导出与 owner 的截图同一画面，带同样的硬边明暗带。
  - 实施时调整（design「挂载点」）：
    - 模型放在场景同名目录下，由 `referenceScene` 按原修订 ID 载入，`seedModel` 新增可选的 `rev`；
    - `occluder-wall` 的灯改从左上后方 (−5.5, 3.6, 4) 斜照，三块板放在不同的位置。布局由脚本校验：板、影子和高光点在画面里互不遮挡，也都不越出墙面。脚本快照见 `discovery/research/occluder-wall-layout.py`。
    - 只读开发库和 OSS 时，连接串取自本机 `.env`。
- [x] 1.2 基线：在参考设备上运行基线用例，把两个新场景在 `workbench` 和 `realtime_gi` 下的 `lit` 导出存到 `discovery/research/baseline/`，`baseline.md` 记录设备和版本（AC-S0-2）。实施时调整：改用单独的 `SCENE_E2E_LIGHT_BASELINE`，不重跑旧场景。结果：4 张导出共 12 MB，见 `baseline.md`；第一次运行有一次 `realtime_gi` 导出偶发卡住，重跑两次都正常，已记录。

## 2. 穿刺页面与参考解

- [x] 2.1 页面骨架：新增 `frontend/spike-light.html` 和 `frontend/spike/light/`，design「穿刺页面」里的参数都能用。原型的有尺寸灯光子类经 `addLight` 注册，`PhysicalLightingModel` 的包装只在页面里装。完成判据：两个场景和 `area-studio` 都能显示；`vite build` 的产物不包含这个页面。
  - 结果：三个场景在 `workbench` 和 `realtime_gi` 下都能出图。`frame-two-lights` 的 `realtime_gi` 编译约 2.5 s，76 帧共 6.4 s。`vite.config.ts` 没有 `rollupOptions.input`，构建只打包 `index.html`。
  - 实施时调整：页面挂载产品的 `ShotScene`，用 `SceneRenderer` 出图，流程同 `openShooter`；候选滤波在 `g.compile` 之前装上。`area-studio` 的面光源缺省 100 cd/m²，保证能看清（`?nits=` 可调）；10000 cd/m² 在这个场景下全白，留给第 9 组。
  - 测量脚本 `spike/light/measure/shot.mjs`。
- [x] 2.2 第 1 组，参考解：按 design「参考解」实现，覆盖球、圆盘、矩形和椭圆，输出 `direct`、`directUnshadowed` 和 `visibility`。完成判据：第 1024 帧与第 2048 帧的差异每通道不超过 1/255（AC-S0-3）。结论写进 `reference.md`。
  - 结果：可行。不带阴影时，参考值与 three 的光栅值相差不超过 0.15%。半径 0.1 m 时，远处照度与 `I/d²` 相差 0.03%–0.15%。
  - 实施时调整：1024 对 2048 帧最大差 3 级，参考解改用 8192 帧。8192 对 16384 帧时，五种光源都不超过 1 级。
  - 顺带测出：聚光和点光的阴影 `bias` 为 0，造成自阴影条纹，未被遮挡的墙面被压暗到 66%；PCF 的旋转不随帧变化，TRAA 平均不掉。都交给第 2 组。

## 3. 软阴影

- [x] 3.1 第 2 组，候选做法：`pcf-radius`、`pcss`、`smtrace`、`vsm-pcss`、`msm-pcss`、`mip-pcss`，以及导出时的 `jitter` 对照。每种都按 design「测量与判定」测量：
  - 误差和半影宽度，在 `occluder-wall` 的三块板上分别测，覆盖多层遮挡和大半影；
  - `workbench` 和 `realtime_gi` 下的颗粒，核的旋转随帧序号变化；
  - 级联边界和立方体面接缝处是否连续；
  - 接收面偏置下的条纹和漏光；
  - 每盏灯的耗时，热缓存和冷缓存分开。

  结论写进 `soft-shadow-candidates.md`，附 `frame-two-lights` 和 `occluder-wall` 上与参考解的并排图集（AC-S0-5）。
  - 进展：聚光、点光和平行光的 PCSS 已在 `occluder-wall` 和 `frame-two-lights` 上与参考解对照，含半影宽度、叠放遮挡、两种渲染器的成片和耗时；`smtrace` 只做了聚光。
  - 实施时调整：
    - `vsm-pcss`、`msm-pcss`、`mip-pcss` 不做，理由见 `soft-shadow-candidates.md`「取舍」；owner 2026-10-26 确认：默认 PCSS，`smtrace` 不进产品，多层遮挡的误差可以接受；
    - 颗粒用「单帧对多帧平均」的误差差值和 `workbench` 单帧成片来体现，没有另算局部方差；
    - 新增 `?layout=stack`（多层遮挡）、`?sunSize=`、`?maxSlope=` 和 `SPIKE_WARM`，`?cold=` 停用。
    - 导出时的 `jitter` 对照（逐帧抖动光源位置再累积）没有做：PCSS 在单层遮挡下已与参考解吻合，`workbench` 的颗粒改由 U2 的时域累积解决（`thresholds.md`）。
- [x] 3.2 第 3 组，读取原始深度（U7）：在 WebGL2 和 WebGPU 上，验证同一张设了 `compareFunction` 的阴影深度图能否在一个着色器里同时做比较采样和原始读取。不行时，验证阴影 pass 额外输出线性深度的备选方案。结论写进 `depth-read-backends.md`。
  - 结果：比较采样加原始读取在 WebGPU 上有条件可行，在 WebGL2 上不可行；「去掉比较、最近点过滤、全部手动比较」在两个后端都可行，画质与前者相同，建议统一采用。WebGPU 和 WebGL2 各需绕开一个 three r186 的缺陷。线性深度备选不需要了。
- [x] 3.3 第 4 组，接法：
  - `PhysicalLightingModel` 新入口的包装方式，以及 clearcoat 和 sheen 的处理；
  - 四个滤波接入点：2D、立方体（`dp` 线性化）、`SunCascades` 逐级设置、standIn 编出同一个着色器；
  - 尺寸为 0 时按 uniform 分支走原来的 PCF，与基线的差异不超过 1/255，且不把比较采样放进按像素分叉的分支。

  结论写进 `integration.md`。
  - 结果：
    - 滤波函数只从它所过滤的 `shadow` 读参数，阴影的 `copy()` 带上 `filterNode` 和尺寸，四个接入点就共用同一个滤波函数。
    - standIn 编出的着色器本来就与真实的不同（产品现有限制），带上滤波函数后，聚光和点光的停顿从约 0.3 s 降到几十毫秒；平行光级联因 `ownNames` 编号对不上，交给 S1。
    - 尺寸为 0 的灯不挂滤波函数，与基线逐像素一致。
    - 高光推荐「混合」模型：能量在 8% 以内、面积在 15% 以内、峰值在 20% 以内；Karis 原版在小半径时高光面积大了约 2 倍。
  - 实施时调整：
    - 尺寸为 0 不走 uniform 分支，改为不挂滤波函数，理由见 `integration.md`「尺寸为 0」；
    - `addLight` 换不掉 three 内置的灯光类，原型直接改写 `lightNodes`，S1 用自己的灯光子类。

## 4. 面光源、接触阴影、近似与对照

- [x] 4.1 第 5 组，面光源阴影（U3、U5）：
  - 比较中心处的立方体贴图和正面半球的透视贴图；
  - 测单视点的误差；
  - 阴影相机的轴与面光源的轴不对齐时，滤波范围怎么算；
  - 半影上限随尺寸怎样增长。

  结论写进 `area-shadow.md`。
  - 结果：中心的立方体阴影图与参考解几乎一样，前半球透视图半影只有一半（U3）；细长光源要用与光源轴对齐的矩形核；上限取 128 texel 并把采样加到 32 + 64 次最好，但每帧多 19 ms，建议出图与视口分档（U5）。
- [x] 4.2 第 6、7 组：
  - 实时 GI 的接触阴影：按半影宽度缩短追踪长度，或超过阈值就关掉（U4）；
  - 球形光源的地平线裁剪：Snyder 和代表点两种做法，都必须做出包裹，以及 sheen 近似的误差（U6）。

  结论分别写进 `contact-shadow.md`、`sphere-horizon-sheen.md`。
  - 结果：
    - 第 6 组：主光有尺寸时，关掉屏幕空间接触阴影。它在有尺寸的灯下只会叠出硬暗带和颗粒虚线，抖动追踪方向也几乎没有改善。
    - 第 7 组：漫反射用 Snyder 的精确照度，交界线与参考解相差不超过 1°，整条曲线几乎重合；代表点包裹过头 7–10°。sheen 随漫反射走；参考解没有 sheen，误差没有量。
- [x] 4.3 第 8 组，Blender Cycles 对照的做法（后续验收项）：写 `blender-cycles.md`，包括 `occluder-wall` 的搭建步骤、Cycles 渲染设置、要读的相对量和读法，以及参考解给出的预期值；同时写明 U8 的核对步骤。本机不渲染（owner 2026-10-26）。
- [x] 4.4 第 9 组，缺省值的观感：在 `frame-two-lights` 上看半径 0.1 m 的效果，在 `area-studio` 上看 10000 cd/m² 的面光源效果。图交 owner 看，结论写进 `defaults-look.md`。
  - 结果：owner 2026-10-26 按建议定平行光缺省角径 0.545°；面光源缺省值保留。导出耗时随后优化：采样循环改用 TSL 的 `Loop`，冷启动导出 `workbench` +30%、`realtime_gi` +14%（原来是 +190% 和 +89%），画面逐像素不变。

## 5. 结论与收尾

- [x] 5.1 第 10 组，阈值：提出 AC-1、AC-3（包裹）、AC-4、AC-5、AC-8 的阈值，附依据，写进 `thresholds.md`。U1–U8 每项给出建议（AC-S0-4）。对比图集和阈值交 owner 选定。
  - 结果：owner 2026-10-26 按建议选定 `thresholds.md` 的阈值和 U1–U8，包括 AC-4 改为「与参考解比」（原文的「与点光源比」在大光源下不成立）；聚光和点光的条纹放进 S1 一起修，AC-2 的基线改为修复后的画面。
- [x] 5.2 原型收尾：用 `git mv` 把 `frontend/spike-light.html` 和 `frontend/spike/light/` 移进 `discovery/research/prototype/`。完成判据：`git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空，`frontend/spike*` 不存在，前端静态检查通过（AC-S0-6）。
  - 结果：已移入 `discovery/research/prototype/`，附重新运行的说明（`README.md`）。`develop` 在本分支拉出后又合入了 `scene-probe-gi`，所以判据按分支自身的改动核对（`git diff develop...HEAD`）：上述三处为空；`frontend/` 下只剩任务 1.1–1.2 的基线场景和 e2e。`frontend/spike*` 不存在；`oxlint` 无错误，`tsc -b` 通过。
- [ ] 5.3 Epic 修订：在主检出的 `develop` 上，把 owner 的选择和阈值写回 Epic 的 design 和 proposal，呈给 owner。批准后执行 `lti epic revise`（AC-S0-7）。
