# 迁移前的 WebGL 基线

R0 任务 1.1、1.2 的产物。R1 的 AC-1 对照、AC-7 性能对比，以及 R2 的 AC-4 都以这里的数据为准。

## 环境

| 项 | 值 |
|---|---|
| 设备 | Mac16,8，Apple M4 Pro（owner 2026-10-21 确认为参考设备） |
| 系统 | macOS 26.5 |
| 浏览器 | Playwright 的 Chromium 145.0.7632.6，headless，`--use-angle=metal --ignore-gpu-blocklist --enable-gpu` |
| WebGL 渲染器 | ANGLE (Apple, ANGLE Metal Renderer: Apple M4 Pro) |
| 视口 | 1600 × 1000，2 倍像素比（画布 2394 × 1692 设备像素） |
| 代码 | `develop` 9bc7c651 的渲染管线（R0 不改 `frontend/src`） |

## 重新生成

在 `frontend` 下，开发服务器运行在 `CREATIVE_SCENE_URL` 上：

```sh
SCENE_E2E_ONLY=baseline SCENE_E2E_BASELINE=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
SCENE_E2E_ONLY=AC-9 SCENE_E2E_PERF=1 SCENE_E2E_BASELINE=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
```

参考场景在 `frontend/scripts/fixtures/scene-reference/`，e2e 用 `referenceScene(server, name)` 载入。常规用例 `reference scenes: each opens and draws…` 保证六组场景在日常 e2e 里都能打开。

## 导出

每组场景用 `?export=1` 打开，切到最终渲染，走导出对话框导出三种图（长边 2048）。耗时从点击「导出」到上传完成，包含离屏根创建、着色器编译、渲染和读回。

| 场景 | 内容 | 图数 | 耗时 | PNG 合计 |
|---|---|---|---|---|
| `noon` | 院子，太阳 55°，EV 0 | 3 | 478 ms | 1.6 MB |
| `dusk` | 院子，太阳 5°，EV +2 | 3 | 402 ms | 1.6 MB |
| `room` | 两盏 40 cd 壁灯的房间 | 3 | 433 ms | 2.3 MB |
| `materials` | 15 种材质的方块和透射球 | 3 | 595 ms | 2.4 MB |
| `dof` | 景深机位 A（F1.4）和 2:3 竖幅机位 B | 6 | 612 ms | 1.8 MB |
| `cornell` | 盒子间（红墙、绿墙、哑光白地面、光滑地板、金属球） | 3 | 498 ms | 2.1 MB |

- 文件名为 `<场景>-<机位名>-<pass>.png`。
- 2048 长边下单张 PNG：`lit` 1.0–1.7 MB，`clay` 0.2–0.8 MB，`line` 约 0.06–0.12 MB。按像素数线性外推，8K 的 `lit` 单张约 16–27 MB，会碰到暂存图片 25 MiB 的上限（组 6 实测确认）。
- 本目录共 14 MB，低于 proposal 定的 30 MB，全部提交。

## 正交白模

`<场景>-ortho-clay.png`：编辑器打开前预置一个已保存视角（`viewState.ts` 的格式，从右前上方看、正交、每米 80 像素），白模下截视口。这是正交视图 GTAO 迁移的对照。

顺带发现：「显示全部」（Home）用 camera-controls 的 `fitToBox`，会把视角吸附到包围盒最近的轴向面，所以按 Home 总是回到前视。Blender 的 Home 保持当前方向。这是现有编辑器的行为，与本 Epic 无关，没有改动，只在这里记录。

## 视口帧时间

C5 AC-9 的性能场景（约 200 个物件、8 种材质、太阳加 4 盏灯，其中 2 盏聚光投影），用两指滑动持续环绕 4 秒（`perf.json`）：

| 显示模式 | 平均 | p95 | 最大 |
|---|---|---|---|
| 线框 | 8.3 ms | 9.2 ms | 9.4 ms |
| 白模 | 8.5 ms | 9.2 ms | 17.1 ms |
| 材质预览 | 15.2 ms | 17.4 ms | 17.7 ms |
| 最终渲染 | 19.8 ms | 25.8 ms | 30.9 ms |

- 线框和白模约 8.3 ms，贴着 120 Hz 的刷新上限，实际渲染开销低于这个值；比较时主要看材质预览和最终渲染。
- 加一盏灯时最长一帧 120 ms，删掉时 136 ms（three 重新编译着色器）。
