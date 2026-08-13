---
doc_type: feature-review
feature: 2026-08-05-plan-ingestion-capture
status: blocked
review_state: full-rereview-after-qa-fix
reviewer: subagent
review_round_cap: 3
round: 3
reviewed: 2026-08-07
lane_a_state: completed
lane_a_ref: "plan_ingestion_code_review_r3"
lane_a_reason: ""
lane_b_state: failed
lane_b_ref: ""
lane_b_reason: "ocr review 启动后返回 invalid line range 警告并持续无响应；主进程以退出码 130 中止，未形成可核验 OCR 结果"
---

# plan-ingestion-capture 首次实现审查报告

## 结论

首次独立 Task reviewer 因 provider 余额/权限失败（HTTP 403，account balance is negative）未能返回。用户随后明确要求“继续”，主 agent 将其作为本轮 OCR/local 降级审查的 owner continuation 授权；因此本报告不伪造 subagent 结果，而以 OCR + 本地事实核验完成本轮 closure。review round 仍为 1，没有启动第 2 或第 3 轮完整审查。

本轮确认并已修复以下真实问题：

| ID | 严重度 | 来源 | 问题 | 窄修复 |
|---|---|---|---|---|
| REV-001 | blocking | ocr + local | public transition 接口接受 `committed`，可绕过 core/reference/media/activity 的 atomic commit | HTTP 与 application transition 仅接受 `abandoned`；`committed` 仍只由 `Commit` 在同一事务内写入 |
| REV-002 | important | ocr + local | commit session validation 按 candidate ID 硬编码解析 link target，不能可靠验证自定义 client refs | kept shot/readiness map 改按实际 accepted client ref 验证 |
| REV-003 | important | ocr + local | 前端将非 plan reference link 的 target ref 改写为 plan ID | 保留 snapshot 中的 `target_client_or_id_ref` |
| REV-004 | important | ocr + local | readiness link 状态是单值，未实现设计要求的 0..N 显式关联 | UI 改为 `Record<string, string[]>` 与多选；commit 为每个 pair 生成 `LinkDecision` |
| REV-005 | important | ocr + local | review 只渲染 active candidates，discard 后不能恢复 | ReviewStep 渲染完整 candidates，统计仍使用 kept 集合 |
| REV-006 | important | ocr + local | 所有候选/参考链接 discard 后仍可进入保存摘要，commit 静默 return，形成死路 | 摘要按钮改按 kept candidates/references/assets 判定 |
| REV-007 | nit | ocr + local | `.planning-panel` 摄取样式会污染既有 planning workspace；focus token 使用未定义 `--accent-tint` | 摄取规则改为 `.ingestion-page .planning-panel`，focus 改用 `--accent-weak` |

## 验证

修复后通过：

- `make generate-check`
- `cd backend && go test -p=1 ./internal/shootplanning/ingestion ./internal/platform/httpapi ./internal/shootplanning -count=1 -parallel=1`
- `cd frontend && npm run test:plan-ingestion`
- `cd frontend && npm run build`
- `cd frontend && npm run lint`
- `git diff --check`

现有 S8 DoD、scope gate 与 evidence pack 在修复前已通过；修复后的针对性命令同样通过。未执行 commit、stage、push、PR、merge、发布或部署。

## Closure 核验

本轮已把前一版 residual contract gap 收口并重新生成机器证据：

- OpenAPI transition enum 已收窄为 `abandoned`；generated Go/TypeScript contract 已重新生成且 `make generate-check` 通过。
- preview contract 新增 content/reference overrides 和 readiness link selections；后端在同一 session revision CAS 下持久化编辑、来源变化 acknowledgement、dropped restore 以及 explicit 0..N link selection。
- commit 现在拒绝未确认的 `source_changed` / `source_missing` / `needs_confirmation` 候选，并继续在 scoped core participant 中校验已有 shot/readiness server refs。
- 新增 `preview_overrides_test.go` 覆盖来源 ack、恢复 dropped candidate、link keep/discard 和 stale acknowledgement rejection。
- `make build` 已重新同步 embedded web UI；完整 `make check`、DoD runner、scope gate、evidence pack 均重新执行并通过。

非阻断 residual risks 保留如下，交由 QA 矩阵观察，不阻塞本轮 review：

- stage-1 evidence store query 仍需在多 execution-window revision fixture 中补充隔离证据；当前 projector 没有擅自批准 owner gate。
- PlanReady observation attribution 依赖同一 plan 只有一份 editing session 的生命周期约束；并发 terminal/history fixture 由 QA 补证。
- down migration 在 populated feature-specific idempotency rows 下的回滚语义继续遵循既有 migration policy，不删除业务事实。

## 审查轮次与下一步

这是第 1 轮，未超过 feature 的 3 轮上限。没有 commit、stage、push、PR、merge、发布或部署。review gate 现已完成，下一步进入 QA；QA 只覆盖上述 residual risks 和本轮新增 preview/override API 闭环，不重新启动 code review。

## 第 2 轮完整复审（qa-fix 后）

本轮为实质行为与 HTTP 契约变化后的完整独立复审，review round 为 2，未超过 feature 的 3 轮上限。环节 A 为独立 reviewer，环节 B 为 OCR；两者结果均已逐条与当前代码和真实 QA 失败证据核对。

### blocking

- [ ] REV-008 `backend/internal/shootplanning/ingestion/reparse.go:298-316`：重复 fingerprint 段的 reparse 会遍历同一 fingerprint 的全部旧候选，并用单一 fresh segment 定位替换，两个相同段可能互相覆盖，导致候选 ID、来源行和用户编辑串项。
  - 来源：independent-agent。
  - 影响：违反 D6/D7 与 A5 的重复段稳定身份，真实 reparse 后无法可靠恢复逐项编辑。
- [ ] REV-009 `backend/internal/shootplanning/ingestion/application.go:153-166`、`preview_overrides.go:19-50`：dropped candidate 恢复 ID 只在当前 fresh dropped snapshot 中查找；再次 preview/reparse 后旧 dropped ID 不再可识别，恢复候选无法持久化。
  - 来源：independent-agent；与 QA-005 的 dropped 恢复关注点一致。
  - 影响：违反 A5/A14 的 dropped 恢复闭环，用户可见恢复动作会在正式 preview 边界返回 400。
- [ ] REV-010 `backend/internal/shootplanning/ingestion/reparse.go:136-155`、`preview_overrides.go:120-150`：readiness link 的 source-change 状态没有独立 preview override/ack seam；重建 selection 时可把 changed link 静默重置为 current，取消关联又会被 commit 的 needs-confirmation 校验拒绝。
  - 来源：independent-agent。
  - 影响：违反 D7/D8 的逐项 ack 与 explicit discard 契约，来源变化可能被绕过或合法 discard 无法完成。
- [ ] REV-011 `backend/internal/shootplanning/ingestion/commit.go:307-365`：commit 只校验请求中出现的 decision，未强制覆盖 snapshot 全集合且未校验 decision kind；可遗漏候选形成隐式丢弃，或把 shot 放入 readiness decisions。
  - 来源：independent-agent。
  - 影响：违反设计 §2.2/line 291 的 missing decision=400，可能写入错误核心资源，削弱 atomic commit 的输入完整性。

### important

- [ ] REV-012 `backend/internal/shootplanning/ingestion/parser.go:345-350,394-398`：placeholder、over-limit、重复 unsupported URL 的 dropped candidate occurrence 仍固定，非 blank dropped 仍可能重复 ID。
  - 来源：independent-agent + OCR（OCR high，经本地核验）。
- [ ] REV-013 `backend/internal/shootplanning/ingestion/preview_overrides.go:15-47`：restore dropped append 可能触发 slice realloc，使 `contentByID` 的既有 candidate 指针失效；override 顺序不同会丢编辑。
  - 来源：independent-agent + OCR（OCR high，经本地核验）。
- [ ] REV-014 `backend/internal/shootplanning/ingestion/parser.go:332-340`：URL occurrence 仍从 0 开始，但 OpenAPI minimum=1。
  - 来源：independent-agent。
- [ ] REV-015 `frontend/src/planning/ShootPlanIngestionPage.tsx:167-175`、`backend/internal/shootplanning/ingestion/commit.go:354-365`：readiness link 使用人造 `readiness-link-*` client ID 并在 commit 中跳过 snapshot candidate 检查，可伪造 provenance/unknown link。
  - 来源：independent-agent。
- [ ] REV-016 `backend/internal/shootplanning/ingestion/commit.go:339-346`：reference decision 只校验 raw URL，target kind/ref/label 可绕过 preview persistence 直接改变。
  - 来源：independent-agent。
- [ ] REV-017 `backend/internal/shootplanning/ingestion/evidence.go:148-154`：stage-1 evidence query 未按 execution-window revision 过滤，仍保留为 QA residual risk；本轮 OCR high 命中但未升级为 blocking，因为 review round 1 已明确归因并交给 QA。
  - 来源：OCR + local，residual-risk，不阻塞本轮修复判定。

### nit / suggestion / learning / praise

- nit：HTTP preview DTO 已补公开 snake_case JSON tags；本轮新增 JSON 结构体测试仍建议后续补 handler-level strict decoder fixture。
- learning：preview/session 的 stable identity 必须同时覆盖内容候选、所有 dropped reason 与 readiness-link participant；只修 blank occurrence 会留下同类 ID 碰撞。
- praise：本轮真实浏览器 QA 暴露了 application 内部单测未覆盖的 strict JSON 边界，且修复后已重跑端到端 preview、reload、commit、terminal 与 revision 409 路径。

### Test And QA Focus

- 重复相同段落 reparse/reorder，编辑第一项后确认每个 candidate ID、source line 与 edit 不互串。
- placeholder、unsupported、over-limit、blank 多个 dropped 项逐项恢复，至少连续两次 preview 后仍能恢复并持久化。
- readiness link source changed / deselect / explicit discard 与 0/1/N selection 全部经过 ack 校验。
- commit adversarial：遗漏 snapshot candidate、跨 kind decision、伪造 readiness link ID、篡改 reference target/label 均应 400 且无 participant 写入。
- URL occurrence 与生成 schema、handler-level strict JSON、override 顺序（restore first）需有真实 HTTP 或 function fixture。

## 第 2 轮结论

- Status: changes-requested
- Next: 回 `cs-feat` implementation qa-fix，仅修 REV-008 至 REV-016；修复后进入第 3 轮完整 code review，再重跑 QA。REV-017 继续由 QA 作为 residual risk 观察，不扩大本轮范围。

## 第 3 轮完整复审（最终轮）

本轮是 qa-fix 后的最终完整独立复审，严格消耗本 feature 允许的第 3 轮 review 配额。环节 A 独立 Task reviewer 已完成；环节 B OCR 已启动但在返回 `invalid line range` 后持续无响应，主进程以退出码 130 中止，因此没有把 OCR 结果伪装成已完成。由于 review gate 有已启动但失败的 lane，本报告按协议标记 `blocked`；环节 A 的 findings 仍逐条记录如下。不得再开启第 4 轮。

### blocking

- [ ] R3-B01 `frontend/src/planning/ShootPlanIngestionPage.tsx:162-172`、`backend/internal/shootplanning/ingestion/commit.go:305-355,381-403`：正常的 1/N readiness link 提交使用不一致的 client ref。
  - Evidence：snapshot link 两端保存内容 candidate ID；前端 shot/readiness decision 的 `client_ref` 却分别加上 `shot-` / `readiness-` 前缀，link decision 仍发送原始 candidate ID；后端只在 `keptShots` / `keptReadiness` 中查找带前缀 ref，且原始 hash ID 不满足既有资源 fallback。
  - Impact：新建候选存在 1 或 N 条关联时，正常 preview→commit 主路径返回 400，A8 核心闭环不成立。
  - Expected fix scope：统一 content candidate ID 与 batch client ref 映射，并以真实 `buildCommitInput` 形状补充 preview→commit 集成测试。

- [ ] R3-B02 `frontend/src/planning/ShootPlanIngestionPage.tsx:130-136,169-172,187-199,268-272`、`backend/internal/shootplanning/ingestion/parser.go:232-275`：改类或丢弃关联候选后仍发送不可见的旧 link selection。
  - Evidence：`editCandidate` / `discardCandidate` 不清理 `readinessLinks`；关联端点随后从多选控件消失，但 `previewEdits` 继续发送旧 pair；builder 对改类端点报错，丢弃端点可能产生指向 discarded candidate 的 keep link。
  - Impact：用户无法取消残留关联，preview 或 commit 被卡住，违背改类、discard、取消选择均须显式处理的契约。
  - Expected fix scope：端点 kind/action 变化时同步生成 explicit discard，并让服务端拒绝对 discarded endpoint 新建 keep selection；覆盖两端改类和任一端 discard。

- [ ] R3-B03 `frontend/src/planning/ShootPlanIngestionPage.tsx:174-182,187-198,268-272`、`backend/internal/shootplanning/ingestion/preview_overrides.go:77-118`：reference source-change 没有前端 acknowledgement 入口。
  - Evidence：后端对 source_changed/source_missing reference 要求精确 acknowledgement revision；前端 reference 行只有 keep/discard checkbox，不能设置 ack，新 source change 只能回传空 ack。
  - Impact：URL 修改或含 URL 段删除后，用户无法通过正式 UI 确认 keep/discard，preview 永久返回 400。
  - Expected fix scope：为 reference 增加逐项来源变化确认动作并回传精确 revision，keep 与 discard 都覆盖。

- [ ] R3-B04 `backend/internal/shootplanning/ingestion/reparse.go:40-48,99-138`：删除含 reference 的 segment 时旧 reference 静默消失。
  - Evidence：`reconcileReferences` 只处理 LCS exact pair 与等长 gap pair；不等长 gap 中未配对的旧 segment 没有追加 source_missing reference 的逻辑，fresh snapshot 直接丢弃旧 reference。
  - Impact：用户编辑过或明确保留的 reference 会绕过 source_missing/needs_confirmation，违反 provenance 与可恢复性契约。
  - Expected fix scope：处理未配对旧 segment 的 reference；保留 identity 并标记 source_missing，只有未编辑且已 discard 的旧 reference 可被移除。

- [ ] R3-B05 `backend/internal/shootplanning/ingestion/application.go:50-89`、`parser.go:159-166`、`backend/internal/platform/httpapi/planning_ingestion.go:36-70`、`api/openapi.yaml:3984-3993`、`frontend/src/planning/ShootPlanIngestionPage.tsx:46-70,97-106,183-198`：staged asset intent 没进入 snapshot，也未形成完整 commit provenance。
  - Evidence：session 只保存 `staged_asset_intent_count`；前端传 assets 总数而非选中 IDs，刷新时 `selectedAssets` 不恢复；commit 临时构造 `asset-${asset.id}`，后端未按当前 snapshot 校验；create 仍要求非空 source text，asset-only 路径不能创建。
  - Impact：素材选择无法可靠中断恢复，资产可在未 preview 的情况下进入 commit，A6/A10/A14 及 asset-only 场景不成立。
  - Expected fix scope：把完整 asset intent 纳入 session snapshot/preview canonical，commit 精确校验全集与字段，create 改为 source 或至少一个 staged intent 二选一。

- [ ] R3-B06 `backend/internal/shootplanning/ingestion/reparse.go:21-34,49-55,71-92`：source-missing 重现逻辑可能覆盖已按等长 gap 配对的候选。
  - Evidence：等长 gap 已按 ordinal 配对但未写入 `newToOld`；后续 missingByFP 重现逻辑仍把同一 new segment 当未匹配项，可能再次替换 gap-paired identity，且队列未按 candidate ID ASC 稳定排序。
  - Impact：多轮消失→相同文本重现会串联用户编辑与 candidate identity，破坏 D7 配对优先级。
  - Expected fix scope：等长 gap pair 计入 paired new-index 集合后再执行 missing reattach，并稳定排序 missing queue；补至少三轮 reparse fixture。

### important

- [ ] R3-I01 `backend/internal/shootplanning/ingestion/commit.go:308-356`、`frontend/src/planning/ShootPlanIngestionPage.tsx:162-168`：Shot/Readiness commit payload 未与 preview snapshot 字段绑定，前端还丢失解析正文与 readiness 元数据。
  - Evidence：后端只比对 candidate ID/kind/action，不比对 title、normalized content、category、requirement、preflight 等写入字段；前端只提交 title。
  - Impact：API 可在 commit 阶段写入未经过 preview 的 payload，正常 UI 也丢失聊天解析正文。
- [ ] R3-I02 `backend/internal/shootplanning/ingestion/parser.go:340-350`：URL occurrence 按 digest 分组而非 paragraph 内出现顺序；不同 URL 同行时 occurrence 都为 1。
- [ ] R3-I03 `backend/internal/shootplanning/ingestion/preview_overrides.go:19-50`：恢复 dropped 后同一 candidate ID 同时存在于 content 与 dropped 集合，刷新后 UI 重复展示且候选全局 identity 不唯一。

### Test And QA Focus

- 必须优先验证真实前端 `buildCommitInput` 的 readiness 0/1/N preview→commit、改类与任一端 discard、以及 source-change reference keep/discard ack。
- 补充删除含已编辑 URL 的 segment、连续三次 reparse 的 gap/missing 配对、URL 混合 occurrence、dropped restore 两次 preview/刷新/commit、staged asset 选择恢复与 asset-only create。
- 补充 content payload 篡改、asset 注入/遗漏、伪造 link/ref candidate ID 的 400 与 0 participant 证据。

### Independent review / OCR lane

- 环节 A：`plan_ingestion_code_review_r3`，completed；以上 R3 findings 已由主 agent 对照当前源码与测试核验。
- 环节 B：`ocr` available，started；返回一条 `invalid line range` 警告后持续无响应，已中止（exit 130），无可合并 findings。
- Merge policy：未将 OCR 结论伪造成完成；因已启动 lane 失败，review gate 不能写 passed。

## 第 3 轮结论

- Status: blocked（同时存在 6 条独立 reviewer blocking finding；即使 OCR lane 恢复，也不能进入 QA）
- Next: owner checkpoint。依据用户已确认的最多 3 轮约束，不得再开启第 4 轮 review；需由 owner 决定是否接受当前 blocking/residual risk、另开后续 feature/issue，或明确授权在不新增 review 轮次的前提下处理。
