# 宿主实测（任务 8.1）

2026-10-22，在 worktree 构建的服务上跑：`MCP_ENABLED=true`，`PUBLIC_BASE_URL=http://localhost:18080`，独立的 PostgreSQL 容器，一个测试账号和一张有 2 个文字节点的画布。浏览器由 Playwright 驱动，登录和确认都在真实页面上完成。

## 已跑通

| 场景 | 宿主 | 注册方式 | `host_kind` | 结果 |
|---|---|---|---|---|
| 登录 → 确认 → 回调 | Codex CLI 0.161.0（`codex mcp login --no-browser`，独立的 `CODEX_HOME`） | CIMD，`client_id=https://chatgpt.com/oauth/codex/client.json`，`client_name=Codex` | `codex` | 浏览器停在 Codex 自己的回调页「Authentication complete」，`codex mcp list` 显示 `Auth: OAuth` |
| AC-C1-1、3、4（部分）、5，AC-C1-2 的协议部分 | 模拟宿主（Node 脚本，直接发 JSON-RPC） | DCR，回调端口与注册时不同 | `claude_code` | 全部通过，见下 |

模拟宿主依次检查了这些点：

- 授权：回调带 `state` 和 `iss`，授权码换令牌成功。
- 列出工具：`initialize` 带 `instructions`，`tools/list` 列出 6 个工具。
- 读取：
  - `get_capabilities`、`list_canvases`、`read_canvas`、`search_assets`、`read_library` 都返回结果；
  - `list_canvases` 和 `read_canvas` 的画布链接是可点击的 Markdown 链接；
  - 不存在的画布返回 `not_found`。
- 分域：MCP 令牌调 `/api/v1/me` 是 401。
- 顶栏与断开：
  - 调用后，画布顶栏显示「Claude Code 已连接」；
  - 在接入页断开后，该行显示「已断开」，下一次 `/mcp` 请求是 401，`WWW-Authenticate` 带 `resource_metadata`，顶栏指示随之消失。
- refresh：
  - 换出的新令牌可用，refresh token 已轮换；
  - 重放旧 refresh token 得到 `invalid_grant`，整个连接被断开，新访问令牌也变成 401。

走 Go SDK 客户端的完整链路由 `backend/internal/platform/httpapi/mcp_test.go` 覆盖（`-race`）。

Codex 这一遍没有让它真正调用工具：调用要跑模型，会花 owner 的额度。工具调用由模拟宿主覆盖。

## 实测中发现并已修的问题

- **接入页断开后，连接直接从列表里消失。** AC-C1-5 要求列表显示「已断开」。现在断开的那一行留在原处显示「已断开」，下一次刷新列表时才移走（`AgentsPage.tsx`）。
- 测试账号是手工激活的，邮箱身份没有标记已验证，`/api/v1/me` 因此返回 500，登录页显示「网络错误」。这是测试数据的问题，不是产品缺陷：正常的邮箱验证会同时写 `verified_at`。

## 没有测到

- 读取图片：测试画布没有图片节点。图片的缩放和大小上限有单元测试覆盖（`creativemedia/fit_preview_test.go`、`creativeagent/external_test.go`）。
- 访问令牌真实过期（1 小时）后宿主自己续期：协议部分已测；Claude Code、Codex 的自动续期需要实际用满一小时。
- claude.ai 连接器、ChatGPT：授权由它们的服务端发起，需要公网 https，本机测不了（AC-C1-8 记为限制项）。

## owner 实测步骤

先在本机后端 `.env` 里设 `MCP_ENABLED=true`，然后照常起后端和 `npm run dev`。地址用 `http://localhost:5173/mcp`。如果之前加过原型的 `creative-proto`，先删掉，免得混淆。

### Claude Code（终端）

1. `claude mcp add --transport http --scope user creative http://localhost:5173/mcp`
   - 加 `--scope user` 会写进 `~/.claude.json`，所有项目都能用；不加只对当前目录生效。
2. 进 Claude Code 输入 `/mcp`，选 `creative` → Authenticate。浏览器会打开授权页：没登录先登录，然后点「允许」。
3. 说「看看我最近的画布」。在浏览器里打开返回的画布链接，顶栏应显示「Claude Code 已连接」。
4. 在「创意空间 → 外部助手」断开。再让它读一次，应提示需要重新授权。

### Claude 桌面端（Code 模式）

和 Claude Code 共用 `--scope user` 的配置，不用另加。步骤 2 到 4 相同，署名同样是「Claude Code」。

### Codex CLI

1. `codex mcp add creative --url http://localhost:5173/mcp`
   - 这条命令写全局的 `~/.codex/config.toml`，添加后会立即开始登录并打开浏览器。
   - 如果没打开，运行 `codex mcp login creative`。
2. 在浏览器里点「允许」。页面会停在 Codex 的「Authentication complete」。
3. 在 Codex 里说「看看我最近的画布」，然后按 Claude Code 的步骤 3、4 检查。

### 记录

每个宿主跑完，在下表补一行。`host_kind` 和注册方式看后端日志里的 `oauth authorize request`。

| 日期 | 宿主与版本 | 注册方式（`registration`） | `client_name` | `host_kind` | 读到 `instructions` | 顶栏署名 | 问题 |
|---|---|---|---|---|---|---|---|
| 2026-10-22 | Codex CLI 0.161.0（只做了登录） | cimd | Codex | codex | 未测 | — | — |
| 2026-10-23 | Claude Code（owner 实测，版本待补） | cimd（`https://claude.ai/oauth/claude-code-client-metadata`） | Claude Code | claude_code | 待补 | 待补 | 第一次认证后调用工具全部提示重新登录：重新授权建了新连接，Claude Code 沿用旧会话，会话绑连接 ID 导致 403；已改为按账号绑定（f56eeec7）。再认证一次后可用 |
|  | Claude 桌面端（Code 模式） |  |  |  |  |  |  |
|  | Codex CLI（调用工具） |  |  |  |  |  |  |
