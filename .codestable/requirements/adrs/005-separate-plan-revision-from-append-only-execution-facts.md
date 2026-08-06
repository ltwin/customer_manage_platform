---
id: 005
title: 拍摄策划结构版本与 append-only 执行事实版本分离
status: accepted
date: 2026-08-06
relates_to:
  - 001
  - 002
  - 003
  - requirements/creative-shoot-planning
  - features/2026-08-05-shoot-plan-core
---

# 拍摄策划结构版本与 append-only 执行事实版本分离

## Context

拍摄策划同时承载两种写入节奏和审计要求不同的事实：拍前对创作 brief、镜头顺序、准备项和状态的低频结构编辑，以及现场对逐镜结果、清除和误记作废的高频执行记录。完成策划时还必须冻结一份能追溯到具体执行事实的完成快照。

如果两类写入共享一个聚合版本，现场每次勾选都会使普通编辑冲突，并放大完成时的竞争面；如果执行结果原位覆盖，则无法解释清除、连续纠错、软移出镜头和重新完成后的历史。后续素材、摄取、CRM、分享、提醒和经营反馈都会依赖这组长期事实，数据模型一旦形成依赖后很难替换。

## Decision

1. `ShootPlan.revision` 只拥有创作 brief、当前镜头结构与顺序、准备项、公开规模、执行时间窗和 lifecycle 状态的并发控制。
2. 每个 `Shot` 使用独立的 execution revision 进行逐镜 CAS；`ShootPlan.execution_fact_revision` 作为策划级执行事实水位，供完成操作验证自己读取的是一致事实集合。
3. captured、skipped、cleared 和 void 都保存为 append-only 事件，并由服务端单调 `shot_event_seq` 排序；客户端时间戳和 supersedes 链不充当历史排序真相。
4. 镜头当前结果是可从有效事件序列重建的投影：void 保留原事件，cleared 令当前结果为空，之后的纠错按最大有效序列重新投影。
5. 每次完成都写入不可变 `PlanFinalizationSnapshot`，引用当时的当前镜头、有效结果、准备缺失与双版本；reopen 和再次完成只新增更高 finalization revision，不改写旧快照。
6. 所有业务事实继续遵守 ADR-001 的账号隔离、ADR-002 的 PostgreSQL 主存和 ADR-003 的薄 handler / application 编排边界。

## Consequences

- 正面：现场高频记录不会无谓冲突普通结构编辑；同一镜头的竞争由局部 CAS 收口；完成快照可以证明自己绑定的执行事实版本；清除、误记作废、软移出和重新完成都有可重放历史。
- 正面：后续 feature 可以通过稳定的 command/query port 和事实引用增量扩展，不需要直接读取或重写 core 表。
- 代价：读取详情需要组合结构投影、当前执行投影和可选历史；默认 READ COMMITTED 下的多语句读取可能出现短暂展示不一致，需要用 CAS、刷新恢复或将来的读快照 hardening 控制。
- 代价：执行事件、void 和完成快照会持续增长，需要保留明确的查询边界、归档和未来数据保留策略；不能用删除历史来简化当前投影。
- 新约束：结构 mutation、执行 mutation 和 completion 必须遵守固定锁序与各自 revision owner；不能把执行事实重新塞回 plan revision，也不能新增原位覆盖结果的旁路。

## Alternatives Considered

- **整个 ShootPlan 共用一个 revision**：实现接口较少，但现场每次结果写都会让 brief、准备项和状态操作频繁 409；completion 仍难证明自己没有夹在两次执行写之间。否决。
- **每个镜头保存一行可变 current result，不保留事件**：当前读取简单，但 cleared、void、历史误记、软移出和 reopen/re-complete 无法审计或确定性重放。否决。
- **只保存 append-only 事件，不维护同步 current projection**：历史最纯粹，但每次详情、列表和完成校验都需要重复回放，增加查询成本与实现复杂度。选择“事件为真相源 + 同事务 current projection 缓存”，并以 replay fixture 守住等价性。
- **把执行事实拆成独立外部事件系统**：可提供强事件流能力，但首版单体和低频账号场景没有相应规模需求，会引入远程一致性、部署与运维复杂度。继续使用 PostgreSQL 同事务 append-only 表。
