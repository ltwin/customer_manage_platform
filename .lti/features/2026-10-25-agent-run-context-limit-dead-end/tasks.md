# Tasks

## 1. 运行内裁剪

- [ ] 1.1 红：`context_test.go` 补运行内裁剪用例（截短即够不移除、需要移除时只一条说明并逐步列定位符、再次移除更新同一条说明、最近一步超限原样返回、调用与结果始终成对），以及同时带 `inputKindKey` 与历史分组的 Skill 索引在历史删光后仍在；跑一遍确认全部失败
- [ ] 1.2 `tool_content.go` 新增 `resultLocators(toolName, content)`（`item_id` / `result_id` / 内容修订 ID，去重，最多 8 个），附单测
- [ ] 1.3 `compactMessages` 统一保护判定，阶段 3 只删未受保护的消息；新增阶段 4（较早步骤的工具结果截到 768 字节并附定位符）和阶段 5（整步移除，维护唯一的运行内说明与有界步骤索引）；1.1 全绿，已有 `compactMessages` 用例仍绿

## 2. 计量口径

- [ ] 2.1 红：走真实的改写→绑定→`prepareModelStep` 路径，带 `read_asset` / `render_view` 图片和思考内容，预算在临界区间逐字节扫描，断言最终请求按 `EstimateInputTokens` 不超上限；跑一遍确认失败
- [ ] 2.2 抽出 `toolImageLabel`，`bindMessages` 改用它且行为不变；`size()` 计入 `ReasoningContent` 与图片绑定消息的 16 字节 + 标签；2.1 绿

## 3. 可恢复

- [ ] 3.1 红：运行中 `read_history` 能读到本次运行较早的工具消息，仍读不到其他窗口在触发点之后追加的消息；移除一个只带 `item_id` 的步骤后，用说明里的 `item_id` 调 `read_run_result` 读到原结果；跑一遍确认前者失败
- [ ] 3.2 `readHistory` 范围改为 `ordinal<$before AND (ordinal<$trigger OR run_id=$run)`，`before_ordinal=0` 不设上限；`contextToolInfo` 更新 `read_history` 描述；3.1 绿，已有 `read_history` 用例仍绿

## 4. 压缩覆盖超大的最近一轮

- [ ] 4.1 红：`compaction_test.go` 补唯一一轮超预算时 `/compact` 生成检查点覆盖到该轮末尾、失败后直接发下一句先自动压缩且摘要请求含该轮、唯一一轮超过扫描行数时 `/compact` 仍有检查点、超大一轮开头的约束与全部补充都在摘要请求里；跑一遍确认失败
- [ ] 4.2 `planCompaction` 保留轮数取 `min(CompactRecentTurns, Kept)`，空投影且未扫描完时覆盖到 `Before - 1`；`compactionRequestInTx` 两遍裁剪，创作者的行最后舍弃；4.1 绿，`TestTheCompactCommandSkipsAShortConversation` 与 `TestCompactionPreservesCurrentInstructionAndToolPairs` 等已有用例仍绿

## 5. 端到端与前端

- [ ] 5.1 端到端用例：桩模型连续返回工具调用、工具返回大结果、上限调小，运行成功不以 `creative_context_limit` 失败
- [ ] 5.2 `agentState.ts` 的 `creative_context_limit` 文案提示可以压缩上下文后继续
- [ ] 5.3 门禁：`make check-go PKG=./internal/creativeagent/...` 与 `make check-frontend` 通过
