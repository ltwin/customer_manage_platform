# 光源尺寸之前的基线（任务 1.2）

2026-10-26，`feat/scene-light-size-spike`，在 `be38cb06` 之上加入任务 1.1 的固定数据。

- **设备**：Mac16,8（Apple M4 Pro），darwin 26.5。
- **浏览器**：Chromium 145.0.7632.6，ANGLE Metal，渲染器为 WebGPU。
- **视口**：1600 × 1000，2× 像素。
- **导出**：`lit`，2K。两个场景分别用 `workbench` 和 `realtime_gi` 各导出一次。

```sh
cd frontend && CREATIVE_SCENE_URL=http://localhost:<端口> SCENE_E2E_ONLY="light baseline" \
  SCENE_E2E_LIGHT_BASELINE=<目录> node scripts/creative-scene.e2e.mjs
```

| 场景 | 渲染器 | 尺寸 | 导出耗时 |
|---|---|---|---|
| `frame-two-lights` | `workbench` | 1365 × 2048 | 0.65 s |
| `frame-two-lights` | `realtime_gi` | 1365 × 2048 | 7.5 s |
| `occluder-wall` | `workbench` | 2048 × 1365 | 0.36 s |
| `occluder-wall` | `realtime_gi` | 2048 × 1365 | 3.7 s |

数据见 `light-baseline.json`，图见同目录的 PNG。

## 从图里看到的现状

- **`frame-two-lights`**：
  - 与 owner 反馈的画面一致：后墙上有几道边缘生硬的斜向明暗带，左上那盏平行光和右下那盏点光的阴影都是一刀切的硬边。
  - 场景摄像机是 2:3 的竖幅，owner 的截图是编辑器视角的横幅。
- **`occluder-wall`**：三块板的影子都是硬边，不随距离变化，正是 AC-1 要改的现象。另有两处现状缺陷，留给第 2 组测量：
  - **自阴影条纹**：聚光斜照墙面时，整面墙出现细密的斜向条纹。聚光的 `bias` 为 0，又没有随角度变化的偏置。对应 Epic 技术选型里的「接收面深度偏置」。
  - **锯齿**：离墙 3 m 那块板的影子边缘锯齿明显。聚光的阴影贴图是 1024，光锥半角 0.9 rad，覆盖范围太大，texel 粗。

## 一次偶发的导出卡住

第一次运行时，`frame-two-lights` 的 `realtime_gi` 导出 600 s 内没有提交，用例超时。紧接着用 `SCENE_E2E_CONSOLE=1` 重跑，又正常完成（约 15 s），之后再跑一次仍然正常。

- 浏览器控制台里只有已知的 three 弃用警告，没有报错。
- 那次运行紧跟在一个 207 s 的长用例之后，机器负载较高。

没有再复现，也不在 S0 范围内，先记录在这里。如果之后再次出现，按 `2026-10-25-scene-trace-ready-stall` 的思路，用 tracing 查是否卡在世界空间追踪层就绪之前。
