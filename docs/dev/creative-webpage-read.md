# 助手读取与搜索网页（Firecrawl Cloud 试点）

设计见 [agent-webpage-read-design.md](../../.codestable/features/2026-10-14-agent-webpage-read/agent-webpage-read-design.md)、[agent-web-search-design.md](../../.codestable/features/2026-10-14-agent-web-search/agent-web-search-design.md)；选型调研见 [firecrawl-webfetch.md](../research/firecrawl-webfetch.md)。

## 组成

- `platform/webfetch.Client` 直调 Firecrawl `POST /v2/scrape`，请求参数固定（只要 markdown、只取正文、允许 10 分钟缓存、不跳过 TLS 校验），不自动重试，响应体上限 8 MiB。Firecrawl 的错误码和页面状态翻成命名错误；页面状态只有 2xx/304 算正常加载。
- `creativeagent` 的 `read_webpage@1` 工具只收一个 `url`，返回来源信息和正文 Markdown（截到 32 KiB，整条结果不超过 60 KiB；超过 8 KiB 时走现有 `read_run_result` 分页）。
- 同一次运行里同一链接（忽略 host 大小写、fragment、默认端口）只外发一次，之后成功和失败都回放上次结果；每次运行最多 20 次读取，按调用顺序计数，中断未完成的步骤也算。
- 不外发即拒绝：非 http/https、带用户名密码、`localhost` 和私网/回环/链路本地 IP 字面量、x.com / twitter.com / linkedin.com（Firecrawl 走高价通道）、`.pdf` 等文件扩展名、运行剩余时间不足。
- `search_web@1` 只收一个 `query`（200 字符以内），调 Firecrawl `POST /v2/search`，固定 10 条结果（2 credit），只返回标题、链接和摘要，不抓正文；助手挑结果后用 `read_webpage` 读。摘要逐步截短，让结果不超过 8 KiB 直接给模型。同一运行同一搜索词（忽略大小写和多余空白）回放；每次运行最多 10 次搜索。
- 日志 `creative agent webpage read` 记 host、path 哈希、页面状态、耗时、缓存状态，不记完整 URL 和正文。凭证被 Firecrawl 拒绝时记 Error。`creative agent web search` 只记搜索词长度和哈希，不记原文。

## 配置

| 环境变量 | 说明 |
|---|---|
| `FIRECRAWL_API_KEY` | 有值即开启 `read_webpage` 与 `search_web`；留空则两个工具都不出现在目录里 |
| `FIRECRAWL_BASE_URL` | 可选，默认 `https://api.firecrawl.dev`；必须是 https 绝对地址，否则启动失败 |

API 进程决定工具目录，creative-worker 实际执行，两侧必须同时配置。切换开关会改变已注册工具摘要，切换时处于等待中的运行无法从检查点恢复。

Firecrawl 控制台（不进代码）：

- 为本部署单独建 key，设月度 credit 上限，关闭 pay-as-you-go。
- 控制台支持按 key 限制输出格式时，限定为 markdown。

## 试点边界

- 只在开发/测试部署配置 `FIRECRAWL_API_KEY`，生产留空。现有外发说明只覆盖模型服务商，没有覆盖网页服务商；搜索词可能带有对话内容。
- 生产开启前单独改：外发说明增加网页服务商（发送网页链接与搜索词）、升级 `policyVersion` 与前端 `egressNoticeVersion`，并回写 `creative-shoot-planning.md` 关于外部链接的边界。
- 试点结果（20～30 个真实链接的成功率、耗时、正文可用性）记录在 `agent-webpage-read-verification.md`。

## 测试

- `platform/webfetch` 用 `httptest` 覆盖请求参数、全部响应映射、超时语义（子期限是 `ErrTimeout`，不是 `context.DeadlineExceeded`）。
- `creativeagent/webpage_tool_test.go`、`search_tool_test.go` 用假抓取器覆盖开关、前置校验、回放、每次运行上限和结果截断。
