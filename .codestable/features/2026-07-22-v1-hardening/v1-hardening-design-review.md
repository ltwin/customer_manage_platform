---
doc_type: feature-design-review
feature: 2026-07-22-v1-hardening
status: passed
review_state: passed
review_reason: ""
reviewer_id: "/root/v1_hardening_design_review_r7"
reviewed: 2026-07-22
round: 7
---

# v1-hardening feature design 审查报告

## 1. Scope And Inputs

- Design: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-design.md`
- Checklist: `.codestable/features/2026-07-22-v1-hardening/v1-hardening-checklist.yaml`
- Roadmap: `.codestable/roadmap/photographer-private-crm/photographer-private-crm-roadmap.md` 与 `photographer-private-crm-items.yaml`
- Requirement / architecture: `CONTEXT.md`、schedule-calendar requirement、ADR-001～ADR-004、AccountScope / cross-domain read model / avatar immutable generation 等既有 compound 约束
- Code facts checked: AppShell 与正式 routes、API 401 分流、Customers/Calendar/QuickNote/CustomerPicker、server/config、Compose services/volumes、Makefile 与 goal consistency gate
- External semantics checked: Docker / Compose 官方文档；按项目规则先经 Context7 resolve，再查询高信誉 `/docker/docs`
- Review scope: 只审查当前冻结 design/checklist 是否给出可实现、可验证、与 roadmap 一致的方案契约；不把实现、运行时验证或 owner 审批伪装为已经完成

### Independent Review

- Status: completed
- Detection: native-agent
- Final provider / agent: 同类宿主原生 Task agent `/root/v1_hardening_design_review_r7`
- Final raw verdict: `passed`；blocking=0、important=0、nit=0、suggestion=1；四份冻结输入在审查开始和结束时 SHA-256 一致
- Review discipline: Round 7 全程只读，没有创建、修改或删除文件，没有运行 build、Docker、Compose、服务或有状态命令，也没有读取其他 reviewer 的结论
- Attempt history: Round 1～5 均产生有效 changes-requested 结论并由主 agent 逐项回写；Round 6 primary 因汇总过慢曾被中断并 follow-up，随后仍返回有效发现；Round 6 retry 是新的独立交叉审查；Round 7 是当前冻结输入的最终有效复审
- Merge policy: 主 agent 已逐条用 design、checklist、roadmap、goal gate、CodeGraph/源码事实与 Docker 官方语义核验；没有把缺少仓库或协议证据的偏好升级为 finding
- Correlation note: reviewers 均为同类模型/同宿主原生 agent，冻结输入与独立上下文降低了相互污染，但不能消除同类模型审查相关性；该项保留为 residual risk
- Gate effect: design-review gate 已满足，但 design 必须继续保持 `draft`；下一状态只能是 owner 整体确认 `HumanCheckpoint ConfirmDesign`，不得自动批准、创建 GoalPackage 或进入 implementation

## 2. Design Summary

- Goal: 不扩张经营域、HTTP API、OpenAPI、数据库 schema 或账号隔离语义，收口首版页面读取状态、375 CSS px 移动轻路径、生产运维安全网与 V1 全链路回归证据。
- UI contract: 共享 `StateNotice` 只承担 loading/empty/error/refresh-error 的呈现语义，页面继续拥有请求和 ready 数据；Customers 用同一查询结果投影 desktop table 与 mobile cards；Calendar 移动端保持只读轻路径；375/768/769/desktop 与 200% 字体放大均有明确边界。
- Runtime contract: server 增 60 秒 `IdleTimeout` 并脱敏启动错误；production preflight 支持 binary、compose-managed-db、compose-external-db 三种轨道，冻结 seed/key 矩阵、exit code、Docker endpoint pinning 与 Engine 调用策略。
- Backup/restore contract: Compose managed target 使用独立的 provenance 与 physical target identity、daemon-side deterministic mutex、immutable ID/generation/nonce、helper fence、原运行状态恢复、exact-generation package 与 fail-closed destructive restore。
- Machine evidence: D8 冻结 58 个 inventory 行、148 个唯一 case ID、150 个 scenario memberships；结果 schema 分离 operation/oracle、target/package/lock、operation cleanup 与 harness cleanup，并防 subset、自报成功、no-op pipeline、cleanup masking 与伪造 after observation。
- Acceptance: A1～A27 均为 core；A24 保留 roadmap 精确主链“建档→套系→订单→档期→标定金→下一账号自然日摘要→dashboard 五卡”，A27 export 是独立追加节点，不能替代 A24。
- Human checkpoints: H1 决定是否接受当前认证 residual 并授权独立 follow-up；H2 决定 TG true-external transport 证据复用或 fresh 真机复跑。两项均是预期 owner checkpoint，不是 unresolved review finding。
- Plan shape: 7 个稳定 STEP、46 个稳定 CHK、2 个 owner checkpoint，当前全部为 `pending`；required trace 精确覆盖 D1～D8、H1～H2、A1～A27 与 `ROADMAP-V1-CLOSE`。

## 3. Resolution Register

| Round | Verdict / findings | Resolution evidence | Final state |
|---|---|---|---|
| 1 | `changes-requested`；4 blocking、5 important | 稳定 STEP/CHK/trace 与全 pending 状态；补齐三部署 mode、seed/key 矩阵、initialized managed target、Docker endpoint/context pinning及可执行验收入口。后续各轮继续对这些契约做反例复核。 | resolved |
| 2 | `changes-requested`；1 blocking、1 important | backup/restore 公开 surface 固定 initialized seed 语义与 `--break-stale-lock`；ops results 从“只有文件路径”扩为可消费 schema、case/scenario mapping、stage/before/after/sentinel/cleanup 与顶层 pass 推导。 | resolved |
| 3 | `changes-requested`；1 blocking | 将 Compose 配置来源 provenance 与物理互斥 target identity 分离；同 daemon/socket + canonical project/resource namespace 命中同一锁，compose realpath/context alias 不再拆分锁域；增加同 project/different realpath 与 context alias race case。 | resolved |
| 4 | `changes-requested`；4 blocking、1 important | 引入 Engine-side deterministic mutex；release/stale-break/race 只按 immutable full ID 操作；固定 lock ID、owner nonce/generation 与 helper labels；精确定义 app running/exited Engine predicate；破坏性 restore 失败保持可验证 after-state。 | resolved |
| 5 | `changes-requested`；1 blocking、1 important、1 nit | 冻结 required-case inventory 与 structured expected/observed；完善 Engine state 正反例、显式 Docker context 示例、restore failure oracle、375/768/769/desktop/200% UI 边界、A24 精确主链/A27 独立 export，以及 H1/H2 canonical owner checkpoint。 | resolved |
| 6 primary | `changes-requested`；3 blocking、3 important | 将 ops inventory 扩为最终 148 unique IDs；补 required-command、static path、source-volume overlap、restore start/health、delayed helper race；以 exact stage-plan + `backup_package_expectation` 阻断 no-op false pass；分离 operation/harness cleanup；after 字段改为 observation envelope。 | resolved |
| 6 retry | `changes-requested`；1 blocking、1 important | 将 `v1-hardening-dod-contract-results.json` 加入 D8、Required Artifacts 与 checklist；把 binary/PFB 及 binary config reject 的 Engine policy 收紧为 `F`，binary avatar 与 compose avatar case 拆开。 | resolved |
| 7 | `passed`；0 blocking、0 important、0 nit、1 suggestion | 机械复核 58 行、148 unique IDs、A18/19/20/21/22=`66/2/41/2/39`、Plan/Engine/Mutation、cleanup ownership、helper fence、Goal artifacts、A15、A24/A27 与全 trace；冻结输入无漂移。 | passed |

### blocking

- none。Round 1～6 的全部 blocking 已关闭，Round 7 没有新增 blocking。

### important

- none。Round 1～6 的全部 important 已关闭，Round 7 没有新增 important。

### nit

- none。Round 5 的 nit 已随相应契约修订关闭，Round 7 没有新增 nit。

### suggestion

- `R7-SUG-001`：STEP-001 物化 catalog generator / validator 时，把 `required_lock_roles[] → allowed_operation_removers{}` 固化为显式、可单测的 role truth table。
  - 至少覆盖 `owner / invalid-candidate / existing-owner / contender / stale-candidate / winner / loser / old-owner / new-owner / old-helper`。
  - 每个 role 只允许映射为 owning generation 的 `owner`、满足 stale-break 条件的 `authorized-breaker` 或 `none`。
  - 该项不阻塞 design：D5.3 已用 immutable ID、nonce/generation、candidate ID 与 actor ownership 给出唯一安全语义；D8、CHK-030/CHK-045 和 CMD-004 又覆盖 per-role map、错误 immutable-ID remover 与 finalizer masking。显式 truth table 用于降低 148-case 实现时的人为映射风险。

### learning

- production `operation_cleanup` 与独立 `harness_cleanup` 必须分层；测试 finalizer 最终清空资源，不能反向证明 production cleanup 成功。
- `target_before/target_after` 用 `observed|absent|unreadable` envelope 表达，才能在 destructive restore 失败后既保留真实 after，又避免伪造 counts/hash 或把整个 after 写成 null。
- `backup_package_expectation`、exact stage-plan、五件套 package oracle 与 CMD-004 no-op 反例必须联合使用；仅凭 exit 0 / terminal complete 不足以证明 pipeline 真的执行并原子发布。
- Compose 文件路径与 context 名称属于配置 provenance，不是物理资源 namespace；互斥 identity 必须绑定 canonical daemon/socket 与 project/resource namespace。
- daemon container-name uniqueness 和 immutable ID 支撑锁的基础原语，但 delayed request、double breaker、release↔new acquire 与 cleanup ownership仍必须由真实 Engine smoke 证明。

### praise

- D5/D8 已从叙述式 smoke 收敛为 catalog、result、coverage、stage、package、target、lock、cleanup 多层机器契约；复杂度与 destructive restore、跨客户端竞争和 exact-generation 数据风险相称。
- H1/H2 没有被伪装成已经解决；两项保留明确推荐项、持久落点与 owner 选择，未回答前禁止把 design 改为 approved。
- A24 roadmap 主链与 A27 reference-only export 保持独立，避免用较容易的 export 节点替代真实业务闭环。
- A15 通过独立 CHK-046 固定 CustomerPicker 空 options、ArrowDown、`aria-activedescendant`、Tab/blur/close 与焦点行为，关闭了此前容易被总体移动回归掩盖的键盘边界。

## 4. User Review Focus

owner 需要对完整 design/checklist 与两个 HumanCheckpoint 分别作出明确回答；任一项都不得由 reviewer 或主 agent代替拍板。

### 4.1 完整设计

- 是否批准 `.codestable/features/2026-07-22-v1-hardening/v1-hardening-design.md` 与 `v1-hardening-checklist.yaml` 的完整范围、接口、148-case 运维契约、A1～A27 验收和 7-step 执行顺序。
- 当前独立 verdict 只说明方案 gate 通过；在 owner 回答前，frontmatter 必须保持 `status: draft`。

### 4.2 H1 — 认证 residual

- 推荐项：当前 V1 接受以下 residual：无登录限速、30 天 JWT、localStorage Bearer、无改密 API。
- 若 owner 接受：授权在 GoalPackage 前创建 `.codestable/issues/2026-07-22-auth-hardening/auth-hardening-report.md`，作为 canonical follow-up intake；本轮不静默宣称风险已消除。
- 若 owner 不接受：退回当前 design 扩认证范围并重新进行 design review；不得先进入 implementation。
- 在 owner 明确接受前，不创建 auth-hardening report。

### 4.3 H2 — Telegram true-external 证据

- 推荐项：复用 2026-07-17 owner-attested 的 TG true-external transport/binding 证据；同时 fresh 验证同一 synthetic fixture 的下一账号自然日 digest payload 与调度关联。
- 若 owner 要求 fresh true-external：由 owner 提供脱敏真机证据；仓库 synthetic 结果不得冒充 owner 的真实外部 transport attestation。
- 无论选择复用还是 fresh，A24 同一 fixture 的 payload、调度关联与完整 roadmap 主链都必须 fresh；H2 不能替代 A24。

## 5. Evidence Confidence Ledger

| Check | Verdict | Evidence Class | Basis | Follow-up |
|---|---|---:|---|---|
| Frozen input integrity | pass | E | Round 7 开始/结束四文件 SHA-256 一致；reviewer 全程只读 | review report 落盘后重算完整 artifact hashes |
| Acceptance Coverage Matrix | pass | E | A1～A27 全部映射 step、evidence、command/action 与 core；A24/A27 独立 | implementation/QA 生成真实证据 |
| DoD Contract | pass | E | 五类 DoD、CMD-001～006 与 design-review/implementation/review/QA/acceptance/evidence/dod-contract/dod/gate artifacts 齐全 | 下游逐阶段生成并由 gate 消费 |
| Steps / checks traceability | pass | E | 7 STEP、46 CHK、2 owner checkpoints 全部 pending；required trace exact set 无缺失或未知项 | owner 批准后按稳定 ID 更新 terminal status |
| Ops inventory | pass | E | 58 行、148 unique IDs、150 memberships；A18/19/20/21/22=`66/2/41/2/39`；Plan/Oracle/Exit/Stage/Engine/Mutation/Cleanup 枚举闭合 | STEP-001 物化 catalog 并做 deep-equality validator |
| Roadmap contract compliance | pass | C | `v1-hardening` 在 main/items 唯一 in-progress；A24 与 roadmap 完成链一致，A27 不替代主链 | acceptance 后才允许回写 done |
| Module/interface design | pass | C | StateNotice/page ownership、ops CLI、physical target identity、mutex/helper fence、restore ordering 与代码/Compose 事实相容 | code review 与真实 daemon smoke 核验 |
| Validation and artifacts | pass | C | D8、checklist、Makefile 接线契约、goal consistency gate 与 Docker 官方语义共同支撑 | CMD-001～006 尚待实现后运行 |
| Scope guard | pass | E | 明确禁止暗扩 HTTP API/OpenAPI/schema/领域状态机/账号隔离；H1 不接受时必须扩 design 并重审 | implementation diff 持续审计 |

Summary: E=5，C=3，H=0；H-only core checks=none。H1/H2 是 owner 决策 checkpoint，不是“缺少证据却被算作 pass”的 core review check。

## 6. Residual Risk

- H1 是真实上线风险：当前仍无登录限速、JWT 有效期 30 天、Bearer token 存在 localStorage、没有改密 API。只有 owner 接受并授权 canonical follow-up，或扩当前设计重审，二者之一完成后才能继续。
- H2 的 prior true-external transport + fresh synthetic payload/scheduling 证据组合需要 owner 明确选择；review verdict 不替 owner 判断外部证据充分性。
- helper fence、double breaker、release↔new acquire、context/realpath alias 与 delayed old helper 的整体线性化，只有 CMD-004 negative corpus 和 CMD-005 真实 daemon smoke 能最终证明；静态设计与 Docker 文档不能替代竞态实测。
- ops catalog 的 fixture/action 文本要到 STEP-001 才逐项物化；实现必须确认其非空、非占位、case-local 且与 frozen expected 深相等，尤其落实 `R7-SUG-001` 的 role truth table。
- ECS TLS、网络边界、异地介质、生产 mount/write/fsync/dir-sync 与断电级 durability 不能由本机 preflight exit 0 消除；A23、restore drill 与 owner attestation 仍不可替代。
- 设计审查没有运行 CMD-001～006，也没有实现当前尚不存在的 Makefile 定向入口、运维脚本、浏览器证据或 canonical result artifacts；实际可编译性、运行时清理与证据真实性由 implementation、code review、QA、acceptance继续验证。
- 独立审查由同宿主同类 Task agents 完成；多轮冻结输入、独立 reviewer 与主 agent 本地核验降低了遗漏概率，但仍保留相关性偏差。

## 7. Verdict

- Status: `passed`
- Design state: `draft`
- Review gate: satisfied
- Owner checkpoints: H1/H2 均为 `pending`
- Next: 停在 `HumanCheckpoint ConfirmDesign`，等待 owner 同时确认完整 design/checklist、H1 与 H2。
- Prohibited before approval: 不得把 design 改为 `approved`，不得创建 auth-hardening report，不得创建 GoalPackage，不能进入 implementation，不能询问 branch/worktree，不能 commit。
