# Diagnosis

> 状态：先记录（owner 2026-10-24「尚未解决的问题可以先记录成 issue」）。症状稳定复现，根因未查明。

## Symptom

e2e「real-time GI: the box room bleeds red beside the red wall, mirrors the walls in the ball and darkens contacts, against the workbench」稳定失败：

```
AssertionError [ERR_ASSERTION]: the ball mirrors more than the workbench's environment
```

Cornell 盒子 2K 出图，材质光照，同一区域对比工作台与实时 GI。实测（develop 基线 `934f08ba`，本机 Metal 下的无头 Chromium，2026-10-24）：

```json
{"lit_floor_by_red_wall_red_green":[0.876,2.224],"lit_cube_face_red_green":[0.875,2.56],"lit_ball_difference":8.063,"lit_floor_by_green_wall_green_red":[1.25,1.515],"lit_cube_front_foot_grey":[136.549,144.391],"lit_cube_contact_grey":[7.793,112.535]}
```

- **球的反射**：金属球区域两种渲染器的平均差 8.06，期望 ≥ 10。render-settings 交付时测得 22.2（`scene-render-settings` tasks.md 第 88 行）。
- **接触变暗**：`lit_cube_contact_grey` 工作台 7.8、实时 GI 112.5，像是方向反了。它的断言排在球之后，这次没执行到，是否也会失败未知。

`feat/scene-volumes` 上同样失败，修复前后结果一样，与 W5 无关。

## Root Cause

未查明。可疑方向：

- 交付之后 develop 上改过显示观感（W1 AgX 与白模，`scene-display-look`）、曝光（W2 物理天空与相机曝光）、天空图层（`73067304`），以及阳光级联阴影（`f8b2f34e`）。其中任何一个都可能改变 SSR 的命中或环境的亮度，让「球比工作台多反射的量」变小。置信度低，需要二分。
- 接触区域的数值反常，可能是测量框的位置在某次修改后不再落在接触处（例如场景或相机缺省值变了），也可能是 AO 合成真的回归了。

验证方法：用这项检查在 `scene-render-settings` 归档的提交与 `934f08ba` 之间做 `git bisect`。

## Evidence(红)

```sh
cd frontend && CREATIVE_SCENE_URL=http://localhost:<port> SCENE_E2E_ONLY="real-time GI: the box room bleeds" node scripts/creative-scene.e2e.mjs
```

develop `934f08ba` 与 `feat/scene-volumes` 上都稳定失败（2026-10-23、10-24 多次运行）。

## Blast Radius

- 实时 GI 的 SSR 与 AO 合成（`render/realtimeGI.ts`），视口和出图都受影响。
- 同一节的其他区域断言（红墙渗色、立方体侧面）实测仍满足阈值。

## Fix Direction

1. 先 bisect 找到引入回归的提交。
2. 如果是有意的观感变化（例如 W1 的 AgX），同 owner 确认后重新标定阈值，写明依据；如果是真回归，就修 SSR 或 AO 合成。
3. 接触区域另外核对测量框位置。
