---
status: proposed
version: 0.2
created: 2026-09-10
---

# Gateway / Harness 数据与事务协议

[data-model](../data-model.md)拥有领域归属、run状态机和保留期限；本稿补充第三批字段、约束与事务顺序。全部新业务表均有account_id、显式账号/key关联、无数据库外键，保留PK/UNIQUE/CHECK/index。下列是完整模块记录设计；[协议探针SQL](gateway-harness-protocol.sql)仅截取竞态验证需要的列，不是全量迁移草案或可直接投产的DDL。

## 1. 身份与载荷

沿用TEXT前缀UUID：ccco会话、ccms消息、ccrn运行、ccst步骤、ccri输入、ccar产物、ccec外发同意、ccad附件草稿、ccat附件、llmr模型请求、llma真实尝试、llmv预留、llmu用量证据、llmc结算回执。run/step/request不是同一种身份；操作UUID独立。revision/epoch/seq按BIGINT十进制字符串出API。

所有表除复合key关系表外保留created_at/updated_at；完整记录有schema_version与有界JSON。实体id全局PK且UNIQUE(account_id,id)，关系PK包含account。actor/调用方/时间由服务端确定。JSON内显示用来源快照允许失效，任何真实内容保留必须有显式revision关系；服务写守卫验证同账号同对象，不依赖数据库外键隐式执行。

## 2. Gateway 表及索引

| 记录 | 字段细化 / 约束与常用索引 |
|---|---|
| llm_call_groups | caller_service、caller_group_id、limit_micros/token_limit、deadline_at、created_at；PK(account,caller,group)，只保存固定准入上限，不保存第二份spent；先锁此行串行跨月预留，实际使用量从Gateway预留/核算事实汇总 |
| llm_budgets | period_start DATE（UTC月首）、currency CHAR(3)、limit/reserved/spent_micros、token_limit/reserved_tokens/used_tokens、revision；PK(account,period,currency)，所有数值非负；spent可超limit以反映真实费用 |
| llm_usage_reservations | id、caller_service/operation_id/group_id、group_limit_micros/group_token_limit、request_id?、budget_period/currency、initial_reserved_micros/tokens、remaining_hold_micros/tokens、hold_generation、retry_eligible、actual_micros/tokens?、settlement_state、price_version、expires_at、revision；调用方操作唯一，request非空唯一；group上限由首次预留固定，之后不静默放大；索引account/caller/group与state/expires |
| llm_requests | id、caller_service/operation_id/group_id、request_hash、model/price_snapshot、request_payload（有界）、deadline_at、state、cancel_requested_at/closed_at?、result_payload?/result_hash?、result_revision、created_at、retained_until；幂等三元唯一；succeeded必须有完整result/hash；过期可压缩为不可自动重放的墓碑，不再派发 |
| llm_attempts | id、request_id、attempt_number、reservation_generation、dispatch_state、dispatch_epoch、permit_digest、provider_request_id?、dispatched_at/finished_at?、error_class、transport_finished_at?、limit_key、permit_released_at?；(account,request,attempt_number)唯一；同request只有一个dispatching/streaming/unknown活动attempt的部分唯一索引；permit摘要唯一且不可重领 |
| llm_usage_measurements | id、attempt_id、measurement_key、cost_component、currency、usage_dimensions、price_snapshot、cost_micros?/token_count?、certainty、evidence_kind/rank、provider_revision?、supersedes_id?、evidence_digest；(account,attempt,measurement_key)唯一；只追加，零和未知不同 |
| llm_measurement_dispositions | measurement_id、state=pending/applied/obsolete/disputed、reason、resolved_by_measurement_id?、updated_at；PK(account,measurement)，处置可改变，原证据不可变；分叉不动预算 |
| llm_cost_positions | attempt_id、cost_component、currency、current_measurement_id、booked_cost_micros、certainty、revision；PK(account,attempt,component,currency)，每分量唯一当前值，非负金额 |
| llm_token_positions | attempt_id、dimension=budget_total_tokens、current_measurement_id、booked_tokens、certainty、revision；同维度唯一，和成本分开核算；不得将cached再加到input_total |
| llm_settlement_receipts | id、attempt_id、position_kind/key、position_revision、measurement_id、old/new_booked_value、old/new_remaining_hold、budget_period/currency、created_at；UNIQUE(account,attempt,position_kind,key,position_revision)，事务回放依据，无媒体/模型正文 |
| llm_result_consumers | request_id、caller_service、consumer_key、state=pending/consumed/abandoned、registered_at、released_at?；PK(account,request,caller,consumer)，只能受信调用方登记；pending阻止完整结果被清，消费与业务保存同事务 |

请求state为prepared/dispatching/streaming/succeeded/failed/cancelled/unknown；attempt.dispatch_state为dispatching/streaming/succeeded/rejected/unknown。run的费用投影枚举为not_started/pending/settled/unknown，unknown优先，其次未决pending；没有任何调用才not_started。费用状态unclaimed/reserved/partially_settled/settled/unknown/released属于预留；API映射明确，不用费用枚举污染run.state。请求完整成功而费用unknown允许Harness继续使用结果；不能因用量字段缺失就重发已成功模型请求。

平台并发控制额外用 `platform_llm_limits(limit_key PK, capacity, active_count, window_start, window_count, rate_limit, revision)`；它是按供应商credential/deployment共享的基础设施记录，由平台服务访问，不开放给摄影师CRUD。active_count根据未释放permit审计，使用原attempt持有者条件释放；窗口计数限制速率。所有业务表仍带账号。该平台记录不混入跨账号业务查询端口。

## 3. Harness 表及索引

| 记录 | 字段细化 / 约束与索引 |
|---|---|
| creative_agent_conversations | project_id、canvas_id、title、revision、next_message_ordinal；账号/project/canvas对应守卫；索引account/canvas/updated_at/id |
| creative_agent_messages | conversation_id、ordinal、run_id?、role、status、body schema、revision、source_step_id?；(account,conversation,ordinal)唯一，source_step_id角色/消息用途唯一防重复消费；正文和ref分离 |
| creative_message_chunks | message_id、chunk_index、body、message_revision；PK(account,message,chunk_index)，每块≤4KiB；完成/中断消息合并后可删除chunks，正文保持可读 |
| creative_message_content_refs | message_id、content_revision_id、role；PK(account,message,revision,role)，消息是实际保留根 |
| creative_agent_runs | 上层模型字段，加revision、claim_token?、next_step_ordinal、execution_read_set/执行依赖水位（有schema的并发前置条件，不作为内容保留根）、last_event_seq/pruned_through_seq、waiting_token?、waiting_reason?、limits_snapshot、change_group_id、settlement_state投影/observed_at；trigger消息唯一；state合法、deadline>created；终态finished非空，等待/终态无worker lease |
| creative_agent_slots | PK(account)、run_id/claim_token/acquired_at同空同非空；租约仍在run，不把过期当空slot |
| creative_run_inputs | run_id、batch_ordinal、ordinal、content_revision_id?、来源node/asset/message快照、input_role、read_level、source_revision_snapshot、node_data_snapshot、reference_path；(account,run,batch,ordinal)唯一；实际文字/媒体必须有完整载荷或revision，不能只存截断摘要 |
| creative_agent_steps | run_id、ordinal、attempt、retry_of_step_id?、kind=model/tool/check、state、execution_epoch、tool_key/version?、input_hash/input/output、llm_request_id?、operation_id?、parent_model_step_id?/tool_call_index?、started/finished_at；(account,run,ordinal,attempt)唯一，parent_model_step/tool_index非空唯一；重试steps可共享原operation，不对operation设step全局唯一 |
| creative_run_artifacts | run_id、step_id?、content_revision_id?、proposal/preconditions/影响摘要、proposal_hash、planned_operation_id、revision、apply_state、applied_change_id?、expires_at?；应用时revision+state CAS；planned_operation固定；pending内容有下表完整引用 |
| creative_artifact_content_refs | artifact_id、content_revision_id、role；PK(account,artifact,revision,role)，proposal涉及多修订不能只保留第一张图；pending保留至期限，applied时转正式根并释放临时refs |
| creative_run_events | run_id、seq、schema_version、event_type、payload、created_at；PK(account,run,seq)，索引created_at；附属短期投影，不能充当回执或原始完整消息 |
| creative_egress_consents | vendor_key（实现改名，原稿作 provider_key；`provider` 在 Gateway 已指协议族，此处要的是接收字节的公司）、purpose、conversation_id?、scope、policy_version、revision、granted/revoked_at；scope只存范围模式/类别，不存实际ID，撤销递增revision |
| creative_egress_consent_contents | consent_id、content_revision_id、approved_at；PK含账号；授权历史不作为媒体保留根 |
| creative_agent_drafts | conversation_id、revision、state=open/submitted/expired/discarded、expires_at、submitted_message_id?；索引account/conversation/expires；禁止发到另一会话 |
| creative_agent_attachments | draft_id、content_revision_id?、kind、state=ready/attached/discarded/expired、revision、attached_message_id?、content_revision_id_snapshot?；ready必须有revision且无message；attached必须有message且清空临时revision；ready是GC根 |

模型精确请求媒体清单来自run_inputs和step的引用登记，读pin保证事务外I/O；新增step工具输入/输出中未被run_inputs或artifact覆盖的真实修订，必须追加 `creative_step_content_refs(account_id,step_id,content_revision_id,role)`，与step同事务。step_refs在运行详情清理时释放；不可把IDs只藏入input/output JSON。

关系守卫和GC清单同步新增：message_refs、run_inputs、artifact_refs、step_refs、ready attachment；egress consent contents、历史ID、已attached附件快照、Gateway账本都不是媒体保留根。消费者保留保护Gateway完整结果，媒体保留仍走内容/媒体域，不混成通用任意reference表。

## 4. 全局锁序与事务配方

在公共锁序上补齐第三批，不改变已有相对顺序：

1. 已知operation幂等锁（涉及多个按规范key排序）；模型派发的共享platform_llm_limits锁（仅需要供应商准入的事务）。
2. 账号capability/barrier → Agent slot → Gateway分组准入行 → 预算桶（按period/currency）→ 库根。
3. conversation/draft（按类型/id）→ run（多行按id）。
4. Gateway request → reservation → attempt → cost/token position → result consumer（各类内部按id）。
5. asset → upload/candidate → project/canvas → node/content → egress consent/用途声明授权 → content revision → blob。

需要的key先有界预读，锁后重新校验真实关系；纯Gateway维护可以从预算层开始，绝不反向锁run或canvas。纯GC只锁revision/blob检查所有根，不反向锁run/asset。slot取得前不得先锁run；apply命令operation锁必须在run前获取。归档/取消等只使用相应子序列；不得先锁画布再取消run，先在独立控制事务停止run或由外层按完整顺序协调。

Gateway BeginDispatch在业务校验之后执行，但它需要的高层锁已由LockInTx提前取得；绝不能在持有consent/blob后才调用会新取预算/request锁的Prepare。数据校验与派发意图同事务提交，事务外只读已冻结/已pin载荷并发网络I/O。允许范围内的动态库结果每次派发重新校验当前库/用途，检索时获准不等于发送时永远获准。

| 原子单元 | 写集 | 崩溃后的证据 |
|---|---|---|
| CreateRun | 消息、run+输入refs、slot、初始reserve、event、River job、202receipt | 同operation回放；没有可见run而有孤立已入队任务的状态不允许 |
| BeginModel | step、request、reserve认领、consumer pending | 唯一步骤/请求；prepared可恢复，派发以后必须核实 |
| Dispatch / Rearm | 可选重建同request hold/generation、月桶/分组准入、许可计数、attempt与意图、step dispatched、实际同意快照摘要 | COMMIT不明不发；崩溃不重领一次性许可 |
| ConsumeResult | Gateway consumer、step/output、assistant消息、工具计划、events、必要refs | 已完成消费返回原工具计划；不从同一模型结果生成第二批工具 |
| ApplyTool | 领域effect/change、operation receipt、step、artifact、execution_read_set自身结果承接、event | receipt唯一权威；重复/响应丢失不重复效果 |
| ApplyFinalOnly | control与planned-operation幂等锁、slot/run、提案/领域effect、两种回执、产物、终态及slot释放同事务；无模型调用/入队 | 成功200就是已应用；失败整体回滚保持waiting_apply；deadline在实际效果提交守卫检查 |
| Cancel/Close | run epoch/state、slot释放或保留、可释放reserve、event、control receipt | 旧worker不能再写；未知费用保留且独立对账 |
| Measurement | 不可变证据、处置、当前位置、预算/remaining_hold、settlement receipt | 同measurement不二次入账；CAS分叉留pending/disputed |
| AppendEvent | run seq、消息块/状态、对应event | 事务回滚不留下跳号；前缀清理记录pruned水位 |

River负责至少一次唤醒，slot/step/request/receipt负责效果安全。使用现有store/jobs的受信事务桥接让InsertTx与业务同物理事务，未验证前不接正式run入口。任务参数只有服务端account/run或request身份、schema_version，不含提示正文。[River事务入队](https://riverqueue.com/docs/transactional-enqueueing)、[唯一任务](https://riverqueue.com/docs/unique-jobs)。

## 5. 保留、清理与核实

数值回写到[data-model保留总表](../data-model.md#保留策略总表)：附件草稿/ready附件7天；Gateway用量/成本证据与紧凑核算位置首期随账号保留，不设自动删除；完整请求/结果按原90天且pending消费/unknown例外。费用证据只保留核算维度/供应商定位/摘要，不因此永久保留完整私有提示或媒体。

pending consumer不是永久泄漏许可：消费者在业务持久化后置consumed；run结束且无需结果时置abandoned，必须用Harness外层事务确认调用方已停止。维护扫描由Harness从消费者追踪key查询自身run并以一致锁序清理，Gateway不能反向JOIN或锁run。调用方异常失联保守保留并告警；unknown核算证据不靠取消清空。完整结果到期后留请求key/hash/created_at与不可恢复墓碑，墓碑与成本证据按账号保留，避免内部旧step重放成为新调用；正文依期限清理。

账号/项目未来删除尚未在首期定义，归档不启动销毁。停用Agent或供应商仍允许读取历史和核实费用。完整载荷清理必须先确认不存在活动run、pending消费者、所需对账数据；费用查询无需原始大prompt。核算unknown无自动“过几天算零”的出口；运维列表按年龄告警，受信带证据确认可以结算，但不得为了释放额度抹去实际支出。


## Eino运行时存储扩展

[Eino接入§6](../eino-adoption.md#6-checkpoint恢复和运行事实)拥有Checkpoint和context item的字段补充：账号/run/runtime/serializer/目录版本/epoch/水位、opaque checkpoint payload，以及有界tool_result/summary/skill_resource。新增checkpoint/context item的显式content refs参与GC，不用框架序列化payload推断引用。业务run与step仍为唯一执行事实，不复制成第二套Agent业务表；八项适配实验的probe_*表均非产品schema。
