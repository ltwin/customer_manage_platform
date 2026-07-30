---
doc_type: feature-design-review
feature: 2026-07-12-reminder-engine
status: passed
reviewed: 2026-07-12
round: 1
---

# reminder-engine feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-12-reminder-engine/reminder-engine-design.md`
- Checklist: `.codestable/features/2026-07-12-reminder-engine/reminder-engine-checklist.yaml`
- Intent / brainstorm: none（从 roadmap 条目起头；原始素材 = 2026-07-05 brainstorm 全局文档）
- Roadmap: `photographer-private-crm-roadmap.md`（§4.1-§4.4、§5 条目 8、§8 2026-07-09 变更日志）+ items.yaml
- Related docs: `requirements/reminder-engine.md`（draft）、CONTEXT.md、ADR-001/003、compound `2026-07-09-cross-domain-read-model`、`2026-07-07-openapi-feature-tag-slicing`
- Code facts checked: `api/openapi.yaml`（reminders/settings paths+schemas）、`httpapi/auth.go`（AccountTimezoneProvider:22）、`store/scope.go`（InsertOnConflictDoNothingReturning、ScalarAggregate:311 无 GROUP BY）、`customer/avatar_maintenance.go`（runner 先例）、`cmd/server/main.go`（serverLifecycle）、migrations 0002/0005、`customer/repository.go:328`（Merge 事务）、`order/model.go`、`oapi-codegen.yaml`、`CustomerDetailPage.tsx`（提醒 tab 占位:308/312）

### Independent Review

- Status: completed
- Detection: native-agent（无 paseo MCP 工具，使用宿主原生 Task agent 只读独立审查；同类 agent 降级已记录，残余风险=非异构视角）
- Provider / agent: Claude Task agent（独立上下文，只给原始材料，未透露本地结论）
- Raw output: 无 blocking / 4 important / 7 nit / 3 suggestion / 2 learning / 3 residual-risk（摘要合并至下文）
- Merge policy: 已逐条本地核验（scope.go:311 无 GROUP BY、repository.go:328 Merge、pages 目录 9+2 文件、openapi tag 描述"未实现"字样均实证）后合并
- Gate effect: none（reviewer 已完成并合并）

## 2. Design Summary

- Goal: 三类规则幂等扫描 + done/dismiss + Settings 落地可配置 + 进程内每日调度 + 前端三面垂直切片
- Key contracts: settings/reminders/reminder_scan_state 三表；settings 域（默认值 entry 级叠加）+ reminder 域（Scan pipeline：auto-dismiss→三规则）；timezone provider 单点替换；merge 迁移增量
- Steps: 8（S8 偏大，见 FDR-012，接受不拆）
- Checks: 24 条，全部可追溯到 design 章节
- Baseline / validation: make check + 生成物零漂移 + testcontainers flake 预检口径（与 attention.md 一致）

## 3. Findings

### blocking

- none

### important（本轮全部已修复）

- [x] FDR-001 `design.md#D1/§2.2-churn` churn_thresholds 缺项时阈值取值未定义
  - Evidence: D1 只写"行级叠加默认"，PATCH 校验不要求数组全覆盖，`Settings.churn_thresholds` OpenAPI 仅注"默认全类型 180"
  - Impact: 部分覆盖 PATCH 后其余类型阈值语义悬空，场景 11 预期不唯一
  - Fixed: D1 明确 entry 级叠加（全类型默认 180 逐条被覆盖）；场景 11 增"只 PATCH portrait 后 cosplay 仍按 180"用例；checklist 同步
- [x] FDR-002 `design.md#2.2-auto-dismiss/§3.2` auto-dismiss 与 archived 客户断言互相矛盾
  - Evidence: 排除面写"不进候选集"，§3.2 反向断言无限定语"归档客户 pending 保持 pending"，孤儿清理路径冲突
  - Impact: QA 会写出互相矛盾用例；孤儿 order_id 永久留存
  - Fixed: 明确 auto-dismiss 按提醒行评估不看客户 status；§3.2 断言限定"非孤儿"；checklist 同步
- [x] FDR-003 `design.md#A3` 排序假设只回写 OpenAPI，绕过语义权威源
  - Evidence: CLAUDE.md 硬规则 3 + §4 头注；GET /orders 排序先例经 roadmap update 钉入 §4.3
  - Impact: §4 与 openapi.yaml 单向漂移（本项目已为此建过 compound 的债型）
  - Fixed: 增"契约回写班车"——acceptance 时 `cs-roadmap update` 回写 §4.3 排序（A3）、§4.4 dedup 年份措辞（新增假设 A4）与 custom 键格式（D7）
- [x] FDR-004 `design.md#D3` churn 候选聚合超出 AccountScope 表达力、取数策略未落纸
  - Evidence: `scope.go:311` ScalarAggregate 注释明确不支持 GROUP BY；Query 无 JOIN；compound 禁 N+1 与裸 SQL
  - Impact: S4 临场选择易滑向 N+1 或子查询绕开基座过滤（ADR-001 执行方式风险）
  - Fixed: D3 补取数策略——每账号批量查询（customers/orders/packages 映射）+ 内存组合，零 AccountScope 扩展；checklist 增专项 check

### nit（已修复 6 / 接受 1）

- [x] FDR-005 清洁度例外漏"扫描完成摘要"日志（与 §2.2 可观测矛盾）→ 例外扩为三类，checklist 同步
- [x] FDR-006 birthday dedup 年份是对契约"{当年年份}"的裁定未标假设 → 升格为 A4 并入回写班车
- [x] FDR-007 "01:00 前完成扫描"过承诺 → 改为"跨日 tick 于 00:00-01:00 触发"，A2 放宽为"digest_hour 早于首次跨日扫描完成时刻的组合"
- [x] FDR-008 merge 事务位置失准（service.go → repository.go:328）、pages 计数 8→9 → 已改
- [x] FDR-009 OpenAPI tags description"未实现/settings-foundation"将过期 → S1 顺手更新，进推进策略与 checklist
- [x] FDR-010 验收矩阵 go test 路径以仓库根不可执行 → 统一 `cd backend &&` 前缀（design + checklist CMD-003）
- [x] FDR-011 规则生成行 content/customer_id/order_id 填充未声明 → §2.2 各规则补携带字段与 content 模板示意（模板细节归 implement）

### suggestion

- [ ] FDR-012 S8（前端三面 + 收尾 + make check）偏大可二分 → 接受不拆：三场景 exit_signal 已可独立证伪，实现期过重再拆（记录于此，implement 可引用）
- [x] FDR-013 手动 scan date 取值域应声明是否有意不限制 → 新增假设 A5（有意不限制，测试/补扫入口）
- [x] FDR-014 D1 settings 包归属建议回写 roadmap §3 而非 ADR → 采纳，写入 D1 与 §4

### learning

- "行可缺省 + entry 级叠加默认"的 settings 有效值模式可复用（telegram-digest/dashboard 都将消费 Settings）——落地后建议 `cs-keep` 进 compound
- 检查点驱动"每本地日一次"runner 是 avatar 小时 tick 的第二实例；第三个 runner 出现前不抽象，compound 记形状约定可防过早抽象

### praise

- 现状→变化全部有代码实证（auth.go:22、scope.go 唯一约束与 conflict 列含 account_id 严丝合缝、迁移序号、占位行行号）
- D5/auto-dismiss 恰在 2026-07-09 变更日志授权面内；bind-token/dashboard 404 严格沿用 tag 切片 compound
- D6 双 pending、首扫历史噪音选择"接受并说明"而非预支去重/抑制——与项目反预支复杂度口径一致

## 4. User Review Focus

- 用户需要重点拍板：A1（02-29→02-28）、A2（digest 空窗留给 telegram-digest）、A3/A4/D7（补齐型契约裁定 + acceptance 回写班车）、A5（scan date 不限未来）、D5（order_id 无外键）、D6（merge 双 pending 噪音接受）、前端三面范围
- implement 需要重点遵守：幂等唯一写入路径、AccountClock 时区单点、D3 批量取数禁 N+1、薄 handler、清洁度三类日志例外
- code review / QA / acceptance 需要重点复核：双跑零新增硬验收、时区/DST 注入用例、runner 注入 clock 用例、residual-risk 三条、契约回写班车执行

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design §3.3 覆盖 23 场景 + roadmap 条目 8 完成信号 7/7 | none |
| DoD Contract | pass | E | design §3.4 与 checklist dod.commands 字段一致 | none |
| Steps and checks traceability | pass | E | 8 steps exit_signal 可证伪；24 checks 均溯源 | S8 过重时可按 FDR-012 拆 |
| Roadmap contract compliance | pass | C | 规则口径逐条对照 §4.4；A3/A4/D7 补齐型裁定入回写班车 | acceptance 执行 cs-roadmap update |
| Module interface design | pass | C | settings deep module、Reminder 资源 seam、无假 adapter、D3 取数策略落纸 | none |
| Validation and artifacts | pass | E | CMD-001/002/003 可执行；交付物清单可从仓库事实反查 | none |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

1. 复购后订单取消 → 该客户 churn 因 dedup_key 撞已 dismissed 行而静默至新 closed 单出现——dedup_key 契约结构固有代价，acceptance 报告记已知边界（已入 design §4 观察项②）
2. digest_hour 空窗与宕机重启补扫时序：本 feature 无推送无实害；telegram-digest 以"推送前顺带扫描"闭合（design §4 观察项①）
3. PATCH timezone 西移可致当日不补扫（自愈型）——QA 可加观察用例（design §4 观察项③）
4. 独立审查为同类 agent（非异构 provider）——残余风险由用户整体 review 与后续 code review/QA 覆盖

## 7. Verdict

- Status: passed
- Next: 交给用户整体 review（design 保持 draft，用户确认后改 approved 进入 goal-package）
