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
| `RedispatchInTx` | 重新准入 + 派发意图**同一事务**；重试一律走它，不要自己拼 `RearmInTx` + `BeginDispatchInTx` |
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
  另外 `endUndispatched` 只在本事务**确实把请求取消掉**时才放弃保留声明：否则一个已付费、尚未消费的结果会被标成 `abandoned` 而永远取不回来。准备阶段不需要补偿——`admit` 同事务，失败即整体回滚。执行失败后只有网关**自己证明了未受理**（请求回到 `prepared` 且 `retry_eligible`）才重试一次，`maxAttempts` 封顶。
- **未结清的尝试永不重发**：请求处于 `dispatching` / `streaming` / `unknown` 时，同 `BindingKey` 再次 `Call` 直接返回 `ErrUnknown` 要求核实，不会「发一次看看到底受理没有」——那正是一次性许可要防的重复计费。`CallOutcome.RequestID` 在 `Call` 返回错误时**也会填**（`Dispatched` 同理），否则调用方无法说出要核实的是哪个请求。**已知缺口见下方「验证缺口」第 4 条：卡在 `dispatching` 的请求目前没有核实入口。**
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

`Service.NewEinoModel(ModelSession)` 返回实现 `model.ToolCallingChatModel` 的适配器：`Generate` / `Stream` 都只是把一次 `Call` 包成框架形状，`WithTools` 返回隔离配置而非改共享实例。框架重试、failover 与 Skill 模型覆盖不开放——真实重试只由协调层的 `RedispatchInTx` 控制，补偿规则与上面一致。

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
| `CREATIVE_LLM_CATALOG` | 可选，覆盖内置目录的 JSON 文件路径 |
| `CREATIVE_LLM_MONTHLY_LIMIT_MICROS` | 每账号月度费用上限（micros，默认 20_000_000 = 20 USD） |
| `CREATIVE_LLM_MONTHLY_TOKEN_LIMIT` | 每账号月度 token 上限（默认 5_000_000） |

内置目录 `DefaultCatalog` 版本 `deepseek-2026-09-11`，价格来源见
<https://api-docs.deepseek.com/quick_start/pricing>（2026-09-11 读取），三个部署：

| model_key | 供应商模型 | 思考模式 | tool_choice | 图片输入 |
|---|---|---|---|---|
| `deepseek-flash` | `deepseek-flash` | disabled | ✓ | ✗ |
| `deepseek-flash-thinking` | `deepseek-flash` | enabled | ✗（派发前拒绝） | ✗ |
| `deepseek-v4-pro` | `deepseek-v4-pro` | disabled | ✓ | ✗ |

图片输入虽然 flash 官方支持，但**目前没有实测的字节→token 映射**，硬预算无法给出可信上界，因此目录里关闭；恢复它需要先补该映射（属 FND-07 接入图片上下文前的前置项）。

共用同一 `limit_key` 的部署必须声明相同的 `Concurrency` / `RateLimitPerMin`，否则 `NewCatalog` 直接拒绝——它们共享一条平台准入记录，参数不一致会让每次派发互相改写对方的容量。运行期调小 `Concurrency` 不会把容量压到在飞许可数以下（`GREATEST(新容量, active_count)`），缩容随在飞许可自然回落生效，不会因为表上的 `capacity` 约束把该 key 的派发全部打成约束错误。

**启用真实 Anthropic 部署前的前置项**：`provider_anthropic.go` 的 usage 映射没有独立的 cache-write 分量，而 Anthropic 对写入缓存单独计价——直接开一个真实部署会系统性少计该笔费用。必须先补第四个不重叠分量（及其价格配方）再启用；目录里当前没有任何 Anthropic 部署，`CREATIVE_LLM_CATALOG` 覆盖时也不要自行加。

思考模式是**固定的部署参数**而非调用方参数：2026-09-11 实测确认，思考模式下带 `tool_choice` 会被供应商以 400 `Thinking mode does not support this tool_choice` 拒绝，因此该能力在目录里关掉、由 `CheckCapability` 在派发前拒绝，不浪费一次尝试去换这个结论。同时思考模式会把输出预算大量用在 reasoning 上，小 `max_tokens` 会直接 `finish_reason=length` 且正文为空。

## 验证

```bash
make check-go PKG=./internal/platform/llmgateway/...
```

真实供应商验收默认跳过，需要显式注入凭证才会花钱：

```bash
set -a && . ./.env && set +a && cd backend && go test ./internal/platform/llmgateway/ -run TestLiveDeepSeek -count=1 -v
```

真实调用测试自带 0.05 USD 的月度上限，三个用例覆盖非流式、流式重组与真实工具调用；它不进入默认 `make check-go` 的必跑路径。默认 `-run` 全跑时是 60 个非 live 用例通过 + 3 个 live 用例按凭证缺失 skip。

尚无覆盖、明确留给后续的验证缺口：

- `applyCostPosition` / `applyTokenPosition` 的 `affected == 0` 并发分叉分支（READ COMMITTED + `FOR UPDATE` 下后到者会重读到新 revision 并成功推进，该分支当前不可达，留作防御）。
- **FND-10 的预留到期清扫必须走 `ReleaseReservationInTx` / `CancelInTx`**，或完整复刻「锁预留行 → 读当时 hold → 按该值扣月桶 → 归零」。若改成从 `llm_reservation_expiry` 索引扫出行、在另一事务里直接减 `reserved_micros`，本模块任何守卫都拦不住额度被还两次——保护来自同事务内的 `FOR UPDATE` 重读，不是 UPDATE 上的 revision 条件。
- **`dispatching` 没有核实出口（上线前必须补，已在 owner 排期清单「完善异常恢复」内）**：`BeginDispatchInTx` 提交后、`Execute` 的 `claimPermit` 之前进程退出（部署、OOM），或 `Execute` 成功但 `finishSuccess` 第一个事务没在 30s `finishTimeout` 内跑完（钱已花、结果已拿到却没落库），请求会永久停在 `dispatching`。此后每次 `Call` 都返回 `ErrUnknown` 要求核实，但 `VerifyUnacceptedInTx` 第一句就是 `state != StateUnknown → ErrState`——**核实入口不收 `dispatching`**，仓库里也没有任何 `dispatching → unknown` 的迁移。同一缺口还漏并发名额：`platform_llm_limits.active_count` 是纯计数器，没有租约或回收，每次这样崩溃都永久漏掉一个名额，而该行按 `limit_key` **跨账号共享**（内置目录三个部署共用 `deepseek/api`），漏够 `Concurrency` 次之后所有账号的派发都会 `ErrRateLimited` 且不自愈。补的时候两件事要一起做：给 `dispatching` 一个带证据的核实/降级入口，以及 `active_count` 的回收（按 attempt 的 `permit_claimed_at` / `permit_released_at` 与 `dispatched_at` 判定陈旧）。
- 吞吐观察（不影响正确性）：`finishSuccess` / `finishFailure` 会把 `platform_llm_limits` 那一行持有到事务提交，而该行按 `limit_key` 跨账号共享（内置目录三个部署共用 `deepseek/api`），`finishFailure` 持锁期间还要写 3 条零费用证据与相应位置/预算。事务内无网络 I/O，语句都很短；若将来并发上去，可把 `releasePermit` 拆成紧随其后的独立短事务（收尾事务就不必取该行），或把 `finishFailure` 的核算移入第二个事务（`finishSuccess` 已是这个形状）。观测看「限流等待」与「收尾时延」两个维度。
