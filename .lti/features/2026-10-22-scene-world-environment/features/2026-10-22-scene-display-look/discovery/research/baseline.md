# W1 之前的基线

W1 任务 1.1 的产物。AC-W1-1、AC-W1-3、AC-W1-4、AC-W1-6 的前后对照都以这里为准。R0 的基线（`scene-webgpu-spike/discovery/research/baseline`）不改写。

## 环境

| 项 | 值 |
|---|---|
| 设备 | Mac16,8，Apple M4 Pro（R0 的参考设备） |
| 系统 | macOS 26.5 |
| 浏览器 | Playwright 的 Chromium 145.0.7632.6，headless，`--use-angle=metal --ignore-gpu-blocklist --enable-gpu` |
| 渲染器 | ANGLE (Apple, ANGLE Metal Renderer: Apple M4 Pro)，场景走 WebGPU 后端 |
| 视口 | 1600 × 1000，2 倍像素比（画布 2394 × 1692 设备像素） |
| 代码 | 分支 `feat/scene-display-look` 的 8ef49bf1，`frontend/src` 与 `develop` f48841a3 相同 |

## 重新生成

在 `frontend` 下，开发服务器运行在 `CREATIVE_SCENE_URL` 上（这次用 5189 端口）：

```sh
SCENE_E2E_ONLY='looks: the reference' SCENE_E2E_LOOKS=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
SCENE_E2E_ONLY='baseline: the reference' SCENE_E2E_BASELINE=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
SCENE_E2E_ONLY='AC-9: frame times' SCENE_E2E_PERF=1 SCENE_E2E_PERF_OUT=<本目录绝对路径> node scripts/creative-scene.e2e.mjs
CREATIVE_EDITOR_URL=<同一服务器> CREATIVE_QA_CHECK=C6-model-preview CREATIVE_QA_OUTPUT_DIR=<目录> node scripts/creative-assets.e2e.mjs
```

改完后用同样的命令输出到另一个目录，再与这里对照。

## 内容

| 文件 | 内容 | 用途 |
|---|---|---|
| `<场景>-view-<模式>.png`、`looks.json` | 六组参考场景在线框、白模、材质预览、最终渲染下的视口（编辑器默认视角），以及整幅和下四分之一（地面）的平均亮度、过曝比例（亮度 > 250）、RGB 均值 | 线框逐像素对照（AC-W1-3）；白模和最终渲染的亮度对照 |
| `<场景>-<机位>-<pass>.png`、`baseline.json` | 六组参考场景导出的 `clay`、`line`、`lit`（长边 2048），以及正交视角的白模视口 | 线稿逐像素对照（AC-W1-3）；白模、材质光照导出的前后对照；白模导出交 gpt-image-2.5 重绘（AC-W1-4） |
| `perf.json` | R1 性能场景的视口帧时间 | AC-W1-6 |
| `model-preview-materials.png`、`model-preview-clay.png` | 资产库查看浮窗的 3D 预览（SwiftShader，与 `creative-assets.e2e.mjs` 相同） | 任务 3.2、4.3 的前后对照 |

参考场景在 `frontend/scripts/fixtures/scene-reference/`。

- 视口截图先等着色器编译完（`window.__sceneGPU.compiling` 为 0），再等两张连续截图相同。第一次截取时没有等编译，`room` 和 `cornell` 的最终渲染截在壁灯点亮之前（`room` 平均亮度 37 对 60），已改为等编译后重截。
- 同一代码连续截两次，24 张视口图逐像素相同（差 > 2 的像素为 0）。
- 本目录约 28 MB：视口 15 MB（其中线框 3.9 MB），导出 11 MB。

## 测量

视口（`looks.json`，平均亮度 0–255）：

| 场景 | 线框 | 白模 | 材质预览 | 最终渲染 | 最终渲染地面 | 地面 R−B |
|---|---|---|---|---|---|---|
| `noon` | 64.2 | 212.9 | 201.4 | 196.2 | 199.8 | 6.9 |
| `dusk` | 64.2 | 172.6 | 201.4 | 146.9 | 163.7 | 29.7 |
| `room` | 64.5 | 156.8 | 200.7 | 59.8 | 56.4 | 11.4 |
| `materials` | 64.7 | 208.8 | 198.9 | 184.4 | 190.8 | 9.7 |
| `dof` | 64.1 | 207.2 | 204.4 | 177.1 | 177.4 | 13.4 |
| `cornell` | 64.5 | 150.6 | 201.0 | 15.7 | 6.2 | 0.1 |

- `dusk` 的白模（172.6）比 `noon`（212.9）暗：现在的白模跟着天空和太阳走。W1 之后两者应相同。
- `cornell` 默认视角在盒子外面，最终渲染几乎全黑，只适合逐像素和白模的对照。

e2e「calibration」（SwiftShader，1280 × 820，日常 e2e 的环境）：

| 场景 | 整幅平均 | 过曝 | 地面平均 | 地面 RGB |
|---|---|---|---|---|
| 正午 EV 0 | 195 | 0.0% | 199 | 201, 199, 195 |
| 黄昏 EV +2 | 150 | 0.0% | 165 | 177, 163, 148 |
| 两盏 40 cd 壁灯的室内 EV 0 | 64 | 0.4% | 58 | 62, 57, 51 |

视口帧时间（`perf.json`）：线框、白模、材质预览、最终渲染的平均帧时间都是 8.3–8.4 ms，p95 9.1–9.2 ms，贴着 120 Hz 的刷新上限。加一盏灯时最长一帧 141 ms，删掉时 275 ms。帧时间对照只能说明「没有超过刷新上限」，比较时以白模是否仍在上限内为准。
