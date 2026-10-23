# Tasks

改代码前先截取白天与手动模式的基线，再按「数据 → 天文 → 资源与银河 → 天空与光照 → 编辑界面 → 助手 → 验收证据」推进。证据存进本子项的 `discovery/research/<主题>`。每个任务结束时 `tsc -b`、`oxlint`、相关单测和 `check-go` 都通过；e2e 只按 `SCENE_E2E_ONLY=W3` 跑受影响的用例，端口 5293 避开 owner 的 Vite。

## 1. 基线

- [x] 1.1 在实施分支第一次改代码之前（与 `develop` 相同）截取 AC-W3-6 的基线，存进 `discovery/research/baseline`：物理天空正午（月亮不入画）的 `lit` 导出与最终渲染视口；同一场景切到手动模式、太阳 −8° 的 `lit` 导出；R1 性能场景在物理天空入夜（W2 的黑夜）下的视口帧时间与拖动时刻的帧率（AC-W3-10 对照用）。e2e 新增「W3 day unchanged」用例，与基线逐像素比较（差 > 2 的像素为 0）。完成判据：用例在未改动的代码上绿，`baseline.md` 写明场景、机位、设备与测量值
  - 结果见 `discovery/research/baseline.md`：`physical-noon`（月亮在地平线下 49°）与 `physical-manual`（太阳 −8°）两组，「W3 day unchanged」在未改动的代码上通过；入夜静止 8.3 ms，拖动约 26 fps（与 W2 的 25 fps 一致）。性能用例另写成「W3 performance」（`SCENE_E2E_W3_PERF`），8.2 复用。

## 2. 数据契约与校验

- [x] 2.1 先写 `scene_test.go`：物理天空只写 `night` 的一部分时逐项补齐；W2 时期没有 `night` 的快照读取后补齐为缺省值；返回 400 的写法——程序化、HDRI、纯色天空带 `night`，四个数值越界，`night` 里写未知字段；删去「night 在 W3 前返回 400」的用例，`aerial_perspective`、`clouds`、`environment.weather` 仍返回 400。再改 `model.go:Sky`（`Night`）、`sky.go`（`physicalOnly`、`completeSky`、`validatePhysicalSky`）；`api/openapi.yaml` 的 `CreativeSceneSky` 加 `night`，运行 `make generate`。完成判据：测试先红后绿，`make check-go PKG=./internal/creativescene/...` 通过，生成物已更新
  - `TestPhysicalSkyIsCompleted` 加缺省与逐项补齐，`TestPhysicalSkyRefusesBadFields` 加四个越界与 `night` 里的未知字段（报错写 `night.moon_strength` 等字段名），`TestOtherSkiesRefusePhysicalFields` 加程序化天空带 `night`；先红（`没有字段 night`）后绿。`Night` 用指针字段，与 `Sky` 其他字段一致；未知字段沿用通用的形状检查。旧快照解码与写入都经 `completeSky`，同一处补齐。`make check-go PKG=./internal/creativescene/...` 通过（lint 0 issues）；`api.gen.go`、`schema.d.ts` 已更新，`tool_schemas.gen.go` 无变化。
- [x] 2.2 先改 `fingerprint_internal_test.go`：改夜空任一字段，`lit` 指纹变、`clay` 与 `line` 不变；W2 时期的物理天空快照补齐 `night` 后 `lit` 指纹变化（预期的一次过时）；程序化与纯色天空的旧指纹不变。完成判据：测试绿，`fingerprint.go` 不需改动（如需改动，写明原因）
  - `TestPhysicalSkyFingerprintsOnlyLit` 加七个夜空字段；新增 `TestThePhysicalSkysNightStalesItsLitOnce`：W2 时期的快照没有 `night`（`omitempty`，哈希输入里没有这个键），补齐后 `lit` 变、`clay`/`line` 不变。程序化天空的固定指纹（`pinned`）不变。`fingerprint.go` 未改。
- [x] 2.3 前端 `world/physicalSky.ts`：`physicalSkyDefaults` 加 `night` 缺省值，`completeSky` 逐项补齐，`isPhysical` 要求 `night` 已补齐；`state/apply.ts` 的 `set_environment` 跟着补齐。完成判据：`creative-scene.test.ts` 加补齐用例并通过，`tsc -b` 通过
  - `PhysicalSky` 的 `night` 为完整的 `NightSky`，新增 `nightSkyDefaults`；补齐用例加在已有的「物理天空按服务端规则补齐」单测里（`creative-scene-astro.test.ts`，而非 `creative-scene.test.ts`），与实现同时写成。`state/apply.ts` 经 `completeSky` 自动补齐，未改。`tsc -b` 通过。

## 3. 天文与月亮参考

- [x] 3.1 先从 JPL Horizons 取四个参考地点在满月、上弦、新月各一天的月亮视方向、照亮比例和地心距离，以及北极星、天狼星、织女星、南十字座 α 的地平坐标，整理成 `frontend/scripts/fixtures/astro/` 下的测试数据（记下查询参数与日期）。再写 `creative-scene-astro.test.ts` 的月亮与亮星用例（角距 ≤ 0.2°，照亮比例 ≤ 0.02，北京的北极星高度与纬度差 ≤ 1°），然后在 `world/astro.ts` 加 `physicalMoon`（方向、视星等、视直径、自转轴）、`moonTimes`（月相名称与照亮比例、月出月落、前后最近的满月和新月）、`equatorToScene`、`galacticToScene`。完成判据：单测先红后绿；前端算出的北京、上海一个月的月相时刻与月出月落写进后端测试数据，供 3.2 对照
  - 测试数据：`fixtures/astro/horizons-moon.json`（JPL Horizons API，四个参考地点 × 2026-10-10 新月、10-18 上弦、10-26 满月，逐小时；生成脚本 `scripts/night-sky/horizons-moon.mjs`）；`fixtures/astro/skyfield-stars.json`（Skyfield 1.55 + DE421，BSC5 的北极星、天狼星、织女星、十字架二在北京、上海、悉尼共 5 个时刻；脚本 `skyfield_stars.py`）。Horizons 输出里日月标记为空时列会错位，脚本改为取每行最后五个数。
  - `world/astro.ts` 新增 `moonAt`、`physicalMoon`（含月球北极在场景中的方向）、`equatorToScene`、`galacticToScene`、`moonPhaseName`、`moonTimes`。单测 5 项：月亮对 Horizons 角距 ≤ 0.2°（视高度 ≥ 0 的点）、照亮比例 ≤ 0.02、视直径相对误差 1.3 × 10⁻⁴；亮星对 Skyfield ≤ 0.2°，北京的北极星高度与纬度差 ≤ 1°、悉尼的在地平线下；银心经两条矩阵落在同一方向（≤ 0.05°）；月相、月出月落与前后的满月新月。实现先于测试写成，没有单独跑出失败。
  - 后端对照数据 `backend/internal/creativescene/testdata/moon_frontend.json`：北京、悉尼 2026 年 10 月每天 22:00（脚本 `scripts/night-sky/moon-times-frontend.ts`）。月相按日期对照，不按时刻。
- [x] 3.2 先写 `moon_test.go`：与 3.1 的前端数据对照（月相时刻 ≤ 10 分钟，月出月落 ≤ 3 分钟，照亮比例 ≤ 0.02）、月亮不升起或不落下的日子、`scene_azimuth_deg` 随正北方向变化；`agent_tools_test.go`：地点模式下 `read_scene` 和写了 `set_environment` 的 `edit_scene` 带 `moon`，手动模式、其他天空、没写环境的编辑不带，`night.moon` 关闭时照样带。再新增 `moon.go`（移植 Meeus 第 47、49 章），改 `agent.go` 的 `SceneRead`、`EditResult` 和编辑结果的取值处。完成判据：测试先红后绿，`check-go` 通过
  - `moon.go`：月亮位置用 Meeus 第 47 章（表 47.A/47.B 各 60 项，系数取自 soniakeys/meeus 的转录，MIT，写进第三方声明）；以 UT 代替力学时；视差与 NOAA 的折射公式。满月、新月的日期不移植第 49 章，改为每 6 小时步进找黄经差过 180°/0° 再二分到秒：少一套表，精度由位置决定。与前端对照：北京、悉尼 2026 年 10 月每天 22:00，位置最大差 0.071°，月出月落最大差 1 分钟，月相名称、照亮比例和前后满月新月的日期全部一致；新月、满月时刻与 astronomy-engine 相差在 1 分钟内。
  - `agent.go`：`SceneRead`、`EditResult` 加 `moon`；`writtenSun` 改为 `writtenSky`，太阳和月亮参考取自同一份补齐后的天空。`TestAgentReadsTheSun` 加月亮参考的出现条件（`night.moon` 关闭时照样带）。
  - 测试与实现同时写成，没有单独跑出失败。`TestAgent` 有一次在 62 秒后失败（未留下失败用例名），之后连续三次 `-count=1` 都通过，疑为共享测试库的偶发问题，留意后续是否复现。golangci-lint 0 issues。

## 4. 资源与银河

- [x] 4.1 `frontend/scripts/night-sky/`：从 BSC5 生成 `stars.bin`（V ≤ 6.5，按列打包，缺 B−V 取 0.6），从 NASA SVS CGI Moon Kit 生成 1024 × 512 灰度 `moon.webp`（均值归一），脚本里写下载地址与校验和，产物文件名带内容哈希。新增 `world/nightSky/assets.ts`（按需加载与缓存、`loadNightAssets(scene)`：不需要时立即返回，失败时 reject）和 `world/nightSky/light.ts`（星等→照度、B−V→色调、夜天光与银河的标定、星空常数项）。先写 `creative-scene-night-sky.test.ts`：天狼星照度、满月视星等对应 0.2–0.3 lux、B−V 0.65 的色调接近太阳、色温 6500 时色调为 (1, 1, 1)、白天或手动模式不需要资源、加载失败时 reject。完成判据：单测先红后绿；`stars.bin` 恰为 8404 颗，体积与调研相符
  - `stars.bin` 8404 颗、84 048 B，gzip 后 67.5 KB；`moon.webp` 1024 × 512 灰度 68 KB（调研估 49 KB，差在把线性均值缩放到 0.5 之后的细节；原图线性均值 0.507，SVS 的图偏亮，缩放 0.986，没有截断）。脚本校验原始文件的 sha256，依赖 libvips 与 cwebp。
  - 资源 URL 用 `new URL('./stars.bin', import.meta.url)`，Vite 构建时会给文件名加哈希；Node 单测通过可替换的 `nightAssetLoaders` 注入。
  - 偏差（回写 design）：星等换照度不用 −14.18 这个常数，改为相对太阳：E = SUN_LUX × 10^(−0.4 (m + 26.74))，与渲染端太阳的 128 klux 一致；满月 −12.7 等为 0.31 lux（原常数下为 0.26 lux）。星星的颜色以太阳（B−V 0.65）为白，与太阳光在大气顶为白一致；月面反照的色调取 B−V 0.92。
  - 实测：夜天光的水平照度 8.9 × 10⁻⁴ lux，星表全部亮星 4.9 × 10⁻⁵ lux（实际星光的大部分来自更暗的星，这里只有亮星）。
  - 单测 6 项全绿，与实现同时写成，没有单独跑出失败。
- [ ] 4.2 `world/nightSky/milkyWay.ts`：按 design「程序化银河」的模型写 TSL 烘焙 pass（2048 × 1024 RGBA16F，固定种子，整数哈希噪声），以及 CPU 上同一模型的粗网格积分（供常数项）。单测：CPU 模型的银心方向最亮、银纬 30° 以外接近 0、大暗隙处比两侧暗、麦哲伦云在南天。完成判据：单测通过；e2e 导出一张银河的等距柱状图存进 `discovery/research/milky-way/`，与公开照片并排供 owner 看；烘焙耗时记进备注

## 5. 天空与光照

- [ ] 5.1 `world/atmosphere/luts.ts`：天空视图 pass 抽成按光源参数渲染到指定目标，加月亮的一组（`setMoon`、`renderMoonSkyView`、`sampleMoonSkyView`）；`world/atmosphere/sky.ts:physicalSkyColor` 加月盘（球面着色、反照率贴图、视直径、自转轴）、月亮天空、银河和夜天光，背景与天空光照来源分别取用；`render/sky.ts:physicalSkyMesh` 传入夜空的 uniform。完成判据：W2 的「W2 atmosphere drift」与 1.1 的「W3 day unchanged」仍绿；月亮关闭、星空关闭时背景与 W2 相同
- [ ] 5.2 `world/nightSky/stars.ts`：星星精灵（`PointsNodeMaterial`，按画面高度缩放、能量守恒、亮星略大、经透射率衰减、地平线以下与月盘内不画、折射）。完成判据：e2e 在 WebGPU 与 SwiftShader 下各出一张北京入夜朝北的图，北极星的像素位置与 3.1 的方向一致
- [ ] 5.3 `objects/Lighting.tsx:PhysicalLighting`：主光、副光的分配（太阳高于 −6° 或月亮在地平线下时主光取太阳）；月光由 `daylight` 求；太阳低于 0° 时重算月亮的天空视图；E 加上月光、月光照亮的天空与星空常数项，求 s_env；挂上星星；资源到位时 `invalidate`。`host/render.ts:openShooter` 挂载前 `await loadNightAssets`，失败时这张图失败，原因写「夜空资源加载失败，请稍后重试」。先写单测：满月高于 30°、太阳 −18° 时 CPU 的 E 在 0.1–0.4 lux；新月时月亮项为 0；主光切换的边界。完成判据：单测先红后绿；e2e 的满月之夜在 EV100 −3 下能看清院子，入夜前后材质没有重新编译（e2e 记录编译次数）

## 6. 编辑界面

- [ ] 6.1 `panels/PhysicalSkyFields.tsx`：「夜空」一组（三个开关、月亮与星空的亮度和色温，手动模式置灰并说明原因，底部注明星表出处）；时间一组的月亮信息（月相与照亮比例、月出月落、前后最近的满月和新月，点日期改成那一天）；`panels/RenderPanel.tsx` 的 EV100 参考值加夜景；`editing.ts` 的相应提交。完成判据：e2e 走一遍——开关夜空各部分、调倍率与色温、点「下一个满月」、Cmd+Z 撤回；手动模式下一组置灰

## 7. 助手

- [ ] 7.1 `creativeagent/scene_tools.go`：`edit_scene` 加 `night` 的字段与范围、「只在地点模式下画」、夜景的 EV100 参考值；`read_scene` 说明 `moon` 参考与「满月、新月先读月亮参考选日期」；`scene_tools_test.go` 固定关键句。`docs/dev/creative-scene-agent.md`、`docs/dev/agent-skills-tools.md` 记录变更；`docs/third-party-notices.md` 加 BSC5 与 NASA SVS CGI Moon Kit。完成判据：`check-go` 通过

## 8. 验收证据

- [ ] 8.1 e2e「W3」用例覆盖 AC-W3-1 至 AC-W3-8 中能自动检查的项（design「验证与交付」），参考场景加「满月之夜」「新月之夜」（院子，北京）；`SCENE_E2E_GPU` 下两个后端对照，`SCENE_E2E_HIDDEN` 下满月之夜连拍两次逐像素一致。完成判据：`SCENE_E2E_ONLY=W3` 全绿，GPU 门控项的结果记进 `discovery/research/`
- [ ] 8.2 在参考设备上测 AC-W3-10（入夜拖动时刻的帧率、静止帧时间，与 1.1 对照；白天的网络请求里没有星表）；导出 AC-W3-11 的三组样例（满月之夜、新月之夜 EV100 −7 的银河、黄昏后月亮初升），存进 `discovery/research/evidence/`，交 owner 判断。完成判据：数据与样例齐全，未达标项写明原因与建议
