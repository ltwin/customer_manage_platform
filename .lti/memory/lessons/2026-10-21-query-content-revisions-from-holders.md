---
slug: query-content-revisions-from-holders
status: observed
scope: backend Go：按载荷字段查 creative_content_revisions
createdAt: 2026-10-21T03:25:03.066Z
source: 07c86d41-6dbb-4054-88aa-e92384e4d8bc
---
creative_content_revisions 存着本账号所有内容的每个修订，包括每一版场景快照（单条大、数量随编辑增长）。按 payload 的 JSON 字段筛选时，不要写成「在本账号修订里按载荷筛选再和持有方对上」：走哪条路由优化器决定，可能扫过整个场景历史。从持有方出发（如 creative_assets 用 kind 索引取出 content_revision_id），再按主键 id=ANY($2) 读这些修订比较载荷，成本只随持有方数量增长。例子：creativelibrary.FindModelAssetInTx。测试库表小、优化器总选顺序扫描，所以这条靠查询形状保证，不靠扫描计数测试。
