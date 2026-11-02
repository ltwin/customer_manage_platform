# Change 详细设计

## 继承的设计

- **Epic 批准基线**：`creative-mcp-server` v2。本次引用以下条目：
  - design：「助手主体」「工具集与异步句柄」「生成预检」「画布交接链接」「工具结果的文字」「限流」「错误映射」「账号隔离与安全」「数据与存储设计」；
  - 不变量 X-I1 到 X-I4。
- **交付契约**：`api: MCP 写入与生成工具`。
  - roadmap 原本还让 C2 交付 `data: 通用文档`（MODIFY）。proposal「Risk Notes」建议把它挪到 C3，需要 Epic 修订 v3。本设计按修订后的范围写；owner 不同意时，回到 proposal 列的另一种做法。
- **C1 已交付的基础**：`docs/dev/creative-mcp.md`，以及 C1 design 的「外部连接主体」「读路径的三项职责」「工具结果」。
  - 外部门面在 `backend/internal/creativeagent/external.go`；
  - 读路径的调用方接口是 `read_access.go:readAccess`；
  - MCP 适配在 `backend/internal/creativemcp/handler.go`。
- **原型**：
  - 各工具的摘要以 `docs/prototypes/creative-mcp/shared/results.mjs` 为起点；
  - `instructions` 以 `docs/prototypes/creative-mcp/instructions.md` 为起点。

本次承担的用例：外部助手改画布、写提示词、预检、生成、等待、压缩图片、整理资产库。卡片（C2b）与场景、Skill（C3）不在本次。

## 领域模型与业务规则

### 写路径的调用方

C1 把读路径的三项职责抽成了 `readAccess`。写路径还剩 Epic「助手主体」表里的六项。本次照同样的办法，抽成一个调用方接口 `writeAccess`，运行和外部连接各实现一份。工具实现只依赖这个接口。

| 职责 | 运行（现状，行为不变） | 外部连接 |
|---|---|---|
| 画布 | 由对话查出（`creative_agent_conversations.canvas_id`） | 参数 `canvas_id`，读画布时按账号范围校验，别的账号的画布是 `not_found` |
| 幂等种子 | `run_id + "/" + step` | `"external/" + connection_id + "/" + call_id`；`call_id` 由 `creativemcp` 为每次 `tools/call` 生成（UUID） |
| 新建文档的写入者 | `Actor{agent, run_id}` | C2 不新建文档：`edit_canvas` 遇到 `add_node` + `scene` 时整批拒绝（见「局部设计与偏差」1） |
| 写事务里的发起方复核 | `admitRunInTx`：`agent_start`，加运行的 claim、epoch、lease | `agent_start`，加连接仍有效（由 `platform/oauth` 在同一事务里检查，经 `ExternalConfig` 注入） |
| 生成调用方 | 不变：插入执行时仍写 `photographer`（现状，见「局部设计与偏差」3） | `caller_kind = external_agent`，`caller_external_connection_id = 连接 ID` |
| 等待上限 | 生成 `min(150 s, 运行期限 − 45 s)`；压缩 `min(90 s, 运行期限 − 45 s)` | 生成、压缩都是 40 s |
| 资产库日志 | `run_id` + `conversation_id`，按对话读取 | `external_connection_id`，按连接读取 |
| 资产库日志的收尾 | 运行已结束（`creative_agent_runs.finished_at`）时收尾 | 执行中超过 10 分钟时收尾（见「资产库多阶段写入」） |

### 不变量

- **C2-I1 句柄只属于创建它的连接**：`wait_tasks` 只返回 `caller_external_connection_id` 等于本连接的生成执行。账号相同、连接不同的句柄也是 `not_found`。
- **C2-I2 预检不留痕迹**：`dry_run` 的受理事务最后一定回滚。不写生成执行、网关预留、操作回执、任务队列，也不改画布。
- **C2-I3 运行路径不变**：内置助手的写入者、幂等种子、操作 ID、日志行、等待上限与改动前逐字相同。资产库日志的操作 ID 仍由 `run_id + "/" + step` 派生，已有日志的重放结果不变。
- **C2-I4 断开后不再受理**：连接断开（`revoked_at` 非空）提交之后，任何生成受理事务、资产库阶段事务都在复核时失败；断开前已经提交的照常完成（Epic X-I3）。

### 生成句柄的状态

句柄就是生成执行 ID（`cwexec_…`），不另存句柄表。`wait_tasks` 按执行行映射状态：

| `state` | `apply_state` | `wait_tasks` 的 `status` | `result` / `error` |
|---|---|---|---|
| `queued` | — | `pending` | null |
| `running`、`reconciling` | — | `running` | null |
| `succeeded` | `pending` | `running`（结果正在放到节点上） | null |
| `succeeded` | `applied` / `conflicted` / `discarded` / `expired` | `succeeded` | 与 `generate_node` 同步完成时的结果相同；`apply_state` 照实给出 |
| `failed` | — | `failed` | `error = {code: "generation_failed", message: 执行的 error}` |
| `cancelled` | — | `cancelled` | null |

`expired` 与 `waiting_for_page` 是场景任务的状态，C3 起才会出现。

## 模块职责与协作

```
creativemcp.toolHandler ── call_id = uuid ──► ExternalTools.Call(ctx, principal, call_id, name, args)
  └─ 外部门面（external.go）：查目录 → 能力检查（读 creative_read；写 agent_start）→ 工具
       ├─ 画布与生成（canvas_tools.go 抽出的共用核心，依赖 writeAccess）
       │    ├─ edit_canvas / set_node_prompt → creativecanvas 命令
       │    ├─ generate_node → creativegeneration.GenerateNode（Caller、Wait、Admit）
       │    │                  或 creativegeneration.DryRun（回滚）
       │    └─ compress_image → creativeimageedit.CompressForAgent（Wait = 40 s）
       ├─ 资产库（library_journal.go，依赖 writeAccess 提供的日志归属）
       ├─ wait_tasks（external_tasks.go，新文件）→ creativecanvas 读执行 → 节点版本 → 图片
       └─ 摘要（external_write.go，新文件）→ ExternalResult
```

- **`creativeagent`**：
  - `writeAccess` 和两份实现，与 `readAccess` 放在一起；
  - 写工具的共用核心：从 `*runtimeHandler` 的方法改为 `*Service` 的方法，参数里带 `writeAccess`，`runtimeHandler` 的方法只做转接；
  - 外部目录、`wait_tasks`、摘要。
  - 不知道 MCP 和 OAuth：连接是否有效，由 `ExternalConfig.ConnectionActive` 回答。
- **`creativegeneration`**：
  - `AgentGeneration` 增加 `Caller`。
  - 把 `Product.request` 的受理事务主体抽成 `admitInTx`，真实提交和 `DryRun` 共用。`DryRun` 返回预检结果，然后回滚。
- **`creativecanvas`**：
  - `AdmitGenerationInTx` 增加调用方参数，写入 `caller_kind` 和 `caller_external_connection_id`；
  - 新增按句柄读执行的函数：只按 ID 和连接 ID 查，不需要画布 ID。
  - 摄影师的提交路径（`execution.go` 的插入）不变。
- **`creativeimageedit`**：不改。外部调用传入的 `CallerKind` 仍是 `agent`（见「局部设计与偏差」2）。
- **`creativemcp`**：
  - 生成 `call_id`；
  - 从 `ExternalTool.Destructive` 设置 `destructiveHint`；
  - 正在等待的句柄数按连接计数（上限 8）。
- **`platform/oauth`**：`RequireActiveInTx(ctx, tx, connectionID)`：在调用方的事务里，对连接行加 `FOR SHARE` 并检查 `revoked_at IS NULL`。断开时 `Revoke` 对同一行加 `FOR UPDATE`，所以两者互斥：断开先提交时复核失败；复核先拿到锁时，断开要等这个事务结束。
- **`platform/httpapi/mcp.go`**：把 `oauth.RequireActiveInTx` 接进 `ExternalConfig`。
- **前端 `CanvasView`**：打开画布时读取 `?focus=`，选中这些节点并用现有的 `fitCanvas(0, ids)` 定位。

依赖方向不变：`httpapi → oauth`，`httpapi → creativemcp → creativeagent → creative*`。

## 关键机制

### 一次写工具调用

```
tools/call(name, args)
 → creativemcp：限流（每连接每分钟 120 次）→ call_id
 → ExternalTools.Call
     → requireCreativeRead；写工具另加 RequireCreativeCapability("agent_start")
     → 解析参数（strictArguments；外部专用的 canvas_id 先取出，余下的交给共用核心）
     → 共用核心（writeAccess = externalWriteAccess{principal, call_id, ConnectionActive}）
     → 摘要 + 结构化结果（+ 图片）
 → 领域拒绝 → isError 工具结果（C1 的 externalRefusal，补本次的错误）
```

外部参数在共用 schema 上追加，沿用 C1 的 `extraParams`：

| 工具 | 外部追加 |
|---|---|
| `edit_canvas`、`set_node_prompt`、`generate_node`、`compress_image`、`save_to_library` | 必传 `canvas_id` |
| `generate_node` | 可选 `dry_run` |

资产库其余工具不追加参数。

### generate_node

```
dry_run? ──是──► DryRun（下节）
   │否
GenerateNode(Seed, CanvasID, NodeID, DraftRevision, Wait=40s,
             Caller{external_agent, connection_id},
             Admit = agent_start + ConnectionActive)
   ├─ 40 s 内有结论 → 结果 + 图片
   └─ 还没结论 → {status:"pending", task_id: execution_id, kind:"generation", message, canvas_url, focus}
```

- **图片**：取 `content_revision_ids` 的前 4 个，复用 C1 的 `ExternalTools.image`，长边不超过 1024。图片太大或格式不支持时只省略这一张，摘要里说明。视频、音频不附图片，摘要给画布链接。
- **重复调用**：宿主重试会得到新的 `call_id`，也就是新的执行 ID。
  - 能拦住的：节点上还有进行中的执行时拒绝（「正在生成，完成后再试」）；提示词版本不符时拒绝。
  - 拦不住的：前一次已经完成后，再用同一个 `draft_revision` 调用，会再生成一次、再扣一次费。这和摄影师对同一版提示词再点一次「生成」是同一种操作，平台分不出它是不是重试。
  - Epic 契约里「依靠 `draft_revision` 拦截重复生成」说得过满，实际只覆盖上面两种情况（见「局部设计与偏差」6）。
  - 缓解：
    - 生成 40 秒就返回，远小于已知宿主的超时（Codex 60 秒），宿主因超时重试的机会很小；
    - 工具说明和 `instructions` 写明「调用失败或没有收到结果时，先 `read_canvas` 看节点是否已有新版本，不要直接重发」。

### dry_run：回滚式预检

真实提交在一个受理事务里依次做：锁账号 → 准备网关的生成准入（含额度）→ 编译提示词 → 网关 `PrepareGenerationInTx` → `Admit` → `AdmitGenerationInTx`（画布与节点校验）→ 绑定 → 入队。

`DryRun` 在一个普通事务里，按同样的顺序跑同一个 `admitInTx`，记下这些内容后返回哨兵错误，让事务回滚：

- `admission.Prepared.Quote()`；
- 规范化后的参数；
- 参考列表（内容修订 ID 和来源节点）。

```go
// creativegeneration（示意）
func (p *Product) DryRun(ctx, scope, in AgentGeneration) (DryRunResult, error) {
    err := scope.WithTxScope(ctx, func(tx) error {
        tx.RequireCreativeCapability(ctx, "node_generate")      // Executor 平时替提交做的检查
        lock account（与 creativeops.Executor 相同的顺序）
        r, err := p.admitInTx(ctx, tx, request, operation, in.Admit, &capture)
        if err != nil { return err }
        return errDryRun                                         // 一定回滚
    })
    if errors.Is(err, errDryRun) { return capture.result(), nil }
    return DryRunResult{}, AgentRefusal(err)                     // 与真实生成相同的拒绝
}
```

- 预检不经过 `creativeops.Executor`，所以不写操作回执；入队用的 `jobs.EnqueueInTx` 在事务内，随回滚撤销。
- `Ensure`（为账号登记部署，事务外、幂等）照常执行，和真实提交一致。
- **结果**：契约的 `{dry_run: true, model, node_type, parameters, references, estimated_cost, budget}`。
  - `estimated_cost.amount` 是 `EstimateMicros / 1e6`，保留 2 位小数；`unit` 是币种；`is_estimate` 固定为 `true`。
  - `budget` 与 `get_capabilities` 一样，取 `gateway.MonthBudget`。
  - 积分落地后，这两项随网关换成积分，这里不另做。
- **锁**：预检和真实提交拿同样的锁，持有时间等于一次受理事务。它和摄影师自己的提交互斥，不会读到一半的状态。

### wait_tasks

```
参数校验：task_ids 1–8 个、去重；timeout_s 1–45，缺省 40
连接的等待计数 + len(task_ids) > 8 → rate_limited（retry_after_s = 5）
每个句柄：creativecanvas.ExternalExecution(scope, connection_id, task_id)
   └─ 不存在、不属于本连接 → 整次调用 not_found（契约）
循环：每 2 秒读一次状态；任一句柄到达终态（succeeded / failed / cancelled）或超时 → 返回全部
成功的句柄：节点版本 → content_revision_ids → 图片（整次结果合计最多 4 张，按 task_ids 顺序分配）
```

- **只读**：工具标 `readOnlyHint`。等待本身不改任何东西，和原型一致。
- **等待计数**：在进程内存里，按连接计数，调用结束就减掉。单进程部署下够用（Epic「部署形态」）。

### 资产库多阶段写入

日志表 `creative_agent_library_calls` 改成两种归属，二选一。日志的读写都经 `writeAccess`：

| 操作 | 运行 | 外部连接 |
|---|---|---|
| 日志 ID | `toolOperation(run_id + "/" + step + "/library-result")` | `toolOperation(seed + "/library-result")` |
| `tool_step_id` | step | `"external:" + call_id` |
| 读取条件 | `conversation_id = 对话` | `external_connection_id = 连接` |
| 阶段操作 ID | `toolOperation(run_id + "/" + step + "/i/phase/attempt")` | `toolOperation("external/" + connection_id + "/" + step + "/i/phase/attempt")` |
| 阶段提交前复核（`libraryGuard`） | `admitRunInTx` | `agent_start` + `ConnectionActive` |
| 执行中的日志何时收尾 | 运行已结束 | `created_at` 早于 10 分钟前 |
| `save_to_library` 的画布 | 对话的画布 | 参数 `canvas_id` |

**为什么用 10 分钟收尾**：外部调用就在 MCP 请求里同步执行，进程重启后没有谁继续执行它。

- 收尾先把日志状态改为 `settling`。之后仍在执行的阶段会在 `libraryGuard` 里因状态不是 `executing` 被拦住（`errLibraryFenced`）。所以即使收尾得早，也只会让后面的阶段记为「未执行」，不会重复执行，也不会丢失已提交的结果。
- 一次整理最多涉及的阶段数受计划预算限制（8 MiB），正常执行远少于 10 分钟。
- 收尾只发生在 `read_library_result` 读到这条日志的时候，和运行路径的时机相同。

### 断开时的在途写入

```
Revoke:    BEGIN … SELECT … FOR UPDATE OF c … UPDATE revoked_at … COMMIT
受理/阶段:  BEGIN … 网关锁 … SELECT revoked_at FROM external_agent_connections WHERE id=$ FOR SHARE … 画布/资产锁 … COMMIT
```

- 复核放在 `Admit` 和 `libraryGuard` 原有的位置，锁的顺序与运行停止时相同：先额度、再发起方、后画布和资产。
- 画布命令（`edit_canvas`、`set_node_prompt`）和内置助手一样，不在事务里复核。断开后的下一次请求在 `/mcp` 就是 401，已在执行的这一次命令照常完成。

### 画布交接链接

- `canvas_url = <PUBLIC_BASE_URL>/creative/canvases/<canvas_id>?focus=<id1>,<id2>`。最多带 20 个节点 ID；`focus` 数组本身照实给全。
- 各工具的 `focus`：

| 工具 | `focus` |
|---|---|
| `edit_canvas` | 新建、改动、连接涉及的节点（`remove_nodes` 删掉的不算） |
| `set_node_prompt`、`generate_node`、`compress_image` | 目标节点 |
| `save_to_library` | 来源节点 |

- **前端**：`CanvasView` 首次拿到节点后，如果 URL 带 `focus`，就选中仍存在的节点并 `fitCanvas(0, ids)`，然后用 `replace` 去掉这个参数，刷新页面时不会再跳一次。所有节点都已不在时什么也不做。

### 摘要

沿用 C1 的格式：第一行是结论，然后是关键字段、Markdown 画布链接和「下一步」，不超过 8 行，再附结构化结果。各工具的第一行：

| 工具 | 第一行 |
|---|---|
| `edit_canvas` | 「已修改画布《标题》：新建 N 个节点、连接 M 条……」；整批拒绝时是工具错误，不走摘要 |
| `set_node_prompt` | 「已写入「节点标题」的提示词（版本 R），还没有生成。」 |
| `generate_node`（完成） | 「生成完成：「节点标题」新增 N 张，已放到节点上。」；`apply_state` 不是 `applied` 时写明没有采用及原因 |
| `generate_node`（进行中） | 「已提交生成：`cwexec_…`，还在进行。」，下一步写 `wait_tasks` |
| `generate_node`（预检） | 「预检完成：没有生成，没有扣费。」，然后是模型、参考、预计扣费（估算）和剩余额度 |
| `compress_image` | 「已压缩「节点标题」：X → Y（减少 Z%）。」；`not_reduced` 等状态写明没有改动 |
| `wait_tasks` | 「N 个任务里 K 个已完成。」，然后逐个列出；没有全部完成时，下一步写「再调用一次 `wait_tasks`」 |
| 资产库写工具 | 取日志汇总：「已整理 N 项，M 项未执行。」；有待核对时写「用 `read_library_result` 核对，不要重发」 |
| `read_library_result` | 「整理结果：完成 / 执行中」，有下一页时写明 |

### instructions（外部版本）

在 C1 版本上补充以下内容，篇幅控制在 C1 版本的两倍以内：

- 写入前先读；
- 写入失败先读再重试，不要原样重发；
- 生成要花额度，`dry_run` 是可选的预检，创作者要求全自动时可以不做；
- 长任务用 `wait_tasks` 跟进，不要重复提交；
- 汇报时给 `canvas_url`；
- 场景节点与场景工具稍后开放。

卡片相关的段落留给 C2b。

## 局部设计与偏差

1. **外部新建场景节点在 C2 被拒绝**（依赖 Epic 修订 v3）：`edit_canvas` 的动作里有 `add_node` + `node_type: scene` 时，整批拒绝，说明「场景节点和场景工具稍后开放，现在可以请创作者在创意空间里新建」。
   - 外部写入者在 `creativedocument` 里仍不被接受：即使漏过这道检查，新建文档也会失败，整个事务回滚。
   - C3 交付文档写入者时解除这个限制。
2. **压缩的调用方沿用 `agent`**：`creative_image_edit_renders.caller_kind` 只区分「摄影师手动」和「助手」，不记身份，也没有读取方。外部压缩记 `agent`，不为它加迁移。压缩的结果是节点的新版本，署名随画布署名一起交给 `canvas-undo-own-changes`。
3. **内置助手的生成仍记 `photographer`**：现状是两处插入都写死 `photographer`，`agent` 取值从未使用，这一列也没有读取方。C2 只给外部路径传调用方，不顺手改运行路径（C2-I3）。要改的话应单独开一个小修复。
4. **画布写入不署名**：按 roadmap「与其他工作的衔接」，C2 先于 `canvas-undo-own-changes` 落地，画布命令照内置助手现在的方式写入。
5. **等待计数放在进程内存**：与 C1 的限流相同，单进程部署够用。多进程时要改成数据库计数，届时再做。
6. **重复生成只拦住进行中和过时的提示词**：见「generate_node」的「重复调用」。不为外部路径加去重：MCP 没有幂等键，按参数去重又会挡住正常的「再来一张」。Epic 契约「交互规则」这一句在归档时按实际行为改写（随 Epic 修订 v3 一起提给 owner）。

## 接口与数据契约

- **`api: MCP 写入与生成工具`**：完整交付，正文在 Epic `contracts.md`，本次不改。补充：
  - `generate_node` 进行中的结果在契约字段之外另带 `canvas_url` 和 `focus`。契约规定写工具的成功结果都带这两项，进行中也算成功结果。
  - `edit_canvas` 新建场景节点的限制是临时的，写进本子项的 `specs.md`，不写进契约。
- **数据**：Epic「数据与存储设计」已定的两项扩展，表不在项目契约中，迁移在这里写全：

```sql
-- 0081_external_agent_writes.up.sql
ALTER TABLE creative_node_executions DROP CONSTRAINT creative_node_executions_caller_kind_check;
ALTER TABLE creative_node_executions ADD CONSTRAINT creative_node_executions_caller_kind_check
  CHECK (caller_kind IN ('photographer','system','agent','external_agent'));
ALTER TABLE creative_node_executions ADD COLUMN caller_external_connection_id TEXT;
ALTER TABLE creative_node_executions ADD CONSTRAINT creative_node_executions_external_caller
  CHECK ((caller_kind = 'external_agent') = (caller_external_connection_id IS NOT NULL));

ALTER TABLE creative_agent_library_calls ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE creative_agent_library_calls ALTER COLUMN conversation_id DROP NOT NULL;
ALTER TABLE creative_agent_library_calls ADD COLUMN external_connection_id TEXT;
ALTER TABLE creative_agent_library_calls ADD CONSTRAINT creative_agent_library_calls_owner CHECK (
  (run_id IS NOT NULL AND conversation_id IS NOT NULL AND external_connection_id IS NULL) OR
  (run_id IS NULL AND conversation_id IS NULL AND external_connection_id IS NOT NULL));
CREATE INDEX creative_agent_library_calls_connection
  ON creative_agent_library_calls(account_id, external_connection_id, id) WHERE external_connection_id IS NOT NULL;
```

- 约束名在实施时按库里的实际名字确认（`0040` 用的是列级 CHECK，名字由 PostgreSQL 生成）。
- 存量行不变：执行都是 `photographer`、连接 ID 为 null；日志都带运行和对话。
- `down` 先检查有没有 `external_agent` 执行行或外部日志行，有就拒绝回滚（`RAISE EXCEPTION`），前向修复（与 Epic「迁移只加列」和 C1 `0080` 的做法一致）。
- 连接 ID 不建外键（Epic：不同模块的聚合，连接永不物理删除）。
- 迁移号：develop 当前到 `0080`。合并时如果和别的分支撞号，按惯例改号。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativeagent/read_access.go` | 修改 | 新增 `writeAccess` 与 `runWriteAccess`、`externalWriteAccess` |
| `backend/internal/creativeagent/canvas_tools.go:runtimeHandler.editCanvas` | 修改 | 抽成 `Service` 上依赖 `writeAccess` 的共用核心；外部不允许新建 `scene` |
| `backend/internal/creativeagent/canvas_tools.go:runtimeHandler.setNodePrompt` | 修改 | 同上 |
| `backend/internal/creativeagent/canvas_tools.go:runtimeHandler.generateNode` | 修改 | 同上；`Caller`、`Wait`、`Admit` 由 `writeAccess` 给出 |
| `backend/internal/creativeagent/canvas_tools.go:runtimeHandler.applyOnce` | 修改 | 改为接收 `store.AccountScope` |
| `backend/internal/creativeagent/canvas_tools.go:canvasTool.Info` | 修改 | `set_node_prompt` 的模型目录改由 `Service` 生成，外部目录共用 |
| `backend/internal/creativeagent/library_tools.go:runtimeHandler.resolveCanvasAssets` | 修改 | 可读判定改走 `readAccess` |
| `backend/internal/creativeagent/image_tools.go:runtimeHandler.compressImage` | 修改 | 抽成共用核心，`Wait` 由 `writeAccess` 给出 |
| `backend/internal/creativeagent/library_journal.go` | 修改 | 日志的 ID、读取条件、操作 ID、复核与收尾都经 `writeAccess`；`scanLibraryCall` 读可空列 |
| `backend/internal/creativeagent/library_plan.go:runtimeHandler.prepareLibrarySave` | 修改 | 画布由 `writeAccess` 给出 |
| `backend/internal/creativeagent/library_retry.go` | 修改 | 改走 `writeAccess` 的范围 |
| `backend/internal/creativeagent/external.go:externalCatalog` | 修改 | 增加 11 个工具；`Destructive` 标注；`Call` 增加 `callID` |
| `backend/internal/creativeagent/external.go:ExternalConfig` | 修改 | 增加 `ConnectionActive` |
| `backend/internal/creativeagent/external.go:externalRefusal` | 修改 | 补本次的错误（生成拒绝、资产库拒绝） |
| `backend/internal/creativeagent/external_write.go` | 新增 | 写工具的外部入口与摘要 |
| `backend/internal/creativeagent/external_tasks.go` | 新增 | `wait_tasks` |
| `backend/internal/creativeagent/instructions/external.md` | 修改 | 写入、生成、预检、长任务 |
| `backend/internal/creativegeneration/agent.go:AgentGeneration` | 修改 | 增加 `Caller` |
| `backend/internal/creativegeneration/agent.go:Product.DryRun` | 新增 | 回滚式预检 |
| `backend/internal/creativegeneration/product.go:Product.request` | 修改 | 受理事务主体抽成 `admitInTx`，并传调用方 |
| `backend/internal/creativegeneration/service.go:Service.requestInTx` | 修改 | 传调用方；预检时把报价和参考交给调用者 |
| `backend/internal/creativecanvas/generation.go:AdmitGenerationInTx` | 修改 | 增加调用方参数并写入 |
| `backend/internal/creativecanvas/execution.go` | 修改 | 新增 `ExternalExecution`：按执行 ID 和连接 ID 读执行 |
| `backend/internal/creativemcp/handler.go:toolHandler` | 修改 | 生成 `call_id`，传给 `Call` |
| `backend/internal/creativemcp/handler.go:sdkTool` | 修改 | `destructiveHint` 取自 `Destructive` |
| `backend/internal/platform/oauth/connections.go` | 修改 | 新增 `RequireActiveInTx` |
| `backend/internal/platform/store/oauth.go` | 修改 | 连接行的 `FOR SHARE` 查询 |
| `backend/internal/platform/httpapi/mcp.go` | 修改 | `ExternalConfig.ConnectionActive` 接线 |
| `backend/internal/platform/store/migrations/0081_external_agent_writes.up.sql` / `.down.sql` | 新增 | 见上 |
| `frontend/src/creative-canvas/editor/CanvasView.tsx` | 修改 | `?focus=` 定位后去掉参数 |
| `docs/dev/creative-mcp.md` | 修改 | 工具清单、句柄、预检、排错 |

## 验证与交付

| 验收 | 怎么验证 |
|---|---|
| AC-C2-1 | 外部与内置对照测试（同一组动作，画布结果相同）；`focus` 的前端单测 |
| AC-C2-2、AC-C2-4 | `creativeagent` 用假生成器：40 秒内完成、超时返回句柄、`wait_tasks` 拿到同样的结果；别的连接、别的账号的句柄是 `not_found`；超过 8 个句柄被拒绝 |
| AC-C2-3 | `creativegeneration` 集成测试：预检前后执行行、网关预留、操作回执、任务队列的行数不变；额度不足、提示词版本不符时，错误与真实提交相同 |
| AC-C2-5 | 资产库对照测试；外部日志只能由本连接读到；手工把日志的 `created_at` 改到 10 分钟以前，`read_library_result` 能收尾 |
| AC-C2-6 | 假压缩器：40 秒上限生效、超时不改图 |
| AC-C2-7 | 两账号越权测试覆盖 11 个工具；在受理事务的复核前断开连接，生成不受理；资产库阶段之间断开，之后的阶段记为未执行 |
| AC-C2-8 | `edit_canvas` 外部新建 `scene` 被整批拒绝，画布不变 |
| AC-C2-9 | `creativeagent` 现有测试全部通过；资产库日志的操作 ID 派生有固定值测试，保证运行路径逐字不变 |
| AC-C2-10 | owner 在 Claude Code、Codex CLI 实测，结果记入 `discovery/research/host-runs.md` |

- **迁移**：`store` 的迁移测试覆盖新约束、存量行不受影响、`down` 遇到外部行时拒绝。
- **MCP 端到端**：`httpapi/mcp_test.go` 用 Go SDK 客户端：`tools/list` 有 17 个工具；`generate_node` 进行中返回句柄，`wait_tasks` 拿到结果。
- **定向门禁**：`check-go PKG=` 覆盖 `creativeagent`、`creativegeneration`、`creativecanvas`、`creativemcp`、`platform/oauth`、`platform/httpapi`、`platform/store`，加上 `check-frontend`；合回 develop 时跑 `check-merge`。
- **观测**：沿用 C1 的 `external agent tool call` 日志，另记：
  - 生成句柄：`execution_id`；
  - 预检：`dry_run=true`；
  - 资产库收尾：`library call settled`，带 `result_id`、`reason=external_window`。
- **灰度与回滚**：仍由 `MCP_ENABLED` 控制。迁移只加列、放宽约束；回滚代码不需要回滚数据。

## 未决事项

| # | 问题 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|---|
| U4 | 画布写入署名 | 本次画布命令 | 已按 roadmap 定为 C2 不署名（「局部设计与偏差」4） | 已解决 |
| C2-U1 | ChatGPT、claude.ai 连接器的工具超时：40 秒加句柄是否够用 | AC-5 在网页宿主上的表现 | 有公网域名的环境实测；不够时只调小等待时间，不改句柄机制 | Epic 整体验收前 |
| C2-U2 | Epic 修订 v3：`data: 通用文档` 挪到 C3 | 本次范围、AC-C2-8 | owner 与本设计一起审阅 | 本设计批准时 |
