# Tasks

## 1. 迁移与同意领域

- [x] 1.1 迁移 `0083_creative_egress_account_consent`（up/down）：新增 `creative_egress_consent_states`（`account_id` PK、`current_consent_id`、`last_revoked_at`、`revision`），`creative_egress_consents` 加 `superseded_at` 列和账号级当前行的部分唯一索引；down 在已有账号级行时拒绝执行。迁移测试覆盖 up → down → up 往返，以及有账号级行时 down 报错
- [x] 1.2 `consents.go` 新增 `GrantAccountConsent` / `RevokeAccountConsent` / `ReadAccountConsent`，`policyVersion` 升为 `egress-2026-11-1`。先写集成测试：版本不符返回 `creative_egress_policy_changed`；内容相同不新增行；修改素材库勾选时插入新行并给旧行盖 `superseded_at`；撤回让本账号所有对话的摘要以 `consent_revoked` 失效；同一 operation 回放结果相同；账号 B 读不到也改不了账号 A 的状态
- [x] 1.3 `effectiveConsentInTx`：按 design「门禁结论」表的五种情形写表驱动测试（无状态行、撤回、从未同意、当前同意旧版本、准入版本旧），并用 `pg_locks` 断言同账号两个门禁事务能同时持有共享锁

## 2. 门禁替换与读取收敛

- [x] 2.1 测试夹具新增「给账号建有效同意」helper；先写读取分支表驱动测试（红）：原授权清单内的修订分别处在画布、本对话、本运行消息、素材库开关、他人账号、已删除、权利撤销几种状态，断言删清单后结果不变，撤回分支的放宽单列断言
- [x] 2.2 `context.go` 读取分支收敛为画布、本对话、素材库三条，参数改为 `Consent`，删除 `approvedRevision` 和返回值 `revoked`；同步 `character_reference.go`、`builtin_assets.go`、`scene_tools.go` 的调用；2.1 的测试转绿
- [x] 2.3 `runs.go` / `retry.go`：`CreateRun` 和终态重试不再接收 `egress_consent_id`，改调门禁，把当前同意 ID 写进运行；删除清单检查和 `coversRun`；`planHistoryInTx` 改收 `Consent`；handler 暂时忽略请求里的该字段。补测试：没有有效同意时建运行和重试被拒、无预算预留；模型不能看图时附图建运行仍以 `ErrModelCapability` 被拒
- [x] 2.4 `steer.go`、`waiting.go`、`execute.go`（`assemble`、`admitDispatchInTx`）、`scene_waiter.go:admitRunInTx`、`context_window.go`、`compaction.go`、`title.go`、`inputs.go`、`runtime.go` 全部改调门禁。沿用 `TestAWithdrawnAuthorisationStopsTheDispatch` 的模式写提交顺序测试：撤回或版本升级先提交时供应商调用 0 次；派发先提交时恰好 1 次后停止；结束原因分别是 `creative_egress_revoked` / `creative_egress_required`
- [x] 2.5 `library_tools.go` / `read_access.go`：门禁通过但素材库关闭时返回 `refuseTool`，运行不结束；`runReadAccess` 改调门禁。集成测试：关闭后新运行没有素材库工具，进行中运行的素材库工具被拒，重新打开后新运行可以检索；外部助手读取行为不变
- [x] 2.6 改写 `creativeagent` 里依赖逐条授权构造数据的既有测试，补跑插队、补充、重试和历史回放测试；`make check-go PKG=./backend/internal/creativeagent/...` 通过

## 3. 契约与路由

- [x] 3.1 `api/openapi.yaml`：新增 `GET/POST /creative/egress-consent`、`POST /creative/egress-consent/revoke` 和 `CreativeEgressConsentState`；删除两个旧路径和对应 schema；建运行和重试请求删除 `egress_consent_id`；`make generate` 并提交 `api.gen.go`、`schema.d.ts`
- [x] 3.2 `httpapi/creative_agent.go`：手写注册三条新路由、移除两条旧路由，新增 `creative_egress_policy_changed` 映射；删除 `GrantConsent`、`RevokeConsent`、`readConsentForUpdate`、`maxApprovedRevisions`。HTTP 冒烟走真实路由：绕过弹窗建运行得到 403 和服务层文案，跨账号隔离，新接口成功状态（不只断言 404）；`make check-go` 通过

## 4. 前端

- [x] 4.1 新增 `useEgressConsent` 替代 `useEgressNotice`，`EgressNotice.tsx` 改为新版说明（自动回复计费条款、素材库勾选默认勾选）；`agentState.ts` 删除建授权步骤和 `localStorage` 版本记录，本地与服务端版本不一致时提示刷新；单测覆盖已同意直接放行、未同意弹窗、取消保留草稿
- [x] 4.2 `AgentPanel.tsx` 删除 `approvedIDs` 200 条上限和重试的 `egress_consent_id`；建运行、插队、补充、重试收到 `creative_egress_required` / `creative_egress_revoked` 时清缓存、重新询问，同意后用原草稿重发，插队队列和补充草稿不丢；`HomeComposer.tsx` 改用服务端同意
- [x] 4.3 助手面板菜单新增「外发说明」面板（版本、同意时间、素材库开关、二次确认的撤回）及 `agent.css` 样式；扩展 `frontend/scripts/agent-send.e2e.mjs`：首次发送弹窗、取消、清空 `localStorage` 后不再弹窗、撤回后重新弹窗；`make check-frontend` 通过

## 5. 文档

- [x] 5.1 `PrivacyPage.tsx`「创作助手会发送什么」改为：素材库检索可选、后台任务完成后可能自动回复并计费、可在「外发说明」里撤回、已发出内容无法召回；`docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」改写为账号级规则；`docs/dev/creative-agent.md` 更新门禁和读取范围；`docs/api/manifest.yaml` 增删接口条目
