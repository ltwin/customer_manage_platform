---
risk:
  factors: [data-migration, concurrency]
behavior: changed
reads: []
decisionsPending: true
---

## Why

画布现在的 Cmd+Z 会撤销画布上最近的一步，不管是谁改的，助手的改动也会被撤掉。这违反了 2026-10-16 owner 定下的协同原则（Cmd+Z 只管自己的操作；历史列表和本轮清单可以明确地回退；助手总会被告知），会让助手困惑，也让摄影师在助手持续写入时无法精准撤销自己的操作。

## What Changes

- `creative_changes` 记录写入者（摄影师 / 助手及运行 ID）；存量改动按摄影师处理。
- 画布撤销栈只包含当前摄影师自己的改动。如果要撤销的那一步之后，同一节点或字段又被助手改过，就跳过这一步并提示，不覆盖助手的改动。
- 助手改动的回退改走本轮改动清单或对话（与场景、策划一致）；助手在下一轮得知摄影师做过的修改。
- 服务端撤销接口拒绝撤销非本人写入的改动，防止旧客户端绕过。

## Acceptance Criteria

- WHEN 助手在画布上新建了节点 A，摄影师随后移动了节点 B 并按 Cmd+Z THEN 撤销的是 B 的移动，A 保留；再按 Cmd+Z 也不会删除 A。
- WHEN 摄影师移动节点 B 之后，助手又改了 B 的标题，摄影师再按 Cmd+Z THEN 撤销 B 的移动；如果两次改动是同一字段，则跳过这一步并提示，不覆盖助手的改动。
- WHEN 客户端直接调用撤销接口撤销一条助手写入的改动 THEN 返回 409，不产生改动。
- WHEN 迁移在存量数据上运行 THEN 已有改动都按摄影师写入处理，现有撤销和重做行为不变。

## Impact

- 代码：`backend/internal/creativecanvas`（`graph_state.go` 写改动、`undo.go`、改动读取），`backend/internal/creativeagent`（画布写入时带上写入者），前端 `frontend/src/creative-canvas/editor/graph.ts` 的 `canvasHistory` 与撤销入口，迁移。
- 契约：画布快照的 `changes` 项新增写入者字段；撤销接口新增拒绝条件。
- 外部：无。

## Risk Notes

- data-migration：`creative_changes` 加列并回填，现有约 30 天保留期内的改动全部按摄影师处理。
- concurrency：跳过规则依赖现有字段级 token 冲突检查（`undo.go`），要补「助手写入夹在中间」的测试。
- 待定（`decisionsPending: true`）：画布上助手改动的「本轮清单」入口和变更知晓摘要的具体形态，要和场景 epic 的 C3、V6-079 §7.7 对齐后再写 design。本 change 排在通用文档底座和策划之后，开工前重新确认范围。
