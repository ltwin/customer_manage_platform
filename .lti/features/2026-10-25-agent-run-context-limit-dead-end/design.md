# Design

## 设计上下文与关键决策

根因见 `diagnosis.md`。压缩一期设计（`.codestable/features/2026-10-14-agent-context-compaction/agent-context-compaction-design.md`）把「运行内中途压缩」放到二期（§14），一期由 `compactMessages` 兜底，但它只处理带 `creative_history_group` 标记的历史；§5 写过「最近 2 轮太大时只保留 1 轮」，`planCompaction` 没有实现。

采用方案：

1. **运行内用有损裁剪，不做运行内摘要。** 沿用 `compactMessages` 现有的做法（截短工具结果、整轮删除），把对象从历史扩展到本次运行较早的步骤。运行内摘要要在 `BeforeModelRewriteState` 里额外调用模型、替换 Eino 状态，并处理计费与失败兜底，是二期的完整方案；本次只解决「长任务必然失败」。
2. **有损必须可恢复。** 被截短或移除的结果，定位符留在上下文里（运行内说明里的步骤索引、截短提示后的定位符），`read_history` 能读到本次运行较早的消息。「按来源重新读取」只有在来源标识还在时才成立（方案评审 P2-1）。
3. **受保护的消息在所有阶段都不动。** 带 `inputKindKey` 的输入（摘要、原话、Skill 索引）不论是否带历史分组，任何阶段都不截短、不删除。这同时修掉 Skill 发现索引被当作最早一轮历史删除的既有问题（方案评审 P2-3）。
4. **裁剪的计量覆盖绑定后才出现的内容。** 工具图片在绑定时变成一条额外的用户消息，它的文字和消息开销按绑定时的同一份标签计入（方案评审 P2-2）。
5. **压缩保留的轮数不超过放得下的轮数**：`keep = min(CompactRecentTurns, projection.Kept)`。放不进预算的轮次本来就不会进入下一轮的输入，让它进摘要严格好于丢弃。这比 §5 的「只保留 1 轮」更进一步：1 轮也放不下时保留 0 轮。
6. **摘要请求先舍弃工具与助手的记录，最后才舍弃创作者的话。** 创作者的原始要求不再依赖从新往旧取、有预算上限的原话区（方案评审 P2-4）。
7. **扫描不到唯一一轮的开头时，覆盖到触发点之前**：投影丢掉了不以创作者消息开头的半截运行，此时 `Turns` 为空、`Unscanned` 为真，压缩范围取 `Before - 1`。

## 领域模型与业务规则

本次只涉及模型输入的组装、`read_history` 的读取范围和摘要请求的裁剪，不新增持久化对象。

- **步骤（step）**：本次运行里一条带工具调用的助手消息，加上回答这些调用的工具消息。它不带 `creative_history_group`，也不带 `inputKindKey`。
- **最近一步**：状态里最后一个步骤。它的结果是模型下一次回答要用的，永远不截短、不移除。
- **受保护的消息**：系统消息、所有用户消息（创作者的提交和运行中的补充）、带 `inputKindKey` 的输入、最近一步、运行内说明。所有阶段都不动它们。
- **定位符**：能把一段被省略的结果取回来的标识。从工具结果的 JSON 里提取：`item_id`（用 `read_run_result` 读）、`result_id`（`read_library_result`）、`content_revision_id` / `content_revision_ids` / `image_revision_id`（`read_asset`）。
- **运行内说明**：一条用户角色的消息，`Extra["creative_run_trim"]` 记录已省略的步数和步骤索引。整个状态里最多一条，位于第一个保留下来的步骤之前。
- **不变量**：每个保留下来的工具调用都有结果，每个工具结果都有对应调用；移除以步骤为单位；被截短或移除的结果，只要原来带定位符，定位符就仍在模型输入里，或者能用 `read_history` 读到。

## 模块职责与协作

| 位置 | 职责 | 本次变化 |
|---|---|---|
| `waiting.go:BeforeModelRewriteState` | 每次调用模型前改写 Eino 状态，计算预算，调用 `compactMessages` | 不变 |
| `context.go:compactMessages` | 把状态裁剪到预算以内 | 统一的保护判定；阶段 4、5；`size()` 计入思考内容和图片绑定开销 |
| `context.go:bindMessages`（图片绑定段） | 把工具图片绑定成额外的用户消息 | 标签文字抽成共用函数，供 `size()` 使用；行为不变 |
| `context.go:readHistory` | 按页读取会话消息 | 读取范围加上本次运行自己较早的消息；工具描述随之更新 |
| `execute.go:prepareModelStep` | 按 `EstimateInputTokens` 判定上限，超出返回 `ErrLimit` | 不变：最近一步本身超限时仍在这里失败 |
| `compaction.go:planCompaction` | 决定摘要覆盖的范围 | 保留轮数受 `Kept` 限制；空投影且未扫描完时覆盖到 `Before - 1` |
| `compaction.go:compactionRequestInTx` | 组装摘要请求的对话记录 | 超出时先舍弃非创作者的行，创作者的行最后舍弃 |
| `runs.go:planHistoryInTx` | 创建运行时决定是否先压缩 | 不变；它的自动条件已包含 `Kept < len(Turns)` 和 `Unscanned`，改了规划后自然覆盖新情况 |
| `agentState.ts:failures` | 失败码到说明文案 | 改 `creative_context_limit` 的文案 |

## 关键机制

### 运行内裁剪的顺序

`compactMessages` 按下面的顺序处理，每一步做完就检查，已在预算内就返回。所有阶段都跳过受保护的消息。

| 阶段 | 对象 | 动作 | 现状 |
|---|---|---|---|
| 1 | 历史消息的图片 | 去图，保留引用 | 已有 |
| 2 | 历史工具结果（> 1 KiB） | 截到 768 字节，附提示 | 已有 |
| 3 | 历史整轮 | 从最早的一轮起删除该轮未受保护的消息；只含受保护消息的分组跳过 | 改：现在会删掉 Skill 发现索引 |
| 4 | 本次运行较早步骤的工具结果（> 1 KiB，最近一步除外） | 从最早的步骤起截到 768 字节，提示后附该结果的定位符 | 新增 |
| 5 | 本次运行较早的步骤（最近一步除外） | 从最早的步骤起整步移除，把它记进运行内说明的步骤索引 | 新增 |

阶段 5 之后仍超出（只剩受保护的消息）就原样返回，由 `prepareModelStep` 以 `ErrLimit` 结束运行，行为与现在相同。

本次运行的图片已经由 `limitHistoryImages` 按 `ImagePreviewCount` 控制数量（工具消息的图片算可选），不再单独处理。

### 步骤的识别

```text
steps = []
for i, m in messages:
    if m 不受保护，role=assistant，且有 ToolCalls:
        step = {assistant: i, results: 紧随其后、ToolCallID 属于这些调用的 tool 消息}
        steps.append(step)
latest = steps 的最后一个；可处理的步骤 = steps[:-1]
```

只处理调用全部有结果的步骤。正在等待工具结果的步骤（中断恢复、等待创作者确认）只可能是最近一步，本来就不处理。工具名由助手消息的 `ToolCalls` 按 `ToolCallID` 对应。

### 定位符与恢复

提取：`resultLocators(toolName, content)` 解析工具结果 JSON 的顶层字段，按上面「定位符」的定义收集，去重，最多 8 个；解析失败返回空。

两条恢复路径：

1. **直接给出**：阶段 4 截短的结果在提示后附「定位符：item_id=…（read_run_result）」等；阶段 5 移除的步骤写进运行内说明的步骤索引，每步一行：工具名 + 定位符。
2. **翻原文**：`read_history` 的范围从「本次提交之前」扩展为「本次提交之前，或属于本次运行」：

   ```sql
   conversation_id=$2 AND status='complete' AND ordinal<$before
     AND (ordinal<$trigger OR run_id=$run)
   ```

   `before_ordinal` 为 0 时不再默认取触发点，而是不设上限。触发点仍然挡住其他窗口在本次运行期间追加的消息（原注释「The trigger fences history」的用意不变）。本次运行的工具消息已经逐条持久化（`persistToolMessage`），截短结果里的 `item_id` 落在前 1 KiB 内（`runtime.go:392` 的结果只有 `item_id`、大小、工具名和 512 字节预览）。

运行内说明的步骤索引有界：最多记最近被移除的 40 步，每步一行不超过 256 字节；更早的只计数（「另有 N 步未列出，用 read_history 查看」）。说明的大小计入 `size()`。

文字（明确标出不是创作者的话）：

> [运行内提示，不是创作者的消息] 为控制长度，本次运行较早的 N 步工具调用及其结果已从上下文中移除，下面是它们的工具和结果定位符。之前得出的结论以你后来的回复为准；需要原始内容时用对应的定位符读取，或用 read_history 查看本次运行较早的消息，不要凭记忆补全。
> - read_canvas：（无定位符）
> - read_asset：content_revision_id=…
> - render_view：item_id=…（read_run_result）

移除 n 步后：状态里已有说明就追加索引、更新步数；没有就在第一个保留下来的步骤之前插入一条。Eino 状态在整个运行里延续，所以说明和已移除的步骤在之后的调用里保持一致，不会反复插入。

用户角色而不是系统角色：网关和各供应商都接受工具结果之后、助手之前出现用户消息，运行中的补充已经这样做（`BeforeModelRewriteState` 把补充追加为用户消息）；系统消息在部分供应商只能出现在开头。

### 计量口径

`size()` 和 `prepareModelStep` 用的 `EstimateInputTokens` 要算同一份请求。现在漏掉的两项：

| 漏项 | 来源 | 本次怎么算 |
|---|---|---|
| 思考内容 | `EstimateInputTokens` 计入 `m.Reasoning`（`eino_model.go:351`） | 加 `len(m.ReasoningContent)` |
| 工具图片的绑定消息 | `bindMessages` 为工具结果里的图片追加一条用户消息：引用标签 + 「（read_asset 读取结果）」或「（render_view 截图）」，另有每条消息 16 字节（`context.go:322`） | 带图片的工具消息再加 `16 + len(toolImageLabel(id, toolName))`；标签抽成 `toolImageLabel`，绑定和计量共用 |

图片因数量或总大小超限被省略时，`omitLimitedImage` 把内容换成一段原因（约 200 字节），少于计量里已算的 4096 字节，不会超出估算。

### 压缩范围

```go
keep := min(max(s.limits.CompactRecentTurns, 0), p.Kept)
switch covered := len(p.Turns) - keep; {
case covered > 0:
    plan.CoversThrough = p.Turns[covered-1].Last
case p.Unscanned && len(p.Turns) > 0:
    plan.CoversThrough = p.Turns[0].Items[0].Ordinal - 1
case p.Unscanned:
    plan.CoversThrough = before - 1 // 唯一的一轮长到扫描不到开头
}
```

`compactInRun` 已有的判断 `CoversThrough <= From` 照常给出「暂无可压缩的内容」。

### 摘要请求保住创作者的话

`compactionRequestInTx` 现在超出时从最早的一行删起。改为两遍：

1. 从最早的一行起删非创作者的行（助手、工具），直到放得下；
2. 仍放不下，再从最早的一行起删创作者的行。

创作者的行原样拼接，不截短（与现在相同）。这样超大一轮最开头的提交和中间的补充优先进入摘要请求；工具结果在记录里本来就只留 1 KiB（`transcriptToolBytes`）。

**剩余的有损边界**：范围内创作者说的话本身就超过摘要请求的余量（上限 256 KiB 减去指令和上一份摘要）时，最早的话仍会被舍弃。这时原文还在会话里，`read_history` 能读到（每条前 1 KiB）。这是明确接受的边界，不再额外设计。

压缩后的原话区（`creatorOriginalsInTx`）仍是第二层保留，不改。

## 接口与数据契约

- 契约状态：不变
- 契约引用与变更：无。OpenAPI、错误码、数据表都不变。`creative_context_limit` 的含义收窄为「最近一步本身放不下」，码和 HTTP 语义不变；前端只改文案。`read_history` 的参数不变，读取范围扩大、描述更新；它是给模型用的工具，不在 OpenAPI 里。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/creativeagent/context.go:compactMessages` | 修改 | 统一保护判定，阶段 4、5，运行内说明与步骤索引，`size()` 两个漏项 |
| `backend/internal/creativeagent/context.go:bindMessages` | 修改 | 图片标签改用 `toolImageLabel` |
| `backend/internal/creativeagent/context.go:readHistory` | 修改 | 读取范围加上本次运行 |
| `backend/internal/creativeagent/context.go:contextToolInfo` | 修改 | `read_history` 描述 |
| `backend/internal/creativeagent/tool_content.go` | 修改 | 新增 `resultLocators` |
| `backend/internal/creativeagent/compaction.go:planCompaction` | 修改 | 保留轮数受 `Kept` 限制；空投影覆盖到 `Before - 1` |
| `backend/internal/creativeagent/compaction.go:compactionRequestInTx` | 修改 | 两遍裁剪，创作者的行最后舍弃 |
| `backend/internal/creativeagent/context_test.go` | 修改 | 运行内裁剪、保护、计量、`read_history` 的测试 |
| `backend/internal/creativeagent/compaction_test.go` | 修改 | 超大一轮的手动与自动压缩、扫描不到开头、摘要请求保住创作者的话 |
| `frontend/src/creative-canvas/editor/agentState.ts` | 修改 | `creative_context_limit` 文案 |

## 验证与交付

先写红，再改代码。

| 验收 | 验证 | 红的依据 |
|---|---|---|
| 运行内裁剪（截短即够、需要移除、只一条说明、最近一步超限原样返回） | `context_test.go` 直接构造状态调用 `compactMessages`，每条检查调用与结果成对 | 现在本次运行的消息原样返回 |
| 受保护的输入 | 同时带 `inputKindKey` 和历史分组的 Skill 发现索引，在历史全部删除后仍在 | 现在第一个被删 |
| 定位符可恢复 | 移除一个结果只有 `item_id` 的步骤 → 运行内说明里有这个 `item_id`，`read_run_result` 用它读到原结果；`read_history` 能读到这条工具消息 | 现在 `item_id` 随步骤消失，`read_history` 读不到本次运行 |
| 计量与最终请求一致 | 夹具里走真实的改写→绑定→`prepareModelStep` 路径：本次运行带 `read_asset` 与 `render_view` 图片、带思考内容，预算设在临界值附近（逐字节扫一段区间），断言最终 `ChatRequest` 的 `EstimateInputTokens` 不超上限、运行不以 `creative_context_limit` 失败 | 现在少算绑定消息和思考内容，临界时被拒 |
| 长任务不再失败（端到端） | 桩模型连续返回工具调用、工具返回大结果，上限调小，运行成功 | 现在以 `creative_context_limit` 失败 |
| 一轮超大时 `/compact` | 一轮大于预算 → `/compact` 生成检查点，覆盖到该轮末尾 | 现在是 skipped |
| 摘要请求保住创作者的话 | 超大一轮：开头的提交带一条约束，中间多次补充，工具结果合计远超余量 → 摘要请求里有这条约束和全部补充 | 现在从最早一端删，开头的提交先被删 |
| 失败后继续 | 直接发下一句 → 先压缩，摘要请求里有该轮内容 | 现在没有压缩，该轮被丢弃 |
| 扫描不到开头 | `HistoryMessages` 调小，唯一一轮超过它 → `/compact` 有检查点 | 现在是 skipped |
| 回归 | `TestTheCompactCommandSkipsAShortConversation`、最近 2 轮原样保留的已有用例、`TestCompactionPreservesCurrentInstructionAndToolPairs`、`read_history` 已有用例（不读其他窗口在触发点之后追加的消息） | — |

门禁：`make check-go PKG=./internal/creativeagent/...`，前端只改字符串，跑 `make check-frontend` 里的类型检查和 lint（按 AGENTS.md 的判定表）。不需要迁移，不需要灰度：裁剪只在超限时走新分支；`read_history` 多读本次运行的消息，不影响其他会话或窗口。
