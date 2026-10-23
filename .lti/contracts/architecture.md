# 项目全局架构

记录当前已交付系统。Epic 的目标架构保留在 Epic 设计中，随 Change 实际交付分批更新这里。
已有 canonical 架构文档可通过 schema reference 接入；不要同时维护两份相同正文。

<!-- 使用独立 ## 章节描述：系统边界、模块职责与依赖方向、关键数据流、部署、非功能约束和核心技术选择。
新项目可以先只有最小架构；未实现的远期方案不写成现状。通过 architecture delta 随交付演进。
-->

## 通用文档底座

- 「画布节点背后的独立文档」由 `backend/internal/creativedocument` 统一承载：文档行、修订、引用、工作会话指针、回收站、列表、命令外壳、类型注册表。
- 具体文档类型（`internal.document` 内置；`photo.shoot_plan`、`scene` 由各自功能实现）通过类型端口注册，只负责载荷、单元和操作语义。
- 依赖方向：`httpapi` / `creativecanvas` / 各类型包 → `creativedocument` → `creativecontent` / `platform/store`。`creativedocument` 不依赖 `creativecanvas`，也不依赖任何具体类型。
- 文档与画布节点是两个聚合：只有 `add_node` 新建时同事务创建，之后节点只持有 `document_id`。删除节点不删除文档；彻底删除文档后，节点读取为不可用。
- 实时：文档头的变化经已有的 `creative_documents_notify` 触发器走账号级失效通道，不新增 SSE。

## 场景模块

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
- 渲染统一用 three.js 的 `WebGPURenderer`，WebGPU 不可用时用它的 WebGL2 后端；着色全部用 TSL（见 `patterns: 场景着色写法`）。视口、截图、导出、模型缩略图和资产 3D 预览都经同一个渲染器创建入口获得渲染器；视口、截图和导出经同一条渲染管线（`SceneRenderer`）出图。
- 环境类效果（物理天空，以及之后在其上增加的环境效果）的换算与绘制只在渲染端：前端 `frontend/src/creative-scene/world/` 下的天文计算（`astro`）、物理大气（`atmosphere`）和曝光（`exposure`）是不依赖编辑器外壳的模块，画布页按需加载渲染端时同样可用。服务端按数据契约校验字段（时区名用二进制内嵌的 `time/tzdata` 校验），不参与画面上的天体换算；渲染指纹照常由服务端从快照算出。服务端另为助手算「太阳参考」（当前太阳的高度和方位、日出日落、正午、黄金时刻和蓝调时刻，NOAA 公式），只放在助手读写场景的结果里，不存储，不进指纹（修订 v2）。
- 曝光属于观看者：物理天空下，每次出图由场景摄像机的手动曝光（ISO、快门、光圈）或默认摄像机（视口自由视角、`render_view` 的临时视点）的默认曝光值求出曝光，在送进 GPU 前乘到光源上（预曝光）；天空和环境贴图按只取决于环境的相对单位存储，再乘以相对系数。不做自动曝光。其他天空模式沿用场景级的 `render.exposure_ev`。白模不读取环境和灯光，用固定的工作室光。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图。摄影师手动导出时调 `POST /creative/documents/{id}/scene-exports`；助手导出（`export_shots`）把图片清单随完成任务提交，在完成事务里放置。两条路径走同一个放置流程：画布命令层建图片节点，写入 `based_on`。读画布时经上述端口为导出图节点计算过时状态。
- 环境到画面：`environment` 单元与观看者的曝光 → 渲染端求天体方向、大气 LUT（参数变化时重算）和曝光 → 天空背景、太阳光、天空光照及其上的环境类效果 → 色调映射。所有环境类效果读同一份天体方向、大气 LUT 和曝光。

## 外部助手接入

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
