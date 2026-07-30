---
doc_type: issue-fix
issue: 2026-07-30-customer-total-spend-formatting
status: confirmed
path: fast-track
fix_date: 2026-07-30
tags:
  - frontend
  - customer-detail
  - money-formatting
---

# 客户累计消费金额格式错误修复记录

## 1. 问题描述

客户详情页「累计消费」直接显示接口返回的整数 `113800`，缺少金额单位换算、人民币符号、千分位和小数位，容易把分值误读为元金额。

## 2. 根因

OpenAPI 契约规定 `customer.stats.total_order_amount` 的单位为分，但 `frontend/src/pages/CustomerDetailPage.tsx` 原实现直接把该字段插值到页面，没有执行分到元的换算和人民币格式化。接口统计本身与同一客户订单数据一致，不需要修改后端或存量数据。

## 3. 修复方案

1. 新增客户累计消费专用纯函数 `formatCustomerTotalSpend`：金额除以 100 从分转换为元，使用 `zh-CN` 千分位，并固定保留两位小数。
2. 客户详情页通过该函数渲染累计消费；当前样例从 `113800` 变为 `¥1,138.00`。
3. 增加整数元、角、分和零值测试，并把测试接入 `package.json` 与项目 `make test` 门禁。
4. 不修改 OpenAPI、后端统计、订单金额展示或其他页面的金额精度，避免扩大为全站金额格式重构。

## 4. 改动文件清单

- `frontend/src/pages/CustomerDetailPage.tsx`：累计消费改用人民币金额格式化函数；保留该文件已有未提交的视觉与可访问性修改。
- `frontend/src/pages/customerDetailMoney.ts`：新增客户累计消费格式化函数。
- `frontend/scripts/customer-money.test.ts`：新增金额格式化回归测试。
- `frontend/package.json`：新增 `test:customer-money` 脚本；保留已有未提交的依赖与格式调整。
- `Makefile`：把金额格式化测试接入 `make test`。
- `.codestable/issues/2026-07-30-customer-total-spend-formatting/`：新增 report、approval-report 和本修复记录。

## 5. 验证结果

- [x] 针对性测试：`cd frontend && npm run test:customer-money`，1/1 通过。
- [x] 前端 lint：`cd frontend && npm run lint`，通过。
- [x] 前端生产构建：`cd frontend && npm run build`，通过；仅有既存的 500 kB chunk 提示，无构建错误。
- [x] 浏览器复现：打开报告中的客户详情页，累计消费显示为 `¥1,138.00`，不再显示 `113800`。
- [x] 浏览器布局：金额元素和父统计格均无横向溢出；金额宽约 148 px，父格宽约 177 px。
- [x] 影响面回归：客户名称、订单数 `1`、最近拍摄及详情页其他信息正常显示。
- [x] 其余前端测试：除下述既有遗留外，额外运行的 71 条测试全部通过。
- [x] Diff 卫生：本次涉及的 tracked 文件通过 `git diff --check`。
- [x] 独立代码审查：`customer-total-spend-formatting-review.md` 为 `status: passed`、`reviewer: subagent`，无 blocking、important 或 nit。

## 6. 遗留事项

> 顺手发现：`frontend/scripts/prototype-contract.test.mjs` 的“new customer referral picks from active customers instead of manual id input”测试失败。它要求 `CustomerNewPage.tsx` 直接调用 `listCustomers`，而当前实现改为使用 `CustomerPicker`；`CustomerNewPage.tsx` 在本工作区相对 HEAD 无修改，且与本次金额格式化无调用关系。本 issue 不越界修复，建议后续单独确认是更新过时契约测试，还是恢复其预期实现。

本次没有其他遗留风险；独立代码审查结果将在 review 报告中记录。
