# plan-assignment-reminders · S6 全矩阵 / 生产接线证据

- feature: `2026-08-05-plan-assignment-reminders`
- 日期: 2026-08-14
- 工作树: `.worktrees/creative-shoot-planning`
- 分支: `feat/creative-shoot-planning`
- 基线 commit: `dbdada08`（ITEM-5 已提交；S1–S6 实现未提交）
- stage-1 / stage-2: **仍未标 passed**（本步不改）
- ITEM-6 checkbox: 父代理复核后标 implemented，并做里程碑 commit（本文件随同提交）

## 1. 门禁结果（本机实跑）

| ID | 命令 | exit | 说明 |
|---|---|---|---|
| CMD-001 | `make generate-check` | **0** | OpenAPI → Go/TS 无漂移 |
| CMD-002 | `cd backend && go test -p=1 ./internal/reminder/... ./internal/planshare/... ./internal/shootplanning/... ./internal/order ./internal/schedule ./internal/settings/... ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi ./cmd/server -count=1 -parallel=1` | **0** | 指定矩阵全绿 |
| CMD-003 | `cd frontend && npm run test:plan-assignment-reminders` | **0** | 5 pass |
| CMD-004 | `cd frontend && npm run lint && npm run build` | **0** | oxlint 既有 AccountCenterContext warning；build 通过 |
| CMD-005 | `node --test frontend/scripts/planning-prototype-v2.test.mjs` | **0** | 6 pass（含「真实浏览器截图 out of scope」显式 gap） |
| CMD-006 | `make check` | **0** | 完整门禁；见下方过程 |

### `make check` 过程记录

1. **第一次完整跑**：`gofmt` 失败（`planning_reminder_fence.go`、`freshness.go`）。已 `gofmt -w` 修复。
2. **第二次完整跑**：后端中途 Testcontainers 偶发 `port "5432/tcp" not found`：
   - `TestCreateValidationRollback`（`internal/customer`）
   - `TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel`（`internal/reminder/digest`）
3. **失败包定向重跑**（仍偶发）→ 短停后重跑：`customer` / `digest` **ok**。
4. **第三次完整 `make check`**：**exit 0**（含 auth-legacy tip=`28`、security-catalog、v1-ops、generate-check）。

补充：S6 收口前另跑 CMD-001～005 全部 **exit 0**（与上表一致）。

### 本步修过的非偶发失败

| 失败 | 根因 | 修复 |
|---|---|---|
| `dataexport` BindingRevision 期望 | migration 0028 默认 `telegram_binding_revision=1`，fixture 仍期望旧值 | 读导出 settings 字段并对齐 fixture |
| digest `conn busy` | `loadPlanningDigestItems` 嵌套 Query | 先收集 group 再查 reminder |
| `gofmt` | 未格式化文件 | `gofmt -w` |
| CMD-006 Docker 偶发 | attention.md 已知 mapped port | `-p=1` 定向重跑后整仓再绿 |

## 2. S6 exit_signal 对照

| 信号 | 结果 | 证据 |
|---|---|---|
| A1–A24 自动化矩阵绿 | **通过**（浏览器截图除外） | S1–S5 PG/HTTP/前端测试 + S6 补测；CMD-002/006 |
| CMD-001～006 全绿 | **通过** | 上表真实 exit 0 |
| 无 Noop production wiring | **通过** | `shootPlanningOptionsForCapability` 对 reminder 注入真实 archive/CRM；settings timezone 真实 participant；缺 wiring 启动失败 |
| 无内存队列 fallback | **通过** | `NewPlanningAssignmentRunner` PG lease/SKIP LOCKED tick；`TestS6ProductionRunnerNotMemoryQueue` |
| 无手写 DTO | **通过** | OpenAPI generate + `TestS6OpenAPI…` + 前端 `test:plan-assignment-reminders` |
| 无 AI / 客户消息 / 经营字段漂移 | **通过** | H1/H4/shot_at/price 源码扫描；无「已通知客户」；无第二套 TG bot |

## 3. 生产接线（S6 核心）

| 点 | 行为 |
|---|---|
| `ArchiveCapabilityReminder` | `PlanningShareReminderArchiveImpactPolicyV1` + `planshare.ReadinessRemovalGuard` + `reminder.NewPlanArchiveReminderAdapter()` + 真实 CRM adapter |
| `core` / `planning-share-v1` | archive participant **不**注入真实（保持 Disabled 路径）；始终真实 CRM adapter |
| Settings | `WithPlanningReminderTimezone(true, NewTimezoneChangePlanningParticipant(...))`；失败则启动失败 |
| Telegram digest | `NewDigestIntentService(messages, NewAssignmentReminderFreshness())`；freshness nil fail-closed |
| Background | legacy `ScanRunner` + `PlanningAssignmentRunner` 同 `backgroundRunner` lifecycle |

测试：`TestShootPlanningOptionsReminderWiringMatrix`（reminder composition 成功；Disabled/错 policy/缺 guard 失败；不 Promote 生产 DB）。

## 4. S6 补测清单

| 测试 | 覆盖 |
|---|---|
| `TestS6MonotonicBudgetThroughBeginCurrentCall` | 100ms remainder + 80/120ms pause 穿过 `BeginCurrentCall` / `clock_timestamp()` |
| `TestS6DigestIntentRetentionRedactAndDelete` | terminal intent 7d redact / 90d delete metadata |
| `TestS6NegativeGuardsShotAtPriceCustomerDelivery` | H4 / 禁 `Order.shot_at` 新依赖 / 禁 price / 禁客户投递 |
| `TestS6H1LegacyNoPlanBaselineNoAssignmentDrift` | 无 plan 账号 legacy 基线无 assignment 漂移 |
| `TestS6OpenAPIReminderTypeAdditiveNoHandwrittenDTO` | `plan_assignment_checklist` additive；无手写 DTO |
| `TestS6ProductionRunnerNotMemoryQueue` | 生产 runner 非内存队列 |

迁移 tip 仍为 **0028**（无 0029）；`SchemaVersion` / `fullVersion == 28` 已钉；**未** `migrate force`。

## 5. Residual（不阻塞 S6 落盘；禁止标 passed）

1. **真实浏览器** 1600/1280/375 / 键盘 / 200% zoom **截图**（owner 自验）— `browser_responsive_accessibility_screenshots` 保持 pending/residual；**不**把 A17 浏览器项标 passed。
2. **stage-1 / stage-2** 仍 pending；本步不改、不补空 SHA。
3. ITEM-6 里程碑 commit：父代理复核 `generate-check` + reminder/digest/`cmd/server` + 前端契约测试后提交；**未 push**。
4. 本地历史 dirty PG v16：**未** force。

## 6. Checklist 步骤状态（本步）

| Step | status | 依据 |
|---|---|---|
| S1–S5 | done | 先前落盘 |
| S6 | **done** | 生产接线 + runner + 负向 guard + CMD 全绿 |

## 7. 本步关键改动文件（S6 增量 / 接线）

- `backend/cmd/server/main.go` — capability composition、settings timezone、freshness、assignment runner
- `backend/cmd/server/shoot_planning_test.go` / `main_test.go`
- `backend/internal/reminder/assignment_background_runner.go`
- `backend/internal/reminder/digest_intent_retention.go`
- `backend/internal/reminder/assignment_s6_pg_test.go`
- `backend/internal/platform/store/assignment_quarantine_claim.go`
- `backend/internal/reminder/digest/intent.go`（conn busy 修复）
- `backend/internal/dataexport/repository.go` + test（binding revision）
- 本证据 / checklist / epic note
