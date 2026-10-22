# 调研：宿主里的交互面与原型做法（2026-10-22）

> 状态：调研结论，供 owner 取舍。采纳后以 D10 起记入 proposal 决策记录，并修订 Epic。

## 结论先行

1. 图形界面宿主的内嵌 UI 已有统一标准 **MCP Apps**（扩展标识 `io.modelcontextprotocol/ui`）。Claude（网页、桌面端、手机端）和 ChatGPT 都按它渲染，OpenAI 原来的 `openai/outputTemplate` 只是兼容别名。我们写一套卡片，两家共用。
2. 卡片只是增强。所有宿主（包括图形界面宿主）都靠工具结果里的 `content` 文字工作；Codex CLI 和 Claude Code 只看得到这段文字。所以设计顺序是：先把文字和链接做完整，再给少数「看了才知道好不好」的结果加卡片。
3. 文字宿主里，最终给创作者看的话是外部助手写的，我们控制不了。我们能设计的是四样东西：工具结果文字、服务端 `instructions` 里的汇报规则、工具的 `title` 和 annotations，以及链接打开后的网页落地状态。所以「文字 + 链接」的体验，一半在网页端。
4. 原型分两层：静态 HTML 原型看界面和流程（沿用 `docs/prototypes/` 的做法）；再起一个假数据的 MCP 服务，接到四个真实宿主里看实际渲染。宿主行为（U7）只有在真宿主里才能确认。

## 一、四个宿主能显示什么

| | 内嵌卡片（MCP Apps） | 工具结果里的图片 | 链接 | 宿主自带的确认 | 我们能设计的 |
|---|---|---|---|---|---|
| Claude 桌面端 / 网页 | 支持：行内卡片、行内轮播、全屏。首次显示要创作者点「允许」或「始终允许」 | 给模型看 | 卡片内用 `ui/open-link`；正文链接由模型写 | 工具调用许可 | 卡片、结果文字、`instructions`、工具 title |
| ChatGPT（Developer Mode） | 支持：行内卡片、行内轮播、全屏、画中画 | 给模型看 | 同上 | 按 `readOnlyHint` / `destructiveHint` 决定是否确认 | 同上，外加 annotations |
| Codex CLI | 不渲染网页卡片。TUI 能显示工具结果里第一张可解码的图片（终端支持时） | 显示 + 给模型看 | 模型输出的 Markdown 链接，终端可点击 | 审批策略 + annotations | 结果文字、`instructions`、插件界面字段（`displayName`、`defaultPrompt` 等） |
| Claude Code | 按文字处理。「CLI 渲染 MCP Apps」的需求已被标为 not planned，但另有「按连接器关闭卡片渲染」的需求在讨论，需实测 | 有「图片和 `structuredContent` 同时出现时 `content` 被丢弃」的问题报告，需实测 | 同上 | 工具许可提示 | 结果文字、`instructions`、工具 title |

几条数字约束：
- Claude Code：工具输出超过 10,000 token 会警告，默认上限 25,000 token；调用超过 2 分钟会转为后台任务。
- Codex 插件已经在 `.mcp.json` 里调大超时（见 D9）。

## 二、MCP Apps 要点

- **组成**：一个 `ui://` 资源（`mimeType: text/html;profile=mcp-app`，单个 HTML，脚本和样式内联），加上工具定义里的 `_meta.ui.resourceUri`。宿主可以在调用前预取和缓存。资源 URI 就是缓存键，不兼容的改动要换新 URI。
- **通信**：卡片在沙箱 iframe 里，经 postMessage 走 JSON-RPC。
  - 宿主 → 卡片：`ui/notifications/tool-input`、`tool-input-partial`、`tool-result`、`tool-cancelled`、`host-context-changed`、`ui/resource-teardown`。
  - 卡片 → 宿主：`tools/call`（调本服务的工具）、`ui/message`（以创作者身份发一句话）、`ui/update-model-context`（下一轮给模型的补充上下文）、`ui/open-link`、`ui/request-display-mode`、`ui/notifications/size-changed`。
- **工具可见性**：`_meta.ui.visibility` 取 `["model"]`、`["app"]` 或两者。`["app"]` 的工具只给卡片调，模型的工具列表里看不到。卡片轮询任务、点「存入资产库」都走这条路。
- **宿主上下文**：`ui/initialize` 返回主题（light/dark）、标准 CSS 变量（颜色、字体、圆角、阴影）、`displayMode`、`containerDimensions`、`safeAreaInsets`、`platform`、`locale`。卡片必须给每个变量写默认值，因为宿主可能不传。
- **安全**：
  - 外部来源默认全部禁止，要在 `_meta.ui.csp` 里逐项声明：`connectDomains`、`resourceDomains`、`frameDomains`、`baseUriDomains`。
  - Claude 目前限制 `frameDomains`，所以**不能把创意空间整页 iframe 进卡片**。
  - Claude 要求 `_meta.ui.domain` = 服务地址 SHA-256 的前 32 位十六进制 + `.claudemcpcontent.com`。
- **模型看到什么**：
  - 规范写的是：`content` 给模型和文字宿主；`structuredContent` 给卡片，不进模型上下文；`_meta` 不给模型。
  - OpenAI 文档却说 ChatGPT 的模型能看到 `structuredContent`。
  - 结论：`content` 必须自给自足；`structuredContent` 保持精简、不放敏感信息；签名图片地址这类只给卡片用的数据放 `_meta`。
- **降级**：不支持扩展的宿主按普通工具处理。服务端应检查客户端是否声明了 `io.modelcontextprotocol/ui`，据此决定是否返回卡片相关字段。这也可以绕开 Claude Code 丢 `content` 的问题。
- **版本**：规范是 `2026-01-26` 稳定版加 draft。SDK `@modelcontextprotocol/ext-apps` 2.0.3（2026-09-25）改了依赖，线协议不变。我们服务端用 Go，卡片前端可以不依赖 SDK，直接实现 postMessage 协议，或者只在卡片里用 SDK 的 `App` 类。

## 三、两家设计规范的硬约束（取交集）

| 方面 | 约束 |
|---|---|
| 何时用卡片 | 只在「看见比读文字更好」时用。数据类工具不挂卡片，让模型能连续调用；只给负责展示结果的工具挂 |
| 行内卡片 | 最多 2 个动作，放在底部；最多 4–5 个数据点；高度随内容，**不能嵌套滚动**；不要标签页、下钻、面包屑；不要菜单和弹出层（会被容器裁掉），用可见的分段按钮、开关 |
| 行内轮播 | 3–8 项；每项一张图、一个标题、最多 3 行信息、最多 1 个 CTA；尺寸一致 |
| 全屏 | 宿主的输入框始终在上面，要给它留位置；不用浮动面板，用可折叠侧栏或分页；卡片自己提供「全屏」按钮 |
| 画中画 | 只有 ChatGPT 支持，用于持续进行的活动，不放静态内容。本 Epic 不用 |
| 颜色 | 背景、文字、边框、图标用宿主变量；品牌色只用于强调和徽标；不用渐变和花纹；明暗两套都要测 |
| 字体 | 继承宿主字体（Claude 运行时提供 Anthropic Sans，ChatGPT 用系统字体），不加载自定义字体；只用标题、正文、说明三档字号，常规和强调两档字重 |
| Logo | 不在卡片里放 logo，宿主会在卡片前加应用名和图标 |
| 交互分界 | 直接操作（选择、确认已准备好的动作、展开）留在卡片里；需要语言理解的（自由输入、要求修改、追问）交给宿主的输入框 |
| 加载 | 用骨架屏，不用转圈 |
| 移动端 | 最窄 320px；点按区域至少 44pt；遵守 `safeAreaInsets` |

这些约束和创意空间的视觉方向不冲突：照片仍是唯一的高饱和内容，界面退到宿主的中性色里。但卡片**不使用** v6 的近黑主题，要跟随宿主的明暗。

## 四、对本 Epic 的设计判断

### 原则

- **P1 文字为本**：每个工具的 `content` 都能单独成立。第一行是结论，接着是关键字段，然后是 Markdown 链接，最后是下一步提示（例如「任务未完成，用 `wait_tasks` 继续等待」）。卡片只是同一结果的另一种呈现。
- **P2 少挂卡片**：只给结果是图像、需要创作者判断的工具挂卡片。读写画布、资产库查找、Skill 一律不挂，否则外部助手连续操作时，对话里会堆满卡片。
- **P3 卡片不碰网页端接口**：卡片里的动作都经宿主 `tools/call` 走 MCP 令牌，不直连 `/api/v1`，也不需要 `connectDomains`。图片走 `resourceDomains`，放行对象存储或 CDN 域名。
- **P4 动作回报给模型**：创作者在卡片里点了「生成」「存入资产库」后，卡片用 `ui/update-model-context` 告诉外部助手，避免它不知情又重复提交（对应 LibTV 的「不重复提交」规则）。
- **P5 链接是主交接**：深度编辑永远回到创意空间页面（`canvas_url` 可聚焦节点）。卡片的全屏只做只读查看，不在宿主里复刻编辑器。

### 卡片清单（建议首批 3 个资源）

| 卡片 | 挂在哪些工具 | 形态 | 动作（≤2） | 所属子项 |
|---|---|---|---|---|
| U-1 画布卡 | `list_canvases`（轮播）；新增渲染工具 `show_canvas`（行内卡片，可全屏） | 轮播：封面（按节点版式画缩影，同 v6 首页）、标题、更新时间。行内：画布缩影，高亮本轮改动的节点；全屏：只读平移缩放 | 「在创意空间打开」；全屏 | C1 / C2 |
| U-2 生成与任务卡 | `generate_node`（含 `dry_run`）、`wait_tasks` | 一张卡三种状态：预检（模型、参考缩略图、张数、预计点数）→ 进行中（骨架屏，卡片经 `["app"]` 可见的工具轮询）→ 结果（1–4 张图）。场景任务没有页面认领时显示「等待页面」 | 预检时：「生成」「不生成」。结果：「在画布中查看」「存入资产库」。等待页面：「打开创意空间」 | C2 |
| U-3 镜头卡 | `render_view`、`export_shots` | 轮播：回看截图或分镜，标镜头号 | 「在画布中查看」 | C3 |

不做：资产库卡片（查找是数据工具，主要给模型挑参考用）、Skill 卡片、画中画、在卡片里编辑画布。

预检卡和 D9 一致：是否预检仍由外部助手决定。没有预检就直接是进行中状态；有预检时，卡片上的「生成」等于创作者亲手确认。

### 文字宿主要设计的东西

1. **结果文字模板**：每个工具一份，C1 先给读工具定格式，后续子项沿用。
2. **`instructions` 里的汇报规则**：
   - 给画布和结果附可点击链接，不贴 JSON 和内部 ID。
   - 生成后附缩略图链接。
   - 「等待页面」时明确告诉创作者打开哪个链接。
3. **工具 `title` 和 annotations**：宿主的工具行和许可提示会显示它们。读工具标 `readOnlyHint`；`trash_assets` 这类标 `destructiveHint`，ChatGPT 和 Codex 会据此弹确认。
4. **Codex 插件界面字段**：`displayName`、`shortDescription`、`longDescription`、`defaultPrompt`（3 条起步提示）、`logo`、`brandColor`，显示在 Codex 的插件列表里。
5. **链接落地状态**（网页端，见下节）。

不采用 elicitation（服务端发起的表单或链接弹窗）：
- 由服务端强制确认，和 D9「预检由外部助手决定」冲突。
- Codex 对第三方服务的 URL 模式还没做完。

## 五、网页端要原型的页面

| # | 页面 / 状态 | 要点 | 视觉 |
|---|---|---|---|
| W-1 | 外部助手接入页 | 四个宿主切换；每个宿主「复制发给助手」的文字和手动步骤；Team 回退（Claude Code 本机命令、「发给管理员」文字）；MCP 地址；已连接列表（宿主名、最近调用、断开）；案例演示位 | 创意空间 v6，进入 v6 首页侧栏 |
| W-2 | OAuth 授权确认页 | 显示谁在请求（客户端名、图标、回调域名）、授权给哪个账号（可切换）、能做什么（白话列出：读写画布、生成并扣点数、整理资产库、搭场景）；允许 / 拒绝；客户端无效、链接过期、已拒绝等错误状态 | 沿用登录页视觉，因为授权是登录的延续 |
| W-3 | 授权完成页 | 本机回调宿主（Codex、Claude Code）完成后浏览器停在这里：「已连接 Codex，可以回到终端」 | 同 W-2 |
| W-4 | 创意空间「外部助手已连接」 | 顶栏指示（宿主名、最近一次调用）；点开看最近操作，跳到接入页断开；外部助手正在改的节点用什么标记（复用「助手」琥珀描边，还是另做标签） | v6 |
| W-5 | 链接落地 | `canvas_url` 带聚焦节点时，定位并闪一下高亮；有场景任务等这个页面时，显示横幅「Codex 有 2 个任务等此页面执行」 | v6 |
| W-6 | 场景历史署名（C3） | 修订列表里显示宿主名，可预览、恢复 | v6 |

## 六、原型怎么做

### 第一层：静态 HTML 原型（主体）

放在 `docs/prototypes/creative-mcp/`，和 v6 一样用 `python3 -m http.server` 打开，不依赖前后端。

- `web/`：W-1 到 W-5，用 v6 的 `tokens.css`；W-2、W-3 用登录页的样式。
- `widgets/`：U-1 到 U-3，按 MCP Apps 协议写成真正的视图（原生 JS，只用宿主 CSS 变量加默认值）。将来可以直接作为 `ui://` 资源的起点。
- `hosts/`：四个模拟宿主外壳，同一个剧本并排对照。
  - 外壳：Claude（按官方颜色变量做明暗两套）、ChatGPT、Codex CLI 终端、Claude Code 终端。
  - 剧本：看最近画布 → 改提示词 → 预检 → 生成 → 等待 → 看结果 → 搭场景时没有打开的页面。
  - 图形界面外壳实现最小的 postMessage 桥（`ui/initialize`、推送 `tool-result`、模拟 `tools/call`），把 `widgets/` 装进 iframe。
  - 终端外壳只显示工具行、结果文字和外部助手的回复，并标出哪些是我们写的、哪些是模型写的。

### 第二层：活原型（验证宿主行为）

起一个假数据的 MCP 服务：Node 加 `@modelcontextprotocol/ext-apps` 2.x，不接真实账号，鉴权用最简的假 OAuth 或不鉴权。它提供同样的 3 个卡片资源和结果文字，接到四个真宿主里看：
- Claude 桌面端：本机配置或 `mcp-remote`。
- Claude Code：`claude mcp add --transport http`。
- Codex CLI：手动配置。
- ChatGPT Developer Mode 和 claude.ai 网页自定义连接器：需要公网 https，要开隧道把假服务临时暴露出去，开之前单独确认。

要回答的问题：
- 两家卡片的实际尺寸、明暗、首次许可提示。
- 卡片里 `tools/call` 和 `ui/update-model-context` 是否生效。
- Claude Code 和 Codex CLI 怎么显示结果文字和图片，Claude Code 丢 `content` 的问题是否还在。
- ChatGPT 的模型是否读到 `structuredContent`。
- 各宿主的默认超时。

这一层是一次性代码，答完 U7 就丢，不进生产。

## 七、owner 决定（2026-10-22）

- **D10**：采纳 MCP Apps 内嵌卡片，ChatGPT 与 Claude 共用一套；文字加链接仍是所有宿主的基础。
- **D11**：首批卡片只做 U-2 生成与任务卡和 U-3 镜头卡。U-1 画布卡与 `show_canvas` 不做，画布交接仍靠 `canvas_url` 链接。资产库卡片不做。
- **D12**：卡片单列子项 `mcp-host-ui`，排在 C2 之后：C1 → C2 → `mcp-host-ui`（卡片通信基础与 U-2）→ C3（场景工具连同 U-3 镜头卡）。
- **D13**：原型做两层：静态 HTML 原型，加假数据的活原型。活原型接 ChatGPT 和 claude.ai 网页时要开隧道，开之前单独确认。

据此，第五节的 W-1 到 W-6 不变；第六节 `widgets/` 只做 U-2、U-3。Epic 的 proposal、contracts、roadmap 修订放在原型走查之后一并提交 owner 批准。

## 八、新增待核实项

- **U9**：Claude Code 当前是否渲染 MCP Apps，以及图片与 `structuredContent` 同时出现时的行为。
- **U10**：ChatGPT 模型是否读取 `structuredContent`（与规范说法不一致）。
- **U11**：卡片里显示对象存储签名图片的有效期和 CSP 配置；Claude 的 `ui.domain` 在多环境下怎么算（生产一个地址，本地开发另算）。

## 来源

- MCP Apps 概览与客户端：<https://modelcontextprotocol.io/extensions/apps/overview>、<https://modelcontextprotocol.io/extensions/client-matrix>
- MCP Apps 规范：<https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx>（另读 `specification/draft/apps.mdx`）；ext-apps v2.0.0 发布说明
- Claude：<https://claude.com/docs/connectors/building/mcp-apps/design-guidelines>、<https://claude.com/docs/connectors/building/mcp-apps/getting-started>
- ChatGPT：<https://developers.openai.com/plugins/build/chatgpt-ui>、<https://developers.openai.com/plugins/concepts/ui-guidelines>、<https://developers.openai.com/plugins/build/mcp-server>
- Claude Code：<https://code.claude.com/docs/en/mcp>；issue 汇总 <https://claudeissues.com/issue/48132-feature-preview-tool-should-support-rendering-mcp-apps-ext-apps>、<https://claudeissues.com/issue/54737-bug-mcp-tool-results-image-content-blocks-dropped-when-structuredcontent-is-also>
- Codex：elicitation 支持 <https://codex.danielvaughan.com/2026/04/10/mcp-elicitation-codex-cli/>；URL 模式 issue openai/codex#29344；TUI 图片渲染 PR openai/codex#9815（二手资料，待活原型核实）
