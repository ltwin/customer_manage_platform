---
doc_type: feature-acceptance
feature: 2026-07-15-telegram-digest
status: passed
accepted: 2026-07-17
round: 1
---

# Telegram 每日经营摘要 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-07-17
> 关联方案 doc：`.codestable/features/2026-07-15-telegram-digest/telegram-digest-design.md`
> 状态说明：9 节核对 + final audit 完成；两项 owner 决策已落定——(1) S19 真机以 owner-attested residual 记录（owner 2026-07-17 确认真机通过，不伪造截图）；(2) owner 授权 requirement draft→current 与 roadmap in-progress→done 回写已执行。verdict = passed。git commit 仍待 owner 明确指示。

## 1. 接口契约核对

对照 design 第 2.1 节名词层与 2.2 流程图：

**接口示例逐项核对**：
- [x] `POST /api/v1/settings/telegram/bind-token`（`backend/internal/platform/httpapi/telegram_binding.go:10` `CreateTelegramBindToken`）：认证账号 → 200 `{token, deep_link}`，未认证 → 401 → 与 `TestCreateTelegramBindTokenRequiresAuthAndReturnsOpaqueDeepLink` 一致
- [x] `BindingService.IssueBindToken`（`binding.go:200`）：32-byte→43-char base64url opaque token、10 分钟、hash-only 落库 → 与 `TestBindingServiceIssuesOpaqueSingleUseTokenAndReplacesPrevious` 一致
- [x] `BindingService.HandleStart`（`binding.go:216`）：private-only、单次消费、supersede 旧 ack、chat 唯一 → 一致
- [x] `TelegramSender`/`TelegramUpdateSource` + typed `TelegramError`（`telegram.go`）：production `telegram.Client` 与 `FakeTelegram` 共用最小接口，Kind 穷尽八值 → 与 `TestTelegramErrorKindsAreExhaustive`/`TestTelegramPolicyIsTotalForOperationAndKind` 一致

**名词层"现状 → 变化"逐项核对**：
- [x] Delivery 状态机（claim/lease/finalize/release）：`model.go`+`repository.go`+`sender.go` 落地，CAS 与 attempts 预算与 design D7 一致
- [x] RecipientGate（cancellation-aware、rebind-priority）：`gate.go` 落地，review REV-004 后 gate 内顺序回到 D7
- [x] DigestSnapshot/Renderer（冻结 target date/timezone、≤3500 code point）：`snapshot.go`/`renderer.go`/`message_builder.go` 落地

**流程图核对**（2.2 mermaid）：
- [x] ISSUE→LINK→POLL→START/AUTH→BIND/SCAN→OUTCOME→CLAIM→SNAP→RENDER→ACLAIM→RGATE→SEND→FINALIZE 各节点在代码均有实际落点（httpapi/binding/poller/update_handler/daily/snapshot/renderer/sender/gate）

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] 安全绑定、同口径摘要、每日调度、可重试投递 → 全链路代码 + 测试落地（见 QA §2 矩阵）

**明确不做逐项核对（design §3.2 反向核对项）**：
- [x] N1 无向 Customer/SocialIdentity 发送路径（grep 无客户触达）
- [x] N2 不注册 webhook、无多 channel/多 chat schema（getUpdates-only；无 webhook 注册）
- [x] N3 摘要不写 Reminder done/dismiss、Order PATCH、Schedule 写接口（snapshot 只读）
- [x] N4 snapshot 无 `today+2`；未来 Reminder 不进摘要（�narrow window，`TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel`）
- [x] N5 Bot token env-only、不进前端；Bind token 不入 DB 明文/storage/DOM/日志/bundle（config + 前端契约测试 + STEP-007 DOM 核查）
- [x] N6 生产绑定不读"最新 chat"；`telegram-smoke.sh` 未被 server import（grep 无命中）
- [x] N7 无多实例选主/消息队列/事件总线/通用通知模块

**关键决策落地**：
- [x] 单一 DeliverySender 唯一 TelegramSender caller（review 清扫确认）
- [x] AccountScope 隔离（ADR-001）：token/chat/snapshot/delivery 全经 scope，客户端不传 account_id
- [x] Handler 薄层（ADR-003）：`telegram_binding.go` 只做 JSON/scope 适配

**挂载点反向核对（可卸载性，design §2.3）**：
- [x] MOUNT-1 HTTP/OpenAPI：`router.go:130` + `telegram_binding.go` + OpenAPI `telegram-digest` tag
- [x] MOUNT-2 schema：`migrations/0011_telegram_digest.{up,down}.sql` + chat 唯一约束 + 列级 settings mutation（`TestTelegramDigestMigrationUpDownAndConstraints`）
- [x] MOUNT-3 config：`TELEGRAM_BOT_TOKEN`/`TELEGRAM_BOT_USERNAME` + `.env.example` + `docker-compose.yml`
- [x] MOUNT-4 runner：`main.go:166 buildTelegramIntegration` + `NewTelegramRunner`
- [x] MOUNT-5 UI：`SettingsPage.tsx` Telegram 卡片 + `telegramBinding.ts` + `client.ts createTelegramBindToken`
- [x] 反向 grep：本 feature 代码引用均落在清单内，无清单外挂入点
- [x] 拔除沙盘：卸载 MOUNT-1~5 + digest 包 + migration 后无残留（poll/daily/sender 均在 runner 内，config-gated 不启动）

## 3. 验收场景核对

对照 design §3.1（S1–S30）与 §3.3 覆盖矩阵，逐条运行证据见 `telegram-digest-qa.md` §2 矩阵（QA passed）：

- [x] S1–S4 安全绑定：pass（QA-011/QA-022）
- [x] S5/S14/S30 `/today` 与去重/kind 冻结：pass（QA-015）
- [x] S6–S8/S11/S22 摘要口径与渲染：pass（QA-013）
- [x] S9/S10/S21 daily 调度：pass（QA-014）
- [x] S12/S13/S25 失败重试与 operation×kind：pass（QA-016）
- [x] S18/S28 账号隔离与 resolver：pass（QA-012）
- [x] S20 生命周期有界退出：pass（QA-018）
- [x] S23/S24/S26 并发/claim/fence/cancel/重试日期：pass（QA-017 + REV-001/004/006 回归）
- [x] S27 token 契约/泄漏 residual：pass（opaque 契约测试 + design A9 residual 已披露）
- [x] S29 adapter/error 脱敏：pass（QA-020）
- [x] S16/S17 前端卡片：pass（QA-023，4/4 契约 + 375px 截图）
- [x] **S2/S19 真 Bot synthetic 三图**：**owner-attested residual**（owner 2026-07-17 确认真机 binding/daily/`/today` 通过）——三张脱敏截图未落盘 `evidence/`，按 owner 决策记为口头确认，不伪造。代码路径由 production adapter + PostgreSQL 状态机 + 生产 client 取消集成测试全覆盖；真机仅验证 true-external 现场。

**review 报告重点复核**：
- [x] `telegram-digest-review.md` §4 Test And QA Focus 逐条覆盖（QA §2）
- [x] §5/§6 residual risk 逐条处理（见本报告 §9 与 QA §5）；round 2 review = passed，无 unresolved blocking

**QA 报告重点复核**：
- [x] 证据来源 = `telegram-digest-qa.md`（status passed）
- [x] 覆盖 design 关键场景与 review QA focus
- [x] feature 性质（functional）与核心证据说明合理
- [x] failed/blocked = none
- [x] residual-risk 逐条处理，未承载核心验收缺口（唯一 owner-gated 项 S19 明确标注）
- [x] Evidence pack / DoD Results / Gate Results 已复核；CMD-001~005 均有 pass evidence

## 4. 术语一致性

对照 design §0 + §2.1 命名 grep：

- Telegram 摘要 / 绑定令牌（Bind Token）/ 投递（Delivery）/ Telegram 会话：代码命名一致（digest 包、BindToken、Delivery、chat）
- 防冲突：禁用词「用户」grep 本 feature 新增代码无命中（统一用 账号/客户/chat）
- Bind token ≠ 登录 Bearer ≠ Bot token：三者在 config/binding/auth 各自独立，无混用

## 5. 领域影响盘点（提示而非代写）

对照 design §4：

- [x] 新名词候选（Telegram 摘要 / 绑定令牌 / 投递任务）：design §4 标注 acceptance 后可补入 `CONTEXT.md`。→ **建议走 `cs-domain`**（不在 accept 代写）。owner 可决定是否本轮执行。
- [x] 结构性选择：reminder 域 TelegramPort / 单体 long polling / production+fake adapter 已由 roadmap §4.5 + ADR-003 钉死，design §4 明确**不新增 ADR**。→ 不需要。
- [x] 流程级约束（Bot token env-only、scan-before-send、durable→offset ack、at-least-once 残余窗口、单 caller）：design §4 标注可提炼回 roadmap/compound。→ **建议退出后 `cs-keep`**（owner 决定）。

本节为登记表，不在 accept 直接改 CONTEXT.md / 写 ADR。

## 6. requirement delta / clarification 回写

- design frontmatter `requirement: telegram-digest`，该 req 原 `status: draft`。
- 本次实现**忠实落地 draft req 的用户故事与边界，无 capability-boundary 扩张**（逐条比对 §用户故事/§边界：单账号单会话、今日及逾期窗口、只读不改状态、降级不影响主应用、需部署方凭证——全部符合）。
- Governance（L3）：draft→current 是长期 requirement 写动作。**owner 于 2026-07-17 终审授权**。✅ 已执行：`.codestable/requirements/telegram-digest.md` `status: draft→current`、`implemented_by: [2026-07-15-telegram-digest]`、`last_reviewed: 2026-07-17`、追加变更日志（保留原始愿景），与 `reminder-engine`/`dashboard` 同型收尾一致。

## 7. roadmap 回写

- design frontmatter `roadmap: photographer-private-crm` / `roadmap_item: telegram-digest`，两字段都有值。
- **owner 于 2026-07-17 终审授权**。✅ 已执行：
  - items.yaml `slug: telegram-digest` `status: in-progress→done`（`feature` 核对一致）；`python3 yaml.safe_load` 校验通过、telegram-digest status=done。
  - 主文档同步：`photographer-private-crm-roadmap.md` §4.5 条目 9 状态 `in-progress→done` 并补完成备注。
  - 两份一致。

## 8. attention.md 候选盘点

- [x] 候选：**Telegram digest 测试用串行 Testcontainers（`go test -p=1 -parallel=1 -count=1`），逐包并发会偶发丢失 PostgreSQL mapped port**。此坑本轮 QA 已实测复现（httpapi 首轮 flake，重跑通过），`make check` 已固定串行。→ 但 `Makefile` 注释已写明，且属既有 reminder/customer 测试共性，非本 feature 独有；**建议登记但由 owner 决定是否补入 attention.md**（本节只登记，不擅自写）。
- 其他知识出口：单 caller/gate 顺序/at-least-once 窗口等稳定约束 → §5 已标 `cs-keep` 候选。

## 9. 遗留

- 后续优化点：
  - REV-002 入站失败安全提示（owner 延后，需单独设计决策：单一 caller vs 无账号回执）→ 建议开 issue。
  - review round 2 两条非阻塞 suggestion（REV-006 repair 在 gate 内 ≤2s 上界；release 退避覆写精度）→ 均有 CAS/lease 兜底，规模化前可选优化。
- 已知限制（design 已披露 residual）：at-least-once 极窄重复窗口；Bind link 10 分钟 possession；长停机 >24h 丢未确认 updates；`replicas>1` 无 leader election（靠 compose 约束）。
- 实现阶段"顺手发现"：无（review-fix 仅按 findings 修复，未扩范围）。
- 基线无关：`install-cpamp.sh` 根级未跟踪脚本与本 feature 无关，未纳入交付/结论。

## 10. 最终审计

- 验证证据来源：`telegram-digest-qa.md`（status passed）+ 本轮 accept-inline 复核
- Evidence sources：`telegram-digest-evidence-pack.md` / `telegram-digest-dod-results.json`（CMD-001~005 exit 0）/ `telegram-digest-gate-results.json`（scope-gate passed）
- 聚合命令（本轮最终工作区重跑）：
  - `make check` → exit 0（frontend build + golangci-lint 0 issues + oxlint + `go test -p=1 ./... -count=1 -parallel=1` 全 ok + codegen `git diff --exit-code` = 0）
  - `make generate && git diff --exit-code -- api.gen.go schema.d.ts` → exit 0（无 drift）
  - `go test ./internal/reminder/digest/... -count=1 -parallel=1` → ok（digest 32.3s / telegram 0.96s）
  - `git diff --check` → clean
  - 独立 re-review subagent 另跑 `go build`/`go vet`/digest+cmd-server → 全绿
- 场景复核：re-verified S1、S3–S18、S20–S30 + N1–N7（自动化 + grep，共 ~29 项）；trust-prior-verify 2 项（**S2/S19 真 Bot 三图** — owner 口头确认，无落盘截图；**S17 键盘 Enter 激活** — STEP-007 记录浏览器自动化边界，以原生 button/focus-visible/click 契约等价证据替代）。trust-prior 比例 ~6%，低于 30%；但 S19 属核心 owner-gated 项，单独阻塞（见结论）。
- 交付物复核：代码（digest 包 + httpapi handler + main 装配）/ 配置（env + compose）/ schema（migration 0011）/ 路由（bind-token）/ 文档（runbook）/ 生成物（api.gen.go + schema.d.ts）— 均真实落盘。requirement `draft→current`、roadmap `in-progress→done`（items.yaml + 主文档）**已在 owner 授权后写入并校验**。
- 完整工作区复核：`git status` 含 feature 代码 + docs + 生成物 + feature 目录 + 回写的 requirement/roadmap；`install-cpamp.sh` 基线无关已标注；无临时污染。
- diff 清洁度：通过（review-fix 引入的两处 gofmt 与一处未用 import 已修；`git diff --check` clean；无 debug/TODO/注释代码）。
- 知识沉淀出口：§5 `cs-keep` 候选（单 caller/gate 顺序/at-least-once）、§8 attention 候选（串行 Testcontainers）已登记，退出后按提示分流。
- 结论：**通过**。功能性核心路径（绑定/摘要/投递/重试/并发/隔离/降级/生命周期/前端）均有运行证据；S19 真机按 owner 决策记为 owner-attested residual（不伪造截图）；requirement + roadmap 回写已在 owner 授权后执行并校验。唯一剩余动作为 owner 指示下的 git commit。
