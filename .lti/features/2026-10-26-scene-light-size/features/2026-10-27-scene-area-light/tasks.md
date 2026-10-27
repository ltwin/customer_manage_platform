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

- [ ] 2.1 面光源字段：`model.go` 的 `Light`、`lightPatch`、`lightFields`、`lightNames`、`completeLight`、`validateLight` 加 `shape`、`width`、`height`，强度缺省与范围按类型。先写测试（`area_light_test.go`、`scene_test.go`）：补齐、范围、形状枚举、面光源写 `radius_m` 和点光写 `width` 返回 400 并指出字段。完成判据：`make check-go PKG=./internal/creativescene/...` 通过。
- [ ] 2.2 换类型换算（I6）：从 `lookat.go` 抽出只按 `s.Objects` 计算世界矩阵的函数，`lookAtScene.worldMatrix` 改用它；`mergeLight(cur, p, before, after)` 按世界半轴叉积算面积，换算后截断，显式强度优先；`updateObjectsOp.apply` 在写 `o.Transform` 之前算 `before`。先写测试：点光 400 cd ↔ 面光源 6400 cd/m²、截断、显式强度优先、父级带缩放、同一次改缩放和类型。完成判据：同上，`lookat` 原有测试不变。
- [ ] 2.3 对外说明：`openapi.yaml` 的两个灯光 schema 加 `area` 和三个字段、强度上限按类型，`make generate` 后提交两份生成物；`scene_tools.go` 的灯光字段与打光指引补面光源，`scene_tools_test.go` 断言新字段。完成判据：两个包的 `check-go` 通过，`make generate-check` 干净，`TestLightModelStalesLitOnce` 不变。

## 3. 光栅着色与阴影

- [ ] 3.1 灯光子类与矩形面光源：`lights.ts` 加 `SceneAreaLight`（`updateMatrixWorld` 算世界半轴）、`SizedAreaShadow`（near 0.05，`copy()` 带轴）、`hasSize`；`nodes.ts` 注册 `AreaLightNode`、初始化 LTC 表、升级核对清单；`objects/lights.ts` 的 `buildLight('area')`、`applyLight`、`lightKey`；`SceneLights.tsx` 传 `final`；`litLook.ts` 加 `area: 10000`；`engines.ts:softShadowsIn` 认面光源。提交参考场景 `area-studio`。先写 e2e：WebGPU 和 WebGL2 下面光源所在之处着色不缺失、不全黑，背面不受光。完成判据：e2e 通过，S1 的 AC-S1-2 尺寸为 0 基线不变，`make check-frontend` 通过。
- [ ] 3.2 椭圆 LTC：新增 `ltc.ts`（椭圆形状因子、`LTC_ClippedSphereFormFactor` 副本、照度 π·L·FF）。先写 `creative-scene-ltc.test.ts`：正对与 πL·R²/(R²+d²) 对照，不对称视角与数值积分对照。完成判据：单测通过。
- [ ] 3.3 直接光：`direct.ts` 的 `installAreaDirect` 包住 `directRectArea`，椭圆分支、clearcoat、sheen。完成判据：light-check 在 `area-studio` 上 AC-S2-1 的能量（≤ 5%）和 AC-S2-2（远处 ≤ 2%，近处 ≤ π·L）达标；不达标先查实现，再交 owner。
- [ ] 3.4 软阴影：`shadowFilter.ts` 的 `cubePCSS({ kernel, search, filter, maxTexels })` 加矩形、椭圆核与搜索核，上限按实际核轴换算，采样档按 `final` 选；`attachSoftShadow` 按类型选核；`sceneRenderer.ts` 的 standIn 与 `volumes/shadow.ts` 按立方体处理面光源。完成判据：AC-S2-1 的全图阴影误差 ≤ 0.005、板剖面 1×1 m ≤ 0.02、2×0.5 m 和 2×2 m ≤ 0.05；`workbench` 导出连续两次逐像素相同。

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
