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

S0 是 Epic `scene-light-size` 基线 v1（owner 2026-10-26 批准）的第一个子项。软阴影有好几种做法，近似误差、噪声和后端可行性各不相同。按记忆「画质优先于性能」，近似手段要先给 owner 看图再定。所以 S1 和 S2 开工前，S0 要先回答 Epic design 的未决事项 U1–U8，并为 AC-1、AC-3（包裹）、AC-4、AC-5、AC-8 提出阈值。结论经 Epic 修订回填，S1 在新版本上开工。

S0 同时为后两个子项准备对照依据：参考场景、本 Epic 之前的基线图，以及光追参考解的生成方法。

S0 不交付契约，不改产品行为，产品代码不合入。做法与 `scene-world-space-gi` 的 GI-S0 相同。

## What Changes

1. **参考场景**，作为固定数据提交到 `frontend/scripts/fixtures/scene-reference/`，新开一个列表 `lightReferenceNames`，不放进 `referenceNames`，现有基线不受影响：
   - `frame-two-lights`：复制 owner 2026-10-26 反馈的原始场景，即画布 `cccv_3fa2de7c-…` 上的「参考还原｜工业科幻反应舱」（AC-11）。左上那盏是投影的平行光，右下那盏是投影的点光；两个模型实例按代码模型放进固定数据；
   - `occluder-wall`：三块遮挡板分别离墙 0.05、0.5、3 m，一盏有尺寸的聚光、一盏点光和一个平行光可以分别打开，另有一个球体，用来看包裹效果（AC-1、AC-3、AC-4）。

   两个场景只用现有字段，产品编辑器能直接打开。光源尺寸由穿刺页面按 URL 参数设置。面光源棚拍场景要等 S2 有了 `area` 类型才能存成固定数据，S0 先把它写成穿刺页面里的用例（见 design「局部设计与偏差」）。
2. **基线**：用现有的按需基线用例，记录两个新场景在 `workbench` 和 `realtime_gi` 下的 `lit` 导出，存进 S0 的 evidence。
3. **穿刺**：按 Epic roadmap 的 10 组清单逐项做原型，每组只做到能回答问题为止。
   - 原型放在只在开发服务器里存在的独立页面（`frontend/spike-light.html`、`frontend/spike/light/`），不进产品构建。
   - 页面可以 import 产品模块，例如 GI-S1 的光追底座，但不修改它们。
   - S0 结束时，原型源码快照移进 discovery，并从 `frontend` 删除。
4. **Blender Cycles 对照**（清单第 8 组）：本机没有 Blender，owner 2026-10-26 决定记为后续验收项，以后在另一台机器上验收，这一轮由 owner 自己看效果。S0 交付 `blender-cycles.md`：搭建步骤、渲染设置，以及参考解给出的点光改半径时四种变化的预期值。U8 先按 Epic 暂定的「不随缩放」推进。
5. **结论**：
   - 每组写一篇 research：做法、结果（可行 / 有条件可行 / 不可行）、数据和对照图；
   - U1–U8 每项给出建议和依据；
   - 提出各项 AC 的阈值；
   - 做一份对比图集，交 owner 看图选定软阴影的做法。
6. **Epic 修订草稿**：结论写回 Epic design 的对应章节、未决事项表和 proposal 的阈值，呈给 owner，批准后执行 `lti epic revise`。

## Acceptance Criteria

- **AC-S0-1 参考场景**：**WHEN** 运行「每个参考场景都能打开」的检查，**THEN** 两个新场景在 `workbench` 和 `realtime_gi` 下都能载入编辑器并导出，没有报错。
- **AC-S0-2 基线**：**WHEN** 基线用例结束，**THEN** evidence 里有两个新场景在两种渲染器下的 `lit` 导出，并注明设备、操作系统和浏览器版本。
- **AC-S0-3 参考解**：
  - **WHEN** 穿刺页面对球、圆盘、矩形和椭圆四种光源生成参考解；
  - **THEN** 第 1024 帧和第 2048 帧的差异每通道不超过 1/255；
  - 并且在 `occluder-wall` 的墙上，离球形光源 10 倍半径以外的照度与解析式 `I/d²` 的相对误差不超过 1%；
  - 并且 `blender-cycles.md` 写明了 `occluder-wall` 的搭建步骤和 Cycles 渲染设置，以及参考解给出的半影宽度、远处照度比、交界线位置和高光面积比的预期值，供后续验收对照。
- **AC-S0-4 穿刺结论**：**WHEN** S0 结束，**THEN**：
  - 10 组清单每组都有 research 记录；
  - U1–U8 每项都有建议和依据；
  - AC-1、AC-3（包裹）、AC-4、AC-5、AC-8 的阈值都已提出；
  - 着色器编译时间和首帧耗时，热缓存和冷缓存分开写（lesson `webgpu-perf-warm-shader-cache`）。
- **AC-S0-5 owner 看图**：**WHEN** 第 2 组结束，**THEN** 每种候选做法都有：
  - `frame-two-lights` 和 `occluder-wall` 上与参考解的并排图；
  - `workbench` 和 `realtime_gi` 下各一组；
  - 误差统计和每盏灯的耗时。

  owner 看过后选定做法，选择写进 Epic 修订。
- **AC-S0-6 不合入产品代码**：**WHEN** S0 合回 `develop`，**THEN**：
  - 改动只涉及 `frontend/scripts` 下的固定数据和 e2e 列表，以及 S0 目录下的文档、evidence 和原型快照；
  - `frontend/src`、`package.json` 和产品构建都没有变化。
- **AC-S0-7 回填 Epic**：**WHEN** S0 归档前，**THEN** Epic 修订草稿已经呈给 owner；owner 批准后执行 `lti epic revise`。

## Impact

- `frontend/scripts/fixtures/scene-reference/`：新增两个场景的 JSON。
- `frontend/scripts/creative-scene.e2e.mjs`：
  - 新增 `lightReferenceNames`，并入「每个参考场景都能打开」的检查和基线用例；
  - 现有用例和存档的基线都不改。
- 穿刺页面和原型代码只在 S0 分支上临时存在，结束时移进 `discovery/research/`。
- S0 目录下的 `discovery/research/`：research、基线图、对照图、性能数据、Blender 文件、原型快照。
- 不改 `frontend/src`、后端、契约、场景格式和依赖。

## Risk Notes

- **参考设备**：沿用本机 Mac16,8（Apple M4 Pro）。性能数据只在这台设备上有效。
- **Blender**：本机不装 Blender（owner 2026-10-26）。对照记为后续验收项，在这之前，参考解是唯一的真值。如果参考解本身有系统性偏差，要到后续验收时才会发现。为降低这个风险，AC-S0-3 要求参考解自身收敛，并用解析式核对远处照度 `I/d²`。
- **WebGL2 读取原始深度**（U7）：这一项可能只能在其中一个后端做到。到那时 S0 照常归档结论，由 owner 决定走额外的线性深度输出，还是经 Epic 修订改成只支持 WebGPU，不在 S0 内私自换路线。
- **着色器缓存**：冷缓存的测法按 lesson `webgpu-perf-warm-shader-cache`。
- **重建材质**：灯的组合变了，three 才会重建受光材质（lesson `three-rebuild-materials-by-lights`）。穿刺页面切换候选做法时，按这个 lesson 换灯或重建页面，否则对比的仍是旧着色器。
- **证据体积**：沿用 GI-S0 的规则，超过 30 MB 时只提交对照必需的部分，其余记录重新生成的命令和提交号。
- **新增决定**：
  1. 面光源棚拍场景先写成穿刺页面里的用例，S2 有了 `area` 类型再存成固定数据。
  2. 两个新参考场景单独列为 `lightReferenceNames`，不进 `referenceNames`，现有的存档基线不必重跑。
  3. Blender Cycles 对照改为后续验收项（owner 2026-10-26）。
