# 统一 LLM Gateway 开发说明（FND-06）

实现 Epic `creative-workspace-system` 的 FND-06 / 切片 GH-01 与 GH-04 的 Gateway 部分。契约权威源是
[modules/gateway.md](../product/creative-canvas-system/modules/gateway.md)、
[gateway-harness-data.md](../product/creative-canvas-system/modules/gateway-harness-data.md)
与 [eino-adoption.md](../product/creative-canvas-system/eino-adoption.md)；本文只写实现事实与运维接口。

## 边界

- 归属 `backend/internal/platform/llmgateway`，**只从服务端使用**：本项不新增任何 HTTP 路由，客户端可见入口属 GH-02/FND-07。
- Gateway 不 import `creativecanvas` / `creativecontent` / `creativemedia`；`caller_service + caller_group_id` 只是受信调用方的追踪与额度分组，不是要求 JOIN run 表的外键。
- 业务包拿不到供应商 client、endpoint 或密钥；凭证只在派发瞬间由 `CredentialEnv` 命名的环境变量读出，不入库、不入日志、不进 request/result/model/price 快照（有定向测试断言）。
- 平台级 `platform_llm_limits` 无 `account_id`，按 `securitybudget` 先例落在 `store`，经 `txcap.LLMLimitView` 密封能力在同一物理事务内使用；它不参与任何账号范围业务查询。

## 端口

`Call` 是业务侧唯一该用的入口；其余各行是它内部按序使用的机制，直接用它们只在需要非常规控制流时（例如运维核实、到期清扫）才有必要。

| 端口 | 作用 |
|---|---|
| **`Call(ctx, CallSession, CallInput)`** | **协调入口**：预留 → 准备 → 派发 → 执行 → 补偿 → 消费一次走完；同 `BindingKey` 可反复调用 |
| `Models()` | 版本化模型目录；未配置凭证的部署**保持可见但禁用并带原因**，不静默消失 |
| `ReserveInTx` / `RearmInTx` | 保守上界预留；同 caller operation 回放不重复占额；未受理后按固定原上界重建 hold 并递增 generation |
| `PrepareInTx` / `BeginDispatchInTx` / `Execute` | 固定请求身份与 model/price 快照；一次性派发许可；事务外网络 I/O；结果完整校验后才持久 |
| `RedispatchInTx` | 重新准入 + 派发意图**同一事务**；同一请求的已证实未受理重试走它，不要自己拼 `RearmInTx` + `BeginDispatchInTx` |
| `ConsumeInTx` / `AbandonConsumerInTx` | 把完整结果交给调用方自己的持久化，并在同一事务里落「已消费」 |
| `RecordMeasurementInTx` / `GetUsage` | 只追加证据 + 唯一当前位置 + 差额结算 + 结算回执 |

## 关键实现事实

- **步骤身份由调用方持久化，operation id 由它派生**：`CallInput.BindingKey` 是调用方自己对「这一步」的稳定命名（run+step 号、命令 id 等，必须来自它已落库的东西，不能是进程内生成的值）。网关用固定命名空间 `7353fc6d-…` 对 `caller_service + binding_key + 用途` 做 UUIDv5，派生出预留与请求两个 operation id 以及 consumer key。**不另建绑定表**：`llm_usage_reservations` / `llm_requests` 上已有的 `(caller_service, caller_operation_id)` 唯一索引就是这份绑定，再加一张表只会出现第二个自称权威的来源。因此同一步骤恢复时会回放到同一预留、同一请求、同一保留声明，读到已有结果而不是再付一次钱。**该命名空间常量永久固定**，改它等于让所有已存绑定失效。
- **结果消费与业务落库同事务**：`CallInput.Consume(tx, result)` 跑在 `ConsumeInTx` 标记「已消费」的**同一个事务**里。Agent 步骤、工具调用记录与这个标记同生共死：调用方写失败则整体回滚、结果仍待消费（可原样重试），标记成功则业务行一定在。放在两个事务里会出现「结果已消费、步骤却没落库」——那条结果此后谁也拿不到。
- **预留与准备必须同事务**：`Call` 的 `admit` 把 `ReserveInTx` + `PrepareInTx` 放在一个事务。拆开会**永久毒化该 `BindingKey`**：预留只能在 `unclaimed` 时被认领一次，Prepare 失败后留下的预留没有任何请求会来认领；而「补偿释放」更糟——`released` 同样不是 `unclaimed`，下一次同 key 的 `Call` 会回放到它并永远报 `reservation already claimed`，全仓没有任何路径把它写回 `unclaimed`。同事务则等于「这次准备从未发生」，调用方改掉被拒的内容后同一步骤照常能跑。这条之所以有实际触发面，是因为 Prepare 校验了 Reserve 不校验的东西：模型能力（`CheckCapability`）、请求期限、以及 hold 是否仍覆盖请求。
- **重新准入与派发意图必须同事务**：重试走 `RedispatchInTx`（先取平台限流行 → `RearmInTx` → `BeginDispatchInTx`，全在一个事务里）。拆成两个事务是**契约违规且会卡死步骤**：中间进程退出或派发被限流，会留下「预留已重新占住、请求却还标着待重新准入」的组合，此后每次 `Call` 都在 `RearmInTx` 上撞 `reservation is not released for retry`，额度一直占到过期。同事务则整体回滚，步骤保持原样可恢复。
- **补偿是白名单：只结束证明不可能再派发的身份**。`compensate` 显式只对 `ErrDeadline` / `ErrAttemptsUsed` / `ErrCancelled` 取消请求（连同保留声明，请求持有预留，不绕过它单独还 hold）；**其余一律不动**，包括无法识别的错误和连请求都没读出来的失败。默认取消是错的——期限内还能恢复的步骤会被就地作废。明确落在「不补偿」一侧的三类：
  - `ErrRateLimited`：共享并发名额，自己会清。
  - `ErrBudget`：别人此刻正占着的上限，或一笔低于 hold 的结算就会腾出来的月桶。Harness 把它呈现为账号可处理的状态，网关无权替它宣判步骤已死。
  - `ErrState`：这一回合不归本次调用——别的 worker 正在派发，或它已终态。
  另外 `endUndispatched` 只在本事务**确实把请求取消掉**时才放弃保留声明：否则一个已付费、尚未消费的结果会被标成 `abandoned` 而永远取不回来。准备阶段不需要补偿——`admit` 同事务，失败即整体回滚。底层单请求模式在网关**证明未受理**后可重试一次，`maxAttempts` 封顶。生产 `Call` 使用下述独立计量的对话容灾链，每次额外尝试创建自己的请求和预留，不改写上一笔未知费用。
- **未结束传输的尝试不重复派发（底层单请求模式）**：请求处于 `dispatching` / `streaming` / `unknown` 时，同 `BindingKey` 再次 `Call` 直接返回 `ErrUnknown` 要求核实，不会「发一次看看到底受理没有」——那正是一次性许可要防的重复计费。`CallOutcome.RequestID` 在 `Call` 返回错误时**也会填**（`Dispatched` 同理），否则调用方无法说出要核实的是哪个请求。**已知缺口见下方「验证缺口」第 4 条：卡在 `dispatching` 的请求目前没有核实入口。**
- **请求身份**：`(account_id, caller_service, caller_operation_id)` 唯一；`request_hash` 覆盖精确消息、工具 schema、model/price 快照、输出上限与媒体内容摘要，不含 epoch、临时 URL 与 trace。价格版本变化会产生新 hash，但**不会改写已有请求**。
- **派发许可**：`llm_attempts.permit_digest` 全账号唯一，且 `permit_claimed_at` 只能由一次 `Execute` 置位；旧进程迟到或 worker 接管都领不到第二个许可。`Execute` 还会用 `request_hash` 核对调用方传入的载荷，不一致直接拒发。
- **按冻结的部署发送**：`Execute` 在发送前用 `sameDeployment` 核对当前目录条目仍是准备时冻结的那个部署（catalog 版本、deployment key、provider、请求 model id、接受别名、价格版本与币种），不一致直接 `ErrConflict` **拒发**；线上请求体里的 model id 取自冻结快照而非当前配置。否则「改一次目录」就会把已按旧模型定价、定 hold 的请求发到另一个收费模型上——等结果回来再判协议错误，钱已经花了。
- **收尾不跟随调用方取消**：网络调用用调用方 ctx，但 `finishSuccess` / `finishFailure` 的持久化跑在 `context.WithoutCancel` + 30s 超时的独立 ctx 上。调用方中途放弃时，本次尝试证明了什么仍要落库、已结束的传输仍要归还共享并发名额；共用 ctx 会把 attempt 永远留在 `dispatching` 并漏掉平台名额。
- **证据分类**：`unaccepted`（零费用、可重新准入）只来自**该部署在目录里显式声明**的「处理前拒绝」状态码（`unaccepted_statuses`，DeepSeek 据官方错误表列 429——依据是厂商文档，不是实测到的 429），或受信运维的 `VerifyUnacceptedInTx` 核实。未声明的状态码一律不享受该待遇：400/401/402/422 是 `rejected`，5xx、超时、**任何传输层失败**、流中断都是 `unknown`（保留 hold 与费用疑问）——Go 的传输错误不能证明「一个字节都没发出去」，所以网关不从它推断未受理。协议越界（模型路由变化、重复 tool_call_id、结束前断流）单列 `protocol`。**重发一次可能已计费的调用是本网关最贵的错误，所以默认永远是 unknown。**
- **证据优先级**：`evidence_rank`（供应商账单 10 > 核实未受理 8 > 本地按用量推算 5 > 运维口述 3）在推进当前位置前生效。低等级证据、`Supersedes` 指向的不是当前证据、或**同级之间没有可判定先后**时，**不动账本**，只把该证据挂 `pending` 等人工裁决，并计入 `GetUsage.OpenEvidence`。同级要放行只有两条路：`Supersedes` 明确指向当前已入账证据，或供应商自己的 `ProviderRev` 是**可比较的递增整数**（其它形式一律判为不可判定——到达时间不是事实先后，`received_at` 不参与判断）。
- **币种一律挂起不折算**：证据币种与该请求预留的币种不一致时，措施同上（留史 + `pending`），不会开出该币种的位置、不会进月桶，也不参与 hold 重算。网关内不做任何汇率换算。
- **锁序**：统一为 平台限流记录 → 分组准入行 → 预算桶 → request → reservation → attempt → position。`RearmInTx` 按 `gateway.md` 原话先锁分组/原预算桶再锁 request；`RedispatchInTx` 把它和 `BeginDispatchInTx` 合成一个事务，因此**先**取平台限流行再进 `RearmInTx`（否则会变成 L2→L4→L1 的逆序）；释放 hold（取消、拒绝后释放、未认领预留释放）都先锁月桶再锁预留；`finishSuccess` / `finishFailure` 在动 request 前先用 `LimitView.Lock` 取平台限流行（它们随后要释放并发许可），因此派发事务与收尾事务不会以相反顺序拿这两行；`VerifyUnacceptedInTx` / `CancelInTx` 不触碰该行，从月桶开始即可。预留的 period/currency、分组身份、原上界与请求的 model_key 都不可变，先无锁有界预读、锁后重新校验（`RearmInTx` 会核对上界/月份未变），FND-07 的 Harness 按同一顺序实现即不构成 ABBA。
- **月桶只由预留创建**：结算路径只锁桶不建桶，缺桶按 `ErrConflict` 报错而不是补一个空桶（补桶会把该月上限钉成 policy 默认值，掩盖账本缺行）。已知边界：桶的 `limit_micros` 在当月第一次准入时写定，中途调整账号上限要到下个月生效。
- **hold 重算按 attempt 计**：判断「哪些分量仍未决」只看**当前 attempt** 的位置，前一次尝试的零费用拒绝证据不会释放本次尝试的 hold。已入账总额 `actual_micros` 仍跨 attempt 汇总。
- **预留必须覆盖请求**：`PrepareInTx` 用与预留同一套配方重算上界（含**工具 schema** 字节），超出 `initial_reserved_micros` 直接 `ErrBudget`——请求变大只能走新准入。
- **计价**：三个不重叠分量 `input_cached` / `input_uncached` / `output`，`input_uncached = prompt_tokens − cached`，reasoning 已含在 output 不重复计。费率按**派发时刻**所在时段取，预留按 peak/off-peak 的较大者取保守上界。金额为整数 micros，`CostMicros` 用 `big.Int` 有理数向上取整。**token 与费用两个预算各自结算**：token 总量只需要输入/输出两个总数，缓存明细缺失或不自洽只让费用留疑问、不拖住 token 额度。
- **余额与超支**：`spent + reserved <= limit` 只约束**新准入**；实际超支全额入账并从此拒绝新准入，不把 spent 截断到 limit。
- **unknown 出口**：DeepSeek 无请求状态查询与取消 API（目录记 `query=none` / `cancel=none`），unknown 只能由受信运维入口 `VerifyUnacceptedInTx(requestID, evidenceSource)` 带来源核实后清除；没有「过几天算零」的自动出口。

## Eino 模型出口

`Service.NewEinoModel(ModelSession)` 返回实现 `model.ToolCallingChatModel` 的适配器：`Generate` / `Stream` 都只是把一次 `Call` 包成框架形状，`WithTools` 返回隔离配置而非改共享实例。框架重试、failover 与 Skill 模型覆盖不开放——真实重试只由 Gateway 协调层控制（对话容灾链或底层 `RedispatchInTx`），补偿规则与上面一致。

`ModelSession` = `CallSession` + 三项框架侧配置：

- `TurnKey(ctx, chat) (string, error)`：**必填，无默认值**。框架调用 `Generate` 时没有任何「恢复」概念，持久步骤身份只能从这里进来；给一个进程内默认值等于让每次恢复都重新付费，所以宁可构造时报错。
- `Consume`：直接透传给 `CallInput.Consume`。要记录 step 的运行必须设置它，没有自己业务行的辅助调用（如摘要）可以留空。
- `Stream`：`Generate` 的默认取值；`Stream()` 恒为真。

两处刻意的转换行为：

- Eino 的 `ParamsOneOf` 无法表达 `additionalProperties`，适配投影会为每个 object 节点补 `additionalProperties:false`（收紧，不是丢弃调用方意图）；其余不在受测子集内的关键字仍被拒绝。
- 框架 state 携带的多模态分片一律拒绝（`ErrCapability`）：媒体必须由调用方的 ContextBuilder 以已授权、已 pin 的句柄传入。
- `finish_reason` 为 `length` / `error` 时**不把工具调用交还框架**（`FinishReason` 仍如实返回），避免 ReAct 循环拿被截断的参数去跑真实业务工具。

## 部署配置

| 环境变量 | 作用 |
|---|---|
| `DEEPSEEK_API_KEY` | DeepSeek 凭证；**只经环境变量注入**，不入库不入 git |
| `AICODEWITH_API_KEY` | AiCodeWith 中转凭证（GPT-6 系列）；同上 |
| `KRILL_AI_API_KEY` | Krill AI Openai 官渠凭证；配置后 GPT-6 对话优先走 Krill |
| `CREATIVE_LLM_CATALOG` | 可选，覆盖内置目录的 JSON 文件路径 |
| `CREATIVE_LLM_MONTHLY_LIMIT_MICROS` | 每账号月度费用上限（micros，默认 20_000_000 = 20 USD） |
| `CREATIVE_LLM_MONTHLY_TOKEN_LIMIT` | 每账号月度 token 上限（默认 5_000_000） |

### 模型与部署

目录分两层（设计：`.codestable/features/2026-10-09-model-routing/model-routing-design.md`）：

- **模型**（`Model`）是摄影师选择的对象：`model_key`、显示名、声明的能力、按优先级排列的部署列表。目录 API 只暴露这一层，不出现供应商。
- **部署**（`Deployment`）是某家服务商对这个模型的一次具体接入：协议、`vendor_key`、地址、凭证变量、`request_model_id`（各家叫法不同就写在这里，如 `gpt-6-astra-0907`）、`accepted_model_ids`、价格、限流。一个部署只属于一个模型。
- 加载时校验：模型声明的每项能力，它列出的每个部署都必须支持（上下文与最大输出不得小于声明值），否则拒绝加载——路由到任何一个部署都不会超出请求被准入的能力。请求按模型声明的能力检查，不按部署实际更强的能力。
- **路由**（`Catalog.route`）：按优先级取第一个已启用的部署；没配凭证的部署被禁用并带原因。所有部署都不可用时模型显示为不可用，原因取优先级最高的那个部署。管理后台将来在这一处接入数据库里的顺序与开关。
- 请求在 `PrepareInTx` 时冻结路由选中的部署（`model_snapshot.deployment_key`）；派发、重新准入与执行都按冻结的部署取配置（`Catalog.Deployment`），优先级之后怎么变都不会把已准备的调用挪走；冻结的部署从目录里删掉时以 `ErrConflict` 拒绝，不发送。
- 网关启动时要求**每个已启用的部署**都有适配器和凭证，而不只是当前排第一的那个。
- 2026-10-10 起，生产对话 `Call` 按下述策略重试和切换；每个实际请求保留自己的部署/价格快照。Owner 明确要求暂不做熔断，每轮重新从最高优先级开始。

`CREATIVE_LLM_CATALOG` 文件格式与内置目录同构：

```jsonc
{
  "version": "…", "currency": "USD",
  "models": [{"model_key": "gpt-6-astra", "display_name": "GPT-6 Astra", "capability": {…},
              "deployments": ["aicodewith/api/gpt-6-astra"]}],
  "deployments": [{"deployment_key": "aicodewith/api/gpt-6-astra", "vendor_key": "aicodewith",
                   "provider": "openai_responses", "credential_env": "AICODEWITH_API_KEY", "enabled": true, …}]
}
```

文件里 `enabled: true` 的部署若凭证变量未设置，加载时同样被禁用并写明原因。2026-10-09 之前的扁平格式（`models` 直接是部署条目）已作废。

### 内置目录

内置目录 `DefaultCatalog` 版本 `deepseek-aicodewith-krill-2026-10-10`，GPT-6 模型依次尝试选择已配置凭证的 Krill、AiCodeWith 部署，DeepSeek 保持单部署。DeepSeek 价格来源见
<https://api-docs.deepseek.com/quick_start/pricing>（2026-09-11 读取），三个模型：

| model_key | 供应商模型 | 思考模式 | tool_choice | 图片输入 |
|---|---|---|---|---|
| `deepseek-flash` | `deepseek-flash` | disabled | ✓ | ✓ |
| `deepseek-flash-thinking` | `deepseek-flash` | enabled | ✗（派发前拒绝） | ✓ |
| `deepseek-v4-pro` | `deepseek-v4-pro` | disabled | ✓ | ✗ |

flash 的图片输入 2026-10-09 按实测开启：64–400 px 约 184 个输入 token，1600 px 起封顶约 994（3000 px 相同），在 `EstimateInputTokens` 每图 4096 的上界之内；证据见 `TestLiveDeepSeekFlashSeesAnImage`。v4-pro 保持关闭：发图只多 5 个 token，却照样描述了一张没收到的图。

GPT-6 系列原有 AiCodeWith 部署经 AiCodeWith 中转（`vendor_key=aicodewith`，`https://api.aicodewith.ai/v1`），走 **Responses 协议**（`openai_responses` 适配器）而不是 chat completions：OpenAI 文档写明 chat completions 不把推理带回后续轮次，多步工具调用会降质。请求固定 `store:false` + `include:["reasoning.encrypted_content"]` + `reasoning.context:"all_turns"`，加密推理条目随助手消息存入 `reasoning` 列、下一轮原样交回。推理只回给**产生它的同一部署、同一模型**：加密内容绑定上游组织，同一个模型换到另一家服务商会被 400 拒绝，而 400 不允许切换；换了部署就丢弃推理，只损失这一轮的连续性与缓存。DeepSeek 的明文推理与 GPT 的加密条目互不串用，DeepSeek 思考部署收到 GPT 轮次时 `reasoning_content` 置空。

| model_key | 推理强度 | 输入 / 缓存 / 输出（USD 每百万） | 上下文 / 最大输出 |
|---|---|---|---|
| `gpt-6.1-sol` | medium | 2 / 0.1 / 10 | 1M / 128K |
| `gpt-6-sol` | medium | 2 / 0.2 / 10 | 1M / 128K |
| `gpt-6-astra` | medium | 10 / 1 / 50 | 1M / 128K |

**中转注入的系统提示**（2026-10-09 实测）：请求不带 `instructions` 时，AiCodeWith 会塞入约 2.2 万字符、约 4300 token 的 Codex 编程助手提示词，模型会自称 Codex，每次调用也多出这部分输入。所以适配器把**开头的纯文字系统消息合并进顶层 `instructions`**（对话中途的系统消息仍留在 `input` 原位）；带上自己的指令后，一句话提问的输入从约 4300 token 降到约 120。中转仍会在指令末尾追加一段 `<gateway_time_context>`，时区写的是 `America/Los_Angeles`，并声明它优先于其他来源的当前时间——北京上午时它给出的「今天」是前一天，涉及日期的提示词要注意这一点。

其他实测：三个模型回包的模型名与请求 ID 完全一致；400 px 图片约 200 个输入 token 且描述正确；加密推理连续三轮跨轮交回都被接受；medium 强度下一轮 3–8 秒，远低于 2 分钟传输上限；medium 下简单步骤经常不产生推理（推理 token 为 0），这是模型自己的选择。真实接口测试为保证产生推理，工具循环用 high 强度。

价格是 OpenAI 官方价（经 Artificial Analysis 2026-10-09 读取）。AiCodeWith 按「官方价 × 渠道折扣」扣费（订阅渠道约 1 折），便宜渠道被风控时自动切到更贵的渠道，所以目录按官方价记账，作为始终成立的上界；账面费用会高于中转实扣。中转未公开任何「处理前拒绝」的状态码，`UnacceptedStatuses` 为空。三个部署共用 `aicodewith/api` 限流键。

### Krill AI 接入（2026-10-10）

依据登录后的 [Openai 官渠教程](https://www.krill-code.com/app/tutorial?channel=Openai+%E5%AE%98%E6%B8%A0&client=api-code)、同页 OpenCode 配置和[模型广场](https://www.krill-code.com/app/pricing)：

- `vendor_key=krill-ai`，部署键为 `krill-ai/codex/<model_key>`，API Base URL 为 `https://api.krill-code.net/codex/v1`；使用 Bearer 鉴权，凭证环境变量为 `KRILL_AI_API_KEY`。
- 接入现有 `gpt-6.1-sol`、`gpt-6-sol`、`gpt-6-astra` 模型，同名发送；复用 Responses 适配器，POST `/codex/v1/responses`，medium 推理、`store:false`、加密推理回传。`reasoning_context` 是部署可选扩展，Krill 留空不发送，AiCodeWith 继续设置 `all_turns`。
- 模型广场给出 1.05M 上下文、128K 输出，目录沿用保守的 1M/128K；教程 OpenCode 示例统一写 272K，不用该客户端示例覆盖模型广场的能力表。
- 两个密钥都配置时，新请求优先 Krill；没有 Krill 密钥时继续使用 AiCodeWith。已冻结的 AiCodeWith 请求仍使用原部署。超时、429、5xx 按下述对话容灾策略重试和切换，每次单独记账。历史文本与工具结果保留，其他部署的加密推理不发给 Krill。
- 三个模型共享 `krill-ai/codex` 限流键（并发 4、每分钟 60）；查询、取消能力暂不声明。生图仍使用 AiCodeWith。

平台费用继续采用**保守上界**，不是 Krill 钱包扣款镜像。登录价格表显示 Standard 渠道倍率 0.15，且钱包有单独的充值换算；平台不把折扣或钱包单位直接当 USD 结算。按未打折价格、Sol 的 >272K 长上下文档，以及非缓存输入可能包含的缓存写入上界记账：

| model_key | 输入 / 缓存读取 / 输出（平台 USD 每百万 token） |
|---|---|
| `gpt-6.1-sol` | 5 / 0.4 / 15 |
| `gpt-6-sol` | 5 / 0.8 / 15 |
| `gpt-6-astra` | 12.5 / 2 / 50 |

缓存写入包含于未命中输入，按较高输入单价保守计入，不重复相加。未启用 Fast / Ultrafast。若要精确映射钱包账单，应另行扩展阶梯价格、缓存写入分量与币种换算。

在 API 与 creative-worker 的环境中配置密钥并重启两者即可启用；若设置了 `CREATIVE_LLM_CATALOG`，该文件会完全覆盖内置目录，需要同步增加 Krill 部署。不同线路可在自定义目录调整 `base_url`，不要把密钥写进目录文件。

本次模拟协议测试覆盖路由优先级、缺失凭证、旧部署可恢复、路径与鉴权、JSON/SSE、工具调用、usage 和跨供应商历史隔离；真实供应商验收已于 2026-10-10 16:02（Asia/Shanghai）通过：

```bash
# 从项目工作区根目录运行；会调用真实供应商，测试账号预算上限 1 USD。
set -a && . ./.env && set +a
cd backend
KRILL_AI_LIVE=1 go test ./internal/platform/llmgateway/ -run '^TestLiveKrill' -count=1 -v
```

实测 `TestLiveKrillModelsAndToolHistory` 全部通过：共 5 次请求，三个模型的非流式回答均返回匹配的模型 ID 和输入/输出用量；GPT-6.1 Sol 完成图片识别、流式工具调用及工具结果回传后的回答。主体耗时 85.83 秒，其中三个单轮回答分别为 56.49 / 11.87 / 5.46 秒。测试账号预算上限 1 USD，不代表实际消费 1 USD；未核对供应商钱包扣款。该用例允许模型不产生加密推理，因此不单独证明每轮均有非空加密推理回传。

真实测试覆盖三个模型、图片与流式工具回合；仅有密钥不会触发，必须显式设置 `KRILL_AI_LIVE=1`。AiCodeWith 的真实测试固定选用其自身部署，不因 Krill 优先级而误测另一家。

共用同一 `limit_key` 的部署必须声明相同的 `Concurrency` / `RateLimitPerMin`，否则目录加载直接拒绝——它们共享一条平台准入记录，参数不一致会让每次派发互相改写对方的容量。运行期调小 `Concurrency` 不会把容量压到在飞许可数以下（`GREATEST(新容量, active_count)`），缩容随在飞许可自然回落生效，不会因为表上的 `capacity` 约束把该 key 的派发全部打成约束错误。

**启用真实 Anthropic 部署前的前置项**：`provider_anthropic.go` 的 usage 映射没有独立的 cache-write 分量，而 Anthropic 对写入缓存单独计价——直接开一个真实部署会系统性少计该笔费用。必须先补第四个不重叠分量（及其价格配方）再启用；目录里当前没有任何 Anthropic 部署，`CREATIVE_LLM_CATALOG` 覆盖时也不要自行加。

思考模式是**固定的部署参数**而非调用方参数：2026-09-11 实测确认，思考模式下带 `tool_choice` 会被供应商以 400 `Thinking mode does not support this tool_choice` 拒绝，因此该能力在目录里关掉、由 `CheckCapability` 在派发前拒绝，不浪费一次尝试去换这个结论。同时思考模式会把输出预算大量用在 reasoning 上，小 `max_tokens` 会直接 `finish_reason=length` 且正文为空。

## 对话重试与供应商容灾（2026-10-10 owner 决策）

本节取代此前“不确定结果不重试”的生产对话策略；生图任务不受本次调整影响。

- 每轮模型调用从目录优先级最高的已启用部署开始，没有熔断或跨轮降级记忆。Krill → AiCodeWith，仅切换同一模型的部署。
- 每家首次调用失败后，最多再重试 5 次，即每家最多 6 次；距离上次传输结束至少 5 秒后再派发，切换下一家也沿用该间隔。
- HTTP 429、500/502/503/504、网络错误、超时、断流和上游响应失败可重试。401/402/403 直接跳到下一家；参数错误等不可恢复错误不反复发送。每次派发重新执行授权、运行资格、额度和限流检查；取消和截止时间会中断等待及后续派发。
- 所有供应商尝试耗尽后返回 `ErrProvidersExhausted`，Agent 结束本轮并释放运行槽，显示“当前负载较高，请稍后尝试”。费用结算可继续为 unknown，不因此展示无限核实进度。
- 耗尽有两种可区分的原因，错误同时包裹 `ErrProvidersExhausted` 与具体原因：后续尝试因月度额度放不下新预留而无法准入（包裹 `ErrBudget`，先尝试下一家，仍不行即结束，Agent 显示 `creative_llm_budget_exceeded`）；模型的所有部署都已禁用或从目录撤下、一次也没准入（包裹 `ErrUnavailable`，Agent 显示 `creative_model_unavailable`）。已撤下或禁用的部署在准入前跳过，不会让进行中的路线在重放时永久冲突。
- **额度算术（默认配置下的已知限制）**：每次尝试按上界独立预留（输入上界 256K token、输出上界 128K token），未知结果的预留在核实前不释放。GPT-6 Astra 每次约 9–9.7 USD，Sol 经 Krill 每次约 3.2 USD，每次约 39 万 token；默认月度上限 20 USD / 500 万 token 下，连续 5xx 时 Astra 约第 3 次、Sol 约第 6 次就会因额度停止，到不了“每家 6 次再切换”。收紧预留和核实释放由计费整改 issue 处理。
- `Build` 启用 `Config.ChatRetry`，低层 `New`/阶段端口仍可使用原有单请求协议。`llm_chat_routes` 冻结本轮部署顺序并指向最终成功请求；后续尝试采用由首请求和序号派生的稳定绑定。一次完整结果最多消费一次；已结束的对话调用重放不会重新购买尝试。
- 每次网络尝试独立准入、冻结部署/价格、预留和记账。未知结果的请求在已确认本地传输结束后标为响应失败，原 attempt 的 unknown 和预算预留继续保留；`VerifyUnacceptedInTx` 接受这类“failed 但仍有 unknown attempt”的请求，核实后只释放预留，不会把它恢复成可派发。Agent 的运行结算按整个调用组汇总，重试后成功的运行仍会因被取代的 unknown 尝试显示待核实。重试可能重复产生上游费用，这是 owner 授权的新可用性策略；不会伪造“未受理”来释放旧预留。计费核实与清理仍由独立整改 issue 跟踪。
- 仅有明确的本地传输结束记录才进入额外尝试。进程中断、正在派发的并发调用、无结束证据的历史 unknown 请求保持原有核实路径，不因升级自动重发。
- 流式重试会清除失败尝试的正文预览；只有完整验证并落库的工具调用才交给 Agent 执行。跨部署时仍不传递另一家产生的加密推理。
- 新增迁移 `0058_chat_failover`，以及请求的 `failure_http_status` 字段，用于保留具体 HTTP 错误码；没有 HTTP 应答的失败（超时、断连）记为 NULL。部署时先启动 API 完成迁移，再启动 creative-worker。

## 验证

```bash
make check-go PKG=./internal/platform/llmgateway/...
```

真实供应商验收默认跳过，需要显式注入凭证才会花钱：

```bash
set -a && . ./.env && set +a && cd backend && go test ./internal/platform/llmgateway/ -run TestLiveDeepSeek -count=1 -v
set -a && . ./.env && set +a && cd backend && go test ./internal/platform/llmgateway/ -run TestLiveAICodeWith -count=1 -v
```

AiCodeWith 的真实调用测试按官方价自带 1 USD 月度上限（中转实扣约其一折），覆盖三个模型各答一次（并断言输入没有被注入提示词撑大）、加密推理跨轮交回的流式工具循环（连跑三轮）、看图。中转的 Cloudflare 会以 403「error code: 1010」拦截 Python urllib 默认 UA，手工探测时需要带 `User-Agent`；Go 客户端不受影响。

真实调用测试自带 0.05 USD 的月度上限，三个用例覆盖非流式、流式重组与真实工具调用；它不进入默认 `make check-go` 的必跑路径。默认 `-run` 全跑时是 60 个非 live 用例通过 + 3 个 live 用例按凭证缺失 skip。

尚无覆盖、明确留给后续的验证缺口：

- `applyCostPosition` / `applyTokenPosition` 的 `affected == 0` 并发分叉分支（READ COMMITTED + `FOR UPDATE` 下后到者会重读到新 revision 并成功推进，该分支当前不可达，留作防御）。
- **FND-10 的预留到期清扫必须走 `ReleaseReservationInTx` / `CancelInTx`**，或完整复刻「锁预留行 → 读当时 hold → 按该值扣月桶 → 归零」。若改成从 `llm_reservation_expiry` 索引扫出行、在另一事务里直接减 `reserved_micros`，本模块任何守卫都拦不住额度被还两次——保护来自同事务内的 `FOR UPDATE` 重读，不是 UPDATE 上的 revision 条件。
- **`dispatching` 没有核实出口（上线前必须补，已在 owner 排期清单「完善异常恢复」内）**：`BeginDispatchInTx` 提交后、`Execute` 的 `claimPermit` 之前进程退出（部署、OOM），或 `Execute` 成功但 `finishSuccess` 第一个事务没在 30s `finishTimeout` 内跑完（钱已花、结果已拿到却没落库），请求会永久停在 `dispatching`。此后每次 `Call` 都返回 `ErrUnknown` 要求核实，但 `VerifyUnacceptedInTx` 第一句就是 `state != StateUnknown → ErrState`——**核实入口不收 `dispatching`**，仓库里也没有任何 `dispatching → unknown` 的迁移。同一缺口还漏并发名额：`platform_llm_limits.active_count` 是纯计数器，没有租约或回收，每次这样崩溃都永久漏掉一个名额，而该行按 `limit_key` **跨账号共享**（内置目录三个部署共用 `deepseek/api`），漏够 `Concurrency` 次之后所有账号的派发都会 `ErrRateLimited` 且不自愈。补的时候两件事要一起做：给 `dispatching` 一个带证据的核实/降级入口，以及 `active_count` 的回收（按 attempt 的 `permit_claimed_at` / `permit_released_at` 与 `dispatched_at` 判定陈旧）。
- 吞吐观察（不影响正确性）：`finishSuccess` / `finishFailure` 会把 `platform_llm_limits` 那一行持有到事务提交，而该行按 `limit_key` 跨账号共享（内置目录三个部署共用 `deepseek/api`），`finishFailure` 持锁期间还要写 3 条零费用证据与相应位置/预算。事务内无网络 I/O，语句都很短；若将来并发上去，可把 `releasePermit` 拆成紧随其后的独立短事务（收尾事务就不必取该行），或把 `finishFailure` 的核算移入第二个事务（`finishSuccess` 已是这个形状）。观测看「限流等待」与「收尾时延」两个维度。


## 孤立派发恢复（2026-09-13）

`creative-worker` 启动立即运行恢复巡检，此后每 15 秒扫描一页账号（每页 100 个、每账号最多 100 个 attempt，单轮 10 秒超时）。账号采用 keyset 游标，包含非 active 账号，错误会记录并在后续轮次重试。恢复不依赖模型目录或供应商凭证。

每次派发从已持久的 `dispatched_at` 起最多允许 120 秒本地传输，且不超过请求 deadline；HTTP client 配置只能进一步缩短该时间。过期许可不能被领取。派发满 180 秒后，巡检按平台限流→预算→请求→attempt 的锁序，将未收尾的 dispatching/streaming 转为 unknown，并以 `permit_released_at IS NULL` 幂等归还名额。额度 hold 不释放，也不生成零费用证据。`transport_finished_at` 不伪填为观测时间，恢复只记录自己的状态与名额回收时间。

完整结果的首个落库事务失败后，同样通过此路径解除阻塞；进程仍持有的迟到完整结果允许归并到同一 unknown attempt，已被核实或重试替换的旧 attempt 不得覆写当前请求。核实未受理仍必须走受信 `VerifyUnacceptedInTx`，扫到过期不等于供应商未收费。

供应商适配器必须遵守传入 context 的硬截止时间。首次部署此修复须停止旧版执行器后再启动新版，旧代码没有这个传输上限，不能与回收器并行运行。没有新增数据库迁移，仍要求已有 0043；恢复依赖运行中的 `creative-worker`。持久化彻底失败且供应商无查询能力时，丢失的回答无法凭空重建，状态保留 unknown，费用交由核实。
