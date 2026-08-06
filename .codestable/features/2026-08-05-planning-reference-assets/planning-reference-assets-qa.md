---
doc_type: feature-qa
feature: 2026-08-05-planning-reference-assets
status: passed
qa_owner: subagent
qa_date: 2026-08-06
review_round_cap: 3
---

# planning-reference-assets 独立 QA 重试报告

## 结论

**passed**。本次为 round-3 之后的独立 QA 重试，不启动第 4 轮 code review。已验证 REV-016 的 owner-cap 窄修复：`CreateBinding` 对缺失/非法 required、minimum、holder kind、holder id 等输入返回可识别的 `planningmedia.ErrValidation`，HTTP handler 的 `h.fail` 稳定映射为 `400 validation_failed`，不会再把 malformed bind 请求渲染为 500。生成物、后端/前端门禁与 diff 检查均通过。

## 执行证据

| 检查 | 结果 |
|---|---|
| `make generate-check` | 通过；Go 与 TypeScript OpenAPI 生成完成且无差异错误 |
| `make check` | 通过（exit 0）；包含前端 build/lint、Go build、golangci-lint 与串行 `go test -p=1 ./... -count=1 -parallel=1`。Testcontainers 按 attention 规则串行运行，未复现 mapped-port 抖动 |
| `go test -p=1 ./internal/planningmedia ./internal/platform/httpapi ./internal/shootplanning -count=1 -parallel=1` | 通过：planningmedia、httpapi、shootplanning 全部 PASS |
| `frontend npm run test:planning-media` | 通过，4/4 |
| `frontend npm run build` | 通过；仅有 Vite chunk-size 提示，不是失败 |
| `frontend npm run lint` | 通过 |
| `git diff --check` | 通过，无 whitespace 错误 |

## REV-016 核验

- `backend/internal/planningmedia/application.go` 的 `CreateBinding` 在进入幂等执行器、数据库及 holder authorizer 前，校验 plan/asset、generation、expected revisions、holder id 和 holder kind；失败值通过 `fmt.Errorf("%w: ...", ErrValidation)` 包装。
- `backend/internal/platform/httpapi/planning_media.go` 的 `bind` 继续使用 strict JSON（未知字段/trailing value 拒绝），并将 `errors.Is(err, planningmedia.ErrValidation)` 映射为 400。
- 因而 `{}`、required 字段缺失或为 0、未知 `holder_kind`、空 `holder_id` 等 malformed bind 输入不再落到默认 500，也不会产生 binding/idempotency side effect。
- OpenAPI `CreateAssetBindingInput` 的 `additionalProperties: false`、required、minimum、holder enum 与实现校验一致；`make generate-check` 确认 Go/TS 边界未漂移。

## 重点安全/生命周期核验

只读检查当前 design/checklist/implementation/evidence/gates/DoD、round-3 review 与源码，确认以下仍保持：

- `AccountScope` 覆盖 gallery、asset/rendition/binding/lease/read-pin/inventory；cross-account 与不存在 plan/shot 失败关闭，object key 不进入 DTO/DOM/公开 URL。
- holder proof 在同一 scoped transaction 内签发；plan/shot not-found=404、revision conflict=409，archived/completed lifecycle 按契约返回冲突。
- multipart rights 使用 strict decoder（未知字段及 trailing JSON 拒绝）；upload spool 有界，原图/display rendition、对象/DB/ledger 失败清理和 orphan reconciliation seam 存在。
- bind/release canonical idempotency frame、single claim/replay、duplicate active binding、required revisions 与 holder errors 保持 fail-closed；release revision 缺失在幂等/DB 前返回 validation。
- `BatchShotAccessRefsInScope` 对去重 shot id 使用受控 SQL `IN` pushdown，shot 数超过 30 返回 projection unavailable，结果最多 100；Run Mode 首次 callback 投影、replay 不重复查询，projection unavailable 降级为空 snapshot。
- OpenDisplay/read-pin/GC/inventory/account key 路径保持 scoped；GC physical delete failure 保留 `gc_pending` 并可重试，删除 inventory 过滤已删除资产。
- Run Mode failure/loading/race、ReferenceSheet execution-only 与不可用提示、avatar regression/scope negatives 未见本轮回退。

## Residual risks（非阻断）

1. 当前仓库仍缺真实 HTTP multipart negative fixture；rights unknown field、重复 JSON value、MIME/413/415 边界应在后续 QA 环境补充 route-level 证据。
2. projection 100/101 binding、30-shot 上界及跨 plan/account 的真实 Testcontainers 边界证据仍建议补齐；本轮已有实现与集成测试通过。
3. `PlanningMediaPanel` 的极端慢 list/upload 交错仍有 REV-017 所述 generation-loading 清理风险（旧 list finally 过期时可能遗留 loading）；这是已知 non-blocking residual risk，不重新开启 review。
4. 浏览器真实 375px/coarse pointer/键盘/200% zoom 视觉证据尚未在本次命令门禁中采集。
5. archguard/meta-cc 等 provider signals 为 skipped/disabled；这是外部 provider warning，不应误判为实现失败。

## Review 轮次声明

本报告是 round-3 后的 QA closure retry。review round cap 已达到 3；未进行、也不应自动进行第 4 轮 review。若未来发现新的 blocking 问题，应交由 owner checkpoint 决策。
