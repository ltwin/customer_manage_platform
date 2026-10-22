# 活原型结论（D13）

2026-10-22，`docs/prototypes/creative-mcp/live/` 的假数据服务接进本机宿主。证据是 `live/logs/live-2026-10-22.jsonl`（不进 git）和 owner 在 Claude 桌面端的截图。owner 当天判断原型体验已够用，C1 直接开工，其余项在 C1 用真实服务复测。

## 实际跑到的宿主

| 宿主 | 客户端自报 | 声明卡片扩展 | 结果 |
|---|---|---|---|
| Claude 桌面端（Code 模式） | `claude-code` 2.1.304 | 是（`io.modelcontextprotocol/ui`，`text/html;profile=mcp-app`） | 走完生成剧本两遍 |
| Claude Code（VS Code 扩展里的会话） | `claude-code` 2.1.308 | 否 | 只建立了连接，没有调用工具 |
| 参考宿主 `refhost`（ext-apps 官方 AppBridge） | `refhost` | 是 | 两种模式都正常 |
| Codex CLI、ChatGPT | — | — | 没有测到 |

Claude 桌面端的 Code 模式复用 Claude Code 的 MCP 配置，走 HTTP，客户端名与终端里的 Claude Code 相同，只是多声明了卡片扩展。

## 对未决事项的结论

- **U7 `instructions`**：Claude 桌面端读到了。探针要求先以 `probe:"instructions-v1"` 调 `get_capabilities`，两次会话都照做。Codex、ChatGPT 待 C1 复测。
- **U7 超时**：`wait_tasks` 单次等 20 秒（假数据）没有问题。45 秒上限在 C1 复测。
- **U9 卡片与终端**：没有声明扩展的 Claude Code 不会收到卡片字段，这一点由服务端保证。图片与 `structuredContent` 同时出现时的问题没有复现条件，C1 先按保守做法：带图片的结果不给 `structuredContent`（见 C1 design）。
- **卡片里调用工具**：Claude 桌面端在 `ui/initialize` 的结果里没有声明 `hostCapabilities.serverTools`，卡片发起的 `tools/call` 一次都没有到服务端。卡片必须在宿主不转发时降级：不显示「生成」按钮，改为提示在对话里回复；不自己轮询（原型 c162575e 已实现）。这是 C2b 的输入。
- **U11 卡片里的图片**：Claude 桌面端拦下了卡片发往 `http://127.0.0.1:8774` 的全部请求（图片和诊断信标都没到），尽管资源的 `_meta.ui.csp.resourceDomains` 声明了这个地址。参考宿主里 `data:` 图片和公网 https 图片都正常。生产用对象存储的 https 签名地址，需要在 C2b 用真实 https 地址复测；本地开发的卡片图片改用 `data:`。
- **卡片高度**：`documentElement.scrollHeight` 不会小于 iframe 当前高度，卡片变矮时要量 `body` 的高度。
- **重复的完成卡**：卡片自己等到结果、模型又调用 `wait_tasks` 时，对话里会出现两张完成卡。C2b 设计时处理（服务端记住已在卡片里展示的任务，后一张只写一行）。

## 对 C1 的直接影响

1. `instructions` 可以承载使用规则，`get_capabilities` 仍按 design 返回 `hints` 作为兜底。
2. 本机三个宿主里，Claude 桌面端和 Claude Code 共用同一份本机配置，署名都是「Claude Code」。
3. 卡片相关的结论都留给 C2b，C1 不下发任何卡片字段。
