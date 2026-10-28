# Diagnosis

> 完整报告见 [report.md](report.md)（2026-10-27，Apple M4 Pro、Chrome for Testing 145、three 0.186.1、真实 Metal/WebGPU）。原始数据与复现脚本在本机 `var/diagnostics/2026-10-27-scene-gi-stalls/`（Git 忽略）。本文按 issue 格式摘要。

## Symptom

实时 GI（`realtime_gi` 的 lit 视图）下主线程连续冻结 0.5–1 s：

- **首次进入**：Cornell 完整 GI 一轮长任务 770、252、245、808、806、798 ms；frame-two-lights 861、254、250、916、947、911 ms。工作台最长 106 ms，只开屏幕空间 GI 最长 183–332 ms。
- **探针扩容**：新物体沿 X 移 30 m，探针 4 → 5 级，长任务 593、964、993、998 ms。移动 0.2 m 不触发。
- **Agent `render_view(mode:lit)` 回看**：每张图同步探针预热 499/533 ms，随后三次同步 `SceneRenderer.render` 867/745/850 ms、1006/775/800 ms（rAF 间隔佐证）。
- **增删灯光**：100–400 ms 级的灯光材质生成。

期望：上述操作都不出现明显的长时卡顿，画面不变。

## Root Cause

1. **同一反射材质重复生成**（放大因素，置信度高，已做单变量实验）：`render/nodes/SSRTraceNode.js:1451–1453` 的 `setup` 每被一个下游节点的 builder 调用一次，就重设 `_ssrFn`、调 `_buildSSRMaterial()`（`:680–682` 置 `needsUpdate`）。`realtimeGI.ts` 里反射节点有四个消费者（`temporalReproject`、`recurrentDenoise` 的 `raw`、`reflection` 的 mix、`cover`），材质版本 1 → 4 → 5，同一 603,181 字符的 shader 生成三次（644、720、721 ms）。去重实验消掉每轮后两次 0.8–1 s 停顿。three 自带的显示节点（SSGI、GTAO、去噪等）是同一写法。
2. **着色器图过大**（根本因素）：`probes/lookup.ts:167` 的 `probeIrradiance` 用 JS 循环按级数展开，`levelAt` 再展开每级 8 个邻近探针；`trace/hit.ts:~192` 的 `surfaceAt` 把整张预设表构成 Switch，反射命中点再查探针漫反射与高光。结果是 600–700 KB 的片元 shader，`NodeBuilder.build`（JS 侧建图与生成代码）单次 650–900 ms；`createShaderModule` 仅几毫秒。
3. **管线阶段切换**：`render/sceneRenderer.ts:825–835` 按 trace 就绪、探针预热选择 look，key 变化就 dispose 旧管线、build 新管线；`:886` 同步 `pipeline.render()`。首次进入经历 `gi-interactive` → `+trace-full` → `+probes` 三套管线。
4. **探针扩容重建**：`sceneRenderer.ts:748–762` 的 `probesFor` 在 outgrown 时建 growing 体；`world.serial` 在管线 key 里（`:347–348`），替换后整条 GI 管线重建；级数在 JS 里展开，级数变化必然生成新 shader。探针 compute 内核首建约 453 ms（384,121 字符）。
5. **回看冷启动**：`host/render.ts:121–141, 194–211` 每张图新建隐藏 canvas、GPU renderer、`SceneRenderer`，`warm(camera, frame, PROBE_WARMUP=64)` 后渲染多帧；所有 shader 与内核每张图重建一次。
6. **灯光变体**：灯光结构变化的场景材质生成 100–400 ms（与 GI 无关，屏幕空间 GI 下也有）。

## Evidence（红）

```sh
# 主检出另起 Vite（独立端口），从仓库根：
python3 var/diagnostics/2026-10-27-scene-gi-stalls/build.py
SCENE_E2E_ONLY='DIAG GI stalls' CREATIVE_SCENE_URL=http://127.0.0.1:5186 DIAG_MODES=full DIAG_MOVE=30 DIAG_LABEL=repro \
  node --experimental-transform-types frontend/scripts/.gi-stall-diag.mjs
# 预期红：UI blocked >200 ms in stages: open … move …
# 回看路径：build-agent-host.py 生成，同样命令；DIAG_ONCE=1 为 setup 去重实验
```

口径：longtask observer 在回看路径上漏报（实际 1006 ms 只报 534 ms），判定须看 `SceneRenderer.render` 包装计时或 rAF 帧间隔。

## Blast Radius

- `realtimeGI.ts` 的全部 TempNode 消费链：SSRTraceNode（fork）、SSGINode（fork）、GTAONode（fork），以及 three 的 denoise / recurrentDenoise / temporalReproject / traa / sss 都在 `setup` 里重建内部材质。
- 探针查询 `probeIrradiance` 的调用方：`realtimeGI.ts` 的 `probeLight` 屏幕 pass、`trace/hit.ts` 的命中点着色（反射 shader 与探针射线内核都含）。
- 管线构建入口：视口 `SceneRenderer`（`Viewport.tsx` LookView）与截图 `host/render.ts`（`render_view`、导出）共用同一 `SceneRenderer`。
- 普通小范围远端编辑（报告「定向复核 A」）不在范围内：已证实不重建管线。

## Fix Direction

按 [report.md](report.md)「修复建议顺序」：

1. 显示节点 `setup` 去重：同一节点只构建一次内部材质，保留参数 setter 的失效语义。
2. 缩小 TSL 图：探针查询与命中点着色做成带 layout 的 WGSL 函数，级数改为运行时循环，避免 JS 展开。
3. 回看复用：截图路径复用设备、管线与探针内核，每张图仍清空时域状态。
4. 探针扩容不重建整条管线；预热纳入预算。
5. 管线切换前预编译（旧管线继续画，新管线就绪再换）。
6. 灯光结构变化单独预编译。
