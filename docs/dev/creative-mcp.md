# 外部助手：MCP 端点与 OAuth

Epic `creative-mcp-server`。Claude Code、Codex、Claude（桌面端与网页）、ChatGPT 经远程 MCP 端点使用创意空间的工具，经本系统自带的 OAuth 授权服务器取得令牌。规格见 `.lti/features/2026-10-21-creative-mcp-server/`。

## 开关

`MCP_ENABLED=true` 时挂载：

| 路径 | 作用 |
|---|---|
| `/.well-known/oauth-protected-resource/mcp` | 受保护资源元数据（RFC 9728） |
| `/.well-known/oauth-authorization-server` | 授权服务器元数据（RFC 8414） |
| `/oauth/register` | 动态客户端注册（DCR） |
| `/oauth/authorize` | 授权请求；校验后跳到前端确认页 `/oauth/consent` |
| `/oauth/token` | 授权码换令牌、refresh |
| `/mcp` | MCP streamable HTTP |
| `/api/v1/creative/agent-connections` | 网页端：连接列表与断开 |
| `/api/v1/oauth/authorize/decision` | 网页端：确认页的「允许」「拒绝」 |

缺省关闭：协议端点都是 404，连接列表也是 404，接入页显示「外部助手暂未开放」。不需要额外的密钥：访问令牌和授权请求包的签名密钥都从 `AUTH_TOKEN_SECRET` 按用途派生，网页端令牌和 MCP 令牌互不通用。

地址全部来自 `PUBLIC_BASE_URL`：资源是 `<PUBLIC_BASE_URL>/mcp`，令牌的 `aud` 就是它。本机开发时 `PUBLIC_BASE_URL=http://localhost:5173`，Vite 把 `/mcp`、`/oauth/{authorize,token,register}`、`/.well-known/oauth-*` 转发到后端（`frontend/vite.config.ts`），`/oauth/consent` 留给前端。

## 代码位置

| 包 | 职责 |
|---|---|
| `backend/internal/platform/oauth` | 授权服务器：注册（DCR、CIMD）、授权请求与请求包、授权码 + PKCE、令牌签发与校验、refresh 轮换与重放断开、连接 |
| `backend/internal/platform/store/oauth.go` | 上面的存储；迁移 `0080_external_agent_oauth` |
| `backend/internal/creativemcp` | 官方 Go SDK 的 server：工具注册、结果转换、按连接限流 |
| `backend/internal/creativeagent/external*.go` | 外部工具门面：工具目录、调用入口、错误映射、摘要 |
| `backend/internal/creativeagent/read_access.go` | 读工具的可读判定接口：运行和外部连接各一份实现 |
| `backend/internal/creativeagent/instructions/external.md` | 服务端 `instructions`（外部版本） |
| `backend/internal/platform/httpapi/{oauth,mcp,agent_connections}.go` | HTTP 适配 |
| `frontend/src/creative-canvas/agents/` | 接入页 |
| `frontend/src/pages/auth/AgentConsentPage.tsx` | 授权确认页 |
| `frontend/src/creative-canvas/editor/AgentConnectionBadge.tsx` | 画布顶栏「已连接」 |
| `integrations/codex-plugin/` | Codex 插件（见其中的 README） |

新增外部工具时：在 `creativeagent/external.go` 的 `externalCatalog` 加一项，同名工具复用内置工具的 `Info()` 和领域入口，只在 `appendix` 写外部差异；摘要第一行是结论；同步更新 `instructions/external.md`。

## 本机接入

后端 `MCP_ENABLED=true` 起服务，前端照常 `npm run dev`。下面的地址按本机的 `PUBLIC_BASE_URL`。

| 宿主 | 添加 | 授权 | 删除 |
|---|---|---|---|
| Claude Code | `claude mcp add --transport http --scope user creative http://localhost:5173/mcp` | 进 Claude Code 输入 `/mcp`，选 `creative` | `claude mcp remove --scope user creative` |
| Claude 桌面端（Code 模式） | 与 Claude Code 共用 `--scope user` 的配置 | 同上 | 同上 |
| Codex CLI | `codex mcp add creative --url http://localhost:5173/mcp` | `codex mcp login creative` | `codex mcp remove creative` |

`--scope user` 写进用户级配置（`~/.claude.json`），所有项目都能用；不加时只对当前目录生效。Codex 的 `mcp add` 本来就写全局的 `~/.codex/config.toml`。

claude.ai 连接器（Claude 桌面端与网页的「添加自定义连接器」）和 ChatGPT 应用由各自的服务端发起授权，需要公网 https 地址，本机测不了。

## 排错

- **宿主一直提示需要授权**：看 `/mcp` 的 401 是否带 `WWW-Authenticate: Bearer resource_metadata="…"`；元数据里的 `resource` 必须与宿主填的地址逐字相同（含 `http`/`https`、端口）。
- **授权页显示「发起连接的客户端无法识别」**：DCR 客户端不存在（换过数据库），或 CIMD 地址拉取失败（只允许公网 https、3 秒、5 KB）。
- **「回调地址不在它登记的范围内」**：回环地址只忽略端口，路径必须一致。
- **断开后宿主还在用**：访问令牌每次请求都查连接状态，断开后下一次请求就是 401；若没有，检查是否连的是另一个环境。
- **限流**：每个连接每分钟 120 次工具调用，超出返回 `rate_limited` 和 `retry_after_s`。DCR 每个 IP 每小时 20 次。两者都在进程内存里计数。

## 日志

| 消息 | 字段 |
|---|---|
| `oauth client registered` | `client_id`、`client_name`、`auth_method`、`redirect_hosts` |
| `oauth authorize request` | `client_id`、`client_name`、`registration`、`redirect_host`、`host_kind` |
| `oauth authorize refused` / `oauth token refused` | `reason`、`client_id`、`connection_id` |
| `oauth connection granted` / `oauth connection revoked` | `connection_id`、`client_id`、`host_kind` |
| `external agent tool call` | `connection_id`、`tool`、`outcome`、`duration` |
| `external agent tool failed` | `ref`（返回给外部助手的编号）、`error` |

日志里没有令牌、授权码和客户端密钥的明文。宿主识别规则（`oauth/hostkind.go`）按 `oauth authorize request` 的字段修正。
