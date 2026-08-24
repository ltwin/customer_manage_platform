---
epic: ../epics/dashboard-v2-redesign.md
item: ITEM-4
status: implementing
created: 2026-08-24
---

# feat · /dashboard/v2 聚合读模型（ITEM-4）

## 目标

交付 epic `dashboard-v2-redesign` ITEM-4：OpenAPI `GET /dashboard/v2` 契约 + dashboard 域聚合实现 + router 手工注册 + codegen 切片；一次返回 next_shoot / today_slots / today_openings / delivery_queue / revenue_waterfall（含环比）/ schedule_utilization / channel_matrix / due_reminders 行样式。openings/利用率 Go 权威实现与前端 TS 以共享 golden fixtures 对拍（DEC-9）；「在途」等口径在本子项冻结并回写 epic 共享语言表。

## 现场（开工检索）

- 语义权威：roadmap §4.3 新增 `GET /dashboard/v2` 契约块（2026-08-24 定稿，先于实现——ITEM-6 预门槛）；epic 共享语言表「在途」「90 天复购占比」「下一场」三行同步冻结回写（epic 明文要求的写回，approved_revision 变更记入游标）。
- dashboard 域现状：五块聚合（model/service/repository），TimezoneProvider seam 注入 settings.Service；today_slots 复用 schedule.AssembleListItems（D8）。
- TS 侧算法现状：`frontend/src/pages/calendar/model.ts`（resolveWorkingWindow/openingsForDay/calculateMonthOverview）+ `components/schedule/timezone.ts`（projectSlotToLocalDays/localDayRange，Temporal DST compatible）；取消判定 `isCancelledShoot` = shoot ∧ order_status=cancelled。
- 关键风险（开工即验）：Go 原生 `time.Date` 在 DST 跳跃日给跳跃前 offset 的较早时刻，与 Temporal 'compatible'（跳跃后）**不一致**——用 /tmp 探针实证（2026-03-08 02:30 NY：Go=06:30Z，Temporal=07:30Z）。
- compound 命中：`2026-07-09-cross-domain-read-model`（dashboard repository 直查多表 + AccountScope 受控聚合，不注入他域 service）、`2026-07-07-openapi-feature-tag-slicing`（OpenAPI 全量、include-tags=dashboard 已含、router 手工注册）。核验：oapi-codegen.yaml include-tags 已有 dashboard；两 compound 均仍然成立，直接影响实现归属与暴露面。

## 边界与取舍

- **「在途」冻结**：status ∈ {scheduled, shot, selected, retouching} 且非 cancelled 的 price 之和（存量时点值、无窗口；price NULL 跳过）；已交付未结清归待收段不属在途。已交付未结清（receivable）= delivered ∧ balance_paid=false 订单的 outstanding 之和（NULL→0；DEC-10 录满未点收讫计笔数不计金额），复用 v1 loadUnpaidOrders 行集（与 v1 unpaid_orders 口径同源）。
- **瀑布其余口径**：confirmed=既有 revenue_confirmed 30 天窗 + 上一相邻窗环比（prev=0 缺省）；cash_received_30d=paid_at 落窗的 amount_paid 之和（历史无 paid_at 不虚构时间）；AOV=confirmed÷计入笔数（price 非空）；90 天复购=拍摄 ≥2 单客户占比（4 位小数）；账龄=今日−最早未结清已交付 delivered_at。
- **next_shoot**：end_at 越过本地今日 00:00 即候选（含进行中的跨日拍摄），start_at ASC 取第一个未取消 shoot；实现扫描上界 365 天（工程上界，非语义）。
- **Go 权威算法落点**：`backend/internal/schedule/openings.go`（档期语义归属 schedule 域；dashboard 消费，不注入 service）。DST 显式 compatible 解析（歧义取较早、gap 推移跳跃后）；错误消息与 TS 逐字一致（golden errors 面对拍）。利用率分子逐投影累加不合并重叠——Calendar v2 既有口径，golden 已钉（重叠小时双计，conflict_days 另行标记）。
- **golden fixtures 归宿**：`api/golden/openings/*.json`（跨端契约资产随 OpenAPI 同级）；生成器 `frontend/scripts/gen-openings-golden.ts`（期望值来自现行 TS 实现=双实现并存期基线）；TS 守护测试 `openings-golden.test.ts` + Go 对拍 `openings_golden_test.go`；npm `test:openings-golden` + Makefile test 接线。改语义必须先改用例重生成，禁止手改期望值迁就实现。
- **settings 扩展**：settings.Service 新增 AvailabilityForAccount（与 TimezoneForAccount 对称）；dashboard.Service 的 seam 从 TimezoneProvider 升级为 V2SettingsReader（时区+可约偏好），settings.Service 天然满足；schedule 包不 import settings（AvailabilityPlan 本地镜像 + dashboard 适配层转换），settings 不 import schedule/dashboard（无环验证过）。
- **month 档期/next_shoot 复用 AssembleListItems 三次调用**（today/月/年窗，各自 2 查询；摄影师单账号量级），不为其扩 AccountScope 聚合面。
- **矩阵累计无窗口**：balance_paid ∧ 非 cancelled ∧ price 非空按快照聚合；无数据渠道不出空行；行序 total DESC, channel ASC。
- ITEM-5（前端消费）不在本子项；Calendar 前端切换消费服务端结果归二期（DEC-9 双实现并存记遗留）。

## 证据

- roadmap §4.3 v2 契约块 + epic 共享语言表三行回写（commit 内 diff）。
- `backend/internal/schedule/openings_test.go`：DST compatible（gap 07:45Z/歧义 05:15Z）、未配置日 nil、格式与 end≤start 错误消息、取消/相邻合并/min 边界、跨日投影、月分母分子/分母 0 → nil。
- `backend/internal/schedule/openings_golden_test.go` ×4 fixtures（basic-weekday / dst-window-resolution / openings-boundary / month-utilization）与 TS 全对拍；`frontend/scripts/openings-golden.test.ts` 同 fixtures 守护。
- `backend/internal/dashboard/dashboard_v2_test.go`：八块全量断言（固定时钟 2026-07-14 SHA）+ 时区失败/非法时区不回退 + 无 availability 边界（working_window nil、utilization nil、空派生指标全缺省）。
- `backend/internal/platform/httpapi/dashboard_v2_test.go`：路由 401/200、next_shoot/瀑布/矩阵/逾期队列/待办摘要/默认 availability 路径。
- `make check` 单轮全绿（见状态节验证记录）。

## 验收（epic ITEM-4 要点对照）

- [x] 一次返回八块（next_shoot/today_slots/today_openings/delivery_queue/revenue_waterfall 含环比/schedule_utilization/channel_matrix/due_reminders 行样式）
- [x] 复用 schedule 摘要装配（AssembleListItems），无逐行 N+1（摘要批量 fetch）
- [x] openings/利用率 Go 权威实现 + TS 共享 golden fixtures 对拍（DEC-9）
- [x] 「在途」定义定稿并回写共享语言表（+90 天复购、下一场）
- [x] 渠道矩阵含未归因桶（unattributed）
- [x] 时区/日界同源（全部窗口由 Settings.timezone 单点派生；测试锁时区失败不回退）
- [x] 跨日 shoot、取消 slot、无 availability 边界明确（域测+golden 各有专门用例）
- [x] change review 独立闭环（round 1「可合」0B/0I/3N，nit 全修；round 2 终审见下）
- [x] make check 全绿（r2 单轮 EXIT=0；r1 失败仅为 attention.md 记录的 Testcontainers port 5432 偶发环境故障，串行重跑过，非代码缺陷）

## 状态

- 完成（2026-08-24）

## 验证记录

- make check r1：EXIT=2，两处失败同根——`TestAvatarMaintenanceA23…` 的 Testcontainers `port "5432/tcp" not found` 偶发（auth-security catalog 的 account-scope-consumers 目录用例复跑同一测试连带失败）；串行重跑该包与目录脚本均过（attention.md 已记录该环境 flake，ITEM-1 亦遇）。
- make check r2：EXIT=0 单轮全绿（33 Go 包 + 前端全套含 test:openings-golden + 4 shell 门禁 + generate 零漂移）。日志 /tmp/makecheck-item4-r1.log、/tmp/makecheck-item4-r2.log。
- nit 修复后定向重跑：schedule golden（含必备名单锁）/ TS golden / dashboard 域测。

## change review 记录

- 触发理由：新增多消费者公开契约（OpenAPI 端点）+ 跨域金额聚合语义 + DEC-9 算法权威移植（DST/舍入正确性不确定区）。
- reviewer 创建方式：宿主 subagent（Agent 工具，同步非后台派发，精简 scoped prompt）；异构回落原因：codex 未安装、claude CLI 上游模型不可用（400 模型不存在，2026-08-24 当会话复测）。round 1 reviewer identity：agent_25a503ed-fa86-4560-8bf4-407d86c92094（round 2 沿用同 reviewer follow-up）。
- 冻结目标：staged diff SHA-256 `0b9f593847ca67d050f1c28b3eb688938b9b3a4a9d66f1dece7cb660cb51d8c2`（28 文件 +3755/−18）。
- round 1（2026-08-24）：结论「可合」，blocking 0 / important 0 / nit 3；reviewer 独立重跑 schedule 包与 TS golden 全绿。三条 nit 均已修复：N1 测试失败消息 0.3333→0.6667 笔误、N2 本文档验收/状态同步、N3 双端 golden 守护加必备用例名单锁。
- round 2 终审（2026-08-24，同 reviewer follow-up）：新冻结目标 staged diff SHA-256 `f42019efb63ba56f28a4fcba9aba5c4054abc16274650a214bb1680f2b4cba0f`（28 文件 +3791/−18）。结论「可合」：round 1 三条全部 resolved、无回退，生产代码零改动；新 nit 1（本文档补 round 2 记录，即本行）已随合并闭环。reviewer 再次独立重跑 schedule/dashboard/TS golden 全绿。

## 未决

- 验收标准 3 的 parity 契约测试=golden fixtures（本轮交付）；如 owner 认为需要 Calendar 页级数值对拍（运行时双读数比对），归 ITEM-5 或二期再议。
- DEC-9 双实现并存期漂移窗口仍在（Calendar 前端仍用 TS 实现，靠 golden 兜底），切换消费服务端结果归二期。
- next_shoot 扫描上界 365 天（工程上界）；超过一年的超远期档期不进 next_shoot，量级上无实际影响。
