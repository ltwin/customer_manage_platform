---
doc_type: feature-qa
feature: 2026-07-11-customer-avatar
status: passed
tested: 2026-07-13
round: 2
---

# customer-avatar QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-design.md`（`status: approved`）
- Checklist: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-checklist.yaml`（8 个 `steps` 均为 `done`；20 个 `checks` 仍为 `pending`，QA 未修改）
- Review: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-review.md`（round 4，`status: passed`，`reviewer: subagent`，blocking=none；基于 qa-fix + REV-008/REV-009 review-fix 后的当前 diff）
- Prior QA: round 1 `status: failed`（QA-F001～QA-F004）；本轮为 qa-fix / review-fix 后的复测
- Implementation evidence: `.codestable/features/2026-07-11-customer-avatar/customer-avatar-implementation-evidence.md`（含 QA-fix、Review-fix 段）
- Evidence pack: none
- Gate results: none
- DoD results: none
- Diff basis: `feat/customer-avatar`，基线 `develop@ededf8de9eda0b848c50f537bc74ef839194e86b`；当前 unstaged + untracked feature 实现批次；0 staged。本轮 QA 只读运行验证并覆写本报告，未改生产代码、design 或 checklist。
- Baseline dirty files: 三个 photographer-private-crm roadmap 文件仍是 implementation start 前共享规划 dirty，不归因于本 feature 实现。
- Review freshness: review round 4 mtime 晚于 qa-fix/review-fix 涉及的 `config.go` / `local.go` / `main.go` / `main_test.go`；QA 期间无代码改动，review 未过期。
- Feature type: `functional`。本 feature 同时改变 UI、鉴权 API、图片处理、数据库 schema、对象持久化、后台维护、进程生命周期与备份恢复语义。
- Core evidence gate: A1–A24 均为 design 核心验收。Round 2 重点复测 round 1 四个 blocking 项及 review §5 Test And QA Focus；其余核心路径以无缓存定向/串行集成测试、前端自动化、构建门禁与 round 1 已落盘的浏览器/Docker/manifest 证据交叉确认（qa-fix 未改前端与 HTTP 语义）。

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | A1、CMD-001 | core-functional | 全仓 build/lint/codegen | command | `make build`、`make lint`、临时 index 下 generate-check | exit 0 | pass |
| QA-002 | A2、D12、QA-F001 | core-functional | require-mount 空值/非法/false；schema/migration | process + unit + integration | 真实进程三态；`go test ./internal/platform/config`；migration 定向测 | 空值→false 越过 config；非法 fail-fast；false 进入 DB 初始化 | **pass**（round 1 fail 已关） |
| QA-003 | A3–A7 | core-functional | 上传/缓存/图片规范化 | integration | `go test ./internal/platform/httpapi -run TestCustomerAvatarHTTP`；avatarimage 包 | 合法链路与反例成立 | pass |
| QA-004 | A8–A11、A24 | core-functional | immutable/CAS/session loss/迟到删除 | integration | `go test ./internal/customer -parallel=1` | exact-generation 隔离成立 | pass |
| QA-005 | A12–A14、QA-F002、QA-F004、REV-008 | core-functional | 中间 symlink、固定叶子 symlink、错误脱敏 | unit + fault | avatarstore symlink 矩阵 + Temporary 无 root 测试 | Put/Open/Stat/List/Delete/Inventory 拒绝 symlink；错误无 root | **pass**（round 1 fail 已关） |
| QA-006 | A15–A16 | core-functional | 列表/详情头像与写操作 | component + prior browser | `npm run test:customer-avatar`；round 1 浏览器证据未失效 | grapheme/媒体/auth revision 隔离 | pass |
| QA-007 | A17–A20 | core-functional | CustomerPicker 与布局 | component + regression | customer-avatar / schedule / avatar-layout 测试 | active 矩阵、exclude、pinned、布局 | pass；空态 ArrowDown residual 保留 |
| QA-008 | A21 | core-functional | mount/volume/manifest | process + prior Docker | `require-mount=true` 非挂载 fail-fast；round 1 named volume/manifest | 无挂载失败；有卷重建保留 | pass（本轮 spot-check + round 1 全量） |
| QA-009 | A22 | core-functional | 范围与清洁度 | diff + lint | `git diff --check`、debug/TODO 扫描、lint | 无方案外实现与临时污染 | pass |
| QA-010 | A23、QA-F003、REV-009 | core-functional | listener/signal 两条有界 runner 等待 | unit + process wiring | `go test ./cmd/server` lifecycle 四测；A23 维护测试 | listener error 与 root cancel 均 bounded | **pass**（round 1 fail 已关） |
| QA-011 | review §5 | core-functional | qa-fix 包 race | race | `go test -race` config/avatarstore/server | 无 data race | pass |

## 3. Command Results

- `make lint` → exit 0：golangci-lint `0 issues`；oxlint 0 warnings/errors。
- `make build` → exit 0：前端 tsc+vite、webui-sync、`go build ./...` 与 `bin/server` 成功；仅既有 Vite chunk 537.17 kB > 500 kB warning。
- 临时 `GIT_INDEX_FILE` 登记预期 codegen 后 `make generate` + `git diff --exit-code` 对 `api.gen.go` / `schema.d.ts` → exit 0：无契约漂移。
- `go test ./internal/platform/config ./internal/customer/avatarstore ./cmd/server -count=1` → exit 0。
- `go test -race ./internal/platform/config ./internal/customer/avatarstore ./cmd/server -count=1` → exit 0。
- `go test ./internal/customer -count=1 -parallel=1` → exit 0（41.639s）。
- `go test ./internal/platform/httpapi -count=1 -parallel=1` → exit 0（18.055s）；`TestCustomerAvatarHTTP` 定向复跑通过。
- `go test ./internal/platform/store ./internal/platform/idempotency ./internal/order ./internal/package ./internal/schedule -count=1 -parallel=1`（order 首次 1 项 testcontainers 端口失败后 retry 通过）→ 全绿。
- 默认高并行 `go test ./...` 多次出现 `container connection string: port "5432/tcp" not found`（Testcontainers/Docker Desktop 映射竞态）；单测 `TestCreateListAndDetail` 与 `-parallel=1` 包测稳定通过。**归因环境并行压力，非本 feature 逻辑失败**；QA 以串行无缓存包测作为 A1 后端运行证据。
- 前端：`npm run test:customer-avatar` 6/6、`test:avatar-layout` 1/1、`test:schedule` 32/32、`test:package-price` 4/4、`test:api-client` 3/3、`lint`、`build` → 全绿。
- `docker compose config` → exit 0。
- 真实进程 require-mount 三态（`AUTH_TOKEN_SECRET` + 不可达 `DATABASE_URL` + 临时 root）：
  - `env -u AVATAR_LOCAL_REQUIRE_MOUNT` → exit 1，错误为 DB dial refused（**已越过 config**，证明空值默认 false）。
  - `AVATAR_LOCAL_REQUIRE_MOUNT=sometimes` → exit 1，`AVATAR_LOCAL_REQUIRE_MOUNT 必须是 true 或 false`。
  - `AVATAR_LOCAL_REQUIRE_MOUNT=false` → exit 1，DB dial refused（config 通过）。
  - `AVATAR_LOCAL_REQUIRE_MOUNT=true` 且 root 非独立挂载 → exit 1，`AVATAR_LOCAL_ROOT 不是可验证的独立挂载点`。
- `git diff --check` → exit 0；生产 diff 无新增 `console.log` / `fmt.Print` / TODO/FIXME/XXX。

## 4. Scenario Results

- [x] QA-001 全仓门禁：pass。
  - Evidence: build/lint/generate-check 全绿；后端以 `-parallel=1` 无缓存包测全绿。
  - Notes: 默认并行 `go test ./...` 在本机 Docker Desktop 下不稳定；不据此判 product fail。

- [x] QA-002 A2 / QA-F001 契约与 require-mount：**pass**（关闭 round 1 QA-F001）。
  - Evidence: `parseRequiredBool` 空串→false；`TestLoadAvatarStorageDirectBinaryDefaultsRequireMountToFalse` 通过；真实 unset 进程进入 DB 初始化失败而非 config 拒绝；非法值仍 fail-fast；migration 定向测通过。
  - Notes: production Compose 仍显式 true；direct-binary 缺省 false 与 D12 一致。

- [x] QA-003 A3–A7 上传/缓存/规范化：pass。
  - Evidence: HTTP avatar 垂直切片与 avatarimage 包无缓存通过；round 1 真实 JPEG PUT/GET/304 证据未因 qa-fix 失效。

- [x] QA-004 A8–A11/A24 跨资源一致性：pass。
  - Evidence: customer 包串行全测覆盖 application CAS、maintenance exact-generation、DB session loss 与 pre-current burn。

- [x] QA-005 A12–A14 / QA-F002 / QA-F004 / REV-008：**pass**（关闭 round 1 QA-F002、QA-F004）。
  - Evidence:
    - 六层中间目录 symlink × Put/Open/Stat/List/Delete 全部返回 `ErrAvatarObjectKey`，root 外 sentinel 不变。
    - `content`/`metadata.json` 固定叶子 symlink × Put/Open/Stat/Delete/Inventory 全部 `ErrAvatarObjectKey`，外部 generation 完整。
    - `safeJoin` 逐段 `Lstat` 拒 symlink；`validateGenerationLeaves` 拒 symlink/非 regular。
    - `temporaryError` 只保留分类与 operation 名；`TestLocalStoreTemporaryErrorsDoNotExposeRootPath` 断言错误串不含 root。
  - Notes: TOCTOU / 启动期 PathError 仍为 residual（review 已声明非阻塞）。

- [x] QA-006 A15–A16 列表/详情/写操作：pass。
  - Evidence: 组件 6 测全绿；qa-fix 未改前端；round 1 真实详情页 grapheme/上传/移除浏览器证据仍适用。

- [x] QA-007 A17–A20 CustomerPicker 与布局：pass。
  - Evidence: picker 矩阵与 schedule 32 项、layout 测全绿；round 1 订单 picker 与 375px 浏览器证据仍适用。
  - Notes: 空结果 ArrowDown `activeIndex=-1` 保留 residual-risk。

- [x] QA-008 A21 mount/volume/backup：pass。
  - Evidence: 本轮 `require-mount=true` 非挂载 fail-fast；round 1 Docker named volume recreate + exact-generation manifest generate/verify/篡改拒绝仍有效（qa-fix 未改 Dockerfile/manifest 语义）。

- [x] QA-009 A22 范围与清洁度：pass。
  - Evidence: 无 OSS/预签名/建档头像/裁剪相册/禁用格式/JSON binary export/UI 库；lint 与 diff 清洁度通过；安全日志 round 1 失败已由 QA-F004 关闭。

- [x] QA-010 A23 / QA-F003 / REV-009：**pass**（关闭 round 1 QA-F003）。
  - Evidence:
    - `serverLifecycle.wait`：listener error 分支 `cancel` + 共享 timeout 的 `waitForRunner`；signal/root-cancel 分支另调 `Shutdown` 后同一 deadline 等待。
    - `TestServerLifecycleBoundsListenerErrorWhenRunnerDoesNotStop`：runner 永不结束时仍在 deadline 内返回。
    - `TestServerLifecycleBoundsSignalShutdownWhenRunnerDoesNotStop`：断言调用 HTTP Shutdown 且 bounded。
    - `waitForRunner` deadline / normal-done 单测通过。
    - A23 pointer audit + 三页 restart/single-flight/100 due 测试包含于 customer 包串行全测。
  - Notes: 完整 DB/router composition 进程 smoke 与 signal fake deadline 断言仍为 review suggestion residual，不阻塞。

- [x] QA-011 race：pass。
  - Evidence: qa-fix 三包 `-race` 通过。

## 5. Findings

### failed

- none。

### blocked

- none。

### residual-risk

- 默认高并行 `go test ./...` 在本机 Docker Desktop 下可能因 Testcontainers `port "5432/tcp" not found` 偶发失败；串行 `-parallel=1` 稳定。属测试环境竞态，不是产品行为缺陷；acceptance 若跑默认 `make check` 的 `go test ./...` 需注意同一环境限制。
- REV-006：CustomerPicker 空结果 ArrowDown 内部 `activeIndex=-1`（非核心键盘状态）。
- REV-007：option 稳定 ID / `aria-activedescendant`、Tab/blur/click-outside 自动化不足。
- REV-011/012/013：Lstat→I/O TOCTOU；signal fake 未断言 deadline；lifecycle 测试未跑完整 DB/router composition。
- 启动期 config/NewLocal/cleanup 的底层 PathError 仍可能被 main 完整记录（不等于后台 runner temporary 脱敏范围）。
- MaintenanceRunner 跨进程双 runner lost-update 主要依赖行锁/唯一约束/幂等 Delete。
- durability 依赖目标 ECS 文件系统对 directory fsync/atomic rename 的支持；Docker Desktop named volume 不能替代上线机留证。
- React DOM 未自动化快速 mount/unmount + 401 + image onError 组合时序。
- 三个 roadmap baseline dirty 文件后续必须 scoped commit 排除或单独归因。

## 6. Cleanliness

- Debug output: pass。
- Temporary TODO/FIXME/XXX: pass（命中仅为文档/证据文字，非生产标记）。
- Commented-out code: pass。
- Unused imports / dead code from this feature: pass（lint/build 全绿）。
- Credentials: pass。进程烟雾仅用本地临时 secret/不可达 DB，未写入仓库。
- PII fixtures: pass。
- Out-of-scope files: pass with baseline note（三份 roadmap dirty 已记录）。
- Safe operational logs: pass（QA-F004 已关；后台 temporary 错误不含 root）。

## 7. Verdict

- Status: `passed`
- Blocking QA items: none。Round 1 的 QA-F001、QA-F002、QA-F003、QA-F004 均已关闭并有运行证据。
- Passed evidence: 串行无缓存后端集成、qa-fix 定向与 race、前端全相关测试与构建、lint、codegen 一致性、真实 require-mount 进程三态、symlink/叶子 hardening、lifecycle bounded wait；UI/Docker/manifest 核心路径由 round 1 全量证据 + 本轮自动化回归共同支撑。
- Acceptance gate: checklist `checks[]` 仍保持 `pending`，由 acceptance 阶段逐项勾选。
- Next: 进入 `cs-feat` **acceptance** 阶段（`/cs-feat --stage accept` 或兼容入口 `cs-feat-accept`）。
