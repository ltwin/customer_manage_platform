---
doc_type: feature-design
feature: 2026-10-09-model-routing
requirement: FND-06 扩展（统一 LLM Gateway；无 V6 清单条目，影响 V6-029）
status: approved
date: 2026-10-09
---

# 模型与供应商解耦：按优先级路由与安全故障切换

## 0. 结论

- **摄影师只看到模型，看不到供应商**。目录拆成两层：「模型」是对外的身份（`model_key`、显示名、能力），「部署」是某家供应商对这个模型的一次具体接入（地址、凭证、请求时用的模型名、价格、限流）。一个模型挂一个或多个部署，按列表顺序就是优先级。
- **故障容错优先靠「选择时绕开」，而不是「失败后重发」**。每次派发前按优先级挑第一个可用的部署：没配凭证的、熔断中的都跳过。只有供应商**证明这次没有受理、没有扣费**时，才在同一个请求里换下一个部署重发。超时、5xx、流式中途断开这类结果不确定的情况一律不重发：重发会让一次调用付两份钱，这是网关最贵的错误。
- **可控性分三步交付**：本期用目录里的顺序和凭证开关控制；接入第二家供应商时加熔断和请求内切换；有管理后台后，把优先级和启用开关搬进数据库，后台改完立即生效。三步共用同一个选择函数，后台只是替换它读取顺序的来源。
- **外发授权改为平台级**。授权不再按供应商记录，改为「发送给平台签约的模型服务商」，服务商名单放在隐私说明里。每次调用实际发给了哪家供应商，仍然在调用记录里逐条留存，审计不受影响。这一条要改 PRD AS-64 和 V6-029 的验收标准，见 §6 D1。

本期不做：跨供应商的负载分摊（按比例分流）、本地限流满时溢出到备选部署、按价格自动挑最便宜的部署、图片生成链路的多供应商（它有自己的 `generation_policy`，等 V6-023 定了供应商再说）。

## 1. 现状

| 关注点 | 现在的做法 | 代码位置 |
|---|---|---|
| 对外身份 | 每条 `ModelConfig` 既是模型又是部署；`model_key` 一对一绑定一个部署 | `llmgateway/catalog.go` |
| 名字映射 | `request_model_id` 是发给供应商的名字，`accepted_model_ids` 是回包允许出现的名字。单个部署内部可以映射，一个模型却挂不了多个部署 | 同上 |
| 供应商暴露 | 目录 API 返回 `vendor_key`、`provider` 和 `vendors` 列表；前端发授权时带 `vendor_key`；`coversRun` 发现供应商变了就要求重新授权 | `openapi.yaml` 的 `CreativeAgentModel` 与授权相关 schema、`AgentPanel.tsx:416`、`creativeagent/runs.go` 的 `coversRun` |
| 重试 | 每个请求最多尝试 2 次；只有 `OutcomeUnaccepted`（目录登记过的「处理前拒绝」状态码）才重试，而且仍然发给同一个部署 | `dispatch.go` 的 `maxAttempts` 和 `finishFailure` |
| 部署冻结在哪 | 部署快照 `model_snapshot`、价格快照 `price_snapshot` 冻结在 `llm_requests` 上，请求哈希里包含 `DeploymentKey` 和 `PriceVersion`；`llm_attempts` 只记录 `limit_key` | `dispatch.go` 的 `PrepareInTx` 和 `BeginDispatchInTx`、`contract.go` 的 `RequestHash` |
| 预留 | 按该部署的价格上界预留，结算按派发时的价格 | `admission.go`、`accounting.go` |
| 配置 | 内置目录写在 Go 代码里；`CREATIVE_LLM_CATALOG` 用一个 JSON 文件整份替换内置目录 | `catalog_default.go`、`assemble.go` 的 `OpenCatalog` |

## 2. 目录模型

### 2.1 两层结构

```jsonc
{
  "version": "2026-10-xx",
  "currency": "USD",
  "models": [
    {
      "model_key": "gpt-6-astra",          // 对外身份，运行记录里存的就是它
      "display_name": "GPT-6 Astra",
      "capability": { "image_input": true, "tool_calling": true, ... },
      "deployments": ["aicodewith/gpt-6-astra", "openai/gpt-6-astra"]  // 顺序即优先级
    }
  ],
  "deployments": [
    {
      "deployment_key": "aicodewith/gpt-6-astra",
      "vendor_key": "aicodewith",
      "provider": "openai_responses",
      "base_url": "https://api.aicodewith.ai/v1",
      "credential_env": "AICODEWITH_API_KEY",
      "request_model_id": "gpt-6-astra",           // 各家叫法不同就写在这里，例如 gpt-6-astra-0907
      "accepted_model_ids": ["gpt-6-astra"],
      "reasoning_effort": "medium",
      "capability": { ... },                        // 这个部署实际支持的能力
      "price": { ... },
      "limit_key": "aicodewith/api", "concurrency": 4, "rate_limit_per_minute": 60,
      "unaccepted_statuses": []
    }
  ]
}
```

- **能力由模型声明，部署必须全部覆盖**。加载目录时检查：模型声明的每一项能力，挂在它下面的每个部署都要支持；上下文长度和最大输出取所有部署的最小值。这样无论切到哪个部署，都不会出现「请求按能看图准入、结果发给了不能看图的部署」。DeepSeek V4 Pro 发图不报错却会编造画面描述，这类部署根本不能挂到声明了看图的模型下面。
- **思考模式属于部署，但它改变模型行为，所以不同思考模式算不同的模型**。`deepseek-flash` 和 `deepseek-flash-thinking` 仍然是两个模型，各自挂各自的部署。
- **同一个显示名意味着同一个模型**，这由维护目录的人负责。各家实际的版本后缀写在部署的 `request_model_id` 和 `accepted_model_ids` 里。
- **兼容现有数据**：现有的 `model_key`（`deepseek-flash` 等）原样保留为模型的 key，各挂一个部署，`deployment_key` 沿用现值。`creative_agent_runs.model_key` 和 `llm_requests.model_key` 的语义变成「模型」。现在每个模型只有一个部署，两种语义取值相同，所以不需要回填数据。

### 2.2 配置方式

- 内置目录继续写在 Go 代码里，改价格或能力仍然要升级目录版本号（每次请求都会持久化它）。
- `CREATIVE_LLM_CATALOG` 改用新格式。旧格式不兼容：仓库里没有提交覆盖文件，部署环境如果自己写过，需要按新格式重写（D5）。
- 优先级的选择集中在 `Catalog.route` 一处：本期直接按目录顺序取第一个已启用的部署，第三步在这里接入数据库来源（§4.3）。实现时没有提前抽出 `RouteSource` 接口——只有一种来源时它只是一层转发，等第三步有第二种来源再抽。

## 3. 选择与切换

### 3.1 什么时候选

每次尝试派发时选一次（`BeginDispatchInTx`），不是在建请求时选：

1. 取这个模型的部署顺序（本期是目录顺序，第三步换成数据库来源）。
2. 依次过滤：凭证没配置的、被管理员停用的、熔断中的（第二步才有），以及本请求前一次尝试已经失败过的部署。
3. 取剩下的第一个。如果全部被熔断，退回优先级最高的那个，让熔断器借这次请求探测它是否恢复，而不是直接拒绝。
4. 在选中部署的 `limit_key` 上申请本地并发和限流。申请不到时和现在一样返回 `ErrRateLimited`，不溢出到下一个部署（本期不做，见 §0）。

### 3.2 请求内什么时候切换

前提：`maxAttempts = 2` 不变，所以最多换一次。

| 结果 | 现在 | 改成 |
|---|---|---|
| `OutcomeUnaccepted`（目录登记过的处理前拒绝，例如 DeepSeek 的 429） | 重试同一个部署 | 优先换下一个部署；没有下一个就重试同一个 |
| 凭证被拒（401/403） | `OutcomeRejected`，归入 `invalid_request`，失败 | 新增类别 `credential_refused`，可以切换 |
| 余额不足（402，或供应商文档写明的余额错误） | `OutcomeRejected` / `insufficient_balance`，失败 | 可以切换 |
| 其他 400/422（请求本身的问题） | 失败 | 不变。换一家多半同样会拒绝，而且可能掩盖我们自己的 bug |
| 超时、5xx、断流、协议违规（`unknown` / `protocol`） | 失败，保留预留额度，等待核实 | 不变，不切换。可能已经扣费 |

401、402 这类拒绝能切换，前提是**供应商在处理之前就拒绝了**，钱没有花出去。哪些状态码属于这一类，以各家文档或实测为准，按部署登记，和现在的 `unaccepted_statuses` 一样写在目录里。没有证据的状态码不登记。

一次运行有多轮模型调用，每一轮都会重新选择。所以某个部署在某一轮返回不确定结果之后，这一轮失败，下一轮会被熔断器绕开。这就是「不确定时不切换」的代价：最多损失一轮，不会重复扣费。

### 3.3 熔断（第二步）

- 状态放在进程内存里，按 `deployment_key` 记录。只影响「先选谁」，各进程状态不一致也不会出错，所以不需要落库。
- 5 分钟内连续 3 次「可用性失败」就打开熔断，60 秒后半开，放一个请求过去试探。可用性失败包括：处理前拒绝、凭证被拒、余额不足、超时、5xx、断流。不确定的结果虽然不能用来切换，但足以说明这个部署出了问题。
- 请求本身的 400 和 422 不计入。

### 3.4 请求身份与快照（第二步，需要迁移）

请求内切换意味着同一个请求的两次尝试可能发往不同的部署，所以部署相关的冻结信息要从请求下放到尝试：

- `llm_requests.model_snapshot` 改为冻结模型这一层：`model_key`、目录版本、候选部署顺序、能力上界。请求哈希去掉 `DeploymentKey` 和 `PriceVersion`，改为对模型快照做哈希。
- `llm_attempts` 新增 `deployment_snapshot` 和 `price_snapshot`（jsonb，可空）。派发许可 `Permit` 改从尝试行构造；结算、证据记录、回包模型名校验都按尝试的部署进行。旧行这两列为空，回退读请求上的快照，不需要回填。
- **预留按所有候选部署的最高价预留**，各维度分别取最大值，结算按实际派发的部署。所以切到更贵的部署不会超出预留（D3）。
- 迁移要同时补 store 回滚链测试。

第一步完全不改这些：派发时仍然只用优先级最高的可用部署，部署冻结在请求上，不需要迁移。

### 3.5 推理只回给同一个部署

GPT 的加密推理绑定的是上游组织，不只是模型。同一个 `gpt-6-astra`，AiCodeWith 返回的加密推理交给 OpenAI 直连会被 400 拒绝，按 §3.2 不能切换，整次运行就会失败。所以：

- Responses 适配器的推理信封在现有的 `model` 之外，加记 `deployment_key`。只有下一轮发往同一个部署时才带回；换了部署就丢掉推理，代价是这一轮的思考连续性和缓存命中。
- DeepSeek 的明文推理不受影响：任何 DeepSeek 兼容部署都能读，原样带回。
- 这一项不依赖本设计的其他部分，可以随 GPT-6 接入的那次提交一起做。

AiCodeWith 内部会在多个上游渠道之间自动切换，渠道换了加密推理也可能解不开。这一点要靠真实接口测试确认（`TestLiveAICodeWithReasoningToolLoop`）。如果确实解不开，AiCodeWith 的部署就要关掉推理回传，结论回写到本节。

## 4. 对外与授权

### 4.1 API 契约

- `CreativeAgentModel` 删除 `vendor_key` 和 `provider`；目录响应删除 `vendors`。摄影师只看到 `model_key`、显示名和能力。
- 授权请求删除 `vendor_key`，由服务端固定写入 `platform`（满足现有列约束 `^[a-z][a-z0-9_-]{0,63}$`，不需要迁移）。授权响应保留这个字段，值恒为 `platform`；也可以一起删掉（D4）。
- 改完运行 `make generate`，提交 Go 和 TS 两份生成物；前端 `AgentPanel` 不再传 `vendor_key`。

### 4.2 授权语义

- `coversRun` 去掉供应商比对；`policyVersion` 升级，旧的按供应商授权自然失效。前端发送时本来就会自动创建授权，摄影师感觉不到变化。
- 授权的含义变成：这个会话的内容可以发给平台签约的模型服务商。服务商名单由 `Catalog.Vendors()` 生成，供隐私说明页使用。它不再出现在模型选择里，也不在每次发送时展示。
- 审计：每一次尝试实际发往哪个部署和供应商，都记录在 `llm_attempts` 上（第一步通过请求快照，第二步通过尝试快照），可以按会话追查。

### 4.3 管理后台（第三步，只定接口）

- 新表 `platform_llm_routes(model_key, deployment_key, priority, enabled, updated_by, updated_at)`。这是平台级数据，不带 `account_id`，和 `platform_llm_limits` 同类。
- 数据库来源：表里有记录就用表里的顺序和开关，没有记录就回退到目录里的顺序。只能在目录已声明的部署之间排序或停用，不能凭空新增部署，因为部署涉及价格和能力，必须走目录版本。
- 后台改完，下一次派发就生效；已经在飞的尝试不受影响。

## 5. 分步与验收

| 步 | 内容 | 迁移 | 验收 |
|---|---|---|---|
| 0 | GPT-6 接入（Responses 适配器，代码已写完未提交）加 §3.5 推理绑定部署 | 无 | 真实接口测试三项通过；离线测试覆盖换部署时丢弃推理 |
| 1 | 两层目录、`Catalog.route`（读目录顺序）、按优先级跳过没配凭证的部署、契约去掉供应商、平台级授权 | 无 | 目录加载拒绝能力不覆盖的部署；主部署没配凭证时派发落到备选部署；目录 API 和前端都看不到供应商；旧授权失效后发送仍然正常；PRD 与 V6-029 同步修改 |
| 2 | 熔断、请求内切换、尝试级快照、按最高价预留 | `llm_attempts` 加两列 | 处理前拒绝和凭证被拒会换部署；不确定结果不换且不重发；结算按实际部署；熔断打开后绕开、半开后恢复；回滚链测试 |
| 3 | `platform_llm_routes` 和后台操作 | 新表 | 改顺序或停用后，下一次派发生效 |

第一步建议在第 0 步的真实接口测试之后再做：如果 AiCodeWith 的推理无法跨轮带回，适配器还要改，那时再动目录结构，返工最少。第二步等第二家供应商真正接入时再做，否则切换逻辑没有真实对象可以验证。

## 6. 已拍板（owner 2026-10-09，均按推荐）

| # | 问题 | 推荐 |
|---|---|---|
| D1 | 授权改为平台级，修改 PRD AS-64（「首次弹窗说明供应商」改为「说明内容会发送给平台签约的模型服务商，名单见隐私说明」），并修改 V6-029 的 AC-01 和 AC-03（去掉「供应商变化重新询问」）；结论回写 PRD §11.3 | 同意 |
| D2 | 请求内切换只在「处理前拒绝、凭证被拒、余额不足」时发生，不确定的结果不切换 | 同意 |
| D3 | 允许自动切到比主选更贵的备选部署（预留已按最高价计算）；不允许的话，只能在价格不高于主选的部署之间切换 | 允许，部署由人工审核后才进入目录，价格差是预期内的 |
| D4 | 授权响应里的 `vendor_key` 是保留（恒为 `platform`）还是删除 | 删除，少一个没有意义的字段 |
| D5 | `CREATIVE_LLM_CATALOG` 的旧格式直接作废，不做兼容 | 作废；先确认生产环境没有在用 |
