# Tasks

## 1. 首批条目

- [x] 1.1 核对候选清单（design 未决 1）：用 Poly Haven API 逐个确认 2K 的 `diff`、`nor_gl`、`arm` JPG 都在，记录大小与真实尺寸；缺的换同品类其他资产，替换项写回 proposal 的候选表。下载全部文件算出 sha256，写成 `textured` 条目进 `materials.json`（名称、品类、`size_m`、`maps`、`defaults`、`files`、`source`）。完成判据：24 个条目齐全，`bytes` 与 sha256 来自实际下载。结果：24 个候选都有 2K 的三张 JPG，无替换；`fabric_pattern_07` 有三种配色，基础色取 `col_1`；72 个文件共 177 MB，下载后按 API 的 md5 和大小核对

## 2. 后端：目录、校验与存储

- [x] 2.1 目录认识 `textured`：先写 `loadMaterials` 的坏样例单测（缺文件引用、文件名不是 sha256、`defaults` 键不在原理化参数表或越界、品类未知、缺缩略图），再改 `materials.go`；`checkMaterial` 对 `textured` 用 `principled@1` 的参数表；`MaterialCatalog()` 不逐条列出 `textured`，改为指向内置来源的一句说明；新增 `BuiltinTextureFiles()`（`BuiltinMaterials`、`BuiltinThumbnail` 和缩略图的加载校验随 2.5 做，缩略图要在 4.1 渲染出来之后才有）。目录加一张品类表 `categories`（ID 与中文名，展示顺序），前后端共用。完成判据：正式目录能加载，单测全绿；内置条目的外观（尺寸、文件、通道、缺省值）在单测里锁定
- [x] 2.2 槽位校验放宽：先写 `checkTextures`、`completeTextures` 的决策表单测，以及命令接口的用例（`textured` 带 `maps` 返回 400 并指出字段；不带 `mapping` 时存储补齐 `size_m`；目录外的 ID 带 `mapping` 返回 400），再改 `textures.go`
- [ ] 2.3 内置贴图存储与导入：`builtin_textures.go` 的 `Open`、`Missing`、`Import`（`immutablefs`，前缀 `scene-textures/`），配置项 `SCENE_TEXTURE_LOCAL_ROOT`；先写桩下载器的单测（成功、跳过、大小不符、哈希不符、下载失败、重复运行）。`creativectl textures import` 和 `textures check`，输出与退出码按 design。完成判据：本机运行 import 写入全部文件，再次运行全部跳过，check 退出码为 0
- [ ] 2.4 读取接口：OpenAPI 加 `GET /creative/scene-textures/{file}` 和错误码 `creative_texture_unavailable`，运行 `make generate`；httpapi handler 与路由注册；先写 401、404、503、200 响应头、304 的测试。`server` 装配存储，启动后台检查并记错误日志
- [ ] 2.5 助手的内置来源：先写单测（`search_assets source:builtin` 在没有 consent 时可用、按文字和品类过滤、`kind` 不是材质时为空；不写 `source` 时结果不变；`read_asset builtin_id` 返回 `slot`、缩略图附件，未知 ID 报错并列出可用 ID），再改 `library_tools.go`、`context.go`，并更新 `asset_tool_info.go`、`context.go`、`scene_tools.go` 的工具说明

## 3. 前端：渲染

- [ ] 3.1 目录与展开：`catalog.ts` 加 `textured` 条目（参数表取原理化，按 `defaults` 覆盖缺省值），`resolve.ts:catalogMaterial` 把它展开成原理化加内置贴图；先写单测：参数逐层覆盖、映射缺失时取 `size_m`、`texturingOf` 对内置贴图的输出
- [ ] 3.2 图片来源扩展：`images.ts` 的 `MapRef` 与纹理键部件改为 `source`，`imagePlatform.fetch` 遇到 `tex:` 走内置读取接口；`textureRevisions` 改为 `textureSources`，内置源不计入场景图片。先写单测：内置源与修订源混用、去重、失败（非 200 时报加载失败、不显示状态码）、预算；现有图片测试不变仍绿

## 4. 前端：资产库的「内置」来源

- [ ] 4.1 缩略图：`frontend/scripts/render-texture-thumbnails.mjs` 用 `renderMaterialBall` 渲染每个条目，输出 256 × 256 WebP 到 `backend/internal/creativescene/builtin_thumbnails/`；`Makefile` 的 `generate` 复制到 `frontend/src/creative-canvas/assets/builtin/`，`generate-check` 检查两边一致。完成判据：24 张缩略图提交，`make generate-check` 通过
- [ ] 4.2 `assets/builtin.ts`（展示项与本地过滤），以及资产库页面的「内置材质」节点（`LibraryTree.tsx`、`AssetsPage.tsx`、`AssetBrowser.tsx`，整理操作不可用）和查看浮窗（`AssetViewer.tsx`、`AssetInfoPanel.tsx`）；先写 `builtin.ts` 的过滤单测
- [ ] 4.3 场景编辑器：`AssetPanel.tsx` 材质页的「我的 / 内置」切换和拖放（带 `builtin_id`），`LibraryPicker.tsx:MaterialPicker` 的来源切换，`MaterialSlots.tsx` 落下内置材质、`textured` 槽位的属性栏（名称与缩略图、参数、映射与透明方式，复用 `TextureFields.tsx`）。e2e（mock 读取接口，用小图，端口 5189，一次一个）：拖到物件上、调尺寸、撤销；「从资产库选」；资产库页面筛选和查看；读取返回 503 时显示占位和提示、导出失败

## 5. 部署、说明与实测

- [ ] 5.1 部署与说明：`scripts/production-preflight.sh` 调 `creativectl textures check`；`docker-compose.yml` 加本地驱动的 `scene-textures` 卷；新增 `docs/dev/scene-texture-library.md`（导入、检查、新增条目、缩略图重新生成、来源与许可）
- [ ] 5.2 实测与目视：在本机导入后，记录首批总大小，以及一个用了 5 套材质的场景首次打开时的下载时长和显存占用；在材质预览和最终渲染下逐个检查 24 套（真实尺寸、法线方向、ARM 通道），截图给 owner。助手样例：「给地面铺木地板、墙面换红砖」，确认它经内置来源查到并写对。结论写回 design 的未决 2
