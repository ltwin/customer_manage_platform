---
doc_type: feature-qa
feature: 2026-07-06-platform-skeleton
status: passed
tested: 2026-07-06
round: 1
---

# platform-skeleton QA 报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-design.md`（approved）
- Checklist: `platform-skeleton-checklist.yaml`（steps 全 done，含 S8=done）
- Review: `platform-skeleton-review.md`（round 2，status=passed，reviewer=subagent+ocr）
- Evidence pack / Gate results / DoD results: none（非 goal/gate 模式）
- Diff basis: worktree `.claude/worktrees/platform-skeleton`，分支 feat/platform-skeleton，HEAD=0b9f27c（review round 2 后新增一个 commit：仅归档 A12 TG 截图 + S8 状态回写 + review 报告落盘，零代码文件改动，review 未过期）
- Baseline dirty files: `.codestable/attention.md`（M，owner 新增「提交须人工同意」约定，非本 feature 改动）
- Feature type: **functional**（登录/鉴权/API/账号隔离/部署三形态/TG 冒烟均为运行行为）
- Core evidence gate: A1-A14 + 反向核对 7 项全部要求实际运行证据；本轮已对全部 core 路径取得真机 / 真容器 / 真测试证据（下详）

## 2. Verification Matrix

| ID | 来源 | 核心性 | 场景 / 风险 | 证据类型 | 命令或动作 | 期望 | 结果 |
|---|---|---|---|---|---|---|---|
| QA-001 | A1 / CMD-001 | core-functional | build+lint+test+契约漂移一键全绿 | command | `make check` | exit 0 | **pass** |
| QA-002 | A2-A6 | core-functional | 登录→token→/me 往返 + 各 401/400 封套 | integration | `go test -count=1` httpapi | 全绿 | **pass** |
| QA-003 | A7-A8 | core-functional | healthz 200/503、未注册 404 封套、panic 500 存活 | integration | 同上 | 全绿 | **pass** |
| QA-004 | A9 | core-functional | 双账号 AccountScope 读/写隔离 | integration(容器) | `go test -count=1` store | 全绿 | **pass** |
| QA-005 | A10 | core-functional | 空库 migrate up + 幂等 seed + 缺密码 fail-fast | integration(容器) | 同上 | 全绿 | **pass** |
| QA-006 | A11 / CMD-002 | core-functional | 契约生成物零漂移 | command+diff | `make generate && git diff --exit-code` | 无 diff | **pass** |
| QA-007 | A11 | core-functional | openapi 端点集合双向核对（§4.1+§4.3+§4.6 ∪ {/me}） | diff review | grep + review round2 复核 | 无差集 | **pass**（22 path/29 op，round2 已复核） |
| QA-008 | A12 / CMD-004 | core-functional | TG 冒烟 owner 真机收到消息 | screenshot | `a12-tg-smoke-received.png` | 收到消息 | **pass**（真机截图，含 CRM 冒烟文案+时间戳） |
| QA-009 | A13 | core-functional | 浏览器三段：错密码提示 / 登录见账号 / 未登录跳登录 | screenshot | 3 张 a13-*.png | 三段可辨 | **pass**（详见 §4，附 legibility 说明） |
| QA-010 | A14 / CMD-007 | core-functional | 容器镜像可构建 | command | `docker build -t crm:local .` | exit 0 | **pass**（68.2MB） |
| QA-011 | A14 / CMD-008 | core-functional | compose 全容器 healthz 200 | command | `docker compose up -d --wait` + curl | 200 | **pass** |
| QA-012 | A14 / R2-08 | core-functional | 二进制/容器接现有 PG（--no-deps） | command | 外部 PG + APP_DATABASE_URL + `up --no-deps app` | 连外部库、healthz 200 | **pass**（compose postgres 未起，extdb seed=1） |
| QA-013 | A14 | core-functional | 二进制直跑 go:embed 托管 / SPA fallback | evidence+screenshot | `a14-binary-embed-evidence.md` + png | / 与 /login 返登录页 | **pass**（md5 三方一致，round2 已核） |
| QA-014 | CMD-005 | supporting | 裸 TG token 反查零命中 | command | `! git grep -nE '[0-9]{6,}:[A-Za-z0-9_-]{30,}' -- ':!.codestable'` | 零命中 | **pass** |
| QA-015 | CMD-006 | supporting | .env gitignore 且未跟踪 | command | `git check-ignore -q .env && ! git ls-files --error-unmatch .env` | 通过 | **pass** |
| QA-016 | 反向核对 | supporting | 业务域端点零实现 / 无 RLS / 无 telegram / 无 UI 库 / 无 CI | grep/ls | 见 §3 | 全部零命中 | **pass** |
| QA-017 | review REV-007 | supporting | 四路由旁路实际行为 | API | 4 条 curl 探测 | 记录行为 | **pass（记录，非阻塞）** |

## 3. Command Results

- `make check` → exit 0（build+lint+test+generate-check 全绿；lint 含 golangci-lint depguard + oxlint）
- `go test -count=1 ./internal/platform/...`（强制绕过缓存）→ httpapi 8 测试 + store 7 测试全 PASS（store 测试起真实 PG 容器：TestAccountScopeIsolation 2.44s、Idempotent seed 1.18s、fail-fast 1.16s 等）
- `make generate && git diff --exit-code -- api.gen.go schema.d.ts` → 无 diff（在 make check 内执行）
- `docker build -t crm:local .` → exit 0，image 68.2MB
- `docker compose up -d --wait` → postgres+app 均 Healthy；`curl /healthz` → `{"status":"ok"}` rc=0；`down` 清理干净
- R2-08 接现有 PG：外部 PG 容器（extdb，端口 5455）+ `APP_DATABASE_URL` + `compose up -d --no-deps --wait app` → 仅 app 运行（compose postgres 未拉起），extdb `SELECT count(*) FROM accounts` = 1（app 完成 migrate+seed 于外部库），healthz 200
- REV-007 旁路探测（运行容器上）：`GET /api/v1/me/` → 301 text/html；`GET /api` → 200 index.html；`GET //api/v1/me` → 200 index.html；`POST /whatever` → 200 index.html。对照：`GET /api/v1/customers` → 404 `{"error":{"code":"not_found",...}}`
- 反向核对：路由注册仅 healthz/auth-login/me（grep router.go）；迁移 SQL 无 POLICY/ROW LEVEL；backend/*.go 无 telegram import；frontend/package.json 无 antd/mui/chakra/element-plus；无 .github / ansible / terraform
- CMD-005 → PASS（零命中）；CMD-006 → PASS

## 4. Scenario Results

- [x] QA-001~007 后端/契约：全部 pass。测试非缓存复跑，15 个测试真实通过，含 testcontainer 隔离与迁移
- [x] QA-008 A12 TG 冒烟：pass
  - Evidence: `a12-tg-smoke-received.png`（1.46MB 真手机截图，Telegram 会话收到「CRM 平台基座 TG 冒烟：<时间戳>」）
  - Notes: S8 由 commit 0b9f27c 闭环，CMD-004 core 命令实质满足（owner 真机执行）
- [x] QA-009 A13 浏览器三段：pass
  - Evidence: a13-1（错密码，14.5KB）/ a13-2（登录成功见账号，21.8KB）/ a13-3（守卫跳登录，12.2KB）
  - Notes: 三图为浅色极简 UI，缩略后视觉偏白，但**字节尺寸随内容量递增可区分三态**（成功页含账号数据最大 > 错误页含提示 > 裸登录页最小），非同一空白文件；A14 证据链已证实 SPA 与 assets 正常托管。legibility 偏低登记为 residual
- [x] QA-010~013 A14 三形态：全部 pass（容器构建 / compose 全容器 / 接现有 PG / 二进制 go:embed），首次取得三形态真运行证据（review 两轮均未实跑）
- [x] QA-017 REV-007 四旁路：pass（记录，非阻塞）
  - Evidence: 见 §3 探测输出
  - Notes: 四条均返回静态页/重定向，**不触达受保护数据、无鉴权绕过**；已注册 API 路径正确 404 封套。属 round1 nit，owner 决定「修 or 接受偏差」

## 5. Findings

### failed

none

### blocked

none

### residual-risk

- REV-003（owner round1 已接受）：compose PG 端口 0.0.0.0 + 弱默认口令回退——owner 声明本地测试、云上改密码、发布端口为远程访问所需。真实部署若 .env 漏配 `POSTGRES_PASSWORD` 即弱口令公网可达，归 v1-hardening / 部署实操复核
- REV-007（round1 nit，本轮已复现）：四路由旁路（`/api/v1/me/` 301、`/api`、`//api/v1/me`、非 GET 非 API 路径 → 200 index.html）。无安全影响，但 checklist「ErrorEnvelope 是唯一非 2xx 结构」核对项按字面在这些边缘路径不成立。建议 owner 拍板修（RedirectTrailingSlash=false + 前缀判断精确化 + fallback 限 GET/HEAD）或明确记录为接受偏差
- REV-004（round2 residual）：`http.Server` 有 ReadHeaderTimeout=5s 但无 IdleTimeout；空闲 keep-alive 连接占 fd，severity 低于 Slowloris，登记 v1-hardening
- A13 截图 legibility 偏低（浅色 UI 缩略偏白）：内容按字节尺寸可区分，但肉眼细节弱；如需强证据可重截带地址栏 + 放大。非核心路径阻断
- design 已登记安全残留：无登录限速、明文 HTTP、JWT 30 天窗口、localStorage token XSS 面——归 v1-hardening
- R2-07（review round2）：feat 分支历史含已删除的 1.5MB 二进制 blob，合并 develop 须 squash/rebase，否则永久进共享历史。合并决策点须知情
- store 测试每用例起独立 PG 容器（本轮实测 6 个容器 / 轮，store 包 8.6s），随域 feature 线性增长，登记趋势

## 6. Cleanliness

- Debug output: **pass**（backend 无 fmt.Println，frontend/src 无 console.*；`telegram-smoke.sh:16` 的 `print(chat["id"])` 是 Python 单行脚本回传 chat_id 给 shell 的功能输出，非调试残留）
- Temporary TODO/FIXME/XXX: **pass**（backend/frontend/src/scripts 零命中）
- Commented-out code: **pass**（未发现）
- Unused imports / dead code: **pass**（golangci-lint goimports + oxlint 纳入 make check，已全绿）
- Out-of-scope files: **pass**（本 feature 交付物均在 design 挂载点清单内）
- 禁用词「用户」: **pass**（backend/frontend/src 零命中）

## 7. Verdict

- Status: **passed**
- Next: `cs-feat-accept`
- 提醒 acceptance：① 合并 develop 用 squash（R2-07）；② REV-007 四旁路请 owner 拍板「修 or 接受偏差」，若接受则移入 acceptance residual；③ REV-003 owner 已接受，随安全残留清单提示；④ 后续 commit 按 attention.md 新约定须人工同意
