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
| Go 跨包改动 / 接口签名 / 迁移 | `make check-go`（PKG 默认 `./...`） | ~55s |
| `api/openapi.yaml` | `make generate` + 提交两份生成物 + 两侧受影响包 | — |
| `scripts/` 下部署与 ops 契约 | `make check-ops` | ~2min |
| 合回 `develop` 前 / 发布前 | `make check` | ~3min |

分层门禁只改变**跑什么**，不改变**标准**。缩小范围是因为其余部分与改动无关，不是为了让红灯消失；一旦不确定影响面，就往上取一档。

### `make check` 的成本构成

暖缓存下各段实测：

| 段 | 耗时 | 占比 |
|---|---|---|
| 后端 `go test -p=4 ./...` | ~50s | 27% |
| `scripts/test-auth-security-catalog.sh` | 86s | 46% |
| `scripts/test-auth-legacy-cutover.sh` | 22s | 12% |
| `build` + `lint` + `generate-check` | 9s | 5% |
| 前端全部单测（312 个） | 0.9s | <1% |

后端已不再是大头。现在最贵的单项是 `test-auth-security-catalog.sh`（86s），它内部也是 `go test`——下一个要优化的就是它。

集成测试的容器由 `internal/platform/store/storetest` 统一提供：**每个测试二进制一个容器**，测试之间靠独立 database 隔离（从模板库克隆，约 37ms）。全量一轮 19 个容器。新写集成测试直接用 `storetest.NewURL(t)`，不要自己起容器——自起会同时丢掉复用和下面那条等待策略。

### 端口映射 flake 的根因（已修）

历史上 `port "5432/tcp" not found` 长期存在，一度被当成「testcontainers 并发缺陷」用 `-p=1` 串行绕开。真正的原因是**等待策略只等日志、不等端口发布**：日志打出 `ready to accept connections` 时 Docker 可能还没发布端口映射，紧接着的 `MappedPort` 就报错。

`storetest.waitReady()` 补上 `wait.ForListeningPort` 后，`-p=4` 连续六轮全绿。**任何新起容器的代码都必须用 `waitReady()`**，只等日志就会把这个洞带回来。

并发度由 `GOTEST_P` 控制，默认 4，是这台机器（12 核 / Docker 8GB）的实测上限：`-p=8` 会把容器启动压到排队，十分钟跑不完。调高前先按下面的判据实测。

### 后端红灯分诊

后端测试报红先单独重跑那个包：

```
cd backend && go test ./internal/<包>/ -count=1
```

**默认假设是真回归。** 容器复用与等待策略修好之后，`make check` 是能一次通过的（连续多轮实测），红灯不再有「大概率是 flake」这个先验。

判为环境问题需要三条同时成立：

1. **错误文本**是容器启动期失败（`port "5432/tcp" not found`、`rootless Docker not found`），而非断言失败或 diff 输出。
2. **失败点游走**：重跑时换成同包另一个测试失败 → 竞态；每次钉死同一个测试 → 真回归。
3. **重跑能变绿**：跑三轮，出现过绿。

三条都成立才放行，并在提交或合并说明里写清跑了哪几轮。任一条不成立就当回归查。

还有一类会被误判成 flake 的**真问题**：测试自带真实墙钟窗口，并发下拿不到时间片就失败（`TestS6MonotonicBudgetThroughBeginCurrentCall` 曾用 100ms 窗口跑两轮数据库往返）。这类要改测试——把墙钟窗口放大到与被测语义无关的量级，而不是调低并发度。

### 测试不是唯一的验证手段

选与结论匹配的证据，别用跑得动的东西冒充跑得对的东西：

- **视觉 / 布局 / 对比度**：浏览器里实测计算值（`getComputedStyle`、规则解析、面积统计）。单测不看圆角和对比度，全绿不代表没改坏。
- **契约**：`make generate-check` 的漂移比对，不是手读 diff。
- **并发 / 幂等**：容器集成测试，不是单测桩。

### 前端测试的登记方式

`make test-frontend` 用 glob 发现 `frontend/scripts/*.test.ts` 与 `*.test.mjs`，**新增测试文件自动入闸，不需要改 Makefile**。三处注册守卫（`auth` / `account-center` / `plan-share`）钉住这个发现机制，换回手工清单会让它们失败——手工清单曾经漂移过，三个 `test:*` 脚本存在却从未进门禁。

`scripts/*.e2e.mjs` 不匹配测试文件名模式，不会被卷进来：它需要 Playwright、运行中的前端和一对真实账号口令，只能手动跑。
