# 组 1：接入与帧推进

R0 任务 3.1，回答 Epic design 未决事项 5。结论：**可行**，有三个条件写进 R1 设计（见文末「R1 须知」）。

原型：`prototype/webgpu/integration.tsx`、`frames.ts`；测量：`prototype/measure/integration.mjs`（Playwright）、`hidden.mjs`（真实 Chrome 后台标签页）。数据和截图在 `integration/`。

## R3F 9.8.1 接入

| 项 | 结果 |
|---|---|
| `<Canvas gl={async (props) => { const r = new WebGPURenderer(...); await r.init(); return r }}>` | 可行，WebGPU 和 `forceWebGL` 两个后端都正常 |
| 按需渲染（`frameloop="demand"`） | 可行：空闲 1.5 秒绘制次数不变（3 → 3），两指环绕半秒后为 128 |
| 离屏根（`createRoot(canvas)` + `configure({ gl: 异步工厂, frameloop: 'never' })` + `render`） | 可行，与现有 `openShooter` 的结构一致 |
| `CameraControls`（drei） | 可行 |
| `GizmoHelper` 的二次渲染 | **有条件可行**，见下 |
| `GizmoViewport`（drei） | 调用 `gl.capabilities.getMaxAnisotropy()`，WebGPURenderer 没有 `capabilities`，直接报错。补一行垫片（`renderer.capabilities = { getMaxAnisotropy: () => renderer.getMaxAnisotropy() }`）后可用 |

### Gizmo 叠加在管线输出上

编辑器的做法是：主画面在 `useFrame` 优先级 1 渲染，Gizmo 用 drei 的 Hud 在优先级 2 以 `autoClear = false`、`clearDepth()` 再渲染一次叠上去。WebGPU 下实测：

| 画布 MSAA | 渲染器做色调映射和 sRGB | 结果 |
|---|---|---|
| 开（`antialias: true`） | 是 | Gizmo 画出来，主画面被清掉（黑） |
| 开 | 否（管线输出节点里做） | 同上 |
| 关 | 是 | 同上 |
| 关 | 否 | **正确叠加** |

原因（`Renderer.js`、`WebGPUBackend.js`，r186）：

- 渲染器设了色调映射或输出色彩空间不是工作空间时（`needsFrameBufferTarget`），每次画到画布都先画进内部帧缓冲再整体输出，后一次输出覆盖前一次。
- 画布开 MSAA 时，颜色画进多重采样缓冲再 resolve 到画布纹理，后一次 `load` 拿不到前一次的内容。

可行组合：画布 `antialias: false`；渲染器 `toneMapping = NoToneMapping`、`outputColorSpace = LinearSRGBColorSpace`；管线 `outputColorTransform = false`，输出节点显式 `renderOutput(节点, ACESFilmicToneMapping, SRGBColorSpace)`。代价是 Hud 里的 Gizmo 不经 sRGB 编码，颜色偏深（截图 `viewport-start.jpg`），R1 要把它的颜色预先换算，或者把 Gizmo 改成管线里的一个通道或 HTML 叠层。工作台的 MSAA 改在 `pass()` 上开（`samples: 4`），不在画布上。

### drei 组件兼容性（编辑器现在用到的）

| 组件 | 底层 | WebGPU |
|---|---|---|
| `CameraControls`、`OrbitControls` | DOM 事件 | 可用 |
| `GizmoHelper` | Hud：`gl.render`、`clearDepth` | 有条件可用（上节） |
| `GizmoViewport` | `gl.capabilities` | 加垫片可用 |
| `TransformControls` | three-stdlib，基础材质 | 可用（R1 实测交互） |
| `Edges` | drei `Line` → three-stdlib `LineMaterial`（ShaderMaterial） | **不可用**，改 `three/addons/lines/webgpu` 的 `LineSegments2` + `Line2NodeMaterial` |
| `Outlines`、`Grid` | `shaderMaterial`（GLSL） | **不可用** |
| `Sky` | three-stdlib `Sky`（ShaderMaterial） | **不可用**（Epic 已定 fork TSL 天空） |

## 帧推进与确定性

`frames.ts` 的做法：

- `renderer._animation.stop()` 停掉渲染器内部的 rAF 循环；
- 每张图开始时 `nodeFrame.frameId = 0`、`time = 0`；
- 每帧 `frameId += 1`、`time += 1/60`，同步写 `renderer.info.frame`。

节点帧对象是私有接口，R1 照 Epic 的约定封装在渲染器创建入口里，并配单元测试防升级时失效。

出图器每张图新建一次管线（GTAO + TRAA）。原因是 TRAA 的抖动序号是 JS 状态（`_jitterIndex`，每次管线渲染加一），不随帧序号重置。盒子间场景，960 × 640，12 帧：

| 方式 | 前台 | 后台（真实 Chrome，rAF 为 0） |
|---|---|---|
| 可控推进 | 两次一致，`cac236f3` | 两次一致，`cac236f3`，**与前台相同** |
| 交给 three 的循环，12 帧同步渲染 | 帧序号全是 1，**整张黑图**（`ce1d06ff`） | 同左 |
| 交给 three 的循环，帧间等 20 ms | 帧序号跳着走（1, 5, 9…），结果碰巧与可控相同 | 帧序号冻结在 1，**黑图**；计时器节流使 12 帧耗时 11.8 秒 |

- 评审 B1 实锤，而且后果比「第二张复用第一张」更重：TRAA 的历史缓冲按帧只更新一次，帧序号不变时输出是黑的。助手的导出常在后台标签页里跑，不接管帧推进就会交出黑图。
- 帧间等待时结果与可控相同，是因为 GTAO 和 TRAA 只要求每次渲染是新的一帧，与帧序号的具体值关系不大。SSGI 用 `frameId % 6`、`% 4` 选旋转和偏移，值本身有影响，组 5 再验证；无论如何都由出图方给定。
- **跨构建一致**：同一台机器上，Playwright 的 Chromium 145 和 Chrome 155 用 WebGPU 后端出的图逐像素相同。WebGL2 后端两者不同（`13cba8f6` 与 `224f3228`）。
- 耗时：第一张 668 ms（含着色器编译），之后每张 12 帧约 60 ms。

### WebGL2 后端：后台标签页里读回不返回

退回 WebGL2 后端时，前台可控推进同样两次一致（`13cba8f6`）。但在真实后台标签页里，`readRenderTargetPixelsAsync` 永远不返回（90 秒超时）。原因是 `WebGLUtils._clientWaitAsync` 用 `requestAnimationFrame` 轮询 fence，后台标签页没有 rAF。WebGPU 后端用 `mapAsync`，不受影响。R1 在 WebGL2 后端上读回要换做法（同步 `readPixels` 或画布 `toDataURL`，现有离屏端就是后者），组 7 验证。

## 后处理节点与 MSAA

`pass()` 默认继承渲染器的采样数。渲染器 `antialias: true` 时深度纹理是多重采样的，GTAO 的 `textureGather` 不接受，着色器编译失败（`renderPipeline_GTAO` 无效）。送进 AO、TRAA、SSGI 等需要采样深度的节点的 pass 必须 `pass(scene, camera, { samples: 0 })`。

## 阴影包围盒拟合（`sunShadow.ts`）

节点阴影（`ShadowNode.renderShadow`）每次仍调用 `shadow.updateMatrices(light)`，`normalBias` 按引用读取，所以现有的 `fitSunShadow` 改写方式**原样有效**（截图对比：拟合后阴影完整）。前提是保持 `bias = 0`（编辑器现在就是 0）：拟合把 far 延伸 1000 m，非零的深度偏移会按这个范围放大，阴影几乎消失。

## R1 须知

1. 帧推进必须由渲染器创建入口接管：停内部循环，每张图从 0 开始，帧序号和时间都由出图方给定；每张图新建或重置时域节点（TRAA 的抖动序号）。
2. 视口画布不开 MSAA，色调映射和 sRGB 编码放在管线输出节点里，Gizmo 才能叠加；工作台的 MSAA 开在 `pass()` 上，送深度给后处理节点的 pass 用 `samples: 0`。
3. WebGL2 后端的读回不能用 `readRenderTargetPixelsAsync`（后台标签页里挂起）。
4. drei 白名单：`CameraControls`、`OrbitControls`、`GizmoHelper`、`TransformControls`；`GizmoViewport` 需要垫片或自写；`Edges`、`Outlines`、`Grid`、`Sky` 必须替换。
