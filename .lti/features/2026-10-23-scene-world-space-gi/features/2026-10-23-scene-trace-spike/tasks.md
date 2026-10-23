# Tasks

## 1. 参考场景、压力场景与基线

- [ ] 1.1 新参考场景：`frontend/scripts/fixtures/scene-reference/` 下新增 `backlit-room.json`、`thin-wall.json`、`mirror-board.json`（内容按 design「新参考场景」），以及区域定义 `gi-regions.json`；`referenceNames` 加入三个场景。完成判据：定向运行 R0 基线用例（输出到临时目录），九个场景都能导出、没有报错；W1 的 `SCENE_E2E_LOOKS` 用例仍通过
- [ ] 1.2 压力场景生成器：`frontend/scripts/fixtures/scene-stress/` 下的 `spec.json`、`generate.mjs`、`generate.test.mjs`。先写测试：同一个种子两次生成的几何哈希相同，去重后的面数和实例数在规格的 ±5% 以内。完成判据：`node --test` 通过，单次生成耗时记入测试输出
- [ ] 1.3 实时 GI 基线用例：`SCENE_E2E_GI_BASELINE=<dir>` 时运行，对七个参考场景完成以下各项（做法按 design「实时 GI 基线用例」），并写 `gi-baseline.json`：
  - 在 WebGPU 和 WebGL2 两个后端各导出一套 `lit`；
  - 记录视口帧时间；
  - 计算区域亮度；
  - 记录 `backlit-room` 的转动序列。

  在参考设备上运行，输出到 `discovery/research/gi-baseline/`；`gi-baseline.md` 记录设备、版本和数据，证据超过 30 MB 时按 proposal 的规则取舍

## 2. 穿刺页面与接入

- [ ] 2.1 穿刺页面骨架：`frontend/spike-trace.html` 与 `frontend/spike/trace/`。`WebGPURenderer` 直接创建，`?scene=` 载入参考场景或压力场景并画出来，`?limits=default` 和 `?cold=1` 生效；`frontend/spike/trace/measure/` 下的测量脚本能打开页面、读出适配器和设备的 limits，并截图；`vite build` 产物不含该页面。完成判据：开发服务器上 `cornell` 和 `stress` 都能显示，页面显示当前的 limits
- [ ] 2.2 组 1 接入：compute pass 接进 `RenderPipeline`、自己收集网格、`BVHComputeData` 打包、每像素一条主光线、材质索引写进变换槽、两次运行逐像素比较。结论写入 `discovery/research/integration.md`，按 design 判定标准给出结果

## 3. 穿刺：吞吐、着色与反射

- [ ] 3.1 组 2 吞吐与上限：
  - 三类光线 × 原始与代理 × 参考场景与压力场景 × 两种 limits；
  - Worker 构建和简化的耗时、IndexedDB 冷热就绪时间、显存；
  - 添加和拖动一个物件时的上传量与最长帧；
  - 增量追加能否通过扩展实现；
  - BVH 深度余量。

  结论写入 `discovery/research/throughput.md`，回答 U1 和 U9 的打包与缓存部分
- [ ] 3.2 组 3 命中点着色：
  - `wood@1`、`marble@1` 副本改为宽度函数，光栅逐像素对照；
  - compute 里调用，比较两种宽度取法；
  - 采样阴影贴图和环境贴图；
  - 同一表面上光栅与命中点着色对照；
  - 14 个预设内联后的冷、热编译耗时。

  结论写入 `discovery/research/hit-shading.md`
- [ ] 3.3 组 4 反射补追：fork `SSRNode`，A、B 两种做法都覆盖屏幕边缘淡出带；在 `mirror-board` 上检查背后物件的反射和转动时的接缝；测片元阶段读存储缓冲在两种功能级别下的可用性，以及帧时间。结论写入 `discovery/research/reflections.md`，回答 U3

## 4. 穿刺：探针与代理画质

- [ ] 4.1 组 6 探针原型：在 `cornell` 和 `thin-wall` 上跑两级级联 DDGI，替换 `A_env` 并粗调 `GI_INTENSITY`；记录漏光、收敛帧数、两级过渡、每帧耗时，以及与基线的区域亮度差。结论写入 `discovery/research/probes.md`，给出 U2 的方向和 `M` 的建议
- [ ] 4.2 组 5 代理画质：对四个候选 ε 生成代理，用 4.1 的探针分别追代理和原始几何，在三个场景上出并排图和差异图，在 `mirror-board` 上出反射追代理的并排图；统计压缩比、实际误差、平均差和超过 2/255 的比例。结论写入 `discovery/research/proxy-quality.md`，并交 owner 看图选定 ε_probe 和分档门槛

## 5. 收尾

- [ ] 5.1 组 7 阈值与汇总：汇总基线和各组数据，提出 AC-2、AC-3、AC-4、AC-7、AC-11、AC-12 的阈值及依据，汇总 U1、U2、U3、U5、U9 的建议，写入 `discovery/research/summary.md`
- [ ] 5.2 原型移出：`frontend/spike-trace.html` 与 `frontend/spike/` 整体 `git mv` 到 `discovery/research/prototype/`，并附 README。完成判据：`git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空，前端静态检查通过
- [ ] 5.3 Epic 修订草稿：在主检出的 `develop` 上回填 Epic design 的未决事项与相应关键机制、proposal 的阈值，必要时修改 roadmap，呈给 owner
