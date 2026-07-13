---
doc_type: feature-review
feature: 2026-07-11-customer-avatar
status: passed
reviewer: subagent
reviewed: 2026-07-13
round: 4
---

# customer-avatar 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md`（`status: approved`）
- Checklist: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-checklist.yaml`（8 steps 全 `done`；20 checks 仍 `pending`）
- QA: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-qa.md`（round 1，`status: failed`，QA-F001～QA-F004）
- Evidence pack / Gate results / DoD results: none
- Implementation evidence: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-implementation-evidence.md` 的 QA-fix 与 REV-008/REV-009 review-fix 段
- Diff basis: `develop@ededf8de9eda0b848c50f537bc74ef839194e86b` 上当前 unstaged + untracked feature diff；本轮复审范围为 config、local adapter、server lifecycle、对应测试和 README。无 staged diff。
- Baseline dirty files: 三个 photographer-private-crm roadmap 文件属于 implementation start 前共享规划 dirty，不归因于 qa-fix。
- Prior rounds: round 2 在 QA 前 passed；QA-fix 后 round 3 发现 REV-008/REV-009 并 `changes-requested`；本报告是 review-fix 后 round 4。

### Independent Review

- Detection: 原生 Codex Task agent 可用；OCR CLI 可用且 `ocr llm test` 成功。
- 环节 A 独立隔离 Task agent: `native-agent + completed`。窄范围 retry reviewer 完成 round 3 与 round 4 复审；round 4 只读核验最新 REV-008/REV-009 diff，并运行定向 race 测试。
- 环节 B OCR CLI: `skipped-scope-ambiguous`。工作区同时包含实现前 roadmap dirty 与整批未提交 feature，按协议不裸扫；本轮人写文件由 Task agent 与主 agent逐行覆盖。
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded。
- Merge policy: Task agent 输出已由主 agent用当前源码、测试与 QA 契约逐条核验；REV-008/REV-009 均有 production wiring + 会真实失败的回归测试。
- Gate effect: none；可进入 QA round 2。

## 2. Diff Summary

- QA-fix 修改：`backend/internal/platform/config/config.go`、`config_test.go`、`backend/internal/customer/avatarstore/local.go`、`local_test.go`、`backend/cmd/server/main.go`、`README.md`、implementation evidence。
- QA-fix 新增：`backend/cmd/server/main_test.go`。
- 删除：none。
- 未跟踪 / staged：新增测试与原 feature 新文件仍 untracked；0 staged。
- 风险热点：filesystem no-follow/regular-file、exact-generation Inventory、后台错误脱敏、HTTP/runner lifecycle、测试假阳性。
- 范围守护：未处理 CustomerPicker、a11y、双 runner、TOCTOU、启动期日志或其他 residual；公开 store API、object key、GC/HTTP 语义不变。

## 3. Adversarial Pass

- 假设的生产 bug：安全修复只覆盖领域 key 目录却遗漏 adapter 固定叶子，或测试只测 helper、生产 wiring 删除后仍绿。
- 主动攻击过的反例：六层目录 symlink；完整 root/outside generation 的 `content`/`metadata.json` symlink；Put 错误 replay、Open/Stat 越界读、Delete/Inventory；非 regular leaf；listener error + runner 永不结束；root cancel + runner 永不结束；HTTP Shutdown 是否实际调用；unset/invalid/false 三态；Lstat→I/O TOCTOU。
- 结果：round 3 的 fixed-leaf 与 lifecycle wiring 反例已由 review-fix 关闭；TOCTOU、启动期路径日志与完整 composition smoke 留 residual/suggestion，不阻塞本轮。

## 4. Findings

### blocking

none。Round 3 REV-008/REV-009 均已关闭。

### important

none。

### nit

none。Round 3 REV-010（测试用 empty 表达 unset）由真实 `env -u` 进程证据补足，不影响行为判断。

### suggestion

- [ ] REV-012 `backend/cmd/server/main_test.go:69` signal fake 目前只证明 Shutdown 被调用，没有断言传入 context 带 deadline。
  - Evidence: production `main.go:133-136` 明确传 `shutdownCtx`，当前无 bug；未来误传 `context.Background()` 时，runner wait 仍可能让现测试绿色。
  - Expected scope: 后续可让 fake 检查 `ctx.Deadline()` 或阻塞到 `ctx.Done()`；不作为本轮门禁。
- [ ] REV-013 `backend/cmd/server/main_test.go:31-88` 测试覆盖真实 production coordinator 分支，但不执行完整 DB/router composition。
  - Evidence: `run → serverLifecycle.wait` 只有 `main.go:100-107` 的直接装配，静态可核验；QA-F003 明确允许可注入测试。
  - Expected scope: 后续需要更强 composition 防回退时补薄 assembly seam 或进程 smoke；本轮不扩 server framework。
- [ ] REV-011 逐段 Lstat 后执行完整路径 I/O 仍有 check-then-use 竞态；未来 Linux hardening 可评估 directory handle + no-follow。

### learning

- avatar generation 的安全路径不变量同时包含六段 object key 与 adapter 固定 `content`/`metadata.json`；online API 与 offline Inventory 必须复用同一验证。
- lifecycle 测试必须覆盖 production coordinator 的分支与副作用，单测 `waitForRunner` helper 不能证明 wiring。

### praise

- Fixed-leaf 测试同时预置 root/outside 两个完整 generation，能真实捕获旧 Put 错误 `Created:false`，并对 Put/Open/Stat/Delete/Inventory 逐项证明 root 外 sentinel 不变。
- `validateGenerationLeaves` 保持 public store port 不变；symlink 返回 key error，其他非 regular file 返回 integrity，所有对象操作复用。
- `serverLifecycle.wait` 是窄 composition seam：listener-error 与 root-cancel 直接使用单一 timeout 预算，测试删除任一 cancel/wait/Shutdown wiring 都会失败。
- QA-F001 空值→false、非法非空 fail-fast、README/Compose 双轨一致；QA-F004 后台 temporary error 保留分类但不传播 root。

## 5. Test And QA Focus

- QA 必须重点复核：六层目录 symlink 矩阵继续通过；`content`/`metadata.json` × Put/Open/Stat/Delete/Inventory 返回 `ErrAvatarObjectKey`，Put 不再错误 replay，root 外 bytes/metadata/目录完整。
- QA 必须重点复核：正常 generation 的 Put replay/Open/Stat/List/Inventory/Delete；directory/socket/FIFO 等非 regular leaf 返回 integrity；idempotent Delete 保持成功。
- QA 必须重点复核：listener error + runner 永不结束时 cancel 且 bounded，Shutdown 不调用；root cancel + runner 永不结束时 cancel + Shutdown 且共享单一 deadline；runner 正常结束时保留原 listener/Shutdown error。
- QA 必须重点复核：真实 unset、非法非空、显式 false；后台 slog 不含 avatar root。
- Evidence pack residual risks / gate warnings: 无独立 evidence pack；OCR scope ambiguous 已记录。
- 建议加强：signal fake 验 deadline；进程级 SIGTERM/listener bind failure smoke；更多 Put/Open/List/Delete filesystem error 的 slog capture。
- 不能靠 review 完全确认：同主机并发换链 TOCTOU；启动期 PathError；目标 ECS filesystem/mount 参数。

## 6. Residual Risk

- 启动期 config/NewLocal/cleanup 的底层 PathError 仍可能被 main 完整记录；不重现本次 QA-F004 的后台 runner error，但“所有运维日志无部署路径”仍需后续单独 hardening。
- Lstat pre-check 不是强 no-follow 原语；能并发修改卷的同主机写入方仍可能在检查与 I/O 之间交换目录。
- CustomerPicker `activeIndex=-1`、`aria-activedescendant`/Tab/blur、双 runner lost-update、React 快速 mount/unmount+401+onError、目标 ECS fsync/atomic rename 等前轮 residual 继续保留；qa-fix 未授权处理。
- 三个 roadmap baseline dirty 文件后续必须 scoped commit。

## 7. Verdict

- Status: `passed`
- Blocking: none；REV-008、REV-009 closed。
- Important: none。
- Next: 进入 `cs-feat` QA round 2；QA passed 后才可进入 acceptance。
