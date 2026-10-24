# S0 原型

S0（`scene-trace-spike`）的穿刺页面和测量脚本，原来在 `frontend/spike-trace.html` 和 `frontend/spike/`，S0 结束时整体移到这里（任务 5.2）。这是一次性的原型，不是产品代码；结论和数据在上一级的各篇 research 里。

## 怎么重新运行

原型按原来的位置写相对路径（`../../src/...`、`/scripts/fixtures/...`），要先放回 `frontend/` 才能跑。

```sh
# 在仓库根目录
P=.lti/features/2026-10-23-scene-world-space-gi/features/2026-10-23-scene-trace-spike/discovery/research/prototype
cp -r $P/spike $P/spike-trace.html frontend/
cd frontend
npx vite --port <端口> --strictPort        # 另开一个终端，保持运行
node spike/trace/measure/<脚本>.mjs http://localhost:<端口> <输出目录> [场景,…]
# 跑完删掉放回的副本，不要提交进 frontend/
rm -rf spike spike-trace.html
```

- **环境**：在参考设备上用 headless Chromium（Metal）。脚本自带启动参数，需要 `playwright`。
- **测量期间不要改页面会加载的文件**：Vite 会重新加载页面，打断测量。
- **依赖版本**：测量时为 three 0.186.1、three-mesh-bvh 0.9.15、meshoptimizer 1.1.1。
  - 原型从 `three-mesh-bvh/src/*` 深路径导入打包辅助函数，库升级后可能失效。
  - meshoptimizer 不是 `frontend` 的直接依赖，是 `@types/three` 带进来的，原型直接借用。S1 要把它加为直接依赖。

## 页面参数

`spike-trace.html?scene=<场景>&case=<组>`：

- `scene`：参考场景名（`scripts/fixtures/scene-reference/`），或 `stress`（压力场景，由生成器现场生成）；
- `limits=default`：只按 WebGPU 默认上限请求设备；
- `cold=1`：每次载入都换一个常量，让着色器缓存失效。

各组的其他参数见下表和各文件开头的注释。脚本的 `EXTRA` 环境变量会原样拼到页面地址后面，例如 `EXTRA="&proxies=1"`。

## 文件

| 组 | 页面用例（`case=`） | 主要文件 | 测量脚本 | 结论 |
|---|---|---|---|---|
| 环境 | — | `main.ts`（页面入口、设备上限） | `env.mjs` | `environment.md` |
| 1 接入 | `integration` | `integration.ts`，`trace.ts`（调用库求交的 WGSL 包装函数） | `integration.mjs` | `integration.md` |
| 2 吞吐与上限 | `throughput`（`part=`、`cache=`、`eps=`）、`parallel` | `throughput.ts`、`layer.ts`（增量打包 `TraceLayer`）、`build.worker.ts` 与 `pool.ts`（Worker 构建、几何缓存）、`rays.ts`（四类光线）、`parallel.ts` | `throughput.mjs`、`parallel.mjs` | `throughput.md` |
| 3 命中点着色 | `shading` | `shading.ts`（材质表、光照）、`patterns.ts`（宽度函数版的预设副本）、`computePatches.ts`（three r186 在 compute 里的补丁） | `shading.mjs` | `hit-shading.md` |
| 4 反射补追 | `reflections`（`rmax=`、`layer=library`、`probe=`、`retrace=`） | `SSRTraceNode.js`（fork 自 three 0.186.1 的 `SSRNode.js`，改动处标「fork:」）、`hitShade.ts`、`reflections.ts` | `reflections.mjs` | `reflections.md` |
| 6 探针 | `probes`（`dims=`、`below=`、`ahead=`、`octDepth=`、`rays=`、`spacing=`、`blend=`、`frames=`、`refPasses=`） | `probes.ts` | `probes.mjs` | `probes.md` |
| 5 代理画质 | `probes` 和 `reflections` 加 `proxies=1`（或 `proxies=0.003,0.01`） | `proxy.ts` | `probes.mjs`、`reflections.mjs` | `proxy-quality.md` |

各篇 research 的「重新生成」一节给出了得到其中数据的完整命令。
