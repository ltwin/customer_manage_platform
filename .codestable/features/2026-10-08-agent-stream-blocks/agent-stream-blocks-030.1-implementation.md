---
doc_type: feature-implementation-note
feature: 2026-10-08-agent-stream-blocks
requirement: V6-030.1
status: committed
date: 2026-10-09
---

# V6-030.1 后端推送底座：实现记录

对应 design §6 的 030.1 子项，已提交（`bdd50bc`）。030.1 不单独勾选 AC：AC-01–04 的证据见[验证记录](agent-stream-blocks-verification.md)。

## 落地内容

| design 章节 | 实现位置 |
|---|---|
| §3 迁移 0056、回滚拒绝与回滚链 | `store/migrations/0056_*`、`creative_agent_blocks_migration_test.go`，各回滚链测试补上 `creative-agent-blocks` |
| §2.3 Notifier | `store/realtime_events.go`：每进程一条 LISTEN `creative_realtime`，topic 为 `{Kind,Account,ID}`；重连后唤醒全部订阅者 |
| §2.3 Source / Serve | `platform/realtime`：`Server.Open` 在写响应头前完成连接上限（每账号 5 条）、鉴权、游标、订阅；`Stream.Serve` 负责批量（100）、写超时（10 秒）、心跳（15 秒，同时重新鉴权并兜底读一次）、寿命（5 分钟） |
| 画布流迁移 | `creativecanvas.EventSource` + `httpapi.serveRealtime`；画布触发器改发 `creative_realtime` |
| §1 块模型、注册表、`writeBlockInTx` | `creativeagent/blocks.go`：text / thinking / tool / plan / notice 五种块；块写入与事件同事务、run 行锁下分配 ordinal 和 seq；已终态的块不再改写 |
| §1.4 step → 工具块 | `writeToolBlockInTx`：每次从 step 记录重建 payload；同一模型回答的工具块排在该回答正文之后 |
| `update_plan` | `creativeagent/plan_tool.go`：调用块 `hidden`，效果是 run 唯一的 `plan` 块；run 成功时 `active` 的计划收为 `complete` |
| §2.2 gateway Sink | `llmgateway/sink.go`：`Delta(kind, text)` / `Reset()`；OpenAI 兼容与 Anthropic 两个 provider 发 `DeltaText`；重试前 `Reset` |
| §2.2 Harness 节流 | `creativeagent/stream_sink.go`：100ms / 512 字节；步骤首次写入或 Reset 之后用 `set`，其余用 `append`；只有当前持有者能写；最终以校验后的 `Result.Text` 覆盖 |
| 终态收尾 | `settleBlocksInTx` 在 `closeRunInTx` / `endControlledRun` 里与 `run.finished` 同事务 |
| §2.3 run 事件流 | `creativeagent.RunEventSource`、`GET /creative/agent-runs/{id}/events`；`after_seq` 优先于 `Last-Event-ID` |
| 快照 | `GET /creative/agent-runs/{id}` 在 REPEATABLE READ 中返回 `blocks` 与 `snapshot_seq` |
| 工具详情 | `GET /creative/agent-runs/{id}/blocks/{block_id}/detail`，返回参数和模型实际看到的有界结果 |
| 前端 `watchStream` | `frontend/src/api/watchStream.ts`；`canvasEvents.ts` 改为在其上的薄封装，对外函数不变 |

`step.state_changed` 和 `message.completed` 已不再写入。

## 验证

- Go DB 测试：`creativeagent/blocks_test.go`
  - 快照与从 seq 1 回放事件得到的列表逐块相等，seq 连续，revision 逐一递增；流式 append 确实进入日志；正文块与 assistant 消息一致。
  - 网关重试：被替换的 attempt 的预览先被 `set` 成空，最终块等于第二次的结果。
  - 崩溃重做：新 sink 对已有块用 `set` 覆盖，不产生第二个块；过期 epoch 的写入被忽略。
  - 停止：所有未结束的块被标记为 `interrupted`，已完成的块不变，之后的写入不会重新打开它们。
  - 注册表拒绝未知类型、未知状态、未知字段和缺少 payload 的写入。
  - 游标越界返回 `ErrSnapshotRequired`，格式错误返回 400，游标已被裁剪时要求重读快照，其他账号读取得到 unavailable。
- SSE 测试：`httpapi/creative_agent_events_test.go`：401 / 404 / 409 / 400；快照之后、订阅之前提交的事件不丢失；外部连接写入后被唤醒；run 结束且事件发完后服务端关闭连接。
- `realtime` 单测（`-race`）：唤醒重复或合并都无害；唤醒丢失由心跳兜底；超出连接上限被拒、`Close` 幂等；慢消费者被断开；凭证过期后在下一次心跳时断开；`done`、`snapshot_required`、`unavailable` 都会结束流。
- store：`TestRealtimeCanvasCommitIsolationAndReconnect` 覆盖提交后才唤醒、账号隔离、LISTEN 被终止后重连并补发唤醒、run 事件只唤醒自己的 topic。
- 画布 SSE 现有测试（`creative_canvas_events_test.go`）和前端 `creative-canvas-events.test.ts` 都没改断言，直接通过。
- 门禁：`golangci-lint` 对改动包 0 问题；`creativeagent` / `llmgateway` / `realtime` / `creativecanvas` / `httpapi`（Creative 相关）/ `store` 测试通过；`make check-frontend` 通过（414 项）。

**未运行**：画布同步 e2e（`creative-canvas-commands` 等），需要真实后端和账号。

## 与 design 的出入（实现时的取舍）

- `Notifier` 只有 `Subscribe`。发布全部由表触发器在业务事务内完成（design 已允许这种方式），本期没有代码调用 `Publish`，所以没有加这个方法。
- 块注册表的 `respond` 钩子没有预先加。031 实现第一个需要回应的块时再加，避免留下没有调用方的字段。
- 同一个回答里的多个工具调用会一起显示为 `running`，但实际是按顺序执行的。如果走查时觉得误导，可以改成只在真正执行时才置为 `running`。
- 计划块在 `update_plan` 执行时才创建，所以会排在同一回答的工具行之后（块只追加）。
- 画布流对 Bearer 凭证的重新校验，从「每次失效通知前」改为「每次心跳」。对象级权限仍然在每次唤醒时校验，未授权的变化不会被泄露。
- 7 天事件 GC 不在本子项范围内，没有实现。

## Code review 修复（2026-10-09，第 1 轮）

两个 P2 都先用测试复现，再修复。

- **SSE 补读积压时跳过重鉴权和连接寿命**：`drain` 原来要把日志读完才回到外层循环。复现：寿命 60ms 的连接回放 2000 条事件用了 2.28 秒，全部发完，期间没有一次重鉴权。修复后寿命是整个连接的上下文截止时间，读取和写入截止时间都受它约束；每次读取和每帧发送前检查是否到了重鉴权时点。测试 `TestBacklogReplayStillReauthenticatesAndEndsOnTime` 同时覆盖凭证在回放中途过期的情况。
- **终态事件缺 `finished_at`**：`block.updated` 现在带上这次写入实际落库的 `finished_at`；`set` 同时带 `payload_version`（它也会被这次写入改写，属于同一类缺口）。回放测试改为比较完整的 `RunBlock` JSON；停止路径另有断言，确认只改状态的事件也带结束时间。design §2.1 和 OpenAPI `CreativeAgentBlockUpdated` 已同步。

