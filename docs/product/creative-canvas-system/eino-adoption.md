---
status: proposed
version: 0.1
created: 2026-09-10
---

# Eino ADK 接入决策与验证

摄影师明确推荐Eino，并授权继续适配验证。默认Agent技术方向调整为 **Eino ADK + 业务执行控制 + LLM Gateway**。Eino承接通用Agent机制；业务权限/内容快照/命令回执/费用事实保留原领域归属。该调整替代此前“自行编写通用ReAct循环”的实现安排；不表示全部FND-01或Agent已实现。

## 1. 本次锁定的基线

- Eino：`github.com/cloudwego/eino v0.9.19`，模块发布信息时间2026-09-01；源码tag与模块校验通过。模块sum为 `h1:i71YUBK3nwY4L53dkzRgZpAcPSZ4v4eRponN7W9sDtk=`，完整依赖锁见[probe.mod](probes/eino/probe.mod)/[probe.sum](probes/eino/probe.sum)。
- 验证工具链：Go1.25.5、darwin/arm64；通过当前backend模块的replace引用实际storetest和pgx5.10.0，使用标准PostgreSQL17隔离容器。
- EinoExt按组件独立发布。本次注入自己的模型/Skill/存储适配接口，**未引入EinoExt模块**；正式供应商实现如采用EinoExt，FND-06须分别锁定实际组件版本，不能填一个不存在的统一EinoExt版本号。
- 采用`schema.Message`、ChatModelAgent、Runner、接口式Handlers；v0.10 alpha和新Agentic消息路径不纳入本次基线。生产backend/go.mod/go.sum未修改，八项实验编译的是独立临时模块及所用依赖，不代表全应用在新依赖下已通过构建。

当前网页和检索摘要可能混合API世代。例如本次Context7某份自动文档摘要给出Checkpoint Save/Load/Delete，但v0.9.19实际 `adk.CheckPointStore` 是 `Get/Set`，Delete为可选接口。实现以固定tag源码和编译结果为准；不能将可变main文档片段当锁定版本契约。[v0.9迁移说明](https://www.cloudwego.io/zh/docs/eino/release_notes_and_migration/eino_v0.9._agentic-runtime/eino_v0.9_migration_notes/)、[固定版本Runner源码](https://github.com/cloudwego/eino/blob/v0.9.19/adk/runner.go)。

## 2. 职责与调用方向

| 层 | 拥有的职责 | 接入方式 |
|---|---|---|
| RunApplication / ExecutionController | 账号、run/step、期限/slot/epoch、外发同意、提案采纳、命令及实际效果 | 创建/恢复Eino运行的唯一业务入口；每次副作用前检查持久执行权 |
| Eino ADK | ChatModelAgent ReAct、Runner、中间件、Skill发现加载、AgentAsTool及中断恢复机制 | Harness内部使用框架运行循环，不在外面再写一套竞争的LLM→tool循环 |
| GatewayModelAdapter | Eino模型输入/输出与Gateway契约转换 | 实现固定版本BaseChatModel/ToolCallingChatModel，Generate/Stream均经Gateway；WithTools返回独立配置，不修改共享实例 |
| BusinessToolAdapter | 将Eino工具调用交给已注册的画布与资产业务用例 | 参数/来源别名→已准备step/operation→同事务领域命令与回执；不返回数据库连接或任意SQL |
| Checkpoint / Context Backend | Eino状态与有界上下文材料存储 | 账号/run隔离的PostgreSQL适配；版本/生命周期/GC引用由本系统控制 |
| River | 持久任务唤醒、延期/救援和Worker管理 | 唤醒应用驱动器，后者按当前事实选择Runner.Run/Resume或只核实；队列重投不授权重放模型或工具 |
| Product Event Adapter | 将框架事件映射为产品消息与通知 | 先写消息/步骤/实际状态，再发已有SSE；Eino原始AgentEvent不直接充当已保存回执 |

Eino Graph用于Agent内部计算和流程编排。创意画布仍保存作品与参考关系，只有显式业务工具会操作画布。取消由RunApplication先失效epoch，再尽力取消Eino/Gateway的活动调用；首期不启用TurnLoop自动抢占并改变现有“新消息/取消/补充”的产品语义。

## 3. 上下文管理

保留原ContextBuilder的业务职责：发送前保存选中内容、冻结修订和来源、有限展开上游、校验媒体用途与外发同意。Eino处理送给模型的运行上下文；run_inputs与execution_read_set不能被summary或middleware改写为另一份业务事实。

上下文处理顺序固定为：业务输入清单/已获准引用 → 已授权Skill指引 → 有界工具结果 → Reduction卸载 → 达到配置阈值时Summarization → 最终外发/能力/预算守卫 → Gateway。实际摘要生成自身也是一次Gateway模型请求，必须先校验将用于摘要的全部输入、当前供应商/用途/预算；不能等摘要完才检查原始材料是否允许外发。模型、摘要和子Agent的每次实际调用都计入共享运行模型调用/时间/金额上限，不能只统计主ReAct轮数。

- **Reduction**：完整工具结果先有实际持久记录，再以定位符/有界摘要进入模型上下文。Backend写入`creative_agent_context_items`，路径是逻辑名字空间，禁止直接使用/tmp或任意服务器文件。ReadRunResult只读取本run被登记的对象、范围和字节上限，并重新校验用途；不是自由文件系统工具。现有64KiB工具结果、256KiB模型结果等上限继续有效，卸载不扩大输入许可。
- **Summarization**：替代此前“首期不做自动模型压缩”的安排，设计为可配置的受控中间件。完整原消息/输入保留，摘要是有版本和来源的派生context item。摘要保留任务约束、已应用operation与结果、待办/冲突、Skill定位；不得从摘要恢复权限、精确版本或收费事实。阈值/保留后缀/token计数器和Finalize策略在FND-07用中文/多模态样本验证后启用；验证前默认关闭摘要，达到容量上限明确反馈。
- **授权闭包**：摘要与卸载材料继承所有来源的外发约束，不能把“只发送摘要”当作绕过原来源授权。实际数据来源/引用清单在结构化记录中，文本summary不能删除它们。
- **媒体**：Eino state仅携带受信内部媒体定位符和读取级别，实际预览字节/临时URL由模型适配器在最终出站阶段解析；不让checkpoint长期存签名URL/凭证。真实内容根仍由run/step/context/checkpoint显式revision关联保留，读取前重新检查，不从URI本身推导授权。

[Summarization](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/middleware_summarization/)和[Reduction](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/middleware_toolreduction/)提供机制；中文token预算、来源保真和存储/外发守卫仍由本应用验收。

## 4. Skill发现、固定版本与权限

采用Eino Skill Backend的List/Get和接口式Skill Middleware。平台部署/发布包仍由key/version/digest标识；Eino FrontMatter没有直接承载我们全部版本/权限字段，因此外层SkillRegistry保存这些元数据，为每个run构造固定目录快照的Backend：

1. List只返回本run允许发现的name/description；模型上下文先拿目录信息，不加载全部正文。
2. Get按name解析到该run已固定的version/digest，返回SKILL.md正文；拒绝同key/version不同digest或不可用技能。运行过程中不能解析到部署刚更新的“latest”。
3. references/assets通过只读ReadSkillResource按包digest+相对路径读取，拒绝路径越界与未登记文件。首次读取保存内容hash与run关联，历史恢复仍指向原包；不从技能指令自动开放shell或script。
4. 摄影师明确选择Skill时可以在初始化阶段主动激活该固定包；Agent自行发现只能在本次许可目录内加载。普通对话仍可无专项Skill。
5. 首期只启用inline模式；fork/fork_with_context、frontmatter model覆盖须被平台策略拒绝或在未来明确启用。摘要同样使用受信模型适配，不能由Skill文本绕过固定模型/供应商授权。

业务工具按[画布工具化契约](canvas-tooling.md)扩展；此外新增的是受控运行时工具：Skill加载、ReadSkillResource、ReadRunResult。它们同样进入工具注册/允许集、计数、输出上限、事件和恢复协议；Eino中间件自动添加工具不等于自动获得权限。首期不开放通用MCP发现、第三方Skill安装或主机文件系统。

[Skill官方接口](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/eino_adk_chatmodelagentmiddleware/middleware_skill/)。

## 5. 工具执行、顺序和调用身份

BusinessToolAdapter通过BaseTool/InvokableTool表达已存在的领域用例；共享工具定义由版本化registry生成，Eino ToolInfo是适配投影，不是第二份可独立变更的schema。业务写工具配置顺序执行，保留原12工具上限；框架MaxIterations仅是额外防线，Resume/重新构建Runner不得重置持久run计数。

模型完整结果在Gateway保存后，按原 `model_step_id + tool_call_index` 建立唯一工具计划，将框架CallID映射到真实step/operation。CallID只是定位信息，不是账号授权、唯一业务操作或跨轮次去重依据。参数规范化、prepared后固定hash、前置条件与执行读集承接继续使用当前契约。

工具包装器在同事务内做epoch/cancel/权限/版本校验、实际effect、change/operation receipt和step结果。中间件用于框架调用边界与事件映射；**不能把领域提交放在一个事务、AfterTool callback再开另一个事务补回执**。框架将结果交回模型之前，应用应已确认完整持久结果。

所有框架模型重试、failover、Skill模型覆盖默认禁用；供应商真实重试仅由Gateway在可靠未受理且Rearm成功后执行。摘要中间件也不自行重试/换模型，未知费用不当0。第一阶段业务工具串行，未来只读并发由独立预算/权限协议扩展。

## 6. Checkpoint、恢复和运行事实

Eino Checkpoint保存框架中断位置与可序列化状态；run/step/operation receipts仍保存业务执行事实。Runner正常返回或进程任意位置崩溃，并不自动证明最后副作用已有一致checkpoint。CheckPointStore.Set/Get缺少我们的账号/epoch/CAS参数，适配器必须通过受信运行上下文补齐；所有读取和写入限定账号/run，拒绝旧epoch写回。

Checkpoint记录：`creative_agent_checkpoints(id, account_id, run_id, runtime_version, serializer_version, registry_digest, skill_catalog_digest, execution_epoch, revision, payload BYTEA, created_at, retained_until)`；byte payload为受控框架状态，大小有界。对checkpoint依赖的内容登记 `creative_checkpoint_content_refs(account_id,checkpoint_id,content_revision_id)`。checkpoint删除与refs释放同事务；旧Eino运行时序列化不能在新版本中盲解码，必须匹配版本/注册类型或有经过验证的迁移。版本不兼容保留业务结果并明确不可自动恢复。

上下文材料记录：`creative_agent_context_items(id, account_id, run_id, kind, source_step_id_snapshot?, source_manifest JSONB, payload, digest, revision, retained_until)`，kind为tool_result/summary/skill_resource；媒体保留另有 `creative_context_item_content_refs(account_id,item_id,content_revision_id)`。source_manifest中的消息/步骤定位属于可过期历史，不授予权限。所有新增业务表继续无外键，服务验证key关系；正式DDL与所有根扫描随FND-07实现并由FND-10联验。

恢复决策由应用驱动器执行：

| 已有事实 | 行为 |
|---|---|
| 有兼容checkpoint，待人工补充/采纳且当前权限/期限有效 | 原子取得slot/新epoch，再Runner.Resume；工具重新进入时仍查询原receipt |
| effect已提交，checkpoint仍在effect之前 | 恢复工具必须用原operation回放已提交结果；不再次执行effect，再继续后续已记录路径 |
| Gateway结果已保存，Eino未消费 | 通过原model step/request返回原完整结果，唯一工具计划不能重复展开 |
| 派发后结果未知 | 进入原reconciling，只查询核实；Eino不能主动重发或换模型 |
| 无checkpoint或checkpoint与journal之间无法证明一致接续 | 保留已交付结果/草稿，按事实进入partial/failed或reconciling；不能直接从原问题Query并假称安全恢复；显式重试仍只承接可证实未执行的原步骤 |
| 运行已取消/终态/超期 | 拒绝新效果与调用；迟到结果只保存诊断/核算；业务状态不因Resume重新打开 |

生产桥接需持久绑定框架执行帧、模型轮次和step身份，核对checkpoint覆盖的业务水位；已有回执记录与待调用序列必须可重建。实验中的固定model-1/model-2与简化hash只证明两轮轨迹的回放接缝，**不能复制成通用恢复算法**。完整多轮、同参数重复调用、同名子Agent与升级恢复仍是FND-07/08入口前验证项；没有一致证据时采用明确停止的保守出口。

[Runner与Checkpoint](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_extension/)、[中断恢复](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_hitl/)。

## 7. 多Agent演进

未来默认使用AgentAsTool，将独立任务/必要修订引用交给子Agent，主Agent汇总；避免把完整父对话和权限默认复制给孩子。v0.9将Transfer/相关Workflow Agent/Supervisor标为新项目不推荐，选择AgentTool路线。首期仅做隔离上下文接口实验，不开启生产委派工具。

未来扩展必须有parent/root run关联、每个child独立输入和状态、root总预算与步数、取消传播、结果汇合/部分失败。只读与提案可以并行，写入先由主协调者经当前命令应用；父run占slot等待子run抢相同slot的协议不可采用。供应商仍经同一Gateway，子Agent无权扩大模型/素材范围。

[Agent协作](https://www.cloudwego.io/zh/docs/eino/core_modules/eino_adk/agent_collaboration/)。

## 8. 适配实验及开发入口

[实验README](probes/eino/README.md)记录真实ADK/PG、跨进程及故障点；模型均为本地脚本函数，无供应商请求或费用。EinoExt、真实Gateway准入账本、River适配、真实画布命令、全部生命周期与前端SSE没有在这组实验中实现。

- FND-01增加精确依赖、接口编译、Checkpoint/工具回执间隙、Skill/上下文插件适配验证；现有八项是其中一部分，React Flow/River/生产scope/完整应用依赖验证仍未完成。
- FND-06实现GatewayModelAdapter及所有辅助模型出口，保留统一准入/核算。
- FND-07用Eino Runner/ChatModelAgent实现Harness，接入受控context/checkpoint backend、Skill加载/摘要配置与业务journal；删除原自研通用loop任务。
- FND-08注册真实工具/Skill与运行时只读工具，验证框架事件到真实消息/回执/SSE的转换、运行总上限及恢复。
- FND-10加入checkpoint/context refs清理，FND-12增加运行时升级与多轮恢复的成品验收。暂不因框架接入缩减原有可靠性验收或直接调低总工期估算。
