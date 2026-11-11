# Tasks

## 1. 后端：骨架、rig 与模型命令

- [x] 1.1 OpenAPI：`CreativeModelManifest` 加 `skeleton`；`CreativeModelPayload`、`CreativeModelPreviewPayload`、`CreativeSceneModel` 加 `rig`；`CreativeSceneModelRef` 改为 `content_revision_id` / `builtin` 二选一；`CreativeSceneSetInstanceModel` 的 `model` 加 `builtin`，并加可选的 `material_slots`；`CreativeAssetFromGLBPayload` 加 `rig`；`/creative/scene-models/{file}` 路径随 2.1 的 handler 一起加；`CreativeFieldDetails` 加可选的 `reason`；`/creative/scene-textures/{file}` 的 pattern 与媒体类型。运行 `make generate`。完成判据：`go build ./...` 与前端 `tsc -b` 通过，读模型引用的前端调用点先按类型收窄（界面留给 5.1）
- [x] 1.2 `creativeglb` 的蒙皮子集：先写测试，蒙皮子集的每条规则各一个反例（joint 越界或不在场景树、缺逆绑定矩阵、带 skin 的节点没有 mesh、网格同时被带 skin 与不带 skin 的节点引用、`JOINTS_0` 越界、权重越界或和不为 1、`JOINTS_1`、关节超限）；根节点带旋转、网格节点自身带变换时，蒙皮包围盒与手算结果一致；`Skeleton` 的并集、父关节和 `s1` 指纹；`Matches` 比对骨架。然后实现 `skin.go`，修改 `glb.go`；前端 `sandbox/inspect.ts` 同步镜像（共用 `testdata/cases.json`，两侧须同时通过，故从 4.1 提前）。现有 `creativeglb` 测试与 `scripts/creative-scene-model.test.ts` 全绿
- [x] 1.3 `creativescene/rig.go` 与共用测试向量 `testdata/humanoid_rig.json`（`Makefile` 的 `generate` / `generate-check` 复制到前端）。向量覆盖：穿刺①的五个角色与两个素体的骨架；缺必需骨骼；重复映射；脚挂在控制骨下；可选骨骼越级；约束的源不是关节；源越界；父级受约束的 aim；成环。先写读向量的 Go 测试，再实现 `Validate`。向量由暂存区脚本从原文件抽取骨架后生成，每例给 `valid` 与第一条问题的 `field`、`reason`（`unknown_bone` / `missing_required` / `not_joint` / `duplicate_node` / `hierarchy` / `invalid` / `limit` / `source_is_node` / `cycle`）；`creativeops.FieldError` 加 `Reason`，`abortFieldError` 填 `details.reason`
- [x] 1.4 模型命令与导入：
  - `import.go` 收 `rig`，按 design「服务端的骨架与 `rig` 校验顺序」复核。
  - `commitModelOp` 拒绝带骨架的 GLB。
  - `deriveModelOp` 照抄 `Rig`，并拒绝带骨架的来源。
  - `library.go` 的库副本照抄 `Rig`（库副本原样复制载荷，无需改代码，只补测试）。
  - `setInstanceModelOp` 接受 `material_slots`（整体替换，与 `update_objects` 同样只校验外形与贴图）；`builtin` 的解析依赖 2.1 的清单，随 2.2 的 `validateModelRef` / `targets` 一起做。
  - 先写测试，每个分支各一例（含 AC-4 的 400 与 AC-10）。

## 2. 后端：内置模型

- [x] 2.1 内置模型库与贴图库放宽：
  - `builtin_textures.go`：`.png`、`quaternius`、`listed`；没有 `url` 的文件从目录导入。`character` 品类本身是 `materials.json` 的数据，随 6.1 的贴图条目一起加（现在加会让资产浏览器多出一个空分组）。
  - 新增 `builtin_models.go`：清单与缩略图的嵌入、加载校验、`scene-models/` 存储、`Open` / `Missing` / `Import`。
  - `config.go` 的 `SCENE_MODEL_LOCAL_ROOT`。
  - `httpapi/creative_scene_model_files.go` 与路由注册（与已有的 `creative_scene_models_test.go` 区分开）；handler 依赖 `SceneModelFiles` 接口，测试不依赖清单条目；`cmd/server/main.go` 的后台缺失检查。
  - `creativectl models import/check` 与 `textures import --dir`。
  - `Makefile` 复制 `models.json` 与模型缩略图。
  - 先写测试：清单校验的失败例；handler 的 200、304、404、503（AC-12）；import 核对 sha256、`Inspect` 和 manifest，不一致时不写入。
  - 用测试条目跑通：清单校验用共用向量里 xbot 的骨架和映射拼出的条目，导入用 `creativeglb/testdata/skinned-turned-root.glb`；仓库里的 `models.json` 暂为空，素体条目在 6.1 加入。
  - 文档先写了 `docs/dev/builtin-models.md` 与贴图库的 `--dir`（`.env.example` 引用它），6.1 补素体的生成步骤。
- [x] 2.2 场景引用与助手：
  - `validateModelRef`、`modelRefs`、`readModels`、`opEnv.resolve`、`setInstanceModelOp.targets` 认得 `builtin`。
  - 有效槽位取清单的 manifest（`materials.go`）。
  - `faces.go`、`jobs.go`、`library.go`、`agent.go` 的 `write_model` 对内置模型拒绝；`ModelView` 带 `builtin`。
  - `set_instance_model` 的 `builtin`（从 1.4 移来）；助手路径（`resolveSlots`）核对 `set_instance_model.material_slots` 的槽位名属于新模型。
  - 助手：`builtinSearch` 支持 `kind: model`，并过滤 `listed: false`；`readBuiltinAsset` 支持内置模型；`edit_scene` 的说明与 schema 更新；`editScene` 遇到内置模型而没写材质时补上条目的 `materials`（添加和换模型都补）。
  - 先写测试：AC-9、AC-11（现有指纹测试不变，没有内置模型的场景指纹不变），以及以上各个拒绝分支。
  - 实施记录：
    - 仓库清单暂为空，测试经 `creativescene.UseBuiltinModelsForTest` 换上 `testdata/builtin_models.json` 的两个测试素体（xbot 骨架）。
    - `derive_model` 与 `write_model` 的 `base_content_revision_id` 写内置模型 ID 时，`creative_model_unavailable` 的消息说明它是内置模型；面分配随 `derive_model` 一并拒绝，`faces.go` 无需改动。
    - `search_assets(source: builtin)` 不写 kind 时材质与模型都列出；内置模型缩略图的图片定位用 `builtin-model:` 前缀，与内置材质的 `builtin:` 分开。
    - 读场景的模型摘要补上 `rig`（1.1 已加进 OpenAPI 的 `CreativeSceneModel`，1.4 漏了读取）。
    - 工具说明约增加 1 KB，压缩回归测试的窗口相应调大。

## 3. 前端：骨骼核心（`rig/`）

- [x] 3.1 `bones.ts`、`humanoid.ts`、`detect.ts`、`retarget.ts`、`constraints.ts`：
  - 由穿刺原型整理而来。
  - 先写测试：
    - 跑共用测试向量。
    - 识别五个角色（VRM 1.0 / 0.x、Mixamo、UE 命名、RobotExpressive 手动修改后仍不成立）。
    - 把穿刺①的数值检查改写成测试：4 组姿态的肢体方向误差 < 1°；合成变体误差 < 0.01°；握拳。
    - 约束：父级受约束时，aim 的方向与 three-vrm 参考实现的结果一致。
    - 骨架几何条件的四种退化夹具。
  - 实施记录：
    - 测试在 `scripts/creative-scene-rig.test.ts`。夹具 `scripts/fixtures/rig/skeletons.json` 只含穿刺用的 7 个角色的节点树（名称、局部变换、skin 关节、VRM 人形与约束扩展），不含网格、材质和贴图；每个角色记了来源。
    - `prepare` 的结果（朝向 F、T(n)）在模型根空间里算，不含根节点自己的变换，同一模型的实例可以共用；合成变体加了一例「模型根在场景里平移、旋转、缩放」，误差同样 < 0.01°。
    - 名字识别：命名表要认出髋骨才采用。RobotExpressive 的 `Head` 与 UE 命名表同名，不加这条会被误认成 UE 骨架。
    - `pruneOptional` 在提交前去掉服务端会拒绝的可选骨骼（不是关节、与其他骨骼同节点、违反层级嵌入），必需骨骼留给 `checkRig` 判定；`checkRig` 的顺序、字段、原因和消息与 `rig.go` 一致。
    - 约束的求值分三步：`resetConstraints` → `apply` → `runConstraints`，由 3.2 的 `pose.ts` 组合。依赖排序与 `rig.go` 的成环判定同一规则，共用向量的成环用例同时覆盖。
    - three-vrm 没有引入为依赖：aim 的公式按它的实现写，测试核对父级受约束时 aim 轴指向源，并验证不按依赖排序时会偏。
- [x] 3.2 `template.ts`、`skin.ts`、`pose.ts`：
  - 骨架模板与实例克隆；按 design 的坐标空间做 CPU 蒙皮与静态化几何。
  - 部件 ID 取 `n<节点>p<primitive>`；姿态哈希。
  - 缓存：256 MiB 字节预算，在用的钉住，其余按最近使用淘汰并释放。
  - `__scenePose.set`（原写 `__sceneTest.setPose`；`__sceneTest` 是 e2e 页面的布尔开关，测试钩子照 `__sceneGPU` 另挂）。
  - 先写测试：
    - 坐标空间三种情况（根节点旋转、网格节点自身变换、同模型两个不同实例），three 光栅顶点与静态化几何逐顶点一致。
    - 部件身份：两个网格各有 primitive 0；同一网格不同 skin。
    - 缓存的淘汰与钉住。
  - 实施记录：
    - 测试在 `scripts/creative-scene-rig-skin.test.ts`，蒙皮 GLB 在测试里由 JSON 拼出：一个小模型覆盖坐标空间与部件身份，Xbot 的节点树加几个绑在左手上的顶点覆盖套姿态。
    - 部件 ID 要用节点下标：`sandbox/gltf.ts:parseModelGLB` 的结果加 `nodes`（对象 → GLB 节点下标），已补进 design 挂载点。
    - three 的 GLTFLoader 本来就按单位绑定矩阵、`attached` 模式绑定，与 glTF 规定一致，模板不再重新绑定；坐标空间测试核对了光栅结果（`getVertexPosition` 乘网格矩阵）与静态化几何乘模型根矩阵逐顶点一致（< 1e-5 m）。
    - 骨架模板是解析结果的拷贝，去掉不挂在关节下的静态网格（下面挂着关节的换成空节点）；`taken` 交给 5.2 的 `partsOf` 跳过。
    - 静态化几何的 uv 等属性与原几何共用数组、各用各的属性对象（three 按属性对象释放显存）；缓存按位置、法线、切线、索引和拾取 BVH 计字节。
    - 静止姿势不跑约束，得到的就是存储的模型；没有人形映射或骨架几何退化的模型，标准姿态一律按静止姿势处理。
    - 粗测：9000 个顶点、每个 4 个影响，CPU 蒙皮约 1.3 ms（Node）。

## 4. 前端：导入带骨骼的角色

- [x] 4.1 解析、换算与写出：
  - `parse.ts`：放行蒙皮；关节上限；读 VRM 扩展与约束（去掉源不是关节的约束）；有多组影响时从原始访问器裁剪，压缩过又有多组影响的 primitive 拒绝。
  - `convert.ts`：保留关节和挂在关节下的对象，保留 `skinIndex` / `skinWeight`；带骨骼的模型改为平移祖先节点来移动原点。
  - `write.ts`：回写节点下标并删去标记。
  - `sandbox/host.ts` 拒绝带 `skins` 的代码模型产物（`sandbox/inspect.ts` 的蒙皮子集已在 1.2 完成）。
  - 先写测试：
    - 多组影响的最小模型经完整解析链路后，保留第 4–7 根骨骼（AC-5）。
    - AC-5b：合成夹具（根节点旋转、多个 skin、两组影响、原点移到底面中心）写出再解析，静止姿势与非平凡姿态下逐顶点位置、法线、包围盒一致。
    - 节点下标回写后与识别出的映射一致；导出物不带标记。
  - 合成夹具提交在 `frontend/scripts/fixtures/`。
  - 实施记录：
    - 测试在 `scripts/creative-scene-import-rig.test.ts`。合成夹具是 `scripts/fixtures/rig/glb.ts`：用 JSON 拼 GLB 的工具和「角色节点树 + 绑在一根关节上的三角形」，3.2 的测试也改用它。
    - 拒绝原因 `skinned` 改为 `skin`，只用于「多组影响又经过压缩」；关节超限用已有的 `limit`。
    - 全为 0 的权重：three 的加载器会先把它们改成 (1, 0, 0, 0)，换算阶段已分辨不出，所以改在解析时从原始访问器里数，进换算报告。
    - 「原点移到底面中心」不用改做法：原本就是平移最上层节点、不改顶点。只补了一条：算包围盒前先更新世界矩阵，蒙皮网格按静止姿势计入。测试确认顶点和逆绑定矩阵不变，蒙皮结果整体平移，存储的包围盒底面为 0。
    - `writeGLB(scene, track)` 返回被跟踪对象的新节点下标；`writtenRig` 把按原文件下标的 `rig` 换成写出后的下标（4.2 起跟踪全部关节，见下）。three 导出时会去掉节点名里的 `:` 等字符，回写按标记找节点，不受影响。
    - 导入对话框里勾选原点时的复制改用 `SkeletonUtils.clone`：`Object3D.clone` 不会把蒙皮网格重新绑到复制出的骨骼上。
    - `sandbox/host.ts` 的拒绝（AC-10）测在 `creative-scene-model.test.ts`，用 `creativeglb/testdata/skinned-turned-root.glb`。
- [x] 4.2 导入对话框与提交：
  - `ImportModelDialog.tsx` 显示映射结果（骨骼数、手指、约束数、去掉的项、非均匀缩放的提示）。
  - 不成立时说明原因。
  - 新增 `RigMapping.tsx`：手动指定，即时重新校验。
  - 提供「按非人形骨架导入」。
  - `run.ts` 提交 `rig`。
  - 先写单测：RobotExpressive 手动修改后仍提示脚的问题、可按非人形骨架导入；Xbot 错映射一根骨骼后手动改回成立。
  - e2e（单条）：导入 VRM 1.0 样例，放进场景后贴地、贴图正确（AC-2）。
  - 夹具许可逐个核对；不能再分发的素材在测试里下载，下载失败时只跳过这些。
  - 实施记录：
    - 校验与提交的逻辑在新增的 `import/mapping.ts`：`checkMapping`（剪掉可选骨骼 → `checkRig` → 静止骨架的几何条件）、`fileConstraints`（服务端不收的约束整组去掉并说明）、`submittedRig`、`jointTree`、`followClone`。对话框只管状态和显示。
    - 换算后写出时跟踪全部关节，而不是只跟踪已映射的骨骼：手动改映射不用重新导出，回写总能找到节点。勾选原点时，用 `followClone` 把关节对应到复制出的场景上再写出。
    - 映射不成立时「导入」按钮不可用，提供「手动指定」和「按非人形骨架导入」；按非人形导入后可以「改回人形映射」。没有认出任何骨骼也按「不成立」处理，由摄影师二选一。
    - 单测加在 `scripts/creative-scene-import-rig.test.ts`（RobotExpressive 两种手动映射分别卡在腿和脚、按非人形导入；Xbot 错映射后改回；原点复制后的回写；约束成环时整组去掉）。
    - e2e（AC-2）移到 5.2 之后：贴地和贴图对不对，要等蒙皮网格能在场景里渲染才看得出。
    - 夹具许可：仓库里只提交节点树（名字、变换、关节表、VRM 扩展），不含网格、材质和贴图。RobotExpressive、两个 UBC 素体是 CC0；VRM1 扭转样例按 VRM Public License 1.0；Xbot、Michelle（Mixamo）与 VRM 0.x 女孩（VRoid Hub 条款）的条款对「只取骨架数据」没有明说，交 owner 判断。若不接受，改为测试里下载、下载失败时跳过。

## 5. 前端：渲染

- [x] 5.1 模型键与内置模型的加载：
  - 新增 `models/key.ts:modelKey`、`models/catalog.ts`。
  - `downloadGLB` 遇到 `builtin:` 时走 `/creative/scene-models/`，503 时视口占位并提示、导出失败。
  - design 列出的查表调用点全部改用模型键（`SceneEditor.tsx`、`saveModel.ts`、`modelEdits.ts`、`host/*`、`panels/MaterialSlots.tsx`、`state/*`、`faceEdit/*`、`placement/*`）。
  - 完成判据：`tsc -b` 与 `oxlint` 无报错；现有场景相关单测和一条现有模型 e2e 照常通过（AC-11）。
  - 实施记录：
    - `models/key.ts`：`modelKey`、`builtinOf`、`builtinKey`。`models/catalog.ts`：清单副本、条目查询、`builtinModelURL`、内置摘要表，以及 `summaryOf(models, ref)`：先查传入的表，内置模型再查目录。宿主读到的 `doc.models` 和历史预览的表都来自服务端、不含内置模型，所以取景、出图加载、物件分发都用 `summaryOf`；编辑器的表在 `modelSummaries` 和历史预览两处都并入了内置摘要。
    - 改为按模型键的：`SceneObjects.tsx`（`ModelMesh` 的键与摘要）、`host/render.ts:loadModels`、`host/framing.ts:sceneBounds`、`panels/MaterialSlots.tsx`（槽位、常量、来源槽位、部件加载）、`SceneEditor.tsx`（`modelOf`、属性栏的模型、槽位名）。
    - 只对账号修订有意义的调用点保持读 `content_revision_id`，内置模型自然被排除：改代码和参数（`modelEdits.ts`、`retune`、代码面板）、派生与面分配（`faceEdit/assign.ts`）、同步层的未知修订判断与回执换号（`state/sync.ts`、`state/intents.ts`）、`pageExecutor` 的替换查询。`placement/*` 不查模型表，无需改动。
    - 明确提示：面编辑对内置模型和 manifest 带 `skeleton` 的模型给出「带骨骼的模型和内置模型不能面编辑」（状态栏不再提示 Tab）；存入资产库对内置模型给出「内置模型不能存入资产库」。属性栏的代码模型参数区对内置模型不显示，内置模型的信息块留给 6.2。
    - `downloadGLB` 对 `builtin:` 键经 `authorizedFetch` 读 `/creative/scene-models/<file>`；503 时报「内置模型的文件暂时读不到」，走现有的加载失败占位（导出同样失败）。
    - 验证：`tsc -b`、`oxlint` 无报错；相关单测 11 个文件共 251 条通过；e2e（端口 5199，单条）「asset panel: Shift+A」和「a model that cannot be shown」通过。「a code model: 代码模型…」在改动前的提交上同样超时（等不到生成命令），与本次无关，未处理。
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
  - e2e（单条，自 4.2 移来）：导入 VRM 1.0 样例，放进场景后贴地、贴图正确（AC-2）。

## 6. 内置素体与收尾

- [ ] 6.1 素体内容：
  - 新增 `frontend/scripts/builtin-models/convert.mjs`：读来源包，修正贴图引用，材质改名为 `body` / `eyes` / `brows`，走导入的换算与写出，输出 GLB、PNG 贴图和清单片段。
  - `render-texture-thumbnails.mjs` 加模型模式。
  - 把两个素体写进 `models.json`，贴图材质写进 `materials.json`（`listed: false`、`character`），提交缩略图。
  - `creativectl textures import --dir`、`models import --dir` 导入共享对象存储，`check` 通过。
  - compose 部署（本地驱动）：仿 `scene_texture_data` 加 `scene_model_data` 卷；`production-preflight.sh` 的卷集合、挂载检查加上它，并加 `models check` 一步；同步 `test-production-preflight.sh` 等运维测试（`make check-ops`）。
  - `materials.json` 的 `categories` 加 `character`，前端选材面板与资产浏览器同时过滤 `listed: false`。
  - 文档：`docs/dev/builtin-models.md` 补素体的生成步骤。
  - 完成判据：服务启动时清单校验通过；两个素体的 `rig` 都是 52 根骨骼。
- [ ] 6.2 素体的界面与验收：
  - `SceneEditor.tsx` 加「添加 > 素体」快捷入口；`AssetPanel.tsx` 的模型类加「我的 / 内置」切换（Epic 修订 v8，取代单独的「素体」分区）。
  - 资产库页面：`LibraryTree` 加「内置模型」，`BuiltinBrowser` 支持模型，`BuiltinViewer` 显示 3D 预览、来源与授权。
  - `editing.ts:newModelInstance` 改为接收模型引用。
  - `ModelParams.tsx`：内置模型的信息块，以及「换成男素体 / 女素体」（带目标材质）。
  - 带骨骼的模型和内置模型隐藏面分配、存入资产库、改参数。
  - 选材面板与资产库的内置来源过滤 `listed: false`。
  - e2e（单条）：AC-1，含资产库内置模型、资产面板拖入、换素体与撤销。
  - e2e：把 GLB 请求改成 503，检查占位与导出失败（AC-12）。
- [ ] 6.3 实测与 owner 材料：
  - AC-8：在实时 GI 下给素体换一个姿态，记录 CPU 蒙皮、静态化、段重建、GI 收敛到可导出的耗时。
  - 一个接近上限的导入角色的加载与静态化耗时。
  - 结果写进 design。
  - AC-1、AC-2、AC-6 的截图放进 `discovery/research/`，交 owner 看图。
