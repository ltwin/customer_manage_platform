---
doc_type: roadmap-review
roadmap: photographer-private-crm
status: passed
reviewed: 2026-07-13
round: 12
---

# photographer-private-crm roadmap 审查报告

## 1. Scope And Inputs

- Roadmap：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`
- Items：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`
- 当前 feature：`.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md` 与 `customer-avatar-checklist.yaml`
- Related docs：customer-profile requirement、CONTEXT、ADR-001/002/003、AccountScope/OpenAPI 相关 compound
- Code facts checked：现有 Customer merge 行锁、CustomerSummary/OpenAPI、server lifecycle、AccountScope、客户选择入口与部署配置

### Independent Review

- Status：completed
- Detection：native-agent
- Provider / agent：宿主原生 Codex subagent `avatar_roadmap_review`（Ptolemy）
- Raw output：最终增量复核 `passed`；`blocking/important/nit = none`；旧 round 11 冻结 verdict 已作废
- Frozen inputs：roadmap `c8e5c82d`；items `8b496c95`；design `991230dd`；checklist `311e5778`
- Merge policy：主 agent 逐条核验 owner 决策、状态矩阵、并发、PII/备份边界、DAG 和机器校验
- Gate effect：none

## 2. Roadmap Summary

- Goal completion signal：安全设置/替换/鉴权读取/移除头像；merged 只允许 cleanup-only DELETE；不可复用 generation、双向 reconciliation、可验证本地卷与 exact-generation 备份恢复。
- Module split：customer 拥有头像生命周期与 `AvatarObjectStore` port；platform 提供 provider adapter、配置和 runner 装配；webapp 用 CustomerAvatar/CustomerPicker 收口展示和选择。
- Interface contracts：`avatar_revision` 负责写 CAS，`avatar_version` 负责内容 URL/ETag，`avatar_object_id` 负责不可复用物理代次；application ReadContent 完整验证后才由薄 handler 返回。
- Owner decisions：merged PUT 恒 409，GET 可读，DELETE 是唯一 object→none 清理例外；GC 默认值为 24h grace、每小时、每账号每 tick 各一页 inventory/audit +100 due。
- Items：12 条，`6 done / 1 in-progress / 5 planned`；customer-avatar 绑定 `2026-07-11-customer-avatar`。
- Dependency shape：DAG 无未知节点、自依赖或环；唯一 minimal loop 为 customer-core。

## 3. Findings

### blocking

none

### important

none

### nit

none

### suggestion

none

### learning

- 将 merged DELETE 收窄为 object→none 的 PII 清理例外，可以复用 revision CAS、pointer 清空和精确代次 GC，而不恢复 merged 的通用编辑能力。
- DDD 保持上层契约稳定，但跨 PostgreSQL 与对象存储的一致性、存量迁移和回滚仍须由 generation、reconciliation 与迁移流程显式处理。

### praise

- Owner 决策已同步到 roadmap、items、design、checklist、A10/A12/A16 和变更日志，而不是只改产品措辞。
- 历史备份边界已明确：在线 cleanup 不等于跨备份彻底擦除。

## 4. User Review Focus

- 已拍板：merged 禁止 PUT，但允许显式 cleanup-only DELETE；GET 仍可读取清理前的既有头像。
- 已拍板：24h grace、每小时 runner、每账号每 tick 各一页 inventory/current-pointer audit +100 due。
- 待整稿批准：是否接受当前 Roadmap/Design/Checklist 作为实现契约。
- 延后 gate：data-export 的 reference-only JSON 或媒体便携包选择，留到 data-export design 启动前。
- implement/QA 重点：真实 DB session loss、迟到 Delete、commit unknown、浏览器 Blob 生命周期、signal shutdown、mount recreate 与 exact-generation restore。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Granularity Gate | pass | E | 跨 customer/platform/webapp，含 API、存储、后台任务、部署和 UI 闭环 | none |
| Goal Coverage Matrix | pass | E | A1-A24 覆盖功能、安全、并发、恢复、UI 和运维 | acceptance 取证 |
| DAG and minimal loop | pass | E | 12 items 图校验通过，customer-core 为唯一 minimal loop | none |
| Interface contract usability | pass | E+C | 字段、错误、状态机、port、GC 与迁移语义可执行并有代码 seam | 实现零漂移 |
| Module interface depth | pass | E+C | customer 隐藏生命周期，platform 隐藏 provider I/O，handler 不直连 store | code review |
| PII/backup boundary | pass | E+C | 在线 cleanup、活动存储、历史备份和恢复边界均显式 | 运维 retention/销毁 |

Summary：E=3，E+C=3，H=0；H-only core checks=none。

## 6. Residual Risk

- Cleanup DELETE 只清在线 pointer，并按 24h+runner 节奏回收活动存储；不追溯擦除历史备份，恢复旧备份可能重新带回 PII。
- Owner 接受的 runner 上限使 GC、orphan 发现和 integrity audit 非即时；积压时延取决于对象量和失败率。
- 鉴权读取在发送 200/304 前最多缓冲 5 MiB，实现期仍须观察并发内存峰值。
- mountpoint attestation 不等于持久性证明，最终依赖 container recreate 和 exact-generation 备份恢复证据。

## 7. Verdict

- Status：passed
- Next：进入 owner 的 feature design 整体 review；design 保持 `draft`，owner 明确批准整稿后才可改为 `approved`
