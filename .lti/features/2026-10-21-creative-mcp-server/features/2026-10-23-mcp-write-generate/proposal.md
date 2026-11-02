---
risk:
  factors:
    - trust-boundary
    - data-migration
    - broad-impact
behavior: changed
reads:
  - creative/external-agents
  - creative/library
  - creative/documents
decisionsPending: false
---
# 本次交付

## Why

本子项是 Epic `creative-mcp-server` 的 C2。C1 已经打通接入和只读，owner 2026-10-23 在 Claude Code 里实测后验收。C2 让外部助手开始干活：改画布、写提示词、生成图片、整理资产库、压缩图片。

C2 承担 Epic 的 AC-4、AC-5 中的生成部分，以及 AC-10 中与写入和生成有关的部分。roadmap 给 C2 列的三个不确定性，本子项这样解决：

- **资产库多阶段写入在没有运行时怎样恢复**：日志改挂外部连接，用时间窗口代替「运行已结束」来判断要不要收尾。
- **异步句柄能不能在各宿主的默认超时下用**：生成等 40 秒，没完成就返回句柄，再由 `wait_tasks` 每次最多等 45 秒。Claude Code、Codex CLI 在本机实测；ChatGPT 和 claude.ai 连接器要公网 https，留到有域名的环境再测。
- **画布写入署名怎样和 `canvas-undo-own-changes` 衔接**：`canvas-undo-own-changes` 还是草稿，C2 先落地。按 roadmap 的约定，C2 的画布写入不署名，由 `canvas-undo-own-changes` 落地时把外部连接一并纳入。画布没有历史列表界面，这不影响 AC-7 的场景部分。

## What Changes

1. **外部写工具**（`api: MCP 写入与生成工具`）：MCP 上新增 11 个工具。
   - 画布：`edit_canvas`、`set_node_prompt`、`generate_node`、`compress_image`；
   - 资产库：`save_to_library`、`organize_assets`、`manage_library`、`trash_assets`、`restore_assets`、`read_library_result`；
   - 外部专用：`wait_tasks`。

   同名工具和内置助手走同一个领域入口、同一套校验和错误。写画布的工具都要显式传 `canvas_id`，成功结果带 `canvas_url` 和 `focus`。
2. **生成**：
   - `generate_node` 新增可选 `dry_run`。为 `true` 时走完生成前的全部校验，包括额度，然后整体回滚：不建生成执行，不扣费。返回将用的模型、参数、参考和预计扣费。
   - 实际生成和摄影师点「生成」走同一个扣费入口。40 秒内完成就直接返回结果，图片随结果附上；没完成就返回任务句柄。
   - 生成执行记为外部助手发起，并记下连接 ID，`wait_tasks` 据此只认本连接创建的句柄。
3. **`wait_tasks`**：一次最多 8 个句柄、最多等 45 秒，任一句柄到达终态或等待超时就返回全部句柄的状态。完成的生成随结果附上图片。
4. **助手主体在写路径上的其余职责**（Epic design「助手主体」）：
   - 写入者、幂等种子、生成调用方、资产库日志归属，以及写事务里对发起方的复核，都改为由调用方提供，运行和外部连接各实现一份。
   - 运行路径的行为不变。
5. **数据**：一个迁移。
   - `creative_node_executions` 的调用方增加外部助手，并新增连接 ID 列；
   - `creative_agent_library_calls` 的运行、对话两列改为可空，新增连接 ID 列，三者满足「运行 + 对话」或「外部连接」二选一。
6. **画布交接链接**：画布页读取链接里的 `focus` 参数，打开时定位到这些节点。
7. **服务端 `instructions` 与工具说明**：补上写入、生成、预检、长任务的用法（以原型 `docs/prototypes/creative-mcp/instructions.md` 为起点）。

不在本子项：
- 卡片（C2b）。
- 场景与 Skill 工具、场景历史署名（C3）。外部助手用 `edit_canvas` 新建场景节点，在 C2 会被拒绝，并说明场景工具稍后开放，原因见 Risk Notes。

## Acceptance Criteria

- **AC-C2-1 改画布**：**WHEN** 外部助手用 `edit_canvas` 新建、移动、连接节点，或用 `set_node_prompt` 写提示词，**THEN**
  - 浏览器里打开的画布实时出现变化；
  - 结果第一行写明改了什么，带可点击的 `canvas_url`；
  - 打开链接后画布定位到改动的节点。
- **AC-C2-2 生成**：**WHEN** 外部助手对写好提示词的节点调用 `generate_node`，**THEN**
  - 生成按与摄影师相同的规则扣额度；
  - 40 秒内完成时，结果带新版本的内容修订 ID，生成的图片作为图片内容附上（最多 4 张，长边不超过 1024）。
  - **WHEN** 节点上的提示词在此之后被别人改过，**THEN** 拒绝生成，并提示重新读取。
- **AC-C2-3 预检**：**WHEN** 外部助手以 `dry_run: true` 调用 `generate_node`，**THEN**
  - 返回模型、参数、参考清单、预计扣费（注明是估算）和本月剩余额度；
  - 不建生成执行、不预留额度、不扣费，画布没有变化；
  - 校验不通过时，错误和真实生成相同（例如「本月生成额度不足」）。
- **AC-C2-4 长任务**：**WHEN** 生成 40 秒内没有完成，**THEN** `generate_node` 返回 `{status: "pending", task_id, kind: "generation"}`，外部助手用 `wait_tasks` 继续等，单次不超过 45 秒，最终拿到与同步完成时相同的结果。**WHEN** `wait_tasks` 传入别的连接或别的账号的句柄，**THEN** 结果是 `not_found`。
- **AC-C2-5 资产库**：**WHEN** 外部助手把画布节点存入资产库、整理分组和标签、移入回收站或恢复，**THEN** 结果与内置助手相同，资产库页面实时更新；`read_library_result` 只能读到本连接的整理结果。**WHEN** 一次多阶段整理在执行中断开（服务重启），**THEN** 之后的 `read_library_result` 把它收尾成已完成或未执行的明细，不会一直停在执行中。
- **AC-C2-6 压缩**：**WHEN** 外部助手调用 `compress_image`，**THEN** 40 秒内完成时节点切换到压缩后的版本，结果带原始大小、输出大小和减少比例；超时则放弃，图片不变。
- **AC-C2-7 隔离与断开**：
  - **WHEN** 任一写工具传入别的账号的画布、节点或素材，**THEN** 结果是 `not_found`，什么都没有改。
  - **WHEN** 创作者在生成提交、资产库整理的过程中断开连接，**THEN** 之后的生成不再受理、整理的后续阶段不再执行；已经提交的生成照常完成和扣费（Epic X-I3）。
- **AC-C2-8 场景节点**：**WHEN** 外部助手用 `edit_canvas` 新建 `scene` 节点，**THEN** 这批动作全部不生效，结果说明场景工具稍后开放。
- **AC-C2-9 内置助手不变**：内置助手同一组工具的行为、结果和署名都不变，现有测试照常通过。
- **AC-C2-10 实测**：owner 在 Claude Code 和 Codex CLI 里，让外部助手改画布、预检、生成、等待并整理资产库，流程不出错。宿主版本和问题记入 `host-runs.md`。ChatGPT 和 claude.ai 连接器的超时与图片展示，留到有公网域名的环境再测，验收时记为限制项。

## Impact

- 后端：
  - `backend/internal/creativeagent`：写工具从「依赖运行」改为「依赖调用方接口」；外部目录增加 11 个工具；新增 `wait_tasks`；资产库日志按调用方归属；更新外部版 `instructions`。
  - `backend/internal/creativegeneration`：生成调用方，以及 `dry_run` 的回滚式预检。
  - `backend/internal/creativecanvas`：生成执行落库时写入调用方。
  - `backend/internal/creativeimageedit`：外部调用沿用 `agent` 调用方。
  - `backend/internal/creativemcp`：每次调用生成调用 ID，并标注删除类工具。
  - `backend/internal/platform/oauth`：提供在事务内复核连接有效的检查。
  - `backend/internal/platform/httpapi`：把上面这项检查接进外部工具。
  - `backend/internal/platform/store`：迁移 `0081`。
- 前端：`frontend/src/creative-canvas/editor/CanvasView.tsx` 读取 `focus`。
- 契约：交付 `api: MCP 写入与生成工具`。MCP 工具不在 OpenAPI 里，`api/openapi.yaml` 不变。
- 接入物料：Codex 插件不变（工具超时已是 300 秒）。

## Risk Notes

- **需要 owner 同意的 Epic 修订**：roadmap 让 C2 交付 `data: 通用文档`（MODIFY，文档写入者增加外部助手），建议把它挪到 C3。
  - 原因：C2 里外部助手唯一会写到通用文档的地方，是 `edit_canvas` 新建场景节点时附带建出的空场景。这条修订要用 `external_agent` 署名，可是修订历史接口和场景历史界面要到 C3（`api: MODIFY 列出文档修订`）才认这个取值。C2 也没有场景工具，新建出来的空场景外部助手用不上。
  - 改法：C2 拒绝外部新建场景节点；文档写入者的迁移、历史接口和署名在 C3 一起交付。Epic 只改 roadmap 的交付列和 design、contracts 里标注的批次（C2 → C3），目标、契约正文和选型都不变。
  - 另一种做法：C2 同时交付文档写入者迁移，并提前做修订历史接口对 `external_agent` 的支持。这样会把 C3 的一部分（历史接口与前端署名）拉进 C2，范围更大。
- **重构运行路径**：内置助手的写工具要改成依赖调用方接口，影响面大。用现有的内置助手测试兜底，另加外部与内置的对照测试（Epic X-I4）。
- **重复调用**：MCP 没有幂等键，宿主重试同一调用会得到新的调用 ID。
  - `generate_node` 能拦住节点上还在进行的生成，以及提示词已被改过的情况。
  - 拦不住的是：前一次已经完成后，用同一版提示词再调一次。这会再生成一次、再扣一次费，和摄影师再点一次「生成」是同一种操作。Epic 契约说「依靠 `draft_revision` 拦截重复生成」说过头了，随修订 v3 改成实际行为。
  - 缓解：生成 40 秒就返回，远小于宿主的超时；工具说明和 `instructions` 写明「没有收到结果时先读画布，不要直接重发」。
- **断开与在途写入**：
  - 生成受理和资产库每个阶段提交前，都在同一事务里复核连接有效，断开后不再有新的生成或整理阶段落库；
  - 画布命令和内置助手一样，只在调用开始时检查（Epic X-I3 只要求下一次请求被拒绝）。
- **`dry_run` 的估算**：预计扣费取网关生成价格的估算值，可能与实际扣费有差异，结果里注明是估算。
- **实测花费**：真实生成要花 owner 的额度。本机试用环境没有配置生成模型；生成链路用假生成器做集成测试，真实生成由 owner 在自己配了模型的环境里测。
