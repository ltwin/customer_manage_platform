# Diagnosis

> 状态：先记录（owner 2026-10-26「已知但难以解决问题可以都记录issue，然后你继续推进」）。来源是 `scene-probe-gi`（S2）任务 5.2 的调参结果。`cornell` 立方体那一处 owner 当天已按已知偏差接受，AC-S2-2 按 3 次命中的口径判定，这份 issue 不阻塞 S2。代码在 `feat/scene-probe-gi`，合入 develop 之前按该分支复现。根因是中等置信度的判断。

## Symptom

探针的间接光与逐像素参考（每像素 256 条路径）相比，有两处偏亮，调参改不掉：

1. **`cornell` 立方体朝红墙的那一面**：+21%（AC 的 ±15%），追 6 次的参考下 +22%。数值不随参考追的次数变化，是局部的偏差；
2. **薄墙场景，参考追得越深越亮**：`thin-wall` 整图比值在追 1 次时为 0.97、3 次 1.04、6 次 1.15。按 6 次算，窗下墙面、墙角、阴影里的地面在 +15% 到 +19%，`@level2` 隔窗看的室内 +31%。按 AC 的 3 次口径，这几处都在 ±15% 以内。

数值与对照图在 `scene-probe-gi/discovery/research/evidence/ac-s2-2.md`、`after/probes-5.2.json`、`after/ac-s2-2-views.jpg`。

## Root Cause

**1. `cornell` 凹角（中等置信度）：探针间距相对这个凹角太粗。**
这个面离红墙 0.6 m，落在第 1 级（间距 0.5 m），面和墙之间只有一层探针。`frontend/src/creative-scene/render/probes/lookup.ts:98-143` 的 `levelAt` 在八个探针间三线性插值，这层探针看到的红墙比面上的点看到的多，所以偏亮。试过的做法都改不了它（`ac-s2-2.md`「试过的做法」）：
- 采样点偏移 0.3 / 0.5 / 0.7 × 间距、偏移方向（`lookup.ts:101`）；
- 关掉重定位；
- Chebyshev 取 1、3、6 次方（`lookup.ts:130-131`）。

**2. 薄墙（中等置信度）：漏过 2 cm 墙的光进了探针之间的反弹链。**
墙比探针间距（第 0 级 0.25 m）薄得多。墙外的探针与墙内表面之间，Chebyshev 可见性只能部分挡住（`lookup.ts:126-133`，小权重压低的门槛是 0.2），所以有少量墙外的光漏进室内。探针每帧读上一帧的探针作为光线命中处的间接光，这份漏光逐次累积：参考追得越深，读探针的比重越小，参考越接近真值，差距就越明显。`@level2` 用的是第 2 级（间距 1 m），漏得最多。

**验证方法**：
- 把墙加厚到 0.25 m，看整图比值是否不再随追的次数上升；
- 给探针光线加一项「命中背面就记为黑」的统计，看墙外探针打进墙内的比例。

## Evidence(红)

```sh
# feat/scene-probe-gi；SCENE_E2E_PROBE_BOUNCES=6 换成追 6 次的参考，SCENE_E2E_PROBE_VIEWS 只取几个视图
cd frontend && SCENE_E2E_PROBES=<dir> SCENE_E2E_ONLY="probes: AC-S2-1 and AC-S2-2" \
  SCENE_E2E_PROBE_VIEWS=cornell,thin-wall,thin-wall@level2 \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

用例在 `<dir>/probes.json` 写出每个区域相对参考的误差：`cornell` 立方体 > +15% 为红；薄墙一项以 `SCENE_E2E_PROBE_BOUNCES=6` 下 `@level2` > +15% 为红。

## Blast Radius

- 凹角、贴墙放置的物件：所有落在单层探针里的近距离凹角；
- 薄墙房间：墙比所在级的探针间距薄得多的室内场景，从屋外远看（第 2 级以上）时最明显；
- 命中点着色（反射补追、探针光线自己）也读同一份探针，镜中的室内同样偏亮。

## Fix Direction

1. **凹角**：
   - 换屏幕空间的短半径修正，SSGI 在近处补细节（S2-U7 的对照图里 AO 能压暗墙角，owner 已定缺省不加）；
   - 或者在 Epic 后续用 Lumen 式屏幕探针（方案 C）。
2. **薄墙**：
   - 探针光线命中单面墙背面时，按 DDGI 的做法把那个探针标为「在墙里」并关掉；
   - 或按法线和朝向偏移，把采样点推离墙体；
   - 再不行就要求场景的墙有厚度，写进场景格式的提示。
3. 修之前先把「追 6 次」的薄墙一项做成用例的判据，免得调参时只看 3 次口径。
