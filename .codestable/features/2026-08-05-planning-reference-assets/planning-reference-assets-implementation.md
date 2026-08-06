---
doc_type: feature-implementation
feature: 2026-08-05-planning-reference-assets
status: ready-for-acceptance
summary: S1-S8 已完成；3轮独立 review、QA 与 canonical gates 通过，进入 acceptance；REV-017 等非阻塞 residual 已记录
tags: [planning-media, implementation, evidence]
---

# Planning Reference Assets 实现记录

## S1：rights matrix、typed key、图片 pipeline 与 immutablefs seam

### 退出信号

`matrix exhaustive fixture 与 adversarial image corpus 先红后绿；avatar 既有 public interface、object bytes、key 和 manifest digest 零漂移`。

### 变更

- `backend/internal/platform/immutablefs` 提供领域中立的 immutable publish、safe relative path、verified open、exact remove、稳定 inventory、目录 fsync 和可配置私有临时目录前缀。
- `backend/internal/customer/avatarstore.Local` 委托 immutablefs 完成 publish/open/stat/list/delete；avatar 仍在 adapter 层保留 typed key、最大字节数、叶子 regular-file/symlink 检查、inventory/manifest 语义和原有错误映射。
- `planningmedia` 固化 8 个 source class、4 个 rights basis、3 个 purpose 的 closed matrix；新增 canonical planning object key 构造器，不接受路径分隔符、空代际或未知 rendition。
- 图片 pipeline 固化 JPEG/PNG/static WebP、20 MiB、最大边 12000、60MP、display 2560 长边和 PNG display re-encode；显式拒绝动态 WebP、GIF、超尺寸/超像素和解码失败，并验证 display 不携带 PNG 文本元数据。

### 验证证据

| 证据 | 命令 / 断言 | 结果 |
|---|---|---|
| matrix exhaustive | `go test ./internal/planningmedia -run 'TestRightsMatrixIsExhaustiveAndFailClosed|TestPlanningObjectKeyIsTypedAndCanonical' -count=1` | 通过；8×4×2×3 组合及未知 source/purpose 均 fail-closed |
| adversarial image corpus | `go test ./internal/planningmedia -run 'TestProcessImage' -count=1` | 通过；坏字节、GIF、20 MiB+、10000×7000 像素炸弹、动画 WebP、PNG tEXt metadata 均有断言 |
| immutablefs | `go test ./internal/platform/immutablefs -count=1` | 通过；immutable replay、safe path、verified open、inventory、exact remove |
| avatar characterization | `go test ./internal/customer/avatarstore -count=1` | 通过；public store contract、symlink/traversal/temporary error、key、content bytes、metadata bytes、manifest inventory digest 全绿 |
| cross-package regression | `go test -p=1 ./internal/customer ./internal/customer/avatarimage ./internal/customer/avatarstore ./internal/platform/immutablefs ./internal/planningmedia -count=1 -parallel=1` | 通过 |
| cleanliness | `git diff --check` | 通过；无新增 debug 输出、TODO/FIXME 或注释代码 |

### TDD / 说明

本轮恢复时 S1 的初版 pipeline/immutablefs 骨架已经存在；新增行为采用 characterization-first：先以既有 avatar 测试和固定 bytes/manifest 作为零漂移基线，再增加 closed-matrix、typed-key、图片 adversarial 与 metadata stripping 断言，最后完成 adapter 委托并复跑全量 targeted package。未把该证据误记为完整从零 RED→GREEN；后续 S2 起按 step 内 RED→GREEN→VERIFY 记录。

### 清洁度与范围

本步未引入 AI/provider、匿名分享、通用 BlobStore、production backup schema-v2、HTTP route 或前端行为；这些仍由后续步骤或后置 Epic 负责。

## S2：account-scoped schema、repository、inventory 与 restore fixture

### 退出信号

`真实 PostgreSQL + 临时 volume 证明 exact generation、稳定 inventory、缺失/孤儿/corrupt 分类与全成全败 restore`。

### 变更

- migration `0013_planning_media` 建立 asset、rights、generation、rendition、binding、lease、read pin 与 physical inventory 表；current generation 使用 deferred composite FK，rights declaration ID 与 exact asset/generation 一致，source×rights 约束在数据库层继续 fail-closed。
- gallery 使用固定名 `planning_media_asset_gallery` projection view；`AccountScope.QueryPage` 不再接收 raw JOIN 字符串，所有 `TxAccountScope.Insert` 均由基座唯一注入 `account_id`。
- repository 可按账号稳定列出 gallery 与 exact rendition inventory；内部 object key 只留在 maintenance projection，不进入公开 DTO。
- manifest 从 typed key 解析 asset/generation/rendition，稳定排序并计算整体 digest；restore 只接受空目标、完整且无额外对象的 source inventory，逐对象 verified open，任一失败清理已创建对象，完成后再次核对 manifest digest。
- inventory classifier 对 DB expected set 与 volume actual set给出 `missing | orphan | corrupt` 三类稳定结果。

### TDD 与验证证据

| 阶段 | 证据 | 结果 |
|---|---|---|
| TDD exception | 恢复现场时静态契约核对已确认：`AccountScope.Insert` 会自动注入 `account_id`，且 table guard 明确拒绝 raw JOIN；该未完成骨架在补 PostgreSQL fixture 前就无法形成合法执行路径，因此不虚构 RED 命令 | 先按既有 store contract 窄修复，再用 fresh PostgreSQL fixture 作替代证据 |
| GREEN | 去除业务层 `account_id` 列，新增固定 gallery projection view、composite constraints 与真实 integration fixture | repository 可经 scoped API 正常读写 |
| VERIFY | `go test ./internal/planningmedia -run 'TestPostgresPlanningMediaSchemaRepositoryAndAccountIsolation' -count=1 -parallel=1` | 通过；fresh PostgreSQL migrate、exact generation deferred FK、数据库 rights 拒绝、跨账号 gallery/inventory 隔离均成立 |
| VERIFY | `go test ./internal/planningmedia -run 'TestRestore|TestOpenVerified' -count=1` | 通过；稳定 manifest、非空目标拒绝、missing/orphan/corrupt、digest 篡改、第二对象写故障后的目标清空均成立 |
| CLEAN | `git diff --check` | 通过 |

### 清洁度与范围

S2 仍未注册运行时 route，也没有把 module restore 冒充 production schema-v2 备份；对象发布/DB rollback orphan 收口属于 S3，binding/lease/pin/GC 线性化属于 S4。

## S3：bounded spool、single upload claim、dual rendition 与 orphan 收口

### 退出信号

`A1/A4-A6 全绿；同 key 只有一份 request identity，replay 的 full decode/publish callback 为 0；claim crash 与所有 DB/ledger/对象故障点都有确定回滚或 48h orphan 清理证据`。

### 变更

- `UploadSpool` 将 multipart 图片写入 request-private 0600 临时文件，边读边计算 checksum/size，并在 full decode 前完成 magic sniff、声明 MIME 一致性和 20 MiB bounded read；Close 负责清理。
- upload canonical body 只引用 spool 的 checksum/size/sniffed MIME；executor callback 首次 claim 才读 spool、完整 decode、派生 display、发布 original/display 并写业务行与 physical inventory。
- upload asset/rights ID 由账号、plan、幂等 key 和固定 kind 做 deterministic UUID 派生，使 retry 可复用 exact immutable object identity；响应与 DOM 只保留 display access ref，object key 仍是私有字段。
- original/display 发布结果记录 created metadata；callback、数据库事务或 store-success ledger 更新失败时，外层对 newly-created objects 做 best-effort `RemoveExact`，失败对象由 reconciliation inventory 接管。
- `ReconcilePhysicalOrphans` 按 DB expected rendition set 与 volume actual set 做三类差异，记录 first_seen，只有 physical metadata mtime 和 first_seen 都超过 48h 才 exact-delete；checksum/size 不匹配时 fail closed。
- idempotency migration 将五个 planning-media operation 纳入既有 ledger closed check；resource/operation frame 由 typed matcher 校验。

### TDD / 验证证据

| 阶段 | 证据 | 结果 |
|---|---|---|
| TDD exception | 进入本步时 application 骨架已有 `[]byte` 输入和双 publish；先用静态 contract 核对锁定必须改为 spool/单 canonical claim，再以 targeted fixture 作为替代 RED→GREEN 证据，不把旧半成品测试冒充红灯历史 | 记录真实边界 |
| spool / adversarial | `go test ./internal/planningmedia -run 'TestUploadSpool' -count=1` | 通过；bounded size、magic sniff、checksum、private file cleanup、MIME mismatch |
| replay / fault | `go test ./internal/planningmedia -run 'TestPostgresUploadSingleCanonicalClaimReplayAndObjectFaultCleanup' -count=1 -parallel=1` | 通过；fresh PG 下 first/replay callback 只读 spool 一次、same key 异 body conflict、双 rendition inventory、display publish fault 清空物理对象、48h orphan first_seen/删除 |
| ledger failure | `go test ./internal/planningmedia -run 'TestPostgresUploadLedgerStoreFailureCleansPublishedObjects' -count=1 -parallel=1` | 通过；注入 idempotency store-success update 失败，PG 事务回滚且已发布对象清理 |
| idempotency contract | `go test ./internal/platform/idempotency -count=1` | 通过；planning-media operation/resource frame 可进入既有 executor |
| cleanliness | `git diff --check` | 通过 |

### 清洁度与范围

S3 没有开放原图 route、AI/provider、匿名分享或新的 ledger；handler 的 runtime route/error/header 矩阵、binding/permit/GC 仍由 S4/S5 收口。

## S4：binding、lease、read pin 与 GC 线性化（已完成）

### 当前收口

- binding create 先锁 asset，再验证 HolderProof、exact generation/rights/purpose，并将重复 active binding 映射为显式冲突；binding release 重新计算 `gc_eligible_at = max(released_at, created_at + 48h)`。
- lease reserve 重新读取 generation rights、校验 active binding、holder read proof、bounded owner 字段与最长 24 小时 expiry；lease release 先定位 asset 再锁 asset/lease，not-found 与 released/expired 均保持确定错误语义。
- display read 分 active binding 与 staged upload-context 两类 authorization anchor，建立对应 read pin；读取时重新查询 exact generation rights 并按 binding purpose / staged moodboard purpose 做 `ValidatePurpose`，随后使用 verified metadata/checksum 打开对象；对象缺失或完整性失败会在独立事务把 exact generation 标成 `corrupt`。
- GC 改为 claim（asset row lock、无 active binding/lease/pin、revision+1、`gc_pending`）→事务外 `RemoveExact` → finalize（校验 claim revision/generation、`deleted`、清理 inventory）；删除失败保留 `gc_pending` 以便重试。

`OpenDisplay` 已提供内部 typed `DisplayStream` seam：事务内完成 holder/rights/purpose 校验与 read pin 签发，提交后 `OpenVerified`，HTTP 使用 `DataFromReader`，`DisplayStream.Close` 幂等释放 pin。旧 `ReadDisplay` bytes API 保留给兼容调用，但 HTTP 不再使用它。

## S4 fixture / S5 static closure 增量

- 新增 `TestPostgresPlanningMediaBindingReadAndGCLifecycle`：真实 PostgreSQL + Local immutablefs 验证 bound read 的 binding anchor、detach 后 `max(asset.created_at+48h, released_at)`、staged read 的 upload-context anchor，以及 exact GC 删除双 rendition 后 finalize；同一 fixture 注入 physical delete failure，确认失败保留 `gc_pending` 并可重试。
- 同一 fixture 增加 blocking object-open 双 goroutine probe：permit 签发事务提交后 detach 可继续，response stream 仍保持可读；active pin 在 stream Close 前阻止 GC finalize，Close 后 GC 才可继续。
- OpenAPI 补齐 `UploadPlanAssetResult.generation`，并增加完整的 `PlanAssetGeneration` / `PlanAssetRightsDeclaration` response schemas；`make generate-check` 已通过，Go/TypeScript generated types 与 contract 同步。
- planning-media HTTP handler 补齐 corrupt→503、binding/state/gc/plan lifecycle→409、rights/grant→400、holder authorization unavailable→503 的错误矩阵。

S4 的 issue-vs-detach / issue-vs-GC fixture 已闭合：stream open 期间 detach 可以完成但 GC 看到 active pin 不得 finalize；Close 后 GC 才可继续。该 seam 不要求新增 design review。

### 预验证

本轮已通过：

```text
gofmt -w internal/planningmedia/application.go internal/planningmedia/repository.go internal/shootplanning/media_holder.go
go test ./internal/planningmedia ./internal/shootplanning -count=1
go test ./internal/planningmedia -run 'TestPostgres|TestRestore|TestGC|TestBinding|TestLease|TestRead' -count=1 -parallel=1
git diff --check
make generate-check
cd frontend && npm run test:planning-media && npm run build && npm run lint
```

S4 A7-A10 主路径与 lifecycle fixture 已完成；仍保留后续可增强的高并发 stress coverage，但不影响本 feature 当前线性化 exit signal。

`OpenDisplay` streaming seam 的验证还包含：stream active 时 `ReconcileGC` 返回未删除，`DisplayStream.Close` 后同一 GC 调用可 exact-delete/finalize；HTTP content handler 已切换到 `DataFromReader` 并声明/发送 ETag、Cache-Control、Content-Length、nosniff。

## S7 additive Run Mode projection

- `planningmedia` 新增 `BatchShotAccessRefsInScope`，使用单个固定 projection view 查询 active shot bindings（`shot_reference_display`），稳定按 shot/created_at/binding 排序并最多返回 100 条，未知/不可用素材不会阻断 session open。
- `shootplanning` 通过可选 `ShotAccessRefProjector` 注入同一事务；open-session 首次 callback 在加载 shots 后写入 `Shot.AssetAccessRefs`，idempotency replay 不重新执行 callback/query。
- server composition 已先创建 planning-media application，再以 projector 注入 shootplanning application；OpenAPI/generated `ShootPlanShot.asset_access_refs` 已同步。
- Run Mode 新增 execution-only、只读 `本镜参考` sheet；每张图通过 Bearer display fetch，加载失败仅显示“部分参考素材暂不可用，不影响现场记录”，不阻断 capture/skip。移动端使用两列、按钮保持 coarse-pointer 触控尺寸。

验证：`make generate-check`、shootplanning/planningmedia/server/store targeted Go tests、`npm run test:planning-media`、frontend build/lint 均通过。

## S8：全面验证与交付审计（已完成）

- canonical `make generate-check` 通过，OpenAPI、Go contract 与 TypeScript generated schema 无漂移。
- `go test -p=1 ./... -count=1 -parallel=1` 通过；Testcontainers 使用串行门禁，避免已知 mapped-port 并发抖动。
- `make check` 通过，包含 Go build、golangci-lint、前端 build/lint 与全仓测试；Vite 大 chunk warning 记录为非阻塞性能 residual。
- `git diff --check` 通过；未发现新增 debug 输出、匿名 media route、公开静态对象目录、object key DTO、AI/provider 依赖或重复 ledger。
- migration down 依赖顺序已按 `read_pins → leases → bindings → view → renditions → assets → generations → rights` 收口，并由 planning migration focused tests 验证。
- 外部 stage-1/stage-2 evidence gate 与 production-shaped rehearsal 继续保持 pending；本 feature 不消费或替代这些 owner checkpoint。

S8 exit signal 已满足，下一步为 implementation.before_review gate 与第 1 轮独立 code review。

## Implementation gate ledger

| Step | Status | Evidence |
|---|---|---|
| S1 | done | rights matrix、immutablefs、avatar characterization、image adversarial tests |
| S2 | done | fresh PostgreSQL schema/repository/inventory/restore fixtures |
| S3 | done | bounded spool、single claim、dual rendition、orphan cleanup/idempotency fault fixtures |
| S4 | done | binding/lease/read pin/GC lifecycle、OpenDisplay Close seam、issue-vs-GC fixture |
| S5 | done | OpenAPI/generated types、route composition、error/header matrix、`make generate-check` |
| S6 | done | planning-media frontend contract、build/lint、staged/active/corrupt UI states |
| S7 | done | BatchShotAccessRefs projection、server injection、Run Mode reference sheet、non-blocking fetch failure behavior |
| S8 | done | canonical `make check`, scope/DoD/evidence cleanliness and final diff audit |

`make check`、生成、构建、lint 与串行全仓测试均已通过；implementation gate 结果已落盘，review 与 QA 已闭环，进入 acceptance。

## Review / QA closure（2026-08-06）

- 第 1–3 轮均由独立 subagent 完成；第 3 轮是 owner 明确批准的最后一轮 review，review round cap=3，未启动第 4 轮。
- REV-010–REV-015 已在第 2/3 轮前完成修复。第 3 轮发现的 REV-016（CreateBinding malformed required 字段可能落 500）在 review 后做了一次 owner-cap 窄修复：application 层对 plan/asset/generation/revision/holder 输入统一返回 `planningmedia.ErrValidation`，HTTP 层稳定映射为 `400 validation_failed`，且在幂等/数据库/HolderProof 前 fail closed。
- REV-016 的窄修复由独立 QA closure 重跑并验证：`make generate-check`、planningmedia/httpapi/shootplanning targeted Go tests、frontend planning-media tests/build/lint、`make check`、`git diff --check` 全部通过。该验证不是第 4 轮 review，也没有改变 round=3 的审查上限。
- review 报告已按当前源码状态标记为 `passed`，并保留 REV-017 与 HTTP multipart/边界 fixture 等非阻塞 residual；这些不承载首版核心验收缺口。
- QA 报告 `planning-reference-assets-qa.md` status=`passed`；QA 明确记录真实 HTTP multipart negative fixture、projection 边界、真实浏览器 375/coarse/200% 证据、provider signals 和 REV-017 为后续 residual，不将其伪装为已完成。
- 外部 `stage-1-evidence-go`、`stage-2-evidence-go`、`production-shaped-rehearsal` 仍按 Goal protocol 保持 pending，本 feature 不消费或替代这些门禁。

## Acceptance handoff

当前 implementation gate、review-evidence、QA-evidence 与 DoD 证据均已具备；下一步是创建并通过 feature acceptance、把 checklist checks 回写为 `passed`，再同步 roadmap/goal-state 并执行一次受权 scoped commit。AI/provider、AI 脚本/分镜、生图和知识库维护继续属于后置 Epic，不在本 feature 的验收范围内。

### 流程约束

该 feature 遵守 owner 已批准的 review-round cap：实现完成后的独立 code review、QA closure 与 acceptance 不能通过自动追加第 4 轮完整复审规避；第 3 轮后非阻塞项进入 residual risk，仍有 blocking 则生成 handoff 并停止该 feature。
