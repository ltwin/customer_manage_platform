---
slug: pgx-tx-no-query-while-rows-open
status: observed
scope: backend/internal/creativeagent, store.TxAccountScope
createdAt: 2026-11-14T09:36:10.035Z
source: egress-consent-account-level
---
同一个 TxAccountScope 事务只有一条连接：tx.Query/QueryPage 返回的 rows 还没 Close 时，再在同一事务里查询（比如在遍历中途调 readScopeInTx/effectiveConsentInTx）会报 pgx 'conn busy'；在 Postgres 测试里常表现为卡住直到 go test 10 分钟超时。做法：需要的附带读取放到打开 rows 之前，或先把 rows 读完并 Close 再查。2026-11-13 compactionRequestInTx 就是这样修好的。
