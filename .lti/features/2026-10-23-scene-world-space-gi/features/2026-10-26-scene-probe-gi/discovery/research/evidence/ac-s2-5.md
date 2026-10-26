# AC-S2-5：不构建这一层时与 S2 之前相同

## 第一步：`ambient` 的 alpha 改写 c（任务 2.1）

> 2026-10-26。对照的是 `before/`（任务 1.1）。改动后的代码连跑两次，用来量出同一份代码两次运行之间的差异。

### 结论

| 后端 | 结果 |
|---|---|
| `webgpu-off`（`traceOff`）、`webgpu-unsupported`（`traceUnsupported`）、`webgl2`（兼容模式） | **逐像素相同**，7 个场景都是 |
| `webgpu-compat`（`forceCompatibility`） | 6 个场景逐像素相同。`room` 有 2.45% 的像素差 1–3 级，改动后两次运行之间相同。S1 量过同一张图在同一份代码两次运行之间差 3.38%、最大 4 级（S1 `ac-s1-3.md`），这一张本来就在几种结果之间随机落在其中一种，所以差异在运行间的噪声以内 |
| `webgpu`（构建这一层，S1 的反射） | 不在 AC-S2-5 的范围里，一并记下：差异与两次运行之间同量级（最大 9 级对 6 级），差超过 8 级的像素都不到 0.0001%，与 S1 的已知问题 `scene-trace-export-repeat` 相同 |

- 「不同像素」按 RGB 任一通道不同计，「最大差」是 0–255 的级数。
- 按 design 的写法，这一步提交后要逐像素相同。不构建这一层的三套严格满足；`forceCompatibility` 的一张图跨运行本来就不能逐像素复现，按 S1 的口径判定为相同（差异不超过同一份代码两次运行之间的差异）。

### 明细

| image | 改动前 ↔ 改动后: differing, max, >8 | 改动后 ↔ 改动后第二次: differing, max, >8 | 改动前 ↔ 改动后第二次: differing, max, >8 |
|---|---|---|---|
| `backlit-room-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `backlit-room-shot-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `backlit-room-shot-webgpu` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `backlit-room-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `backlit-room-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `cornell-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `cornell-shot-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `cornell-shot-webgpu` | 3.68%, 9, 0.0000% | 3.79%, 6, 0.0000% | 3.28%, 5, 0.0000% |
| `cornell-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `cornell-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `materials-board-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `materials-board-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `materials-board-webgpu` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `materials-board-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `materials-board-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `mirror-board-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `mirror-board-shot-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `mirror-board-shot-webgpu` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `mirror-board-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `mirror-board-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `noon-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `noon-shot-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `noon-shot-webgpu` | 1.12%, 1, 0.0000% | 0.00%, 0, 0.0000% | 1.12%, 1, 0.0000% |
| `noon-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `noon-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `room-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `room-shot-webgpu-compat` | 2.45%, 3, 0.0000% | 0.00%, 0, 0.0000% | 2.45%, 3, 0.0000% |
| `room-shot-webgpu` | 6.09%, 4, 0.0000% | 7.30%, 4, 0.0000% | 7.28%, 3, 0.0000% |
| `room-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `room-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `thin-wall-shot-webgl2` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `thin-wall-shot-webgpu-compat` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `thin-wall-shot-webgpu` | 0.03%, 1, 0.0000% | 0.03%, 1, 0.0000% | 0.00%, 0, 0.0000% |
| `thin-wall-shot-webgpu-off` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| `thin-wall-shot-webgpu-unsupported` | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% | 0.00%, 0, 0.0000% |
| worst | 6.09%, 9, 0.0000% | 7.30%, 6, 0.0000% | 7.28%, 5, 0.0000% |

### 重新生成

```sh
cd frontend
SCENE_E2E_ONLY="gi-baseline" SCENE_E2E_GI_BASELINE=<目录> SCENE_E2E_GI_COMPAT=1 SCENE_E2E_GI_OFF=1 \
  CREATIVE_SCENE_URL=http://localhost:5192 node scripts/creative-scene.e2e.mjs      # 每次约 5 分钟，改动后跑两次
python3 -I compare.py <before> <改动后> <改动后第二次>                              # 比较脚本见 compare.py
```
