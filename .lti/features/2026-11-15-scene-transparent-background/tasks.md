# Tasks：3D 场景透明背景导出

## 1. 契约与生成物

- [ ] 1.1 修改 `api/openapi.yaml`：`CreativeSceneRender` 新增必填 `transparent_background: boolean`，`CreativeSceneRenderPatch` 与 `CreativeSceneSetRender` 新增可选 `transparent_background: boolean`；在 `CreativeSceneExportPayload` 与 `CreativeSceneJobCompletePayload.export` 增加条件必传的 `effective_transparent_background: boolean` 并注明失败语义；运行 `make generate` 生成 `backend/internal/platform/httpapi/api.gen.go` 与 `frontend/src/api/schema.d.ts`，`make generate-check` 通过

## 2. 后端：场景设置、任务覆盖、渲染指纹、媒体展示图与助手工具

- [ ] 2.1 `creativescene` 设置、`encodeScene` 精简落库与读取补齐：`model.go` 的 `Render` 新增无 `omitempty` 的 `TransparentBackground bool`、`encodeScene` 经私有 `persistedRender` 在值为 `false` 时省略 `"transparent_background"` 键、`renderPatch` 新增 `TransparentBackground *bool`；`view.go` 的 `storedRender` 新增 `TransparentBackground *bool` 并纳入 `complete()`，`completedScene` 改为直接 `json.Marshal(s)` 保证读取接口始终返回布尔值；`ops.go` 更新 `set_render` 空补丁守卫与 `apply`；在 `scene_test.go` 先红后绿覆盖旧快照与 `false` 新快照落库省略键但读取返回 `false`、`true` 落库与切回 `false` 省略键、非布尔/`null`/空对象返回 400、以及 `restore_units` 撤销恢复，串行运行 `cd backend && go test ./internal/creativescene/ -run TestScene -count=1` 通过
- [ ] 2.2 导出任务覆盖、回报校验与指纹：`jobs.go` 保存显式覆盖，`jobs_complete.go` 接收回报并把覆盖与回报传给 `exports.go:placeExportsInTx`。手动接口只接收回报。按实际渲染修订校验有效值，通过后再消费暂存图和写入节点。缺少必传回报或值不匹配时按接口契约返回刷新提示。扩展 `SceneExportBasedOn` / `ExportSource` 记录显式覆盖，落位用实际渲染修订计算指纹，`Freshness.Check` 用当前修订并把有效值纳入缓存键。覆盖存量 `v1:` 哈希、开关可逆、显式 `false`、落位前改机位立即 `stale`、旧请求兼容、明确回报 `null` 或非布尔值被拒、回报拒绝后零节点且暂存图未消费。按影响面运行 `creativescene`、`creativecanvas` 与相关 HTTP 测试
- [ ] 2.3 `creativemedia` 透明展示图：`verify.go` 新增 `imageOpaque` 像素不透明度检查，在长边 `> 1600 px` 生成 `Display` 渲染件时对 `out.Mime != "image/jpeg" && !imageOpaque(decoded)` 用 `png.Encode` 输出 `image/png`，其余保持 `jpeg.Encode`（`Quality: 86`）；在 `service_test.go` 覆盖 `> 1600 px` 透明 PNG、透明 WebP、全不透明四通道 PNG 与 JPEG 四类样本，运行 `cd backend && go test ./internal/creativemedia/ -count=1` 通过
- [ ] 2.4 `creativeagent` 独立透明导出参数与上下文预算：`scene_tools.go` 为 `export_shots` 增加可选 `transparent_background: boolean` 参数及透传逻辑，精简更新 `set_render`、`export_shots`、`render_view` 说明与 `instructions/base.md`（总增量控制在 `300 B` 以内）；在 `scene_export_tools_test.go` 覆盖场景开关关闭时 `export_shots(transparent_background: true)` 导出透明任务、以及 `edit_scene(set_render)` 记入 `RunChanges`「渲染设置」，运行 `cd backend && go test ./internal/creativeagent/ -run 'TestSceneExport|TestCompaction' -count=1` 确认工具与压缩预算测试均通过

## 3. 前端：编码策略、任务透传、离屏捕获与导出链路

- [ ] 3.1 编码与时限纯函数：`engines.ts` 将 `ImageType` 扩为 `'image/png' | 'image/jpeg' | 'image/webp'`，更新 `imageTypeOf(pass, resolution, transparent)`（`1k`/`2k` 为 PNG，`4k`/`8k` 的 `clay`/`lit` 在透明时为 WebP `0.95`，`line` 保持 PNG）、`extensionOf`（支持 `'webp'`）、`encodeNs['image/webp'] = 120` 与 `estimateMs` / `shotBudgetMs`
- [ ] 3.2 任务覆盖、离屏捕获与错误兜底：`gpu.ts` 的 `canvasBlob` 识别 `toDataURL('image/webp', ...)` 回退 PNG 的不支持情形并抛出可区分错误；`host/executor.ts` 在 `exportPlan` 中校验并透传 `request.transparent_background` 至 `ExportPlan.transparentBackground`；`host/render.ts` 开启离屏渲染器 `alpha: true`、为 `ShotOptions` 增加 `transparent?: boolean` 与 `ImageType`、在 `shoot` 中按 `use !== 'view' && kind !== 'line'` 采用 `options.transparent ?? (scene.render.transparent_background === true)` 并向 `capture` 转发 WebP `quality`；`host/exportShots.ts` 按 `plan.transparentBackground ?? (doc.scene.render.transparent_background === true)` 决定编码与拍摄，返回实际采用的背景模式（仅线稿为 `false`），在手动导出与任务完成中均提交 `effective_transparent_background`；执行器收到明确失败回执时丢弃本批暂存图。修复 `uploadShots` 的 `image/webp` MIME 透传，并对 WebP 不支持及单图 `> 25 MiB` 返回带降档建议的 `hostError: true`；更新 `creative-scene-camera.test.ts`、`creative-scene-agent.test.ts`、`creative-scene-gpu.test.ts`、`creative-scene.test.ts` 并运行对应 `node --test` 通过

## 4. 前端：3D 渲染管线、Pass 级上下文隔离与 8K 早期验证

- [ ] 4.1 透明 pass 与玻璃：`giSplit.ts` 新增 `transparentContext`，用独立的 `GILightingPass.transparentBackground` 控制去底和上下文，保留继承的 `PassNode.transparent` 半透明队列。`transparentTransmission.ts` 局部移植 Three `0.186.1` 的 `PhysicalLightingModel.start` 及透射采样依赖，仅在透明 pass 的透射材质启用，从同一次采样读取原始 `B_a`，保留透射 RGB、粗糙度、色散及既有光照调用，再由 `finish` 计算高光覆盖率。`gpu.ts` 按同一标记门控未混合透射材质的雾色预乘。补上游结构守卫、开启态生成的 WGSL 核对，以及同材质 `off → on → off` 的关闭态 WGSL 等价检查
- [ ] 4.2 景深（`RGBA16F + R8`）、体积、雾与空气透视、辅助层与四维 TRAA：`dof.ts` 在 `transparent = false` 时保持单附件零增量，在 `transparent = true` 时使用 `RGBA16F` + `RedFormat/UnsignedByteType`（`R8`）半分辨率双附件 MRT 混合完整 `vec4`；`volumes/composite.ts` 更新穿过体积处的覆盖率 alpha；`world/atmosphere/aerial.ts` 与 `realtimeGI.ts` 将大气散射与线性雾色乘以像素 alpha、修复视口 `aside` 叠加层 Porter-Duff `over` alpha，并在透明模式下使用 `render/nodes/TransparentTRAANode.js`（含类型声明），子类继承上游历史资源管理，只移植 `setup` 并让本地 resolve 调用四维 `clipAABB`。加上游结构守卫和开启态实际编译检查，验证全透明邻域直接归零
- [ ] 4.3 `SceneRenderer` 串联、预编译同步与四通道 SMAA：`sceneRenderer.ts` 在 `Frame`/`Look`/`keyOf`/`shapeOf` 中加入 `transparent`；`TransparentScenePass` 保存和恢复 `clearColor + clearAlpha` 与 `transparentContext`，`asItsPass` 预编译包装同步注入同一上下文；透明模式排除 `SKY_LAYER = 29`；串联 `aerialBehind`、`applyVolumes`、`dof` 的 `vec4` alpha；透明模式下包装 `smaa.setup` 将边缘检测扩为 `max(r, g, b, a)`（并在 `creative-scene-gpu.test.ts` 加 `SMAANode` 结构守卫）；修复 `pipeline.outputNode` 保留 `output.a` 及视口 `overlay` 的 Porter-Duff `over` alpha
- [ ] 4.4 8K 方形+景深与 WebP 编码早期实测（解决 `U1`）：在真浏览器离屏管线实测 4K 与 8K 方形（`8192 × 8192`）开启景深的透明 WebP `0.95` 出图，核对解码像素透明度、显存稳定性/`DeviceLostError` 降档提示与 `toDataURL` 单像素耗时（`ns/px`），必要时据此校准 `engines.ts` 的 `encodeNs['image/webp']` 并回填 `design.md` `U1`

## 5. 前端：渲染面板、导出对话框与视口棋盘格

- [ ] 5.1 编辑器界面与视口联动：`RenderPanel.tsx` 在输出分辨率旁新增「透明背景」开关与说明文案；`SceneEditor.tsx` 与 `ExportDialog.tsx` 在开启透明背景且勾选了 `clay` 或 `lit` 时于长边旁显示「`· 白模、材质光照使用透明背景`」、仅勾选 `line` 时隐藏；`Viewport.tsx` 开启 `alpha: true`，仅在 `display === 'rendered'` 时联动 `transparent` 重绘并切换 `.cc-scene-viewport.is-transparent`（`solid` 与 `preview` 保持原有中性灰底），在 `scene.css` 添加中性暗色棋盘格样式；运行 `make check-frontend` 通过

## 6. 浏览器端到端验收与文档收尾

- [ ] 6.1 场景编辑器 GPU 验收：在 `frontend/scripts/creative-scene.e2e.mjs` 新增透明背景用例，用 `SCENE_E2E_ONLY` 逐条串行运行并核对定量判据（物理天空院子 2K 白模/材质光照/线稿逐像素对照、常规与景深边缘叠白/黑底无描边、`forceWebGL`/无 MSAA 下 `< 0.1` 暗色斜边叠白底抗锯齿平滑、RealtimeGI 运动前景露出空背景无残影、清玻璃与粗糙玻璃跨轮廓及彩色体积 `source-over` 表现、夜晚星月透明与月光保留、关闭时与原输出一致（含 `alpha_mode: blend` 物件）、4K 透明 WebP 导出与不支持/超限错误提示、导出对话框仅线稿隐藏提示、开关与 `Cmd+Z` / 多标签页同步 / `rendered` 棋盘格与 `solid`/`preview` 中性灰底不变）
- [ ] 6.2 助手跨标签页免疫与全栈画布媒体验收：在 `frontend/scripts/creative-scene-agent.e2e.mjs`（用 `SCENE_AGENT_E2E_ONLY` 串行运行）验证 `render_view` 与关闭透明背景像素一致且为带背景 JPEG、场景开关关闭或另一标签页并发关闭时 `export_shots(transparent_background: true)` 仍产出透明图且仅忽略场景开关引起的过时；用旧请求外形完成显式覆盖任务应失败，刷新后重新发起只落位一组图、`edit_scene(set_render)` 出现在本轮改动「渲染设置」；在 `frontend/scripts/creative-media.e2e.mjs` 验证 2K 透明 PNG 与 4K 透明 WebP 上传落位画布后服务端生成 `1600 px` PNG `display` 渲染件，查看节点与大图预览均透出深色底而非纯黑或纯白；同步更新 `docs/dev/creative-scene-agent.md`
