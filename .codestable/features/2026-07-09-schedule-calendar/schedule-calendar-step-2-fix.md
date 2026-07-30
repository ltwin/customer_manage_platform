# Step 2 窄范围修复说明

## 失败的 exit signal / checks

- exit signal：过期 idempotency record 并发时仅一个 owner 接管，其余请求等待后 replay。
- 失败测试：`TestExecuteCreateProtocol/expired_record_has_one_concurrent_takeover_owner`。
- 实际错误：`load idempotency key: no rows in result set`。

## 根因判断

PostgreSQL `INSERT ... ON CONFLICT DO NOTHING` 在冲突旧行被另一事务删除并替换的并发窗口中，可能返回 no-row；随后当前事务第一次 `SELECT ... FOR UPDATE` 又看不到已被替换的旧版本。claim 获取需要在 no-row 后重新尝试 insert/select，直到取得 owner 或锁定当前成功记录。

## 允许修改的范围

- `backend/internal/platform/idempotency/idempotency.go`：仅补 claim/load 的同事务重试循环，不改变 operation、hash、TTL、回放或错误契约。
- `backend/internal/platform/idempotency/idempotency_test.go`：仅在现有过期并发场景确有必要时补证据，不新增需求。

## 修复后必须重跑

- `go test ./internal/platform/idempotency -run TestExecuteCreateProtocol -count=1`
- `go test ./internal/platform/store/... ./internal/order/... ./internal/platform/idempotency/...`
