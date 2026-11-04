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

### 未认证 capability 的窄扩展

`AccountScope` 的上述结论继续只适用于 authenticated `AccountContext`，不得为了匿名链接放宽其构造器。在 owner 已批准的 anonymous-share 产品范围下，`creative-shoot-planning` round 8 为 planshare 引入了不同类型、不同能力面的窄 architecture delta。补充产品取舍已由 owner 确认（`d83f1556-b56e-440b-b1bd-a45526c21f70`），core round 6 与 share round 5 也已分别通过独立 design review；因此它现在是**已独立审查、但尚待 epic 全量 design 统一确认和实现/验收证明的设计契约**。不得把它写成已实现或 production 事实，也不得据此跳过后续统一 design gate。

- 全局 selector resolver 只允许读取 planshare token 行并做 constant-time commitment 校验；客户端提交的 `account_id` 永不参与；
- 只有验证成功的数据库 token row 能由 store/platform trusted factory 构造 neutral `platform/txcap` 所有的 sealed `ValidatedShareContext` / `ShareTransactionCapability`；HTTP 不获得通用账号 ID、`AccountScope`、`TxAccountScope` 或 SQL；
- capability 只开启 account-filtered planshare transaction，并只调用白名单 query/participant port；proposal/full 权限仍由 exact projection 强制；
- generic `TransactionRunner[planshare.ShareTxScope]` 从同一 physical transaction 提供 executor-only `LedgerTxView` 与 callback-only typed scope；anonymous mutation 通过通用 idempotency executor 复用唯一 claim→replay-or-callback→store-success 算法，不复制第二套 ledger；
- 无 `account_id` 的 `SecurityAttemptBudgetV1` 只属于 platform pre-auth security data：仅保存 policy/action/dimension、版本化 HMAC digest、window 与 attempts；不保存账号、raw IP/token/key/body/receipt，不承载 planshare 业务事实，也不向 HTTP/application 暴露 Store、SQL、reset 或任意 action/dimension 能力；
- 该设计在 `plan-share-collaboration` implementation/acceptance 前仍是待实现契约；生产实现必须用构造负向测试、跨账号测试、双 Store/重启 counter fixture 和 depguard 证明没有形成第二个通用 scope或无边界的 accountless 业务存储。

因此“业务表读写必须持有 AccountScope”在未认证入口处应读作“必须持有由可信服务端事实构造的、用途受限的账号过滤能力”；普通 authenticated 业务代码仍然必须持有 `AccountScope`，不得借 planshare capability 绕过 auth。

## 证据

- 代码落点：`backend/internal/platform/store/scope.go`
- 测试落点：`backend/internal/platform/store/scope_test.go`、`scope_internal_test.go`
- 审查记录：`.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-review.md` REV-006、R2-05
- ADR 背景：`.codestable/requirements/adrs/001-account-scoped-data-model.md`
- Supplemental owner confirmation：`.codestable/roadmap/creative-shoot-planning/approval-report.md#round-8-share-contract`，confirmation id `d83f1556-b56e-440b-b1bd-a45526c21f70`
- Core design review：`.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design-review.md` round 6，`passed`
- Share design review：`.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-design-review.md` round 5，`passed`
