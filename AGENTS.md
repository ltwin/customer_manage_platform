# AGENTS

<skills_system priority="1">

## Available Skills

<!-- SKILLS_TABLE_START -->
<usage>
When users ask you to perform tasks, check if any of the available skills below can help complete the task more effectively. Skills provide specialized capabilities and domain knowledge.

How to use skills:
- Each skill has a <path> tag containing the full path to the SKILL.md file
- Read the skill file directly using: Read("<path-from-skill-tag>")
- The skill content will load with detailed instructions on how to complete the task
- The parent directory of SKILL.md contains bundled resources (references/, scripts/, assets/)

Usage notes:
- Only use skills listed in <available_skills> below
- Do not invoke a skill that is already loaded in your context
- Each skill invocation is stateless

Skills directory: /Users/samson/.claude/skills
</usage>

<available_skills>

</available_skills>
<skill>
<name>daily-summary</name>
<description>Generate daily work reports from Cursor chat history. Use when users request work summaries, daily reports, or need to review daily work content. Supports querying sessions by date, reading conversations, identifying work types, extracting tech debt, and saving summaries. Can optionally integrate git commit analysis.</description>
<path>/Users/samson/.claude/skills/daily-summary/SKILL.md</path>
<location>global</location>
</skill>

<skill>
<name>go-react-stack</name>
<description>Create a new Go+React full-stack project with DDD architecture, OpenSpec specifications, and React best practices. Includes complete project scaffolding with backend (Go with Gin), frontend (React with TypeScript), OpenSpec structure, and example code following project conventions. Use this skill when users request creating a new full-stack project, scaffolding a Go+React application, or setting up a project with OpenSpec specifications.</description>
<path>/Users/samson/.claude/skills/go-react-stack/SKILL.md</path>
<location>global</location>
</skill>

<skill>
<name>openspec</name>
<description>Specification-driven development workflow tool for reaching consensus on requirements before coding. Use when working with OpenSpec workflows, creating change proposals, implementing approved changes, or archiving completed work.</description>
<path>/Users/samson/.claude/skills/openspec/SKILL.md</path>
<location>global</location>
</skill>

<!-- SKILLS_TABLE_END -->

</skills_system>

## Project

项目规则手册与文档索引见 [CLAUDE.md](CLAUDE.md)；CodeStable 工作流启动必读 `.codestable/attention.md`。

## 验证范式

**门禁按影响面选，不按习惯选。** `make check` 是合回 `develop` 与发布前的门禁，不是每次编辑的默认动作。改一行 CSS 就跑全量，浪费的不只是时间——等待中人会倾向于跳过验证，那才是真代价。

### 判定表

| 改动落在哪 | 跑什么 | 实测耗时 |
|---|---|---|
| 前端 CSS / 组件 / 页面 | `make check-frontend` | 4s |
| 前端且引用了生成的 `schema.d.ts` | 上面 + `make generate-check` | +1s |
| 单个 Go 包内部逻辑 | `make check-go PKG=./internal/xxx/...` | 2~60s，取决于包 |
| Go 跨包改动 / 接口签名 / 迁移 | `make check-go`（PKG 默认 `./...`） | ~6min |
| `api/openapi.yaml` | `make generate` + 提交两份生成物 + 两侧受影响包 | — |
| `scripts/` 下部署与 ops 契约 | `make check-ops` | ~2min |
| 合回 `develop` 前 / 发布前 | `make check` | ~8min |

分层门禁只改变**跑什么**，不改变**标准**。缩小范围是因为其余部分与改动无关，不是为了让红灯消失；一旦不确定影响面，就往上取一档。

### 为什么 `make check` 贵

暖缓存下各段实测：

| 段 | 耗时 | 占比 |
|---|---|---|
| 后端 `go test -p=1 ./...` | ~350s | 73% |
| `scripts/test-auth-security-catalog.sh` | 86s | 18% |
| `scripts/test-auth-legacy-cutover.sh` | 22s | 5% |
| `build` + `lint` + `generate-check` | 9s | 2% |
| 前端全部单测（312 个） | 0.9s | <1% |

后端 18 个包各起各的 PostgreSQL 容器（22 个启动点，无 reuse），且 `-p=1 -parallel=1` 强制全串行——串行是为了绕开 testcontainers 的端口映射 flake。**慢和 flake 是同一个根因**：每个包都要自己抢一次端口映射。

### 后端红灯分诊

后端测试报红先单独重跑那个包：

```
cd backend && go test ./internal/<包>/ -count=1
```

单独跑过 = 已知 flake（`port "5432/tcp" not found`），不是回归，可以继续；单独跑也红 = 真回归，必须修。不要因为"上次也这样"就直接放行，每次都要跑这一步确认。

### 测试不是唯一的验证手段

选与结论匹配的证据，别用跑得动的东西冒充跑得对的东西：

- **视觉 / 布局 / 对比度**：浏览器里实测计算值（`getComputedStyle`、规则解析、面积统计）。单测不看圆角和对比度，全绿不代表没改坏。
- **契约**：`make generate-check` 的漂移比对，不是手读 diff。
- **并发 / 幂等**：容器集成测试，不是单测桩。

### 前端测试的登记方式

`make test-frontend` 用 glob 发现 `frontend/scripts/*.test.ts` 与 `*.test.mjs`，**新增测试文件自动入闸，不需要改 Makefile**。三处注册守卫（`auth` / `account-center` / `plan-share`）钉住这个发现机制，换回手工清单会让它们失败——手工清单曾经漂移过，三个 `test:*` 脚本存在却从未进门禁。

`scripts/*.e2e.mjs` 不匹配测试文件名模式，不会被卷进来：它需要 Playwright、运行中的前端和一对真实账号口令，只能手动跑。
