# Tasks

按「存储 → 授权服务器 → 读工具门面 → MCP 端点 → 网页接口 → 前端 → 接入物料」推进。每个任务结束时，受影响包的 `make check-go PKG=...` 通过；前端任务结束时 `make check-frontend` 通过。e2e 只跑新增用例，换端口避开 owner 的 Vite。

## 1. 存储

- [x] 1.1 迁移 `0080_external_agent_oauth`：四张表、CHECK、每连接一行 `current` 的部分唯一索引、连接列表索引、通知触发器（新建、`revoked_at` 变化、由不活动变为活动）；down 在有连接行时拒绝。先写迁移测试：约束逐条（`host_kind`、`revoked_reason` 必填条件、`registration` 与认证方式、两行 `current` 被拒）和触发器的通知条件。完成判据：测试先红后绿，`check-go PKG=./internal/platform/store/...` 通过
- [x] 1.2 `store/oauth.go` 实现 `oauth.Store`（客户端、授权码、refresh、连接的建、查、断开、节流写 `last_used_at`），`Store.RealtimeWatching`。集成测试覆盖锁与一次性语义（并发换同一授权码只有一个成功）、断开作废全链、按账号列出。完成判据：测试绿

## 2. 授权服务器

- [x] 2.1 `platform/oauth`：回调地址规则、DCR 注册（公共与机密客户端、限流）、CIMD 拉取（SSRF 防护、缓存）、宿主识别、元数据。单测覆盖 design「验证与交付」授权服务器一节的注册与 CIMD 部分。完成判据：测试先红后绿
- [x] 2.2 授权请求校验与请求包、确认（建连接与授权码）、换令牌、refresh 轮换与重放、访问令牌签发与校验、断开。单测覆盖 PKCE、过期、重放、`resource`、`client_id`、密钥、篡改与过期的请求包；`auth.DerivePurposeKey`。完成判据：测试先红后绿，`check-go PKG=./internal/platform/oauth/...` 通过

## 3. 读工具门面

- [x] 3.1 `readAccess` 接口与运行实现，`read_canvas`、`read_asset`、`search_assets`、`read_library` 改为共用核心，`toolFailure` 从运行路径抽出。运行路径现有测试不改、全部通过。完成判据：`check-go PKG=./internal/creativeagent/...` 通过
- [x] 3.2 `ExternalTools`：工具目录（`external` 标记、`title`、说明、JSON Schema）、6 个只读工具的外部实现与摘要、错误映射、`instructions/external.md`；`llmgateway.MonthBudget`、`creativemedia.AgentImageFit`。测试：两账号越权得 `not_found`；内置与外部的结构化结果对照；摘要第一行；`get_capabilities` 各字段（没有模型、没有网关时）；`read_asset` 的图片长边 ≤ 1024。完成判据：测试先红后绿

## 4. MCP 端点

- [x] 4.1 `creativemcp`：SDK server、工具注册、结果转换（有图片时不给 `structuredContent`）、限流、关闭本机 Host 保护。`httpapi/mcp.go` 挂载与主体注入，`httpapi/oauth.go` 的协议端点，`router.go` 按 `MCP_ENABLED` 挂载，`cmd/server` 装配。集成测试用 SDK 客户端走完整授权（DCR → authorize → decision → token → initialize → tools/list → tools/call → refresh），以及 401、令牌分域、断开后 401、开关关闭 404。完成判据：测试绿，`check-go PKG=./internal/platform/httpapi/...` 通过

## 5. 网页接口

- [x] 5.1 `api/openapi.yaml` 加连接列表、断开、确认授权，`make generate`；`httpapi/agent_connections.go`。测试：列表的 `active` 与 `include_revoked`、断开幂等、他人连接 404、query 非布尔 400。完成判据：测试先红后绿，生成物已提交

## 6. 前端

- [x] 6.1 确认页 `/oauth/consent`：未登录时带原查询串去登录再回来；允许、拒绝、出错、已授权（回环回调时显示「可以回到 <宿主>」）；DCR 客户端名称警示。Vite 开发代理。完成判据：`make check-frontend` 通过，本机能走完 Claude Code 的授权
- [x] 6.2 接入页 `/creative/agents` 与侧栏入口：四个宿主的接入文字与回退文字、复制、连接列表与断开、暂未开放；画布页顶栏指示（60 秒刷新、实时失效时重取、本地到期隐藏）。完成判据：`make check-frontend` 通过，截图与原型对照

## 7. 接入物料与文档

- [x] 7.1 `integrations/codex-plugin/`（manifest、`.mcp.json` 模板带 `tool_timeout_sec: 300`、marketplace、`build.sh`、README）；`docs/dev/creative-mcp.md`（开关、本机三个宿主的接入命令、排错、日志字段）；CLAUDE.md 文档索引加一行。完成判据：`build.sh https://example.invalid out/` 生成的目录结构与 LibTV 插件一致

## 8. 验收准备

- [x] 8.1 本机起服务（`MCP_ENABLED=true`），用 SDK 客户端充当宿主、Playwright 驱动真实浏览器完成登录与确认，走一遍 AC-C1-1、AC-C1-3、AC-C1-5；把 Claude Code、Claude 桌面端、Codex CLI 的实测步骤写进 `discovery/research/host-runs.md` 交给 owner，留出记录 `host_kind`、注册方式、宿主版本的位置。完成判据：模拟宿主的一遍走通，实测步骤已写
