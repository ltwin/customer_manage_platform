---
epic: ../epics/creative-workspace-redesign.md
phase: planning
approved_revision: pending
current_item: null
next_action: owner 已审阅 pm-revision-4（hash 91b486d8384de20e29454aaf63fc5c790cc05e51fea2f8aed50c5c2414e2db07）且决定不再复审；等待 owner 选择 item progression、milestone commit 与 remote publish 策略
blocked_by: owner-confirmation
item_progression: pending
milestone_commit: pending
remote_publish: pending
---

## 子项进度

- [ ] ITEM-1A · 形态原型与走查
- [ ] ITEM-1B · 跨 Epic barrier、迁移矩阵与契约回写（B1 不依赖 gate，可与 1A 并行）
- [ ] ITEM-2 · pilot 经营写入隔离与 legacy 安全预检
- [ ] ITEM-3 · 创作空间、卡片、拍摄项与 legacy-read 地基
- [ ] ITEM-4 · 批量搬入、灵感墙与图优先现场 pilot
- [ ] ITEM-5 · 连续真实项目证据与产品处置

## 临时决策与证据

- 2026-09-03 owner 以目标摄影师身份明确反馈：现有策划系统过于臃肿，因机械流程和计价绑定而不愿使用；目标应转向创意空间、未来画布/知识库/AI Agent 与可认可的现场模式。
- 2026-09-03 owner 确认该重设计优先推进，并授权先落地 Epic 文档；本授权不包含实现、commit 或远端发布。
- 旧产品事实：`.codestable/work/creative-shoot-planning-pm-review.md` 已记录“工程纵深远超产品验证”，真实 G1/G2/G3 样本仍不足；`.codestable/work/epic-creative-shoot-planning.md` 保持 acceptance / owner-final-acceptance，不在本次改写。
- 旧 v1 roadmap、feature design 和 v2 prototype 作为只读迁移输入；新稳定结论只进入本 Epic，待 owner 批准后由 ITEM-1B 回写 canonical requirement 和交叉 Epic。
- 当前工作区已有用户的未跟踪文件 `.codestable/epics/package-sku-pricing.md`、`.codestable/work/epic-package-sku-pricing.md` 与 `.workflow/`；本次不覆盖或清理。
- design review round 1（2026-09-03）：冻结 hash `7995f7d0331d21160781648ba3017176f951139e5924c9c6188742efdfe2199b`，reviewer `/root/creative_workspace_design_review`（宿主 collaboration subagent，`gpt-5.6-sol` ultra）结论“建议修改后再确认”，4 blocking / 4 important / 0 nit。处理：把 7 项完整平台路线收缩为 5 项可部署 pilot；新增 prototype 与真实项目两道具名 gate、前瞻性 eligible 分母和事件定义；在新旧两个 Epic 建双向 planner barrier 并前置停写；补全旧事实/路由迁移矩阵和不可逆单写状态机；限定一空间一项目、拍摄项来源快照与单向差异；离线写、画布、分享/CRM、全量迁移和 AI 全部移到证据门后，AI 只作 no-go/defer/replan disposition。待同 reviewer round 2 完整复审。
- design review round 2（2026-09-03）：冻结主 Epic hash `b136dd5b1debd0b1db3c7d021417630ec7c42b008a363e3acd3be8578889d0e2`、package Epic hash `8c2294cf9604f0d6bf869e92cf87366012aa0d3ed37f54fd4b279a88ef14daf8`，同 reviewer 确认 round 1 的 B1/B2/B3、I1–I4 均已解决，B4 仍有“旧对象只读会中断活跃履约”blocking，并新增原型 gate 可被 ITEM-2 绕过、现场窗口/漏登记仍可筛选两项 important。处理：选择简单的干净账号准入，不建设 legacy draining；preflight 逐类拒绝任何活跃旧计划/现场/分享互动/认领提醒/草稿，终态历史只读；ITEM-2/3/4 的 mutation 全部依赖具名 prototype go，固定 gate 字段与非 go 游标行为；enrollment 冻结业务资格、项目发现来源和版本化拍摄窗口，订单/档期/人工清单漏登核对，客户端不能提交 live，漏登计 non-use 或具名异常。待 round 3 完整复审。
- design review round 3（2026-09-03）：冻结主 Epic hash `614a50c466095e2f180221a16eb87030c8408e5b643febd2fe565aa45f2b22b6`、package Epic hash `b61d462194eec4a27aaffbbaf9895f661a3cfd5a402433e19861c2d77c44c64f`，同 reviewer 确认 round 2 blocking/important 全部 resolved，无新 finding，终局 `PASS`，可进入 owner confirmation。评审确认：范围仅保留线性灵感流、卡片、拍摄项、在线现场、经营隔离、终态 legacy 兼容与真实证据；原型非 go 时无业务 mutation 可调度；干净账号 preflight 不会截断旧履约；cohort/live 可测；package barrier 双向 fail-closed。
- pm revision 1（2026-09-03，owner 指示优化后待审阅）：新主 Epic hash `923937ad5f467490cdd89523b0945ddeb1ad49229ab7f975190fd418149ae75c`（原 `614a50c4...` 作废）。产品视角复审认为前三轮 design review 只验证了论证严密度、未验证摄影师是否愿意用，据此作七项修订：(1) 默认视图由线性灵感流改为图片优先等宽网格「灵感墙」，理由是竞品收藏夹的真实形态是图片网格，线性文本流验证的形态弱于竞品会使否定结果归因不明；(2) 批量搬入（多行粘贴按行成卡、多图拖入按张成卡）提为主路径并新增「素材搬入耗时 ≤5 分钟」指标，原 30 秒「首次价值耗时」降级为诊断指标——旧摄取工作台仍为 legacy-only，恢复的只是建案成本而非旧流程；(3) 现场使用指标新增 `live-unverified` 补报口径，修掉「断网只读 + 只认服务端在线打开」在外景场景下的系统性假阴性；(4) 砍掉拍摄项来源的字段级差异与逐字段接受，改为值拷贝 + 来源两态回跳，差异合并移入证据门后候选；(5) 新增每账号唯一的「未归类」空间，给无项目归属的日常灵感一个落点；(6) preflight 由被动拒绝改为附带逐类清场清单的主动路径，避免结构性排除最活跃账号，enrollment 要求含 owner 之外的种子摄影师，prototype gate 要求 owner + ≥2 名外部摄影师走查；(7) 原 ITEM-1 拆为 ITEM-1A（原型与走查，最前置）与 ITEM-1B（barrier/迁移矩阵/契约回写，其中只处置旧世界耦合的 package barrier 裁决 B1 不受 prototype gate 阻塞，尽早解冻计价主线）。另新增 DEC-13 预注册提前终止、DEC-14 形态先于保护层，以及界面摄影师语言与新旧入口层级的验收条款。验收标准由 16 条增至 18 条。待 owner 审阅；若认可，建议同 reviewer 就修订项做一次增量复审而非全量重审。
- pm revision 2（2026-09-03，owner 追加两条产品建议后修订）：新主 Epic hash `6ef7e9816db2cdb117592bf335d294d2abf46ca356d2ff22ba7ddd39acc196b7`（pm-revision-1 的 `923937ad...` 作废）。owner 提出两点：(a) 应与 CRM 建立弱关联，使空间和订单可互相跳转、空间归属可辨识，避免策划案与订单变多后搞错；(b) 旧准备项的思想值得保留，但应降级为摄影师私有的拍摄备忘，不增加负担。两条均采纳并落为 DEC-15 / DEC-16 与验收 19 / 20。关联：可选、至多一个订单**或**一个客户、可换可解除，双向跳转，未命名空间显示名可回落为关联对象名（补上"零必填导致零可辨识性"的洞）；实现明确不复用旧 `shootplanning/crm` 引擎（link epoch、connection/projection revision、事件溯源、档期窗口派生、订单生命周期级联、reminder 驱动），只做单值可空外键，对经营侧零写、不进 pricing fingerprint、不解锁策划计价；迁移矩阵新增该边界行。备忘：一行一条、批量粘贴、可打勾、现场模式可拉出；显式禁止 required、截止时间、负责人、提醒引擎、客户认领与完成度展示，旧 ReadinessItem 的四项语义一律不迁移——旧准备项令人抗拒的正是它当了状态机守卫。两者都排除在 owner 核心轻主链之外，只作诊断指标（关联使用、备忘使用）报告原始分子分母、不设阈值、不参与 `go` 判定，避免摊薄"形态是否值得打开"这个核心假设。prototype 走查任务由 2 条增至 3 条（新增整理与备忘）。新增遗留风险 10-12：弱关联滑回经营耦合的引力、备忘滑回准备项的引力、两者挤压主链工期时优先保主链。验收标准 18 → 20 条。待 owner 审阅。
- pm revision 3（2026-09-03，owner 提出为未来 AI+画布节点与垂类知识库预留扩展性后修订）：新主 Epic hash `03c641231fb967fa1242a0a0571b257009c98acd9d0ac723013b68ed7ca4a4cf`（pm-revision-2 的 `6ef7e981...` 作废）。owner 明确远期愿景：类 TapNow 的画布节点（图片/视频/文本自由组合）+ AI 一键产出策划案与分镜图 + 内置与账号私有知识库，用于解决通用 LLM 对冷门番剧/游戏等垂类理解不足的问题，并要求现在设计时即考虑扩展性。采纳方式是把"扩展性"拆成单向门与双向门两类，落为 DEC-17 与验收 21：**现在只做三项不可逆留门且都不提供功能**（合计不足一天）——(a) 图片卡片记录素材来源分类、默认最严格，复用既有 `planningmedia` 来源×权利×用途矩阵与 fail-closed 的 `generation_reference` grant（该矩阵旧实现已完成，是生成用途最贵的前置条件，新建卡片路径不得绕开）；(b) 批量搬入持久化原始整段文本与批次 ID、卡片保留批次内序号（一段聊天拆成几十张卡后上下文会永久丢失，而这是 AI 生成策划最有价值的输入）；(c) 卡片顺序与分组作为布局信息与卡片内容分离存放、卡片行不含坐标（将来画布只需新增边表，不改卡片写路径）。**画布渲染、节点连线、自由坐标、model provider、检索与知识库表一律不建**：卡片加分组已是退化的图，画布是新增而非重构，现在做与以后做成本相同；也不建通用 block 抽象或插件架构。owner 确认先导版**暂不支持视频导入**，卡片类型限定文字/图片/链接三类，不做转码/抽帧/时长/播放器，视频列为画布阶段候选节点、与画布一并评估。另修掉一个会误判产品死刑的口径问题：AI+画布主张"不必自己整理"与本轮验证"是否愿意自己整理"方向相反，同一批数据可支持相反结论。因此在 pilot gate 增加窗口开始前冻结的两种失败形态解读表——"素材不堆积"支持 reshape/stop；"素材堆积但策展不发生"（搬入正常、放弃率达标，但再次打开/清单采用低）不得判 stop，改记具名 disposition `manual-curation-too-costly`，作为 AI/画布方向的直接证据输入；复用意愿回访固定追加一问"如果有个功能能自动把这些素材整理成策划，你会用吗"，只作后继立项输入、不参与本轮 go 判定。新增遗留风险 12（愿景与本轮主张方向相反）、13（三项留门不得被当成实现许可，先导 UI 不得出现任何 AI/生成/画布入口或占位按钮）。验收标准 20 → 21 条。待 owner 审阅。
- pm revision 4（2026-09-04，owner 确立创作系统四支柱定位后修订）：新主 Epic hash `91b486d8384de20e29454aaf63fc5c790cc05e51fea2f8aed50c5c2414e2db07`（pm-revision-3 的 `03c64123...` 作废）。owner 明确远期形态为「资源库 + 知识库 + Agent 助手 + 画布节点」共同构成的创作系统，灵感墙即资源库的第一形态，知识库在存储形状上与资源库同构。采纳为「起点」第 3 条修订方向：pilot 验证的是创作系统的资源层，“素材堆积但策展不发生”是资源层已成、创作层待建的正常状态；四支柱建设顺序为资源库 → 知识库 + Agent（输出先落成候选卡片/候选拍摄项，由现有灵感墙与拍摄清单承接）→ 画布，画布不在 Agent 之前立项。由此发现 epic 一处与该定位冲突的数据形状：若卡片行以 `workspace_id` 作为所有权，将来改成账号级资源库要迁移全部读写路径，符合 DEC-17「以后补贵」判据。处理：新增 DEC-17 (d) 卡片由账号持有、空间归属与顺序/分组同放成员关系表（`WorkspaceCardMembership`），卡片行不含空间所有权字段，先导版约束一张卡同一时间只有一条成员记录、归档空间不删卡；留门由三项增为四项，同步修订先导范围、证据门后候选、非目标、共享语言、DEC-3、验收 21、ITEM-3 与交付索引。范围不变：跨空间浏览/检索/一卡多空间引用仍在证据门后；新增非目标“不建知识库、不建通用资产抽象合表，只共享权利矩阵与媒体身份”。另修正一处事实：`planningmedia` 可复用的是矩阵判定与媒体身份，其上传/绑定路径以旧 ShootPlan 为 holder（要求 `plan_id` + `expected_plan_revision`），ITEM-3 需新增 holder 类型或独立资产生命周期，此前文档把它写成“已完成的前置条件”低估了工作量，已写入 DEC-17 与 ITEM-3 可交付。新增遗留风险 18（资源库定位有做成内容平台的引力）、19（planningmedia 与旧 holder 耦合被低估）。四支柱愿景本身不写入 epic 范围，已另落 `.codestable/brainstorms/creative-system/brainstorm.md` 并加入本 Epic 的事实与历史输入。owner 决定（2026-09-04）：接受 pm-revision-4，不再对 revision 1–4 做 design review 增量复审。
