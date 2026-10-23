---
risk:
  factors: []
behavior: none
reads:
  - creative/scene
decisionsPending: false
---
# 本次交付

## Why

S0 是 Epic `scene-world-space-gi` 基线 v2（owner 2026-10-23 批准）的第一个子项。roadmap 交给 S0 五个问题，S1、S2 的预算、阈值和接法都依赖这些答案：

- 浏览器里的软件 BVH 每帧追得起多少光线；
- 设备上限够不够；
- 程序化预设改成宽度函数之后，能不能在命中点调用；
- 反射补追放在片元还是 compute；
- 压力场景下，追踪代理能否做到不损画质，一次性开销和编辑代价能否接受。

结论要回答 Epic design 的未决事项 U1、U2、U3、U5、U9，并为 AC-2、AC-3、AC-4、AC-7、AC-11、AC-12 提出阈值。这些结论经 Epic 修订回填，S1 在新版本上开工。

S0 同时为后两个子项准备对照依据：三个新参考场景、一个压力场景，以及参考设备上「本 Epic 之前」的实时 GI 基线，包括导出图、区域亮度和帧时间。AC-2、AC-3、AC-6、AC-7、AC-9 和 AC-11 都以这些数据为准。

S0 不交付契约，不改产品行为，产品代码不合入。做法与 `scene-webgpu-renderer` 的 R0 相同。

## What Changes

1. **三个新参考场景**，作为场景 e2e 的固定数据提交到 `frontend/scripts/fixtures/scene-reference/`，内容按 Epic proposal「验收标准」：
   - `backlit-room`：房间，摄像机背后有彩色物体，还有一面被阳光照亮的墙（AC-1、AC-2）；
   - `thin-wall`：墙为 2 cm 薄板的房间，墙外有阳光（AC-3 漏光）；
   - `mirror-board`：光滑地板上放镜面柜和金属球，摄像机背后有彩色物体（AC-1、AC-12 的反射部分）。

   每个场景都带导出机位，并加入 e2e 的参考场景列表。
2. **压力场景**：提交一个确定性生成器和一份规格，不提交几何文件（见 Risk Notes）。
   - 规格：约 67 个不同几何，每个约 30 万面；另加 2 个 150 万面的高面数几何，用来测单个几何的构建上限。去重后约 2000 万面，摆成约 2000 个实例的室内布局。
   - 同一个种子总是得到同样的几何。
   - 穿刺页面直接用它生成网格。GLB 写出和 e2e 载入留给 S1 的 AC-11 用例。
3. **基线**：新增一个按需运行的 e2e 用例（只在设了环境变量时运行，与现有的基线、性能用例相同）。它在参考设备的 GPU 上，把参考场景的渲染器设为实时 GI，用现有管线记录以下数据，存进 S0 的 evidence：
   - 每个参考场景的 `lit` 导出（2K），WebGPU 和 WebGL2 两个后端各一套。WebGL2 那套是 AC-6「兼容模式画面不变」的对照。
   - 视口帧时间（平均、p95、最大）。
   - 区域亮度：沿用现有的 `lumaStats`。
   - `backlit-room` 原地转动一圈的亮度序列，作为 AC-2 阈值的依据。
4. **穿刺**：按 roadmap 的 7 组清单逐项做原型，每组只做到能回答问题为止，不做产品级实现。
   - 原型在 S0 分支上一个只在开发服务器里存在的独立页面中运行，不进产品构建。
   - S0 结束时，原型源码快照存进 discovery，并从 `frontend` 删除。
5. **结论**：
   - 每组写一篇 research，包括做法、结果（可行 / 有条件可行 / 不可行）、数据和截图。
   - U1、U2、U3、U5、U9 每项给出建议方案和依据。
   - 六项 AC 的阈值，以及 ε_probe 和分档门槛的建议值，附上对照图，由 owner 选定。
6. **Epic 修订草稿**：把结论写回 Epic design 的对应章节、未决事项表和 proposal 的阈值。修订呈给 owner，批准后执行 `lti epic revise`。

## Acceptance Criteria

- **AC-S0-1 参考与压力场景**
  - **WHEN** 在参考设备上运行基线用例，**THEN** 三个新参考场景在工作台和实时 GI 下都能载入编辑器、导出，没有报错。
  - **WHEN** 用同一个种子运行两次压力场景生成器，**THEN** 两次的几何逐字节相同；去重后的面数和实例数与规格的偏差在 5% 以内。
- **AC-S0-2 基线**：**WHEN** 基线用例结束，**THEN** evidence 里有以下数据，并注明设备型号、操作系统和浏览器版本：
  - 参考场景在实时 GI 下的 `lit` 导出，两个后端各一套；
  - 视口帧时间（平均、p95、最大）；
  - 区域亮度；
  - `backlit-room` 的转动亮度序列。
- **AC-S0-3 穿刺结论**：**WHEN** S0 结束，**THEN**：
  - 7 组清单每组都有 research 记录；
  - U1、U2、U3、U5、U9 每项都有建议方案和依据；
  - AC-2、AC-3、AC-4、AC-7、AC-11、AC-12 的阈值都已提出；
  - 着色器编译和首帧耗时，热缓存与冷缓存两组数字分开写（lesson `webgpu-perf-warm-shader-cache`）。
- **AC-S0-4 画质对照**：**WHEN** 组 5 结束，**THEN**：
  - 每个候选 ε 都有「探针追代理」对照「追原始几何」的并排图和差异统计；
  - 反射追代理时，近处镜面的并排图也齐全；
  - owner 看过后选定 ε_probe 和分档门槛，选择写进 Epic 修订。
- **AC-S0-5 不合入产品代码**：**WHEN** S0 合回 `develop`，**THEN**：
  - 改动只涉及 `frontend/scripts` 下的 e2e、固定数据和生成器，以及 S0 目录下的文档、evidence 和原型快照；
  - `frontend/src`、`package.json` 和产品构建都没有变化。原型需要的 `meshoptimizer` 用现有的间接依赖，不改依赖声明。
- **AC-S0-6 回填 Epic**：**WHEN** S0 归档前，**THEN** Epic 修订草稿已呈给 owner。owner 批准后执行 `lti epic revise`，S1 在新版本基线上开工。

## Impact

- `frontend/scripts/creative-scene.e2e.mjs`：
  - 参考场景列表加入三个新场景；
  - 新增按需运行的实时 GI 基线用例。

  现有用例不改。
- `frontend/scripts/fixtures/scene-reference/`：三个新参考场景的 JSON。
- `frontend/scripts/fixtures/scene-stress/`：压力场景的规格和确定性生成器，浏览器和 Node 都能用。
- 穿刺页面和原型代码只在 S0 分支上临时存在，结束时移进 `discovery/research/`。
- S0 目录下的 `discovery/research/`：research 文档、基线图、性能数据、对照图、原型快照。
- Epic 文档：S0 结束时的修订草稿。
- 不改 `frontend/src`、后端、契约、场景格式和依赖。

## Risk Notes

- **参考设备**：沿用 R0 的本机 Mac16,8（Apple M4 Pro，Metal 4）。性能数据只在这台设备上有效。
  - 为了给分档门槛提供依据，组 2 额外在「只用 WebGPU 默认 limits」的设备上测一遍，模拟上限较低的设备：渲染器创建入口不额外请求存储上限，`maxStorageBufferBindingSize` 为 128 MiB。
  - 真实的低端设备不在 S0 范围内。
- **证据体积**：
  - 压力场景按 67 个 30 万面计算，几何会有数百 MB，不能进 git，所以只提交生成器和规格。
  - 图片沿用 R0 的规则：超过 30 MB 时只提交对照必需的部分，其余记录重新生成的命令和提交号。
- **着色器缓存**：Chromium 会跨页面缓存已编译的着色器。冷缓存的测法按 lesson `webgpu-perf-warm-shader-cache`：临时加一个每次加载都不同的随机常量，测完撤掉，并用 grep 确认已撤掉。
- **非公开接口**：材质索引写进变换槽的对齐字段、增量打包都要碰 `three-mesh-bvh` 的非公开实现。S0 只判定可行性和要扩展或 fork 的范围，不在原型之外修改依赖。
- **阻断性结论**：如果穿刺发现以下任一情况，S0 照常归档结论，由 owner 决定修订 Epic 还是调整方向，不在 S0 内私自换路线：
  - 软件 BVH 的吞吐撑不起参考场景；
  - 代理在任何可用的 ε 下都看得出差别；
  - 预设无法在保持光栅逐像素不变的前提下改成宽度函数。
- **新增决定**：
  1. 压力场景提交确定性生成器，不提交几何。
  2. 原型放在只在开发服务器里存在的独立页面，结束时移进 discovery（沿用 R0）。
  3. 实时 GI 基线用例按需运行，不进日常 e2e。
  4. 组 5（代理画质）依赖组 6 的探针原型，所以按 6 → 5 的顺序执行。
