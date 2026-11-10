# Tasks

## 1. 后端：契约、数据与材质字段

- [x] 1.1 OpenAPI 与生成器形态：`api/openapi.yaml` 的 `CreativeModelGenerator` 拆成按 `language` 区分的 `oneOf`，新增 `CreativeModelImport`，`CreativeModelPayload` / `CreativeModelPreviewPayload` 加 `import`，`CreativeSceneTextureMaps` 加 `emissive`，新路径 `/creative/assets/from-glb` 与 `CreativeAssetFromGLBPayload`，运行 `make generate`。`models.go`：`Generator.MarshalJSON`、`Imported()`，`ModelPayload.Import`。先写测试：无参数代码模型经 `commit_model`、场景读取、`derive_model`、存入资产库后仍有 `params_schema: {}`、`params: {}`；导入形态只输出 `language`、`mount` 并能往返。完成判据：`go build ./...` 与前端 `tsc -b` 通过（读生成器的调用点先用 `models/generator.ts:codeGenerator` 做类型收窄，界面留给 3.1；接口路径随 2.1 的 handler 一起加）
- [x] 1.2 自发光贴图与顶点色字段：`model.go:TextureMaps` 加 `Emissive`，`validateSlots` 接受它并拒绝 `channel`；`images.go:textureUses` 计入它；`materials.json` 的 `principled@1` 加 `vertex_color`（`make generate`）。先写测试：允许、`channel` 被拒、计入 64 张上限、`vertex_color` 原样保存

## 2. 后端：导入接口与读取方

- [x] 2.1 导入接口：新增 `creativescene/import.go:ImportModelToLibrary`，按 design「导入接口的校验与写入顺序」实现；`httpapi/creative_media.go` 加路由，`creative_documents.go` 让 `creative_upload_unavailable` 带 `details.upload_id`。先写测试：成功（载荷、持有关系、资产、上传均已消费）；每个失败分支各一例（400 ×3、404、409、413 ×2）；同一操作 ID 重放不建第二份，也不再消费上传
- [x] 2.2 只属于资产库与派生：`ops_model.go:isLibraryCopy` 改为按 `materials` 判断（`isLibraryOwned`），`(*deriveModelOp).apply` 照抄 `Import`。先写测试：三种载荷的判断结果；导入的模型做面分配后得到新内容，仍是 `imported` 且带 `import`，资产不变（AC-10）
- [x] 2.3 助手与读取方：`jobs.go:CreateBakeJobInTx` 和 `agent.go` 的 `write_model` 遇到导入的 base 时拒绝；`include_code` 与模型视图带 `imported`；`creativecontent/model_facts.go:ModelFactsOf` 支持导入形态；`creativeagent/context.go` 的 `search_assets` 要点和 `read_asset` 附 `imported`、`import`；`scene_tools.go` 更新说明（导入的模型、自发光贴图、顶点色）。先写测试覆盖以上各处（AC-11）

## 3. 前端：类型与材质渲染

- [x] 3.1 生成器收窄：新增 `models/generator.ts:isImported`；`panels/ModelParams.tsx`、`modelEdits.ts`、`SceneEditor.tsx`、`host/executor.ts`、`creative-canvas/viewer/modelInfo.ts` 按 `language` 收窄，导入的模型显示来源信息块（原文件名、授权说明、来源页面），不显示参数和代码。完成判据：`tsc -b` 与 `oxlint` 无报错，单测覆盖信息块的数据来源
- [x] 3.2 自发光贴图与顶点色（光栅）：`materials/maps.ts:MapKind`、`materials/images.ts`、`materials/instances.ts`、`materials/principled.ts:principledProps`、`materials/slotEdits.ts:mapInputs`、`panels/TextureFields.tsx`。先写单测：没配新字段的槽位，材质缓存键与修改前相同；配了以后自发光节点和 `vertexColors` 生效。e2e：现有材质贴图用例照常通过（AC-13），在基础体上配自发光贴图并撤销（AC-7）
- [x] 3.3 自发光贴图与顶点色（追踪层）：`render/trace/maps.ts`、`render/trace/hit.ts`、`render/trace/tables.ts`。先写单测：行参数带 `vertex_color`，纹理数组按四类建；e2e：实时 GI 下镜中能看到自发光贴图（单条用例）

## 4. 前端：导入流程

- [x] 4.1 预检与解析：`import/parse.ts`，在加载前扫描外部 URI、KTX2 / Basis 和蒙皮，然后注册 Draco（解码器随前端打包）与 meshopt 解析，再取图片原字节（`bufferView` 与 `data:` URL）。先写单测：外部图片 URI 和外部缓冲 URI 都在加载前被拒绝；`data:` URL 图片能取出原字节；Draco、meshopt 夹具（提交在 `frontend/scripts/fixtures/`，附来源说明）都能解析
- [x] 4.2 换算：`import/convert.ts`，实现 Epic 换算表的全部行，包括反量化、槽位命名、颜色换算、ORM 共图、贴图变换烘焙、寻址方式检查、原点移动和换算报告。先写单测：换算表每一行至少一例；量化夹具三种（整数位置且节点带缩放、归一化 UV、交错属性）；镜像与钳制寻址各两种（UV 在界内、跨界）
- [x] 4.3 写出与图片：`import/write.ts` 用 `GLTFExporter` 写出 GLB 并调用 `inspect()`；`import/images.ts` 处理超限缩小、按用途和透明度选编码、按哈希去重。先写单测：导出物通过 `inspect()`，没有 `images` / `textures` / `KHR_mesh_quantization`，包围盒与原文件相同；超限的透明 WebP 和法线贴图编码为 PNG，超限的不透明基础色编码为 JPEG
- [x] 4.4 提交边界：`import/run.ts` 依次完成上传、等就绪、预置本地图片并渲染缩略图、提交，按状态图重试或丢弃；`creative-canvas/editor/media.ts` 的 `uploadGenerated` 允许 `image/webp`。先写单测（注入网络操作）：响应丢失后用同一操作 ID 重试，只建一份且不丢弃上传；收到 4xx 后丢弃；提交中不能取消；上传中取消会丢弃（AC-12、AC-12b、AC-12c）
- [x] 4.5 导入对话框与入口：`import/ImportModelDialog.tsx` 包含换算报告、进度、标题、授权说明（用 `asset.copyright` 预填）、来源页面、原点选项、贴附方式，提交中关闭页面用 `beforeunload` 询问。入口：`creative-canvas/assets/AssetImport.tsx` 与 `creative-canvas/editor/fileImport.ts:importPlan`（`.glb` 交给对话框，拖到画布上的提示「模型只能导入到资产库」）、`panels/AssetPanel.tsx` 的「导入模型」、`SceneEditor.tsx` 的「添加 > 导入模型…」（导入后放入场景）。e2e（单条）：导入夹具 GLB，资产出现、缩略图非空；拖进场景，检查材质预览、实时 GI 镜中反射、`lit` 导出三处的自发光与挖空；撤销（AC-1 的自动部分、AC-9）

## 5. 验收与收尾

- [x] 5.1 大文件实测：用 10 / 50 / 150 MB 的素材记录各阶段耗时；单个阶段卡顿超过 1 秒时，把 parse、convert、write 移进 worker。结论写回 design 未决事项
- [ ] 5.2 owner 验收材料：准备 3–4 个公开授权素材（带贴图道具、贴图变换、顶点色、透明叶片）和一份 `.blend` 对照场景（同一相机、太阳加均匀环境光、手动曝光、AgX），把本项目三处截图与 Blender 截图放进 `discovery/research/`；统计角色加布景用到的不同图片张数（AC-14）。请 owner 判断 AC-1、AC-5、AC-6，并决定 64 张上限，结论写回 Epic 未决事项
