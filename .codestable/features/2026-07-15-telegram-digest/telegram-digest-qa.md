---
doc_type: feature-qa
feature: 2026-07-15-telegram-digest
status: passed
tested: 2026-07-17
round: 1
---

# telegram-digest QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md`（approved）
- Checklist: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-checklist.yaml`（steps 全 done；checks pending 待 acceptance）
- Review: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-review.md`（round 2 = **passed**，无 unresolved blocking）
- Evidence pack: `.codestable/features/2026-07-15-telegram-digest/telegram-digest-evidence-pack.md`
- Gate results: `telegram-digest-gate-results.json`（scope-gate passed）
- DoD results: `telegram-digest-dod-results.json`（CMD-001~005 exit 0）
- Diff basis: 工作区 unstaged + staged + untracked；基线 `8420d4f`；分支 `codex/feat/telegram-digest`；含 review-fix round 1 改动
- Baseline dirty files: `install-cpamp.sh`（仓库根未跟踪脚本，与本 feature 无关，不在 QA 结论范围）；其余 dirty/untracked 全属本 feature
- Feature type: **functional**（新增账号级 Telegram 安全绑定、每日/`/today` 摘要投递、入站命令、可靠重试；改变用户可见行为、持久化、外部集成、运行时后台任务）
- Core evidence gate: 安全绑定（S1–S4）、摘要口径/渲染（S6–S8、S11、S22）、每日/`/today` 投递与去重（S5、S9、S10、S14、S30）、失败重试与 attempt 预算（S12、S13、S25）、并发 gate/claim/fence/cancel（S23、S24、S26）、账号隔离（S18）、配置可选与降级（S15）、生命周期有界退出（S20）、前端设置卡片（S16、S17）——均要求运行证据；真机 S19（真 Bot 三图）为 owner-gated，见 §5 residual。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | DoD CMD-001 | core-functional | 聚合门禁（build+lint+全量串行测试+codegen 一致） | build/lint/test | `make check` | exit 0 | pass |
| QA-002 | DoD CMD-002 | core-functional | OpenAPI 双端生成物无 drift | diff | `make generate && git diff --exit-code -- api.gen.go schema.d.ts` | exit 0 | pass |
| QA-003 | DoD CMD-003 | core-functional | digest/settings/config/httpapi/cmd-server 串行测试 | integration | `go test ./internal/reminder/... ./internal/settings/... ./internal/platform/config/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` | 全 ok | pass |
| QA-004 | DoD CMD-004 | supporting | 前端 Telegram 卡片契约测试 | function | `npm run test:telegram-digest` | 4/4 | pass |
| QA-005 | DoD CMD-005 | supporting | 无空白错误 | diff | `git diff --check` | exit 0 | pass |
| QA-006 | review REV-001 / QA focus #1 | core-functional | send-started cancel：真 client × sender 不烧 budget、保留 claim | integration | `TestDeliverySenderWithProductionClientKeepsClaimOnSendStartedCancel` + `TestClientPreservesParentCancellation…` | attempts 0、finalize/release 0、cancel 不成 network | pass |
| QA-007 | review REV-006 | core-functional | FinalizeStale+nil 不当成功、幂等重试不重发 | integration | `TestDeliverySenderRetriesStaleFinalizeWhileClaimStillOwnsDelivery` + `TestDeliverySenderRepairsResultCommitUnknownWithoutResending` | 2 finalize、1 Telegram call、status=sent | pass |
| QA-008 | review REV-003 | supporting | daily scan/enqueue 失败结构化脱敏日志 | function | `TestDailySchedulerLogsSanitizedScanFailure` | 含 account/date/error，不含 chat/token | pass |
| QA-009 | review REV-005 | supporting | `/start@bot`、`/today@bot` 命令规范化 | unit+integration | `TestParseCommandStripsOptionalBotMention` + `TestBindingServiceAcceptsStartWithBotMention` | 剥离 @bot 正确路由/绑定 | pass |
| QA-010 | review REV-004 / D7 | core-functional | gate 临界区顺序（Build 移出）+ call-boundary fence | integration | `TestDeliverySenderCallBoundaryFenceReleasesWithoutSendingOrBudget` + `TestDeliverySenderFakeFlowClaimsFencesSendsAndFinalizes` + 全 sender 套 | fence 不发/不烧、正常 send 落 sent | pass |
| QA-011 | design S1/S3/S4 | core-functional | 绑定：opaque 单次 token、过期/篡改/重放/群聊/跨账号 chat 冲突 | integration | `TestBindingServiceIssuesOpaqueSingleUseTokenAndReplacesPrevious` / `…PrivateStartConsumesBindsAndSupersedesOldAck` / `…RejectsGroupTamperExpiredAndCrossAccountChat` / `TestPostgresBindingRepositoryConcurrentIssueLeavesExactlyOneCurrentToken` | 全 pass | pass |
| QA-012 | design S18/S28 | core-functional | token/chat resolver 0/1/>1/DBError 跨账号隔离 | integration | `TestBindTokenResolverReturnsZeroOneManyAndDBError` + `TestChatAccountResolverReturnsZeroOneManyAndDBError` | typed outcome 正确 | pass |
| QA-013 | design S6/S7/S8/S11/S22 | core-functional | 摘要口径（20+overflow、DST/跨午夜冻结、窄 unpaid、空态、3500 截断） | integration | `TestRendererIsDeterministicShowsOverflowAndEmptyState` / `TestRendererTruncatesAt3500UnicodeCodePoints` / `TestWindowForUsesFrozenTimezoneDateAndDSTHalfOpenDay` / `TestDigestMessageBuilderUsesDeliveryFrozenTargetAcrossMidnightAndTimezoneChange` / `TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel` | 全 pass | pass |
| QA-014 | design S9/S10/S21 | core-functional | daily scan-before-send、local-date once、重启补发、scan 失败不发 | integration | `TestDailySchedulerScanBeforeSendOnceAndRestartCatchup` + `TestDailySchedulerScanFailureDoesNotEnqueueIncompleteDigest` | 全 pass | pass |
| QA-015 | design S5/S14/S30 | core-functional | `/today` EnsureScan→message_kind→claim、重投不重复/不换 kind | integration | `TestUpdateHandlerTodayFreezesFirstMessageKindAcrossReplay` | kind 冻结、replay 不换 | pass |
| QA-016 | design S12/S13/S25 | core-functional | 失败重试 1/5/30→failed@4、rate_limit retry_after、invalid_auth 不烧、八类 operation×kind 总函数 | integration | `TestDeliveryFailureBudgetUsesOneFiveThirtyThenFailedAtFour` / `TestDeliveryRateLimitUsesLaterRetryAfter` / `TestDeliverySenderInvalidAuthDoesNotBurnFailureBudget` / `TestTelegramPolicyIsTotalForOperationAndKind` / `TestPollPolicyBackoffSuspendAndRateLimit` | 全 pass | pass |
| QA-017 | design S23/S24/S26 | core-functional | 双 claim/lease/stale result CAS、reclaim barrier、gate priority/cancel、pre-send cleanup | integration | `TestDeliverySenderExpiredOwnerDoesNotSendAfterAnotherClaimantReclaims` / `TestPostgresDeliveryRepositorySupersededClaimRejectsLateResult` / `TestRecipientGateGivesQueuedRebindPriorityOverLaterSend` / `TestRecipientGateWaitIsCancellationAware` / `TestDeliverySenderPreSendCancelUsesIndependentBoundedCleanupContext` | 全 pass | pass |
| QA-018 | design S20 | core-functional | SIGTERM/context 取消下 poller/runner 有界退出 | integration | `TestPollerContextCancellationInterruptsLongPoll` + `TestTelegramRunnerStartsAllLoopsAndStopsOnCancellation` | 有界退出 | pass |
| QA-019 | design S15 | core-functional | Telegram 空/非法配置或 invalid_auth 下主应用可用、pending 可恢复 | integration | `TestLoadTelegramConfigurationIsOptional` / `TestPollerInvalidAuthSuspendsSharedIntegration` / `TestDeliverySenderRunnerPausesDuringIntegrationSuspension` / `TestIntegrationSuspensionGuardsBindingUntilProbeWindow` / STEP-008 隔离 disabled 启动 | 主应用/Reminder/Web 正常 | pass |
| QA-020 | design S29 | core-functional | TelegramError/URL/slog 不泄漏 bot token/正文 | integration | `TestClientClassifiesRateLimitAndRedactsToken` + `TestTelegramErrorIsTypedAndRedacted` | 脱敏 | pass |
| QA-021 | design MOUNT-2 | core-functional | bind/delivery migration up/down、chat 唯一、账号约束 | integration | `TestTelegramDigestMigrationUpDownAndConstraints` | 全 pass | pass |
| QA-022 | design S1 / HTTP | core-functional | bind-token 200{token,deep_link}/401 契约 | API/integration | `TestCreateTelegramBindTokenRequiresAuthAndReturnsOpaqueDeepLink` | pass | pass |
| QA-023 | design S16/S17 | supporting | 设置卡片状态/375px/focus/role=alert/无 token 泄漏 | function+browser | `npm run test:telegram-digest`（4/4）+ STEP-007 隔离 375px 真机截图 `evidence/settings-telegram-375-error.jpg` | 无横向溢出、DOM/storage 无 token、role=alert | pass |
| QA-024 | design S2/S19 | core-functional | 真 Bot synthetic：binding→ack→daily→`/today` 三图脱敏 | manual/telegram | owner-managed secret + owner-stop 真机 | 三张脱敏截图 | **residual（owner-gated，见 §5）** |
| QA-025 | 清洁度 | supporting | debug/TODO/注释代码/无用 import/方案外文件 | static | grep + gofmt + golangci-lint（make check 内） | 无 | pass |

## 3. Command Results

- `make check` → exit 0：frontend build（仅既存 >500kB chunk warning）、`golangci-lint run ./...` = 0 issues、`oxlint` 通过、`go test -p=1 ./... -count=1 -parallel=1` 全 ok、codegen `git diff --exit-code` = 0。
- `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` → exit 0：REV-007 OpenAPI 描述改动不产生生成物 drift。
- `go test ./internal/reminder/... -count=1 -parallel=1` → 全 ok（digest ~33s；含 REV-001/004/006 sender、REV-003 daily、REV-005 binding 回归）。
- `go test ./internal/reminder/digest/telegram/... -run 'TestClientPreservesParentCancellation|TestClientClassifiesTransport'` → ok（生产 client 取消/超时分类）。
- `git diff --check` → exit 0。
- 独立 re-review subagent 另行重跑 `go build ./...`、`go vet`、digest + cmd/server 测试 → 全绿（round 2 交叉验证）。
- 未运行：真 Bot getUpdates/sendMessage 真机链路（S19/S2 真机图）→ 需 owner-managed Bot secret 与 owner-stop，QA 环境无凭证，不阻塞代码但阻塞真机证据（见 §5）。

## 4. Scenario Results

- [x] QA-001~005 DoD 门禁：pass（见 §3）
- [x] QA-006 send-started cancel（REV-001）：pass — 真 `telegram.Client` × `DeliverySender`，attempts 0、finalize/release 0 次、`processed=true`；生产 client 保留 `context.Canceled`
- [x] QA-007 stale finalize（REV-006）：pass — `FinalizeStale`+nil 触发按 claim 重读+幂等重试，Telegram 仅 1 call，status→sent
- [x] QA-008 daily 脱敏日志（REV-003）：pass — 日志含 `account_id`/`target_local_date`/`error`，不含 chat/token
- [x] QA-009 `/start@bot` 规范化（REV-005）：pass — parseCommand 剥离 @bot；真 PG 绑定 `/start@studio_digest_bot <token>` → BindApplied
- [x] QA-010 gate 顺序 + call-boundary fence（REV-004）：pass — Build 移出 gate，D7 顺序 reload→recipient→fence→send→CAS；过期边界不发不烧
- [x] QA-011 绑定安全矩阵（S1/S3/S4）：pass — opaque 单次、过期/篡改/重放/群聊拒绝、跨账号 chat 冲突、并发只留一枚 current
- [x] QA-012 resolver 0/1/>1/DBError（S18/S28）：pass
- [x] QA-013 摘要口径（S6/S7/S8/S11/S22）：pass — overflow/空态/DST 冻结/窄 unpaid/3500 截断
- [x] QA-014 daily（S9/S10/S21）：pass — scan-before-send、local-date once、重启补发、scan 失败不发
- [x] QA-015 `/today`（S5/S14/S30）：pass — kind 冻结、replay 不重复/不换 kind
- [x] QA-016 失败重试与预算（S12/S13/S25）：pass — 1/5/30→failed@4、retry_after、invalid_auth 不烧、operation×kind 总函数
- [x] QA-017 并发（S23/S24/S26）：pass — reclaim barrier 旧 claimant 0 call、stale result no-op、gate rebind 优先、cancel-aware、pre-send 独立 2s cleanup
- [x] QA-018 生命周期（S20）：pass — long poll cancel、runner 全循环取消退出
- [x] QA-019 配置可选/降级（S15）：pass — 空/非法配置不启动 Telegram 循环、Web/Reminder 正常；invalid_auth 全局 suspended 且可恢复
- [x] QA-020 脱敏（S29）：pass — TelegramError/slog 不泄漏 token/正文
- [x] QA-021 migration（MOUNT-2）：pass — up/down、chat 唯一、账号约束
- [x] QA-022 bind-token HTTP（S1）：pass — 200{token,deep_link}/401
- [x] QA-023 设置卡片（S16/S17）：pass — 4/4 契约测试 + 375px 真机截图；DOM/storage 无 token、role=alert、键盘 focus
- [ ] QA-024 真 Bot synthetic 三图（S2/S19）：**residual（owner-gated）** — owner 已口头确认真机测试通过；三张脱敏截图（binding/ack/daily/today）未落盘到 `evidence/`。实现与 review 均明确此项不得用 fake 冒充，须 owner-managed secret + owner-stop 采集。
- [x] QA-025 清洁度：pass

## 5. Findings

### failed

none

### blocked

none

### residual-risk

- **真 Bot S19/S2 三图未落盘（QA-024）**：owner 已确认真机 binding/`/today`/daily 测试通过，但脱敏截图未存入 `.codestable/features/2026-07-15-telegram-digest/evidence/`。代码路径由 production adapter + PostgreSQL 状态机 + 生产 client 取消集成测试全覆盖，真机仅验证 true-external 现场（BotFather token、getUpdates 限流/webhook 现场）。按 design/review 约定不得用 fake 冒充；作为 owner-gated 验收项交由 acceptance 记录 owner 决策。不阻塞代码正确性。
- **at-least-once 重复投递极窄窗口**：design 已接受；REV-006 收紧 finalize 后窗口进一步缩小。
- **Bind link 10 分钟 possession capability**：design A9 已接受。
- **长停机 >24h 丢未确认 updates**：design 已披露（不持久化 global offset）。
- **`replicas>1` 无 leader election**：design A4；靠 compose replicas=1 + stop-first 部署约束，非代码保证。
- **REV-002 入站失败无安全提示**：owner 于 review-fix round 1 决定延后（单一 caller vs 无账号回执的设计矛盾，需单独设计决策）；失败 update 仍 terminal-consumed，不泄漏、不影响队列。
- **Vite chunk >500kB**：既有构建 warning，build exit 0，与本 feature 无关。
- 非核心非功能：`install-cpamp.sh` 根级未跟踪脚本与本 feature 无关，未纳入结论。

## 6. Cleanliness

- Debug output: pass（无 `fmt.Print`/`console.log`；仅结构化 slog）
- Temporary TODO/FIXME/XXX: pass
- Commented-out code: pass
- Unused imports / dead code from this feature: pass（gofmt + golangci-lint 0 issues；已修 review-fix 中两处 gofmt 与一处未用 import）
- Out-of-scope files: pass（`install-cpamp.sh` 非本 feature，已标注归因，未纳入交付）

## 7. Verdict

- Status: **passed**
- Next: `cs-feat` acceptance 阶段。功能性核心路径（绑定/摘要/投递/重试/并发/隔离/降级/生命周期/前端）均有运行证据；唯一 owner-gated 项 QA-024（真 Bot 三图）作为 residual 交由 acceptance 记录 owner 决策，不得用 fake 冒充。
