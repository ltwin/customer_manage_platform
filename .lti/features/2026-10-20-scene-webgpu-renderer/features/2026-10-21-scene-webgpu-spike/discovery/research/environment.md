# 运行环境：headless Chromium 与 WebGPU 设备

R0 任务 2.2 的结论；读回、隐藏标签页节流和 Safari（任务 4.3）补在后面。

## headless Chromium 的启动参数

脚本：`prototype/measure/env.mjs`（R0 期间在 `frontend/spike/webgpu/measure/env.mjs`）。Playwright 1.58.2 的 Chromium 145.0.7632.6，本机 M4 Pro。数据：`environment/env.json`，截图：`environment/env-*.png`。

| 启动方式 | 参数 | WebGPU 适配器 | 页面实际后端 |
|---|---|---|---|
| headless shell，默认 | 无 | 无 | 无：WebGL2 也拿不到，three 退回后报错 |
| headless shell | `--use-angle=metal --ignore-gpu-blocklist --enable-gpu` | Apple metal-3 | WebGPU |
| headless shell | 同上加 `--enable-unsafe-webgpu` | Apple metal-3 | WebGPU |
| headless shell | `--use-angle=swiftshader --enable-unsafe-swiftshader`（现有场景 e2e） | 无 | WebGL2（自动退回） |
| headless shell | 同上加 `--enable-unsafe-webgpu --use-webgpu-adapter=swiftshader` | Google SwiftShader（fallback 适配器） | WebGPU |
| 新 headless（`channel: 'chromium'`） | 默认或 Metal 参数 | Apple metal-3 | WebGPU |
| 新 headless | SwiftShader 参数加两项 WebGPU 参数 | Google SwiftShader | WebGPU |

结论：

- **真实 GPU**：沿用现有性能用例的 Metal 参数即可拿到 WebGPU，不需要 `--enable-unsafe-webgpu`。
- **软件渲染**：SwiftShader 要额外加 `--enable-unsafe-webgpu --use-webgpu-adapter=swiftshader` 才有 WebGPU 适配器。
- **现有 e2e 的参数正好覆盖退回路径**：只有 `--use-angle=swiftshader` 时没有 WebGPU 适配器，`WebGPURenderer` 自动改用 WebGL2 后端（控制台是 `warn`，不是 `error`，现有 e2e 的报错收集不受影响）。R1 可以让日常 e2e 两套参数各跑一部分：WebGPU（SwiftShader 适配器）和 WebGL2 退回（AC-3）。
- **两者都没有时**（headless shell 默认参数）three 退回 WebGL2 后在取扩展时抛错。R1 的能力探测要在创建渲染器前判断「WebGPU 或 WebGL2 至少有一个」，都没有时给出明确提示，与现有 `SceneEditor` 检查 webgl2 的做法一致。

## 设备的限制和特性

| 项 | Apple M4 Pro（Metal） | SwiftShader |
|---|---|---|
| 适配器 `maxTextureDimension2D` | 16384 | 8192 |
| 适配器 `maxBufferSize` | 4 GiB | 1 GiB |
| `maxColorAttachmentBytesPerSample` | 128 | 128 |
| `core-features-and-limits` | 有 | 有 |
| `timestamp-query` | 有 | 有 |
| `float32-filterable` / `float32-blendable` | 有 / 有 | 有 / 有 |
| `shader-f16` | 有 | 无 |

**three 申请设备的方式**（`WebGPUBackend.init`，r186）：

- 适配器固定用 `featureLevel: 'compatibility'` 申请，再把适配器支持的特性全部列为必需特性。设备带 `core-features-and-limits` 时按完整模式运行；没有时 three 进入兼容模式并把 MSAA 采样数置 0（Epic design 已记录）。本机和 SwiftShader 都是完整模式。
- `requiredLimits` 默认为空，所以**设备只拿到 WebGPU 的默认上限**：`maxTextureDimension2D` 是 8192，即使适配器支持 16384。要用更高的上限，创建渲染器时须先自行 `requestAdapter` 读出适配器的限制，再经 `requiredLimits` 传入（或直接传入自建的 `device`）。这对组 6（4K 线稿的 2 倍渲染、8K）是关键：在支持 16384 的设备上可以把上限提上去。

## 其他

- r186 的 `WebGPURenderer` 已移除 `PCFSoftShadowMap`，设置后会警告并改用 `PCFShadowMap`。现有视口用 R3F 的 `shadows`（默认即 `PCFSoftShadowMap`），迁移后阴影边缘会变硬，组 3 需要找替代（`shadow.radius`、`VSMShadowMap` 或自定义过滤），否则 AC-1 的阴影对不上。
- R3F 9.8.1 内部仍用 `THREE.Clock`，r186 会打印一次弃用警告，不影响功能。
- 渲染器内部有一个常驻的 rAF 循环（`Animation.js`）：`init()` 之后每个 rAF 都执行 `nodeFrame.update()`，帧序号加一，`time` 和 `deltaTime` 按墙钟时间累加。所以帧推进的控制不仅是帧序号，也包括时间：依赖 `time` 的节点同样要由出图方给定（组 1 验证）。

## 读回（任务 4.3）

原型：`prototype/webgpu/readbackCase.ts`。测量：`prototype/measure/readback.mjs`（Playwright 的 Chromium，加真实 Chrome 的后台标签页）、`safari.mjs`。数据见 `environment/readback.json`、`safari.json`。

### 格式

一张四象限测试卡：线性灰 0.5、红、alpha 0.5 的绿、alpha 0 的蓝。用四种写法写进 64 × 64 的 RGBA8 目标，读回各象限中心的值：

| 写法 | 灰 0.5 | 绿，alpha 0.5 | 蓝，alpha 0 |
|---|---|---|---|
| `renderOutput(…, sRGB)` 写入普通 RGBA8 | 188 | (0, 173, 0, 128) | (0, 0, 0, 0) |
| 目标设为 sRGB 色彩空间，输出线性颜色 | 188 | (0, 255, 0, 128) | (0, 0, 255, 0) |
| 目标设为 sRGB，同时用 `renderOutput` | 223（编码了两次） | (0, 215, 0, 128) | (0, 0, 0, 0) |
| `RenderPipeline` 默认的输出变换 | 同第一行 | 同第一行 | 同第一行 |

WebGPU、WebGL2 两个后端，以及 Safari，读回的值都完全相同。WebGL2 的 sRGB 目标一行用常量颜色测得，见文末「其他」。

- **行方向**：WebGPU 从上到下，行宽补齐到 256 字节，最后一行不补；WebGL2 从下到上，行宽不补。
- **颜色空间**：`renderOutput` 写进普通 RGBA8，读回的就是 sRGB 编码值，与 Epic design「读回」一节一致。
- **alpha 与 design 不一致**：
  - r186 的 `renderOutput` 把输入当作预乘的线性颜色：先反预乘，再做色调映射和色彩空间转换，最后在输出空间重新预乘（`RenderOutputNode.setup`）。所以读回的是**预乘 alpha**，不是 design 约定的直通 alpha。
  - 透明背景上做 MSAA 解析，输出本来也是预乘形式，两者一致。
  - 实测：白色球、透明背景、4× MSAA 时，边缘像素在 alpha 130 处的红是 116，按直通 alpha 编码会出现一圈暗边；在 `renderOutput` 之后除以 alpha，边缘变成 227，与球内部相同（`environment/edges-as-is.png`、`edges-straight.png`）。
  - 只有缩略图用透明背景，需要做这一步。
  - 反预乘要放在着色器里、8 位量化之前，否则 alpha 小的像素精度会丢失。也可以改走下面的画布路径，由浏览器在编码时处理。

### 隐藏标签页

真实 Chrome 155，窗口放在屏幕外，前面再开一个空白标签页（与组 1 相同），先前台测一遍，再后台测一遍：

| 项 | 前台 | 后台 |
|---|---|---|
| WebGPU 出图：12 帧，960 × 640，含读回，连拍 3 张 | 101 / 38 / 31 ms | 68 / 39 / 42 ms，哈希与前台、与组 1 相同 |
| 2048 × 1365 的 `toDataURL('image/png')` | 60 ms | 50 ms |
| `toBlob` | 45 ms | 57 ms |
| `OffscreenCanvas.convertToBlob` | 48 ms | 49 ms |
| 画到画布后立刻 `toDataURL`，WebGPU 后端 | 24 ms | 22 ms，哈希与前台相同 |
| 画到画布后立刻 `toDataURL`，WebGL2 后端 | 45 ms | 40 ms，哈希与前台相同 |

- **WebGPU 读回不受节流**：后台标签页里的 `readRenderTargetPixelsAsync`（走 `mapAsync`）与前台一样快，回答了 design 交给 R0 核实的问题。
- **`toBlob` 没有复现延迟**：标签页隐藏几秒时，`toBlob` 并不慢。`host/render.ts` 注释里说的延迟（等节流后的空闲时间才编码）可能要隐藏更久才出现，Chrome 的强节流在隐藏 5 分钟后才生效，这次没有复现。仍沿用 `toDataURL`。
- **画布路径**：把输出画到渲染器自己的画布上，在同一个任务里调用 `toDataURL`。
  - 两个后端在后台标签页里都能出图。
  - 解码后与渲染目标读回逐像素相同，平均差 0.000。
  - 不用处理行方向和补齐，透明背景时浏览器在编码时自动反预乘。
  - WebGPU 画布在呈现之后不保留内容，但离屏渲染端的画布不在页面上，同一个任务里调用时内容还在，后台标签页也一样。
  - 这正是现有离屏端的做法，也解决了组 1 的 WebGL2 后台读回卡住的问题。

**建议**：

- 离屏端的截图和导出，在两个后端上统一用「画到画布后立刻 `toDataURL`」，与现在的 `host/render.ts` 相同。
- `readRenderTargetPixelsAsync` 只在 WebGPU 上用于需要原始像素的场合，例如测试里的哈希和差异图，用时要处理行宽补齐和反预乘。
- Epic design「读回」一节相应修订。

### Safari

**测法**：

- Safari 26.5，本机 macOS 26。
- Playwright 的 WebKit 不是 Safari 的 WebGPU 实现；safaridriver 要先在 Safari 里手动打开「允许远程自动化」，所以 Safari 不能进自动化测试。
- `safari.mjs` 用 `open -a Safari` 打开带 `?report=` 的页面，页面跑完后把结果发给本地的接收端。

**结果**：

- **WebGPU 默认开启**：适配器为 Apple，`maxTextureDimension2D` 16384，`maxBufferSize` 2 GiB；带 `core-features-and-limits`，three 按完整模式运行；也有 `shader-f16`、`float32-filterable`。
- **读回**：格式和边缘 alpha 的结果与 Chrome 完全相同。
- **组 1**：可控帧推进下两次出图逐像素相同（`fd778012…`）。哈希与 Chrome 不同，但画面一致：两者的平均差为 0.20%，单纯 JPEG 压缩的误差是 0.19%（`environment/safari-shooter.jpg`）。
- **组 5**：实时 GI（逐帧噪点版加累积，32 帧，960 × 640）正常出图，每帧 6.2 ms（`environment/safari-gi.jpg`）。
- **没有覆盖**：Safari 后台标签页的行为（脚本控制不了 Safari 标签页的可见性）；Safari 的 `toDataURL` 不能编码 WebP，组 6 已记录。

### 其他

- 测试卡的 TSL `Fn` 内含 `select`，直接作为整个 `fragmentNode` 时，WebGL2 后端构建失败（`VarNode.build` 里读 `addToStack` 报错），画面全黑，WebGPU 后端正常。包一层 `renderOutput` 后两个后端都正常。属于 three 的 GLSL 构建器问题，R1 写 TSL 时如果遇到同样的报错，可以先往这个方向查。

## R1 须知（读回）

1. **编码路径**：截图和导出在两个后端上都用「画到画布后立刻 `toDataURL`」。WebGL2 后端不要用 `readRenderTargetPixelsAsync`，它在后台标签页里会卡住。
2. **需要原始像素时**：只在 WebGPU 上用 `readRenderTargetPixelsAsync`。行宽按 256 字节补齐，从上到下；颜色是预乘的，要在 `renderOutput` 之后除以 alpha。
3. **渲染目标**：用普通 RGBA8 加 `renderOutput`。不要再把目标设成 sRGB 色彩空间，否则会编码两次。
4. **Safari**：Safari 26.5 能跑通组 1 和组 5。Safari 不进自动化测试，发版前人工打开一次即可。
