# 光源尺寸：现状与 Blender 的做法

2026-10-26。起因是 owner 反馈：两盏灯隔着前景框架照向后墙，墙上出现三道过渡生硬的明暗带，问「阴影是不是不算光衰，多远都是硬边」。

## 现状（three r186，`develop` 411fcc80）

- **衰减是算了的**。点光和聚光的缺省值是 `decay = 2`、`distance = 0`（`frontend/src/creative-scene/state/apply.ts` 的 `lightDefaults`）。three 的 `getDistanceAttenuation`（`three/src/nodes/lighting/LightUtils.js`）在 `distance = 0` 时就是 `1 / max(d^decay, 0.01)`，即平方反比，只在 0.1 m 以内钳住。光追命中点着色（`render/trace/hit.ts` 的 `lightAt`）调的是同一个函数。平行光不衰减，这一点和 Blender 的 Sun 一样。
- **光源没有尺寸**。三种光都是理想点光源或理想平行光，文档字段里也没有半径或角径（`data: 场景文档`「灯光」）。在物理上，点光源的阴影就是一刀切的硬边。
- **阴影滤波的半径是固定的**。视口和导出都用 `shadows="percentage"`（`Viewport.tsx`、`host/render.ts`），也就是 three 的 PCF。r186 的 `PCFShadowFilter`（`three/src/nodes/lighting/ShadowFilterNode.js`）只取 5 个 Vogel 盘样本，半径为 `shadow.radius × 1 texel`，默认 radius 为 1，按屏幕噪声旋转。所以不管遮挡物离受光面多远，过渡带都只有约 1 个 texel 宽。文档灯光的阴影贴图是 1024（`objects/lights.ts`），太阳是 2048，分 3 级级联（`render/sunShadow.ts`）。
- 这是迁移 WebGPU 时有意保留的现状：`scene-webgpu-renderer` design 写着「半径 1 与现状观感几乎相同，更大的半径带屏幕噪声颗粒；实时 GI 有 TRAA 时可以加大半径，属于 R2 的调参项」，之后没有调。
- **扩展点**：
  - `ShadowNode` 先取 `shadow.filterNode`，取不到才用渲染器的阴影类型（`ShadowNode.js:479`），可以按灯换成自己的滤波函数；
  - 项目里已有三个 `ShadowNode` 子类：`VolumeShadowNode`、`VolumePointShadowNode`、`SunCascades`；
  - `renderer.library.addLight(节点类, 灯光类)` 可以为自定义灯光类注册光源节点，three 的 `examples/jsm/lights/SunLight.js` 就是这样接入的；
  - three r186 的 `WebGPURenderer` 支持 `RectAreaLight`：`RectAreaLightNode` 用 LTC，需要先用 `RectAreaLightTexturesLib` 初始化，但它不投阴影。
- **视口抗锯齿**：`workbench` 没有 TRAA，只有 `realtime_gi` 有 TRAA 和多帧累积（`render/sceneRenderer.ts`）。所以随机旋转的滤波核在 `workbench` 下会留下颗粒。

## Blender 的光源模型

| 光源 | 尺寸参数 | 缺省（bpy） | 作用 |
|---|---|---|---|
| Point | `shadow_soft_size`（界面叫 Radius） | 0.0 | 半径大于 0 时从一个球面发光，"Lights with larger size have softer shadows and specular highlights" |
| Spot | 同 Point 的 Radius；`spot_size`、`spot_blend` | — | Size 和 Blend 只控制光锥和光锥边缘，"do not control the shadow's softness" |
| Sun | `angle`：从地球看太阳的角直径 | 0.00918043 rad（0.526°） | 角径越大阴影越软 |
| Area | `shape`（SQUARE / RECTANGLE / DISK / ELLIPSE）、`size`、`size_y`、`spread` | SQUARE，0.25 m × 0.25 m，spread = π | 面光源，阴影边缘柔和；spread 模拟带格栅的柔光箱 |

- **功率单位**：点光、聚光和面光源用 W（辐射通量），不是灯泡的电功率；Sun 用 W/m²。改光源尺寸时，Blender 的总功率不变。
- `use_soft_falloff`（缺省 True）：光源几何与物体相交时加一段衰减，避免出现硬边。
- **EEVEE（4.2 起）的阴影**：
  - Virtual Shadow Map，加上 Shadow Map Raytracing，即在阴影图里朝光源形状上的采样点追几步，估出半影；
  - Jitter：每个采样抖动光源位置，重画阴影图，更准，但阴影图不能缓存，代价高，视口默认关；
  - Overblur：给 Jitter 的每个采样再加一点追踪模糊，代价是不符合物理；
  - Filter：圆形核的 PCF，超过 1 px 容易漏光。
  - EEVEE 在 PCF 的 PR 里实测：开 PCF 约 3.3 ms，只用阴影图追踪约 3.2 ms，两者开销相近。
- **Cycles**：直接在光源表面上采样，追阴影光线。

## 来源

- [Blender Manual – Light Objects](https://docs.blender.org/manual/en/4.5/render/lights/light_object.html)
- [Blender Manual – Light Settings (EEVEE)](https://docs.blender.org/manual/en/4.5/render/eevee/light_settings.html)
- [bpy.types.PointLight](https://docs.blender.org/api/current/bpy.types.PointLight.html)、[SunLight](https://docs.blender.org/api/current/bpy.types.SunLight.html)、[AreaLight](https://docs.blender.org/api/current/bpy.types.AreaLight.html)
- [EEVEE-Next: Add Shadows PCF #118220](https://projects.blender.org/blender/blender/pulls/118220)
- [EEVEE-Next: Jittered Soft Shadows #119753](https://projects.blender.org/blender/blender/pulls/119753)
