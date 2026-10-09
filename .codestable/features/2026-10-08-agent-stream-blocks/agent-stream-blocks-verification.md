---
doc_type: feature-verification
feature: 2026-10-08-agent-stream-blocks
requirement: V6-030
date: 2026-10-09
---

# V6-030 验证记录

030.1（后端推送底座）的落地内容和测试见[实现记录](agent-stream-blocks-030.1-implementation.md)。本文记录 030.2（前端块渲染与流）和四条 AC 的证据。

## 030.2 落地内容

| design §4 | 实现位置（`frontend/src/creative-canvas/editor/`） |
|---|---|
| 合并规则、过程段派生 | `agentBlocks.ts`：`applyEvent`（seq 连续、revision 逐一递增，否则回读快照；未知信封版本停止合并）、`mergeSnapshot`、`layout`、`segmentSummary` |
| 运行 store 与流控制器 | `agentRuns.ts`：zustand store 放在面板之外；`followRun` 先读快照再订阅，未结束的 run 才订阅；流连不上时每 2 秒轮询快照 |
| 块组件 | `AgentBlocks.tsx`：`BlockView` 按类型分派并 memo；展开状态按 block id 存在 store 里；工具详情展开时才读取 |
| Markdown | `markdown.ts` 解析成树，`Markdown` 直接生成 React 元素；没闭合的 `**`、`` ` `` 按普通文本显示 |
| 面板 | `AgentPanel.tsx`：历史区改为「用户气泡 + 这次 run 的块」；输入框上沿的状态条显示当前步骤和「停止」；入口按钮上的点表示运行中（琥珀）或等待补充（白） |
| `watchStream` | `onEvent` 返回 `'snapshot'` 时断开并回读快照，再从新游标续读 |

## 顺带删掉的旧设计（owner 2026-10-09 要求）

- 每次发送前的外发同意勾选框，以及「带入最近对话引用的 N 份素材」勾选框。发送时仍然为这条消息创建一份授权，后端逐次校验不变。首次说明和范围复用还是 V6-029 的事。
- 底部的「运行状态」卡片（用量已结算、查看 N 个步骤、恢复此读取）。过程现在就在对话里；失败的 Skill 资源读取在它自己的工具行上给「重试这一步」。
- 消息上的「我 / 创作助手 / 运行记录」角标、工具结果的 `<details><pre>`、引用素材的 `<<<id>>>` 展开块。引用现在是气泡里的 chip。
- 每 2 秒轮询 runs / messages / steps。列表只在打开、发送后和 run 状态变化时读取。
- 「对话已创建，可以开始输入问题」提示，以及必须先手动新建对话才能输入：没有对话时，第一条消息会自动开一个。
- 「上次提交的结果尚未确认。请确认原操作」改为「上一条还没有确认送达 · 重新发送」，机制不变。

## AC 证据

mock e2e：`frontend/scripts/creative-agent-blocks.e2e.mjs`。HTTP 用 route 模拟，SSE 用包了一层的 `fetch` 由测试逐帧推送；每条 AC 一个新的 browser context。

| AC | 证据 |
|---|---|
| AC-01 稳定块 ID、流式 Markdown、展开/悬停不重置 | e2e 第 1 段：工具行展开详情后继续推 5 次正文增量，行仍展开、详情仍在，DOM 元素是同一个；`**主` 未闭合时是普通文本，闭合后变粗体；列表、行内代码逐步出现。后端：`TestBlocksAndEventsRebuildTheSameList` |
| AC-02 工具状态来自服务端，过程段收起为耗时 / 操作次数，可回看 | e2e 第 2 段：两个工具行在运行中直接显示、没有段头；后面出现正文后收成「已处理 6 秒 · 2 次操作 · 1 项失败」；点开后能看到两行和详情，之后的增量和 run 结束都不会把它收回去。单测：`segmentSummary`、`layout` |
| AC-03 计划步骤状态可见，缩略图挂在对应工具行下 | e2e 第 3 段：计划的完成 / 进行中 / 跳过 / 待办各自有样式，更新后计数从 1/4 变 2/4；带 `node_refs` 的工具行下出现该节点的缩略图，另一行没有。`node_refs` 是 mock 的，真实链路等 V6-034/036 |
| AC-04 刷新重建同一顺序，不伪造推理 | e2e 第 4 段：刷新后块的类型、ID、状态和顺序与刷新前一致，正文相同；没有思考块时页面上不出现思考块，只有状态条写「正在思考」。后端：DeepSeek 的 `reasoning_content` 不落库（030.1） |

设计 §6 列的其余场景：

- 断流重连：e2e 第 5 段，第二次连接带 `after_seq=2`，断开期间的 3 个事件补齐，正文是「一二三」，没有重复块；之后人为跳过 3 个 seq，页面回读快照并恢复。
- 收起面板后仍在跟踪：e2e 第 6 段，入口按钮在运行中带 `data-state="run"`，run 结束后消失；重新打开时正文完整，展开的行还是展开的，期间最多多读一次快照。
- 流不可用时轮询：e2e 第 7 段。
- 单测 `frontend/scripts/creative-agent-blocks.test.ts`：合并结果与快照相等、未变化的块保持同一引用、缺口 / 跳号 / 未知块 / 未知信封、迟到的快照只更新 run 字段、过程段、Markdown。
- 回归有效性：把 `AgentPanel.tsx` 和 `agentState.ts` 换回 HEAD（`bdd50bc`）后，块 e2e 在第一步就失败（页面上没有 `.cc-agent-turn`）。
- 原有 `creative-agent.e2e.mjs`（提交身份、刷新恢复草稿、素材引用）去掉勾选步骤后通过。
- 门禁：`make check-frontend` 通过（424 项）。

**未运行**：接真实后端和真实模型的整条链路走查。mock 的帧格式和 `realtime` / `RunEventSource` 的输出逐字段核对过，但没有在浏览器里跑过一次真实回复。

## 已知取舍

- 打开对话时，每个 run 各读一次快照，没有做懒加载。对话很长时会有一批并发请求。
- 一个 run 中途的补充消息（`waiting_input` 后的用户气泡）排在这次 run 的全部块之后，而不是插在中间。V6-031 引入提问块时一起处理。
- 030.1 记录的两点仍在：同一回答的多个工具会一起显示为运行中；计划块排在同一回答的工具行之后。
- 自动跟随滚动只做了最基本的「在底部就跟随」，「回到最新」按钮属于 V6-041。
