# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 报告语言

CodeStable 所有落盘产出的正文用**中文**：plan / design、plan review / design-review、code review、QA、验收、issue（report / analysis / fix-note）、refactor、roadmap、goal、沉淀（compound）等所有人读报告都用中文表达。机器状态（YAML / JSON / `state.yaml` / frontmatter 字段）保持机读格式不翻译。如需改默认语言，改这一节。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

### 运行与本地起服务

### 测试

### 命令与脚本陷阱

### 路径与目录约定

### 环境变量与凭证

- TG bot token 等一切凭证只经环境变量注入，不入库、不入 git（roadmap §4.5；2026-07-05 拍板）。

### 其他

- Git 按 GitFlow：日常从 `develop` 拉 `feat/` `fix/` `refactor/` 分支，`main` 只收发布合并（详见 CLAUDE.md「Git 约定」）。
- 在开发之前必须提问用户是在当前branch开发还是使用worktree新开branch，单feature开发倾向于不开worktree，而并行开发则只能使用worktree
