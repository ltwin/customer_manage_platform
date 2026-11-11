# Design

## 设计上下文与关键决策

依据：[proposal](proposal.md)（已批准）；现有取消/核实设计见 `docs/dev/creative-agent.md`「CancelRun」段、`docs/product/creative-canvas-system/modules/harness.md` §5。

现在的取消有两段：`cancel` 遇到未定论的模型请求时进入 `reconciling`，并保留会话槽位；`close-reconciliation`（「结束等待」）再把运行结束，未知费用交给 Gateway 在后台核实。第二段已经证明，**运行进入终态与费用核实互不依赖**：终态之后，sweeper 会继续刷新 `settlement_state`（`control.go:sweepRun` 的 terminal 分支），迟到的结果被 `AbandonCallerGroupResultsInTx` 放弃，预算 hold 保留到核实结束。

采用的方案：

1. **取消一步进入终态**：`cancel` 不再走 `reconciling` 分支，直接执行现有的 `endControlledRun`。不新增状态，也不新增收尾逻辑，只删掉一个分支。
2. **worker 靠实时通知中断**：复用 `store.SubscribeRealtime`（`realtime.Notifier`），订阅 `agent_run` topic。取消事务会写 `run.finished` 事件，事件表上的触发器会 `pg_notify`。worker 被唤醒后读一次运行行，发现取消就 `cancelAttempt()`。现有的 10 秒续租检查保持不变，作为兜底。`SubscribeRealtime` 会一直阻塞到 LISTEN 就绪（`realtime_events.go:90`；连不上时 hub 无限重试），所以订阅在监听协程里异步建立，主执行和续租从不等它（方案评审第 1 条）。
   - 没选「缩短续租间隔」：续租是写事务，改成 1 秒一次，每个运行的写入量会放大 10 倍，而且仍然有 1 秒延迟。
   - 没选「API 进程直接通知 worker」：两者是不同进程，也可能在不同主机（见共享任务队列），数据库通知是现有的跨进程通道。
3. **前端乐观停止**：点击时把运行的当前视图冻结成快照存进 store，停止期间渲染这份快照。流事件照常同步到真实视图，但屏幕上的文字不再增长（方案评审第 3 条）。流里读到终态后撤掉快照，显示服务端保存的内容；失败时撤掉快照，显示最新的真实视图。取消请求不走 `perform`（它会锁住整个输入框），直接发出。cancel 按运行身份幂等，不需要 pending chain 做恢复。
4. **HTTP 成功不等于已结束**：cancel 的幂等回执保留 90 天，命中就原样回放（`creativeops/executor.go:48`），所以升级前产生的 `reconciling` 回执仍会被重放。前端以响应里的 `state` 为准：终态才算停完；非终态就换一个新 key 再发一次（新 key 走新逻辑，一定得到终态）。旧回执保持不可变（方案评审第 2 条）。

## 领域模型与业务规则

不新增领域对象，只改变运行状态机的一条边：

```
running/queued/waiting_*/reconciling --cancel--> cancelled | partial   （同一事务；原来 unsettled 时会 --> reconciling）
reconciling --close-reconciliation--> cancelled | failed               （保留，仅由非停止导致的 reconciling 使用）
```

不变量：

- I1：`cancel` 提交后，运行处于终态，`finished_at` 非空，会话槽位已释放，`execution_epoch` 已递增。
- I2：未知费用不被记为零。终态时 `settlement_state` 可以是 `unknown`/`pending`，之后只能由 Gateway 核实推进到 `settled`；推进时运行状态不变（`sweepRun` terminal 分支只更新 settlement）。
- I3：旧 epoch 的 worker 不能再写任何东西。现有的围栏不变：`admitRunInTx`、`textSink.flushLocked`、`completeAttempt` 都核对 `state=running AND execution_epoch`。
- I4：已经交给生成服务的生成照样跑完；还没发出的生成在取消提交**之后**尽力撤回（`CancelRun` 在 receipt 之后调用 `stopQueuedGenerations`，`control.go:111` 的注释写明：先一步派发的、或进程崩溃遗留的，会照原样生成）。本次沿用，不升级为派发侧强约束。
- I6：同一会话里，取消 A 之后立刻启动的 B 不受 A 迟到结果的影响：B 的槽位、消息和预算消费只属于 B。
- I5：存量运行里，`cancel_requested_at` 非空但还停在 `reconciling` 的，由 sweeper 在下一轮直接结束（这一条兼容部署前的数据）。

## 模块职责与协作

| 模块 | 本次职责 | 代码位置 |
|---|---|---|
| 运行控制 | 取消一步进入终态；sweeper 结束遗留的已取消运行 | `backend/internal/creativeagent/control.go` |
| 取消监听 | worker 执行运行期间订阅运行通知，发现取消或 epoch 改变就中断当前尝试 | 新增 `backend/internal/creativeagent/cancel_watch.go`，由 `execute.go:workRun` 启停 |
| 组合根 | worker 进程把 `*store.Store` 作为 `realtime.Notifier` 注入；API 进程不执行运行，不注入 | `backend/cmd/creative-worker/main.go` |
| 前端运行 store | `stopping` 冻结快照：点击时保存、读到终态时自动撤掉、失败时撤掉 | `frontend/src/creative-canvas/editor/agentRuns.ts` |
| 助手面板 | 发送/停止合一的按钮；发出取消；如有正在进行的停止，发送前先等它完成 | `frontend/src/creative-canvas/editor/AgentPanel.tsx` |
| 运行渲染 | 把 `stopping` 当作已停止渲染；去掉费用核实文案 | `frontend/src/creative-canvas/editor/AgentBlocks.tsx` |

主流程：

```
点停止 ─▶ freezeStopping(id, 当前视图) ─▶ 面板：按钮变回发送、渲染冻结快照、块标「已停止」、状态栏消失
       └▶ POST cancel ─▶ controlCommand: lock → stamp cancel
                          → endControlledRun（epoch+1、释放槽位、append run.finished）→ COMMIT
                     └▶ （提交后，best effort）stopQueuedGenerations
                     └▶ 响应 state 非终态（旧回执回放）→ 新 key 再发一次
                                    │ pg_notify(agent_run, id)
              worker watchCancel ◀──┘ 读 state/epoch/cancel → cancelAttempt() → 供应商请求 ctx 取消
                                    │                          → Gateway finishFailure: unknown, hold 保留
              前端流 ◀── run.finished ─ 读快照（cancelled）─▶ put() 时撤掉冻结快照，显示服务端内容
              sweeper（每 15 s）── terminal 分支 ── Gateway 核实后 settlement → settled
```

## 关键机制

### 取消事务（`control.go:controlCommand`）

删除这个分支：

```go
if c.unsettled() && !closeWaiting && now.Before(c.DeadlineAt) {
    err = s.moveControlledRun(ctx, tx, c, RunReconciling, "creative_run_cancelled", now)
}
```

其余代码保持不变：`cancel` 写入 `cancel_requested_at` 后走 `endControlledRun`，状态为 `RunCancelled`，有已交付回答时会被改写为 `RunPartial`。`close-reconciliation` 的前置条件（只接受 `reconciling`）和它的结果不变。幂等性来自 `creativeops.Executor`（同一 key 回放），以及 `terminalRun` 直接返回当前终态（新 key）。`CancelRun` 在 receipt 之后调用 `stopQueuedGenerations`，行为不变。重复调用时，`StopAgentGeneration` 是按 seed 幂等的。

### 遗留已取消运行（`control.go:sweepRun`）

把条件 `c.cancelled != nil && !c.unsettled()` 改成 `c.cancelled != nil`：只要请求过取消，就结束为 `cancelled`。部署前停在 `reconciling` 的已取消运行，会在 sweeper 下一轮被结束（I5）。

### 取消监听（`cancel_watch.go:watchCancel`）

```go
// watchCancel ends the attempt once the run is stopped elsewhere, instead of
// waiting for the next lease renewal to find out.
func (s *Service) watchCancel(ctx context.Context, scope store.AccountScope, held claim, cancelAttempt context.CancelFunc) func()
```

- `s.notifier == nil`（API 进程、单测默认）时返回 no-op，完全依赖续租兜底。
- **立即返回**：函数本身只起一个 goroutine，然后返回 stop 函数，调用方 `workRun` 不会因为监听而阻塞。`SubscribeRealtime(watchCtx, scope, "agent_run", held.RunID)` 在 goroutine 里调用。`watchCtx` 是 attempt ctx 的子 ctx，stop 或 attempt 结束时都会被取消，所以 LISTEN 一直连不上时，订阅调用也会随 attempt 一起返回，不会泄漏。订阅出错（hub 停止、ctx 取消）就记一条 warn 后退出，只剩续租兜底。
- 循环：收到唤醒后，如果离上次检查不足 `cancelCheckInterval = 500ms`，就先等到满 500ms。然后用带 `cancelCheckTimeout = 2s` 的只读查询读 `state, execution_epoch, cancel_requested_at`。如果出现 `cancel_requested_at != nil`、`epoch != held.Epoch` 或 `state != running` 中的任意一种，就调用 `cancelAttempt()` 并退出。查询出错或超时就记 debug，继续等下一次唤醒。channel 关闭（notifier 停止）时退出。
- 限流原因：worker 自己的流式增量也会写运行事件、触发通知。channel 容量为 1，唤醒会自然合并，再加上 500ms 下限，每个运行最多每秒 2 次读。
- 退出条件包括 `watchCtx.Done()`；返回的 stop 函数会取消 `watchCtx` 并等待 goroutine 退出，和 `renewLease` 的写法一致。订阅调用也受 `watchCtx` 约束，所以 stop 的等待是有界的。

延迟预算：通知送达约 1 次 RTT，读一次约 1 次 RTT，加上最多 500ms 限流。对远端 PG（约 50ms RTT）来说，总共在 1 秒以内，满足验收的 2 秒。

### 前端停止覆盖（`agentRuns.ts`）

```ts
interface RunsState { runs; open; stopping: Record<string, RunView | undefined> }
// 点击时冻结当前视图；没有读到视图（还在读快照）时冻结为空块列表
export function freezeStopping(id: string)
export function releaseStopping(id: string)
// put(): view.run 已是终态时同时删除 stopping[id]——之后显示服务端保存的内容
// 渲染：useAgentRuns(s => s.stopping[id] ?? s.runs[id])；follow() 始终写 runs，不碰 stopping
```

面板：

```ts
const active = live.find((run) => isActiveRun(run) && !stopping[run.id])
const waiting = live.find((run) => run.state === 'waiting_input' && !stopping[run.id])
function stop(run: AgentRun) {
  freezeStopping(run.id)
  stopRequest.current = cancelUntilFinished(run.id).then(
    () => true,
    () => { releaseStopping(run.id); setError('停止没有成功，请重试'); return false },
  )
}
// 最多两次：第一次响应非终态（旧版本回执回放）就换新 key 再发
async function cancelUntilFinished(id: string) {
  for (let i = 0; i < 2; i++) {
    const step = agentStep(`/agent-runs/${id}/cancel`, {})
    const reply = await api.send<AgentRun>(account, step.path, stepBody(step), step.operation)
    if (isFinished(reply)) return reply
  }
  throw new Error('run still active after cancel')
}
// sendDraft/sendCommand：先 await stopRequest.current；false 时不发送，草稿保留
```

停止完成后显示的内容：服务端保存的块，可能比点击瞬间多出「点击 → 取消提交」之间已落库的尾部文字（正常 < 0.5 秒）。理由：这部分已进入运行记录，刷新页面和下一轮模型上下文看到的都是它；如果前端一直藏着，界面与助手实际“说过的话”不一致。见未决事项。

发出到建好运行之间（`outgoing` 已画出、还没有 `outgoing.run`）也按运行中渲染（owner 验收反馈，前端响应优先）：

- 状态栏 `RunStatus` 在没有运行时显示「正在处理」，消息下方画 `TurnLoading`。
- 停止键此时可点（不受 `perform` 的 `busy` 锁影响）：只记下 `stopOnStart` 并立即显示「已停止」，不中途掐断请求链（链中途中断会让结果不确定）；`perform` 拿到新运行时立刻对它调用 `stop()`。链失败且已请求停止时不显示错误，草稿照常放回。
- 每次新发送清掉上一次的 `stopOnStart`。

按钮决策表：

| 运行状态（扣除 stopping） | 草稿 | 按钮 | 点击 |
|---|---|---|---|
| 无运行、刚发出未建好 | 任意 | 停止（可点） | 记下 `stopOnStart`，立即显示已停止 |
| 无运行 | 任意 | 发送（按现有 `sendable` 禁用） | 发送 |
| `queued`/`running`/`waiting_client`/`reconciling` | 任意 | 停止 | `stop(active)` |
| `waiting_input` | 空 | 停止 | `stop(waiting)` |
| `waiting_input` | 非空 | 补充并继续 | 提交补充 |

`RunTurn` 渲染 `stopping[runID] ?? runs[runID]`，`finished = isFinished(run) || !!stopping[runID]`。stopping 时显示「已停止」备注，块按 settled 渲染（不再闪光）。

## 接口与数据契约

- 契约状态：**不变**（结构）。`cancelCreativeAgentRun` 的语义收窄为「新版本首次执行就进入终态」：状态枚举、字段和错误码都不变。同一 `Idempotency-Key` 的回放返回首次执行时的快照，所以升级前产生的回执仍可能回放出 `reconciling`。调用方以响应里的 `state` 为准，非终态时用新 key 重发。唯一的消费者是本前端；外部助手（MCP）不暴露 cancel。`close-reconciliation` 保留，不改。
- 变更：只改 `api/openapi.yaml` 里 cancel 的 `summary` 和 `200` 的说明文字，然后 `make generate` 生成，同时提交生成物。没有数据库迁移，也没有新字段。lti contracts 里没有 agent 端点的章节，因此没有 contracts.md delta。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativeagent/control.go:controlCommand` | 修改 | 删除 cancel 转入 reconciling 的分支 |
| `backend/internal/creativeagent/control.go:sweepRun` | 修改 | 只要请求过取消就结束为 cancelled |
| `backend/internal/creativeagent/cancel_watch.go` | 新增 | `watchCancel`、`cancelCheckInterval` |
| `backend/internal/creativeagent/execute.go:workRun` | 修改 | 与 `renewLease` 并列启动和停止 `watchCancel` |
| `backend/internal/creativeagent/service.go:Service` | 修改 | `notifier realtime.Notifier` 字段与 `SetNotifier` |
| `backend/cmd/creative-worker/main.go` | 修改 | `agent.SetNotifier(db)` |
| `backend/internal/creativeagent/control_test.go` | 修改 | `TestCUnknownWaitsForReconciliationAndCanBeClosed` 改写为取消直接终态；新增重复取消、遗留 reconciling 被 sweeper 结束、旧 reconciling 回执回放后新 key 得到终态、取消 A → 同会话启动 B → A 迟到返回 |
| `backend/internal/creativeagent/cancel_watch_test.go` | 新增 | 流式中取消，2 秒内供应商 ctx 被取消；notifier 为 nil、订阅立即报错、订阅一直不返回三种情况下 `workRun` 照常执行、由续租兜底，attempt 结束后监听协程退出 |
| `api/openapi.yaml` | 修改 | cancel 的 summary/说明 |
| `backend/internal/platform/httpapi/api.gen.go` | 生成 | `make generate` |
| `frontend/src/api/schema.d.ts` | 生成 | `make generate` |
| `frontend/src/creative-canvas/editor/agentRuns.ts` | 修改 | `stopping` 冻结快照、`freezeStopping` / `releaseStopping`、`put` 读到终态时撤掉 |
| `frontend/src/creative-canvas/editor/AgentPanel.tsx` | 修改 | 发送/停止合一按钮、`stop`、发送前等待停止、`RunStatus` 去按钮 |
| `frontend/src/creative-canvas/editor/AgentBlocks.tsx` | 修改 | stopping 当作已停止；去掉费用核实文案 |
| `frontend/src/creative-canvas/editor/agentState.ts` | 修改 | 删除 `hasUnsettledCost`；`creative_reconciliation_closed` 文案改为「已停止」 |
| `frontend/src/creative-canvas/editor/agent.css` | 修改 | 停止态的按钮样式；删除状态栏按钮样式 |
| `frontend/scripts/creative-agent.test.ts` | 修改 | 跟随文案和导出的变化 |
| `frontend/scripts/agent-stop.e2e.mjs` | 新增 | mock HTTP：按钮切换、cancel 延迟 3 秒且期间持续推送文本事件（断言文字不变）、cancel 失败回滚到最新真实视图、首次响应非终态时用新 key 重发、停止中发送会等待 |
| `docs/dev/creative-agent.md` | 修改 | CancelRun 段改为一步终态 + 实时中断 |
| `docs/product/creative-canvas-system/modules/harness.md` | 修改 | §5 取消说明 |
| `docs/product/creative-canvas-system/modules/agent-api-events.md` | 修改 | cancel / close-reconciliation 两行 |

## 验证与交付

| 验收 / 不变量 | 验证 | 判据 |
|---|---|---|
| 流式中取消直接进入终态（AC「停止立即生效」第 3 条、I1、I2） | 改写后的 `TestCUnknownWaitsForReconciliationAndCanBeClosed`（假供应商返回 unknown 后取消） | `cancelled`，settlement 为 `unknown`，槽位为 0，pending consumer 保留，sweeper 后不重派 |
| 迟到结果不写入（I3） | 现有 `TestCCancelDuringDispatchDoesNotPublishLateAnswer` 不改，只回归 | 没有 assistant 消息，sweeper 后 settlement 为 `settled` |
| 新旧运行重叠（I6） | 新增：A 的假供应商阻塞 → 取消 A → 同会话启动并跑完 B → 放行 A 返回结果 → 跑 sweeper | B 成功，assistant 消息只有 B 的；槽位最终为 0；A 的用量只记在 A 的请求上，B 的预算消费不含 A；A 仍是 `cancelled` |
| 重复取消幂等 | 新增：同一运行先后用两个 key 取消 | 两次都返回同一终态，`StopAgentGeneration` 调用不出错 |
| 旧回执回放 | 新增：插入一条 `agent.cancel_run` 的 `reconciling` 回执，用原 key 重放，再用新 key 取消 | 原 key 返回 `reconciling`（回执不可变）；新 key 返回终态 |
| 遗留数据（I5） | 新增：构造 `reconciling + cancel_requested_at` 的运行，跑 sweeper | 变成 `cancelled`，槽位释放 |
| worker 2 秒内中断 | 新增 `cancel_watch_test.go`：假供应商阻塞在流里，记录 ctx 取消时刻；另一协程提交 cancel | 取消提交到 ctx.Done 的间隔 < 2s |
| 监听降级不阻塞 | 同一文件：注入三种假 notifier——nil、立即报错、`SubscribeRealtime` 阻塞到 ctx 取消 | 三种情况下模型调用都照常发出；取消后在续租时中断（测试用缩短的 lease 参数）；attempt 结束后监听协程退出（用 done 信号断言） |
| 前端按钮、乐观停止、冻结文字、失败回滚、旧回执重发、停止中发送 | `agent-stop.e2e.mjs`，一次跑一个用例，换端口 | 见 proposal 前两组 AC；cancel 延迟期间推送文本事件，块文字和点击时一致 |
| 文案 | `creative-agent.test.ts` | 没有「费用仍在核实」字样 |

门禁：后端 `make check-go PKG=./internal/creativeagent/...`；前端 `make check-frontend`；OpenAPI 改完跑 `make generate` 并确认没有漂移。合回 develop 时按 check-merge。

部署与回滚：没有迁移。回滚代码后，新语义下已经结束的运行仍是合法终态，不需要处理数据。worker 进程会多一条 LISTEN 连接（每个进程一条，首次订阅时建立）。

交付顺序：后端（控制 + 监听）→ OpenAPI 生成 → 前端 → 文档。前后端可以分别提交，但最后要一起合入。发布顺序：API 和 worker 都更新完成后，再发布前端。只上线前端时，停止后仍可能停在 `reconciling`，用新 key 重发也只会回到 reconciling，所以前端不能先于后端上线。

## 已决事项

- 停止完成后显示服务端已保存的内容，包括「点击 → 取消提交」之间落库的尾部文字（owner 2026-11-10 批准；spec 与 proposal 已按此 `lti revise`）。
