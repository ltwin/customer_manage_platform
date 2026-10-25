# 视口卡顿排查：draw call 而不是面数（2026-10-24）

> **结论**
> - 视口帧时与每帧 draw 数成正比，每次约 3 µs，全在 CPU 上；面数到 1560 万面/帧还不是瓶颈。
> - 代码模型的每个 Mesh 原样成为一次 draw（`models/cache.ts` `partsOf`），是现在卡顿的根因。
> - 同一场景在 GLB 里按材质预先合并后，所有显示模式都回到刷新率上限。
> - 最终渲染里，4 级阴影级联占 CPU 时间的 41%。

## 做法

- **环境**：参考设备 Apple M4 Pro，Chromium（Playwright，`--use-angle=metal`），WebGPU 后端，视口 1600 × 1000、2× 像素，刷新率上限 8.3 ms。
- **夹具**：复制 `frontend/scripts/creative-scene.e2e.mjs` 的场景服务和打开流程，改用独立的 fixture 文件名，在独立的 Vite 端口运行。脚本是一次性的，已删除；P1 会把它做成正式的 e2e 生成器。
- **模型**：合成 GLB 由 Node 里的 `GLTFExporter` 生成，经 `inspect()` 校验；材质用 `slot:a`、`slot:b` 两个槽位。
- **测量**：
  - 帧时：滚轮环绕视角，每帧都渲染，取 3 秒内 rAF 间隔的均值和 p95。
  - 每帧 draw 数：`__sceneGPU.redrawViewport(10)` 前后 `renderer.info.render` 的差值除以 10。
  - CPU：用 CDP 采样 CPU profile。

## 数据

每帧耗时（ms）：

| 场景 | mesh 数 | 场景面数 | 白模 | 材质预览 | 最终渲染（工作台） | 最终渲染（实时 GI） |
|---|---|---|---|---|---|---|
| 只有地面 | 1 | — | 8.3 | 8.3 | 8.3 | 8.3 |
| 8 个模型，每个 4 个高面数球（26 万面） | 32 | 208 万 | 8.3 | 8.3 | 8.3 | 11.9 |
| 1000 个方块基础体 | 1000 | 1.2 万 | 8.3 | 8.4 | 17.6 | 24.0 |
| 4 个模型，每个 1000 个 part | 4000 | 4.8 万 | 16.0 | 24.5 | 62.8 | 83.8 |
| 同上，GLB 里按材质预先合并 | 8 | 4.8 万 | 8.3 | 8.3 | 8.3 | 8.7 |

每帧绘制次数：

| 场景 | 白模 | 最终渲染（工作台） | 最终渲染（实时 GI） |
|---|---|---|---|
| 4000 mesh | 8806 | 22007 | 26023 |
| 1000 基础体 | 2182 | 5481 | 6492 |
| 高面数（32 mesh） | 85，499 万面/帧 | 201，1248 万面/帧 | 269，1561 万面/帧 |

每个 mesh 每帧大约要画：
- 白模 2.2 次（beauty 和 AO prepass）；
- 工作台最终渲染 5.5 次（再加 4 级太阳级联）；
- 实时 GI 6.5 次。

## CPU profile（4000 mesh，工作台最终渲染）

- three 的逐对象渲染路径约占 90%：`renderObject`、`_draw`、`updateForRender`（bindings）、`getForRender`、`needsRenderUpdate`、`_projectObject`，以及原生的 `setIndexBuffer`、`setVertexBuffer`。
- 阴影（`renderShadow`）占 41%。
- 应用自己的代码（`fitCascades` 等）约 2%，没有异常热点。

## 推论

- **显卡无关**：瓶颈在 CPU，所以 RTX 4090 一样卡。WebGL2 后端的单次 draw 开销更高。
- **代码模型的来源**：沙箱禁止 `InstancedMesh`，`lib.array` / `lib.radial` 会复制出独立的 mesh。`inspect` 允许一个模型最多 10000 个 primitive 实例。
- **合并在材质上是无损的**：`lib.material(slot)` 在一次编译里，同一槽位返回同一个材质，所以同槽位的 part 常量完全相同，合并不改变材质。
- **大规模时还有第二个瓶颈**：`scene-world-space-gi` S0 测得，2011 万面、2000 个实例时，光栅每帧 68–78 ms（GPU 时间戳，没有阴影和后处理），那时受限于三角形吞吐（`2026-10-23-scene-trace-spike/discovery/research/throughput.md`「顺带发现」）。
