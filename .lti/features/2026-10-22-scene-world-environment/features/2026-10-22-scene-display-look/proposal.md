---
risk:
  factors:
    - broad-impact
behavior: changed
reads:
  - creative/scene
  - creative/library
decisionsPending: false
---
# 本次交付

## Why

本子项是 Epic `scene-world-environment` 基线 v1 的 W1，也是第一个子项。它交付 Epic 的目标 1：白模只看结构，色调映射改用 AgX（proposal「目标」1，AC-1、AC-1b）。

- **先做的理由**（roadmap「排序原则」）：
  - 白模先与环境脱钩，之后每加一项环境能力，都只需要考虑最终渲染。
  - 物理天空（W2）可以直接在 AgX 下标定，不用先在 ACES 下标一次再重标。
- **为什么两件事一起交付**：两件事都改变「画面看起来怎样」。放在同一个子项里，已有导出图只因此过时一次。
- **要解决的不确定性**（roadmap W1）：
  - 固定光下，白模导出的重绘一致性会不会变差；
  - AgX 下，现有参考场景的观感如何。

  两项都由 owner 看前后对照图判断。

## What Changes

1. **白模固定工作室光**：
   - 白模不再画天空，也不受太阳、环境光、雾和灯光影响。背景是中性灰，光照固定。范围包括视口的白模、正交视图、`render_view` 的 `clay` 和白模导出。
   - 景深和 AO 照旧（Epic design「白模工作室光」）。
   - 工作室光与 Blender 实体模式的默认设置一致（owner 2026-10-22 定，结清 Epic 未决事项 5）：灯随视角转动，不投射阴影。AO 照旧保留（design「工作室光」）。
   - 资产库查看浮窗里 3D 预览的「白模」用同样的灯位和强度，让白模在各处看起来一致。预览本来就没有阴影和 AO，背景仍是预览的灰色，这两点不变。
2. **色调映射改为 AgX**：
   - 白模、材质预览、最终渲染、`clay` 和 `lit` 导出、`render_view`，以及资产库的 3D 预览，都从 ACES Filmic 改为 three 0.186.1 的 `AgXToneMapping`，不做成可选项（Epic design「显示变换」）。
   - 线稿和线框保持原样。
   - 重新标定最终渲染的亮度，让现有参考场景（正午、黄昏、室内灯光、材质板）改完后的整体亮度接近原来的。
     - 调研（discovery/research/agx-vs-aces.md）表明，three 的 AgX 和 ACES 把中灰映射到几乎相同的值，差别在对比度：暗部更亮，亮部更暗。
     - 所以标定的重点是对比度造成的偏差，不是整体加曝光。
3. **环境选项卡的提示**：
   - 不在最终渲染模式时，「环境」选项卡提示环境只在「最终渲染」里生效。
   - 与灯光、渲染器、曝光补偿现有的提示写法一致（`panels/LightFields.tsx`、`panels/RenderPanel.tsx`）。
4. **渲染指纹**（Epic design「渲染指纹」）：
   - `clay` 不再计入 `environment`，与线稿相同。
   - `clay` 和 `lit` 加一个常量键 `view_transform: "agx"`。
   - 指纹版本不升级。上线后，已有的 `clay` 和 `lit` 导出各显示一次「场景已更新」，线稿不提示。
5. **助手**：
   - `edit_scene` 的说明写明 `clay` 用固定的工作室光，环境只作用于 `lit`。
   - `render_view`、`export_shots` 的说明与之一致。
   - `docs/dev/agent-skills-tools.md` 记录这次说明的变更。
6. **规格**：
   - 修订 `creative/scene` 的「四种显示模式」「编辑灯光与环境」「导出图提示场景已更新」，即 roadmap 点名的两处规定，外加显示模式的描述。
   - 顺带改正「渲染器与输出分辨率」里一处与代码不符的描述：「白模模式下同样有染色和接触处的暗部」。实际上白模总按工作台画，R2 的 Epic 修订 v4 已经这样规定。

本子项不交付 Epic 的目标契约（roadmap「交付契约：无」）：场景数据和 API 的外形都不变。`architecture: MODIFY 场景模块` 里「白模不读取环境与灯光」这一条，由 W2 随整节交付。

## Acceptance Criteria

整体验收留在 Epic。本子项承担 Epic 的 AC-1 和 AC-1b。参考设备沿用 R0（M4 Pro，24 GB）。

- **AC-W1-1 白模不受环境影响**（Epic AC-1）：
  - **WHEN** 改太阳高度和方位，把天空换成纯色，打开雾，改环境光，或在画面里加一盏投射阴影的点光，**THEN** 同一摄像机的 `render_view` 的 `clay` 和白模导出都不变（逐像素比较，差 > 2 的像素为 0）。
    - 视口的白模只比较环境的改动，判定方式相同。加灯不比较视口，因为视口会画出灯光图标并选中它。
  - **WHEN** 场景的天空是程序化天空或纯色，**THEN** 白模的背景都是同一种中性灰，看不到天空和太阳。
  - **WHEN** 从不同摄像机或正交视图看白模，**THEN** 每个视角都是正面受光，光从视角左上方照来，没有投影，AO 照旧；打开景深的摄像机仍按光圈虚化。
- **AC-W1-2 白模导出与过时**（Epic AC-1）：
  - **WHEN** W1 上线后读取已有导出图的状态，**THEN** 已有的 `clay` 和 `lit` 导出各显示一次「场景已更新」，`line` 不显示。
  - **WHEN** 之后在白模导出后改太阳、天空、雾或环境光，**THEN** 只有 `lit` 导出显示「场景已更新」，`clay` 和 `line` 不显示。
  - 只用后端单测验证，因为过时判断在服务端完成：
    - 保留现有固定的 W1 之前的指纹，断言 `line` 等于旧值，`clay` 和 `lit` 不等于旧值；
    - 另外固定新值；
    - 断言 `clay` 的指纹与 `environment` 无关。
- **AC-W1-3 AgX**（Epic AC-1b）：
  - **WHEN** 在最终渲染、`lit` 导出和 `render_view` 的 `lit` 下打开参考场景（正午、黄昏、两盏壁灯的室内、材质板），**THEN** 输出经过 AgX。
    - 整幅画面和地面区域的平均亮度与 W1 之前相差不超过 10%。
    - 现有 e2e「calibration」检查的各项区间仍然成立，必要时按标定结果修订区间，修订理由写进 evidence。
    - 落日天空和饱和色的灯光不再偏成橙黄或发白。
    - 前后对照图存进 evidence，由 owner 判断。
  - **WHEN** 打开材质预览，**THEN** 输出经过 AgX；前后对照图由 owner 判断。
  - **WHEN** 打开线框或导出线稿，**THEN** 画面与 W1 之前逐像素相同（差 > 2 的像素为 0）。
- **AC-W1-4 重绘对照**：**WHEN** 用同一组机位分别导出 W1 前后的白模，交给 gpt-image-2.5 重绘，**THEN** 样例存进 evidence，由 owner 判断重绘一致性没有变差。
- **AC-W1-5 提示与助手**：
  - **WHEN** 摄影师在白模、材质预览或线框模式下打开「环境」选项卡，**THEN** 看到「环境在「最终渲染」里生效」的提示。
  - **WHEN** 助手读工具说明，**THEN** 说明里写明 `clay` 用固定的工作室光、不受环境影响；工具说明的单测固定这段文字。
- **AC-W1-6 两个后端与确定性**：
  - **WHEN** 用 `forceWebGL` 导出参考场景的 `clay` 和 `lit`，**THEN** 与 WebGPU 后端的结果在 R1 的阈值内。
  - **WHEN** 在隐藏标签页连续两次导出，**THEN** 工作台的 `clay` 和 `lit` 逐像素相同（AC-R1-3 不变）。
  - 视口帧时间不高于 W1 之前。

## Impact

- 前端 `frontend/src/creative-scene/`：
  - `objects/Lighting.tsx`：白模改为工作室光，去掉对 `environment` 的读取；
  - `render/sky.ts`：去掉不再使用的 `claySky`；
  - `render/sceneRenderer.ts`：输出节点改用 AgX，线框例外；按视角摆放工作室光；
  - `render/litLook.ts`：重新标定的常量；
  - `preview/ModelPreview.tsx`：色调映射与工作室光；
  - `panels/EnvironmentPanel.tsx`、`SceneEditor.tsx`：环境提示。
- 后端：
  - `backend/internal/creativescene/fingerprint.go` 及其测试；
  - `backend/internal/creativeagent/scene_tools.go` 及其测试。
- 测试：
  - `frontend/scripts/creative-scene.e2e.mjs`、`creative-scene-agent.e2e.mjs`：新增白模不受环境影响，以及线框、线稿不变；改写依赖旧白模的用例；标定改为 AgX（design「挂载点」）；
  - 前端单测 `creative-scene-lighting.test.ts`。
- 文档：
  - `docs/dev/creative-scene-agent.md`：显示模式与导出的光照、色调映射；
  - `docs/dev/agent-skills-tools.md`：工具说明的变更记录。
- 规格：`creative/scene` 的 4 处修订（specs.md）。

## Risk Notes

- **影响面广（broad-impact）**：
  - 所有已有的白模和材质光照导出都会显示一次「场景已更新」，这是 Epic 批准的预期行为。
  - 已有场景的白模画面全部改变，例如带雾或黄昏天空的场景，白模里不再有雾和黄昏色调。
  - 助手基于旧白模图做的重绘提示不受影响：图片不变，只是标为过时。
- **对比度变化**：
  - AgX 的整条曲线更平，中灰几乎不变，暗部更亮、亮部更暗。以亮面为主的画面会显得发灰。
  - 用 e2e 测量和 owner 看图把关，不满足时逐项列出差距交 owner 决定。
- **背景和视口辅助对象的颜色**：
  - 除线框外，背景是和画面一起经过色调映射的。换成 AgX 后，纯色背景会变暗一点，例如 `#8a8f98` 从约 (153, 158, 167) 变为 (144, 148, 154)；资产库预览的背景则会变亮一点。
  - 灯光图标、摄像机视锥和视口网格也画在主画面里，换成 AgX 后颜色会变灰一些。它们只出现在视口，导出不画。
  - 在前后对照里一并给 owner 看。
- **上线时新旧页面混用**：渲染指纹在服务端导出时计算，前端没有版本校验。上线时还开着的旧页面导出的图，用的是 ACES 和旧白模，却会拿到新指纹，之后一直显示为最新。这个窗口只到页面刷新为止，予以接受，上线说明里提醒刷新。
- **没有新的共享设计问题**，不需要修订 Epic。
