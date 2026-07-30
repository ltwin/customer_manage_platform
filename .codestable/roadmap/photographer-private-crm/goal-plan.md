---
doc_type: roadmap-goal-plan
roadmap: photographer-private-crm
status: awaiting-authorization
created: 2026-07-22
baseline_ref: f27d4296edfc86e8f4cceef0553ba4f1dd41a0c9
---

# photographer-private-crm Goal 执行计划

## 1. Scope And Inputs

- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md`
- Items: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-items.yaml`
- Goal state: `.codestable/roadmap/photographer-private-crm/goal-state.yaml`
- Runtime protocols: `goal-protocol.md`、`goal-protocol-feature-loop.md`、`goal-protocol-gates.md`、`goal-protocol-audit.md`
- Feature specs: `.codestable/roadmap/photographer-private-crm/goal-features/*.md`
- Git baseline: `f27d4296edfc86e8f4cceef0553ba4f1dd41a0c9`
- Content confirmation: roadmap 已确认；全部子 feature design 已批准；`v1-hardening` Round 7 design-review passed
- Current execution authorization: pending；不得派发 driver、accept 或自动 commit

## 2. Feature Order And Baseline Projection

Goal state 使用 runtime 给出的拓扑顺序。前 11 项在本 goal package 创建前已经由各自 feature 流程完成，均有 approved design、passed review/QA/acceptance，且 checklist steps/checks 已 terminal；因此投影为 `accepted` 历史基线，不重新执行实现。第 12 项 `v1-hardening` 为唯一 pending feature，`current_feature_index=11`。

| # | Feature | 性质 | 状态 | 一句话交付物 |
|---:|---|---|---|---|
| 1 | platform-skeleton | mixed | accepted baseline | Go+React 单体、OpenAPI/codegen、登录/账号隔离、错误封套与部署命令基线 |
| 2 | customer-core | functional | accepted baseline | 30 秒建档、客户搜索与详情的最小闭环 |
| 3 | package-catalog | functional | accepted baseline | 套系 CRUD、上下架与引用保护 |
| 4 | customer-profile-complete | functional | accepted baseline | 渐进档案、身份、备注、归档/恢复、合并与转介绍 |
| 5 | order-tracking | functional | accepted baseline | 八态订单、收款标记、查询与完整性约束 |
| 6 | schedule-calendar | functional | accepted baseline | 可靠档期 CRUD、月历、重叠提示与异常恢复 |
| 7 | reminder-engine | functional | accepted baseline | 按账号时区幂等生成与管理生日/回访/流失提醒 |
| 8 | customer-avatar | mixed | accepted baseline | 安全头像代际、鉴权展示、维护/GC 与 exact-generation 部署边界 |
| 9 | telegram-digest | mixed | accepted baseline | 安全绑定、每日摘要、`/today` 与可恢复投递 |
| 10 | dashboard | functional | accepted baseline | 真实五卡经营台与台上提醒/尾款动作 |
| 11 | data-export | mixed | accepted baseline | 账号级一致快照的 reference-only JSON 导出 |
| 12 | v1-hardening | mixed | pending | 页面状态、375px 轻路径、生产预检/备份恢复与 V1 全链路证据 |

历史 accepted projection 不豁免最终审计。旧 feature 缺少当前版本 canonical evidence-pack/gate JSON 时，最终审计只能基于真实既有产物与重新运行的命令回填；无法证明的 core evidence 必须 handoff，禁止生成自报 passed 的占位文件。

## 3. Roadmap Core Acceptance Paths

### 3.1 Fresh V1 主链

owner 在真机浏览器和真实 API 数据上逐节点确认同一 synthetic 主链：

1. 带 identity 建客户档案；
2. 创建套系；
3. 创建订单；
4. 创建档期；
5. 标记定金；
6. 验证下一账号自然日 Telegram digest payload 与调度关联；
7. 验证 dashboard 五卡逐项与领域 API 一致；
8. 在同一 fixture 上额外执行 reference-only export，但不得以 export 替代前七个节点。

H2 使用 owner 已批准的组合：复用 2026-07-17 owner-attested true-external transport/binding；本轮 fresh 验证 payload、调度关联和完整主链。

### 3.2 375px 三条轻路径

- 查档期：375 CSS px 下进入 Calendar、切月/今天、选日期、查看空/密集/冲突日，无整页横向滚动，移动写入口不可见且不可聚焦。
- 搜客户：同一查询结果在 375/768 为 cards、769/desktop 为 table，字段/动作等价且只有一次列表请求。
- 记备注：成功确认、500 保留输入、连按/IME 不重复、Escape 返回焦点、401 清 token 回登录；375 + 200% 字体仍可完成。

### 3.3 页面状态与认证回归

- AppShell、`/login` 与 9 个受保护 screen routes 的 loading/empty/error/ready(current|stale)/401 状态矩阵逐项 terminal。
- 首次错误不冒充 empty；stale refresh error 不冒充 current；401 先清 token 再回登录。
- CustomerPicker 空 options 的 ArrowDown、active descendant、Tab/blur/close 与焦点行为稳定。

### 3.4 生产运维核心路径

- production preflight 的 binary、compose-managed-db、compose-external-db 三轨与 seed/key/selector matrix 真实执行。
- local synthetic initialized target 真实执行 backup，生成并自校验 exact-generation 五件套 package。
- 把目标变更后做 destructive restore，验证 DB counts、头像 generation、manifest 与原 app running/exited 状态语义。
- 真实 daemon 演练锁 busy/stale、immutable-ID cleanup、realpath/context alias、double breaker、release/new owner、delayed helper fence、start/health 失败和 cleanup masking。
- sentinel project 始终不变；operation cleanup 与 harness cleanup 分层；无半包、匿名 volume、temp compose/env 或 secret-bearing evidence 残留。

## 4. Key Assumptions

1. `v1-hardening` 的四个 implementation dependency（customer-avatar、telegram-digest、dashboard、data-export）在 items.yaml 中持续为 `done`。
2. Docker Desktop/Engine 可用，Docker Compose 至少 v2.24，本机 Unix socket endpoint 可被显式 context 解析。
3. 浏览器可记录 375/768/769/desktop、DPR、200% 字体与 DOM/网络断言；不以 PNG 物理宽度冒充 CSS viewport。
4. 2026-07-17 owner-attested TG true-external transport/binding 证据仍有效；本轮不需要再次使用真实 token，除非 owner 后续改变 H2。
5. ECS TLS、网络、异地介质、mount/write/fsync/dir-sync 与断电级 durability 需要 owner/environment attestation，不能由本机 exit 0 代替。
6. `.workflow/` 与 `install-cpamp.sh` 是任务外未跟踪文件，任何 goal stage 都不得触碰、stage 或 commit。

## 5. Top 3 Risks And Mitigations

1. **Destructive restore、锁 alias 或迟到 helper 损坏同一物理 target。**
   - 缓解：pinned Unix endpoint；provenance/physical identity 分离；daemon-side deterministic lock 与 helper fence；immutable full ID、nonce/generation；validate-before-mutate；failure-stop 到 Engine exited；148-case machine contract 与 sentinel smoke。
2. **全站状态/移动改造回退既有桌面行为或把旧数据伪装成成功。**
   - 缓解：StateNotice 不拥有数据；逐 route 状态矩阵；同一 customer fixture 对拍 375/768/769/desktop；API/schema 零增量；fresh browser/network evidence。
3. **历史 accepted feature 的现代 gate artifact 缺口导致最终审计出现伪回填或不可追溯结论。**
   - 缓解：历史 feature 不重实现；以现有 approved/passed/accepted 产物为事实，重跑聚合命令；缺 modern JSON 时用当前 gate 对真实输入生成；core evidence 不足时 handoff，不创建占位或自报 passed artifact。

## 6. Mandatory Validation Commands

### 6.1 v1-hardening DoD commands

```bash
make check
cd frontend && npm run test:v1-hardening
bash -n scripts/*.sh
./scripts/test-v1-ops-contract.sh && docker build -t crm:v1-hardening .
./scripts/v1-ops-smoke.sh
git diff --check
```

CMD-001 必须真实证明 `npm run test:v1-hardening` 与 `test-v1-ops-contract.sh` 已接入长期 Makefile 基线；CMD-004 的 negative corpus 与 CMD-005 的真实 daemon smoke 不能互相替代。

### 6.2 Final aggregate commands

roadmap 完成前去重重跑：

```bash
make check
make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts
cd backend && go test ./... -count=1 -parallel=1
cd frontend && npm run lint && npm run build
cd frontend && npm run test:schedule
cd frontend && npm run test:customer-avatar && npm run test:avatar-layout
cd frontend && npm run test:telegram-digest
cd frontend && npm run test:data-export
cd frontend && npm run test:v1-hardening
bash -n scripts/*.sh
docker compose config
./scripts/test-v1-ops-contract.sh && docker build -t crm:v1-hardening .
./scripts/v1-ops-smoke.sh
git diff --check
python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/photographer-private-crm
```

功能性核心命令不得因耗时跳过。某个历史定向命令已由 `make check` 真实包含时可记录去重证据；只有非核心命令允许带明确理由 trust-prior。

## 7. Preflight Strategy

1. 每次恢复先运行 `codestable-workflow-next.py epic --roadmap ... --json`，以 repo facts 决定 next stage。
2. Goal execution authorization 通过后、写实现代码前，按 `.codestable/attention.md` 让 owner 选择当前 branch 或新 worktree/branch；在选择前不得开始 implementation。
3. 运行 `codestable-workflow-next.py feature --feature .codestable/features/2026-07-22-v1-hardening --require-implementation-ready --json`，确认四个依赖严格为 done。
4. 记录 baseline、branch、tracked/unstaged/untracked；保护任务外文件，不使用 destructive git 命令。
5. 检查 Go/Node/Docker/Compose/浏览器和所需脚本；缺 runner 时按下一节恢复。
6. 任何真实凭证只经环境变量/owner-controlled runtime 注入，不写日志、JSON、截图或仓库。

## 8. Missing Tool Recovery

- 只能安装/修复真实测试依赖、lockfile、既有 runner 配置或更新/重装 CodeStable skill 包。
- 禁止新增 `go`、`docker`、`jest`、`pytest.py` 等同名 shim，禁止通过空脚本、跳过测试或伪造 JSON 让 gate 变绿。
- Docker/浏览器/外部环境是 core path 时，缺失必须记录为 needs-human/handoff；不能降级成静态 grep 冒充运行证据。
- provider unavailable 按 Provider Policy 记录，不自动阻塞基础流程；但 provider warning 中的核心风险必须由 review/QA/audit 解释。

## 9. DoD Policy

- Design：approved design + passed independent design-review + parseable checklist + Acceptance Coverage Matrix + DoD Contract。
- Implementation：7 STEP 全 done；scope-gate、dod-runner、evidence-pack 通过；core commands 有真实日志；无未解释范围外 diff。
- Review：独立 Task agent review passed、无 unresolved blocking、消费 evidence/gates，并给 QA 明确 focus。
- QA：passed；覆盖 A1～A27、DoD、review focus、真实 browser/API/daemon core paths，不把核心缺口降为 residual。
- Acceptance：`goal-acceptance` named approval 有效；46 CHK 全 passed；canonical artifacts/gates/results 完整；roadmap item 回写 done。
- Roadmap audit：所有 goal features accepted、items terminal、聚合命令/核心主链通过、授权有效、goal consistency 与 goal audit gates passed。

## 10. Gate Policy

- 运行时权威：`.codestable/roadmap/photographer-private-crm/goal-protocol-gates.md`。
- implementation.before_review：scope-gate + dod-runner + evidence-pack。
- review.before_pass：review-evidence-gate。
- qa.before_acceptance：qa-evidence-gate。
- acceptance.before_done：acceptance-dod-gate。
- roadmap_audit.before_complete：goal-consistency-gate + goal-audit-gate。
- `protocol-only` gate 由对应 stage 消费 evidence，不得当作缺失 executable script。
- 同一 blocking 三轮仍失败时持久化 handoff，不循环伪修。

## 11. Provider Policy

- archguard / meta-cc unavailable 只记录为 provider unavailable/fallback，不自动阻塞。
- provider warning 必须被独立 review、QA 或 final audit 明确解释、修复或提升为 blocking。
- meta-cc 首批只消费已有摘要；不得为满足 provider 字段伪造输出。
- 最终审计聚合 provider warnings、E/C/H summary 与 H-only core checks；任何未批准的 H-only core check 必须 handoff。

## 12. Final Audit Deliverables

最终审计至少核验：

- roadmap/items/goal-state/approval-report 的状态、路径、identity 与授权一致性；
- 每个非 dropped item 与 goal-state feature 一一对应；
- approved design、checklist、review、QA、acceptance、evidence pack、evidence pack results、scope gate、DoD contract/results、gate results；
- A24 fresh 主链、A27 独立 export、375px 三路径、逐 route 状态、生产运维 smoke 与外部 attestation；
- requirement/architecture/roadmap/README 写回与 N/A 理由；
- tracked/staged/unstaged/untracked、debug/TODO/FIXME/XXX、临时 runner/shim/package、`__pycache__` 与 Docker residual；
- `.codestable/roadmap/photographer-private-crm/goal-audit.md`，包含 final aggregate commands、core acceptance、deliverables、provider/E-C-H、workspace cleanliness 与 verdict；
- 必跑：`python3 /Users/samson/.agents/skills/cs-onboard/tools/codestable-goal-consistency-gate.py --roadmap .codestable/roadmap/photographer-private-crm`。

## 13. Goal Authorization Policy

- `goal-acceptance`：允许 driver 在 review/QA/gates 真实通过后，以 `ResumeGoalAcceptance approval-report.md#goal-acceptance` 完成 feature acceptance；当前为 pending。
- `goal-commits`：允许每个 feature accepted 后自动做一次 scoped commit，范围仅含该 feature 的代码/spec/evidence/review/QA/acceptance、roadmap 回写及对应 goal-state 更新；当前为 pending。
- 两项必须在 `approval_groups.goal-execution` 中由同一次 owner answer 原子批准，并使用同一非空 confirmation ID；不能批准一项后继续另一项。
- scoped commit 不授权 remote push、PR、merge、publish、release、deploy、promotion 或 production cutover。
- Approval refs 固定为 `approval-report.md#goal-acceptance` 与 `approval-report.md#goal-commits`，互不替代。
