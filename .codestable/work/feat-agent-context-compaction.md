---
type: feat
design: .codestable/features/2026-10-14-agent-context-compaction/agent-context-compaction-design.md
branch: feat/creative-workspace-redesign（worktree .worktrees/creative-workspace-redesign）
---

# 助手上下文压缩：实现游标

## 目标

按已批准设计（revision 4）实现：上下文压缩、`/compact`、Skill 索引与按需加载、上下文用量小环、称谓中性化。

## 现场

- 基线 commit `47f9ae6`。Owner 于 2026-10-15 明确批准提交本批功能与后端评审修复。
- 已完成：
  - **后端**
    - 模型窗口 `context_window_tokens`，内置模型 256k（2026-10-15 由 128k 调整）。
    - Limits 升到 `creative-agent-8`。
    - 迁移 0066。
    - 压缩核心：`compaction.go`。
    - token 计量与用量：`context_window.go`。
    - 历史投影：`inputs.go`。
    - 提交时判断与 `/compact`：`runs.go`、`instruction.go`。
    - worker 分支：`execute.go`。
    - `load_skill` 加载索引版本：`runtime.go`。
    - 撤销授权使检查点失效：`consents.go`。
    - 模型可见文字统一用「创作者」，并加语法树扫描测试。
  - **契约**：OpenAPI 与 Go、TS 生成物已同步。
  - **前端**
    - 压缩分隔条：`AgentBlocks.tsx`。
    - 命令菜单与发送：`AgentPanel.tsx`。
    - 上下文用量小环：`AgentContextRing.tsx`，计算逻辑在 `agentContext.ts`。
    - 用量事件合并：`agentBlocks.ts`、`agentRuns.ts`。
  - **文档**：`CONTEXT.md` 新增两个词条；`docs/dev/creative-agent.md` 新增一节；设计文档 §18 实现记录。

## 边界与关键事实

- 网关预留按「1 字节 ≤ 1 token」定上界，请求字节数 × 单价不能超过预留。因此字节上限 = `min(InputTextBytes, 窗口 × 4)`。
- 压缩步骤占用序号 1，接住运行创建时的首轮预留；首轮模型调用改为序号 2，另行预留。
- `runExecution.run` 统计步骤数时排除 `kind='compaction'`。
- 网关单请求最多 400 条消息：冻结历史 ≤ 240 条，运行内兜底 ≤ 380 条。
- 旧运行的 limits 快照里新字段为零值：不压缩，字节上限退回 `InputTextBytes`。
- 本机 5173 端口被占用；面板 e2e 用 5179 端口的 Vite。

## 证据

- 后端：`go test -count=1 -parallel=1 ./internal/creativeagent ./internal/platform/llmgateway ./internal/platform/store ./internal/platform/httpapi`。
- `make generate-check`、`make check-frontend` 通过。
- 后端评审修复后，完整 `make check-go` 通过；最后预算修复后的 `make check-go PKG=./internal/creativeagent/...` 通过（测试 79.144s，lint 0 issues）。
- `compaction_regression_test.go` 覆盖小窗口、压缩中断恢复、历史条数触发、窗口快照、Skill 正文排除、最近轮次保留、检查点替换，以及授权扩大后原话/摘要回退；预算包含 Skill 中间件时与实际发送字节一致。
- e2e：`agent-context`、`agent-history`、`creative-agent-blocks`、`agent-markdown`、`creative-agent`、`agent-asset-lookup`、`agent-library-organize` 全部通过。

## 验收

设计 §15 第 1 到 11 条有自动化证据。第 12 条（连续性评测）需要用 owner 的长对话样本、在真实模型上运行。

## 状态与未决

- 状态：实现与后端评审修复完成，owner 自行检查后批准提交。最终预算增量通过回归，未追加第五轮独立复审。
- 未决：
  - 连续性评测（§15 第 12 条）。
  - 真实模型下压缩摘要的质量走查。
