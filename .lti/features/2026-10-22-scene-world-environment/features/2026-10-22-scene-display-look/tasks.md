# Tasks

先在改代码前截取基线，再改指纹、换 AgX、接工作室光，最后标定和收验收证据。证据存进本子项的 `discovery/research/<主题>`，R0 的 evidence 不改写。分支上各任务之间允许画面暂时未标定（例如 AgX 换好、`litLook.ts` 还没重标时最终渲染偏暗），但每个任务结束时 `tsc -b`、`oxlint`、相关单测和 `check-go` 都通过。

## 1. 基线

- [x] 1.1 在实施分支第一次改代码之前（与 `develop` 相同）截取基线，存进 `discovery/research/baseline`：线框和线稿的参考图（AC-W1-3 逐像素对照用）；「calibration」的正午、黄昏 +2、两盏 40 cd 壁灯的室内，以及材质板（`fixtures/scene-reference/materials.json`）的 `clay`、`lit` 导出与材质预览截图，连同整幅和地面区域的平均亮度、过曝比例、地面 R−B；R1 性能场景的视口帧时间；一组机位的白模导出（AC-W1-4 重绘对照用）。完成判据：`baseline.md` 写明场景、机位、设备与测量值，图与测量能由脚本复现
  - 结果见 `discovery/research/baseline.md`。e2e 新增 `SCENE_E2E_LOOKS` 用例截四种显示模式的视口，改完后用同一用例出对照。
  - 设计挂载点之外的改动：`scripts/creative-assets.e2e.mjs` 多存两张查看浮窗的舞台截图，供 3.2、4.3 对照。
  - 视口截图要先等着色器编译完：没等时 `room` 的最终渲染截在壁灯点亮之前。

## 2. 渲染指纹

- [x] 2.1 先改测试：`fingerprint_internal_test.go` 保留第 122–126、162–166 行 W1 之前的指纹，断言 `line` 等于旧值、`clay` 和 `lit` 不等于旧值，另外固定新值，新增「`clay` 与 `environment` 无关」；`exports_test.go` 的 `TestFingerprintCountsWhatThePassDraws` environment 一行改为只有 `lit` 过时，`TestBasedOnStateFollowsTheScene` 改环境后 `clay` 保持 current。再改 `fingerprint.go:Fingerprint`（`clay` 清空 `environment`，`clay`、`lit` 加 `view_transform: "agx"`，`line` 的 JSON 逐字节不变）。完成判据：测试先红后绿，`make check-go PKG=./internal/creativescene/...` 通过
  - 两个测试原先各自固定一份 W1 之前的指纹，现合成 `pinnedScene`、`beforeW1`、`pinned`，新增 `TestViewTransformStalesClayAndLitOnce`、`TestClayIgnoresTheEnvironment`。`TestBasedOnStateFollowsTheScene` 里改环境之后白模保持 current，所以后面「清掉线稿」一步的白模也从 stale 改为 current。

## 3. 色调映射

- [x] 3.1 先写 e2e：线框和导出线稿与 1.1 的参考图逐像素比较（差 > 2 的像素为 0）。再改 `SceneRenderer.build` 的输出节点为 `renderOutput(picture, wireframe ? ACESFilmicToneMapping : AgXToneMapping, SRGBColorSpace)`，`toneMappingExposure` 的处理不变。完成判据：线框、线稿用例绿；白模、材质预览、最终渲染、`clay` / `lit` 导出和 `render_view` 经过 AgX（e2e 读回的输出节点或像素可证）
  - 先把线框也换成 AgX 验证用例能抓到：六组场景的线框视口全部不同，线稿不受影响；再改回线框用 ACES，用例绿。e2e 新增 `SCENE_E2E_W1` 用例（线框视口、导出线稿对 1.1 基线逐像素）。
  - 只换 AgX、未重标时的视口平均亮度（基线 → AgX）：`noon` 最终渲染 196.2 → 173.8，`room` 59.8 → 71.6，白模 `noon` 212.9 → 189.8；材质预览 201.4 → 186.0。亮面变暗、暗部变亮，与调研一致，由 4.4、5.1 标定。
  - 当时没有跑日常 e2e。4.1 时发现 AgX 让两条用例不再成立，都是按亮度绝对阈值判断的：「AC-4: Shift+A adds a spot」（亮度 > 200 的像素，AgX 压低了亮部）和「renderer in effect」（实时 GI 改变的像素按 RGB 差 > 48 计）。在 `develop` 的渲染器上两条都绿。归入 5.1 和最终渲染一起标定。
- [x] 3.2 `ModelPreview.tsx` 在 `onCreated` 里设 `AgXToneMapping`。完成判据：资产库查看浮窗的 3D 预览经过 AgX，前后截图存进 `discovery/research/calibration`
  - 查看浮窗背景 (90, 92, 98) → (102, 105, 108)，略变亮，与 proposal 的风险说明一致。截图 `calibration/model-preview-*-agx.png`，基线见 `baseline/model-preview-*.png`。白模的灯在 4.3 换成工作室光后再出一次。

## 4. 白模工作室光

- [x] 4.1 先写 e2e「白模不受环境影响」：依次改太阳高度和方位、换纯色天空、开雾、改环境光、在画面里加一盏投射阴影的点光，每步取同一摄像机的 `render_view` `clay` 和白模导出，与第一张逐像素比较；视口白模只比较环境的改动；程序化天空和纯色天空下背景是同一种中性灰。再新增 `render/studio.ts`（灯组常量、`STUDIO_BACKGROUND`、`aimStudio`），`Lighting.tsx` 的 `Lighting` 在白模下挂 `StudioLighting`、删除 `ClayLighting`、`ClayLights` 改为 `StudioLights`（主光带 `userData.studio`），删除 `sky.ts:claySky` 及只供它用的类型，`SceneRenderer.render` 画之前调 `aimStudio`。完成判据：新用例绿；两台摄像机和正交视图下都是左上方正面受光、无投影、有 AO，打开景深的摄像机仍按光圈虚化；白模不再画阴影贴图
  - e2e「W1 clay」：先把三个文件换回旧的白模光照，用例在「a low sun」一步失败；换回工作室光后绿。两台相对的摄像机看到的方块正面亮度相同（200 / 200）。
  - `creative-scene-lighting.test.ts` 新增 `aimStudio` 单测：三个视角下主光都在视角左上前方，场景里其他平行光不动。
- [x] 4.2 改写依赖旧白模的用例：`creative-scene.e2e.mjs` 第 1175 行（开雾后画面变化）、第 3154 行（影子变长）、第 3198–3202 行（天空颜色）、第 3207 行（拉远后天空铺满）改为在最终渲染下检查或改写断言；`creative-scene-agent.e2e.mjs` 第 100–104、443 行改按 `STUDIO_BACKGROUND` 的显示值判断有没有模型。完成判据：这些用例按 `ONLY` 过滤运行全部绿，没有删除覆盖面
  - `creative-scene.e2e.mjs`：开雾改为在最终渲染下打开；太阳高度用例改为在白模里调太阳、白模不变（只比较实色区域，网格细线在渲染过最终渲染后会有差异），最终渲染的影子和天空照旧检查；纯色天空用例改为白模是中性灰背景；拉远看天空只检查最终渲染。
  - 另有一条不在设计清单里的用例需要改写：「export: two cameras」的景深检查。原来用对焦区域的梯度能量保留 85% 判断清晰，靠的是暗方块对亮天空的强边缘；工作室光下方块和地面、背景接近，区域里最强的边缘是失焦的地平线，失焦的背景还会在清晰物体边缘外溢出一个像素。改为只量地平线以上的方块边缘，判据改为清晰区域保留的比例超过失焦区域的 4 倍（实测 0.71 对 0.12）。
  - `creative-scene-agent.e2e.mjs`：背景色改为工作室灰的显示值 (144, 148, 154)，容差 24 → 12；黄昏样例原来比较白模的天空，改为白模是工作室灰、材质光照画出更暗的黄昏天空。
  - 以上用例与「AC-1: lights」「reference scenes」「AC-2: polished metal」「guides stay」「output resolution」以及助手 e2e 的 `render_view`、导出、实时 GI 用例都已按 `ONLY` 过滤运行，全部绿。
- [x] 4.3 `ModelPreview.tsx` 的白模改用 `StudioLights`，在 `useFrame` 里调 `aimStudio`（向上 +Y），背景仍是预览的灰色。完成判据：查看浮窗转动模型时光始终从视角左上方照来，截图存进 `discovery/research/calibration`
  - 查看浮窗白模截图 `calibration/model-preview-clay-studio.png`：光从左上方照来，右侧面较暗（旧光照见 `baseline/model-preview-clay.png`）。
- [ ] 4.4 标定工作室光（结清未决事项 1）：受光面显示值 195–225，背光面至少暗 30，过曝 < 1%，背景显示值在目标 ±2 内；`STUDIO_BACKGROUND` 的最终取值和主光角度出截图给 owner 定。完成判据：owner 定下的取值写进 `studio.ts` 和 design 未决事项 1，截图与测量存进 `discovery/research/calibration`

## 5. 最终渲染重新标定

- [x] 5.1 只调 `litLook.ts` 的 `SUN_MAX`、`SKY_SHARE`、`AMBIENT_FILL`、`AMBIENT_FILL_GI`，让正午、黄昏 +2、室内壁灯和材质板的整幅与地面平均亮度与 1.1 的基线相差 ≤ 10%；复核 `NEW_LIGHT_INTENSITY`；更新 `creative-scene-lighting.test.ts`；e2e「calibration」改为 AgX 下的区间并加入材质板，第 3592、3653 行改为对照本子项的新基线。完成判据：单测和「calibration」用例绿，区间修订理由与前后对照图存进 `discovery/research/calibration`；常量无法同时满足所有场景时逐项列出差距交 owner
  - 采用 `SUN_MAX` 3.2、`SKY_SHARE` 0.35、`AMBIENT_FILL` 0.25、`AMBIENT_FILL_GI` 0.2；五组可比的参考场景整幅和地面都在基线的 10% 以内（最大是 `dusk` 地面 −7.2%），过程见 `discovery/research/calibration.md`。
  - e2e「calibration」加入材质板，并断言四个场景的整幅和地面都在 W1 之前的 10% 以内；原有的绝对区间仍然成立，没有修订。「AC-4: Shift+A」的亮度阈值按 AgX 换算为 177（理由见 calibration.md）。
  - 新增 opt-in 用例 `SCENE_E2E_GI_FILL=1`，判定实时 GI 相对工作台的亮度变化与 W1 之前相差不超过 5 个百分点。R2 记录的数值用的是另一种测法，W1 之前的值在 `develop` 上按同一测法重测。
  - `NEW_LIGHT_INTENSITY` 不变：室内参考场景的壁灯就是新点光的缺省强度 40 cd，亮度与基线相差 2%。`creative-scene-lighting.test.ts` 的断言跟着常量走，不用改。
  - 第 3592、3653 行（现已移位）对照 R0 基线的 `SCENE_E2E_GPU` 用例，要等白模方案定下（4.4）后再截新基线，移到 8.1。

## 6. 环境提示

- [x] 6.1 `EnvironmentPanel` 新增 `display` 参数，不在最终渲染时显示「环境在「最终渲染」里生效」，写法沿用 `LightFields.tsx`、`RenderPanel.tsx`；`SceneEditor.tsx` 传入 `display`。先写 e2e：白模、材质预览、线框下打开「环境」选项卡看到提示且仍能编辑，最终渲染下没有提示。完成判据：用例绿
  - 提示放在选项卡最上方，与灯光、渲染选项卡的 `<small>` 提示同样写法。e2e「W1 environment tab」：去掉提示时失败，加上后绿。

## 7. 助手与文档

- [x] 7.1 `scene_tools.go` 中 `edit_scene` 的渲染方式说明与 `set_environment`、`render_view`、`export_shots` 的说明写明 `clay` 用固定的工作室光、不受环境影响，环境只作用于 `lit`；`scene_tools_test.go` 先固定新文字；`docs/dev/agent-skills-tools.md` 记录说明变更，`docs/dev/creative-scene-agent.md` 写显示模式与导出的光照、色调映射。完成判据：`make check-go PKG=./internal/creativeagent/...` 通过
  - 新增 `TestSceneToolsSayClayIsTheStudioLight`，先红后绿。说明共增长约 0.3 KB，参数和版本号不变，`agent-skills-tools.md` 记了排空后升级。`instructions/base.md` 没有与新行为矛盾的表述，未改。

## 8. 验收证据

- [ ] 8.1 AC-W1-6：沿用 R1、R2 的 `forceWebGL` 和隐藏标签页用例，换上新参考图，`clay`、`lit` 在 R1 阈值内、连续两次导出逐像素相同；R1 性能场景的视口帧时间不高于 1.1 的基线。完成判据：用例绿，数据存进 `discovery/research/perf`，不满足的逐项列出交 owner
- [ ] 8.2 AC-W1-3、AC-W1-4 的 owner 看图材料：参考场景的前后对照（含落日天空和饱和色灯光、背景与视口辅助对象的颜色）；同一组机位 W1 前后的白模交 gpt-image-2.5 重绘（凭证只经环境变量，拿不到时交 owner）。完成判据：样例与记录存进 `discovery/research/redraw` 和 `calibration`，等 owner 结论
