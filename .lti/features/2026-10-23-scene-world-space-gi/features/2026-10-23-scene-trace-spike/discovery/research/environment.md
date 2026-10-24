# 穿刺页面与设备上限

> 2026-10-23，S0 任务 2.1。穿刺页面（S0 期间为 `frontend/spike-trace.html`，现在在 `prototype/`，运行方法见 `prototype/README.md`）在参考设备上的设备上限，以及两个场景的就绪时间。数据在 `environment/env.json`，截图在同一目录。

## 环境

- **设备**：Mac16,8（Apple M4 Pro）。
- **适配器**：`vendor = apple`，`architecture = metal-3`；适配器没有给出 `featureLevel`。
- **浏览器**：Chromium 145.0.7632.6，headless，启动参数用 e2e 的 `metal`（`--use-angle=metal --ignore-gpu-blocklist --enable-gpu`），无需 `--enable-unsafe-webgpu`。
- **测量脚本**：`node spike/trace/measure/env.mjs <开发服务器> <输出目录>`。

## 设备上限

页面按两种方式创建渲染器：按适配器上限请求（缺省），或不额外请求任何存储上限（`?limits=default`，模拟上限较低的设备）。设备实际得到的上限如下：

| 上限 | 按适配器请求 | WebGPU 默认 | 两者之比 |
|---|---|---|---|
| `maxStorageBufferBindingSize` | 4 GiB − 4 B | 128 MiB | 32 |
| `maxBufferSize` | 4 GiB | 256 MiB | 16 |
| `maxStorageBuffersPerShaderStage` | 10 | 8 | — |
| `maxStorageTexturesPerShaderStage` | 8 | 4 | — |
| `maxComputeWorkgroupStorageSize` | 32 KiB | 16 KiB | — |
| `maxComputeInvocationsPerWorkgroup` | 1024 | 256 | — |

对后续各组的影响：
- **单个存储缓冲的容量**：参考设备上不是瓶颈，到了默认上限的设备上，单个缓冲最多 128 MiB。底层几何缓冲按「一个绑定」设计时，128 MiB 大约容纳 1–2 百万面。组 2 据此给出分档门槛。
- **产品的渲染器创建入口**：现在只请求了 `maxTextureDimension2D` 和 `maxBufferSize`（`render/gpu.ts:280`），存储缓冲的绑定上限停在 128 MiB。S1 要按 Epic design「能力与容量」在适配器上限之内额外请求。

## 两个场景

| 场景 | 网格 | 三角形 | 搭建 | 页面就绪 | 首帧 |
|---|---|---|---|---|---|
| `cornell` | 9 | 4 054 | 11 ms | 0.3–1.1 s | 约 60 ms |
| `stress` | 2 000 | 20 115 680 | 2.1–2.2 s，其中生成 2.1 s | 2.7–3.0 s | 130–140 ms |

- 只用光栅画 2011 万面、2000 个网格时，首帧约 140 ms（未开阴影和后处理），之后的帧时间由组 2 测量。
- 两种上限下的就绪时间相同：光栅部分不依赖存储缓冲的上限。
- 页面产物：`vite.config.ts` 没有多页面的构建入口，构建只打包 `index.html`，穿刺页面不会进入产品构建（同 R0 和 `creative-lab.html`）。
