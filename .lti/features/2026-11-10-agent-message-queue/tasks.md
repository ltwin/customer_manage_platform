# Tasks

## 1. 穿刺：续跑可行性（U1）

- [x] 1.1 在 `steer_test.go` 用脚本化 stubVendor 写三个穿刺用例：①最终回答后插入一个批次，用 `AfterAgent` 留下的消息 `runner.Run` 续跑，断言第二次模型请求的消息序列（系统指令不重复、`Extra` 保留、插队文字恰好一次）；②续跑中遇到工具等待，从检查点恢复后能继续；③续跑前退出，运行留在可被 sweep 处理的状态。①②可行就继续；不可行就停下，回报 owner 并改用 `WrapModel` 方案。结果：①② 通过（`TestSteerAfterTheFinalAnswerIsAnsweredInTheSameRun`、`TestSteerContinuationResumesFromItsCheckpoint`），整包回归通过；③ 依赖 R7，并入 3.1

## 2. 后端：输入批次与插队

- [x] 2.1 先写测试（红）：补充带片段、老的纯文字补充不变、`text` 与片段同时给出时返回 400；插队的 R1/R2 表驱动（各运行状态、已请求取消、已过期、最新步骤为 prepared / 带工具调用 / 终局 / failed / 还没有步骤）；引用的四种情况（本账号可用但不在清单内则接受，其他账号、授权已撤销、模型不支持图片则拒绝）；幂等（同一个 ID 重放；同一个 ID 跨端点返回 409）；引用行在老查询下按标签读出
- [x] 2.2 `blocks.go` 登记 `input`（`pending` / `delivered`，通用终态 `interrupted`）；`waiting.go` 抽出 `appendInputBatchInTx`（消息、批次、引用行同时写入标签 `body`、`input` 块），`SupplementRun` 接受片段；新增 `steer.go:SteerRun`、`finalModelStepInTx`。2.1 中的写入类用例全部变绿（`TestSupplementCarriesSegments` 验证的是送达，3.2 变绿）。默认测试模型支持图片，所以「图片 + 授权缺预览图类别 → 403」替代了「模型不支持图片」的用例；模型能力检查与 `CreateRun` 写法相同

## 3. 后端：送达、续跑与恢复

- [x] 3.1 先写测试（红）：工具执行期间插队，下一次模型请求只包含这一批次一次，块变为 `delivered`；截止点的两种顺序（入账前插队则续跑一次，入账后插队返回 409），`-count=10`；插队与取消竞争；插队后模型出错，块变为 `interrupted`；续跑前退出后 sweep 把运行记为 `partial` / `creative_steer_undelivered`；「文字、图 A、文字、图 B」两张图都在且顺序与片段一致
- [x] 3.2 `BeforeModelRewriteState` 按批次组装消息（文字一条，每个引用一条），并记下本次带上的批次；`prepareModelStep` 在事务里把这些批次对应的块标为 `delivered`；`runtime.go` 增加 `AfterAgent` 与 `finalMessages`；`execution.go:run` 实现 R4 循环；`control.go:sweepRun` 实现 R7。3.1 变绿，`make check-go PKG=./internal/creativeagent/...` 通过

## 4. 契约与生成物

- [x] 4.1 `api/openapi.yaml` 新增 steers 路径和 `CreativeAgentSupplementSegment`，Supplement payload 增加片段，新增 `input` 块 payload；`httpapi/creative_agent.go` 接线 `SteerCreativeAgentRun`；`make generate` 后提交 `api.gen.go` 与 `schema.d.ts`，`make generate-check` 无漂移；补一个 HTTP 层冒烟测试，确认路由与错误码映射

## 5. 前端：队列

- [x] 5.1 先在 `creative-agent.test.ts` 写测试（红）：`nextQueueAction` 决策表的每一行、重复快照不重发、`sending` 条目在有链和无链两种情况下的恢复、存储不可用时退化为内存
- [x] 5.2 新增 `agentQueue.ts`（状态、`sessionStorage` 持久化、`nextQueueAction`、交接）；`agentState.ts` 把 `steers` 加入可重放路径，新增 `draft.queueItem`。5.1 变绿

## 6. 前端：面板与界面

- [x] 6.1 `AgentPanel.tsx`：从 `sendDraft` 抽出 `sendMessage`；运行中有内容时按钮为「加入排队」，回车排队；接入 `nextQueueAction` 自动发出与「继续发送」；插队与 `waiting_input` 下的补充都走 `perform(single(...))`；队列发出被拒时放回队首、暂停，不回填输入框；补充可以带附件
- [x] 6.2 新增 `AgentQueue.tsx`（列表、插队、删除、继续发送、置灰原因、被拒提示），`agent.css` 加样式；`AgentBlocks.tsx` 渲染 `input` 块（`interrupted` 时附「助手没有读到这条」）和乐观气泡；对话列表跳过已由 `input` 块显示的消息。`make check-frontend` 通过
  - 实施记录（清单外改动）：`agentBlocks.ts` 把 `input` 登记为可渲染的块类型（不登记会降级成「需要更新页面」）；`agentRuns.ts` 的 `TurnContext` 加 `titleOf`，让运行里的 `input` 气泡显示引用标题；`agentOutgoing.ts` 加 `isRunInput`，`refreshAfterSend` 对插队/补充返回 `none`（气泡由 `input` 块画出，不重读对话），连带 `scripts/agent-outgoing.test.ts`。`UserMessage` 从 `AgentPanel.tsx` 移到 `AgentBlocks.tsx` 供两处共用。
  - 实施中补的两处：`steerSegments` 去掉引用之间的空白文字片段（接口不收空白片段）；`recoverQueue` 在条目没发出时清掉 `settledRun`，否则交接中刷新后那条不会再自动发出。队列条目经「重新发送」重放时由面板接手，被拒回到输入框。

## 7. e2e 与回归

- [x] 7.1 新增 `frontend/scripts/agent-queue.e2e.mjs`（`AGENT_QUEUE_E2E_ONLY` 单用例过滤），逐个用例通过：回车排队同帧清空；成功后只发一条；停止或 `partial` 后暂停并能继续发送；插队同帧出现气泡；409 回队首；响应丢失后重放同一请求；`waiting_input` 达到上限时的提示；Skill / 图片置灰；刷新后队列还在；交接中刷新；请求与直接发送一致
- [x] 7.2 回归：`agent-stop.e2e.mjs`、`agent-send.e2e.mjs` 逐个用例通过
  - 实施记录：`agent-stop.e2e.mjs` 的「等补充」用例原来用 `#creative-agent-draft` 文本框，补充改用 `PromptEditor`（可带附件）后改为按「补充内容」输入框操作，断言不变。

## 8. 文档

- [x] 8.1 `docs/product/creative-canvas-system/data-model.md` 补写外发资源边界的新规则；`docs/dev/creative-agent.md` 写插队、补充片段、送达与续跑；`docs/product/creative-canvas-system/modules/agent-api-events.md` 增加 steers 行和 `input` 块
