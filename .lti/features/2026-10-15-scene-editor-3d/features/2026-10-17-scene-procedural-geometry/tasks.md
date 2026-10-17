# Tasks

## 1. GLB 校验与内容层（后端）

- [x] 1.1 `creativeglb` 包：`Inspect` / `Manifest` / `Limits`，按 design「接受的 GLB 子集与 manifest」实现容器、缓冲、场景树（单场景、无环、单父、下标有效）、网格属性、访问器边界与对齐、材质无纹理、扩展白名单和预算检查；manifest 从 POSITION 实际数值算局部范围并与声明的 `min` / `max` 逐分量核对，世界包围盒取 8 角点变换；名称取 JSON 原文。`glb_test.go` 内置夹具生成器（`go test -run TestFixtures -update` 写出 `testdata/*.glb` 与 `*.manifest.json`），表驱动测试先写：4 类合规夹具 manifest 正确，11 类不合规夹具各返回对应原因码（AC-7）
- [ ] 1.2 迁移 `0074_creative_model_content` up/down：内容类型、上传与候选的 `declared_kind` 加 `model`，候选 `adopted_target_kind` 加 `document`；存在 `model` 内容、`model` 上传或被文档采用的候选时 down 拒绝。`creative_model_content_migration_test.go` 新增回滚链头 `rollbackModelContent`，`rollbackSceneDocument` 先调用它，各迁移测试的回滚标签列表最前面加 `creative-model-content`；up / down / 有数据时拒绝回滚三种情况通过
- [ ] 1.3 内容类型 `model`：`creativecontent.Payload` 加 `Model json.RawMessage`；`model` 列为受限类型，`Validate` / `WriteAndRetain` 拒绝（资产、画布 `replace_content`、生成输出都写不进）；`WriteMediaAndRetain` 改用内部 `validate`；新增 `WriteModelAndRetain`（可信对象绑定，可选 `base_content_id` 追加修订）；带对象的判断改为「媒体或模型」，`IsMediaKind` 现有语义不变。按经验 `shared-content-kind-write-entries` 逐个检查直接写入口；`content_test.go` 先写受限类型、两个入口、追加修订用例
- [ ] 1.4 媒体链路与暂存消费：上传类型 `model`（`.glb`，`model/gltf-binary`，16 MiB，只允许 `staged` 目标）；`verifyModel` 调用 `creativeglb.Inspect`，不合规以 `media_unsupported` 结束，通过后暂存形态载荷写入服务端 manifest；票据角色加 `thumbnail`；GC 类型范围加 `model`；`AdoptCandidate` 拒绝 `declared_kind='model'`；`creativestaging.Consume`（`applied`、清空内容 ID、`adopted_target_kind='document'` 与文档 ID、修订号）。测试覆盖 GLB 上传成功 / 失败、`model` 只能暂存、`thumbnail` 票据、候选不能被采用、GC 持有与回收（AC-7、AC-10）

## 2. 场景模型命令（后端）

- [ ] 2.1 回执扩展：`creativedocument.Changes` 加 `Response map[string]json.RawMessage`，`persist` 并入 `CommandResult` 顶层，与通用字段冲突时整个事务回滚，随幂等记录重放；`document_test.go` 先写附加字段、键冲突回滚、重放一致、不设置时回执不变
- [ ] 2.2 场景模型与读取：`model.go` 加 `model_instance` 种类与 `model` 字段、R1 引用上限（≤ 300 个不同模型修订）、`encodeScene` 填 `Refs`；`models.go` 实现 `ModelPayload` / `Generator` 校验（复用 `paramSpec`）和 R3 manifest 比对；`view` 用 `ReadDisplayBatch` 批量返回 `models`（不含代码，不可用项只有 `usable:false`），按经验 `query-count-guard-pg-stat-xact` 断言查询次数不随模型数增长；`unitView`、按单元恢复把 `model` 归入内容单元（AC-9）
- [ ] 2.3 模型操作：`sceneOp.apply` 带 `env{tx, retain, refs}`；`commit_model`（`ref` 规则、每批 ≤ 16 个、`generator` 校验、两次 `Lock` 与类型检查、manifest 比对、`WriteModelAndRetain`、两次 `Consume`、回执 `models`）、`set_instance_model`（`this` / `all_instances` 显式 `object_ids`）、`add_objects` 的 `model_instance`（`{ref}` / `{content_revision_id}`，只允许 `model` 和 `material_slots`）；`creativeDocumentError` 映射 409 `creative_upload_unavailable`、400 `creative_model_unavailable`。测试先写：manifest / 哈希不符、参数越界、`ref` 未定义 / 重复 / 未被引用、他人的或已消费的上传、上传类型不符、模型不可用、分叉三种组合、撤销换回旧修订，全部断言错误码且不产生修订（AC-1、AC-2、AC-3、AC-7）
- [ ] 2.4 契约与集成：`api/openapi.yaml` 按「接口与数据契约」修改并 `make generate`，提交两份生成物；`httpapi` 测试覆盖读取 `models`、上传 `model` 类型、两个新错误码、跨账号 404；`concurrency` 新增屏障测试各 20 轮：移动与换模型并发都成功、两边换模型恰好一方成功、`all_instances` 期间别处复制出共用实例时请求照常成功且新实例不变（AC-8）

## 3. 编译沙箱

- [ ] 3.1 沙箱页面：`package.json` 加 `three-bvh-csg`、`three-mesh-bvh`、`acorn`（精确版本）与沙箱构建前置；`scripts/build-scene-sandbox.mjs` 两次 IIFE 构建输出 `public/scene-sandbox/`（加入 `.gitignore`）；`sandbox/harness.ts`（每次编译新建 classic Worker）、`worker.ts`（design「Worker 内的步骤」1–6）、`lib.ts`（`lib@1` 六类函数）、`protocol.ts`、`csp.txt`；Vite 插件提供 `/scene-sandbox/sandbox.html` 并带 CSP；Go `staticHandler` 对 `scene-sandbox/` 前缀带 CSP 返回、模板缺失 404、前缀下不回退首页，`httpapi` 测试覆盖。完成判据：开发服务器下默认模板在沙箱中编译出 GLB
- [ ] 3.2 宿主：`host.ts`（单编译、5 秒超时移除重建、只收 `event.source` 匹配的消息、逐字段校验、`module` 按服务端规则校验、`crypto.subtle` 计算代码哈希）；`inspect.ts`（`creativeglb.Inspect` 的 TS 版，读取同一组 `testdata` 夹具，两端接受 / 拒绝与 manifest 一致）；`thumbnail.ts`（预检通过后才交给 `GLTFLoader`，`setURLModifier` 只放行 `blob:` / `data:`）。补一个由 `GLTFExporter` 实际导出的默认模板夹具，Go 与前端都通过。`creative-scene-model.test.ts` 先写协议、`module` 校验、上限、运行错误行号解析（AC-4、AC-5）
- [ ] 3.3 安全样例集 `scripts/scene-sandbox.test.mjs`：真实沙箱 + 攻击者端口，覆盖网络（fetch、XHR、WebSocket、EventSource、动态 `import`、`importScripts`）、cookie、`localStorage`、IndexedDB、父页面 DOM、顶层导航、`eval`、死循环、内存炸弹、GLB 内含外链 URI，断言攻击者端口 0 请求、宿主心跳正常、之后的编译照常；Chromium 纳入 `make check-frontend`，`SCENE_SANDBOX_BROWSERS=webkit` 手动运行（AC-6）

## 4. 编辑器

- [ ] 4.1 会话扩展：`state/apply.ts` 加 `commit_model`（只记录 `ref`）和 `set_instance_model` 的投影，`{ref}` 写成占位 ID `local:` / `local-content:`，`contentKeys` 加 `model`；`sync.ts` 的 `failed` 加新错误码分支（不进回收站 / 不存在分支）、含未确认占位 ID 的意图不可发送、回执先改写队列与撤销 / 重做栈里的占位 ID 再推进 base、被拒时依赖意图转 `blocked`；新增编辑基线 `session.baseline(units)`，本机回执改写其中的 `{after}`，远端重读不更新，`edit(ops, label, {baseline})` 沿用冻结值。单测先写：回执前复制和再调参、基线期间远端修改后提交收到冲突（AC-3、AC-8）
- [ ] 4.2 模型渲染：`media.ts` 程序内上传 `model`（不开放文件选择）、票据角色 `thumbnail`；`models/cache.ts` 按修订取票据、加载与缓存，加载走同一 URL 限制；`ModelMesh` 用 clay 材质、`clone()` 共享几何；`SceneObjects` 的 `model_instance` 分支：加载中按 manifest 包围盒画线框，不可用或加载失败显示灰色方块并在属性栏标出；历史预览同样显示模型（AC-9）
- [ ] 4.3 编辑编排 `modelEdits.ts`：冻结基线 → 编译 → 宿主校验 → 缩略图 → 两个暂存上传并行 → 按 R4 分叉表组织命令 → `edit`；本机预览覆盖与新建时的半透明临时物件；更新的输入取代旧的并丢弃已就绪的候选；目标实例已删除时放弃并提示；失败表各阶段的提示。单测覆盖分叉表三行、取代与丢弃、各阶段失败不产生意图（AC-2、AC-3、AC-4）
- [ ] 4.4 面板与入口：`CodePanel.tsx`（CodeMirror 6 按需加载、默认模板、生成 / 重新生成、错误行标记、与历史面板互斥）、`ModelParams.tsx`（四种参数控件、范围限制、300 ms / 回车 / 失焦提交、「与 N 个实例共用」与「应用到全部」开关）；添加菜单「代码模型…」、属性栏与「物件」菜单的「编辑代码」、物件列表图标。`creative-scene.e2e.mjs`（真实沙箱 + mock 接口）覆盖新建、调参与撤销重做、分叉 / 全部、语法错误行号且场景不变、修改参数定义后重新生成、超限提示、焦点在代码面板时 Cmd+Z 只撤文字、模型不可用占位（AC-1 至 AC-5、AC-9）

## 5. 收尾

- [ ] 5.1 文档与实测：`docs/dev/creative-media.md` 补上传类型 `model`、`thumbnail` 票据和「先升级 creative-worker 再升级 server」；本机真实服务测 20 次调参，记录编译、宿主校验、缩略图、上传与 worker 校验、提交命令各阶段 p50 / p95（远程开发库数据仅作参考），结论与 WebKit 手动运行结果写入 verification（AC-11、AC-6）
- [ ] 5.2 交 owner 本机体验：新建代码模型、调参、分叉、改代码、撤销重做、两个标签页并发调参；记录体验结论与遗留问题
