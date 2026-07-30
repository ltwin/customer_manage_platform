---
doc_type: feature-review
feature: 2026-07-21-data-export
status: passed
reviewer: subagent
reviewed: 2026-07-21
round: 3
---

# data-export 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-21-data-export/data-export-design.md`
- Checklist: `.codestable/features/2026-07-21-data-export/data-export-checklist.yaml`；STEP-001～STEP-007 均为 `done`，acceptance checks 尚未验收
- Evidence pack: `.codestable/features/2026-07-21-data-export/data-export-evidence-pack.md`
- Gate results: `.codestable/features/2026-07-21-data-export/data-export-gate-results.json`
- DoD results: `.codestable/features/2026-07-21-data-export/data-export-dod-results.json`
- Implementation evidence: `.codestable/features/2026-07-21-data-export/data-export-implementation.md`
- Diff basis: 当前未暂存工作区 diff、未跟踪的新实现文件与生成物；Git index 为空
- Baseline dirty files: `.workflow/`、`install-cpamp.sh` 为本 feature 范围外的既有未跟踪项，不纳入结论，也不得修改或删除

### Independent Review

- Detection: 原生 Codex Task agent 可用；`ocr` CLI 可用且连接正常，但当前工作区包含本 feature 范围外的未跟踪路径
- 环节 A 独立隔离 Task agent: `native-agent` + `completed`（`/root/data_export_goal_driver/data_export_code_review_r1`）
- 环节 B OCR CLI: `skipped-scope-ambiguous`；裸 workspace 模式会把 `.workflow/`、`install-cpamp.sh` 纳入扫描，违反 current-scope 规则
- OCR severity mapping: High→blocking/important，Medium→nit/suggestion，Low→discarded
- Merge policy: Task agent 结论已按 design、生产中间件、实现代码、测试与证据文件逐条核验后合并；OCR 因范围歧义未启动，不以本地扫描伪装成 OCR 结果
- Gate effect: `reviewer: subagent` 满足独立性锚点；round 3 的 spec compliance 与 code quality 均 passed，0 unresolved blocking/important/nit，可以进入 QA

## 2. Diff Summary

- 新增：`backend/internal/dataexport/`、data-export HTTP handler/route tests、只读快照事务文件与集成测试、前端导出测试脚本、`DataExportCard` 与下载协作模块，以及本 feature 的 CodeStable 证据产物
- 修改：OpenAPI 与 Go/TS 生成物、server/router/auth/store/settings 装配、SettingsPage、Makefile/package scripts、roadmap item 与主文档
- 删除：none；`scope.go` 中事务代码为纯移动至 `scope_tx.go`，不是能力删除
- 未跟踪 / staged：本 feature 新文件均未跟踪；Git index 为空；`.workflow/`、`install-cpamp.sh` 为范围外 baseline
- 风险热点：账号级 PostgreSQL snapshot、跨七类实体 projection、HTTP 取消/发送边界、PII allowlist、前端 Blob 生命周期与浏览器交互证据

## 3. Adversarial Pass

- 假设的生产 bug：HTTP request context 仍有效，但下层返回包装的 `context.Canceled` / `DeadlineExceeded`；handler 仅凭错误类型把它当作请求取消，最终隐式返回空 200。
- 主动攻击过的反例：request context 是否 done 与下层 error chain 是否 canceled 的四象限、生产 middleware 后置行为、Clock/Repository 时序、scanner/nullable/终态/tie-break、Settings parity、七类 route 全文档、A7/A13/A14 证据分级与 evidence pack 单独阅读口径。
- 结果：CR-DE-001 原始“已取消请求被渲染为 500”已修复，但 active request 的孤立取消错误被吞为 200，仍保留 important；CR-DE-002/003/004 resolved；新增 2 个证据敏感度/口径 nit。

## 4. Findings

### blocking

none。

### important

- [x] CR-DE-001 `backend/internal/platform/httpapi/data_export.go:87-102` request context 已成为取消分类的唯一权威（来源：native-agent；round 3 resolved）。
  - Evidence: `dataExportRequestCanceled` 在 `Request.Context().Err() == nil` 时，仍会因为 `errors.Is(err, context.Canceled/DeadlineExceeded)` 返回 true；`ExportAll` 随即直接返回，不调用 `c.Error` 或写明确 status。当前 wrapped-cancellation 测试使用 active `context.Background()`，且只断言无 body/header，没有断言应为 500。
  - Impact: Repository/driver/child context 的孤立取消错误会被 fail-open 成空 200，请求日志与成功率指标错误；前端最终虽会因 MIME 缺失拒绝，但服务端违反 design D7“仍连接失败走标准错误封套”。
  - Resolution: active request 的 wrapped canceled/deadline 均走 500 ErrorEnvelope；真实 canceled/deadline request 不写附件/封套，`c.Errors=0`，日志只有固定 stage/reason 且不泄漏 wrapped `PII-SENTINEL`。Build 成功后的最终 pre-send 取消也继续覆盖。

- [x] CR-DE-002 `backend/internal/dataexport/service.go:43` `exported_at` 已在 snapshot 前捕获（来源：native-agent；round 2 resolved）。
  - Evidence: 当前 Service 先执行 `repo.LoadSnapshot`，Repository 返回后才调用 `clock.Now().UTC()`；design A-H3 与时序图要求先捕获导出时间，再进入 Repository。
  - Impact: 大账号或慢查询下 `exported_at` 可能比请求启动、snapshot 建立晚数秒或数分钟，破坏导出文档的审计语义；现有测试仅校验值，没有校验调用顺序。
  - Resolution: Service 严格 `clock → repository` 且 Clock 一次；Document 与文件名继续使用同一 `ExportedAt`。

- [x] CR-DE-003 `.codestable/features/2026-07-21-data-export/data-export-implementation.md` 核心证据已补强且过度声明已纠正（来源：native-agent；round 2 resolved）。
  - Evidence: Settings parity 只比较 timezone 与 threshold 长度；Repository 对七类实体主要断言长度和少数字段；真实 route fixture 只覆盖 Customer/SocialIdentity；A7 只真实注入头像/GC sentinel；A9 绕过生产 middleware；React loading/retry/401/快速重复触发主要依靠源码正则；截图原始像素尺寸与报告中 1280/375 viewport 口径不自洽。
  - Impact: 当前绿灯无法可靠捕获 scanner 列顺序错位、nullable 字段丢失、mapper 漏字段、Settings 默认补齐错误、完整 route projection 回归和生产 middleware 改写错误路径；实现报告会误导后续 QA/acceptance 对证据等级的判断。
  - Resolution: 完整 expected Snapshot/Settings deep parity/七类真实 route exact JSON/counts/连续导出均已落地；A7/A13/A14 已诚实分级，截图 metadata 已补。tie-break fixture 插入顺序仍有一个非阻塞敏感度 nit，见 CR-DE-005。

### nit

- [x] CR-DE-004 `api/openapi.yaml:44` export tag 已去除“未实现”（来源：native-agent；round 2 resolved）。
  - Evidence: 受保护的生产 route 与真实 Service composition 已存在。
  - Impact: 生成文档会误导开发者，但不影响运行时。
  - Resolution: 仅更新 tag 描述，endpoint/schema/reference-only 边界均未扩张。

- [x] CR-DE-005 `backend/internal/dataexport/repository_test.go` tie-break fixture 已改为逆序/固定乱序插入（来源：native-agent；round 3 resolved）。
  - Evidence: Customer、Identity、Note、Package、Order、Slot、Reminder 的插入顺序与 expected 顺序基本一致。
  - Impact: 生产排序当前正确，但测试对未来误删 `id` tie-break 的敏感度不足。
  - Resolution: Customer 为 `a,c,b`，其余 tied 集合为逆序或非最终顺序；expected 仍为 `created_at ASC, id ASC`，生产排序未改。

- [x] CR-DE-006 `.codestable/features/2026-07-21-data-export/data-export-evidence-pack.md` 已区分 machine-runner warnings 与 feature residual risks（来源：native-agent；round 3 resolved）。
  - Evidence: A13 React 行为、A14 精确 viewport、同步内存模型、headers 后中断、typed-nil/partial writer 均仍有明确残余风险。
  - Impact: 单独阅读 evidence pack 会把“machine runner 无 warning”误解为“feature 无剩余风险”。
  - Resolution: evidence pack 明确 canonical CMD-001～004 全绿，同时列出 A13/A14 QA、同步内存、headers 后中断、reference-only 头像与非阻塞 suggestion 风险。

### suggestion

- [ ] SUG-DE-001 对 `ClockFunc(nil)` 这类 interface typed-nil 增加局部防御或明确构造约束；当前生产 composition 传入非 nil `ClockFunc(time.Now)`，不引入通用反射框架。
- [ ] SUG-DE-002 增加 `n < len(body), err != nil` 的 partial writer 测试，证明仍只记录脱敏 transport failure 且不尝试第二封套；无需支持违反 `io.Writer` 契约的 `n < len(body), nil`。

### learning

- HTTP 取消与“发送前不写”必须经过真实 middleware 链测试；直接 handler 单测只能证明局部顺序，无法覆盖 handler 返回后的错误封套行为。
- 证据报告必须区分“生产实现采用 allowlist”与“自动化 fixture 对每个禁止字段注入唯一哨兵”，也必须区分 CSS viewport、devicePixelRatio 与 PNG 原始像素尺寸。

### praise

- 账号隔离与只读边界保持扎实：七类数据与 Settings 共用同一账号级 `REPEATABLE READ, READ ONLY` snapshot，`ReadTxAccountScope` 不暴露写 delegate 或原始 transaction。
- reference-only allowlist 符合 owner 决策：公开 revision/version/url 可导出，头像字节、内部 object ID、物理 key、manifest、GC/reconciliation 与凭证没有生产查询或 projection 路径。
- OpenAPI 命名 schema、完整预序列化和前端严格 MIME/filename/Blob 生命周期方向正确，且没有手写重复的 ExportDocument 顶层 DTO。

## 5. Test And QA Focus

- QA 必须重点复核：active/canceled request × wrapped canceled/deadline 四象限；Clock 在 Repository 前且只调用一次；七类全字段 route 文档与 counts；Settings 完整 parity；同一静态数据连续导出；保存并重新解析下载文件。
- 浏览器必须真实复核：401 转登录、500 后重试、`response.blob()` rejection、快速重复点击、Settings 加载失败时卡片仍可用；失败时 object URL/create/click 均为 0，成功时只触发一次并 revoke。
- 视觉证据必须记录 CSS viewport、devicePixelRatio、innerWidth、scrollWidth、PNG 原始像素尺寸；不能把物理 PNG 宽度直接称为 viewport 宽度。
- Evidence pack residual risks / gate warnings：Docker/Testcontainers 偶发 `port "5432/tcp" not found` 有既有 provenance；同一代码状态下定向串行复跑和完整 `make check` 已通过，QA 仍需保留原始结果与重跑链。
- 建议新增或加强的测试：完整 middleware cancellation、Service 调用顺序、七类实体/nullable/终态/tie-break、真实 route 全文档、Settings deep parity、partial writer。
- 不能靠 review 完全确认的点：同步全内存物化在真实最大账号规模下的耗时/内存、内核缓冲后的 transport 断连可观测性、跨部署头像 URL 不可恢复边界。

## 6. Residual Risk

- 批准的同步全内存物化没有文件大小、耗时或并发导出硬上限；本 feature 不扩成 streaming/async，QA 只记录风险，不改变设计。
- 成功 headers 写出后的网络中断不可逆，服务端未必能观察内核缓冲后的断连；依赖 Content-Length、浏览器 Blob 读取失败和 UI 不下载/可重试共同收敛。
- 头像 URL 是当前部署的 reference-only 引用，不是媒体备份，不能承诺跨部署恢复；UI 与 acceptance 必须持续保持该口径。
- 当前两张 PNG 的页面内容可信，但精确 1280/375 viewport 不能仅由现有物理像素尺寸独立复核；review-fix 应补元数据，QA 再执行真实 viewport 验收。
- `Clock` interface typed-nil 仍是低概率装配风险；当前生产 composition 不受影响，若本轮不修则保留为 acceptance residual risk。

## 7. Verdict

- Status: passed
- Findings: 0 unresolved blocking、0 unresolved important、0 unresolved nit
- Spec Compliance: passed
- Code Quality: passed
- Next: `cs-feat` QA 阶段；真实执行 A1～A18，重点覆盖 A13 React/browser、A14 精确 viewport/键盘/截图元数据与实际下载文件解析。
