# plan-share-collaboration · S8 全矩阵验证证据

- feature: `2026-08-05-plan-share-collaboration`
- 日期: 2026-08-14
- 工作树: `.worktrees/creative-shoot-planning`
- 分支: `feat/creative-shoot-planning`
- 基线 commit: `be182a6f58333fc10798505bb94858e07eec58f4`（ITEM-4）
- stage-1 / stage-2: **仍未标 passed**（本步不改）

## 1. 门禁结果

| 命令 | 结果 | 说明 |
|---|---|---|
| `make generate-check` | **通过**（exit 0） | S8 前后各跑一次，无 OpenAPI 生成物漂移 |
| `make check` | **通过**（exit 0） | 第三次完整跑通；见下方过程记录 |

### `make check` 过程记录（可复现）

1. **第一次**：后端 `go test -p=1 ./...` 全绿；前端 `test:plan-share` / `planning-prototype-v2` 绿；在 `./scripts/test-auth-legacy-cutover.sh` 失败。
   - 根因：`TestAuthLegacyCutoverHarness` 仍把 tip 钉在 `fullVersion == 18`，ITEM-5 已加到 `0024`，报告 `limiter_schema_rollback:false`，脚本断言要求 `true`。
   - 修复：`auth_legacy_cutover_harness_test.go` 改为 `fullVersion == 24 && versionBefore == 12`。
2. **第二次**：Testcontainers 偶发 `port "5432/tcp" not found`（attention.md 已知），无关包失败。
3. **失败包定向重跑**：`customer` / `dashboard` / `order` / `idempotency` / `store` / `reminder` / `reminder/digest` 全部 **ok**。
4. **第三次完整 `make check`**：exit 0（含 auth-legacy、security-catalog、v1-ops、generate-check）。

补充定向（第三次前后）：

```bash
cd backend && go test -p=1 ./internal/planshare/ ./internal/planningmedia/ \
  ./internal/platform/httpapi/ ./internal/platform/store/ ./cmd/server/ \
  -count=1 -parallel=1
# 以及新增 fixture 过滤跑：
# TestSharePolicyExpiryQuoteStaleIndependent
# TestAnonymousProjectionArchiveIndependent404
# TestBearerFeedbackPageAPIAllowlistGolden
# TestSecurityAttemptBudgetPreviousDigestGraceContinuesWindow
# TestValidateCandidatesPreviousDayGraceWindow
```

前端（被 `make check` 覆盖）：

- `npm run test:plan-share` → 9 pass
- `node --test frontend/scripts/planning-prototype-v2.test.mjs` → 6 pass
- `npm run test:auth` → 11 pass

## 2. A1–A24 证据映射

图例：`covered` = 关键路径有可复现断言；`partial` = 有证据但 design 子句未穷尽；`missing` = 无可观察证据。

| ID | 状态 | 命令 / 测试名 | 文件 |
|---|---|---|---|
| A1 | covered | `TestBearerShareIssueRotateRevokeAndPolicy`（issue/replay）；`TestShareTokenCommitmentRoundTripAndConstantTimeMiss`；`TestResolverReturnsSealedCapabilityWithoutAccountScope`；`TestAnonymousShareRouteDepsOmitAccountScope` | `planshare/bearer_integration_test.go`；`token_test.go`；`scope_guard_test.go`；`httpapi/anonymous_share_deps_test.go` |
| A2 | covered | 同上 Bearer issue/rotate/revoke；quoted default TTL / `expiry_quote_expired`；**S8 补** `TestSharePolicyExpiryQuoteStaleIndependent` | `bearer_integration_test.go`；`policy_test.go` |
| A3 | partial | full 无资格 409；window clamp（past/normal/far）；unlink/relink epoch；**缺** issue↔cancel/delete 双连接锁序 fixture | `bearer_integration_test.go`；`policy_test.go` |
| A4 | partial | unlink 后 full 统一 404；relink 新 epoch 再签发；**缺** cancel/delete 与 GET 双连接线性化独立 fixture | `anonymous_integration_test.go`；`bearer_integration_test.go` |
| A5 | covered | proposal 不查 shots/offers/orders/customers；DTO 禁字段；前端 proposal DOM 无 shots/assignment | `TestAnonymousShareProjectionUniform404AndBoundaries`；`TestProposalDTOExcludesFullOnlyFields`；`plan-share.test.ts` |
| A6 | covered | full projection 含 shots/opportunities；禁 notes/price | 同上 + `token_test.go` |
| A7 | partial | scale/window 经匿名投影与 policy clamp；business query=0 由 proposal probe 覆盖；filled/null 组合未拆独立矩阵名 | `anonymous_integration_test.go`；`policy_test.go` |
| A8 | covered | `TestAnonymousConcurrentGETIdempotent`；content 并发首 GET 单 ref | `anonymous_integration_test.go`；`anonymous_content_integration_test.go` |
| A9 | partial | 错 token / revoke / expire / **S8 补** archive 投影独立 404；content archive 404；**缺** ≥500 样本 p95≤25% 等时机 | `anonymous_integration_test.go`；`anonymous_content_integration_test.go` |
| A10 | covered | `TestSensitiveRequestLabelRedactsShareTokenAndAssetRef`；`TestMiddlewareLogsSanitizeShareTokenCanary`；匿名 HTTP log canary | `httpapi/middleware_path_test.go`；`TestAnonymousHTTPHeadersAndPathCanary` |
| A11 | covered | Cache-Control / Referrer-Policy / nosniff / CSP；`credentials:'omit'`；CSP `default-src 'none'` | `TestAnonymousHTTPHeadersAndPathCanary`；`plan-share.test.ts` |
| A12 | covered | feedback 幂等 / 异 body / 旧 revision / HTML / 超限 / 429 Retry-After | `TestFeedbackS5ProposalFullIdempotencyDisposition` |
| A13 | covered | proposal Shot HTTP 404；full plan/Shot feedback；Bearer list deep-link | 同上 |
| A14 | covered | adopt 不改 core shot；disposition revision | 同上 |
| A15 | partial | readiness claim + lead snapshot（item 有值 / platform default）；**缺** 账号级 lead override fixture（实现仅 platform default） | `TestAssignmentS6ClaimRevokeConcurrencyGuardWiring`；`assignment.go#resolveLeadSnapshot` |
| A16 | covered | on-site offer 并发 claim；lead 字段空路径 | `assignment_integration_test.go`（`AssignmentTargetOnSiteSupport`） |
| A17 | covered | 双客户并发 claim 一胜一 409；reclaim 新 ID/rev1；receipt commitment | 同上 |
| A18 | covered | WebCrypto 不可用禁用；`CommitClaimReceipt` 只存 hash；UI 无弱 fallback | `plan-share.test.ts`；`TestClaimReceiptCommitmentOnlyHash` |
| A19 | covered | self-revoke rev1→2；rotate 后原 receipt；re-claim | `assignment_integration_test.go` |
| A20 | partial | photographer revoke；remove_readiness 在 active 时 409；planning-share-v1 接线；**缺** claim↔remove 双连接两种终局、archive HTTP exact effects、账号 lead override | `assignment_integration_test.go`；`shootplanning` archive wiring |
| A21 | covered | SharedAssetAccessRef 矩阵、并发 GET、跨 token、release/archive | `TestAnonymousSharedMediaContentMatrix` 等 |
| A22 | partial | Origin 门禁代码路径（mutation 要求 Origin==PublicBaseURL）；跨实例 counter；**S8 补** previous digest grace / 午夜后续窗；canonical 特殊字符；**缺** 完整 23:59/00:00 双实例 rate 矩阵、admission/ledger 双连接 exact 全称 | `plan_share_anonymous.go`；`security_attempt_budget_test.go`；`securitybudget/budget_test.go`；`canonical_anonymous_test.go` |
| A23 | partial | sealed scope / depguard；reciprocal FK / populated down / writer-first；activation→revoke；**缺** 部分双实例 fence 与跨 tx 负向穷尽 | `scope_guard_test.go`；`planning_share_migration_contract_test.go`；assignment/feedback 集成 |
| A24 | partial | 工作台协作区 / proposal·full 文案；feedback/assignment 分页；源码级 1600/1280/375/coarse/200%/键盘/读屏契约；**缺真实浏览器截图**（runner 明确不伪造） | `plan-share.test.ts`；`planning-prototype-v2.test.mjs`；`ShareCollaborationPanel.tsx` |

### §3.2 反向核对

| 项 | 结果 | 证据 |
|---|---|---|
| 匿名 DTO/DOM/query 无 price/cost/labor/payment/customer recipient | 通过 | proposal leak 扫描；`plan-share.test.ts`；OpenAPI share 路径无 recipient |
| 无 AI/OCR/第三方 analytics | 通过 | share 面与 `frontend/package.json` 扫描无 openai/ocr/mixpanel/gtag 等 |
| proposal 无 Shot/assignment 结构 | 通过 | query probe + 前端源断言 |
| token/receipt 不进 DB/ledger/storage/log | 通过 | commitment-only 测试 + middleware canary + 前端禁 Web Storage |
| assignment 不因 hint/token lifecycle 自动创建删除；本 feature 不写 reminder 行 | 通过（抽查） | claim 显式 API；无 planshare→reminder 写路径 |

## 3. 清洁度扫描

扫描范围：`backend/internal/planshare/`、`httpapi/plan_share*.go`、`planningmedia/content_permit.go`、`share_display.go`、`frontend/src/planning/share/`、相关 OpenAPI share 面。

| 检查 | 结果 |
|---|---|
| placeholder / 501 / NotImplemented | **无**业务 placeholder；仅 CSS `.share-mood-placeholder`（空图位，非 stub handler） |
| TODO / FIXME | **无** |
| 手写重复 DTO | **无**；`api.ts` 引用 `schema.d.ts`，`plan-share.test.ts` 断言禁手写 Input interface |
| 匿名 DTO/SQL/DOM 出现 price/cost/labor/customer recipient | **无**（匿名面）；OpenAPI 其他 CRM 订单 `price` 字段属既有业务，非 share 投影 |
| AI / OCR / 第三方 analytics | **无** |
| raw token/receipt 进 log/storage | canary 测试仍绿；前端禁 localStorage/sessionStorage/console |
| 文案「用户」 | share UI **无**；`plan-share.test.ts` 断言禁「用户」 |

## 4. S8 期间补的便宜 fixture

| 残差 | 测试 | 状态 |
|---|---|---|
| S2 `expiry_quote_stale` | `TestSharePolicyExpiryQuoteStaleIndependent` | 已补且绿 |
| S3 archive 投影独立 404 | `TestAnonymousProjectionArchiveIndependent404` | 已补且绿 |
| S3 IP digest previous-day grace | `TestValidateCandidatesPreviousDayGraceWindow` + `TestSecurityAttemptBudgetPreviousDigestGraceContinuesWindow` | 已补且绿 |
| S5 Bearer feedback HTTP allowlist golden | `TestBearerFeedbackPageAPIAllowlistGolden` | 已补且绿（DTO 映射层 golden；非全链路 Bearer e2e） |
| auth-legacy tip 钉死 | `fullVersion == 24` | 已修且脚本绿 |

## 5. Residual（不阻塞本条里程碑 commit；未用空话宣称通过）

以下 **未** 用空话宣称通过；`make check` 已绿，记入后续 hardening / 浏览器验收：

1. **A3/A4**：issue↔cancel/delete 双连接锁序 / GET 线性化独立 fixture
2. **A9**：≥500 样本 p95 等时机（不宣称互联网绝对等时）
3. **A15/A20**：账号级 lead override；claim↔remove_readiness 双连接两种终局；archive HTTP exact effects
4. **A21/S4**：content open vs GC finalize 双连接（现有 release/archive 与 planningmedia lifecycle 分测，未合双连接）
5. **A22**：午夜 23:59 / 00:00 双实例 rate 全矩阵、admission/ledger 双连接 exact 穷尽
6. **A24/S7**：真实浏览器 1600/1280/375/coarse/200%/键盘/读屏**截图**（源码 runner 已有；**禁止伪造**）
7. **stage-1 / stage-2**：仍 pending；本步不改
8. 本地 dirty PG：**未** migrate force

## 6. Checklist 步骤状态（本步）

| Step | status | 依据 |
|---|---|---|
| S1 | done | canary / sealed scope / PG counter / schema 边界有测试证据 |
| S2 | pending | exit_signal 要求 cancel/delete 双连接矩阵；仍 residual |
| S3 | done | 投影/404/archive 独立 fixture + query probe |
| S4 | pending | content↔GC 双连接 residual |
| S5 | done | feedback 矩阵 + Bearer allowlist golden |
| S6 | pending | claim↔remove 双连接 / archive HTTP exact / 账号 lead residual |
| S7 | pending | 真实浏览器截图缺失（源码 runner 不能代替） |
| S8 | done | `make check` 绿；A1–A24 均有可观察映射；清洁度通过；残差已记录 |

## 7. 本步改动文件

- `backend/internal/planshare/policy_test.go`
- `backend/internal/planshare/anonymous_integration_test.go`
- `backend/internal/platform/securitybudget/budget_test.go`
- `backend/internal/platform/store/security_attempt_budget_test.go`
- `backend/internal/platform/store/auth_legacy_cutover_harness_test.go`
- `backend/internal/platform/httpapi/plan_share_feedback_allowlist_test.go`（新）
- `.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-s8-evidence.md`（本文件）
- `.codestable/features/2026-08-05-plan-share-collaboration/plan-share-collaboration-checklist.yaml`（步骤 status）
- `.codestable/work/epic-creative-shoot-planning.md`（ITEM-5 备注）
