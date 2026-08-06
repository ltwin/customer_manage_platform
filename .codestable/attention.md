# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 报告语言

CodeStable 所有落盘产出的正文用**中文**：plan / design、plan review / design-review、code review、QA、验收、issue（report / analysis / fix-note）、refactor、roadmap、goal、沉淀（compound）等所有人读报告都用中文表达。机器状态（YAML / JSON / `state.yaml` / frontmatter 字段）保持机读格式不翻译。如需改默认语言，改这一节。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

### 运行与本地起服务

### 测试

- 本机 Docker Desktop 下默认高并行 `go test ./...`（Testcontainers）可能偶发 `port "5432/tcp" not found`；包内串行更稳：`go test ./... -count=1 -parallel=1`（customer-avatar QA/验收 2026-07-13）。

### 命令与脚本陷阱

### 路径与目录约定

### 环境变量与凭证

- TG bot token 等一切凭证只经环境变量注入，不入库、不入 git（roadmap §4.5；2026-07-05 拍板）。

### 编码规范（任何模型 / agent 写码前必读）

- Go 后端：Uber Go Style Guide；review 按 `docs/go-style-checklist.md`；工具可查项交 gofmt / golangci-lint（决策：`.codestable/compound/2026-07-06-decision-go-uber-style-guide.md`）。
- 前端：TS `strict: true` + typescript-eslint strict + react-hooks + Prettier；review 按 `docs/frontend-style-checklist.md`；API 类型一律引用 openapi-typescript 生成的 `schema.d.ts`，禁手写重复 DTO（决策：`.codestable/compound/2026-07-07-decision-frontend-toolchain-first-standard.md`）。
- 两套规范均分层：CLAUDE.md 硬规则（账号隔离 / 薄 handler / 凭证 / 术语）优先级最高，checklist 是风格与惯用法层，冲突时硬规则赢。
- 工具链配置落地前（platform-skeleton 之前），工具项在 review 中记 N/A，人工项照常检查。
- lint 门禁：pre-commit hook 调 `make lint`，不过不许提交；**任何 agent 禁用 `git commit --no-verify`**，绕过只能由 owner 人工执行（决策：`.codestable/compound/2026-07-07-decision-lint-gate-pre-commit.md`；hook 在 platform-skeleton 落地）。

### 其他

- Git 按 GitFlow：日常从 `develop` 拉 `feat/` `fix/` `refactor/` 分支，`main` 只收发布合并（详见 CLAUDE.md「Git 约定」）。
- 在开发之前必须提问用户是在当前branch开发还是使用worktree新开branch，单feature开发倾向于不开worktree，而并行开发则只能使用worktree
- 除非必要，否则最好一个featue一个提交，且提交代码必需经过人工同意，不能自动进行，这是为了在后续执行review的时候更加精准。
- 同一 feature 的实现/设计审核与修复循环默认最多 3 轮（含独立 review/QA closure）；第 3 轮后非阻塞项转为 residual risk，仍有 blocking 则停在 owner checkpoint，额外复审须 owner 明确授权。
