---
doc_type: issue-fix
issue: 2026-10-16-generation-stuck-and-failure-state
status: confirmed
path: fast-track
fix_date: 2026-10-16
tags:
  - backend
  - frontend
  - creative-generation
  - llm-gateway
---

# 生成卡在「正在确认结果」与节点失败态修复记录

## 1. 问题描述

两个问题，owner 2026-10-16 一起提出：

1. 生成任务跑得久之后，节点一直卡在「正在确认结果」，永远不结束。
2. 生成失败后节点直接变成空节点，看不出失败；多版本节点也没有「这一版失败了」的状态，没法重试或切到别的版本继续用。

卡住的那条线上记录（当时截止为 30 分钟）：`creative_node_executions.state=reconciling`、`error=generation_verification_required`，绑定 `failures=5`、`verification_required=TRUE`；网关 `llm_requests.state=unknown`、`failure_class=recovery_requires_verification`，任务 `state=running`、`action=observe`、`failures=5`；`cancel_requested_at` 和 `closed_at` 都为空（没到 30 分钟截止）。

## 2. 根因

1. **网关把已知任务的查询失败当成受理未知**：拿到 task id 后的 observe 失败，和提交结果未知共用「连续 5 次就判核实」的上限。退避是 15、30、45、60 秒，所以供应商查询接口只要抖动约 2.5 分钟，任务就被判核实，之后再也不会自动推进。查询失败的原因也没有记日志。
2. **创作侧停驻后进了死状态**：网关判核实后，创作侧每轮都报错，攒满 5 次就停驻。停驻时调 `complete()`，但网关请求还没到终态，`ReleaseEmptyGenerationInTx` 返回 `ErrState`，收尾事务整体回滚。于是节点被标成 `reconciling`、绑定被标成 `verification_required=TRUE`。`claim` 和 `SweepDue` 都会跳过这种绑定，两层的 `Resume` 也没有任何入口调用。
3. **排队被算成失败**：供应商名额占满时 `AdvanceGeneration` 返回 `ErrRateLimited`，创作侧把它记为一次失败。RunningHub 的共享并发是 1，前一张跑超过 2.5 分钟，后面排队的那张就会被判失败。
4. **创作侧失败计数只增不减**：成功的一轮不清零，任务越久越容易攒满。
5. **前端不认失败**：后端失败后会通过 `latest_execution_id` 下发 `generation_state=failed` 和 `error`，但画布卡片只认 `queued / running / reconciling`，`failed` 被当成空闲，于是显示成空节点。面板把 `failed` 写成「生成未全部完成」。

## 3. 修复方案

### 后端

- 网关（`generation_dispatch.go`、`generation_runtime.go`）：已知 task id 的查询失败不再判核实，退避上限 1 分钟（`maxGenerationBackoff`），一直查到 24 小时恢复窗口用尽。5 次上限（`maxBlindFailures`）只约束受理未知的提交和 lookup。领取时失败计数用 `LEAST(failures+1,5)`，以符合表上的 CHECK 约束。查询失败记一条 `generation provider call failed` 警告日志（只含已脱敏的错误分类）。
- 创作侧（`creativegeneration/service.go`）：
  - 只数连续失败，成功一轮就清零。
  - 网关判核实时直接把节点收为失败（`generation_unverified`），不再空转 5 轮。
  - 名额占满（`ErrRateLimited`）算排队，不计入失败。
  - 过了截止且网关还没到终态时，直接把节点收为超时失败（`generation_timeout`）；供应商那边由网关继续跑完并核算。
  - 停驻时网关请求还没到终态，也照常收尾：`complete()` 容忍 `ReleaseEmptyGenerationInTx` 返回的 `ErrState`，已交接的作品按部分成功发布。收尾失败时不再写死状态，而是保持调度，下一轮再收。
  - 删除 `MarkGenerationVerificationInTx`，生成执行不再进入 `reconciling`。
- 失败原因（`creativecanvas/generation.go` 的 `GenerationFailure*`）：节点 `error` 收敛为 `generation_failed / generation_content_rejected / generation_invalid_input / generation_timeout / generation_unverified`。供应商额度、凭证、限流、部署不可用一律归为 `generation_failed`，不向创作者暴露（见 owner 2026-10-15「部署错误不暴露给用户」）。助手读画布时的 `generation_error` 同步拿到这些原因码。
- 图片生成截止从 30 分钟改为 10 分钟（`creativegeneration/aicodewith.go` 的 `imageDeadline`，owner 2026-10-16：「时间太长了，给10分钟足矣」）。截止包含供应商排队与路由切换。
- 存量修复（`claim`、`recovery.go`）：恢复扫描额外补投「绑定 `verification_required=TRUE`、执行仍是 `reconciling`」的旧记录，重新领取一次，由 `step` 收为失败。上线后约 15 秒内，线上那条卡住的执行会变成 `failed / generation_unverified`。

### 前端（owner 2026-10-16 拍板三条）

1. 重试用**当前**提示词与连线，等于再点一次生成，走生成面板同一条准入路径。
2. 主版本「跟随最新」时，卡片在旧内容上盖一层失败态（原因 + 重试）；版本条末尾多一个失败格，点其他版本切回内容，点失败格切回失败态。下游仍用旧主版本。已手动固定主版本时卡片不变，只在版本条标出失败格。
3. 只保留最近一次失败，下次生成开始就自动收起（失败格的判定就是「节点最近一次执行没有产出可用作品」）。

- `labelMeta.ts`：`generationFailure(status)` 判定失败（`failed`、过期，或成功但未采用）。主动停止（`cancelled + discarded`）不算失败。`generationFailureText` 给出各原因的文案。
- `GenerationFailure.tsx`：新增失败态遮罩。沿用生成中的毛玻璃，不做动画，只有状态点用红色；文本节点上排成一行贴底。
- `NodeVersionStrip.tsx`：新增失败格；看失败格时右侧显示「最近一次生成失败」，重试入口只放在卡片上，每张卡只有一个。
- `CanvasView.tsx`：按主版本模式决定是否显示失败态，版本条里的切换按这次执行记住；`retry` 选中节点并把带 `generate` 的请求交给面板。
- `WorkspacePage.tsx` / `NodeGenerationPanel.tsx`：聚焦请求增加 `generate`。面板解锁、目录到达后，能生成就按当前提示词生成一次，生成不了时只聚焦输入框，原因在面板里。面板状态 `failed` 改为「生成失败」，失败说明用统一文案。

## 4. 改动文件清单

- `backend/internal/platform/llmgateway/generation_dispatch.go`、`generation_runtime.go`
- `backend/internal/creativegeneration/service.go`、`recovery.go`
- `backend/internal/creativecanvas/generation.go`
- 测试：`llmgateway/generation_lifecycle_test.go`（已知任务持续重试、恢复窗口用尽才判核实）；`creativegeneration/service_test.go`（部分成功停驻即收尾、名额占满排队到截止才超时、供应商审核拒绝原因上节点、旧 reconciling 记录被补投并收为失败）
- `frontend/src/creative-canvas/editor/labelMeta.ts`、`GenerationFailure.tsx`（新增）、`NodeVersionStrip.tsx`、`CanvasView.tsx`、`WorkspacePage.tsx`、`NodeGenerationPanel.tsx`、`generation.ts`、`workspace.css`
- `frontend/scripts/creative-generation-failure.e2e.mjs`（新增，纯 mock，每个场景一个新的 browser context）

## 5. 验证结果

- `go test ./internal/creativegeneration/ ./internal/creativecanvas/ ./internal/platform/llmgateway/`：全部通过（三个包并行跑会把测试库连接打满，llmgateway 需单独跑）。
- `gofmt`、`golangci-lint`（三个包）：0 issues。
- `make check-frontend`：lint 只有原有 warning，build 通过，单测全部通过；改动文件按 `--single-quote --no-semi` 格式化（原有文件的既有格式问题未动）。
- `creative-generation-failure.e2e.mjs`：5 个场景全部通过（首次失败显示失败态并重试、面板状态与原因、跟随最新时盖住旧版本并可切回、固定主版本只标失败格、主动停止不算失败）。
- 回归有效性：把前端 7 个改动文件换回 HEAD 后，第一个场景就等不到失败态而失败；换回修复后通过。
- 同时通过：`creative-generation`、`creative-canvas-interaction`、`creative-node-label` e2e。

未验证：

- 真实供应商走查：没有实际触发 aicodewith / RunningHub 的长任务或查询抖动。
- 线上存量修复：远端库没有连，上线后需确认那条 `cwexec_af6ba933-…` 变为 `failed / generation_unverified`。它在网关侧的请求仍是 `verification_required`（费用待核实），这次不自动解除；截止已过，结果不会再采用。
- 文本节点生成失败后重试的真实后端链路（e2e 里是 mock）。
