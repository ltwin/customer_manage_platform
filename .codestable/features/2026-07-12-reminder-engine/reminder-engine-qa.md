---
doc_type: feature-qa
feature: 2026-07-12-reminder-engine
status: passed
tested: 2026-07-13
round: 2
---

# reminder-engine QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-12-reminder-engine/reminder-engine-design.md`（approved）
- Checklist: steps 全 done；checks 仍 pending（acceptance 勾选）
- Review: `reminder-engine-review.md` round 2 **passed**（reviewer: subagent）
- Evidence pack: `reminder-engine-evidence-pack.md`
- Gate / DoD results JSON: none（轻量 evidence 已写）
- Diff basis: 工作区 unstaged + untracked（`feat/reminder-engine`，相对 baseline `7549150`）
- Baseline dirty files: none（均可归因本 feature）
- Feature type: **functional**（API + 后台扫描 + 前端三面）
- Core evidence gate:
  - 扫描幂等 / 三规则 / auto-dismiss / Settings+/me / reminder API / runner / 404 反向 → 自动化、HTTP 冒烟与本轮全量测试
  - 前端场景 21–23 → 三张 owner 截图 + 本轮真实浏览器操作复核，覆盖加载态、空态、校验错误、保存、扫描、新增与状态变更
- Round 2 trigger: round 1 的 QA-012 缺证据阻塞已由 `evidence/*.png` 和浏览器运行复核解除；review 后产品代码 diff 未变化

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | design 场景1 / review | core | 同日双跑 created=0 | unit/integration | `go test ./internal/reminder/ -run TestScanIdempotent` | created=0 第二次 | pass |
| QA-002 | design 场景9–12 / REV-001 | core | delivered 非终态不 churn；closed 才 churn；再 delivered auto_dismiss | integration | `TestChurnRequiresNoNonTerminalOrders` | 负例+正例+auto_dismiss | pass |
| QA-003 | design 场景15 / 12 | core | 孤儿订单 / 复购 auto-dismiss | integration | `TestAutoDismissOrphanOrderAndRepurchase` | auto_dismissed≥1 | pass |
| QA-004 | design 场景3–6 部分 | core | 生日窗口/跨年/02-29 纯函数 | unit | `TestNextBirthdayOccurrence*` / `TestBirthdayDedup*` | 发生日与 leap clamp | pass |
| QA-005 | design 场景19 | core | Settings 默认 + entry 级叠加 + timezone provider | integration | `TestSettingsDefaultsAndTimezoneProvider` | cosplay 仍 180；provider 跟随 | pass |
| QA-006 | design 场景17–18 / D7 | core | custom dedup + done 幂等 + merge reassign | integration | `TestCustomDoneDismissAndMergeReassign` | custom:{id}；改挂 target | pass |
| QA-007 | design 场景20 | core | runner 每本地日一次 / 跨日 | unit | `TestScanRunnerOncePerLocalDay` | checkpoint 推进 | pass |
| QA-008 | design 场景16–19 API | core | HTTP：settings/me/reminders/scan/校验/404 | API smoke | server :18080 + curl 套件 | 见 §3 | pass |
| QA-009 | design 反向 | core | bind-token / dashboard 404 | API + unit | smoke + packages/router 测试 | 404 not_found | pass |
| QA-010 | CMD-001 部分 | supporting | lint/build/test 全量 | command | frontend lint+build；golangci；`go test ./... -parallel=1` | 全绿 | pass |
| QA-011 | CMD-002 | supporting | generate 幂等 | command | 连续两次 `make generate` + diff | 零二次漂移 | pass |
| QA-012 | design 场景21–23 | core | 前端三路径浏览器运行 | browser + screenshot | `/reminders`、`/settings`、客户档案提醒 tab | 空/载/错+扫描/保存/新增/状态操作 | pass |
| QA-013 | review residual / design §4 | supporting | digest 空窗 / merge 双 pending 等 | residual | 不测 | 已知边界 | residual-risk |
| QA-014 | 清洁度 | supporting | debug/TODO/无用 import/TG 凭证 | diff/grep | 新人写代码扫描 | 无 console/TODO；无 bot token | pass |

## 3. Command Results

- `make check`（真实 index，Docker 启动后）→ build、lint、全部 Go/前端测试通过；仅最终 `generate-check` 因本 feature 生成物尚未提交、相对 HEAD 有预期差异而返回非零
- `GIT_INDEX_FILE=<临时 index> make check` → exit 0：临时 index 只纳入两份当前生成物，用于模拟同提交基线；真实暂存区未改动。前端 build、Go build、golangci-lint（0 issues）、oxlint、全量 Go 测试、4 组前端脚本测试、generate-check 全通过
- `cd backend && go test ./... -count=1 -parallel=1` → exit 0：全包绿；`internal/reminder` 6 条 PostgreSQL 集成/runner 测试通过
- `make generate` + SHA-256 前后比较 → exit 0：`api.gen.go` 哈希 `a0759a...bad6c`、`schema.d.ts` 哈希 `5849c7...a77c7`，二次生成均未变化
- `git diff --check` → exit 0
- 浏览器运行复核（本地 Vite `:5173` + API `:8080`）→ 三路径通过，console error 数为 0
- Round 2 首次 `make check` → Testcontainers 因 Docker daemon 未运行统一失败；启动 Docker Desktop 后复跑已解除，归因环境而非产品
- Round 1 HTTP 冒烟：`./backend/bin/server-qa` @ `:18080` + curl 套件 → 见 §4 QA-008

## 4. Scenario Results

- [x] QA-001 双跑幂等：pass — `TestScanIdempotentAndBirthdayFollowUpChurn`
- [x] QA-002 delivered/closed churn 口径：pass — `TestChurnRequiresNoNonTerminalOrders`
- [x] QA-003 孤儿/复购 auto-dismiss：pass
- [x] QA-004 生日纯函数：pass
- [x] QA-005 Settings 默认与 entry 叠加 + /me 时区：pass（集成测 + HTTP 冒烟双重）
- [x] QA-006 custom/done/merge reassign：pass
- [x] QA-007 runner checkpoint：pass
- [x] QA-008 HTTP 契约冒烟：pass
  - GET `/settings` 默认全类型 180
  - PATCH timezone=America/Los_Angeles + portrait=90 → cosplay 仍 180；GET `/me` 跟随
  - 非法 IANA → 400 validation_failed
  - POST custom → `dedup_key=custom:{id}`；list pending total≥1
  - done 幂等 200；done 后再 dismiss → 400 validation_failed（REV-005）
  - scan 两次均返回三计数 JSON；非法 date → 400
  - bind-token / dashboard → 404 not_found；无 token → 401
  - content 空白 custom → 400
- [x] QA-009 反向 404：pass
- [x] QA-010 全量测试/lint/build：pass
- [x] QA-011 generate 幂等：pass
- [x] QA-012 前端浏览器路径：pass
  - Evidence screenshots:
    - `evidence/reminders-page.png`（3022×1550）
    - `evidence/settings-page.png`（3022×1548）
    - `evidence/customer-reminders-tab.png`（3022×1548）
  - `/reminders`：观察到加载态后出现 1 条 pending；切换「已完成」得到「暂无提醒」空态；手动扫描完成后显示「新建 0 · 跳过 1 · 自动忽略 0」
  - `/settings`：默认/有效值完整加载；提交 `Invalid/Timezone` 显示「timezone 非法 IANA 时区」；恢复 `Asia/Shanghai` 后显示「设置已保存」
  - 客户 `test002` 提醒 tab：真实计数与生日提醒加载；新增 `QA 浏览器验证提醒` 后计数 1→2；执行忽略后状态刷新为 `dismissed`
  - Loading / empty / error：分别由提醒页加载态、已完成筛选空态、settings 非法 IANA 校验态获得运行证据
  - Browser console：0 条 error
  - Notes: 本轮在本地 dev 数据中保留一条已 `dismissed` 的 `QA 浏览器验证提醒` 作为状态变更证据；未改产品代码
- [x] QA-014 清洁度：pass

## 5. Findings

### failed

- none

### blocked

- none

### residual-risk

- design §4 观察项：digest_hour 空窗、复购 cancel 后 churn 静默、timezone 西移自愈、首扫历史噪音、merge 双 pending（D6）
- REV-007 孤儿 auto-dismiss N+1（量级可接受）
- httpapi 专项契约单测仍薄（已由 HTTP 冒烟部分覆盖）
- 真实 index 下 generate-check 在未提交前会把本 feature 的两份生成物视为差异；二次生成哈希不变，临时 index 模拟同提交时完整 `make check` exit 0。提交时必须同时纳入 `api.gen.go` 与 `schema.d.ts`
- 冒烟库为共享 dev PG（曾改账号 password_hash 为 QA 密码以便登录——仅本地 dev）
- 浏览器 QA 在共享本地 dev PG 留下一条已忽略的 `QA 浏览器验证提醒`；不影响产品行为，若需要干净演示数据可人工清理数据库

## 6. Cleanliness

- Debug output: pass（无 console.log / fmt.Println；slog 仅扫描摘要/跳过/runner 错误）
- Temporary TODO/FIXME/XXX: pass
- Commented-out code: pass
- Unused imports / dead code: pass（已删 CustomerExistsActive）
- Out-of-scope files: pass
- Telegram 生产调用: pass（settings 仅存 telegram_chat_id 列；无 bot token）

## 7. Verdict

- Status: **passed**
- Reason: review 通过；后端、API、runner 与生成链路有全量自动证据；场景 21–23 已有截图和真实浏览器操作证据；无 failed / blocked QA item
- Next: `cs-feat` acceptance 阶段

### 后端/API 就绪摘要（供 owner 专注前端）

| 面 | 状态 |
|---|---|
| 扫描规则 + REV-001 | 绿 |
| Settings + /me | 绿 |
| Reminder CRUD/scan HTTP | 绿 |
| 404 反向 | 绿 |
| 全量 go test / lint / FE build | 绿 |
| 浏览器 UI | 绿：三路径截图 + 真实操作复核 |
