# 外部助手 · 活原型

第二层原型（D13）：一个只有假数据的 MCP 服务，接进本机的 Claude Code、Codex CLI、Claude 桌面端，看卡片和结果文字在真实宿主里的样子。ChatGPT 和 claude.ai 网页需要公网隧道，这次不接（owner 2026-10-22）。

工具、结果文字和卡片全部复用静态原型（`../shared/results.mjs`、`../widgets/`）。服务不鉴权、不连数据库，是一次性代码，不进生产。

## 启动

```sh
cd docs/prototypes/creative-mcp/live
npm install
npm start          # http://127.0.0.1:8774/mcp，给 Claude Code 和 Codex CLI
```

Claude 桌面端会自己用 `--stdio` 启动一个进程，并在 8775 端口提供页面。

两个辅助地址：
- `node selftest.mjs`：自检。分别用「声明卡片扩展」和「不声明」两种客户端跑一遍。
- `http://127.0.0.1:8774/reset`：清空任务和「页面已打开」状态。换宿主之前访问一次（Claude 桌面端用 8775）。

## 接入

| 宿主 | 怎么加 | 怎么删 |
|---|---|---|
| Claude Code | `claude mcp add --transport http creative-proto http://127.0.0.1:8774/mcp`，进 Claude Code 后用 `/mcp` 确认已连接 | `claude mcp remove creative-proto` |
| Codex CLI | `codex mcp add creative-proto --url http://127.0.0.1:8774/mcp`（手动配置，用默认的 60 秒超时） | `codex mcp remove creative-proto` |
| Claude 桌面端 | 设置 → Developer → Edit Config，在 `mcpServers` 里加下面这段，然后重启 Claude 桌面端 | 删掉这段，重启 |

```json
"creative-proto": {
  "command": "/Users/samson/.nvm/versions/node/v24.12.0/bin/node",
  "args": ["/Users/samson/workspace/my_project/customer_manage_platform/docs/prototypes/creative-mcp/live/server.mjs", "--stdio"]
}
```

## 剧本

每个宿主开一个新对话，依次说：

1. 「看看我最近的画布。把《九月，向海而行》里镜头 02 改得更有黄昏感，先预检，再生成两张。」
2. 确认生成：
   - Claude 桌面端：在预检卡里点「生成 · 40 点」。
   - 终端宿主：回复「可以，生成吧」。
3. 等结果，约 25 秒。
   - Claude 桌面端再试「放大查看」和「存入资产库」。
   - 然后问：「你看到结果了吗？哪张更像黄昏？」
4. 「按镜头 02 搭个 3D 场景，回看一下主机位。」
   - 点卡片里的「打开创意空间」，或终端里的链接，打开画布页。
   - 终端宿主再回复「开了」。
5. 「把三个镜头导出到画布。」
6. 只在终端宿主里做：
   - 「调用 probe_timeout，等 70 秒。」
   - Claude Code 再试 150 秒。

## 要看什么

我会读 `logs/` 里的日志，以下几项不用你记：
- 宿主声明了什么能力，有没有读界面资源；
- `instructions` 有没有交给模型（探针）；
- 卡片拿到的主题和尺寸、图片能不能加载、上下文更新是否成功、链接能不能打开。

需要你用眼睛看，并截图存到 `live/shots/`：

| 宿主 | 要看的 |
|---|---|
| Claude 桌面端 | 首次显示卡片的许可提示；卡片的大小和明暗；点「生成」后卡片的变化；全屏查看；第 3 步外部助手知不知道你在卡片里做了什么 |
| Claude Code | 工具结果显示几行、Markdown 链接怎么显示、图片显不显示；第 3 步外部助手能不能描述两张图（U9）；超时探针的表现 |
| Codex CLI | 同 Claude Code；另看图片是否显示在终端里、许可提示长什么样 |

U9 的对照实验：Claude Code 走完一遍后，用 `NO_STRUCTURED=1 npm start` 重启服务，再问一次第 3 步，看外部助手能不能描述图片。

## 文件

| 文件 | 内容 |
|---|---|
| `server.mjs` | 服务：MCP（HTTP 或 stdio）、卡片资源、页面与图片、链接落地、日志 |
| `selftest.mjs` | 自检客户端 |
| `refhost/` | 用 ext-apps 官方 AppBridge 加载卡片的参考宿主，验证卡片和标准实现兼容。`node refhost/build.mjs` 构建后打开 <http://127.0.0.1:8774/p/creative-mcp/live/refhost/index.html> |
| `logs/`、`shots/` | 日志和截图，不进 git |
