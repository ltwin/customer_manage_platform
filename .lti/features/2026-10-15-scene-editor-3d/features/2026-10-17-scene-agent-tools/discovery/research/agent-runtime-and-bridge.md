# 调研：助手运行等待、实时通知与场景写入路径（C3 起草前，2026-10-18）

目的：回答 epic 未决 #4（实时通道能否送达客户端任务事件）和 #5（运行等待客户端任务用什么状态），并摸清 C3 要复用的写入、恢复与图片输入路径。只读代码调查，行号以 2026-10-18 的 develop（8f16d17 之后）为准。

## 1. 运行状态机与等待（未决 #5）

- 运行状态（`creativeagent/runs.go:70-80`，CHECK 在 `0046_creative_agent_runs.up.sql:22-24`）：`queued`、`running`、`waiting_input`、`waiting_apply`、`reconciling`、`succeeded`、`partial`、`failed`、`cancelled`。
- **每个账号只有一个写入槽位**：`creative_agent_slots(run_id, claim_token)`，`acquireSlotInTx`（runs.go:690）、`releaseSlotInTx`（runs.go:719）；锁序 slot → gateway group → run（`lockControlledRun`，control.go:37）。
- 现有暂停：`waitForInput`（waiting.go:24）在同一事务里换 `waiting_token`、写 `wait_reason`、转 `waiting_input`、释放槽位；恢复是 `SupplementRun`（waiting.go:41），同事务取槽、清令牌、`enqueueControlledRun`。生产代码里没有工具会触发这条路径（只有 `checkpoint_test.go:47-64` 用 `tool.StatefulInterrupt`）。
- `waiting_apply` 已声明（DB、清扫、前端「等待采纳」），无写入方，语义保留给提案采纳。
- 等待生成（`generate_node`）不挂起：工具内每 2 秒轮询、最长 150 秒，期间占槽位和租约。
- 工具结果在恢复时的回放：每次工具调用是一行 `creative_agent_steps`，`callTool`（runtime.go:242）对已终态步骤直接回放 `output`；`compatibleJournal`（checkpoint.go:139）允许检查点时已准备的步骤在恢复时才成功。
- 清扫（`SweepRuns`，control.go:349）只在 `deadline_at` 到期时结束等待中的运行，从不恢复它；运行期限固定（默认 6 小时，service.go:156），等待不延长。
- 取消（`controlCommand`，control.go:101）可从任何非终态结束运行。

**结论**：复用 `waiting_input` 不行：`SupplementRun` 只查状态和令牌，摄影师发一句话就会在任务未完成时唤醒运行，界面也按该状态显示补充输入框（`AgentPanel.tsx:325`）。`waiting_apply` 语义被占用。「queued + 保留槽位」违背 epic「等待期间释放槽位」。建议新增 `waiting_client`，沿用 interrupt → 检查点 → `waiting_token` → 释放槽位 → 回放 → `enqueueControlledRun` 这套已有部件。需要改：迁移（CHECK 与 0060 的活跃索引）、状态常量、`waitingRunStates`（creativecanvas/projects.go:31）、OpenAPI 枚举、前端状态文案、排空升级文档；完成任务时取槽，取不到则保持等待，由清扫在槽位空出后恢复；取消时在同一事务把任务转 `cancelled`。

## 2. 实时通知（未决 #4）

- 一条共享 LISTEN/NOTIFY 通道 `creative_realtime`（`platform/store/realtime_events.go:16`），载荷只有 `{Kind, Account, ID}`；提交后才送达，跨进程有效。
- `notify_creative_canvas_changed('account')`（0042）已装在 `creative_documents` 等表上，场景保存已经会唤醒同账号的画布流。
- 场景编辑器挂在画布页 `WorkspacePage` 里，共用画布页的一条流；`canvasEvents.ts` 是页内广播，`creative-scene/session.ts:34` 收到后重读场景（500 ms 节流）。
- 流的特性：每次（重）连接先发一次 `invalidate`；15 秒心跳也会发；每个账号每进程最多 5 条流；标签页隐藏时断开。
- 上传状态靠轮询（`media.ts:286-301`），没有 SSE。

**结论**：给 `creative_scene_jobs` 装 `notify_creative_canvas_changed('account')` 触发器即可，不新增事件类型或端点。注意：
- 事件不带文档 ID，编辑器在每次 invalidate（含心跳）时都要认领一次，认领必须便宜、有索引；
- 每次任务行变化会让同账号所有画布页重读，续租这类不改可见状态的更新要在触发器里跳过；
- 唤醒可能丢失，正确性来自「打开、重连、回到前台时认领」，不能依赖收到某一条事件。

## 3. 场景写入、恢复与图片

- 助手写入入口：`creativedocument.CommandAs(ctx, scope, Actor{Kind:"agent", RunID}, c)`（command.go:119），修订行记 `actor_kind`、`run_id`（document.go:278）。它总是自己开事务（executor.go:37），`commandInTx` 未导出：「完成任务 = 写场景 + 任务终态 + 运行继续」同事务需要导出一个事务内入口。
- 先例：`admitRunInTx`（creativeagent/execute.go:643）在工具写入事务里锁运行行、校验令牌和租约；`applyOnce`（canvas_tools.go:240）固定 operation_id 重试。
- 修订列表 `ListRevisions(RunID, SinceRevision)`（history.go:63），`since` 不含本身；整份恢复写 `changed_units=["*"]`。变更知晓 = 取本运行最后写入的修订号，再列其后非本运行的修订。
- `restore_units` 必须单独成批，指定来源修订 k（1 ≤ k < 当前）；场景侧要求重新出现或删除的物件同时列出摆放与内容单元（creativescene/restore.go:72-87）。
- 截图读取：暂存上传经 `creativestaging.Lock` 得到内容修订，`creativemedia.Service.AgentImage`（agent.go:39）可转成模型的图片输入（超大时缩成 JPEG）。现有只有 `read_asset` 会把图片交给模型（context.go:163-206），受模型 `ImageInput` 能力、`image_preview` 授权和数量 / 字节上限约束；`render_view` 要加入同一路径并登记 `toolContentRevisions`（tool_content.go:11）。
- 未建的东西：策划文档的助手工具（`read_plan` / `write_plan` 只在文档里）、本轮改动清单、变更知晓。C3 是第一批文档类助手工具。
- 新工具会改变检查点指纹，按 `docs/dev/agent-skills-tools.md`「排空后升级」，排空清单要加上新状态。
