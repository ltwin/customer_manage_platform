---
risk:
  factors:
    - trust-boundary
    - data-migration
    - broad-impact
behavior: changed
reads:
  - creative/documents
  - creative/library
decisionsPending: false
---
# 本次交付

## Why

本子项是 Epic `creative-mcp-server` 的 C1，按批准基线 v2 实施。它打通「接入页 → 宿主接入 → 授权 → 调到工具」这条最小链路，消除认证与宿主兼容这两个最大的不确定性（roadmap「顺序原则」第 1 条）。承担 Epic 的 AC-1、AC-1a、AC-2、AC-3、AC-9，以及 AC-10 中与接入和只读有关的部分。

- **活原型**（D13）的结论见 Epic `discovery/research/live-prototype.md`。owner 2026-10-22 判断原型体验已够用，指示「直接开工，完工后用真实场景检查效果」。没有测到的 Codex CLI、ChatGPT，以及 U7 的超时，在本子项用真实服务复测。
- **验收方式**：owner 在真实宿主里接入并读取自己的画布。本机能直接测的是 Claude Code、Claude 桌面端（Code 模式，共用 Claude Code 的本机配置）和 Codex CLI。claude.ai 连接器和 ChatGPT 需要公网 https 地址，等部署到有域名的环境后再测，届时按 roadmap 的验收责任补记。

## What Changes

1. **OAuth 授权服务器**（`api: OAuth 授权服务器`、`data: 外部助手连接与 OAuth`）：
   - 两个元数据端点；
   - 客户端注册：CIMD 与 DCR 都支持，DCR 接受公共客户端和机密客户端；
   - 授权请求与确认页、授权码加 PKCE、令牌签发、refresh 轮换与重放断开。
2. **MCP 端点**（`api: MCP 端点`）：`<PUBLIC_BASE_URL>/mcp`，用官方 Go SDK 实现。
   - 每个请求校验令牌和连接状态，连接断开后立即 401。
   - 下发服务端 `instructions`（C1 版本只写已提供的工具）。
   - 按连接限流；节流更新最近使用时间。
3. **只读工具**（`api: MCP 只读工具`）：`get_capabilities`、`list_canvases`、`read_canvas`、`read_asset`、`search_assets`、`read_library`。
   - 和内置助手同名的工具走同一个领域入口、同一套错误；可读判定改为账号级（授权即 consent）。
   - 结果是「摘要 + 结构化结果」，`read_asset` 的图片以 `ImageContent` 返回。
4. **外部助手连接**（`glossary: 外部助手`、`glossary: 外部助手连接`、`architecture: 外部助手接入`）：外部连接是与运行并列的助手主体。C1 只接入读路径需要的准入、画布和可读判定。
5. **连接列表与断开**（`api: 外部助手连接列表与断开`）：网页端接口，断开在一个事务里作废令牌链。
6. **前端**：
   - 「外部助手」接入页（创意空间侧栏入口）：选宿主，复制接入文字，展开回退方法；下方是连接列表和断开。
   - 授权确认页。
   - 画布页顶栏的「外部助手已连接」指示。
7. **接入物料**：
   - 四个宿主的接入文字与 Team 成员的回退文字；
   - Codex 插件目录 `integrations/codex-plugin/`；
   - 服务端 `instructions` 的外部版本。
8. **开关**：`MCP_ENABLED` 关闭时，`/mcp`、`/oauth/*` 和两个元数据端点都不挂载，接入页显示「暂未开放」。

不在本子项：写入与生成工具、`wait_tasks`（C2）；卡片（C2b）；场景与 Skill 工具、场景历史署名（C3）。

## Acceptance Criteria

- **AC-C1-1 接入**：**WHEN** 创作者在接入页选 Claude Code（或 Codex CLI），把接入文字发给它，**THEN** 宿主完成注册、打开浏览器授权页；创作者已登录时直接看到确认页，未登录时先登录再回到确认页；点「允许」后浏览器停在完成页，宿主随即能列出 6 个工具。
- **AC-C1-2 续期**：**WHEN** 访问令牌过期（1 小时），**THEN** 宿主用 refresh token 换到新令牌，不需要再次授权。**WHEN** 一个已轮换的 refresh token 被再次使用，**THEN** 整个连接被断开，宿主需要重新授权。
- **AC-C1-3 读**：**WHEN** 创作者说「看看我最近的画布」，**THEN** 外部助手调用 `list_canvases` 列出画布（带可点击的画布链接），用 `read_canvas` 读到节点，用 `read_asset` 看到引用图片，用 `search_assets`、`read_library` 查找资产库。**WHEN** 调用 `get_capabilities`，**THEN** 得到账号显示名、连接署名、可用的生成模型、本月剩余额度和有没有打开的创意空间页面。
- **AC-C1-4 隔离与分域**：**WHEN** 外部助手传入其他账号的 `canvas_id` 或内容修订 ID，**THEN** 结果是 `not_found`，看不到任何内容。**WHEN** 用网页端令牌调 `/mcp`，或用 MCP 令牌调 `/api/v1/*`，**THEN** 都是 401。
- **AC-C1-5 感知与断开**：**WHEN** 外部助手在最近 5 分钟内调用过工具，**THEN** 画布页顶栏显示「Claude Code 已连接」。**WHEN** 创作者在接入页断开这个连接，**THEN** 它的下一次请求是 401，接入页的列表显示已断开，顶栏的指示消失。
- **AC-C1-6 文字**：**WHEN** 在 Claude Code 或 Codex CLI 里调用任一工具，**THEN** 结果第一行能单独看懂，画布链接是可点击的 Markdown 链接，结果里没有卡片字段。
- **AC-C1-7 开关**：**WHEN** `MCP_ENABLED` 未开启，**THEN** `/mcp`、`/oauth/*` 与两个元数据端点都是 404，接入页显示暂未开放，内置助手不受影响。
- **AC-C1-8 回退文字**：接入页的 Claude 桌面端和 ChatGPT 标签页给出「发给管理员」的文字，Claude 桌面端另给「改用 Claude Code」的路径。没有 Team 账号，实测记为限制项（roadmap「验收责任」）。

## Impact

- 后端：新增 `backend/internal/platform/oauth`、`backend/internal/creativemcp`；扩展 `backend/internal/creativeagent`（外部工具门面、读工具的可读判定接口）、`backend/internal/platform/httpapi`（挂载与连接接口）、`backend/internal/platform/store`（OAuth 存取、迁移 `0080`）、`backend/internal/platform/llmgateway`（本月剩余额度的只读查询）、`backend/internal/platform/config`（`MCP_ENABLED`）。新依赖 `github.com/modelcontextprotocol/go-sdk` v1.8.0。
- 契约：`api/openapi.yaml` 增加连接列表、断开、确认授权三个网页端接口，重新生成 Go 与 TS。
- 前端：接入页、授权确认页、顶栏指示、侧栏入口；Vite 开发代理增加 `/mcp`、`/oauth/`、`/.well-known/oauth-*`。
- 接入物料：`integrations/codex-plugin/`。
- 内置助手的行为不变：读工具抽出共用核心后，运行路径的可读判定与结果保持原样。

## Risk Notes

- **安全**：这是第一个对第三方客户端开放的认证面。重点在回调地址校验、PKCE、一次性授权码、refresh 重放、CIMD 拉取的 SSRF 防护、两种令牌互不通用。每一项都有单测与集成测试（design「验证与交付」）。
- **反向代理与本机保护**：SDK 默认拒绝「从本机地址进来、Host 不是本机」的请求，以防 DNS 重绑定。生产在反向代理后面，请求都来自本机，会被误拒。MCP 端点有 OAuth 保护，关掉这项保护（design「局部设计与偏差」）。
- **Codex 插件**：公开 marketplace 仓库（U3）和生产域名都还没有定。插件文件按设计做好，接入页的 Codex 文字先给手动添加的命令，插件可用后再切换（design「局部设计与偏差」）。这不改变接入方式的目标，只是推迟主路径的上线。
- **网页宿主**：本机没有公网 https，claude.ai 连接器、ChatGPT 的接入在本子项只按官方文档写接入文字，实测留到有域名的环境，验收时记为限制项。
