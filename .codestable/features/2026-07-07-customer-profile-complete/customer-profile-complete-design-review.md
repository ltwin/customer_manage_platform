---
doc_type: feature-design-review
feature: 2026-07-07-customer-profile-complete
status: passed
reviewed: 2026-07-07
round: 2
---

# customer-profile-complete feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-design.md`
- Checklist: `.codestable/features/2026-07-07-customer-profile-complete/customer-profile-complete-checklist.yaml`
- Intent / brainstorm: none（roadmap 起头）
- Roadmap: `.codestable/roadmap/photographer-private-crm/`（主文档 §4.2/§4.3/§5 条目 3 + items.yaml）
- Related docs: `requirements/customer-profile.md`、`requirements/CONTEXT.md`、ADR-001/003、compound《AccountScope fail-loud》《OpenAPI tag 切片》
- Code facts checked: `backend/internal/customer/customer.go`、`platform/httpapi/customers.go`、`platform/store/scope.go`、`store/migrations/0002_customers.up.sql`、`api/openapi.yaml`（PATCH/identities/notes/merge 段）、`frontend/src/pages/CustomerDetailPage.tsx` / `CustomersPage.tsx`

### Independent Review

- Status: completed（round 1 + round 2 各一次独立 Task agent 只读审查）
- Detection: native-agent（无 paseo 工具；同类 agent 降级已记录残余风险）
- Provider / agent: Claude Task agent 独立上下文
- Raw output: 两轮均已回传主 agent 并逐条本地核验合并
- Merge policy: round 1 的 B-1/I-1/I-2 关键事实由主 agent 复读代码与 OpenAPI 二次确认后采纳；round 2 复审确认 FDR-001~013 全部 resolved，新报 1 important（N1）+ 2 nit（N2/N3）已在定稿前修复
- Gate effect: none（reviewer 已完成，verdict 在其结论之后定稿）

## 2. Design Summary

- Goal: 补全客户档案生命周期——渐进字段/渠道修正（null 三态清空）、身份增删（末位守护）、备注、归档恢复、merge（含转介绍指针重定向与自指清空）
- Key contracts: 5 个已定义 OpenAPI 操作激活（tag 切片 + 逐端点 409 矩阵 + nullable + status 子集枚举）；merge 单事务迁移面 = 身份 + 备注 + referrer 重定向 + source 置 merged；状态矩阵 merged 只读（409 customer_merged）/ archived 可编辑；Create/PATCH 介绍人须 active（D8）
- Steps: 9 步（含微重构第 1 步；超 4-8 区间有 roadmap 分片验收明文依据）
- Checks: 12 条，来源全部可追溯 design
- Baseline / validation: `make check` + generate 零漂移 + go test + npm build；含 generate-check 临时 index 基线风险提示

## 3. Findings

### blocking

- none

### important

- [x] FDR-001（round 1 blocking）建档 Create referrer 校验缺失 —— resolved：D8 决策 + §2.1 现状/变化表 + A4b 场景 + Coverage Matrix 独立行 + checklist S3
- [x] FDR-002 409 矩阵不完整 —— resolved：D6 逐端点矩阵 + checklist S2 action/exit_signal
- [x] FDR-003 null 清空不可表达 —— resolved：D6 nullable 增量 + D9 三态实现口径（禁手写 DTO）
- [x] FDR-004 自指清空终态未闭合 —— resolved（owner 拍板）：允许「referral+空 referrer」合法历史态，不改 channel；D1/A10/Interface⑤/checklist 一致
- [x] FDR-005 PATCH 自指未拦截 —— resolved：D2 自指 400 + A4
- [x] FDR-006 不变量作用域 —— resolved：限定「非 merged 客户」，术语表/D3/A10 同步
- [x] R2-N1 D4「作为 merge target → customer_merged」与 D5/A11 的 merge_conflict 矛盾 —— resolved：D4 删去该项，merge 端点统一 merge_conflict

### nit

- [x] FDR-007 行数 ±1 —— resolved（改为约 281/316）
- [x] FDR-008 术语结论句 —— resolved（新增 1 条）
- [x] FDR-009 notes 并列次序 —— resolved（created_at DESC, id DESC）
- [x] FDR-010 单独传 referrer 未定义 —— resolved（400）
- [x] FDR-011 archived referrer 404 说明 —— resolved（D6 契约描述注明）
- [x] R2-N2 S2 exit_signal 缺 409 断言 —— resolved
- [x] R2-N3 现状 bullet 重复 —— resolved（合并）

### suggestion

- [x] FDR-012 merge_conflict 前端先重取详情 —— 已纳入关键假设⑤
- [x] FDR-013 status 子集枚举 —— owner 拍板收窄，已入 D6

### learning

- 错误语义增量要按「错误码 × 端点」矩阵对 OpenAPI 逐格核对，不能只核被点名的端点（compound 候选，与《openapi-roadmap 双向核对》配套）
- 「不变量」措辞带作用域：写「任何时刻」前先过一遍终态实体（merged 壳）

### praise

- 现状→变化事实纪律好：占位 Notes、scope 写面、缺表、五操作 tag/409/nullable 现状逐条与代码相符
- roadmap §4.2「转介绍指针语义」增量真实回写且与 D1/D2 逐句一致，REV-006 正式闭环，未绕开契约
- 明确不做配 grep/diff 级反向核对表；挂载点按 deletion test 收紧到 4 点
- 9 步切片超区间有 roadmap 明文依据且主动说明；微重构四步可证明序列完整

## 4. User Review Focus

- 用户已拍板：referrer 状态断链方案（merge 重定向 + 自指清空 + 归档保留指针）、referral+空 referrer 合法历史态、status 枚举收窄、仅 active 可被选介绍人、channel 可改且联动
- implement 需要重点遵守：merge 事务原子性与账号过滤、末位守护同事务防并发、AccountScope fail-loud 不绕开、D9 三态禁手写 DTO
- code review / QA / acceptance 需要重点复核：409 错误矩阵逐端点落地、null 清空三态、双账号隔离用例、A4b 建档收紧

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | A1-A16 + A4b 每格有 step/证据/命令 | none |
| DoD Contract | pass | E | 五阶段 DoD + 4 条命令 + artifacts 齐 | none |
| Steps and checks traceability | pass | E | 9 steps exit_signal 均 yes/no，12 checks 全部可追溯 | none |
| Roadmap contract compliance | pass | E+C | §4.2 增量已回写一致；Create/PATCH 双面执行齐备 | none |
| Module interface design | pass | C | deep/seam/adapter 结论经代码核验成立 | none |
| Validation and artifacts | pass | E | 命令具体可跑、交付物可从仓库反查 | none |

Summary: E=5, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- notes 无分页无上限：契约如此，自用阶段可接受；将来加分页属契约变更走 `cs-roadmap update`
- merge 迁移面「随域生长」依赖 order/reminder feature 自觉补用例：roadmap 已写死义务，属流程约束非代码约束
- A15 UI polish 靠截图人工判定，可证伪性有档差：UI 类验收固有属性
- 独立 reviewer 与主 agent 同为 Claude 系（非异构审查）：残余风险记录在案
- D9 三态实现机制（oapi-codegen 对 nullable optional 的表达）留给 implement 首步验证；若 codegen 表达不理想，回 design 调整 D9 口径而非绕契约

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review；用户放行后 design `status: draft → approved`，进入 `cs-feat-impl`
