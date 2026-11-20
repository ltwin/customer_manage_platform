<!-- 本 Feature 的目标契约，批准前为草稿，不代表项目当前已实现。正文以 2026-11-17 的项目契约为底；其他活动 Epic/Feature 也修订同一章节时，后合回的一方以当时的项目契约为底重写本条。 -->

## data: MODIFY 场景文档

### 用途与存储角色
- **用途**：保存 3D 场景的编排，包括对象树、变换、环境、渲染设置和单元版本。不保存几何（几何在模型内容里）、GLB 字节、生成代码和导出图。
- **存储**：PostgreSQL。
  - 通用文档层：`creative_documents` 行，`type_key = 'scene'`。
  - 修订：`creative_document_revisions`。
  - 引用：`creative_document_content_refs`。
  - 每个修订的全文快照：一条 `creative_content_revisions`，内容类型 `scene`。
  - 通用文档的表（文档行、修订、引用、工作会话）由通用文档底座 change 交付，本条目只定义 `scene` 类型的载荷、单元和引用登记。
- **角色**：权威主数据。`creative_documents.content_revision_id` 指向当前快照；修订表是历史的唯一顺序来源。
- **所有权**：只有 `creativescene` 写入，摄影师和助手都经场景命令；画布命令层只在新建 `core.scene` 节点时同事务创建文档。读取方是场景接口、画布节点视图和助手工具。

### 结构与身份
- 文档 ID 形如 `cwdoc_<uuid>`，账号内唯一。对象 ID 形如 `sobj_<21位 nanoid>`，由客户端生成，场景内唯一，删除后不复用。不复用由客户端随机生成来保证；服务端只拒绝与当前快照重复的 ID，不查历史。
- 快照载荷（`creative_content_revisions.payload`，`schema_version = 1`，≤ 512 KiB）：

| 路径 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `scene.environment.sky.kind` | `"procedural"` \| `"hdri"` \| `"color"` | 是 | 新建场景为 `procedural` |
| `scene.environment.sky.sun_elevation_deg` | number −10…90 | `procedural` 时 | 新建为 35；0° 为地平线，90° 为天顶 |
| `scene.environment.sky.sun_azimuth_deg` | number 0…360 | `procedural` 时 | 新建为 135；0° 指向 +Z，90° 指向 +X（与 three.js Sky 示例的 `azimuth` 相同） |
| `scene.environment.sky.turbidity` | number 1…20 | `procedural` 时 | 新建为 3 |
| `scene.environment.sky.hdri` | string，内置 HDRI ID（`^[a-z0-9_]+@[1-9][0-9]*$`） | `hdri` 时 | — |
| `scene.environment.sky.color` | `#rrggbb` | `color` 时 | — |
| `scene.environment.ambient` | `{color:#rrggbb, intensity:0…10}` | 是 | `{#ffffff, 0.4}` |
| `scene.environment.fog` | `{color:#rrggbb, near>0, far>near}`（three.js `Fog`，米）或 null | 是 | null |
| `scene.render.mode` | `"clay"` \| `"lit"` | 是 | `clay`；只影响编辑器默认显示，导出时另行指定 |
| `scene.render.exposure_ev` | number −10…10（曝光补偿，档） | 否 | 0；只作用于 `lit`（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）：渲染时 three.js `toneMappingExposure = 2^exposure_ev`。白模、线稿和材质预览不受影响（修订 v10） |
| `scene.render.engine` | `"workbench"` \| `"realtime_gi"`（渲染器） | 否 | `workbench`；只决定最终渲染怎么出图（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）。`workbench` 是光栅化渲染；`realtime_gi` 在其上加入屏幕空间的间接光、反射与接触阴影，并在满足上限的 WebGPU 设备上用世界空间追踪补上屏幕外的反射和漫反射（`scene-world-space-gi`；兼容模式、设备不支持或场景过大时只做屏幕空间，不改写场景），视口停止操作后收敛，导出和 `render_view` 多帧累积（`scene-webgpu-renderer` design「渲染器与显示模式」）。白模、线稿、线框和材质预览不受影响；正交视图按 `workbench` 显示。只计入 `lit` 的渲染指纹，值为 `workbench` 时不计入；`realtime_gi` 计入时连同世界空间追踪的版本号一起计入（`scene-world-space-gi` S2 起为 1），所以 S2 上线后已有的实时 GI `lit` 导出各显示一次「场景已更新」，`workbench` 的 `lit` 和 `clay`、`line` 不受影响，指纹版本前缀不升级。`path_tracing` 由后续 Epic 加入，现在写入返回 400 |
| `scene.render.resolution` | `"1k"` \| `"2k"` \| `"4k"` \| `"8k"`（输出分辨率） | 否 | `2k`；导出图（`clay`、`line`、`lit`）的长边分别为 1024、2048、4096、8192 px，短边按摄像机的画幅比例。不影响视口、`render_view`（JPEG，长边 1024）和缩略图。不计入渲染指纹 |
| `scene.render.transparent_background` | boolean（透明背景） | 否 | `false`（持久化快照在值为 `false` 时省略该键、仅在 `true` 时写入 `"transparent_background": true`；场景读取接口、修订读取接口与 `read_scene` 始终补齐返回布尔值）；开启后，跟随场景设置的白模（`clay`）和材质光照（`lit`）导出图里相机直接看到天空的像素（天空、纯色背景、太阳圆盘、夜空的月亮与星星和银河）透明（`alpha = 0`），物体照旧由天空、太阳、月光、环境反射和灯光照亮，金属与镜面映出的天空保留；抗锯齿与景深边缘按覆盖率输出半透明，玻璃等透射材质与体积物件保留场景内的物理透射、折射与反射，并对空背景输出单通道 W3C `source-over` 合成用的近似标量透明度；`1k` 至 `8k` 均按选定分辨率编码为无损 `image/png`。编辑器的最终渲染模式在视口背景显示棋盘格，白模、材质预览保持原有中性灰背景，线框不变。线稿（`line`）不受影响，仍是白底 `image/png`；助手回看（`render_view`）和缩略图不受影响。助手 `export_shots` 另可按次显式指定 `transparent_background` 覆盖本设置而不改写快照（见 `data: 场景客户端任务`、`data: 导出图来源`）。计入 `clay` 和 `lit` 的渲染指纹（`line` 不计入），有效值为 `false` 时省略该键，存量导出图不因本次上线变为过时 |
| `scene.objects[]` | 对象数组，≤ 2000 | 是 | 新建为一个内置地面 |
| `objects[].id` | string | 是 | 见身份规则 |
| `objects[].kind` | `"primitive"` \| `"model_instance"` \| `"light"` \| `"camera"` \| `"group"` | 是 | `model_instance` 自 C2 起可写入 |
| `objects[].name` | string ≤ 80 字符 | 是 | 可为空串 |
| `objects[].parent_id` | string 或 null | 是 | null 表示根；父对象必须是 `group`；不能成环 |
| `objects[].order` | integer ≥ 0 | 是 | 同一父对象下的兄弟顺序，兄弟之间不重复 |
| `objects[].transform` | `{position:[3], rotation:[4]（单位四元数 x,y,z,w）, scale:[3]（每项 0.001…1000）}` | 是 | 数值有限；位置每轴 ±100000 m |
| `objects[].visible` / `locked` | boolean | 是 | `true` / `false` |
| `objects[].primitive` | `{builtin:"<name>@<ver>", params:{…}, faces?}` | `kind=primitive` 时 | `builtin` 必须在服务端登记的内置清单里（C1 交付清单）；`params` 外形同模型参数值 |
| `objects[].primitive.faces` | object，结构同 `data: 模型内容` 的 `model.faces` | 否 | 面级材质分配（C5b，修订 v13）。基础体不在物件间共用，分配直接记在物件内容上，改它只涉及 `object:<id>:content`，不产生模型修订。基础体只有一个网格，`meshes[]` 至多一项，`path` 固定为 `[["", 0]]`、`primitive` 为 0；区间 ≤ 256 个。保留还是作废只按 `data: 场景内置基础体` 的 `topology` 声明判断（编辑器与服务端同一规则），改了声明里的参数即清空，见 `api: 提交场景命令`；指纹只用于加载时兜底核对 |
| `objects[].model` | `{content_revision_id}` 或 `{builtin}` | `kind=model_instance` 时 | 二者恰好写一个（scene-pose-system 子项 3）。`content_revision_id` 必须是本账号 `model` 类型、`ready` 且可展示的内容修订；`builtin` 必须是 `data: 内置模型库` 清单里的条目 ID，否则返回 400 `creative_model_unavailable`。内置模型的实例不能做面级材质分配，有效槽位取清单条目的 manifest |
| `objects[].material_slots` | `{<slot_id>:{preset, params, maps?, mapping?, alpha_mode?, alpha_cutoff?}}` | 否 | 贴图与透明方式见下方「材质贴图」（修订 v17）。缺省 `{}`；`preset` 匹配 `^[a-z0-9_]+@[1-9][0-9]*$`；`params` 是平铺对象，≤ 32 键，值为 number/string(≤80)/boolean。键对应物件的有效槽位（修订 v13）：基础体为 `main` 加 `primitive.faces` 分出的槽位，模型实例为模型的有效槽位（`data: 模型内容`）。服务端不按槽位清单校验键（与 C5 相同），不在有效槽位里的键照常保存、渲染时不用 |
| `objects[].light` | 按 `type` 区分，字段与 three.js 灯光类同名同义，见下方「灯光」 | `kind=light` 时 | 只有 `type` 必填，其余缺省取 three.js 默认值，存储时补齐 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near>0, far>near（米，同 three.js `PerspectiveCamera`）, label≤40, depth_of_field:bool, f_stop:0.5…64, focus>0（米，同 three.js `PerspectiveCamera.focus`）, focus_object_id:物件 ID 或 ""}` | `kind=camera` 时 | 各字段可省略，缺省 `{35,[36,24],"3:2",0.1,1000,"",false,2.8,10,""}`，存储时补齐；景深四个字段为修订 v8 新增，旧快照在读取时补齐。朝局部 −Z 看（three.js 摄像机约定）；视场角按画面长边对应传感器长边算出：`tan(fov/2) = max(sensor_mm) / (2·focal_mm) / max(画幅宽高比, 1)`，`fov` 为 three.js 的垂直视场角（修订 v8），不写 three.js 的 `fov`。`focus_object_id` 非空时按该物件原点沿视线方向的深度对焦：写入时必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己；之后目标被删除不算违反契约，按 `focus` 对焦。写空字符串取消对焦物件（按键合并下 `null` 等于没写）。`depth_of_field = true` 时，白模导出按光圈和对焦虚化（design「渲染模式与导出」） |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型和图片 `content_revision_id`（图片自修订 v17）。内置模型（`model.builtin`）不是账号内容，不进引用表。
- **材质贴图**（修订 v17，C5c1）：槽位值的可选字段，只在 `preset = principled@1` 时允许，写在其他预设上返回 400 并指出字段。修订 v20（C5c2）：内置贴图材质（材质目录里 `kind = textured` 的预设，见 `data: 内置贴图库`）也接受 `mapping`、`alpha_mode`、`alpha_cutoff`，但不接受 `maps`（贴图由目录给出），写了返回 400；`mapping` 省略时存储为 `{kind:"triplanar", size_m:<目录里的真实尺寸>, rotation_deg:0, normal_strength:1}`，`alpha_mode` 省略时补 `opaque`。`params` 的键与范围同 `principled@1`（助手工具层按目录校验，命令接口照旧只校验外形），与目录的贴图相乘，规则同下；没写的参数取目录条目的缺省值。目录里没有的预设 ID 照旧只校验格式，但带了这三个字段时返回 400（无从判断是不是内置贴图材质）。

| 字段 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `maps` | object | 否 | 键为 `base_color`、`normal`、`roughness`、`metalness`、`ao`、`emissive`（scene-pose-system 子项 2），至少一个；未知键返回 400 |
| `maps.<kind>.content_revision_id` | string（`ccrv_…`） | 是 | 本账号 `image` 类型、`ready` 且可展示的内容修订，校验与模型引用相同；否则返回 400 `creative_image_unavailable` |
| `maps.<kind>.channel` | `"r"` \| `"g"` \| `"b"` \| `"a"` | 否 | 只对 `roughness`、`metalness`、`ao` 有效，缺省 `r`，存储时补齐；写在 `base_color`、`normal`、`emissive` 上返回 400。glTF 的打包 ORM 图可以三项都指定同一张图，分别取 `r`、`g`、`b` |
| `mapping` | object | 否 | 只在有 `maps` 时允许；有 `maps` 而没写时存储为 `{kind:"triplanar", size_m:1, rotation_deg:0, normal_strength:1}` |
| `mapping.kind` | `"triplanar"` \| `"uv"` | 否 | 缺省 `triplanar`：按物件空间的米铺开，物件缩放时不拉伸（同 Blender 的 Box 投影）。`uv` 用模型的 `TEXCOORD_0`；模型没有 UV 时按三平面投影显示，编辑器和助手都会被提示。助手按 manifest 的 `uv = false` 判断（修订 v18），旧模型没有这个字段时不提示；基础体都带 UV |
| `mapping.size_m` | number 0.01…100 | 否 | 只在 `triplanar` 时允许，缺省 1：一块贴图的宽度（米），高度按图片宽高比 |
| `mapping.repeat` | `[u, v]`，各 0.01…1000 | 否 | 只在 `uv` 时允许，缺省 `[1, 1]` |
| `mapping.rotation_deg` | number 0…360 | 否 | 缺省 0 |
| `mapping.normal_strength` | number 0…4 | 否 | 缺省 1；没有法线贴图时也允许保存 |
| `alpha_mode` | `"opaque"` \| `"clip"` \| `"blend"` | 否 | 对应 glTF 的 OPAQUE / MASK / BLEND。缺省 `opaque`；有 `maps` 时存储时补齐。不带基础色贴图也允许，此时透明度只来自 `opacity` |
| `alpha_cutoff` | number 0…1 | 否 | 只在 `alpha_mode = clip` 时允许，缺省 0.5，存储时补齐 |

  - 贴图与常数参数相乘，语义同 three.js 和 glTF：基础色 = `color` × 贴图（按 sRGB 读取）；糙度、金属度 = 参数 × 所选通道；AO 只压暗间接光；法线贴图是切线空间，OpenGL 约定（+Y 朝上，同 glTF、Blender），乘 `normal_strength`；自发光 = `emissive` × 贴图（按 sRGB 读取）× `emissive_intensity`，同 glTF 的 `emissiveTexture`，`emissive` 为黑色时贴图不起作用（scene-pose-system 子项 2）。除基础色和自发光外按线性读取。
  - 透明方式：`opaque` 忽略贴图的透明通道，`opacity`、`transmission` 的行为与修订 v17 之前完全相同；`clip` 把 `opacity × 贴图透明通道` 低于 `alpha_cutoff` 的部分挖掉，留下的部分完全不透明，挖掉的部分不投影，屏幕外反射和探针 GI 里同样挖空；`blend` 把贴图透明通道乘到 `opacity` 上，按半透明物件处理（与 `opacity < 1` 相同），即使整张贴图都不透明。
  - 顶点色（scene-pose-system 子项 2）：`principled@1` 的 `params` 加布尔参数 `vertex_color`，缺省 `false`。打开时基础色再乘模型的 `COLOR_0`（按线性读取，同 glTF），与基础色贴图同时生效；带透明通道的 `COLOR_0` 只取 RGB。模型没有 `COLOR_0`、基础体和内置贴图材质上，这个参数不起作用。参数在材质目录里定义，命令接口照旧只校验外形。修订之前，没有分配材质的槽位按 GLB 自带的常数显示时已经带顶点色，这条规则不变。
  - 白模、线稿和线框不看材质（与 Blender 相同），贴图和透明方式都不体现；材质预览、最终渲染、`render_view` 和 `lit` 导出体现。
  - 单个场景最多引用 64 张不同的图片（跨所有槽位去重计数），超出返回 413 `creative_size_limit`。这是初始值，C5c1 实测后调整；scene-pose-system 子项 2 用带贴图的角色加布景实测，验收时决定是否调整。
- **灯光**（字段与 three.js 同名同义，渲染时原样赋给对应的灯光类）：

| `type` | three.js | 字段与缺省 |
|---|---|---|
| `directional` | `DirectionalLight` | `color`（#rrggbb，#ffffff）、`intensity`（0…1000，1）、`cast_shadow`（false） |
| `point` | `PointLight` | 以上三项，加 `distance`（米，≥ 0，0 表示不截断，缺省 0）、`decay`（0…10，2） |
| `spot` | `SpotLight` | `point` 的全部字段，加 `angle`（弧度，光锥中心到边缘的半角，0 < angle ≤ π/2，缺省 π/3）、`penumbra`（0…1，0） |

  - 写了该类型没有的字段返回 400 并指出字段（例如点光写了 `angle`）。
  - `intensity` 的含义同 three.js 默认的物理光照：点光和聚光为坎德拉（cd），平行光为 three.js 的强度值。
  - 方向：平行光和聚光沿物件局部 −Z 照射，与 three.js 摄像机、Blender 灯光一致。three.js 的 `target` 不进格式，渲染时由旋转算出；平行光的位置只影响阴影范围。
- **命名原则**（修订 v7）：助手要写的字段与 three.js 同名同义；three.js 没有对应字段的，保留带单位的名字（`sun_elevation_deg`、`focal_mm`、`sensor_mm`）。不接受别名，写错名字返回 400 并指出字段。变换的 `rotation` 保持四元数：three.js 里它对应 `quaternion`（three.js 的 `rotation` 是欧拉角），但两者长度不同，写错会被拒绝而不会悄悄出错，C3 试用中助手也全部写对。

### 访问模式与保证
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、生成参数、名称提示，不含代码和地址）和图片修订摘要（宽高、MIME、是否可用，修订 v17）。图片像素由编辑器按修订另取媒体读取票据（`role = original`），不经场景接口。GLB 和缩略图由编辑器按模型修订另取媒体读取票据（C2 修改）。内置模型不在模型修订摘要里：编辑器用随代码发布的清单副本，GLB 经 `api: 读取内置模型文件` 读取（scene-pose-system 子项 3）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型和图片可用（内置模型按清单判断）、单元版本单调。图片的可用性只在它第一次出现在场景里时检查，已被当前修订引用的图片批量复核，与模型相同（修订 v17）。
  - 由数据库保证：修订号主键唯一、`account_id` 限定、内容类型 CHECK。
- **历史**：按 `revision DESC` 分页列出修订；`changed_units` 直接读，不比对快照。

### 生命周期与演进
- 进回收站与彻底删除沿用通用文档层。彻底删除时删除修订行和引用行，之后模型修订和图片修订失去持有方，可被 GC 回收。
- 内容类型 CHECK 加 `scene`。回滚时如已有 `scene` 内容则拒绝，前向修复。
- 快照 `schema_version` 升级时，读取端兼容旧版本，写入只写新版本；不做批量回填。
- 例外：修订 v7 的改名（雾和摄像机的 `near_m`/`far_m` → `near`/`far`，灯光 `range_m` → `distance`、`angle_deg` → `angle` 并补 `decay`）发生在上线之前，生产库没有场景数据。由一次迁移改写已有的场景快照，`schema_version` 仍为 1，不保留旧字段的读取兼容。旧的 `angle_deg` 没有定义是整锥角还是半角，也从未渲染过，迁移按整锥角换算（`angle = angle_deg / 2`，换成弧度）；2026-10-18 开发库里没有灯光物件。
- 修订 v8（C4 起草时）：摄像机视场角改为画面长边对应传感器长边（原为固定按 `sensor_mm[1]` 算垂直视场角，竖幅画面比同焦距的全画幅相机窄约 1.5 倍）。只改渲染端的换算，字段外形和存储不变，不迁移；默认传感器下 3:2 横幅的画面不变，助手已建的非 3:2 摄像机和 `render_view` 临时视点的取景随之改变。
- 修订 v8（C4 起草时）：摄像机新增 `depth_of_field`、`f_stop`、`focus`、`focus_object_id`。不迁移：读取时补齐默认值（服务端解码时补齐，读取场景的响应里也带齐），之后的写入按完整字段存储。「空物体」是没有子物件的 `group`，不新增 `kind`。
- 修订 v10（C5 起草时）：`render` 新增 `exposure_ev`。灯光按 three.js 的物理单位（坎德拉），室外有太阳和只有几盏灯的室内亮度差几个数量级，固定曝光无法同时照顾两者，所以由场景自己定曝光，与摄影里按场景定曝光一致；用「档」而不是倍数，与相机的曝光补偿、Blender 色彩管理的曝光相同。不迁移：读取时补 0，读取场景的响应里也带齐。
- 修订 v13（C5b 起草时）：基础体的 `primitive` 新增可选的 `faces`。不迁移：没有这个字段就是没有面分配。回滚到不认识它的后端会读取失败，只做前向修复。
- 修订 v17（C5c1 起草时）：材质槽新增可选的 `maps`、`mapping`、`alpha_mode`、`alpha_cutoff`，引用表开始登记图片。不迁移：没有这些字段就是没有贴图、`opaque`，渲染与之前相同。改了 `maps` 的槽位会进入渲染指纹（指纹已整体计入 `material_slots`），已有的 `lit` 导出图照常提示过时；没改的场景指纹不变。回滚到不认识这些字段的后端会读取失败，只做前向修复。
- 修订 v20（C5c2 起草时）：`kind = textured` 的预设可以带 `mapping`、`alpha_mode`、`alpha_cutoff`。只放宽校验，不迁移；旧编辑器不认识这些预设，按未知预设显示中性灰，所以前后端同一版本上线。内置贴图不进引用表，也不计入单场景 64 张图片的上限。
- `scene-webgpu-renderer` R2：`render` 新增 `engine` 和 `resolution`。两者是场景的渲染设置而不是本机偏好，这样编辑器、`render_view` 和导出看到同样的画面，摄影师、其他标签页和助手也一致。不迁移：读取时补 `workbench`、`2k`，读取场景的响应里也带齐。回滚到不认识这两个字段的后端会读取失败，只做前向修复。
- scene-pose-system 子项 2（外部 GLB 导入）：`maps` 加 `emissive`，`principled@1` 加参数 `vertex_color`。不迁移：没有这两项就是没有自发光贴图、不乘顶点色，渲染与之前相同；自发光贴图的图片进引用表、计入 64 张上限，与其他贴图相同。旧编辑器不认识 `emissive` 键会读取失败，前后端同一版本上线，回滚只做前向修复。OpenAPI 的 `CreativeSceneTextureMaps` 加可选的 `emissive`。
- scene-pose-system 子项 3（蒙皮人形角色）：`objects[].model` 可以写 `{builtin}`（`data: 内置模型库`），与 `content_revision_id` 二选一。不迁移：已有物件都是 `content_revision_id`。渲染指纹按 `builtin` 的条目 ID 计入模型身份（条目发布后不变，与内容修订 ID 同样稳定），没有内置模型的场景指纹不变。OpenAPI 的场景模型引用改为二选一的两种结构。旧编辑器和旧后端都不认识 `builtin`，前后端同一版本上线，回滚只做前向修复。
- `scene-transparent-background`：`render` 新增 `transparent_background`（缺省 `false`），经 `set_render` 按键合并读写（非布尔值或 `null` 返回 400 `validation_failed`）。不迁移：存储层（`encodeScene`）在值为 `false` 时省略该键、仅在值为 `true` 时写入 `"transparent_background": true`，而内存模型与全部读取出口（读取场景、读取历史修订、`read_scene`、冲突报告）始终返回完整布尔值；OpenAPI 的 `CreativeSceneRender` 增加必返的 `transparent_background`，`CreativeSceneRenderPatch` 增加可选的 `transparent_background`。未开启透明背景的新建或编辑场景（以及开启后又关回的最新修订）在库内不含该键，回滚到旧后端完全可读；已写入 `transparent_background: true` 的历史修订仍可能用于历史读取或恢复，关闭开关只影响新写入的修订。前后端与 worker 同版本上线，含 `true` 的修订适用既定的前向修复策略。

## data: MODIFY 场景客户端任务

### 用途与存储角色
- **用途**：服务端派给创意空间页面（渲染端）执行的工作（烘焙模型、回看截图、导出），以及它的认领、租约、结果和来源助手运行。
- **存储**：新表 `creative_scene_jobs`（PostgreSQL）。
- **角色**：权威主数据（任务状态）。产物本身在暂存上传或内容里，不在本表。
- **所有权**：`creativescene` 创建、过期、取消；渲染端经认领、续租、完成接口推进；`creativeagent` 经 `creativescene` 的终态端口读取任务结果，并前台在终态时继续运行，后台按「助手耗时工具任务与完成通知」发布结果。

### 结构与身份

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `id` | text | 是 | `csjob_<uuid>`，主键 |
| `account_id` | text | 是 | 账号隔离 |
| `document_id` | text | 是 | 目标场景 |
| `kind` | text | 是 | `bake_model` / `render_view` / `export_shots`（CHECK） |
| `request` | jsonb | 是 | 按 kind 定义，见下 |
| `state` | text | 是 | `queued`/`claimed`/`succeeded`/`failed`/`expired`/`cancelled`（CHECK） |
| `attempt` | integer | 是 | 从 0 开始，每次租约过期回到 queued 时加 1；达到 3 时不再回队，直接转 `failed`（`host_error`） |
| `claim_token` | text | 认领时 | 随机令牌，过期或终态时清空 |
| `editor_session_id` | text | 认领时 | 认领者标签页 |
| `lease_until` | timestamptz | 认领时 | 租约到期时间 |
| `deadline` | timestamptz | 是 | 创建时间 + 30 分钟 |
| `run_id` / `tool_call_id` | text | 助手创建时 | 关联运行与工具调用；摄影师发起的导出为 null |
| `result` | jsonb | 终态时 | 见下 |
| `created_at` / `updated_at` | timestamptz | 是 | — |

`request`：
- `bake_model`：`{code, params, lib_version, name_hint, target, expected}`。
  - `target` 为 `{op:"add", object:{…}}`（新物件的名称、变换、父级等，字段同 `add_objects` 的条目，不含 `model`），或 `{op:"replace", object_id, scope:"this"|"all_instances", object_ids}`；`all_instances` 时 `object_ids` 是建任务时共用同一模型修订的全部实例，此后不再展开。
  - `expected`：建任务时冻结的替换目标内容单元版本（`{"object:<id>:content": rev}`）；`add` 为空对象，完成时按当时的 `objects` 版本写入。
- `render_view`：三选一，加上 `mode`（`clay` 或 `lit`，缺省 `clay`；`lit` 自修订 v10 起，与 `lit` 导出同一套渲染：材质、灯光物件、天空、阴影和 AO）：
  - `{camera_object_id}`：按场景里的摄像机，画幅取摄像机的比例；
  - `{viewpoint:{position, target, focal_mm}}`：临时视点，4:3；
  - `{preset:"overview"|"top"|"front"|"side"}`：按全部可见物件的包围盒自动取景，4:3（总览为斜上方 3/4 视角）。
  - `depth_of_field`（修订 v8，可选，默认 `false`）：`true` 时按这台摄像机的光圈和对焦渲染虚化，画面与白模导出同一套渲染（含 AO），长边仍为 1024。只能与 `camera_object_id` 一起用，并且建任务时这台摄像机的 `depth_of_field` 必须为 `true`，否则工具直接返回错误、不建任务；渲染时按当时快照里的光圈和对焦，不再检查开关。缺省时全清晰。修订 v9：截图与编辑器视口、白模导出同一套渲染，带 AO；`depth_of_field` 只决定是否虚化。
- `export_shots`：`{camera_object_ids, passes, canvas_id, scene_node_id, transparent_background?}`（C4；分镜字段见 `data: 场景时间线`「客户端任务的分镜字段」；`transparent_background` 自 `scene-transparent-background` 起）。
  - `camera_object_ids`：建任务时当前快照里的摄像机（工具未指定时为全部摄像机，按物件顺序）；`passes`：`clay` / `line` / `lit` 的子集，不重复。图片总数 ≤ 48（`api: 导出分镜` 的上限）。
  - `canvas_id`、`scene_node_id`：运行所在画布，以及该画布上这个场景的节点（有多个时取最早创建的）；建任务时确定，场景不在该画布上时不建任务。
  - `transparent_background`（可选布尔值）：助手调用 `export_shots` 显式指定本次导出的 `clay`、`lit` 是否使用透明背景（`line` 恒为白底），直接覆盖执行时场景快照的 `render.transparent_background` 而不改写场景文档，也不受其他标签页切换场景开关的影响；省略时渲染端按执行时刻场景快照的 `render.transparent_background` 出图。服务端在完成事务里以实际渲染修订和任务覆盖值核对渲染端回报的 `effective_transparent_background`。校验通过后，显式覆盖值原样写入导出节点的 `based_on.transparent_background`（见 `api: 完成场景客户端任务`、`data: 导出图来源`）。
  - 渲染端出图上传后，把图片清单随完成任务提交（`api: 完成场景客户端任务` 的 `export`），节点在完成事务里创建。任务没有成功，画布上就没有节点；同一任务最多一组节点。

`result`：
- 成功：`bake_model` 为 `{content_id, content_revision_id, object_ids, revision}`；`render_view` 为 `{image_upload_id, content_revision_id, scene_revision}`（暂存截图及其实际渲染的场景修订）；`export_shots` 与 `api: 导出分镜` 的成功响应相同：`{change_id, scene_revision, nodes:[{camera_object_id, pass, node_id, content_revision_id, lens}]}`（`lens` 见 `data: 导出图来源`，修订 v9）。
- 失败：`{code, message}`。`code` 取完成接口的错误码，或服务端写入的 `conflict`（场景单元冲突）、`invalid_result`（结果不合规，例如 manifest 与 GLB 不符、物件超出上限、父级不存在；导出无法放置，例如画布已归档、场景节点已不在画布）、`host_error`（第 3 次租约过期）、`expired`、`cancelled`。`conflict` 时另带 `conflicts`（单元、预期与当前版本、改动者）和 `bake`（`{glb_upload_id, thumbnail_upload_id, module, manifest}`），助手重试时复用这份烘焙产物，不重新编译。

索引：`(account_id, state, created_at)`，用于账号级认领；`(account_id, document_id, state, created_at)`，用于按场景认领；`(state, deadline)`，用于过期扫描；`(run_id)`，用于取消运行时找到它的任务。

### 访问模式与保证
- **认领**：取最早的 `queued` 任务（指定场景时只在该场景内），用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。没有任务时只走索引、不加锁，渲染端在每次失效通知时都会调用。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。终态进入关联任务收尾，前台继续运行，后台生成完成通知。
- **完成**：前台按既有能力、运行期限、Skill 与外发同意复查，验证令牌和租约，落产物并写终态；运行侧在同一事务里写耗时工具任务终态并续跑，不经收尾器。前台复查不通过时按原规则结束运行并取消任务。后台按受理时的账号、对话、目标和任务自身期限复查，仍检查创意能力、租约、令牌和领域写入条件，来源运行作为溯源；写回场景不是外发，不复核外发同意。后台准入拒绝时记录可解释的任务终态。后台任务的终态由源表触发器标记收尾，收尾器写完成通知。无耗时工具任务关联的存量场景任务沿用原终态端口。
- **取消**：运行停止或终止时，同事务把属于前台的非终态场景任务改为 `cancelled` 并清空令牌。后台场景任务由对应后台句柄控制取消，和渲染端完成争同一源任务锁。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。修订 v10：`render_view` 的 `mode` 增加 `lit`（C5），请求外形不变，旧任务缺省按 `clay`。`scene-transparent-background`：`export_shots` 的 `request` 增加可选布尔字段 `transparent_background`，未带该字段的任务照旧按渲染时刻的场景设置出图。

## data: MODIFY 导出图来源

- **用途**：记录画布图片节点由哪个场景、哪个修订、哪个分镜（或哪台摄像机）、哪种导出方式生成，用于提示过时。
- **存储**：`creative_nodes.config` 内的 `based_on` 对象，只对导出产生的 `core.image` 节点写入：`{scene_document_id, scene_revision: CreativeRevision, marker_id, frame, camera_object_id, pass:"clay"|"line"|"lit", content_revision_id, fingerprint, lens, transparent_background?}`。
  - `marker_id`、`frame`（`scene-pose-system` 子项 1）：按分镜导出时为分镜 ID 和导出时它的帧号；按摄像机导出时 `marker_id` 为 null、`frame` 为 0。旧节点没有这两个字段，按 null 和 0 读取。
  - `camera_object_id` 是渲染所用的摄像机（按分镜导出时取导出时分镜绑定的摄像机）。
  - `content_revision_id` 是导出时放进节点的那份图片内容修订。
  - `fingerprint` 是 `scene_revision` 的快照按 `frame` 求值后，对这台摄像机、这种导出方式的渲染指纹（带版本前缀，如 `v1:`），客户端只当不透明字符串。
  - `transparent_background`（`scene-transparent-background`，可选布尔值）：仅当助手 `export_shots` 显式传入 `transparent_background` 参数时写入（`true` 或 `false`），记录这张图按哪一种透明背景覆盖设置导出；摄影师手动导出和未传该参数的 `export_shots` 省略该键，表示跟随场景的 `render.transparent_background`。
- **镜头快照 `lens`**（修订 v9）：导出时这台摄像机在这一帧求值后的镜头，`{focal_mm, depth_of_field, f_stop, focus_m, focus_object_name}`。`focus_m` 是实际对焦距离（米）：有对焦物件时按物件原点沿视线的深度算出，否则为 `focus`，并夹在 `near`、`far` 之间；`focus_object_name` 是对焦物件的名称，按距离对焦或对焦物件已删除时为空字符串。景深关闭时也照样记录。`lens` 只说明这张图是怎么拍的，不参与过时判断（指纹已计入这些参数）；没有 `lens` 的节点按未知处理。
- **角色**：派生信息，只用于提示，不参与权限和内容。
- **写入**：只由导出的放置流程经画布命令层写入，入口是「导出分镜」接口和 `export_shots` 的完成事务；画布命令的 `add_node` 不接受 `config`，客户端无法写入。写入后不再修改：节点当前内容不是 `content_revision_id`（换了图、切了版本）时，来源信息不生效，所以改图的操作不需要逐一清除它。复制节点时随 `config` 一起复制，内容相同，判断照样成立。
- **读取**：画布节点快照新增可选只读字段 `based_on_state`，只出现在 `based_on` 生效的节点上（不生效时不返回）：
  - `unavailable`：来源场景不存在、在回收站或快照不可展示，不显示过时；
  - `current`：场景之后没有与这张图相关的修改；
  - `stale`：有，前端显示「场景已更新」。
- **助手读取**（修订 v9）：`read_canvas` 返回的节点里，`based_on` 生效的导出图带 `export_source: {scene_document_id, scene_revision, marker_id, frame, camera_object_id, pass, lens, state}`，`state` 即 `based_on_state`。画布的 HTTP 节点快照不变，仍只返回 `based_on_state`。
- **过时判断**：读取时计算，不落库。对场景当前快照按同样的方式求值、算同一种导出方式的渲染指纹，与 `fingerprint` 比较，不同即 `stale`：
  - 按分镜导出的节点：在当前快照里按 `marker_id` 找到分镜，按它**当前的**帧号求值，用它**当前绑定的**摄像机算指纹。分镜已删除、未绑定摄像机或摄像机已删除，都算 `stale`。
    - 分镜挪到别的帧（关键帧随它一起挪），求值结果相同时不算过时。
    - **分镜换绑另一台摄像机算 `stale`**，即使两台摄像机的参数和位置完全相同：指纹计入摄像机 ID，这一点与本条修改之前相同，旧图的指纹因此不变。
  - 按摄像机导出的节点（含旧节点）：按第 0 帧求值，用 `camera_object_id` 算指纹；这台摄像机已不在当前快照里算 `stale`。
  - 指纹只计入求值结果，不计入 `timeline` 和通道本身。所以只改别的分镜的关键帧，这一张不会过时；没有通道和分镜的场景，指纹与本条修改之前完全相同，**指纹版本不升级**。
  - 指纹计入哪些内容见 `scene-editor-3d` design「渲染模式与导出」，按导出方式区分。改动后又改回原样不算过时。指纹版本与当前不同时算 `stale`，不读旧快照重算（修订 v9，读画布不增加查询；指纹版本只在渲染规则变化时升级，届时旧图本来就该重新导出）。
  - 透明背景（`scene-transparent-background`）：`clay` 和 `lit` 的渲染指纹计入有效透明背景布尔值，`line` 不计入（线稿恒为白底）。落位指纹以 `scene_revision` 对应的实际渲染快照计算，过时检查以画布读取时的当前快照计算。节点 `based_on.transparent_background` 存在（助手按次显式指定导出）时，两次计算均用该布尔值替代各自快照的场景开关，因此单独切换场景开关不影响过时状态。几何、摄像机、材质、灯光等相关改动仍照常触发 `stale`，包括渲染完成后、落位前发生的改动。覆盖值省略时按各自快照的场景开关计算，其他渲染输入相同的情况下，切换开关使 `clay`、`lit` 变为 `stale`、切回恢复 `current`。有效值为 `false` 时从指纹输入里省略该键，指纹版本前缀 `v1:` 与上线前导出的图哈希保持不变。

## api: MODIFY 读取场景

### 用途与调用
- **用途**：编辑器打开或重读场景时，取得当前快照和渲染所需的模型信息。
- **消费者**：前端场景编辑器；助手工具 `read_scene` 走领域层，不经 HTTP。
- **调用**：`GET /api/v1/creative/documents/{id}`，响应为 `application/json`。这是通用读取接口在 `type_key = "scene"` 时的分支：通用字段（`id`、`type_key`、`title`、`revision`、`trashed_at`）由底座的 `api: 读取文档` 定义，本条目定义 `scene` 分支增加的字段。
- **权限**：已登录且账号具备创意空间能力；场景必须属于当前账号，否则返回 404。回收站中的场景可以读取，响应标明 `trashed_at`。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景文档 ID |

无查询参数和请求体。

### 成功响应
200：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `id`、`type_key`、`title`、`revision`、`content_revision_id`、`unit_revs`、`trashed_at`、`created_at`、`updated_at` | — | 是 | — | 通用字段，见底座 `api: 读取文档`；`type_key` 为 `"scene"`；场景的单元命名见 `data: 场景文档` |
| `scene` | object | 是 | 否 | 快照里的 `scene`，结构见 `data: 场景文档` |
| `models` | object | 是 | 否 | 以 `content_revision_id` 为键，覆盖快照引用的全部模型；没有模型实例时为 `{}` |
| `models.*.content_id` | string | 是 | 否 | 模型内容 ID |
| `models.*.usable` | boolean | 是 | 否 | false 时 `manifest`、`generator` 为 null，编辑器显示「模型不可用」占位 |
| `models.*.manifest` | object | 是 | 是 | 见几何模块契约「产物」；取服务端从 GLB 算出的值 |
| `models.*.generator` | object | 是 | 是 | `{language, contract_version, lib_version, code_sha256, params_schema, params, mount}`，**不含代码** |
| `models.*.name_hint` | string | 是 | 是 | 模型名称提示 |
| `models.*.faces` | object | 是 | 是 | 面级材质分配（修订 v13），结构见 `data: 模型内容`；没有时为 null。编辑器加载 GLB 后按它拆分槽位 |
| `images` | object | 是 | 否 | 修订 v17。以图片 `content_revision_id` 为键，覆盖当前快照引用的全部图片（目前只有 `material_slots.*.maps.*`）；没有贴图时为 `{}`。不列历史修订引用的图片 |
| `images.*.usable` | boolean | 是 | 否 | false 表示图片当前不可展示。场景持有期间图片不会被 GC 回收，所以只在内容状态异常时出现；此时 `width`、`height`、`mime` 为 null，编辑器在槽位上显示「图片不可用」，渲染按没有这张贴图处理 |
| `images.*.width`、`images.*.height` | integer | 是 | 是 | 原图像素尺寸（已按 EXIF 方向换算），编辑器据此决定缩放到多大再上传 GPU |
| `images.*.mime` | string | 是 | 是 | `image/jpeg`、`image/png` 或 `image/webp`；PNG、WebP 可能带透明通道 |

- **GLB 与缩略图**：响应不含地址。编辑器按模型修订调用现有的 `POST /creative/media-access-tickets`（`content_revision_id`，`role` 为 `original` 取 GLB、`thumbnail` 取缩略图，`purpose` 为 `display`），按修订缓存，与画布图片的做法一致。
- **代码**：通过 `GET /creative/content-revisions/{content_revision_id}` 单独读取（载荷的 `model.generator.code`），避免每次重读快照都传几十 KB 代码。
- **图片像素**（修订 v17；`scene-transparent-background` 更新）：响应不含地址。编辑器按图片修订调用 `POST /creative/media-access-tickets`（`role = original`、`purpose = display`）取原图：展示图（`role = display`，长边超过 1600 px 时生成，缩至长边 1600 px；不透明原图编为有损 JPEG，含透明像素的 PNG/WebP 原图编为保留透明通道的无损 `image/png`）已缩小分辨率且对不透明图有损压缩，不能用作贴图。
- **性能**：模型摘要和图片摘要在同一个读快照事务里批量读取，查询次数不随模型数、图片数增长。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 401 | 现有认证错误码 | 未登录或会话过期 | 走现有续期 |
| 404 | `not_found` | 不存在或不属于当前账号 | 关闭编辑视图，回到画布 |

### 交互规则
只读，没有分页。重复调用没有副作用。

### 调用示例
```http
GET /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10
Authorization: Bearer <token>
```
```json
{
  "id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10",
  "type_key": "scene",
  "title": "黄昏教堂中殿",
  "revision": "6",
  "content_revision_id": "ccrv_3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7",
  "unit_revs": {"meta": "1", "environment": "3", "render": "1", "objects": "6",
                "object:sobj_V1StGXR8_Z5jdHi6B-myT": "1", "object:sobj_V1StGXR8_Z5jdHi6B-myT:content": "1",
                "object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "6", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "6"},
  "trashed_at": null,
  "created_at": "2026-10-20T08:00:00Z",
  "updated_at": "2026-10-22T10:00:00Z",
  "scene": {
    "environment": {
      "sky": {"kind": "procedural", "sun_elevation_deg": 12, "sun_azimuth_deg": 240, "turbidity": 4},
      "ambient": {"color": "#ffffff", "intensity": 0.4},
      "fog": null
    },
    "render": {"mode": "clay"},
    "objects": [
      {"id": "sobj_V1StGXR8_Z5jdHi6B-myT", "kind": "primitive", "name": "地面", "parent_id": null, "order": 0,
       "transform": {"position": [0, 0, 0], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": true,
       "primitive": {"builtin": "ground@1", "params": {"size": 40}},
       "material_slots": {"main": {
         "preset": "principled@1",
         "params": {"color": "#ffffff", "roughness": 1, "metalness": 1, "opacity": 1, "transmission": 0},
         "maps": {
           "base_color": {"content_revision_id": "ccrv_1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"},
           "normal": {"content_revision_id": "ccrv_2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d6e"},
           "roughness": {"content_revision_id": "ccrv_3c4d5e6f-7a8b-4c9d-8e0f-2a3b4c5d6e7f", "channel": "g"},
           "metalness": {"content_revision_id": "ccrv_3c4d5e6f-7a8b-4c9d-8e0f-2a3b4c5d6e7f", "channel": "b"},
           "ao": {"content_revision_id": "ccrv_3c4d5e6f-7a8b-4c9d-8e0f-2a3b4c5d6e7f", "channel": "r"}},
         "mapping": {"kind": "triplanar", "size_m": 2, "rotation_deg": 0, "normal_strength": 1},
         "alpha_mode": "opaque"}}},
      {"id": "sobj_Z1x2C3v4B5n6M7a8S9d0f", "kind": "model_instance", "name": "左侧柱 1", "parent_id": null, "order": 3,
       "transform": {"position": [-3, 0, -2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": false,
       "model": {"content_revision_id": "ccrv_9e2a0d4c-1b3f-4a5e-8c7d-6f5e4d3c2b1a"}, "material_slots": {}}
    ]
  },
  "models": {
    "ccrv_9e2a0d4c-1b3f-4a5e-8c7d-6f5e4d3c2b1a": {
      "content_id": "ccnt_51c0e8a2-7d6b-4c3a-9f1e-2d4c6b8a0e1f",
      "usable": true,
      "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                   "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []},
      "generator": {"language": "three-module", "contract_version": 1, "lib_version": 1,
                    "code_sha256": "sha256-6f1ed002ab5595859014ebf0951522d9f2b8d1c5e8a6a7c3b1d0e9f8a7b6c5d4",
                    "params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                    "params": {"height": 6}, "mount": "floor"},
      "name_hint": "哥特束柱"
    }
  },
  "images": {
    "ccrv_1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d": {"usable": true, "width": 2048, "height": 2048, "mime": "image/jpeg"},
    "ccrv_2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d6e": {"usable": true, "width": 2048, "height": 2048, "mime": "image/png"},
    "ccrv_3c4d5e6f-7a8b-4c9d-8e0f-2a3b4c5d6e7f": {"usable": false, "width": null, "height": null, "mime": null}
  }
}
```
上例中 ORM 图（`ccrv_3c4d…`）已不可用：编辑器在地面槽位上显示「图片不可用」，糙度、金属度、AO 按常数参数渲染；离屏出图（`lit` 导出、`render_view`）遇到不可用的图片直接失败，不出缺贴图的图。
失败示例：
```http
GET /api/v1/creative/documents/cwdoc_not_mine
```
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```

### 兼容与变更
- 在通用读取接口上新增 `scene` 分支，其他类型不受影响。
- C2 修改：`models.*` 去掉 `glb_url`、`thumbnail_url`（C1 期间 `models` 恒为空，没有调用方用过这两个字段），加 `name_hint` 和 `generator.code_sha256`；去掉 503（读取不再签发票据）。
- 修订 v13（C5b）：模型摘要增加 `faces`，基础体的 `primitive.faces` 随快照返回。只增加字段，旧客户端忽略。
- 修订 v17（C5c1）：响应增加必返的 `images`（没有贴图时为 `{}`），快照的材质槽可能带 `maps`、`mapping`、`alpha_mode`、`alpha_cutoff`。只增加字段；旧编辑器忽略它们，按常数参数渲染，但会在下次整体替换槽位时丢掉贴图，所以前后端同一版本上线。
- `scene-transparent-background`：响应 `scene.render` 增加必返的布尔字段 `transparent_background`（上线前旧快照与省略存储 `false` 的快照在读取时均补齐返回 `false`，对应 OpenAPI `CreativeSceneRender`）；服务端校验长边超过 1600 px 的上传图片（含场景导出图）时，含透明像素的 PNG/WebP 原图生成长边 1600 px 的无损 `image/png` 展示图（保留透明通道），JPEG 和完全不透明的 PNG/WebP 原图仍生成有损 JPEG 展示图。只增加响应字段并修正透明展示图，旧客户端忽略未知展示字段，前后端同一版本上线。

## api: MODIFY 完成场景客户端任务

### 用途与调用
- **用途**：渲染端提交任务结果（成功或失败）。服务端在同一事务里落结果、改终态并登记助手任务收尾条件。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/complete`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：任务属于当前账号，`claim_token` 匹配，且租约未过期。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `claim_token` | string | 是 | — |
| `outcome` | `"succeeded"` \| `"failed"` | 是 | — |
| `error` | `{code, message}` | 失败时 | `code` 取值：`compile_error`、`compile_timeout`、`limit_exceeded`、`render_error`、`host_error`（宿主侧失败：媒体校验失败、WebGL 上下文丢失、代码加载失败等）；`message` ≤ 4000 字符，会原样交给助手 |
| `bake` | object | `kind=bake_model` 且成功时 | `{glb_upload_id, thumbnail_upload_id, manifest, module, faces}`。`faces` 是渲染端从替换前的修订按 `data: 模型内容`「重新生成时」带过来的面级分配，没有时省略，原样写入 `commit_model.faces`（修订 v13）。任务结果的外形不变：保留和作废了多少，由工具层比较新旧两个模型修订的分配得出。`module` 为 `{params_schema, params, mount}`：沙箱回传、宿主按服务端规则校验过的规范化模块信息。服务端用任务 `request` 里的代码计算 `code_sha256`、组装 `generator`，按 `api: 场景模型命令` 的规则校验，再组装 `commit_model` 和 `add_objects` / `set_instance_model`，经场景命令写入，写入者记为助手的 `run_id`；替换目标的预期版本取任务 `request.expected` |
| `render` | object | `kind=render_view` 且成功时 | `{image_upload_id, scene_revision}`：暂存图片（JPEG，长边 1024），以及渲染端实际渲染的场景修订（不得大于当前修订） |
| `export` | object | `kind=export_shots` 且成功时 | `{scene_revision, shots, effective_transparent_background?}`：实际渲染修订、图片清单和实际背景模式。`shots` 的结构与 `api: 导出分镜` 相同，必须正好覆盖任务 `request` 的摄像机和导出方式。服务端在保存点内按 `api: 导出分镜` 的规则放置节点（位置、标题、`based_on`），放到任务 `request` 的画布和场景节点旁，节点按 `request.camera_object_ids` 的顺序排行，写入者记为助手的 `run_id` |

### 成功响应
200：`{job_id, state, result}`，`result` 结构见 `data: 场景客户端任务`。`state` 为 `succeeded` 或 `failed`。下列情况任务记为 `failed` 并照常返回 200，渲染端不重试：
- 写入场景时单元冲突，例如替换目标在烘焙期间被摄影师改过或删除（`result.code = "conflict"`，带冲突信息和烘焙产物）；
- 结果内容不合规：manifest 与服务端从 GLB 算出的不符、参数不符合定义、物件超出上限、父级不存在（`result.code = "invalid_result"`）；
- 导出无法放置：画布已归档、场景节点已不在画布上或不指向这个场景、场景在回收站或已删除、摄像机在所报修订里不存在、`scene_revision` 晚于当前修订、画布节点超出上限（`result.code = "invalid_result"`，`message` 说明原因）。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 请求外形不对：缺少与 kind 对应的结果字段、错误码非法、`export.shots` 与任务请求的摄像机和导出方式不一致。任务不变 | 修正 |
| 404 | `not_found` | 任务不存在或不属于当前账号 | 放弃 |
| 409 | `creative_state_conflict` | 任务已不是 `claimed`、令牌不匹配或租约已失效；或前台关联运行复查不通过（运行已结束，任务已取消） | 丢弃产物；任务会被重新认领、已过期或已取消 |
| 409 | `creative_upload_unavailable` | 暂存上传不存在、已过期或已被消费 | 租约仍有效时重新上传，用新的 operation id 重试 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
- **背景模式校验**（`scene-transparent-background`）：由任务请求中显式的 `transparent_background`（含 `false`）覆盖实际渲染修订的场景开关，得到白模和材质光照的期望背景模式。含白模或材质光照，且任务显式覆盖或期望值为 `true` 时，`export.effective_transparent_background` 必传。仅线稿的期望值为 `false`，可省略。字段存在时必须匹配期望，显式 `null` 或其他非布尔输入返回 `400 validation_failed`（任务不变）。应传而缺少或值不匹配时，任务按 `invalid_result` 失败并返回 200，提示「导出背景设置不一致，请刷新页面后重新导出」。校验在消费暂存图和创建节点前执行，整批拒绝。已识别失败的新页面丢弃暂存图，旧页面遗留的暂存图由现有过期清理回收。刷新后重新发起导出。
- **原子性**：结果落库与任务终态在同一事务里。前台在同一事务记录助手工具任务终态并续跑，存量任务沿用原有续跑路径。后台在同一事务标记待收尾，随后由收尾器发布完成通知。场景写入和导出的画布放置在保存点内执行，冲突、结果不合规或无法放置时回滚到保存点，任务记为 `failed`（带原因），前台运行或后台通知随后将结果送给助手。导出的节点只在任务成功时创建。
- **幂等**：沿用公共约定。网络中断后用同一 key 重试或查回执。

### 调用示例
```json
{"operation_id": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", "client_created_at": "2026-10-22T10:05:12Z",
 "payload": {"claim_token": "clm_Q2w3E4r5T6y7U8i9", "outcome": "succeeded", "render": {"image_upload_id": "ccup_cc33", "scene_revision": "12"}}}
```
```json
{"job_id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "state": "succeeded",
 "result": {"image_upload_id": "ccup_cc33", "content_revision_id": "ccrv_77aa", "scene_revision": "12"}}
```
烘焙成功示例（请求）：
```json
{"operation_id": "5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f", "client_created_at": "2026-10-22T10:06:40Z",
 "payload": {"claim_token": "clm_Z9y8X7w6V5u4T3s2", "outcome": "succeeded",
             "bake": {"glb_upload_id": "ccup_g1", "thumbnail_upload_id": "ccup_t1",
                      "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                                   "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []},
                      "module": {"params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                                 "params": {"height": 6}, "mount": "floor"}}}}
```
编译失败示例（请求）：
```json
{"operation_id": "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f", "client_created_at": "2026-10-22T10:07:00Z",
 "payload": {"claim_token": "clm_Z9y8X7w6V5u4T3s2", "outcome": "failed",
             "error": {"code": "compile_error", "message": "ReferenceError: archCount is not defined (line 12)"}}}
```
导出成功示例（请求，修订 v8）：
```json
{"operation_id": "7b8c9d0e-1f2a-4b3c-8d4e-5f6a7b8c9d0e", "client_created_at": "2026-10-25T14:02:30Z",
 "payload": {"claim_token": "clm_K1j2H3g4F5d6S7a8", "outcome": "succeeded",
             "export": {"scene_revision": "18",
                        "shots": [{"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
                                   "images": [{"pass": "clay", "upload_id": "ccup_dd44"}, {"pass": "line", "upload_id": "ccup_ee55"}]}]}}}
```

### 兼容与变更
首次定义。修订 v6：`bake` 增加 `module`；`render` 增加 `scene_revision`；错误码增加 `host_error`；暂存上传不可用改用 `creative_upload_unavailable`（与 `api: 场景模型命令` 一致）；冲突与结果不合规记为任务 `failed` 并返回 200（`creativeops` 只提交 2xx 回执）；完成时复查关联运行。修订 v8（C4 起草时）：`export` 由 `{export_operation_id}` 改为 `{scene_revision, shots}`，导出节点在完成事务里创建，不再经 `api: 导出分镜`。C3 只做了占位（服务端对 `export` 结果直接拒绝，没有调用方），不需要兼容。修订 v13（C5b）：`bake` 增加可选的 `faces`；旧渲染端烘焙时不带 `faces`，等于全部作废，服务端照常写入。
- 本次扩展后台任务的完成准入和收尾。后台任务在本账号内核对已受理的对话、目标、任务期限和租约，来源运行可已终止；不复核外发同意。前台任务的完成事务照旧续跑运行。后台任务归属、终态和通知语义见「助手耗时工具任务与完成通知」。
- `scene-transparent-background`：任务完成的 `export` 增设条件必传的背景模式回报。传统不透明任务和仅线稿任务仍接受旧请求外形。

## api: MODIFY 导出分镜

### 用途与调用
- **用途**：把渲染端出的导出图落成画布图片节点，并记录 `based_on`。
- **消费者**：前端编辑视图（摄影师点导出）。助手导出不调用本接口，图片清单随 `api: 完成场景客户端任务` 提交，放置规则与本接口相同。
- **调用**：`POST /api/v1/creative/documents/{id}/scene-exports`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：场景和目标画布都属于当前账号，画布未归档，且场景节点在该画布上、指向这个场景。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `canvas_id` | string | 是 | 场景节点所在画布 |
| `scene_node_id` | string | 是 | 场景节点；新节点放在它的右侧，与它同一父级 |
| `scene_revision` | CreativeRevision | 是 | 出图时的场景修订；必须等于或早于当前修订，并且仍存在 |
| `effective_transparent_background` | boolean | 条件必传 | 渲染端实际给白模、材质光照使用的背景模式。含任一该导出方式且 `scene_revision` 对应的场景开关为 `true` 时必传。仅线稿的有效值为 `false` |
| `shots` | array，1–32 | 是 | 每个分镜（或每台摄像机）一项；节点按这个顺序逐行排列 |
| `shots[].marker_id` | string | 与 `camera_object_id` 二选一 | 在 `scene_revision` 的 `timeline.markers` 里存在，且绑定的摄像机在该修订里存在、`kind=camera`。分镜不重复。渲染端按这个分镜的帧求值、用它的摄像机出图 |
| `shots[].camera_object_id` | string | 与 `marker_id` 二选一 | 在 `scene_revision` 里存在且 `kind=camera`；按第 0 帧求值。摄像机不重复。只用于没有分镜的场景和旧客户端 |
| `shots[].images` | array，1–3 | 是 | `{pass:"clay"\|"line"\|"lit", upload_id}`，同一镜头内 `pass` 不重复；暂存图片就绪、未消费、类型为图片。全部镜头合计 ≤ 48 张 |

一次请求里的 `shots` 只能全部用 `marker_id`，或全部用 `camera_object_id`。

- 手动导出与助手任务均通过 `POST /api/v1/creative/uploads` 建立图片暂存会话。`target` 含 `kind: "staged"` 及 `scene_export: {canvas_id, scene_node_id}`。建会话时校验当前账号可写画布及其场景节点，文件仍走分片上传、媒体校验和账号存储额度。建会话与媒体校验均以单张 320 MiB 限制原图。
- 绑定场景的图片暂存项仅可由该场景的手动导出或助手任务消费。旧页面的通用暂存项仍可用原有分镜落位流程消费。普通上传的默认单张上限为 25 MiB。

### 成功响应
200：`{change_id, scene_revision, nodes:[{marker_id, camera_object_id, pass, node_id, content_revision_id, lens}]}`。
- `marker_id` 在按摄像机导出时为 null；`camera_object_id` 总是返回，是渲染所用的摄像机。
- 节点经画布命令层在一次修改里创建，可以在画布上一次撤销。`lens` 与节点 `based_on.lens` 相同，按这个分镜那一帧求值后的摄像机计算。
- 位置：从场景节点右侧 80 起，每个镜头一行、每种方式一列（按 `clay`、`line`、`lit` 的顺序），节点宽 300、高按这一帧摄像机的画幅；整块与同一父级下的已有节点相交时，下移到相交节点下方。
- 标题：按分镜导出时，分镜名非空就用分镜名，否则用「分镜 N」（N 是它在帧序里的序号）；按摄像机导出时，摄像机 `label` 非空就用它，否则用物件名。后面接「 · clay」「 · 线稿」或「 · 材质光照」。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法；`pass` 重复；分镜或摄像机重复；同一请求混用 `marker_id` 和 `camera_object_id`；分镜不存在于该修订、未绑定摄像机或绑定的摄像机不存在；摄像机不存在于该修订；`scene_revision` 晚于当前修订或不存在 | 修正 |
| 404 | `not_found` | 场景、画布或场景节点不存在，不属于当前账号，或场景节点不指向这个场景 | 停止 |
| 409 | `archived_read_only` | 画布已归档（沿用画布现有错误码） | 恢复画布后重新导出 |
| 409 | `creative_state_conflict` | 场景在回收站 | 丢弃暂存图片 |
| 409 | `creative_upload_unavailable` | 暂存图片不存在、未就绪、已过期、已被消费、不是图片或绑定到另一个场景 | 重新上传后用新的 operation id 重试 |
| 413 | `creative_size_limit` | 图片合计超过 48 张，或画布节点数超过上限 | 分次导出 |

### 交互规则
- **背景模式校验**（`scene-transparent-background`）：按请求中实际渲染的 `scene_revision` 确定期望值。回报字段存在时必须与期望一致，显式 `null` 属于外形错误。应传而缺少或报告不符时返回 `400 validation_failed`，提示「导出背景设置不一致，请刷新页面后重新导出」。在消费暂存图和写入节点前校验，拒绝时整批无节点，客户端按现有规则丢弃暂存图。
- **原子性**：全部节点一起创建或都不创建。
- **幂等**：沿用公共约定。网络中断、超时或 5xx 时，用同一 operation id 和 `client_created_at` 重试，得到同一组节点；只有服务端明确拒绝（4xx）时才丢弃暂存图片。
- **过时**：后续场景修改不影响已创建的节点；过时提示由节点视图计算（`data: 导出图来源`）。

### 调用示例
```json
{"operation_id": "4d5e6f70-8192-4a3b-9c4d-5e6f708192a3", "client_created_at": "2026-11-10T14:00:00Z",
 "payload": {"canvas_id": "ccv_12ab", "scene_node_id": "cnode_34cd", "scene_revision": "42",
   "shots": [{"marker_id": "smk_L1m2N3o4P5q6R7s8T9u0V",
              "images": [{"pass": "clay", "upload_id": "cupl_dd44"}, {"pass": "line", "upload_id": "cupl_ee55"}]}]}}
```
```json
{"change_id": "cchg_56ef", "scene_revision": "42", "nodes": [
  {"marker_id": "smk_L1m2N3o4P5q6R7s8T9u0V", "camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "clay", "node_id": "cnode_78a1", "content_revision_id": "ccrev_a1b2",
   "lens": {"focal_mm": 50, "depth_of_field": false, "f_stop": 2.8, "focus_m": 10, "focus_object_name": ""}},
  {"marker_id": "smk_L1m2N3o4P5q6R7s8T9u0V", "camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "line", "node_id": "cnode_78a2", "content_revision_id": "ccrev_a1b3",
   "lens": {"focal_mm": 50, "depth_of_field": false, "f_stop": 2.8, "focus_m": 10, "focus_object_name": ""}}]}
```
```json
{"error": {"code": "validation_failed", "message": "分镜 smk_L1m2N3o4P5q6R7s8T9u0V 在修订 42 中未绑定摄像机"}}
```

### 兼容与变更
首次定义。图片节点 `config` 新增 `based_on`，节点快照新增可选的 `based_on_state`，都属于兼容扩展；旧读取端忽略这些字段。修订 v8（C4 起草时）：错误码对齐画布与暂存的现有实现（归档为 `archived_read_only`，暂存不可用为 `creative_upload_unavailable`）；合计 48 张上限（画布一次修改最多牵动 100 个对象，每张图占节点和版本两个）；响应增加 `scene_revision`；只服务手动导出，去掉 `job_id`，助手导出改随完成任务提交；`based_on` 增加 `content_revision_id` 和 `fingerprint`；写明位置、标题和重试规则。修订 v9（C4 验收中）：`based_on` 和响应的节点增加镜头快照 `lens`，兼容扩展。

`scene-pose-system` 子项 1：`shots[]` 增加 `marker_id`，与 `camera_object_id` 二选一；响应节点增加 `marker_id`。旧客户端只发 `camera_object_id`，行为不变（没有通道的场景按第 0 帧求值就是现在的快照）。前端同一版本上线。

`scene-transparent-background`：增设条件必传的背景模式回报。旧页面提交不透明来源修订或仅线稿时可省略；需要该字段的请求须刷新后重新出图。

## api: MODIFY 助手场景工具的输入与写入结果

### `edit_scene@1`

已获场景权限的调用者通过既有的场景文档 ID 和读取基线提交原子批次。普通写入需要 `based_on_revision`。单独撤回按现有撤回规则执行。

| 字段 | 可省略与含义 |
|---|---|
| `add_objects.objects[].id` | 省略则服务端生成合法物件 ID |
| `parent_id`、`visible`、`locked`、`order` | 新增时分别默认根级、可见、未锁定和在同级末尾追加 |
| `transform` | 新增时缺省为原点、单位旋转、单位缩放，更新时只应用传入的分量。新物件使用 `look_at` 时须提供 `position` |
| `look_at`、`yaw_deg`、`transform.rotation` | 三者至多给一个。目标点是世界坐标，水平旋转是局部 Y 轴的角度 |
| `added_object_ids` | 写入新增物件时按新增输入的顺序返回 ID |

分镜更新只写相关关键帧。缺失、错位和互斥参数返回 `creative_tool_refused`，整批回滚。修订冲突和回执重放沿用既有语义。存量调用保持有效。

### `write_model@1`

目标保持 `add` 或 `replace` 二选一。新任务给非空 `code` 或可复用的 `base_content_revision_id`。冲突重试给 `retry_job_id`，与前两种来源及 `params` 互斥。参数错误在入队前返回具体提示。

### `export_shots@1`

除既有的 `document_id`、`shots`、`camera_object_ids`、`passes` 外，支持可选布尔参数 `transparent_background`（`scene-transparent-background`）：
- 显式传入 `true` 或 `false` 时，本次导出的 `clay`、`lit` 直接按该透明背景状态出图并记入导出节点的 `based_on.transparent_background`，不修改场景文档 `scene.render.transparent_background`、不产生场景修订，也不受场景开关当前取值或其他标签页切换开关的影响；`line` 始终为白底。传入非布尔值返回 `creative_tool_refused`。
- 省略时，按渲染端执行任务时读取到的 `scene.render.transparent_background` 出图。

## patterns: MODIFY 场景着色写法

- **约定**：
  - 适用范围：`frontend/src/creative-scene/`，以及所有渲染场景或模型的前端代码（模型缩略图、资产 3D 预览）。
  - 渲染器只从渲染器创建入口获得（`WebGPURenderer`，WebGPU 不可用时自动改用它的 WebGL2 后端），其他代码不直接创建渲染器。
  - 着色只用 TSL（`three/tsl`）：材质用节点材质（`*NodeMaterial`），或由渲染器自动映射的内置材质；后处理用 `RenderPipeline` 和节点。不使用 `ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`，也不写 GLSL / WGSL 源码字符串（包括 `glslFn`、`wgslFn`、`wgslTagFn`）。例外只有以下几处（`scene-world-space-gi`）：
    - npm 依赖 `three-mesh-bvh/webgpu` 的 BVH 遍历是 WGSL（owner 2026-10-23）；
    - `render/trace/` 里一个调用它求交的包装模块：在 three r186 上，库的求交不能从 TSL 直接调用，包装模块只构造参数、声明局部结果、按值返回，不写任何着色逻辑（修订 v3）。上游提供可从 TSL 调用的版本后删除。
  - 程序化材质预设是 TSL `Fn`：输入只有对象空间位置（米）、对象空间法线、参数 uniform、像素尺度和宽度函数，输出 `{albedo, roughness, metalness, height, emission}`；不读取渲染器状态、屏幕坐标、帧序号或时间。函数体里不直接调用屏幕导数（`fwidth`、`dFdx`、`dFdy`）：抗锯齿需要的宽度经输入的宽度函数取得，光栅路径传 `fwidth`（生成的着色与改动前相同，已发布预设的外观不变），光线命中点（屏幕外，没有屏幕导数）传由光线锥合成光线微分、再取有限差分得到的宽度，像素尺度同样按光线锥估计（`scene-world-space-gi`）。材质函数的输入在 TSL 的 `Switch` 之前存成变量（见下文「分叉之前先存成变量」）。凹凸由 `height` 在预设函数之外求得；程序化预设的凹凸在命中点不做，材质贴图的法线在命中点照做（`scene-editor-3d` 修订 v17，见下一条）。
  - 已发布的预设外观不变，改外观发布新版本（沿用材质目录的规则）。
  - 材质贴图（`scene-editor-3d` 修订 v17，C5c1）：
    - 映射（三平面投影、UV）、法线解码与混合、透明裁切只写一份（`materials/maps.ts`），光栅和光线命中点各自传入采样方式：光栅用隐式导数，命中点一律 `.grad()`，梯度来自光线锥微分。
    - 三平面投影的三个轴无条件采样，再按权重混合，不按权重分叉（见下文「分叉里不放隐式导数的采样」）。
    - UV 模式按变换后的贴图坐标（旋转、`repeat` 之后）求切线框架：光栅用屏幕导数，命中点用三角形三个顶点。不用 three 的 `normalMap` 和几何体的 `tangent` 属性，因为它们只认原始 `uv()`，贴图旋转后凹凸方向会错。
    - 纹理绑定预算（每个着色阶段 16 张，WebGPU 默认值，three 为每张纹理另配一个 sampler）：每套材质最多 3 个贴图绑定（基础色、法线、ORM 打包），普通表面的阴影贴图最多 8 张（过渡做法，最终方案另立 change）。同一个 `Texture` 对象只占一个绑定，所有占位图共用同一个对象。
  - 按像素分叉的控制流里，不放依赖隐式导数的纹理采样。
    - **范围**：
      - 分叉指条件随像素变化的 `If`、`Loop` 里的提前退出，以及条件不是常量的 `select`：three 把这种 `select` 编成 if/else；
      - 依赖隐式导数的采样，指 `texture()` 不带 `.level()` 或 `.grad()` 的采样，以及深度比较采样。
    - **原因**：
      - WGSL 规定这类采样只在一致的控制流里有定义，而 three 生成 WGSL 时关掉了 `derivative_uniformity` 诊断，所以编译不报错；
      - 没进分支的相邻像素，坐标为零或未定义，导数随之为 NaN；
      - 结果是整块纯白或纯黑。
    - **写法**：在分叉之外无条件采样，再用算术（`mix`、`step`）选择；或者显式指定 LOD。
    - **验证**：这类错误 SwiftShader 不复现，要在本机 GPU 上验证。

    出处：`scene-world-space-gi` 修订 v5；来源是 2026-10-26 体积雾的纯白（`f3c4807f`）。
  - 分叉之前先存成变量：按像素分叉的控制流里共用的表达式，在分叉之前存成变量（`.toVar()`）。
    - **范围**：
      - 分叉与上一条相同：`Switch`、条件随像素变化的 `If`、`Loop` 里的提前退出，以及条件不是常量的 `select`；
      - 共用指几个分支（包括循环展开出的几份分支）用到同一个节点，也包括作为参数传进函数的表达式，例如 `dir.negate()`。
    - **原因**：
      - TSL 在第一次构建某个表达式的分支里把它存成变量，只在那个分支里赋值；
      - 其他分支读到的是没赋过值的变量，编译不报错；
      - 结果是数值错了，而不是整块纯白或纯黑，所以更难发现。
    - **写法**：在函数入口或分叉之前存成变量，函数收到的参数也一样。
    - **验证**：写不成静态检查。调整分叉的结构（例如调换分支顺序、加提前退出）时，在同一个着色器里算出新旧两种写法，逐像素对照。

    出处：`scene-world-space-gi` 修订 v6。来源是 S2 任务 7.0：探针逐级回退只调换了累加顺序，`thin-wall@level2` 就从 +13% 变成 +34%。原因是传进来的视线方向只在第一级的分支里赋了值。
  - 需要随帧变化的量（TRAA 抖动、时域噪声）只取渲染器创建入口提供的帧序号；不读墙钟时间，也不依赖渲染器自己的 rAF 计数。每张导出图从帧序号 0 开始；跨帧累积的状态（实时 GI 世界空间追踪层的探针和反射历史）在这时一并清空。
  - 预计算表（LUT，例如物理大气的透射率、多次散射、天空视图和空气透视）用全屏片元 pass 写入 `RenderTarget`，3D 表写入 `RenderTarget3D` 的各层或拼成 2D 图集；不用 compute 写存储纹理。理由是 WebGL2 后端的 compute 由 transform feedback 模拟，只能写存储缓冲。
  - 默认两个后端都支持。只支持 WebGPU 的功能须经所属 Epic 修订确认，并按统一规则降级和认领（`scene-world-environment` design「只支持 WebGPU 的统一规则」）。已确认只支持 WebGPU 的：实时 GI 的世界空间追踪层（`scene-world-space-gi`，降级与认领方式见该 Epic design「能力与容量」）。
  - 光追（实时 GI 的世界空间追踪层，以及将来的路径追踪）：场景的 BVH、材质表、求交和命中点着色只经光追底座（`render/trace/`）获得，每个渲染设备一份、跨导出图复用。命中点着色使用与光栅相同的材质参数和预设函数、灯光 uniform（含预曝光）和场景环境贴图，阴影朝灯光发阴影光线（屏幕外的命中点不在视锥拟合的阴影贴图覆盖范围内）；命中点的光照模型是与光栅等价的另一份实现（three 的光照模型只在材质构建时运行），由同一表面的光栅对照测试把关。只支持 WebGPU 的光追可以用 compute 写存储缓冲和存储纹理（上一条对 LUT 的限制只针对两个后端都要运行的预计算表）。为它重新标定的常数（SSGI 增益、环境光填充、收敛帧数、时限）只在这一层构建时生效，其余情况保持原值。为性能做的几何简化只用在画面上看不出差别的地方：漫反射探针的光线追误差受控的代理（按 ε 简化，由画质对照把关），锐利反射和将来的路径追踪追原始几何；规模超出设备容量时逐档降级并明确提示，不悄悄降低画质（`scene-world-space-gi` design I8）。导出和 `render_view` 等光追底座就绪后才开始计帧，结果不随构建快慢变化。
  - 物理天空下光照按物理单位（亮度 cd/m²，平行光照度 lux，点光和聚光 cd），光源强度在送进 GPU 前乘以曝光系数（预曝光）；天空 LUT、空气透视和环境贴图等环境派生数据按「环境相对单位」存储（乘以只取决于环境的 s_env），每个观看者再乘以相对系数；天空、灯光和材质写入 HDR 缓冲前钳到 60 000。这样 RGBA16F 缓冲在正午不溢出，夜里也不下溢（`scene-world-environment` design「物理单位与曝光」）。着色代码里不写绝对亮度的常数；绝对量只出现在 `world/exposure` 和 `world/atmosphere` 的 CPU 端。
  - 环境类效果（天空、太阳光、天空光照、空气透视、体积，以及将来的云和地表效果）取同一份天体方向、大气 LUT 和曝光，不各自近似。天体位置只由文档字段算出，不读当前时间。
  - 第三方 3D 组件（drei 等）只用不含自定义着色器的。引入新组件前先确认它能在 `WebGPURenderer` 下运行，再加入静态检查的白名单。
  - three 锁定精确版本；升级时重跑迁移等价对照（`scene-webgpu-renderer` AC-1 的参考场景集）。`three-mesh-bvh` 和 `meshoptimizer`（直接依赖）同样锁定精确版本；升级时重跑 `scene-world-space-gi` 的参考场景对照，并核对 `three-mesh-bvh/webgpu` 的不稳定接口（包括项目用到的非公开扩展点）。
  - fork 进项目的 three 节点和着色代码（正交 GTAO、天空、SSGI，以及 `scene-world-space-gi` 起的 SSR）在文件头注明上游版本和改动的行；升级 three 时逐个与上游比对。`three-mesh-bvh` 的增量打包用子类扩展，不 fork（修订 v3），依赖的非公开接口在文件头列出。
  - three r186 在 compute 阶段的限制（`TextureNode.grad()`、PMREM 的环境旋转）由 `render/trace/` 里一个限定范围的补丁模块处理：只改 compute 阶段原本会报错的分支，光栅着色器不受影响；文件头注明上游版本、改动的方法和对应的上游 issue，升级 three 时先检查能否删除（`scene-world-space-gi` 修订 v3）。按论文或外部示例自写的着色代码（Hillaire 物理大气）在文件头注明参照的论文和示例版本。
  - 透明背景渲染钩子（`scene-transparent-background`）：对 `NodeMaterial.prototype.setupFog` 与 `PhysicalLightingModel`（透射材质 `(1 - F)` 覆盖率与 `finish` 镜面高光 alpha 下界）的扩展须严格按当前渲染通道的 `builder.context` 标记门控（仅透明主场景 pass 及对应的预编译入口注入稳定的派生 `contextNode`，隔离着色器缓存键），确保不透明导出、非最终渲染视口与 `render_view` 编译出与上游完全一致的着色器；GI 主画面的去底由独立的 `transparentBackground` 字段控制，保留 Three 的 `PassNode.transparent` 半透明绘制队列语义。四通道 SMAA 边缘检测与本地 TRAA 子类只用于透明模式。TRAA 子类局部移植 `setup` 的 resolve 建图并继承上游历史资源管理。玻璃的本地 `start` 从原始折射采样取得背景 alpha，保留透射 RGB 和上游光照顺序。上述位置注明 `three 0.186.1` 上游文件与行号并由单测守卫，升级 three 时逐项核对。
- **为什么**：`WebGPURenderer` 只接受节点材质。只有一份着色源码，WebGPU 和 WebGL2 两个后端以及将来的 compute 路径追踪器才能共用同一份定义，不会出现两套实现的外观漂移；预设写成不依赖渲染器的纯函数，才能在 compute 着色器里复用。
- **不适用于**：不做 3D 渲染的 2D canvas（例如画布页的生成遮罩用 2D context）；沙箱里的几何代码只生成几何、不渲染。
- **检查**：`make check-frontend` 中的静态检查（`scene-webgpu-renderer` R1 交付）：`frontend/src` 里出现 `WebGLRenderer`、`WebGLRenderTarget`、`ShaderMaterial`、`RawShaderMaterial`、`onBeforeCompile`、`ShaderLib`、`ShaderChunk`、GLSL 模板字符串、`three/examples/jsm/postprocessing/*`、`three/addons/objects/Sky.js`、非 WebGPU 版的 `lines/*`，或白名单以外的 drei 组件时失败。`frontend/src/creative-scene/world/` 和 `render/` 里出现 `Date.now()` 或无参的 `new Date()` 时也失败（`scene-world-environment` W2 交付；节流用的 `performance.now()` 不影响画面，不在此列）。`frontend/src` 里出现 `wgslFn` 或 `wgslTagFn` 时同样失败，`render/trace/` 的包装模块除外（`scene-world-space-gi` S1 交付）。「分叉里不放隐式导数的采样」和「分叉之前先存成变量」写不成静态检查，由在本机 GPU 上运行的定向 e2e 把关。
- **例子**：
  - `frontend/src/creative-scene/materials/patterns.ts` 的预设函数；
  - `frontend/src/creative-scene/materials/maps.ts` 的贴图映射，光栅与 `render/trace/hit.ts` 共用（C5c1 交付后）；
  - `render/` 下的渲染器创建入口（R1 改写后）；
  - `render/trace/` 的光追底座（`scene-world-space-gi` S1 交付后）；
  - 分叉里的采样：`render/volumes/lights.ts` 的点光阴影查找，每步无条件采样，再乘以范围掩码；`render/probes/lookup.ts` 的探针采样，显式指定 LOD（S2 交付后）。
  - 分叉之前先存成变量：`render/probes/lookup.ts` 的 `probeIrradiance` 在入口把位置、法线和视线方向存成变量（S2 交付后）。
