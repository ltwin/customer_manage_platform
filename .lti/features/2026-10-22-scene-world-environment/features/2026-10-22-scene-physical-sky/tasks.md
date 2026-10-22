# Tasks

先在改代码前截取非物理天空的基线，再按「后端字段 → 天文 → 大气 → 曝光与光照 → 编辑界面 → 助手 → 验收证据」推进。证据存进本子项的 `discovery/research/<主题>`。物理天空在第 6 组之前只能经固定数据、单测和 e2e 检查，界面在第 6 组才接上。每个任务结束时 `tsc -b`、`oxlint`、相关单测和 `check-go` 都通过；e2e 只按 `SCENE_E2E_ONLY` 跑受影响的用例，换端口避开 owner 的 Vite。

## 1. 基线

- [x] 1.1 在实施分支第一次改代码之前（与 `develop` 相同）截取 AC-W2-6 的基线，存进 `discovery/research/baseline`：参考场景（程序化天空的正午与黄昏、纯色天空加雾、室内壁灯、材质板）的 `lit` 导出、`render_view` 的 `lit` 和最终渲染的视口截图；另加一台程序化天空太阳入画、打开景深的摄像机；R1 性能场景在程序化天空下的视口帧时间（AC-W2-11 对照用）。e2e 新增「W2 other skies」用例，与基线逐像素比较（差 > 2 的像素为 0）。完成判据：用例在未改动的代码上绿，`baseline.md` 写明场景、机位、设备与测量值
  - 结果见 `discovery/research/baseline.md`。六个参考场景的 `lit` 导出与最终渲染视口直接对照 W1 的 `after/`（在未改动的代码上逐像素相同），不另存；本目录只存三个新场景：`noon-fog`、`room-fog`（两种天空下的雾）和 `noon-sun`（对着太阳、打开景深的摄像机）。雾会把天空连同太阳一起盖住，所以太阳入画单独成一个场景。
  - e2e 新增「W2 baseline」（`SCENE_E2E_W2_BASELINE`）和「W2 other skies」（`SCENE_E2E_W2_SKIES`），都在本机 GPU 上跑；`referenceScene`、`exportReference`、`viewportLooks` 加了可选的 `edit`，用来在参考场景上做变体。
  - `render_view` 与导出同经 `shoot` 出图，导出的 `lit` 覆盖它。帧时间四种模式都贴着 120 Hz 的刷新上限，AC-W2-11 在这台设备上按「是否仍在上限内」判断。

## 2. 数据契约与校验

- [x] 2.1 先写 `scene_test.go`：物理天空只写 `kind`、`date` 时补齐契约缺省值；读取旧快照时补齐 `iso`、`shutter_s`、`exposure_ev100`；返回 400 的写法逐一覆盖——地点三项缺一、时区无效、`asia/shanghai`（大小写不对）、`Local`、空串、日期不存在或越界、`local_time` 格式不对、各数值越界、物理天空带 `turbidity` / `hdri` / `color`、程序化 / HDRI / 纯色天空带任一物理天空独有的字段、写 `night` / `aerial_perspective` / `clouds` / `environment.weather`；`set_render` 写入 `exposure_ev100`（含 0）；摄像机 `iso`、`shutter_s` 的校验与合并。再改 `model.go`（`Sky`、`Camera`、`Render`、`renderPatch`、`validateEnvironment`、`validateCamera`、`mergeCamera`、`completeCamera`、`completeRenderSettings`、`Validate`），新增 `sky.go`（`completeSky`、物理天空的校验、三个非物理分支共用的「物理天空独有字段」判断、`time/tzdata`），改 `ops.go`（`setEnvironmentOp.apply` 写入时补齐、`set_render`）和 `view.go`；`api/openapi.yaml` 的四个 schema 加字段，运行 `make generate`。完成判据：测试先红后绿，`make check-go PKG=./internal/creativescene/...` 通过，生成物已更新
  - 新增 `TestPhysicalSkyIsCompleted`、`TestPhysicalSkyRefusesBadFields`（36 种写法）、`TestOtherSkiesRefusePhysicalFields`、`TestDefaultExposure`、`TestCameraExposure`，先编译失败再全绿；已有用例里写死的完整摄像机、渲染设置补上新字段。
  - 与 design 的偏差（已回写 design）：`exposure_ev100` 不用指针，用 `Render.UnmarshalJSON` 先填缺省值，`Render` 仍可比较；时区大小写不靠 `loc.String()`（实测它只回显传入的名字，macOS 上 `asia/shanghai` 照样通过），改为对照系统时区目录里文件的实际写法，结果缓存。**Epic 契约「data: 物理天空」的 `timezone` 一行仍写着 `loc.String()`，待 owner 同意后随下一次 Epic 修订更正。**
  - 设计挂载点之外的改动：`state/apply.ts:cameraDefaults`（原在 5.1）提前补 `iso`、`shutter_s`，否则生成的 `schema.d.ts` 让 `tsc -b` 失败；`creative-scene.test.ts` 两处摄像机断言随之更新；`agent.go` 拒绝 HDRI 时提示可选物理天空；`creativeagent/scene_export_tools_test.go` 的渲染设置断言补上默认曝光。`tool_schemas.gen.go` 不含场景字段，生成后没有变化。
- [x] 2.2 先改 `fingerprint_internal_test.go`：W1 固定的旧指纹保持不变（旧场景补齐的曝光字段在非物理天空下被清掉）；每条规则一个用例——非 `lit` 或非物理天空下改 `iso`、`shutter_s` 不变；物理天空的 `lit` 下改曝光会变，景深关闭时改 `f_stop` 也会变，改 `render.exposure_ev` 不变；任何时候改 `exposure_ev100` 都不变；物理天空的字段只影响 `lit`。再改 `fingerprint.go`。完成判据：测试先红后绿，`check-go` 通过
  - 加上 2.1 的字段后，W1 固定的三个指纹先变了（白模把补齐的 ISO、快门算了进去），清掉之后恢复原值。新增 `TestCameraExposureFingerprintsOnlyPhysicalLit`、`TestPhysicalLitCountsTheApertureNotTheCompensation`、`TestDefaultExposureNeverCounts`、`TestPhysicalSkyFingerprintsOnlyLit`；`sceneOf` 由原来的 `pinnedScene` 抽出。`make check-go PKG="./internal/creativescene/... ./internal/creativeagent/..."` 通过。

## 3. 天文与太阳参考

- [x] 3.1 先写 `creative-scene-astro.test.ts`：四个参考地点、三个日期的 NOAA 数据点（由 `astronomy/compare-results.json` 整理成 `frontend/scripts/fixtures/` 下的测试数据），按角距比较、只比视高度 ≥ 0 的点；日出日落相差 ≤ 2 分钟；`localToUtc` 的 20 个用例按 compatible 规则的期望值；悉尼正午太阳在北；罗盘方位换成文档方向（正北方向 180° 时正东是 +X）；手动模式直接给方向；早晚的黄金时刻（−4°…6°）和蓝调时刻（−6°…−4°）。再加 `astronomy-engine` 2.1.19（精确版本），新增 `world/astro.ts`；`creative-scene-shading.test.ts` 加墙钟时间的规则（`world/`、`render/` 里出现 `Date.now()` 或无参的 `new Date()` 时失败）。完成判据：单测先红后绿，`make check-frontend` 通过；上海夏至由前端算出的日出日落、黄金时刻和蓝调时刻写进后端测试数据，供 3.2 对照
  - 新增 `world/astro.ts`（`localToUtcMs`、`skyInstant`、`sunAt`、`documentAzimuth`、`physicalSun`、`sunTimes`）；单测 10 项全绿：时区换算 20 例与 Python zoneinfo（fold=0，即 compatible）逐一一致，四个参考地点三个日期太阳方向与 NOAA 的角距都在 0.02° 内，日出日落与 NOAA 相差不超过 1 分钟，北极圈内的极昼和极夜。测试与实现同时写成，没有单独跑出失败；上海夏至一组期望值先失败（手填的正午 11:54 有误，按经度和时差核算应为 11:56），核对边界高度后改正。
  - 调研的 `local-time.mjs` 是 `earlier` 规则（纽约 02:30 落在 01:30），改为 compatible：跳过的时刻用跳变之前的偏移。用例里 4 个「不存在的时刻」的期望值随之改写。
  - 黄金时刻、蓝调时刻与日出日落同一口径：按太阳中心的几何高度（不含折射）判定，用 `SearchAltitude`。
  - 设计挂载点之外：新增 `world/physicalSky.ts`（物理天空的缺省值、`completeSky`、`isPhysical`，不引入 `astronomy-engine`，供编辑器外壳和 `state/apply.ts` 用）；`state/apply.ts` 的 `set_environment` 按服务端的规则补齐物理天空；测试数据 `scripts/fixtures/astro/`（`noaa-sun.json`、`local-time.json`）。
  - 墙钟规则加在 `creative-scene-shading.test.ts` 的同一个检查里，`make check-frontend` 的单测会跑到；`performance.now()` 和 `new Date(时刻)` 不算。
- [x] 3.2 先写 `sun_test.go` 与 `testdata/noaa_sun.json`：同一组 NOAA 数据点（角距 ≤ 0.05°，日出日落 ≤ 1 分钟）、时间换算的 20 个用例、上海夏至各时刻与 3.1 的前端结果相差 ≤ 1 分钟、北极圈内的极昼和极夜（省略到不了的项，给出 `polar`）、`scene_azimuth_deg` 随正北方向变化；`agent_tools_test.go`：地点模式下 `read_scene` 和写了 `set_environment` 的 `edit_scene` 带 `sun`，手动模式、程序化天空、没写环境的编辑不带。再新增 `sun.go`，改 `agent.go` 的 `SceneRead`、`AgentReadInTx`、`EditResult`、`AgentEditInTx`。完成判据：测试先红后绿，`check-go` 通过
  - `sun.go` 直接按 UTC 时刻算 NOAA 的 `calcAzEl`（当地时区在真太阳时里抵消），当天各时刻逐分钟取几何高度、线性插值，正午取最高的一分钟再按抛物线细化。新增 `TestLocalTimeIsCompatible`、`TestSunMatchesNOAA`（400 多个可见点角距 ≤ 0.05°，日出日落 ≤ 1 分钟）、`TestSunReportMatchesTheFrontend`（上海夏至各时刻与前端相差 ≤ 1 分钟）、`TestSunReportAtThePlaceAndTime`、`TestSunReportAboveTheArcticCircle`、`TestSunReportOnlyForALocatedPhysicalSky`、`TestAgentReadsTheSun`，先编译失败再全绿。
  - 编辑结果的太阳参考取自这次最后一个 `set_environment` 的环境（补齐后），不再读一次快照。
  - 测试数据 `testdata/noaa_sun.json`、`local_time.json` 拷自前端的 `scripts/fixtures/astro/`；`sun_times_frontend.json` 是 3.1 前端单测钉住的上海夏至结果。

## 4. 物理大气

- [x] 4.1 先写 `creative-scene-exposure.test.ts`：晴朗、太阳 60° 时 CPU 算出的水平照度在 100–130 klux；预设改变照度的方向（雾霾更暗、高原更亮）；EV100 公式；1/3 档的快门和 ISO 表；新建摄像机的曝光换算（EV 12 普通、EV −5 快门到 30 s 靠 ISO 补足、EV 19 不可达时封顶）；色温 6500 K 为 (1, 1, 1)，2000 K 各通道不小于 0，亮度不随色温变；s_env 夹在 −12…20；米氏吸收按 (0.6, 1.2, 3.0) 不归一。再新增 `world/atmosphere/constants.ts`、`world/atmosphere/cpu.ts`（typed array，无逐步分配；星球地面取场景地面高度，摄像机至少高出 10 m）和 `world/exposure.ts` 的纯函数部分。完成判据：单测先红后绿；本机测一次 CPU 部分的耗时（换大气参数、换时刻），超出设计目标的记进任务备注，由 8.1 在参考设备上复测
  - 单测 9 项全绿：晴朗、太阳 60° 时水平照度在 100–130 klux，天空光偏蓝；雾霾更暗、高原更亮、沙尘让阳光偏黄、太阳落到地平线下后只剩天空光；新摄像机的曝光在 f/2.8 可达的 EV100 −11.9…17.9 内与默认曝光相差不超过 0.25 档（相机标的 1/3 档是取整值，0.6 s 实为 0.63 s，最近一档最多差约 0.21 档，design 原写的「不超过 1/6 档」已更正）；EV 19 时封顶在 1/8000 s、ISO 25，偏暗约 1.1 档；色温 6500 K 为 (1, 1, 1)，各色温亮度为 1、通道不小于 0。
  - CPU 耗时（本机 M4 Pro，Node）：换大气参数 22 ms，换时刻 1.1 ms，都在设计目标内。CPU 的透射率表 64 × 32、多次散射表 16 × 16，比 GPU 的 LUT 粗，只给 CPU 的积分用。
  - 米氏散射基准取原型的「示例 × 5」（散射 0.01998 /km、吸收 0.00202 /km），design 写的 0.02、0.0222 是它的约数。
  - 星球地面的天空光（Ψms × 反照率）在 CPU 和 GPU 的天空视图里都加；原型里没有这一项，4.2 出图后由 8.2 的样张检查地平线。
- [ ] 4.2 新增 `world/atmosphere/luts.ts`（透射率、多次散射、384 × 216 天空视图三个 TSL 片元 pass，RGBA16F）和 `world/atmosphere/sky.ts`（背景节点与生成天空光照用的节点），从原型 `atmoGpu.ts` 改写；`render/gpu.ts:createRenderer` 安装材质输出钳制（文件头注明 three 0.186.1）。先写 e2e「W2 atmosphere drift」：读回透射率和多次散射 LUT 的固定纹素、天空视图的几个方向，与 `cpu.ts` 比较，阈值参照穿刺的误差放宽；临时只改一边的常量，确认用例能抓到。完成判据：防漂移用例在 SwiftShader 下绿；「W2 other skies」仍然绿（钳制不改变非物理天空）

## 5. 曝光与光照

- [ ] 5.1 预曝光的管道，先不接物理天空：`world/exposure.ts` 加 `preExposure` uniform（模块单例，源码注明不得在写入和渲染之间插入 `await`）和 `applyExposure`；`objects/lights.ts:applyLight` 记 `userData.baseIntensity`；`materials/instances.ts:makeMaterial` 的自发光乘 `preExposure`；`render/sceneRenderer.ts` 的 `Frame.preExposure`，`render` 之前调用 `applyExposure`；`host/framing.ts` 的 `Framing.ev100` 与 `cameraFraming`；`state/apply.ts:cameraDefaults` 补 `iso`、`shutter_s`；`host/render.ts` 的 `shoot` 和 `Viewport.tsx:LookView` 求 k（相机视图的 EV100 单独求，不依赖只在景深预览时才算的 `framing`）。非物理天空下 k = 1，不写 `environmentIntensity`，不改雾。完成判据：「W2 other skies」逐像素仍然绿；单测覆盖 `applyExposure` 在非物理天空下不写环境强度
- [ ] 5.2 `objects/Lighting.tsx` 新增 `PhysicalLighting`：LUT 的生成，按大气键和环境键重算；太阳光（CPU 透射率 × 亮度 × 色温）、天空背景、天空光照（PMREM 200 ms 节流，记下这一份的 s_env）、雾（雾色 × E / π × k）、`scene.userData.physical`（卸载时清掉）；设备丢失时随渲染器重建。`render/sky.ts` 的 `sunDirection` 交给 `world/astro.ts`，`skyEnvironment` 不再处理物理天空。新增物理天空的参考场景（`fixtures/scene-reference/` 的院子：晴天正午、黄金时刻、日落后 20 分钟）。先写 e2e：AC-W2-3 阳光 16 法则（正午曝光正常，日落后 20 分钟测光低 5 档以上），改两级光圈亮度约变 2 档；AC-W2-4 太阳亮度 2 时线性亮度约为 2 倍，6500 K 与不设时逐像素相同，色温调低时 R/B 变大；AC-W2-5 40 cd 台灯在正午改动的像素 ≤ 0.01%，入夜后照亮的区域明显更亮；正午的雾与受光地面亮度相当；自发光白天几乎看不出；切回程序化天空后与基线逐像素相同。完成判据：用例先红后绿，「W2 other skies」仍然绿

## 6. 编辑界面

- [ ] 6.1 城市数据：`frontend/scripts/cities/` 的构建脚本与补丁表（婺源、稻城、漠河、林芝，以及 57 条缺中文名的条目；新疆的条目改为 `Asia/Shanghai`），生成 `world/cities.json`（带国家代码、不带人口，按人口排序，含 `attribution` 与取数日期）；`world/cities.ts` 按需加载与搜索（前缀先于包含，同类按行序，取前 8 项，显示「城市 · 国家」）；新建 `docs/third-party-notices.md`（GeoNames、`astronomy-engine`）。先写 `creative-scene-cities.test.ts`：「成都」「Chengdu」「稻城」排第一，搜「京都」不先出「东京都」，两条「伦敦」按国家区分，新疆的城市是 `Asia/Shanghai`。完成判据：单测先红后绿；城市表 gzip 不超过 170 KB，首屏包里没有它
- [ ] 6.2 「环境」选项卡的物理天空：`editing.ts:withSkyKind` 加 `physical`（契约缺省值，`date` 取本机的当天）；`EnvironmentPanel.tsx` 按 design 的分组排列——城市搜索框（下拉底部署名）、时区下拉与退回推算时的提示、时间一组显示太阳高度方位、日出日落、黄金时刻和蓝调时刻，环境光置灰；`SceneEditor.tsx`：切到手动模式时在同一次提交里写入当前角度（低于 −10° 时夹到 −10°），选城市一次写入四项，手改经纬度清空 `place`，选预设写入四个大气参数。先写 e2e：AC-W2-1 上海夏至拖动时刻（时刻序列存进 evidence），改正北方向后影子转动，悉尼正午太阳在北；AC-W2-2 切到手动时 `render_view` 的 `lit` 逐像素相同，Cmd+Z 回到地点模式，入夜后切手动夹到 −10°；AC-W2-12 选城市后写入经纬度和时区，下拉有署名；物理天空下环境光置灰。完成判据：用例先红后绿
- [ ] 6.3 曝光的界面：`RenderPanel.tsx` 在物理天空下显示默认曝光（EV100）和参考值，其他天空照旧显示曝光补偿；`Properties.tsx:CameraFields` 新增「曝光」一组（ISO、快门、与景深共用的光圈、只显示的 EV100），非物理天空下置灰；`editing.ts:newCamera` 和 `SceneEditor.tsx` 的添加摄像机按默认曝光写入曝光（4.1 的换算）。先写 e2e：自由视角下改默认曝光，视口变、相机视图和导出不变；默认曝光为 12 和 −5 时添加摄像机，相机视图与添加前的视口亮度相差 ≤ 1/3 档；程序化天空下曝光一组和默认曝光置灰。完成判据：用例先红后绿

## 7. 助手与文档

- [ ] 7.1 `scene_tools.go` 写入：`set_environment` 的物理天空写法（字段、单位、地点三项成组、IANA 时区名区分大小写、日期必填）；预设参数表；太阳的亮度与色温；物理天空下环境光不生效；太阳参考 `sun` 的字段、两个时段的口径和用法（按它选 `local_time`，用 `scene_azimuth_deg` 摆逆光、侧光的机位）；`set_render` 的 `exposure_ev100`；摄像机的 `iso`、`shutter_s`；两段曝光建议（物理天空给 EV100 参考值）；`render_view` 的临时视点按默认曝光。`scene_tools_test.go` 先固定关键句。`docs/dev/creative-scene-agent.md` 写物理天空、曝光和太阳参考，`docs/dev/agent-skills-tools.md` 记录说明的变更。完成判据：`make check-go PKG=./internal/creativeagent/...` 通过，说明增长的体积记进任务备注

## 8. 验收证据

- [ ] 8.1 AC-W2-8、AC-W2-9、AC-W2-11：`SCENE_E2E_GPU` 加物理天空参考场景的 WebGPU 与 `forceWebGL` 对照（含场景的画面沿用 R1 的 `lit` 阈值，只含天空的平均差 ≤ 0.5、差 > 8 的像素 ≤ 0.05%）；`SCENE_E2E_HIDDEN` 加物理天空的两次导出逐像素一致；e2e 走一遍「改时刻后 `lit` 过时、`clay` 不过时」和「只改一台摄像机的曝光」；`SCENE_E2E_PERF` 测物理天空静止时比 1.1 基线多出的帧时间、拖动时刻滑杆的帧率与天空光照跟上的时间、CPU 部分的耗时，热、冷着色器缓存分开写。完成判据：用例绿，数据存进 `discovery/research/after`；不满足的逐项列出交 owner（天空视图 384 × 216 超预算时按 design 让视口退回 192 × 108）
- [ ] 8.2 owner 看图材料：AC-W2-1 上海夏至 05:00–20:00 的时刻序列；AC-W2-7 四个预设在正午和低太阳时的图，高原低太阳平视地平线，晴朗日落太阳入画的 8K 局部；正午手动曝光与程序化天空的对照（约暗 0.4 档）；太阳在 −6°…−9° 时的样张（晨昏蒙影偏亮的已知局限）；AC-W2-10 真实模型对话的提示词与检查点（时刻是否落在太阳参考的黄金时刻里），由 owner 验收时抽查。完成判据：样例与说明存进 `discovery/research/evidence`，等 owner 结论
