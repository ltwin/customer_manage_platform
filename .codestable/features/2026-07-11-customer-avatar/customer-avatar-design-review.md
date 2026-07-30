---
doc_type: feature-design-review
feature: 2026-07-11-customer-avatar
status: passed
reviewed: 2026-07-13
round: 2
---

# customer-avatar feature design 审查报告

## 1. Scope And Inputs

- Design：`.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md`
- Checklist：`.codestable/features/2026-07-11-customer-avatar/customer-avatar-checklist.yaml`
- Roadmap：`.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`
- Roadmap review：round 12，`status: passed`
- Related docs：customer-profile requirement、CONTEXT、ADR-001/002/003、AccountScope/OpenAPI/frontend compound
- Code facts checked：Customer merge/行锁、CustomerSummary/OpenAPI、server lifecycle、AccountScope、referral/order/schedule 客户选择入口

### Independent Review

- Status：completed
- Detection：native-agent
- Provider / agent：宿主原生 Codex subagent `avatar_design_review` + 其独立专项 reviewer `merged_cleanup_review`
- Raw output：两者最终均 `passed`，`blocking/important/nit/suggestion = none`
- Frozen inputs：roadmap `c8e5c82d`；items `8b496c95`；design `991230dd`；checklist `311e5778`
- Merge policy：主 agent 已逐条核验状态矩阵、流程图、A10/A12/A16、GC 默认值与历史备份边界
- Gate effect：none

## 2. Design Summary

- Goal：安全设置、替换、鉴权读取与移除客户头像，并让全部客户展示/选择面统一使用真实头像或 fallback。
- State matrix：active/archived 可 PUT/GET/DELETE；merged PUT（含 same-content）恒 409，GET 可读，DELETE 是唯一 cleanup-only 例外。
- Key contracts：`avatar_revision` 负责写 CAS，`avatar_version` 负责内容身份，`avatar_object_id` 负责不可复用物理 generation。
- Orchestration：对象先发布、revision CAS 后切 pointer、旧代次延迟 GC；进入 GC 的 generation 永不再晋升；双向 reconciliation 覆盖 orphan 与 current integrity。
- Layering：application ReadContent 负责 Stat/Open/完整字节验证，HTTP handler 只做协议适配。
- Frontend：CustomerAvatar 管理鉴权 Blob、auth generation、引用计数与 fallback；merged 详情有图时只显示“移除头像”，不显示设置/替换。
- Steps / checks：8 steps、20 checks、6 commands；A1-A24 连续。
- GC defaults：24h grace、ready 后立即一轮、之后每小时 single-flight、每账号每 tick 各一页 inventory/audit +100 due。

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

- cleanup-only DELETE 通过“状态矩阵 + revision”授权：merged 状态阻断 PUT，DELETE 再用 revision 防止清错版本；无需给 merged 恢复通用编辑能力。
- DDD 隔离 provider 替换，但不会自动处理跨持久化结果未知或历史备份销毁。

### praise

- D6、流程图、并发约束、A10/A12 和 A16 从领域、控制流、API 与 UI 四层表达同一非对称状态矩阵。
- 历史备份边界落到 D14、A21 和 checklist，不把在线 cleanup 冒充跨备份擦除。

## 4. User Review Focus

- 已拍板：merged 禁止 PUT，但允许 cleanup-only DELETE；有头像时用当前 revision 清理，无头像重放 200 no-op。
- 已拍板：GC 默认值采用 24h / 每小时 / 每账号各一页 audit +100 due。
- 待整稿批准：当前 draft 的其余接口、存储、一致性、前端、运维与验收契约是否整体放行。
- 延后：data-export 的 reference-only JSON / 媒体包选择。
- 实现重点：真实 DB session loss/迟到 Delete、Blob 引用计数与 logout 清理、signal shutdown、mount recreate、备份恢复和 retention 文档。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1-A24 连续并映射 S1-S8、证据和命令 | QA 取证 |
| DoD Contract | pass | E | 五道 blocking gate、6 commands 与 artifacts 完整 | 逐 gate 执行 |
| Steps and checks traceability | pass | E | 8 steps、20 checks 可回到 D2-D14、范围/挂载点/A1-A24 | none |
| Roadmap contract compliance | pass | E+C | §4.2/4.3/4.3a 与 round 12 review 一致 | OpenAPI 双向核对 |
| Module interface design | pass | E+C | handler→application→store，ownership/close/List/Delete barrier 明确 | code review |
| Owner decision consistency | pass | E | D6、流程图、A10/A12/A16、checklist 与 items 一致 | none |
| PII/backup boundary | pass | E | D14、A21、S8 明确在线清理与历史备份责任分离 | 运维策略 |

Summary：E=5，E+C=2，H=0；H-only core checks=none。

## 6. Residual Risk

- PostgreSQL 与对象存储之间的结果未知、DB session 丢失和迟到 Delete，仍须实现期用真实 session termination/DB restart 与 storage fault injection 证明。
- 前端鉴权 Blob 的 auth-generation、消费者引用计数、单消费者卸载和 logout/401 abort/revoke 是实现复杂点。
- Cleanup 不追溯历史备份；旧备份恢复可能重新引入已在线清理的头像 PII，retention/销毁属于独立运维责任。
- 鉴权读取最多缓冲 5 MiB，runner 吞吐有界，mount attestation 也不单独证明持久性。

## 7. Verdict

- Status：passed
- Next：交给 owner 做整份 feature design 最终 review；design 继续保持 `draft`，明确批准整稿后才改为 `approved`
