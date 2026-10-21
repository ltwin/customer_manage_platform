# Tasks

分支上各任务之间允许画面暂时不完整（例如管线换好、预设还没迁移时，预设物件按普通材质显示），但每个任务结束时 `tsc -b`、`oxlint` 和相关单测都通过。

## 1. 渲染器创建入口

- [x] 1.1 `render/gpu.ts`：`createRenderer({ canvas, alpha, output, forceWebGL })`（`requiredLimits`、停内部循环、`GizmoViewport` 垫片、`pipeline` / `direct` 输出约定、接管 `onDeviceLost`）、`capture`、`advanceFrame` / `resetFrames`（含清空三张更新记录表）、`cache`、`isLost` / `lost`、`probeRender`、测试钩子 `loseDevice({ afterCaptures })`。先写 `scripts/creative-scene-gpu.test.ts`：私有字段存在、推进加一、重置归零且同一节点再次执行 `updateBefore`、`capture` 在已丢失时抛 `DeviceLostError`、`cache` 随 `dispose` 释放。完成判据：单测先红后绿

## 2. 工作台管线

- [x] 2.1 `SceneRenderer` 改用 `RenderPipeline`：预通道（排除透视材质和 `aside` 图层）→ fork 的 `render/nodes/GTAONode.js`（+ `.d.ts`）→ `DenoiseNode` → MSAA 主画面 × AO → 输出节点（曝光 uniform、ACES、sRGB）；线框也经管线；`top` 叠层在管线内合成。`Viewport.tsx` 的 `Canvas` 改用 `gl` 工厂并传 `linear`、`flat`、`shadows="percentage"`，`LookView` 每帧 `advanceFrame()`。结清未决 L1（四条退出条件，不满足则改为预换算颜色并记在本任务下）和 L2（预通道是否重画阴影贴图）。完成判据：开发服务器上四种显示模式（含正交）都能显示，白模有 AO，变换 gizmo 颜色正确且能拖动
  - 完成记录：四种显示模式（含正交）在开发服务器与 Metal 截图对照中显示正常；「the move tool drags」等定向 e2e 通过（SwiftShader，WebGL2 后端）。
  - L1 结清：四条退出条件都满足（叠层 pass 不画背景并清成透明、`samples` 与主画面一致、gizmo 射线检测开启第 31 层），`top` 叠层在管线内按预乘 sRGB 合成；只有 drei 视图 gizmo 的线条颜色经 `overlayColor()` 预换算（其 canvas 文字贴图是原始值，不换算）。
  - L2 结清：`ShadowNode` 按摄像机每帧只更新一次，预通道不重画阴影贴图。
  - 偏差 1：线框的背景改为色调映射之后再垫在画面下面（`BackgroundlessPass`）。旧视口的线框绕过合成器直接画到画布，清屏色不经色调映射；不这样做线框背景会偏暗（差 12.8 → 0.3）。
  - 偏差 2：编辑器辅助（描边外壳、粗线、网格）放在第 30 层（`ASIDE_LAYER`），主画面画、AO 预通道不画；gizmo 在第 31 层（`OVERLAY_LAYER`）。旧管线 GTAOPass 剔除背面，外壳不进 AO，这样才与之一致。
  - 偏差 3：GTAO fork 不止改两行。r186 的 GTAONode 换了地平线搜索（二次步长、按距离平方淡出），接触阴影和墙角约浅一半，导出 clay 差 > 8 的像素 3–8%，调参越调越远；fork 内用标注块换回 GTAOShader 的搜索与积分后降到 ≤ 0.25%。守护单测改为「把标注块换成上游的块、去掉标注行后与上游逐字相同」。

## 3. 材质与天空

- [x] 3.1 `materials/patterns.ts` 14 个预设改写为 TSL `Fn`，`instances.ts`、`principled.ts` 改产出 `MeshPhysicalNodeMaterial`，透视材质设 `transparent`；删除 `materials/inject.ts`。先改 `scripts/creative-scene-materials.test.ts`：去掉 GLSL 锚点测试，改为每个预设都有 TSL 函数且输出五项、共享实例规则不变。完成判据：单测绿；材质板参考场景在最终渲染下 14 个预设都显示图案
  - 完成记录：`creative-scene-materials.test.ts` 绿；定向 e2e「AC-2: every preset」「AC-2: a brick wall」通过。
  - 偏差：透射材质不需要手动设 `transparent`，three 按 `transmission` 自动归入透明列表。
  - 待 owner：WebGPU 下 `lit` 有两张图差 > 8 的像素略超 1%（noon 1.35%、materials 1.12%），集中在混凝土预设气孔的硬边上，图案位置一致；同一 TSL 代码在 WebGL2 后端是 0.06% / 0.19%，来源是 Metal 上 WGSL 的浮点差异，不是移植误差。按 9.1「超阈值的图交 owner，不放宽」处理。
- [x] 3.2 天空与阴影：fork `render/nodes/SkyMesh.js`（+ `.d.ts`，Z 轴向上、无云、可不画太阳盘）实现 `litSky`，移植旧版天空为 `claySky`；`skyEnvironment`、`roomEnvironment` 改用 WebGPU 的 PMREM 并经 `gpu.cache`；`Lighting.tsx` 换用两个天空，阴影 `PCFShadowMap`、`radius = 1`、`bias = 0`，`sunShadow.ts` 换掉 WebGL 类型。完成判据：`noon`、`dusk` 参考场景的白模和最终渲染天空、阴影与基线目视一致，`creative-scene-lighting.test.ts` 绿
  - 完成记录：`noon`、`dusk` 的白模与最终渲染导出对基线平均差 0.27–0.55，`creative-scene-lighting.test.ts` 绿。
  - 偏差：天空没有 fork `SkyMesh.js`，而是在 `render/sky.ts` 里用 TS 写成 `litSky()` / `claySky()`（共用散射函数，文件头记着对照来源）；效果与计划相同，少一份 JS fork。

## 4. 景深与线稿

- [x] 4.1 `render/dof.ts` 替换 `dofPass.ts`，`render/lineArt.ts` 替换 `linePass.ts`（法线用 `directionToColor(normalView)`，2 倍分辨率不够时退 1 倍），删除两个旧文件；`creative-scene-camera.test.ts` 跟随改名。完成判据：`dof` 参考场景开景深的白模虚化、`line` 导出与基线目视一致，单测绿
  - 完成记录：六组参考场景的 `line` 导出对基线平均差 ≤ 0.001；`dof` 两台摄像机开景深的 clay / lit 平均差 0.31–0.43，差 > 8 的像素 ≤ 0.001%；单测绿。

## 5. 编辑器辅助

- [x] 5.1 `objects/Outline.tsx`（反向外壳）、`objects/EdgeLines.tsx`（1 px `LineSegments` / 2 px `LineSegments2` + `Line2NodeMaterial`）、`objects/InfiniteGrid.tsx`（参数沿用 drei `Grid`），替换 `SceneObjects.tsx`、`ModelMesh.tsx`、`Viewport.tsx` 里的 drei `Outlines`、`Edges`、`Grid`；`AxisGizmo` 颜色经 `overlayColor()`；删除 `render/transparent.ts` 与登记代码。完成判据：选中物件、平面物件、助手高亮、网格、视图 gizmo 在四种显示模式下显示正常，场景交互单测绿
  - 完成记录：Metal 截图对照四种显示模式 × 编辑器辅助正常；场景交互单测绿。
  - 校正：`EdgeLines` 线宽按设备像素（实测迁移前 2 px 选中边线在 2× 屏上为 1–2 设备像素），不乘 dpr。
  - 已知差异：摄像机线框图标颜色更接近设定值（迁移前因多重采样覆盖偏浅）；正交视图旧管线有 TransformControls 平面画成黑方块的问题，新管线没有。验收截图对照时一并说明。

## 6. 离屏出图与缩略图

- [x] 6.1 `host/render.ts`：`openShooter` 经创建入口（`pipeline`）与 R3F 的 `linear` / `flat` / `shadows`，每张图 `resetFrames()` + 新建 `SceneRenderer` + `capture`；`renderView`、`exportShots.ts` 的 `renderShots` 在上传前过提交点（Worker 节拍器让出一拍再查 `isLost()`，丢失则整批丢弃并抛 `DeviceLostError`）。结清未决 L3：连续 48 张导出前后 `renderer.info.memory.textures` 回到基线，有残留就逐个释放。完成判据：定向 e2e「C4 导出」「guides stay in the editor」通过，提交点有单测
  - 完成记录：定向 e2e「export: two cameras」「guides stay in the editor」「AC-6」通过；提交点单测（设备在提交点丢失时 shooter 关闭、`renderShots` 抛 `DeviceLostError`、手动导出不上传任何图）。
  - L3 结清：玻璃场景 16 台摄像机 × 3 种方式共 48 张，修正前 `lit` 每张多 1 张纹理（10 → 25），修正后 clay / line 恒为 5、lit 恒为 10。原因与修正见下条。
  - three r186 的 `ViewportTextureNode`（玻璃取背后画面）有两个问题，在 `render/gpu.ts` 的 `ownViewportTextures` 里补上并有单测：按渲染目标克隆的纹理共享 `source`，视口与导出尺寸互相覆盖，玻璃场景导出时报 `Destroyed texture used in a submit`；克隆从不释放，每张图新建管线就多留一张。节点值会在自己的 pass 运行前被绑定，所以不能随渲染目标立即释放，改为把纹理还给节点给下一个渲染目标复用。
  - 顺带：手动导出遇 `DeviceLostError` 提示「渲染设备暂时不可用，请重试」（原属 7.2，一行分支）。
- [x] 6.2 缩略图与资产预览：`sandbox/thumbnail.ts` 的 `sharedGPU()`（`direct`），`renderThumbnail` 与 `library/thumbnail.ts` 的 `renderMaterialThumbnail` 改为 async、经 `capture`、丢失时重建设备重试一次，`room` 改为 `gpu.cache`；`SceneEditor.tsx` 改为 await；`preview/ModelPreview.tsx` 用 `gl` 工厂（`direct`）。完成判据：烘焙模型、存入资产库后卡片缩略图正常（透明背景），资产查看浮窗的 3D 预览正常
  - 完成记录：定向 e2e「C6 save to library」（卡片缩略图）与 `creative-assets` 的「C6-model-preview」（资产查看浮窗 3D 预览）通过。

## 7. 设备丢失、探测与退回

- [x] 7.1 探测：`probeRender()` 接入 `SceneEditor.tsx`（探测期间加载态，失败显示现有「设备不支持」提示）和 `host/launcher.ts`（`probe` 改为 async）；兼容模式设备的工作台关 MSAA、加 `SMAANode`。先写单测：launcher 在 `probe` 为 false 时不认领。完成判据：单测绿，`creative-scene-agent.test.ts` 跟随 async 的 `probe` 后仍绿
  - 完成记录：`creative-scene-agent.test.ts` 绿（含新增「探测只问一次、认领等探测结果」）；`creative-library` e2e 仍断言画布页不加载 3D 引擎并通过；兼容模式关 MSAA、加 SMAA 已在 `sceneRenderer.ts`（2.1）。
  - 调整：`probeRender` 与取适配器的 `adapter()` 放进不依赖 three 的 `render/probe.ts`（`gpu.ts` 引用它），因为画布页的 launcher 也要探测，不能为此加载 three。`SceneEditor` 探测期间沿用「正在读取场景…」。
- [x] 7.2 设备丢失：视口 `canvasEpoch` 重建（10 秒内第 3 次停止并提示刷新）；`SceneExecutor` 遇 `DeviceLostError` 返回 `abandoned`，经 `pageExecutor.ts` 通知 launcher `deviceLost()`，暂停认领并在唤醒时（间隔 ≥ 10 秒）试建设备，成功后恢复；手动导出显示「渲染设备暂时不可用，请重试」。先写单测：执行器放弃不调 `complete`、launcher 暂停与恢复、视口重建计数。完成判据：单测绿
  - 完成记录：单测绿——执行器遇 `DeviceLostError` 返回 `abandoned`、不调 `complete`、不上传并通知页面（`render_view` 与 `export_shots` 各一遍）；launcher 丢失后不认领、不足 10 秒不重试、试建失败继续暂停、成功后恢复；视口 `lossCounter` 10 秒内第 3 次停止。另加 e2e「device loss: the viewport」（9.3 的视口部分，先在这里跑通）：重建后画面相同、选中与视角保留，连续第 3 次显示「渲染设备不可用，请刷新页面」。
  - 补充：视口只认当前渲染器的丢失，新渲染器建好时释放上一个（R3F 卸载时不会释放 WebGPU 渲染器；StrictMode 或上一次重建留下的旧渲染器在驱动重置时也会报丢失，不能算进 10 秒 3 次）。
  - 测试钩子按 design 只在测试模式挂到 `window.__sceneGPU`（e2e 夹具页先设 `window.__sceneTest`）；e2e 的 `viewportReady()` 等画布有尺寸，`open()` 与重建后都用它。

## 8. 删除旧管线与静态检查

- [x] 8.1 `scripts/creative-scene-shading.test.ts`：TypeScript AST 检查标识符、导入路径、drei 白名单，字符串里查 GLSL 特征，fork 豁免用写死的路径清单；内置违规样例和「注释里提到不报」样例。删除残余的 WebGL 写法（`lookAlongZ`、`EffectComposer`、`three-stdlib` 的 `Sky` 等），跟随修改其余单测。完成判据：`make check-frontend` 通过；把一个违规样例临时放进 `frontend/src` 时检查失败（只在本地验证，不提交）
  - 完成记录：`make check-frontend` 通过（lint、build、744 个单测）；临时在 `frontend/src` 放一个含 `ShaderMaterial` 与 GLSL 字符串的样例时检查失败，报出文件、行号和规则（只在本地验证，已删除）。`frontend/src` 现有代码零命中，残余的 WebGL 写法在前面各任务里已删净。
  - 偏差：`lines/` 规则放行三个不含着色器的几何模块（`LineSegmentsGeometry`、`LineGeometry`、`WireframeGeometry2`），`lines/webgpu/LineSegments2` 本身依赖它们；豁免清单只有 `render/nodes/GTAONode.js`（天空没有 fork，见 3.2）。

## 实施中的测试调整

- e2e `open()` 等画布有尺寸（渲染器异步创建完成）后再交给用例；否则冷启动的第一页按键和截图落在默认 300 × 150 的画布上（「AC-2: a brick wall」因此失败过）。

## 9. 验收证据

- [ ] 9.1 迁移对照 e2e（`SCENE_E2E_GPU`，Metal 参数）：六组参考场景 × `clay`、`line`、`lit` + 正交白模，对 R0 `baseline/` 算平均差与差 > 8 的像素比例并按阈值断言，并排图和差异图写进 `discovery/research/parity/`；同一用例加 `forceWebGL` 一轮，与 WebGPU 的图比（AC-R1-1、AC-R1-4 画面）。首次跑通后按实测值的 1.5 倍收紧阈值（不超过 Epic 上限）；超阈值的图交 owner，不放宽
- [ ] 9.2 确定性与退回 e2e：自启 Chrome 后台标签页连续两次导出（3 台摄像机 × 3 种方式）逐像素相同，并与前台同一张图相同（AC-R1-3）；`--use-angle=swiftshader` 下退回 WebGL2 能打开、编辑、导出且无原始错误；headless 默认参数下显示「设备不支持」且不认领（AC-R1-4 功能）
- [ ] 9.3 设备丢失 e2e（AC-R1-5）：视口丢失后恢复且选中、视角、已进 outbox 的编辑保留；`render_view`、`export_shots` 丢失后回队并最终完成（缩短租约）；`export_shots` 第 2 张之后丢失时一张都不上传；手动导出丢失后提示、画布无图、重试成功；资产卡片缩略图丢失后重试成功
- [ ] 9.4 性能与外观收尾：`SCENE_E2E_PERF` 重跑 AC-9 性能用例，与 `baseline/perf.json` 逐项对比（含加灯、删灯最长一帧）并写进 evidence（AC-R1-7）；四种显示模式 × 编辑器辅助截图对照迁移前（AC-R1-6）；定向回归交互、C4 导出、C5 校准、C5 AC-6、参考场景用例；尝试兼容模式设备，Safari 26.5 人工打开并导出一次，结果与已知限制（含提交点残余窗口）写进 `discovery/research/acceptance.md`（AC-R1-8）
