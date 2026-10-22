# 创意空间 · Codex 插件

外部助手（epic `creative-mcp-server`）的 Codex 接入物料。插件只做一件事：把 Codex 连到创意空间的远程 MCP 端点 `<PUBLIC_BASE_URL>/mcp`，并把工具超时调到 300 秒。使用说明由服务端 `instructions` 下发，插件里没有平台 Skill 正文（D3）。

## 结构

| 文件 | 内容 |
|---|---|
| `template/.agents/plugins/marketplace.json` | marketplace：一个插件 `creative`，安装时就授权（`ON_INSTALL`） |
| `template/plugins/creative/.codex-plugin/plugin.json` | 插件清单：显示名、简介、起步提示，`mcpServers` 指向 `.mcp.json` |
| `template/plugins/creative/.mcp.json.tmpl` | MCP 配置模板，`{{MCP_URL}}` 由 `build.sh` 填入 |
| `build.sh` | 生成发布目录 |

格式依据 LibTV 公开插件仓库（2026-10-22），并用 codex-cli 0.161.0 的本地 marketplace 实测：`.mcp.json` 必须写成 `{"mcpServers": {"<名字>": {...}}}`，扁平的单个服务对象不会被加载；`codex mcp get creative` 能读到 `tool_timeout_sec: 300`。

## 发布

只发一个指向生产地址的包，不按环境拆包（LibTV 的教训：残留的同名配置会冲突）。

```sh
integrations/codex-plugin/build.sh https://<生产域名> <公开仓库的检出目录>
```

公开仓库的位置和同步方式待 owner 决定（Epic 未决事项 U3）。在那之前，创意空间接入页的 Codex 文字用手动配置：

```sh
codex mcp add creative --url <PUBLIC_BASE_URL>/mcp
codex mcp login creative
```

插件发布后，把接入页 `frontend/src/creative-canvas/agents/hostGuides.ts` 的 Codex 文字换成：

```sh
codex plugin marketplace add <公开仓库地址> --ref main
codex plugin add creative@creative-plugins
```

## 本地试装

用隔离的 `CODEX_HOME`，不改本机的 Codex 配置：

```sh
out=$(mktemp -d); home=$(mktemp -d)
integrations/codex-plugin/build.sh https://example.invalid "$out"
CODEX_HOME=$home codex plugin marketplace add "$out"
CODEX_HOME=$home codex plugin add creative@creative-plugins
CODEX_HOME=$home codex mcp get creative
```
