---
doc_type: issue-report
issue: 2026-10-10-agent-node-references
status: open
source_feature: 2026-10-09-image-generation
created: 2026-10-10
tags: [creative-agent, canvas, references, follow-up]
related_items: [V6-028, V6-039]
---

# 创作助手的节点引用表达不完整

## 1. 摘要

2026-10-09 owner 测试助手改图时发现：指定某个节点让助手修改，助手对「上传的节点」和「生成过的节点」做法不一致。行为本身已通过工具说明修正（见 image-generation design §3.10），但排查中暴露出节点引用在助手这里的表达有几处缺口，owner 要求先记录、后续处理。

## 2. 现状（2026-10-10 代码事实）

| 方向 | 怎么表达 | 代码位置 |
|---|---|---|
| 摄影师在对话里 @ 节点 → 助手 | 发送该节点**当前内容的修订 ID**（`content_ref`），模型只看到 `<<<ccrv_…>>>` 标记，没有节点 ID、标题、类型 | `frontend/src/creative-canvas/editor/agentReferences.ts`，`backend/internal/creativeagent/instruction.go`、`inputs.go` |
| 助手读画布 | `read_canvas` 返回节点 ID、内容修订 ID、生成设置和连线 `{source, target}` | `backend/internal/creativeagent/context.go` `canvasView` |
| 助手改画布 | `edit_canvas`、`set_node_prompt`、`generate_node` 都用节点 ID；提示词里 `@[节点ID]` 点名参考 | `backend/internal/creativeagent/canvas_tools.go` |
| 助手 → 摄影师 | 工具块的 `node_refs` 字段一直为空，只能在文字里提标题或 ID | `backend/internal/creativeagent/blocks.go` `writeToolBlockInTx` |

## 3. 问题

1. **对话里引用的是内容，不是节点**。助手要改「这个节点」时得用修订 ID 反查节点：节点生成新版本后旧修订 ID 对不上；复制出的节点可能共用同一修订，对应不唯一。
2. **很多节点无法被 @**。前端只把带内容的文字、图片节点列为可引用素材，空节点、链接、视频、音频节点选不到。
3. **助手不知道摄影师选中了哪些节点**（V6-028 未做）。
4. **同一写法两种含义**：对话里的 `@[…]` 是内容修订 ID，提示词里的 `@[…]` 是节点 ID，模型容易混淆。
5. **`read_canvas` 看不到草稿附件**：提示词草稿上直接附的参考（`node_inputs`，非连线）没有返回，助手看不到，却会在生成时被使用。
6. **工具块不标注涉及的节点**：`node_refs` 为空，界面无法显示可点击的节点标签（V6-039）。

## 4. 建议方向（未拍板）

1. 对话新增「节点引用」片段：发送节点 ID，后端在发送时解析出标题、类型和当前内容修订 ID 呈现给模型（如 `<<<节点 cwnode_…「日出人像」图片，当前内容 ccrv_…>>>`）；资产库里不在画布上的素材仍按内容引用。解决问题 1、2、4。
2. 发送消息时带上画布当前选中的节点（V6-028 的核心部分）。解决问题 3。
3. `read_canvas` 补充草稿附件。解决问题 5。
4. 写工具在结果里给出涉及的节点，工具块据此填 `node_refs`；界面标签随 V6-039 做。解决问题 6。

1、3、4 改动较小；2 牵涉前端发送逻辑，工作量中等。

## 5. 同期记录的相关缺陷

- `set_node_prompt` 的参数只靠说明文字约束：见 image-generation design §3.11。
