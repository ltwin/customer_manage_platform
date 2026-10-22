# Roadmap

## 子项

串行推进（policy: serial）：前一项归档并合回 `develop` 后，下一项才进入实施；可以提前准备设计。

顺序原则：先打通「接入页 → 宿主接入 → 授权 → 调到工具」这条最小链路，尽早消除认证和四个宿主兼容这两个最大的不确定性；再接入有副作用的写入和生成；最后做依赖页面渲染的场景工具和 Skill。

| # | 子项 ID（暂定） | 本批成立的场景 | 消除的不确定性 | 依赖 | 交付契约 |
|---|---|---|---|---|---|
| C1 | `mcp-connect-read` | 创作者在接入页选宿主、复制文字，在 ChatGPT、Codex CLI、Claude Code、Claude 桌面端完成接入和授权（含 Team 成员的回退路径）；外部助手能调用 `get_capabilities` 自检，列出画布、读画布、看引用素材的图片、查找和浏览资产库；服务端 `instructions` 生效；接入页能看到连接并断开；创意空间显示「外部助手已连接」 | OAuth 授权服务器能否被四个宿主无缝使用：客户端注册方式、本机回调与网页回调、机密客户端、续期、受众校验；官方 Go SDK 与 Gin 的集成；图形界面宿主对工具说明、`instructions` 和图片结果的展示；「外部助手连接」主体能否在读路径上替换运行的 admit 和 consent | 无 | `architecture: 外部助手接入`、`glossary: 外部助手`、`glossary: 外部助手连接`、`data: 外部助手连接与 OAuth`、`api: OAuth 授权服务器`、`api: MCP 端点`、`api: 外部助手连接列表与断开`、`api: MCP 只读工具` |
| C2 | `mcp-write-generate` | 外部助手修改画布、写提示词、生成图片并看到结果；可以先用 `dry_run` 预检（可选）；整理资产库（整理、管理结构、回收站、恢复、存入资产库）；压缩图片；工具结果带画布交接链接；长任务经 `wait_tasks` 续等；生成按摄影师规则扣额度 | 资产库多阶段写入在没有运行时的恢复（日志改挂外部连接）；异步句柄在各宿主默认超时下的可用性（ChatGPT 的超时未知）；画布写入署名与 `canvas-undo-own-changes` 的衔接 | C1 | `data: 通用文档`（MODIFY，写入者增加外部助手）、`api: MCP 写入与生成工具` |
| C3 | `mcp-scene-skill` | 页面开着时，外部助手读场景、改场景、写模型、回看、导出分镜；没有页面时任务排队并提示打开链接；历史里外部助手的场景修订署名为宿主名（Claude、Claude Code、Codex、ChatGPT）并可恢复；外部助手查找、加载 Skill 并按 Skill 策划 | 场景工具「自上次写入后的变化」与「撤回自己的改动」以连接为单位是否成立；客户端任务不挂运行时的完成准入；外部模型执行平台 Skill 的效果 | C2 | `data: 场景客户端任务`（MODIFY）、`api: 列出文档修订`（MODIFY）、`api: MCP 场景与 Skill 工具` |

### 接入物料随子项演进

C1 建立全部接入物料：
- Codex 插件：manifest、MCP 配置，调大工具超时，只发生产包。
- 四个宿主的接入文字，以及 Team 成员的回退文字。
- 服务端 `instructions`。

C2、C3 只在 `instructions` 和工具说明里补充新工具的用法，不改接入方式。

### 与其他工作的衔接

- `canvas-undo-own-changes`（独立 change，草稿）计划给 `creative_changes` 加写入者列。画布署名归它负责：它先归档时，C2 在它的写入者枚举里加 `external_agent`；C2 先实施时，画布写入暂不署名（画布没有历史列表界面，不影响 AC-7 的场景部分），由 `canvas-undo-own-changes` 落地时把外部连接一并纳入。C2 设计阶段按当时状态二选一，并在 C2 proposal 里写明。
- 积分账（credits，独立分支）尚未落地。外部助手的生成走摄影师同款扣费入口，积分落地后自动生效，本 Epic 不另做计费。

### 验收责任

- 每个子项按自己的 proposal 验收，由 owner 接受后归档。
- 以下几项需要 owner 在真实宿主里亲自走一遍，技术检查通过不等于体验通过：
  - C1：四个宿主的接入与回退路径（AC-1、AC-1a、AC-10）。
  - C3：Skill 效果（AC-8）。
- 回退路径需要一个没有连接器权限的 Claude Team 成员账号；没有这样的账号时，C1 验收记为限制项。
- Epic 整体验收按 proposal 的 AC-1 到 AC-10，在 C3 归档后进行。

### 远期（不在本 Epic）

- 服务端无头渲染，让场景工具不依赖打开的页面。
- 按工具或按画布的细粒度授权、只读连接。
- 上架官方目录：Claude 连接器目录、ChatGPT 应用目录、OpenAI 插件目录。
- 国内宿主（OpenClaw、Hermes、CodeBuddy 等）；Codex 桌面 app、IDE 扩展的正式验收。
- Claude 桌面扩展（`.mcpb`）本地代理：在连接器权限之外再提供一条回退路径，C1 实测后视需要再立项。
- 随插件公开的工作流 Skill，编排外部助手的本机能力（research L6）。
- 外部助手调用 CRM 业务数据。
