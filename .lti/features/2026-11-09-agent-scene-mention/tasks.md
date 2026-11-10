# Tasks：助手引用画布节点与 3D 场景物件

## 1. 契约

- [ ] 1.1 `api/openapi.yaml` 新增 `scene_ref` 片段与消息块、`CreativeAgentMessageScene.revision` / `objects[].missing`、`objects.minItems` 放宽为 0、`scene_context.selected_object_ids` 标 deprecated；`make generate` 后 Go/TS 两份生成物提交，`make check-go PKG=./internal/creativeagent/...` 与前端 `tsc` 编译通过

## 2. 后端

- [ ] 2.1 `instruction.go`：`InstructionSegment` 新增 `DocumentID` / `ObjectIDs`，`validSegments` 加 `scene_ref` 形状校验、跨类型字段检查和 `maxSceneRefs = 5`，`ResolveInstruction` 生成 `scene_ref` 占位块；`instruction_test.go` 先红后绿覆盖 400 各分支、超过 5 个场景 413、同一场景两个引用各 40 个物件通过
- [ ] 2.2 `creativescene.ReferenceContextInTx` 新增（账号范围读取标题、版本、物件名）；`CreateRun` 事务内检查 `canvas_metadata` consent、解析引用并填快照（missing 只有 ID）；`BlockScene.Revision`、`BlockSceneObject.Missing`、`validBody` 接受 `scene_ref`；`scene_context_test.go` 覆盖 404 / 403 / 快照 / missing，失败时 `runs` 表数量不变
- [ ] 2.3 冻结与措辞：`freezeRequestInputsInTx` 按位置写 instruction 行，抽出 `sceneRefWording` 供 `historyLine` / `bodyMessage` 共用，正在编辑的场景写「当前编辑的场景中的物件」，`sceneInputsInTx` 把被引用场景加入变化摘要候选，`EditorContextInTx` 无选中时换新措辞；测试断言多段语序、编辑场景只出现一次、冻结后改名重试请求哈希不变、旧 `selected_object_ids` 测试不改仍绿
- [ ] 2.4 预算：`bodyBytes` 按 `scene_ref` 实际措辞加 `perMessageOverhead` 计费，事务内补齐后重算 `planned.Bytes`，`validBody` 超过 32 KiB 返回 413；临界预算测试（补齐前低于压缩线、补齐后超过 → 触发压缩规划；补齐后超 32 KiB → 413 且 `runs` 不变）
- [ ] 2.5 `scene_ref_live_test.go`：`CREATIVE_AGENT_LIVE=1` 门控的三条模型行为验收（跨场景同名物件、同场景分批、已删除物件先询问且本轮无写场景工具调用）；默认 `go test` 跳过，手动执行一次并记录结果

## 3. 前端数据层

- [ ] 3.1 `PromptReference.scene?`、`agentReferences` 输出 `scene_ref` 片段（本机键 `sref_*` → `sceneRefs`，只序列化正文用到的键）、`agentState` 的 `AgentDraft.scenes` / `readDraft` 与 `messageSteps` 固定传 `selected_object_ids: []`；`creative-agent.test.ts` 覆盖本机键 ↔ 片段、草稿恢复、5 个场景上限、两个引用各 40 个物件
- [ ] 3.2 `agentOutgoing` 的 `outgoingParts` / `messageParts` 显示场景引用 chip（整个场景、物件列表、「已删除的物件」）；`agent-outgoing.test.ts` 新用例通过

## 4. 物件列表与菜单搜索

- [ ] 4.1 实测 U1（开发库物件最多的场景，`readScene` 响应体积与耗时），结论记入 design 未决事项；超出假设则停下修订 design
- [ ] 4.2 抽出 `creative-scene/outlineRows.ts`，`Outliner` 改用且行为不变；新增 `scenePickerModel.ts`（单击 / 修饰键 / Shift 范围 / 拖选区间、搜索保留上级、清空搜索保留选中）；`scene-picker.test.ts` 通过
- [ ] 4.3 `ScenePicker.tsx` 接入 `PromptEditor` 的场景展开（首行「整个场景」、拖选 `setPointerCapture` + 边缘自动滚动），点场景 chip 重新编辑时生成新键并用 `execCommand('insertHTML')` 替换；`generation.css` 补样式
- [ ] 4.4 `@` 菜单顶部搜索框（与 `query` 同步）、按「场景 / 图片 / 文本」分组、`highlighted` 节点排前、最大高度 320px；`make check-frontend` 通过

## 5. 待选引用

- [ ] 5.1 `promptDOM` 新增 `pendingChip`，`readPromptDOM` 跳过 `[data-pending]`；`PromptEditorHandle` 新增 `setPending(id|null)` / `commitPending()`，编辑器有焦点时 `setPending` 不生效，聚焦时自动转正、光标停在其后
- [ ] 5.2 `AgentPanel`：选中目标边沿检测（画布单选 / 编辑器选中集合，切换视图重置基线并移除待选）、`pendingKey` 计入发送资格、点发送先 `commitPending` 再经 `sendQueued` effect 调 `sendDraft`、删除 `SceneSelectionTag` 浮层；`WorkspacePage` 传 `canvasFocus` / `editingScene`；`make check-frontend` 通过

## 6. e2e 与收尾

- [ ] 6.1 新增 `agent-scene-mention.e2e.mjs`（画布待选插入 / 替换 / 转正、选中没变不新增、空正文只有待选时发送、图片能力限制拦截、搜索 200 节点 < 100 ms、二级菜单多选与拖选、剪切→粘贴、删除→撤销、插入→撤销→重做、编辑→撤销），一次只跑一个用例；同时实测 U2（500 个物件列表帧耗时）并记入 design
- [ ] 6.2 改 `creative-scene-agent.e2e.mjs`：去掉浮层标签断言，改为编辑器选中 → 待选 chip → 正式 chip → `scene_ref`，并断言分批语序的 `instruction_segments`；逐个用例跑通
