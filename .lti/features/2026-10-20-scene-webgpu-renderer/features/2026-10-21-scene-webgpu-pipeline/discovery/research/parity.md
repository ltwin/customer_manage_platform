# R1 迁移对照（AC-R1-1、AC-R1-4 画面、AC-R1-8 兼容模式模拟）

R1 任务 9.1 / 9.4。e2e `parity`（`SCENE_E2E_GPU=<本目录>/parity`；Apple M4 Pro，Chromium 145.0.7632.6，Metal 参数）。六组参考场景在最终渲染视图下导出 `clay`、`line`、`lit`（2048 长边 PNG）加保存的正交视角白模截图，逐张与 R0 的 WebGL 基线（`scene-webgpu-spike/discovery/research/baseline/`）比；再用 `forceWebGL` 经 WebGL2 后端导出一遍，与 WebGPU 的同一张图比。并排图在 `parity/`（左 R0 或 WebGPU，中 R1 或 WebGL2，右差异 ×8），数据在 `parity/parity.json`。

## 阈值

AC-R1-1 上限：`line` 平均差 ≤ 0.1、差 > 8 的像素 ≤ 0.05%；`clay`（含正交白模）和 `lit` ≤ 1.5、≤ 1%。首次跑通后按上限以内各图实测最大值的 1.5 倍收紧（不超过上限）：

| 方式 | 平均差 | 差 > 8 的像素 |
|---|---|---|
| `line` | 0.0016 | 0.0026% |
| `clay`（含正交白模） | 1.5 | 0.62% |
| `lit` | 0.63 | 0.18% |

## 结果：48 次比较，43 次在阈值内，5 次超出（交 owner 看图，阈值不放宽）

| 超出的图 | 对照 | 平均差 | 差 > 8 | 原因 |
|---|---|---|---|---|
| `noon-shot-lit` | R0 WebGL | 0.5551 | 1.3503% | 混凝土墙气孔的硬边 |
| `materials-board-lit` | R0 WebGL | 0.6151 | 1.1154% | 同上（材质板上的混凝土） |
| `noon-shot-lit` | WebGL2 后端 | 0.2208 | 1.2939% | 同上 |
| `materials-board-lit` | WebGL2 后端 | 0.2512 | 0.968% | 同上（在上限内，超出收紧后的值） |
| `materials-ortho-clay` | R0 WebGL | 0.9345 | 2.9057% | 正交视图里方块之间窄缝的 AO 比迁移前深 |

- **混凝土气孔**：`concrete@1` 的气孔是 `smoothstep(0.12, 0.18, noise)` 的窄过渡，同时进凹凸高度。气孔位置与基线一致，差别是单个气孔边缘的像素在阈值两侧翻转（并排图 `noon-shot-lit.jpg` 的墙面）。同一 TSL 代码在 WebGL2 后端对 R0 只有 0.06% / 0.19%，来源是 Metal 上 WGSL 的浮点运算，不是移植误差；Safari 的结果与 Chrome 相同。可选：接受；或给气孔边缘加按像素宽度的抗锯齿（改变预设外观，需 owner 同意）。
- **正交窄缝 AO**：fork 的 GTAO 已换回 GTAOShader 的算法（透视导出因此从差 > 8 的像素 3–8% 降到 ≤ 0.25%），其余五组正交截图也比 r186 的节点更接近基线；只有材质板方块之间的窄缝更深。视口的 AO 以 CSS 像素计算、降噪改用 three 的 `DenoiseNode`（旧管线是 GTAOPass 自带的泊松降噪），窄缝里两者不同。可选：接受；或继续调降噪参数逼近（需另测透视不变差）。

## AC-R1-1 逐张（对 R0 WebGL）

| 图 | 平均差 | 差 > 8 的像素 | 结论 |
|---|---|---|---|
| `noon-shot-clay.png` | 0.3208 | 0.0659% | 通过 |
| `noon-shot-line.png` | 0 | 0% | 通过 |
| `noon-shot-lit.png` | 0.5551 | 1.3503% | **超阈值** |
| `noon-ortho-clay.png` | 0.7092 | 0.3749% | 通过 |
| `dusk-shot-clay.png` | 0.2747 | 0.0473% | 通过 |
| `dusk-shot-line.png` | 0 | 0% | 通过 |
| `dusk-shot-lit.png` | 0.4262 | 0.1002% | 通过 |
| `dusk-ortho-clay.png` | 0.6894 | 0.4129% | 通过 |
| `room-shot-clay.png` | 0.3734 | 0.0518% | 通过 |
| `room-shot-line.png` | 0.0011 | 0.0017% | 通过 |
| `room-shot-lit.png` | 0.3621 | 0.1082% | 通过 |
| `room-ortho-clay.png` | 0.8883 | 0.1376% | 通过 |
| `materials-board-clay.png` | 0.4015 | 0.2309% | 通过 |
| `materials-board-line.png` | 0.0005 | 0.0008% | 通过 |
| `materials-board-lit.png` | 0.6151 | 1.1154% | **超阈值** |
| `materials-ortho-clay.png` | 0.9345 | 2.9057% | **超阈值** |
| `dof-cama-clay.png` | 0.3058 | 0.0002% | 通过 |
| `dof-cama-line.png` | 0 | 0% | 通过 |
| `dof-cama-lit.png` | 0.3851 | 0.0012% | 通过 |
| `dof-camb-clay.png` | 0.4301 | 0% | 通过 |
| `dof-camb-line.png` | 0 | 0% | 通过 |
| `dof-camb-lit.png` | 0.3232 | 0.0001% | 通过 |
| `dof-ortho-clay.png` | 1.0227 | 0.1562% | 通过 |
| `cornell-shot-clay.png` | 0.1722 | 0.0517% | 通过 |
| `cornell-shot-line.png` | 0 | 0.0001% | 通过 |
| `cornell-shot-lit.png` | 0.1946 | 0.1203% | 通过 |
| `cornell-ortho-clay.png` | 0.6808 | 0.1392% | 通过 |

## AC-R1-4 画面（WebGL2 后端对 WebGPU）

| 图 | 平均差 | 差 > 8 的像素 | 结论 |
|---|---|---|---|
| `noon-shot-clay.png` | 0.0045 | 0.0231% | 通过 |
| `noon-shot-line.png` | 0 | 0% | 通过 |
| `noon-shot-lit.png` | 0.2208 | 1.2939% | **超阈值** |
| `dusk-shot-clay.png` | 0.0023 | 0.007% | 通过 |
| `dusk-shot-line.png` | 0 | 0% | 通过 |
| `dusk-shot-lit.png` | 0.0529 | 0.0706% | 通过 |
| `room-shot-clay.png` | 0.007 | 0.0198% | 通过 |
| `room-shot-line.png` | 0.0007 | 0.0011% | 通过 |
| `room-shot-lit.png` | 0.1258 | 0.068% | 通过 |
| `materials-board-clay.png` | 0.013 | 0.0631% | 通过 |
| `materials-board-line.png` | 0.0004 | 0.0006% | 通过 |
| `materials-board-lit.png` | 0.2512 | 0.968% | **超阈值** |
| `dof-cama-clay.png` | 0.0005 | 0.0001% | 通过 |
| `dof-cama-line.png` | 0 | 0% | 通过 |
| `dof-cama-lit.png` | 0.0194 | 0.0003% | 通过 |
| `dof-camb-clay.png` | 0 | 0% | 通过 |
| `dof-camb-line.png` | 0 | 0% | 通过 |
| `dof-camb-lit.png` | 0.0049 | 0.0001% | 通过 |
| `cornell-shot-clay.png` | 0.0024 | 0.0071% | 通过 |
| `cornell-shot-line.png` | 0 | 0.0001% | 通过 |
| `cornell-shot-lit.png` | 0.0316 | 0.0192% | 通过 |

## AC-R1-8 兼容模式（模拟）

本机拿不到兼容模式适配器（Chrome 的 `--force-webgpu-compat`、OpenGL ES、SwiftShader 适配器都仍报 `core-features-and-limits`）。用测试钩子 `forceCompatibility` 走工作台的兼容分支（`samples: 0` + SMAA），与 MSAA 的同一张图比（并排图 `*-smaa.jpg`）：

| 图 | 平均差 | 差 > 8 的像素 |
|---|---|---|
| `noon-shot-clay.png` | 0.072 | 0.1348% |
| `noon-shot-lit.png` | 0.0885 | 0.1726% |
| `materials-board-clay.png` | 0.152 | 0.404% |
| `materials-board-lit.png` | 0.1737 | 0.4989% |

真实兼容模式设备上 three 还会强制 `samples = 0`，这条分支与之相同；设备本身未验证，作为已知限制。

## 导出耗时（AC-R1-7 的导出部分，单次，毫秒）

| 场景 | R0 WebGL | R1 WebGPU | R1 WebGL2 后端 |
|---|---|---|---|
| noon | 478 | 467 | 2987 |
| dusk | 402 | 521 | 641 |
| room | 433 | 527 | 1999 |
| materials | 595 | 892 | 3928 |
| dof | 612 | 568 | 1283 |
| cornell | 498 | 522 | 1670 |

## 同时确认的

- 后端日志来自创建入口：WebGPU 一轮视口与离屏都是 `webgpu`，`forceWebGL` 一轮离屏是 `webgl2`（AC-R1-2 的一部分）。
- 材质板覆盖 14 个预设，预设版本号不变（`@1`）。
- Safari 26.5（`safari/`）：同一页面里导出材质板，离屏后端 `webgpu`，对 R0 的差与 Chrome WebGPU 相同（clay 0.40 / 0.23%、line 0.0005、lit 0.62 / 1.12%）。
