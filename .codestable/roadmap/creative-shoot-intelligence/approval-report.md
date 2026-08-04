---
doc_type: approval-report
unit: creative-shoot-intelligence
status: pending
reason: roadmap-and-upstream-gates
approvals:
  epic-split: approved
  roadmap-plan: pending
  upstream-evidence-go: pending
  operator-adr: pending
  platform-catalog-adr: pending
  live-provider: pending
  production-content-policy: pending
approval_groups:
  epic-split-2026-08-04:
    status: approved
    confirmation_id: chat-2026-08-04-split-creative-planning-intelligence
    decisions: [epic-split]
  g4-roadmap-start:
    status: pending
    confirmation_id: ""
    decisions: [roadmap-plan, upstream-evidence-go]
  intelligence-execution-prerequisites:
    status: pending
    confirmation_id: ""
    decisions: [operator-adr, platform-catalog-adr, live-provider, production-content-policy]
approval_evidence:
  upstream-evidence-go:
    path: ""
    sha256: ""
    gate_version: ""
created_at: 2026-08-04
updated_at: 2026-08-04
---

# 创作型拍摄 AI 与知识增强批准报告

## Decision History

- 2026-08-04：owner 同意把 AI/知识能力从首版拆为独立后置 epic；`epic-split` 已批准。
- 2026-08-04：拆分不等于批准后置范围、G4、operator 访问、platform catalog、live provider 费用或生产内容政策；这些 named decision 全部保持 pending。

## Decision Needed

### `roadmap-plan`

当前有意保持 `pending`。独立 review 可以先完成，但标准 `cs-epic` 没有“路线已确认、execution 仍 draft 等 G4”的中间状态，因此不在首版 evidence 就绪前请求 roadmap confirmation。到 G4 时，owner 用同一次确认原子决定 `roadmap-plan` 与 `upstream-evidence-go`；批准后同步把 frontmatter `draft→active` 并进入标准 child design batch。

### `upstream-evidence-go`

G4 启动 gate。只有首版 8 条完成、生产恢复通过、G1–G3 有真实 evidence 和 owner disposition，并已创建 durable `creative-shoot-intelligence` requirement 后才可决定。该 decision 与 `roadmap-plan` 由 `g4-roadmap-start` group 同一次原子确认；二者 approved 后 roadmap 才激活，任何 item 才能进入 child design。

同一次原子更新还必须写 `approval_evidence.upstream-evidence-go` 的 canonical bundle path、SHA-256 和 gate version；bundle 内容变化会使 G4 批准失效，不能只看旧的 approved 字符串。

### 后置独立 gates

- `operator-adr`：submission-bound operator 读取租户提交的 ADR-001 窄例外；
- `platform-catalog-adr`：物理隔离 platform published catalog、审核与 published media；
- `live-provider`：provider、区域、凭证、预算与调用副作用；
- `production-content-policy`：版权、来源、运营责任与生产开放政策。

这四项允许在 12 条 child design/design-review 期间形成完整决策材料，但必须在统一 design 确认之后、写入/授权 goal package 之前，由 `intelligence-execution-prerequisites` group 原子批准。任一 pending/rejected 时不得把 goal-state 置为 `ready-to-dispatch`。因此它们不依赖 goal driver 中途理解自定义 prose gate。

## Why Now

把后置能力独立建档，可以在不承诺立即开发的前提下完整设计冷门角色知识维护系统，并让首版证据成为真正的启动条件，而不是同一 DAG 中不可执行的注释。

## Context

路线依次交付项目研究包、后台研究履约、AI gap check、账号/平台知识、风格统计、文本 proposal、分镜、评测、账号导出、部署 retention/restore 和 hardening。唯一最小闭环是“明确缺口→后台补录→项目研究包→有引用 gap check”。

## Options

### Option A — 现在继续保持 draft/pending（当前推荐）

- 本轮只完成独立 review 和规划修订；
- `roadmap-plan`、`upstream-evidence-go` 与 `g4-roadmap-start` 全部保持 pending；
- 不触发标准 `ConfirmRoadmap`，不创建 child feature；
- G4 时再基于首版证据一次确认路线与启动。

### Option B — G4 时批准路线并启动

- 前置 requirement、首版 evidence 和 review 均有效；
- 原子批准 `g4-roadmap-start`、两项 named decision 并记录 confirmation id；
- roadmap `draft→active`，随后按标准流程完成全部 child design；
- goal package 仍等待四项 execution prerequisite 和统一 goal authorization。

### Option C — G4 时修改或拒绝

- 修改则保持 pending、更新 roadmap 并重跑独立 review；
- 拒绝则原子标记 group/decision rejected，保留 brainstorm 洞察但不启动执行路线。

## Recommendation

当前选择 Option A，即只把规划修到可 review，不请求确认。G4 evidence 和 requirement 就绪后再评估 Option B。这避免制造 CodeStable 无法恢复的“plan approved / roadmap draft”分裂状态。

## Risks And Tradeoffs

- 后台知识维护是持续运营能力，不是一次开发即可完成；必须衡量每个研究请求的成本和复用。
- operator 与 platform catalog 触及 ADR-001、隐私和版权，不能用 roadmap approval 代替专项批准。
- live provider 能力、价格和政策会变化，适配器应在 child design 时查询当期官方文档。
- 文本/图片生成置于治理和风格之后，上市速度更慢，但降低“模型不认识却先生成”的核心风险。

## Non-Automatic Actions

任何当前或未来批准都不自动：

- 认定首版 G1–G3 已通过；
- 授予 operator capability 或允许跨账号浏览；
- 接受版权/内容政策风险；
- 使用 provider 凭证、产生费用；
- 收集租户数据作为 eval corpus；
- 实现、commit、push、merge 或 deploy。

## After You Answer

- 当前：保持 draft/pending，无运行时状态变化。
- G4 Option B：先原子批准 `g4-roadmap-start` 并激活 roadmap；完成全部 child design 后，再准备四项 execution prerequisite 与标准 Goal execution authorization。
- 任一 rejected：记录原因和替代路径，不由 agent 自动绕过。
