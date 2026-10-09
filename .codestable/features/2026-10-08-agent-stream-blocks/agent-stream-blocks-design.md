---
doc_type: feature-design
feature: 2026-10-08-agent-stream-blocks
requirement: V6-030
status: approved
date: 2026-10-08
---

# 助手推送基础设施：有序块、事件日志与 SSE

主责条目是 V6-030。V6-031（提问）、032（提案）、033（确认）、038（改动卡）、040（状态与后台）、041（排队）的交互都建在这套结构上。本文先冻结它们共用的协议，各条只在自己的 design 里补充块类型的细节。

在 [agent-api-events.md](../../../docs/product/creative-canvas-system/modules/agent-api-events.md) v0.1 草案的基础上收敛：保留信封、`seq`、快照加 `after_seq` 补读、水位这些部分；把事件种类从「每种对象一种事件」改成「块事件」；思考摘要按 D1 处理。

## 0. 结论

- **一切可见过程都是块（block）**。一次运行（run）就是一轮回复，由一列有序的块组成：正文、思考、工具、计划、提问、确认、提案、改动卡、提示。块有稳定 ID、只追加的顺序号、类型、状态、修订号和类型化的 payload。新增交互只需要加一种块类型，不需要新的事件种类、新的通道或新的表。
- **块落库，事件只是增量**。`creative_agent_blocks` 存储每个块的当前全文；`creative_run_events` 是已有的表，在 run 行锁下分配连续的 `seq`，记录每次块变化。刷新时读快照，断线后从 `after_seq` 补读，两种情况都以数据库为准。
- **推送基础设施独立成 `platform/realtime`**（§2.3），分为唤醒总线 `Notifier`、业务事件源 `Source` 和 SSE 传输 `Serve` 三层。本期唤醒用 PG NOTIFY，只负责唤醒，数据从库里读 `seq > cursor`，唤醒丢了也不丢数据。画布流迁到同一套设施上，以后其他业务接入时只需实现一个 `Source`。Redis 作为 `Notifier` 和热缓存的替换实现，切换条件已写明。
- **用户对块的回应只走一个入口** `POST /agent-runs/{id}/blocks/{block_id}/respond`，用 `waiting_token` 和 `operation_id` 保证幂等。每种块类型在后端注册自己的校验和处理逻辑。
- **前端把运行状态放进面板之外的 store**。面板收起后仍然跟踪运行（V6-040 的状态点和 toast 依赖这一点）。每个块单独 memo，只有变化的块才重渲（NF-03），展开状态挂在 block_id 上，流式更新时不会被重置（AC-01）。

本期明确不做：WebSocket、业务之间的事件订阅总线（`realtime` 只负责推给浏览器，不让服务端模块互相订阅）、Redis（理由和切换条件见 §2.3）、整个会话共用一条流。画布 SSE 的对外行为不变，只是底层迁到 `realtime`。

## 1. 块模型

### 1.1 通用字段

| 字段 | 含义 |
|---|---|
| `id` | `ccbk_<uuid>`，创建后不变 |
| `run_id` | 所属运行，对应一轮回复 |
| `ordinal` | 运行内从 1 开始递增，在 run 行锁下分配；块只追加，不重排 |
| `type` | 见 1.2 |
| `state` | 类型自己的状态，加上通用终态 `interrupted` |
| `revision` | 每次变化加 1；文本追加同样加 1 |
| `payload` / `payload_version` | 按 `(type, payload_version)` 校验的 JSON。版本按块类型各自递增，和信封的 `schema_version` 无关：某一种块升级时，只有这一种块在旧客户端上降级显示，信封版本不变，其他块照常增量合并。信封的 `schema_version` 只在信封本身或合并规则（`append`/`set`）变化时才升级 |
| `source_key` | 运行内唯一的幂等键，例如 `step:<step_id>:text`、`call:<call_id>`。worker 崩溃后重做同一步时 upsert 到同一个块，不会产生重复块 |
| 等待身份 | 块本身不存 token。一个 run 同一时刻最多只有一个等待点：多道题放在同一个提问块里，确认和提案按顺序出现。所以沿用 run 上已有的 `waiting_token` 和 `wait_reason`（0049），新增 `runs.waiting_block_id` 指向正在等待的块 |
| `started_at` / `finished_at` | 服务端时间；进入终态时写 `finished_at`。前端的耗时和段时长都由它们算出 |

块和消息（`creative_agent_messages`）各司其职：消息是交给模型的对话事实，继续作为上下文和压缩的来源；块是给摄影师看的呈现。一轮回复在面板上的样子是「触发消息（用户气泡）+ 这次 run 的块」。现有的 agent 代码只在这个 feat 分支上，没有生产数据，所以旧 run 不做回填；没有块的旧 run 按现在的消息方式渲染。

### 1.2 块类型目录

本条只实现前 4 种块和 `notice`。其余类型在这里登记 ID 和状态机，payload 的细节由各自条目补充。原型里每一种 part 都映射到下表中的一种块，没有遗漏（对照 `agent.mjs` 的 `PART` 表）。

| type | 主责 | state | payload 要点 | 需要用户回应 |
|---|---|---|---|---|
| `text` | 030 | `streaming → complete` | `markdown` | 否 |
| `thinking` | 030 | `streaming → complete` | `summary`（公开摘要）、`duration_ms` | 否 |
| `tool` | 030 | `running → succeeded / failed / skipped` | `step_id`、`tool_key`、`title`（服务端生成的人话标题，例如「读取画布」）、`started_at`、`duration_ms`、`detail_ref`（详情按需读取）、`node_refs[]`（产出节点，V6-039 的 chip 和缩略图使用）、`error_code` | 否 |
| `plan` | 030 | `active → complete` | `steps[{key,title,state: todo/doing/done/skipped}]` | 否 |
| `notice` | 030 | `complete` | `code`、`message`（例如失败或预算原因） | 否 |
| `question` | 031 | `waiting → answered / skipped / stopped` | `questions[{key,kind: single/multi/image/aspect/text,options,allow_free,allow_skip}]`、`current`（第几题） | 是 |
| `confirm` | 033 | `waiting → accepted / declined / stopped` | `items[{label,cost}]`、`total_cost`、`mode` | 是 |
| `proposal` | 032 | `waiting → applied / rejected / stale` | `node_id`、`base_revision`、`before`、`after`、`proposal_hash` | 是 |
| `changes` | 038 | `complete → undone` | `created[]`、`edges`、`prompts[]`、`cost`、`change_group_id` | 撤销走画布 undo，不走 respond |
| `suggestions` | 042 | `complete` | `items[]`，即后续建议任务 | 点击等于新发一条消息 |

`interrupted` 是所有类型共有的终态。run 被停止或失败时，所有还处在非终态的块会在同一个事务里被标记为 `interrupted`，所以刷新后不会看到一直在转圈的块。

### 1.3 思考摘要（D1，已拍板）

AC-04 要求「没有公开摘要时，不伪造内部推理」。现有草案 §2 写的是「不存、不展示模型私有推理链」。当前三个模型都是 DeepSeek，其中 thinking 模型会返回 `reasoning_content`，这是原始推理，不是经过整理的摘要。

建议的做法是：`thinking` 块只来自模型显式给出的公开内容。

- 支持 summary 的供应商（例如 Anthropic 的 thinking summary）直接映射成 `thinking` 块。
- DeepSeek 的 `reasoning_content` 不展示，不进块，也不出现在任何接口响应里。这类模型的过程只显示工具行和计划。
- 但它必须作为模型上下文回传（2026-10-09 走查发现，owner 要求支持思考模型并回传原文以保证思考质量）：思考模式下，带工具的请求里每条历史 assistant 消息都要带 `reasoning_content`，否则 DeepSeek 返回 400。原文随 assistant 消息存在 `creative_agent_messages.reasoning`（0057，只作上下文），网关结果里也保留一份供崩溃重放；后续请求和后续 run 都原样回传，计入历史预算。思考模型之外的模型不冻结、不发送它；没有原文的旧消息回传空字符串（实测可被接受）。
- 原型「思考中」那一行改成 run 状态条上的「正在思考」（V6-040 状态条的当前步骤），不生成块。

另一种做法是把 `reasoning_content` 当作摘要直接展示。这样改动小，但违背了草案和 AC-04 的原意，而且原始推理可能很长，还会夹带上下文原文。

### 1.4 工具行和「按轮收起」

- 工具块和 `creative_agent_steps` 一一对应（AS-03）：`step.state` 一变化，就在同一个事务里更新对应的块。标题由后端的工具注册表生成，前端不拼。
- **过程段**：一次 run 就是一个 ReAct 循环，里面会交替出现多次「模型思考 → 调用工具 → 读结果」，中途还可能被提问、确认打断，打断后在同一个 run 里继续。PRD §7.1 把块分成过程块（`thinking`、`tool`、`plan`）和结论块（`text`、`question`、`confirm`、`proposal`、`changes`、`suggestions`、`notice`）。前端把**相邻过程块组成的最长连续区间**归为一个过程段，任意一个结论块都会切断它。
  - 段只在前端派生，不落库，协议里也没有它。块只追加，所以一个段只会在尾部变长，段的第一个块永远不变。段的身份因此直接用第一个块的 id，展开状态挂在这个 id 上，流式更新时不会错位。
  - 段头显示「已处理 X 秒 · N 次操作」。X 从段内第一个块的 `started_at` 算到最后一个块的 `finished_at`，所以等待用户回答的时间不计入；N 是可见的工具块数量。段里有块失败或被停止时，段头显示「1 项失败」或「已停止」。
  - 只有一个块的段不加外壳，直接显示这个块。
  - 运行中，尾部的段展开，显示实时的行，最新一行就是当前步骤。后面一旦出现结论块，或者 run 结束，段就自动收起。用户手动展开或收起过的段，以用户的选择为准，不再自动改变。
  - 模型在调用工具前常会说一句「我先看看画布」这样的 `text`，它会把段切成两截。目前按 PRD 的规则保留：正文属于结论块。如果实际走查发现段被切得太碎，再考虑把短的过渡正文并进段里。
  - 这取代了原型的「整轮折叠」：原型在 run 结束后把一轮里所有的过程块收成最上面的一行，结论块留在原位，于是过程块和结论块的先后顺序就看不出来了。
- 工具详情（参数、结果）可能很大，不放进块 payload，按需从 `GET /agent-runs/{id}/blocks/{block_id}/detail` 读取，读取时复用现有 step 结果的有界读取逻辑。

## 2. 事件与传输

### 2.1 事件种类

块相关事件只有三种，另外加上已有的 run 级事件：

| type | payload | 说明 |
|---|---|---|
| `block.created` | 完整块 | 新块出现，`ordinal` 决定位置 |
| `block.updated` | `{block_id, revision, state?, finished_at?, payload_version?, patch?}` | 增量更新，只带这次写入改变的字段，依次应用后与快照里的同一块逐字段相等。`patch` 只允许两种形式：`{append: {path, text}}`（流式追加文本）或 `{set: payload}`（整个 payload 替换）；只改状态时没有 `patch`。块进入终态的那次写入带 `finished_at`，`set` 带 `payload_version`（2026-10-09 code review 后补充：缺这两项时持续订阅者算不出耗时，和刷新后的快照不一致） |
| `block.deleted` | — | 不提供。块只追加不删除，撤回用 state 表达 |
| `run.state_changed` / `run.finished` / `usage.updated` | 已有 | 保留不变；新增字段 `current_block_id`，供状态条显示当前步骤 |

现有的 `step.state_changed` 和 `message.completed` 在迁移完成后不再写入，前端也不消费它们。

前端只需要一条规则：如果收到的 `revision` 不等于本地块的 revision + 1，就丢弃本地的这个块，回读快照。遇到未知的块类型，或者已知类型但 `payload_version` 比本地支持的新，只把这一个块渲染成「此内容需要更新页面后查看」，其他块照常显示。如果信封的 `schema_version` 未知，就停止增量合并，回读快照，并提示刷新页面。前端不猜测未知内容的含义。

### 2.2 写入路径

所有块变化都经过一个函数 `writeBlockInTx(tx, run, source_key, change)`。它在同一个事务里完成三件事：upsert 块、给 revision 加 1、调用 `appendRunEventInTx` 写事件。块和事件永远一致，不可能出现「有事件没有块」或者反过来的情况。

正文流式增量的写入方式：

- **gateway 加增量回调**。`llmgateway.Call` 新增一个可选参数 `Sink`，有两个方法：`Delta(kind DeltaKind, text string)` 和 `Reset()`。`DeltaKind` 本期只实现 `DeltaText`。`readStream` 每读到一个 `delta.content`，就调用 `Delta(DeltaText, …)`；gateway 内部换下一次 attempt 重试之前，先调用 `Reset`。以后要加 `DeltaThinkingSummary`（Anthropic 流式摘要）或 `DeltaToolArgs`（流式显示「正在写提示词…」），只需要加枚举值，provider 不认识的 kind 不会发出来，现有实现不用改。此外的行为不变：仍然由 gateway 拼出完整结果并校验，增量不算结果（`contract.go` 里对 `Result` 的约定不变）。Anthropic provider 用同一个接口。
- **Harness 节流落库**。距离上一次落库超过 100ms，或者缓冲超过 512 字节，就写一次 `append` 事件。模型调用失败或崩溃时，缓冲里没落库的内容直接丢弃。这样一个运行中的 run 最多每秒写 10 个小事务，run 行锁的持有时间很短。
- **以最终结果为准**。模型步骤结束时，用校验后的 `Result.Text` 写一次 `set`，并把块标记为 `complete`。收到 `Reset`，或者 worker 重做这一步时，块会先被 `set` 成空，再重新追加。所以 append 过的内容只是临时显示，块最终的内容一定和落库的 assistant 消息一致。
- 文本追加只改变块的 revision 和 seq，不改变 `run.revision`，沿用草案的约定，避免每个字都让提交操作遇到冲突。

### 2.3 实时推送基础设施 `platform/realtime`

SSE 作为平台能力，不属于助手。本期抽出来，画布流和助手流共用；以后生成进度、上传进度、通知等需要推送的业务，只要实现一个 `Source`。分三层：

| 层 | 职责 | 本期实现 | 换成 Redis 时 |
|---|---|---|---|
| `Notifier` 唤醒总线 | `Publish(tx, topic)` 和 `Subscribe(ctx, topic) (<-chan struct{}, cancel)`。topic 格式为 `<kind>:<account>:<id>`。只负责唤醒，可以丢失或合并，不承载数据 | PG：每个进程只开一条 LISTEN 连接（统一 channel `creative_realtime`，payload 就是 topic），服务所有 topic。发布写在业务事务里（`pg_notify`，提交后才投递），或者由表触发器完成。连接重连后，向所有订阅者补发一次唤醒 | 换成 Redis pub/sub，接口不变 |
| `Source` 业务事件源 | 由每个业务实现：`Authorize(ctx, scope, id)`、`Cursor(ctx, raw) (cursor, error)`（过期或越界时返回 `ErrSnapshotRequired`）、`ReadAfter(ctx, scope, id, cursor, limit) ([]Event, done bool, err)`。事实源永远在 PG | 画布：没有游标的失效型 source，`ReadAfter` 固定返回一条 `invalidate`。助手 run：基于 seq 日志的 source，`done` 表示 run 已结束且事件已经全部发出 | 可以在前面加一层 Redis Streams 热缓存（XADD 按 seq 去重，maxlen 裁剪），加快 `ReadAfter`；缓存未命中时回落到 PG |
| `Serve` SSE 传输 | 通用 handler：先校验，失败就在写 SSE 头之前返回 JSON 错误；然后写响应头（`text/event-stream`、`no-store`、`X-Accel-Buffering: no`）。之后订阅、补读、追尾。发送规则：`id` 为游标，`event` 为 type，data 为信封；每批最多 100 条；单次写超时 10 秒；每 15 秒心跳，同时重新鉴权并兜底查一次库。连接寿命 5 分钟；每个账号最多 5 条连接；慢消费者直接断开，由客户端补读；运行中游标被裁剪时，发 `control.snapshot_required` 后断开 | 从画布 handler 抽出来 | 不变 |

约定：

- **先订阅再补读**：`Subscribe` 返回之后才执行第一次 `ReadAfter`，这样补读和追尾之间提交的事件不会漏掉。任何一次唤醒都只触发「从游标开始再读一遍」，所以唤醒丢失、合并或重复都是无害的。
- **鉴权**：`Serve` 不认识业务对象，只调用 `Source.Authorize`，周期性重新鉴权也由 `Serve` 统一负责。
- **前端对称抽取**：把 `canvasEvents.ts` 的 `watchCanvasEvents` 改造成通用的 `watchStream({connect, cursor, onEvents, onSnapshotRequired})`。它负责 fetch 流解析、`Last-Event-ID`/`after_seq`、1/2/4/8/15 秒抖动退避、60 秒看门狗，以及 401/403/404 视为终止。画布和助手都用它。
- **画布迁移**：画布的触发器改为发 `creative_realtime`，payload 是 topic `canvas:<account>:<canvas>`。`WatchCreativeCanvas` 改用 `Serve` 加画布 source。对外行为保持不变，包括失效语义和路由，由现有的 `creative_canvas_events_test.go` 和前端 e2e 保证。
- **助手 run 流**：
  - 在 `creative_run_events` 上加 AFTER INSERT 触发器，发布 topic `agent_run:<account>:<run>`。
  - 路由为 `GET /agent-runs/{id}/events?after_seq=S`（也接受 `Last-Event-ID`）。游标低于 `pruned_through_seq`，或者大于 `last_event_seq`，返回 409 `creative_snapshot_required`。
  - 快照接口是 `GET /agent-runs/{id}`，新增 `blocks[]` 和 `snapshot_seq`，在一个 REPEATABLE READ 短事务里读出。前端先应用快照，再从 `snapshot_seq` 开始订阅。

**什么时候换成 Redis**：满足下面任意一条就切换，届时只需替换 `Notifier`，按需再加 Streams 缓存，`Source` 和 `Serve` 不用改。

- API 多实例部署后，NOTIFY 提交排队（全局锁）出现在 p95 提交延迟里。
- 单个进程的推送连接超过约 2k 条。
- 需要跨 PG 实例或跨服务扇出。

本期不引入 Redis，原因有两个：

- 块和事件必须与业务状态在同一个事务里提交。用 Redis 做 box 就需要 PG outbox 加中继，事实源仍然在 PG，本期的工作量不会减少。
- 部署从 app + postgres 变成三个有状态组件，对当前单实例、每账号一个活跃 run 的规模没有收益。

### 2.4 回应入口（契约在这里冻结，由 031 第一个实现）

`POST /agent-runs/{id}/blocks/{block_id}/respond`，请求体为 `{operation_id, waiting_token, response}`，成功返回 202。

服务端的处理步骤：

1. 锁住 run 行，校验 `runs.waiting_block_id` 等于这个块，并且 `waiting_token` 匹配。
2. 交给这个块类型注册的 `Respond` 校验 `response`。
3. 在同一个事务里写回块：状态改为 answered 等终态，答案写进 `payload.response`，同时写事件。
4. 需要继续时把 run 入队，复用现在补充输入的继续机制。

幂等和并发规则：

- 相同 `operation_id` 重复提交，返回同一个回执。
- `waiting_token` 已过期返回 409。多个窗口同时回答时，只有一个能推进运行（031 AC-04）。
- 停止（`cancel`）会把所有等待中的块改成 `stopped`。

后端用块注册表约束每种类型：

```go
// 每种块类型注册一次：payload 校验、可选的回应处理。新增交互 = 新增一项注册。
type blockKind struct {
	validate func(payload json.RawMessage) error
	respond  func(ctx context.Context, tx store.TxAccountScope, run heldRun, b block, response json.RawMessage) (blockChange, bool, error) // bool: 是否继续运行
}
```

模型通过工具产生结构化的块，例如 `update_plan`（由 030 提供）和 `ask_user`（由 031 提供）。工具调用本身也会产生一个工具块，这类「过程工具」标记为 `hidden`，不显示成工具行。

## 3. 数据与迁移

新增迁移 `0056_creative_agent_blocks`：

```sql
CREATE TABLE creative_agent_blocks (
 id TEXT PRIMARY KEY CHECK(id ~ '^ccbk_[0-9a-f-]{36}$'), account_id TEXT NOT NULL,
 run_id TEXT NOT NULL, ordinal INT NOT NULL CHECK(ordinal>0),
 type TEXT NOT NULL CHECK(char_length(type) BETWEEN 1 AND 32),
 state TEXT NOT NULL CHECK(char_length(state) BETWEEN 1 AND 32),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 payload_version INT NOT NULL DEFAULT 1 CHECK(payload_version>0),
 source_key TEXT NOT NULL CHECK(char_length(source_key) BETWEEN 1 AND 160),
 hidden BOOLEAN NOT NULL DEFAULT false,
 payload JSONB NOT NULL CHECK(octet_length(payload::text) <= 262144),
 started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), finished_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,run_id,ordinal), UNIQUE(account_id,run_id,source_key));
ALTER TABLE creative_agent_runs ADD COLUMN next_block_ordinal INT NOT NULL DEFAULT 1,
 ADD COLUMN waiting_block_id TEXT;
-- creative_run_events AFTER INSERT → pg_notify('creative_run_event', {Account, Run})
```

- `type` 和 `state` 不在数据库里写死枚举，由 Go 端的注册表校验，这样新增块类型不需要迁移。payload 上限 256 KiB，单个文本块超过上限时切到下一个 text 块继续写。
- 事件保留 7 天，GC 规则沿用草案。块和 run 的寿命相同。
- 画布触发器函数改为发布 `creative_realtime`，topic 为 `canvas:<account>:<canvas>`；回滚时恢复原来的 channel。
- 回滚时如果表里有块，就拒绝回滚，store 的回滚链测试也要跟着补上（做法同 0055）。

## 4. 前端

- **运行 store**（zustand，已经是依赖）：用 `runID → {snapshotSeq, lastSeq, blocks: Map, order: string[], run}` 记录状态，挂在 WorkspacePage 这一层，生命周期和画布相同。面板收起不会卸载它，V6-040 的状态点、toast 和后台跟踪都直接读这个 store。
- **流控制器** `watchRun(runID)`：建立在通用的 `watchStream`（§2.3）之上。先取快照，再订阅；SSE 一直不可用时，退回到每 2 秒轮询一次快照。seq 有缺口、revision 对不上或者遇到未知版本时，重新读快照。
- **订阅范围**：只订阅还没结束的 run，同一时刻最多一个，因为账号只有一个 slot。已经结束的 run 只读一次快照，然后缓存。
- **渲染**：`<BlockView>` 按 type 分派到具体组件，用 `memo` 比较 `(id, revision)`。展开、收起、悬停的状态按 block_id 存在 store 里，所以流式更新和面板重开都不会重置它们（AC-01）。过程段由块的顺序派生，规则见 §1.4。
- **Markdown**：沿用原型的子集（段落、有序和无序列表、加粗、行内代码），另外补上标题和代码块。实现上直接生成 React 元素，不使用 `innerHTML`，也不引入依赖。流式写到一半的标记（例如还没闭合的 `**`）按普通文本显示，等闭合后再渲染。
- **视觉迁移**：本条把面板的历史区（用户气泡、块、折叠行、运行页脚）迁到 v6 原型的样式。输入区、上下文 chip、模式切换由 028、033、041 各自迁移。

## 5. 与兄弟条目的接口

| 条目 | 使用本条提供的 | 本条为它预留 |
|---|---|---|
| 031 提问 | `question` 块和 respond 入口 | `runs.waiting_block_id`、注册表里的 respond 钩子 |
| 032 提案 / 033 确认 | `proposal`、`confirm` 块和 respond | 同上；`run.revision` 在进入等待时递增 |
| 038 改动卡 | `changes` 块，run 结束前写入；撤销走画布 undo 组 | run 的 `change_group_id` 由 038 设计 |
| 039 节点 chip | 工具块的 `node_refs` | — |
| 040 状态与后台 | 运行 store、`run.state_changed.current_block_id`、等待块 | Esc 和停止复用 `cancel` |
| 041 排队 | 运行 store 里的 run 列表 | 排队本身是前端加上新建 run，不改协议 |
| 042 重答 / 建议 | `suggestions` 块；重答就是新建一个 run | — |

## 6. 拆分与验收

本条按 README 的约定拆成两个子项，各自提交，父条用完整的 AC 关闭：

- **V6-030.1 后端推送底座**：`platform/realtime`（包括画布流迁移）、迁移、块注册表、`writeBlockInTx`、step 到工具块的映射、`update_plan` 工具、gateway 的 Sink 和节流、run 事件流端点、快照中的 blocks、OpenAPI。前端的 `watchStream` 抽取和画布迁移也放在这一子项，确保画布流行为不变。验证方式：
  - Go DB 测试：块和事件一致；崩溃重做时 upsert 不重复；gateway 重试后以 `set` 覆盖；停止后所有块都是 `interrupted`。
  - SSE 测试：快照之后、订阅之前恰好提交的事件不会丢；游标过期返回 409；NOTIFY 丢失时由心跳兜底。
  - `realtime` 单测：唤醒丢失、合并、重复都无害；LISTEN 断线重连后补发唤醒；超过连接上限被拒；慢消费者被断开。画布 SSE 现有测试和前端画布同步 e2e 不改断言即可通过。
- **V6-030.2 前端块渲染与流**：运行 store、`watchRun`、各块组件、Markdown、折叠、v6 样式。验证方式：
  - 单测：合并规则、缺口回读、Markdown。
  - mock e2e：流式时展开状态不被重置；刷新后顺序一致；断流重连后内容完整；收起面板后仍在跟踪。
  - 回归有效性：把代码换回改动前，确认上述 e2e 会失败。

AC-03 的缩略图：目前还没有能产出节点的写工具（那是 034 和 036 的范围），本条用 mock 的 `node_refs` 验证渲染，真实的数据链路在 034 和 036 时补上证据。

## 7. 扩展性与已知上限

扩展成本低的部分：

- 新交互卡片：在块类型注册表里加一项，前端加一个组件。需要用户回应的，复用 respond 入口。
- 其他业务需要推送：实现一个 `Source`。
- 并行 run：只需放开 slot 的限制。
- 新供应商：接入 Sink 并提供公开摘要。
- 按账号分片：所有键都以 `account_id` 开头，可以直接按账号分。

需要提前知道的上限，以及到时候的扩展路径：

- **数据量**：事件保留 7 天，块跟随 run 永久保留。估算一年约 2 亿行，届时按月分区或归档冷数据。会话和 run 的保留期是产品规则，SaaS 阶段要补上，本期不定。
- **单个 run 的事件吞吐**：取号依赖 run 行锁，单个 run 大约能支撑每秒上百个事件。以后工具并行执行时，如果在这里排队，就改成按 step 预分配号段。
- **推送规模**：按 §2.3 的切换条件换成 Redis，只替换 Notifier 和缓存层。
- **会话分支**（编辑后重发、分叉）：messages 是按会话线性排序的。要支持分支，需要给 messages 加 `parent_message_id`，上下文按祖先链组装。重答（V6-042）走新建 run，不依赖分支。
- **子 Agent 嵌套**：子任务作为子 run，父 run 的工具块 payload 带上 `child_run_id`，前端在这一行下面嵌套渲染子 run 的块，不需要改表。嵌套后的折叠规则到时另行设计。

设计约束：

- messages 是模型上下文，块是界面视图，两份记录只在 step 结束时对齐。以后凡是改动已发生内容的功能（编辑历史回复、删除或隐藏某个块、撤回），都要同时规定两边如何变化，不能只改其中一边。
- 块只追加、不删除、不重排。过程段的划分和展开状态都依赖这一点。确实需要隐藏的，用 `hidden` 或 state 表达。

## 8. 已拍板（owner 2026-10-09，均按推荐）

结论已回写 PRD AS-01、AS-06 和 §11.3 #11、#12，V6-030 AC-02 同步改为按段。

- **D1**：思考块只来自供应商给出的公开 summary；DeepSeek 的 `reasoning_content` 不展示，只作为模型上下文随消息保存并原样回传（2026-10-09 修订）。见 §1.3。
- **D2**：Markdown 自己实现一个子集，直接生成 React 元素，不引入依赖。
- **D3**：拆成 030.1 后端、030.2 前端两次提交。
- **D4**：按过程段收起，不按整轮收起。见 §1.4。
- **D5**：SSE 作为平台基础设施 `platform/realtime`（Notifier / Source / Serve），画布流一并迁移；本期唤醒用 PG NOTIFY，不引入 Redis，切换条件见 §2.3。
- **D6**：块 payload 按类型带版本（`payload_version`）；gateway Sink 改为带类型的增量 `Delta(kind, text)`，本期只实现 text。
