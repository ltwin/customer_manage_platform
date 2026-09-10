---
status: proposed
version: 0.1
created: 2026-09-10
---

# 节点内容、Prompt Box 与生成版本

本稿落实摄影师的新范围：节点承载生成结果，每个节点有Prompt Box，媒体生成结果可反复生成并切换版本。它修订此前“生产生成后续接入”的范围，以[需求](../../../.codestable/requirements/creative-canvas-foundation.md)为准。首期规划文字、图片、视频、音频真实生成，每类至少一个经验证的模型；3D、完整策划、复杂剪辑仍后续。截图只作为提示词面板、引用缩略图、模型/参数/生成入口的交互参考，不承诺截图中的模型、价格或参数。

## 1. 核对既有设计与节点契约

原有creative_nodes已有类型、标题、坐标/尺寸、parent_id、config与content_revision_id；creative_edges独立存参考边，父节点必须为同画布组，子节点相对父组定位。已有不可变内容修订和执行任务契约，但尚缺统一status投影、节点生成草稿，以及独立于撤销期限的作品版本列表。本次补齐这些缺口，不另建一套内容/文件存储。

节点读取契约明确分为以下部分（语义示意，正式DTO仍从OpenAPI生成）：

```text
Node
  id / canvas_id / parent_id?
  metadata { type_key, type_version, title, intent, x, y, width, height, z_order }
  data { schema_version, config, content_id?, content_revision_id?, selected_version_id? }
  status { content_state, generation_state, active_execution_id?, latest_execution_id?,
           apply_state?, progress?, error?, status_revision, active_agent_run_id?, agent_task_state? }
  prompt { draft_id, draft_revision, action_key, text, model_key?, parameters, reference_summary }
  capabilities { actions, prompt_mode, disabled_reason? }
  placement_revision / data_revision
```

- `data`按类型验证：文字为完整文字修订及编辑配置；图片/视频/音频为不可变内容修订、媒体摘要与展示配置；链接为URL/标题/描述修订；组仅有分组配置。大正文按明确修订读取，字节在对象存储，资源引用不只藏在JSON里。
- `status`是服务端事实投影，客户端不可PATCH。content_state为empty/ready/unavailable；generation_state为idle/queued/running/succeeded/failed/cancelled/reconciling，来自节点当前/最近执行。已有作品ready时可以同时生成running或failed，不能用一个互斥状态丢掉旧作品。
- `progress`允许为空；供应商没有进度就显示运行中，不伪造百分比。status_revision单独递增；进度/状态通知不增加data_revision、不进入撤销历史，否则任务会与自己的进度发生版本冲突。
- metadata分组只是API表达，数据库继续显式列；data_revision覆盖实际内容/生成配置等创作状态，placement_revision覆盖布局。prompt草稿有独立版本；保存草稿不更改当前作品，也不使正在运行的输入快照漂移。
- `edges`仍是独立参考关系；`parent_id`只表达空间组织。组有多个直接孩子、每个孩子最多一个父组；禁止父链环。组关系不等于参考边，不自动生成或执行整棵子树。

## 2. 每个节点的Prompt Box

Prompt Box属于节点，通过选择节点显示在其下方并跟随节点；可展开编辑，取消选择后收起但保存草稿。它包含参考缩略图/引用、提示词、可用模型、类型专属参数、生成/停止与版本入口；生成状态在节点卡上也可见。保持暗色玻璃交互语言；此稿定义行为，不继续打磨原型像素。

| 节点 | Prompt Box行为 |
|---|---|
| 文字、图片、视频、音频 | 直接请求本节点注册的generate动作；无需先去Agent聊天。空节点可生成，有内容节点生成新版本；模型列表按输出类型、输入能力和账号权限过滤 |
| 链接 | 可用提示词整理手填标题/描述，保留URL；不自动抓取网页，不谎称理解远程页面。输出形成链接内容的新修订 |
| 组 | 描述组内创作任务，展开并展示明确成员范围，提交给已有Agent Harness；它是组范围任务，不把组变成媒体输出，也不因点一下而递归生成全部孩子 |
| 未实现/未知扩展类型 | 保留统一面板入口，显示当前只读或能力未开放原因；不接受未知配置或伪造生成成功 |

首期3D/专业节点未正式开放，不因统一面板补建编辑器。组任务复用Agent模型/Skill/上下文上限；普通节点生成使用该动作的生成模型目录，不能把右侧chat模型机械复用成图片/视频模型。没有已配置模型时保留草稿、显示缺失原因；配置缺失不能算对应类型验收完成。

参考来源包括可见连线、节点内显式参考选项和Prompt Box添加的独立附件。所有引用具有明确角色和位置；`@`只是引用别名，不能接受模型编造的ID。连线与面板引用分开记录并在提交时合并、去重，冲突角色明确拒绝。独立附件使用`node_prompt`上传目标，绑定draft而非偷偷收藏；发送时固定到execution输入，删除草稿不释放已被任务保留的内容。

点击生成先确认草稿保存成功，提交draft_revision、目标data_revision和来源读集；服务端冻结提示词、模型/参数版本、实际参考修订、顺序/角色、用途和外发同意。编辑草稿仅影响下一次提交。权限和授权已覆盖时不反复弹确认；新增参考、供应商或用途须真实校验。Prompt文本授权绑定该draft及冻结文本，不能借用无关Agent会话同意；外发同意新增node_prompt_draft_id显式key，见data-model。

### 组Prompt的任务身份与停止

组Prompt使用SubmitGroupPrompt应用入口，输入operation_id、group_id、draft_revision与明确选区/成员读集。按已有账号slot/会话run→canvas→group/draft锁序，在同一事务建立真实Agent消息、run_inputs、任务/初始预算/入队及group_prompt_submission关联、受理回执；失败整体回滚。关联保存account_id、group_id、draft_id/revision快照、run_id、operation/hash与固定成员/参考输入清单。UNIQUE(account_id,operation_id)回放同run；组的active_agent_run_id在组锁内验证为空，已有非终态run（含waiting）时新operation拒绝。组不创建伪node execution，active_agent_run_id与active_execution_id互斥。

组状态通过该关联读取Harness真实run状态，generation_state保持idle，面板通过agent_task_state显示queued/running/waiting/终态；刷新后仍能定位任务。停止路由到同一run的CancelRun，不能查无关的node execution；终态清空active关联并保留最近submission供查询。Prompt固定引用和成员快照交接到run_inputs，仍使用同一外发/用途检查；消息正文与组draft授权需明确匹配，不将draft同意冒充整个会话许可。组draft中的选区/参考在提交时有界展开为run输入，不生成以组为端点的参考边，不改变组无内容端口的规则。

删除组或UngroupNodes之前，命令预读活动submission并按slot/run→canvas→group/draft次序预取锁，锁内复核关联未变；发现遗漏活动run则释放并重新规划，不持canvas锁反向取run。取消关联run/attached子执行的写入资格与删除/解组同事务提交；已提交效果保留，解组后的孩子保持原内容且不再接受旧组任务的后续写入。恢复组不会重新启动旧run。普通成员调整不会重定向已冻结任务，仍由实际节点/父链/拓扑前置条件识别冲突。以上关联/输入引用纳入FND-07/08与10，组无独立媒体版本列表。

## 3. 版本：作品结果、内容修订、任务彼此独立

一个execution表示一次生成意图，一次可产生多个output；每个验证完成的output形成一个节点结果版本，引用一个不可变content_revision。失败请求保留任务记录，不生成一个空白“成功版本”。再次生成创建新execution；网络重试沿同operation查原execution。再次生成不是覆盖旧文件。

节点有独立版本列表和selected_version_id。生成版本保存不可变prompt/model/parameter/input快照的生成说明，浏览历史不依赖可能先清理的聊天、执行载荷或撤销日志。供应商request/execution ID是历史定位，不作为读文件的授权来源。

| 操作 | 语义 |
|---|---|
| 浏览版本/大图预览 | 仅改变当前浏览器的预览位置；不改节点当前输出、下游输入、Prompt草稿或data_revision |
| “用于节点”切换版本 | 具名SelectNodeVersion命令，校验账号/节点/版本及expected_data_revision；原子更新selected_version_id与内容绑定，递增data_revision，产生回执，可撤销 |
| 首次导入或替换媒体 | 同样登记一个import版本；普通文字编辑仍按原修订/撤销规则。清空当前内容只解绑selected，不隐式删除历史版本 |
| 再次生成 | 使用当前草稿/明确参考创建新的输入快照，旧版本不变。复用历史参数用显式“复用参数”命令填充新草稿，并重查引用可用性与外发授权 |
| 复制节点 | 默认只复制当前采用内容为新节点自己的一个版本，复用不可变字节；不复制活动任务或全部历史。复制图结构仍按既有规则 |

动态连线在新执行提交时解析上游节点当前采用的版本，执行期间固定修订。浏览历史不改变下游；切换采用版本后，尚未提交的下游任务读取新版本；已提交的任务仍用原快照，应用时按读集处理变化，不自动重跑。固定位点引用始终读所选修订。

首期每个非组节点最多一个尚未结束的执行（含发布pending及外部结果未知），用画布/节点锁和active_execution_id维护，不借Agent账号slot限制人工生成；同operation回放，不同operation返回409并带当前任务。不同节点可并行，受独立账号生成并发/预算限制。一次任务多个输出按稳定output_ordinal顺序编号，默认只尝试自动采用第一个成功输出，其他保留候选；首期UI默认一次一个结果，供应商批量能力须单独验证后开放。

输出已生成时先通过媒体校验/持久存储/配额交接。只有当节点还存在可写、任务未取消、仍拥有active_execution_id、目标/来源前置条件吻合且当前采用指针未被人工改变，才自动采用。生成期间切换历史版本或改内容会让自动采用冲突，成果仍在历史中标记“未采用”，不会突然切回新作品。取消/到期先撤销自动写入权；迟到结果只能进入执行候选保留区，不能在终态后修改节点或版本列表，之后可由摄影师明确保存为版本。

**登记历史与自动采用分别校验。** PublishExecutionResult先判断节点存活/可写、同账号、结果ready、生成发布许可和当前任务资格（Agent还检查父run/slot）。资格仍有效时按execution/output身份幂等登记结果版本及长期输出/输入根；目标或来源数据已改变不会单独阻止这一步。然后检查采用所需目标/来源读集与selected指针：吻合才切换采用并推进data_revision，否则同事务提交已登记的未采用版本与conflicted结果，而非回滚历史。取消、删除、停用或期限使发布资格失效时仍只留execution候选，不能借此继续改节点历史。

发布回执分别记录registered_version_ids和实际是否采用；未采用历史登记也是实际效果，Agent来源同事务登记父run效果与change_group_id，但不推进没有改变的节点data_revision/内容读集。重复publish回放同一回执，不重复版本。撤销按实际效果闭包处理历史成员及采用变化，其他合法引用仍保留内容。原受理202不变；只有已登记版本的长期根才独立于执行候选到期窗口。

应用状态沿用[工具化契约](canvas-tooling.md)：生成成功不等于已用于节点。Agent发起时采用其attached等待/取消/slot与原子效果承接，选择版本同样是可授权工具；人工Prompt执行独立于Agent，不需创建伪聊天run。不同输出数量不足或部分落库时展示实际结果和失败明细，不把部分结果汇总成全部成功。

## 4. 表与事务责任

以下为本轮新增的逻辑表/字段契约，FND-04/13形成正式DDL/OpenAPI及并发测试。沿用account_id+显式key无外键，业务事务负责归属和存在性；不以JSON关联替代引用登记。

| 记录 | 关键字段与约束 |
|---|---|
| creative_nodes扩展 | selected_version_id?、active_execution_id?、latest_execution_id?、status_revision；status按执行和内容事实投影。当前selected必须属于同账号同节点，且与content_revision_id一致，三者同事务修改；无版本内容绑定只用于兼容迁移/普通手工文字修订，不伪造历史 |
| creative_node_prompt_drafts | id、canvas_id/node_id、action_key、text、model_key、parameters JSONB、revision；UNIQUE(account_id,node_id)，同账号多窗口CAS保存，冲突保留双方草稿；不把临时输入每个按键写进作品历史 |
| creative_node_prompt_refs | draft_id、slot/ordinal、source_node_id?或content_revision_id?、role；动态节点与固定修订二选一，同画布动态关系参与现有参考图环校验；固定修订是真实GC根 |
| creative_node_executions | 复用工具化契约；增加draft_revision/prompt_snapshot、model/parameter_schema/price版本、desired_output_kind、gateway_request_id?、generation_manifest、output/publish水位；同account/operation唯一、同节点活动执行由active指针串行化 |
| creative_node_versions | id、canvas_id/node_id、version_no、origin_kind、content_revision_id、execution_id_snapshot?、output_ordinal?、generation_provenance JSONB、created_at；UNIQUE(account_id,node_id,version_no)，生成来源UNIQUE(account_id,execution_id_snapshot,output_ordinal)。列表成员显式保留输出修订；版本号不用于并发CAS、删除后不复用 |
| creative_node_version_input_refs | version_id、ordinal、content_revision_id、role；生成输入也显式保留，支持重新查看/复用参考；来源node/asset为允许失效的快照key，不跨项目拉动当前指针 |
| creative_group_prompt_submissions | id、group_id、draft_id/revision快照、run_id、operation/hash、成员快照；节点active_agent_run_id及最近submission配套。受理与实际run同事务；输入修订由run_inputs显式保留，历史关联不强留已清理run载荷 |
| 执行input/output refs | 固定输入与落库但未归入版本的候选独立保留；引用/保留期限与状态由执行所有者管理，不只存在JSONURL里 |

`status`和版本列表不重复存媒体字节。状态查询包含单调水位，SSE只发持久事实通知；重连或乱序用同账号权威查询核对，不用供应商原始流片段宣布ready。长期任务的调度、供应商轮询、产物导入、结果发布各有幂等身份；不维持长事务或占一个Worker等待视频完成。

保留数值与窗口由[data-model总表](data-model.md#保留策略总表)统一拥有。生成版本的默认保留期是节点仍存在期间，包括未采用的已成功版本，不跟随30天撤销或90天Agent载荷清理。配额按实际去重字节和已有保留协议计算，预留不足在提交前拒绝；不静默删最老版本腾空间。首期提供显式删除未采用版本，当前采用版本须先切换/清空；删除命令创建撤销保留引用。删除节点释放版本/草稿的使用方根，但其可撤销期内必须保留重建节点及版本列表需要的元数据与修订，期限后仍检查资产、其他节点、任务等全部根。归档不删除版本。历史输入与输出均有反向索引及GC竞争测试。

## 5. Gateway与生成执行器

节点生成应用编排由creativecanvas的execution边界拥有；媒体处理/校验由creativemedia，内容修订由creativecontent，供应商调用与成本账本由LLM Gateway。生成不是必须跑一轮Agent ReAct；UI与Agent都调用相同RequestNodeExecution入口。

Gateway在现有Chat兼容接口之外增加类型化生成请求与异步提交/查询/取消/结果领取端口；不能把视频任务硬塞进Chat响应或只按token计费。目录分别声明输出类型、参考输入类型、比例/时长等参数、同步/异步、取消/查询/幂等能力和计费维度。输入合法性由业务与模型能力共同检查，结果持久性和画布采用由业务负责；Gateway不写节点。

成本继续只有Gateway一份权威账本，单位包括token、图片数、音视频秒数等非重叠分量，价格快照固定、预留/差额核算/unknown协议复用。generation_manifest记录稳定逻辑请求和attempt；供应商受理超时先查询/核实，不新建收费尝试。无法查询且无法证明未受理时维持unknown，允许到期关闭业务执行但保留核算和结果处理资料，不能自动重发。

首期生成执行有单独的按动作上限deadline与账号并发限额，不能机械套用Agent的短对话期限；FND-13依据实际模型时长、测试预算和轮询限额固定配置。Agent attached执行仍不得超过父run既有deadline，准入不满足时明确拒绝并允许改从节点手工发起，不能擅自延长Agent期限或改成detached。暂停Agent不应暂停独立人工节点生成；生成停用也不影响人工编辑/历史预览。

供应商结果URL只是临时取件定位，不能作为作品永久地址。Gateway保留受控结果交接身份；媒体Worker按固定供应商白名单、地址/重定向限制、文件/时长/大小约束取件并校验，复制到本账号受控对象存储后才建立ready修订。取件/入库重试不得重复生成；未完成持久交接、active或unknown结果不得因默认请求载荷到期丢弃。输出与输入各自经用途校验，供应商凭证和临时签名URL不进节点公开data。

FND-13接入前冻结每类至少一个具体供应商/模型版本、真实请求与响应契约、参数schema、编码样本、查询/取消能力、结果TTL、测试费用与取件规则。现在不声称某一家供应商已经支持全部能力；缺资源可先用契约桩开发，但真实生成验收不能用桩或内部text-compose代替。

## 6. 开发与验收

- FND-04：Node data/status DTO、Prompt草稿/引用/版本的基础schema与应用命令、组结构不变、内部执行器及状态投影隔离；单调并发版本和作品版本号分开。
- 新增FND-13（依赖04/05/06）：节点Prompt前端与四类真实生成、媒体Gateway异步适配/核算、供应商结果持久化、生成历史/预览/采用/删除、独立节点任务取消与恢复。链接提示词编辑沿文字调用且不抓取URL。
- FND-07/08：既有Agent执行权与新真实执行器结合，组Prompt范围任务；Agent查/改prompt、执行、查版本与采用版本均复用应用入口。FND-08硬依赖13后才完成生产工具验收。
- FND-10：增加草稿固定引用、全部节点历史输入/输出、未交接供应商结果/执行候选/删除撤销的保留根，验证过旧撤销窗口仍可看版本。
- FND-12：CAN-09/10/11与FLOW-10逐项验收，四类每类至少两次真实生成，切换/重开/下载一致，人工与Agent执行均有证据；缺一种不得标完整首期通过。

必测：同operation双击只产生一任务；同节点不同operation并发拒绝；重生成时旧内容仍可看；生成失败保留旧版本；先选旧版再遇迟到完成不能被覆盖；浏览与采用对下游影响不同；取消/节点删除/归档与发布竞争；多输出重复/乱序不重复版本；任务成功但取件失败能只重试导入；状态刷新不导致data_revision冲突；账号隔离、配额/真实成本、持久结果与全根GC恢复。

组Prompt验收：提交→切换/重开→状态/停止定位同run；重复提交不重复run；删除/解组与工具提交竞争有确定先后，复原组不重启旧任务，输入授权和引用不借用无关会话。生成启停验收按[migration-release](migration-release.md)分别覆盖准入、自动采用与维护，停用不破坏历史可读性。

生成停用仅阻止新生成派发和执行器自动登记/采用，不阻止获准的人工导入/替换/复制/选旧版。已落库候选可用新人工operation显式保存，不能借旧执行器身份自动回放；各类入口的具体守卫以[migration-release](migration-release.md#生成开关与人工版本操作的区别)为准，全局writer停用仍禁止所有写入。
