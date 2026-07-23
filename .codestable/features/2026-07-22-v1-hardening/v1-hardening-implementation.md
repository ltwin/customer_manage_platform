---
doc_type: feature-implementation
feature: 2026-07-22-v1-hardening
status: in-progress
current_step: STEP-004
updated: 2026-07-23
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
- 浏览器控制面无法改变 Chrome text zoom，故本轮未伪造 200% 终态；A26 的 375+200% canonical evidence 继续保持 pending，必须在 STEP-007 用可记录 text zoom 的环境补跑后才能判 `passed`。

### 完整验证与清洁度

- `npm run lint`、`test:v1-hardening` 7/7、`test:api-client` 4/4、`test:schedule` 32/32、`test:data-export` 8/8、`test:telegram-digest` 4/4、`npm run build` 全部通过。
- 构建仅保留 approved design 已接受的约 560.60 kB chunk warning；不扩为方案外拆包。
- `git diff --check` 通过；STEP-003 文件未命中 `console.log/error`、`debugger`、`TODO/FIXME/XXX`，无 secret、PII、临时 Docker 资源或合成备注残留。

## 6. STEP-004 档期与 shell 移动收口（进行中）

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

### 尚未完成，禁止标 done

- 尚需新的浏览器会话对 Calendar 做 375px 切月/今天/选日、42 格、空/密集/冲突/长文本 drawer、current/stale error、写控件不可见/不可聚焦与页面/抽屉 scroll metrics。
- 200% text zoom 仍需可记录的真实方法；不得以截图缩放或普通 375px 结果冒充。
- 因上述浏览器证据未完成，checklist `STEP-004`、A7～A10/A26 与所有 checks 保持 `pending`，Goal ledger 不追加 STEP-004。

## 7. 下一步

恢复 STEP-004：启动同一 local synthetic fixture，完成 Calendar/browser 矩阵并清理本地资源；证据真实通过后才把 STEP-004 标 `done` 并进入 STEP-005。无需新的设计或执行授权。
