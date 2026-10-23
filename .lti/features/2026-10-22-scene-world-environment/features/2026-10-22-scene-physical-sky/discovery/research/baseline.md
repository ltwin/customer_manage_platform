# W2 之前的基线

W2 任务 1.1 的产物，供 AC-W2-6（程序化和纯色天空的画面不变）和 AC-W2-11（帧时间）对照。

## 环境

| 项 | 值 |
|---|---|
| 设备 | Mac16,8，Apple M4 Pro（R0 的参考设备） |
| 系统 | macOS 26.5 |
| 浏览器 | Playwright 的 Chromium 145.0.7632.6，headless，`--use-angle=metal --ignore-gpu-blocklist --enable-gpu` |
| 渲染器 | ANGLE (Apple, ANGLE Metal Renderer: Apple M4 Pro)，场景走 WebGPU 后端 |
| 视口 | 1600 × 1000，2 倍像素比（画布 2394 × 1692 设备像素） |
| 代码 | 分支 `feat/scene-physical-sky` 的 1a4b1895，`frontend/src` 与 `develop` e02d89a7 相同 |

## 对照的范围

| 画面 | 基线 | 说明 |
|---|---|---|
| 六个参考场景（`noon`、`dusk`、`materials`、`dof` 是程序化天空，`room`、`cornell` 是纯色天空）的 `lit` 导出与最终渲染视口 | W1 的 `scene-display-look/discovery/research/after/` | W1 之后渲染端没有再改。在未改动的代码上重拍，与它逐像素相同，所以不再另存一份 |
| `noon-fog`：`noon` 打开全局雾 | 本目录 | 程序化天空下的雾 |
| `room-fog`：`room` 打开全局雾 | 本目录 | 纯色天空下的雾 |
| `noon-sun`：`noon` 加一台对着太阳的摄像机 `sun`（35 mm，景深打开，f/1.4，对焦 2 m） | 本目录 | 检查材质输出的钳制不改变程序化天空的太阳盘。乘 `SKY_GAIN` 后太阳盘的峰值约 1.34 万，在 60 000 以下。不和雾放在一起，因为雾会把天空连同太阳一起盖住 |

- `render_view` 与导出都经 `host/render.ts` 的 `shoot` 出图，只是尺寸和编码不同，导出的 `lit` 不变即可说明 `render_view` 的 `lit` 不变。
- 三个新场景由 e2e 在参考场景的基础上修改，参考场景的文件本身不改，W1 和 R1 的对照照常可用。

## 重新生成与对照

在 `frontend` 下，开发服务器运行在 `CREATIVE_SCENE_URL` 上（这次用 5293 端口）：

```sh
# 截取新场景的基线（只在改代码前跑一次）
SCENE_E2E_ONLY='W2 baseline' SCENE_E2E_W2_BASELINE=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
# 对照：参考场景对 W1 的 after/，新场景对本目录，差 > 2 的像素必须为 0；不一致的图把对照图写进 <输出目录>
SCENE_E2E_ONLY='W2 other skies' SCENE_E2E_W2_SKIES=<输出目录> node scripts/creative-scene.e2e.mjs
# 帧时间
SCENE_E2E_ONLY='AC-9: frame times' SCENE_E2E_PERF=1 SCENE_E2E_PERF_OUT=<目录> node scripts/creative-scene.e2e.mjs
```

改代码前的对照结果：「W2 other skies」绿，用时 48 s。

## 帧时间

`perf.json`，R1 性能场景，程序化天空：

| 模式 | 平均 | p95 | 最长 |
|---|---|---|---|
| 线框 | 8.3 ms | 9.1 ms | 9.4 ms |
| 白模 | 8.3 ms | 9.2 ms | 9.3 ms |
| 材质预览 | 8.4 ms | 9.3 ms | 11.3 ms |
| 最终渲染 | 8.3 ms | 9.2 ms | 9.4 ms |

- 加一盏灯时最长一帧 142 ms，删掉时 275 ms，与 W1 之后相同。
- 四种模式都贴着 120 Hz 的刷新上限。AC-W2-11 的「物理天空静止时帧时间多出不超过 2 ms」在这台设备上只能按「物理天空的最终渲染是否仍在刷新上限内」判断；需要实际的 GPU 耗时时，8.1 另用 GPU 计时查询或更重的场景测量。
