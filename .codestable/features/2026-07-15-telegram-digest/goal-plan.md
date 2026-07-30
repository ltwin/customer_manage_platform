# telegram-digest goal plan

## 路径

- feature：`.codestable/features/2026-07-15-telegram-digest/`
- design：`telegram-digest-design.md`（status: approved）
- checklist：`telegram-digest-checklist.yaml`（8 steps / 56 checks / S1～S30 / N1～N7）
- design-review：`telegram-digest-design-review.md`（status: passed, round 8）

## 用户确认依据

- 2026-07-15 owner 在会话中回复“接受”，接受 design 整体、A1～A10 与已披露的 current-binding 推荐语义；后续独立审查把该语义工程化为 A11、recipient gate、Delivery claim lease/call-boundary/cancellation 状态机，未改变用户目标或功能范围。
- Round 8 独立 design-review passed；0 blocking、0 important。
- 开发位置：owner 已明确选择在当前工作区开发；已从 `develop` 创建并切换到 `codex/feat/telegram-digest`，不创建额外 worktree。

## 基线

- baseline ref：`8420d4f1445eeaec2171445d63aa01a0bf123f6f`
- 当前工作区位于 `codex/feat/telegram-digest`；已有本 feature 的 CodeStable 文档改动，尚无业务代码改动、暂存、commit 或 push。
- 已知环境风险：Docker Desktop 高并行 Testcontainers 偶发 `port "5432/tcp" not found`；先用串行 `go test ... -parallel=1` 归因。

## 必跑验证命令

机读权威源为 checklist `dod.commands`：

| ID | 命令 | 失败处理 |
|---|---|---|
| CMD-001 | `make check` | fix-or-block |
| CMD-002 | `make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts` | fix-or-block |
| CMD-003 | `cd backend && go test ./internal/reminder/... ./internal/settings/... ./internal/platform/config/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1` | fix-or-block |
| CMD-004 | `cd frontend && npm run test:telegram-digest` | fix-or-block |
| CMD-005 | `git diff --check` | fix-or-block |

## Implementation TDD policy

- STEP-001：migration/chat唯一/token+delivery约束必须 RED→GREEN→VERIFY；OpenAPI tag/codegen 与配置文档可写 `TDD exception`，替代证据为生成物 diff、config tests 与 migration up/down evidence。
- STEP-002：DeliveryRepository claim/lease/finalize/release、RecipientGate/Resolver、Telegram fake 必须先写失败测试；覆盖双 claim、stale writer、superseded、typed outcome。
- STEP-003：opaque token、scoped enumeration、ConsumeAndBind/rebind/ack supersede/Settings PATCH 并发必须 RED→GREEN→VERIFY。
- STEP-004：Snapshot/Renderer 是纯计算与跨域读取，所有口径、排序、DST、截断先 RED。
- STEP-005：getUpdates offset、rollback/commit-unknown、operation×kind 全函数与取消必须 table-driven RED。
- STEP-006：DeliverySender、call-boundary fence、取消恢复表、fake clock/barrier、daily once/retry 必须 RED→GREEN→VERIFY。
- STEP-007：API wrapper、Settings 状态与内存生命周期先补前端失败测试；视觉/键盘/375px 可写 `TDD exception`，替代证据为浏览器截图和可访问性观察。
- STEP-008：config/lifecycle/disabled/no-overlap 以测试和 runbook evidence 为主；真 Bot 与运维文档允许 `TDD exception`，替代证据为 synthetic runbook、脱敏截图、config/compose diff。
- 任一行为 step 缺 RED/GREEN/VERIFY 且无明确 `TDD exception`，implementation gate 不通过。

## 核心验收路径

1. 已认证设置页签发 43-char、10分钟、单次、hash-only opaque Bind token；重签使旧 token 失效。
2. private `/start` scoped 匹配并原子绑定；群聊、过期、篡改、重放、跨账号 chat 冲突均拒绝且不泄漏。
3. `/today` 固定 ChatAccountResolver→EnsureScan→message_kind→ClaimCommandIfCurrentChat；rollback/commit-unknown 不推进 offset。
4. 摘要只含今日及逾期 pending Reminder、今日 shoot 档期、delivered 未结清计数；20条+overflow、≤3500 code point。
5. daily 按账号时区/digest_hour、scan-before-send、同 local date once、重启补发；空数据仍发存活摘要。
6. TelegramError 八值 operation×kind 全函数；总 failure budget=4；invalid_auth/recipient/cancel 不烧 budget。
7. S23：claim lease/call-boundary/stale result、recipient gate/rebind、ack supersede、2秒 cleanup、5分钟 recipient backoff、账号公平全部 barrier/fake-clock/PG 通过。
8. 设置页未绑定/已绑定/rebind/loading/error/popup blocked/375px/键盘/focus 通过；Bind token 不进 DOM/storage/log。
9. Telegram disabled/suspended 时 Web/Reminder/dashboard 正常；SIGTERM 10秒内退出；部署 `replicas=1 + Recreate/no-overlap`、无 webhook。
10. synthetic fixture 真 Bot binding/daily/`/today` 三类脱敏截图齐全，不含 token/chat_id/真实客户信息。

## DoD / gate policy

- design approved + design-review passed 是 implementation 前置；checklist 8 steps 完成并留下 step evidence。
- implementation gates 全绿后才进入独立 `cs-code-review`；review 必须分别给 spec 合规与代码质量结论。
- review passed 后进入 QA；QA failed/blocked 修复后必须重跑 review+QA。
- QA passed 后进入 acceptance；回写 checklist、requirement draft→current、roadmap item in-progress→done、related_requirements 与长期稳定约束。
- 清洁度：禁止调试输出、临时 TODO/FIXME、注释掉代码、无用 import、凭证/PII 日志、真实客户验收材料。
- Git：不得自动 commit；commit 必须 owner 明确同意；禁 `--no-verify`；不得 push，除非 owner 另行授权。

## Handoff 条件

- 需要改变 approved design、feature 范围、公开契约、A1～A11 或 roadmap item。
- 独立 reviewer pending/failed/blocked 且无 owner 降级授权。
- 同一失败项三轮修复仍不通过。
- 缺 Telegram 凭证、Docker/PostgreSQL 或真机环境，导致核心行为无法判断。
- owner 主动暂停、改方向或终止。
