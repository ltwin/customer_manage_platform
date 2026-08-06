---
doc_type: feature-acceptance
feature: 2026-08-05-shoot-plan-core
status: passed
audit_state: completed
audit_reason: ""
auditor_id: ""
acceptance_authorization_ref: "approval-report.md#goal-acceptance"
accepted: 2026-08-06
round: 1
---

# shoot-plan-core 验收报告

> 阶段：阶段 3（Goal acceptance 闭环）
> 关联方案：`.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md`
> 授权：`approval-report.md#goal-acceptance`，与 Goal state 的授权 ref 精确匹配。

## 1. 接口契约核对

- [x] Create 示例：`POST /api/v1/shoot-plans` 的 `CreatePlan` 由 AccountScope + typed idempotency callback 创建、读取 detail 并把公开 201 body 存 ledger；HTTP 首次/原 key replay 均返回首次快照。
- [x] List/detail 示例：`ListPlans` 使用稳定分页，`GetPlan` 返回 current projection、可选 execution history 和服务端 archive acknowledgement；跨账号统一 404。
- [x] 聚合 command 示例：`ApplyPlanCommand` 只接受一个 generated discriminator，锁 plan、校验 revision/state、原子写结构并保存成功响应；同 key 异 body/resource 409。
- [x] Transition/Run 示例：`TransitionPlan` 覆盖 ready/start/complete/reopen/archive；`OpenRunSession` 由服务端判定 capture mode；`AppendShotResult`/`VoidExecutionEvent` 通过 execution revision 与 server seq 追加事实。
- [x] 名词层“现状→变化”：`ShootPlan`、`CreativeBrief`、`Shot`、`ReadinessItem`、`RunModeSession`、`ShotExecutionEvent`、`ShotExecutionEventVoid`、`PlanFinalizationSnapshot` 均在 domain/model、repository、OpenAPI、前端或证据中有实际落点；客户端不传 account_id/capture_mode。
- [x] 流程图落点：结构 command→ledger→application→repository、Run Mode→session→result/void→projection、completion→snapshot 的每个节点均由 `application.go`、`command_engine.go`、`execution.go`、HTTP adapter、PG integration tests 覆盖。

## 2. 行为与决策核对

- [x] 需求摘要：无客户/订单/档期也可建策划；可维护 brief、Shot、Readiness、规模、时间窗；现场可逐镜执行、纠错和完成；既有 CRM 无“缺策划”分支。
- [x] 明确不做：未引入 AI/provider/知识库、媒体上传/摄取、CRM 关联、匿名分享、客户反馈/认领、提醒投递、经营价格/成本、离线写入或实时多人编辑。
- [x] D1/D2：ShootPlan 是独立聚合；plan revision 与 per-shot/plan execution fact revision 分离，completion 使用双 revision CAS。
- [x] D3/D4/D7：结果/void append-only；当前 projection 可 replay；Shot 软移出但保留历史；空 Shot/未完成 Shot 不能 complete。
- [x] D5/D6/D8/D9：手动执行 window 和服务端 live/backfill/unknown；Run Mode 只允许 ready/in_progress；ready invariant 破坏回 draft；列表稳定分页且不读后置域。
- [x] D10/D11：列表 GET 与通用 Execute ledger 是已批准 seam；旧 order/schedule wrapper characterization 通过，未创建第二套 core ledger。
- [x] 反向核对：`rg` 扫描 core backend/frontend 未发现匿名 planning route、provider/AI/media/business surface、debug output、placeholder handler、TODO/FIXME/XXX 或 client account scope。
- [x] 挂载点：migration、OpenAPI/生成物、planning application/repository、HTTP/router/composition、planningctl、AppShell/workspace/Run Mode、tests/evidence 均存在；删除清单中的 route/wiring/目录后不会保留可达 core surface。共享 platform 文件的改动均由 approved design 挂载点归因。

## 3. 验收场景核对

- [x] A1–A2：真实 PG/API 建案、分页、隔离、resource-aware idempotency、CAS、legacy wrapper 和 capability transaction probe 通过。
- [x] A3–A5：required readiness gate、ready→draft、in-progress 新 Shot、状态 admission、session close 和 complete/archive 通过。
- [x] A6–A8：fixed-clock capture mode、captured/skipped/cleared、void 真值表、server seq 和并发 execution CAS 通过。
- [x] A9–A11：带执行历史 Shot 软移出、finalization snapshot、reopen/re-complete、旧 snapshot 保留通过；前端按 retained history 判断 acknowledgement。
- [x] A12：strict fields、generated closed unions、archive registry、typed stale 409、capability marker/bootstrap/promotion/TTL、planningctl、migration/fence/rollback 通过。
- [x] A13–A14：1600/1280/375 工作台与 375 Run Mode 浏览器证据目检通过；coarse pointer、focus、high contrast、offline/success-only、execution-only DOM contract 通过。
- [x] A15–A18：List 固定查询上界、schema/route/dependency negative、readiness guard、neutral fence、reciprocal FK、generation watermark 和双连接 rollback 通过。
- [x] review QA focus 已逐条消费：非末尾尾插、duplicate reorder、create exact replay、typed archive 409、fingerprint、generate failure、OpenAPI variants、multi-Shot 组合证据均通过；missing/extra/cross-plan case 强度与 READ COMMITTED/read-down 风险已登记为非核心 residual。
- [x] QA 报告：`.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-qa.md` status=passed，A1–A18 为 18 pass / 0 fail / 0 blocked，CMD-001～005 最终全绿，cleanliness passed。
- [x] Evidence pack、scope/DoD/evidence gate results 已复核；archguard/meta-cc unavailable 与 Vite chunk warning 按 provider/非阻塞政策解释。

## 4. 术语一致性

- [x] `CONTEXT.md` 已通过 `cs-domain` 补入：拍摄策划、创作 brief、镜头、准备项、执行会话、执行结果事件、误记作废、完成快照；代码与 OpenAPI 使用同一中英文名。
- [x] 防冲突：`用户` 未作为业务对象进入本 feature；账号=摄影师数据归属单位，客户=既有 CRM 对象；Shot 不写成照片，Readiness 不写成 Reminder/Assignment。
- [x] generated DTO 仅来自 `frontend/src/api/schema.d.ts`；OpenAPI discriminator variants 不再形成不可构造 intersection。

## 5. 领域影响盘点

- [x] 新术语已由 `cs-domain` 写入 `.codestable/requirements/CONTEXT.md`，保持单 context 拓扑；未创建 CONTEXT-MAP 或拆分子 context。
- [x] 结构性决策满足守门 3 判据：双 revision + append-only execution facts 会影响后续七条 child、难回退且存在明确 alternatives；已由 `cs-domain` 写入并接受 ADR-005 `005-separate-plan-revision-from-append-only-execution-facts.md`。
- [x] 账号隔离、PostgreSQL 主存、薄 handler 沿用 ADR-001/002/003，不修改既有 ADR；媒体二进制边界沿用 ADR-004。
- [x] 领域边界保持：core 只提供后续模块所需 typed query/command/participant/fence seam，不把 AI、知识库、share/reminder/CRM 实现偷渡进来。

## 6. requirement delta / clarification 回写

- [x] `creative-shoot-planning` requirement 已有 owner-approved draft，但本 feature 只实现 Epic 的 core slice；摄取、参考素材、CRM、分享、提醒、经营反馈和 hardening 尚未完成，不能把整份能力愿景标为 current。
- [x] 本轮无 capability-boundary delta、无用户故事扩写、无 `*-req-delta.md`；按 Global Route Governance 保持 `.codestable/requirements/creative-shoot-planning.md` `status: draft`、`implemented_by: []`，不自由改写长期 requirement。
- [x] VISION 的 draft 索引与后置 `creative-shoot-intelligence` 边界不变；AI/知识支持继续留在独立后置 roadmap。

## 7. roadmap 回写

- [x] `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-items.yaml` 中唯一匹配 `slug: shoot-plan-core` 的 item 已从 `in-progress` 改为 `done`，`feature` 指针保持 `2026-08-05-shoot-plan-core`。
- [x] 主 roadmap 第 6 节对应条目已同步标注 `shoot-plan-core（done）`，并记录 implementation、独立 review、QA、acceptance 已完成；`last_updated` 同步为 2026-08-06。
- [x] YAML 校验、feature workflow 恢复与 scope attribution 通过；`acceptance-dod-gate` 已机械核验为 passed，Goal state 已同步 `shoot-plan-core=accepted` 与 `current_feature_index=1`。

## 8. attention.md 候选盘点

- [x] Testcontainers `port "5432/tcp" not found` 是 attention.md 已有 Docker Desktop 瞬态说明，本 feature 未新增重复规则；失败包串行复跑与完整 make check 重跑均通过。
- [x] `make generate-check` 的 fail-fast 修复与前端 domain-directory convention 是可复用经验候选，已登记在 implementation/review residual，退出后可分别走 `cs-keep`；本验收不直接改 attention.md。
- [x] provider unavailable、Vite chunk warning、reorder case coverage 与 populated-down policy 均为本 feature residual，不升级为每次必读 attention 规则。

## 9. 遗留

- RR-001：capture/void ledger 内部 response_status=200 与公开 201 不一致；当前用户路径无差异，后续可走窄 hygiene 修复，若修生产代码需重新 review+QA。
- RR-002：reorder missing/extra/cross-plan 缺各自独立 PG 回归；共同 validator 已在 position 写入前 fail closed。
- RR-003：READ COMMITTED 多语句 detail 与高频执行写交错没有专门 stress fixture，可能有短暂展示混合，不形成数据损坏。
- RR-004：core populated planning-ledger down 的正式 rollback runbook 尚未固定；当前 SQL fail closed，不静默删除 ledger。
- RR-005：multi-Shot Run Mode 有组合 API/DOM/状态证据，但没有单独“两 Shot 导航+各自 retry”的浏览器录屏。
- RR-006：三处非冲突 generated union `as` 断言属于长期类型卫生项；不影响 REV-004 closure。
- provider/bundle：archguard、meta-cc unavailable；Vite chunk >500 kB；scope warning 来自后续 checklist 验收文本。
- 上述遗留均不承载 core 功能缺口；后置 feature、阶段 1/2 evidence gate、production-shaped rehearsal、AI/provider/知识库仍按 Goal protocol 保持未启动。

## 10. 最终审计

- 验证证据来源：`shoot-plan-core-qa.md` passed；Round 2 review passed；implementation evidence、evidence pack、DoD/gate JSON 均复核。
- 聚合命令：`make generate-check`、目标 Go/PG/API/planningctl suite、frontend 11/11、frontend build/lint、`make check`；最终 canonical 退出码均为 0。`make check` 首次 Testcontainers 端口映射瞬态已按既有规则受控复跑闭合。
- 场景复核：A1–A18 re-verified 18；浏览器截图 4 项 trust-prior-verify（本轮已逐图目检）；无核心路径仅靠 residual 放行。
- 交付物复核：migration、OpenAPI/Go/TS schema、domain/application/repository、HTTP/router/composition、planningctl、前端 routes/workspace/Run Mode、tests、screenshots、review、QA、requirements CONTEXT、ADR-005、roadmap/items 均真实存在。
- 完整工作区复核：当前为 `feat/creative-shoot-planning` 独立 worktree；Epic baseline 的 dirty/untracked 文件均在批准 allowlist，staged 为空；无任务外文件。
- diff 清洁度：`git diff --check` / `git diff --cached --check` 通过；core 源码无 debug/TODO/FIXME/XXX/placeholder/注释旧代码/无用 import；生成物零漂移。
- 知识沉淀出口：`make generate-check` fail-fast 与 planning domain directory 进入 `cs-keep` 候选；双 revision/append-only 已进入 ADR-005；Docker 瞬态已由既有 attention 覆盖；没有新的 attention 强规则。
- 覆盖率诚实标记：`re-verified=18`，`trust-prior-verify=4`；trust-prior 比例不高于 30%，且四张截图均已目检，未替代功能性运行证据。
- Acceptance DoD gate：协议 gate 已执行；14/14 checklist checks、review、QA、acceptance 与 scope/DoD/evidence/DoD-contract 机器结果均为 passed，roadmap item 已回写 done，residual risk 不包含核心验收缺口。
- 结论：通过；无未处理核心验收缺口，允许按 Goal protocol 进入受权提交。
