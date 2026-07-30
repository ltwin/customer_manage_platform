---
doc_type: feature-qa
feature: 2026-07-09-schedule-calendar
status: passed
tested: 2026-07-10
round: 1
---

# schedule-calendar QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-design.md`（`status: approved`）
- Checklist: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-checklist.yaml`（8/8 steps `done`；A1-A26 checks 仍由 acceptance 更新）
- Review: `.codestable/features/2026-07-09-schedule-calendar/schedule-calendar-review.md`（`status: passed`，round 4，无 unresolved blocking/important）
- Evidence pack: none（标准 feature 流程）
- Gate results: none（标准 feature 流程）
- DoD results: none（标准 feature 流程）
- Implementation evidence: `schedule-calendar-step-2-fix.md`、`schedule-calendar-step-3-fix.md`、`schedule-calendar-step-4-fix.md`、review-fix 对话证据、`evidence/*.png`
- Diff basis: 当前 `develop` 的 unstaged tracked diff + 未跟踪 schedule-calendar 文件；真实 staged diff 为空。QA 启动时当前 feature 源码没有晚于 round 4 review 的改动；QA 期间仅重新生成了字节一致的 Go/TS 契约产物，并新增本报告。
- Baseline dirty files: `.codestable/issues/2026-07-10-customer-profile-avatar-stretch/**`、`frontend/scripts/avatar-layout.test.mjs`，以及 `Makefile`、`frontend/package.json`、`frontend/src/index.css` 中明确归因 avatar-layout 的 hunks；这些内容不进入本 feature verdict。`test:avatar-layout` 只作为仓库 baseline 运行。
- Feature type: functional
- Core evidence gate: slot CRUD/隔离/状态矩阵、重叠与月历、订单/slot/backfill_order 幂等恢复、`customer_changed`、账号时区 fail-closed/retry、日历与客户档案共享排期、历史 backfill、深链、375px 与键盘路径必须有 unit/function/integration/API/browser 运行证据，不能只靠 lint/typecheck。
- Review basis check: review 明确以当前 `develop` 的 unstaged tracked diff 和未跟踪 schedule 文件为 basis；QA 启动时 staged 为空，且未发现 review 后新增的 feature 代码批次，因此 round 4 review 仍覆盖当前实现。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | design A1-A9、A13-A14 | core-functional | CRUD、半开区间、nullable PATCH、账号隔离、列表 union、一订单一 shoot、幂等原子性与并发 | integration/API | 目标 Go 测试、HTTP 测试、真实 API 同 key 重放 | 合法写入成功；非法/跨账号按契约失败；同 key 只生成一份资源 | pass |
| QA-002 | design A2；review REV-010/013 | core-functional | 拟修改区间与原 slot 区间完全不相交时，PATCH 遇到 `customer_changed` | browser/API/manual | 一次性 409 故障代理；编辑 2026-07-11 10:00-12:00 为 2026-07-20 09:00-10:00 | 用原区间找回事实；保留拟修改时间/note/order；清旧冲突并重拉候选/冲突 | pass |
| QA-003 | design A15-A16；review REV-002/003/005/009 | core-functional | order/slot/backfill_order 的 unknown、known-resource、`idempotency_conflict`、2h/24h 边界 | function/API | `test:schedule`、逐 phase inline assertions、真实 API 重放/异 body 冲突 | journal/key 不变，不 reset/新建第二份资源；24h 后仅人工放弃 | pass |
| QA-004 | design A11/A21；review REV-011 | core-functional | `/me` 首次失败、账号时区未知、显式重试；DST 与浏览器时区隔离 | browser/function/integration | 临时停止 Go server 后 reload，再恢复 server 并点击重试；timezone tests | `timezone=null` 时 fail-closed；无需整页刷新恢复 Asia/Shanghai 与 slot 查询；无浏览器时区兜底 | pass |
| QA-005 | design A10/A12/A21 | core-functional | 固定 42 格、密集日、真实重叠计数、前三条+更多、抽屉完整、10 秒查阅 | browser/function | 临时创建 5 条 hold/busy；浏览器打开 2026-07-27；calendar model tests | 5 条档期、4 条冲突，前三条+还有 2 条，抽屉 5 条，≤10 秒 | pass |
| QA-006 | design A17-A19 | core-functional | 新建咨询订单→shoot slot→刷新→scheduled；共享弹窗、候选/backfill 与收口 | browser/function/API | 单次连续浏览器全流程、backfill/helper tests、真实 backfill_order API 幂等 | 30 秒内可靠完成；slot 真实可见；backfill 不重复订单且时间/note 不丢 | pass |
| QA-007 | design A20；review REV-001/006/007 | core-functional | 非法 date、失效 slot、跨月有效 date+slot、malformed details | browser/function | 真实登录态导航 4 类 URL；typed-details helper tests | 不崩溃；失效目标有说明；有效目标切月并聚焦；畸形 details 安全降级 | pass |
| QA-008 | design A22-A23 | core-functional | 375px 查阅轻路径、无编辑工作台、无横向溢出；Escape/focus return | browser/manual | viewport 375x812、抽屉、Escape、普通点击打开/关闭 | 42 格、摘要完整、无可见创建/编辑/删除；Escape 关闭并回到日期按钮 | pass |
| QA-009 | design A24-A26 | supporting | 范围守护、schedule 域无 order 写、无 prototype store、DTO 同源 | diff/typecheck/lint | scoped rg、OpenAPI 生成、build/lint、路由测试 | 无组合端点/额外 feature；无 prototype import；类型来自 schema | pass |
| QA-010 | design 必跑命令；checklist CMD-001-CMD-005 | supporting | 构建、lint、全量/目标测试、生成物零漂移、YAML、diff 清洁度 | build/test/typecheck/diff | `make check`（隔离临时 index）、目标命令、byte compare、validate-yaml、diff checks | 全部 exit 0，无真实生成漂移 | pass |
| QA-011 | review REV-004/005、Test And QA Focus | core-functional | order 已知成功但 slot 未成功时不显示虚假“档期已保存”；known resource 本地收口失败不重复创建 | function/browser/diff | flow helper tests、真实完整创建、API 幂等、成功回调观察 | 只有 slot 完成且刷新后显示结果；known order/slot 只做恢复/本地收口 | pass |
| QA-012 | design A18/A19；review residual risk | core-functional | 两入口复用与客户/订单定位；status sync 分页 fallback | function/browser/build | 共享 dialog 的日历实跑、实现期客户页 UAT、候选/fallback helper tests、前端 build | 同一组件承担两入口；候选、深链与 status sync 定位稳定 | pass |

## 3. Command Results

- `make check`（真实 index，首次基线诊断）→ exit 2：build、lint、全量 Go tests、4+3+32+1 个前端测试均先通过；仅尾部 `generate-check` 把当前未提交的 feature 生成物与 `HEAD` 比较而报告 diff。该失败由 dirty-worktree/index 语义导致，不是重新生成后的内容漂移。
- `make generate` + 生成前副本逐字节 `cmp` → exit 0：Go `api.gen.go` 与 TS `schema.d.ts` 重新生成后字节一致；SHA-256 分别为 `69f9491ea9a238e06d3b0282a1fb777b0f429aa83d80077f580c883f220acc1b`、`9b8c44e949230fbc861a76ba472686861d65e00abfbc573caa9b683f58e328b3`。
- 隔离临时 Git index 预载当前两份生成物后执行 `make check` → exit 0：build、lint、全量 tests、`generate-check` 全部通过；真实 `.git/index` 未改动，最终 staged diff 仍为空。
- `cd backend && go test ./internal/platform/idempotency/... ./internal/schedule/... ./internal/order/... ./internal/platform/httpapi/... -count=1` → exit 0：四包 fresh 通过，分别约 2.5s / 7.9s / 17.0s / 17.9s。
- `go test ./internal/schedule -run 'TestScheduleCRUDRangeSummaryIsolationAndIdempotency|TestScheduleConcurrentCreatesKeepOneShootWithTypedConflict|TestScheduleUpdateRetriesAfterCustomerMerge|TestScheduleCreateReturnsCustomerChangedAfterTwoMoves|TestScheduleCreateRetriesAfterCustomerMergeAndRejectsArchive' -count=1 -timeout=2m -v` → exit 0：5/5 通过，约 8.5s。
- `go test ./internal/platform/idempotency -run TestExecuteCreateProtocol -count=1 -v` → exit 0：同 hash 串并发只写一次、异 hash 冲突、确定性失败清 claim、invalid success rollback、committed/rolled-back error 收敛、过期单 owner 接管共 7 个子场景通过。
- `go test ./internal/order -run 'TestCanScheduleAtUsesSharedFutureHistoryMatrix|TestCreationModesAndSchedulablePagination|TestDeleteOrderInUseReturnsTypedDetails|TestScheduleWritesAndOrderDeleteLinearizeWithoutDeadlock' -count=1 -v` → exit 0：未来/历史矩阵 10 个子场景、候选分页、typed details、三种锁序并发场景通过。
- `go test ./internal/platform/httpapi -run 'TestMeUsesInjectedAccountTimezoneProvider|TestScheduleAPIEndpointsIdempotencyNullableAndIsolation|TestScheduleAPIReturnsCustomerChangedAfterTwoMoves' -count=1 -v` → exit 0：时区 provider、HTTP CRUD/nullable/隔离/幂等、双迁移 `customer_changed` 通过。
- `cd frontend && npm run test:schedule && npm run build && npm run lint` → exit 0：schedule 32/32；TS/Vite build 通过；oxlint 通过。Vite 保留既有 `>500 kB` chunk warning，不影响本 feature 行为判定。
- `python3 .codestable/tools/validate-yaml.py --file ...items.yaml --yaml-only && python3 ... --file ...checklist.yaml --yaml-only && git diff --check` → exit 0：两份 YAML 有效，diff 无 whitespace error。
- `git diff --cached --check` → exit 0；`git diff --cached --stat` 为空。
- scoped cleanliness `rg`（`console.log/debug`、`debugger`、`TODO/FIXME/XXX`、prototype store/data import）→ 0 命中。
- 真实 API 幂等动作 → order / backfill_order / slot 均为：首次 201、同 key/body 重放 201 且同 id、同 key/异 body 409 `idempotency_conflict`、资源计数 1。
- 前端逐 phase CLI 断言 → exit 0：order / slot / backfill_order 均分类为 `idempotency_conflict`，attempt key 与 journal object 不变，`resume/reset/keep/compensate/delete/abandon` 在 24h 前全部 false；满 24h 自动 replay 抛错且只有 `abandon=true`。
- QA 临时数据清理检查 → exit 0：完整创建的 2 个 shoot slot/订单、密集日 5 个 slot、幂等 API 临时资源全部清理；最终 `qa_temp_slots=0`、`qa_temp_orders=0`。
- 最终本地服务检查：正常 Go server 仅监听 `:8080`，Vite 监听 `:5173`；一次性 409 proxy 与 `:18080` upstream 已停止。

## 4. Scenario Results

- [x] QA-001 后端 CRUD、隔离、矩阵、并发与 typed contract：pass
  - Evidence: fresh integration/HTTP tests 全绿；`TestScheduleConcurrentCreatesKeepOneShootWithTypedConflict` 证明并发最终 1 成功 + 1 typed 409；HTTP 测试覆盖 nullable PATCH 与跨账号 404。
  - Notes: Testcontainers fresh 运行未复现 review 记录的 `port 5432/tcp not found` 抖动。

- [x] QA-002 完全不相交区间的 `customer_changed`：pass
  - Evidence: 原 slot 为 `2026-07-11 10:00-12:00 Asia/Shanghai`；浏览器拟改为 `2026-07-20 09:00-10:00`，note=`QA customer_changed 保留拟修改备注`。一次性代理仅对 PATCH 返回 409。
  - Evidence: PATCH body 为 `2026-07-20T01:00:00Z`→`02:00:00Z`；随后恢复 GET 明确查询原事实区间 `2026-07-11T10:00:00+08:00`→`12:00:00+08:00`，而不是拟修改区间。
  - Evidence: 恢复后表单仍为 7 月 20 日 09:00-10:00、同一 note/known order；按钮从“保存修改”退回“检查冲突”，提示“订单客户已发生变化，已刷新客户与订单候选；请重新检查冲突后确认”。
  - Evidence: 候选以拟修改 end `2026-07-20T02:00:00Z` 重拉；拟修改冲突区间在提交前和恢复后人工重查各请求一次。数据库复核 slot 仍是原 7 月 11 日时间与原 note。

- [x] QA-003 三 phase 幂等冲突与 2h/24h 恢复：pass
  - Evidence: 真实 API 对 order、slot、backfill_order 分别产生 201→同 id 201→409 `idempotency_conflict`，每类最终都只有 1 份资源。
  - Evidence: phase-level CLI 断言证明 attempt key 与 journal object 不变、无 replay/reset/补偿/第二资源入口；24h 前不可 abandon，满 24h 不自动重放且只开放人工 abandon。
  - Evidence: schedule tests 证明 matching backfill pending 在 3h 时仍恢复 draft；无关 pending 不延长已过期普通 draft；known order 写 pending 后的本地重试零重复网络。

- [x] QA-004 账号时区 fail-closed/retry 与 DST：pass
  - Evidence: 临时停止 `:8080` 后 reload，页面显示 `账号时区加载失败：请求失败（502）`、`时间按 账号时区不可用`，新建/今天/当天加档期禁用，slot 不按伪日界渲染。
  - Evidence: 恢复 server 后只点击“重试账号时区”，未整页刷新；同一 URL 恢复 `时间按 Asia/Shanghai`、2 条 slot、无错误 alert。
  - Evidence: helper tests 覆盖 23/25 小时 DST 日、不存在时间拒绝、重复时间必须 occurrence，以及不依赖浏览器时区的账号日期转换。

- [x] QA-005 月历、冲突与密集日计时：pass
  - Evidence: 2026-07-27 临时 5 条 slot 中两组真实重叠；浏览器 `aria-label` 为“5 条档期，4 条冲突档期”，日格显示前三条 +“还有 2 条”，抽屉完整 5 条且每个冲突项有文字提示。
  - Evidence: 从导航到 DOM 可回答上述信息耗时 682ms，低于 10 秒；document client/scroll width 均为 1280，无横向溢出。
  - Evidence: `test:schedule` 覆盖周一首列、42 格、跨日半开投影、稳定排序与按自然日去重冲突；现有截图 `evidence/desktop-dense-day.png` 与 `evidence/mobile-375-dense-day.png` 可复核布局。

- [x] QA-006 新咨询订单到可靠排期、候选与 backfill：pass
  - Evidence: 浏览器连续执行“7 月 26 日→新建档期→新咨询订单→选 active 客户 test001→选在架套系测试001→带出标题/680 元→检查冲突→保存”，从开始到日历真实出现 scheduled shoot 共 21.493s；`新建档期` dialog 关闭，日期按钮变为 1 条档期，抽屉展示订单状态“定档”。
  - Evidence: 单次确认提交到 `order create → slot create → refresh → status sync → visible` 为 2.456s。
  - Evidence: 客户档案入口复用同一 `ScheduleSlotDialog` 的实现期 UAT 已在 checklist Step 7 完成；本轮对同一共享 dialog 的完整运行、固定客户接口编译与 build/lint 复核未发现分叉实现。
  - Evidence: schedule tests 覆盖 draft handoff、只允许历史六态、最终状态裁剪时间戳、pending→draft→clear、本地重试零重复网络、返回后恢复原时间/note/customer/order；真实 backfill_order API 幂等也已通过。

- [x] QA-007 深链与容错：pass
  - Evidence: `/calendar?date=2026-13-40` 保持可用，显示“链接中的日期无效，已返回当前月份”，回到 2026 年 7 月。
  - Evidence: `/calendar?date=2026-07-10&slot=missing-slot-qa` 显示“链接指向的档期已不存在”。
  - Evidence: 先到 2026 年 8 月，再导航到 `/calendar?date=2026-07-11&slot=slot_...`，月历切回 7 月，目标 `.slot-entry[data-slot-id]` 存在且 `document.activeElement` 正是该 entry。
  - Evidence: helper tests 证明 `order_in_use` 按账号时区生成 date+slot、跨月 known-slot link 正确、malformed details 只降级到 `/calendar`。

- [x] QA-008 375px 与键盘：pass
  - Evidence: 375x812 下 document client/scroll width 都为 375，无 main/grid/drawer 横向溢出；42 个日期按钮仍在，7 月 11 日 aria-label 可读“2 条档期，0 条冲突档期”，抽屉完整展示两条摘要。
  - Evidence: 移动端可见 workflow buttons 为 `[]`，即新建/当天加档期/编辑/删除均不��见；只有关闭与查阅路径。当前运行截图视觉上无文本重叠或撑破。
  - Evidence: 从日期按钮常规打开抽屉后按 Escape，`openDialogs=0`，焦点回到同一 `2026-07-11` 日期按钮；直接深链关闭也把 drawer 标为 `aria-hidden=true` 并移出视口。

- [x] QA-009 范围守护与生成类型：pass
  - Evidence: scoped cleanliness 查询无 prototype store/data import、debug/TODO；CalendarPage 与 API client 使用生成 schema 类型；OpenAPI→Go/TS 重新生成零字节漂移。
  - Evidence: route/domain tests 保持 reminder/dashboard/settings/export 未注册与 schedule 域不写 orders；无 `/schedule/book`、周视图、拖拽、周期重复或第三方日历依赖。

- [x] QA-010 必跑命令与清洁度：pass
  - Evidence: 隔离临时 index 的完整 `make check` exit 0；目标 Go/前端命令、YAML、diff checks 均 exit 0；真实 staged diff 始终为空。
  - Notes: 原始 `make check` 的唯一非零已通过 byte compare 和隔离 index 证明为未提交生成物相对 HEAD 的基线语义，不是代码生成漂移。

- [x] QA-011 known-resource、虚假成功与本地收口：pass
  - Evidence: tests 证明 `scheduleRequestResultKnown` 仅按持久事实判断、known order 的本地 persistence retry 不重复网络、known slot link 能定位。
  - Evidence: 浏览器完整创建只有 slot 已创建、刷新并完成 status sync 后才在日历出现成功结果；order phase 本身没有单独“档期已保存”观察信号。
  - Notes: review 已确认 status PATCH 5xx 保持 unknown，而 known order/slot 后的刷新/本地收口失败进入结果已知恢复；本轮 API 幂等与 browser full flow 未发现重复资源。

- [x] QA-012 两入口、订单定位与 status sync fallback：pass
  - Evidence: API client tests 验证 `schedulable_at` 与 Idempotency-Key；schedule tests 验证当前已排期订单在候选被排除时仍可见、先按刷新后的 customer 分页再稳定无过滤 fallback、后续非 cancelled 状态满足 sync。
  - Evidence: 浏览器 shoot 抽屉中的“查看订单”链接携带真实 customer/order id；深链焦点和订单列表分页定位逻辑已由 review-fix 与前端 build/lint 覆盖。

## 5. Findings

### failed

none

### blocked

none

### residual-risk

- `REV-012` 的小型 create-failure classifier 提取仍是后续可选维护性建议；当前 phase-level 运行断言、API 幂等和浏览器恢复均已覆盖行为，不构成验收阻塞。
- review 的 OCR CLI 因 avatar issue 与 feature 共用 tracked 文件、scope 歧义而跳过；独立 Task agent review 已通过，本轮 QA 又补充了 fresh integration/API/browser 证据，因此这只保留为审查流程层风险，不承载任何未运行的核心功能路径。
- Vite 仍提示单 chunk 大于 500 kB；当前 build 通过、10 秒/30 秒交互计时通过，属于后续包体优化候选，不是本 feature 功能失败。

## 6. Cleanliness

- Debug output: pass（scoped `console.log/debug`、`debugger` 查询 0 命中）
- Temporary TODO/FIXME/XXX: pass（scoped 查询 0 命中）
- Commented-out code: pass（feature diff/新增文件复核未见临时注释块）
- Unused imports / dead code from this feature: pass（Go `golangci-lint` 0 issues；前端 oxlint 通过；TS build 通过）
- Out-of-scope files: pass（avatar issue 明确作为 baseline 排除；QA 本轮只新增本报告，未改实现/design/checklist）
- Prototype stub import: pass（CalendarPage/schedule 生产路径 0 命中）
- Handwritten API DTO: pass（OpenAPI、Go codegen、`schema.d.ts` 同步且重新生成零漂移）
- Temporary runtime state: pass（一次性 proxy、`:18080` upstream 与全部 QA 测试数据已清理；正常 `:8080`/`:5173` 恢复）

## 7. Verdict

- Status: passed
- Rationale: A1-A26、review Test And QA Focus 与 residual risk 中可运行项均有 fresh 自动化/API/浏览器证据；核心 `idempotency_conflict`、`customer_changed`、timezone fail-closed/retry、backfill 恢复、深链、密集日、30 秒排期与 375px 路径均通过；没有 failed/blocked QA item。
- Next: `cs-feat-accept`
