# 媒体生成路由与 RunningHub

## 模型、部署和结果身份

`creativegeneration.ImageRegistrations` 将部署策略合并为唯一逻辑模型，摄影师和 Agent 继续只提交 `model_key`。GPT Image 2 优先 AiCodeWith，备用 RunningHub；Nano Banana Pro、Nano Banana 2 使用 RunningHub 稳定版；Nano Banana 2.1 使用 owner 指定的 RunningHub 低价渠道版（economy），当前仅此一个部署。

受理时按参数及参考素材筛选部署，冻结候选顺序和能力策略；禁止静默降低分辨率、减少张数或拆分调用。每候选最多2次提交、可恢复失败间隔5秒，最多8个候选，共享原生成期限。凭证、供应商余额及渠道不可用在确认未受理/终止后切换；参数错误、审核拒绝和取消停止。受理未知仍须核实，不因暂停计费而允许重发。

`llm_generation_routes` 保存当前调用及唯一结果调用，`llm_generation_route_calls` 以账号、根请求、序号绑定独立请求。画布始终使用根请求，Gateway内部解析当前调用。`GenerationProgress.Route` 表达路由状态，`Request` 保留真实调用事实。拿到合规的完整或部分结果后，下载、入库、发布失败只重试对应阶段。

## 配置与当前计费范围（2026-10-16）

API 与 creative-worker 使用相同配置，保留已有 `CREATIVE_GENERATION_PROTECTION_KEY`（32字节base64）。凭证只经环境注入。

- `AICODEWITH_API_KEY`：已有供应商，缺少凭证只关闭该供应商。
- `RUNNINGHUB_API_KEY`：RunningHub Enterprise-Shared Key，配置后启用 GPT Image 2、Nano Banana Pro、Nano Banana 2、Nano Banana 2.1；GPT Image 2 在两家都有凭证时作为备用部署。

RunningHub 不再要求价格配置文件、已确认费率或有效期。共享并发暂为1，每分钟10次提交；账号授权、外发同意、参考图校验、部署启停、次数上限及截止时间继续生效。API 与 Worker 同步部署，凭证调整后重启。

RunningHub 首期不接入供应商计费，不占账号/调用组预算、不写实际成本、不进入账单待核实，不将张数或未知费用伪造为零价。调用身份、供应商任务身份及原始观察/用量证据仍保存。计费后续单独立项。

AiCodeWith、Chat 和摄影师现有计费行为保持原样。混合路由里，旧 AiCodeWith 未知费用继续保留预留，RunningHub 备用调用不会挪用或结清它；AiCodeWith 后续账单仍按冻结价格正常结算。

实现以受信策略的 `billing=disabled` 固定该调用的计费范围，不能由公共请求传入。为复用已有许可/attempt身份，`llm_usage_reservations` 保留 `not_applicable` 身份行，金额预留为0、实际金额为NULL；不创建或修改预算桶，也不产生费用测量/结算回执。该状态明确表示“暂未接入计费”，不表示免费。路由的 `max_cost_micros` 只约束启用计费的调用预留，不能解释为含 RunningHub 的真实总费用上限；所有调用均受最大提交次数约束。

旧调用仍按其冻结策略执行。旧 RunningHub 已有预留不会因升级被清除；不提供用量的旧调用终态后停止费用轮询，继续等待独立账单证据。新 RunningHub 调用终态后直接结束自动调度；只有结果身份矛盾或受理未知才需异常核实，取件定位过期可按需查询原任务。

## 官方协议核对（2026-10-15）

- [GPT Image 2](https://www.runninghub.ai/runninghub-api-doc-en/api-448969336)：`rhart-image-g-2-official/text-to-image` / `image-to-image`。
- [Nano Banana Pro](https://www.runninghub.ai/runninghub-api-doc-en/api-448184497)：`rhart-image-n-pro-official/text-to-image` / `edit`。
- [Nano Banana 2](https://www.runninghub.ai/runninghub-api-doc-en/api-448184501)：`rhart-image-n-g31-flash-official/text-to-image` / `image-to-image`。
- [Nano Banana 2.1 文生图](https://www.runninghub.ai/zh-cn/call-api/api-detail/2134100800000100001) / [图生图](https://www.runninghub.ai/zh-cn/call-api/api-detail/2134100800000100002)：`rhart-image-n-nb21/text-to-image` / `image-to-image`。2026-10-16 从官方 SKU detail 的 `rhEndpoint` 和 `inputConfigJson` 核对；这是低价渠道版，不能标注成官方稳定版。
- 上述路径均以 `/openapi/v2/` 开头，使用Bearer鉴权。查询为 [POST /openapi/v2/query](https://www.runninghub.ai/runninghub-api-doc-en/api-425767807)。
- [新上传接口](https://www.runninghub.ai/runninghub-api-doc-en/api-425761098)为 `/openapi/v2/media/upload/binary`，multipart字段为 `file`，读取 `data.download_url`。2026-10-16 真实上传验证成功为 HTTP 200、JSON `code: 0`、`data.type: image`，成功码必须显式存在；香港区上传返回 `rh-hk-images-switch.xiaoyaoyou.com`，该精确域名仅加入上传返回地址校验，不凭上传证据扩展结果取件白名单。[旧工作流上传接口](https://www.runninghub.ai/runninghub-api-doc-en/api-425761099)返回内部路径，禁止拼接成公网URL。
- 文档中的GPT/Pro参考图上限10、Nano 2上限14；本产品保持最多9张。GPT支持JPEG/PNG/WebP；Nano的文档只声明JPEG/PNG。参考字节先与冻结摘要匹配再上传。25MiB是本地保守上限，不宣称供应商硬上限已实测。
- Nano Banana 2.1 独立注册为 `nano-banana-2.1`；14种比例、1K/2K/4K，产品默认2K。参考图限JPEG/PNG、每张10MiB，供应商最多14张，画布保持9张；超限直接拒绝，不压缩或删图。SKU宣传三档思考但输入配置未定义字段，因此不发送思考参数，也不接受GPT的quality。输出继续沿用现有受控取件域名，封面域名不作为生成结果域名的证据。
- 首期单张输出，拒绝透明背景、显式不支持的参数和批量出图。清晰度在产品内用 `1K/2K/4K`，发送时映射为小写。Nano不注册GPT的quality参数。
- 结果域名固定为官方示例中的 `rh-images-1252422369.cos.ap-beijing.myqcloud.com`、`rh-images-switch-1252422369.cos.ap-guangzhou.myqcloud.com`、`rh-images.xiaoyaoyou.com`，禁止任意域名或重定向。15分钟是本地重新查询定位的刷新窗口，不是供应商TTL保证。
- [错误码参考](https://www.runninghub.ai/runninghub-api-doc-en/doc-8287467)。未知提交响应不能因HTTP 5xx或错误文字而推断未受理。标准模型取消/按客户端ID找回能力未确认，Adapter不声明支持。

## 恢复、迁移和验证

先运行0069、0070迁移，再同步部署 API 与 Worker。0070 只扩展未计费调用状态，不改写任何旧账目。回滚前停止新准入，保留兼容 Worker 处理在途任务；0070 down 遇到未计费调用拒绝降级，0069 down 遇到路由记录拒绝删除。旧任务不回填路由。

账号隔离、部署门锁、调用组锁、路由行锁与单次提交许可继续生效。派发前部署检查统一在领取路径内复核；仅续期不改变修订号，真正策略/启停变更仍使冻结候选失效。标准HTTP Transport在取得可用连接前的DNS、拨号、TLS失败可证明未派发；连接后写入/响应失败仍属受理未知。参考图上传后不足15秒提交时间则不创建生成任务。

只读关系扫描见 [creative-generation-route-audit.sql](creative-generation-route-audit.sql)。相同成功结果刷新临时地址时不增加作品版本，不重复发布。

本地协议测试与真实供应商验收分开记录。测试使用本地TLS服务及 `storetest.NewURL(t)`，覆盖密钥独立启用、能力约束、计费关闭的完成/失败/取消/拒绝/未知、重试上限、混合路由保留旧预留、迟到账单、恢复及一次发布。全量后端门禁使用 `make check-go`。

真实供应商仍需验证四个模型的文生图/参考图、凭证类型、输出域名和格式、限流响应是否实际建任务。未知错误码保持保守分类。当前未执行真实供应商调用，不将模拟测试表述为生产验收；价格核实不再是本阶段的启用前置条件。

## 本轮验证记录（2026-10-16）

- 密钥独立启用、三个模型协议、画布备用出图/恢复、未计费调用全生命周期、混合路由保留旧预留及迟到账单回归通过。
- 独立审查发现两个通用调用组入口未兼容无预算桶状态，已修复；撤去修复的 overlay 回归明确失败，修复后完整 Gateway 门禁通过（构建、lint、全包测试，130.970s）。第二轮独立审查无未解决的 blocking / important。
- 全量 `make check-go` 的构建、lint 及所有其他包通过，唯一失败为 `reminder/digest` 的 Docker 容器启动端口检查超时，未进入业务断言；该包随后独立三轮通过（45.134s / 19.451s / 16.975s）。不将原全量命令记为通过。
- 未计费生命周期、重复Worker、取消、路由与旧账单终态的 `go test -race` 通过（215.352s）；该轮在调用组入口补丁前完成，调用组补丁由上述完整 Gateway 门禁覆盖。
- `git diff --check` 通过；无前端或公开 OpenAPI 契约变化，未执行真实供应商调用，未创建 Git 提交。

## Nano Banana 2.1 增量验证（2026-10-16）

- 先增加文生图/参考图端点用例，确认旧实现因缺少 `nano-banana-2.1` 失败；注册并映射新端点后，RunningHub 定向协议测试通过。
- 覆盖2.1参数映射、参考图地址、10MiB边界、9张上限、格式与未知参数拒绝、未计费策略、API Key独立装配及模型目录去重。
- `make check-go` 全部通过（构建、lint、全包测试），`git diff --check` 通过。无需新增迁移或公开契约变更；未执行真实供应商出图，未创建 Git 提交。

## 2026-10-16：退役平台供应商配额

Chat 与 Generation 不再设置或检查平台共享并发数、每分钟请求数。账号级并发和频率限制留待后续项目；现有账号/调用组预算、授权、单任务租约、幂等和最大提交次数保持原语义。供应商实际返回的限流仍按适配器失败语义处理，不能由本地放开额度消除。

新 attempt/task 的 `limit_key` 为空，不创建、锁定或递增 `platform_llm_limits`。0071 迁移允许 attempt 的空键；旧在途任务仍按原键幂等释放历史名额。旧表暂留用于兼容清理，已移除 Acquire 能力。API/Worker 一起升级；先迁移再启服务。回退不收紧空键约束，以免破坏新格式记录；旧 Worker 只适合处理它原先拥有的旧格式任务。

RunningHub 结果域名增加精确的 `rh-hk-images-1252422369.cos.ap-hongkong.myqcloud.com`（真实 `/openapi/v2/query` 成功任务核实）。适配器和受控取件使用同一列表，不放开 COS 通配符；上传专用域名不因此成为结果域名。
