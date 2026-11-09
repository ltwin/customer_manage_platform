<!-- 本 Epic 的目标契约 delta。已批准不代表已实现；项目 .lti/contracts/ 记录已交付事实。
子项 1（scene-timeline-shots）交付以下全部条目。子项 2–6 的条目在各自起草前由 Epic 修订补全（见 design「接口设计」「数据与存储设计」）。 -->

## glossary: ADD 分镜

- 定义：3D 场景时间线上的一个标记，记着帧号和绑定的摄像机；渲染这个分镜，就是按这一帧求值整个场景，再用这台摄像机出图。
- 不是：策划文档里的「镜头」（策划镜头引用分镜，可以引用一个主分镜和几个备选分镜）；也不是摄像机（多个分镜可以绑定同一台摄像机）。
- 边界场景：同一瞬间从两个机位拍，是相邻两帧上的两个分镜，绑定不同的摄像机；主体换了动作，是另一帧上的分镜。一个场景里的帧号不重复。
- 拼写：代码 `marker`，ID 前缀 `smk_`；界面「分镜」；字段 `scene.timeline.markers[]`、`marker_id`。

## glossary: ADD 关键帧通道

- 定义：物件的一个可动画属性在各帧上的关键帧序列。属性有通道时，它在每一帧的值由通道求出；没有通道时取基础值，所有帧共用。
- 不是：物件在某一帧的整份快照；不是形态键（形态键是网格的顶点形变，本项目尚未支持）。
- 边界场景：主体在分镜 2 往前走了一步，是主体 `transform.position` 通道上第 2 帧的关键帧；挪动没有通道的桌子，改的是桌子的基础值，所有分镜里的桌子都变。
- 拼写：代码 `channel`、`keyframe`；字段 `objects[].animation.<path>.keys[]`；界面「关键帧」。

## patterns: ADD 关键帧求值

- 约定：
  - 场景按帧求值只有一套规则，Go（`backend/internal/creativescene` 的求值器）与 TS（`frontend/src/creative-scene` 的求值器）各实现一份，两端跑同一份测试向量：`backend/internal/creativescene/testdata/keyframe_eval.json`，由 `make generate` 复制到前端，`make generate-check` 比对漂移。改规则时先改测试向量。
  - 帧号是非负整数。通道的关键帧按帧号严格递增。
  - 帧在第一个关键帧之前时取第一个关键帧的值，在最后一个关键帧之后时取最后一个的值。
  - 帧落在两个关键帧之间时，按左边关键帧的 `interp` 计算：
    - `constant` 取左值。
    - `linear`：数值和数组逐分量线性插值，t = (帧 − 左帧) / (右帧 − 左帧)；四元数在点积为负时先把右值取反（走最短路径），再做球面线性插值，结果归一化。两个四元数夹角小于 1e-6 弧度时退化为线性插值后归一化。
  - 离散属性（布尔、枚举、字符串和 `aspect`、`sensor_mm`、`focus_object_id`）只允许 `constant`。
  - 求值不改写快照：有通道的属性以求值结果为准，基础值保留但不参与这一帧。
  - 求值结果的使用方：渲染、导出、渲染指纹、助手读取。不要在别处另写插值。
- 为什么：服务端按求值结果算渲染指纹，编辑器按求值结果出图，两边差一点就会误报过时或导出与视口不一致。规则很小，与 manifest 算法一样在两端各实现一份，比在服务端引入 JS 运行时简单。
- 不适用于：贝塞尔插值和曲线编辑（后续加 `interp = "bezier"` 时扩展本条）；蒙皮后的顶点不由求值器计算，求值器只给出骨骼通道的值。
- 例子：子项 1 交付后补充求值器所在的文件和符号。

## data: ADD 场景时间线

### 用途与存储角色
- **用途**：扩展 `data: 场景文档`，记录分镜标记和物件属性的关键帧通道。一个场景因此可以有多个分镜，每个分镜的物件位置、可见性和摄像机参数可以不同，没有通道的属性所有分镜共用。
- **存储**：与场景文档相同，位于快照载荷（`schema_version = 1`）内：`scene.timeline` 在新单元 `timeline` 内；`objects[].animation` 按属性归入物件现有的两个单元（见下「单元」）。不新增表或列。
- **角色**：权威主数据。按帧求值的结果是派生量，不落库（`patterns: 关键帧求值`）。
- **所有权**：只有 `creativescene` 经场景命令写入（`api: ADD 场景时间线命令`），写入者是摄影师（编辑器）或助手（`edit_scene`）。读取方是编辑器、渲染端、渲染指纹和助手工具。

### 结构与身份

`scene.timeline`：

| 路径 | 类型 / 约束 | 必须 | 缺省 / 说明 |
|---|---|---|---|
| `scene.timeline` | object | 否 | 读取没有这个字段的快照时补 `{fps: 24, markers: []}` |
| `timeline.fps` | integer 1…120 | 是 | 24。本 Epic 只用于时间线面板的刻度显示，不影响静态分镜的渲染；以后的动画播放按它换算时间 |
| `timeline.markers[]` | array，≤ 200 | 是 | 按 `frame` 升序存储（服务端写入时排序） |
| `markers[].id` | string，`smk_<21 位 nanoid>` | 是 | 客户端生成，场景内唯一，删除后不复用（同物件 ID 的规则） |
| `markers[].name` | string ≤ 80 字符 | 是 | 可为空串；界面上为空时显示「分镜 N」（按帧序编号） |
| `markers[].frame` | integer 0…100000 | 是 | **场景内不重复** |
| `markers[].camera_object_id` | string，物件 ID 或 `""` | 是 | 写入时非空就必须指向修改后场景里 `kind = camera` 的物件；之后摄像机被删除不算违反契约，这个分镜视为「未绑定摄像机」，不能导出或回看，编辑器和助手工具都会提示 |

`objects[].animation`：

| 路径 | 类型 / 约束 | 必须 | 缺省 / 说明 |
|---|---|---|---|
| `objects[].animation` | object，键为通道路径 | 否 | 缺省 `{}`；没有通道时存储时去掉这个字段 |
| `animation.<path>.keys[]` | array，1…1000 项 | 是 | 帧号严格递增；没有关键帧的通道不存储 |
| `keys[].frame` | integer 0…100000 | 是 | — |
| `keys[].value` | 按路径的类型 | 是 | 约束与对应的基础值字段相同；四元数写入时长度与 1 的差 ≤ 1e-3，存储时归一化 |
| `keys[].interp` | `"linear"` \| `"constant"` | 否 | 连续属性缺省 `linear`，离散属性只允许并缺省 `constant`；存储时补齐 |

可动画的路径（白名单；以后的骨骼通道、灯光和环境属性经 Epic 修订加入）：

| 路径 | 物件 | 值 | 类别 | 所在单元 |
|---|---|---|---|---|
| `transform.position` | 全部 | `[x, y, z]`，同 `transform.position` | 连续 | `object:<id>` |
| `transform.rotation` | 全部 | `[x, y, z, w]` 单位四元数 | 连续（球面插值） | `object:<id>` |
| `transform.scale` | 全部 | `[x, y, z]`，同 `transform.scale` | 连续 | `object:<id>` |
| `visible` | 全部 | boolean | 离散 | `object:<id>` |
| `camera.focal_mm` | `camera` | 同 `objects[].camera` | 连续 | `object:<id>:content` |
| `camera.f_stop`、`camera.focus` | `camera` | 同上 | 连续 | `object:<id>:content` |
| `camera.exposure_compensation`、`camera.iso`、`camera.shutter_s` | `camera` | 同 `data: 摄像机曝光` | 连续 | `object:<id>:content` |
| `camera.sensor_mm`、`camera.aspect`、`camera.depth_of_field`、`camera.focus_object_id`、`camera.exposure_mode` | `camera` | 同上 | 离散 | `object:<id>:content` |

- 路径不在白名单里，或者与物件的 `kind` 不符（例如方块写 `camera.focal_mm`），返回 400 并指出路径。
- `camera.focus_object_id` 的关键帧值按基础值的规则校验：写入时非空就必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己。
- `near`、`far`、`label` 和物件的名称、锁定、父级、顺序、模型、材质槽都不能打关键帧（Epic design 不变量 P4）。

### 分镜帧必有关键帧
- **规则**（Epic design 不变量 P8）：一个属性只要有通道，它在每个分镜的帧上都必须有关键帧。分镜的画面因此只由关键帧决定，不受外推和插值影响；分镜之外的帧可以另有关键帧。
- **检查时机**：服务端在每批命令应用后检查，不满足时整批拒绝（400）。检查范围如下：
  - 本批写到的通道（`add_objects` 带的 `animation`，`update_objects` 的 `animation`）。
  - 本批改了 `timeline.markers` 时，检查全部通道。
  - `restore` 和 `restore_units` 不检查：恢复部分单元后可能出现缺口。
- **缺口的处理**：求值仍按 `patterns: 关键帧求值` 进行，结果有定义。读取方要提示缺口：编辑器在时间线上标出缺口，助手的 `read_scene` 在结果里列出「通道 × 分镜」的缺口。之后对这条通道或分镜列表的写入，要连同补齐缺口一起提交。
- **由调用方在同一批里写全的操作**（Epic design「编辑语义」S1–S6），服务端不隐式补帧：
  - 首次创建通道（在任意一帧）：其他每个分镜的帧写修改前的值，基础值保持修改前的值。
  - 新建分镜：每条通道在新帧写基础值。复制分镜：每条通道在新帧写被复制分镜的求值结果。
  - 改分镜帧号：每条通道的关键帧随分镜挪到新帧。
  - 删除分镜：删掉每条通道在这一帧的关键帧。
  - 添加只属于当前分镜的物件：基础值 `visible = false`，`visible` 通道在当前分镜为 `true`，其他分镜为 `false`。

### 单元
- 新增单元 `timeline`，存 `scene.timeline` 的全部内容。
- 通道不新增单元，归入它的属性原来所在的单元：
  - 摆放类路径（`transform.*`、`visible`）在 `object:<id>`。
  - 摄像机路径在 `object:<id>:content`。
  - 改通道与改同一属性的基础值按同一单元做并发控制。
  - 这样每个物件仍然只占两个单元，单元总数（上限 4096）只多一个 `timeline`。

### 客户端任务的分镜字段
`data: 场景客户端任务` 的现有请求外形不变，增加以下可选字段。本节定义它们，那一节的修改由 `creative-mcp-server` 的子项负责，两边不互相改写正文：

- `render_view`：
  - 新增第四种选择方式 `{marker_id}`：按这个分镜的帧求值，用它绑定的摄像机渲染，画幅取这台摄像机在这一帧的 `aspect`。
  - 分镜不存在或未绑定摄像机时，工具直接返回错误、不建任务。
  - `depth_of_field = true` 可以与 `marker_id` 一起用，条件是这台摄像机在这一帧求值后 `depth_of_field` 为 `true`。
  - 其他三种方式（`camera_object_id`、`viewpoint`、`preset`）增加可选的 `frame`（integer 0…100000，缺省 0），按这一帧求值。
- `export_shots`：
  - 新增 `marker_ids`（数组，不重复，与 `camera_object_ids` 二选一）。
  - 工具没指定时：场景有分镜就取全部分镜（按帧序），没有分镜就照旧取全部摄像机。
  - 有分镜未绑定摄像机时，工具直接返回错误、不建任务，并列出这些分镜。
  - 结果的 `nodes[]` 增加 `marker_id`（按摄像机导出时为 null）。

### 访问模式与保证
- **写**：按 `api: ADD 场景时间线命令` 的规则在文档行锁内校验并应用，与其他场景命令同一事务。由应用保证：
  - 标记 ID 唯一、帧号不重复、摄像机引用合法。
  - 通道路径在白名单内并与 `kind` 相符。
  - 关键帧帧号严格递增，值合法。
  - 分镜帧必有关键帧（检查范围见上）。
  - 全场景关键帧总数 ≤ 20000（初始值，子项 1 实测后调整），快照仍 ≤ 512 KiB。
- **读**：读取场景时随快照原样返回；服务端解码时补齐 `timeline` 的缺省值和 `interp`。
- **求值**：按 `patterns: 关键帧求值`。渲染指纹、导出放置和助手读取在服务端求值；编辑器和渲染端在浏览器里求值。
- **删除**：删除物件时，它的通道随物件一起删除；被删的摄像机仍可能被分镜引用（见上）。
- **恢复**：`restore` 和 `restore_units` 按单元恢复：`timeline` 单元恢复分镜标记；物件单元恢复时，这个单元里的通道一起恢复。

### 生命周期与演进
- 不迁移：没有 `timeline` 和 `animation` 就是没有分镜、没有通道，求值结果就是现在的快照，画面和渲染指纹都不变。
- 前后端同一版本上线：旧编辑器不认识这两个字段，`update_objects` 整体替换不会丢失它们（字段不在旧编辑器写的键里），但旧编辑器会按基础值显示有通道的物件，与新编辑器不一致。回滚到不认识它们的后端会读取失败，只做前向修复（同修订 v17）。
- 后续：
  - `interp = "bezier"`（动画播放和曲线编辑时加）。
  - 骨骼通道 `pose.*`（本 Epic 子项 4）。
  - 灯光、天空、雾的通道。
  - 密集动画拆出快照存储。

## api: ADD 场景时间线命令

### 用途与调用
- **用途**：编辑分镜标记和物件的关键帧通道。
- **消费者**：前端场景编辑器（outbox）；助手工具 `edit_scene` 经同一领域命令，不经 HTTP。
- **调用**：与 `api: 提交场景命令` 同一端点和外层（`POST /api/v1/creative/documents/{id}/commands`，`Idempotency-Key` 必填）。本条目新增一个操作 `set_timeline`，并给 `add_objects`、`update_objects` 增加 `animation` 字段。外壳规则不变：整批原子、按单元乐观并发、每个单元在一批里最多出现一次。
- **权限**：同 `api: 提交场景命令`。

### 请求

新增操作：

| op | 字段 | 涉及单元 |
|---|---|---|
| `set_timeline` | `timeline`：按键合并。`fps` 可省略；`markers` 可省略，写了就整体替换整个分镜列表（元素外形见 `data: ADD 场景时间线`） | `timeline` |

现有操作的新字段：

| op | 新字段 | 规则 | 涉及单元 |
|---|---|---|---|
| `add_objects` | `objects[].animation` | 完整的通道对象，外形与校验见数据契约。用于复制带动画的物件、撤销删除 | 同 `add_objects` |
| `update_objects` | `updates[].animation` | `{clear?: [path], delete?: [{path, frame}], set?: [{path, frame, value, interp?}]}`，至少一项。按 `clear` → `delete` → `set` 的顺序应用：`clear` 删掉整条通道；`delete` 删掉这一帧的关键帧，帧上没有关键帧时忽略；`set` 按帧覆盖或插入。通道删空后不再存储。同一项里可以同时改基础值，例如删掉最后一个关键帧时，把基础值写成删除时这一帧的求值结果 | 每条路径按数据契约的「所在单元」：摆放类路径涉及 `object:<id>`，摄像机路径涉及 `object:<id>:content`；与同一项里的基础值改动合并计算 |

- 首次创建通道、新建分镜、复制分镜、改分镜帧号、删除分镜、添加只属于当前分镜的物件，都由编辑器和助手的工具层在同一批里写全（`data: ADD 场景时间线`「分镜帧必有关键帧」），例如 `set_timeline` 加上 `update_objects` 的 `animation`。服务端不隐式补帧，只检查结果。
- 一批里 `set_timeline` 最多一个（`timeline` 单元只能出现一次）。

### 成功响应
与 `api: 提交场景命令` 相同：回执 `revision`、`changed_units`、`annotated_units`、`removed_units`、`reset_required`。不增加字段。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 有通道的属性在某个分镜帧上缺少关键帧（`details.missing` 列出 `[{object_id, path, marker_id, frame}]`，最多 50 项）；标记 ID 重复或格式不对、帧号重复或越界、`camera_object_id` 不指向摄像机；通道路径不在白名单或与物件 `kind` 不符；关键帧帧号不递增、值越界、四元数长度偏差过大、离散属性写了 `linear`；`focus_object_id` 的关键帧值不可对焦；`expected` 缺少新字段涉及的单元。`details.field` 指出字段路径，例如 `operations[0].updates[2].animation.set[1].value` | 修正，不可原样重试 |
| 404 | `not_found` | 同 `api: 提交场景命令` | 同左 |
| 409 | `creative_document_conflict` | 任一单元的 `rev` 与 `expected` 不符，`details.conflicts` 同现有规则 | 重读后重新应用 |
| 409 | `creative_state_conflict` | 场景在回收站 | 先恢复场景 |
| 413 | `creative_size_limit` | 分镜超过 200 个、单个通道超过 1000 个关键帧、全场景关键帧超过 20000 个，或应用后快照 > 512 KiB；`details.limit` 给出触发的上限 | 减少分镜或关键帧 |

### 交互规则
- **原子性、幂等、并发**：同 `api: 提交场景命令`。
- 不适用：分页、异步、限流（沿用全局请求体上限）。

### 调用示例

场景已有分镜「回头」（第 0 帧）。新建分镜「拔剑」（第 10 帧），并在这一镜把主体往前挪：主体的位置原来没有通道，所以按首次创建通道的规则，第 0 帧写修改前的位置（即基础值 `[0, 0, -1.2]`），第 10 帧写新位置：

```json
{"operation_id": "9b1c2d3e-4f50-4a61-8b72-c3d4e5f60718", "client_created_at": "2026-11-10T09:00:00Z",
 "payload": {
   "expected": {"timeline": "3", "object:sobj_Body7xQ2mN9pL4tR6vW8yZ": "5"},
   "operations": [
     {"op": "set_timeline", "timeline": {"markers": [
       {"id": "smk_A1b2C3d4E5f6G7h8I9j0K", "name": "回头", "frame": 0, "camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ"},
       {"id": "smk_L1m2N3o4P5q6R7s8T9u0V", "name": "拔剑", "frame": 10, "camera_object_id": "sobj_3kTMd9Qp2xLw7nB0cR5yZ"}]}},
     {"op": "update_objects", "updates": [
       {"object_id": "sobj_Body7xQ2mN9pL4tR6vW8yZ",
        "animation": {"set": [{"path": "transform.position", "frame": 0, "value": [0, 0, -1.2]},
                              {"path": "transform.position", "frame": 10, "value": [0.4, 0, -1.2]}]}}]}]}}
```

```json
{"revision": "42", "changed_units": ["timeline", "object:sobj_Body7xQ2mN9pL4tR6vW8yZ"], "annotated_units": [], "removed_units": [], "reset_required": false}
```

```json
{"error": {"code": "validation_failed", "message": "transform.position 在分镜「回头」（第 0 帧）上缺少关键帧",
  "details": {"missing": [{"object_id": "sobj_Body7xQ2mN9pL4tR6vW8yZ", "path": "transform.position", "marker_id": "smk_A1b2C3d4E5f6G7h8I9j0K", "frame": 0}]}}}
```

### 兼容与变更
- 只增加操作和可选字段，旧客户端不会发送它们。
- `update_objects` 的 `animation` 是增量写法，不整体替换。旧编辑器改基础值时不会碰到已有的通道。
- 回滚到不认识这些操作的后端会返回 400，前端同一版本上线。

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
| `shots` | array，1–32 | 是 | 每个分镜（或每台摄像机）一项；节点按这个顺序逐行排列 |
| `shots[].marker_id` | string | 与 `camera_object_id` 二选一 | 在 `scene_revision` 的 `timeline.markers` 里存在，且绑定的摄像机在该修订里存在、`kind=camera`。分镜不重复。渲染端按这个分镜的帧求值、用它的摄像机出图 |
| `shots[].camera_object_id` | string | 与 `marker_id` 二选一 | 在 `scene_revision` 里存在且 `kind=camera`；按第 0 帧求值。摄像机不重复。只用于没有分镜的场景和旧客户端 |
| `shots[].images` | array，1–3 | 是 | `{pass:"clay"\|"line"\|"lit", upload_id}`，同一镜头内 `pass` 不重复；暂存图片就绪、未消费、类型为图片。全部镜头合计 ≤ 48 张 |

一次请求里的 `shots` 只能全部用 `marker_id`，或全部用 `camera_object_id`。

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
| 409 | `creative_upload_unavailable` | 暂存图片不存在、未就绪、已过期、已被消费或不是图片 | 重新上传后用新的 operation id 重试 |
| 413 | `creative_size_limit` | 图片合计超过 48 张，或画布节点数超过上限 | 分次导出 |

### 交互规则
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

## data: MODIFY 导出图来源

- **用途**：记录画布图片节点由哪个场景、哪个修订、哪个分镜（或哪台摄像机）、哪种导出方式生成，用于提示过时。
- **存储**：`creative_nodes.config` 内的 `based_on` 对象，只对导出产生的 `core.image` 节点写入：`{scene_document_id, scene_revision: CreativeRevision, marker_id, frame, camera_object_id, pass:"clay"|"line"|"lit", content_revision_id, fingerprint, lens}`。
  - `marker_id`、`frame`（`scene-pose-system` 子项 1）：按分镜导出时为分镜 ID 和导出时它的帧号；按摄像机导出时 `marker_id` 为 null、`frame` 为 0。旧节点没有这两个字段，按 null 和 0 读取。
  - `camera_object_id` 是渲染所用的摄像机（按分镜导出时取导出时分镜绑定的摄像机）。
  - `content_revision_id` 是导出时放进节点的那份图片内容修订。
  - `fingerprint` 是 `scene_revision` 的快照按 `frame` 求值后，对这台摄像机、这种导出方式的渲染指纹（带版本前缀，如 `v1:`），客户端只当不透明字符串。
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

## architecture: MODIFY 场景模块

### 职责与边界
- 依赖通用文档底座（`creative-document-foundation`）：场景以文档类型端口注册为 `scene` 类型，读取、命令、回收站和历史都走通用入口。
- `backend/internal/creativescene`：
  - 场景快照的 schema 校验、单元版本与命令应用、恢复。
  - 模型提交（消费暂存上传，写 `model` 内容）。
  - 客户端任务生命周期、导出编排。
  - 场景助手工具的领域入口。
  - 场景引用图片的校验与持有登记（修订 v17）：快照里出现的图片修订校验账号、种类和可展示性，登记进引用表；收集入口集中在一个函数里，后续物件类型（`cg-composite` 的人片卡片、原片背景）在这里加入口。
  - 材质资产的存入（修订 v17）：写 `material` 修订、持有关系和资产，复制语义同模型资产。
  - 分镜标记与关键帧通道的校验和命令应用（`scene-pose-system` 子项 1）。
  - 按帧求值（`patterns: 关键帧求值`）：无 I/O 的纯函数，供渲染指纹、导出放置和助手读取使用。服务端只算属性值，不做蒙皮、不渲染。
- 不职责：执行几何代码、渲染、画布节点的布局规则（调用画布命令层完成）、读取或处理图片像素（修订 v17）。
- 依赖方向：`creativeagent` → `creativescene` → `creativecontent` / `creativestaging` / `creativecanvas`（命令层）/ 通用文档层 / `platform/store` / `platform/realtime`。`creativescene` 不 import gin（ADR-003），`gin.Context` 不下穿。客户端任务到达终态时，`creativescene` 经自己定义的端口通知 `creativeagent`（进程启动时注册实现），不反向 import。画布读取导出图节点时，`creativecanvas` 经自己定义的端口问 `creativescene` 这张图是否过时（进程启动时注册实现），`creativecanvas` 不 import `creativescene`。
- 前端 `frontend/src/creative-scene/`：
  - 编辑器外壳（R3F）、场景状态（快照 + outbox + 撤销栈）、沙箱宿主、渲染端（客户端任务执行器）、材质库。
  - 页面级图片缓存（按媒体票据读原图、各 GPU 设备共用纹理对象、离屏出图前等图片就绪，修订 v17）。
  - 导出器。
  - 时间线面板与按帧求值的 TS 实现（`scene-pose-system` 子项 1）：视口、渲染端和导出都只消费求值后的场景；「已改未记录」等关键帧编辑状态只存在于编辑器，不写入快照。
  - 编辑视图嵌入画布的同一个助手面板。
  - 渲染端不依赖编辑器外壳：画布页在认领到任务时按需加载它，摄影师不打开场景也能让助手搭建；没有任务的画布页不加载 3D 引擎。
- 渲染、烘焙、截图、导出只在浏览器完成；服务端不渲染。服务端经客户端任务派发工作，经暂存上传接收产物。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：
  - 渲染端按分镜（或摄像机）对快照求值、出图并暂存上传。
  - 摄影师手动导出时调 `POST /creative/documents/{id}/scene-exports`；助手导出（`export_shots`）把图片清单随完成任务提交，在完成事务里放置。
  - 两条路径走同一个放置流程：服务端对该修订按同一帧求值、算渲染指纹，画布命令层建图片节点，写入 `based_on`。
  - 读画布时经上述端口，对当前快照按同一分镜求值，计算导出图节点的过时状态。
