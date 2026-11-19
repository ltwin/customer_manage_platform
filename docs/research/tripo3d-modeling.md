# Tripo3D API 建模能力接入调研

调研日期：2026-11-10。本文是研究结论与待设计事项，不代表已批准实现。

资料范围：Tripo 官方开发者文档（国际站 `developers.tripo3d.ai` 与中国大陆站 `developers.tripo3d.com`，含站点提供的 `llms-full.txt` 全文语料与各页 `.md` 原文）、官方定价页与模型页（含其前端 bundle 中的价格数据）、官方服务条款、官方隐私政策（`www.tripo3d.ai/privacy` 对脚本访问返回 403，改读 Internet Archive 2026-10-16 快照）、官方 GitHub 组织 `Tripo-Developer` 与 `VAST-AI-Research` 的仓库元数据与源码。先用 Context7 定位：Context7 收录的 `/websites/platform_tripo3d_ai` 是已弃用的 v2 文档（`platform.tripo3d.ai` 现已跳转到 `developers.tripo3d.ai`），因此本文结论全部以 2026-11-10 直读的 v3 官方页面为准。

**未实测声明**：本次未注册账号、未使用任何 API key、未调用任何收费或免费接口，也未从阿里云 ECS 做网络连通性测试。文中耗时、价格、限额均为官方页面所写数字，不是本项目实测值。示例响应里的 `credits_consumed: 100.00` 等是文档示意值，不代表该任务的真实单价。

## 1. 结论摘要

### 已核实事实（要点）

| 主题 | 结论 | 来源 |
|---|---|---|
| API 版本 | 当前是 **v3**（2026-04 发布），按能力拆成专用端点；v2 通用端点 `POST /v2/openapi/task` 已计划 2026-10-31 关停 | [Changelog](https://developers.tripo3d.com/en/docs/changelog)、[SDK Integration](https://developers.tripo3d.com/en/docs/sdk) |
| 地区 | 国际站 `https://openapi.tripo3d.ai/v3` 与中国大陆站 `https://openapi.tripo3d.com/v3` 是两套账号/控制台/计费，API key 只在所属地区有效 | [Introduction（国际）](https://developers.tripo3d.ai/en/docs/introduction)、[Introduction（大陆）](https://developers.tripo3d.com/en/docs/introduction)、[v2→v3 迁移 Skill 附录 B](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md) |
| 任务模型 | 全部异步：POST 创建拿 `task_id` → `GET /v3/tasks/{task_id}` 轮询或批量 `POST /v3/tasks/list`；另有控制台配置的 **Webhook**（HMAC-SHA256 签名） | [Task Lifecycle](https://developers.tripo3d.com/en/docs/task-lifecycle)、[Webhooks](https://developers.tripo3d.com/en/docs/webhooks) |
| 取消 / 幂等 | 文档有 `cancelled` 状态，但**未提供取消端点**；**没有**客户端幂等键，官方明确要求创建请求响应丢失时不要盲目重发 | [Task Query](https://developers.tripo3d.com/en/docs/task-query)、[Error Handling](https://developers.tripo3d.com/en/docs/error-handling) |
| 输出 | 生成默认输出 GLB，`pbr` 默认 `true`（base_color/metallic/roughness/normal）；H 系列 `quad=true` 会强制输出 FBX；格式转换支持 GLTF/FBX/USDZ/OBJ/STL/3MF | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard)、[Convert](https://developers.tripo3d.com/en/docs/models-convert) |
| 绑骨 | `POST /v3/animations/rig`，`spec` 可选 `mixamo` / `unity` / `unreal` / `vrm` / `actorcore` 骨骼命名（仅 biped），输出 GLB/FBX | [Auto Rigging](https://developers.tripo3d.com/en/docs/animations-rig) |
| 面数控制 | 有 `face_limit`、`smart_low_poly`、`quad`；P 系列（P1/P2）专做干净拓扑/低模，P2 原生四边面 | [Models & Versions](https://developers.tripo3d.com/en/docs/models-and-versions) |
| 计费 | credit 制，先冻结后扣，失败/取消不扣费；国际站 1 credit = $0.01，大陆站 1 credit = ¥0.07；两站 credit 单价数字相同 | [Billing（国际）](https://developers.tripo3d.ai/en/docs/billing)、[Billing（大陆）](https://developers.tripo3d.com/en/docs/billing)、[Pricing](https://developers.tripo3d.ai/en/pricing) |
| 许可 | 付费用户拥有输入与输出的权利，官方声明不拿付费用户的输入输出训练；**但条款禁止未经书面授权向第三方（含最终用户）提供其生成式 3D 基础模型服务**，这条直接影响 SaaS 转售 | [Terms of Service](https://developers.tripo3d.ai/en/terms) |
| Go SDK | **有官方 Go SDK**：`github.com/Tripo-Developer/tripo-go-sdk`，v0.5.0，MIT，2026-10-23 最近提交 | [SDK Integration](https://developers.tripo3d.com/en/docs/sdk)、[tripo-go-sdk](https://github.com/Tripo-Developer/tripo-go-sdk) |

### 建议（非官方事实）

- 部署在阿里云大陆 ECS、面向大陆摄影师时，优先评估**中国大陆站**（`openapi.tripo3d.com`，支付宝结算，产物走百度 CDN 域名）；国际站从大陆服务器访问的稳定性未在官方来源找到，需要实测。
- 接入前必须先就第 7 节的「向第三方/最终用户提供服务」条款取得 Tripo 书面确认或企业协议，否则 SaaS 形态有合规风险。
- 创建任务的 HTTP 调用按「可能已扣费」处理：超时或 5xx 后不自动重发，先查任务列表/用量；这与本项目现有生图供应商「异步提交 + 轮询」模式一致。
- 输出 URL 是临时签名链接，任务成功后应立即转存到本项目对象存储，不能把 Tripo URL 当持久资产。

## 2. 地区、账号与鉴权

### 两套地区

| 项目 | 国际站 | 中国大陆站 | 来源 |
|---|---|---|---|
| API Base URL | `https://openapi.tripo3d.ai/v3` | `https://openapi.tripo3d.com/v3` | [Introduction（国际）](https://developers.tripo3d.ai/en/docs/introduction)、[Introduction（大陆）](https://developers.tripo3d.com/en/docs/introduction) |
| 控制台（key、webhook、计费） | `developers.tripo3d.ai` | `developers.tripo3d.com` | [迁移 Skill B.2](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md) |
| 支付方式 | Stripe | 支付宝（另有企业对公转账、发票指南） | [Tripo CLI](https://developers.tripo3d.com/en/docs/cli)、[Enterprise Bank Transfer](https://developers.tripo3d.com/en/docs/b2b-payment)、[Invoicing Guide](https://developers.tripo3d.com/en/docs/invoice-guide) |
| credit 折算 | 1 credit = $0.01 USD | 1 credit = ¥0.07 CNY | [Billing（国际）](https://developers.tripo3d.ai/en/docs/billing)、[Billing（大陆）](https://developers.tripo3d.com/en/docs/billing) |
| 产物下载域名（防火墙白名单用） | `tripo-data.rg1.data.tripo3d.com`（CloudFront） | `openapi.cdn.tripo3d.com`（百度 CDN） | [迁移 Skill B.2](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md) |
| 企业联系 | business@tripo3d.ai | business@tripo3d.com | [Pricing（国际）](https://developers.tripo3d.ai/en/pricing)、[Pricing（大陆）](https://developers.tripo3d.com/en/pricing) |

- API key 只在所属地区有效；用错地区的 Base URL 会返回 HTTP 401。[迁移 Skill B.1](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)
- v3 端点、参数、响应结构与错误码两区相同；已知差异仅为 P1 `face_limit` 下限（国际 48、大陆 50）以及 P1 对禁用参数的校验宽严不同，官方建议对 P1 完全省略 `quad` / `smart_low_poly` / `generate_parts` / `geometry_quality`。[迁移 Skill B.3](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)
- 官方 SDK 页逐行对比：两站 SDK 页面除 Base URL / region 选择外内容一致；Billing、Rate Limits、Changelog 两站除币种外一致（本次逐文件 diff）。[SDK（国际）](https://developers.tripo3d.ai/en/docs/sdk)、[SDK（大陆）](https://developers.tripo3d.com/en/docs/sdk)
- 从大陆服务器访问国际站 API 的可用性、延迟和是否被限制：**未在官方来源找到**。官方仅提示「从中国访问请使用中国站」。[Introduction](https://developers.tripo3d.com/en/docs/introduction)

### 鉴权

- 所有请求带 `Authorization: Bearer {api_key}`；key 在控制台创建，只显示一次。[Authentication](https://developers.tripo3d.com/en/docs/authentication)
- 官方安全建议：key 放环境变量（示例变量名 `TRIPO_API_KEY`），不要进代码仓库，**不要在前端直接使用，需经后端代理**。[Authentication](https://developers.tripo3d.com/en/docs/authentication)
- key 前缀示例为 `tsk_...`。[SDK Integration](https://developers.tripo3d.com/en/docs/sdk)
- 签名下载链接不需要也不应附带 `Authorization` 头。[SDK Integration](https://developers.tripo3d.com/en/docs/sdk)

### 统一响应格式

成功为 `{"code": 0, "data": {...}}`；失败为 `{"code": <非0>, "status": "error", "message", "suggestion", "request_id"}`，`request_id` 可能缺失。[Introduction](https://developers.tripo3d.com/en/docs/introduction)、[Error Handling](https://developers.tripo3d.com/en/docs/error-handling)

## 3. 任务模型

### 生命周期

| 项目 | 官方事实 | 来源 |
|---|---|---|
| 创建 | 各能力专用 `POST` 端点，同步返回 `{"data": {"task_id": "..."}}`（UUID 形式） | [Introduction](https://developers.tripo3d.com/en/docs/introduction) |
| 单任务查询 | `GET /v3/tasks/{task_id}`，返回 `type`、`status`、`progress`(0–100)、`output`、`credits_consumed`、`created_at`、`completed_at`、失败时 `error_code` / `error_message` | [Task Query](https://developers.tripo3d.com/en/docs/task-query) |
| 批量查询 | `POST /v3/tasks/list`，`task_ids` 最多 100 个，返回 `tasks` 映射与 `missed` 列表 | [Batch Task Query](https://developers.tripo3d.com/en/docs/task-batch-query) |
| 状态枚举 | `queued` → `running` → `success` / `failed` / `cancelled` | [Task Lifecycle](https://developers.tripo3d.com/en/docs/task-lifecycle) |
| 历史状态 | v2 的 `banned`（审核拒绝）、`expired`（排队超时）在 v3 归入 `failed`，分别对应 `error_code` 2008 / 2018；偶发 `unknown` 不应当作成功或终态，应有界等待 | [Task Lifecycle](https://developers.tripo3d.com/en/docs/task-lifecycle)、[迁移 Skill §5.1](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md) |
| 任务错误码 vs 请求错误码 | 失败任务的 `error_code` 与请求级 `code` 是两套编号，同一数字含义可能不同 | [Error Handling](https://developers.tripo3d.com/en/docs/error-handling) |
| 轮询建议 | 每 1–2 秒查询一次；设最大轮询时长（示例 5 分钟）；批量任务用 `tasks/list` | [Task Lifecycle](https://developers.tripo3d.com/en/docs/task-lifecycle) |

### 回调：Webhook

| 项目 | 官方事实 |
|---|---|
| 配置方式 | 在控制台 Settings → Webhooks 添加 HTTPS 端点，选择事件类型；**不是每次请求传 callback URL** |
| 事件 | `task.completed`、`task.failed`（含被取消）、`balance.low`（余额低于控制台设定阈值） |
| 签名 | 头 `Tripo-Webhook-Signature: t=<unix>,v1=<hex>`，对 `{timestamp}.{raw_body}` 做 HMAC-SHA256，密钥为控制台一次性显示的 `whsec_…`；建议拒绝超过约 5 分钟的时间戳 |
| 去重 | 用 `Tripo-Webhook-Delivery` 头去重；同一逻辑事件可能投递多次 |
| 重试 | 非 2xx 或超时自动重试；建议约 5 秒内返回 2xx；重试次数与间隔**未在官方来源找到** |
| payload | 任务事件含 `task_id`、`type`、`status`，成功时含 `output`；失败事件示例为 `error: {code, message}` |

来源：[Webhooks](https://developers.tripo3d.com/en/docs/webhooks)。官方 Go SDK v0.5.0 已包含 webhook 验签代码（`webhook.go`）。[tripo-go-sdk](https://github.com/Tripo-Developer/tripo-go-sdk/tree/v0.5.0)

WebSocket 推送：v3 **不提供**。迁移 Skill 明确写 v2 的 `GET /task/watch/*` WebSocket 在 v3 无对应接口，改用轮询加 webhook。[迁移 Skill §6](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)

### 结果 URL 有效期

- v3 文档只说生成的模型 URL 是**临时签名链接**，应立即下载，不要当永久资产保存；过期后重新查询任务拿新链接。[SDK Integration](https://developers.tripo3d.com/en/docs/sdk)
- 官方迁移 Skill 写明「两区签名下载 URL 约 24 小时有效」。[迁移 Skill B.2](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)
- 资料冲突：Context7 收录的旧 v2 文档写「默认 5 分钟过期」，旧 FAQ 又写「60 秒」。这些是已弃用文档，不作为 v3 依据。具体时长仍应实测，实现上按「拿到即转存」处理。

### 取消与幂等

- **取消**：状态里有 `cancelled`（「cancelled by user」），取消的任务退还冻结额度；但 v3 端点总览、SDK 路由矩阵中都**没有取消接口**。是否能通过控制台取消、API 层如何触发取消：**未在官方来源找到**。[Task Query](https://developers.tripo3d.com/en/docs/task-query)、[Introduction](https://developers.tripo3d.com/en/docs/introduction)、[SDK 路由矩阵](https://developers.tripo3d.com/en/docs/sdk)
- **幂等 / 客户端 ID**：创建端点**无**幂等键或客户端自定义 ID 参数；`request_id` 只是排障标识。官方原文：创建请求即使响应丢失也可能已成功，结果未知前不要重新提交。[Error Handling](https://developers.tripo3d.com/en/docs/error-handling)
- 官方 Go SDK 的实现说明了这点：任务创建按提交计费，429/503 视为「服务端未受理、可安全重试」；408/425/500/502/504 视为「可能已受理」，非幂等方法不自动重试，并提示调用方先检查任务列表，避免重复扣费。[tripo-go-sdk http.go](https://github.com/Tripo-Developer/tripo-go-sdk/blob/7a040a3f198b/http.go)
- 对账辅助：`GET /v3/account/usage/history` 可按时间窗（≤31 天）分页列出该 key 的**成功**任务及扣费；不含 prompt、输入输出和下载链接；限 60 次/分钟/key；建议只查 30 分钟前结束的时间窗。[Usage History](https://developers.tripo3d.com/en/docs/account-usage-history)

### 任务归属范围

- 批量查询的 `missed` 指「当前认证客户端找不到的任务」；Smart Segmentation 以 `task_id` 作为输入时要求是「同一 API key 创建的成功任务」。[Batch Task Query](https://developers.tripo3d.com/en/docs/task-batch-query)、[Smart Segmentation](https://developers.tripo3d.com/en/docs/mesh-smartsegment)
- 同一账号下不同 API key 能否互查任务、其他后处理端点用 `task_id` 输入时是否要求同 key：**未在官方来源找到明确说明**。

## 4. 任务类型与参数

### 4.1 v3 端点总览

| 类别 | 端点 | 说明 | 来源 |
|---|---|---|---|
| 3D 生成 | `POST /v3/generation/text-to-model` | 文生 3D；H / P 系列共用端点，靠 `model` 区分 | [Text to 3D — H](https://developers.tripo3d.com/en/docs/generation-text-to-model/standard)、[— P](https://developers.tripo3d.com/en/docs/generation-text-to-model/p) |
| 3D 生成 | `POST /v3/generation/image-to-model` | 单图生 3D | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard)、[— P](https://developers.tripo3d.com/en/docs/generation-image-to-model/p) |
| 3D 生成 | `POST /v3/generation/multiview-to-model` | 2–4 张多视图（正面必填）生 3D | [Multiview — H](https://developers.tripo3d.com/en/docs/generation-multiview-to-model/standard)、[— P](https://developers.tripo3d.com/en/docs/generation-multiview-to-model/p) |
| 3D 生成 | `POST /v3/generation/image-to-splat` | 单图生 Gaussian Splat（`.splat`） | [Image to Splat](https://developers.tripo3d.com/en/docs/generation-image-to-splat) |
| 图像 | `text-to-image` / `image-to-image` / `image-to-multiview` / `edit-multiview` | 生图与多视图预处理 | [Text to Image](https://developers.tripo3d.com/en/docs/generation-text-to-image)、[Image to Multiview](https://developers.tripo3d.com/en/docs/generation-image-to-multiview) |
| 模型处理 | `POST /v3/models/texture` | 对已有模型重新贴图 | [Texture](https://developers.tripo3d.com/en/docs/models-texture) |
| 模型处理 | `POST /v3/models/convert` | 格式转换与导出处理 | [Convert](https://developers.tripo3d.com/en/docs/models-convert) |
| 模型处理 | `POST /v3/models/import` | 导入外部模型供后续处理 | [Import](https://developers.tripo3d.com/en/docs/models-import) |
| 网格 | `POST /v3/mesh/segment` / `mesh/complete` / `mesh/smartsegment` | 部件分割、部件补全、端到端智能分割 | [Segment](https://developers.tripo3d.com/en/docs/mesh-segment)、[Complete](https://developers.tripo3d.com/en/docs/mesh-complete)、[SmartSegment](https://developers.tripo3d.com/en/docs/mesh-smartsegment) |
| 网格 | `POST /v3/mesh/decimate` | 重拓扑 / 减面 | [Retopology](https://developers.tripo3d.com/en/docs/mesh-decimate) |
| 网格 | `POST /v3/mesh/uv` | Smart UV：重展 UV 并迁移贴图、骨骼、动画 | [Smart UV](https://developers.tripo3d.com/en/docs/mesh-uv) |
| 动画 | `POST /v3/animations/rig-check` / `rig` / `retarget` | 可绑定检测、自动绑骨、预设动画重定向 | [Rig Check](https://developers.tripo3d.com/en/docs/animations-rig-check)、[Rig](https://developers.tripo3d.com/en/docs/animations-rig)、[Retarget](https://developers.tripo3d.com/en/docs/animations-retarget) |
| 通用 | `tasks/{id}`、`tasks/list`、`files`、`files/presign`、`account/balance`、`account/usage`、`account/usage/history` | 查询、上传、账户 | [Introduction](https://developers.tripo3d.com/en/docs/introduction) |

**已下线**：`refine`、`stylize` 在 v3 已退役（Python SDK 1.x 调用时直接报错不发请求）；v2 的 `classification`、`render_video` 无 v3 替代。[SDK Integration](https://developers.tripo3d.com/en/docs/sdk)、[迁移 Skill](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)。题目中的 `stylize` 因此不可用；`animate` 对应 v3 的 `rig-check` / `rig` / `retarget`，`segment/part` 对应 `mesh/segment`、`mesh/complete`、`mesh/smartsegment` 与生成参数 `generate_parts`。

**另一官方入口（仅生图）**：官方 Go SDK README 说明 `<base URL>/openai` 提供 OpenAI Images API 兼容协议，只接受 `gpt-image-2`、`gpt-image-2.5-flare`、`gpt-image-2.5-sunburst`，返回签名 URL。该入口未出现在文档站端点总览里，本文不展开。[tripo-go-sdk README](https://github.com/Tripo-Developer/tripo-go-sdk/blob/7a040a3f198b/README.md)

### 4.2 3D 生成模型版本

| `model` | 系列 | 官方定位 | 面数范围 | 来源 |
|---|---|---|---|---|
| `v3.1-20260211` | H3.1 | 推荐，最新最高精度；示例默认值 | 三角 Standard ≤1,500,000，Ultra（`geometry_quality=detailed`）≤2,000,000；Quad ≤150,000 | [Models & Versions](https://developers.tripo3d.com/en/docs/models-and-versions)、[Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| `v3.0-20250812` | H3.0 | 上一代稳定版，向后兼容 | 三角 Standard ≤1,000,000，Ultra ≤2,000,000 | 同上 |
| `v2.5-20250123` | H2.5 | 旧版，历史项目兼容 | 三角 ≤500,000；不支持 v3.0+ 专属参数 | 同上 |
| `P2-20260801` | P2 | 原生四边面拓扑，面向游戏角色、硬表面、实时管线 | 三角 48–50,000；`quad=true` 时 48–25,000 | [Models & Versions](https://developers.tripo3d.com/en/docs/models-and-versions)、[Tripo P2](https://developers.tripo3d.ai/en/models/p2) |
| `P1-20260311` | P1 | 低模性能最好，面向 UGC / 移动端 | 50–20,000（国际站下限 48） | 同上 |

- `model` 对三类 3D 生成端点**必填**。生产环境官方建议固定具体版本号。[Quick Start](https://developers.tripo3d.com/en/docs/quick-start)、[Models & Versions](https://developers.tripo3d.com/en/docs/models-and-versions)
- v1.x / v2.0 / Turbo 等旧版本已退役，映射到 `v2.5-20250123`。[迁移 Skill](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)

### 4.3 生成端点关键参数

下表合并 H 系列与 P 系列 image-to-model / text-to-model / multiview-to-model 的参数；「H」「P」列标注是否支持。

| 参数 | 默认 | 语义 | H | P | 来源 |
|---|---|---|---|---|---|
| `prompt` | — | 文生 3D 必填，≤1024 字符 | ✓ | ✓ | [Text to 3D — H](https://developers.tripo3d.com/en/docs/generation-text-to-model/standard) |
| `negative_prompt` | — | ≤255 字符 | ✓ | ✓ | 同上 |
| `image_seed` | 随机 | 文生 3D 内部先文生图，再转 3D；控制中间参考图 | ✓ | ✓ | 同上 |
| `input` | — | 图生 3D：`file_token` / 公网 URL / 生图任务的 `task_id`，三选一；PNG/JPEG/WebP，≤20 MB，建议 ≥256×256 | ✓ | ✓ | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| `inputs` | — | 多视图：view-key 数组（`front`/`left`/`back`/`right`，推荐）、4 元位置数组，或单个 `image_to_multiview` / `edit_multiview` 任务 ID；正面必填，至少 2 张 | ✓ | ✓ | [Multiview — P](https://developers.tripo3d.com/en/docs/generation-multiview-to-model/p) |
| `model_seed` / `texture_seed` | 随机 | 同 seed 同输入得到相同网格 / 贴图；固定 `model_seed` 换 `texture_seed` 可只换贴图 | ✓ | ✓ | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| `face_limit` | 自适应 | 输出面数上限；见 4.2 | ✓ | ✓ | 同上 |
| `texture` | `true` | `false` 输出无贴图白模 | ✓ | ✓ | 同上 |
| `pbr` | `true` | 生成 `base_color`、`metallic`、`roughness`、`normal` 贴图；为 `true` 时强制 `texture=true` | ✓ | ✓ | 同上 |
| `texture_quality` | `standard` | `fast`（需贴图 v3.5，价同 standard）/ `standard` / `detailed` / `extreme`（8K） | ✓ | ✓ | 同上 |
| `texture_version` | 随几何版本 | `v3.5-20260815`（最新）/ `v3.0-20250812` / `v2.5-20250123` | ✓ | ✓ | 同上、[Changelog](https://developers.tripo3d.com/en/docs/changelog) |
| `delight` | `true` | 仅贴图 v3.5：去除参考图里烘焙的光照；做 CG/渲染想保留原始光影时关掉 | ✓ | ✓ | 同上、[Tripo Texture](https://developers.tripo3d.ai/en/models/texture) |
| `texture_alignment` | `original_image` | 贴图优先贴合原图颜色或几何 | ✓ | ✓ | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| `orientation` | `default` | `align_image` 使模型朝向对齐输入图视角；仅 `texture=true` 时生效 | ✓ | ✓ | 同上 |
| `enable_image_autofix` | `false` | 生成前自动增强低质量输入图 | ✓ | ✓ | 同上 |
| `auto_size` | `false` | 按真实尺寸缩放，单位米 | ✓（≥v3.0） | ✓ | 同上 |
| `export_uv` | `true` | `false` 更快、文件更小 | ✓ | ✓ | 同上 |
| `export_orientation` | `+x` | 前向轴 `+x` / `-x` / `+y` / `-y`；官方提醒：后续要做贴图/绑骨/转换时**不要设置**，否则可能方向错误却仍报成功，应在最后一步用 convert 调整 | ✓ | ✓ | 同上 |
| `geometry_quality` | `standard` | `detailed` = Ultra 精细几何；仅 ≥v3.0 | ✓ | ✗ | 同上 |
| `quad` | `false` | 输出四边面；H 系列未设 `face_limit` 时默认 10,000 面，且**强制输出 FBX**；P 系列仅 P2 支持 | ✓（≥v3.0） | P2 | 同上、[Image to 3D — P](https://developers.tripo3d.com/en/docs/generation-image-to-model/p) |
| `smart_low_poly` | `false` | 手工风格干净低模拓扑；面数固定为三角 500–20,000 / 四边 500–10,000；复杂输入可能失败 | ✓（≥v3.0） | ✗ | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| `generate_parts` | `false` | 生成可编辑的分件；必须 `texture=false` 且 `pbr=false`，否则 1004；与 `quad` / `smart_low_poly` 互斥或被覆盖 | ✓（≥v3.0） | ✗ | 同上 |
| `compress` | — | `geometry` = meshopt 压缩 | ✓（≥v3.0） | ✗ | 同上 |

- 「风格」参数：v3 生成端点**没有**独立 `style` 参数；风格靠 prompt 或贴图端点的 `texture_prompt`（文本 / `style_image` / 参考图）表达；`stylize` 已退役。[Text to 3D — H](https://developers.tripo3d.com/en/docs/generation-text-to-model/standard)、[Texture](https://developers.tripo3d.com/en/docs/models-texture)
- 成功输出：`output.model_url`（模型）与 `output.rendered_image_url`（预览图）；v2 的 `pbr_model` / `model` / `base_model` 合并为 `model_url`。[Task Query](https://developers.tripo3d.com/en/docs/task-query)、[迁移 Skill §5.2](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)

### 4.4 输出格式与 PBR

| 问题 | 官方事实 | 来源 |
|---|---|---|
| 默认输出格式 | 文档快速入门：从 `output.model_url` 下载 **GLB**，可直接用于 Blender、Unity、Three.js | [Quick Start](https://developers.tripo3d.com/en/docs/quick-start) |
| GLB 是否带 PBR | `pbr` 默认 `true`，产出 base_color / metallic / roughness / normal 四类贴图；H3.1 模型页标注输出「Mesh + PBR」 | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard)、[Tripo H3.1](https://developers.tripo3d.ai/en/models/v3-1) |
| 例外 | H 系列及 convert 的 `quad=true` 强制输出 FBX；P2 `quad=true` 的输出格式文档未写明 | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard)、[Convert](https://developers.tripo3d.com/en/docs/models-convert) |
| 是否带 emissive / AO / 透明 | **未在官方来源找到** | — |
| 坐标/尺度 | 默认前向 `+x`；`auto_size=true` 才按米制缩放，否则尺度未承诺；导入外部模型会做归一化，并在输出返回 `normalize_scale` / `normalize_translation` | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard)、[Import](https://developers.tripo3d.com/en/docs/models-import) |

### 4.5 格式转换 `POST /v3/models/convert`

| 参数 | 默认 | 语义 |
|---|---|---|
| `input` | — | `task_id` / `file_token` / URL；源格式 GLB/GLTF/FBX/OBJ/STL，≤150 MB |
| `format` | 必填 | `GLTF`、`FBX`、`USDZ`、`OBJ`、`STL`、`3MF`（列表中**没有 GLB**，GLB 是生成默认产物） |
| `quad` / `force_symmetry` | `false` | 四边面化（强制 FBX，未设面数默认 10,000）；对称仅在 quad 时有效 |
| `face_limit` | 保持原面数 | 输出面数上限 |
| `texture_size` / `texture_format` | `4096` / `JPEG` | 贴图尺寸、格式（PNG/WEBP/EXR 等） |
| `bake` | `true` | 把高级材质烘焙到基础贴图 |
| `pack_uv` / `export_vertex_colors` | `false` | 合并 UV；顶点色仅 OBJ/GLTF |
| `pivot_to_center_bottom` / `scale_factor` / `flatten_bottom` | `false` / `1` / `false` | 轴心、缩放、底面压平（3D 打印用） |
| `with_animation` / `animate_in_place` | `true` / `false` | 是否包含骨骼动画、原地动画 |
| `fbx_preset` | `blender` | `blender` / `3dsmax` / `mixamo` / `bake_scale` |
| `texture_packaging` | `embedded` | 仅 FBX：`zip` 时返回 `.fbx` + 独立贴图目录 |
| `export_orientation` | `+x` | 前向轴 |
| `part_names` | — | 仅导出分割得到的指定部件 |

来源：[Convert](https://developers.tripo3d.com/en/docs/models-convert)、[Changelog 2026-11](https://developers.tripo3d.com/en/docs/changelog)

### 4.6 绑骨与动画

| 项目 | 官方事实 | 来源 |
|---|---|---|
| Rig Check | `POST /v3/animations/rig-check`，返回 `riggable` 与推荐 `rig_type`；**免费** | [Rig Check](https://developers.tripo3d.com/en/docs/animations-rig-check)、[Pricing](https://developers.tripo3d.ai/en/pricing) |
| Rig 输入 | `task_id`（3D 生成任务）/ `file_token` / URL；GLB/FBX/OBJ/STL，≤150 MB | [Auto Rigging](https://developers.tripo3d.com/en/docs/animations-rig) |
| `model` | 默认仍是 `v1.0-20240301`（仅 biped，90+ 预设动画）；`v2.5-20260210`（非人形生物）；`v3.0-20260909` **官方推荐用于人形**，更快更好，需显式指定 | 同上 |
| `rig_type` | `biped`（默认）/ `quadruped` / `hexapod` / `octopod` / `avian` / `serpentine` / `aquatic`；v3.0 新增 `others` | 同上 |
| `spec`（骨骼命名，仅 biped） | `tripo`（默认）/ **`mixamo`（Mixamo 兼容命名）** / `actorcore` / `unity`（Unity Humanoid）/ `unreal`（Unreal Mannequin）/ `vrm`（VRM 1.0） | 同上 |
| 输出 | `out_format` = `glb`（默认）或 `fbx`；含骨架与蒙皮权重 | 同上、[Tripo Rigging](https://developers.tripo3d.ai/en/models/rig) |
| Retarget | `POST /v3/animations/retarget`，`input` 必须是已绑骨任务的 `task_id`；`animation` 单个或 `animations` 最多 5 个；`animate_in_place`、`export_with_geometry`；输出 GLB/FBX | [Retarget](https://developers.tripo3d.com/en/docs/animations-retarget) |
| 预设动画 | v3.0 biped：`preset:idle/walk/run/dive/climb/jump/slash/shoot/hurt/fall/turn`；四足等各 1 个 walk/march；v1.0 biped 有 90+ 个 `preset:biped:*`；v2.5 biped 旧骨架需重新绑骨才能用新预设 | 同上 |
| 自定义动画 / 上传 BVH/FBX 动作 | 只支持预设 ID；上传自有动作文件重定向**未在官方来源找到** | 同上 |

关于「能否与 Mixamo 风格骨架对接」：官方只承诺「Mixamo-compatible bone naming」以及 convert 的 `fbx_preset=mixamo`。骨骼层级、rest pose（T/A pose）、手指骨数量是否与 Mixamo 完全一致：**未在官方来源找到**，需要用样例模型实测。

### 4.7 贴图、重拓扑、UV 与分割

| 端点 | 关键参数 / 约束 | 来源 |
|---|---|---|
| `models/texture` | `model` 默认 `v3.0-20250812`，可选 `v3.5-20260815` / `v2.5-20250123`；`texture_prompt` 支持 `text`（可附 `style_image`）、单张 `image`、2–4 视图 `images`；`pbr` 默认 `true`；`part_names` 可只给部分部件贴图；`bake`、`compress`、`delight` | [Texture](https://developers.tripo3d.com/en/docs/models-texture) |
| `mesh/decimate` | `model` = `v3.0`（可同任务 `smart_uv`）/ `v2.0`（默认，智能高转低）/ `v1.0`（基础减面）；v3.0 `face_limit` 三角 500–20,000、四边 500–10,000，省略为自适应；`quad`、`bake`（v1.0 不支持）、`part_names`；模型过复杂报 2018 | [Retopology](https://developers.tripo3d.com/en/docs/mesh-decimate)、[Changelog](https://developers.tripo3d.com/en/docs/changelog) |
| `mesh/uv` | 输入 ≤80,000 等效三角形（超出时任务失败并退款）；可选 `bake`；输出带 `uv_render_url` UV 预览 | [Smart UV](https://developers.tripo3d.com/en/docs/mesh-uv) |
| `mesh/segment` | `v1.0-20250506`（几何分割，默认）/ `v2.0-20260430`（Beta，语义标注，可给 `ref_image` 掩码）；输出部件节点名如 `tripo_part_0` | [Segment](https://developers.tripo3d.com/en/docs/mesh-segment) |
| `mesh/complete` | 输入为 segment 任务 ID；`completion_mode` = `ai_completion`（默认）/ `quick_cap` | [Complete](https://developers.tripo3d.com/en/docs/mesh-complete) |
| `mesh/smartsegment` | `seg_type` = `image`（含自动建模）或 `model`（仅 GLB，需 4×4 `transform`）；`granularity`、`hint` | [SmartSegment](https://developers.tripo3d.com/en/docs/mesh-smartsegment) |
| `generation/image-to-splat` | 单图生成 `.splat`，固定 30 credits，典型约 4 分钟 | [Image to Splat](https://developers.tripo3d.com/en/docs/generation-image-to-splat) |

## 5. 文件上传

| 方式 | 官方事实 | 来源 |
|---|---|---|
| 直接上传 | `POST /v3/files`，`multipart/form-data` 字段 `file`，返回 `file_token`（如 `file_abc123`）；图片 JPEG/PNG ≤20 MB，模型 GLB/GLTF/FBX/OBJ/STL ≤150 MB；>60 MB 建议预签名直传 | [File Upload](https://developers.tripo3d.com/en/docs/files) |
| 预签名直传 | `POST /v3/files/presign` 传 `format`（图片 jpeg/jpg/png/webp/bmp/tiff；模型 glb/gltf/fbx/obj/stl/3mf/usdz），返回 `presigned_url`（PUT，30 分钟有效，无需鉴权头）与 `file_token`；返回的 `expires_in` 与签名实际期限不一致，官方说不要依赖 | [File Upload (Large)](https://developers.tripo3d.com/en/docs/files-presign) |
| 直接传 URL | 各任务 `input` 接受「可公网访问的直链」，无需先上传 | [Image to 3D — H](https://developers.tripo3d.com/en/docs/generation-image-to-model/standard) |
| STS | v2 的 STS 上传已移除；v3 Changelog 提到的 `/v3/files/upload-credentials` 在 Java SDK 中只是内部描述符，没有公开客户端流程 | [迁移 Skill](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)、[SDK Integration](https://developers.tripo3d.com/en/docs/sdk) |

格式限制不完全一致：`/files` 图片只列 JPEG/PNG，而生成端点 `input` 列 PNG/JPEG/WebP，presign 还列 BMP/TIFF。`file_token` 的有效期**未在官方来源找到**。带查询串的对象存储签名 URL（如阿里云 OSS 预签名链接）能否作为 `input` 被 Tripo 拉取：**未在官方来源找到**。

## 6. 计费、额度与限流

### 计费模型

- 创建时冻结所需 credit；成功转为扣费；**失败或取消退还，不收费**。[Billing](https://developers.tripo3d.com/en/docs/billing)
- `balance`、`frozen`、`credits_consumed` 是最多两位小数的 JSON number，不能按整数解析（VIP 折扣会产生小数）。[Changelog 2026-08](https://developers.tripo3d.com/en/docs/changelog)
- 2026-12-01 起控制台改为以货币显示（国际 $0.01/credit，大陆 ¥0.07/credit），API 字段仍以 credit 返回，不影响余额与价格。[Billing（国际）](https://developers.tripo3d.ai/en/docs/billing)、[Billing（大陆）](https://developers.tripo3d.com/en/docs/billing)
- 充值：国际站预设 $50 / $100 / $250，最低 $1 任意金额（Stripe）；大陆站支付宝，套餐金额未在 SSR 页面显示。企业定制量、SLA 联系销售。[Pricing（国际）](https://developers.tripo3d.ai/en/pricing)、[Pricing（大陆）](https://developers.tripo3d.com/en/pricing)
- 条款：API credit 预付且购买后不退款。[Terms §6.4](https://developers.tripo3d.ai/en/terms)

### 单价（credit；两站数字相同，2026-11-10 读取）

3D 生成：

| 任务 | H 系列 无贴图 | H 系列 标准贴图 | P1 无 / 标准贴图 | P2 无 / 标准贴图 |
|---|---:|---:|---:|---:|
| Text to 3D | 10 | 20 | 30 / 40 | 100 / 110 |
| Image to 3D | 20 | 30 | 40 / 50 | 100 / 110 |
| Multiview to 3D | 20 | 30 | 40 / 50 | 100 / 110 |

H 系列叠加项：HD 贴图（`detailed`）+10、8K 贴图（`extreme`）+20、HD 几何（`geometry_quality=detailed`）+20、Quad +5、Smart Low-poly +10、Generate Parts +20。P2 贴图叠加项与 P1 相同（detailed / extreme = 120 / 130）。H3.1 模型页给出完整矩阵（例：Image to 3D 无贴图 / 标准 / 精细 / 8K = 20 / 30 / 40 / 50）。官方定价页 H 系列表格不区分 v3.1 / v3.0 / v2.5。

来源：[Pricing（国际）](https://developers.tripo3d.ai/en/pricing)、[Pricing（大陆）](https://developers.tripo3d.com/en/pricing)、[Tripo H3.1](https://developers.tripo3d.ai/en/models/v3-1)、[Tripo P2](https://developers.tripo3d.ai/en/models/p2)、[Changelog 2026-08](https://developers.tripo3d.com/en/docs/changelog)。P 系列数字取自定价页前端数据（页面切换 Tab 才渲染），两站 bundle 数值一致。

后处理与动画：

| 操作 | credit | 操作 | credit |
|---|---:|---|---:|
| Texture fast / standard | 10 / 10 | Rig Check | 0（免费） |
| Texture HD / 8K | 20 / 30 | Auto Rig | 25 |
| Convert 基础 / 高级 | 5 / 10 | Retarget | 10 / 每个动画 |
| Retopology v3.0 三角 / 四边 | 35 / 40 | Segmentation | 40 |
| Retopology v2.0 / v1.0 | 30 / 10 | Part Completion 默认 / Quick Cap | 50 / 30 |
| Smart UV | 20 | Smart Segmentation 图 / 模型 | 85 / 55 |
| Image to Splat | 30 | Image to Multiview / Edit Multiview | 10 / 5 每张 |

Convert 只要 `quad`、`face_limit`、`flatten_bottom`、`flatten_bottom_threshold`、`texture_size`、`texture_format`、`pivot_to_center_bottom`、`scale_factor` 任一为非默认值，就按高级 10 credit 计。[Pricing](https://developers.tripo3d.ai/en/pricing)

### 免费额度

- 大陆站 Codex 插件指南写「新账号会获得免费 credit」；控制台文案有「trial credits，到期日 …」字样。**具体数量与有效期未在官方来源找到。** [Codex Plugin](https://developers.tripo3d.com/en/docs/codex-plugin)
- 条款：免费用户的输入输出权利归 Tripo（见第 7 节），试用期产出不宜用于正式交付。

### 余额与用量接口

`GET /v3/account/balance`（`balance`、`frozen`）、`GET /v3/account/usage`、`GET /v3/account/usage/history`（见第 3 节）。另可在控制台设置余额告警并订阅 `balance.low` webhook。[Account](https://developers.tripo3d.com/en/docs/account)、[Webhooks](https://developers.tripo3d.com/en/docs/webhooks)

### 限流与并发

| 维度 | 官方事实 | 来源 |
|---|---|---|
| 速率限制 | 按 **API key** 计；生成端点较低、查询端点较高；超限 HTTP 429 + `code 1007`；响应头 `X-RateLimit-Limit/Remaining/Reset` | [Rate Limits](https://developers.tripo3d.com/en/docs/rate-limits) |
| 具体速率 | 文档站未给数值；官方迁移 Skill 写「约 200 次生成/分钟、50 次 refine/分钟、10 次上传/分钟」 | [迁移 Skill §8](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md) |
| 并发 | 按**账号**（非 key）、按任务类别独立计；超限 HTTP 429 + `code 2000` + `Retry-After` | [Rate Limits](https://developers.tripo3d.com/en/docs/rate-limits) |
| 默认并发 | H 系列生成 10；P 系列生成 5；图像生成 1；动画（rig/check/retarget）10；模型处理（texture/convert/refine）5；网格操作 10 | 同上 |
| 提额 | 联系支持，可按类别定制 | 同上 |

## 7. 商用许可与数据政策

### 服务条款（两站 `/en/terms` 与 `/zh/terms` 为同一英文文本，签约方 Holymolly Ltd，香港法律，HKIAC 仲裁，最后更新 2025-07-11）

| 条款 | 原意 | 来源 |
|---|---|---|
| §5.2.2 付费用户 | 付费用户总体上拥有其输入与输出的全部权利（使用、修改、分发、商用、许可等），但需授权公司为提供服务而使用、展示输入输出（免版税、永久、不可撤销、全球、非独占）；**公司不会用输入输出训练、验证、测试或改进任何 AI 技术** | [Terms](https://developers.tripo3d.ai/en/terms) |
| §5.2.1 免费用户 | 免费用户的输入输出及相关知识产权，Tripo 保留全部权利 | 同上 |
| §3.2 输出限制 | 可将输出用于合法的商业或非商业用途；禁止用输出训练与其竞争的模型/服务；**未经书面授权，不得把「Generative 3D Foundation Model Service」提供给任何第三方，包括最终用户** | 同上 |
| §2.1 / §3.2 | 不得与他人共享 API key，未经书面批准不得买卖或转让访问凭证 | 同上 |
| §3.2 免责 | 输出可能不唯一，其他用户可能得到相似或相同的输出，不保证排他性 | 同上 |
| §3.1 审核 | 公司可以但无义务审查输入，可删除违规输入、终止服务 | 同上 |
| §4 存储 | 除另有书面约定外，Tripo 无义务存储输入输出 | 同上 |

### 隐私政策（Holymolly Limited，生效日 2026-09-30；读取 Internet Archive 2026-10-16 快照）

- 处理输入以生成输出；授权人员（含工程师）可为排障及评估质量、安全、性能审阅输入输出，官方称「与模型训练分开」。
- 「不使用个人信息训练模型」；但可使用**聚合或匿名化后的信息（包括文本 prompt 与输出）**改进性能、开发并改进模型，「受与你签订的协议限制」。
- 保留期：已保存的输入输出（含会话历史）在对应会话、资产或账号删除时删除；未给出 API 任务产物的固定保留天数。
- 跨境：总部在香港；面向巴西的条款写明数据可能在美国等地存储和处理；EEA/UK 数据出境使用 SCC。

来源：[Privacy Policy（官方 URL）](https://www.tripo3d.ai/privacy)、[Internet Archive 快照](https://web.archive.org/web/20261016030604/https://www.tripo3d.ai/privacy)

隐私政策中「匿名化 prompt 与输出可用于改进模型」与条款 §5.2.2「付费用户输入输出不用于训练」需要合并理解：后者是对付费用户的合同承诺，前者以「受协议限制」为前提。是否存在 API 专用的数据处理协议（DPA）或零保留选项：**未在官方来源找到**。

### 内容审核

- 请求级 `code 2008`：输入违反内容政策被拒；任务级审核拒绝在 v3 表现为 `failed` + `error_code 2008`。[Error Handling](https://developers.tripo3d.com/en/docs/error-handling)、[迁移 Skill §5.1](https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md)
- 生图：seedream 可选 `watermark`；banana 系列总是嵌入不可移除的隐形水印；chat_image 无水印控制。3D 模型是否加水印：**未在官方来源找到**。[Text to Image](https://developers.tripo3d.com/en/docs/generation-text-to-image)
- 大陆站是否以境内主体运营、是否完成生成式 AI 服务备案：**未在官方来源找到**（大陆站条款仍是香港 Holymolly 英文文本）。

## 8. 官方生态

| 组件 | 语言 / 版本 | 最近提交 | 状态与说明 | 来源 |
|---|---|---|---|---|
| tripo-go-sdk | Go 1.21+，v0.5.0，MIT，无第三方依赖（go.mod 仅 module 声明） | 2026-10-23 | 覆盖除 image-to-depth 外的全部 v3 路由，含 SmartSegment、Splat、Import、presign、usage、webhook 验签；内置「可能已受理不重试」逻辑 | [tripo-go-sdk](https://github.com/Tripo-Developer/tripo-go-sdk)、[SDK 路由矩阵](https://developers.tripo3d.com/en/docs/sdk) |
| tripo-python-sdk | PyPI `tripo3d==1.0.0`，MIT | 2026-10-14 | 1.x 全量走 v3；0.x 随 v2 关停失效 | [tripo-python-sdk](https://github.com/Tripo-Developer/tripo-python-sdk) |
| tripo-js-sdk | npm `@vastai/tripo-sdk@0.5.0`，MIT | 2026-10-23 | 官方说明只用于服务端，不要在浏览器放 key | [tripo-js-sdk](https://github.com/Tripo-Developer/tripo-js-sdk) |
| tripo-rust-sdk / tripo-java-sdk | Rust 0.5.0；Java 源码快照 0.1.0-SNAPSHOT | 2026-10-23 / 2026-07-13 | Java 未暴露生图、SmartSegment、Smart UV 等 | [SDK Integration](https://developers.tripo3d.com/en/docs/sdk) |
| tripo-mcp | Python，MIT，alpha | 2025-04-14 | 只对接 Tripo Blender 插件（依赖 blender-mcp 思路），**不是通用 API MCP server**，已一年半未更新 | [tripo-mcp](https://github.com/Tripo-Developer/tripo-mcp) |
| Tripo CLI | `tripo-cli` 0.9.0（Node 20+） | — | 面向 Agent / 终端；支持 `ov` / `cn` 区域与多 profile；国际站默认开启匿名使用统计，可用 `TRIPO_ANALYTICS_ENABLED=0` 关闭 | [Tripo CLI](https://developers.tripo3d.com/en/docs/cli) |
| 引擎插件 | Unity 1.0.1、Unreal 1.0（5.6）、Blender 0.7.3（3.0+）、Godot、Cocos、ComfyUI 1.3.0 | Blender 仓库 2025-11-06 | 下载包托管在官方 CDN | [Plugins](https://developers.tripo3d.com/en/docs/plugins)、[tripo-3d-for-blender](https://github.com/Tripo-Developer/tripo-3d-for-blender) |
| 开源研究模型 | TripoSR、TripoSG、TripoSF、TripoSplat（VAST-AI-Research，MIT） | 2026-08 | 与托管 API 模型不是同一产品，不能用来推断 API 质量 | [VAST-AI-Research](https://github.com/VAST-AI-Research) |

仓库最近提交时间取自 GitHub 公共 API（未鉴权，2026-11-10）。官方 OpenAPI 机器契约文件：**未在官方来源找到**；文档站提供 `/llms.txt`、`/llms-full.txt` 与逐页 `.md`。

## 9. 耗时与版本选择

| 能力 | 官方耗时 | 来源 |
|---|---|---|
| H3.1 生成（无贴图 / 带贴图） | 约 40 秒 / 约 120 秒 | [Tripo H3.1](https://developers.tripo3d.ai/en/models/v3-1) |
| Auto Rig | 约 30 秒 | [Tripo Rigging](https://developers.tripo3d.ai/en/models/rig) |
| Retarget | 约 5 秒 | [Tripo Animation](https://developers.tripo3d.ai/en/models/animation) |
| Image to Splat | 约 4 分钟 | [Image to Splat](https://developers.tripo3d.com/en/docs/generation-image-to-splat) |
| P1 / P2、贴图、重拓扑、分割等 | **未在官方来源找到** | — |

官方 SDK 示例把整次等待上限设为 20 分钟，而 Task Lifecycle 页示例为 5 分钟；两者都只是示例值。[SDK Integration](https://developers.tripo3d.com/en/docs/sdk)、[Task Lifecycle](https://developers.tripo3d.com/en/docs/task-lifecycle)

官方版本建议：新项目 3D 用 `v3.1-20260211`；最高质量用 v3.1 + `geometry_quality=detailed`；低模/游戏资产用 P 系列（P2 原生四边面、面数上限更高，或 P1）；生产环境固定版本号。H3.1 定位「最复杂的生成工作，fidelity 比速度重要」；P2 定位「更易绑骨、动画、编辑的网格」。[Models & Versions](https://developers.tripo3d.com/en/docs/models-and-versions)、[Tripo P2](https://developers.tripo3d.ai/en/models/p2)

贴图 v3.5 的 `delight` 建议：做写实游戏资产保持开启，让引擎光照下 base color 干净；做渲染、VFX、CG 时关闭以保留风格化光影。[Tripo Texture](https://developers.tripo3d.ai/en/models/texture)

不同版本质量差异的量化评测（基准、样张对比）：**未在官方来源找到**。

## 10. 对接入的启示（建议，非官方事实）

1. **地区选择**：服务器在阿里云大陆、摄影师在大陆，倾向大陆站；但凭证只能对应一个地区，配置中把 Base URL 与 key 成对注入，不让模型或客户端选择地区。
2. **计费不确定性**：创建请求超时或 5xx 时记为「结果未知」，不自动重发；用 `tasks/list`、`usage/history` 与 webhook 事件补齐状态。
3. **产物落地**：`model_url` / `rendered_image_url` 是临时签名链接，任务成功后立即下载转存，同时记录 `task_id`、`model` 版本、`credits_consumed`。
4. **参数收窄**：给 Agent 暴露的参数宜收窄到少数几个（输入、系列/质量档、是否贴图、面数档），避免 `export_orientation` 这类官方都提示「可能方向错误但仍报成功」的参数。
5. **three.js 兼容**：默认 GLB + PBR 契合 three.js；`quad=true` 会改为 FBX，若编辑器只吃 GLB，需要禁用 quad，或在服务端再走一次 convert。
6. **合规前置**：§3.2「不得向第三方/最终用户提供服务」与免费用户权利归 Tripo，两点都要在上线前取得书面确认，并在隐私说明中披露把用户图片发给 Tripo 处理。

## 11. 来源版本与资料冲突

| 来源 | 读取方式与快照 |
|---|---|
| 大陆站全文语料 | `https://developers.tripo3d.com/llms-full.txt`，2026-11-10 读取，约 8,150 行，覆盖全部文档页 |
| 国际站单页 | `https://developers.tripo3d.ai/en/docs/{introduction,billing,rate-limits,sdk,changelog,webhooks}.md`，与大陆站逐页 diff，仅 Base URL / 币种 / region 不同；国际站 `llms-full.txt` 返回 404 |
| 定价 | 两站 `/en/pricing` SSR 文本 + 前端 bundle（国际 `PricingPage-8pbbctNd.js`，大陆 `index-EGQt3NlD.js`）中的价格数组 |
| 模型页 | `developers.tripo3d.ai/en/models/{v3-1,p2,rig,animation,texture}` |
| 迁移 Skill | `https://developers.tripo3d.com/assets/developer/tripo-v2-to-v3-migration/SKILL.md`（官方文档站托管的 AI 迁移指引；部分数字如 URL 约 24 小时、速率约 200/分钟只出现在这里） |
| 条款 / 隐私 | `developers.tripo3d.ai/en/terms`（2025-07-11 版）；隐私政策经 Internet Archive `20261016030604` 快照 |
| GitHub | `Tripo-Developer/tripo-go-sdk` 提交 `7a040a3f198b`（tag v0.5.0 = `c8df05f7917e`）；其余仓库只读元数据 |

已知冲突：

- 结果 URL 期限：v3 文档只说「临时」，迁移 Skill 说约 24 小时，旧 v2 文档说 5 分钟或 60 秒。
- 示例中的 `credits_consumed` 多为 100.00，与定价页不符，是示意值。
- 上传格式：`/v3/files` 图片只列 JPEG/PNG，生成端点与 presign 列出更多格式。
- refine / stylize：SDK 页写「已退役，不列为支持」，Changelog 与迁移 Skill 附录 A 仍列 `/v3/models/refine`、`/v3/models/stylize`（stylize 有 lego/voxel 等 7 种风格），迁移 Skill 也说 refine 在 v2 已实际不可用。v3 文档站没有这两个端点的页面，本文按「不可用」处理。
- 错误码 2018：请求级表示「模型过复杂无法重拓扑」，迁移 Skill 又用作「排队超时」的任务级 `error_code`；官方明确两套编号可能含义不同。
- 第三方博客（apidog 等）仍描述 v2 `api.tripo3d.ai/v2/openapi/task`，已过时，未采用。

## 待 owner 确认 / 未核实事项

1. **SaaS 转售许可**：条款 §3.2 禁止未经书面授权把生成式 3D 基础模型服务提供给第三方（含最终用户）。本项目让摄影师通过 Agent 调用，是否属于该情形，需向 Tripo（business@tripo3d.com / business@tripo3d.ai）取得书面确认或企业协议。
2. **账号地区**：用大陆站还是国际站？大陆 ECS 访问国际站 `openapi.tripo3d.ai` 及其 CloudFront 产物域名的连通性与延迟未实测；大陆站是否有境内运营主体、是否完成生成式 AI 备案未在官方来源找到。
3. **结果 URL 与 `file_token` 有效期**：v3 文档未给确切值；需实测。
4. **取消任务**：v3 没有公开取消端点；Agent 停止或用户取消时只能停止轮询，已提交的任务仍会运行并扣费（成功时）。是否能接受？
5. **幂等**：没有客户端幂等键；网络失败后的「未知」状态如何呈现给摄影师、是否允许手动重试，需要产品决定。
6. **免费额度数量与有效期**：未找到；免费用户产出版权归 Tripo，开发测试期产出不应进入正式交付。
7. **Mixamo 骨架对接程度**：只承诺命名兼容；层级、rest pose、手指骨需用样例实测。本项目 3D 编辑器若有自己的人形骨架（见场景姿态相关 Epic），需对照验证。
8. **P2 `quad=true` 的输出格式**：H 系列明确强制 FBX，P2 未写明。
9. **OSS 签名 URL 作为 `input`**：能否直接传阿里云 OSS 预签名链接，还是必须先上传换 `file_token`，未找到说明。
10. **数据处理**：是否有 API 专用 DPA、零保留或指定区域存储选项；隐私政策允许使用匿名化 prompt 与输出改进模型，与条款「付费用户不用于训练」的关系需书面澄清。
11. **具体速率限制数值**：文档站未公布，仅迁移 Skill 给出约数；并发默认值（H 10 / P 5 / 图像 1）是否满足多租户峰值，需评估是否申请提额。
12. **质量与耗时**：P 系列、贴图、重拓扑耗时及不同版本质量差异无官方量化数据，需要样张实测；`texture_quality` 与 `geometry_quality` 组合下的实际扣费，也应以真实任务的 `credits_consumed` 核对。
13. **Webhook 重试策略**：重试次数与间隔未公开；webhook 是账号级（控制台配置），多环境（开发/生产）共用一个账号时如何区分事件来源需设计。

## 本项目接入设计

本节是对照 2026-11-10 `develop`（`d601db99`）代码写的接入方向，**不是已批准设计**。正式落地应走 `lti-new` 起草 proposal，契约先改 `api/openapi.yaml`。

### D1. 定位：补上「AI 生成」这条建模路径

- 编辑器已定「AI 原生，不做 DCC 网格编辑」：网格内部交给助手写代码（`write_model`）、外部导入（`POST /creative/assets/from-glb`）、AI 生成三条路径。前两条已有，Tripo 补的是第三条。
- 产出物是**资产库的 `model` 资产**（`creativelibrary/list.go` 的 `assetKinds` 已含 `model`），不是画布节点：`creativecanvas/graph_commands.go:kindMismatch` 规定模型只能放进 3D 场景。生成完成后由助手用 `edit_scene` 的 `add_objects` 放入场景，或由摄影师从资产库拖入。
- 与 cosplay 全流程的直接契合点：PRD S2「人设是 AI 三视图或导入的角色模型」。AI 三视图 → `multiview-to-model` → 角色模型，后续接姿态系统，是 Tripo 最有价值的用法。

### D2. 一个领域入口，两层外壳

沿用 `generate_node` 的做法：「Generator is the node generation entry the assistant shares with the canvas」（`creativeagent/canvas_tools.go`）。Agent 工具与前端 HTTP API 调同一个领域入口，准入、外发、计费、版本保存只有一条路径。

```
前端 HTTP（Gin handler，薄）──┐
                              ├─> 领域入口：模型生成（受理 / 查询）
Agent 工具 generate_model ────┘        │
                                       ▼
                    llmgateway 生成路由（新种类 model，Tripo 适配器）
                                       │ 提交 / 轮询 / 受理未知核实 / 取件
                                       ▼
                       原始产物转存对象存储（服务端，不依赖页面）
                                       │
                                       ▼
          入库转换（复用浏览器导入管线）──> 资产库 model 资产
```

### D3. 供应商层：放进 llmgateway，新增 `model` 生成种类

- `llmgateway/generation_contract.go` 目前只有 `text`、`image`、`video`、`audio` 四种。新增 `GenerationModel`，输出策略限定 `model/gltf-binary`，另带一张预览图。
- Tripo 的任务模型（异步提交、无幂等键、超时可能已受理、结果链接临时）与 RunningHub 一致。`generation_runninghub.go` 的「受理未知不重发、按候选路由、取件阶段独立重试」可以直接复用，这也是放进 gateway、而不是在业务包里另写一个 HTTP 客户端的主要理由。
- **不直接依赖官方 Go SDK**，按 `generation_runninghub.go` / `generation_krill.go` 的模式写薄适配器。理由：gateway 要自己掌握重试判定、观察证据和恢复，SDK 还在 v0.x。Webhook 验签和「可能已受理」的状态码分类可以参照 SDK 的 `webhook.go`、`http.go` 实现。
- 首期**只轮询，不接 Webhook**。Webhook 是账号级控制台配置，开发和生产共用账号时无法区分事件来源；轮询 1–2 秒一次，配合 `tasks/list` 批量查询足够。并发量上来后再加 Webhook 作为加速信号，仍以轮询为准。
- 凭证：`TRIPO_API_KEY` 与 `TRIPO_BASE_URL` 成对经环境变量注入（地区由 key 决定，写错返回 401），缺失时只关闭这个供应商，与 `RUNNINGHUB_API_KEY` 的约定相同。结果取件域名按 §2 表格固定白名单（大陆站 `openapi.cdn.tripo3d.com`），不跟随重定向。

### D4. 产物落地：两段式，第二段复用浏览器导入管线

关键约束：外部 GLB 入库是**在浏览器里转换的**。`frontend/src/creative-scene/import/parse.ts` + `convert.ts` 负责拆出贴图、把 glTF 材质换算成材质槽位、解 Draco/meshopt；服务端 `ImportModelToLibrary` 只收转换后的静态 GLB、缩略图和贴图，再用 `creativeglb` 按静态子集校验。

| 段 | 在哪执行 | 做什么 | 失败语义 |
|---|---|---|---|
| ① 生成与转存 | creative-worker（服务端） | Tripo 任务成功后立即下载 `model_url`、`rendered_image_url`，存进对象存储，记下 `task_id`、模型版本、`credits_consumed` | 钱已花在这一段；产物必须落盘，不能依赖页面开着 |
| ② 入库转换 | 打开着的创意空间页面 | 取回转存的原始 GLB，走现有 `parse → convert → from-glb` 链路生成 `model` 资产 | 不再花钱，可无限次重试 |

②有两种触发方式，建议都做：

- 前端发起时，结果页直接在当前页面转换，与手动导入同一套 UI。
- Agent 发起时，新增场景客户端任务种类 `import_generated_model`（与 `bake_model` 并列，见 `creativescene/jobs.go`），由打开着的页面认领。这符合 2026-10-15「3D 渲染在前端，页面关闭时助手不能渲染」的既有决定。

**不建议把转换移植到 Go。** 那会造出第二份材质换算算法（含 Draco 解码），两边结果不一致的风险远大于收益。

### D5. 向 Tripo 发的参数要锁死，保证 GLB 能过导入

| 参数 | 固定值 | 原因 |
|---|---|---|
| `quad` | 不开 | H 系列开 `quad` 会强制输出 FBX，convert 也不出 GLB，编辑器只吃 GLB |
| `pbr` / `texture` | `true` | 产出 base_color / metallic / roughness / normal，对应 Principled 材质槽位 |
| `auto_size` | `true` | 场景以米为单位（`creativescene/model.go`），不开时尺度没有保证 |
| `export_orientation` | 不设 | 官方提醒后续处理时设置可能方向错误却仍报成功；朝向在编辑器里摆 |
| `delight` | `true`（默认） | 场景自己打光，base color 不能烘焙参考图的光影 |
| `compress` | 不设或 `geometry` | meshopt 导入已支持（`parse.ts`），首期不开，减少变量 |
| `generate_parts` | 不开 | 要求无贴图，与 PBR 互斥 |

`parse.ts` 拒绝 KTX2 贴图和带骨骼蒙皮的模型。Tripo 默认贴图格式没有写明，需要 spike 确认（见 D9）。

对 Agent 和摄影师只暴露**档位**，不暴露 Tripo 原始参数（同 RunningHub「摄影师和 Agent 只提交 `model_key`」的做法）：

| 档位 | Tripo 映射 | 单价（credit，图生 3D） | 适用 |
|---|---|---:|---|
| 草稿 | H3.1、`texture=false` | 20 | 摆布景、试形体，白模够用 |
| 标准 | H3.1、标准贴图 | 30 | 默认 |
| 精细 | H3.1、`geometry_quality=detailed` + HD 贴图 | 60 | 主体道具、特写 |
| 低模 | P1 | 50 | 大量布景件，控制 draw call（视口卡顿的瓶颈在 draw call，不在面数） |

P2（110 credit）首期不开，等样张证明它比 P1 值这个价。

### D6. Agent 工具：`generate_model@1`

- 在 `creativeagent/tool_registry.go` 登记：`EffectClass: "generate"`、`MaxImpact: 1`、`deployed` 看 Tripo 供应商是否就绪；资源键占资产库写（`keyLibrary` 写）。
- **命名冲突**：现有 `write_model` 的显示名就叫「生成模型」（`tool_registry.go:81`）。新工具显示名建议「AI 建模」，并把 `write_model` 改为「代码建模」，否则助手的活动记录里两者分不清。
- 参数：`mode`（`text` / `image` / `multiview`）、`prompt`、`references`（画布或资产库里的图片内容修订 ID，多视图带 `front` / `left` / `back` / `right` 角色）、`tier`（D5 的档位）、可选 `scene_id`（入库后直接放进该场景）。
- 等待语义照抄 `generate_node`：同步等待上限（`generate_node` 是 150 秒；带贴图约 120 秒，建议 3D 放宽到 240 秒），超时返回 `running`，结果稍后出现在资产库，工具描述里写明「不要重复调用」。
- 描述里写明会消耗额度，只在创作者明确要建模时调用，与 `generate_node` 的措辞对齐。

### D7. 前端 HTTP API

契约先行：先改 `api/openapi.yaml`，再跑 `make generate`。建议的最小面：

| 接口 | 作用 |
|---|---|
| `POST /creative/model-generations` | 受理：`mode`、`prompt`、`references`、`tier`；带幂等键（防双击）；返回生成 ID |
| `GET /creative/model-generations/{id}` | 查询：状态、进度、预览图、原始产物、入库后的资产 ID |
| `POST /creative/model-generations/{id}/import` | 第二段入库：提交浏览器转换好的 GLB、缩略图、贴图（复用 `ImportModelInput` 结构） |

进度推送复用现有画布通知（账号级 realtime），不另开 WebSocket。所有表带 `account_id`，过滤在 repository 基座里做（ADR-001）；handler 不碰 Tripo 细节（ADR-003）。

### D8. 二期：自动绑骨接姿态系统

- 姿态系统 Epic 把「编辑器内的自动绑定和权重绘制」列为下一期，原因是「内置素体和导入资产都在外部绑好」。Tripo 的 `animations/rig`（v3.0，`spec=mixamo` 或 `vrm`）能直接补上这个空位，而 Epic 已规定导入时识别 VRM 和 Mixamo 的骨骼命名。
- 前提是子项 `scene-skinned-humanoid` 先落地：现在 `parse.ts` 会把带蒙皮的模型以 `skinned` 拒掉，`creativeglb` 也拒 `skins`。
- 预设动画（`retarget`）不接：Epic 的非目标包含动画播放，分镜只需要静态姿态。

### D9. 落地前的 spike（建议 1–2 天，花费约 1,000 credit，大陆站约 ¥70）

用大陆站账号跑一组样张，回答下表问题。每一项都会直接改写 D4、D5：

| 问题 | 判定 |
|---|---|
| H3.1 / P1 产出的 GLB 能否原样通过 `parse.ts` + `convert.ts` + `creativeglb` | 全部通过才能沿用浏览器导入管线；否则要先加转换规则 |
| 贴图格式（JPEG / PNG / WebP / KTX2）、贴图尺寸、文件大小 | KTX2 要么请求时规避，要么导入支持 |
| `auto_size=true` 的尺度是否合理（人物约 1.7 米） | 不合理就在入库时归一化 |
| 默认朝向（`+x` 前向）与编辑器约定的差异 | 决定入库时是否固定旋转 |
| 大陆 ECS 到 `openapi.tripo3d.com` 与产物 CDN 的连通性和耗时 | 决定等待上限与超时 |
| 实际 `credits_consumed` 是否与定价页一致 | 校准档位定价 |
| rig v3.0（`spec=mixamo`）的层级、rest pose、手指骨 | 二期 D8 的可行性 |

### D10. 阻断项与需要 owner 拍板的事

1. **许可（阻断）**：条款原文「make the Generative 3D Foundation Model Service available to any third party, including end users, without the prior written authorization and consent of Holymolly」（2026-11-10 复核 `developers.tripo3d.ai/en/terms`）。产品定位是多租户 SaaS，摄影师就是 end users。**上线前必须拿到 Tripo 的书面授权或企业协议**；spike 和内部开发不受影响。
2. **地区**：建议大陆站（ECS 在大陆、支付宝结算、产物走国内 CDN）。需要确认大陆站的运营主体和生成式 AI 备案情况，这关系到对摄影师的合规披露。
3. **计费**：Tripo 失败不扣、成功才扣，与「生成成功不退」的积分方向一致。首期是否像 RunningHub 那样以 `billing=disabled` 先上（只留调用证据，不进积分账），还是等积分账落地再开？
4. **停止语义**：Tripo 没有取消接口。「Agent 停止即停」刚批准，但对 Tripo 只能停止等待，已提交的任务仍会跑完并扣费。建议**结果照常入库**，钱已经花了，不应丢弃；同时在 UI 上如实说明。
5. **外发**：参考图会发给 Tripo。现有约定是不做逐次授权（`creativegeneration/service.go`，owner 2026-10-09），隐私说明里要加上 Tripo（香港 Holymolly，可能在境外处理）。
