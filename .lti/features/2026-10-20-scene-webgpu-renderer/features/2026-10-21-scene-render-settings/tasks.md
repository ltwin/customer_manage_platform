# Tasks

先做不确定性最大的实时 GI 管线（局部未决 L1–L3），再接场景格式、选项卡和出图。分支上各任务之间允许功能暂时不完整（例如 GI 管线先只能经测试参数打开），但每个任务结束时 `tsc -b`、`oxlint`、相关单测和 `go test` 都通过。

## 1. 实时 GI 管线

- [ ] 1.1 `render/nodes/SSGINode.js`（+ `.d.ts`，黄金比例采样、直接输出 `vec4(GI, AO)`）、`render/giSplit.ts`、`render/realtimeGI.ts`，`SceneRenderer` 的 `Frame.engine` / `quality` 与 `keyOf` 扩展（此时只经测试参数打开）。先写单测：SSGI fork 守护（换回上游块后逐字相同）、`giSplit` 守护（无标记时着色代码与不包装时逐字相同）、合成代数（常量纹理）。完成判据：盒子间的 `lit`、`clay` 实时 GI 出图有渗色和反射、没有均匀提亮；结清 L1（MRT 读 property、三附件建管线，Metal 与 `forceWebGL` 都试）与 L3（递归降噪接法），不成立时按 design 的退路实现并记在本任务下
- [ ] 1.2 SSR 命中覆盖率（结清 L2）、接触阴影的主光源选择、`final` 档关掉 `flickerSuppression`、实时 GI 的环境光补偿（`litLook.ts`、`Lighting.tsx`，`room` 与盒子间平均亮度与工作台相差 ≤ 10%）；GI 管线释放。完成判据：连续导出 48 张实时 GI 图后 GPU 纹理数不增长（e2e，计数同 R1 的灯光用例）；`forceWebGL` 下盒子间与 WebGPU 在 AC-R1-1 阈值内

## 2. 场景格式与后端

- [ ] 2.1 `creativescene`：`Render` / `renderPatch` 新字段、`decodeScene` 补缺省、`completedScene` 补齐 `render`、`newScene`、`Validate`、`set_render` 提示、`fingerprintRender.Engine`；`creativemedia` 的 `MaxImagePixels`。先改测试：`TestRenderMergesByKey` 的期望、老场景（SQL 剥字段）读取补齐且写入成功、固定 v1 指纹不变、`engine` 只影响 `clay` / `lit`、`resolution` 不影响、`path_tracing` / 未知档位 / `null` 被拒。完成判据：`make check-go PKG=./internal/creativescene/...`、`./internal/creativemedia/...` 绿
- [ ] 2.2 `api/openapi.yaml` 两个 schema 加字段，`make generate` 提交 Go 与 TS 生成物；前端读 `render` 的地方去掉对缺字段的猜测，改用 `engines.ts` 的取值函数。完成判据：`make generate-check`、`tsc -b` 绿

## 3. 渲染选项卡与有效渲染器

- [ ] 3.1 属性区三个选项卡（`SceneEditor.tsx`、`sceneStore.ts`、`scene.css`）与 `panels/RenderPanel.tsx`；`EnvironmentPanel` 去掉曝光；后端提示读 `SceneGPU`。完成判据：e2e「渲染选项卡」用例绿（内容、路径追踪禁用、自动切换、只读预览禁用），现有曝光用例改到新位置后仍绿
- [ ] 3.2 `render/engines.ts` 的 `effectiveEngine` 接入视口（实时 GI 时 1× 像素比、正交角标）和离屏渲染端（`render_view`、导出）；视口收敛（`LookView` 区分自己续的帧）。先写 `engines.ts` 决策表单测。完成判据：e2e 用例绿——换渲染器后视口、`render_view`、`clay` / `lit` 导出一致，撤销、两个标签页同步、过时提示三种情况（AC-R2-2）；线框、材质预览、`line` 与工作台逐像素相同、正交角标（AC-R2-4）；实时 GI 视口帧计数停在 N、工作台停在 1

## 4. 累积与输出分辨率

- [ ] 4.1 `gpu.settle()`、异步 `shoot` 与 K 帧累积（`SceneRenderer` 的累积节点、`host/render.ts`）、`exportShots.ts` / `executor.ts` 逐张 await 与按估算的时限（`engines.ts`）。先写单测：时限公式、帧之间超时、`ManualTicker` 跟随新时限。完成判据：隐藏标签页里实时 GI 的多摄像机三种方式连拍两次逐像素相同、与前台一致（`creative-scene-agent-background.e2e.mjs`）；8K 实时 GI 导出期间续租照常（日志或钩子计数）
- [ ] 4.2 输出分辨率：`framing.ts` 长边按档位、`imageTypeOf` 与上传文件名 / MIME、纹理上限检查、4K / 8K 丢设备以 `host_error` 结束且手动导出提示、`ExportDialog` 显示档位。完成判据：e2e 四档 × 1:1 / 3:2 / 竖幅的尺寸与格式、8K 1:1 能上传成画布节点、压低纹理上限的失败、4K 丢设备不回队（AC-R2-6）

## 5. 标定

- [ ] 5.1 在参考设备（Metal）上标定 N、K（导出与 `render_view`）、`final` 质量档、TRAA 的 `flickerReduction`、累积目标精度，方法见 design「收敛」「累积与出图」；常量写进 `engines.ts`，曲线与图存进 `discovery/research/calibration`。完成判据：残余噪点阈值写进 AC-R2-5 的 e2e 并通过；2K 实时 GI 一张的耗时记录在案

## 6. 助手

- [ ] 6.1 `scene_tools.go` 三个工具的说明、`instructions/base.md`、`scene_tools_test.go`；`docs/dev/agent-skills-tools.md` 工具表与排空后升级记录、`docs/dev/creative-scene-agent.md`。完成判据：`make check-go PKG=./internal/creativeagent/...` 绿；助手脚本化回合 e2e「换成实时 GI，按 4K 导出」绿（AC-R2-7）

## 7. 验收证据

- [ ] 7.1 AC-R2-3 效果用例：盒子间的区域测量（红墙旁地面红绿比、金属球与工作台的差、接触处亮度）写进 e2e，图存进 evidence；切回工作台对 R1 冻结参考图；SwiftShader 的 WebGL2 下实时 GI 出图不报错（AC-R2-8 后半）。完成判据：用例绿，evidence 齐
- [ ] 7.2 AC-R2-9 性能：实时 GI 视口帧时间（盒子间、R0 性能场景，1× 像素比）、停止操作到停止重绘的时间、四档两种渲染器的导出耗时、工作台视口帧时间对 R1。完成判据：数据存进 `discovery/research/perf`，不满足的逐项列出交 owner；结清 L4（8K 实时 GI）
- [ ] 7.3 AC-R2-10 重绘对照：同机位工作台与实时 GI 各导出一组，用 gpt-image-2.5 重绘（凭证只经环境变量，拿不到时交 owner）；AC-R2-11 Safari 人工检查。完成判据：样例与记录存进 evidence，等 owner 结论
