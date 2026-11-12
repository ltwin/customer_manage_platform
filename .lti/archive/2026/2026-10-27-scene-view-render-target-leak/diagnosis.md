# Diagnosis

记录于 2026-10-27，来自 `scene-light-radius` 实施期间的发现；同日 owner 说开始后完成定位。

## Symptom

前置条件：视口为「最终渲染」，场景里有一个木箱、一根柱子、一盏投影聚光、两盏不投影的点光（e2e 的固定数据）。天空为程序天空，太阳投影，阴影分 4 级（`render/volumes/state.ts` `CASCADES = 4`）。

一轮操作：

1. 用 Shift+A 加一盏点光；
2. Cmd+Z 撤销；
3. 显示模式切到「白模」，等 1.5 s（未用的材质超过 1 s 会被释放，`materials/instances.ts`）；
4. 切回「最终渲染」，等编译完成，画面稳定。

实际：第 1 轮之后，每多做一轮，视口渲染器的 `info.memory` 多出 4 个 render target 和 16 张纹理。第 1 轮后是 `renderTargets: 15, textures: 53`，第 3 轮后是 `renderTargets: 23, textures: 85`。`programs`（43）和 `uniformBuffers` 不变。

期望：第 1 轮之后资源数稳定，后面几轮不再增长。

## Root Cause

**已定位，置信度高**（运行时补丁的红转绿对照，见 Evidence）。

three r186 的 `CSMShadowNode.dispose()`（`frontend/node_modules/three/examples/jsm/csm/CSMShadowNode.js:586`）只把每级的灯光从父节点上移除，没有释放每级的 `ShadowNode`（构造时 `this._shadowNodes.push( shadow( lwLight, lShadow ) )`，同文件 `:182`）。每级 `ShadowNode` 有自己的阴影贴图 render target：一张 `ShadowMap` 颜色纹理和一张 `ShadowDepthTexture`（`nodes/lighting/ShadowNode.js:336-343`）。只有 `ShadowNode.dispose()` 或 `_reset()` 会释放它（`:739`、`:752`）。

我们的 `cascadeSunShadow` 返回的 undo（`frontend/src/creative-scene/render/sunShadow.ts:92-100`）只调用了 `node.dispose()`，所以每拆一次太阳的级联，4 级阴影贴图就留在渲染器里。three 只在 `Textures._destroyTexture` 里调 `backend.destroyTexture`，这些纹理从未走到这一步，GPU 显存同样没有释放。

切换显示模式（「只切显示模式」，每轮 +4 render target，+12 纹理）：

- 「最终渲染」的灯光组件按显示模式挂载（`objects/Lighting.tsx:45`）。切到白模时，`Sun`（`:201`）或 `PhysicalLighting`（`:285`）卸载，调用 undo；切回时重建太阳和级联，第一帧画出 4 张新的 2048 阴影贴图。
- 旧的 4 张（render target 和深度纹理），以及材质绑定过的深度纹理，都留在渲染器里。

加灯（「只加灯再撤销」，每轮 +4 纹理，render target 不变）：

- 新灯第一次显示前，视口用替身场景预编译（`render/sceneRenderer.ts` `compile()`）。替身复制了太阳，并通过 `shadowLike(o, copy, cascadeSunShadow)` 给副本也挂上级联（`:218`）。
- 编译时材质绑定了副本 4 级的 `ShadowDepthTexture`，three 因此计数并分配了纹理。
- 替身用完后，`disposeStandIn` 调用同一个 undo（`:239`），4 张深度纹理就留下了。
- 替身从未画过阴影，render target 本身没有初始化，所以只留纹理，不留 render target。即使 `ShadowNode` 被释放，它的 render target 的 `dispose` 也不会顺带释放这张深度纹理，需要单独 `dispose`。现有的 `l.shadow?.map?.depthTexture?.dispose()`（`:243`）只顾到了灯光自己的 `shadow.map`，而级联用的是克隆出来的阴影。

两处相加，正是每轮 +4 render target、+16 纹理。不是 `scene-light-radius` 引入的：5d2308ff 上同样失败，级联阴影从 `scene-sun-cascaded-shadows` 起就是这样。

## Evidence（红）

可重跑的命令：在 `frontend` 目录、本机 Vite 端口上运行

```
CREATIVE_SCENE_URL=http://localhost:<端口> SCENE_E2E_ONLY='warms its lights in one compile' node scripts/creative-scene.e2e.mjs
```

失败于 `frontend/scripts/creative-scene.e2e.mjs:3190` 的检查「lights: the rendered view warms its lights in one compile; adding, undoing and switching away and back leave nothing behind」，断言 `the same resources after two more rounds`：

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

定位用的临时脚本（不提交）是这条检查的副本。它包住视口渲染器的 `info.createTexture` / `info.destroyTexture`，按纹理对象记录创建时的调用栈和所在步骤，并且可以分别只做加灯、只切模式：

| 变体 | 第 1→3 轮 | 第 1 轮后新建、到第 3 轮仍未释放的纹理 |
|---|---|---|
| 完整一轮 | RT 15→23，纹理 53→85 | 每轮切回时 4 张 `ShadowMap` 2048 和 8 张 `ShadowDepthTexture` 2048；每轮加灯时 4 张 `ShadowDepthTexture` 2048（`compileAsync` 绑定） |
| 只切显示模式 | RT 15→23，纹理 49→73 | 前者的切回部分 |
| 只加灯再撤销 | RT 11→11，纹理 41→49 | 前者的加灯部分 |

绿（对照）：同一脚本在页面里给 three 的 `CSMShadowNode.prototype.dispose` 补上「先释放每级 `ShadowNode`，再释放它 render target 的深度纹理」，不改源码。第 1 轮后和第 3 轮后都是 `renderTargets: 11, textures: 37`，仍然存活的只有第 3 轮正在用的资源（当前太阳的 4 级、聚光阴影、天空的 PMREM）。

## Blast Radius

`cascadeSunShadow` 的全部调用方都受同一个根因影响，undo 都只调用了 `node.dispose()`：

| 调用方 | 何时拆级联 | 受影响 |
|---|---|---|
| `objects/Lighting.tsx:201` `Sun`（程序天空） | 离开「最终渲染」 | 是，每次约 4 × 2048² × (RGBA8 + 32 位深度) ≈ 128 MB |
| `objects/Lighting.tsx:285` `PhysicalLighting`（物理天空） | 离开「最终渲染」；`parts` 只在挂载时建一次 | 是，同上 |
| `objects/lights.ts:74` 场景里的太阳灯（`SceneSunLight`） | 删除这盏灯（`disposeLight`，`:78`；关掉投影时级联保留，不拆） | 是，删一次 4 张，贴图尺寸为 `shadowMapSize` |
| `render/sceneRenderer.ts:218` 替身里的太阳副本 | 每次替身预编译之后（新灯第一次显示） | 是，每次 4 张深度纹理 |

导出用各自的渲染器，用完整体释放，不会累积。其他 `CSMShadowNode` 用法：没有，只有 `render/sunShadow.ts:33` 的 `SunCascades` 继承它。

## Fix Direction

在唯一的拆除口 `cascadeSunShadow` 的 undo 里，先逐级释放 `node._shadowNodes`，同时释放每级阴影贴图的深度纹理（替身那种从未画过的情况需要），再调用 `node.dispose()`。这样四个调用方一次修好，不必在每处补丁。`_shadowNodes` 是 three 的内部字段，按 `render/lightModel/nodes.ts` 的惯例注明「r186 核对过，升级时重看」。

不需要改 e2e：现有检查就是红转绿的判据。可以顺带核对 `objects/lights.ts` 删除太阳灯的路径，看资源是否回落。
