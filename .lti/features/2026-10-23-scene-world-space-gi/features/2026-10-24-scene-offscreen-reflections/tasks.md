# Tasks

## 1. 准备

- [ ] 1.1 依赖与静态检查：
  - `meshoptimizer` 加为 `frontend` 的直接依赖，锁定精确版本（与现在间接依赖的 1.1.1 相同）；
  - `creative-scene-shading.test.ts` 加禁 `wgslTagFn`，`wgslFn` 只对 `render/trace/wgsl.ts` 放行。

  完成判据：`make check-frontend` 通过；临时在别处写一个 `wgslFn` 时检查失败（AC-S1-10）
- [ ] 1.2 改动前的基线：
  - 在独立的测试页里，每个预设单独构建、顺序固定，记下 14 个预设生成的 WGSL 的哈希；
  - 在 `traceOff` 下导出参考场景集的实时 GI `lit`，WebGPU 和 WebGL2 各一套，作为 AC-S1-3 和 AC-S1-6 的「改动前」；
  - 导出 `mirror-board` 的改动前图，作为 AC-S1-1 的并排对照。

  只保存到 S1 的 evidence 目录，不进 git 的部分记下重新生成的命令

## 2. 宽度函数（AC-S1-3，单独提交）

- [ ] 2.1 `PatternInput.width`：`patterns.ts` 的 6 处 `fwidth` 改为 `width(...)`，`aaStep`、`joint`、`planar` 往下传；`instances.ts` 传 `fwidth`。完成判据：
  - 14 个预设的 WGSL 哈希与 1.2 相同；
  - `SCENE_E2E_WORKBENCH` 和 W1 的存档对照通过；
  - 实时 GI 在 `traceOff` 下的导出与 1.2 逐像素相同

## 3. 底座

- [ ] 3.1 能力：
  - `gpu.ts` 在适配器上限之内请求存储上限，填 `SceneGPU.trace`；
  - `capacity.ts` 的能力判定和分档决策表，先写单测。

  完成判据：单测覆盖 design「分档」的每一行；兼容模式和 `forceCompatibility` 下为 `off`
- [ ] 3.2 收集与比对：`collect.ts`，先写单测（可见性、图层、透明与透射、辅助对象、W5 的替身灯，以及比对的每一类变化）；`instances.ts` 的 `slotOf`；`ModelCache.load`、`ModelCache.alias` 和 `primitiveGeometry` 写 `traceKey`。完成判据：单测通过
- [ ] 3.3 Worker 与缓存：
  - `pool.ts`、`build.worker.ts`、`cache.ts`、`normals.ts`；
  - 原始 BVH 的构建与打包成区段；缓存键、LRU、配额；请求合并。

  先写单测：法线往返、缓存键、LRU 记账、区段格式。完成判据：
  - 单测通过；
  - 参考场景冷、热两次打开，第二次全部命中缓存
- [ ] 3.4 层与同步：
  - `layer.ts`：预分配、改基址提交、顶层一个根一项、按区间上传、扩容、空闲区段；
  - `base.ts`：状态机（就绪后新几何只增加 `pending`）、`sync`、`ready`；离屏一次打包，用 `backgroundTicker` 让出，上传后释放 CPU 侧数组；
  - `tables.ts` 的材质行和灯光行。

  完成判据：
  - 每像素一条主光线，与光栅深度逐像素对照（参考场景，不符 ≤ 0.01%）；
  - 拖动、增删、换材质之后再对照一次（AC-S1-5 的用例，标注为升级时必跑）
- [ ] 3.5 代理（第 2 档）：
  - 按槽位简化，ε 级别随实例缩放；
  - 代理层与档位切换。

  完成判据：
  - 多槽位几何的槽位边界没有裂缝，材质查表正确（单测加 e2e）；
  - `traceBudget` 压低后进入第 2 档，再压低时进入「场景过大」

## 4. 命中点着色

- [ ] 4.1 包装模块与命中点：
  - `wgsl.ts` 的 `traceFirst`、`traceWithin`；
  - `hit.ts`：几何、宽度、材质 `Switch`（输入先存变量）、顶点色、光照、阴影光线、单面材质的背面。

  开始前先解决未决事项 S1-U1（环境旋转）。完成判据：`materials` 场景逐网格对照通过 AC-S1-2（材质输出；两边都关掉阴影的受光颜色；去掉半影带后阴影明暗的一致比例）

## 5. 反射补追

- [ ] 5.1 `SSRTraceNode.js`：
  - fork 自 three 0.186.1，补追没命中的采样和边缘淡出带；
  - 修好的 GGX 采样，只补追方向有限的光线；
  - `rMax` 按质量取值。

  `realtimeGI.ts` 有 `trace` 时用它。完成判据：`mirror-board` 的镜面柜映出背后的方块；俯仰序列的倒影亮度变化 ≤ 2%（AC-S1-1）
- [ ] 5.2 接入与切换：
  - `sceneRenderer.ts` 在曝光之后调 `sync`，`Look.trace` 并入 `keyOf`；
  - `Viewport.tsx` 订阅状态；`LookView` 在底座有待办时请求帧并把收敛计数归零。

  完成判据：
  - 就绪前的帧与改动前逐像素相同；
  - 视口停着时（不操作）也会在就绪后自动接上，接上后照常收敛、停止重画；
  - 测量切换那一帧（未决事项 S1-U2）

## 6. 导出、确定与时限

- [ ] 6.1 导出等待：
  - `host/render.ts` 在挂载 `lit` 外观时（`openShooter` 和 `Shooter.setLook`）等 `ready`，最多 15 s，计入 `lookBudgetMs`；
  - 超时时抛 `TraceNotReady`，`host/executor.ts` 和 `host/exportShots.ts` 给出说明。

  完成判据：
  - 隐藏标签页里两次导出多台摄像机逐像素相同（AC-S1-4），包括先拍白模、线稿再切到材质光照的默认导出；
  - `traceDelayMs` 让构建变慢时，导出按说明失败，画布上没有半成品
- [ ] 6.2 时限：
  - `engines.ts` 的单张时限加 `world`；`viewBudgetMs` 仍是常量，改按「构建这一层」的上界（`world` 的帧耗时加 15 s 等待）；
  - 在参考设备上实测每像素每帧耗时，加余量写入；
  - 单测在 `creative-scene-gi.test.ts`。

  完成判据：2K 导出和 `render_view` 在新时限内完成，首次编译在 `lookBudgetMs` 内（AC-S1-7 的时限部分）

## 7. 界面与说明

- [ ] 7.1 渲染选项卡：
  - `sceneStore` 的 `ui.trace`，经 `SceneEditor.tsx` 传给 `RenderPanel`；
  - 按 design「降级与提示」的表显示说明；渲染器说明随状态变化。

  完成判据：兼容模式（`forceWebGL`）、设备不支持（`forceCompatibility`、`traceUnsupported`）、准备中（`traceDelayMs`）、第 2 档、场景过大五种说明在 e2e 里逐一出现，编辑与导出照常（AC-S1-6）

## 8. 复杂场景与性能

- [ ] 8.1 压力场景进编辑器：
  - `scene-stress/glb.mjs` 写出 GLB，带测试；
  - e2e 经模拟服务端载入，加一面镜子映出扭结环。

  完成判据：压力场景能在编辑器里打开，并在实时 GI 下就绪
- [ ] 8.2 性能与复杂场景用例（`SCENE_E2E_TRACE_PERF`）：
  - 参考场景和压力场景的反射补追帧时间（冷、热分开）；
  - AC-S1-8 的五项。

  完成判据：AC-S1-7、AC-S1-8 的阈值；切换那一帧单独记录（S1-U2）；未决事项 S1-U3（视口与导出并存时的峰值）、S1-U4（压力场景耗时）有结论。超出阈值时先交 owner，不擅自降低画质
- [ ] 8.3 反射追代理的对照：
  - 第 1 档断言反射层为原始层；
  - 强制第 2 档，出原始与代理的并排图和差异统计。

  完成判据：证据齐全，交 owner 判断（AC-S1-9）

## 9. 收尾

- [ ] 9.1 上游报告的草稿：
  - 求交函数不能从 TSL 调用（附包装函数的做法）；
  - `ggxReflectionSample` 的 NaN（附修法）；
  - `Switch` 各分支共享表达式。

  写在 S1 的 discovery 里，交 owner 过目，同意后再提交
- [ ] 9.2 文档与证据：
  - S1 的 `discovery/research/evidence/` 整理，沿用 30 MB 规则；
  - design「实施时的调整」记录偏差；
  - 未决事项 S1-U1 到 S1-U4 写明结论
