---
doc_type: feature-review
feature: 2026-07-06-platform-skeleton
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-06
round: 2
---

# platform-skeleton 代码审查报告（round 2 复审）

> round 1（2026-07-06，status: changes-requested，reviewer: subagent）产出 REV-001~014；review-fix（commit e9e6b10）声称修复 REV-001/002（blocking）与 REV-004/005/006（important），REV-003 owner 拍板接受。本轮复审裁定修复有效性，并用新增的 ocr CLI 补跑环节 B 行级扫描。round 1 报告全文见 git 历史（e9e6b10 引入版本）；本文件为 round 2 定稿。

## 1. Scope And Inputs

- Design: `platform-skeleton-design.md`（approved，未变）
- Checklist: `platform-skeleton-checklist.yaml`（S1-S7/S9/S10 done；S8 TG 冒烟仍 pending，owner 外部依赖）
- Evidence pack / Gate results / DoD results: none（非 goal/gate 模式）
- Implementation evidence: review-fix 汇报（对话内）+ commit e9e6b10 + `evidence/a14-binary-embed-evidence.md`（新增证据链）
- Diff basis: 复审重点 = `b4bde0b..HEAD`（3254102 纯 attention.md 文档 + e9e6b10 review-fix 本体）；环节 B 扫描范围 = `5b19e58..HEAD` 全量 feature diff
- Baseline dirty files: `.codestable/attention.md`（用户未提交的新增约定「一个 feature 一个提交、提交须人工同意」——非本 feature 改动，不计入审查范围；该约定自本轮起生效：review 侧不再自动 commit）

### Independent Review

- Detection: Paseo 不可用；原生 Claude Task agent 可用；`which ocr` + `ocr llm test` 通过（gpt-5.5 backend，与主 agent 异构）
- 环节 A 独立隔离 Task agent: native-agent + completed（独立上下文，只给原始材料与 round 1 报告，未透露主 agent 裁定）
- 环节 B OCR CLI: completed（`ocr review --audience agent --from 5b19e58 --to HEAD` + spec background；输出 4 条 finding，无 High/Medium/Low 标签，由主 agent 按内容定级）
- OCR severity mapping: 3 条核验属实 → nit；1 条纯风格（Promise 链 vs async/await）→ Low 噪音丢弃；无可升级为 blocking/important 的项
- Merge policy: 两环节全部返回后定稿；每条经主 agent 本地事实核验（含 openapi 204 反查、git ls-files/md5 复验）
- Gate effect: none（双环节完成，`reviewer: subagent+ocr`）

## 2. Round 1 Findings 裁定

| 编号 | round 1 严重度 | 裁定 | 核验证据（环节 A + local 双重验证） |
|---|---|---|---|
| REV-001 入库二进制 | blocking | **resolved** | `git ls-files backend/server` 零命中；`.gitignore` `/backend/server` 实测 check-ignore 命中。尾巴：1.5MB blob 仍在分支历史（→ R2-07） |
| REV-002 A14 证据复制 | blocking | **resolved** | 新截图 md5 与 a13-3 不同；证据链多环可复验——served asset md5 == frontend/dist == go:embed 输入目录三方一致，`backend/bin/server` 二进制 strings 含 embed 资产路径，时间线自洽。字面偏差：无地址栏截图，以指纹链 + 请求日志 + 5173 无监听替代，溯源强度更高，判定满足意图 |
| REV-003 PG 端口 0.0.0.0 | important | **owner 接受不修**（round 1 已拍板） | compose 保持 `"5432:5432"`；残留面见 Residual Risk |
| REV-004 无 HTTP 超时 | important | **resolved** | `main.go:59-64` `ReadHeaderTimeout: 5s`，与建议边界一致；优雅停机留 v1-hardening（注释登记）。衍生观察 → R2-04 |
| REV-005 compose 接现有 PG 失效 | important | **resolved** | `${APP_DATABASE_URL:-…}` 双路径经 `docker compose config` 实测正确（缺省→compose 内 postgres；设值→精确覆盖；`:-` 使空值安全）；README 补 `--no-deps` 同时解决 depends_on 连带拉起。无实跑证据 → R2-08 归 QA |
| REV-006 AccountScope SQL 片段契约 | important | **resolved** | doc comment 契约 + `identPattern` 校验，四方法均在触达 pool 前短路；新测试用 nil pool 构造——校验被删即 panic，非假阳性（实跑 PASS）；全仓现有调用点无误杀；`$` 无换行绕过；大写拒绝是契约设计 |
| REV-007~011（nit）/ REV-012~014（suggestion） | — | **未动，符合预期** | fix diff 未触碰对应文件，review-fix 未扩大范围；全部结转 QA/后续 |

## 3. Adversarial Pass（针对 fix diff 本身）

- 假设：修复引入新问题——标识符正则误杀、compose 插值兼容性、超时误伤、证据链伪造缝隙
- 攻击结果：全部未升级为 blocking/important。误杀面核验通过（多列 split+trim 正确、现有调用点全过）；compose 嵌套插值 v2 即支持（`env_file` 长语法需 v2.24+ → R2-06）；ReadHeaderTimeout 只约束请求头读取，不影响慢 body 合法请求；证据链剩两条自报文本（served md5 行、5173 无监听）与全部可复验环节零矛盾，采信

## 4. Findings（round 2 新增）

### blocking

none

### important

none

### nit

- [ ] R2-01 `evidence/a14-binary-embed-evidence.md:6` 标题「启动日志（migrate → ensure → 监听）」下实际只有「HTTP 监听」一行（migrate/ensure 本就不产日志）；措辞失实但证据效力不受影响 ｜native-agent
- [ ] R2-02 流程缝隙：round 1 review 报告首次入库混在 fix commit e9e6b10 里，报告的 pre-fix 状态无独立 git 锚点；本轮靠内容交叉印证采信。后续轮次 review 报告应先独立提交再触发 review-fix（与 attention.md 新约定「提交须人工同意」配合执行）｜native-agent
- [ ] R2-03 `scope_internal_test.go:36-39`「合法标识符不误杀」分支直接调 `validateIdents` 并复制 Query 内部 split 逻辑，Query 切分逻辑变更时该分支不会察觉 ｜native-agent
- [ ] R2-09 `frontend/src/api/client.ts:43` 共享 request helper 对 204/空 body 响应会 `res.json()` 抛 SyntaxError——契约已有 2 处 204（DELETE identities / DELETE schedule slots，openapi.yaml:303,762），当前调用面（login/me）不触发，第一个 DELETE 域 feature 必踩（fail-loud，修复 trivial）｜ocr（本地核验属实）
- [ ] R2-10 `frontend/src/App.tsx:11` 守卫重定向 state 只保留 `location.pathname`，丢 query/hash；当前仅 `/` 一个受保护路由无影响，域 feature 带参数路由后登录回跳会丢参 ｜ocr（本地核验属实）
- [ ] R2-11 `scripts/telegram-smoke.sh:10-18` getUpdates 取任意 chat 的最新消息——bot 被拉群或他人发消息时冒烟会发错 chat；且未检查响应 `ok:false`。一次性冒烟脚本、owner 本人执行，风险低；可支持 `TELEGRAM_CHAT_ID` 显式覆盖 ｜ocr（本地核验属实）

### suggestion

- [ ] R2-05 `identPattern` 已知演进点：columns 无法表达 `*`/`count(*)`/别名/`public.foo` 限定名；customer-core 第一个聚合查询会撞上，届时显式扩展 validateIdents（fail-loud 是设计意图），不得绕开 AccountScope ｜native-agent
- [ ] R2-06 compose 文件有效版本地板 = Docker Compose v2.24+（`env_file` 长语法 `required: false`）；README 可补一句版本要求 ｜native-agent
- （OCR 第 4 条「HomePage Promise 链改 async/await」为纯风格偏好，按 Low 噪音丢弃，不进报告计数）

### learning

- round 1 两条 learning 结转（AccountScope 返回 pgx.Rows 的 depguard 演进点；oapi-codegen readOnly+required 产指针）
- `ocr --audience agent` 模式输出无优先级标签的 diff 建议，需主 agent 自行定级后合并——与 skill 假定的 High/Medium/Low 映射略有出入，已按内容实质定级

### praise

- review-fix 范围纪律好：只动 5 个 REV 的边界文件，round 1 的 nit/suggestion 一个没顺手改
- REV-002 的补证方式（资产 md5 三方指纹 + 二进制 strings 物证 + 请求日志时间线）比原要求的地址栏截图更难伪造，值得作为后续「运行形态证据」的范式

## 5. Test And QA Focus

- QA 必须重点复核（round 1 项全部继承 + round 2 新增）：
  1. **接现有 PG 形态实跑**（R2-08）：独立 PG + `.env` 设 `APP_DATABASE_URL` + `docker compose up -d --no-deps --wait app`，确认无 compose postgres 被拉起、app 连的是外部库
  2. CMD-007/008 原文复跑（compose 全容器轨两轮均未实跑，仅静态审查）
  3. REV-007 四个路由旁路逐条打（`/api/v1/me/`、`/api`、`//api/v1/me`、POST 非 API 路径），修或记录接受偏差
  4. `.env` 缺 `POSTGRES_PASSWORD` 场景（REV-003 残留面）；全新环境 `make check`
  5. S8 TG 冒烟（CMD-004，仍 pending）
- 建议新增或加强的测试：异 secret 伪造 token 的 401 用例；panic 路径请求日志断言（暴露 REV-009）；若日后调整 Query 列切分，先修 R2-03 使测试穿过 Query 本体
- 不能靠 review 完全确认的点：Dockerfile 构建 / compose 冒烟（未执行）；A13 截图对应关系（采信）；TG 冒烟

## 6. Residual Risk

- **R2-07（REV-001 尾巴）**：1.5MB blob 仍在 feat/platform-skeleton 历史中——合并 develop 必须 squash / rebase，否则永久进共享历史；merge 决策点须知情
- **R2-08（REV-005 尾巴）**：「接现有 PG」形态仅静态验证，无运行时证据，归 QA 第 1 项
- **R2-04**：`http.Server` 无 `IdleTimeout`（ReadTimeout=0）——空闲 keep-alive 连接无限期占 fd；severity 低于 Slowloris（需先完成合法请求），round 1 只要求 ReadHeaderTimeout 故不算未解除；与优雅停机同批登记 v1-hardening
- **REV-003（owner 接受）**：PG 端口 0.0.0.0 + 弱默认口令回退维持现状；真实部署若 `.env` 漏配 `POSTGRES_PASSWORD` 即弱口令公网可达，部署实操时复核
- **S8/A12 TG 冒烟 pending**：验收（DOD-QA/ACCEPT）前必须补跑或 owner 明确处置
- design 已登记安全残留照旧（无登录限速、明文 HTTP、JWT 30 天、localStorage token XSS 面）
- store 测试容器数随域 feature 线性增长（登记趋势）

## 7. Verdict

- Status: **passed**（round 2；blocking 两条 resolved，important 三条 resolved + 一条 owner 接受；round 2 新 findings 均为 nit/suggestion，不阻塞）
- Next: 进入 **`cs-feat-qa`**。QA 输入 = 本报告第 5 节 + checklist checks（40 条 pending）+ DoD 命令表。三个提醒随行：① 合并 develop 用 squash（R2-07）；② S8 TG 冒烟仍 pending；③ 按 attention.md 新约定，后续任何 commit 先征得 owner 同意
