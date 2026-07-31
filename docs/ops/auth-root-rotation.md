# 认证根密钥轮换与失败恢复

本文描述 `AUTH_TOKEN_SECRET` 的生产轮换顺序。它是一份人工 runbook，不是自动化入口。仓库内
`./scripts/test-auth-rotation-rollback.sh` 只用 synthetic secret 与一次性 PostgreSQL 验证旧 access、
refresh family、replay ciphertext、limiter namespace 和 migration rollback 边界；不会读取生产环境、
修改生产数据库、切换公开注册、部署或轮换真实密钥。

## 适用边界

- `AUTH_TOKEN_SECRET` 是 root secret。运行时固定派生
  `crm-auth/v1/access-signing`、`crm-auth/v1/refresh-replay-aead` 和
  `crm-auth/v1/limiter-hmac` 三个 32-byte 子密钥。
- 轮换会使旧 access JWT、旧 replay ciphertext 和旧 limiter digest namespace 失效；refresh wire token
  本身不是由 root secret 签名，所以切换 root secret 前必须撤销全部 refresh family。
- 不长期同时接受旧、新 access/replay/limiter key。新 key 已签发 token 后，不把旧 key 作为常驻 fallback。
- 本 runbook 不授权生产 deploy、数据库写、密钥管理系统变更、公开注册开关变更或 cutover��每项真实动作都要
  有 owner 单独批准的变更单、维护窗和回滚负责人。

## 轮换前准备

1. 确认当前发布 revision、数据库 schema version、非秘密配置 fingerprint 和 secret version reference。
   secret version reference 是版本别名，例如 `vault:crm/auth-token:v3`；不得把 secret 值写进版本字段。
2. 准备两份独立回滚材料：当前 root secret 的受控恢复引用，以及新 root secret 的受控引用。任何报告、命令行、
   shell history、截图和工单都只记录引用，不记录值。
3. 运行 `./scripts/test-auth-rotation-rollback.sh`，确认 synthetic rehearsal 全绿。
4. 为当前配置准备 fresh 的 mail accepted receipt、monitor、安全 catalog、rotation 和 rollback evidence envelope。
5. 记录维护窗起止、operator、审批引用、当前 revision 和当前 secret version reference；确认公开流量入口可以保持
   “登录/恢复可用、创建新账号关闭”的状态。

## 固定执行顺序

顺序不可交换：

1. 将 `AUTH_PUBLIC_REGISTRATION_ENABLED` 设为 `false`，部署该配置并确认
   `GET /api/v1/auth/capabilities` 返回注册关闭。此动作需单独授权。
2. 记录维护窗已开始，并阻止其他发布或认证配置变更并行发生。
3. 在单一受审事务中撤销数据库内全部尚未撤销的 refresh family，并记录受影响行数的脱敏审计结果。仓库故意
   不提供可直接指向生产库的自动 rotation/down 脚本；真实撤销步骤必须由当次变更单携带、人工复核并单独授权。
4. 确认旧 refresh token 已固定失败后，把 secret manager 中的 root secret 引用切到新版本；同时更新
   `AUTH_TOKEN_SECRET_VERSION`，使 config fingerprint 绑定新版本引用。
5. 以同一新 root secret 重启所有签发与校验实例。不得留下仍使用旧 key 的并行实例。
6. 运行负向验证：旧 access JWT 失败；旧 refresh token 失败；旧 replay ciphertext 不能解密；相同 action/subject/
   source 在新 `limiter-hmac` namespace 下得到不同 digest。验证材料只记录布尔结果和不可逆引用。
7. 使用当前 revision、新 schema version、新 config fingerprint 和 fresh evidence 运行 production preflight。
   只有输出 `complete/preflight=enable-ready` 才表示“具备恢复公开注册的条件”。
8. 结束维护窗前观察 `accountctl auth monitor --cutover-mode=active`；存在 refresh reuse、legacy claim failure、
   异常限速或邮件连续失败时保持注册关闭。
9. 只有 owner 对“恢复公开注册”再次独立授权后，才把 `AUTH_PUBLIC_REGISTRATION_ENABLED` 改为 `true` 并发布。

## 失败恢复

按“是否已用新 key 签发任何 token”分界：

- 尚未签发新 token：保持公开注册关闭，再次确认全部 refresh family 已撤销；若新配置无法启动，可在审批人确认后
  临时回装旧 root secret 引用以恢复服务。恢复后重新开始完整轮换，不能跳过撤销和负向验证。
- 已签发新 token：不要长期 dual-key，也不要直接回装旧 key。保持公开注册关闭，再次撤销全部 refresh family，
  修复新配置并以单一 key 重启。若业务必须回装旧 key，视为新的完整轮换事件，重新执行全部撤销、重启和负向验证。
- preflight 或 monitor 失败：保持注册关闭；修复缺失、过期、future-skew、revision/schema/fingerprint mismatch、
  limiter schema、legacy cutover 或邮件证据。不得把 `status=failed` 改成 `passed` 来绕过。
- migration rollback：只有 synthetic catalog 已覆盖的 legacy-only 边界可作为计划依据。存在新式账号时 0012 down
  必须以 `auth_schema_down_blocked_new_accounts` fail closed；仓库不提供生产 down 命令。

## 完成条件

- 所有运行实例只使用同一新 root secret 版本。
- 旧 access、refresh、replay 和 limiter namespace 四项负向验证均通过。
- production preflight 对当前 revision/schema/config 输出 `enable-ready`。
- monitor 无未处理 high/warning，维护窗记录与 evidence envelope 不含 secret、完整邮箱、数据库连接串或 token。
- 公开注册是否恢复有独立 owner 授权；没有授权时，系统保持 `secure-baseline-ready`。
