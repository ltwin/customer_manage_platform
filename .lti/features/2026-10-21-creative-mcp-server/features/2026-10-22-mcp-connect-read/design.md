# Change 详细设计

## 继承的设计

- **Epic `creative-mcp-server` 批准基线 v2**。适用条目：
  - design「领域模型与业务规则」「架构设计」「技术选型」；
  - 「关键机制」中的助手主体、授权与 consent、令牌与撤销、宿主接入与回退、服务端 instructions 与自检、画布交接链接、工具结果的文字、活动状态与页面指示、限流、错误映射、账号隔离与安全；
  - 「验证与交付」。
- **本次完整交付的契约**：
  - `architecture: 外部助手接入`
  - `glossary: 外部助手`、`glossary: 外部助手连接`
  - `data: 外部助手连接与 OAuth`
  - `api: OAuth 授权服务器`、`api: MCP 端点`、`api: 外部助手连接列表与断开`、`api: MCP 只读工具`
- **活原型结论**：Epic `discovery/research/live-prototype.md`。
- **项目约定**：
  - 账号隔离（ADR-001）、Handler 薄层（ADR-003）、凭证只走环境变量。
  - 网页接口契约先行（`api/openapi.yaml` + `make generate`）。
  - 创意能力只由账号状态决定（lesson `creative-capability-follows-account-status`）。

本次只接入读路径。写入、生成、`wait_tasks`、卡片、场景与 Skill 在后续子项，它们在本次建立的门面上加工具，不改协议与授权。

## 领域模型与业务规则

### 外部连接主体

`creativeagent.ExternalPrincipal` 是外部助手调用工具时的身份，由 `/mcp` 的令牌校验得出，交给工具门面：

```go
type ExternalPrincipal struct {
	Scope        store.AccountScope // 只来自令牌所属连接的账号（X-I1）
	ConnectionID string             // cxcon_…
	DisplayName  string             // 「Claude Code」等，署名与 get_capabilities 用
	HostKind     string             // claude / claude_code / codex / chatgpt / other
}
```

主体只在一次工具调用内存在，不持久化。业务模块只看到 `ConnectionID` 和 `DisplayName`，不读 OAuth 的表（Epic design「聚合与一致性边界」）。

### 读路径的三项职责

Epic design「助手主体」表里，C1 落地准入、画布和可读判定三项：

| 职责 | 运行 | 外部连接（本次） |
|---|---|---|
| 准入 | 锁运行、核对 claim/epoch/lease | 令牌有效、连接未断开、账号 `creative_read`、连接限流 |
| 画布 | 由对话查出 | `read_canvas` 显式传 `canvas_id`，查不到或不属于本账号时 `not_found` |
| 可读判定 | 运行的 consent、画布、触发消息、资产库授权 | 本账号未删除、可展示的内容都可读；数据类别全开；图片总能返回（外部助手自己决定能不能看图） |

可读判定抽成一个接口，运行和外部连接各自实现。`read_canvas`、`read_asset`、`search_assets`、`read_library` 的核心逻辑只写一份（X-I4）：

```go
// readAccess 是读工具对调用者的全部要求。
type readAccess interface {
	// readable 判断一个内容修订能否被这次调用读到；library 表示资产库授权也算。
	readable(ctx context.Context, tx store.TxAccountScope, revisionID string, library bool) (bool, error)
	consent(ctx context.Context, tx store.TxAccountScope) (Consent, error) // 数据类别
	imageCapable() bool
	libraryAllowed(ctx context.Context, metadata bool) error
}
```

- 运行的实现包住现有的 `readableRevisionInTx`、`readConsentForUpdate`、`requireLibrary`，行为不变。
- 外部的实现：
  - `readable` 只要求修订属于本账号且 `RequireUsable(display)` 通过；
  - `consent` 返回全部数据类别；
  - `imageCapable` 恒为真；
  - `libraryAllowed` 恒放行。

### 授权请求包

`GET /oauth/authorize` 校验通过后，把参数签成一个请求包，交给前端确认页，再由确认页原样提交回来（`api: OAuth 授权服务器`「确认授权」的 `request`）：

```
request = "v1." + base64url(payload JSON) + "." + base64url(HMAC-SHA256(key_request, "v1." + payload))
payload = {client_id, client_name, redirect_uri, code_challenge, state, resource, scope, host_kind, exp}
```

- `exp` 是签发后 10 分钟。
- payload 不保密，前端直接解码显示客户端名和回调主机。签名只防篡改。
- `host_kind` 在签发时按客户端元数据识别好（见「宿主识别」），确认时不再拉取 CIMD。

### 令牌

| 令牌 | 形态 | 存储 | 有效期 |
|---|---|---|---|
| 访问令牌 | JWT HS256。`iss=<PUBLIC_BASE_URL>`、`aud=<PUBLIC_BASE_URL>/mcp`、`sub=<connection_id>`、`acct`、`scope=creative`、`iat`、`exp`、`jti` | 不存 | 1 小时 |
| refresh token | `cxr_` + 32 字节随机数的 base64url | SHA-256 | 空闲 30 天、绝对 90 天 |
| 授权码 | 32 字节随机数的 base64url | SHA-256 | 60 秒，一次性 |
| DCR 客户端密钥 | 32 字节随机数的 base64url | SHA-256 | 不过期 |

签名密钥都从 `AUTH_TOKEN_SECRET` 按用途派生（`auth.deriveKey` 的 HKDF），用途标签分别是 `crm-auth/v1/mcp-access` 和 `crm-auth/v1/mcp-authorize-request`。网页端令牌用的是 `crm-auth/v1/access-signing`，所以两种令牌就算受众校验出错也互不通用（X-I2）。

## 模块职责与协作

```
宿主 ──► httpapi（oauth.go、mcp.go）──► platform/oauth.Service ──► oauth.Store（store/oauth.go）
                         │
                         └─► creativemcp.Handler ──► creativeagent.ExternalTools ──► creativecanvas / creativelibrary / creativecontent / creativemedia
前端 ──► httpapi（agent_connections.go、oauth.go 的 decision）──► platform/oauth.Service
```

- **`platform/oauth`**：
  - 授权服务器的全部规则：元数据、注册、授权请求校验与签名、确认、换令牌、refresh、访问令牌校验、连接的列出、断开与活动状态。
  - 依赖自己的 `Store` 端口和 `auth`（只用密钥派生）。不 import gin，不 import 任何 `creative*`。
  - CIMD 拉取用注入的 `*http.Client`，拨号器拒绝私网与回环地址。
- **`platform/store/oauth.go`**：`oauth.Store` 的 pgx 实现。
  - OAuth 客户端、授权码、refresh token 不属于某个账号，按哈希或主键全局查找，与网页端 refresh 会话（`store/auth_session.go`）同一种做法。
  - 连接的列出与断开按账号限定。
- **`creativemcp`**：MCP 协议适配，不含业务规则，不 import gin。
  - 用官方 SDK 建 server，从 `ExternalTools.List` 注册工具，下发 `instructions`。
  - 在 SDK 的 `auth.RequireBearerToken` 前后包两层：前面是令牌校验（调用注入的 `Verifier`），后面是按连接的限流。
  - 把 `ExternalResult` 转成 `CallToolResult`，领域拒绝转成 `isError`。
- **`creativeagent`（扩展）**：
  - `ExternalTools`：外部工具目录与调用入口。
  - 6 个只读工具的外部实现：`get_capabilities`、`list_canvases` 是外部专用；其余 4 个调用共用的读核心。
  - 结果摘要按工具生成，与工具说明放在一起。
  - `instructions/external.md`：外部版本的使用说明。
- **`platform/httpapi`（扩展）**：
  - 挂载协议端点和网页端接口，只做 HTTP 适配。
  - `/mcp` 交给 `creativemcp.Handler`；令牌校验的结果（连接、账号）由 `oauth.Service.VerifyAccess` 给出，`httpapi` 据此造 `ExternalPrincipal`。
- **`platform/llmgateway`（扩展）**：`MonthBudget(ctx, scope)` 只读本月账（`llm_budgets`），给 `get_capabilities` 用。
- **前端**：接入页、确认页、顶栏指示（见「前端」）。

## 关键机制

### 首次连接与授权

```
宿主 ──POST /mcp（无令牌）──► 401 WWW-Authenticate: Bearer resource_metadata="…/.well-known/oauth-protected-resource/mcp", scope="creative"
宿主 ──GET 受保护资源元数据、授权服务器元数据
宿主 ──POST /oauth/register（DCR）          或 client_id = https URL（CIMD）
宿主 ──打开浏览器 GET /oauth/authorize?…
服务端：
  1. 解析客户端：https URL → CIMD 拉取；否则查 DCR 客户端。失败 → 302 /oauth/consent?error=invalid_client
  2. redirect_uri 精确匹配（回环地址忽略端口）。失败 → 302 /oauth/consent?error=invalid_redirect_uri
  3. 其余参数：response_type=code、S256 的 code_challenge（43–128 字符）、resource 等于 MCP 地址、scope 为空或 creative
     失败 → 302 redirect_uri?error=invalid_request|invalid_target|invalid_scope&state&iss
  4. 签请求包 → 302 /oauth/consent?request=…
前端确认页（需要登录；未登录先去 /login，登录后带着原查询串回来）：
  解码 payload 显示客户端名、回调主机、能做的事和数据去向
  点「允许」或「拒绝」→ POST /api/v1/oauth/authorize/decision {request, decision}
服务端 decision：
  验签、未过期 → 账号 creative_read 与 status=active
  allow：同一事务建连接 + 写授权码 → {redirect_to: redirect_uri?code&state&iss}
  deny ：{redirect_to: redirect_uri?error=access_denied&state&iss}
前端：
  回环回调（命令行宿主）：浏览器跳过去，宿主本机页面接管；确认页先显示「已授权，可以回到 <宿主>」
  https 回调（网页宿主）：直接跳转
宿主 ──POST /oauth/token（authorization_code）──► 访问令牌 + refresh token
```

- 客户端或回调地址不合法时只在本站显示错误，绝不重定向到未校验的地址（防开放重定向）。
- 确认页的路由是 `/oauth/consent`，与服务端的 `/oauth/authorize`、`/oauth/token`、`/oauth/register` 分开，Vite 开发代理只转发后三个。

### 回调地址规则

| 地址 | 规则 |
|---|---|
| `http://127.0.0.1:*`、`http://[::1]:*`、`http://localhost:*` | 允许，比较时忽略端口（RFC 8252 §7.3） |
| `https://…` | 允许，必须逐字相等 |
| 其他（`http` 非回环、自定义 scheme、带 fragment、带 userinfo） | 拒绝 |

注册（DCR）和 CIMD 元数据里的每个地址都按这张表校验，1–5 个。

### 换令牌与 refresh

```
authorization_code：
  锁授权码行（按哈希）→ 不存在 / 过期 → invalid_grant
  已用 → 断开对应连接（revoked_reason 记 refresh_replay）→ invalid_grant
  client_id 不符 / 机密客户端密钥不符 → invalid_client
  redirect_uri 不符、PKCE 不符 → invalid_grant
  resource 不符 → invalid_target
  标记已用 → 写第一张 refresh（absolute = now + 90 天）→ 签访问令牌

refresh_token：
  锁 refresh 行（按哈希）→ 不存在 → invalid_grant
  连接已断开 / state=revoked / 空闲超过 30 天 / 超过绝对期限 → invalid_grant
  state=rotated → 断开连接（refresh_replay），该连接所有行改 revoked → invalid_grant
  client_id 不符 → invalid_client
  当前行改 rotated → 写新的 current（absolute 沿用）→ 签访问令牌
```

授权码被重放时，契约要求「断开对应连接」。`revoked_reason` 只有 `user`、`refresh_replay`、`account_disabled` 三个取值，授权码重放同样是凭证泄漏的信号，记为 `refresh_replay`，不为它扩枚举。

### 每次 MCP 请求

```
Authorization: Bearer …
  → JWT：HS256、iss、aud = MCP 地址、未过期、sub 形如 cxcon_…
  → 按 id 读连接：revoked_at 为 null，账号 status=active
  → 节流写 last_used_at（为 null 或早于 1 分钟前才写；单条 UPDATE 带条件，不加锁）
  → TokenInfo{UserID: 连接 ID, Scopes: [creative], Expiration, Extra: {principal}}
  → 限流：每个连接 60 秒滑动窗口 ≤ 120 次 tools/call，超出返回 isError rate_limited 和 retry_after_s
  → SDK 处理 JSON-RPC；tools/call → ExternalTools.Call(principal, name, args)
```

- 任一步失败返回 401 和 `WWW-Authenticate`，宿主据此先 refresh、再重新授权。
- `TokenInfo.UserID` 设为连接 ID，SDK 会拒绝别的连接用同一个会话 ID（防会话劫持）。
- 账号停用时连接还在，但请求被拒。维护任务批量断开（`account_disabled`）留到账号停用流程出现时再做，现在没有停用入口。

### 工具结果

`ExternalTools.Call` 返回：

```go
type ExternalResult struct {
	Summary    string          // 摘要，第一行是结论，≤ 8 行
	Structured json.RawMessage // 结构化结果
	Images     []ExternalImage // read_asset 的图片：JPEG 或 PNG，长边 ≤ 1024，单次 ≤ 4 张
}
```

`creativemcp` 组装成：

- `content[0]` 为文字：`Summary + "\n\n" + Structured`。
- 随后每张图片是一项 `ImageContent`。
- `structuredContent` 为 `Structured`。有图片的结果不给 `structuredContent`，原因见「局部设计与偏差」第 2 条。

领域拒绝时，`isError: true`，文字是 `{"code", "message", "retry_after_s"?}`：

| 来源 | `code` | `message` |
|---|---|---|
| 参数不符合 schema（`errToolArguments`）、`creativeops.ErrValidation` | `validation_failed` | 与内置助手相同的说明 |
| 画布、内容、资产不存在或不属于本账号 | `not_found` | 「画布不存在」等 |
| 账号没有创意能力（`store.ErrCreativeAccessDenied`） | `forbidden` | 「账号没有创意空间能力」 |
| 工具自己的业务拒绝（`toolRefusal`、`creativeops.Public`） | `refused` | 拒绝原文（与内置助手看到的一致） |
| 限流 | `rate_limited` | 带 `retry_after_s` |
| 其他 | `internal` | 「系统内部错误，原因已记录（编号 …）」，编号写进日志 |

运行路径的错误分支（`runtime.go` 中 `executeTool` 的 `switch`）抽成 `toolFailure(err)`，两条路径共用。运行看到的 JSON 不变。

### 宿主识别

授权请求时按客户端元数据识别 `host_kind`，按顺序取第一条命中：

| 条件 | `host_kind` | 署名 |
|---|---|---|
| 回调主机是 `claude.ai` / `claude.com` 或其子域 | `claude` | Claude |
| 回调主机是 `chatgpt.com` / `openai.com` 或其子域 | `chatgpt` | ChatGPT |
| `client_name` 含 `claude code`（不分大小写），或 CIMD 地址的主机是 `claude.ai` 且回调是回环地址 | `claude_code` | Claude Code |
| `client_name` 含 `codex` | `codex` | Codex |
| 其他 | `other` | `client_name` |

活原型里 Claude 桌面端的 Code 模式与 Claude Code 共用本机配置，所以都识别为 `claude_code`。规则在 C1 实测后修正；每次授权在日志里记录 `client_name`、回调主机和 CIMD 地址，供修正时查。

### CIMD 拉取

- 只拉 `https` URL，端口必须是 443 或不写；不跟随重定向。
- 拨号前解析出的地址必须是公网单播地址：拒绝回环、私网、链路本地、组播、未指定地址，也拒绝 IPv4 映射的 IPv6。用 `net.Dialer.Control` 检查真实连接的地址，防 DNS 重绑定。
- 3 秒超时，响应 ≤ 5 KB，`Content-Type` 为 JSON。
- 元数据里的 `client_id` 必须等于 URL；`redirect_uris` 按回调地址规则校验；认证方式只能是 `none`。
- 结果写入 `oauth_clients`（`registration='cimd'`），`fetched_at` 后 24 小时内不再拉取。

### 动态注册限流

同一 IP 每小时 ≤ 20 次注册，超出返回 429 和 `Retry-After`。复用现有的 `securitybudget`（`store.ConsumeSecurityAttemptBudget`），IP 取自 `httpapi` 的可信代理解析（与登录相同）。

### get_capabilities 与 list_canvases

- `get_capabilities`：
  - `account.display_name` 读 `account_profiles.display_name`，为空时摘要写「已连接创意空间账号」。
  - `generation.models` 来自 `Generator.Models()`：`key`、`display_name`（`Label`）、`node_type`（`Kind`）、`parameters_summary`（参数名的逗号列表）。
  - `generation.budget` 来自 `llmgateway.MonthBudget`：`remaining` 为本月上限减已花和已占用，按网关币种换成两位小数的字符串；`unit` 为币种代码（如 `USD`）；`period_end` 为下个月第一天的 UTC 零点。网关没有配置时为 null。
  - `workspace_open`：本进程的实时连接里，这个账号有没有 `canvas` 类订阅（`store.Store.RealtimeWatching`）。单进程部署下准确，多实例时只代表本实例，所以只作提示。
  - `skills.count`：平台 Skill 与账号 Skill 中当前可用版本的数量（`creativeskill` 的现有查询）。
  - `hints`：
    - 没有打开的页面时，提示「把画布链接交给创作者打开，能实时看到变化」；
    - 没有生成模型时，提示「当前部署没有可用的生成模型」，不说部署细节。
- `list_canvases`：复用 `creativecanvas.ListProjects`（未归档，按最近编辑倒序，`query` 为标题包含）。
  - 每个项目取默认画布：`canvas_id` = `DefaultCanvasID`，`title` = 项目名，`updated_at` = `EditedAt`。
  - `canvas_url` = `<PUBLIC_BASE_URL>/creative/canvases/<canvas_id>`。
  - 游标就是 `ListProjects` 的游标，已绑定账号和查询条件。

### read_canvas、read_asset、search_assets、read_library 的外部差异

- **`read_canvas`**：
  - 入参是内置的 `offset`、`limit` 加必传的 `canvas_id`。
  - 画布用 `creativecanvas.GetCanvas`；文字节点的正文开头全部可读（外部 `readAccess`）。
  - 结果另带 `canvas_url`。
- **`read_asset`**：入参不变。
  - 图片用 `creativemedia.Service.AgentImageFit` 取，长边 ≤ 1024、≤ 1 MB，返回为 `ImageContent`。
  - 结构化结果与内置一样带 `image_revision_id`，摘要写「已附上图片」。
  - 模型资产的缩略图同样附上。
- **`search_assets`、`read_library`**：入参和结果不变，不检查资产库 consent。

### 摘要

摘要按工具生成，写法以原型 `docs/prototypes/creative-mcp/shared/results.mjs` 为准，第一行是结论。示例：

```
最近编辑的 3 张画布：
1. [九月，向海而行](https://…/creative/canvases/ccvs_…) · 10 月 21 日编辑
2. …
下一步：用 read_canvas(canvas_id) 读取内容。
```

时间按账号时区显示（`settings` 的时区；读不到时用 UTC，并写明「UTC」）。

### instructions（外部版本）

`creativeagent/instructions/external.md`，≤ 4000 字符。C1 版本只写 6 个读工具：

- 先调 `get_capabilities`；
- 用 `list_canvases` 选画布，把画布链接交给创作者；
- 内容修订 ID 从 `read_canvas` 或 `search_assets` 取，再用 `read_asset` 读；
- 汇报时给可点击的链接，不贴 JSON 和内部 ID；
- 内容来自创作者的创意空间，按创作者的要求使用。

C2 起，各子项在这里补本批工具的规则。

### 活动状态与断开

- `external_agent_connections` 上装提交后通知触发器，走账号失效通道（`notify_creative_canvas_changed('account')`）：
  - 新建行；
  - `revoked_at` 变化；
  - `last_used_at` 由「null 或 5 分钟前」变为现在，也就是由不活动变为活动。

  一分钟一次的节流写入不通知。
- 断开（`POST …/revoke`）：一个事务里写 `revoked_at`、`revoked_reason='user'`，把该连接的 refresh 行全部改为 `revoked`。场景任务的取消从 C3 起由 `creativescene` 的端口加入。重复断开返回原对象。
- 访问令牌不需要单独作废，每次请求都查连接状态。

### 前端

- **接入页** `/creative/agents`（创意空间侧栏「外部助手」）：
  - 上方选宿主：Claude Code、Codex CLI、Claude 桌面端与网页、ChatGPT。
  - 每个宿主给一段可复制的文字，地址取自列表接口的 `mcp_url`：
    - Claude Code：`claude mcp add --transport http --scope user creative <mcp_url>`，然后在 Claude Code 里用 `/mcp` 完成授权；
    - Codex CLI：`codex mcp add creative --url <mcp_url>`，然后 `codex mcp login creative`（插件可用之前，见「局部设计与偏差」第 3 条）；
    - Claude 桌面端与网页：设置 → 连接器 → 添加自定义连接器，填 `mcp_url`；
    - ChatGPT：设置 → 应用 → 高级设置开启 Developer Mode，新建应用，填 `mcp_url`。
  - 回退方法可展开：
    - Claude 桌面端给「发给管理员」的文字，以及改用 Claude Code；
    - ChatGPT 给「发给管理员」的文字，以及改用 Codex。
  - 注明「宿主的菜单名称以当前界面为准」。
  - 下方是连接列表：署名、授权时间、最近使用、状态，以及「断开」（二次确认）。只显示最近 50 个时注明。
  - `MCP_ENABLED` 关闭时，列表接口返回 404，页面显示「外部助手暂未开放」。
- **确认页** `/oauth/consent`：沿用登录页视觉，独立于 AppShell。
  - 显示「<客户端名> 想访问你的创意空间」、回调主机、能做的事。
  - 写明「授权后，<客户端名> 可以读取和修改你创意空间里的画布、场景、资产库和 Skill，这些内容会被发送给它使用的 AI 模型服务；生成会按你的额度扣费」。
  - DCR 客户端的名称无法验证，加一行警示。
  - 允许、拒绝、出错、已授权四种状态。
- **顶栏指示**：画布页顶栏显示「<署名> 已连接」，悬停看最近使用时间，点击去接入页。
  - 数据来自 `GET …/agent-connections?active=true`：进入页面时取一次，之后每 60 秒取一次，画布的实时失效通知到达时也重取。
  - 前端按 `last_used_at + 5 分钟` 自己到期隐藏。
- 视觉与交互依据 `docs/prototypes/creative-mcp/web/`（connect、authorize、canvas）。

## 局部设计与偏差

1. **关掉 SDK 的本机 Host 保护**：`StreamableHTTPOptions.DisableLocalhostProtection = true`。
   - SDK 默认拒绝「来自回环地址、Host 不是本机」的请求，防的是本机 MCP 服务被 DNS 重绑定。
   - 本服务在反向代理后面，所有请求都来自回环地址、带公网 Host，会被全部拒绝。
   - 端点本身有 OAuth 保护，DNS 重绑定拿不到令牌，所以关掉。
2. **带图片的结果不给 `structuredContent`**（U9 的保守做法）：
   - 有问题报告说 Claude Code 在结果同时含图片和 `structuredContent` 时丢掉 `content`。活原型没测到这种情况。
   - 文字里已经有完整 JSON，少给 `structuredContent` 不丢信息。
   - C1 实测 Claude Code 没有这个问题时，再改回总是给，契约不变（契约说 `structuredContent` 为同一结构化结果，没给时以文字为准）。
3. **Codex 接入先走手动添加**：
   - 插件目录按设计做好（manifest、`.mcp.json` 的 `tool_timeout_sec: 300`、marketplace），由 `integrations/codex-plugin/build.sh <生产地址>` 生成发布目录。
   - 公开仓库（U3）和生产域名定下来之前，接入页的 Codex 文字给 `codex mcp add` + `codex mcp login`。
   - 插件发布后，接入页改成「添加 marketplace、安装插件」，只改前端文字。
4. **会话**：用 SDK 默认的有状态会话（`Mcp-Session-Id`），不开 `Stateless`。
   - C2b 要按会话记住客户端是否声明了卡片扩展，无状态模式拿不到 `initialize` 的参数。
   - 服务重启后会话丢失，宿主按规范重新初始化。
5. **限流计数在进程内存**：单进程部署（Epic 技术选型），重启清零可以接受。

## 接口与数据契约

- **共享契约**：按绑定交付，正文见 Epic `contracts.md`，本子项不复制。
- **网页端接口**：三个接口写进 `api/openapi.yaml`，`make generate` 生成 Go 与 TS：
  - `GET /api/v1/creative/agent-connections`
  - `POST /api/v1/creative/agent-connections/{id}/revoke`
  - `POST /api/v1/oauth/authorize/decision`
- **协议端点**：`/.well-known/*`、`/oauth/*`、`/mcp` 不进 OpenAPI，与 `/healthz` 一样手动挂载。它们遵循 OAuth 与 MCP 规范，不用 `ErrorEnvelope`。
- **迁移 `0080_external_agent_oauth`**：
  - 建四张表、CHECK、部分唯一索引（每连接一行 `current`），以及索引 `(account_id, revoked_at, last_used_at DESC)`；
  - 加通知触发器。
  - 回滚删除四张表；存在连接行时拒绝回滚，前向修复。
- **局部契约**：本子项没有上层未定义的局部契约，`contracts.md` 留空。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/platform/store/migrations/0080_external_agent_oauth.up.sql` | 新增 | 四张表、约束、索引、通知触发器 |
| `backend/internal/platform/store/migrations/0080_external_agent_oauth.down.sql` | 新增 | 有连接行时拒绝 |
| `backend/internal/platform/store/oauth.go` | 新增 | `oauth.Store` 的实现 |
| `backend/internal/platform/store/realtime_events.go:Store.RealtimeWatching` | 新增 | 账号有没有某类实时订阅 |
| `backend/internal/platform/oauth/` | 新增 | `service.go`、`metadata.go`、`client.go`（DCR、CIMD、回调规则）、`authorize.go`（请求包、确认）、`token.go`（换令牌、refresh、访问令牌）、`hostkind.go`、`connections.go`、`errors.go` |
| `backend/internal/platform/auth/crypto.go:DerivePurposeKey` | 新增 | 导出按用途派生密钥，供 `oauth` 使用 |
| `backend/internal/platform/config/config.go` | 修改 | `MCP_ENABLED` |
| `backend/internal/platform/llmgateway/budget.go:Service.MonthBudget` | 新增 | 本月剩余额度只读查询 |
| `backend/internal/creativemedia/agent.go:Service.AgentImageFit` | 新增 | 长边与字节上限内的图片 |
| `backend/internal/creativeagent/read_access.go` | 新增 | `readAccess` 接口与运行、外部两个实现 |
| `backend/internal/creativeagent/context.go:runtimeHandler.readCanvas` | 修改 | 改为调用共用核心 `readCanvasView` |
| `backend/internal/creativeagent/context.go:runtimeHandler.readAsset` | 修改 | 改为调用共用核心 `readAssetResult` |
| `backend/internal/creativeagent/library_tools.go:runtimeHandler.searchAssets` | 修改 | 共用核心 |
| `backend/internal/creativeagent/library_tools.go:runtimeHandler.readLibrary` | 修改 | 共用核心 |
| `backend/internal/creativeagent/runtime.go:toolFailure` | 新增 | 从读工具执行的错误分支抽出，两条路径共用 |
| `backend/internal/creativeagent/tool_registry.go:agentToolDefinition` | 修改 | 增加 `external` 标记；D6 的 6 个排除工具不标 |
| `backend/internal/creativeagent/external.go` | 新增 | `ExternalTools`、`ExternalPrincipal`、`ExternalResult`、`List`、`Call`、错误映射 |
| `backend/internal/creativeagent/external_read.go` | 新增 | 6 个只读工具的外部实现与摘要 |
| `backend/internal/creativeagent/instructions/external.md` | 新增 | 外部版本的 instructions |
| `backend/internal/creativemcp/` | 新增 | `handler.go`（SDK server、工具注册、会话选项）、`result.go`、`ratelimit.go` |
| `backend/internal/platform/httpapi/oauth.go` | 新增 | 元数据、注册、授权请求、令牌、确认授权 |
| `backend/internal/platform/httpapi/mcp.go` | 新增 | `/mcp` 挂载与主体注入 |
| `backend/internal/platform/httpapi/agent_connections.go` | 新增 | 连接列表与断开 |
| `backend/internal/platform/httpapi/router.go:NewRouter` | 修改 | `RouterDeps.OAuth`、`RouterDeps.MCP`，按开关挂载 |
| `backend/cmd/server/main.go` | 修改 | 装配 `oauth.Service`、`ExternalTools`、`creativemcp.Handler` |
| `backend/go.mod` | 修改 | `github.com/modelcontextprotocol/go-sdk` v1.8.0 |
| `api/openapi.yaml` | 修改 | 三个网页端接口 |
| `frontend/src/creative-canvas/agents/` | 新增 | `AgentsPage.tsx`、`hostGuides.ts`、`api.ts`、`agents.css` |
| `frontend/src/pages/auth/AgentConsentPage.tsx` | 新增 | 授权确认页 |
| `frontend/src/creative-canvas/editor/AgentConnectionBadge.tsx` | 新增 | 顶栏指示 |
| `frontend/src/creative-canvas/editor/WorkspacePage.tsx` | 修改 | 挂顶栏指示 |
| `frontend/src/creative-canvas/home/AppSidebar.tsx` | 修改 | 「外部助手」入口 |
| `frontend/src/App.tsx` | 修改 | 两个路由 |
| `frontend/vite.config.ts` | 修改 | 开发代理 `/mcp`、`/oauth/(authorize\|token\|register)`、`/.well-known/oauth-*` |
| `integrations/codex-plugin/` | 新增 | manifest、`.mcp.json` 模板、marketplace、`build.sh`、README |
| `docs/dev/creative-mcp.md` | 新增 | 开关、本机接入与排错 |

## 验证与交付

- **授权服务器**（`platform/oauth` 单测 + `store` 集成测试）：
  - 回调地址表的每一行；
  - PKCE 不符、授权码过期、授权码重放（断开）、`resource` 不符、`client_id` 不符、机密客户端密钥不符；
  - refresh 轮换、已轮换的重放（断开并作废全链）、空闲与绝对期限；
  - 请求包篡改与过期；
  - CIMD：私网地址、重定向、超大响应、`client_id` 不符、缓存期内不重拉；
  - DCR 限流。
- **令牌分域（X-I2）**：网页 JWT 调 `/mcp` 得 401；MCP 访问令牌调 `/api/v1/me` 得 401。
- **账号隔离（X-I1）**：两个账号的夹具，每个外部工具传入另一账号的 ID 都得 `not_found`。
- **语义一致（X-I4）**：同一画布与资产，内置路径和外部路径的 `read_canvas`、`read_asset`、`search_assets`、`read_library` 结构化结果相同（外部多 `canvas_url`）；运行路径现有测试全部照旧通过。
- **撤销（X-I3）**：断开后下一次 `/mcp` 请求 401，refresh 得 `invalid_grant`。
- **MCP 端到端**（`httpapi` 集成测试，用 SDK 的客户端）：
  - 无令牌 401 与 `WWW-Authenticate`；
  - `initialize` 带 `instructions`；
  - 工具列表是 6 个，带 `title` 与 `readOnlyHint`，没有 `_meta.ui`；
  - 各工具调用，`read_asset` 返回 `ImageContent`；
  - 限流。
- **开关**：`MCP_ENABLED` 关闭时，协议端点 404、列表接口 404。
- **前端**：`make check-frontend`；接入页、确认页、顶栏指示的组件测试按既有测试方式补。e2e 只跑新增用例，换端口避开 owner 的 Vite。
- **宿主实测**：交付后由 owner 在本机的 Claude Code、Claude 桌面端、Codex CLI 上走 AC-C1-1、AC-C1-3、AC-C1-5、AC-C1-6，记录宿主版本与 `host_kind` 识别结果。claude.ai 连接器、ChatGPT 记为限制项。
- **观测**：
  - 每次工具调用记录连接 ID、工具名、耗时、错误码；
  - 授权失败（PKCE、受众、回调不符、重放）单独记录原因和客户端；
  - 日志里没有令牌、授权码和密钥的明文。
- **灰度与回滚**：默认 `MCP_ENABLED=false`。回滚代码不需要回滚数据；回滚迁移在没有连接时才允许。

## 未决事项

| # | 问题 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| U2 | 能否只做 CIMD | `/oauth/register` 是否保留 | owner 实测时看日志里各宿主走的注册方式 | C1 归档前（只会收窄） |
| U3 | Codex 插件的公开仓库与生产域名 | 接入页 Codex 的主路径 | owner 定仓库与域名；在此之前用手动添加 | 插件上线前，不阻塞 C1 |
| U7 | Codex、ChatGPT 是否读取 `instructions`；45 秒等待在各宿主下是否安全 | C2 的长任务 | C1 实测 Codex；ChatGPT 等有域名的环境 | C2 design 批准前 |
| U8 | Team 成员的回退路径 | 回退文字 | 按官方文档写，验收记为限制项 | C1 验收前 |
| U9 | Claude Code 在图片与 `structuredContent` 并存时是否丢 `content` | 带图片结果的形态 | C1 实测 `read_asset` | C1 归档前 |
| 宿主识别 | `host_kind` 规则是否覆盖本机三个宿主 | 署名 | 实测时看日志 | C1 归档前 |
