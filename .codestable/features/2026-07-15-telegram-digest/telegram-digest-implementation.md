---
doc_type: feature-implementation
feature: 2026-07-15-telegram-digest
status: completed
---

# Telegram 每日经营摘要实现证据

## 基线与实现深度

- 基线：`8420d4f1445eeaec2171445d63aa01a0bf123f6f`；实现前 settings/config/store 定向测试与原始 codegen 均通过。
- 第一性原则：只改变账号 Telegram 安全绑定、daily/`/today` 摘要与可靠投递；保持 AccountScope、opaque hash-only token、durable offset、单 Sender 和 claim/gate/cancel 契约；不引入通用通知平台、多实例选主、Webhook、客户触达或 exactly-once。
- 深度：数据库/状态机/摘要属于长期正确性关键路径，使用真实 PostgreSQL 与状态机测试；Telegram true-external 边界使用 production adapter + 可控 fake，真机只在最终 synthetic fixture 验收。

## STEP-001

- 退出信号：CMD-002；migration up/down、chat 唯一、token/delivery 账号约束 PostgreSQL 测试通过。
- RED：`TestLoadTelegramConfigurationIsOptional` 因 `TelegramStatus`/配置字段不存在而编译失败；`TestTelegramDigestMigrationUpDownAndConstraints` 因相同 chat 可写入两个账号而失败。
- GREEN：新增 Telegram env-only 可选配置与脱敏状态校验；新增 0011 up/down migration、partial unique chat index、hash-only token 表、Delivery 约束；OpenAPI 加 `telegram-digest` tag 并生成 Go/TS 契约。
- VERIFY：`go test ./internal/platform/store/... ./internal/platform/config/... -count=1 -parallel=1` 通过；原样 `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` 通过（生成物单独暂存、未 commit）；`git diff --check` 通过。
- TDD exception：OpenAPI tag、`.env.example`、compose wiring 是契约/配置文件，替代证据为双端 codegen 与配置测试；migration 行为本身已由 PostgreSQL RED/GREEN 覆盖。
- 影响面：config、OpenAPI/oapi-codegen、0011 migration、env/compose；无调试输出、临时 TODO/FIXME、注释代码、凭证或 PII。

## STEP-002

- 退出信号：fake 跑通 claim lease→gate→call-boundary→Send→finalize；repository result/release、独立 cleanup、recipient/superseded 与 gate 契约通过。
- RED：RecipientGate/TelegramError/DeliveryRepository/production adapter/DeliverySender 测试先分别因对应 public API 不存在而编译失败；首次 repository GREEN 暴露同一 tx 未关闭 rows 导致 `conn busy`，修正为锁行前关闭 cursor。
- GREEN：新增 digest 名词/ports、八值 TelegramError、raw Bot API client 与可控 FakeTelegram；实现 rebind-priority/cancel/account-keyed RecipientGate；实现 128-bit claim、30秒 lease、callable fence、claim-id CAS finalize/release 与 DeliverySender 20/10/2 秒边界。
- VERIFY：`go test ./internal/reminder/digest/... -count=1 -parallel=1` 通过；覆盖 stale/superseded late writer、recipient 5分钟退避不烧 budget、call boundary 过期调用数=0、pre-send cancel 使用独立有界 cleanup context；`git diff --check` 通过。
- 需求迭代：无；fake 只替代 true-external Telegram HTTP 边界，数据库/状态机使用真实 PostgreSQL。
- 影响面：`backend/internal/reminder/digest/` 及 Telegram adapter 子包；无 DB transaction 跨网络、无 Retryable 第二真相源、无敏感错误正文或 URL。

## STEP-003

- 退出信号：token/private start/resolvers/rebind/并发/PATCH/ack supersede/HTTP 契约全部通过且无敏感泄漏。
- RED：BindingService/Repository/Resolvers 测试先因 public API 不存在编译失败；stale Settings 写测试真实复现普通 PATCH 把并发 rebind 的 `chat-new` 覆盖回 `chat-old`；HTTP 测试因生成接口缺 handler 编译失败。
- GREEN：实现 32-byte→43-char base64url token、SHA-256 only store、10分钟/单次/重签替换；AccountScopeEnumerator 逐 scope token/chat resolver；gate 内 5秒 ConsumeAndBind 事务原子消费、列级绑定、旧 ack supersede、新 ack、recipient 唤醒；Settings 改为只 upsert 自有列；注册受保护 bind-token handler。
- VERIFY：正常/过期/篡改/重放/群聊/跨账号 chat 冲突/同账号 rebind 全通过；并发 Issue 只留一个 token、并发 Consume 仅一次 applied；resolver 0/1/>1/DB error 通过；digest/settings/httpapi 定向包通过（修正旧 feature 的过时 404 断言后重跑 httpapi 全绿）。
- 需求迭代：无；possession capability residual risk 保持 design A9，不增加 Telegram 身份二次核验。
- 影响面：binding application/repository/resolvers、AccountScope 列拥有型 Upsert、Settings repository、HTTP route/handler/tests；客户端不参与构造 AccountScope。

## STEP-004

- 退出信号：Reminder 20+overflow、shoot parity、窄 unpaid、空态、DST、冻结 target 与 Unicode 上限通过。
- RED：Window/Snapshot/Renderer public API 缺失导致测试编译失败；DigestMessageBuilder frozen target 测试同样先因构造入口缺失而失败。
- GREEN：新增 `WindowFor(LocalTarget)` 用 IANA 本地午夜 `AddDate` 建半开日界；SnapshotRepository 账号内批量读 pending due、复用 schedule ListItem 装配后只留 shoot、计 delivered 未结清；Renderer 纯函数和 3500 code point 稳定截断；MessageBuilder 按 Delivery 冻结 target 重建 snapshot。
- VERIFY：America/New_York DST spring day=23h；23条 Reminder 只展示稳定前20+overflow、未来排除；hold/busy 排除且客户/套系 parity；unpaid 仅 delivered+false；空数据存活摘要；跨午夜/发送时状态变化仍为 enqueue 日期/时区；digest 全包串行通过。
- 需求迭代：无；不复用 dashboard today+2，不调用 Reminder/Order/Schedule 写接口。
- 影响面：snapshot/window/renderer/message builder 与测试；同 Snapshot 文本确定、UTF-8 完整。

## STEP-005

- 退出信号：fetch/scan/claim/批次失败与重投、operation×kind、terminal poison/cancel 通过；非 durable update 不确认后缀。
- RED：Policy/Poller/UpdateHandler 测试先因 API 缺失编译失败；随后分别最小实现并独立 GREEN。
- GREEN：Poller 30秒 long polling、update_id 排序、逐条 durable terminal 才推进 next offset、失败即停连续后缀；UpdateHandler 固定 ChatAccountResolver→Target→EnsureScan→message_kind→ClaimCommandIfCurrentChat；实现 poll/send×八值总函数与可取消 backoff；扩充 adapter HTTP/JSON/transport 分类矩阵。
- VERIFY：批次 `[3,1,2]` 在 2 commit-unknown 模拟失败时只确认到 offset=2 且不处理 3，重投 2/3 后到4；同 `/today` 先 scan 失败落 temporary_unavailable，恢复重投仍不换 kind，新 update 才 digest；network/timeout/server/rate/webhook/auth/client/protocol 全有策略，adapter token/body 脱敏；digest 全包通过。
- 需求迭代：无；不持久化 Bot global offset，长停机 24h residual risk 保持设计披露。
- 影响面：poller/update handler/policy、command claim、Telegram adapter tests；poll/daily/command 仍只入队，不直发。

## STEP-006

- 退出信号：daily timezone/scan/retry；claim/fence/cancel/lease/stale/gate/recipient 公平通过。
- RED：DailyScheduler tests 先因 SettingsTargetProvider/DailyRepository/Scheduler API 缺失编译失败；实现后真实 PostgreSQL GREEN。retry/cancel micro-cases 在现有 claim seam 上补齐并验证。
- GREEN：本地时区/小时 target provider、daily exists→EnsureScan→唯一 enqueue、立即首 tick+每分钟 runner；Reminder scan adapter、current Settings recipient resolver、每账号最多20且账号失败不饿死后缀的单 DeliverySender runner；完整 failure budget 与取消语义沿用 claim-id CAS。
- VERIFY：到时后先 scan 再 enqueue；重复 tick/重启同 local date 仍1条，scan 失败0条且恢复后补发；network 失败依次+1并在1/5/30分钟后 due、第四次 attempts=4+failed；429 取17分钟；invalid_auth/recipient 不烧；pre-send cancel 独立2秒 cleanup；send-started cancel 保留 claim 等 repair/lease；stale/superseded writer no-op；不同账号公平；digest 全包通过。
- 需求迭代：无；首版仍单 replica、单 sender，不引入 leader election/queue。
- 影响面：daily/settings/reminder adapters、sender runner、retry/cancel 状态机测试。

## STEP-007

- 退出信号：`test:telegram-digest` 单独通过并已接入 `make test`；375px、focus、错误恢复与敏感数据生命周期有可复核证据。
- RED：首次运行 `npm run test:telegram-digest` 因脚本不存在失败；新增脚本后又因 `telegramBinding.ts` 不存在失败。
- GREEN：API client 直接引用 OpenAPI 生成类型；Settings 增加未绑定/已绑定、绑定/重新绑定、loading、`role=alert` error、popup-blocked retry 状态；deep-link 只在组件内存存活，成功/重签/unmount/10分钟到期均清理；`window.open` 固定 `_blank` + `noopener,noreferrer`，DOM 不生成 deep-link `href`。
- VERIFY：`npm run test:telegram-digest` 4/4、`npm run build`、`npm run lint` 通过；构建仅有既存的 >500kB chunk warning。隔离 PostgreSQL `crm_ui_20260715` + 独立 `127.0.0.1:8081` synthetic 账号真实加载 `/settings`：375×812 下 `scrollWidth=clientWidth=375`，卡片宽 351px 且无横向 overflow；卡片/页面 DOM 无 43-char token、无 `start=` href；原生 `button type=button` 键盘聚焦后 `:focus-visible=true`、2px solid outline；真实 500 后出现唯一 `role=alert` “内部错误”且按钮恢复可用。脱敏截图：`evidence/settings-telegram-375-error.jpg`。
- 浏览器工具限制：本轮全局 Tab 注入未移动 `body` focus；locator `press('Enter')` 能形成真实 focus-visible 但未由该自动化边界触发 click。替代证据为原生 button/`onClick`/disabled 契约测试、真实 focus-visible 样式与真实 click→error recovery；未伪造 Enter 激活观察。loading 因本地 500 返回过快未截图，其 `disabled={binding}` 与 loading 文案由自动化契约覆盖。
- TDD exception：React 项目当前无 DOM test runtime，未为单卡片新增测试框架；以 Node 契约测试 + 真实浏览器 DOM/截图作为等价前端证据。
- 影响面：`frontend/src/api/client.ts`、Settings 卡片/helper/CSS、frontend test、Makefile；不使用 local/session storage、console 或 token 文本渲染。

## STEP-008

- 退出信号：单 TelegramRunner/config/lifecycle/disabled/suspended/no-overlap、全量回归与范围清扫完成；true-external synthetic 三图未伪造，明确留给 QA owner-stop。
- RED：`TelegramRunner` 启动/取消测试先因构造器不存在失败；server optional composition 测试先因 `buildTelegramIntegration` 不存在失败；shared IntegrationState 测试先因 state/guard/三个 `WithIntegrationState` 不存在失败；result commit-unknown 测试真实返回 `commit outcome unknown`；claim commit-unknown PG fault seam 先因缺恢复入口失败。
- GREEN：composition root 仅注册一个内部承载 poll/daily/唯一 sender 的 TelegramRunner，并向 HTTP 注入 guarded BindingService；配置空/非法不启动 Telegram 循环。poll/send invalid_auth 共享五分钟 suspended state，暂停 bind-token/sender 并由 poll 到期探测；runner 全部共用 server context 并等待退出。结果写回 unknown 在独立2秒 DB-only context 内重读并对 current claim 幂等重写，不再次发 Telegram；claim commit unknown 按 claim_id 重读已提交 reservation。新增 S23 精确 barrier：A resolver 后暂停、lease 到期后 B reclaim，A Telegram 调用0次、B仅1次并完成 sent。
- VERIFY：隔离 PostgreSQL+8081 空配置启动输出脱敏 `status=disabled`，Settings/Reminder/Web 正常、bind-token 500、SIGINT 0.006s 退出；`docker compose config --quiet` 通过。`docker-compose.yml` 固定 replicas=1、stop-first 和 12秒容器 grace；`docs/telegram-digest-runbook.md` 覆盖 env-only secret、owner 手工 BotFather、`getWebhookInfo.url` 为空、禁止自动删 Webhook、Recreate/no-overlap、SIGTERM、disabled/suspended、脱敏日志与 synthetic 三条真机路径。
- CMD-001～005 第二轮：`make check`=0；`make generate && git diff --exit-code -- ...`=0；定向 Go CMD-003=0；frontend Telegram 4/4=0；`git diff --check`=0。`make check` 首轮曾因沙箱 Go package load 与 Docker Desktop 高并行 mapped-port 抖动失败；两项真实 lint 已修，Testcontainers 门禁改为 `-p=1 -parallel=1 -count=1` 后连续完整通过。
- 范围/凭证清扫：production `.SendMessage` 唯一 caller 是 `DeliverySender`；无 Bot-token 格式字面量；无 `fmt.Print`/`console.log`/TODO/FIXME；poll/daily/command 仍只入队；未新增 Webhook、客户触达、多 Bot、多会话、通用通知、队列、选主或 exactly-once。
- TDD exception：compose/runbook 与真实 Bot 属配置/外部环境证据。替代证据为 compose parse、隔离 disabled UI/HTTP/SIGTERM、production adapter+FakeTelegram+PostgreSQL 状态机；owner 未提供真实 Bot secret，因此没有 binding/daily/`/today` 三张 Telegram 截图。该缺口不得以 fake 冒充，进入 QA 后若仍无 owner-managed secret，按 goal protocol 写 handoff。
- 影响面：`backend/cmd/server`、digest runner/state/repair/tests、compose、Makefile、runbook 与 feature evidence；无 commit/push。

## Implementation gate 总结

- Checklist steps：STEP-001～008 均完成实现；56 项 checks 留给 review/QA/acceptance 逐项判定。
- 本地 gates：CMD-001～005 全绿；codegen 无漂移；Go/TS lint 全绿；Docker compose 配置可解析；工作区无 whitespace error 或凭证字面量。
- 下一阶段：独立 `cs-code-review`，分别给出 spec compliance 与 code quality verdict。true-external S19 不在本地 implementation evidence 中宣称通过。

## Review-fix round 1（2026-07-17）

针对 `telegram-digest-review.md`（round 1, changes-requested）修复：

- **REV-001（blocking）** `telegram/client.go`：`classifyTransport` 在 network 兜底前先 `errors.Is(err, context.Canceled)` 原样透出，parent cancel 不再被包成 `TelegramErrorNetwork`。新增 `TestClientPreservesParentCancellationInsteadOfWrappingAsNetwork`（生产 client 断言 cancel 不成 TelegramError）与新文件 `sender_cancel_integration_test.go`（`digest_test` 外部包，真 `telegram.Client` × `DeliverySender` 端到端：send-started cancel 时 SendNext 吸收、finalize/release 调用 0 次、attempts 不增）。修掉「只测 Fake 假绿」。
- **REV-003** `daily.go`：`NewDailyScheduler` 注入 `*slog.Logger`（nil→`slog.Default()`）；`RunOnce` 对 enumerate/due/exists/scan/enqueue 失败结构化 `ErrorContext`，字段仅 `account_id`+`target_local_date`+`error`，不含 chat/token/正文。新增 `TestDailySchedulerLogsSanitizedScanFailure`（DB-free stub，断言含账号/日期/错误、不含 `chat`/`token`）。call sites：`main.go`、`daily_test.go` ×3。
- **REV-004** `sender.go`：`messages.Build` 移出 `RecipientGate.WithSend` 临界区，在进入 gate 前按 claim 冻结字段渲染；gate 内顺序回到 design D7（reload → recipient → call-boundary → ≤10s Send → 短 CAS）。build 失败按 pre-send cleanup 释放 claim（cancel/deadline 吸收）。
- **REV-005** `binding.go`/`update_handler.go`：新增 `parseCommand` 剥离可选 `@botusername`；`HandleUpdate` 与 `HandleStart` 都经它规范化，`/start@bot <token>`、`/today@bot` 正确路由。新增 `TestParseCommandStripsOptionalBotMention`、`TestBindingServiceAcceptsStartWithBotMention`。
- **REV-006** `sender.go` `finalize`：仅 `FinalizeApplied`+nil 视为成功；`FinalizeStale`（含 +nil error）按 claim 重读，仅当仍是 current pending owner 且 lease 未过期时幂等重试同一 result CAS，不重发 Telegram。新增 `TestDeliverySenderRetriesStaleFinalizeWhileClaimStillOwnsDelivery`。
- **REV-007** `api/openapi.yaml`：settings tag 描述删除「未实现」；`make generate` 后 `api.gen.go`/`schema.d.ts` 无 drift（CMD-002=0）。

- **REV-002（important，owner 延后）**：owner 决定本轮不实现，写入 residual risk（见 review 文档 §6）。理由：入站失败安全提示（无效 token / 群聊 `/start` / 未绑定 `/today` / 未知命令）在**无匹配账号**的入站路径上要求直接发消息，与 design line 155/400「poll/command 不得直调 Telegram；单一 DeliverySender 是唯一 TelegramSender caller」互相冲突；需一次单独设计决策（best-effort 直发通道 vs 保持单 caller），不在 review-fix 轮内单方面改公开设计约束。当前行为：这些 update 仍 terminal-consumed（durable、推进 offset、不建业务 Delivery），仅缺可观察回执。

- 验证（review-fix 后）：`go build ./...`、`go vet` clean；`go test ./internal/reminder/... -count=1 -parallel=1` 全绿（digest 33s）；settings/config/cmd/server 全绿；`make generate` 后生成物无 drift。httpapi 首轮出现一次与本 feature 无关的 Testcontainers mapped-port 启动 flake（`customers_profile_test.go`），重跑通过。
- 下一步：按 review verdict 重跑独立 `cs-code-review`（不得直接进 QA）。
