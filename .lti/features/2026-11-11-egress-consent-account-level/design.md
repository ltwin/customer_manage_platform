# 账号级外发同意设计

## 设计上下文与关键决策

依据 [proposal.md](proposal.md)（2026-11-13 批准）。现状核对如下：

- 「发送前说明」只写进浏览器的 `localStorage`（`frontend/src/creative-canvas/editor/agentState.ts:noticeSeen`，版本 `egress-2026-10-2`），服务端不知道摄影师是否同意过。
- 前端每次发新消息前，先调 `POST /creative/conversations/{id}/egress-consents` 建一份会话级授权，固定写入 `account_library` 和四个数据类别中的三到四个，`selected_revision_ids` 是本条消息引用的修订；再把授权 ID 传给建运行（`agentState.ts:messageSteps`）。
- 后端所有门禁都汇总到 `runs.go:coversRun`（撤回、会话匹配、说明版本、数据类别、模式），共 17 处调用 `consents.go:readConsentForUpdate`（`FOR UPDATE`）。读取范围由 `context.go:directlyReadableInTx` 和 `heldReadable` 决定，它们有五条分支：被撤回授权列过的修订一律拒绝 → 授权清单 → 当前画布 → 本对话在触发消息之前的完整消息，或本运行自己的消息 → `account_library` 下的素材库。
- `creative_egress_consents.conversation_id` 本来就允许为空，没有外键（迁移 0044「Explicit account keys only; no foreign keys」）。
- 前端打包嵌入后端二进制（`backend/internal/platform/webui/dist`），两端同版本发布。

采用的方案：

1. **沿用 `creative_egress_consents` 存账号级同意**：`conversation_id` 为空的行就是账号级同意。另加一张每个账号一行的「同意状态」表，记录当前生效的是哪一行。好处是运行上的 `egress_consent_id`、`Consent` 结构和数据类别检查都不用改，存量记录天然只读保留。备选是新建一张独立表，那样 `runs.egress_consent_id` 的含义会分成两套，每个读取点都要区分新旧，所以没有采用。
2. **门禁按「账号当前同意」核对，运行上的同意 ID 只作为准入证据**。新增一个统一的门禁函数，替换 `coversRun` 加 `readConsentForUpdate` 的组合。门禁读账号当前同意，再核对运行准入时的说明版本仍是当前版本。这样撤回和版本升级走同一条规则（提案「同意失效」），修改素材库勾选也不会打断进行中的运行。
3. **读取分支删两条，保留三条**：删除「授权清单」和「被撤回授权列过即拒绝」两条分支，保留画布、本对话、素材库三条。删除清单分支不会让任何内容变得原来读不到、现在读得到：前端的授权清单就是本条消息的引用（`AgentPanel.tsx:approvedIDs`），而这些引用写在触发消息上，并且 `run_id` 等于本运行（`runs.go:357`）；插队和补充的消息也带 `run_id`（`waiting.go:appendInputBatchInTx`），所以本来就已经被「本运行的消息」分支覆盖。删除撤回分支属于行为放宽，提案已确认接受。
4. **读者用共享锁，写者用排他锁**。同意从会话级变成账号级之后，同一账号所有对话的派发都会读同一行。如果沿用 `FOR UPDATE`，这些派发会互相排队，所以读者改用 `FOR SHARE`。撤回和同意要更新状态行，必须等进行中的读事务提交，这正是验收标准里「谁先提交谁生效」需要的线性化。
5. **撤回仍然让压缩摘要失效**，范围从一个对话扩大到本账号所有对话。理由与 `consents.go:RevokeConsent` 的原规则相同：摘要是在已撤回的同意下外发的内容生成的，不带进以后的运行。说明版本升级不让摘要失效，与现状一致。

## 领域模型与业务规则

**外发同意**（glossary 新增）：账号对「发送前说明」某个版本的同意。它决定本账号的哪些类别的内容可以发给平台签约的模型服务商，以及助手能否检索素材库。

| 概念 | 身份 | 可变性 | 规则 |
|---|---|---|---|
| 同意记录 | `creative_egress_consents` 中 `conversation_id IS NULL` 的行，`ccec_<uuid>` | 写入后内容不变，只会被盖章 `superseded_at` 或 `revoked_at` | 数据类别固定为全部四类；`library_access=true` 映射为 `scope.mode=account_library`，`false` 映射为 `selected_revisions`（不带清单，即不允许读素材库，沿用该模式「空集不允许读库内容」的原语义） |
| 同意状态 | `creative_egress_consent_states`，每个账号一行 | `current_consent_id` 和 `revision` 会变 | `current_consent_id` 为空表示没有同意（从未同意或已撤回）。同一时刻至多一条当前同意 |
| 准入证据 | `creative_agent_runs.egress_consent_id` | 不变 | 建运行时写入账号当前同意的 ID，此后只用来读它的 `policy_version` |

**有效同意**：账号状态行存在，`current_consent_id` 不为空，并且该行的 `policy_version` 等于服务端当前的说明版本（`service.go:policyVersion`，本次升为 `egress-2026-11-1`）。

**门禁结论**（对外错误码沿用现有映射，`httpapi/creative_agent.go`）：

| 情形 | 错误 | 对外 |
|---|---|---|
| 没有状态行，或当前同意为空且从未撤回过 | `ErrEgressRequired` | 403 `creative_egress_required` |
| 当前同意为空，且最近一次是撤回 | `ErrConsentRevoked` | 409 `creative_egress_revoked` |
| 当前同意的版本不是当前版本 | `ErrEgressRequired` | 403 `creative_egress_required` |
| 已有运行准入时的版本不是当前版本（含本次上线前的会话级旧授权） | `ErrEgressRequired` | 运行以 `creative_egress_required` 结束 |
| 有效 | 返回当前同意（`Consent`） | — |

几点说明：

- 撤回后立刻重新同意：已有运行在下一次过门禁时看到新的当前同意，而且版本相同，于是继续执行。这符合「失效在派发时判断」的规则。如果运行在两次操作之间已经过了门禁，就按撤回结束。
- 修改素材库勾选：插入新行，旧行盖章 `superseded_at`。进行中的运行继续，读素材库的分支按当前同意判断：关闭后，素材库工具对模型返回拒绝说明，运行不结束；重新打开后，新运行才会注册素材库工具，因为工具集在运行配置时就固定了（`runtime.go:configure`）。
- 外部助手的读取（`read_access.go:externalReadAccess`）不经过这套同意，不变。

## 模块职责与协作

| 模块 | 职责 | 非职责 | 位置 |
|---|---|---|---|
| 同意领域 | 同意、撤回、读状态；有效同意门禁；撤回时让摘要失效 | 不判断具体内容是否可读 | `backend/internal/creativeagent/consents.go` |
| 读取范围 | 画布、本对话、素材库三条分支 | 不再查授权清单 | `backend/internal/creativeagent/context.go` |
| 各个门禁调用点 | 建运行、终态重试、插队、补充、等待恢复、派发复核、标题、压缩、工具读取统一调用门禁 | — | 见挂载点 |
| HTTP | 三个新接口；删除两个旧接口；建运行和重试不再接收 `egress_consent_id` | — | `backend/internal/platform/httpapi/creative_agent.go` |
| 前端 | 从服务端读同意状态；弹出说明并提交同意；「外发说明」面板；收到 `creative_egress_required` / `creative_egress_revoked` 时重新询问 | 不再存 `localStorage` | `frontend/src/creative-canvas/editor/`、`home/HomeComposer.tsx` |

主流程（新消息）：前端读同意状态（有缓存）→ 无效时弹出说明 → 摄影师同意，提交同意 → 建运行，不带同意 ID → `CreateRun` 在原来的同意锁位置调门禁 → 把当前同意 ID 写进运行 → worker 每次派发前在 `admitRunInTx` 再调一次门禁。

## 关键机制

### 有效同意门禁

```go
// consents.go
// effectiveConsentInTx 返回账号当前有效的同意。admitted 为空表示新建运行；
// 不为空时，还要求这个运行准入时的说明版本仍是当前版本。
func effectiveConsentInTx(ctx, tx, admitted string) (Consent, error) {
    st := SELECT current_consent_id, last_revoked_at FROM creative_egress_consent_states FOR SHARE
    no row                        -> ErrEgressRequired
    st.current == nil && revoked  -> ErrConsentRevoked
    st.current == nil             -> ErrEgressRequired
    c := 读 creative_egress_consents id=st.current（无锁；内容不可变，盖章不影响判断）
    c.PolicyVersion != policyVersion -> ErrEgressRequired
    if admitted != "" && 读 admitted 行的 policy_version != policyVersion -> ErrEgressRequired
    return c
}
```

- 锁顺序：状态行占据原来「同意」的位置（项目 → 槽位 → 网关分组与预算 → 会话 → 运行 → **同意状态** → 内容），与 `runs.go` 和 `execute.go:admitDispatchInTx` 注释里的全局顺序一致。
- 状态行由同意、撤回这两种写操作 `FOR UPDATE`，所以门禁读到的当前同意和派发意图在同一事务里线性化：
  - 撤回先提交：门禁看到 `current=nil`，供应商一次都没被调用；
  - 门禁先拿到共享锁：撤回要等派发意图提交之后才能执行，这一次请求照常发出。
- 不在同意行上加锁。同意行内容不可变，状态行已经串行化了所有变更，这样也就不会出现「`SELECT … WHERE superseded_at IS NULL FOR SHARE` 遇到并发更新、重新求值后读成空」的问题。

### 同意与撤回

| 操作 | 事务内步骤 | 幂等 |
|---|---|---|
| 同意 `{policy_version, library_access}` | 校验 `policy_version == 当前版本`，否则返回 409 `creative_egress_policy_changed` → `INSERT … ON CONFLICT DO NOTHING` 建状态行，再 `FOR UPDATE` 锁住 → 如果当前同意的版本和素材库选择都与请求相同，直接返回现状，不新增行 → 否则插入新同意行，旧当前行盖章 `superseded_at`，状态行指向新行，`revision+1` | creativeops 按 operation 回放；内容相同的不同 operation 也不新增行 |
| 撤回 | 状态行 `FOR UPDATE` → 当前同意为空时返回现状 200 → 否则当前行盖章 `revoked_at`，状态行 `current=nil`、`last_revoked_at=now`、`revision+1` → `UPDATE creative_agent_compactions … WHERE invalidated_at IS NULL`（本账号全部，原因 `consent_revoked`） | 同上 |

两个操作都要求 `agent_start` 能力，与现在的授予和撤回接口相同。

### 读取范围

`directlyReadableInTx` / `heldReadable` / `readableRevisionInTx` / `textureReadableInTx` 的参数从「同意 ID」改为「门禁返回的 `Consent`」：

```
readable(id) = 当前画布节点持有
            OR 本对话中 status=complete 且（序号在触发消息之前 OR run_id=本运行）的消息引用
            OR (library && consent.Scope.Mode == account_library && 未进回收站的资产当前修订)
```

- 素材库分支不再用 SQL 子查询查同意表，直接用 `consent.Scope.Mode`。
- 返回值里的 `revoked` 去掉，调用方相应简化：`textureReadableInTx` 和 `character_reference.go:characterAttachmentsInTx`。
- 所有读取仍然再过 `RequireUsable(display)`、数据类别和模型看图能力的检查（`context.go:readReference`、`runs.go:CreateRun` 的逐条引用循环），这部分不变。`CreateRun` 循环里「必须在授权清单中」的检查删除。

### 素材库关闭时的工具行为

`library_tools.go:requireLibrary` 先调门禁，失败照常结束运行（同意失效）；门禁通过但素材库已关闭时，返回 `refuseTool("素材库检索已关闭…")`，不再根据配置时的 `libraryEnabled` 来区分。`read_access.go:libraryAllowed` 相应简化。

**已经返回的结果**（2026-11-13 审计补充）：素材库工具的结果是内联文本（标题、标签、分组），没有修订引用，派发门禁的逐修订复核管不到它。处理分三处：

| 环节 | 素材库关闭时 |
|---|---|
| `context.go:bindMessages`（每次模型请求，覆盖本运行和冻结的历史） | `libraryResultTool(name, args)` 命中的工具结果换成 `libraryWithheld` 占位；`search_assets` 按调用参数区分，`source: builtin` 不受影响 |
| `context.go:readHistory`、`compaction.go:transcriptLine` | 同样换成占位（没有调用参数，`search_assets` 一律按账号素材库处理） |
| 已有压缩摘要 | `GrantAccountConsent` 在素材库从开到关时，同一事务里把本账号所有未失效摘要标记为 `consent_revoked`，与撤回相同 |

组装与派发之间关闭的竞态：`bindMessages` 记下这次请求是否带了素材库结果（`runtimeHandler.libraryBound`），派发事务里 `admitDispatchConsentInTx` 返回门禁读到的同意，若请求带了素材库结果而素材库已关，以 `ErrLibraryClosed` 拒绝这次派发（失败即关闭）。`ErrLibraryClosed` 包着 `ErrEgressRequired`，各处门禁照旧当缺少同意处理；只有 `failureCode` 把它单列成 `creative_egress_library_closed`，前端提示「素材库检索已关闭」，不再误报「发送前说明有更新」。`admitDispatchConsentInTx` 复用门禁在同一事务里读到的同意，不再另查一次。

已知边界：`read_asset` 读到的素材库内容挂在本运行的工具结果消息上，是本运行自己的会话引用，派发复核按「已有会话引用仍按原许可读取」放行；素材库关闭后它仍随之后的请求发出，不换成占位。

### 前端发送链

- 新增 `useEgressConsent(account)`，替代 `useEgressNotice`：
  - 读 `GET /creative/egress-consent` 并按账号缓存；
  - `confirm()` 在 `effective` 为真时直接放行，否则弹出新版说明（含素材库勾选，默认勾选）；
  - 摄影师同意后调用同意接口，成功才放行；
  - 本地说明版本与服务端 `policy_version` 不一致时，提示刷新页面，不提交同意。
- `messageSteps` 删除建授权那一步，建运行请求不带 `egress_consent_id`；重试请求（`AgentPanel.tsx` 第 1074 行附近）同样删除。前端的 200 条引用上限检查（`approvedIDs.length > 200`）随授权清单一起删除，消息引用数量由服务端现有上限把关。
- 建运行、插队、补充、重试返回 `creative_egress_required` 或 `creative_egress_revoked` 时，清掉缓存，重新弹出说明；摄影师同意后，用同一份草稿再发一次。插队队列和补充草稿保留，不丢消息。实现：`AgentPanel.tsx:perform` 包住所有发送入口（新消息、命令、插队、补充、重试、队列发出、重新发送），被拒后 `confirm()`，同意则以新操作号重发同一条链；首页 `HomeComposer.tsx:run` 同样处理。
- 「外发说明」放在助手面板菜单里，显示当前版本、同意时间、素材库开关（切换即提交同意）和「撤回同意」（二次确认，文案写明已发出的内容无法召回）。
- 同意写入以界面读到的 `revision` 为条件（`expected_revision`）：面板打开后别处撤回，旧面板切换素材库开关得到 `creative_revision_conflict`，面板重读并显示最新状态；弹窗里的同意遇到冲突时重读，已按同样选择同意则放行，否则请摄影师再点一次。
- `egressConsent.ts` 对结果未知的写入按意图保留原信封，同一意图再点时重放原操作；回放得到的回执之后重新读取状态。
- 说明版本升级、弹窗要「刷新页面」时，`confirm(keep)` 先调用方的 `keep`：首页把输入和参考图落盘到既有的首页恢复记录；画布把草稿存到 `creative-agent-reload-draft:<account>:<canvas>`，刷新后在没有待确认链时找回一次。

## 接口与数据契约

- 契约状态：**破坏**（删除两个接口；建运行和重试的请求删除 `egress_consent_id`）+ 扩展（新增三个接口，两张表各有变化）。
- 契约变更：见 [contracts.md](contracts.md)，包括：
  - glossary：外发同意；
  - data：新增账号外发同意；修订「运行中附带内容的外发授权」；
  - api：新增读取外发同意、同意发送前说明、撤回外发同意；删除授予会话外发授权、撤回外发授权；修改建运行和终态重试的外发字段。
- 兼容安排：前后端同一个二进制发布。发布时已打开的旧标签页会因为请求带 `egress_consent_id`（schema 是 `additionalProperties: false`）或拿不到账号同意而失败，前端提示刷新即可。发布时进行中的运行准入版本都是 `egress-2026-10-2`，在下一次派发时以 `creative_egress_required` 结束，与「旧说明的运行在下一次派发被拒」的现行规则相同，所以选在低峰期发布。
- 产品权威文档 `docs/product/creative-canvas-system/data-model.md`「外发同意的资源边界」整段改写为账号级规则。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/platform/store/migrations/0083_creative_egress_account_consent.up.sql`、`backend/internal/platform/store/migrations/0083_creative_egress_account_consent.down.sql` | 新增 | 状态表、`superseded_at` 列、账号级当前行的部分唯一索引；down 在已有账号级行时拒绝执行（序号按开发时最新编号顺延） |
| `backend/internal/creativeagent/consents.go` | 修改 | `GrantAccountConsent`、`RevokeAccountConsent`、`ReadAccountConsent`、`effectiveConsentInTx`；删除 `GrantConsent`、`RevokeConsent`、`maxApprovedRevisions` 和清单写入；`readConsentForUpdate` 删除或改为只读 |
| `backend/internal/creativeagent/service.go` | 修改 | `policyVersion` 升为 `egress-2026-11-1` |
| `backend/internal/creativeagent/runs.go` | 修改 | `CreateRun` 不再接收 `egress_consent_id`，改调门禁并删除清单检查；删除 `coversRun`；`planHistoryInTx` 接收 `Consent` |
| `backend/internal/creativeagent/retry.go` | 修改 | 重试不再接收 `egress_consent_id`，改调门禁 |
| `backend/internal/creativeagent/steer.go`、`backend/internal/creativeagent/waiting.go` | 修改 | 插队、补充、恢复和输入组装改调门禁 |
| `backend/internal/creativeagent/execute.go` | 修改 | `assemble`、`admitDispatchInTx` 改调门禁，并把 `Consent` 传给读取函数 |
| `backend/internal/creativeagent/scene_waiter.go:admitRunInTx` | 修改 | 门禁替换 `coversRun` |
| `backend/internal/creativeagent/context.go` | 修改 | 读取分支收敛；`approvedRevision` 删除；调用门禁 |
| `backend/internal/creativeagent/context_window.go`、`backend/internal/creativeagent/compaction.go`、`backend/internal/creativeagent/title.go`、`backend/internal/creativeagent/inputs.go`、`backend/internal/creativeagent/runtime.go`、`backend/internal/creativeagent/scene_tools.go`、`backend/internal/creativeagent/builtin_assets.go`、`backend/internal/creativeagent/character_reference.go` | 修改 | 改调门禁或改用 `Consent` 参数 |
| `backend/internal/creativeagent/library_tools.go`、`backend/internal/creativeagent/read_access.go` | 修改 | 素材库关闭时返回工具拒绝；`runReadAccess` 改调门禁 |
| `api/openapi.yaml`、`backend/internal/platform/httpapi/api.gen.go`、`frontend/src/api/schema.d.ts` | 修改 | 新增 `/creative/egress-consent` 等三个接口和 `CreativeEgressConsentState`；删除两个旧路径及 `CreativeEgressConsentGrantPayload`、`GrantCreativeEgressConsentRequest`、`RevokeCreativeEgressConsentRequest`；两个请求结构删除 `egress_consent_id` |
| `backend/internal/platform/httpapi/creative_agent.go` | 修改 | 手写路由注册三条新接口并移除两条旧接口（见 lesson creative-agent-routes-registered-by-hand）；新增错误码 `creative_egress_policy_changed` 的映射 |
| `frontend/src/creative-canvas/editor/EgressNotice.tsx` | 修改 | 新版说明文案、素材库勾选、服务端状态 |
| `frontend/src/creative-canvas/editor/agentState.ts` | 修改 | 删除建授权步骤和 `localStorage` 版本记录；新增同意状态读写 |
| `frontend/src/creative-canvas/editor/AgentPanel.tsx` | 修改 | 删除 `approvedIDs` 上限和重试的 `egress_consent_id`；新增「外发说明」菜单与面板；失效后重新询问 |
| `frontend/src/creative-canvas/home/HomeComposer.tsx` | 修改 | 改用服务端同意 |
| `frontend/src/creative-canvas/editor/agent.css` | 修改 | 外发说明面板样式 |
| `frontend/src/pages/privacy/PrivacyPage.tsx` | 修改 | 隐私说明页「创作助手会发送什么」：素材库检索改为可选、自动回复计费、在「外发说明」里撤回、已发出内容无法召回 |
| `docs/product/creative-canvas-system/data-model.md`、`docs/dev/creative-agent.md`、`docs/api/manifest.yaml` | 修改 | 资源边界、开发说明、接口清单 |

## 验证与交付

| 验收 / 不变量 | 方法 | 完成判据 |
|---|---|---|
| 同意、取消、换浏览器不再询问、首页、旧授权不算数（提案「同意与弹窗」） | PostgreSQL 集成测试 + 浏览器场景（`frontend/scripts/agent-send.e2e.mjs` 扩展） | 状态接口和弹窗行为与验收标准一致；清空 `localStorage` 后不再弹窗 |
| 绕过弹窗直接建运行、跨账号 | HTTP 冒烟（真实路由） | 403 `creative_egress_required` 且没有运行和预算预留；B 账号读不到 A 账号的状态。断言服务层文案，不只断言 404 |
| 读取范围不比原来宽 | 读取分支表驱动测试：原清单内的修订分别处在画布、本对话、本运行消息、素材库开关、他人账号、已删除、权利撤销几种状态 | 删除清单后，原来能读的结果不变，原来不能读的仍然不能读（撤回分支的放宽除外，单列断言） |
| 素材库开关 | 集成测试：关闭后新运行没有素材库工具；进行中运行的素材库工具返回拒绝、运行不结束；重新打开后新运行可以检索 | 符合验收标准 |
| 失效：撤回或版本升级 | 可控版本常量 + 两个事务的提交顺序测试（沿用 `TestAWithdrawnAuthorisationStopsTheDispatch` 的模式） | 撤回先提交时供应商调用次数为 0；派发先提交时恰好 1 次之后停止；运行结束原因分别是 `creative_egress_revoked` / `creative_egress_required` |
| 并发读者不互相阻塞 | 同账号两个对话同时派发 | 两个门禁事务能同时持有共享锁（测试里用 `pg_locks` 断言，不比较耗时） |
| 幂等 | 同一 operation 回放；内容相同的不同 operation | 不新增同意行，返回相同结果 |
| 插队、补充、重试和历史 | 补跑既有插队、补充、重试和历史回放测试 | 通过；重试请求不带同意 ID |
| 摘要失效 | 撤回后检查本账号所有对话的摘要 | 都标为 `consent_revoked` |

交付顺序：

1. 迁移，加上同意领域和门禁（后端可以独立验证）；
2. 替换各调用点、收敛读取分支，改写测试夹具（夹具统一用一个「给账号建有效同意」的 helper）；
3. OpenAPI 和生成物、路由；
4. 前端；
5. 文档。

门禁：后端跨包跑 `make check-go`，前端跑 `make check-frontend`，API 跑 `make generate` 和 `make generate-check`，合回 `develop` 跑 `make check-merge`。

回滚：down 迁移在已有账号级同意时拒绝执行。回滚靠前向修复；在此期间，可以把旧版本二进制配合保留下来的表结构部署回去：旧代码只读写会话级行，账号级行被忽略。观测：门禁拒绝按结论分类计数，日志只记录账号、运行和错误码。

## 未决事项

| 问题 | 影响 | 解决方式 | 最晚时点 |
|---|---|---|---|
| （已解决）提案验收标准「模型不支持看图 → 图片只以引用标识送达」和现状不符，现状是 `runs.go:CreateRun` 以 `ErrModelCapability` 拒绝建运行 | 读取与外发范围那组验收标准 | owner 于 2026-11-13 同意维持现状，提案已用 `lti revise` 修订为「建运行被拒，提示换能看图的模型」 | 已关闭 |
