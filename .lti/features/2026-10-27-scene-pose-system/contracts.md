<!-- 本 Epic 的目标契约 delta。已批准不代表已实现；项目 .lti/contracts/ 记录已交付事实。
子项 1（scene-timeline-shots）交付「data: MODIFY 导出图来源」「architecture: MODIFY 场景模块」及之前的条目；子项 2（scene-glb-import）交付「data: MODIFY 场景文档」及之后的四条。子项 3–6 的条目在各自起草前由 Epic 修订补全（见 design「接口设计」「数据与存储设计」）。 -->

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

## patterns: MODIFY 几何模块契约

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
- **产物**：沙箱返回 GLB（经 `GLTFExporter` 二进制导出）和规范化的模块信息 `{contract_version, lib_version, mount, params_schema, params}`（`params` 为补齐默认值后实际使用的值）。manifest 不由沙箱提供，由宿主和服务端从 GLB 算出：`{bounds:{min:[x,y,z],max:[x,y,z]}, triangles, slots:[{id,hint}], parts:[id], anchors:[{id,position:[x,y,z]}], uv}`。`uv`（修订 v18）为 true 表示每个三角网格都带 `TEXCOORD_0`，贴图的 `uv` 映射才能整体按模型 UV 显示。GLB 是几何事实源，只有代码、参数或 `lib_version` 变化时才重新编译。
- **上限**：代码 ≤ 64 KiB；编译 ≤ 5 秒；三角形 ≤ 2000000；GLB ≤ 200 MiB（scene-pose-system 修订 v5，与 `data: 模型内容` 的静态子集预算相同，导入的模型共用；贴图不在 GLB 里，不计入）。
- **为什么**：只用 primitive 的 JSON 表达力不够；整个场景是一段代码则无法支持手动编辑、点选修改和撤销。以物件为粒度写代码，既有建模自由度，又让场景编排可编辑。先例见 research §3（Pascal authored objects）。
- **不适用于**：内置基础体是前端打包的可信模块，遵守同一契约，但不经沙箱、不烘焙。外部 GLB 导入（scene-pose-system 子项 2）不经过本契约，只与它共用上面的三角形和 GLB 上限。
- **例子**：C2 交付后补充内置基础体 `box@1` 的实现路径作为参照。

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
| `objects[].model` | `{content_revision_id}` | `kind=model_instance` 时 | 必须是本账号 `model` 类型、`ready` 且可展示的内容修订 |
| `objects[].material_slots` | `{<slot_id>:{preset, params, maps?, mapping?, alpha_mode?, alpha_cutoff?}}` | 否 | 贴图与透明方式见下方「材质贴图」（修订 v17）。缺省 `{}`；`preset` 匹配 `^[a-z0-9_]+@[1-9][0-9]*$`；`params` 是平铺对象，≤ 32 键，值为 number/string(≤80)/boolean。键对应物件的有效槽位（修订 v13）：基础体为 `main` 加 `primitive.faces` 分出的槽位，模型实例为模型的有效槽位（`data: 模型内容`）。服务端不按槽位清单校验键（与 C5 相同），不在有效槽位里的键照常保存、渲染时不用 |
| `objects[].light` | 按 `type` 区分，字段与 three.js 灯光类同名同义，见下方「灯光」 | `kind=light` 时 | 只有 `type` 必填，其余缺省取 three.js 默认值，存储时补齐 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near>0, far>near（米，同 three.js `PerspectiveCamera`）, label≤40, depth_of_field:bool, f_stop:0.5…64, focus>0（米，同 three.js `PerspectiveCamera.focus`）, focus_object_id:物件 ID 或 ""}` | `kind=camera` 时 | 各字段可省略，缺省 `{35,[36,24],"3:2",0.1,1000,"",false,2.8,10,""}`，存储时补齐；景深四个字段为修订 v8 新增，旧快照在读取时补齐。朝局部 −Z 看（three.js 摄像机约定）；视场角按画面长边对应传感器长边算出：`tan(fov/2) = max(sensor_mm) / (2·focal_mm) / max(画幅宽高比, 1)`，`fov` 为 three.js 的垂直视场角（修订 v8），不写 three.js 的 `fov`。`focus_object_id` 非空时按该物件原点沿视线方向的深度对焦：写入时必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己；之后目标被删除不算违反契约，按 `focus` 对焦。写空字符串取消对焦物件（按键合并下 `null` 等于没写）。`depth_of_field = true` 时，白模导出按光圈和对焦虚化（design「渲染模式与导出」） |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型和图片 `content_revision_id`（图片自修订 v17）。
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
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、生成参数、名称提示，不含代码和地址）和图片修订摘要（宽高、MIME、是否可用，修订 v17）。图片像素由编辑器按修订另取媒体读取票据（`role = original`），不经场景接口。GLB 和缩略图由编辑器按模型修订另取媒体读取票据（C2 修改）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型和图片可用、单元版本单调。图片的可用性只在它第一次出现在场景里时检查，已被当前修订引用的图片批量复核，与模型相同（修订 v17）。
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

## data: MODIFY 模型内容

### 用途与存储角色
- **用途**：可复用的几何物件，由场景实例和资产库资产共同引用。
- **存储**：
  - `creative_contents.kind = 'model'`。
  - 每个版本一条 `creative_content_revisions`（`schema_version = 1`）。
  - GLB 存为 blob，经 `creative_content_objects` 以 `role='original'` 绑定；缩略图 PNG 以 `role='thumbnail'` 绑定。
  - 上传走 `creative_uploads`，`declared_kind` 加 `model`，校验格式加 `model/gltf-binary`（`.glb`），上限 200 MiB（修订 v5），只允许暂存目标。
  - **两种形态**：
    - 暂存形态：worker 校验 GLB 通过后写入，载荷只有 `{format, manifest}`，manifest 由服务端从 GLB 算出；由暂存候选持有，只供 `commit_model` 消费，不能被场景引用，也不能被采用到资产或画布节点。候选被消费后（`adopted_target_kind = 'document'`）失去持有方，按 GC 回收。
    - 完整形态：`commit_model` 写入，载荷为下表结构；GLB 与缩略图对象复用暂存修订的 blob，不复制字节。`derive_model`（修订 v13）也写完整形态：载荷来自来源修订，只换 `faces`，GLB 与缩略图复用来源修订的 blob。导入接口（`api: 导入模型到资产库`，scene-pose-system 子项 2）也写完整形态：`generator` 为导入形式，另带 `import` 和 `materials`，见下方「导入的模型」；GLB 复用暂存修订的 blob。
  - **写入口**：`model` 不能经通用写入口（`creativecontent.Validate` / `WriteAndRetain`）写入，资产、画布 `replace_content`、生成输出都写不进来。暂存形态只由 `WriteMediaAndRetain`（worker）写，完整形态只由 `WriteModelAndRetain`（`commit_model`、`derive_model`、导入接口）写。
- **角色**：权威主数据，内容修订不可变。
- **所有权**：由 `creativescene` 的 `commit_model` 写入（消费暂存上传）；导入的模型由 `creativescene` 的导入接口写入，只属于资产库（`data: 模型资产`）；`creativelibrary` 存入资产库时只引用同一修订，不复制字节（复制语义由「不可变 + 编辑时分叉」实现，见 I4、I7）。

### 结构与身份

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `model.format` | `"glb"` | 是 | — |
| `model.generator.language` | `"three-module"` \| `"imported"` | 是 | `imported` 表示外部导入的 GLB（scene-pose-system 子项 2），只由导入接口写入；此时 `generator` 只有 `language` 和 `mount`，下面标「`three-module` 时」的字段都没有 |
| `model.generator.contract_version` | integer | `three-module` 时 | 当前为 1 |
| `model.generator.lib_version` | integer | `three-module` 时 | 当前为 1 |
| `model.generator.code` | string | `three-module` 时 | ≤ 64 KiB，UTF-8 |
| `model.generator.code_sha256` | `sha256-<64 hex>` | `three-module` 时 | 服务端重算并核对 |
| `model.generator.params_schema` | object | `three-module` 时 | 按几何模块契约的参数定义 |
| `model.generator.params` | object | `three-module` 时 | 每个键都在 schema 中，值在范围内 |
| `model.generator.mount` | `floor`/`wall`/`ceiling`/`free` | 是 | 导入的模型由导入时选择，缺省 `floor` |
| `model.manifest` | 见几何模块契约「产物」 | 是 | `triangles` ≤ 2000000（修订 v5）；`slots` ≤ 32；`parts` ≤ 200 |
| `model.name_hint` | string ≤ 80 | 否 | 助手或摄影师起的名字 |
| `model.faces` | object | 否 | 面级材质分配（C5b，修订 v13），见下方「面级材质分配」；缺省表示没有 |
| `model.import` | object | `imported` 时 | 导入来源与授权说明，见下方「导入的模型」 |

**面级材质分配**（修订 v13，owner 2026-10-28）：

- **含义**：把模型的一部分三角形分到别的槽位。与 Blender 相同，「一个面属于哪个槽位」是网格数据，记在模型修订上；「槽位用什么材质」仍记在实例的 `material_slots`。GLB 不改写：渲染端加载 GLB 后按这份分配把三角形拆到对应槽位，所有带材质的渲染入口（编辑器的材质预览与最终渲染、`render_view` 的 `lit`、`lit` 导出、资产库的材质缩略图与查看浮窗）经同一套套用逻辑。白模、线稿和线框不区分槽位，不受影响；模型修订自己的白模缩略图因此可以复用。
- **结构**：

```json
{"meshes": [{"path": [["part:capital", 0], ["", 2]], "primitive": 0, "topology": "t1-9f86d081884c7d65",
             "assign": {"brick": [[0, 12], [40, 44]], "stone": [[12, 16]]}}],
 "hints": {"brick": "brick"}}
```

| 路径 | 类型 | 约束 |
|---|---|---|
| `meshes[].path` | `[[name, n], …]`，1–64 段 | 网格身份：从场景根节点到网格所在节点的路径。每段为节点名（GLB JSON 原文，可以为空串，≤ 64 字符）和它在同级节点里同名者中的序号（从 0 开始，按 `children` / `scene.nodes` 的顺序）。只与树结构和名字有关，树的其他地方增删节点不影响它。同一 `path` + `primitive` 只出现一次 |
| `meshes[].primitive` | integer 0…255 | 节点所引用网格里的 primitive 下标 |
| `meshes[].topology` | string，`^t1-[0-9a-f]{16}$` | 写入时这个网格的拓扑指纹，见下 |
| `meshes[].assign` | `{<slot_id>: [[start, end], …]}`，至少一项 | 三角形区间，左闭右开，按这个 primitive 自己的三角形顺序；每个槽位的区间升序，同一网格内所有区间互不重叠；`end > start`。没被分配的三角形留在 GLB 原来的槽位。`slot_id` 规则同 GLB 材质名 `slot:<id>`（`^[A-Za-z0-9_-]{1,64}$`），可以是 GLB 已有的槽位 |
| `hints` | `{<slot_id>: string ≤ 40}` | 可选；面分配新建的槽位的语义提示，作用同材质的 `extras.hint` |

- **拓扑指纹 `t1`**：描述三角形怎样连接，不含坐标，所以只改尺寸的重新生成通常保持不变。先得到每个三角形角点的「连接编号」：有索引的网格直接用索引；没有索引的网格按位置焊接，坐标完全相等（float32 逐位相等）的顶点取同一编号，编号按首次出现的顺序分配。再对 `[顶点数, 三角形数, 连接编号序列]`（uint32 小端）算 SHA-256，取前 16 位十六进制。在变换烘进几何、镜像翻转三角形之前计算。连接方式变化、一块连通网格里的三角形重排会改变指纹；坐标变化不会。互不相连的几块交换顺序时指纹可能不变：相同的部件对调后索引相同，没有索引时分开的三角形对调后编号相同（修订 v14），重新生成时按下表处理。
- **有效槽位**：manifest 的 `slots` 加上 `faces` 里出现的槽位，后者的提示取 `hints`，没有则为空；合计 ≤ 32 个。场景物件的 `material_slots`、资产库的 `materials`、编辑器材质面板和助手看到的槽位都按有效槽位。某个槽位的三角形被全部分走后，它仍是有效槽位。
- **上限**：区间合计 ≤ 8192，`meshes` ≤ 2000，`faces` 序列化后 ≤ 128 KiB。超出时拒绝，提示在模型代码里用 `lib.material` 划分槽位。
- **校验分工**：服务端只校验外形和上限，不读 GLB。路径是否存在、区间是否落在网格内、指纹是否相符，由渲染端在加载时核对：不符的网格不套用分配，按 GLB 原来的槽位显示，编辑器在这个物件上提示。分配只影响本账号这个模型的外观，不涉及安全。
- **重新生成时**（改参数、改代码、助手 `write_model`）：执行烘焙的渲染端按新 GLB 逐项判断，结果随 `commit_model` 的 `faces` 写入（owner 2026-10-28：按网格保留，不按位置迁移）：

  | 新 GLB 里的情况 | 结果 |
  |---|---|
  | 没有这个 `path` + `primitive`，或指纹不同 | 作废 |
  | 存在且指纹相同，同级没有同名、同 primitive 下标、同指纹的网格 | 保留 |
  | 存在且指纹相同，但同级有同名、同指纹的网格（例如阵列出来的一排未命名柱子），代码与 `lib_version` 未变、只改了参数 | 保留：代码相同时生成顺序只由参数决定，参数改变节点顺序的写法罕见，owner 2026-10-28 接受这一残余风险 |
  | 同上，但代码或 `lib_version` 变了 | 作废，提示「代码改动后无法确认对应关系」：交换两个相同网格的创建顺序这类改动无法从 GLB 里分辨 |
  | 存在且指纹相同，但网格按位置焊接（坐标完全相等）后分成互不相连的几块，代码或 `lib_version` 变了 | 作废，同上提示：几块相同的部件对调后指纹不变，分配会落到另一块上（修订 v14）。只改了参数时保留，残余风险同上 |
  | 按上面各行保留的分配，加上新 GLB 自己的槽位，有效槽位会超过 32 个 | 按条目顺序累计，使合计超过 32 的那条作废（修订 v14）；否则 `commit_model` 会因有效槽位超限被拒 |

  带过来的 `hints` 只保留仍被用到的槽位（修订 v14）。残余风险：一块连通网格里，局部结构相同的两段顶点和索引整段对调，指纹不变，仍认不出（owner 2026-10-29 接受）。

  作废的部分在编辑器里提示；助手从 `write_model` 的工具结果得知（工具层比较新旧修订的分配）。系统说明提示助手：要让面分配跨代码修改保留，给相关物件起 `part:<id>` 这类唯一的名字。
- **写入口**：`commit_model` 的 `faces`（重新生成时带过来），`derive_model`（只改分配，复用 GLB），见 `api: 场景模型命令`。库副本照抄来源修订的 `faces`（`data: 模型资产`）。

**导入的模型**（scene-pose-system 子项 2，owner 2026-11-09）：

- **含义**：账号从外部导入的静态 GLB。浏览器把原文件换算成静态子集（拆出图片、材质换成槽位、解压几何），只有换算后的 GLB 进存储；原文件不保存。几何没有代码和参数，改不了形状，可以换材质、做面级材质分配。
- **结构**：`generator = {language: "imported", mount}`；`import` 如下；`materials` 是导入时由 glTF 材质换算出的分配，结构同 `data: 模型资产` 的库副本；没有 `source_revision_id`。

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `model.import.file_name` | string 1–255 | 是 | 原文件名（含扩展名），按 Unicode 码点计，不含路径 |
| `model.import.attribution` | string ≤ 2000 | 否 | 来源与授权说明，摄影师填写；导入时用 glTF 的 `asset.copyright` 预填。缺省表示没有填写 |
| `model.import.source_url` | string ≤ 2048 | 否 | 素材的来源页面，只允许 `http`、`https` |

- 导入时间取内容修订的 `created_at`，不另存。服务端不核实授权说明的真假，只保存和显示；授权由摄影师在上传时声明，与其他上传相同。
- **场景里派生**：导入的模型放进场景后做面级材质分配时，`derive_model` 照常从它派生出场景自己的修订：载荷照抄 `generator`、`import`、`manifest`、`name_hint`，换 `faces`，不带 `materials`。派生出的修订仍是 `imported`。`commit_model` 的 `generator` 只接受 `three-module`，写不出导入的模型。
- **读取方**：
  - 编辑器：属性栏不显示参数和代码，显示「导入的模型」、原文件名、来源与授权说明；不能改参数、不能改代码。
  - 助手：`write_model` 以导入的模型为基础时拒绝，提示「导入的模型没有代码，只能换材质或摆放」；以它为基础的烘焙任务同样拒绝。`read_scene` 的 `include_code` 对它不返回代码。
  - 场景读取照常不带代码；导入的模型本来就没有。

### 访问模式与保证
- **接受的 GLB 子集**（worker 的 verify 阶段用 Go 实现，编辑器宿主用同一算法预检；不执行代码，文件里的任何声明都不直接采信；不在子集内的一律拒绝）：
  - 容器：magic 为 `glTF`，version 为 2，总长度与实际一致；JSON 块加至多一个 BIN 块，4 字节对齐。
  - 缓冲：至多一个 `buffer`，没有 `uri`。
  - 场景树：恰好一个 `scene`；节点下标全部有效，每个节点至多一个父节点，无环；变换只用 TRS 或 `matrix` 之一，数值有限。
  - 网格：primitive 的 `mode` 为 4；属性只允许 `POSITION`（`FLOAT VEC3`，必有）、`NORMAL`、`TANGENT`、`TEXCOORD_0/1`、`COLOR_0`；不允许 `targets`、`JOINTS_*`、`WEIGHTS_*`。
  - 访问器：不用 `sparse`；按组件对齐；`byteStride` ≥ 元素大小；全部数据落在 `bufferView` 和 BIN 内；索引值小于顶点数；索引数或非索引顶点数是 3 的倍数。
  - 不允许 `images`、`textures`、`samplers`、`skins`、`animations`、`cameras`；`extensionsRequired` 为空，`extensionsUsed` 只允许 `KHR_materials_emissive_strength`。
  - 预算：节点 ≤ 10000，遍历到的 primitive 实例 ≤ 10000，三角形 ≤ 2000000，字节 ≤ 200 MiB（修订 v5）；读取的 POSITION 顶点总数（每个访问器计一次）≤ BIN 块字节数 / 12。访问器可以指向同一段数据，少了这一项，各项计数都在预算内的文件也能让校验读上数十亿个顶点；访问器互不重叠的文件不会超出。
  - 不合规时上传以 `failed` 结束，失败码沿用现有的 `media_unsupported`。
- **manifest 的算法**：三角形数按场景树遍历累加；每个 primitive 的局部范围**从 POSITION 实际数值算出**，并与访问器声明的 `min` / `max` 逐分量核对（必须存在且相等，否则拒绝）；包围盒取局部范围的 8 个角点经节点世界矩阵变换后合并（与 three.js `Box3.setFromObject` 非精确模式一致）；世界矩阵、包围盒与挂点坐标必须是有限值，各节点数值有限但相乘溢出时拒绝；槽位取材质名 `slot:<id>` 与 `extras.hint`，部件取节点名 `part:<id>`，挂点取节点名 `anchor:<id>` 及其世界坐标，名称取 JSON 原文；槽位 ≤ 32、部件 ≤ 200、挂点 ≤ 200；`uv` 为遍历到的每个 primitive 都有 `TEXCOORD_0` 时取 true，否则 false（修订 v18）。
- **提交时**：manifest 必须与暂存形态中服务端从 GLB 算出的一致，不一致则拒绝。比对规则：`slots`、`parts`、`anchors` 先按 `id` 排序；`id`、`hint`、`triangles`、`uv` 精确相等（暂存形态在修订 v18 之前算出、没有 `uv` 时不比对 `uv`）；`bounds` 与挂点坐标容差 1e-4 m。名称取 GLB JSON 原文（three.js 加载后会删掉节点名里的 `:`）。写入完整形态时使用服务端的值。
- **读取**：GLB 和缩略图走现有媒体读取票据（`IssueTicket` / `Open`），角色分别为 `original` 和 `thumbnail`（票据角色加 `thumbnail`）。

### 生命周期与演进
- 持有方：场景修订引用表、资产、画布节点（后续若有）。三者均登记进 `creativereference.LiveOwnerCondition` 和守卫测试。没有持有方的修订按媒体 GC 规则回收。媒体 GC 的类型范围（`creativemedia/gc.go` 的 `unheldMediaRevision`）目前只含 image / video / audio，C2 必须把 `model` 加进去并登记守卫测试；上线时先升级 creative-worker，再开放模型写入。
- scene-pose-system 子项 2（外部 GLB 导入，owner 2026-11-09）：`generator.language` 加 `imported`，`generator` 的代码与参数字段改为只在 `three-module` 时必有，新增 `model.import`。不迁移：已有修订都是 `three-module`。GLB 静态子集不变，导入时由浏览器换算进子集。修订 v5 把静态子集的预算放宽到三角形 2000000、GLB 200 MiB，所有模型共用：不迁移，已有修订都在旧预算内；OpenAPI 里 manifest 的 `triangles`、面级材质分配区间端点的最大值改为 2000000，上传 `kind: model` 的说明改为 200 MiB；回滚到旧后端时，超过旧预算的模型上传会校验失败，已写入的修订照常读取。OpenAPI 的 `CreativeModelGenerator` 改为按 `language` 区分的两种结构（`three-module` 原结构；`imported` 只有 `language`、`mount`），`CreativeModelPayload` 与 `CreativeModelPreviewPayload` 加可选的 `import`；`commit_model` 用的 `CreativeModelGeneratorInput` 不变。旧编辑器读到没有代码的 `generator` 会出错，前后端同一版本上线，回滚只做前向修复。
- 修订 v13（C5b）：新增可选的 `model.faces`，新增写入口 `derive_model`。不迁移，没有这个字段就是没有面分配。OpenAPI 的 `CreativeModelPayload` 与 `CreativeModelPreviewPayload` 加可选的 `faces`。回滚只做前向修复。
- 修订 v14（C5b 验收）：「重新生成时」补两种作废情况和 `hints` 的保留规则，改正 `t1` 关于重排的说明。只改渲染端的带入规则，数据外形不变，不迁移。
- 修订 v18（C5c1）：manifest 加 `uv`。不回填：之前写入的模型修订没有这个字段，读取方按「不知道」处理（编辑器按加载出的几何体判断，助手不提示），重新生成或派生自新修订后带上；`derive_model` 和库副本沿用来源修订的 manifest。OpenAPI 的 manifest 加可选的 `uv`。回滚只做前向修复。

## data: MODIFY 模型资产

- **用途**：资产库可以存放 `model` 资产：一份模型（几何、代码、参数，或导入的几何）加它的材质分配，相当于 Blender 的物件资产。位置、旋转、缩放是场景物件的变换，不随资产保存（修订 v12，owner 2026-10-20）。
- **存储**：
  - `creative_assets.kind` 的 CHECK 加 `model`。资产库现有能力（分组、标签、评分、回收站、筛选）对 `model` 一视同仁；按画面的筛选（主色、画幅）只作用于图片，模型自然不命中。
  - **库副本**（修订 v12）：存入时新建一个模型内容，写一个完整形态的修订，资产引用它。它是场景里那个模型修订的复制品：
    - 载荷：`format`、`generator`（含代码）、`manifest`、`faces`（修订 v13）、`name_hint` 与来源修订相同，另加两个字段：

      | 路径 | 类型 | 必须 | 约束 |
      |---|---|---|---|
      | `model.materials` | object | 库副本必有 | 槽位到材质的分配，结构同场景对象的 `material_slots`（`{<slot_id>: {preset, params, maps?, mapping?, alpha_mode?, alpha_cutoff?}}`，贴图字段自修订 v17），只保留有效槽位（manifest 的槽位加面级分配的槽位，修订 v13），可以是空对象 |
      | `model.source_revision_id` | string | 库副本必有 | 复制自哪个模型修订。来源本身是库副本时取它的 `source_revision_id`，所以总指向场景里烘焙出的修订 |

      场景命令写出的修订没有这两个字段；有 `source_revision_id` 就是库副本。
    - 对象：`original` 复用来源修订的 GLB blob，不复制字节；`thumbnail` 是存入时上传的 PNG，按所配材质和面级分配渲染（未分配的槽位按 C5 的规则取提示或默认材质）。
    - 写入口：只由「模型存入资产库」写入；`commit_model` 的操作结构里没有这两个字段，场景命令写不出库副本。约束由应用保证。
    - 修订 v12 之前直接引用场景修订的模型资产只存在于开发环境，照常可读，视为没有材质分配，不迁移。
  - **导入的模型**（scene-pose-system 子项 2）：导入接口新建一个模型内容，写一个完整形态的修订（`data: 模型内容`「导入的模型」），资产引用它。
    - 载荷带 `materials`（导入时由 glTF 材质换算，规则在 Epic `scene-pose-system` 的设计「导入的材质换算」），不带 `source_revision_id`。
    - 对象：`original` 复用暂存上传的 GLB blob；`thumbnail` 是导入时上传的 PNG，按 `materials` 渲染。
    - 写入口：只由导入接口写入，场景命令写不出。约束由应用保证。
- **查重**（修订 v12，替代 v11 的按模型修订查重）：按「来源修订 + 材质分配」。来源修订相同、材质分配完全相同（JSON 相等）且不在回收站的模型资产已存在时复用，只追加归类；材质不同就是另一份资产。不按 GLB 的 sha256 合并：GLB 相同而代码或参数不同的两个修订是两份资产，各自保留自己的代码。导入不查重：每次导入新建一份资产，同一文件导入两次就是两份（scene-pose-system 子项 2）。
- **持有**：资产是库副本修订的 GC 持有方，经现有的 `creativereference.LiveOwnerCondition`（按 `content_revision_id` 与 `kind` 匹配）。与来源修订共用的 GLB、存入时上传的缩略图随之保留；不新增引用列。
  - 库副本 `model.materials` 里引用的图片（修订 v17）：写库副本修订的同一事务里，每张图片写一行 `creative_content_revision_refs`（`holder_revision_id` = 库副本修订，`role = texture`），结构与 GC 规则见 `data: 材质资产`。存入时这些图片按场景的图片规则复核（本账号、`image`、可展示），不可用时存入失败，返回 400 `creative_image_unavailable`。
  - 导入的模型（scene-pose-system 子项 2）：`materials` 里的图片同样在写修订的事务里写 `creative_content_revision_refs`（`role = texture`）。这些图片是导入时拆出、以暂存上传上来的账号图片，导入成功时消费这些暂存上传；它们不单独作为图片资产出现在资产库里。
  - 场景命令写出的模型修订不带 `materials`，所以不写持有关系；场景里的贴图由场景修订的引用表持有。
- **库副本只属于资产库**（修订 v12）：载荷带 `materials` 的模型内容（库副本和导入的模型，后者自 scene-pose-system 子项 2）都只属于资产库。`commit_model` 或 `derive_model` 的 `base_content_id` 指向这样的内容时，服务端新建内容，不在库副本上追加修订，响应的 `models` 照常返回新内容的 ID。这样放进场景后的修改，不论来自编辑器还是助手、页面是否刷新过，总是分叉出场景自己的模型，库副本只有存入时的那一个修订。这条补充 `api: 场景模型命令` 的分叉规则（I4），由本条交付。
- **读取**（修订 v11）：
  - 资产列表、资产读取返回的模型内容去掉 `generator.code`，与场景读取一致；需要代码时按内容修订读取（`GET /creative/content-revisions/{id}`）。`materials`、`source_revision_id` 照常返回（修订 v12）。
  - 助手（修订 v11）：
    - `search_assets` 对模型项附模型要点 `model`：名称、尺寸 `size_m`（米）、`mount`、面数、材质槽（有效槽位，修订 v13）、参数名和材质分配 `materials`（修订 v12）。导入的模型没有参数名，另附 `imported: true`（scene-pose-system 子项 2）。不附缩略图的宽高，因为 256 × 256 不是模型尺寸。
    - `materials` 里的贴图只给图片的 `content_revision_id`、宽高和名称，不附像素（修订 v17）；助手经这个库副本可以把这些图片写进场景槽位，规则见 `data: 材质资产`「助手可读」。
    - `read_asset` 读模型时返回完整模型信息：在要点之外加部件、挂点、参数定义和当前值；导入的模型没有参数，改为附 `import`（原文件名、来源与授权说明、来源页面）。字段名与 `write_model` 回报的一致，不含代码。超出单次工具结果上限时依次省略部件与挂点、参数定义，`omitted` 列出省略项（修订 v12）。
    - 缩略图作为图片附件交给模型，需要模型能看图且摄影师允许图片预览。条件不满足，或之后因图片上限省略、读不到时，只保留模型信息，在 `thumbnail_omitted` 里说明原因，不报错。
    - 模型信息按 `media_metadata` 外发，与资产库元数据相同。
  - 卡片用 `thumbnail` 角色的 PNG，查看浮窗按 `original` 角色读 GLB，都走现有媒体读取票据；查看浮窗的「材质」模式按 `materials` 着色（修订 v12）。
- **放置**：模型资产只能放进场景（`add_objects` 的 `model.content_revision_id`），不能放到画布：没有 `core.model` 节点类型，画布的放入、填充和参考选择都不接受它。放入时实例的 `material_slots` 写资产的 `materials`：编辑器自动写，助手按 `edit_scene` 的说明写。之后在场景里换材质只改实例（修订 v12）。
- **OpenAPI**（修订 v12）：`CreativeModelPayload` 与只用于响应的 `CreativeModelPreviewPayload` 的 `model` 加可选的 `materials`、`source_revision_id`；scene-pose-system 子项 2 再加可选的 `import`。`CreativeModelPayload` 也被请求体引用，但模型不能经通用写入口写入，放宽不产生新的写法。
- **生命周期**：回滚时如已有 `model` 资产则拒绝，前向修复。资产库不记录资产被哪个场景使用过（I7）。

## api: ADD 导入模型到资产库

### 用途与调用
- **用途**：把摄影师导入的外部静态 GLB 存进资产库，成为一份 `model` 资产（`data: 模型内容`「导入的模型」、`data: 模型资产`）。成功即资产已建好，可以从资产库放进场景。
- **消费者**：资产库页面、资产库抽屉和编辑器资产面板的「导入模型」，以及编辑器「添加 > 导入模型…」（导入后再按 `api: 模型存入资产库`「兼容与变更」里的放入方式放进场景）。助手不能导入。
- **调用**：`POST /api/v1/creative/assets/from-glb`，`Idempotency-Key` 必填，外层为公共约定（`{operation_id, client_created_at, payload}`，`Idempotency-Key` 等于 `operation_id`），JSON。成功响应复用 `CreativeAssetCreateResult`（`api/openapi.yaml`）。
- **权限**：登录账号，有创意空间写入权限（同 `/creative/assets/from-scene-model`）；所有上传必须属于当前账号。
- **前置步骤（不属于本接口）**：浏览器先在本地解析原文件并换算（规则见 Epic 设计「导入的材质换算」），再经 `POST /creative/uploads` 以暂存目标上传：换算后的 GLB（声明类型 `model`）、拆出的每张图片（声明类型 `image`，PNG、JPEG 或 WebP）、缩略图（`image`，PNG）。等每个上传都 `ready` 后调用本接口。GLB 不合静态子集时，上传在 worker 校验阶段以 `media_unsupported` 失败，不会走到本接口。

### 请求
`payload`：

| 字段路径 | 位置 | 类型/格式 | 必传 | 可为 null | 缺省行为 | 约束与业务含义 |
|---|---|---|---|---|---|---|
| `glb_upload_id` | body | string | 是 | 否 | 缺失报错 | 换算后 GLB 的暂存上传：声明类型 `model`、`ready`、未过期未消费。服务端用暂存修订里自己算出的 manifest，不接受客户端的 |
| `thumbnail_upload_id` | body | string | 是 | 否 | 缺失报错 | 缩略图的暂存上传：声明类型 `image` 的 PNG，256 × 256，按 `materials` 渲染，取景与烘焙缩略图相同 |
| `texture_upload_ids` | body | string[] | 是 | 否 | 缺失报错 | 拆出的图片的暂存上传，可以为空数组，0–64 项，不重复。每一项的内容修订都必须出现在 `materials` 里，否则 400；导入成功时全部消费 |
| `mount` | body | `floor` \| `wall` \| `ceiling` \| `free` | 否 | 否 | `floor` | 写入 `generator.mount`，决定从资产库放入场景时怎么贴附表面 |
| `import.file_name` | body | string 1–255 | 是 | 否 | 缺失报错 | 原文件名，见 `data: 模型内容`「导入的模型」 |
| `import.attribution` | body | string ≤ 2000 | 否 | 否 | 不保存 | 来源与授权说明；空字符串等同不传 |
| `import.source_url` | body | string ≤ 2048 | 否 | 否 | 不保存 | `http` 或 `https` 的 URL；其他协议 400 |
| `materials` | body | `{<slot_id>: CreativeSceneMaterialSlot}` | 是 | 否 | 缺失报错 | 可以是空对象。键必须是 GLB 的槽位（manifest 的 `slots`），否则 400；值的规则同场景对象的 `material_slots`（`data: 场景文档`「材质贴图」），按场景命令同一套校验。贴图引用的 `content_revision_id` 可以是 `texture_upload_ids` 的修订，也可以是本账号已有的可用图片 |
| `target` | body | `CreativeUploadAssetTarget` | 是 | 否 | 缺失报错 | 沿用现有结构：`title` 必填（1–200），`description`（≤ 1000 码点）、`group_ids`、`tag_ids`、`new_tags`、`is_favorite` 可选。`title` 的前 80 个码点同时写入 `model.name_hint` |

### 成功响应
- 201：新建了模型内容和资产。导入不查重（`data: 模型资产`），`reused` 总是 `false`。

| 字段路径 | 类型/格式 | 必返或出现条件 | 可为 null | 业务含义与约束 |
|---|---|---|---|---|
| `asset_id` | string | 必返 | 否 | 新资产 |
| `revision` | string | 必返 | 否 | 资产修订，新建为 `"1"` |
| `content_revision_id` | string | 必返 | 否 | 导入的模型修订；放进场景时写进 `add_objects` 的 `model.content_revision_id` |
| `library_revision` | string | 必返 | 否 | 资产库修订 |
| `tag_mapping` | object | 必返 | 否 | `new_tags` 到新标签 ID，同 `/creative/assets/from-scene-model` |
| `reused` | boolean | 必返 | 否 | 总是 `false` |
| `title` / `description` | string | 必返 | 否 | 本次写入的值 |

随幂等回执重放：同一 `operation_id` 重复调用返回同一结果，不再新建资产，也不再消费上传。

### 失败响应
错误体为公共约定的 `{error: {code, message, details?}}`。

| HTTP 状态码 | 业务错误码 | 触发条件 | 调用方处理/是否可重试 |
|---|---|---|---|
| 400 | `validation_failed` | `mount`、`import` 字段不合法；`materials` 的键不是 GLB 的槽位，或值不合 `material_slots` 规则（`details` 指出字段路径）；`texture_upload_ids` 有重复，或某项的修订没被 `materials` 用到；缩略图不是 PNG | 修正后重试 |
| 400 | `creative_image_unavailable` | `materials` 引用的图片不属于本账号、不是 `image`、不可展示或已删除（同场景命令） | 重新上传图片后重试 |
| 404 | `not_found` | `target` 引用的分组或标签不存在（同 `/creative/assets/from-scene-model`） | 重读资产库 |
| 409 | `creative_upload_unavailable` | GLB、缩略图或某张图片的上传不存在、属于其他账号、声明类型不对、已过期、已消费或尚未就绪；`details.upload_id` 指出哪一个 | 重新上传后重试 |
| 413 | `creative_size_limit` | `materials` 引用的不同图片超过 64 张（单个场景的上限，超过就放不进任何场景） | 在导入前减少贴图，或合并贴图 |
| 409 | `idempotency_conflict` 等 | 公共约定 | — |

### 交互规则
- **写操作**：一个事务里写模型内容修订（GLB 与缩略图对象复用暂存 blob，不复制字节）、图片持有关系（`creative_content_revision_refs`，`role = texture`）和资产，再消费 GLB、缩略图和全部图片的暂存上传。任一步失败整体回滚，上传保持未消费，可以修正后重试；放弃导入时由浏览器丢弃这些上传，否则按暂存规则 7 天后过期回收。
- **幂等**：`Idempotency-Key` 作用域与有效期同公共约定；相同 key、不同请求体返回 `idempotency_conflict`。
- **配额**：上传按现有上传规则计入账号配额；本接口不另设限流。

### 调用示例
请求：

```http
POST /api/v1/creative/assets/from-glb
Idempotency-Key: 0b3c5d7e-1f2a-4b6c-8d9e-0a1b2c3d4e5f
Content-Type: application/json
```

```json
{"operation_id": "0b3c5d7e-1f2a-4b6c-8d9e-0a1b2c3d4e5f", "client_created_at": "2026-11-12T08:30:00Z",
 "payload": {"glb_upload_id": "cupl_7a01", "thumbnail_upload_id": "cupl_7a02", "texture_upload_ids": ["cupl_7a03", "cupl_7a04"],
             "mount": "floor",
             "import": {"file_name": "dragon_sword.glb", "attribution": "Dragon Sword by Example Artist, CC BY 4.0", "source_url": "https://example.com/models/dragon-sword"},
             "materials": {"blade": {"preset": "principled@1", "params": {"color": "#ffffff", "metalness": 1, "roughness": 1, "emissive": "#ffffff", "emissive_intensity": 2},
                                     "maps": {"base_color": {"content_revision_id": "ccrv_tex_base"},
                                              "roughness": {"content_revision_id": "ccrv_tex_orm", "channel": "g"},
                                              "metalness": {"content_revision_id": "ccrv_tex_orm", "channel": "b"},
                                              "emissive": {"content_revision_id": "ccrv_tex_base"}},
                                     "mapping": {"kind": "uv", "repeat": [1, 1], "rotation_deg": 0, "normal_strength": 1},
                                     "alpha_mode": "opaque"}},
             "target": {"title": "龙纹剑", "description": "cos 道具参考", "group_ids": ["cgrp_31aa"]}}}
```

其中 `ccrv_tex_base`、`ccrv_tex_orm` 分别是 `cupl_7a03`、`cupl_7a04` 就绪后的内容修订。

201：

```json
{"asset_id": "cast_c41d", "revision": "1", "content_revision_id": "ccrv_m8f2", "library_revision": "318", "tag_mapping": {}, "reused": false, "title": "龙纹剑", "description": "cos 道具参考"}
```

409（第二张图片的上传已过期）：

```json
{"error": {"code": "creative_upload_unavailable", "message": "上传不可用，请重新上传", "details": {"upload_id": "cupl_7a04"}}}
```

### 兼容与变更
首次定义，无已有消费者。新增 OpenAPI 路径与请求体 `CreativeAssetFromGLBPayload`；响应复用 `CreativeAssetCreateResult`。导入的模型的读取形态见 `data: 模型内容` 的修订说明（`CreativeModelGenerator` 按 `language` 区分，是对响应消费者的破坏性变更，前后端同一版本上线）。
