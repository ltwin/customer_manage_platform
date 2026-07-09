---
doc_type: approval-report
unit: .codestable/features/2026-07-08-order-tracking
status: approved
reason: blocker
created_at: 2026-07-09
---

# Approval Report

## Decision History

- 2026-07-09：owner 回答“按照你的建议”，批准 Option 1：走 `cs-req backfill` 补 `requirements/order-tracking.md` 为 current，然后恢复 acceptance。

## Decision Needed

`order-tracking` 已实现并通过 code review / QA，但 feature frontmatter 仍是 `requirement: ""`；同时 `.codestable/requirements/VISION.md` 仍把「订单记录」列为待起草能力。

按 `cs-feat-accept` L3 规则，新增用户可感能力在验收阶段必须完成 requirement 落档。accept 不允许直接自由改长期 requirement，因此需要 owner 决定下一步。

## Why Now

验收第 6 节是 requirement delta / clarification 回写。若跳过，后续 feature 只会从 roadmap 与代码里推断「订单记录」能力，`requirements/` 的当前能力清单会漏掉已经上线的核心能力。

## Context

- 设计文档：`.codestable/features/2026-07-08-order-tracking/order-tracking-design.md`
- Review：`.codestable/features/2026-07-08-order-tracking/order-tracking-review.md`，`status: passed`
- QA：`.codestable/features/2026-07-08-order-tracking/order-tracking-qa.md`，`status: passed`
- Requirement 现状：无 `requirements/order-tracking.md`
- VISION 现状：「订单记录」仍在“待起草的能力”说明中

## Options

1. **已批准：走 `cs-req backfill` 补 `requirements/order-tracking.md` 为 `current`，再恢复 acceptance**
   - 适合现状：能力已经实现完成，属于从未写过 req 的 backfill。
   - 需要 owner review 一份人话 requirement 初稿。

2. **先停在 blocked，暂不补 requirement**
   - 保留当前实现、review、QA 结果，但 acceptance 不写 passed，不回写 roadmap done。
   - 适合 owner 想先人工检查 requirement 边界或改能力命名。

3. **显式批准 requirement override：本 feature 不落 req**
   - 不推荐。会让 `requirements/` 与真实能力脱节，也违背本项目 VISION 里的待起草提示。

## Recommendation

选择 1：立即用 `cs-req backfill` 为「订单记录」补一份 `status: current` 的 requirement。建议 slug 为 `order-tracking`，pitch 可收敛为：

> 把每次约单从咨询到交付收尾记清楚，尾款、状态和历史记录都有据可查。

## Risks And Tradeoffs

- 选择 1 会新增一份长期 requirement，需要 owner 认可文案和边界；这是正确的流程成本。
- 选择 2 会延迟 acceptance 和 roadmap done 回写，但不改代码。
- 选择 3 会留下长期文档缺口，下个 feature 读能力层时容易漏掉订单域边界。

## Non-Automatic Actions

本报告不会自动 commit、merge、push，也不会自动把 feature 验收标记为 passed。未得到 owner 明确答复前，不会写 `requirements/order-tracking.md`，也不会把 roadmap `order-tracking` 改成 `done`。

## After You Answer

- 若选 1：进入 `cs-req backfill`，起草并落盘 `requirements/order-tracking.md`，更新 `VISION.md`，然后回到 `cs-feat-accept` 第 6 节继续验收。
- 若选 2：保持 acceptance blocked，等待后续 owner 决策。
- 若选 3：记录 owner override 后继续 acceptance，但验收报告第 6 节会明确该风险。
