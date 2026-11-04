---
id: 003
title: 单体优先：Gin + JSON/OpenAPI 契约（而非 Kratos + proto-first）
status: accepted
date: 2026-07-05
relates_to: [002]
---

# 单体优先：Gin + JSON/OpenAPI 契约（而非 Kratos + proto-first）

## Context

首版形态是单二进制模块化单体：一名开发者、约 30 个 JSON REST 端点、唯一 API 消费者是浏览器（React），外加每日提醒扫描与 Telegram Bot。owner 关注将来业务扩增时向微服务 / gRPC 演进的成本，曾候选 Kratos v2 与 proto-first 契约。

## Decision

- HTTP 框架用 **Gin**；后端按**轻量 DDD** 组织：domain 包（实体 + 领域服务，状态机 / merge / 提醒规则住这里）、repository 接口、handler 薄适配层。**领域逻辑不得 import 路由框架**（`gin.Context` 不下穿 service / repository 层）——已写入 roadmap §4.1，进 code review 检查口径。
- 对外契约为 **JSON/REST**，以 OpenAPI 文件为机器可执行形式（roadmap §4 为语义权威源），双端 codegen（Go: oapi-codegen / TS: openapi-typescript）保证类型对齐。
- 不引入 CQRS、领域事件总线等重仪式；真需要时另立 ADR。

## Consequences

- 正面：浏览器原生消费零翻译层；契约先行 + 编译期类型对齐；单人开发心智负担与工具链最轻。
- 负面 / 新约束：`gin.Context` 粘在 handler 签名上，换路由框架成本中等——由 handler 薄层约束把粘性控制在适配层内；将来拆微服务时，**服务间接口以 proto 新生**，与本决策不冲突；微服务化的真实大头成本（数据拆分、分布式事务、运维）与本决策无关，今日选 Kratos 也不会减少。

## Alternatives Considered

- **Kratos v2**：微服务治理框架（注册发现、gRPC/HTTP 双传输、proto 优先、可观测性），价值面向多服务多团队；本项目没有第二个服务来消费这些治理能力，每条 feature 都要付 proto + codegen + 框架概念税；其 proto 正统路径与 roadmap 的 JSON 字段级契约冲突。否决。
- **proto-first（grpc-gateway 翻译出 REST）**：唯一消费者是浏览器，维护翻译链的产出恰好是直接写 REST 本来就有的东西；409 子错误码、文件下载（Content-Disposition）、TG deep-link 等 HTTP 语义过 gateway 需额外定制。否决。
- **chi / echo / net/http(1.22+)**：均可行，非结构性差异；Gin 胜在生态厚度与绑定校验对 30 个 CRUD 端点的效率。若将来更换，属适配层内的低成本变更，不构成本 ADR 的 supersede。
