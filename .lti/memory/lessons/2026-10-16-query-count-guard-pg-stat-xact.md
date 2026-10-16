---
slug: query-count-guard-pg-stat-xact
status: observed
scope: backend Go 集成测试：证明批量读取的查询次数不随条数增长
createdAt: 2026-10-16T18:08:28.241Z
source: d04f6c6d-5e57-41d1-8e19-62777ff4d5fb
---
用 pg_stat_xact_user_tables 在同一事务内数目标表的 seq_scan+idx_scan。store 只允许带 account_id 的查询，所以在测试库建视图：SELECT '<账号>'::text AS account_id, (coalesce(seq_scan,0)+coalesce(idx_scan,0))::bigint AS scans FROM pg_stat_xact_user_tables WHERE relname='<表>'，在被测函数前后各读一次取差值（不要读绝对值，未刷新的统计可能跨事务累积）。只适用于被测函数接受调用方事务的情况，自己开事务的函数测不到。例子：backend/internal/creativedocument/document_test.go TestSummariesReadReferencesOnce。
