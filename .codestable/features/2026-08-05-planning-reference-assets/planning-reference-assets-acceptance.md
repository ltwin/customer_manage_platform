---
doc_type: feature-acceptance
feature: 2026-08-05-planning-reference-assets
status: passed
audit_state: completed
audit_reason: ""
auditor_id: ""
acceptance_authorization_ref: "approval-report.md#goal-acceptance"
accepted: 2026-08-06
round: 1
---

# planning-reference-assets 验收报告

> 阶段：阶段 3（Goal acceptance 闭环）
> 验收日期：2026-08-06
> 关联方案：`.codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design.md`
> 授权：`.codestable/roadmap/creative-shoot-planning/approval-report.md#goal-acceptance`，与 Goal state 的授权 ref 精确匹配。

## 1. 接口契约核对

- [x] 上传契约：`POST /api/v1/shoot-plans/{plan_id}/assets` 使用 Bearer、AccountScope、`Idempotency-Key` 与 multipart `file/rights`；服务器按 source×rights×purpose matrix 裁决，响应只返回 typed display access ref、generation 和审计元数据，不返回 object key。
- [x] Gallery/content：gallery 只返回当前账号可见的 display projection；`GET .../assets/{ref}/content` 经过 HolderProof、binding/upload-context、rights、purpose、generation integrity 与 AssetReadPin，HTTP 使用 `DataFromReader` 发送 ETag、Cache-Control、Content-Length 和 nosniff headers。
- [x] Binding/lease：plan/shot binding、release、AssetLease reserve/release 均使用 typed revision 与 idempotency frame；重复 active binding 返回既有 binding，非法 required/enum/minimum 输入统一为 `400 validation_failed`。
- [x] Run Mode projection：`RunInputSnapshot.shots[].asset_access_refs[]` 由同一事务中的 `BatchShotAccessRefsInScope` 首次投影并写入 stored response；replay 直接返回首次 snapshot，不重新执行 media query/callback。
- [x] 名词层“现状→变化”：`PlanAsset`、immutable generation、original/display rendition、rights declaration、HolderProof、AssetLease、AssetReadPin、opaque `AssetAccessRef`、physical inventory 与 module restore 均在 `backend/internal/planningmedia`、`shootplanning/media_holder.go`、OpenAPI/generated types 和前端面板/Run Mode 中有实际落点。
- [x] 流程图落点：spool→single claim→双 rendition→DB/ledger/orphan reconciliation、binding→permit/read pin→stream、GC claim→physical delete→finalize、Run Mode stored projection 的节点均有源码与 PostgreSQL/object fault fixture 支撑。

## 2. 行为与决策核对

### 需求与明确边界

- [x] 支持策划工作台上传、gallery 浏览、状态反馈、绑定整案/Shot、解绑、展示读取和 Run Mode `本镜参考`；素材不可用只提示，不阻断现场 capture/skip。
- [x] rights matrix、图片格式/尺寸/像素/动画 WebP/PNG metadata 限制均由服务端和 versioned pipeline 裁决；客户端 `permitted_uses` 不能放宽权限。
- [x] 账号隔离覆盖 gallery、asset/rendition/binding/lease/read-pin/inventory、HolderProof 与 object access；跨账号或不存在资源 fail closed。
- [x] 明确不做：AI/LLM、AI 脚本、AI 分镜、生图、知识库维护、OCR/相似图搜索/自动版权判断、视频/GIF/SVG/PDF/RAW/PSD、通用附件中心、匿名分享、生产 schema-v2 备份与真实 production rehearsal。

### 关键决策落地

- [x] D1/D12：planningmedia 持有媒体身份/物理生命周期；shootplanning 持有 holder 归属/生命周期/revision；二者用同一 `TxAccountScope` 的 `HolderProof` 协作，`immutablefs` 只提供中性文件 primitive，avatar 保留 typed adapter 与 characterization。
- [x] D2/D9/D10：一次上传创建 immutable asset/generation，upload operation/resource/canonical body/stored response 四元组版本化，object store 不被伪装成 PostgreSQL 事务，失败由 rollback/orphan reconciliation 收口。
- [x] D3–D8：binding closed union、purpose/rights 逐层重检；lease 只延长 liveness；permit 建立 binding 或 staged upload-context read pin；Run Mode 只读 display ref 且 replay 不重新查询。
- [x] D11：GC 采用 asset row lock 下的 claim→exact physical delete→finalize，失败保留 `gc_pending`，inventory 对 missing/orphan/corrupt 稳定分类。

### 挂载点反向核对

- [x] 清单挂载点均可定位：migration/repository/store/inventory、OpenAPI/Go/TS generated types、HTTP/router/composition、frontend planning panels/Run Mode、avatar/immutablefs、shared-tx ingestion seam 与测试/证据均存在。
- [x] 已对 `planningmedia`、`planning_media`、`asset_access_refs`、`PlanningMediaPanel`、`OpenDisplay`、`AssetReadPin` 做 `rg` 反向扫描；没有发现清单外的匿名 media route、公开 static 目录、object key DTO、AI/provider 依赖或第二套 ledger。
- [x] 拔除沙盘：移除 planning-media route/composition、projection 注入、frontend panel 与 migration 后，不会留下可达公开媒体 surface；`immutablefs` 仅由 typed avatar/planningmedia adapter 使用，不成为通用 BlobStore。

## 3. 验收场景核对

| 场景 | 结果与证据 |
|---|---|
| A1 owned JPEG + moodboard 上传 | 通过；matrix/pipeline、真实 PG schema/repository、original/display inventory 与 staged gallery fixture。 |
| A2 official/fan/customer-supplied/unknown web generation reference | 通过；closed matrix、binding/lease/permit negative tests，客户端用途声明不能越权。 |
| A3 licensed generation grant | 通过；rights grant 必须显式覆盖 generation purpose，strict JSON 与 generation rights fixture 通过。 |
| A4 adversarial image corpus | 通过；magic/MIME、GIF/SVG、动态 WebP、20MiB、12000 边界、60MP、坏字节和 PNG metadata stripping 断言通过。 |
| A5 canonical upload/replay/crash | 通过；single claim、同 key 同 body replay、异 body 409、response loss、claim/pipeline version 和 callback 计数 fixture 通过。 |
| A6 object/DB/ledger failure | 通过；双 rendition fault cleanup、ledger failure rollback、48h orphan inventory/reconciliation 通过。 |
| A7–A9 binding/lease/detach/GC | 通过；plan/Shot ownership/lifecycle/revision、duplicate binding、lease/pin liveness、fixed clock GC claim/delete/finalize 与 physical delete retry fixture 通过。 |
| A10 display content | 通过；bound/staged anchor、stale/released/corrupt/cross-account、OpenDisplay stream close、headers 与 object key non-exposure 通过。 |
| A11 gallery/Run Mode projection | 通过；shot-id pushdown、最多 100 bindings/最多 30 shots、首次 stored snapshot、replay media query=0、不可用素材不阻断执行。 |
| A12 restore/avatar | 通过；empty-target module inventory/manifest restore 全成全败、missing/orphan/corrupt 分类、avatar key/bytes/manifest characterization 零漂移。 |
| A13 工作台响应式/状态 | 通过；frontend contract、1600/1280/375 CSS 结构、empty/loading/error/staged/active/corrupt/gc_pending 状态、build/lint 与 planning-media tests 通过。QA 未新增真实浏览器截图，作为非阻塞 residual 记录，不伪称为本轮浏览器证据。 |
| A14 Run Mode 参考 sheet | 通过；execution-only DOM、display fetch 失败提示、capture/skip 不阻断、coarse-pointer/touch-size CSS 与 Run Mode contract 通过；真实 375/200%/强光浏览器采样保留为后续 residual。 |
| A15 scope negative | 通过；OpenAPI/routes/dependencies/DOM 与 diff 扫描未发现匿名 media route、AI/provider SDK、public/static 直出、object key 或 avatar lifecycle 复用。 |
| A16 ingestion prepared seam | 通过；shared transaction probe、core 新 revision HolderProof、new/existing Shot binding、outer rollback 与 replay callback=0 的 fixture 已落盘。 |

**Review / QA focus 复核**：REV-010–REV-016 已闭环；REV-017、HTTP multipart negative route、projection 边界、真实浏览器极端尺寸和 provider signals 均被 QA 明确列为非阻塞 residual。第 3 轮已达到 review cap=3，REV-016 的 post-review 窄修复由独立 QA 验证，没有启动第 4 轮 review。

**证据来源**：`planning-reference-assets-qa.md` status=`passed`；`planning-reference-assets-evidence-pack.md`、`*-dod-results.json`、`*-gate-results.json`、`*-dod-contract-results.json` 已复核；QA 命令结果和核心 PostgreSQL/object/runtime 路径可重现。

## 4. 术语一致性

- [x] `PlanAsset`、`PlanAssetGeneration`、`PlanAssetRendition`、`PlanAssetRightsDeclaration`、`HolderProof`、`AssetLease`、`AssetReadPin`、`AssetAccessRef`、`UploadSpool`、`gc_pending`、`RunInputSnapshot` 在设计、代码、OpenAPI/generated schema、前端和测试中的含义一致。
- [x] `original` 只表示受约束的不可变原始合规字节，`display` 才是浏览器/Run Mode 读取版本；`binding`、`lease`、`read pin` 不互相冒充授权。
- [x] 禁止词/边界扫描无命中：匿名媒体访问、公开静态对象、客户端 permitted uses 放权、通用 BlobStore、AI/provider、knowledge base 和价格/客户协作能力均未进入本 feature。

## 5. 领域影响盘点

- [x] 新术语候选：`PlanAsset`、rights matrix、HolderProof、AssetLease、AssetReadPin、opaque AssetAccessRef、planning-media inventory/restore。当前 accept 不直接改 `requirements/CONTEXT.md`；建议退出后走 `cs-domain` 统一补入 planning 领域术语和 ownership。
- [x] 结构性决策候选：original/display 双 rendition、immutablefs 中性 seam、单 claim + object/DB/ledger 补偿、asset-lock GC/read-pin 线性化、Run Mode stored projection。这些满足“难回退且有权衡”，建议退出后由 `cs-domain` 评估是否新增 ADR；本报告不代写 ADR。
- [x] 流程约束候选：rights/purpose fail-closed、object key 不出 DTO、corrupt→503、staged/bound read pin、GC partial delete retry。先作为 design/review/QA 的可执行约束保留，后续若跨 feature 复用再沉淀 ADR/compound。

## 6. requirement delta / clarification 回写

- [x] 本 feature 实现的是已批准 `creative-shoot-planning` 首版 Epic 的 planning reference assets slice；没有改变长期 requirement 的用户故事、边界或 roadmap pitch，因此无 req delta，不将 requirement draft 自由改为 current。
- [x] AI/provider、AI 脚本/分镜、生图与知识库维护仍属于后置 `creative-shoot-intelligence` Epic；本 feature 只留下 `generation_reference` 的 rights/lease/permit fail-closed 地基，不声称已有 AI 能力。

## 7. roadmap 回写

- [x] `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml` 中唯一 `slug: planning-reference-assets` 条目已在本次 acceptance 回写为 `status: done`，保留 feature 指针与依赖 `shoot-plan-core=done`。
- [x] `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md` 对应路线条目标记为已完成，并记录 implementation、3轮 review、QA、acceptance 与 scoped commit 路径。
- [x] `.codestable/roadmap/creative-shoot-planning/goal-features/planning-reference-assets.md` frontmatter 与状态回写为 `accepted`；Goal state 的该 feature 状态回写为 `accepted`，`current_feature_index` 前进到 2。
- [x] 外部 checkpoints 不提前消费：`stage-1-evidence-go`、`stage-2-evidence-go`、`production-shaped-rehearsal` 继续 `pending`。

## 8. attention.md 候选盘点

- [x] Docker Desktop Testcontainers mapped-port 瞬态已由既有 `.codestable/attention.md` 覆盖，本 feature 不重复追加规则。
- [x] 可复用候选：planning-media/PG fixture 需串行运行、`make generate-check` 必须先于 generated diff 审计、frontend planning domain directory convention、Vite chunk-size warning。当前仅记录为候选，不在 acceptance 里直接改 attention.md；可在退出后分别走 `cs-keep`/`cs-note`。
- [x] provider unavailable（archguard/meta-cc）、真实浏览器极端尺寸证据和 HTTP multipart negative fixture 是本 feature residual，不升级为每次会话必读规则。

## 9. 遗留与风险

- RR-001（非阻塞）：缺完整真实 HTTP multipart negative route fixture；后续补 required/enum/minimum/unknown/trailing JSON、rights strict decoder、MIME/413/415 的 route-level evidence。
- RR-002（非阻塞）：projection 100/101 binding、30-shot 上界、跨 plan/account 的真实 PG boundary fixture 仍建议补齐。
- RR-003（非阻塞）：`PlanningMediaPanel` 极端慢 list/upload 交错可能遗留 loading 或 stale notice（REV-017）；后续改为 generation-aware state machine 时另开窄 feature/issue，并重新 review+QA。
- RR-004（非阻塞）：本轮 QA 未采集真实浏览器 375/coarse pointer/键盘/200% zoom/强光对比截图；当前由 UI contract、CSS、DOM tests、build/lint 与 prior prototype 结构证据覆盖，真实体验测试留给后续验收/实测。
- RR-005（非阻塞）：archguard/meta-cc signals skipped，Vite chunk >500 kB warning；不影响本 feature correctness。
- RR-006（流程约束）：review round cap=3 已达到；任何新的 blocking 发现必须回 owner checkpoint，不自动启动第 4 轮 review。
- 外部 gate：stage-1、stage-2、production-shaped rehearsal 仍 pending，不属于当前 feature 可消费的证据。

## 10. 最终审计

- **状态**：通过；acceptance authorization=`approval-report.md#goal-acceptance` 已由 Goal state 机械核验为 approved，commit authorization 同一 approval group 也为 approved。
- **产物**：design、design-review、checklist、implementation、review、QA、evidence pack、四份 gate/DoD JSON 和本 acceptance 均存在；canonical feature/roadmap identity 一致。
- **实现/测试**：`make generate-check`、planningmedia/immutablefs/avatar/shootplanning/httpapi/idempotency targeted Go tests、PostgreSQL/object fault fixtures、`npm run test:planning-media`、frontend build/lint、`make check`、`git diff --check` 均有通过证据；known Testcontainers mapped-port 瞬态按 attention 规则串行复跑闭合。
- **覆盖率诚实标记**：A1–A16 均有运行或静态/契约替代证据；`re-verified` 以 QA closure 的 REV-016、核心 lifecycle、projection、scope 与 generated gates 为主，A13/A14 的真实浏览器极端尺寸仅标记 trust-prior/contract evidence，不冒充本轮真实截图。残余风险不承载核心验收缺口。
- **工作区**：当前独立 worktree `feat/creative-shoot-planning`；实现代码、生成物、测试、迁移和 CodeStable 产物均在批准 allowlist。未授权的 push、PR、merge、release、deploy、promotion、production cutover、AI/provider 调用均未执行。
- **结论**：planning-reference-assets 已完成本 feature 验收，允许进入受权 scoped commit；Goal 下一 feature 为 `plan-ingestion-capture`，但本轮不自动实现它。
