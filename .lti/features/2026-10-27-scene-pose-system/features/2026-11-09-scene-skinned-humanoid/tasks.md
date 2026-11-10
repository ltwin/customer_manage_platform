# Tasks

## 1. 后端：骨架、rig 与模型命令

- [x] 1.1 OpenAPI：`CreativeModelManifest` 加 `skeleton`；`CreativeModelPayload`、`CreativeModelPreviewPayload`、`CreativeSceneModel` 加 `rig`；`CreativeSceneModelRef` 改为 `content_revision_id` / `builtin` 二选一；`CreativeSceneSetInstanceModel` 的 `model` 加 `builtin`，并加可选的 `material_slots`；`CreativeAssetFromGLBPayload` 加 `rig`；`/creative/scene-models/{file}` 路径随 2.1 的 handler 一起加；`CreativeFieldDetails` 加可选的 `reason`；`/creative/scene-textures/{file}` 的 pattern 与媒体类型。运行 `make generate`。完成判据：`go build ./...` 与前端 `tsc -b` 通过，读模型引用的前端调用点先按类型收窄（界面留给 5.1）
- [x] 1.2 `creativeglb` 的蒙皮子集：先写测试，蒙皮子集的每条规则各一个反例（joint 越界或不在场景树、缺逆绑定矩阵、带 skin 的节点没有 mesh、网格同时被带 skin 与不带 skin 的节点引用、`JOINTS_0` 越界、权重越界或和不为 1、`JOINTS_1`、关节超限）；根节点带旋转、网格节点自身带变换时，蒙皮包围盒与手算结果一致；`Skeleton` 的并集、父关节和 `s1` 指纹；`Matches` 比对骨架。然后实现 `skin.go`，修改 `glb.go`；前端 `sandbox/inspect.ts` 同步镜像（共用 `testdata/cases.json`，两侧须同时通过，故从 4.1 提前）。现有 `creativeglb` 测试与 `scripts/creative-scene-model.test.ts` 全绿
- [x] 1.3 `creativescene/rig.go` 与共用测试向量 `testdata/humanoid_rig.json`（`Makefile` 的 `generate` / `generate-check` 复制到前端）。向量覆盖：穿刺①的五个角色与两个素体的骨架；缺必需骨骼；重复映射；脚挂在控制骨下；可选骨骼越级；约束的源不是关节；源越界；父级受约束的 aim；成环。先写读向量的 Go 测试，再实现 `Validate`。向量由暂存区脚本从原文件抽取骨架后生成，每例给 `valid` 与第一条问题的 `field`、`reason`（`unknown_bone` / `missing_required` / `not_joint` / `duplicate_node` / `hierarchy` / `invalid` / `limit` / `source_is_node` / `cycle`）；`creativeops.FieldError` 加 `Reason`，`abortFieldError` 填 `details.reason`
- [ ] 1.4 模型命令与导入：
  - `import.go` 收 `rig`，按 design「服务端的骨架与 `rig` 校验顺序」复核。
  - `commitModelOp` 拒绝带骨架的 GLB。
  - `deriveModelOp` 照抄 `Rig`，并拒绝带骨架的来源。
  - `library.go` 的库副本照抄 `Rig`。
  - `setInstanceModelOp` 接受 `builtin` 与 `material_slots`（整体替换）。
  - 先写测试，每个分支各一例（含 AC-4 的 400 与 AC-10）。

## 2. 后端：内置模型

- [ ] 2.1 内置模型库与贴图库放宽：
  - `builtin_textures.go`：`.png`、`quaternius`、`listed`、`character`；没有 `url` 的文件从目录导入。
  - 新增 `builtin_models.go`：清单与缩略图的嵌入、加载校验、`scene-models/` 存储、`Open` / `Missing` / `Import`。
  - `config.go` 的 `SCENE_MODEL_LOCAL_ROOT`。
  - `httpapi/creative_scene_models.go` 与路由注册；`cmd/server/main.go` 的后台缺失检查。
  - `creativectl models import/check` 与 `textures import --dir`。
  - `Makefile` 复制 `models.json` 与模型缩略图。
  - 先写测试：清单校验的失败例；handler 的 200、304、404、503（AC-12）；import 核对 sha256、`Inspect` 和 manifest，不一致时不写入。
  - 用一个测试条目跑通，素体条目在 6.1 加入。
- [ ] 2.2 场景引用与助手：
  - `validateModelRef`、`modelRefs`、`readModels`、`opEnv.resolve`、`setInstanceModelOp.targets` 认得 `builtin`。
  - 有效槽位取清单的 manifest（`materials.go`）。
  - `faces.go`、`jobs.go`、`library.go`、`agent.go` 的 `write_model` 对内置模型拒绝；`ModelView` 带 `builtin`。
  - 助手：`builtinSearch` 支持 `kind: model`，并过滤 `listed: false`；`readBuiltinAsset` 支持内置模型；`edit_scene` 的说明与 schema 更新；`editScene` 遇到内置模型而没写材质时补上条目的 `materials`（添加和换模型都补）。
  - 先写测试：AC-9、AC-11（现有指纹测试不变，没有内置模型的场景指纹不变），以及以上各个拒绝分支。

## 3. 前端：骨骼核心（`rig/`）

- [ ] 3.1 `bones.ts`、`humanoid.ts`、`detect.ts`、`retarget.ts`、`constraints.ts`：
  - 由穿刺原型整理而来。
  - 先写测试：
    - 跑共用测试向量。
    - 识别五个角色（VRM 1.0 / 0.x、Mixamo、UE 命名、RobotExpressive 失败与手动成立）。
    - 把穿刺①的数值检查改写成测试：4 组姿态的肢体方向误差 < 1°；合成变体误差 < 0.01°；握拳。
    - 约束：父级受约束时，aim 的方向与 three-vrm 参考实现的结果一致。
    - 骨架几何条件的四种退化夹具。
- [ ] 3.2 `template.ts`、`skin.ts`、`pose.ts`：
  - 骨架模板与实例克隆；按 design 的坐标空间做 CPU 蒙皮与静态化几何。
  - 部件 ID 取 `n<节点>p<primitive>`；姿态哈希。
  - 缓存：256 MiB 字节预算，在用的钉住，其余按最近使用淘汰并释放。
  - `__sceneTest.setPose`。
  - 先写测试：
    - 坐标空间三种情况（根节点旋转、网格节点自身变换、同模型两个不同实例），three 光栅顶点与静态化几何逐顶点一致。
    - 部件身份：两个网格各有 primitive 0；同一网格不同 skin。
    - 缓存的淘汰与钉住。

## 4. 前端：导入带骨骼的角色

- [ ] 4.1 解析、换算与写出：
  - `parse.ts`：放行蒙皮；关节上限；读 VRM 扩展与约束（去掉源不是关节的约束）；有多组影响时从原始访问器裁剪，压缩过又有多组影响的 primitive 拒绝。
  - `convert.ts`：保留关节和挂在关节下的对象，保留 `skinIndex` / `skinWeight`；带骨骼的模型改为平移祖先节点来移动原点。
  - `write.ts`：回写节点下标并删去标记。
  - `sandbox/host.ts` 拒绝带 `skins` 的代码模型产物（`sandbox/inspect.ts` 的蒙皮子集已在 1.2 完成）。
  - 先写测试：
    - 多组影响的最小模型经完整解析链路后，保留第 4–7 根骨骼（AC-5）。
    - AC-5b：合成夹具（根节点旋转、多个 skin、两组影响、原点移到底面中心）写出再解析，静止姿势与非平凡姿态下逐顶点位置、法线、包围盒一致。
    - 节点下标回写后与识别出的映射一致；导出物不带标记。
  - 合成夹具提交在 `frontend/scripts/fixtures/`。
- [ ] 4.2 导入对话框与提交：
  - `ImportModelDialog.tsx` 显示映射结果（骨骼数、手指、约束数、去掉的项、非均匀缩放的提示）。
  - 不成立时说明原因。
  - 新增 `RigMapping.tsx`：手动指定，即时重新校验。
  - 提供「按非人形骨架导入」。
  - `run.ts` 提交 `rig`。
  - 先写单测（RobotExpressive 手动把 `Body` 指定为髋后成立）。
  - e2e（单条）：导入 VRM 1.0 样例，放进场景后贴地、贴图正确（AC-2）。
  - 夹具许可逐个核对；不能再分发的素材在测试里下载，下载失败时只跳过这些。

## 5. 前端：渲染

- [ ] 5.1 模型键与内置模型的加载：
  - 新增 `models/key.ts:modelKey`、`models/catalog.ts`。
  - `downloadGLB` 遇到 `builtin:` 时走 `/creative/scene-models/`，503 时视口占位并提示、导出失败。
  - design 列出的查表调用点全部改用模型键（`SceneEditor.tsx`、`saveModel.ts`、`modelEdits.ts`、`host/*`、`panels/MaterialSlots.tsx`、`state/*`、`faceEdit/*`、`placement/*`）。
  - 完成判据：`tsc -b` 与 `oxlint` 无报错；现有场景相关单测和一条现有模型 e2e 照常通过（AC-11）。
- [ ] 5.2 蒙皮渲染：
  - `models/cache.ts` 分出骨架模板，合并时跳过蒙皮网格。
  - 新增 `objects/SkinnedModel.tsx`。
  - `SceneObjects.tsx` 分发。
  - `render/trace/collect.ts` 与 `base.ts:TraceBase.instances` 用（静态化几何, 模型根矩阵）。
  - 拾取、吸附、取景、`Outline.tsx` 用同一组数据。
  - 导出与 `render_view` 等静态化几何和段重建完成；构建期间视口在追踪层剔除这个角色。
  - 预览与资产缩略图按静止姿势显示。
  - 先确认导出线稿的取几何路径（design 未决事项），需要时改为静态化几何。
  - 测试：合并与 `traceKey` 的单测（AC-7）。
  - e2e（单条）：`setPose` 后，视口、实时 GI 镜中、`lit` 导出三处的手部位置一致，包围盒更新（AC-6）；`__sceneNoMerge` 开与关的画面一致。

## 6. 内置素体与收尾

- [ ] 6.1 素体内容：
  - 新增 `frontend/scripts/builtin-models/convert.mjs`：读来源包，修正贴图引用，材质改名为 `body` / `eyes` / `brows`，走导入的换算与写出，输出 GLB、PNG 贴图和清单片段。
  - `render-texture-thumbnails.mjs` 加模型模式。
  - 把两个素体写进 `models.json`，贴图材质写进 `materials.json`（`listed: false`、`character`），提交缩略图。
  - `creativectl textures import --dir`、`models import --dir` 导入共享对象存储，`check` 通过。
  - 文档：`docs/dev/builtin-models.md`、`docs/dev/scene-texture-library.md`。
  - 完成判据：服务启动时清单校验通过；两个素体的 `rig` 都是 52 根骨骼。
- [ ] 6.2 素体的界面与验收：
  - `SceneEditor.tsx` 加「添加 > 素体」；`AssetPanel.tsx` 加「素体」分区。
  - `editing.ts:newModelInstance` 改为接收模型引用。
  - `ModelParams.tsx`：内置模型的信息块，以及「换成男素体 / 女素体」（带目标材质）。
  - 带骨骼的模型和内置模型隐藏面分配、存入资产库、改参数。
  - 选材面板与资产库的内置来源过滤 `listed: false`。
  - e2e（单条）：AC-1，含换素体与撤销。
  - e2e：把 GLB 请求改成 503，检查占位与导出失败（AC-12）。
- [ ] 6.3 实测与 owner 材料：
  - AC-8：在实时 GI 下给素体换一个姿态，记录 CPU 蒙皮、静态化、段重建、GI 收敛到可导出的耗时。
  - 一个接近上限的导入角色的加载与静态化耗时。
  - 结果写进 design。
  - AC-1、AC-2、AC-6 的截图放进 `discovery/research/`，交 owner 看图。
