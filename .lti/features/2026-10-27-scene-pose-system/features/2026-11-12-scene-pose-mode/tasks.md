# Tasks

## 1. 后端：存储、通道与兼容检查

- [ ] 1.1 OpenAPI 与生成物：`CreativeScenePose`、`CreativeSceneBonePose`，对象与 `update_objects` 的 `pose`（更新里值可为 `null`）。worktree 里直接跑 `oapi-codegen` 与 `npm run generate`。完成判据：`go build ./...` 与前端 `tsc -b` 通过
- [ ] 1.2 `pose.go`（`Pose`、`BonePose`、`validatePose`、`normalizePose`、`mergePose`）与 `model.go:Object.Pose`、`validateObject`、`ops.go:objectUpdate` 的合并、`restore.go`。先写测试：键规则（标准骨骼名要人形映射、`node:` 下标在骨架里、`position` 只在 `hips`、512 键上限）、长度偏差 1e-3 的拒绝、归一化与 w ≥ 0、小数位、静止项去掉、`null` 重置一根骨骼、空 `pose` 去掉字段
- [ ] 1.3 骨骼通道：`channels.json` 三行路径模板与 `rig` 列（`Makefile` 复制到前端），`channelOf` 按模板解析、`loadChannels` 一致性检查、`validateAnimation` 只许 `model_instance`；`keyframe_eval_gen.py` 加骨骼用例并再生成向量；`EvaluateAt` 克隆 `Pose` 并写入通道值；`fingerprint.go` 计入求值后的 `Pose`（省略空值，已有场景指纹不变的测试，AC-12）。先写测试，Go 读共用向量全绿
- [ ] 1.4 `checkPoses` 与 `type.go:sceneType.Apply`：只查这批改到的实例，新旧模型各读一次（含内置模型），按 design 的判定表（无骨架、非人形、关节不在骨架、指纹变化）返回 400 `validation_failed` 与最多 50 条 `details.incompatible`。先写测试：男女素体互换通过（AC-9）、换到非人形骨架未清除时 400、同批清除时通过、未改模型的实例不读

## 2. 前端：数据、求值与渲染

- [ ] 2.1 前端数据与求值：`timeline/channels.ts` 模板解析、`objectPaths`、`baseValue` / `withBaseValue` 的 `pose` 分支与骨骼中文标签（`pose/names.ts`）；`keys.ts:written`、`routeEdit` 按骨骼合并；`evaluate.ts:evaluateObject` 骨骼通道；`state/apply.ts` 的 `updateObject`（与 Go 同一套规范化）、`contentKeys`、`changedPart`；`intents.ts:mergeUpdate`。先写测试：共用向量的骨骼用例前后端一致；本地投影与服务端快照逐字节一致；S1–S6 编辑语义对骨骼通道成立（新建、复制、挪动、删除分镜）
- [ ] 2.2 姿态求值与渲染：`rig/pose.ts` 的 `poseFromScene`、`pose()` 支持骨盆位移与 `node:` 旋转，`testPoses` 只作测试覆盖层；`rig/skin.ts` 的 `poseHash` 覆盖合成后的输入、新增 `skinInto`；`SkinnedModel.tsx` 以场景姿态为来源。完成判据：单元测试覆盖骨盆位移与 `node:` 键；子项 3 的蒙皮 e2e 不回归

## 3. 前端：模式轮盘

- [ ] 3.1 `modes/modes.ts`（`modesFor` 决策表、模式切换）与 `modes/ModeWheel.tsx`（手势状态机：按住指向松开进入、按住不指向松开不切换、轻按常亮后点选或再按 Tab 关闭、Esc、失焦；不支持的模式不显示，方向固定）；`keymap.ts` 的 Tab 改为 `mode-wheel`、删除 `face-edit` 动作；`SceneEditor.tsx` Tab 分支；`faceEdit/state.ts` 由模式切换进入与退出；`Viewport.tsx` 顶栏模式下拉。先写单元测试（决策表、状态机各分支）；面编辑的 e2e 改为从轮盘进入后逐个重跑通过

## 4. 前端：姿态模式

- [ ] 4.1 进入、骨骼显示与选择：`sceneStore` 的 `mode`、`poseMode`；`pose/state.ts`、`BoneOverlay.tsx`、`pick.ts`（12 px 屏幕空间拾取）、A / Alt+A、Shift 加选；`PosePanel.tsx` 列出选中骨骼并按现有配色标关键帧状态；姿态模式下其他物件不可选
- [ ] 4.2 FK：`rig/poseEdit.ts` 反向换算（标准骨骼与 `node:` 键，父先于子）与 `pose/fk.ts` 接入 `transform/modal.ts` 的 R 模态（锁轴、局部轴、数值输入、取消）；`pose/live.ts` 拖动时的实时值，确认时写一条场景修改。先写测试：换算往返误差、父子同选与 Blender 结果一致；Cmd+Z 只撤销一次（AC-2、AC-7、AC-11）
- [ ] 4.3 IK 与骨盆：`pose/ik.ts` 的 `solveTwoBone`（弯曲平面在拖动开始时固定、够不着时伸直、手保持局部旋转）与骨盆位移；手、脚按 G 或直接拖动，`hips` 按 G 移动，其他骨骼提示只能旋转。先写测试：可达、不可达、贴近伸直时不抖（AC-3）
- [ ] 4.4 剪贴板与清除：`pose/clipboard.ts` 复制、粘贴、镜像粘贴（人形）；`pose/clear.ts` 的 Alt+R、Alt+G；I / Alt+I 给选中骨骼插入、删除关键帧。先写测试：镜像的四元数与位移公式、左右骨骼对应（AC-4、AC-5、AC-6）

## 5. 换模型与助手

- [ ] 5.1 换模型：`pose/compat.ts`（与 Go 同一张判定表）与 `modelEdits.ts`，换模型时在同一批命令里写出清除，并提示摄影师；`SceneEditor.tsx` 接入。先写测试：两侧判定一致的表驱动用例；Cmd+Z 恢复原模型和原姿态（AC-9）
- [ ] 5.2 助手：`agent.go` 的 `ObjectView.Pose`、`objectView`、`AgentEditInTx` 的 `clearIncompatiblePoses`、`EditResult.PosesCleared`；`creativeagent/scene_tools.go` 的说明（骨骼通道只能清除、`poses_cleared`）。先写测试：`read_scene` 带姿态与骨骼通道、指定分镜时为求值结果（AC-13）；换模型时自动补清除

## 6. 拖动时的追踪层

- [ ] 6.1 refit 与剔除：`capacity.ts` 的 `REFIT_TRIANGLES`；`build.worker.ts` 的代理段带 `sources`、`cache.ts` 的 `BUILDER_VERSION` 升到 5；`TraceLayer.refitSegment`；`collect.ts` 的键冻结、`liveVersion` 与超预算剔除；`base.ts:sync` 发起 refit、松手后在重建提交前沿用 refit 的结果；`gpuTestHooks.poseDrag` 计时。先写测试：refit 后的包围盒与 three-mesh-bvh 重建的结果一致（含代理段）
- [ ] 6.2 预算实测：用 2 万、5 万、8 万三角形的角色测 `poseDrag`，以主线程每帧 ≤ 8 ms 定预算，结果写进 design「AC-8 实测」；e2e `pose-drag-trace` 覆盖贴墙、镜面、自遮挡三个场景并截图给 owner 判断（AC-8）

## 7. 验收

- [ ] 7.1 e2e 逐个运行（`SCENE_E2E_ONLY`，Vite 端口 5199）：`pose-mode-wheel`、`pose-fk`、`pose-ik`、`pose-shots`、`pose-base`、`pose-node`；AC-10 快照大小的测试记录实测值；AC-12 旧场景的画面与过时状态不变
