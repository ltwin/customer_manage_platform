---
status: proposed
version: 0.2
created: 2026-09-10
---

# 第三批模块：实施切片与验证

本批设计入口为 [Gateway](gateway.md)、[Harness](harness.md)、[API/SSE](agent-api-events.md)、[数据与事务协议](gateway-harness-data.md)。GH编号只是局部开发依赖，不替代整个Epic正式执行队列。当前没有接通真实模型或实现业务服务。

| 切片 | 依赖 | 可验收交付 |
|---|---|---|
| GH-01 Gateway准入与适配 | CM-01事务桥、模型接入配置 | 正式schema、Catalog、Reserve/Prepare/Dispatch、两种供应商协议的统一契约测试；至少一个获准真实供应商通话。验证工具碎片/缺失usage/超限、关闭SDK隐藏重试、不能静默切模型 |
| GH-02 会话/输入/附件与创建运行 | CM-02/03、LC-01/02、GH-01 | 正式OpenAPI、消息/选区输入、Skill目录、外发同意、附件草稿及媒体交接、slot与River原子入队；刷新能读回真实运行 |
| GH-03 Harness与实际领域工具 | GH-02、LC-03/04 | Eino ADK下的6种业务工具及reference-direction@1，受控Skill/资源/上下文读取工具；真实检索与节点写入，固定读集、范围控制、提案、结果核验；同操作回放，手工变更冲突保留 |
| GH-04 取消、恢复与核算 | GH-01/03 | 旧epoch隔离、未知请求核实、终态恢复已有步骤、hold/实际用量/费用更正、消费保留清理；费用不得驱动终态重开 |
| GH-05 SSE与整体闭环 | GH-02/03/04、LC-05 | 鉴权fetch SSE、快照补读、取消与断流、回收保留联验、真实模型固定案例与部署降级演练 |

GH-01不先建一个裸chat代理再补预算；GH-02不先提交run再异步“顺手入队”；GH-03不直接根据助手文本改前端节点。每片只启用其真实完成能力，模型/Worker不可用时人工库和画布仍可工作。

## 1. 实现前的适配准入清单

首个供应商/型号取自实际获准配置，不虚构凭证、报价或可用接入。GH-01需记录明确部署版本、返回model别名、上下文/图片/工具能力、价格配方及币种、usage完整度、可靠未受理分类、查询/取消能力、传输资源限制、隐式重试关闭方式。没有可靠查询能力的适配允许接入，但unknown必须能解释并结束等待，不能宣传恰好一次或自动无费恢复。测试模拟第二种供应商协议证明内部接口不依赖首家格式；若实际启用第二家，也必须单独通过真实接入验收。

River/pgx桥接版本、同事务入队/回滚、至少一次重投、job救援与业务epoch兼容在GH-02接run之前证明。协议探针不包含River库迁移或InsertTx，因此不能代替此项。媒体agent_attachment目标在GH-02与媒体正式schema/目标union一起实现，不把旧13表草案直接当已支持附件。

## 2. 故障与验收矩阵

| 场景 | 必须观察的行为 / 需求 |
|---|---|
| 两供应商协议、实际能力不匹配 | 统一完整结果和工具调用；不支持输入在派发前拒绝，音视频元信息说明可见 / AG-01/08、FLOW-09 |
| 重复CreateRun、事务回滚、入队失败 | 202固定，一个run/消息/reserve；任一失败整体不可见 / AG-05/07 |
| 选区未保存、上下文超限、授权不覆盖搜索结果 | 不读取假快照、不静默截断；尚未获准的工具结果不外发，草稿/本地结果仍可见 / AG-02/08 |
| 附件ready→send与过期/媒体候选并发 | 一个持久接收目标，message/inputs先接棒后释放临时根，不自动收藏 / AG-01、LIB-06 |
| 3张自然光参考+方向文字，只有2张符合 | 创建真实2张和方向节点、明确不足与来源；UI回复必须与真实回执一致 / AG-03/04、FLOW-04 |
| model stream工具参数分片/截断/重复 | 完整validated结果才能生成工具步骤；同model-step/index仅一次 / AG-07 |
| 取消先于派发、派发先于取消、COMMIT unknown | 第一种无外发；第二种可能收费但取消后无新工具写入；未知不重领许可 / AG-05/07 |
| Eino checkpoint与业务提交间隙、框架升级 | 固定框架版本/执行帧映射，已提交工具回执复用；缺一致checkpoint不盲Query；旧epoch/不兼容payload拒绝，原作品保留 / AG-05/07 |
| Skill渐进加载、摘要/卸载材料再读 | 固定目录/包digest，受控虚拟资源；摘要同意/预算覆盖原来源，辅助调用计入run；完整内容不因缩减丢失 / AG-02/04/08 |
| Worker lease过期、新worker接管、旧worker迟到 | slot不被旧claim释放；epoch过期无效果，已有工具回执不重复写 / AG-05/07 |
| 人工改字后旧Agent覆盖、改变选区、提案过期 | 固定输入不变、旧前置条件冲突、保留双方；不自动采用最新版本 / AG-02/06/07、FLOW-05 |
| 等待/关闭/到期与未知结果 | slot有明确释放路径，hold保持、费用待核实；终态不重开 / AG-05/08 |
| 未受理零费核实、重试重新占额与竞争 | 同request unknown→prepared后可重新准入；A释放20、B占20时A不能派发，B释放后才可重建hold；取消/到期/次数上限拒绝 / AG-05/08 |
| 自身写入后的连续工具与外部写入 | A拓扑7→8，B只承接自身回执8；人工9冲突；纯数据命令不推进拓扑，已prepared命令不改hash / AG-03/07 |
| 最终提案只采纳与继续运行 | apply_only同事务落库并终结200；继续模式202需再检查deadline；slot/版本失败保留waiting_apply / AG-06 |
| 月边界、多调用方、请求认领、部分usage | 单一Gateway账本；不重复预留，跨周期run上限不重置，已知分量+未知hold不漏算 / AG-08 |
| 重复/乱序/分叉measurement、实际超预算 | 当前值差额结算，分叉挂起；超支实际全额保存并阻止新准入 / AG-08 |
| SSE快照/订阅间写入、GC全清、慢客户端 | 补齐或要求快照，seq/chunk不重复；断线不取消，慢消费者不阻塞Worker / AG-01/05 |
| 用途撤销、跨账号ID、恶意工具参数/素材提示 | 不绕过真实授权；内容指令不扩展工具白名单 / AG-07/08 |
| 清运行载荷、消息/节点仍引用媒体 | 已采纳作品与永久消息正常，过期证据不可假称可恢复；pending消费/unknown不早清 / FLOW-06 |

模型评估固定上述任务与负例，保存实际model/catalog/Skill/schema版本、输入案例摘要、实际工具回执、是否达成、耗时和用量。权限/重复副作用/旧版本覆盖类案例要求零违规；任务质量与时延基线在真实模型运行后记录，不用预设演示回复填充通过率。此次不把SQL探针结果当LLM质量或用户体验证据。

## 3. 本轮已执行的存储协议探针

运行 `python3 docs/product/creative-canvas-system/modules/run-schema-probe.py --scope gateway-harness`。复用storetest的隔离PostgreSQL17、日志+端口ready等待；加载12张协议字段投影表，临时包/容器结束清理，不连接应用数据库。

- TestReservationClaimAndPartialSettlement：稳定调用方预留重入只占一次、初始预留认领唯一、账号隔离、6已知+14保留及结算事务回滚。PASS，0.06s。
- TestConcurrentCostCorrection：两事务同时基于10更正至8/7，恰一方推进；重复无副作用、随后核实到7，预算与当前位置及回执一致。PASS，0.04s。
- TestCancelFenceAndDispatchIntent：分别验证取消/派发两种已串行的顺序，unknown尝试唯一约束与旧claim不能释放新slot。PASS，0.06s；并非真实HTTP与worker竞态实测。
- TestEventRollbackAndPruneCursor：事件与计数回滚原子、快照水位后的尾部可读、事件全清后仍识别过旧游标。PASS，0.04s；未运行浏览器SSE。
- TestResultConsumptionAtomicity：消费回滚保留pending、不遗留工具计划；重复成功消费只有一个工具计划；原工具调用身份唯一及无FK检查。PASS，0.04s。
- TestVerifiedRejectionRearmsOnlyWithBudget：unknown核实为未受理后同request回prepared；A释放20、B占满时A不能创建第二attempt；B释放后A重建hold/generation才派发，重复不能再领许可。PASS，0.04s。这是确定性竞争顺序的SQL实验，不是供应商查询或完整Rearm服务验收。
- 最终6项测试包总2.956s。这些是SQL协议实验，没有验证完整服务守卫、供应商网络、分量定价配方、River桥、React前端或真实模型费用。

正式DDL、服务与适配测试必须走上述切片，不能直接将协议字段投影放入迁移目录。共用runner原有两个scope的兼容验证记录见本批work证据。


Eino框架验证及生产接入前仍须补齐的证据见[接入方案](../eino-adoption.md)与[八项实验记录](../probes/eino/README.md)。本表取消自研通用loop安排，由Eino承担循环、业务层承担持久效果与执行守卫。
