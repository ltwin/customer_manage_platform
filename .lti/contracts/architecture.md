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
