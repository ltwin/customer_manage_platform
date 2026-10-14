---
doc_type: feature-design
feature: 2026-10-14-agent-parallel-tools
requirement: 无编号（owner 2026-10-14 提出：互不相连的节点批量生成要能并行，批量出创意图、后续视频分镜都需要）
status: draft
date: 2026-10-14
revision: 3（按 S1 设计审计与自审修订，见 §10、§11）
---

# 助手工具按资源并行执行

## 0. 结论

| 决定 | 内容 | 来源 |
|---|---|---|
| 目标 | 模型一次回复里的多个工具调用，**能证明互不影响的**同时执行；有影响或无法证明的，按模型给出的顺序执行，结果与串行完全一致 | owner 2026-10-14；S1 审计 |
| 调度依据 | 每个工具声明它读、写哪些资源（共享 / 独占）；两个调用争同一资源且至少一方要写，后一个等前一个结束 | 本设计 §2；借鉴 buzzy `toolfx` 的 `ExclusiveKeys`，改为读写区分 + 两两判依赖 |
| 资源从哪来 | 参数里的节点 ID，加上批次开始时读到的画布连线：生成会读取上游节点的当前内容，所以上游节点也算它读的资源 | 本设计 §3；S1-1 |
| 默认值 | 没有声明、参数非法、或资源无法确定的调用，独占整个运行，与所有调用串行 | 本设计 §2.3 |
| 有关联的生成 | 保持原串行语义：B 引用 A 时，同批的「生成 A、生成 B」先 A 后 B | S1 推荐；owner 2026-10-14 确认 |
| 典型效果 | 一次回复里对 6 个互不相连的节点调用 `generate_node`，6 个同时生成；读画布以外的查询类工具之间并行 | 本设计 §3 |
| 画布写入 | `edit_canvas`、`set_node_prompt` 仍然一个接一个 | 本设计 §3 |
| 执行方式 | 打开 Eino 的并行模式，在已有的 `WrapInvokableToolCall` 里按依赖等待；依赖由步骤日志和当时的画布连线算出 | 本设计 §4.1 |
| 并发上限 | 同一运行同时执行的调用最多 8 个；在等依赖的调用不占名额 | 本设计 §4.1 |
| 准入时机 | 每个调用在依赖与名额都就绪后、真正执行前一刻过运行准入，与串行时相同；等待期间运行被取消，调用不会再开始 | 本设计 §4.1；自审 R1 |
| 停止 | 分两种：同批某个调用让运行结束时，停止派发、已开始的正常收尾；取消、撤权、租约失效时，沿用现有协议，旧执行者不发布任何结果 | 本设计 §4.4；S1-2、S1-3 |
| 生成能否取消 | 请求已发给供应商的生成不能取消，照常完成、落到节点、计费；还没发出的（在网关排队）可以终止。摄影师停止 Agent 时，本次运行发起、尚未发出的生成随之终止 | owner 2026-10-14（参照 LibTV：已开始执行的节点无法停止）；本设计 §4.6 |
| 不新增 | 不改 OpenAPI、不加迁移、不改工具契约和版本号、前端不改、不加进程锁、不引入队列或工作流引擎 | S1-5 及审计结论 |

本期不做（见 §8）：

- 同批新建节点自动打组
- 前端批次卡片
- 按账号、按订阅档位的生成并发上限
- 整批积分预检
- 视频的异步等待

## 1. 现状

2026-10-14 核对，代码基线 `cf6887e`（`feat/creative-workspace-redesign`）。

| 事实 | 位置 | 对本设计的影响 |
|---|---|---|
| 所有工具串行：`ExecuteSequentially: true` 是全局开关 | `creativeagent/runtime.go` 的 `configure` | 本设计改掉它 |
| Eino 并行模式：同一批调用全部起 goroutine，无并发上限；结果按调用顺序组装；任一调用出错，要等全部结束才返回 | Eino v0.9.19 `compose/tool_node.go` 的 `parallelRunToolCall` | 并发上限、依赖等待、停止传播都由我们自己做 |
| Eino 串行模式：出错后仍会继续执行后面的调用 | 同上 `sequentialRunToolCall` | 新设计更收敛（§4.4） |
| Eino 从中断状态恢复时，已执行过的调用**不会再进入包装器** | 同上 `Invoke` 的 `executedTools` | 已完成信号不能靠回放路径关闭（§4.1，S1-4） |
| 模型返回时，每个工具调用按 `tool_call_index` 先落为 `prepared` 步骤，序号连续 | `runtime.go` 的 `planInTx` | 批次成员与顺序可以从步骤日志重建 |
| `callTool` 先查本批全部步骤，已完成的直接回放；I/O 不持锁；收尾事务重新过准入，然后写上下文项、更新步骤、追加工具消息、写工具块 | `runtime.go` 的 `callTool` | 在这里插入依赖等待 |
| `h.step` 记录「正在执行的那个调用」，写入类工具用它派生幂等身份（`seed = RunID/step`） | `runtime.go` 的 `toolStep`；`canvas_tools.go`、`library_journal.go`、`search_tool.go`、`webpage_tool.go` | 并行后会串号，改成随 `ctx` 传递（§4.2） |
| 生成的参考在受理时按**实时**画布取：连入的边和带 `source_node_id` 的输入，取上游节点当时的 `content_revision_id` | `creativecanvas/generation_prompt.go` 的 `orderedGenerationRefs` | 生成读上游节点；资源不能只看参数（S1-1） |
| 存入素材库读取节点**当前**内容修订 | `creativecanvas/media_ports.go` 的 `SaveNodeToLibrary` | 保存读目标节点（S1-1） |
| 节点生成的准入只看目标节点的提示词版本；结果采用只看目标节点自己（ada89a3） | `creativecanvas/generation.go`、`publish.go` | 不相连节点的生成可以安全并行 |
| 画布写入带整张画布的拓扑修订和读集合；冲突时重读重试最多 3 次 | `canvas_tools.go` 的 `applyOnce` | 画布写入保持串行 |
| 取消、结束运行：提升 `execution_epoch`，把所有 `prepared` 步骤置为 `failed`，并为未回答的调用补答 | `control.go` 的 `endControlledRun` | 在途工具的收尾事务会被准入拒绝，旧执行者不能发布结果（S1-3） |
| 普通写事务用 `pgx.TxOptions{}`，即数据库默认隔离级别；REPEATABLE READ 只用于只读快照和分享链路 | `platform/store/scope_tx.go` 的 `withinTx` | 开发库实测 `default_transaction_isolation = read committed`（PostgreSQL 16.15）；仓库与部署配置都没有改过它。抢行锁的事务会排队等待并读到最新行，不会报 40001（S1-5） |
| 运行准入检查运行状态、epoch、`cancel_requested_at` 与租约；取消请求要等下一次续租（每 10 秒）失败，执行者的 `ctx` 才会被取消 | `execute.go` 的 `admitRunInTx`、`renewLease`、`leaseRenewal` | 取消后最长约 10 秒内 `ctx` 仍然有效，只有准入能及时拦住新调用；准入必须紧贴执行（§4.1，自审 R1） |
| 生成受理只校验目标节点和它的上游节点的修订（`required`），不校验整张画布的读集合 | `creativecanvas/generation.go` 的 `validateReadSet(in.ReadSet, required, ids)` | 不相连节点的生成并行时，彼此的发布不会让对方受理冲突 |
| `list_conversation_assets` 列出本对话出现过的内容，**包括助手读过或成功采用的生成结果**，即同批前面调用写入的工具消息 | `asset_tool_info.go` 的说明、`library_tools.go` | 它依赖同批前面所有调用的结果（§3，自审 R2） |
| `read_history` 只读本次触发消息之前的历史 | `context.go` 中按 `trigger_message_id` 截断 | 与同批调用无关 |
| 连接池用 `pgxpool.New` 默认值（`MaxConns = max(4, CPU 数)`），没有显式配置 | `platform/store/store.go` 的 `Open` | 每个运行最多 8 个调用同时开事务，需要验证没有「持有事务时再向连接池取连接」的写法（§4.3，自审 R3） |
| 检查点注册摘要包含工具列表与 `Limits`；摘要不符时明确停止 | `checkpoint.go` 的 `newRunCheckpoint`、`execute.go` 对 `errCheckpointVersion` 的处理 | 并发上限用包内常量，不进 `Limits`；没有兼容检查点的运行仍按现有规则明确停止 |
| `generate_node` 原地等结果，最多 150 秒（不超过运行截止前 45 秒） | `canvas_tools.go` 的 `generationWait` | 并行后 N 个生成一起等 |
| 网关生图通道限 4 路并发、每分钟 30 次 | `creativegeneration/compose.go` | 超过 4 个时在网关排队 |
| 网页读取、搜索按步骤序号统计此前的调用次数，限次结果确定；同链接回放要等前一个**已完成**才生效 | `webpage_tool.go` 的 `priorCalls` | 同一链接、同一搜索词声明独占 |
| 工具块 `prepared` 显示为「运行中」 | `blocks.go` 的 `toolBlockStates` | 前端不用改（§7） |

## 2. 调度模型

### 2.1 资源声明

```go
// claim is one resource a call touches. Shared claims coexist; an exclusive
// one waits for, and holds off, every other claim on the same key.
type claim struct {
	key       string
	exclusive bool
}
```

key 的形式：

- `canvas`：画布拓扑与结构；
- `node:<id>`：某个节点的内容；
- `node:*`：全部节点的内容，用于读整张画布；
- `library`：素材库；
- `plan`：计划块；
- `search:<规范化搜索词>`、`webpage:<规范化链接>`：外部查询；
- `run`：整个运行。

所有 key 只在本运行内有效。一次运行只绑定一张画布，所以 key 不带画布 ID。

### 2.2 依赖规则

同一批调用按 `tool_call_index` 排序。调用 j 依赖排在它前面的调用 i，条件是两者有一对 key 相交，且其中至少一方是独占。相交的定义：

- 两个 key 相同；或
- 一方是 `node:*`、另一方以 `node:` 开头；或
- 任一方是 `run`。

每个调用只等自己依赖的那些结束，其余立即开始。

- 依赖只指向排在前面的调用，不会成环，不会死锁。
- 互相冲突的两个调用，执行顺序与串行时完全相同。不冲突的调用顺序不影响结果。

**为什么不照抄 buzzy 的泳道。** buzzy 按 key 分泳道。一个调用的 key 同时命中两条已有泳道时，它只并入第一条，可能和第二条泳道里的调用并发写同一资源；它也不区分读写。两两判依赖天然覆盖多 key 的情况，读与读之间也能并行。

### 2.3 保守默认

以下情况一律声明 `run` 独占，与所有调用串行：

- 工具没有声明资源；
- 参数解析失败；
- 资源依赖画布连线，但批次开始时读不到画布。

只有能证明独立的调用才并行，忘了声明的工具只会慢，不会出错。

## 3. 工具声明

| 工具 | 声明 | 说明 |
|---|---|---|
| `read_canvas` | `canvas` 共享 + `node:*` 共享 | 读整张画布：排在同批生成、压缩之后时，等它们结束，与串行一致 |
| `read_asset`、`read_history`、`read_run_result`、`read_skill_resource`、`load_skill` | 无 | 读不可变内容或本运行自己的记录 |
| `search_assets`、`read_library`、`read_library_result` | `library` 共享 | |
| `list_conversation_assets` | `run` 独占 | 它读同批前面调用产生的生成结果与读取记录，而生成之间要并行，无法用更细的资源表达。这个工具用于找回旧内容，频率低，保守串行 |
| `organize_assets`、`manage_library`、`trash_assets`、`restore_assets` | `library` 独占 | |
| `save_to_library` | `library` 独占 + `canvas` 共享 + `node:<node_id>` 共享 | 保存读目标节点当前内容：同批里排在该节点生成之后时，等生成结束 |
| `search_web` | `search:<规范化搜索词>` 独占 | 同一搜索词排队，后一个回放前一个的结果 |
| `read_webpage` | `webpage:<规范化链接>` 独占 | 保住「同一链接只外发一次」 |
| `update_plan` | `plan` 独占 | |
| `edit_canvas`、`set_node_prompt` | `canvas` 独占 | 整张画布判冲突，串行 |
| `compress_image` | `canvas` 共享 + `node:<node_id>` 独占 | 读写目标节点；实现时核对 `creativeimageedit` 只依赖目标节点 |
| `generate_node` | `canvas` 共享 + `node:<node_id>` 独占 + 每个上游节点 `node:<上游 id>` 共享 | 上游节点见 §3.1 |

### 3.1 生成的上游节点

生成在受理时读取上游节点的当前内容（`orderedGenerationRefs`），这些节点不在参数里，所以在构建批次时读一次画布确定：

- 目标节点的入边，其源节点都算上游；
- 目标节点的输入中带 `source_node_id` 的，其源节点也算上游；
- 读取方式与 `orderedGenerationRefs` 用同一份图结构，不另写一套规则。

同批里如果在这个 `generate_node` 之前有 `canvas` 独占的调用（`edit_canvas`、`set_node_prompt`），它们可能改变连线，批次开始时读到的上游就不可靠。这种情况下，该 `generate_node` 改为声明 `run` 独占，整体退回串行。

实际影响很小：`generate_node` 需要 `set_node_prompt` 返回的 `draft_revision`，正常流程里写提示词和生成本来就在前后两次回复中。

由此得到的效果：

- 生成 A、生成 B，且 B 连着 A：B 读 `node:A` 共享，A 写 `node:A` 独占，所以先 A 后 B，B 用到 A 的新结果，与串行一致。
- 生成 A、生成 B 互不相连：两者并行。
- 生成 N、保存 N：先生成后保存，保存的是新内容。

**这份依赖为什么不需要持久化。**

- 批次开始时读一次画布，算出的依赖在本次执行内固定不变。
- 中断恢复后会按当时的画布重算。这正是剩下的调用受理时实际会读到的连线，所以按它排序才是对的。已完成的调用不受重算影响。
- 摄影师在批次执行过程中手动改连线，属于人与助手并发编辑；串行时同样存在，不在本设计范围内。

## 4. 实现

### 4.1 批次调度器

```go
// batch is the dispatch plan of one model answer. It is rebuilt from the
// journal and the canvas as they are when execution (re)starts.
type batch struct {
	deps  [][]int
	done  []chan struct{}
	close []sync.Once // closing is idempotent: replay, finish and build may all try
	slots chan struct{}
	mu    sync.Mutex
	// stopped is the first error that ends the run. Calls that have not
	// started by then never will.
	stopped error
	started []bool
}
```

- **构建。** 由本批第一个进入 `callTool` 的调用负责，其余调用等构建完成，等待期间不占数据库连接。
  - 构建在一个独立的只读快照（`WithReadSnapshot`）里完成：读出本批全部步骤（工具名、参数、状态），以及画布的节点、边和输入，按 §3 算出声明和依赖。
  - 结果存进 `runtimeHandler.batches`，以模型步骤 ID 为 key，持锁访问。
  - 同一批只构建一次。进入新的模型步骤时，丢弃上一批的记录。
- **已完成信号按日志初始化。** 构建时，状态为 `succeeded` / `failed` 的步骤直接关闭它的 `done`。
  - Eino 从中断状态恢复时会跳过已执行的调用，不再进入包装器。所以不能只靠回放路径去关闭 `done`，否则依赖它的调用会一直等到 `ctx` 结束（S1-4）。
  - 所有关闭操作都通过 `sync.Once`，可重复调用。
- **单个调用的执行顺序：**
  1. 取得（或等待）本批的构建结果，按调用 ID 找到自己的序号。构建时已完成的调用跳到第 5 步，不等依赖、不占名额。
  2. 等依赖的 `done` 全部关闭；同时监听 `ctx.Done()`，取消时立即返回 `ctx.Err()`。
  3. 取并发名额，同样监听 `ctx.Done()`。
  4. **拿到名额后**，在 `b.mu` 内检查 `stopped`，并登记 `started[i] = true`。已停止则归还名额、关闭 `done`、返回 `stopped`，不执行。这一步修正了 S1-2：检查停止必须放在取到名额之后。
  5. 走 `callTool` 现有的第一个事务：运行准入（`admitDispatchInTx`），查出本调用的步骤，已完成的回放保存的结果。**准入紧贴执行**，与串行时一样，在执行前一刻检查取消请求、epoch 和租约。
     - 不能复用构建时的检查结果：调用可能已经等了几十秒依赖，而取消后执行者的 `ctx` 最长约 10 秒才会被取消（自审 R1）。
  6. 执行，并走收尾事务。
  7. 归还名额，关闭 `done`。
- **任何退出路径**都用 `defer` 关闭 `done`、归还已取得的名额。
- **并发上限。** `const toolConcurrency = 8`，包内常量，不进 `Limits`，因此不改变检查点摘要。等依赖的调用不占名额。生成在网关还有自己的通道上限。
- **Eino 配置。** `ExecuteSequentially` 改为 `false`。批次最大 64 个调用，goroutine 数量有界。

### 4.2 调用身份随 `ctx` 传递

- 删除 `runtimeHandler.step` 和 `toolStep()`。
- `callTool` 在执行前用 `withToolStep(ctx, stepID)` 把步骤 ID 放进 `ctx`。读取闭包改为 `read func(context.Context) (string, error)`，由 `WrapInvokableToolCall` 传给 `next`。
- `canvasTool.InvokableRun`、`library_journal`、`search_tool`、`webpage_tool` 改用 `toolStepFrom(ctx)`。拿不到时返回 `ErrRunState`，不猜。
- `failures` 计数已经持锁访问。其余字段（`held`、`revisions`、`libraryEnabled`、`current`）在一批调用执行期间只读。

### 4.3 事务与锁

- 写事务使用数据库默认的 READ COMMITTED（§1）。同一运行内多个调用同时收尾时，事务会在运行行、会话行的 `FOR UPDATE` 上排队，并读到最新的行。会话消息序号由会话行锁保证连续，不会报 40001。**不加进程锁。**
- 加锁顺序一致：收尾事务先锁运行行（准入）再锁会话行；工具体内需要准入的事务（`library_journal`）同样先锁运行行。所以不会死锁。
- 测试里用 8 个（并发上限）同时收尾的读调用验证（§6）。
- 如果以后把写事务改成 REPEATABLE READ 或更高，这些事务需要补「回滚后重试」，到时在本设计上追加。

**连接池（自审 R3）。** 连接池用默认上限 `max(4, CPU 数)`。并行后，一个运行最多 8 个调用同时开事务，creative-worker 里还有其他运行、生成派发和网关巡检共用这个池。

- 等待时不占连接：等依赖、等名额、生成轮询的间隔、网页抓取都不持有事务。连接只在毫秒级事务期间占用，排队即可。
- 真正的风险是**持有事务时再向连接池取连接**：连接都被等行锁的事务占满后，持锁者再要第二个连接，就会永久卡住。
  - 实现时逐个检查 `callTool` 收尾事务、`library_journal` 守卫和画布、生成准入路径，确认事务回调里只用 `tx`，没有用 `h.scope` 或 `s.pool`。
  - 再加一个连接池上限为 2 的测试，跑满一批并行调用，必须在超时前完成（§6）。
- 本期不调整连接池配置。如果测试或上线后发现连接排队明显，再在 `DATABASE_URL` 上显式设置 `pool_max_conns`。

### 4.4 停止

两种停止要分开处理（S1-3）。

**① 批次停止派发**：同批某个调用让运行结束，本执行者仍持有运行。

- **触发条件**：`callTool` 返回任何非空错误。工具自身的失败都已转换为结果返回模型，凡是返回错误的，都会让 Eino 结束这一批、进而结束运行。这包括 `stopsRun` 列出的错误，也包括 `errToolFailing`（同一工具 3 次未解释失败）。
- 第一个这样的错误记入 `b.stopped`。
- 还没登记开始的调用，拿到名额后看到 `stopped`，不执行，步骤保持 `prepared`，由运行结束时统一置为 `failed`。
- 已经开始的调用照常执行完。它们的收尾事务仍能通过准入（epoch 没变），正常写步骤日志和工具消息。
- Eino 等全部 goroutine 结束后才返回错误，所以运行结束最多被在途的生成拖后约 150 秒。这与串行时「错误之后还会执行剩余调用」相比只会更短。

**② 取消、撤权、租约失效、运行截止**：执行权已经不在本执行者手上，或运行已被关闭。

- 沿用现有协议：`endControlledRun` 提升 epoch，把 `prepared` 步骤置为 `failed`，为未回答的调用补答。
- 在途工具即使拿到结果，收尾事务也会被准入拒绝，旧执行者不发布任何结果。这与串行时在途那一个调用的处理完全相同，只是现在可能有多个。
- 已经产生的领域事实留在各自的账里，由有权限的路径处理，与本设计无关：
  - 画布命令的操作回执；
  - 生成登记的版本（生成在画布上独立继续，按 ada89a3 的规则采用）；
  - 网关的预留与结算。
- 等依赖、等名额的调用都监听 `ctx`，执行者被取消时立即退出，不会再开始新调用。

### 4.5 中断恢复

- **边界不变：** 只有存在兼容检查点时才续跑。检查点缺失、版本不符或与日志对不上的，仍按现有规则明确停止（`errCheckpointMissing` / `errCheckpointVersion` / `errCheckpointJournal`）。本设计不承诺任意进程崩溃后都能自动续跑。
- **续跑时：**
  - 一批里可能有多个调用停在 `prepared`（串行时最多一个）。
  - 批次按 §4.1 从日志和当时的画布重建；已完成的调用直接视为结束，不依赖 Eino 是否再次调用它。
  - `prepared` 的调用按原步骤 ID 重做，幂等性与现在相同：画布命令查操作回执，生成按 `seed` 去重，素材库写入按步骤日志；网页读取和搜索可能重新外发，与现状相同。
- **旧检查点：** 它们只保存控制流，依赖由日志重算。所以新运行时可以接着跑，不需要升级检查点版本。

### 4.6 停止 Agent 时还没发出的生成

**规则（owner 2026-10-14）**：生成一旦发给供应商就不能取消；还没发出的可以终止。

**现状：**
- 节点生成有自己的执行和网关调用组（组 ID = 执行 ID，`creativegeneration/service.go`）。停止 Agent 不会碰它们，生成会全部跑完。
- 生成因通道并发上限在网关排队时，请求停在 `prepared`（`generation_dispatch.go` 拿不到许可时返回 `ErrRateLimited`，不改变状态）。
- 网关 `CancelInTx` 已经符合这条规则：`prepared` 的请求直接终止并释放预留；已发出或结果未知的，只记录取消意图，费用事实照常。
- 画布层的生成取消 `cancelNodeExecution(..., generation=true)` 不看是否已发出，一律把执行置为 `cancelled / discarded`。前端没有入口，但画布命令 API 能调到；调用后会丢掉已付费的结果，与规则不符。

**改动：**

1. **收紧画布层取消**：生成执行的取消，只在网关请求仍为 `prepared` 时生效。
   - 生效时，在同一事务里依次：锁网关请求 → `CancelInTx` → 锁画布 → 执行置为 `cancelled / discarded`，节点的生成遮罩随之消失。加锁顺序与发布一致：先网关，再画布。
   - 已发出的生成，取消返回业务拒绝「生成已经开始，无法取消，结果会照常出现在画布上」。不改变任何状态。
   - 执行已建、网关请求还没受理的窗口，实现时核对受理流程：如果两者不在同一事务里建立，就把这种情况也视为「未发出」并终止。
2. **摄影师停止 Agent 时，终止本次运行尚未发出的生成。**
   - 只针对摄影师主动停止（取消请求）。运行超时、出错、撤权结束时不处理：这些生成是摄影师要的，照常完成。
   - 找到本次运行所有 `generate_node` 步骤。执行 ID 由步骤 ID 确定性派生（`seed = RunID/stepID` → `agentOperation` → 执行 ID），由 `creativegeneration` 提供派生函数，不在 Agent 包里重写一遍规则。
   - 对每个执行，按上一条的规则逐个取消：没发出的终止；已发出的跳过；查不到执行的（调用还没提交）跳过。
   - 放在停止事务提交之后，每个执行单独一个事务，尽力而为。与派发并发时，谁先锁到请求行谁说了算：派发先拿到就算已发出，照常完成。进程恰好在这之间退出时，未终止的生成会照常发出并计费；这只损失一次「省钱」的机会，不会出错。
3. **并行批次自身**：停止时还没开始的工具调用，本来就不会提交生成（§4.4）。

**摄影师看到的效果**：比如并行 6 张图时停止 Agent，生图通道限 4 路，那么 4 张已发出的照常出图并计费；2 张排队中的被终止，节点回到生成前的样子，预留的额度退回。

### 4.7 不变的部分

- 工具消息按完成先后追加到会话；历史冻结按 `tool_call_id` 配对（`inputs.go`），与顺序无关。当轮交给模型的工具结果由 Eino 按调用顺序组装。
- 同一工具 3 次未解释失败就结束运行，规则不变。

## 5. 模型引导

`instructions/base.md` 增加：

> 互不依赖的调用放在同一次回复里一起发出，会同时执行：例如一次读取多处内容，或对多个节点调用 generate_node。后一步要用到前一步的返回值时（例如新建节点后才能拿到节点 ID），分到下一次回复。

`generate_node` 的说明末尾增加：

> 多个节点要生成时，在同一次回复里一起调用：互不相连的节点会同时生成，连在一起的按先后顺序生成。

原有约束不变，例如「生成消耗额度，只在对方明确要生成时调用」。

## 6. 测试

| 层 | 用例 |
|---|---|
| 依赖计算（表驱动） | 读与读并行；`edit_canvas` 后的 `read_canvas` 串行；`generate_node(N)` 后的 `read_canvas` 串行；两个不相连节点的生成并行；同一节点两次生成串行；B 连着 A 时「生成 A、生成 B」串行，顺序反过来也串行；「生成 N、保存 N」串行；`edit_canvas` 之后的生成退回 `run` 独占；读不到画布时生成为 `run` 独占；同链接读取串行、不同链接并行；素材库写入之间串行；未声明工具、参数非法与所有调用串行；一个调用的多个 key 分别命中前面两个调用时两者都要等 |
| 并行执行（假生成器） | 两个不相连节点的 `generate_node`：假生成器设屏障，两个都进入才放行，证明确实并发；B 连着 A：B 受理时拿到的上游修订是 A 的新版本 |
| 停止派发 | 8 个调用执行中、第 9 个在等名额；其中一个返回 `ErrLimit` 后，第 9 个不执行、步骤为 `prepared`、假工具调用计数不增加；另测 `errToolFailing` 同样触发停止 |
| 取消 | 两个生成在执行中时取消运行：步骤全部为 `failed`，旧执行者没有写入任何工具消息，生成版本仍按 `publish` 规则登记；一个调用在等依赖时写入取消请求（`ctx` 尚未取消），依赖结束后该调用被准入拦下，假工具调用计数不增加 |
| 找回对话内容 | 同批「`generate_node(N)`、`list_conversation_assets`」：列表里有 N 的新结果 |
| 连接池 | 连接池上限设为 2，跑一批 8 个并行读调用加 2 个生成，在超时前全部完成 |
| 停止与未发出的生成 | 6 个生成中 4 个已发出、2 个在网关排队时，摄影师停止 Agent：2 个请求为 `cancelled`、预留释放、执行为 `cancelled / discarded`；4 个照常完成、采用、结算；运行超时结束时 6 个都不受影响 |
| 画布层取消 | 对已发出的生成执行 `cancel_execution`：返回拒绝，执行与网关请求状态都不变；对排队中的生成：终止并释放预留 |
| 恢复 | 检查点中 A 已完成、B 依赖 A 尚未执行：重建批次后 B 不等待，直接执行；两个生成在执行中时中断，续跑后各自只生成一次 |
| 收尾并发 | 8 个读调用同时收尾，无错误，会话消息序号连续（`-count=20`） |
| 竞态 | `go test -race ./internal/creativeagent/...` |
| 门禁 | `make check-go PKG=./internal/creativeagent/...` |

## 7. 风险与边界

- **等依赖的调用也显示「运行中」。** 批次卡片（§8）一并解决。
- **网关排队。** 同批超过 4 个生图时，后面的在网关排队，可能超过等待时间，返回「仍在进行，结果稍后出现在画布上」。这是现有语义。
- **额度准入逐个进行。** 同批生成按调用逐个预留，额度不够时后面的被拒并说明。整批预检等积分账落地（§8）。
- **第三方限流。** 网页读取并发后，Firecrawl 可能返回 429，按现有规则转成拒绝说明。上限 8 是第一道防线。
- **运行结束可能被在途生成拖后。** 批次停止派发时，最多拖后约 150 秒（§4.4①）。
- **部署隔离级别。** 生产库须保持默认 READ COMMITTED。上线前在生产执行一次 `SHOW default_transaction_isolation` 确认。
- **连接池。** 默认上限较小，见 §4.3。上线后观察连接等待时间。
- **等待中的时间预算。** `generate_node` 的等待时长在开始执行时按「距运行截止还剩多少」计算。排队较久的调用剩余时间更少，可能刚提交就返回「仍在进行」。串行时这个问题更严重，所以不是本设计引入的新问题。

## 8. 后续（不在本期）

1. **同批新建自动打组。**
   - `edit_canvas` 开放 `group_nodes`。
   - 一次新建 2 个以上待生成节点时，默认放进同一组；对已有节点重新生成时不打组。
   - 判定写成工具声明的能力，不按工具名硬编码。
2. **前端批次卡片。** 执行前公布并行关系，逐个上报开始和结束，最后汇总成败；顺带区分「等待中」与「执行中」。参照 buzzy `batch_plan_notifier.go`。
3. **按账号的生成并发上限。** 在 PostgreSQL 里实现，与订阅档位挂钩，随积分账一起做。
4. **整批积分预检。**
5. **视频的异步等待。**

## 9. 参考

- buzzy：
  - `buzzy-workflow/internal/agents/toolfx/`（`tool.go`、`dispatch.go`、`runner.go`）
  - `internal/agents/tools/batch_canvas_group.go`、`batch_plan_notifier.go`、`batch_generation.go`
  - `internal/feature/parallelism/`
- 相关设计：`docs/product/creative-canvas-system/eino-adoption.md` 第 63 行、`agent-harness.md` 第 48 行。

## 10. 审计处理记录（S1，2026-10-14）

| 编号 | 结论 | 处理 |
|---|---|---|
| S1-1 资源声明漏掉节点内容的读取依赖（阻塞） | 成立。生成按实时连线读上游内容，保存读目标节点当前内容，参数里都没有 | §3：保存读目标节点；生成写目标节点、读上游节点；上游在批次开始时从画布确定；前面有画布写入时退回串行；§3.1 说明依赖为何按当时的画布重算即可，不需持久化 |
| S1-2 停止检查在取名额之前，会漏放等待中的调用（高） | 成立；另外 `errToolFailing` 不在 `stopsRun` 里 | §4.1：取到名额后在锁内检查停止并登记开始，等待均监听 `ctx`；§4.4：任何返回错误的调用都触发停止 |
| S1-3「已执行的照常完成并写日志」与取消协议冲突（高） | 成立。`endControlledRun` 提升 epoch 并关闭 `prepared` 步骤，在途收尾会被准入拒绝 | §4.4：分为批次停止派发和取消 / 撤权 / 租约失效两种，后者沿用现有协议，旧执行者不发布结果 |
| S1-4 恢复时已完成调用不一定再进入包装器（高） | 成立。Eino 恢复会跳过已执行调用 | §4.1：构建批次时按日志状态初始化已完成信号，关闭幂等；§4.5：保留「没有兼容检查点就明确停止」的边界 |
| S1-5 事务隔离前提有误（建议） | 成立。写事务用数据库默认隔离级别，开发库实测为 READ COMMITTED | §4.3：删去进程锁，改为说明行锁排队与加锁顺序，加并发收尾测试；§7：上线前确认生产隔离级别 |
| 待 owner 拍板：有关联的生成是否保持串行 | 按审计推荐保持串行 | owner 2026-10-14 确认 |

## 11. 自审记录（2026-10-14，第 3 稿）

逐条重走执行时序，并对照代码核实。

| 编号 | 问题 | 依据 | 处理 |
|---|---|---|---|
| R1（高） | 第 2 稿把运行准入放在等依赖之前：调用等了几十秒依赖后直接执行，不再检查取消。取消后执行者的 `ctx` 最长约 10 秒才被取消，这段时间里可能提交新的付费生成。这是相对串行的回退 | `admitRunInTx` 检查 `cancel_requested_at`；`renewLease` 每 10 秒续租，失败才取消 `ctx`；`submitNode` 不检查运行状态 | §4.1：构建改用独立只读快照；准入挪到拿到名额之后、执行之前，沿用 `callTool` 现有的第一个事务；补测试 |
| R2（中） | `list_conversation_assets` 被归为 `library` 共享，但它读的是本对话的消息，包括同批前面调用刚写入的生成结果和读取记录；并行后可能漏掉 | 工具说明「包括助手读过或成功采用的生成结果」 | §3：改为 `run` 独占；补测试 |
| R3（中） | 连接池用默认上限 `max(4, CPU 数)`，并行后同时开事务的数量变多；如果有代码在持有事务时再向池取连接，池会被占满而卡死 | `store.Open` 使用 `pgxpool.New` 默认值 | §4.3：实现时逐个检查事务回调，加连接池上限为 2 的测试；本期不改配置 |
| R4（低） | 第 2 稿的收尾测试写 16 个同时收尾，超过了并发上限 8；`batches` 没写清理 | — | §4.3、§6 改为 8 个；§4.1 写明进入新模型步骤时丢弃上一批 |

核对后确认没有问题的点：

- 不相连节点并行生成不会互相冲突：受理只校验目标节点与上游节点的修订，不校验整张画布。
- `read_history` 只读触发消息之前的历史，与同批调用无关。
- `read_asset`、`read_run_result` 需要的 ID 只能来自之前的返回，同一次回复里拿不到同批调用的结果。
- `set_node_prompt` 能改变生成的上游（输入里的 `source_node_id`），已在 §3.1 的「前面有 `canvas` 独占调用则退回串行」里覆盖。
