---
doc_type: feature-design-review
feature: 2026-08-05-plan-ingestion-capture
status: passed
review_state: passed
review_reason: round-3-reparse-reference-candidate-focused-pass
reviewer_id: /root/creative_roadmap_focus_review
reviewed: 2026-08-05
round: 3
supersedes_round: 2
review_mode: focused-delta
---

# plan-ingestion-capture feature design 审查报告

## 1. Scope And Inputs

- Design / checklist：`.codestable/features/2026-08-05-plan-ingestion-capture/`；
- Parent：approved creative-shoot-planning requirement、active roadmap/items 与 roadmap review round 6；
- Upstream designs：`shoot-plan-core` round 4 passed、`planning-reference-assets` round 3 passed；
- Prototype：tracked v2 `plan-ingestion.html` 与 README authority/conformance；
- Code facts：AccountScope/TxAccountScope、single-hash idempotency ledger、OpenAPI/generated clients 和当前无 ingestion/outbox/crawler 实现。

### Independent Review

- Reviewer：`/root/creative_roadmap_focus_review`；全程只读，未修改文件；
- Raw results：round 1=`changes-requested`（3 blocking、4 important），round 2=`changes-requested`（2 important），round 3=`pass`；
- Gate：全部 blocking/important 关闭后才写 `passed`；design 保持 `draft`，返回 epic batch。

### Review History

| Round | Verdict | 主要问题 |
|---|---|---|
| 1 | changes-requested | URL/speaker 顺序、缺 ReadinessLinkCandidate、误用 media proof、多 editing session、terminal delta、parser 唯一性、source-change ack |
| 2 | changes-requested | 前五项关闭；reparse 对齐算法与 ReferenceLinkCandidate 持久 schema 仍不闭合 |
| 3 | passed | frame_v1 + exact LCS/gap/reappear、reference closed override/ack 与 production Noop guard 全部关闭 |

## 2. Design Summary

- 保守 parser v1 只做 URL-first、paragraph/blank、严格 speaker prefix、placeholder、fixed readiness cue、exact duplicate 与固定 limits；无 AI/OCR/crawl。
- CandidateSnapshot 完整包含 Content、ReadinessLink、ReferenceLink、Dropped 与 staged intents；source change 使用 per-candidate revision acknowledgement。
- 每 plan 最多一份 editing session；create loser 409 返回 active session ref，GET 用于恢复，terminal 不 resume/copy/move。
- `plan-ingestion.commit.v1` 直接引用 upstream exact canonical/result；0 core candidates 使用 additive plan snapshot，不改变 core-only 400。
- combined commit 只有一个 outer ExecuteInScope；media 使用自己的 HolderProof，ReferenceLink 使用独立 PlanTargetProof。
- ordinary/ready/abandon 使用同一 server-time accumulator，terminal 前计 final delta；stage-1 report 与 owner gate 权限分离。
- tracked v2 brief 静态例不进入 v1 commit，避免静默扩 BatchInput/canonical；正式 UI 仍保留三步摄取、候选/丢弃恢复、link/media 和 conflict IA。

## 3. Findings And Closure

### blocking

none。

### important

none。

### nit

none。round 3 的 raw-unpadded base64url 建议已在落报告前补齐。

### 已关闭 finding

| ID | 原严重度 | Closure evidence |
|---|---|---|
| R1-B1 | blocking | URL tokenizer先读raw source；speaker grammar排除协议/时间；pure/speaker/time/query/标点golden |
| R1-B2 | blocking | ReadinessLinkCandidate完整model、0..N显式editor、snapshot provenance与commit mapping |
| R1-B3 | blocking | 独立PlanTargetProof/reference authorizer；media HolderProof不可互换 |
| R1-I1 | important | partial unique `(account,plan) WHERE editing`；并发create winner/409 loser/recovery |
| R1-I2 | important | ready/abandon复用accumulator，在terminal CAS前计final delta；replay=0 |
| R1-I3 | important | parser v1机械URL/speaker/blank/placeholder/cue/duplicate/title/sort/limit表 |
| R1-I4 | important | source_changed/source_missing重置needs_confirmation；逐项revision ack，旧tab409 |
| R2-I1 | important | length-framed raw-unpadded ID + exact LCS backtrack/gap/reappear算法与对齐golden |
| R2-I2 | important | ReferenceLinkCandidate补raw URL/label/target/source/revision/action完整schema与closed override |
| R2-N1 | nit | S6加入route-enabled+Noop sink production composition必失败 |

## 4. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Parser determinism | pass | E | 完整规则顺序、framing、LCS tie-break与golden清单 | implementation提交versioned corpus |
| Candidate provenance | pass | E | 三类candidate ID/source/ack/keep-discard链闭合 | reparse/multi-tab integration |
| Upstream commit compatibility | pass | E/C | exact v1 fields/response owner；新增ack只在preview session | table-driven canonical fixture |
| Combined transaction | pass | E/C | session/core/media/reference/observation/ledger单tx与callback=0 | PG双连接/故障注入 |
| Capability isolation | pass | C | media HolderProof / PlanTargetProof 类型与operation分离 | dependency/type assertion |
| Observation accuracy | pass | E | one active session、server tick、300s cap、terminal final delta | fake clock/report hash |
| SSRF/scope exclusions | pass | E/C | raw URL只保存、无server HTTP client、schema/route guard | dependency/transport fail fixture |
| Checklist traceability | pass | E | S1-S8、12 checks、CMD-001..006与A1-A17对齐 | execution不删证据 |

Summary：E=7，C=4，H=0；H-only core checks=`none`。

## 5. Residual Risk And User Review Focus

- 需统一确认输入/retention假设：200KiB/50k rune/1000行、300内容/100 URL/50 asset，以及30日editing inactivity/raw redaction。
- 确定性 parser 有意保守，readiness默认 optional/unassigned、tags为null；G2真实样本若不过线应改交互，不得偷接模型。
- terminal observation 后同plan继续摄取只记post-terminal count、不改旧G2，这是防证据重写取舍。
- v1摄取不写CreativeBrief；用户保存后在工作台编辑。若整体确认希望摄取brief，必须升级core batch和ingestion commit operation，不可改v1。

## 6. Verdict

- Status：`passed`；
- Blocking/important：none；
- Next：运行feature hook并返回cs-epic batch；不单独请求确认，不进入implementation、Goal execution或stage-1 evidence go。
