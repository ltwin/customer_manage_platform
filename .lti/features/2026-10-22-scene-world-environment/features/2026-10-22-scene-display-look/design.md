# Change 详细设计

## 继承的设计

- **Epic**：`scene-world-environment` 基线 v1。
  - 本子项沿用的条目：
    - design「领域模型与业务规则」的不变量：白模不读取环境与灯光；
    - design「架构设计」模块表中 `objects/Lighting.tsx` 和 `render/sceneRenderer.ts` 输出节点两行；
    - design「关键机制」中的「显示变换」「白模工作室光」「渲染指纹」；
    - design「验证与交付」；
    - design「未决事项」第 5、9 项。
  - 本子项遵守 `patterns: 场景着色写法`：只用 TSL，不写 GLSL 或 WGSL。
  - 本子项不交付 Epic 的目标契约。
- **调研**：discovery/research/agx-vs-aces.md。three 的 AgX 与 ACES 在中灰几乎相同，差别在对比度和饱和色。
- **项目现状**：
  - 白模由 `objects/Lighting.tsx:ClayLighting` 搭建：天空、雾和环境光都取自场景，太阳方向跟随 `environment.sky`，太阳投射阴影。
  - 所有显示模式都经过 `render/sceneRenderer.ts` 第 573 行的 `renderOutput(picture, ACESFilmicToneMapping, SRGBColorSpace)`。
  - 线稿走自己的 `LineRenderer`，不经过这里。
  - 资产库查看浮窗的 3D 预览（`preview/ModelPreview.tsx`）由 R3F 的 `<Canvas>` 默认设为 ACES Filmic，没有 `flat`；它的白模用 `ClayLights`，太阳固定在 (3, 5, 4)。
  - 模型缩略图（`sandbox/thumbnail.ts`）不做色调映射，本子项不改。

## 领域模型与业务规则

不新增领域对象，只改变三条规则：

- **白模画面只由几何、摄像机和工作室光决定**：
  - 几何包括物件、变换、可见性和层级；摄像机包括位置、朝向、视场角和景深参数。
  - 与 `environment`、灯光物件、材质和 `render` 都无关。
  - 视口、正交视图、`render_view` 的 `clay`、白模导出，以及资产库预览的「白模」，用同一套工作室光。
- **色调映射**：

  | 输出 | 色调映射 |
  |---|---|
  | 白模、材质预览、最终渲染、`clay` / `lit` 导出与 `render_view`、资产库 3D 预览 | AgX |
  | 线框 | 保持 ACES Filmic（见「色调映射」） |
  | 线稿、模型缩略图 | 不做色调映射，不变 |

- **渲染指纹**：按 Epic design「渲染指纹」的前两行执行，见「渲染指纹」一节。

## 模块职责与协作

| 模块 | 本次职责 | 不负责 |
|---|---|---|
| `objects/Lighting.tsx` | `StudioLighting` 取代 `ClayLighting`：中性背景，加一组固定的工作室灯，不接收 `environment`。`ClayLights` 改为 `StudioLights`，供资产库预览复用 | 灯的朝向随视角更新，由下一行负责 |
| `render/studio.ts`（新增） | 工作室光的常量：灯在视空间里的方向、强度、背景色。`aimStudio(scene, camera)` 按摄像机摆好标了 `userData.studio` 的主光 | 色调映射 |
| `render/sceneRenderer.ts` | 输出节点按显示模式选色调映射；`render()` 在画之前调用 `aimStudio`，这是视口、`render_view` 和导出共用的唯一入口 | 搭建灯光 |
| `render/litLook.ts` | AgX 下重新标定的常量 | — |
| `preview/ModelPreview.tsx` | 改用 AgX；白模用 `StudioLights`，每帧调用 `aimStudio` | — |
| `panels/EnvironmentPanel.tsx`、`SceneEditor.tsx` | 不在最终渲染模式时显示提示 | — |
| `backend/internal/creativescene/fingerprint.go` | `clay` 清除 `environment`；`clay`、`lit` 加 `view_transform` | — |
| `backend/internal/creativeagent/scene_tools.go` | 工具说明 | — |

主流程不变：`Lighting` 按显示模式挂载对应的光照，`SceneRenderer.render(camera, frame)` 出图。本次只在 `render()` 里、画之前加一步 `aimStudio`。场景里没有工作室光（最终渲染、材质预览）时，这一步什么都不做。

## 关键机制

### 工作室光（Epic 未决事项 5，owner 2026-10-22 定：与 Blender 一致）

按 Blender 实体模式（Solid）的默认设置：

| Blender 实体模式的默认设置 | 本项目的白模 |
|---|---|
| Lighting 为 Studio：最多 4 盏虚拟灯，只照亮视口，不参与渲染 | 一组固定的工作室灯，不读取场景的天空、太阳、环境光、雾和灯光物件 |
| World Space Lighting 关闭：灯随视角转动 | 灯随视角转动，每台摄像机和正交视图都是正面受光 |
| Shadow 关闭 | 不投射阴影 |
| Cavity 关闭 | AO 保留（见下） |

- **AO 保留**：Blender 默认不开 Cavity，但现有规格「视口与导出同一套白模画面」规定白模带 AO，Epic「白模工作室光」也写明保留。没有阴影以后，AO 是交代物体贴地和相互接触的主要线索，所以保留，不与 Blender 一致。
- **不采用的选项**：
  - 固定在世界里（对应 Blender 打开 World Space Lighting）：背对灯的机位会是逆光，正面偏暗。
  - 打开阴影（对应 Blender 打开 Shadow）：Blender 默认不开，owner 定为一致。
- **灯组**（强度在 AgX 下标定）：
  - 主光：平行光，从视角左上方照来，即视空间里向左 40°、向上 40°，强度 6。不投射阴影，带 `userData.studio = true`。补光（半球光 0.6）和环境光（0.7）沿用旧白模的取值。owner 2026-10-22 看对照图选定（方案 A，`discovery/research/calibration.md`）。
  - 补光：半球光，上方偏白，下方偏暖灰。上方向取竖直方向：视口里是 +Z（`space.ts:positionToView`），资产库预览里是 +Y，与现在的 `ClayLights` 的 `up` 参数相同。
  - 一点固定的环境光，让背光面不至于死黑。
- **背景**：中性灰常量 `STUDIO_BACKGROUND`。
  - 背景和画面一起经过输出节点的色调映射，只有线框单独合成背景。所以这个常量按屏幕上的显示值来定，不按写入的颜色。
  - 取现在白模在非程序化天空下的缺省色 `#8a8f98`，它在 AgX 下显示为 (144, 148, 154)。owner 2026-10-22 看图确认不改。
- **标定目标**：在「院子」参考场景的白模里测量 8 位亮度。
  - 白盒受主光的面在 195–225；
  - 背光面比受光面暗至少 30，形体看得出来；
  - 过曝像素少于 1%；
  - 背景的显示值与选定的目标相差不超过 2。
- **跟随方式**：
  - `aimStudio(scene, camera)` 找到 `userData.studio` 的平行光，把视空间的固定方向用 `camera.quaternion` 转到世界空间，设置 `light.position = light.target.position + 方向 × 60`。
  - `SceneRenderer.render` 在画之前调用它，所以视口、正交视图、`render_view` 和导出都走同一条路径。
  - 资产库预览不经过 `SceneRenderer`，在 `useFrame` 里调用同一个函数。
- **资产库预览**：灯位和强度与场景的白模相同。预览本来就没有 AO，背景仍是 `previewBackground`（`ModelPreview.tsx:65-75`），这两点不变。
- **确定性**：灯只取决于摄像机，同一镜头每次导出都相同。
- **性能**：白模不再画阴影贴图，视口和导出都少一个阴影 pass。

### 色调映射

- **输出节点**（`sceneRenderer.ts` 的 `build`）：改为 `renderOutput(picture, wireframe ? ACESFilmicToneMapping : AgXToneMapping, SRGBColorSpace)`。
- **曝光**：`toneMappingExposure` 照旧在输出节点生效，`lit` 是 `2^exposure_ev`，其余是 1。three 的 `agxToneMapping` 同样接收曝光参数。
- **线框保持 ACES，理由**：
  - 线框的轮廓线（`wireColor`）和画进主画面的辅助对象，颜色都是在 ACES 下定的。
  - 线框保持 ACES，就能做到画面完全不变，代价只是一个条件判断。
  - 不采用的做法：改为不做色调映射，再把 `wireColor` 换成现在显示出来的颜色。这样轮廓线能保持不变，但灯光图标、摄像机视锥这些辅助对象的颜色会变。
- **资产库预览**：
  - 在 `<Canvas>` 的 `onCreated` 里设 `toneMapping = AgXToneMapping`。
  - R3F 9.8.1 只在 `flat` 改变时才改写 `toneMapping`，所以 `onCreated` 设的值会一直保留。
- **背景与视口里的辅助对象**：
  - 纯色背景、灯光图标、摄像机视锥和视口网格（aside 层，`Viewport.tsx:1082`）都画在主画面里，改用 AgX 后显示值会变。
    - 白模背景 `#8a8f98` 从约 (153, 158, 167) 变为 (144, 148, 154)；
    - 资产库预览的背景 `#5b5d61` 从约 90 变为 102。
  - 辅助对象和网格不出现在导出里。
  - 前后对照时一并给 owner 看。如果不能接受，再把背景像线框那样单独合成，或把辅助对象移到不做色调映射的覆盖层。

### 重新标定最终渲染

- **基线**：动工前在 `develop` 上截取参考图，存进本子项的 evidence，R0 的 evidence 不改写。场景包括：
  - 现有 e2e「calibration」用例的正午、黄昏 +2、两盏 40 cd 壁灯的室内；
  - 材质板：复用 R0 的参考场景 `frontend/scripts/fixtures/scene-reference/materials.json`，即 45° 程序化天空下的 16 种预设。
- **测量**：
  - 整幅画面和地面区域的平均亮度；
  - 过曝像素比例；
  - 地面的 R−B，检查日光是否中性。
- **调整**：
  - 只动 `litLook.ts` 的常量，即 `SUN_MAX`、`SKY_SHARE`、`AMBIENT_FILL`、`AMBIENT_FILL_GI`，目标是各场景的平均亮度与基线相差不超过 10%。
  - 调研表明中灰不变，所以预期只需小幅调整：
    - 正午偏暗，因为亮面被压低；
    - 室内偏亮，因为暗部被抬高。
  - 灯光物件的强度存在场景里，不能按场景重标。改常量也不能同时满足所有参考场景时，逐项列出差距，交 owner 取舍。
- **新灯的缺省强度**：`NEW_LIGHT_INTENSITY` 只影响新加的灯，标定后按同样的室内场景复核。
- **材质预览**：不调参数，只出前后对照图。如果 owner 认为偏灰，再调 `PreviewLighting` 的 `environmentIntensity`。

### 渲染指纹

`Fingerprint` 的改动：

```go
p := Scene{Environment: s.Environment}
if pass != PassLit {           // 原来只有 PassLine
    p.Environment = Environment{}
}
...
raw, err := json.Marshal(struct {
    V             int              `json:"v"`
    Pass          string           `json:"pass"`
    CameraID      string           `json:"camera_id"`
    ViewTransform string           `json:"view_transform,omitempty"` // clay、lit 为 "agx"，line 为空
    Scene         fingerprintScene `json:"scene"`
}{...})
```

- 线稿序列化出的 JSON 与原来逐字节相同，所以指纹不变。
- 已有的 `clay` 和 `lit` 指纹因为多了 `view_transform` 而不再匹配，`clay` 还因为清除了 `environment`。
- 指纹版本仍是 `v1`。

## 局部设计与偏差

- **资产库预览也改**：Epic roadmap 列出的白模范围是视口、`render_view` 和导出。资产库预览不在其中，但它同样显示「白模」，也同样经过 ACES。一起改，能让模型在资产库和场景里看起来一致。这只是范围内的一致性调整，不涉及共享设计，所以不修订 Epic。
- **线框保持 ACES**：Epic design「显示变换」要求线框画面不变，没有规定做法，见「色调映射」。
- **规格中一处不符的改正**：`creative/scene`「渲染器与输出分辨率」的场景「换成实时 GI」写着「白模模式下同样有染色和接触处的暗部」。它与同一条规定的描述「白模……总按工作台画」以及代码（`engines.ts:effectiveEngine`）都不符，是 R2 修订 v4 时漏改的。本子项改正这一句。
- **`architecture: 场景模块`**：「白模不读取环境与灯光」写在 Epic 的 `architecture: MODIFY 场景模块` 里，由 W2 整节交付。W1 交付后到 W2 归档前，项目的 architecture 契约暂时还写着旧规则。这是 Epic 批准的交付安排，不另行处理。

## 接口与数据契约

不变。场景数据、API 和 OpenAPI 都不改。渲染指纹是服务端内部的计算，它的变化按 Epic 的规定生效。工具说明属于助手提示，不是契约。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `frontend/src/creative-scene/render/studio.ts` | 新增 | 工作室光的常量、`STUDIO_BACKGROUND` 和 `aimStudio` |
| `frontend/src/creative-scene/objects/Lighting.tsx:Lighting` | 修改 | `solid` 挂载 `StudioLighting`，不再传 `environment` |
| `frontend/src/creative-scene/objects/Lighting.tsx:ClayLighting` | 删除 | 由 `StudioLighting` 取代 |
| `frontend/src/creative-scene/objects/Lighting.tsx:ClayLights` | 修改 | 改为 `StudioLights`：工作室灯组，主光带 `userData.studio` |
| `frontend/src/creative-scene/render/sky.ts:claySky` | 删除 | 白模不再画天空；连同仅供它使用的类型和注释 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.build` | 修改 | 输出节点改用 AgX，线框例外 |
| `frontend/src/creative-scene/render/sceneRenderer.ts:SceneRenderer.render` | 修改 | 画之前调用 `aimStudio` |
| `frontend/src/creative-scene/render/litLook.ts` | 修改 | AgX 下重新标定的常量及注释 |
| `frontend/src/creative-scene/preview/ModelPreview.tsx` | 修改 | `onCreated` 设 AgX；白模改用 `StudioLights` 和 `aimStudio` |
| `frontend/src/creative-scene/panels/EnvironmentPanel.tsx:EnvironmentPanel` | 修改 | 新增 `display` 参数，不在最终渲染时显示提示 |
| `frontend/src/creative-scene/SceneEditor.tsx` | 修改 | 把 `display` 传给 `EnvironmentPanel` |
| `backend/internal/creativescene/fingerprint.go:Fingerprint` | 修改 | 见「渲染指纹」 |
| `backend/internal/creativescene/fingerprint_internal_test.go` | 修改 | 保留第 122–126、162–166 行固定的 W1 之前的指纹，断言 `line` 等于旧值，`clay` 和 `lit` 不等于旧值；另外固定新值；新增「`clay` 与 `environment` 无关」 |
| `backend/internal/creativescene/exports_test.go` | 修改 | `TestFingerprintCountsWhatThePassDraws` 的 environment 一行改为只有 `lit` 过时；`TestBasedOnStateFollowsTheScene` 改环境后 `clay` 保持 current |
| `backend/internal/creativeagent/scene_tools.go` | 修改 | `edit_scene` 的渲染方式说明与 `set_environment`、`render_view`、`export_shots` 的说明 |
| `backend/internal/creativeagent/scene_tools_test.go` | 修改 | 固定新的说明文字 |
| `frontend/scripts/creative-scene.e2e.mjs` | 修改 | 新增用例见「验证与交付」。依赖旧白模的用例改为在最终渲染下检查，或改写断言：第 1175 行开雾后画面变化、第 3154 行影子变长、第 3198–3202 行天空颜色、第 3207 行拉远后天空铺满。第 3592、3653 行 clay 和 lit 与 R0 基线的对照，改为对照本子项的新基线 |
| `frontend/scripts/creative-scene-agent.e2e.mjs` | 修改 | 第 100–104、443 行用天空色 `#3355ff` 当白模背景判断有没有模型，改为按 `STUDIO_BACKGROUND` 的显示值判断 |
| `frontend/scripts/creative-scene-lighting.test.ts` | 修改 | 标定常量变化后的单测 |
| `docs/dev/creative-scene-agent.md` | 修改 | 显示模式与导出的光照、色调映射 |
| `docs/dev/agent-skills-tools.md` | 修改 | 工具说明的变更记录 |

## 验证与交付

| 验收 | 验证 |
|---|---|
| AC-W1-1 | e2e 新增「白模不受环境影响」。<br>• 依次改太阳、换纯色天空、开雾、改环境光、在画面里加一盏投射阴影的点光，每步取 `render_view` 的 `clay`，与第一张逐像素比较。<br>• 视口只比较环境的改动。<br>• 再在两台摄像机之间切换，检查各自受光面的亮度落在标定区间内 |
| AC-W1-2 | 只用后端单测（见挂载点）：过时判断在服务端完成，e2e 里没有对应的用例 |
| AC-W1-3 | e2e「calibration」改为 AgX 下的区间，并加入材质板；基线图与新图存进 evidence，由 owner 看图。线框和线稿：动工前在 `develop` 上截取参考图，之后逐像素比较 |
| AC-W1-4 | 手动：同一组机位，W1 前后各导出白模，交给 gpt-image-2.5 重绘，样例存进 evidence |
| AC-W1-5 | e2e 检查提示文字；`scene_tools_test.go` 固定说明 |
| AC-W1-6 | 沿用 R1、R2 已有的 `forceWebGL` 和隐藏标签页用例，换上新的参考图；视口帧时间用 R1 的性能场景记录 |

- 验证范围按项目约定：前端单测、定向 e2e（用 `ONLY` 过滤，换端口）、`check-go PKG=./internal/creativescene/... ./internal/creativeagent/...`；合回 `develop` 时运行 `make check-merge`。
- **上线与回退**：
  - 没有数据迁移。上线后，已有的 `clay` 和 `lit` 导出各显示一次「场景已更新」。
  - 指纹在服务端导出时计算（`exports.go:187`），前端没有版本校验。上线时还开着的旧页面导出的图，用的是 ACES 和旧白模，却会拿到新指纹，之后一直显示为最新。这个窗口只到页面刷新为止，予以接受，上线说明里提醒刷新。
  - 回退代码后，指纹回到旧规则：W1 之前的导出又恢复为最新，W1 期间导出的图会显示过时。

## 未决事项

无。原未决事项 1（`STUDIO_BACKGROUND` 的取值和主光角度）已由 owner 2026-10-22 看对照图定为方案 A：主光向左 40°、向上 40°，强度 6，背景 `#8a8f98`。
