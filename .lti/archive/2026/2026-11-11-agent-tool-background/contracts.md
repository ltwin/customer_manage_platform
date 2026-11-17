# 契约增量

## glossary: ADD 后台任务

- 摄影师和助手都可以把一项耗时工具交给后台执行。后台任务有稳定句柄、原工具卡片和独立的完成结论，停止来源运行后仍按任务自身时限推进。
- 完成通知是对话里的系统消息。它记录后台任务的结果，也作为助手后续读取的输入。摄影师可以在原卡片取消进行中的后台任务。

## data: ADD 助手耗时工具任务与完成通知

### 用途与存储

PostgreSQL `creative_agent_tool_tasks` 由 `creativeagent` 维护。每行代表一个已受理的耗时工具步骤，作为前后台归属、取消协调、结果发布和通知送达的权威记录。执行、费用、产物和采用事实分别由源领域持有。工具卡片和对话列表均是投影。

### 字段与约束

| 字段 | 类型 | 必须 | 语义 |
|---|---|---|---|
| `id` | text | 是 | `cctask_<uuid>`，主键，由步骤身份稳定派生 |
| `account_id` | text | 是 | 账号作用域，所有访问通过 AccountScope |
| `conversation_id`、`canvas_id` | text | 是 | 从来源运行解析并校验，受理后固定 |
| `source_run_id`、`tool_step_id` | text | 是 | 来源运行和工具步骤，同账号归属由受理事务检查。唯一键 `(account_id, tool_step_id)` |
| `tool_key` | text | 是 | `generate_node`、`compress_image`、`write_model`、`render_view`、`export_shots` |
| `source_id` | text | 是 | 前两种分别关联 `creative_node_executions.id` 和 `creative_image_edit_renders.id`，后三种关联 `creative_scene_jobs.id`。对 `(account_id,tool_key,source_id)` 唯一 |
| `mode` | text | 是 | `foreground` 或 `background`，应用只允许前者单向转换 |
| `state` | text | 是 | `running`、`succeeded`、`failed`、`cancelled`、`expired`，应用只允许从运行态进入终态 |
| `revision` | bigint | 是 | 从 1 开始，卡片可见事实变化时递增，以十进制字符串返回 |
| `result` | jsonb | 终态必有 | 原工具的终态对象，类型见「耗时工具的等待参数与结果」。前后台复用结果构建器 |
| `finished_at` | timestamptz | 终态必有 | 任务结果确定的服务端时间，用于计算同配置组的唤醒时间 |
| `model_key`、`skill_version_id` | text | 模型必有 | 来源运行选定的模型和主 Skill 版本，无 Skill 为 null。记录选择以支持来源日志过期后的唤醒准入 |
| `notification_state` | text | 是 | `none`、`pending`、`delivered`、`suppressed`。前台和未完成任务为 `none`，后台终态改为 `pending` 或 `suppressed` |
| `notification_message_id` | text | 有通知必有 | 对话系统消息，账号内唯一，来源步骤与 role 唯一约束作为第二道去重 |
| `delivery_model_step_id` | text | 已送达必有 | 首次派发携带该通知的模型步骤，与 dispatch intent 同事务提交。连同 `notification_state` 是唯一的送达依据 |
| `wake_after` | timestamptz | 可空 | 同配置组的唤醒时间：`min(组内最后一条 finished_at + 2 秒, 第一条 finished_at + 10 秒)`，组内每多一条通知重算。null 表示由当前运行或摄影师后续输入送达，或无需再唤醒 |
| `wake_skip_reason` | text | 可空 | 不自动开轮的原因：`source_stopped`、`source_failed`、`chain_limit`、`egress`、`credits`、`unavailable`。有值时 `wake_after` 为 null，通知只随摄影师下次输入送达 |
| `wake_run_id` | text | 可空 | 已尝试自动运行的身份，同新运行的预算、槽和入队提交 |
| `next_check_at`、`stop_requested_at` | timestamptz | 可空 | 待收尾或待撤回工作时间，null 表示无需处理。撤回意图用于停止后补扫 |
| `created_at`、`updated_at` | timestamptz | 是 | 服务端时间，跟踪提交和修复 |

数据库 CHECK 约束工具、模式、状态、正数版本、终态结果完整性和通知组合。应用确保关联实体同账号且属于同一来源，源键不可更换，禁止逆转终态及前后台模式。消息通过 `creative_message_content_refs` 持有可用结果修订。运行模型步骤另增可空的 `background_notification_ids` JSON 数组，保存按实际装配顺序提交的任务句柄，只作重放用的冻结输入，不作送达依据。`creative_agent_runs` 增加可空 `trigger_kind`，自动运行为 `background_tool`，省略表示既有入口。终局模型步骤另存可空的 `background_notification_through_ordinal` bigint，标记本轮续跑能够领取到的对话序号上界。已派发请求保存实际送出引用，重放使用冻结的步骤输入和引用，沿用原步骤保留期。

### 访问与生命周期

- 索引：worker 跨账号扫描走全局部分索引 `(next_check_at, id) WHERE next_check_at IS NOT NULL` 和 `(wake_after, id) WHERE wake_after IS NOT NULL`，按批 `FOR UPDATE SKIP LOCKED`，每项按行上的 `account_id` 建 AccountScope 处理。对话待送达项走 `(account_id, conversation_id, finished_at, id) WHERE notification_state = 'pending'`。打开历史卡片用来源步骤唯一键读任务。
- 源状态触发器：`creative_node_executions`（`state`、`apply_state`）、`creative_image_edit_renders`（`state`）、`creative_scene_jobs`（`state`）上的 `AFTER UPDATE` 触发器，值变化时把对应运行中任务的 `next_check_at` 置为 `now()`，命中行时 `pg_notify('creative_agent_tool_tasks', '')`。触发器不读写运行、会话和消息，不抛异常；没有任务行时什么都不做。creative-worker 用专用 LISTEN 连接接收，另每 5 秒兜底扫描。
- `source_failed` 包含以 `failed` 结束的来源运行，以及带错误码的 `partial`。自动运行取得项目锁和会话槽锁后复查待送达通知，再决定开轮和预留积分。
- 模式、工具步骤结论、卡片投影和必要的运行续跑原子提交。终态通知和系统消息原子提交。只有完成时的后台任务可以进入通知待送达。前台场景任务在完成事务里由运行侧同时写任务终态并续跑，不经收尾器。
- 任务在创建源任务时登记。既有前台场景任务由无对应关联的旧路径继续处理。升级前已启动的生成、压缩运行走旧检查点兼容路径。
- 对话存在期间保留系统消息、终态卡片的最小投影及待送达任务。关闭或删除对话后抑制尚未写出的通知，任务源按照自身期限结束。对话永久删除时按账号和对话清理完成消息的引用及终态关联，仍在执行的任务留下结算和执行所需字段，终态后由维护扫描清除。运行、源记录或执行事件到期不会删除仍由对话持有的终态卡片与消息。
- 迁移增加表、索引、源状态触发器和可空字段，无存量回填。回滚先撤回后台入口和自动唤醒，让兼容 worker 完成任务与停止清理。已有任务及通知随数据保留，反向迁移在有这些记录时拒绝破坏性删除，保留 schema 并前向修复。
- 访问例：同账号两次收到一个执行的终态信号，收尾第一次提交 `succeeded + pending + notification_message_id`，第二次读取既有结果。若运行在 dispatch intent 前被停止，新的人工运行仍按对话查到 `pending`。

## api: ADD 耗时工具的等待参数与结果

### 用途与输入

`generate_node`、`compress_image`、`write_model`、`export_shots` 四个工具新增可选 `run_in_background`，布尔类型且不可为 null，省略等同于 `false`。`render_view` 不支持后台：截图是给助手当场看的，它的前台等待同样停靠、释放会话槽位。其余字段沿用各工具既有定义。未声明该字段的工具（含 `render_view`）携带该字段返回现有工具参数错误，源业务操作未提交。

| 工具 | 已受理任务 | 前台结果的内容 |
|---|---|---|
| `generate_node` | 画布生成执行 | `AgentGenerationResult`，含节点、执行、生成状态、采用状态、错误和已登记作品修订 |
| `compress_image` | 图片压缩候选与固定源修订 | `AgentCompressResult`，含候选、真实大小与尺寸、质量、结果状态及已采用版本 |
| `write_model` | 场景烘焙 | `sceneToolResult` 的烘焙分支，成功含对象和内容修订，失败含原因或冲突 |
| `render_view`（仅前台） | 场景截图 | `sceneToolResult` 的截图分支，成功含截图修订、所见场景修订和尺寸等事实 |
| `export_shots` | 场景导出 | `sceneToolResult` 的导出分支，成功含创建节点和所见场景修订 |

- 前台在任务和采用有结论后返回。同步参数拒绝及无需启动任务的直接结果走既有工具结果。后台受理后立即返回 `{"task_handle":"cctask_5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f","state":"running"}`。原工具调用已转后台时，用相同结构返回并附 `"detached":true`。
- 工具重放沿用同一受理身份。前台等待及后台通知使用同一领域结果。取消时前后台都报 `state: "cancelled"`。**语义变化**：生成被取消时前台原来报 `failed`，本次改为 `cancelled`；消费者只有助手模型和本仓库前端卡片，同版本上线。生成 `succeeded` 不等于采用成功，只有 `apply_state:"applied"` 表明画布采用了结果。
- 可用示例：`generate_node({"node_id":"ccnd_example","draft_revision":4,"run_in_background":true})`。错误示例：`search_web({"query":"镜头","run_in_background":true})` 返回工具参数错误。

## api: ADD 转后台与取消后台任务

### 用途与调用

| 方法与路径 | operationId | 用途 |
|---|---|---|
| POST `/api/v1/creative/agent-tool-tasks/{handle}/detach` | `detachCreativeAgentToolTask` | 摄影师让助手结束对这项工具的前台等待 |
| POST `/api/v1/creative/agent-tool-tasks/{handle}/cancel` | `cancelCreativeAgentToolTask` | 摄影师取消一项已在后台运行的任务 |

消费者为创意空间助手面板。登录账号、任务、来源运行及目标关系必须匹配。转后台需要 `agent_start` 和来源运行仍可前台等待。取消需要相应的任务控制与领域权限，经句柄定位真实源任务后执行；取消生成的规则见「后台任务取消画布生成」。

### 请求

| 字段 | 位置 | 类型 | 必传 / 可空 | 约束 |
|---|---|---|---|---|
| `handle` | path | string | 是 / 否 | `cctask_<uuid>` |
| `Idempotency-Key` | header | uuid | 是 / 否 | 与 `operation_id` 一致 |
| `operation_id` | body | uuid | 是 / 否 | 重试保留原值 |
| `client_created_at` | body | RFC 3339 datetime | 是 / 否 | 创意操作既有时间窗口 |
| `payload` | body | 空对象 | 是 / 否 | 拒绝额外字段 |

### 成功响应

200 返回 `CreativeAgentToolTask`，同一结构用于原工具块的可选 `task` 字段。

| 字段 | 类型 | 必返 / 可空 | 含义 |
|---|---|---|---|
| `handle` | string | 是 / 否 | 任务句柄 |
| `mode` | enum | 是 / 否 | `foreground`、`background` |
| `state` | enum | 是 / 否 | `running`、`succeeded`、`failed`、`cancelled`、`expired` |
| `revision` | CreativeRevision | 是 / 否 | 任务版本，合并时按版本排序 |
| `result` | JSON object | 终态必返 / 否 | 与 tool_key 匹配的终态工具结果，执行中省略 |
| `finished_at` | RFC 3339 datetime | 终态必返 / 否 | 服务端确定结果时间 |
| `tool_key` | enum | 是 / 否 | 对应五个工具；`render_view` 只会以前台出现 |
| `run_id` | string | 是 / 否 | 来源运行 |
| `tool_step_id` | string | 是 / 否 | 对应原工具卡片 |

转后台发现源已完成时返回 `foreground` 和终态，助手得到真实结果。重复转后台保持已有结果。取消操作以领域终态为准，成功取消返回 `cancelled`，已结束的任务返回既有终态。已送往供应商的费用按原规则结算。成功回执由 creativeops 原样重放。更新过的状态由快照取得。

### 错误与示例

| HTTP | 错误码 | 触发与处理 |
|---|---|---|
| 400 | `validation_failed` | 键、句柄或信封不合法。修正请求 |
| 401 / 403 | 认证或创意权限错误 | 会话失效或账号能力不足，重新登录或恢复权限 |
| 404 | `not_found` | 句柄缺失或属于其他账号，放弃本次操作 |
| 409 | `creative_run_state_conflict` | 来源运行已停止且任务仍为前台，或取消对象尚未转后台。重读卡片 |
| 409 | `idempotency_conflict` / `creative_operation_expired` | 键用于另一请求或回执超期，按创意操作公共约定处理 |
| 5xx | 通用服务错误 | 重读卡片或以同一信封重放 |

请求示例：

```http
POST /api/v1/creative/agent-tool-tasks/cctask_5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f/detach
Idempotency-Key: 4c6f28c0-9122-4e84-a1e8-aa1e40a7b211
Content-Type: application/json

{"operation_id":"4c6f28c0-9122-4e84-a1e8-aa1e40a7b211","client_created_at":"2026-11-13T10:05:12Z","payload":{}}
```

转后台成功与另一账号请求失败的响应示例：

```json
{"handle":"cctask_5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f","mode":"background","state":"running","revision":"2","tool_key":"generate_node","run_id":"ccrn_8b54a8c0-f1a2-4b84-90e8-144b1f6c2425","tool_step_id":"ccst_9b54a8c0-f1a2-4b84-90e8-144b1f6c2425"}
```

```json
{"error":{"code":"not_found","message":"任务不存在"}}
```

新增入口沿用 creativeops 写入和回执约定。并发以项目、账号范围、来源和任务事实决定，无分页。版本协商沿用现有客户端和服务端发布顺序。

## api: ADD 助手后台任务的快照和事件

- `CreativeAgentBlock` 工具块 payload 新增可选 `task: CreativeAgentToolTask`。同一块通过 `block.updated.patch.set` 更新。任务状态由 `task.state` 表示，工具日志的 block state 仍表示助手是否收到工具调用结果。
- `CreativeAgentMessage` 新增可选 `background_task: CreativeAgentToolTask`。只在后台任务的 `role=system` 完成通知出现，必为终态。原消息接口的权限、顺序与分页沿用既有规则。消息 `body` 提供纯文字提示，任务字段提供结构化结果。助手自动运行可引用该消息作 trigger，系统消息仍只有一行。
- `CreativeAgentRun` 新增可选 `trigger_kind:"background_tool"`，省略表示既有入口。终态会话运行的事件流仍关闭。运行流新增 `background_tool.completed`，`payload` 形如 `{"handle":"cctask_5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f","message_id":"ccms_8b54a8c0-f1a2-4b84-90e8-144b1f6c2425"}`，触发增量重读。通用事件信封版本仍为 1。
- `waiting_client` 的原 `client_job`、`slot_busy` 原因保持可读。新增 `tool_task` 表示等待持久化的耗时工具任务。停靠释放槽位，真正续跑时重新取得。
- 画布失效通知由现有通道发送，触发相同账号和画布的读快照。新字段均为扩展，刷新和断线后以快照恢复。

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
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。终态进入关联任务收尾，前台继续运行，后台生成完成通知。
- **完成**：前台按既有能力、运行期限、Skill 与外发同意复查，验证令牌和租约，落产物并写终态；运行侧在同一事务里写耗时工具任务终态并续跑，不经收尾器。前台复查不通过时按原规则结束运行并取消任务。后台按受理时的账号、对话、目标和任务自身期限复查，仍检查创意能力、租约、令牌和领域写入条件，来源运行作为溯源；写回场景不是外发，不复核外发同意。后台准入拒绝时记录可解释的任务终态。后台任务的终态由源表触发器标记收尾，收尾器写完成通知。无耗时工具任务关联的存量场景任务沿用原终态端口。
- **取消**：运行停止或终止时，同事务把属于前台的非终态场景任务改为 `cancelled` 并清空令牌。后台场景任务由对应后台句柄控制取消，和渲染端完成争同一源任务锁。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。修订 v10：`render_view` 的 `mode` 增加 `lit`（C5），请求外形不变，旧任务缺省按 `clay`。

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
| 409 | `creative_state_conflict` | 任务已不是 `claimed`、令牌不匹配或租约已失效；或前台关联运行复查不通过（运行已结束，任务已取消） | 丢弃产物；任务会被重新认领、已过期或已取消 |
| 409 | `creative_upload_unavailable` | 暂存上传不存在、已过期或已被消费 | 租约仍有效时重新上传，用新的 operation id 重试 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
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

## api: ADD 助手图片压缩的无人值守采用

- `compress_image` 拆成两步：候选受理（固定源修订、质量参数，派生创建、采用、丢弃三个 operation ID），以及采用。工具调用内不再每 2 秒轮询，也去掉 90 秒等待上限。
- 候选 ready 后走统一收尾，调用图片编辑面板也使用的同步 apply。快速完成的候选可以由当前工具调用直接进入收尾。收到提交回执或读回候选确认已采用后报告 `applied`。这取代 `docs/product/creative-canvas-system/modules/image-editing-backend.md` §5.3 第 2、4 条（「等待上限 90 秒」「取消 / 进程崩溃后没有后台自动采用者」），该文档同步修订。
- 在 apply 事务内复核任务归属和当前模式，前台同时复核运行执行权。沿用源修订和画布写入检查、非内容版本冲突的有界重试及回执核实。apply 已发出而响应未知时按原操作回执核实，不误报未应用。
- 不采用的情况：来源运行停止时任务仍为前台；卡片取消；候选失败或过期。这些情况丢弃候选，任务结果为 `not_applied`（取消为 `cancelled`）。
- 摄影师在编辑面板发起的压缩和裁切不变，仍由前端等 ready 后自己调用 apply。

## api: ADD 后台任务取消画布生成

- 2026-10-09 起摄影师不能取消生成类执行（`CancelPhotographerExecution`）：供应商受理后照样扣费，取消省不了钱。本次开一个局部例外：助手发起、**已在后台**的生成，可以经 `POST /api/v1/creative/agent-tool-tasks/{handle}/cancel` 取消。画布命令 `cancel_execution` 对摄影师照旧拒绝生成；前台等待中的生成没有取消入口（先转后台）。
- 实现：按句柄定位执行，调用内部 `CancelNodeExecution`。未派发的请求撤回、不计费；已派发的照常跑完并扣费，结果不采用（`apply_state=discarded`）。任务进入 `cancelled` 并发完成通知。
- 卡片在取消按钮旁写明「已发出的生成仍会扣费」。

## data: MODIFY 运行中附带内容的外发授权

- 权威定义在 `docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」，本次整段改写为账号级规则。
- 外发需要同时满足两点：
  - 账号有有效的外发同意（见「账号外发同意」）。
  - 内容在本运行的读取范围内：当前画布节点持有的内容；本对话中、序号在触发消息之前或属于本运行的完整消息所引用的内容；**本对话中后台任务完成通知（`role=system` 且关联后台任务）所引用的内容**；同意允许检索素材库时，未进回收站的资产的当前修订。
- 各内容仍逐项校验：属于本账号且当前可用（`RequireUsable(display)`，含用途权利）；数据类别在同意范围内；图片要求运行模型能看图；派发前照常复核同意和可用性。
- 摄影师新建消息、插队、补充时附带或 @ 引用的内容，都写在本运行的消息上，所以由上面第二条分支覆盖，不需要清单。
- 完成通知单列一条分支，是因为运行中途送达的通知序号在触发消息之后，自动运行的触发消息本身也不满足「序号在之前」；通知不属于某次运行，按对话读取。
- 删除的规则：
  - `selected_revisions` 清单（`creative_egress_consent_contents`）不再参与判断；
  - 「被本对话已撤回的授权列过的修订一律拒绝」这条规则不再存在。
- 不变：工具返回的素材、助手检索得到的内容，仍按读取范围和逐项校验处理。
- 素材库关闭后，本运行和本对话历史里素材库工具（`search_assets` 查账号素材库、`read_library`、`read_library_result`、整理/管理/回收站/恢复、`save_to_library`）返回的内容不再随之后的请求发出，换成一句占位说明；`read_history` 和压缩摘要的转录同样处理。请求组装时素材库还开着、派发事务提交前关了的，这次派发被拒绝，运行以 `creative_egress_library_closed` 结束（门禁上按缺少同意处理，只是结束原因单列，前端提示「素材库检索已关闭」而不是「发送前说明有更新」）。`read_asset` 读到的素材库内容属于本运行自己的会话引用，按「已有会话引用仍按原许可读取」继续可读，不在占位范围内。
- 修订（agent-tool-background）：读取范围增加完成通知分支。
