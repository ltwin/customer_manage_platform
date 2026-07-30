---
id: 004
title: 客户头像二进制经 AvatarObjectStore 作为 PostgreSQL pointer 的 durable adjunct（而非库内 BLOB 或首版直连 OSS）
status: accepted
date: 2026-07-13
relates_to:
  - 001
  - 002
  - requirements/customer-profile
  - features/2026-07-11-customer-avatar
---

# 客户头像二进制经 AvatarObjectStore 作为 PostgreSQL pointer 的 durable adjunct（而非库内 BLOB 或首版直连 OSS）

## Context

`customer-avatar` 要为客户提供可选头像的设置、替换、移除与鉴权读取。业务事实（谁有头像、当前版本、写并发）必须落在账号隔离的关系模型里；像素字节则可能达数 MiB、需要 durable 文件语义，并预留日后迁到对象存储。

ADR-002 已选定 PostgreSQL 为**唯一主存储**（业务事实与引用完整性）。本决策不推翻该结论，而是回答：头像**二进制**放哪里、业务层如何与存储解耦、如何在「库事务」与「外部对象」之间保持可恢复一致性。

约束来自 roadmap / design 拍板：首版 ECS/local 持久卷、经 port 隔离、不开放匿名媒体、不把 token 放进 URL、merge 不自动迁头像；owner 接受 24h grace 与精确代次 GC 默认值。

## Decision

1. **PostgreSQL 仍是 system of record**：Customer 行保存始终存在的写 token（`avatar_revision`）与可选五字段 current pointer（version / object_id / media_type / size / updated_at）。客户端永不传 `account_id`；指针与 GC 队列均带账号维度（ADR-001）。
2. **二进制是 durable adjunct，不进业务行 BLOB**：像素对象经 customer 拥有的 **AvatarObjectStore** port（`PutImmutable` / `Open` / `Stat` / `List` / `Delete`）读写。首版唯一 production adapter 为本地可挂载卷；in-memory 仅测试。未来 OSS 是同一 port 的第三实现，不是 handler 直连 SDK。
3. **公开交付仍走应用鉴权代理**：`avatar_url` 固定为同源相对路径 `/api/v1/customers/{id}/avatar/content?v={avatar_version}`；不暴露本地路径、object key、bucket 或预签名 URL。READ 在 application 层完整校验后再由薄 handler 发 200/304。
4. **跨 store 一致性用三分离 + 精确代次 GC**（实现细节见 compound `2026-07-13-avatar-immutable-generation-pattern`）：
   - content version（规范化字节 checksum）只用于 URL/ETag；
   - write revision 只用于 If-Match，关闭 ABA；
   - 随机 `avatar_object_id` 标识不可复用物理代次；**进入 GC 的代次不得再晋升 current**；Delete 只针对精确 ObjectRef，使 DB session 丢失后的迟到删除不伤新 current。
5. **运维双轨**：production 要求独立挂载可证明（require-mount）；一致备份 = 停写后的 `pg_dump` + 卷归档 + exact-generation manifest（按 object_id 核验，不能只比 checksum）。

本 ADR **不修改** ADR-002 的 PostgreSQL 主存选择；JSONB 半结构化字段规则与业务表归属仍以 002 / 001 为准。

## Consequences

- 正面：业务契约与前端可在 local→OSS 时保持稳定；指针事务与 merge 共锁仍在 PostgreSQL；迟到 Delete / 结果未知 / ABA 有可测模型；备份与恢复可按 exact generation 拒绝错误代次。
- 负面 / 新约束：
  - 运维必须同时管理 PG 与头像卷（或未来 bucket），备份不能只做 `pg_dump`；
  - 应用层承担 generation 编排与 GC/reconciliation，复杂度高于「单行 BLOB」；
  - 在线 DELETE / cleanup 不追溯擦除历史备份中的头像 PII，恢复策略需单独评估；
  - 禁止在 handler 或页面拼本地路径 / 预签名 URL 绕过 port。
- 与 ADR-003：领域规则留在 customer application，Gin handler 只做 multipart/binary 适配。

## Alternatives Considered

- **头像字节存 PostgreSQL BYTEA / 大对象**：备份单一、事务简单，但放大库体积与备份窗口，不利于 CDN/对象存储演进，且大对象 I/O 与业务行锁耦合。否决为首版路径。
- **业务层直接操作本地路径或全局万能 BlobStore**：首版代码少，但路径/原子写/安全校验泄漏进编排，迁 OSS 必改上层；万能 Blob 无头像生命周期语义。否决。
- **首版即私有 OSS + 预签名直读**：可行但改变交付层（过期、CORS、URL 形态），并把「应用鉴权代理」决策推迟到以后更难改。owner 拍板 local-first；预签名直读若需要须另起媒体交付 design。否决为本 feature 范围。
- **覆盖式 stable key / 依赖 DB session 生命周期的 fence 锁护 Delete**：实现短，但 session 丢失后迟到 Delete 可删掉同 checksum 的新 current（roadmap review 已证伪）。否决；改为不可复用 object_id + 精确代次 GC。
