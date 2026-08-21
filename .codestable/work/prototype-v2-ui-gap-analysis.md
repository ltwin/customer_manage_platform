---
kind: ui-gap-analysis
epic: ../requirements/creative-shoot-planning.md
prototype_ref: docs/prototypes/creative-shoot-planning/v2/
baseline_commit: bb9589e63efd8146920df8362d55deebdcada065
analysis_date: 2026-08-20
status: open
disposition: epic-final-acceptance-input
gap_counts:
  true_gap_total: 38
  true_gap_remaining: 15
  true_gap_fixed: 21
  true_gap_partial: 2
  residual: 2
  by_design: 7
fixed_ids:
  - GAP-RUN-01
  - GAP-RUN-02
  - GAP-RUN-03
  - GAP-ING-01
  - GAP-ING-02
  - GAP-ING-03
  - GAP-ING-04
  - GAP-ING-05
  - GAP-SHARE-01
  - GAP-SHARE-02
  - RES-SHARE-01
  - GAP-WS-01
  - GAP-WS-02
  - GAP-WS-03
  - GAP-WS-04
  - GAP-WS-09
  - GAP-RUN-04
  - GAP-RUN-05
  - GAP-RUN-06
  - GAP-WS-05
  - GAP-WS-08
  - GAP-WS-06
  - GAP-WS-07
  - GAP-ING-06
partial_ids:
  - GAP-WS-04
  - GAP-WS-07
last_reconciled: 2026-08-21
reconcile_note: 2026-08-21 行级对账修正；旧计数器（true_gap:19→15 等）自初版起与表格行数存在漂移，现以行级清点为准。
---

# 拍摄策划 v2 原型 vs 实现 UI 差距分析

本文件是 `creative-shoot-planning` Epic 最终人工验收的输入之一：对照冻结原型
`docs/prototypes/creative-shoot-planning/v2/`（5 页，2026-08-05 冻结快照）与实现
（`frontend/src/planning/`），逐域列出界面层差距并定性。分析基线为分支
`feat/creative-shoot-planning` 的 `bb9589e`（2026-08-19）；文中 file:line 均指该提交。

**定性框架**（依据原型 README 的权威优先级：roadmap > OpenAPI > feature design > 原型）：

- **BY-DESIGN**：design/roadmap 有意收紧或替换了原型设计，不算缺陷，不需要修；
- **RESIDUAL**：epic 进度或 feature checklist 已记录的未收口项（如 plan-share S2/S4/S6/S7）；
- **GAP**：design/schema 已支持（或 design 明确要求）但 UI 未落地的真差距。

每条差距带稳定 ID（`GAP-{域}-{序号}`），供后续 cs-feat/cs-review 或验收引用。

## 总体结论

四个页面的数据契约与安全骨架完成度高（一次性分享密钥、幂等键、乐观锁 409、
append-only 执行历史、capture_mode 服务端判定、断网明确失败均落地，部分超出原型），
但按原型设计的**交互形态与信息密度**普遍被简化。差距集中在三类：

1. **摄取页候选编辑深度**：API schema 全支持的编辑能力（规范标签、准备项四字段、链接归属）UI 未接；
2. **Run Mode 现场交互**：备注、镜头清单、跳过强制原因等保障现场数据质量的交互缺失；
3. **工作台引导闭环**：「被拒时告诉你差什么并带你去处理」退化为显示一条错误消息。

## 一、摄取工作台（plan-ingestion）

### BY-DESIGN（有意偏离，不修）

| ID | 内容 | 依据 |
|---|---|---|
| BD-ING-01 | 原型顶部「保存到哪份策划」选择器不存在；摄取 session 进入前即绑定单一策划 | ingestion design：session 定义为「针对**一份** ShootPlan 的工作单」 |
| BD-ING-02 | 原型「创作 brief 层」候选不存在；候选仅 shot/readiness 两类 | ingestion design D8：「没有 brief candidate、AI confidence 或隐式 taxonomy」 |
| BD-ING-03 | 建案计时等内部观测正确地未显示 | 红线 H1；`planning.css:353` 注释明确不展示 |

### GAP（真差距，按影响排序）

| ID | 优先级 | 内容 | 证据 |
|---|---|---|---|
| GAP-ING-01 | 高 | 镜头候选的 5 个规范标签（取景/灯光方向/灯光质感/色调/类型）不可编辑，commit 只传 `title`/`notes`；`ShotWrite` 全字段支持 **【已修复 2026-08-20，待提交：含加性契约扩展（候选/override 携带标签、reparse 保留）】** | `ShootPlanIngestionPage.tsx:181`；`schema.d.ts:2044-2062` |
| GAP-ING-02 | 高 | 准备项候选的层级/预期负责人/是否必需/默认核对提前量界面上不可查看不可改，只能被动接受服务端解析值；design 要求「用户必须显式改成 required/负责人/提前量」（parser 默认全 optional/unassigned） **【已修复 2026-08-20，待提交：四字段可编辑，经 preview override 持久化】** | `ShootPlanIngestionPage.tsx:208-216, 304`；ingestion design §80 |
| GAP-ING-03 | 高 | 「重新解析」无确认弹窗、无「已编辑候选保留」说明；design §333 明确要求该弹窗 **【已修复 2026-08-20，待提交】** | `ShootPlanIngestionPage.tsx:106-120`；design §333 |
| GAP-ING-04 | 高（疑似 bug） | 恢复被丢弃候选时 kind 硬编码 `'shot'`，准备项类候选恢复后会变成镜头 **【已修复 2026-08-20，待提交：重复项按 winner kind 恢复，其余默认镜头】** | `ShootPlanIngestionPage.tsx:158` |
| GAP-ING-05 | 中 | 参考链接候选的归属（`target_kind`）与标注不可编辑（下拉 disabled、label 无输入）；API 支持 `reference_link_overrides` **【已修复 2026-08-20，待提交：标注输入 + 归属/归属镜头选择】** | `ShootPlanIngestionPage.tsx:228, 304`；`schema.d.ts:2816-2826` |
| GAP-ING-06 | 中 | 参考图上传来源声明硬编码 `customer_supplied/display_consent`，无来源选择 **【已修复 2026-08-21，待提交：上传入口加来源选择（8 类，共享 mediaRights 模块），声明按矩阵派生】** | `ShootPlanIngestionPage.tsx:127-129` |
| GAP-ING-07 | 中 | 丢弃原因退化为原始字符串展示，无 blank/duplicate/unsupported/手动丢弃 分类标签与对照说明；丢弃区无展开/收起 | `ShootPlanIngestionPage.tsx:304` |
| GAP-ING-08 | 低 | 保存摘要粒度简化（仅 3 个计数，无分类明细与版本号变化）；无顶栏「已选 N 条」chip | `ShootPlanIngestionPage.tsx:274, 310` |
| GAP-ING-09 | 低 | 保存前主动重取 revision 规避 409，冲突时仅 stale 横幅无差异查看 UI | `ShootPlanIngestionPage.tsx:245-252` |
| GAP-ING-10 | 低 | 「全选」快捷按钮缺失 | 原型 :171 |

注：GAP-ING-03 中「已编辑候选是否被保留」由服务端持久化 candidate snapshot 保障（design D7），
前端重新解析不携带 overrides 不构成数据丢失风险，缺的是**确认与说明**。

## 二、现场 Run Mode

### BY-DESIGN

无（断网明确失败、capture_mode 服务端判定、≥48px 触控、幂等重试均达标或超出）。

### GAP

| ID | 优先级 | 内容 | 证据 |
|---|---|---|---|
| GAP-RUN-01 | 高 | 现场备注完全缺失：无输入 UI，请求体未传 `notes`；schema `AppendShotResultInput.notes` 存在，design §186 将 notes 列入 execution event 字段 **【已修复 2026-08-20，待提交】** | `ShootPlanRunPage.tsx:83-89`；`schema.d.ts:2630` |
| GAP-RUN-02 | 高 | 跳过原因退化为页内下拉：默认值可直接提交（无强制选择），「其他」必填补充说明缺失且请求体不传 notes **【已修复 2026-08-20，待提交】** | `ShootPlanRunPage.tsx:28-36, 42, 87, 178-183` |
| GAP-RUN-03 | 高 | 镜头清单抽屉缺失：跨镜跳转只能逐镜移动；进度条为单一连续条，无分段三态（ok/skip/now）、不可点击 **【已修复 2026-08-21，待提交：进度条改为逐镜分段按钮（ok=绿/skip=橙/now=蓝/待执行=灰，coarse 指针 44px 命中区），点段或位置标签「第 N 镜 / 共 M 镜 ▾」打开镜头清单抽屉（role=dialog，逐条 已捕获/已跳过/当前/待执行 标签，点按跳转并关闭，Escape/背板/返回关闭）；底部计数「已捕获 X · 已跳过 Y」】** | `ShootPlanRunPage.tsx:144-147, 153-157` |
| GAP-RUN-04 | 中 | 主按钮不随状态变化：原型「已捕获→点击撤销 / 已跳过→改为已捕获」的一步撤销/改判，实现需「清除本镜结果」+ 重新标记两次提交 **【已修复 2026-08-21，待提交：主按钮随本镜状态三态——待执行「✓ 完成拍摄」（primary）/ 已拍摄「已拍摄 · 点击撤销」（一步清除）/ 已跳过「已跳过 · 改为已拍摄」（一步改判，supersedes 自动携带）；「清除本镜结果」保留为回到待执行的入口】** | `ShootPlanRunPage.tsx:177, 184` |
| GAP-RUN-05 | 中 | 规范标签行缺失（含「灯光方向 未填」占位语义）；空字段直接不渲染 **【已修复 2026-08-21，待提交：镜头卡头部渲染 5 个规范标签 chip（取景/灯光方向/灯光质感/色调/类型，复用 ingestionCandidates 的字段定义），空值显示「未填」弱化 chip】** | `ShootPlanRunPage.tsx:208-211` |
| GAP-RUN-06 | 中 | 收尾提示为静态横幅：无分项统计（捕获/跳过数）、无「结束本场」行动按钮 **【已修复 2026-08-21，待提交：收尾卡显示「已捕获 X · 已跳过 Y · 共 N 镜」分项统计 + 「结束本场 · 回工作台」主按钮（回工作台链接）+「再看看」收起；再次记录结果会重新展示】** | `ShootPlanRunPage.tsx:189` |
| GAP-RUN-07 | 低 | session 元信息缺「第几场/版本号/时钟」；`plan_revision` 字段有值未渲染 | `ShootPlanRunPage.tsx:141`；`schema.d.ts:2620` |
| GAP-RUN-08 | 低 | 参考素材为内嵌网格直出（可接受），但空态无「本镜未绑定参考素材」提示、无「仅比对不生成」说明；跳过原因 4 项用户语言与原型不一致（如「准备未完成」vs「准备物料缺失」） | `ShootPlanRunPage.tsx:28-36, 194-200` |

## 三、策划工作台（列表 + 详情）

### BY-DESIGN

| ID | 内容 | 依据 |
|---|---|---|
| BD-WS-01 | 执行历史从镜头卡内嵌区提升为独立第 7 tab | 结构性调整，append-only 语义保留 |
| BD-WS-02 | 列表↔详情用真实路由（`/shoot-plans` → `/shoot-plans/:id`）而非同页切换 | 实现更合理 |

### GAP

| ID | 优先级 | 内容 | 证据 |
|---|---|---|---|
| GAP-WS-01 | 高 | 「标记完成」被拒时无「去镜头表看看」引导与剩余计数说明，仅显示后端错误消息 **【已修复 2026-08-21，待提交：被拒后重取 detail 派生剩余数，引导卡带「去镜头表看看/进入 Run Mode」跳转，topbar 通用错误对引导类错误码静默】** | `ShootPlanWorkspacePage.tsx:277-279`；`presentation.ts:12-29` |
| GAP-WS-02 | 高 | 「标记已就绪」被拒时不列出具体缺失项；就绪校验面板无警示 note 与面板内按钮（按钮在 topbar） **【已修复 2026-08-21，待提交：引导卡逐项列出未核对必需项；准备项面板加警示 note；按钮按 BD-WS 先例留在 topbar】** | `ReadinessPanel.tsx:40-48`；`ShootPlanWorkspacePage.tsx:297` |
| GAP-WS-03 | 高 | 反馈「去修改该镜头」只切 tab，不定位/展开/打开编辑镜头；反馈目标显示原始 shot_id 而非「第 07 镜」 **【已修复 2026-08-21，待提交：focus=shot 经工作台传入镜头表，滚动定位+高亮+自动打开编辑弹窗；反馈条目显示「第 07 镜 · 标题」，已移除镜头回退显示「已移除的镜头」】** | `ShareCollaborationPanel.tsx:183, 207, 251`；`ShotsPanel.tsx` 无 focus 消费 |
| GAP-WS-04 | 高（API+UI 双缺） | 列表卡片缺关联订单/客户、执行时间窗、捕获统计、侧栏状态标签；列表 API 本身不返回这些字段 **【部分收口 2026-08-21，待提交：列表 API 加性扩 4 组字段（crm_summary/execution_window_summary/execution_stats/readiness_summary），迁移 0030 扩 shoot_plan_list_projection（订单取链接时快照、客户名 join 档案、捕获统计 join 执行事件、准备项标量子查询），卡片改三行结构；侧栏扩展标签（分享状态/认领待核对/草稿待确认）按方案 D2 明确不做，剩余记 GAP-WS-04 残余】** | `ShootPlansPage.tsx:88-100`；`schema.d.ts:1977-1991` |
| GAP-WS-05 | 中 | 镜头卡只外显景别+类型两标签，无「未填」占位；捕获状态无 capture_mode 区分（「已捕获 · 现场」）；无「捕获于 09:41 · 第 1 场」元信息；无「复制镜头」 **【已修复 2026-08-21，待提交：标签行改为 5 个规范标签 chip（复用 ingestionCategories 字段定义，空值「未填」）；捕获徽章带 capture_mode（已捕获 · 现场/补记）与跳过原因中文标签；元信息行显示「捕获于/跳过于 MM-DD HH:mm（· 补记）」——场次与版本号不在 current_outcome 契约内，如实不展示；新增「复制」按钮预填打开「复制为新镜头」弹窗（保存即在末尾新增）】** | `ShotsPanel.tsx:77-79, 106-111` |
| GAP-WS-06 | 中 | 素材面板用途写死 `moodboard_display`：无用途选择与「生成参考（本来源禁止）」联动、无来源×权利×用途矩阵表、素材卡无来源/权利标签 **【已修复 2026-08-21，待提交：上传表单用途三选（按矩阵禁用「生成参考」，licensed 可勾选生成授权后解锁）；来源×权利×用途对照表（details 收纳）；素材卡显示来源/权利依据/生成授权标签；矩阵镜像纯函数落 mediaRights.ts，裁决权威仍在服务端 matrix.go】** | `PlanningMediaPanel.tsx:42, 53, 66, 70-75` |
| GAP-WS-07 | 中 | 素材仅支持整案挂载与图片上传：无镜头级绑定、无纯链接素材 **【部分收口 2026-08-21，待提交：镜头级绑定落地（素材卡选镜头 → holder_kind=shot + shot_reference_display，绑定结果经 active_bindings 回显）；纯链接素材按 owner 决策（2026-08-21）明确不做，记残余——需新资产形态与上传管线/安全审查，宜单独立项】** | `PlanningMediaPanel.tsx:52-54, 64` |
| GAP-WS-08 | 中 | 准备项列表不按妆造/场地/道具器材分组；条目无认领人姓名与「现场缺失」红标联动 **【已修复 2026-08-21，待提交：按 styling/location/prop_equipment/other 分组渲染（eyebrow 组头）；拉取认领记录显示「客户认领 · 姓名」；「现场缺失」红标按当前投影派生（关联镜头 current_outcome=skipped+preparation_missing，补拍成功自动解除），标签四态 必需·现场缺失/必需·待核对/必需/可选】** | `ReadinessPanel.tsx:7, 53-72` |
| GAP-WS-09 | 中 | 分享签发 409 一律显示「页面已刷新」话术，`full_view_not_eligible` 等资格错误会被误导性覆盖；无完整档资格说明文案 **【已修复 2026-08-21，待提交：409 分流——stale/revision 类才显示「页面已刷新」，`full_view_not_eligible` 显示资格条件与 CRM 关联卡引导，其余 409 保留服务端领域文案；完整档卡片前置资格说明 note（订单状态口径与 eligibility.go 一致，含取消后失效不降级）】** | `ShareCollaborationPanel.tsx:435, 723-735` |
| GAP-WS-10 | 低 | 执行时间窗无显式「来源」三选一（slot 投影/手动/暂不设置）——投影联动拆到了 CRM 面板，语义等价但入口分散 | `BriefPanel.tsx:200-254`；`CrmLinkPanel.tsx:129, 194-207` |
| GAP-WS-11 | 低 | 经营草稿逐行表缺「依据」列与建议总价行；应用确认无金额前后对照 | `BusinessPanel.tsx:259-267, 128-133` |
| GAP-WS-12 | 低 | 撤销认领确认缺「核对提醒一并撤销」联动说明；认领条目无「现场缺失/待核对」状态标签；无「认领规则说明」note | `ShareCollaborationPanel.tsx:297` |
| GAP-WS-13 | 低 | 公开规模说明文案弱化（「签发后客户页可见/未知隐藏」「不从付费场地推断」缺失）；进度流转四步说明面板缺失 | `BriefPanel.tsx:134` |
| GAP-WS-14 | 低 | 新建策划为弹窗表单且标题/主体必填，原型为一键空白建案（客户订单均可空） | `ShootPlansPage.tsx:115-162` |

## 四、客户分享页（shared-plan）

### BY-DESIGN

| ID | 内容 | 依据 |
|---|---|---|
| BD-SHARE-01 | 凭证安全模型升级：WebCrypto 32 字节 secret + SHA-256 commitment + 一次性弹窗展示，替代原型的 `Math.random` 短码常驻展示 | share design ClaimReceipt 模型（安全性为增强） |
| BD-SHARE-02 | proposal 档类型级隔离（不渲染镜头/认领数据）；HTTP 头级 no-referrer；匿名请求 credentials omit | 架构保证为增强 |

### RESIDUAL（已记录，挂 S6/S7 收口）

| ID | 内容 | 证据 |
|---|---|---|
| RES-SHARE-01 | 客户侧「凭码自助取消认领」无 UI：`selfRevokeSharedAssignment` API 已就绪（design A19 全覆盖后端验证），前端无调用 **【UI 已修复 2026-08-20，待提交：凭码输入 + 格式预检 + 404/409 分级文案 + 签名幂等键；S6/S7 其余收口仍按 residual 跟踪】** | `share/api.ts:88-102`；checklist S6/S7 pending |
| RES-SHARE-02 | S7 要求的 v2 conformance 与 1600/1280/375/coarse/200%/keyboard/screen-reader 证据未产出 | checklist S7 pending |

### GAP

| ID | 优先级 | 内容 | 证据 |
|---|---|---|---|
| GAP-SHARE-01 | 高 | 整案反馈发送后无回显列表（客户看不到自己已提交的意见）；逐镜反馈无「你已确认/你提过…」状态标记，`SharedShotV1` 无反馈状态字段 **【已修复 2026-08-20，待提交：本次访问内本地回显（匿名端不回读他人反馈），逐镜已提意见带摘要标记】** | `SharedCommon.tsx:153, 187`；`schema.d.ts:3028-3045` |
| GAP-SHARE-02 | 中 | 认领区三条用户语言承诺缺失：「摄影师会自己核对、不会催你」「凭证码用途」「找不到凭证联系摄影师」 **【已修复 2026-08-20，待提交】** | `SharedFullSections.tsx:143-145` |
| GAP-SHARE-03 | 中 | moodboard 无 caption/用途说明（数据模型层 `SharedMoodboardItemV1` 仅 ref+checksum，API 层缺） | `schema.d.ts:3006-3009` |
| GAP-SHARE-04 | 低 | `document.title` 不随 full/proposal/expired 变化；页脚丢失「专属免登录、可能被更新或关闭」生命周期告知；锁定占位与昵称字段的原第一人称/隐私说明文案弱化 | `SharedProposalView.tsx:20-24`；`SharedCommon.tsx:115-117, 172` |

## 五、通用交互（跨页）

| ID | 优先级 | 内容 | 证据 |
|---|---|---|---|
| GAP-UX-01 | 中 | 确认对话框不统一：混用 `window.confirm`/`prompt`/自定义 dialog/inline alertdialog；原型为统一模态 | `ShootPlanWorkspacePage.tsx:285,300`、`ShotsPanel.tsx:31`、`ExecutionHistoryPanel.tsx:15-17` 等 |
| GAP-UX-02 | 中 | 无 toast 体系：操作反馈用页面级 status 与局部 message 替代 | 全局无 toast 实现 |

## 处置建议（优先级排序）

1. **GAP-RUN-01/02 + GAP-ING-04**：影响现场与摄取数据正确性/质量，且属 contract 内字段，最先修——**已完成（2026-08-20，待提交）**：Run Mode 增加逐镜现场备注（随 captured/skipped 提交 `notes`，≤1000 rune）、跳过原因强制显式选择且「其他」必须填备注；摄取恢复丢弃候选按 winner kind 继承。验证：test:shoot-planning 26/26、test:plan-ingestion 4/4、planning-prototype-v2 6/6、lint 0 error、build 通过；e2e 脚本已同步新交互，完整 e2e 需本地栈环境后补跑；
2. **GAP-ING-01/02/03/05**：design 明确要求的候选编辑能力——**已完成（2026-08-20，待提交）**：候选快照与 preview override 加性扩展（5 个规范标签 + 准备项四字段，枚举白名单 fail-closed，reparse 经 Reconcile 整体携带自动保留），镜头/准备项/参考链接的编辑 UI 落地，commit 决策携带完整 ShotWrite/ReadinessWrite，重新解析加确认弹窗（文案按 design §333）。验证：ingestion Go 包全绿、`make generate-check`、test:plan-ingestion 7/7、test:shoot-planning 26/26、planning-prototype-v2 6/6、lint 0 error、build 通过；已知限制：override 无法表达「清除已设标签/字段」（nil=未编辑，与 reference label override 既有语义一致）；
3. **RES-SHARE-01 + GAP-SHARE-01/02**：分享页闭环——**UI 部分已完成（2026-08-20，待提交）**：客户凭码自助取消认领（含承诺文案与 404/409 分级提示）、整案/逐镜反馈本次访问内回显。验证：test:plan-share 12/12（3 条新测试）、planning-prototype-v2 6/6、lint 0 error、build 通过。plan-share checklist S6/S7 的后端矩阵证据与 v2 conformance 浏览器证据仍按原 residual 跟踪，不因本 UI 修复自动关闭；
4. **GAP-WS-01/02/03/04**：引导闭环需前后端配合（列表 API 扩字段、错误码结构化），建议单独 feature——**已完成（2026-08-21，待提交）**：被拒引导卡（前端从刷新后 detail 派生缺失项，无需结构化错误码）+ 准备项警示 note + 反馈「去修改该镜头」定位/高亮/自动开编辑与「第 07 镜」标签 + 列表 API 加性扩字段与三行卡片。GAP-WS-04 的侧栏扩展标签按方案决策 D2 不做（分享状态等工作台内已可见）。验证：go store/shootplanning/httpapi 全绿（含新增 TestPlanListEnrichmentProjection 真库集成测试）、`make generate-check`、test:shoot-planning 35/35、planning-prototype-v2 6/6、lint 0 error、build 通过；迁移 down 回滚测试清单已同步补 0030；e2e 完整跑仍需本地栈；
5. **GAP-UX-01/02 与各低优先级文案项**：打磨批，可在 final acceptance 后统一处理。

修复应走 cs-feat/residual 流程而不是直接改码；涉及 API 字段新增的（GAP-WS-04、GAP-SHARE-01/03）
需先回 OpenAPI 契约。本文件不改变任何 gate/decision 状态，仅作为 owner 最终验收的对照输入。

## 六、剩余清单（2026-08-21 行级对账）

> 文中各「待提交」标记均已随四批提交入库（批 4 提交：`0f13dc9` 引导闭环 / `59b02f8` 列表增强 / `d382c15` 台账）。

38 条真差距中 21 条完整修复、GAP-WS-04 与 GAP-WS-07 部分收口，**剩余 15 条可执行**（4 中 / 11 低）+ 2 项残余（WS-04 侧栏扩展标签、WS-07 纯链接素材）：

| 域 | ID | 优先级 | 内容 |
|---|---|---|---|
| Run | GAP-RUN-07 | 低 | session 元信息缺「第几场/版本号/时钟」 |
| Run | GAP-RUN-08 | 低 | 参考素材空态提示缺失；跳过原因用户语言与原型不一致 |
| 摄取 | GAP-ING-07 | 中 | 丢弃原因无分类标签与展开/收起 |
| 摄取 | GAP-ING-08 | 低 | 保存摘要粒度简化；无「已选 N 条」chip |
| 摄取 | GAP-ING-09 | 低 | 冲突时无差异查看 UI |
| 摄取 | GAP-ING-10 | 低 | 「全选」快捷按钮缺失 |
| 工作台 | GAP-WS-04 残余 | — | 列表侧栏扩展标签（分享状态/认领待核对/草稿待确认/反馈条数），批 4 按方案 D2 明确不做 |
| 工作台 | GAP-WS-07 残余 | — | 纯链接素材（贴 URL 不传图），owner 2026-08-21 决策本批不做，宜单独立项 |
| 工作台 | GAP-WS-10 | 低 | 执行时间窗三选一入口分散 |
| 工作台 | GAP-WS-11 | 低 | 经营草稿缺「依据」列与建议总价行 |
| 工作台 | GAP-WS-12 | 低 | 撤销认领确认缺联动说明与状态标签 |
| 工作台 | GAP-WS-13 | 低 | 公开规模说明与进度流转说明弱化 |
| 工作台 | GAP-WS-14 | 低 | 新建策划为必填弹窗，非一键空白建案 |
| 分享 | GAP-SHARE-03 | 中 | moodboard 无 caption/用途说明（需回契约 `SharedMoodboardItemV1`） |
| 分享 | GAP-SHARE-04 | 低 | `document.title` 不随档位变化；页脚生命周期告知弱化 |
| 通用 | GAP-UX-01 | 中 | 确认对话框不统一（confirm/prompt/自定义混用） |
| 通用 | GAP-UX-02 | 中 | 无 toast 体系 |

**Residual（挂 plan-share checklist S6/S7）**：S6 后端验证矩阵证据（design A19）、S7 v2 conformance 浏览器证据（1600/1280/375/coarse/200%/keyboard/screen-reader）。不因 RES-SHARE-01 的 UI 修复自动关闭。

**建议处置顺序**：高优先级、WS-09 与 RUN 三件套均已清零；剩余中优先级按域打包（ING-07 摄取丢弃分类、SHARE-03 moodboard 说明、UX-01/02 打磨批）；素材域仅剩 WS-07 纯链接素材残余（owner 已决策不做）；GAP-UX-01/02 作为统一「交互打磨批」最后收敛 confirm/toast 两个体系。挂账：历批修复的 e2e 完整跑（需本地栈 + `PLANNING_E2E_EMAIL/PASSWORD`）。

## 引用

- 原型快照：`docs/prototypes/creative-shoot-planning/v2/`（README 含权威优先级与冻结声明）
- Epic 进度：`../work/epic-creative-shoot-planning.md`（原路径 `.codestable/work/epic-creative-shoot-planning.md`）
- 各 feature design：`.codestable/features/2026-08-05-*/`（legacy 冻结证据）
- 分析方法：4 个并行只读子代理逐域对比（原型页 ↔ React 实现），定性经 feature design/checklist 交叉核验
