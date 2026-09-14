---
status: proposed
created: 2026-09-14
scope: skill-storage-and-instruction-foundation
implementation_status: not-started
---

# Skill 资源化与结构化输入底座方案

本方案承接[市场与审核灵感记录](../skill-marketplace-brainstorm.md)，作为 FND-07 里程碑 B 前的底座改造候选。当前任务只编写方案，未实现表、接口或迁移；不代表市场、审核后台或自定义编辑器已经立项。

**2026-09-13 对照当前工作区源码复核一轮，改正 6 处、补全 4 处**：迁移编号不得预留（§4）、对象端口选定并上提（§3、§10）、Skill 版本保留根改为「本期只保证不删除」（§4.5、§9、§11.7）、`limitsVersion` 递增到 `creative-agent-2`（§7.1）、导入回收的执行载体写明为管理 CLI（§9）、示例 ID 改为仓库真实形状（§7.1）；另补充消息 schema_version=2 无需 DDL（§7.2）、catalog 语义切换回归（§11.11）、实施前走 `cs-epic` 边界重确认（§2）。实施仍以边界重确认通过为前提。

## 1. 目标与范围

把 Skill 的运行时权威来源从 Go 内嵌包改为数据库与对象存储，使平台维护内容不必重新发布服务；建立固定版本引用，令选框、斜杠命令和后续媒体输入共用一个有类型的输入协议。

| 本阶段落实 | 后续迭代 |
|---|---|
| Skill 身份、不可变版本、资源清单及数据库目录 | 摄影师自定义编辑器与草稿自动保存 |
| 受信导入、资源校验、版本激活、禁用及固定版本读取 | 市场投稿、审核、发布、安装、付费与分成 |
| text / skill_ref / content_ref 输入协议与解析端口 | 多 Skill 编排、图片/视频模型适配、独立附件 UI |
| 消息与运行引用的保留及权限契约 | 完整审核后台、推荐搜索和市场运营 |
| 平台包导入替换运行时 go:embed | 任意脚本、第三方可执行插件不在本方案内 |

“不在本阶段”是能力不开放，不返回伪造的可用状态。纯文本和固定 Skill 引用可在 B 的真实运行中使用；图片、视频的引用结构先定义，模型不支持时明确拒绝，不承诺当前可以理解媒体。

## 2. 当前事实与设计差异

- 当前 `internal/creativeagent/skills.go` 使用 `go:embed skillpkg`，`SkillRegistry` 在启动时加载；`Catalog` 同时返回 Gateway 模型与 Skill 摘要，工具尚未注册时 Skill 不可用。
- 当前消息采用 `schema_version=1` 与 `body.blocks`；`ContentRefs` 单独写入 `creative_message_content_refs`。
- 当前工作区已补入消息保留根与 Skill 深拷贝修复。本方案继承这两项约束，不把修复再开成重复任务；引用写入的归属/状态校验仍需在新的解析流程补齐。
- [Harness](harness.md) 的“包随二进制发布”将由本方案替代；固定版本、工具交集、运行上限和恢复不换输入继续有效。
- [数据契约](gateway-harness-data.md)与[Agent API](agent-api-events.md)在实施前同步。
- **实施前先走 `cs-epic` 边界重确认（2026-09-13 owner 拍板）。** 换机制本身在 FND-07 的交付原文「模型与 Skill 目录…受控 Skill 加载/摘要/结果卸载」之内，它没有写死 embed 还是数据库；但本方案另外带来新领域包 `creativeskill`、4 张新表、2 个新 REST 端点、消息 body v2，以及 CreateRun 请求契约从 `text + skill_key/version` 改为 `instruction_segments`——这是子项交付定义的变更。永久 Epic 在 `active` 期间冻结，只在 work 游标里承接不够。同一批重确认里一并处理已经欠着的 `.codestable/requirements/creative-canvas-foundation.md:119`（外发权利边界偏差），不再累加第二条待办。
- 本次源码依据为当前工作区。知识图谱 generation 为 2026-09-04，未覆盖 creativeagent 新代码，已经回退读取实际文件；本方案不是新一轮代码审查。

## 3. 模块与权威来源

建议新增 `internal/creativeskill`，拥有 Skill 身份、导入、固定版本、资源和访问策略。`creativeagent` 保留运行编排、消息、输入解析和模型上下文组装，通过窄端口消费 Skill；Gateway 不依赖 Skill 领域。

| 职责 | 权威来源 |
|---|---|
| 作者、名称、slug、可用状态与版本指针 | PostgreSQL |
| 冻结正文与 manifest | PostgreSQL 的版本记录 |
| 参考文件字节 | OSS；本地开发由同一对象端口的 Local 实现提供（端口见下） |
| 路径、大小、类型、SHA-256、对象精确版本 | PostgreSQL 资源清单 |
| 版本 digest | 服务端按规范化内容与排序资源清单计算 |
| 缓存 | 可丢弃副本，按 version_id + digest 缓存 |

不让 PostgreSQL 和 OSS 各保存一份可独立修改的正文。导出包可以包含正文与 manifest，但只是版本的派生归档。媒体/文件读取沿用现有对象适配能力，Skill 文件使用独立服务端命名空间与保留规则；不能把任意 Markdown 假装成图片插入 `creative_blobs`。

**对象端口（2026-09-13 owner 拍板）**：把 `creativemedia.Adapter` 及其 local / oss 实现上提到 platform 层，`creativeskill` 与 `creativemedia` 同为消费方。仓库里另一个端口 `platform/immutablefs.ObjectStore` 的 `Metadata` 没有版本概念、写入收 `[]byte`，无法满足 §4.3 的 `object_version`，因此不选它；`Adapter` 的 `PublishVerified` / `StatVersion` / `OpenVersion` / `DeleteExact(key,version)` 正是受信字节写入与固定版本读取所需的形状。上提只搬接口与实现、不改媒体生命周期语义，`creativemedia` 的调用点随之改 import；这样也不引入 [harness.md](harness.md) 依赖方向图里不存在的领域间依赖边。`immutablefs` 的既有消费方（avatarmedia、planningmedia、planshare）不在本次触及范围。

平台侧的 `Adapter` 有 11 个方法，其中 `InitMultipart` / `AuthorizePart` / `ListParts` / `CompleteMultipart` / `AbortMultipart` 这套分片暂存语义是媒体上传独有的，`creativeskill` 只需要 `PublishVerified` / `StatVersion` / `OpenVersion` / `DeleteExact` 四个。按 Uber 风格由消费方声明窄接口，`creativeskill` 定义自己的 4 方法端口并由平台 `Adapter` 满足，不把整个媒体上传面暴露给 Skill 领域。

## 4. 数据结构：现在落什么、后面加什么

下列为逻辑结构；实施时按仓库迁移风格编写 DDL，所有业务表带 account_id，所有关联由服务校验。

**迁移编号按落地顺序取下一个未用编号，不为后续工作预留。** 当前最新是 `0044_creative_agent_conversations`，本方案先落地即取 `0045`，里程碑 B 顺延 `0046`。`store.migrateUpFS` 用 golang-migrate，`Up()` 只向版本号更高的方向推进：若本方案避让 0045 而取 0046 并应用，`schema_migrations` 停在 46，之后补写的 0045 永远不会被执行。

ID 沿用仓库惯例：四字母前缀 + UUID-36，由迁移的 `CHECK(id ~ '^xxxx_[0-9a-f-]{36}$')` 强制（已用 `ccco_` 会话、`ccms_` 消息、`ccec_` 外发同意）。本方案的新前缀在实施时与既有前缀一并登记，避免重复。

### 4.1 creative_skills

| 字段 | 当前用途与约束 |
|---|---|
| id / account_id | 稳定身份与作者归属；作者账号由服务端确定 |
| origin | platform / account；本阶段只有受信管理入口能创建平台来源 |
| slug | 作者范围内唯一的发现名称；重命名不改变 ID，不作为执行身份 |
| display_name / description | 当前目录展示信息 |
| current_version_id | 当前推荐使用的已冻结版本；必须属于此 Skill 和作者 |
| availability | active / disabled；限制新执行，独立于内容 digest |
| revision / next_version_number | 并发更新检测与锁内版本号分配 |
| created_at / updated_at | 管理时间 |
| marketplace_version_id | **物理预留，可空，本阶段强制为空**；未来只能由市场发布事务维护 |

不增加“reviewing / published”的 Skill 总状态：v1 上架、v2 审核中不能挤在一行状态里。`current_version_id` 服务于作者/平台目录；未来 `marketplace_version_id` 专门服务市场，二者不混用。

### 4.2 creative_skill_versions

| 字段 | 当前用途与约束 |
|---|---|
| id / account_id / skill_id | 不可变版本身份与作者归属 |
| version_number | 正整数，UNIQUE(account_id, skill_id, version_number)，Skill 行锁下分配 |
| schema_version | 版本文档格式，与版本号不同 |
| display_name_snapshot / description_snapshot | 该版的显示事实，历史消息展示不依赖后来名称 |
| instructions / manifest | 完整正文与有 schema 的声明，不允许原地编辑 |
| digest | 服务端计算；包括格式版本、名称快照、正文、manifest 与资源清单 |
| based_on_version_id | 可空；当前导入更新时可记录同一 Skill 的前版，未来草稿据此追踪编辑基线 |
| created_at | 版本冻结时间 |
| execution_status / disabled_at / disabled_reason | active / disabled；这是允许变更的执行控制元信息，不计入内容 digest |

不可变内容字段与可变控制字段的更新端口分开。发布新内容必须增加版本，不能复用旧版本号。导入方提供的 digest 只用于核对，不能被当成计算结果直接入库。

本阶段不新增 draft_id 空列；未来草稿表记录 base_version_id，冻结时再建立草稿到版本的关联。不预填审核结论、审核人或许可证默认值。

### 4.3 creative_skill_version_resources

字段：account_id、skill_version_id、path、mime、byte_size、sha256、storage_driver、bucket、object_key、object_version、created_at。唯一键 `(account_id, skill_version_id, path)`。

path 只允许规范化相对路径；拒绝绝对路径、`..`、重复规范路径与符号链接。正文和 manifest 不是隐藏资源。资源不可覆盖，对象地址是服务端内部字段，API 不返回可复用存储密钥。

初版资源只开放 UTF-8 参考文本，每文件≤64KiB、最多20个；正文和 manifest 各≤64KiB，正文+manifest+全部资源≤256KiB。限制纳入版本化策略；未来图片/视频等资源类型再按媒体能力增加，不能仅凭客户端 mime 放开。

### 4.4 creative_skill_imports

为对象上传与数据库不共事务保留一个有界导入记录：id、account_id、operation_id、request_hash、target_skill_id、expected_skill_revision、state、verified_manifest、staged_objects、result_version_id、expires_at、revision、created_at/updated_at。

state 为 preparing / ready / finalized / failed / expired。`staged_objects` 是有 schema、受上述20文件上限约束的对象清单，只定位该导入持有的字节，不是通用内容引用表。UNIQUE(account_id, operation_id)，同操作不同请求报冲突。

### 4.5 显式引用

- 本阶段新增 `creative_message_skill_refs`：account_id 为阅读消息的账号，message_id、segment_ordinal、skill_id、skill_version_id、digest，以及服务端解析出的 skill_owner_account_id。唯一键 `(account_id,message_id,segment_ordinal)`。
  - **它记录引用事实，本阶段不是被查询的保留根（2026-09-13 owner 拍板）。** 理由是本阶段根本没有删除路径：§9 规定 Skill 版本不做物理删除，没有东西需要被根查询守护，现在建这个查询等于为一个还不存在的 GC 预支设计。这张表是 FND-10 通用 GC 上线时的根来源。
  - 届时的跨账号根查询**不需要 ADR 豁免，也不需要裸的全库查询端口**：平台 Skill 版本归受信发布账号、这张表按阅读账号落行，所以「某版本还有没有人引用」确实跨账号，但仓库已有现成范式——`creativemedia.SweepAllAccounts` 用 `store.ActiveAccountIDs` 枚举账号后逐账号 `ScopeFor`，`llmgateway.SweepExpiredDispatches` 用 `MaintenanceAccountIDs` 游标分页做同一件事，两者都是系统级全库清扫且没有发出一条无账号范围的查询。照这个形状实现并封装在 `creativeskill` 内即可。内容侧仍复用 `creative_message_content_refs` 这个已生效的账号内保留根，不受影响。
- B 新增 `creative_run_skill_refs`：同样固定使用账号、作者定位、版本、digest，并绑定 run；运行快照和消息引用共用固定版本，不能重新解析 current_version_id。
- 内容仍复用 `creative_message_content_refs` 与 B 的 run_inputs；不把 Skill 包和通用内容修订强塞进任意 reference 表。
- 未来独立增加 skill_drafts、skill_review_requests/events、skill_marketplace_releases、skill_installations。审核记录绑定具体版本与 digest，安装记录分别拥有使用账号与授权发布版本。

## 5. 平台 Skill 与账号隔离

本阶段读取范围仅为“自己的 Skill + 受信平台目录”，不开放账号之间的自由分享。

平台 Skill 由服务端配置的受信发布账号持有，仍落带 account_id 的业务表。目录及固定版本读取通过专门的访问解析端口：先验证请求账号有效，再把候选限制在本人和配置中的平台发布者；对平台发布者只读取标记为 platform 的正式冻结版本。发布账号从服务端配置取，客户端不能传 account_id、origin 或任意发布者范围。

每条 SQL 仍通过作者账号范围查询。跨账号平台读取必须在 `creativeskill` 内完成，返回已授权的值对象；不向 Harness 暴露其他账号的 AccountScope，也不把通用“全库查版本”端口给调用方。普通账号不能通过写 origin=platform 进入平台目录。

未来市场通过审核通过且可访问的发布记录建立授权关系；不能把 `visibility=public` 当作审核已经通过。本阶段不新增这个客户端可写字段。

## 6. 导入、激活与读取协议

1. 受信管理命令提交导入意图，保存 operation_id、目标 Skill 和 expected_revision；此时版本不可见。
2. 事务外上传资源到本次导入独占的对象身份，流式校验长度/类型/哈希，记录精确对象版本。重试读取已完成的对象结果，不覆盖旧发布对象。
3. 校验正文、manifest、资源路径、完整包大小和平台工具声明；得到服务端计算的 digest，将导入标记 ready。提交未知时先查同 operation 的记录。
4. 短事务锁导入与 Skill，复核状态、expected_revision 和所有已验证资源；插入冻结版本及资源清单，递增版本号，可选择激活，并写 finalized 与结果回执。对象网络 I/O 不进入该事务。
5. 新版本激活只影响之后的新选择，旧消息/运行/版本链接继续固定旧版。相同导入操作回放相同版本；旧 expected_revision 冲突时保留导入结果供明确重试，不默默覆盖他人更新。
6. 失败或过期导入的对象在保留窗口之后回收。回收与 finalize 争同一导入状态，先标记不可发布再事务外删除；结果未知时查证，不删除已有版本引用的对象。

受信管理入口首先做成内部应用端口与管理 CLI，不为本阶段建设后台页面，也不开放摄影师发布或任意文件上传接口。管理命令同样经过作者范围、幂等和审计；执行时的具体 CLI/OSS 参数留给实现阶段按官方文档核实。

运行加载时，先解析固定版本并建立引用/执行租约，再在事务外获取所需资源并核对哈希。OSS 不可用时有界失败或重试原版本，不换 latest，也不为弥补文件读取失败重发已完成的模型请求。

新建运行时再次验证 Skill/版本未禁用；后续工具或模型派发也复查执行状态。禁用和派发意图通过同一版本控制锁协调：禁用先提交，之后的派发不得通过；已经提交的派发不承诺召回。B 的锁预取顺序需纳入 Skill 控制行，沿用 operation/账号/slot/预算/会话/run/请求在前的约束，禁止在资源锁内反向取高层锁。

缓存只保存不可变版本载荷；权限与禁用状态不由长期缓存裁定。Get 和资源读取返回独立副本，继续保持当前深拷贝修复的不变量。

## 7. 结构化输入契约

### 7.1 请求

```json
{
  "schema_version": 1,
  "instruction_segments": [
    {"type": "skill_ref",
     "skill_id": "ccsk_1f0c9a52-4e7b-4a11-9d3e-6b2c8f5a01d4",
     "skill_version_id": "ccsv_8b41d07e-2c96-4f58-a0b7-13de9c4a77f2"},
    {"type": "text", "text": "参考这张图片和这段视频，整理一个拍摄方案。"},
    {"type": "content_ref", "content_revision_id": "ccrv_3a7e51c8-9d02-4b6f-8e14-5c07b2fa9de3"},
    {"type": "content_ref", "content_revision_id": "ccrv_6d20b849-7f35-4c1a-b9e8-24af013c6b5d"}
  ]
}
```

ID 用的是仓库真实形状（`creativecontent.newID` 产出 `ccrv_<uuid>`），`ccsk_` / `ccsv_` 是本方案建议的新前缀，实施时按 §4 一并登记。这段 JSON 会作为 OpenAPI 的 example，不写占位串。请求的 schema_version 与消息正文 schema_version 是不同文档的版本号。

- 是有序的判别联合，每种 type 只允许自身字段；text.text 是文字，引用使用明确 ID 字段。
- skill_ref 必须同时带 Skill 与版本 ID，服务端验证关联；名称、版本号显示值、digest 和作者均由服务端读取。
- content_ref 指向固定内容修订，类型由内容域返回。节点选择仍带 node_id/data_revision 做来源校验，服务端转换为固定内容引用，保留节点来源快照；裸 ID 不授予越界读取权限。
- 本阶段每条输入最多一个 skill_ref，最多200片段；拒绝未知类型与重复 Skill，不拼接多个 Skill 指令。
- UTF-8 输入文本预算沿用现有限制配置；片段元数据单独计入请求体上限。正式上限从一处限制配置投影到 OpenAPI 与运行校验，不能出现几套不同数字。
- **`limitsVersion` 递增到 `creative-agent-2`。** 现值是 `creativeagent/service.go` 里的冻结常量 `creative-agent-1`，约定是「调大任何一个上限都必须改它，因为明天恢复的 run 要按它创建时的上限来判定」。本方案新增片段数（200）、单次 skill_ref 数（1）、资源文件数（20）与包体上限四个维度，都会参与 run 恢复判定——虽然不是「调大」既有数字，但改变了判定集合，同样必须换版本号，否则跨版本恢复的 run 会按错误的上限集合校验。
- 对话选框与斜杠选择器插入相同 skill_ref；普通 `/文字` 不由后端自动解析成 Skill。名称重名时前端显示作者与版本用于区分。
- 本阶段支持 text、skill_ref 和可读的文字 content_ref。图片/视频示例表达扩展形态；模型链路未实现相应能力时返回明确的 unsupported/capability 错误，不忽略、不擅自抽帧外发。
- B 的 CreateRun 以 instruction_segments 作为指令唯一来源，不再并行接受顶层 text 与 skill_key/version 两份可能冲突的事实。task_scope、模型选择、选区读集、外发同意与预算保持独立字段。
- FND-09 再加入附件草稿引用类型；只能解析同账号同会话 ready 附件，发送时原子转正式消息/run 引用。当前不接受任意 URL 或尚在上传的对象。

### 7.2 消息与运行的投影

输入解析器返回 `ResolvedInstruction`：有序展示片段、已授权且固定的 SkillSnapshot、内容修订与来源清单、实际大小和必要保留信息；不只返回一段拼接文本。

消息正文升级为 schema_version=2，保留现有 body.blocks 外壳，增加明确的 skill_ref、content_ref 块，保存服务端确认的 ID、digest 与显示名快照；既有 text / tool_result / notice 保留。instruction_segments 是提交协议，body.blocks 是持久展示协议，不维护第二份可编辑消息正文。

这一步**不需要 DDL**：0044 对该列的约束是 `schema_version INT NOT NULL CHECK(schema_version>0)`，没有钉死 1，v2 直接可写。

schema_version=1 的已有历史消息仍按原结构读取；新写入用 v2，不把未知历史 reference 猜成 Skill。这仅是持久消息读取约束，不建设旧工作台或功能开关兼容层。若需要迁移历史，必须有明确映射并保留原始事实。

B 的 CreateRun 在一个短事务内完成幂等判定、能力/预算/同意验证、消息、Skill refs、content refs、run_inputs、run 和 job 入队。媒体/资源的大字节读取在事务外，受固定对象身份、引用保留和执行资格约束。JSON 里的引用不代替关系表保留根。

Skill 正文由 Harness 作为受控工作说明装配，媒体、素材文本与工具结果作为待处理数据装配。引用 Skill 不授予更多工具；工具范围始终取 Skill 声明、平台注册、账号权限及本次 task_scope 的交集。completion_check_key 只能选择注册检查器，不能使包自带代码获得执行权。

本方案不改变已经讨论的外发权利边界。外发同意仍必须覆盖实际请求涉及的数据类别与来源；同意不覆盖新输入时明确拒绝或走后续既定补充流程，不能因为资源来自 Skill 就绕开外发控制。

## 8. 目录与内部端口

建议端口按职责划分，避免每张表各建一层 service/repository：

- `ListAccessibleSkills(viewer, query, cursor, limit)`：本人及平台可发现摘要，返回 Skill/版本 ID、名称、slug、digest、可用性及原因。
- `ResolveVersion(viewer, skillID, versionID)`：授权并读取固定不可变版本，返回值对象和保留所需身份，不取 latest。
- `ReadResource(authorizedVersion, path)`：有界读取声明资源并核验，不接受请求方指定存储路径。
- `BeginImport / FinalizeImport / ActivateVersion / DisableVersion`：受信管理写端口，使用正式幂等与 revision。
- `ResolveInstruction`：位于 creativeagent，消费 Skill 和内容端口，拒绝不支持的片段或能力。

新增 `GET /creative/agent/skills?q=&cursor=&limit=` 供选框与斜杠统一查询，默认20、上限50；游标绑定账号、查询与稳定排序键。第一阶段按 slug 前缀和展示名搜索，用 PostgreSQL，不引入搜索集群。新 `GET /creative/agent/skills/{id}/versions/{version_id}` 只返回获准的版本摘要与声明，不直接公开对象密钥。

现有 `GET /creative/agent/catalog` 保留模型/工具/限制与有界默认 Skill 摘要，摘要复用上述目录端口；独立提供 skill_catalog_revision，不复用 Gateway catalog_version。暂不注册工具的 Skill 继续 available=false，资源已导入不等于执行工具已经交付。

私有/受信目录使用 Cache-Control:no-store；前端可以在当前账号与编辑会话内短暂复用搜索结果，发送时服务端重新验证。未来新增市场目录端点，不把当前账号私有目录直接变成公共 API。

## 9. 保留、禁用与异常恢复

- **正式版本在本阶段不做物理删除，这就是 Skill 版本的全部保留机制。** 消息/run 引用只做显式登记，供 FND-10 通用 GC 上线时作为根来源（跨账号查询的封装要求见 §4.5）。删除界面入口或停用不清除历史版本。
- Skill 包资源由版本保留；通用图片/视频/文字继续由内容域保留。版本资源对象不能交给媒体上传过期任务扫描。
- 新增内容引用先在正确账号范围解析真实 ready 修订，并在同事务写根；不能通过插入一条悬空 ref 使无效 ID 变为可读。
- 只剩消息根时仍可读；没有合法根的内容仍拒绝。媒体实际读取继续检查当前使用资格。
- 对象损坏、丢失或 digest 不一致时标记明确故障；不以当前新版或其他路径替代。检查上传/发布回执后恢复原对象身份或停止执行。
- 版本与资源读取有字节/时间上限；加载失败发生在供应商派发之前。已完成的模型结果仍按原 Gateway 消费和幂等机制恢复。
- 导入清理与备份恢复窗口一起设计；首版只回收明确未发布、已过期的导入对象，正式版本保留。未来市场下架、安装与作者删除规则见 brainstorm，不在这里提前实现。
- **回收的执行载体是管理 CLI 的一个显式子命令，本阶段不建 River 定时 job。** §10 已经定了本阶段只做内部端口与管理 CLI，没有后台页面，那么 `expires_at` 到期不会有任何东西自动触发：过期导入的对象一直留在 OSS，直到有人跑一次回收命令。这是首版可接受的代价（受信入口、导入量极低、上限 20 文件），但必须写明，不能让 `expires_at` 这个字段看起来像是有人在扫。§6 第 6 步描述的「先标记不可发布再事务外删除」的竞争顺序在 CLI 路径上同样执行。真正的定时回收随 FND-10 的通用到期回收一起上。

## 10. 实施顺序与影响面

| 步骤 | 交付与验收 |
|---|---|
| S0 端口上提 | `creativemedia.Adapter` 与 local/oss 实现移到 platform，creativemedia 现有用例原样通过 |
| S1 版本存储 | 新领域与迁移 `0045`，账号隔离、版本不可变、引用结构、导入幂等 |
| S2 资源与平台包导入 | 上提后的对象端口、独占对象与哈希校验、受信导入/激活；把现有 reference-direction@1 作为一次性种子导入 |
| S3 目录与输入契约 | 统一搜索、固定版本解析、消息 v2、显式引用；OpenAPI 生成 Go/TS 两份类型 |
| B 接入 | Runner 固定 SkillSnapshot、CreateRun 原子保存 refs/输入、恢复与禁用检查；沿用 Gateway |
| C 接入 | v5 Agent 面板中选框与斜杠共用引用标签；普通保存继续静默 |

种子文件可以留仓库作可审计导入素材，运行时不再使用 go:embed fallback。先部署表、配置受信发布账号并完成幂等导入，再切换目录读取；未导入时清楚返回无可用 Skill，普通对话按其独立能力工作。API 与 worker 使用同一数据库目录来源；不做双写和两套包身份。

必须修改：creativeagent 的 registry 依赖、目录/正文类型、输入解析；新增 creativeskill；把 `creativemedia.Adapter` 及 local/oss 实现上提到 platform 并改 creativemedia 的 import；服务组合根；迁移与新引用表；API 和生成物；Harness/数据/API/dev 文档及 work 游标。

需要验证：creativecontent 读取与保留、上提后的对象适配（creativemedia 现有用例必须原样通过，这是上提没有改变语义的证据）、现有会话 API、Gateway 目录边界、B 的锁序/恢复、C 的 v5 交互。Gateway 计费与供应商适配本身无需为 Skill 改写。

仍待实施核实：实际 OSS 私有前缀与权限、平台发布账号的部署配置、开发库已应用到哪个迁移版本（0044 目前只在测试容器跑过）。没有这些事实前不写运行命令或声称已完成部署。

## 11. 验收与验证计划

1. 同导入操作重复调用只产生一个版本；不同请求 hash 冲突；并发更新同 expected_revision 只有一个激活成功。
2. 新版激活后，旧消息/旧运行仍得到原正文、资源、工具声明和 digest；调用方修改返回的 slice/map 不改变存储或缓存。
3. 本人可读取私有版本，另一账号不能读取或猜出资源；平台版本只经受信目录读取，伪造 origin/作者字段无效。
4. 资源哈希、路径、大小不合法时版本不激活；OSS 成功而事务失败后可重放；清理与 finalize 竞争不能删除已发布资源。
5. Skill 选框与斜杠产生同一个请求结构；名称重名、改名和新版发布均不会改变已选版本。
6. 多 Skill、未知片段、版本归属不匹配、跨账号内容、未 ready 附件、不支持的媒体输入均明确失败，不隐式降级或扩大授权。
7. 消息的 content refs 与 skill refs 和正文同事务写入；移除资产根后历史引用内容可读，无根对照仍拒绝。Skill 版本一侧只验证「不删除」这一事实与引用行落库正确，不验证根查询——本阶段没有这个查询（§4.5）。
8. 禁用先提交则之后不能派发；缓存命中不能绕过禁用；恢复不重新取 current_version_id，不重复付费调用。
9. 目录查询有界且不读取 OSS 文件；输入计量涵盖 Skill 正文与实际加载资源，沿用统一 limits_version。
10. 迁移升级、幂等种子导入、空库回滚和有版本/历史时拒绝有损回滚均有证据。
11. catalog 语义切换有回归：切换前未注册工具时 `reference-direction@1` 以 `available:false` 加原因出现；切换后未完成种子导入则 `skills` 为空数组，完成导入后恢复原有的「可见但不可用 + 原因」表现。两种状态都不得让调用方以为部署坏了。

实施时 Go 跨包/迁移运行 make check-go；OpenAPI 变更运行 make generate 与 make generate-check；C 的前端变更运行 make check-frontend 并做浏览器交互验收。当前仅文档变更检查链接、示例 JSON 与 diff，不重复跑业务测试或追加代码复审。
