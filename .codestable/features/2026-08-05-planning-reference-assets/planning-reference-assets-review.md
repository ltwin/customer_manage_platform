---
doc_type: feature-review
feature: 2026-08-05-planning-reference-assets
status: passed
reviewer: subagent
reviewed: 2026-08-06
round: 3
lane_a_state: completed
lane_a_ref: ""
lane_a_reason: "第 3 轮也是 owner 批准的最后一轮独立 closure review；已核对 round-2 修复及当前 worktree"
lane_b_state: unavailable
lane_b_ref: ""
lane_b_reason: "OCR/行级扫描 lane 在 round-2 超时退出；本轮不重启该 lane，结论来自当前源码、规格、生成物与可见测试证据"
---

# planning-reference-assets 第三轮最终 closure review

> 当前 verdict：`passed`。下方 REV-016 保留第 3 轮发现的原始记录；其后 owner-cap 窄修复已由独立 QA closure 验证并消费。本报告仍是 round=3，不代表启动第 4 轮 review。

## 1. 审查范围与 verdict

本轮只读核对了 feature design、checklist、implementation、evidence pack、所有 gate/DoD JSON、round-2 review、当前 diff，以及 planning-media 后端、shootplanning projection、OpenAPI/生成代码和前端工作台/Run Mode。重点验证 REV-010 至 REV-015 的修复，并额外做了一次整体安全、隔离、生命周期、错误矩阵、幂等、迁移和 UI 行级审查。

**第 3 轮当时 verdict：changes-requested。** 发现一条阻断首版 API 契约的 validation 错误映射问题（REV-016）。这是本 feature 允许的第 3 轮、也是最后一轮 review；本轮之后不得自动再开 review。该 finding 随后以 owner-cap 窄修复闭环，并由独立 QA 重新验证；因此当前报告最终状态为 `passed`，不是第 4 轮 review。

## 2. Round-2 findings closure

| Finding | 本轮结论 |
|---|---|
| REV-010 HolderAuthorizer core 错误未映射 | **已修复**：`planning_media.go` 显式把 `shootplanning.ErrPlanNotFound`/`ErrShotNotFound` 映射为 404，把 `ErrPlanRevisionConflict` 映射为 409；`MediaHolderAuthorizer` 保持在同一 `TxAccountScope` 内验证 plan/shot/lifecycle。 |
| REV-011 release 缺 `expected_plan_revision` fail-open | **已修复**：`ReleaseBinding` 在进入 idempotency/DB/holder 调用前要求 plan、asset、binding 三个 revision 均 `>=1`，使用 `ErrValidation`，handler 映射 400。 |
| REV-012 projector 先取全 plan 100 行再过滤 shot | **已修复**：`BatchShotAccessRefsInScope` 将去重后的 shot id 作为受控 SQL `IN` 条件下推，仍为单次 query、最多 100 条返回，并对超过 30 个 shot 返回 `ErrProjectionUnavailable`；core 对该不可用结果降级为空 snapshot。当前 boundary fixture 仍属于 QA focus。 |
| REV-013 rights JSON 未 strict decode | **已修复**：multipart `rights` 使用 `decodeStrictJSON`，`DisallowUnknownFields` 且拒绝 trailing JSON value；`permitted_uses`/未知字段不会被静默接受。当前仍缺真实 HTTP multipart negative fixture，列入 residual risk/QA focus。 |
| REV-014 panel 缺 loading 且 list/upload 覆盖竞态 | **大部分已修复**：面板增加 `loading` 三态和上传禁用；`requestVersion` 令旧 list 不能覆盖上传后的本地 asset。见 REV-017 的剩余竞态说明。 |
| REV-015 顶层 OpenAPI tag 缺 planning-media | **已修复**：`api/openapi.yaml` 顶层 `tags` 已补 `planning-media` 描述，paths 与生成边界一致。 |

## 3. Blocking finding

### REV-016：CreateBinding 缺失/非法 required 字段仍落 500，而契约要求 400

- **位置**：`backend/internal/planningmedia/application.go:601-604`；`backend/internal/platform/httpapi/planning_media.go:117-138,192-216`。
- **事实**：bind handler 的 `decodeStrictJSONBody` 只拒绝未知字段/trailing value，不检查 OpenAPI `CreateAssetBindingInput` 的 required/minimum 字段。请求 body `{}` 或省略 `generation`、`holder_kind`、`holder_id`、`purpose`、`expected_plan_revision`、`expected_asset_revision` 后，匿名输入仍进入 application。`CreateBinding` 对零值只返回裸 `fmt.Errorf("binding_input_invalid")`，该错误既不是 `planningmedia.ErrValidation`，也不被 `h.fail` 识别，随后 `c.Error` 由 envelope middleware 统一渲染为 **500 internal**。
- **此外**：`holder_kind` 的未知值、空 `holder_id` 等非法 union/required 语义也可能经过 application 后由 holder authorizer 变成 404，而非 400；这同样违反 S5 的 validation matrix 和 OpenAPI required/enum/minimum 契约。
- **影响**：客户端无法区分 malformed request 与 server fault；错误请求可能触发不必要的 DB/idempotency 路径，并破坏“所有 schema validation fail-closed 为 400”的 API 约定。
- **修复要求**：在 handler 使用 required-field/minimum/enum 校验，或让 `CreateBinding` 返回可被 `errors.Is(err, ErrValidation)` 识别的 typed validation error；handler `h.fail` 必须稳定映射为 `400 validation_failed`。至少覆盖 body `{}`、每个 required 字段缺失/0、未知 `holder_kind`、空 holder id 与 trailing/unknown JSON 的 route regression。不要将其降级为 409。

## 4. Important residual issue

### REV-017：list/upload 竞态在旧请求异常时仍可把 loading 永久留在 true

- **位置**：`frontend/src/planning/panels/PlanningMediaPanel.tsx:24-34,36-46`。
- **事实**：`load` 开始时递增 `requestVersion` 并设置 `loading=true`；`selectFile` 在上传前再次递增版本。若旧 list 尚未结束即进入 upload（例如浏览器恢复、自动触发或未来解除按钮禁用），list 的 `finally` 因版本过期不会清理 loading，而 upload `finally` 只清理 `busy`，因此 panel 可永久显示 loading。旧 list 的 catch 也未按版本 guard，可能覆盖上传成功 notice。
- **影响**：极端慢网络/未来交互扩展下，面板可能无法回到 loaded-empty/loaded-list，或以 stale error 覆盖较新的成功反馈。
- **建议**：将 list 请求状态改为 request-generation 对应的状态机；mutation 开始时显式结束/暂停当前 list loading，或在 mutation 完成后按最新 generation 重新 load；所有 success/error/finally 均只允许当前 generation 写状态。该项不改变本轮 blocking verdict，但应进入 QA 与 residual risk。

## 5. 其它整体核验结果

已确认当前源码具备以下性质，未发现新的 blocking/important（REV-016 除外）：

- `AccountScope` 覆盖 gallery、asset/rendition/binding/lease/read-pin/inventory 查询；cross-account 与不存在资源通过 holder/asset scope fail closed，object key 不进入公开 DTO、OpenAPI、DOM 或前端 URL。
- Holder proof 仍由 shootplanning 在同一事务内签发；planningmedia 不反查 core 表。plan/shot not-found 与 plan revision conflict 的 HTTP envelope 已按 404/409 映射；archive/completed lifecycle 仍按设计返回 409。
- 上传 spool 有界读取、magic/MIME/尺寸/像素/动画 WebP/PNG metadata 处理、immutable original/display 双 rendition、object/DB/ledger 故障清理及 orphan reconciliation seam 仍保持。
- upload/bind/release 的 canonical idempotency frame、single claim、replay 行为与 response-lifetime read pin/GC claim-delete-finalize 线性化未见回退；GC physical delete failure 保留 `gc_pending`，重试路径可继续清理剩余 rendition。
- `BatchShotAccessRefsInScope` 使用单个 scoped projection query、shot-id pushdown、稳定排序和 100 条总上界；core RunInputSnapshot 首次 callback 才投影，replay 不重复 media query/callback；projection unavailable 降级为空不阻断现场 capture/skip。
- `ReferenceSheet` 的 `onFail` callback 已稳定（`useCallback`），不会因父 render 重跑 effect；fetch 失败仅增加不可用提示，不阻断现场记录。参考素材继续 execution-only，不提供结构编辑。
- OpenAPI 顶层 tag、planning-media paths、Go/TypeScript generated schema 与 route composition 一致；未发现 AI/provider、知识库、匿名分享、通用 BlobStore 或 post-feature capability 泄漏。
- migration 0013 up/down order、avatar regression、generated check、full `make check`/frontend build/lint/diff-check 的通过证据与实现记录一致；本轮未重复全仓命令。

## 6. QA focus / residual risks

在 REV-016 修复后，QA 应优先补真实 HTTP 与边界证据：

1. bind route 的 required/enum/minimum/unknown/trailing JSON 全矩阵，确认 malformed request 永远 400 且不产生 binding/idempotency side effect。
2. multipart rights strict decoder：合法 snake_case、`permitted_uses`、未知字段、重复 JSON 值、MIME/size 边界，确认 400/413/415。
3. holder/account/lifecycle matrix：不存在/跨账号 plan/shot=404，旧 revision=409，archived/completed=设计规定 409；gallery/content/bind/release 一致。
4. projection 100/101 binding 边界、30 shot 上界、跨 plan/account、release 后首次 snapshot 与 replay query=0；display fetch 失败仍可 capture/skip。
5. GC partial-delete retry、duplicate active binding with a new key、read-pin/GC concurrency and deleted-inventory filtering。
6. 前端慢 list/upload 顺序、loading→loaded-empty/error、375/coarse pointer/键盘/200% zoom；修复或明确记录 REV-017 的 generation state semantics。

仍保持 pending、不可用本地 fixture 冒充的外部证据门：`stage-1-evidence-go`、`stage-2-evidence-go`、`production-shaped-rehearsal`。

## 7. Review-round limit

这是 round 3，已达到 owner 要求的每 feature 最多三轮 review。本报告之后不得自动启动 round 4；REV-017 等 non-blocking 项进入 residual risk。REV-016 已在 review 后做 owner-cap 窄修复，并由 `.codestable/features/2026-08-05-planning-reference-assets/planning-reference-assets-qa.md` 的独立 QA closure 验证：malformed bind 输入均在幂等/数据库/HolderProof 前返回可识别 `ErrValidation`，HTTP 为 400，生成物和全仓门禁通过。该 closure 不新增 review 轮次；若未来发现新的 blocking 问题，应交由 owner checkpoint 决策。

## 8. Post-review closure evidence

- REV-016 修复位置：`backend/internal/planningmedia/application.go`、`backend/internal/platform/httpapi/planning_media.go`。
- 独立 QA 证据：QA 报告 status=`passed`；`make generate-check`、`make check`、planningmedia/httpapi/shootplanning targeted tests、frontend planning-media tests/build/lint、`git diff --check` 全部通过。
- 当前无 unresolved blocking findings；REV-017、真实 HTTP multipart negative route fixture、projection boundary fixture、浏览器极端尺寸/辅助功能证据和 provider unavailable 均为非阻塞 residual，已转入 acceptance 报告。
