---
doc_type: feature-review
feature: 2026-07-15-telegram-digest
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-16
round: 2
round2_reviewed: 2026-07-17
round2_reviewer: independent-subagent
round2_status: passed
---

> **Round 2 (2026-07-17) — PASS.** 独立 code-review subagent 复审 review-fix diff：REV-001/003/004/005/006 CONFIRMED-FIXED，REV-007 已改，REV-002 owner 合法延后且未隐藏新 bug；0 新 blocking/important，仅 2 条非阻塞 suggestion（REV-006 repair 在 gate 内的 ≤2s 上界、release 退避覆写——均为既有、有 CAS/lease 兜底）。独立重跑 `go build`/`go vet`/digest+cmd/server 测试全绿。Verdict：clear to proceed to QA；真机 S19 仍为 owner-gated QA 项，非代码问题。详见 §7。

# telegram-digest 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md`（`status: approved`）
- Checklist: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-checklist.yaml`（STEP-001～008 全 `done`；CHK-* 仍 pending 属验收字段）
- Evidence pack: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-evidence-pack.md`
- Gate results: `telegram-digest-gate-results.json`（`implementation.before_review` passed）
- DoD results: `telegram-digest-dod-results.json`（CMD-001～005 exit 0）
- Implementation evidence: `telegram-digest-implementation.md`（status completed）
- Diff basis: 工作区 unstaged + staged + untracked；基线 `8420d4f`；分支 `codex/feat/telegram-digest`
- Baseline dirty files: none beyond 本 feature 可归因改动（roadmap/VISION/settings/openapi/frontend/digest 全属本条）

### Independent Review

- Detection: Paseo daemon 不可用 → 宿主原生 Task agent（`general-purpose` read-only）；`ocr` CLI 可用且 `ocr llm test` 通过
- 环节 A 独立隔离 Task agent: `native-agent` + `completed`（subagent `019f68fa-b060-7943-b674-ac980a5cd940`）
- 环节 B OCR CLI: `completed`（`ocr review --audience agent`）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 两路结果均经本地 `file:line` 与 design 契约核验后合并；未核验的 OCR 性能/规模建议降为 residual 或 nit
- Gate effect: 无 pending 环节；`reviewer: subagent+ocr` 可写

## 2. Diff Summary

- 新增：
  - `backend/internal/reminder/digest/**`（应用 + Telegram adapter + 状态机测试）
  - `backend/internal/platform/httpapi/telegram_binding.go` / `_test.go`
  - `backend/internal/platform/store/migrations/0011_telegram_digest.{up,down}.sql` + migration test
  - `frontend/src/components/telegramBinding.ts`、`frontend/scripts/telegram-digest.test.ts`
  - `.codestable/features/2026-07-15-telegram-digest/**`、`docs/telegram-digest-runbook.md`、`requirements/telegram-digest.md`
- 修改：
  - `backend/cmd/server/main.go`（单 TelegramRunner 装配）、config、settings 列级 Upsert、`store/scope.go` Upsert 列约束
  - OpenAPI tag / oapi-codegen / staged `api.gen.go`、frontend Settings 卡片、Makefile/compose/env
  - roadmap items：`telegram-digest=in-progress`
- 删除：none
- 未跟踪 / staged：见上；`api.gen.go` 已 staged
- 风险热点：外部 Bot I/O、claim/lease 并发、AccountScope 绑定、凭证生命周期、入站 offset 确认、前端 deep-link

## 3. Adversarial Pass

- 假设的生产 bug：SIGTERM 卡在 `SendMessage` 时 production adapter 错误分类 cancel，烧掉 failure budget 并放大重复投递窗口
- 主动攻击过的反例：
  - design 不一致：S3 安全提示缺失；D7 send-started cancel 与 production path 不一致
  - 错误路径：`classifyTransport` 吞掉 `context.Canceled`
  - 状态转换：FinalizeStale(nil) 被当成成功（已知 at-least-once 窗口）
  - 并发时序：gate 内 snapshot 装配拉长 rebind 等待
  - 权限/隔离：token/chat scoped 枚举、列级 Settings PATCH — 未发现越权
  - 测试假阳性：cancel 测试仅 Fake 返回 raw `ctx.Err()`，绕过 adapter
- 结果：B1 升级为 blocking；安全提示/日志/gate 边界为 important；OCR 若干 Medium 核验后降级或并入 residual

## 4. Findings

### blocking

- [x] REV-001 `backend/internal/reminder/digest/telegram/client.go:138-146` + `sender.go:135-154` Production adapter 将 parent cancel 误分类为 `network`，破坏 send-started cancel 语义 — **FIXED (review-fix round 1, 2026-07-17)**：`classifyTransport` 先 `errors.Is(context.Canceled)` 原样透出；新增生产 client cancel 单测 + `sender_cancel_integration_test.go` 真 client×sender 端到端（attempts 0、claim 保留、finalize/release 0 次）
  - Evidence:
    - `classifyTransport` 对非 timeout 的 transport error 一律 `TelegramErrorNetwork`，**不**保留 `context.Canceled`
    - `DeliverySender` 仅在 `errors.Is(sendErr, context.Canceled)` 时走「保留 claim、不烧 budget」；`TelegramError` 分支会 `finalize(... ErrorKind:network)` → `attempts+1`
    - design D7 表：`SendMessage 已开始，parent cancel / outcome unknown → attempts delta 0，不立即重发、不清 claim`
    - `TestDeliverySenderSendStartedCancellationLeavesClaimForRepairOrLease` 使用 `cancelOnSendTelegram` 直接返回 `ctx.Err()`（`retry_test.go:100-124`），**不经 production client**，因此假绿
  - Impact: 关停/超时窗口错误消耗 failure budget；Telegram 已接受时立刻重投窗口被放大；验收 S23 send-started cancel 在真路径不可信
  - Expected fix scope:
    - `classifyTransport`：`context.Canceled`（及可识别的 parent cancel）不得包成 network
    - sender 保持：true timeout → timeout kind 可计 budget；parent cancel → 0 budget + 保留 claim
    - 补 **production client 或等价 transport** 的 cancel 集成测试，禁止只测 Fake
  - Source: `native-agent`（本地核验确认）

### important

- [~] REV-002 `backend/internal/reminder/digest/update_handler.go:51-66` 入站失败路径无 best-effort 安全提示 — **DEFERRED (owner decision, review-fix round 1)**：与 design line 155/400「单一 DeliverySender 唯一 TelegramSender caller」冲突（无账号入站路径无法建 account-scoped Delivery 承载回执），需单独设计决策；本轮不实现，写入 §6 residual risk。当前失败 update 仍 terminal-consumed（durable、推进 offset、不建业务 Delivery），仅缺可观察回执
  - Evidence: design S3 / §错误语义要求无效 token、群聊 `/start`、未绑定 `/today`、未知命令返回安全提示且不泄漏账号/token；当前仅 durable terminal `true` 并沉默
  - Impact: 验收矩阵可观察结果不满足；用户误判系统故障
  - Expected fix scope: 固定无 PII 文案、best-effort 发送失败仍 terminal-consumed、不创建业务 Delivery
  - Source: `native-agent`（design S3 核验）

- [x] REV-003 `backend/internal/reminder/digest/daily.go:148-172` daily scan/enqueue 失败静默丢弃 — **FIXED (review-fix round 1)**：`NewDailyScheduler` 注入 `*slog.Logger`，enumerate/due/exists/scan/enqueue 失败结构化记录 `account_id`+`target_local_date`+`error`（禁 token/chat/正文）；新增 `TestDailySchedulerLogsSanitizedScanFailure`
  - Evidence: design D5「扫描失败只记结构化错误并留待下个 tick」；`RunOnce` 对 enumerate/scan/enqueue 错误直接 `continue`/`return`，无 slog
  - Impact: 生产无法核对「scan 卡住 / enqueue 失败」；S13 可观测证据链断
  - Expected fix scope: 注入 logger，至少记录 account_id、target date、error（禁 token/chat/正文）
  - Source: `native-agent` + `ocr`（Medium 升级为 important）

- [x] REV-004 `backend/internal/reminder/digest/sender.go:104-133` RecipientGate 临界区包含跨域 Snapshot 装配 — **FIXED (review-fix round 1)**：`messages.Build` 移出 gate，在进入 `WithSend` 前按 claim 冻结字段渲染；gate 内顺序回到 D7（reload → recipient → call-boundary → ≤10s Send → 短 CAS），build 失败按 pre-send cleanup 释放 claim
  - Evidence: design D7 固定 gate 内顺序为 reload → recipient → call-boundary → ≤10s Send → 短 CAS；实现在 gate 内调用 `messages.Build`（snapshot 多表读）
  - Impact: rebind 等待可能远超「当前 active send 的 ≤10 秒网络调用」边界
  - Expected fix scope: snapshot 移出 gate 或加载后再次 fence；若保留须回写 design 并评估 rebind 延迟
  - Source: `native-agent`

- [x] REV-005 `backend/internal/reminder/digest/binding.go:220-223` + `update_handler.go:53` 不接受 Telegram `/start@botusername` 命令形态 — **FIXED (review-fix round 1)**：新增 `parseCommand` 剥离可选 `@bot`；`HandleUpdate`/`HandleStart` 都经它规范化路由；新增 `TestParseCommandStripsOptionalBotMention` + `TestBindingServiceAcceptsStartWithBotMention`
  - Evidence: `HandleStart` 要求 `fields[0] == "/start"`；`UpdateHandler` 用 `HasPrefix("/start")` 路由，`/start@bot token` 会进入绑定流后被 BindInvalid，offset 仍推进
  - Impact: 部分客户端 deep-link 绑定静默失败且不可重放同一 update
  - Expected fix scope: 规范化命令（剥离可选 `@bot` 并与配置 bot username 比对）
  - Source: `ocr`（本地核验确认）

- [x] REV-006 `backend/internal/reminder/digest/sender.go:173-180` `finalize` 将 `FinalizeStale`+nil error 当作成功 — **FIXED (review-fix round 1)**：仅 `FinalizeApplied` 视为成功；`FinalizeStale`（含 +nil）按 claim 重读，仅当仍是 current pending owner 且 lease 未过期时幂等重试同一 result CAS，不重发；新增 `TestDeliverySenderRetriesStaleFinalizeWhileClaimStillOwnsDelivery`
  - Evidence: `FinalizeAttempt` 在 `n!=1` 时返回 `(FinalizeStale, nil)`；`finalize` 只看 `err == nil` 即返回，不检查 outcome、不进入 reload/repair
  - Impact: 成功 send 后 lease 过期时本地可能仍 pending，依赖后续 reclaim 重发；与 design 已接受的 at-least-once residual 同族，但当前路径跳过「仍有效 claim 时幂等重试同一 result CAS」
  - Expected fix scope: 仅 `FinalizeApplied` 视为成功；Stale 时按 claim 重读并按 design repair 语义处理
  - Source: `ocr` High（映射 important；不升 blocking，因 design 已披露成功后本地未确认 residual）

- [x] REV-007 文档/契约描述滞后 — **FIXED (review-fix round 1)**：`api/openapi.yaml` settings tag 删除「未实现」；`make generate` 后 `api.gen.go`/`schema.d.ts` 无 drift。runbook 已在 STEP-008 明确 Telegram 生产配置语义（env-only secret、getUpdates-only、无 Webhook），非「仅 smoke」
  - Evidence: OpenAPI settings 描述仍可能写「telegram 未实现」类文案（实现已接通）；运维 README 若仍写 token 仅 smoke 会误导
  - Impact: 后续 feature/运维误读能力边界
  - Expected fix scope: 与本条一并校正 OpenAPI 描述与 runbook/README 中 Telegram 配置语义
  - Source: `native-agent`

### nit

- [ ] REV-008 `backend/internal/reminder/digest/policy.go:51-59` send 分支策略表未接入 `DeliverySender`，形成第二真相源风险
  - Source: `ocr`

- [ ] REV-009 `backend/internal/reminder/digest/adapters.go` production `NewClient(..., nil)` 回落 `http.DefaultClient` 无全局 Timeout（依赖 per-call context）
  - Source: `ocr` / `native-agent`

- [ ] REV-010 `frontend/src/components/telegramBinding.ts:11` `noopener,noreferrer` 在部分浏览器使 `window.open` 返回 null，可能误报弹窗拦截
  - Source: `ocr`

- [ ] REV-011 `backend/internal/reminder/digest/telegram/client.go:122-128` 非 JSON 错误体先 decode 失败会归 `protocol_error`，可能把 5xx/429 误挂起 poll 策略
  - Source: `ocr`

- [ ] REV-012 `RecipientGate` 无嵌套/同 goroutine 重入 runtime 断言（design 禁止靠纪律）
  - Source: `native-agent`

### suggestion

- [ ] REV-013 `ReloadClaim` 接口含 claimID 但未参与查询条件，建议按 claim 条件读取防误用
  - Source: `native-agent`

- [ ] REV-014 账号规模化前保持 AccountScope 枚举可接受；未来再做受审计身份索引（design 已披露 O(accounts)）
  - Source: `ocr`（性能建议，首版单账号不阻塞）

### learning

- Fake adapter 测试必须覆盖 production error 分类路径；cancel/timeout 若只在 Fake 返回 raw `ctx.Err()`，会系统性漏掉 `classifyTransport` 包装 bug（REV-001）。
- 「best-effort 安全提示」易在 durable offset 做完后被省略；应用验收矩阵强制可观察结果。
- RecipientGate 临界区膨胀会把 rebind 延迟绑到读模型性能上。

### praise

- AccountScope 纪律扎实：token/chat 只走 enumerator；客户端 payload 不构造 scope。
- Settings 列级 PATCH 有真实并发回归测试，避免 stale read 解绑。
- Delivery claim/lease/stale writer/supersede/fence/reclaim 测试质量高。
- `/today` message_kind 冻结（scan 失败 → temporary_unavailable，重投不换 kind）与测试对齐。
- 凭证：DB 只存 hash；错误脱敏；前端不渲染 token、不用 storage；compose `replicas=1` + stop-first。
- 摘要口径未复用 dashboard 近 3 天窗口；shoot-only、窄 unpaid、3500 code point 有测试。

## 5. Test And QA Focus

- QA 必须重点复核：
  1. 修完 REV-001 后：SIGTERM/cancel 正在 `SendMessage` 时 attempts 仍为 0、claim 保留
  2. 真机 S19：bind → binding_ack → daily → `/today` 三图脱敏（implementation 明确未伪造）
  3. 无效 token / 群聊 / 未绑定 `/today` / `/start@bot`：安全提示与不泄漏
  4. scan 失败：daily 不发；`/today` temporary_unavailable；同 update 重投不换 kind
  5. Settings PATCH 与 rebind 并发：chat 不被写回
  6. 跨账号 chat 冲突；digest_hour 后重启补发且 local-date once
  7. 前端 DOM/storage 无 token；375px 无横向溢出
  8. Telegram disabled/invalid config 时 Web/Reminder 正常
- Evidence pack residual risks / gate warnings：Vite chunk >500kB 既有警告；真机证据未齐
- 建议新增或加强的测试：
  - P0：production client + canceled context → 不 finalize network、不 +attempts
  - P1：无效 start/today 安全提示（fake sender 断言文案）
  - P1：daily scan/enqueue 失败结构化日志契约
  - P2：gate 内慢 snapshot 时 rebind 等待上界；非 JSON 5xx 分类；popup 误报
- 不能靠 review 完全确认的点：真 Bot 限流/webhook 现场；浏览器弹窗拦截生命周期；compose replicas 在非 swarm 下是否被忽略；长停机 >24h 丢 update

## 6. Residual Risk

- Telegram 成功但本地 finalize 失败 → 重复投递：design 已接受 at-least-once；REV-006 收紧后可缩小窗口
- Bind link 10 分钟 possession capability：A9 已接受
- 长停机 >24h 丢未确认 updates：已披露
- `replicas>1` 无 leader election：A4；靠部署约束，非代码保证
- 真机 S19：留待 QA owner-stop，不得用 fake 冒充
- **入站失败无安全提示（REV-002，owner 于 review-fix round 1 延后）**：无效 token / 群聊 `/start` / 未绑定 `/today` / 未知命令当前 terminal-consumed 且静默，不回安全提示。根因是 design line 155/400「单一 DeliverySender 唯一 caller」与 line 142/406/S3「失败回执」在无账号入站路径上冲突，需单独设计决策后再实现，不在本 feature review-fix 轮内改公开契约。可观察影响：用户对无效操作无即时反馈；不泄漏账号/token，不建业务 Delivery，不影响队列/offset/其他账号。

## 7. Verdict

### Round 1 (2026-07-16) — changes-requested

- Status: **changes-requested**
- Next: 回到 `cs-feat` implementation 的 **review-fix**（只修 blocking；important 建议同轮至少处理 REV-002/003；其余 important 可由 owner 决定是否延后并写入 residual）
- 不建议 `blocked`：主骨架与隔离/幂等核心正确，修复面集中在 adapter 分类、安全提示、日志与 gate 边界，无需推倒重来
- 修完 blocking 后必须 **重跑 `cs-code-review`**，不得直接进 QA

### Round 2 (2026-07-17) — passed

- Status: **passed**（独立 subagent 复审，非本地判定）
- 结果：REV-001（blocking）CONFIRMED-FIXED（含生产 client × sender 端到端 cancel 集成测试，堵住 round-1 Fake 假绿）；REV-003/004/005/006 CONFIRMED-FIXED；REV-007 已改且 codegen 无 drift；REV-002 owner 合法延后并写入 §6 residual，changed code 无隐藏新 bug。
- 新发现：0 blocking / 0 important；2 条非阻塞 suggestion（REV-006 repair 在 gate 内 ≤2s 上界；release 退避覆写）——均为既有行为、有 claim-id CAS + 30s lease 兜底，不 gate QA。
- 独立验证：`go build ./...`、`go vet`、digest + cmd/server 测试全绿。
- Next: 进入 **QA**。真机 S19（bind/daily/`/today` 三张脱敏截图）为 owner-gated QA 项，须 owner-managed secret + owner-stop，不得用 fake 冒充。
