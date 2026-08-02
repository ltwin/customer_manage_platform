---
doc_type: feature-design-review
feature: 2026-08-02-avatar-media-safety-net
status: passed
review_state: passed
review_reason: round-2-focused-closure
reviewer_id: 7b8e0238-92df-43d9-a8c6-244c8136ccbd
reviewed: 2026-08-02
round: 2
closure: focused
---

# avatar-media-safety-net · design review

## 1. Scope And Inputs

- Design / checklist：当前落盘修订稿（round 2 后 focused closure）
- Round 2 独立审查：`7b8e0238-92df-43d9-a8c6-244c8136ccbd`（changes-requested）
- Round 2 授权：blocking/IMP 修复后做 focused closure，不必完整复审

## 2. Independent Review Execution

| Round | Verdict | 说明 |
|---|---|---|
| 1 | changes-requested | `2c197f82-240a-4858-9b52-1e4df35ada46` |
| 2 | changes-requested | `7b8e0238-92df-43d9-a8c6-244c8136ccbd`；B1/B2/B4 等已闭合，新 BLOCK-1/2/3 |
| 2b | focused closure → passed | 主 agent 按 round 2 指定范围复核 BLOCK-1/2/3 与 IMP-1/4（及同批 IMP-2/3/5/6/7） |

## 3. Round 2 Findings Disposition

| ID | Disposition | 闭合证据 |
|---|---|---|
| BLOCK-1 A17/CMD-004 | **fixed** | D14：本 feature 自有 `test-avatar-media-v1-restore-wipe.sh`；不改 hardening catalog；不误用 `restore-success` 的 v1≡v2 manifest 摘要；物理+计数字表 marker；CMD-004 重写 |
| BLOCK-2 A18 grep | **fixed** | CMD-005 → 专用 scope 脚本；merge-base…HEAD + untracked；命中失败；无 `\|\| true` |
| BLOCK-3 content-validation | **fixed** | D10 拆分共享 decode confirm vs customer PNG normalize；登记读路径 DecodeConfig；CHK-016/017/018；A18 |
| IMP-1 编号错位 | **fixed** | §3 重排 A1–A18；Coverage Matrix 对齐 CHK |
| IMP-2 typed Key | **fixed** | D1 明确采用 typed Key/Prefix/Cursor；CHK-005 |
| IMP-3 部署顺序 | **fixed** | D12／CHK-014 正向「脚本不晚于镜像」 |
| IMP-4 marker 形态 | **fixed** | §0／D14：计数字表多余行 + 物理 account-profile 对象 |
| IMP-5 allowlist | **fixed** | D9 allowlist ∩ 等于 format |
| IMP-6 v1 投影 | **fixed** | D4／A7／CHK-004 |
| IMP-7 Docker 前置 | **fixed** | CMD-004 notes |
| SUGG-1 golden | **adopted** | D2／STEP-005 |
| SUGG-2 oracle allowlist | **optional D15** | 非 A17 前置 |

## 4. Focused Closure Ledger

| Check | Verdict | Class | Basis |
|---|---|---|---|
| BLOCK-1 可执行 harness | pass | E | D14＋CMD-004 不再使用不存在的 `--filter`／冻结 catalog |
| BLOCK-1 oracle 可表达 | pass | E | 自有脚本断言 counts+物理路径；显式禁止 v1≡v2 manifest 摘要 |
| BLOCK-2 scope 门禁 | pass | E | CMD-005 契约含 untracked、失败退出 |
| BLOCK-3 seam 形状 | pass | E/C | D10 vs roadmap §2/§4.3 原始字节；processor.go PNG 策略留在 customer |
| IMP-1 traceability | pass | E | A↔CMD↔CHK 无悬空引用 |
| IMP-4 marker | pass | E | 术语表双形态 |

## 5. Residual Risk

- R1 已缓解：A17 不再修改 `2026-07-22-v1-hardening` catalog。
- R2：CMD-004 仍依赖本机 Docker／镜像——notes 要求写明前置；CI 可复现性属后续。
- R3：真实 `account_profiles` 表清空证明仍属 profile-center。

## 6. Verdict

- **review_state: passed**
- design 保持 `status: draft`（epic_child_batch；统一确认前不标 approved）
- Blocking：none unresolved
- 下一步：交回 `cs-epic` child design batch → `account-profile-center`
