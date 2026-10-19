# 项目全局架构

记录当前已交付系统。Epic 的目标架构保留在 Epic 设计中，随 Change 实际交付分批更新这里。
已有 canonical 架构文档可通过 schema reference 接入；不要同时维护两份相同正文。

<!-- 使用独立 ## 章节描述：系统边界、模块职责与依赖方向、关键数据流、部署、非功能约束和核心技术选择。
新项目可以先只有最小架构；未实现的远期方案不写成现状。通过 architecture delta 随交付演进。
-->

## 通用文档底座

- 「画布节点背后的独立文档」由 `backend/internal/creativedocument` 统一承载：文档行、修订、引用、工作会话指针、回收站、列表、命令外壳、类型注册表。
- 具体文档类型（`internal.document` 内置；`photo.shoot_plan`、`scene` 由各自功能实现）通过类型端口注册，只负责载荷、单元和操作语义。
- 依赖方向：`httpapi` / `creativecanvas` / 各类型包 → `creativedocument` → `creativecontent` / `platform/store`。`creativedocument` 不依赖 `creativecanvas`，也不依赖任何具体类型。
- 文档与画布节点是两个聚合：只有 `add_node` 新建时同事务创建，之后节点只持有 `document_id`。删除节点不删除文档；彻底删除文档后，节点读取为不可用。
- 实时：文档头的变化经已有的 `creative_documents_notify` 触发器走账号级失效通道，不新增 SSE。

## 场景模块

### 职责与边界
- 依赖通用文档底座（`creative-document-foundation`）：场景以文档类型端口注册为 `scene` 类型，读取、命令、回收站和历史都走通用入口。
- `backend/internal/creativescene`：
  - 场景快照的 schema 校验、单元版本与命令应用、恢复。
  - 模型提交（消费暂存上传，写 `model` 内容）。
  - 客户端任务生命周期、导出编排。
  - 场景助手工具的领域入口。
- 不职责：执行几何代码、渲染、画布节点的布局规则（调用画布命令层完成）。
- 依赖方向：`creativeagent` → `creativescene` → `creativecontent` / `creativestaging` / `creativecanvas`（命令层）/ 通用文档层 / `platform/store` / `platform/realtime`。`creativescene` 不 import gin（ADR-003），`gin.Context` 不下穿。客户端任务到达终态时，`creativescene` 经自己定义的端口通知 `creativeagent`（进程启动时注册实现），不反向 import。
- 前端 `frontend/src/creative-scene/`：编辑器外壳（R3F）、场景状态（快照 + outbox + 撤销栈）、沙箱宿主、渲染端（客户端任务执行器）、材质库、导出器。编辑视图嵌入画布的同一个助手面板。渲染端不依赖编辑器外壳：画布页在认领到任务时按需加载它，摄影师不打开场景也能让助手搭建；没有任务的画布页不加载 3D 引擎。
- 渲染、烘焙、截图、导出只在浏览器完成；服务端不渲染。服务端经客户端任务派发工作，经暂存上传接收产物。

### 关键数据流
- 手动编辑：编辑器 outbox → `POST /creative/documents/{id}/commands` → 新文档修订 → 账号失效通知 → 其他标签页重读。
- 助手写模型 / 截图 / 导出：工具创建客户端任务 → 运行进入 `waiting_client` 并释放槽位 → 渲染端（场景编辑视图，或按需加载渲染模块的画布页）认领、执行、暂存上传 → 完成任务 → 同事务落结果并让运行继续。
- 导出：渲染端暂存上传导出图 → `POST /creative/documents/{id}/scene-exports` → 画布命令层建图片节点，写入 `based_on`。
