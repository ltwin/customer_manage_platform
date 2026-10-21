# R1 迁移对照（AC-R1-1、AC-R1-4 画面）

R1 任务 9.1。e2e `parity`（`SCENE_E2E_GPU=<本目录>/parity`，本机 GPU：Apple M4 Pro，Chromium 145.0.7632.6，Metal 参数），六组参考场景在最终渲染视图下导出 `clay`、`line`、`lit`（2048 长边 PNG），加保存的正交视角白模截图；先与 R0 的 WebGL 基线（`scene-webgpu-spike/discovery/research/baseline/`）逐张比，再用测试参数 `forceWebGL` 经 WebGL2 后端导出一遍，与 WebGPU 的同一张图比。每张图的并排图在 `parity/`：左 R0（或 WebGPU）、中 R1（或 WebGL2）、右差异 ×8；数据在 `parity/parity.json`。

## 阈值

AC-R1-1 的上限：`line` 平均差 ≤ 0.1、差 > 8 的像素 ≤ 0.05%；`clay`（含正交白模）和 `lit` ≤ 1.5、≤ 1%。第一次跑通后按上限以内各图实测最大值的 1.5 倍收紧（不超过上限）：

| 方式 | 平均差 | 差 > 8 的像素 |
|---|---|---|
| `line` | 0.0016 | 0.0026% |
| `clay`（含正交白模） | 1.5 | 0.62% |
| `lit` | 0.63 | 0.18% |

## 结果：48 次比较，43 次在阈值内，5 次超出（交 owner 看图）

| 超出的图 | 对照 | 平均差 | 差 > 8 | 原因 |
|---|---|---|---|---|
| `noon-shot-lit` | R0 WebGL | 0.55 | 1.35% | 混凝土墙气孔的硬边 |
| `materials-board-lit` | R0 WebGL | 0.62 | 1.13% | 同上（材质板上的混凝土） |
| `noon-shot-lit` | WebGL2 后端 | 0.25 | 1.30% | 同上 |
| `materials-board-lit` | WebGL2 后端 | 0.31 | 0.97% | 同上（在上限内，超出收紧后的 0.18%） |
| `materials-ortho-clay` | R0 WebGL | 0.93 | 2.91% | 正交视图里方块之间窄缝的 AO 比迁移前深 |

- **混凝土气孔**：`concrete@1` 的气孔是 `smoothstep(0.12, 0.18, noise)` 的窄过渡，同时进凹凸高度；气孔位置与基线一致，差别是单个气孔边缘的像素在阈值两侧翻转（并排图 `noon-shot-lit.jpg` 墙面）。同一 TSL 代码在 WebGL2 后端对 R0 只有 0.06% / 0.19%，所以来源是 Metal 上 WGSL 的浮点运算，不是移植误差。可选做法：接受；或给气孔边缘加按像素宽度的抗锯齿（会改变预设外观，需 owner 同意）。
- **正交窄缝 AO**：fork 的 GTAO 已换回 GTAOShader 的算法（透视导出因此从 3–8% 降到 ≤ 0.25%），正交视角的其他五组场景也比 r186 的节点更接近基线（例如 room 0.38% → 0.14%）；只有材质板方块之间的窄缝更深。视口的 AO 以 CSS 像素分辨率计算、降噪改用 three 的 `DenoiseNode`（旧管线是 GTAOPass 自带的泊松降噪），窄缝里两者的结果不同。可选做法：接受；或继续调降噪参数逼近（会让透视的匹配变差的风险需另测）。

## AC-R1-1 逐张结果（对 R0 WebGL）

| 图 | 平均差 | 差 > 8 的像素 | 结论 |
|---|---|---|---|
| `noon-shot-clay.png` | 0.3206 | 0.0643% | 通过 |
| `noon-shot-line.png` | 0 | 0% | 通过 |
| `noon-shot-lit.png` | 0.5516 | 1.346% | **超阈值** |
| `noon-ortho-clay.png` | 0.7089 | 0.3719% | 通过 |
| `dusk-shot-clay.png` | 0.2746 | 0.0476% | 通过 |
| `dusk-shot-line.png` | 0 | 0% | 通过 |
| `dusk-shot-lit.png` | 0.4223 | 0.1004% | 通过 |
| `dusk-ortho-clay.png` | 0.6893 | 0.4117% | 通过 |
| `room-shot-clay.png` | 0.3735 | 0.0549% | 通过 |
| `room-shot-line.png` | 0.0011 | 0.0017% | 通过 |
| `room-shot-lit.png` | 0.3629 | 0.1088% | 通过 |
| `room-ortho-clay.png` | 0.8881 | 0.1357% | 通过 |
| `materials-board-clay.png` | 0.4009 | 0.232% | 通过 |
| `materials-board-line.png` | 0.0005 | 0.0008% | 通过 |
| `materials-board-lit.png` | 0.6155 | 1.1267% | **超阈值** |
| `materials-ortho-clay.png` | 0.9342 | 2.9052% | **超阈值** |
| `dof-cama-clay.png` | 0.3058 | 0.0001% | 通过 |
| `dof-cama-line.png` | 0 | 0% | 通过 |
| `dof-cama-lit.png` | 0.385 | 0.0013% | 通过 |
| `dof-camb-clay.png` | 0.4301 | 0% | 通过 |
| `dof-camb-line.png` | 0 | 0% | 通过 |
| `dof-camb-lit.png` | 0.3232 | 0.0001% | 通过 |
| `dof-ortho-clay.png` | 1.0226 | 0.1556% | 通过 |
| `cornell-shot-clay.png` | 0.1727 | 0.0514% | 通过 |
| `cornell-shot-line.png` | 0 | 0.0001% | 通过 |
| `cornell-shot-lit.png` | 0.1944 | 0.1207% | 通过 |
| `cornell-ortho-clay.png` | 0.681 | 0.1393% | 通过 |

## AC-R1-4 画面（WebGL2 后端对 WebGPU）

| 图 | 平均差 | 差 > 8 的像素 | 结论 |
|---|---|---|---|
| `noon-shot-clay.png` | 0.0365 | 0.0417% | 通过 |
| `noon-shot-line.png` | 0 | 0% | 通过 |
| `noon-shot-lit.png` | 0.25 | 1.3034% | **超阈值** |
| `dusk-shot-clay.png` | 0.0366 | 0.026% | 通过 |
| `dusk-shot-line.png` | 0 | 0% | 通过 |
| `dusk-shot-lit.png` | 0.0836 | 0.0836% | 通过 |
| `room-shot-clay.png` | 0.048 | 0.0218% | 通过 |
| `room-shot-line.png` | 0.0007 | 0.0011% | 通过 |
| `room-shot-lit.png` | 0.1425 | 0.0708% | 通过 |
| `materials-board-clay.png` | 0.0997 | 0.0774% | 通过 |
| `materials-board-line.png` | 0.0004 | 0.0006% | 通过 |
| `materials-board-lit.png` | 0.3062 | 0.9663% | **超阈值** |
| `dof-cama-clay.png` | 0.0048 | 0.0001% | 通过 |
| `dof-cama-line.png` | 0 | 0% | 通过 |
| `dof-cama-lit.png` | 0.0237 | 0.0003% | 通过 |
| `dof-camb-clay.png` | 0 | 0% | 通过 |
| `dof-camb-line.png` | 0 | 0% | 通过 |
| `dof-camb-lit.png` | 0.0049 | 0.0001% | 通过 |
| `cornell-shot-clay.png` | 0.0644 | 0.0157% | 通过 |
| `cornell-shot-line.png` | 0 | 0.0001% | 通过 |
| `cornell-shot-lit.png` | 0.0503 | 0.0199% | 通过 |

## 同时确认的

- 视口与离屏渲染器的后端日志来自创建入口：WebGPU 一轮为 `scene renderer (viewport): webgpu`、`scene renderer (offscreen): webgpu`，`forceWebGL` 一轮离屏为 `webgl2`（AC-R1-2 的一部分）。
- 材质板覆盖 14 个预设，预设版本号不变（`@1`）。
