# 竞品调研：LibTV Plugin（2026-10-22）

## 来源与局限

- 页面 `https://www.liblib.tv/plugin`：Browser MCP 截图和无障碍快照。宿主和接入方式的切换点击一直超时，只看到默认的「OpenAI + Plugin」视图。
- 公开插件仓库 `https://github.com/liblib-ai/marketplace`（浅克隆，当日 main）：README、`.agents/plugins/marketplace.json`、`plugins/libtv/.codex-plugin/plugin.json`、`.mcp.json`、3 个 Skill。
- 公开端点：`https://mcp.liblib.tv/.well-known/oauth-protected-resource[/mcp]`、`/.well-known/oauth-authorization-server`、`/docs`，以及未鉴权 `POST /mcp` 的 401 响应。
- 没有登录，所以没有拿到 `tools/list` 和服务端 `instructions` 原文。工具清单只能从 Skill 文本反推。

## 事实

### 接入页（站内「插件」页）
- 侧边导航与「项目」「资产」并列有一个「插件」入口。主标题为「LibTV Plugin，让你的 Agent 开始创作」，附飞书使用指南链接。
- 宿主选择：OpenAI、Claude、Claude Code、OpenClaw、Hermes、CodeBuddy。接入方式：Plugin / CLI。
- 两步：
  1. 「复制并发送以下内容给你的 Agent」：一段自然语言加安装命令，让 Agent 自己装插件。原文：「帮我安装 LibTV Plugin……1. 添加插件目录：执行 `codex plugin marketplace add https://github.com/liblib-ai/marketplace.git --ref main` 2. 安装插件：执行 `codex plugin add libtv@libtv` 3. 新建任务后完成 LibTV 账户授权」。
  2. 「连接账户，开始创作」。
- 下方是案例演示，分三个标签：全模型调用生成、多资产管理提案、可控多场景生成。演示内容是 Agent 的对话流加生成结果视频。
- 视觉：深色、单卡片、宿主图标横排，和站内其他页面一致。

### 插件仓库
- 公开 GitHub 仓库，只有一个生产插件；测试包和预发包已撤下（README：按环境拆包会和残留的 `mcp_servers.libtv` 配置冲突）。
- 只有 Codex 格式：`.codex-plugin/plugin.json` 加 `.agents/plugins/marketplace.json`。marketplace 条目里 `policy.authentication: "ON_INSTALL"`，安装时就触发授权；`category: "Creativity"`。
- manifest 的 `interface` 字段：`displayName`、`shortDescription`、`longDescription`（能力的长段介绍）、`capabilities: [Read, Write, Interactive]`、`brandColor`、`logo`、`defaultPrompt`（3 条起步提示，例如「查看我在 LibTV 中的项目」）。
- `.mcp.json`：`{"type":"http","url":"https://mcp.liblib.tv/mcp","tool_timeout_sec":1200}`。用插件配置把 Codex 的工具超时拉到 20 分钟。不预置 client ID、回调或凭据。
- 每个 Skill 附 `agents/openai.yaml`，写 Codex 界面用的 `display_name`、`short_description`、`default_prompt`。

### 授权
- 独立域名 `mcp.liblib.tv`（istio 网关，服务名 `libtv-remote-mcp-gateway`）。
- 授权服务器元数据：有 `registration_endpoint`（DCR），没有声明 CIMD；有 `revocation_endpoint`；`token_endpoint_auth_methods_supported` 同时支持公共客户端和机密客户端；PKCE 只支持 S256。
- 6 个细粒度 scope：`libtv:account:read`、`libtv:project:read`、`libtv:project:write`、`libtv:media:write`、`libtv:task:write`、`libtv:export:write`。
- 未鉴权的 401 带 `WWW-Authenticate: Bearer error="invalid_token", resource_metadata=".../.well-known/oauth-protected-resource/mcp"`。

### 服务端约定（从 Skill 反推）
- 服务端 `instructions` 被当作「运行契约」：登录、项目、花费、监控、校验、浏览器交接的规则都写在里面。Skill 要求每次先读它。
- 有 `get_capabilities` 工具和 `doctor=ready` 一类的自检状态，用来确认工具来源和运行状态。
- 浏览器交接：修改项目前，必须先在 Codex 右侧的内置浏览器面板里打开这个画布，并在整个过程中保持打开。交接链接可带聚焦节点；做不到时返回画布链接、标明「可见性未验证」并停止。
- 花费与重试：付费生成、重试、覆盖、删除、上传前，确认当前请求是否授权；生成前做一次只读预检（模型、模式、参考、输出设置、提示词）；盯住具体的任务 ID，轮询结果没变化时不重复提交。
- 产品内还有「Canvas Skill」（`skillDecision=skipped_by_user` 等字段），和插件里随包公开的工作流 Skill 是两套。
- 随包的工作流 Skill 主要编排外部助手本机能做的事：Blender 白模运镜、在 Zen Piano 上演奏录音（macOS Swift 脚本）、本地生成 HTML 导演提案。产品里的生成只是其中一步。

## 对本 Epic 的启发（待 owner 取舍）

| # | 借鉴点 | 对应本 Epic | 建议 |
|---|---|---|---|
| L1 | 站内「插件」接入页：选宿主 → 复制一段话让 Agent 自己安装 → 授权；附案例 | 现设计只有设置里的连接列表，没有接入页 | 采纳，放进 C1 |
| L2 | 服务端 `instructions` 当运行契约，加 `get_capabilities` 自检工具 | 现设计把使用说明放在插件 Skill 里，图形界面宿主读不到 | 采纳。自检同时报告「有没有打开的创意空间页面」，正好服务 D2 |
| L3 | 浏览器交接：返回画布链接（可带聚焦节点），Codex 桌面端可以在内置浏览器里打开 | `list_canvases` 已有 `url`，场景任务有 `waiting_for_page` | 采纳为统一规则：写工具的结果都带画布链接 |
| L4 | 插件 `.mcp.json` 设 `tool_timeout_sec` | 现设计靠 45 秒句柄避开 Codex 的 60 秒超时 | 两者都要：插件把超时调大，句柄机制保留给手动配置的宿主（ChatGPT、Claude connector） |
| L5 | 生成前只读预检 | 现设计没有 | 可选：`generate_node` 加 `dry_run`，返回模型、参考和预计扣费 |
| L6 | 随包公开的工作流 Skill，编排本机能力（Blender、本地文件、HTML） | D3 只说平台 Skill 不打包；插件只有一份工具说明 | 远期：外部助手的独特价值在本机能力，插件可以带公开的工作流 Skill，和平台 Skill 不冲突 |
| L7 | 细粒度 scope | D5 只有一个 `creative` scope | 不采纳，维持 D5；元数据里已为以后的细粒度授权留了位置 |
| L8 | 只发一个生产包，不按环境拆包 | 现设计没提环境 | 采纳：插件只指向生产地址，本地开发用宿主的手动配置覆盖 |
| L9 | 宿主覆盖国内 Agent（OpenClaw、Hermes、CodeBuddy）和 Claude 桌面端 | 现设计只有 Claude Code 和 Codex | 跟随「是否纳入图形界面宿主」一起决定 |
