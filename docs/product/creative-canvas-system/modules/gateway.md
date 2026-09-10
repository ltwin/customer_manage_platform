---
status: proposed
version: 0.2
created: 2026-09-10
---

# LLM Gateway 模块设计

本稿承接[Gateway 边界](../llm-gateway.md)，与 [Harness](harness.md)、[数据与事务协议](gateway-harness-data.md)一起定义第三批模块契约。首期仍为 Go 进程内模块、共享 PostgreSQL、Worker 执行外部调用；不部署 LiteLLM Proxy，不开放浏览器直接访问模型的数据面。这里的接口、参数为待实施契约。

## 1. 模块入口与内部职责

建议目录 `internal/platform/llmgateway`；公开 Catalog、Admission、Requests、Accounting 四个深接口，内部包含供应商适配、受控派发和恢复。业务不能拿到供应商 client 或密钥。Gateway 不 import creativeagent/creativecanvas；来源 `caller_service + caller_group_id` 是受信调用方的追踪/额度分组，不是要求 JOIN run 表的外键。

| 端口 | 输入 → 输出与事务责任 |
|---|---|
| Catalog.List(scope) | 可用稳定 model_key、展示名、provider、能力、目录版本、暂不可用原因；无凭证 |
| Admission.ReserveInTx | 可信调用方、操作身份、分组上限、固定模型/价格、最大输入输出、截止时间 → reservation_id 与保守预留；已有同key同hash回放，不重复占额 |
| Requests.PrepareInTx | caller_operation_id、规范化请求、输入摘要、已有初始预留或新预留 → request_id、固定身份；初始预留只能被一个请求认领 |
| Admission.RearmInTx | 同request/预留、前一次attempt可靠未受理零费用证据、预期hold_generation → 在固定原预算桶及caller_group重新检查额度并重建hold；与下一attempt派发意图同事务，不借Reserve的幂等回放占额 |
| Requests.LockInTx / BeginDispatchInTx | 先按公共锁序取得请求相关锁，返回不可由模型构造的事务内句柄；调用方完成业务/用途校验后，BeginDispatch 再核对该句柄并原子写派发意图/attempt，不在此阶段反向补锁 |
| Requests.Execute | 已提交的唯一派发许可 → 标准流与最终结果；事务外网络I/O；同一许可只能被派发执行器消费一次，重启无内存许可时只核实 |
| Requests.Get / CancelInTx | 返回结果状态、费用状态、保留期限；取消阻止新派发，已派发尽力中断，不伪称供应商没有受理 |
| Requests.ConsumeInTx | 将已持久的完整结果供调用方保存到消息/step，原子释放该消费者保留；重复消费回放业务记录，不再次生成工具步骤 |
| Accounting.GetUsage / RecordMeasurement | 按可信请求或分组查询；仅受信适配器/核实流程可以追加用量证据；按当前位置差额核算 |

上述签名是领域形状，正式 Go 类型在实现切片写入。不提供 ReserveAndCall 这种把数据库事务包住网络请求的接口。HTTP/SDK重试必须关闭；所有真正请求尝试只能由本模块创建并计数。

## 2. 统一模型契约 v1

`ChatRequest` 的业务语义固定为：contract_version、model_key、messages、tools、tool_choice、output_limit、可选 temperature、可选受支持 response_format。`n=1`；不接受任意供应商参数、浏览器自填system指令或私有URL。身份、deadline、预算、追踪来自控制封套，不塞进模型消息。

- Message role 为 system/user/assistant/tool；内容块为 text 或 image_input，工具消息必须引用已验证的本次对话 tool_call_id。音视频首期只提供注明范围的元信息文本，不能冒充完整媒体理解。
- image_input 是调用方已授权并读取的有界媒体输入句柄，绑定不可变修订/渲染摘要。Gateway 适配器只读取句柄提供的流，不从资产ID或任意HTTP URL抓取内容。发送前最后一次业务校验仍由 Harness 执行，临时读取票据不入持久请求hash。
- ToolDefinition 固定 name、description、input_schema、schema_version；只支持已测试的 JSON Schema 子集（object/properties/required/additionalProperties:false、基本标量、array/items、enum、数量/长度范围）。Gateway检查模型能力；Harness使用同一正式schema二次校验参数。
- 输出只有一个完整 assistant result：文本、结构化 ToolCall 列表、finish_reason、usage_evidence、实际模型信息。finish_reason 统一 stop/tool_calls/length/refused/error；length/error下的工具参数不执行，即使局部JSON看似完整。
- 文本/工具参数的流增量不是成功结果。适配器按 choice/tool index 重组碎片，验证ID唯一、名称、UTF-8、JSON尺寸与最终结束标记后保存完整结果；并行工具请求在 Harness v1 中依返回次序串行处理。
- 供应商返回的model只能匹配固定部署及目录记录的允许版本别名；发生不允许的路由变化标记协议错误，保留费用证据，不执行工具。能力缺失在派发前拒绝，不静默删除字段。

模型目录以部署版本化配置维护（不需要一套可编辑模型管理后台）：model_key、catalog_version、provider_key、deployment_key、accepted_model_ids、endpoint配置引用、credential环境变量引用、input_kinds、tool_calling、json_schema、context/output限制、usage维度、计价版本、查询/取消能力、启用状态。客户端只能选择key。运行保存无密钥快照；同key改配置不改变活动请求，旧快照适配不可用则明确失败。可切模型是新请求/新运行，不热替换正在运行的模型。

LiteLLM 的标准流和工具碎片形式是兼容参考，具体参数仍以本契约为准。官方示例支持流式重组，但这不提供本系统持久回放保证。[流式接口](https://docs.litellm.ai/docs/completion/stream)、[工具碎片类型](https://github.com/BerriAI/litellm/blob/main/litellm/types/llms/openai.py)。当前未选定具体供应商、型号或SDK补丁版；启用模型前必须通过 GH-01 适配清单。

## 3. 身份、派发与未知结果

请求唯一键 `(account_id,caller_service,caller_operation_id)`；operation在步骤准备时由服务端生成并持久化。hash覆盖精确消息、工具/Skill产生的实际指令、模型/价格快照、输出限制及媒体内容摘要；不含epoch、临时URL、trace。服务端稳定created_at与墓碑防止过期调用被误认首次。价格变化不能改已有请求的hash或预留。每次新的模型轮次有新operation，网络恢复沿用原operation。

请求 result_state 与 settlement_state 分开；前者仅描述能否取得完整模型结果。状态转换是 Gateway 自己的事实，不替代 run.state：

| 当前 → 下一状态 | 条件与动作 |
|---|---|
| 新建 → prepared | 模型能力、预算通过；保存输入、初始消费者保留和请求；未外发 |
| prepared → dispatching | 账号能力、调用方执行权、用途、同意、期限、限流都通过；短事务原子保存唯一attempt派发意图与消费许可摘要 |
| dispatching → streaming | 原执行器观测到供应商响应；结果仍不完整 |
| dispatching/streaming → succeeded | 完整且合法结果已持久；usage缺失可保留 settlement=unknown，不阻止读取已知结果 |
| dispatching/streaming → unknown | 网络/进程中断、非可靠拒绝或无法证明结果；禁止再发请求 |
| dispatching → prepared | 可靠证明未受理且无费用，记录attempt rejected/零核实证据并释放旧hold；仍在期限内、未取消/关闭且尝试未耗尽时保留原request身份待重新准入，prepared不等于当前已有额度 |
| dispatching/streaming → failed | 已明确不可恢复的请求拒绝，或可靠未受理但取消/关闭/到期/尝试耗尽；核算按实际证据处理，模型内容拒答仍是完整result的refused finish_reason |
| prepared → failed/cancelled | 不可恢复校验失败/截止/取消，尚未派发；释放未用额度 |
| unknown → prepared | 查询可靠证明未受理且零费用，未取消/关闭、仍在期限内、尝试未耗尽；原attempt置rejected，释放旧hold，保存可重新准入标记，不生成新request/operation |
| unknown → succeeded/failed | 获得完整结果则succeeded；已知不可重试拒绝，或未受理但已取消/关闭/到期/次数耗尽则failed并保存原因；没有可靠证据保持unknown |

succeeded/failed/cancelled无新派发出边；只有上表明确的prepared恢复分支允许原身份再次派发。Harness按Gateway的retry_eligible/原因决定queued或结束，不把任意failed当可重试；unknown可在执行关闭后长期保持，但不再占Harness slot。request.closed_at表示调用方停止等待，不证明无费用；后续查询只补结果和核算。Cancel在prepared时原子终结；已dispatching/streaming/unknown时设置cancel_requested_at，尽力取消，按已知事实归并，不能将unknown强转无费cancelled。

派发意图提交和真正发HTTP之间无法原子：事务成功后、发送前崩溃也按unknown核实，首期宁可产生可解释的未执行任务，也不重复潜在收费请求。仅原进程拿到成功COMMIT并持有一次性许可时才发出；COMMIT unknown不发送。Worker租约转移/旧进程迟到不能获得第二个许可；逻辑派发先于撤销视为已经外发，原调用可能完成，后续结果只按当前执行权决定是否用于工具。

可靠未受理由具体适配器证据分类，不能把任意429/5xx/timeout等同未受理。最多2次实际尝试，只重试可证实未受理无费用的情况；不对冲、不静默降级、不自动切供应商。返回结果未知且供应商没有查询能力时提供“结束等待”，费用保持待核实。人工核实使用带来源的受信运维入口，不让摄影师直接提交cost=0。

## 4. 预算与核算

预算桶固定账号/UTC月份/币种，初期只允许一种配置币种；金额使用整数micros。每次预留同时检查月桶与 caller_group_id 的运行上限（按同组预留/已入账事实聚合，先锁同组准入行、再锁相关预算桶；不在Agent维护spent）。分组准入行在首次预留创建并固定上限，后续预留均先锁它；跨月同组仍统计全部相关月份并按月份排序锁桶，不能换月份重置运行上限。请求认领初始预留不再占一次；后续轮次新预留。

run_limit_micros必须来自服务端配置与摄影师允许上限的较小值；request预留由输入计量/保守上界、输出上限和固定价格配方得到。缺失价格或不能给出可信上界的模型不可启用硬预算调用。输入上限包含系统/Skill、工具schema、历史、图像和预期工具结果；预留后实际请求扩大须重新准入，不改原请求。

计量维度至少包含 input_total_tokens、input_cached_tokens、output_tokens；unknown用NULL/显式certainty，不能用0补齐。计价配方将包含关系转为不重叠分量（如普通输入=input_total−cached、缓存输入、输出）；reasoning若已含在output不可再加一次。每个分量按定点费率、有理数计算并向上取整到micros。Token预算用配方定义的input_total+output（不再加cached），与成本维度分别核算。

`llm_usage_measurements`只追加；`llm_cost_positions`是每attempt/分量/币种的唯一当前值；token也有单独当前位置。每次有效新证据锁预算→请求/预留→attempt→当前位置，计算新值减已入账值，同事务更新预算与唯一revision结算回执。证据优先级、分叉挂起遵循[data-model](../data-model.md#llm-gateway-数据归属)，收到时间不决定新旧。成本证据应用与处置状态分表，不能修改不可变measurement。

预留采用 remaining_hold 而非简单“成功释放全部”：初始hold=保守上界；入账已知分量后按未决分量上界保留剩余hold；全部分量已明确时remaining_hold=0。预算 `spent + reserved` 中spent是当前已入账事实之和，reserved是remaining_hold之和。每次变更在同事务调整两个差额；已知实际超预留仍全额入账并拒绝后续准入，不把spent限制为≤limit。纯估算不释放unknown敞口，也不当实际成本；费用已知而token未知时只保留token额度，反之亦然。可靠未受理无费用记录零核实证据并释放，不仅修改一枚settled布尔值。

### 未受理后的再次准入

同request最多两次attempt；其reservation固定原budget_period/currency和价格，不因跨月重试搬走已预留的桶。每轮新模型请求才可按当前月份新建预留。旧attempt零核实并释放hold后，prepared请求的reservation处于released、retry_eligible=true；只是可申请，不保证一定执行。

再次派发事务先锁分组/原预算桶，再锁run/request/reservation/attempt，确认最近attempt有可靠未受理零费用证据、无活动/unknown attempt、次数未满，且执行权/同意/期限仍有效。Rearm按固定请求的上界将remaining_hold从0恢复，检查月桶 `spent+reserved+新hold<=limit` 与跨月group总量，递增hold_generation；同事务增加预算reserved、创建下一attempt（保存reservation_generation）、写派发意图。任一失败整体回滚，不生成许可、不增加attempt次数。重复事务凭同generation/request状态回放已记录派发事实，不能领到第二个可消费许可；COMMIT unknown继续核实，不重领。

额度不足时返回budget_exceeded并保留prepared/released状态和原证据，由Harness进入waiting_input展示额度问题（没有未知结果才能等待）；摄影师在原deadline内明确继续时重试准入，未继续到期终结。不新建operation绕过限额，不自动借未来月份。若A预留20后明确未受理释放，B占满20，A重新准入必须失败；B释放后A才可在原身份/generation前置条件下重新占20并派发。Reserve同key仍只回放，绝不承担Rearm语义。

示例：上界20，确认部分成本6、未决上界14，账本spent=6/reserved=14；最终实际9变为9/0。收到重复结果不变。当前成本10，两份并发更正8和7只有一条能从10推进；另一个进入待核实，再从当前值调到7，不能扣到5。迟到费用可以更新已终态run的只读用量投影，不重新执行任务。

## 5. 限流、保留与监测

- Provider并发/速率限制按实际credential/deployment共享域，使用平台专用准入记录与短事务。该记录是基础设施配置/许可，不是无account_id的业务表；业务请求始终账号隔离。多Worker不能各自在内存给同一供应商放满额度。租约到期但请求未知的许可不直接重发；停止本地I/O后可释放并发许可，费用hold独立保留。全局上限与账号限流都须命中。该并发限制约束本平台活动传输，不承诺供应商后台任务已经停止；仅在本地传输确认结束或进程已失效且硬传输期限届满后回收许可，remote结果未知仍不能重新派发。
- 请求保留完整输入只为崩溃恢复，在受控数据库载荷中，绝不记入普通日志。网关输入媒体不复制持久blob；调用方pin直到发送结束，传输已确定结束后释放，unknown对账仅保留必要摘要/供应商ID。结果与消费者保留详见数据契约；完整result用于step消费，与可丢弃流通知区分。
- 观察维度：请求等待/首字/完成时延、adapter错误、unknown时长、限流、预算拒绝、hold余额及年龄、核算分叉、消费保留超期。日志关联request/attempt/caller key，指标不使用资产名或完整提示，request ID不作为高基数指标标签。
- Gateway健康异常只停新模型派发，人工画布/素材搜索继续可用。关闭能力先通过账号/系统barrier停止新派发，再停止队列；不能只隐藏菜单。
- 微服务提取保留相同contract_version与请求身份；跨服务鉴权、持久准入许可、撤销顺序和远程消费ACK替代同库事务后方可切换，见[拆分门槛](../llm-gateway.md#7-从模块到独立服务)。


## Eino模型入口

[Eino接入](../eino-adoption.md)通过GatewayModelAdapter实现固定版本模型接口，Generate/Stream及摘要模型均使用同一Gateway准入/结果/核算协议；WithTools返回隔离配置。Eino默认模型重试、failover和Skill模型覆盖不开放，真实重试由Rearm控制。上下文摘要调用也有持久step/request身份、原始来源外发校验及run总额度，不能成为第二条模型出口。
