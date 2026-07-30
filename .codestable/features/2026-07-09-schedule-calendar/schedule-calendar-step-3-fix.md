# Step 3 窄范围修复说明

## 失败的 exit signal / checks

- exit signal：`make build lint test` 全绿，`test:schedule` 已接入项目检查。
- 第一次失败：`npm ci` 报 package/lock 间接依赖不同步；已通过重新生成 lock 并单独 `npm ci` 证明 clean install 修复。
- 第二次失败：`golangci-lint` 的 `pgx-only-in-store` 拒绝 `backend/internal/order/repository.go` 直接 import `github.com/jackc/pgx/v5`。

## 根因判断

order 为让 `AccountScope` 与 `TxAccountScope` 共用只读行接口，直接引用了 `pgx.Row`；这违反 ADR-001 的 store 边界。store 已经是该类型的 owner，应由 store 暴露扫描行类型，order 只依赖 store。

## 允许修改的范围

- `backend/internal/platform/store/scope.go`：仅导出当前 `pgx.Row` 的 store 边界别名。
- `backend/internal/order/repository.go`：移除 pgx import，接口返回类型改为 `store.Row`。
- 不改变 SQL、事务、矩阵、slot 或前端行为。

## 修复后必须重跑

- `make build lint test`
- `git diff --check`
