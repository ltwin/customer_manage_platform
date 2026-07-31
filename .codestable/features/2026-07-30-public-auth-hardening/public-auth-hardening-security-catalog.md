---
doc_type: feature-evidence
feature: 2026-07-30-public-auth-hardening
evidence_kind: auth-security-catalog
status: passed
generated: 2026-07-31
---

# Public Auth Security Catalog 证据

## 1. 结论

`scripts/test-auth-security-catalog.sh`以synthetic数据重放公开认证攻击面、账号隔离和发布安全边界。13个固定case全部通过；每个case写入临时成功marker，A1～A18再通过显式依赖映射逐场景输出passed，缺case、Go测试空匹配或数量漂移均fail closed：

```json
{"version":1,"status":"passed","synthetic":true,"scenarios":["A1","A2","A3","A4","A5","A6","A7","A8","A9","A10","A11","A12","A13","A14","A15","A16","A17","A18"],"production_effect":false}
```

目录不依赖系统`migrate`或benchmark工具，不读取`.env`，不发送真实邮件，也不执行deploy、production migration／rollback、cutover、root rotation或公开注册开关切换。

## 2. A1～A18 映射

| Scenario | 固定case | 主要机械证据 |
|---|---|---|
| A1 | `password-session`、`timing-enumeration`、`event-redaction` | generic forgot、各目标分支timing预算和固定事件allowlist／redaction |
| A2 | `password-session` | purpose-bound／single-use reset token、过期／重放／错误purpose固定失败 |
| A3 | `password-session`、`origin-cookie` | bcrypt更新、全部refresh family撤销和成功cookie清理 |
| A4 | `password-session`、`origin-cookie`、`frontend-auth`、`event-redaction` | wrong current password、all-family撤销、anonymous状态与password_changed allowlist |
| A5 | `origin-cookie` | reset／change Origin、cookie与400／401／403／429／500矩阵 |
| A6～A9 | `limiter-proxy` | PostgreSQL预算、并发／窗口／跨实例、registration=false零消费、Retry-After脱敏、trusted XFF链 |
| A10 | `timing-enumeration` | login一次same-cost bcrypt、mail存在／缺失／provider failure的200样本统计预算 |
| A11 | `api-contract`、`frontend-auth`、`browser-evidence` | OpenAPI Go／TS零漂移、fragment one-shot、无Web Storage、production build与既有1440×900／375×812手工证据 |
| A12 | `password-session`、`frontend-auth`、`origin-cookie` | password成功后的server session撤销、cookie清理和客户端anonymous状态 |
| A13 | `event-redaction` | 七事件allowlist、HTTP／Resend／sink完整邮箱、IP、token、cookie、provider body canary为零 |
| A14～A15 | `monitor` | reuse／rate-limit／mail／legacy阈值边界、JSONL／journald、exit 0／1／2／3、损坏行隔离 |
| A16 | `production-preflight` | 48个false安全基线、true enable-ready、live/evidence/freshness/fingerprint/redaction case |
| A17 | `rotation-rollback` | old access／replay／limiter namespace negative、all-family revoke、0013/0012 rollback与旧binary边界 |
| A18 | `two-account-isolation`、`account-scope-consumers` | 两verified账号全域HTTP隔离、active-only枚举与六类后台消费者 |

## 3. 固定case结果

| Case | 结果 |
|---|---|
| `password-session` | passed |
| `origin-cookie` | passed |
| `limiter-proxy` | passed |
| `timing-enumeration` | passed |
| `api-contract` | passed |
| `frontend-auth` | passed |
| `browser-evidence` | passed |
| `event-redaction` | passed |
| `monitor` | passed |
| `production-preflight` | passed，48 cases |
| `rotation-rollback` | passed |
| `two-account-isolation` | passed |
| `account-scope-consumers` | passed |

所有Go case统一使用`go test -p=1 ... -count=1 -parallel=1`。日志写入权限受限的`mktemp`目录并在退出时删除；失败只允许输出固定case名和最多120行无禁项日志。如果日志命中DSN、secret env、完整email、JWT／refresh token形状、Authorization/Bearer、Set-Cookie或provider body标记，目录自身fail closed且不回显命中内容。

## 4. CMD-001～007

| ID | 结果 | 说明 |
|---|---|---|
| CMD-001 | passed | auth／store／httpapi核心包全部通过；store和HTTP使用真实PostgreSQL/Testcontainers |
| CMD-002 | passed | accountctl monitor／readiness／legacy命令全部通过 |
| CMD-003 | passed | frontend auth 11/11、api-client 6/6、production build通过；仅既有chunk warning |
| CMD-004 | passed | OpenAPI Go／TS生成物零漂移 |
| CMD-005 | passed | production preflight 48 cases |
| CMD-006 | passed | 本security catalog 13 case、A1～A18全绿；场景由case marker显式映射而非无条件输出 |
| CMD-007 | passed | build、lint、全仓Go／frontend、legacy／ops／security catalog与generate-check全部通过 |

CMD-007首轮仅在lint发现monitor fixed diagnostic的3个未处理write error；窄修后accountctl测试和lint 0 issues，完整命令重跑通过。没有隐藏或跳过失败。

## 5. 清洁度与边界

- `git diff --check`通过。
- 新增catalog、E2E fixture与monitor窄修无debug output、临时TODO／FIXME／XXX或sleep-based timing test。
- 测试数据全部synthetic；证据不保存完整邮箱、IP、password、token、cookie、Authorization、DSN、API key或provider body。
- `production_effect=false`：没有生产数据库、邮件、部署、迁移、rollback、rotation、cutover或注册开关副作用。
