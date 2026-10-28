# Diagnosis

> 状态：建档跟踪（owner 2026-10-28 决定另开 issue，后续再诊断）。下面是 scene-area-light 6.1 测性能时得到的事实与线索，根因尚未定位。

## Symptom

实时 GI（`realtime_gi`）下打开冷编译测试钩子 `gpuTestHooks.cold` 时，three 在控制台报错：

```
Cannot read properties of undefined (reading 'addToStack')
```

- 前置条件：`realtime_gi` 引擎；冷编译钩子打开（性能页 `?cold=1`，或 e2e 页面 `?gpu=cold`）。钩子只在测试页和性能页能打开，正式页面不会打开。
- 复现入口：
  - 性能页：`frontend/scripts/light-check/perf.html?engine=realtime_gi&cold=1`；
  - 导出：e2e 的 `exportReference(..., { hooks: 'cold,...' })`，`realtime_gi` 下导出。
- 不开冷编译不报；`workbench` 下开冷编译也不报。
- 期望：开冷编译钩子时两个引擎都不报错，它只应让着色器重新编译、画面最多差一级。

影响：冷编译是 AC-S1-7 / AC-S2-4 量冷启动耗时的口径。scene-area-light 的 AC-S2-4 导出检查目前只放过这一条报错（`tolerate: /reading 'addToStack'/`），对照与面光源两边一致；develop 上 S1 的 AC-S1-7 导出检查没有放过，`realtime_gi` 那一组预计会因此失败（未单独重跑确认）。

## Root Cause

未定位。已知线索：

- **出现时间**：S1 测 AC-S1-7（ed77bcc1，冷编译、两个引擎都导出）时不报；scene-area-light 的分支起点 develop 38b4a4e0 报（临时检出起点的 `frontend/src` 复现过）。这段区间里改动 `frontend/src` 的只有两个提交：
  - 8fb48979 `fix(scene-light-radius): 按代码审计修软阴影与拆层的边界问题`；
  - 41f55694 `fix(scene-view-render-target-leak): 拆太阳级联时释放每级阴影贴图`。
  
  两者都改阴影的拆除与释放，嫌疑最大（置信度中：区间推断，尚未二分验证）。
- **报错位置**：`addToStack` 在 three 的 `VarNode.build` 里读取 builder 当前的 stack。builder 没有 stack 时报这个错，即某个节点在 stack 之外被构建。早先 WebGPU spike 也遇到过同一报错（WebGL2 下 `Fn` 直接作 `fragmentNode`，见 `scene-webgpu-spike/discovery/research/environment.md`「其他」），成因不同，可作排查方向。
- **钩子的作用**：`render/gpu.ts` 的 `saltMaterials()` 改写 `NodeMaterial.prototype.setupOutput`，把输出乘 `1 + salt·2⁻²³`，输出不是 vec4 的保持原样。疑点：某个在拆除或重建阴影时被复用的节点，经过这层包装后在 stack 之外构建（置信度低，待验证）。

验证方法：用性能页 `?engine=realtime_gi&cold=1`，在 ed77bcc1、8fb48979、41f55694 上各开一次，看报错从哪个提交开始；再在 three 里给 `VarNode.build` 打断点，看是哪个材质、哪个节点。

## Evidence（红）

```sh
# worktree 或主检出另起 Vite（避开 owner 的端口），开性能页看控制台：
#   http://localhost:<port>/scripts/light-check/perf.html?engine=realtime_gi&cold=1
# 或跑 develop 上 S1 的冷编译导出检查（一次只跑这一个用例）：
SCENE_E2E_LIGHT_PERF=<dir> SCENE_E2E_ONLY='light size: AC-S1-7' CREATIVE_SCENE_URL=http://localhost:<port> \
  node frontend/scripts/creative-scene.e2e.mjs
# 预期红：控制台报 "Cannot read properties of undefined (reading 'addToStack')"；导出检查因页面报错失败
```

## Blast Radius

`gpuTestHooks.cold` 的使用方：

- `frontend/scripts/light-check/perf.tsx`（`?cold=1`）；
- e2e 的 AC-S1-7 导出检查（`hooks: 'cold,...'`，没有放过这条报错）；
- scene-area-light 的 AC-S2-4 导出检查（放过这条报错，修好后去掉 `tolerate`）。

如果根因在阴影拆除，而不只在测试钩子上，正式渲染也可能受影响。定位后要确认这一点。

## Fix Direction

先二分到具体提交，再判断：

1. 根因在钩子的包装方式：改 `saltMaterials()`（例如只包 vec4 颜色输出之外，再避开会被复用的节点），正式渲染不受影响；
2. 根因在阴影拆除或重建：修那段代码，并补一个不靠冷编译钩子也能复现的检查。

修好后去掉 AC-S2-4 的 `tolerate`，AC-S1-7 在 `realtime_gi` 下应恢复通过。相关但不同的问题：scene-gi-stalls（实时 GI 编译卡顿）。
