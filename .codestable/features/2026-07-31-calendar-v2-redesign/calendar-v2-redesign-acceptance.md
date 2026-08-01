---
doc_type: feature-acceptance
feature: 2026-07-31-calendar-v2-redesign
status: passed
audit_state: completed
audit_reason: ""
auditor_id: ""
acceptance_authorization_ref: "approval-report.md#goal-acceptance-calendar-v2"
accepted: 2026-08-01
round: 1
---

# Calendar v2 redesign 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-08-01
> 关联方案：`.codestable/features/2026-07-31-calendar-v2-redesign/calendar-v2-redesign-design.md`
> 授权：owner 以 confirmation `f127b48a-5b54-4b5c-8bae-8b3aeb6a4f58` 明确授权验收、scoped commit 与合入本地 `main`；同时要求不再追加 review，并保护主工作区现有改动。

## 1. 接口契约核对

- [x] Settings：`availability.weekly` 使用 ISO 星期 `"1"`～`"7"` 完整映射，每日为单窗口或 `null`；局部 strict JSON 解码、默认值、整体 PATCH、账号隔离和服务端响应回填均已落地。
- [x] Schedule：shoot 列表项批量装配订单价格、定金/尾款和套系拍摄类型；hold/busy 不泄漏 shoot 字段；repository 批量路径保持无 N+1。
- [x] Export：公开导出升级为 `schema_version=2`，独立 Settings 投影与 `GET /settings` 的非默认 availability 一致，既有实体数组、counts 和 reference-only 头像边界不变。
- [x] 双端生成类型由 `api/openapi.yaml` 生成；`make generate-check` 最新执行通过，没有手写重复 DTO。

## 2. 行为与决策核对

- [x] 月视图、周视图、可约空档、利用率、硬重叠、转场提醒和 DST 投影共享同一批 schedule slots，并由前端纯模型派生，不新增持久化副本。
- [x] Calendar 继续复用既有 `ScheduleSlotDialog`、journal、Idempotency-Key、历史补录、customer changed 与 unknown recovery；冲突预览使用当前范围 key/generation，迟到响应不能确认新范围。
- [x] Settings 与 slots 独立加载和失败降级；Settings 失败不会伪造可约结论，但已有档期查看与 CRUD 保持可用。
- [x] 删除完成的 URL、month/date、selection、focus、notice 与 latest-timezone range 由同一 canonical completion 协调；Settings 永不 settle 或 auth refresh 卡住时 UI 最多等待 1 秒。
- [x] 挂载点已反向核对：migration/Settings key、OpenAPI、`/settings`、schedule repository/HTTP、`/calendar`、Settings 页面和 dataexport 均有真实落点，可按 UI → API → domain/store 逆序卸载。
- [x] 范围守护成立：没有新增 openings/overview/book、拖拽、重复规则、多窗口、第三方日历、自助预约或主动消息发送。

## 3. 验收场景核对

验证来源为 `calendar-v2-redesign-qa.md` round 2 `passed`、evidence pack、DoD/gate JSON、真实浏览器 DOM/PNG/API trace，以及 REV-022/REV-023 浏览器矩阵；failed/blocked 均为 none。

| 场景 | 证据 | 结果 |
|---|---|---|
| A1–A3 Settings 默认/校验/隔离与 shoot 批量摘要 | Go unit/integration/HTTP、OpenAPI 生成检查 | 通过 |
| A4–A8 月周布局、重叠/转场、空档文案与概览 | schedule 55/55、1600/1280/375 DOM+PNG | 通过 |
| A9–A11 详情面板、可靠写流程与移动完整 CRUD | BrowserRouter/API trace、移动删除与 focus 证据 | 通过 |
| A12 DST、跨午夜与全天 | Shanghai/New York/Lord Howe 确定性测试与 DOM geometry | 通过 |
| A13–A15 状态、深链、响应式、键盘与可访问性 | generation tests、REV-022/023、三档 viewport 证据 | 通过 |
| A16 导出一致性 | repository/service/route parity tests、schema v2 | 通过 |
| A17 Settings 编辑 | settings 6/6、失败保草稿、pending freeze、成功 rehydrate | 通过 |

- [x] Review Test And QA Focus 中的写流程、取消语义、转场、空档、DST、深链、Settings、响应式与删除竞态均有运行证据。
- [x] QA residual 仅保留非阻塞项：现有 Vite 大 chunk warning、浏览器 fixture 尚未封装为正式一键 e2e、完整 ARIA row hierarchy 可后续增强；没有承载核心验收缺口。

## 4. 术语一致性

- “账号、客户、订单、套系、档期、设置”与 `requirements/CONTEXT.md` 一致；没有用“用户”混指账号或客户。
- “可约时段”属于 Settings，“可约空档”是前端派生读模型，“硬重叠”和“转场紧张”保持不同语义，没有引入平行领域实体。

## 5. 领域影响盘点

- 新的可约偏好与档期展示语义已经由批准的 `schedule-calendar` requirement delta 和 roadmap §4 承载，不需要在验收阶段另写 CONTEXT 或 ADR。
- Temporal compatible、本地单窗口与导出 schema v2 是本 feature 的公开契约；若未来增加多窗口、预约承诺或第三方日历，需要新的 requirement/design 决策，不能从当前实现静默扩张。

## 6. Requirement delta / clarification 回写

- [x] `.codestable/requirements/schedule-calendar.md` 和 `VISION.md` 已包含 owner 批准的 Calendar v2 增量；状态保持 current。
- [x] 本验收只确认已批准 delta 的实现，没有自由改写其他长期 requirement。

## 7. Roadmap 回写

- [x] design 的 `roadmap: photographer-private-crm` 与 `roadmap_item: calendar-v2-redesign` 映射完整。
- [x] items.yaml 与 roadmap 主文档中的 Calendar v2 状态已由 `in-progress` 更新为 `done`。
- [x] parent goal-state 中 Calendar v2 状态已更新为 `accepted`；`v1-hardening` 仍保持 implementing，parent `status: handoff` 与 `current_feature_index: 11` 未被本 feature 改写。

## 8. attention.md 候选盘点

- 本 feature 没有发现需要新增到 `attention.md` 的长期环境规则；worktree 隔离和提交需人工批准已是现有规则。
- QA fixture 与端口隔离属于一次性执行证据，已经清理，不沉淀为运行依赖。

## 9. 遗留

- 非阻塞：Vite production build 保留既有单 chunk 大于 500 kB warning。
- 非阻塞：本次 BrowserRouter/network fixture 以 JSON/DOM/PNG 证据归档，尚未封装成仓库内一键 e2e runner；核心协调逻辑已有可执行 Node tests。
- 非阻塞：月/周 grid 的完整 ARIA row hierarchy 可在后续可访问性专项增强；当前键盘、focus、dialog/bottom-sheet role 与移动可达性已通过。

## 10. 最终审计

- **最新完整门禁**：`make generate-check`、`make check`、`git diff --check`、`git diff --cached --check` 均 exit 0；golangci-lint 0 issues。前端专项为 schedule 55/55、settings 6/6、api-client 7/7、v1-hardening 9/9。
- **场景复核**：`re-verified 6 / trust-prior-verify 11`。REV-022/023 的删除/Settings 竞态矩阵和最终 Git 清洁度在收口阶段重新核对；其余 A1～A17 复用同一未变代码 diff 上刚完成的 QA、自动化与浏览器证据，避免无意义重复跑整日 review。
- **完整工作区**：所有 Calendar v2 tracked/untracked 路径均纳入 scoped commit；临时 mock、Vite、fixture、build output、Python cache均已清理；`backend/internal/platform/webui/dist/.gitkeep` 保留。
- **主工作区隔离**：提交和合并均在独立 worktree 完成；主工作区 `feature/userCenter` 的已有提交、未提交文件和运行服务不进入本 feature diff。
- **授权边界**：本报告消费 `approval-report.md#goal-acceptance-calendar-v2`；owner 同时授权 scoped commit 与合入本地 `main`。未授权 push、PR 或 deploy。
- **结论**：通过。15/15 checks 已更新为 `passed`，实现、QA、交付物和授权均完整，没有未处理核心缺口。
