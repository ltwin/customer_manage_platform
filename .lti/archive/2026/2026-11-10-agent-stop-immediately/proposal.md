---
risk:
  factors: [concurrency]
behavior: changed
reads: []
decisionsPending: false
---

## Why

助手运行中点「停止」后，如果模型请求正在流式输出，运行会停在「核实费用」状态，最长要等 23 分钟；摄影师只能再点一次「结束等待」才能真正停下，两次点击响应都慢，而且停止按钮不在常规 Agent 产品放的位置（发送按钮）。停止应当一次生效，费用核实属于后台记账，不应该让摄影师来等。

## What Changes

- 运行中（排队、执行、等页面执行任务、核实中）时，输入框右下角的发送按钮变成停止按钮；输入框上方状态栏里的「停止」和「结束等待」按钮移除，状态栏只保留进度文字。
- 等待补充（`waiting_input`）时，输入框为空则按钮显示停止，有内容则显示「补充并继续」。
- 点发送的同一帧就进入运行中样式：按钮变停止、状态栏显示「正在处理」、回答位置显示加载轮廓，不等建会话/授权/建运行这几个请求返回（owner 验收反馈 2026-11-10：前端响应优先）。这一段点停止，立即显示已停止，运行一建好就被取消。
- 点停止后，面板立即显示已停止：按钮变回发送，正在输出的块标为「已停止」，不等接口返回；接口失败时恢复原状并提示。
- `POST /agent-runs/{id}/cancel` 一次就把运行结束为终态（`cancelled`，已有交付的回答时为 `partial`），释放会话槽位，不再转入 `reconciling`；未知费用由 Gateway 在后台继续核实，和现在「结束等待」之后的记账方式相同。
- worker 订阅运行的实时通知：取消提交后，立即中断正在进行的模型请求，不再等下一次 10 秒续租；通知不可用时仍由续租兜底。
- 已停止的运行不再显示「已发生的费用仍在核实」，也不再出现「已结束等待，费用仍会继续核实」的文案。
- 不变：已经交给生成服务的生成照样跑完并计费，还没发出的生成被撤回（owner 2026-10-14）；`close-reconciliation` 接口保留给非停止导致的核实中运行，前端不再调用。

## Acceptance Criteria

**停止按钮的位置**

- WHEN 运行处于 `queued` / `running` / `waiting_client` / `reconciling` THEN 输入框右下角只有一个按钮，`aria-label` 为「停止」，点击后发出 cancel；状态栏里没有任何按钮。
- WHEN 没有运行中的任务 THEN 按钮为「发送」，行为与现在一致。
- WHEN 摄影师发出一条新消息，建运行的请求被人为延迟 1.5 秒 THEN 点发送后的下一帧里按钮已是「停止」，状态栏显示「正在处理」，消息下方有加载轮廓。
- WHEN 运行还没建好时点停止 THEN 下一帧里显示「已停止」、加载轮廓消失；运行建好后前端立即对它发 cancel；若建运行失败，不显示错误，草稿放回输入框。
- WHEN 运行处于 `waiting_input` 且输入框为空 THEN 按钮为「停止」；输入任意文字后按钮变为「补充并继续」，回车提交补充。

**停止立即生效**

- WHEN 摄影师点击停止，cancel 请求被人为延迟 3 秒 THEN 点击后的下一帧里按钮已变回「发送」，当前块显示「已停止」，状态栏消失；延迟期间即使继续收到文本事件，块文字也保持点击时的内容；停止完成后显示服务端已保存的内容。
- WHEN cancel 首次响应的 `state` 不是终态（升级前的幂等回执回放）THEN 前端换新 key 再发一次 cancel，拿到终态后才算停完；仍非终态则按失败处理。
- WHEN cancel 请求返回失败 THEN 面板恢复为运行中，并显示错误提示；再次点击可以重试。
- WHEN 模型请求正在流式输出时调用 cancel THEN 响应中的 `state` 为 `cancelled` 或 `partial`，`finished_at` 非空，同一会话可以立刻发出下一条消息；settlement 为 `unknown`，没有被记为零。
- WHEN 同一个运行重复调用 cancel（同一 `Idempotency-Key` 或新 key）THEN 都返回同一个终态，不报错，也不重复撤回生成。
- WHEN 运行被取消后，旧 worker 的模型流又迟到返回了内容 THEN 这些内容不会写进对话，也不会改变运行的终态；用量照常入账。

**worker 及时中断**

- WHEN 模型流进行中，取消事务提交 THEN worker 在 2 秒内取消发给供应商的请求（测试用假供应商记录 ctx 取消时间）。
- WHEN 实时通知连接不可用 THEN worker 仍会在下一次续租（≤ 10 秒）时发现取消并中断。

**后台记账**

- WHEN 被取消运行的模型请求后来核实完成（拿到真实用量，或按上限估算）THEN 运行的 `settlement_state` 更新为 `settled`，运行状态保持终态不变；面板上不出现任何费用核实文案。

## Impact

- 代码：`frontend/src/creative-canvas/editor/`（`AgentPanel.tsx` 发送/停止按钮与 `RunStatus`、`AgentBlocks.tsx` 停止文案、`agentState.ts` 错误文案、运行 store 的乐观更新）；`backend/internal/creativeagent/`（`control.go` 取消路径、`execute.go` worker 取消监听）；`backend/cmd/creative-worker`（如需注入实时订阅）。
- 契约：`cancelCreativeAgentRun` 的语义收窄为「总是返回终态」，状态枚举与字段不变；OpenAPI summary 与说明同步修改，无生成物结构变化。`close-reconciliation` 保留。产品与开发文档同步：`docs/product/creative-canvas-system/modules/harness.md` §5、`agent-api-events.md`、`docs/dev/creative-agent.md` 的取消说明。
- 外部：无。供应商侧调用本来就会被中断，只是中断得更早。

## Risk Notes

- 并发：现在设计在核实期间保留会话槽位，避免旧请求还没定论时同一会话启动新运行。改成立即释放后，新运行可能在旧供应商流被中断前（正常 < 2 秒，兜底 ≤ 10 秒）就开始。旧 worker 的写入已经被取消事务里的 epoch 递增挡住，迟到的结果由 `AbandonCallerGroupResultsInTx` 放弃；旧请求的预算 hold 在核实结束前一直保留，新运行准入时照常扣减。「结束等待」现在走的就是这条路径，所以本次不引入新的终态，只是让它自动发生。验收需要覆盖迟到结果被丢弃、终态不被覆盖这两点。
- 实时通知：LISTEN 连接每个进程一条，worker 进程首次订阅时才会建立；订阅失败不能影响运行本身，只能退回续租兜底。
- 乐观更新：如果 cancel 实际失败，而面板已经显示停止，会造成状态错觉，所以失败时必须回滚并提示；cancel 是幂等的，重试是安全的。
