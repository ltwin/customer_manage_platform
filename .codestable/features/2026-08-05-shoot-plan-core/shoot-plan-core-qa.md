---
doc_type: feature-qa
feature: 2026-08-05-shoot-plan-core
status: passed
runner_state: completed
runner_reason: "独立 QA runner 的验证矩阵已由主 Goal driver 核验并合并；0 blocking"
runner_id: "/root/creative_shoot_planning_goal_driver/shoot_plan_core_qa"
tested: 2026-08-06
round: 1
---

# shoot-plan-core QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-design.md`
- Checklist: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-checklist.yaml`
- Review: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-review.md`（Round 2 passed，0 blocking/important）
- Evidence pack: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-evidence-pack.md`
- Gate results: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-gate-results.json`
- DoD results: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-dod-results.json`
- Implementation evidence: `.codestable/features/2026-08-05-shoot-plan-core/shoot-plan-core-implementation.md`
- Diff basis: Goal baseline `64e1105c0e08b474851431fa80f2ec3d595a85a5` 到当前批准的 unstaged/untracked Epic worktree；scope gate allowlist passed；staged none。
- Baseline dirty files: 整个 Creative Shoot Planning Epic 的批准基线；QA 只归因 core implementation 和本 feature evidence。
- Feature type: functional / high-risk。
- Core evidence gate: 账号隔离、幂等/CAS、结构与执行双 revision、append-only replay、迁移/capability/fence、HTTP/API、工作台和独立 Run Mode 均需实际 PostgreSQL/API/frontend/browser 证据，不能用 typecheck 单独替代。
- Independent runner: `/root/creative_shoot_planning_goal_driver/shoot_plan_core_qa` 以只读方式完成矩阵；主线程复跑 canonical commands、核验源码/测试并目检浏览器证据后合并。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | A1 | core-functional | 独立创建、分页详情、账号隔离、无客户端 account scope | PG/API | CMD-002 + HTTP vertical slice | 账号 A 成功，B 同 ID 404，DTO 无 `account_id` | pass |
| QA-002 | A2 | core-functional | resource-aware 幂等、CAS、三个 executor、旧 wrapper | PG/integration/API | CMD-002 | exact replay；异 body/跨 resource 409；不同 operation 不串响应 | pass |
| QA-003 | A3–A4 | core-functional | readiness gate、ready 回 draft、in-progress 新 Shot、completed write gate | domain/PG/API | CMD-002 | 状态与 409 规则确定 | pass |
| QA-004 | A5–A6 | core-functional | Run Mode admission、session close、live/backfill/unknown、拒绝客户端 capture mode | fixed-clock/PG/API | CMD-002 | 仅 ready/in-progress 打开；mode 服务端派生 | pass |
| QA-005 | A7–A8 | core-functional | result/cleared/recapture、void replay、同 revision 并发 | unit/PG concurrency | CMD-002 | seq 单调，projection 按 active max seq，只有一个并发 winner | pass |
| QA-006 | A9–A11 | core-functional | 软移出保留历史、completion snapshot、reopen/re-complete | PG/integration | CMD-002 | current 移出但 facts/snapshots 保留；finalization revision 单调 | pass |
| QA-007 | A12 | core-functional | 字段/union/archive acknowledgement、capability marker、promotion/TTL | schema/PG/API/CLI | CMD-001/002 | closed union、typed 409、相邻 CAS、错误 wiring/inventory 0 写 | pass |
| QA-008 | A13 | core-functional | 台账/工作台 empty/error/stale 与 1600/1280/375 | frontend/browser | CMD-003/004 + 3 张 S6 截图目检 | 仅四个 core 分区，无后置假入口 | pass |
| QA-009 | A14 | core-functional | 375/coarse/200%/高对比/offline Run Mode | frontend/browser | CMD-003/004 + S7 截图/实现浏览器记录 | 44px 目标、execution-only、无 2xx 不显示保存、同 key 可重试 | pass |
| QA-010 | A15 | supporting | stable pagination/query bound | PG/query structure | CMD-002 | List 固定 Count+QueryPage，无按 item N+1 | pass |
| QA-011 | A16 | core-functional | request/route/dependency/DOM 负向范围 | schema/API/lint/diff | CMD-001～004 | 禁 account/capture mode/匿名 route/AI/media/business surface | pass |
| QA-012 | A17 | core-functional | readiness removal guard 与 archive participant rollback/wiring | PG/integration | CMD-002 | lock 后 guard；active 409；错误 whole-tx rollback | pass |
| QA-013 | A18 | core-functional | neutral fence、generation/FK、rollback/watermark | PG two-connection/compile | CMD-002 | token fail closed、无 gap/orphan、watermark 连续推进 | pass |
| QA-014 | review focus | core-functional | 非末尾移出后尾插、duplicate reorder | PG integration | targeted repository fixture | 顺序保持，非法请求不改 revision/position | pass |
| QA-015 | review focus | core-functional | create exact 201 replay、typed archive 409、fingerprint | HTTP/PG | targeted HTTP/PG fixtures | 首次 body/status 精确重放；authoritative details；完整 domain frame | pass |
| QA-016 | review focus | supporting | generate failure propagation | negative CLI | `make -s generate-check MAKE=false` | 非零向顶层传播 | pass（预期 rc=2） |
| QA-017 | review residual | supporting | READ COMMITTED 多语句 detail 与执行写交错 | PG concurrency + design reasoning | capture-vs-complete fixture/静态核验 | 无数据损坏；短暂展示风险显式记录 | pass with residual |
| QA-018 | cleanliness | non-functional | debug/TODO/dead code/out-of-scope/生成漂移 | lint/diff/scope | CMD-001/004/005 + scans | 无施工痕迹，scope 可归因 | pass |

## 3. Command Results

- `make generate-check` → exit 0：Go/TS 生成物零漂移。
- `cd backend && go test -p=1 ./internal/shootplanning/... ./internal/platform/idempotency ./internal/platform/planningcapability ./internal/platform/store/... ./internal/order ./internal/schedule ./internal/platform/httpapi ./cmd/server ./cmd/planningctl -count=1 -parallel=1` → exit 0：全部目标包通过。
- `cd frontend && npm run test:shoot-planning` → exit 0：11/11，通过 retained-history、generated types、archive projection、幂等 retry、offline success-only、execution-only DOM、coarse/high-contrast/responsive contracts。
- `cd frontend && npm run build && npm run lint` → exit 0：`tsc -b`、Vite、oxlint 通过；仅有 611.98 kB chunk 非阻塞提示。
- `make check` 首轮：主线程一次在 `internal/platform/store`、runner 一次在其他 Testcontainers 包遇到 `port "5432/tcp" not found`；均符合 `.codestable/attention.md` 记录的 Docker Desktop 映射瞬态。
- `cd backend && go test -p=1 ./internal/platform/store -count=1 -parallel=1` → exit 0：主线程失败包受控复跑通过；runner 也将其失败包受控复跑至 GREEN。
- `make check` 受控完整复跑 → exit 0：全仓 Go、前端专项、148-case ops contract、build/lint/codegen 全绿。最终 CMD-005 状态为 passed，不再构成环境 blocker。
- `make -s generate-check MAKE=false` → 预期 rc=2；外层断言非零后 exit 0，证明生成失败正确传播。
- `git diff --check`、`git diff --cached --check` → exit 0；staged none。
- 浏览器证据目检：`evidence/s6-ledger-empty-1600.png`、`s6-workspace-readiness-1280.png`、`s6-workspace-readiness-375.png`、`s7-run-mode-375.png` 均与批准 core UI/响应式边界一致。

## 4. Scenario Results

- [x] QA-001～QA-013 A1～A18：pass。
  - Evidence: design 的 18 条关键场景已由领域测试、真实 PostgreSQL、双连接并发、HTTP vertical slice、CLI、前端专项和浏览器证据组合覆盖；完整索引见 implementation 报告“A1～A18 证据索引”。
- [x] QA-014 Reorder：pass。
  - Evidence: 真实 PG 覆盖 `[A,B]→[B,A]`、移出首位、再无 anchor 新增，最终 `[A,C]` 且 position 单调；duplicate 返回 validation，revision/顺序不变。
  - Notes: missing/extra/cross-plan 复用同一无分支的双侧唯一精确集合 validator，均在 position 写入前拒绝；三者尚无各自独立 PG case，记测试锁定强度 residual。
- [x] QA-015 Exact replay / archive / fingerprint：pass。
  - Evidence: create 后新增 Shot，再用原 key/body 重放仍为 201 且 body 逐字节等于首次；stale archive effects 返回 authoritative typed 409；run-session fingerprint 与 `account_id NUL operation NUL key` 的完整 SHA-256 exact-byte 相等。
- [x] QA-016 Generate failure：pass。
  - Evidence: 正常生成 exit 0，注入 `MAKE=false` 时 rc=2。
- [x] QA-017 并发详情：pass for data integrity / residual for transient presentation。
  - Evidence: capture/complete 双连接 fixture 没有 snapshot gap；revision CAS 防止混合投影形成错误写入；前端 409 刷新恢复通过。
- [x] QA-018 清洁度：pass。

## 5. Findings

### failed

none。

### blocked

none。Testcontainers 映射瞬态已由失败包和完整 canonical command 双重重跑闭合。

### residual-risk

- RR-001：capture/void callback 在 ledger 内保存 `response_status=200`，公开 HTTP 首次和重放固定为 201。当前用户可见 status/body 一致，通用 executor replay 也证明 callback=0；但内部元数据不忠实，未来 adapter 若消费 stored status 会形成差异。延续 review NIT-001，作为后续窄修复候选；本轮不改生产代码、不触发额外完整复审。
- RR-002：reorder missing/extra/cross-plan 没有各自独立 PG case；共同精确集合 validator 已由 duplicate 的真实 PG 写前拒绝路径覆盖，属于回归锁定强度，不是已观察的功能失败。
- RR-003：`GetPlan` 默认 READ COMMITTED 多语句详情没有高频 capture/void 专门压力 fixture；可能短暂混合显示，CAS 与刷新恢复可防数据损坏和陈旧覆盖。
- RR-004：core 0012 在已有 planning idempotency ledger 时 down 会 fail closed；fresh/empty roundtrip 和 planshare populated-down writer exclusion 已验证，但 core populated-down 的正式 rollback runbook 尚未固定。
- RR-005：多 Shot Run Mode 有数组/导航/每 Shot revision/DOM 与后端组合证据，但现有浏览器截图没有单独记录“两 Shot 前后导航 + 各自 pending-key 重试”的完整序列。
- RR-006：三处 UI `as PlanCommand/PlanTransition` 为长期类型卫生风险；当前受控输入未观察到非法请求，原 REV-004 冲突 discriminator 已关闭。
- Provider/bundle：archguard、meta-cc unavailable 按 Goal policy 非阻塞；Vite chunk warning 属独立性能治理。scope 的 4 个 TODO/FIXME warning 均来自后续 checklist 中“禁止该标记”的验收文本，不是源码临时项。

## 6. Cleanliness

- Debug output: pass。
- Temporary TODO/FIXME/XXX: pass；core 源码无命中。
- Commented-out code: pass。
- Unused imports / dead code from this feature: pass；golangci-lint 0 issues、oxlint/build passed。
- Duplicate OpenAPI DTO: pass；planning frontend 引用 generated schema，canonical codegen passed。
- Placeholder handler: pass；真实 runtime vertical slice，无固定空 2xx/501/panic。
- Out-of-scope files: pass；scope gate allowlist 已通过，Epic dirty baseline 有明确归因。

## 7. Verdict

- Status: passed
- Summary: A1～A18 为 18 pass / 0 fail / 0 blocked；CMD-001～CMD-005 最终为 5 pass / 0 fail / 0 blocked；cleanliness passed；0 blocking finding。
- Next: 运行 `qa-evidence-gate`，通过后进入 `cs-feat` acceptance 阶段；acceptance 显式携带 RR-001～RR-006，不把它们伪装为已完成动作。
