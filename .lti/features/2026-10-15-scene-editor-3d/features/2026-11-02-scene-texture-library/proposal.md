---
risk:
  factors: [perf-sensitive, broad-impact]
behavior: changed
reads: [creative/scene, creative/library]
decisionsPending: false
---
# C5c2 内置贴图库

## Why

C5c1 之后，材质能用账号里的图片做贴图，但摄影师和助手手上都没有现成的 PBR 贴图：要么自己找图上传，要么只能用 14 种程序化预设。Epic 定了要内置一批 CC0 贴图，不依赖用户素材（owner 2026-11-02，修订 v16）。修订 v20（owner 2026-11-04）定了形态、分辨率、托管方式和首批规模，本次按它交付。

## What Changes

- **内置贴图材质**：材质目录新增第三类 `textured`，与原理化 BSDF、程序化预设并列。首批约 24 套、10 类，见下表。每套是一份完整的 PBR 材质：基础色、法线、ARM（AO、糙度、金属度打包），2K，按来源站标注的真实尺寸三平面铺开。
- **槽位里怎么用**：选中后槽位只记 `preset` 和参数。参数表同原理化 BSDF，与贴图相乘，可以调色调、糙度、金属度等，也可以改映射方式（三平面尺寸、旋转，或模型 UV 的重复次数）、法线强度和透明方式。不能替换其中的单张贴图：要换图，用原理化 BSDF 配自己的图（C5c1）。
- **资产库里的「内置」来源**（参照 Blender 的 Essentials 库）：资产库页面、场景编辑器的资产面板、材质槽的「从资产库选」，在材质下都可以在「我的」和「内置」之间切换，两边不混排。
  - 内置项按品类排列，有材质球缩略图、名称和真实尺寸，可以按名称或品类筛选。
  - 能打开查看浮窗看 3D 材质球，能拖到物件或用到槽位上，用法与材质资产相同。
  - 内置项只读：不能评分、打标签、分组、收藏。
  - 缩略图离线渲染、随前端打包，打开资产库时不下载贴图。
  - 用到槽位之后，参数和映射在现有的属性栏里调整。
- **全部渲染入口生效**：与 C5c1 的贴图相同，包括材质预览、最终渲染、`render_view`、`lit` 导出、资产缩略图和 3D 预览、屏幕外反射和探针 GI。离屏出图等贴图就绪。
- **托管**：贴图原文件存进对象存储的独立前缀 `scene-textures/`，不进 git，也不打进二进制。`creativectl textures import` 按目录里的清单从 Poly Haven 下载，校验 sha256 后写入，可以重复执行；`creativectl textures check` 只做检查，供生产预检使用。编辑器经 `GET /api/v1/creative/scene-textures/{file}` 读取，响应长缓存。
- **没导入时**：服务启动时记错误日志，生产预检报错。编辑器按「图片加载失败」处理，不显示原始错误。
- **助手**：
  - `search_assets` 加 `source: "builtin"`，按文字和品类查内置材质。
  - `read_asset` 加 `builtin_id`，返回要点和缩略图。
  - 与账号的材质资产同一套用法，不需要资产库授权。
  - 写入用 `{preset: "<id>"}`，工具层按目录校验。
  - `edit_scene` 的说明只提一句「内置材质用 `search_assets` 查」，不逐条列出。
  - 外部助手的同名工具随之获得这两个可选参数。
- **材质资产**：用了内置贴图材质的槽位可以照常存成材质资产，资产只存 `preset` 和参数。
- **不做**：
  - 单张内置图用进原理化 BSDF 的某个贴图输入。
  - 槽位提示映射到内置贴图材质：现有映射照旧指向程序化预设，已有场景外观不变。
  - 内置贴图的转码（WebP、KTX2）和分级加载。
  - 内置 HDRI，留给 C5c3。

### 首批候选（Poly Haven，CC0；第一项任务逐个核对文件和尺寸，可能有个别替换）

| 品类 | 条目（来源资产 ID，真实宽度） |
|---|---|
| 木材 `wood` | 木地板 `wood_floor`（1.7 m）、复合地板 `laminate_floor_02`（1.7 m）、橡木饰面 `oak_veneer_01`（1.83 m）、旧木板 `weathered_brown_planks`（1.8 m） |
| 石材 `stone` | 岩壁 `rock_face_03`（2.7 m）、大理石 `marble_01`（1.5 m） |
| 砖墙 `brick` | 红砖 `red_brick`（1.4 m）、砖墙 `brick_wall_001`（3 m）、中世纪石块 `medieval_blocks_03`（2 m） |
| 混凝土与抹灰 `concrete` | 旧混凝土地面 `concrete_floor_worn_001`（3 m）、刷漆抹灰墙 `painted_plaster_wall`（2 m）、米色墙面 `beige_wall_001`（3 m） |
| 金属 `metal` | 金属板 `metal_plate`（0.5 m）、锈蚀金属 `rusty_metal_02`（1 m） |
| 布料 `fabric` | 花纹布 `fabric_pattern_07`（0.4 m）、牛仔布 `denim_fabric`（0.1 m） |
| 地面 `ground` | 林地 `forrest_ground_01`（2 m）、草地 `leafy_grass`（2 m）、砂砾 `sandy_gravel_02`（2.53 m） |
| 瓷砖 `tiles` | 地砖 `floor_tiles_06`（3 m）、水磨石 `terrazzo_tiles`（2 m） |
| 屋面与沥青 `roofing` | 沥青路面 `asphalt_02`（3 m）、石板瓦 `roof_slates_03`（3 m） |
| 皮革 `leather` | 棕色皮革 `brown_leather`（0.4 m） |

## Acceptance Criteria

**编辑器**

- WHEN 摄影师在资产库页面或场景资产面板的材质下切到「内置」 THEN 按品类列出全部内置贴图材质，每项有缩略图、名称和尺寸；输入「木」只剩木材类；打开时不下载任何贴图原文件；评分、标签、分组、收藏等整理操作不可用。
- WHEN 摄影师在查看浮窗里打开一个内置材质 THEN 显示可拖动旋转的 3D 材质球和名称、品类、尺寸、来源。
- WHEN 摄影师把内置材质拖到物件上，或在材质槽的「从资产库选」里选中它 THEN 槽位写入这个内置材质，与使用材质资产一样是一步可撤销的修改。
- WHEN 物件用了一种内置贴图材质 THEN 材质预览和最终渲染里显示这套贴图，并按真实尺寸三平面铺开：一块 1.7 m 的木地板铺在 3.4 m 宽的地面上，横向重复两次；缩放物件时贴图不拉伸。
- WHEN 摄影师调这个槽位的色调、糙度、三平面尺寸、旋转或法线强度，或改成模型 UV THEN 画面随之改变；撤销、刷新后与保存的一致。
- WHEN 内置贴图文件读取失败（没导入或对象存储出错） THEN 视口显示占位并提示「贴图加载失败」，不显示接口错误原文；离屏出图失败并说明原因。

**渲染与出图**

- WHEN 场景里有内置贴图材质，执行 `render_view`、导出 `lit` 图、生成材质资产缩略图或打开资产 3D 预览 THEN 都显示贴图，离屏出图等贴图就绪后才渲染。
- WHEN 内置贴图材质的物件出现在屏幕外反射或探针 GI 里 THEN 按贴图采样，不用平均色近似（与 C5c1 相同）。
- WHEN 同一场景里多个槽位用同一种内置贴图材质，或再次打开同一场景 THEN 每个文件只下载一次，浏览器缓存命中时不再请求。

**助手**

- WHEN 助手按「给地面铺木地板」修改场景 THEN 它用 `search_assets`（`source: "builtin"`）查到木地板，写入 `preset: "wood_floor@1"`（可带 `mapping`），用 `render_view` 回看时看得到贴图；这次对话没有资产库授权时也能查到。
- WHEN 助手用 `read_asset` 读一个内置材质 THEN 返回名称、品类、尺寸、可直接写入的 `slot` 和缩略图；`builtin_id` 不存在时返回错误并列出可用 ID。
- WHEN 助手用 `search_assets` 查账号资产（不写 `source`） THEN 结果与现在相同，不混入内置项。
- WHEN 助手给内置贴图材质写了 `maps`，或写了未知参数、越界参数 THEN 工具层返回错误并说明原因；命令接口对 `maps` 同样返回 400 并指出字段。

**托管与部署**

- WHEN 在本地驱动或 OSS 驱动下运行 `creativectl textures import` THEN 下载清单里的全部文件，核对 sha256 后写进 `scene-textures/`；再次运行全部跳过；某个文件校验不符时它不写入、命令以非零退出并说明是哪个文件。
- WHEN 对象存储里缺少清单中的文件 THEN `creativectl textures check` 和生产预检报错并列出缺失项，服务启动时记错误日志，但照常启动。
- WHEN 请求 `GET /api/v1/creative/scene-textures/{file}` THEN 未登录返回 401；文件名不在目录里返回 404；在目录里但没导入返回 503 `creative_texture_unavailable`；成功时返回原文件，带 `ETag` 和 `Cache-Control: private, max-age=31536000, immutable`，带 `If-None-Match` 时返回 304。

**资产库**

- WHEN 摄影师把用了内置贴图材质的槽位存成材质资产，再用到另一个物件上 THEN 外观一致；资产里只记 `preset` 和参数，不产生图片引用。

## Impact

- **后端**：
  - `backend/internal/creativescene`：`materials.json` 增加 `textured` 条目，`materials.go` 加载和校验它，`textures.go` 放宽校验并补齐映射；助手材质说明 `MaterialCatalog()`。
  - 新增内置贴图的存储与读取：`immutablefs` 前缀和配置项 `SCENE_TEXTURE_LOCAL_ROOT`，HTTP 路由，启动检查。
  - `backend/cmd/creativectl`：新增 `textures import` 和 `textures check`。
  - `backend/internal/creativeagent`：`search_assets` 的 `source`、`read_asset` 的 `builtin_id`。
  - `api/openapi.yaml` 增加读取路由，运行 `make generate`。
- **前端**：
  - `frontend/src/creative-scene/materials/`：`catalog.ts`、`resolve.ts` 把 `textured` 展开成带贴图的原理化材质；`images.ts` 增加内置文件作为图片来源。
  - `frontend/src/creative-canvas/assets/`：资产库页面的「内置」来源。
  - `frontend/src/creative-canvas/viewer/`：查看浮窗显示内置材质。
  - `frontend/src/creative-scene/panels/AssetPanel.tsx`：场景资产面板的「内置」来源。
  - `frontend/src/creative-scene/panels/LibraryPicker.tsx`：「从资产库选」材质的选择框。
  - `frontend/src/creative-scene/panels/MaterialSlots.tsx`：槽位用了内置材质时的属性栏。
  - 缩略图离线渲染脚本。
- **部署**：
  - 生产预检脚本 `scripts/production-preflight.sh`。
  - `docker-compose.yml`：本地驱动的卷。
  - 新增开发说明 `docs/dev/scene-texture-library.md`，写清导入、检查、更新条目和来源许可。
- **契约**：
  - 交付 `data: 内置贴图库`、`api: 读取内置贴图`。
  - 交付 `data: 场景文档`、`api: 提交场景命令` 的修订 v20 部分。
- **行为规格**：`creative/scene` 增加「内置贴图材质」。

## Risk Notes

- **加载体积**：一套约 10 MB，首次打开用了 5 套的场景要下载约 50 MB。这是 owner 选 2K 时接受的代价；浏览器长缓存之后不再下载。实施时记录实测加载时长，如果明显影响首屏，再向 owner 提转码或分级加载（不在本次范围）。
- **显存**：每套三张 2K 纹理约 67 MB，C5c1 的 2 GiB 预算约容纳 30 套同时显示，超出时按 C5c1 的规则显示占位并提示。
- **来源站变化**：Poly Haven 的下载地址或文件可能变化。导入按 sha256 校验，不符就不写入；已导入的文件不受影响。开发环境首次导入需要能访问 dl.polyhaven.org。
- **外部助手**：`search_assets` 和 `read_asset` 的外部同名工具归活动 Epic `creative-mcp-server` 负责，这次只增加可选参数，不改变已有入参和结果；合并前与那边核对一次。
- **缩略图一致性**：缩略图由离线脚本用编辑器的材质球渲染，材质构建改动后需要重新生成。脚本可重复运行，生成结果提交进仓库。
