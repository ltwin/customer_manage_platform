---
risk:
  factors: []
behavior: changed
reads: []
decisionsPending: false
---

## Why

助手执行工具调用很多的长任务时，本次运行自己累积的工具结果从不被裁剪，最终以「内容超过本次运行的长度上限」失败；此时压缩又因为「保留最近 2 轮」而什么都不做，创作者没有出路（诊断见 `diagnosis.md`）。本次补上压缩一期留到二期的运行内裁剪的最小可用版本，并让压缩能覆盖超出预算的最近一轮。

## What Changes

- 运行内裁剪覆盖本次运行：历史删光后仍超出时，先截短本次运行较早步骤的工具结果，再从最早的步骤起整步移除；最近一步、创作者的提交与补充、摘要、原话、Skill 索引在所有阶段都不动（包括修掉 Skill 发现索引被当作历史删除的既有问题）。
- 被截短或移除的结果可以取回：定位符留在截短提示和运行内说明的步骤索引里；`read_history` 能读到本次运行较早的消息。
- 运行内裁剪的计量与最终请求一致：计入思考内容和工具图片绑定时追加的消息。
- 压缩规划保留的最近轮数不超过放得进预算的轮数，唯一一轮长到扫描不到开头时覆盖到触发点之前；手动 `/compact` 和提交时的自动压缩都按此规划。
- 摘要请求超出时先舍弃工具与助手的记录，最后才舍弃创作者的话。
- `creative_context_limit` 的失败提示告诉创作者可以压缩上下文后继续。

## Acceptance Criteria

**运行内裁剪**

- WHEN 一次运行连续调用工具，本次运行的消息累积超过单次请求上限，而最近一步本身放得下 THEN 运行继续调用模型，不以 `creative_context_limit` 失败；发给模型的请求里，创作者的提交和补充原文都在，最近一步的工具调用和结果完整，每个工具调用都有对应结果。
- WHEN 只截短较早步骤的工具结果就足够 THEN 不移除任何步骤；被截短的结果保留开头，提示后附原结果里的定位符。
- WHEN 移除了较早的步骤 THEN 请求里有且只有一条运行内说明，写明省略的步数，并逐步列出工具名和定位符；之后再次移除时更新这条说明，不新增第二条。
- WHEN 最近一步本身就超过单次请求上限 THEN 运行仍以 `creative_context_limit` 失败，最近一步不被截短或移除。
- WHEN 会话的历史全部删除后仍超出，状态里有同时带历史分组的 Skill 发现索引 THEN 索引仍在请求里。

**可恢复**

- WHEN 一个结果只有 `item_id` 的步骤被移除 THEN 运行内说明里有这个 `item_id`，模型用 `read_run_result` 能读到原结果。
- WHEN 模型在运行中调用 `read_history` THEN 能读到本次运行较早的消息；仍读不到其他窗口在本次提交之后追加的消息。

**计量**

- WHEN 本次运行带 `read_asset` / `render_view` 图片和思考内容，裁剪后的大小贴近上限 THEN 最终发出的请求按 `EstimateInputTokens` 不超过上限，运行不以 `creative_context_limit` 失败。

**压缩覆盖超大的最近一轮**

- WHEN 会话只有一轮，这一轮超出单次请求的历史预算，创作者发 `/compact` THEN 生成一个检查点，覆盖到这一轮的最后一条消息，结果不是「暂无可压缩的内容」。
- WHEN 超大一轮的开头提交里有一条约束、中间有多次补充，工具结果合计远超摘要请求的余量 THEN 摘要请求里有这条约束和全部补充。
- WHEN 一次运行以 `creative_context_limit` 失败后，创作者直接发下一句话 THEN 这一轮先自动压缩，摘要请求里包含失败那一轮的对话记录，然后正常回答。
- WHEN 会话里唯一的一轮比历史扫描行数上限还长 THEN `/compact` 仍生成检查点，覆盖到 `/compact` 之前的最后一条消息。
- WHEN 最近 2 轮都放得进预算 THEN 压缩后最近 2 轮仍原样出现在下一轮的模型输入里（回归）。
- WHEN 会话只有一轮且放得进预算，创作者发 `/compact` THEN 仍提示「暂无可压缩的内容」，不调用模型（回归）。

**前端提示**

- WHEN 运行以 `creative_context_limit` 失败 THEN 失败说明里提示可以压缩上下文后继续。

## Impact

- 代码：`backend/internal/creativeagent`（`context.go` 的 `compactMessages`、`bindMessages`、`readHistory`，`tool_content.go`，`compaction.go` 的 `planCompaction`、`compactionRequestInTx`，相关测试）；`frontend/src/creative-canvas/editor/agentState.ts` 的失败文案。
- 契约：无。OpenAPI、错误码、数据表都不变；`creative_context_limit` 仍在最近一步本身超限时返回；`read_history` 是给模型的工具，参数不变、读取范围扩大。
- 外部：无。

## Risk Notes

- 没有列 factor。裁剪只在请求超出上限时走新分支，不超限的运行与现在相同；`compactMessages` 只有 `BeforeModelRewriteState` 一个调用方，`planCompaction` 只在 `planHistoryInTx` 里调用。
- 运行内移除步骤是有损的：模型看不到较早步骤的原始结果，可能重复读取，额外调用计入用量。定位符和 `read_history` 保证能取回，但模型是否去取由它自己判断。
- `read_history` 读取范围扩大到本次运行，触发点仍挡住其他窗口追加的消息，账号与会话范围不变，不是新的信任边界。
- 自动压缩会比现在多触发一种情况（最近一轮放不下预算时），多一次摘要模型调用；以前这种情况下这一轮会被直接丢出上下文，并不进摘要。
- 摘要请求的剩余有损边界：范围内创作者说的话本身超过摘要请求的余量时，最早的话仍被舍弃，原文用 `read_history` 可读（每条前 1 KiB）。
