---
doc_type: feature-design
feature: 2026-07-21-data-export
requirement: null
roadmap: photographer-private-crm
roadmap_item: data-export
status: approved
summary: 提供账号级一致快照的 reference-only JSON 全量导出，并在设置页交付带 PII 与头像不可携带说明的一键下载入口
tags: [data-export, data-portability, json, account-scope, pii]
---

# data-export 设计

## 0. 术语约定

| 术语 | 定义 | 防冲突结论 |
|---|---|---|
| 全量导出（Data Export） | 当前已认证账号在 roadmap §4.6 明列的全部业务实体与有效设置，打包为一个 schema_version=1 的 JSON 附件 | 不是数据库 dump，也不是管理员跨账号备份 |
| reference-only JSON | Customer 中保留公开的 avatar_revision、avatar_version、avatar_url 引用元数据，但不包含头像字节、内部 object_id 或恢复 manifest | 2026-07-21 owner 已选择；不称“可携带头像备份” |
| 导出文档（Export Document） | exported_at、schema_version、counts、七类实体数组和 settings 组成的公开响应 | shape 以 roadmap §4.6 与 api/openapi.yaml 为准 |
| 导出快照（Export Snapshot） | 同一账号、同一个 PostgreSQL REPEATABLE READ 只读事务中看到的七张业务集合与设置 | 不是长期保存的 snapshot 表，也不新增数据库实体 |
| 有效设置（Effective Settings） | 存储行叠加 Settings 默认值后的读取结果；无存储行时仍返回完整默认设置 | 与 GET /settings 的语义一致，不导出部署环境变量 |
| 稳定数组顺序 | 每类实体按 created_at ASC、id ASC 排列；同一数据库快照的数组内容与顺序可复核 | exported_at 每次不同，因此整个文件不承诺逐字节相同 |

## 1. 决策与约束

### 1.1 需求摘要

本 feature 承接 photographer-private-crm roadmap 的 data-export 条目：摄影师在设置页执行一次操作，即可下载当前账号的结构化业务数据。后端返回 GET /api/v1/export 的 application/json 附件，包含 customers、social_identities、customer_notes、packages、orders、schedule_slots、reminders 与 settings；counts 必须逐项等于对应数组长度。

成功标准：

1. 一个含各域真实数据的账号能下载 schema_version=1 JSON，七类数组、字段、可空性与 roadmap §4.2 / OpenAPI 一致。
2. 导出读取是账号级一致快照；并发写入不能造成数组之间前后时点混杂。
3. 无数据账号也得到七个空数组、全零 counts 和有效默认 settings。
4. 未认证请求返回 401；账号 B 永远看不到账号 A 的任何行。
5. 设置页明确提示文件含全部 PII、需妥善保管，以及头像只含引用、不包含图片且不承诺便携恢复。
6. 下载响应带安全、可预测的附件文件名；失败时不返回伪装成成功附件的半份 JSON。

本 feature 的 requirement 字段为空：data-export 并非本轮首次提出的新能力，而是已批准 roadmap §4.6 和 OpenAPI 中既有的规划条目；本轮不另造愿景文档。若后续要把“可恢复的数据便携性”提升为独立长期能力，再由 cs-req 建立对应 capability。

### 1.2 明确不做

1. 不导出头像二进制、avatar_object_id、对象 key、头像卷 inventory、GC / reconciliation 状态或 exact-generation manifest。
2. 不承诺把本 JSON 导入另一套部署后恢复头像；不实现 import、restore、校验恢复或双向迁移。
3. 不做分页、过滤、字段选择、压缩包、异步导出任务、导出历史、进度轮询或断点续传。
4. 不导出 Account.password_hash、JWT、TG bot token、临时 bind token、idempotency_records、提醒扫描 checkpoint、Telegram delivery/update 状态、后台任务日志等内部或凭证数据。
5. 不提供跨账号管理员导出；客户端不传 account_id，也不能用 query 参数切换账号。
6. 不把导出 JSON 或 PII payload 持久化到服务端临时文件，不把实体内容写入日志。
7. 不清扫全站空态、错误态或其他 375px 轻路径；这些仍属于 v1-hardening。本条只保证设置页导出卡片自身的加载、错误、禁用、键盘与小屏状态。
8. 不改变既有业务实体写语义，不新增业务表或 migration。
9. 不恢复已被物理删除的订单、套系或其他记录，也不补造审计历史；“全量”指当前数据库仍存在的全部契约实体。

### 1.3 复杂度档位

- 健壮性 = L3（偏离内部工具默认 L2：这是生产环境的全量数据出口，所有鉴权、数据库、编码与取消失败都必须 fail closed）。
- 安全性 = hardened（偏离默认 validated：接口可一次性带走全部 PII，需要账号隔离、no-store、敏感字段 allowlist、日志脱敏和负向测试）。
- 可测试性 = verified（偏离默认 tested：账号隔离、快照一致性、内部字段排除和 counts 不变量必须有 PostgreSQL 集成证据）。
- 确定性 = reproducible：数组稳定排序；exported_at 与文件名按本次 UTC 时间变化。
- 性能 = reasonable：每张业务表至多一次账号级批量查询，禁止 N+1；首版不设延迟/内存硬预算。
- 其余沿用项目生产 Web feature 档位：结构 layers、可读性 team、可演进性 stable、可观测性 logged。

### 1.4 方案深度 pre-pass

候选一是将全部记录、公开 OpenAPI projection 和最终 JSON bytes 都先物化到内存，完整序列化成功后再发 200；候选二是边查边向 HTTP 流式写；候选三是写到服务端临时文件后再下载。

本场景选择候选一，理由不是“实现更简单”，而是：

- 首版是单账号摄影师自用，排除头像字节后只剩结构化 JSON，当前没有大体量证据要求流式化。
- 在发送任何 200 header 前可完成全部数据库读取、公开响应映射与 JSON 序列化；任一应用层节点失败仍能返回标准错误封套，而不是留下无法判定的截断附件。
- 不在服务器磁盘留下额外 PII 临时副本，也不引入临时文件权限、清理和空间耗尽风险。

代价是内存为 O(导出 JSON 总量)，并且映射/编码期间可能同时保留领域记录与响应对象。这个边界在首版显式接受；若真实账号的结构化数据规模使内存或延迟不可接受，再以测得的体量另起 streaming / async export 设计，不能在本 feature 偷加不完整流式路径。

核心逻辑不使用 fake 替代：生产路径必须走真实 PostgreSQL 账号快照；fake repository 只用于 Service 的时钟、schema_version、len-derived counts 与 Document 构造单测。稳定排序、账号隔离和事务一致性由真实 PostgreSQL repository 集成测试证明。

### 1.5 关键决策

#### D1 — owner 选择固化为 reference-only schema v1

Customer 仍按公开 Customer shape 导出 avatar_revision、可选 avatar_version 与可选 avatar_url。avatar_url 是当前部署的同源鉴权相对 URL，只是引用；输出不得出现内部 avatar_object_id、object key、实际字节或 manifest。UI 和验收证据必须直说“头像图片未包含，不能据此跨环境恢复头像”。

#### D2 — 独立 dataexport 跨域读模型，不注入七个领域 Service

新增独立 dataexport 只读模块，由它的 Postgres repository 在 AccountScope 下读取各业务表并组装 Snapshot。遵守 compound 2026-07-09-cross-domain-read-model：同库跨域展示读模型归展示方 repository，不为每个领域增加 ListAll service-to-service 假 seam。

被拒方案：

- 让 dataexport.Service 依赖 customer/order/schedule/reminder 等七个 Service：每个公开列表都有分页、筛选或摘要语义，无法表达“原始全量实体”；还会跨多次事务并把导出口径散到多个 caller。
- 用 information_schema、SELECT row_to_json(*) 或数据库 dump 自动导出全部列：会绕过 schema v1 allowlist，未来新增内部列时静默泄密，并把内部 avatar generation / 状态表带出。

#### D3 — 一个账号级 REPEATABLE READ、READ ONLY 快照

AccountScope 增加最小只读快照入口，回调只得到 ReadTxAccountScope；该窄类型只暴露 Query / QueryRow / QueryPage / Count / ScalarAggregate / Exists 等受控读取能力，不暴露 Insert / Upsert / Update / Delete / QueryRowForUpdate。底层同时以 PostgreSQL REPEATABLE READ + READ ONLY 开启事务，形成“Go 能力面 + 数据库模式”两层只读守护。dataexport repository 在同一回调里顺序读取七类实体与设置。任何 query、scan、设置解析或 commit 失败，整个 Snapshot 失败。

不使用现有默认 WithTxScope 代替：它由 pool.Begin 使用默认 READ COMMITTED，多个 SELECT 可能观察到不同提交时点。也不使用表锁或 FOR UPDATE；MVCC 一致读不应阻塞正常业务写。

#### D4 — counts 从最终数组长度派生，数组稳定排序

PostgresRepository 返回非 nil 数组，并按 created_at ASC、id ASC 稳定排序；排序属于 repository invariant，由真实 PostgreSQL 测试用乱序 fixture 证明。Service 不负责排序，只从最终数组 len 计算 counts，不另跑 count(*)，因此 counts 与返回数组不可能因查询时点或过滤漂移而不一致。

所有状态都导出：Customer 的 active/archived/merged、Package 的 active/archived、Order 的八态、Reminder 的 pending/done/dismissed、Schedule Slot 的三态均不做业务列表默认过滤。

#### D5 — settings 输出有效读取结果

settings 是单对象，不进入 counts。无 settings 行时导出 DefaultSettings；有行时复用 settings 域的默认叠加纯函数，保证和 GET /settings 一致。telegram_chat_id 属于 roadmap Settings shape，因此可选导出；TG bot token、bind token 和 delivery 状态不属于 Settings shape，始终排除。

#### D6 — 导出模型是公开 allowlist，HTTP 层映射复用现有 API mapper

实现时必须把 api/openapi.yaml 中 `/export` 的匿名响应提升为 `components.schemas.ExportDocument` 与 `components.schemas.ExportCounts`，并让 200 `application/json` 响应通过 `$ref` 指向 `ExportDocument`。两个命名 schema 保持现有 JSON 字段与 required 集合不变，均设 `additionalProperties: false`；`ExportCounts` 的七个整数字段均设 `minimum: 0`。双端 codegen 必须生成可命名引用的顶层类型，不能因匿名 response 缺少 Go 类型而临时手写顶层 HTTP DTO。

Snapshot 可以承载现有领域实体，但最终 Export Document 必须映射为上述 OpenAPI 生成类型。Customer 映射复用现有公开投影逻辑，只从 customer ID、revision、version 派生 avatar_url，绝不序列化领域结构中的 AvatarObjectID / media metadata。Package、Order、ScheduleSlot、Reminder、Settings 同样按既有 API mapper 输出，避免导出另造字段口径。

#### D7 — 全部序列化成功后才发送附件

成功路径固定为：Service Build Document → HTTP adapter 映射 OpenAPI 公开 shape → 完整 json.Marshal（或等价的全量预序列化）得到 bytes → 再检查一次 request context → 最后设置 200、附件 headers、Content-Length 并写出。Build、映射、序列化或发送前 context 失败时不得写成功 headers；仍连接的请求走标准错误封套，已取消请求只记录取消并由调用端观察。

成功 headers 写出后的底层 transport 中断是不可逆边界：服务端不能把已经发送的 200 改写成 500，只能记录不含 PII 的 transport failure；Content-Length 与浏览器 body/blob 读取失败负责让调用端识别不完整下载。设计不承诺把网络中断伪装成可回滚的应用错误。

#### D8 — 安全下载 headers 与文件名

成功响应至少包含：

- Content-Type: application/json
- Content-Disposition: attachment; filename="photographer-crm-export-YYYYMMDDTHHMMSSZ.json"
- Content-Length: 完整预序列化 JSON bytes 的长度
- Cache-Control: no-store
- X-Content-Type-Options: nosniff

文件名与 exported_at 使用 Service 捕获的同一个 UTC 时点，只含固定 ASCII 前缀和数字时间，不含账号 ID、客户名或任何 PII。OpenAPI 显式声明 Content-Disposition 与 Cache-Control，不能只留在描述文字。

#### D9 — OpenAPI 全量契约、Go tag 切片、router 真实暴露面保持一致

api/openapi.yaml 的 export tag 已存在；本 feature 把 export 加入 backend/oapi-codegen.yaml include-tags，运行双端 codegen，并在受 auth 中间件保护的 group 手工注册 GET /export。router_test 不再把 /api/v1/export 列为未实现端点，改为验证无 token 401 与未知 API 仍 404。

#### D10 — 设置页交付一键下载卡片

SettingsPage 增加独立 DataExportCard，状态与设置表单、Telegram 绑定互不耦合；普通 Settings 加载失败时卡片仍可渲染并独立尝试导出。API client 用 Bearer fetch；对 200 响应用 MIME media-type parser 解析 Content-Type，base media type 必须精确为 `application/json`，合法参数（例如 `charset=utf-8`）允许并忽略，禁止用 substring 匹配，也不接受任意 `+json`。client 从 Content-Disposition 读取文件名时只接受 whole-string regex `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$`；任何路径分隔符、控制字符、超长或非预期名字都回退到本地固定 UTC 名。只有 body/blob 完整读取成功并返回 `{blob, filename}` 后，UI 才创建临时 object URL、触发下载并释放 URL；读取拒绝时 client 必须 reject，UI 展示可重试错误且不得创建 object URL 或触发下载。

卡片必须有：

- 明确的“导出全部 JSON 数据”按钮；
- 下载中禁用与可读文案；
- 401 返回登录页；
- 其他错误在卡片内 role=alert 展示，可再次点击重试；
- 常驻敏感信息保管提示：文件包含姓名、手机号、社交身份、备注、Telegram chat ID 等信息，只应保存到受控位置且不需要时及时删除；头像仅含公开引用，图片未包含且不可便携恢复；
- 桌面与 375px 下不横向溢出，按钮可键盘聚焦和触发。

#### D11 — 敏感路径 fail closed 且只记录元数据

所有表读取都通过由 auth.AccountContext 构造的 AccountScope；任何空 scope 或 Service/Repository/ScopeFactory/Clock 依赖缺失都要返回显式内部错误，不 panic、不回退其他账号或伪造空文档。服务端允许记录 export_completed / export_failed、失败阶段、耗时、序列化字节数和七类 counts，但禁止记录 JSON payload、姓名、手机、社交 handle、备注、chat_id 或 token。浏览器不把 Blob 写入 localStorage / sessionStorage / IndexedDB。

#### D12 — 不新增数据库 schema，schema_version 只描述导出文档

schema_version 固定为整数 1，表示导出 JSON 的公开结构，不等于数据库 migration 版本。字段有破坏性变化时必须另走 roadmap / OpenAPI 决策并递增版本；本 feature 不实现多版本协商或 import compatibility。

### 1.6 执行风险与证据计划

Top 3 风险：

1. 跨表前后时点混杂，导出出现已删除客户仍被后一次查询引用或 counts 漂移。缓解：D3 一致读快照、store 事务集成测试与 A8 并发写证据。
2. 高价值 PII 或头像内部 generation 意外泄漏。缓解：D6 allowlist 映射、A6/A7 负向 JSON key 扫描、跨账号 A5 与 no-store headers。
3. 浏览器拿到 200 但文件被截断、文件名错误或 object URL 泄漏。缓解：D7 预序列化 + Content-Length + 明确 transport 边界、后端 writer 与前端 body/blob rejection 双侧测试，以及 A9/A12/A13 证据。

非显然依赖：

- platform-skeleton、customer-profile-complete、customer-avatar、package-catalog、order-tracking、schedule-calendar、reminder-engine 均为 done，所需表与公开 shape 已存在。
- 当前 OpenAPI 已有 /export，但 Go include-tags 和 router 故意未暴露；实现顺序必须保持 codegen 与真实路由同步。
- AccountScope 当前只有默认 READ COMMITTED 事务入口，D3 需要先增加受控只读快照能力。
- Settings 允许无存储行，导出不能把“无行”错误解释为空对象。

关键假设（整稿 review 时可反驳）：

- A-H1：单摄影师首版排除媒体后，结构化 JSON 可在合理内存内一次物化；当前不需要流式/异步。
- A-H2：数组按 created_at ASC、id ASC 最适合审计和稳定复核；业务列表的展示排序不应污染导出。
- A-H3：exported_at 表示本次导出开始时间，统一输出 UTC。
- A-H4：全量意味着包含 archived/merged/cancelled/done/dismissed 等历史状态，而不是复用各列表默认过滤。
- A-H5：设置页常驻警示 + acceptance 后建议 cs-docs 补摄影师使用指南/API 参考，足以满足本 feature 的 PII 保管说明；不在 cs-feat 内越权维护完整外部文档。
- A-H6：telegram_chat_id 是 Settings 业务字段，按既有契约导出；它是 PII，UI 的“全部数据”警示覆盖它。

验证基线：

- CLAUDE.md 与 Makefile 指定 make check 为聚合门禁，OpenAPI 改动必须 make generate 并提交 Go/TS 生成物。
- 最新已完成 feature 的验收记录显示门禁全绿。进入本 design 前的既有无关脏项是未跟踪 .workflow/ 与 install-cpamp.sh；本 feature 当前预期 diff 是新增 data-export design/checklist/design-review，并修改 roadmap items（以及 owner 决策 resolved 注记），不得把两类改动混淆。
- 本机 Docker/Testcontainers 高并发存在已知端口 flake；定向和聚合 Go 测试都按 -parallel=1 归因，不能把环境 flake 当成本 feature 缺陷。
- 当前 GET /api/v1/export 被 router_test 明确当作未实现端点，运行时 404；这是本 feature 的预期起点。

必跑验证命令见第 3 节 DoD Contract。交付物类别：

- OpenAPI export headers / 生成物 / export tag；
- 账号级 read snapshot store 能力及集成测试；
- dataexport read model、Service、PostgreSQL repository 与 HTTP adapter；
- router 与 composition root 装配；
- 前端 Blob 下载 client、DataExportCard、前端测试与浏览器证据；
- 本 design/checklist、review/QA/acceptance/evidence 产物及 roadmap items 状态回写。

清洁度规则：

- 禁止调试打印、console.log、临时占位标记、注释掉代码、无用 import。
- 禁止记录/持久化导出 payload、Blob、PII 或 token；测试 fixture 使用虚构数据。
- 禁止手写重复的前端 Export DTO，类型只从生成的 schema.d.ts / paths 派生。
- 禁止把内部 avatar_object_id、存储路径或数据库 row 整体直接 JSON marshal。

## 2. 名词与编排

### 2.1 名词层

#### 现状

- api/openapi.yaml 已用匿名 inline object 定义 GET /export 的 schema_version=1、counts、七类数组与 settings，尚无可命名引用的 ExportDocument / ExportCounts component schema；export tag 未进入 backend/oapi-codegen.yaml，因此 api.gen.go 没有 ExportAll handler 或顶层导出响应类型，router.go 也未注册，router_test 将其断言为 404。
- backend/internal/platform/store/scope.go 的 AccountScope / TxAccountScope 是业务查询唯一账号隔离句柄；WithTxScope 使用默认 pool.Begin，没有只读、REPEATABLE READ 快照入口。
- customer、package、order、schedule、reminder、settings 已各自有领域实体和 PostgreSQL scanner；Customer 领域结构还含不应公开的 avatar_object_id/media pointer。
- settings.Service.Get 负责“无行用默认值、有行叠加默认值”；默认叠加函数当前只在 settings 包内使用。
- frontend/src/api/client.ts 的普通 request 只处理 JSON 解析，头像 client 已有 Bearer Blob fetch 与错误封套解析的可复用模式。
- SettingsPage 当前承载设置表单与 Telegram 绑定卡片，没有数据导出入口；AppShell 已展示“数据可随时导出”文案。

#### 变化

| 动作 | 名词 | 变化与动机 |
|---|---|---|
| 新增 | dataexport.Snapshot | 七类领域记录 + 有效 Settings 的同一时点只读集合 |
| 新增 | dataexport.Document / Counts | schema_version=1 的应用层导出结果；counts 从数组长度派生 |
| 新增 | OpenAPI ExportDocument / ExportCounts | 把匿名 `/export` response 提升为命名 component；保持现有字段/required，禁止额外属性，counts 最小值为 0 |
| 新增 | dataexport.Service | 固定 UTC exported_at、编排 repository、构造 Document，不依赖 Gin |
| 新增 | dataexport.Repository + PostgresRepository | 在 dataexport 展示域内集中读取所有 allowlist 表 |
| 新增 | store.ReadTxAccountScope | 只暴露账号级读能力，类型层禁止导出流程调用写 delegate |
| 扩展 | AccountScope.WithReadSnapshot | 只暴露 ReadTxAccountScope 的 REPEATABLE READ、READ ONLY 回调 |
| 扩展 | Settings 默认叠加纯函数 | GET /settings 与 data export 共用同一有效设置语义 |
| 启用 | OpenAPI ExportDocument / ExportCounts Go 类型与 ExportAll operation | export tag 进入 include-tags；HTTP 只映射生成的顶层类型，仍由 router 手工挂载 |
| 新增 | 前端 fetchDataExport | Bearer fetch → {blob, filename}，错误复用 ApiError |
| 新增 | DataExportCard | 设置页内聚下载、loading/error、PII 与头像限制文案 |

#### 接口示例

应用与存储接口：

~~~go
type Repository interface {
    LoadSnapshot(context.Context, store.AccountScope) (Snapshot, error)
}

type Service struct {
    repo  Repository
    clock Clock
}

type Clock interface {
    Now() time.Time
}

func (s *Service) Build(context.Context, store.AccountScope) (Document, error)

func (scope AccountScope) WithReadSnapshot(
    context.Context,
    func(ReadTxAccountScope) error,
) error
~~~

WithReadSnapshot 的 caller 只知道“账号隔离、只读、同一快照”；ReadTxAccountScope 不暴露任何写方法，caller 也不能拿到 pgx.Tx、开启嵌套事务、改 isolation 或注入 account_id。Service 的 Repository / Clock 依赖为空时 Build 返回显式内部错误，不 panic、不构造假文档。

HTTP 正常示例：

~~~http
GET /api/v1/export
Authorization: Bearer <token>

HTTP/1.1 200 OK
Content-Type: application/json
Content-Disposition: attachment; filename="photographer-crm-export-20260721T083015Z.json"
Content-Length: <完整 JSON bytes 长度>
Cache-Control: no-store
X-Content-Type-Options: nosniff
~~~

~~~json
{
  "exported_at": "2026-07-21T08:30:15Z",
  "schema_version": 1,
  "counts": {
    "customers": 1,
    "social_identities": 1,
    "customer_notes": 0,
    "packages": 0,
    "orders": 0,
    "schedule_slots": 0,
    "reminders": 0
  },
  "customers": [{
    "id": "cus-1",
    "account_id": "acct-1",
    "created_at": "2026-07-01T00:00:00Z",
    "display_name": "示例客户",
    "channel": "other",
    "status": "active",
    "avatar_revision": "ar-2",
    "avatar_version": "sha256-...",
    "avatar_url": "/api/v1/customers/cus-1/avatar/content?v=sha256-..."
  }],
  "social_identities": [],
  "customer_notes": [],
  "packages": [],
  "orders": [],
  "schedule_slots": [],
  "reminders": [],
  "settings": {
    "timezone": "Asia/Shanghai",
    "birthday_lead_days": 3,
    "follow_up_after_days": 7,
    "churn_thresholds": [
      {"shoot_type": "portrait", "days": 180},
      {"shoot_type": "cosplay", "days": 180},
      {"shoot_type": "other", "days": 180}
    ],
    "digest_hour": 9
  }
}
~~~

主要错误：

- 无 token / token 无效 → 401 unauthorized。
- 任一快照查询、scan、设置解析、事务 commit、响应映射或 JSON 预序列化失败 → 500 internal，且不带成功附件 headers。
- 发送成功 headers 前 context 取消 → 不写成功响应；headers 后 transport 中断不可改写 500，记录失败并由客户端 body/blob 读取与 Content-Length 检出。
- 空 AccountScope 或缺 Repository / Clock / ScopeFactory 等依赖 → 500 internal，不 panic、不回退其他账号、不返回全空 200。
- 账号 B 请求时只能得到 B 的数据；A 的 ID 即使可猜也不成为请求输入。

#### Design It Twice

| 候选 | Depth / locality | 快照能力 | 泄漏风险 | 结论 |
|---|---|---|---|---|
| dataexport 直接注入七个领域 Service | 接口宽且多，分页/摘要语义散到多个 caller | 无法自然共享一个事务 | 每域新 Export 方法容易漂移 | 拒绝 |
| dataexport 专用 read-model repository | HTTP 只看到一个 Build；allowlist、排序、counts 集中 | 一个 AccountScope read snapshot | 可在一个模块做字段负向扫描 | 采用 |
| 通用数据库 JSON/dump | 表面接口最小，但内部不隐藏业务语义 | 可做 DB snapshot | 新内部列会自动泄漏，无法保证公开 shape | 拒绝 |

##### Interface 设计检查

- Module：dataexport（新增跨域只读 read model）+ AccountScope read snapshot（扩展现有 store 边界）。
- Interface：caller 只传 context 与 AccountScope；成功返回完整 Document，失败不返回部分结果；schema_version、排序、counts 与 reference-only allowlist 是不变量。
- Seam：HTTP GET /export 是远端 seam；dataexport.Repository 是本地可替换存储 seam；AccountScope 是唯一账号隔离 seam。
- Depth / locality：删除 dataexport 后，七表 allowlist、快照、稳定顺序、counts 和排除规则会散回 handler/各域，复杂度不会消失，通过 deletion test。Handler 仍只做 auth、映射、headers。
- Dependency strategy：PostgreSQL 是 local-substitutable；HTTP 是 remote-owned（浏览器消费）；无 true external。
- Adapter：production = PostgresRepository；Service 测试 = fake Repository。账号隔离/事务不依赖 fake，另有真实 PostgreSQL 集成测试。
- Test surface：真实 PostgresRepository 观察 A2–A8、A11、A16/A17（含稳定排序与 query 次数）；store 同包集成测试通过 ReadTxAccountScope 的私有 transaction runner 在同一 session 读取事务属性，观察 A18，而不扩展生产只读接口；Service fake 只观察时钟、schema_version、counts 与 Document 构造；HTTP seam 观察命名生成类型、headers/401/500/发送边界；前端 fetch seam 观察 Blob/filename/MIME media type/401 与 body/blob 读取拒绝。

### 2.2 编排层

~~~mermaid
sequenceDiagram
  participant UI as DataExportCard
  participant Client as fetchDataExport
  participant API as GET /export
  participant Svc as dataexport.Service
  participant Repo as PostgresRepository
  participant Scope as AccountScope Read Snapshot
  participant DB as PostgreSQL

  UI->>Client: 点击“导出全部 JSON 数据”
  Client->>API: Bearer GET
  alt 未认证
    API-->>Client: 401 ErrorEnvelope
    Client-->>UI: ApiError，转登录页
  else 已认证
    API->>Svc: Build(ctx, scope)
    Svc->>Svc: 捕获 exported_at UTC
    Svc->>Repo: LoadSnapshot(ctx, scope)
    Repo->>Scope: WithReadSnapshot(ReadTxAccountScope)
    Scope->>DB: BEGIN REPEATABLE READ READ ONLY
    loop allowlist 七类实体 + settings
      Repo->>DB: account-scoped SELECT
      DB-->>Repo: rows
    end
    Repo->>Repo: 解析有效 settings + 稳定排序
    Scope->>DB: COMMIT
    Repo-->>Svc: Snapshot
    Svc->>Svc: counts = len(arrays)，schema_version=1
    Svc-->>API: Document
    API->>API: 映射 OpenAPI 类型 + 完整序列化 bytes
    API->>API: 发送前检查 ctx；最后设置 headers/Content-Length
    API-->>Client: 200 application/json bytes
    Client-->>UI: blob + 安全文件名
    UI->>UI: 临时 object URL 下载并释放
  end
~~~

#### 现状

当前 /export 只存在于机器契约和 TS 全量类型中，运行时仍 404。没有跨七类实体的后端编排；各业务页面分别走分页/筛选 repository。AccountScope 多查询事务是 READ COMMITTED，不能作为导出一致快照。

#### 变化

流程升级为“受 auth 保护的线性只读 pipeline + 单事务多节点查询”。Repository 内部节点顺序固定，但结果语义不依赖查询顺序，因为它们共享同一 MVCC snapshot。Service 不调用任何领域写 API；前端不拼七个分页 API。

#### 流程级约束

- 错误语义：401 由 auth 中间件产生；依赖缺失、空 scope、查询/scan/commit/映射/预序列化失败统一 500。成功 headers 只在完整 bytes 生成且发送前 context 仍有效后设置；headers 后 transport 中断只能记录并让客户端识别，不能回退 500。
- 幂等性：GET 不写数据库；同一底层 snapshot 会得到同内容/顺序，重复请求只允许 exported_at 和文件名变化。
- 并发：REPEATABLE READ 只读事务不锁业务行；事务开始后提交的新写入留到下次导出。请求 context 取消必须中止查询并回滚。
- 顺序：每类数组 created_at ASC、id ASC；counts 在排序后从最终数组 len 计算。
- 账号隔离：每个 SELECT 经 ReadTxAccountScope 自动拼接 account_id；无裸 pool query、无客户端 account_id。
- 扩展点：schema v2 只能经 roadmap/OpenAPI 决策新增；不能通过“自动导出新表/新列”扩展。
- 可观测性：允许结构化记录结果、失败阶段、耗时、序列化字节数与 counts；禁止 payload 和单条实体字段。
- 内存：只物化结构化 JSON；不读取 avatar bytes。无数据时数组必须编码为 [] 而不是 null。

### 2.3 挂载点清单

1. MOUNT-1 — OpenAPI / Go codegen：api/openapi.yaml export operation headers + backend/oapi-codegen.yaml export tag — 修改。
2. MOUNT-2 — HTTP 暴露面：受保护 router group 注册 GET /api/v1/export — 新增。
3. MOUNT-3 — 设置页公共 UI：SettingsPage 挂入 DataExportCard — 新增。

删除 2 后后端能力不可达；删除 3 后摄影师失去“一键导出”入口；删除 1 会让实现与公开契约/codegen 漂移。dataexport 内部文件、scanner 与 composition-root import 属实现细节，不列为挂载点。

### 2.4 推进策略

1. 结构微重构：把 AccountScope 事务类型、入口与 TxAccountScope delegates 纯移动到独立事务文件，不改签名或行为。
   退出信号：store 既有测试全绿，公开 Go 签名与 SQL 行为零变化，diff 只有 move/import 调整。
2. 契约与隔离编排骨架：把 `/export` 匿名 response 提升为 OpenAPI `ExportDocument` / `ExportCounts` 命名 component，细化 headers、启用 export tag/codegen，建立 dataexport Document/Service/Repository interface 与未挂生产 route 的 HTTP handler 隔离测试。
   退出信号：`/export` 响应 `$ref` 到 `ExportDocument`，生成物出现可命名的 ExportDocument / ExportCounts 顶层类型且零漂移；两个 schema 保持原字段/required、禁止额外属性、counts 均不小于 0；handler fake 测试返回 schema_version=1 的七个空数组与零 counts；router/composition root 仍未暴露伪空生产导出。
3. 一致快照与全量计算节点：实现 ReadTxAccountScope / WithReadSnapshot、七类 allowlist 批量查询、有效 settings、repository 稳定排序和 len-derived counts；真实 repository 完成后才注册受保护 route 与 composition-root 注入。
   退出信号：真实 PostgreSQL fixture 的全部状态/字段逐项相等，空账号返回 [] + 默认 settings，A2–A4、A8、A16–A18 自动化通过；A18 在同一事务 session 独立证明 `transaction_isolation=repeatable read` 与 `transaction_read_only=on`，且生产 ReadTxAccountScope 仍无写方法；生产 route 无 token 401、有效 token 读取真实数据。
4. 安全与失败 harden：补跨账号、敏感字段排除、依赖/空 scope、并发时点、发送前预序列化/context cancel、headers 后 transport 边界、headers/文件名和脱敏日志证据。
   退出信号：A5–A11 负向测试通过，JSON key 扫描无内部 generation/凭证状态。
5. 前端下载 client：实现 Bearer Blob fetch、严格 MIME media-type 解析、固定 whole-string filename regex/fallback、ApiError/401 和 object URL 可释放的下载协作；补 body/blob 读取拒绝边界，新增 npm run test:data-export 并接入 Makefile test。
   退出信号：定向脚本覆盖 token、成功 Blob、`application/json` 合法参数、错误 base type/substring/`+json`、精确文件名、500/401 与 `response.blob()` rejection；读取拒绝时 client 不返回 `{blob, filename}`，协作层不创建 object URL、不触发下载并呈现可重试错误；make test 会实际运行该脚本，且不写浏览器持久存储。
6. 设置页交互：挂入 DataExportCard，补 loading/error/retry、常驻 PII/reference-only 文案、键盘与 375px 状态。
   退出信号：桌面与 375px 浏览器均能一次点击下载可解析 JSON；失败可重试，无横向溢出或重复请求。
7. 全链路验证与范围清扫：运行 CMD-001～004，保存 API headers/JSON 核对与浏览器证据，确认显式不做项均未进入 diff。
   退出信号：全部核心场景有证据、make check 全绿、无临时/敏感产物残留。

### 2.5 结构健康度与微重构

#### 评估

- compound：命中 2026-07-09-cross-domain-read-model、2026-07-06-accountscope-fail-loud、2026-07-07-openapi-feature-tag-slicing；本设计直接按这些归属与命名约束执行。结构关键词未命中需要另立的前端目录 convention。
- 文件级 — backend/internal/platform/store/scope.go（564 行）：职责仍围绕 AccountScope，但事务入口、TxAccountScope delegates 与普通 scoped CRUD 已形成两个自然子块；本次还要新增 ReadTxAccountScope / read-snapshot 事务语义，继续塞入会扩大已超过 500 行的文件。
- 文件级 — backend/internal/platform/httpapi/router.go（190 行）、backend/cmd/server/main.go（约 247 行）：本轮各一处 route/dependency 装配，改动密度低。
- 文件级 — frontend/src/api/client.ts（418 行）：虽集中多个 API 域，但本轮新增一个附件 fetch，职责仍是统一 transport/auth/error；接近 500 但只加一处，不在本 feature 重划全部 client。
- 文件级 — frontend/src/pages/SettingsPage.tsx（280 行）：已含表单与 TG 绑定，若再内联下载状态会出现第三个独立交互职责；新逻辑应放独立 DataExportCard，页面只挂载。
- 文件级 — frontend/src/index.css（802 行）：本卡片可复用现有 card/button/form-error/settings-stack，不要求追加专用样式，因此不触碰该胖文件。
- 目录级 — backend/internal/dataexport 当前不存在：新增小型独立 slice，与 dashboard/order/reminder 并列，不把七域 query 塞进 platform/httpapi。
- 目录级 — backend/internal/platform/httpapi 当前 25 个 Go 文件，但已形成“每个业务 slice 一个 handler + test”的稳定命名；新增 data_export.go / data_export_test.go 延续模式。整体重组会触及生成物与全部路由，超出只搬不改。
- 目录级 — frontend/src/components 已有子目录分组；新增单个 DataExportCard 不满足“同层新增 ≥2”摊平触发器。

#### 结论：微重构（拆文件）

#### 方案

- 搬什么：从 scope.go 纯移动 TxAccountScope 类型、WithinTx / WithTxScope / 私有 transaction helper，以及 TxAccountScope 的既有 delegates；此步尚不新增 ReadTxAccountScope。
- 搬到哪：同包 backend/internal/platform/store/scope_tx.go；AccountScope 定义、普通 Query/Count/CRUD 与校验继续留 scope.go。
- 行为不变怎么验证：gofmt；store 全部既有测试通过；公开方法签名零 diff；SQL snapshot/isolation 在纯移动步骤不改变。
- 步骤序列：
  1. 只移动现有类型/方法与必要 imports，运行 store 测试。
  2. 确认 git diff 无语义修改后，再在 scope_tx.go 新增窄 ReadTxAccountScope、WithReadSnapshot 及其测试。

#### 超出范围的观察

- httpapi 同包文件数已多，但按业务 slice 命名仍可定位；未来若要分子 package，会涉及生成 ServerInterface、共享 mapper 与 router 依赖重划，应另走 cs-refactor。
- client.ts 正接近 500 行；如果 v1-hardening 继续新增多个媒体/附件 transport，可另走 cs-refactor 按 transport 类型拆分。本 feature 只加 data-export 所需最小附件入口。

## 3. 验收契约

### 3.1 关键场景清单

| ID | 输入 / 触发 | 期望可观察结果 | 证据类型 |
|---|---|---|---|
| A1 | 实现前后执行聚合门禁与 codegen 漂移检查 | 全部命令退出码 0；既有 Docker 端口 flake 单独归因 | command |
| A2 | 完成 OpenAPI codegen；账号 A 各创建至少一条 Customer、Identity、Note、Package、Order、Slot、Reminder 与非默认 Settings 后导出 | `/export` 响应 `$ref` 到命名 ExportDocument，Go 生成可引用的 ExportDocument / ExportCounts；200、schema_version=1、七数组 shape 与 OpenAPI 相同，counts 逐项等于 len | codegen diff + PostgreSQL integration + API |
| A3 | 新账号无业务行、无 Settings 行后导出 | 七数组均为 []、counts 全零、settings 为完整默认值，不出现 null 数组 | integration + JSON |
| A4 | fixture 含 active/archived/merged 客户、active/archived 套系、订单八态、档期三态、提醒三态 | 所有历史/终态记录都出现，且各数组 created_at ASC、id ASC | integration |
| A5 | 无 token 请求；账号 B 请求且库中 A/B 均有数据 | 无 token 401；B 文档只含 B 行且不接受 account_id 切换 | security integration |
| A6 | 客户有真实头像 pointer；禁止字段和值分别注入唯一哨兵后导出，并解析 JSON 递归核对结构键 | 有公开 revision/version/url；禁止键与哨兵值均不出现；无 avatar_object_id、object key、media metadata、字节、manifest/GC 字段 | integration + structural negative scan |
| A7 | account auth、idempotency、scan checkpoint、Telegram bind/delivery 状态与 bot/bind token 分别注入唯一哨兵后导出 | allowlist 结构中无 password_hash/JWT/bot token/bind token/idempotency/checkpoint/delivery/log 字段或哨兵值；telegram_chat_id 仅作为 Settings 字段按契约出现 | security structural scan + diff |
| A8 | 导出事务第一类查询后，并发事务新增/修改后续域记录并提交 | 当前导出只看到同一旧 snapshot；下一次导出看到新提交；两次 counts 都与数组一致 | concurrency integration |
| A9 | 分别注入空 AccountScope、缺 Repository/Clock/ScopeFactory、query/scan/settings/commit/mapping/JSON 预序列化失败、发送前 context 取消，以及 headers 后 transport write 失败 | 发送前失败无 panic、无账号 fallback、无 200/附件 headers/假空文档；可响应时走 500，已取消时不写成功响应；headers 后 write 失败只记脱敏 transport failure、不尝试第二个错误封套；客户端识别另由 A12/A13 证明 | fault test + HTTP writer |
| A10 | 成功导出 | Content-Type JSON、Content-Length 等于 bytes、attachment 文件名与 exported_at 同一 UTC 时点、Cache-Control no-store、nosniff | HTTP test |
| A11 | 同一静态数据连续导出两次 | 无数据库写；数组内容与顺序一致；只允许 exported_at/文件名变化 | integration + diff |
| A12 | 前端 client 收到 200 `application/json`、带合法参数的 `application/json; charset=utf-8`、错误 base type/仅 substring 命中/任意 `+json`，以及合法、缺失、含路径/控制字符/超长/非固定格式的 Content-Disposition；另 mock `response.blob()` 或 body read reject | Bearer 正确发送；MIME parser 只接受 base media type 精确为 `application/json` 并忽略合法参数；文件名只接受整串 `^photographer-crm-export-[0-9]{8}T[0-9]{6}Z\.json$`，其余回退固定 UTC 名；body/blob 读取拒绝时 client reject，不返回 `{blob, filename}` | frontend test |
| A13 | 点击导出成功、500、401、Settings 加载失败、body/blob 读取中断，并快速重复点击 | 下载中按钮禁用；成功只触发一次下载并释放 object URL；500 或读取中断在卡片内显示可重试错误；读取中断不创建 object URL、不触发下载；401 转登录；普通 Settings 加载失败不隐藏独立导出卡 | frontend test + browser |
| A14 | 桌面、375px 与键盘访问设置页导出卡片 | 常驻文案列出姓名、手机号、社交身份、备注、Telegram chat ID 等敏感信息，说明受控保管/及时删除；明确头像图片不包含且不可便携恢复；无横向溢出；按钮可聚焦/Enter 或 Space 触发 | browser screenshot + manual |
| A15 | 检查路由、UI 与 git diff | 无 import/restore、分页/filter、zip/async/history；未知 API 仍 404；无服务端临时文件与浏览器持久化 | diff review + route test |
| A16 | 账号有部分/完整自定义 churn thresholds 与 telegram_chat_id | 导出 settings 与 GET /settings 有效值逐字段一致，默认 entry 不丢失 | API parity test |
| A17 | 用 query recorder / PostgreSQL statement 证据运行含多条各域数据的导出 | 七类业务表与 settings 各至多一次账号级批量读取，无逐实体附加查询或 N+1；counts 不另查 count(*) | repository test + diff review |
| A18 | store 同包集成测试进入 WithReadSnapshot，并经 ReadTxAccountScope 的私有 transaction runner 在同一 transaction session 读取 `current_setting('transaction_isolation')` 与 `current_setting('transaction_read_only')`；同时做类型能力断言 | 分别得到 `repeatable read` 与 `on`；production ReadTxAccountScope 不暴露 Insert/Upsert/Update/Delete/QueryRowForUpdate 或原始 pgx.Tx，不为测试新增公开写面 | PostgreSQL integration + interface assertion |

### 3.2 明确不做的反向核对

- 解析后的结构键 allowlist 与唯一哨兵值双重扫描不得出现把 avatar_object_id、avatar_object_gc、avatar_reconciliation_checkpoint、物理 key 或头像字节写入 Export Document 的路径；不得对备注正文做普通关键词误报扫描。
- router 不新增 import/restore/upload 端点；OpenAPI 只有既有 GET /export。
- dataexport 不查询 accounts.password_hash、idempotency_records、reminder_scan_state、Telegram bind/update/delivery 表或日志表。
- 后端不创建 export 临时目录/文件；前端不写 localStorage、sessionStorage、IndexedDB。
- UI 不宣称“完整备份”“可恢复头像”或“从所有备份删除”。

### 3.3 Acceptance Coverage Matrix

| Scenario | Covered By Step | Evidence Type | Command / Action | Core? |
|---|---|---|---|---|
| A1 全量门禁 | STEP-007 | command | CMD-001 / CMD-002 | yes |
| A2/A4/A17 命名 codegen、全实体、历史状态与无 N+1 | STEP-002/003 | codegen + PostgreSQL integration + query evidence | CMD-002 / CMD-003 | yes |
| A3/A16 有效 settings 与空账号 | STEP-003 | integration + parity | CMD-003 | yes |
| A5 账号隔离/认证 | STEP-004 | security integration | CMD-003 | yes |
| A6/A7 reference-only 与敏感字段排除 | STEP-004 | negative JSON scan | CMD-003 + diff review | yes |
| A8 一致快照 | STEP-003/004 | concurrency integration | CMD-003 | yes |
| A18 REPEATABLE READ + READ ONLY 独立属性证据 | STEP-003 | same-session PostgreSQL integration + interface assertion | CMD-003 | yes |
| A9 服务端发送前失败与 headers 后 writer 中断 | STEP-004 | fault + HTTP writer | CMD-003 | yes |
| A10/A11 headers、无写与稳定顺序 | STEP-003/004 | HTTP + integration | CMD-003 | yes |
| A12 前端附件 client 与 body/blob 读取中断 | STEP-005 | Node test + rejected body/blob | CMD-004 | yes |
| A13 下载状态、读取中断与恢复 | STEP-005/006 | frontend test + browser；失败时无 object URL/download | CMD-004 + manual | yes |
| A14 PII/头像文案与可访问性 | STEP-006 | screenshot + manual | 1280px / 375px / keyboard | yes |
| A15 范围守护 | STEP-007 | diff + route | CMD-001 + review | yes |

### 3.4 DoD Contract

| ID | 要求 | 证据 | 阻塞级别 |
|---|---|---|---|
| DOD-DESIGN-001 | design、checklist 与 reference-only owner 决策一致，独立 design review passed | design review | blocking |
| DOD-IMPL-001 | checklist steps 全部完成；read snapshot、导出 read model、HTTP 与 UI 证据落盘 | checklist / implementation evidence | blocking |
| DOD-REVIEW-001 | code review passed；无 unresolved blocking；重点审账号隔离、snapshot、allowlist、错误前置与 Blob 生命周期 | review report | blocking |
| DOD-QA-001 | QA 覆盖 A1–A18、核心命令、真实 PostgreSQL、API headers 与桌面/375px 下载 | QA report / evidence | blocking |
| DOD-ACCEPT-001 | acceptance 核对 reference-only 限制、roadmap 回写、产物清洁度与最终 git diff | acceptance report | blocking |

Validation Commands:

| ID | 命令 | 目的 | 核心性 | 失败处理 |
|---|---|---|---|---|
| CMD-001 | make check | 全仓 build、lint、测试与 codegen drift | core | fix-or-block |
| CMD-002 | make generate && git diff --exit-code -- backend/internal/platform/httpapi/api.gen.go frontend/src/api/schema.d.ts | OpenAPI 双端生成物一致 | core | fix-or-block |
| CMD-003 | cd backend && go test ./internal/platform/store/... ./internal/dataexport/... ./internal/platform/httpapi/... ./cmd/server/... -count=1 -parallel=1 | 账号快照、全量 projection、HTTP、装配与隔离定向验证 | core | fix-or-block |
| CMD-004 | cd frontend && npm run test:data-export && npm run build | Blob/filename/error client、设置页契约与 TS 构建 | core | fix-or-block |

Required Artifacts: design-review、implementation evidence、code review、QA、acceptance、evidence pack、命令输出、API JSON/header 样本、桌面与 375px 截图。

## 4. 与项目级架构文档的关系

- 遵守 ADR-001：每个导出查询由 AccountScope / ReadTxAccountScope 自动限定账号；不新增跨账号管理面。
- 遵守 ADR-002：PostgreSQL 仍是业务数据 system of record；导出快照是只读 MVCC 视图，不是第二存储。
- 遵守 ADR-003：Gin handler 只做 auth、应用调用、OpenAPI 映射与附件 headers；导出编排和 allowlist 不进入 handler。
- 遵守 ADR-004：头像二进制是 durable adjunct；reference-only JSON 只含公开 pointer 引用，不替代 pg_dump + volume + exact-generation manifest 的一致备份。
- 复用 compound：AccountScope fail-loud、OpenAPI tag 切片、跨域聚合 read model。
- owner 的 reference-only 选择已写入本 design 与 roadmap items notes；现有 roadmap §4.6 JSON shape 无需改为媒体包契约。
- 不新增 ADR：dataexport read model 和 read snapshot 是既有模块化单体、AccountScope 与 PostgreSQL 决策下的可回退内部实现；若未来引入异步导出存储、跨版本 import 或媒体便携包，再单独记录结构性决策。
- acceptance 后建议按 cs-docs 补 data-export 摄影师使用指南/API 参考，并按 cs-docs-neat 同步 README/索引；本 feature 的阻塞性 PII 与头像限制先由设置页常驻文案和验收证据保证。
