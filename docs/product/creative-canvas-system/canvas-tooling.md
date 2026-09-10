---
status: proposed
version: 0.2
created: 2026-09-10
---

# 画布能力工具化与节点执行边界

摄影师要求初版基础架构就面向Agent开放画布能力，包括查看节点、添加节点、请求执行节点，并为后续整个系统的Agent操作保留路径。本稿承接[需求](../../../.codestable/requirements/creative-canvas-foundation.md)与[画布命令](modules/canvas.md)，把该要求落实为能力目录、应用入口及验收，不把它简化为提示词或前端自动点击。

## 1. 界面和Agent共用应用能力

每个画布动作先定义领域查询/命令及输入输出契约，再分别接HTTP/界面操作与Eino Tool Adapter。两者复用同一账号范围、图规则、版本检查、幂等回执和结果读取。Agent写入后，前端经正常快照/回执同步看到真实变化；摄影师在界面修改后，Agent下次读取同一事实。

调用关系：界面动作或Eino工具 → 受信应用入口 → 领域查询/命令/执行请求 → 持久结果。Agent不能从模型输出直接改React Flow nodes，也不通过浏览器坐标完成产品自己的画布操作。框架内部Graph继续负责计算编排，创意画布连线继续表达参考输入。

能力目录按部署版本固定，每项描述：capability_key/schema_version、query/command/execution类别、输入输出schema、支持的节点类型、所需账号能力、目标规则、影响类别、数量/大小限制、是否可撤销、是否可能外发/收费、可见失败原因。权限取当前账号、任务范围、节点/内容用途和Skill许可的交集。目录描述不是授权；执行时重检实际状态。

NodeDefinition在现有type_key/schema_version/config/ports/render/maximize基础上补充：creation_supported、editable_fields、actions、execution_descriptor?。execution_descriptor包含action_key、executor_key/version、input_schema、output_schema、required_capabilities、运行上限和副作用类别。服务端返回effective_capabilities及disabled_reason，界面菜单与Agent发现使用同一结果，不在前端单独猜节点能否执行。

## 2. 查询、命令与执行三类工具

| 类别 | 基础能力 | Eino侧表达与范围 |
|---|---|---|
| 查询 | 画布概览、按范围列举节点、读取节点完整内容/参考输入、查询执行状态 | 增加GetCanvasOverview，复用ReadNodes；GetNodeExecution读取具体执行记录。概览有界分页，只返回本次获准范围内的节点/关系/版本/有效动作，不默认把整张画布内容外发 |
| 创建 | 创建已开放类型的空节点、从资产创建节点、创建文字节点 | CreateNodes为类型目录内的有限创建接口；AddAssetNodes/CreateTextNodes为便捷适配。原始Blob、任意配置或未注册类型不能绕过内容/图校验 |
| 编辑与组织 | 编辑内容/元信息、移动/调整尺寸、连接/断开、打组/解组、复制、移除、撤销/重做 | 全部先有正式应用能力；按明确任务范围注册具名、有限schema的工具，复用画布CommandExecutor。原UpdateTextNodes/ArrangeSelection保留为便捷工具 |
| 执行控制 | 请求节点已声明的动作、查询结果、取消执行 | ExecuteNode/GetNodeExecution/CancelNodeExecution；先看实际节点的可执行动作，不把创建空节点或播放视频叫作任务执行 |

首期保底六种业务工具继续有效，同时补齐画布概览、通用有限创建和节点执行控制适配；注册目录不再以“永远只有六个工具”封闭。编辑/组织的全部基础应用能力在FND-04建立，FND-08按任务范围将其接成具名工具。禁止给模型一个可以传任意方法名/数据库表名的万能调用入口；字段union只来自服务端已注册schema，未注册动作明确拒绝。

GetCanvasOverview的scope来自摄影师明确授权的画布/选区范围，默认当前任务范围；分页每批最多50节点，内容仍按ReadNodes的限制读取。返回别名、类型、结构/版本和可用动作；标题/说明等业务内容也受外发同意控制。没有覆盖所需范围的同意时，先在本地展示范围并取得相应授权，不把“查看概览”当发送全画布的许可。现有selected_revisions只覆盖列明修订，account_library不覆盖任意画布独有内容；未覆盖部分不可先交给模型。

普通任务不需要一次加载所有工具schema。Eino侧按当前任务和节点类型选择允许集；未来目录变大可接受控ToolSearch，但基础能力的可用性/授权仍由应用层判断。所有业务工具与运行时只读工具都计入当前run的工具/模型/时间/费用上限。

## 3. “执行节点”的明确语义

执行某节点是对该类型已声明的action发起一次受控任务。例如未来图片生成、视频处理或策划生成各有自己的executor。创建/编辑节点不会隐式执行；请求执行也不会自动递归重跑所有上游。上游已存在且可用的结果按固定修订作为输入，缺少必需输入明确失败。

首期文字/图片/视频/音频按[节点生成契约](node-generation.md)接入实际generate动作，链接元信息使用文字处理，组Prompt提交范围任务；未注册/未配置动作返回不可用及原因。基础框架必须实现真实的执行分派契约与拒绝路径，并通过只在开发/测试启用的 `internal.text-compose` 执行器验证：读取两个已保存文字修订→生成新的合成文字修订→持久执行结果→有条件应用到目标。这个内部动作不调用模型、不冒充正式生成能力，也不进入生产类型/工具目录。内部执行器不能替代首期FND-13的真实媒体生成验收；3D与完整策划仍后续。

| 应用入口 | 关键约束 |
|---|---|
| RequestNodeExecution | operation_id、node_id/action_key、expected_data_revision、来源读集；事务内核对类型/action、实例配置、输入修订、权限和资源准入，保存独立execution_id、服务器预分配publish_operation_id及输入快照/保留、入队和202回执 |
| GetNodeExecution | 同账号读取该执行的状态、输入/输出摘要、应用状态及失败原因；不依赖Agent聊天消息存在 |
| CancelNodeExecution | 原子撤销该执行的继续执行权，再尽力取消外部任务；已提交效果/可能费用分别保留 |
| PublishExecutionResult | 先持久化输出和必要引用，再根据目标版本/权限/调用方执行权有条件发布；目标已改变保留候选，不覆盖人工修改 |

202只表示执行受理，最终完成与结果是否应用分开。Request的operation回执保持原受理结果；后台发布使用独立publish_operation_id及标准画布change/回执，不覆盖原202。同一execution重复发布回放同一结果。执行身份不同于node_id、Agent run_id及某次供应商request_id；重试同operation返回同execution，不能每次工具调用创建新任务。执行动作不能混入普通画布批次事务持锁跑网络；外部I/O走Worker，发布结果才进入短事务命令。

## 4. 执行记录、恢复与Agent联动

节点执行由画布执行应用边界拥有，使用类型化executor registry调用所属模块。记录至少包括：account_id/execution_id、node_id/action_key/executor_version、operation_id/hash、输入快照与明确revision refs、目标前置条件、state、execution_epoch/lease、deadline、caller_kind、可选caller_agent_run_id、result refs、apply_state/实际change定位及费用请求关联。新业务表保持显式key无外键；全部执行输入/未采纳输出列入GC根清单。正式字段/schema/状态转换及事务细节在FND-04内部执行器切片完成，并在开放执行入口前审查验证。

状态至少表达queued/running/succeeded/failed/cancelled/reconciling，apply_state独立表达pending/applied/conflicted/discarded/expired；succeeded只表示完整输出已保存，不能仅凭它声称画布已经更新。实际外部效果未知时只核实，不盲重发；异步生成类executor的完整供应商/费用协议在首期FND-13及后续新增executor接入时验证，内部文本执行器没有外部unknown。

- **人工发起**：执行独立于Agent会话；关闭聊天不会中止它。界面、工具可查询相同execution，但取消仍需当前权限。
- **Agent发起**：首期固定为attached执行，在请求创建时校验当前Agent执行权，并持久绑定caller_agent_run_id与execution_id；不默认创建脱离原任务的后台执行。Agent进入等待执行结果时保存pending execution关联和可恢复Eino checkpoint，失效旧worker lease/epoch，转queued并同事务登记有界延期查询任务，保留本run写slot；不会占用一个持续运行的Worker，也不让模型反复轮询。节点执行器使用自己的执行权，不再抢一个Agent slot。Agent worker接管轮换epoch不使已登记attachment失效，取消/终态才撤销其自动发布资格；发布端遵守统一锁序（涉及Agent时先slot、再run、再canvas），核对父run处于queued/running、未取消/未超期、仍绑定该execution，且账号slot仍由该run与当前claim_token持有，再核对节点执行自己的epoch/lease。子执行从锁内当前记录核对绑定，不复用旧Agent worker凭证或抢占另一slot；waiting_input/waiting_apply/reconciling及终态均禁止自动发布。
- **取消/终态**：Agent取消/超期/终结先阻止后续节点执行及结果自动应用，再按绑定关系取消attached执行。迟到输出可作为未应用结果保留，不能在原run结束后继续改画布；已在取消前提交的效果保留。人工执行不受无关Agent终态影响。
- **版本冲突**：发布时重新核对目标/来源读集，纯布局变化是否相关沿用画布版本规则；数据/输入变化不能被“模型已完成”覆盖。新结果成为可比较候选，重新应用是明确的新操作。
- **等待而不假成功**：ExecuteNode回执是accepted，工具结果携带execution_id/state；ResultCheck必须检查最终执行与apply_state。若原Agent期限内无法完成，显示待处理/部分结果并按attached规则结束，不假称已执行完。

第一版不增加Agent run状态枚举。等待已知queued/running或succeeded且apply_state=pending的节点执行时，父run使用queued并保留slot，记录wait_reason=node_execution及pending_execution_id；到期扫描和取消仍能关闭它。查询任务按持久关联幂等重投，queued→running先取得新epoch，然后查询同一execution；尚未完成则有界延期回queued，不产生新的模型/工具调用。输出成功但应用pending时继续有界延期，不恢复模型、不结束父run，也不释放slot；applied且效果承接已提交后才从兼容checkpoint恢复。conflicted须先记录候选和停止该execution自动发布，解除pending绑定后才能按提案协议进入waiting_apply并释放slot；discarded/expired或执行failed/cancelled解除pending绑定并按实际已交付效果结束或报告失败，不记作完成。所有等待均受原deadline限制。只有存在未知外部效果才进入reconciling。waiting_input仅用于确实需要摄影师补充，不冒充自动等待。缺checkpoint或一致接续证据时遵循Eino保守恢复出口，停止自动续跑并保留实际结果。

**异步发布属于父run的实际效果。** 受信事务编排先取得publish_operation的幂等串行边界，再沿统一领域锁序校验当前slot/run与子执行。画布变更、publish回执、execution的apply_state、父run的执行效果关联、execution_read_set承接、artifact/消息查询投影及持久事件在同一事务提交；变更带父run_id并归入其change_group_id。原ExecuteNode工具步骤保存accepted与execution_id的关联，发布作为该执行的独立效果记录，不覆盖受理步骤/202，也不伪造第二次模型工具调用。

读集承接复用Harness自身命令协议：仅从本次publish回执的object_results，且旧执行读集与publish前置条件链吻合时，更新实际受影响字段组/拓扑水位；初始run_inputs不变。不得读取“最新节点版本”替代自己的结果版本。发布后人工修改会使后续命令出现真实冲突；重复消费同一publish回执按效果身份回放，不能重复承接、重复加入撤销组或把读集回退。父worker恢复时只从已提交效果水位构造执行条件，不能拿旧checkpoint覆盖业务读集。发布失败不推进未发生的效果水位。节点生成按[node-generation](node-generation.md)区分历史登记与采用：资格有效但目标版本冲突可登记未采用历史，其实际效果须记录；未改变的当前内容读集不推进。资格失效时只保留execution候选。

## 5. 全系统Agent操作的演进原则

画布先建立可复用模式，个人库、独立策划、CRM等未来模块按相同方式提供能力目录、查询/命令/任务、权限、回执和运行结果。Agent通过Skills组合这些业务能力，界面始终可看到、接续和按规则撤销实际变更。

“接管系统”表示在摄影师授予的任务范围内完成跨模块工作，不等于创建另一个写数据库的通道。只读、低影响可逆编辑可直接执行；涉及覆盖、发布、外发、费用或不可逆结果时，按具体影响形成可检查的提案/许可。范围一旦已明确，不逐步重复确认。此次仅建立可演进契约，CRM写入、系统账号设置、支付不因愿景自动开放；生产生成执行器按已新增的FND-13明确范围交付。

## 6. FND验收承接

- FND-01：能力目录契约/版本与两种调用方适配原则；不以Eino能调用普通函数就宣布画布工具完成。
- FND-04：所有基础画布应用能力、NodeDefinition有效动作与执行器registry、未注册执行拒绝、内部文本执行的真实任务/结果/有条件发布；无前端直写旁路。
- FND-07：Agent与attached节点执行的queued等待/期限/取消/slot联动按本契约实现并验证后才能开放ExecuteNode；缺协议时能力保持未启用，不假装依赖已满足。
- FND-08：Eino通过同一应用能力完成查看→创建→连接/组织→请求执行→核对结果的内部类型端到端验收；并验证未注册/未配置动作不可执行、重复请求同一execution、取消/目标变化不产生迟到覆盖；输出已保存但发布pending时仍等待，发布→继续编辑→整组撤销完整，发布后人工修改和重复消费回执不污染读集。
- FND-10：节点执行输入/输出/候选与其他GC根一起验收；删除节点不直接物理删除其他合法引用的执行产物。

首期FND-13及后续可执行类型上线时须有实际executor、效果/费用恢复及上述契约测试；注册一个空工具或返回演示成功不能算验收。

节点Prompt草稿、data/status读模型、真实生成与长期结果版本规则由[node-generation](node-generation.md)补充；UI和Agent统一调用，生成的生产验收在FND-13，不能以内部fixture替代。
