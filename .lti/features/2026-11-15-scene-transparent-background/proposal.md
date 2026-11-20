---
risk:
  factors: [perf-sensitive, broad-impact]
behavior: changed
reads: [creative/scene]
decisionsPending: false
---

## Why

导出图常要拿去合成（换背景、叠到海报或画布素材上），现在白模和材质光照导出总带着天空，还得手动抠图。需要像 Blender「Film › Transparent」一样：开启后导出图的背景透明，但天空、太阳、环境给物体的光照和反射保持不变。

## What Changes

- 「渲染」选项卡新增场景设置「透明背景」（缺省关闭），挂在输出分辨率旁边；和其他渲染设置一样静默保存、可撤销、多标签页同步，助手可读可改。助手的导出工具（`export_shots`）另支持可选的 `transparent_background` 参数，按次直接指定是否使用透明背景导出，不改写场景文档，也不受场景「透明背景」开关或其他标签页切换开关的影响（未传时沿用导出时刻场景的「透明背景」设置）。
- 透明背景导出时，白模和材质光照导出图里相机直接看到天空的像素（天空、纯色背景、太阳圆盘、夜空的星星和月亮）变为透明；物体照旧由天空光、太阳、月光、环境反射照亮，金属和镜面里映出的天空保留。线稿不受影响，仍是白底。
- 透明度随覆盖率过渡：抗锯齿边缘（含兼容模式无 MSAA 下的暗色剪影与实时 GI 运动前景边缘）、景深虚化后的边缘是半透明的，不带天空色或黑色描边；半透明材质（玻璃等）和体积物件保留场景内的物理透射、折射与表面反射，并对空背景输出供单通道 W3C `source-over` 外部合成使用的近似标量透明度（光滑玻璃后方完全落在不透明物体内部的区域与关闭时一致，磨砂玻璃跨越物体与天空轮廓的采样带平滑过渡为半透明，不对外部新背景做折射变形或分通道彩色透射吸收）。
- 透明背景下，白模和材质光照在 1K 至 8K 均导出无损 PNG。分镜原图单张预算为 320 MiB，普通图片上传为 25 MiB。场景开启「透明背景」且勾选白模或材质光照时，在长边旁显示背景提示。
- 视口：最终渲染模式下背景显示棋盘格，与材质光照透明导出一致；白模、材质预览保持原有的中性灰背景（不显示棋盘格，而白模导出在开启透明背景时为透明底），线框不变。
- 画布：含透明像素的导出图在画布上透出底色。服务端为大图生成的展示图现在一律是 JPEG、会丢掉透明，改为对含透明像素的原图保留透明。
- 渲染指纹计入透明背景：按场景设置导出时，切换场景「透明背景」后已导出的白模、材质光照图提示「场景已更新」；助手按 `export_shots(transparent_background)` 显式指定导出的节点在 `based_on` 记录该次设置，过时判断按该值计算，不受场景「透明背景」开关切换影响。关闭时指纹与现在完全相同，旧图不会因本次上线变为过时。

## Acceptance Criteria

### 设置

- WHEN 打开属性区「渲染」 THEN 看到「透明背景」开关，旧场景显示关闭；打开后 Cmd+Z 回到关闭，另一个标签页同步显示。
- WHEN 场景「透明背景」关闭时请助手「导出透明背景的材质光照图」（或导出期间另一标签页切换场景「透明背景」开关） THEN 助手直接带 `transparent_background: true` 调用 `export_shots` 导出透明背景图，场景开关保持原值。切换场景开关不触发这张图的过时提示，渲染后发生的机位、几何等相关变动照常触发提示。请助手「把场景的透明背景打开」时，助手通过 `set_render` 修改场景设置，这次修改出现在本轮改动清单里。
- WHEN 客户端写入非布尔值的透明背景 THEN 场景命令被拒绝（400），场景不变。

### 导出画面

- GIVEN 物理天空的院子场景，地面上一个白色立方体和一个金属球，打开透明背景 WHEN 导出白模和材质光照 THEN 两张都是 PNG（2K），天空所在像素解码后 alpha = 0，物体内部像素 alpha = 255；立方体的明暗与阴影、金属球里映出的天空，与关闭透明背景时导出的同一张图在物体区域内逐像素一致（允许实时 GI 的噪声差异，平均差 < 1/255 量级，阈值在 design 定）。
- GIVEN 同一场景 WHEN 导出线稿 THEN 线稿与关闭透明背景时完全相同（白底）；导出对话框勾选白模或材质光照时写明「白模、材质光照使用透明背景」，只勾选线稿时不显示该提示。
- GIVEN 打开透明背景 WHEN 导出（含兼容模式下的深暗色物体剪影、实时 GI 下前景运动露出空背景，以及摄像机打开景深） THEN 物体边缘和景深虚化边缘的过渡像素都是半透明且无历史残影，把图叠到纯白和纯黑底上都看不到天空色或黑色描边。
- GIVEN 场景里一块玻璃板一部分后面只有天空、一部分后面有不透明物体（含光滑玻璃与磨砂玻璃），以及空背景前的有色体积物件 WHEN 打开透明背景导出材质光照 THEN 只有天空的玻璃区域与有色体积区域均呈半透明（0 < alpha < 255，保留体积散射色与玻璃表面反射），光滑玻璃后方完全落在不透明物体内部的区域照常显示后面的物体（alpha = 255），磨砂玻璃跨越物体与天空轮廓的过渡带呈平滑半透明（0 < alpha < 255）且无天空色硬边。
- GIVEN 夜晚的物理天空，有星星和月亮 WHEN 打开透明背景导出材质光照 THEN 星星、月亮不出现在图里，月光对物体的照明保留。
- GIVEN 透明背景关闭（且助手导出未显式指定 `transparent_background: true`） WHEN 导出 THEN 输出与本次改动前完全相同（格式、画面、文件大小量级）。
- GIVEN 输出分辨率 4K 或 8K、打开透明背景 WHEN 导出白模和材质光照 THEN 原图保留所选分辨率、以无损 PNG 编码并保留像素透明度。超过普通图片上限的原图按单张 320 MiB 分镜预算上传。设备、上传或额度不足导致失败时说明原因，整批不落位。

### 画布与过时提示

- GIVEN 一张 2K 或 4K 透明背景导出图放到画布上 WHEN 查看节点和打开大图 THEN 透明背景处透出底色，不是黑色或白色。
- GIVEN 按场景设置已导出一组白模、线稿和材质光照图 WHEN 切换场景「透明背景」 THEN 这组白模、材质光照图提示「场景已更新」，线稿不提示；再切回原值后不再提示。
- GIVEN 本次上线前导出的图 WHEN 上线后读取画布 THEN 不因本次上线变为「场景已更新」。
- GIVEN 上线前打开的旧页面提交白模或材质光照图，场景修订要求透明背景或助手为该任务显式指定了背景模式 WHEN 页面未报告实际采用的背景模式 THEN 服务端拒绝整批落位并提示刷新页面后重新导出。

### 视口

- GIVEN 打开透明背景 WHEN 视口切到最终渲染 THEN 背景处显示棋盘格，物体光照与关闭时一致；切到白模、材质预览时保持各自原有的中性灰背景（不显示棋盘格）。

## 范围与非目标

- 助手回看（`render_view`）不受透明背景影响：仍是带背景的 JPEG，给模型看的画面不变。
- 不做 Blender 的单独「透明玻璃」开关、阴影捕捉（shadow catcher）和多通道（AOV）输出；单张 RGBA 导出按 W3C `source-over` 标量透明度合成，不对外部新背景做折射变形或分通道彩色透射吸收；需要地面阴影投在透明背景上属于阴影捕捉，另开变更。
- 雾和空气透视只作用于物体，与现在一致（天空背景本来就不受影响），透明背景处不残留雾色。

## Impact

- 代码：
  - 前端 `frontend/src/creative-scene/`：`render/sceneRenderer.ts`、`render/realtimeGI.ts`、`render/giSplit.ts`、`render/dof.ts`、`render/gpu.ts`、`render/volumes/composite.ts`、`world/atmosphere/aerial.ts`（预乘 alpha、玻璃与雾按渲染通道上下文门控、SMAA/TRAA 透明边缘与历史钳制、景深单字节混合附件）、`host/render.ts`、`host/exportShots.ts`、`host/executor.ts`、`render/engines.ts`（编码、按次透明覆盖与时限）、`panels/ExportDialog.tsx`、`panels/RenderPanel.tsx`、视口棋盘格。
  - 后端 `backend/internal/creativescene`（`render` 字段与 `encodeScene` `false` 省略存储、导出任务按次 `transparent_background` 覆盖、落位核对渲染端回报、`based_on.transparent_background` 与渲染指纹）、`backend/internal/creativecanvas/scene_exports.go`（`SceneExportBasedOn`）、`backend/internal/creativemedia/verify.go`（含透明像素原图的展示图保留透明）、`backend/internal/creativeagent`（`export_shots` 的 `transparent_background` 参数与 `set_render` 说明）。
- 契约：OpenAPI `CreativeSceneRender` 新增必返 `transparent_background: boolean`（旧场景与省略存储的快照读出 `false`），`CreativeSceneRenderPatch` 新增可选 `transparent_background: boolean`；`data: 场景文档`、`data: 场景客户端任务`、`data: 导出图来源`、`api: 读取场景`、`api: 助手场景工具的输入与写入结果`、`api: 导出分镜`、`api: 完成场景客户端任务`、`patterns: 场景着色写法` 同步扩展；`make generate` 生成 Go/TS 两份产物。4K/8K 透明导出使用无损 `image/png` 并适用单张 320 MiB 的场景导出上传预算。
- 外部：无。

## Risk Notes

- perf-sensitive：最终画面多带一路 alpha 穿过 AO、空气透视、体积、景深（透明模式下附加单通道 `R8` 混合权重附件，关闭时零新增显存）、SMAA/TRAA 和累积；导出时限按编码估算（`engines.ts estimateMs`），8K 正方形 + 景深 + PNG 编码的显存、耗时与文件大小需在管线完成后尽早核对；画质须与关闭时等价（不透明物体内部区域逐像素对照）。
- broad-impact：`SceneRenderer` 同时服务视口、导出和助手回看，玻璃与雾的着色钩子须严格按透明渲染通道的上下文门控并隔离着色器缓存键，确保关闭透明背景和 `render_view` 编译出原样着色器；展示图编码改动作用于所有上传的含透明像素 PNG/WebP（顺带修正画布展示图透明变黑问题），不影响 JPEG 和全不透明图的现有展示图。
- 指纹：透明背景只在有效值为 `true` 时计入指纹，保证存量导出图不批量变为过时、指纹版本不升级；助手按次指定 `transparent_background` 的导出节点把该值记入 `based_on`，与场景开关解耦。
- 数据：场景快照为 `false` 时省略该键，旧后端可解码该修订。关闭开关只影响新写入的修订，仍保留的 `true` 历史修订继续适用前向修复策略。前后端与 worker 同版本上线，已打开的旧页面由导出落位校验兜底。
