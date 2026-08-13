---
doc_type: feature-qa
feature: 2026-08-05-plan-ingestion-capture
status: failed
runner_state: completed
runner_reason: ""
runner_id: "local-browser-qa"
tested: 2026-08-07
round: 1
---

# plan-ingestion-capture QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-design.md`
- Checklist: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-checklist.yaml`
- Review: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-review.md`
- Evidence pack: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-evidence-pack.md`
- Gate results: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-gate-results.json`
- DoD results: `.codestable/features/2026-08-05-plan-ingestion-capture/plan-ingestion-capture-dod-results.json`
- Diff basis: `feat/creative-shoot-planning` 未提交工作区；review round 1 后尚无额外代码改动，S1-S8 均为 `done`。
- Baseline dirty files: 本 feature 的实现、生成物与证据文件均未提交；没有把其他 feature 的既有改动归入本轮 QA。
- Feature type: functional
- Core evidence gate: 真实账号上下文中的 `workspace -> create ingestion -> parse -> edit/ack/link -> preview persistence -> reload -> atomic commit -> terminal read-only`，以及 discard/restore、stale recovery、abandon 与响应式/键盘路径。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | design A14-A15 / review closure | core-functional | 真实账号进入拍摄策划工作台并创建 ingestion session | browser + API | `http://127.0.0.1:5173/shoot-plans` -> 工作台 -> 从聊天整理 | 会话创建成功并进入三步摄取页 | pass |
| QA-002 | design A1-A5 / A14 | core-functional | 粘贴原文、URL 与空行后解析候选 | browser + API | 输入聊天文本并执行首次解析与 reparse | 内容、参考链接、来源行与 dropped 均可见 | pass |
| QA-003 | review REV-004/005/006 | core-functional | 编辑标题/类型、discard 后恢复、0/1/N readiness links | browser | 编辑标题、候选改为准备项、取消/恢复保留、multi-select 依次选择 0/1/3 个镜头 | 编辑状态稳定且允许继续 | pass（本地状态） |
| QA-004 | review closure | core-functional | source missing/change acknowledgement 与 preview persistence | browser + API | 点击“确认来源变化”后“查看保存摘要” | preview 200，revision 前进，刷新后恢复编辑 | fail |
| QA-005 | design D5/D7 / A14 | core-functional | 多个 blank dropped candidate 的唯一身份与恢复路径 | browser + console + DB snapshot | 两个空行段产生两个 dropped 项 | 每项 candidate ID 唯一且 React key 稳定 | fail |
| QA-006 | design A8-A12 | core-functional | atomic commit 与 committed terminal read-only | browser + API | preview persistence 后确认保存并刷新 | 所有 participant 原子成功，session terminal 只读 | blocked by QA-004 |
| QA-007 | design A6-A7 | supporting | stale revision recovery、abandon | browser + API | 并发旧 revision 与结束本次摄取 | 旧 revision 409；abandon terminal 只读 | blocked by QA-004 |
| QA-008 | design A14-A15 | supporting | 1280px、430px、窄视口、键盘 focus-visible | browser/manual | viewport override 与键盘导航 | 无横向溢出，focus 可见，多选可操作 | blocked：核心路径先失败，本轮未继续低收益视觉覆盖 |
| QA-009 | review residual risk | non-functional | stage-1 execution-window 隔离、observation attribution、populated rollback | integration/diff | 复用 review、evidence pack 与既有测试证据 | 不越权批准 gate；风险被明确保留 | residual-risk |
| QA-010 | DoD | supporting | 构建、契约漂移、全仓检查与目标测试 | command | `make build`、`make generate-check`、`make check`、目标 Go/前端测试、lint、`git diff --check` | exit 0 | pass（review 后证据） |

## 3. Command Results

- `make build` -> exit 0：前端构建与 embedded web UI 同步通过。
- `make generate-check` -> exit 0：OpenAPI 生成物无漂移。
- `make check` -> exit 0：全仓构建、lint、测试与契约检查通过。
- `cd backend && go test -p=1 ./internal/shootplanning/ingestion ./internal/platform/httpapi ./internal/shootplanning -count=1 -parallel=1` -> exit 0。
- `cd frontend && npm run test:plan-ingestion` -> exit 0。
- `cd frontend && npm run build` -> exit 0。
- `cd frontend && npm run lint` -> exit 0。
- `git diff --check` -> exit 0。
- DoD runner：`CMD-001` 至 `CMD-006` 均通过；scope gate 与 evidence pack 均为 `passed`。
- 浏览器运行环境：本地 PostgreSQL、Go API `:8080`、Vite `127.0.0.1:5173`；`/api/v1/me`、plan GET、asset list、session create 与首次 preview 均返回 2xx。
- 未运行 atomic commit、stale、abandon、terminal 与完整 viewport 矩阵：preview persistence 已暴露核心实现失败，继续这些路径不能补足通过判据。

## 4. Scenario Results

- [x] QA-001 真实账号与摄取会话创建：pass
  - Evidence: 进入 `S8摄取闭环` 工作台，点击“从聊天整理”；`POST /api/v1/shoot-plans/{plan}/ingestion-sessions` 返回 201，生成 session `ing_26cfe2c1-8e4a-4f10-a53c-c70d405ba37f`。
  - Notes: 浏览器已有有效本地登录态，`/api/v1/me` 返回 200；本轮未重新输入密码。
- [x] QA-002 parse/reparse：pass
  - Evidence: 首次解析返回内容候选与 `https://example.com/rare-character` 参考链接；加入空行后 reparse 返回 4 个内容候选、1 个参考链接与 2 个 blank dropped 项。
  - Notes: source missing 候选正确显示“确认来源变化”。
- [x] QA-003 编辑、discard/恢复与 0/1/N link：pass（本地状态）
  - Evidence: 标题可改为“星夜祭司·回眸”；候选可取消保留并重新勾选；“蓝色假发与银色法杖”可改为准备项；关联多选的实际 selected values 依次为 `[]`、1 个 shot ID、3 个 shot ID。
  - Notes: 这一项只证明浏览器状态；服务端持久化由 QA-004 单独判定。
- [ ] QA-004 preview persistence 与来源确认：fail
  - Evidence: 点击“确认来源变化”并提交完整 content/reference/link overrides 后，`POST .../preview` 返回 400，页面显示“请求参数不合法”，session revision 仍为 2。
  - Notes: `ContentCandidateOverride` 与 `ReferenceLinkCandidateOverride` 的 Go 字段缺少 snake_case JSON tags；strict JSON decoder 将前端的 `candidate_id` 等视为未知字段，导致公开 UI 契约无法进入 application persistence seam。现有直接调用 `applyPreviewOverrides` 的 Go 测试绕过了 HTTP JSON 边界，未覆盖该失败。
- [ ] QA-005 dropped 唯一身份：fail
  - Evidence: DB snapshot 中来源行 3 与 5 的 blank dropped 项具有相同 `candidate_id=YhHc_joEcyODGSGirop_YGMSoJMoB8LWS4vUkTW4h2s`；浏览器控制台连续报告 duplicate React key。
  - Notes: `blankDrops` 对每个相同空白段固定使用 occurrence `0`，无法区分多个 blank run；这破坏逐项恢复身份与 React 列表稳定性。
- [ ] QA-006 atomic commit 与 terminal：blocked by QA-004
- [ ] QA-007 stale、abandon：blocked by QA-004
- [ ] QA-008 响应式与键盘矩阵：blocked by core implementation failure
- [x] QA-009 residual risks：residual-risk
  - Evidence: review 与 evidence pack 已记录 stage-1 query 未按 execution-window revision 过滤、PlanReady attribution 依赖每 plan 单 editing session、populated down migration 语义三个边界。
  - Notes: 这些风险不替代 QA-004/005 的核心失败，也不作为通过理由。

## 5. Findings

### failed

- [ ] QA-004 HTTP preview override DTO 与 OpenAPI/前端 JSON 契约不一致
  - Evidence: 真实 UI 请求 400；后端 `ContentCandidateOverride` / `ReferenceLinkCandidateOverride` 缺少 `json:"..."` tags，而 handler 使用 strict JSON decode。
  - Impact: 任何候选标题/类型/keep-discard、来源 acknowledgement、reference override 或 readiness 选择都无法通过正式 UI 持久化；因此 atomic commit 主路径不可达。
  - Expected fix scope: 只收口 preview override HTTP JSON 边界，并增加经过 handler 的请求测试，禁止只测 application 内部 DTO。
- [ ] QA-005 多个 blank dropped candidate 生成重复 ID
  - Evidence: 同一 session 的两个 blank dropped 项 candidate ID 相同，React 报 duplicate key。
  - Impact: dropped 项无法稳定逐项标识/恢复，可能出现重复、遗漏或恢复错项。
  - Expected fix scope: blank dropped ID 纳入稳定 occurrence，并增加两个及以上 blank run 的 parser/UI fixture。

### blocked

- [ ] QA-006/007/008：由 QA-004 的核心路径失败阻塞；不是环境不可用。

### residual-risk

- stage-1 evidence store query 尚无多 execution-window revision 隔离证据。
- PlanReady observation attribution 仍依赖同一 plan 同时最多一份 editing session 的生命周期约束。
- populated feature-specific idempotency rows 的 down migration 语义继续遵循现有 migration policy；不能通过删除业务事实伪造回滚成功。

## 6. Cleanliness

- Debug output: pass（未发现 feature 引入的生产 debug 输出；浏览器 duplicate-key error 属 QA-005 产品缺陷）
- Temporary TODO/FIXME/XXX: pass
- Commented-out code: pass
- Unused imports / dead code from this feature: pass（lint 通过）
- Out-of-scope files: pass（scope gate passed）

## 7. Verdict

- Status: failed
- Next: `cs-feat` implementation 阶段 qa-fix（仅 QA-004/005）-> `cs-code-review` 第 2 轮 -> `cs-feat` QA 第 2 轮。修复前不得进入 acceptance。
