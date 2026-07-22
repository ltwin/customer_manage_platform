---
doc_type: feature-implementation
feature: 2026-07-21-data-export
status: passed
updated: 2026-07-21
---

# data-export 实现证据

## 基线与第一性原则 pre-pass

- Branch：`codex/feat/data-export`；baseline：`3a02be57a80eeac1d192685825bcbeaab3fbf50d`。
- 基线预检：CodeStable runtime check 为 `ok`；`cd backend && go test ./internal/platform/store/... -count=1 -parallel=1` 在改动前通过。
- 外部行为：当前认证账号可从设置页下载一个账号一致的 reference-only JSON 附件。
- 不可破约束：账号隔离；`REPEATABLE READ, READ ONLY`；公开 OpenAPI allowlist；counts 由最终数组长度派生；完整序列化后再发送成功 headers。
- 最小充分改动：OpenAPI/codegen、受保护后端 route 与 dataexport read model、设置页独立下载卡片及必要测试。
- 必须不写：头像字节/内部对象信息、import/restore、streaming/async、临时导出文件、跨账号入口或新的数据库 schema。
- 既有无关未跟踪项 `.workflow/`、`install-cpamp.sh` 保持不动。

## STEP-001 — AccountScope 事务代码纯移动

- 退出信号：store 既有测试全绿、公开 Go 签名与 SQL 行为零变化，diff 只有 move/import 调整。
- TDD exception: behavior-preserving move。此步不改变行为；替代证据是移动前/后相同定向测试、公开签名对照与源码 diff。
- 影响面：从 `backend/internal/platform/store/scope.go` 纯移动 `TxAccountScope`、`WithinTx`、`WithTxScope`、`withinTx` 与全部既有 delegate 到同包 `scope_tx.go`。
- RED：不适用；这是批准设计明确要求的纯移动前置。
- GREEN：不适用；未新增或改变行为。
- VERIFY：移动前、移动后均执行 `cd backend && go test ./internal/platform/store/... -count=1 -parallel=1`，分别通过；baseline 与当前公开方法签名逐项一致；尚未新增 `ReadTxAccountScope` / `WithReadSnapshot`。
- 清洁度：`gofmt`、`git diff --check` 通过；无调试输出、临时 TODO/FIXME、注释掉代码、无用 import 或方案外文件。
- 结果：通过。

## STEP-002 — 命名契约与未挂 route 的编排骨架

- 退出信号：生成物出现可命名的 `ExportDocument` / `ExportCounts` 顶层类型且零生成漂移；handler fake 返回 schema v1、七个空数组与零 counts；生产 route 尚未暴露。
- TDD exception：OpenAPI schema 与 codegen 是机器契约，替代证据为 schema diff、`make generate` 成功、Go/TS 生成物与前端 build。
- Service 行为 RED：`TestServiceBuildsVersionedDocumentWithLengthDerivedCounts` 首次因 `Snapshot`、`NewService`、`Counts` 不存在而按目标失败。
- Service GREEN：新增 `dataexport.Snapshot`、`Document`、`Counts`、`Repository`、`Clock` 与 `Service.Build`；从最终数组长度派生 counts，统一 UTC `exported_at`，空数组规范化为非 nil。
- Handler 行为 RED：`TestExportAllProjectsEmptyVersionedDocument` 首次因 `handlers` 缺 `ExportAll` / `dataExport` 而按目标失败。
- Handler GREEN：新增窄 `DataExportService` 依赖与 OpenAPI projection；fake document 可得到 schema v1、七个 `[]` 与零 counts。
- VERIFY：`make generate` 成功；Go 生成物含命名 `ExportDocument` / `ExportCounts`；OpenAPI 根与 counts 均 `additionalProperties: false`，七项 counts 均 `minimum: 0`；`cd backend && go test ./internal/dataexport/... ./internal/platform/httpapi/... -count=1 -parallel=1` 通过；`cd frontend && npm run build` 通过；既有 router 测试仍证明 `/api/v1/export` 未注册时为 404。
- 清洁度：`gofmt`、`git diff --check` 通过；未新增生产 route、伪空 repository、重复顶层 HTTP DTO、调试输出或方案外能力。
- 结果：通过。

## STEP-003 — 一致快照、全量计算节点与生产挂载

- 退出信号：真实 PostgreSQL fixture 覆盖 A2–A4、A8、A16–A18；事务属性为 `repeatable read` / `on`，窄读类型无写面；受保护 route 无 token 为 401、有效 token 读取真实数据。
- Read snapshot RED：`TestReadSnapshotUsesRepeatableReadAndReadOnlyTransaction` 首次因 `WithReadSnapshot` / `ReadTxAccountScope` 不存在而编译失败。
- Read snapshot GREEN：新增只暴露 Query/QueryRow/QueryPage/Count/ScalarAggregate/Exists 的 `ReadTxAccountScope`；`WithReadSnapshot` 使用 `pgx.RepeatableRead` + `pgx.ReadOnly`，并覆盖 callback、rollback、commit 与 panic rollback。
- Repository RED 1：`TestPostgresRepositoryLoadsCustomersInStableOrderAndDefaultsEmptySettings` 首次因 `NewPostgresRepository` 不存在而失败。
- Repository GREEN 1：真实只读快照先完成 Customer allowlist、created_at/id 排序与无 Settings 行默认值。
- Repository RED 2：全实体/全终态 fixture 首次得到其余六类空数组，按目标证明节点未实现。
- Repository GREEN 2：补 SocialIdentity、CustomerNote、Package、Order、ScheduleSlot、Reminder 与有效 Settings；每类一次账号级批量读取，repository 排序，Service counts 仍只取最终 len。
- Route RED 1：无 token 的 `/api/v1/export` 首次为未注册 404；注册受保护 route 后 GREEN 为 401 unauthorized。
- Route RED 2：有效 token + 真实 Customer/Identity 首次因 composition 缺 dataexport Service 返回 500；注入真实 `PostgresRepository` 与 clock 后 GREEN 为 200 且 counts 等于数组长度。
- VERIFY：同 session 属性测试得到 `repeatable read` / `on`；反射断言生产只读类型无 Insert/Upsert/Update/Delete/QueryRowForUpdate/事务/raw tx 面；并发提交验证当前 snapshot 不见后写、下一 snapshot 可见。review-fix 后全量 fixture 以完整 expected Snapshot 深比较七类全部公开字段，覆盖 nil/non-nil nullable、Customer 3 态、Package 2 态、Order 8 态、Slot 3 态、Reminder 3 态，以及相同 `created_at` 下按 `id` 的 tie-break；时间按同一 instant/UTC 比较。Settings 与 `settings.Service.Get` 逐字段 parity，包含 timezone、birthday/follow-up、digest hour、Telegram chat ID、threshold 的 type/days/顺序与 UpdatedAt instant。
- A17：启用 PostgreSQL `log_statement=all` 的真实 repository 测试，逐表断言 customers、social_identities、customer_notes、packages、orders、schedule_slots、reminders、settings 各恰好一次账号级 SELECT，且无业务表 `count(*)`。
- 验证命令：`cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/settings/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` 通过；`go test ./... -run '^$'` 编译通过。
- 清洁度：`gofmt`、窄依赖方向与只读能力面核对通过；未用七个领域 Service fan-in、通用 dump、N+1、行锁或默认 READ COMMITTED。
- 结果：通过。

## STEP-004 — 安全与失败 hardening

- 退出信号：A5–A11 负向测试通过；解析后 JSON key 与唯一哨兵扫描无内部 avatar generation、凭证或运行状态。
- Headers/预序列化 RED：完整文档 handler 测试首次得到 Gin 默认 `application/json; charset=utf-8`，且缺附件、安全与长度 headers。
- Headers/预序列化 GREEN：固定 Build → OpenAPI projection → `json.Marshal` 完整 bytes → 最后一次 context 检查 → headers/Content-Length → 单次 write；文件名由 `Document.ExportedAt` 同一 UTC 时点生成。
- 故障 VERIFY：fake Build、projection、encoder 与已取消 context 均不写 `Content-Disposition` 或响应 body；review-fix 后包装的 `context.Canceled` 与 Build 成功后的最终 context 取消均经过生产 recovery/request-log/error-envelope 中间件链，证明不写 Content-Type/Content-Disposition/body、不排队错误封套，只记录固定 stage/reason 取消元数据。空 Scope、缺 Repository/Clock/ScopeFactory/Service 均显式报错；真实 PostgreSQL 错误 Settings shape 返回 settings stage error；context 在 callback 后取消产生显式 commit-stage error。
- Transport VERIFY：注入 headers 后 write error 时只尝试一次 write、不向 Gin errors 排队第二封套；结构化日志只含 stage、serialized_bytes 与 error，不含注入的 `PII-SENTINEL` 或实体 payload。
- 账号隔离：真实 A/B 数据 fixture 中 B snapshot 只返回 B 行；客户端没有 account_id 输入面；所有 repository 查询继续经 `ReadTxAccountScope`。
- Reference-only：真实 Customer 同时写入公开 avatar revision/version 与内部 object_id/media/size/updated_at、GC last_error 唯一哨兵；响应保留 `avatar_revision/avatar_version/avatar_url`，且值扫描排除这两类真实内部哨兵。其他 password/JWT/bot/bind/idempotency/checkpoint/delivery/log 状态没有逐项注入唯一值；其排除证据是专用 allowlist SQL 不查询相关表/列、OpenAPI projection 不提供相应字段，以及解析后 JSON 结构键扫描。不得把后者继续表述为“每类内部状态均已注入唯一哨兵”。
- Headers：`Content-Type: application/json`、精确 UTC attachment、正确 `Content-Length`、`Cache-Control: no-store`、`X-Content-Type-Options: nosniff` 均由自动化测试核对。
- 验证命令：`cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` 与相关 `go vet` 通过；`git diff --check` 通过。
- 清洁度：没有 payload 日志、临时文件、调试输出、注释掉代码或额外 API；未知 API 的既有 404 测试保留。
- 结果：通过。

## STEP-005 — 前端附件 client 与下载协作

- 退出信号：定向脚本覆盖 Bearer、Blob、合法/非法 MIME、精确 filename/fallback、500/401、body/blob rejection；失败不产出结果、不创建 URL/下载；成功一次下载并 revoke；Makefile 持续执行脚本。
- RED：`npm run test:data-export` 首次因 `dataExportDownload.ts` 不存在而模块解析失败，确认下载能力尚未实现。
- GREEN：新增 `fetchDataExport`，只向固定 `/api/v1/export` 发 Bearer GET；成功先解析 Content-Type/Content-Disposition，再完整 `response.blob()`，最后返回 `{blob, filename}`。新增 `saveDataExport` / `requestAndDownloadDataExport`，只在 client 成功后创建 object URL，并在 `finally` 中释放。
- MIME parser：按分号、quoted-string 与 escape 状态解析 media type；base type 大小写无关但必须精确 `application/json`；接受合法 token/quoted 参数，拒绝缺失、语法错误、substring、`application/jsonish` 与任意 `+json`。
- Filename：只接受整串 `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$`；缺失、路径、追加扩展、非固定名、超长和未 quoted 均回退本地固定 UTC 格式。
- Error/transport：500/401 复用 `ApiError`，401 清 token；mock `response.blob()` reject 时 Promise 原样 reject，没有 `{blob, filename}`，协作层没有 object URL 或 click。
- VERIFY：`npm run test:data-export` 6/6 通过；`npm run build`、`npm run lint` 通过；成功路径只 create/click/revoke 各一次。`package.json` 新增 `test:data-export`，`make -n test` 显示聚合 `test` 目标会执行它，实际 `make test` 已启动并完成串行 backend + frontend 测试链。
- TDD exception：package script 与 Makefile 接线是纯配置；替代证据为 `make -n test` 展开和实际聚合命令完成。
- 清洁度：Blob 未写 localStorage/sessionStorage/IndexedDB；没有手写 Export DTO、调试输出或通用附件框架。
- 结果：通过。

## STEP-006 — 设置页 DataExportCard 与浏览器验收

- 退出信号：桌面与 375px 可触发导出；Settings 加载失败不隐藏卡片；导出失败可重试；无横向溢出或重复请求。
- RED：`npm run test:data-export` 新增 UI contract 后 6 项既有 client 测试通过，2 项因 `DataExportCard.tsx` 不存在按目标失败。
- GREEN：新增独立 `DataExportCard`，内部持有 downloading/error；重复触发先由 `if (downloading) return` 守护，按钮同步 disabled；401 调用 `onUnauthorized`，其他错误用 `role=alert` 展示“可重试、未完整读取不会保存”。SettingsPage 在 loading、error 与成功三种普通 Settings 状态都挂载同一个 card element。
- 文案：常驻列出姓名、手机号、社交身份、备注、Telegram chat ID；明确只存受控位置、不需要时及时删除；明确头像图片未包含、公开引用不可跨部署恢复。
- 自动化 VERIFY：`npm run test:data-export` 8/8、`npm run build`、`npm run lint` 通过。Blob rejection、成功时单次 create/click/revoke 为函数级行为测试；原生 button、disabled/重复触发 guard、401 分支、alert、Settings 三状态挂载与无浏览器持久化目前是源码 contract，只能作为静态补充，不能宣称已自动执行 React 状态转换。401、500/Blob reject、快速重复触发与 Settings failure 保留为 QA 浏览器行为重点。
- 浏览器 VERIFY：使用全新隔离 PostgreSQL、虚构账号与显式禁用 Telegram 的本地服务；桌面设置页正确呈现独立卡、完整 PII/reference-only 文案与设置表单；按钮 Enter 触发后端一次真实 `GET /api/v1/export` 200。375×812 下 `innerWidth=375`、body/document scrollWidth=360、卡片 left=12/right=348/width=336，无横向溢出。停止后端并 reload 后，普通 Settings 显示 502 的同时导出卡仍可见；点击导出显示卡内可重试错误。
- 截图：`evidence/data-export-settings-desktop.png` 原始像素为 1969×1109，`evidence/data-export-settings-375.png` 原始像素为 360×1101；均只含虚构空账号/default Settings。viewport、DPR、innerWidth/scrollWidth 与证据限制见 `evidence/data-export-browser-capture-metadata.md`，不再把 PNG 物理宽度直接称作 1280/375 viewport。
- TDD exception：桌面/375px 像素布局和真实键盘触发无法由现有无 DOM test runner 可靠断言；当前替代证据为真实浏览器截图、已有 DOM snapshot、移动场景 scrollWidth/bounding-box 与服务端单次 200 请求日志。由于 desktop DPR/innerWidth 未持久化、mobile PNG 为 360px，精确 viewport 与交互矩阵必须在 QA 重拍/复测，不能由本报告声称全自动证明。
- 执行环境备注：首次准备浏览器时曾误从仓库 `.env` 启动 backend，Telegram runner 短暂运行约 22 秒；发现后立即停止，浏览器尚未登录或访问该实例。随后所有验收均改用隔离 DB、虚构凭证与 Telegram 显式禁用环境；未发送消息、未操作真实 CRM 数据。该短暂 runner 可能执行过外部 polling，已向主执行者报告。
- 清洁度：浏览器/Vite/隔离 backend 与容器均已停止，临时 viewport 已 reset、浏览器 tab 已 finalize；无真实 PII、token 或下载 payload 保存到 evidence。
- 结果：通过。

## STEP-007 — 全链路验证与范围清扫

- 退出信号：全部核心场景有证据，`make check` 全绿，无临时或敏感产物残留。
- TDD exception: verification-only。本步骤不新增生产行为；替代证据为 CMD-001～004、机器 gate、API fixture、桌面/375px 截图与最终 diff review。
- CMD-001：`make check` 完整重跑 exit 0；覆盖 backend/frontend build、双端 lint、串行 Go 全包、全部前端脚本与 generate-check。第一次因 `repository_test.go` 的 reader close 未检查被 lint 拦截，窄修复为显式忽略 close error 后定向 lint 0 issues。另两次机器 runner 命中 attention 已记录的 Testcontainers `port 5432/tcp not found`；受影响 customer/order 既有用例单独串行通过，同一代码状态下完整 `make check` 已通过。
- CMD-002：临时 `GIT_INDEX_FILE` 中仅放当前两份生成物，执行字面 `make generate && git diff --exit-code -- ...` exit 0；真实 index 最终为空，未暂存用户工作区。
- CMD-003：store/dataexport/httpapi/server 全部通过；最近实跑分别约 20.2s / 8.3s / 26.3s / 1.6s。
- CMD-004：data-export 8/8 + frontend build 通过。
- 机器结果：DoD Contract `passed`；scope gate `passed`；checklist/goal-state/roadmap-items YAML 全部 valid；汇总 `data-export-dod-results.json` 为 passed，并保留 runner 原始环境 flake provenance。
- API/浏览器证据：`evidence/data-export-api-sample.md`、desktop/375px PNG；全部使用虚构 fixture，不含 owner PII 或 token。
- 范围清扫：无 import/restore/zip/async/history/progress 生产能力，无数据库 migration，无临时导出文件/目录，无 Blob 持久存储；未知 API 404 测试保留；`.workflow/` 与 `install-cpamp.sh` 原样保留且不在 feature gate 范围。
- 清洁度：`git diff --check` 通过；真实 Git index 为空；新增生产/测试文件无 console/fmt 调试打印、TODO/FIXME/XXX、注释掉代码或无用 import。
- 结果：通过。

## Review-fix round 1 — CR-DE-001～CR-DE-004

### CR-DE-001 — 取消请求不进入通用 500 封套

- 行为：Build 返回包装后的取消错误，或 Build 成功后最终发送前发现 request context 已取消时，只记录脱敏取消元数据，不写附件 headers/body，也不把错误排入 `c.Errors` 供 `errorEnvelopeMiddleware` 渲染 500。
- RED：新增 `TestExportAllDoesNotRenderErrorEnvelopeWhenBuildReturnsWrappedCancellation` 与 `TestExportAllDoesNotRenderErrorEnvelopeWhenContextIsCanceledBeforeSend`，均通过安装 production recovery/request-log/error-envelope 的真实 Gin engine 执行；修复前两例都得到 `Content-Type: application/json; charset=utf-8`，证明 middleware 实际写了 500 错误封套。
- GREEN：`ExportAll` 统一识别 request context error、`errors.Is(context.Canceled)` 与 `errors.Is(context.DeadlineExceeded)`；取消时只记录固定 `stage`/`reason`，不记录包装错误或 payload，随后直接返回。真正的 Build/query/scan/mapping/serialization error 继续进入通用 500。
- VERIFY：两个完整 middleware 取消测试与既有 pre-send fault table 同时通过；断言无 Content-Type/Content-Disposition/body、无第二封套，日志含 `data export canceled` 且不含包装错误或 PII sentinel。
- REFACTOR：无；仅新增 handler 内局部取消分类，不改变公共接口或通用 middleware。

### CR-DE-002 — `exported_at` 表示导出开始时间

- 行为：依赖校验后先捕获一次 Clock，再进入 Repository snapshot；Document 使用该已捕获时间。
- RED：`TestServiceCapturesExportTimeBeforeLoadingSnapshot` 修复前实际事件为 `repository,clock`，与期望 `clock,repository` 不符。
- GREEN：`Service.Build` 在 `LoadSnapshot` 前执行一次 `exportedAt := s.clock.Now().UTC()`，Repository 返回后不再读取 Clock。
- VERIFY：事件严格为 `clock,repository`，Clock 调用一次，Document 时间等于同一 UTC instant；既有 HTTP header 测试继续证明附件文件名从 `Document.ExportedAt` 派生。
- REFACTOR：无。

### CR-DE-003 — 提升证据强度并纠正证据等级

- TDD exception：本项不新增产品行为，目标是让测试对批准契约真正敏感并纠正报告口径；替代证据为真实 PostgreSQL/HTTP 强断言和文档 diff。
- Repository：`TestPostgresRepositoryLoadsEveryEntityAndTerminalStatus` 改为完整 expected Snapshot 深比较，逐字段覆盖七类实体、nil/non-nil nullable、全部历史/终态与相同 `created_at` 下的 `id` tie-break；所有 timestamp 只按 UTC instant 比较。
- Settings parity：空账号逐字段等于 `DefaultSettings()`；非默认设置逐字段等于 `settings.Service.Get`，包含 timezone、birthday/follow-up、digest hour、Telegram chat ID、每个 churn threshold 的 type/days/顺序与 UpdatedAt instant。
- 真实 route：`TestDataExportRouteReturnsRealAccountData` 现在使用真实 PostgreSQL 创建七类各一条、全部公开字段与非默认 Settings；解析 `ExportDocument` 并对完整 raw JSON 做 exact deep comparison，七项 counts 均等于数组长度；连续请求两次，清除 `exported_at` 后完整文档相等。首次强化运行只因 PostgreSQL timestamp location 为 `+08:00`、expected 为 `Z` 而失败，比较层规范化同一 instant 到 UTC 后通过，生产序列化未改。
- A7 口径：仅 avatar object 与 GC 使用真实唯一哨兵；其他凭证/运行状态明确由专用 allowlist query、OpenAPI projection 与解析后结构键审查证明，不再宣称逐类注入唯一哨兵。
- A13/A14 口径：Blob/object URL 保留行为测试；React loading/retry/401/快速重复触发与精确 viewport 降格为静态 contract + browser/QA focus。新增 `evidence/data-export-browser-capture-metadata.md`，记录 PNG 原始像素、已知 innerWidth/scrollWidth/bounding box、未持久化 DPR/desktop metrics 与证据限制。
- VERIFY：`go test ./internal/dataexport/... ./internal/platform/httpapi/... -count=1 -parallel=1` 通过；dataexport 约 7.2s、httpapi 约 23.4s。

### CR-DE-004 — OpenAPI tag 状态

- TDD exception：纯文档描述，不改变 endpoint/schema/生成类型或运行时行为。
- 修复：`api/openapi.yaml` 的 export tag 从“全量导出（data-export，未实现）”改为“全量导出（data-export）”。
- VERIFY：纳入后续 CMD-002 codegen 零漂移与 OpenAPI gate。

### Review-fix 正式门禁

- 第一次普通 `make check` 在新增 fixture 的两个 `if/else` 被 staticcheck `QF1003` 拦截；仅改为 tagged switch，定向 golangci-lint 返回 `0 issues`。
- 第二次普通 `make check` 已通过 build、双端 lint、全部 Go/前端测试，最后只因真实 Git index 仍是 feature baseline 而把本次合法 Go/TS 生成物 diff 报成 generate drift；生产与测试阶段均无失败。
- 使用只承载当前 Go/TS 生成物的临时 `GIT_INDEX_FILE` 重新执行正式 DoD runner，CMD-001～CMD-004 全部 exit 0；完整 `make check` 包含 build、双端 lint、串行全包 Go 测试、全部前端脚本和 generate-check。真实 Git index 前后均为空。
- DoD Contract、scope gate、YAML、`git diff --check` 与 evidence pack 均为 passed；`.workflow/`、`install-cpamp.sh` 仍为范围外未跟踪项，未修改、未删除。

### 未采纳的非阻塞 suggestion

- typed-nil Clock 防御与 partial writer 补测仍为 suggestion，不是 CR-DE-001～004 的必要修复；本轮不引入反射式通用 nil 检查，也不扩大 transport abstraction。若 round 2 仍建议，可作为 acceptance residual risk 记录。

## Review-fix round 2 — CR-DE-001、CR-DE-005、CR-DE-006

### CR-DE-001 — request context 是取消分类的唯一权威

- 行为：只有 `Request.Context().Err() != nil` 才按“请求已取消”处理；active request 下的孤立 wrapped `context.Canceled` / `DeadlineExceeded` 必须进入标准 500 ErrorEnvelope。
- RED：新增四象限完整 middleware 测试。修复前，active request + wrapped canceled 与 active request + wrapped deadline 均实际得到空 `200`，body 为空；已取消/已超时 request 两例无附件/封套。
- GREEN：`dataExportRequestCanceled` 不再仅凭下层 error chain 判定 request cancellation；Build error 时先检查 request context，仍 active 就调用 `c.Error` 交给 error-envelope middleware。最终 pre-send context check 继续只在 request context done 时记录取消。
- VERIFY：四象限全部通过：canceled/deadline request 不写 Content-Type/Content-Disposition/body、不排入 `c.Errors`，日志 reason 分别为 `canceled` / `deadline_exceeded`；wrapped error 中实际包含 `PII-SENTINEL`，取消日志不包含它。active 两例均为 500、`error.code=internal`、handler error 数为 1，且不产生 cancellation log。Build 成功后的最终发送前取消测试也继续通过。
- REFACTOR：无；没有修改通用 middleware、公开 API 或 dataexport Service。

### CR-DE-005 — tie-break fixture 对排序回归敏感

- TDD exception：只增强既有测试 fixture 的反回归敏感度，不改变生产行为。
- 修复：在外键允许的前提下，Customer 使用 `a,c,b` 固定乱序；Identity/Note/Package/Order/Slot/Reminder 使用逆序或非最终顺序插入；expected Snapshot 仍严格为 `created_at ASC, id ASC`。
- VERIFY：`TestPostgresRepositoryLoadsEveryEntityAndTerminalStatus` 在逆序/乱序 fixture 下通过；删除某类显式 sort 时不再可依赖插入顺序假绿。

### CR-DE-006 — evidence pack 区分 runner warning 与 feature residual risk

- TDD exception：证据文档口径修复，不改变产品行为。
- 修复：evidence pack 不再写泛化的 `Residual Risks: none`；明确 machine-runner 当前无 warning，同时列出 A13/A14 QA、同步内存模型、headers 后不可逆中断、reference-only 头像与 typed-nil/partial writer 非阻塞风险。
- VERIFY：round 3 reviewer 与 acceptance 可单独阅读 evidence pack，不会把“机器命令全绿”误解成“feature 没有残余风险”。

### Round 2 正式门禁

- 相关包 lint：`golangci-lint run ./internal/dataexport/... ./internal/platform/httpapi/...` → `0 issues`。
- 首次相关包全量运行命中既有 Docker/Testcontainers `port "5432/tcp" not found`；package 串行后 dataexport 通过，httpapi 另一个既有 customer 用例再次命中同一 flake；受影响用例单独复跑通过。
- 第一轮正式 DoD runner：CMD-001 的既有 `TestReferralVisibility` 命中同一 Docker port flake；CMD-003 的 statement-log 即时读取暂缺 reminders 行。两项分别单独复跑均通过，生产代码未改；原始失败输出由执行记录保留。
- 第二轮正式 DoD runner 使用临时 Git index，CMD-001～CMD-004 全部 exit 0；CMD-003 当前代码的 store/dataexport/httpapi/server 均通过，CMD-004 为 8/8 + build。DoD Contract、scope、evidence-pack、YAML、diff cleanliness 重新为 passed；真实 Git index 为空。

## 实现完成汇报

### 动了哪些文件

- 契约/生成：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、Go/TS 生成物。
- 后端：`store/scope.go` 纯移动配合新 `scope_tx.go`；新 `internal/dataexport/*`；新 HTTP adapter/tests；router/auth/composition root；Settings 有效默认纯函数导出。
- 前端：API client、新下载协作、`DataExportCard`、SettingsPage、定向脚本、package/Makefile test wiring。
- CodeStable：feature design/checklist/review/goal/implementation/DoD/gate/evidence 与 roadmap 两文件。
- 既有无关未跟踪项 `.workflow/`、`install-cpamp.sh` 未触碰；没有 staged file、commit 或 push。

### 改了哪些函数 / 类型（按步骤分组）

- STEP-001：纯移动 `TxAccountScope`、`WithinTx`、`WithTxScope`、`withinTx` 与 delegates。
- STEP-002：新增 `Snapshot`、`Document`、`Counts`、`Repository`、`Clock`、`Service.Build`；OpenAPI `ExportDocument` / `ExportCounts`；未挂 route 的 `ExportAll` projection。
- STEP-003：新增 `ReadTxAccountScope` / `WithReadSnapshot`、`PostgresRepository.LoadSnapshot` 与七类扫描器；公开 `settings.EffectiveSettings`；router/main 装配。
- STEP-004：`ExportAll` 增加 projection/encoder fault seam、完整 JSON 预序列化、安全 headers、context 与 transport boundary。
- STEP-005：新增 `fetchDataExport`、MIME/filename parser、`saveDataExport` / `requestAndDownloadDataExport`。
- STEP-006：新增 `DataExportCard`，SettingsPage 三状态常驻挂载。
- STEP-007：只新增/更新验证与证据，不改生产行为。

### 是否触碰到方案外的文件？

否。全部改动可回到 approved design 的三个 MOUNT、内部 read model、事务前置和测试/证据。外部 Telegram runner 的短暂误启动已作为执行环境备注单独披露，不对应代码范围扩张。

### 是否引入了方案 doc 里没有的新概念 / 抽象？

否。代码名均来自 `Data Export`、`Export Document/Counts/Snapshot`、`ReadTxAccountScope`、`Effective Settings`、`DataExportCard` 和已说明的下载协作；没有通用 export framework 或新领域实体。

### 第一性原则 pre-pass 核对

- 外部行为：认证账号下载一个 reference-only JSON；设置页明确 PII 和头像不可携带。
- 不可破约束：账号隔离、一致只读快照、公开 allowlist、len counts、预序列化后 headers。
- 最小充分：命名契约 + 一个专用 read model + 一个受保护 route + 一个独立 card。
- 必须不写：媒体包/import/restore/streaming/async/临时文件/跨账号/新 schema；最终 diff 均未出现。

### 代码质量反射检查自检

- `scope.go` 超长信号按 design 2.5 先纯移动解决，行为步骤后置。
- `SettingsPage` 第三个交互职责独立到 `DataExportCard`，页面只挂载。
- `client.ts` 加入单个附件 transport 后为 517 行；approved design 已明确本 feature 只加这一处，未来出现更多媒体/附件入口才另走 `cs-refactor`，本次不顺手重划全部 client。
- `repository.go` 368 行承载单一深模块的七个同形 allowlist scanner 与一个事务编排；未抽万能 scanner/helper，也未拆成 service fan-in。
- 没有特殊账号分支、第 4+ 个生产参数、万能 util 或方案外重命名。

### Step 证据与失败恢复

- STEP-001～006 的 RED/GREEN/VERIFY 与例外已逐节记录。
- STEP-007 第一次真实失败是新增 test reader close 的 lint，已作一行窄修复并重跑；Docker flake 与 feature 无关，受影响既有用例和完整门禁均已获得通过证据。
- 7/7 steps 现均 `done`。

### TDD 证据

- STEP-001/007：明确 TDD exception + 行为不变/verification-only 替代证据。
- STEP-002：Service 与 handler 两个 RED→GREEN。
- STEP-003：read snapshot、分阶段 repository、受保护 route、真实 composition 多轮 RED→GREEN。
- STEP-004：headers/预序列化 RED→GREEN；fault/security cases VERIFY。
- STEP-005：缺下载模块 RED，client/协作 GREEN，8 项 VERIFY。
- STEP-006：缺组件 2 项 RED，组件/挂载 GREEN，自动化 + browser VERIFY。

### 基线预检与清洁度

- 基线 runtime `ok`，store 测试在第一行代码前通过。
- 已知 Testcontainers port flake 被重复真实观察并按 attention 归因；不掩盖，原始 JSON 保留。
- `gofmt`、Go/TS lint、build、diff check、scope cleanliness 均通过。

### 实际交付物索引

- 代码：read snapshot、dataexport read model/service/repository、HTTP adapter、client/download/card。
- 配置：OpenAPI export components/headers、Go include tag、Makefile/package test。
- 路由：受 auth 保护的 `GET /api/v1/export`；无新增 import/restore 路由。
- 数据库：零 migration、零临时导出文件。
- 文档/证据：implementation、DoD/gates、API fixture、desktop/375px screenshots；roadmap 仍 `in-progress`，等待 acceptance 后改 done。

### 知识回写候选

- 无新增项目级候选；Docker port flake 已在 attention.md，OpenAPI tag/codegen 与 AccountScope 约束已在既有 compound/ADR。

### 最后一轮本地审计

- CMD-001～004 均有 exit 0；DoD Contract/scope/YAML/diff/cleanliness 通过。
- A1～A18 均有对应证据路径，但证据等级明确区分：A2/A4/A9/A11/A16 已由 review-fix 强化为真实 PostgreSQL/完整 middleware/全文档行为证据；A7 的非头像内部状态依赖 allowlist + projection + 结构键审查；A13 React 交互与 A14 精确 viewport 仍需 QA 浏览器复核，不再宣称全部由自动化证明。
- 真实 index 为空；未 commit/push；无 owner 数据进入测试或 evidence。

### 推进顺序退出信号核对

- STEP-001～007：全部 `done`，每步退出信号和证据见上文对应章节。

### 验收场景自检

- A1：CMD-001/002。
- A2/A4/A17：命名 codegen、全实体/全终态 fixture、PostgreSQL statement log 每表一次。
- A3/A16：空账号 defaults 与 Settings Service parity。
- A5：无 token 401 + A/B 隔离。
- A6/A7：公开 avatar reference；avatar object/GC 使用真实唯一哨兵值排除，其余凭证/运行状态使用专用 allowlist query + OpenAPI projection + 解析后结构键审查。
- A8/A18：并发 snapshot + same-session isolation/read-only + 无写面反射。
- A9：缺依赖/空 scope/Settings decode/commit/projection/encode/write faults；包装取消与最终发送前取消经过完整 Gin middleware 链且不写第二封套。
- A10/A11：完整 headers、稳定 repository 顺序与无写路径。
- A12/A13：MIME/filename/blob reject/object URL 为行为测试；React retry/401/快速重复触发/Settings failure 为静态 contract + 既有浏览器观察，QA 必须真实执行。
- A14：桌面/移动文案截图与已有 layout measurement；截图元数据限制已单独记录，精确 1280/375、DPR 与键盘由 QA 复核。
- A15：route/diff/persistence 范围清扫。
- 反向核对：reference-only、无 media package/import/restore/temporary/persistence 全部守住。
