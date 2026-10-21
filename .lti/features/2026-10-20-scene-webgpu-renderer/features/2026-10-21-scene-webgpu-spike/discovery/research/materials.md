# 组 2：材质、景深与正交视图

R0 任务 3.2。结论：**可行**。程序化预设、景深都能用 TSL 逐像素复现现有 GLSL；正交视图的 GTAO 需要项目内的一份改版（改两行）。

原型：`prototype/webgpu/patterns.ts`、`dof.ts`、`GTAONodeOrtho.js`，对照页 `materialsCase.ts`、`dofCase.ts`、`aoCase.ts`；测量：`prototype/measure/materials.mjs`、`dof.mjs`、`ao.mjs`、`compare.mjs`。图和数据在 `materials/`。

## 对照方法

同一组物体、灯光和机位，不开多重采样，渲染两次：一次用编辑器现有代码在 `WebGLRenderer` 上（`makeMaterial` 的 `onBeforeCompile` 注入、`DofPass`），一次用 TSL 改写在 `WebGPURenderer` 上，再在 WebGPURenderer 的 WebGL2 后端上跑一次。两边都是 ACES 加 sRGB。指标：每通道平均差（0–255）、任一通道差超过 8 的像素比例、最大差。

这里只比较移植本身，和整体光照外观（天空、环境贴图、曝光）分开，后者是 R1 的 AC-1。

## 程序化预设

更正 design 的一处说法：`wood@1` 是三维实体纹理（年轮加 fbm），不是三平面投影。三平面投影的预设是 `tile`、`stone_brick`、`wood_plank`、`fabric`。所以这里选 `tile@1`（三平面投影，含按像素宽度抗锯齿的灰缝、凹凸）和 `marble@1`（6 个八度的 fbm 噪声、按导数淡出的纹路），覆盖 `patterns.ts` 公共部分的全部函数：哈希、值噪声、fbm、`patFootprint`、`patAAStep`、`patJoint`、`patTriWeights`、`patBumpNormal`。

| 对比 | 平均差 | 差 > 8 的像素 | 最大差 |
|---|---|---|---|
| GLSL（WebGL）对 TSL（WebGPU） | 0.548 | 0.004%（35 个，都在球的轮廓上） | 58 |
| GLSL（WebGL）对 TSL（WebGL2 后端） | 0.546 | 0 | 1 |
| TSL：WebGPU 对 WebGL2 后端 | 0.002 | 0.004% | 58 |

改写要点（R1 写全部 14 个预设时照做）：

- 哈希和噪声用 `Fn(...).setLayout(...)` 生成真正的 WGSL 函数，避免每次调用都内联展开。
- fbm 的八度数是常量，在 JS 里展开，不用 `Loop` 加 `break`。
- `fwidth`、`dFdx`、`dFdy` 与 GLSL 等价；`patBumpNormal` 的表面梯度写法换到 WGSL 的 y 方向后结果不变。
- 预设输出（albedo、roughness、metalness、height）用 JS 对象传递，节点被复用时 TSL 只生成一次。
- 材质用 `MeshPhysicalNodeMaterial` 的 `colorNode`、`roughnessNode`、`metalnessNode`、`normalNode`，对应现有注入的五个锚点；放射用 `emissiveNode`。
- 对象空间位置：`varying(positionLocal.mul(modelScale))`，等价于现有的 `position * patScale`。

## 景深

`dofPass.ts` 的三步（16 像素分块取最大模糊、半分辨率 Vogel 盘采样 128 次、按模糊量混合）逐段改写为 TSL：分块和采样各是一个固定尺寸的 `rtt()`，循环用 `Loop`。参考场景 `dof` 的机位 A（50 mm，F1.4，对焦 8 m），1536 × 1024，地面铺 `tile@1` 以便看出虚化：

| 对比 | 平均差 | 差 > 8 的像素 | 最大差 |
|---|---|---|---|
| GLSL `DofPass` 对 TSL（WebGPU） | 0.522 | 0.100% | 49 |
| GLSL `DofPass` 对 TSL（WebGL2 后端） | 0.496 | 0 | 5 |

两边都有正确的虚化（`dof-tsl-webgpu.jpg`：远处的塔和近处地面虚化，对焦处方块清晰）。差异集中在虚化边缘，来自半精度纹理和采样位置的细微差别。

注意：`rtt()` 每帧只更新一次（`updateBefore` 按帧），景深也依赖组 1 的帧推进。

## 正交视图的 GTAO

three r186 的 `GTAONode` 和 WebGL 版一样，视线方向写的是 `normalize(-viewPosition)`，只适用于透视相机（另一处透视深度换算只在对数深度缓冲时生效）。`room` 场景，右前上方视角，只输出 AO：

| 相机 | 原版 GTAONode | 改版 |
|---|---|---|
| 正交 | 墙面等平面被错误地压暗一层灰（`ao-ortho-three.jpg`） | 平面纯白，只有墙角和接触处变暗（`ao-ortho-fork.jpg`） |
| 透视 | — | 与原版逐像素相同 |

WebGL 版的问题是整片 AO 发黑，现在由 `lookAlongZ` 改写着色器字符串规避。TSL 版的错误没那么明显，但仍然是错的。TSL 节点在 `setup()` 里一次构建，无法在运行时改，所以采用 Epic 定的「项目内 TSL 版 GTAO」：复制 `GTAONode.js`，加一个按相机类型确定的 `_orthographic` uniform，正交时视线方向取 `(0, 0, 1)`，共改两行。升级 three 时要和上游重新比对。

## 给 R1 的数据

- 移植误差的量级：平均差约 0.5，差 > 8 的像素不超过 0.1%，集中在轮廓和虚化边缘。R1 迁移对照的自动化阈值以此为基础，加上光照外观迁移本身的差异，在任务 5.1 里给出。
- 改写全部 14 个预设的工作量：现有 GLSL 334 行，TSL 写法行数相当，没有遇到 TSL 表达不了的写法。
