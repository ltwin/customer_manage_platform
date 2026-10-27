# Tasks

e2e 都在 `frontend/scripts/creative-scene.e2e.mjs`，用 `SCENE_E2E_ONLY` 过滤，`CREATIVE_SCENE_URL` 换端口（不用 5173、5179）。各任务先写断言，看它失败，再实现。

## 1. 数据与服务端

- [x] 1.1 灯光字段：`model.go` 的 `Light`、`lightPatch`、`lightFields`、`fields`、`completeLight`、`mergeLight`、`validateLight` 加 `radius_m`、`angular_diameter_deg`；`decodeScene` 对每盏灯补齐；`view.go` 的 `completedScene` 在灯光缺字段时返回补齐后的场景。先写测试：新增 `light_size_test.go`，包括旧快照读取补齐，仿 `TestOldCameraIsCompletedOnRead`；另外更新 `scene_test.go` 的字段列表、缺省值、换类型和错类型 400。完成判据：`make check-go PKG=./internal/creativescene/...` 通过。
  - 结果：通过。新增 `TestOldLightIsCompletedOnRead`、`TestLightSizeRanges`，`model_test.go` 补了缺字段、越界和错类型三种。物理天空 `sun_size_deg` 的缺省值改用同一个常量 `defaultSunDiameterDeg`，全项目的太阳视直径只有一个数。
- [x] 1.2 指纹与对外说明：
  - `fingerprint.go` 加 `lightModelVersion = "lights-v2"` 和 `light_model` 字段（omitempty），只在 `lit`、并且有灯光对象或天空为程序化、物理时填写；
  - `openapi.yaml` 的两个灯光 schema 加字段，`make generate` 后提交两份生成物；
  - `scene_tools.go` 的灯光字段说明补上两个字段。
  - 先写测试：`fingerprint_internal_test.go` 新增 specs.md 场景「加入光源尺寸后提示一次」的 A、B、C 三种场景，以及 `clay`、`line` 不变；更新固定的指纹；`TestEditSceneDescribesThreejsFields` 断言新字段。
  - 完成判据：两个包的 `check-go` 通过，`make generate-check` 干净。
  - 结果：通过。`TestLightModelStalesLitOnce` 覆盖 A（有灯）、B（程序化天空、无灯）、C（纯色天空、无灯）；`pinned` 的 lit 指纹按设计变化，旧值留作 `beforeLightSize`。
  - 计划外改动：`compaction_regression_test.go` 的窗口从 13000 调到 13500。这条测试给工具定义留固定空间，`edit_scene` 的说明变长后，原始历史放不下；先例是 W3 夜空那次从 9000 调到 10000。

## 2. 光源模型

- [ ] 2.1 灯光子类与注册：
  - 新增 `render/lightModel/lights.ts`：三个灯光子类、三个阴影子类（无参构造，覆写 `copy()`）、`sizeOf`；
  - 新增 `nodes.ts`，`installLightModel` 在 `gpu.ts` 建渲染器后调用；
  - `objects/lights.ts` 改用子类，写 `size`；`SceneLights.tsx` 的重建键加「尺寸 > 0」。
  - 本任务里节点只做「尺寸为 0 时原样返回父类，非 0 时乘有效距离」，不改高光和漫反射。
  - 完成判据：尺寸全为 0 的 `occluder-wall`、`frame-two-lights` 与 S0 的基线逐像素一致（这时还没修条纹）；`make check-frontend` 通过。
- [ ] 2.2 直接光：
  - 新增 `formulas.ts`：有效距离、`hybrid`、Snyder，`sinHalf` 钳在 [10⁻³, 0.9999]；
  - 新增 `direct.ts`：基础层、clearcoat 和漫反射分三份（design「灯光与阴影子类」）。
  - 先写 e2e：
    - AC-S1-3：照度、近处有限、交界线；
    - AC-S1-4：粗糙度 0.15 和 0.4 的高光板、clearcoat 板、平行光 0.545° 和 5°；参考解用 S0 的方法，取 8192 帧。
  - 完成判据：这些 e2e 在阈值内。0.4 不达标时按 proposal 调两个参数；仍不达标时停下交 owner。

## 3. 软阴影

- [ ] 3.1 聚光和点光的 PCSS：
  - 新增 `shadowFilter.ts`：`perShadow`、`mapPCSS(spotGeometry)`、`cubePCSS`、原始深度读取、`attachSoftShadow`；
  - 注释写明 U7 的两个 three 缺陷和 `Loop` 提前声明的坑。
  - 先写 e2e：
    - AC-S1-1 的聚光、点光部分（`occluder-wall` 三块板）；
    - WebGPU 和 `forceWebGL` 下带尺寸的灯着色不缺失、不全黑。
  - 完成判据：这些 e2e 通过。
- [ ] 3.2 级联与 standIn：
  - `mapPCSS(sunGeometry)`；
  - `fitCascades` 每帧同步每级的 `lightSize`，带尺寸时 `normalBias = 0`；
  - `ownNames` 后缀按投影平行光的序号；
  - 文档平行光在 `cascadeSunShadow` 之前挂滤波函数。
  - 先写 e2e：AC-S1-1 的平行光部分；级联 1–3 有阴影；改角径后最近一级的半影随之变化。
  - 完成判据：这些 e2e 通过；按 S0 的方法复测 standIn 的冷编译，记入本任务。
- [ ] 3.3 天空的太阳和月亮：
  - 程序化天空的 `Sun` 改为 `SceneSunLight`，0.545°；
  - 物理天空的 `sun`、`aside` 改为 `SceneSunLight`，取 `sun_size_deg` 或 0.52，太阳和月亮互换时 `size` 跟着换；只有 `sun` 挂滤波函数。
  - 先写 e2e：AC-S1-5，两种天空下柱子的半影；`sun_size_deg` 从 0.545° 调到 5° 时变宽。
  - 完成判据：这些 e2e 通过。
- [ ] 3.4 尺寸为 0 时的条纹：
  - 在 `VolumeShadowNode` 和 `VolumePointShadowNode` 里覆写 `setupShadowPosition`，加 1.5 texel 的法线偏置，没有 `filterNode` 时才加；
  - 同时出固定 0.05 m 的对照图。
  - 两种做法在 `occluder-wall`、`frame-two-lights` 上的图交 owner 看。owner 选定后，用 `SCENE_E2E_LIGHT_BASELINE` 重做基线并提交。
  - 完成判据：AC-S1-2 的 e2e 在两种渲染器下与新基线相差 ≤ 1/255。

## 4. 体积、累积与接触阴影

- [ ] 4.1 体积：
  - `volumes/lights.ts` 用有效距离；带尺寸的槽位读原始深度，手动做 2×2 双线性比较；
  - `volumes/state.ts` 的 `Slot`、`Candidate`、`structure`、`signatureOf` 加「是否带尺寸」，槽位写半径。
  - 先写 e2e：
    - 一盏聚光在 0 和非 0 之间切换、旁边有投影的体积时，体积着色不缺失、不全黑；
    - 尺寸为 0 时体积的画面与基线一致。
  - 完成判据：这些 e2e 通过，两个后端都测。
- [ ] 4.2 `workbench` 累积与接触阴影：
  - `engines.ts`、`host/render.ts`、`sceneRenderer.ts`：`lit` 画面里有带尺寸的投影灯时累积 16 帧，导出和回看都是；第 2–16 帧冻结阴影图，结束后恢复；
  - `realtimeGI.ts`：主光有尺寸时跳过接触阴影的追踪。
  - 先写 e2e：`workbench` 的 `lit` 连续两次导出逐像素相同；尺寸全为 0 时仍是 1 帧，画面与基线一致。
  - 完成判据：这些 e2e 通过。

## 5. 命中点

- [ ] 5.1 灯光表与命中点：
  - `tables.ts` 的 `LIGHT_ROW = 5`，`writeLight` 按 design 的表填写，平行光写 `sin(θ/2)`；
  - `hit.ts` 两处直接光改用 `formulas.ts`：受光判断和蓄水池权重用弯折后的方向；阴影光线在光源表面上采样，`reach` 停在采样点前。
  - 先写 e2e：AC-S1-6，按 GI-S1 AC-S1-2 逐网格对照，加入带尺寸的灯。
  - 完成判据：e2e 在阈值内；尺寸为 0 时与现状一致。

## 6. 编辑器

- [ ] 6.1 编辑器：
  - `state/apply.ts` 的 `lightFields` 和按类型分开的 `lightDefaults`；
  - `LightFields.tsx` 加「半径（m）」和「角径（°）」，角径不设步长；
  - `LightIcon` 加圆环：每帧写世界矩阵（世界位置、摄像机朝向、半径），`depthTest = false`。
  - 先写 e2e：
    - 面板改半径后保存，可以撤销；
    - 物件和外层分组都缩放 2 倍时，圆环在屏幕上的大小不变；
    - 内存服务器里换类型、错类型 400 的行为与服务端一致。
  - 完成判据：这些 e2e 和 `make check-frontend` 通过。

## 7. 验收材料

- [ ] 7.1 性能（AC-S1-7）：
  - 新增 4 盏灯的固定数据：`frame-two-lights` 加一盏聚光，天空为程序化天空；
  - 冷启动开关移进 `gpuTestHooks`；
  - 在参考设备上测：视口帧时、第一次构建、导出时长（1200 × 800、4K、8K、加投影体积），以及 0 与非 0 切换的停顿。
  - 完成判据：结果写进 verify，满足 Epic AC-8 的三项。超出时先查哪一项占大头，再交 owner。
- [ ] 7.2 owner 看图（AC-S1-11）：用 owner 反馈的场景出改前改后的对比图，`workbench` 和 `realtime_gi` 各一组。阴影图的台阶看得出来时，加出 2048 的对比并复测 7.1。完成判据：owner 给出结论。
