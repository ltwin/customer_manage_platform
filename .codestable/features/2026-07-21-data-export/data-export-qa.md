---
doc_type: feature-qa
feature: 2026-07-21-data-export
status: passed
tested: 2026-07-22
round: 1
---

# data-export QA 报告

## 1. Scope And Inputs

- Design：`.codestable/features/2026-07-21-data-export/data-export-design.md`，`status: approved`。
- Checklist：`.codestable/features/2026-07-21-data-export/data-export-checklist.yaml`；STEP-001～STEP-007 均为 `done`，24 个 acceptance check 在本阶段仍保持 `pending`。
- Review：`.codestable/features/2026-07-21-data-export/data-export-review.md`，round 3 `passed`；Spec Compliance 与 Code Quality 均为 passed，0 unresolved blocking/important/nit。
- Evidence pack：`.codestable/features/2026-07-21-data-export/data-export-evidence-pack.md`。
- Gate results：`.codestable/features/2026-07-21-data-export/data-export-gate-results.json`。
- DoD results：`.codestable/features/2026-07-21-data-export/data-export-dod-results.json`。
- Diff basis：以 baseline `3a02be57a80eeac1d192685825bcbeaab3fbf50d` 到当前未暂存工作区为准；QA 期间没有修改生产代码或测试代码，只新增本报告与两张隔离浏览器截图，并更新截图元数据/goal state。
- Baseline dirty files：`.workflow/`、`install-cpamp.sh` 为本 feature 范围外既有未跟踪项，未读取内容、未修改、未纳入验证或提交范围。
- Feature type：functional。它新增受保护 API、账号级 PostgreSQL 读取、JSON 附件下载与设置页交互，功能性核心路径必须有 PostgreSQL、HTTP、前端行为与真实浏览器证据。
- Core evidence gate：A1～A18 全部进入矩阵；CMD-001～004 必跑；浏览器必须覆盖成功下载并重新解析、桌面/375px/键盘、快速重复点击、Settings 失败独立可用、500/retry、401 与 response body 中断。

### 证据隔离说明

- QA 使用 fresh-reset 的 `codex-data-export-qa` PostgreSQL 容器、synthetic 单账号、显式本地数据库连接、独立 JWT secret、Telegram disabled 与临时头像目录；浏览器页面显示“尚未绑定”，导出为七类空数组/默认 Settings，不含 owner 数据。
- 恢复旧 driver 环境时发现其 10:02 留下的 Go/Vite 进程误用了工作区 `.env`，并未连接它声称的隔离容器。该轮证据立即作废：只读导出没有远端写操作；临时下载已删除；旧进程已停止；后续截图与结论全部来自显式隔离环境。报告不保存远端连接、token、账号 ID 或 payload。
- QA 结束后已删除所有本轮下载文件，停止 Go/Vite/故障注入服务，移除隔离容器，并恢复浏览器 viewport。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | A1 / DOD-QA-001 | core-functional | 全仓 build/lint/test/codegen | command | CMD-001 / CMD-002 | 全绿且生成物零漂移 | pass |
| QA-002 | A2/A4/A17 | core-functional | 命名 OpenAPI schema、七类实体/终态、稳定顺序、每表一次批量读且无 count/N+1 | codegen + PostgreSQL integration | CMD-002 / CMD-003 | exact projection/counts/query budget | pass |
| QA-003 | A3/A16 | core-functional | 空账号 defaults 与部分/完整 Settings parity | PostgreSQL + API | CMD-003 + 隔离浏览器实际下载解析 | 七个 `[]`、全零 counts、有效 Settings | pass |
| QA-004 | A5 | core-functional | 无 token 401、账号 B 不见 A、客户端无 account_id | HTTP + integration + browser | CMD-003；旋转隔离 JWT secret 后点击导出 | 401 并转 `/login`，无跨账号面 | pass |
| QA-005 | A6/A7 | core-functional | reference-only avatar 与内部/凭证状态排除 | structural scan + integration + diff | CMD-003；解析隔离下载结构键 | 公开引用可存在；禁止键/哨兵不存在 | pass |
| QA-006 | A8/A18 | core-functional | 一致 snapshot 与独立 isolation/read-only 属性 | PostgreSQL concurrency | CMD-003 | 旧 snapshot/下次新值；repeatable read/on；无写面 | pass |
| QA-007 | A9 | core-functional | 所有发送前失败、取消、headers 后 writer | fault + production middleware HTTP | CMD-003 | 发送前无附件 headers；发送后不写第二封套 | pass |
| QA-008 | A10/A11 | core-functional | headers、文件名、Content-Length、稳定读、无写 | HTTP + integration | CMD-003 + 浏览器下载 | 安全附件；数组稳定；只允许时间/文件名变化 | pass |
| QA-009 | A12 | core-functional | MIME/filename/ApiError/blob rejection | frontend behavior | CMD-004 | 严格 media type/whole filename；读取拒绝无结果 | pass |
| QA-010 | A13 | core-functional | 成功、500/retry、401、Settings failure、body interruption、快速重复 | browser + frontend test | 隔离页面操作 + CMD-004 | 单次下载/revoke；失败可重试且无坏文件；401 登录 | pass |
| QA-011 | A14 | core-functional | 1280、375、键盘、PII/头像文案 | browser screenshot + measurement | 1280×900、375×812、Enter | 无横向溢出；文案常驻；按钮可键盘触发 | pass |
| QA-012 | A15 | supporting | 无 import/restore/zip/async/history/temp/persistence，未知 API 404 | diff + route + cleanliness | CMD-001 / CMD-003 / diff scan | 显式不做项不进入 diff | pass |
| QA-013 | review focus | supporting | active/canceled request × wrapped canceled/deadline；Clock 顺序；全字段 route/settings parity | unit + integration | CMD-003 | round 3 修复保持绿色 | pass |
| QA-014 | review residual | supporting | 同步内存、headers 后断连、当前部署头像引用、typed-nil/partial writer | evidence review | design/review/evidence + browser interruption | 核心行为验证；非核心边界保留风险 | pass |

## 3. Command Results

- `make check` → 初次 exit 2：build、lint、全仓 Go 测试与全部前端测试均通过；最后 `generate-check` 把本 feature 尚未 commit 的 Go/TS 生成物相对 `HEAD` 的预期差异当成 drift。没有生成器二次改动，也没有生产失败。
- `GIT_INDEX_FILE=<temporary> make check`（临时 index 先以当前两份生成物作为比较基线）→ exit 0：完整 build、lint、串行全仓 Go、全部前端脚本与 codegen drift 均通过；真实 Git index 在前后都为空。该方法与 implementation/review 已记录的 canonical runner 一致，若生成器改变当前文件仍会由 `git diff --exit-code` 失败。
- `GIT_INDEX_FILE=<temporary> make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` → exit 0；命名 `ExportDocument` / `ExportCounts` 与双端生成物零漂移。
- `cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` → exit 0：store 19.512s、dataexport 7.501s、httpapi 24.464s、server 1.851s。
- `cd frontend && npm run test:data-export && npm run build` → exit 0：data-export 8/8；TypeScript/Vite build 通过。Vite 仍报告既有单 chunk >500 kB warning，不影响本 feature 行为或构建退出码。
- `git diff --check` → exit 0；真实 index 无 staged file。

## 4. Scenario Results

- [x] QA-001～QA-003 契约、全实体、默认 Settings、query budget：pass。
  - Evidence：`TestPostgresRepositoryLoadsEveryEntityAndTerminalStatus`、`TestPostgresRepositoryUsesOneBatchReadPerExportTableAndNoCountQuery`、`TestDataExportRouteReturnsRealAccountData`、CMD-001～003。
  - 隔离实际下载重新解析为 `schema_version=1`、七类 `[]`、七项 counts 全零且逐项等于数组长度；Settings keys 为 timezone/birthday/follow-up/churn/digest，未伪造 Telegram chat ID。
- [x] QA-004～QA-006 鉴权、隔离、allowlist、snapshot/read-only：pass。
  - Evidence：账号隔离 integration；解析后禁止结构键扫描；`TestReadSnapshotDoesNotObserveLaterCommittedWrites`；`TestReadSnapshotUsesRepeatableReadAndReadOnlyTransaction`。
  - 隔离 JWT secret 旋转后点击导出，浏览器实际转到 `http://localhost:5173/login`。
- [x] QA-007～QA-009 失败边界、headers、前端 parser/blob：pass。
  - Evidence：完整 middleware 取消四象限、pre-send stages、transport writer、strict MIME/filename、mock body/blob rejection 8/8。
- [x] QA-010 浏览器下载协作：pass。
  - 成功：键盘 Enter 触发一个 538-byte synthetic JSON 附件；文件名满足 `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$`；重新解析结构/counts 后删除文件。
  - 快速重复：在隔离 PostgreSQL 对 `customers` 持有 AccessExclusiveLock；第一次点击后按钮变为 disabled/“正在准备导出…”；第二次同坐标物理点击后仍只有 1 个等待锁的 export query；解锁后恢复。
  - Settings/500：注入合法 JSONB 但错误 shape 后，Settings 加载为 500，DataExportCard 仍常驻；导出显示“导出失败，请重试。未完整读取的文件不会保存。”，按钮恢复可用，Downloads 中新增文件数为 0；删除故障行后同一卡片重试成功。
  - body interruption：故障服务先返回合法 `application/json; charset=utf-8` 与固定附件文件名，再发送不完整 chunked body；中断 Vite 下游连接后真实浏览器进入同一可重试错误态，Downloads 新增文件数为 0。CMD-004 同时断言 `response.blob()` reject 时 object URL/create/click 均为 0。
  - 401：旧 token 请求旋转 secret 的隔离服务，卡片实际导航到 `/login`。
- [x] QA-011 视觉与键盘：pass。
  - Desktop：CSS viewport 1280×900，DPR=1，document/body scrollWidth=1265，无横向溢出；card left=260/right=836/width=576。
  - Mobile：CSS viewport 375×812，DPR=1，document/body scrollWidth=360，无横向溢出；card left=12/right=348/width=336，按钮 width=294。
  - 两种页面均常驻姓名、手机号、社交身份、备注、Telegram chat ID 的 PII 提示、受控保管/及时删除，以及头像图片未包含/不可跨部署恢复；native button 由 Enter 实际触发下载。
  - Screenshots：`evidence/data-export-qa-desktop-1280.jpg`、`evidence/data-export-qa-mobile-375.jpg`。
- [x] QA-012～QA-014 范围、review focus、residual：pass。
  - router 未新增 import/restore；前后端没有 export local/session/IndexedDB 持久化；无服务端临时导出文件；未知 API 404 测试保持绿色。

## 5. Findings

### failed

none。

### blocked

none。

### residual-risk

- 已批准的同步全内存预物化仍没有文件大小、耗时、内存或并发导出硬上限；真实最大账号规模性能不在本 feature 承诺内。
- 200 headers 已发送后的网络断连不可逆，且服务端未必观察到内核缓冲后的断连；当前以 Content-Length、浏览器完整 blob 读取和失败不下载共同收敛。
- `avatar_url` 是当前部署 reference-only 引用，不是媒体备份，不承诺跨部署恢复。
- Code review 的低概率建议仍保留：interface typed-nil Clock/Repository 防御和 `n < len(body), err != nil` partial writer 的额外测试；当前生产装配与已测 transport contract 不受影响。
- Vite production build 继续提示单 chunk >500 kB；属于既有 bundle warning，本 feature 没有大小预算门槛，build 与全部功能证据均通过。

## 6. Cleanliness

- Debug output：pass；生产 diff 无 `console.log/error`、debugger 或临时诊断输出。
- Temporary TODO/FIXME/XXX：pass。
- Commented-out code：pass。
- Unused imports / dead code from this feature：pass；Go/TS lint 与 build 通过。
- Sensitive payload artifact：pass；QA 报告/截图不含 token、owner PII、远端连接或导出 payload；所有下载测试文件已删除。
- Out-of-scope files：pass；`.workflow/`、`install-cpamp.sh` 保留未跟踪且不纳入范围。
- Runtime cleanup：pass；临时 Go/Vite/故障服务、下载文件、QA container 与 viewport override 已清理。

## 7. Verdict

- Status：passed。
- Blocking QA items：0 failed / 0 blocked。
- Core evidence：A1～A18 均有运行或结构证据；功能性 API 与浏览器主路径没有降格为 residual-risk。
- Next：进入 `cs-feat` acceptance 阶段，核对 24 个 checks、roadmap `in-progress → done`、最终产物清洁度与 commit gate。
