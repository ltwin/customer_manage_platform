---
doc_type: feature-design-review
feature: 2026-07-06-platform-skeleton
status: passed
reviewed: 2026-07-06
round: 3
---

# platform-skeleton feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-design.md`
- Checklist: `.codestable/features/2026-07-06-platform-skeleton/platform-skeleton-checklist.yaml`
- Intent / brainstorm: none（feature 目录无 intent/brainstorm；roadmap 级脑暴 `.codestable/brainstorms/photographer-private-crm/brainstorm.md` 作背景输入）
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`（§3/§4/§5 为硬约束）+ items.yaml（条目 platform-skeleton，已回写 in-progress）
- Related docs: `requirements/CONTEXT.md`、ADR-001/002/003、compound《2026-07-06-decision-go-uber-style-guide》、CLAUDE.md 硬规则、`docs/go-style-checklist.md`
- Code facts checked: 仓库为 greenfield（根目录仅 `.codestable/` `docs/` 与 agent 入口文档，无业务代码）——design"现状 = 无现状"属实；`.codestable` 为 git 跟踪目录（CMD-005 自匹配问题的核验依据）；CMD-005/006 命令在当前 git 树实测通过

### Independent Review

- Status: completed
- Detection: native-agent（无 `mcp__paseo__create_agent`，使用宿主原生 Agent 工具；与主 agent 同类模型，属同类降级，残余风险已记入第 6 节）
- Provider / agent: general-purpose subagent（round 1: aeecee96fc20ae467；round 2 与终验、round 3 增量复审: a4779bf6ba459061b）
- Raw output: round 1 全量报告 + round 2 复审 + 终验确认 + round 3 增量复审（owner 拍板部署双轨后的实质修订；会话内回传；round 1 agent 的一次 resume 因上游 API 403 失败，改起全新 agent 完成 round 2，未降级 local-only）
- Merge policy: 主 agent 对全部 findings 逐条做了本地事实核验（含实测 CMD-005 自匹配、核对 §4.1/§4.3/§4.6 端点清单、探针表缺口比对 schema 交付物）后合并
- Gate effect: none（reviewer 已 completed 且终验判定"可交 owner 人工 review"）

## 2. Design Summary

- Goal: greenfield 安全网条目——工程骨架 + `make check` 命令基线、§4 契约固化为 OpenAPI + 双端 codegen、账号隔离 repository 基座（AccountScope）、单账号登录与错误封套、部署形态双轨（D6/D7，owner 2026-07-06 review 中拍板）、TG 冒烟。
- Key contracts: 名词层全新增（Account / AccountContext / AccountScope / ErrorEnvelope / openapi.yaml / JWT claims）；编排层两条线（运行时请求线含中间件链、go:embed 静态承接与启动序；开发时契约线含 tag 过滤 codegen 与漂移检查）；核心 seam = AccountScope（deep，local-substitutable，探针表测试面）；配置 = 12-factor env-only（.env.example 唯一入库清单）。
- Steps: 10 步（TG 冒烟与部署工件各自独立成步，超出 4-8 常规区间有显式理由——owner review 中扩展了部署双轨需求），exit_signal 全部 yes/no。
- Checks: 42 条，来源覆盖范围守护 7 / 名词契约 5 / 编排骨架 4 / 流程级约束 4 / 挂载点 8 / 验收场景 14，均可追溯到 design 对应节。
- Baseline / validation: greenfield 无既有基线（无"既有红灯"归因问题）；S1 起 `make check` 成为预检入口；CMD-001~008 与 checklist `dod.commands` 逐条一致。

## 3. Findings

### blocking

- [x] FDR-001 `design §2.1/§3 A9` A9 双账号隔离测试缺被测对象——本条 schema 只有 accounts 表，没有任何带 `account_id` 的表可供"写数据"
  - Evidence: round 1 交付物清单仅含 0001 accounts；A9/S4 要求跨账号读写断言
  - Impact: 实现者被迫在最安全敏感处自行发明测试面，基座测试可能"绿"而对真实业务表无效
  - Expected fix scope: D5/2.1/A9/S4 补探针表策略
  - **已修复（round 2 确认）**：测试专用探针表（account_id 外键、经同一迁移机制建于测试库、不进生产迁移序列、与 A10 断言隔离说明）四处落地

### important

- [x] FDR-002 `design §2.2` 405 封套化无 §4.1 错误码支撑，与"不自造错误码"自相矛盾 → **已修复**：方法不匹配一律 404 not_found，不开启 405 区分；S5/A7 同步
- [x] FDR-003 `checklist dod.commands CMD-005` git grep 自匹配必炸（checklist 含正则字面量且 .codestable 被跟踪）+ 漏检裸 TG token + 排除方向反了 → **已修复**：重写为 `! git grep -nE '[0-9]{6,}:[A-Za-z0-9_-]{30,}' -- ':!.codestable'`（round 2 实测通过），新增 CMD-006；env key 真实值核查显式归 code review 人工口径
- [x] FDR-004 `design D4/§3 A11` GET /me 无 §4 契约权威且 A11 单向核对 → **已修复**：显式白名单 + A11 双向核对 + §4 节登记"回 cs-roadmap update 收编"的 owner 拍板项
- [x] FDR-005 `checklist S8` TG 冒烟与 harden 收尾捆绑，违反 design 自己的风险缓解 → **已修复**：拆 S8（TG 冒烟）/ S9（harden 收尾与终验），Coverage Matrix 同步
- [x] FDR-006 `design D3` 空库缺 SEED_ADMIN_PASSWORD 行为未定义 → **已修复**：fail-fast + A10/S3 负向分支
- [x] FDR-007 `design D4/§3 A11`（round 2 新发现）端点集合公式漏 §4.1 login 与 §4.6 export，A11 照字面不可满足 → **已修复（终验确认）**：公式改为「§4 全部 HTTP 端点（§4.1+§4.3+§4.6）∪ {GET /me}」，design 四处 + checklist 三处同构一致
- [x] FDR-017 `design 3.y CMD-008`（round 3 新发现）全容器冒烟命令隐含前置未写明——fresh checkout 缺 `.env`（compose env_file 来源）必失败；`--wait` 就绪语义依赖 app healthcheck；失败时 `&&` 链不执行 down 残留容器 → **已修复**：D6 拍板 `.env.example` 占位值即本地可跑 dev 值（标注禁止用于生产）+ compose 不硬编码凭证 + app 配 healthz healthcheck；CMD-008 改写为失败也执行 down 的形式并加前置注记；S9 action 同步

### nit

- [x] FDR-008 元文本「用户」两处 → 改「owner」
- [x] FDR-009 S2 与 D4 的 codegen tag 口径不一致 → login 与 me 同挂 auth tag，两处对齐
- [x] FDR-010 healthz 失败语义未定义 → 补 503 {status:degraded}，A7/S5 同步
- [x] FDR-011 "/me 不含 password_hash" check 在 design 无出处 → 2.1 示例补"永不含 password_hash"
- [x] FDR-012 挂载点缺前端路由表与 HTTP_ADDR；CMD-003 空库来源未写 → 均已补
- [x] FDR-013 CMD-006 抓不住".env 已被跟踪"情形 → 补 `! git ls-files --error-unmatch .env`
- [x] FDR-014 CMD-005 不扫提交历史 → 已知局限写入反向核对表（骨架期接受）
- [x] FDR-018（round 3）D4 末句"未注册路径 404"未加 API 限定 → 已收窄为"未注册 API 路径"
- [x] FDR-019（round 3）改号残留 5 处（风险 3 与安全残留的 S9→S10、挂载点缺 db-up、Required Artifacts 缺 A14 截图、Docker 阻塞面未扩到 S1/S9/CMD-007/008）→ 全部已修
- [x] FDR-020（round 3）"生产模式走 SPA fallback"措辞暗示运行时模式分支，与 D6"无 APP_ENV 开关"有表面张力；go:embed 的"前端构建先于后端编译"顺序约束未列 → 措辞改为"编译期恒定行为"，顺序约束已补
- [x] FDR-021（round 3）compose 凭证须经 env_file 注入不硬编码，D6 隐含未明说 → D6 已明写

### suggestion

- [x] FDR-015 AccountScope"无法绕开"补机器断言 → depguard（domain/service 禁 gin；pgx 仅 store 系包）落入 D5/S1/S6
- [x] FDR-016 A11 落地为双向断言 → 已并入 FDR-004/007 修复

### learning

- "roadmap 完成信号引入、但 §4 契约未收录"的端点（/me）是一类规格缝隙：任何进 OpenAPI 的端点必须能指到 §4 条文或显式白名单，契约覆盖核对一律双向。已列入 design §4 的 compound 候选。

### praise

- compound《go-uber-style-guide》消费到位（golangci-lint 落地纳入 S1，review 口径引 checklist 文档）。
- 术语表带防冲突结论；AccountScope 不进领域术语表的边界判断正确；全文无「用户」领域性误用。
- D3 内联凭证红线核对（bcrypt 派生值入库 ≠ 凭证入库）论证干净。
- "明确不做"7 项全部配可执行反向核对；greenfield 基线处理明确。

## 4. User Review Focus

- **owner 需要重点拍板**：D1 仓库布局（backend/frontend/api + 根 Makefile）；D2 golang-migrate；D3 认证形态（bcrypt 入库 + JWT 30 天，无吊销）；D4 契约策略与 **/me 是否回 `cs-roadmap update` 收编进 §4**（一行改动，收编后清空白名单）；D5 AccountScope + 探针表 + depguard；**D6 部署双轨与 12-factor 配置**（含 `.env.example` 占位值 = 本地可跑 dev 值这一 F1 解法、roadmap §7"均自装"措辞更新的同步方式）；**D7 前端 go:embed 单工件**；关键假设（testcontainers、无 CI、healthz 不进 OpenAPI、react-router 守卫骨架）；备份策略只落文档建议、脚本化归 v1-hardening。
- **implement 需要重点遵守**：S 顺序与每步 exit_signal；探针表不进生产迁移序列；错误码不自造（API 方法不匹配 = 404；非 API 路径 SPA fallback）；depguard 依赖方向；compose 不硬编码凭证；OpenAPI 忠实转写、发现契约问题回 cs-roadmap update 不私改。
- **code review / QA / acceptance 需要重点复核**：A9/A11/A14 三个 core 场景；CMD-005~008 实际执行结果；凭证 env key 真实值的人工口径；A12 截图与 owner 前置动作；反向核对 7 项。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---|---|---|
| Acceptance Coverage Matrix | pass | E | design 3.x 全部 core 场景映射到 S1-S9 + 证据类型 + 命令/动作 | none |
| DoD Contract | pass | E | design 3.y 五级 DoD + CMD-001~006 与 checklist dod.commands 逐条一致 | none |
| Steps and checks traceability | pass | E | 9 steps exit_signal 全 yes/no；39 checks 均能回到 design 对应节（round 1 曾有 1 条无出处，已补出处） | none |
| Roadmap contract compliance | pass | E+C | 端点公式覆盖 §4.1+§4.3+§4.6；/me 白名单显式登记而非静默绕开；错误码/封套/分页/时区均引 §4.1；契约问题回 cs-roadmap update 的约束写入 D4；D6 对 §7 拍板包 #3 的更新为登记式（round 3 核验） | owner 拍板 /me 收编与 §7 观察项同步 |
| Module interface design | pass | C | AccountScope depth/seam/dependency strategy/adapter/test surface 齐全；探针表补齐测试面；depguard 使结构性强制可机检 | implement 时验证 depguard 配置真实生效 |
| Validation and artifacts | pass | E | CMD-005/006 在当前 git 树实测通过；CMD-007/008 前置与就绪语义已写明（round 3 F1 修复）；交付物清单可从文件系统反查；清洁度规则含显式例外 | none |

Summary: E=4, C=2, H=0, H-only core checks=none。

## 6. Residual Risk

- **同类 reviewer 降级**：独立审查由同宿主 Claude agent 完成（无异构 provider），盲区可能同构——owner 人工 review 是下一道异构视角。
- **登录端点无限速/失败锁定**（bcrypt 慢哈希是唯一阻尼）+ **公网明文 HTTP**（TLS/反代属部署面）：design 已登记为 v1-hardening 候选，acceptance 时提示 owner 显式认领。
- **JWT 泄漏最长 30 天窗口 + seed 密码 env 驻留、无改密 API**：S9 README 须写 token 轮换与"seed 后可移除变量"及改密现状；QA 核对该段真实落盘。
- **CMD-005 不扫 git 历史**：曾入库后删除的凭证抓不到；骨架期接受，review 人工口径兜底。
- **testcontainers / 部署工件对 Docker 的硬依赖**：owner 本机 Docker 不可用会让 `make check`、`make db-up`、CMD-007/008 整体不可跑；implement 首日即会暴露，届时可回退 docker-compose 常驻方案（design 已列备选）。

## 7. Verdict

- Status: passed
- Next: 交给 owner 整体 review（design 保持 `status: draft`，owner 放行后改 `approved`）。round 1 全部 blocking/important 已修复并经 round 2 独立复审确认；round 2 的 FDR-007 已修复并经终验确认；round 3（owner 拍板部署双轨后的实质修订）经独立增量复审判定"可交 owner 继续人工 review"，其 F1（FDR-017）与 4 条 nit（FDR-018~021）已按 reviewer 给定的修复边界全部落地，yaml 校验与措辞残留 grep 均通过。
