# 客户头像：content version + write revision + 不可变 generation

## 背景

`customer-avatar`（2026-07-11）要把可选头像写进 PostgreSQL 业务事实，同时把二进制放在本地持久卷（未来可换 OSS）。早期若用「覆盖式 current key」或「transaction-scoped advisory lock 护住 Delete」，在 PostgreSQL session 丢失、storage timeout、同 checksum 重发、A→B→A 重放时，迟到的 Delete 可能删掉新 current，或 If-Match 用 checksum 无法挡住 ABA。

## 结论

跨「关系库 pointer + 外部对象存储」的安全模型应拆成三层，并固定几条硬规则：

1. **公开 content version**：最终规范化字节的 `sha256-{64 hex}`，只用于 `avatar_url?v=` / ETag / If-None-Match；相同精确字节得到相同 version。
2. **写 revision**：独立非负整数（API 编码 `ar-{n}`），PUT/DELETE 的 If-Match 只比它；pointer 每次成功变化 +1，same-content no-op / already-none 不增。用来关闭 A→B→A 的 ABA。
3. **物理 generation / object_id**：每次实际发布生成 crypto-random 128-bit id，key 含 version+object_id；**离开 current 后永不复用**。GC 只删队列里的精确 ObjectRef，迟到 Delete 在存储侧天然隔离。
4. **新代次先落、pointer 后切**：先 `PutImmutable`，再在 AccountScope 事务里锁 Customer（与 merge 共锁）做 revision CAS 切换五字段 pointer；DB 失败最多留下 inventory 可发现的 orphan，不破坏旧 current。
5. **进入 GC 即烧毁**：任意 GC row 出现在某 object_id 上，该代次不得再晋升 current；pre-current 与历史代次一律换 fresh id。
6. **读路径先验完整性**：application READ 在发 200/304 header 前完整缓冲并核对 media type/size/checksum；handler 不直接碰 store。
7. **运维**：production 用 require-mount 证明独立卷；一致备份 = 停写 + pg_dump + volume archive + exact-generation manifest（按 object_id 核验，不能只比 checksum）。

适用边界：二进制是业务实体附属物、需要条件写与可恢复备份时优先这套；纯 JSON 字段或不需要跨 store 原子性时不必上 generation。

## 证据

- Design：`.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md` D4/D7、A8–A11/A24
- 实现：`backend/internal/customer/avatar_application.go`、`avatar_store.go`、`avatarstore/local.go`、`avatar_maintenance.go`
- 测试：application ABA/same-content repair、DB session loss 迟到 Delete、pre-current burn、HTTP 垂直切片
- QA/验收：`customer-avatar-qa.md` round 2、`customer-avatar-acceptance.md` passed
- 运维说明：`README.md`「客户头像持久卷与一致备份」
