# Tasks

## 1. 契约

- [x] 1.1 `api/openapi.yaml`：新增 `POST /creative/documents/{id}/scene-exports`（`CreativeSceneExportPayload`/`CreativeSceneExportResult`）；`export_shots` 的任务请求与结果分支；完成任务的 `export` 改为 `{scene_revision, shots}`；`render_view` 请求加 `depth_of_field`；摄像机加 `depth_of_field`、`f_stop`、`focus`、`focus_object_id`；`CreativeCanvasNode.based_on_state`。`make generate` 后 Go/TS 生成物更新，`make check-go` 与 `make check-frontend` 编译通过

## 2. 场景格式与 `look_at`

- [x] 2.1 `creativescene/model.go`：`Camera` 景深字段、取值校验（光圈 0.5–64、`focus > 0`）、`completeCamera`/`mergeCamera`（`focus_object_id` 写空字符串取消）、`decodeScene` 补齐；`view.go` 有摄像机缺字段时输出补齐后的场景。先写测试：缺省补默认值、按键合并、越界 400、旧快照读取带齐字段
- [x] 2.2 `creativescene/ops.go`：`add_objects`/`update_objects` 写到 `focus_object_id` 时校验目标（存在、类别为基础体/模型实例/分组、不是自己）。测试：非法目标 400；对焦物件之后被删除，删除、撤销、按单元恢复照常成功
- [x] 2.3 `creativescene/lookat.go` + `agent.go:AgentEditInTx`：`resolveLookAt` 与向量/四元数数学，换算后去掉 `look_at`。测试：three.js 金样例（根级、旋转缩放过的分组、目标在正上/正下方、摄像机/灯光/普通物件）逐项比对；与 `rotation` 同写、目标重合、同一命令父级被移动或改挂均 400

## 3. 导出放置与过时判断（后端）

- [x] 3.1 `creativecanvas`：`Action.Config`（`json:"-"`）与 `add_node` 应用；`scene_exports.go` 的 `PlaceSceneExportsInTx`（锁内拓扑修订、位置计算、`add_node` + `fill_from_upload` 写 `based_on`）与 `DocumentNodeInTx`。测试：6 张图一个 `change_id`、3 行 2 列、第二次排在下方；画布命令 `add_node` 带 `config` 仍 400（E3）；归档 409、暂存不可用 409
- [x] 3.2 `creativescene/fingerprint.go`：投影规则与 `Fingerprint`。表驱动测试覆盖设计「验证与交付」的过时/不过时/景深各行，旧快照补齐前后指纹相同
- [x] 3.3 `creativescene/exports.go`：`ExportInput`/`ExportResult`、`placeExportsInTx`、`creative.scene_export` 操作；`httpapi/creative_documents.go` 路由与错误映射。测试：摄像机不在修订里 400、49 张 413、跨账号 404、场景在回收站 409、同一 operation id 与 `client_created_at` 回放同一组节点
- [x] 3.4 过时判断端口：`ExportFreshness`/`SetExportFreshness`、`fillNodeViews` 收集生效 `based_on` 并入 `SummaryBatch`、`Node.BasedOnState` 与 `MarshalJSON`、`cmd/server/main.go` 注册。测试：`current`/`stale`/`unavailable`/不返回四种情况，指纹版本不符时按 `scene_revision` 重算；画布与 httpapi 画布测试回归通过；记录 `GetCanvas` 查询次数与耗时（200 物件、6 张导出图）

## 4. 客户端任务与助手工具（后端）

- [x] 4.1 `creativescene/jobs.go`：`executableKinds` 加 `export_shots`、`ExportRequest`、`JobResult.Export`、`CreateExportJobInTx`（找画布上的场景节点、定摄像机、≤ 48 张）；`RenderRequest.DepthOfField` 与 `CreateRenderJobInTx` 的校验。测试：场景节点不在画布报错；`depth_of_field` 配 `viewpoint`/`preset` 或摄像机没开景深时报错且不建任务；不带参数时请求不变
- [x] 4.2 `creativescene/jobs_complete.go`：`ExportOutput{SceneRevision, Shots}`、`validate` 与 `completeExport`（保存点内放置，放置失败转 `invalid_result`）。测试：一个事务里节点、任务成功、运行继续；`Admit` 不通过与令牌失效不建节点；暂存不可用整体回滚；场景节点移除或画布归档 `invalid_result`；`shots` 与请求不符 400；换手后只有一组节点；烘焙与截图完成测试不变
- [x] 4.3 `creativeagent/scene_tools.go`、`tool_registry.go`：`export_shots@1` 工具与结果；`render_view` 的 `depth_of_field` 参数与说明；`edit_scene` 说明（摄像机全部参数、`look_at`、长边视场角、景深与对焦、空物体、先回看再导出）。工具测试通过

## 5. 前端：光学、渲染与执行

- [ ] 5.1 `cameraOptics.ts`（视场角、对焦距离、弥散圆、清晰范围、等效焦距、传感器预设）；`host/framing.ts` 与 `CameraFrustum` 改按长边；`state/apply.ts` 的 `cameraDefaults` 加景深字段。单元测试：3:2 与现在相同、2:3 长边视场等于 3:2、清晰范围约 2.74–3.32 m、APS-C 35 mm 约 53.6 mm、对焦距离夹在 near/far 之间、弥散圆上限
- [ ] 5.2 `render/clayExport.ts`、`render/dofPass.ts`、`render/linePass.ts`；`host/render.ts` 一次导出共用离屏根、`longSide` 参数、`toBlob` 异步编码、`render_view` 带景深时走白模导出流程。用样例场景定线稿阈值、GTAO 参数、景深采样（未决 #1），写成常量
- [ ] 5.3 `host/exportShots.ts`（出图、并发 2 上传、失败丢弃、手动提交复用 `operation_id` 与 `client_created_at` 的重试）；`api.ts:exportScene`；`host/executor.ts`、`host/jobs.ts` 执行 `export_shots` 经 `deliver` 完成、`render_view` 传 `depth_of_field`。单元测试覆盖失败丢弃与重试

## 6. 前端：编辑器与画布

- [ ] 6.1 `editing.ts:newCamera`/`newEmpty`；`SceneObjects.tsx` 对焦十字与清晰范围短线、没有子物件的分组常显可点选十字；`SceneEditor.tsx` 添加菜单（摄像机、空物体）。单元测试覆盖 `newCamera`、`newEmpty`
- [ ] 6.2 `panels/Properties.tsx` 摄像机属性：标签、焦距、画幅、高度；景深开关、光圈快捷值、对焦（距离/物件列表/视口拾取）、清晰范围、对焦物件已删除的提示；「更多」里传感器预设、宽高与等效焦距、`near`/`far`；滑块 300ms 合并
- [ ] 6.3 相机视图：`Viewport.tsx` 取景框、视场角放大、构图辅助线、预览景深、退出条件；`input/keymap.ts` 的 `camera-view`/`camera-to-view`；`state/sceneStore.ts`、`viewState.ts` 的 `cameraView`、`lastCamera`、辅助线选择。单元测试覆盖相机视图视场角公式
- [ ] 6.4 `panels/ExportDialog.tsx`（摄像机全选、方式、张数、0 张与 48 张上限）；`SceneEditor.tsx` 导出入口与 `canvasID`/`sceneNodeID`；`WorkspacePage.tsx` 传参；`CanvasView.tsx`「场景已更新」标记。`make check-frontend` 通过

## 7. 文档与验收

- [ ] 7.1 `docs/dev/agent-skills-tools.md`（工具变化、排空后升级）、`docs/dev/creative-scene-agent.md`（长边视场角、景深回看）
- [ ] 7.2 `scripts/creative-scene.e2e.mjs`：添加摄像机 → 相机视图 → 导出 → PNG 尺寸与画布节点；空物体对焦 + 景深预览 + 导出虚化对比；传感器预设与辅助线不进导出图。`scripts/creative-scene-agent.e2e.mjs`：`export_shots`；助手写光圈与对焦物件，`render_view` 默认清晰、带 `depth_of_field` 虚化。记录出图、编码、上传与景深耗时
- [ ] 7.3 AC-9 重绘一致性样例：3 台摄像机导出，景深关、开各一组，记录到 `discovery/research/repaint-consistency.md`，结论由 owner 给出
