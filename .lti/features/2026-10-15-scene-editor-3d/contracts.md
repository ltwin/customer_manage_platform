<!-- 已批准的目标契约，不代表项目当前已实现。每个条目由一个子项完整交付（见 roadmap.md「交付契约」列）。
公共约定引用：错误封套 `ErrorEnvelope`（api/openapi.yaml components.schemas.ErrorEnvelope）；幂等与回执沿用 creativeops：
写接口必带 `Idempotency-Key` 头（uuid），请求体外层为 `{operation_id, client_created_at, payload}`，operation_id 与头一致；
同一 key 同一请求体回放原回执，同一 key 不同请求体返回 409 `idempotency_conflict`；未知结果用 `GET /api/v1/creative/operations/{operation_id}` 查回执。
修订号类型沿用 `CreativeRevision`（正 BIGINT 的十进制字符串）。所有路径前缀 `/api/v1`，鉴权为现有登录会话 Bearer token，资源一律限定当前账号；
跨账号或不存在统一返回 404 `not_found`。
通用文档的读取外壳、命令外壳（`expected` + `operations`）、冲突错误 `creative_document_conflict`、回收站、修订历史和工作会话指针，
由独立 change `creative-document-foundation`（通用文档底座）定义和交付。本 Epic 只定义 `type_key = "scene"` 的分支，以及场景专属的动作。 -->

## architecture: ADD 场景模块

### 职责与边界
- 依赖通用文档底座（`creative-document-foundation`）：场景以文档类型端口注册为 `scene` 类型，读取、命令、回收站和历史都走通用入口。
- `backend/internal/creativescene`：
  - 场景快照的 schema 校验、单元版本与命令应用、恢复。
  - 模型提交（消费暂存上传，写 `model` 内容）。
  - 客户端任务生命周期、导出编排。
  - 场景助手工具的领域入口。
- 不职责：执行几何代码、渲染、画布节点的布局规则（调用画布命令层完成）。
- 依赖方向：`creativeagent` → `creativescene` → `creativecontent` / `creativestaging` / `creativecanvas`（命令层）/ 通用文档层 / `platform/store` / `platform/realtime`。`creativescene` 不 import gin（ADR-003），`gin.Context` 不下穿。
- 前端 `frontend/src/creative-scene/`：编辑器外壳（R3F）、场景状态（快照 + outbox + 撤销栈）、沙箱宿主、渲染端（客户端任务执行器）、材质库、导出器。编辑视图嵌入画布的同一个助手面板。
- 渲染、烘焙、截图、导出只在浏览器完成；服务端不渲染。服务端经客户端任务派发工作，经暂存上传接收产物。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行等待并释放槽位 → 编辑器认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图 → `POST /creative/documents/{id}/scene-exports` → 画布命令层建图片节点，写入 `based_on`。

## patterns: ADD 几何模块契约

- **约定**：模型的生成代码是一个不含 `import` / `export` 以外语句的 ES 模块文本，由沙箱以注入依赖的方式执行：

```js
export const params = {
  height: { type: 'number', default: 6, min: 2, max: 20, step: 0.1, unit: 'm', label: '高度' },
  broken: { type: 'boolean', default: false, label: '断裂' },
  style:  { type: 'enum', default: 'gothic', options: ['gothic', 'roman'], label: '样式' },
}
export const mount = 'floor' // 'floor' | 'wall' | 'ceiling' | 'free'
export default function build({ params, THREE, lib, rng }) {
  const group = new THREE.Group()
  // ……用 THREE 与 lib（布尔、挤出、旋转体、阵列等标准库）构建
  return group
}
```

- **参数类型**：`number`（`default`、`min`、`max` 必填，`step`、`unit` 可选）、`integer`、`boolean`、`enum`（`options` 1–32 项）、`color`（`#rrggbb`）。每项可带 `label`，最多 32 项。
- **坐标与单位**：米，Y 轴向上，正面朝 +Z。原点：`floor` 为底面中心，`wall` 为背面底边中心，`ceiling` 为顶面中心，`free` 为几何中心。
- **依赖**：`THREE`、`lib` 和 `rng` 都由宿主注入，模块内不得 `import`。`lib` 是平台的建模标准库，按版本发布（`lib@1`），模块文本里声明 `export const lib_version = 1`，省略时为 1。`rng` 是带种子的确定性随机数；`Math.random`、`Date` 在沙箱内不可用。
- **材质**：只能使用 `MeshStandardMaterial`（或 `lib.material(slot, hint)` 生成的同类材质）。材质名 `slot:<id>` 表示可分配材质预设的槽位，`userData.hint` 给语义提示（如 `stone`、`wood`）。不得使用 `ShaderMaterial` / `RawShaderMaterial`、外部纹理 URL 或视频纹理。程序化外观由编辑器材质库在渲染时提供。
- **命名约定**（进入 manifest）：对象名 `part:<id>` 表示可寻址部件，用于点选改某个部位；空对象 `anchor:<id>` 表示挂点。
- **产物**：沙箱返回 GLB（经 `GLTFExporter` 二进制导出）和 manifest：`{bounds:{min:[x,y,z],max:[x,y,z]}, triangles, slots:[{id,hint}], parts:[id], anchors:[{id,position:[x,y,z]}]}`。GLB 是几何事实源，只有代码、参数或 `lib_version` 变化时才重新编译。
- **上限**（初始值，见 design 未决 #3）：代码 ≤ 64 KiB；编译 ≤ 5 秒；三角形 ≤ 300k；GLB ≤ 16 MiB。
- **为什么**：只用 primitive 的 JSON 表达力不够；整个场景是一段代码则无法支持手动编辑、点选修改和撤销。以物件为粒度写代码，既有建模自由度，又让场景编排可编辑。先例见 research §3（Pascal authored objects）。
- **不适用于**：内置基础体是前端打包的可信模块，遵守同一契约，但不经沙箱、不烘焙。外部 GLB 导入（下一版）不经过本契约。
- **例子**：C2 交付后补充内置基础体 `box@1` 的实现路径作为参照。

## patterns: ADD 客户端代码沙箱

- **约定**：任何来自模型或助手的代码，只能在满足以下条件的环境中执行：
  1. 独立的 `<iframe sandbox="allow-scripts">`（不含 `allow-same-origin`，因此是 opaque origin），内容由平台自带的沙箱页面提供，CSP 至少包括 `default-src 'none'; script-src 'self' blob:; connect-src 'none'; img-src blob: data:`。
  2. 实际编译在沙箱页面内部的 Worker 中进行，宿主通过 `postMessage` 传入代码、参数和 `lib_version`，只接收 GLB 的 ArrayBuffer、manifest 和错误信息。传出的消息有固定结构，宿主逐字段校验。
  3. 宿主为每次编译设硬超时（5 秒）；超时后销毁 Worker，必要时重建 iframe。沙箱不持有凭证，不能访问 cookie、存储、父页面 DOM 和网络，也不能导航顶层页面。
  4. 宿主用 `GLTFLoader` 解析返回的 GLB；解析失败按编译失败处理。
- **为什么**：助手会读网页，网页内容可能把恶意代码注入生成代码。代码在摄影师浏览器里执行，和摄影师的登录会话同处一个浏览器，所以必须隔离。
- **验收**：C2 交付安全样例集（尝试 `fetch`、`XMLHttpRequest`、`WebSocket`、`document.cookie`、`localStorage`、`indexedDB`、`parent.document`、`top.location`、死循环、超大输出），全部被阻断或在时限内终止，并纳入 `make check-frontend`。
- **不适用于**：前端自带的可信代码（内置基础体、材质库）。
- **备选**：安全样例无法在 iframe + Worker 方案下全部通过时，改用 QuickJS-wasm 执行，契约其余部分不变（design 未决 #2）。

## data: ADD 场景文档

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
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、缩略图、GLB 读取票据）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型可用、单元版本单调。
  - 由数据库保证：修订号主键唯一、`account_id` 限定、内容类型 CHECK。
- **历史**：按 `revision DESC` 分页列出修订；`changed_units` 直接读，不比对快照。

### 生命周期与演进
- 进回收站与彻底删除沿用通用文档层。彻底删除时删除修订行和引用行，之后模型修订失去持有方，可被 GC 回收。
- 内容类型 CHECK 加 `scene`。回滚时如已有 `scene` 内容则拒绝，前向修复。
- 快照 `schema_version` 升级时，读取端兼容旧版本，写入只写新版本；不做批量回填。

## data: ADD 模型内容

### 用途与存储角色
- **用途**：可复用的几何物件，由场景实例和资产库资产共同引用。
- **存储**：
  - `creative_contents.kind = 'model'`。
  - 每个版本一条 `creative_content_revisions`（`schema_version = 1`）。
  - GLB 存为 blob，经 `creative_content_objects` 以 `role='original'` 绑定；缩略图 PNG 以 `role='thumbnail'` 绑定。
  - 上传走 `creative_uploads`，`declared_kind` 加 `model`，校验格式加 `model/gltf-binary`（`.glb`）。
- **角色**：权威主数据，内容修订不可变。
- **所有权**：由 `creativescene` 的 `commit_model` 写入（消费暂存上传）；`creativelibrary` 存入资产库时只引用同一修订，不复制字节（复制语义由「不可变 + 编辑时分叉」实现，见 I4、I7）。

### 结构与身份

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `model.format` | `"glb"` | 是 | — |
| `model.generator.language` | `"three-module"` | 是 | 外部导入（后续）将使用其他值 |
| `model.generator.contract_version` | integer | 是 | 当前为 1 |
| `model.generator.lib_version` | integer | 是 | 当前为 1 |
| `model.generator.code` | string | 是 | ≤ 64 KiB，UTF-8 |
| `model.generator.code_sha256` | `sha256-<64 hex>` | 是 | 服务端重算并核对 |
| `model.generator.params_schema` | object | 是 | 按几何模块契约的参数定义 |
| `model.generator.params` | object | 是 | 每个键都在 schema 中，值在范围内 |
| `model.generator.mount` | `floor`/`wall`/`ceiling`/`free` | 是 | — |
| `model.manifest` | 见几何模块契约「产物」 | 是 | `triangles` ≤ 300000；`slots` ≤ 32；`parts` ≤ 200 |
| `model.name_hint` | string ≤ 80 | 否 | 助手或摄影师起的名字 |

### 访问模式与保证
- **GLB 校验**（worker 的 verify 阶段，Go 实现，不执行代码）：
  - magic 为 `glTF`，version 为 2，长度与 chunk 一致；JSON chunk 可解析。
  - `buffers[].uri` 必须为空（只用 BIN chunk）；`images[].uri` 只允许空或 `data:image/png|jpeg`。
  - accessor 计数与 BIN 长度相符。
  - 三角形数 ≤ 上限，按 indices 或 POSITION 计算。
  - 字节 ≤ 16 MiB。
  - 不合规时上传标记为 failed，返回 `creative_state_conflict`（经暂存状态呈现）。
- **提交时**：manifest 的 `bounds` 和 `triangles` 必须与服务端从 GLB 计算的结果一致（容差 1e-4 m），不一致则拒绝。服务端以自己的计算为准。
- **读取**：GLB 和缩略图走现有媒体读取票据（`IssueTicket` / `Open`）。

### 生命周期与演进
- 持有方：场景修订引用表、资产、画布节点（后续若有）。三者均登记进 `creativereference.LiveOwnerCondition` 和守卫测试。没有持有方的修订按媒体 GC 规则回收。媒体 GC 的类型范围（`creativemedia/gc.go` 的 `unheldMediaRevision`）目前只含 image / video / audio，C2 必须把 `model` 加进去并登记守卫测试；上线时先升级 creative-worker，再开放模型写入。
- 外部 GLB 导入（下一版）会加 `generator.language = "imported"`，且没有 `code`。本版读取端遇到未知 `language` 时只能显示，不能调参数。

## data: ADD 场景客户端任务

### 用途与存储角色
- **用途**：服务端派给编辑器页面执行的工作（烘焙模型、回看截图、导出），以及它的认领、租约、结果和关联的助手运行。
- **存储**：新表 `creative_scene_jobs`（PostgreSQL）。
- **角色**：权威主数据（任务状态）。产物本身在暂存上传或内容里，不在本表。
- **所有权**：`creativescene` 创建、过期、取消；编辑器经认领、续租、完成接口推进；`creativeagent` 只读任务结果，并在任务终态时继续运行。

### 结构与身份

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `id` | text | 是 | `csjob_<uuid>`，主键 |
| `account_id` | text | 是 | 账号隔离 |
| `document_id` | text | 是 | 目标场景 |
| `kind` | text | 是 | `bake_model` / `render_view` / `export_shots` |
| `request` | jsonb | 是 | 按 kind 定义：`bake_model` 为 `{code, params, lib_version, target}`，`target` 为 `{op:"add", object:{…}}` 或 `{op:"replace", object_id, scope:"this"\|"all_instances"}`；`render_view` 为 `{camera_object_id}` 或 `{viewpoint:{position,target,focal_mm}}`，加上 `mode`；`export_shots` 为 `{camera_object_ids, passes, canvas_id}` |
| `state` | text | 是 | `queued`/`claimed`/`succeeded`/`failed`/`expired`/`cancelled`（CHECK） |
| `attempt` | integer | 是 | 从 0 开始，每次租约过期回到 queued 时加 1 |
| `claim_token` | text | 认领时 | 随机令牌，过期或终态时清空 |
| `editor_session_id` | text | 认领时 | 认领者标签页 |
| `lease_until` | timestamptz | 认领时 | 租约到期时间 |
| `deadline` | timestamptz | 是 | 创建时间 + 30 分钟（初始值） |
| `run_id` / `tool_call_id` | text | 助手创建时 | 关联运行；摄影师发起的导出为 null |
| `result` | jsonb | 终态时 | 成功：产物摘要（模型修订 ID、截图暂存上传 ID、导出节点 ID 等）；失败：`{code, message}` |
| `created_at` / `updated_at` | timestamptz | 是 | — |

索引：`(account_id, document_id, state, created_at)`，用于认领下一个；`(state, deadline)`，用于过期扫描。

### 访问模式与保证
- **认领**：按场景取最早的 `queued` 任务，用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；把超过 `deadline` 的非终态任务改为 `expired`，并让关联运行继续（工具结果为过期）。
- **完成**：校验令牌与租约 → 落产物 → 改终态 → 让运行继续，在同一事务里提交（design「客户端任务」）。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（design 未决 #4）。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；读取端遇到未知 kind 时不认领。

## data: ADD 导出图来源

- **用途**：记录画布图片节点由哪个场景、哪个修订、哪台摄像机、哪种导出方式生成，用于提示过时。
- **存储**：`creative_nodes.config` 内的 `based_on` 对象，只对导出产生的 `core.image` 节点写入：`{scene_document_id, scene_revision: CreativeRevision, camera_object_id, pass:"clay"|"line"|"lit"}`。
- **角色**：派生信息，只用于提示，不参与权限和内容。来源场景被删除后，读取时视为「来源已不可用」，不显示过时。
- **写入**：只由「导出分镜」接口经画布命令层写入；摄影师替换图片内容后清除 `based_on`。
- **过时判断**：节点视图读取时，按 design「渲染模式与导出」的规则用修订表的 `changed_units` 计算，不落库。

## data: ADD 模型资产

- **用途**：资产库可以存放 `model` 资产。
- **存储**：`creative_assets.kind` 的 CHECK 加 `model`；资产引用模型的 `content_revision_id`。资产库现有能力（分组、标签、评分、回收站、查重）对 `model` 一视同仁；查重按 GLB 的 sha256。
- **生命周期**：回滚时如已有 `model` 资产则拒绝，前向修复。资产库不记录资产被哪个场景使用过（I7）。

## api: ADD 读取场景

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
| `models` | object | 是 | 否 | 以 `content_revision_id` 为键，覆盖快照引用的全部模型；C2 之前恒为 `{}` |
| `models.*.content_id` | string | 是 | 否 | 模型内容 ID |
| `models.*.usable` | boolean | 是 | 否 | false 时其余字段为 null，编辑器显示「模型不可用」占位 |
| `models.*.manifest` | object | 是 | 是 | 见几何模块契约「产物」 |
| `models.*.generator` | object | 是 | 是 | `{language, contract_version, lib_version, params_schema, params, mount}`，**不含代码** |
| `models.*.glb_url` | string | 是 | 是 | 10 分钟有效的读取票据 URL |
| `models.*.thumbnail_url` | string | 是 | 是 | 同上 |

代码通过助手工具或代码面板单独读取（C2 局部设计），避免每次重读快照都传几十 KB 代码。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 401 | 现有认证错误码 | 未登录或会话过期 | 走现有续期 |
| 404 | `not_found` | 不存在或不属于当前账号 | 关闭编辑视图，回到画布 |
| 503 | `creative_dependency_unavailable` | 媒体票据服务不可用 | 退避重试 |

### 交互规则
只读，没有分页。重复调用没有副作用。读取票据每次签发新的。

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
  "revision": "4",
  "content_revision_id": "ccrv_3e4f5a6b-7c8d-4e9f-a0b1-c2d3e4f5a6b7",
  "unit_revs": {"meta": "1", "environment": "3", "render": "1", "objects": "4",
                "object:sobj_V1StGXR8_Z5jdHi6B-myT": "1", "object:sobj_V1StGXR8_Z5jdHi6B-myT:content": "1",
                "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ:content": "2"},
  "trashed_at": null,
  "created_at": "2026-10-20T08:00:00Z",
  "updated_at": "2026-10-20T09:10:00Z",
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
      {"id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "kind": "camera", "name": "主机位", "parent_id": null, "order": 1,
       "transform": {"position": [0, 1.6, 8], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
       "visible": true, "locked": false,
       "camera": {"focal_mm": 35, "sensor_mm": [36, 24], "aspect": "3:2", "near_m": 0.1, "far_m": 1000, "label": "镜头 1"}}
    ]
  },
  "models": {}
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
在通用读取接口上新增 `scene` 分支，其他类型不受影响。`models` 在 C2 之前恒为空对象，C2 起才填充，属于兼容扩展。

## api: ADD 提交场景命令

### 用途与调用
- **用途**：对场景做一组原子编辑，成功时产生一个新修订。
- **消费者**：前端场景编辑器（outbox）；助手工具 `edit_scene` 经同一领域命令（不经 HTTP，写入者记为助手和 `run_id`）。
- **调用**：`POST /api/v1/creative/documents/{id}/commands`，`Idempotency-Key` 必填，`application/json`。这是通用命令接口在 `scene` 类型上的操作集合：外壳（`expected` 为单元到修订号的映射、`operations` 数组、整批原子、单元乐观并发）由底座的 `api: 提交文档命令` 定义，本条目定义场景的单元和操作。
- **权限**：场景属于当前账号，且不在回收站。

### 请求
外层 `{operation_id, client_created_at, payload}`（公共约定）。`payload`（外壳见底座，以下是场景的约束）：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `expected` | object（单元 → CreativeRevision 或 null，见底座） | 非 `restore` 批次必传 | 本批涉及的每个单元及其预期 `rev`，null 表示预期该单元不存在；缺少涉及单元时返回 400。`restore` 批次改用底座的 `expected_revision` |
| `operations` | array，1–200 项 | 是 | 按顺序应用；每个单元最多出现一次（同一对象的多处改动合并成 `update_objects` 里的一项）。批量操作的条目上限与物件上限一致（2000），一次多选操作因此始终是一个操作、一个修订 |

`operations[]` 按 `op` 区分：

| op | 字段 | 涉及单元 |
|---|---|---|
| `rename` | `title`（1–200） | `meta` |
| `add_objects` | `objects`（1–2000 个完整对象，见数据契约；`kind` 本条目允许 `primitive`/`light`/`camera`/`group`；`primitive.params` 缺省的键补成默认值） | `objects`（一次；新对象的两个单元以 N+1 出现在 `changed_units` 中） |
| `update_objects` | `updates`（1–2000 项）；每项 `object_id`，以及 `name`/`transform`/`visible`/`locked`/`primitive.params`/`material_slots`/`light`/`camera` 中任选，至少一项（省略即不改；`primitive.params` 按键合并；`material_slots`、`light`、`camera` 整体替换；字段必须与对象的 `kind` 相符）；同一物件只能出现一次 | 每项：只改摆放字段时为 `object:<id>`；只改内容字段时为 `object:<id>:content`；两类都改时两个单元都涉及 |
| `remove_objects` | `object_ids`（1–2000；分组连同后代一起删除） | `objects`，以及每个被删对象的 `object:<id>` 和 `object:<id>:content` |
| `reparent_objects` | `moves`（1–2000 项 `{object_id, parent_id（null 为根）, order}`） | `objects`（一次） |
| `set_environment` | `environment`（整体替换） | `environment` |
| `set_render` | `render`（整体替换） | `render` |
| `restore` | `revision`（旧修订号）；**必须单独成批**，带 `expected_revision` | 全部单元；按文档修订号检查（底座规则） |
| `restore_units` | `units`（1–4096 个单元名，随底座放宽；不能含 `objects`）、`from_revision` | 列出的单元；**必须单独成批**，删除大分组后的撤销也是一个请求，整体成功或整体失败。对象两边都存在时只换列出单元的字段，`parent_id`、`order` 保持现状。存在性会变化时，该对象的两个单元都必须列出，并额外涉及 `objects`，当前不存在的单元在 `expected` 中写 null。服务端先算出恢复后的完整对象集合，再统一解析层级：每个对象沿来源快照（加回的取旧修订，保留的取当前）的祖先链，挂到第一个仍在最终集合中且是分组的祖先下，没有就放到根；父对象同在本次加回的集合里时层级保持不变；旧修订中不存在的对象被删除时，它的后代按同一规则上移，不连带删除。顺序：父对象没变的保留原 `order`，其余按来源快照的层级和顺序依次放入，原 `order` 被占用时排到末尾。结果与 `units` 的排列顺序无关 |

### 成功响应
200（同一 operation 回放原回执）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
回执外形由底座 `api: 提交文档命令` 定义：`revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`。场景不增加字段（`api: 场景模型命令` 另加 `models`）。删除大分组或整份恢复时 `reset_required = true`，编辑器重新读取场景。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、单元重复、`expected` 缺单元、对象违反数据契约（父子成环、未知内置体、数值越界、ID 与当前对象重复等）、引用的物件不存在、`restore_units` 列了 `objects` 或只列了存在性会变化的对象的一个单元 | 修正，不可原样重试 |
| 404 | `not_found` | 场景不存在或不属于当前账号 | 关闭编辑视图 |
| 409 | `creative_document_conflict` | 任一单元的 `rev` 与 `expected` 不符。`details.conflicts` 为数组：`[{unit, expected_rev, current_rev}]` | 重读，按冲突界面处理后再提交 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 409 | `idempotency_conflict` | 同一 key 用于不同请求体 | 换新 key 前先查原回执 |
| 413 | `creative_size_limit` | 请求体超限，或应用后快照 > 512 KiB / 对象 > 2000 / 摄像机 > 32 | 缩小改动 |

### 交互规则
- **原子性**：全部成功或全部不生效。
- **幂等**：沿用公共约定。
- **并发**：按单元乐观并发，规则见 design「单元版本、写入与撤销」。
- 不适用：分页、异步、限流（沿用全局请求体上限）。

### 调用示例
```http
POST /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10/commands
Authorization: Bearer <token>
Idempotency-Key: 5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60
Content-Type: application/json
```
```json
{
  "operation_id": "5f0c3a8e-6a51-4d0b-9a77-1b2c3d4e5f60",
  "client_created_at": "2026-10-20T09:30:00Z",
  "payload": {
    "expected": {"object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "objects": "4"},
    "operations": [
      {"op": "update_objects", "updates": [{"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
       "transform": {"position": [1.5, 1.4, 7], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}}]},
      {"op": "add_objects", "objects": [{
        "id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "kind": "primitive", "name": "石台", "parent_id": null, "order": 2,
        "transform": {"position": [0, 0, -3], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
        "visible": true, "locked": false,
        "primitive": {"builtin": "box@1", "params": {"width": 2, "height": 0.8, "depth": 1}}, "material_slots": {}}]}
    ]
  }
}
```
```json
{"revision": "5", "changed_units": ["object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "objects", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a:content"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
冲突示例（助手已先改过这台摄像机）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
  "details": {"conflicts": [{"unit": "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "expected_rev": "4", "current_rev": "6"}]}}}
```

### 兼容与变更
在通用命令接口上新增 `scene` 类型的操作集合，其他类型不受影响。`api: 场景模型命令` 会在此基础上增加操作，并放开 `add_objects` 的 `model_instance`，属于兼容扩展。旧客户端不会发送新操作。

### 补充说明
新建场景不经本接口，而是走画布命令 `add_node`（`type_key = "core.scene"`）。底座的文档类型端口负责在同一事务里创建文档；场景类型提供初始快照（一块内置地面 `ground@1`，摄像机随 C4 的放置 UI 出现）和卡片摘要。

## api: ADD 场景模型命令

### 用途与调用
- **用途**：把烘焙好的模型提交为 `model` 内容，并在场景中新增实例或替换实例的模型。
- **消费者**：编辑器代码面板（C2）；客户端任务完成时由服务端内部调用（C3，经领域层）。
- **调用**：同 `api: 提交场景命令` 的端点与外层，增加以下操作：

| op | 字段 | 涉及单元 |
|---|---|---|
| `commit_model` | `ref`（本批内的临时名，`^[a-z0-9_]{1,32}$`）、`glb_upload_id`、`thumbnail_upload_id`（均为就绪且未消费的暂存上传）、`generator`（完整的 `model.generator`，含代码）、`manifest`、`name_hint`、`base_content_id`（可选，有则作为该模型内容的新修订，否则新建模型内容） | 无场景单元（不改快照） |
| `add_objects` 放开 `kind=model_instance` | `objects[].model` 写 `{content_revision_id}` 或 `{ref}`（引用本批的 `commit_model`） | `objects` |
| `set_instance_model` | `object_id`、`model`（`{content_revision_id}` 或 `{ref}`）、`scope`（`"this"` 或 `"all_instances"`，缺省 `"this"`） | `object:<id>:content`（只动内容单元，所以摄影师同时移动这个物件不会冲突）；`all_instances` 时还涉及所有引用同一旧修订的对象的内容单元 |

- **分叉规则（I4）**：
  - `base_content_id` 被其他实例或资产引用时，`commit_model` 仍然在该内容上追加修订。是否让其他实例跟随，由 `set_instance_model` 的 `scope` 决定。
  - 资产持有的旧修订不受影响，因为资产引用的是修订而不是内容的最新版。
  - 编辑器和助手在 `scope = "this"` 且共享时，应传入不带 `base_content_id` 的 `commit_model`，即新建内容（分叉）。
- **持有方交接**：`commit_model` 在场景类型的 `Apply` 中新建模型内容，使用底座传入的 `Retain` 回调预登记引用（底座 `patterns: 文档类型端口`）。
- **校验**：暂存上传属于本账号；GLB 已通过结构校验；manifest 与服务端计算一致；`code_sha256` 正确；参数符合 schema。
- **成功响应**：在 `api: 提交场景命令` 的响应上增加 `models: {<ref>: {content_id, content_revision_id}}`。
- **失败响应**（在基础接口之上增加）：

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 409 | `creative_state_conflict` | 暂存上传不存在、已过期、已消费或尚未就绪 | 重新上传 |
| 400 | `validation_failed` | manifest 与 GLB 不符、哈希不符、参数越界、`ref` 未定义或重复 | 修正 |
| 404 | `not_found` | `content_revision_id` 不存在、不是 `model` 或不可用 | 重读 |

- **示例**：
```json
{
  "operation_id": "0b7e2c1d-3f4a-4b5c-8d9e-0f1a2b3c4d5e",
  "client_created_at": "2026-10-22T10:00:00Z",
  "payload": {
    "expected": {"objects": "5"},
    "operations": [
      {"op": "commit_model", "ref": "pillar", "glb_upload_id": "cupl_aa11", "thumbnail_upload_id": "cupl_bb22",
       "name_hint": "哥特束柱",
       "generator": {"language": "three-module", "contract_version": 1, "lib_version": 1,
                     "code": "export const params={height:{type:'number',default:6,min:2,max:20}};export const mount='floor';export default function build({params,THREE}){const g=new THREE.Group();g.add(new THREE.Mesh(new THREE.CylinderGeometry(0.4,0.5,params.height,16),new THREE.MeshStandardMaterial({name:'slot:stone'})));return g}",
                     "code_sha256": "sha256-6f1ed002ab5595859014ebf0951522d9f2b8d1c5e8a6a7c3b1d0e9f8a7b6c5d4",
                     "params_schema": {"height": {"type": "number", "default": 6, "min": 2, "max": 20}},
                     "params": {"height": 6}, "mount": "floor"},
       "manifest": {"bounds": {"min": [-0.5, 0, -0.5], "max": [0.5, 6, 0.5]}, "triangles": 64,
                    "slots": [{"id": "stone", "hint": "stone"}], "parts": [], "anchors": []}},
      {"op": "add_objects", "objects": [{
        "id": "sobj_Z1x2C3v4B5n6M7a8S9d0f", "kind": "model_instance", "name": "左侧柱 1", "parent_id": null, "order": 3,
        "transform": {"position": [-3, 0, -2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]},
        "visible": true, "locked": false, "model": {"ref": "pillar"}, "material_slots": {}}]}
    ]
  }
}
```
```json
{"revision": "6", "changed_units": ["objects", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
 "annotated_units": [], "removed_units": [], "reset_required": false,
 "models": {"pillar": {"content_id": "ccnt_51c0", "content_revision_id": "ccrev_9e2a"}}}
```
- **兼容**：在 `api: 提交场景命令` 上做兼容扩展。

## api: ADD 认领场景客户端任务

### 用途与调用
- **用途**：编辑器认领本场景下最早的一个待执行任务。
- **消费者**：前端渲染端。收到任务变化通知、打开编辑视图、或完成上一个任务后调用。
- **调用**：`POST /api/v1/creative/documents/{id}/scene-jobs/claim`，`application/json`。认领本身不需要幂等键：重复认领只会拿到下一个任务，或续上自己的租约。
- **权限**：场景属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景 ID |
| `editor_session_id` | body | string，`^[A-Za-z0-9_-]{16,64}$` | 是 | 标签页随机生成的会话 ID |
| `kinds` | body | string[] | 否 | 本端能执行的任务类型；缺省为全部已知类型 |

### 成功响应
- 200，认领成功：`{job: {id, kind, request, attempt, claim_token, lease_until, deadline}}`。`request` 的结构见 `data: 场景客户端任务`。
- 204，没有可认领的任务：无响应体。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 会话 ID 非法、`kinds` 含未知值 | 修正 |
| 404 | `not_found` | 文档不存在、不属于当前账号或不是场景 | 停止认领 |

### 交互规则
- 用 `FOR UPDATE SKIP LOCKED` 保证每个任务最多被一个会话认领。同一会话可以连续认领多个任务，按顺序执行。
- 租约 30 秒，续租见 `api: 续租场景客户端任务`。

### 调用示例
```http
POST /api/v1/creative/documents/cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10/scene-jobs/claim
Authorization: Bearer <token>
Content-Type: application/json

{"editor_session_id": "tab_Vx9q2LmN7pR4sT8w"}
```
```json
{"job": {"id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "kind": "render_view",
  "request": {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "mode": "clay"},
  "attempt": 0, "claim_token": "clm_Q2w3E4r5T6y7U8i9", "lease_until": "2026-10-22T10:05:30Z", "deadline": "2026-10-22T10:35:00Z"}}
```
失败示例：
```json
{"error": {"code": "validation_failed", "message": "editor_session_id 格式不正确"}}
```

### 兼容与变更
首次定义。

## api: ADD 续租场景客户端任务

### 用途与调用
- **用途**：执行中的编辑器延长租约，表明自己仍在工作。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/lease`，`application/json`，不需要幂等键（续租本身幂等）。
- **权限**：任务属于当前账号，且 `claim_token` 匹配。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `job_id` | path | string | 是 | 任务 ID |
| `claim_token` | body | string | 是 | 认领时拿到的令牌 |

### 成功响应
200：`{lease_until}`，延长到当前时间 + 30 秒，但不超过 `deadline`。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 任务不存在或不属于当前账号 | 放弃执行 |
| 409 | `creative_state_conflict` | 任务已不是 `claimed`，或令牌不匹配（租约已过期被回收、任务已过期或取消） | 立即停止执行并丢弃产物 |

### 调用示例
```json
{"claim_token": "clm_Q2w3E4r5T6y7U8i9"}
```
```json
{"lease_until": "2026-10-22T10:06:00Z"}
```
```json
{"error": {"code": "creative_state_conflict", "message": "任务已被回收"}}
```

### 兼容与变更
首次定义。

## api: ADD 完成场景客户端任务

### 用途与调用
- **用途**：编辑器提交任务结果（成功或失败）。服务端在同一事务里落结果、改终态、让关联运行继续。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/complete`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：任务属于当前账号，`claim_token` 匹配，且租约未过期。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `claim_token` | string | 是 | — |
| `outcome` | `"succeeded"` \| `"failed"` | 是 | — |
| `error` | `{code, message}` | 失败时 | `code` 取值：`compile_error`、`compile_timeout`、`limit_exceeded`、`render_error`；`message` ≤ 4000 字符，会原样交给助手 |
| `bake` | object | `kind=bake_model` 且成功时 | `{glb_upload_id, thumbnail_upload_id, manifest}`。服务端据任务 `request` 组装 `commit_model` 和 `add_objects` / `set_instance_model`，经场景命令写入，写入者记为助手的 `run_id` |
| `render` | object | `kind=render_view` 且成功时 | `{image_upload_id}`（暂存图片，长边 1024） |
| `export` | object | `kind=export_shots` 且成功时 | `{export_operation_id}`：渲染端已调用 `api: 导出分镜` 时的操作 ID，服务端据此读回执 |

### 成功响应
200：`{job_id, state, result}`，`result` 结构见 `data: 场景客户端任务`。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 缺少与 kind 对应的结果字段、错误码非法 | 修正 |
| 404 | `not_found` | 任务不存在或不属于当前账号 | 放弃 |
| 409 | `creative_state_conflict` | 租约已失效或令牌不匹配；暂存上传不可用 | 丢弃产物；任务会被重新认领或已过期 |
| 409 | `creative_document_conflict` | 写入场景时单元冲突（例如目标对象已被摄影师删除） | 不重试；服务端已把任务记为 `failed`，并把冲突信息交给助手 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
- **原子性**：结果落库、任务终态、运行继续在同一事务里。`creative_document_conflict` 是例外：此时任务记为 `failed`（结果为冲突信息）后提交，返回 409 只是告知编辑器。
- **幂等**：沿用公共约定。网络中断后用同一 key 重试或查回执。

### 调用示例
```json
{"operation_id": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", "client_created_at": "2026-10-22T10:05:12Z",
 "payload": {"claim_token": "clm_Q2w3E4r5T6y7U8i9", "outcome": "succeeded", "render": {"image_upload_id": "cupl_cc33"}}}
```
```json
{"job_id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "state": "succeeded",
 "result": {"render": {"content_revision_id": "ccrev_77aa"}}}
```
编译失败示例（请求）：
```json
{"operation_id": "2c3d4e5f-6a7b-4c8d-9e0f-1a2b3c4d5e6f", "client_created_at": "2026-10-22T10:07:00Z",
 "payload": {"claim_token": "clm_Z9y8X7w6V5u4T3s2", "outcome": "failed",
             "error": {"code": "compile_error", "message": "ReferenceError: archCount is not defined (line 12)"}}}
```

### 兼容与变更
首次定义。

## api: ADD 导出分镜

### 用途与调用
- **用途**：把渲染端出的导出图落成画布图片节点，并记录 `based_on`。
- **消费者**：前端渲染端（摄影师点导出，或执行 `export_shots` 任务时）。
- **调用**：`POST /api/v1/creative/documents/{id}/scene-exports`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：场景和目标画布都属于当前账号，画布未归档，且该画布上存在这个场景的节点。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `canvas_id` | string | 是 | 场景节点所在画布 |
| `scene_node_id` | string | 是 | 场景节点；新节点放在它的右侧 |
| `scene_revision` | CreativeRevision | 是 | 出图时的场景修订；必须等于或早于当前修订，并且仍存在 |
| `shots` | array，1–32 | 是 | 每台摄像机一项 |
| `shots[].camera_object_id` | string | 是 | 在 `scene_revision` 里存在且 `kind=camera` |
| `shots[].images` | array，1–3 | 是 | `{pass:"clay"\|"line"\|"lit", upload_id}`，同一镜头内 `pass` 不重复；暂存图片就绪且未消费 |
| `job_id` | string | 否 | 来自 `export_shots` 任务时填写 |

### 成功响应
200：`{change_id, nodes:[{camera_object_id, pass, node_id, content_revision_id}]}`。节点经画布命令层创建，可以在画布上撤销（撤销组为同一 `change_group_id`）。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、`pass` 重复、摄像机不存在于该修订 | 修正 |
| 404 | `not_found` | 场景、画布或场景节点不存在，或不属于当前账号 | 停止 |
| 409 | `creative_state_conflict` | 画布已归档；暂存上传不可用 | 恢复画布或重新导出 |
| 413 | `creative_size_limit` | 一次创建的节点超过画布批量上限 | 分批导出 |

### 交互规则
- **原子性**：全部节点一起创建或都不创建。
- **幂等**：沿用公共约定。
- **过时**：后续场景修改不影响已创建的节点；过时提示由节点视图计算（`data: 导出图来源`）。

### 调用示例
```json
{"operation_id": "4d5e6f70-8192-4a3b-9c4d-5e6f708192a3", "client_created_at": "2026-10-25T14:00:00Z",
 "payload": {"canvas_id": "ccv_12ab", "scene_node_id": "cnode_34cd", "scene_revision": "18",
   "shots": [{"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
              "images": [{"pass": "clay", "upload_id": "cupl_dd44"}, {"pass": "line", "upload_id": "cupl_ee55"}]}]}}
```
```json
{"change_id": "cchg_56ef", "nodes": [
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "clay", "node_id": "cnode_78a1", "content_revision_id": "ccrev_a1b2"},
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "line", "node_id": "cnode_78a2", "content_revision_id": "ccrev_a1b3"}]}
```
```json
{"error": {"code": "validation_failed", "message": "摄像机 sobj_x 在修订 18 中不存在"}}
```

### 兼容与变更
首次定义。图片节点 `config` 新增 `based_on`，属于兼容扩展；旧读取端忽略这个字段。

## api: ADD 模型存入资产库

### 用途与调用
- **用途**：把场景里某个实例当前引用的模型修订存入资产库（复制语义，见 I7）。
- **消费者**：编辑器对象菜单「存入资产库」；后续的助手库整理工具。
- **调用**：`POST /api/v1/creative/assets/from-scene-model`，`Idempotency-Key` 必填，外层为公共约定。成功响应复用 `CreativeAssetCreateResult`（`api/openapi.yaml`）。
- **权限**：场景属于当前账号。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `document_id` | string | 是 | 场景文档 ID |
| `object_id` | string | 是 | `kind=model_instance` 的对象 |
| `expected_object_rev` | CreativeRevision | 是 | 该对象单元当前 `rev`，防止存入摄影师没看到的版本 |
| `target` | `CreativeUploadAssetTarget` | 是 | 沿用现有结构：`title` 必填，`description` / 分组 / 标签 / 收藏可选 |

### 成功响应
- 201：已创建资产。
- 200：同一模型修订已在库中，`reused = true`，只追加归类。

两者的语义都与 `/creative/assets/from-canvas-node` 一致。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 对象不是模型实例；`target` 不合法 | 修正 |
| 404 | `not_found` | 场景或对象不存在 | 重读 |
| 409 | `creative_document_conflict` | `expected_object_rev` 与当前不符 | 重读后确认 |
| 409 | `creative_state_conflict` | 模型修订不可用 | 提示 |

### 调用示例
```json
{"operation_id": "6e7f8091-a2b3-4c4d-8e5f-60718293a4b5", "client_created_at": "2026-10-28T09:00:00Z",
 "payload": {"document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10", "object_id": "sobj_Z1x2C3v4B5n6M7a8S9d0f",
             "expected_object_rev": "6", "target": {"title": "哥特束柱", "description": "可调高度的束柱"}}}
```
```json
{"asset_id": "cast_90ab", "revision": "1", "content_revision_id": "ccrev_9e2a", "library_revision": "211", "tag_mapping": {}, "reused": false, "title": "哥特束柱"}
```

### 兼容与变更
首次定义。**从资产库放入场景**不新增接口：编辑器用资产的 `content_revision_id` 调 `add_objects`（`kind=model_instance`）。之后编辑时按 I4 分叉，资产不受影响。示例按 2026-10-16 的 `CreativeAssetCreateResult` 编写；C6 实施前再核对一次。
