# Tasks

分支上各任务之间允许画面暂时不完整（例如管线换好、预设还没迁移时，预设物件按普通材质显示），但每个任务结束时 `tsc -b`、`oxlint` 和相关单测都通过。

## 1. 渲染器创建入口

- [x] 1.1 `render/gpu.ts`：`createRenderer({ canvas, alpha, output, forceWebGL })`（`requiredLimits`、停内部循环、`GizmoViewport` 垫片、`pipeline` / `direct` 输出约定、接管 `onDeviceLost`）、`capture`、`advanceFrame` / `resetFrames`（含清空三张更新记录表）、`cache`、`isLost` / `lost`、`probeRender`、测试钩子 `loseDevice({ afterCaptures })`。先写 `scripts/creative-scene-gpu.test.ts`：私有字段存在、推进加一、重置归零且同一节点再次执行 `updateBefore`、`capture` 在已丢失时抛 `DeviceLostError`、`cache` 随 `dispose` 释放。完成判据：单测先红后绿

## 2. 工作台管线

- [ ] 2.1 `SceneRenderer` 改用 `RenderPipeline`：预通道（排除透视材质和 `aside` 图层）→ fork 的 `render/nodes/GTAONode.js`（+ `.d.ts`）→ `DenoiseNode` → MSAA 主画面 × AO → 输出节点（曝光 uniform、ACES、sRGB）；线框也经管线；`top` 叠层在管线内合成。`Viewport.tsx` 的 `Canvas` 改用 `gl` 工厂并传 `linear`、`flat`、`shadows="percentage"`，`LookView` 每帧 `advanceFrame()`。结清未决 L1（四条退出条件，不满足则改为预换算颜色并记在本任务下）和 L2（预通道是否重画阴影贴图）。完成判据：开发服务器上四种显示模式（含正交）都能显示，白模有 AO，变换 gizmo 颜色正确且能拖动

## 3. 材质与天空

- [ ] 3.1 `materials/patterns.ts` 14 个预设改写为 TSL `Fn`，`instances.ts`、`principled.ts` 改产出 `MeshPhysicalNodeMaterial`，透视材质设 `transparent`；删除 `materials/inject.ts`。先改 `scripts/creative-scene-materials.test.ts`：去掉 GLSL 锚点测试，改为每个预设都有 TSL 函数且输出五项、共享实例规则不变。完成判据：单测绿；材质板参考场景在最终渲染下 14 个预设都显示图案
- [ ] 3.2 天空与阴影：fork `render/nodes/SkyMesh.js`（+ `.d.ts`，Z 轴向上、无云、可不画太阳盘）实现 `litSky`，移植旧版天空为 `claySky`；`skyEnvironment`、`roomEnvironment` 改用 WebGPU 的 PMREM 并经 `gpu.cache`；`Lighting.tsx` 换用两个天空，阴影 `PCFShadowMap`、`radius = 1`、`bias = 0`，`sunShadow.ts` 换掉 WebGL 类型。完成判据：`noon`、`dusk` 参考场景的白模和最终渲染天空、阴影与基线目视一致，`creative-scene-lighting.test.ts` 绿

## 4. 景深与线稿

- [ ] 4.1 `render/dof.ts` 替换 `dofPass.ts`，`render/lineArt.ts` 替换 `linePass.ts`（法线用 `directionToColor(normalView)`，2 倍分辨率不够时退 1 倍），删除两个旧文件；`creative-scene-camera.test.ts` 跟随改名。完成判据：`dof` 参考场景开景深的白模虚化、`line` 导出与基线目视一致，单测绿

## 5. 编辑器辅助

- [ ] 5.1 `objects/Outline.tsx`（反向外壳）、`objects/EdgeLines.tsx`（1 px `LineSegments` / 2 px `LineSegments2` + `Line2NodeMaterial`）、`objects/InfiniteGrid.tsx`（参数沿用 drei `Grid`），替换 `SceneObjects.tsx`、`ModelMesh.tsx`、`Viewport.tsx` 里的 drei `Outlines`、`Edges`、`Grid`；`AxisGizmo` 颜色经 `overlayColor()`；删除 `render/transparent.ts` 与登记代码。完成判据：选中物件、平面物件、助手高亮、网格、视图 gizmo 在四种显示模式下显示正常，场景交互单测绿

## 6. 离屏出图与缩略图

- [ ] 6.1 `host/render.ts`：`openShooter` 经创建入口（`pipeline`）与 R3F 的 `linear` / `flat` / `shadows`，每张图 `resetFrames()` + 新建 `SceneRenderer` + `capture`；`renderView`、`exportShots.ts` 的 `renderShots` 在上传前过提交点（Worker 节拍器让出一拍再查 `isLost()`，丢失则整批丢弃并抛 `DeviceLostError`）。结清未决 L3：连续 48 张导出前后 `renderer.info.memory.textures` 回到基线，有残留就逐个释放。完成判据：定向 e2e「C4 导出」「guides stay in the editor」通过，提交点有单测
- [ ] 6.2 缩略图与资产预览：`sandbox/thumbnail.ts` 的 `sharedGPU()`（`direct`），`renderThumbnail` 与 `library/thumbnail.ts` 的 `renderMaterialThumbnail` 改为 async、经 `capture`、丢失时重建设备重试一次，`room` 改为 `gpu.cache`；`SceneEditor.tsx` 改为 await；`preview/ModelPreview.tsx` 用 `gl` 工厂（`direct`）。完成判据：烘焙模型、存入资产库后卡片缩略图正常（透明背景），资产查看浮窗的 3D 预览正常

## 7. 设备丢失、探测与退回

- [ ] 7.1 探测：`probeRender()` 接入 `SceneEditor.tsx`（探测期间加载态，失败显示现有「设备不支持」提示）和 `host/launcher.ts`（`probe` 改为 async）；兼容模式设备的工作台关 MSAA、加 `SMAANode`。先写单测：launcher 在 `probe` 为 false 时不认领。完成判据：单测绿，`creative-scene-agent.test.ts` 跟随 async 的 `probe` 后仍绿
- [ ] 7.2 设备丢失：视口 `canvasEpoch` 重建（10 秒内第 3 次停止并提示刷新）；`SceneExecutor` 遇 `DeviceLostError` 返回 `abandoned`，经 `pageExecutor.ts` 通知 launcher `deviceLost()`，暂停认领并在唤醒时（间隔 ≥ 10 秒）试建设备，成功后恢复；手动导出显示「渲染设备暂时不可用，请重试」。先写单测：执行器放弃不调 `complete`、launcher 暂停与恢复、视口重建计数。完成判据：单测绿

## 8. 删除旧管线与静态检查

- [ ] 8.1 `scripts/creative-scene-shading.test.ts`：TypeScript AST 检查标识符、导入路径、drei 白名单，字符串里查 GLSL 特征，fork 豁免用写死的路径清单；内置违规样例和「注释里提到不报」样例。删除残余的 WebGL 写法（`lookAlongZ`、`EffectComposer`、`three-stdlib` 的 `Sky` 等），跟随修改其余单测。完成判据：`make check-frontend` 通过；把一个违规样例临时放进 `frontend/src` 时检查失败（只在本地验证，不提交）

## 9. 验收证据

- [ ] 9.1 迁移对照 e2e（`SCENE_E2E_GPU`，Metal 参数）：六组参考场景 × `clay`、`line`、`lit` + 正交白模，对 R0 `baseline/` 算平均差与差 > 8 的像素比例并按阈值断言，并排图和差异图写进 `discovery/research/parity/`；同一用例加 `forceWebGL` 一轮，与 WebGPU 的图比（AC-R1-1、AC-R1-4 画面）。首次跑通后按实测值的 1.5 倍收紧阈值（不超过 Epic 上限）；超阈值的图交 owner，不放宽
- [ ] 9.2 确定性与退回 e2e：自启 Chrome 后台标签页连续两次导出（3 台摄像机 × 3 种方式）逐像素相同，并与前台同一张图相同（AC-R1-3）；`--use-angle=swiftshader` 下退回 WebGL2 能打开、编辑、导出且无原始错误；headless 默认参数下显示「设备不支持」且不认领（AC-R1-4 功能）
- [ ] 9.3 设备丢失 e2e（AC-R1-5）：视口丢失后恢复且选中、视角、已进 outbox 的编辑保留；`render_view`、`export_shots` 丢失后回队并最终完成（缩短租约）；`export_shots` 第 2 张之后丢失时一张都不上传；手动导出丢失后提示、画布无图、重试成功；资产卡片缩略图丢失后重试成功
- [ ] 9.4 性能与外观收尾：`SCENE_E2E_PERF` 重跑 AC-9 性能用例，与 `baseline/perf.json` 逐项对比（含加灯、删灯最长一帧）并写进 evidence（AC-R1-7）；四种显示模式 × 编辑器辅助截图对照迁移前（AC-R1-6）；定向回归交互、C4 导出、C5 校准、C5 AC-6、参考场景用例；尝试兼容模式设备，Safari 26.5 人工打开并导出一次，结果与已知限制（含提交点残余窗口）写进 `discovery/research/acceptance.md`（AC-R1-8）
