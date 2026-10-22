# 创意空间 MCP 服务与 Claude Code / Codex 插件

> 状态：探索中（2026-10-21）。本文件保存当前共识，不是批准的设计。

## 原始诉求

实现 MCP 服务，并支持 Claude Code 和 Codex 插件，让外部编码助手拥有内置创意助手（creative agent）几乎全部能力。

## 已核实事实

### 内置助手的能力面

工具注册集中在 `backend/internal/creativeagent/tool_registry.go:23`（`agentToolDefinitions`），当前 28 个工具，按性质可分四组：

| 组 | 工具 | 对 MCP 的意义 |
|---|---|---|
| 领域读写 | 画布读/改、写提示词、生成、压缩图片、存入资产库、资产库查找/读结构/整理/管理/回收站/恢复/读整理结果、读引用素材 | 直接对应领域命令，MCP 可等价暴露 |
| 场景 | read_scene、edit_scene、write_model、render_view、export_shots | write_model / render_view / export_shots 依赖**浏览器渲染端**执行的客户端任务（见下） |
| Skill | load_skill、read_skill_resource、search_skills | 涉及 Skill 指令是否离开服务端 |
| 运行内部 | update_plan、read_history、read_run_result、find conversation assets | 绑定内置助手的「运行/对话」，外部助手有自己的计划与上下文 |
| 网页 | search_web、read_webpage | 外部助手自带同类能力 |

- 每个工具都绑定在 `runtimeHandler` 上（`creativeagent/runtime.go:51`）：一次运行固定一个画布（`creative_agent_runs.canvas_id`）、模型、Skill、consent、资源锁（`resources`）、失败计数和日志。MCP 调用没有「运行」，需要另一种执行上下文。
- 工具 schema 已由公开路由派生（`backend/cmd/creative-tool-schema-gen`：「No second hand-maintained input/output schema」），即工具输入输出本就是 OpenAPI 的投影。
- **场景客户端任务**（`lti contract show data --section 场景客户端任务`）：`bake_model` / `render_view` / `export_shots` 由创意空间页面认领执行，30 分钟 deadline，`run_id` 可为空（摄影师发起的导出即如此）。没有打开的页面，这三类任务不会完成。
- 认证：10 分钟 JWT access token + `__Host-crm_refresh` cookie（`platform/auth/token.go:12`、`httpapi/auth.go:38`），没有面向第三方客户端的令牌或 OAuth。
- 现有 Skill 约定：Skill 正文只在服务端加载，`SkillVersionView` 注释写明「the instructions and the storage addresses stay on the server」（`creativeagent/catalog.go:48`）。平台 Skill 目前 2 个：`reference-direction.v1`、`cosplay-shoot-planning.v1`（`deploy/creative-skills/`）。
- 既有产品文档「首期不开放通用 MCP 发现」（`docs/product/creative-canvas-system/eino-adoption.md:57`）指内置助手**消费** MCP，不涉及对外**提供** MCP。

### 宿主侧（2026-10 外部资料）

- Claude Code 插件：`.claude-plugin/plugin.json`，MCP 服务器可放插件根 `.mcp.json`，支持 `http` 类型远程服务器与 OAuth。
- Codex 插件（2026-03 上线）：`.codex-plugin/plugin.json`，同样打包 skills（SKILL.md）与 MCP 服务器（`.mcp.json`），支持仓库级 / 个人级 marketplace。
- 结论：一个插件仓库可同时带两份 manifest，共用 `.mcp.json`（指向远程 `https://<域名>/mcp`）和 `skills/`。精确字段以两家官方文档为准，进入设计时再核对。

## Owner 决定（2026-10-21 第 1 轮）

- **D1 目标场景**：专业创作者协同。创作者开着浏览器里的创意空间，在 Claude Code / Codex 里让外部助手读写画布、生成、搭场景，页面实时同步。不以无人值守批量为首要场景。
- **D2 场景渲染**：要求页面开着。建模烘焙、回看截图、导出分镜照旧派客户端任务，由打开的页面认领；没有可认领的页面时，返回明确提示和要打开的链接，不做服务端无头渲染。
- **D3 Skill 正文**：经 MCP 按需下发。load_skill / read_skill_resource 对登录账号可用，正文进入外部模型上下文；平台 Skill 不打包进公开插件。需修订 `catalog.go:48` 那条「正文不离开服务端」的约定：改为不经浏览器接口暴露，经已授权的 MCP 会话可读。

## Owner 决定（2026-10-21 第 2 轮）

- **D4 改动呈现**：外部助手的改动实时同步到页面，修订历史署名为「Claude Code」或「Codex」，可以回退；页面显示「外部助手已连接」指示。不镜像到内置助手的对话面板。
- **D5 开放范围**：与内置助手相同，有创意能力的账号就能连接。生成照常扣积分，外部助手的对话 token 平台不收费。
- **D6 排除项**：网页组（search_web、read_webpage）和运行内部组（update_plan、read_history、read_run_result、找回对话内容）都不暴露。其余 22 个工具全部经 MCP 提供。

## Owner 决定（2026-10-22 第 3 轮，Epic 起草后）

- **D7 宿主范围**：图形界面宿主纳入本 Epic。首批只做 4 个宿主：ChatGPT、Codex CLI、Claude Code、Claude 桌面端（含网页）。国内宿主（OpenClaw、Hermes、CodeBuddy 等）和 Codex 桌面 app、IDE 扩展不在首批验收内。
- **D8 Claude 系列只用连接器**：参照 LibTV，Claude 桌面端、网页和登录 claude.ai 账号的 Claude Code 共用账号里的一个连接器，不做 Claude 插件；使用说明全靠服务端 `instructions`。必须有回退路径：Team 成员没有创建连接器的权限时，Claude Code 用本机 `claude mcp add`，桌面端给一段「发给管理员」的文字。
- **D9 借鉴 LibTV**（见 `research/libtv-plugin.md`）：采纳 L1 接入页、L2 `get_capabilities` 自检、L3 画布交接链接、L5 生成预检 `dry_run`。L4（Codex 插件调大工具超时）和 L8（只发一个生产包）按技术建议采纳。不采纳 L7 细粒度 scope（维持 D5）。预检是 Agent 的可选操作：服务端说明只介绍它的用途，不强制；创作者希望全自动执行时，Agent 可以不再预检（owner 2026-10-22 补充）。

## Owner 决定（2026-10-22 第 4 轮，宿主交互面调研后）

见 `research/host-ui-surfaces.md`。先做原型设计，再修订 Epic。

- **D10 内嵌卡片**：图形界面宿主（ChatGPT、Claude 桌面端和网页）采纳 MCP Apps 内嵌卡片，两家共用一套。文字加链接仍是所有宿主的基础，Codex CLI 和 Claude Code 只有文字加链接。
- **D11 首批卡片**：生成与任务卡（`generate_node` 含预检、`wait_tasks`），以及镜头卡（`render_view`、`export_shots`）。不做画布卡、`show_canvas`、资产库卡片。
- **D12 子项归属**：C2 之后单列 `mcp-host-ui`（卡片通信基础 + 生成与任务卡）；镜头卡随 C3。
- **D13 原型**：静态 HTML 原型 + 假数据活原型（接真实宿主验证渲染与超时）；对外暴露的隧道开之前单独确认。

## 工作假设（进入设计时由我决定，必要时再提）

- **A1 部署形态**：远程 streamable HTTP MCP，挂在现有 Go server 下（`/mcp`），复用账号隔离与领域服务；不做本地 stdio 进程。理由：SaaS 定位，账号数据都在服务端。
- **A2 认证**：MCP 规范的 OAuth 2.1（PKCE + 动态客户端注册），授权页复用现有登录；令牌按账号、可在设置里撤销。
- **A4 执行上下文**：每个 MCP 会话按「外部助手」写入者落修订，历史里可见、可回退，符合人机协同撤销原则；资源锁与影响上限沿用工具定义。
- **A5 长任务**：生成与场景任务异步化：工具立即返回任务句柄，另给等待/查询工具（单次长轮询有上限），避免宿主工具超时。

- **A6 插件形态**（被 D8 修订）：只做 Codex 插件；Claude 系列走连接器，ChatGPT 走 Developer Mode 应用。使用说明以服务端 `instructions` 为准。

## 未决项（设计阶段解决，不阻塞收敛）

- OAuth 细节：是否开放动态客户端注册，还是只预注册 Claude Code / Codex 两个客户端；令牌有效期；在账号设置里列出和撤销已授权的连接。
- MCP 服务端用哪个库（官方 Go SDK 或自写 streamable HTTP），用 context7 核对当前版本。
- 外部会话的执行上下文怎么复用 `agentToolDefinitions`：资源锁、影响上限、consent 在没有「运行」时的等价物。
- 异步工具的等待上限、限流，以及每个账号同时能连几个外部会话。
- 生成结果怎么回传：MCP image content 还是资源链接，图片尺寸上限。
- 需要修订的既有约定：`catalog.go:48` 的 Skill 正文边界（D3）；修订写入者模型加「外部助手」类型（D4）。
