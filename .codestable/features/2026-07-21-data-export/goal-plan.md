# data-export goal plan

## 路径

- feature：`.codestable/features/2026-07-21-data-export/`
- design：`data-export-design.md`（status: approved）
- checklist：`data-export-checklist.yaml`（7 steps / 24 checks / A1～A18）
- design-review：`data-export-design-review.md`（status: passed, round 3）

## Owner 确认依据

- 2026-07-21 owner 在完整 design 人工 checkpoint 回复“批准”，整体接受 reference-only JSON 边界、同步全内存预物化假设、“全量”只含当前仍存在记录的语义、`telegram_chat_id` 随 Settings 导出，以及外部使用/API 文档可在 acceptance 后由 `cs-docs` 补齐。
- Round 3 独立 design-review 为 `passed`：FDR-001～FDR-016 全部 resolved，0 blocking、0 important；唯一 R3-NIT-001 仅要求执行时区分 STEP-005 client/download 协作与 STEP-006 UI 呈现责任，不改变 approved design。
- 开发位置尚未确认。当前工作区位于 `develop`；按 `.codestable/attention.md`，在 owner 选择当前工作区 feature branch 或新 worktree 前，不写业务代码、不派发 Goal driver。

## 基线

- baseline ref：`3a02be57a80eeac1d192685825bcbeaab3fbf50d`
- 当前预期 feature 文档改动：新增 data-export design/checklist/design-review/goal 包，修改 photographer-private-crm roadmap 主文档与 items 状态；尚无 data-export 业务代码改动、暂存、commit 或 push。
- 既有无关未跟踪项：`.workflow/`、`install-cpamp.sh`；必须保留，不纳入本 feature，不得删除或覆盖。
- 本机 Docker Desktop 高并行 Testcontainers 可能偶发 `port "5432/tcp" not found`；Go 定向测试按 `-parallel=1` 运行并单独归因环境 flake。

## 必跑验证命令

机读权威源为 checklist `dod.commands`：

| ID | 命令 | 失败处理 |
|---|---|---|
| CMD-001 | `make check` | fix-or-block |
| CMD-002 | `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` | fix-or-block |
| CMD-003 | `cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` | fix-or-block |
| CMD-004 | `cd frontend && npm run test:data-export && npm run build` | fix-or-block |

## Implementation TDD policy

- STEP-001：纯移动 `TxAccountScope` 事务代码，不改变行为。允许写 `TDD exception: behavior-preserving move`；替代证据必须包含移动前/后 store 测试、公开签名核对与只含 move/import 的 diff。只有这一步验证后才可新增 read snapshot。
- STEP-002：OpenAPI 命名 component 与 codegen 属机器契约，可写 `TDD exception`，替代证据为 schema diff、生成物与 CMD-002；未挂生产 route 的 handler 行为必须先写失败测试，再 GREEN/VERIFY。不得在真实 Repository 完成前返回生产伪空 200。
- STEP-003：`ReadTxAccountScope`、`WithReadSnapshot`、PostgresRepository、有效 Settings、稳定排序、len-derived counts 和 route/composition wiring 必须 RED→GREEN→VERIFY；使用真实 PostgreSQL 覆盖 A2～A4、A8、A16～A18。A18 必须在同一 session 证明 `transaction_isolation=repeatable read`、`transaction_read_only=on`，并断言生产只读类型无写面。
- STEP-004：跨账号、敏感字段排除、空 scope/缺依赖、query/scan/commit/mapping/序列化/context、headers 后 writer、headers/日志场景先写负向或 fault RED，再实现 GREEN 并用 A5～A11 VERIFY。不得用日志或 evidence 保存真实 PII payload。
- STEP-005：Bearer Blob client、MIME media-type、精确 filename regex、ApiError、`response.blob()` rejection 与下载协作必须先写前端失败测试；读取失败必须无 `{blob, filename}`、无 object URL、无下载。新增 `test:data-export` 并接入 Makefile `test`。
- STEP-006：DataExportCard 的 loading/error/retry/401、Settings 加载失败独立可用、重复点击与 URL 生命周期先 RED→GREEN→VERIFY；375px、键盘、焦点与常驻 PII/reference-only 文案允许 `TDD exception`，替代证据为浏览器截图和可访问性观察。R3-NIT-001 的执行边界：STEP-005 证明未挂载协作不下载，STEP-006 才完成卡片可重试呈现与页面挂载。
- STEP-007：全链路命令、截图与范围清扫不新增生产行为，可写 `TDD exception: verification-only`；替代证据为 CMD-001～004 输出、API JSON/header 样本、桌面/375px 截图、diff/route/敏感产物检查。
- 任一行为 step 缺 RED/GREEN/VERIFY evidence 且无明确 `TDD exception` 与替代证据，implementation gate 不通过。

## 核心验收路径

1. 命名 `ExportDocument` / `ExportCounts` component 保持既有 JSON 字段/required，根与 counts 均禁止额外属性，counts 最小值为 0；Go/TS 生成物可引用且无手写重复顶层 DTO。
2. 当前账号有七类实体与非默认 Settings 时，受保护 `GET /api/v1/export` 返回一个完整 JSON 附件；counts 逐项等于最终数组长度，数组按 `created_at ASC, id ASC` 稳定排序。
3. 空账号得到七个 `[]`、全零 counts 与完整默认 Settings；部分 Settings 与 `GET /settings` 有效值逐字段一致，`telegram_chat_id` 按现有契约可选返回。
4. 所有 active/archived/merged/cancelled/done/dismissed 等当前仍存在的历史/终态记录均导出；物理删除记录与审计历史不恢复、不补造。
5. 无 token 为 401；账号 B 看不到 A；客户端没有 account_id 切换面；七类表与 settings 各至多一次批量读，无 N+1、无额外 `count(*)`。
6. A8 证明一致 snapshot；A18 独立证明 REPEATABLE READ、READ ONLY 与窄 Go 只读能力面，且不使用行锁或默认 READ COMMITTED 多查询。
7. Customer 只带公开 `avatar_revision/avatar_version/avatar_url`；结构键 allowlist + 唯一哨兵值双证据排除头像字节、内部 object_id/key/GC/reconciliation/manifest，以及 password/JWT/TG token/idempotency/checkpoint/delivery/log 状态。
8. Build/scan/settings/commit/mapping/JSON 预序列化/发送前 context 任一失败均不发成功附件 headers；headers 后 writer 失败不写第二封套，只记脱敏 transport failure。
9. 前端只接受 MIME base type 精确 `application/json`、允许合法参数，拒绝 loose substring 与任意 `+json`；文件名只接受 `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$`，其余安全 fallback。
10. body/blob 中断时 client reject、卡片可重试、无 object URL/download；成功只下载一次并释放 URL。Settings 普通加载失败不隐藏卡片，401 转登录。
11. 设置页在桌面/375px/键盘下可用，常驻说明文件含姓名、手机号、社交身份、备注、Telegram chat ID 等 PII，应受控保管/及时删除；头像图片未包含且引用不可跨部署恢复。
12. 服务端不持久化临时导出文件，浏览器不写 localStorage/sessionStorage/IndexedDB；未知 API 继续 404，import/restore/zip/async/history/progress 等显式不做项不进入 diff。

## DoD / gate policy

- implementation 前置为 design `approved` + design-review `passed` + owner 已确认开发位置；checklist 7 steps 完成并留下 step ledger 与 evidence pack。
- implementation gates 全绿后才进入独立 `cs-code-review`；review 必须分别给出 spec 合规与代码质量结论，两者缺一不算 passed。
- review blocking 时写 `review/fixing`，完成 review-fix 后重跑 review；review passed 后才进入 QA。
- QA failed/blocked 时写 `qa/fixing`，修复后必须回 `review/ready`，重新运行 code review 和 QA；QA passed 后才进入 acceptance。
- acceptance 核对 24 checks、reference-only 限制、roadmap `in-progress→done`、产物清洁度与最终 diff；完整外部使用/API 文档如需补充，转 `cs-docs`，不在实现中越权扩写。
- 清洁度：禁止调试输出、临时 TODO/FIXME、注释掉代码、无用 import、敏感 payload artifact、真实账号/客户 PII fixture、token/chat_id 日志。
- Git：2026-07-22 owner 已明确授权本 feature commit；只有 review、QA、acceptance 与最终门禁全部通过后，才可把本 feature 范围内实现、生成物与 CodeStable 产物合并为一次正常提交。不得纳入 `.workflow/`、`install-cpamp.sh`，禁止 `git commit --no-verify`；不得 push，除非 owner 另行授权。

## Handoff 条件

- 需要改变 approved design、reference-only 范围、OpenAPI/HTTP 公开契约、roadmap item 或 A1～A18。
- 独立 Task agent reviewer pending/failed/blocked 且没有 owner 明确降级授权。
- 同一失败项三轮修复仍不通过。
- Docker/PostgreSQL、浏览器或其他核心环境缺失，导致关键行为无法判断。
- owner 主动暂停、改方向或终止。
