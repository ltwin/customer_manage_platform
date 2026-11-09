# Tasks

## 1. 后端：数据、求值与命令

- [x] 1.1 求值器与测试向量：先写 `testdata/keyframe_eval.json`（首前外推、尾后外推、线性、四元数最短路径、近似相同四元数退化、阶跃、离散属性、空通道），再写 `evaluate.go:EvaluateAt` 和 `evaluate_test.go`；`Makefile` 的 `generate` 把测试向量复制到 `frontend/src/creative-scene/timeline/keyframe_eval.gen.json`，`generate-check` 比对。完成判据：Go 测试全绿，`make generate-check` 通过
- [x] 1.2 类型与校验：`model.go` 新增 `Timeline`、`Marker`、`Channel`、`Key` 与 `Object.Animation`，解码时补齐 `timeline` 缺省值和 `interp`；新增 `channels.go`，包含通道白名单（所在单元、允许的 `kind`、值类型、连续或离散，经 `make generate` 输出 `channels.gen.json` 给前端）和值校验（把关键帧值写进基础值副本，复用现有的 `Camera`、`Transform` 校验）；`Validate` 调用 `validateTimeline`、`validateAnimation`。先写坏样例单测：路径不在白名单、与 `kind` 不符、帧号不递增或越界、四元数偏差过大、离散属性写 `linear`、`focus_object_id` 不可对焦、标记 ID 或帧号重复、摄像机引用不对、超过 200 个分镜、超过 1000 或 20000 个关键帧
- [x] 1.3 命令与单元：`ops.go` 新增 `setTimelineOp`、`objectUpdate.Animation`（`clear` → `delete` → `set`）和 `add_objects` 的 `animation`，按路径计算涉及的单元，`changeSet` 记录本批写到的通道和分镜列表是否改动；`type.go:Apply` 应用后调用 `checkShotKeys`；`Units` 列出 `timeline`；`restore.go` 支持 `timeline` 单元，物件单元按路径恢复通道。先写测试：P8 拒绝（带 `missing` 详情）与通过、`restore` / `restore_units` 不检查、按单元恢复通道不碰另一个单元、改关键帧与改同一属性的基础值冲突、已有文档首次写 `timeline` 用 `expected: null`
- [x] 1.4 指纹、导出与 OpenAPI：`api/openapi.yaml` 加 `timeline`、`animation`、`set_timeline`、导出的 `marker_id`，运行 `make generate`；`exports.go` 支持 `marker_id`（先求值，再取摄像机、`lensOf`、指纹，标题用分镜名）；`creativecanvas` 的 `SceneExportBasedOn` 加 `MarkerID`、`Frame` 并在 `export_source` 带出；`fingerprint.go:Freshness` 按分镜或第 0 帧求值。先写测试：现有指纹样例在求值前后逐字节相同（A8）、只改别的分镜、改布景、挪分镜、换绑等价摄像机、删除分镜、混用 `marker_id` 与 `camera_object_id` 返回 400；加基准测试（2000 物件、200 分镜、20000 关键帧，单次求值 < 5 ms，结果写回 design 未决事项）

## 2. 后端：助手

- [x] 2.1 客户端任务：`jobs.go` 的 `RenderRequest` 加 `MarkerID`、`Frame`，`ExportRequest` 加 `MarkerIDs`；建任务时的缺省规则（有分镜取全部分镜，没有就取全部摄像机）、未绑定摄像机直接报错、`marker_id` 与 `depth_of_field` 的条件；`jobs_complete.go` 的导出节点带 `marker_id`。先写测试覆盖以上分支
- [x] 2.2 读写工具：`agent.go` 的 `read_scene` 支持 `shot`，返回 `timeline`、`animated`、`shot_key_gaps`；`edit_scene` 支持 `shot`，不带分镜改有通道的属性时报错；新增 `agent_shots.go`，负责 `add_shot`（S2a / S2b）、`update_shot`（S3）、`remove_shot`（S4）、`only_in_shot`（S6）的展开和 `keyedUpdates`（S1），展开放在冲突基线计算之前；`lookat.go`、`reparent.go` 带 `shot` 时按求值后的场景计算；`creativeagent/scene_tools.go` 更新参数和说明。先写测试：每种展开后满足 P8、报错信息、带 `shot` 的 `look_at`、冲突覆盖展开后的单元、`render_view` / `export_shots` 的 `shot` 参数

## 3. 前端：求值与状态

- [x] 3.1 TS 求值器：`timeline/evaluate.ts`、`timeline/channels.ts`（读取 `channels.gen.json`），`evaluate.test.ts` 跑 `keyframe_eval.gen.json`；`api.ts` 导出 `Timeline`、`Marker`、`Channel`。完成判据：与 Go 同一份向量全部通过
- [x] 3.2 状态层：`state/apply.ts` 的 `applyOp` 支持 `set_timeline`、`animation`，`updateKeys`、`placementKeys`、`contentKeys` 按路径归属；`state/intents.ts` 的 `touchedUnits` 按路径归单元，`mergeUpdate` 只合并两边都只有 `set` 的 `animation`；`state/undo.ts` 的 `patchFor` 支持 `timeline` 单元和按单元的通道；`state/sceneStore.ts` 加 `frame`、`unkeyed`、`autoKey`、`baseline`。先写单测：「先 set 后 clear」不合并且保存后通道被清除、撤销删除分镜后关键帧回来、两个单元的通道各自恢复
- [x] 3.3 关键帧规则：`timeline/keys.ts` 的 `firstKey`（任意帧，带基线或无基线）、`addShot`（S2a）、`duplicateShot`（S2b）、`moveShot`、`removeShot`、`clearChannel`、`onlyInShot`。先写单测：生成的操作经 `applyOp` 应用后满足 P8（TS 版检查）、新建分镜取基础值、复制分镜取被复制分镜、只属于某个分镜的物件不进新建分镜

## 4. 前端：编辑器

- [x] 4.1 求值与编辑分流：`SceneEditor.tsx` 在 `shown` 之后插入 `evaluateAt` 并叠加 `unkeyed`（按 `(view, frame)` 记忆化）；`edit()` 前加编辑分流，并管理基线的记录、使用和失效（`state/sync.ts` 收到他人修订时清空对应物件的基线）；`cameraToView`、`addCamera`（`editing.ts:localPose`、`editing.ts:newCamera`）和 `grouping.ts` 的三处 `worldOf` 改用求值后的物件，改挂有变换通道的物件时提示。先写单测：基线的五种失效、没有基线时按 I、父级有动画时的 `cameraToView`、编组保持当前画面的世界位置
- [x] 4.2 时间线面板与交互：`timeline/TimelinePanel.tsx`（帧标尺、分镜标记的新建、复制、拖动和删除，当前帧指针，选中物件的关键帧点，自动关键帧开关）放在 Viewport 与 StatusBar 之间；点分镜时跳帧并进入它的摄像机视图；`input/keymap.ts` 加 I 和 Alt+I（在分镜帧上提示改用清除动画）；`panels/Properties.tsx` 显示求值后的值、关键帧点的三种配色、右键菜单（插入关键帧、清除动画）；物件菜单加「只在当前分镜显示」。e2e（一次一条，用 `ONLY` 过滤，换端口）：AC-1、AC-2、AC-2b、AC-3、AC-5、AC-6、AC-7、AC-9

## 5. 前端：导出与渲染端

- [x] 5.1 按分镜导出：`panels/ExportDialog.tsx` 有分镜时按分镜列出，导出前处理未记录的改动（三个选项）；`host/exportShots.ts` 按分镜求值，按出图场景键分组；`host/render.ts:openShooter` 与 `host/ShotScene.tsx` 增加 `setScene`（场景变了就重建加速结构、GI 失效后重新收敛；只换摄像机时清零累积）；`render.ts:renderView` 支持 `marker_id` / `frame`；`host/executor.ts:exportPlan` 支持 `marker_ids`。先写单测：出图场景键（同一瞬间不同机位同键；只动灯光或体积物件时不同键；摄像机下挂物件时，摄像机移动则不同键）。e2e：建 3 个分镜 → 分镜 2 移动并插帧 → 按分镜导出（AC-10）→ 改分镜 2，只有分镜 2 过时（AC-11）→ 有未记录改动时导出（AC-8b）；另跑旧场景导出不变（AC-12）

## 6. 收尾

- [ ] 6.1 owner 试用与助手样例：在本机请助手完成 AC-13 的样例，用真实模型跑两次并记录结果；请 owner 试用编辑语义，定下自动关键帧的默认值；把结论写回 design 的未决事项
