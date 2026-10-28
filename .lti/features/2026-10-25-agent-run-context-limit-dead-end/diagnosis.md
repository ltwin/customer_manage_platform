# Diagnosis

> 状态：先记录（owner 2026-10-25 报告）。根因来自代码走查，置信度高；还没有写红色测试，也没有查到出事那次运行的数据库记录。这是助手上下文压缩一期设计里明确留到二期的缺口（`.codestable/features/2026-10-14-agent-context-compaction/agent-context-compaction-design.md` §14「运行内中途压缩」）。

## Symptom

1. 在创意空间的助手里提交一个需要大量工具调用的长任务（例如批量读取画布节点、读多张图、反复生成），让单次运行一直调用工具；
2. 运行进行到一半失败，提示「内容超过本次运行的长度上限」（错误码 `creative_context_limit`）；
3. 点「压缩上下文」（即 `/compact`），compact 运行马上结束，提示「暂无可压缩的内容：压缩会保留最近的对话，目前没有更早的内容需要整理。」

期望：长任务在一次运行里不会因为自己累积的工具结果撞上限；即使撞了，创作者也有能用的出路，压缩不会说「没有可压缩的内容」。

## Root Cause

两处叠加，形成死路。

**(1) 运行内裁剪只处理历史，不处理本次运行自己的消息。**

- 每次调用模型前，`BeforeModelRewriteState`（`backend/internal/creativeagent/waiting.go:246`）用 `compactMessages` 做运行内保护（`waiting.go:292`）。
- `compactMessages`（`backend/internal/creativeagent/context.go:664`）只对带 `creative_history_group` 标记的消息去图、截短工具结果、整轮删除（`context.go:683`、`:691`、`:699`）。这个标记只在冻结历史时打上（`inputs.go:760`），以及 Skill 索引（`execution.go:98`）。
- 本次运行里模型的工具调用和工具结果都不带这个标记，永远不会被缩减。工具调用一多，请求一直变大。
- 历史删光之后仍然超出，`prepareModelStep` 按 `EstimateInputTokens > requestByteCeiling` 判定（`execute.go:741`），返回 `ErrLimit`，映射成 `creative_context_limit`（`execute.go:1236`）。

**(2) 手动压缩按「轮」保留最近 2 轮，而一次运行就是一轮。**

- 历史投影按 run 分组成轮（`inputs.go:207`，`item.Group = *runID`），所以这次失败的运行在投影里只是一轮。
- `planCompaction`（`compaction.go:279`）保留最近 `CompactRecentTurns = 2` 轮原文（`service.go:145`），只摘要更早的轮次（`compaction.go:287`）。
- 会话只有 1～2 轮时（长任务通常就是会话的第一、二句话），`CoversThrough` 为 0，`compactInRun` 直接跳过并给出那句提示（`compaction.go:489`）。
- 即使会话更长，压缩也只摘要更早的轮次，撑爆上限的那一轮原样保留，问题没有解决。

这是设计 §8.2 写明的行为（「可压缩的内容不足时……提示暂无可压缩的内容」），只是设计没有考虑「最近一轮本身就超限」的情况。§5 写过「最近 2 轮本身太大时，先截短其中的工具结果；仍超出则只保留最近 1 轮」，在 `planCompaction` 里没有对应实现。

**次要不一致（低置信度，不是主因）**：`compactMessages` 的 `size()` 不计 `ReasoningContent`，而 `EstimateInputTokens` 计入 `m.Reasoning`（`backend/internal/platform/llmgateway/eino_model.go:351`）。会思考的模型可能在保护认为已经够小时，仍被上限判定拒绝。

## Evidence(红)

还没有可重跑的红色复现。代码走查之外，建议在修复前补两个包内单测作为红：

```sh
cd backend && go test ./internal/creativeagent -run 'TestCompactMessagesShedsCurrentRunToolResults|TestPlanCompactionCoversOversizedLatestTurn' -count=1
```

- `compactMessages`：只有一条用户提交 + 若干对不带 `creative_history_group` 的超长 assistant/tool 消息，预算小于总量 → 现在原样返回，仍超预算；
- `planCompaction`：投影只有 1 轮且该轮超出预算 → 现在 `CoversThrough == 0`。

待核实（查库）：出事那次运行的 `creative_agent_runs.error_code`、会话的轮数，以及这次运行的工具消息是否都是 `status='complete'`（不是的话这一轮在投影里被判无效丢掉，轮数更少）。

## Blast Radius

- `compactMessages` 只有 `BeforeModelRewriteState` 一个调用方；外部助手（MCP）不走这条运行，不受影响。
- `planCompaction` 有两个调用方，都在 `planHistoryInTx`（`runs.go:461` 手动、`runs.go:466` 自动）。自动压缩同样不会覆盖超大的最近一轮：失败后创作者直接再发一句话，超大那轮因放不进预算被投影丢弃（`inputs.go` 按预算从新往旧取轮），又因为属于「最近 2 轮」不进摘要——下一轮的助手对刚才的长任务一无所知，只能靠 `read_history` 翻原文。
- 前端：`AgentPanel.tsx:1003` 的「压缩上下文」入口在这个错误下给了一个必然无效的出路。

## Fix Direction

1. **运行内裁剪覆盖本次运行（主修，设计 §14 的二期）**：`compactMessages` 在历史删光后继续处理本次运行——保留系统指令、本次提交和最近 N 步，更早步骤的工具结果按现有规则截短并附「按来源重新读取」的提示、去掉图片，调用/结果成对保留。原始结果已持久化，`read_asset` / `read_history` 能取回。改成真正的运行内摘要（替换 Eino checkpoint 状态）更彻底，代价也大，可以作为后续。
2. **压缩能覆盖超大的最近一轮**：`planCompaction` 落实设计 §5 那句——最近的轮次放不进预算时，减少保留的轮数，必要时把这一轮也纳入摘要（摘要请求本身已按上限从最早端裁剪）。手动和自动压缩一起受益。
3. **出错后的出路**：`creative_context_limit` 的提示说明可以「继续」或压缩后继续；压缩确实无内容时，提示不要暗示问题出在没有更早的对话。
4. 顺手对齐 `size()` 与 `EstimateInputTokens` 的计量口径（计入 reasoning）。
