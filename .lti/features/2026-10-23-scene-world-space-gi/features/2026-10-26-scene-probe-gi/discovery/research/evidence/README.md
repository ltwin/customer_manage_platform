# S2 的证据

S2（`scene-probe-gi`）各项验收的数据和图。超过 30 MB 的部分不进 git，只提交对照需要的文件，并在这里写明怎么重新生成（design「验证与交付」）。

## 环境

- **设备**：Mac16,8（Apple M4 Pro）。
- **浏览器**：headless Chromium，e2e 的 `metal` 启动参数（ANGLE Metal）。
- **服务**：在 `frontend/` 下运行 `npx vite --port 5192 --strictPort`，下面的命令都用 `CREATIVE_SCENE_URL=http://localhost:5192`。
- 新建的 worktree 第一次跑 e2e 时，Vite 会在中途重新优化依赖并刷新页面，导出因此丢失、超时失败。先随便跑一个短用例让 Vite 热起来，再跑长的。

## `before/`：S2 之前（任务 1.1）

代码：`feat/scene-probe-gi` 的 `f7a521f5`，与 develop `411fcc80` 相同，只多了 e2e 的改动，不影响画面。

| 文件 | 内容 | 用于 |
|---|---|---|
| `gi-baseline.json` | 7 个参考场景在实时 GI 下的 `lit` 导出（2K）：整图与区域亮度、导出耗时、视口帧时间。五套：`webgpu`（带 S1 的反射）、`webgl2`（兼容模式）、`webgpu-compat`（`forceCompatibility`）、`webgpu-unsupported`（`traceUnsupported`）、`webgpu-off`（`traceOff`）。另有 `backlit-room` 左墙的固定点（45° 与 100°）和扫视（`left_wall_sweep`） | AC-S2-5：后四套是不构建这一层的情况；AC-S2-1：`webgpu` 是并排图的「之前」，扫视是「之前」的数值 |
| `trace-frames.json` | 视口 1536 × 1024 下每帧的耗时，带 S1 的反射与不带，7 个参考场景和大厅，各三轮的中位数（S1 的 `trace perf: AC-S1-7` 用例） | AC-S2-6：S2 之前这一层（只有反射）的增量 |
| `*.png` | 全套导出图（35 张），约 76 MB | 只在本机，见 `before/.gitignore` |

**整图亮度**（0–255 的亮度均值）。后四套之间相同，与 S1 的 `before/` 也相同；`webgpu` 带 S1 的反射，所以有镜面和金属的场景更亮：

| 场景 | `webgpu` | 其余四套 |
|---|---|---|
| `backlit-room` | 148.5 | 148.0 |
| `thin-wall` | 202.3 | 202.2 |
| `mirror-board` | 140.7 | 137.1–137.2 |
| `noon` | 164.3 | 164.1 |
| `room` | 155.6 | 154.6 |
| `materials` | 153.5 | 152.6 |
| `cornell` | 89.5 | 82.3 |

**`backlit-room` 的左墙**：

- 固定点（`[-2.49, 1.2, 1.4]`）在 45° 与 100° 下的亮度之比为 0.998，与 S0、S1 的基线（0.997）相同。这个点两个视角都偏亮墙远，比值本来就接近 1，对 AC-S2-1 的区分力有限。
- 扫视：左墙上 10 个点（z 为 −2.4、−1.2、0、1.2、2.4 m，高 0.9、1.8 m），每个角度只取画面里看得到的点。
  - 靠近摄像机背后亮墙的点（z 2.4 m，高 1.8 m）在 75° 时为 166.6，转到 90° 以后亮墙进入画面，升到 192–194，变化 16%。这正是 S2 要消除的「间接光随画面内容变化」。
  - 各角度可见点的均值从 155.9（30°）到 170.7（105°），与总平均之比为 0.956–1.047。但每个角度看得到的点不同，这个比值混入了「看的是墙的哪一段」，所以 S2 之后同时报两种算法：可见点的均值之比，以及每个点在它可见的各角度之间的比值（AC-S2-1 判据的解读，验收时交 owner 确认）。
- 「底座还没就绪」：导出会等底座，等不到就失败（S1 任务 6.1），没有可存档的导出。这一情况在视口里核对，用的合成与 `traceOff` 相同。

**视口帧时间**（`trace-frames.json`）：参考场景 13.9–18.2 ms，反射的增量在 −1.4 到 +0.2 ms 之间，就是测量的噪声；大厅 522 ms，增量 19.9 ms，超过 S1 的 8 ms（S1 合入时测得 10 ms，用例照旧报 FAIL，S1 验收按现状接受）。这一轮所有场景都比 S1 合入时的记录慢 10%–20%（参考场景当时 14–15 ms，大厅 490 ms），同一台机器，负载不同。AC-S2-6 判的是同一轮里带与不带这一层之差，所以以 S2 之后同一轮的对照为准，这里只作参考。

**重新生成**（本分支，任何还没改 giSplit 的提交上）：

```sh
cd frontend
SCENE_E2E_ONLY="gi-baseline" SCENE_E2E_GI_BASELINE=<目录> SCENE_E2E_GI_COMPAT=1 SCENE_E2E_GI_OFF=1 \
  CREATIVE_SCENE_URL=http://localhost:5192 node scripts/creative-scene.e2e.mjs      # 约 5 分钟
SCENE_E2E_ONLY="trace perf: AC-S1-7" SCENE_E2E_TRACE_PERF=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5192 node scripts/creative-scene.e2e.mjs      # 约 6 分钟，大厅一项按 S1 的阈值报 FAIL
```

## `after/probes-5.1.json`：探针的数值（任务 5.1）

代码：任务 5.1 的提交。`probes: AC-S2-1 and AC-S2-2` 用例的输出，5.2 据此判定：

- `views`：AC-S2-2 的 5 个视图（`cornell`、`thin-wall`，`thin-wall` 的两个远机位 `@level1`、`@level2`，`physical-noon`），各自的区域在 `gi-regions.json`。每个区域记录以下各项（亮度，曝光与导出相同）：
  - 合成用的间接光辐照度（`drawn`）；
  - 探针单独的值与覆盖度；
  - 环境贴图的值；
  - 逐像素参考（每像素 256 条路径，每条 3 次命中，只有最后一次读探针）；
  - 相对误差。

  另有整图平均差和每级探针的状态。不计像素自己的环境光灯（合成里单独加）。
- `lit_wall_in_view`、`left_wall_sweep`：AC-S2-1 的固定点与扫视，取法与 `before/` 相同。扫视多了逐点的比值（`perPoint`，每个点在它可见的各角度之间、相对它自己的均值）。
- 对照图（每个视图三张：导出、探针的辐照度、参考的辐照度，后两张同一曝光）只在本机生成，5.2 选图交 owner。

**重新生成**：

```sh
cd frontend
SCENE_E2E_ONLY="probes: AC-S2-1 and AC-S2-2" SCENE_E2E_PROBES=<目录> \
  CREATIVE_SCENE_URL=http://localhost:5192 node scripts/creative-scene.e2e.mjs      # 约 70 秒，写 probes.json 和 15 张图
```

## 任务 5.2

见 `ac-s2-2.md`：数值在 `after/probes-5.2.json`，对照图是 `after/ac-s2-2-views.jpg`、`after/ac-s2-2-bias.jpg`、`after/ac-s2-1-before-after.jpg`、`after/s2-u7-ao.jpg`。

## 任务 5.2a：拉远后的割裂

- `after/far-compare.jpg`：四个拉远的视角，每行依次是：改前的成图、改后的成图、改后探针给的间接辐照度、逐像素参考。改前探针盒子够不着场景，辐照度图里是全黑。
  - `cornell@mid`：13 m；
  - `cornell@far`：19 m，85 mm；
  - `thin-wall@far`：从窗外 16 m 处看；
  - `physical-noon@far`：22 m。
- `after/probes-5.2a.json`：原参考视角加拉远视角的全部数值，以及 AC-S2-1。

**重新生成**：命令同 5.1，视角已列在 e2e 的 `probeViews` 里（`SCENE_E2E_PROBE_VIEWS=cornell@mid,cornell@far,thin-wall@far,physical-noon@far` 只跑拉远的四个）。改前的图要先检出 `98e35bac` 再跑。

## 任务 5.2b：镜中光照与画面不一致

- `after/mirror-compare.jpg`，上下两行：
  - 上：thin-wall 加一面白色金属镜（粗糙度 0）和一个红球，改前与改后。改前镜中的地面和靠近镜子的右墙泛蓝白色，镜中的球泛紫；
  - 下：`trace: a ball close to the mirror` 的 0.3 m，只把命中点高光改读探针时球背面变黑（73%），探针光线把金属当漫反射后为 100%。
- 墙面逐点数值：在 e2e 输出里，`mirror.json` 写在给定目录下。

**重新生成**：`SCENE_E2E_ONLY="probes diag: a wall seen in a mirror" SCENE_E2E_PROBE_MIRROR=<dir> node scripts/creative-scene.e2e.mjs`；加 `SCENE_E2E_PROBE_MIRROR_SHADE=object,distance` 另出命中点按物体或距离着色的图（看哪里是世界追踪命中）。球的那一行：`SCENE_E2E_ONLY="trace: a ball close to the mirror" SCENE_E2E_TRACE=<dir>`。改前的图要先检出 `46a24366` 再跑。

## 任务 5.3：编辑与收敛

- `after/probes-5.3.json`：每种编辑的读数。`samples` 是逐帧的读回（`during` 表示还在拖动），`off` 是各区域相对收敛值的最大偏差，`offOfChange` 是相对「收敛值与变化量中较大的那个」的偏差；`flicker` 是停下后 8 帧里逐像素逐帧变化之和占均值的比例（S0 的口径）。
- 测法与 S0 相同：视口切到场景摄像机，按 `gi-regions.json` 的区域，在每个像素第一次命中处取探针给出的辐照度（`gpuTestHooks.probeView`）。编辑落地后视口暂停自行请求帧（`viewportHold`），用例每画一帧读一次。帧数取各级最小编辑年龄乘周期中的最大值。
- `after/follow-compare.mp4`：cornell 里拖动立方体约 5 秒，左为 A0 = 12、跟随期 α = 0（现行），右为 α = 0.5（S2-U5）。两段各录一次，时间没有严格对齐。
- 大厅的两项只记录（owner 2026-10-26）。一次连跑参考场景、视频和大厅时，大厅「转动太阳」那一步页面发生了一次导航（`Execution context was destroyed`），单独重跑没有复现，原因未查。

**重新生成**：`SCENE_E2E_ONLY="probes: AC-S2-3" SCENE_E2E_PROBE_EDITS=<dir> node scripts/creative-scene.e2e.mjs`，约 3 分钟；加 `SCENE_E2E_PROBE_FOLLOW_VIDEO=1` 录两段视频，加 `SCENE_E2E_PROBE_EDITS_HALL=1` 另记大厅（约 22 分钟），`=only` 只跑大厅。对照视频由两段 webm 剪成：`ffmpeg -ss 6 -i <α=0>.webm -ss 6 -i <α=0.5>.webm -filter_complex "[0:v]crop=900:640:330:100,scale=640:-2[a];[1:v]crop=900:640:330:100,scale=640:-2[b];[a][b]hstack" -c:v libx264 -pix_fmt yuv420p -crf 23 follow-compare.mp4`。

## 任务 5.4：导出

- `after/freeze-compare.jpg`：三个参考场景的 2K 材质光照导出，每行依次为预热后继续更新探针（现行）、冻结、两者之差 × 16；从上到下是 cornell、thin-wall、physical-noon。两张图最多差 2–9 级，超过 8 级的像素为 0.000%，与同一做法连续导出两次的差（下一条）相当。
- `after/probes-5.4.json`：两种做法的耗时、预热耗时与级数。测的那一轮机器负载高（load 4.8–7.2），耗时比前一轮慢约 60%，只作参考，正式耗时留给 5.5。三轮的预热都是 0.82–1.23 s，三级。
- **连续导出两次不逐像素相同**：cornell 在这一层打开时，连续导出每次都差几级，最多 6–9 级（隐藏标签页里 16 级），超过 8 级的像素为 0.0000%，差异散在整张图上。二分的结果：
  - 两次导出的探针缓冲在预热之后和最后一帧之后逐字节相同（`gpuTestHooks.probeDigest`），探针的计算是确定的；
  - 探针不画（`probesOff`）、屏幕命中改回原始输出，仍然不同；关掉这一层，逐像素相同；
  - S1 的代码（a81f639c，在 S1 的 worktree 上另开 Vite）同样不同，最多 6–8 级。所以不是 S2 引入的，与 issue `scene-trace-export-repeat` 同类。mirror-board 两次相同。

**重新生成**：`SCENE_E2E_ONLY="probes: 5.4" SCENE_E2E_PROBE_EXPORT=<dir> node scripts/creative-scene.e2e.mjs`，约 2 分钟，导出图写在 `<dir>` 下；`SCENE_E2E_PROBE_EXPORT_SCENES=` 跳过三个场景，`SCENE_E2E_PROBE_EXPORT_REPEAT=<n>` 连续导出 cornell n 次并逐次比较，附探针缓冲的摘要，加 `SCENE_E2E_PROBE_EXPORT_UNDRAWN=1` 不画探针、`SCENE_E2E_PROBE_EXPORT_TRACE_OFF=1` 关掉这一层。对照图：`ffmpeg -i export-<场景>-update.png -i export-<场景>-freeze.png -filter_complex "[0:v]scale=640:-2,split[a][a2];[1:v]scale=640:-2,split[b][b2];[a2][b2]blend=all_mode=difference,lutrgb=r=val*16:g=val*16:b=val*16[d];[a][b][d]hstack=3" row-<场景>.png`，三行再 `vstack`。
