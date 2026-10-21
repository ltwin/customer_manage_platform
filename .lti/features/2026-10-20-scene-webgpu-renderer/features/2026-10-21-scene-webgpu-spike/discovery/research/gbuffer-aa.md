# 组 4：G-buffer 与抗锯齿

R0 任务 3.4，回答 Epic design 未决事项 8 的 G-buffer 部分。结论：**预通道可行，单 pass MRT 不可行**。AO（以及组 5 的 GI）所需的法线和深度，由一个只画不透明物体的预通道提供；工作台的主画面开 4× MSAA，实时 GI 的主画面不开 MSAA、用 TRAA。

原型：`prototype/webgpu/gbufferCase.ts`；测量：`prototype/measure/gbuffer.mjs`。图和数据在 `gbuffer-aa/`。

## 先确定的一点：AO 怎么作用于画面

现在 WebGL 的 `GTAOPass` 把 AO **乘在整张画面上**（直射光也被压暗）。three 官方示例的 `builtinAOContext` 只把 AO 用于间接光，观感不同。等价迁移沿用后乘：`主画面 × AO`。

## 两种做法

场景：C5 AC-9 的性能场景（196 个物件、8 种材质、太阳和 4 盏灯，其中 3 盏投影），加 6 个半透明球。帧时间为 3200 × 2000 下每帧推进的 60 帧平均。

| 做法 | 结构 | 帧时间 | 透明物体是否排除在法线/深度之外 |
|---|---|---|---|
| 预通道（工作台） | 法线/深度预通道（`samples: 0`，不画透明）→ GTAO；主画面 `samples: 4`；`主画面 × AO` | 8.0 ms | 是（`gbuffer-normals.jpg` 上） |
| 单 pass MRT（实时 GI 设想） | 一个 `samples: 0` 的 pass 同时输出颜色、法线、速度；透明材质用 `mrtNode` 写 alpha 为 0 的法线；`× AO` 后 TRAA | 8.9 ms | **否** |

单 pass 方案失败的原因：法线附件并不按 alpha 混合，透明材质写的零法线直接覆盖了后面的不透明法线（`gbuffer-normals.jpg` 中间一行是灰色圆块），AO 在那里算成 0，成图里透明球变成**黑色**（`gbuffer-mrt-picture.jpg`）。不屏蔽时透明球的法线写进去，同样不对（下一行）。PassNode 有 `opaque` / `transparent` 开关，但把透明物体单独放进另一个 pass 时没有共享深度，不能被不透明物体遮挡，所以也不是捷径。

另外，单 pass 带 MSAA 不可行：多重采样的深度纹理不能直接给 GTAO 用（见 `integration.md`）。工作台本来就需要一个不带 MSAA 的深度来源。

**预通道排除透明物体的写法**：`prePass.transparent = false`（PassNode 自带的开关），与用相机图层排除效果相同、帧时间相同（8.02 ms），不需要维护图层约定。两点注意：

- three 的「透明」只看 `material.transparent`。编辑器现在把 `opacity < 1` 或 `transmission > 0` 都算透视（`principled.ts` 的 `seeThrough`）；R1 要给透射材质也设 `transparent = true`，或者另用图层排除。
- 网格等辅助物体也不能进预通道。现在由 `overlays.aside()` 临时隐藏；R1 的网格要改写成 TSL，画在管线之后的叠层里，或者放进单独的图层。

预通道多画一遍几何，正是 WebGL 现在的结构（GTAOPass 自己再画一遍场景取法线和深度），开销相当。

## 逐材质 `mrtNode` 的约束

给某个材质设了 `mrtNode` 后，这个材质再出现在一个没有 MRT 的 pass 里（例如工作台的主画面），生成的片元输出结构为空，WGSL 编译失败（`structures must have at least one member`）。同一份材质要在两种管线里共用时，不能带 `mrtNode`，或者要按管线切换。

## MSAA 与 TRAA 的切换

两种管线交替构建、渲染、释放 12 次，没有报错，画面正确（每次新建管线，TRAA 历史不残留）。但**渲染器的纹理数每重建一次实时 GI 管线就多 3 张、不再回落**（8, 11, 11, 14, 14, 17 …；同一个场景，只重建管线）。工作台管线没有这个问题。可能是 TRAA 的历史缓冲（带深度纹理）或 GTAO 的噪声纹理没有在 `dispose()` 里释放。这些是全屏尺寸的纹理，反复切换渲染器会累积显存。R1 要么定位后手动释放，要么两条管线各建一次、切换时只改输出。

## 兼容模式设备上的 SMAA

在这台 Mac 上，以 `featureLevel: 'compatibility'` 申请适配器并且不要求 `core-features-and-limits`，Chrome 给出的设备仍是完整模式（three 判定 `compatibilityMode = false`），所以**兼容模式本身无法在本机验证**。SMAA 节点（`smaa()`，接在 `renderOutput` 之后）本身可用：放大对比 `smaa-edges.jpg`，物体轮廓明显平滑。兼容模式下 three 会把 MSAA 关掉，届时工作台改用 SMAA；这条路径要在真正的兼容模式设备上补验证，R1 先按 SMAA 实现。

## R1 须知

1. 法线/深度来自不透明预通道（`samples: 0`，`transparent = false`）；工作台主画面 `samples: 4`；AO 后乘到主画面。
2. 透射材质要标为透明；辅助物体（网格）不能进预通道。
3. 共用材质不带 `mrtNode`。
4. 重建实时 GI 管线会残留 3 张纹理，要处理。
5. 兼容模式设备上的工作台用 SMAA，本机无法验证，需要真实设备。
