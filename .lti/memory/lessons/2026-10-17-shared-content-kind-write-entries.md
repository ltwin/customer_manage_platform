---
slug: shared-content-kind-write-entries
status: observed
scope: 后端：creativecontent 共享内容层新增内容类型
createdAt: 2026-10-17T10:29:04.780Z
source: 32c90485-2ed7-497a-9411-b09fb0608521
---
给 creativecontent.Validate 加新类型分支后，所有直接写内容的入口都会跟着接受它：资产创建（随后撞上资产表的类型约束，返回 500），以及画布 replace_content（它从节点类型去掉 core. 前缀推出内容类型，会把未经类型 schema 校验的内容挂到节点上）。只应经文档层写入的类型，要在共享层标成文档专用（Validate、WriteAndRetain 拒绝，只有 WriteDocumentAndRetain 能写），不要在每个入口各加一道限制。新增类型时用 grep 列出 Validate / WriteAndRetain 的全部调用方，逐个确认。
