---
doc_type: feature-qa
feature: 2026-07-31-calendar-v2-redesign
status: passed
runner_state: completed
runner_reason: "Round 1 历史失败已由 REV-022/REV-023 修复矩阵关闭；owner 明确终止追加 review，Round 2 复用完整 canonical gates、既有响应式/Settings 证据与最新真实 BrowserRouter/API 矩阵做一次性收口"
runner_id: "/root/calendar_v2_code_review_round1"
tested: 2026-08-01
round: 2
---

# Calendar v2 redesign QA 报告

## 1. Scope And Inputs

- Design：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
- Checklist：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-checklist.yaml`
- Review：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-review.md`（Round 8 `passed`，`reviewer: subagent`，blocking/important 均为 0）
- Evidence pack：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-evidence-pack.md`
- Gate results：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-gate-results.json`
- DoD results：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-dod-results.json`
- Implementation evidence：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-implementation.md`
- Diff basis：`feat/calendar-v2-redesign` 在 HEAD `3c28495dbcdc24d33ef4ade71f9baa154d0b51be` 之上的 Calendar v2 未暂存/未跟踪改动；44 个 tracked modified、11 个 untracked status paths；staged diff 为空。
- Baseline dirty files：当前独立 worktree 的 dirty scope 全部已由 scope gate 归因于 Calendar v2；主工作区 `feature/userCenter` 的开发改动不在本 QA 范围，且禁止触碰。
- Feature type：functional（同时改变持久化、公共 API、UI、路由、异步错误语义与真实写流程）。
- Core evidence gate：A1–A17、review §5/§6 的浏览器/runtime 风险、四条 DoD 命令、账号隔离、真实响应式 DOM/geometry/focus、deferred DELETE/latest timezone、Settings 保存失败/成功回填都必须有运行证据；不能只依赖 typecheck、纯 helper 或 source-contract regex。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | DoD / review | supporting | 当前 diff 的 build/lint/generated/schema/全仓回归 | build/lint/unit/integration/diff | `make generate-check`、frontend tests/build/lint、`make check`、diff checks | fresh canonical exit 0；staged 为空 | pending |
| QA-002 | A1 | core-functional | Settings 默认、合法整体 PATCH、缺/多 weekday、坏时间/阈值 | integration/API | Go tests + 真实 `/settings` GET/PATCH | 合法值 round-trip；非法 400 且旧值不变 | pending |
| QA-003 | A2 | core-functional | Settings/slot/Openings 的账号隔离，客户端无 `account_id` | integration/API/diff | 双账号 Go tests + request trace | 无跨账号数据 | pending |
| QA-004 | A3 | core-functional | shoot price/payment/shoot type 摘要；hold/busy union | integration/API/browser | schedule repository/HTTP tests + detail UI | 摘要完整、non-shoot 不泄漏 shoot 字段 | pending |
| QA-005 | A4 | core-functional | 周视图含全天、跨午夜、重叠、5+、短档期和 cancelled | function/browser/DOM | model tests + week DOM geometry/click | 时间比例与 visual lanes 正确；事件均可点 | pending |
| QA-006 | A5 | core-functional | 42 日月格、跨月/跨日、密集日/长标题/取消痕迹 | function/browser | model tests + month DOM/screenshot | 格高稳定、最多三条加 N、标记正确 | pending |
| QA-007 | A6 | core-functional | endpoint-touching、真实 overlap、cancelled、turnaround | function/API/browser | schedule tests + detail/POST trace | 领域 conflict 与纯展示 collision 分离；软提醒不阻止 | pending |
| QA-008 | A7 | core-functional | 14 日 openings、单日多区间、8/9 天、前 5 天文案、dirty draft | function/browser/clipboard | model tests + openings dialog | 短碎片剔除；编辑不被刷新覆盖；仅本机复制 | pending |
| QA-009 | A8 | core-functional | shoot/hold/busy/cancelled/重叠利用率与概览 | function/browser | model tests + overview DOM | 口径可手算且 ≤100% | pending |
| QA-010 | A9 | core-functional | desktop side panel / mobile bottom sheet、shoot 摘要、取消折叠 | browser/DOM | 1600/1280/375 | role/aria/focus/layout mode 与内容正确 | pending |
| QA-011 | A10 | core-functional | create/edit/delete、preview latest-range、journal/unknown/customer_changed | unit/browser/API | schedule regressions + mock API trace | 不重复 mutation；旧 preview 不保存新范围；成功重拉 | pending |
| QA-012 | A11 | core-functional | 375 月/周、选日、事件、FAB、编辑、删除、焦点返回 | browser/DOM | mobile workflow | 全部动作可达且弹层不越界 | pending |
| QA-013 | A12 | core-functional | Shanghai/New York 23/25h、Lord Howe 30m transition、跨午夜/全天 | function/browser/DOM | deterministic tests + timeline DOM | tick/geometry/offset 与事件投影一致 | pending |
| QA-014 | A13 | core-functional | loading/stale/error/retry、openings 独立失败、generation latest-wins | unit/browser/API | failure injection + request trace | 当前 range 才落 state；Settings 失败仍可 CRUD 且隐藏可约 | pending |
| QA-015 | A14 / REV-017～021 | core-functional | date/slot/draft 深链、404、Back/Forward replace、deferred DELETE 导航/range/timezone | function/browser/API | BrowserRouter + deferred fixture/trace | 旧 refresh 不覆盖 current；删除 URL/selection/notice/focus 保持 latest | fail |
| QA-016 | A15 | core-functional | 1600/1280/375、workspace width、scrollWidth、keyboard、dark/reduced-motion | browser/DOM/screenshot | viewport matrix | mode 由 content box 决定；页面无横向溢出；模态语义正确 | pending |
| QA-017 | A16 | core-functional | 非默认 Settings 与全量导出 parity、schema v2、v1 边界 | integration/API | dataexport repository/HTTP tests | availability 一致、账号隔离、schema_version=2 | pending |
| QA-018 | A17 | core-functional | 七天/null/非默认回显；全表单 freeze；失败保草稿；成功按响应 rehydrate | function/browser/API | settings tests + failure/override fixture | 不误改其他字段、不重复 submit、不丢草稿 | pending |
| QA-019 | 反向范围/清洁度 | supporting | 禁止 endpoint/原型依赖/第三方 calendar/debug/TODO/副产物 | diff/grep/status | scope gate + cleanliness scan | 无禁止项、无缓存/fixtures 遗留 | pending |

## 3. Command Results

- 独立 runner：schedule 53/53、settings 6/6、api-client 7/7、v1-hardening 9/9、两个 diff check 全部 exit 0；生产源 scope/cleanliness scan 通过。
- 最新 canonical `make check` / build / lint / generate 证据见 implementation ledger；QA-fix 改变 diff 后必须重跑相关门禁与 code review。

## 4. Scenario Results

- [x] QA-018 Settings 保存失败、pending freeze、成功响应回填：pass。
  - Evidence：`qa-evidence/settings-failed-save-dom.json`、`settings-pending-save-api.json`、`settings-success-rehydrate-dom.json`、`settings-success-rehydrate-api.json`。
  - 结果：失败 PATCH 保留 `Asia/Tokyo / 130 / 5` 草稿且服务端未写；pending 时完整可写表单与提交按钮冻结、仅 1 次 PATCH；释放后按服务端响应回填 `America/New_York / 140`，其余字段保留请求值。
- [ ] QA-015 TZ1→TZ2、Settings 先完成、DELETE 后完成：fail。
  - Evidence：`qa-evidence/timezone-delete-settings-first-failure.json`、`timezone-delete-settings-first-dom.json`。
  - 结果：旧 TZ1 refresh 没有覆盖当前 TZ2，slot 已删除，URL 与焦点均为 `2026-08-05`；但详情标题仍为 slot 在 TZ2 下的 `8 月 4 日`，URL `date` 与 selected detail 不同步。
  - 归因：本 feature 的删除完成编排只 replace search params / focus，没有在 timezone 变化导致 slot 本地日期改变后同步 selected date。

## 5. Findings

### failed

- [ ] QA-015 deferred DELETE + latest timezone 完成后的 URL/selection 同步。
  - Evidence：`TRACE-TZ-DELETE-SETTINGS-FIRST-001`、`DOM-TZ-DELETE-SETTINGS-FIRST-001`。
  - Impact：违反 A14 的 anchor/selected 同步及 Round 7 要求的最终 URL/date/focus 一致；用户看到地址与聚焦日期为 8 月 5 日，但详情仍显示 8 月 4 日。
  - Expected fix scope：仅收敛删除当前 deep-link slot 后的 selected date/month 同步；不改变公共 API、range identity、删除语义或 Settings 契约。

### blocked

none（QA 执行中）。

### residual-risk

- pending；功能性核心路径不得在缺少实际运行证据时降级到 residual-risk。

## 6. Cleanliness

- Debug output：pending
- Temporary TODO/FIXME/XXX：pending
- Commented-out code：pending
- Unused imports / dead code from this feature：pending
- Out-of-scope files：pending

## 7. Verdict

- Status：failed
- Next：`cs-feat` implementation qa-fix → `cs-code-review` → 重新执行 `cs-feat` QA；未复审和复测前不得进入 acceptance。

## 8. QA Round 2 一次性收口（最终状态）

Owner 于 2026-08-01 明确要求停止持续 review。Round 11 reviewer 已中止，且不会再启动新的 review 轮次；`approval-report.md#code-review-local-only` 只授权本地质量收口，不等同于 Goal acceptance authorization。本节保留上方 Round 1 失败历史，并以当前 diff 的 fresh gates 与修复后证据给出最终 QA verdict。

### Final Verification Matrix

| ID | 当前证据 | 结果 |
|---|---|---|
| QA-001 | fresh `make generate-check`、`make check`、frontend lint/build、两个 diff check；schedule 55/55、settings 6/6、api-client 7/7、v1-hardening 9/9 | pass |
| QA-002 | `make check` 覆盖 Settings 默认值、strict weekly decode、合法整体 PATCH、坏时间/阈值、迁移 backfill/rollback 与 corruption fail-closed 的 Go tests | pass |
| QA-003 | `make check` 覆盖 AccountScope read/write isolation、Postgres 双账号隔离；客户端请求不携带 `account_id` | pass |
| QA-004 | schedule repository/HTTP tests 覆盖 shoot price/payment/type 摘要、hold/busy union、批量装配与稳定排序 | pass |
| QA-005 | schedule 55/55 覆盖周投影、全天/跨日、重叠、cancelled display lanes、5/15 分钟 endpoint-touching；1600/1280/375 DOM/PNG 证据覆盖真实 UI | pass |
| QA-006 | 42 日 month grid、cross-week/all-day、密集日/移动月视图由 schedule tests、`calendar-month-375-dom.json` 与 PNG 覆盖 | pass |
| QA-007 | active/cancelled occupancy、endpoint-touching、turnaround 与 POST overlaps 排除 cancelled 由前后端 tests 覆盖 | pass |
| QA-008 | distinct-date 8/9 边界、前 5 天文案、dirty draft 与 clipboard-only 由 schedule tests、Openings DOM/请求证据覆盖 | pass |
| QA-009 | shoot/hold/conflict/open-day/utilization 口径由 deterministic overview test 与浏览器概览 DOM 覆盖，利用率保持 ≤100% | pass |
| QA-010 | 1600/1280/375 evidence 记录 workspace/content layout、side panel/bottom sheet、shoot 摘要与取消折叠 | pass |
| QA-011 | conflict preview generation、mutation lock/journal/unknown recovery、移动 delete pending/success API+DOM 与 exactly-once DELETE 证据 | pass |
| QA-012 | 375 月/周、选日、事件、FAB、编辑/删除可达与稳定 focus 由 mobile DOM/PNG、delete evidence、v1-hardening 9/9 覆盖 | pass |
| QA-013 | New York 23/25h、Lord Howe 30 分钟 transition、fold/gap、跨午夜/全天、mapped reversal fail-closed 由 schedule 55/55 覆盖 | pass |
| QA-014 | settings failure degradation、latest generation、openings 独立失败，以及 non-settling/401 refresh stall 下 CRUD liveness 由 tests 与 REV-023 browser matrix 覆盖 | pass |
| QA-015 | Round 1 失败保留；REV-022 五组 finite-order matrix 与 REV-023 non-settling/late-TZ2/navigation matrix 证明 URL/date/month/focus/range canonical 一致 | pass |
| QA-016 | 1600/1280/375 DOM/PNG、keyboard/focus contracts、dark theme 与 reduced-motion CSS media contract；无 Calendar workspace 横向溢出 | pass |
| QA-017 | dataexport repository/service/route tests 覆盖 non-default availability parity、schema v2、length-derived counts 与账号隔离 | pass |
| QA-018 | settings 6/6 + failed/pending/success DOM/API evidence 证明七天/null/非默认回显、全表单 freeze、失败保草稿、成功按响应 rehydrate | pass |
| QA-019 | scope gate passed；无 debug/TODO/FIXME/注释死代码/方案外文件；Python cache 已清理，staged 为空 | pass |

### Fresh Command Results

- `make generate-check` → exit 0；Go/TS 生成物无漂移。
- `make check` → exit 0；frontend build/lint、Go build、golangci-lint 0 issues、全部 Go 包串行测试、全部前端专项、auth legacy/security、v1 ops 与 generate-check 全部通过。仅有既有 Vite 大 chunk warning。
- `npm run test:schedule` → 55/55；新增可执行协调用例覆盖 `idle | timeout | cancelled` 和迟到时区 latest-search guard。
- `git diff --check` / `git diff --cached --check` → exit 0；staged diff 为空。
- 浏览器：`qa-evidence/rev-022-delete-canonical-browser-matrix.json` → pass；`qa-evidence/rev-023-delete-settings-liveness-browser-matrix.json` → 3/3 pass。

### Final Findings

#### failed

none。Round 1 QA-015 已由 REV-022/REV-023 的修复与真实浏览器矩阵关闭。

#### blocked

none。Owner 明确批准停止追加 review 并做 local-only closure；该决定已命名落盘，不伪造 Round 11 独立 verdict。

#### residual-risk

- OCR 因 API key 当日额度耗尽返回 HTTP 402，未启动；它是可选行级通道，不影响已经完成的编译、测试和浏览器功能证据。
- 当前 mounted async regression 以可审计 fixture JSON/DOM 证据存在，而不是仓库内一键 e2e runner；核心纯协调 seam 已转为可执行 Node test，未来仍可把 fixture 封装成正式 e2e。
- Month/Week grid 的完整 ARIA `row` hierarchy 是既有非阻塞可访问性建议；不影响本次已验证的键盘、focus、role/aria-modal 与移动可达性。

### Final Cleanliness

- Debug output：pass。
- Temporary TODO/FIXME/XXX：pass。
- Commented-out code：pass。
- Unused imports / dead code from this feature：pass（lint 通过）。
- Out-of-scope files：pass（scope gate 通过；临时 QA fixture 不进入仓库并将在收尾删除）。
- Machine artifacts：pass（`scripts/lib/__pycache__` 已清理；最终清理继续删除 ignored build outputs 并保留 webui `.gitkeep`）。

### Final Verdict

- Status：passed。
- Next：停止在 Goal acceptance 独立授权 checkpoint；当前 design approval、旧 parent Goal authorization 与本次 review-cap 指令都不等同于 acceptance authorization。
