# Tasks

## 1. 准备

- [x] 1.1 改动前的基线：
  - 在兼容模式、`forceCompatibility`、`traceUnsupported`、`traceDelayMs`（就绪前）下导出参考场景集的实时 GI `lit`，作为 AC-S2-5 的「S2 之前」存档；
  - 导出 `backlit-room` 在 45° 和 100° 的图，以及 30° 到 120° 每 15° 一格的扫视，作为 AC-S2-1 的并排对照；
  - 跑一次 S1 的 `SCENE_E2E_TRACE_PERF`，作为 AC-S2-6「不带这一层」的一侧。

  只保存到 S2 的 `discovery/research/evidence/`，超过 30 MB 的部分留在本机并记下重新生成的命令

  结果见 `evidence/README.md` 的 `before/`：
  - 五套导出，`traceOff` 等四套与 S1 的存档亮度相同；
  - `traceDelayMs` 下导出会等底座、等不到就失败，所以「就绪前」改在视口里核对；
  - 扫视里靠近亮墙的点在亮墙进入画面时变亮 16%；
  - 这一轮机器比 S1 合入时慢 10%–20%，帧时间只作参考
- [x] 1.2 S2-U1 的最小 compute 用例：在 three r186 的 compute 里显式 LOD 采样（`textureSampleLevel`）和向 RGBA16F 存储纹理写入（`textureStore`）各写一个，读回核对。不行的部分在 `trace/computePatches.ts` 里补（连同 `TextureNode.grad()`、PMREM 的环境旋转）；仍不行时按 design 的退路改用 `textureLoad` 加手工双线性。完成判据：用例读回正确，结论写进 design 的 S2-U1

  结果：`SCENE_E2E_PROBES` 的 `probes: S2-U1` 用例通过，两项都不需要补丁。`computePatches.ts` 的 `grad()` 和环境旋转两处补丁要在 compute 里采 PMREM 才用得上，改到任务 4.2 随光线内核一起加、一起验证

## 2. 不构建时的两处改动（AC-S2-5，各自单独提交）

- [x] 2.1 `giSplit.ts` 的 `ambient.a` 改写 c（`computeMultiscattering` 算出的 c_E × 材质 AO，亮度加权，下限 1e-3）；`realtimeGI.ts` 的 `split` 改为 `ambient.a > 0`。先在 `creative-scene-shading.test.ts` 加 giSplit 写 c 的核对。完成判据：单测通过；AC-S2-5 的四种情况与 1.1 的存档逐像素相同

  结果见 `evidence/ac-s2-5.md`：`traceOff`、`traceUnsupported`、WebGL2 三套逐像素相同；`forceCompatibility` 有一张图（`room`）与存档差 1–3 级，在同一份代码两次运行之间的噪声以内（S1 的口径）。单测放在 `creative-scene-gi.test.ts`，giSplit 原有的单测在那里，不在挂载点表写的 `creative-scene-shading.test.ts`；sheen 的能量补偿要用 three 未导出的 `IBLSheenBRDF`，复制了一份，单测守住它与上游的常数相同
- [x] 2.2 光照 MRT 的 `base` 改为 sRGB 编码。完成判据：AC-S2-5 的四种情况与 1.1 的存档在 R2 的一级以内

  结果见 `evidence/ac-s2-5.md` 第二步：35 张图都在一级以内，差超过 8 级的像素最多 0.0031%（`mirror-board` 反射锯齿边缘上的 80 个像素，对照图 `after/base-srgb-mirror-board.png`）；整图亮度不变

## 3. 底座的两份精度

- [x] 3.1 `base.ts`、`capacity.ts`：两份精度同时服务，代理始终构建；`geometryVersion` 与 `lightingVersion`；场景包围盒（不含 `ground@*`）和改动的实例包围盒；`TraceHandle` 改为 `{reflect, probes, tables, tier, key}`；两份一起估算与判档。`tables.ts` 交出 `L_amb`，灯光与环境的变化计入 `lightingVersion`。先在 `creative-scene-trace.test.ts` 写单测：编排（一次就绪、换档、几何失败）、版本号、包围盒。完成判据：单测通过；S1 的 `SCENE_E2E_TRACE` 用例照过

  结果：`creative-scene-trace.test.ts` 26 项通过（新增判档的三行、两份精度的编排、版本号与包围盒）；`SCENE_E2E_TRACE` 的 12 个 `trace:` 用例全部通过。实现上的取舍：
  - 第 1 档的判定改为原始与代理合计 × 1.25 不超过预算，且两份都在设备上限以内；
  - 灯光是否变化按「已曝光值 ÷ k」比较，留 1e-5 的相对容差（经过 float32 和除法）。场景的环境光不参与预曝光，但 k 只在物理天空下不为 1，那时没有环境光；
  - 失败重试时重新判定要不要建原始几何；
  - `traceStats` 多了 `probes`、`geometryVersion`、`lightingVersion`，`target` 改为反射正在建的那一份

## 4. 探针体

- [x] 4.1 `probes/cascade.ts`、`probes/octahedral.ts`：级数、原点对齐、欧氏取模（含负坐标）、环形重置的判定、轮转表、α(编辑年龄)、成熟度、`viewportSettleFrames`；八面体编解码。先在新增的 `creative-scene-probes.test.ts` 写单测。完成判据：单测覆盖 design「级联的放置与轮转」「更新」里的每一张表

  结果：`creative-scene-probes.test.ts` 12 项通过。两处局部调整：
  - 轮转的错开由「按级号偏移」改为逐级贪心选偏移（`offsetsFor`）。按级号偏移在大厅的周期（4, 4, 8, 8, 16）下，有的帧更新两级、有的帧一级都不更新；贪心之后每帧最多比其他帧多一级；
  - 八面体图块的边框由「每个内部纹素写哪几个边框纹素」（`bordersOf`）给出，单测核对每个边框纹素只有一个来源，且折回之后与来源是同一方向。TSL 版本的编解码在任务 4.2 的 GPU 对照里核对
- [x] 4.2 `probes/volume.ts`、`probes/kernels.ts`：`ProbeVolume` 按 `n_alloc` 分配；`step(frame)`，先派发所有级的光线、再派发所有级的更新（P2）；原点锁定、世界格核对；s_env 换算内核；`hit.ts` 的 `radiance`（含单面材质的背面、没命中取 PMREM 粗糙度 0.5）。`checks.ts` 加 GPU 侧的对照：槽号与世界格（含负坐标）、八面体编码，与 JS 结果比较。`gpu.ts` 的 `make` 包住 `createComputePipeline`，`pipelineFailed` 也看 compute 管线。完成判据：GPU 对照与 JS 一致；compute 管线失败时走 S1 的失败处理

  结果：`SCENE_E2E_PROBES` 的两个新用例通过，S1 的 `trace: AC-S1-*` 5 个用例照过：
  - GPU 对照：72 组带负坐标的世界格和槽号、128 个方向的八面体编解码，与 JS 逐值一致；
  - 冒烟测试：小场景上的导出探针体跑 64 帧。值全部有限；第 0 级每个探针都更新了 64 次；方块内部 64 个探针全部停用，方块外没有停用的；墙边比空旷处亮；两次运行逐位相同。
  - `gpu.ts` 的 `computeFailed()` 已就位，由任务 4.3 的 SceneRenderer 接到底座的 `fail(true)`。

  实现上的局部调整，记理由：
  - 光线缓冲每个探针体一份，按级分区，不是每级一个缓冲。每帧先把所有到期级的光线一次派发完，再一次派发所有更新，P2 照样成立，绑定更少；
  - 各级的原点和到期级用单独的 uniform，不用 `uniformArray`：后者每次 render 只上传一次，导出预热在两次 render 之间要派发多帧；
  - 光线内核从状态纹理读偏移和世界格，不读状态缓冲：这样只绑 7 个存储缓冲，在 WebGPU 默认的 8 个以内；
  - 逐级回退的覆盖度按 1 − Π(1 − cᵢ) 合成，不是 design 写的 max：它与「逐级 mix」严格对应，合成按覆盖度混合时不会重复计入；
  - 场景变大、要多分配一级时，探针体整个重建（冷启动），不把已有级的数据拷过去（design「分配」写的是拷过去）。这种情况很少见，S2 先这样做；
  - TSL 的 `If` 回调不能返回语句节点（`textureStore`、`addAssign`），否则会被当成分支的值去生成，报「Invalid generated code」。一律写成块体
- [x] 4.3 `probes/lookup.ts` 与合成：`probeIrradiance` 逐级回退；`realtimeGI.ts` 的屏幕采样 pass，`composite` 的 `world` 分支，`world` 时不建 SSGI 漫反射降噪链、`GI_INTENSITY` 取 0；`RealtimeGIOptions` 收 `TraceHandle` 和 `ProbeVolume`。`sceneRenderer.ts` 的 `build` 交出两者，`render` 由视口的 `SceneRenderer` 持有探针体（按代理层身份做键，管线重建时保留）并 `step`，底座就绪后在后台预热到数据年龄 ≥ A0 + 14 再切换。先在 `creative-scene-gi.test.ts` 写 `world` 分支的纯数值单测（`coverage` 为 0 时等于 `A_env · ao + A_lt · ao`）。完成判据：单测通过；`backlit-room` 能看到摄像机背后墙的反弹光

  结果：`creative-scene-gi.test.ts` 15 个通过，其中 `world` 分支的单测：覆盖度为 0 时等于 `A_env · ao + A_lt · ao`，为 1 时环境那份换成探针的 `kd · c · E / π`（不乘 AO），环境光灯那份保留并乘 AO；半覆盖、`fill` 和环境份额不小于 0 也各有断言。导出能看到探针的光（`gi-baseline` 的同一机位，接入前 → 接入后的画面均值）：
  - `backlit-room`：148.5 → 153.7，左墙亮度比 1.001；
  - `cornell`：89.5 → 161.9。偏亮多少要在 5.2 对照参考图判断。
  S1 的用例照过：AC-S1-1/2/5/6、6.1、7.1 通过，WebGL2 画面与接入前逐像素相同（P1）。另有两处要后续处理：
  - AC-S1-4 三次里有一次失败（一张图最大差 3 级），可能与 `scene-trace-export-repeat` 同源，放到 5.4 查；
  - 导出变慢：`backlit-room` 5.2 → 9.0 s，`cornell` 5.3 → 6.6 s，5.4、5.5 核对时限。
  S1 的 `trace: 5.2` 改了一处断言：层就绪后视口每帧少画一次（53 → 52）。这是设计内的变化：探针接入后不再建 SSGI 漫反射的重投影和降噪（两次绘制），多了探针的屏幕采样 pass（一次）。断言改为「探针已接入，且绘制次数正好少 1」，两轮都通过。

  实现上的局部调整，记理由：
  - `ProbeVolume` 带一个页面内递增的 `serial`，管线键用它：换了探针体，管线就重建；
  - 先加了 5.1 的 `gpuTestHooks.probeStats`（级数、各级更新次数、是否已预热、本帧是否画了探针），5.2 的断言要用；
  - `host/render.ts` 在第一帧前调用 `warm`，原属 5.4，导出要看到探针就得先接上；时限的计入留在 5.4
- [ ] 4.4 命中点的间接光：`hit.ts` 的 `shade` 读探针，乘 `r_fill`；`hitCheck` 可关掉读探针。数一遍反射片元的实际纹理绑定（S2-U2），超出 16 张时把状态并进辐照度图集的 alpha。完成判据：S1 的反射用例照过；S2-U2 的结论写进 design

## 5. 验收与标定

- [ ] 5.1 e2e 用例 `SCENE_E2E_PROBES` 与 `gpuTestHooks`（`probeLayer`、`probeStats`、`probeRegions`、`probeFreeze`）；逐像素参考搬进 `checks.ts`（每像素 256 条路径、3 次命中、最后一次才读探针）。完成判据：AC-S2-1 的比值和扫视、AC-S2-2 的区域辐照度（含 `thin-wall` 的两个新机位）能跑出数值
- [ ] 5.2 漏光与亮度（AC-S2-1、AC-S2-2、S2-U3、S2-U6、S2-U7）：调重定位和 Chebyshev 参数，记下 `cornell`、`thin-wall`、`physical-noon` 对照参考的结果；`r_fill`、短半径 AO 的对照图交 owner。完成判据：结果与图写进 evidence；没达标或要改 `r_fill` 的地方先交 owner，不自行放宽
- [ ] 5.3 编辑与收敛（AC-S2-3、S2-U5）：拖动物件、关一盏灯、改太阳高度之后的帧数；拖动期间 `coverage` 不归零；`Viewport.tsx` 的 `LookView` 改用 `viewportSettleFrames`，后台预热期间持续请求帧。录 A0 = 12、α = 0 与 α = 0.5 的拖动对照视频交 owner。完成判据：参考场景回到 10% 以内的帧数达标，压力场景记下帧数
- [ ] 5.4 导出（AC-S2-4、S2-U9）：`SceneRenderer.warm`（M = 64，写明前置步骤）；`host/render.ts` 的 `shoot` 在第一帧前 `warm`；继续更新与冻结做成开关，缺省继续更新；`engines.ts` 的 `drawnFrames`、`estimateMs`、`shotBudgetMs`、`viewBudgetMs` 加 `world`，`PROBE_WARMUP = 64`；`exportShots.ts` 的时限计入探针。完成判据：隐藏标签页里参考场景两次导出逐像素相同；两种做法的耗时和对照图交 owner
- [ ] 5.5 性能与复杂场景（AC-S2-6、AC-S2-7、AC-S2-8、S2-U4、S2-U8）：S1 的 `SCENE_E2E_TRACE_PERF` 带与不带这一层、冷热着色器缓存分开；AC-S1-8 用例照跑；`probeLayer = 'original'` 与代理的差异统计和并排图；导出 `r_max` 0.5 与 1.0 的对照。完成判据：数值写进 evidence；超出时按 design「性能」的顺序，每动用一项先交 owner 看图

## 6. 提示与指纹

- [ ] 6.1 提示文字：`base.ts` 的 `TraceNotReady`、`RenderPanel.tsx` 的提示和渲染器说明改为「屏幕外的光照与反射」。完成判据：AC-S2-5 的提示文字用例通过
- [ ] 6.2 指纹（AC-S2-10）：`fingerprint.go` 加 `world_trace`，实时 GI 的 `lit` 取 1；先在 `fingerprint_test.go` 写单测：实时 GI 的 `lit` 与 S2 前不同，工作台的 `lit`、`clay`、`line` 不变。完成判据：`make check-go PKG=./internal/creativescene/...` 通过；e2e 读画布看到「场景已更新」

## 7. 收尾

- [ ] 7.1 门禁与契约（AC-S2-11）：`make check-frontend` 通过；逐条核对 `architecture: 场景模块`、`patterns: 场景着色写法`、`data: 场景文档` 与实现一致，不一致的先提 Epic 修订
