# Diagnosis

> 状态：先记录（owner 2026-10-25「一时半会儿难以解决的问题可以先记issue，然后往后续推进」）。在 `scene-offscreen-reflections`（S1）任务 8.2 里顺带发现，与 S1 的改动无关：问题出在 R2 的时间模型（develop 上已有）。复现用的大厅在 `feat/scene-offscreen-reflections`。

## Symptom

大厅（2001 万面、67 个模型）在编辑器里以实时 GI 按 1K 导出时，一张材质光照超时失败。同一场景按 2K 导出能完成。

期望：导出的时限覆盖参考设备上合理的耗时；超大场景即使慢，也要在时限内完成，或者给出能理解的提示。

## Root Cause

已知：R2 的单张时限只按像素估算（`frontend/src/creative-scene/render/engines.ts`）：

```
shotBudgetMs = max(30 s, 8 × estimateMs)
estimateMs   = 宽 × 高 × (帧数 × 每像素每帧的 ns + 编码的 ns) / 1e6
```

- 实时 GI 每像素每帧按 30 ns 估，带世界空间追踪层再加 10 ns；一张图画 76 帧（收敛 12 帧加累积 64 帧）；
- 1K 的估时乘 8 之后仍在 30 s 以内，时限按下限 30 s 算；
- 大厅一帧的耗时主要花在几何上，和像素数关系不大。视口 1536 × 1024 时每帧约 477 ms，按此推算，1K 的 76 帧约 30 s 以上，超过了下限。2K 的像素是 1K 的 4 倍，时限随之放大到下限以上，而大厅一帧的耗时增加不多，所以能完成。

## Evidence(红)

```sh
# feat/scene-offscreen-reflections：把「trace perf: S0’s hall exported twice」里导出对话框的尺寸改成 1K（默认 2K），约 5 分钟
cd frontend && SCENE_E2E_TRACE_PERF=1 SCENE_E2E_ONLY="trace perf: S0’s hall exported twice" \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

2026-10-25（Apple M4 Pro）：1K 时第一次导出的材质光照超时失败。之后这个诊断用例改用 2K。

## Blast Radius

- 几何为主的大场景（数百万面以上）按小尺寸导出时都会受影响，与这一层无关；
- 实时 GI 的 `render_view` 时限也用同一个模型（`viewBudgetMs` 下限 60 s），按同样的推算风险较小，但没有实测；
- 工作台只画 1 帧，不受影响。

## Fix Direction

1. 估时加上几何项，例如「帧数 × 三角面数 × 每面 ns」。系数按参考设备在大厅和参考场景上实测；
2. 或者按视口最近测得的帧时间来估，导出前就知道这台设备画这个场景一帧要多久；
3. 两种做法都要先改 R2 的 design「输出分辨率、编码与时限」，再改代码。时限只决定什么时候算失败，不影响画质。
