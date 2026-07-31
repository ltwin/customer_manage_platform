---
doc_type: feature-evidence
feature: 2026-07-30-public-auth-hardening
evidence_kind: auth-monitor
status: passed
generated: 2026-07-31
---

# 认证事件与 monitor 证据

## 1. 固定事件协议

共享模块 `backend/internal/platform/authevent` 固定七类事件：`auth.login`、`auth.email_verified`、`auth.rate_limited`、`auth.refresh_reuse`、`auth.mail_delivery`、`auth.password_changed`、`auth.legacy_claim`。

事件 payload 只允许：时间、event、result、固定 failure class、脱敏 account／session／family ref、action、source digest、provider message ID，以及仅供 legacy claim 使用的 boolean `dry_run`。JSON slog 的 `time`／`level`／`msg` 仅作为日志 envelope；事件生产者不记录完整邮箱／IP、密码、token、cookie、Authorization、provider body或损坏输入原文。

生产者证据：

- HTTP：login／email verified／rate limited／refresh reuse／password changed 使用同一 `authevent.Event.Attrs()`。
- Mail：Resend accepted／failure 使用 `result=success|failure`、action与固定failure class；不再输出provider／purpose／status／accepted_at自造字段。
- Ops：legacy claim的allowlisted事件与CLI业务报告分离；事件始终带boolean `dry_run`，命令报告可继续提供脱敏remediation信息。

## 2. Monitor 阈值矩阵

| 规则 | 负边界 | 正边界 | 结果 |
|---|---:|---:|---|
| refresh reuse | 无reuse | 任一reuse | high |
| 5m rate limit global | 19 | 20 | warning |
| 5m rate limit same source | 4 | 5 | warning |
| mail连续失败 | 4 | 5 | warning |
| 15m mail failure rate | 10样本／2失败 = 20% | 10样本／3失败 > 20% | warning |
| legacy dry-run failure | `dry_run=true` | `dry_run=false` | off=warning；active=high |

窗口按事件时间稳定排序，刚好5m／15m边界仍包含在窗口内；输出只给固定alert code、severity和计数，不回显source或输入行。

## 3. 输入与退出码矩阵

| 输入 | 告警 | 损坏 | Exit | 状态 |
|---|---:|---:|---:|---|
| 空／正常JSONL | 否 | 否 | 0 | clean |
| 告警JSONL | 是 | 否 | 1 | alert |
| 损坏行后正常事件 | 否 | 是 | 2 | degraded |
| 损坏行后reuse事件 | 是 | 是 | 3 | alert_degraded |
| reader／file open失败 | 否 | 是 | 2 | degraded |

默认读取stdin；`--file`读取JSONL文件；`--source=journald`从journald JSON的`MESSAGE`归一化事件。结构化auth行损坏时只向stderr写`line=<n> class=<fixed>`并继续；非auth的普通journald消息被忽略，不误报degraded。未知auth字段固定归类`event_schema`，损坏原文canary为零输出。

## 4. 验证

- `go test -p=1 ./cmd/accountctl/... -count=1 -parallel=1`：通过。
- `go test -p=1 ./internal/platform/authevent/... ./internal/platform/authmail/... -count=1 -parallel=1`：通过。
- `go test -p=1 ./internal/platform/httpapi/... -count=1 -parallel=1`：通过，33.792s。
- `git diff --check`：通过。
- 生产源码debug／TODO／FIXME／XXX／sleep扫描：零命中。
- 一次早期聚合回归遇到项目attention已记录的Docker `port "5432/tcp" not found`瞬态；按项目约定包内串行重跑后全部通过，没有以跳过测试换取绿灯。
