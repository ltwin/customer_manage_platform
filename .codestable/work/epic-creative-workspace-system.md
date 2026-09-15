---
epic: ../epics/creative-workspace-system.md
phase: executing
approved_revision: 2b79fb8c8111fd9dea326ca33923ba27af293c3231b60b373c4a055fd64bebab
current_item: FND-07
next_action: S0（对象端口上提）与S1（迁移0045 + creativeskill领域）已实现并通过make check-go，待owner同意后提交（milestone_commit: manual）；随后做S2受信导入CLI与reference-direction@1种子导入、S3目录与输入契约，再接里程碑B
blocked_by: null
item_progression: per-item
milestone_commit: manual
remote_publish: manual
---

## 历史候选子项进度（未获批，不作为当前执行队列）

- [ ] ITEM-1
- [ ] ITEM-2
- [ ] ITEM-3
- [ ] ITEM-4
- [ ] ITEM-5
- [ ] ITEM-6
- [ ] ITEM-7
- [ ] ITEM-8
- [ ] ITEM-9
- [ ] ITEM-10

## 临时决策与证据

- 2026-09-07 owner 提出最终形态为画布 + 资产库 + Agent，交付分镜表、场地选择/设计指导、布光、风格、道具、后期等产物；认可 Pinterest 瀑布流 + 搜索作为资产层参考，要求完整迭代规划。本轮仅起草规划，不开始产品实现。
- 文档在已有 `feat/creative-workspace-redesign` 工作区起草；HEAD `c71a69e`。原有未跟踪 `docs/prototypes/creative-workspace/` 保留。本草案未改旧批准 Epic，其 SHA-256 仍为 `ac6b32090a67fff970213fffb135c8bd2ea9f87eacfc27b267a4dd14bd017159`。
- 参考来源：先导 Epic/work、creative-system brainstorm、creative-shoot-intelligence 历史 roadmap、creative-shoot-planning-pm-review、当前实现与开发文档。历史 v1 目录只读；没有新建或改写 lesson。
- 图谱调查采用 Verify 的有界范围：project `Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign`，generation `2026-09-04T15:47:38Z`；creativeworkspace 符号查询 total=0/has_more=false；coverage 对 model/repository/service/0036/WorkspacePage 报 `not_tracked`，故读取这些路径的模型、迁移、成员关系、媒体与来源回跳相关源码。matrix.go 为 metadata_match 并直接核对用途枚举。没有对调用链或全仓覆盖作完备断言。
- 核实影响路线的事实：一卡一空间仍写入模型/数据库；媒体资产保留 workspace_id，AssetsInWorkspace 校验上传空间；fillShootRefs 仍按单个成员关系确定回跳；现有拍摄项是独立快照。多空间复用需完整子项，不能只更换视图。
- 既有工作游标的 continuous/authorized/manual 属于先导 Epic；本 Epic 暂不继承批准 hash 或执行授权。建议新 Epic 批准时采用 continuous / authorized / manual，最终以 owner 明确选择为准。
- 更精确的媒体基线：`OpenDisplay` 已按账号 + asset ID + checksum 读取，不强制原上传空间；需要演进的是账号级入口、上传绑定及返回契约，不将读取路径记成已确认的越权或空间限制缺陷。
- 设计审查采用宿主 `multi_agent_v1` fresh reviewer Hilbert（ID `01a07b2a-cf04-75c0-a01e-3ef96bb82ef5`），按 cs-review 只读执行。未发现可调用的异构 provider，Paseo 偏好文件不存在；遵循宿主工具约束继承主模型而不指定 override。单一设计审查阶段共两轮，未派生其他 reviewer。
- Round 1：目标 hash `404eb800c781389b7abdc25cf8cb3f602b8daba5ce7a6f2175c1e35c9bb8c670`，无 blocking、2 important。修复：明确普通素材归档不改变项目冻结展示、当前用途撤销优先；将项目内所选参考的分组/顺序建议和采纳完整归入 ITEM-7，补齐版本、幂等、撤销与作用范围验收。
- Round 2：目标 hash `09975fca014734ddadcc38df48e11baaf33ba5f2009e4bbfe372abb84d758c75`，同 reviewer 检查完整候选与增量，两个问题 resolved，无新 blocking/important，结论通过 design review。此 hash 是审查版本，不是批准版本。
- 文档验证：10 个稳定子项 ID 与游标一一对应；11 条验收均有责任子项；16 个已有相对路径存在；无行尾空白；旧 Epic hash 未变。仅文档改动，未运行应用测试；未修改产品代码、旧原型、旧冻结契约，未提交或发布本草案。
- 2026-09-07 owner 进一步提出未来资产市场与社区，明确要求落头脑风暴、初版 PRD，并让现有设计考虑后续演进；不要求把市场纳入本期。新增 `../../docs/product/creative-asset-platform/brainstorm.md` 与 `../requirements/creative-asset-platform.md`（draft v0.1），VISION 仅增未来候选索引；旧 brainstorm 目录保持只读。
- 当前 proposed Epic 随之新增 EV-1–6、AC-12，并就近补充 ITEM-2/4/7/10；阶段和 10 个子项依赖不变。仅追加现有元信息、用途与私有边界的演进要求，不建发布、权益、公共查询、关注、支付或市场骨架。之前 `09975f...` design review 只覆盖当时版本；本轮为新的 contract review 阶段。
- 平台契约审查 round 1：fresh reviewer Epicurus（宿主 multi_agent_v1，ID `01a07b43-5262-7122-ab1d-38eb96083589`，遵守工具约束继承主模型），按 cs-review 只读检查四份完整文档；结论通过，无 blocking/important，无需修订，未派生其他 agent。
- 本轮冻结目标：当前 Epic `f0ff54c8eb8df0432e47b3459478f223726cc0a7c08c75d0bbea35ce4ec15813`；未来 PRD `4eb447ef68ea665ee84f2648b75d9849249f8f34d14bffebb1bb17e7a259c234`；头脑风暴 `24cb1895b28dd2c41845ff40acaed49dbc218ce5cf6f268dd105fccd4ddb2999`；VISION `18902df58253202ebcc0eeb9a9de3cd2c3f9076cd978c5b5b20bb48fdbd4ae55`。这是已审查草案，approved_revision 仍 pending。
- 本轮证据范围：直接核对 model.go 的载体/账号语义及 planningmedia/matrix.go 三个既有用途；图谱 generation 仍 `2026-09-04T15:47:38Z`，查询 total=0/has_more=false，coverage 为 model not_tracked / matrix metadata_match，已直接读原文，不作缺失实现的全仓否定。外部参考来自 Pinterest/Gumroad/Adobe Stock 官方说明，链接与访问日期见两份新文档。
- 本轮文档验证通过：10 ITEM 与游标映射、12 AC / 6 EV / 13 AP 唯一编号、相对链接、行尾空白；旧先导 Epic hash 未变。本轮只改文档，未实现产品、未运行应用测试、未提交或发布。
- 2026-09-07 owner 明确要求先实现设计原型：自适应长宽比资源卡片、懒加载 + 空白卡预占位瀑布流、液态玻璃与优雅艺术气质。按该范围交付 `../../docs/prototypes/creative-workspace/v4/`，保留 v3。只覆盖 M1 核心走查：素材/集合/项目/心选、搜索筛选、图片/文字/链接导入、笔记、多处引用；数据仅在独立浏览器存储，不接生产业务。
- v4 采用原始尺寸占位 + 最短列布局 + IntersectionObserver 临近加载，宽度变化重排，图片解码和失败重试不重排；玻璃样式只包围操作界面，照片不加滤镜。新增独立原型的 DESIGN/UX-CONTRACT，并在根 DESIGN.md 标注作用域。
- v4 验证：几何/资源检查通过；浏览器 12 组检查通过、pageErrors=[]；图片加载前后卡片 bounding rect 不变；桌面和 375px/320px、错误重试、IME、刷新持久化、原子引用、存储冲突及离线本地操作有证据。详情图片固有高度溢出经截图发现并修复，新增边界断言后回归通过。Design.md lint 0 errors / 0 warnings。premium 静态扫描 29 项为分离事件绑定与全局 textarea CSS 的检测局限，未宣称 strict 通过；详见 v4/QA.md。
- ITEM-1 未标完成：当前是 owner 指定的 M1 原型切片，后续完整信息架构/Agent/画布样例及外部走查仍未完成；不把这次原型授权扩为完整 Epic 开发、提交或发布授权。
- 2026-09-07 owner 确认并要求修改：心选改称「我的最爱」，作为集合页固定、不可删除的系统集合；普通集合可新建/重命名/删除及移入移出素材；未归类只按普通集合关系推导，最爱和项目引用不影响归类。已同步当前 proposed Epic 的术语/入口/数据规则 3/ITEM-2 与未来 PRD 的私有整理边界。旧已批准 pilot Epic 未修改；此前完整文档审查 hash 是历史版本，不声称覆盖本次局部语义修订，approved_revision 仍 pending。
- 原型 v4 已实现三项主导航、系统集合/自动视图、普通集合管理与归类；保留原 favorites 数据及 view=saved 深链，不清空浏览器数据。`collections.mjs` 独立处理系统守卫与关系变更。删除只影响普通集合本身和其成员，所有素材、最爱、其他集合及项目保留。
- 本次验证：check.mjs 集合语义与几何检查通过；collections.qa.mjs 5 组新增浏览器检查通过；原 qa.mjs 12 组主链通过，pageErrors=[]。最新静态扫描 35 项仍是独立事件绑定/全局 CSS 的检测局限，浏览器检查 81 个按钮均有实际事件/委托/提交路径；详见 v4/QA.md。未接生产接口、未提交或发布本次改动。
- 2026-09-07 owner 指出项目与集合形态相似，并同意先展示完整项目原型，即便本期功能未全部开发，也可用“暂未开发”屏蔽。已据此补全项目工作台，并在 proposed Epic 中记录原型可领先实现的例外；不提前批准任何后期业务实现或修改旧 pilot 冻结契约。
- 项目原型：列表有创作摘要/参考/分镜数量；内页有概览、参考、六类策划、画布布局预览、现场本地演示。意图/场地/时间/器材可编辑；项目参考说明独立于素材 note；分镜使用参考快照，可手写/编辑；现场完成/跳过/撤销与策划共用内容，道具核对保持展开及焦点。新建项目保持空白，具名示例项目使用预先手写内容。项目导入进入全库及项目，仍按普通集合关系判未归类。
- 尚未接入的 AI、PDF/专业布光图、CRM 关联和画布拖拽/连线/布局保存均有可见说明或禁用入口；所有可操作演示仅存浏览器 IndexedDB，不产生业务请求或真实拍摄执行事实。项目实现位于 v4/projects.mjs 与 project.css，通过 studio.js 的统一存储/弹窗/导航接入。
- 验证：projects.qa.mjs 9 组检查通过、pageErrors=[]、apiRequests=[]；原素材 12 组和集合 5 组回归通过；check.mjs 加入快照与空白项目检查并通过。新代码未改变生产前后端，未运行生产全量测试。premium 静态扫描 37 项为此前解释的检测局限，未声称 strict 通过。截图与报告保存在 v4/evidence/project-*，详情见 QA.md。ITEM-1 的真人走查、原型确认和其余承接任务仍未标完成。
- 2026-09-07 owner 明确要求提交当前工作区所有改动，本次快照包含上述规划/PRD/索引和原有未跟踪的 v3 原型。原型 JavaScript 自检与 walk.mjs 语法检查通过；这不代表真人走查或 pilot 证据通过。本次授权为提交当前快照，不改变 proposed / planning 状态，不开启产品实现或远端发布。

- 2026-09-07 owner 反馈分镜无移除、手写/编辑无配图操作。v4 已补具名确认移除（保留原素材/项目引用/历史快照）、素材库选图/搜索、本地上传/替换/清空图片；图片和文字以草稿一起保存，取消不导入，保存失败保留输入。专项浏览器 9 组、项目 9 组、素材 12 组、集合 5 组及模型/几何检查通过；桌面/手机截图已实看，见 v4/QA.md。范围仍为本地原型，不接生产接口、不自动提交、不改变 Epic 审批状态。

- 2026-09-07 owner 指出混合图片/文字/链接收集易误认为图片描述。v4 改为按载体类型独立表单：图片逐张标题/描述/标签/来源，文字整篇一份，链接独立网址/说明；切换及拖图保护未保存草稿，图片说明不另建文本，保存动作固定底部。当前 proposed Epic 的收集规则/ITEM-3 与原型文档已同步；旧 pilot 冻结契约未变。collector.mjs 复用共享弹窗与原子存储。专项 10 组、素材 12 组、项目 9 组、集合 5 组、分镜 9 组及模型/几何检查通过，截图已实看，详见 v4/QA.md；仅本地原型，未提交/发布。

- 2026-09-07 owner 要求素材删除和回收站、可设置保留时长、手动恢复/彻底删除。v4 已加入详情/批量移入、素材库回收站入口、单个/批量恢复与彻底删除、清空，默认 30 天可选 7/90 天或不自动清理；缩短期限显示立即到期数量并确认，设置与清理原子提交。恢复仅恢复仍存在的原关系；分镜快照/执行记录独立保留。原型在打开/使用页面时清理，无关闭页面期间的后台任务。当前 proposed Epic 规则 4/ITEM-2 与原型文档同步，旧 pilot 未改。模型与回收站 7 组、素材12/项目9/分镜9/集合5/收集10组回归通过，截图已实看，见 v4/QA.md。本轮仍未提交、未发布或接入生产。

- 2026-09-07 owner 要求卡片外右下角菜单、详情回收站改垃圾桶图标并入底部操作栏。v4 新增共享 card-menu.mjs，复用查看/最爱/集合/项目/回收操作，浮层不改瀑布流几何并适配键盘和手机边界；详情四按钮同排。专项7组、素材12组、回收站7组及模型/几何检查通过，桌面/手机截图已实看。仅原型UI，未改生产或自动提交。

- 2026-09-07 owner 要求参考 Eagle 增加标签管理/分组/颜色/收集时选或新建/筛选；随后明确标签选择不要二级弹窗，选择时应即时反馈、结果实时变化，当前不考虑后端缓存。v4 tags.mjs 提供固定 ID 与旧标签兼容迁移，tag-ui.mjs 提供管理和就地选择；筛选实时更新芯片/数量/卡片，创建及就地新标签写入素材草稿，详情选择即时保存。保留分组删除/回收站恢复/改名不破坏引用规则，未实现缓存或生产 API。标签模型、标签11组、即时选择6组与既有回归通过，桌面/手机截图已实看，详见 v4/QA.md。同步 proposed Epic 和原型说明；旧 pilot 未改，未提交或发布。

- 2026-09-07 owner 指出固定题材分类无创建/选择能力、与标签重复，要求以真实素材类型筛选并优化协调性。v4 移除固定分类，单工具栏整合类型/标签/排序/批量/显示，管理标签和回收站位于数量栏，选中标签才占条件行；视频显式暂未开放。新 mediaType 按 kind 匹配，旧文字/链接 category 兼容，旧题材不再过滤；更细素材分组留待后续。工具栏5组、素材12/即时标签6/收集10/项目9及几何模型检查通过；821px/375px截图已实看，详见 QA.md。同步 proposed Epic 与原型设计说明；未改旧pilot、未提交或发布。

- 2026-09-07 owner 反馈类型选择残留方框且原生下拉风格割裂。v4 以 glass-select.mjs 统一类型/排序自绘菜单，原 select 只保留隐藏值/事件契约；指针选择无轮廓残留，键盘保留圆角焦点与标准导航，视频禁用有可见说明。专项6组、工具栏5组、素材12组通过，821px/手机截图已实看。仅原型UI，未提交或发布。

- 2026-09-07 owner 要求自建集合外层快捷菜单，排除我的最爱/未归类。v4 在普通集合卡片加三点菜单，复用打开/重命名/删除处理与共同菜单；重命名留在列表，删除及时刷新并保留素材和其他引用。避免嵌套按钮，修正同位置延迟滚动事件导致菜单闪退。专项6组、集合5组、素材菜单7组通过，821px/手机截图已实看；未提交/发布。

- 2026-09-07 owner 进一步明确长期形态以 LibTV 式画布节点为主体，资产库/Agent 辅助，策划产物作为特殊节点连接现场，并通过 Skill 扩展创作、资产整理和 CRM 能力；本次要求思考发散并记录头脑风暴。新增 `../../docs/product/creative-canvas-system/brainstorm.md`：区分资产/项目内容/节点实例、创作关系/任务输入、策划多视图/现场版本、Skill/工具/节点扩展；给出两个贯穿场景、候选阶段与验证实验。新路线建议提前验证最小画布，不直接沿用旧 M0–M4 顺序与工期。
- 同步候选 Epic 顶部方向提示、资产平台文档承接链接和 VISION 探索索引；本次没有重排 ITEM 或扩大已批准范围，approved_revision 仍 pending，旧先导 Epic 未改。参考 LibTV 官网与 libtv-labs/libtv-skills 的公开资料，具体产品契约均明确为本项目建议，未进行登录功能走查。本次只改讨论文档，未修改原型/产品代码，未提交或发布。
- 本次文档检查：6 份文档的 28 个相对链接有效、代码围栏成对、无行尾空白；旧先导 Epic SHA-256 未变，planning / pending 状态保持。图谱 generation 仍为 `2026-09-04T15:47:38Z`，这些文档覆盖为 not_tracked 或 metadata_changed，直接读取 Markdown 核对，未作源码结构或实现完备性判断；纯文档变更不运行应用测试。

- 2026-09-08 owner 澄清通用画布底座与摄影业务扩展：策划 Skill 产出独立策划业务文档，特殊节点只呈现摘要并连接策划交付/现场，镜头表不强关联画布图片节点。随后明确否定“项目可选关联客户”的建议：项目不关联任何 CRM 业务，Skill 可选引用订单信息作为本次输入，产出后可选发布到订单（默认本次来源订单，可另选），之后自行选择现场、继续画布或手工编辑。上述建议已替换到头脑风暴 v0.2 与候选 Epic 的相应表述，不把被否定的客户关联保留为新方向。
- 补充的版本方案仍标为建议：工作稿、订单发布版本、现场采用版本分开；发布不等于公开、发送客户或更改订单/档期状态。资产平台 PRD 同步输入隐私与公开发布边界。仅文档变更；历史关联迁移和新版子项仍待详细设计，旧冻结 Epic 与现有原型/产品未改，未提交。

- 2026-09-08 owner 明确连线是参考图/视频/文本等输入关系的直观表达，具体角色由目标节点及模式解释，例如视频首尾帧与全能参考；风格参考用图片/视频节点内的选项，从资产库或画布节点选择，底层仍是参考媒体加专门用途提示。已记入头脑风暴 v0.3 §3，替换先前自由关系/任务输入两类连线的提案，并同步候选 Epic 的局部表述。按要求只记录方向，端口、模式切换、提示词与模型适配等留待详细设计；未改代码或原型，未提交。

- 2026-09-08 owner 提出知识库采用向量检索 RAG，包含图片、文本，视频可能后续支持，并以 Gemini 为能力参考。已补入头脑风暴 v0.4 §7：原始资料/可重建索引/任务应用分层、图文与片段定位、精确条件结合向量、检索证据与生成参考区别、图文先行及视频验证候选。通过 Context7 与 Google 官方文档核对：Gemini Embedding 2 支持多模态向量化，File Search 当前不支持音视频，不能将理解/向量化等同于托管检索能力；未进行模型实测或确定选型。候选 Epic 与平台 PRD 仅追加方向承接，未扩大实施范围、未改代码或原型、未提交。

- 2026-09-08 owner 授权制作暗色液态玻璃画布原型：左工具/资产、中画布、右 Agent，个人与公共市场放在资产侧栏。开发中纠正“拍摄策划不是侧栏子页面”，已改为节点最大化/还原状态，移除独立策划导航，文档仍可从新建节点菜单重新引用。
- v5 实现节点新建、拖拽、连接、缩放、撤销/重做、资产入画布、策划最大化、分镜/章节编辑、演示订单发布与跳转、固定版本现场演示及 Agent 示例工作流。资产能力复用 v4 同源嵌入变体，使用独立数据库；未改生产前后端与旧冻结 Epic。
- Chrome 已走通类型/标签实时筛选、市场收藏、软删除恢复、分镜换图、发布目标默认、现场进度刷新、版本分离和文档重新引用。发现并修复侧栏导入弹窗固定宽度溢出；视口状态改为标签页记忆，不争用内容修订。具体结果与限制见 ../../docs/prototypes/creative-workspace/v5/QA.md。
- 本轮只交付原型，不批准旧 ITEM 顺序与工期，不提交或发布代码。

### v5 交互反馈修订（2026-09-08）

按 owner 反馈实现左键框选、中键/空格平移、选区上方跟随工具条、自动节点引用、独立图片附件及 LLM 演示菜单。交互边界与验证结果见 v5 UX-CONTRACT.md / QA.md；仍为设计原型，未接入真实模型、未提交。


### 基础框架需求设计（2026-09-09）

- 摄影师明确停止 UI 细节打磨，要求依次推进需求设计、系统架构设计、模块设计、开发、验收。本轮恢复同一 proposed Epic，新增 canonical 需求 `../requirements/creative-canvas-foundation.md`，不新建第二份 Epic 或路线文件。
- 通过本轮文字回复确认首期媒体范围：图片、文字、视频、音频导入、基础信息及引用；Agent 真实对话、检索和画布操作；图片/视频生成与 3D 后续扩展。其余首期细分建议仍以需求草案标注为准。
- 当前阶段为需求设计。Epic 添加当前周期和出口，原 M0–M4 / ITEM-1–10 / AC 与估算整体标为历史候选；历史审查结论不覆盖新草案，approved_revision 仍 pending。未要求摄影师重选已有工作区。
- 已知对象边界取自本会话确认及 brainstorm/v5 文档；图谱 Verify 项目与 generation 仍为 `2026-09-04T15:47:38Z`，相关 Markdown 为 not_tracked / metadata_changed，新需求为 missing，已直接读取文档。旧实现事实只作历史调查输入，未宣称已重新完成生产源码审计。
- 下一阶段应在确定的工程基线上核实资产/媒体/空间现状，围绕需求研究架构与关键风险，不提前写数据库或供应商绑定代码。只改规划文档，未提交或发布。
- 文档验证：5 份文档的 33 个相对链接有效，22 条功能需求与 9 个贯穿场景编号完整且唯一，代码围栏及空白检查通过，旧先导 Epic SHA-256 未变；git diff --check 通过。纯文档变更，未运行应用测试；本轮未进行新路线独立设计审查。


### 基础框架系统架构设计（2026-09-09）

- 摄影师要求系统设计至少确定技术选型（推荐 React Flow）、核心数据/数据库和模块划分。本轮交付 `../../docs/product/creative-canvas-system/architecture.md`、`../../docs/product/creative-canvas-system/data-model.md`，保持同一 Epic 与工作区；未改原型或正式前后端。
- 应用 backend-architect、Context7；核对 React Flow/Zustand、PostgreSQL、OSS、River 官方资料。采用 RF + Zustand 的受控编辑器、Go 模块化单体、PG17 关系+JSONB、OSS、River、Go 有界 Agent；内容修订、资产、节点实例相互独立，图/媒体/运行经统一领域入口写入。
- Verify 图谱 project 与 generation 沿用 `2026-09-04T15:47:38Z`；creativeworkspace 查询 total=0/has_more=false，新文件 not_tracked，scope/compose metadata_changed，已直接读 service/model/repository/0036/scope/immutablefs/manifest。关键事实：上传仍在tx内写不可变对象、卡片一空间、媒体workspace绑定、scope不开放pgx.Tx、compose为PG17。没有将局部调查表述为全仓完备审计。
- 按 cs-epic 新建独立架构审查阶段：宿主 multi_agent_v1 reviewer Fermat，ID `01a08503-9564-7303-bcd0-44284c83435a`；未发现可调用合格异构provider，Paseo偏好文件不存在，按宿主工具要求继承父模型，不指定override。reviewer按cs-review只读，无再委派。
- Round 1 冻结 architecture `0feb70228539dd0cbf1ab7663a562a1d3d0d338e4eccde5a8dc5c701525449fb`、data-model `1f34870d7f202216e9280d5fc478186b389c2ea294a458237f84958d59b3fc22`。2 blocking：绑定冲突的上传修订缺独立保留根；账号写运行槽位误置于周期/币种预算桶。已补上传候选 revision FK/采用及到期互斥，新增按账号唯一 slot/run/token，完善接管/释放/锁序和竞态验收。
- Round 2 同reviewer全文及增量复核：两项resolved，无unresolved/new findings，通过design review。最终冻结 architecture `0c50dbf5637c4061787b978a184882367320c82e8d34ea6fafbe4c1a45715e42`、data-model `e9263ac56c8b88995cacaf091510a3487be68f3ea2316ddf357bef056afcb75f`；这是审查版本，不是approved_revision。
- 文档相对链接、编号、围栏、空白检查通过，旧先导冻结hash保持。未安装新依赖、运行DDL或生产迁移、连接付费模型、实测框架性能/OSS；上述需在明确环境和后续模块切片验证。未提交或发布。


### 显式 key 关联修订（2026-09-09）

- 摄影师要求尽可能不用数据库外键。新创意空间业务表改为显式 account/object key 关联，取消新设计中的物理外键及自动级联/置空，保留 PK/UNIQUE/NOT NULL/单行CHECK/索引。仅改架构和数据设计，旧数据库/旧ADR/先导冻结Epic及River内部schema不修改。
- data-model §1 新增关联归属、写入/删除共享锁、账号和画布校验、GC保留根、历史来源与有效依赖区分，以及异常反关联检查。SQL示例改为显式带account_id的JOIN，数据库不会自动拒绝悬空key，验收改测服务事务守卫。
- 新独立contract review阶段：宿主multi_agent_v1 reviewer Huygens `01a08596-676a-7952-9c00-2f19c42168e9`，继承父模型、只读不再委派。Round1目标 architecture `a0b6309d9ae351cbaaffe88ac258998298427e164175fcc1f7661ec87b3582d0`、data `69eecbd61f515de7792662f4d109665ad362bf7422dafab6a472475ba761bb60`，2 important：资产purge需显式清完标签/检索投影；日志到期后的历史操作回指需定义允许失效语义。
- 修订后同reviewer Round2全文及增量复核，两项resolved、无新finding，通过。最终 architecture `1d84d7c46036fcc95753c62574a84b16e6d75f1e3868bb4d86097edeb67360b3`，data `a9a39c05a76cf535a5bd34e7e694ddb2184ead417741097026609a838e5f179a`。此结论只覆盖两份设计，不把旧review当作当前证据。
- 文档检查无新FK DDL或数据库自动级联残留、链接/围栏/空白检查通过；未运行应用测试或DDL，未提交/发布。approved_revision保持pending。

- 2026-09-09 摄影师认可对审计的核查建议并授权落实设计修订。本轮仅文档：需求 v0.2、架构/数据模型 v0.3、新 ADR-008、审计处置记录及媒体部署契约；未开始功能实现、未提交。旧审计原稿保留为历史证据，新结论落在 docs/product/creative-canvas-system/audit-disposition.md。
- 当前契约明确 TEXT 前缀 ID、无外键写入守卫从首个持久化切片交付、派生列一致性、唯一运行状态机、运行关闭与未知费用分离、独立 creative-v2 前缀、预签名分片上传但读取代理、保留策略总表、不自动抓链接、不强制收藏上传冲突、首次外发与可复用同意。分包/worker/版本/库根锁保留；同哈希跨上传去重未加入首期。
- 核查证据沿用本轮 Verify：图谱 generation 2026-09-04T15:47:38Z，planning ReconcileGC/ReconcilePhysicalOrphans 图谱查询 0/has_more=false；application.go 直接读取确认 planning/{account}/assets 范围，creativeworkspace/service.go 确认旧 creative 前缀。新设计 not_tracked、object-storage metadata_changed，均直接读取；不宣称完整调用链或全仓覆盖。Context7 查询 OSS Go SDK v2 Presign/UploadPart，官方来源支持分片预签名能力；中文搜索官方文档确认无 trigram 时可全索引扫描，不宣称已验证目标库 locale。
- 定向验证：7 份设计/部署/索引文档的相对链接、围栏、行尾空白、22 LIB/CAN/AG 和 9 FLOW 唯一编号通过；git diff --check 通过；旧审计 hash 44e722d6bfaa0f53545161420e9b2cb35fd30e87556e923496b5957e3b77ccc6 和旧冻结 Epic hash ac6b32090a67fff970213fffb135c8bd2ea9f87eacfc27b267a4dd14bd017159 不变。未运行应用测试、执行 DDL、配置 OSS 或调用真实模型。
- 本轮 fresh contract reviewer：James，multi_agent_v1 ID `01a08602-f5a4-7043-a367-2ddccf73dd1d`。未发现异构 provider 工具，Paseo 偏好文件实际读取缺失；遵循宿主限制继承主模型，不设置 override。按 cs-review 只读执行，同一 session 完成三轮。
- Round 1：无 blocking，3 important（终态重试来源/操作承接、外发同意资源版本与显式 key、ready 上传保留释放）及 1 nit（版本示例类型）。均已修复。原架构 hash `f591878c6908a3f1cdbda5d38c9b2fd379417bece00d2ecbce34ecb70b0bfb94`；原数据 hash `e00e2b6ecfd3acb3b12791800d08eda44a110eb35dd9afff3bf7e47533e21925`。
- Round 2：旧问题 resolved，新增 1 important：缺回执一律拒绝与可恢复未执行步骤冲突。修复为分别核对本应存在的回执和可证明未派发/明确回滚的内部步骤，不能只凭查不到回执恢复。该轮数据 hash `63b6b891b9778797ed736b75309b4f3d72aa53bd22ae79a34e90606d6fd486e1`。
- Round 3：完整候选与增量复核通过，旧发现持续 resolved，无 blocking/important/nit；目标未变化。下列是已审设计哈希，不是整个 Epic 开发批准哈希；phase/approved_revision/提交授权保持 planning/pending。

| 当前已审文档 | SHA-256 |
|---|---|
| `.codestable/requirements/creative-canvas-foundation.md` | `4610e29f4bc95abf460f74f41c65b05906f36b8c9db54409a19d6c521234a2d8` |
| `docs/product/creative-canvas-system/architecture.md` | `7c036599a5cd784e0c3851217022ceb79d3cc6196fd6a3a7f65b442722da2fcd` |
| `docs/product/creative-canvas-system/data-model.md` | `8c69a40b3c8b77197d66b5dbbf118e164ed1c2eaef4f3dbf361baf6b9a679e0f` |
| `.codestable/requirements/adrs/008-creative-explicit-key-integrity.md` | `56215c7e8626b15c20688a2150db272ff45159bd49bbce3b3dafba4e1a5baedb` |
| `docs/product/creative-canvas-system/audit-disposition.md` | `2634ed888fcf31e1c26c20b0c7b4cd7400cc26a497fef7fc7736dc796a05f958` |
| `docs/dev/object-storage.md` | `fb0ee38bb1e4f681c8e08bce9bd0724fc99af74536e835479fc243a28d12fc7d` |
| `.codestable/epics/creative-workspace-system.md` | `76c84c3f287a264a3bd2b172c4a15e476b1773a7a6e2c69c90656a5c0b415a22` |


- 2026-09-09 摄影师要求绘制架构图与领域设计。新增 docs/product/creative-canvas-system/domain-design.md，包含部署与领域协作图、核心对象关系、聚合/一致性边界、值对象/扩展、用例事务及主链路时序；沿用当前状态机/保留策略，不扩大首期工具或 CRM 关联范围。现有 CONTEXT 新增通用创意空间术语；architecture/Epic 仅补入口链接。
- 三张图以 diagrams/*.dot 为图源，使用本机 Graphviz 生成 SVG/PNG。已逐张查看 PNG，检查中文文字、连线、图例与首期/后续边界；生成图片分别为 2133×1766、2916×1316、2111×1646。SVG 可解析、PNG 格式/尺寸及图源生成通过；文档相对链接、围栏、行尾空白与 git diff --check 通过。未运行无关应用测试，未修改产品实现或原型。
- 本轮 Verify 以 canonical 文档为目标设计证据：coverage 新文档/图源 not_tracked，CONTEXT metadata_changed，均直接读取；不把 proposed 图当现有代码调用链。图谱项目和 generation 沿用当前会话记录，无新全仓结论。
- 本次为独立 fresh design review：Feynman，宿主 multi_agent_v1，ID 01a08634-3d9b-7fb0-bb92-3cd6f36128a8；无可用异构 provider、Paseo 偏好读取缺失，按宿主限制继承主模型不 override。只读核对完整8份冻结目标及需求/数据模型，首轮通过，0 blocking/important/nit；图形视觉由主线程检查，review 不冒称视觉验收。
- 文档哈希如下；需求 v0.2 与数据模型 v0.3 本轮未变，整个 Epic 仍 proposed/planning、approved_revision pending，未提交或发布。

| 本轮已审目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/domain-design.md` | `ee2835844fe6547a1d50ff428b73a688722e6466f41bdd34416fafbbd3ab7fe3` |
| `docs/product/creative-canvas-system/diagrams/README.md` | `1eb2cf417e95accb94ac082042a57fe2e2ee1d088d076d416349219bfc139387` |
| `.codestable/requirements/CONTEXT.md` | `8267e334596770eae1dc956f5d503db36845f2b146dc6b5f9debbd3ffd2eb1d6` |
| `docs/product/creative-canvas-system/architecture.md` | `e8682cb166ae532a7b4ec167c42a6b0e878413d9377054d03c3223ce6a9b03fb` |
| `.codestable/epics/creative-workspace-system.md` | `5b3073c02658629d544e9d61c14355e7caa222766935f0a473b59864e3151b90` |
| `docs/product/creative-canvas-system/diagrams/architecture.dot` | `4af107dfbce42f9a5a3e5d31ef3f89c1800decf6d0d928f6ba2fa9cda2342fa8` |
| `docs/product/creative-canvas-system/diagrams/domains.dot` | `671a81dc30447a5ce57ff604684e980e6d20bd86de86470abf106beb5c0297c0` |
| `docs/product/creative-canvas-system/diagrams/content-model.dot` | `2cb0b2684865c9cb6e1f989c3b2be01f4ac4d15ced37341eb187e965e9144efa` |


- 2026-09-09 摄影师明确提出统一 LLM Gateway、类似 LiteLLM 的多供应商统一接口，并进一步明确 Agent = LLM + Harness、Tools + Skills 是 Harness 的基石。已新增 llm-gateway.md / agent-harness.md，架构/数据模型更新至 v0.4、领域设计至 v0.2，同步 CONTEXT、Epic 与两张架构/领域图。
- Gateway 拥有模型请求/真实尝试、统一观测、预算预留、用量与供应商成本；Harness 拥有 Skill/工具、上下文、执行循环、权限、运行恢复及产物检查。摄影师商业计费后续单独建模，不等同于供应商成本。当前默认进程内 Go 模块，具体 LiteLLM 产品部署与微服务切换仍须选型及跨服务事务/幂等/撤销协议验证；未安装或部署网关。
- 预算草图归属从 creative_agent_budgets / creative_usage_reservations 提升为 llm_budgets / llm_usage_reservations（尚无实际迁移），Agent 通过初始预留 key 和步骤请求 key 关联，防止双账本/双扣。统一核算使用独立成本位置、按当前已入账值更正、去重回执与证据优先级；并发分叉和无法确定先后的证据先核实。
- 官方证据：Context7 先 resolve LiteLLM，再查询 /berriai/litellm，结合 docs.litellm.ai 确认多供应商统一 OpenAI 风格接口、流式和工具调用；不把接口兼容视为模型能力一致，也不宣称选定某个产品版本。图谱 Verify 仍以文档为目标设计事实：本轮九份目标 not_tracked / CONTEXT metadata_changed，均直接读取；不作实现或全仓调用链声明。
- 验证：文档链接、围栏、行尾空白、git diff --check 通过；两张图重新由 DOT 生成 SVG/PNG 并视觉检查。旧冻结先导 Epic 与审计原稿保持不变。未运行无关应用测试、修改真实数据库或生产配置，未提交。
- 独立 contract reviewer Bacon，宿主 multi_agent_v1，ID 01a08643-6041-7ff1-94cf-9e4b8ba5b4e9；无可用异构 provider，Paseo 偏好文件已实际读取缺失，遵循工具约束继承主模型。Round1 无 blocking、1 important（更正的唯一当前核算与乱序语义），已修复；Round2 完整候选及增量复核通过，0 blocking/important/nit。审查结果仅为设计一致性，SQL 并发验证仍待模块实施。
- 当前已审哈希如下；approved_revision/开发及提交策略仍 pending，不把该模型边界确认当整个 Epic 开发或发布许可。

| Gateway / Harness 当前已审目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/llm-gateway.md` | `d57cbe289981c79c1792dc23402c88b64de300bbbeb6366414516f3f337b356a` |
| `docs/product/creative-canvas-system/agent-harness.md` | `5c3646b9347701d16565fe1072682a21c9a38d6582a16e08dbdd6da7dd9f10fb` |
| `docs/product/creative-canvas-system/architecture.md` | `fd877a707a48556cedbab93861d2283bac1778797a549c817c786a0f576975c3` |
| `docs/product/creative-canvas-system/data-model.md` | `56114ce9d1ae9cb5b313ff76198b9447acece18b7208dc5792a4737cc90eff08` |
| `docs/product/creative-canvas-system/domain-design.md` | `2b21bcaae48a6b5c343d7a4c2e2f6be889e66a6d5ebec6c72bdcce832681fe11` |
| `.codestable/requirements/CONTEXT.md` | `99a58d7238aa0b624a3ed1f1cbca5bd33827096a25c702c6fc2b232e80a52c22` |
| `.codestable/epics/creative-workspace-system.md` | `fc650b73f64d551a6fc2cd5063c4715efa25c3fa7d01bf9261abcaa732af408c` |
| `docs/product/creative-canvas-system/diagrams/architecture.dot` | `7de42ad765483e385e0ded7a0c4069bb7c3571184ad20145398641ce80604b7a` |
| `docs/product/creative-canvas-system/diagrams/domains.dot` | `9ef38a4bcbb4e1fd6cf6264746be4d6b289118c1b6518528f54a8a94c436fef6` |


- 2026-09-09 摄影师对齐整个架构后授权开始模块设计。本批完成公共契约、内容与媒体第一版模块方案，见 modules/README.md；细化13表DDL草案、局部 CM-01–05 切片和测试矩阵。主栈/原型/生产API没有实现改动，codec与资源启用清单、票据/派生细表及跨模块业务端口的实施前置条件明确保留在切片中。
- 接口沿用现有 ErrorEnvelope、AccountScope/TxAccountScope；新幂等不扩旧白名单，采用短事务与类型化结果。内容增加明确声明和来源关联；上传区分业务状态与 io_phase/执行权，创建/完成202回执不可变，服务器预分配独立发布操作ID，发布与候选采用结果均持久保存。
- 本轮 Verify 图谱确认 generation 2026-09-04T15:47:38Z；查询 WithTxScope/Problem/ObjectStore/NewURL 相关模式 total=0/has_more=false，直接读取 envelope.go、scope_tx.go、immutablefs/store.go、storetest.go 和 OpenAPI 相关范围。coverage scope_tx/OpenAPI metadata_changed、其余 match；新模块稿 not_tracked，均直接读取。不作完备调用链结论。Context7 PostgreSQL17 查询作为约束背景，实际DDL探针作为语法/锁行为证据。
- 实测使用现有 storetest.Main/NewURL、空模板和标准 postgres:17-alpine 容器，未连接开发/生产数据库。临时 internal 测试包在执行后自动移除，容器终止。初轮2探针通过但 ready 断言被审查发现假阳性；修复后加入合法对照及持久来源/发布身份探针，最终3项PASS：0.09s/0.62s/0.17s，包总3.783s。仅证明DDL/SQL锁和记录查询，不替代尚未实现的服务守卫、完整生命周期、真实OSS或端到端验收。复现脚本与准确范围见 modules/verification.md。
- Fresh design reviewer Maxwell（multi_agent_v1 ID 01a0866b-c860-7401-bbb8-ea028f826699），同阶段两轮。未发现异构provider，Paseo偏好已实际读取缺失；遵守宿主工具限制继承主模型不override。Round1：1 blocking（上传来源未持久关联）、2 important（受理/发布回执身份、ready测试假阳性）；全部修复并重跑相应探针。Round2完整12目标与增量复核通过，0 blocking/important/nit。review只读未重新执行Docker；主线程保留实际执行证据。
- 文档链接/围栏/空白、无FK草案检查与 git diff --check 通过。当前12份已审哈希如下；旧审计与冻结先导Epic未改，未提交。整个Epic仍planning/pending，下一步为资产库与画布模块设计，不沿用历史ITEM进度或提交许可。

| 本批已审模块目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/modules/common-contracts.md` | `ee3d76dd0d6c475cc2ebe6e793c75a9e054b7bb17d75bb03632201d9ac4ac057` |
| `docs/product/creative-canvas-system/modules/implementation-slices.md` | `5a32b88a620f30846b188253cbd1206b635aa888e1844da578cb228df4a8f486` |
| `docs/product/creative-canvas-system/modules/run-schema-probe.py` | `ed32f6023a3c5e9746aa2617ec386786bf7e80efb293b4bb8c79498c809dd016` |
| `docs/product/creative-canvas-system/modules/content.md` | `20c5f2a721a4b3966a1d697c80fe12d8713eb18415e618911f8d391a307880c0` |
| `docs/product/creative-canvas-system/modules/schema_probe_test.go` | `a358d4d0207be4003f06c7f91181c33e061b9fa7d9dd8f870869c307e714d1aa` |
| `docs/product/creative-canvas-system/modules/README.md` | `622ff58056e3ba1c8f30c6e64bc85c742080b47600f721b8c0ea6d9fdea48d90` |
| `docs/product/creative-canvas-system/modules/verification.md` | `c1d80ad40a384a7321ceeea89894a3d20f9f5a7bc5ba8a308bed27181a82e74a` |
| `docs/product/creative-canvas-system/modules/content-media-schema.sql` | `c94b041e9ce8fe3c8ad14eedb5669f24a2f2ef2df8fac94ff64ad705f1175ebb` |
| `docs/product/creative-canvas-system/modules/media.md` | `ac99aa07ac33081111f3d98d4da483dc1509208f6d8247f1a134f76194e716ed` |
| `docs/product/creative-canvas-system/architecture.md` | `2a9679247fcdb0027d594ab91bb54ae109b6581fd2bd4a1bb09840e0be2eb96f` |
| `docs/product/creative-canvas-system/data-model.md` | `3d9aac70d551d41c68ceb9a870f3245e2ade9fba14744f863af6adb2a15cc69f` |
| `.codestable/epics/creative-workspace-system.md` | `9b67636fd701f2aaa04d4c47a6f402f42f89e1e5e3b70762d6acd999a3fde0eb` |


- 2026-09-09 摄影师授权继续下个模块设计，本批完成个人资产库与画布模块方案，入口 modules/README.md。细化资产检索/分组/标签/回收站、类型化画布命令、父子坐标、动态/固定引用、保存队列、撤销重做，以及库/媒体/画布的 InTx 交接端口；新增16表草案叠加首批13表，继续无外键、显式账号与对象key关联。项目不关联CRM，媒体/内容/资产/节点身份保持分离。
- 设计新增持久图对象身份记录以防删除恢复后版本倒退；撤销区分物理版本和字段组逻辑状态。只有验证后的撤销可恢复旧状态token，手工改回同值不能伪装；连续及重叠写集按状态链合成，关系闭包防止吞掉外部新增连接/子节点。回执固定逐对象版本及拓扑前后版本，本地依赖命令只承接可证明来自自己的结果，外部变化保持冲突，不通过新快照静默更新旧前置条件。
- 模块依赖划分为 LC-01–05：个人库查询整理、画布图与命令、媒体/库/画布交接、保存同步与撤销、回收保留联验；与 CM-01–05 的交接关系已双向同步，不把这些局部编号当正式已批准开发队列。
- Verify 图谱确认同一 worktree 项目、generation 2026-09-04T15:47:38Z，ready 18138节点/79888边；12份候选 coverage 为 not_tracked，直接逐份读取，未作新生产调用链或全仓完备结论。Context7 先 resolve 再查询 /websites/reactflow_dev：parentId 相对坐标、父节点先于子节点、extent parent边界约束；仅group可作为父节点属于本系统约束，未安装或运行React Flow验证。
- 实测使用标准 storetest 隔离 PostgreSQL17，执行真实 library-search.sql。最终4项SQL探针全部PASS：库组合筛选0.09s（10子场景及账号隔离/标签改名）、单行约束0.08s、图身份恢复0.07s、单字段Undo状态与回执对象/拓扑版本协议0.09s，包2.911s。共用runner的首批默认scope回归3项PASS（0.06/0.56/0.05s，包3.348s）。临时包和容器已清理；未连接开发或生产数据库。探针不等于真实应用服务、完整Undo/关系闭包、前端队列、React Flow性能或端到端验收，这些保留在切片矩阵。
- Fresh design reviewer Schrodinger（multi_agent_v1 ID 01a086ac-4d94-7490-a69a-67205e3c8115），同阶段3轮；无可用异构provider，Paseo偏好文件已实际读取缺失，按宿主约束继承主模型。Round1：1 blocking（连续/组撤销状态链）、1 important（回执缺少对象结果版本）；Round2 blocking解决，important仍有拓扑版本映射残留；Round3全部解决，无新增，0 blocking/important/nit。完整冻结12文件与增量复核通过，reviewer只读未重跑Docker。最终清单SHA-256为6642f37d4720157555d92e321eb7f5f880d38a9ea6fba4e8d88aad35ae896e51，具体文件哈希如下。
- 文档链接、围栏、空白与 git diff --check 通过；需求、旧审计原稿与冻结先导Epic哈希保持不变。未修改生产实现、运行生产迁移或提交。整个Epic仍为 planning / approved_revision pending / current_item null；下一批为 Gateway 与 Harness 模块详细设计。

| 第二批已审模块目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/modules/library.md` | `64181db9e6cfde70f95b4f170b1e64fc5fc9aa9808bff91b7c02f6d44e4ffbaf` |
| `docs/product/creative-canvas-system/modules/canvas.md` | `89578e37bfb913484071ee8c24d8f223d4cf2a53a21558ad386aacdbbbd9218c` |
| `docs/product/creative-canvas-system/modules/library-canvas-schema.sql` | `d8317cbb736c2b7598892ac579842c1e4a00bce9da38cca3eb7658901eabbb07` |
| `docs/product/creative-canvas-system/modules/library-search.sql` | `948b158a58d3978517e73c0a3d1bb423ae3b03ee745d86ba9b2c4635460eb591` |
| `docs/product/creative-canvas-system/modules/library_canvas_probe_test.go` | `a395cf849c2c092ba80eaf0f35947ed36585eb01e554285a289ad79da3193e58` |
| `docs/product/creative-canvas-system/modules/library-canvas-slices.md` | `683acfb16192ef4f4b98b0add60303aab1a1f4f9429b72eada22cac01a8ba541` |
| `docs/product/creative-canvas-system/modules/run-schema-probe.py` | `2bc802fba9e2011594d802fb258293d840ff47dbfec4a85f6e65d106c780ed1f` |
| `docs/product/creative-canvas-system/modules/README.md` | `68f47493fc4e7b6a78037175a145eeb2a205a007663b0e628ace40739586dc1c` |
| `docs/product/creative-canvas-system/modules/common-contracts.md` | `314096259dcba2e8d4e54112cad1a95045362058a8c3a06b1d85e8ec79a1cd56` |
| `docs/product/creative-canvas-system/modules/implementation-slices.md` | `78d843bde74a269d7e78771820efae0b1aac7d43c32b0c0ed635d4ee8b89e464` |
| `docs/product/creative-canvas-system/data-model.md` | `af29402f8c3e0cf19f08a1691a6e556494ae6ad9f9ad215083e640266743f7ac` |
| `.codestable/epics/creative-workspace-system.md` | `f191629b6cf3273b4d8f26309de621b247e993bae2e00977197f5b4edc4b63b4` |


- 2026-09-09至09-10 摄影师授权继续模块设计。本批完成 modules/gateway.md、harness.md、agent-api-events.md、gateway-harness-data.md 与 GH-01–05 切片，同步模块入口、媒体附件目标、公共锁序、data-model保留及记录归属、Gateway/Harness边界索引和当前Epic设计入口。首期仍为Go同库模块，不部署LiteLLM Proxy，不接CRM，不引入任意脚本或第三方Skill安装。
- Gateway固定统一输入/完整结果契约、Catalog能力、Reserve/Prepare/派发/消费/核实端口；请求、真实attempt、工具效果和费用是不同事实。预算分组准入行串行跨月总量，不在Agent另记spent；partial usage只释放有证据的hold，实际超支仍全额记录。成本证据/紧凑墓碑随账号保留，大请求/结果仍按原90天及活动消费/unknown例外，避免清理后旧操作被当首次。
- Harness定义平台Skill reference-direction@1、六种受控工具、不可变选区/内容输入、执行读集、独立附件草稿/发送交接、slot/epoch、提案采纳和状态恢复。REST控制与fetch SSE分开，事件先持久再通知，pruned水位识别事件全清后的旧游标；消息完整结果与短期通知分离。原run状态机仍为唯一权威。
- 图谱Verify本轮确认worktree项目ready 18138节点/79888边；check_index_coverage generation2026-09-04T15:47:38Z，15候选均not_tracked，直接全文读取/校验。无新增生产结构或全仓负面结论，无未完成符号查询分页。历史审计命中只作来源，不改原稿；attention旧Docker串行建议不沿用，使用现行AGENTS规定的storetest策略。
- Context7先resolve后查 /berriai/litellm 的流/工具碎片、/riverqueue/river 的InsertTx；官方网页 docs.litellm.ai/docs/completion/stream、riverqueue.com/docs/transactional-enqueueing 与 /unique-jobs核对统一流和事务入队参考。协议的持久回放/费用语义由本系统定义，不宣称库自动提供。未选择实际供应商型号、报价、SDK版本或已接通能力；启用前适配清单在GH-01/02。
- 新增12表SQL协议字段投影及6项Go探针，只在标准storetest隔离PostgreSQL17运行，非完整模块DDL/生产迁移。最终PASS：预留认领/部分结算0.06s、并发费用更正0.04s、取消与派发两种顺序0.06s、事件回滚/清理游标0.04s、原子消费0.04s、未受理后预算竞争下重新准入0.04s，包2.956s。成本更正为真实并行两事务；取消和重新占额为确定性串行顺序实验，不冒充HTTP/供应商竞态。缺失的服务/价格配方/River桥/浏览器/真实模型验收全部列入切片。
- 共用runner新增gateway-harness scope后，原content-media默认scope回归3项PASS（0.06/0.64/0.08s，包3.669s），library-canvas回归4项PASS（0.18/0.13/0.09/0.11s，包3.948s）。临时internal包与容器清理完成，backend/frontend/api/scripts无持久改动，未连接开发或生产数据库，未产生真实模型费用。
- Fresh design reviewer Kierkegaard（multi_agent_v1 ID 01a086fc-27d3-7ba0-98ac-970c20c6da43）。可调用工具未发现异构provider；Paseo偏好文件实际读取缺失，按宿主工具规则继承父模型不override。首次spawn命中并发上限，关闭已完成前阶段Schrodinger后创建新的独立reviewer，没有沿用前阶段轮次。Round1：2 blocking（未受理后的恢复状态冲突、重试缺少重新占额）和2 important（自身写入后的读集承接、最终提案同步/异步路径冲突）；已修复并加第6探针重跑。Round2完整候选+增量复核通过，0 unresolved/new/blocking/important/nit，15文件hash一致；reviewer只读未跑Docker。
- 修正要点：unknown可靠未受理且仍可重试时原request回prepared；RearmInTx在原预算桶和跨月group重新准入，hold_generation/下一attempt与派发意图原子，B占满额度时A无新attempt；执行读集仅按自身receipt与拓扑前后版本承接，不改历史输入或已prepared操作；apply_only同步200落库/终态，apply_and_continue异步202且执行时再次检查deadline。
- 文档链接/围栏/空白与git diff --check通过，需求/旧审计/冻结先导Epic哈希不变，未提交。整个Epic仍planning/pending/current_item null。三批主要模块方案已形成，下一步收敛正式开发依赖、适配前置项与整体迁移发布/验收计划；本次不将设计通过标为产品实现完成。
- 最终审查清单SHA-256：e5b2bb63b019fa6c89165789f88f7efcc6c6d8c37403230a76d74f8ffcf81110。已审文件如下。

| 第三批已审模块目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/modules/gateway.md` | `6557c35490df69f080257615d3707420ec05e9e726ce7b58022b797550b4c8ef` |
| `docs/product/creative-canvas-system/modules/harness.md` | `b4f450a8e8915ecfa2fdbe6e16469c238f4c39af10aad4e094d7ecda93d507f1` |
| `docs/product/creative-canvas-system/modules/agent-api-events.md` | `90b5f4a36bffc951df48cc92f73b191844919964e7a45c133daa90ca6b939b06` |
| `docs/product/creative-canvas-system/modules/gateway-harness-data.md` | `da5e4ef6185c2272699a71c19aed276f6cdaa7b9f15f4844643d647819904317` |
| `docs/product/creative-canvas-system/modules/gateway-harness-slices.md` | `3d69b0da8d0727769fe0e1cdbea8c997fc1d1d06d43b588bb5655513b3b9eee3` |
| `docs/product/creative-canvas-system/modules/gateway-harness-protocol.sql` | `80c757d7ff41fd71602616a3b1e04e11df818c9b76d028d71c5950773e62fb5f` |
| `docs/product/creative-canvas-system/modules/gateway_harness_probe_test.go` | `a79a920b4fdd9aa427b63337dbf75eaa157afb8f18f3c835a59fb6b42cc5168a` |
| `docs/product/creative-canvas-system/modules/run-schema-probe.py` | `93d67f1c2ce145cde5c26d73e70fddc6ada7f25ebcdd25a8e0e4bf324d16d960` |
| `docs/product/creative-canvas-system/modules/README.md` | `be9cb68936dfc79098dd416a04cb5537abc00d57d0d75996e503f3bc943fb298` |
| `docs/product/creative-canvas-system/modules/common-contracts.md` | `a4c57a2350010779651b1e56fd121baf700c6c4af917a5ce0c438dafd648062d` |
| `docs/product/creative-canvas-system/modules/media.md` | `f9dfda721065546599566e823246faf53ae78e4884ec2a0eeda9218246f7464f` |
| `docs/product/creative-canvas-system/data-model.md` | `867ae61015b912c5d9ab5be9273be299977a29b1194deef959c6e2c78a2b1264` |
| `docs/product/creative-canvas-system/llm-gateway.md` | `4bdaeb9aefb63c2d1bee369471eaebb0ed3ba77e104a15d1db71e8cec39bc3fc` |
| `docs/product/creative-canvas-system/agent-harness.md` | `3fc407b62165e163d723b80a87361b1a6d45a48ae6b793ffad64ec563507ccd4` |
| `.codestable/epics/creative-workspace-system.md` | `afe0d8f04aed38b4636ff35fe6a58306e7818aea184ff23d1fd23d1e092a7d9e` |


- 2026-09-10 摄影师授权继续收敛正式开发计划。本轮将永久Epic当前入口重写为FND-01–12唯一任务/依赖/交付/验收出口，旧<details>历史全文保留，SHA-256 e79377c8d01b28ab375182efc701bf673cd9e59be42ee070cd796c0eaa3ac5c6未变。新增delivery-plan.md、migration-release.md、acceptance-plan.md，同步模块README和架构索引。原需求、先导Epic冻结稿、历史审计未改。
- 执行出口为最小文字闭环→人工图文音视频主链→三部分真实主链→可发布候选。CM/LC/GH共15个局部切片全部映射正式FND；媒体发布与库/节点交接合入FND-05，不保留循环依赖。独立文档扩展归FND-04、全根生命周期FND-10、旧数据承接FND-11、完整验收FND-12。并行分支只是可选依赖事实，不是本轮并行或提交授权。
- PRE-01–10列明基线/依赖版本、React Flow/River、格式与存储、真实模型/费用、引用schema、迁移writer、RPO/RTO、发布范围的负责项和最晚截止点。缺真实服务凭证只阻止对应能力验收，不声称桩已接通，也不阻塞无依赖人工链。粗估43–69单工程师人日为假设范围，待FND-01/05/09实测重估，非上线日期承诺。
- 发布契约区分应用部署、账号writer切换和物理清理启用；旧媒体/来源/CRM关联保留兼容读取，新项目不建立CRM关联。新域有编辑后不得直接恢复旧writer。只读预检、源冻结、固定快照、可重入复制、逐对象对账、原子切换、停止/恢复各有条件。RPO/RTO仍须目标环境固定及恢复证明，未填虚构服务承诺。
- 验证采用文档结构证据而非无关应用测试：12任务依赖存在且无环，全部22需求/9FLOW、15局部切片映射完整；所有FND引用合法，6目标本地链接/围栏/空白与git diff --check通过；历史块及原稿哈希核对通过。没有新增生产代码、OpenAPI/迁移/依赖安装、模型调用、对象操作或应用测试；不把之前SQL探针计为FND已完成。
- 图谱Verify确认项目ready18138节点/79888边，coverage generation2026-09-04T15:47:38Z；6目标+需求+3模块切片not_tracked，直接全文读取；go.mod/package.json为metadata_match并读取配置，只用于实施前再次核对的背景，无新生产结构/全仓负面声明、无待补分页。未新查技术API或声称依赖已经兼容，本轮为既有设计的路线整合。
- Fresh design/route reviewer Socrates（multi_agent_v1 ID 01a08722-92f0-79d0-9153-2ce56c278cb0），无异构provider创建能力，Paseo偏好实际读取缺失，按宿主规则继承父模型。Round1 0 blocking/1 important（切换前放弃后旧域变化，现有映射无法重新建基线）；修正未采用目标/映射/hold退役与分页恢复，加入FND-11正反例。Round2旧finding解决，新1 important（旧文字将执行身份定义为账号+规则版本，与新增execution_id冲突）；统一每执行的状态/任务/cursor/operation/回执隔离，规则版本与稳定source key不承担执行身份。Round3完整候选与增量通过，0 blocking/important/nit/unresolved/new。审查只读未执行数据库。
- 当前计划可进入FND-01实施确认；仍phase planning、approved_revision pending、current_item null，item_progression/milestone_commit/remote_publish未从历史推定。未提交或发布。最终清单SHA-256 ae6e6c5e9f522695757b96e445921c5b26929d9f6956d1f76e52e7e8f6f530b4；已审文件如下。

| 正式开发候选已审目标 | SHA-256 |
|---|---|
| `.codestable/epics/creative-workspace-system.md` | `fd27e4e2cf78d8d7967ce0565db1cbd0db8039fb4c232b5e893b0c55c90c8c57` |
| `docs/product/creative-canvas-system/delivery-plan.md` | `53196a35d169e97bc2df19aa384513c9fe9de8836b0f17a7b9c43e80bdb41afc` |
| `docs/product/creative-canvas-system/migration-release.md` | `494288bf7f20c7611cf055caf5af273dcec388d8b41b058b713a81c96f1fad03` |
| `docs/product/creative-canvas-system/acceptance-plan.md` | `20b8d890557b33bb9541f5b08027d2867a476180d8cfa75ba672f635f5479f94` |
| `docs/product/creative-canvas-system/modules/README.md` | `e81ea96ebb983af49fa9526924a6b34eec049c1631e42a19b0d4d6c7b7a25661` |
| `docs/product/creative-canvas-system/architecture.md` | `7efde8b102e995b6a56c87f8dd36dcceb270a85b8f2bf7cc1e4e1f4905a632a0` |


- 2026-09-10 摄影师明确推荐Eino并授权继续。本轮完成Eino ADK接入决策与隔离适配，新增eino-adoption.md及probes/eino；架构/data-model至v0.5、Harness/Gateway相关模块至v0.2，同步FND-01/06/07/08/10、实施/验收和架构图。默认通用Agent机制由Eino承担，业务执行权/内容/命令回执与Gateway费用权威保留；没有再自建第二个通用loop。
- 版本证据：go list -m @latest返回Eino v0.9.19，随后固定下载该tag，GoVersion1.18、模块sum h1:i71YUBK3nwY4L53dkzRgZpAcPSZ4v4eRponN7W9sDtk=；本机Go1.25.5 darwin/arm64。独立probe.mod/sum锁Eino和实际backend/storetest所需依赖；没有引入EinoExt，正式组件按FND-06独立锁定。backend/go.mod/go.sum及生产代码无变更，不能把实验模块成功编译当全应用依赖兼容完成。
- Context7先resolve再查询Eino，结合CloudWeGo官方版本/Runner/Skill/摘要/工具/AgentAsTool文档与GitHub releases；检索中的自动文档Checkpoint Save/Load/Delete接口与固定源码不一致，直接读取v0.9.19的Get/Set及可选Delete并编译验证。Go模块缓存源码不属于项目图谱，使用固定tag的model/interface、adk runner/handler、tool interrupt、skill/summarization/reduction、core interrupt等有界直接读取，不改依赖源码。
- 图谱Verify同worktree ready18138/79888，coverage generation2026-09-04T15:47:38Z：20候选not_tracked并逐份直接读取；storetest/go.mod metadata_match，go.sum not_tracked按文件核对。无新增生产调用链或全仓负面声明，无待补图谱分页。原需求/审计/冻结先导Epic、历史折叠块哈希保持不变。
- 最终八项真实Eino+PG实验PASS（包2.750s）：跨独立进程Checkpoint恢复0.10s、工具提交后Checkpoint.Set前进程退出71再固定轨迹回放0.08s、取消后Resume无写入0.08s、Skill元信息/正文渐进加载、注入摘要模型、Reduction完整结果PG保留0.06s、未知模型错误不自动重试、AgentAsTool不继承父私有输入并返回结果。TestProbeChild父阶段skip是专用子进程入口，在恢复用例中实际执行。标准storetest一个父容器、每用例独立库、子进程复用该库；容器/临时模块已清理。
- 模型与最小业务表是替身，无供应商/OSS/真实Gateway账本/真实River/前端SSE调用或费用。提交间隙验证使用预定model-1/model-2与简化hash，只证明两轮轨迹接缝，不能当通用重放算法。Checkpoint实验只有账号隔离Get/Set；生产run/epoch/CAS/TTL、完整工具帧映射、同参数多轮身份、schema升级恢复、只读资源工具与所有外发/媒体守卫仍由FND-07/08实施验证。Stream仅签名/单消息适配，本轮没有验证分片流。
- 设计明确SkillRegistry映射Eino List/Get固定目录/包digest，首期inline；受控ReadSkillResource/ReadRunResult与业务工具一起计数。摘要/结果卸载是运行上下文变换，原始输入/执行读集不变；摘要验证前关闭，所有辅助调用同样经Gateway的授权和预算，默认不启用Eino retry/failover或Skill模型覆盖。新增checkpoint/context items及显式content refs清理责任；无一致恢复证据时保留结果并停止/核实，不盲目从原问题Query。多Agent仅接口验证，不开启生产委派。
- 验证：20目标本地链接/围栏/空白、历史块/FND数量、git diff --check通过；Graphviz重生成architecture SVG/PNG并视觉检查。初次锁文件生成及后续只读锁/runner控制环境隔离后的实验均通过，最终时长以上述2.750s为准。不为纯文档适配再跑无关全量应用测试。
- Fresh design/contract reviewer Fermat（multi_agent_v1 ID 01a08764-5ebf-7100-bec1-0b9e93d4bd2b），只读完整20候选/19文本和PNG，Round1通过，0 blocking/important/nit；实验范围、简化与生产待验收项未过度声称。无可调用异构provider，Paseo偏好实际读取缺失，按宿主约束继承父模型。reviewer未重跑实验/编辑/委派。最终清单SHA-256 7f93d94fba39e965c7661f5f8fd3f8c002828ec409f895e060df7484d2d96b94。
- 本次为已授权Eino适配子范围，不将FND-01整项、生产Agent或全部Epic标完成。phase planning/approved_revision pending/current_item null保持；未提交、发布或迁移账号。下一步补FND-01其余实际工程底座及依赖验证，执行/提交策略不从历史推定。

| Eino接入已审目标 | SHA-256 |
|---|---|
| `docs/product/creative-canvas-system/eino-adoption.md` | `9177165692976381b3295d28b22f01874c4931f7ab3bf691a266096d12276c5c` |
| `docs/product/creative-canvas-system/probes/eino/README.md` | `220631b6e645e797ef2bbee13921fb5b666917f85db16752f9441b98073169b2` |
| `docs/product/creative-canvas-system/probes/eino/adapter_probe_test.go` | `7112aa039f47c6cd27978cf0a5b73f464557ecdf740a3d878433c35ab96fb291` |
| `docs/product/creative-canvas-system/probes/eino/run.py` | `f68c73a716445a7717490d2c2887e121ac34e91e05a8c9e90807c74676f760eb` |
| `docs/product/creative-canvas-system/probes/eino/probe.mod` | `4da56b09bc1cc55bce57a4d4298dacdea8bec471a8ad4789eb31bb65bfade06f` |
| `docs/product/creative-canvas-system/probes/eino/probe.sum` | `5486e21ee2857399008869d10f65897d64a25212b80ba314d7170d51f076d969` |
| `docs/product/creative-canvas-system/architecture.md` | `e1d88f64f68d6521e387d12e6eabbb20095f30cbfc531530daf3bfa809c9959d` |
| `docs/product/creative-canvas-system/agent-harness.md` | `2bee43a43f0a45bb3bc0a5c4fb0fe1e5bcdbbcc794044fe04269bd07de8a7260` |
| `docs/product/creative-canvas-system/data-model.md` | `c63de3b42503c37a986aa9c5ada0fa14b78d2516ad7030de8acfa899a71afa40` |
| `docs/product/creative-canvas-system/modules/harness.md` | `0ff58d59cd7767a772f0315e5e9900d86cb96f5616fe61306d53eed06a177c8c` |
| `docs/product/creative-canvas-system/modules/gateway.md` | `5677f2f2a61cccc1247341c28494da2e03b7e3f98cba1e9d0a1a9c5f9156d2eb` |
| `docs/product/creative-canvas-system/modules/gateway-harness-data.md` | `f0d077f570c7cd933df6fe4815c7a7649b567eddcbe2f27b1eb0e71f0440611f` |
| `docs/product/creative-canvas-system/modules/gateway-harness-slices.md` | `76c4ead9db5ee08fb3ce9eee33965a106a35aa00d18975f99ecb7ba84aae969b` |
| `docs/product/creative-canvas-system/modules/README.md` | `b77a169b9a081bd7767b05352112596925d2c4559c112d76ace03abc9908568e` |
| `docs/product/creative-canvas-system/delivery-plan.md` | `d13826d3d7bc5caf7c754ec76674b1b63f4722f5a65cce2f54bb7c5ccaed45a8` |
| `docs/product/creative-canvas-system/acceptance-plan.md` | `9990a9ccb6e50b6d8c124dc21b13fd4437f72d04edf61ae66931d3e386ab9236` |
| `docs/product/creative-canvas-system/diagrams/architecture.dot` | `ca711b1c7c67d53c031a61388a4b5f749118038bf9256350c0a06df3199b1251` |
| `docs/product/creative-canvas-system/diagrams/architecture.svg` | `049c584e09fe207097ae243c183bac486ddf16aed9d89a11708a6939532c12e3` |
| `docs/product/creative-canvas-system/diagrams/architecture.png` | `d81dc3d6bb133f2f8fa837db2a1b9b24d439330f0c8fe4ecea75d91b66869928` |
| `.codestable/epics/creative-workspace-system.md` | `6d258039278826b8fbae9e10cf420882497d9ff945bf687fb71b74b8d9ae0fd4` |

## 2026-09-10 画布能力工具化设计补充

- owner明确初版就支持Agent查看、添加、执行画布节点，并保留未来全系统Agent操作路径。本轮更新需求至v0.3/AG-03，同步系统、模块、FND与验收；不是production实现或发布授权。项目继续独立于CRM，新业务表继续显式key、无外键。
- 新增canvas-tooling.md：UI与Eino工具共用应用能力目录；查询、创建/编辑/组织、执行控制均有明确契约。原六业务工具是便捷接口而非封闭上限。节点声明可用动作，执行具有独立身份/快照/状态/回执，内部text-compose在FND-04/08验证真实执行；静态媒体不可执行，生产生成/3D仍后续。
- attached节点执行有独立租约，不再抢Agent slot；父run queued有界等待，输出成功但应用pending仍等待；取消/期限/父slot状态共同约束发布。异步发布同事务承接父run实际效果/读集/撤销组，不能用最新版本或旧checkpoint覆盖业务事实。
- 当前阶段独立审查：cs-review reviewer Boole，ID 01a08915-fab0-7591-96f0-003f8bf20309。第一轮0 blocking/2 important，分别为pending应用分支与异步发布效果归属；第二轮修复后PASS，未解决/新增均0。未发现异构provider、偏好文件仍缺失；遵宿主限制继承模型，不自行覆盖。审查期间候选冻结，reviewer只读。
- Verify图谱project Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign，generation 2026-09-04T15:47:38Z；10候选coverage全部not_tracked，采用文档直接读取，不声称代码结构覆盖。
- 验证：最终10文件hash、相对链接、代码围栏、行尾空白通过；12个FND依赖无环、22需求与9个FLOW映射完整。旧先导Epic、原审计与历史折叠块hash未变。纯文档改动未重复运行生产测试，新增执行器/竞态验收尚待对应FND实施，未冒充已实现。
- 最终冻结清单 `/tmp/creative-canvas-tooling-review.json` SHA-256 `4be73705818d9cac297554adb7d6cd4ba55cbf0d5d534bcdc5b3019edc9a2672`；包含本轮有意更新的需求v0.3，当前候选hash如下：

  - `.codestable/requirements/creative-canvas-foundation.md`：`7926b36afe4552cd59d5140687303d011f19135b3a7d1887f07bc9b68e9a9a39`
  - `.codestable/epics/creative-workspace-system.md`：`0fbbf3905f171337c2dae8668c9e87d1bfc688547f07d4f074ea1a53e32f443b`
  - `docs/product/creative-canvas-system/canvas-tooling.md`：`de96f343462a6db9915f9bc12743578d10721935970e54e1b14cad8e99b42552`
  - `docs/product/creative-canvas-system/modules/canvas.md`：`ad6c7b6464cd7e1bf3abf179174ccc1e6da089b3949a38ab6a788141a30bc763`
  - `docs/product/creative-canvas-system/modules/harness.md`：`5e011b70f8dace7d60174937089673962522041781569e539eaf21b8be7f9e1d`
  - `docs/product/creative-canvas-system/architecture.md`：`3751a5471550ac4d48a1853becca87bc681d4c2875ad2adecfac91d9e853756d`
  - `docs/product/creative-canvas-system/data-model.md`：`35afe1ce86503e78249e91922b8087f2e62970b2aaca8d8527bf02bdd92f976d`
  - `docs/product/creative-canvas-system/delivery-plan.md`：`72830c4c6b594629c084122ce043f4f18a92ab24c51540e55279552bff2e7e3c`
  - `docs/product/creative-canvas-system/acceptance-plan.md`：`ba6dc4a81bb8f692f8dff0e560f1169500e50f47a02b902b0a8e04e26c1a7c80`
  - `docs/product/creative-canvas-system/eino-adoption.md`：`351b2cc05daf4a412e72fdbd7e76aced81dc71ae210b7101bf9ed9f21423eac5`

## 2026-09-10 节点Prompt、真实生成与结果版本范围更新

- owner明确节点承载生成结果，要求元信息/data/status、参考连线、组父子关系，每节点Prompt Box首期直接生成及媒体多次生成版本切换。已核对原节点模型：元信息/config/内容引用、独立edge、parent_id/相对坐标已有；status投影、持久Prompt和长期结果历史是本次新增。截图仅作交互参考，不从截图认定供应商模型/价格事实。
- 本轮按文字/图片/视频/音频四类各至少一个真实模型起草。已用异步问题征询范围并给出组Prompt的任务解释，等待合理时间后明确以四类继续；截至收尾未收到该问题回复，这一细分是当前设计假设，不记成摄影师已单独确认四类。首期生成要求本身来自owner，替代此前生产生成后置范围；3D/完整策划/CRM仍后续。
- 新增node-generation.md，需求v0.4增加CAN-09/10/11和FLOW-10。metadata/data/status/prompt分开，状态进度不增加内容版本；组Prompt关联Harness真实run。任务/输出版本/不可变内容修订分开，预览不改变采用，历史长期保留，复用参数重新校验输入。UI和Agent共用生成与版本命令，Gateway统一任务/费用，媒体Worker负责安全取件与持久入库。
- 新增FND-13依赖04/05/06，08依赖13，原项ID与历史折叠不变。04交付基础DTO/schema/命令，13在其上交付真实模型、Prompt面板与版本体验；10覆盖新增根；12覆盖25需求/10FLOW。旧43–69人日只属旧范围，须重估，不宣称覆盖新增成本。
- Fresh只读cs-review reviewer Tesla，ID 01a08927-176c-74a2-ba7d-29b49fb8de3b；遵宿主模型限制继承父模型，未发现异构provider，未自行指定override。共三轮终态：R1 0 blocking/3 important（组任务关联、独立生成启停、FND04责任）；R2旧项全resolved、新增1 important（停生成不应禁人工版本操作）；R3旧项全resolved、新增1 important（历史登记与自动采用共用了目标读集门槛）。没有第四轮。
- R3后完成针对性修订和自检：历史登记与自动采用分别守卫；资格有效但目标/来源版本改变时仍登记未采用历史，采用冲突不回滚历史；取消/停用/删除/超期等资格失效只保留execution候选。回执记录实际登记与采用效果，Agent归组但未改内容不推进内容读集。最终候选尚未取得独立PASS，不把自检写成审查通过；该修订的并发/生命周期实证与独立复核留到FND-04/13契约实现前。
- Verify图谱project Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign，generation2026-09-04T15:47:38Z；16候选coverage为not_tracked，采用直接读取。无代码调用链或图谱完备断言。文档链接/围栏/空白/表结构、25需求/10FLOW映射、13项FND无环通过；原先导Epic/审计/历史折叠hash未变。未改生产代码或运行供应商调用，真实生成/schema/竞态测试仍未实施，未提交。
- 最终自检清单 `/tmp/creative-node-generation-final.json` SHA-256 `ddfb23e5615076836e78e32d8d4b59c0a9fe60d4f353ec16da0320956dbee2db`；独立R3清单保留 `/tmp/creative-node-generation-review.json`，最终16文件如下：

  - `.codestable/requirements/creative-canvas-foundation.md`：`dfc990f3481c1306a9c338cb8374e71c2bdec742fa726a90e322c9091d34f571`
  - `.codestable/epics/creative-workspace-system.md`：`b080396d6c001eb834087ba0418542b63a34cea21dbf6fb52662bc8c9b9cda97`
  - `docs/product/creative-canvas-system/architecture.md`：`22313c40677e43863458a06945d9781d058629315b19cb7d7d37a556d7931e7c`
  - `docs/product/creative-canvas-system/llm-gateway.md`：`26abadaf7336538f94be046f3f85c7bc25510f0e19a13e5a29573205498e7fa6`
  - `docs/product/creative-canvas-system/acceptance-plan.md`：`9c29139767e6fb1a95d7fd300f5621c37937b3e304bc1ce5e5047cc8c9f0f1c7`
  - `docs/product/creative-canvas-system/delivery-plan.md`：`c3ca7fe8a118c94a58692f9b1903e093e8e20b4fae37430a3e4b9272a2b1b790`
  - `docs/product/creative-canvas-system/migration-release.md`：`073f5949289dccd1a4fb35cd69022eaa2d94217810ae8cfe8d51544d80111ae5`
  - `docs/product/creative-canvas-system/canvas-tooling.md`：`e92ff853832b29bb5f2344498785384fb16a0fccf80c75511b0d2c3fd9b0217f`
  - `docs/product/creative-canvas-system/data-model.md`：`a19c8eb25d2bfadc4fc4c16ccf5009a142b8e2be0ecd9eea874364f47e9f582f`
  - `docs/product/creative-canvas-system/domain-design.md`：`fb15be47ca346c8a77d914690033c9c5a3810cf2be4c684879ad976d45ef71ce`
  - `docs/product/creative-canvas-system/modules/content.md`：`29b22c0daa115f5e349051463a48064addafb935c8e558115ef16c14251902a1`
  - `docs/product/creative-canvas-system/modules/canvas.md`：`6716225c9a7cc97792d645d72be3e4c415e48fec14987308889c57643f6c8cc0`
  - `docs/product/creative-canvas-system/modules/README.md`：`fe2352c2d94b1094d105a21ad813b0dbebc2b32404bccec64ae9ec3e3d91e8b7`
  - `docs/product/creative-canvas-system/modules/harness.md`：`4e58e4282ded1291310ad622c86f456415067ffa0eb34caf988e0a74ebc53352`
  - `docs/product/creative-canvas-system/modules/media.md`：`f51ebba225fcc00c661374f245a0c4b66f7fb02d9f418fd9e19067810ea315c4`
  - `docs/product/creative-canvas-system/node-generation.md`：`ae30da59926e899eae270102af7b8158a522788103d7290139bc49fc41c5c3d9`

## 2026-09-10 开始正式开发

- owner认可整体设计并明确要求开始推进开发，当前Epic转active，批准hash见frontmatter。沿用已授权创意空间worktree，从FND-01开始实施；提交与发布继续manual，不继承历史提交许可。按当前项交付保留可检查改动，不为选择工作树或重复批准已有范围再次暂停。
- 上轮R3后的历史登记/自动采用分离已修订并自检；其实际并发与独立核验由FND-04/13覆盖，不声称历史审查取得PASS，也不追加第四轮设计审查。当前FND-01的公共事务/队列不依赖该细节。
- [x] FND-01 工程底座与适配验证（实现与验证完成，未提交）

### FND-01 交付证据

- 正式代码：platform/creativeops的身份/版本/严格JSON、规范化命令与回执、Eino具名能力适配；store账号barrier与0037能力/回执两表；platform/jobs封闭同事务入队与独立Worker/迁移检查；OpenAPI两条认证GET接口及Go/TS生成物；ReactFlow/Zustand开发验证页。详细入口见docs/dev/creative-foundation.md。
- 新能力默认关闭，capabilities返回foundation_only与空目录；生产Worker尚无真实业务处理器，默认启动明确拒绝、-migrate/-check可用。没有伪造内容/生成任务，不提前宣称FND-02/05/07/13完成。未连接或迁移本地真实业务数据库；数据库与队列验证仅使用storetest隔离实例。
- 依赖：保持Go1.25.5，River/riverpgxv5 v0.40.0、Eino v0.9.19、ReactFlow12.11.6、Zustand5.0.15。River最新版Go1.26要求由模块元信息实查；Context7查River/ReactFlow后对安装源码签名复核。旧Eino探针因新增River需更新实验锁文件，显式refresh后8项重新通过3.410s；不是更换框架版本或供应商。
- 新测试：8个creativeops测试（含同key并发/异hash/账号隔离/撤销写许可后回放、回滚/到期、Eino共用回执、业务行与job行xmin相同、两次投递一次效果、真COMMIT后丢确认、BIGINT与header约束、歧义JSON）；HTTP3项、队列迁移就绪1项。新增前端3项进入自动glob，总341项通过；浏览器手势/父子移动/toolbar/固定Handle/200节点300边/390px/无API请求通过，截图已实看。
- 验证：最终make check-go（build、golangci-lint 0 issues、全包测试）通过；make check-frontend通过，保留原AccountCenterContext两条Fast Refresh警告和原bundle大小提示；make generate-check通过；新包race通过6.432s；git diff --check通过。新迁移导致旧down测试漏退一步，已补0037并保留目标迁移原行为断言。未运行合并/发布门禁make check，因为本轮没有合并或发布。
- FND-01技术基准：1440×900 Chromium，200节点300边3秒样本p95 9.2ms，页面提供10分钟采样和环境记录下载；这不是FND-12的完整交互/目标部署性能验收。独立Vite服务127.0.0.1:5174/creative-lab.html（会话73899）供查看，原5173/8080/8770服务未重启；已验证开发HTML/文案不进入正式dist构建。
- 本轮fresh change reviewer Boyle（01a0895d-a4ac-7003-a6ab-c3391abd7593），按cs-review只读，遵宿主继承模型限制，无异构provider。R1 1 blocking+2 important：hash/实际JSON语义差异、HTTP数字float64损失、部分River迁移误报ready。全部先复现失败，再补严格递归键检查/统一规范化输入、UseNumber、完整Validate；红绿日志均保存。R2 PASS，全部resolved，无新增blocking/important/nit；共两轮，没有沿用上一阶段设计审查轮次。
- 图谱Verify：project Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign；generation2026-09-04T15:47:38Z。ScopeFor搜索3项分页完整、精确snippet、both depth1 9caller/0callee；新文件not_tracked及老文件metadata_changed均直接读取当前源码，不将旧图谱当完备调用图。47候选及scope/scope_tx已查coverage。
- 现有npm audit 7项high依赖版本与HEAD相同，未由新依赖引入、未跨范围升级；详见/tmp/creative-foundation-npm-audit.json。文档中原型/生产边界与正式业务尚未实现的限制保持明确。
- 日志：/tmp/creative-foundation-go-check.log、frontend-check.log（同creative-foundation前缀）、race.log、eino-probe.log、review-red.log、review-green.log；浏览器/tmp/creative-foundation-qa。以上本机临时证据不是生产验收或永久监测。
- R2冻结清单 `/tmp/creative-foundation-review.json` SHA-256 `c481dedcdf82c5298aff23820f19e002dc8890c4a49bcb07f95b9897c436f4fb`，47文件hash如下（当前Epic批准hash与HEAD未改变）：

  - `DESIGN.md`：`c067be534de0b52a8ae8506dd53cc1739f4a2d23798c653e6829236bf76d8d72`
  - `api/openapi.yaml`：`c4cee58c2eecc00c5000c6725251c05325dcc5f546cc321aef151b97003e8aed`
  - `backend/cmd/creative-worker/main.go`：`e8f6693b4ef7bf75877fc3d8132add9ba5d8fd671ef7553dd898edd46b4b9687`
  - `backend/go.mod`：`6b2385f0c6b2828202cab628e08ff1195d2f654b00debbaa98e812fec517b0ba`
  - `backend/go.sum`：`75f1beeed921df7dd4bdc499bd59f04735a2dc8e88c13095d3271e28d52a8c94`
  - `backend/internal/platform/creativeops/catalog.go`：`d87b5d776fd4f84e22051f23f0b01f40c9dba019652ef175f85980037079c396`
  - `backend/internal/platform/creativeops/commit_unknown_test.go`：`5f2bd6407f8c5a57920c620def6fd030e51c07fa8d6b824dbc5fd916013c288f`
  - `backend/internal/platform/creativeops/executor.go`：`c075f12fbe3f019889b8ca92aac81641ba7e4cc74cf4371674459edbe063f1c0`
  - `backend/internal/platform/creativeops/executor_test.go`：`3a7c718ee3ee384ab75eb0ea5f4aa4e73e21526ec36de08bc3df7ab2da7876d5`
  - `backend/internal/platform/creativeops/types.go`：`592a8327bb6745ff0e6d625a07198a85ad13ec222511dc9d36314acb912ec6e9`
  - `backend/internal/platform/httpapi/api.gen.go`：`b74a5f0827545a3f101b4631c1e9cad689953982ff7a4e7a1d91477a20ae0e29`
  - `backend/internal/platform/httpapi/creative_foundation.go`：`803555027da0a99281e2f678fe384efe570558aaf0d88b50d4d60dcccab4d759`
  - `backend/internal/platform/httpapi/creative_foundation_numbers_test.go`：`f1c202439a47ef7a4bdfe9cfdd988422c4b0fadfacb115ff22644cace7893902`
  - `backend/internal/platform/httpapi/creative_foundation_test.go`：`fa8e5bd9aa2c14c523d66a6c893bc185d5322f7b0f4abf61f9889fd592c76890`
  - `backend/internal/platform/httpapi/router.go`：`c925ecaae6bd328dd4479f9c7eeb54f6b34d13bcc187bf38bc1fbcc7c8df0ef3`
  - `backend/internal/platform/jobs/jobs.go`：`7d2832f0efb252a66774070f4c54e2ba6f77a65146055728e31e553948573e92`
  - `backend/internal/platform/store/auth_legacy_cutover_harness_test.go`：`9513c2ce66dce59023960165a354351f449ce8cfd4ed4282efa868b622a1830e`
  - `backend/internal/platform/store/auth_limiter_test.go`：`54b303d2c7344ae4bb18a6cb17d17a2ca19811b87f341c9fe28d81545d0e64b9`
  - `backend/internal/platform/store/auth_migration_test.go`：`6fecdfa588111451f543e26e0b3808bb718755ab656be13bac0f58d2204b3fb4`
  - `backend/internal/platform/store/creative_foundation.go`：`b3eca4aaf020c40557b8511fe2191a383711b460380cc9f7a91e5ca329f5a35d`
  - `backend/internal/platform/store/creative_jobs.go`：`1fa0ba01f0120ebb239bdbbb84ca0bf59ba5fb4800673205d5d8b5a023209add`
  - `backend/internal/platform/store/creative_jobs_test.go`：`f6e23dd77a827a0b4ec585f072d22978bad8b5107fd2e3f5df9f92ba709ce8ba`
  - `backend/internal/platform/store/migrations/0037_creative_foundation.down.sql`：`36d042190f1f7a6c94c7ee3ce6e93c8e780c4d1c87eca29db3f71201faf07411`
  - `backend/internal/platform/store/migrations/0037_creative_foundation.up.sql`：`cfac27519763b3dd4ee0c0faf163c2bd3fd5a8ba0098535ee2095a23e3588dc4`
  - `backend/internal/platform/store/orders_attribution_snapshot_migration_test.go`：`2fd8b85631b2f6fcccc7d47ba590e5eb4136e91985cd253ae02994db6b954334`
  - `backend/internal/platform/store/orders_delivery_due_migration_test.go`：`d9c08fcd26f65aeb3b94a6542ffd072c67bd075d78059156b494df07c5166cb9`
  - `backend/internal/platform/store/orders_payment_facts_migration_test.go`：`951cca44372e75f8d656a1f0008f2ef36f72890aaedce4f9a3f7a3a5c47cd4a6`
  - `backend/internal/platform/store/planning_migration_test.go`：`53eb91eb2502be23bb23d1d62f8845e9014918ed496c4c3196ada138cbebcda2`
  - `backend/internal/platform/store/planning_share_migration_contract_test.go`：`fd3da589733bf46d81de4e08125674ddae76b7575ee5a0f28bb681510c21324e`
  - `backend/internal/platform/store/settings_availability_migration_test.go`：`fbe40bc3b7cea44ffe9b7bd1492833f91b65bc117e9d32f82d7da03bfdb8c7d1`
  - `backend/internal/platform/store/settings_health_tiers_migration_test.go`：`11438dd58e218a19af949a7acf9130d75ac06828955b14f90a1363b123db6c34`
  - `backend/internal/platform/store/store_test.go`：`2a129b20626a8270c9cdba48b31cb9ed1ada4b1d783933cfff10c5d51cdfaec7`
  - `backend/internal/platform/store/telegram_digest_migration_test.go`：`1138b29993571717c57fdde9d1c2af3ea593ef7db2893d196852669aa509e944`
  - `backend/oapi-codegen.yaml`：`af609accf07847e6c3dc6b624c39e39c1da35316f44fbd0a87aabdd8d8aa1087`
  - `docs/dev/creative-foundation.md`：`b7c8f1fae11617798afd69c2deb0d9c36fb46f9b0a30a8ea987a8c045b4d8234`
  - `docs/product/creative-canvas-system/probes/eino/probe.mod`：`e1ebb48b7411b5b76a556f38a97a472933144edf187ea5f525363c7b52467c1e`
  - `docs/product/creative-canvas-system/probes/eino/probe.sum`：`3c16b510d6aee908eb3647921de35b6b1c3d41cec8939ed788801455605194d2`
  - `frontend/creative-lab.html`：`a51dee776c561a2ad1ca6b037aaeef6e2d89f0ed141113151e3ad82b1b018eb8`
  - `frontend/package-lock.json`：`c04d4de9eefd6028d34b019bdb9e08d8c38c0643fd69e4790105a236e1aa7b88`
  - `frontend/package.json`：`e6ac495187569a84ccb3abb7e7919fea8db49d8c861ae687082b331467725eed`
  - `frontend/scripts/creative-canvas-foundation.e2e.mjs`：`bfcc70db25d7fdf609c5e886088d16073716112670e5a1707dd02e660cc40892`
  - `frontend/scripts/creative-canvas-foundation.test.ts`：`d4eef6572948f35920bc7969ca7db728dfcd74e8d931706e29fc931a7112e3da`
  - `frontend/src/api/schema.d.ts`：`3315fb0e1ed7ba02261f334426f36c7a7ee3f9219e609a954cdd08919f1cca4b`
  - `frontend/src/creative-canvas/lab/CanvasLab.tsx`：`3ddd17e0e6edd3ac1d4a84ab3c3fdad8265157a96b5f9ecbde9ea978e46ca743`
  - `frontend/src/creative-canvas/lab/main.tsx`：`0e409253d75b7536e0628d1cad09df0af54c313103d8e697816fcb15b8ef1d06`
  - `frontend/src/creative-canvas/lab/state.ts`：`2147379aea5619f32003d4de8e5667331c1fc778113201091a968464acebeecf`
  - `frontend/src/creative-canvas/lab/style.css`：`41574d90216974edca9bd6b5ab306cdc074480588f4b88c0d58af8d941d0d44d`

### FND-01 滚轮交互微调

- owner要求默认滚轮上下滚动，Ctrl（Windows）/Command（Mac）加滚轮才缩放。验证页用ReactFlow panOnScroll/Vertical、zoomOnScroll=false及Control/Meta修饰键实现；同步页内提示与开发说明。此规则沿后续正式画布使用。
- Context7及已安装12.11.6源码核对配置；目标三路径coverage为not_tracked，直接读源。扩充现有浏览器验证：普通滚轮只改Y不改scale，两种修饰键分别缩放、释放后恢复平移；原框选/中键/空格/组移动/toolbar/固定端点/窄屏回归通过。缩放测试用小delta避免先碰最大缩放上限，采样结果等待实际渲染后断言。
- make check-frontend通过（341测试），浏览器e2e通过，git diff --check通过。未改后端、未提交；此前R2代码审查hash保留历史证据，不冒称覆盖这次局部UI变更。

## FND-02 开发启动

- owner明确允许提交当前代码并启动下一项，已提交43fbc24（包含FND-01、滚轮微调及此前创意规划/原型），未push。该授权不沿用到FND-02提交，继续manual。
- [x] FND-02 第一个可保存的文字创作闭环（完成）

### FND-02 第一内容切片证据

- 新增creativecontent的text/link类型校验、Create/Fork/Append与WriteAndRetain、当前显示用途读取守卫；首次节点编辑分叉，自己后续编辑追加，不广播修改其他使用方。复用planningmedia纯显示权利矩阵，来源声明由输入明确提供，不自动授予AI/生成用途；派生继承原声明。链接只存URL/元信息，不抓取远程资源。
- 0038建立本项实际所需的9张内容/声明/授权/修订/资产/库根/项目/画布/节点基础表，不含媒体或Agent表；新表全部显式account/key、无FK。last_sequence在内容身份中持久递增，删除历史最高修订也不复用序号；有引用才提交的守卫核对revision/content/kind配对。旧迁移down清单增加0038一级并保留既有断言。
- 5项真实PostgreSQL测试通过：无根回滚/跨账号/用途撤销与AI拒绝；两个画布中的节点共享素材→一处Fork/Append→其他节点和资产仍指原修订；链接无请求、危险协议拒绝；错误内容配对不构成根；清理未引用高版本后序号继续递增。make check-go全量build/lint/测试通过；增加第五项序号测试后单包再次通过3.008s。日志/tmp/creative-fnd02-content-check.log、/tmp/creative-fnd02-content-targeted.log，git diff --check通过。
- Fresh只读cs-review：Meitner，ID 01a08996-e37e-7de0-aac8-a419a0d7548a。单轮PASS，blocking/important/nit均0，首尾16/16 hash匹配；仅审本内容切片，不代表完整FND-02通过。遵宿主继承模型限制，无异构provider，未派生或提交。
- 图谱Verify当前root已确认ready；WriteAndRetain未命中、generation仍2026-09-04T15:47:38Z，候选新文件not_tracked/旧测试metadata_changed，matrix metadata_match，均以直接源码核对为证，不作全图或调用链完备断言。
- 未完成且保留原目标：creativelibrary/creativecanvas实际应用命令、项目归档恢复、API/生成DTO、账号准入路径、真实前端与跨窗口/回执/草稿验证、引用一致性扫描，以及FND-02浏览器完整验收。当前只读写隔离测试数据库，没有对真实账号启用新writer、执行0038或自动提交新代码。
- 冻结清单 `/tmp/creative-fnd02-content-review.json` SHA-256 `60448691428870c69d17e44a3656f0bc0301f41870cbc8f36a65c7f578e7f090`，基线HEAD `43fbc244259b353f323f5fb593e4b64f3affe231`，16文件：

  - `backend/internal/creativecontent/content.go`：`4588ca2e7d59f94ad342817d0ec1fab789a51cb9ae3f82c29f95ddbf299ec062`
  - `backend/internal/creativecontent/content_test.go`：`a0111f56d66f58d0094238195d42b9ebb9ce9b983d493b3de902ffe73aeb7afa`
  - `backend/internal/platform/store/auth_legacy_cutover_harness_test.go`：`652935e327989d10f04720acebc46fd6ddf897a40fecc7368d0900487e578142`
  - `backend/internal/platform/store/auth_limiter_test.go`：`7f8b0957b39ae2d30c55a0a262433a0668b2240c6177a6494db1b6ddad2a9b71`
  - `backend/internal/platform/store/auth_migration_test.go`：`962d051885bffe00b61d23ce67f6e82eeb2f7cfeb796367e9a4b255556e8084e`
  - `backend/internal/platform/store/migrations/0038_creative_text_canvas.down.sql`：`fba457088d55aa368dc80892f214c7ab5f78eb93fdbe9017a4c203e2b6e47db4`
  - `backend/internal/platform/store/migrations/0038_creative_text_canvas.up.sql`：`5756982d30e1a9ecf86eb93179a1eee94a90b1708283851e3b06737e694cb8b7`
  - `backend/internal/platform/store/orders_attribution_snapshot_migration_test.go`：`c2a52c8407cee29cd8d9e4224def76b4cc342524e3d041c5deac2e4fb7dc42b0`
  - `backend/internal/platform/store/orders_delivery_due_migration_test.go`：`41cf93cf699259b1c5ac6e86253c01f49bb4c6db868951f7416d5769185938fd`
  - `backend/internal/platform/store/orders_payment_facts_migration_test.go`：`f85dfcb6274cf265eac378041fcb71de417f0d4d7f43b5a5dab9ccc610193e15`
  - `backend/internal/platform/store/planning_migration_test.go`：`1169e47c257e7e6adbdd4e87bea4218df934265f181a2dd6a7bee0aec3be46df`
  - `backend/internal/platform/store/planning_share_migration_contract_test.go`：`ccc3fc09def1f07a1df843d70df953b4c64e2484c6d4f10059313a3d4e05405c`
  - `backend/internal/platform/store/settings_availability_migration_test.go`：`f338b97c5502fc1fd0bd0a579e3e5305fd954d772a145d15b82b27259d85a511`
  - `backend/internal/platform/store/settings_health_tiers_migration_test.go`：`e90ae0a8b9136fe802d9714082af8f6c685983fae181ee1f5545a26565e60d2a`
  - `backend/internal/platform/store/store_test.go`：`5d618e77dff262bc5102bf03c47b40846a7c5fa3dab95b319a2f510c74071228`
  - `backend/internal/platform/store/telegram_digest_migration_test.go`：`1d3d8862eefe1f6d3d14587a9334bc70c39b88bda7a638ede3fabc030dffbd85`

### 2026-09-10 跨任务接续：FND-02 应用层与 API 切片

- 从任务 `01a079d8-b0e9-7bd3-9d54-0eeaf7e2f2d7` 接续，沿用 `.worktrees/creative-workspace-redesign`。现场 HEAD 为 `70203b6`（内容层已提交），保留并完成原先未跟踪的 creativecanvas/creativelibrary 代码；此前提交权限不沿用，本轮未提交、未推送。
- 项目及唯一默认画布创建、改名、归档/恢复和分页；个人资产库文字/链接创建、读取、分页/水位；节点新增/资产引用、首次内容、分叉/追加、独立布局移动和快照已接通。所有命令复用同事务的 creativeops 回执/账号能力屏障；根关系、版本和用途失败整体回滚。
- 新增 11 个 HTTP 方法，OpenAPI 同步生成 Go/TS DTO；1 MiB 请求边界，路径目标由服务端注入，header/body 操作标识一致。页面、账号准入和前端保存队列尚未接入，capabilities 仍为 foundation_only；未对真实账号开启 writer、运行迁移或写入业务数据库。本轮未新增迁移。
- 长文字快照/列表预览上限 2000 Unicode 字符且标记 truncated；完整正文按精确修订读取，陈旧修订已失去合法根时公开读取返回404，内部缺根仍是完整性错误。用途声明使用独立输入类型，仅三个当前字段、说明最多500字，不通过旧结构接受生成授权。
- 验证：make check-go 全量 build/lint/测试通过；make check-frontend build/lint/341测试通过；make generate-check 通过；git diff --check 通过。六项 creativecanvas 数据库用例覆盖双画布隔离/重放、归档并发、用途回滚、空节点、分页、长正文与陈旧读取；真实 HTTP 用例增加8种坐标缺失/null组合、无回执/无版本变化、缺失nullable字段、声明500/501/2000及额外授权字段、陈旧读取、非法header。日志 `/tmp/creative-fnd02-backend.log`、`/tmp/creative-fnd02-frontend.log`、`/tmp/creative-fnd02-http-final.log`、`/tmp/creative-fnd02-drift.log`。
- cs-feat 因持久化/并发/账号边界触发独立审查；宿主 fresh reviewer `/root/review_fnd02_api`，明确 gpt-6-astra/high，无可用异构provider配置。R1：1 blocking（缺失/null坐标被解码为0）、2 important（声明契约、陈旧修订500）；R2：全部resolved，0新问题，PASS。未开第三轮。本结论仅针对应用/API切片，不代表完整FND-02。
- 图谱 Verify：原 worktree 项目 ready，generation 2026-09-04T15:47:38Z。相关符号未命中，新路径not_tracked、已有router/生成契约metadata_changed，逐路径coverage后全部直接源码fallback，不作图谱完备声明。
- 已将接口边界、恢复规则和后续工作归入 [开发说明](../../docs/dev/creative-text-canvas.md)。尚待正式前端入口、账号准入、每账号/画布/标签页草稿与命令恢复、跨窗口冲突及浏览器双画布完整验收、引用一致性检查；FND-02保持进行中。
- R2冻结清单 `/tmp/creative-fnd02-api-review-r2.json` SHA-256 `b047a769b048a2c6a2868afa636bdd5d0db0c8a45f523077e2429219738dfec9`，14代码/契约文件如下；本节及开发说明为审查后进度记录，不冒充已在冻结目标内：
  - `backend/internal/creativecanvas/projects.go`：`c26985178529ccafe795ddc2ee610984acde11af14e6cb3fd56e9d84e675adf7`
  - `backend/internal/creativecanvas/service.go`：`b7e11329fc0d45d323512ade0fc9ad3a245938780cf1c303a0e8db2442a615c8`
  - `backend/internal/creativecanvas/service_test.go`：`d9a93d82aacbc7b43d41b51b26513143fb7f5fa3e87b29246063359f27676ee5`
  - `backend/internal/creativelibrary/list.go`：`153ecc9f27e4250b02441c41aa79138346c2791a1a253253c16a563032e4a5d0`
  - `backend/internal/creativelibrary/service.go`：`6c7a7ad438091ac1470ac9f1ea170ec05af43dd05abb2fbfd4f55eb8d0a073ad`
  - `backend/internal/platform/creativeops/page.go`：`f6d98f4380fb508ae37ab9f005d389771c89da3c87d4478f81916a7bf69896ab`
  - `backend/internal/platform/httpapi/creative_canvas.go`：`c1c66d6445465918e5b7437732b527d321b0e0807cd324fab2f08bedb01dc353`
  - `backend/internal/platform/httpapi/creative_canvas_test.go`：`efdcdd099d381a83cd12959c446108220db2ae241062575044108165dda0bad3`
  - `api/openapi.yaml`：`a8b763e3288a49745cc780f68233a736c9a2571dc72f4c180a25f723f26f5e25`
  - `backend/internal/creativecontent/content.go`：`fa8fa4c0532ffa10e9cf18bbd5c12218fefeeddbfe17f1d525bee9ff48cb4640`
  - `backend/internal/creativecontent/content_test.go`：`dab72ca9aa4a1ecea62e2896274d5417a81d2d8359797689c82987f9849795a6`
  - `backend/internal/platform/httpapi/api.gen.go`：`05973217f266ee79a96243f0d4a491ee1900fcea8c2464b7e3fe7557b220be6b`
  - `backend/internal/platform/httpapi/router.go`：`09e79aa7120f8f72ef22d7fbe303303e9514bbb057ef90a0da6fe23bec754c51`
  - `frontend/src/api/schema.d.ts`：`e82fff9e7ced59a0be863e9bc6acae49b3cb4da89879eeef64b91b6c13f9b385`

### FND-02 正式创作闭环收尾（2026-09-10）

- 接续同一工作树，新增 `/creative` 正式入口及个人库、项目/默认画布、文字/链接节点、正文与位置编辑、改名/归档/恢复。入口按真实账号能力显示，React Flow 延迟加载；未开放媒体/Agent，未对真实业务账号执行准入命令。
- IndexedDB 保存账号/标签页草稿及完整命令，节点草稿再按画布/节点分隔；未知回执恢复沿用原 body/key。Web Lock 生命周期关闭并排空本机写入，旧网络回执不覆盖新会话。冲突展示版本后确认，保留新草稿。
- 受信任 `creative-access --mode read|write` 与只读 `--check` 已实现，含账号范围引用一致性检查；沿用0038，无新增迁移、无真实业务库写入。操作说明见 `../../docs/dev/creative-text-canvas.md`。
- 同一独立 reviewer `/root/review_fnd02_editor` 完成三轮：R1 两项 blocking（三项 important）分别为旧队列晚回执、失败导航写旧画布、认证续期、同版本权限投影、非法测试账号状态；R2 上述全部 resolved，新增离线轮询失败禁用本机输入；R3 全部 resolved，PASS，无新增 blocking/important。R2 曾因额度中断，恢复的是同一轮/同一reviewer，不将中断算通过。
- R3 冻结39文件清单 `/tmp/creative-fnd02-editor-review-r3.json`，SHA-256 `d4d97307e016975abc67480f88696e6d192aec855d8152020f693a6e1407391d`；reviewer首尾及主流程归档前全部hash一致。审查期间未修改工作树；本记录及最终开发文档验证说明在审查返回后追加，不冒充审查代码目标。
- `make check-frontend` 351/351 PASS（包含队列生命周期、令牌续期/账号切换与快照权限投影回归）；已有AccountCenter热更新warning与主包体积warning保留。`make generate-check` PASS；premium strict 0；Design.md lint 0 errors/0 warnings；`git diff --check` PASS。
- 真实临时PostgreSQL + 注册/验证/登录 + HTTP服务浏览器14场景PASS（末轮22.84s，日志 `/tmp/creative-editor-e2e-r3.log`）：无项目资产库、资产/节点丢回执恢复无重复、真实拖拽、双画布分叉、草稿重开、跨窗口冲突、位置按钮、归档恢复、链接不抓取、持续离线轮询失败后输入及重开、放弃确认、失败画布切换、390px布局。截图已实看，位于 `/tmp/creative-editor-qa/desktop.png` 与 `mobile.png`。
- **完整Go门禁未通过，不能宣称全绿。** build/lint通过，前两轮本次受影响包均通过，但全量先在package/digest、再在dashboard出现Docker容器启动期端口映射context deadline exceeded。单跑package三轮PASS/PASS/启动FAIL，digest三轮PASS，dashboard三轮PASS；inspect确认失败容器HostConfig要求随机5432映射而NetworkSettings为空。第三轮全量共享reaper启动失败，多个包在执行断言前报No such container，已停止无效重跑。日志 `/tmp/creative-editor-go-r2.log`、`/tmp/creative-editor-go-final.log`、`/tmp/creative-editor-go-final3.log`。未调整并发、超时、storetest或Docker配置；待环境恢复后补跑 `make check-go`，再结束FND-02验收。
- 图谱Verify仍为generation `2026-09-04T15:47:38Z`；新增文件not_tracked、变动文件metadata_changed，已按39文件覆盖报告直接读取源码核对，不作图谱完备性断言。未提交/发布，不自动推进per-item的下一项。

### FND-02 使用反馈：正常保存静默

- owner要求取消节点拖动/新增等操作的保存过程与成功通知。已移除正式创作台的“正在保存/确认中/已同步”状态和保存成功Toast；正常状态不渲染空提示栏。仅显示未提交正文草稿、离线、错误和需要人工恢复的未知/拒绝请求。保存队列、持久化与权限守卫未改。
- UX-CONTRACT同步该反馈策略。浏览器用MutationObserver覆盖整个操作过程，确认未出现这些通知，而非只检查保存结束时；14场景PASS，前端351项PASS，premium strict0，diff check通过。证据 `/tmp/creative-silent-browser-final.log`、`/tmp/creative-silent-frontend.log`；桌面截图已实看。仅前端呈现改动，无需重复全量Go门禁；此前Docker门禁限制仍待处理。未提交。

### owner方向修订：正式暗色工作台，取消试用与旧功能兼容

- owner明确项目尚未上线，旧创意空间可直接移除，不需要功能白名单或新旧兼容；随后再次明确不受旧CRM视觉约束，正式界面遵循v5暗色+液态玻璃，以成熟优雅的产品体验为目标。这是明确的新范围授权，沿用当前工作树；不涉及代码提交、发布、清空真实数据库或开放未实现的功能。
- 必须修改：正式创作台布局/主题与移动面板；主导航；旧页面/API/试用开关/旧writer切换代码；新画布的账号白名单；相关OpenAPI生成物、回归和使用文档。需要验证：正常账号无需开通、未认证/未激活仍拒绝、账号隔离和内容用途/归档/队列保持、独立拍摄策划正常工作、旧API为404、暗色桌面与窄屏。待调查项无。
- 旧creativeworkspace领域包、HTTP入口、前端页面及客户/订单旧空间组件已移除；拍摄策划作为独立业务保留，去除试用兼容只读锁。历史SQL迁移保持不可变，不执行清库或数据迁移。`creative-access`与CREATIVE_WORKSPACE_PILOT_ACCOUNTS已移除；产品支持能力只由服务端实现决定，正常active账号直接读取/人工写入，账号隔离和真实用途授权仍强制。
- 新视觉由editor/workspace.css统一拥有，原型v5为来源；全屏暗色画布、浮动玻璃工具/资产/编辑面板、绿色选中态、细描边与高光；保留静默保存。未给未实现Agent/媒体放置假按钮。DESIGN与UX-CONTRACT及开发文档已同步。
- 本轮执行 `make check-go` 一次完整通过，build/lint无问题；此前Docker阻塞已在本次运行解除。前端351项与第一轮14场景浏览器通过；补充移动面板回归捕获“收起编辑后误露出资产库”，修正为回到画布，正在最终回归。旧FND-02 R3仅覆盖此前目标，本次权限/退役调整由新独立审查阶段验收。
- 原冻结Epic中的试用白名单与旧域承接（包括FND-11旧迁移）的前提被本次owner决定替代；保留原批准文件作为历史，不按该旧兼容任务执行，不把未来其他FND项提前标完成。后续路线整理以本次决定为边界。

### 正式工作台改造最终验证

- 独立新阶段review使用宿主collaboration fresh `/root/review_studio_retirement`，gpt-6-astra/high；无可用异构provider工具，Paseo偏好文件不存在，按同构最强模型回退。R1无blocking、1important（手机新增资产入口缺失），已将唯一入口移入个人库标题栏；R2 PASS，无未解决或新增finding。移动缩放候选通过等待CSS过渡结束后测量排除，单节点约192px。
- 冻结目标75路径：R1 `/tmp/creative-redesign-review-r1.json` SHA256 `23faed2deb24aa06dc004e03f0d5b204c5e75507e5b7eabff467774e089d50ca`；R2 `/tmp/creative-redesign-review-r2.json` SHA256 `12a6c23ef83cb04c317921c8f48fe4acc39b9e8dbc6497e97b6133511b6f7eb5`。两轮首尾与主流程归档前hash一致；审查期间不编辑工作树。本段与开发文档验证记录在审查结束后追加。
- `make check-go`完整PASS（build、lint0、全包测试）；`make check-frontend`351/351PASS（保留已有AccountCenter热更新与主bundle体积warning）；`make generate-check`PASS；Design.md lint0errors/0warnings；premium strict0；diff check通过。日志在 `/tmp/creative-redesign-go.log`、`creative-redesign-frontend.log`、`creative-redesign-drift.log`。
- 浏览器16场景PASS `/tmp/creative-redesign-browser-r2.log`：真实新账号无开通记录、文字/链接、双画布隔离、拖放与节点保存、丢回执恢复、静默保存、离线草稿、冲突、归档恢复、失败导航，以及390px无项目时创建两类资产、手机面板开合与稳定视口。桌面、手机库/编辑/画布、冲突截图已实看，`/tmp/creative-editor-qa/`。
- graph Verify generation仍2026-09-04，75路径已查coverage；新增not_tracked、修改metadata_changed、删除missing，源码直接读取/搜索补足。`.env.example`已记录partial第32行是邮件sender说明，原文已读，本次只删试用env。无索引完备性断言。
- 本轮改动无需旧账号开通、无需迁移。保留历史SQL不会在运行时重新开启旧功能；未执行真实数据库删除、提交或发布。FND-02先前全量Go环境阻塞已由本次完整PASS关闭，后续FND仍按各自范围执行。

### owner反馈：创意空间一级首页

- owner提供Buzzy截图，明确创意空间需要一级页面展示已有项目与新建项目，未来作为相对独立产品入口。按现有暗色液态玻璃设计，不复制促销、模型或未实现Agent功能。
- `/creative`新增项目首页；`/creative/canvases/:canvasID`进入画布，`/creative/library`是无项目资产库。画布返回首页，项目创建和列表移出编辑器侧栏。AppShell对该路由子树使用沉浸布局；首页自身有项目、个人资产库和CRM返回导航。
- 首页真实列表/归档/分页来自现有API，项目创建复用SaveQueue和project-form草稿，不改数据库或后端协议。成功回到进行中列表，卡片点击进入画布；未知结果恢复原请求、草稿重开与会话锁保持。请求过期结果由abort/generation过滤。
- 验证覆盖首页无画布、首页↔画布、项目草稿刷新、丢回执不重复、归档筛选与手机首页；旧16场景随新层级改写并继续执行。新卡片为抽象项目封面，不虚构项目内容或真实生成缩略图。

### 一级首页最终验收

- 独立新阶段review `/root/review_creative_home`，宿主gpt-6-astra/high同构回退（无异构provider工具、Paseo偏好文件不存在）。R1一项blocking：创建/恢复完成落盘通知延迟到离页后，旧setParams可能抢回首页；已用页面代际与实际pathname双守卫修复，覆盖React过渡期间尚未卸载的窗口。R2 PASS，无未解决或新增finding。
- 冻结10路径：R1清单 `/tmp/creative-home-review-r1.json` SHA256 `547542abdaf783577a65cd1cc567b6b94fadd4f70bd0db55b09381373c4cb3ee`；R2 `/tmp/creative-home-review-r2.json` SHA256 `1732febaf8bd15f18f650e7b1fa185f16a43ada13bfa7b1f1f2b9e52f2760234`，首尾及主流程归档前一致。审查期间未修改工作树；此验收记录与开发文档在结果返回后追加。
- `make check-frontend`351项PASS；真实DB浏览器20场景PASS `/tmp/creative-home-browser-r2-final.log`，包括创建/恢复两条完成事件延迟切页路径。测试只延迟真实IndexedDB已提交写入的完成通知，不伪造业务成功；先红日志 `/tmp/creative-home-navigation-red.log` 与单代际不足的 `/tmp/creative-home-browser-r2.log`，完整双守卫后绿。首页项目卡片、草稿重开/未知恢复、归档切换、画布/资产库导航、390px首页全部验证。
- premium strict0、Design lint0errors/0warnings、diffcheck通过。未改后端/API/schema，不重复全量Go门禁；此前完整Go通过仍有效。截图 `/tmp/creative-editor-qa/home-desktop.png`、`home-mobile.png`、`home-empty.png`已实看；本轮不提交/发布。

2026-09-10 一键新建项目调整完成：默认「未命名项目」，创建/回执恢复后直接进入指定画布，沿用画布改名。352项前端测试、20个真实浏览器场景（含双击去重、回执丢失重开恢复、改名刷新及迟到回调不抢导航）、generate-check、premium strict和本地backend-build通过；桌面/手机截图已检查。未提交。

2026-09-10 拖动卡顿修复：移除位置保存对整页loading、拖动禁用与旧坐标重建的依赖；位置意图本机持久化、120ms合并、回执推进版本，保留原操作恢复/冲突守卫，跨回执拖动使用自身确认版本。355项前端测试、22个真库浏览器场景（慢网连续拖动/旧快照/丢回执后再拖及重开/离线重连）、generate-check、premium strict和backend-build通过。未提交。

### FND-02 提交授权与最终审查

2026-09-10 owner明确授权「提交，然后进入下一个feature开发」。本次提交范围包含FND-02正式文字/链接闭环、旧空间退役、独立项目首页、一键创建及前端优先拖动。最新并发增量独立审查由宿主gpt-6-astra/high执行（无异构provider，Paseo配置缺失），R1发现拖回旧坐标被no-op忽略；回归先红后绿，R2全目标PASS，0遗留问题。冻结manifest SHA256 7634e5b51594429b898356860dced1a09852a23f3eadc3704a49f84fbebffe0f。356项前端测试、此前22个真库浏览器场景通过，完整Go门禁已在退役阶段通过。未push；提交授权仅针对当前FND-02改动，FND-03仍按manual提交。

### FND-03 开发启动

依据已批准Epic FND-03及modules/library.md实现同一库模型的层级分组、多重归类、标签/分类、精确检索/分页水位及人工回收。旧账号试用和旧空间兼容不恢复。必须修改：库应用、正式迁移、OpenAPI/生成物、共享资产侧栏；必须验证：账号隔离、结构/批量竞争、草稿临时标签、purge引用保留、查询过期响应及10,000条基准。归属延续creativelibrary，内容根释放由资产行删除表达；物理GC与自动到期任务留FND-10。规范化使用同一固定Unicode实现，升级时显式重建，分页绑定算法版本与完整条件。


### FND-03 与 v5 对齐验收（2026-09-10）

- [x] FND-03 个人库整理与组合检索（实现、独立审查、验证完成，未提交）
- 完成层级分组/子组提升、多重归类、最爱/未归类、标签颜色与分类、资产表单内联标签原子创建、组合搜索/名称与时间排序/查询绑定分页/水位、批量整理与人工回收/恢复/逻辑purge、7/30/90/无限保留设置。共享项目侧栏和无项目个人库。自动过期与物理GC留FND-10，媒体与节点完整命令留各自后续项。
- owner指出正式UI偏离v5后，对照实际原型截图与canvas.css/asset-shelf.css重做结构：54px通高工具轨、318px可伸缩资产侧栏（720px展开）、目录树与双列文字卡、13px宋体节点、单击选中/双击模态编辑、跟随节点浮条、手动缩放控件。删除常驻右侧表单与重复顶部工具条；资产侧栏开合/拖宽不自动fitView。保留独立项目首页、一键未命名创建和静默前端优先同步。
- 明确文字草稿关闭保留本机，放弃才确认；共享useFocusTrap用栈保证只让顶层模态接管Tab/Escape及焦点归还。预览结果绑定asset ID+content revision，不可展示状态优先，不显示此前资产正文。
- 独立change review沿同一reviewer lineage、宿主gpt-6-astra/high执行（无合格异构provider，按cs-feat回退）。R1修复3 important：叠层焦点、批量回执ID/revision不匹配、撤销用途后详情丢整理关联；R2修复预览旧正文串位；均先有red再修复。R3完整目标PASS，无blocking/important遗留。R3 manifest /tmp/fnd03-review-r3.json，SHA eae787b263400c5fefbc4fe8b07d19064855b4a7b8426c616f2b770b4d76f2eb。审后只更新本执行记录，产品实现未改。
- 验证全部通过：make check-frontend（356单测、build、lint）；make generate-check；make check-go（build、golangci-lint 0问题、全Go测试）；premium strict 0问题；designmd lint 0错误/0警告。新增真实数据库测试覆盖并发规范化标签归并/层级冲突、跨账号游标、查询水位、撤销用途不泄露正文、批量原子性与回执、0039升级回填/重入/回滚保留旧资产。旧迁移回滚用例逐项加入0039步，不缩小测试范围。
- 最终浏览器TestCreativeEditorBrowser PASS 70.06s，日志 /tmp/fnd03-browser-green-retry.log；包含原23条流程和v5尺寸/视口稳定、嵌套确认焦点、不可用预览双击/Space断言，以及10k库40次输入到绘制采样。先前一次页面等待超时未用旧PASS替代；不改源代码重跑当前冻结版本后通过。Go一次idempotency容器启动超时也先单包核实，再独占跑完整gate通过（/tmp/fnd03-go-gate-clean.log），没有降低并发或豁免断言。
- 性能证据：当前仅文字/链接支持范围，10,001资产/100组/200标签，100混合领域查询首次390.44ms、p95 106.75ms（/tmp/fnd03-search-benchmark-final.log）；最终浏览器40次采样p95 655.13ms，包含300ms防抖、网络、DOM与绘制（/tmp/creative-editor-qa/library-search-performance.json）。不外推到尚未实现的完整媒体库。
- 桌面/窄屏截图及实测值：/tmp/creative-editor-qa/desktop.png、desktop-library-expanded.png、mobile-canvas.png、library-organized-desktop.png、library-organized-mobile.png、v5-metrics.json。原型对照：/tmp/v5-canvas-reference.png、/tmp/v5-library-reference.png。DESIGN.md与UX-CONTRACT.md记录正式映射，后续功能按v5继续，不回退CRM旧样式。
- FND-02提交为734bfff，本FND-03与本轮UI修正未提交、未push。下一项FND-04负责连线、布局分组、复制/解组、Undo/Redo、节点扩展与内部执行版本基础。当前无阻塞，保持manual提交和per-item推进。

### FND-04 开工（2026-09-10）

- owner明确授权提交已完成改动并继续下一feature；FND-03提交bccc558，未push。沿现有工作区开发。v5为视觉和交互依据，仅功能不合理处作有据局部调整。
- 归属creativecanvas：统一有界命令执行、布局/数据/Prompt/任务状态分离，服务端字段状态链支持连续撤销，不由前端整图回写。修改节点/边/输入/历史/执行/版本的正式存储、OpenAPI及编辑器。内容模块补齐显式引用守卫。需验证迁移、原子性、读集竞争、版本单调、任务取消/迟到、浏览器分组/连线/撤销与v5几何、拖动。外部模型和Agent仍属后续项。
- Verify图谱：project Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign，generation 2026-09-04T15:47:38Z；creativecanvas符号查询total0/has_more=false，6条候选路径not_tracked。已回退精确源码，不作图谱完备断言。

### FND-04 实现与独立审查（2026-09-11）

- 已实现正式图命令/状态链、嵌套分组与参考关系、批量本地拖动队列、撤销/重做、Prompt CAS与不可变版本、独立文档fixture、内部真实text-compose任务及条件发布。前端继续v5暗色/玻璃控件；普通保存静默。内部executor不在生产注册，真实模型生成/媒体及成品Prompt历史面板留在FND-05/13。
- 独立change review由宿主fresh `review_fnd04`（gpt-6-astra/high）执行cs-review；已发现可调用委派工具，无可用异构provider，Paseo配置不存在，显式同构回退。R1冻结patch SHA256 `a14d66f97fee4699a4d8df2d9fc4cf44ef74e9d7382b074b7047fec9b8356559`；报告`/tmp/fnd04-review-r1.md`提出4 blocking+1 important，未记为通过。
- 对应修复：关系槽位先释放再写最终图；移动回执包含no-op目标的真实版本，前端滤无变化手势；四边/四角缩放提交原点与尺寸，分组同时补偿直接子节点；输入按显式顺序合并去重；普通读集剔除永久墓碑，Undo用目标change的有界当前读集。输入合并语义与读集投影分别补入现有node-generation/canvas契约，没有新增lesson。
- 回归先红后绿：`/tmp/fnd04-review-regressions-red.log`复现唯一约束与缺no-op回执；`review_regressions_test.go`覆盖替换/Undo、混合no-op、输入执行/历史/复用顺序、5100旧身份、Prompt同槽替换与边重连。`/tmp/fnd04-r2-regressions.log`PASS；前端360/360通过；`/tmp/fnd04-r2-resize-actual.log`真实浏览器验证普通节点和分组的四边/四角、Undo/Redo及DOM尺寸还原，19.147s PASS。R2复审待完成。
- 性能证据：临时Go overlay在500活跃空文字节点下GetCanvas79.4ms，三次move事务66.4/69.0/67.6ms（`/tmp/fnd04-scale.log`）；不把数据库耗时当成前端帧率。旧浏览器含延迟保存/陈旧轮询期间持续拖动、断网及回执丢失恢复；R1完整双脚本PASS85.025s，R2修复后继续最终合跑。此前race两包PASS；严格UI静态审计0，DESIGN lint0。
- 0040仅在隔离测试数据库应用；用户的开发数据库、已有5173服务未迁移/重启。FND-04未提交、未push；先完成本项复审和验证，FND-05不在本轮并行启动。

- R2完整候选SHA256 `25444799f8f695fd0aa7267ed65193af7cb5b04aa92b38b09f4c01270c5b6a30`；同reviewer报告`/tmp/fnd04-review-r2.md`确认原前4项resolved，指出读集缩减漏掉复制/删除节点的活跃版本，剩1 blocking。R2全Go、frontend360、双browser104.118s、race两包、generate-check及premium严格审计通过，但未把这些门禁代替review结论。
- R3对应修复：所有活跃版本在快照中提供node_id归属及真实revision；普通命令保持不携带版本，复制闭包只带当前selected version，删除闭包带其活跃版本，Undo继续使用目标change读集。永久墓碑不回流普通命令，活跃版本不受近期100条change限制。新增101条后续操作后复制/删除旧版本测试，以及真实UI“填写正文→复制→再次复制副本→删除→撤销恢复”路径。`/tmp/fnd04-r3-domain.log`8.746s、`/tmp/fnd04-r3-frontend.log`361/361、`/tmp/fnd04-r3-browser.log`24.892s均PASS；最后一轮独立复审待完成。


### FND-04 完成（2026-09-11）

- R3同lineage独立审查PASS：`/tmp/fnd04-review-r3.md`，0未解决blocking/important、0新发现；完整冻结patch SHA256 `39069ea58d29844ada629c2733fc49f68a7fa6ede5c106c904bc88f0399ef841`。三轮审查已结束。审查后仅更新本游标与DESIGN的已实现状态，产品代码保持冻结候选一致。
- 最终`make check-go`明确exit0（build、lint0 issues、全包测试，`/tmp/fnd04-r3-go.log`）；`make check-frontend`361/361、build/lint通过；`make generate-check`通过；premium strict0；DESIGN lint0 errors/0 warnings；`git diff --check`通过。保留项目已有AccountCenter Fast Refresh与主bundle体积warning，不因本项扩大修复。
- 真实隔离浏览器：R3新版graph脚本24.892s PASS，覆盖正文编辑、全部缩放方向、分组/复制/连线/Undo/Redo、versioned副本再次复制/删除/恢复、手机与独立文档；R2双脚本104.118s PASS，包含原有离线/慢请求/回执未知/跨窗口及个人库完整回归。R2两包race12.592s/4.901s PASS；R3变化是版本只读投影与命令读集构造，新增领域/前端/浏览器回归及全Go门禁覆盖。当前截图`/tmp/creative-commands-qa/grouped-desktop.png`、`grouped-mobile.png`、`document-maximized.png`。
- 已将owner的v5优先约束记录到既有DESIGN.md，输入顺序与目标读集规则归入既有node-generation/canvas契约；未新建lesson。保留Epic工作游标供下一项延续。
- 本项就绪但未提交、未push；上一项提交仍为bccc558。开发数据库未应用0040，已有5173服务未改动。临时5176测试服务验收后关闭。下一项为FND-05，未在本轮越界启动。

### FND-04 画布交互纠偏（2026-09-11，owner新增反馈）

- 范围：修复节点加号弹性跟随与v5样式、点击菜单定位、触摸板双向平移；同轮补充节点主体落线和边缘中心吸附。保留原有FND-04暂存改动，新修复未提交；未启动FND-05。
- 根因：缺失原型磁吸逻辑；菜单复用了新节点预定坐标；PanOnScrollMode固定Vertical；连接仅依赖小端口命中且onConnectEnd忽略节点主体；端口背景继承CRM浅色--bg。图索引2026-09-04 generation对相关路径not_tracked，按精确源码回退核实。
- 修复：屏幕像素阻尼动画只写端口样式，不更新React/节点坐标；菜单使用点击屏幕坐标和实际尺寸避让；Free平移；预览与松手共享节点主体/18px邻近命中，同一左右边缘中心锚点用于预览和最终连线，支持反向连接和分组子节点；工作台补齐暗色--bg。吸附邻域使用已有节点几何，避免每次拖线对所有节点读取DOM布局。
- 红色证据：`/tmp/creative-port-red.log`同时复现加号跟随/样式、菜单和横向平移失败；`/tmp/creative-connection-red2.log`显示节点主体落线edges=0、snapped=false；`/tmp/creative-port-color-red.log`显示圆环底色误为CRM浅色oklch。
- 验收：`make check-frontend`361/361、build/lint通过（`/tmp/creative-interaction-frontend-final.log`）；generate-check通过；premium strict0及DESIGN lint0错误/0警告。完整真实隔离浏览器双脚本110.887s PASS（`/tmp/creative-interaction-browser-verified.log`），覆盖新交互、减少动态效果、键盘菜单、窄屏边界、正反向/邻近/缩放分组连线，以及原有延迟保存/陈旧轮询/断网/跨窗口路径。保留既有Fast Refresh和bundle大小warning。
- 旧E2E修正：双向平移生效后，旧脚本可能把节点移出视口仍直接拖动；补显式适应视图并将等待请求改为有超时的断言，避免无限等待。首次完整跑因该准备步骤缺陷停止，不计通过；上面的110.887s是修正后的完整重跑。
- 视觉证据：`/tmp/creative-commands-qa/port-menu-desktop.png`、`port-menu-mobile.png`、`connection-snap.png`；已实际查看。设计规则归入现有DESIGN.md，不新增lesson。此修复限定前端，不改后端契约/持久化/队列一致性，不触发新独立审查；此前R3结论仅属于原FND-04冻结候选。
- 开发数据库未变更；现有5173服务未改动。临时5176服务测试后关闭。新增修复保留未提交，下一项仍为FND-05。

### 连线松手延迟修复（2026-09-11）

- 根因：原先仅在服务端回执后刷新Canvas才显示正式边，预览结束与刷新之间存在空档。新增ConnectionOverlay只负责本地连线显示；复用现有job持久化与幂等恢复。同步投影、未知保留、拒绝回滚、成功回执到对应快照之间保留并去重，跨画布/已删除节点不串入；不改变串行调度或journal schema。纯connect batch适用，混合新增节点命令仍走原流程。
- 红色证据：`/tmp/connection-immediate-red-assertion.txt`在人为延迟请求后观察到0条边（应为1）。365项前端测试与构建/lint通过；generate-check通过；完整双浏览器脚本113.641s PASS。独立R1发现pending临时边选择ID可流入删除命令；已限制临时边选择/指针交互，并在点击、双击、键盘删除入口核验正式ID。
- R1报告`/tmp/connection-immediate-review-r1.md`，冻结SHA256 `52cf81652879d2cdfeddb06b66114c26b006195480f93123b79f55ae6ac56fea`。owner明确要求修复后不重复review，因此没有R2；发现由本机修复及回归闭环，不宣称独立复审通过。最终`/tmp/connection-immediate-frontend-closure.log`365/365通过；`/tmp/connection-immediate-browser-closure.log`31.886s PASS，涵盖延迟发送、期间点击临时边、确认后删除及撤销。此前完整回归见`/tmp/connection-immediate-browser-final.log`。premium strict0，DESIGN lint0。
- 验证过程中一次容器启动context deadline exceeded，未进入测试，不计通过；随后隔离重跑31.886s成功。早期临时边删除用例的焦点/选择准备步骤已修正。保留先前代码与本次改动，未提交、未push、未改开发数据库；临时5176测试服务关闭，既有5173保留。FND-05尚未启动。

### FND-04 提交里程碑（2026-09-11）

owner明确授权提交本阶段feature。本提交包含FND-04基础能力、v5交互纠偏及连线松手延迟修复；沿用已通过的对应验证和owner不重复复审的决定。未推送远端、未合并develop、未应用开发数据库迁移；FND-05等待后续开发指令。

### FND-05 开工（2026-09-11）

- 依据已批准Epic FND-05、modules/media.md、content.md §1-4、library.md §5、canvas.md §5 与 docs/dev/object-storage.md 新版契约实现。上一项FND-04已提交c18430a；本项沿同一工作区继续，遵循v5暗色/玻璃，普通保存静默。
- 归属：新建 `internal/creativemedia`（上传会话/状态机/StorageAdapter Local+OSS/校验/发布/候选/票据与read pin/Worker任务）；`creativecontent` 扩展 image/video/audio 修订与 content_objects 投影及候选根；`creativelibrary` 扩展 kind 过滤、`CreateFromRevisionInTx` 与「画布节点存入库」；`creativecanvas` 增加 core.image/video/audio 节点目录及 `ValidateNodeTargetInTx`/`BindNodeRevisionInTx` 端口；HTTP 新增 `/creative/uploads*`、`/upload-candidates*`、`/media-capabilities`、`/media-access-tickets`、`/media/{revision}/{role}`（票据鉴权，供浏览器媒体标签）、`/assets/from-canvas-node`；`cmd/server` 与 `cmd/creative-worker` 共同装配媒体 Worker（API 只注册不启动，Worker 启动）。
- 必须修改：迁移0041（contents/assets kind 扩展、blobs/content_objects/uploads/parts/candidates/read_pins/quotas 正式表）、OpenAPI 与两份生成物、config（`CREATIVE_MEDIA_LOCAL_ROOT`、`CREATIVE_FFPROBE`、`CREATIVE_MEDIA_QUOTA_BYTES`）、前端资产侧栏导入队列/媒体缩略图/预览、画布媒体节点/拖入文件/替换/最大化/下载/存回库、多选拖入。
- 需要验证：账号隔离；同事务入队与回滚；分片授权不能拿到 final key；假 MIME/超限/未知格式逐项失败不影响已成功项；complete/init 返回丢失可恢复且不重复发布；旧 epoch worker 不能写；目标变化转候选、adopt/discard/到期只能一方成功；ready 交接后 uploads.blob_id 清空；撤销用途后票据/读取拒绝；Range 206/416、HEAD、ETag；read pin 建立与续期；JPEG/PNG/WebP 逐项、WebM(VP9/Opus) 与 MP4(H.264/AAC) 视频、MP3/WAV 音频真实解码/播放；浏览器导入→拖入画布→预览/下载/替换→重开一致。
- 仍待调查：真实 OSS bucket 验收（PRE-05）需授权测试 bucket，本项只能完成适配器与 fake conformance，不标 OSS 真实验收通过；物理删除/到期 GC 留 FND-10；Chromium headless 对 H.264 的播放支持以实际样本为准，不支持则只用 WebM 作浏览器证据。
- 风险与保障：改 schema/迁移、并发/一致性语义与信任边界（票据、签名 URL）→ 定向 red→green 测试 + 一轮独立 change review（owner 既定：默认一轮，blocking 清零即关）。设计取舍：Local 分片 PUT 走鉴权 API 且带 HMAC 签名 query（与 OSS 预签名等价的临时能力），票据为 HMAC 无状态短期令牌（10 分钟），read pin 落表；图片在校验阶段生成 ≤1600px display 渲染件（原件仍保留），视频/音频用 ffprobe 只探测不转码。

### FND-05 实现、审查与验证（2026-09-11）

- 已实现：迁移0041（contents/assets kind扩展；blobs/content_objects/uploads/parts/candidates/read_pins/revision_holds/media_quotas）；`internal/creativemedia`（Local分片经API HMAC token、OSS预签名UploadPart+版本固定；状态机created→uploading→verifying→ready/failed/expired/cancelled+io_phase+execution_epoch/lease；Worker init/complete/verify/abort；mimetype嗅探+图片解码+ffprobe只探测；`publish_operation_id`下单份回执发布，目标变化转pending候选，adopt/discard/到期同行锁；HMAC无状态票据10分钟+read pin+精确版本读取；配额预留/结算/释放；SweepExpired只标记）。content：media kind、`WriteMediaAndRetain`、派生继承对象、候选/hold保留根。library：`ValidateImportTarget[InTx]`、`CreateFromRevisionInTx`、kind过滤。canvas：core.image/video/audio、`ValidateNodeTargetInTx`/`BindNodeRevisionInTx`（可撤销change）、`SaveNodeToLibrary`。HTTP新增uploads*/upload-candidates*/media-capabilities/media-access-tickets/media/{revision}/{role}（票据鉴权、单段Range 206/416、HEAD）/assets/from-canvas-node；server与creative-worker共享票据密钥，API只入队。前端：导入托盘/来源确认/媒体缩略图与节点（票据懒加载）/预览/下载/替换/说明编辑/存库/多选与文件拖入。文档：DESIGN、UX-CONTRACT、docs/dev/creative-media.md、object-storage、.env.example、api manifest。
- 独立change review：Codex两次因CLI不支持`gpt-6-astra`失败无报告（不计轮次），回退宿主同构fresh reviewer。R1目标SHA256 `df2224c59edb236b915c52dee6afcfc66a60784b911a7a99657e79dff9a13f6a`：建议先改再合，1 blocking（前端重试不触发）+5 important（发布中断无救援、媒体节点编辑入口矛盾、契约文字、Range双开、任务超时），全部修复并加red→green。R2（同reviewer follow-up）目标 `284c702abaccf5554898deae59be438a287d4574b9495210b40d597891de7459`：有条件可合，0 blocking、1 important（N1 verify/promote阶段租约60s不续期，长任务可能被并发claim误判死亡）。N1修复：租约按阶段预算（init/abort 2min、complete 5min、verify 20min）并以测试固定「租约≥任务超时」不变量；owner明确要求最多一轮审查，故N1修复不再复审，由本机测试与门禁闭环。residual：RenewPin无调用方、`UploadView.ContentRevisionID`恒nil、票据/分片token共用签名器以哨兵区分。
- 最终候选SHA256 `72475edd6b9d3440aeb475e3743dbacd03df3d31931421511a201b9512e98de2`（80 files, +9396/−194）。验证：`make check-go` exit0（40包、lint 0，`/tmp/fnd05-r3-go.log`）；`make check-frontend` 369/369、`make generate-check`、`git diff --check`通过（`/tmp/fnd05-r2-*.log`，R3只改后端）；`go test ./internal/creativemedia`覆盖7种格式真实校验、假MIME逐项失败、创建准入、候选/采用/放弃竞争、幂等重放、epoch/取消、票据/Range/撤销、发布中断救援与租约不变量；真实浏览器`creative-media.e2e.mjs` PASS（导入/假文件重试/筛选/预览/多选拖入/拖文件建节点/替换与撤销/说明编辑/放大/下载字节一致/HEAD-206-416-伪造角色403/视频播放Range/存回库/重开），既有两脚本回归PASS（`/tmp/fnd05-r2-browser-regress.log`）。截图 `/tmp/creative-media-qa/`。样本媒体入库 `internal/creativemedia/testdata`（WebM由Chromium MediaRecorder录制）。
- 未做：真实OSS bucket验收（PRE-05，需授权测试bucket，文档已标注）；物理删除/自动到期清理归FND-10。开发数据库未应用0041，5173服务未动，临时5176已关闭。

### FND-05 提交里程碑（2026-09-11）

owner授权提交：`126da5f`（80 files，pre-commit lint通过）。未push、未合并develop、未应用开发数据库迁移。下一项等待owner指令。

### FND-05 补丁：画布本机优先、媒体节点比例、去掉来源确认（2026-09-11）

- owner 反馈三点：媒体节点增删改/移动响应慢且每次操作都同步；节点不按媒体长宽比定尺寸；上传强制选来源没必要。owner 拍板 ContentForm 的来源选择一并去掉。
- 响应慢根因：除拖动位置外，所有画布命令走 `perform`，置 loading、等回执、再 `refresh()` 重拉画布与资产库，且队列一次只允许一个待确认操作。改动：journal 新增有序 `outbox`（`outbox.ts`：意图/投影/合并/读集；`queue.ts`：`stageIntent`/`cancelTail`/`sendNextIntent`/`pruneOutbox`，请求槽 `claim` 原子化，回执 `Receipt` 含 created/omitted）。画布编辑立即投影到 `view`（新增/删除/移动/缩放/标题/内容/连线/断开本机可画，分组/复制/版本等回执后拉快照），250ms 静默后按序发送；连续 move/resize 合并进未发出的队尾；撤销未发出的编辑本机取消并入 redo 栈；被拒绝的意图连同其后同画布意图撤回投影并禁用画布编辑直到人工放弃。读集取自「快照 + 该意图之前的回执」投影，不含本机节点与 `pending:` 边；本机节点的位置意图等创建回执。删除 `connectionOverlay.ts`（被投影取代）。UX-CONTRACT「每个会话最多一个未完成命令」改为「同一时刻一个在途请求」。
- 比例：服务端 `fitMediaSize`（`BindNodeRevisionInTx` 与 add_node asset 路径）：节点仍是默认 280×180 且非音频时高度 = 74 + 280×h/w，夹在 140–640；前端 `fitMediaHeight` 同规则用于资产拖入的本机投影。
- 来源：OpenAPI `rights` 在内容草稿/上传创建中改为可选；`RightsDeclarationInput.OrDefault` 缺省 `photographer_owned/ownership_attested`；`replace_content` 空节点不再要求 rights；前端删除 UploadDialog 与 ContentForm 来源下拉，选文件即上传。
- 验证：Go `creativecanvas/creativecontent/creativemedia/creativelibrary` 通过（新增 `TestFitMediaSize…`、`TestFirstContentWithoutRightsDefaultsToOwnWork`、上传省略 rights 用例），`check-go PKG=./internal/creativecanvas/...` 与 httpapi Creative 用例 exit 0，golangci 0 issues；前端 `check-frontend` 372/372、`generate-check`、`git diff --check` 通过；`creative-save-queue.test.ts` 新增 7 条 outbox 用例（投影/合并/本机取消/拒绝撤回/重载续发/本机节点位置延后/读集来自前序回执/比例）。真实浏览器三脚本联跑 PASS（138.8s，`/tmp/fix3-browser.log`）；媒体脚本新增节点尺寸断言（64×48 样本 → 284 高，音频 180）。e2e 驱动改为每脚本独立账号（此前共用账号导致资产计数串扰）。
- 遗留：`UX-CONTRACT`/`DESIGN`/`docs/dev/creative-text-canvas.md`（新增「本机 outbox」节）/`creative-media.md` 已更新；分组/复制/版本操作仍等快照，未做本机投影；未提交。
- 2026-09-11 追加：拖动位置合并进 outbox。删除 `positions`/`placementRevisions`/`stagePosition(s)`/`sendNextPosition`/`reconcilePositions`/`forgetPosition`/`withOwnPlacements` 及 `Job.position(s)`，新增 `SaveQueue.stageMoves`（选区根、落点不变不入队）；CanvasView 去掉 `positions` 覆盖层，投影本身承载本机坐标；旧 journal 未同步 positions 加载时转 move 意图。此前 owner 报告的频繁「内容已变化」409 根因即两条路径各记版本号，合并后消除；服务端 409 文案改为「内容已被其他窗口修改，本机这次修改未保存」。验证：`creative-save-queue.test.ts` 20 条（新增在途合并/回退落点/组根/本机节点顺序/旧 journal 迁移），前端 369/369、lint/build、diff --check，三脚本浏览器联跑 PASS 137.6s（text-canvas 的 move 计数改为识别 batch 内 move_node）。

- 2026-09-11 提交里程碑：`e7ce9e0`（38 files，pre-commit lint 通过），含 OSS 日志/CORS 文档批次、三问题修复与 outbox 统一。未 push、未合并 develop。

- 游标补记（2026-09-11）：`b694364`（fix(creative): 修复画布交互并接入 SSE 实时同步，47 files）已提交并 push 到 origin/feat/creative-workspace-redesign；其增量需求决策在 `.codestable/requirements/creative-shoot-planning.md`（画布节点类型收敛、画布同步补充决策），实现文档在 `docs/dev/creative-text-canvas.md` 各 2026-09-11 章节。本条只补 commit 指针，不复述其当轮验证。

### 视频节点悬停预览（2026-09-11，owner 反馈）

- owner 反馈画布视频节点交互：现状是点击播放、再次点击暂停，且视频表面无法拖动节点；要求改为悬停播放、离开暂停、可拖拽。随后 owner 纠正：不能去掉播放按钮和进度条，手动暂停仍可用，离开后再悬停继续播放。
- 实现（3 files，前端）：`MediaFigure` 新增 `hoverPlay` 参数（仅画布视频启用）——容器 `mouseenter` 调 `play()`（默认有声，`.catch` 兜底浏览器自动播放策略；静默失败时用户仍可手动点播放）、`mouseleave` 调 `pause()`；手动暂停后只要指针未离开节点就不会被覆盖，再次进入从暂停处续播。视频元素在 hover 模式去掉常驻 `nodrag`，节点可从视频表面拖动（拖动中视频继续播放）；原生控制条占视频底部约 48px，按压坐标落在该区域时仅为该次按压临时加 `nodrag` 类（`pointerup`/`pointercancel` 释放），进度条拖动与播放/暂停、音量操作命中控件而不是拖动节点。`CanvasView` 视频/音频仍传 `controls`，仅视频追加 `hoverPlay`；媒体预览弹窗与资产库用法不变，预览打开时照旧暂停画布视频。
- 验证：`make check-frontend` 381/381 通过，`git diff --check` 通过；`creative-canvas-interaction.e2e.mjs`（真实 Chromium、页内 MediaRecorder 录制 WebM、API 全 mock、临时 Vite 5177）连续两轮 PASS，新增断言：controls 属性存在、非静音、悬停后 `!paused && currentTime>0`、手动暂停后节点内移动仍暂停、离开暂停、再次进入续播、从视频表面拖动节点位移 >60px、控制条区域按压节点位移 <1px；既有交互回归全部保持。commands/media e2e 需真实后端与已迁移数据库（0041/0042 未应用于开发库），本轮未跑。
- 文档：`docs/dev/creative-text-canvas.md` 新增「视频节点悬停预览（2026-09-11）」节。
- 2026-09-11 提交里程碑：owner 授权提交 `36d3280`（5 files，pre-commit lint 通过）。未 push、未合并 develop。

### FND-06 开工（2026-09-11）

- 依据已批准 Epic FND-06、`docs/product/creative-canvas-system/modules/gateway.md`、`gateway-harness-data.md` §1/2/4/5、`gateway-harness-slices.md` GH-01 与 `eino-adoption.md` §1/8 实施。上一项 FND-05 及其补丁已提交（最新 `36d3280`）；owner 明确沿用当前 worktree 分支 `feat/creative-workspace-redesign`，不新开 worktree。
- **PRE-06 本轮满足**：owner 提供 OpenAI 兼容供应商 DeepSeek 的 API Key，经 `.env` 的 `DEEPSEEK_API_KEY` 注入（`.env` 已在 `.gitignore` 第 2 行、未被 git 跟踪、权限 600）。密钥只经环境变量读取，不入库、不入文档、不进日志/快照/测试固件。因此本项按「真实供应商通话」验收推进，不再只做桩。
- 供应商事实（Context7 `/websites/api-docs_deepseek` 核对）：OpenAI 兼容 `https://api.deepseek.com`；`usage` 含 `prompt_tokens`/`prompt_cache_hit_tokens`/`prompt_cache_miss_tokens`/`completion_tokens`/`completion_tokens_details.reasoning_tokens`，正好支撑 gateway.md §4 的「缓存输入 / 非缓存输入 / 输出」三不重叠分量，reasoning 已含于 output 不重复计；错误码 400/401/402/422/429/500/503；**无请求状态查询 API、无取消 API** → 目录记 `query=none`/`cancel=none`，unknown 只能「结束等待 + 费用待核实」。实际可用 model id 与价格以真实 `GET /models` 与官方计价页为准，不按文档快照猜测。
- 归属：新建 `internal/platform/llmgateway`（统一契约与 hash、模型目录、Catalog/Admission/Requests/Accounting 四端口、供应商适配、派发许可与 unknown 核实、预算/用量/成本位置核算、GatewayModelAdapter）；平台级 `platform_llm_limits` 按 `securitybudget` 先例在 `internal/platform/store` 实现（无账号业务表不进 AccountScope 端口）。Gateway 不 import creativecanvas/creativecontent/creativemedia，不新增 HTTP 路由（只从服务端使用，客户端入口属 GH-02/FND-07）。
- 必须修改：迁移 `0043`（llm_call_groups / llm_budgets / llm_usage_reservations / llm_requests / llm_attempts / llm_usage_measurements / llm_measurement_dispositions / llm_cost_positions / llm_token_positions / llm_settlement_receipts / llm_result_consumers + 平台 `platform_llm_limits`，全部显式 key、无外键、保留 PK/UNIQUE/CHECK/index）；config 增模型目录与凭证环境变量引用；`.env.example` 只记键名不记值；`docs/dev/` 增 Gateway 开发说明。
- 必须验证：两种供应商协议（OpenAI 兼容 + Anthropic Messages）产出同一完整结果契约；工具碎片重组/截断/重复/非法 JSON 不产生可消费结果；缺失 usage 保留 settlement=unknown 不阻塞读取已知结果；能力缺失派发前拒绝、不静默删字段；供应商返回 model 不在允许别名内判协议错误；SDK/HTTP 隐藏重试关闭（假供应商计请求次数）；预留幂等回放不重复占额、初始预留只能被一个请求认领；A 预留 20 → 可靠未受理释放 → B 占满 20 → A 重新准入必须失败、B 释放后 A 才能按原身份重建 hold；跨月分组上限不重置；并发成本更正只有一方从当前值推进、另一方挂 pending；实际超预留全额入账并阻止后续准入；取消先于派发无外发、派发后取消不得伪称无费用；COMMIT unknown 不重领许可；账号隔离；凭证不出现在任何持久载荷/日志/回执中。
- 真实通话验收：`GET /models` 确认部署别名，`chat/completions` 非流式与流式各至少一次真实调用，核对真实 usage 三分量入账与预算差额结算；测试预算设保守上限（单次输出 token 上限 + 账号月度上限），真实调用测试按 `DEEPSEEK_API_KEY` 存在与否 skip，不把真实调用塞进默认 `make check-go` 的必跑路径。
- 仍待调查：真实计价数字与币种取官方计价页当时版本，写入目录配置的 `price_version` 并记录来源与日期；DeepSeek 无查询能力下 unknown 的运维核实入口本项只提供受信内部接口，运维 UI 不在本项；Harness/会话/River 驱动、SSE、工具执行归 FND-07/08。
- 风险与保障：新增 schema 与迁移、并发/顺序/一致性语义（派发许可、预算 hold、Rearm、成本分叉）、信任边界（供应商凭证与外发）、不可恢复代码外副作用（真实付费调用）→ 定向 red→green 测试 + 一轮独立 change review（owner 既定：默认一轮，blocking 清零即关）+ 迁移 up/down 验证 + 真实调用的保守预算上限。

- **FND-06 独立 change review 与修复（2026-09-11）**：
  - 第 1 轮（fresh reviewer，Opus；codex 侧 MCP/CLI 均不可用、gemini 在 `cli-tools.json` 关闭，故按 FND-05 先例用宿主 fresh reviewer）目标 `c7c18fc7…`：2 blocking + 7 important。修复：B1 证据 rank/supersedes 从未生效（运维估算可回滚 900_092 micros 发票）；B2 `recomputeCostHold` 跨 attempt 统计（前次拒绝的零证据释放本次 attempt 的 hold）；I1 Eino 适配器预留泄漏；I2 截断结果把工具调用交回框架；I3 429 无条件判未受理；I4 measurement 未校验 attempt 归属；I5 锁序与契约相反；I6 上界估算漏工具 schema 且猜图片 token；I7 Prepare 未校验预留覆盖。B1/B2 均以临时回退复现 red 再转 green。
  - 第 1 轮的 follow-up 复审在 4 秒内被账号级 session 限额（HTTP 429）打断、无任何分析产出，session 不可恢复 → 该轮不计入轮次；限额重置后按协议新建 fresh reviewer 承担同阶段 follow-up，并把前任报告原文一并交付。
  - 第 2 轮目标 `e74a9706…`：确认 B2/I1/I2/I3/I4/I6/I7 与 7 项 nit 已解决；新增 1 blocking + 4 important。**修复如下，全部改代码而非改文档**：
    - **F1（blocking）同级证据仍可静默回退账本**：`data-model.md:285` 在同一句内要求「同级只接受可验证且递增的供应商修订序号或明确更正链，`received_at` 不作为事实先后」。`outrankedByCurrent` 现读出当前证据的 `provider_revision`（此前是写入即死的字段）：同级且未填 `Supersedes` 时，只有可比较的**递增整数** `ProviderRev` 才放行，否则挂 `pending`；`Supersedes` 指向当前已入账证据是另一条放行路径（此前该字段无任何可达放行路径）。红证：临时去掉守卫后 `TestSameRankedEvidenceNeedsADecidableOrder` 复现 900_092 → 99 的账本回退。
    - **F2 缺币种守卫**：同句还要求「币种/分量不一致挂起核实」。`applyMeasurement` 在推进位置前比对该请求预留的币种，不一致则留史 + `pending`，不开该币种位置、不进月桶；`recomputeCostHold` 只用预留币种的位置决定 hold 与 `actual_micros`。红证：临时去掉守卫后 `TestForeignCurrencyEvidenceIsParkedNotConverted` 把 900_000 EUR micros 记进 USD 月桶（120 → 900_120）。
    - **F3 锁序分裂（模块内 ABBA）**：按 `gateway-harness-data.md:64-68` 与 `gateway.md:85` 统一为 平台限流记录 → 分组 → 预算桶 → request → reservation → attempt → position。`RearmInTx` 改为先锁分组/原预算桶再锁 request（并在锁后核对上界/月份未变，校验顺序保持「状态/generation 先于额度」以免把 stale generation 报成额度不足）；`releaseReservation` / `ReleaseReservationInTx` / `releaseHoldForRetry` 收敛到单一 `releaseHold`（先锁月桶再锁预留，UPDATE 带 revision 守卫，同一笔 hold 不可能被还两次）；`finishSuccess` / `finishFailure` / `VerifyUnacceptedInTx` / `CancelInTx` 在动 request 前先取平台限流行（`txcap.LLMLimitView` 新增 `Lock`，只取行锁不改计数）与月桶。同时修掉「取消一个已 settled/unknown 的预留会把状态覆写成 released」：只有 `reserved`（未认领口为 `unclaimed`）才可释放。
    - **F4 下调 `Concurrency` 会触发 CHECK 违例**：`capacity=GREATEST(EXCLUDED.capacity, active_count)`，缩容随在飞许可自然回落生效，不再把该 limit_key 上所有派发打成裸 23505。
    - **F5 账号隔离断言无判别力**：5 个端口改为断言具体错误（`ErrNotFound`/`RecordMeasurement` 的 `ErrConflict`），并同时断言本账号调用不会得到同一错误——否则断言与隔离无关。
    - nit：`lockBudget` 不再顺手建空桶（缺桶按 `ErrConflict` 报错，避免把月上限钉成 policy 默认值）；Prepare 回放返回预留真实 settlement 状态；Eino 适配器直接用 Prepare 返回的 request id（补偿路径不会拿不到 id）；删除不可达的 `isDialFailure`/`not_delivered` 分支（Go 传输错误不能证明未送达，一律 unknown）；共用 `limit_key` 的部署参数不一致时 `NewCatalog` 直接拒绝；429 条目注明依据是厂商错误表而非实测；Anthropic cache-write 前置项与月桶上限固化边界写入 `docs/dev/creative-llm-gateway.md`。
    - 测试另修：同级/币种/`Supersedes` 更正链新增 3 类用例；`gateway.md:87` 的「B 释放后 A 才能重占」改为走真实 `ReleaseReservationInTx`，删掉 `UPDATE llm_budgets` 的裸 SQL 固件；hold 断言从「非零」改为等于 inputBound；`ErrAttemptsUsed || ErrState` 析取断言改为精确断 `ErrState`（并注明 attempt 上界是防御层、不由该断言覆盖）。
  - 验证：`make check-go` 全绿、golangci-lint `0 issues`；`llmgateway` 45 个非 live 用例 + `store` 全绿；真实 DeepSeek 三例重跑 PASS（`input=23 cached=0 output=27 booked=21 micros USD`，与价格配方逐项对账）。
  - 第 3 轮（终轮）目标 `86e16947…`：**可合入，blocking 清零**。reviewer 对全部加锁点做穷举抽取，确认每个事务都是 `L1(platform_llm_limits) → L2(llm_call_groups, llm_budgets) → L4(request → reservation → attempt → position/consumer)` 的单调不减子集，两个 ABBA 对消解；独立复跑 `go vet` 干净、llmgateway 53 非 live PASS / 3 live SKIP、store 全绿。新增 3 项 nit 已在报告后顺手改掉（因此最终提交内容比被审哈希多这 4 处）：
    - 文档第 31 行原称 `VerifyUnacceptedInTx` / `CancelInTx` 也先取平台限流行，实际只有 `finishSuccess` / `finishFailure` 取（那两个函数不碰该行，取 L2 起的后缀即合法）→ 改成逐函数的事实描述，避免 FND-07 依赖一句错的锁序。
    - `RearmInTx` 锁后等值核对补上 `callerService` / `groupID`（它们决定了预读锁的是哪一行 group），并把「group 行缺失」从静默回落改为 `ErrConflict`——缺行意味着没有那把跨月串行锁。
    - `releaseHold` 注释改写：真正防止同一笔 hold 被还两次的是同事务 `FOR UPDATE` 重读（扣的就是该行当时的 hold 并立即归零），UPDATE 上的 revision 条件只是针对事务内陈旧读的断言，不是并发控制——原注释会让后来者以为可以去掉重读。
    - 文档「验证缺口」补两条：FND-10 的到期清扫必须走 `ReleaseReservationInTx` / `CancelInTx` 或复刻同一序列，否则额度会被还两次；`finishSuccess` / `finishFailure` 把跨账号共享的 `platform_llm_limits` 行持有到提交的吞吐观察及两条改法。
  - 修复后复跑：`make check-go PKG=./internal/platform/llmgateway/...` 绿、golangci-lint `0 issues`、`store` 绿。
  - **owner 复核（2026-09-11，目标 `d0501c54…`）**：三项已复现问题，全部核实成立并修复，每项都先复现红证：
    - **[P1] 取消传输后并发名额无法释放**：`Execute` 把调用方 ctx 一路传给收尾事务，调用被取消时收尾整体失败。红证：attempt 停在 `dispatching`、`platform_llm_limits.active_count=1`。改为 `finishSuccess` / `finishFailure` 在 `context.WithoutCancel` + 30s 超时的独立 ctx 上持久化——「本次尝试证明了什么」与「归还已结束传输的名额」都不该依赖调用方是否还在等。费用疑问照旧保留（unknown 保 hold）。
    - **[P1] 目录更新会让旧请求发给新模型**：hash 用持久快照校验，但 `provider.Invoke` 用的是当前目录条目。红证：准备时 `vendor-model`，换目录后真的发给了 `different-paid-model`（付费调用已发生，事后判协议错误已晚）。新增 `sameDeployment`，发送前核对 catalog 版本 / deployment key / provider / 请求 model id / 接受别名 / 价格版本与币种，不一致直接 `ErrConflict` 拒发；两个适配器的线上 model id 改取冻结快照而非当前配置。
    - **[P2] 缺少缓存明细阻止已知 token 入账**：token 分量被 `inputUsable`（即缓存拆分是否可用）把住。红证：输入 1000 / 输出 200 / 无缓存明细 → `booked=0 hold=1456`。token 总量只依赖两个总数，改为与费用分量各自结算；缓存明细缺失或不自洽只保留费用 hold。
    - 新增 3 条定向测试（`TestCancelledTurnStillReleasesTheSharedSlot` / `TestCatalogChangeNeverRedirectsAPreparedRequest` / `TestTokenTotalIsBookedWithoutTheCacheSplit`），逐条回退修复复现上述红证后转绿。文档同步三条实现事实。

### FND-06 提交里程碑（2026-09-12）

- owner 授权提交 `30c9367`（38 files，+7979/−4，pre-commit 钩子通过，未用 `--no-verify`）。被提交内容即暂存哈希 `339ee328…`：终轮判定的 `86e16947…` 加上终轮 3 条 nit，再加 owner 复核三项修复。
- 未 push、未合并 `develop`、未应用开发数据库迁移（0043 只在隔离测试库跑过）。凭证仍只经环境变量注入，`.env.example` 只有空占位。
- 门禁记录一处需说明的失败：首次全量 `make check-go` 中 `llmgateway` / `planshare` / `reminder` 同时失败（各 ~64s），根因是共享测试 Postgres 容器在 4 路并行下掉连接（`unexpected EOF` / `connection reset by peer`）；三包单跑与随后的全量重跑均全绿，且 `planshare` / `reminder` 不 import 本次改动，判为环境抖动而非代码回归。

### FND-06 前置加固：调用协调与步骤身份（2026-09-12）

owner 给出 FND-07 前置优化清单。三项 P0 逐条在代码里核实，全部成立，且是同一个缺口的三个面：网关备好了回放/消费/重试机制，却没有一个协调层去用它们，唯一的生产调用方（Eino 适配器）把三者都绕开了。

- **P0-1 步骤与请求身份绑定**：`eino_model.go` 每次 `uuid.NewString()` 生成 operation id。`ReserveInTx` / `PrepareInTx` 按 `(caller_service, caller_operation_id)` 的回放分支因此在生产路径上不可达——恢复即新身份即再付一次钱。
- **P0-2 消费与业务落库原子衔接**：`ConsumeInTx(..., nil)` 且结果由 `Execute` 直接返回，业务行与「已消费」标记落在两个事务。
- **P0-3 调用协调集中化**：完整 reserve→prepare→dispatch→compensate 序列私有在 Eino 适配器内；`RearmInTx` 有实现有测试但**没有任何生产调用者**，真实重试从未发生过。

实现（新增 `coordinate.go`，**无 schema 变更、无迁移**）：

- `Service.Call(ctx, CallSession, CallInput)` 成为业务侧唯一入口，一次走完预留→准备→派发→执行→补偿→消费。
- 步骤身份：`CallInput.BindingKey` 由调用方持久化，网关用固定命名空间对 `caller_service + binding_key + 用途` 做 UUIDv5 派生两个 operation id 与 consumer key。**刻意不建绑定表**——两张表上已有的 `(caller_service, caller_operation_id)` 唯一索引就是这份绑定，另建一张只会产生第二个自称权威的来源。
- `CallInput.Consume(tx, result)` 跑在 `ConsumeInTx` 落「已消费」的同一事务里。
- 补偿规则：Prepare 失败→释放未认领预留；派发失败→连同保留声明取消请求；**`ErrRateLimited` 例外**，保持 `prepared` 让同一 BindingKey 稍后可跑（把自愈条件变成永久失败更糟，占用交 FND-10 清扫）；只有网关自证未受理才走 `RearmInTx` 重试，`maxAttempts` 封顶；`dispatching`/`streaming`/`unknown` 一律 `ErrUnknown` 要求核实，绝不重发。
- `ModelSession` 改为 `CallSession` + `TurnKey`（**必填、无默认**：框架无恢复概念，给默认值等于让每次恢复重新付费）+ `Consume` + `Stream`；适配器 152 行压到把一次 `Call` 包成框架形状。

红证（逐条回退修复复现）：改回每次新 UUID → `a resumed step opened a new request: llmr_d90060cb… then llmr_2916dd8c…`；把 consume 移出消费事务 → `the result was marked "consumed" although the caller's write rolled back`；去掉补偿重试 → `a refused attempt must be retried by the coordinator: provider unaccepted (connect_refused)`。新增 5 条定向测试。

验证：`make check-go` 全绿、golangci-lint `0 issues`；llmgateway 55 非 live PASS / 3 live SKIP。`dispatch.go` / `accounting.go` / `provider_*.go` / 迁移**未改动**（见 diff），真实 DeepSeek 验收沿用 `30c9367` 的结果，未重复付费重跑。

**owner 清单中本轮不做的排期项**（按其标注时机，不从本轮推定进度）：

| 时机 | 项 | 备注 |
|---|---|---|
| 上线前 | 完善异常恢复（进程退出、提交结果未知、未消费结果、遗留并发许可，明确核实出口） | 与 FND-10 到期清扫强耦合，`ErrRateLimited` 保留的 hold 由它兜底 |
| 多模型接入时 | Provider 能力与计量契约（Claude cache-write 计价、Gemini 适配、GPT 逐模型验收） | cache-write 分量是启用任何真实 Anthropic 部署的前置项 |
| FND-07 接入时 | 模型目录 API 与前端选择器 | 客户端可见入口本就属 GH-02/FND-07 |
| 近期确定接口 | 点数计费衔接（独立点数账本、定价版本、冻结/扣点/退还、事务或 outbox 幂等） | 可能改变 FND-06/FND-13 子项定义，需要时走 `cs-epic` 边界重确认 |
| 上线前 | 观测接口与指标（token/成本/耗时/错误/限流/未知结果/预留积压） | 吞吐观察维度已记在 `docs/dev/creative-llm-gateway.md` 验证缺口 |
| 流式体验开发时 | 分离实时增量与最终结果 | 属 FND-08 流式体验；网关侧「片段不是结果」已成立 |
| 数据增长前 | 数据保留与查询设计（载荷/结果/证据保留期、按账号/模型/时间聚合） | `retained_until` 已有，聚合查询未做 |
| 压测后 | 数据库热点优化（共享限流行、账号预算桶锁等待） | 两条候选改法已写进文档 |
| 扩展媒体生成时 | 独立异步生成契约（提交/查询/取消/取件） | 不硬套聊天接口；属 FND-13 |

- **owner 复核（2026-09-12，目标 `133fbe69…`）**：两项恢复缺陷，均核实成立并修复，各自先复现红证：
  - **[P1] 重新预留与派发分开提交 → 步骤卡死**：`rearm` 与 `beginDispatch` 是两个事务，中途进程退出或派发被限流，会留下「预留已重新占住（`reserved`）、请求仍标 `retry_eligible`」的组合，此后每次 `Call` 都在 `RearmInTx` 撞 `reservation is not released for retry`，额度占到过期。**可达路径正是上一轮我自己加的 `ErrRateLimited` 例外**——为让限流可恢复而提前返回，恰好把卡死状态变成常规路径。新增 `RedispatchInTx`：先取平台限流行（否则合并后会变成 L2→L4→L1 逆序）→ `RearmInTx` → `BeginDispatchInTx`，全在一个事务；失败整体回滚，步骤保持原样可恢复。`RearmInTx` 补上「必须与派发意图同事务提交」的契约注释。红证：改回两事务后 `the reservation was left "reserved" (retry_eligible=false) with no attempt to go with it`。
  - **[P1] 并发恢复放弃他人已付费的结果**：派发拿到 `ErrState` 后仍进补偿，`endUndispatched` 又忽略 `CancelInTx` 的 `ErrState`，把一个 `pending` 的已付费结果标成 `abandoned`，此后无法消费。两处都改：`compensate` 对 `ErrState` 一律不补偿（这一回合不归本次调用——别人正在派发，或它已终态）；`endUndispatched` 只在本事务**确实把请求取消掉**（`view.State == StateCancelled`）时才放弃保留声明。红证：`the loser marked a paid result "abandoned"; it can never be consumed again`。
  - 新增 2 条定向测试：`TestARefusedRedispatchLeavesTheStepResumable`（`RateLimitPerMin: 1` 的目录让重新派发被确定性拒绝）、`TestALostDispatchRaceNeverAbandonsAPaidResult`（用 `session.Limits` 作屏障，让败者停在取许可前，确定性复现竞态）。`setupGateway` 抽出 `setupGatewayWith(catalog)`。
  - 验证：`make check-go` 全绿、golangci-lint `0 issues`；llmgateway 57 非 live PASS / 3 live SKIP。`dispatch.go` 本轮为纯新增（单 hunk 无删除行），`Execute` / `finishSuccess` / `finishFailure` / `provider_*.go` / 迁移仍未改动，真实 DeepSeek 验收沿用 `30c9367`。
  - **追加 [P1]（同轮 owner 复核）重新准入撞预算上限会永久取消可恢复步骤**：`compensate` 当时是黑名单——只放过 `ErrRateLimited` / `ErrState`，其余一律取消。`ErrBudget` 恰恰是**共享且会变**的量（别人正占着的分组上限，或一笔低于 hold 的结算就会腾出的月桶），取消等于把别人腾出额度就能恢复的步骤就地作废。三条缺陷是同一个错误的三面：补偿用了「除非认识否则取消」的默认。改为白名单——只有 `ErrDeadline` / `ErrAttemptsUsed` / `ErrCancelled` 这类证明身份永不可再派发的条件才结束请求，其余（含无法识别的错误、连请求都没读出来的失败）一律不动。红证：改回黑名单后 `a temporary ceiling ended the step as "cancelled" after 1 attempts`。新增 `TestACeilingRefusalLeavesTheStepRecoverable`：hold 用 `CostMicros` + `EstimateInputTokens` 按协调层同一配方推导（不写死数字），分组上限恰好容一笔，竞争者经真实 `ReleaseReservationInTx` 释放后原身份恢复成功。
  - 复跑：`make check-go` 全绿、golangci-lint `0 issues`；llmgateway 58 非 live PASS / 3 live SKIP。

### FND-06 前置加固：独立 change review（2026-09-12）

owner 授权后开一轮独立 change review，目标冻结为暂存差异 `60cdb855…`。

- **reviewer 创建**：先按协议选异构——`maestro delegate --to codex`（gpt-5.5，cli-tools.json 中 enabled）。codex CLI 缺原生依赖 `@openai/codex-darwin-arm64`，退出码 0 但**无任何分析产出**，按协议属「运行失败无报告」，不计轮次、允许更换创建方式。opencode 亦不可用（缺 `opencode-aicodewith-auth`）；gemini 在 cli-tools.json 为 `enabled: false`，按 CLAUDE.md 不绕过。无合格异构候选，按 FND-05/FND-06 先例回退宿主 fresh reviewer 并**显式指定 Opus**。（环境待修项：codex 需 `npm install -g @openai/codex@latest`。）
- **reviewer 判定**：核对哈希一致；5 项声称修复逐条独立验证**全部通过**（含锁序单调性穷举与 hold 双还推演）；结论「建议先修再合入」，1 blocking + 2 important + 2 nit。

**[blocking] Prepare 失败后释放未认领预留 → 该 BindingKey 被永久毒化**（我方复核成立）：`requestFor` 对任何 Prepare 错误都释放预留，而 `released` 不是 `unclaimed`——下次同 key 的 `Call` 回放到它，`PrepareInTx` 的认领条件 `settlement_state='unclaimed'` 永不匹配，从此恒报 `ErrConflict: reservation already claimed`，全仓没有任何路径写回 `unclaimed`。可达触发面：`CheckCapability` 只在 Prepare 里跑（`ReserveInTx` 不查），`OutputLimit` 超部署上限即可；另有恢复时目录价格版本变更、两事务间期限过期。

根因与上一轮的 [P1] 同源：**该在一个事务里的事被拆成了两个**。修法不是过滤补偿，是去掉补偿的必要性——新增 `admit` 把 `ReserveInTx` + `PrepareInTx` 合并为单事务，删除 `releaseUnclaimed`（净减代码）。红证：改回两事务后 `a refused preparation left 1 reservations behind`，把前置断言降级后进一步看到修正输入的第二次调用仍报 `gateway identity conflict: reservation already claimed`，与 reviewer 描述逐字一致。

**[important] `compensate` 的「会取消」一侧零测试**：5 个用例全在断言「不取消」，把 `compensate` 函数体删空所有测试仍绿。新增 `TestATerminalRefusalEndsTheRequestAndReturnsTheHold`（用 `session.Limits` 屏障让请求期限在准备与取许可之间过期），断言请求 `cancelled`、hold 归零、保留声明 `abandoned`、provider 零调用。红证：删空 `compensate` 后 `a turn that can never dispatch was left "prepared" after 0 attempts`。

**[important] `dispatching` 无核实出口 + `active_count` 不回收**（reviewer 判 pre-existing，我方同意不列入本轮 blocking）：派发事务提交后、`claimPermit` 前进程退出，或 `finishSuccess` 首个事务超 30s，请求永久停在 `dispatching`；而 `VerifyUnacceptedInTx` 只收 `unknown`，核实入口不适用。同一缺口永久漏掉跨账号共享的并发名额，漏够 `Concurrency` 次后全账号派发不自愈。**已按 reviewer 要求把排期项「完善异常恢复」的覆盖面写实**：必须同时覆盖 `dispatching`（不只 `unknown`）与 `active_count` 回收，细节写入 `docs/dev/creative-llm-gateway.md` 验证缺口第 4 条。

**[nit] 两条均已处理**：`RedispatchInTx` 补注释说明限流行此时必然存在（否则 `lockSharedAdmission` 静默跳过会造成 L1 后置的逆序）；`CallOutcome.RequestID` / `Dispatched` 在 `Call` 返回错误时也填，否则调用方无法指名要核实哪个请求。

- reviewer 对新增测试的质量评价：8 个用例「咬得相当实」，逐条列出了各自钉住的不变量；断言不足处即上述两条 important，已补。
- 验证：`make check-go` 全绿、golangci-lint `0 issues`；llmgateway 60 非 live PASS / 3 live SKIP。供应商路径（`Execute` / `finishSuccess` / `finishFailure` / `provider_*.go` / 迁移）全程未改动，真实 DeepSeek 验收沿用 `30c9367`。

- 2026-09-12 提交里程碑：owner 授权提交 `381e4a2`（9 files，+1134/−134，暂存哈希 `d8786efc…`，pre-commit 钩子通过，未用 `--no-verify`）。未 push、未合并 `develop`、未应用开发数据库迁移。审查阶段以 blocking 清零结束；唯一遗留 important（`dispatching` 无核实出口 + `active_count` 不回收）为 pre-existing，已在 owner 排期清单「完善异常恢复」内并写实覆盖面，owner 知情后授权提交。


### 孤立 LLM 派发恢复修复（2026-09-13）

owner 授权修复 `dispatching` 无核实出口及跨账号共享名额泄漏。原复现：两次 BeginDispatch 提交后丢弃许可，推进 48 小时，Verify 仍拒绝且另一账号 active_count=2 无法派发。实现固定传输期限、过期许可 fencing、账号范围恢复与创意 Worker 分页巡检；unknown 保留费用 hold，迟到完整结果仅允许更新尚未被核实/替换的同一 attempt，回收名额幂等。部署需停止旧执行器，不新增迁移；无 commit 授权。定向测试包含 orphan/过期旧许可/结果持久化失败/迟到结果与另一在飞名额/非 active 账号分页。

验证闭环：`TestExpiredPermitCannotStartBeforeSweep` 对照 HEAD 的 dispatch.go 红（过期许可仍成功），当前版本五条恢复测试全绿；`make check-go` 全绿（首次仅 goimports 分组报错，修正后通过）。独立单轮 cs-review：候选快照 SHA-256 `ea12e95e902348e3305172251f45d4a176f7b1044cd8eec4ae61ae185975966b`，无 blocking/important，可合；仅本段记录在审查后追加。日志 `/tmp/gateway-recovery-red.log`、`/tmp/gateway-recovery-tests.log`、`/tmp/gateway-recovery-go.log`。未重复 review，未付费调用、未修改开发数据库、未提交。

### FND-07 开工（2026-09-13）

依据已批准 Epic FND-07、`modules/harness.md`、`modules/gateway-harness-data.md` §3/4/5、`modules/agent-api-events.md`、`eino-adoption.md`、`data-model.md` §7「运行状态机（唯一权威）」与 §8 实施。上一项 FND-06 及其两轮加固已提交（最新 `ce48635`）；owner 明确沿用当前 worktree 分支 `feat/creative-workspace-redesign`，不新开 worktree。

- **依赖核实**：FND-03/04/06 均已提交。`gateway-harness-slices.md` §1 要求「River/pgx 桥接、同事务入队/回滚在 GH-02 接 run 之前证明」——已由 FND-05 的 `store/creative_jobs.go`（`river.InsertTx` 绑定同物理 `pgx.Tx`，`jobs.TxEnqueuer` 密封能力）落地并随媒体 Worker 验证，本项不重复建桥。Eino v0.9.19 与 River v0.40.0 已在 `backend/go.mod`。最新迁移 `0043_llm_gateway`，本项从 `0044` 起。`internal/creativeagent` 与前端 Agent 面板均为全新。
- **范围边界**（由 Epic 自身划分推出，非本项缩窄）：SSE 流式与消息 chunk 持久化 → FND-08「流式体验」；独立附件 draft / `agent_attachment` → FND-09；画布写工具与提案采纳（AG-06）→ FND-08。FND-07 只开放真实对话与**只读运行时工具**（Skill List/Get、ReadSkillResource、ReadRunResult）——正是这三个只读工具为 ReAct 循环提供真实多轮，使 checkpoint/resume 有可验对象；没有它们，「有界运行适配」将无从验证。因无写工具，`waiting_apply` / `run_artifacts` 在本项只保留状态机出口，实际提案与采纳随 FND-08 的写工具落地。
- **模型调用形态**：本项用 Gateway 非流式调用（`CallInput.Stream=false`），完整结果一次落库为完整 assistant 消息；`creative_message_chunks` 随 FND-08 流式一并落地，不提前建空表。
- **owner 拍板（2026-09-13）交付粒度**：分 3 个里程碑提交，每段独立可验证、独立 review、逐段授权提交。理由是 `attention.md`「一个 feature 一个提交」写了「除非必要」，而本项一次性 diff 约 60+ 文件会让独立 review 失准。
  - **A**：迁移 0044 + 会话/消息 + 外发同意 + 模型/Skill 目录 + 对应 REST/OpenAPI。
  - **B**：迁移 0045 + run/step/slot/epoch 状态机 + Eino Runner/ChatModelAgent 接入 + checkpoint/context/skill 三个 backend + 只读运行时工具 + Gateway 结果消费 + River Worker。
  - **C**：等待/取消/对账/终态恢复 + 最小 Agent 面板（轮询）+ 真实 DeepSeek 对话验收。
- **风险与保障**：改 schema/迁移、并发/顺序/一致性语义（slot/epoch/lease/CAS、全局锁序第 2-4 层）、信任边界（外发同意、Skill 包 digest、跨账号）、不可恢复代码外副作用（付费模型调用）→ 每个里程碑各做定向 red→green 测试 + 一轮独立 change review（owner 既定：默认一轮，blocking 清零即关）。

### FND-07 里程碑 A：会话、消息、外发同意与目录（2026-09-13）

- **归属**：新建 `internal/creativeagent`（ConversationApplication / 外发同意 / SkillRegistry / Catalog）；迁移 `0044_creative_agent_conversations`（5 表：conversations / messages / message_content_refs / egress_consents / egress_consent_contents）；HTTP 新增 `/creative/agent/catalog`、`/canvases/{id}/conversations`（GET/POST）、`/conversations/{id}/messages`、`/conversations/{id}/egress-consents`、`/egress-consents/{id}/revoke`；`cmd/server` 装配 `creativeagent.Compose(catalog)`；开启 `agent_start` 能力（此前 `CreativeCapabilities` 恒为 false）。
- **owner 拍板（2026-09-13）外发权利边界**：**不引入 `ai_analysis` 这项独立的外发权利校验**；`creativecontent` 一行未改。我在提问时已写明该选项与 AG-08「两项独立校验」直接矛盾，owner 知情后选定。
  - **准确表述（经独立审查纠正）**：授权时对每个选中修订调的 `creativecontent.RequireUsable(..., "display")` **不只是身份完整性校验**，它实际执行四项：同账号 / `state='ready'` / `requireRoot` 保留根；`ValidatePurpose(..., PurposeMoodboardDisplay)` 权利矩阵（`PurposeAllowed` 对该用途恒 true，故不构成来源限制）；`creative_usage_grants` 存在未撤销的 `display` 授权；`creative_content_required_grants` 派生闭包。即 **display 权利闸门仍然生效**——摄影师自己能展示的修订可授权外发，display 授权被撤销的不能。逐条见 `docs/dev/creative-agent.md`。
  - **需求偏差待处理**：`.codestable/requirements/creative-canvas-foundation.md:119`「账号外发同意与素材用途权利是两项独立校验，任何一项不满足都不能发送」与实现相反。需在 FND-12 前经 `cs-epic` 边界重确认修订该句，否则 FND-12 会记为不符。已写入 `docs/dev/creative-agent.md`。
  - owner 同轮补充：后续也不启用图片/视频权利校验，因为页面已不提供来源选框。核实这一现状 FND-05 已成立——`RightsDeclarationInput.OrDefault()` 注释即「Clients no longer ask for a source」，文字与媒体都在服务端默认 `photographer_owned`/`ownership_attested` 并自动建 `display` grant。本项因此无需改动。
- **Gateway 补 `VendorKey`**：外发同意需要「字节发给哪家公司」，而 FND-06 只有协议族 `Provider`（`openai_compatible`）和按模型的 `DeploymentKey`（`deepseek/api/flash`）。新增 `ModelConfig.VendorKey`（必填，`^[a-z][a-z0-9_-]{0,63}$`）、`CatalogEntry.VendorKey`、`Catalog.Vendors()`。按公司记录授权，同一家的两个模型间切换不重新索要授权。设计稿 `agent-api-events.md` 的 `provider_key` 语义即此字段，实现统一改名 `vendor_key` 并在设计稿标注。
- **范围内的诚实缺口**：`tools` 目录恒空、内置 Skill `reference-direction@1` 一律 `available:false` 附原因（写工具属 FND-08）；消息 chunk 与流式不建表（FND-08）；附件草稿不建表（FND-09）。
- **红证**：① 会话行锁降级为普通读 → `duplicate key value violates unique constraint "creative_agent_messages_account_id_conversation_id_ordinal_key"`，序号变成 1 和 0；② 去掉 `RequireUsable` 归属校验 → `a revision of another account must not be authorisable, got <nil>`。
- **测试**：`creativeagent` 17 个用例（会话取画布的项目而非请求的、并发追加不共享序号、步骤重放不追加第二条消息、往回分页的阅读顺序与游标排他、跨账号修订不可授权、未知厂商不可授权、同一操作只生成一份同意、二次撤销被拒且证据保留、陈旧 revision 撤销不生效、Skill 在工具未注册前不可用、同 key/version 不同 digest 拒绝启动、包外资源拒绝、目录冻结上限与可达厂商）；`store` 新增 `TestAgentConversationMigrationShapeAndLossyRollback`（账号键、序号唯一、步骤按角色唯一、vendor_key 不收 URL、空历史可回滚、有历史拒绝回滚且提示先导出）；`llmgateway` 新增 `TestAModelWithoutAVendorCannotBeLoaded`。
- **存量维护**：新增迁移使 9 个文件的逐级回滚列表各少一步，已补 `agent-conversations` 标签与对应 down 调用；`creative_graph_migration_test.go` 回滚步数 4→5。
- **验证**：`make check-go PKG=./...` 全绿（golangci-lint `0 issues`）；`make check-frontend` 381 用例通过。`make generate` 已跑，Go/TS 两份生成物同提交。开发数据库未应用 0044，未启动任何服务。

### FND-07 里程碑 A：独立 change review（2026-09-13）

- **reviewer 创建**：先按协议选异构——`maestro delegate --to codex` 的 codex CLI 仍缺原生依赖 `@openai/codex-darwin-arm64`（与 FND-06 同一故障，未修复），`opencode` 亦不可用，`gemini` 在 `cli-tools.json` 为 `enabled: false`。无合格异构候选，按 FND-05/06 先例回退宿主 fresh reviewer 并显式指定 Opus。第 1 轮目标 `8410f13a…`。
- **判定**：哈希核对一致，2 blocking + 6 important + 9 nit，结论「建议先修再合」。两条 blocking 均核实成立且都是实现者的错。

**[blocking B-1] 六个 Agent 端点注册在未认证路由组，带合法 token 也恒 401**：`registerCreativeAgent(api, h)` 落在 `protected := api.Group("", authMiddleware(...))` 之前，即注释所述「ticket/签名授权」组，而 Agent 端点没有票据机制。fail closed 不构成越权，但里程碑 A 整个 REST 面在生产不可用。根因是插入时锚在 `registerCreativeMediaBytes` 上，位置正好错在分界线前。漏测原因：`creative_foundation_test.go` 的鉴权用例是**硬编码路径清单**，新路由不会进去。已改为遍历 `r.Routes()` 断言所有 `/api/v1/creative/*` 非票据路由返回 401，票据例外收窄为与 `registerCreativeMediaBytes` 一一对应的显式 map。红证：放回 `api` 组后新测试报 4 条路由非 401。

**[blocking B-2] OpenAPI 把路径注入字段声明为必填，符合契约的客户端必被 400**：三个新 payload schema 把 `creativeWrite` 从 URL 注入的 `canvas_id`/`conversation_id`/`consent_id` 列进 `required`，而 `creative_canvas.go` 对客户端自带该键直接返回 400。既有 `CreativeProjectRenamePayload` 等四处都不含注入字段，新增三个是唯一例外。违反 CLAUDE.md 硬规则 3。已删除并重跑 `make generate`。

- **important 处置**：
  - **I-1**（采用 reviewer 选项②）`RequireUsable(..., "display")` 并非纯身份完整性校验——它还跑 `ValidatePurpose(..., PurposeMoodboardDisplay)`、要求未撤销的 display grant、要求 `creative_content_required_grants` 派生闭包。注释、`docs/dev/creative-agent.md` 与本游标的偏差记录已改为如实描述：**偏差是「没有引入 `ai_analysis` 这项独立外发权利校验」，display 权利闸门仍生效**。不选①（换成裸归属检查），因为那会放宽到允许 display 授权已撤销的内容外发，owner 没要这个。
  - **I-2** `GrantConsent` 先插 consent 再锁 content，与全局锁序第 5 层相反（今天不死锁，里程碑 B 派发时复核 consent+content 就会双向）。改为先校验/取锁全部修订再插 consent 与 contents，顺带让「被拒授权不留 consent」从依赖回滚变成结构性。
  - **I-3** `Compose()` 自建第二份目录，与 `llmgateway.Build` 的 `CatalogPath` 分支可能不同源；在「外发同意是唯一闸门」前提下闸门白名单必须与派发端同源。新增 `llmgateway.OpenCatalog(path, credential)` 作唯一入口，`Build` 改调它，`Compose` 改为接收注入，`cmd/server` 建一份传入。
  - **I-4** (a) step 重放探测在会话行锁**之前**，并发消费同一 step 时双方都探测不到 → 败者撞唯一索引变成裸 500 而非重放。已移到行锁之后（step 属 run、run 属会话，必然争同一把锁）。(b) `messageForStepInTx` 未限定 `conversation_id`，传错会话会拿回另一会话的消息。已加。
  - **I-5** 补 4 组测试：跨账号读/列表/撤销/在他人会话授权（四条断言，此前**零覆盖**）、`ContentRefs` 写入与三类坏引用、`validBody` 超限与六种坏 block、真实重复撤销路径。并按 reviewer 建议把 `RevokeConsent` 的 revoked 检查调到 revision 之前——原实现下客户端拿自己刚用过的 revision 重试会得到 409「内容已被其他窗口修改」，对已撤销授权是误导文案。
  - **I-6** `creativeError` ⇄ `creativeAgentError` 互相递归，仅靠注释维持不爆栈。已把 `creativeAgentError` 的 default 改为终端处理。
- **nit 已处理**：迁移 `scope` 加 CHECK（mode 枚举 + `data_classes` 非空数组）；`Catalog` 能力检查前置；`ContentRefs` 校验提到消息落库前并拒同次重复；`gateway-harness-data.md:52` 字段名同步。**CHECK 第一版有 NULL 语义漏洞**——缺键时 `jsonb_typeof` 返回 NULL、CHECK 只拒 FALSE，`{"mode":"account_library"}` 被放行；迁移测试的负例当场抓到，已用 `coalesce` 修正。
- **未处理的 nit（已记为已知边界）**：skill digest 由加载方计算而非 registry 重算（改后测试无法构造不匹配分支）；`maxSkillFileBytes` 只约束 `Resources`；`ToolAllowlist` 条目形状未校验；`consents.go` 的 `tx.Update` 影响行数丢弃；`VendorKey` 必填对使用 `CatalogPath` 的部署是破坏性配置变更（`llmgateway.Build` 尚未接入 `cmd/server`，今天无实际影响）。
- **复跑**：`make check-go PKG=./...` 全绿、golangci-lint `0 issues`、`make check-frontend` 381 例通过。第 2 轮目标冻结为 `ac2cf312ce8a79af0a6581ae8a3225be3f68568ccf1b0507c15baed265f1d51d`，已交回同一 reviewer 同 session 复核。

**第 2 轮复核（同 reviewer 同 session，目标 `ac2cf312…`）**：首次重发因 API 限流中途终止且**无终态报告**，按协议属「运行失败无报告」，不计轮次；限流恢复后重发，工作区与冻结哈希均未漂移。

- **判定：无 blocking，有条件可合**。第 1 轮的 B-1/B-2/I-2/I-4/I-5/I-6 与 5 条 nit 判 `resolved`；I-1、I-3 判**部分未解决**，条件即这两条。
- **[R-1 important] I-1 只改了一半**：`docs/dev/creative-agent.md` 改准了，但 `consents.go` 的函数级 doc comment 仍写「the rights matrix deliberately does not participate」，本游标里程碑 A 段也仍是原话——同一份文档相隔 20 行自相矛盾，且错的那条排在前面、挂在 owner 会读的「外发权利边界」标题下。已**就地改正**（不是追加）：函数注释改为指向 dev 文档的四项清单，inline 注释补上 `ValidatePurpose` 与派生闭包，游标那段改为「不引入 `ai_analysis` 这项独立外发权利校验；display 权利闸门仍生效」。同段顺带修正 `Compose()` → `Compose(catalog)`、用例数 13 → 17。
- **[R-2 important] I-3 的接缝建好了但组合根没接上**：`main.go` 仍是 `OpenCatalog("", nil)`，而 `OptionsFromEnv` 会读 `CREATIVE_LLM_CATALOG`。部署一旦设了该变量，里程碑 B 接入 `Build(OptionsFromEnv())` 后 Gateway 用文件目录、`creativeagent` 用内置目录，闸门白名单与派发端再次分叉——正是 I-3 要防的事；且 `Build` 当时的签名也不接受预解析的 catalog，注释里「milestone B hands this same instance」没有实现机制支撑。已给 `Options` 加 `Catalog *Catalog` 字段（`Build` 优先用注入的那份），`main.go` 改为 `OptionsFromEnv()` → `OpenCatalog(opts.CatalogPath, opts.Credential)` → 存回 `opts.Catalog` 传给 `Compose`；`OpenCatalog` 的注释也改成如实说明它没有单例语义。
- **[R-3 nit] `scope` CHECK 的第三个合取项依赖 AND 短路**：`{"mode":"account_library","data_classes":"text"}` 会让 `jsonb_array_length` 对标量**抛错**而非返回 NULL，PG 不保证布尔子表达式求值顺序。已改用 `CASE`，并把该输入连同 `null` / 数组 / 标量三种 `scope` 加进迁移测试负例。
- **[R-4 nit] 路由豁免 map 以路径为键、不含方法**：将来在 `/creative/media-parts` 上再注册一个未认证动词会被整条豁免。已改为 `method+" "+path`（三条），占位符正则补数字 `[a-zA-Z_0-9]+`。
- **[R-5/R-6 nit] 已处理**：注释写明重放身份是 `(step, role)` 不含 body；补块数上限用例；测试局部变量 `append` 改名避免遮蔽内建；`TestRetryingTheSameRevoke…` 注释点明走的是领域路径而非回执重放。
- **记为已知边界并写入 dev 文档「验证缺口」**：`ContentRefs` 不校验 `RevisionID` 归属/`ready`/保留根（与 `GrantConsent` 不对称，里程碑 B 接真实 run 时补）；重放不比对 body。reviewer 无 finding 的两处经核实成立：`OpenCatalog` 抽取未改变 `credential` 回落语义；`RevokeConsent` 检查顺序调换让并发撤销从 `creative_revision_conflict` 变为 `creative_egress_revoked`，两者 HTTP 同为 409 但 code 可区分，新语义更准确。
- 第 2 轮后一度按 owner 既定规则（默认一轮、blocking 清零即关）收尾于候选 `d38aaee7…`；随后 owner 转交第 3 轮报告，见下。

**第 3 轮（owner 转交，1 blocking + 1 important，两条均已复现并修复）**：

- **[blocking] 消息引用没有真正成为保留根**。`appendMessageInTx` 写了 `creative_message_content_refs`，但 `creativecontent.requireRoot` 的根表清单里没有它——于是「消息是保留根」只在文档和迁移注释里成立，代码里不成立。原资产改指向新修订（或该资产消失）之后，只被历史消息引用的旧内容 `Read` 返回 `creative content not found`，即摄影师往回翻对话看到的是空。漏测原因：`TestAMessageKeepsTheContentItShowsReadable` 只数了 ref 行数，没有真的去读。
  - 修法是在 `requireRoot` 的清单里加 `{"creative_message_content_refs", "content_revision_id=$2"}`，不加生命周期谓词——消息不会被删，这正是它作为根的意义；索引 `creative_message_ref_revision(account_id,content_revision_id)` 已覆盖该查询。核实过 `requireRoot` 是保留判定的唯一实现点，没有第二处枚举根表的清理器需要同步。
  - 测试改为：移掉资产根后 `creativecontent.Read` 仍返回原文；并加一条对照——无人引用的修订在失去资产后必须读不到，证明这是一条根而不是一张放行票。红证：抽掉该行后逐字复现 `a message must keep what it shows readable: creative content not found`。
- **[important] Skill 包可被调用方改写而 digest 不变**。`NewSkillRegistry` 与 `Get` 都是浅拷贝：`SkillPackage` 是值，但 `Resources` map、其中的 `[]byte` 与 manifest 的 `ToolAllowlist` / `InputKinds` / `RequiredModelCapabilities` 不随值语义复制。拿到包的调用方能改掉指令资源、凭空塞进一份参考、或放宽工具白名单，而 digest 纹丝不动——digest 唯一的用处（回答「当时跑的是哪份文本」）就此失效。
  - 修法：`SkillPackage.clone()` 深拷贝全部可变字段，构造时与 `Get` 时各调一次；新增包内 `lookup` 供 `Resource` 与 `Get` 复用，避免读一个资源要深拷整个包。`packagesInOrder` 仍返回存量值（包内列举口，只投影 manifest 字段不写），避免一次目录请求复制全部资源字节；已在注释里写明这条约束。
  - 红证分两半各自复现：只去掉 `Get` 的拷贝 → `a caller added …injected to a published package`；只去掉构造时的拷贝 → `…sneaked`，证明两处拷贝都不是死代码。
- 文档同步：`docs/dev/creative-agent.md` 的「消息是保留根」补上执行点与红证，「验证缺口」第 5 条改为「保留根这一侧已生效，但一行指向不存在修订的 ref 仍写得进去」；Skill 一段补深拷贝的理由与 `packagesInOrder` 的例外。
- **轮次已用满 3 轮**（本阶段：第 1 轮 `8410f13a…`、第 2 轮 `ac2cf312…`、第 3 轮 owner 转交）。按 attention.md，再加一轮需 owner 明确授权。
- 复跑：`make check-go PKG=./...` 全绿（无 FAIL）、golangci-lint `0 issues`；`creativeagent` 18 例、`creativecontent` 全绿。最终候选 49 文件 +4272/−50（较第 2 轮多出 `internal/creativecontent/content.go`）。本轮不再冻结新的审查目标，故不记哈希。**未提交，等 owner 授权。**

## 2026-09-13 边界重确认（contract review 阶段，已批准）

- **触发**：FND-07 里程碑 B 前要把 Skill 从 `go:embed` 改为数据库 + 对象存储。这带来新领域包、4 张新表 + 消息引用表、迁移 0045、2 个新 REST 端点、消息 body v2、CreateRun 请求契约改为 `instruction_segments`、`limitsVersion` 递增、`creativemedia.Adapter` 上提——属子项交付定义变更，只在游标承接不够，故走本阶段。同批处理欠着的 `creative-canvas-foundation.md` 外发权利边界偏差。
- **owner 拍板（2026-09-13）**：对象端口选「上提 Adapter 到 platform」；Skill 版本保留「本期只保证不删除」；边界重确认「先走再实施」；分支「当前 worktree 继续」。
- **方案复核**：实施前对着工作区源码复核 `skill-foundation.md` 一轮，改正 6 处、补全 4 处。最关键一处是迁移编号——原文要为里程碑 B 预留 0045，但 golang-migrate 的 `Up()` 只向前推进版本号，避让后补写的低编号迁移永远不会执行。
- **reviewer**：宿主 subagent，显式 `model: opus`。三个异构候选当日全部不可用（codex 缺 `@openai/codex-darwin-arm64`、gemini `settings.json` 非法、opencode 缺 `opencode-aicodewith-auth`），按 cs-epic 回退同构最强模型，与本 Epic 前几轮一致。
- **第 1 轮**：目标 `660c9de9…`（Epic）+ `8958eefb…`（需求）+ `b4b05b0f…`（方案）。1 blocking、2 important、8 nit，不通过。
  - **[blocking] FND-07 向 FND-10 转交三项义务，FND-10 契约一条未改**：跨账号保留根查询、过期导入对象定时回收、ADR-008 要求的新增关系入一致性扫描清单。FND-10 因此可在完全不覆盖 Skill 的情况下通过验收并开启通用 GC，而 ADR-008 明写「未覆盖的关系不得进入破坏性 GC」。修法：FND-10 补交付/验收/范围三条，范围直引 ADR-008 原句作硬门禁，并与 FND-07 互相指认。
  - **[important] §4.5 的降级理由写错了**。原文论证「跨账号根查询与 ADR-001 冲突」——不成立。仓库已有范式：`creativemedia.SweepAllAccounts` 经 `store.ActiveAccountIDs` 枚举后逐账号 `ScopeFor`，`llmgateway.SweepExpiredDispatches` 用 `MaintenanceAccountIDs` 游标分页，两者都是系统级全库清扫却没有一条无账号范围的查询。降级本身对（本期无删除路径），理由错会让 FND-10 实施者去申请 ADR 豁免。已重写并点名两个范式。
  - **[important] `harness.md:32` 仍按旧口径**说 Skill「包由平台随部署发布」。已就地加过期指针。
- **第 2 轮**（同 reviewer 同 session follow-up）：目标 `1f2aefbd…` + `c429afdc…` + `ab4d0986…` + `cfc4d51d…`（`harness.md` 因修 I-2 被改，本轮起进冻结集，reviewer 确认此归类恰当）。blocking 与两项 important 全部 resolved，**通过**。新增 1 important + 3 nit：过期导入对象的物理回收在 FND-07 与 FND-10 两侧都只有交付、无验收断言（本次追加唯一的破坏性操作）；`harness.md` 指针漏了「启动时校验」时机也已变；需求文档「用途声明」与「display 授权」用词打架，应作「来源与权利声明」；FND-09 条目排序不齐。
  - 首次派发因 API 限流中断、无终态报告，按 cs-epic 该次不计轮次，恢复同一 session 续跑。本阶段共 2 个有终态报告的轮次。
- **通过后修复**：上述 1 important + 3 nit 均由 reviewer 给出确切修法，属机械修改，未再开第 3 轮（轮次仍余 1，owner 知情）。
- **批准**：owner 2026-09-13 批准，`approved_revision` 由 `b024c98b…` 换为 `2b79fb8c…`。四份定稿零断链、禁用词「用户」各 0 处。
- **连带更新**：`docs/dev/creative-agent.md` 的外发偏差待办随批准失效，改为指向 Epic 的边界重确认小节。
- **提交注意**：`docs/product/creative-canvas-system/modules/skill-foundation.md` 与 `skill-marketplace-brainstorm.md` 仍是 untracked，必须与 Epic 同批提交，否则合回 `develop` 后 Epic 内链接立即断。
- 本轮只改文档，代码一行未动，未提交、未发布。

## 2026-09-14 Skill 底座 S0 + S1（实现，待 owner 同意提交）

- **S0 对象端口上提**：`creativemedia` 的 `storage.go` / `local.go` / `oss.go` 移出为 `platform/versionedfs`（包名取「版本固定」与既有 `immutablefs` 对照）。`creativemedia` 现有用例原样通过——测试文件 diff 只有 7 行限定符改名（`creativemedia.X` → `versionedfs.X`），零断言改动，这是语义未变的证据。
- **错误身份的处理**：`ErrNotFound / ErrState / ErrSizeLimit / ErrRange / ErrUnknownResult` 这五个条件由存储层和媒体域各自会抛出，API 边界对每个只映射一个状态码。`versionedfs` 拥有这五个值，`creativemedia` 直接采用（`var ErrNotFound = versionedfs.ErrNotFound`），而不是在每个 adapter 调用点翻译一次——后者要改约 20 处，漏一处就是静默 500。错误文案随之改为与存储无关的措辞，仓库无任何处依赖这些文案。
- **S1 迁移 0045**：`creative_skills` / `creative_skill_versions` / `creative_skill_version_resources` / `creative_skill_imports` / `creative_message_skill_refs` 五张表，全部带 `account_id`、无外键。`marketplace_version_id` 按方案 §4.1 物理预留并用 `CHECK(... IS NULL)` 强制为空。down 迁移在已有冻结版本或消息 Skill 引用时拒绝回滚。
- **迁移登记**：新增迁移必须在 13 个 store 测试的回滚 roster / 分步序列中登记（`skill-foundation` 领先 `agent-conversations`），这是仓库既有维护动作，0044 落地时做过同样的事。
- **S1 领域 `creativeskill`**：ID 前缀 `ccsk_` / `ccsv_` / `ccsi_` 已登记进 `creativeops.NewResourceID` 白名单。端口按 §3 的窄接口要求，只声明 `PublishVerified / StatVersion / OpenVersion / DeleteExact` 四个操作加 `Driver/Bucket` 两个元信息读取，不暴露媒体的分片上传面。
- **导入协议**：`BeginImport`（冻结意图 + request_hash）→ `StageResource`（事务外上传，按声明校验大小与 sha256）→ `FinalizeImport`（短事务冻结版本）。对象 I/O 全在事务外；同 operation 重放返回原版本，同 operation 换内容报 `ErrImportConflict`。版本号在 Skill 行锁下发放，新 Skill 的行到 finalize 才写入，失败的导入不留空 Skill 占住 slug。
- **验证**：`make check-go`（全包）绿。creativeskill 6 个 DB 用例 + 2 个 digest 内部用例；store 新增 0045 形状与有损回滚用例。一次 `dataexport` 失败是 Docker 冷启动后容器就绪超时（`context deadline exceeded`），单独重跑与全量重跑均通过，与本次改动无关。
- **范围**：受信导入 CLI、`reference-direction@1` 种子导入、去掉 `go:embed`、目录端点与 `instruction_segments` 均属 S2/S3，本次未做；`creativeagent` 的 `SkillRegistry` 仍在用，`docs/dev/creative-agent.md` 记录的事实因此仍然成立。平台 Skill 的跨账号解析端口留到 S3 与其真实调用方一起写，本次只做账号内解析。

### change review（1 轮，无 blocking）

- reviewer：宿主 `multi_agent_v1` fresh reviewer，显式 `model: opus`。异构候选再次全部不可用——`codex` 的 node 入口崩（`@openai/codex-darwin-arm64` 缺失延续）、`gemini` 报配置无效；codex / gemini MCP server 也连不上。遵循「没有合格异构候选时回退同构最强模型」。
- 冻结目标：staged diff，SHA-256 `5eeae220e34c897e0962e8ea32a87bd74ad53eb3645d8b6206c6c0f00aa5c7a7`（37 文件 +1991/−112）。
- 结论：**0 blocking**，5 important + 7 nit，全部已修。
- **被证伪的一条**：reviewer 起初判定 OSS 的 `ForbidOverwrite: "true"` 会让重试 PUT 被 409 顶回、把 import 永久卡在 `preparing`（blocking 级）。它自己查了 `docs/dev/object-storage.md:51` 的既有记载「bucket 开启版本控制后 ForbidOverwrite 不生效」，而版本控制是本仓库的硬要求，于是撤回。记在这里是因为下一个读 `oss.go:164` 的人会产生同样的疑问。
- **I-1**：`BeginImport` 的幂等是裸 check-then-insert，并发同 operation 会让败者撞 UNIQUE 并抛出无类型错误而非重放。已改用基座既有的 `tx.LockCreativeOperation`（`creativeops.Executor` 同款）。
- **I-2**：过期 import 会被当活的重放回去，`expired` 状态无写入方。已实现过期转换与独立的 `ErrImportExpired`。**修的过程中撞到一个真问题**：最初把状态转换和错误放在同一个事务里返回，事务回滚把转换一起丢掉，测试直接抓出来（state 仍是 `preparing`）。改成事务内只置标志、提交后再抛错。
- **I-3**（直接回答我交给 reviewer 的问题）：错误身份合一的取舍本身没错，但 `creativeskill` 让端口错误穿透了自己的边界。`httpapi/creative_canvas.go:86` 那串 `errors.Is` 是**路由闸门**不是渲染——裸的 `versionedfs.ErrNotFound` 会让一次冻结版本字节丢失（服务端完整性事故）被渲染成 404「上传会话不存在」。已在 `creativeskill` 的两个 adapter 调用点翻译为 `ErrResourceUnavailable`。今天没发生只因 creativeskill 零导入方。
- **I-4**：我登记的 13 个文件全对，但漏了第 14 个 `creative_library_migration_test.go`——它裸调 4 次 `MigrateDownOneForTest`，注释写着落到 0038。这份注释在 0043/0044 落地时**就已经**错了，0045 让它错第三步；测试一直通过是因为回填由 `normalization_version` 驱动、与迁移深度无关，于是它承诺的「0039 回滚后重建」早已不再执行。已改成具名 roster `rollbackTo0038`，0039 的回滚与重建现在真的在跑并通过。
- **I-5**：`staged_objects` 不是完整清单——上传成功到写行之间崩溃会留下它看不见的孤儿。0045 的注释原文声称它足以支撑 cleanup，已改写为「回收必须按 import 的 key 前缀扫，不能只信这张清单」。同时补了非崩溃路径的自清理（提交结果未知时不删，遵守 `ErrCommitOutcomeUnknown` 的既有约定）。
- **nit 已修**：`based_on_version_id` 标为保留列（与 `marketplace_version_id` 一致）；`verified_manifest` 的注释说清它装的是整个 intent 而非 manifest（列名保留方案 §4.4 的叫法，不擅自改契约字段名）；`matchStaged` 的注释收窄到它真正能拦的范围；`objectKey` 的 `path.Clean(key)!=key` 是死代码——`path.Join` 已经 Clean 过——而真正的风险恰恰是 Clean 会让带 `..` 的段逃出命名空间，改为断言前缀存活；补「两个 import 同 revision 只能一个 finalize」与「manifest 变更必须改 digest」两个用例。
- **未修的 nit（residual）**：`platform/versionedfs` 无自有测试。纯搬运阶段由 creativemedia 的端到端用例覆盖，但它现在有了第二个消费方，S2 接入真实种子导入时应补一套 conformance。
- 修复后 `make check-go` 全包重跑 EXIT=0、lint 0 issues。

### change review round 2（1 blocking，由 round 1 的修法引入）

- 第一次派发被 429 掐断、无终态报告，按协议不计轮次；冻结目标重新核对未变后在同一 reviewer session 重发。
- 冻结目标：`d34bc920aa335fea620a37b7bfae7999db5a8a6acaa01a15f20c95d85037d5ae`（38 文件 +2228/−136）。
- 12 条（5 important + 7 nit）全部 `resolved`，修法经复核正确。三条值得记的确认：**I-2** 的「事务内置标志、提交后抛错」在并发下之所以无懈可击，恰恰是因为 **I-1** 加的 advisory 锁把同 operation 的重放串行化了——两个并发重放都拿到 `ErrImportExpired`，转换恰好发生一次；**I-3** 的 `%w: %v` 确实切断了 `errors.Is`，全包 5 个 `s.objects.*` 调用点无第三处穿透；**I-4** 的 0039 回滚与重建已真实执行。
- **blocking（我引入的）**：round 1 给 StageResource 加的「事务失败则自删已发布对象」分支有两条早退路径排在 `stagedFor` 认领之前——`State != "preparing"` 与过期判定。local 驱动的 object version 是内容 sha256，而并发 stage 同一路径的字节必然相同（都先过 `declared.SHA256`），于是两个调用算出的 `(Key, Version)` 完全一致，「删掉自己的」就是删掉赢家的。后果：`FinalizeImport` 全程无对象 I/O，照常冻结成功，而 `ReadResource` 此后永远失败——**已冻结版本的字节被永久销毁，冻结本身报成功**。不需要真并行，管理员 CLI 一次客户端超时重试就能触发。OSS 上不触发（每次 PUT 不同 versionId），而测试全跑在 local 上。
- **修法**：认领提到所有状态闸门之前——「这个路径是否已被他人 staged」与 import 此后走到什么状态无关；过期判定改用 `outOfWindow` 以保持与 I-2 一致的错误身份。
- **回归测试**：`TestALosingConcurrentStageMustNotDeleteTheWinnersObject`。用包装 `ObjectPort` 的 `gatedPort` 把第一个调用卡在 PublishVerified 里（字节已落盘、记录未写），让第二个调用跑完整个事务，从而把竞态变成确定性交错。已验红绿：改回原顺序即 FAIL，恢复即 PASS。这也补上了 reviewer 指出的「creativeskill 零并发测试」。
- **顺带**：`creative_graph_migration_test.go` 的 `for range 6` 今天正确但仍是裸计数，一并换成具名 roster，避免下一个迁移再手改数字。
- **记一笔（非缺陷）**：`%w: %v` 的包装保留了底层错误文本但丢掉了身份，`versionedfs.ErrUnknownResult`（「bucket 未开版本控制」）这类可诊断信号只剩文案。受信 CLI 路径可接受。`ObjectPort` 的 `StatVersion` 目前无调用方，保留是因为方案 §3 明确列了这四个操作，S2 的回收命令很可能用到。
- `expired` 标志是闭包外变量，依赖 `WithTxScope` 只执行闭包一次。当前 `withinTx` 无重试逻辑，成立；将来若给事务加重试，这个标志会变脏。
- 修复后 `make check-go` 全包 EXIT=0、lint 0 issues。

### change review round 3（终轮，blocking resolved）

- 冻结目标：`2f88b294309a3cbf4f5b6df5ba5ce21194a1eac8cdee0e14b5f55a40877c6a46`（38 文件 +2333/−137）。三轮累计 1 blocking + 5 important + 8 nit，全部 resolved；reviewer 判定完整候选无 blocking、建议合回。
- **修法的正确性有比「测试通过」更强的依据**：reviewer 审了全部 4 个 `loadImport` 调用点，三个写入方（BeginImport、StageResource 记录事务、FinalizeImport）全部 `forUpdate=true`，唯一 `false` 的是只写不读的预检。`creative_skill_imports` 没有绕过行锁的写入方，因此「认领时未 staged」严格蕴含「回滚时仍未 staged」，并发方无法在该窗口提交。认领前置后，所有触发 blanket delete 的出口都落在这条互斥之下。
- 残留一条不修：`loadImport` 自身驱动级读失败时仍会 blanket delete，此刻无从判断并发方是否已提交引用。需要「DB 读失败」与「并发赢家」同时发生，不值得为它加复杂度。
- 回归测试的确定性是结构保证而非概率：`sync.Once` 只放行第一个进入者，而主 goroutine 阻塞在 `<-gate.entered` 上直到有人进入，此时只有子 goroutine 在跑 StageResource。
- **两个断言封的是两种独立回归**，不是冗余：断言「late 返回 nil」封闸门顺序；断言「`ReadResource` + `bytes.Equal`」封删除决策本身——若有人保留认领块但把 `superseded = &mine` 写成无条件，只有后者会红。reviewer 明确不建议再补「直接 stat 对象存在」的用例：那比 `ReadResource` 更弱（后者既开了记录的精确 key/version，又对着冻结时的 sha256 重验内容），且会把测试耦合到 local 的磁盘布局。

#### 转交 S2 的两条具体事项（不是笼统的「versionedfs 缺测试」）

1. **conformance 套件里最该先写的一条**：认领块的正确性现在依赖「local 同内容必同 version」这个前提，而它只写在注释里，没有任何测试或类型把它钉住。若将来有人把 local 的 version 改成随机值，认领块会静默退化成「每次都判为不同对象」，于是每次并发都删一次。
2. **local 上不可达的分支**：`superseded != nil` 的「该删而删」路径在 local 驱动上结构性不可达（同内容必同 version），只在 OSS 上存在。S2 补 OSS fake 时一并覆盖。
