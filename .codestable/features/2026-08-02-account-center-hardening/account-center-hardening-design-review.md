---
doc_type: feature-design-review
feature: 2026-08-02-account-center-hardening
status: passed
review_state: passed
review_reason: round-2-focused-closure
reviewer_id: 4791e8a9-7840-4acf-af1a-82742cf424eb
reviewed: 2026-08-02
round: 2
closure: focused
---

# account-center-hardening · design review

## 1. Scope And Inputs

- Round 1：`fa84bca5-4f18-4399-9a1d-46884bbd55cb` → changes-requested
- Round 2：`4791e8a9-7840-4acf-af1a-82742cf424eb` → **approved-with-conditions**（BL-1..4 closed；F-1 must-fix）
- Round 2b：focused closure → **passed**

## 2. Independent Review Execution

| Round | Verdict | 说明 |
|---|---|---|
| 1 | changes-requested | BL-1..4 |
| 2 | approved-with-conditions | BL closed；F-1 删旧页 vs make check |
| 2b | focused closure → passed | D11＋F-2..F-8 关键项 |

## 3. Round 2 Findings Disposition

| ID | Disposition | 证据 |
|---|---|---|
| BL-1..4 | **closed**（r2 已认） | 复验深度／证据形态／v2 restore／deferral |
| F-1 legacy 测试互斥 | **fixed** | D11 三选一＋A14／STEP-001b／CHK-014 |
| F-2 openapi 零改动 | **fixed** | D9／A9／CHK-009 |
| F-3 `-p=1` | **fixed** | CMD-003 |
| F-4 STEP-000b | **fixed** | 四项依赖清单 |
| F-5 死 CSS/import | **fixed** | D3.2／A13／CHK-010 |
| F-6 HTTP /settings | **fixed** | D3.3／A12 |
| F-7 证据路径 | **fixed** | D5 feature evidence/ |
| F-8 customer-avatar Makefile | **fixed** | D7／A8／CHK-014 |
| accepted-residual | **fixed** | D4／D8 白名单 |

## 4. Verdict

**passed**（design 仍 `draft`，待 epic ConfirmAllChildDesign）
