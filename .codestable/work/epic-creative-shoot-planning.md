---
epic: ../requirements/creative-shoot-planning.md
phase: acceptance
approved_revision: 91bf0c2b47e2b9d117339803ae3254c731420db0be634d81e9dd66722a579a67
current_item: null
next_action: 等待 owner 完成或处置真实样本、production-shaped rehearsal、真实设备/生产侧证据，并对整个 Epic 做最终人工接受
blocked_by: owner-final-acceptance
item_progression: continuous
milestone_commit: authorized
remote_publish: manual
---

## 子项进度

- [x] ITEM-1 · `shoot-plan-core`
  - commit: `876768c7b877f51385698e20ed7050574883c762`
  - evidence: legacy `.codestable/features/2026-08-05-shoot-plan-core/`
- [x] ITEM-2 · `planning-reference-assets`
  - commit: `becb30ad49b62e4a972484dc08f247d6b758288d`
  - evidence: legacy `.codestable/features/2026-08-05-planning-reference-assets/`
- [x] ITEM-3 · `plan-ingestion-capture`
  - commit: `87372301d4b8f3035404bea7132d1d2fd5685357`
  - verification: `make check` passed（2026-08-12）
  - review_closure: 第 3 轮 blocking findings 已通过定向修复处理，修复后自动化验证通过；根据 owner 指令未启动第 4 轮独立 review。验证通过不等于新增 review 签署。
  - evidence: legacy `.codestable/features/2026-08-05-plan-ingestion-capture/`
- [x] ITEM-4 · `shoot-plan-crm-integration`
  - status: implemented
  - commit: `be182a6f58333fc10798505bb94858e07eec58f4`
  - owner_dispatch: 2026-08-13 授权在 stage-1 canonical JSON 缺失时先实现；不把 gate 标为 passed
  - verification: `make generate-check`；`go test -p=1 ./internal/shootplanning/... ./internal/customer ./internal/order ./internal/schedule ./internal/platform/httpapi -count=1 -parallel=1`；`npm run test:shoot-plan-crm`；`npm run lint`；`npm run build`。CMD-001 / stage-1 仍未机械通过。本地 worktree PG `schema_migrations` dirty version 16，未 force，浏览器烟马未跑。
- [x] ITEM-5 · `plan-share-collaboration`
  - status: implemented
  - verification: `make generate-check` 与 `make check` 通过（2026-08-14）
  - evidence: `.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-s8-evidence.md`
  - checklist: S1/S3/S5/S8=`done`；S2/S4/S6/S7=`pending`（cancel/delete 双连接、content↔GC 双连接、claim↔remove/archive HTTP exact、真实浏览器截图等 residual）
  - note: 按 owner 授权提交本条里程碑后进入 ITEM-6。stage-1/stage-2 仍未标 passed；未 push。
- [x] ITEM-6 · `plan-assignment-reminders`
  - status: implemented
  - verification: 父代理复核 `make generate-check`、`go test -p=1 ./cmd/server ./internal/reminder/ ./internal/reminder/digest/ -count=1 -parallel=1`、`npm run test:plan-assignment-reminders` 均 exit 0；S6 证据记录 CMD-001～006（含 `make check`）exit 0。
  - evidence: `.codestable/features/2026-08-05-plan-assignment-reminders/plan-assignment-reminders-s6-evidence.md`
  - checklist: S1–S6=`done`；真实浏览器截图 residual；stage-1/stage-2 仍未标 passed
  - note: 按 owner 授权提交本条里程碑。生产走真实 archive/CRM/timezone/freshness 与 PG lease runner，扩展既有 Telegram digest，不向客户投递。未 push。
- [x] ITEM-7 · `plan-business-feedback`
  - status: implemented
  - verification: `npm run test:shoot-planning` 24/24；`npm run test:schedule` 59/59；`npm run test:settings` 6/6；`npm run test:package-price` 4/4；`npm run build`；`npm run lint`；`make generate-check`；`go test ./internal/shootplanning/business ./internal/platform/httpapi ./internal/settings -count=1 -parallel=1`。
  - browser: synthetic 隔离账号完成经营草稿生成、确认应用、时长更新、规则过期与普通档期投影；1600/1280/375/640px（200% 等效）无水平溢出，窄屏事实表单单列可达。
  - review: 独立 change review 经 3 轮闭环，最终 `PASS / 可合`；blocking/important 为 0。
  - residual: `stage-2-evidence-go` 仍为 `pending`，真实 5 个 eligible shared shoots 的 G3 owner approval 留到 Epic 最终人工验收，不以 synthetic 浏览器证据替代。
- [x] ITEM-8 · `creative-planning-v1-hardening`
  - status: implemented
  - commit: `bb9589e63efd8146920df8362d55deebdcada065`
  - verification: `PYTHONPATH=scripts/lib python3 -m planninghardening.selftest`；`bash scripts/test-planning-hardening.sh`；`bash scripts/test-planning-ops-backup-restore-safety.sh`；`bash scripts/test-v1-ops-backup-restore-safety.sh`；planning HTTP 定向 Go tests；`go test ./cmd/planningctl -count=1`；`npm run test:shoot-planning` 24/24；`npm run test:prototype` 4/4；`npm run build`；`npm run lint`；`make generate-check`；`git diff --cached --check`。
  - review: 两轮独立 change review 后按 findings 修复 canonical approval gate、真实 schema-v2 rehearsal package、planningctl readiness/readback 跨 artifact 绑定和 stage path confinement。owner 2026-08-19 要求审查最多两轮，因此不追加第三轮；修复通过自动化与主流程自审，但不宣称新增独立 reviewer 签署。
  - residual: `stage-1-evidence-go`、`stage-2-evidence-go` 与 owner-authorized production-shaped rehearsal 仍 pending；release verifier 因此正确 fail closed，不以 synthetic fixture 替代。

## 临时决策与证据

- Legacy Goal execution approval `0cfee048-fd76-491a-925c-caa8bf9eb434` 映射为：`item_progression: continuous`、`milestone_commit: authorized`、`remote_publish: manual`。
- `stage-1-evidence-go` 与 `stage-2-evidence-go` 仍为 `pending`；fixture、设计批准、agent 判断或绿色自动化测试都不能替代真实样本证据及 owner 批准。
- 2026-08-13：owner 在会话中回复「我批准」，意图批准 `stage-1-evidence-go`。同日 runner 结果为 `needs-human` / exit 2（named decision 仍 pending；`approval_evidence.stage-1-evidence-go` 的 path/SHA-256/gate_version 为空；roadmap 下不存在 `evidence/*.json`）。口头批准未写入 `approval-report.md`，避免无绑定地把 decision 标成 approved 后变成 `failed`。
- 2026-08-13 本地 worktree PG 计数（不含 PII）：`shoot_plans=4`，`first_ready=1`，`live` run session=0。G1 要求纳入 5 份且至少 3 份 live，G2 要求 5 份 first_ready 且 active 中位数 ≤600s；当前本地库不够生成 `status=passed` 的 canonical JSON。
- 2026-08-13：owner 表示功能大致过完暂无问题，授权先跑后续开发。解释为 **implementation dispatch 延期 stage-1**，不是把 `stage-1-evidence-go` 写成 approved/passed，也不补空 SHA 绑定。ITEM-4 进入 implementing；stage-1 仍是未满足的真实使用证据，stage-2 在 ITEM-7 前继续暂停。工作区沿用已有 `.worktrees/creative-shoot-planning` / `feat/creative-shoot-planning`。
- `.codestable/roadmap/creative-shoot-planning/` 与对应 `.codestable/features/` 路径作为 legacy v1 冻结证据输入保留，不再承担活动进度的 canonical owner，也不在本次更新中改写。
- 分支基线：`feat/creative-shoot-planning` 已包含本地最新 `main`，当前三个 Epic 里程碑提交位于其上；安全 stash 保留，未应用、未删除。
- 2026-08-14：ITEM-4 implementation 收口。自动化验证通过；stage-1 仍 deferred；本地 PG dirty v16 未 force。按 owner 授权提交本条里程碑后进入 ITEM-5。
- 2026-08-14：ITEM-5 S8 全矩阵验证后按授权提交里程碑。`make generate-check` / `make check` 通过；补 `expiry_quote_stale`、archive 投影 404、IP previous-day grace、Bearer feedback allowlist golden；修 auth-legacy tip 钉 `fullVersion==24`。S2/S4/S6/S7 与若干 A 残差记入 evidence，不阻塞本条里程碑。stage-1/stage-2 未标 passed；未 push。
- 2026-08-14：ITEM-6 S1–S6 实现后按授权提交里程碑。生产接线为真实 archive/CRM/timezone/freshness 与 PG lease runner；扩展既有 Telegram digest，不向客户投递。真实浏览器截图 residual；stage-1/stage-2 未标 passed；未 push。ITEM-7 仍被 `stage-2-evidence-go` 挡住。
- 2026-08-17：恢复执行时重跑 `plan-business-feedback + stage-2-evidence-go`，runner 返回 `needs-human` / exit 2；`approval-report.md` 的 named decision 仍为 `pending`，canonical evidence path/SHA-256/gate version 均为空。ITEM-7 保持未开工，ITEM-8 因依赖 ITEM-7 亦不可调度。
- 2026-08-17：owner 指示只有必须人工验证的部分才提醒，优先功能开发，并在整个 Epic 完成后集中列出人工验收项；自动验收尽量使用浏览器模拟。该指令解释为允许 ITEM-7 implementation dispatch 延后 stage-2 真实样本验收，但不把 named decision 或 gate 伪造为 approved/passed；真实 5 个 eligible shared shoot 的 G3 evidence 留到 Epic 最终 owner gate。
- 2026-08-18：ITEM-7 实现与独立审查收口。普通档期 API payload 不携带经营字段；经营时长依据随 `SlotDraft` 保存，手动改结束时间后断开联动；移动端经营事实表单改为真实单列 grid。浏览器使用 synthetic 隔离账号验证生成、应用、过期和响应式布局；纯状态测试补足原生 date/time 输入无法由浏览器控制层触发 React 受控事件的自动验收缺口。
- 2026-08-18：ITEM-7 权威定向验证、前端 build/lint 与 `make generate-check` 通过。全量 `make check` 中 HTTP 测试曾在并发锁等待处超时，HTTP/reminder 首次独立重跑曾出现 Testcontainers `port "5432/tcp" not found`；受影响包使用 `-count=1 -parallel=1` 再次重跑均通过，符合 `.codestable/attention.md` 已记录的本机 Docker 波动，未观察到业务断言回归。
- 2026-08-19：ITEM-8 完成。planning-media schema-v2 package、三资源 replace/migrate/final oracle、rehearsal identity TOCTOU 防护、真实 HTTP optional/duplicate JSON、200% reflow 和 release evidence strict validators 均已实现并通过定向自动化。release verifier 现调用 canonical `planning-evidence-dispatch-gate.py`，只接受 owner-approved roadmap evidence binding；当前两个 stage decision 均 pending，因此公开入口必须非零退出。
- 2026-08-19：rehearsal release evidence 必须携带真实 `planning_restore_package/`，verifier 重算 package bytes 并绑定 app/postgres/restore-tool digest；`planningctl readiness` exact frame、内层 digest、trusted inventory、marker/revision 与 CAS readback 也已闭合。绝对路径和包含 `..` 的 stage evidence path 均拒绝。
- 2026-08-19：ITEM-8 独立 change review 已产生两轮终态报告。第二轮的 release fake-pass blocking 与 stage path confinement important 已完成修复和回归测试；第三轮首次派发被平台内容过滤中止且无报告，不计终态轮次。owner 随后明确要求最多两轮，不再重试；该修复没有第三方 closure 签署，作为 Epic final acceptance review 的透明输入。
- 2026-08-19：完整 `make check` 曾因已记录的本机 Testcontainers `port "5432/tcp" not found` 在 `internal/customer`、`internal/platform/httpapi` 失败；两个受影响完整包随后分别以 `-count=1 -parallel=1` 通过。不能把该次 `make check` 记为原始 exit 0。前端 lint 仅保留既有 `AccountCenterContext.tsx` 两条 Fast Refresh warning，build 仅保留既有大 chunk warning。
- 2026-08-19：fresh Epic final acceptance review 对冻结提交 `bb9589e63efd8146920df8362d55deebdcada065` 和批准契约 SHA-256 `91bf0c2b47e2b9d117339803ae3254c731420db0be634d81e9dd66722a579a67` 返回 `PASS`：0 blocking、0 important、0 nit。审查确认 ITEM-1 至 ITEM-8 的实现与自动验证可进入 owner 最终验收；不把 stage-1/stage-2 真实样本、production-shaped rehearsal、真实设备/读屏、生产发布或 owner 接受宣称为已完成。
- 2026-08-20：对照冻结原型 `docs/prototypes/creative-shoot-planning/v2/` 完成逐域 UI 差距分析，产出 `.codestable/work/prototype-v2-ui-gap-analysis.md`（38 条真差距 + 2 条 residual + 7 条 by-design，含稳定 ID 与处置优先级；【2026-08-21 对账修正：初版记录的 28+3+5 系计数漂移，以行级清点为准】）。结论：数据契约与安全骨架达标且部分超出原型；差距集中在摄取候选编辑深度、Run Mode 现场交互（备注/清单/跳过强制原因）、工作台引导闭环与列表信息密度。该文件作为 owner 最终验收的对照输入，不改变任何 gate/decision 状态；其中与 plan-share checklist S6/S7 重叠的项按原 residual 跟踪。
- 2026-08-20/21：按差距文档处置顺序分四批修复高优先级真差距（每批经 owner 批准开工与提交）。批 1（GAP-RUN-01/02 + GAP-ING-04）、批 2（GAP-ING-01/02/03/05，含候选/override 契约加性扩展）、批 3（RES-SHARE-01 UI + GAP-SHARE-01/02）已提交。批 4（2026-08-21，GAP-WS-01/02/03/04，待提交）：工作台被拒引导卡与准备项警示 note（前端从刷新后 detail 派生缺失项，不扩错误契约）、反馈「去修改该镜头」定位/高亮/自动开编辑与「第 07 镜」标签、列表 API 加性扩 4 组字段（迁移 0030 扩 `shoot_plan_list_projection`：订单链接时快照、客户名 join 档案、捕获统计 join 执行事件、准备项标量子查询）与三行卡片；GAP-WS-04 侧栏扩展标签按方案决策 D2 明确不做（记残余）。验证：go store/shootplanning/httpapi 全绿（含新增 `TestPlanListEnrichmentProjection` 真库集成测试）、`make generate-check`、test:shoot-planning 35/35、test:plan-ingestion 7/7、test:plan-share 12/12、planning-prototype-v2 6/6、lint 0 error（2 条既有 warning）、build 通过；9 处迁移回滚测试清单与 SchemaVersion 断言已同步 0030；一次 store 套件 `port "5432/tcp" not found` 复跑通过（本机 Docker 既有波动）。e2e 完整跑仍需本地栈。批 4 后差距台账（2026-08-21 行级对账修正）：38 条真差距中 12 条完整修复、GAP-WS-04 部分收口，剩余 26 条（1 高 / 13 中 / 11 低 / WS-04 残余）；2 residual；7 by-design；剩余清单与建议处置顺序见差距文档第六节。
- 2026-08-21：GAP-RUN-03（唯一剩余高优先级真差距）已修复，待提交：Run Mode 进度条改为逐镜分段按钮（ok/skip/now/待执行 四态，coarse 指针 44px 命中区，点段跳转），新增镜头清单抽屉（位置标签「第 N 镜 / 共 M 镜 ▾」打开，role=dialog + Escape/背板/返回关闭，逐条状态标签点按跳转），底部「已捕获 X · 已跳过 Y」计数；纯函数 runShotState/runOutcomeCounts 落 runState.ts（cleared 结果按待执行处理，与服务端投影口径一致）。验证：test:shoot-planning 37/37（新增 2 条）、test:plan-ingestion 7/7、test:plan-share 12/12、test:shoot-plan-crm 6/6、planning-prototype-v2 6/6、lint 0 error、build 通过。高优先级真差距清零，剩余 25 条（13 中 / 11 低 / WS-04 残余）。
- 2026-08-21：GAP-WS-09 已修复（待提交）：分享协作区 409 分流——仅 stale/revision 类冲突显示「页面已刷新」，`full_view_not_eligible` 显示完整档资格条件与「创作 brief」CRM 关联卡引导，其余 409（如 share_generation_exists）保留服务端领域文案，不再被通用话术误导性覆盖；完整档卡片前置资格说明 note（订单已定档起可签、咨询中/已取消仅概览、取消后失效不降级，口径与 planshare/eligibility.go 一致）。纯函数落 share/conflictMessages.ts。验证：test:plan-share 14/14（新增 2 条）、test:shoot-planning 37/37、lint 0 error、build 通过。台账：14 完整修复 / 24 剩余（12 中 / 11 低 / WS-04 残余）。
- 2026-08-21：GAP-RUN-04/05/06（Run Mode 现场体验三件套）已修复（待提交）：主按钮随本镜状态三态（待执行「✓ 完成拍摄」/ 已拍摄一步撤销 / 已跳过一步改判为已拍摄，supersedes 自动携带；「清除本镜结果」保留为回到待执行的入口）；镜头卡新增 5 个规范标签 chip（复用 ingestionCandidates 字段定义，空值「未填」弱化显示）；收尾卡改为分项统计（已捕获/已跳过/共 N 镜）+「结束本场 · 回工作台」+「再看看」收起（再次记录结果会重新展示）。mainShotAction 纯函数落 runState.ts。验证：test:shoot-planning 40/40（新增 3 条）、test:plan-ingestion 7/7、planning-prototype-v2 6/6、lint 0 error、build 通过。台账：17 完整修复 / 21 剩余（9 中 / 11 低 / WS-04 残余），Run Mode 域仅剩 RUN-07/08 两条低优先级。
- 2026-08-21：GAP-WS-05/08（工作台镜头卡信息密度 + 准备项分组）已修复（待提交）：镜头卡标签行改为 5 个规范标签 chip（空值「未填」），捕获徽章带 capture_mode（已捕获 · 现场/补记）与跳过原因中文标签，元信息行显示「捕获于/跳过于 MM-DD HH:mm（· 补记）」（场次与版本号不在 current_outcome 契约内，如实不展示），新增「复制」按钮预填打开「复制为新镜头」弹窗；准备项按 styling/location/prop_equipment/other 分组渲染，拉取认领记录显示「客户认领 · 姓名」，「现场缺失」红标按当前投影派生（关联镜头 skipped+preparation_missing，补拍成功自动解除），标签四态 必需·现场缺失/必需·待核对/必需/可选。纯函数落 outcomeLabels.ts。验证：test:shoot-planning 44/44（新增 4 条）、test:plan-share 14/14、test:plan-ingestion 7/7、planning-prototype-v2 6/6、lint 0 error、build 通过。台账：19 完整修复 / 19 剩余（7 中 / 11 低 / WS-04 残余）。
- 2026-08-21：素材域批次（GAP-WS-06 完整修复 + GAP-WS-07 部分收口 + GAP-ING-06 完整修复，owner 当日拍板：纯链接素材不做）：契约加性扩 `PlanAsset.rights`（当前代权利声明快照）与 `PlanAsset.active_bindings`（有效绑定集合，复用 AssetBinding schema）；迁移 0031 扩 `planning_media_asset_gallery` 视图（rights 一对一 LEFT JOIN + active bindings jsonb_agg 相关子查询，避免行倍增破坏 id 游标分页）；上传表单用途三选（按矩阵禁用「生成参考」，licensed 勾选生成授权后解锁）、来源×权利×用途对照表、素材卡来源/权利/生成授权标签与「已挂：整案情绪板 / 第 N 镜」回显、素材卡「挂到镜头」（holder_kind=shot + shot_reference_display）；摄取页上传来源选择替换硬编码 customer_supplied。矩阵镜像纯函数落 mediaRights.ts（裁决权威仍在服务端 matrix.go）。验证：planningmedia 全绿（含新增 TestGalleryProjectionReturnsRightsSnapshotAndActiveBindings 真库测试，覆盖 released 绑定不外泄）、store 迁移链全绿（9 处回滚清单同步 0031）、shootplanning/httpapi 全绿、`make generate-check`、test:planning-media 7/7（新增 3 条）、前端全部 planning 套件、lint 0 error、build 通过；期间本机 Docker daemon 一度掉线（rootless provider 报错），重启 Docker Desktop 后复测通过。台账：21 完整修复 + 2 部分收口（WS-04、WS-07）/ 15 剩余可执行（4 中 / 11 低）。
- 2026-08-21：中优先级清尾批（GAP-ING-07 + GAP-SHARE-03，待提交）：摄取丢弃区分类化——ingestionDropped.ts 纯模块镜像 parser reason 全集（blank/duplicate/unsupported/over_limit，权威在 shootplanning/ingestion/parser.go）+ 前端 manual 分类；丢弃区改 details 展开/收起（默认折叠，summary 计数），行内分类标签、原文摘录（空白折叠 + 60 字截断）、重复项「已并入保留候选」提示、五类对照说明 hint；候选列表中用户取消勾选的候选显示「手动丢弃」tag-warn 标签。moodboard 说明回契约——`SharedMoodboardItemV1` 加性扩 `caption`/`usage_note`（零迁移：caption=素材 display_name，usage_note 按权益来源在服务端 planshare 推导，措辞与前端 mediaRights 镜像对齐；两张表读取走 source.go 既有 per-binding probe 模式）；匿名分享图卡 figcaption 双行渲染（caption + 用途行）与 alt 语义化。验证：go planshare 全绿（含 TestAnonymousSharedMediaContentMatrix 扩展断言 caption/usage_note 投影）、store/shootplanning 全绿、`make generate-check`、test:plan-ingestion 12/12（新增 5 条）、test:plan-share 15/15（新增 1 条）、前端全量 233 中 232 绿（1 失败为 account-center A8 存量正则失配，stash 回 HEAD 复现确认与本 epic 无关，源于 4a11fb6 在 media query 内插入 `.main > .topbar` 规则）、lint 0 error、build 通过。台账：23 完整修复 + 2 部分收口 / 13 剩余可执行（2 中 / 11 低），中优先级仅剩 UX-01/02 打磨批。
- 2026-08-21：交互打磨批（GAP-UX-01 + GAP-UX-02，中优先级清零，待提交）：统一确认对话框——新 ConfirmDialog 组件（复用全局 overlay/dialog 样式；role=alertdialog + aria-modal/labelledby/describedby、Escape 与背板关闭（busy 守卫）、初始聚焦取消键、danger 红色确认、requiredInput 变体支持「需填写原因」且空值禁用确认、onConfirm 传 trimmed 输入）；替换全部 8 处 window/globalThis.confirm（工作台归档/重新打开、摄取重解析、经营草稿应用、清除拍摄时间、移除准备项、移除镜头）与 1 处 prompt+confirm 两连（执行历史作废 → 单对话框必填原因）；CrmLinkPanel 面板内嵌 alertdialog 为自洽范式保留不动。toast 体系——planning 页接入 AppShell 既有 #toast（useShell().notify）：工作台（已保存最新版本/策划状态已更新）与摄取页全部 7 条瞬时成功反馈改走 toast 并移除 role=status 内联块；恢复类告警（提交成功但刷新失败、版本冲突已代刷新、归档影响变化）保留内联 planning-feedback；RunPage 独立全屏路由（AppShell 之外）维持内联反馈。纯前端批次，无契约/后端改动。验证：test:shoot-planning 52/52（新增 4 条，其中 1 条旧断言 window.confirm 存在改为 ConfirmDialog）、test:plan-ingestion 9/9（重写重解析确认断言 + 新增 toast 断言）、前端全量 238 中 237 绿（1 失败为 account-center A8 存量，前批已归因）、planning-prototype-v2 6/6、prototype-contract 4/4、lint 0 error、build 通过。台账：25 完整修复 + 2 部分收口 / 剩余 11 条可执行（全部低优先级）+ 2 残余；高、中优先级真差距全部清零。
