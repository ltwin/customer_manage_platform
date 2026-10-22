<!-- 已批准的目标契约，不代表项目当前已实现。子项通过 epic bind --deliver 引用，不复制正文。 -->

## architecture: ADD 外部助手接入

### 职责与边界
- 外部助手（首批宿主为 ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页））经远程 MCP 端点 `<PUBLIC_BASE_URL>/mcp` 使用创意空间工具，经本系统自带的 OAuth 授权服务器取得令牌。
- `backend/internal/platform/oauth`：授权服务器。负责元数据、客户端注册（CIMD、DCR，含网页端连接器的机密客户端）、授权码与 PKCE、访问令牌签发与校验、refresh 轮换、外部助手连接的增删查、活动状态。不依赖任何 `creative*` 包。
- `backend/internal/creativemcp`：MCP 协议适配，基于官方 Go SDK。把 `creativeagent` 的外部工具目录注册为 MCP 工具，下发服务端 `instructions`（正文由 `creativeagent` 维护），转换错误和图片结果。客户端声明 MCP Apps 扩展（`io.modelcontextprotocol/ui`）时，另外提供两个界面资源（生成与任务卡、镜头卡），并给挂卡片的工具附上卡片元数据；没声明时不出现任何卡片相关内容。是一个 `http.Handler`，不 import gin，不含业务规则。
- `backend/internal/creativeagent`：外部工具门面 `ExternalTools`（`List` / `Call`）。工具实现依赖「助手主体」调用者接口，运行和外部连接各自实现它；同名工具在两条路径上执行同一个领域入口，说明出自同一个 `Info()`。
- `platform/httpapi`：挂载 `/.well-known/oauth-protected-resource/mcp`、`/.well-known/oauth-authorization-server`、`/oauth/*`、`/mcp`，以及网页端的 `/api/v1/creative/agent-connections`。`/mcp` 的 Bearer 校验器由 `platform/oauth` 提供，通过后把外部连接主体交给 `creativemcp`。
- 前端：「外部助手」接入页（按宿主给出接入文字与回退方法，连接列表与断开）、`/oauth/authorize` 授权确认页、创意空间的「外部助手已连接」指示、场景历史的外部助手署名。
- 接入物料：
  - Codex 插件 `integrations/codex-plugin/`：manifest、MCP 配置（调大 `tool_timeout_sec`）、marketplace；只发生产包，不含平台 Skill 正文。
  - Claude 系列只用连接器（桌面端、网页与登录 claude.ai 账号的 Claude Code 共用），ChatGPT 用 Developer Mode 应用，两者都没有插件。
  - 所有宿主共用的使用说明是服务端 `instructions`。
- 不职责：服务端渲染（场景客户端任务仍由打开的创意空间页面执行）；外部助手的对话、计划和模型计费。
- 依赖方向：`httpapi` → `platform/oauth`；`httpapi` → `creativemcp` → `creativeagent` → 各创意领域包。`creativeagent` 不知道 MCP 与 OAuth，只认助手主体。业务表只保存外部连接 ID 作为署名与归属，不读授权服务器的表。

### 关键数据流
- 授权：宿主读受保护资源元数据 → 授权服务器元数据 → CIMD 或 DCR → 浏览器授权确认页（复用网页登录态）→ 授权码 + PKCE 换令牌。
- 工具调用：`/mcp` Bearer 校验（签名、受众、过期、连接有效）→ `creativemcp` → `ExternalTools.Call`（外部准入、限流）→ 与内置助手相同的领域入口，写入者为外部连接 → 结果、图片或任务句柄。
- 异步：生成与场景任务超过 40 秒未完成时返回任务句柄，外部助手用 `wait_tasks` 长轮询（单次 ≤ 45 秒）。场景任务由打开的页面认领执行，完成时只落结果，不唤醒运行。
- 断开：同事务标记连接、作废 refresh 链、取消非终态场景任务 → 账号失效通道通知页面。
- 卡片：图形界面宿主（ChatGPT、Claude 桌面端和网页）读取界面资源，把工具结果渲染成沙箱里的卡片；卡片里的动作由宿主代为调用本服务的工具（同一个连接、同一个令牌），再把创作者的决定作为上下文交给外部助手。服务端分辨不出一次调用来自卡片还是模型，两者的准入和语义完全相同。

### 长期约束
- 网页端令牌与 MCP 令牌受众不同、签名密钥按用途派生，互不通用。
- `MCP_ENABLED` 关闭时 `/mcp`、`/oauth/*` 与两个元数据端点都不挂载。

## glossary: ADD 外部助手

- 定义：在创意空间以外运行、经 MCP 调用创意空间工具的 AI 助手；首批宿主为 ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页）。
- 不是：内置创意助手（界面上的「助手」）；不是摄影师本人。
- 边界场景：创作者在 Claude Code 里让它给画布加节点，是外部助手的写入；在创意空间助手面板里提同样的要求，是助手的写入。
- 拼写：代码 `external_agent`；界面显示宿主名「Claude」「Claude Code」「Codex」「ChatGPT」，泛称「外部助手」。

## glossary: ADD 外部助手连接

- 定义：账号对某个外部助手客户端的一次 OAuth 授权，是外部助手在平台上的身份；署名、任务归属、断开都以它为单位。
- 不是：一次对话或一次工具调用；不是 OAuth 客户端（同一个客户端被两个账号授权，是两个连接）。
- 边界场景：同一账号在两台电脑上各授权一次 Claude Code，是两个连接，可以分别断开。
- 拼写：代码 `external_connection_id`，ID 前缀 `cxcon_`；界面「外部助手连接」。

## data: ADD 外部助手连接与 OAuth

### 用途与存储角色
- **用途**：外部助手的授权与身份：连接、OAuth 客户端、授权码、refresh token。
- **存储**：PostgreSQL，新表 `external_agent_connections`、`oauth_clients`、`oauth_authorization_codes`、`oauth_refresh_tokens`。
- **角色**：连接是权威主数据；客户端、授权码、refresh token 是授权服务器内部数据。
- **所有权**：只有 `platform/oauth` 读写。业务模块只拿到连接 ID 和展示名（经助手主体传入），不读这些表。

### 结构与身份

`external_agent_connections`：

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `id` | text | 是 | `cxcon_<uuid>`，主键 |
| `account_id` | text | 是 | 账号隔离；外键到账号 |
| `client_id` | text | 是 | OAuth 客户端标识：CIMD 时是元数据 URL，DCR 时是 `oclient_<uuid>` |
| `client_name` | text ≤ 100 | 是 | 授权时客户端声明的名称，原样保存，用于确认页和审计 |
| `host_kind` | text | 是 | `claude`（claude.ai 连接器，含共用它的 Claude Code）/ `claude_code`（本机添加的 Claude Code）/ `codex` / `chatgpt` / `other`（CHECK）；授权时按客户端元数据（客户端名、回调地址主机、CIMD 地址）识别，识别规则在 C1 实测后固定 |
| `display_name` | text ≤ 100 | 是 | 界面署名：「Claude」「Claude Code」「Codex」「ChatGPT」，`other` 取 `client_name` |
| `scopes` | text[] | 是 | 当前只有 `{creative}` |
| `created_at` | timestamptz | 是 | 授权时间 |
| `last_used_at` | timestamptz | 否 | 最近一次工具调用；每分钟最多更新一次 |
| `revoked_at` | timestamptz | 否 | null 表示有效 |
| `revoked_reason` | text | 否 | `user`（摄影师在接入页断开）/ `refresh_replay`（检测到已轮换的 refresh token 被重放）/ `account_disabled`（CHECK）；`revoked_at` 非空时必填 |

索引：`(account_id, revoked_at, last_used_at DESC)`。

`oauth_clients`：

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `client_id` | text | 是 | 主键 |
| `registration` | text | 是 | `cimd` / `dcr`（CHECK） |
| `client_name` | text ≤ 100 | 是 | — |
| `redirect_uris` | text[] | 是 | 1–5 个；每个都是回环地址（`http://127.0.0.1:*`、`http://localhost:*`，命令行宿主）或 https（网页端宿主的服务端回调，例如 claude.ai、ChatGPT） |
| `token_endpoint_auth_method` | text | 是 | `none`（公共客户端）/ `client_secret_post` / `client_secret_basic`（机密客户端，网页端宿主）（CHECK）；CIMD 客户端只能是 `none` |
| `client_secret_hash` | text | 机密客户端时 | 注册时签发的密钥的 SHA-256；明文只在注册响应里返回一次 |
| `metadata` | jsonb | 是 | 注册时或 CIMD 拉取到的原始元数据（≤ 5 KB） |
| `fetched_at` | timestamptz | CIMD 时 | 上次拉取时间；超过缓存期（≤ 24 小时）再用时重新拉取 |
| `created_at` | timestamptz | 是 | — |

客户端不属于任何账号：同一个客户端（例如 claude.ai 的连接器）可被多个账号授权。DCR 客户端 90 天没有被任何连接使用时由维护任务删除。

`oauth_authorization_codes`：主键 `code_hash`（SHA-256），列 `client_id`、`account_id`、`connection_id`、`redirect_uri`、`code_challenge`（S256）、`resource`、`expires_at`（签发后 60 秒）、`used_at`。只能用一次；第二次使用时断开对应连接。过期一天后删除。

`oauth_refresh_tokens`：主键 `token_hash`（SHA-256），列 `connection_id`、`account_id`、`state`（`current` / `rotated` / `revoked`，CHECK）、`created_at`、`last_used_at`、`absolute_expires_at`（连接内第一张 refresh token 签发后 90 天）。每个连接同时最多一行 `current`（部分唯一索引）。空闲超过 30 天视为失效。

### 访问模式与保证
- **授权确认**：同一事务建连接、写授权码。同一账号对同一 `client_id` 再次授权时新建一个连接，不复用旧连接（旧连接保持原状，摄影师可以在接入页断开）。
- **换令牌**：锁授权码行 → 校验未用、未过期、`redirect_uri`、PKCE、`resource` → 标记已用 → 写第一张 refresh token。
- **refresh**：锁 refresh 行 → `current` 则改为 `rotated` 并写新的 `current`；`rotated` 则断开连接（`refresh_replay`）、把该连接所有行改为 `revoked`。
- **每次 MCP 请求**：按 `id` 读连接，`revoked_at` 为 null 才放行。
- **断开**：同一事务写 `revoked_at`、作废 refresh 链，并通过 `creativescene` 的端口取消该连接非终态的场景客户端任务（C3 起）；提交后发账号失效通知。
- **保证方**：
  - 由数据库保证：主键唯一、CHECK、每连接一行 `current`。
  - 由应用保证：授权码一次性、PKCE 与 `resource` 校验、断开的级联。

### 生命周期与演进
- 连接永不物理删除。业务表用它的 ID 做署名，已断开的连接仍是署名来源。
- 账号停用时，维护任务断开它的全部连接（`account_disabled`）。
- 以后做细粒度授权时扩展 `scopes`，存量连接保持 `{creative}`。

## data: MODIFY 通用文档

### 用途与存储角色
- **用途**：画布文档的头、修订历史、引用登记和工作会话指针。类型载荷存放在内容修订里（`creative_content_revisions`），不在这些表中。
- **存储**：PostgreSQL。扩展 `creative_documents`，新增 `creative_document_revisions`、`creative_document_content_refs`、`creative_document_conversations`，迁移 `0072_creative_document_foundation`（合入 develop 时与 `0069`–`0071` 的生成路由迁移撞号，改号为 0072）。
- **角色**：
  - 文档行：权威主数据（当前修订、标题、单元修订号、回收站状态）。
  - 修订表：历史的唯一顺序来源。
  - 引用表：GC 持有登记。
  - 会话表：指针，不参与版本。
- **所有权**：只有 `creativedocument` 写入；`creativereference` 只读修订表和引用表来判定持有。

### 结构与身份

`creative_documents`（扩展，原有列不变）：

| 列 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `created_at` / `updated_at` | timestamptz | 是 | `clock_timestamp()`；每次写命令、进出回收站都更新 `updated_at` |
| `trashed_at` | timestamptz | 否 | null 表示不在回收站 |
| `unit_revs` | jsonb | 是 | 对象，键为单元名，值为正整数修订号；必须含 `meta`；键 ≤ 4096；CHECK `jsonb_typeof='object'` |

索引：`(account_id, type_key, updated_at DESC, id DESC)`、`(account_id, updated_at DESC, id DESC)`。

`creative_document_revisions`：

| 列 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `account_id`, `document_id`, `revision` | text, text, bigint > 0 | 是 | 主键 `(account_id, document_id, revision)` |
| `content_revision_id` | text | 是 | 该修订的快照；GC 持有方 |
| `title` | text ≤ 200 | 是 | 该修订时的标题，供整份恢复 |
| `actor_kind` | text | 是 | `photographer` / `agent` / `external_agent` / `system`（CHECK）；`external_agent` 为外部助手（epic `creative-mcp-server`） |
| `run_id` | text | 否 | `actor_kind = 'agent'` 时必填，其他写入者为 null（CHECK） |
| `external_connection_id` | text | 否 | `actor_kind = 'external_agent'` 时必填，其他写入者为 null（CHECK）；外部助手连接 ID（`data: 外部助手连接与 OAuth`），不建外键 |
| `restored_from` | bigint | 否 | 由恢复产生时为来源修订号 |
| `changed_units` | text[] | 是 | 本修订推进了修订号的单元；整份恢复记为 `{'*'}`；新建记为 `{'*'}` |
| `annotated_units` | text[] | 是 | 本修订只做标注、未推进修订号的单元（如确认），缺省 `{}` |
| `removed_units` | text[] | 是 | 本修订删除的单元，缺省 `{}`；整份恢复记为空，由 `changed_units = {'*'}` 表示全部重置（实施审计补充） |
| `unit_revs` | jsonb | 是 | 本修订写入**之后**的完整单元修订号映射（与当时的文档头一致）；供旧版本读取、恢复钩子判断「当时是否已确认」等使用 |
| `created_at` | timestamptz | 是 | — |

索引：`(account_id, document_id, run_id, revision DESC)`；`(account_id, document_id, external_connection_id, revision DESC)`（外部助手「自上次写入后的变化」与按连接过滤历史）；`(account_id, content_revision_id)`（GC 按快照判定持有时用）。

`creative_document_content_refs`：主键 `(account_id, document_id, revision, content_revision_id)`，索引 `(account_id, content_revision_id)`。每个修订登记一次它快照引用的全部内容修订。

`creative_document_conversations`：主键 `(account_id, document_id, canvas_id)`，列 `conversation_id text not null`、`updated_at`。

### 访问模式与保证
- **写命令**：锁文档行 → 版本检查 → 写内容修订、修订行、引用行 → 更新文档头，在一个事务里完成（design「一次命令的处理顺序」）。
- **读当前**：按 ID 读文档行和当前快照。
- **列表**：按 `(updated_at, id)` 倒序加过滤。
- **历史**：按 `revision` 倒序，可按 `run_id` 或 `external_connection_id` 过滤。
- **保证方**：
  - 由数据库保证：修订号主键唯一、写入者 CHECK、`agent` 必带 `run_id`、`external_agent` 必带 `external_connection_id`、`unit_revs` 为对象。
  - 由应用保证：每个修订行的 `unit_revs` 等于该修订提交时文档头的 `unit_revs`。
  - 由应用保证：修订号连续递增、单元修订号规则（D-I2）、回收站只读、文档行和最新修订行一致（D-I1）。

### 生命周期与演进
- **回填**：迁移为每份存量文档补一行修订（`actor_kind='system'`，`changed_units='{*}'`，标题和快照取当前值），并把文档头与这一行的 `unit_revs` 设为 `{"meta": revision}`。`internal.document` 再补 `body`，与 `meta` 相同。
- **彻底删除**：删除会话指针、引用行、修订行和文档行。快照内容修订（`text`、`plan`、`scene` 等非媒体类型）不在现有媒体 GC 的回收范围内（`backend/internal/creativemedia/gc.go` 的 `unheldMediaRevision` 只处理 `image` / `video` / `audio`），首版保留为无入口数据，**不承诺物理回收**。被快照引用的媒体在失去全部持有方后，照常由媒体 GC 回收。
- **回滚**：存在 `system` 以外写入者的修订行，或存在 `trashed_at` 时拒绝回滚，前向修复。
- **外部助手扩展**（epic `creative-mcp-server` C2）：迁移只放宽 `actor_kind` 的 CHECK、新增可空列和索引，存量行不变；回滚时存在 `external_agent` 修订行则拒绝回滚，前向修复。
- **GC**：`LiveOwnerCondition` 增加修订表的 `content_revision_id` 和引用表两行，只扩大持有范围。


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
| `run_id` / `tool_call_id` | text | 助手创建时 | 关联运行与工具调用；外部助手创建时 `run_id` 为 null、`tool_call_id` 为 MCP 调用 ID；摄影师发起的导出两者都为 null |
| `external_connection_id` | text | 外部助手创建时 | 外部助手连接 ID（`data: 外部助手连接与 OAuth`）；与 `run_id` 最多一个非空（CHECK）；不建外键 |
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

索引：`(external_connection_id, state)`（部分索引，非空时），用于断开连接时取消它的任务和 `wait_tasks` 校验归属；`(account_id, state, created_at)`，用于账号级认领；`(account_id, document_id, state, created_at)`，用于按场景认领；`(state, deadline)`，用于过期扫描；`(run_id)`，用于取消运行时找到它的任务。

### 访问模式与保证
- **认领**：取最早的 `queued` 任务（指定场景时只在该场景内），用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。没有任务时只走索引、不加锁，渲染端在每次失效通知时都会调用。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。两者都经终态端口让关联运行继续。
- **完成**：
  - 运行的任务：经终态端口复查关联运行（与助手写工具同一套准入：能力、deadline、Skill、consent）→ 校验令牌与租约 → 落产物 → 改终态 → 让运行继续，在同一事务里提交（design「客户端任务」）。复查不通过时运行按现有规则结束，任务随之取消。
  - 外部助手的任务（`external_connection_id` 非空）：复查连接仍有效、账号有 `agent_start` → 校验令牌与租约 → 落产物（写入者为外部连接）→ 改终态，同一事务提交；不唤醒任何运行，结果由外部助手经 `wait_tasks` 读取。连接已断开时任务改为 `cancelled`，不落产物。
- **取消**：运行被取消时，在取消运行的同一事务里把它的非终态任务改为 `cancelled`，清空令牌。外部助手连接被断开时，在断开的同一事务里经 `creativescene` 的端口把该连接的非终态任务改为 `cancelled`。
- **等待页面**：任务处于 `queued` 且创建超过 15 秒时，`wait_tasks` 对外显示为 `waiting_for_page`。这是读取时的派生状态，不写入 `state`。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 外部助手扩展（epic `creative-mcp-server` C3）：新增可空列、CHECK 与部分索引，存量行不变；`request` / `result` 的外形不变，`export_shots` 的 `canvas_id` 由工具参数给出而不是运行所在画布。
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。修订 v10：`render_view` 的 `mode` 增加 `lit`（C5），请求外形不变，旧任务缺省按 `clay`。


## api: ADD OAuth 授权服务器

### 用途与调用
- **用途**：让外部助手按 MCP 授权规范取得、续期访问令牌。成功的标志是宿主拿到受众为 `<PUBLIC_BASE_URL>/mcp` 的访问令牌和 refresh token。
- **消费者**：四个首批宿主的 MCP 客户端（Codex CLI、Claude Code 在本机发起；claude.ai 连接器、ChatGPT 在各自服务端发起）；`/oauth/authorize` 的确认页（前端）。
- **调用**：
  - `GET /.well-known/oauth-protected-resource/mcp`：受保护资源元数据（RFC 9728）。
  - `GET /.well-known/oauth-authorization-server`：授权服务器元数据（RFC 8414）。
  - `POST /oauth/register`：动态客户端注册（RFC 7591，兼容用途）。
  - `GET /oauth/authorize`：授权请求，由前端页面承接。
  - `POST /api/v1/oauth/authorize/decision`：确认页提交「允许」或「拒绝」（网页端鉴权）。
  - `POST /oauth/token`：授权码换令牌、refresh。
- **格式**：元数据、注册、令牌接口为 `application/json`；令牌请求体为 `application/x-www-form-urlencoded`（OAuth 规定）。错误体使用 OAuth 标准错误格式 `{error, error_description}`，不使用项目 `ErrorEnvelope`，因为调用方是遵循 OAuth 的通用客户端。`decision` 接口是网页端接口，沿用 `ErrorEnvelope`。
- **权限**：元数据、注册、授权请求、令牌接口无需鉴权（OAuth 协议本身的公开端点），访问边界由 PKCE、回调地址校验和一次性授权码保证。`decision` 需要网页端 Bearer（已登录的摄影师），连接建在该账号下。

### 受保护资源元数据
`GET /.well-known/oauth-protected-resource/mcp`，无请求参数。200：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `resource` | string | 是 | `<PUBLIC_BASE_URL>/mcp` |
| `authorization_servers` | string[] | 是 | `[<PUBLIC_BASE_URL>]` |
| `scopes_supported` | string[] | 是 | `["creative"]` |
| `bearer_methods_supported` | string[] | 是 | `["header"]` |
| `resource_name` | string | 是 | 「创意空间」 |

### 授权服务器元数据
`GET /.well-known/oauth-authorization-server`，无请求参数。200：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `issuer` | string | 是 | `<PUBLIC_BASE_URL>` |
| `authorization_endpoint` | string | 是 | `<PUBLIC_BASE_URL>/oauth/authorize` |
| `token_endpoint` | string | 是 | `<PUBLIC_BASE_URL>/oauth/token` |
| `registration_endpoint` | string | DCR 保留时 | `<PUBLIC_BASE_URL>/oauth/register`（见 design U2） |
| `response_types_supported` | string[] | 是 | `["code"]` |
| `grant_types_supported` | string[] | 是 | `["authorization_code", "refresh_token"]` |
| `code_challenge_methods_supported` | string[] | 是 | `["S256"]` |
| `token_endpoint_auth_methods_supported` | string[] | 是 | `["none", "client_secret_post", "client_secret_basic"]` |
| `scopes_supported` | string[] | 是 | `["creative"]` |
| `client_id_metadata_document_supported` | boolean | 是 | `true` |
| `authorization_response_iss_parameter_supported` | boolean | 是 | `true` |

### 动态客户端注册
`POST /oauth/register`，body JSON：

| 字段 | 类型 | 必传 | 约束 |
|---|---|---|---|
| `client_name` | string | 是 | 1–100 字符 |
| `redirect_uris` | string[] | 是 | 1–5 个，回环地址或 https |
| `grant_types` | string[] | 否 | 缺省 `["authorization_code","refresh_token"]`；只能是这两个的子集 |
| `token_endpoint_auth_method` | string | 否 | 缺省 `none`；可为 `client_secret_post`、`client_secret_basic`；其他值拒绝 |
| 其他 RFC 7591 字段 | — | 否 | 原样存入 `metadata`，不影响行为 |

201：`{client_id, client_id_issued_at, client_name, redirect_uris, grant_types, token_endpoint_auth_method, client_secret, client_secret_expires_at}`。`client_secret` 只在机密客户端时返回（只返回这一次），`client_secret_expires_at` 为 `0`（不过期）；公共客户端不含这两个字段。

失败（400）：`invalid_redirect_uri`（不是回环或 https、超过 5 个）；`invalid_client_metadata`（缺 `client_name`、不支持的认证方式或授权类型）。同一 IP 每小时注册 ≤ 20 次，超出 429，带 `Retry-After`。

### 授权请求
`GET /oauth/authorize`，query：`response_type=code`、`client_id`、`redirect_uri`、`code_challenge`、`code_challenge_method=S256`、`state`、`resource`（必须等于 `<PUBLIC_BASE_URL>/mcp`）、`scope`（可选，只能是 `creative`）。

- 服务端先校验客户端：`client_id` 是 https URL 时按 CIMD 拉取元数据（只允许 https、公网地址、≤ 5 KB、3 秒超时，元数据里的 `client_id` 必须等于该 URL）；否则必须是已注册的 DCR 客户端。`redirect_uri` 必须精确匹配（回环地址允许端口不同）。
- 客户端或回调地址不合法时，**不重定向**，直接显示错误页（防止开放重定向）。
- 其他参数错误（缺 PKCE、`resource` 不符、`scope` 不支持）时，重定向回 `redirect_uri`，带 `error=invalid_request|invalid_scope|invalid_target`、`state`、`iss`。
- 合法时返回前端确认页。前端未登录则走现有登录，登录后回到确认页。确认页显示 `client_name`、回调地址主机、能做的事和数据去向（design「授权与 consent」）。

### 确认授权
`POST /api/v1/oauth/authorize/decision`，网页端 Bearer，body JSON：

| 字段 | 类型 | 必传 | 说明 |
|---|---|---|---|
| `request` | string | 是 | 授权请求页面拿到的签名请求包（服务端校验过的 authorize 参数，10 分钟有效，防篡改） |
| `decision` | string | 是 | `allow` / `deny` |

200：`{redirect_to}`，前端跳转。`allow` 时建连接和授权码，`redirect_to` 带 `code`、`state`、`iss`；`deny` 时 `redirect_to` 带 `error=access_denied`、`state`、`iss`。

失败：400 `validation_failed`（请求包过期或签名不符，前端提示重新从外部助手发起）；401 沿用网页端处理；403 `forbidden`（账号没有创意空间能力）。

### 令牌
`POST /oauth/token`，form：

| 字段 | 必传 | 说明 |
|---|---|---|
| `grant_type` | 是 | `authorization_code` 或 `refresh_token` |
| `client_id` | 是 | 必须与授权码或 refresh token 所属连接的客户端一致；机密客户端按注册的认证方式另带密钥（表单 `client_secret` 或 HTTP Basic），不符时 `invalid_client` |
| `code`、`redirect_uri`、`code_verifier` | `authorization_code` 时 | `redirect_uri` 与授权请求一致；`code_verifier` 经 S256 后等于 `code_challenge` |
| `refresh_token` | `refresh_token` 时 | — |
| `resource` | 是 | 必须等于 `<PUBLIC_BASE_URL>/mcp` |

200（`Cache-Control: no-store`）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `access_token` | string | JWT，`aud=<PUBLIC_BASE_URL>/mcp`、`sub=<connection_id>`、`acct=<account_id>`、`scope=creative`，HS256，签名密钥按用途从网页 JWT 密钥派生 |
| `token_type` | string | `Bearer` |
| `expires_in` | integer | 3600 |
| `refresh_token` | string | 新的 refresh token；每次 refresh 都轮换 |
| `scope` | string | `creative` |

失败（400，OAuth 错误格式）：
- `invalid_grant`：授权码无效、过期、已用（已用时同时断开连接）；PKCE 不符；`redirect_uri` 不符；refresh token 无效、过期、已轮换（已轮换时断开连接）；连接已断开。宿主需要重新授权。
- `invalid_client`：`client_id` 不符。
- `invalid_target`：`resource` 不符。
- `unsupported_grant_type`：其他授权类型。

### 调用示例
```http
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&client_id=oclient_3b2c1d0e-9f8a-4b7c-8d6e-5f4a3b2c1d0e&code=Qm9n7kYp2sV1xZ4c&redirect_uri=http%3A%2F%2F127.0.0.1%3A43117%2Fcallback&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&resource=https%3A%2F%2Fcrm.example.com%2Fmcp
```
```json
{"access_token": "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJodHRwczovL2NybS5leGFtcGxlLmNvbS9tY3AifQ.sig", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "r1.8f3e2d1c0b9a8f7e6d5c4b3a2f1e0d9c", "scope": "creative"}
```
```json
{"error": "invalid_grant", "error_description": "授权码已使用"}
```

### 兼容与变更
首次定义，无已有消费者。DCR 按 MCP 规范已弃用，保留为兼容；若 C1 实测四个宿主都走 CIMD，则不提供 `/oauth/register`，元数据不返回 `registration_endpoint`（只收窄，不影响其他字段）。

## api: ADD MCP 端点

### 用途与调用
- **用途**：外部助手调用创意空间工具的唯一入口。
- **消费者**：ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页）的 MCP 客户端，经 Codex 插件、Claude 连接器、ChatGPT 应用或本机手动配置接入。
- **调用**：`<PUBLIC_BASE_URL>/mcp`，MCP streamable HTTP 传输，JSON-RPC 2.0。协议版本由官方 Go SDK 协商，服务端按 MCP 2026-07-28 实现，兼容 SDK 支持的旧版本。
- **权限**：`Authorization: Bearer <access_token>`，令牌来自 `api: OAuth 授权服务器`。校验顺序：签名 → `aud` 等于 `<PUBLIC_BASE_URL>/mcp` → 未过期 → 连接未断开。账号范围只来自令牌所属连接（ADR-001）。

### 请求
- MCP 方法：工具发现（`tools/list` 或新版协议的发现方法）与 `tools/call`；客户端声明 MCP Apps 扩展时另有 `resources/read`，只用于读取界面资源（见 `api: MCP 生成与任务卡`）。不提供 prompts、sampling；除界面资源外不提供 resources。
- `tools/call` 的 `name` 与 `arguments` 按 `api: MCP 只读工具`、`api: MCP 写入与生成工具`、`api: MCP 场景与 Skill 工具` 定义。工具清单随子项交付逐步增加，未交付的工具不出现在发现结果里。

### 成功响应
- 连接建立时（`initialize` 或新版协议的发现方法）下发服务端 `instructions`：所有宿主共用的使用说明与运行规则（design「服务端 instructions 与自检」），≤ 4000 字符。
- 工具发现：返回当前部署可用的外部工具，每个带 `name`、`title`（中文显示名，宿主的工具行和许可提示会显示它，例如「列出画布」「写提示词」「生成」「回看」）、`description`（与内置助手同一份说明加外部差异）、`inputSchema` 和 `annotations`（只读工具 `readOnlyHint: true`；`trash_assets` 等删除类工具 `destructiveHint: true`）。部署条件与内置相同：没有生成模型时不列 `set_node_prompt`、`generate_node`；没有压缩器时不列 `compress_image`。客户端声明 MCP Apps 扩展时，挂卡片的工具另带 `_meta.ui`（见两个卡片契约），其余工具带 `_meta.ui.visibility: ["model"]`，卡片不能调用它们。
- `tools/call`：
  - `content` 的第一项是文字，分两段。前一段是摘要：第一行是结论（终端宿主往往只显示前 3–4 行），随后是关键字段、Markdown 格式的画布链接和给外部助手的下一步提示，不超过 8 行。后一段是结构化结果的 JSON 文本，供不读 `structuredContent` 的宿主使用。两段之间空一行。之后是可选的图片（`ImageContent`，长边 ≤ 1024，单次 ≤ 4 张）。
  - `structuredContent` 为同一结构化结果。
  - 只给卡片用的数据（例如带签名的缩略图地址）放在 `_meta.creative`，只在客户端声明 MCP Apps 扩展时返回；外部助手的模型看不到 `_meta`。
  - 摘要由 `creativeagent` 的外部工具门面按工具生成，和工具说明一起维护；不同宿主拿到的文字相同。

### 失败响应
| 信号 | 触发条件 | 调用方处理 |
|---|---|---|
| HTTP 401 + `WWW-Authenticate: Bearer resource_metadata="<PUBLIC_BASE_URL>/.well-known/oauth-protected-resource/mcp", scope="creative"` | 缺令牌、签名或受众不符、过期、连接已断开 | 宿主先 refresh，失败则重新授权 |
| HTTP 403（同上 `WWW-Authenticate`，`error="insufficient_scope"`） | 以后的细粒度授权；当前不会出现 | — |
| JSON-RPC 错误 `-32602` | 工具不存在、参数不符合 `inputSchema` | 修正调用 |
| 工具结果 `isError: true`，文字内容为 `{"code": "<领域错误码>", "message": "<中文说明>", "retry_after_s": <可选>}` | 领域拒绝：`not_found`、`validation_failed`、`conflict`、`forbidden`（账号无创意能力）、`budget_exhausted`（生成额度不足）、`rate_limited`（连接限流，带 `retry_after_s`）、以及各工具已有的错误码 | 按错误码处理；与内置助手看到的错误一致 |

### 交互规则
- **限流**：每个连接每分钟工具调用 ≤ 120 次，同时等待中的任务句柄 ≤ 8 个（初始值，上线后调整，不作承诺）。
- **超时**：单次 `tools/call` 服务端最长处理 45 秒；会超过的工作改为任务句柄（见 `api: MCP 写入与生成工具` 的 `wait_tasks`）。
- **重复调用**：MCP 没有幂等键。宿主重试同一调用可能重复执行写工具；工具说明提示「写入失败后先读再决定是否重试」。领域层已有的版本检查（`draft_revision`、场景 `base_revision` 等）照常拦截过时写入。
- **活动状态**：成功通过鉴权的调用节流更新连接的 `last_used_at`（≤ 1 次/分钟）。

### 调用示例
```http
POST /mcp
Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJodHRwczovL2NybS5leGFtcGxlLmNvbS9tY3AifQ.sig
Content-Type: application/json
Accept: application/json, text/event-stream

{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": {"name": "list_canvases", "arguments": {"limit": 2}}}
```
```json
{"jsonrpc": "2.0", "id": 7, "result": {"content": [{"type": "text", "text": "最近编辑的 1 张画布：\n1. [教堂中殿分镜](https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f) · 10 月 21 日编辑 · `ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f`\n下一步：用 read_canvas(canvas_id) 读取内容。\n\n{\"items\":[{\"canvas_id\":\"ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f\",\"title\":\"教堂中殿分镜\",\"updated_at\":\"2026-10-21T08:10:00Z\",\"canvas_url\":\"https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f\"}],\"next_cursor\":\"\"}"}], "structuredContent": {"items": [{"canvas_id": "ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f", "title": "教堂中殿分镜", "updated_at": "2026-10-21T08:10:00Z", "canvas_url": "https://crm.example.com/creative/canvases/ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"}], "next_cursor": ""}}}
```
```http
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer resource_metadata="https://crm.example.com/.well-known/oauth-protected-resource/mcp", scope="creative"
```

### 兼容与变更
首次定义。工具的增加是兼容扩展；工具 schema 跟随内置工具的 `ToolEntry.Version`，破坏性变化（删字段、改语义）时在工具说明里写明版本，并在本契约记录。

## api: ADD 外部助手连接列表与断开

### 用途与调用
- **用途**：接入页列出和断开外部助手连接，并拿到接入文字要用的 MCP 地址；创意空间顶栏显示「外部助手已连接」。
- **消费者**：前端「外部助手」接入页、创意空间顶栏。
- **调用**：`GET /api/v1/creative/agent-connections`；`POST /api/v1/creative/agent-connections/{id}/revoke`。
- **格式**：`application/json`，错误沿用 `ErrorEnvelope`。
- **权限**：网页端 Bearer；列表要 `creative_read`，断开要 `manual_write`；只能看到和断开本账号的连接。

### 请求
列表 query：

| 字段 | 类型 | 必传 | 缺省 | 说明 |
|---|---|---|---|---|
| `active` | boolean | 否 | 不过滤 | `true` 只返回有效且 5 分钟内用过的连接（顶栏用） |
| `include_revoked` | boolean | 否 | `false` | `true` 时一并返回已断开的连接 |

不分页：一个账号的连接数量很小；返回最近创建的 50 个，超出部分不返回（接入页提示「只显示最近 50 个」）。

断开：path `id`，无请求体。

### 成功响应
列表 200：`{mcp_url, items:[…]}`。`mcp_url`（string，必返）为 `<PUBLIC_BASE_URL>/mcp`，接入页据此生成各宿主的接入文字，不在前端写死。`items` 按 `created_at` 倒序：

| 字段 | 类型 | 必返 | 可为 null | 说明 |
|---|---|---|---|---|
| `id` | string | 是 | 否 | `cxcon_…` |
| `display_name` | string | 是 | 否 | 「Claude」「Claude Code」「Codex」「ChatGPT」或客户端名 |
| `host_kind` | string | 是 | 否 | `claude` / `claude_code` / `codex` / `chatgpt` / `other` |
| `created_at` | string(date-time) | 是 | 否 | 授权时间 |
| `last_used_at` | string(date-time) | 是 | 是 | 从未调用过工具时为 null |
| `active` | boolean | 是 | 否 | 有效且 `last_used_at` 在 5 分钟内 |
| `revoked_at` | string(date-time) | 是 | 是 | 有效时为 null |

断开 200：返回该连接的上述对象（`revoked_at` 非空）。重复断开已断开的连接返回 200 与原对象，不改变 `revoked_at`。

### 失败响应
| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | query 参数不是布尔值 | 修正 |
| 404 | `not_found` | 连接不存在或不属于本账号 | 刷新列表 |
| 403 | 现有 `CodeForbidden` | 账号没有创意空间能力 | 沿用现有处理 |

### 交互规则
- 断开的副作用见 `data: 外部助手连接与 OAuth` 的「断开」，在一个事务里完成；提交后发账号失效通知，顶栏和接入页重读列表。
- 连接由不活动变为活动时也发账号失效通知；变为不活动不通知，前端按 `last_used_at + 5 分钟` 自行到期隐藏。

### 调用示例
```http
GET /api/v1/creative/agent-connections?active=true
Authorization: Bearer <token>
```
```json
{"mcp_url": "https://crm.example.com/mcp", "items": [{"id": "cxcon_7a6b5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d", "display_name": "Claude Code", "host_kind": "claude_code", "created_at": "2026-10-21T07:58:12Z", "last_used_at": "2026-10-21T08:10:03Z", "active": true, "revoked_at": null}]}
```
```http
POST /api/v1/creative/agent-connections/cxcon_0000aaaa-1111-4bbb-8ccc-2222dddd3333/revoke
Authorization: Bearer <token>
```
```json
{"error": {"code": "not_found", "message": "连接不存在"}}
```

### 兼容与变更
首次定义，无已有消费者。

## api: MODIFY 列出文档修订

- **用途**：历史版本列表、「本轮改动」清单，以及告诉助手「自你上次写入后发生了什么」。
- **调用**：`GET /api/v1/creative/documents/{id}/revisions`。
- **权限**：`creative_read`。回收站中的文档也可以读。
- **请求（query）**：
  - `run_id`（可选，只列该运行写入的修订）
  - `external_connection_id`（可选，只列该外部助手连接写入的修订；与 `run_id` 不能同时使用）
  - `since_revision`（可选，CreativeRevision，只列修订号大于它的修订；可与 `run_id` 同时使用）
  - `limit`（1–100，缺省 50）
  - `cursor`（上一页的 `next_cursor`，绑定文档、`run_id`、`external_connection_id` 与 `since_revision`）
- **成功响应**：200，`{items:[{revision, actor_kind, run_id, external_connection_id, actor_display_name, restored_from, changed_units, annotated_units, removed_units, title, created_at}], next_cursor}`。按 `revision` 倒序；`actor_kind` 取值 `photographer` / `agent` / `external_agent` / `system`；`run_id`、`external_connection_id` 和 `restored_from` 可为 null；`actor_display_name` 只在 `external_agent` 时非 null，为连接的界面署名（「Claude」「Claude Code」「Codex」「ChatGPT」或客户端名，连接已断开也照常返回），历史列表据此署名分组；不含快照内容。`annotated_units` 是这个修订只做了标注、修订号没变的单元；`removed_units` 是这个修订删除的单元，删除时 `changed_units` 只含集合单元，具体删了什么要看它；整份恢复的 `removed_units` 为空，由 `changed_units = ["*"]` 表示全部重置（这两个字段实施时补充，「本轮改动」清单和告知助手需要区分改动、确认和删除）。
- **失败响应**：
  - 400 `validation_failed`：参数或游标非法；`run_id` 与 `external_connection_id` 同时给出。
  - 404 `not_found`：文档不存在或不属于当前账号。
- **示例**：
```http
GET /api/v1/creative/documents/cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b/revisions?limit=2
```
```json
{"items": [
  {"revision": "4", "actor_kind": "photographer", "run_id": null, "external_connection_id": null, "actor_display_name": null, "restored_from": null, "changed_units": ["meta", "body"], "title": "灯光参考说明", "created_at": "2026-10-16T09:20:01Z"},
  {"revision": "3", "actor_kind": "agent", "run_id": "crun_6f5e4d3c-2b1a-4f9e-8d7c-6b5a4f3e2d1c", "external_connection_id": null, "actor_display_name": null, "restored_from": null, "changed_units": ["body"], "title": "参考说明", "created_at": "2026-10-16T09:01:12Z"}],
 "next_cursor": "eyJyIjoiMyJ9"}
```
```json
{"error": {"code": "not_found", "message": "文档不存在"}}
```
- **兼容**：兼容扩展（epic `creative-mcp-server` C3）：新增可选 query 与两个响应字段，原有字段与缺省行为不变；`actor_kind` 新增取值 `external_agent`，现有前端遇到未知取值时须按「其他写入者」显示（C3 同时更新 `frontend/src/creative-scene/history.ts`）。


## api: ADD MCP 只读工具

### 用途与调用
- **用途**：外部助手自检、找到画布、读取画布与引用素材、查找和浏览资产库。
- **调用**：经 `api: MCP 端点` 的 `tools/call`。
- **权限**：连接有效；账号 `creative_read`。授权即账号级 consent（design「授权与 consent」）：本账号未进回收站的画布、内容和资产都可读，不再按对话判定可读范围。

### 工具
| 工具 | 来源 | 与内置工具的差异 |
|---|---|---|
| `get_capabilities` | 外部专用 | 见下 |
| `list_canvases` | 外部专用 | 见下 |
| `read_canvas` | 内置同名 | 新增必传 `canvas_id`（string，`list_canvases` 返回的 ID）；文字节点全部可读；结果另带 `canvas_url` |
| `read_asset` | 内置同名 | 入参不变；可读范围为整个账号；图片结果以 `ImageContent` 返回，长边 ≤ 1024；账号模型能力不再参与判定（外部助手自己决定能否看图） |
| `search_assets` | 内置同名 | 入参与结果不变；不需要资产库 consent |
| `read_library` | 内置同名 | 入参与结果不变；不需要资产库 consent |

#### get_capabilities
- 入参：无。
- 结果：
  - `account`：`{display_name}`，授权账号的显示名。
  - `connection`：`{id, display_name, host_kind}`。
  - `generation`：`{models:[{key, display_name, node_type, parameters_summary}], budget}`。`budget` 为 `{remaining, unit, period_end}`，沿用网关的生成预算；积分落地后 `unit` 改为积分，字段不变。部署没有生成模型时 `models` 为空数组。
  - `workspace_open`：boolean，当前有没有打开的创意空间页面。这是账号级实时通道是否有活动订阅的近似判断，只作提示，不作保证。
  - `skills`：`{count}`，可用 Skill 数量（平台加账号自己的）。
  - `hints`：string[]，给外部助手的简短提醒，例如没有打开页面时提示场景任务需要页面。
- 不返回部署配置、版本号或内部地址。
- 错误：只有 `api: MCP 端点` 的通用错误。

#### list_canvases
- 入参：`query`（string，可选，按标题包含匹配，≤ 100 字符）、`limit`（integer，可选，1–50，缺省 20）、`cursor`（string，可选，上一页的 `next_cursor`）。
- 结果：`{items:[{canvas_id, title, updated_at, canvas_url}], next_cursor}`，按 `updated_at` 倒序；`canvas_url` 是创意空间里打开该画布的链接；`next_cursor` 为空字符串表示末页。不含已归档画布。
- 错误：`validation_failed`（参数越界、游标非法）。

### 失败响应
各工具沿用内置工具的错误码；另有 `api: MCP 端点` 的通用错误。`read_canvas` 的 `canvas_id` 不存在或不属于本账号时为 `not_found`。

### 调用示例
```json
{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": {"name": "read_canvas", "arguments": {"canvas_id": "ccvs_1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"}}}
```
成功时结构化结果与内置 `read_canvas` 相同。失败示例：
```json
{"jsonrpc": "2.0", "id": 8, "result": {"isError": true, "content": [{"type": "text", "text": "{\"code\":\"not_found\",\"message\":\"画布不存在\"}"}]}}
```

### 兼容与变更
首次定义。内置工具的行为不变。

## api: ADD MCP 写入与生成工具

### 用途与调用
- **用途**：外部助手修改画布、写提示词与生成、压缩图片、整理资产库，并等待长任务。
- **调用**：经 `api: MCP 端点` 的 `tools/call`。
- **权限**：连接有效；账号 `agent_start`；`generate_node` 另需 `node_generate` 并经网关生成预算（与摄影师相同的扣费入口，积分落地后照此扣积分）。

### 工具
| 工具 | 与内置工具的差异 |
|---|---|
| `edit_canvas` | 新增必传 `canvas_id`；写入者为外部连接（画布署名见 roadmap「与其他工作的衔接」） |
| `set_node_prompt` | 新增必传 `canvas_id` |
| `generate_node` | 新增必传 `canvas_id`；新增可选 `dry_run`（见下）；40 秒内未完成时返回任务句柄（见下）；生成执行记 `caller_kind = external_agent` 与连接 ID |
| `compress_image` | 新增必传 `canvas_id` |
| `save_to_library` | 新增必传 `canvas_id` |
| `organize_assets`、`manage_library`、`trash_assets`、`restore_assets`、`read_library_result` | 入参与结果不变；资产库日志归属外部连接；不需要资产库 consent |
| `wait_tasks` | 外部专用，见下 |

#### 画布交接链接
`edit_canvas`、`set_node_prompt`、`generate_node`、`compress_image`、`save_to_library` 的成功结果另带：
- `canvas_url`（string）：打开该画布的链接。
- `focus`（string[]）：本次改动涉及的节点 ID；带在 `canvas_url` 的查询参数里，打开后定位到这些节点。

外部助手应把链接交给创作者打开，或在能内置浏览器的宿主里自己打开；打开与否不影响写入。图形界面宿主里，`generate_node`、`wait_tasks` 的结果另以生成与任务卡显示（`api: MCP 生成与任务卡`）。

#### generate_node 的 dry_run
- `dry_run`（boolean，可选，缺省 `false`）。
- 为 `true` 时走完生成前的全部校验（节点、模型、参数、参考、`draft_revision`、额度），不创建生成执行、不扣费。返回 `{dry_run: true, model, node_type, parameters, references:[{node_id, content_revision_id}], estimated_cost:{amount, unit, is_estimate: true}, budget:{remaining, unit}}`：`parameters` 是将要使用的生成参数（数量、画幅等，键与内置 `generate_node` 相同），`budget` 与 `get_capabilities` 的生成预算一致。
- 校验不通过时返回与真实生成相同的错误码。
- 是否预检由外部助手决定；工具说明和 `instructions` 不要求每次预检，创作者要求全自动执行时可以不做。

#### 任务句柄
40 秒内未完成的工具返回结构化结果 `{status: "pending", task_id, kind, message}`：`task_id` 是生成执行 ID 或场景任务 ID（`csjob_…`），`kind` 为 `generation` / `scene_job`，`message` 是给外部助手的中文说明（例如「生成中，用 wait_tasks 继续等待」）。

#### wait_tasks
- 入参：`task_ids`（string[]，必传，1–8 个，只能是本连接创建的句柄）、`timeout_s`（integer，可选，1–45，缺省 40）。
- 行为：任一句柄到达终态或等待超时即返回，返回全部句柄的当前状态。
- 结果：`{tasks:[{task_id, kind, status, result, error, open_url}]}`：
  - `status`：`pending` / `waiting_for_page`（场景任务排队 15 秒以上仍未被页面认领，C3 起）/ `running` / `succeeded` / `failed` / `cancelled` / `expired`。
  - `result`：成功时与对应工具同步完成时的结果相同（生成为节点与产物；图片同时以 `ImageContent` 返回）；其他状态为 null。
  - `error`：失败时为 `{code, message}`，否则为 null。
  - `open_url`：`waiting_for_page` 时为要打开的创意空间链接，否则为 null。
- 错误：`validation_failed`（数量越界）、`not_found`（句柄不存在或不属于本连接）。

### 交互规则
- **副作用**：与内置同名工具相同；按工具已有的原子性规则，不因外部路径改变。
- **重复调用**：见 `api: MCP 端点`；`generate_node` 依靠 `draft_revision` 拦截重复生成同一版提示词。
- **异步**：句柄的生命周期就是领域对象的生命周期（生成执行、场景任务），不另存句柄表；连接断开后它的句柄查询返回 401（连接级）。

### 调用示例
```json
{"jsonrpc": "2.0", "id": 12, "method": "tools/call", "params": {"name": "wait_tasks", "arguments": {"task_ids": ["cwexec_4d3c2b1a-0f9e-4d8c-8b7a-6f5e4d3c2b1a"], "timeout_s": 40}}}
```
```json
{"jsonrpc": "2.0", "id": 12, "result": {"content": [{"type": "text", "text": "{\"tasks\":[{\"task_id\":\"cwexec_4d3c2b1a-0f9e-4d8c-8b7a-6f5e4d3c2b1a\",\"kind\":\"generation\",\"status\":\"pending\",\"result\":null,\"error\":null,\"open_url\":null}]}"}], "structuredContent": {"tasks": [{"task_id": "cwexec_4d3c2b1a-0f9e-4d8c-8b7a-6f5e4d3c2b1a", "kind": "generation", "status": "pending", "result": null, "error": null, "open_url": null}]}}}
```
```json
{"jsonrpc": "2.0", "id": 13, "result": {"isError": true, "content": [{"type": "text", "text": "{\"code\":\"budget_exhausted\",\"message\":\"本月生成额度不足\"}"}]}}
```

### 兼容与变更
首次定义。内置工具行为不变；`creative_node_executions.caller_kind` 与资产库日志的扩展见 design「数据与存储设计」，只加取值、不改存量。

## api: ADD MCP 场景与 Skill 工具

### 用途与调用
- **用途**：外部助手读写 3D 场景、写模型、回看、导出分镜，查找和加载 Skill。
- **调用**：经 `api: MCP 端点` 的 `tools/call`。
- **权限**：连接有效；读工具 `creative_read`，写工具 `agent_start`。

### 工具
| 工具 | 与内置工具的差异 |
|---|---|
| `read_scene` | 入参不变；「自上次写入后的变化」以本连接最近一次写入该场景的修订为起点 |
| `edit_scene` | 入参不变；写入者为外部连接；成功结果另带 `canvas_url`（场景所在画布，场景在多个画布上时取最近更新的）和 `focus`（改动的场景物件 ID，打开后进入场景并选中）；`revert_run` 操作对外部连接的含义由 C3 设计决定（design U5），决定前不对外部开放该操作 |
| `write_model` | 入参不变；任务归属外部连接；40 秒内未完成返回任务句柄 |
| `render_view` | 入参不变；不再按模型图像能力判定是否可用；截图以 `ImageContent` 返回；40 秒内未完成返回任务句柄 |
| `export_shots` | 新增必传 `canvas_id`（导出图放到哪个画布，场景必须在该画布上）；40 秒内未完成返回任务句柄 |
| `search_skills` | 入参与结果不变 |
| `load_skill` | 新增可选 `skill_version_id`；缺省时加载该 Skill 当前可用版本；返回正文与资源目录（D3：正文经已授权连接下发）；不记录到运行 |
| `read_skill_resource` | 新增必传 `skill_version_id`（`load_skill` 返回的版本），因为没有运行固定版本 |

场景任务没有被页面认领时，`wait_tasks` 返回 `waiting_for_page` 与 `open_url`（见 `api: MCP 写入与生成工具`）。图形界面宿主里，`render_view`、`export_shots` 的结果另以镜头卡显示（`api: MCP 镜头卡`）。

### 失败响应
沿用内置工具的错误码。另有：已停用的 Skill 版本 `load_skill` 返回 `not_found`（与发现结果一致）；`export_shots` 的场景不在 `canvas_id` 画布上时 `validation_failed`。

### 调用示例
```json
{"jsonrpc": "2.0", "id": 21, "method": "tools/call", "params": {"name": "render_view", "arguments": {"document_id": "cwdoc_0f8e2a6c-1d3b-4e5f-9a7b-2c4d6e8f0a1b", "preset": "overview", "mode": "clay"}}}
```
```json
{"jsonrpc": "2.0", "id": 21, "result": {"content": [{"type": "text", "text": "{\"status\":\"pending\",\"task_id\":\"csjob_9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b\",\"kind\":\"scene_job\",\"message\":\"等待创意空间页面渲染，用 wait_tasks 继续等待\"}"}], "structuredContent": {"status": "pending", "task_id": "csjob_9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b", "kind": "scene_job", "message": "等待创意空间页面渲染，用 wait_tasks 继续等待"}}}
```

### 兼容与变更
首次定义。内置工具行为不变。`catalog.go` 中「Skill 正文不离开服务端」的约定改为「不经浏览器接口暴露；经已授权的外部助手连接可读」（D3）。

## api: ADD MCP 生成与任务卡

### 用途与调用
- **用途**：在支持卡片的图形界面宿主里，把 `generate_node`（含预检）和 `wait_tasks` 的结果显示成内嵌卡片：预检后由创作者确认、进度、结果图、等待页面。本契约同时定义两张卡片共用的界面资源规则，`api: MCP 镜头卡` 引用这些规则。
- **消费者**：MCP Apps 宿主（首批为 ChatGPT、Claude 桌面端和网页），以及卡片本身。Codex CLI 与 Claude Code 不渲染卡片，只用 `content` 文字。
- **调用**：宿主经 `api: MCP 端点` 的 `resources/read` 读取 `ui://creative/tasks/v1`，在沙箱 iframe 里渲染；卡片经宿主发起 `tools/call`，走同一个连接和令牌。
- **权限**：与 `api: MCP 端点` 相同。卡片发起的调用和模型发起的调用在服务端无法区分，准入、限流、计费完全相同。

### 协商
- 客户端声明 MCP Apps 扩展 `io.modelcontextprotocol/ui`（并支持 `text/html;profile=mcp-app`）时启用。
- 没有声明时：工具发现不带 `_meta.ui`，工具结果不带 `_meta.creative`，`resources/read` 读界面资源返回资源不存在。工具行为与文字结果完全不变。

### 界面资源（两张卡共用）
| 字段 | 值 |
|---|---|
| `uri` | `ui://creative/tasks/v1`（镜头卡为 `ui://creative/shots/v1`）。URI 是宿主的缓存键，卡片与结果数据不兼容的改动发布新版本号，旧版本保留到新版本上线 30 天后 |
| `mimeType` | `text/html;profile=mcp-app` |
| `text` | 单个 HTML5 文档，样式与脚本全部内联，不加载外部脚本、样式和字体 |
| `_meta.ui.csp.resourceDomains` | 卡片要显示的图片所在的对象存储或 CDN 域名，按部署配置生成 |
| `_meta.ui.csp.connectDomains`、`frameDomains`、`baseUriDomains` | 空数组：卡片不直接访问任何网络接口，动作都经宿主调用工具；不嵌套 iframe |
| `_meta.ui.permissions` | 不申请（不用摄像头、麦克风、定位、剪贴板） |
| `_meta.ui.domain` | Claude：`<PUBLIC_BASE_URL>/mcp` 的 SHA-256 前 32 位十六进制 + `.claudemcpcontent.com`；其他宿主按各自格式给出或省略（U11） |
| `_meta.ui.prefersBorder` | `true` |

卡片的展示规则：
- 颜色、字体、圆角只用宿主下发的标准 CSS 变量，并为每个变量写默认值；跟随宿主明暗；照片之外不用品牌色铺底。
- 行内卡片最多两个动作，放在底部；不嵌套滚动；不用菜单和弹出层。
- 可以请求全屏，只用于放大查看结果图，不做编辑。深度编辑一律回到创意空间页面（`canvas_url`）。
- 加载中用骨架屏；「等待页面」不放骨架屏，免得像在运行。

### 工具元数据
| 工具 | `_meta.ui.resourceUri` | `_meta.ui.visibility` |
|---|---|---|
| `generate_node` | `ui://creative/tasks/v1` | `["model", "app"]` |
| `wait_tasks` | `ui://creative/tasks/v1` | `["model", "app"]` |
| `save_to_library` | 无 | `["model", "app"]` |
| `render_view`、`export_shots` | `ui://creative/shots/v1`（`api: MCP 镜头卡`） | `["model"]` |
| 其他外部工具 | 无 | `["model"]` |

### 卡片数据 `_meta.creative`
`generate_node`、`wait_tasks` 的结果另带（只在协商启用时）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `canvas_url` | string | 是 | 带 `focus` 的画布链接，与结构化结果里的相同 |
| `references` | `[{node_id, title, thumbnail_url}]` | `dry_run` 时 | 参考的缩略图，签名地址 |
| `tasks` | `{<task_id>: {results, open_url}}` | 有任务句柄时 | `results` 为 `[{url, width, height, label}]`，成功时是结果图（签名地址，长边 ≤ 1024），否则为空数组；`open_url` 在 `waiting_for_page` 时为要打开的创意空间链接，否则为 null |

签名地址的有效期至少 1 小时（U11）。卡片的其余数据（模型、数量、预计扣费、任务状态）读 `structuredContent`，不在 `_meta` 里重复。

### 卡片状态与动作
| 结构化结果 | 卡片显示 | 动作 |
|---|---|---|
| `dry_run: true` | 预检：模型、数量与比例、参考缩略图、预计扣费与剩余额度（≤ 5 个数据点） | 「生成 · N 点」：用工具输入的同一组参数调用 `generate_node`，去掉 `dry_run`；「不生成」 |
| `pending` / `running` | 进行中：按比例的骨架屏、进度 | 「在画布中查看进度」 |
| `succeeded` | 结果图，按原比例；可全屏查看 | 「在画布中查看」；「存入资产库」：调用 `save_to_library` 保存这些产物 |
| `failed` / `cancelled` / `expired` | 错误码对应的中文说明 | 「在画布中查看」 |
| `waiting_for_page`（场景任务） | 「回看要由一个打开的创意空间页面来渲染」 | 「打开创意空间」：`ui/open-link` 打开 `open_url` |

- 非终态时，卡片用 `wait_tasks` 长轮询（`timeout_s` ≤ 20），收到结果立即发下一次；卡片被宿主销毁时停止。轮询计入连接限流；遇到 `rate_limited` 按 `retry_after_s` 延后。
- 卡片发起的调用失败时，卡片显示错误说明并回到原状态（例如预检卡恢复可点），不自动重试写操作。

### 告诉外部助手
创作者在卡片里做的决定，卡片用 `ui/update-model-context` 交给外部助手。规范规定每次更新覆盖上一次，宿主也可能等到创作者的下一条消息才交给模型，所以卡片维护一份累计的摘要，每次发送完整内容：

| 时机 | 摘要里追加的一行 |
|---|---|
| 点「生成」且提交成功 | 创作者在预检卡里确认了生成：已提交任务 `<task_id>`（N 张，预计 M 点）。不要重复提交；进度和结果显示在卡片里。 |
| 点「不生成」 | 创作者在预检卡里选择了不生成，没有扣点。 |
| 任务到达终态 | 卡片显示：任务 `<task_id>`（`<节点标题>`）已完成 / 失败（错误说明）。结果创作者已在卡片里看到，不必再调用 `wait_tasks`。 |
| 存入资产库成功 | 创作者在卡片里把 N 张结果存入了资产库。 |

这一轮外部助手不会因为卡片动作而发言。服务端 `instructions` 写明：收到这类上下文时以它为准。

### 失败响应
| 信号 | 触发条件 | 调用方处理 |
|---|---|---|
| JSON-RPC 错误 `-32002`（资源不存在） | 未声明扩展时读界面资源；URI 版本不存在 | 宿主不渲染卡片，按文字结果显示 |
| 工具结果 `isError: true` | 卡片调用的工具被领域拒绝（同 `api: MCP 端点`） | 卡片显示说明并回到原状态 |
| 宿主拒绝 `ui/open-link` | 创作者或宿主策略拒绝打开链接 | 卡片保留按钮，不重试 |

### 调用示例
```json
{"jsonrpc": "2.0", "id": 3, "method": "resources/read", "params": {"uri": "ui://creative/tasks/v1"}}
```
```json
{"jsonrpc": "2.0", "id": 3, "result": {"contents": [{"uri": "ui://creative/tasks/v1", "mimeType": "text/html;profile=mcp-app", "text": "<!doctype html><html lang=\"zh-CN\">…</html>", "_meta": {"ui": {"csp": {"resourceDomains": ["https://media.crm.example.com"], "connectDomains": [], "frameDomains": [], "baseUriDomains": []}, "domain": "3f1c9a0e7b2d4c6a8e0f1b3d5c7e9a2b.claudemcpcontent.com", "prefersBorder": true}}}]}}
```
工具发现里的一项：
```json
{"name": "generate_node", "title": "生成", "description": "…", "inputSchema": {"type": "object"}, "_meta": {"ui": {"resourceUri": "ui://creative/tasks/v1", "visibility": ["model", "app"]}}}
```
预检结果（卡片部分）：
```json
{"structuredContent": {"dry_run": true, "model": "gpt-image-2.5", "node_type": "image", "parameters": {"count": 2, "aspect_ratio": "3:4"}, "references": [{"node_id": "cnode_2", "content_revision_id": "ccrev_9"}], "estimated_cost": {"amount": 40, "unit": "points", "is_estimate": true}, "budget": {"remaining": 1252, "unit": "points"}}, "_meta": {"creative": {"canvas_url": "https://crm.example.com/creative/canvases/ccvs_1c2d?focus=cnode_5", "references": [{"node_id": "cnode_2", "title": "参考 · 海风", "thumbnail_url": "https://media.crm.example.com/t/…?sig=…"}]}}}
```

### 兼容与变更
首次定义，资源版本 v1。不声明扩展的客户端完全不受影响。卡片的视觉与交互依据 `docs/prototypes/creative-mcp/`（owner 2026-10-22 走查通过）。

## api: ADD MCP 镜头卡

### 用途与调用
- **用途**：在支持卡片的图形界面宿主里，把 `render_view` 的回看截图和 `export_shots` 导出的分镜显示成卡片；任务排队等页面时给出「打开创意空间」。
- **消费者、调用、权限、协商、资源规则**：同 `api: MCP 生成与任务卡`，资源为 `ui://creative/shots/v1`。工具元数据见该契约的「工具元数据」表。

### 卡片数据 `_meta.creative`
`render_view`、`export_shots` 的结果另带（只在协商启用时）：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `canvas_url` | string | 是 | 场景或导出目标画布的链接 |
| `shots` | `[{url, width, height, label, meta, canvas_url}]` | 成功时 | `url` 为截图（签名地址，长边 ≤ 1024）；`label` 如「镜头 02 · 逆光长裙」；`meta` 为最多 2 行的机位说明（焦距、机位高度等）；`canvas_url` 聚焦到对应的导出节点 |
| `tasks` | 同 `api: MCP 生成与任务卡` | 返回任务句柄时 | 排队、渲染中、等待页面时使用 |

### 卡片状态与动作
| 情况 | 卡片显示 | 动作 |
|---|---|---|
| 任务未完成（`pending` / `running` / `waiting_for_page`） | 同生成与任务卡 | 同生成与任务卡 |
| `render_view` 成功 | 单张截图（横图按原比例，行内最宽 560px）、机位说明；可全屏 | 「在画布中查看」 |
| `export_shots` 成功 | 横向轮播，3–8 项；每项一张图、标题、最多 2 行机位说明；超过 8 项只显示前 8 项并注明总数 | 每项一个「在画布中查看」 |

- 任务到达终态时，卡片按 `api: MCP 生成与任务卡` 的「告诉外部助手」规则追加一行：「卡片显示：回看 / 导出已完成（N 个镜头）。」
- 镜头卡不发起写操作。

### 失败响应
同 `api: MCP 生成与任务卡`。

### 调用示例
`export_shots` 成功结果里的卡片部分（`structuredContent` 沿用内置 `export_shots`）：
```json
{"_meta": {"creative": {"canvas_url": "https://crm.example.com/creative/canvases/ccvs_1c2d", "shots": [{"url": "https://media.crm.example.com/s/…?sig=…", "width": 1024, "height": 683, "label": "镜头 01 · 礁石远景", "meta": ["24mm · 机位高 1.6m"], "canvas_url": "https://crm.example.com/creative/canvases/ccvs_1c2d?focus=cnode_31"}]}}}
```

### 兼容与变更
首次定义，资源版本 v1，随 C3 交付。
