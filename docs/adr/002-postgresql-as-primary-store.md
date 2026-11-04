---
id: 002
title: 存储选型 PostgreSQL（而非 MongoDB / SQLite）
status: accepted
date: 2026-07-05
relates_to: [001]
---

# 存储选型 PostgreSQL（而非 MongoDB / SQLite）

## Context

Greenfield 首版启动前必须拍板存储引擎（roadmap 条目 1 的启动前提）。候选有三：owner 环境已连的 MongoDB 集群、零运维的 SQLite、PostgreSQL。

系统的数据形态给出了强信号：客户—社交身份—订单—档期—提醒是强关系结构；客户合并（merge）是跨表原子操作；roadmap 契约要求引用完整性（引用已归档客户须 409、转介绍指向真实客户）；dashboard 与二期的画像/渠道分析全是聚合查询。ADR-001 要求全量数据带账号维度。

## Decision

PostgreSQL 作为唯一主存储，部署于阿里云 ECS 自装（首版与应用同机）。半结构化字段（偏好备注、套系参数差异）用 JSONB。大数据/分析类需求真实出现时再按需引入专用存储（列存 / ES 等），不预支复杂度。

## Consequences

- 正面：merge 走数据库事务；外键约束在库层兜住引用完整性；`account_id` 过滤是普通 WHERE，产品化时可升级 Row-Level Security 把隔离从约定变强制；SQL 聚合直接支撑 dashboard 与二期分析。
- 负面 / 新约束：自部署即自担备份责任——pg_dump 定时 + 异地副本（落点在 roadmap 条目 1 与 11）；比 SQLite 多一份运维；schema 迁移工具成为必需（platform-skeleton 的 feature-design 选定）。

## Alternatives Considered

- **MongoDB**（owner 环境已有集群）：支持它的是环境证据而非数据模型证据；多文档事务是其短板、无外键，与本系统强关系形态错配，引用完整性全靠应用层自觉。否决。
- **SQLite**（零运维、备份即拷文件）：纯本地单机自用的好选择，但部署已定阿里云、且产品化预留下并发与 RLS 演进空间弱。否决。
