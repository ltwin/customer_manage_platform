---
risk:
  factors:
    - perf-sensitive
    - broad-impact
behavior: changed
reads:
  - creative/scene
decisionsPending: false
---
# 本次交付

## Why

S1 是 Epic `scene-light-size` 的第二个子项，依据批准基线 v3：v2 是 owner 2026-10-26 看过 S0 的对比图后的修订，v3 加上 U8 的结论。Epic 的目标、光源模型、软阴影做法和 AC 阈值都在 Epic 的 proposal 和 design 里，这里不重复。

S1 是 Epic 的最小贯通场景：点光、聚光、平行光，以及程序化天空和物理天空的太阳、月亮，都带上尺寸。完成后，owner 2026-10-26 反馈的场景（两盏灯隔着框架照墙）的阴影会随距离变软，高光随光源变大。这一批同时打通整条链路：数据字段、光照模型扩展、软阴影的四个接入点、命中点着色和编辑器。S2 的面光源在这条链路上加一种类型。

S1 完整交付 Epic 的目标契约 `data: 光源尺寸`（`epic bind --deliver`）。

## What Changes

按 Epic roadmap 的 S1 行，逐项对应 Epic design 的章节：

1. **数据与服务端**：
   - 点光和聚光加 `radius_m`，平行光加 `angular_diameter_deg`，按 `data: 光源尺寸` 校验、补缺省值、换类型时保留或去掉；
   - 读取旧快照时补齐；
   - `lit` 指纹在有灯光对象或太阳会投影时计入光源模型版本 `lights-v2`；
   - OpenAPI 加这两个字段，提交 Go 和 TS 两份生成物；
   - 助手和外部助手共用的场景工具说明里补上这两个字段。
2. **光照模型**（Epic design「直接光的入口」）：
   - 新目录 `render/lightModel/`，扩展 `PhysicalLightingModel`：
     - 球和圆盘的高光用混合模型，clearcoat 用自己的粗糙度；
     - 漫反射和 sheen 用 Snyder 照度；
   - 灯光子类 `SceneSphereLight`、`SceneSpotLight`、`SceneSunLight` 经 `addLight` 注册各自的光源节点；
   - 衰减改用有效距离 `max(d, r)`。
3. **软阴影**（Epic design「软阴影的接入」）：
   - PCSS 滤波，2D 和立方体两套；
   - 四个接入点：聚光、点光、级联、standIn；
   - 带尺寸的灯的阴影图去掉比较、改为最近点过滤；
   - 接收面深度偏置；核随帧旋转；
   - 尺寸在 0 和非 0 之间切换时重建阴影节点；
   - 级联 `ownNames` 的后缀改为按序号。
4. **尺寸为 0 时的条纹**：修复聚光和点光尺寸为 0 时的自阴影条纹，作为 AC-2 的新基线。
5. **噪声与接触阴影**：
   - `workbench` 的最终导出加时域累积（U2）；
   - 主光有尺寸时关掉实时 GI 的屏幕空间接触阴影（U4）。
6. **天空的太阳和月亮**：程序化天空的太阳、物理天空的太阳和月亮改用 `SceneSunLight`。角径分别取 0.545°、`sky.sun_size_deg` 和 0.52°。
7. **命中点**：
   - 灯光表每行扩到 5 个 vec4，一次定到能放下 S2 的面光源；
   - 命中点用同一套公式着色，阴影光线在光源表面上采样。
8. **体积**：有尺寸的光源在体积里仍按中心点计算，衰减按 I2 钳在 `max(d², r²)`。
9. **编辑器**：
   - 属性面板加「半径（m）」「角径（°）」；
   - 点光和聚光的图标画出半径球，抵消物件缩放；
   - `lightDefaults` 改成按类型分开的表。
10. **验证**：e2e 断言、性能复测、owner 反馈场景的改前改后对比图，见「验收标准」。

**不在本次**：
- 面光源及其 LTC 和阴影，属于 S2；
- 体积里的软阴影、拖拽改半径，是 Epic 的非目标。

## Acceptance Criteria

阈值都取自 Epic proposal（修订 v2）。数据在参考设备上测得，对照用 S0 的参考解和参考场景（`occluder-wall`、`frame-two-lights`）。下文「Epic AC-n」指 Epic proposal 的同名条目，S1 只验收其中点光、聚光、平行光和太阳的部分。

- **AC-S1-1 半影随距离变宽**（Epic AC-1）：WHEN 聚光、点光和平行光分别带上尺寸，照遮挡板和墙，遮挡板离墙 0.05、0.5、3 m，THEN 与参考解的差异满足 Epic AC-1 的三项阈值，并且遮挡物贴墙处半影不超过 2 个 texel。
- **AC-S1-2 尺寸为 0**（Epic AC-2）：WHEN 半径和角径都写成 0，THEN 这些灯不挂滤波函数，光源节点原样走 three 的点光源路径，着色器与修复条纹后的基线相同。光栅输出与该基线逐通道相差不超过 1/255。修复后的基线图交 owner 看过，再作为基线提交。
- **AC-S1-3 平方反比与包裹**（Epic AC-3）：
  - WHEN `decay = 2`、`distance = 0`，在点光或聚光的法线方向上取 d ≥ 10r 处，THEN 照度与 `I/d²` 相差不超过 2%；
  - d < r，或 r = 0、d < 0.1 m 时，照度有限；
  - 明暗交界线与参考解相差不超过 2°。
- **AC-S1-4 高光**（Epic AC-4）：
  - WHEN 球形光源半径为 0.1、0.3、1 m，在粗糙度 0.15 和另一档粗糙度（0.4）的高光板上测；
  - THEN 与参考解比，总能量偏差 ≤ 10%，面积偏差 ≤ 20%，峰值偏差 ≤ 25%；
  - 另加一块 clearcoat 高光板（基础层粗糙度 0.5、clearcoat 0.05），clearcoat 层同样满足上面三项；
  - 0.4 不达标时，先调混合模型的两个参数（1.5、0.6），在两档粗糙度上都复测；调不到时交 owner 决定。
- **AC-S1-5 天空的太阳和月亮**（Epic AC-6）：
  - WHEN 程序化天空或物理天空的太阳投影，THEN 在最近一级级联里测得的半影宽度按角径计算，允许 1 个 texel 的误差；
  - 物理天空把 `sun_size_deg` 调到 5° 时，阴影随太阳盘一起变软。
- **AC-S1-6 命中点一致**（Epic AC-7）：WHEN 实时 GI 的屏幕外反射映出被有尺寸的点光、聚光或平行光照亮的表面，THEN 命中点着色与光栅的差异不超过 GI-S1 AC-S1-2 的同类阈值。
- **AC-S1-7 性能**（Epic AC-8 的 S1 部分）：
  - 场景：参考设备，1200 × 800，`frame-two-lights`（已有平行光和点光）加一盏聚光，天空为程序化天空，即聚光、点光、平行光、太阳各一，4 盏投影的有尺寸光源；
  - 对照：同一场景尺寸全为 0；
  - WHEN 冷启动测量，THEN 满足 Epic AC-8（修订 v4）：视口每帧增量 ≤ 8 ms，`realtime_gi` 导出时长增量 ≤ 50%；第一次构建的增量和 `workbench` 导出时长的增量按实测记录进 verify；
  - `workbench` 的导出时长另在 4K、8K 和加一个投影体积的同一场景上测，结果记录进 verify；
  - 另外实测尺寸在 0 和非 0 之间切换时视口的停顿，记录在 verify 里；
  - 含面光源的复测属于 S2。
- **AC-S1-8 编辑器与助手**（Epic AC-9 的 S1 部分）：
  - 属性面板能编辑半径和角径，撤销可用；
  - 点光和聚光的图标画出半径球，缩放物件时球的大小不变；
  - 助手按工具说明写入新字段；
  - 点光写 `angular_diameter_deg`、平行光写 `radius_m` 返回 400，并指出字段；
  - 点光、聚光和平行光之间换类型时，按 `data: 光源尺寸` 保留、去掉或补齐字段。
- **AC-S1-9 存量场景**（Epic AC-10）：
  - WHEN 读取本 Epic 之前的场景，THEN 灯光补齐缺省值，不迁移；
  - 有灯光对象，或天空是程序化天空或物理天空的场景，已有的 `lit` 导出各提示一次「场景已更新」；
  - HDRI 或纯色天空、又没有灯光的场景不提示；`clay` 和 `line` 不受影响。
- **AC-S1-10 防回退**：
  - WebGPU 和 WebGL2 各有一个 e2e，检查带尺寸的灯所在之处着色正确，不缺失、不全黑（U7 的两个 three 缺陷）；
  - 平行光的级联 1–3 仍有阴影（`Loop` 的坑）；
  - `workbench` 最终导出连续两次逐像素相同；
  - 物理天空调 `sun_size_deg` 后，级联的半影随之变化；
  - 灯在 0 和非 0 之间切换时，投影体积的着色不缺失、不全黑。
- **AC-S1-11 owner 看图**（Epic AC-11）：用 owner 2026-10-26 反馈的场景出改前改后的对比图，`workbench` 和 `realtime_gi` 各一组，由 owner 判断是否接受。阴影图的台阶看得出来时，试 2048 的阴影图并复测 AC-S1-7（S0 `thresholds.md`）。

## Impact

- **服务端**：`backend/internal/creativescene`（灯光字段、补齐、指纹）、`backend/internal/creativeagent`（工具说明）、`api/openapi.yaml` 和两份生成物。
- **前端渲染**：
  - 新增 `frontend/src/creative-scene/render/lightModel/`；
  - 改动 `objects/lights.ts`、`objects/Lighting.tsx`、`render/sunShadow.ts`、`render/volumes/`、`render/sceneRenderer.ts`、`render/realtimeGI.ts`、`render/trace/tables.ts`、`render/trace/hit.ts`。
- **编辑器**：`panels/LightFields.tsx`、`objects/SceneObjects.tsx`（`LightIcon`）、`state/apply.ts`。
- **e2e**：`frontend/e2e/` 的场景 e2e，用 S0 提交的参考场景。
- **契约**：交付 `data: 光源尺寸`；Living Spec 加「光源尺寸与软阴影」，并修改「导出图提示场景已更新」（见 specs.md）。
- **画面**：本 Epic 之前有灯光或太阳投影的场景，打开后阴影变软、高光变大。这是要修的问题（Epic D2），存量的 `lit` 导出各提示一次。

## Risk Notes

- **U8 已解决**：owner 2026-10-27 在 Blender 里核对，点光和聚光缩放物件时发光半径不变，与 Epic 原定的「不随缩放」一致（Epic 修订 v3）。
- **高光参数只拟合过一档粗糙度**：AC-S1-4 加测 0.4。调参在 S1 范围内；两档都调不到阈值时，交 owner 决定。
- **性能只在两盏灯上测过**：AC-S1-7 在 4 盏灯上复测。超出时先减少级联的滤波样本，再交 owner。不悄悄降低画质（记忆「画质优先于性能」）。
- **合并顺序**：GI-S2 `scene-probe-gi` 已合入 develop，S1 以当前 develop 为底开工。合回前按 `make check-merge` 处理（Epic design「合并顺序」）。
- **three r186 的绕法**：U7 的两个缺陷、`Loop` 提前声明、`addLight` 不能覆盖内置灯，都靠绕开，各加注释和 e2e 防回退。以后升级 three 时，先跑这几条 e2e。
