---
doc_type: feature-acceptance
feature: 2026-07-12-reminder-engine
status: passed
accepted: 2026-07-13
round: 1
---

# reminder-engine 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-13
> 关联方案 doc：`.codestable/features/2026-07-12-reminder-engine/reminder-engine-design.md`
> Owner terminal sign-off：confirmed（2026-07-13）

## 1. 接口契约核对

对照 design 第 2.1 节逐项反查当前工作区：

- [x] `POST /admin/reminders/scan`：`httpapi.ScanReminders` 解析可选 date，调用 reminder service 共用扫描入口，返回 `created/skipped/auto_dismissed`；非法日期走 `validation_failed`。
- [x] `GET /reminders`：支持 status/customer_id/due_before/page/page_size；repository 固定 `due_date ASC, id ASC`。Acceptance 已把该排序同步到 roadmap §4.3 和 OpenAPI operation description。
- [x] `POST /reminders`：只允许 custom，成功返回 201；repository 使用 `custom:{reminder_id}`，已同步 roadmap §4.4。
- [x] `POST /reminders/{id}/done|dismiss`：pending→目标态，重复目标操作幂等；done↔dismissed 交叉覆盖被拒绝。
- [x] `GET/PATCH /settings`：无行时返回默认值；PATCH 校验 IANA、days、digest_hour 与 shoot_type；churn_thresholds 在 entry 级叠加默认。
- [x] `GET /me` timezone：settings service 实现 `AccountTimezoneProvider`，由 composition root 注入；前端与扫描共用账号时区语义。
- [x] 名词层变化：`settings`、`reminders`、`reminder_scan_state` 三表存在；reminder/settings 两个 Go 域包存在；customer merge 在事务内重挂 reminders。
- [x] 数据约束：`settings.account_id` 主键；`reminders` 有 `UNIQUE(account_id,dedup_key)`、customer 复合外键、order_id 无外键；scan state 每账号一行。
- [x] 流程图节点均有落点：runner/manual → Service.Scan → auto-dismiss → birthday → follow_up → churn → 计数；runner 成功后才推进检查点。

偏差处理：验收反查发现 OpenAPI 原先未写默认排序，已补 `description: 默认按 due_date ASC, id ASC 稳定排序` 并重新生成 Go/TS 类型；没有保留未处理偏差。

## 2. 行为与决策核对

### 需求摘要与明确不做

- [x] 生日、回访、流失三类自动提醒和 custom 提醒均落地；done/dismiss、Settings、手动 scan、每日 runner 与前端三面均有运行证据。
- [x] 无 Telegram 生产调用或绑定 UI；`/settings/telegram/bind-token` 保持未注册 404。
- [x] dashboard 不属于本 feature，`/dashboard` API 保持未注册 404。
- [x] 无 `PATCH /reminders`，前端无编辑入口。
- [x] 零成交线索不产生 churn；merged/archived 客户不进入生成候选。
- [x] 归档客户既有非孤儿 pending 不被自动清理；孤儿提醒按提醒行清理，不读取客户 status。

### 关键决策与流程级约束

- [x] D1：Settings 独立归 `backend/internal/settings`；默认值、entry 叠加和 IANA 校验集中在 service。已同步 roadmap §3。
- [x] D2：账号时区由 settings provider 单点提供；date-only 规则集中经 AccountClock/local date 语义。
- [x] D3：reminder repository 经 AccountScope 批量读取 active customers、非 cancelled orders 与 package meta，再在内存组合；无 per-customer 候选 N+1。
- [x] D4：ReminderScanRunner 小时 tick、single-flight、按账号本地日检查点、失败不推进、账号间错误隔离。
- [x] D5：order_id 无外键；孤儿 pending 在下次 scan auto-dismiss。
- [x] D6：customer merge 原样重挂 source 的全部提醒到 target。
- [x] D7：custom dedup 固定为 `custom:{reminder_id}`，已同步 roadmap §4.4。
- [x] 账号隔离：新表带 account_id；查询/更新经 AccountScope；handler 不接收客户端 account_id。
- [x] 薄 handler：Gin 只在 httpapi；扫描规则、状态机与校验均在 reminder/settings service/repository。
- [x] 幂等：自动规则写入只经 `InsertOnConflictDoNothingReturning`，冲突计入 skipped；同日双跑 created=0。
- [x] 可观测：只保留扫描摘要、缺时间戳跳过和 runner 错误的结构化 slog；无临时打印。

### 挂载点反向核对与拔除沙盘

- [x] M1 migrations：0009 settings、0010 reminders/scan state 及 down 文件齐全。
- [x] M2/M3 HTTP/codegen：router 新增 7 个 operation；oapi-codegen include-tags 只增 reminder-engine；OpenAPI 与双端生成物同步。
- [x] M4 composition root：main 装配 settings repository/service、timezone provider、reminder service/runner，并纳入 server lifecycle。
- [x] M5/M6 webapp：App 两条新路由、AppShell 两个导航、CustomerDetailPage 接真实 CustomerRemindersPanel。
- [x] 跨域挂载：customer repository merge 重挂 reminders；auth timezone provider seam 被 settings 替换。
- [x] 文档挂载：requirements/VISION、roadmap/items、API manifest 与 feature reports 已纳入本次范围。
- [x] 反向 grep 发现的 reminder/settings 引用均落入上述迁移、域包、HTTP、composition root、customer merge、webapp、契约/生成物或文档挂载；prototype dashboard 数据是既有原型，不属于本 feature 新生产路由。
- [x] 拔除推演：逆序移除 webapp 路由/面板 → server runner/handlers/provider → customer merge 增量 → reminder/settings 域包 → migrations → OpenAPI tag/生成物，可恢复 feature 前运行面；需同时回退 requirement/roadmap/API manifest 状态，未发现清单外强耦合。

验收发现并修复：design 交付清单要求 `docs/api/manifest.yaml` 增 reminder/settings 条目，原实现缺失；现已补两条 pending API reference 索引并通过 YAML 校验。

## 3. 验收场景核对

证据来源：`reminder-engine-qa.md` round 2（passed）、`reminder-engine-evidence-pack.md`、本轮 final audit。

- [x] S1–S2 幂等/计数/date：集成测试与 HTTP 冒烟证明第二次 created=0、缺省/显式 date、非法 date 400。
- [x] S3–S6 birthday：窗口两端、双格式、跨年发生年、02-29 clamp、非默认/DST 时区由纯函数与集成测试覆盖。
- [x] S7–S8 follow_up：delivered 到期生成、未到期/非 delivered 不生成、缺 delivered_at 跳过。
- [x] S9–S12 churn：终态口径、阈值/套系类型/默认叠加、零成交负例、复购 auto-dismiss 通过；REV-001 delivered 非终态回归通过。
- [x] S13–S15 排除/lifecycle：merged/archived 不生成、merge 重挂、已删订单孤儿 auto-dismiss 通过。
- [x] S16–S19 API/Settings：过滤分页稳定排序、custom、done/dismiss 幂等与交叉门禁、跨账号、Settings 默认/校验、`/me` timezone 跟随通过。
- [x] S20 runner：同本地日一次、跨日触发、检查点与有界退出测试通过。
- [x] S21 客户提醒 tab：真实计数/列表；浏览器成功新增 custom 并忽略，状态刷新为 dismissed；截图 `evidence/customer-reminders-tab.png`。
- [x] S22 全局提醒页：真实列表、状态筛选空态、加载态、手动 scan 三计数反馈；截图 `evidence/reminders-page.png`。
- [x] S23 设置页：默认值/长阈值列表、非法 IANA 错误、恢复并保存成功；截图 `evidence/settings-page.png`。
- [x] 浏览器 console error：0。
- [x] 反向 404：bind-token/dashboard 仍未注册；无 Telegram 生产代码。

Review focus 全覆盖；QA failed/blocked 为 none。QA residual risk 已转入第 9 节，没有用 residual risk 承载核心未验证路径。

## 4. 术语一致性

- 「提醒（Reminder）」与「流失预警（Churn Alert）」已在 `requirements/CONTEXT.md`，代码/契约沿用 reminder/churn。
- 「账号」用于摄影师与隔离单位，「客户」用于拍摄对象；未在新增人读正文中把二者写成禁用的「用户」。
- Settings 在代码、OpenAPI、roadmap 和前端统一称 Settings/设置；未与 platform 配置混称。
- ReminderStatus/ReminderType、pending/done/dismissed、birthday/follow_up/churn/custom 在 Go/OpenAPI/TS 一致。

## 5. 领域影响盘点（提示而非代写）

- [x] CONTEXT 候选：`Settings（设置）` 是 reminder、`GET /me`、后续 telegram-digest 共用的账号级业务配置名词，当前 CONTEXT 未定义。建议后续用 `cs-domain` 补术语；acceptance 未越权修改 CONTEXT。
- [x] ADR 候选：Settings 独立域包是既有轻量 DDD 的自然延伸，可回退且 design 已判断不到 ADR 级；ReminderScanRunner 复用既有 runner 模式，不新增架构决策。无需新 ADR。
- [x] 流程约束候选：有效默认值 entry overlay、账号本地日 runner 检查点可沉淀到 compound；建议后续 `cs-keep`，不阻塞验收。

## 6. requirement delta / clarification 回写

- Requirement：`.codestable/requirements/reminder-engine.md`。
- 初始状态：draft；feature 目录最初无 approved req delta，acceptance 按治理规则暂停并写 `approval-report.md`。
- 2026-07-13 owner 选择 Option A，批准最小 delta。
- [x] 已机械应用：status→current、last_reviewed→2026-07-13、implemented_by 追加本 feature、添加实现变更日志。
- [x] 未改 pitch、用户故事、既有边界；Telegram/dashboard 仍在后续能力。
- [x] `requirements/VISION.md` 已把 reminder-engine 从 draft 移入 current。

## 7. roadmap 回写

- Roadmap：`photographer-private-crm`；item：`reminder-engine`。
- [x] items.yaml 原状态 `in-progress` 且 feature 指向当前目录；已机械更新为 `done`。
- [x] roadmap 第 5 节条目 8 同步为 `done`、对应 feature 为 `2026-07-12-reminder-engine`。
- [x] roadmap §3 补 Settings 域包归属（D1）。
- [x] roadmap §4.3 补 GET reminders 默认排序（A3）。
- [x] roadmap §4.4 补生日发生日年份（A4）与 custom dedup（D7）。
- [x] roadmap §7/§8 记录 digest 空窗、复购取消后 churn 静默、时区西移检查点自愈三项已知边界与本次变更日志。
- [x] items.yaml 通过 CodeStable YAML validator。

## 8. attention.md 候选盘点

- 本 feature 没有新增需要每个后续 feature 启动即知的项目注意事项。
- Docker/Testcontainers 依赖与串行测试归因已经存在于 attention.md，无需重复追加。
- 分流：Settings 有效默认值模式、runner 检查点模式 → `cs-keep` 候选；reminders/settings API reference → `cs-docs --mode api`；提醒/设置用户与开发指南 → `cs-docs` tutorial 候选。

## 9. 遗留

### 已知限制 / residual risk

1. `digest_hour` 早于 runner 当日扫描完成时存在摘要空窗；telegram-digest 应在推送前触发幂等 scan。
2. 复购使旧 churn dismissed 后若新订单再取消，原 dedup 行不会恢复 pending，可能静默到新的最近成交单出现。
3. 账号时区向西修改可能让检查点暂时领先本地日期，后续自然日推进后自愈。
4. 孤儿 auto-dismiss 当前逐提醒检查订单，存在可接受量级的 N+1（REV-007）。
5. 浏览器 QA 在共享 dev PG 留下一条已 dismissed 的 `QA 浏览器验证提醒`，只影响演示数据洁净度。
6. 真实 Git index 未提交时 `generate-check` 会显示本 feature 的新生成物；两份生成物必须与 OpenAPI 同一提交。

### 文档与沉淀候选

- API manifest 已登记 reminders/settings，完整 API 参考尚未生成。
- Settings 术语尚未通过 `cs-domain` 写入 CONTEXT。
- Settings overlay / runner checkpoint 可评估 `cs-keep`。

以上均不是未验证核心路径或验收缺口，不阻塞当前 feature；后续 bug 另走 issue 流程。

## 10. 最终审计

- 验证证据来源：`reminder-engine-qa.md` round 2（passed）。
- Evidence sources：`reminder-engine-evidence-pack.md`；本 feature 未生成独立 gate-results/dod-results JSON，命令与场景证据在 QA/本节落盘。
- 原始契约复读：design 第 1/2/3/4 节、checklist、review、QA、requirement、CONTEXT、ADR-001/003、roadmap 与当前 diff 已复核。
- 聚合命令：`GIT_INDEX_FILE=<临时 index> make check` → exit 0；前端 build、Go build、golangci-lint 0 issues、oxlint、全部 Go/前端测试、双端 generate-check 通过。临时 index 只模拟当前生成物与契约同提交，真实暂存区未修改。
- 支撑命令：`cd backend && go test ./... -count=1 -parallel=1` → exit 0；`make generate` 二次生成无漂移；`git diff --check` → exit 0。
- 浏览器：本轮在最终产品代码状态运行 `/reminders`、`/settings`、客户提醒 tab；加载/空/校验错误/保存/扫描/新增/忽略均有实际观察，console error 0。之后仅修改 spec/docs/OpenAPI description 并重新生成，无产品行为代码变化。
- 场景复核：re-verified 23 / trust-prior-verify 0；核心功能路径没有仅靠历史报告或 residual risk 放行。
- 交付物复核：迁移、reminder/settings 域包、handlers/routes、codegen、composition root、customer merge、两页+客户 tab、截图、API manifest、requirement、roadmap 均存在。
- 完整工作区复核：tracked diff 与 untracked feature/源码文件均纳入；当前分支 `feat/reminder-engine`；未发现无法归因的 baseline dirty 文件。
- diff 清洁度：无 whitespace error、console.log、fmt.Print、临时 TODO/FIXME/XXX、注释掉实现或 Telegram 凭证；仅允许的结构化 slog 存在。
- 知识沉淀出口：CONTEXT 候选 1、compound 候选 2、API/tutorial docs 候选已在第 5/8/9 节分流；attention 无新增候选。
- Final audit 修复记录：①补 `docs/api/manifest.yaml` reminders/settings 条目；②补 OpenAPI listReminders 默认排序说明并重新生成；两项修复后全部验证通过。
- 结论：技术验收通过，无 failed/blocked item；owner 已于 2026-07-13 完成 terminal sign-off，可将 goal 标为 complete/passed。
