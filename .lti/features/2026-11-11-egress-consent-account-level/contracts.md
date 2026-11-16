# 契约增量

## glossary: ADD 外发同意

- 定义：账号对「发送前说明」某个版本的同意。它决定本账号的内容能否发给平台签约的模型服务商，以及助手能否检索素材库。同意按账号记录，与对话、浏览器和设备无关。
- 边界：外发同意不是逐资产授权。具体内容能否外发，还要看它是否属于本账号、当前可用、允许展示，以及是否在读取范围内（当前画布、本对话、勾选时的素材库）。外部助手连接用自己的 OAuth 授权，不使用外发同意。
- 失效：说明版本升级或摄影师撤回，效果相同。失效后不能开始新的回复，进行中的回复在下一次调用模型前停下。

## data: ADD 账号外发同意

### 用途与存储角色

- 用途：服务端判断一个账号当前能否外发，以及能否检索素材库；保留每次同意、修改和撤回的证据。
- 存储：PostgreSQL。沿用 `creative_egress_consents`，用 `conversation_id IS NULL` 的行表示账号级同意；新增 `creative_egress_consent_states`，每个账号一行。
- 角色：权威主数据。所有权：`creativeagent`。
- 外部可见形式是派生读模型：`GET /creative/egress-consent` 的 `CreativeEgressConsentState`。

### 结构与身份

`creative_egress_consent_states`：

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `account_id` | text | 是 | 主键 |
| `current_consent_id` | text | 否 | 当前同意的 `creative_egress_consents.id`；为 null 表示没有同意 |
| `last_revoked_at` | timestamptz | 否 | 最近一次撤回的时间，用来区分「已撤回」和「从未同意」 |
| `revision` | bigint | 是 | 从 1 开始，每次同意变化或撤回加 1 |
| `created_at`、`updated_at` | timestamptz | 是 | 服务端时间 |

`creative_egress_consents` 中的账号级行：

| 列 | 账号级取值 |
|---|---|
| `id` | `ccec_<uuid>` |
| `conversation_id` | null |
| `vendor_key` / `purpose` | `platform` / `creative_assistance`，与现有约定相同 |
| `scope` | `{"mode": "account_library" \| "selected_revisions", "data_classes": ["text","image_preview","media_metadata","canvas_metadata"]}`。`account_library` 表示允许检索素材库；`selected_revisions` 不带清单，表示不允许 |
| `policy_version` | 同意时的说明版本，本次为 `egress-2026-11-1` |
| `superseded_at` | **新增列**，可空。因修改素材库选择而不再是当前同意的时间 |
| `revoked_at` | 撤回时间 |

约束：

- 部分唯一索引 `(account_id) WHERE conversation_id IS NULL AND superseded_at IS NULL AND revoked_at IS NULL`，保证每个账号至多一条未盖章的账号级行。
- 账号级行没有 `creative_egress_consent_contents` 子行。

### 访问模式与保证

- **门禁**：先 `FOR SHARE` 锁住状态行，再按 ID 读当前同意行（不加锁），然后核对版本。锁的位置在「会话 / 运行之后、内容之前」，沿用原同意锁的位置。多个对话的派发可以同时持有共享锁。
- **同意与撤回**：状态行 `FOR UPDATE`。同意时插入新行，旧行盖章 `superseded_at`，`current_consent_id` 指向新行；撤回时当前行盖章 `revoked_at`，`current_consent_id` 置 null。两者都在一个事务里提交。撤回还会在同一事务里，把本账号所有未失效的 `creative_agent_compactions` 标记为 `consent_revoked`。
- **数据库保证**：主键、部分唯一索引、CHECK 约束（沿用现有 `scope` 约束）。**应用保证**：`current_consent_id` 指向的是同账号、账号级、未盖章的行；内容相同的同意不新增行。
- 运行的 `egress_consent_id` 保存准入时的当前同意 ID，作为证据，之后只读取它的 `policy_version`。
- 访问示例：账号同意（行 A，开启素材库）→ 关闭素材库（行 B；A 盖章 superseded）→ 撤回（B 盖章 revoked，current=null，last_revoked_at 有值）。之后门禁返回 `creative_egress_revoked`。

### 生命周期与演进

- 同意行和状态行随账号保留，不清理。
- 会话级的旧行（`conversation_id` 不为 null）和 `creative_egress_consent_contents` 改为只读历史，不再写入，也不参与门禁和读取；它们的清理沿用会话删除时的既有规则。
- 迁移只增加表、列和索引，不回填。所有账号上线后都处于「没有同意」状态。反向迁移在已有账号级行时拒绝破坏性删除。
- 说明版本升级只改服务端常量，不改数据；旧版本的同意自然失效。

## data: MODIFY 运行中附带内容的外发授权

- 权威定义在 `docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」，本次整段改写为账号级规则。
- 外发需要同时满足两点：
  - 账号有有效的外发同意（见「账号外发同意」）。
  - 内容在本运行的读取范围内：当前画布节点持有的内容；本对话中、序号在触发消息之前或属于本运行的完整消息所引用的内容；同意允许检索素材库时，未进回收站的资产的当前修订。
- 各内容仍逐项校验：属于本账号且当前可用（`RequireUsable(display)`，含用途权利）；数据类别在同意范围内；图片要求运行模型能看图；派发前照常复核同意和可用性。
- 摄影师新建消息、插队、补充时附带或 @ 引用的内容，都写在本运行的消息上，所以由上面第二条分支覆盖，不需要清单。
- 删除的规则：
  - `selected_revisions` 清单（`creative_egress_consent_contents`）不再参与判断；
  - 「被本对话已撤回的授权列过的修订一律拒绝」这条规则不再存在。
- 不变：工具返回的素材、助手检索得到的内容，仍按读取范围和逐项校验处理。
- 素材库关闭后，本运行和本对话历史里素材库工具（`search_assets` 查账号素材库、`read_library`、`read_library_result`、整理/管理/回收站/恢复、`save_to_library`）返回的内容不再随之后的请求发出，换成一句占位说明；`read_history` 和压缩摘要的转录同样处理。请求组装时素材库还开着、派发事务提交前关了的，这次派发被拒绝，运行以 `creative_egress_library_closed` 结束（门禁上按缺少同意处理，只是结束原因单列，前端提示「素材库检索已关闭」而不是「发送前说明有更新」）。`read_asset` 读到的素材库内容属于本运行自己的会话引用，按「已有会话引用仍按原许可读取」继续可读，不在占位范围内。

## api: ADD 读取外发同意

### 用途与调用

- 用途：前端判断发送前是否需要弹出说明，以及在「外发说明」面板中展示当前同意。
- 消费者：创意空间的画布助手面板和首页「一句话开始」。
- 调用：`GET /api/v1/creative/egress-consent`，响应为 `application/json`。
- 权限：登录会话，需要 `creative_read` 能力，只返回当前账号的数据。

### 请求

无参数。

### 成功响应

200，`CreativeEgressConsentState`：

| 字段 | 类型 | 必返 | 可为 null | 含义 |
|---|---|---|---|---|
| `policy_version` | string | 是 | 否 | 服务端当前的说明版本 |
| `effective` | boolean | 是 | 否 | 当前同意存在，且版本等于 `policy_version` |
| `revision` | CreativeRevision | 是 | 否 | 状态版本；没有状态行时为 `"0"` |
| `consent` | object | 是 | 是 | 当前同意；没有同意（从未同意或已撤回）时为 null |
| `consent.id` | string | 是 | 否 | `ccec_<uuid>` |
| `consent.policy_version` | string | 是 | 否 | 同意时的版本，可能低于当前版本 |
| `consent.library_access` | boolean | 是 | 否 | 是否允许检索素材库 |
| `consent.granted_at` | RFC 3339 datetime | 是 | 否 | 同意时间 |
| `revoked_at` | RFC 3339 datetime | 否 | 否 | 仅在当前没有同意、且最近一次是撤回时返回 |

### 错误

| HTTP | 错误码 | 触发 | 处理 |
|---|---|---|---|
| 401 / 403 | 认证或创意能力错误 | 会话失效或账号不可用 | 重新登录 |

### 示例

```json
{"policy_version":"egress-2026-11-1","effective":true,"revision":"2","consent":{"id":"ccec_5d6e7f80-1a2b-4c3d-8e4f-5a6b7c8d9e0f","policy_version":"egress-2026-11-1","library_access":true,"granted_at":"2026-11-20T09:12:03Z"}}
```

```json
{"policy_version":"egress-2026-11-1","effective":false,"revision":"3","consent":null,"revoked_at":"2026-11-21T10:00:00Z"}
```

### 兼容性

新增接口。不分页、不过滤，只是读操作，没有限流以外的特殊约定。

## api: ADD 同意发送前说明

### 用途与调用

- 用途：记录摄影师对当前版本说明的同意，或修改素材库选择。成功后账号的同意生效。
- 消费者：发送前说明弹窗、「外发说明」面板里的素材库开关。
- 调用：`POST /api/v1/creative/egress-consent`，creativeops 写入信封，`Idempotency-Key` 必填。
- 权限：登录会话，需要 `agent_start` 能力，只作用于当前账号。

### 请求

| 字段 | 位置 | 类型 | 必传 | 可为 null | 约束与含义 |
|---|---|---|---|---|---|
| `Idempotency-Key` | header | uuid | 是 | 否 | 等于 `operation_id` |
| `operation_id` | body | uuid | 是 | 否 | 重试时保持原值 |
| `client_created_at` | body | RFC 3339 datetime | 是 | 否 | creativeops 的时间窗口 |
| `payload.policy_version` | body | string | 是 | 否 | 摄影师看到的说明版本，必须等于服务端当前版本 |
| `payload.library_access` | body | boolean | 是 | 否 | 是否允许检索素材库 |
| `payload.expected_revision` | body | string | 是 | 否 | 发起方读到的状态 `revision`（`^(0\|[1-9][0-9]*)$`，从未同意为 `"0"`）。与服务端当前状态不符时拒绝，不写入 |

`payload` 拒绝额外字段。

### 成功响应

200，`CreativeEgressConsentState`，`effective` 为 true。如果当前同意的版本和素材库选择与请求相同，不新增记录，返回现状；否则建新同意，旧同意盖章 `superseded_at`。同一个 operation 回放原回执。

同意以 `expected_revision` 为条件：界面打开后别处撤回或改过，旧界面上的同意或素材库开关不会把同意恢复回来。客户端在结果未知（断网、超时、5xx）时按原信封重试；收到的回执可能是更早那次的回放，客户端随后重新读取状态。

素材库从开到关时，同一事务里把本账号所有未失效的压缩摘要标记为 `consent_revoked`（摘要可能引用了素材库检索结果）。

### 错误

| HTTP | 错误码 | 触发 | 处理 |
|---|---|---|---|
| 400 | `validation_failed` | 信封或字段不合法 | 修正请求 |
| 401 / 403 | 认证或创意能力错误 | — | 重新登录 |
| 409 | `creative_egress_policy_changed` | `policy_version` 不是当前版本 | 刷新页面，看最新说明后再同意 |
| 409 | `creative_revision_conflict` | `expected_revision` 不是当前状态版本 | 重新读取状态，再请摄影师确认 |
| 409 | `idempotency_conflict` / `creative_operation_expired` | 公共约定 | 按公共约定处理 |

### 示例

```http
POST /api/v1/creative/egress-consent
Idempotency-Key: 4c6f28c0-9122-4e84-a1e8-aa1e40a7b211
Content-Type: application/json

{"operation_id":"4c6f28c0-9122-4e84-a1e8-aa1e40a7b211","client_created_at":"2026-11-20T09:12:01Z","payload":{"policy_version":"egress-2026-11-1","library_access":true,"expected_revision":"0"}}
```

```json
{"error":{"code":"creative_egress_policy_changed","message":"说明已更新，请刷新后重新查看"}}
```

### 兼容性

新增接口。与撤回并发时按状态行加锁串行，后提交的一方读到前者的结果。

取代会话级授权接口 `POST /api/v1/creative/conversations/{id}/egress-consents`（`grantCreativeEgressConsent`），该接口随本次删除：逐条授权由账号级外发同意取代；唯一消费者是本仓库前端的发送链（`agentState.ts:messageSteps`），同一版本删除。前后端同一个二进制发布，发布即下线；已打开的旧标签页请求失败，刷新后恢复。

## api: ADD 撤回外发同意

### 用途与调用

- 用途：摄影师撤回外发同意。撤回后不能开始新的回复，进行中的回复在下一次调用模型前停下；已经发出的内容无法召回。
- 消费者：「外发说明」面板。
- 调用：`POST /api/v1/creative/egress-consent/revoke`，creativeops 写入信封，`Idempotency-Key` 必填。
- 权限：登录会话，需要 `agent_start` 能力，只作用于当前账号。

### 请求

信封字段同「同意发送前说明」；`payload` 为空对象，拒绝额外字段。

### 成功响应

200，`CreativeEgressConsentState`：`effective=false`，`consent=null`，带 `revoked_at`。已经没有同意时返回现状，不报错。同一事务里，本账号所有对话的压缩摘要失效。

### 错误

| HTTP | 错误码 | 触发 | 处理 |
|---|---|---|---|
| 400 | `validation_failed` | 信封不合法 | 修正 |
| 401 / 403 | 认证或创意能力错误 | — | 重新登录 |
| 409 | `idempotency_conflict` / `creative_operation_expired` | 公共约定 | 按公共约定处理 |

### 示例

```json
{"operation_id":"7b8c9d0e-1f2a-4b3c-8d4e-5f6a7b8c9d0e","client_created_at":"2026-11-21T10:00:00Z","payload":{}}
```

```json
{"policy_version":"egress-2026-11-1","effective":false,"revision":"3","consent":null,"revoked_at":"2026-11-21T10:00:00Z"}
```

### 兼容性

新增接口，取代 `POST /api/v1/creative/egress-consents/{id}/revoke`（`revokeCreativeEgressConsent`），该接口随本次删除：撤回改为账号级；前端从未调用，只有后端测试在用，测试已改写。下线安排同「同意发送前说明」。

## api: ADD 建运行与重试的外发准入

- `CreativeAgentRunPayload` 没有外发字段，`required` 为 `[instruction, model_key]`。服务端按账号当前有效同意准入；没有有效同意时返回 403 `creative_egress_required`，已撤回时返回 409 `creative_egress_revoked`，并且不建运行、不预留预算。
- `CreativeAgentRetryPayload` 没有外发字段，`required` 为 `[step_ids]`。准入规则同上。
- `CreativeAgentRun.egress_consent_id`：准入时账号的当前同意。本次上线之前的运行仍指向会话级旧授权。
- 插队和补充的请求没有同意字段，准入规则同上。
- 运行在执行中因同意失效而结束时，结束原因是 `creative_egress_revoked`（已撤回）或 `creative_egress_required`（版本过期等）；请求组装后、派发前素材库被关闭的，结束原因是 `creative_egress_library_closed`。

### 兼容性

**BREAKING**：两个载荷删除了原来的 `egress_consent_id`，带这个字段的请求按 `additionalProperties: false` 返回 400。消费者只有本仓库前端，同一版本改完。
