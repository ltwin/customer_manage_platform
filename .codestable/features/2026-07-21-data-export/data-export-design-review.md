---
doc_type: feature-design-review
feature: 2026-07-21-data-export
status: passed
reviewed: 2026-07-21
round: 3
---

# data-export feature design 审查报告

## 1. Scope And Inputs

- Design: .codestable/features/2026-07-21-data-export/data-export-design.md
- Checklist: .codestable/features/2026-07-21-data-export/data-export-checklist.yaml
- Intent / brainstorm: none
- Requirement: none；能力直接承接已批准 roadmap §4.6 与既有 OpenAPI `/export`
- Roadmap: .codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md 与 photographer-private-crm-items.yaml
- Related docs: ADR-001/002/003/004；AccountScope fail-loud、OpenAPI tag slicing、cross-domain read model、avatar immutable generation compounds
- Code facts checked: AccountScope / TxAccountScope transaction 入口、OpenAPI `/export` 匿名 response、oapi-codegen tag、router、Settings 有效默认、前端 API client / SettingsPage、Makefile

### Independent Review

- Status: completed
- Detection: native-agent
- Provider / agent: 宿主原生 Task agent `/root/data_export_design_review_retry`
- Retry / provider history: Paseo subagent 工具在本宿主不可用，按协议使用同类原生 Task agent；首次 `/root/data_export_design_review_r1` 因 provider 长时间无结果被中断，未作为 verdict 证据；retry agent 完成有效 round 1、round 2 与本次冻结输入的 round 3 只读复审
- Raw output: round 3 逐项确认 FDR-011～FDR-016 resolved，重新检查全部核心 invariants；blocking=none、important=none、nit=1，建议 verdict=`passed`
- Merge policy: 主 agent 已把独立结果逐条对照当前 design/checklist、roadmap §4.6/§5/§7、items.yaml、OpenAPI 与 AccountScope 代码事实核验；未把未经仓库证据支持的结论升级为 finding
- Gate effect: independent review gate 已满足；design 仍为 `draft`，只允许进入 owner 整体确认 checkpoint，不得自动批准或进入实现

## 2. Design Summary

- Goal: 当前认证账号以一个 reference-only JSON 附件导出七类业务实体与有效 Settings；Customer 只保留公开头像引用，不含头像二进制、内部 object_id、物理 key、manifest/GC/reconciliation，也不承诺跨部署恢复头像
- Key contracts: 账号级 `REPEATABLE READ, READ ONLY` 一致快照、窄 `ReadTxAccountScope`、公开 OpenAPI allowlist、len-derived counts、稳定数组顺序、完整预序列化后再发成功 headers、设置页独立一键下载与 PII 警示
- Steps: 7；按“事务代码纯移动 → 命名 OpenAPI 契约与未挂 route 骨架 → 真实 repository/route → 安全 hardening → client → UI → 全链路证据”推进
- Checks: 24；全部为 `pending`，D/A/MOUNT trace 均能回到 design
- Baseline / validation: checklist 与 roadmap items YAML 可解析；DoD Contract gate passed；tracked 与候选新文件无 whitespace error；placeholder/禁用术语扫描为空；design frontmatter 仍为 `draft`

## 3. Findings

### Resolution Register

| ID | Status | Resolution evidence |
|---|---|---|
| FDR-001 | resolved | D7/A9/CHK-007 固定“Build → OpenAPI 映射 → 完整 JSON bytes → 发送前 context → headers/Content-Length → write”，区分发送前失败与 headers 后不可逆 transport failure |
| FDR-002 | resolved | 新增只暴露读能力的 `ReadTxAccountScope` 契约；生产 callback 不获得写 delegate 或原始 `pgx.Tx`，PostgreSQL READ ONLY 是第二层守护 |
| FDR-003 | resolved | 稳定排序唯一归属 PostgresRepository 并由真实 PostgreSQL fixture 证明；Service fake 只测时钟、schema version、counts 与 Document 构造 |
| FDR-004 | resolved | STEP-002 只做未挂生产 route 的 handler 骨架；STEP-003 完成真实 repository 后才挂 route/composition root；STEP-004 独立负责安全与失败 hardening |
| FDR-005 | resolved | STEP-005/CHK-024 要求新增 `npm run test:data-export` 并接入 Makefile `test`，使 `make check` 持续执行；CMD-004 保留定向证据 |
| FDR-006 | resolved | A9 覆盖空 scope/缺依赖与全部发送前失败；A17 证明七表/settings 各至多一次批量查询、无 N+1、counts 不另查 `count(*)` |
| FDR-007 | resolved | git baseline 区分进入 design 前的无关 `.workflow/`、`install-cpamp.sh` 与本 feature 预期 design/roadmap diff |
| FDR-008 | resolved | design §2.3 明确定义 MOUNT-1/2/3，与 checklist trace 对齐 |
| FDR-009 | resolved | roadmap §7 写入 2026-07-21 owner reference-only resolved 注记，§4.6 JSON shape 保持不变 |
| FDR-010 | resolved | `Clock.Now() time.Time` 与 nil Repository/Clock/ScopeFactory 的显式内部错误语义已进入接口、A9 与 checklist |
| FDR-011 | resolved | roadmap §5 data-export 已同步 `in-progress`、feature 路径与 reference-only 完成信号；manifest 明确为媒体包分支专属、本 feature N/A |
| FDR-012 | resolved | D6/STEP-002/名词层/A2/CHK-001 固定 `components.schemas.ExportDocument`、`ExportCounts` 与 `/export` `$ref`；required 保持、`additionalProperties: false`、counts `minimum: 0`，并要求生成顶层 Go 类型 |
| FDR-013 | resolved | A18 在同一 transaction session 独立读取 `transaction_isolation=repeatable read`、`transaction_read_only=on`，并断言 production `ReadTxAccountScope` 无写面；STEP-003/CHK-005/Coverage/DoD 均已回链 |
| FDR-014 | resolved | A9/CMD-003 证明后端 writer 边界；A12/A13/CMD-004 证明 body/blob reject、client 无结果、UI 可重试且无 object URL/download |
| FDR-015 | resolved | D10/A12/CHK-010 使用 whole-string regex `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$` |
| FDR-016 | resolved | D10/A12/CHK-010 要求 MIME media-type parsing：base type 仅 `application/json`，合法参数允许忽略，禁止 substring，拒绝任意 `+json` |

### blocking

- none。

### important

- none。

### nit

- R3-NIT-001：STEP-005 的退出信号同时写了 client/download 协作与“呈现可重试错误”，而 STEP-006 才负责 DataExportCard 的 loading/error/retry 与页面挂载。
  - Evidence: design/checklist STEP-005 与 STEP-006；Coverage Matrix 已把 A13 正确分配到 STEP-005/006。
  - Impact: 严格逐步执行时会有轻微责任重叠，但最终验收、安全边界和证据没有缺口，不阻塞 passed。
  - Implementation note: STEP-005 可先以未挂载 controller/协作测试证明 client reject 且不创建 URL；卡片可重试呈现与 SettingsPage 挂载在 STEP-006 完成。该执行解释不改变冻结 design 契约。

### suggestion

- 实现 A12 时显式增加 Content-Type 缺失与 media type 语法错误两个 reject case，避免只覆盖合法参数和明确错误 base type。
- STEP-002 code review 同时确认 Go 顶层类型确由 codegen 生成、HTTP 无重复手写顶层 DTO、TS 继续引用生成 paths/components，且 `additionalProperties: false` / `minimum: 0` YAML 层级正确。
- transport fault 保持两个窄测试面：后端 short/error writer 与前端 `response.blob()` reject；不为此引入通用生产 transport abstraction。

### learning

- 匿名 OpenAPI response 与命名 component 可以保持完全相同的 JSON shape，但 Go codegen 的可命名类型结果不同；提升到 components 不需要改写 roadmap §4.6。
- snapshot consistency、database READ ONLY 与窄 Go interface 是三个互补性质：A8、A18 与类型能力断言分别证明，不能互相替代。
- headers 后 transport 状态不可回滚；正确契约是服务端不写第二封套、浏览器 body/blob 失败、UI 不保存坏文件并允许重试。

### praise

- round 1/2 的 FDR-001～FDR-016 全部回写到 D 决策、现状→变化、interface、steps、acceptance、coverage、checks 与 DoD，没有 design/checklist 漂移。
- roadmap §4.6、§5、§7 与 items.yaml 已统一到 owner 的 reference-only 选择，manifest 明确 N/A。
- dataexport read model、窄 read snapshot seam、Repository 排序/counts 责任、薄 HTTP adapter 与现有 ADR/compound 一致。
- Acceptance Coverage Matrix 覆盖 A1～A18，包括 codegen、全状态、空账号/settings、隔离、PII 排除、snapshot/READ ONLY/N+1、transport、MIME/filename、UI 与范围守护。

## 4. User Review Focus

- 已拍板且无需重问：reference-only JSON；Customer 可含公开 `avatar_revision`、`avatar_version`、`avatar_url`；严格排除头像二进制、内部 `avatar_object_id`、物理 key、inventory/GC/reconciliation、exact-generation manifest；不承诺跨部署头像恢复。
- owner 需确认同步内存模型：首版在一次请求中同时物化领域记录、公开 projection 与最终 JSON bytes，没有文件大小、内存、耗时或并发导出硬上限；真实规模不合适时另起 streaming/async feature。
- owner 需确认“全量”边界：包含数据库当前仍存在的 active/archived/merged/cancelled/done/dismissed 等历史与终态记录；不恢复物理删除行，不补造审计历史。
- owner 需确认 PII 告知边界：本 feature 以设置页常驻说明为阻塞性交付；完整摄影师使用指南/API 参考允许 acceptance 后由 `cs-docs` 补齐。
- owner 需确认 `telegram_chat_id` 按现有 Settings shape 导出；bot token、bind token、delivery/update 状态继续严格排除。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1～A18 全部有 step、evidence、command/action 与 core 标记 | implementation/QA 执行 |
| DoD Contract | pass | E | Design/Implementation/Review/QA/Acceptance DoD、CMD-001～004 与 Required Artifacts 完整；机器 gate passed | 下游保存命令证据 |
| Steps and checks traceability | pass | E | 7 steps、24 checks；D/A/MOUNT trace 均存在 | STEP-005/006 按 nit 的顺序解释执行 |
| Roadmap contract compliance | pass | E+C | §4.6、§5、§7、items 与 owner reference-only 选择一致 | acceptance 回写 done |
| Module interface design | pass | E+C | dataexport deep module、窄 ReadTx scope、真实 Repository seam、薄 HTTP adapter | code review 核验依赖方向 |
| Read snapshot invariants | pass | E+C | A8 snapshot + A18 same-session isolation/read-only + type assertion | CMD-003 实跑 |
| Error / transport boundary | pass | E | D7、A9/A12/A13、CHK-007/010/011 与后端/前端双侧 coverage 闭合 | fault tests 实跑 |
| Validation and artifacts | warn | E+C | 设计期 YAML/DoD/diff/placeholder gate 已过；实现命令与生成物尚未运行 | implementation/QA 运行 CMD-001～004 |

Summary: E=4，C=4，H=0；H-only core checks=none。

## 6. Residual Risk

- 同步全量预物化会同时持有领域记录、公开 projection 与最终 JSON bytes；首版没有数据量、文件大小、内存、延迟或并发导出硬上限，需 owner 在整体 review 明确接受，并由运行元数据判断是否另起 streaming/async 设计。
- reference-only `avatar_url` 依赖当前部署、当前鉴权和当前 Customer pointer；长期或跨环境可用性不在本 feature 保证内。
- headers 后网络/transport 中断天然不可逆，不能把已发送的 200 改写为 500；Content-Length、浏览器读取失败、UI 不下载与可重试只能正确处理，不能消除网络不可靠性。
- 本轮是设计审查，未运行 implementation build/test/codegen；当前 OpenAPI、route、ReadTxAccountScope、client/UI 与 Makefile 仍是实现前状态，实际可编译性和命令全绿必须由后续证据证明。
- 七类实体 schema、scanner 与 mapper 的逐字段实现仍需 code review 对 OpenAPI 生成类型核验，尤其是 nullable/omitted、历史状态与 Customer 头像公开 projection。
- 独立审查由同宿主原生 Task agent 完成，而非异构 Paseo provider；通过冻结输入与独立上下文降低偏差，但仍保留同类模型审查的相关性风险。

## 7. Verdict

- Status: passed
- Next: design 保持 `draft`，停在 owner 整体 review 的 `HumanCheckpoint ConfirmDesign`；只有 owner 明确批准完整设计后，才可标记 approved 并进入 goal package。当前不得实现、不得自动批准、不得询问 branch/worktree。
