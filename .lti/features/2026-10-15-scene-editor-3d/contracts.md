<!-- 已批准的目标契约，不代表项目当前已实现。每个条目由一个子项完整交付（见 roadmap.md「交付契约」列）。
公共约定引用：错误封套 `ErrorEnvelope`（api/openapi.yaml components.schemas.ErrorEnvelope）；幂等与回执沿用 creativeops：
写接口必带 `Idempotency-Key` 头（uuid），请求体外层为 `{operation_id, client_created_at, payload}`，operation_id 与头一致；
同一 key 同一请求体回放原回执，同一 key 不同请求体返回 409 `idempotency_conflict`；未知结果用 `GET /api/v1/creative/operations/{operation_id}` 查回执。
修订号类型沿用 `CreativeRevision`（正 BIGINT 的十进制字符串）。所有路径前缀 `/api/v1`，鉴权为现有登录会话 Bearer token，资源一律限定当前账号；
跨账号或不存在统一返回 404 `not_found`。
通用文档的读取外壳、命令外壳（`expected` + `operations`）、冲突错误 `creative_document_conflict`、回收站、修订历史和工作会话指针，
由独立 change `creative-document-foundation`（通用文档底座）定义和交付。本 Epic 只定义 `type_key = "scene"` 的分支，以及场景专属的动作。 -->

## architecture: MODIFY 场景模块

### 职责与边界
- 依赖通用文档底座（`creative-document-foundation`）：场景以文档类型端口注册为 `scene` 类型，读取、命令、回收站和历史都走通用入口。
- `backend/internal/creativescene`：
  - 场景快照的 schema 校验、单元版本与命令应用、恢复。
  - 模型提交（消费暂存上传，写 `model` 内容）。
  - 客户端任务生命周期、导出编排。
  - 场景助手工具的领域入口。
- 不职责：执行几何代码、渲染、画布节点的布局规则（调用画布命令层完成）。
- 依赖方向：`creativeagent` → `creativescene` → `creativecontent` / `creativestaging` / `creativecanvas`（命令层）/ 通用文档层 / `platform/store` / `platform/realtime`。`creativescene` 不 import gin（ADR-003），`gin.Context` 不下穿。客户端任务到达终态时，`creativescene` 经自己定义的端口通知 `creativeagent`（进程启动时注册实现），不反向 import。画布读取导出图节点时，`creativecanvas` 经自己定义的端口问 `creativescene` 这张图是否过时（进程启动时注册实现），`creativecanvas` 不 import `creativescene`。
- 前端 `frontend/src/creative-scene/`：编辑器外壳（R3F）、场景状态（快照 + outbox + 撤销栈）、沙箱宿主、渲染端（客户端任务执行器）、材质库、导出器。编辑视图嵌入画布的同一个助手面板。渲染端不依赖编辑器外壳：画布页在认领到任务时按需加载它，摄影师不打开场景也能让助手搭建；没有任务的画布页不加载 3D 引擎。
- 渲染、烘焙、截图、导出只在浏览器完成；服务端不渲染。服务端经客户端任务派发工作，经暂存上传接收产物。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图。摄影师手动导出时调 `POST /creative/documents/{id}/scene-exports`；助手导出（`export_shots`）把图片清单随完成任务提交，在完成事务里放置。两条路径走同一个放置流程：画布命令层建图片节点，写入 `based_on`。读画布时经上述端口为导出图节点计算过时状态。

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
- **依赖**：`THREE`、`lib` 和 `rng` 都由宿主注入，模块内不得出现 `import` 语句或动态 `import()`。`lib` 是平台的建模标准库，按版本发布（`lib@1`），模块文本里声明 `export const lib_version = 1`，省略时为 1。`rng` 是带种子的确定性随机数。`Math.random` 在沙箱内被替换为每次编译从固定种子开始的确定性序列（three.js 创建对象时用它生成 UUID，不能禁用）；`Date` 不可用。
- **`lib@1`**（发布后行为不再改变，需要新能力时发布 `lib@2`，沙箱同时内置所有已发布版本）：
  - `lib.material(slot, hint?)`：名为 `slot:<slot>` 的 `MeshStandardMaterial`，`userData.hint = hint`；同一次编译里同名槽返回同一个材质。
  - `lib.subtract(a, b)`、`lib.union(a, b)`、`lib.intersect(a, b)`：两个 `Mesh` 按各自的世界变换做布尔运算（`three-bvh-csg`，保留材质分组），返回位于原点的新 `Mesh`。
  - `lib.extrude(shape, depth, {bevel?})`：`shape` 是 `THREE.Shape` 或二维点数组，返回 `BufferGeometry`。
  - `lib.lathe(points, segments = 32)`：二维轮廓绕 Y 轴旋转，返回 `BufferGeometry`。
  - `lib.array(object, count, step)`：线性阵列，`step` 为 `[x,y,z]`，返回 `Group`。
  - `lib.radial(object, count, radius, axis = 'y')`：环形阵列，返回 `Group`。
- **材质**：只能使用 `MeshStandardMaterial`（或 `lib.material(slot, hint)` 生成的同类材质）的常量参数。材质名 `slot:<id>` 表示可分配材质预设的槽位，`userData.hint` 给语义提示（如 `stone`、`wood`）。不得使用 `ShaderMaterial` / `RawShaderMaterial`，也不得使用任何纹理（外部 URL、数据纹理、视频纹理）。程序化外观由编辑器材质库在渲染时提供。导出的 GLB 必须落在 `data: 模型内容` 的静态 GLB 子集内：没有形变、骨骼、动画、灯光和摄像机。
- **命名约定**（进入 manifest）：对象名 `part:<id>` 表示可寻址部件，用于点选改某个部位；空对象 `anchor:<id>` 表示挂点。
- **几何**：只接受三角网格（GLB 中 primitive 的 `mode` 为 4）；线和点会被拒绝。
- **产物**：沙箱返回 GLB（经 `GLTFExporter` 二进制导出）和规范化的模块信息 `{contract_version, lib_version, mount, params_schema, params}`（`params` 为补齐默认值后实际使用的值）。manifest 不由沙箱提供，由宿主和服务端从 GLB 算出：`{bounds:{min:[x,y,z],max:[x,y,z]}, triangles, slots:[{id,hint}], parts:[id], anchors:[{id,position:[x,y,z]}]}`。GLB 是几何事实源，只有代码、参数或 `lib_version` 变化时才重新编译。
- **上限**（初始值，见 design 未决 #3）：代码 ≤ 64 KiB；编译 ≤ 5 秒；三角形 ≤ 300k；GLB ≤ 16 MiB。
- **为什么**：只用 primitive 的 JSON 表达力不够；整个场景是一段代码则无法支持手动编辑、点选修改和撤销。以物件为粒度写代码，既有建模自由度，又让场景编排可编辑。先例见 research §3（Pascal authored objects）。
- **不适用于**：内置基础体是前端打包的可信模块，遵守同一契约，但不经沙箱、不烘焙。外部 GLB 导入（下一版）不经过本契约。
- **例子**：C2 交付后补充内置基础体 `box@1` 的实现路径作为参照。

## patterns: ADD 客户端代码沙箱

- **约定**：任何来自模型或助手的代码，只能在满足以下条件的环境中执行：
  1. 独立的 `<iframe sandbox="allow-scripts">`（不含 `allow-same-origin`，因此是 opaque origin），内容是平台自带的沙箱页面 `/scene-sandbox/sandbox.html`（具体文件路径；该前缀下找不到的文件返回 404，不回退到应用首页）。页面由 HTTP 头下发 CSP，模板只有一份（`frontend/public/scene-sandbox/csp.txt`）：
     `default-src 'none'; script-src <站点源>/scene-sandbox/ blob:; worker-src blob:; connect-src 'none'; img-src blob: data:; style-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors <站点源>`。
     `script-src` 和 `frame-ancestors` 必须写明确的站点源，不能用 `'self'`：WebKit 在 opaque origin 里不把 `'self'` 匹配到站点源（穿刺实测）。限定到沙箱路径，避免代码对其他路径发起带 cookie 的脚本请求。
  2. 实际编译在沙箱页面内部的 **classic** Worker 中进行，Worker 从页面内嵌的源码经 Blob URL 创建（Chromium 拒绝 opaque origin 里从 Blob 创建 module Worker）。**每次编译新建一个 Worker**，结束即销毁，避免上一次的代码改写 three.js 或 `lib` 影响下一次。
  3. 宿主通过 `postMessage` 传入代码、参数和 `lib_version`，只接收 GLB 的 ArrayBuffer、规范化的模块信息和错误信息。只接受来自该 iframe 的消息；传出的消息有固定结构，宿主逐字段校验，超长或结构不符一律丢弃。模块信息按不可信 JSON 校验（与服务端同一套参数定义规则）；代码哈希由宿主对自己发出的代码计算，不取沙箱的值。
  4. 宿主为每次编译设硬超时（5 秒）；超时后移除 iframe 并重建。沙箱不持有凭证，不能访问 cookie、站点存储、父页面 DOM 和网络，也不能导航顶层页面。
  5. **沙箱回传的内容不可信**：宿主先按 `data: 模型内容` 的静态 GLB 子集做与服务端相同的预检，并按同一算法从顶点数据重新计算 manifest、检查上限；预检不通过按编译失败处理。预检通过后才交给 `GLTFLoader`，并用 `LoadingManager.setURLModifier` 拒绝 `blob:`、`data:` 以外的地址，否则 GLB 里的 `uri` 会让可信页面带着 cookie 发请求。服务端另以 worker 从 GLB 算出的 manifest 为准。
- **为什么**：助手会读网页，网页内容可能把恶意代码注入生成代码。代码在摄影师浏览器里执行，和摄影师的登录会话同处一个浏览器，所以必须隔离。
- **验收**：C2 交付安全样例集（尝试 `fetch`、`XMLHttpRequest`、`WebSocket`、`EventSource`、动态 `import`、`importScripts`、`document.cookie`、`localStorage`、`indexedDB`、`parent.document`、`top.location`、`eval`、死循环、内存耗尽、超大输出、GLB 内含外链 URI），全部被阻断或在时限内终止，外部地址收不到请求。样例集在 Chromium 上纳入 `make check-frontend`；WebKit 用环境变量手动运行，结果记入 verification。穿刺记录见子项 C2 的 `discovery/research/sandbox-spike.md`。
- **不适用于**：前端自带的可信代码（内置基础体、材质库）。
- **备选**：将来某个目标浏览器的样例无法全部通过时，改用 QuickJS-wasm 执行，契约其余部分不变。

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
| `objects[].light` | 按 `type` 区分，字段与 three.js 灯光类同名同义，见下方「灯光」 | `kind=light` 时 | 只有 `type` 必填，其余缺省取 three.js 默认值，存储时补齐 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near>0, far>near（米，同 three.js `PerspectiveCamera`）, label≤40, depth_of_field:bool, f_stop:0.5…64, focus>0（米，同 three.js `PerspectiveCamera.focus`）, focus_object_id:物件 ID 或 ""}` | `kind=camera` 时 | 各字段可省略，缺省 `{35,[36,24],"3:2",0.1,1000,"",false,2.8,10,""}`，存储时补齐；景深四个字段为修订 v8 新增，旧快照在读取时补齐。朝局部 −Z 看（three.js 摄像机约定）；视场角按画面长边对应传感器长边算出：`tan(fov/2) = max(sensor_mm) / (2·focal_mm) / max(画幅宽高比, 1)`，`fov` 为 three.js 的垂直视场角（修订 v8），不写 three.js 的 `fov`。`focus_object_id` 非空时按该物件原点沿视线方向的深度对焦：写入时必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己；之后目标被删除不算违反契约，按 `focus` 对焦。写空字符串取消对焦物件（按键合并下 `null` 等于没写）。`depth_of_field = true` 时，白模导出按光圈和对焦虚化（design「渲染模式与导出」） |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型 `content_revision_id`。
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
- 例外：修订 v7 的改名（雾和摄像机的 `near_m`/`far_m` → `near`/`far`，灯光 `range_m` → `distance`、`angle_deg` → `angle` 并补 `decay`）发生在上线之前，生产库没有场景数据。由一次迁移改写已有的场景快照，`schema_version` 仍为 1，不保留旧字段的读取兼容。旧的 `angle_deg` 没有定义是整锥角还是半角，也从未渲染过，迁移按整锥角换算（`angle = angle_deg / 2`，换成弧度）；2026-10-18 开发库里没有灯光物件。
- 修订 v8（C4 起草时）：摄像机视场角改为画面长边对应传感器长边（原为固定按 `sensor_mm[1]` 算垂直视场角，竖幅画面比同焦距的全画幅相机窄约 1.5 倍）。只改渲染端的换算，字段外形和存储不变，不迁移；默认传感器下 3:2 横幅的画面不变，助手已建的非 3:2 摄像机和 `render_view` 临时视点的取景随之改变。
- 修订 v8（C4 起草时）：摄像机新增 `depth_of_field`、`f_stop`、`focus`、`focus_object_id`。不迁移：读取时补齐默认值（服务端解码时补齐，读取场景的响应里也带齐），之后的写入按完整字段存储。「空物体」是没有子物件的 `group`，不新增 `kind`。
- 修订 v10（C5 起草时）：`render` 新增 `exposure_ev`。灯光按 three.js 的物理单位（坎德拉），室外有太阳和只有几盏灯的室内亮度差几个数量级，固定曝光无法同时照顾两者，所以由场景自己定曝光，与摄影里按场景定曝光一致；用「档」而不是倍数，与相机的曝光补偿、Blender 色彩管理的曝光相同。不迁移：读取时补 0，读取场景的响应里也带齐。

## data: ADD 模型内容

### 用途与存储角色
- **用途**：可复用的几何物件，由场景实例和资产库资产共同引用。
- **存储**：
  - `creative_contents.kind = 'model'`。
  - 每个版本一条 `creative_content_revisions`（`schema_version = 1`）。
  - GLB 存为 blob，经 `creative_content_objects` 以 `role='original'` 绑定；缩略图 PNG 以 `role='thumbnail'` 绑定。
  - 上传走 `creative_uploads`，`declared_kind` 加 `model`，校验格式加 `model/gltf-binary`（`.glb`），上限 16 MiB，只允许暂存目标。
  - **两种形态**：
    - 暂存形态：worker 校验 GLB 通过后写入，载荷只有 `{format, manifest}`，manifest 由服务端从 GLB 算出；由暂存候选持有，只供 `commit_model` 消费，不能被场景引用，也不能被采用到资产或画布节点。候选被消费后（`adopted_target_kind = 'document'`）失去持有方，按 GC 回收。
    - 完整形态：`commit_model` 写入，载荷为下表结构；GLB 与缩略图对象复用暂存修订的 blob，不复制字节。
  - **写入口**：`model` 不能经通用写入口（`creativecontent.Validate` / `WriteAndRetain`）写入，资产、画布 `replace_content`、生成输出都写不进来。暂存形态只由 `WriteMediaAndRetain`（worker）写，完整形态只由 `WriteModelAndRetain`（`commit_model`）写。
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
- **接受的 GLB 子集**（worker 的 verify 阶段用 Go 实现，编辑器宿主用同一算法预检；不执行代码，文件里的任何声明都不直接采信；不在子集内的一律拒绝）：
  - 容器：magic 为 `glTF`，version 为 2，总长度与实际一致；JSON 块加至多一个 BIN 块，4 字节对齐。
  - 缓冲：至多一个 `buffer`，没有 `uri`。
  - 场景树：恰好一个 `scene`；节点下标全部有效，每个节点至多一个父节点，无环；变换只用 TRS 或 `matrix` 之一，数值有限。
  - 网格：primitive 的 `mode` 为 4；属性只允许 `POSITION`（`FLOAT VEC3`，必有）、`NORMAL`、`TANGENT`、`TEXCOORD_0/1`、`COLOR_0`；不允许 `targets`、`JOINTS_*`、`WEIGHTS_*`。
  - 访问器：不用 `sparse`；按组件对齐；`byteStride` ≥ 元素大小；全部数据落在 `bufferView` 和 BIN 内；索引值小于顶点数；索引数或非索引顶点数是 3 的倍数。
  - 不允许 `images`、`textures`、`samplers`、`skins`、`animations`、`cameras`；`extensionsRequired` 为空，`extensionsUsed` 只允许 `KHR_materials_emissive_strength`。
  - 预算：节点 ≤ 10000，遍历到的 primitive 实例 ≤ 10000，三角形 ≤ 300000，字节 ≤ 16 MiB；读取的 POSITION 顶点总数（每个访问器计一次）≤ BIN 块字节数 / 12。访问器可以指向同一段数据，少了这一项，各项计数都在预算内的文件也能让校验读上数十亿个顶点；访问器互不重叠的文件不会超出。
  - 不合规时上传以 `failed` 结束，失败码沿用现有的 `media_unsupported`。
- **manifest 的算法**：三角形数按场景树遍历累加；每个 primitive 的局部范围**从 POSITION 实际数值算出**，并与访问器声明的 `min` / `max` 逐分量核对（必须存在且相等，否则拒绝）；包围盒取局部范围的 8 个角点经节点世界矩阵变换后合并（与 three.js `Box3.setFromObject` 非精确模式一致）；世界矩阵、包围盒与挂点坐标必须是有限值，各节点数值有限但相乘溢出时拒绝；槽位取材质名 `slot:<id>` 与 `extras.hint`，部件取节点名 `part:<id>`，挂点取节点名 `anchor:<id>` 及其世界坐标，名称取 JSON 原文；槽位 ≤ 32、部件 ≤ 200、挂点 ≤ 200。
- **提交时**：manifest 必须与暂存形态中服务端从 GLB 算出的一致，不一致则拒绝。比对规则：`slots`、`parts`、`anchors` 先按 `id` 排序；`id`、`hint`、`triangles` 精确相等；`bounds` 与挂点坐标容差 1e-4 m。名称取 GLB JSON 原文（three.js 加载后会删掉节点名里的 `:`）。写入完整形态时使用服务端的值。
- **读取**：GLB 和缩略图走现有媒体读取票据（`IssueTicket` / `Open`），角色分别为 `original` 和 `thumbnail`（票据角色加 `thumbnail`）。

### 生命周期与演进
- 持有方：场景修订引用表、资产、画布节点（后续若有）。三者均登记进 `creativereference.LiveOwnerCondition` 和守卫测试。没有持有方的修订按媒体 GC 规则回收。媒体 GC 的类型范围（`creativemedia/gc.go` 的 `unheldMediaRevision`）目前只含 image / video / audio，C2 必须把 `model` 加进去并登记守卫测试；上线时先升级 creative-worker，再开放模型写入。
- 外部 GLB 导入（下一版）会加 `generator.language = "imported"`，且没有 `code`。本版读取端遇到未知 `language` 时只能显示，不能调参数。

## data: MODIFY 场景客户端任务

### 用途与存储角色
- **用途**：服务端派给创意空间页面（渲染端）执行的工作（烘焙模型、回看截图、导出），以及它的认领、租约、结果和关联的助手运行。
- **存储**：新表 `creative_scene_jobs`（PostgreSQL）。
- **角色**：权威主数据（任务状态）。产物本身在暂存上传或内容里，不在本表。
- **所有权**：`creativescene` 创建、过期、取消；渲染端经认领、续租、完成接口推进；`creativeagent` 经 `creativescene` 的终态端口读取任务结果，并在任务终态时继续运行。

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
- `export_shots`：`{camera_object_ids, passes, canvas_id, scene_node_id}`（C4）。
  - `camera_object_ids`：建任务时当前快照里的摄像机（工具未指定时为全部摄像机，按物件顺序）；`passes`：`clay` / `line` / `lit` 的子集，不重复。图片总数 ≤ 48（`api: 导出分镜` 的上限）。
  - `canvas_id`、`scene_node_id`：运行所在画布，以及该画布上这个场景的节点（有多个时取最早创建的）；建任务时确定，场景不在该画布上时不建任务。
  - 渲染端出图上传后，把图片清单随完成任务提交（`api: 完成场景客户端任务` 的 `export`），节点在完成事务里创建。任务没有成功，画布上就没有节点；同一任务最多一组节点。

`result`：
- 成功：`bake_model` 为 `{content_id, content_revision_id, object_ids, revision}`；`render_view` 为 `{image_upload_id, content_revision_id, scene_revision}`（暂存截图及其实际渲染的场景修订）；`export_shots` 与 `api: 导出分镜` 的成功响应相同：`{change_id, scene_revision, nodes:[{camera_object_id, pass, node_id, content_revision_id, lens}]}`（`lens` 见 `data: 导出图来源`，修订 v9）。
- 失败：`{code, message}`。`code` 取完成接口的错误码，或服务端写入的 `conflict`（场景单元冲突）、`invalid_result`（结果不合规，例如 manifest 与 GLB 不符、物件超出上限、父级不存在；导出无法放置，例如画布已归档、场景节点已不在画布）、`host_error`（第 3 次租约过期）、`expired`、`cancelled`。`conflict` 时另带 `conflicts`（单元、预期与当前版本、改动者）和 `bake`（`{glb_upload_id, thumbnail_upload_id, module, manifest}`），助手重试时复用这份烘焙产物，不重新编译。

索引：`(account_id, state, created_at)`，用于账号级认领；`(account_id, document_id, state, created_at)`，用于按场景认领；`(state, deadline)`，用于过期扫描；`(run_id)`，用于取消运行时找到它的任务。

### 访问模式与保证
- **认领**：取最早的 `queued` 任务（指定场景时只在该场景内），用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。没有任务时只走索引、不加锁，渲染端在每次失效通知时都会调用。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。两者都经终态端口让关联运行继续。
- **完成**：经终态端口复查关联运行（与助手写工具同一套准入：能力、deadline、Skill、consent）→ 校验令牌与租约 → 落产物 → 改终态 → 让运行继续，在同一事务里提交（design「客户端任务」）。复查不通过时运行按现有规则结束，任务随之取消。
- **取消**：运行被取消时，在取消运行的同一事务里把它的非终态任务改为 `cancelled`，清空令牌。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。修订 v10：`render_view` 的 `mode` 增加 `lit`（C5），请求外形不变，旧任务缺省按 `clay`。

## data: ADD 导出图来源

- **用途**：记录画布图片节点由哪个场景、哪个修订、哪台摄像机、哪种导出方式生成，用于提示过时。
- **存储**：`creative_nodes.config` 内的 `based_on` 对象，只对导出产生的 `core.image` 节点写入：`{scene_document_id, scene_revision: CreativeRevision, camera_object_id, pass:"clay"|"line"|"lit", content_revision_id, fingerprint, lens}`。`content_revision_id` 是导出时放进节点的那份图片内容修订；`fingerprint` 是 `scene_revision` 的快照对这台摄像机、这种导出方式的渲染指纹（带版本前缀，如 `v1:`），客户端只当不透明字符串。
- **镜头快照 `lens`**（修订 v9）：导出时这台摄像机的镜头，`{focal_mm, depth_of_field, f_stop, focus_m, focus_object_name}`。`focus_m` 是实际对焦距离（米）：有对焦物件时按物件原点沿视线的深度算出，否则为 `focus`，并夹在 `near`、`far` 之间；`focus_object_name` 是对焦物件的名称，按距离对焦或对焦物件已删除时为空字符串。景深关闭时也照样记录。`lens` 只说明这张图是怎么拍的，不参与过时判断（指纹已计入这些参数）；没有 `lens` 的节点按未知处理。
- **角色**：派生信息，只用于提示，不参与权限和内容。
- **写入**：只由导出的放置流程经画布命令层写入，入口是「导出分镜」接口和 `export_shots` 的完成事务；画布命令的 `add_node` 不接受 `config`，客户端无法写入。写入后不再修改：节点当前内容不是 `content_revision_id`（换了图、切了版本）时，来源信息不生效，所以改图的操作不需要逐一清除它。复制节点时随 `config` 一起复制，内容相同，判断照样成立。
- **读取**：画布节点快照新增可选只读字段 `based_on_state`，只出现在 `based_on` 生效的节点上（不生效时不返回）：
  - `unavailable`：来源场景不存在、在回收站或快照不可展示，不显示过时；
  - `current`：场景之后没有与这张图相关的修改；
  - `stale`：有，前端显示「场景已更新」。
- **助手读取**（修订 v9）：`read_canvas` 返回的节点里，`based_on` 生效的导出图带 `export_source: {scene_document_id, scene_revision, camera_object_id, pass, lens, state}`，`state` 即 `based_on_state`。画布的 HTTP 节点快照不变，仍只返回 `based_on_state`。
- **过时判断**：读取时计算，不落库。对场景当前快照算同一台摄像机、同一种导出方式的渲染指纹，与 `fingerprint` 比较，不同即 `stale`；这台摄像机已不在当前快照里也算 `stale`。指纹计入哪些内容见 design「渲染模式与导出」，按导出方式区分。改动后又改回原样不算过时。指纹版本与当前不同时算 `stale`，不读旧快照重算（修订 v9，读画布不增加查询；指纹版本只在渲染规则变化时升级，届时旧图本来就该重新导出）。

## data: ADD 模型资产

- **用途**：资产库可以存放 `model` 资产。
- **存储**：`creative_assets.kind` 的 CHECK 加 `model`；资产引用模型的 `content_revision_id`。资产库现有能力（分组、标签、评分、回收站、查重）对 `model` 一视同仁；查重按 GLB 的 sha256。
- **生命周期**：回滚时如已有 `model` 资产则拒绝，前向修复。资产库不记录资产被哪个场景使用过（I7）。

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

## api: MODIFY 提交场景命令

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
| `add_objects` | `objects`（1–2000 个完整对象，见数据契约；`kind` 本条目允许 `primitive`/`light`/`camera`/`group`；`primitive.params` 缺省的键补成默认值；`light`、`camera` 缺省的字段补成数据契约里的默认值） | `objects`（一次；新对象的两个单元以 N+1 出现在 `changed_units` 中） |
| `update_objects` | `updates`（1–2000 项）；每项 `object_id`，以及 `name`/`transform`/`visible`/`locked`/`primitive.params`/`material_slots`/`light`/`camera` 中任选，至少一项（省略即不改；`primitive.params`、`light`、`camera` 按键合并，`light.type` 改变时去掉新类型没有的字段、缺少的补默认值；`material_slots` 整体替换；字段必须与对象的 `kind` 相符）；同一物件只能出现一次 | 每项：只改摆放字段时为 `object:<id>`；只改内容字段时为 `object:<id>:content`；两类都改时两个单元都涉及 |
| `remove_objects` | `object_ids`（1–2000；分组连同后代一起删除） | `objects`，以及每个被删对象的 `object:<id>` 和 `object:<id>:content` |
| `reparent_objects` | `moves`（1–2000 项 `{object_id, parent_id（null 为根）, order, transform?}`）；可选 `create_groups`（1–2000 个新分组，外形同 `add_objects` 里 `kind=group` 的对象）和 `remove_groups`（1–2000 个分组 ID）。按顺序执行：先建 `create_groups`；再按 `moves` 改挂，`parent_id` 可以是本次新建的分组，带 `transform` 时同时替换该物件的局部变换（调用方据此保持世界位置，服务端照写）；最后删除 `remove_groups`，删除时它们必须没有子物件。同一物件在三处合计只能出现一次（修订 v10） | `objects`（一次）；带 `transform` 的移动另涉及 `object:<id>`；新建的分组的两个单元以 N+1 出现在 `changed_units` 中；删除的分组涉及 `object:<id>` 和 `object:<id>:content` |
| `set_environment` | `environment`（整体替换） | `environment` |
| `set_render` | `render`（按键合并，只改写到的字段；修订 v10） | `render` |
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
| 400 | `validation_failed` | 外形非法、单元重复、`expected` 缺单元、对象违反数据契约（父子成环、未知内置体、数值越界、ID 与当前对象重复等）、引用的物件不存在、本次写到的 `focus_object_id` 不可对焦（不存在、是摄像机自己、灯光或摄像机）、`reparent_objects` 里同一物件出现多次、`create_groups` 不是分组、`remove_groups` 不是分组或删除时仍有子物件、`restore_units` 列了 `objects` 或只列了存在性会变化的对象的一个单元 | 修正，不可原样重试 |
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
编组示例（修订 v10，Ctrl + G：新建分组，把两个分属不同父级的物件挂进去，并写入保持世界位置的局部变换；一个修订，撤销是另一个 `reparent_objects`）：
```json
{"operation_id": "8a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", "client_created_at": "2026-10-20T09:40:00Z",
 "payload": {
   "expected": {"objects": "5", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ": "4", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a": "5"},
   "operations": [{"op": "reparent_objects",
     "create_groups": [{"id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "kind": "group", "name": "石台组", "parent_id": null, "order": 3,
       "transform": {"position": [0.75, 0, 2], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}, "visible": true, "locked": false}],
     "moves": [
       {"object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 0,
        "transform": {"position": [0.75, 1.4, 5], "rotation": [0, 0.0998, 0, 0.995], "scale": [1, 1, 1]}},
       {"object_id": "sobj_Q9w8E7r6T5y4U3i2O1p0a", "parent_id": "sobj_G1h2J3k4L5z6X7c8V9b0n", "order": 1,
        "transform": {"position": [-0.75, 0, -5], "rotation": [0, 0, 0, 1], "scale": [1, 1, 1]}}]}]}}
```
```json
{"revision": "6", "changed_units": ["objects", "object:sobj_G1h2J3k4L5z6X7c8V9b0n", "object:sobj_G1h2J3k4L5z6X7c8V9b0n:content", "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "object:sobj_Q9w8E7r6T5y4U3i2O1p0a"],
 "annotated_units": [], "removed_units": [], "reset_required": false}
```
取消编组时，`moves` 把子物件挂回分组原来的父级并写回局部变换，`remove_groups` 删除这个分组。

冲突示例（助手已先改过这台摄像机）：
```json
{"error": {"code": "creative_document_conflict", "message": "文档已被修改",
  "details": {"conflicts": [{"unit": "object:sobj_3kTMd9Qp2xLw7nB0cR5yZ", "expected_rev": "4", "current_rev": "6"}]}}}
```

### 兼容与变更
在通用命令接口上新增 `scene` 类型的操作集合，其他类型不受影响。`api: 场景模型命令` 会在此基础上增加操作，并放开 `add_objects` 的 `model_instance`，属于兼容扩展。旧客户端不会发送新操作。

修订 v10（C5 起草时）：`set_render` 由整体替换改为按键合并（`render` 新增 `exposure_ev` 后，只写 `mode` 的旧写法不能把曝光重置为 0；旧客户端总是写全部字段，不受影响）。`reparent_objects` 增加可选的 `moves[].transform`、`create_groups` 和 `remove_groups`，编组和取消编组因此能在一次提交里完成：同一批里每个单元只能被一个操作涉及，「新建分组 + 改挂」或「改挂 + 删除分组」若拆成两个操作，会两次涉及 `objects`。旧请求不受影响；`remove_objects` 删除分组时仍连同后代一起删除。

修订 v7：`update_objects` 里的 `light`、`camera` 由整体替换改为按键合并（字段允许省略后，整体替换会把没写的字段悄悄重置成默认值；three.js 里改 `light.intensity` 也只改这一项）；`add_objects` 可省略它们的字段。字段改名见 `data: 场景文档`。编辑器同一版本更新，旧字段名返回 400。

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
| `set_instance_model` | `object_id`、`model`（`{content_revision_id}` 或 `{ref}`）、`scope`（`"this"` 或 `"all_instances"`，缺省 `"this"`）、`object_ids`（`all_instances` 时必填，含 `object_id`，列出调用方看到的全部共用实例；每个都必须批前引用同一旧修订） | `object:<id>:content`（只动内容单元，所以摄影师同时移动这个物件不会冲突）；`all_instances` 时为 `object_ids` 中每个对象的内容单元。由调用方列出而不是服务端展开，避免并发新增的共用实例让请求缺少预期单元 |

- **分叉规则（I4）**：
  - `base_content_id` 被其他实例或资产引用时，`commit_model` 仍然在该内容上追加修订。是否让其他实例跟随，由 `set_instance_model` 的 `scope` 决定。
  - 资产持有的旧修订不受影响，因为资产引用的是修订而不是内容的最新版。
  - 编辑器和助手在 `scope = "this"` 且共享时，应传入不带 `base_content_id` 的 `commit_model`，即新建内容（分叉）。
- **持有方交接**：`commit_model` 在场景类型的 `Apply` 中新建模型内容，使用底座传入的 `Retain` 回调预登记引用（底座 `patterns: 文档类型端口`）。
- **校验**：暂存上传属于本账号且未消费，GLB 上传的声明类型为 `model`、缩略图为 PNG 图片；manifest 与暂存形态中服务端算出的一致；`code_sha256` 正确；参数符合 schema（缺省的键补默认值）；`commit_model` 必须出现在引用它的操作之前。消费后两个暂存候选标为已被文档采用。
- **成功响应**：在 `api: 提交场景命令` 的响应上增加 `models: {<ref>: {content_id, content_revision_id}}`，经底座的类型专属回执字段返回（`patterns: 文档类型端口`），随幂等回执重放。
- **失败响应**（在基础接口之上增加）：

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 409 | `creative_upload_unavailable` | 暂存上传不存在、属于其他账号、已过期、已消费或尚未就绪 | 重新上传 |
| 400 | `validation_failed` | manifest 与 GLB 不符、哈希不符、参数越界、上传类型不符、`ref` 未定义 / 重复 / 未被本批引用、单批 `commit_model` 超过 16 个 | 修正 |
| 400 | `creative_model_unavailable` | `content_revision_id` 不存在、不是完整形态的 `model` 或不可展示 | 重读后重新选择 |

不使用 404 和 `creative_state_conflict`：编辑器把命令返回的 404 当作文档不存在、把 `creative_state_conflict` 当作文档进了回收站。

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
- **用途**：渲染端认领一个待执行任务。指定场景时只取该场景最早的任务，否则取账号里最早的任务。
- **消费者**：前端渲染端：场景编辑视图（带 `document_id`）和画布页的后台渲染端（不带）。收到失效通知、连接或回到前台、完成上一个任务后调用。
- **调用**：`POST /api/v1/creative/scene-jobs/claim`，`application/json`。认领本身不需要幂等键：重复认领只会拿到下一个任务。
- **权限**：任务属于当前账号；指定的场景属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `editor_session_id` | body | string，`^[A-Za-z0-9_-]{16,64}$` | 是 | 标签页随机生成的会话 ID |
| `document_id` | body | string | 否 | 只认领该场景的任务 |
| `kinds` | body | string[] | 否 | 本端能执行的任务类型；缺省为全部已知类型 |

### 成功响应
- 200，认领成功：`{job: {id, document_id, kind, request, attempt, claim_token, lease_until, deadline}}`。`request` 的结构见 `data: 场景客户端任务`。
- 204，没有可认领的任务：无响应体。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 会话 ID 非法、`kinds` 含未知值 | 修正 |
| 404 | `not_found` | 指定的文档不存在、不属于当前账号或不是场景 | 停止认领该场景 |

### 交互规则
- 用 `FOR UPDATE SKIP LOCKED` 保证每个任务最多被一个会话认领。同一会话可以连续认领多个任务，按顺序执行。
- 没有任务时只读索引、不加锁，可以在每次失效通知时调用。
- 租约 30 秒，续租见 `api: 续租场景客户端任务`。

### 调用示例
```http
POST /api/v1/creative/scene-jobs/claim
Authorization: Bearer <token>
Content-Type: application/json

{"editor_session_id": "tab_Vx9q2LmN7pR4sT8w", "kinds": ["bake_model", "render_view"]}
```
```json
{"job": {"id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10",
  "kind": "render_view", "request": {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "mode": "clay"},
  "attempt": 0, "claim_token": "clm_Q2w3E4r5T6y7U8i9", "lease_until": "2026-10-22T10:05:30Z", "deadline": "2026-10-22T10:35:00Z"}}
```
失败示例：
```json
{"error": {"code": "validation_failed", "message": "editor_session_id 格式不正确"}}
```

### 兼容与变更
首次定义。修订 v6 起为账号级路径（此前未交付）。

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
200：`{lease_until}`，延长到当前时间 + 30 秒，但不超过 `deadline`。续租不触发任务通知。

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

## api: MODIFY 完成场景客户端任务

### 用途与调用
- **用途**：渲染端提交任务结果（成功或失败）。服务端在同一事务里落结果、改终态、让关联运行继续。
- **调用**：`POST /api/v1/creative/scene-jobs/{job_id}/complete`，`Idempotency-Key` 必填，外层为公共约定。
- **权限**：任务属于当前账号，`claim_token` 匹配，且租约未过期。

### 请求
`payload`：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `claim_token` | string | 是 | — |
| `outcome` | `"succeeded"` \| `"failed"` | 是 | — |
| `error` | `{code, message}` | 失败时 | `code` 取值：`compile_error`、`compile_timeout`、`limit_exceeded`、`render_error`、`host_error`（宿主侧失败：媒体校验失败、WebGL 上下文丢失、代码加载失败等）；`message` ≤ 4000 字符，会原样交给助手 |
| `bake` | object | `kind=bake_model` 且成功时 | `{glb_upload_id, thumbnail_upload_id, manifest, module}`。`module` 为 `{params_schema, params, mount}`：沙箱回传、宿主按服务端规则校验过的规范化模块信息。服务端用任务 `request` 里的代码计算 `code_sha256`、组装 `generator`，按 `api: 场景模型命令` 的规则校验，再组装 `commit_model` 和 `add_objects` / `set_instance_model`，经场景命令写入，写入者记为助手的 `run_id`；替换目标的预期版本取任务 `request.expected` |
| `render` | object | `kind=render_view` 且成功时 | `{image_upload_id, scene_revision}`：暂存图片（JPEG，长边 1024），以及渲染端实际渲染的场景修订（不得大于当前修订） |
| `export` | object | `kind=export_shots` 且成功时 | `{scene_revision, shots}`：渲染端实际渲染的场景修订，以及图片清单。`shots` 的结构与 `api: 导出分镜` 相同，必须正好覆盖任务 `request` 的摄像机和导出方式。服务端在保存点内按 `api: 导出分镜` 的规则放置节点（位置、标题、`based_on`），放到任务 `request` 的画布和场景节点旁，节点按 `request.camera_object_ids` 的顺序排行，写入者记为助手的 `run_id` |

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
| 409 | `creative_state_conflict` | 任务已不是 `claimed`、令牌不匹配或租约已失效；或关联运行的复查不通过（运行已结束，任务已取消） | 丢弃产物；任务会被重新认领、已过期或已取消 |
| 409 | `creative_upload_unavailable` | 暂存上传不存在、已过期或已被消费 | 租约仍有效时重新上传，用新的 operation id 重试 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
- **原子性**：结果落库、任务终态、运行继续在同一事务里。场景写入和导出的画布放置在保存点内执行，冲突、结果不合规或无法放置时回滚到保存点，任务记为 `failed`（带原因），运行照常继续，助手据此重做。导出的节点只在任务成功时创建。
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
首次定义。修订 v6：`bake` 增加 `module`；`render` 增加 `scene_revision`；错误码增加 `host_error`；暂存上传不可用改用 `creative_upload_unavailable`（与 `api: 场景模型命令` 一致）；冲突与结果不合规记为任务 `failed` 并返回 200（`creativeops` 只提交 2xx 回执）；完成时复查关联运行。修订 v8（C4 起草时）：`export` 由 `{export_operation_id}` 改为 `{scene_revision, shots}`，导出节点在完成事务里创建，不再经 `api: 导出分镜`。C3 只做了占位（服务端对 `export` 结果直接拒绝，没有调用方），不需要兼容。

## api: ADD 列出场景客户端任务

### 用途与调用
- **用途**：列出某个场景当前未结束的任务，供编辑器高亮「助手正在修改」的物件、在烘焙目标处显示占位。
- **消费者**：场景编辑视图。打开时、收到失效通知时（节流）调用。
- **调用**：`GET /api/v1/creative/documents/{id}/scene-jobs`，只读，无请求体。
- **权限**：场景属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景 ID |

### 成功响应
200：`{items: [{id, kind, state, run_id, created_at, target}]}`，只含 `queued` 和 `claimed`，按创建时间排序，最多 50 条。
- `target`：`bake_model` 为 `{op:"add", object:{name, parent_id, transform}}` 或 `{op:"replace", object_ids}`；其他 kind 为 null。
- 不返回代码、参数和认领信息。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 404 | `not_found` | 文档不存在、不属于当前账号或不是场景 | 停止 |

### 交互规则
只读，没有分页；重复调用没有副作用。

### 调用示例
```json
{"items": [{"id": "csjob_1f2e3d4c-5b6a-4789-8a9b-0c1d2e3f4a5b", "kind": "bake_model", "state": "claimed",
  "run_id": "crun_8c7b6a59", "created_at": "2026-10-22T10:05:00Z",
  "target": {"op": "replace", "object_ids": ["sobj_Z1x2C3v4B5n6M7a8S9d0f"]}}]}
```

### 兼容与变更
首次定义（修订 v6）。

## api: MODIFY 读取运行的场景改动

### 用途与调用
- **用途**：列出某次助手运行对一个场景改了哪些物件和设置，并给出每一处恢复到运行之前所需的 `restore_units`；已被别人改过的标为不可恢复。「恢复这一处」「撤销本轮」和助手撤回自己的改动共用这一份计算。
- **消费者**：助手面板的本轮改动清单；`edit_scene` 的撤回操作在领域层调用同一函数，不经 HTTP。
- **调用**：`GET /api/v1/creative/documents/{id}/run-changes?run_id=<run_id>`，只读。
- **权限**：场景和运行都属于当前账号。

### 请求
| 字段 | 位置 | 类型 | 必传 | 说明 |
|---|---|---|---|---|
| `id` | path | string | 是 | 场景 ID |
| `run_id` | query | string | 是 | 助手运行 ID |

### 成功响应
200：

| 字段 | 类型 | 可为 null | 说明 |
|---|---|---|---|
| `run_id`、`document_id` | string | 否 | — |
| `revisions` | string[] | 否 | 这次运行写出的修订号，升序；为空表示没有改过这个场景 |
| `entries` | array | 否 | 每个改动过的物件一条，`environment` / `render` / `meta` 各一条；运行只调整过层级或顺序时（只动了 `objects` 单元）另有一条 `{kind:"unit", unit:"objects"}` |
| `entries[].target` | `{kind:"object", object_id}` \| `{kind:"unit", unit}` | 否 | — |
| `entries[].name` | string | 否 | 当前名称；已删除的取运行之前的名称 |
| `entries[].change` | `"added"` \| `"removed"` \| `"modified"` | 否 | 相对运行之前 |
| `entries[].restore` | `{from_revision, units, expected}` | 是 | 恢复这一处的命令内容（`restore_units` 的参数加上预期版本）；不可恢复时为 null |
| `entries[].blocked` | `{reason, revision, actor_kind}` | 是 | 不可恢复的原因：`changed_by_others`（运行之后别人改过这一处）、`restored_whole`（之后整份恢复过）或 `hierarchy_not_restorable`（运行调整过层级或顺序；按单元恢复不改层级，只能在历史版本列表整份恢复。修订 v10 起助手可以编组和改挂：某条修订改了物件的父级或顺序时，这些物件的条目同样以此原因阻止恢复，否则只恢复局部变换、不恢复父级，物件会跳位；判断按该修订前后快照里的 `parent_id`、`order`，不按单元） |
| `undo_all` | array | 否 | 撤销本轮的命令内容，通常 1 批；只包含可恢复的条目；全部不可恢复时为空数组 |

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 缺少 `run_id`；运行写出的修订超过 500 个；运行第一次写入之后的修订（含别人的）超过 5000 个 | 改用历史版本列表 |
| 404 | `not_found` | 文档或运行不存在、不属于当前账号，或文档不是场景 | 停止 |

### 交互规则
- 某一处的旧值取它在本次运行第一次被写之前那一版。一批 `restore_units` 只能有一个来源修订，这些来源修订之间若没有别人改过，就合成一批；否则按来源修订分批。
- 同一物件的摆放和内容两个单元若来自不同的来源修订，且两者之间有别人改过，这一条不拆成两批，整条标为 `changed_by_others`（`blocked.revision` 为那次修改），留给历史版本列表。
- 返回的 `expected` 是读取时的当前版本；提交后若有人刚好又改了，照常冲突。
- 只读，没有副作用。

### 调用示例
```json
{"run_id": "crun_8c7b6a59", "document_id": "cwdoc_7b1d6c1e-2f0a-4c55-9a0e-3c1f5e2a9b10", "revisions": ["7", "8", "9"],
 "entries": [
   {"target": {"kind": "object", "object_id": "sobj_Z1x2C3v4B5n6M7a8S9d0f"}, "name": "左侧柱 1", "change": "added",
    "restore": {"from_revision": "6", "units": ["object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
                "expected": {"object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "7", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "9", "objects": "7"}},
    "blocked": null},
   {"target": {"kind": "object", "object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ"}, "name": "主机位", "change": "modified",
    "restore": null, "blocked": {"reason": "changed_by_others", "revision": "10", "actor_kind": "photographer"}}],
 "undo_all": [{"from_revision": "6", "units": ["object:sobj_Z1x2C3v4B5n6M7a8S9d0f", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content"],
               "expected": {"object:sobj_Z1x2C3v4B5n6M7a8S9d0f": "7", "object:sobj_Z1x2C3v4B5n6M7a8S9d0f:content": "9", "objects": "7"}}]}
```

### 兼容与变更
首次定义（修订 v6）。修订 v7 补记 C3 实现时确定的第三种不可恢复原因、5000 条历史上限和多来源规则，与已交付的实现一致，接口外形不变。修订 v10（C5）：助手可以编组和改挂，`hierarchy_not_restorable` 扩展到改了父级或顺序的物件条目，接口外形不变。

## api: ADD 导出分镜

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
| `shots` | array，1–32 | 是 | 每台摄像机一项，摄像机不重复；节点按这个顺序逐行排列 |
| `shots[].camera_object_id` | string | 是 | 在 `scene_revision` 里存在且 `kind=camera` |
| `shots[].images` | array，1–3 | 是 | `{pass:"clay"\|"line"\|"lit", upload_id}`，同一镜头内 `pass` 不重复；暂存图片就绪、未消费、类型为图片。全部镜头合计 ≤ 48 张 |

### 成功响应
200：`{change_id, scene_revision, nodes:[{camera_object_id, pass, node_id, content_revision_id, lens}]}`。节点经画布命令层在一次修改里创建，可以在画布上一次撤销。`lens` 与节点 `based_on.lens` 相同（修订 v9）。
- 位置：从场景节点右侧 80 起，每台摄像机一行、每种方式一列（按 `clay`、`line`、`lit` 的顺序），节点宽 300、高按摄像机画幅；整块与同一父级下的已有节点相交时，下移到相交节点下方。
- 标题：摄像机 `label` 非空时用它，否则用物件名，接「 · clay」「 · 线稿」或「 · 材质光照」。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 外形非法、`pass` 或摄像机重复、摄像机不存在于该修订、`scene_revision` 晚于当前修订或不存在 | 修正 |
| 404 | `not_found` | 场景、画布或场景节点不存在，不属于当前账号，或场景节点不指向这个场景 | 停止 |
| 409 | `archived_read_only` | 画布已归档（沿用画布现有错误码） | 恢复画布后重新导出 |
| 409 | `creative_state_conflict` | 场景在回收站 | 丢弃暂存图片 |
| 409 | `creative_upload_unavailable` | 暂存图片不存在、未就绪、已过期、已被消费或不是图片 | 重新上传后用新的 operation id 重试 |
| 413 | `creative_size_limit` | 图片合计超过 48 张，或画布节点数超过上限 | 分次导出 |

### 交互规则
- **原子性**：全部节点一起创建或都不创建。
- **幂等**：沿用公共约定。网络中断、超时或 5xx 时，用同一 operation id 和 `client_created_at` 重试，得到同一组节点；只有服务端明确拒绝（4xx）时才丢弃暂存图片。
- **过时**：后续场景修改不影响已创建的节点；过时提示由节点视图计算（`data: 导出图来源`）。

### 调用示例
```json
{"operation_id": "4d5e6f70-8192-4a3b-9c4d-5e6f708192a3", "client_created_at": "2026-10-25T14:00:00Z",
 "payload": {"canvas_id": "ccv_12ab", "scene_node_id": "cnode_34cd", "scene_revision": "18",
   "shots": [{"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ",
              "images": [{"pass": "clay", "upload_id": "cupl_dd44"}, {"pass": "line", "upload_id": "cupl_ee55"}]}]}}
```
```json
{"change_id": "cchg_56ef", "scene_revision": "18", "nodes": [
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "clay", "node_id": "cnode_78a1", "content_revision_id": "ccrev_a1b2"},
  {"camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ", "pass": "line", "node_id": "cnode_78a2", "content_revision_id": "ccrev_a1b3"}]}
```
```json
{"error": {"code": "validation_failed", "message": "摄像机 sobj_x 在修订 18 中不存在"}}
```

### 兼容与变更
首次定义。图片节点 `config` 新增 `based_on`，节点快照新增可选的 `based_on_state`，都属于兼容扩展；旧读取端忽略这些字段。修订 v8（C4 起草时）：错误码对齐画布与暂存的现有实现（归档为 `archived_read_only`，暂存不可用为 `creative_upload_unavailable`）；合计 48 张上限（画布一次修改最多牵动 100 个对象，每张图占节点和版本两个）；响应增加 `scene_revision`；只服务手动导出，去掉 `job_id`，助手导出改随完成任务提交；`based_on` 增加 `content_revision_id` 和 `fingerprint`；写明位置、标题和重试规则。修订 v9（C4 验收中）：`based_on` 和响应的节点增加镜头快照 `lens`，兼容扩展。

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
