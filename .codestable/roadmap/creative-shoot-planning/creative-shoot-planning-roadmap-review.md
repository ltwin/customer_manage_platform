---
doc_type: roadmap-review
roadmap: creative-shoot-planning
status: passed
review_state: passed
review_reason: round-2-split-v1-no-blocking
reviewer_id: /root/creative_roadmap_rereview
reviewed: 2026-08-04
round: 2
supersedes_round: 1
---

# creative-shoot-planning 独立规划审查报告

## 1. Scope And Inputs

- Roadmap：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`；
- Items：`.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml`；
- Approval：`.codestable/roadmap/creative-shoot-planning/approval-report.md`；
- Brainstorm：`.codestable/brainstorms/creative-shoot-planning/brainstorm.md`；
- Related requirements：customer-profile、order-tracking、schedule-calendar、package-catalog、reminder-engine、telegram-digest；
- Architecture：ADR-001..004；
- 代码事实：Order/OrderStatus、ScheduleSlot、reminder/digest recipient、Telegram account binding、现有 backup/export seam；
- 工作流事实：`cs-epic` child design batch、goal package、feature loop、handoff/needs-human 与 approval conventions。

本报告只审查拆分后的 8-item 首版。原 16-item 混合路线和旧 11-item approval 已被结构性替代，不作为 passed 证据。

## 2. Independent Review Execution

| Pass | Verdict | 结果 |
|---|---|---|
| 初始独立复审 | changes-requested | 找到 child-design evidence barrier 死锁、经营草稿假设不存在的 revision/CAS、assignment 撤销 seam 缺口，并提示 G1/G3 cohort 不可作因果解释 |
| Focused closure | passed | 所有 finding 已关闭；重新核验当前磁盘内容、DAG、minimal loop、goal handoff 恢复、事务 fingerprint、API seam 和 evidence binding，无剩余 blocking/important |

- Detection：independent-agent；
- Reviewer：`/root/creative_roadmap_rereview`；
- Independence：两次均只读，未修改 roadmap、items、approval、review 或业务代码；
- Merge policy：主 agent 逐条用 CodeStable 协议与当前代码事实核验，完成修订后由同一独立 reviewer 做 focused closure；reviewer 不替 owner 批准路线。

## 3. Findings And Disposition

| Finding | Severity | Final disposition |
|---|---|---|
| PLN-RMR-001 G1/G2、G3 在 child design 前形成“先实现、后设计”死锁 | blocking | closed：8 条全部进入标准 child design batch；CRM 依赖 ingestion、business 依赖 reminders 固定 implementation 顺序；evidence gate 移至 goal feature dispatch，定义 gate result、index 不变、handoff、approval-first typed resume 和 owner 单独 deploy 边界 |
| PLN-RMR-002 经营草稿依赖不存在的 Order/Slot revision 与现有 CAS | blocking | closed：删除 revision/CAS 假设；draft 使用 canonical target fingerprint，事务锁行后重算，不匹配 409；匹配后原子写 OrderPriceAdjustment 审计并更新目标；不扩大既有 Order/Schedule 全局并发契约 |
| PLN-RMR-003 assignment/feedback 缺少匿名撤销和摄影师管理 seam | important | closed：补 Bearer feedback/assignment list/disposition/revoke；补匿名 DELETE；full token + receipt、expected revision、Idempotency-Key、404/409 和 reminder outbox 一致性明确 |
| PLN-RMR-004 G3 与 G1 cohort 不同却可能被解释为因果改善 | suggestion | closed：evidence 必须报告原始分子/分母、cohort 差异、可比子集和小样本区间；只作方向性 evidence，不使用因果措辞 |
| 旧 combined roadmap 的 G4 只是 prose、AI 节点会提前 ready | blocking | closed by split：AI/知识节点已移出本 YAML；后置 roadmap 在 G4 前保持 draft/pending |
| 原最小闭环只到手工 core，不含摄取 | blocking | closed：唯一 `minimal_loop=true` 为 `plan-ingestion-capture`，其依赖闭包覆盖 core、media、摄取、shot list 与 run mode |
| proposal/full、匿名媒体、提醒收件人、business facts/style tags、素材用途和 v1 backup 缺口 | blocking | closed：全部成为 §4 契约与明确 owner item；客户提醒收窄为摄影师检查清单；生产 planning-media restore 归 v1-hardening |

## 4. Mechanical Checks

- YAML/frontmatter：pass；
- Items：8，必填字段齐全，slug 唯一；
- DAG：无未知依赖、自依赖或环；
- Topological order：`shoot-plan-core → planning-reference-assets → plan-ingestion-capture → shoot-plan-crm-integration → plan-share-collaboration → plan-assignment-reminders → plan-business-feedback → creative-planning-v1-hardening`；
- Unique minimal loop：pass，仅 `plan-ingestion-capture` 为 true；
- Roadmap §6 与 YAML slug/顺序一致；
- Approval 状态：`pending`；`epic-split=approved`，`roadmap-plan` 与两个 evidence decision 均 pending；
- Evidence binding：stage-1/stage-2 都预留 canonical path/SHA-256/gate version，pending 时为空；
- Whitespace：`git diff --check` pass；
- 本轮只改规划文档，未运行 `make check`，不把文档校验冒充业务代码验证。

## 5. Evidence Confidence Ledger

`E` = roadmap/items/approval/命令直接可见；`C` = requirement/ADR/当前代码事实；`H` = 启发式。

| Check | Verdict | Evidence class | Basis | Follow-up |
|---|---|---:|---|---|
| Granularity Gate | pass | E | 8 个 item 跨 shootplanning、planningmedia、planshare、reminder 与 backup，可独立验收又需 epic 级汇合 | child design 重新做单 item granularity gate |
| Goal Coverage Matrix | pass | E | 每个首版完成信号有唯一 owner 和证据类型 | acceptance 逐项落真实证据 |
| DAG and minimal loop | pass | E/C | YAML 无环；唯一 minimal loop 为 ingestion；goal 顺序支持两个真实 evidence handoff | goal package 固化 dispatch gate |
| Interface contract usability | pass | E/C | run/observation、rights、share、assignment、fingerprint draft 和 backup seam 可直接约束 child design | OpenAPI/design 时补完整 DTO |
| Module interface depth | pass | E/C | 聚合、媒体、匿名 token、reminder、backup 各有 deep owner；business 不读取 share/reminder | design-review 核验 owner 不漂移 |
| Approval recoverability | pass | E/C | named decision + evidence hash 是 durable surface；handoff/index 是可修复 projection | typed resume 必须 approval-first |
| H1/H2/H3/H4/H5 coverage | pass | E | negative tests、disabled AI、recipient 与 source/use 矩阵均进入 completion | v1-hardening 汇总 |

Summary：E=7，C=4，H=0；H-only core checks=`none`。

## 6. Residual Risks And Owner Review Focus

### V1-RR-001 · 分阶段真实 evidence 会拉长 goal 生命周期

前三条和协作/提醒分别 accepted 后，goal 会 handoff，等待 owner 另行授权试点 deploy 和真实样本。推荐保留，因为它让 kill criteria 真实生效；代价是不能把一次 goal 当作不间断流水线。

### V1-RR-002 · 5 个样本只提供方向性证据

G1/G2/G3 都必须同时报告原始样本、排除理由、cohort 差异和区间。owner 不应把 3/5 或 40% 当统计显著性，也不能把 G3 变化写成协作导致的因果结论。

### V1-RR-003 · 首个匿名公网读写面

token URL、trusted proxy/IP 限流、日志脱敏、Referrer/CSP/no-store、receipt、滥用处置和 retention 必须在 feature threat model/QA 实证。token 转发本身无法完全阻止，proposal 白名单是主要降损手段。

### V1-RR-004 · 首版不做离线可写

G1 失败时要区分“现场网络不可用”和“摄影师不愿使用”。若外景/地下场地高频阻塞，必须回 planning update，而不是在 hardening 临时加同步。

### V1-RR-005 · backup-v2 成对发布

应用、writer、新 reader 和 restore 工具/镜像必须成对；旧 reader 必须拒绝 v2。一次真实恢复演练是发布 blocking evidence。

## 7. Verdict

- Status：`passed`；
- Blocking findings：none；
- Important findings：none；
- Roadmap state：继续 `draft`；
- Approval state：`roadmap-plan=pending`，未替 owner 确认；
- Next：owner 可审阅 8 条路线与 residual risks；明确批准后才把 roadmap 设为 active、创建长期 requirement 并进入全部 child design batch。Goal package 必须保留 stage-1/stage-2 dispatch gate，不得把 roadmap/Goal 启动授权解释为 deploy 授权。
