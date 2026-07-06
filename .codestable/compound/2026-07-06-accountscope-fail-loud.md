# AccountScope 隔离基座要 fail-loud

## 背景

platform-skeleton 为 ADR-001 的账号维度数据隔离落了 `AccountScope`。review 中出现过 SQL 标识符拼接契约问题：如果 scope 为了支持复杂查询而过度放宽，后续域代码很容易绕开账号过滤；如果过度静默兼容，则跨账号串数据会变成隐性风险。

## 结论

`AccountScope` 是账号隔离的结构性边界，策略应是 fail-loud：

- 业务表读写必须持有 AccountScope，无 scope 无查询。
- scope 只能从 AccountContext 构造，客户端参数永不参与账号过滤。
- 标识符校验宁可先拒绝复杂表达式，也不要允许任意 SQL 片段进入基座。
- 后续 feature 如果需要 `*`、`count(*)`、别名、schema 限定名或聚合查询，必须显式扩展 AccountScope 的校验和测试，不得绕开 scope。
- `domain/service` 不 import gin，pgx 仅 store 系包 import，继续用 depguard 守住依赖方向。

## 证据

- 代码落点：`backend/internal/platform/store/scope.go`
- 测试落点：`backend/internal/platform/store/scope_test.go`、`scope_internal_test.go`
- 审查记录：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-review.md` REV-006、R2-05
- ADR 背景：`.codestable/requirements/adrs/001-account-scoped-data-model.md`
