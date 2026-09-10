# 创意空间基础框架设计审计（需求 · 架构 · 数据模型）

- 审计日期：2026-09-09
- 审计对象：
  - 需求设计 `.codestable/requirements/creative-canvas-foundation.md`（v0.1，sha256 `5c78927d…`）
  - 架构设计 `docs/product/creative-canvas-system/architecture.md`（v0.2，sha256 `1d84d7c4…`）
  - 数据库设计 `docs/product/creative-canvas-system/data-model.md`（v0.2，sha256 `a9a39c05…`）
- 判断标准：从资深架构师与数据库工程师视角，检查三份设计与仓库既有基座（ADR、迁移、平台包、运维 runbook）的一致性、数据库范式与约束完备性、首期功能覆盖度，以及是否为单人首期预支了复杂度。
- 范围：只读审计；对照 worktree `feat/creative-workspace-redesign` 当前源码逐条核对。未执行 DDL、未安装依赖、未压测、未连接对象存储或模型供应商。
- 与既有审查的关系：Epic 工作记录已有 Fermat / Huygens 两轮独立 cs-review（架构一致性与契约）。本报告不重复那两轮结论，只补充「与仓库基座冲突」「范式与约束」「复杂度取舍」三类此前未覆盖的角度；不构成 CodeStable gate，不改变 `status: proposed`。

## 结论摘要

**设计整体成熟，清零 2 项阻断后可进入模块设计。** 内容 / 资产 / 节点三层身份分离、不可变修订、人工与 Agent 共用命令写路径、运行的 epoch / lease / slot 机制，都是正确且工程上少见的严谨方案，不需要推翻。

问题集中在两类：

1. **与仓库基座冲突（2 项阻断）**：无外键决策与 ADR-002 正面冲突且未 ADR 化；新表 ID 形态（`UUID` + 复合主键）与全仓 `TEXT` 前缀 ID 约定不一致。这两项决定后续所有 DDL 与 DTO，改晚了代价最大。
2. **为单人首期预支的复杂度（多项建议）**：第三套媒体子系统、五个领域包、独立 worker 进程、上传候选状态机、多币种预算、七种版本计数器。每一项单看都有理由，叠加后是一个人维护不动的面积。

需求文档本身质量高，每条需求都有可观察结果，FLOW 场景与架构 §12 验证项的追溯完整。缺口在链接节点语义、外发知情提示、保留期归口三处。

## 级别定义

| 级别 | 含义 |
|---|---|
| 阻断 | 进入模块设计前必须拍板，否则后续 DDL / DTO / 迁移返工 |
| 重要 | 应在模块设计中解决；不解决会在开发或验收阶段暴露 |
| 建议 | 复杂度取舍或命名一致性；由 owner 按「反预支复杂度」口径自行决定 |

## 一、系统架构

### A-1 ·【阻断】无外键决策与 ADR-002 冲突，且未 ADR 化

- 发现：data-model §1 与 architecture ARCH-04 / §10 采用「新创意空间业务表不建数据库外键」。ADR-002 否决 MongoDB 的核心理由恰是「无外键，引用完整性全靠应用层自觉」，并把「外键在库层兜住引用完整性」列为选 PostgreSQL 的正面收益。同一库内旧表全 FK（全仓迁移 52 处 `REFERENCES accounts`，0036 亦全表 FK）、新表零 FK，两套治理并存。
- 证据：`adrs/002-postgresql-as-primary-store.md:23,28`；`data-model.md:15,21-23`；`architecture.md:135,273`；Epic `creative-workspace-system.md:29`；`0036_creative_workspace.up.sql` 全文。
- 影响：data-model §1 为此补了「关联注册清单」「反关联检查」「所有 writer 使用同一守卫」三套应用层机制，等于用代码重造数据库已经免费提供的能力；单人项目里这些检查最容易被后续切片跳过。
- 建议：
  1. 无论最终选哪种，先写 ADR-008 记录真实动因（迁移灵活性？删除编排可控？）与代价，避免后来者按 ADR-002 反推为错误。
  2. 技术上推荐混合策略，把「同生命周期的结构性引用」与「跨生命周期的历史引用」分开：

     | 引用类型 | 建议 | 例子 |
     |---|---|---|
     | 所有表 → `accounts` | 保留 FK（仓库先例） | 全部新表 |
     | 同根结构引用 | FK `NO ACTION`，不用 CASCADE / SET NULL | project→canvas、canvas→node/edge/node_inputs、content→revision、revision→content_objects |
     | 跨生命周期引用 | 不建 FK，按现设计走服务守卫 | node→revision、asset→revision、快照 key、`legacy_map`、run_inputs |

     `NO ACTION` 只在语句结束时校验，不与 RemoveNodes 的显式删除顺序冲突，也不会产生设计文档担心的隐式级联。这样 §1 的反关联检查可以缩到只覆盖第三类。
  3. 若 owner 维持全无 FK，把「反关联检查」从「定期」改为首个开发切片的必交项，并在 storetest 里加「悬空 key 写入被服务层拒绝」的集成测试。

### A-2 ·【重要】creativemedia 是仓库第三套媒体生命周期

- 发现：仓库已有 avatarmedia 与 planningmedia 两套对象生命周期；后者已实现 generations / renditions / leases / read_pins / object_inventory 与 `ReconcileGC` / `ReconcilePhysicalOrphans`。data-model §5 再建 blobs / content_objects / read_pins / revision_holds / quotas 与新 GC 协议。
- 证据：`0017_planning_media.up.sql:21-189`；`planningmedia/application.go:243,559`；`data-model.md:182-188`；`architecture.md:115`。
- 影响：三套 GC 在同一 bucket 上运行，互不知晓对方的保留根；权利矩阵纯函数（`planningmedia/matrix.go`）在 data-model `creative_rights_declarations` 里被第三次建模。
- 建议：明确 creativemedia 的定位二选一。(a) 作为取代 planningmedia 的平台层：把 object_inventory、物理孤儿对账、权利矩阵抽到 `platform/` 复用，planningmedia 后续迁到其上；(b) 并列第三套：在 §6.3 写清 key 前缀隔离与「新 GC 只扫 `creative/` 前缀」的硬规则。推荐 (a)，但可以先做 (b) 的隔离规则、把抽取排到后续。

### A-3 ·【重要】五个领域包 + 应用层协调器，单人维护偏重

- 发现：`creativecanvas / creativecontent / creativelibrary / creativemedia / creativeagent` 五个包，加 `platform/jobs`、`platform/modelprovider`。每条画布命令都要跨 canvas / content / media 三个事务端口。
- 证据：`architecture.md:112-118`；`architecture.md:120`「由应用层协调一个账号事务，通过各模块的事务端口完成」。
- 建议：content 与 media 合并为一个包，对外只暴露「修订」与「blob」两个端口；canvas / library / agent 与产品三支柱一一对应。三个领域包 + 一个内容包，是单人能维护、又不失边界的粒度。

### A-4 ·【重要】直传 OSS 改变既有「无公开直链」立场，授权方式未定

- 发现：ARCH-06 与 §6.2 采用浏览器临时授权直传 staging。对象存储 runbook 明确「下载一律经 API 回环流式转发（无公开直链）」，且「预签名直链」列为后续条目。架构未说明用预签名 URL 还是 STS 临时凭证；后者需要新建 RAM 角色与 `sts:AssumeRole`。
- 证据：`architecture.md:45,101,275`；`docs/dev/object-storage.md:3,59`。
- 建议：首期用预签名分片 PUT，只需现有 AK / RAM Role；bucket CORS 规则与 staging 前缀的 RAM 最小权限写进 runbook。读取仍走回环代理，与 §6.3 一致。

### A-5 ·【重要】链接节点的 SSRF 风险完全未覆盖

- 发现：需求与架构都提到「链接」内容与 `core.link` 节点，但没有定义是否服务端抓取标题 / 预览图。一旦抓取，就是对用户提交 URL 的出站请求，需要私网地址拒绝、重定向限制、超时与大小上限。三份文档均无「抓取」「出站」相关内容。
- 证据：`creative-canvas-foundation.md:33,87`；`data-model.md:92`。
- 建议：需求层先定语义。若首期只存 URL 与用户手填标题，在 CAN-02 写明「不抓取」；若抓取，架构 §10 增加出站策略并进 §12 验证项。

### A-6 ·【建议】独立 worker 进程是单台 ECS 上的第二个部署单元

- 证据：`architecture.md:42,123`。
- 建议：River worker 首期在 API 同进程启动，用 River 的 queue 并发限制隔离慢任务；队列积压出现后再拆进程。River 自带 schema 迁移与 `cmd/migrate` 的执行顺序要进发布步骤。

### A-7 ·【建议】版本计数器有七种

- 发现：canvas 的 `revision` / `topology_revision`，节点的 `placement_revision` / `data_revision`，库的 `hierarchy_revision` / `library_revision`，加各表自身 `revision`。
- 证据：`architecture.md:150-161`；`data-model.md:87-88,94,139`。
- 建议：placement / data 拆分由 AG-07「运行期间人工改动不被过期结果覆盖」直接支撑，保留。`topology_revision` 与 `hierarchy_revision` 首期并入各自根对象 `revision`，冲突率有数据后再拆。

### A-8 ·【建议】上传候选状态机是为边缘场景设计的独立子系统

- 发现：`creative_upload_candidates` 处理「上传期间目标节点已变」，带 pending / applied / discarded / expired 四态、7 天期限与采用 / 到期竞态规则。
- 证据：`data-model.md:183,190`；`architecture.md` §6.2 第 4 步。
- 建议：首期退化为「目标已变则落入个人库并提示」，候选机制留到模块设计按实际冲突频率决定。

## 二、数据库设计

### D-1 ·【阻断】ID 形态与全仓约定不一致

- 发现：全仓 ID 为 `TEXT` 前缀 + uuid（`cus_`、`cas_`、`cw_`），主键形状是 `id TEXT PRIMARY KEY` + `UNIQUE (account_id, id)`。新设计改为 `id UUID` 且 `PRIMARY KEY (account_id, id)`。
- 证据：`creativeworkspace/repository.go:46`；`customer/repository.go:37`；`0036_creative_workspace.up.sql:72,83`；`data-model.md:14-15`。
- 影响：前端 `schema.d.ts` 要同时处理两种 ID 类型；`legacy_map` 的 source / target 类型不对称；日志与错误信息里无法一眼区分对象类型；AG-07「伪造资源 ID 被拒绝」失去前缀这道最廉价的第一道校验。
- 建议：沿用 `TEXT` 前缀 ID 与仓库主键形状。若坚持 UUID，用 v7 避免随机主键的 B-tree 写放大，并在 data-model §1 说明与旧表并存的处理。

### D-2 ·【重要】三处派生列在无 FK 下不受保护

- 发现：
  - `creative_nodes` 同时存 `content_id` 与 `content_revision_id`，后者已决定前者；现只有 `node_content_pair` CHECK 保证同空同非空。
  - `creative_assets.kind` 复制自 `creative_contents.kind`。
  - `creative_agent_runs.canvas_id` 可由 `conversation_id` 推导。
- 证据：`data-model.md:88,121,138,221`。
- 判断：三处都是为免 JOIN 做的合理反范式，但文档没标为「派生列」。无 FK 下它们是最容易漂移的地方。
- 建议：在表定义里标注「派生列 + 服务端不变量」。若采纳 A-1 混合策略，用复合 FK `(account_id, content_id, id)` 一次锁死第一处。

### D-3 ·【重要】幂等回执双轨

- 发现：平台已有 `idempotency_records`，operation 白名单靠每次迁移 DROP / ADD CHECK 全量重建，0036 重建时已近 40 项。新设计另立 `creative_operation_receipts` 与 `creative_changes.operation_id` 唯一约束。
- 证据：`0006_idempotency_records.up.sql:1-11`；`0036_creative_workspace.up.sql:10-50`；`data-model.md:204,206`。
- 建议：二选一并显式声明。倾向新表族用自己的回执表（命令式写路径与旧 HTTP 幂等语义确实不同），但要写明「creative 命名空间不再向 `idempotency_records` 白名单追加 operation」，避免两处都长。

### D-4 ·【重要】Agent 运行状态机在两份文档间不一致

- 发现：
  - `recoverable` 出现在状态转换表，不在状态列表中。
  - `cancel_requested` 一处是状态、一处是列 `cancel_requested_at`。
  - `reconciling` 只有入边，无出边定义。
- 证据：`architecture.md:226,230`；`data-model.md:221,242,246,247`。
- 建议：模块设计前先出一张唯一权威的状态转换表：每个状态的出边、触发方（worker / 摄影师 / 到期扫描）、是否占 slot。两份文档只引用它。

### D-5 ·【重要】`pg_trgm` 对中文有 locale 陷阱

- 发现：`pg_trgm` 默认只对字母数字提取 trigram，判定依赖数据库 `lc_ctype`。若生产库以 C locale 初始化，中文字符会被整体剥离，GIN 索引对中文查询形同虚设。架构 §6.1 已提「短中文词可能退化」，但没有指向 locale 这个根因。docker 用 `postgres:17-alpine`，生产是 ECS 自装库，两者 locale 不一定相同。
- 证据：`architecture.md:182`；`data-model.md:158,169,176`；`docker-compose.yml:58`。
- 建议：§12 搜索验证加一条：核对生产库 `SHOW lc_ctype`，并用真实中文样本看 `EXPLAIN (ANALYZE)` 是否走 GIN。

### D-6 ·【建议】索引与列缺口

| 缺口 | 证据 | 影响需求 |
|---|---|---|
| `creative_blobs` 无 `(account_id, sha256)` 索引 | `data-model.md:184`；§4 / §10 索引清单均无 | LIB-06 / FLOW-02「不重复上传」 |
| `normalized_title` 只在正文提到，未入 `creative_assets` 表定义 | `data-model.md:138,176` | LIB-04 按名称排序分页 |
| `creative_projects` 无 `last_opened_at`，0036 有此列 | `data-model.md:86`；`0036:82,94` | CAN-01 项目列表按最近打开排序 |

去重规则建议同时写进 LIB-06 验收：同账号按 sha256 复用 blob，跨账号一律不去重（与 EV-4「同哈希不合并归属」一致）。

### D-7 ·【建议】库根锁粒度过粗

- 发现：所有资产 purge 与成员修改都锁 `creative_library_settings` 一行。批量上传完成的 worker 与摄影师打标签会互相排队。
- 证据：`data-model.md:29,151`；`architecture.md:178,204`。
- 建议：只有分组 / 标签结构变更锁根行；单资产写入锁资产行。

### D-8 ·【建议】多币种预算是预支复杂度

- 发现：`creative_agent_budgets` 以 `currency` 入主键，并为此写了跨币种 slot 竞态规则与测试项。
- 证据：`data-model.md:226-227,232,294`。
- 建议：首期单币种，`currency` 保留为普通列；测试项第 4 条去掉「跨币种」。

### D-9 ·【建议】命名不统一

- `position` / `ordinal` / `sequence` / `seq` 表达同一「同父序号」概念；`creative_edges.position` 语义不明。
- 证据：`data-model.md:89,90,219,223,225`；`creative_content_revisions.sequence`。
- 建议：统一为 `ordinal`；边上的排序改名 `target_ordinal`。

## 三、需求与基础功能完善度

需求文档 22 条 LIB / CAN / AG 与 9 条 FLOW 编号完整，每条有可观察结果，架构 §12 的验证项能逐条回溯。以下是边界缺口：

| 编号 | 缺口 | 证据 | 建议 |
|---|---|---|---|
| R-1 | 链接节点语义（存 URL 还是抓取预览）未定义，决定 A-5 是否存在 | `creative-canvas-foundation.md:33,87` | CAN-02 增加一句明确语义 |
| R-2 | 视口持久化归属：CAN-01 要求「内容状态和个人视口分开保存」，架构落到浏览器本地偏好，换设备重开视口丢失 | `creative-canvas-foundation.md:86`；`architecture.md:70` | 需求明示「首期视口不跨设备」或改为服务端按账号画布保存 |
| R-3 | 保留期散落五处：命令日志 30 天、回执 90 天、候选 7 天、SSE 事件 7 天、运行载荷 90 天、回收站 7/30/90 | `architecture.md:170`；`data-model.md:139,190,208,250` | data-model 增加一张保留策略总表，作为隐私说明与到期扫描的唯一来源 |
| R-4 | 素材发往外部模型供应商的知情提示无需求编号。AG-08 只讲能力匹配；Epic EV-4 要求「模型分析」用途独立检查 | `creative-canvas-foundation.md:110`；Epic `:173` | 增加 AG-09 或并入 AG-08：首次外发前提示并记录同意 |
| R-5 | 搜索只有单列拼接文本，无法按标题命中优先 | `data-model.md:145,169` | 首期可接受，LIB-04 验收写明「不承诺相关度排序」 |

## 四、确认无需返工的设计

以下设计经核对与仓库基座一致或优于现状，不建议改动：

- 内容 / 资产 / 节点三层身份与「第一次编辑分叉」规则（architecture §4、data-model §2）。
- 人工与 Agent 共用领域命令、乐观前置条件、`operation_id` 回放（architecture §5.1）。
- 运行的 `execution_epoch` / `lease_until` / 账号 slot `claim_token`、外部调用不包事务、`reconciling` 对结果未知的处理（architecture §7.2）。
- River 事务入队通过 `TxAccountScope` 受信桥接，域服务不拿裸 `pgx.Tx`（data-model §8），与 `scope_tx.go` 现有封装方向一致。
- `creative_legacy_map` 与 expand → 停 writer → 回填 → 对账 → 切换的迁移序（architecture §10），与 0036 的 pilot 账号状态机兼容。
- 读取沿用回环代理加 Range/206，不在持久数据存 URL（architecture §6.3），与 object-storage runbook 一致。

## 五、处置建议

1. **先拍板 A-1 与 D-1**。两项都是与仓库基座的一致性问题，决定后续所有 DDL 与 DTO。
2. **定 A-2 的定位**，再决定 A-3 是否合包。
3. 其余重要项（A-4、A-5、D-2 至 D-5）与建议项直接带入模块设计，不必回架构文档再走一轮 review。
4. 需求缺口 R-1 至 R-4 各是一句话的补充，可与 A-1 / D-1 同批修订。

## 未验证项

- `pg_trgm` 在目标库 locale 下对中文的实际行为。
- React Flow v12 与 React 19、River 与 pgx/v5 的版本兼容（架构已声明未安装）。
- OSS 分片直传的 CORS / RAM 权限。
- 以上均已在架构 §12 列为验证项，本报告不重复宣称。
