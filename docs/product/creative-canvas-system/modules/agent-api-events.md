---
status: proposed
version: 0.1
created: 2026-09-10
---

# Agent 控制 API、消息与 SSE

遵循[公共契约](common-contracts.md)，路径均以 `/api/v1/creative` 为前缀，尚未部署。正式 DTO 与事件union在 GH-02/GH-05写入OpenAPI生成两侧类型；模型供应商格式不直接暴露为产品API。

## 1. REST

所有写入携带operation_id/client_created_at，header key与body一致；有对象修改时携带对应expected_revision。返回稳定身份/状态，详细内容走GET。首次创建run固定模型/Skill/同意，浏览器不传账号、epoch、step/tool授权或供应商URL。

| 路由 | 请求关键字段 → 响应 |
|---|---|
| GET /agent/catalog | 可用models/skills/tools摘要、limits_version和限制；实际模型能力来自Gateway，UI不能自行猜测 |
| POST /canvases/{id}/conversations | title → 201 conversation_id/revision；校验项目/画布对应 |
| GET /canvases/{id}/conversations | 分页同画布会话 |
| GET /conversations/{id}/messages | before_ordinal/limit → 消息、prev_cursor、会话revision；ordinal十进制字符串，同快照消息有稳定message_id |
| POST /conversations/{id}/drafts | 创建附件草稿 → draft_id/revision/expires_at；同一会话可有多个窗口草稿 |
| GET /agent-drafts/{id} | 返回ready附件及待上传状态，不返回可复用对象密钥 |
| POST /conversations/{id}/egress-consents | provider_key、purpose=creative_assistance、scope模式/数据类别、selected_revision_ids → consent_id与证据摘要；资源ID写显式关联表，scope不藏ID |
| POST /egress-consents/{id}/revoke | revision → revoked_at；先提交的撤销阻止之后派发；已发内容不能召回 |
| POST /conversations/{id}/runs | text、selected_nodes[{id,data_revision}]、model_key/catalog_version、skill_key/version或null、egress_consent_id、可选draft_id/revision及attachment_ids、task_scope、允许额度 → 202 run_id/trigger_message_id/revision/state |
| GET /agent-runs/{id} | 当前run/步骤摘要/产物/等待项/限制/费用状态，以及snapshot_seq、earliest_event_seq、finished_at；大输入/结果单独有界读取 |
| GET /agent-runs/{id}/inputs | 冻结来源、读取级别、修订与到期状态；读取媒体仍检查当前用途 |
| GET /agent-runs/{id}/artifacts | 提案、影响范围、应用状态、过期与实际变更定位 |
| POST /agent-runs/{id}/supplements | expected_revision、waiting_token、明确消息/已授权输入 → 202 queued；保持原模型/同意/截止时间 |
| POST /agent-runs/{id}/apply | expected_revision、artifact_id/revision、proposal_hash、waiting_token、mode=apply_only或apply_and_continue → 前者200原子应用及终态，后者202取得slot入队、GET查询真正效果；mode必填，不隐式继续模型调用 |
| POST /agent-runs/{id}/cancel | 幂等取消当前运行；200返回失效后的revision/state与费用待核实提示，不能只取消队列job |
| POST /agent-runs/{id}/close-reconciliation | 只在reconciling，明确结束等待 → 200终态/settlement；核实继续，写slot释放 |
| POST /agent-runs/{id}/retry | source_run_id由路径、明确待恢复step IDs、原或新有效同意 → 202新run/新trigger消息；恢复已有动作，不从头重新规划 |
| GET /agent-runs/{id}/events?after_seq=... | fetch SSE；只读通知流，不承担上述任何写命令 |

apply_only在同一短事务中校验当前deadline、应用领域命令、记录产物/终态/回执并释放slot；冲突或slot忙则整体回滚保持等待。apply_and_continue可能因排队到期未应用，202不保证期限内完成；两种模式明确展示给摄影师。具体双operation锁序见[Harness采纳协议](harness.md#6-结果采纳和撤销)。

取消使用“取消目前这个run”的语义，不因流事件使revision变动而要求反复重试旧expected_revision；不可取消别的run。补充/采纳则需要固定revision+waiting_token，避免多窗口分别唤醒同一等待点。waiting_token由服务端生成，每次进入等待轮换，是并发身份而非授权凭证。run.revision在状态/等待/产物变化时递增，文本chunk只递增消息revision和event seq，避免每个字使采纳冲突。

task_scope是摄影师明确意图的类型化声明（只读、添加、指定文字更新、整理选区及目标区域）；后端与实际选择、Skill和工具交集验证，不让客户端字段绕过资源权限。未明确的影响生成提案；不靠模型猜测“授权所有”。返回错误继续沿公共ErrorEnvelope：slot忙429 creative_agent_busy（含可见本账号active_run_id）；预算429 creative_llm_budget_exceeded；能力422 creative_model_capability_missing；上下文413 creative_context_limit；外发403 creative_egress_required/revoked；版本/等待409 conflict；原证据过期409 creative_operation_expired。不自动扩大权限或换模型重试。

## 2. 消息与事件分工

消息是持久阅读事实：role user/assistant/tool/system，body schema_version，状态 streaming/complete/interrupted，内容块包括text、reference、tool_result摘要及notice。不存或展示模型私有推理链；只展示可解释的工具步骤和公开回复。可见消息引用content通过message_refs，模型用的临时上下文/大工具结果保存在step，不自动复制成永久消息。

Gateway可发内部delta，Harness批量持久成消息块（起点250ms或4KiB，先达到者触发）。`(message_id,chunk_index)`唯一、message_revision单调；同事务保存块、更新消息和追加run_event，再通知连接。完整模型结果以Gateway保存的完整内容为准，消费时校验已存前缀，最终 `message.completed` 通知客户端重取权威完整消息；中途崩溃未刷的delta可缺失，但不能伪造为已保存或执行工具。中断消息保留interrupted状态。

每个run持有next_event_seq/last_event_seq计数，追加时锁run递增，不能用事务外MAX(seq)+1。seq为十进制字符串；全局无跨run顺序。事件只含定位/状态或≤16KiB的文本块，不含base64、签名URL、原始媒体或完整大提案。

| type | payload |
|---|---|
| run.accepted / run.state_changed | state、run_revision、reason?、waiting_token?、settlement_state |
| message.delta | message_id、message_revision、chunk_index、text_delta；只属于已落库块 |
| message.completed / message.interrupted | message_id、message_revision、内容摘要hash与状态；完整内容通过消息API获取 |
| step.state_changed | step_id、kind/tool_key、state、error_code?；无模型私有推理 |
| artifact.created / artifact.updated | artifact_id/revision、apply_state、影响摘要 |
| canvas.changed | canvas_id、operation_id、change_id、result_revision；浏览器走画布同步器，不能照助手文字改画布 |
| usage.updated | request_id、settlement_state、金额/单位摘要；来自Gateway已提交账本 |
| run.finished | 终态、交付/未完成摘要、费用状态、finished_at；不表示未来费用不再更正 |

事件信封 `{schema_version:1,run_id,seq,type,created_at,payload}`；SSE `id`同seq，event字段同type。未知版本/事件要求刷新快照而非猜写效果。费用维护不反向锁run：Gateway提交核算后，Harness定期读取对应request投影，在自己的事务更新费用摘要并发usage事件；GET可直接通过Gateway端口取得最新费用，投影允许短暂滞后且带observed_at。

## 3. 重连与快照边界

GET run快照在短REPEATABLE READ事务中读取状态、消息摘要、产物和snapshot_seq=S。浏览器先替换快照，再订阅after_seq=S；服务端先验证账号和游标，然后以seq>S补读再追尾。首个补读与实时尾随都从数据库查询，不依赖进程内通知不丢失。

- 相同seq幂等忽略；若收到的下一seq不连续，停止增量合并并补读，不能默默跳过。每个连接独立after_seq，旧订阅通过request generation忽略，避免切换会话后旧字串入新会话。
- 事件保留7天；run保存 `pruned_through_seq` 水位，GC按连续前缀推进。after_seq低于该水位返回409 `creative_snapshot_required`，含快照路径/当前水位，不以“查不到事件”假装已追平。after_seq大于last_event_seq也拒绝。全部事件被清时仍可识别旧游标。
- 过期游标在写SSE头前返回JSON错误；已打开连接期间水位追过慢消费者，发送无id的 `control.snapshot_required` 后断流。心跳注释每15秒，不递增seq。
- 断线按1/2/4/8/15秒抖动退避；频繁断流时UI可回退2秒状态轮询，后台标签放慢。401走既有认证恢复，403/404停止订阅。服务端每30秒和关键读取前重新检查会话/账号有效性；已撤销权限不继续发送新批次。
- 反向代理关闭该路径buffering；Cache-Control:no-store，Content-Type:text/event-stream。单连接缓冲≤256KiB，慢消费者断开补读，不拖慢Worker。初期每账号最多5流连接，每run每连接最多100事件一批。
- run.finished后可关闭主事件连接；页面的费用待核实区域仍查询费用/快照。迟到核算不必保持一个永久SSE。HTTP断开不取消任务，POST cancel才是取消。

正式浏览器测试必须覆盖快照S与订阅之间恰好提交事件、重复chunk、过期水位、跨窗口切换、断流/刷新后最终内容完整、取消后迟到结果。SQL事件序列探针只能证明存储顺序，不能代替这些验收。
