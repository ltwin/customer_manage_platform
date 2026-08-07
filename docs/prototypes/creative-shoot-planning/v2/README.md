# 创作型拍摄策划首版前端原型 v2

本目录是 `creative-shoot-planning` 首版 epic 的**受版本控制参考快照**，来源为本机原型工作目录 `frontend/proto-design/planning/`，同步日期为 2026-08-05。原工作目录受 `frontend/.gitignore` 管理；后续 feature design、实现、QA 和验收必须引用本目录，不能依赖某台机器上的 ignored 文件。

## 页面索引

| 页面 | 用途 | 主要 roadmap item |
|---|---|---|
| [index.html](index.html) | 原型入口、页面地图、8 条 feature 的界面落点与 H1–H5 红线 | 全局 |
| [planning-workspace.html](planning-workspace.html) | 策划台账、详情六分区、状态流转、CRM、参考素材、分享反馈和经营草稿 | `shoot-plan-core`、`shoot-plan-crm-integration`、`planning-reference-assets`、`plan-share-collaboration`、`plan-assignment-reminders`、`plan-business-feedback` |
| [plan-ingestion.html](plan-ingestion.html) | 原文与图片摄取、候选确认、丢弃恢复、原子保存 | `plan-ingestion-capture`、`planning-reference-assets` |
| [run-mode.html](run-mode.html) | 独立移动端逐镜执行、跳过原因、参考速查、镜头清单和收尾 | `shoot-plan-core` |
| [shared-plan.html](shared-plan.html) | proposal/full 匿名分享、失效态、反馈与认领凭证 | `plan-share-collaboration`、`plan-assignment-reminders` |
| [planning.css](planning.css) | 本原型共享视觉样式 | 全局 UI 参考 |

`*.artifact.json` 保留原型工具生成时的来源元数据。页面中指向用户中心、今日概览和档期日历的跨原型链接只用于表达 AppShell 上下文，不属于本快照承诺的可运行路径；本目录保证上述五个 planning 页面及其内部跳转可读。

## 权威优先级

原型不是领域或 API 真相源。实现时按以下顺序处理冲突：

1. `.codestable/roadmap/creative-shoot-planning/creative-shoot-planning-roadmap.md`：产品边界、权限、数据生命周期、跨模块接口、错误码和事务语义；
2. `api/openapi.yaml` 与生成类型：实现期机器契约；
3. 经确认的 child feature design：单 feature 内部状态与实现选择；
4. 本原型：信息架构、用户语言、视觉层级、交互路径和关键状态参考。

若原型与前 3 项冲突，不得让实现按原型猜测；必须回到 `cs-epic` planning/update 或对应 feature design 解决。页面中的“评审注释 · 正式产品不显示”只解释工程契约，不得进入正式 UI。

## 本快照固定的交互意图

- 策划是非强制辅助工具；既有客户、订单、档期、报价与提醒不能出现“缺策划”提示或阻塞。
- 工作台默认进入策划列表，详情采用创作 brief、镜头表、准备项、参考素材、分享与反馈、经营草稿六分区。
- 摄取是“原文/图片 → 可编辑候选 → 一次原子保存”，不调用 AI、不 OCR、不抓取链接。
- Run Mode 是执行专用界面，不提供镜头结构编辑；断网明确失败并保留待重试，不伪装成功。
- captured 撤销与 skipped 改 captured 都保留 result 历史；错误勾选/错误跳过原因从工作台“执行历史与纠错”追加误记作废事实，不删除原记录。completed 后纠错必须先 reopen。
- proposal 只开放公开创作摘要、moodboard、公开规模摘要和整案反馈；full 才开放逐镜、逐条反馈与准备分工。
- 计划造型/场景数在创作 brief 的公开规模区维护，business 区只读消费；客户页永不展示价格、成本、经营预估时长、加价或经营草稿，“计划拍摄时长”只能由公开执行时间窗计算。
- 摄取中的预期负责人和默认提前量都只是未来认领输入；只有客户正式认领 readiness 后才快照提前量并生成摄影师核对提醒，on-site support 不生成 due。快照在认领生命周期内不可变；设错时通过撤销、修改默认值、重新认领纠正。
- 客户认领前由浏览器安全生成 receipt，成功后只展示当前内存中的本地 secret；原型中的短码只是静态交互示例，不是生产格式、随机性、服务端响应或持久化契约。昵称不是身份，也不是提醒收件地址。
- 解除关联、归档、删除有执行记录的镜头、轮换/撤销分享、撤销认领和应用经营草稿等破坏性操作必须先说明副作用并确认。

## 生命周期

- 当前 v2 与同日 roadmap update 一起接受独立 review；review 通过且 owner 确认 roadmap 后，v2 视为冻结参考。
- 后续只修错别字或无语义的展示缺陷可以原位更新；信息架构、关键状态或用户流程发生实质变化时，新建 `v3/` 并回到 roadmap planning/update 评估影响。
- 原型中的静态示例数据、动画延迟、占位图片、控制栏和具体视觉数值不是 API 或数据库契约。
