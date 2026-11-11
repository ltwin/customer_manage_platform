# Design

## 设计上下文与关键决策

依据：[proposal](proposal.md)、[specs](specs.md)、[contracts](contracts.md)。运行状态机与取消沿用 `agent-stop-immediately`（已合入 develop）。owner 拍板：

- 排队只存前端（2026-11-10）。
- 插队落空时同一运行再跑一轮（2026-11-10）。
- 停止、失败后队列暂停（2026-11-10）。
- 补充和插队可以带引用（2026-11-10）。
- 摄影师亲手附带的内容由账号级同意覆盖（2026-11-11）。
- 方案评审第 1 轮的意见已全部纳入，本文是当前有效方案。

现有基础：

- `SupplementRun`（`backend/internal/creativeagent/waiting.go`）只在 `waiting_input` 时接受文字。它向对话追加一条 `role=user, run_id=运行` 的消息，在 `creative_run_inputs` 写一个 `batch_ordinal≥1` 的批次，然后让运行重新排队。
- `runtimeHandler.BeforeModelRewriteState`（同文件）每次调用模型前，把还没进入 Eino 状态的批次追加为用户消息，用 `Extra["creative_supplement"]` 去重。
- 模型结果入账（`planInTx` → `admitDispatchInTx` → `admitRunInTx`，`execute.go`）和 `SupplementRun` 都 `FOR UPDATE` 锁运行行，彼此串行。
- 运行进入终态时，`settleBlocksInTx` 把所有未完成的块记为通用终态 `interrupted`（`blocks.go`）。
- 前端的待确认链（`PendingAgentChain`，`agentState.ts`）先把请求原样落盘（路径、操作 ID、`client_created_at`、payload），再发出请求。结果未知时保留，可重放；明确被拒时撤销。`supplements` 已经走这条路（`AgentPanel.tsx:sendDraft`）。

采用的方案：

1. **插队用新接口 `POST /agent-runs/{id}/steers`**。补充要求 `waiting_input`、token 和 revision，补完后让运行重新排队；插队要求运行在进行中，不改变状态，也不校验 revision。两者共用写入输入批次的逻辑 `appendInputBatchInTx`。
2. **引用按账号级同意放行**：只核对四件事——本账号且可用（`creativecontent.RequireUsable`）、本轮授权没撤销、授权包含 `image_preview`、模型支持图片。不要求引用出现在建运行时的清单（`creative_egress_consent_contents`）里。写入的消息挂在本运行上，因此 `readableInTx` 的「本运行引用过」分支会让它在派发时可读。这是有意的规则，派发前照常复核撤销和可用性。外发契约同步补一条（见「接口与数据契约」）。
3. **截止点是「最终回答已入账」**：模型给出不含工具调用的回答并入账之后，这一轮视为结束，插队被拒（409），消息回到前端队首，作为下一轮发出。入账在前、插队在后的情况已经在运行行锁上串行。入账到关闭运行之间只有几毫秒的收尾，把截止点后移到关闭事务，就要在 `finishRun` 里重新进入执行，代价不值得。
4. **送达状态持久化在 `input` 块上**：插队或补充写入时追加一个 `input` 块，状态为 `pending`。这次运行的某次模型请求带上它时，在准备该模型步骤的事务里把它记为 `delivered`。
   - 正常收尾、续跑判定和 sweep 恢复，都用同一个持久判定「本运行还有 `pending` 的 `input` 块」。
   - 运行结束时还在 `pending` 的块，由 `settleBlocksInTx` 记为 `interrupted`，前端显示「助手没有读到这条」。
   - 这个块同时决定摄影师气泡在这一轮里的位置：块只追加、不重排。
5. **落空在同一次尝试内续跑**：`drainAgent` 成功之后，如果还有 `pending` 的 `input` 块，就用 `AfterAgent` 留下的最终消息列表再 `runner.Run` 一次。
   - 能否这样做依赖未决事项 U1，第一个任务先做穿刺验证。
   - 续跑开始前 worker 崩溃时不尝试恢复续跑：sweep 按上面的持久判定把运行结束为 `partial`，不会误标为 `succeeded`。
6. **前端发请求一律经待确认链**：插队和队首自动发出都走 `perform`。响应丢失时用同一个请求重放，不会换一个新的操作 ID 去重复发送。队列条目在交接期间标记为 `sending`，等发送链落盘后再移出队列，所以刷新时不会出现两边都没有的情况。

## 领域模型与业务规则

### 服务端：运行的追加输入

不新增实体。一次补充或插队，就是这次运行的一个**输入批次**（`creative_run_inputs`，`batch_ordinal≥1`，写入后不变），外加一条挂在本运行上的 `role=user` 消息和一个 `input` 块。

| 规则 | 内容 |
|---|---|
| R1 接受插队 | 运行处于 `queued` / `running` / `waiting_client`；未请求取消；未过期限；项目未归档；最新模型步骤不是终局（R2）；批次数小于 12（与补充共用 `maxSupplements`）；本轮授权未撤销、`policy_version` 有效、同一会话（`coversRun`）；每个引用通过 `RequireUsable(…, "display")`；有图片时授权含 `image_preview`，且模型支持图片输入 |
| R2 终局 | 本运行中 `kind='model'`、`ordinal` 最大的步骤状态不是 `prepared`，且输出里没有工具调用（`failed` / `unknown` 也算终局）。还没有模型步骤时不算终局 |
| R3 送达 | 已接受的批次，要么在本运行下一次模型请求里送达（`input` 块随即变为 `delivered`），要么由 R4 触发续跑。每个批次只送达一次（按 `Extra` 去重） |
| R4 续跑 | `drainAgent` 成功后，只要本运行还有 `pending` 的 `input` 块就续跑。续跑的模型请求照常计费，受期限和预算约束；每轮最多 12 个批次，所以最多续跑 12 次 |
| R5 停止优先 | 插队与取消按行锁顺序：取消先提交，插队被 R1 拒绝；插队先提交，取消照常结束运行，`input` 块变为 `interrupted` |
| R6 出错不续跑 | 运行以错误结束时不续跑。没送达的 `input` 块变为 `interrupted` |
| R7 恢复 | `sweepRun` 原来「已有最终回答且没有未完成步骤就记为 `succeeded`」的分支，增加一个条件：还有 `pending` 的 `input` 块时改记为 `partial`（错误码 `creative_steer_undelivered`） |

**R2 + R4 不丢的论证**：最终回答在 T1 入账（锁运行行）。

- 插队在 T1 之前提交：它的 `input` 块此时是 `pending`。`drainAgent` 在入账提交之后才返回，所以 R4 一定能看到它并续跑。
- 插队在 T1 之后提交：它读到的最新模型步骤已经是终局，被 R1 拒绝。

所以不存在「已接收，却既没送达也没续跑、只被悄悄丢掉」的插队。只剩两种结果：要么被读到，要么被标为 `interrupted` 并显示出来。

已知的窄窗口：R4 决定续跑之后、续跑建出新模型步骤之前，最新步骤仍是终局，这时到达的插队会被拒绝并回到队首。运行随后以 `succeeded` 结束，这条消息会作为下一轮发出，不会丢。

### 前端：消息队列

```ts
// agentQueue.ts
interface QueuedMessage {
  id: string
  parts: MessagePart[]   // 队列与气泡里显示的内容
  draft: SavedDraft      // 与 PendingAgentChain.draft 同形
  send: { model: string; imageInput: boolean; revisionIDs: string[]
          segments: Segment[]; scene: SceneContext | null }   // 原样交给 messageSteps
  steer: Segment[] | null   // 去掉 skill_ref 的片段；选了 Skill 时为 null
  hasImage: boolean
  sending?: true            // 正在交给待确认链
}
interface ConversationQueue { items: QueuedMessage[]; paused: boolean; settledRun?: string }
```

按对话存进 `sessionStorage`（`cc-agent-queue:<conversationID>`），读写都包在 try/catch 里；存储不可用时退化为只保存在内存。

`nextQueueAction(queue, latestRun, busy)`，在运行快照变化时求值（用 `shownRun`，所以乐观停止也算数）：

| 最新运行 | 条件 | 动作 |
|---|---|---|
| 活动中或 `waiting_input` | — | 无 |
| `succeeded` | `settledRun ≠ run.id`，未暂停，队列非空，面板不忙，没有待确认链 | `settledRun = run.id`；队首标记 `sending`，交给 `sendMessage` |
| `cancelled` / `failed` / `partial` | `settledRun ≠ run.id` | `settledRun = run.id`；`paused = true` |
| 任意终态 | `settledRun = run.id` | 无（重连、重复快照、刷新都不会重发） |
| — | 点「继续发送」 | `paused = false`，同第二行 |

**发送交接**：
- `sendMessage(saved, send, origin)` 是从 `sendDraft` 抽出的组链与 `perform` 代码，输入框和队列共用，所以请求与直接发送完全一致。
- `origin = queue item id` 时，链的 `draft` 带上 `queueItem` 字段；待确认链落盘后，回调把这一条从队列移出。
- 刷新后遇到带 `sending` 的条目：如果存在 `draft.queueItem` 等于它的待确认链，就移出队列（由链负责后续）；否则去掉 `sending`，它仍在队首（链还没落盘，说明请求没有发出）。
- 明确被拒时：如果来源是队列，放回队首并 `paused = true`，不回填输入框；来源是输入框的，行为不变。

**插队**：
- 运行在进行中时，走 `perform(single('/agent-runs/{id}/steers', {instruction_segments}, saved))`；运行处于 `waiting_input` 时，走 `perform(single(supplements, {expected_revision, waiting_token, instruction_segments}, saved))`。条目在交接期间同样标记 `sending`。
- 链落盘的同一帧，条目移出队列，`RunTurn` 末尾出现乐观气泡。
- 202：快照的 `revision` 达到响应的 `revision` 时，`input` 块已经在快照里（同一事务写入），撤掉乐观气泡。
- 结果未知：链保留，显示现有的待确认提示和「重新发送」，重放同一个请求。
- 明确被拒（409 / 413 / 422 / 403）：撤掉气泡，放回队首并提示，提示文案按运行状态区分（见 proposal）。

插队按钮可用的条件：`steer !== null`；`!(hasImage && !model.imageInput)`；最新运行在进行中或处于 `waiting_input`；没有待确认链。

## 模块职责与协作

| 模块 | 本次职责 | 代码位置 |
|---|---|---|
| 运行输入 | `appendInputBatchInTx`：校验引用，写入消息、批次和 `input` 块；`SupplementRun` 接受片段 | `backend/internal/creativeagent/waiting.go` |
| 插队 | `SteerRun`、`finalModelStepInTx`（R1/R2） | 新增 `backend/internal/creativeagent/steer.go` |
| 输入送达 | 中间件按批次组装消息（与 `assemble` 一致：文字一条，每个引用经 `resolveReference` 各一条，同一批次打相同的 `Extra`），并把本次带上的批次交给模型步骤 | `waiting.go:runtimeHandler.BeforeModelRewriteState` |
| 送达标记 | 准备模型步骤的事务里，把本次请求带上的批次对应的 `input` 块记为 `delivered` | `backend/internal/creativeagent/execute.go:prepareModelStep` |
| 续跑 | `AfterAgent` 留下最终消息；R4 循环 | `backend/internal/creativeagent/execution.go:runExecution.run`、`backend/internal/creativeagent/runtime.go` |
| 恢复 | R7 | `backend/internal/creativeagent/control.go:sweepRun` |
| 块注册 | `input` 类型（`pending` → `delivered`，通用终态 `interrupted`） | `backend/internal/creativeagent/blocks.go` |
| HTTP | 新路由接线 | `backend/internal/platform/httpapi/creative_agent.go` |
| 队列 | 状态、持久化、`nextQueueAction`、交接 | 新增 `frontend/src/creative-canvas/editor/agentQueue.ts` |
| 队列界面 | 列表、插队、删除、继续发送、置灰原因 | 新增 `frontend/src/creative-canvas/editor/AgentQueue.tsx`，`agent.css` |
| 面板 | 运行中有内容时按钮是「加入排队」；抽出 `sendMessage`；接入队列；被拒时不回填输入框 | `frontend/src/creative-canvas/editor/AgentPanel.tsx` |
| 运行渲染 | `input` 块渲染为摄影师气泡（`interrupted` 时附「助手没有读到这条」）；乐观气泡；对话列表跳过已由 `input` 块显示的消息 | `frontend/src/creative-canvas/editor/AgentBlocks.tsx`、`AgentPanel.tsx` |
| 步骤与链 | `steers` 加入可重放路径；`PendingAgentChain.draft.queueItem` | `frontend/src/creative-canvas/editor/agentState.ts` |

## 关键机制

### 插队写入

```go
// steer.go — Operation{Key: "agent.steer_run", Capability: "agent_start"}
Validate: 片段 1..InstructionSegments，type ∈ {text, content_ref}，文字合计 ≤ MessageBodyBytes
Apply:
  canvasID ← run.canvas_id                        // 不存在 → ErrNotFound
  LockProjectForWorkInTx(canvasID)               // 归档 → ErrConversationArchived
  c ← lockControlledRun(runID)                    // FOR UPDATE
  state ∉ {queued, running, waiting_client} 或已请求取消 → ErrRunState
  !now.Before(deadline) → creativeops.ErrExpired
  finalModelStepInTx(c.ID) → ErrRunState          // R2
  consent ← readConsentForUpdate; coversRun(consent, c.ConversationID, nil)
  appendInputBatchInTx(c, segments, consent, model)
  revision+1，runStateChangedInTx                // 前端拿到新 revision
  return runOutcome(run, 202)
```

`appendInputBatchInTx`：
1. 批次数已达 12 → `ErrLimit`。
2. 逐个引用：`RequireUsable(…, "display")`，失败按 `translateContent` 转换。是图片时：授权不含 `image_preview` → `ErrEgressRequired`；模型不支持图片或 `s.images == nil` → `ErrModelCapability`；图片数超过 `ImagePreviewCount` → `ErrLimit`。这些检查与 `CreateRun` 相同，只是**不查** `creative_egress_consent_contents`（决策 2）。
3. `appendMessageInTx(role=user, run_id, body)`，引用随之写入 `creative_message_content_refs`。
4. `creative_run_inputs`：文字行写 `body`；引用行写 `content_revision_id`、`read_level=text|image_preview`，**`body` 同时写入 `referenceLabel(id)`**。这样回滚到老 worker 时，它只会把引用当文字标签读，不会因为把 NULL 扫进 string 而报错（评审意见代码第 5 条）。
5. `input` 块：`createBlockInTx(kind=input, state=pending, source_key="input:<batch>", payload={message_id, body})`。

`SupplementRun`：`text` 与 `instruction_segments` 二选一；原来的「追加消息 + 写批次」改为调用 `appendInputBatchInTx`。

### 送达与续跑

```
BeforeModelRewriteState:
  未进入状态的批次（升序）→ 每批：文字一条用户消息 + 每个引用 resolveReference 一条，Extra 都标这个批次
                          （与 assemble 处理首批引用的方式相同，图片绑定沿用一条消息一个图片）
  h.pendingDelivery = 本次新带上的批次
TurnKey → prepareModelStep 事务: 把 h.pendingDelivery 对应的 input 块记为 delivered（幂等：只改 pending）

run():
  runErr = drainAgent(iterator)
  for runErr == nil && 本运行有 pending 的 input 块（读库，同 R7 的判定）:
      runner.Run(ctx, h.finalMessages(), adk.WithCheckPointID(runID)); runErr = drainAgent(...)
```

- 续跑与原执行共用同一个 handler、网关适配器和检查点存储。模型步骤序号接着库里的计数，`TurnKey` 绑定不受影响。续跑中途遇到中断（工具等待页面、等待补充）时，照常写检查点并走现有的恢复路径。
- 引用展开时把 id 加入 `h.revisions`，之后每次派发都会经 `admitDispatchInTx` 复核撤销和可用性。

### 前端交接时序（自动发出队首）

```
succeeded 快照 → nextQueueAction → 队首标 sending（落盘）→ sendMessage → perform
   → afterStop() → 待确认链落盘（draft.queueItem=id）→ 回调：队列移出该条（落盘）→ 发请求
任一时刻刷新：sending 条目 + 同 queueItem 的链 → 移出；无链 → 取消 sending
```

## 接口与数据契约

- 契约状态：**扩展**。详见 [contracts.md](contracts.md)：
  - `POST /creative/agent-runs/{id}/steers`。
  - `supplements` 的 payload 增加 `instruction_segments`。
  - `CreativeAgentBlock` 类型 `input`：payload、状态、关联方式，以及历史纯文字补充的显示规则。
  - 外发授权资源边界补充一条。
- 机器形式：改 `api/openapi.yaml` 后运行 `make generate`，提交 Go/TS 两份生成物。
- 数据：不加表、不加列，没有迁移。`creative_agent_blocks.type` 只有格式 CHECK（迁移 0056）；`creative_run_inputs` 已有 `content_revision_id`、`read_level`，CHECK 要求 `body` 和 `content_revision_id` 至少有一个，两个都写也合法。
- 产品契约文档：`docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」补写「摄影师在本运行中亲手附带或引用的本账号内容，由账号级同意（发送前说明）覆盖；仍校验归属、可用性、撤销与数据类别」。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `api/openapi.yaml` | 修改 | steers 路径与 schema；Supplement payload；`input` 块 payload |
| `backend/internal/platform/httpapi/api.gen.go` | 修改（生成） | — |
| `frontend/src/api/schema.d.ts` | 修改（生成） | — |
| `backend/internal/platform/httpapi/creative_agent.go` | 修改 | `SteerCreativeAgentRun` |
| `backend/internal/creativeagent/steer.go` | 新增 | `SteerRun`、`finalModelStepInTx` |
| `backend/internal/creativeagent/waiting.go` | 修改 | `appendInputBatchInTx`；`SupplementRun`；`BeforeModelRewriteState` |
| `backend/internal/creativeagent/execute.go` | 修改 | `prepareModelStep` 记录送达 |
| `backend/internal/creativeagent/execution.go` | 修改 | `runExecution.run` 续跑 |
| `backend/internal/creativeagent/runtime.go` | 修改 | `AfterAgent`、`finalMessages`、`pendingDelivery` |
| `backend/internal/creativeagent/control.go` | 修改 | `sweepRun` R7 |
| `backend/internal/creativeagent/blocks.go` | 修改 | 登记 `input` |
| `backend/internal/creativeagent/steer_test.go` | 新增 | 见验证表 |
| `frontend/src/creative-canvas/editor/agentQueue.ts` | 新增 | — |
| `frontend/src/creative-canvas/editor/AgentQueue.tsx` | 新增 | — |
| `frontend/src/creative-canvas/editor/AgentPanel.tsx` | 修改 | — |
| `frontend/src/creative-canvas/editor/AgentBlocks.tsx` | 修改 | — |
| `frontend/src/creative-canvas/editor/agentState.ts` | 修改 | — |
| `frontend/src/creative-canvas/editor/agent.css` | 修改 | — |
| `frontend/scripts/creative-agent.test.ts` | 修改 | `nextQueueAction`、交接恢复 |
| `frontend/scripts/agent-queue.e2e.mjs` | 新增 | — |
| `docs/product/creative-canvas-system/data-model.md` | 修改 | 外发资源边界 |
| `docs/dev/creative-agent.md` | 修改 | 插队、补充片段、送达、续跑 |
| `docs/product/creative-canvas-system/modules/agent-api-events.md` | 修改 | steers 行、`input` 块 |

## 验证与交付

| 验收 / 不变量 | 验证 | 判据 |
|---|---|---|
| R1 / R2 | `steer_test.go` 表驱动：各运行状态、已请求取消、已过期、最新步骤为 prepared / 带工具调用 / 终局 / failed / 还没有步骤 | 接受和拒绝与表一致，拒绝时不写入任何东西 |
| 截止点两种顺序（并发） | 用脚本化 stubVendor 卡住最终回答入账，同时发起插队，分别测入账前提交、入账后提交；`-count=10` | 前者续跑一次，模型输入里包含插队文字，`input` 块变为 `delivered`；后者返回 409 |
| 送达不打断工具 | 在 stub 工具执行期间插队 | 工具正常完成；下一次模型请求里这一批次只出现一次 |
| R5 / R6 | 插队与取消竞争；插队后让模型出错 | 运行终态正确；未送达的 `input` 块为 `interrupted` |
| R7 | 让最终回答入账后、续跑前的进程退出，然后运行 sweep | 运行为 `partial` / `creative_steer_undelivered`，不是 `succeeded` |
| 引用 | 本账号可用、但不在清单里的修订（评审给的反例）→ 接受，模型请求里有图片；其他账号的修订 → 拒绝；授权已撤销 → 409；模型不支持图片 → 422 | 全部符合 |
| 多图顺序 | 片段「文字、图 A、文字、图 B」，抓取网关请求 | 两张图都在，顺序与片段一致 |
| 回滚兼容 | 对新写入的引用批次执行老查询（`batch_ordinal,body` 扫进 string） | 不报错，读到的是标签文字 |
| 幂等 | 同一个操作 ID 发两次；同一个 ID 先用于 steers 再用于 supplements | 前者只写一次；后者返回 409 `idempotency_conflict` |
| 补充兼容 | 老的 `text` 补充；新的片段补充；两者都给 | 行为不变；带片段；400 |
| 队列决策表与交接 | `creative-agent.test.ts`：`nextQueueAction` 每一行；重复快照；`sending` 条目在有链和无链两种情况下的恢复 | 全部通过 |
| 交互 | `agent-queue.e2e.mjs` 逐个用例（`AGENT_QUEUE_E2E_ONLY`）：回车排队同帧清空；成功后只发一条；停止或 partial 后暂停并能继续发送；插队同帧出现气泡；409 回队首；响应丢失后重放同一请求；`waiting_input` 达到上限时的提示；Skill / 图片置灰；刷新后队列还在；交接中刷新；请求与直接发送一致 | 每个用例单独通过 |
| 回归 | `agent-stop.e2e.mjs`、`agent-send.e2e.mjs`、`make check-go PKG=./internal/creativeagent/...` | 通过 |

- 交付顺序：U1 穿刺 → 块注册、批次写入、补充扩展、插队 → 送达标记、续跑、R7 → OpenAPI 与生成 → 队列模块与单测 → 面板与界面 → e2e → 文档。
- 观测：插队被拒（附原因）、续跑、R7 记为 `partial`，各打一条带 `run_id` 的日志。
- 回滚：没有迁移。老前端遇到未知的 `input` 块按约定降级显示；老 worker 把引用批次读成标签文字（见上）。

## 未决事项

无。U1（续跑能否以 `AfterAgent` 的消息作为 `runner.Run` 的输入）已在任务 1.1 穿刺验证可行：
- 框架会把指令作为系统消息放进状态，`finalMessages` 去掉系统消息，由框架重新加上。
- `Extra` 原样保留，插队只送达一次。
- 续跑停在工具上时，检查点和恢复照常工作。
- 「续跑前崩溃」的用例依赖 R7，并入任务 3.1。
