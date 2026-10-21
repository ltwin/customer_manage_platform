# 组 3：天空、阴影与描边

R0 任务 3.3，回答 Epic design 未决事项 4，并为未决 5 补上阴影部分。结论：**可行**。天空和环境光照逐像素复现；阴影用 PCF 半径 1 与现状一致；选中描边建议用反向外壳（drei `Outlines` 的 TSL 移植版）。

原型：`prototype/webgpu/sky.ts`、`skyCase.ts`、`shadowCase.ts`、`outlineCase.ts`；测量：`prototype/measure/sky.mjs`、`shadow.mjs`、`outline.mjs`。图和数据在 `sky-shadow-outline/`。

## 天空

编辑器现在有**两种**天空，算法不同，R1 都要改写：

| 外观 | 现在用的 | 与 three `Sky.js` 的区别 |
|---|---|---|
| 白模 | drei `Sky`，即 three-stdlib 的旧版天空，`up = (0, 0, 1)` | `rayleigh` 默认 0.5；太阳圆盘系数 19000；`sunfade` 读视图空间的 y（原实现的怪癖）；视线方向从世界原点出发；输出多一条 `pow(c, 1 / (1.2 + 1.2·sunfade))` 曲线 |
| 最终渲染 | `render/sky.ts` 的 `viewSky()`：three `Sky.js` 改写着色器字符串 | 视线方向换到文档空间、去云、输出乘 `SKY_GAIN` |

TSL 改写 `sky.ts`：`litSky()` 以 three 的 `SkyMesh`（`Sky.js` 的 TSL 版，散射模型和常数相同）为底，`claySky()` 逐行移植旧版天空。正午和黄昏、朝向太阳和背向太阳，共 8 组对比，**最大差都是 1**（平均差 0.002–0.34）。

**环境光照**：最终渲染用天空（不画太阳圆盘）生成 PMREM 环境贴图来照明和反射。WebGPU 的 `PMREMGenerator.fromScene` 加 `litSky()`，与现有 `skyEnvironment()` 对比：三个球（粗糙、光滑电介质、抛光金属）只受环境光照，**球体区域最大差 1**。整图差异大只是因为对照装置里旧版直接渲染到画布，纯色背景没经过色调映射；编辑器现在走 EffectComposer 加 OutputPass，背景同样会被色调映射，与 WebGPU 管线一致。

## 阴影

r186 的 `WebGPURenderer` 没有 `PCFSoftShadowMap`（见 `environment.md`）。节点阴影的 PCF 是 5 个样本的 Vogel 盘，半径由 `shadow.radius` 控制，按屏幕噪声旋转。白模灯光（`ClayLights`）加拟合后的太阳光，2048 阴影贴图，与 WebGL 的 `PCFSoftShadowMap` 对比（裁图 `shadow-noon-edges.jpg`）：

| WebGPU 做法 | 观感 |
|---|---|
| PCF，半径 1（默认） | **与 PCFSoft 几乎相同**：同样的锯齿状软边，球底同样有法线偏移造成的一道亮缝 |
| PCF，半径 2、3 | 更软，半影里有屏幕噪声的颗粒 |
| VSM，模糊 8 样本 | 最平滑，球底亮缝消失，观感明显不同 |

等价迁移用 `PCFShadowMap`、`radius = 1`。实时 GI 有 TRAA 时，更大的半径加上逐帧变化的噪声可以被平均掉，属于 R2 的调参项。（整图差异指标在这里不适用：对照装置里旧版的纯色背景没经过色调映射，同上。）

阴影包围盒拟合（`sunShadow.ts`）原样可用，见 `integration.md`。

## 选中描边（未决 4）

编辑器现在：立体物件用 drei `Outlines`（反向外壳；drei 的 `screenspace` 命名是反的，默认 false 时实际按屏幕像素偏移，3 px），平面物件用 drei `Edges`（2 px 粗线）。两者都依赖 ShaderMaterial，必须替换。

院子场景加一根 2 cm 粗的细杆、一个两块方块的组和一块 1 mm 厚的平板，选中球、细杆和组，2400 × 1600（`outline-compare.jpg`，帧时间来自每帧推进的 120 帧平均）：

| 做法 | 观感 | 帧时间（无描边 0.4–0.5 ms） |
|---|---|---|
| 反向外壳：drei `Outlines` 的顶点着色器移植为 TSL（`vertexNode` 在裁剪空间沿法线外推 3 px，背面、纯色） | 清晰的 3 px 线，细杆、球、组都有；有棱角的方块在转角处有缺口，现有编辑器同样如此 | 测不出额外开销 |
| three `OutlineNode`（`downSampleRatio: 1`，边宽 1.5，强度 3） | 默认是偏淡的辉光，细杆几乎看不出；描整个选中集合的外轮廓，也显示被遮挡的部分 | 多约 0.7 ms |
| 平板边线：`three/addons/lines/webgpu` 的 `LineSegments2` + `Line2NodeMaterial`，2 px | 与 drei `Edges` 相同 | — |

**建议**：R1 用反向外壳加 `LineSegments2`，与现状一致、几乎零开销、细物件可靠。OutlineNode 的组外轮廓和被遮挡部分是新功能，要另行调参，不在等价迁移的范围内。

## R1 须知

1. 两个天空各一个 TSL 函数，参数与现状一致；升级 three 时与上游 `SkyMesh` 重新比对。
2. 最终渲染的环境贴图用 WebGPU 的 `PMREMGenerator.fromScene`，与现状一致。
3. 阴影：`PCFShadowMap`，`radius = 1`，`bias = 0`，保留 `fitSunShadow`。
4. 选中描边：反向外壳（TSL `vertexNode`）加 `LineSegments2` 粗线，替换 drei `Outlines` 和 `Edges`。
