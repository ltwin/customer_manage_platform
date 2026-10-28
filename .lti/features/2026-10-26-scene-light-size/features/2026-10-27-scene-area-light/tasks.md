# Tasks

e2e 都在 `frontend/scripts/creative-scene.e2e.mjs`，用 `SCENE_E2E_ONLY` 过滤，`CREATIVE_SCENE_URL` 换端口（不用 5173、5179）。测量用 `frontend/scripts/light-check/`。各任务先写断言，看它失败，再实现。

## 1. 未决事项 N2

- [x] 1.1 查清 S0 矩形偏亮 4%：正对的矩形和圆盘，在法线上取几处距离，光栅、参考解（`light-check/reference.ts`）和解析解三方对照，定位是哪一边的归一化不对，修掉那一边；参考解有误时复查 S1 用到的同一段代码。完成判据：三方在 1% 内一致，原因和修法写进本条结果；改的是参考解时，S1 的 AC-S1-3、AC-S1-4 重测仍达标。
  - 结果：光栅一边的问题，参考解没错，S1 不受影响。three r186 的 `directRectArea` 比它自己的 `direct` 少了漫反射的 `1 − F`（`PhysicalLightingModel.js:673` 有，`:714` 没有），F0 = 0.04 时约 4%，与尺寸无关；高光的 `multiScatteringCompensation` 和 sheen 的漫反射能量系数也没有。参考解和命中点（`hit.ts:316`）都按 `direct` 乘了 `1 − F`。
  - 测法：在 light-check 里临时放 S0 的 1×1 m 软箱，墙面取 4 处像素，用墙色 #bdbdb8 的 R、B 差把漫反射分离出来，与 CPU 上对发光面积分的照度（解析解）对照。
    - 漫反射：光栅为解析解的 1.000 倍，参考解为 0.959 倍（即 1 − F）；高光两边一致（0.041 / 0.040）。两边的几何归一化都没错。
    - 整面墙光栅 / 参考解：1×1 m 为 1.040，2×2 m 为 1.043；临时给漫反射乘上 `1 − F`（方向取灯的中心）后为 1.0016 和 1.0044，4 处像素都在 1% 内。
  - 修法写进 design「直接光」：`installAreaDirect` 先另存 three 的结果，再给漫反射乘 `1 − F`（方向取向量形状因子的平均方向）、给高光乘 `multiScatteringCompensation`，在任务 3.3 实现；命中点的面光源分支同样乘 `1 − F`（任务 4.1）。临时测量代码已删除。

## 2. 数据与服务端

- [x] 2.1 面光源字段：`model.go` 的 `Light`、`lightPatch`、`lightFields`、`lightNames`、`completeLight`、`validateLight` 加 `shape`、`width`、`height`，强度缺省与范围按类型。先写测试（`area_light_test.go`、`scene_test.go`）：补齐、范围、形状枚举、面光源写 `radius_m` 和点光写 `width` 返回 400 并指出字段。完成判据：`make check-go PKG=./internal/creativescene/...` 通过。
  - 结果：通过。新增 `area_light_test.go` 的 `TestAreaLightDefaults`、`TestAreaLightRanges`；`scene_test.go` 的「unknown light type」改用 `hemisphere`（`area` 已是合法类型），未知字段的提示里多了 `shape、width、height`。强度越界的提示改为带类型名和该类型的上限（「面光源须有 … 0…100000 的强度」）。
- [x] 2.2 换类型换算（I6）：从 `lookat.go` 抽出只按 `s.Objects` 计算世界矩阵的函数，`lookAtScene.worldMatrix` 改用它；`mergeLight(cur, p, before, after)` 按世界半轴叉积算面积，换算后截断，显式强度优先；`updateObjectsOp.apply` 在写 `o.Transform` 之前算 `before`。先写测试：点光 400 cd ↔ 面光源 6400 cd/m²、截断、显式强度优先、父级带缩放、同一次改缩放和类型。完成判据：同上，`lookat` 原有测试不变。
  - 结果：通过。`TestAreaLightTypeChangeKeepsLuminousIntensity` 覆盖 13 种情况（含父级 2×3 缩放、父级拉伸加子级旋转 45° 的平行四边形、同一次改缩放和类型的两个方向、两种截断、显式强度、与平行光互换）。
  - 实施时调整：
    - 没有另抽世界矩阵函数，而是加 `sceneWorld(objects)` 构造只含现有物件的 `lookAtScene`，复用它的 `worldMatrix`；原来 `lookat.go`、`reparent.go`、`lens.go` 三处各自手写的构造也改用它。面积系数 `worldAreaScale` 取世界矩阵前两列叉积的长度；
    - `updateObjectsOp.apply` 改为先写完本次所有更新、最后合并灯光：「更新后的缩放」要包括同一批里父级的变换，「更新前」取本次 op 开始时的快照（只在有面光源换类型时复制）。
- [x] 2.3 对外说明：`openapi.yaml` 的两个灯光 schema 加 `area` 和三个字段、强度上限按类型，`make generate` 后提交两份生成物；`scene_tools.go` 的灯光字段与打光指引补面光源，`scene_tools_test.go` 断言新字段。完成判据：两个包的 `check-go` 通过，`make generate-check` 干净，`TestLightModelStalesLitOnce` 不变。
  - 结果：通过。`scene_tools_test.go` 断言新增的面光源字段、亮度单位、换算和「柔光箱用面光源」指引，`creativeagent` 的 lint 为 0，`make generate-check` 干净，`TestLightModelStalesLitOnce` 不变。
  - 计划外改动：
    - `compaction_regression_test.go` 的 `TestCompactionRejectsSummaryBeyondWindow` 窗口从 10000 调到 10500：`edit_scene` 的说明变长后，工具定义放不下。先例是 S1 和 W3 夜空；
    - 生成的 schema 给灯光类型加了 `area`，前端几张按类型的表不补就编译不过，这一步先补了：`state/apply.ts` 的 `lightFields` 和 `lightDefaults`（形状、宽、高）、`editing.ts` 的 `lightLabels`、`litLook.ts` 的 `NEW_LIGHT_INTENSITY`（10000）。按类型的强度缺省和本地换算留在任务 5.1。

## 3. 光栅着色与阴影

- [x] 3.1 灯光子类与矩形面光源：`lights.ts` 加 `SceneAreaLight`（`updateMatrixWorld` 算世界半轴）、`SizedAreaShadow`（near 0.05，`copy()` 带轴）、`hasSize`；`nodes.ts` 注册 `AreaLightNode`、初始化 LTC 表、升级核对清单；`objects/lights.ts` 的 `buildLight('area')`、`applyLight`、`lightKey`；`SceneLights.tsx` 传 `final`；`litLook.ts` 加 `area: 10000`；`engines.ts:softShadowsIn` 认面光源。提交参考场景 `area-studio`。先写 e2e：WebGPU 和 WebGL2 下面光源所在之处着色不缺失、不全黑，背面不受光。完成判据：e2e 通过，S1 的 AC-S1-2 尺寸为 0 基线不变，`make check-frontend` 通过。
  - 结果：新增 e2e「area light: lights what it faces and nothing behind it」：`area-studio` 的 1×1 m 面光源对照隐藏它的同一场景，WebGPU 和 WebGL2 各有 204 / 256 格被照亮；灯转半圈后这些格都回到隐藏时的亮度（0 格仍亮）。「reference scenes: each opens」含 `area-studio` 通过；AC-S1-2 尺寸为 0 对 S1 的 `baseline-after`：`workbench` 差 0 级，`realtime_gi` 最大差 1 级。类型检查和 oxlint 无新增问题。
  - 实施时调整：
    - `AreaLightNode.update` 先调父类（它更新颜色），再用灯上的世界半轴覆盖父类按 `extractRotation` 算出的 `halfWidth`、`halfHeight`，效果同 design；
    - `SceneAreaLight` 同时覆写 `updateWorldMatrix`，`getWorldPosition` 这类只走它的调用也会更新半轴；加了 `area()`（平行四边形面积），面板和体积共用；
    - 文档层加 `lightHasSize`（面光源恒真），`lightKey`、`softShadowsIn` 用它；`lightSize` 对面光源返回 0；
    - e2e 的 `referenceScene` 和 `sizeless()` 只给点光、聚光写 `radius_m`（原来是 6.2 的一项）；「light size 0」只比 S1 的两个场景。
  - 待 3.4：这一步面光源还没有 PCSS，是硬阴影；墙面右侧有立方体阴影图的自阴影条纹，S1 修条纹的抬升在 PCSS 里，3.4 接上后复查。
- [x] 3.2 椭圆 LTC：新增 `ltc.ts`（椭圆形状因子、`LTC_ClippedSphereFormFactor` 副本、照度 π·L·FF）。先写 `creative-scene-ltc.test.ts`：正对与 πL·R²/(R²+d²) 对照，不对称视角与数值积分对照。完成判据：单测通过。
  - 结果：`creative-scene-ltc.test.ts` 4 项通过：三次方程的根；正对的圆盘 4 组 R、d 与 R²/(R²+d²) 相差 < 10⁻⁶；3 个偏离法线、半轴不垂直的椭圆与数值积分（400 × 800 中点）相差 < 0.2%；LTC 的倾斜矩阵下，椭圆与 three 的多边形 LTC（照抄它取负 T2 的坐标系，精确棱积分）算的 256 边形相差 < 0.2%，背面为 0。
  - 实施时调整：三次方程不用 ltc_disk 的 Blinn 解法，改用 Viète 的三角解法。Blinn 的算法 D 给出的不一定是最小根（ltc_disk 事后排序），而正对的圆盘是重根，它在那里得到 0/0（单测先红了这两处）。Viète 对三个实根直接给出有序结果，acos 的参数钳在 [−1, 1]，重根时就是两个相等的根。
  - TSL 版本与 CPU 版本逐行对应；TSL 的正确性由 3.3 的参考解对照验证。
- [x] 3.3 直接光：`direct.ts` 的 `installAreaDirect` 包住 `directRectArea`，椭圆分支、clearcoat、sheen。完成判据：light-check 在 `area-studio` 上 AC-S2-1 的能量（≤ 5%）和 AC-S2-2（远处 ≤ 2%，近处 ≤ π·L）达标；不达标先查实现，再交 owner。
  - 结果（新增 e2e「area light: energy as the reference has it …」，参考解 1024–4096 帧）：
    - AC-S2-1 能量（不带阴影，光栅 / 参考解）：墙面、高光板分别为矩形 1×1 m 1.0023、1.0004，椭圆 1×1 m 1.0021、1.0003，矩形 2×0.5 m 1.0023、1.0004，椭圆 2×0.5 m 1.0020、1.0002，矩形 2×2 m 1.0052、1.0016。S0 的 4% 偏差已消除。
    - AC-S2-2：法线上 10 倍边长（0.5 m 光源、5 m）处的照度为 `L·A/d²` 的 0.9965（矩形）、0.9973（椭圆），参考解相同，剩下的 0.3% 是 10 倍边长处真实的近场项；离墙 5 cm 时最大照度为 π·L 的 0.992、0.990，没有非有限值。
    - AC-S2-6 的两个后端 e2e 加上椭圆：WebGPU、WebGL2 下矩形 204 格、椭圆 203 格被照亮，转半圈后 0 格。
  - 实施时调整：
    - 漫反射和 sheen 不自己写公式，而是再调用一次 three 的 `direct`：方向取向量形状因子的平均方向（低于地平线时抬到 10⁻³），光源颜色取「π·L·F ÷ cos」，照度正好是 π·L·F。这样 `1 − F`、sheen 和 sheen 的能量系数都与点光同一套公式，远处退化为点光。design「直接光」的伪代码按此改写；
    - `rectVectorFormFactor` 在视空间里按 three 的角点顺序求和得到的是 −F：three 的 LTC 坐标系取负了 T2，是一个镜像，把方向翻了过来。第一次跑矩形漫反射几乎全黑（墙面 0.079），取负后正常；椭圆的算法自己把 V3 朝向中心，不受影响；
    - 加 `clippedFormFactor(f, n)`：裁剪只用 |f| 和 f·n，在视空间里直接算；
    - light-check 加 `area-studio` 的 `shape`、`areaW`、`areaH`、`nits` 参数和 `layout=far`、`layout=close`，参考解改为从灯上读形状和世界半轴；`measure.ts` 加 `axisIrradiance`、`nearIrradiance`，用墙色 R − B 分离出漫反射再换算照度（同 N2 的测法）。
- [x] 3.4 软阴影：`shadowFilter.ts` 的 `cubePCSS({ kernel, search, filter, maxTexels })` 加矩形、椭圆核与搜索核，上限按实际核轴换算，采样档按 `final` 选；`attachSoftShadow` 按类型选核；`sceneRenderer.ts` 的 standIn 与 `volumes/shadow.ts` 按立方体处理面光源。完成判据：AC-S2-1 的全图阴影误差 ≤ 0.005、板剖面 1×1 m ≤ 0.02、2×0.5 m 和 2×2 m ≤ 0.05；`workbench` 导出连续两次逐像素相同。
  - 结果（新增 e2e「area light: soft shadows as the reference has them」，参考解 4096 帧，光栅出图档 64 帧平均）：

    | 面光源 | 全图阴影误差（≤ 0.005） | 板剖面均值 0.05 / 0.5 / 3 m |
    |---|---|---|
    | 矩形 1×1 m | 0.0018 | 0.0022 / 0.0023 / 0.0108（≤ 0.02） |
    | 椭圆 1×1 m | 0.0015 | 0.0022 / 0.0016 / 0.0100（≤ 0.02） |
    | 矩形 2×0.5 m | 0.0018 | 0.0012 / 0.0017 / 0.0058（≤ 0.05） |
    | 矩形 2×2 m | 0.0037 | 0.0018 / 0.0053 / 0.0095（≤ 0.05） |

    - 0.5 m 板的半影宽度与参考解相差不超过一个 5 mm 的格；2×0.5 m 时左右 0.17–0.18 m、上下 0.04 m，矩形核给出了两个方向不同的半影。3.1 时墙面右侧的自阴影条纹消失。
    - `workbench` 连续两次导出：`occluder-wall` 和 `area-studio` 都逐像素相同（AC-S1-10 的检查加上 `area-studio`）。
    - 两个后端：AC-S2-6 两种形状仍是 204–205 格亮、转半圈 0 格；S1 的 AC-S1-10（点光的圆盘核走同一段代码）0 格异常；「reference scenes: each opens」在视口档下通过。
  - 实施时调整：圆盘核的 `toward` 仍按 S1 的写法在采样时乘半径（面光源的核轴事先乘好），点光的着色器表达式与 S1 相同；`volumeShadowNode` 对面光源用立方体的 `VolumePointShadowNode`，`buildLight('area')` 就挂上（体积在 4.2 读它）；light-check 的标题对 `area-studio` 写 `(area)`。

## 4. 命中点与体积

- [ ] 4.1 命中点：`collect.ts` 收集面光源，`tables.ts` 加 `LIGHT.area` 和面光源行，`hit.ts` 的 `lightAt`、`directAt` 加单独分支（照度 π·L·FF 同作权重与 picked，阴影光线在发光面上均匀取点），`tableLight` 不处理面光源。先写 `creative-scene-trace.test.ts` 的面光源行用例。完成判据：单测通过，`SCENE_E2E_TRACE` 下 AC-S2-3 在 AC-S1-2 同类阈值内。
- [ ] 4.2 体积：`volumes/state.ts` 收集面光源、`structure` 加 `area`、估计里用 2 和等效发光强度；`volumes/lights.ts` 单面余弦光、钳 `max(d², A/π)`、立方体阴影。先写 e2e：投影的面光源照着投影体积，体积着色不缺失、不全黑。完成判据：e2e 通过。

## 5. 编辑器

- [ ] 5.1 本地状态：`state/apply.ts` 的 `lightFields`、按类型的 `lightDefaults`、`lightSize` 不收面光源、`mergeLight` 换算。先写 `creative-scene.test.ts` 用例，与 2.2 的服务端用例取相同输入和结果。完成判据：单测通过。
- [ ] 5.2 界面：`editing.ts` 的 `lightLabels`、`newLight`，`SceneEditor.tsx` 新建菜单，`LightFields.tsx` 的形状、宽高、亮度与等效 cd、切换类型写强度，`SceneObjects.tsx:LightIcon` 的轮廓和朝向短线、`RadiusRing` 排除面光源。先写 e2e：菜单新建、面板改字段与撤销、切换类型、缩放物件时轮廓与发光面的投影范围一致。完成判据：e2e 通过，`make check-frontend` 通过。

## 6. 验收

- [ ] 6.1 性能（AC-S2-4）：`SCENE_E2E_LIGHT_PERF` 加面光源场景，冷启动，与 S1 尺寸为 0 的对照场景比，视口每帧增量与 `realtime_gi` 导出增量写进本条结果；超出阈值交 owner。
- [ ] 6.2 防回退（AC-S2-6）：重跑 AC-S1-2 尺寸为 0 基线与 AC-S1-10 的 e2e；加面光源再删除后视口 `renderTargets` 回到原数；`light-check`、e2e 的 `sizeless()` 不给面光源写 `radius_m`。完成判据：全部通过。
- [ ] 6.3 owner 看图（AC-S2-7）：`area-studio` 的矩形、椭圆面光源，`workbench` 和 `realtime_gi` 各一组，交 owner。
