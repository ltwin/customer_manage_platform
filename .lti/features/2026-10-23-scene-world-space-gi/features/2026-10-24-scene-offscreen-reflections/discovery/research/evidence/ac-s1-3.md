# AC-S1-3：预设改为宽度函数之后

> 2026-10-25，任务 2.1（提交 `3f1b437f`）。对照的是 `before/`（任务 1.2）。

## 结论

| 判据 | 结果 |
|---|---|
| 14 个预设和原理化材质生成的 WGSL 与改动前逐字相同 | **相同**。15 个材质 × 工作台、实时 GI，每页生成的全部 WGSL 模块哈希与 `before/wgsl.json` 一致，没有新模块 |
| 参考场景集的工作台导出与改动前逐像素相同 | **相同**。`SCENE_E2E_WORKBENCH`：6 个场景 27 张图（含正交白模），平均差全部为 0 |
| W1 的存档对照（线框与线稿） | **通过**（`SCENE_E2E_W1`） |
| 实时 GI（不构建这一层）的导出与改动前逐像素相同 | **跨运行无法逐像素复现，见下**。21 张里 15 张逐像素相同；其余 6 张的差异与「同一份代码两次运行之间」的差异同量级，没有差超过 8 级的像素 |
| 已发布的预设不升版本 | 没有升版本 |

## 实时 GI：两次运行之间本来就不完全相同

改动前后的 WGSL 完全一样，导出图却有几张不同，所以又用改动后的同一份代码把其中 4 个场景再导出一次，比较两次运行：

| 导出图 | 改动前 ↔ 改动后：不同像素 | 最大差 | 差超过 8 级的像素 | 改动后两次运行之间：不同像素，最大差 | 改动前 ↔ 第二次：不同像素，最大差 |
|---|---|---|---|---|---|
| `backlit-room-shot-webgl2` | 0.00% | 0 | 0.0000% | — | — |
| `backlit-room-shot-webgpu-compat` | 0.00% | 0 | 0.0000% | — | — |
| `backlit-room-shot-webgpu` | 0.00% | 0 | 0.0000% | — | — |
| `cornell-shot-webgl2` | 0.00% | 0 | 0.0000% | 9.46%，5 | 9.46%，5 |
| `cornell-shot-webgpu-compat` | 0.00% | 0 | 0.0000% | 0.00%，0 | 0.00%，0 |
| `cornell-shot-webgpu` | 0.00% | 1 | 0.0000% | 5.97%，3 | 5.97%，3 |
| `materials-board-webgl2` | 0.00% | 0 | 0.0000% | — | — |
| `materials-board-webgpu-compat` | 0.00% | 0 | 0.0000% | — | — |
| `materials-board-webgpu` | 0.00% | 0 | 0.0000% | — | — |
| `mirror-board-shot-webgl2` | 0.00% | 0 | 0.0000% | — | — |
| `mirror-board-shot-webgpu-compat` | 0.00% | 0 | 0.0000% | — | — |
| `mirror-board-shot-webgpu` | 0.00% | 0 | 0.0000% | — | — |
| `noon-shot-webgl2` | 0.00% | 0 | 0.0000% | 0.00%，0 | 0.00%，0 |
| `noon-shot-webgpu-compat` | 0.00% | 0 | 0.0000% | 0.00%，0 | 0.00%，0 |
| `noon-shot-webgpu` | 1.98% | 3 | 0.0000% | 1.98%，3 | 0.00%，0 |
| `room-shot-webgl2` | 0.00% | 1 | 0.0000% | 0.00%，1 | 0.00%，0 |
| `room-shot-webgpu-compat` | 3.57% | 2 | 0.0000% | 3.38%，4 | 3.57%，4 |
| `room-shot-webgpu` | 3.60% | 4 | 0.0000% | 3.47%，3 | 3.26%，3 |
| `thin-wall-shot-webgl2` | 0.00% | 0 | 0.0000% | 0.00%，0 | 0.00%，0 |
| `thin-wall-shot-webgpu-compat` | 0.00% | 0 | 0.0000% | 0.00%，0 | 0.00%，0 |
| `thin-wall-shot-webgpu` | 2.03% | 1 | 0.0000% | 2.03%，1 | 0.00%，0 |

- 「不同像素」按 RGB 任一通道不同计，「最大差」是 0–255 的级数。
- 同一份代码的两次运行，差异与改动前后同量级（最大 5 级）；`noon`、`thin-wall` 的第二次运行与改动前逐像素相同，第一次不同：同一张图在几种结果之间随机落在其中一种。
- 这是改动前就有的现象：实时 GI 的导出在不同页面、不同进程之间不能逐像素复现。R2 的确定性用例（AC-R2-5，`SCENE_E2E_HIDDEN`）对实时 GI 的标准本来就是「一级以内」：最大差不超过 1，或差超过 8 级的像素不超过 0.01%。上表全部满足后一条。
- 所以这一项的证据是：WGSL 逐字相同（同样的着色器、同样的输入），加上导出差异不超过同一份代码两次运行之间的差异。按 AC-S1-3 的原文（逐像素相同）跨运行验证做不到，**判据待 owner 确认**。
- 这也影响 AC-S1-4 和 Epic AC-5（连续两次导出逐像素相同，比 R2 的「一级以内」严）：任务 6.1 要先确认同一页里连续两次导出是否逐像素相同，不同就先找到来源。

## 重新生成

`before/` 见 `README.md`。改动后的对照（Vite 在 5191）：

```sh
cd frontend
SCENE_E2E_ONLY="wgsl:" SCENE_E2E_WGSL=<evidence>/before CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs
# 工作台：先在改动前的代码上 SCENE_E2E_WORKBENCH_WRITE=1 写参考图，再在改动后的代码上对照
SCENE_E2E_ONLY="workbench:" SCENE_E2E_WORKBENCH=<目录> [SCENE_E2E_WORKBENCH_WRITE=1] CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs
SCENE_E2E_ONLY="W1:" SCENE_E2E_W1=<目录> CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs
SCENE_E2E_ONLY="gi-baseline" SCENE_E2E_GI_BASELINE=<目录> SCENE_E2E_GI_COMPAT=1 CREATIVE_SCENE_URL=http://localhost:5191 node scripts/creative-scene.e2e.mjs
```

改动后的导出图在本机，没有提交。
