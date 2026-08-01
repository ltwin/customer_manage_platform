# calendar-v2-redesign 实现记录

## S1 契约基线

状态：完成（2026-07-31）

### 交付

- `api/openapi.yaml` 新增 `ScheduleAvailabilityWindow`、`ScheduleAvailabilityWeekly`、`ScheduleAvailability` 与具名 `UpdateSettingsBody`。
- `ScheduleAvailabilityWeekly` 明确声明 ISO 星期 `"1"`–`"7"` 七个 required 属性；每项允许 `null`，且拒绝额外属性。
- `Settings.availability` 为 required，`UpdateSettingsBody.availability` 为 optional。
- `ShootScheduleSlotListItem` 增加可选 `order_price`、必返收款标记及可选 `package_shoot_type`；non-shoot 分支保持不带订单摘要。
- `ExportDocument.schema_version` 固定升级为 2，`settings` 继续直接引用公共 `Settings`。
- 重新生成 Go 与 TypeScript 契约；没有新增手写 DTO。

### 生成结果核对

- Go 数字星期字段生成名为 `N1`–`N7`，JSON 名保持 `"1"`–`"7"`，类型为 `nullable.Nullable[ScheduleAvailabilityWindow]`。
- TypeScript 数字星期属性生成名为 `1`–`7`，类型为 `ScheduleAvailabilityWindow | null`。
- Go `UpdateSettingsJSONRequestBody` 指向具名 `UpdateSettingsBody`；TypeScript 同步生成可选 `availability`。
- Go 枚举常量与 TypeScript literal 均把导出版本固定为 `2`。

### 验证证据

- 修改前基线：`make generate-check` 通过。
- 修改前后端目标包串行测试通过：settings、schedule、dataexport、dashboard、store、httpapi。
- 修改前前端 `npm run test:schedule`：32/32 通过。
- 修改前前端 `npm run build` 与 `npm run lint` 通过。
- 修改后 `make generate-check` 通过。
- 修改后 `git diff --check` 通过。

### 后续约束

S1 只固定公共契约；`Settings.availability` 的持久化、默认值、严格解码、运行时校验、HTTP 投影与 dataexport parity 在 S2 实现。shoot 摘要的批量数据装配在 S3 实现。

## S2 Settings 持久化、API 与导出

状态：完成（2026-07-31）

### RED

- 新增 Settings 领域测试，先观察到 `Settings.Availability`、`PatchInput.Availability` 与 availability 类型不存在导致的预期编译失败。
- 新增 HTTP/PostgreSQL 场景，固定无行默认、合法整体替换、缺失/额外星期、坏时间、反转窗口、阈值越界、双账号隔离和损坏存储数据的外部行为。
- 将 dataexport service 测试的预期版本先改为 2，旧实现按版本 1 返回而失败。

### GREEN

- 新增深模块 `backend/internal/settings/availability.go`，集中完整七日模型、默认值、严格 JSON 解码和领域校验；每天允许单窗口或 `null`。
- migration `0014_settings_availability` 给新旧 settings 行提供相同的 JSONB 默认值，并带可验证 down migration。
- Settings repository 的显式列、scan/upsert 与 HTTP GET/PATCH 投影同步 availability；PATCH 只在请求显式含 availability 时整体替换。
- handler 对 availability 做局部 strict decode，拒绝根对象、weekday map 和 window 的未知字段，且拒绝缺少或额外 weekday。
- 持久化 JSON 不能严格解码或违反领域约束时不叠加默认值，fail-closed 为 500；客户端非法请求为 400 且不写入。
- dataexport 的独立显式列、严格解码与 API 投影同步，`schema_version` 固定为 2；非默认 availability 与 `GET /settings` 一致。
- 新 migration 推进 schema version 后，同步修正既有 migration up/down 测试的版本栈假设（current version 14）。

### 验证证据

- Settings 单元测试通过：完整默认、合法整体替换、时间与分钟边界非法时零写入。
- migration 集成测试通过：13 → 14 的旧行获得七日默认；14 → 13 删除 availability 列。
- HTTP 集成测试通过：默认、合法更新、六类非法输入、非法更新后旧值不变、双账号隔离、损坏 JSON 返回 500。
- dataexport repository/route 测试通过：非默认 weekly/null/阈值与 Settings service parity，导出 JSON 版本为 2。
- 设计规定的后端核心命令通过：settings、schedule、dataexport、dashboard、store、httpapi，`-p=1 -parallel=1`。
- `make generate-check`、`npm run test:schedule`（32/32）、前端 build/lint 全部通过。

## S4 日历纯计算模型

状态：完成（2026-07-31）

### RED

- 在 `npm run test:schedule` 中先引入新的页面专属 model seam，观察到目标模块不存在的预期失败。
- 测试先固定取消排除、半开端点、重叠簇泳道、跨周/全天投影、转场、空档、概览、无工作日与 DST compatible 行为。

### GREEN

- 新增 `frontend/src/pages/calendar/model.ts`，集中输出 `Opening`、`TightTurnaround`、`WeekSlotLayout`、`CalendarDayModel` 与 `MonthOverview`，不持有 React state 或网络状态。
- 取消订单的 shoot projection 仍保留并标记 cancelled，但排除硬重叠、空档占用、拍摄数和利用率。
- 硬冲突按同一自然日内的真实半开瞬时间隔计算；端点相接不冲突。
- timed projection 按相交簇分配最小可用泳道，laneCount 只在簇内共享；全天与取消项不进入 timed lane cluster。
- 转场从当前非冲突档期向前选择最近结束的非取消、非全天档期；非负且小于阈值时产生软提醒，零分钟端点相接可报告但不计硬冲突。
- openings 在真实账号时区工作窗口内减去 shoot/hold/busy 的合并占用并丢弃短碎片；重叠占用不会重复扣除。
- 月度概览按自然月计算：取消排除、shoot 按 slot 去重、hold/conflict/open 按日计数；利用率分子只累计 shoot+hold，重叠按各档期时长相加并封顶 100%，busy 只占空档；无工作窗口返回 `null`。
- recurring availability 采用 Temporal `compatible`：spring gap 向后平移、fall fold 取 earlier；映射后反转时当日 fail-closed，保留可观察 error 且不产出 openings/利用率。
- API client 只增加指向生成 schema 的 availability type alias，没有手写重复 DTO。

### 验证证据

- `npm run test:schedule`：37/37 通过（原 32 项 + S4 新增 5 个聚合场景）。
- 覆盖跨午夜、`end_at=00:00`、全天、跨周、23/25 小时自然日、spring gap、fall fold、端点相接、传递重叠簇、取消项和阈值边界。
- `npm run build` 与 `npm run lint` 通过。
- `make generate-check` 与 `git diff --check` 通过。

### 范围守护

- 未新增 schedule openings/overview/book 端点。
- 未让客户端传入 `account_id`；所有持久化继续通过 `AccountScope`。
- 未改 Settings 以外端点的 JSON 严格绑定策略。

## S3 schedule 展示读模型

状态：完成（2026-07-31）

### RED

- 先扩展既有 schedule repository 测试，要求 shoot list item 返回价格、定金/尾款状态和套系拍摄类型；测试因 `schedule.ListItem` 缺少四个新字段而预期编译失败。
- HTTP union 测试先要求两个收款标记必返，并要求无价格/无套系时可选字段缺省、hold/busy 不泄漏任何订单摘要。

### GREEN

- `schedule.ListItem` 与内部 `orderSummary` 增加 `OrderPrice`、`OrderDepositPaid`、`OrderBalancePaid`、`PackageShootType`。
- 继续复用既有批量装配：一次查询相交 slots、一次按 order IDs 取 orders、一次按 customer IDs 取 customers、一次按 package IDs 取 packages；只扩显式列，不引入逐档期查询或跨域 service 注入。
- package 批量摘要从仅 name 扩为 name + shoot_type。
- HTTP shoot union 投影新增 required 收款标记和 optional price/shoot type；non-shoot 投影代码保持独立且无这些字段。
- dashboard 今日档期继续调用同一个 `AssembleListItems`，与 schedule list 保持逐字节 parity。

### 验证证据

- schedule repository 测试通过：有价格、有收款状态、有套系时摘要正确；hold/busy 领域项不携带摘要。
- HTTP schedule 测试通过：shoot 必返两个收款字段；未设置 price/package 时对应 optional key 缺省；hold/busy 不出现九类订单/客户摘要字段。
- schedule、dashboard、httpapi 全包串行回归通过。
- dashboard today_slots 与同窗口 schedule list 的逐字节 parity 测试通过。
- `make generate-check`、`npm run test:schedule`（32/32）、前端 build/lint 全部通过。

## S5 工作区结构与交互

状态：完成（2026-07-31）

### RED

- 在 `npm run test:schedule` 中先约束唯一容器宽度断点：`SIDE_PANEL_MIN_WIDTH=1080`，1079 输出 `bottom-sheet`，1080 与 1600 输出 `side-panel`；目标模块不存在时测试按预期失败。

### GREEN

- 新增 `CalendarWorkspace`、工具栏、月视图、周视图、当日详情、空档弹层、format/types/layout-mode seam，并把页面专属样式集中到 `frontend/src/pages/calendar/calendar.css`。
- `CalendarWorkspace` 默认周视图并持有筛选、取消项、详情与空档弹层 UI state；所有 server state 和 CRUD 仍由外层 props/callback 提供，不在展示组件拼 API 或复制写状态机。
- 月视图固定 42 格、每天最多三条加「还有 N 条」；周视图按真实本地时间比例摆放并复用 S4 的 cluster-local lane/laneCount，全天行与内部横向滚动独立。
- 当日详情在宽模式保持非模态侧栏，在窄模式输出 `role=dialog` 与 `aria-modal=true`；shoot 详情展示价格、款项、套系类型和冲突，取消项默认放入折叠区，桌面和窄模式均保留新建/编辑/删除入口。
- 空档弹层按日期分组，最多列出 8 个有空档的自然日、复制文案取前 5 个自然日且可编辑；同一天的多个合法区间全部保留；组件只调用传入的本机 copy callback，不发送网络请求。
- CSS 只消费根节点 `data-layout-mode`，没有建立第二套 viewport 断点；页面根与 stage 均 `min-width:0/overflow` 收口，只有周时间轴允许内部横向滚动；包含 dark token 与 reduced-motion 处理。

### VERIFY

- `npm run test:schedule`：38/38 通过。
- `npm run build` 与 `npm run lint` 通过；仅保留 Vite 既有大 chunk 警告，无编译或 lint 失败。
- 临时静态 fixture（验证后已删除）模拟 236px shell sidebar：
  - viewport 1600、workspace 1308：`side-panel`；详情无 `role`/`aria-modal`；document、body 与 workspace 的 `scrollWidth === clientWidth`。
  - viewport 1280、workspace 988：`bottom-sheet`；详情为 `role=dialog`、`aria-modal=true`，FAB 可见；document、body 与 workspace 的 `scrollWidth === clientWidth`。
  - 1280 月视图固定 42 个 cell，month/workspace 无横向溢出；1600 月视图同样无横向溢出。
- 浏览器肉眼检查发现窄模式曾继承宽模式 `top:76px`，已最小修复为 `top:auto` 并复测抽屉最终 `bottom === viewportHeight`。
- `git diff --check` 通过；calendar 新增代码无 `console.log/error`、临时 TODO/FIXME/XXX、无原型运行时 import，静态 fixture 未留在工作树。

## S6 真实状态与写流程接线

状态：完成（2026-07-31）

### RED

- 在 `npm run test:schedule` 中先固定两个 latest-wins 行为：月份 A 的迟到响应不能覆盖月份 B；冲突预览旧时间范围不能覆盖新范围。目标 request generation/range seam 不存在时按预期失败。
- 在 `npm run test:api-client` 中先要求 slots 与 Settings GET 把调用者的 `AbortSignal` 传入 fetch；旧 client 签名不接受 signal 时按预期失败。
- 将 Settings 读取失败场景加入纯模型测试：availability 缺失时仍需投影真实 slots、硬冲突和周泳道，但不得产生工作窗口、空档或转场等伪造结论。

### GREEN

- 新增 `calendar/requestState.ts` 与 `schedule/conflictPreview.ts`，用 `rangeKey/key + generation` 判断响应是否仍属于当前请求。
- `listScheduleSlots(from,to,signal?)` 与 `getSettings(signal?)` 将 `AbortSignal` 真实传入 fetch；创建档期的 `Idempotency-Key` 保持不变。
- `CalendarPage` 改为真实 v2 容器：Settings 与 42 日 slots 并行、独立 loading/error/retry；主范围和未来 14 天空档各自使用 `AbortController + rangeKey + generation`；写成功统一重拉，不在本地伪造服务端结果。
- 保留 `?date`、`?slot`、`?schedule_draft`；slot 深链按账号时区定位自然日并聚焦事件。点击事件先开详情，只有明确编辑动作才进入既有 `ScheduleSlotDialog`。
- Settings 失败时 availability 为 `null`：真实 slots、详情和 CRUD 保持可用；可约背景、空档、利用率和转场等结论隐藏或禁用。
- `ScheduleSlotDialog` 在输入范围变化时立即 abort/失效旧预览并清除旧 preview；保存前要求 preview key 等于当前规范化范围，否则重新检查冲突。
- 未重构 journal、Idempotency-Key、unknown recovery、`customer_changed`、历史补录或写入状态机；既有回归测试继续穿过同一公共写入口。

### VERIFY

- `npm run test:schedule`：40/40 通过，包含 pending journal、Idempotency-Key、unknown/deterministic recovery、`customer_changed`、backfill handoff、status sync、source draft、写弹窗抑制详情层，以及新增 request generation/范围保护。
- `npm run test:api-client`：6/6 通过，证明 slots/Settings GET 接收同一个 signal，create slot 仍携带 Idempotency-Key。
- `npm run build` 与 `npm run lint` 通过；Vite 只有既有大 chunk 警告。
- 真实 HTTP 浏览器冒烟（临时 mock API 与 Vite 代理均已在验证后删除）：
  - `/refresh`、`/me`、`/settings`、`/schedule/slots` 真实 fetch 成功；`?date=2026-07-31&slot=busy-1` 定位并聚焦目标事件。
  - 1600 viewport、workspace 1308：`side-panel`，document/workspace 均无横向溢出。
  - 新建个人占用“外出取景”与既有 shoot 重叠时先展示 1 条冲突，点击“仍然保存”后创建成功并自动重拉；周视图和详情都立即显示冲突，冲突日从 0 变为 1。
  - 删除该档期后自动重拉，“外出取景”消失而原 shoot/busy 保留，冲突日恢复为 0；toast 为“档期已删除，订单和订单状态保持不变”。
  - 强制 `/settings` 返回 500 后，shoot/busy 与新建/编辑/删除仍可达；未来空档禁用，概览五项均显示 `—`，错误可重试；document/workspace 继续无横向溢出。
  - 恢复 Settings 后，未来空档通过独立真实 slots 请求打开，列出账号工作窗口内的未来 14 天结果并生成前 5 条可编辑复制文案。
- `git diff --check` 在 S6 临时 fixture 清理后通过；新增运行时代码无 mock/fixture import、无调试输出或临时 TODO。

## S7 Settings UI 与体验收尾

状态：完成（2026-08-01）

### Settings 编辑闭环

- `AvailabilitySettingsDraft` 从非默认服务端响应完整回显七天窗口，其中周三为 `null`；启用周三后得到独立默认窗口，不修改其他星期。
- 浏览器把最小空档改为非法值 `10` 后，原生约束阻止提交且输入草稿保持为 `10`；`npm run test:settings` 另外固定反转窗口、坏 `HH:MM`、非整数与上下界的 validator 错误语义。
- 临时 API fixture 令下一次 Settings PATCH 返回 503：周三启用、最小空档 `135`、转场 `65` 均保留，页面显示“设置保存暂时失败”，可直接重试。
- 重试时请求体仍携带 timezone、birthday/follow-up、digest 与三类 churn thresholds 的当前值；fixture 将成功响应刻意规范化为最小空档 `140`、转场 `70`，页面随服务端响应重新 hydrate 为 `140/70`，toast 为“设置已保存”，证明成功分支不沿用提交前本地值。
- `npm run test:settings`：4/4 通过，覆盖七天/null clone、时间与阈值校验、availability 替换及其他可写字段保持不变；`Makefile` 的 canonical `check` 已包含该脚本。

### 三档响应式与可访问性

- viewport `1600×1000`、workspace `clientWidth=1308`：`side-panel`；详情无 `role`/`aria-modal`；document、body、workspace 的 `scrollWidth === clientWidth`；周时间轴为 `overflow-x:auto`，`clientWidth=932`、`scrollWidth=940`。
- viewport `1280×900`、workspace `clientWidth=988`：`bottom-sheet`；详情为 `role=dialog`、`aria-modal=true`，抽屉 `bottom=900`；document/body/workspace 无页面级横向溢出。
- viewport `375×812`、workspace `clientWidth=351`：`bottom-sheet`；document/body/workspace 无横向溢出；周时间轴 `clientWidth=349`、`scrollWidth=940`，仅内部横向滚动；月视图固定 42 格且 `month.scrollWidth=month.clientWidth=349`。
- 周标题方向键从 `2026-08-03` 右移到 `2026-08-04`，只移动焦点；详情 Escape 关闭后焦点回到对应日期按钮。实测发现原实现会把焦点留在隐藏的关闭按钮，已在 `CalendarWorkspace` 记录触发项并以选中日期作深链回退，复测通过。
- 实测发现移动 FAB 与全局主题按钮在 375px 完全重叠，导致“新建档期”实际切换主题；已在单一 `bottom-sheet` 规则中把 FAB 横向移开，不新增 viewport 断点。复测两个按钮矩形不相交，均可操作。
- 删除确认打开时以既有 `detailSuppressed` seam 隐藏详情面板，页面只有一个可见 `aria-modal`；取消后详情恢复。空档弹层 Escape 关闭后焦点回到“未来空档”按钮。
- 深色主题在 375 与 1280 实际渲染；`prefers-reduced-motion: reduce` 规则静态核对为关闭 skeleton animation 和抽屉 transition。本次浏览器控制面不提供媒体偏好模拟，未改系统级辅助功能设置。

### 移动写操作与页面状态

- 375px 从修复后的 FAB 新建“个人占用”→检查冲突→保存；列表重拉后出现“移动端 QA 外出”。
- 从当日底部详情进入编辑，改为“移动端 QA 外出（已编辑）”并保存；重拉后周视图和详情同步更新。
- 从同一详情进入删除确认并删除；档期消失，toast 明确“档期已删除，订单和订单状态保持不变”。
- Settings GET 失败时，真实 shoot/hold/busy、详情和写入口继续可用；未来空档禁用、五项概览显示 `—`，错误关闭详情后可重试，恢复后重新显示可约背景与概览。
- 在主月份不覆盖未来 14 天窗口时，单独注入 openings slots 500：错误只出现在空档弹层，复制禁用；点击重试后返回 8 条结果并恢复复制。可编辑文案写入本机剪贴板，未调用外部消息服务。
- 首次加载 skeleton、空日期详情、Settings 降级错误、openings 独立错误/重试与正常结果均取得真实 DOM 证据和关键截图。

### TDD 与验证

- Draft/validator/serializer 行为由 `npm run test:settings` 自动化覆盖；layout mode、方向键、纯模型、请求 generation 与写流程回归由 `npm run test:schedule` 覆盖。
- TDD exception：焦点恢复、固定层碰撞和双模态属于实际布局/浏览器焦点栈行为，仓库没有 React DOM 测试运行器；以失败前后的真实 1280/375 DOM、焦点、矩形和 CRUD 浏览器证据替代，修复范围仅限 `CalendarWorkspace`、Calendar 容器抑制条件与局部 CSS。
- `npm run test:schedule`：41/41；`npm run test:settings`：4/4；`npm run test:api-client`：6/6；`npm run build`、`npm run lint` 与 `git diff --check` 通过。Vite 仅有既有大 chunk warning。
- 临时 mock API 与 Vite override 只位于 `/tmp`，生产前端无 fixture/mock import；将在 S8 证据收口后停止并删除。

## S8 全量回归与范围守护

状态：实现、核心门禁与范围核对完成（2026-08-01）；独立 review、QA 与 acceptance 作为后置质量 gate 继续执行并另行归档。

### 核心门禁

- `make generate-check` 通过，OpenAPI 生成的 Go/TypeScript 契约与仓库内容一致。
- 后端目标包串行回归通过：`settings`、`schedule`、`dataexport`、`dashboard`、`store`、`httpapi`，使用 `-p=1 -parallel=1` 避免 Testcontainers mapped port 偶发抖动。
- 前端专项回归通过：`npm run test:schedule` 41/41、`npm run test:settings` 4/4、`npm run test:api-client` 6/6；build 与 lint 通过。
- canonical `make check` 完整通过：前后端 build、golangci-lint（0 issues）、frontend lint、全部 Go 包、全部前端脚本、安全 catalog、legacy cutover 与 v1 ops contract 均为 pass。Vite 只有既有的大 chunk warning。
- Goal before-review DoD runner 的第一次 CMD-002 在 `platform/store` 命中 attention 已记录的 Testcontainers mapped-port 偶发错误；同轮 `make check` 仍通过。保留失败证据后单独重试 CMD-002 通过，再完整重跑 CMD-001–004 全部通过；最终 `dod-results`、`gate-results` 与 `evidence-pack-results` 均为 `passed`。
- `git diff --check` 通过；`make check` 产生的未跟踪 Python `__pycache__` 已删除，没有把测试副产物留在工作树。

### 过期回归契约修正

- `frontend/scripts/v1-hardening.test.ts` 原先断言移动端必须隐藏三个 `desktop-schedule-actions`，与批准后的 Calendar v2「375px 新建、编辑、删除全部可达」契约冲突，因此成为 `make check` 的唯一初始失败。
- 测试已改为直接约束 v2 的公开挂载点：移动 FAB 调用新建、当日详情保留新建/编辑/删除、旧隐藏 class 不得回流、bottom sheet 高度受限且正文可滚动、FAB 在窄模式可见并避开全局主题按钮、coarse pointer 继续满足 44px 触控目标。
- `npm run test:v1-hardening` 9/9 通过，随后完整 `make check` 通过；没有为了绿灯恢复旧版移动端隐藏行为。

### 六个挂载点反向核对

- 数据库：`0014_settings_availability` 在 `settings.availability` 上提供完整七日 JSONB 默认值，包含可回滚 down migration 与迁移测试。
- 公共契约：`api/openapi.yaml`、生成的 `api.gen.go` 与 `schema.d.ts` 均包含 `ScheduleAvailability`，没有手写平行 DTO。
- `/settings`：HTTP 局部 strict decode、领域校验、repository 显式 scan/upsert 与 PATCH 整体替换链路完整。
- `/calendar`：`CalendarPage` 从真实 Settings 和 slots 派生工作窗口、空档、冲突、转场和概览；Settings 不可用时 fail-closed 但保留已有档期与 CRUD。
- Settings 页面：独立 draft/validator/serializer 与 availability editor 已挂载，失败保留草稿，成功按服务端响应重新 hydrate。
- dataexport：独立显式 settings 列与 strict decode 包含非默认 availability，ExportDocument 版本固定为 2，并有 repository/service/route parity 测试。

### 禁止项与清洁度

- 未新增 `/schedule/openings`、`/schedule/overview`、`/schedule/book` 或外部消息发送端点；空档、概览和复制文案均为前端本地派生/本机剪贴板行为。
- `frontend/package.json` 只新增 `test:settings` script；没有第三方 calendar dependency。
- 生产代码没有引用 `frontend/proto-design/v2`、prototype fixture/store 或 demo runtime；仓库既有 `frontend/src/crm/PrototypeStore.tsx` 与本 feature 无关。
- 未加入拖拽、重复规则或多工作窗口；新增代码没有临时 TODO/FIXME/XXX、debugger 或 console 调试输出。

## Code Review Round 1 修复闭环

状态：行为修复与定向自动化完成（2026-08-01）；等待 material round 2 完整复审。

### 已核实问题与修复

- REV-001：把冲突预览 loading 与真实 mutation saving 拆为两个状态；`invalidateConflictPreview()` 只在确有 active preview controller 时释放 preview loading，永不释放 mutation lock。mutation 另有同步 ref guard 防止 React 重渲染前的快速双击重入；真实 PATCH/create/recovery pending 时，`schedule-dialog-fields` 冻结所有会改变请求体的控件。
- REV-002：`DayDetailPanel` 消费 `tightTurnarounds`，对 current/previous slot 显示具体间隔和 Settings 阈值；软提醒保持独立样式，不计入硬冲突数、不阻止保存。
- REV-003 / REV-007：新增 openings 纯函数，先按不同 date 分组再截取 8 天/5 天；同日多区间合并为同一日期行。弹层维护 dirty 状态，用户编辑后后台 openings 刷新不再静默覆盖，下次重新打开才重新生成。
- REV-004：legacy 写前 preview 与 v2 model 共用 cancelled-shoot occupancy predicate；取消 shoot 不进入 preview、冲突、openings、利用率或转场。
- REV-005：`CalendarDayModel` 保留账号自然日 instant timeline、真实分钟数、DST tick 与 projection offset；周布局按 elapsed instant 计算 top/height。America/New_York fall fold 的 first/second 01:30 现在相隔 60 分钟，并显示 `UTC-04:00` / `UTC-05:00` 区分。
- REV-006：在既有 `/schedule/slots/{id}` resource 上补 `GET` operation，按 AccountScope 返回与列表相同的 `ScheduleSlotListItem` 摘要。Calendar slot-only 深链先以独立 abort/generation 请求定位自然日，再加载对应 42 日窗口；不做无界列表查询。跨账号读取返回 404。
- REV-008：Settings 的完整可写区由 `settings-edit-fields` 包裹，在保存或 stale rehydrate 期间整体 disabled；失败仍保留草稿，成功继续只按服务端响应 hydrate。
- REV-009 / REV-010：周事件阻止 doubleClick 冒泡，空 track 双击选日保持；slot article 的程序化深链焦点纳入局部 focus-visible 样式，取消深链自动展开取消区。

### 定向验证

- `go test ./internal/schedule ./internal/platform/httpapi`：通过；覆盖单档期摘要读取及跨账号 404。
- `npm run test:schedule`：45/45；新增 cancelled preview、日级 openings、5/8 天边界、fall-fold elapsed/offset、可见转场与 mutation/preview 分离静态合同。
- `npm run test:settings`：5/5；新增完整表单 pending/stale freeze 合同。
- `npm run test:api-client`：7/7；新增单档期深链客户端路径编码与 AbortSignal 合同。
- `npm run build`、`npm run lint`、`git diff --check`：通过；Vite 仅有既有大 chunk warning。

### 范围说明

- 新增的是既有 schedule slot resource 的窄 GET 能力，只用于满足批准 design A14 的 slot-only 深链定位；没有新增 openings/overview/book、外部消息、拖拽、重复排期或第三方日历能力。
- schedule 单条与区间列表共用同一摘要装配 helper，未引入 N+1 或 service-to-service adapter。
- 行为代码已变化，review closure 分类为 Material；不能复用 round 1 verdict，必须重新执行完整双通道代码审查后才能进入 QA。

## Code Review Round 2 修复闭环

状态：REV-011～REV-013 与已核实 nit 的行为修复、RED/GREEN 定向验证完成（2026-08-01）；等待 material round 3 完整复审。

### 已核实问题与修复

- REV-011：周公共轴改为优先选择普通 24 小时自然日，不再把 transition day 的 local labels 套到普通日；每列仍按自身 instant timeline 计算 top/height。23/25 小时列新增独立带 UTC offset 刻度，spring gap 与 fall fold 可在本列解释；全周高度继续取最大真实 duration。`calendarTimeline()` 对 Lord Howe 等 30 分钟 offset 变化日补精确 `24:00` terminal tick。
- REV-012：删除确认新增同步 `deleteInFlightRef` 与 `deleting`；pending 时禁用确认/取消、Escape 与 backdrop close，快速双击不会重复 DELETE。成功刷新后关闭确认弹层并双 `requestAnimationFrame` 聚焦稳定日期控件。shoot 删除提示绑定当前自然日；切换日期、关闭详情、开始新删除或删除 non-shoot 都会清理旧提示。
- REV-013：`CreatePreparedInScope` 在同一账号事务中批量读取相交 shoot 的订单状态，过滤 `cancelled` 后再装配 response overlaps；active shoot、hold、busy、稳定排序与半开区间均保持，不引入逐行查询。
- nit：Settings PATCH 增加同步重复 submit guard；slot-only lookup cleanup 同时 abort 并使 generation 失效，AbortError 不再落 query error；慢 conflict preview 可主动关闭并中止预览，真实 mutation 仍不可关闭；空 month 标题、月格 tab stop、未用 `copyButtonRef` 已收口。

### TDD 与定向验证

- RED：`npm run test:schedule` 因缺少 `calendarWeekAxisTimeline` 真实失败；新增 repository/HTTP tests 均复现 cancelled shoot 出现在 overlaps；新增 UI/Settings source contracts 在 guard/fallback 落地前失败。
- GREEN：`npm run test:schedule` 49/49、`npm run test:settings` 6/6、`npm run test:api-client` 7/7、`npm run test:v1-hardening` 9/9；`npm run build`、`npm run lint` 通过，Vite 仅有既有大 chunk warning。
- GREEN：`go test -p=1 ./internal/schedule/... ./internal/platform/httpapi/... -count=1 -parallel=1` 通过；新增测试同时证明 cancelled shoot 排除、active shoot/hold/busy 保留和 endpoint-touching 不重叠。
- `git diff --check` 通过；生产代码无 fixture/mock import、调试输出或临时 TODO。
- `make generate-check` 通过；Goal CMD-002 的 settings/schedule/dataexport/dashboard/store/httpapi 串行回归通过。
- canonical `make check` 完整通过：前后端 build、golangci-lint（0 issues）、全部 Go 包、全部前端专项测试、安全 catalog、legacy cutover、v1 ops contract 和生成物检查均通过；只有既有 Vite 大 chunk warning。命令产生的 Python `__pycache__` 已作为测试副产物清理。

### 范围说明

- 修复只统一批准设计已有的 DST、取消占用、删除 mutation/focus 与键盘可达语义；没有新增 endpoint、业务实体、可配置项或设计外工作流。
- 行为代码和后端 create response 语义均变化，review closure 分类为 Material；不能复用 round 2 reviewer，必须进入完整 Round 3 双通道复审。

## Code Review Round 3 修复闭环

状态：REV-014～REV-016 的行为修复、定向 RED/GREEN 与完整门禁完成（2026-08-01）；material Round 4 复审待执行。

### 已核实问题与修复

- REV-014：`calendarTimeline()` 的非 terminal tick 现在使用真实 zoned local `HH:mm`，不再把分钟硬编码成 `:00`；重复标签判定继续基于最终完整 local label，terminal 保留精确 `24:00`。Lord Howe spring/fall 的半小时 transition 中间刻度分别固定为 `02:30/03:30/04:30` 与 `01:30/02:30/03:30`。
- REV-015：领域 conflict 仍只依据真实半开区间；周视图 lane cluster/复用则额外使用与 22px/44px-per-hour 一致的 30 分钟最小视觉边界。连续短档期仍为 `conflicting=false`，但会进入不同横向 lane，避免最小可读/点击高度在同一列互相遮挡。事件 `top/height` 本身恢复真实 elapsed 比例，CSS 的最小呈现高度由同一 `WEEK_EVENT_MIN_VISUAL_MINUTES` 派生；working window 不再被 22px 规则扩大。
- REV-016：新增纯 `calendarSearchParamsAfterDelete()` seam；删除成功并完成列表刷新后，仅当当前 `querySlot` 与被删除 ID 相同，才把 URL 规范化为 `{date}`（无合法日期时为空）。删除其他 slot 或当前无 slot selection 时不改变仍有效的深链。

### TDD 与定向验证

- RED：先扩展 `schedule.test.ts`；在生产 seam 尚不存在时，`npm run test:schedule` 以 `ERR_MODULE_NOT_FOUND .../calendar/urlState.ts` 真实失败，同时新测试已固定 Lord Howe 中间刻度、短档期视觉 lanes 与删除深链条件语义。
- GREEN：`npm run test:schedule` 51/51。新增断言证明 Lord Howe 23.5/24.5 小时自然日的真实半小时 tick 和 terminal；两个连续 15 分钟 slot 分配不同视觉 lane 但均不冲突；只有删除当前深链目标才生成清理后的 search params。
- 前端并行门禁通过：`npm run test:schedule` 51/51、`npm run test:settings` 6/6、`npm run test:api-client` 7/7、`npm run test:v1-hardening` 9/9、`npm run lint`、`npm run build`；Vite 只有既有大 chunk warning。
- `make generate-check` 与 `git diff --check` 通过；生成的 Go/TypeScript OpenAPI 契约无漂移。
- canonical `make check` 前两次分别在既有 `platform/store` 与 `customer` Testcontainers 测试命中 `port "5432/tcp" not found` 环境抖动；失败证据保留，随后 `go test -p=1 ./internal/platform/store/... -count=1 -parallel=1` 与 `go test -p=1 ./internal/customer/... -count=1 -parallel=1` 均通过。最终完整 `make check` exit 0，同时通过 customer/store、全部 Go 包、前后端 build/lint、全部前端专项、安全 catalog、legacy cutover、v1 ops contract 与生成物检查。
- `make check` 产生的 `scripts/lib/__pycache__` 已清理；无 staged diff。
- React router、deferred DELETE、实际 DOM rectangle/clickability 仍必须在 QA 以真实浏览器和 API trace 补足；source contract/pure seam 不冒充浏览器行为证据。

### 范围说明

- 本轮没有改变后端、公共 API、持久化或领域冲突判定，只修正批准设计内的 DST 展示、短档期呈现和删除后 URL 状态闭环。
- 生产展示与 URL 行为均发生变化，review closure 分类为 Material；不能直接进入 QA，必须完成 Round 4 独立完整复审。

## Code Review Round 4 修复闭环

状态：REV-017～REV-018 的行为修复、定向 RED/GREEN 与完整门禁完成（2026-08-01）；material Round 5 复审待执行。

### 已核实问题与修复

- REV-017：删除 mutation 完成时不再使用请求发起 render 捕获的 `querySlot/selectedDate`。由于生产入口明确挂载 `BrowserRouter`，成功分支直接读取当前 `window.location.search`，纯 helper 仅在完成时最新 `slot` 仍等于 deleted ID 时 clone 并删除该键；其余当前参数原样保留。清理调用 `setSearchParams(next, {replace:true})`，不会把失效 slot 留成可 Back 恢复的 history entry。若用户在 deferred DELETE 期间已导航到其他 slot/date，helper 返回 null，迟到结果不会覆盖 URL、notice 或焦点。
- React Router 7.18.1 契约经 Context7 官方 `/remix-run/react-router` 文档核实：`SetURLSearchParams` 第二参数透传 `NavigateOptions`，`replace:true` 执行 history replace；函数式 updater 使用 setter 闭包内 `searchParams` 的副本且不具备 React state queue 语义，因此本修复没有把函数式 updater 误当作异步完成时的全局最新 URL。
- REV-018：抽出 `layoutWeekProjections(finalVisibleProjections)`；`WeekCalendar` 先按 type filters 与 `showCancelled` 得到最终可见集合，再计算纯展示 collision lanes。显示取消项时 cancelled 与 active 一起参与真实/最小视觉边界 lane；隐藏取消项时 active 恢复 full width。projection 的 `cancelled/conflicting` 事实保持原值，领域 conflict、openings、utilization、turnaround 和后端 overlaps 完全未改。

### TDD 与定向验证

- RED：测试先引用尚未导出的 `layoutWeekProjections`，`npm run test:schedule` 以 `SyntaxError: ... does not provide an export named 'layoutWeekProjections'` 真实失败；新用例同时固定 latest params、replace history 与 cancelled final-visible layout 契约。
- GREEN：`npm run test:schedule` 52/52。真实 `createMemoryRouter` history 测试证明 replace 后 Back 返回前一页面而不恢复 deleted slot；helper 证明当前已切到 slot B 时不生成更新，并保留 `date/view` 等完成时参数。
- GREEN：active + cancelled 的同时段、部分重叠、短 endpoint-touching 三组模型均进入不同视觉 lane、`laneCount=2`、`conflicting=false`；隐藏 cancelled 后 active `laneCount=1`。
- `npm run lint`、`npm run build` 通过；Vite 只有既有大 chunk warning。
- `make generate-check`、`git diff --check` 与 `git diff --cached --check` 通过；staged diff 为空。
- canonical `make check` 本轮一次完整 exit 0：前后端 build、golangci-lint（0 issues）、全部 Go 包（含 customer/httpapi/store）、全部前端专项（schedule 52/52、settings 6/6、api-client 7/7、v1-hardening 9/9）、legacy cutover、安全 catalog、v1 ops contract 与生成物检查全部通过；只有既有 Vite 大 chunk warning。
- `make check` 产生的 `scripts/lib/__pycache__` 已清理。
- deferred 真实 DELETE、BrowserRouter Back/Forward、最终 DOM rectangles 与两个事件按钮 click target 仍列为 QA 必测；MemoryRouter/纯模型证据不冒充浏览器布局证据。

### 范围说明

- 本轮只改变删除后的 router history 编排和周视图最终可见集合的纯展示 lanes；没有改变公共 API、后端、持久化或 cancelled 领域占用语义。
- 生产 router 与展示行为发生变化，review closure 分类为 Material；必须完成 canonical 门禁和 Round 5 独立完整复审后才能进入 QA。

## Code Review Round 5 修复闭环

状态：REV-019 的行为修复、定向 RED/GREEN 与完整门禁完成（2026-08-01）；material Round 6 复审待执行。

### 已核实问题与修复

- REV-019：新增 `reconcileCalendarDeleteRefresh()` 纯异步编排 seam。DELETE 完成后先读取最新 pathname/search：已离开 `/calendar` 时不发起旧 refresh；仍在 Calendar 但当前 slot 已不是 deleted ID 时，不调用请求发起 render 捕获的 `refreshCalendarData`，只递增 `readReloadTick`，由当前 render/range effect 重载 slots（openings active 时也随 tick 重载）；只有最新上下文仍是 deleted slot 时才允许 await 原 refresh。
- 原 refresh 开始后若再发生导航，新的 range load 会按既有 generation 中止/取代旧请求；refresh 返回后 seam 再读一次最新 pathname/search，只有仍匹配 deleted ID 才允许后续 replace、notice 与 focus。M1 旧闭包不能在 M2 已经成为当前上下文后重新登记为更高 generation。
- 同轮关闭 Round 5 nit：timed `WeekSlotLayout.cancelled` 改为 `projection.cancelled`，不再与 projection/全天 fallback 形成两套事实；生产样式与领域占用语义未改变。

### TDD 与定向验证

- RED：测试先引用尚不存在的 `reconcileCalendarDeleteRefresh`，`npm run test:schedule` 以 `SyntaxError: ... does not provide an export named 'reconcileCalendarDeleteRefresh'` 真实失败。
- GREEN：`npm run test:schedule` 53/53。新增异步用例覆盖：完成时仍在 A 则原 refresh 一次并返回清理参数；完成前已到 B/M2 则原 refresh 为 0、current reload tick 为 1；原 refresh 中途导航到 B 则最终不改 URL；离开 Calendar 不执行旧 refresh 或 Calendar reload。
- cancelled lane 测试新增断言 `WeekSlotLayout.cancelled === true`；`npm run lint`、`npm run build` 通过，Vite 只有既有大 chunk warning。
- `make generate-check`、`git diff --check`、`git diff --cached --check` 通过；staged diff 为空。
- canonical `make check` 本轮一次完整 exit 0：前后端 build、golangci-lint（0 issues）、全部 Go 包（含 customer/httpapi/store）、全部前端专项（schedule 53/53、settings 6/6、api-client 7/7、v1-hardening 9/9）、legacy cutover、安全 catalog、v1 ops contract 与生成物检查全部通过；只有既有 Vite 大 chunk warning。
- `make check` 产生的 `scripts/lib/__pycache__` 已清理。
- 真实 BrowserRouter + deferred API 的两种网络响应顺序、跨月 DOM server state 与离开 Calendar 行为仍列入 QA；纯编排 seam 不冒充完整 React 挂载证据。

### 范围说明

- 本轮只改变删除成功后的 refresh orchestration 与一个重复布局事实字段；没有改变公共 API、后端、持久化、领域 conflict 或 cancelled occupancy。
- 生产异步编排行为发生变化，review closure 分类为 Material；必须完成 canonical 门禁和 Round 6 独立完整复审后才能进入 QA。

## Code Review Round 6 修复闭环

状态：REV-020 的行为修复、定向 RED/GREEN 与完整门禁完成（2026-08-01）；material Round 7 复审待执行。

### 已核实问题与修复

- REV-020：`reconcileCalendarDeleteRefresh()` 新增 `expectedRangeKey`，old `refreshOriginal` 的资格现在同时要求：仍在 `/calendar`、完成时 slot 仍为 deleted ID、完成时 effective range key 与 mutation 发起 render 的 42 日 range key 相同。
- `calendarRangeKeyForSearch()` 从完成时最新 URL `date`、账号 timezone 同步派生当月 42 日窗口的 `from:to` key；slot-only/非法 date 才回退到当前 `mainRequestRef.rangeKey`。这样即使 M2 render effect 尚未启动，`date=d2&slot=A` 也能在调用任何旧 closure 前识别 range 已变化。
- same-slot/different-range 时不调用旧 M1 refresh，而是请求 current reload，并返回 clone 后删除 slot 的 M2 search params：当前 M2 slots/openings 会重载，deleted A 的 URL 同时以既有 replace 语义清理。same-month 不同日期因有效 42 日 range 相同，继续允许安全 refresh。

### TDD 与定向验证

- RED：测试先引用尚未存在的 `calendarRangeKeyForSearch`，`npm run test:schedule` 以 `SyntaxError: ... does not provide an export named 'calendarRangeKeyForSearch'` 真实失败。
- GREEN：`npm run test:schedule` 53/53。用例证明 8 月任意 date 派生同一 range、9 月派生不同 range；A/d1(M1)→A/d2(M2) 时 `originalRefreshes` 不增加、`currentReloads` 增加、返回参数保留 M2 date 并删除 A。
- `npm run lint`、`npm run build` 通过；Vite 只有既有大 chunk warning。
- `make generate-check`、`git diff --check`、`git diff --cached --check` 通过；staged diff 为空。
- canonical `make check` 本轮一次完整 exit 0：前后端 build、golangci-lint（0 issues）、全部 Go 包（含 customer/httpapi/store）、全部前端专项（schedule 53/53、settings 6/6、api-client 7/7、v1-hardening 9/9）、legacy cutover、安全 catalog、v1 ops contract 与生成物检查全部通过；只有既有 Vite 大 chunk warning。
- `make check` 产生的 `scripts/lib/__pycache__` 已清理。
- 真实 BrowserRouter + deferred API、跨月最终 DOM/server data 仍在 QA 复核；纯 range/reconcile seam 不冒充完整 React 挂载证据。

### 范围说明

- 本轮只收紧 old refresh 的 effective range 资格并增加纯 range-key 派生；没有改变公共 API、后端、持久化或领域语义。
- 生产异步编排行为发生变化，review closure 分类为 Material；必须完成 canonical 门禁和 Round 7 独立完整复审后才能进入 QA。

## Code Review Round 7 修复闭环

状态：REV-021 的行为修复、定向 RED/GREEN 与完整门禁完成（2026-08-01）；material Round 8 复审待执行。

### 已核实问题与修复

- REV-021：`CalendarPage` 新增 `latestAccountTimezoneRef`，完成时 URL range 不再使用 DELETE 发起 render 捕获的旧 `accountTimezone`，而是用完成时最新账号时区与最新 URL 派生 42 日 range key。若同一 `date+slot` 在 pending DELETE 期间由 TZ1 切换到 TZ2，old TZ1 refresh 不再具备执行资格，只请求当前 render/range 重载。
- latest ref 在已 commit render 中由 `useLayoutEffect` 同步；`loadSettings()` 发起时先同步回落到最新 `shellTimezone`，Settings GET 成功后又在 `setSettings(result)` 之前同步写入 `result.timezone`。这同时覆盖 Settings 与 DELETE promise 在同一 tick 完成、React 尚未 commit 新 render 的竞态。
- `loadSettings` 依赖补入 `shellTimezone`；本轮没有改变公共 API、后端、持久化、删除协议或 range 定义。

### TDD 与定向验证

- RED：生产代码修改前，`npm run test:schedule` 为 52/53；新增 source-contract 回归因缺少 `const latestAccountTimezoneRef = useRef(accountTimezone)` 真实失败。纯 range/reconcile 用例同时证明同一 date 在 `Asia/Shanghai` 与 `America/New_York` 的 key 不同，TZ1→TZ2 时 old refresh 为 0、current reload 为 1，最新 date 保留且 deleted slot 被清理。
- GREEN：`npm run test:schedule` 53/53；`npm run lint` 与 `npm run build` 通过，Vite 只有既有大 chunk warning。
- `make generate-check`、`git diff --check`、`git diff --cached --check` 通过；staged diff 为空。
- 第一次 canonical `make check` 的 build/lint 通过，但 Go Testcontainers 在 `customer` 与 `order` 各出现一次仓库已知随机基础设施错误 `port "5432/tcp" not found`，真实保留为失败记录；`go test -p=1 ./internal/customer ./internal/order -count=1 -parallel=1` 随后通过。
- 第二次 canonical `make check` 完整 exit 0：前后端 build、golangci-lint（0 issues）、全部 Go 包（含 customer/order/httpapi/store）、全部前端专项（schedule 53/53、settings 6/6、api-client 7/7、v1-hardening 9/9）、legacy cutover、安全 catalog、v1 ops contract 与生成物检查全部通过；只有既有 Vite 大 chunk warning。
- 同一 date/slot、Settings retry 令 TZ1→TZ2、DELETE promise 与 Settings promise 同 tick 完成的真实 BrowserRouter/API 时序仍列为 QA 必测；纯 helper 与 source-contract 证据不冒充完整 React 浏览器证据。

### 范围说明

- 本轮只修正完成时 latest timezone identity；没有改变批准设计、后端能力、公开契约或领域语义。
- 生产异步编排行为发生变化，review closure 分类为 Material；必须完成 Round 8 独立完整复审后才能进入 QA。

## QA Round 1 失败与 qa-fix 闭环

状态：QA-015 的 URL/selected detail 时区竞态已完成 RED/GREEN 与真实 BrowserRouter/API 复测（2026-08-01）；代码 diff 已变化，等待 Round 9 独立复审，复审通过后必须重新进入 QA。

### 真实失败

- Settings 初读失败时 Calendar 以 shell `Asia/Shanghai` 保持 CRUD；手动 retry 与 DELETE 同时 pending，服务端 Settings 切到 `America/New_York`，先释放 Settings、后释放 DELETE。
- 删除完成后旧 TZ1 refresh 没有覆盖 TZ2，slot 已从服务端与 DOM 删除，URL 和焦点也保留 `2026-08-05`；但 slot lookup 已按 TZ2 把 selected detail 移到 `8 月 4 日`，删除完成只 replace URL / focus，没有把 selected date/month 同步回最新 URL，形成地址/焦点与详情不一致。
- 失败证据：`qa-evidence/timezone-delete-settings-first-failure.json`、`timezone-delete-settings-first-dom.json`。

### 窄修复

- `calendarDateAfterDelete()` 统一删除完成后的日期选择：最新 URL 含合法 `date` 时以它为准；slot-only 或非法 date 时按 deleted slot start 与完成时 latest timezone 还原本地日期。
- `confirmDelete()` 在清理当前 deep-link slot 时同时同步 `selectedDate`、`month`、replace search params、date-bound notice 与最终 focus；删除 non-current slot、离开 Calendar、不同 range/current reload、公共 API 和领域删除语义均未改变。

### TDD 与验证

- RED：`schedule.test.ts` 先引用尚未导出的 `calendarDateAfterDelete`，Node test 真实以 missing export 失败；新增用例固定 URL date 优先、slot-only 与非法 date 使用 latest timezone 本地日期。
- GREEN：schedule 54/54；settings 6/6；api-client 7/7；v1-hardening 9/9；frontend lint/build 通过，Vite 仅有既有大 chunk warning。
- 真实复测：同一 Settings-first/DELETE-second 时序下，最终 URL `?date=2026-08-05`、详情 `8 月 5 日`、焦点 `aria-label=2026-08-05`、时区 `America/New_York`，deleted hold 不在 DOM/服务端；DELETE 后只有 TZ2 `2026-07-27T04:00:00Z → 2026-09-07T04:00:00Z` range GET，没有旧 TZ1 refresh。
- 通过证据：`qa-evidence/timezone-delete-settings-first-pass.json`、`timezone-delete-settings-first-pass-dom.json`。
- `make generate-check`、`git diff --check`、`git diff --cached --check` 通过；staged diff 为空。

### 范围说明

- 本轮只修复 QA-015 已证明失败的删除完成 selection reconciliation，没有处理 Round 8 的 Settings GET latest-response 建议、ARIA grid hierarchy 或其他非阻塞项。
- qa-fix 改变生产行为，必须按 CodeStable gate 完成 Round 9 独立代码审查；不能直接把 Round 1 QA 改写为 passed，也不能进入 acceptance。

## Code Review Round 9 / QA Round 1 第二轮修复闭环

状态：REV-022 与 QA-015 的 canonical URL、latest-timezone range、Settings/DELETE 同 turn 及迟到 slot lookup 闭环已完成 RED/GREEN 和真实浏览器矩阵（2026-08-01）；代码 diff 再次发生 Material 变化，等待 Round 10 独立复审。

### 真实失败与根因

- Round 9 指出第一轮 qa-fix 单独推导 UI 日期，但 replace URL 和 range identity 仍不是同一个 canonical state：slot-only 会留下空 URL，非法 `date` 会保留非法值，reload/Back/Forward 无法恢复可见 selection。
- 首轮 canonical `{searchParams,date,rangeKey}` 落地后，真实同 turn fixture 又证明仅延迟 reload 不足：DELETE continuation 可能先于 Settings continuation 恢复，二次 canonical 读取仍会使用旧 `Asia/Shanghai`，最终页面已切到 `America/New_York` 但 URL/详情落在 `2026-08-05`。
- 等待 Settings settle 后继续复测，React 还可能在 router replace commit 前用“新时区 + 旧 slot/date query”重启 slot lookup 或旧非法 date effect，造成最终 URL 正确但残留 404/非法 date alert，并丢失稳定焦点。

### 窄修复

- `calendarDeleteCompletion()` 原子返回 `{searchParams,date,rangeKey}`：有效 date 保留；slot-only/非法 date 按 deleted slot `start_at` 与完成时 latest timezone 推导 effective date，并把它写回 replacement params；range key 只从同一 effective date + timezone 派生。
- Calendar reload 在 Settings GET pending 时不再发旧 timezone 请求；`waitForSettingsIdle()` 用当前 request 的 waiter 队列等待 latest Settings request settle，finally 在更新 latest timezone、安排 current reload 后统一唤醒。DELETE 先完成时不会发旧范围，Settings 成功或失败后才进行二次 canonical 读取。
- DELETE 成功后以 `deletedSlotIDRef` 标记当前目标；router replace 完成前，同一 ID 的 lookup 不会因 timezone render 重启，旧 lookup 同步 abort/generation advance。旧非法 date effect 在同一短窗口也跳过；query 离开目标后 ref 立即清空，后续真实失效链接仍保留正常 404 语义。
- 删除完成继续使用同一个 completion 同步 selected date、month、replace URL、date-bound notice 与最终 focus；没有改变公共 API、后端、删除领域语义或 Settings 契约。

### TDD、自动化与真实浏览器证据

- RED：`schedule.test.ts` 先新增 canonical completion、Settings idle waiter、deleted slot lookup/date-effect guard 的 source contract；各生产 seam 落地前均真实得到 53/54 失败。初次仅延迟 reload 的版本在真实 BrowserRouter/API 同 turn 场景复现 URL `2026-08-05` 与 New York 本地日期不一致；随后又复现 canonical 正确但残留 slot 404，以及非法 date alert/focus 被旧 effect 覆盖。
- GREEN：`schedule.test.ts` 最终 54/54；`npm run lint`、`npm run build` 通过，Vite 只有既有大 chunk warning。
- 真实浏览器矩阵五组全部通过：slot-only 两种同 turn release order、非法 `date+slot`、Settings 先/DELETE 后、DELETE 先/Settings 后。slot-only/非法 date 最终统一为 `/calendar?date=2026-08-04`、详情 `8 月 4 日`、focus `aria-label=2026-08-04`；合法 date 保留 `/calendar?date=2026-08-05` 并同步详情/focus。
- 所有场景最终 timezone 为 `America/New_York`、deleted hold 在 DOM/服务端均不存在、alert 为空；release 后 range GET 只出现 `2026-07-27T04:00:00Z → 2026-09-07T04:00:00Z`，旧 Shanghai `2026-07-26T16:00:00Z → 2026-09-06T16:00:00Z` 次数为 0。DELETE 先完成时，Settings release 前 range GET 为 0，证明没有废弃 refresh。
- 证据：`qa-evidence/rev-022-delete-canonical-browser-matrix.json`。既有失败 trace 保留，不改写历史。
- 定向前端门禁最终通过：schedule 54/54、settings 6/6、api-client 7/7、v1-hardening 9/9、lint、build；`make generate-check` 无生成物漂移。
- canonical `make check` 一次完整 exit 0：前后端 build、golangci-lint（0 issues）、全部 Go 包、全部前端专项、legacy cutover、安全 catalog、v1 ops contract 与生成物检查均通过；只有既有 Vite 大 chunk warning。`git diff --check`、`git diff --cached --check` 通过且 staged 为空。

### 范围说明

- 本轮只收敛 REV-022 / QA-015 已证明的删除完成 canonical state、Settings settle 顺序与被删除 deep-link 的迟到 effect；没有处理 Round 8 nit、ARIA grid suggestion 或任何方案外优化。
- 修复改变生产异步编排，review closure 分类为 Material；必须先完成 canonical 门禁和 Round 10 完整独立复审，review passed 后才能重新进入 QA Round 2。

## Code Review Round 10 修复闭环

状态：REV-023 的 Settings 无界等待、迟到时区与卸载收敛已完成 RED/GREEN 和真实浏览器回归（2026-08-01）；生产异步编排再次发生 Material 变化，等待 Round 11 独立复审。

### 已核实问题与窄修复

- Round 10 独立 reviewer 确认：DELETE 204 已成功后，`refreshCalendarAfterDelete()` 会无界等待任意 active Settings GET；普通 fetch 与 401 refresh 都没有 deadline，弹窗因此可能永久保持 `deleting`，违反 A13“Settings 失败不阻塞 CRUD”。
- 新增 `waitForCalendarSettingsIdle()` 可执行协调 seam，结果只有 `idle | timeout | cancelled`；默认上限 1000ms，timeout/abort 都会注销 listener，组件卸载会 abort 协调 controller、清理 deferred context 并 discard continuation。
- DELETE 成功后立即从当前 ready slots 与 openings slots 乐观移除服务端已删除目标；即使 Settings 永不 settle，1 秒后仍可关闭弹窗、replace canonical URL、显示成功通知并聚焦稳定日期控件。DELETE 不会重发；timeout 后仍保留 `reloadAfterSettingsRef`，但不发送旧时区 range。
- slot-only/非法 date 在 fallback 时区下先形成可恢复 URL；若 Settings 随后返回新时区，`calendarDeleteCompletionAfterSettings()` 只在当前 search 仍精确等于 fallback search 时重算 effective date。用户导航/改 query 会清除 deferred context，迟到结果不能覆盖最新页面。
- late-reconcile callback 通过稳定 ref 交给 `loadSettings()`，避免 URL replace 改变 callback identity并额外重启 Settings effect；真实请求日志确认 fallback replace 后没有额外 Settings GET。

### TDD、自动化与真实浏览器证据

- RED：测试先引用尚不存在的 `deleteCoordination.ts`，Node 以 `ERR_MODULE_NOT_FOUND` 真实失败；不是 source regex 假 RED。
- GREEN：schedule 55/55。可执行用例真实覆盖 Settings timeout 后 listener 清理、正常 idle 唤醒、AbortSignal cancellation，以及迟到新时区只在 search identity 匹配时重算日期；source contract 同步固定乐观移除、稳定 callback ref 与卸载 abort wiring。
- `npm run lint` 无告警；`npm run build` 通过，只有既有 Vite 大 chunk warning。
- 真实 BrowserRouter/API non-settling 场景：Settings 保持 pending 时，确认删除后 1250ms 观察到弹窗关闭、URL `/calendar?date=2026-08-05`、focus `2026-08-05`、hold DOM/server 均不存在、alert 为空、DELETE 恰好 1 次、DELETE 后旧 Shanghai range 0 次、URL replace 后额外 Settings GET 0 次。
- 随后 release Settings(TZ2)：最终 URL `/calendar?date=2026-08-04`、详情 `8 月 4 日`、focus `2026-08-04`、timezone `America/New_York`；New York range 恰好 1 次，DELETE 后旧 Shanghai range 0 次、deleted-slot lookup 0 次。
- 导航/unmount：协调等待期间离开到 `/customers`，release Settings 后仍停在 `/customers`；DELETE 恰好 1 次，DELETE 后 Calendar range/slot lookup 0 次。
- 401 refresh stall：Settings 两次 401 进入共享 token refresh，fixture 保持 refresh 永不 settle；DELETE 后 1250ms 弹窗仍正常关闭，hold DOM/server 不存在、focus 稳定、DELETE 恰好 1 次、Calendar range GET 0 次，且服务端仍记录 pending auth refresh。
- 证据：`qa-evidence/rev-023-delete-settings-liveness-browser-matrix.json`。临时 fixture 的 401/deferred-refresh 控制仅用于 QA，不进入仓库，最终按 QA 清理协议删除。
- `make generate-check`、`git diff --check`、`git diff --cached --check` 通过且 staged 为空。canonical `make check` 一次完整 exit 0：frontend build/lint、Go build、golangci-lint（0 issues）、全部 Go 包串行测试、全部前端专项（schedule 55/55、settings 6/6、api-client 7/7、v1-hardening 9/9）、legacy cutover、安全 catalog、v1 ops 与生成物检查全部通过；只有既有 Vite 大 chunk warning。门禁产生的 `scripts/lib/__pycache__` 已清理。

### 范围说明

- 本轮只关闭 REV-023 / A13 的 mutation completion liveness；没有处理 Round 10 的 Python cache nit、Round 8 ARIA hierarchy suggestion 或其他方案外优化。
- 生产异步编排与新增 runtime seam 属于 Material；必须重跑 canonical gates 并完成 Round 11 独立复审，review passed 后才能进入 QA Round 2。
