---
doc_type: feature-acceptance
feature: 2026-07-21-data-export
status: passed
accepted: 2026-07-22
round: 1
---

# data-export 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-22
> 关联方案 doc：`.codestable/features/2026-07-21-data-export/data-export-design.md`
> 验收结论：通过。第 1～9 节、24 项 checks 与最终审计均已完成。

## 1. 接口契约核对

对照方案第 2.1 节逐项核查，未发现实现与批准契约的偏差。

- [x] 应用与存储接口：`dataexport.Repository.LoadSnapshot(ctx, AccountScope)`、`Service.Build(ctx, AccountScope)`、`Clock.Now()` 与 `AccountScope.WithReadSnapshot(ctx, func(ReadTxAccountScope) error)` 均真实落盘；caller 不能取得原始 `pgx.Tx`，`ReadTxAccountScope` 不暴露写方法。
- [x] HTTP 正常示例：受保护的 `GET /api/v1/export` 返回 `application/json` 附件；`Content-Disposition`、`Content-Length`、`Cache-Control: no-store`、`X-Content-Type-Options: nosniff` 与设计一致。
- [x] HTTP 错误语义：无认证 401；查询、scan、Settings、commit、映射和预序列化等发送前失败走 500；已取消请求不写成功附件；headers 后 transport 失败不尝试第二封套。
- [x] OpenAPI 名词层变化：`/export` 的 200 response 通过 `$ref` 指向命名 `ExportDocument`；`ExportDocument` / `ExportCounts` 保持 required、`additionalProperties: false` 与非负 counts，Go/TS 生成物均有对应顶层类型。
- [x] 导出文档 shape：固定 `exported_at`、`schema_version=1`、`counts`、七类数组与有效 `settings`；counts 由最终数组长度派生，空集合为 `[]`。
- [x] Customer 公开投影：可包含 `avatar_revision`、`avatar_version`、`avatar_url`；最终 OpenAPI projection 不含头像字节、内部 object ID/key、manifest、GC 或 reconciliation 状态。
- [x] 前端接口：`fetchDataExport` 发送 Bearer，严格解析 JSON media type 与固定文件名，只有完整读取 Blob 后才返回 `{blob, filename}`；错误复用 `ApiError`，没有手写重复 Export DTO。

流程图已按实际调用链复核：`SettingsPage → DataExportCard → requestAndDownloadDataExport → fetchDataExport → protected GET /export → dataexport.Service → PostgresRepository → WithReadSnapshot → PostgreSQL`，成功后才创建并释放临时 object URL。图中每个节点与调用关系均有代码落点。

## 2. 行为与决策核对

### 需求摘要与关键决策

- [x] 认证账号可一次下载当前账号的七类结构化实体与有效 Settings；真实 PostgreSQL、HTTP 与隔离浏览器证据均通过。
- [x] 七类读取共享同一个 `REPEATABLE READ, READ ONLY` 账号快照；事务能力面和数据库模式形成双层只读守护。
- [x] 空账号返回七个 `[]`、全零 counts 与完整默认 Settings。
- [x] 无 token 返回 401；跨账号集成测试证明 B 看不到 A，客户端不存在 `account_id` 切换面。
- [x] 设置页常驻 PII 保管、及时删除及头像 reference-only / 不可跨部署恢复说明。
- [x] 全部公开对象映射与 JSON bytes 完成后才发送成功 headers；附件文件名使用与 `exported_at` 相同的 UTC 时点。
- [x] D1～D12 均按批准方案落地：reference-only schema v1、独立跨域读模型、单快照、len-derived counts、有效 Settings、OpenAPI allowlist、预序列化、安全 headers/tag/router、独立下载卡片、fail-closed/脱敏日志、零 migration。

### 明确不做与范围守护

- [x] 未新增头像媒体包、import/restore/upload、分页/filter、zip、streaming、async/history/progress、断点续传或版本协商。
- [x] 未导出凭证、Account password hash、JWT、bot/bind token、idempotency、扫描 checkpoint、Telegram delivery/update/log 等内部状态。
- [x] 未新增跨账号管理员导出、数据库表/migration、服务端临时导出文件或浏览器持久化。
- [x] “全量”只包含数据库仍存在的历史/终态行，不补造物理删除记录；所有既有终态均有集成 fixture。
- [x] 同步全内存物化是 owner 已批准的 v1 边界，未在本 feature 偷加半成品流式/异步路径。

### 挂载点反向核对与拔除沙盘

- [x] **MOUNT-1 — OpenAPI / codegen**：`api/openapi.yaml`、`backend/oapi-codegen.yaml`、生成的 `api.gen.go` 与 `schema.d.ts` 对齐。移除该组会立即使公开契约、生成类型或 generate-check 失配。
- [x] **MOUNT-2 — HTTP 暴露面**：`router.go` 只在受 auth 保护的 group 注册 `GET /export`；`main.go` 注入真实 Service，handler 以 AccountContext 构造 AccountScope。移除 route 后后端能力不可达，未知 API 404 保持不变。
- [x] **MOUNT-3 — 设置页公共 UI**：`SettingsPage` 在 Settings loading/error/success 三种状态均挂入独立 `DataExportCard`。移除该挂载后摄影师失去一键入口，但其他 Settings 交互不受影响。
- [x] **反向检索**：先用 CodeGraph 获取 `ExportAll`、`ExportDocument/Counts`、`DataExportCard`、`fetchDataExport` 的生产调用路径，再用 `rg` 枚举生产引用；所有公开引用均归入 MOUNT-1～3。`dataexport`、`scope_tx`、HTTP adapter、composition root 与 `dataExportDownload` 是上述挂载点的内部实现，不是额外公开挂载点。
- [x] **拔除沙盘**：按 MOUNT-3 → MOUNT-2 → MOUNT-1 逆序移除，再删除内部 adapter/read-model/snapshot 与定向测试，可完整卸载本功能；没有数据库 migration、持久化导出表、临时文件目录、后台任务或浏览器存储残留。

## 3. 验收场景核对

验证证据来源为已通过的 `data-export-qa.md`，并复核 `data-export-review.md`、evidence pack、DoD 与 gate 结果。QA 为 functional feature，真实执行了 PostgreSQL、HTTP、前端测试和隔离浏览器核心路径；0 failed / 0 blocked。

| 场景 | 可观察证据 | 验收结果 |
|---|---|---|
| A1 | canonical 临时 index `make check`、CMD-002 均 exit 0；普通未提交工作区只在 HEAD-based generate-check 命中预期生成物差异 | 通过 |
| A2 | 命名 OpenAPI schema、双端生成物、真实七类 route 文档与 counts exact comparison | 通过 |
| A3 | 隔离空账号下载解析为七个 `[]`、全零 counts、完整默认 Settings | 通过 |
| A4 | 各历史/终态与乱序/tie-break fixture 按 `created_at ASC, id ASC` 全部出现 | 通过 |
| A5 | 无 token 401、A/B 账号隔离、客户端无 `account_id`；浏览器旋转 JWT 后转 `/login` | 通过 |
| A6 | 公开头像引用可见；结构键与唯一哨兵负向扫描无字节、内部 ID/key、manifest/GC/reconciliation | 通过 |
| A7 | allowlist query/projection 与结构扫描排除 password/JWT/token/idempotency/checkpoint/delivery/log；`telegram_chat_id` 只按 Settings 输出 | 通过 |
| A8 | 并发写发生于首类查询后，当前导出保持旧快照，下次导出看到新提交 | 通过 |
| A9 | 空 scope、缺依赖、query/scan/settings/commit/mapping/serialize/cancel/writer fault 均 fail closed；完整 middleware 四象限通过 | 通过 |
| A10 | JSON、Content-Length、UTC 文件名、no-store、nosniff 与 `exported_at` 同一时点 | 通过 |
| A11 | 同一静态数据连续导出，清除 `exported_at` 后文档相等，数据库无写 | 通过 |
| A12 | 前端 8/8 覆盖 Bearer、严格 MIME、whole filename、500/401、body/blob rejection | 通过 |
| A13 | 隔离浏览器覆盖成功、500/retry、401、Settings failure、body interruption、快速重复点击；失败零下载，自动化证明失败零 object URL/click | 通过 |
| A14 | 1280×900、375×812、DPR/scrollWidth/bounds 与 Enter 下载证据；PII/头像文案常驻、无横向溢出 | 通过 |
| A15 | route/diff/cleanliness 扫描无范围外能力、临时文件或持久化；未知 API 404 | 通过 |
| A16 | 部分/完整阈值、Telegram chat ID 与 `GET /settings` 有效值逐字段 parity | 通过 |
| A17 | query recorder 证明七表与 Settings 各至多一次批量读，无 N+1、无 `count(*)` | 通过 |
| A18 | 同 transaction session 得到 `repeatable read` / `on`，类型断言证明无写 delegate 或原始 transaction | 通过 |

Review 第 5 节重点已全部覆盖：取消四象限、Clock-before-Repository、七类全字段 route、Settings deep parity、连续导出和完整下载解析均有证据。Residual risks 没有承载核心验收缺口：同步内存规模、headers 后不可逆网络断连、跨部署头像不可恢复、typed-nil/partial writer 建议与既有 Vite chunk warning均按批准边界保留。

Evidence pack、`data-export-dod-results.json`、`data-export-gate-results.json`、`data-export-dod-contract-results.json` 均为 passed，blocking DoD 有相应 pass evidence。

## 4. 术语一致性

- `Data Export / 全量导出`、`Export Document / 导出文档`、`Export Snapshot / 导出快照`、`Effective Settings / 有效设置`、`ReadTxAccountScope`、`DataExportCard` 与设计命名一致。
- OpenAPI、Go、TypeScript 与 UI 对外统一使用 export / data export；`schema_version` 只描述公开导出文档版本，没有混称数据库 migration。
- 本 feature 生产 diff 的禁用词检查无新增“用户”；界面使用“账号”“客户”“摄影师”，符合 `CONTEXT.md`。
- UI 没有“完整备份”“可恢复头像”“从所有备份删除”等冲突表述。

## 5. 领域影响盘点（提示而非代写）

- [x] **术语候选**：`Data Export / 全量导出`、`Export Document / 导出文档`、`Export Snapshot / 导出快照` 当前不在 `requirements/CONTEXT.md`。建议后续走 `cs-domain` 判断是否把它们补入术语表；本验收不直接修改 CONTEXT。
- [x] **已有术语**：Customer、Social Identity、Account、Settings 等均已在 CONTEXT 定义；Effective Settings 是既有 Settings 的读取语义，不新增业务实体。
- [x] **结构性选择**：实现遵守 ADR-001 账号隔离、ADR-002 PostgreSQL 主存、ADR-003 Gin/OpenAPI 薄 handler、ADR-004 头像二进制 adjunct/reference-only 边界；专用 read-model 与 read snapshot 是这些决策下可回退的内部实现，不满足新增 ADR 的门槛。
- [x] **未来决策边界**：async export storage、跨版本 import/restore 或媒体便携包会改变长期能力与存储边界，届时需先走新的 `cs-req`/design，必要时再由 `cs-domain` 记录 ADR。

## 6. requirement delta / clarification 回写

本 feature 不更新 requirement 文件。

理由：批准 design 的 `requirement: null` 是 owner 已确认的 clarification；data-export 的用户可见能力此前已由批准的 roadmap §4.6 与 OpenAPI 承载，本轮只是机械实现既有能力，并把 owner 选择的 reference-only 边界落实为代码、UI 与证据，没有新增或改变长期 capability boundary。因此不自由 backfill/rewrite requirement，也不生成 approval report。

若未来承诺 import/restore、媒体便携性、跨部署恢复或版本兼容，必须另走 `cs-req` / owner-approved delta；不能从本次 JSON 下载能力静默扩张。

## 7. roadmap 回写

- [x] 方案 frontmatter 的 `roadmap: photographer-private-crm` 与 `roadmap_item: data-export` 两字段完整。
- [x] 回写前 items 条目为 `status: in-progress` 且 `feature: 2026-07-21-data-export`，映射一致。
- [x] `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml` 已改为 `status: done`。
- [x] 主 roadmap 第 5 节第 11 条已同步为 `状态：done`；feature ID、reference-only、counts、PII 与头像限制备注保持不变。
- [x] YAML 校验纳入最终审计。

## 8. attention.md 候选盘点

- 已有知识：Docker/Testcontainers 高并行偶发 `port "5432/tcp" not found` 已写入 `attention.md`，本次无需重复。
- 新候选：隔离浏览器 QA 启动本地 backend 时，应显式覆盖并核验实际数据库连接，避免工作区 `.env` fallback 让进程误连非隔离数据源。该经验建议验收后由 owner 决定是否通过 `cs-note` 加入 `attention.md`；本阶段不直接写入。
- 临时 Git index 让未提交 OpenAPI 生成物参与 canonical generate-check 的做法可作为 CodeStable/OpenAPI workflow 经验候选，建议后续用 `cs-keep` 沉淀，不写入项目运行注意事项。

## 9. 遗留

- 后续优化点：code review suggestion 保留 `Clock/Repository` interface typed-nil 局部防御与 partial writer 额外测试；均不影响当前生产装配或核心契约，未作为本 feature 阻塞项开 issue。
- 已知限制：同步全内存物化没有文件大小、耗时、内存或并发导出硬上限；这是 owner 已批准的 v1 边界。
- 已知限制：200 headers 发出后的网络断连不可逆；当前以 Content-Length、完整 Blob 读取、失败不下载和可重试收敛。
- 已知限制：`avatar_url` 是当前部署的 reference-only 引用，不是媒体备份，不承诺跨部署恢复。
- 已知限制：Vite production build 有既存单 chunk >500 kB warning；build 退出码与本 feature 功能证据均通过。
- 文档出口：摄影师使用指南与公开 API 参考按 design 明确在 acceptance 后提示 `cs-docs`；不把外部文档扩入本 feature 阻塞范围。
- 实现阶段顺手发现：旧 driver 留下的进程误用工作区 `.env`；相关证据已作废，确认只读、完成下载/进程/容器清理并以显式隔离证据替换，没有产品代码遗留。

## 10. 最终审计

- **验证证据来源**：`data-export-qa.md`，round 1 `passed`；功能性核心路径同时有本轮重新执行的自动化证据，不以 QA 自述替代终审。
- **Evidence sources**：`data-export-evidence-pack.md`、`data-export-dod-results.json`、`data-export-gate-results.json`、`data-export-dod-contract-results.json`、API sample、浏览器 capture metadata 与四张隔离截图。
- **聚合命令 CMD-001**：临时 Git index 以当前 Go/TS 生成物为比较基线执行 `make check`，exit 0；覆盖前后端 build/lint、串行全仓 Go 测试、全部前端脚本与 generate-check。真实 Git index 前后均为空。
- **生成物 CMD-002**：同一 canonical 临时 index 执行字面 `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts`，exit 0。
- **后端 CMD-003**：`go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1`，exit 0；store 20.961s、dataexport 8.338s、httpapi 25.821s、server 0.661s。
- **前端 CMD-004**：`npm run test:data-export && npm run build`，exit 0；data-export 8/8，TypeScript/Vite production build 通过。仅保留已知单 chunk >500 kB 非阻塞 warning。
- **场景复核**：`re-verified 17 / trust-prior-verify 1`。A1～A13、A15～A18 由本轮命令、静态范围审计及 QA 的真实 PostgreSQL/API 证据复核；A14 的精确 viewport、DPR、边界框、键盘与肉眼文案依赖已通过 QA 截图/metadata，标为 `trust-prior-verify`。A13 核心下载与失败不落坏文件路径有本轮自动化重跑，不是纯 trust-only 浏览器 gate。
- **交付物复核**：代码、OpenAPI/config、Go/TS schema、受保护路由、Settings UI、测试、中文流程文档、架构/requirement 结论与 roadmap `done` 均真实存在；零 migration、零 import/restore、零媒体包。
- **完整工作区复核**：`git status`、tracked diff 与 untracked files 均纳入判断。本功能新增/修改路径全部属于 feature、roadmap、Makefile、api、backend 或 frontend；`.workflow/`、`install-cpamp.sh` 是既有范围外未跟踪项，保持未修改且不纳入提交。
- **diff 清洁度**：`git diff --check`、scope gate、debug/TODO/FIXME/XXX、禁用词与范围漂移扫描通过；未发现调试输出、注释掉代码、无用 import、敏感 payload、下载 JSON、临时服务或容器残留。
- **知识沉淀出口**：隔离 QA 的 `.env` 数据源核验登记为 attention/cs-note 候选；临时 Git index 的 OpenAPI generate-check 用法登记为 cs-keep 候选；Data Export 术语登记为 cs-domain 候选；摄影师指南/API 参考登记为 cs-docs 候选。
- **结论**：通过。原始契约满足，验证证据在最终工作区仍成立，承诺交付物已落盘，24/24 checks 全部 `passed`，没有未处理验收缺口。
