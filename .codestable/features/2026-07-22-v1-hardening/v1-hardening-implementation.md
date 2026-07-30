---
doc_type: feature-implementation
feature: 2026-07-22-v1-hardening
status: in-progress
current_step: STEP-007
updated: 2026-07-28
---

# v1-hardening 实现记录

## 1. 第一性原则 Pre-pass

- 外部行为：完成已批准的页面状态、375px 三条轻路径、运维安全网与 V1 证据闭环。
- 不可破约束：零 HTTP API、OpenAPI、schema、领域状态机与认证模型扩张；401、stale、exact-generation 与 physical target identity 语义不回退。
- 最小充分改动：严格按 STEP-001～007 推进；新 UI 只落既定组件/样式，新运维编排只落既定脚本入口。
- 必须不写：移动完整 CRUD、认证重做、TLS/云厂商集成、同名工具 shim、伪造 evidence 与方案外重构。

## 2. 基线预检

- `cd frontend && npm run build`：通过；保留既有 555.35 kB chunk warning。
- `cd frontend && npm run test:api-client`：3/3 通过。
- `cd frontend && npm run lint`：通过。
- `cd backend && go test ./cmd/server -count=1`：通过。
- 基线风险：无阻塞红灯；chunk warning 按 approved design 明确不扩为拆包工作。

## 3. STEP-001 状态与证据契约骨架

退出信号：页面读取状态可独立观察；A1～A27 唯一 pending；148 个 frozen ops case 完整物化；canonical artifact 与长期测试入口固定。

### 变更

- 新增 `PageReadState<T>`、`PageReadNotice` 与 `pageReadPresentation`，用 discriminated union 固定 unauthorized → loading/error → empty → ready(current|stale) 语义。
- 新增 `StateNotice`；loading 使用 `role=status`，error/refresh-error 使用 `role=alert`，只有 retryable/refresh-error 暴露重试。
- 新增 `v1-hardening-regression.yaml`，包含 AppShell、10 个正式 screen route、redirect 的状态矩阵以及 A1～A27 唯一 pending record。
- 新增 `v1-hardening-ops-case-catalog.yaml`，从 approved design D8 冻结 inventory 展开 148 个唯一 case；scenario membership 为 A18/A19/A20/A21/A22 = 66/2/41/2/39。
- 新增 catalog contract validator 与 shell test；validator 对 frozen expected、schema、replay fixture/action 和 membership 做机械核验，并拒绝 missing/unknown/duplicate/scenario/oracle/expected/placeholder 负例。
- `npm run test:v1-hardening` 与 `scripts/test-v1-ops-contract.sh` 已接入 Makefile `test`。
- 通过官方 gate 生成 canonical `v1-hardening-dod-contract-results.json`，`status=passed`，输入 digest 指向当前 approved design。

### TDD 证据

- 页面状态 RED：`npm run test:v1-hardening` 因 `pageReadState.ts` 不存在真实失败（`ERR_MODULE_NOT_FOUND`）。
- 页面状态 GREEN/VERIFY：实现后定向测试 1/1、build 与 lint 通过。
- ops contract RED：`./scripts/test-v1-ops-contract.sh` 因入口不存在真实失败。
- ops contract GREEN/VERIFY：148 cases 通过，8 组负例全部被拒绝；`bash -n scripts/*.sh` 通过。
- REFACTOR：无；当前接口已是 approved design 的最小呈现/判定分离。
- 需求迭代：无。

### 完整验证与清洁度

- `make check`：通过；真实日志含 `npm run test:v1-hardening` 与 `./scripts/test-v1-ops-contract.sh`，见 `evidence/commands/CMD-001.log`。
- YAML：checklist、regression、ops catalog 均通过解析。
- DoD contract gate：passed，无 blocking/warnings。
- `git diff --check`：通过。
- 新增文件未命中 `console.log/error`、`debugger`、`TODO/FIXME/XXX` 或 `fmt.Print`；无 secret、PII、备份半包或 Docker 残留。

## 4. STEP-002 页面状态接入

退出信号：正式 route matrix 中首次加载、成功空态、terminal/retryable error、stale refresh 与 401 分流互斥且可观察；不存在 error 冒充 empty、stale 冒充 current 或未处理 401。

### 变更

- `AppShell`、Dashboard、Customers、CustomerDetail、Orders、Calendar、Packages、Reminders 与 Settings 均接入 `PageReadState` / `StateNotice`；Login 与 CustomerNew 没有页面级 initial read，保持其 action state，CustomerPicker 子区留在 STEP-003 收口。
- initial failure 不再和 empty/ready 同时显示；successful empty 只由成功读取结果显式产生。
- refresh/load-more failure 在已有数据时保留数据并转为 `ready(stale)`，附带 refresh-error 与真实 retry；filter/route identity 变化时清除不匹配旧结果并进入 loading。
- 受保护请求 401 由 API client 先清 token，再交页面导航至 `/login`；业务页不继续渲染旧成功数据。
- Dashboard 的合法零值卡保持 ready；CustomerDetail 404 与 Calendar 缺 timezone 使用 terminal error，不暴露伪 retry。
- Calendar initial error 不再渲染成功网格，stale refresh failure 则保留网格；Settings stale form 禁止保存未知旧值；Packages/Orders load-more failure 保留现有列表；AppShell timezone refresh failure 保留旧 timezone 为 stale。

### TDD 与回归证据

- RED：新增状态转换测试首先因 `pageReadState.ts` 未导出 `beginPageRead` 真实失败。
- GREEN：实现 `beginPageRead`、`completePageRead` 与 `failPageRead` 后，`test:v1-hardening` 3/3 通过，覆盖 fixed-priority presentation、refresh failure 后 current→stale、initial error 与 successful empty 互斥。
- 401 回归：`test:api-client` 新增“protected 401 clears the token before page navigation”，4/4 通过。
- 既有 data-export source-shape regression 因 Settings 从 `loading` boolean 迁移到 `presentation.showReadyData` 首次失败；测试更新为等价的新状态契约后 `test:data-export` 8/8 通过，没有跳过或弱化断言。
- REFACTOR：无；页面只接入 STEP-001 的批准状态契约，没有扩大 API、schema、认证或领域范围。
- 需求迭代：无。

### 验证与清洁度

- fresh verification：`npm run lint`、`npm run test:v1-hardening`、`npm run test:api-client`、`npm run test:schedule`、`npm run build` 全部通过；schedule 32/32，通过构建仍只有 approved design 接受的约 557 kB chunk warning。
- 前一轮定向验证另含 `test:data-export` 8/8、`test:telegram-digest` 4/4，均通过。
- `git diff --check` 通过；STEP-002 改动文件未命中 `console.log/error`、`debugger`、`TODO/FIXME/XXX`，无注释掉旧代码、无方案外 API/schema/auth 变化。

## 5. STEP-003 客户移动垂直切片

退出信号：Customers 的同一查询结果在 375/768 显示 cards、769/desktop 显示 table；搜索、详情与备注动作保持同一页面上下文；QuickNote/NotesPanel 的成功失败、IME、重复提交与焦点语义稳定；CustomerPicker 空 options 不产生非法 active option。

### 变更

- 新增 `CustomerResult` 移动卡片投影；CustomersPage 仍是唯一查询、筛选、`items/total` 与 reload owner。桌面表格与移动卡片都消费同一 `items`，卡片组件不导入或调用 `listCustomers`，没有第二次列表请求。
- 375/768 只显示移动卡片，769 及以上只显示桌面表格；两种投影共享客户状态/日期呈现，卡片包含头像、昵称、建档日期、渠道、约单数、最近拍摄、状态、打开详情与 QuickNote。
- QuickNote 新增成功 `role=status`、失败 `role=alert`、输入保留、同步 submission gate、IME Enter 抑制与 Escape 取消后焦点归还；NotesPanel 复用同一键盘动作与 submission gate，并新增成功/失败可访问反馈。
- CustomerPicker 的 active index 改为 `number | null`；空 options 的 ArrowDown/ArrowUp 保持 null，只有合法 option 才生成 `aria-activedescendant`。Escape、Tab 与 focus 离开时关闭并清 active；option 获得稳定 id；候选读取错误可重试，401 不渲染错误残影。
- 新移动规则集中在 `v1-hardening.css`；卡片无固定内容高度，长昵称/事实值可换行，移动详情与备注核心动作最小 44 CSS px，QuickNote 输入使用可收缩 grid，避免 375px 横向溢出。

### TDD 证据

- 双投影 RED：新增“one page-owned query across 768/769”测试后，因 `CustomerResult.tsx` 不存在真实失败（`ENOENT`）；实现卡片、页面挂载与 CSS 边界后 GREEN。
- 键盘 RED：导入 `noteInputAction` 后因 `noteInteraction.ts` 不存在真实失败；实现 Enter/IME/Shift+Enter/Escape 分类并接入 QuickNote/NotesPanel 后 GREEN。
- 防重 RED：测试要求 `createNoteSubmitGate` 时因缺少导出真实失败；实现同步 gate 并替换仅依赖异步 React state 的守护后 GREEN，连续 start 在 settle 前被拒绝。
- Picker RED：测试要求 active-descendant/index/close 模型时因缺少导出真实失败；实现并接入后 GREEN，空 options 的 next/previous 均为 null。
- VERIFY：`test:v1-hardening` 7/7 通过；build 与 lint 通过且无 warning。Fast Refresh lint 首次指出组件文件混出非组件导出，随后把共享展示函数纯搬迁到 `customerResultModel.ts`，重跑全绿。
- 需求迭代：无；未改变 API/OpenAPI/schema/auth/领域状态机。

### 本地浏览器证据

- 使用本地 compose 数据与 Vite，在临时可恢复账号 hash 下验证；原 hash 只保存在受控 shell 进程内，验证后已恢复。唯一合成备注按固定前缀删除（`DELETE 1`）；新建 app container 与 avatar volume 已删除，原 postgres container 回到 `exited`。
- 375×812：`innerWidth=375`，`body/document scrollWidth=360`，10 张 cards 可见、desktop table=`display:none`；首个详情动作高 49px、记备注动作高 44px，无整页横向 overflow。
- 768：cards=10、table=0；769 与 1280：cards=0、table=1；同一页面显示 10/10 条结果。
- QuickNote：Escape 后 active element 为文本“记备注”的 BUTTON，草稿输入关闭；真实保存后 DOM 出现 `role=status` 的“备注已保存”，列表/筛选上下文未丢失。
- CustomerPicker：空结果时 ArrowDown 后 `aria-activedescendant=null`、expanded=true；Tab 后 listbox=0、expanded=false、active descendant=null，焦点自然前移到下一 SELECT。
- 该次 STEP-003 会话未取得 text zoom；后续 STEP-004 补证已按 design D4 允许的 32px 根字号等价方法完成，方法与指标见第 6 节和 canonical viewport metadata。

### 完整验证与清洁度

- `npm run lint`、`test:v1-hardening` 7/7、`test:api-client` 4/4、`test:schedule` 32/32、`test:data-export` 8/8、`test:telegram-digest` 4/4、`npm run build` 全部通过。
- 构建仅保留 approved design 已接受的约 560.60 kB chunk warning；不扩为方案外拆包。
- `git diff --check` 通过；STEP-003 文件未命中 `console.log/error`、`debugger`、`TODO/FIXME/XXX`，无 secret、PII、临时 Docker 资源或合成备注残留。

## 6. STEP-004 档期与 shell 移动收口

退出信号：375px 下 Customers/Calendar 直接可达，42 日网格与 drawer 无整页横向溢出，移动写入口不可见且不可聚焦，核心触控区与长文本/密集内容稳定；200% 字体可滚达证据终态成立。

### 已完成实现

- `desktop-schedule-actions` 在 ≤768px 明确 `display:none`；Calendar 的新建、编辑、删除与“在这天加档期”全部挂在三个既有 desktop-only 容器内，移动 DOM 不存在可聚焦写入口。
- 移动 `calendar-content`、`cal-head/grid/cell`、drawer/body 与 slot entry 全部允许收缩；日历固定 `repeat(7,minmax(0,1fr))`，避免七列 intrinsic width 撑开页面。
- 密集日点阵允许换行并限制在 cell 内；drawer 限制 `max-width:100%`、隐藏横向溢出、内部纵向滚动，并为 footer 加 safe-area bottom padding。
- slot 主文案、meta、冲突提示与删除提示使用 `overflow-wrap:anywhere`，长客户昵称、长备注与警示不再撑破 drawer。
- 移动粗指针下 bottom-nav、切月按钮与 drawer 关闭按钮最小 44×44 CSS px；现有 bottom-nav 保持 Customers/Calendar 直接入口、48px 高度与 safe-area。

### TDD 与验证

- RED：新增 mobile calendar bounded/read-only contract 后，因 `v1-hardening.css` 尚无 desktop action、long-content 与 44px hardening 规则真实失败。
- GREEN：补充局部 hardening CSS 后 `test:v1-hardening` 8/8 通过；新增 shell direct-entry regression 后 9/9 通过。
- 既有 `test:schedule` 32/32 通过，其中 `month grid starts Monday and always contains 42 days` 固定 42 日网格，drawer/dialog 互斥与焦点恢复模型继续通过。
- `npm run build` 与 lint 通过；构建只有既有约 560.60 kB chunk warning；`git diff --check` 通过。
- 清洁度：新增规则/测试未命中 debug 输出、TODO/FIXME/XXX；未修改 schedule journal、幂等编排、API/schema 或桌面写流程。

### 浏览器证据

- 2026-07-23 新浏览器会话已完成 375x812 的 Calendar 主体矩阵：底部导航中 Customers/Calendar 直接可达；月历固定 42 格，前后切月、从 6 月回今天、选日和空日均通过；合成密集日显示 6 条档期和 6 条冲突档期，drawer 同时呈现跨日、长客户名、长套系/备注、归档客户和取消订单警示。
- 375px DOM 指标：`body/root scrollWidth=clientWidth=375`，日历宽 351px、七列各 49px；dense drawer `clientWidth=scrollWidth=375`，drawer body `clientWidth=scrollWidth=360`、`clientHeight=678`、`scrollHeight=1116`、`overflow-y:auto`，长文本与内部滚动未造成整页横向溢出。
- 移动写入口的全部祖先链均命中 `display:none`，矩形为 0；真实 Tab 顺序只在 drawer 顶部/底部两个“关闭”按钮间循环，新建、查看订单、编辑、删除和“在这天加档期”均未进入焦点环。底部七个直接导航项实测约 51.28x51.27 CSS px。
- current/initial error + retry 已补齐：在 42 格、已有 ready 数据的 7 月页停止隔离后端，再切换到 8 月触发首次读取失败；页面显示 `role=alert` 的“请求失败（502）”和一个真实“重试”按钮，网格不可见、cell count=0，没有把失败冒充 empty/ready。重启后端并点击页面“重试”后，8 月恢复 42 格、alert=0、`aria-busy=false`。
- stale refresh error + retry 已补齐：使用仅作用于隔离环境的透明代理，在 UI 真实删除合成档期成功（后端 DELETE 204）后，只让随后的同范围 GET 返回一次 502。页面保留 7 月 42 格与旧的“2026-07-24，1 条档期”投影，同时显示 `role=alert` 的“合成刷新失败”和真实“重试”，证明 refresh failure 是 stale 而非 error/empty。点击“重试”后真实 GET 200，alert 清零，投影更新为“2026-07-24，0 条档期”。
- 补证使用 Chrome DevTools 375×812 device toolbar，实测 `(pointer:coarse)=true`、`(hover:none)=true`；Customers 详情/备注、Calendar 日格和底部导航核心触控区均达到至少 44 CSS px，两个页面都没有整页横向溢出。
- 200% 字体证据采用 design D4 明确允许的等价方法：仅在隔离 localhost response proxy 返回的 HTML 中注入 `html { font-size:32px!important }`，把根字号从 16px 提升到 32px；仓库 source 与 production bundle 未修改。证据没有宣称这是 Chrome 原生页面 zoom，也没有把截图缩放冒充字体放大。
- 32px 根字号下 Customers 的详情、记备注、输入焦点与 Escape 焦点归还均可完成；Calendar 保持 42 日、移动写入口不可见且不可聚焦；Customers/Calendar 直接入口仍可见，两个页面 `scrollWidth=clientWidth=375`。
- canonical 证据为 `evidence/browser/viewport-metadata.json`、`A08-calendar-375-coarse.png`、`A08-calendar-375-textzoom200.png`、`A11-customers-375-coarse.png`、`A26-customers-375-textzoom200.png`。
- 环境安全记录：仓库现有 `.env` 的 `DATABASE_URL` 存在且 host 脱敏分类为非 loopback；本轮未 source `.env`、未输出任何 URL/host/用户名/密码，也未连接该外部数据库。启动代码顺序是 `config.Load → store.MigrateUp → store.Open → EnsureDefaultAccount → HTTP ListenAndServe`；此前外部目标启动已到达 HTTP 监听并成功响应 `/me`，只能证明 `MigrateUp` 返回成功，现有本地日志无法区分 `migrate.ErrNoChange` 与实际应用过 migration。外部 schema 是否变化只能由获授权 operator 做只读核验。
- 本轮补证完全使用合成隔离环境：临时 PostgreSQL、后端、透明代理与 Vite 分别使用 15432、18081、18080、15173；没有生产客户数据。补证结束后 UI 删除的合成档期已由重试确认不存在；全部进程、容器、临时 Vite/proxy 文件和 avatar 目录均已清理，四个端口均确认空闲。机器上既有的 8080/5173 进程未触碰。
- 外部数据库 migration/DDL 只读核验仍是获授权 operator 的独立生产审计项；本轮没有读取或 `source` 仓库 `.env`，也没有连接外部数据库。它不改变 STEP-004 的本地移动退出信号，不能被上述 synthetic/browser 证据冒充完成。
- STEP-004 退出信号已满足，checklist 与 Goal ledger 已按顺序记为 `done`；acceptance checks 仍保持 `pending`。

## 7. STEP-005 平台运行 hardening

退出信号：Go/config/进程测试与 A18 的 66 个 required cases 全量执行，逐 key/parser/exit/Engine/Compose/context/pinned-host/initialized-seed 的 structured expected=observed，且输出不含 secret 或原始路径值。

### 变更

- `http.Server` 保留 5 秒 `ReadHeaderTimeout`，新增 60 秒 `IdleTimeout`；graceful shutdown 编排保持不变。
- server 初始化与配置错误改为按 operation、配置 key 和稳定错误类别输出，不再透出底层原始路径或配置值；Go 测试覆盖 timeout 构造和启动错误脱敏。
- 新增 `production-preflight.sh` 与公共 shell helper：strict dotenv parser 只把 RHS 当数据，不 `source`/`eval`；覆盖 binary、compose-managed-db、compose-external-db 三种模式、seed/TG/DB/avatar/auth 矩阵及稳定 exit 0～6。
- compose app `env_file` 改为显式 `${CRM_ENV_FILE:-.env}`；脚本把同一个 canonical env file 同时用于 interpolation 与 app env_file，并拒绝隐式/远程 Docker selector。
- local Docker context 只解析一次 absolute Unix socket，后续每次 Docker/Compose 调用都显式 pin 同一 host；同 Engine 的 context alias 不产生第二物理 target identity。

### 验证与证据

- A18 的 66 个 `OPS-PF-*` case 全部真实执行并通过；missing/unknown/duplicate 均为空。
- `bash scripts/test-production-preflight.sh`、Go server/config 定向测试、`bash -n scripts/*.sh` 与 shell common fixture 均通过。
- 缺命令反例使用最小 PATH 进入脚本自身 dependency gate，确认 exit 6、terminal stage=`dependency-check` 且 Engine calls=0；env 注入 payload 未被执行或回显。
- STEP-005 退出信号已满足，checklist 与 Goal ledger 已按顺序记为 `done`。

## 8. STEP-006 备份恢复、隔离 smoke 与 README

退出信号：A19～A22 对应的 backup/restore/lock/cleanup frozen case 全量执行；package、target identity、Engine state、staging、race、failure-stop 与分层 cleanup 的 structured observed=expected；results 顶层 pass 且 README 可执行。

### 变更

- 新增 `backup-compose.sh`、`restore-compose.sh`、`v1-ops-smoke.sh` 及公共 ops helper，完整实现 pinned Engine、physical target hash、daemon-side lock、helper fence、immutable-ID ownership 与 stale-break/race 守护。
- backup 固定生成并自校验五件套，按原 app running/exited predicate 恢复状态；失败不发布半包。
- restore 先复制 `umask 077` 私有 staging snapshot，再验证 schema、checksum、regular/no-symlink、tar traversal/link/device、typed project 和 input race；破坏后任一失败都执行 failure-stop 并证明 app 精确为 Engine exited，同时忠实记录 DB/avatar after-state envelope。
- restore helper 显式使用 root helper 用户读取安全策略固定为 0600 的 staged artifacts、替换 root-owned avatar volume entry；app/postgres 容器自身权限未放宽。
- README 补齐最短使用路径、三种生产预检轨、凭证轮换、显式 compose target、一致备份/恢复、stale lock、失败恢复、异地介质与 ECS/TLS/network/durability attestation 边界。
- `v1_ops_results.py` 对 exact stage order/set/outcome、coverage、operation/harness cleanup、lock remover、backup 五件套、restore package oracle/data envelope 和 destructive failure stopped+exited 做严格校验，并带可证明会改变输入的 self-test 负例。

### 实现中发现并修复的问题

- STEP-007 首次按公开命令执行 `./scripts/v1-ops-smoke.sh` 时发现新文件缺 executable bit，真实返回 exit 126；补齐可执行权限后从同一公开入口重跑 canonical smoke。
- production `v1_candidate_is_alone` 原来用 `docker ps -aq` 的短 ID 与完整 candidate ID 比较，会让合法 stale break 永远误判存在其他 helper；改为 `docker ps -aq --no-trunc`，runner 的资源归属查询同步使用完整 ID。
- runner 原先用内存 NUL 计算 target hash，而 production 使用字面 `\\0` bytes；现已与 production 的 canonical bytes 逐字一致，避免测试与真实锁域产生不同 identity。
- lock label/state invalid 反例的详细 mismatch 保留在 Docker JSONL；冻结 schema 的 projection 使用 `state:null`，不把 schema 禁止的异常 state 写成合法 observed。
- cleanup residual case 证明 operation cleanup failure 不能被 harness 最终清空掩盖：terminal stage=`cleanup`、exit 11，operation fail 而 harness cleanup pass。

### 全量验证

- canonical smoke：`executed=148`、`passed=148`、failed=[]；按 ID 前缀为 preflight 66、backup 27、restore 37、lock 17、cleanup 1。
- coverage：missing=[]、unknown=[]、duplicate=[]；`suite_cleanup=pass`，residual resources=[]。
- 独立 validator：`python3 scripts/v1_ops_results.py ... --self-test` 输出 `v1 ops results contract: passed (148 cases)`。
- Docker 结束态：查询 `label=com.photographer-crm.ops=true` 无任何 container 残留。
- 静态/安全回归：Python compile、`bash -n`、package self-test、common fixture 15 cases、backup/restore safety test 与 `git diff --check` 均通过。
- STEP-006 退出信号已满足，checklist 与 Goal ledger 已按顺序记为 `done`；checks 仍留待 acceptance 更新。

## 9. 下一步

STEP-007 的本地可执行部分已完成，但三个 core 场景仍缺 owner/operator 事实，因此本 step 不标 `done`，也不提前运行要求“全部 steps done”的 implementation.before_review gates。

### 聚合命令

- CMD-001 `make check`：canonical retry 通过；Go build/lint/串行 Testcontainers、前端 build/lint/全部定向测试、preflight/common/package/backup-restore/ops-contract 与 OpenAPI 漂移检查全绿。仅保留 approved design 已接受的 560.60 kB chunk warning。
- CMD-002 `npm run test:v1-hardening`：9/9 通过。
- CMD-003 `bash -n scripts/*.sh`：通过。
- CMD-004 ops contract + `docker build -t crm:v1-hardening .`：148-case positive/negative contract 通过，镜像构建通过。
- CMD-005 `./scripts/v1-ops-smoke.sh`：修正入口 executable bit 后从公开命令 fresh 重跑，148/148 通过，coverage 全空，suite cleanup 通过。
- CMD-006 `git diff --check`：规范化 Vite 生成日志的一处尾随空格后 canonical retry 通过。
- 六份日志均位于 `evidence/commands/CMD-001.log`～`CMD-006.log`，末行 `exit_code: 0`。

### 范围与清洁度

- CodeStable runtime 1.0.4 `--check --json` 为 `status=ok`，base/goal-gates/workflow-next capabilities 无缺失或 drift。
- regression route matrix 12/12 为 `pass`；A1～A27 已全部 terminal：24 pass、A23/A24/A27 blocked、0 pending。
- `api/openapi.yaml`、Go generated API 与前端 `schema.d.ts` 零 diff；没有 API/schema/auth 模型扩张。
- task code 未命中 `console.log/error`、`debugger`、`TODO/FIXME/XXX`、`fmt.Print`；`__pycache__` 已清理；Docker ops label 无残留，results 的 residual resources 为空。
- `.workflow/` 与 `install-cpamp.sh` 仍是任务外 untracked 文件，本轮没有读取、修改、stage 或删除。
- evidence 只出现配置 key 与脱敏 status，没有原始 secret、PII、外部数据库连接串或本机绝对生产路径；本轮没有读取/`source` 仓库 `.env`，也没有连接外部数据库。

### 当前三个 core 阻塞

- A23：README、CLI、synthetic backup/restore 已通过；仍缺生产 ECS mount/write/fsync/dir-sync、TLS、network、offsite-medium attestation，以及获授权 operator 对此前外部数据库启动窗口的 migration/DDL 只读审计。
- A24：H2 已批准复用 2026-07-17 owner-attested Telegram true-external transport/binding；仍缺 owner 对 fresh 同 fixture 的 customer→package→order→slot→deposit→next-day digest→dashboard 五卡逐节点 walkthrough 与 payload/调度关联证明。
- A27：reference-only export 自动化 8/8 和既有 feature acceptance 已通过；但必须在 A24 同一 fresh fixture 上复核 counts/边界，当前不能用独立测试替代。

只有 A23/A24/A27 取得真实证据并改为 `pass` 后，才能把 STEP-007 标 `done`，随后按 Goal 协议运行 scope-gate、dod-runner、evidence-pack 和独立 code review。当前不会为了进入 review/commit gate 把 blocked 改成 pass。
