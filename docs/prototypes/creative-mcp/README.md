# 外部助手 · 静态原型

Epic `creative-mcp-server` 的第一层原型（D13）。它覆盖两块：
- 网页端：接入、授权、交接。
- 四个宿主里的交互：ChatGPT 和 Claude 有内嵌卡片，Claude Code 和 Codex CLI 只有文字加链接。

全部是假数据，不连前后端。依据和决定见 `.lti/features/2026-10-21-creative-mcp-server/discovery/research/host-ui-surfaces.md`（D10–D13）。

## 打开

在仓库根目录运行 `python3 -m http.server 8773 --bind 127.0.0.1`，打开 <http://127.0.0.1:8773/docs/prototypes/creative-mcp/>。

不能直接双击 HTML 打开：页面用 ES 模块，卡片在 iframe 里，都需要 http 服务。

## 内容

| 页面 | 说明 | 状态切换 |
|---|---|---|
| `hosts/index.html` 宿主对照 | 同一个 8 步剧本，在四个宿主里同步推进 | 宿主标签、浅色/深色、「标注内容来源」，← → 翻步 |
| `widgets/gallery.html` 卡片状态总览 | U-2 生成与任务卡、U-3 镜头卡的 8 种状态，分别在两家宿主的浅色和深色下 | 无 |
| `instructions.md` | 服务端 `instructions` 草稿 | — |
| `web/connect.html` W-1 接入页 | 选宿主、复制发给助手、Team 回退、已连接与断开 | 右下角：有连接 / 还没有连接 |
| `web/authorize.html` W-2/W-3 授权页 | 授权确认、完成、拒绝、出错 | 右下角：7 种 |
| `web/canvas.html` W-4/W-5/W-6 画布交接 | 「外部助手已连接」指示、链接聚焦、等页面执行的任务、场景历史署名 | 左下角：4 种；也认链接参数 `focus`、`tasks`、`host` |

右下（画布页在左下）紫色虚线框是原型状态面板，不属于产品界面。

## 文件

| 文件 | 内容 |
|---|---|
| `shared/data.mjs` | 示例账号、宿主、连接、画布 |
| `shared/results.mjs` | 工具结果文字模板、工具显示名，是文字宿主体验的主体，走查时重点看 |
| `widgets/bridge.js` | 卡片一侧的 MCP Apps 通信 |
| `widgets/cards.js` | 两张卡片的渲染和动作 |
| `widgets/widget.css` | 只用 MCP Apps 标准 CSS 变量，带默认值 |
| `hosts/host-bridge.mjs` | 宿主一侧的通信：沙箱 iframe、ui/initialize、转发 tools/call |
| `hosts/mock-server.mjs` | 内存里推进任务的假 MCP 服务 |
| `hosts/scenario.mjs` | 剧本 |
| `hosts/hosts.mjs` | 四个宿主外壳 |

## 原型里定下的设计

**文字（所有宿主）**
- 每个工具结果按「结论 → 字段 → 链接 → 下一步」写。终端只露前 3–4 行，第一行必须能单独看懂。
- 链接一律指向生产地址：`canvas_url?focus=节点` 聚焦节点，`&tasks=1` 表示有任务等这个页面。原型里点链接会打开本地页面。
- `wait_tasks` 没完成时，结果里写明「再调用一次」；回看排队时说清楚为什么等、要打开哪个链接。
- 工具显示名用中文动作：检查连接、列出画布、写提示词、生成、回看、导出分镜。宿主的许可提示里会出现这些名字。

**卡片（图形界面宿主）**
- 只给 `generate_node`、`wait_tasks`（`ui://creative/tasks`）和 `render_view`、`export_shots`（`ui://creative/shots`）挂卡片，读写画布等数据工具不挂。
- 预检状态有 4 个数据点、2 个动作（「生成 · N 点」「不生成」）。点「生成」就算创作者亲手确认。
- 卡片的动作经宿主 `tools/call` 走 MCP。对卡片开放的只有 `generate_node`、`wait_tasks`、`save_to_library`。
- 卡片每 1.5 秒轮询一次 `wait_tasks`。完成后用 `ui/update-model-context` 告诉外部助手「已完成，不必再等」；确认生成后同样告诉它「已提交，不要重复提交」。
- 这些上下文要等创作者下一句话，外部助手才读得到。所以点了卡片按钮后，外部助手这一轮不会说话。
- 状态标签放在标题下方，琥珀色只表示进行中或等待，和创意空间的语义一致。「等待页面」不放骨架屏，免得像在跑。
- 行内结果图有最大宽度：竖图两列最宽 420px，单张横图最宽 560px。完成后可以全屏查看，宿主的输入框压在底部，内容要避开安全区。
- 颜色、字体跟随宿主（Claude 的官方变量值，ChatGPT 的是近似值）。卡片不用 v6 的近黑主题。

**网页端**
- 接入页挂在创意空间侧栏的「外部助手」下面。
  - Codex 给一段「复制发给 Codex」的文字，由它自己安装插件。
  - Claude、ChatGPT 只能手动添加，配步骤说明。
  - 回退（D8）：Claude Code 用本机配置；Claude 和 ChatGPT 给一段「发给管理员」的文字。
- 授权页沿用登录页视觉。
  - 页面写明能做和不能做什么，生成要扣点也写在上面。
  - 自行注册（DCR）的客户端名称无法验证，会加警示。
  - 本机回调的宿主（Codex、Claude Code）授权后停在完成页；网页宿主直接跳回自己的页面。
- 画布页：
  - 顶栏「{宿主} 已连接」，正在操作时变琥珀，点开看最近的操作，并能跳到接入页管理连接。
  - 外部助手改过的节点用琥珀描边，标签是宿主名。
  - 有任务等这个页面时，顶部横幅显示进度和「完成前请保持页面打开」。
  - 场景历史里，外部助手的修订署名为宿主名，可以预览和恢复。⌘Z 只撤自己的改动。

## 近似和待核实（交给活原型）

- 宿主自带的部分都是近似的：许可提示的样子和时机、ChatGPT 的颜色变量、各宿主的菜单名称（U1、U7）。
- Claude Code 终端里图片和链接怎么显示，以及结果里同时有图片和 `structuredContent` 时的问题（U9）。
- ChatGPT 的模型会不会读 `structuredContent`（U10）。原型把签名图片地址放在 `_meta`，`structuredContent` 只放精简数据。
- 对象存储图片在卡片里的 CSP 与签名有效期，以及 Claude 的 `ui.domain`（U11）。
- 插件仓库地址待定（U3），接入页里用占位。

## 活原型

第二层活原型（D13）在 `live/`，见 [live/README.md](live/README.md)：一个假数据 MCP 服务，接进本机的 Claude Code、Codex CLI、Claude 桌面端。它复用这里的 `widgets/` 和 `shared/results.mjs`。ChatGPT 和 claude.ai 网页需要公网隧道，这次不接。
