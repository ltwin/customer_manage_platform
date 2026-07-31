# public-auth-hardening Rollback Case Catalog

## 结论

仓库内 rollback catalog 只操作 synthetic schema/data 和一次性 Testcontainers PostgreSQL；没有 production down、
deploy、cutover、开关或 secret 副作用。

## Case Catalog

| Case | 预期 | 证据 |
|---|---|---|
| limiter migration 0013 up | 固定表、列、action/dimension/digest约束和 index 存在 | `TestAttemptLimiterMigrationCreatesVersionedBudgetTable` |
| limiter migration 0013 down | limiter 表消失，schema 13→12 | `TestAuthReadinessInspectsCurrentLimiterSchemaAndLegacyCutover` |
| legacy-only account-auth down | 0012 down 成功，schema 12→11，账号 hash 与业务数据保留 | `TestAuthLegacyCutoverHarness` |
| 新式账号 account-auth down | 固定 `auth_schema_down_blocked_new_accounts`，账号保留 | `TestAuthLegacyCutoverHarness` |
| 旧 binary 边界 | 新式账号在旧 binary 下不可用，不伪造兼容 | `AUTH_ROLLBACK_CATALOG.old_binary_boundary` |
| root old/new | 旧 access/replay/limiter namespace 均失败 | root rotation 三个 negative tests |
| 全会话撤销 | reset/change transaction 撤销同账号全部 refresh family | `TestPasswordResetAndChangeRevokeAllRefreshFamilies` |

## 既有 harness 修复

0013 成为最新迁移后，legacy rollback harness 先独立回滚 limiter 13→12，再测试 account-auth 12→11；新式账号
negative case同样先安全退到12，再验证0012 fail closed。报告继续保留旧 binary 边界，并新增
`limiter_schema_rollback=true`，避免把“只回滚 limiter”误报为“account-auth rollback 已通过”。

## 验证结果

- `./scripts/test-auth-legacy-cutover.sh`：通过，报告 schema 12→11、limiter rollback、legacy data preserved、
  new-style down blocked 全为 true。
- `./scripts/test-auth-rotation-rollback.sh`：通过，固定报告 `production_effect=false`。

真实 migration down 或 rollback 不在仓库自动入口中；任何生产执行仍需单独变更授权和备份／恢复证据。
