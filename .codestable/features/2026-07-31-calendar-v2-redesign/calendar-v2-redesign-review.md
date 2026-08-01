---
doc_type: feature-review
feature: 2026-07-31-calendar-v2-redesign
status: passed
reviewer: self
reviewed: 2026-08-01
round: 11
lane_a_state: skipped
lane_a_ref: "/root/calendar_v2_code_review_round1"
lane_a_reason: "Owner 于 2026-08-01 明确要求终止反复 review；Round 11 已中止且不产出 verdict。按 approval-report.md#code-review-local-only，以 Round 10 独立 finding、REV-023 修复的可执行测试、真实浏览器矩阵和完整 canonical gates 做本地收口"
lane_b_state: unavailable
lane_b_ref: ""
lane_b_reason: "Round 11 preflight: ocr CLI 已安装，但 ocr llm test 返回 HTTP 402（配置的 LLM API key 今日额度已耗尽）；未启动 review、无 run ref，按协议 not-available，不阻塞 lane A"
---

# Calendar v2 redesign 代码审查报告

## 1. Scope And Inputs

- Design：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
- Checklist：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-checklist.yaml`
- Evidence pack：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-evidence-pack.md`
- Gate results：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-gate-results.json`
- DoD results：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-dod-results.json`
- Implementation evidence：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-implementation.md`
- Diff basis：`feat/calendar-v2-redesign` 在 `3c28495dbcdc24d33ef4ade71f9baa154d0b51be` 之上的未暂存、未跟踪 Calendar v2 改动；44 个 tracked modified、11 个 untracked status paths，tracked diff 2240 insertions / 418 deletions；无 staged diff。
- Review mode：full-rereview，round 8；Round 7 的 material review-fix 已完成定向和 canonical 全量门禁，并重新走完整独立审查。
- Worktree boundary：只审查独立 worktree；主工作区未写入、未切分支、未纳入 Calendar diff。

### Independent Review

- 环节 A：独立只读 Task reviewer `/root/calendar_v2_code_review_round1`，round 8 completed，verdict `passed`；blocking 0、important 0、nit 1、suggestion 1。
- 环节 B：Round 9 OCR preflight 执行 `ocr llm test` 返回 HTTP 402（配置的 API key 当日额度耗尽）；review 未启动、无 run ref，按协议记为 `unavailable/not-available`，不阻塞环节 A gate。
- Merge policy：以当前 worktree 代码、批准 design、可执行反例和实际测试为事实基础；没有把不可用的 OCR 通道伪记为 completed。
- 零写入确认：Task reviewer 的开始/结束 `git status --short` 一致；未编辑文件、生成缓存/构建产物、stage、commit、切分支、push 或触碰主工作区。

## 2. Diff Summary

- 新增：Settings availability 领域/迁移/测试、Calendar v2 页面子模块与样式、Settings editor seam、冲突预览 request seam、feature gate/evidence 产物。
- 修改：requirements/roadmap、OpenAPI 与生成物、Settings/schedule/dataexport 后端、Calendar/Settings 页面、ScheduleSlotDialog、前端 API/测试/Makefile。
- 删除：none。
- 风险热点：异步写锁、URL 深链、DST 周布局、空档日级截断、取消语义、Settings PATCH 草稿、转场可见性、响应式 focus。

## 3. Adversarial Pass

- 生产 bug 假设：Round 2 修复了普通日公共轴和非整小时 DST terminal tick，但中间刻度仍可能丢失分钟；短档期的 CSS 最小高度可能破坏半开区间几何；删除成功后 URL 深链可能仍指向已删除资源。
- 主动验证反例：Australia/Lord_Howe spring/fall 日的 120/180/240 elapsed-minute 刻度；同 lane 两个端点相接的 15 分钟档期；从带 `date+slot` 深链删除当前 slot 后立即刷新或复制 URL。
- 结果：真实中间刻度被截为 `HH:00`、22px 最小高度令不冲突短档期发生视觉/点击重叠、删除当前深链目标后残留失效 `?slot=`，均进入 Round 3 important findings。

## 4. Findings

### blocking

none。

### Round 10 important

#### REV-023：Settings GET 永不 settle 会永久锁住已在服务端成功的 DELETE

- `confirmDelete()` 已完成 `DELETE` 后，`refreshCalendarAfterDelete()` 只要发现 Settings request active，就无界等待 `waitForSettingsIdle()`；waiter 没有 timeout、取消或卸载 fail-safe。
- 等待期间删除弹窗保持 `deleting`，关闭与两个按钮均被禁用；普通 fetch/401 refresh 链路没有隐含 deadline，因此服务端资源已删除但 UI 可永久显示“正在删除…”。
- 这违反 A13 和关键决策 10：Settings 失败或停滞不得阻塞 Calendar CRUD。现有五组 browser matrix 都会最终 release Settings，未覆盖 non-settling transport。
- 修复边界：mutation success finalization 必须有界且可取消；超时后不得发送旧时区 range，迟到 Settings 只能基于 latest pathname/search context 安全重做 canonical reconcile；导航/卸载必须 discard waiter，DELETE 保持 exactly-once。
- Required regression：Settings 永不 settle + DELETE 204 时 bounded 解锁；随后 release TZ2 时 URL/date/month/focus/range 一致、无旧 TZ1 GET、无 deleted-slot lookup；另核对 navigation/unmount/401 refresh stall。

### Round 8 important

none。REV-021 及 REV-014～REV-020 均经完整复审确认关闭；更早已关闭 finding 未发现重开。

### Round 8 nit

- `frontend/src/pages/SettingsPage.tsx:75-94` 的 Settings GET 没有 AbortController、generation 或 latest-response guard。当前生产触发路径没有普通并发 retry，故不阻塞本轮；StrictMode/dev 双 effect 或未来新增 refresh 入口时可再统一到 latest-request 模式。

### Round 8 suggestion

- `MonthCalendar` 与 `WeekCalendar` 已提供 `role="grid"` 和键盘行为，但没有完整 `row` hierarchy；可在后续可访问性专项中补齐并用真实读屏环境验证，不作为本轮 QA blocker。

### Round 8 material closure

- REV-021：`latestAccountTimezoneRef` 在已 commit 的 effective timezone、Settings 开始时 shell fallback、Settings 成功结果落 state 前三个边界同步；DELETE 完成时使用 latest ref 与 latest URL 派生 current range。Settings→DELETE、DELETE→Settings 与同一 event-loop turn 的顺序下，旧 TZ1 refresh 均不能覆盖当前 TZ2 window。
- REV-019～REV-020：old refresh 资格仍同时要求当前 pathname 为 `/calendar`、完成时 slot 与 deleted slot 相同、完成时 effective range 与 mutation-start range 相同；不满足时只请求 current-render reload 或不触发 Calendar refresh。
- REV-017～REV-018：删除仍读取完成时 URL 并以 replace 清理匹配 slot；cancelled item 只参与最终可见集合的展示 collision，不进入领域 occupancy/conflict/openings/utilization/turnaround。
- REV-014～REV-015：非整小时 DST tick 保留真实本地分钟；短 endpoint-touching item 使用视觉 lane，且不转化为领域冲突。
- Round 8 只读定向门禁：schedule 53/53、settings 6/6、api-client 7/7、v1-hardening 9/9、`git diff --check` 与 `git diff --cached --check` 全部通过；fresh build/lint/generate/canonical `make check` exit 0 由 implementation ledger 提供。

### Round 9 important

#### REV-022：删除完成的 selected date 与 replace URL / range identity 仍不是同一 canonical state

- QA Round 1 qa-fix 用 `calendarDateAfterDelete()` 单独派生 `nextDate`，但 `calendarSearchParamsAfterDelete()` 仍只删除 `slot`，随后把未规范化的 `nextSearchParams` 传给 `setSearchParams(..., { replace: true })`。
- `?slot=A` 删除后地址为空，UI/month/focus 却保留 A 在 latest timezone 下的本地日期；`?date=not-a-date&slot=A` 则保留非法 date，但 UI 使用修正日期。reload、Back/Forward 无法复现可见 selection，违反 A14。
- 缺/非法 date 时 `calendarRangeKeyForSearch()` 又回落到 `mainRequestRef.current.rangeKey`；Settings TZ2 与 DELETE 同一 turn 完成、React 尚未 commit 时，旧 TZ1 range 仍可能误判相等，部分重开 REV-021。
- 现有纯测试只断言派生日期，source contract 只证明 setter 存在；真实 browser pass 只覆盖合法 `date+slot`，未覆盖 slot-only/非法 date。
- 修复边界：完成时生成同一个 canonical `{searchParams, date, rangeKey}`，缺/非法 date 时把 latest-timezone effective date 写回 URL，并用该 effective date + latest timezone 派生 current range；浏览器/API 覆盖 slot-only、非法 date、两种 release order 与同 turn completion。

### Round 7 important

#### REV-021：完成时 current range 仍使用 mutation-start 捕获的 timezone

- `confirmDelete()` 的 expected range 使用发起时 timezone 正确，但完成时 `readLocation` 也使用旧 closure 的 `accountTimezone`；valid date 会让 `calendarRangeKeyForSearch()` 完全忽略 current request fallback。
- Settings 初读失败时以 shell TZ1 允许 CRUD；pending DELETE 期间 retry 可返回另一 tab 已更新的服务端 TZ2，并启动 current TZ2 range load。迟到 DELETE 仍用 TZ1 算出 expected key，误放行旧 TZ1 refresh，覆盖 TZ2 window。
- 同一 date 在 Shanghai/New York 的 42 日 UTC window 不同；违反 A12/A13/A14。
- 修复边界：完成时 URL range 必须以 latest account timezone 派生。latest ref 除 render 同步外，还要在 Settings result 落 state 前同步更新，以覆盖两个 promise 同 tick 的竞态；补 same date+slot、TZ1→TZ2 回归。

### Round 6 important

#### REV-020：same-slot 跨 range 导航仍可能调用旧 refresh

- `reconcileCalendarDeleteRefresh()` 只比较 pathname 与 slot ID，没有比较请求发起 render 的 effective range identity。
- M1 `date=d1&slot=A` DELETE pending 时导航到 M2 `date=d2&slot=A`：slot 仍为 A，seam 误判安全并调用旧 M1 refresh，后者会中止/覆盖当前 M2 request，形成 M2 URL/month + M1 slots。
- `queryDate` effect 会切换 M2，而 querySlot 未变化时 lookup effect 不重跑；违反 A13/A14。
- 修复边界：把发起时 expected range key 与完成时从最新 date/timezone 派生的 current range key 一并纳入 old-refresh 资格；range 不同只请求 current render reload，同时仍可清理完成时匹配的 deleted slot URL；补 A/d1→A/d2 回归。

### Round 5 important

#### REV-019：deferred DELETE 仍会从旧 render 发起旧 range refresh

- `confirmDelete()` 在 mutation 成功后先调用请求发起 render 捕获的 `refreshCalendarData()`，之后才读取完成时最新 URL；该 refresh 又捕获旧 `loadSlots/range`。
- M1 DELETE pending 时 Back 到 M2：M2 已启动/完成 current load，迟到的旧 M1 refresh 会中止 M2 request、把 M1 登记成更高 generation 并最终把 M1 slots 落到当前 M2 页面。
- 最新 URL guard 只保护 URL/notice/focus，无法保护此前已启动的旧 server-state refresh；违反 A13/A14。
- 修复边界：mutation 完成时先判最新 pathname/search。仍处于原 deleted-slot Calendar 上下文才允许 await 原 refresh；已导航到另一个 Calendar 上下文则只触发 current render 的 reload tick，离开 Calendar 则不发起旧 refresh。refresh 后再次核验最新 URL，再决定 replace、notice 与 focus；补 deferred 双范围编排测试。

### Round 4 important

#### REV-017：删除深链未闭合 history 与迟到 DELETE 的最新状态语义

- `confirmDelete()` 在请求开始时闭包捕获 `querySlot/selectedDate`，DELETE 和刷新完成后仍用旧值决定 URL；`setSearchParams()` 又使用默认 push。
- 普通成功后浏览器 Back 会恢复已删除 slot 的旧 history entry 并触发 404；DELETE pending 时若用户 Back 到另一个有效 slot/date，迟到成功分支会用旧 `{date}` 覆盖新导航。
- 违反 A11、A13、A14；当前纯参数测试无法观察 router history 或 deferred navigation。
- 修复边界：完成时根据最新 search params 判断；只有最新 `slot` 仍等于被删除 ID 才清理，并使用 replace 语义；保留完成时其余有效参数，不用旧 date 覆盖新导航；补真实 memory router/history 与 deferred-state 测试。

#### REV-018：cancelled timed projection 未参与最终可见集合的视觉 lane

- model lane 算法只纳入 active timed projections；取消项固定 `lane=0/laneCount=1`，但开启“显示已取消”后仍以完整 pointer-enabled 事件盒渲染。
- 同时段 active hold 与 cancelled shoot 会得到相同 top/height/left/width，DOM 盒完全重合，后渲染项遮挡并截获另一项点击。
- cancelled 不参与领域占用/冲突是正确的，但展示 collision 必须独立处理；当前行为违反 A4/A9。
- 修复边界：按最终可见、已过滤的 projections 重新计算纯展示 lanes，让 cancelled 参与展示碰撞但绝不反向进入 conflict/openings/utilization/turnaround；补同时段、部分重叠和短端点相接的模型与浏览器矩形/点击测试。

### Round 3 important

#### REV-014：非整小时 DST 的中间刻度丢失真实分钟

- `calendarTimeline()` 从真实 zoned instant 读取 `local.hour`，但中间 tick 的分钟硬编码为 `:00`；transition day 又会在生产周视图中显示这些标签。
- Australia/Lord_Howe 2026-10-04 的 120/180/240 elapsed-minute 真实当地时间是 `02:30/03:30/04:30`，当前显示 `02:00/03:00/04:00`；fall 日同样把 `01:30/02:30/03:30` 截为整点。
- 事件仍按 instant 正确定位，因此标签与事件错开 30 分钟，违反 A12 的 DST 时间轴契约。
- 修复边界：中间 tick 使用完整真实 `HH:mm`，重复标签也按完整 `HH:mm` 判定，保留 terminal `24:00`；补 Lord Howe spring/fall 中间 tick 与事件投影测试。

#### REV-015：短档期的 22px 最小高度破坏真实时间几何

- lane 模型按真实半开区间允许端点相接事件复用同一 lane，但周视图事件盒用 `Math.max(22, proportionalHeight)` 扩大真实几何。
- 在 44px/hour 下，两个连续 15 分钟 slot 的 top 为 440/451、height 均被放大到 22；前一项 bottom=462，视觉和点击区域与后一项重叠 11px。
- 这会让领域上不冲突的事件看起来重叠，并令后渲染按钮遮挡前项，违反 A4/A6。
- 修复边界：定位 wrapper 保持真实比例高度，把可读/可点击呈现放入不改变时间几何的 inner 元素；补端点相接短档期的结构与矩形断言。

#### REV-016：删除当前深链目标后保留失效 `?slot=`

- 删除成功只刷新数据、关闭确认层并聚焦日期，没有更新 search params；`querySlot` 继续来自 URL 并作为 selected slot 传入。
- 从 `/calendar?date=2026-08-01&slot=slot-1` 删除 `slot-1` 后，页面仍保留相同 URL；刷新或复制链接会 GET 已删除资源并显示 404。
- 影响 A11 的删除闭环与 A14 的 URL/selected 同步。
- 修复边界：仅当 `querySlot === target.id` 时，在删除成功分支把 URL 规范化为 `{ date: selectedDate }`；删除其他 slot 时保留仍有效的当前 selection，并补 deferred DELETE/router 回归。

### Round 2 important

#### REV-011：DST transition 周的公共时间轴错误标注普通日

- `WeekCalendar` 优先把周内 `durationMinutes !== 1440` 的 transition day timeline 选作全周唯一公共轴，但事件仍按各自自然日的 elapsed instant 定位。
- spring 周普通日 elapsed 120 分钟实际是 `02:00`，公共轴却显示 transition day 的 `03:00`；fall 周普通日 `02:00` 会与公共轴第二个 `01:00 UTC-05:00` 对齐，并被放进 25 小时轨道。
- 影响 A12 的 23/25 小时日投影与 A15 的时间轴可理解性。
- 修复边界：保留 instant-based top/height；公共轴优先普通 24 小时日，transition day 在本列内展示带 offset 的独立刻度/边界；增加普通日与 spring/fall transition day 同屏测试。

#### REV-012：删除流程缺完整 mutation、焦点与提示生命周期

- 删除 mutation 没有同步 in-flight guard/deleting state，deferred DELETE 下快速双击可能重复请求；pending 时取消、Escape、backdrop 仍可关闭。
- 成功后先关闭确认弹层，focus trap 会把焦点还给随后被刷新移除的 slot 按钮；`lastDeletedShoot` 又未绑定日期或在导航/新删除时清理。
- 影响 A11 的移动 CRUD、关闭后稳定焦点与提示上下文正确性。
- 修复边界：增加同步 guard 与 deleting state；pending 时禁止重复提交和关闭；刷新后显式聚焦稳定日期控件；notice 绑定日期并在导航/新删除/non-shoot 删除时清理；补异步与浏览器证据。

#### REV-013：POST create response 的 overlaps 仍包含 cancelled shoot

- 前端 preview 已通过共享 occupancy predicate 排除 cancelled shoot，但后端 `findOverlaps()` 只查原始 `schedule_slots`，不知道 shoot 关联订单状态，因而会把 cancelled shoot ID 原样返回在 `CreateResult.Overlaps`。
- 结果是 preview 与公共 API response 的硬重叠语义分裂；active shoot、hold、busy 与半开区间语义本身仍应保留。
- 修复边界：在同一 AccountScope/事务中批量读取相交 shoot 的订单状态并过滤 cancelled，不引入 N+1；补 repository 与 HTTP 测试。

### Round 1 closed findings

#### REV-001：修改表单会解除正在进行的真实写请求锁

- `ScheduleSlotDialog.changeDraft()` 对所有输入变化调用 `invalidateConflictPreview()`；后者无条件 `setSaving(false)`。
- 时间、类型、订单与备注控件在 `saving=true` 时仍可编辑；真实 PATCH/create/recovery 又共用同一 `saving`。
- 反例：PATCH A pending 时修改输入会解锁按钮，用户可发出 preview B 及第二个 PATCH；编辑 PATCH 无 Idempotency-Key，返回顺序会决定最终值与关闭时机。
- 违反 design A10 及“不得破坏 journal/Idempotency-Key/unknown recovery”约束。
- 修复边界：preview 失效只能释放 active preview 的 loading；真实 mutation 必须有独立锁/重入 guard，并冻结所有改变请求体的控件。不得重写 journal 状态机。

#### REV-002：转场缓冲只计算、不展示

- `CalendarDayModel.tightTurnarounds` 已计算，但生产 UI 没有 consumer；`DayDetailPanel` 只显示硬冲突。
- Settings 中 `turnaround_minutes` 因此对用户不可见，design A6“缓冲不足显示软提醒且不进冲突数”未交付。
- 修复边界：将 turnaround 关联到 current/previous slot，在当日详情显示具体间隔与阈值；不得计入 `conflictCount` 或阻止保存。

#### REV-003：空档上限错误地按区间条数而非不同日期截断

- `CalendarPage` 对所有日的 openings `flatMap().slice(0, 8)`；复制文案又 `slice(0, 5)`。
- 一天有 9 个合法碎片时，前 8 项全部来自同一天，第二天完全消失。
- 与 owner 明确的“弹层最多 8 天、文案前 5 天”及 A7 冲突。
- 修复边界：按 date 分组后截取前 8 个有空档的日期；每个入选日期保留全部区间；复制文案取前 5 个日期并补纯函数测试。

### Round 1 important（已关闭）

#### REV-004：cancelled shoot 仍进入写前硬冲突预览

- v2 model 正确排除 cancelled shoot；legacy `overlappingSlots()` 只看半开区间，ScheduleSlotDialog 直接使用后者。
- 结果是月/周视图说无冲突，保存前却要求“仍然保存”，违反 design A6 的统一取消语义。
- 修复边界：抽取共享 active/occupancy predicate，写前 preview 排除 cancelled shoot；保留真实重叠“提示但允许保存”。

#### REV-005：周视图只用墙上时钟字符串摆放 DST fold 档期

- `WeekCalendar.timeBlockStyle()` 仅计算 `displayEnd - displayStart`；fall fold 第一次 01:30 到第二次 01:30 实际 60 分钟，却得到零时长和不可区分文本。
- 违反真实时间比例与 A12 的 23/25 小时日契约。
- 修复边界：布局保留账号自然日 instant 边界，按 elapsed instant 计算 top/height；23/25 小时日时间轴与重复小时文本必须可区分。

#### REV-006：单独 `?slot=` 只能在当前 42 日窗口搜索

- `querySlot` 不决定初始月份；当前 slots 中找不到就立即报“档期已不存在”。
- design A14 明确 `?slot` 可恢复并定位对应日期；远期/历史合法 slot 会被误报删除。
- 修复边界：增加账号隔离的窄单档期读取并返回 list-item 摘要，先定位日期再加载 42 日窗口；禁止无界拉取全部 slots。

#### REV-007：openings 后台刷新会覆盖用户编辑文案

- `OpeningsDialog` effect 依赖 `[open, openings]`，每次新数组都会重建 draft。
- UI 明确承诺复制前可编辑；window focus、CRUD 刷新或 retry 均可能静默丢失措辞。
- 修复边界：维护 dirty；首次打开/首批结果且未编辑时生成，dirty 后 props 变化不覆盖，下次重新打开重新生成。

#### REV-008：Settings PATCH 期间其余字段仍可编辑，成功 rehydrate 会覆盖新输入

- 保存成功按服务端响应 `hydrateSettingsForm(next)`；但仅 availability editor 和 submit 按钮禁用，timezone、提醒、digest、churn thresholds 仍可编辑。
- 用户在请求 pending 时继续输入会被成功响应静默覆盖。
- 修复边界：保存或 stale 时禁用完整可写 fieldset；失败仍保留原草稿，成功继续以服务端响应 rehydrate。

#### REV-009：周视图事件双击冒泡到 day track

- day track 的 `onDoubleClick` 会接收 event button 双击；两次 slot click 后又选择 date，可能清掉 slot query/选择。
- 修复边界：event button 阻止 doubleClick 冒泡，空白 track 双击仍可选日。

### Round 1 nit（已关闭）

#### REV-010：深链程序化聚焦的 slot article 没有可见焦点环

- slot article 使用 `tabIndex={-1}`，但局部 focus-visible 规则仅覆盖 button/input/textarea/summary。
- 修复边界：把 calendar workspace 中可聚焦的 `[tabindex]` 纳入同一 focus-visible 样式。

### suggestion

- 后续可清理旧 `buildCalendarDays()` 的退役测试面，并让 legacy preview 与 v2 model 复用同一 occupancy predicate；本轮只做消除语义漂移所需的窄共享，不进行大模块重构。

### learning

- DST fold 无法只由 `HH:MM` 表达：相同的 01:30 可能是两个相隔一小时的 instant。创建弹窗已允许 occurrence 选择，周布局不能在投影阶段再次丢掉 offset/elapsed 信息。
- 异步 generation helper 绿灯只证明比较函数，不证明 React 编排的 loading、mutation、input 与 unmount 状态互不覆盖；关键状态必须有独立 invariant。

### praise

- Settings strict decode、默认值、损坏 JSON fail-closed、AccountScope 与双账号测试边界扎实。
- schedule 摘要沿用批量装配，无 N+1；Dashboard 复用与稳定排序契约清楚。
- dataexport 显式列、非默认 availability parity 和 schema v2 同步完整。
- main/openings/preview 的 AbortController + generation 总体方向正确；主要缺陷集中在消费与状态边界，而非后端主体能力。

## 5. Test And QA Focus

- 写流程：deferred preview/PATCH/create/recovery 下输入不得解除 mutation lock，不得重复建单/slot，迟到 preview 不改变当前 range 或锁。
- 取消语义：cancelled shoot 不进入 preview、不占 opening/利用率/冲突/转场，但仍可查看和删除。
- 转场：0 分钟端点相接与小于阈值显示软提醒，等于阈值不提醒，硬冲突数保持独立。
- 空档：单日多区间、8/9 天边界、前 5 天文案；编辑后 focus/refresh/retry 不覆盖草稿。
- DST：America/New_York first/second 01:30、spring gap、23/25 小时日、跨午夜与全天。
- 深链：`date+slot`、slot-only 跨月、404、取消 slot 自动展开并聚焦。
- Settings：PATCH pending 全表单不可编辑；失败保草稿；成功以服务端响应回填。
- 浏览器证据：1600/1280/375 的 viewport、workspace clientWidth、layout mode、scrollWidth、焦点元素、role/aria 与关键请求日志。
- Round 2 新增重点：普通日与 DST transition day 同屏时公共轴/列内 offset；deferred DELETE 的双击、Escape/backdrop、成功焦点与 notice 清理；cancelled shoot 在 preview 与 POST overlaps 两端一致。
- Round 3 新增重点：Lord Howe spring/fall 的半小时中间刻度与同屏事件；连续 5/15 分钟端点相接档期的 DOM rectangles/clickability；删除当前 slot 深链后的 URL 规范化、刷新/后退，以及删除非当前 slot 不清理有效 selection。
- Round 4 新增重点：删除当前 slot 后 Back/Forward 不恢复失效资源，deferred DELETE 不能覆盖 pending 期间的新导航；active + cancelled 同时段/部分重叠/短档期在显示取消项时矩形不重叠且均可点击，领域占用语义保持不变。
- Round 5 新增重点：M1 deferred DELETE pending 后跨月导航到 M2 时，旧 M1 refresh 不得中止/覆盖 M2 当前请求或数据；两种响应顺序与离开 Calendar 均需覆盖。
- Round 6 新增重点：slot ID 不变但 date/effective range 从 M1 切到 M2 时，同样不得调用旧 M1 refresh；current range reload 与 deleted URL 清理必须同时成立。
- Round 7 新增重点：同 date/slot、Settings retry 令账号 timezone 从 TZ1 更新到 TZ2 时，完成判断必须使用 latest timezone 派生 current range，不能被 mutation-start timezone 伪装为同范围。
- Round 9 新增重点：slot-only 与非法 date 删除完成时必须把 effective date 同步写入 replace URL；同一 canonical completion 同时驱动 URL、selected detail、month、focus、notice 与 latest-timezone range，覆盖 Settings/DELETE 两种顺序及同一 turn。

## 6. Residual Risk

- 现有 evidence pack 没有归档可复跑的截图、DOM dump、API fixture 或 trace；QA 必须重新生成可审计证据。
- 纯 generation 测试没有穿过 React 编排；若当前测试栈不新增 DOM runner，至少需要纯状态 seam + 浏览器 deferred-network QA 双重证明。
- 当前 `schedule.test.ts` 对 dialog/delete 仍含 source-contract 断言，不能单独证明 deferred 网络、关闭顺序、焦点、DOM rectangles 或 router 行为；这些必须在 QA 中用真实 DOM/API trace 补足。
- evidence pack/DoD 仍归档 review-fix 前的 schedule 41/41、settings 4/4；进入 QA 前必须重生成或显式补充当前轮次的可重放门禁证据。
- Testcontainers mapped-port 抖动为已知环境风险；最终完整回归需保留首次失败和定向重试证据。

## 7. Verdict

- Round 1：`changes-requested`；REV-001～REV-010 已进入 review-fix 并完成定向/全量验证。
- Round 2：`changes-requested`；无 blocking，REV-011～REV-013 三个 important 会影响 A11/A12/A15 及公共 overlap 契约，默认先修。
- Round 3：`changes-requested`；无 blocking，REV-014～REV-016 三个 important 影响 A4/A6/A11/A12/A14，默认先修。
- Round 4：`changes-requested`；无 blocking，REV-017～REV-018 两个 important 影响 A4/A9/A11/A13/A14，默认先修。
- Round 5：`changes-requested`；无 blocking，REV-019 影响 A13/A14 的 current range/server-state 同步，默认先修。
- Round 6：`changes-requested`；无 blocking，REV-020 继续影响 A13/A14 的 same-slot 跨 range 同步，默认先修。
- Round 7：`changes-requested`；无 blocking，REV-021 影响 A12/A13/A14 的 timezone-aware current range identity，默认先修。
- Round 8：`passed`；blocking 0、important 0，REV-021 及全部既有 material closure 均已核验，QA gate 开启。
- Round 9：`changes-requested`；blocking 0、important 1，REV-022 影响 slot-only/非法 date 的 canonical URL/selection/range 与 REV-021 same-turn closure，必须继续窄修复。
- Round 10：`changes-requested`；blocking 0、important 1。REV-017～REV-022 均确认关闭；新增 REV-023 指出 Settings 永不 settle 时 DELETE 成功后的 UI liveness 缺陷。
- Round 11：owner-directed local-only closure。REV-023 已由可执行 timeout/idle/abort seam、exactly-once DELETE、乐观移除、迟到 TZ2 latest-search guard、导航/unmount 与 401 refresh stall 浏览器证据关闭；canonical `make check` 完整 exit 0。Owner 明确终止继续 review，Round 11 reviewer 被中止，不伪造独立 verdict。
- OCR disposition：Round 2 High=0；Settings `%v`、delete `writesEnabled`、`void onCopy`、每日期条数和利用率 merge 建议均因设计事实不成立或与 owner 已批口径冲突而丢弃；Round 3～Round 9 OCR preflight 因 HTTP 402 未启动，真实记为 unavailable。
- Next：按 owner 明确的 review cap 进入一次性 QA 收口；不再启动新的 reviewer。该 local-only closure 不等同于 Goal acceptance authorization。

## 8. Focused Closure

- none；Round 9 已按 Material 类别完成完整独立复审，不适用 focused closure。
