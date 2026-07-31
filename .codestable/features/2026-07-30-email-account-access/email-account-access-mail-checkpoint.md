---
doc_type: feature-external-checkpoint
feature: 2026-07-30-email-account-access
step: STEP-004
status: passed
updated: 2026-07-31
---

# STEP-004 真实邮件 Provider Checkpoint

## 1. 已完成的真实 Adapter

- Provider：Resend。
- Transport：HTTPS API。
- 请求契约来自 Resend 官方文档：`POST /emails`、Bearer authentication、JSON content type、显式 User-Agent 与 24 小时幂等键。
- Adapter 固定 4 秒单次 HTTP timeout、5 秒总 deadline、最多 2 次尝试；只有 transport error、HTTP 429 和 5xx 可重试。
- HTTP 401／403 映射为 `misconfigured`，其他 4xx 映射为 `provider_rejected`，429／5xx 映射为 `temporarily_unavailable`；caller cancellation／deadline 保留为对应 typed failure。
- 成功事件只允许 `event`、provider、purpose、status、provider message ID 与 accepted time；失败事件只允许固定 failure class，不记录 recipient、action URL、credential 或 provider response body。

## 2. 本地证据

- RED：新增 adapter／config tests 后因缺 `Resend`、`ResendAPIKey`、`AuthMailFrom` 与 `ErrResendAPIKeyMissing` 编译失败。
- GREEN：`go test ./internal/platform/authmail ./internal/platform/config ./cmd/server -count=1` 通过，0 失败。
- 覆盖：成功 receipt、Bearer/User-Agent/Content-Type、不可逆 idempotency key、日志脱敏、最多一次 retry、HTTP failure mapping、caller cancellation、whole-operation deadline、config fail-fast 与 composition root。
- 额外修正 deterministic JWT 测试的 inspect parser clock，使其不依赖真实墙钟；目标测试通过。

## 3. 真实外部请求结果

- 首轮已使用 owner-controlled `.env` references 发起受控非生产请求，结果为 HTTP 403、typed failure class=`misconfigured`，未取得 provider message ID；失败 evidence 只保留 HTTP status 与固定分类。
- Owner 完成 permitted recipient alignment 并以固定 resume action 恢复后，只重跑一次受控 live receipt。
- 最终结果：HTTP 2xx accepted。
- Provider message ID：`f646ede0-b7c5-47cc-8d3c-db76bca67c41`。
- Accepted time：`2026-07-31T04:09:32Z`。
- Live runner 同时核验 redacted `auth.mail_delivery` accepted event 存在，且不含 API key、完整 recipient、action URL 或 action bearer。
- 未保存或输出 API key、完整 recipient、action URL、响应正文或 provider body；最终 evidence 只保留 receipt allowlist。
- Checkpoint 可以置为 `receipt-verified`，STEP-004 可以标为 `done`。

## 4. Checkpoint Closure

- Owner 选择了 permitted recipient alignment 路径；本次 closure 不涉及采购服务、修改 DNS 或选择 production sender identity。
- Resend adapter、bounded deadline、finite attempts、typed failure、accepted receipt 与 redacted event 已全部具备。
- Production sender／domain readiness 仍由后续 production preflight fail closed，不由本次非生产 receipt 自动放行。
