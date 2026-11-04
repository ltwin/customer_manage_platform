# GUAN-3 设计记录：仓库切换到 GitFlow + Multica，知识迁入 docs/，归档 .codestable

> **实施基线**：GUAN-3 描述 **revision 23**，Owner tao li 在评论 `01a1a4bb-bc8b-7d96-a509-55e0cb44e919` 批准（「批准 revision 23」）；内容与设计评审第 2 轮 Approve（评论 `01a19c9f-e5c6-7dec-9bfc-1f3f048c38bd`，revision 21）一致，批准后没有 `description_updated`。不是 Epic 子 issue。仓库基线 `origin/develop` = `6013c2e1`。
>
> 下文「需求规格」「技术设计」两节是基线版本原文。唯一改动：旧 ADR 文件名写成 `00[5]-separate-plan-revision…`（glob 写法），避免 AC2 的 `git grep` 命中本记录。

## 长期约定速查

本节摘出以后还会用到的约定；细节和理由见后面的基线原文。

### 目录布局

| 内容 | 位置 |
|---|---|
| 领域术语（canonical） | `docs/domain/glossary.md`（含从 `.lti/contracts/glossary.md` 合并的「外部助手」「外部助手连接」；`.lti/` 迁移前两处同步维护） |
| ADR 001-009 | `docs/adr/`（原两个 005：identity 保留 005，plan-revision 改为 009） |
| 工程决策与经验沉淀 | `docs/decisions/`（原 compound，文件名与 frontmatter 不变） |
| 能力愿景 / 能力需求 | `docs/product/vision.md`、`docs/product/capabilities/` |
| 脚本读取的 v1-hardening 运维输入 | `docs/ops/v1-hardening/`（数据文件，内容不改） |
| 单需求设计记录 | `docs/design/GUAN-<n>.md` |

### 归档引用

- `.codestable/` 已从 `develop` 删除，完整内容在 tag `archive/codestable-final`（打在 `6013c2e1`，1005 个文件）。
- 文档里引用未迁移的历史文件，写成 `archive/codestable-final:.codestable/<path>`，用 `git show <引用>` 打开。
- 带定位符的写成 `archive/codestable-final:.codestable/<path>#<locator>`。locator 是正文里的文本（如 approval-report 的 YAML 键），不是 Markdown 标题 slug。查阅：`ref='archive/codestable-final:.codestable/<path>#<locator>'; git show "${ref%%#*}" | grep -n -- "${ref#*#}"`。归档里没有文件名含 `#` 的文件，按第一个 `#` 拆分无歧义（`scripts/check-doc-links.py` 会断言）。
- 迁移文件和 4 个入口文件以外的 `.codestable/` 文本引用（`docs/dev/`、`DESIGN.md`、`api/openapi.yaml:2` 等）没有改写，按上面的方式查阅。
- 检查：`python3 scripts/check-doc-links.py --verify-archive`（默认扫描 34 份迁移文件 + 4 个入口文件），`--self-test` 用两处真实 locator 引用做正反用例。

### 证据目录

手动运行的 ops / rehearsal 脚本默认把证据写到 `var/evidence/<topic>/`：`var/` 被 `.gitignore` 忽略，`.dockerignore` 也排除 `/var/`。环境变量覆盖（`AVATAR_MEDIA_WIPE_EVIDENCE`、`AVATAR_MEDIA_V2_MIXED_EVIDENCE`）照旧可用。需要长期保留的结论贴进 issue 评论，不入库。

| 脚本 | 默认目录 |
|---|---|
| `scripts/lib/v1-ops-smoke-runner.py`（`scripts/v1-ops-smoke.sh`） | `var/evidence/v1-hardening/ops` |
| `scripts/test-avatar-media-v1-restore-wipe.sh` | `var/evidence/avatar-media-safety-net`；带 profile migration 时 `var/evidence/account-profile-center` |
| `scripts/test-avatar-media-v2-mixed-restore.sh` | `var/evidence/account-center-hardening` |
| `scripts/telegram-smoke.sh` | 不落盘；真机截图贴进对应 issue 评论 |

### pre-commit 分派与偏离 2026-07-07 决策的理由

- `make hooks` 执行 `git config core.hooksPath scripts/githooks`，可重复执行。
- `scripts/githooks/pre-commit` 读 `git diff --cached --name-only --no-renames -z`：重命名拆成「删源 + 增目标」，删除也参与分派。
  - `backend/*.go`、`backend/go.mod`、`backend/go.sum`、`.golangci*` → `make -s lint-go`（全量 `golangci-lint run ./...`；找不到 golangci-lint 时拒绝）；
  - `frontend/*`（含 `frontend/.oxlintrc.json`）→ `make -s lint-frontend`（全量 `npm run lint`）；缺 `frontend/node_modules` 时拒绝并提示 `make frontend-install`，不自动安装；
  - 都不命中 → 跳过 lint，退出 0。
- `make lint` = `lint-go` + `lint-frontend`，命令与拆分前一致，CI 继续复用。
- **偏离理由**：2026-07-07 决策（`docs/decisions/2026-07-07-decision-lint-gate-pre-commit.md`）定的是 pre-commit 全量 `make lint`，前提是「提交频率低」。切到 Multica 后 agent 在自己的分支上自主提交，提交频率变高；`golangci-lint` 一次约 54 秒，`make lint` 还会无条件跑前端 lint，而 agent worktree 默认没有 `node_modules`，只改文档的提交也会被拦住，agent 又不能 `--no-verify`。所以按暂存路径分派，lint 本身仍然全量，不引入 lint-staged。
- **已知边界**：lint 检查的是工作区而不是暂存快照；部分暂存时可能放过未暂存的问题，也可能被未暂存的问题拦住（与原决策口径一致）。
- **worktree 共享语义**：`core.hooksPath` 写在仓库级 `.git/config`，主检出和所有 worktree 共用；值是相对路径，git 在各自工作树根目录下解析，所以每个 worktree 用自己检出的 `scripts/githooks/`。checkout 在没有该目录的旧分支上时找不到 hook，git 直接跳过。回退：`git config --unset core.hooksPath`。
- 自测：`scripts/test-githooks-pre-commit.sh`（在 `make test-ops` 里），8 个用例。

### planning v1 发布就绪：stage 审批源退役

- stage 审批（`stage-1-evidence-go` / `stage-2-evidence-go`）原来记在 CodeStable 的 approval-report，由 dispatch gate 读出，见 `archive/codestable-final:.codestable/roadmap/creative-shoot-planning/`。
- 现在 `scripts/lib/planninghardening/release.py` 的 `_canonical_stage_approvals` 直接抛 `release-stage-approval-source-retired`；`verify_readiness` 先调它，所以在读任何证据之前就失败，`scripts/verify-planning-v1-release-readiness.sh` 对任何证据都返回 9。当前**没有放行路径**。
- dependency conformance 升到 `planning-v1-dependency-conformance-v2`：去掉 `goal-control` 行、`ACCEPTED_ARTIFACTS`、`roadmap_sha256`，行状态只看代码锚点。
- selftest 用夹具 binding 继续覆盖全部结构校验；`docs/ops/planning-backup-restore.md`「Release readiness」一节已改为退役说明。
- planning v1 发布前，要另开 issue 在 Multica Owner 审批之上重建 stage 审批源。

### AC13 保留清单

PR head 上 `git grep -n '\.codestable' -- scripts Makefile` 只剩下面这些，全部是静态字面量，不读写工作区 `.codestable/`，目录不存在时不报错、不重建：

| 位置 | 内容 | 为什么不会在运行时读写 |
|---|---|---|
| `scripts/lib/planninghardening/release.py` `_canonical_stage_approvals` docstring | 归档引用 `archive/codestable-final:.codestable/roadmap/creative-shoot-planning/` | 注释；函数体直接 raise |
| `scripts/check-doc-links.py` 模块 docstring、错误信息、注释 | 描述被检查的 `.codestable/` 文本 | 字符串 |
| `scripts/check-doc-links.py` `CODESTABLE_RE`、`ARCHIVE_REF_RE` | 识别 `.codestable/` 与 `archive/codestable-final:` 前缀的正则 | 只对被扫描文件做文本匹配 |
| `scripts/check-doc-links.py` `archive_names_have_no_hash` | `git ls-tree -r --name-only archive/codestable-final -- .codestable` | 从 tag 读树对象，不访问工作区 |
| `scripts/check-doc-links.py` `self_test` | 2 处真实引用 + 2 个反例 | 经 `git cat-file` 从 tag 读取 |

## 实施中的实现细节偏离

都不涉及接口、契约或数据结构：

1. **前端 lint 触发条件**：设计写的是根目录 `.oxlintrc.json`，实际文件在 `frontend/.oxlintrc.json`，已经被 `frontend/*` 覆盖，所以 hook 里没有单列。后端配置按规格写成 `.golangci*` 通配。
2. **链接检查脚本**：
   - 检查 1、2 跳过 fenced code block，检查 3（`.codestable/` 必须带归档前缀）对全文生效；
   - 含 `<`、`*`、`{`、`$`、`…` 的占位符（如 `CLAUDE.md` 用法说明里的 `.codestable/<path>`）不当作真实引用去 `--verify-archive`；
   - 仓库顶层条目里排除 `.git`（worktree 里它是文件）；
   - `--self-test` 比设计多一个反例（不存在的文件），并断言归档文件名不含 `#`。
3. **术语合并格式**：两个条目放进 `docs/domain/glossary.md` 末尾新增的「### 外部助手」小节，套用该文件的 `**术语（English）**:` 条目格式；「不是 / 边界场景 / 拼写」各行正文原样保留，并注明来源。
4. **selftest**：除了断言 `release-stage-approval-source-retired`，还对不存在的证据路径调用 `verify_readiness`，证明在读取证据之前就失败；另外断言 conformance 是 v2、没有 `roadmap_sha256`、行 ID 与 `REQUIRED_CONFORMANCE_IDS` 一致。
5. **`GEMINI.md` 和 `AGENTS.md` 的措辞**：`GEMINI.md` 里原「技能清单与调用方式」改为「验证范式：AGENTS.md」；`AGENTS.md` 的 `## Project` 只留指向 `CLAUDE.md` 的一句。

## 需求规格

> 依据：GUAN-1 报告（评论 `01a19c49-dc9b-71e6-80a0-a4fcf779b8c1`）第 4 节、第 5 节 C/D，Owner 决策记录（评论 `01a19c58-51ae-71d3-ab58-8e3006635359`），以及基线 `origin/develop` = `6013c2e1` 上的仓库现状。标「假设」的条目按推荐答案写入，Owner 可推翻；Q1、Q2 已由 Owner 确认按推荐答案执行（评论 `01a19c8b-3312-7895-9a4a-6a5f30272ea0`），见「已决问题」。

### 背景与目标

仓库入口文件仍要求 agent 走 CodeStable：`CLAUDE.md:7-9` 要求读 `.codestable/attention.md`、从 roadmap 取条目、以 `items.yaml` 为进度权威（最后更新在 8 月）；`AGENTS.md:3-51` 是本机全局 skill 清单，含 `/Users/samson/...` 路径；`GEMINI.md` 也指向 `attention.md`。`attention.md`「其他」一节要求「提交需人工同意」「开发前先问 branch 还是 worktree」，和 Multica 的异步 run 冲突。长期知识散落在 `.codestable/requirements/`、`adrs/`（有两个 005）、`compound/` 下，`docs/` 里没有统一位置。

**成功标准**

1. agent 读 `CLAUDE.md` / `AGENTS.md` / `GEMINI.md` / `README.md` 时，只会拿到「Multica workflow + GitFlow」规则，没有任何指向 CodeStable 流程的指令。
2. 下面清单中的 34 份长期知识都在 `docs/` 下，链接都能解析。
3. `develop` 上不再有 `.codestable/`，历史可以通过 tag `archive/codestable-final` 完整找回。
4. lint pre-commit 真正装上（`make hooks`），`--no-verify` 禁令写进 `CLAUDE.md`。
5. 删除 `.codestable/` 之后，`scripts/` 不再在运行时读写 `.codestable/`，现有门禁 `make check-ops` 仍然通过（Q1）。
6. 只改文档或脚本的提交不触发 lint；改 Go 或前端代码的提交照常被 lint 拦截（Q2）。

### 范围

**做**

1. **知识迁移**（共 34 份，用 `git mv` 迁移，正文保持原样，只改链接和 ADR 编号）：

   | 来源 | 目标 | 数量 |
   |---|---|---|
   | `.codestable/requirements/CONTEXT.md` 加上 `.lti/contracts/glossary.md` 的 2 个条目（「外部助手」「外部助手连接」） | `docs/domain/glossary.md` | 1 |
   | `.codestable/requirements/VISION.md` | `docs/product/vision.md` | 1 |
   | `.codestable/requirements/` 下的能力需求文档：account-center、creative-asset-platform、creative-canvas-foundation、creative-shoot-planning、customer-profile、dashboard、order-tracking、package-catalog、reminder-engine、schedule-calendar、self-service-account-system、telegram-digest | `docs/product/capabilities/<原文件名>` | 12（原需求写的是 11，实际有 12 份） |
   | `.codestable/requirements/adrs/001-008`（9 篇） | `docs/adr/<编号>-<原 slug>.md` | 9 |
   | `.codestable/compound/*.md` | `docs/decisions/<原文件名>` | 11 |

   放在 `docs/product/capabilities/` 下，是为了避开已有的 `docs/product/creative-asset-platform/` 目录。

2. **ADR 重号**（假设 A2）：`005-separate-account-identity-and-credential`（date 2026-07-30）保留 005；`00[5]-separate-plan-revision-from-append-only-execution-facts`（date 2026-08-06）改为 `009`，文件名、正文标题里的编号和所有指向它的引用一起改。
3. **链接修复**：在 34 份迁移文件和 4 个入口文件（`CLAUDE.md`、`AGENTS.md`、`GEMINI.md`、`README.md`）里，凡是提到 `.codestable/...` 的地方：
   - 指向已迁移文件的，改成新的 `docs/` 路径；
   - 指向未迁移文件的，改成归档引用格式 `archive/codestable-final:.codestable/<path>`。这个格式可以直接用 `git show <ref>` 打开。
4. **改写入口文件**：
   - `CLAUDE.md`：
     - 「工作流」：需求、设计、进度都在 Multica 项目「光合工坊」，仓库只保存长期知识（`docs/`）。
     - 硬规则 3：契约以 `api/openapi.yaml` 为准；改契约走完整轨，经过 Owner「方案确认」。
     - 硬规则 5：术语指向 `docs/domain/glossary.md`。
     - 「Git 约定」：去掉 branch-guard；分支命名 `feat|fix|refactor/GUAN-<n>-<slug>`；PR 标题以 `GUAN-<n>:` 开头；agent 可以在自己的分支上提交和推送；合入 `develop` / `main` 只能由 Owner 完成；禁止 `--no-verify`；先跑 `make hooks`。
     - 「文档索引」：全部改成 `docs/` 路径。
     - 新增两条说明：一是 `.lti/` 是过渡期遗留，新需求不使用，`scene-editor-3d` 收尾之前它的 contracts 和 lessons 仍以 `.lti/` 为准；二是历史 `.codestable/` 路径用 tag `archive/codestable-final` 查阅。
   - `AGENTS.md`：删掉 `<skills_system>` 块（`AGENTS.md:3-51`）；第 55 行只保留指向 `CLAUDE.md` 的内容；「验证范式」一节原样保留。
   - `GEMINI.md`：删掉指向 `attention.md` 的那一行。
   - `README.md`（假设 A8）：第 3、5 行原来说「规格与流程见 `.codestable/`」，改为指向 `docs/` 和 Multica。
5. **`attention.md` 退役**：逐条处理如下，处理完后随 `.codestable/` 一起删除。

   | 条目 | 处理 |
   |---|---|
   | 报告语言：中文 | 并入 `CLAUDE.md`，改成「`docs/` 与 issue 产出的正文用中文」 |
   | 测试：`-parallel=1` 更稳 | 删除（和 `AGENTS.md` 里「端口映射 flake 已修」矛盾） |
   | 凭证红线 | 删除（硬规则 4 已经覆盖） |
   | 编码规范：Go / 前端 | 删除（硬规则 6、7 已经覆盖） |
   | 编码规范：硬规则优先于 checklist | 并入 `CLAUDE.md` |
   | 编码规范：工具链落地前记 N/A | 删除（`make lint` 已经落地，见 `Makefile:36`） |
   | lint 门禁和 `--no-verify` 禁令 | 并入 `CLAUDE.md`「Git 约定」 |
   | GitFlow | 删除（「Git 约定」已经覆盖） |
   | 开发前先问 branch 还是 worktree | 删除（由 Multica worktree 模式决定） |
   | 一个 feature 一个提交、提交需人工同意 | 替换为「agent 可以在自己的分支上提交和推送；一个 issue 对应一个 PR」 |
   | 3 轮评审上限 | 删除（已经写进 Reviewer instructions 和 step instructions，见 GUAN-2） |
   | 创意空间需求归属 | 删除（canonical 位置改为 `docs/product/capabilities/`） |

6. **`make hooks`**：沿用 `docs/decisions/2026-07-07-decision-lint-gate-pre-commit.md` 的方案，即仓库脚本 `scripts/githooks/pre-commit` 加 `core.hooksPath`，不引入 husky / lefthook。pre-commit 按暂存路径分派（Q2）：
   - 暂存了 `backend/` 下的 `.go` 文件，或改了 `backend/go.mod` / `backend/go.sum` / `.golangci*` → 跑后端 lint（`golangci-lint run ./...`，全量）；
   - 暂存了 `frontend/` 下的文件 → 跑前端 lint（`npm run lint`，全量）；缺少 `frontend/node_modules` 时拒绝提交，并提示先执行 `make frontend-install`，hook 不自动安装；
   - 两者都命中就都跑；都没命中（只改文档、脚本、配置）→ 跳过 lint，退出 0；
   - 不引入 lint-staged，不改 `make lint` 本身（后续 CI 继续复用 `make lint`）；
   - 偏离 2026-07-07 决策「全量 `make lint`」的原因（agent 自主提交、提交频率变高）写进 `docs/design/GUAN-3.md`，并在迁移后的该决策文件末尾追加一行「已被 GUAN-3 修订」的指针（这是 A4「正文不改写」的唯一例外）。
7. **归档**：在 PR 与 `origin/develop` 的 merge-base（截至本规格为 `6013c2e1`）上打 tag `archive/codestable-final` 并推送，然后在 PR 分支上 `git rm -r .codestable`。`.dockerignore:4` 的 `.codestable` 行一起删掉，并补一行 `/var/`（假设 A10；`var/` 下会放证据目录和验收 clone，`.gitignore` 的排除不作用于 Docker 构建上下文）。
8. **脚本解耦**（Q1）：删除 `.codestable/` 之后，`scripts/` 不再在运行时依赖它。基线上的依赖点（`git grep -n '\.codestable' -- scripts Makefile`）：
   - **运维输入迁到 `docs/ops/v1-hardening/`**：`v1-hardening-design.md`、`v1-hardening-ops-case-catalog.yaml`、`evidence/ops/v1-ops-smoke-results.json`（来自 `.codestable/features/2026-07-22-v1-hardening/`），用 `git mv`；同步改 `scripts/test-v1-ops-contract.sh:7-14`、`scripts/lib/v1-ops-smoke-runner.py:28`、`scripts/lib/v1-ops-smoke-results.py:76` 的路径。这 3 份不计入 AC1 的 34 份，也不算入 AC4 的扫描范围（假设 A11）。
   - **删除只校验 CodeStable 流程证据的检查**：`scripts/test-auth-security-catalog.sh:95` 的实现报告 grep；`scripts/lib/planninghardening/conformance.py:24-35,88` 的 `ACCEPTED_ARTIFACTS`、`goal-control` 行和 roadmap 哈希。同一脚本里的代码锚点检查保留。
   - **证据输出默认目录改到 gitignore 掉的本地目录**：`scripts/lib/v1-ops-smoke-runner.py:29`、`scripts/test-avatar-media-v1-restore-wipe.sh:30-32`、`scripts/test-avatar-media-v2-mixed-restore.sh:12`；`scripts/telegram-smoke.sh:31` 的提示文字改为同一目录或「贴进 issue 评论」。具体目录由技术设计定（仓库已 gitignore `var/`）。环境变量覆盖（如 `AVATAR_MEDIA_WIPE_EVIDENCE`）保持可用。
   - **其余引用**：`scripts/lib/planninghardening/release.py:83,201,810`（planning v1 发布就绪校验读 roadmap）和 `scripts/check-avatar-media-safety-net-scope.sh:29-47`（历史 scope 白名单）由技术设计逐个给出「删除 / 改路径 / 保留并说明」的处置；保留的必须不会因 `.codestable/` 缺失而失败或重建目录。
   - **运维指南同步退役**：`docs/ops/planning-backup-restore.md`「Release readiness」一节（`:151-206`）仍要求从 canonical approval-report / gate 取审批（`:179-182`）、绑定 roadmap bytes（`:192-193`），并给出正式运行命令（`:199`）。审批源退役后这条放行路径不可执行，本 PR 同步改写这一节，写明入口已退役、审批源重建前一律拒绝、其余 sibling 结构规则的适用边界（见技术设计第 3 节）。
   - 门禁：本 PR 必须跑 `make check-ops`。

**不做**

- `.lti/` 不做任何改动：contracts、lessons、skills、`.gitignore` 里的 lti 块都不动。按 GUAN-1 决策 1，等 `scene-editor-3d` 收尾后另开 issue 迁移（假设 A1）。
- `.codestable/` 下其余内容不迁移：`creative-workspace-v6/` 的 83 个原型文件、roadmap、brainstorm、features、epics、work、gates、tools。
- 迁移文件和入口文件以外的 `.codestable/` 文本引用不改，统一靠 `CLAUDE.md` 里的归档约定查阅（假设 A5）。这包括 `docs/dev/`、`docs/product/`、`docs/prototypes/`、`DESIGN.md:73`、`backend/internal/platform/llmgateway/generation_aicodewith.go:36`、`api/openapi.yaml:2`。不改 `api/openapi.yaml`，所以不需要 `make generate`。
- `.claude/`、`.agents/`、`.cursor/` 下的 skill：这三个目录被 `.gitignore:17,28-29` 忽略，不在仓库里。Owner 主检出里只有 `lti-*`，没有 `cs-*`。agent 侧的禁用已经在 GUAN-2 的 instructions 里完成（假设 A6）。
- GitHub branch protection、`guanghe-knowledge` skill、autopilot 归 GUAN-4；CI 不做。
- 不新建 `docs/contracts/`、`docs/architecture.md`、`docs/lessons/`，它们的内容等 `.lti/` 迁移时再来。
- 不在 Owner 主检出执行 `make hooks`。PR 只提供这个目标，合并后由 Owner 自己执行。

### 验收标准

- **AC1 迁移完整**
  - Given：PR 中的「纯迁移」提交 `<mv>`（只含 `git mv`，见技术设计「提交切分」）。
  - When：执行 `git diff --find-renames --name-status <mv>^ <mv>`。
  - Then：
    - 上面清单里的 34 个来源都以 `R100` 出现，目标路径和清单一致；
    - PR head 上对每个目标文件执行 `git log --follow --oneline -- <目标>`，都能追到 `.codestable/` 下的来源提交。
- **AC2 ADR 不重号**
  - Given：PR head。
  - When：列出 `docs/adr/`。
  - Then：
    - 编号是 001–009，没有重复；
    - `009-separate-plan-revision-from-append-only-execution-facts.md` 的正文标题编号是 009；
    - 仓库内（不含 `.lti/`）没有引用旧文件名 `00[5]-separate-plan-revision`。
- **AC3 术语合并**
  - Given：PR head。
  - When：读 `docs/domain/glossary.md`。
  - Then：
    - 「外部助手」「外部助手连接」两个条目各出现一次；
    - 没有重复的术语标题；
    - `.lti/contracts/glossary.md` 和基线逐字节相同。
- **AC4 链接可解析**
  - Given：PR head。
  - When：用技术设计给出的检查命令（脚本或命令，结果贴进 PR），扫描 34 份迁移文件和 4 个入口文件。
  - Then：
    - 所有相对路径 Markdown 链接 `[..](path)`，以及反引号里指向仓库文件的路径，都能解析到树中已有的文件；
    - 这些文件里出现的每一处 `.codestable/`（含 frontmatter 值），前面都紧跟 `archive/codestable-final:`；
    - 对其中至少 3 处归档引用，`git show archive/codestable-final:<path>` 都能成功打开；带定位符的引用 `<path>#<locator>` 去掉 `#` 及之后的部分再打开，且 `<locator>` 在该文件正文中出现（至少包含 `2026-07-06-accountscope-fail-loud.md` 里的 `#round-8-share-contract` 这一处）。
- **AC5 入口文件干净**
  - Given：PR head。
  - When：执行 `git grep -nE 'items\.yaml|cs-roadmap|\bcs-[a-z]|attention\.md|branch-guard|/Users/|BEGIN MULTICA-RUNTIME' -- CLAUDE.md AGENTS.md GEMINI.md README.md`。
  - Then：没有任何结果；`.codestable` 在这 4 个文件里只出现在含 `archive/codestable-final` 的归档说明里。
- **AC6 `CLAUDE.md` 内容**
  - Given：PR head 的 `CLAUDE.md`。
  - When：人工核对。
  - Then：范围第 4、5 条列出的改写和并入项都在，并且都能对上 `attention.md` 处置表里「并入」的那几行。
- **AC7 `AGENTS.md`「验证范式」不变**
  - Given：PR head。
  - When：比较「验证范式」标题到文件末尾这一段和基线。
  - Then：没有差异；同时 `<skills_system>` 块已经删除。
- **AC8 `make hooks` 可用**
  - Given：一个独立 clone（`git clone` 到 `var/` 下的临时目录；不能用 Owner 仓库的 worktree，`core.hooksPath` 写在共享的 `.git/config`，会顺带改动 Owner 主检出）。
  - When：先执行 `make hooks`，再执行一次。
  - Then：
    - 两次都返回 0；
    - `git config --get core.hooksPath` 指向 `scripts/githooks`；
    - `scripts/githooks/pre-commit` 是可执行文件。
- **AC9 pre-commit 能拦住坏提交**
  - Given：在 AC8 的独立 clone 里已经执行过 `make hooks`。
  - When：暂存一个有 golangci-lint 错误的 Go 文件，然后提交。
  - Then：提交被拒，返回非 0，输出里有 lint 错误；修好之后可以提交。
  - 追加用例（Q2）：
    - Given 只暂存 `docs/` 下的 Markdown，When 提交，Then 不调用 golangci-lint / eslint，提交在数秒内成功；
    - Given `frontend/node_modules` 不存在且暂存了 `frontend/` 下的文件，When 提交，Then 提交被拒，返回非 0，输出提示执行 `make frontend-install`，且没有自动安装依赖；
    - Given 只暂存 `backend/` 下的 Go 文件且 lint 通过，When 提交，Then 只跑后端 lint，不跑前端 lint；
    - Given 把 `backend/` 下的 Go 文件 `git mv` 成 `docs/` 下的非 Go 文件，或把 `frontend/` 下的文件移出该目录，When 提交，Then 仍然跑对应的 lint（重命名按「删除源路径 + 新增目标路径」计入分派）。
  - 以上用例的命令和输出贴进 PR。
- **AC10 tag 已归档**
  - Given：PR 已经推送。
  - When：执行 `git ls-remote --tags origin archive/codestable-final`。
  - Then：
    - 有结果，指向 PR 的 merge-base；
    - 那个提交上 `.codestable/` 的文件数是 1005；
    - PR head 上 `git ls-files .codestable` 为空。
- **AC11 门禁通过**
  - Given：PR head。
  - When：执行 `make check-ops`。
  - Then：通过；执行后工作区里不存在 `.codestable/` 目录（`test -e .codestable` 为假）。
- **AC13 脚本不再依赖 `.codestable/`**（Q1）
  - Given：PR head。
  - When：执行 `git grep -n '\.codestable' -- scripts Makefile`。
  - Then：
    - 每一处剩余结果都在 `docs/design/GUAN-3.md` 里逐条列出，并说明为什么保留、为什么不会在运行时读写 `.codestable/`；预期只剩技术设计第 3 节「AC13 保留清单」里的静态字面量；
    - `docs/ops/v1-hardening/` 下有 design、ops catalog、smoke results 3 份文件，`scripts/test-v1-ops-contract.sh` 读的是这里；
    - 范围第 8 条列出的证据输出脚本，默认目录都在 gitignore 范围内（`git check-ignore` 命中）。
- **AC14 planning v1 发布就绪入口如实退役**（Q1 派生，设计评审第 1 轮）
  - Given：PR head。
  - When：
    - 执行 `make check-ops`（含 `scripts/test-planning-hardening.sh` 的 selftest）；
    - 执行 `./scripts/verify-planning-v1-release-readiness.sh --evidence <任意路径> --json`；
    - 读 `docs/ops/planning-backup-restore.md`「Release readiness」一节。
  - Then：
    - selftest 断言 `verify_readiness` 在读取任何证据之前抛出 `release-stage-approval-source-retired`，且用夹具 binding 的结构校验用例照常通过；
    - 命令返回 9，不输出 `passed`，执行后工作区不存在 `.codestable/`；
    - 该节开头有退役说明：入口已退役、审批源重建前对任何证据一律拒绝、需另开 issue 重建；不再出现「canonical `approval-report.md`」「roadmap bytes」作为现行要求；保留的 sibling 结构规则标明是「审批源重建后的候选契约 / selftest 覆盖的结构校验」，不构成放行路径。
- **AC12 交付形态**
  - Given：PR。
  - Then：
    - 分支是 `refactor/GUAN-3-<slug>`，目标 `develop`，标题以 `GUAN-3:` 开头；
    - PR 里包含 `docs/design/GUAN-3.md`；
    - PR 描述列出门禁命令和结果；
    - 没有使用 `--no-verify`。

### 边界与异常

- **Multica 注入块**：worktree 里的 `CLAUDE.md` 会被 Multica 自动追加 `BEGIN MULTICA-RUNTIME` 块，状态是未提交。这个块不能进提交，AC5 会检查。
- **账号隔离 / 鉴权**：不涉及，没有业务数据和代码路径。仓库是 public，迁移后的文档会公开，Owner 已在 GUAN-1 决策 5 接受。
- **并发 / 冲突**：
  - `.codestable/` 自 2026-10-16 起就没有提交，删除时出现 modify/delete 冲突的风险很低。
  - `CLAUDE.md` / `AGENTS.md` 可能和 Owner 本机分支（如 `scene-editor-3d`）的改动冲突，由 Owner 在 `check-merge` 前解决。
  - 推 tag 之前要确认 merge-base 之后 `origin/develop` 上没有改动 `.codestable/` 的提交；如果有，停下来在 issue 里报告。
- **旧 worktree / 旧分支**：`core.hooksPath` 是仓库级配置，所有 worktree 共用。没有 `scripts/githooks/` 的旧分支找不到 hook，git 会直接跳过，这个行为可以接受，技术设计里要写明。
- **hook 失败**：lint 不过时修代码重新提交。agent 一律不得 `--no-verify`；环境坏了要绕过 hook，只能由 Owner 手动操作并说明原因。
- **tag 打错**：tag 只是新增，不影响已有内容。打错了由 Owner 删除后重建，agent 不删除远端 tag。
- **LTI 不受影响**：已核对，`.lti/config.yaml` 和 Owner 主检出的 `.claude/skills/lti-*` 都没有引用 `codestable`，删除 `.codestable/` 不影响 LTI 流程。`.lti/` 下有 92 个文件在历史正文里提到 `.codestable/`，保持原样。
- **兼容旧引用**：未迁移的文本引用保持不动（A5），通过 `CLAUDE.md` 的归档约定查阅。

### 假设（按推荐答案写入，Owner 可推翻）

- **A1** `.lti/contracts`、lessons 本次不迁移，依据是原始需求第 5 条和 GUAN-1 决策 1。术语表只复制 `.lti` 的 2 个条目，`.lti` 原文件不动。`scene-editor-3d` 收尾后另开 issue 迁移 `.lti/`，届时 `.lti` 期间新增的术语再同步一次。
- **A2** ADR 按日期决定：先出现的 identity（07-30）保留 005，plan-revision（08-06）改为 009，即下一个空号。
- **A3** 11 篇 compound 全部放进 `docs/decisions/`，保留文件名和 frontmatter，不按类型拆到 ADR 或 lessons。
- **A4** 迁移文件的正文不改写，只改链接、ADR 编号，以及术语表追加的条目。
- **A5** 迁移文件和入口文件以外的 `.codestable/` 引用不改，`api/openapi.yaml` 也不动。
- **A6** `.claude/`、`.agents/`、`.cursor/` 不在本次范围。
- **A7** tag 由工程师在 merge-base 上创建并推送。被打 tag 的提交已经在远端，这一步只是新增一个名字。
- **A8** `README.md` 视为入口文件，一起改写。
- **A9** 能力需求文档按实际的 12 份迁移。
- **A10** 删掉 `.dockerignore` 里失效的 `.codestable` 行，补 `/var/`。
- **A11** 迁到 `docs/ops/v1-hardening/` 的 3 份运维输入是脚本数据，正文和格式都不改（YAML / JSON 被脚本解析），不纳入 AC1 的 34 份清单和 AC4 的链接扫描；其中指向 `.codestable/` 的文本保持原样。

### 已决问题

Owner 在评论 `01a19c8b-3312-7895-9a4a-6a5f30272ea0` 确认 Q1、Q2 都按推荐答案执行。

- **Q1 `scripts/` 对 `.codestable/` 的运行时依赖**：在本 PR 里解耦——运维输入迁到 `docs/ops/v1-hardening/`；删除只校验 CodeStable 流程证据的检查，保留代码锚点检查；证据默认写到 gitignore 掉的本地目录；门禁加 `make check-ops`。已写入范围第 8 条、AC11、AC13。不保留 `.codestable/` 的部分文件，也不把 3 个自测移出 `test-ops`。
- **Q2 pre-commit 触发范围**：按暂存路径分派后端 / 前端 lint，只改文档或脚本时跳过；缺 `node_modules` 时拒绝提交并提示，不自动安装；lint 本身仍然全量，不引入 lint-staged。已写入范围第 6 条、AC9。

## 技术设计

> 基线：`origin/develop` = `6013c2e1`。下文的行号都以这个提交为准。不改 `backend/`、`frontend/` 代码，不改 `api/openapi.yaml`，不加迁移。

### 1. 方案总览

一个 PR，分支 `refactor/GUAN-3-codestable-to-docs`，目标 `develop`，分成下面 7 个提交。分提交是为了让纯迁移和内容改动分开，评审和 rename 检测都更容易：

| # | 提交 | 内容 | 对应 AC |
|---|---|---|---|
| C1 | `refactor: 长期知识迁入 docs/（纯迁移）` | 34 份文件 `git mv`，3 份 v1-hardening 运维输入 `git mv`。内容一个字节都不改，ADR 009 的文件名在这一步就改好 | AC1、AC2 |
| C2 | `docs: 修复迁移文件链接、ADR 009 编号、术语合并` | 链接改写、ADR 正文编号、glossary 追加 2 个条目、lint 决策末尾加修订指针 | AC2、AC3、AC4 |
| C3 | `docs: 入口文件切换到 Multica + GitFlow` | `CLAUDE.md`、`AGENTS.md`、`GEMINI.md`、`README.md` | AC5、AC6、AC7 |
| C4 | `refactor: scripts 解耦 .codestable` | 第 3 节的全部脚本改动，以及 `docs/ops/planning-backup-restore.md`「Release readiness」退役改写 | AC11、AC13、AC14 |
| C5 | `feat: make hooks 与按路径分派的 pre-commit` | `Makefile`、`scripts/githooks/pre-commit`、`scripts/test-githooks-pre-commit.sh` | AC8、AC9 |
| C6 | `docs: GUAN-3 设计记录` | `docs/design/GUAN-3.md`、`scripts/check-doc-links.py` | AC4、AC12 |
| C7 | `chore: 删除 .codestable/（已归档到 archive/codestable-final）` | `git rm -r .codestable`；`.dockerignore` 删掉第 4 行、补 `/var/` | AC10 |

C1 之所以必须是纯 rename：`VISION.md` 有 89% 的字节落在含链接的行上，改完链接后整体 diff 的相似度会低于 90%，甚至低于 git 默认的 50% 阈值，被当成「删除 + 新增」。所以 AC1 改成在 C1 上检查 `R100`，再用 `git log --follow` 证明可追溯（见「## 变更记录」）。

### 2. 知识迁移与链接修复（C1、C2）

**目标布局**

```text
docs/
  domain/glossary.md                 ← CONTEXT.md + .lti 的 2 个条目
  product/vision.md                  ← VISION.md
  product/capabilities/*.md          ← 12 份能力需求
  adr/001-…008-*.md, 009-separate-plan-revision-from-append-only-execution-facts.md
  decisions/2026-07-0x-*.md          ← 11 篇 compound
  ops/v1-hardening/                  ← 3 份运维输入（见第 3 节）
  design/GUAN-3.md
```

**链接改写规则**（只改这 34 份文件和 4 个入口文件，A4、A5）：

1. Markdown 相对链接 `[..](path)`：按新位置重新计算相对路径。目标已迁移的，指向新路径；目标没迁移的，改成正文里的归档引用（见第 3 条），不保留失效链接。
2. 反引号里的 `.codestable/<迁移来源>`：改成对应的 `docs/...` 路径。
3. 反引号里或 frontmatter 值里的 `.codestable/<未迁移路径>`：改成 `archive/codestable-final:.codestable/<path>`。原文带 `#<locator>` 的保留在引用末尾，写成 `archive/codestable-final:.codestable/<path>#<locator>`。基线上有 2 处：`compound/2026-07-06-accountscope-fail-loud.md:36` 的 `approval-report.md#round-8-share-contract`（反引号），`requirements/account-center.md:10` 的 frontmatter `authorized_by: …approval-report.md#account-center-requirement`。两个 locator 都是 approval-report 里的 YAML 键（基线 `approval-report.md:11`、account-center 的 `approval-report.md:9`），不是 Markdown 标题，所以不能按标题 slug 解析，只能按文本定位。
   - **查阅约定**（写进 `CLAUDE.md` 和设计记录）：`ref='archive/codestable-final:<path>#<locator>'; git show "${ref%%#*}" | grep -n -- "${ref#*#}"`；不带 `#` 的直接 `git show <ref>`。`.codestable/` 下没有文件名含 `#` 的文件（链接检查脚本会断言），拆分无歧义。
4. ADR 009：frontmatter 里的 `id: 005` 改成 `009`，标题行的编号也一起改。`relates_to` 里的 `requirements/...`、`features/...` 是 CodeStable 的标识符，不是路径，保持原样（A4）。
5. `docs/domain/glossary.md`：把 `.lti/contracts/glossary.md` 的「外部助手」「外部助手连接」两个条目原文追加到文件末尾，按原文件的条目格式写，并注明来源。`.lti/` 下的文件不动。
6. `docs/decisions/2026-07-07-decision-lint-gate-pre-commit.md`：在文件末尾追加一行 `> 已被 GUAN-3 修订：pre-commit 按暂存路径分派 lint，见 docs/design/GUAN-3.md。`

基线粗扫的结果：34 份文件里有 29 个 Markdown 相对链接、25 处 `.codestable/`、60 处指向仓库文件的反引号路径。反引号路径里有 2 处现在解析不到：`frontend/dist` 是 gitignore 掉的构建产物，`scripts/githooks/` 由本 PR 新建。改写量是人工可控的。

**链接检查脚本 `scripts/check-doc-links.py`**（AC4，C6）

- 用法：`python3 scripts/check-doc-links.py [--verify-archive] <file>...`。不带参数时，默认扫描 34 份迁移文件和 4 个入口文件（清单写死在脚本里）。
- 检查 1：相对 Markdown 链接（去掉 `#anchor`，跳过 `http(s):`、`mailto:`、纯锚点）能解析到已有文件或目录。
- 检查 2：反引号里的路径，如果第一段是仓库顶层的已有条目，并且不含 `*`、`<`、`{`、`$`，就必须能解析到已有文件；`git check-ignore` 命中的构建产物（如 `frontend/dist`）也算通过。
- 检查 3：每一处 `.codestable/`（正文、反引号、frontmatter 都算）前面都必须紧跟 `archive/codestable-final:`。
- `--verify-archive`：对每一处归档引用，先在第一个 `#` 处拆成 `<path>` 和 `<locator>`；执行 `git cat-file -e archive/codestable-final:<path>`；有 `<locator>` 时再读 blob，要求其中出现该字符串。验证全部引用，不只是 AC4 要求的 3 处。脚本自带 `--self-test`，用上面 2 处真实引用加 1 个不存在的 locator 做正反用例。
- 脚本里识别 `.codestable/` 的正则是静态字面量，不读写该目录，列入 AC13 保留清单。
- 失败时打印 `file:line: reason`，退出码 1。不进 `test-ops`：它是文档检查，GUAN-4 的知识 skill 可以复用它。

### 3. 脚本解耦（C4，Q1）

逐处给出处置。AC13 走「保留清单」分支：完成后 `git grep -n '\.codestable' -- scripts Makefile` 只允许剩下本节末尾「AC13 保留清单」列出的静态字面量，清单原样写进 `docs/design/GUAN-3.md`。

| 位置 | 处置 |
|---|---|
| `scripts/test-v1-ops-contract.sh:7-14` | 改读 `docs/ops/v1-hardening/v1-hardening-design.md`、`v1-hardening-ops-case-catalog.yaml`、`v1-ops-smoke-results.json`。results 平铺放在 `docs/ops/v1-hardening/` 下，不再套 `evidence/ops/` |
| `scripts/lib/v1-ops-smoke-runner.py:28` `CATALOG_REL` | 改成 `docs/ops/v1-hardening/v1-hardening-ops-case-catalog.yaml` |
| `scripts/lib/v1-ops-smoke-runner.py:29` `EVIDENCE_REL` | 改成 `var/evidence/v1-hardening/ops`。`:1641` 写入的 `evidence/ops/<case>.stdout` 是相对 `var/evidence/v1-hardening/` 的标签，保持不变；校验器只检查它是不是相对路径（`v1_ops_results.py:429-431`） |
| `scripts/lib/v1-ops-smoke-results.py:76` | `catalog.path` 改成新路径。校验器只比对 catalog 的 `sha256` 和 case 列表（`v1_ops_results.py:465-468`），不比对路径字符串；catalog 字节不变，所以已提交的 results 仍然能通过（A11：results 内容不改） |
| `scripts/test-auth-security-catalog.sh:94-97,132,163` | 删除 `verify_browser_evidence`、`run_case browser-evidence` 这一行，并把 `require_scenario 11` 里的 `browser-evidence` 去掉。它只 grep CodeStable 实现报告里的一句话，是流程证据，不检查代码 |
| `scripts/lib/planninghardening/conformance.py:24,26-35,88` | 删除 `goal-control` 行（它的代码锚点本身就在 `.codestable/roadmap/.../goal-tools/` 下）、`ACCEPTED_ARTIFACTS` 和 `roadmap_sha256`。行状态改成只看代码锚点：`conformant = present`；去掉 `accepted_artifact_refs` 字段，`negative_probe` 只保留 `assertion`；schema 升到 `planning-v1-dependency-conformance-v2`。`roadmap: "creative-shoot-planning"` 只是标识，保留 |
| `scripts/lib/planninghardening/release.py:40` | `REQUIRED_CONFORMANCE_IDS` 去掉 `goal-control` |
| `release.py:231-270` `_validate_conformance` | 接受 v2：删掉 `roadmap_sha256` 参数和比对、`accepted_artifact_refs` 非空检查、`goal-control` 的 `gate_disposition` 分支 |
| `release.py:808-811` | 不再计算 roadmap 哈希 |
| `release.py:83,126-183` `CANONICAL_ROADMAP`、`_canonical_stage_approvals` | **退役，fail-closed**：删掉常量，函数体改成直接 `raise ContractError("release-stage-approval-source-retired")`，docstring 写明「stage 审批原来记在 CodeStable 的 approval-report，见 `archive/codestable-final:.codestable/roadmap/creative-shoot-planning/`；planning v1 发布前要另开 issue，在 Multica 审批之上重建」。`verify_readiness`（`:914-917`）先调它，所以在读取任何证据之前就失败。selftest 新增一条断言这个错误码。理由见第 7 节 |
| `docs/ops/planning-backup-restore.md:151-206`「Release readiness」 | **同步退役**（评审第 1 轮 Blocking 2）。在节首加退役说明：「入口已退役：stage 审批源原来是 CodeStable approval-report，已随 `.codestable/` 归档；在 Multica 审批之上重建之前，`verify-planning-v1-release-readiness.sh` 对任何证据都返回 9。重建需另开 issue。」`:179-182` 的 canonical approval-report / gate 要求改写为「审批源待重建，当前无可用来源」；`:192-193` 删掉 roadmap bytes 和 canonical stage evidence approval 两项绑定（v2 conformance 已不计算 roadmap 哈希）；`:196-201` 的命令保留但标注「当前必然返回 9」；其余 sibling 清单与结构规则保留，标明是「审批源重建后的候选契约，现由 selftest 夹具覆盖」，不构成放行路径。`:208` 之后「生产侧保留证据」不动 |
| `release.py:201` `_resolve_bound_stage_path` | 删掉 `.codestable/` 分支，一律相对 `index_dir` 解析 |
| `scripts/test-avatar-media-v1-restore-wipe.sh:30-32` | 默认目录改成 `var/evidence/avatar-media-safety-net`；带 profile migration 时用 `var/evidence/account-profile-center`；`AVATAR_MEDIA_WIPE_EVIDENCE` 覆盖照旧可用 |
| `scripts/test-avatar-media-v2-mixed-restore.sh:6,12` | 默认目录改成 `var/evidence/account-center-hardening`，第 6 行注释同步；`AVATAR_MEDIA_V2_MIXED_EVIDENCE` 覆盖照旧可用 |
| `scripts/telegram-smoke.sh:31` | 提示改成「请在真机确认收到，截图贴进对应 issue 评论」 |
| `scripts/check-avatar-media-safety-net-scope.sh` | **整个删除**。它是 2026-08-02 avatar-media-safety-net feature 的分支范围守卫，仓库里没有调用方（`Makefile`、脚本、文档都没有引用它）；它禁止的 account profile 代码现在已经在 `develop` 上，任何碰 account profile 的分支跑它都会失败 |

**AC13 保留清单**（全部是静态字面量，不读写 `.codestable/`，目录不存在时不报错、不重建）：

| 位置 | 内容 | 为什么不会在运行时读写 |
|---|---|---|
| `scripts/lib/planninghardening/release.py` `_canonical_stage_approvals` docstring | 归档引用 `archive/codestable-final:.codestable/roadmap/creative-shoot-planning/` | 注释；函数体直接 raise |
| `scripts/check-doc-links.py` | 识别 `.codestable/` 与 `archive/codestable-final:` 前缀的正则和 `--self-test` 里的 2 处真实引用 | 只对被扫描文件做文本匹配，归档内容经 `git cat-file` 从 tag 读取，不访问工作区 `.codestable/` |

实现时如有新增命中，要么消掉，要么补进这张表并说明理由，否则 AC13 不通过。

**证据目录约定**：手动运行的 ops / rehearsal 脚本默认写到 `var/evidence/<topic>/`。`var/` 已经被 `.gitignore:23` 忽略；`.gitignore` 不作用于 Docker 构建上下文，所以 C7 在 `.dockerignore` 补 `/var/`（评审第 1 轮建议 1），让 `var/evidence/`、`var/hooktest/` 不进入根构建上下文。要长期保留的结论贴进 issue 评论，不入库。这一条写进 `docs/design/GUAN-3.md` 和 `CLAUDE.md`「文档索引」的 ops 那一行。

### 4. `make hooks` 与 pre-commit（C5，Q2）

**Makefile**

```make
.PHONY: … hooks lint-go lint-frontend

hooks:
	git config core.hooksPath scripts/githooks
	@echo "pre-commit 已启用：core.hooksPath=scripts/githooks"

lint: lint-go lint-frontend
lint-go:
	cd backend && golangci-lint run ./...
lint-frontend:
	cd frontend && npm run lint
```

- `lint` 拆成两个前置目标，执行的命令和顺序都和现在一样（不加 `-j` 时 make 按顺序串行执行前置目标），CI 继续调 `make lint`。`lint-frontend` **不依赖** `frontend/node_modules`，和现在的 `lint` 一样，避免 hook 触发 `npm ci`。
- `make hooks` 可以重复执行（`git config` 覆盖写），满足 AC8 的「执行两次都返回 0」。
- `core.hooksPath` 用相对路径：git 在工作树根目录下解析它，所以每个 worktree 用的是自己检出的 `scripts/githooks/`。没有这个目录的旧分支找不到 hook，git 直接跳过。

**`scripts/githooks/pre-commit`**（bash，文件模式 `100755`）

```text
staged = git diff --cached --name-only --no-renames -z   # rename 拆成「删源 + 增目标」，两端都参与分派
need_go       = 任一路径匹配 backend/*.go | backend/go.mod | backend/go.sum | .golangci.yml
need_frontend = 任一路径匹配 frontend/* | .oxlintrc.json
都不匹配 → echo "pre-commit: 只有文档/脚本/配置改动，跳过 lint"; exit 0
need_frontend 且 frontend/node_modules 不是目录 → 报错并提示「先执行 make frontend-install」, exit 1（不自动安装）
need_go       → command -v golangci-lint 不存在则报错退出 1；否则 make -s lint-go
need_frontend → make -s lint-frontend
任一失败 → exit 1，并提示「修复后重新提交；agent 禁止 --no-verify」
```

- **为什么用 `--no-renames`**（评审第 1 轮 Blocking 1）：带 rename 检测时 `--name-only` 只输出目标路径，`backend/x.go` → `docs/x.txt` 这类移动会让源目录少了代码却两个条件都不命中。已在临时仓库复现：默认只输出 `docs/x.txt`，加 `--no-renames` 后输出 `backend/x.go` 和 `docs/x.txt`。不加 `--diff-filter`，删除也计入分派。
- 前端触发条件比规格多了根目录的 `.oxlintrc.json`：它是前端 lint 的配置，改了就应该跑。
- 已知边界：lint 检查的是工作区，不是暂存快照；部分暂存时可能放过未暂存的问题，也可能被未暂存的问题拦住。这和 2026-07-07 决策的「全量 `make lint`」口径一样，不引入 stash/快照（lint-staged 被 Q2 排除）。

**自动化测试 `scripts/test-githooks-pre-commit.sh`**（加进 `test-ops` 末尾）

- 在 `mktemp -d` 里 `git init` 一个临时仓库，把 `scripts/githooks/pre-commit` 拷进去，设置 `core.hooksPath`，再放一个桩 `Makefile`：`lint-go` / `lint-frontend` 只往标记文件里写一行，能用环境变量让它们失败。在 `PATH` 前面放一个桩 `golangci-lint`。
- 覆盖 8 个用例：只改 docs → 两个 lint 都不跑；只改 Go → 只跑 lint-go；缺 `frontend/node_modules` 时改 frontend → 拒绝，输出含 `make frontend-install`，且没有跑 `npm`；lint-go 失败 → 提交被拒；`.golangci.yml` 改动 → 跑 lint-go；`git mv backend/x.go docs/x.txt` → 跑 lint-go；`git mv frontend/src/a.ts docs/a.ts`（`node_modules` 存在）→ 跑 lint-frontend；只删除一个 Go 文件 → 跑 lint-go。
- 不碰真实仓库的 git config。AC9 的主用例（真实 golangci-lint 拦住坏 Go 文件）在 AC8 的独立 clone 里手动跑，命令和输出贴进 PR。

### 5. 入口文件改写（C3）

按需求规格范围第 4、5 条执行。补充几处实现细节：

- `CLAUDE.md`：开头介绍改成「需求、设计、进度在 Multica 项目「光合工坊」；仓库 `docs/` 只保存长期知识」。「工作流」一节删掉 `.codestable` / roadmap / `items.yaml` 三条，保留门禁和 `make generate` 两条。「Git 约定」按规格重写，并加一句：`make hooks` 写的是仓库级 `core.hooksPath`，所有 worktree 共用。「文档索引」表改成 `docs/` 路径：术语 `docs/domain/glossary.md`、ADR `docs/adr/`、能力愿景 `docs/product/vision.md`、提醒引擎 `docs/product/capabilities/reminder-engine.md`、头像 generation 模式 `docs/decisions/2026-07-13-avatar-immutable-generation-pattern.md`、Go / 前端规范决策 `docs/decisions/…`；删掉 roadmap、`items.yaml`、brainstorm、`attention.md` 这几行。新增两条说明，按规格写：`.lti/` 过渡期，以及归档 tag 的查阅方式：`git show archive/codestable-final:<path>`；带 `#<locator>` 的按第 2 节查阅约定去掉定位符再打开、用 `grep` 定位。
- **Multica 注入块**：worktree 里未提交的 `BEGIN MULTICA-RUNTIME` 块不能进提交。改 `CLAUDE.md` 时要以 `git show origin/develop:CLAUDE.md` 为底稿，提交前用 `git diff --cached CLAUDE.md | grep MULTICA` 确认没有带进去（AC5 也会检查）。
- `AGENTS.md`：删掉第 3-51 行的 `<skills_system>` 块（保留 `# AGENTS` 标题）；`## Project` 一节只保留「项目规则手册与文档索引见 [CLAUDE.md](CLAUDE.md)」；从 `## 验证范式` 到文件末尾逐字节不变（AC7 用 `diff <(sed -n '/^## 验证范式/,$p' …)` 检查）。
- `GEMINI.md`：删掉 `attention.md` 那一行；「技能清单与调用方式：AGENTS.md」这一行因为 skills 块已删，改成「验证范式：AGENTS.md」，否则会留下一条失效的指引。
- `README.md:3,5`：按 A8 改写，指向 `docs/product/vision.md`、`docs/` 和 Multica；不再提 roadmap 和 `items.yaml`。

### 6. 归档与删除（C7）

1. 推 tag 之前先确认：`git log --oneline 6013c2e1..origin/develop -- .codestable` 为空。不为空就停下，在 issue 里报告。
2. `git tag -a archive/codestable-final <merge-base> -m "GUAN-3: CodeStable 冻结归档"`，然后 `git push origin archive/codestable-final`。只新增 tag，不动任何分支（A7）。
3. 验证：`git ls-tree -r --name-only archive/codestable-final -- .codestable | wc -l` 等于 1005。
4. C7：`git rm -r -q .codestable`，并删掉 `.dockerignore:4`。C1 已经把 37 个文件移走了，这一步删除剩下的 968 个。

### 7. 备选方案与取舍

- **planning v1 发布就绪校验的 stage 审批源**（最需要 Owner 看的一处）
  - 采用：**退役并 fail-closed**（第 3 节）。现在 `stage-1-evidence-go` / `stage-2-evidence-go` 在 approval-report 里都是 `pending`，所以 `verify-planning-v1-release-readiness.sh` 今天也返回 9。但**流程能力是变了的**：今天 Owner 改 approval-report 就能放行，改完之后仓库里没有任何放行路径，直到审批源重建。这一点同步写进运维指南（第 3 节）和 AC14，不靠「返回码相同」掩盖。selftest 用夹具 binding（`selftest.py:233-245,500-503`）跑完整的结构校验，结构校验的覆盖率不下降。代价：planning v1 将来要发布时，需要先另开 issue，在 Multica 的 Owner 审批之上重新设计 stage 审批的落点。
  - 备选 A：把 `goal-tools/planning-evidence-dispatch-gate.py`、`approval-report.md`、roadmap 一起迁到 `docs/ops/creative-shoot-planning/`。好处是发布门禁照旧能通过改 approval-report 放行；代价是把 CodeStable 的审批记录（39KB）当成活的流程文件留在仓库里，和「评审、批准只在 Multica」冲突。gate 脚本还硬编码了目录名和 `.codestable/` 前缀，也得一起改。不采用。
  - 备选 B：给 `verify_readiness` 加 `--stage-approvals <file>` 显式输入。这等于现在就重新设计一个还没排期的发布流程，属于预支复杂度。不采用。
- **ACCEPTED_ARTIFACTS 换落点 vs 删除**：这些「已验收」证据全部是 CodeStable 的 feature 报告。换到 `docs/` 就等于把流程证据当成长期知识；代码锚点检查保留，足以证明依赖存在。按 Q1 删除。
- **pre-commit 实现**：hook 自己按路径分派，调用 `make lint-go` / `make lint-frontend`，保持「hook 只调 make」的同源原则。备选 lefthook 或 husky：要引入依赖，2026-07-07 决策已经否决。备选 lint-staged：Q2 已排除。
- **归档定位符的表示**：采用「`#<locator>` 留在引用末尾，解析器拆分」。备选是把定位符移出反引号写成正文说明，但 `account-center.md` 的那一处在 YAML frontmatter 里，没法移出；统一一种写法、让解析器拆分更简单。
- **链接检查**：提交成脚本，而不是在设计里写一次性命令。理由是 AC4 要贴结果，评审要能复跑，GUAN-4 也会用到。成本约 80 行 Python，不加依赖。
- **删除 `check-avatar-media-safety-net-scope.sh` vs 只删其中的 `.codestable` 行**：脚本没有调用方，守护的范围也已经失效，只删几行等于保留一个跑了就会失败的死脚本。选择删除。

### 8. 影响面

- **代码 / 契约 / 部署**：不改 `backend/`、`frontend/`、`api/openapi.yaml`、`deploy/`、`docker-compose.yml`、`Dockerfile`，ECS 部署不受影响。`.dockerignore` 原来就忽略 `docs`、`scripts`；本次删 `.codestable` 行、补 `/var/`，构建上下文少了 `var/`（原来就没有被 `COPY`），镜像内容不变。
- **门禁**：`test-ops` 多一个 hook 自测（只用临时仓库，大约几秒）。`check-merge` 会因为 `scripts/` 有改动而跑 `test-ops`，这是预期行为。
- **Owner 主检出与 agent worktree**：合并后 Owner 执行一次 `make hooks`，会对主检出和全部 Multica agent worktree 生效，因为它们共用 `.git/config`。之后 Fullstack Engineer 在 worktree 里提交前端改动前，需要先 `make frontend-install`，这是 Q2 的预期行为。checkout 在旧分支上的 worktree 没有 hook，提交不受影响。
- **本 PR 自己**：工程师不在共享仓库里执行 `make hooks`（只在 `var/` 下的独立 clone 里验证），所以本 PR 的提交不受 hook 约束。本 PR 不含 Go 或前端改动，hook 装上了也会跳过。
- **LTI**：`.lti/` 和 `scene-editor-3d` 都不受影响（规格「边界与异常」已核对）。
- **运维指南**：`docs/ops/planning-backup-restore.md`「Release readiness」改为退役说明；指南其余各节（备份、恢复、rehearsal、生产侧证据）不变。
- **文档外部引用**：`docs/dev/`、`docs/product/`、`DESIGN.md`、`api/openapi.yaml:2` 等处引用 `.codestable/` 的文本会变成需要按归档约定查阅的历史引用（A5）。

### 9. 风险与回滚

- **数据库迁移 / 账号隔离（ADR-001）/ 鉴权**：都不涉及。
- **公开仓库**：迁移的是已经在 git 历史里的文档，没有新增暴露面（GUAN-1 决策 5）。
- **tag**：只新增。打错了由 Owner 删除后重建，agent 不删除远端 tag。
- **回滚**：PR 整体 revert 就能恢复 `.codestable/` 和全部脚本；只想回退 hook 时，执行 `git config --unset core.hooksPath`。
- **兼容性**：`planning-v1-dependency-conformance` 升到 v2，消费方只有同仓库的 `release.py` 和 selftest，这次一起改。仓库外没有落盘的 v1 报告需要兼容，因为发布就绪校验从来没有跑通过。
- **残余风险**：planning v1 发布前必须先重建 stage 审批源（第 7 节），运维指南已如实写明当前没有放行路径。建议 Owner 在「方案确认」时决定是现在就开 backlog issue，还是等 planning v1 排期时再开。

### 10. 测试策略

| AC | 验证方式 | 由谁、在哪个阶段 |
|---|---|---|
| AC1 | `git diff -M --name-status C1^ C1` 有 34 条 `R100`；对每个目标文件执行 `git log --follow` | 工程师贴进 PR；Reviewer 复跑 |
| AC2 | `ls docs/adr`、`head` ADR 009、`git grep -n '00[5]-separate-plan-revision' -- ':!.lti'` 为空 | 同上 |
| AC3 | `grep -c` 两个条目各为 1；检查标题没有重复；`git diff --quiet origin/develop -- .lti/contracts/glossary.md` | 同上 |
| AC4 | `python3 scripts/check-doc-links.py --self-test` 与 `--verify-archive` 退出码都是 0（含 2 处带 `#locator` 的引用） | 同上 |
| AC5 | 规格里的 `git grep` 命令无输出 | 同上 |
| AC6 | 人工核对 `CLAUDE.md`，对照 `attention.md` 处置表 | Reviewer（测试验收）、Owner |
| AC7 | `diff <(git show origin/develop:AGENTS.md \| sed -n '/^## 验证范式/,$p') <(sed -n '/^## 验证范式/,$p' AGENTS.md)` 为空；`grep -c skills_system AGENTS.md` 为 0 | 工程师、Reviewer |
| AC8 | 在 `var/hooktest` 独立 clone 里执行两次 `make hooks`；检查 `git config --get core.hooksPath`；`test -x scripts/githooks/pre-commit` | 工程师贴进 PR |
| AC9 | 自动化：`scripts/test-githooks-pre-commit.sh` 覆盖 8 个用例（含 2 个 rename、1 个删除）（走 `make check-ops`）。手动：在独立 clone 里用真实 golangci-lint 拦住坏 Go 文件，再修好提交 | 自动化进门禁；手动输出贴进 PR |
| AC10 | `git ls-remote --tags origin archive/codestable-final`；`ls-tree` 计数等于 1005；PR head 上 `git ls-files .codestable` 为空 | 工程师、Reviewer |
| AC11 | `make check-ops` 后执行 `test ! -e .codestable` | 工程师贴进 PR |
| AC12 | 检查 PR 元数据 | Reviewer |
| AC14 | selftest 断言 `release-stage-approval-source-retired`（走 `make check-ops`）；手动执行 wrapper 看返回 9；人工核对运维指南「Release readiness」一节 | 工程师贴进 PR；Reviewer 核对指南 |
| AC13 | `git grep -n '\.codestable' -- scripts Makefile` 的结果与「AC13 保留清单」逐条一致；`git check-ignore -q var/evidence/x` 命中；`test-v1-ops-contract.sh` 读的是 `docs/ops/v1-hardening/` | 工程师、Reviewer |

**门禁**（按 `AGENTS.md`「验证范式」判定表）：`scripts/` 有改动，所以跑 `make check-ops`。不跑 `check-go` / `check-frontend`：没有 Go 或前端改动，`Makefile` 只是拆分了 `lint` 目标。为了确认拆分后行为不变，`make lint` 额外跑一次：如果 worktree 没有 `frontend/node_modules`，就只跑 `make lint-go`，并在 PR 里说明。合入 `develop` 时由 Owner 跑 `make check-merge`。

打 `needs-qa` 标签：AC6 需要人工核对，AC8 / AC9 / AC10 依赖真实 git 环境和远端 tag，测试验收时由 Reviewer 复跑。

### 知识回写

| 路径 | 动作 | 内容 |
|---|---|---|
| `docs/design/GUAN-3.md` | 新增 | 本设计的长期部分：目录布局、链接 / 归档引用约定、证据目录约定 `var/evidence/<topic>/`、pre-commit 分派规则及偏离 2026-07-07 决策的理由、planning v1 stage 审批源退役说明、`core.hooksPath` 的 worktree 共享语义、归档引用 `#<locator>` 查阅约定、AC13 保留清单 |
| `docs/domain/glossary.md` | 新增（迁移） | 来自 CONTEXT.md，加上 `.lti` 的 2 个条目 |
| `docs/adr/001-009` | 新增（迁移） | ADR 编号整理；不新增 ADR，理由是流程切换是协作规则，不是架构决策，落在 `CLAUDE.md` 和设计记录里就够了 |
| `docs/decisions/*` | 新增（迁移） | 11 篇；lint 决策末尾加修订指针 |
| `docs/product/vision.md`、`docs/product/capabilities/*` | 新增（迁移） | 能力愿景和 12 份能力需求 |
| `docs/ops/v1-hardening/*` | 新增（迁移） | 脚本读取的运维输入 |
| `docs/ops/planning-backup-restore.md` | 修改 | 「Release readiness」一节改为退役说明，标明审批源重建前无放行路径 |
| `CLAUDE.md` / `AGENTS.md` / `GEMINI.md` / `README.md` | 修改 | 入口规则 |
| `docs/architecture.md`、`docs/contracts/`、`docs/lessons/` | 无 | 本次不新建（规格「不做」），等 `.lti/` 迁移时再建；`core.hooksPath` 共享语义先记在设计记录里 |
