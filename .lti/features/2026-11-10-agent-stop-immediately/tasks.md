# Tasks

## 1. 取消一步进入终态（后端）

- [x] 1.1 先改写 `TestCUnknownWaitsForReconciliationAndCanBeClosed`，并新增四个测试：重复取消、遗留 `reconciling + cancel_requested_at` 被 sweeper 结束、旧 `reconciling` 回执回放后新 key 得到终态、取消 A → 同会话 B 跑完 → A 迟到返回不污染 B。确认在旧代码上是红的
- [x] 1.2 `control.go:controlCommand` 删除 cancel 转入 reconciling 的分支，`sweepRun` 改为只要 `cancelled != nil` 就结束运行。1.1 全部变绿，`TestCCancelDuringDispatchDoesNotPublishLateAnswer` 不改、仍然通过

## 2. worker 及时中断（后端）

- [x] 2.1 先写 `cancel_watch_test.go`：流式中取消后 2 秒内供应商 ctx 被取消；nil、立即报错、阻塞到 ctx 取消三种 notifier 下模型调用照常发出、取消后由续租兜底、attempt 结束后监听协程退出。确认是红的
- [x] 2.2 新增 `cancel_watch.go:watchCancel`（异步订阅、500ms 限流、2s 查询超时）；`service.go` 加 `notifier` 字段与 `SetNotifier`；`execute.go:workRun` 与 `renewLease` 并列启停监听；`cmd/creative-worker/main.go` 注入 `db`。2.1 变绿，`make check-go PKG=./internal/creativeagent/...` 通过

## 3. 契约文字与生成物

- [x] 3.1 `api/openapi.yaml` 改 cancel 的 summary 和 200 说明（新版首次执行即终态，回放返回当时快照）；`make generate` 后提交 `api.gen.go` 与 `schema.d.ts`，`make generate-check` 无漂移

## 4. 前端：发送/停止合一与乐观停止

- [x] 4.1 `agentRuns.ts` 加 `stopping` 冻结快照、`freezeStopping` / `releaseStopping`，`put()` 读到终态时撤掉快照；`AgentBlocks.tsx` 渲染 `stopping ?? runs`、stopping 视为 finished，并去掉费用核实文案；`agentState.ts` 删除 `hasUnsettledCost`，`creative_reconciliation_closed` 文案改为「已停止」；同步更新 `creative-agent.test.ts`，`make check-frontend` 通过
- [x] 4.2 `AgentPanel.tsx`：按决策表实现发送/停止合一按钮；`stop()` + `cancelUntilFinished()`（非终态换新 key 重发一次，失败回滚并提示）；`sendDraft` / `sendCommand` 先等正在进行的停止；`RunStatus` 去掉两个按钮，不再调用 `close-reconciliation`；`agent.css` 加停止态按钮样式、删除状态栏按钮样式。`make check-frontend` 通过
- [x] 4.3 新增 `frontend/scripts/agent-stop.e2e.mjs`（mock HTTP）。用例：按钮随状态切换；cancel 延迟 3 秒期间持续推送文本事件，文字保持不变、完成后显示服务端内容；cancel 失败回滚到最新真实视图；首次响应非终态时用新 key 重发；停止中发送会等停止完成。一次跑一个用例，换端口，全部通过

## 5. 文档

- [x] 5.1 更新 `docs/dev/creative-agent.md` 的 CancelRun 段、`docs/product/creative-canvas-system/modules/harness.md` §5、`agent-api-events.md` 的 cancel / close-reconciliation 两行，与新语义一致；相对链接检查通过
