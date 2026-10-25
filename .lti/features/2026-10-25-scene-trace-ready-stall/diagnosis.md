# Diagnosis

> 状态：先记录（owner 2026-10-25「一时半会儿难以解决的问题可以先记issue，然后往后续推进」）。症状已实测，(a) 的主要来源已知，(b) 未归因。来源是 `scene-offscreen-reflections`（S1）的未决事项 S1-U2 和 AC-S1-8 的「打开到就绪」一项。owner 同意 S1 只记录、不处理，AC-S1-8 的 100 ms 按「这一层自己的主线程工作」判定。代码在 `feat/scene-offscreen-reflections`，合入 develop 之前按该分支复现。

## Symptom

大厅（2001 万面、67 个模型）在编辑器里以实时 GI 打开：

- **(a) 切换那一帧**：世界空间追踪层就绪、视口换到补追管线后，第一个主线程任务 540–945 ms，后面还有两个 136–484 ms 的任务。`mirror-board` 的同一帧约 220–280 ms。
- **(b) 打开到就绪期间的长任务**：超过 100 ms 的主线程任务，带这一层 16–22 个，不带这一层 8–12 个。后者来自模型载入本身，最长约 650 ms。这一层自己的 `sync()` 最长约 40 ms，多出来的几个长任务还没归因。

期望：切换时用户察觉不到停顿；打开到就绪期间，这一层不增加超过 100 ms 的任务。

## Root Cause

- **(a)，已知主要来源**：
  - 新的存储缓冲在第一次绑定时整份上传，大厅约 700 MiB，在 `renderer.render` 里同步进行；
  - 补追的着色器带 14 个预设，主线程生成 WGSL，GPU 还要编译管线（S0 冷启动 0.4–1.3 s）。
- **(b)，未归因，待验证的假设**：
  - 构建池在派发任务时拷贝几何（`geometryData`），大厅约 2000 万面，产生的大块垃圾触发主 GC；
  - Worker 发回的区段在主线程反序列化；
  - 上面 (a) 的上传有一部分落在就绪之前（三个精度的缓冲先后创建）。

  `longtask` 的归因信息不够用，要用 CDP 的 tracing 或 DevTools 的 Performance 面板看调用栈。

## Evidence(红)

```sh
# feat/scene-offscreen-reflections，约 12 分钟；结果写进 trace-hall.json 的 open / cold / warm（switchTasks、longTasks、syncs）和 off.longTasks
cd frontend && SCENE_E2E_TRACE_PERF=<目录> SCENE_E2E_ONLY="trace perf: AC-S1-8" \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

2026-10-25（Apple M4 Pro）：

| 项 | 打开 | 冷启动 | 缓存命中 | 不带这一层 |
|---|---|---|---|---|
| 切换后的任务（ms） | 781、240、484 | 655、311、332 | 676、136、345 | — |
| 超过 100 ms 的任务数 | 19 | 12 | 22 | 9 |
| 最长任务（ms） | 1000 | 868 | 895 | 644 |
| 这一层最长一次 `sync`（ms） | 35.4 | 40.3 | 28.1 | — |

## Blast Radius

- 只在超大场景里明显。参考场景的切换帧约 0.2–0.3 s，打开时没有多出的长任务；
- 只影响视口，导出不受影响：导出等底座就绪后才开始计帧（AC-S1-8）。

## Fix Direction

1. **(a)**：
   - 就绪之前在后台分块上传缓冲，每帧上传一段，让第一次绑定时不再整份上传；
   - 在就绪之前用 1 × 1 的离屏目标先画一次补追管线，提前生成着色器、编译管线（S1 design S1-U2 原本写的办法）。
2. **(b)**：
   - 先用 Performance 面板录一次打开过程，把长任务归到具体的调用；
   - 如果是 GC，构建池改为复用拷贝缓冲，或者对已经在缓存里的几何根本不拷贝（A9 已经做到「没命中才发几何」，冷启动时仍要全部拷贝）。
