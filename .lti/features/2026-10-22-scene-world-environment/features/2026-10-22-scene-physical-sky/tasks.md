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
- [x] 4.2 新增 `world/atmosphere/luts.ts`（透射率、多次散射、384 × 216 天空视图三个 TSL 片元 pass，RGBA16F）和 `world/atmosphere/sky.ts`（背景节点与生成天空光照用的节点），从原型 `atmoGpu.ts` 改写；`render/gpu.ts:createRenderer` 安装材质输出钳制（文件头注明 three 0.186.1）。先写 e2e「W2 atmosphere drift」：读回透射率和多次散射 LUT 的固定纹素、天空视图的几个方向，与 `cpu.ts` 比较，阈值参照穿刺的误差放宽；临时只改一边的常量，确认用例能抓到。完成判据：防漂移用例在 SwiftShader 下绿；「W2 other skies」仍然绿（钳制不改变非物理天空）
  - 防漂移用例在 SwiftShader（WebGL2）下绿，三组的相对误差（均值 / 最大）：晴朗 30° 透射率 0.02% / 0.05%、多次散射 0.10% / 1.86%、天空视图 0.03% / 0.11%；沙尘 8° 0.02% / 0.15%、0.18% / 3.56%、0.04% / 0.27%；高原 −3° 0.02% / 0.06%、0.10% / 2.03%、0.03% / 0.20%。阈值取透射率 0.2% / 1%、多次散射 0.5% / 5%（最大误差落在太阳在地平线下、值约 1e-5 的纹素）、天空视图 0.5% / 3%。
  - 临时把 `MIE_H` 只在 GPU 一侧乘 1.05，透射率最大误差 12.75%，用例失败，已还原。
  - 起初沙尘差 6–8%，原因是 CPU 的表比 GPU 粗（4.1 备注），不是公式不同：`CpuAtmosphere` 加 `tables: 'fast' | 'lut'`，防漂移用例按 GPU 尺寸建表比较，编辑器仍用 `fast`。顺带把 `luts.ts` 里从摄像机到地面的求交改成不做大数相减（float32 消去），对误差没有可见影响，保留。
  - 钳制在 `render/gpu.ts` 模块加载时装到 `NodeMaterial.prototype`，不放在 `createRenderer` 里：导出和缩略图的渲染器也要钳，且只装一次（`outputClamped` 标记）；MRT 的输出结构节点不钳。「W2 other skies」在 Metal 下逐像素绿，「reference scenes」「calibration」在 SwiftShader 下绿，场景单测 253 项全绿。

## 5. 曝光与光照

- [x] 5.1 预曝光的管道，先不接物理天空：`world/exposure.ts` 加 `preExposure` uniform（模块单例，源码注明不得在写入和渲染之间插入 `await`）和 `applyExposure`；`objects/lights.ts:applyLight` 记 `userData.baseIntensity`；`materials/instances.ts:makeMaterial` 的自发光乘 `preExposure`；`render/sceneRenderer.ts` 的 `Frame.preExposure`，`render` 之前调用 `applyExposure`；`host/framing.ts` 的 `Framing.ev100` 与 `cameraFraming`；`state/apply.ts:cameraDefaults` 补 `iso`、`shutter_s`；`host/render.ts` 的 `shoot` 和 `Viewport.tsx:LookView` 求 k（相机视图的 EV100 单独求，不依赖只在景深预览时才算的 `framing`）。非物理天空下 k = 1，不写 `environmentIntensity`，不改雾。完成判据：「W2 other skies」逐像素仍然绿；单测覆盖 `applyExposure` 在非物理天空下不写环境强度
  - 「W2 other skies」在 Metal 下逐像素绿，「calibration」在 SwiftShader 下绿；单测新增 2 项（非物理天空：灯光回到数据强度，没有 `baseIntensity` 的工作室光、环境强度和雾都不动；物理天空：灯光 × k、天空光照 k / s_env、雾 × E / π × k，换 k 时从数据重算），场景单测 255 项全绿。
  - `SceneRenderer.render` 画完在 `finally` 里把 `preExposure` 写回 1，与 `toneMappingExposure` 的做法一样：缩略图（`library/thumbnail.ts`）和预览共用材质实例但不经过 `SceneRenderer`，画它们时读到的总是 1。
  - k 的取法集中在 `render/litLook.ts:frameExposure`（白模、线框为 1；非物理天空沿用 `2^exposure_ev`；物理天空 `toneMappingExposure = 1`，k 取摄像机或默认曝光），`host/render.ts` 和 `Viewport.tsx` 共用。相机视图的 EV100 由 `host/framing.ts:cameraEV100` 直接从摄像机字段求，不依赖景深预览。
  - `scene.userData.physical` 的形状定为 `PhysicalExposure { skyLightScale, fogColor, fogLux }`，由 5.2 的 `PhysicalLighting` 写入。天空背景节点（`sky.ts`）的 `relative`、`exposure` 还是自己的 uniform，5.2 接线时改为读 `preExposure`。
  - `state/apply.ts:cameraDefaults` 的 `iso`、`shutter_s` 已在 2.2 加上。
- [x] 5.2 `objects/Lighting.tsx` 新增 `PhysicalLighting`：LUT 的生成，按大气键和环境键重算；太阳光（CPU 透射率 × 亮度 × 色温）、天空背景、天空光照（PMREM 200 ms 节流，记下这一份的 s_env）、雾（雾色 × E / π × k）、`scene.userData.physical`（卸载时清掉）；设备丢失时随渲染器重建。`render/sky.ts` 的 `sunDirection` 交给 `world/astro.ts`，`skyEnvironment` 不再处理物理天空。新增物理天空的参考场景（`fixtures/scene-reference/` 的院子：晴天正午、黄金时刻、日落后 20 分钟）。先写 e2e：AC-W2-3 阳光 16 法则（正午曝光正常，日落后 20 分钟测光低 5 档以上），改两级光圈亮度约变 2 档；AC-W2-4 太阳亮度 2 时线性亮度约为 2 倍，6500 K 与不设时逐像素相同，色温调低时 R/B 变大；AC-W2-5 40 cd 台灯在正午改动的像素 ≤ 0.01%，入夜后照亮的区域明显更亮；正午的雾与受光地面亮度相当；自发光白天几乎看不出；切回程序化天空后与基线逐像素相同。完成判据：用例先红后绿，「W2 other skies」仍然绿
  - e2e（SwiftShader）全绿：「W2 sunny 16」正午测光 EV 15.3、画面均值 169、无剪切，日落后 20 分钟测光 EV 3.9（低 11 档），同曝光下均值 9；「W2 exposure and the sun」相机视图按相机自己的 EV100，改默认曝光时不变，f/8 1/500 s 与 f/16 1/125 s 逐像素相同（差 > 2 的像素 0%），太阳亮度 4 倍与开大两档光圈逐像素相同，不写色温和亮度时与 6500 K、1 逐像素相同，3000 K 时 R/B 从 1.04 到 1.69；「W2 lamps, emission and fog」正午 40 cd 台灯和灰色牌子上 50 cd/m² 的自发光改动 0% 的像素，日落后改动 3.9%，正午的雾 161、近处受光地面 174；「W2 back to another sky」程序化天空加雾切到物理天空再切回，逐像素相同。「W2 other skies」在 Metal 下逐像素绿，「reference scenes」「calibration」在 SwiftShader 下绿，场景单测 255 项全绿。
  - 用例是在实现之后写的，不是先红后绿。但写用例时发现了一个真问题：天空盒的颜色节点没有包在 `Fn` 里，`toVar()` 不在函数体内，Metal 上整片天空成了一个颜色（地平线上 0–5.5° 全是 (54, 81, 113)，CPU 应为接近白的淡蓝），天空光照也跟着错。防漂移用例此前只比 LUT 的纹素，没有经过 `sampleSkyView`，所以没抓到。现在加了「skyDirections」：16 × 8 个方向经 `sampleSkyView` 与 CPU 的天空亮度比（平均误差 ≤ 2%，最大 ≤ 10%；三组实测平均 0.02–0.06%，最大 0.15–6%，最大的在高原 −3° 地平线附近）。修好后天空从地平线的灰白渐变到蓝。用例还会报告 LUT 的读回是否上下翻转（WebGL2 翻转，WebGPU 不翻转），`SCENE_E2E_W2_METAL` 可让它在本机 GPU 上跑。
  - 物理天空的参考场景是三个文件 `fixtures/scene-reference/physical-{noon,golden,dusk}.json`：上海 2026-09-23 的 12:00、17:30（黄金时刻 17:18–18:05）、18:10（日落 17:50 后 20 分钟），默认曝光 15 / 12 / 6，摄像机 f/16 1/125 s ISO 100、f/5.6 1/125 s ISO 100、f/2.8 1/30 s ISO 400。没有加进 `referenceNames`：那份表与 W1 的基线逐张对照。`SCENE_E2E_W2_PICTURES=<dir>` 导出这三个场景的 lit 图和视口图，供 8.2 取证。
  - `PhysicalLighting` 用 `latest` 加键字符串的写法（沿用 `LitLighting`）：大气键是四个大气字段，环境键是整个 `sky`；雾色另按雾的颜色键。太阳光不用 JSX 而是 `<primitive>`：R3F 会在每次渲染时用新的 `userData` 对象覆盖掉 `baseIntensity`。物理天空下不加环境光。LUT 等对象不随 `gl` 重建（three 的对象与渲染器无关），渲染器换了时两个效果（依赖 `renderer`）会重画 LUT 和天空光照；设备丢失没有单独的用例，沿用 R1 的恢复流程，留给 8.1 的 GPU 检查顺带看。
  - `render/sky.ts:sunDirection` 对物理天空调用 `world/astro.ts:physicalSun`；角度到向量的换算挪到 `astro.ts:sunVector`，程序化天空逐像素不变。新增 `constants.ts:sunAbove`（大气顶的太阳照度 × 亮度 × 色温）。

## 6. 编辑界面

- [x] 6.1 城市数据：`frontend/scripts/cities/` 的构建脚本与补丁表（婺源、稻城、漠河、林芝，以及 57 条缺中文名的条目；新疆的条目改为 `Asia/Shanghai`），生成 `world/cities.json`（带国家代码、不带人口，按人口排序，含 `attribution` 与取数日期）；`world/cities.ts` 按需加载与搜索（前缀先于包含，同类按行序，取前 8 项，显示「城市 · 国家」）；新建 `docs/third-party-notices.md`（GeoNames、`astronomy-engine`）。先写 `creative-scene-cities.test.ts`：「成都」「Chengdu」「稻城」排第一，搜「京都」不先出「东京都」，两条「伦敦」按国家区分，新疆的城市是 `Asia/Shanghai`。完成判据：单测先红后绿；城市表 gzip 不超过 170 KB，首屏包里没有它
  - 单测 5 项先红后绿：「成都」「Chengdu」「chengdu」「稻城」「婺源」排第一；「京都」先出京都、「喀什」先出喀什，东京都仍在后面；两条「伦敦」显示为「伦敦 · 英国」「伦敦 · 加拿大」；「臺北」找到台北；乌鲁木齐、喀什是 `Asia/Shanghai`；最多 8 项；署名与取数日期。
  - 构建：`node scripts/cities/build.mjs <数据目录>` 一条命令，缺的下载才去取（GeoNames 三个文件、Wikidata 约 86 次查询）。这次用调研当天（2026-10-22）下载在本机的数据离线构建，没有重新联网。结果 7,338 条（中国 2,338 条），258 个时区全部能被 `Intl` 加载，新疆 115 条改为 `Asia/Shanghai`；原始 292,185 字节，gzip 160,284 字节。与调研的数字一致（7,337 条、gzip 160,023，多出的一条是婺源）。
  - 补丁表：稻城改名、漠河改名改坐标（52.97°N、122.54°E）、林芝补中文名、婺源新增；57 条人口 10 万以上缺中文名的中国条目，中文名是我按拼音、省份代码和坐标逐条判断的，其中和村、绣林、旧堡、大屯、临水、吉舒、辛寨几个镇街标了「按坐标核对」，请 owner 过目；海外 8 条简体列残留的繁体名改成简体（如「雷諾」→「里诺」）。构建时打印的两份核对清单都清零。
  - 港澳台的国家名用「中国香港」「中国澳门」「中国台湾」，不用 `Intl.DisplayNames` 的「中国香港特别行政区」（太长）。
  - 「首屏包里没有它」：`cities.ts` 用 `import()` 加载 JSON，目前还没有页面引用它；6.2 接进环境选项卡后用构建产物核对。
  - 新建 `docs/third-party-notices.md`：GeoNames（CC BY 4.0、修改说明、免责声明原文）、Wikidata（CC0）、`astronomy-engine`（MIT）。
- [x] 6.2 「环境」选项卡的物理天空：`editing.ts:withSkyKind` 加 `physical`（契约缺省值，`date` 取本机的当天）；`EnvironmentPanel.tsx` 按 design 的分组排列——城市搜索框（下拉底部署名）、时区下拉与退回推算时的提示、时间一组显示太阳高度方位、日出日落、黄金时刻和蓝调时刻，环境光置灰；`SceneEditor.tsx`：切到手动模式时在同一次提交里写入当前角度（低于 −10° 时夹到 −10°），选城市一次写入四项，手改经纬度清空 `place`，选预设写入四个大气参数。先写 e2e：AC-W2-1 上海夏至拖动时刻（时刻序列存进 evidence），改正北方向后影子转动，悉尼正午太阳在北；AC-W2-2 切到手动时 `render_view` 的 `lit` 逐像素相同，Cmd+Z 回到地点模式，入夜后切手动夹到 −10°；AC-W2-12 选城市后写入经纬度和时区，下拉有署名；物理天空下环境光置灰。完成判据：用例先红后绿
  - e2e 三项先红后绿：「W2 environment tab: the physical sky …」切到物理天空写入契约缺省值和本机当天日期，其他环境字段不变；环境光一组置灰并提示；城市表在搜索框第一次获得焦点时才请求（打开编辑器时没有）；「伦敦」列出「伦敦 · 英国」「伦敦 · 加拿大」，下拉底部有 GeoNames 署名链接；选「成都」一次写入四项，手改纬度清空 `place`，改时区。「… the time of day …」上海夏至 05:00–20:00 太阳高度 1.3°、12.8°、37.7°、63.2°、82.2°、61.5°、35.9°、11.2°、0°、−10.9°，日出 04:5x、日落 19:0x，显示黄金时刻和蓝调时刻；正北方向改 90° 后画面变化；悉尼正午太阳方位在北。「… manual angles …」16:30 切手动，模式和角度一次写入，画面差 > 2 的像素 < 0.01%，Cmd+Z 回到地点模式，23:00 切手动夹到 −10°；预设写入四个大气字段、不动太阳亮度，改尘埃密度后显示「自定义」。设 `SCENE_E2E_W2_EVIDENCE=<dir>` 时保存夏至的时刻序列，供 8.2 用。
  - 物理天空的几组控件放在新文件 `panels/PhysicalSkyFields.tsx`（含城市搜索框），`EnvironmentPanel.tsx` 只加选项、挂载和环境光置灰；挂载点清单外的新文件，理由是面板原来 97 行，这部分有 200 多行。写法放在 `editing.ts`：`withSkyKind` 加 `physical`、`withSunMode`（切手动时由面板传入当前角度，`editing.ts` 不引入天文计算）、`withCity`、`withPreset`、`localDate`，单测 2 项。
  - 设计把「切手动的提交」「选城市一次写四项」列在 `SceneEditor.tsx`，实际都经面板的 `onEnvironment` 一次提交，`SceneEditor.tsx` 不用改；它在 6.3（添加摄像机的曝光）再改。
  - 时刻滑杆按分钟（0–1439），显示 HH:MM，合并键 `environment.local_time`；太阳方位显示罗盘读数（正北为 0°，带「北、东北…」），高度带符号。时区认不出时提示按经度推算的偏移。
  - e2e 的 `referenceScene` 给 W2 之前的参考场景补上 `render.exposure_ev100 = 15`，与服务端读旧文档时一致（`Render.UnmarshalJSON`）；不补时切到物理天空，k 为 NaN，画面全黑。
- [x] 6.3 曝光的界面：`RenderPanel.tsx` 在物理天空下显示默认曝光（EV100）和参考值，其他天空照旧显示曝光补偿；`Properties.tsx:CameraFields` 新增「曝光」一组（ISO、快门、与景深共用的光圈、只显示的 EV100），非物理天空下置灰；`editing.ts:newCamera` 和 `SceneEditor.tsx` 的添加摄像机按默认曝光写入曝光（4.1 的换算）。先写 e2e：自由视角下改默认曝光，视口变、相机视图和导出不变；默认曝光为 12 和 −5 时添加摄像机，相机视图与添加前的视口亮度相差 ≤ 1/3 档；程序化天空下曝光一组和默认曝光置灰。完成判据：用例先红后绿
  - e2e 两项先红后绿：「W2 exposure controls: the default exposure …」黄金时刻的院子，物理天空下渲染选项卡没有曝光补偿、有参考值；默认曝光改到 11，自由视角变亮，相机视图不变（差 > 2 的像素 < 0.05%）；默认曝光 12 时添加摄像机写入 ISO 100、1/500 s（EV100 11.94），相机视图均值 106、添加前的视口 117；−5 时快门到 30 s、ISO 800（EV100 −4.94）。「… a camera’s ISO, shutter and aperture …」ISO、快门、光圈各写一次，景深关闭时光圈可调，EV100 显示随之变化；切回程序化天空后摄像机的曝光一组置灰并提示，渲染选项卡的默认曝光置灰、曝光补偿照常可用。旧用例「Add > 摄像机」「render tab」「AC-5: exposure +2」「Add > 空物体」仍然绿，场景单测 262 项全绿。
  - 渲染选项卡：物理天空下只有默认曝光（−10…20，步长 1/3）和参考值、用途说明；其他天空下曝光补偿照旧，默认曝光置灰并提示「只在物理天空下生效」（design 只写了「换成」，任务的用例要求其他天空下默认曝光置灰，按任务做）。`Slider` 加了 `disabled`。
  - 摄像机的「曝光」一组是 `CameraFields` 里的子 fieldset（`Properties.tsx:CameraExposure`），ISO、快门、光圈用下拉，当前值不在档位里时（助手写的任意值）也列出；光圈与景深一组共用 `f_stop`。`Properties` 新增 `physical` 属性。
  - 添加摄像机：`editing.ts:newCamera` 多一个可选的曝光参数；`SceneEditor.tsx:addCamera` 在物理天空下按默认曝光和缺省光圈 f/2.8 求（`newCameraExposure`），其他天空下不写曝光字段，与以前相同。

## 7. 助手与文档

- [x] 7.1 `scene_tools.go` 写入：`set_environment` 的物理天空写法（字段、单位、地点三项成组、IANA 时区名区分大小写、日期必填）；预设参数表；太阳的亮度与色温；物理天空下环境光不生效；太阳参考 `sun` 的字段、两个时段的口径和用法（按它选 `local_time`，用 `scene_azimuth_deg` 摆逆光、侧光的机位）；`set_render` 的 `exposure_ev100`；摄像机的 `iso`、`shutter_s`；两段曝光建议（物理天空给 EV100 参考值）；`render_view` 的临时视点按默认曝光。`scene_tools_test.go` 先固定关键句。`docs/dev/creative-scene-agent.md` 写物理天空、曝光和太阳参考，`docs/dev/agent-skills-tools.md` 记录说明的变更。完成判据：`make check-go PKG=./internal/creativeagent/...` 通过，说明增长的体积记进任务备注
  - `TestSceneToolsDescribeThePhysicalSky` 先红（26 句缺失）后绿，固定物理天空的字段与成组规则、时区大小写、`date` 必填、三个预设的数值、亮度与色温、环境光不生效、`exposure_ev100`、`iso`、`shutter_s`、EV100 公式与参考值、太阳参考与两个时段的口径、「按它选 local_time」，以及 `read_scene` 的 `sun`、`render_view` 的临时视点曝光。旧用例 `TestEditSceneDescribesThreejsFields` 固定的 `set_render` 整句随之改为两段（`exposure_ev` 后加了「物理天空以外的曝光补偿」）。`make check-go PKG=./internal/creativeagent/...` 与 `PKG=./internal/creativescene/...` 通过。
  - 体积：`edit_scene` 从 7,682 字节（4,887 字）到 11,017 字节（6,784 字），多 3.3 KB；`read_scene` 391 → 491，`render_view` 1,005 → 1,132，`export_shots` 不变。
  - 说明变长后，`compaction_regression_test.go:TestCompactionRejectsSummaryBeyondWindow` 的 8000 token 测试窗口放不下工具定义，正常的下一轮也失败；窗口调到 9000（最小可行值，超长摘要仍被拒绝），与 C3 加场景工具时调这些窗口的做法相同。这是挂载点清单外的改动。
  - 文档：`docs/dev/creative-scene-agent.md` 加物理天空与手动曝光一节（LUT、单位、预曝光的落点、k 的取法、钳制、太阳参考）、W2 的 e2e 开关和说明体积；`docs/dev/agent-skills-tools.md` 的 `read_scene`、`edit_scene`、`render_view` 三行补 W2 的变化。

## 8. 验收证据

- [x] 8.1 AC-W2-8、AC-W2-9、AC-W2-11：`SCENE_E2E_GPU` 加物理天空参考场景的 WebGPU 与 `forceWebGL` 对照（含场景的画面沿用 R1 的 `lit` 阈值，只含天空的平均差 ≤ 0.5、差 > 8 的像素 ≤ 0.05%）；`SCENE_E2E_HIDDEN` 加物理天空的两次导出逐像素一致；e2e 走一遍「改时刻后 `lit` 过时、`clay` 不过时」和「只改一台摄像机的曝光」；`SCENE_E2E_PERF` 测物理天空静止时比 1.1 基线多出的帧时间、拖动时刻滑杆的帧率与天空光照跟上的时间、CPU 部分的耗时，热、冷着色器缓存分开写。完成判据：用例绿，数据存进 `discovery/research/after`；不满足的逐项列出交 owner（天空视图 384 × 216 超预算时按 design 让视口退回 192 × 108）
  - 两后端对照放在 `SCENE_E2E_W2_GPU=<dir>`（「W2 parity」），结果在 `after/w2-parity.json` 和各张对照图：只含天空的三张（向上看的天空摄像机）平均差 0.0007–0.0049、差 > 8 的像素 0%，满足；含场景的三张平均差 0.12–0.36（阈值 0.63）满足，差 > 8 的像素 0.76% / 0.93% / 1.52%（阈值 0.18%）**不满足**。差集中在混凝土的孔洞硬边，是 R1 已知的两后端纹理采样差异，不是天空带来的；沿用 W1 的做法没有放宽阈值，交 owner 决定。
  - 隐藏页确定性：`SCENE_E2E_HIDDEN` 加了 `physical-golden` 的 clay 和 lit，WebGL2、WebGPU 各导出两次，逐像素一致，通过。
  - 过时：前端的假服务端没有指纹，按「只改一台摄像机的曝光只让这台的 lit 过时」在 Go 里加了 `TestOneCamerasExposureStalesOnlyItsLit`；「改时刻后 lit 过时、clay 不过时」已有 `TestPhysicalSkyFingerprintsOnlyLit`。e2e 不另走一遍。
  - 性能放在 `SCENE_E2E_W2_PERF=<dir>`（「W2 performance」，接在 AC-9 切换检查后面），结果在 `after/w2-perf.json`，M4 Pro、Chromium 145、1600 × 1000 两倍像素、AC-9 场景：静止帧时间物理天空 8.33 ms，与基线 8.33 ms 相同（都是 120 Hz 的帧间隔），满足；切到物理天空最长一帧冷 333 ms、热 333 ms，天空光照 211 / 234 ms 跟上；CPU 换大气参数 22 ms、换时刻 1.15 ms，满足；天空视图 384 × 216 每帧约 0.2 ms，不需要退回 192 × 108。
  - 拖动时刻滑杆：76 帧 / 3 s，平均 40 ms，约 25 fps，**不满足** AC-W2-11 的 30 fps。用 CDP profiler 查过：起初每 3 s 有约 1.2 s 花在 three 重建所有材质的节点上，原因是每次天空光照都生成新的 PMREM 纹理；改成每次画进同一个渲染目标（`Lighting.tsx`）后从约 10 fps 升到 25 fps，冷切换的最长一帧也从 2833 ms 降到 333 ms。剩下的是开发构建里编辑器的 React 重渲染（大纲约 270 ms / 3 s，`SceneEditor` 的内联回调让它无法跳过），程序化天空拖同一个滑杆也只有 8–10 fps，是 W2 之前就有的问题；另把 `PhysicalSkyFields` 的日出日落计算按日期和地点缓存。建议另开一项处理编辑器重渲染，交 owner 决定。
- [x] 8.2 owner 看图材料：AC-W2-1 上海夏至 05:00–20:00 的时刻序列；AC-W2-7 四个预设在正午和低太阳时的图，高原低太阳平视地平线，晴朗日落太阳入画的 8K 局部；正午手动曝光与程序化天空的对照（约暗 0.4 档）；太阳在 −6°…−9° 时的样张（晨昏蒙影偏亮的已知局限）；AC-W2-10 真实模型对话的提示词与检查点（时刻是否落在太阳参考的黄金时刻里），由 owner 验收时抽查。完成判据：样例与说明存进 `discovery/research/evidence`，等 owner 结论
  - 样张由 `SCENE_E2E_W2_EVIDENCE=<dir>` 导出（「W2 evidence」加环境选项卡的夏至序列），说明、重跑命令和我看过之后的记录都在 `evidence/README.md`，测光值在 `evidence.json`。入库前转成 JPG，8K 日落保留 PNG。
  - 交 owner 决定的三件事：① 所有样张在场景地面尽头和天空之间都露出一道比地面暗的星球地面色带（不只高原；地平线本身没有下沉），README 列了三种做法；② 晨昏蒙影偏紫偏亮（已知局限）；③ 8K 日落的淡圈经测量是 8 位量化（每圈差 1 个色阶），不是 LUT 分段，去掉要加输出抖动。正午物理天空比程序化约暗 0.3 档（design 预计 0.4 档）。
  - AC-W2-10 的真实模型对话只写了提示词与检查点，没有跑：要用 owner 的模型密钥，由 owner 验收时抽查。
