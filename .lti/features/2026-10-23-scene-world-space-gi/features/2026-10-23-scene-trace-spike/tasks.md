# Tasks

## 1. 参考场景、压力场景与基线

- [x] 1.1 新参考场景：`frontend/scripts/fixtures/scene-reference/` 下新增 `backlit-room.json`、`thin-wall.json`、`mirror-board.json`（内容按 design「新参考场景」），以及区域定义 `gi-regions.json`；`referenceNames` 加入三个场景。完成判据：「每个参考场景都能打开」的检查遍历九个场景通过；三个新场景在 GPU 上两个后端都能导出。实施时调整：新场景另列 `traceReferenceNames`（`referenceNames` 要和存档基线逐张比对），所以 R0 基线和 W1 的用例不受影响、不必重跑
- [x] 1.2 压力场景生成器：`frontend/scripts/fixtures/scene-stress/` 下的 `spec.json`、`generate.mjs`、`generate.test.mjs`。先写测试：同一个种子两次生成的几何哈希相同，去重后的面数和实例数在规格的 ±5% 以内。完成判据：`node --test` 通过，单次生成耗时记入测试输出（实测：4 个约 30 万面的曲面共 175 ms；规格共 2011 万面、60 个几何、2000 个实例）
- [x] 1.3 实时 GI 基线用例：`SCENE_E2E_GI_BASELINE=<dir>` 时运行，对七个参考场景完成以下各项（做法按 design「实时 GI 基线用例」），并写 `gi-baseline.json`：
  - 在 WebGPU 和 WebGL2 两个后端各导出一套 `lit`；
  - 记录视口帧时间；
  - 计算区域亮度；
  - 记录 `backlit-room` 的固定点转动对照（实施时由「转一圈」改为固定点，见 design）。

  在参考设备上运行，输出到 `discovery/research/gi-baseline/`；`gi-baseline.md` 记录设备、版本和数据，证据超过 30 MB 时按 proposal 的规则取舍

## 2. 穿刺页面与接入

- [x] 2.1 穿刺页面骨架：`frontend/spike-trace.html` 与 `frontend/spike/trace/`。`WebGPURenderer` 直接创建，`?scene=` 载入参考场景或压力场景并画出来，`?limits=default` 和 `?cold=1` 生效；`frontend/spike/trace/measure/` 下的测量脚本能打开页面、读出适配器和设备的 limits，并截图；`vite build` 产物不含该页面。完成判据：开发服务器上 `cornell` 和 `stress` 都能显示，页面显示当前的 limits（结果见 `discovery/research/environment.md`）
- [x] 2.2 组 1 接入：compute pass 接进 `RenderPipeline`、自己收集网格、`BVHComputeData` 打包、每像素一条主光线、材质索引写进变换槽、两次运行逐像素比较。结论写入 `discovery/research/integration.md`，按 design 判定标准给出结果（有条件可行：不符像素 ≤ 0.0002%，材质索引全部正确，确定；调用库的求交函数需要一个 WGSL 包装函数，交 owner 决定）

## 3. 穿刺：吞吐、着色与反射

- [x] 3.1 组 2 吞吐与上限：
  - 三类光线 × 原始与代理 × 参考场景与压力场景 × 两种 limits；
  - Worker 构建和简化的耗时、IndexedDB 冷热就绪时间、显存；
  - 添加和拖动一个物件时的上传量与最长帧；
  - 增量追加能否通过扩展实现；
  - BVH 深度余量。

  结论写入 `discovery/research/throughput.md`，回答 U1 和 U9 的打包与缓存部分。结果：可行。
  - 压力场景按适配器上限请求时落在第 1 档，按默认上限时落在第 2 档；
  - 不相干光线约每毫秒 7 万条；冷启动 7–9 s，热启动约 2 s；
  - 增量打包用子类扩展即可，拖动每帧上传 256 KB；
  - 遍历深度 38，上限 60。

  实施时调整：Worker 构建改用普通 Worker 池，并修正了压力场景生成器的扭结环参数（见 design）
- [x] 3.2 组 3 命中点着色：
  - `wood@1`、`marble@1` 副本改为宽度函数，光栅逐像素对照；
  - compute 里调用，比较两种宽度取法；
  - 采样阴影贴图和环境贴图；
  - 同一表面上光栅与命中点着色对照；
  - 14 个预设内联后的冷、热编译耗时。

  结论写入 `discovery/research/hit-shading.md`。结果：有条件可行。
  - 光栅像素逐值相同；
  - 宽度取光线微分加有限差分，与光栅的平均差 ≤ 0.55 级；
  - 冷编译 1.3 s。

  有两个条件交 owner：three r186 在 compute 里的补丁；产品的级联阴影覆盖不到屏幕外，建议命中点改用阴影光线。实施时调整：14 个预设全部做了副本（见 design）
- [x] 3.3 组 4 反射补追：fork `SSRNode`，A、B 两种做法都覆盖屏幕边缘淡出带；在 `mirror-board` 上检查背后物件的反射和转动时的接缝；测片元阶段读存储缓冲在两种功能级别下的可用性，以及帧时间。结论写入 `discovery/research/reflections.md`，回答 U3。结果：可行。
  - 背后的物件映进镜面柜；物件移出画面时倒影只变 0.5%（只有 SSR 时变 12%）；
  - A 与 B 出图相同，A 每帧便宜 0.3–0.8 ms，推荐 A；
  - `r_max` 建议视口 0.5、导出 1.0。

  实施时调整：只取反射项、去掉降噪（见 design）。途中发现 three 的 GGX 采样会产生 NaN 方向，以及 TSL `Switch` 跨分支共享表达式的问题，都已记入 research

## 4. 穿刺：探针与代理画质

- [x] 4.1 组 6 探针原型：在 `cornell` 和 `thin-wall` 上跑两级级联 DDGI，替换 `A_env` 并粗调 `GI_INTENSITY`；记录漏光、收敛帧数、两级过渡、每帧耗时，以及与基线的区域亮度差。结论写入 `discovery/research/probes.md`，给出 U2 的方向和 `M` 的建议。结果：可行，但 Epic 的合成和预热要改。
  - 探针相对逐像素参考：`cornell`、`backlit-room` 在 ±10% 以内，`thin-wall` 在 +4% 到 +24% 之间；现行合成偏离 31%–260%；
  - 深度矩 16 × 16 压住了薄墙的成片漏光，剩下接缝处的细亮线交 owner 看图；
  - 合成：`E_probe` 不乘 SSGI 的 `ao`，`GI_INTENSITY` 取 0（Epic 写法的最佳增益因场景而异，1 到 5 都有）；
  - 预热：先 32 帧整体替换再做均值，`M` 取 48–64；从第一帧起做均值在 96 帧内到不了 2%；
  - U2：两级 16³、第 0 级 0.25 m，近面贴摄像机，交叉淡化一格，64 条光线，稳态滞后 0.93；一帧约 10 ms，S2 需分摊。

  实施时调整：加测了 `backlit-room`；参考改为每像素多次反弹路径（见 design）
- [x] 4.2 组 5 代理画质：对四个候选 ε 生成代理，用 4.1 的探针分别追代理和原始几何，在三个场景上出并排图和差异图，在 `mirror-board` 上出反射追代理的并排图；统计压缩比、实际误差、平均差和超过 2/255 的比例。结论写入 `discovery/research/proxy-quality.md`，并交 owner 看图选定 ε_probe 和分档门槛。结果：可行，ε_probe 待 owner 看图选定。
  - 压力场景：代理为原始面数的 0.36%–2.05%，层从 894 MiB 降到 4.4–19.7 MiB；探针追代理每帧快约 1.6 倍；
  - 出图：3 mm、1 cm 超过 2/255 的像素占 0.26%、1.0%，3 cm 升到 10%；建议 ε_probe = 3 mm；
  - 实测误差有 1.5%–3.5% 的点超过 ε，最大为 ε 的 2–4 倍，AC-12 按出图衡量；
  - 参考场景全由盒子组成，代理与原始相同；`mirror-board` 反射追代理看不出差别，但能简化的物件太少，测试偏弱。

  实施时调整：参考场景简化不了，所以画质结论主要来自压力场景（见 design）

## 5. 收尾

- [x] 5.1 组 7 阈值与汇总：汇总基线和各组数据，提出 AC-2、AC-3、AC-4、AC-7、AC-11、AC-12 的阈值及依据，汇总 U1、U2、U3、U5、U9 的建议，写入 `discovery/research/summary.md`。结果：
  - 六条 AC 的阈值都给出了依据，其中 AC-3 建议改为与逐像素参考对照；薄墙房间的天花板（+24%）超出建议的 ±15%，处理方式交 owner；
  - AC-7 的探针部分要靠 S2 分摊才能进入 6 ms 的预算；
  - 需要 owner 决定的 13 项和需要修订 Epic 的地方都列在 summary.md，交 5.3；
  - 证据超过 30 MB，移出了 WebGL2 的 6 张基线图（在提交 `10759b60` 里），剩约 19 MB
- [x] 5.2 原型移出：`frontend/spike-trace.html` 与 `frontend/spike/` 整体 `git mv` 到 `discovery/research/prototype/`，并附 README。完成判据：`git diff develop --stat -- frontend/src frontend/package.json frontend/package-lock.json` 为空，前端静态检查通过。结果：已移到 `discovery/research/prototype/`，README 写明了放回 `frontend/` 重新运行的方法；`frontend/src` 与 package 文件相对 develop 无差异，`npm run lint` 与 `npm run build` 通过；各篇 research 里的原型路径已改为新位置
- [x] 5.3 Epic 修订草稿：在主检出的 `develop` 上回填 Epic design 的未决事项与相应关键机制、proposal 的阈值，必要时修改 roadmap，呈给 owner。结果：草稿已写在主检出 develop 的 Epic 工作文件里（design、proposal、roadmap、contracts，未提交），按 `summary.md` 的推荐选项填写；待 owner 确认 13 项决定后运行 `lti epic revise` 记为 v3 并提交
