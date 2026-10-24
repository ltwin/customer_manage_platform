# Diagnosis

> 状态：先记录（owner 2026-10-24「尚未解决的问题可以先记录成 issue」）。症状与复现已确认，根因未查明，下面的根因是待验证的假设。

## Symptom

新加一盏灯时，视口的预编译（`SceneRenderer.compile`，替身网格加替身灯，`compileAsync`）偶发两种失败：

1. **实时 GI 视口卡帧**：e2e「real-time GI: a light added in the rendered view compiles first, without an error or a held frame」偶发失败，最长帧 5000.1 ms 或 5.6–5.9 s（期望：没有超过阈值的帧，render-settings 修正后为 116 ms）。同一命令紧接着再跑，最长帧 298 ms，通过。
2. **建管线失败**：e2e「W5 no recompile」（`feat/scene-volumes`，Cornell 盒子，先加一盏投影聚光，约 1 s 后再加一盏投影平行光）在整批 W5 检查里偶发失败，控制台报：

   ```
   THREE.WebGPURenderer: Async render pipeline creation failed (renderPipeline_paint@1_43): Failed to execute 'createRenderPipelineAsync' on 'GPUDevice': Failed to read the 'depthStencil' property from 'GPURenderPipelineDescriptor': Failed to read the 'format' property from 'GPUDepthStencilState': Required member is undefined.
   ```

   同样的报错也出现在 `MeshPhysicalNodeMaterial`、`metal@1` 的管线上。单独跑这一项 6 次都通过。

前置条件：本机 Metal（`SCENE_E2E_W5_METAL=1` 的 `w5Metal()`，或无头 Chromium 的 WebGPU）；两项都与机器负载和时序有关。

## Root Cause

未查明。已知事实与假设：

- **事实**：`SceneRenderer.compile`（`frontend/src/creative-scene/render/sceneRenderer.ts:462`）为每个 pass 克隆 1×1 的渲染目标，`compileAsync` 完成后由 `freeTargets`（`:496`）释放克隆目标及其深度纹理。`asItsPass`（`:235`）在替身网格的渲染对象创建和材质构建期间，临时换上该 pass 的目标、MRT 和上下文。
- **事实**：已归档的 `scene-render-settings`（tasks 1.1、7.2，research/perf.md 第 57 行）记录过同类症状：修正前加灯最长帧 5004 ms，并且每种材质报一次管线创建失败，原因是预编译没带上 GI 光照 pass 的上下文，用 `asItsPass` 修复。现在的 5000.1 ms 与当年的 5004 ms 量级相同。
- **假设 A（中等置信度）**：深度格式为空，说明管线异步创建时，它所属的渲染上下文已经没有深度纹理，即克隆目标先被释放，或者上下文取自了不带深度的目标。可能的时序是两次预编译重叠（聚光的预编译未完成时，平行光又触发一次；`kept` 释放上一个替身），或者 `compileAsync` 返回时仍有管线在创建。验证方法：在 `freeTargets` 与 `createRenderPipelineAsync` 的失败回调里记时间戳和目标 id，看先后顺序。
- **假设 B（低置信度）**：W5 的步进预编译（`render/volumes/march.ts` `VolumeNode.compile`）同步地把渲染目标切到 `marched`（没有深度缓冲），可能与替身预编译后续再读取目标的时刻交错。但 `asItsPass` 在读取目标时会重新设置，而且报错的管线属于场景材质，不是步进材质，所以可能性不高。验证方法：在没有体积的场景里复现症状 2，可以排除这条。
- **5 s 卡帧**：最长帧恰好约 5000 ms，像某个固定超时，可能来自 Chromium 或 three 的异步管线等待；与症状 2 是否同一根因未证实。

## Evidence(红)

偶发失败，不能稳定复现：

```sh
# 实时 GI 卡帧（develop 与 feat/scene-volumes 上都出现过，各跑几次）
cd frontend && CREATIVE_SCENE_URL=http://localhost:<port> SCENE_E2E_ONLY="real-time GI: a light added in the rendered view" node scripts/creative-scene.e2e.mjs

# 建管线失败（feat/scene-volumes，整批更容易出现）
cd frontend && CREATIVE_SCENE_URL=http://localhost:<port> SCENE_E2E_W5_METAL=1 SCENE_E2E_ONLY="W5 " node scripts/creative-scene.e2e.mjs
```

2026-10-23/24 的记录：

- develop 基线 `934f08ba`：卡帧那项一次 5.6–5.9 s 失败、一次通过。
- `feat/scene-volumes`：卡帧那项 5000.1 ms 失败、298 ms 通过。
- 「W5 no recompile」：整批跑 2 次各失败 1 次，单独跑 6 次全部通过。

修复时需要先找到稳定复现的方法，例如压测循环或人为加入延迟。

## Blast Radius

- `SceneRenderer.compile` 的调用方：`gpu.compiler`（`sceneRenderer.ts:443`，只有视口设置）→ `SceneGPU.compile`（`render/gpu.ts:343`）→ `objects/SceneLights.tsx:23` 的 `warm`，也就是新加灯和灯开关投影时的预热。`feat/scene-volumes` 加入的 `VolumesCastLight`（体积第一次打开投射阴影时）也走这条路径。
- 视口第一帧之前的 `renderer.compileAsync(scene, camera)` 不经过替身，不受影响。
- 出图（offscreen shooter）在拍摄前整体编译，不走 `warm`，不受影响。

## Fix Direction

1. 先在 `freeTargets`、`disposeStandIn` 和管线创建失败处加时间线日志，找出稳定复现的方法。
2. 如果确认是假设 A：等替身的所有管线 promise 都结束（包括失败的）之后再释放克隆目标；或者让每次预编译持有自己的目标，直到 three 释放对应的渲染对象。
3. 卡帧那项另查那 5 s 来自哪里，看是否与第 2 点同源。
