# 程序化预设样张

由 `frontend/scripts/creative-scene.e2e.mjs` 的「AC-2: every preset compiles and draws」生成（`SCENE_E2E_SHOTS=<目录>`），2026-10-20，标定前。

- `preview.png`：材质预览；`rendered.png`：最终渲染（`SUN_MAX`、`AMBIENT_FILL` 等常数未标定，整体偏亮偏冷，见任务 3.4）。
- 排列（从后往前、从左往右）：
  - 第一排：`principled@1`、`paint@1`、`plaster@1`、`concrete@1`、`stone_brick@1`；
  - 第二排：`stone@1`、`marble@1`、`tile@1`、`wood@1`、`wood_plank@1`；
  - 第三排：`metal@1`、`fabric@1`、`dirt@1`、`grass@1`、`emissive@1`。
- 方块边长 1.2 m，摄像机 35 mm、离地 5 m、俯角 35.5°。瓷砖的 4 mm 灰缝在这个距离不到一个像素，按抗锯齿规则淡成平均色。
