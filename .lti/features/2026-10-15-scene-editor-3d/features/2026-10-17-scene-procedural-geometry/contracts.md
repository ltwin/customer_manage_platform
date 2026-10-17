<!-- 本 change 的局部契约增量。epic 目标契约（patterns: 几何模块契约、patterns: 客户端代码沙箱、data: 模型内容、api: 场景模型命令）经 epic bind --deliver 交付，正文不复制到这里。下面三条修改已交付的项目契约；「api: 读取场景」按 owner 2026-10-17 的决定（模型地址走媒体票据）修改。 -->

## patterns: MODIFY 文档类型端口

- **约定**：
  - 新的文档类型实现 `creativedocument.Type`（`Key`、`ContentKind`、`NodeTypeKey`、`Creatable`、`Empty`、`Units`、`Touched`、`Apply`、`RestoreTouched`、`RestoreUnits`、`View`、`Summary`），按需实现 `RestoreHook`，在类型所在包的 `init()` 中 `Register`，并保证 server 与 creative-worker 两个进程都导入该包（内置的 `internal.document` 在 `creativedocument` 包里注册）。重复的类型 key 或节点类型注册时直接 panic。
  - 类型不得自行写 `creative_documents` 及修订、引用、会话表，所有写入都经命令外壳。
  - `Touched` 不得写库；`Apply` 可以在同一事务里写类型自己的附属数据。
  - 单元的存在性发生变化时（新增、删除，或恢复一个当前不存在的单元），`Touched` / `RestoreTouched` 必须同时返回类型声明的集合单元（如场景的 `objects`、策划的 `shots`）。这样，「不存在 → 新增 → 删除」之后再提交的旧请求也会冲突。
  - `Apply` 新增的引用和 `RestoreUnits` 从旧修订带回的引用，由通用层分别校验（见 `api: 提交文档命令`「引用校验」）。
  - **节点类型映射**：每个文档类型声明且只声明一个画布节点类型（`NodeTypeKey`），两边都唯一，例如 `scene` ↔ `core.scene`、`photo.shoot_plan` ↔ `photo.shoot_plan`、`internal.document` ↔ `internal.document`。画布按注册表查映射，不写死任何类型。
  - **类型内新建内容的持有方交接**：
    - `Apply` / `RestoreUnits` 收到通用层传入的 `Retain` 回调。类型在事务内新建内容时，调用该内容类型规定的写入口，并把这个回调作为 retain 参数：一般内容用 `creativecontent.WriteAndRetain`；`model` 只能用 `creativecontent.WriteModelAndRetain`（场景的 `commit_model`）。
    - 回调会把该内容修订预登记到本次即将产生的修订（N+1）的引用行上，使内容层返回前的持有方检查通过。
    - 通用层在写入前核对：预登记的引用必须全部出现在最终快照的引用里，否则整个事务回滚。
  - **推进版本与只做标注**：`Changes.Changed` 中的单元推进到新修订号；`Changes.Annotated` 中的单元载荷有变化，但单元修订号**不**推进。用于「确认」这类操作：策划的 `confirm_section` 把 `confirmed_rev` 设为当前 `rev`，本身不改 `rev`。两类都写入修订行，供历史显示。
  - **类型专属的回执字段**：
    - `Changes.Response` 是类型要附加到命令回执的字段（键 → JSON），例如场景 `commit_model` 的 `models`。
    - 通用层把它并入 `CommandResult` 顶层；键与通用字段（`revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`）冲突时视为实现错误，整个事务回滚。
    - 回执随幂等记录保存，重放时原样返回。
    - 没有附加字段的类型不设置它，回执与以前完全相同。
- **单元命名**：`meta` 保留给标题；其余单元由类型定义，形如 `<kind>` 或 `<kind>:<id>`，≤ 120 字符，`[A-Za-z0-9_:.-]`（场景物件 ID 是含大写的 nanoid，实施时放宽）。
- **为什么**：策划和场景共享修订、并发、恢复、回收站和画布接入的全部机制，避免两套实现分叉。回执字段由类型附加，是因为只有类型知道批内临时名（如 `commit_model` 的 `ref`）对应到哪个新内容，调用方需要据此推进本机状态，而不必为此重读整份快照。
- **不适用于**：不挂在画布节点上的业务对象（例如资产库素材），以及不需要修订历史的配置数据。
- **例子**：`internal.document` 的实现在 `backend/internal/creativedocument/internal_type.go`，单元 `body`，操作 `set_body`，不可新建。场景的回执扩展见 `api: 场景模型命令`「成功响应」。

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

- **GLB 与缩略图**：响应不含地址。编辑器按模型修订调用现有的 `POST /creative/media-access-tickets`（`content_revision_id`，`role` 为 `original` 取 GLB、`thumbnail` 取缩略图，`purpose` 为 `display`），按修订缓存，与画布图片的做法一致。
- **代码**：通过 `GET /creative/content-revisions/{content_revision_id}` 单独读取（载荷的 `model.generator.code`），避免每次重读快照都传几十 KB 代码。
- **性能**：模型摘要在同一个读快照事务里批量读取，查询次数不随模型数增长。

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
       "primitive": {"builtin": "ground@1", "params": {"size": 40}}, "material_slots": {}},
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
  }
}
```
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
| `scene.environment.sky.sun_elevation_deg` | number −10…90 | `procedural` 时 | 新建为 35 |
| `scene.environment.sky.sun_azimuth_deg` | number 0…360 | `procedural` 时 | 新建为 135 |
| `scene.environment.sky.turbidity` | number 1…20 | `procedural` 时 | 新建为 3 |
| `scene.environment.sky.hdri` | string，内置 HDRI ID（`^[a-z0-9_]+@[1-9][0-9]*$`） | `hdri` 时 | — |
| `scene.environment.sky.color` | `#rrggbb` | `color` 时 | — |
| `scene.environment.ambient` | `{color:#rrggbb, intensity:0…10}` | 是 | `{#ffffff, 0.4}` |
| `scene.environment.fog` | `{color:#rrggbb, near_m>0, far_m>near_m}` 或 null | 是 | null |
| `scene.render.mode` | `"clay"` \| `"lit"` | 是 | `clay`；只影响编辑器默认显示，导出时另行指定 |
| `scene.objects[]` | 对象数组，≤ 2000 | 是 | 新建为一个内置地面 |
| `objects[].id` | string | 是 | 见身份规则 |
| `objects[].kind` | `"primitive"` \| `"model_instance"` \| `"light"` \| `"camera"` \| `"group"` | 是 | `model_instance` 自 C2 起可写入 |
| `objects[].name` | string ≤ 80 字符 | 是 | 可为空串 |
| `objects[].parent_id` | string 或 null | 是 | null 表示根；父对象必须是 `group`；不能成环 |
| `objects[].order` | integer ≥ 0 | 是 | 同一父对象下的兄弟顺序，兄弟之间不重复 |
| `objects[].transform` | `{position:[3], rotation:[4]（单位四元数 x,y,z,w）, scale:[3]（每项 0.001…1000）}` | 是 | 数值有限；位置每轴 ±100000 m |
| `objects[].visible` / `locked` | boolean | 是 | `true` / `false` |
| `objects[].primitive` | `{builtin:"<name>@<ver>", params:{…}}` | `kind=primitive` 时 | `builtin` 必须在服务端登记的内置清单里（C1 交付清单）；`params` 外形同模型参数值 |
| `objects[].model` | `{content_revision_id}` | `kind=model_instance` 时 | 必须是本账号 `model` 类型、`ready` 且可展示的内容修订 |
| `objects[].material_slots` | `{<slot_id>:{preset, params}}` | 否 | 缺省 `{}`；`preset` 匹配 `^[a-z0-9_]+@[1-9][0-9]*$`；`params` 是平铺对象，≤ 32 键，值为 number/string(≤80)/boolean |
| `objects[].light` | `{type:"directional"\|"point"\|"spot", color, intensity:0…1000, range_m:null\|>0, angle_deg:null\|1…179, penumbra:null\|0…1, cast_shadow:boolean}` | `kind=light` 时 | `spot` 时 `angle_deg` 必填 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near_m>0, far_m>near_m, label≤40}` | `kind=camera` 时 | 新建为 `{35,[36,24],"3:2",0.1,1000,""}` |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型 `content_revision_id`。

### 访问模式与保证
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、生成参数、名称提示，不含代码和地址）。GLB 和缩略图由编辑器按模型修订另取媒体读取票据（C2 修改）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型可用、单元版本单调。
  - 由数据库保证：修订号主键唯一、`account_id` 限定、内容类型 CHECK。
- **历史**：按 `revision DESC` 分页列出修订；`changed_units` 直接读，不比对快照。

### 生命周期与演进
- 进回收站与彻底删除沿用通用文档层。彻底删除时删除修订行和引用行，之后模型修订失去持有方，可被 GC 回收。
- 内容类型 CHECK 加 `scene`。回滚时如已有 `scene` 内容则拒绝，前向修复。
- 快照 `schema_version` 升级时，读取端兼容旧版本，写入只写新版本；不做批量回填。
