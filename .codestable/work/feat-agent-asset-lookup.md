---
type: feat
status: completed
---

# 助手查找资产库与找回会话素材

## 目标

实施 V6-035/V6-077 及当前会话确认的粘贴图片入口。摄影师于 2026-10-13 明确授权开始实施，并在本会话后续明确授权提交代码。

## 现场

沿用 `.worktrees/creative-workspace-redesign`。原设计：`.codestable/features/2026-10-13-agent-asset-lookup/agent-asset-lookup-design.md`，保持原文件不改。开始时已有未提交需求修改和设计，均保留。

结构查询使用 Tier 2；图项目 `Users-samson-workspace-my_project-customer_manage_platform-.worktrees-creative-workspace-redesign`，generation `2026-09-28T12:10:12Z`。相关文件 coverage 为 metadata_changed/not_tracked，已直接读取当前源代码，不依赖旧图作否定结论。

## 边界

- 资产库是按需读取的素材与知识来源；每次发送使用 account_library，清单只含本条引用。不是每次工具调用弹窗确认。
- asset_id 是资产库条目；content_revision_id 是内容版本。暂存上传与会话引用无需资产库条目。
- 已发送消息的引用无时间过期条件；未发送暂存会过期。粘贴走现有 staged 上传。
- 撤销优先于全部读取分支，避免当前画布、资产库或新清单绕过本对话历史撤销。历史 read_asset 正文不会通过文字历史或素材摘录绕开读取检查。
- 直接 content_revision_id 建节点只走本条引用、画布和会话三条规则；库素材使用 asset_id 加内容修订，保留乐观校验。
- 同消息多素材分页增加 before_revision_id/next_before_revision_id，与 ordinal 组成游标，防止翻页漏项。
- generate_node 实际返回 content_revision_ids 数组，applied 后全部登记；compress_image 的成功状态字段是 state=applied。
- 本期维持 read_asset 的文本/图片读取能力；视频、音频、链接可检索和放入画布，不新增视频/音频解码或链接抓取。

## 证据

- 初始后端两个测试红灯：account_library 不接受本条修订；历史超过 20 条后不可读取；修改后通过。
- 真实数据库：对话分页与隔离、撤销、库检索/画布/保存幂等、成功生成结果引用、工具可见性、旧说明版本拒绝、模型工具链通过。
- 真实媒体上传：发现并修复 UploadView 缺少暂存 content_revision_id 的回执；消息引用在候选过期后仍保留图片，移除最后引用后进入 GC；测试通过。
- make check-frontend：486/486；原助手浏览器回归通过；新增粘贴图片浏览器验收通过，含旧版说明重新告知、当前引用清单、手机宽度无横向溢出。
- 全量 make check-go：52 个包通过，build 与 lint 通过。最后一项消息预算修复后，重新执行 make check-go PKG=./internal/creativeagent/...，build、lint、全部包内测试通过（40.718s）。make generate-check 与 git diff --check 通过。
- 真实模型两场景通过（库检索 78.40s；25条以前的会话图片找回 96.32s），22 条工具调用。真实模型运行在审查加固前完成；最终候选另以完整数据库回归和独立审查验证。
- 独立 change review 三轮完成：首轮画布预览与分页历史来源校验问题、第二轮多来源消息预算问题均 red→green；第三轮无 blocking/important/nit，审查者独立重跑三个回归通过（5.590s）。最终候选 SHA-256：cf407595c6716be7b7ab17dc3e003ca43e886d258a96a8b5a2393101c32954a6。
- 原助手、首页、新粘贴三套浏览器验收均通过。可核验日志和真实模型工具记录见 [evidence/agent-asset-lookup](evidence/agent-asset-lookup/)。

## 验收

以设计 §6 与 V6-035/V6-077 为准；另验证粘贴、暂存上传、发送引用、长期保留和多轮找回链路。真实模型仅使用隔离测试账号和合成色块图片，生成后端使用桩，仅验证工具调用与图结构，不支付真实生图费用。

## 状态与未决

实施、验证和独立 change review 已完成；无遗留阻塞；摄影师已授权提交本次实现与修复。原有需求与设计改动保持不动，留在工作区。

毕业清单：读取边界、工具契约、分页、上传与 GC 语义进入 docs/dev/creative-agent.md 的 2026-10-13 节；旧草稿入口说明在 docs/product/creative-canvas-system/modules/harness.md 指向当前实现。约束已由回归测试覆盖，未创建重复 lesson。此游标保留交付记录与日志指针。

## 交付后修复：标签搜索命中已分组图片

2026-10-13 反馈的 search_assets 内部错误：工具补充分组名称时误查 creative_groups，实际表为 creative_asset_groups。真实数据库对照复现：同一带标签图片未分组时成功，加入分组后出现 SQLSTATE 42P01；修正表名后通过。新增 TestSearchTaggedImageReturnsGroupNames，覆盖两种状态及返回标签/分组名称。make check-go PKG=./internal/creativeagent/... 的 build、lint、全部测试通过（40.404s）；未提交。日志为 evidence/agent-asset-lookup/tagged-group-search-{red,green}.log。上文独立审查 hash 对应此前候选，不包含本次一行修正与新增回归；本次不改变查询权限、持久化模型或公开契约。约束归入该回归测试，不另建 lesson。

## 审计跟进：筛选去重与搜索字节预算

摄影师明确要求修复两项 P2。真实 PostgreSQL 与模型桩先复现：助手读取后 source/query 筛选漏项；20 条各带 50 标签的素材返回 74,460 字节，运行触发 creative_context_limit。

已修复：去重只比较符合 source/query 的引用，含同消息多角色；搜索超预算仅缩减标签/分组列表并给出 tags_omitted/groups_omitted，保留全部素材、总数和原游标。无法容纳核心结果时返回可恢复工具拒绝。工具说明与 docs/dev/creative-agent.md 同步。

新增四项数据库回归覆盖读取后多条件检索与分页、同消息多角色、21 素材分页和模型继续缩小查询、JSON 转义后的大归类。make check-go PKG=./internal/creativeagent/...：build 通过、lint 0 issues、全部测试通过（43.327s）。独立修复审查通过，无 blocking/important/nit，审查者另跑四个新用例及既有隔离分页用例通过（4.950s）。修复 patch SHA-256：e39bf98058c3e8c731cd7eb24c61b864e159b5c19cb0ac66cd6dbaf8257be4b6。证据见 evidence/agent-asset-lookup/audit-fixes-{red,green,check}.log。未提交。
