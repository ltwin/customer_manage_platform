---
doc_type: roadmap-review
roadmap: photographer-private-crm
status: passed
reviewed: 2026-07-05
round: 2
---

# photographer-private-crm roadmap 审查报告

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`
- Items: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`
- Related docs: `requirements/customer-profile.md`、`requirements/CONTEXT.md`、`requirements/adrs/001-account-scoped-data-model.md`、`brainstorms/photographer-private-crm/brainstorm.md`、`attention.md`
- Code facts checked: none（greenfield 仓库，无业务代码；roadmap 未声称复用任何现有代码，已核实仓库确为空）
- Compound 检索：`.codestable/compound/` 为空（仅 .gitkeep），无相关沉淀

### Independent Review

- Status: completed
- Detection: native-agent（宿主 Codex MCP，read-only sandbox）——异构 provider，非同类 agent，无降级
- Provider / agent: codex exec（SESSION_ID 019f35cb-b759-7283-a70b-6a07a1b58bf9）
- Raw output: 已回传主 agent（2 blocking / 5 important / 1 nit / 1 suggestion / 4 praise / 3 residual-risk，verdict 建议 changes-requested）
- Merge policy: 主 agent 已逐条对照 roadmap/items/req/ADR 原文核验，全部 finding 事实成立，无需驳回；与主 agent 本地审查发现（状态语义、merge 跨域生长、churn 噪音）合并去重
- Gate effect: round 1 verdict = changes-requested；修复后进入 round 2 复核

## 2. Roadmap Summary

- Goal completion signal: 全链路演示（建档→套系→订单→档期→定金→次日 TG 摘要→dashboard 五卡有数）+ 全部 items 终态；软信号（两周留存）明确排除在门槛外
- Module split: 7 模块（platform/customer/package/order/schedule/reminder/webapp），无 pass-through（通知刻意并入 reminder 域）
- Interface contracts: §4.1-4.6 六份契约，字段/错误码/幂等键/时区口径级
- Items: 11 条，minimal_loop = customer-core；风险热点 = reminder-engine（幂等+时区）与 platform-skeleton（基线+外部依赖前置）
- Dependency shape: DAG 无环（validate-yaml 通过 + 人工核对），最长链 1→2→3→7→8→11

## 3. Findings

> round 1 发现（RMR-001~009 来自独立审查，RMR-010/011 来自主 agent 本地审查）；round 2 已逐条复核修复落点。

### blocking

- [x] RMR-001 `roadmap.md#4.1/4.2/4.4` 业务日期与时区契约缺失（生日/今日/digest_hour 无日界口径）
  - Evidence: round 1 文本只有"ISO 8601 UTC"，Settings 无 timezone
  - Impact: 提醒跨日错位，"治忘"核心不可验收
  - Resolution: ✅ 4.1 增时区总约定；Settings 增 `timezone*(IANA, 默认 Asia/Shanghai)`；4.4 扫描/日界、dashboard"今日"、月度统计全部绑定该口径
- [x] RMR-002 `roadmap.md#4.2/4.4` 订单状态时间戳与提醒规则联动未定义（shot_at/delivered_at 可选但规则依赖）
  - Evidence: round 1 PATCH /orders 未定义自动写入；follow_up/churn 依赖这两个字段
  - Impact: 回访/流失提醒无法稳定验收，跨模块语义漏洞
  - Resolution: ✅ 4.2 新增"订单状态语义与跃迁"块：进入 shot/delivered 自动写时间戳（可覆盖修正）、未结清禁 closed（409 unpaid_balance）、slot 不反向联动订单状态、扫描遇时间戳缺失跳过并记日志；items 5/6/7 验收信号同步

### important

- [x] RMR-003 部署形态与 PII 边界未进拍板项 → ✅ 第 7 节新增"条目 1 启动前拍板包"（技术栈/存储引擎/部署与 PII 三项一次定）；4.6 明确导出文件 PII 责任；item 11 README 覆盖部署/备份/凭证
- [x] RMR-004 v1-hardening 塞导出、原子性弱 → ✅ 拆出独立条目 `data-export` + 新增 4.6 导出契约（counts 核对为验收点）；hardening 收窄为纯收口
- [x] RMR-005 扫描手动触发"命令/端点均可"含糊 → ✅ 4.3 定义唯一入口 `POST /admin/reminders/scan {date?} → {created, skipped, auto_dismissed}`
- [x] RMR-006 TG 外部依赖验证过晚（第 8 条才碰真机） → ✅ item 1 前置 bot 申请 + sendMessage 脚本级冒烟；token 凭证规则写入 4.5 并落 attention
- [x] RMR-007 Customer.archived 有字段无 API/行为定义 → ✅ 4.2 归档语义（不参与扫描/不可下单 409 customer_archived/默认列表隐藏）+ 4.3 端点行为 + item 3 范围补"归档"
- [x] RMR-010（本地）closed 语义与 unpaid 口径矛盾风险 → ✅ 并入 RMR-002 修复：closed=服务与收款均完成，dashboard unpaid 口径改为 status=delivered 且 balance_paid=false
- [x] RMR-011（本地）merge 契约随域生长会静默失效（order/reminder 晚于 merge 落地） → ✅ 4.2 写明"后落地域必须补 merge 迁移本域实体用例"；items 5/7 验收信号各加对应用例

### nit

- [x] RMR-008 `roadmap.md#4.3 套系域` "409 package_in_use 不存在"表述易误读 → ✅ 改写为"归档无 in-use 校验——存量订单继续引用历史套系"

### suggestion

- [x] RMR-009 customer-profile-complete 偏大 → 保持单条（同域内聚），item notes 增加"design 阶段按 merge/归档/notes/referral/渐进字段分片验收"

### learning

- churn 规则对零成交客户的噪音问题（本地发现，round 1 修复）：首版 churn 仅对有成交史客户生效，零成交线索跟进移入"明确不做+二期候选"——提醒类功能的初版宁可少报不可噪音刷屏，这是"治忘"工具的信任前提。

### praise

- 范围边界具体（支付/选片/微信自动化/多账号/客户侧全部点名不做），能有效防 scope creep
- 通知不独立成模块、TG 作 reminder 域内 injected port——避免了单通道下的 pass-through 假 seam
- ADR-001 正确贯穿：account_id 客户端永不传、过滤在基座强制、任何新表带归属
- 最小闭环（customer-core）选择正确：最快验证"登录→建档→可查"的第一块价值

## 4. User Review Focus

- **用户需要重点拍板**：① 条目 1 启动前拍板包——技术栈确认（Go+React 为推断）、存储引擎（MongoDB vs SQLite）、部署形态与 PII 边界；② 条目 3 与 4 的先后顺序（无技术依赖，纯产品偏好）；③ 默认参数初始值（生日前 3 天/回访 7 天/流失 180 天/摘要 9 点）
- **后续 feature-design 需重点复核**：4.2 merge 语义在 order/reminder 域落地时的用例补齐；4.4 时区日界用例；30 秒建档的交互实测
- **不能靠 roadmap review 完全确认的点**：`make check` 基线要到条目 1 才真实存在；TG 可达性要到条目 1 冒烟才证实

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Granularity Gate | pass | E | roadmap §2 表格：7 模块/11 items/DAG/契约，非单 feature 可装 | none |
| Goal Coverage Matrix | pass | E | §5 矩阵 8 行全部映射 item + 验证入口 + 证据类型 | none |
| DAG and minimal loop | pass | E | validate-yaml 通过 + 人工遍历无环；minimal_loop 唯一（customer-core） | none |
| Interface contract usability | pass | E/C | §4.1-4.6 字段/错误码/幂等键级；与 req/ADR/CONTEXT 无冲突 | feature-design 首条落地时回验 |
| Module interface depth | pass | C | 各模块 Depth 判断成文；无 pass-through（通知并入 reminder 有明确理由）；独立审查 praise 佐证 | none |
| 与 req/ADR/术语一致性 | pass | C | customer-profile 用户故事/边界逐条对照；ADR-001 约束进 4.1；术语用「订单/账号/客户」无禁用词 | none |

Summary: E=3, C=2, E/C=1, H=0, H-only core checks=none。

## 6. Residual Risk

- 技术栈与存储引擎未拍板前，items 描述中的"Go+React"是假设——条目 1 不得在拍板前启动（已写入 item 1 notes 启动前提）
- 除 customer-profile 外四份 req 未起草，roadmap 契约暂时承担部分需求职责——各条目 feature-design 时触发 cs-req draft 化解
- greenfield：验证策略当前只是规划可信，真实基线从条目 1 开始建立；条目 1 的 acceptance 质量决定全 roadmap 的验证可信度
- 独立审查与主审查虽为异构 provider，但输入材料同源（全部为本仓库文档）；契约与真实业务的偏差要靠 owner review 与首两条 feature 实践反馈

## 7. Verdict

- Status: **passed**（round 2）
- Next: 交给用户 review——round 1 全部 blocking/important 已修复并复核；用户确认后主文档 status 改 active
