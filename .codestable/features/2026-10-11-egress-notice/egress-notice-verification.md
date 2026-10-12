---
doc_type: feature-verification
feature: 2026-10-11-egress-notice
requirement: V6-029（首次外发说明与覆盖范围复用）
status: done
date: 2026-10-11
---

# 首次外发说明验证记录

设计见 [egress-notice-design.md](egress-notice-design.md)。

## 实现

- `editor/agentState.ts`：新增 `egressNoticeVersion`、`noticeSeen` 和 `rememberNotice`。键名是 `creative-egress-notice:{account}`；读不到本机存储时当作没看过。
- `editor/EgressNotice.tsx`（新）：`useEgressNotice(account)` 返回 `confirm()` 和 `dialog`，弹窗复用 `components/ConfirmDialog`。
- `editor/AgentPanel.tsx`：新消息组装好链之后先 `confirm()`，取消就直接退出。补充消息、重试步骤、「重新发送」和对话管理都不经过它。
- `home/HomeComposer.tsx`：`send()` 在保存首页记录、建项目之前先 `confirm()`。「空白画布」和「重试发送」「查询并恢复原保存」都不经过它。
- `pages/privacy/`：公开的隐私说明初版，`/privacy` 不需要登录。说明经营数据不会发给模型服务商，列出助手和生成各自发送的内容，以及由谁处理：平台签约的模型服务商，平台自动选择，调用记录留证；生成结果在服务商处公开保留至少 3 天。不列具体服务商（owner 2026-10-11）。弹窗里的「隐私说明」链接到这一页，新标签页打开。
- `useFocusTrap` / `ConfirmDialog`：正文里有链接后，初始焦点会先落到链接上。现在用 `data-autofocus` 把焦点固定在「取消」上；其他对话框都没有这个标记，行为不变。
- 后端只新增测试 `TestAConsentUnderAnEarlierNoticeDoesNotStartARun`（`runs_test.go`）：说明版本较早的授权建运行时返回 `ErrEgressRequired`，当前版本的授权可以正常启动。

## 实现偏差

- 弹窗没有新写组件，直接复用 `ConfirmDialog`。默认焦点因此落在「取消」上，而不是设计初稿写的主按钮。这个默认值更稳妥：按 Enter 发送后再按一次 Enter 只会关掉说明，不会误发。设计 §2.2 已同步。

## 门禁

- `make check-frontend`：lint、构建、单测全部通过。
- `make check-go PKG=./internal/creativeagent/...`：golangci-lint 0 issues，测试通过。
- 单测 `creative-agent.test.ts` 12/12，其中新增两条：
  - 按账号和版本记住说明；本机存储抛错或缺失时当作没看过，写入失败不打断发送。
  - 读取后端 `service.go` 的 `policyVersion`，断言它等于前端的 `egressNoticeVersion`。
- e2e：
  - `creative-home.e2e.mjs` 20/20，新增 029-AC-01、029-AC-03 和 029-privacy 三条；其余用例默认已看过说明。
  - `creative-projects.e2e.mjs` 4/4。
  - `creative-agent.e2e.mjs` 通过（`CREATIVE_AGENT_URL=http://localhost:5173`），夹具页默认已看过说明，`?notice=0` 时从没看过开始。
  - `creative-agent-blocks.e2e.mjs` 通过。
- 故意改坏后都能被测试抓到，改完已恢复：
  - 首页去掉说明：029-AC-01 失败，等不到弹窗。
  - 确认后不写本机记录：029-AC-01 失败（记录为 null），029-AC-03 也失败（版本还是旧的）。
  - 画布面板去掉说明：面板 e2e 失败。
  - 把说明挪进 `perform`，导致重放也弹窗：面板 e2e 失败，「重新发送」卡在弹窗上。
  - 后端不再检查说明版本：新增的后端测试失败。

## 验收对应

| AC | 证据 |
|---|---|
| AC-01 首次说明：会发给平台签约的模型服务商（详见隐私说明，不列具体服务商），说明用途和数据范围；拒绝时保留草稿，人工功能照常；不展示具体服务商 | e2e 029-AC-01，首页：<br>① 第一次按 Enter 弹出「发送前说明」，文案依次是服务商、用途、数据范围和「不发送未引用的素材」，此时没有 `POST /projects`。「隐私说明」是指向 `/privacy` 的链接（`target=_blank`），初始焦点在「取消」上。<br>② 点「取消」：文字保留，焦点回到输入框，本机没有记录。<br>③ 按 Esc 同样只是关闭。<br>④ 取消之后「空白画布」照常建项目进入画布，不弹说明。<br>⑤ 点「知道了，发送」后进入画布、面板打开，本机记下当前版本；之后再发不再弹。<br>面板 e2e：新消息先弹窗，取消时不发任何请求、草稿保留；确认后照常发送。弹窗里没有任何服务商名称。e2e 029-privacy：390 宽下打开 `/privacy`，没有发出任何 API 请求，「谁会处理这些内容」写的是平台签约的模型服务商，正文不出现 DeepSeek、Krill、AiCodeWith、OpenAI，有「至少保留 3 天」，页面不横向滚动 |
| AC-02 相同范围内复用；换模型或服务商路由不影响 | 每条消息发送时都按本条范围自动建授权，同一会话后续发送不再询问（面板和首页 e2e：确认一次后再发都不弹）。授权记的是平台，不记具体服务商（`TestAGrantNamesNoVendor`）；`coversRun` 不检查模型，所以换模型不影响 |
| AC-03 新增范围或说明版本变化时重新询问；撤销阻止后续发送 | e2e 029-AC-03：本机记的是旧版本时再次弹窗，确认后更新为当前版本。说明列出了助手当前会发送的全部数据类型，新增类型时必须升级版本（design §0）。服务端：旧版本授权不能建运行（`TestAConsentUnderAnEarlierNoticeDoesNotStartARun`）。撤销：`TestRuntimeRechecksConsentBetweenTurns`、`TestARevokedConsentStopsNaming`，按 owner 拍板不做界面入口 |
| AC-04 后端逐次检查，前端记忆不能扩大授权 | 本机记录只决定弹不弹窗，每次发送仍然建本条范围的授权；`coversRun` 在建运行、派发、补充、重试时核对会话、撤销、说明版本和数据类型（`TestTheAuthorisationMustBeForThisConversation`、`TestImageReferenceSurvivesHistoryAndNeedsRenewedConsent`、`TestCTextOnlyConsentDoesNotGrantLibraryAccess`、上面的版本测试） |

## 浏览器证据

截图都已看过，存放在 `/tmp/creative-home-qa/`：

- `egress-notice.png`：首页第一次发送时的弹窗，三段说明完整，「隐私说明」显示为链接，「取消」带焦点环。
- `privacy-390.png`：隐私说明页，390 宽，整页截图，中文句子之间没有多余空格。

## 未验证

- 隐私说明是初版，文案还没有经过法务审阅（owner 2026-10-11：先写一版，后续再改）。
- 画布内面板的弹窗只在 QA 夹具页里验证过，没有在完整画布页里截图。弹窗挂在 `.cc-workspace` 上，和 `StudioDialog` 的挂法相同。
