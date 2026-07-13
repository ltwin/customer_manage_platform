---
doc_type: feature-review
feature: 2026-07-12-reminder-engine
status: passed
reviewer: subagent
reviewed: 2026-07-13
round: 2
---

# reminder-engine 代码审查报告（round 2）

## 1. Scope And Inputs

- Design: approved
- Checklist: steps 全 done
- Prior review: round 1 `changes-requested`（REV-001 blocking）
- Diff basis: review-fix 相对 round 1 的增量（churn 终态、follow_up 套系名、SetStatus 门禁、前端 tab 同步、日志/死代码）
- Evidence: `go test ./internal/reminder/` 含 `TestChurnRequiresNoNonTerminalOrders` 通过

### Independent Review

- Detection: 原生 Task agent（round 2 复审）；OCR 本轮未重跑（round 1 已完成，增量面由 subagent + 本地核验覆盖）
- 环节 A: native-agent · completed
- 环节 B OCR: skipped（增量复审；round 1 已合并）
- Merge policy: 逐条核验；REV-001 解除成立
- Gate effect: none — blocking 清零，放行 QA

## 2. Diff Summary（review-fix）

- `service.go`: `isNonTerminalStatus` 对齐 order 终态；evalChurn/autoDismiss 使用之；evalFollowUp 回落套系名
- `repository.go`: SetStatus pending 门禁；ListPackageMeta；删 CustomerExistsActive
- `runner.go`: 扫描摘要带 account_id
- `service_test.go`: delivered 负例 + closed 正例 + delivered auto-dismiss
- 前端: panel active 守卫、onChanged 刷新 tab 计数

## 3. Adversarial Pass

- 复验 round 1 主故障：仅 delivered 老单 → 不再 churn；closed 后 churn；再 delivered → auto_dismiss
- 结果：主路径闭合；无新 blocking

## 4. Findings

### blocking

- none（REV-001 已修复并回归）

### important（未阻塞；QA/后续可跟）

- [x] REV-001 churn 终态口径 — **fixed** round 2
- [x] REV-002 follow_up 套系名 — **fixed**
- [x] REV-003 tab 计数同步 — **fixed**
- [~] REV-004 扫描日志 account_id — **partial**（runner 有；HTTP Scan 路径 service 日志仍无 account，可接受）
- [x] REV-005 SetStatus 交叉覆盖 — **fixed**
- [~] REV-006 测试矩阵偏薄 — **partial**（blocking 回归已补；httpapi/merge 契约测仍薄）
- [ ] REV-007 孤儿 N+1 — open（量级可接受，记 residual）
- [x] REV-008 panel active 守卫 — **fixed**

### nit / suggestion / learning / praise

- 见 round 1；N12 超时文案已改 background runner
- learning：跨域终态必须同源；双跑幂等挡不住首次错生成

## 5. Residual Risk

1. follow_up 仅 delivered 状态（契约字面）
2. digest 空窗 / merge 双 pending / 复购 cancel 静默 / 时区西移 / 首扫噪音（design 已知）
3. 孤儿 auto-dismiss N+1（REV-007）
4. httpapi 契约测与 customer.Merge 端到端测仍偏薄（QA 手工补）
5. generate-check 待提交后绿
6. 同类 agent 审查非异构

## 6. Test And QA Focus

- 仅 delivered 不 churn；closed 超阈值 churn；pending churn + delivered → auto_dismiss
- 双跑、生日矩阵、Settings+/me、merge、前端三面、404 反向

## 7. Verdict

- Status: **passed**
- Next: `cs-feat --stage qa`（或 goal 续跑进入 QA）
