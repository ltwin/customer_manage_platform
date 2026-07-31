# public-auth-hardening Root Rotation 演练报告

## 结论

`./scripts/test-auth-rotation-rollback.sh` 使用 synthetic old/new root secret 与一次性 PostgreSQL 完成可重放演练，
结果为 passed；`production_effect=false`。仓库未执行或自动化真实 root rotation。

## 固定顺序

人工 runbook 固定：关闭公开注册并记录维护窗 → 撤销全部 refresh family → 切 root secret 与
access-signing／refresh-replay-aead／limiter-hmac 三类派生 key → 重启签发与校验 → 证明旧 access、refresh、
replay 和 limiter namespace 失效 → 运行 production preflight → owner 另行授权后恢复公开入口。

完整 runbook：`docs/ops/auth-root-rotation.md`。

## Synthetic 证据

- `TestAccessTokenVersionedClaimsAndTTL`：新 root 拒绝旧 access JWT。
- `TestReplayCipherEnvelopeAndAADContract`：新 root 拒绝旧 replay ciphertext。
- `TestRootRotationChangesLimiterNamespace`：相同 action/subject/source 在新 root 下得到不同 limiter digest。
- `TestPasswordResetAndChangeRevokeAllRefreshFamilies`：受审 transaction 能撤销同账号全部 refresh family。
- runner 输出 `dual_key_mode=false`，不建设长期双 key。

失败恢复按是否已经签发新 token 分界：未签发时可在审批下临时回装旧 key；已经签发后保持注册关闭、再次全撤销，
不把旧 key 作为长期 fallback。所有真实动作仍需 owner 独立授权。

## 验证结果

`./scripts/test-auth-rotation-rollback.sh` 输出：root-key-negative、session-revocation、limiter-schema-rollback、
legacy-boundary 全部 passed，并生成固定、无 secret 的 `AUTH_ROTATION_REHEARSAL` 报告。
