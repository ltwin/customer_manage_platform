# Diagnosis

记录于 2026-10-27，来自 `scene-light-radius` 实施期间的发现。owner 要求先记录，暂不诊断、不修复，等 owner 说开始。

## Symptom

前置条件：视口为「最终渲染」，场景里有一个木箱、一根柱子、一盏投影聚光、两盏不投影的点光（e2e 的固定数据）。

一轮操作：

1. 用 Shift+A 加一盏点光；
2. Cmd+Z 撤销；
3. 显示模式切到「白模」，等 1.5 s（未用的材质超过 1 s 会被释放，`materials/instances.ts`）；
4. 切回「最终渲染」，等编译完成，画面稳定。

实际：第 1 轮之后，每多做一轮，视口渲染器的 `info.memory` 多出 4 个 render target 和 16 张纹理。第 1 轮后是 `renderTargets: 15, textures: 53`，第 3 轮后是 `renderTargets: 23, textures: 85`。`programs`（43）和 `uniformBuffers`（99）不变。

期望：第 1 轮之后资源数稳定，后面几轮不再增长。

## Root Cause

**未定位**，本阶段没有排查。已知的只有：

- 不是 `scene-light-radius` 引入的。把 `frontend/src` 换成该分支的起点 5d2308ff（还没有光源尺寸的代码）后，同样失败。
- 每轮固定增长 4 个 render target 和 16 张纹理，像是某个按显示模式重建的东西每次重建都没释放干净。

候选方向（置信度低，均未验证）：

- 切换显示模式时，`render/sceneRenderer.ts` 按 `keyOf(look)` 重建 pipeline（`this.built?.dispose()` 之后 `this.build(look)`），可能有 pass 或后处理节点的 render target 没交给 `disposables`；
- 加灯和撤销导致灯光重建（`objects/SceneLights.tsx`），某个阴影或灯光相关的 render target 没释放。

验证方法：逐步删减一轮里的操作（只切显示模式、只加灯撤销），看哪一步带来增长；再对 render target 按来源打标记。

## Evidence（红）

可重跑的命令：在 `frontend` 目录、本机 Vite 端口上运行

```
CREATIVE_SCENE_URL=http://localhost:<端口> SCENE_E2E_ONLY='warms its lights in one compile' node scripts/creative-scene.e2e.mjs
```

失败于 `frontend/scripts/creative-scene.e2e.mjs` 里的检查「lights: the rendered view warms its lights in one compile; adding, undoing and switching away and back leave nothing behind」，断言 `the same resources after two more rounds`：

```
  {
    programs: 43,
+   renderTargets: 23,
+   textures: 85,
-   renderTargets: 15,
-   textures: 53,
    uniformBuffers: 99
  }
```

同一检查前半段的断言「三盏灯一次编译完」通过。

## Blast Radius

未分析。视口长时间编辑、反复切换显示模式时，GPU 内存会持续增长；导出走单独的渲染器，是否受影响未知。

## Fix Direction

先定位，再决定修法。owner 说开始后，从上面「验证方法」入手。
