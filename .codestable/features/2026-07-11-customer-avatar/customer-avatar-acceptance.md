---
doc_type: feature-acceptance
feature: 2026-07-11-customer-avatar
status: passed
accepted: 2026-07-13
round: 1
---

# customer-avatar 验收报告

> 阶段：阶段 3（验收闭环）  
> 验收日期：2026-07-13  
> 关联方案 doc：`.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md`（`status: approved`）  
> 工作区：`.claude/worktrees/customer-avatar`，分支 `feat/customer-avatar`，基线 `develop@ededf8de`

## 1. 接口契约核对

对照方案第 2.1 节名词层。

**接口示例逐项核对**：

- [x] PUT `/api/v1/customers/{id}/avatar` + quoted `If-Match` + multipart  
  → `backend/internal/platform/httpapi/customer_avatar.go` `putCustomerAvatarRoute`；OpenAPI `putCustomerAvatar`；`AvatarApplication.Set`。  
  实测：HTTP 集成测覆盖 ar-0→ar-1、same-content no-op、revision conflict、merged 409；与 design 示例一致。
- [x] GET `/api/v1/customers/{id}/avatar/content?v=…` + ETag/304/private,no-cache/Vary/nosniff  
  → `getCustomerAvatarContentRoute` + `AvatarApplication.ReadContent`；handler 不直接调用 store。  
  实测：200/304/缺 v 400/旧 v 409/无 token 401/跨账号 404 与 QA round 1 真实请求一致。
- [x] DELETE 条件移除（含 merged cleanup-only）  
  → `deleteCustomerAvatarRoute` + `AvatarApplication.Remove`；merged 匹配 revision 清 pointer 入 GC。  
  实测：application/HTTP 测试与详情页移除路径通过。
- [x] `AvatarObjectStore` port  
  → `backend/internal/customer/avatar_store.go`：`PutImmutable/Open/Stat/List/Delete`；local adapter `avatarstore.NewLocal`；in-memory 仅测试。一致。
- [x] `CustomerAvatar` / `CustomerPicker` 组件接口  
  → `frontend/src/components/customers/CustomerAvatar.tsx`、`CustomerPicker.tsx`；props 含 `avatarRevision`/`avatarUrl`/`candidateStatuses`/`selectedCustomer`/`excludeCustomerIds`。一致。

**名词层「现状 → 变化」逐项核对**：

- [x] `Customer.avatar_revision` 始终存在（`ar-{bigint}`）；`avatar_version`/`avatar_url` 成对可选 → OpenAPI required + model/repository 投影一致。
- [x] `CustomerSummary` 保留 id/display_name/channel/status，只增头像字段 → OpenAPI schema 与生成物一致。
- [x] 五字段 pointer + 独立 revision → migration `0008_customer_avatar` + repository columns/scan 一致。
- [x] `AvatarContent` / `ObjectRef` / store 错误分类 → application + local adapter 一致；temporary 错误不带 root 路径。
- [x] GC / checkpoint 表 → migration + `avatar_repository` / maintenance runner 一致。
- [x] 本地卷 + require-mount → config / compose / Dockerfile / README 双轨一致。

**流程图核对（2.2 mermaid）**：

- [x] UI→If-Match→规范化→PutImmutable→Customer 行锁→pointer CAS / GC enqueue → `avatar_application.go`。
- [x] READ：v 校验→完整缓冲验证→handler 200/304 → `ReadContent` + thin handler。
- [x] MaintenanceRunner：inventory + pointer audit + due GC → `avatar_maintenance.go`。
- [x] CustomerAvatar / CustomerPicker 消费鉴权 GET 与 customers 列表 → 前端组件与 caller 装配。

**偏差**：无未处理偏差。

## 2. 行为与决策核对

**需求摘要**：

- [x] 合法 JPEG/PNG/WebP 可上传并鉴权读取；pointer 指向不可变代次；移除/结果未知/迟到删除安全 → application + store + maintenance 测试与 QA 通过。
- [x] 跨账号不可见；merged 禁 PUT、允许 cleanup-only DELETE；archived 可写 → API/应用测试覆盖。
- [x] 列表/详情/转介绍/merge/订单/档期统一头像或 fallback → 组件测 + QA 浏览器证据 + caller 装配 grep。
- [x] 本地卷重建保留；备份/manifest 可执行 → QA A21 + implementation evidence。
- [x] 门禁全绿 → 本轮 final audit 重跑 build/lint/串行 go test/前端测试/generate-check。

**明确不做（反向核对）**：

- [x] 建档无头像字段/file input → `createCustomer` body 无 avatar；`CustomerNewPage` 无 file input。
- [x] 无 OSS SDK / OSS env / 预签名 → go.mod 与代码 grep 无命中；avatar_url 仍为 `/api/v1/...`。
- [x] 无裁剪/人脸/相册/原图模块。
- [x] GIF/SVG/HEIC 反例测试 400。
- [x] 媒体在 protected group；无 token query。
- [x] merge 本身不改 avatar pointer（`Service.Merge` 无 avatar 写面）；仅显式 DELETE cleanup。
- [x] JSON export 无二进制内嵌。
- [x] frontend 无新增 UI 组件库依赖。

**关键决策落地**：

- [x] D2 AvatarObjectStore 语义 port，非路径透传。
- [x] D3 同源鉴权媒体 URL。
- [x] D4 version / revision / object_id 三分离。
- [x] D5 图片边界与确定性规范化（imaging + 测试）。
- [x] D6 状态与 merge cleanup-only。
- [x] D7 新代次先落 + revision CAS + 精确 GC。
- [x] D8 OpenAPI + codegen tag 同步。
- [x] D9 CustomerAvatar 媒体生命周期。
- [x] D10 CustomerPicker caller 矩阵。
- [x] D11 展示范围一次收口（含订单/档期固定摘要）。
- [x] D12 require-mount 缺省 false / production true（QA-F001 已关）。
- [x] D14 signal-aware lifecycle + bounded runner wait（QA-F003 已关）。

**编排层变化 V1–V8**：契约、编排、计算、持久化、HTTP、展示、选择、运维八条均有代码落点（见挂载点）。

**流程级约束**：错误语义、事务/结果未知、并发/ABA、账号隔离、安全、缓存、生命周期、可观测、OSS 扩展点均由测试/实现证据覆盖；路径 hardening 与日志脱敏经 QA-F002/F004 关闭。

**挂载点反向核对**：

| 清单 | 代码落点 | 反向 grep |
|---|---|---|
| API 契约与三路由 | `api/openapi.yaml`、`router.go` 受保护 PUT/GET/DELETE、`customer_avatar.go`、codegen | 无匿名 avatar 路由 |
| DB schema | `0008_customer_avatar.{up,down}.sql`、scan/columns | pointer/GC/checkpoint 仅此 migration |
| 对象基础设施与运维 | `avatarstore`、`avatar_maintenance*`、`cmd/server` lifecycle、`cmd/avatar-manifest`、compose volume | adapter 不泄漏到 handler |
| 前端 UI 注入 | CustomersPage/Detail/New、ProfileForm、MergeDialog、OrderWorkspace、ShootOrderFlow + CustomerAvatar/Picker | 无第二套 picker |

- [x] 清单外引用：无（图片 helper / media cache 属组件内聚，design 明确不单列挂载点）。
- [x] **拔除沙盘**：移除三路由 + Avatar* 装配 + migration + volume + 四个前端注入点后，系统回到首字色块 + 纯文本选择；无额外隐藏入口。

## 3. 验收场景核对

验证证据来源：`customer-avatar-qa.md` round 2 `passed` + 本轮 final audit 重跑。

| 场景 | 结果 | 证据 |
|---|---|---|
| A1 门禁 | pass | build/lint/generate-check；串行 `go test` 全包；前端测试 |
| A2 契约/schema/config | pass | OpenAPI/migration/config 测试；unset require-mount 进程越过 config |
| A3–A7 上传/缓存/图 | pass | HTTP avatar + avatarimage 测试；QA 真实 JPEG |
| A8–A11 / A24 一致性 | pass | customer application/maintenance 故障注入 |
| A12–A14 状态/隔离/安全 | pass | API + symlink 矩阵 + 错误脱敏 |
| A15–A16 展示/写 UI | pass | 组件测 + QA 浏览器（trust-prior 本轮未重开浏览器） |
| A17–A20 picker/布局 | pass | 组件/schedule/layout 测 + QA 浏览器 trust-prior |
| A21 mount/备份 | pass | require-mount fail-fast 重跑；volume/manifest trust-prior QA/impl |
| A22 范围/清洁度 | pass | grep + lint + diff-check |
| A23 runner/shutdown | pass | maintenance A23 测试 + server lifecycle 四测 |

**review Test And QA Focus**：symlink 六层+叶子、lifecycle bounded wait、unset/invalid/false、slog 无 root → 均 re-verified。

**QA residual-risk**：均非核心缺口；接受为遗留（空态 ArrowDown、a11y、TOCTOU、双 runner、ECS fsync、并行 testcontainers 偶发、roadmap dirty 中与本 feature 无关的既有规划文件需 scoped commit 排除）。

**Evidence pack / DoD JSON**：none（非 gate 包模式）；DoD 由 design 3.y + QA + 本报告覆盖。

## 4. 术语一致性

- 客户 / 账号 / 订单 / 档期 等 CONTEXT 术语在新增代码中沿用；未用「用户」指摄影师。
- 公开契约字段：`avatar_revision` / `avatar_version` / `avatar_url` 与 design 一致。
- 组件名 `CustomerAvatar` / `CustomerPicker` 为前端名，未上升为领域实体。
- `AvatarObjectStore` / `avatar_object_id` 仅服务端内部，不下发。
- 防冲突：无 OSS/FileStore 公开命名污染。

## 5. 领域影响盘点（提示而非代写）

- [x] **新名词「客户头像 / AvatarObjectStore / avatar_revision 三分离」**：CONTEXT.md 尚无「客户头像」词条。design §4 写「若后续被其他模块复用再评估」。  
  **建议**：可走 `cs-domain` 评估是否加 CONTEXT 短词条；非阻塞（组件名不强制入词表）。
- [x] **结构性选择「PG pointer + 本地/未来 OSS 二进制 adjunct」**：触及 ADR-002 边界但不改其 PostgreSQL 主存结论。  
  **建议**：走 `cs-domain` 补 ADR（AvatarObjectStore、不可变 generation、pointer/GC、local→OSS），**不要直接改 ADR-002**。
- [x] **流程级约束「revision CAS + 进入 GC 即烧毁 + exact-generation Delete」**：稳定并发/恢复语义。  
  **建议**：实测有效后可 `cs-keep` 沉淀 generation 模式；本轮不代写 compound。

## 6. requirement delta / clarification 回写

- frontmatter `requirement: customer-profile` 指向 **current** req。
- 本次新增用户可感能力（可选头像与统一选择面），但 **不改变** pitch / 用户故事 / 边界（仍不建档强制、不替订单/档期职责）。
- 分支判定：`requirement` 指向 current 且未改用户视角边界 → **机械追加实现记录，不改愿景正文**。
- 已写：
  - `implemented_by` 追加 `2026-07-11-customer-avatar`
  - `last_reviewed: 2026-07-13`
  - 变更日志一条说明头像落地且边界不变
- 无独立 `*-req-delta.md`；因未改边界/用户故事，不触发 approval-report。

## 7. roadmap 回写

- `roadmap: photographer-private-crm` / `roadmap_item: customer-avatar` 均有值。
- [x] `photographer-private-crm-items.yaml`：`slug: customer-avatar` 原 `in-progress` + `feature: 2026-07-11-customer-avatar` → **`status: done`**；`validate-yaml.py` 通过。
- [x] `photographer-private-crm-roadmap.md` §3 子 feature 清单第 4 条状态同步为 **done**。
- 未改其他 planned 条目 scope。

## 8. attention.md 候选盘点

- [x] **候选 1（建议）**：本机 Docker Desktop 下默认高并行 `go test ./...`（Testcontainers）可能偶发 `port "5432/tcp" not found`；`-parallel=1` 稳定。下一个 feature 若再踩可考虑记入 attention「测试」节。  
  **不擅自写入**——需用户确认后 `cs-note`。
- 其余环境坑（8080 占用改端口、干净镜像 lockfile optional peer）偏一次性，归 learning 即可。

知识出口分流：

- compound / generation 模式 → `cs-keep`（见退出后）
- 用户可见头像操作与备份说明 → `cs-docs` tutorial（README 已有运维段，可补用户向）
- 公开 API 三端点 + Customer 字段 → `cs-docs` api（OpenAPI 已是机器契约）

## 9. 遗留

- CustomerPicker 空结果 ArrowDown `activeIndex=-1`（非核心）。
- option 稳定 ID / `aria-activedescendant`、Tab/blur 自动化不足。
- Lstat→I/O TOCTOU；启动期 PathError 可能仍进 main 日志。
- 跨进程双 runner lost-update 无定向双进程测。
- 目标 ECS 文件系统 fsync/rename 需上线机留证。
- 默认并行 testcontainers 偶发失败（环境）。
- 三个 photographer-private-crm roadmap 相关 dirty 中，**本验收只应提交 items.yaml + roadmap.md 的 status 回写**；`roadmap-review.md` 等若与本 feature 无关须 scoped 排除。
- design 2.5 观察：OrderWorkspace / customer repository 偏胖，后续 `cs-refactor`，本 feature 未扩拆。

## 10. 最终审计

- 验证证据来源：`customer-avatar-qa.md` round 2 `passed` + 本轮 accept 重跑命令
- Evidence pack / gate / DoD JSON：none
- 聚合命令（本轮 re-verified）：
  - `make build` → 0
  - `make lint` → 0（golangci-lint 0 issues；oxlint 0）
  - 临时 index generate-check → 0
  - `go test` 全后端包 `-count=1 -parallel=1` → 0（customer 39.6s、httpapi 18.4s、store 12.9s 等）
  - 前端 `test:customer-avatar` 6、`avatar-layout` 1、`schedule` 32、`package-price` 4、`api-client` 3 → 全绿
  - `docker compose config` → 0
  - unset `AVATAR_LOCAL_REQUIRE_MOUNT` 进程 → 越过 config 至 DB 连接失败
- 场景复核：`re-verified` ≈ 18（A1–A14、A22–A24 自动化 + config/lifecycle/symlink 重点）；`trust-prior-verify` ≈ 6（A15–A21 浏览器与 named-volume/manifest 全量破坏性恢复，依赖 QA round 1/2 与 implementation evidence，qa-fix 未改前端/HTTP/manifest 语义）
- trust-prior 比例约 25%（≤30%）；UI/运维项建议 owner 终审时抽查详情上传与 375px 即可
- 交付物复核：代码入口、OpenAPI、migration、compose volume、README 备份、avatar-manifest、feature 文档、roadmap done、req implemented_by → 均在工作区
- 完整工作区：大量 untracked feature 实现文件属交付物；0 staged；baseline roadmap-review dirty 需 commit 时排除
- diff 清洁度：pass（无 debug/TODO 生产污染）
- 知识沉淀出口：attention 候选 1 待确认；ADR/CONTEXT/`cs-keep` 已分流建议
- **结论：通过**

---

## 验收结论

- checklist 20 个 `checks`：**全部 `passed`**
- design / design-review / code-review / QA：**均 passed**
- roadmap `customer-avatar`：**done**
- requirement `customer-profile`：**current 保留，implemented_by 已追加**
- **status: passed**

待用户终审确认后，本 feature 的 cs-feat 工作流可关闭。后续 BUG 走 `cs-issue`。
