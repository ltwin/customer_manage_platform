---
risk:
  factors:
    - perf-sensitive
behavior: changed
reads:
  - creative/scene
decisionsPending: false
---
# 本次交付

## Why

Epic `scene-world-environment`（批准版本 v6）目标 5「空气透视」：远处景物随距离变蓝变淡，颜色来自同一套大气模型，可以开关、调距离倍率，与全局雾、体积物件共存。W2 已交付大气 LUT、物理单位与曝光，W3 已交付月亮的光与天空视图；本次在它们之上交付空气透视（roadmap W4），完整落地 `data: ADD 空气透视`。

## What Changes

1. **数据**（`data: 空气透视`，Epic 已定义，不改）：
   - 物理天空新增 `sky.aerial_perspective`：`enabled`（缺省 true）、`distance_scale`（0.1…1000，缺省 1）。
   - 其他天空写这个字段返回 400；W4 之前存的物理天空读取时补齐缺省值，它们的 `lit` 导出各提示一次「场景已更新」。
   - 后端校验、补齐与合并；OpenAPI 的 `CreativeSceneSky` 加字段并重新生成。
2. **渲染**：
   - 每帧（摄像机或环境变化时）按 Hillaire 2020 生成空气透视 froxel：32 × 32 × 32，存成 1024 × 32 的 2D 图集，用片元 pass 生成，两个后端共用一份 TSL。
   - 在合成位置（Epic design「合成顺序」第 4 步）按不透明表面的视距查表：颜色 × 透射率 + 散射。太阳和月亮两个光源都计入；天空背景不叠加。
   - 只作用于最终渲染、`lit` 导出和 `render_view` 的 `lit`；工作台和实时 GI 两条路径都接上。
3. **编辑界面**：环境选项卡的物理天空加「空气透视」一组：开关和距离倍率（对数滑杆）。手动太阳模式下同样生效。
4. **助手**：`edit_scene` 的工具说明加 `aerial_perspective` 的字段、范围和「小场景调大倍率才看得出」。
5. **文档**：`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md`。

不在范围内：体积物件（W5）及其与空气透视合用 froxel 的问题（Epic 未决事项 2）；云影、地形遮挡造成的光束（空气透视不算阴影）。

## Acceptance Criteria

- **AC-W4-1 远处变蓝变淡**：**WHEN** 物理天空正午、空气透视开启、距离倍率 100，场景里有近、远两个同色方块（相距约 100 m），**THEN** 远处方块比近处的对比度更低、更偏蓝；改时刻（黄昏）或大气参数（雾霾）时颜色随之变化，黄昏偏暖、雾霾更白。
- **AC-W4-2 距离倍率**：**WHEN** 倍率从 1 调到 100，**THEN** 同一场景里远处方块与近处方块的亮度差明显变小（e2e 记录数值）。
- **AC-W4-3 关闭即无**：**WHEN** 关闭空气透视，**THEN** 画面与 W3 逐像素一致（W3 的「day unchanged」基线在关闭时通过）。
- **AC-W4-4 天空不叠加**：**WHEN** 开启空气透视，**THEN** 天空背景的像素不变，只有表面变化。
- **AC-W4-5 两个后端**：**WHEN** 在 WebGPU 和 WebGL2（SwiftShader）下渲染 AC-W4-1 的场景，**THEN** 两张图的平均差不超过 W2 定的阈值。
- **AC-W4-6 数据与过时**：
  - **WHEN** 写 `aerial_perspective` 的部分字段，**THEN** 其余补缺省值；越界或在非物理天空上写返回 400，并写明字段。
  - **WHEN** 改开关或倍率，**THEN** 物理天空下的 `lit` 导出提示「场景已更新」，`clay`、`line` 不提示。
  - **WHEN** 读取 W4 之前存的物理天空，**THEN** 补齐为开启、倍率 1。
- **AC-W4-7 助手**：**WHEN** 助手读工具说明，**THEN** 能写开关和倍率，并知道小尺度场景要调大倍率。
- **AC-W4-8 确定性**：**WHEN** 同一场景连拍两次，**THEN** 逐像素一致。
- **AC-W4-9 性能**：**WHEN** 在参考设备（M4 Pro）上渲染 W3 的性能场景，**THEN** 开启空气透视的静止帧时间比关闭时多不超过 1 ms，拖动时刻的帧率不低于 W3 实测值减 2 fps。

## Impact

- 前端：`world/atmosphere/`（froxel pass 与查表）、`render/sceneRenderer.ts`、`render/realtimeGI.ts`、`objects/Lighting.tsx`、`world/physicalSky.ts`、`panels/PhysicalSkyFields.tsx`、`state/apply.ts`。
- 后端：`creativescene` 的 `model.go`、`sky.go` 及测试；`creativeagent/scene_tools.go`。
- 契约：`api/openapi.yaml`（`CreativeSceneSky`）及两份生成物；交付 `data: ADD 空气透视`。
- 已存的物理天空场景：画面变化，`lit` 导出提示一次过时（Epic 已接受的预期行为）。

## Risk Notes

- **性能**（perf-sensitive）：每帧多一个 32 768 像素的片元 pass 和一次全屏查表。依据 W2 的 LUT 实测（改时刻 0.3 ms），预计在 1 ms 以内；以 AC-W4-9 实测为准。
- **透明物件**：查表用不透明表面的深度，玻璃取它后面表面的空气透视，与实时 GI 路径里全局雾的现有做法一致，记为局限。
- **小场景看不出**：真实尺度下 100 m 内的空气透视很弱，倍率 1 时院子场景几乎没有变化，这是物理上正确的结果；靠距离倍率放大，工具说明里写明。
- 不改 Epic 共享设计；按 Epic 未决事项给出的 W4 局部决定（froxel 存储方式、合成接入点）写在 design「局部设计与偏差」。
