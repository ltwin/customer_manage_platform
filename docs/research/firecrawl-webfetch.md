# Firecrawl 作为 AI web_fetch 工具的接入调研

调研日期：2026-10-14。用户原文为「filecrawl」，本次暂按 **Firecrawl** 理解。本文是研究结论与待设计事项，不代表已批准实现。

资料范围：Firecrawl 官方文档、官方价格页、官方公开源码与官方安全公告；先用 Context7 定位，再联网读取当前来源。本次未使用抓取凭证、未调用生产或收费 API，未实测质量、延迟、费用或自托管部署。`docs/research/` 原先不存在，本次作为新的研究目录创建。

## 1. 建议结论

首期采用 **Go 服务端 REST 小适配器 → Firecrawl `/v2/scrape` → 本项目 AI 工具**，产品工具契约建议为 `web_fetch@1`，只接收一个已知公共网页 URL，返回正文 Markdown 与可引用元数据。先关闭 crawl、search、extract、browser、actions、登录态和文件解析等扩展入口。当前官方 Go 快速入门也直接调用 REST。[Go Quickstart](https://docs.firecrawl.dev/quickstarts/go)

优先验证 Firecrawl Cloud。若项目要求内容不能流向托管服务，再单独设计自托管；自托管是多服务运维方案，并不能直接视为云服务的完整替代。部署差异见第 6 节。[Open source or Cloud](https://docs.firecrawl.dev/contributing/open-source-or-cloud)

落地前的关键决策是**收费准入、公共 URL 边界、超时后的不确定状态和引用输出**。网页内容读取没有业务写入，并不等于调用不会产生费用。以下“建议”均是本项目设计方向，与 Firecrawl 官方事实分开。

## 2. 单 URL 抓取契约：已核实事实

### 请求

API 地址为 `POST https://api.firecrawl.dev/v2/scrape`，云端认证可用 `Authorization: Bearer <API_KEY>`，JSON 请求体含 `url` 与抓取选项。当前也有按 IP 限额的无 key 层；正式产品建议使用受控服务端凭证。[Scrape API](https://docs.firecrawl.dev/api-reference/endpoint/scrape)、[Rate Limits](https://docs.firecrawl.dev/rate-limits)

| 选项 | 当前官方语义 | 首期建议 |
| --- | --- | --- |
| `formats` | 可选 Markdown 等输出格式 | 固定 `["markdown"]` |
| `onlyMainContent` | 默认 `true`，过滤到主要内容 | 显式 `true` |
| `maxAge` | 毫秒；默认 `172800000`（2 天）；`0` 强制新抓 | 服务配置决定，可先用 10 分钟；不交给模型自由设置 |
| `waitFor` | 默认 `0`；在智能等待之外增加等待 | 默认 `0`，针对实测站点调整 |
| `timeout` | 毫秒；默认 `60000`，范围 `1000..300000` | 配合 run 剩余时间设置供应商超时 |
| `skipTlsVerification` | 默认 `true` | 必须显式 `false` |
| `parsers` | 默认 PDF 解析；`[]` 时 PDF 返回 base64 | 显式 `[]`，响应拒收非网页/二进制内容 |

上表 API 语义以当前官方契约为依据；右栏是设计建议。[Scrape API](https://docs.firecrawl.dev/api-reference/endpoint/scrape)

请求草案如下，仅展示参数，不是执行记录：

```json
{
  "url": "https://example.com/article",
  "formats": ["markdown"],
  "onlyMainContent": true,
  "maxAge": 600000,
  "timeout": 60000,
  "skipTlsVerification": false,
  "parsers": []
}
```

60 秒与 10 分钟是待试点验证的配置值。客户端网络期限需要覆盖该次供应商请求且受 run 剩余期限约束；不能直接复制文档默认值。

### 响应

REST 成功包裹是 `success: true` 加 `data`；正文在 `data.markdown`。API HTTP 状态、`success` 和目标网站的 `metadata.statusCode` 是不同层次。API 返回 200、`success:true` 时，目标页面仍可能是 403/404；应检查页面状态和 `metadata.error`。[Scrape Guide](https://docs.firecrawl.dev/features/scrape#response-metadata-and-status-codes)

当前 OpenAPI 中：`metadata.title` 可以是字符串或字符串数组；`sourceURL` 是原始请求 URL，`url` 是重定向后的最终 URL；还有 `statusCode`、`contentType` 等字段。不要把 `sourceURL` 解释为重定向后的地址。[v2 OpenAPI（固定源码快照）](https://github.com/firecrawl/firecrawl-docs/blob/9142bdf301b7662a5d491cea64a6c871fab34a07/api-reference/v2-openapi.json)

适配器应验证正文与元数据，再输出稳定的本项目结构。建议字段：

| 字段 | 项目语义 |
| --- | --- |
| `markdown` | 有长度上限的正文；按 Unicode 边界截断 |
| `title` | 可选，兼容供应商字符串/数组后归一化 |
| `requested_url / source_url` | 本平台发出的 URL / 供应商报告的原始来源 URL |
| `final_url` | 若供应商提供，记录最终落点；再次做 URL 规则验证 |
| `page_status_code` | 目标页面状态；与供应商 API 状态分开 |
| `content_type` | 用于拒收非首期支持的内容 |
| `retrieved_at` | 本项目收到该原始结果的时间；本地缓存命中保留这个时间，不能宣称是网页最后抓取/更新时刻 |
| `upstream_cache_state` | 供应商提供时记录命中状态，缺失保留未知 |
| `truncated` | 是否被项目输出上限截断 |

不要将整份供应商响应、base64、未清理的 HTML 或页面指令直接作为可信指令交给 Agent。抓取结果是外部资料，应附来源并标记为不可信内容；这是项目安全设计建议。

## 3. 缓存、费用与额度

### 缓存事实

Firecrawl 缓存命中仍按普通页面扣 **1 credit/页**，改善的是响应速度，不是费用；`maxAge:0` 重新抓取。`headers`、`actions`、`profile` 等请求会跳过缓存；`metadata.cacheState` 可报告命中状态。官方的“500% faster”是产品描述，本项目没有实测。[Faster Scraping](https://docs.firecrawl.dev/features/fast-scraping)

**建议**：若希望同一 run 重复读同一 URL 不再付费，应在项目侧按规范化 URL 与固定抓取配置复用已完成结果；如做跨 run 缓存，需定义账号隔离、TTL 与内容留存。供应商缓存不能代替本项目调用去重，缓存年龄也不能证明页面中某项业务状态当前仍有效。

### 当前云端价格快照

价格页注明 **2026-09-04 生效，USD**；以下是 2026-10-14 读取值，采购时须重新核实。[Pricing](https://www.firecrawl.dev/pricing)

| 套餐 | credit/月 | 当前价格 |
| --- | ---: | --- |
| Free | 1,000 | $0；无需信用卡 |
| Hobby | 5,000 | 月付 $19；年付折合 $16/月 |
| Standard | 100,000 | 页面当前年付视图为 $83/月 |
| Growth | 500,000 | 页面当前年付视图为 $333/月 |
| Scale | 1,000,000 | 页面当前年付视图为 $599/月 |

未据此推算未直接确认的高档月付价格，也没有启用套餐、充值或自动续费。

普通抓取产出文档时收 1 credit/页；目标 403/404 产出文档仍收费，无文档通常不收费，但特定选项存在例外。当前 enhanced proxy 与 auto 升级没有额外 credit，供应商内部升级重试属于同一请求。[Billing](https://docs.firecrawl.dev/billing)、[Enhanced Mode](https://docs.firecrawl.dev/features/enhanced-mode)

**不能把所有公共 URL 都预算成 1 credit。** 当前 `x.com`、`twitter.com`、`mobile.twitter.com` 的相关个人页/帖子走 Grok 路径，典型请求 30 credits；LinkedIn profile/company URL 可由第三方 provider 返回记录，费用随 provider 配置改变；正文格式为 Markdown 不代表使用了普通网页引擎。[Billing：Provider-routed URLs 与 X billing](https://docs.firecrawl.dev/billing#provider-routed-urls)

PDF/文档也会被自动检测。当前 Billing 将 PDF 解析列为额外按页收费，而端点的 `parsers` 描述简写为按 PDF 页收费；因此本文不对默认 PDF 总费用写未经验证的公式。`parsers:[]` 可避免 PDF 自动多页解析，但仍可能返回收费的文件 base64，且 URL 扩展名无法可靠判断 MIME 或重定向落点。[Document Parsing](https://docs.firecrawl.dev/features/document-parsing)、[Scrape API](https://docs.firecrawl.dev/api-reference/endpoint/scrape)

### 成本控制建议

- 首期只允许已验证的公共网页范围，排除 X/Twitter、LinkedIn 及明显文档路径；成功响应再验证 MIME、`metadata.provider` 与最终 URL。这些域名限制是当前路由快照，不是永久的供应商计费承诺。
- 保持固定参数，不接受模型传入 JSON 提取、actions、profile、headers、`alexandria` 等选项；`domainTools:false` 只控制工具发现，不是“关闭 provider 路由”的开关。[Alexandria](https://docs.firecrawl.dev/features/alexandria)
- 当前公开 `/scrape` 请求 schema 没有单次 `maxCredits` 或统一的“仅标准引擎/禁止 provider routing”参数；此为对该版本 schema 的有界检查，不等同于供应商未来承诺。[v2 OpenAPI](https://github.com/firecrawl/firecrawl-docs/blob/9142bdf301b7662a5d491cea64a6c871fab34a07/api-reference/v2-openapi.json)
- 设置应用侧每 run 调用上限、账号额度与并发上限，并为项目专用 API key 配置日/周/月 credit 限额；监控供应商实际用量。官方 API-key 限额是期间额度，不能宣称是本项目单次请求的费用硬上限。[Billing：API key spend limits](https://docs.firecrawl.dev/billing#api-key-spend-limits)
- 试点先用受控账户额度，pay-as-you-go 保持关闭；进入采购再确定预算。云端已有免费/月付费用模型，不应通过工具标记将它伪装为无收费调用。

## 4. 超时、重试与恢复：不能误判幂等

普通 URL `/v2/scrape` 是同步入口。当前公开源码每次普通 URL 请求创建新 `uuidv7()`，路由没有为 `/scrape` 安装 crawl 使用的 `idempotencyMiddleware`；`External-Request-Id` 是费用归因标签，不是去重凭证。[scrapeController](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/controllers/v2/scrape.ts#L59)、[v2 routes](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/routes/v2.ts#L245)、[external-request-id](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/lib/external-request-id.ts#L11)

`x-request-id` 的结果 replay 条款适用于传 `alexandria` 的 provider-tool 请求；官方明确说明 provider 服务的 **URL scrape** 每次都会收费。不能把该 replay 条款推广到普通 URL 抓取。[Scrape Guide：Alexandria data tools](https://docs.firecrawl.dev/features/scrape#alexandria-data-tools)

当前 OpenAPI 确有 `GET /v2/scrape/{jobId}`。公开实现需已知 jobId、检查 team 与记录过期时间，再返回已存结果；强制 ZDR team 不支持该接口。[v2 OpenAPI](https://github.com/firecrawl/firecrawl-docs/blob/9142bdf301b7662a5d491cea64a6c871fab34a07/api-reference/v2-openapi.json)、[scrapeStatusController](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/controllers/v2/scrape-status.ts#L7)

**限制**：未找到普通 URL 请求能在响应之前取得稳定 jobId、按客户端 operation ID 查回结果或保证断连不计费的公开契约；服务端有请求关闭时的中止逻辑，但不能据此断言网络失联前一定尚未计费。客户端已经发出请求却没有收到结果时，应记录为“结果/费用未确认”，不要自动再次 POST。源码有内部按 worker job ID 去重的计费逻辑，它保护同一个服务端 job 的重跑，不能保证两个新的客户端 POST 不重复收费。[scrapeController](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/controllers/v2/scrape.ts#L344)、[worker billing](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/services/worker/scrape-worker.ts#L211)

**首期建议**：不自动重试已经发出的超时/断连请求；429 等可重试错误也要先确认没有文档并遵守预算/退避。失败区分 invalid_url、unsupported_content、page_error、provider_auth、quota_exceeded、rate_limited、timeout、provider_failure 与 outcome_unknown。API 200 仍要读 `success`：当前源码的 DNS 错误存在 HTTP 200、`success:false` 路径。[scrapeController error handling](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/controllers/v2/scrape.ts#L465)

## 5. REST、Go SDK 与 MCP 的方案比较

| 方案 | 官方证据与适用点 | 本项目判断 |
| --- | --- | --- |
| Go `net/http` 调 v2 REST | 官方 Go Quickstart 使用 `/v2/scrape` | **首期推荐**；适配器可控制取消、预算、错误、输出与凭证 |
| `firecrawl-go/v2` | 当前官方仓库 `ScrapeURL` 实际调用 `/v1/scrape`；参数仍有 `parsePDF/jsonOptions` | 不把 Go module `/v2` 误当 API v2；首期不采用 |
| 托管 MCP | `/v2/mcp` API-key bearer；`/v2/mcp-oauth` 账户授权 | 适合已有通用 MCP 接入层；首期会扩张工具面和传输依赖 |
| 本地 MCP | 默认 stdio；可 Streamable HTTP，API 可指向自托管 | 需要 Node 运行时与进程运维，首期 REST 无需这一层 |

来源：[Go Quickstart](https://docs.firecrawl.dev/quickstarts/go)、[Go SDK 固定源码](https://github.com/firecrawl/firecrawl-go/blob/101dd314faf08efa7ee004b02956ff9becfdde2d/firecrawl.go#L273)、[MCP Get Started](https://docs.firecrawl.dev/mcp-server)、[MCP Local](https://docs.firecrawl.dev/mcp-server/local)

Go SDK 当前源码的 `ScrapeParams` 有 `MaxAge/Timeout`，但没有当前 v2 的 `parsers`、`skipTlsVerification`，`Formats` 也只是字符串数组。其版本路径是 Go 模块主版本；不能借修改 `APIURL` 稳妥切到 API v2，因为方法追加了固定 `/v1/scrape`。[ScrapeParams 与请求路径](https://github.com/firecrawl/firecrawl-go/blob/101dd314faf08efa7ee004b02956ff9becfdde2d/firecrawl.go#L89)

当前 MCP 官方文档：已认证托管连接暴露完整工具集合，keyless 只暴露 `firecrawl_scrape/search/parse`；读取已知单页应选 `firecrawl_scrape`，整站用 crawl，搜索用 search。部分反馈工具可由环境变量关闭，但这不是任意工具 allowlist 的承诺。[MCP tools](https://docs.firecrawl.dev/mcp-server/tools)

本地指南固定 `firecrawl-mcp@3.23.7`，要求 Node.js 22+；Streamable HTTP 使用 `HTTP_STREAMABLE_SERVER=true`，本地地址为 `http://localhost:3000/mcp`。这些是该指南的验证版本与配置，不能将本地 `/mcp` 与托管 `/v2/mcp` 混用。[MCP Local](https://docs.firecrawl.dev/mcp-server/local)

若后续接 MCP，项目仍应自己只绑定批准的单页工具，收窄输入到 `url`；不要把完整上游 schema 暴露给 Agent。“MCP”是接入协议选择，不会消除上游计费、URL 安全或本地日志/审计要求。

## 6. 自托管：依赖、许可与 SSRF 边界

### 部署与功能

当前官方自托管指南固定 Firecrawl `v2.11.162`，用 Docker Compose 建立 API、Playwright 与支持服务；文档说明起步方案关闭 API 认证，没有持久化存储、TLS 或高可用，不应直接公开暴露。公开 Compose 当前含 Redis、RabbitMQ、PostgreSQL 队列与 API/浏览器服务。[Self-hosting](https://docs.firecrawl.dev/contributing/self-host)、[docker-compose.yaml 固定源码](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/docker-compose.yaml)

默认栈含核心 scrape/crawl/map/search 路由、Fetch 与 Playwright；LLM 格式需外接兼容 provider 或 Ollama；高级 Fire-engine 不包含，默认 Fetch/Playwright 不支持截图或页面 actions。Agent、Browser、Interact 等不在默认栈。认证、存储、监控、升级、备份恢复、并发容量和安全由项目运维承担；官方未发布该默认栈经验证的最小机器规格。[Self-hosting feature support](https://docs.firecrawl.dev/contributing/self-host#self-hosted-feature-support)、[Open source or Cloud](https://docs.firecrawl.dev/contributing/open-source-or-cloud)

### 许可事实

Firecrawl 主仓库根 `LICENSE` 是 AGPL v3；第 13 条涉及修改后通过网络与用户交互时提供相应源码的要求。该事实不能直接推导“调用 REST 的本项目必须整体开源”。若选择修改、分发或深度整合自托管引擎，应核实选中版本和各组件许可及应用边界。[LICENSE 固定源码](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/LICENSE#L540)

### SSRF 与重定向：公开源码有防护，仍需出站隔离

当前 `safeFetch.ts` 对 private IP literal、localhost 和重定向进行检查，直连时也检查 socket remoteAddress；源码明确指出使用 `PROXY_SERVER` 时，其他域名在 proxy 侧解析，必须由 proxy 过滤。`ALLOW_LOCAL_WEBHOOKS=true` 会绕过相关防护，不能在接收不可信 URL 的公共抓取器中启用。[safeFetch 固定源码](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/api/src/scraper/scrapeURL/engines/utils/safeFetch.ts#L15)

当前 Playwright 服务检查 HTTP(S) URL、DNS 地址范围，使用本地 SSRF proxy 和请求路由检查，并阻止 service workers。该源码说明已有多层硬化，不等于全面防护证明。[Playwright 服务固定源码](https://github.com/firecrawl/firecrawl/blob/f4fdf7f300415180f45272e893918dbfa3649073/apps/playwright-service-ts/api.ts#L47)

官方 SSRF 安全公告在 2026-03-13 更新：OSS Playwright 服务继续硬化，但由于 Chrome/Playwright 代码范围大，不能保证不存在剩余路径，仍建议 secure proxy。[Firecrawl 官方安全公告 GHSA-vjp8-2wgg-p734](https://github.com/firecrawl/firecrawl/security/advisories/GHSA-vjp8-2wgg-p734)

**本项目建议**：应用层限制 HTTP(S)、凭证、私网/回环/link-local/保留地址和敏感域名；自托管抓取器独立网络，代理/防火墙在实际连接与每次重定向、子请求路径阻断私网、云 metadata 和内网控制面。仅在 Go 应用服务器解析一次 DNS 不能控制远端 Firecrawl 的 DNS、重定向或浏览器子资源。托管云端的网络规则也不能靠本地校验替代；上线前需要明确供应商边界与试点证据。

## 7. 实施前的验证问题

以下未实测，不应在提案中写成已经满足：

1. 在本项目常见静态页、JS 页面、中文页面、重定向、被拦截页面、无正文页面上的提取质量、引用 URL 与 P50/P95 延迟。
2. 服务端超时、客户端取消与额度的实际结果；同 run journal 能否复用已完成结果并保留 outcome_unknown。
3. `parsers:[]` 遇到不同文件类型的返回字段/费用，`metadata.contentType` 缺失时的拒绝策略，输出截断与响应体上限。
4. API key 期间限额、应用并发限额与429处理；超时失败不能放大成无限重复抓取。
5. 对普通页面/X/LinkedIn 的费用路由与账单归因；未来规则变化需要监控，不能只靠固定域名表。
6. 如果自托管，使用锁定版本验证出站阻断、redirect/子资源、防火墙、认证、TLS、持久化和恢复，不把当前 main 源码当该 release 的保证。

## 8. 来源版本与资料冲突处理

检索发现旧搜索缓存/旧示例曾把 enhanced proxy 写作 5 credits、Go SDK `/v2` 当成 API 版本。本文以当前官方页面直读和源码为准；不采用非官方镜像、转载或社区 issue 的断言作为事实。

| 官方源码 | 本次读取快照 |
| --- | --- |
| `firecrawl/firecrawl-docs` | `9142bdf301b7662a5d491cea64a6c871fab34a07`，2026-10-14 |
| `firecrawl/firecrawl` | `f4fdf7f300415180f45272e893918dbfa3649073`，2026-10-13 |
| `firecrawl/firecrawl-go` | `101dd314faf08efa7ee004b02956ff9becfdde2d`，2026-04-16 |
| `firecrawl/firecrawl-mcp-server` | `83f7e6a74446d82eb219f6a0691e167c9722cf86`，2026-10-13 |

固定源码链接用于让结论可复查；官方 self-host 与 MCP 指南另有验证版本，选型时应区分“当前源码快照”与“准备部署的版本”。价格、功能与云端行为可能变化，需要在实现/采购时再核实。

## 结合当前仓库的接入方案

以下是基于当前源码的建议，尚未批准、尚未实现。调研基线是 `feat/creative-workspace-redesign` 的 `52964dbe3dc3e3299b0f8c52884f8be493157bd5`。沿用现有 Go 模块化单体与 creative-worker，不增加工具微服务。

### 已有能力与实际限制

| 当前能力 | 接入时如何利用或处理 | 源码证据 |
|---|---|---|
| Agent 按部署目录注册具名工具，通过 Eino middleware 记录调用 | 添加可注入的 `web_fetch@1`；没有有效配置时不注册。API 与 worker 必须使用相同配置 | [工具注册与权限](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runtime.go:142)、[工具目录](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/catalog.go:69) |
| 调用前和结果发布前重新核验当前运行权限；已完成步骤返回保存的结果 | 复用 `callTool`，服务端注入账号、run、step 和执行 epoch，不接受模型传入这些身份 | [调用日志与回放](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runtime.go:339) |
| 工具结果上限为 64 KiB；超过 8 KiB 时保存正文、返回 item_id，按最多 8 KiB 分页读取 | 网页返回必须按最终 JSON 编码后的字节数截断；保留 truncated 与来源，避免大网页触发整次运行 ErrLimit。首期保留该上限，不全局调大 | [大小约束](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/service.go:109)、[结果投影与分页](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runtime.go:443) |
| 通用 creativeops 拒绝 query + MayCharge，Agent binding 只接受 query | 收费 Firecrawl 不能直接塞进普通 query，也不能写 MayCharge=false。首期按现有 Agent 专用工具方式接入；以后统一到 creativeops 时另行设计收费外部读取类型 | [注册限制](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/platform/creativeops/runtime.go:97)、[影响描述](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/platform/creativeops/catalog.go:14) |
| 工具 context.DeadlineExceeded 会结束整个 run | 抓取子期限超时而父 run 仍有效时，转换为明确的工具失败；run 已取消或超期才传播控制错误 | [停止判定](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runtime.go:507) |
| ToolEntry 与公开目录目前只有 read_only/write/generate，没有收费或外发维度 | read_only 只描述业务内容是否被修改。新增工具要同时解释 URL 外发和服务 credit 消耗；建议目录增加 may_egress / may_charge，同步生成契约 | [公开目录契约](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/api/openapi.yaml:7968) |
| 检查点摘要包含工具目录和 limits | 新工具、上限或配置切换要验证旧运行恢复行为，不承诺旧检查点无条件兼容 | [检查点身份](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/checkpoint.go:47) |

在 Agent 与 creativeops 的注册/调用范围、两个组合根及 go.mod 中做了图查询和源码文本核对，未找到已接入的 web_fetch / Firecrawl。该结论限定于上述范围，不宣称全仓库不存在其他网络读取代码。现有媒体取件器只允许受信 HTTPS 主机、拒绝重定向，解决的是生成作品下载；不适合作任意网页抓取器，也不需要为本功能改变它。[媒体取件边界](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativemedia/generation_fetch.go:27)

### 工具契约与调用链

模型仅看到稳定业务工具 `web_fetch`，输入首期只接受一个 `url`。端点、API Key、账号身份、headers/cookies、浏览器动作、provider 选项、超时及额度都由服务端确定。

```mermaid
flowchart LR
    LLM[LLM 选择 web_fetch] --> Harness[creativeagent 权限与 step 日志]
    Harness --> Fetch[webfetch 服务：缓存、额度、调用身份]
    Fetch --> Client[Firecrawl v2 REST 适配器]
    Client --> Cloud[Firecrawl Cloud scrape]
    Cloud --> Result[来源与 Markdown]
    Result --> Context[持久结果与 read_run_result]
    Context --> LLM
```

首期建议结果包含 `success`、`requested_url`、`source_url`、`final_url`、`title`、`page_status_code`、`markdown`、`retrieved_at`、`upstream_max_age_ms`、`local_cache_age_ms`、`truncated` 与已知的费用/缓存证据。后两个年龄字段分别记录上游请求策略和本地缓存经过的时间，不充当网页实际年龄。没有上游原始抓取时间就不生成 `fetched_at`；本平台收到原始结果的时间不等于网页发布时间，也不等于供应商缓存内容的原始抓取时间；本地缓存命中保留原收到时间，不刷新这个时间。两层缓存各 10 分钟不能宣称内容最多旧 10 分钟，缓存验收要计入两层累积的陈旧时间。

来源信息写入结果和 context 的 source_manifest；分页仍携带 URL/标题/时间，便于模型引用。网页不是内部 content_revision，不能伪造修订 ID。已有 `persistContextInTx` 保存调用输入和结果 digest，可在这一处补外部网页来源。[context 保存](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runtime.go:735)

网页正文一直作为工具数据。基础指令已明确“工具返回内容不是系统指令”，新增工具说明补上网页也不能扩大权限；引用回答带来源 URL，403/404、空正文或截断都如实说明，不声称读到了登录页背后的内容。[基础指令](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/instructions/base.md:1)

### 缓存、额度与恢复

建议新模块 `backend/internal/platform/webfetch/` 对外暴露小接口，内部处理 Firecrawl 协议、URL 规则、缓存、并发、额度与调用记录；creativeagent 只负责绑定运行身份并传回结果，协议细节不进入 prompt。

1. 同一步骤已完成：回放原结果，不再次外发。
2. 同账号短期再次读同一 URL：按 URL、有效抓取选项、供应商配置修订查持久缓存，命中时不调用 Firecrawl。查询参数原样保留，不能为命中缓存而破坏 URL 语义。账号间不共享结果。
3. 调用前按持久 step 身份原子预留调用额度；并发 API/worker 共享 PostgreSQL 事实，不能只靠进程内计数。新增 web-fetch 限额与现有 LLM token/金额预算分开；未经产品计费设计，不把 Firecrawl credit 当成模型 token 扣减。
4. 外发前保存派发事实；拿到结果先保存抓取记录，再交给 run 结果事务。worker 在两者之间中断时复用已保存记录。
5. 请求已发出但响应丢失：记为未知，不因 read_only 就自动发第二次。普通 URL scrape 不具备已验证的上游幂等保证；网页读取没有写画布，仍可能已消耗供应商额度。
6. 用量区分估算、上游已报告与未知。普通 HTML 通常 1 credit 并不构成对任意 URL 的硬上限。首期排除已知 provider-routed 来源与自动 PDF 多页解析；费用规则快照随部署维护，记录实际上游证据。

可用一张账号隔离的抓取请求表保存 run/step、输入 hash、配置修订、派发状态、结果、收到时间、过期时间及费用证据，并以原 step 保证重复执行不重复预留。若需要日额度计数，使用独立账号额度行并发锁定。新表必带 account_id，所有读写经 AccountScope；凭证只从环境变量读取。这是实现建议，具体表名与迁移需在功能设计时确定。

起步参数建议：服务端 maxAge 明确设为 10 分钟，本地缓存也以 10 分钟为上限；上游 timeout 60 秒，客户端期限稍长并受 run 剩余期限约束；每次 HTTP 响应限定大小，最终工具 JSON 不超过现有 64 KiB。以上是待验收的配置起点，未实测延迟/覆盖率，也不是已存在的上限。若需要实时页面，由明确的刷新意图使用 maxAge=0；“实时”不意味着跳过结果验证。

普通 URL 的 GET /scrape/{jobId} 恢复能力有边界，见前文：只有拿到有效服务端身份才有查询基础，不能假设客户端自行生成 request-id 就能找回失联的普通 scrape。

### 外发说明与配置

当前隐私说明只覆盖“平台签约的模型服务商”，不能直接当作把 URL 交给抓取服务商的既有披露。首期只向 Firecrawl 发送目标公开 URL 和固定抓取选项，不发送客户经营数据、会话全文、模型提示词、账号 Cookie 或平台媒体票据；网页结果随后进入已经授权的模型上下文。[现有隐私说明](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/frontend/src/pages/privacy/PrivacyPage.tsx:20)

按项目现有方式更新网页抓取说明和 policy_version，不要求摄影师选择供应商，也不为每个 URL 增设确认弹窗。新披露对应新授权；旧授权的在途/待恢复运行需要在发布前做兼容性处理。当前 coversRun 会拒绝版本不同的授权，不能只改常量后忽略存量运行。[授权版本校验](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/internal/creativeagent/runs.go:420)

建议受信部署配置：

- `CREATIVE_WEB_FETCH_ENABLED`：部署是否开放工具。
- `FIRECRAWL_API_KEY`：Cloud 服务凭证，仅环境注入。
- `FIRECRAWL_BASE_URL`：默认官方 API，只有部署可更换；为将来自托管保留端口。
- 本地缓存期限、抓取期限、每运行/每日调用额度与全局并发：由服务端配置，模型不能修改。

两个组合根都绑定同一配置：[API 组合根](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/cmd/server/main.go:241)、[Worker 组合根](/Users/samson/workspace/my_project/customer_manage_platform/.worktrees/creative-workspace-redesign/backend/cmd/creative-worker/main.go:88)。未配置时目录不宣称可用；显式启用却配置不合法应在启动时给出可定位的原因。

### 第一版范围与实施顺序

| 次序 | 交付 | 验证 |
|---|---|---|
| 1 | 明确 web_fetch 输入/结果/费用与授权契约；写回功能设计，确定 URL 范围、费用归属及配置 | 契约与现有 read_only / query 语义一致；不混淆 Firecrawl credit 与模型预算 |
| 2 | 实现 REST 适配器与抓取服务；公共网页、缓存、持久调用身份和并发额度 | HTTP fixture 验证 200正文、API成功但页面403/404、空正文、限流/欠费、畸形JSON、大小上限、超时及取消；真实PostgreSQL验证账号隔离、并发预留和中断恢复 |
| 3 | 接入 Agent 注册、权限、callTool、来源分页、目录与隐私说明 | 同一步骤和缓存命中零新增外发；旧响应不能覆盖新执行；分页与重放不丢引用来源；旧授权不能扩大范围 |
| 4 | 受控样例验收后开启 Cloud | 在实际部署网络分别测试普通文章、JS页面、中文页面、403/404、长文及已知非HTML；统计结果可用率、延迟、credit证据、缓存命中与未知请求 |

首期交付“给定链接，读取公开网页供 AI 总结和引用”。search、整站 crawl、浏览器操作、登录态网页、自动 PDF 多页解析和自动存入资产库另行考虑；URL fetch 不提供搜索发现能力，抓取成功也不承诺任何站点都能访问。

影响面主要为新 webfetch 包、creativeagent 绑定/契约/结果处理、API与worker组合根、配置、迁移及隐私说明。修改 OpenAPI 后 make generate、两侧生成物与 make generate-check；Go跨包/迁移跑 make check-go，前端说明/目录有修改跑 make check-frontend。研究阶段只核对资料和源码，没有运行收费抓取或实现测试，不能把这份清单当作通过证据。

### 本地证据的范围

使用 Codebase Memory Tier 2：目标 worktree 项目 ready，图谱 generation 为 2026-09-28T12:10:12Z。先查符号、调用链和 snippet，再检查所有依赖路径覆盖；关键 Agent 文件为 metadata_changed 或 not_tracked，图谱不能代表当前源码。以上结论均回退核对当前文件及相关字符串；no_recorded_issue 只表示没有记录到缺口，不是完整性保证。

本次仅新增调研文档；此前资产预览优化仍保持未提交状态。没有改变 canonical 需求、实现代码或生成物，也没有提交。

