---
risk:
  factors: []
behavior: none
reads: [creative/scene]
decisionsPending: false
---

## Why

太阳的级联阴影每拆一次，4 级阴影贴图就留在渲染器里，GPU 显存不释放。离开「最终渲染」一次约多占 128 MB，新灯预编译一次约多占 64 MB，长时间编辑会不断累积（根因见 [diagnosis.md](diagnosis.md)）。

## What Changes

- `render/sunShadow.ts` `cascadeSunShadow` 的 undo：先逐级释放每级的阴影节点和它阴影贴图的深度纹理，再释放级联节点。四个调用方（程序天空的太阳、物理天空的太阳、场景里的太阳灯、预编译替身里的太阳副本）都走这个 undo，一处修好。

## Acceptance Criteria

- WHEN 视口在「最终渲染」，按 e2e「lights: the rendered view warms its lights in one compile…」做三轮「加点光、撤销、切到白模、切回」THEN 第 1 轮后和第 3 轮后视口渲染器的 `info.memory`（`renderTargets`、`textures`、`uniformBuffers`、`programs`）完全相同，该检查通过。
- WHEN 同一检查切回「最终渲染」THEN 三盏灯仍然一次编译完（`compiles` 只多 1），没有页面错误；拆掉旧级联不影响替身预编译。
- WHEN 在白模和「最终渲染」之间来回切换后 THEN 太阳阴影照常画出：e2e「AC-5: … the sun at 5° lengthens rendered shadows …」通过。
- WHEN 删除一盏投影的场景太阳灯 THEN 视口渲染器的 `renderTargets` 回到加这盏灯之前的数目（临时脚本核对，不新增 e2e）。

## Impact

- 代码：`frontend/src/creative-scene/render/sunShadow.ts`。
- 契约：无。
- 外部：无。

## Risk Notes

无。改动只在级联拆除时执行；画面、着色器、编译路径都不变。用到了 three 的内部字段 `_shadowNodes`（r186），按 `render/lightModel/nodes.ts` 的惯例注明升级 three 时要重新核对。
