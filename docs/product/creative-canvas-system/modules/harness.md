---
status: proposed
version: 0.4
created: 2026-09-10
---

# Agent Harness 模块设计

承接 [Agent = LLM + Harness](../agent-harness.md) 与[需求 AG-01–08](../../../../.codestable/requirements/creative-canvas-foundation.md)。运行状态机仍以[data-model §7](../data-model.md#运行状态机唯一权威)为唯一权威；本稿细化步骤和接口，不新增一套run状态。

## 1. 内部边界与端口

`internal/creativeagent` 对外提供 ConversationApplication、RunApplication、EventReader；内部包含Eino ADK执行适配、ContextBuilder、ToolRegistry、SkillRegistry、ExecutionController、RunStore。这些是职责与测试边界，不要求七套互相转发的service/repository。

| 端口 | 深接口契约 |
|---|---|
| ConversationApplication | 创建/列举会话、分页消息、附件草稿、外发同意；绑定同账号project/canvas且验证对应关系 |
| RunApplication | 创建、补充、采纳、取消、结束核实等待、恢复已记录步骤；负责HTTP幂等与状态机，不由Worker自行创建第二个run |
| ContextBuilder | 将选区、上游、附件、历史及工具结果转为版本化输入清单、引用别名和真实读集合；返回范围/能力说明，超限显式失败 |
| SkillRegistry | 按key/version/digest加载不可变平台包、校验输入与完成标准；运行固定快照，不自动升级；映射为Eino Skill Backend的List/Get |
| ToolRegistry | 查询注册工具schema、影响级别、授权边界、版本；只分派白名单内的已校验请求 |
| ExecutionController | 将账号能力、slot/epoch、deadline、同意/用途、命令读集校验纳入同一事务；保存实际效果和步骤事实 |
| Harness.Advance | 应用驱动器核实持久step/执行权后调用Eino Runner.Run/Resume；通用循环交ChatModelAgent，模型/工具分别经Gateway和领域适配，不再自写竞争循环 |
| EventReader | 从已提交事件/消息读取快照或SSE批次；断线不影响运行，不代替RunStore |

依赖方向：creativeagent → Gateway、creativecanvas、creativelibrary、creativecontent、creativemedia、jobs。Gateway和画布不反向依赖Harness；它们只接受受信调用上下文和事务端口。事务编排入口预取并排序锁key，所有 InTx 函数禁止重新Begin或反向获取高层锁。

## 2. Skill 与 Tool v1

首期部署一个正式版本化Skill `reference-direction@1`（整理参考与创作方向），以及无专项Skill的普通对话模式。普通对话并非伪装成另一个Skill，默认只读；明确画布编辑请求通过已定义动作授权才可获写工具。第三方Skill安装、任意脚本、shell、MCP任意工具、CRM写入不开放。Skill目录经List只发现元信息，Get激活固定版本正文；包内只读资源按需加载，首期inline，模型覆盖/fork不自动启用。

Skill package包括manifest_schema_version、key/version、digest、展示名/说明、输入类别/数量、instructions、tool_allowlist及各schema版本、required_model_capabilities、输出契约、limits、completion_check_key。包由平台随部署发布，启动时校验并拒绝重复key/version不同digest；活动运行保存完整无秘密包快照。升级新增version；停用版本禁止新run，安全撤销则通过能力barrier阻止旧run后续步骤并保留产物。

`reference-direction@1`要求明确主题、可搜索范围、目标画布和放置区域。流程为检索→读取候选→比较→生成一个原子AddAssetNodes+CreateTextNodes提案/命令→核对来源与数量。数量不足如实交付已有结果并说明缺口；成功标准验证实际新增节点、固定来源修订、方向文字和回执，模型说“完成”不能替代检查。一次“找三张图并写比较”尽量在一个change set落地，天然是一条撤销单元。

下表保留最初六种业务工具的便捷接口；初版完整工具入口按[画布工具化](../canvas-tooling.md)扩展概览、有限通用创建、图编辑与节点执行控制，六种不再作为工具数量上限。所有入口复用注册应用能力与运行限额。

| 工具与schema v1 | 输入 / 输出 | 写范围与并发 |
|---|---|---|
| SearchAssets | q、kind、group、tag IDs/all-any、limit≤20；返回asset_ref别名、标题/标签、实际修订、摘要/截断标志、库水位 | 复用库搜索，默认活跃视图，不搜索回收站；结果外发须覆盖account_library或明确修订授权 |
| ReadNodes | 本次选择/已授权图范围内的node_ref≤20；返回实际内容修订、类型、参考关系、data/placement版本及读取说明 | 只读，不允许凭模型猜ID扩大范围；读取完整文字或明确报告超限 |
| AddAssetNodes | 已登记asset_ref、目标区域、位置；返回节点别名与真实命令回执 | 只可用实际查到/摄影师选定的资产；原子验证asset_revision/current revision和画布拓扑 |
| CreateTextNodes | 标题、正文、目标位置/父组；返回节点与内容修订 | 内容模块创建新内容和声明，画布建引用；与来源输入建立可追溯记录 |
| UpdateTextNodes | 明确node_ref与新正文；返回实际结果版本 | 只改本次明确授权的文字节点；期望data版本来自服务读集，模型不能自报latest |
| ArrangeSelection | 固定选区别名、布局枚举row/column/grid与间距/目标区域 | 只移动和必要时按明确指令Group/Ungroup，保持世界坐标；无删除、无范围外节点写入 |

工具可组合成一个类型化画布change set；不提供任意命令名/SQL执行入口。模型参数中资源只用run-local别名；服务端映射到已验证的真实ID，前端结果可展示真实来源。模型结果消费先保存工具计划及依赖顺序，尚不为后续依赖计划冻结最终命令hash；轮到该工具且前序结果已知时才prepare实际step/operation及完整前置条件。已prepared的命令永不改hash或前置条件。搜索新发现的别名只证明读过，不自动允许任意编辑该资产。

每个工具定义 effect_class(read/create/update/layout)、allowed_target_rule、schema、max_impact、required_capabilities、output_contract。ExecutionController用摄影师的明确动作授权、Skill许可和工具声明的交集：添加新节点/指定文字低影响更新/整理选区可直接执行；含糊范围、批量覆盖已有内容或范围外影响形成完整提案等待采纳。运行新增输入或新指令不能由模型自己批准。保存提案含规范化命令、目标、预期版本、受影响清单、来源读集、operation_id和hash；变化后的采纳必须生成新提案，不修改旧hash冒用旧operation。

## 3. 上下文、选区与授权

前端选区是待发送引用。发送前若选中节点有本地未保存内容，先完成保存并使用回执版本；保存冲突则保留草稿，不能将本地内容伪装成服务端快照。运行创建冻结当前选择，此后移动选择不影响run。

创建事务前有界预查，事务内重新核对快照：组展开后去重；从选择节点按输入端口解释上游，动态node_inputs与参考边使用相同图规则；记录拓扑与每个来源的data版本，布局相关工具另录placement闭包。显式输入总量最多50节点，自动上游最多2层，总量仍≤50，超出返回上下文范围错误及实际计数，不能悄悄只读前50个。空节点只包含元信息；未知类型只读支持的摘要，写工具拒绝。

`input_manifest`列出每项来源、role、读取级别、截断原因/范围，run_inputs保存完整不可变文字、节点配置快照和必要content_revision key；引用来源ID允许之后被删，授权/媒体可用性仍每次检查。正文/素材描述/工具返回属于数据层，不能变成system指令或扩大白名单。历史只选最近有界的已完成消息，工具调用与工具结果成对；活动消息不混入另一次run。历史summary若使用也固定版本并保留来源，不把生成概括当原始事实；摘要/工具结果卸载改由Eino中间件受控接入；摘要是派生上下文，原始输入和业务前置条件不变，所有辅助调用仍经Gateway计量。阈值/保真验证前摘要默认关闭，详见[Eino上下文](../eino-adoption.md#3-上下文管理)。

模型输入按能力组装：图像发送获准预览且read_level=image_preview；视频/音频read_level=metadata_only，输出明确未观看/未聆听；URL只发送保存的文字，不抓页面。每次请求包含的消息、附件、素材元信息和工具结果都重新核对外发同意。selected_revisions同意不能覆盖新发现的其他资产；若工具搜索结果超范围，停在waiting_input，只向摄影师展示本地可见结果和授权缺项，不先把结果交模型再补授权。

同意由账号主动授予，绑定供应商/用途/会话及selected_revisions或account_library模式；不由LLM生成。新增范围创建新的consent，原run不热替换consent ID。需扩大授权时保存待补充事实并结束/另建明确后续run使用新同意；原输入与已产生效果可见，不从头静默重做。waiting_input同run补充仅允许原同意已覆盖的消息/修订，固定补充记录、追加输入批次；不改历史快照。用途撤销与派发共用barrier/用途行锁，撤销先提交则不外发。

### 不可变输入与可承接的执行读集

run_inputs/input_manifest是历史事实，永不更新为最新节点内容。另在run持久保存有schema的execution_read_set与依赖水位，作为下一条尚未prepared命令的并发前置条件；初始化来自实际读取版本，字段组/拓扑与[画布版本协议](canvas.md)相同，不赋予新资源权限。

本run的写命令提交时，原子保存receipt与execution_read_set承接：只更新该命令确实改变的对象字段组，必须证明旧执行条件与命令自身preconditions链相接，并使用receipt.object_results的实际结果版本；拓扑只在before_topology_revision等于执行水位且命令实际改了拓扑时承接result_topology_revision。未受影响项不动，删除项标不可继续引用。新节点的版本/别名从真实receipt登记；同run效果后的新正文可作为独立工具结果/输入批次进入下一模型轮次，不能修改初始输入。

例：初始拓扑7；A添加节点回执7→8，下一条未prepared的B只能使用8。期间人工改变为9，B冲突；不能把最新快照9写回执行条件。A只改文字、返回看到的拓扑9时也不能推进旧结构水位7。相同规则适用于node.data/placement与asset来源：只承接自身真实改变的字段组，不能以命令成功洗掉未参与命令的来源变化。

已prepared的B、已展示待采纳artifact及终态恢复的旧命令都保持原operation/hash/preconditions；自身承接不重写它们。必须重新计划时生成明确新提案/新步骤，不能冒用旧操作。Worker接管从持久receipt与执行水位恢复，重复消费同receipt不再次推进。独立读工具发现外部变化时先报冲突/待确认；明确新任务可以读取新输入，不在旧写计划中静默rebase。取消、用途与对象状态检查仍在每次提交执行。

## 4. 独立附件

附件不要求先收藏到个人库。`agent_attachment` 是媒体模块的新增类型化上传目标：先创建同会话open draft，再按draft_revision上传。成功发布创建 `creative_agent_attachments` 与明确内容修订引用，draft内新增附件并递增revision；发送消息在同事务建立message_refs/run_inputs后把附件置attached并清空临时修订根，保存目标消息身份。取消/过期释放临时根，发送与过期锁同一draft，只能成功一方。

草稿7天到期、最多10个附件，发送只能引用同账号同会话draft中的ready附件；过期不自动收藏，摄影师可在期限内显式存入库。目标变化、draft已发送/到期时沿用媒体pending候选，摄影师可将候选重新绑定到新draft或存入个人库；后台不擅自插入已经发送的消息。元信息/权限/codec仍走媒体模块，预签名地址不成为附件身份。加号只负责附件，选区引用无需点击加号。

## 5. 有界执行与恢复

run默认5分钟、最多12工具调用和13模型轮次；只读调用、Skill加载/ReadSkillResource/ReadRunResult及非法模型工具尝试都消耗相应次数；摘要等辅助模型调用也计入13次总模型调用，不用参数修复绕过步数限制。模型每轮最多4个工具请求，总量受run上限约束；一个原子change set可含多种写动作，但其工具调用数量和影响实体数都受限。每轮请求deadline不得超过run deadline。活动租约30秒，每10秒续租；以数据库时钟/CAS为准，不依赖浏览器心跳。

1. **创建**：账号能力→slot→预算/库根→会话/run→来源与授权依次加锁；保存trigger消息、输入根、模型/Skill快照、初始预留、slot、queued事件及EnqueueInTx，任一步失败整体回滚。202回执只表示受理，不会被未来结果覆盖。
2. **准备模型步骤**：固定step ID/ordinal与call_operation，持久精确输入、Gateway请求与消费者保留。每个run同一时刻最多一个待模型结果；重复Worker不创建新的轮次。生成operation后不得混入epoch。
3. **派发**：按数据协议提前锁Gateway请求，再核对当前run epoch/lease/cancel、同意/用途、发送集合、限额，原子写attempt派发意图；COMMIT成功后事务外发送。
4. **结果消费**：Gateway完整结果持久后，短事务将assistant消息、tool plans与步骤结果保存；唯一 `(model_step_id,tool_call_index)`防止重复展开工具。释放Gateway消费者保留。epoch已失效则只登记待恢复的结果，不应用工具；终态迟到结果仅诊断/费用，不发新业务消息或写画布。
5. **工具执行**：外层先取原operation幂等锁，再slot/run与领域锁；合并上下文读集与命令读集，校验每次效果的权限/取消/版本。领域效果、change、operation receipt、step结果、artifact标记、message投影和事件同事务。COMMIT unknown先查原receipt；不能因River重试换ID重写。
6. **继续/结束**：完整工具结果回填下一轮模型输入，重新做外发校验和预留。确定需要补充/采纳时保存事实、失效epoch并释放slot，不挂起一个长期Worker。停止原因、已交付清单、未完成步骤、费用状态分别呈现。纯自然语言回答也必须有持久完成消息才算交付。

结果引用使用run_input/artifact别名，不能让模型编造真实ID。工具输出要么明确success+receipt，要么带稳定code的失败；reply“已添加”但无实际receipt由ResultCheck降为失败/部分完成。一个工具拒绝后允许模型在剩余预算内解释或修正非权限性参数，不允许通过新ID/放宽版本绕过冲突。

Gateway将unknown核实为未受理并返回prepared/retry_eligible后，Harness沿用原model step、call_operation及request恢复prepared步骤，再尝试Rearm；不生成下一模型轮次，也不把永久failed当可恢复。额度被其他调用占用则保存waiting_input及预算原因，无新attempt；明确继续或到期按原状态机处理。

同run接管遵循slot/run epoch：救援不能仅因lease过期把slot给另一run；先识别prepared/已提交工具/unknown模型，再queued或reconciling。取消事务立即失效epoch，未知供应商结果仍核实，deadline或“结束等待”后释放slot，Gateway继续对账。消息/SSE连接断开不取消运行。

终态重试只承接已有步骤，详见[data-model恢复契约](../data-model.md#7-agent-会话运行和工具回执)：成功回执只展示；可证实未执行且在恢复窗口的步骤复用原operation与输入；未知必须先核实。创建新任务与恢复是不同动作。复制已生成文字为新节点是明确新编辑，不是假称恢复旧operation。

## 6. 结果采纳和撤销

run_artifact保存pending/applied/discarded/expired状态、完整提案和前置条件，以及所有涉及内容的显式refs。采纳分为apply_only与apply_and_continue：apply_only在执行期限内临时取得slot，经同一InTx写工具路径原子应用、保存终态并释放slot，200代表已经落库，不再调用模型；apply_and_continue固定待执行指令、取得slot后queued，202仅表示已受理，Worker再次检查期限后应用并执行明确的剩余步骤。两种模式共用冻结planned_operation。同步模式外层按key排序同时取得control operation与planned operation幂等锁，回执分别记录控制动作和实际领域结果，效果/step/artifact/终态/两份回执同事务；失败全部回滚，保持waiting_apply。异步模式队列延迟至deadline后不得写入，终结并保留可见提案；不能把202展示成已应用。重复采纳回放原结果；冲突仍保留原提案可比较。明确重拟产生新artifact和operation，不把最新版本填回原提案自动覆盖。

一个run多个change用change_group_id关联；整组撤销沿用画布的逻辑状态链与闭包检查，工具结果消息保留“该变更已撤销”的查询投影，不回滚历史费用/对话。run终态不会因撤销重新打开。未采纳产物90天保留，但run执行期限后不能直接在原run采纳；允许摄影师明确将其作为新人工命令/任务输入，重新校验所有状态与用途。

## 7. 首期默认资源限制与演进

服务端 limits_version 与每次run一起固定：输入文本总UTF-8≤256KiB（含历史/Skill/工具定义），单消息正文≤32KiB，最多50输入节点/10附件，单模型完整结果≤256KiB，单工具参数≤64KiB、工具结果≤64KiB、每轮≤4调用。媒体原文件上限沿用媒体模块；模型图片预览每张≤2MiB、每次≤8张，总≤12MiB；context_tokens与output_tokens还须服从所选模型和预算配置的更小值。超限响应给出限制维度和实际值，不能以截断结果继续执行写工具。

这些是可配置的开发基线，不是已测容量；模型适配若更小会在目录体现。步数/时间调大须更新配置版本和回归基线。Token/金额数值以实际接入供应商测试冻结，不硬编码展示不存在的模型价格。Skill机制未来可以承载策划，工具可以增添生成/CRM，但需各自权限、幂等、产物保留与验收；不在generic节点payload提前放镜头表。


Eino v0.9.19的具体适配、Checkpoint/context记录与同事务回执、恢复证据不足时的停止出口，统一见[Eino接入契约](../eino-adoption.md)。当前八项隔离实验只证明固定轨迹/接口接缝；不会把一段Runner.Query当作任意崩溃下安全重放。


节点执行工具的受理不代表创作完成：持久关联execution_id，已知任务待完成期间父run转queued、保留slot并延期查询，节点Worker不重复取得Agent slot。取消/终态限制attached执行的结果自动应用。具体等待/发布/回执协议见[节点执行联动](../canvas-tooling.md#4-执行记录恢复与agent联动)，run状态机变更同步于data-model。

attached节点输出成功但apply_state=pending仍按queued持有原slot等待；自动发布仅允许当前queued/running且slot归属匹配的父run。异步publish在同一事务记录父run效果、按自身回执承接execution_read_set并归入change_group_id；旧checkpoint不得覆盖已提交效果水位。完整状态分支与守卫见[画布工具化契约](../canvas-tooling.md)。

## 节点Prompt调用方补充

普通节点Prompt直接走execution应用边界，不创建伪Agent会话；组Prompt走同一Harness的明确组范围任务。FND-08新增查/改Prompt、查版本/采用版本工具并接入FND-13真实生成，沿用attached期限/slot/恢复/效果承接。生成所需期限超过父run剩余期限时不得偷偷延长或脱离任务，按[node-generation](../node-generation.md)拒绝并允许摄影师从节点独立发起。

组Prompt的SubmitGroupPrompt须同事务创建消息/run_inputs/run/job和group_prompt_submission受理关联，active_agent_run_id保证组级幂等/单活动任务，状态与停止直接路由真实run。删除/解组提前按slot/run→canvas顺序预锁并复核，取消写入资格与结构修改同事务，不能在canvas锁内回头取run。具体草稿授权、状态投影与恢复不重启语义见[node-generation](../node-generation.md#组prompt的任务身份与停止)。
