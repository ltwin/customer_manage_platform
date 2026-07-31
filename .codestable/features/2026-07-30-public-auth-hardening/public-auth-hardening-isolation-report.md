---
doc_type: feature-evidence
feature: 2026-07-30-public-auth-hardening
evidence_kind: two-account-isolation
status: passed
generated: 2026-07-31
---

# 两账号全域隔离证据

## 1. 身份与会话入口

`TestAccountAccessHTTPPostgresE2EAndFullIsolation`在独立PostgreSQL中创建两个账号。二者都通过公开HTTP注册、synthetic mail sink捕获fragment token、邮箱验证后成为不同的active verified账号；客户端从未提交`account_id`。账号A随后完成refresh rotation、旧新refresh值不同，并通过`/api/v1/me`确认服务端账号上下文。

测试末尾对账号A执行logout：响应204并清理`__Host-crm_refresh`，旧refresh再次调用固定401并再次发送clear cookie，证明登出不会被仍在内存中的旧refresh恢复。

## 2. 全业务读取隔离

每个账号都写入不同marker、timezone和合法PNG头像内容。以下入口分别证明“能看到自己、看不到对方marker与account ID”：

| 领域 | 入口 | 结果 |
|---|---|---|
| 客户 | `GET /api/v1/customers` | 只含本账号客户marker |
| 订单 | `GET /api/v1/orders` | 只含本账号订单marker |
| 档期 | `GET /api/v1/schedule/slots` | 只含本账号档期marker |
| 提醒 | `GET /api/v1/reminders` | 只含本账号提醒marker |
| 设置 | `GET /api/v1/settings` | A=`Asia/Tokyo`，B=`Europe/Paris`，互不可见 |
| 导出 | `GET /api/v1/export` | 只含本账号snapshot数据 |
| 头像 | customer detail + avatar content | 本人200且精确字节匹配；内容为可真实解码的不同2×2 PNG |

账号A对账号B的customer detail、avatar content、order patch、schedule patch和reminder done请求均得到404／`not_found`。账号A的`AccountScope`直接查询账号B的avatar reconciliation checkpoint也得到`store.ErrNoRows`。

## 3. 后台 `AccountScopes` 消费者

同一E2E额外创建一个pending registration账号和一个legacy-unclaimed账号；`Store.AccountScopes()`只返回两个active verified账号，不包含pending或legacy账号。下列使用服务端枚举／scope的消费者测试均实际运行并通过：

- `TestAvatarMaintenanceA23ThreePageRestartSingleFlightDueLimitAndBackoff`
- `TestScanRunnerOncePerLocalDay`
- `TestBindingServiceRejectsGroupTamperExpiredAndCrossAccountChat`
- `TestChatAccountResolverReturnsZeroOneManyAndDBError`
- `TestDeliverySenderRunnerAccountFailureDoesNotStarveOtherAccounts`
- `TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel`

这组证据把“枚举只给active账号”与“每个后台消费者继续使用对应`AccountScope`”分开验证，避免用一个超大fixture伪装所有后台编排都实际运行过。

## 4. Redaction 与副作用

E2E日志扫描完整synthetic邮箱、密码、access／refresh token、`Authorization`和refresh cookie名均为零命中。catalog临时日志也通过DSN、secret、完整email、token/cookie/provider body扫描。

全部操作仅命中Testcontainers数据库、本地临时avatar object store与synthetic mail sink；没有真实邮件、deploy、生产migration／rollback、rotation、cutover、开关切换、commit或push。
