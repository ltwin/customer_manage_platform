---
doc_type: feature-design-review
feature: 2026-08-05-planning-reference-assets
status: passed
review_state: passed
review_reason: round-3-read-pin-focused-pass
reviewer_id: /root/creative_roadmap_focus_review
reviewed: 2026-08-05
round: 3
supersedes_round: 2
review_mode: focused-delta
---

# planning-reference-assets feature design 审查报告

## 1. Scope And Inputs

- Design：`.codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-design.md`；
- Checklist：同目录 `planning-reference-assets-checklist.yaml`；
- Requirement / roadmap / items：`creative-shoot-planning` 已批准 requirement 与 active roadmap；
- Upstream：`shoot-plan-core` 已 passed design/review，尤其是 `ExecuteInScope`、RunInputSnapshot stored response 与 ingestion combined commit seam；
- Architecture：ADR-004、AccountScope、现有 avatarmedia typed store/inventory/manifest 与 idempotency 单 request hash 代码事实；
- Prototype：tracked v2 `planning-workspace.html`、`plan-ingestion.html`、`run-mode.html`。

### Independent Review

- Reviewer：`/root/creative_roadmap_focus_review`；
- Independence：全程只读，未修改 design、checklist、roadmap、prototype 或代码；
- Raw result：round 1=`changes-requested`（2 blocking、2 important），round 2=`changes-requested`（1 important），round 3=`pass`；
- Gate effect：所有 blocking/important 关闭后才允许本报告写 `passed`；design 仍保持 `draft`，返回 epic child batch，不进入实现。

### Review History

| Round | Mode | Verdict | 主要结论 |
|---|---|---|---|
| 1 | full independent | changes-requested | upload 双 claim、Run snapshot 缺 tx query、ingestion 新 Shot proof 顺序、permit/GC TOCTOU |
| 2 | full closure | changes-requested | 前三项关闭，read pin 行为关闭；发现 staged 无 binding 无法由非空 `AssetLease.binding_id` 表达 |
| 3 | focused delta | passed | 独立 `AssetReadPin` + binding/upload-context anchor 闭合；无新增 blocking/important/nit |

## 2. Design Summary

- 独立 `planningmedia` deep module 拥有 asset/generation/rendition/rights/binding/lease/read-pin/GC/inventory，不复用 avatar 的领域生命周期。
- 一次上传产生 immutable original/display；Web 与 Run Mode 只读去元数据 display。v1 image limits 与 48h staged/orphan rule 作为待整体确认、已版本化假设。
- source×rights×purpose 由服务端 matrix v1 裁决；official/screenshot/setting book/fan/unknown web/customer supplied 均不能作为 generation reference，只有 owned 或 explicit licensed grant 可用。
- HolderProof 在 caller 的 `TxAccountScope` 中由 core 签发；ingestion 先 core commit，再按新 plan revision 为 existing/new holder batch authorize，然后 media bind。
- upload 只有一份 `UploadCanonicalV1` 和一次通用 Execute claim；display facts 是 pipeline v1 的 stored output，不是第二 request identity。
- RunInputSnapshot 首次在 open-session callback 内用 tx-scoped batch projector 组装并进入 ledger；replay media query 为 0。
- `AssetLease` 表达已绑定业务 liveness；`AssetReadPin` 以 binding 或 upload context 二选一锚定实际读取，permit/detach/GC 使用共同 asset row lock。
- module manifest/empty-target restore fixture 属本 feature；生产 schema-v2 组合备份与 rehearsal 保留给 hardening。

## 3. Findings And Closure

### blocking

none。

### important

none。

### nit

none。

### 已关闭 finding

| ID | 原严重度 | Closure evidence |
|---|---|---|
| R1-B1 | blocking | 移除 preflight/final 双 claim；bounded spool 后只用 original facts+metadata+pipeline/matrix version 构造唯一 canonical identity，callback 才 decode/publish |
| R1-B2 | blocking | 新增 `BatchShotAccessRefsInScope`；首次 refs 进入 open-session stored response，replay callback/query=0 |
| R1-I1 | important | ingestion 顺序固定为 core commit→new revision/resolved targets→同 tx batch HolderProof→media bind |
| R1-I2 | important | permit 建持久 read pin；permit/detach/GC 共同 asset lock，先 permit 则本次读完，先 release/GC 则拒绝 |
| R2-I1 | important | `AssetLease.binding_id` 保持非空；新增独立 `AssetReadPin`，binding/upload-context anchor 互斥，覆盖 staged preview |
| R2-N1 | nit | 删除“事务前算 display checksum”旧表述；display facts 只在唯一 callback 内派生 |

## 4. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Rights matrix | pass | E/C | roadmap matrix 与 design exhaustive table/negative A2-A3 一致 | implementation 跑全组合 fixture |
| Upload idempotency | pass | E/C | 单 canonical/单 claim 与当前 single request_hash ledger 可实现 | fault/concurrent/replay characterization |
| DB/FS boundary | pass | E | 明确非 exactly-once；immutable resume/orphan reconciliation/故障点齐全 | 临时 volume 注入故障 |
| Core/media transaction | pass | E/C | tx-scoped snapshot query、post-core proof 与 outer operation ownership闭合 | shared transaction PG fixture |
| Read/GC linearization | pass | E | AssetReadPin 持久模型、互斥 anchor、共同锁序与双连接场景明确 | staged/bound read vs GC fixture |
| Avatar isolation | pass | C | 只抽中性 immutablefs，avatar key/bytes/manifest characterization 是硬 gate | 实现前后 byte diff |
| Restore boundary | pass | E/C | module fixture 与 production schema-v2 hardening owner 分离 | hardening 再做真实 rehearsal |
| Checklist traceability | pass | E | S1-S8 稳定 ID、12 checks、CMD-001..006 与 A1-A16 对齐 | execution 不得删减证据 |

Summary：E=6，C=5，H=0；H-only core checks=`none`。

## 5. Residual Risk And User Review Focus

- 用户统一确认时需拍板 image 假设：JPEG/PNG/static WebP、20 MiB、最大边 12000、60MP、display 2560；这些不是当前实现事实。
- 48h staged/orphan 宽限以 `gc_rule_version=1` 固定，不回写既有 eligible_at；过短会影响重试，过长会增加存储占用。
- full decode/display normalize 位于首次幂等 callback，会占用数据库事务连接；实现需用固定 corpus 和超时/资源证据验证 budget，不能为优化重新引入双 claim。
- read pin 允许“permit 已成功后 detach，本次响应读完”，这是显式线性化取舍；匿名分享后续仍需独立 token-bound ref，不能复用 Bearer permit。

## 6. Verdict

- Status：`passed`；
- Blocking：none；
- Important：none；
- Next：运行 feature workflow hook，返回 `cs-epic` 连续 child design batch；design 保持 `draft`，不单独请求用户确认，不进入 implementation/Goal execution。
