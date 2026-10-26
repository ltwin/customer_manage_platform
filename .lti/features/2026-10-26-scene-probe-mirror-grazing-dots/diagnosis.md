# Diagnosis

> 状态：先记录（owner 2026-10-26「已知但难以解决问题可以都记录issue，然后你继续推进」）。来源是 `scene-probe-gi`（S2）任务 5.2b 修好镜中偏亮、偏蓝之后剩下的小瑕疵，代码在 `feat/scene-probe-gi`（e156cb93），合入 develop 之前按该分支复现。症状每次都能复现；根因是中等置信度的判断，下面写了验证方法。

## Symptom

镜子里斜着看到的地面上，有一行稀疏的单像素亮点，比周围的地面亮。正对镜子看的墙、球没有亮点。

- 复现场景：`thin-wall` 去掉柜子，加一面白色金属镜（金属 1、粗糙度 0）和一个红球，用例 `probes diag: a wall seen in a mirror and out of it`；
- 实际：`mirror.png` 镜中地面靠近镜面下沿、视线最斜的那一带，有一串零星亮点；
- 期望：镜中地面亮度连续，与直接看的地面一致。

对照图 `scene-probe-gi/discovery/research/evidence/after/mirror-compare.jpg` 右上（改后），亮点要放大才看得清。5.2b 之前这一带整片偏蓝白，亮点被盖住了。

## Root Cause

**中等置信度**：同一片镜中地面，有两条路给出颜色，两条路对地面自身的高光处理不同：

1. **屏幕命中**（多数像素）：`SSRTraceNode` 在屏幕上找到地面，取 `seen` 的颜色。`seen` 是不含反射、`cover = 1` 的合成（`frontend/src/creative-scene/render/realtimeGI.ts:298`），所以地面自身的环境高光是 0，只剩多次反弹从上一帧反射里补回的那部分（`frontend/src/creative-scene/render/nodes/SSRTraceNode.js:1096`）；
2. **世界追踪**（少数像素）：屏幕步进没命中的，或屏幕边缘带的那份（`SSRTraceNode.js:1080-1130`），以及步进走到表面背后的光线（`SSRTraceNode.js:1250-1275`），交给 `hitShading` 重追。命中点按地面的菲涅耳算高光，沿反射方向读探针（`frontend/src/creative-scene/render/trace/hit.ts:284-296`）。视线越斜，菲涅耳越接近 1，这份高光越亮。

于是镜中斜看的地面上，零星改走世界追踪的像素比周围亮。补追的像素零散，是因为屏幕步进在掠射角下沿深度缓冲的台阶时中时不中。

**验证方法**：
- `SCENE_E2E_PROBE_MIRROR_SHADE=object` 出一张命中来源图，看亮点是否正好是世界追踪的像素；
- 临时把 `hit.ts` 里 `radiance` 那一项乘 0，看亮点是否消失。

## Evidence(红)

```sh
# feat/scene-probe-gi，约 1 分钟；结果写进 <dir>/mirror.png 与 mirror.json
cd frontend && SCENE_E2E_PROBE_MIRROR=<dir> SCENE_E2E_ONLY="probes diag: a wall seen in a mirror" \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

现在只能看图判断，还没有数值判据。修的时候先给用例加一项：镜中地面那一带的局部亮度跳变（例如单像素与 3×3 邻域中值之比）不超过某个阈值。

## Blast Radius

- 只出现在镜面（预通道粗糙度很低）里斜看到的、本身有可见高光的表面，也就是地面、桌面这类大平面；
- 没有探针（S1 的路径）时也有同样的不一致，只是那时世界追踪的高光取天空，与屏幕命中的差别被整片蓝色盖住；
- 导出同样受影响，参考场景的数值用例都不读这一带。

## Fix Direction

1. **两条路都不算命中点自身的高光**：`hitShading` 用于反射补追时，可以选择去掉 `radiance` 项，与 `seen` 一致。代价是镜中光滑表面的高光整片消失，S1 的「镜中镜」用例要复核。
2. **两条路都算**：`seen` 留下环境高光，不乘 `1 − cover`。代价是屏幕命中的高光又回到不受遮挡的天光，正是 5.2b 修掉的偏蓝。除非 `envSpec` 也换成探针沿反射方向的辐照度，但那要多一趟全屏的探针查找。
3. **从源头少补追**：放宽掠射角下的屏幕命中判定，减少零星补追。改的是 three 的步进，风险最大。

倾向先试第 1 种，再看多次反弹补回的那部分是否足够。
