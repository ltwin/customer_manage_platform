# public-auth-hardening Production Preflight 证据

## 结论

STEP-006 的 production preflight 已从“公开注册恒定拒绝”升级为 fail-closed 两态闸门：

- `AUTH_PUBLIC_REGISTRATION_ENABLED=false`：完成基础配置检查后仅报告
  `complete/preflight=secure-baseline-ready`，不声称公开注册已开启。
- `AUTH_PUBLIC_REGISTRATION_ENABLED=true`：只有当前数据库 live checks 和全部 versioned evidence 同时通过，才报告
  `complete/preflight=enable-ready`。

Preflight 不修改 env、公开开关或数据库，不执行 migration、deploy、cutover、root rotation 或 rollback。

## 配置指纹

`scripts/lib/auth-preflight-evidence.py fingerprint` 对以下排序后的非秘密配置生成 SHA-256：

- mail driver 与 provider；
- sender domain，不含完整收件地址；
- issuer 与 canonical HTTPS `PUBLIC_BASE_URL`；
- canonical、排序后的 `TRUSTED_PROXY_CIDRS`；
- `__Host-crm_refresh|HttpOnly|Secure|SameSite=Strict|Path=/|no-domain` cookie profile；
- `crm-auth/v1/limiter-hmac@v1` KDF/namespace 版本；
- `AUTH_TOKEN_SECRET_VERSION` 非秘密引用，不含 secret 值。

指纹输入不读取或输出 `AUTH_TOKEN_SECRET`、`RESEND_API_KEY`、`DATABASE_URL`、完整 recipient 或 provider body。

## Live checks

`accountctl auth readiness` 由当前 binary 的 embedded build revision 校验调用者声明，并只读当前数据库：

1. `store.Open` 的 ping 与 `schema_migrations` clean 状态；
2. schema version 精确为 13；
3. `auth_attempt_budgets` 表、固定五列、`(action, dimension, digest)`主键与真实`window_start`单列index存在且valid／ready；
4. 自动rollback的live probe实际执行synthetic insert、`ON CONFLICT` update，并证明action／dimension／digest／attempts四类CHECK拒绝非法行；
5. `legacy_unclaimed=0` 且 pending legacy claim 为 0。

Binary 轨显式传入带 revision attestation 的 `accountctl`；compose 轨使用当前 app image 内
`/usr/local/bin/accountctl` 的一次性 `docker compose run --rm --no-deps` 只读探针。探针不调用
`MigrateUp`，不创建账号、不发邮件、不消费真实limiter预算，也不更改 legacy 状态；synthetic probe始终由事务rollback，不留下预算行。

## Evidence envelope

证据目录固定包含：

| 文件 | Freshness | 额外约束 |
|---|---:|---|
| `mail-accepted.json` | ≤24h | provider、provider message ID、secret version ref、`email:<digest>` recipient ref |
| `monitor.json` | ≤24h | 固定公共 envelope |
| `security.json` | ≤24h | 固定公共 envelope |
| `rollback.json` | ≤7d | 固定公共 envelope |
| `rotation.json` | ≤7d | 固定公共 envelope |

公共字段精确为 `version=1`、UTC `generated_at`、40 位 `build_revision`、`schema_version`、64 位
`config_fingerprint`、`environment=production`、`status=passed`、`evidence_path`。未知字段也 fail closed，
避免把 secret 或原始 payload 塞进证据。所有文件必须匹配当前 live report 的 revision/schema/fingerprint；
未来时间偏移允许到 5 分钟，超过即失败。

## TDD 与边界矩阵

- RED 1：fixture 首先要求 false 模式输出 `secure-baseline-ready`，旧实现仍输出 `ok`；要求 true 缺证据用
  `AUTH_READINESS_EVIDENCE` fail closed，旧实现仍返回 `public-auth-hardening-not-complete`。
- GREEN 1：引入两态 completion 后既有 27 个 config/compose/redaction case 全绿。
- RED 2：加入 full matching evidence 的 true positive case，因 helper 与新 flags 尚不存在真实失败。
- GREEN 2：加入 config fingerprint、live report 与 5 类 envelope verifier 后 positive case 得到 `enable-ready`。
- RED 3：`accountctl auth readiness` 测试因 readiness state/probe/report 尚不存在编译失败。
- GREEN 3：最小只读 store probe 与 CLI envelope 完成，unit 与真实 PostgreSQL schema/cutover 测试通过。
- VERIFY：`./scripts/test-production-preflight.sh` 共 48 cases 全绿，覆盖 binary/managed compose positive、每个文件
  missing、status failed、revision/schema/fingerprint mismatch、24h 与 7d 精确边界、边界后 1 秒、未来 5 分钟与
  5分1秒、unknown field、raw recipient、secret version 缺失、live limiter failure、env 注入与输出 redaction。

## 本轮命令

- `./scripts/test-production-preflight.sh`：48 cases passed。
- `go test -p=1 ./cmd/accountctl/... ./internal/platform/store/... -run '^(TestRunAuthReadinessRequiresCurrentSchemaLimiterAndLegacyCutover|TestAuthReadinessInspectsCurrentLimiterSchemaAndLegacyCutover)$' -count=1 -parallel=1`：通过。
- 使用 synthetic 40 位 revision 的 `go build -ldflags ... ./cmd/accountctl`：通过。
- `bash -n`、helper `--help` 与 `git diff --check`：通过。

## 外部边界

已有 Resend acceptance receipt 仍保留在上游 checkpoint；本轮没有发送新邮件，也没有把 owner 的真实
recipient、API key 或 `.env` 内容写入报告。真实 production `enable-ready` 必须由当次运行环境生成 matching、fresh
evidence，开发期的 synthetic positive case不能代替生产授权。
