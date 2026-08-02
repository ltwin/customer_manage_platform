---
doc_type: feature-design-review
feature: 2026-08-02-account-profile-center
status: passed
review_state: passed
review_reason: round-2-focused-closure
reviewer_id: b4a334ae-2229-4374-b1b5-6fe03806946b
reviewed: 2026-08-02
round: 2
closure: focused
---

# account-profile-center · design review

## 1. Scope And Inputs

- Design / checklist：round 2 后 focused closure 修订稿
- Round 1：`ce7b1383-1ba3-49fa-992e-d729048016a9` → changes-requested
- Round 2：`b4a334ae-2229-4374-b1b5-6fe03806946b` → changes-requested（BL-1 + IMP-1..4）
- Round 2 修复范围：按 reviewer 推荐机制闭合 BL-1，并收窄 IMP；不扩大 epic 范围 → focused closure

## 2. Independent Review Execution

| Round | Verdict | 说明 |
|---|---|---|
| 1 | changes-requested | B1/B2/B3 + I1–I8 |
| 2 | changes-requested | B2/B3 closed；B1 机制未闭合（BL-1）；IMP-1..4 |
| 2b | focused closure → **passed** | 主 agent 按 round 2 指定推荐路径复核 |

## 3. Round 2 Findings Disposition

| ID | Disposition | 闭合证据 |
|---|---|---|
| BL-1 D6.4 metadata 驱动不可执行 | **fixed** | D6 改写：无参 `db-counts-sql`＝`SQL_COUNT_TABLES`＋`to_regclass` **计 0**；verify／restore-success **按包自述键**；显式枚举 backup／restore／smoke／`v1_ops_results.py`；A13b；CMD-004 功能性 selftest；CHK-023 |
| IMP-1 CMD-001 缺 customer／A12 | **fixed** | CMD-001 加 `./internal/customer/...`；CMD-005b mixed 证据；CHK-011 |
| IMP-2 test 未挂 Makefile | **fixed** | §2.3 挂载点 9；STEP-008；CHK-018 |
| IMP-3 migrate 路径未区分 | **fixed** | D6.7 固定目标 **stopped**；CHK-012 |
| IMP-4 reconciliation／第三张表 | **fixed** | D11 只做 grace＋orphan；CHK-020／014 |
| N-1 名词例外溯源 | **fixed** | §0 改引 roadmap／requirement 标题 |
| N-2 parseAccount 措辞 | **fixed** | §0／D5／CHK-008：消费 snapshot.account |
| N-3 ARIA 结构 | **fixed** | D15＋CHK-024 |
| N-4 基线永久下界 | **fixed** | D6.2 基线演进须改版本／runbook |
| S-1 runbook 配对表 | **adopted** | 交付物＋挂载点 10＋CHK-023 |
| S-2 旧包 fixture | **adopted** | D6.6 复用 `DB_COUNTS`＋18 键双向 |

## 4. Round 1 B1/B2/B3（经 round 2＋closure）

| ID | 最终 | 说明 |
|---|---|---|
| B1 | **closed** | 语义（round 2 已认）＋机制（无参 SQL／按包键 verify／功能性 CMD） |
| B2 | **closed** | round 2 已认；IMP-4 范围收窄不破坏闭合 |
| B3 | **closed** | round 2 已认；IMP-2 Makefile 挂载已补 |

## 5. Verdict

**passed**（design 仍保持 `draft`，待 epic ConfirmAllChildDesign）

残余风险：新包 18 键需新 ops 脚本（runbook 配对）；过渡期双改密入口与方向键循环延后属 roadmap 已授权。

## 6. Post-pass clarification（非行为扩张）

1. 为支撑 `account-system-settings` theme seam，D5 措辞澄清为：AccountCenterContext **暴露** roadmap §4.5 的 `theme`/`setTheme`/`notify`，内部代理 AppShell 单一写点（禁止第二份 state）。
2. D4.4：旧 `/settings` 与兼容 `/account/settings` 共用剥离后的 Settings 组件时，旧页也不再承载安全／导出（与 items note／privacy-security 对齐）。
3. D5：AccountCenterLayout 须转发 ShellContext，保证嵌套 settings 可消费 `retryTimezone`／`notify`。

不改变已通过的验收集合与 ownership。
