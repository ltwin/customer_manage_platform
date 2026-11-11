<!-- 本 Feature 的目标契约，批准前为草稿，不代表项目当前已实现。机器形式以 api/openapi.yaml 为准。 -->

## api: ADD 运行中插队

### 用途与调用
- 用途：把摄影师的一条消息送进一个还在进行的运行。成功（202）只表示**已接收**：消息已写入这次运行的输入，在对话里以 `input` 块出现，状态为 `pending`。**已送达**以该块变为 `delivered` 为准。运行在送达前结束时，块变为 `interrupted`。
- 消费者：创意空间助手面板（`frontend/src/creative-canvas/editor/AgentPanel.tsx`）。
- 调用：`POST /api/v1/creative/agent-runs/{id}/steers`，operationId `steerCreativeAgentRun`。
- 格式：`application/json`；请求体沿用创意写命令的信封（`operation_id` + `client_created_at` + `payload`），同 `supplements`。
- 权限：登录账号会话；运行必须属于当前账号（repository 基座按 `account_id` 过滤，ADR-001）；需要 `agent_start` 能力；运行所在项目已归档时拒绝。

### 请求

| 字段路径 | 位置 | 类型/格式 | 必传 | 可为 null | 缺省行为 | 约束与业务含义 |
|---|---|---|---|---|---|---|
| `id` | path | string | 是 | 否 | — | 运行 ID |
| `Idempotency-Key` | header | uuid | 是 | 否 | 缺失 400 | 同一条排队消息的重试必须用同一个 key |
| `operation_id` | body | uuid | 是 | 否 | 缺失 400 | 与 `Idempotency-Key` 一致 |
| `client_created_at` | body | date-time（RFC 3339，带时区） | 是 | 否 | 缺失 400 | 客户端创建时间，只用于审计 |
| `payload.instruction_segments` | body | array of `CreativeAgentSupplementSegment` | 是 | 否 | 缺失或空数组 400 | 1–`InstructionSegments` 个，按顺序组成一条消息 |
| `payload.instruction_segments[].type` | body | string enum `text` \| `content_ref` | 是 | 否 | — | 不接受 `skill_ref`、`command`（一轮的 Skill 固定） |
| `payload.instruction_segments[].text` | body | string | `type=text` 时必传 | 否 | — | 非空白；整条消息文字合计不超过 `MessageBodyBytes` |
| `payload.instruction_segments[].content_revision_id` | body | string | `type=content_ref` 时必传 | 否 | — | 本账号可用的内容修订（`RequireUsable(display)`）。由账号级同意覆盖，不要求在本轮授权清单中（见「data: ADD 运行中附带内容的外发授权」） |

判别联合：每种 `type` 只允许自己的字段，多写字段会被拒（同 `CreativeAgentInstructionSegment`）。

### 成功响应
- `202 Accepted`，响应体为 `CreativeAgentRun`（与 `supplements` 相同）。状态不变（仍是 `running` / `queued` / `waiting_client`），`revision` 不变；`last_event_seq` 前进，前进的那条事件就是 `input` 块的创建。客户端读到的快照或流到达这个序号时，块已经在里面了。

### 失败响应
错误体为公共 `ErrorEnvelope`。

| HTTP 状态码 | 业务错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed`（`CodeValidationFailed`） | 片段为空、类型不支持、字段多余、文字为空白 | 不重试；前端不会构造这种请求 |
| 403 | `creative_egress_required` | 引用了图片而本轮授权不含 `image_preview`；或素材的用途不允许外发 | 消息回队首 |
| 404 | `not_found`（`CodeNotFound`） | 运行不存在或不属于当前账号；引用的修订不存在或不属于当前账号 | 消息回队首 |
| 409 | `creative_run_state_conflict` | 运行不是 `queued` / `running` / `waiting_client`；已请求取消；或最新模型步骤已给出最终回答并入账（这一轮已结束，正在收尾） | 消息回队首，按队列规则处理（不可原样重试） |
| 409 | `creative_egress_revoked` | 授权已撤销 | 消息回队首 |
| 409 | `creative_conversation_archived` | 项目已归档 | 消息回队首 |
| 409 | `creative_operation_expired` | 运行已过期限 | 消息回队首 |
| 413 | `creative_context_limit` | 这次运行的补充（含插队）已达 12 次，或消息超长 | 消息回队首，提示“这一轮不能再插入，会在本轮结束后发送” |
| 422 | `creative_model_capability_missing` | 引用了图片而本轮模型不支持图片输入 | 消息回队首；前端事先置灰，正常不会触发 |

### 交互规则
- 写操作：在同一事务内完成三件事：向对话追加一条 `role=user`、`run_id=该运行` 的消息（正文为片段）；写入 `creative_run_inputs` 的新批次；追加一个 `input` 块（`pending`）。运行状态和 `revision` 不变。失败时什么都不写。
- 幂等：回执以 `(account_id, operation_id)` 为键，操作类型（`agent.steer_run`）、规范化后的 payload 和 `client_created_at` 参与请求哈希。回执保留 90 天，命中时原样回放首次响应（`creativeops.Executor`）。同一个 `operation_id` 换了请求体，或被另一个端点（如 `supplements`）复用，都返回 `409 idempotency_conflict`。结果未知时，客户端用原请求（同 ID、同时间戳、同 payload）重放。
- 并发：与模型结果入账、取消在运行行锁上串行。被接收的插队，只要运行正常继续，就一定被读到：要么在下一次模型请求前送达，要么在最终回答之后再调用一次模型。运行被取消、出错或过期时可能读不到，块变为 `interrupted`。
- 异步：送达状态经运行流里 `input` 块的更新推送，没有单独的事件。

### 调用示例

```http
POST /api/v1/creative/agent-runs/crun_7c1e/steers
Idempotency-Key: 4f0f8a52-5d0e-4c3a-9a1f-2a8f3e6b7c10
Content-Type: application/json

{"operation_id":"4f0f8a52-5d0e-4c3a-9a1f-2a8f3e6b7c10","client_created_at":"2026-11-10T08:00:00Z","payload":{"instruction_segments":[{"type":"text","text":"背景换成黄昏的海边"},{"type":"content_ref","content_revision_id":"ccr_91ab"}]}}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{"id":"crun_7c1e","conversation_id":"ccv_12","state":"running","revision":9,"model_key":"gpt-5","deadline_at":"2026-11-10T08:20:00Z","settlement_state":"pending"}
```

（`CreativeAgentRun` 的其余字段同现有 schema，此处只列与本接口相关的字段。）

失败（运行已在结束）：

```http
HTTP/1.1 409 Conflict
Content-Type: application/json

{"error":{"code":"creative_run_state_conflict","message":"该运行的当前状态不允许这个动作"}}
```

### 兼容与变更
- 新增接口，首次定义，没有需要迁移的已有消费者。

## api: ADD 补充并继续运行

### 用途与调用
- 用途：运行停下来等补充（`waiting_input`）时，摄影师补充一条消息，运行在原授权和期限内继续。本次扩展：除纯文字外，可以带引用和附件。
- 消费者：助手面板的输入框（补充并继续）和排队消息的「插队」（运行处于 `waiting_input` 时）。
- 调用：`POST /api/v1/creative/agent-runs/{id}/supplements`，operationId `supplementCreativeAgentRun`（已有）。
- 格式、权限：同「运行中插队」。

### 请求

| 字段路径 | 位置 | 类型/格式 | 必传 | 可为 null | 缺省行为 | 约束与业务含义 |
|---|---|---|---|---|---|---|
| `id` | path | string | 是 | 否 | — | 运行 ID |
| `Idempotency-Key` | header | uuid | 是 | 否 | 缺失 400 | — |
| `payload.expected_revision` | body | integer ≥ 1 | 是 | 否 | — | 运行当前 revision |
| `payload.waiting_token` | body | string | 是 | 否 | — | 本次等待的 token |
| `payload.text` | body | string 1–32768 | 与 `instruction_segments` 二选一 | 否 | — | 已有字段，保持原语义 |
| `payload.instruction_segments` | body | array of `CreativeAgentSupplementSegment` | 与 `text` 二选一 | 否 | — | **新增**；约束同「运行中插队」 |

两者都给或都不给时返回 400。

### 成功响应
- `202 Accepted`，`CreativeAgentRun`，状态变为 `queued`（不变）。

### 失败响应
在现有错误（`409 creative_run_state_conflict`、`409 creative_revision_conflict`（revision/token 不符）、`409 creative_operation_expired`（期限已过）、`413 creative_context_limit`）之外，新增与「运行中插队」相同的 `403 creative_egress_required`、`422 creative_model_capability_missing`，触发条件相同。

### 交互规则
- 写操作、幂等：不变；带片段时，消息正文和输入批次按片段写入。
- 送达：恢复执行后，下一次模型调用前送达（已有机制），引用的内容按授权展开。

### 调用示例

```http
POST /api/v1/creative/agent-runs/crun_7c1e/supplements
Idempotency-Key: 0b4e7f1a-2c3d-4e5f-8a9b-0c1d2e3f4a5b
Content-Type: application/json

{"operation_id":"0b4e7f1a-2c3d-4e5f-8a9b-0c1d2e3f4a5b","client_created_at":"2026-11-10T08:05:00Z","payload":{"expected_revision":12,"waiting_token":"wt_3f9a","instruction_segments":[{"type":"text","text":"用这张做参考"},{"type":"content_ref","content_revision_id":"ccr_77de"}]}}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{"id":"crun_7c1e","conversation_id":"ccv_12","state":"queued","revision":13,"model_key":"gpt-5","deadline_at":"2026-11-10T08:20:00Z","settlement_state":"pending"}
```

### 兼容与变更
- 兼容扩展：`text` 从必填变为与 `instruction_segments` 二选一。只发 `text` 的老客户端行为不变；新增字段对老客户端不可见。
- 生成物：`api.gen.go` 中 `CreativeAgentSupplementPayload.Text` 变为指针类型，需同步改调用处（仅服务端解码，使用 `creativeops.Decode` 解析原始 JSON，不受影响）。

## api: ADD 运行块类型 input

### 用途与调用
- 用途：在一轮运行的块流里，标出摄影师中途插入的一条消息（插队或补充）出现的位置，以及模型是否读到了它。
- 消费者：助手面板的 `RunTurn`；经现有的运行快照和运行流（`CreativeAgentBlock`）下发，没有新路径。
- 权限：同运行快照。

### 字段

| 字段路径 | 类型 | 出现条件 | 可为 null | 业务含义 |
|---|---|---|---|---|
| `type` | string `input` | 始终 | 否 | — |
| `payload_version` | integer `1` | 始终 | 否 | 更高版本按既有约定降级显示 |
| `state` | `pending` \| `delivered` \| `interrupted` | 始终 | 否 | `pending`：已接收，模型还没读到；`delivered`：某次模型请求已带上它；`interrupted`：运行在送达前结束，界面显示「助手没有读到这条」 |
| `payload.message_id` | string | 始终 | 否 | 对应的 `role=user` 消息 ID |
| `payload.body` | `CreativeAgentMessageBody` | 始终 | 否 | 与该消息正文相同（文字与 `content_ref` 块），用于原位渲染 |
| `finished_at` | date-time | `delivered` / `interrupted` 时 | 是 | — |

### 交互规则
- 每个输入批次对应一个块，`source_key = input:<batch_ordinal>`，在运行内唯一。块只追加、不重排，所以气泡位置固定。
- 对话消息列表里，凡是已由某个 `input` 块显示的消息，不再单独画出。历史上没有 `input` 块的纯文字补充（本次之前写入的），仍按原来的方式显示为对话里的一条用户消息。
- 状态只前进：`pending` → `delivered` / `interrupted`；`delivered` 之后不变。

### 示例

```json
{"id":"ccbk_0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b","run_id":"crun_7c1e","ordinal":7,"type":"input","state":"delivered","revision":2,"payload_version":1,"hidden":false,"payload":{"message_id":"cmsg_51","body":{"schema_version":1,"blocks":[{"type":"text","text":"背景换成黄昏的海边"}]}},"started_at":"2026-11-10T08:00:01Z","finished_at":"2026-11-10T08:00:09Z"}
```

### 兼容与变更
- 兼容扩展：块类型本来就由注册表开放登记（「由后续条目登记」）。老客户端遇到未知类型时按约定降级显示，不会出错。

## data: ADD 运行中附带内容的外发授权

- 权威定义仍在 `docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」，本次同步修改那里；这里只登记新增的这条规则。
- 原规则：`selected_revisions` 只覆盖 `creative_egress_consent_contents` 明确列出的修订。新消息建运行时，每个引用都必须在该运行授权的清单里。
- 补充（owner 2026-11-11）：摄影师在一次运行进行中亲手附带或 @ 引用的本账号内容（插队、补充），由账号级同意覆盖，也就是首次「发送前说明」所列的「你附带或 @ 引用的图片」。不要求它出现在运行授权的清单里，但仍需校验：
  - 属于本账号且可用（`RequireUsable`，含用途权利）；
  - 本运行授权未撤销，`policy_version` 有效；
  - 图片要求授权包含 `image_preview`；
  - 派发前照常复核撤销与可用性。
- 不变：工具返回的素材、助手检索得到的内容，仍按原规则授权。授权记录不可修改、不可替换。
- 后续：整套逐资产授权收敛为账号级同意，另开 change 处理。
