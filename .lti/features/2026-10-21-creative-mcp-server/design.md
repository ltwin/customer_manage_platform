# Epic 技术方案

## 设计上下文

需求见 proposal.md，探索共识与代码事实见 `discovery/brainstorm.md`（D1–D9），竞品 LibTV 的接入方式见 `discovery/research/libtv-plugin.md`。采用方案一句话概括：在现有 Go server 上新增一个只服务外部助手的 OAuth 授权服务器和一个远程 MCP 端点；把内置助手工具里「运行」的身份抽象成「助手主体」，让「外部助手连接」与运行并列；MCP 端点只做协议转换，工具语义和说明仍由 `creativeagent` 唯一维护。四个首批宿主共用这一个端点，差别只在接入方式（Codex 插件、Claude 连接器、ChatGPT 应用）。所有宿主共用的使用说明是服务端 `instructions`，图形界面宿主不读插件 Skill。所有宿主都靠工具结果的文字工作；ChatGPT 和 Claude 桌面端、网页另外按 MCP Apps 标准把生成和场景结果显示成内嵌卡片（D10–D13，调研见 `discovery/research/host-ui-surfaces.md`，原型见 `docs/prototypes/creative-mcp/`，owner 2026-10-22 走查通过）。

调研依据（2026-10-21）：
- 内置工具与运行的耦合有 6 处：admit（claim/lease）、consent 与可读判定、文档写入者必须带 `run_id`、场景任务必须带 `run_id` + `tool_call_id` 并经 Waiter 唤醒运行、资产库日志 `run_id`/`conversation_id` 非空、Skill 版本固定在运行上（`creativeagent/execute.go:650`、`context.go:45`、迁移 `0072`、`creativescene/jobs.go:367`、迁移 `0064`、`skill_runtime.go:152`）。领域命令本身大多只要 `AccountScope` 加参数（`creativecanvas.ApplyCommandsAs`、`creativegeneration` 的 `Admit` 可选、`creativelibrary.*`）。
- 资源锁 `resources` 只用于同一回答内多个调用的排序（`batch.go:30`），`MaxImpact` 只用于目录展示，连续失败计数只在运行内存里（`runtime.go:444`）。三者都不进入外部路径。
- 认证只有 10 分钟 HS256 JWT 加 refresh cookie（`platform/auth/token.go:12`），没有第三方客户端概念。
- MCP 规范当前版本 2026-07-28：受保护资源元数据（RFC 9728）必须实现；客户端注册优先级为预注册 > Client ID Metadata Document（CIMD）> 动态注册（DCR，已标弃用）；PKCE S256、资源指示符（RFC 8707）、受众校验、公共客户端 refresh token 轮换均为 MUST。Claude Code 默认走 DCR、能识别 CIMD；Codex 支持 CIMD 和 DCR。Codex 工具调用默认 60 秒超时；Claude Code 默认很长，超过 2 分钟转后台。两者都不支持 Tasks 扩展。claude.ai 自定义连接器支持 OAuth 与 DCR，回调为 claude.ai 的 https 地址，并会同步到登录 claude.ai 账号的 Claude Code；ChatGPT 的自定义 MCP 应用要在 Developer Mode 中添加。
- 官方 Go SDK `github.com/modelcontextprotocol/go-sdk` v1.8.0：streamable HTTP handler 是标准 `http.Handler`，自带 Bearer 校验中间件和受保护资源元数据 handler，不含授权服务器。

以上外部事实来自子代理调研的官方文档链接，C1 设计时逐项对照原文核实（见「未决事项」U1、U2）。

## 领域模型与业务规则

### 术语

- **外部助手**：在创意空间以外运行、经 MCP 调用创意空间工具的 AI 助手。首批是 ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页）。它自己负责对话、计划和模型调用，平台不为它的对话计费。与「助手」（内置创意助手）相对。
- **外部助手连接**：一次 OAuth 授权的结果，等于「某个账号 × 某个外部助手客户端」的一份授权。它是外部助手在平台上的身份：署名、任务归属、撤销都以它为单位。同一账号在两台电脑上各装一次 Claude Code，就是两个连接。
- **助手主体**：执行工具的身份，二选一：运行（内置助手的一次运行）或外部助手连接。领域写入记录的「谁改的」取自助手主体。
- **宿主**：运行外部助手的产品。它决定接入方式和署名：
  - Claude 桌面端、网页的连接器由 claude.ai 发起授权，署名「Claude」；登录 claude.ai 账号的 Claude Code 共用这个连接器，署名同样是「Claude」。
  - 用本机命令添加的 Claude Code 署名「Claude Code」。
  - Codex 署名「Codex」，ChatGPT 署名「ChatGPT」。
- **任务句柄**：外部工具对尚未完成的异步工作（生成执行、场景客户端任务）返回的 ID，外部助手用 `wait_tasks` 查询或等待。

正式定义写入 `glossary`（`contracts.md` 的 `glossary: ADD 外部助手`、`glossary: ADD 外部助手连接`）。「用户」仍是禁用词，界面文案说「创作者」或「账号」。

### 不变量

- **X-I1 账号隔离**：连接属于且只属于一个账号；每次工具调用的 `AccountScope` 只来自令牌所属连接的账号，工具参数里永远没有 `account_id`（ADR-001）。
- **X-I2 令牌分域**：MCP 访问令牌的受众是 MCP 资源地址 `<PUBLIC_BASE_URL>/mcp`，网页端 `authMiddleware` 拒绝它；网页端 JWT 没有这个受众，MCP 端点拒绝它。两者用不同的签名密钥派生（见「令牌」）。
- **X-I3 撤销立即生效**：连接被断开后，它的下一次请求一律 401，不等访问令牌过期；它排队中的场景客户端任务在同一事务里取消；已提交的生成照常完成并按规则扣费（与摄影师取消页面时一致）。
- **X-I4 语义一致**：同名工具在内置助手和 MCP 上执行同一个领域入口、同一套校验和错误码；工具说明出自同一个 `Info()`，外部版只追加与外部路径有关的差异（显式的 `canvas_id`、异步句柄）。不为 MCP 另写第二套业务规则。
- **X-I5 撤销只管自己**：外部助手的写入记为外部主体，不进入摄影师的本人撤销栈；摄影师可以在历史列表里预览和恢复它（沿用人机协同撤销原则）。
- **X-I6 不在服务端渲染**：建模烘焙、回看截图、导出分镜仍是场景客户端任务，只由打开的创意空间页面执行（D2、`architecture: 场景模块`）。

### 聚合与一致性边界

- **外部助手连接**是授权服务器的聚合（表 `external_agent_connections`），连同它的 refresh token 链。连接的状态只有「有效」和「已断开」；最近使用时间是派生出的活动状态，不进入一致性边界。
- **OAuth 客户端**（DCR 注册或 CIMD 缓存）与**授权码**是授权服务器内部的短期数据，不被业务模块读取。
- 业务聚合（画布、文档、场景、资产库、生成执行、场景客户端任务）不变，只是它们记录写入者或归属时多一种取值：外部连接 ID。业务模块只认 `connection_id` 和展示名，不读授权服务器的表。

## 架构设计

### 模块职责与依赖方向

```
宿主（ChatGPT / Claude / Claude Code / Codex）
  │  OAuth 2.1 + PKCE          │  MCP streamable HTTP (Bearer)
  ▼                            ▼
platform/httpapi ──► platform/oauth        platform/httpapi ──► creativemcp ──► creativeagent (外部工具门面)
  (授权端点、连接管理接口)    (授权服务器)                                    │
                                                                            ▼
                                       creativecanvas / creativelibrary / creativegeneration /
                                       creativescene / creativedocument / creativeskill
```

- **`backend/internal/platform/oauth`（新增）**：授权服务器。元数据、客户端注册（CIMD、DCR，含网页端连接器使用的机密客户端）、授权请求校验与授权码、令牌签发与 refresh 轮换、撤销、连接的增删查与活动状态。依赖 `platform/store`、`platform/auth`（复用网页登录态确认授权人身份、复用密钥派生）。不依赖任何 `creative*` 包。
- **`backend/internal/creativemcp`（新增）**：MCP 协议适配。用官方 Go SDK 建 server，把 `creativeagent` 的外部工具目录逐个注册为 MCP 工具，把领域错误映射成 MCP 工具错误，把图片结果转成 `ImageContent`。服务端 `instructions` 也由它下发，正文由 `creativeagent` 维护（与内置助手的 `instructions/base.md` 同目录，单独一份外部版本）。客户端声明 MCP Apps 扩展时，它提供两个界面资源（卡片 HTML 随服务端一起发布），并给挂卡片的工具附上 `_meta.ui`、给结果附上卡片数据（见「内嵌卡片」）。它是一个 `http.Handler`，不 import gin（ADR-003）。不含业务规则。
- **`backend/internal/creativeagent`（扩展）**：新增外部工具门面 `ExternalTools`：`List()` 返回外部可用的工具定义；`Call(ctx, principal, name, args)` 执行一次调用。工具实现从「依赖 `runtimeHandler`」改为「依赖一个调用者接口」，运行和外部连接各自实现它（见「助手主体」）。工具注册表 `agentToolDefinitions` 增加一个字段，标明该工具是否对外部开放（D6 的 6 个排除工具不开放）。
- **`platform/httpapi`（扩展）**：挂载 `/.well-known/oauth-protected-resource/mcp`、`/.well-known/oauth-authorization-server`、`/oauth/*` 和 `/mcp`；`/mcp` 前面是 SDK 的 Bearer 中间件，校验器由 `platform/oauth` 提供；令牌通过后注入外部连接主体，再交给 `creativemcp`。连接管理接口在 `/api/v1/creative/agent-connections`，走网页端鉴权。
- **前端（扩展）**：
  - 「外部助手」接入页（创意空间导航里的独立入口）：选宿主，复制接入文字，展开回退方法；下方是连接列表、最近使用时间、断开。见「宿主接入与回退」。
  - `/oauth/authorize` 授权确认页：未登录先走现有登录，再显示「允许 <客户端名> 访问你的创意空间」，列出它能做什么，以及数据会被发给外部助手的模型厂商。
  - 创意空间顶栏的「外部助手已连接」指示。
  - 场景历史按外部连接分组和署名。
  - 以上页面的视觉和交互依据 `docs/prototypes/creative-mcp/web/`。
- **内嵌卡片（新增）**：生成与任务卡、镜头卡，各是一个内联了样式和脚本的 HTML 文档，在宿主的沙箱 iframe 里运行，只经宿主与服务端通信。源码位置与构建方式由 C2b 定（U12），产物由 `creativemcp` 随服务端发布，不依赖创意空间的前端包。
- **Codex 插件目录 `integrations/codex-plugin/`（新增）**：manifest（含展示名、简介、起步提示）、MCP 配置（调大 `tool_timeout_sec`）、marketplace 文件。只发一个指向生产地址的包，不按环境拆包。不含平台 Skill 正文（D3），使用说明以服务端 `instructions` 为准。

依赖方向只新增 `httpapi → platform/oauth`、`httpapi → creativemcp → creativeagent`。`creativeagent` 不知道 MCP 和 OAuth 的存在，只认助手主体。

### 关键链路

**首次连接**：

```
宿主 ──GET /mcp──► 401 + WWW-Authenticate: resource_metadata=…/.well-known/oauth-protected-resource/mcp
宿主 ──读受保护资源元数据──► authorization_servers=[<PUBLIC_BASE_URL>]
宿主 ──读授权服务器元数据──► 端点、支持 CIMD、支持 S256
宿主 ──(CIMD: client_id 即元数据 URL | DCR: POST /oauth/register)
宿主 ──打开浏览器 /oauth/authorize?client_id&redirect_uri&code_challenge&resource&state
创作者 ──登录(已有) + 确认──► platform/oauth 建连接 + 授权码 ──302──► 宿主本地回调
宿主 ──POST /oauth/token (code + verifier + resource)──► access_token(1h) + refresh_token
宿主 ──POST /mcp (Bearer)──► 工具调用
```

**一次工具调用**：

```
/mcp ─► Bearer 校验（签名、aud、过期）─► 连接仍有效? ─► 外部连接主体
     ─► creativemcp 解析 tools/call ─► creativeagent.ExternalTools.Call
         ─► 外部准入：连接有效、账号能力（读 creative_read；写 agent_start；生成另加 node_generate）、限流
         ─► 与内置工具相同的领域入口（scope = 连接的账号，写入者 = 外部连接）
         ─► 结果（结构化 + 可选图片）或任务句柄
     ─► 节流更新连接的 last_used_at（≤ 1 次/分钟；由不活动变为活动时走账号失效通道）
```

**场景任务（C3）**：外部工具建任务（归属外部连接，不带运行）→ 账号失效通知 → 打开的创意空间页面认领、执行、暂存上传 → 完成接口按外部主体复查准入，同事务落结果 → 外部助手的 `wait_tasks` 读到终态。没有运行可以唤醒，Waiter 端口只对 `run_id` 非空的任务生效。

## 技术选型

| 问题 | 选择 | 理由与边界 |
|---|---|---|
| MCP 服务端库 | 官方 `modelcontextprotocol/go-sdk` v1.x | 规范方维护，跟进协议版本最快；自带 Bearer 中间件和受保护资源元数据；标准 `http.Handler` 可用 `gin.WrapH` 挂载。缺 Tasks 扩展不影响我们：已知宿主都不支持 Tasks，长任务用句柄。备选 `mark3labs/mcp-go` 有 Tasks，但协议版本支持说法不一致，不采用。 |
| 部署形态 | 远程 streamable HTTP，挂在现有 server 进程 | SaaS 定位，数据都在服务端；不另起进程、不做 stdio。单进程部署下，限流与活动状态用数据库加进程内计数即可。 |
| 授权服务器 | 自建于 `platform/oauth`，不引入第三方 IdP | 账号体系已在本系统；需要的只是 MCP 规定的授权码 + PKCE 子集。账号量小，引入 Keycloak 之类是预支复杂度。不做 OIDC、不做 implicit / client credentials / password grant。 |
| 客户端注册 | CIMD 为主，DCR 为兼容 | 规范推荐 CIMD，DCR 已弃用；但 Claude Code 和 claude.ai 连接器都支持 DCR，LibTV 也只做了 DCR，所以两者都支持。回调只允许回环地址（命令行宿主）或 https（claude.ai、ChatGPT 这类网页端宿主的服务端回调）。网页端连接器在服务端换令牌，可能注册成机密客户端，因此 DCR 接受 `none`、`client_secret_post`、`client_secret_basic`，密钥只存哈希。是否能只保留 CIMD 由 C1 实测决定（U2）。 |
| 访问令牌 | 自签 JWT（HS256），1 小时有效，`aud=<PUBLIC_BASE_URL>/mcp`，`sub=连接 ID` | 复用现有签发代码；用独立的派生密钥（HKDF：网页 JWT 密钥 + 用途标签 `mcp-access`），即使受众校验出错，两种令牌也互不通用。每次请求另查连接状态（主键查询），换来立即撤销。 |
| refresh token | 不透明随机串，只存哈希，每次使用轮换；空闲 30 天、绝对 90 天失效；已轮换的旧串被重放时断开整个连接 | 规范要求公共客户端轮换；重放即视为泄漏，沿用网页端 refresh 的处理思路（`platform/auth/service.go:19`）。 |
| 长任务 | 任务句柄 + `wait_tasks` 长轮询，单次等待 ≤ 45 秒 | 手动配置的 Codex 默认 60 秒超时，ChatGPT 和 claude.ai 连接器的超时未公开，45 秒是对所有宿主都安全的值；Claude Code 超过 2 分钟会转后台，45 秒也不触发。Codex 插件另把 `tool_timeout_sec` 调到 300 秒，作为余量，不改变 45 秒上限。不依赖 progress 通知（Codex 不展示）。 |
| 图片回传 | MCP `ImageContent`，长边缩到 ≤ 1024、JPEG/WebP，单次结果 ≤ 4 张 | 外部模型需要看图判断；原图经 `read_asset` 的媒体 URL 可另取。尺寸上限与内置助手的图像输入一致。 |
| 内嵌卡片 | MCP Apps 扩展（`io.modelcontextprotocol/ui`，规范 2026-01-26）；不用 OpenAI 旧的 `openai/outputTemplate` | ChatGPT 当前就按这个标准渲染，旧字段只是兼容别名；Claude 网页、桌面端也支持，一套卡片两家共用（D10）。线协议是 postMessage 上的 JSON-RPC，卡片可以直接实现，也可以用官方 `@modelcontextprotocol/ext-apps` 的 `App` 类，C2b 决定。只做两张卡（D11），其余工具不挂卡片，免得外部助手连续操作时刷屏。 |
| 工具结果文字 | 摘要（结论、链接、下一步）+ 结构化结果 JSON | Claude Code、Codex CLI 只显示结果的前几行，第一行要能单独看懂；规范说 `structuredContent` 不进模型上下文，OpenAI 文档又说 ChatGPT 的模型会读，所以 `content` 必须自给自足，JSON 仍保留给模型用。 |
| 接入方式 | 按宿主区分：Codex 插件；Claude 连接器；ChatGPT 应用（Developer Mode） | 参照 LibTV（research L1、L8）。Claude 系列只用连接器（D8）：一个连接器覆盖桌面端、网页和登录 claude.ai 账号的 Claude Code，避免插件和连接器同时装时出现两份工具。Codex 插件放在本仓库 `integrations/codex-plugin/`，发布时同步到一个公开 Git 仓库作为 marketplace 源（主仓库不公开），同步方式由 C1 定（U3）。 |

## 关键机制

### 助手主体

把 6 处运行耦合统一成一个调用者接口（Go 接口名留给 C1，这里给职责）：

| 职责 | 运行（现状） | 外部连接 |
|---|---|---|
| 准入 | 锁运行、核对 claim token / epoch / lease、`agent_start` | 连接有效、账号能力（读 `creative_read`；写 `agent_start`；生成另加 `node_generate`）、限流 |
| 画布 | 由对话查出固定的 `canvas_id` | 工具参数显式传 `canvas_id`，校验属于本账号 |
| 可读判定 | 按运行的 consent、画布、触发消息判定 | 授权即账号级 consent：本账号创意空间里未进回收站的内容都可读（见下节） |
| 写入者 | `Actor{agent, run_id}` | `Actor{external_agent, connection_id}` |
| 幂等种子 | `run_id/step` | 由 `creativemcp` 为每次 `tools/call` 生成的调用 ID；宿主重试同一调用不保证去重（MCP 没有幂等键），写工具在说明里提示「失败后先读再写」 |
| 场景任务归属 | `run_id` + `tool_call_id`，完成时唤醒运行 | `external_connection_id` + 调用 ID，完成后只落结果 |
| 资产库日志 | `run_id` + `conversation_id` | `external_connection_id`；日志表改为「三者择一」CHECK |
| Skill | 运行固定版本，记录在 `creative_run_skills` | 每次调用解析当前可用版本，或使用参数指定的版本；不记录 |
| 失败计数、批内资源排序 | 有 | 无（外部助手自己决定串并行，领域层并发控制照旧） |

运行路径在这次重构里行为不变；C1 只接入读路径需要的准入、画布和可读判定，C2、C3 依次接入其余职责。

### 授权与 consent

- 内置助手按对话授予「内容可发给厂商」的 consent（`creative_egress_consents`）。外部助手的情况不同：内容是平台发给外部助手，再由外部助手发给它的模型厂商，平台不参与那一段。
- 因此**授权确认页本身就是 consent**：页面写明「授权后，<客户端名> 可以读取和修改你创意空间里的画布、场景、资产库和 Skill，这些内容会被发送给它使用的 AI 模型服务」，创作者确认后，连接获得账号级、全部数据类别的读取许可。断开连接即撤回。
- 授权只有一个 scope：`creative`（D5，不做细粒度授权）。元数据和 401 里照样声明这个 scope，为以后的细粒度授权留位置。

### 令牌与撤销

```
有效 ──断开（接入页 / refresh 重放）──► 已断开（终态）
```

- 访问令牌校验：签名 → `aud` 等于 MCP 资源地址 → 未过期 → 连接有效（查一次主键）。
- refresh：哈希命中当前串 → 签发新的访问令牌和 refresh 串 → 旧串标记已轮换；命中已轮换的串 → 断开连接。
- 断开：同事务把连接标为已断开、作废 refresh 链、取消它非终态的场景客户端任务（C3 起）；提交后走账号失效通道，页面指示消失。

### 宿主接入与回退

接入页按宿主给出一段可复制的文字。命令行宿主能自己执行这段文字里的步骤；图形界面宿主需要创作者按文字手动操作。

| 宿主 | 主路径 | 无权限时的回退 | 署名 |
|---|---|---|---|
| Codex CLI | 发给 Codex：添加 marketplace、安装插件、新建任务后授权 | 不需要（本机配置） | Codex |
| Claude Code | 登录 claude.ai 账号时共用 Claude 连接器；否则发给 Claude Code，由它执行 `claude mcp add --transport http <名称> <PUBLIC_BASE_URL>/mcp`，再在 `/mcp` 里完成授权 | 本机命令不受 claude.ai 组织的连接器权限影响；企业用 managed settings 禁止添加 MCP 时，只能请管理员放行 | Claude / Claude Code |
| Claude 桌面端、网页 | 设置 → 连接器 → 添加自定义连接器，地址填 `<PUBLIC_BASE_URL>/mcp`，保存后授权 | Team 成员没有添加权限时：（1）复制一段「发给管理员」的文字，请组织管理员在组织设置里添加；（2）改用 Claude Code 的本机命令 | Claude |
| ChatGPT | 设置 → 应用 → 高级设置开启 Developer Mode，新建应用，地址填 `<PUBLIC_BASE_URL>/mcp`，授权 | 工作区不允许 Developer Mode 时：复制「发给管理员」的文字；或改用 Codex | ChatGPT |

- 接入文字里的地址、名称（「创意空间」）由服务端按 `PUBLIC_BASE_URL` 生成，不在前端写死。
- 宿主界面的入口名称和步骤会变。接入文字按 C1 实测时的界面写，接入页注明「以宿主当前界面为准」，变化后只改文字，不改服务端。
- 署名按授权时客户端的元数据识别（见 `data: 外部助手连接与 OAuth` 的 `host_kind`），识别不了的宿主显示客户端自报的名称。

### 服务端 instructions 与自检

- `instructions` 是所有宿主共用的使用说明，内容包括：
  - 先调用 `get_capabilities`；
  - 用 `list_canvases` 选画布，把画布链接交给创作者打开；
  - 长任务用 `wait_tasks`，盯住句柄、不重复提交；
  - 场景任务需要打开的页面；
  - 写入失败先读再重试；
  - 生成要花额度，`dry_run` 是可选的预检；
  - 汇报时给可点击的链接，不贴 JSON 和内部 ID；
  - 对话里显示了卡片时不复述卡片内容；收到卡片传来的创作者决定（确认生成、不生成、存入资产库）时以它为准，不重复提交。

  草稿见 `docs/prototypes/creative-mcp/instructions.md`。

  篇幅按图形界面宿主能接受的长度控制，细节放进各工具说明。
- `get_capabilities`（外部专用，只读）返回：
  - 连接的账号显示名、连接 ID 和宿主署名；
  - 当前部署可用的生成模型及其参数摘要；
  - 剩余生成额度（沿用网关的生成预算；积分落地后换成积分）；
  - 有没有打开的创意空间页面（账号级实时通道里是否有活动订阅，近似判断，只作提示）；
  - 平台 Skill 的数量与 `search_skills` 的提示。
- 它不是健康检查接口，不暴露部署细节。部署配置问题（例如没有生成模型）按「部署错误不暴露给摄影师」原则只体现为该能力不可用。

### 画布交接链接

- 所有读写画布、场景的工具结果带 `canvas_url`。写工具另带 `focus`（改动涉及的节点或场景物件 ID），链接打开后定位到这些节点或物件。
- 外部助手应把链接交给创作者打开；能在内置浏览器里打开页面的宿主（例如 Codex 桌面端）可以自己打开。这不是写入的前置条件：没有页面时，画布和文档写入照常生效，只有场景客户端任务需要页面（X-I6）。

### 生成预检

- `generate_node` 增加可选参数 `dry_run`（缺省 `false`）。为 `true` 时走完生成前的全部校验（节点、模型、参数、参考、提示词版本、额度），返回将用的模型、参考清单和预计扣费，不创建生成执行、不扣费。
- 是否预检由外部助手决定。`instructions` 只说明它在什么时候有用（首次用某个模型、预计花费较大），不要求每次都做；创作者要求全自动执行时，外部助手可以不再预检（owner 2026-10-22）。
- 预计扣费沿用网关的生成价格估算，与实际扣费可能有差异；结果里注明是估算。

### 工具结果的文字

- 每个工具的 `content` 文字分两段：
  - 摘要：第一行是结论，随后是关键字段、Markdown 格式的画布链接和给外部助手的下一步提示，不超过 8 行；
  - 结构化结果的 JSON。
- 摘要由 `creativeagent` 外部工具门面按工具生成，和工具说明放在一起维护（X-I4 不受影响：领域结果相同，只多了一段说明文字）。
- 写法：
  - 没完成的任务写明「再调用一次 `wait_tasks`」；
  - 排队等页面时说清楚为什么等、要打开哪个链接；
  - 预检写明「没有生成，没有扣点」。
- 工具的 `title` 用中文动作（检查连接、列出画布、写提示词、生成、回看、导出分镜……），宿主的工具行和许可提示里显示它。读工具标 `readOnlyHint`，删除类标 `destructiveHint`，宿主据此决定是否弹确认。
- 原型里的各工具摘要见 `docs/prototypes/creative-mcp/shared/results.mjs`，各子项实现本批工具时以它为起点。

### 内嵌卡片

```
宿主 ──initialize（声明 io.modelcontextprotocol/ui）──► 服务端记住本会话可用卡片
宿主 ──tools/list──► generate_node / wait_tasks 带 _meta.ui.resourceUri；其余工具 visibility=["model"]
宿主 ──resources/read ui://creative/tasks/v1──► 卡片 HTML（可预取、缓存）
外部助手 ──tools/call generate_node(dry_run)──► 结果（文字 + structuredContent + _meta.creative）
宿主 ──在沙箱 iframe 里渲染卡片，推送结果──► 卡片显示预检
创作者点「生成」──► 卡片 ─(宿主代为)─► tools/call generate_node ──► 任务句柄
卡片 ─(宿主代为)─► wait_tasks 长轮询 … ──► 结果图
卡片 ──ui/update-model-context──► 宿主 ──（下一轮）──► 外部助手得知「已提交、已完成」
```

- **只增强、不替代**：没有声明扩展的宿主收到的内容和以前完全一样。声明了扩展的宿主，模型读到的文字也完全一样；卡片只是同一结果的另一种呈现。
- **卡片的数据**：大部分读 `structuredContent`；只给卡片用的签名缩略图地址、要打开的链接放 `_meta.creative`，外部助手的模型看不到。
- **卡片的动作**：经宿主调用工具，只能调用 `visibility` 含 `app` 的 `generate_node`、`wait_tasks`、`save_to_library`。这一限制由宿主执行，服务端分辨不出调用来自卡片还是模型，所以服务端的准入、限流、计费照旧，不为卡片开后门，也不为它加闸门。
- **告诉外部助手**：卡片把创作者的决定写成一份累计摘要，用 `ui/update-model-context` 交给宿主。宿主一般等创作者的下一条消息才交给模型，所以点卡片按钮后，外部助手这一轮不会说话（原型已演示，owner 接受）。
- **安全**：
  - 卡片在宿主的沙箱 iframe 里，碰不到宿主页面和创意空间的登录态。
  - CSP 只放行图片所在的对象存储或 CDN 域名，不放行任何网络接口，也不嵌套 iframe。Claude 目前限制嵌套 iframe，所以不能把创意空间整页嵌进卡片，深度编辑一律回到创意空间页面。
  - 卡片不申请设备权限。
- **展示**：跟随宿主的明暗和字体，只用标准 CSS 变量，不用创意空间 v6 的近黑主题；琥珀色只表示进行中或等待，和创意空间的语义一致。行内最多两个动作；全屏只用于放大查看。
- **版本**：资源 URI 带版本号（`/v1`），与结果数据不兼容的改动发布新 URI。

### 工具集与异步句柄

- **C1 只读**：`get_capabilities`（外部专用，见上）、`list_canvases`（外部专用：按最近更新列出画布，供外部助手选择）、`read_canvas`、`read_asset`、`search_assets`、`read_library`。
- **C2 写入与生成**：`edit_canvas`、`set_node_prompt`、`generate_node`、`compress_image`、`save_to_library`、`organize_assets`、`manage_library`、`trash_assets`、`restore_assets`、`read_library_result`，以及外部专用的 `wait_tasks`。
- **C2b 内嵌卡片**：卡片协商、两张卡共用的资源规则、生成与任务卡（`ui://creative/tasks/v1`），以及 `generate_node`、`wait_tasks`、`save_to_library` 的卡片元数据。
- **C3 场景与 Skill**：`read_scene`、`edit_scene`、`write_model`、`render_view`、`export_shots`、`search_skills`、`load_skill`、`read_skill_resource`，以及镜头卡（`ui://creative/shots/v1`）。

异步规则（`generate_node` 和 C3 的 4 个场景任务工具）：

```
调用 ─► 提交（生成执行 / 场景任务）─► 在 ≤ 40 秒内完成? ──是──► 直接返回结果
                                          └─否──► {status: "pending", task_id, 进度说明}
wait_tasks(task_ids ≤ 8, timeout_s ≤ 45) ─► 任一终态或超时即返回各句柄当前状态
```

- 句柄就是领域 ID（生成执行 ID、`csjob_…`），只能查到本账号、本连接创建的任务。
- 场景任务排队超过 15 秒仍未被认领时，状态写 `waiting_for_page`，并附创意空间链接，提示创作者打开页面（D2、AC-6）。任务的 30 分钟期限和过期规则不变。

### 活动状态与页面指示

- 连接「活动」= `last_used_at` 在 5 分钟内。`last_used_at` 每分钟最多写一次；由不活动变为活动时走账号失效通道（`notify_creative_canvas_changed('account')` 同类通道），页面收到后重读活动连接。变为不活动不发通知，页面自己按 `last_used_at + 5 分钟` 到期隐藏。
- 指示只到账号级（「Claude Code 已连接」），不做「正在修改哪个物件」。场景编辑器的「助手正在修改」高亮是否扩展到外部主体，留给 C3 决定（U5）。

### 限流

- 每个连接每分钟工具调用 ≤ 120 次，同时等待中的任务句柄 ≤ 8 个；超出返回可重试的工具错误并带等待秒数。生成本身仍受网关的生成预算约束。
- 以上数值是初始值，C1 上线后按真实使用调整，不作为对外承诺。

### 错误映射

- 协议层（未认证、令牌失效、连接已断开）：HTTP 401 + `WWW-Authenticate`，宿主据此重新授权。
- 工具层（参数非法、不存在、冲突、额度不足、能力不足、限流）：MCP 工具结果 `isError: true`，内容里给领域错误码和中文说明，与内置助手看到的错误一致（X-I4）。领域错误不变成 JSON-RPC 协议错误，避免宿主把它当成连接故障。

### 账号隔离与安全

- CIMD 拉取客户端元数据：只允许 https、公网地址（拒绝私网与回环解析结果）、响应 ≤ 5 KB、超时 3 秒、按 HTTP 缓存头缓存 ≤ 24 小时；`redirect_uris` 必须与元数据一致。
- 授权确认页显示客户端名和回调地址的主机；回调只接受回环地址（`http://127.0.0.1:<port>`、`http://localhost:<port>`）或 https。
- 凭证红线不变：签名密钥只来自环境变量（沿用现有 JWT 密钥的派生），refresh token 和授权码只存哈希。
- 外部助手的写入能力与内置助手相同（D5），平台不为外部路径加额外闸门；风险（例如外部助手被提示注入后批量移入回收站）由回收站可恢复、历史可回退兜底，写进遗留风险而不是加审批（沿用「Agent 能力优先于权限控制」）。

## 接口设计

共享契约全部在本 Epic 的 `contracts.md` 中定义一次：

- HTTP：`api: OAuth 授权服务器`、`api: MCP 端点`、`api: 外部助手连接列表与断开`、`api: MODIFY 列出文档修订`。
- MCP 工具：`api: MCP 只读工具`、`api: MCP 写入与生成工具`、`api: MCP 场景与 Skill 工具`。工具的入参和结果沿用内置工具的 schema（`Info()`），契约只定义外部差异和新增的两个工具，不复制内置工具的全部字段。

兼容安排：MCP 工具名与内置工具名相同；外部工具的 schema 变化跟随内置工具，契约按版本号（`ToolEntry.Version`）追踪。MCP 端点不设 URL 版本，协议版本由 SDK 协商。

## 数据与存储设计

- 新增：`external_agent_connections`、`oauth_clients`、`oauth_authorization_codes`、`oauth_refresh_tokens`（`data: 外部助手连接与 OAuth`）。
- 扩展：
  - `creative_document_revisions.actor_kind` 增加 `external_agent`，新增 `external_connection_id`（`data: MODIFY 通用文档`，C2）。
  - `creative_scene_jobs` 新增 `external_connection_id`，`run_id` 与它最多二选一（`data: MODIFY 场景客户端任务`，C3）。
  - `creative_agent_library_calls` 的 `run_id`、`conversation_id` 改为可空，新增 `external_connection_id`，CHECK 二者择一（C2，表不在项目契约中，在 C2 design 写迁移）。
  - `creative_node_executions.caller_kind` 增加 `external_agent`，`caller_agent_run_id` 旁新增 `caller_external_connection_id`（C2，同上）。
- 外部连接 ID 只作为业务表里的归属与署名，不建外键到授权服务器的表（不同模块的聚合）；连接永不物理删除（已断开保留为署名来源），所以不会出现悬空署名。
- 迁移只加列、放宽约束，不改存量数据：存量行全部是运行或摄影师写入，取值不变。

## 验证与交付

- **共享不变量的验证**：
  - X-I1：每个外部工具在多账号夹具下的越权测试。
  - X-I2：网页令牌调 `/mcp`、MCP 令牌调 `/api/v1/*` 双向拒绝。
  - X-I3：断开后立即 401、任务取消。
  - X-I4：同一组输入在内置路径和外部路径上的领域结果对照（每个子项覆盖本批工具）。
  - X-I5：场景撤销 e2e（C3）。
- **宿主实测**：C1 起，每个子项都在真实的 ChatGPT、Codex CLI、Claude Code、Claude 桌面端上跑一遍本批工具（AC-10），记录宿主版本。
- **活原型**（D13）：C1 设计前，用假数据的 MCP 服务接进四个真实宿主，复用 `docs/prototypes/creative-mcp/` 的卡片和结果文字，核实 U1、U7、U9–U11。接 ChatGPT 和 claude.ai 网页需要临时的公网隧道，开之前由 owner 确认。活原型是一次性代码，不进生产。
- **卡片**（C2b、C3）：
  - 两家宿主实测卡片的显示、明暗、首次许可提示、卡片里的工具调用和上下文更新（AC-11）；
  - 不声明扩展的客户端拿到的工具列表和结果不含任何卡片字段；
  - 卡片 HTML 的 CSP 检查。
- **观测**：日志按连接 ID 和工具名记录调用、耗时、错误码；授权失败（PKCE、受众、回调不符、refresh 重放）单独记录，不写令牌明文。
- **灰度与回滚**：`MCP_ENABLED` 环境变量控制 `/mcp` 和 `/oauth/*` 是否挂载；关闭后元数据端点也不暴露。迁移只加列，回滚代码不需要回滚数据。

## 未决事项

| # | 问题 / 假设 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| U1 | 调研得到的规范与宿主行为需要对照原文核实：2026-07-28 版无状态协议、SDK `Stateless` 配置、四个宿主的注册方式（CIMD/DCR、公共/机密客户端）、回调地址、超时 | C1 | C1 设计时读官方文档，并在四个宿主上实测 | C1 design 批准前 |
| U2 | 能否只做 CIMD、不做 DCR | C1 的 `api: OAuth 授权服务器` 范围 | C1 实测 Claude Code 在授权服务器声明支持 CIMD 时是否改走 CIMD；不能就保留 DCR | C1 实施中，改动只收窄契约 |
| U3 | Codex 插件同步到公开仓库的方式（手动、CI、发布脚本）和公开仓库的位置 | C1 插件交付 | C1 设计时由 owner 定仓库位置 | C1 design 批准前 |
| U7 | ChatGPT 与 claude.ai 连接器的工具超时、图片结果展示、`instructions` 是否被读取；各宿主许可提示的样子和时机 | C1 的长任务与说明策略 | 活原型先测，C1 再在真实服务上复测；`instructions` 不被读取时，把关键规则同时写进 `get_capabilities` 的结果和工具说明 | C1 design 批准前（活原型结论），C1 实施中（复测） |
| U9 | Claude Code 是否渲染卡片；结果同时含图片与 `structuredContent` 时 `content` 是否被丢弃（有问题报告） | C1、C2b | 活原型实测；有问题就只在声明了扩展的客户端上返回 `structuredContent` | C1 design 批准前 |
| U10 | ChatGPT 的模型是否读取 `structuredContent`（与规范说法不一致） | C2b | 活原型实测；结论只影响 `structuredContent` 的精简程度，`content` 的规则不变 | C2b design 批准前 |
| U11 | 卡片里显示对象存储签名图片：CSP 域名、签名有效期；Claude `ui.domain` 在生产与本地开发两套地址下怎么算；ChatGPT 的 `domain` 字段 | C2b | 活原型实测，C2b 设计时定 | C2b design 批准前 |
| U12 | 卡片源码放在哪、怎么构建（复用前端工具链，还是放在 `creativemcp` 里用原生 JS），产物怎么随服务端发布 | C2b | C2b 设计时定 | C2b design 批准前 |
| U8 | Claude Team 成员能否添加个人连接器、管理员添加组织连接器的实际步骤；ChatGPT 工作区对 Developer Mode 的管控 | C1 的回退文字 | C1 用真实 Team 账号实测；没有账号时按官方文档写，验收记为限制项 | C1 验收前 |
| U4 | 画布写入署名与 `canvas-undo-own-changes` 谁先落地 | C2 | 见 roadmap「与其他工作的衔接」 | C2 design 批准前 |
| U5 | 场景编辑器「助手正在修改」高亮与撤回自己改动的范围，是否对外部连接同样生效 | C3 | C3 设计时按 C1、C2 的使用情况定 | C3 design 批准前 |
| U6 | 限流与等待上限的初始数值 | 全部 | 上线后按日志调整 | 不阻塞 |
