---
kind: pm-review
epic: ../requirements/creative-shoot-planning.md
baseline_commit: bb9589e63efd8146920df8362d55deebdcada065
review_date: 2026-08-25
status: open
disposition: iteration-planning-input
dimensions:
  usability: 3/5
  feature_completeness_core: 4/5
  feature_completeness_periphery: 2/5
  workflow_design: 4/5
  collaboration_loop: 4/5
  release_readiness: 1/5
---

# 拍摄策划板块产品评审（PM Review）

## 0. 评审范围与方法

- 评审对象：拍摄策划（creative shoot planning）板块整体，含前端 5 页面（`frontend/src/planning/`）、后端三模块（`backend/internal/{shootplanning,planshare,planningmedia}` + reminder 接线）、需求与路线图文档体系。
- 评审维度：易用性、功能完善度、流程设计、协作闭环、验证与发布就绪。
- 输入材料：前端全部源码、api/openapi.yaml 能力面、`.codestable/requirements/creative-shoot-planning.md`、roadmap、epic tracker（`.codestable/work/epic-creative-shoot-planning.md`）、prototype-v2-ui-gap-analysis、各 feature acceptance/design 文档。
- 口径说明：本评审区分三类"未完成"——(a) 流程性未完成（验收文书、状态回写）；(b) 证据性未完成（真实使用证据门，是真风险）；(c) 决策性不做（范围决策，不计缺口）。

## 1. 总评

**工程纵深远超产品验证。** 数据契约层（五态状态机、乐观锁、append-only 执行事实、一次性密钥、幂等）做得接近过度设计；8/8 roadmap item 代码已在 main，38 条原型 UI 差距清零。但决定产品生死的三个真实使用指标——G1 现场真用率（≥3/5 次拍摄）、G2 建案成本（≤10 分钟）、G3 客户互动率（≥40%）——**样本为零**（本地 PG 计数：shoot_plans=4、live run session=0），发布闸门按设计 fail-closed，需求文档 status 仍为 `draft`，README 功能清单未收录本板块。

一句话：**"实现完成、验证未开始"——当前最大的风险不是缺功能，而是整套价值假设未经一次真实拍摄检验。**

| 维度 | 评分 | 要点 |
|---|---|---|
| 易用性 | ★★★☆ | 上手路径优秀；术语外溢与高认知负荷交互拖后腿 |
| 功能完善度（核心） | ★★★★ | 核心链路完整度罕见 |
| 功能完善度（外围） | ★★ | 搜索/导出/通知/模板等产品化能力整体缺失 |
| 流程设计 | ★★★★ | 状态机与信任边界克制且诚实；少数闭环链路偏长 |
| 协作闭环 | ★★★★ | 免登录分层分享是差异化亮点；回流靠轮询削弱闭环 |
| 验证与发布就绪 | ★☆ | 证据门全 pending，发布闸门关闭 |

## 2. 易用性

### 2.1 亮点

- **建案成本压到极致**：列表页「＋ 新建策划」一键空白建案、零必填、直达工作台（`frontend/src/planning/ShootPlansPage.tsx:33-44`）。直接回应立项判断"竞品是微信群+备忘录，建案成本决定生死"（`.codestable/brainstorms/creative-shoot-planning/brainstorm.md:48-52`）。
- **后置校验 + 缺口引导卡**是全场最佳交互：不在表单堆必填，在「标记已就绪/标记完成」被拒时刷新数据并精确告知缺口（"还有 N 项必需准备项未核对：XXX"）+ 直达按钮（`frontend/src/planning/transitionGuidance.ts`、`ShootPlanWorkspacePage.tsx:311-334`）。约束在流转点、引导在拒绝时。
- 空态文案普遍带行动指引；Run Mode 脱离 AppShell 全屏、触屏主按钮 64px、跳过强制结构化原因——现场单手操作场景被认真对待。
- 深链体系（`?tab=readiness`、`?focus=shot:<id>`）使仪表盘待办、提醒、反馈采纳均精确到条目级回流。

### 2.2 摩擦点（按严重度排序）

| # | 问题 | 证据 |
|---|---|---|
| U1 | 规范标签下拉直接显示英文枚举原文（`extreme_closeup`、`high_saturation`…），对中文目标用户是最刺眼的可用性硬伤 | `frontend/src/planning/panels/ShotsPanel.tsx:218-222`、`ingestionCandidates.ts:45-51` |
| U2 | 术语外溢：同一功能三个名字（按钮「从聊天整理」/页面「摄取工作台」）；「进入 Run Mode」中英混排 | `ShootPlanIngestionPage.tsx:297-305` |
| U3 | 分享到期要求手填 **UTC 绝对时间**，实现细节外露给用户换算时区 | `panels/ShareCollaborationPanel.tsx:588-601` |
| U4 | 保存心智模型分散：每卡独立保存 + 仅全局 toast，无表单级成功态；每次保存整页重拉 | `ShootPlanWorkspacePage.tsx:143-148` |
| U5 | 核心卖点路径非默认路径：建案默认空白建案，「从聊天整理」藏在工作台顶栏，列表空态不引导摄取 | `ShootPlansPage.tsx:58`、`ShootPlanWorkspacePage.tsx:219` |
| U6 | 一次性密钥弹窗强制勾选确认（安全性强但操作重）；认领项提醒藏在准备项 tab 下半屏；CRM 搜索非即时、限 8 条 | `OneTimeSecretDialog.tsx:44-49`、`CrmLinkPanel.tsx:52` |
| U7 | 列表页固定 pageSize 50、无翻页/搜索/排序，策划量上来后是瓶颈 | `ShootPlansPage.tsx:51-56` |
| U8 | 拍摄中改镜头结构必须退出 Run Mode 回工作台，现场改分镜动线长 | `ShootPlanRunPage.tsx` 全屏独立页设计 |

## 3. 功能完善度

### 3.1 已建成的核心纵深（完成度罕见）

- 五态状态机 + `expected_revision` 乐观锁 + 双轨 revision（结构 vs 执行事实，ADR-005）+ append-only 执行审计 + 每次完成冻结快照。支撑了"先 skipped 后 captured 仍记一次真实准备遗漏"这类精细业务度量。
- 摄取闭环：规则解析 → 候选逐条确认 → 单事务原子提交（"失败时不会产生半条镜头"）。
- 分层免登录分享：proposal/full 两档，full 与订单状态绑定资格门槛，订单取消后 404 不降级；客户可整案反馈、逐镜留言、认领准备项并凭 `cr1.` 凭证自撤。
- 认领 → 拍前提醒闭环：只发摄影师、档期改期/取消/归档自动重算或撤回，接站内 + Telegram digest。
- 经营草稿：复杂度事实 → 调价/档期时长草稿，应用前 stale 检测 + 审计；客户侧零价格信号（H2）。
- 媒体权利矩阵：来源×权利×用途四层服务端 fail-closed；分享页只暴露"情绪板展示"用途素材。

### 3.2 产品化外围缺失（下一阶段的真 backlog）

| # | 缺口 | 影响 |
|---|---|---|
| F1 | 列表无关键词搜索/排序/翻页 | 策划量增长后管理失效 |
| F2 | 无模板/复用：镜头表、准备项清单不可跨策划复用 | 复购型摄影师（同番同角色）高频诉求；brainstorm 以"风格统计优于模板"论证不做，但风格统计在 AI epic，过渡期无方案 |
| F3 | `/export` 不覆盖策划：shoot plans、反馈、认领、媒体均不可导出（api/openapi.yaml:4935-4941） | 私域经营者数据主权信任受损 |
| F4 | 通知被动：客户反馈/认领后摄影师只能靠 TG digest 轮询或主动开页面，无即时触达 | 协作闭环在"回流"环节慢 |
| F5 | 归档终态不可恢复；分享无访问统计（摄影师不知道客户看没看）；capture 不可挂现场照片 | 中低 |
| F6 | 客户侧不能上传文件（如自带参考图） | 低，首版可接受 |

### 3.3 决策性不做（范围纪律，非缺口）

AI 辅助、纯链接素材（WS-07）、离线可写 Run Mode、多人实时协作、客户账号/直达通知——需求文档明示排除或后置 epic（`creative-shoot-intelligence` 12 项，启动需 G4 gate）。**范围纪律是本项目优点，不计为缺陷。**

## 4. 流程设计

### 4.1 亮点

- **状态机门槛克制且业务正确**：draft→ready 只卡必需准备项；complete 要求每镜有结果（捕获或带原因跳过）；ready 下结构修改致 readiness 不完整自动降级回 draft（`backend/internal/shootplanning/state_machine.go:72-76`）——状态永远诚实。
- **信任边界干净**：协作与定价彻底解耦（H2）；full 档资格绑定订单阶段；归档确认弹窗逐条列影响。
- **创作主导权在摄影师**：反馈采纳/忽略只记录判断、绝不自动改 brief——正确立场。
- `capture_mode`（live/backfill/unknown）由服务端依时间窗判定、不允许伪装 live——为 G1 指标数据可信度负责。

### 4.2 流程问题

| # | 问题 | 说明 |
|---|---|---|
| P1 | 核心卖点非默认路径（同 U5） | 立项价值主张是"把聊天记录变成方案"，但摄取入口是 secondary，直接影响 G2 达标 |
| P2 | 反馈采纳落地链路三步以上：看反馈 → 标记采纳 → 点深链 → 手动改镜头 | 采纳量一多就累；中期可做"采纳时建议编辑"轻量联动（不破坏不自动改原则） |
| P3 | 断网只明确失败、不可写 | 外景无信号是现场常态，Run Mode 最大单点脆弱性，approval-report 已列风险 |
| P4 | 认领凭证丢失不可恢复，只能摄影师撤销重签 | owner 已接受此代价；需 FAQ/文案兜底 |

## 5. 协作闭环

免登录两档分享是板块差异化亮点：客户"不用注册也不用下载任何东西"即可看方案、留反馈、认领准备项，且有凭证自撤、失效页承诺"意见和认领都还在"等信任细节。短板在回流单向性（F4）与分享无访问统计（F5）：摄影师发出链接后处于"盲发"状态，既不知道客户看没看，也不能在客户提交内容的第一时间获知。

## 6. 验证与发布就绪（本评审最强调的一节）

- 正式 acceptance 仅 ITEM-1/2 走完；ITEM-3–8 为"implemented + 自动化验证 + 程度不一的 review 闭环"。
- **G1/G2/G3 真实证据门全部 pending**；stage-1/stage-2 evidence-go 未通过；plan-share S2/S4/S6/S7 证据 residual 开着；`test:shoot-planning:e2e` 未完整执行；production-shaped 备份恢复 rehearsal pending。
- 发布闸门按设计 fail-closed（hardening 项的设计行为，非故障）。
- brainstorm 的 kill criteria 写得很对：5 次拍摄中 live session <3 则暂停扩展重设计——**这条规则必须被执行，而不是被绕过。**

## 7. 后续迭代与优化方案

### 7.1 总体原则

1. **先证据，后迭代**：发布闸门未开、生死指标零样本之前，不启动新功能 epic。
2. **快赢与验证并行**：低成本高确定性的易用性修复不依赖证据，可在证据采集期内穿插做。
3. **每项迭代绑定可度量指标**，沿用 G1–G3 口径，新增外围指标。

### 7.2 Phase 0 — 收尾与真实验证（前置，阻塞一切新功能）

| 事项 | 负责 | 完成判据 |
|---|---|---|
| owner 最终人工验收（epic `blocked_by: owner-final-acceptance`） | owner | epic 状态推进 |
| production-shaped 备份恢复 rehearsal | owner 授权 | rehearsal 报告 |
| `test:shoot-planning:e2e` 完整跑通 | dev | CI/本地记录 |
| plan-share S2/S4/S6/S7 证据补齐（含真实浏览器/读屏/键盘矩阵） | dev | checklist 关闭 |
| **5 次真实拍摄证据采集** | owner | G1（live session ≥3/5）、G2（建案 ≤10min）、G3（互动率 ≥40%）三项处置结论写入 canonical approval |
| 按 kill criteria 处置：达标 → 开闸进入 Phase 2/3；不达标 → 暂停扩展，回到设计 | owner | 发布闸门状态变更 |

### 7.3 Phase 1 — 易用性快赢（不依赖证据，可与 Phase 0 并行）

按"修复成本/确定性"排序，均为小改动：

1. **规范标签中文化**（U1）：为 5 个 taxonomy 枚举建中文显示映射，UI 只显示中文，API 契约不动。
2. **术语统一**（U2）：「摄取工作台」改为「从聊天整理」同义词系；「Run Mode」改「现场模式」。
3. **分享到期改本地时间选择器**（U3），前端换算 UTC。
4. **列表页搜索 + 排序 + 翻页**（U7/F1）：标题/主体关键词、按更新时间排序。
5. **摄取提为建案主路径**（U5/P1）：列表页「新建策划」拆为「从聊天整理」（主按钮）+「空白建案」（次按钮）；空态文案同步引导。——此项直接服务 G2，建议在 5 次真实拍摄**之前**完成，否则 G2 测的是错误路径。
6. Run Mode 内允许查看（只读）镜头结构详情，减少退出改镜的冲动（U8 的低成本缓解）。

指标：G2 建案时长、标记已就绪/完成一次通过率、规范标签使用率。

### 7.4 Phase 2 — 协作回流与数据主权（G1–G3 达标后）

按证据缺口排序：

1. **F4 即时回流通知**：客户提交反馈/认领时，经 Telegram bot 即时推送摄影师（复用现有 digest 通道，升级为事件触发）。指标：反馈首次响应时长。
2. **F3 策划导出**：`/export` 覆盖 shoot plans（含镜头/准备项/执行历史/反馈/认领记录，媒体可选）。指标：导出功能使用率；同时是数据主权信任项。
3. **F2 复用过渡方案**：不做完整模板系统，先做"从既有策划复制镜头表/准备项清单"的轻量复制。指标：复购拍摄建案时长对比。
4. **P2 反馈采纳联动**：标记采纳后弹出"建议编辑"（预填到镜头编辑弹窗），仍由人确认。
5. 分享访问统计（仅"最近访问时间/次数"级别，不建分析后台）。

### 7.5 Phase 3 — AI epic（`creative-shoot-intelligence`，走 G4 gate）

维持路线图既定顺序（研究包 → 履约队列 → **AI 缺口检查先于一切生成** → 知识晋升 → 风格统计 → 文本提案 → 分镜）。启动前确认 G4 四条件：首版 8 项 done + G1–G3 真实证据 + owner 原子批准 + 预算/operator 权限/内容政策单独批准。风格统计落地后自然消解 F2 的模板诉求。

### 7.6 明确不做（维持范围纪律）

离线可写 Run Mode（P3 只保留"断网明确失败"语义并观察真实发生率再决策）、多人实时协作、客户账号体系、纯链接素材（WS-07 已立项挂账）。

## 8. 风险登记

| 风险 | 等级 | 说明 |
|---|---|---|
| 价值假设未验证（G1–G3 零样本） | **高** | 本板块当前最大风险；kill criteria 必须执行 |
| 外景断网导致 Run Mode 不可用 | 中 | 真实拍摄中观察发生率；若高频需回 planning update 讨论离线方案 |
| 凭证丢失摩擦（客户侧） | 低 | 已接受；文案兜底 |
| 英文枚举/术语影响专业形象 | 中 | Phase 1 修复 |
| 发布闸门长期关闭导致板块"完成但不上线" | 中 | Phase 0 需 owner 时间投入，建议排期 |

## 9. 事实来源

- 前端：`frontend/src/planning/`（5 页面 + panels + share）、`frontend/src/App.tsx`、`AppShell.tsx`
- 后端：`backend/internal/{shootplanning,planshare,planningmedia,reminder}`、`api/openapi.yaml`
- 文档：`.codestable/requirements/creative-shoot-planning.md`、`.codestable/roadmap/creative-shoot-planning/`、`.codestable/work/epic-creative-shoot-planning.md`、`.codestable/work/prototype-v2-ui-gap-analysis.md`、`.codestable/brainstorms/creative-shoot-planning/brainstorm.md`、各 feature acceptance/design、`.codestable/requirements/adrs/005-*.md`
