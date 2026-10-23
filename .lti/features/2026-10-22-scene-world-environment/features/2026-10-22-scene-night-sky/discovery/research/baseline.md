# W3 基线（任务 1.1）

在 `feat/scene-night-sky` 第一次改代码之前截取（代码与 develop `35611177` 相同），本机 Mac16,8（Apple M4 Pro）、Chromium 145、Metal 后端，1600 × 1000、2× 像素。

## 白天与手动模式（AC-W3-6）

`baseline/` 下两组，各含参考场景摄像机的 `lit` 导出（`*-shot-lit.png`）和最终渲染视口（`*-view-lit.png`）：

| 组 | 来源 | 说明 |
|---|---|---|
| `physical-noon` | 参考场景 `physical-noon`（上海，2026-09-23 12:00） | 当时月亮在地平线下约 49°，夜空上线后应逐像素不变 |
| `physical-manual` | 同上，`sun_mode` 改为 `manual`，太阳 −8°、方位 250° | 手动模式不画夜空，应逐像素不变 |

e2e「W3 day unchanged」（`SCENE_E2E_W3_DAY`）逐像素对照（差 > 2 的像素为 0），在未改动的代码上通过。`baseline.json` 记录机器与文件。

## 入夜的性能（AC-W3-10 对照）

e2e「W3 performance」（`SCENE_E2E_W3_PERF`，`SCENE_E2E_W3_PERF_LABEL=before`）：AC-9 的场景（约 200 个物件、4 盏灯）放在物理天空下，上海 2026-09-23 21:00，默认曝光 EV100 −3。结果在 `baseline/w3-perf-before.json`：

| 项 | 帧时间均值 | p95 | 最长 |
|---|---|---|---|
| 静止（每帧略转视角） | 8.3 ms | 9.1 ms | 9.4 ms |
| 从 19:00 起每帧拖一分钟，3 s | 38.9 ms（约 26 fps） | 100 ms | 150 ms |

拖动帧率与 W2 验收时的 25 fps 一致（W2 已接受的偏差，follow-up 处理）。AC-W3-10 按「拖动不低于本基线减 2 fps、静止多不超过 1 ms」判断。
