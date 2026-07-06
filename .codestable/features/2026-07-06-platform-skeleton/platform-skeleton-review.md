---
doc_type: feature-review
feature: 2026-07-06-platform-skeleton
status: changes-requested
reviewer: subagent
reviewed: 2026-07-06
round: 1
---

# platform-skeleton 代码审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-design.md`（doc_type=feature-design，status=approved，feature 一致）
- Checklist: `.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-checklist.yaml`——**S1-S7、S9、S10 done；S8（TG 冒烟）pending**，属 design 明示的外部依赖阻塞（owner 提供 bot token），design 允许其不阻塞其余步骤。本轮 review 范围 = S1-S7、S9、S10 的实现；S8/A12 登记为 residual risk，验收前必须补。
- Evidence pack: none（非 goal/gate 模式）；步骤证据 = `evidence/` 4 张截图 + commit 记录
- Gate results / DoD results: none
- Implementation evidence: 10 个 commit（e60996c…b4bde0b），commit message 与 checklist 步骤一一对应
- Diff basis: worktree `.claude/worktrees/platform-skeleton`，分支 `feat/platform-skeleton`，`main..HEAD` 10 commits，66 文件 +7780/−9；`git status --short` 干净，无未提交改动
- Baseline dirty files: none

### Independent Review

- Detection: 无 `mcp__paseo__create_agent`（Paseo 不可用）；原生 Claude Task agent 可用；`which ocr` 失败（OCR CLI 未安装）
- 环节 A 独立隔离 Task agent: native-agent + completed（同宿主同模型，异构降级已知；agent 独立上下文、只给原始材料，未透露主 agent 结论）
- 环节 B OCR CLI: not-available（未安装；如需启用走 `cs-onboard` 的 open-code-review 段）
- OCR severity mapping: n/a
- Merge policy: 环节 A 全部 finding 已逐条本地事实核验后合并（B1/B2/I1/I2 本地独立复现；N1-N5 经代码阅读核验逻辑成立）；本地新增 REV-005（compose 接现有 PG 失效）为主 agent 独立发现
- Gate effect: 环节 A 已完成，verdict 可定稿；OCR 缺席不阻塞（B 环节非 gate 必需）

## 2. Diff Summary

- 新增：`backend/`（Go module：cmd/server、cmd/migrate、internal/platform/{auth,config,httpapi,store,webui}）、`frontend/`（Vite+React+TS：登录页、守卫路由、API client）、`api/openapi.yaml`（22 路径 / 30 操作）、根级 Makefile、`.golangci.yml`（depguard 双规则）、Dockerfile、docker-compose.yml、`.env.example`、`scripts/telegram-smoke.sh`、README、迁移目录（0001 accounts + 测试探针表）、`.codestable/features/.../evidence/` 4 张截图
- 修改：checklist 状态回写、`.gitignore`
- 删除：none
- 未跟踪 / staged：none
- 风险热点：权限/数据隔离（AccountScope、JWT）、部署面（compose/Dockerfile）、**方案外文件（`backend/server` 1.5MB 编译产物入库）**、证据完整性（A14 截图与 A13-3 重复）

## 3. Adversarial Pass

- 假设的生产 bug：账号隔离基座或鉴权链存在可绕过路径；部署工件在真实 ECS 上有安全暴露
- 主动攻击过的反例：JWT alg 混淆 / 空 sub / 过期与篡改 token（防御到位）；AccountScope 空 scope、跨账号读写（测试真实覆盖，含写路径）；SQL 注入面（当前调用点全为编译期字面量，无注入）；路由旁路（发现 4 个封套不变量旁路，nit 级）；seed 并发窗口（单实例部署下纯理论）；证据链核验（**发现 A14 截图为 A13-3 的字节级复制**）
- 结果：升级为 findings——REV-001/002（blocking）、REV-003~006（important）、REV-007~011（nit）；留给 QA focus——A14 三形态真跑、N1 旁路逐条打、compose 弱口令场景

## 4. Findings

（来源标注：`native-agent` = 独立 Task agent，`local` = 主 agent 本地审查；所有 native-agent 结论均已本地核验。）

### blocking

- [ ] REV-001 `backend/server` 1.5MB 编译产物（Mach-O arm64）被提交入库 ｜来源：local + native-agent
  - Evidence: `git ls-files` 命中 `backend/server`；引入于 e60996c（S1/S2）；Makefile 构建输出是 `backend/bin/server`（已被 `.gitignore` 覆盖），此文件是杂散 `go build -o server` 产物，`.gitignore` 未覆盖该路径
  - Impact: 方案外交付物（design 交付物清单无此项），S10「清洁度清扫」声称与仓库事实矛盾；合入 develop 后 1.5MB blob 永久进共享历史；且是一个会持续过期的假部署工件
  - Expected fix scope: `git rm backend/server` + `.gitignore` 补 `/backend/server`；feature 分支尚未共享，建议合并前 rebase/squash 掉该 blob 而非仅追加删除 commit
- [ ] REV-002 `evidence/a14-binary-embed-login.png` 与 `a13-3-guard-redirect-login.png` 字节级相同（md5 均 fb186e1f…）｜来源：local + native-agent
  - Evidence: 两文件 checksum 一致 = 文件复制，非两次独立截屏；截图无地址栏，无法区分来源（Vite dev server vs 二进制直跑）
  - Impact: S9 退出信号「二进制直跑访问 `/` 返回登录页（go:embed 生效，截图）」未被真实证明，checklist S9 标 done 的证据链断裂，A14/DOD-IMPL-001 验收可信度不成立（go:embed 代码本身阅读无误，但"看起来对"不是验收口径）
  - Expected fix scope: `make build && ./backend/bin/server` 真跑二进制轨，浏览器访问 `/`（建议顺带深链 `/login`），**带地址栏**重新截图归档替换

### important

- [x] REV-003 `docker-compose.yml:34-35` postgres 端口 `"5432:5432"` 发布到所有网卡 ｜来源：native-agent（本地核验属实）｜**owner 2026-07-06 明确接受，不修**
  - Evidence: design D6 明确 compose 同时是生产容器轨；ECS 公网机上 `docker compose up` 即把 PG 暴露公网，且 `POSTGRES_PASSWORD` 有弱默认回退 `crm-dev-password`（compose:32）——.env 漏配该 key 时生产库 = 弱口令公网可达
  - Impact: 数据安全边界；这是本次实现最接近"必然踩中的生产事故"的一处
  - 处置: owner 拍板——本地仅测试用途；0.0.0.0 发布是为了远程访问数据库的刻意选择；真实云上部署时会修改密码。已移入 Residual Risk，验收时随安全残留清单一并提示
- [ ] REV-004 `backend/cmd/server/main.go:56` `http.ListenAndServe` 无任何超时 ｜来源：local + native-agent
  - Evidence: 公网直挂（design 无反代形态），无 `ReadHeaderTimeout` 等 = Slowloris 慢连接可耗尽 fd；design 健壮性档位 L3
  - Impact: 可用性；修复极小（`http.Server{ReadHeaderTimeout: …}`）；优雅停机可留 v1-hardening，超时不建议留
- [ ] REV-005 compose 容器轨「接现有 PG」形态失效，README 指引与实际行为不符 ｜来源：local
  - Evidence: `docker-compose.yml:14` `environment` 块硬编码 `DATABASE_URL` 指向 compose 内 `postgres` 服务，而 Compose 语义中 `environment` 恒覆盖 `env_file`——用户按 README:60「`docker compose up -d --wait app`（DATABASE_URL 指过去）」操作时 .env 里的外部 PG 地址永远不生效；且 `depends_on: postgres`（compose:18-20）会把 postgres 服务一并拉起（需 `--no-deps` 才不启）
  - Impact: design D6 明确承诺「接现有 PG 时只跑 app」，该形态当前不可用（A14 只验了另三种形态，未覆盖此路径）；用户会静默连上 compose 的 PG 而误以为连的是自己的库
- [ ] REV-006 `store/scope.go:37,60,74,90` AccountScope 的 table/columns/cond/setClause 经 `fmt.Sprintf` 拼进 SQL，接口契约未写死；占位符手工编号数错不报错 ｜来源：native-agent + local
  - Evidence: 当前所有调用点均为编译期字面量、值参数全走占位符，**今天没有注入**；但该 seam 是全部后续域 feature 必经之路，接口形状邀请把请求派生字符串传进 cond——一旦发生即隔离层内 SQL 注入；Update/Delete 的 `$2` 起手工编号（scope_test.go:90）数错一位静默作用到错误值/行
  - Impact: 未来缺陷温床，非当前 bug；可接受延后但须登记
  - 建议边界: doc comment 写死「四个 SQL 片段参数必须编译期常量，禁止拼接请求输入」+ 廉价标识符校验（`^[a-z_][a-z0-9_]*$`）；编号问题域 feature 铺开前再加固

### nit

- [ ] REV-007 封套不变量存在 4 个旁路（native-agent 实测）：`GET /api/v1/me/`→301 text/html（gin RedirectTrailingSlash 默认开）；`GET /api`→SPA fallback（前缀判断是 `"/api/"`，router.go:53）；`GET //api/v1/me`→SPA fallback（双斜杠绕过前缀，无鉴权绕过）；`POST /非API路径`→200 index.html（fallback 未限 GET/HEAD）。均无安全影响，但 checklist「ErrorEnvelope 是唯一非 2xx 结构」核对项按字面会挂——修（RedirectTrailingSlash=false + 前缀补 `== "/api"` + fallback 限 method）或明确记录为接受偏差
- [ ] REV-008 `store/migrate.go:31` 成功路径 `sql.Open` 的池未关（`m.Close` 只关 driver conn），进程终身挂 1 个闲置连接；一次性启动调用，影响极小
- [ ] REV-009 `httpapi/middleware.go:34-49` requestLog 无 defer，panic 请求缺 method/path/status/耗时日志行（只有 recovery 的 error 日志）；日志逻辑放 defer 即修
- [ ] REV-010 `auth/seed.go:24-43` seed 非事务，双实例同时首次启动理论上可产生 2 个账号；`FirstAccount` 按 created_at ASC 同刻并列时不确定。单实例部署下纯理论，登记即可
- [ ] REV-011 openapi `Account` 标 `required: [id, created_at]` 但 oapi-codegen 因 readOnly 产出 `*string`/`omitempty`（api.gen.go:17-20）；handler 恒赋值故运行时正确，后续域实体大量走生成模型时注意此行为

### suggestion

- [ ] REV-012 路由注册未用 codegen 的 `RegisterHandlers`（router.go:45-47 手写路径），`var _ ServerInterface` 只对齐方法签名、路径与方法仍靠人眼；域 feature 增多后建议改 `RegisterHandlersWithOptions`（auth 豁免用 per-route middleware），把「路由 == 契约」变成机器事实
- [ ] REV-013 healthz 的 `db.Ping` 无独立超时（router.go:87），DB 假死时探测悬挂；包 1-2s `context.WithTimeout` 更干净
- [ ] REV-014 README:67 改密说明建议补一句：改密必须搭配轮换 `AUTH_TOKEN_SECRET`，否则已泄漏旧 token 在 30 天窗口内仍有效

### learning

- `AccountScope.Query` 返回 `pgx.Rows`，域代码**调用**它不触发 pgx import，depguard 断言依然成立；但域包一旦要在签名/字段里**命名**该类型就会被 depguard 逼出 store 层包装类型——customer-core 落地时预期撞到，届时是演进不是缺陷
- oapi-codegen 对 readOnly+required 字段产出指针类型（REV-011），是工具行为不是转写失真

### praise

- JWT 解析双保险（token.go:47-52）：`WithValidMethods([HS256])` + keyfunc 内再断言 HMAC，alg=none / RS256 混淆攻击面关死；空 sub 拒绝是好的纵深
- 隔离基座测试超出 design 要求：A9 只要求读隔离，实现补了写路径隔离（B 的 scope Update/Delete A 的行 → 0 行受影响）和空 scope 防御；独立 agent 在真实 PG 容器复跑通过，且断言「精确可见自己那一条」——WHERE 过滤被删时测试必然失败，不是假阳性测试
- 契约转写忠实度高：端点集合双向核对零差集（§4.1+§4.3+§4.6 ∪ {GET /me}），`account_id` 全部 readOnly 且带 ADR-001 说明，409 子码落位正确
- 清洁度全绿：CMD-005/006 复跑通过；全仓零「用户」、零调试输出、零 TODO/FIXME/注释代码；`.env.example` 占位值显式标注禁用于生产

## 5. Test And QA Focus

- QA 必须重点复核：
  1. **A14 三形态逐一真跑**（REV-002 直接后果）：二进制直跑轨 `make build` → 直跑 → 访问 `/` 与深链 `/login`，截图带地址栏；CMD-007/008 原文执行（本轮 review 未执行 docker build / compose 冒烟，不背书）
  2. **REV-007 四个路由旁路**逐条打一遍，与 owner 确认修或记录为接受偏差——别让 checklist 封套核对项静默通过
  3. **REV-003/005 部署场景**：模拟 .env 缺 `POSTGRES_PASSWORD` 的 `docker compose up`；按 README「接现有 PG」指引操作一遍，确认连的到底是哪个库
  4. A1 `make check` 在全新环境（无 node_modules/缓存）一键全绿
- Evidence pack residual risks / gate warnings：n/a（无 evidence pack）
- 建议新增或加强的测试：401 用例补**异 secret 伪造 token**（现有「篡改」是尾部加字符）；panic 路径断言存在请求日志行（立刻暴露 REV-009）；若修 REV-007，四个旁路各加回归用例
- 不能靠 review 完全确认的点：Dockerfile 构建与 compose 全容器冒烟（未执行）；A13 三张截图与真实浏览器行为的对应（无地址栏，只能采信）；TG 冒烟（S8 未执行）

## 6. Residual Risk

- **S8/A12（TG 冒烟）pending**：CMD-004 core 命令阻塞在 owner 前置动作（BotFather 建 bot + 发消息），checklist 如实标注；验收（DOD-QA-001/DOD-ACCEPT-001）前必须补跑或由 owner 明确处置
- **REV-003（owner 接受）**：compose PG 端口发布 0.0.0.0 + 弱默认口令回退保持现状——owner 声明本地仅测试、云上部署改密码、发布端口为远程访问所需。残留面：真实部署动作里若 .env 漏配 `POSTGRES_PASSWORD`，弱默认口令即公网可达；归 v1-hardening / 部署实操时复核
- design 已登记的安全残留照旧成立（无登录限速、明文 HTTP、JWT 30 天窗口、seed 密码驻留 env）；**追加一条 design 未列的**：token 存 localStorage（frontend/src/auth/token.ts），XSS 即可外带——建议随 v1-hardening 评估 httpOnly cookie
- REV-001 修复后 feature 分支历史仍含 1.5MB blob，除非合并前 rebase/squash；merge 决策须知情
- store 测试每用例起一个 PG 容器（当前 6 个/轮），`make check` 时长随域 feature 线性上涨——登记趋势，暂无动作
- 独立 Task agent 与主 agent 同模型（Paseo 不可用），异构审查降级；OCR 环节缺席（未安装）

## 7. Verdict

- Status: **changes-requested**（2 条 blocking：REV-001 入库二进制、REV-002 A14 证据复制）
- Next: 触发 `cs-feat-impl` 的 **review-fix 模式**，只修 REV-001/REV-002（important REV-003~006 是否同批修由 owner 决定——建议 REV-003 同批，成本一行）；修完**必须重跑 `cs-code-review` 复审**，复审通过后去向 = `cs-feat-qa`。S8（TG 冒烟）与 review-fix 并行推进，不互相阻塞
