---
doc_type: decision
category: convention
date: 2026-07-07
slug: lint-gate-pre-commit
status: active
area: fullstack
tags: [lint, pre-commit, git-hooks, quality-gate, ci]
---

# Lint 检查门禁放入 git pre-commit hook

## 背景

编码规范已两次拍板（Go：2026-07-06 Uber Style Guide；前端：2026-07-07 工具链优先）。规范的三层执行结构中，约定层（attention.md / CLAUDE.md / AGENTS.md / GEMINI.md 入口）与流程层（review 按 checklist）已闭环，但两者对"任意模型 / agent 都遵守"只是劝导，唯一模型无关的硬保证是机器层卡点。截至本决策，机器层工具配置尚未落地（归 platform-skeleton），卡点接入方式未拍板。

## 决定

- lint 检查作为**提交门禁**接入 **git pre-commit hook**：不过 lint 的代码不允许进入提交。
- 统一命令入口：hook 只调用 `make lint`（内部分派 golangci-lint / tsc --noEmit / eslint / prettier --check），门禁逻辑不与具体工具耦合；后续工具增减只改 Makefile。
- hook 实现用**仓库内脚本 + `git config core.hooksPath`**（如 `scripts/githooks/`），由 `make setup`（或等价初始化命令）一次性启用；不引入 husky / lefthook 等 hook 管理器——单人仓库不预支依赖。
- **禁止任何 agent 使用 `git commit --no-verify` 绕过门禁**；确因环境损坏需绕过时必须由 owner 人工执行并说明原因。
- 落地时机：platform-skeleton feature（该条目本含 "build/test/lint 命令基线"），本决策计入其验收标准：① `make lint` 可用；② pre-commit hook 已接入并演示拦截一次坏提交。
- CI 卡点本决策不预支：当前无远程 CI 流水线；待 CI 出现时在其上复用同一个 `make lint` 入口，届时无需新决策。

## 理由

- pre-commit 是唯一在"代码进入版本库之前"生效、且与写码主体（Claude / Codex / Gemini / 人）完全无关的拦截点，恰好补齐三层结构中缺失的硬保证层。
- 项目约定"一个 feature 一个提交、提交需人工同意"（attention.md），提交频率低，pre-commit 全量 lint 的耗时可接受，无需 lint-staged 类增量优化。
- hook 只调 `make lint` 保证本地门禁、未来 CI、agent 手动自查三处口径同源，不会漂移。

## 考虑过的替代方案

- **只靠 CI 卡点**：当前无 CI；且 CI 拦截发生在提交之后，坏提交已入历史，与"review 精准对应干净提交"的约定冲突。
- **husky / lefthook**：功能足够但为单人 monorepo 引入额外依赖与安装环节，`core.hooksPath` + 仓库脚本零依赖等效。
- **lint-staged 增量检查**：提交频率低、全量可承受，属预支复杂度。

## 后果

- platform-skeleton 验收标准新增两条（见"决定"）。
- attention.md 编码规范节补充门禁条目与 `--no-verify` 禁令，任何走 CodeStable 流程的 agent 启动即读到。
- 工具配置落地前（platform-skeleton 之前）本门禁不存在，规范执行仍依赖 review 层兜底——此窗口期与 Go/前端规范决策中"工具项暂记 N/A"的口径一致。

## 相关文档

- `.codestable/compound/2026-07-06-decision-go-uber-style-guide.md`
- `.codestable/compound/2026-07-07-decision-frontend-toolchain-first-standard.md`
- `.codestable/attention.md` —— 编码规范入口（模型无关）
- `CLAUDE.md` —— 硬规则与 Git 约定
