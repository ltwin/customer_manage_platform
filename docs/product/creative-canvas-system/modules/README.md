# 模块设计入口

三批主要模块方案已形成。正式开发候选与依赖以[永久Epic FND-01–13](../../../../.codestable/epics/creative-workspace-system.md#当前子项契约)为准；[实施计划](../delivery-plan.md)映射下列CM/LC/GH局部切片，它们不再作为独立执行队列。以下保留各批设计和验证边界。

1. [公共契约](common-contracts.md)：身份、错误、幂等、版本、查询、REST/SSE 和事务端口。
2. [内容模块](content.md)：Create/Fork/Append、明确修订、用途与引用保留。
3. [媒体模块](media.md)：上传阶段、API、目标绑定、候选、读取与回收。
4. [13 表 DDL 草案](content-media-schema.sql)：在隔离 PostgreSQL 验证，未接生产迁移。
5. [实施切片与测试矩阵](implementation-slices.md)：CM-01–05 的依赖与通过标准。
6. [实际验证记录](verification.md)：已验证的 SQL 边界与尚未完成的实现验证。

本批仍是 proposed 模块方案。媒体允许格式、资源上限、读取票据存储、派生任务记录和实际来源/用途矩阵尚需在对应切片实现前冻结；它们不是已发布能力。后续模块进度见下方第二批入口；不因局部 DDL 已执行于测试库就跳过业务接口和跨模块验收。


## 第二批：个人库与画布

- [个人资产库](library.md)：检索、分组/标签、回收站与跨模块端口。
- [画布](canvas.md)：图命令、父子坐标、引用、保存队列、Undo/Redo 和版本防重用。
- [16表草案](library-canvas-schema.sql)与[查询骨架](library-search.sql)：与首批13表叠加验证，不接生产迁移。
- [第二批实施切片与验证](library-canvas-slices.md)：LC-01–05 及已执行的4项SQL探针。

个人库/画布模块方案已形成；Gateway 与 Harness 的详细接口见下方第三批。未实施的媒体格式/资源、票据、派生任务、真实React Flow及服务验证仍在所属切片中跟进。


## 第三批：LLM Gateway 与 Agent Harness

- [Gateway](gateway.md)：统一模型契约、准入、派发/核实、用量与成本。
- [Harness](harness.md)：平台Skill、受控工具、上下文/附件、执行与恢复。
- [API与事件](agent-api-events.md)：会话/运行控制、消息持久化、SSE补读。
- [数据与事务协议](gateway-harness-data.md)：完整模块记录、显式key与锁序、保留交接；[12表协议字段投影](gateway-harness-protocol.sql)仅供隔离探针，不是全量迁移草案。
- [第三批实施切片与验证](gateway-harness-slices.md)：GH-01–05、故障矩阵与6项SQL实验。

三批模块现由FND路线统一实施，配套[迁移发布](../migration-release.md)和[整体验收](../acceptance-plan.md)。真实供应商接入、River桥、媒体编码配置及正式API/迁移仍须按所属项验证；设计通过不代表这些能力已实现。


Agent运行时已收敛为[Eino ADK接入方案](../eino-adoption.md)；[隔离实验](../probes/eino/README.md)固定v0.9.19，不修改生产依赖。Harness/Gateway和数据补充以接入契约为准，FND路线已承接适配工作。

## 节点生成范围更新

首期新增[node-generation](../node-generation.md)：data/status、每节点Prompt与真实生成、结果历史及长期引用。它补充canvas/media/Gateway/Harness模块；既有探针与SQL草案仅证明原有协议，不覆盖新增schema/生产生成。FND-13正式交付及08/10联合验收，具体顺序以永久Epic为准。

## Skill 底座改造候选（2026-09-14）

[Skill 资源化与结构化输入底座方案](skill-foundation.md)：FND-07 B 前的 proposed 改造方案，定义数据库/OSS、不可变版本、必要预留字段与类型化输入；后续[市场与审核](../skill-marketplace-brainstorm.md)单独排期。该方案尚未实现，批准实施时同步 Harness、数据和 API 契约。
