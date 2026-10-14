# V6-078 助手整理资产库：实现与验收

2026-10-13 本地完成，工作树 `creative-workspace-redesign`，基线 `33361de`。owner已批准设计及实施；**owner现已明确授权提交**。原有V6-035/077及草稿改动保留。

## 实现

- 新增 `organize_assets`、`manage_library`、`trash_assets`、`restore_assets`，分别支持最多50素材、20结构动作、50素材、50素材；不提供永久删除/清空回收站。
- 新增 `read_library_result`：同账号同会话跨运行，90天完整前后值及关系明细分页。写前冻结计划与命令，已提交回执优先；unknown先停止剩余写入，后续查询只核对。锁顺序为操作锁→调用行→领域锁。
- `search_assets`、`read_library`及保存工具升级v2：完整描述、revision、筛选、签名库版本游标、条数/序列化字节双预算；超大关系提供继续入口，版本变化明确stale。不存在500项硬截断。
- 描述新增/更新统一1000 Unicode码点；旧1001–2000码点保留。前端拒绝超限但保留输入，未改描述不随其他字段提交。API写入/读取约束分开，生成物同步。
- 保存绑定节点数据版本，防止中断后保存另一个内容修订；同修订复用保留原文字，返回实际新建/复用标签事实。
- 基础提示词独立go:embed，覆盖工具注册与Skill拼接顺序。结构删除报告真实影响；覆盖原文与新入库先展示内容、等确认。

## 验收映射

| AC | 结果与证据 |
|---|---|
| AC-01 整理文字和归类 | PostgreSQL读回与结果一致；两阶段partial、完整前后值及关系明细。`library_organize_test.go` |
| AC-02 十种结构动作 | 顺序失败停止、实际删除计数、含回收站关联；父组直接成员0与上提子组数；标签复用保留颜色。`library_organize_test.go`、`discovery_test.go` |
| AC-03 回收站 | trash/restore与既有引用权限回归；纯trash元数据不扩张内容权限；浏览器页面恢复通过（API桩）。无purge/empty_trash Agent工具 |
| AC-04 先展示/确认 | 真实模型创作只建议不写；1628码点旧描述完整展示后等待；新存入展示标题、描述、分组、标签，明确同意后才保存。见[确认日志](evidence/live-confirmation.log) |
| AC-05 上限、并发和恢复 | 51素材/21动作写前拒绝；Metadata后、Organize后、进度登记前恢复；unknown持久后只核对；旧attempt及同attempt迟到失败不遮蔽成功，失败先登记则拦截迟到写入。四项独立复现全部修复 |
| AC-06 真实看图整理 | 20张合成图片分8/8/4三批；每个修订先读后写，20张均有描述/标签，新增标签搜索20全命中。[调用记录](evidence/live-20-images.log) |
| AC-07 完整分页 | 完整1000字及旧长描述、合法转义长名称按实际页尾缩页；551归类、精确筛选、单素材关系续页；同版本无漏项/重复，改库后stale。`discovery_test.go`、`asset_lookup_audit_test.go` |
| AC-08 大结果/描述 | 50素材×25标签，1300完整明细且无额外业务写入；小预算写前拒绝；1000码点成功/1001拒绝，旧2000保留，emoji按码点计数。领域/Agent/浏览器均验证 |

附加验证：账号/会话/撤权隔离、90天过期与清理；0064 additive migration、非空回滚保护、空表下行重建；保存时节点变化拒绝、created/reused事实、复用不覆盖原文；准确回执type/payload/CreatedAt哈希验证。基础提示词工具覆盖和Skill拼接顺序通过。

## 门禁与环境分诊

- 前端 `make check-frontend`：486/486，通过。[日志](evidence/frontend.log)
- `make generate-check`：通过。[日志](evidence/generate-check.log)
- 最新跨包 `make check-go`：全库build/lint通过，45个测试包通过、7包在Docker容器启动时timeout，未进入断言；**整轮命令退出2，不宣称整轮绿灯**。[原日志](evidence/go-full.log)
- 7个失败包逐个原命令重跑全过：creativegeneration、creativemedia、creativereference、customer、dataexport、package、httpapi。[重跑](evidence/go-startup-retries.log)。之前轮次启动失败落在其他包，所有失败均为容器启动期，失败点游走且独立重跑全绿；按仓库三条件记录环境问题。未修改全局并发度或绕过断言。
- 最后提示文字及夹具修改后 `make check-go PKG=./internal/creativeagent/...`：全库build、包lint0issues、Agent全测48.983s通过。[日志](evidence/go-agent-final.log)
- 两类确定顺序、跨attempt及同attempt恢复专项通过；独立reviewer重建原四个反例全部通过。[恢复回归](evidence/recovery-regressions.log)、[保存与批次边界](evidence/save-and-limits.log)
- 浏览器实际组件：1000emoji可保存、1001拒绝且输入保留、旧2000未变不提交。[日志](evidence/description-browser.log)
- 完整资产页浏览器回归通过；后端接口为内存桩，不冒充浏览器连接真实数据库端到端。[结果](evidence/assets-browser.json)
- 既有真实模型资产查找、会话素材找回、画布编辑/生成流程通过；图片生成服务为桩，只验模型工具编排。[旧流程日志](evidence/live-before-prompt-refinement.log)

## 真实模型耗时及修正

仅隔离账号和合成图片发往获准供应商，模型 `gpt-6.1-sol`；没有用户资产。成本为测试账本booked值，非账单估计。

| 场景 | 耗时/读取 | 账本成本/报告token | 结论 |
|---|---|---|---|
| 历史提示修正前一次 | 10m36s / 132次 | $1.862766 / 1,034,437 | 功能完成，但反复读图 |
| 仅补基础提示后 | 17m25s / 80次 | $2.662363 / 1,632,642 | 20张已写完，最终核对前预算拒绝；此轮不算通过 |
| 将历史省略明确为“仅本轮未附加”后 | **5m32.816s / 20次** | **$0.715460 / 450,870** | 强化unique/先读后写/标签检索断言通过 |

历史图片被移出当前上下文时，旧提示容易被模型理解为此前也未看过。只改基础提示仍不稳定；修正具体省略提示后，本次复测不再重复读图。以上为单次样本，不承诺固定耗时或成本；最终低于设计的10分钟观察阈值，暂不引入批量缩略图新协议。

确认验收曾遇到夹具模型标识不匹配、重复文本原文逐字展示断言失败，以及一次运行进入creative_llm_unknown；不把后两者武断归因为输出截断或供应商故障。均保留在[失败记录](evidence/live-failed-before-notice-fix.log)。最终采用1628码点编号场景原文、8192夹具输出上限、直接创建节点，断言仍要求完整原文及未确认零写入，真实模型全部通过。产品预算未修改。

## 审查与交付边界

核心实现三轮独立change review通过，四个blocking全部resolved，最终无blocking/important/nit。[审查记录](agent-library-organize-review.md)明确列出冻结指纹、复现证据，以及审查后仅两个文件的提示文案/测试夹具收尾和对应自验证；未发起第4轮规避轮数限制。

确认行为按批准设计属于模型提示词约束，尚无服务端确认门；未来确认交互归V6-033。未部署；本轮按owner明确要求提交。工具结果保留90天，超期明确提示，不承诺永久元数据历史。

## 提交前补充验证

2026-10-13 owner明确要求提交。当前工作树另包含本功能的运行守卫修补：每个新的资产库写入在事务内重新检查运行状态、claim/epoch、租约/截止时间、Skill与授权；已提交命令仍可按回执核对。对应取消、租约过期、epoch/claim变更、超时、撤销授权以及已提交前缀保留测试已随Agent包完整检查通过。证据见 [提交前检查](evidence/go-precommit.log)。这项后续修补不属于原三轮独立审查冻结目标；未宣称获得额外独立复审。

本次提交保留之前资产查找/会话找回的未提交需求草稿；总表仅暂存V6-078增量。最终源文件指纹见 [提交指纹](evidence/commit-files.json)。

提交前 `make lint` 通过（保留现有前端warning，不把warning写作零问题），见 [lint日志](evidence/lint-precommit.log)。归档日志仅规范化行尾空白，未删改测试结果或工具调用内容。
