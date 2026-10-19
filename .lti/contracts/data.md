# 数据与存储契约

记录已交付的数据模型、访问语义与演进约定，适用于表、文档集合、分析数据集、搜索索引、向量集合、图及其他存储形态。
Epic 在自己的 contracts.md 定义目标数据契约，子 Change 引用批准版本；独立 Change 自行提 delta，归档后合入本文件。

起草要求（文档责任，不代表 CLI 已校验数据库配置或真实数据）：
- **必填**：业务用途、实际存储形态、逻辑/物理对象标识、主数据或派生数据角色、结构与身份、读写所有权、关键访问模式、约束与一致性、生命周期和演进方式。
- **按场景必填**：选择下方与实际用途匹配的设计项；一个对象可组合多类，产品名不决定唯一分类。未采用的机制说明原因或准确引用公共约定，不为填表新增存储、索引或中间件。
- **可选**：ER 图、文档形状图、属性图、数据流图、容量分析与调优实验；选择能解释模型的表达方式，不能用一张图替代文字语义。已有容量/延迟目标会影响设计时须给出依据，未知值标为待验证。
- 每个可独立交付的对象或紧密关联模型使用稳定的二级标题，内部用三级及更低标题。跨存储对象的名称应能区分，如“订单主记录”“订单搜索投影”，不要都叫“订单表”。
- 可直接引用仓库中的 DDL、JSON Schema、mapping、collection schema、图模型定义等权威文件，写准确路径/符号/版本和语义补充，不维护两份相互漂移的正文。模板不强制 SQL、ER 图、外键、固定字段或固定维度适用于所有存储。
- 不得只写“schema-less”“JSON”“vector”“节点关系”“最终一致”或 TODO；未知项标明问题、影响对象/子项与解决时点，不虚构默认值和产品能力。约束必须区分业务要求、数据库保证和应用/同步程序保证。

<!-- 以下为数据契约骨架；按实际模型填写，删除无关分支，不把模板注释作为已完成设计。
项目正文标题：## <稳定的数据对象或模型名称>
Epic/Change delta 标题：## data: ADD <名称>（修改为 MODIFY，删除为 REMOVE）
MODIFY 给出完整新章节。REMOVE 使用退役说明：原对象、原因、消费者、停写/停读顺序、保留或清理安排、替代/恢复路径；不用重写已删除模型的全部设计。

## <稳定的数据对象或模型名称>

### 用途与存储角色【必填】
- 业务用途与边界：存什么、不存什么，面向哪些读写场景。
- 存储：实际产品/引擎与相关版本或项目选型引用；说明采用哪些模型（可多选）。未定选型显式列为待决。
- 对象标识：库/命名空间、表/集合/索引/图等逻辑及物理名称，或名称生成规则。
- 角色：权威主数据、派生投影、缓存或临时数据；哪个来源决定冲突时的正确值。不能因选用了搜索或向量产品就自动认定它是派生数据。
- 所有权：负责维护的模块/服务，合法写入者与读取者；多个写入者时明确协调与冲突解决。

### 结构与身份【必填，表达方式随模型选择】
先定义业务粒度：一行/文档/事件/向量/节点/关系分别代表什么，身份如何生成、在哪个范围唯一、如何关联来源。
固定字段或属性可用下表；文档/图/向量也可用原生结构定义配合语义说明，不必伪装成 SQL 表。

| 字段/属性/路径 | 类型与表示 | 必须存在 | 缺失/null/默认行为 | 业务含义与约束 |
|---|---|---|---|---|
| <实际名称> | <具体类型或 schema 引用> | <是/条件/否> | <明确区别> | <单位、精度、枚举、范围等> |

嵌套对象和数组定义元素形状或引用准确 schema。动态字段说明允许范围、命名/类型规则与校验责任，不能以“无固定 schema”为由省略结构约定。
区分业务身份、存储主键、分区/路由键、排序键；不要假定名为 primary key 的结构在所有产品中都保证唯一。

### 访问模式与保证【必填】
列出关键写入/更新/删除/查询/聚合/检索/遍历，给代表性输入与预期结果语义；只列实际使用的操作。
说明过滤、排序、关联或遍历边界；按访问模式解释索引/布局选择，选择不建索引也可写理由。
写清业务不变量及其执行者、一致性与写后可见性、失败后的可观察状态；涉及事务、并发或重复处理时写边界和处理规则。
有租户、敏感数据或权限隔离要求时，说明隔离键/过滤位置与访问边界；没有则说明适用范围。

### 按模型补充【只保留适用项】

| 实际模型/用途 | 应说明的设计决定 | 可采用的表达 |
|---|---|---|
| 关系型（如 MySQL） | 列类型/长度/精度、主键、唯一与检查约束、关系基数、删除/更新规则；外键由数据库还是应用保证；索引列顺序与查询依据、事务边界 | 字段表、DDL 或其引用；ER 图按需 |
| 文档（如 MongoDB） | 文档边界、嵌套/数组、嵌入还是引用及理由、动态字段/校验规则、文档增长边界、读写原子范围；索引和分片键按实际部署说明 | 代表性文档 + schema/校验规则；文档关系示意按需 |
| 分析/列式（如 ClickHouse） | 事实粒度、维度/指标含义、事件时间/时区；引擎、排序/主键与分区各自用途；写入批次、重复/迟到/更正数据语义、保留策略；有聚合视图时说明更新和回填 | 列定义/DDL、查询样例、聚合与数据流说明 |
| 搜索（如 Elasticsearch） | 文档 ID/来源、mapping、参与检索字段的分词或精确匹配语义、嵌套对象处理、过滤/排序/聚合字段；写后可见性；采用路由/别名时说明规则，mapping 演进和重建切换 | mapping 引用 + 样例文档/查询；全文字段说明 analyzer 的选择 |
| 向量（如 Milvus） | 对象/片段 ID、原始内容引用、向量来源及模型/算法版本、预处理/切分规则；稠密/稀疏等表示、维度规则（仅适用时固定）、相似度度量、索引选择依据、元数据过滤；更新/删除传播和模型变更后的重算切换 | collection schema、元数据样例、向量形状与检索条件；不用贴整段高维浮点数 |
| 图（如 Neo4j） | 节点标签/类型、关系类型/方向/两端、节点与关系属性、业务身份、基数与去重规则；约束由谁保证；关键遍历的起终点、深度/路径边界、节点删除后的关系处理 | 节点/关系定义表、属性图、代表性遍历；不要求改写成关系表 ER 图 |
| 其他或混合模型 | 用该存储的原生概念补充身份/布局、访问方式、约束、可见性和演进；如键值/宽列/时序需说明键组织、分区或时间窗口等实际决定 | 原生定义和操作样例；按用途组合上列分支，不强行套某一产品 |

这些是设计问题，不是要求产品必须支持的功能。具体能力、约束是否可强制、语法及默认值须按选用版本核实；不支持的保证要说明由谁实现或接受什么限制。

### 跨存储同步与派生数据【存在派生/复制关系时必填】
- 来源及关联：准确引用权威对象/字段、主数据 ID 与派生 ID 的映射；不复制上游完整定义。
- 生成链路：谁通过事件/CDC/任务等方式写入，转换规则及版本，更新与删除如何传播。
- 一致性：允许的延迟或待确认目标，重复/乱序/漏事件/部分失败如何处理，如何发现与修复偏差。
- 重建：来源是否足以重建，重放/回填范围及检查方式，新旧投影切换与失败恢复；不能重建时写保护与恢复安排。
共享链路可写在项目架构/模式或 Epic design 并准确引用；各数据契约只补本对象差异。

### 生命周期与演进【必填】
- 保留、过期、删除或归档规则；不自动删除也须说明，不为所有模型强制增加软删除字段。
- 本次新增/兼容调整/破坏性变更及受影响消费者。首次建模且无存量数据可明确写无需迁移。
- 有存量变更时说明适用的迁移/回填/重建/重嵌入步骤、双版本共存、切换顺序、完整性检查与恢复方式；具体执行脚本由 Change 实现。
- 回滚、前向修复或重建可恢复到什么状态；不可逆操作如实说明，不能用“可回滚”代替数据恢复方案。

### 表达与样例自查【必填】
至少给一个与所选模型一致的有效实例/结构定义引用和一个关键访问样例（含预期语义）。选用的表、图、schema、查询中的名称与类型必须一致。
关系型可用记录与查询，文档用 JSON 与访问路径，分析用事件/指标与聚合，搜索用文档与检索，向量用片段元数据与向量形状/过滤，图用节点关系与遍历。
可以引用可读的已有示例，不要求每类都写一套，不强制执行样例或提供真实生产数据。

-->

## 通用文档

### 用途与存储角色
- **用途**：画布文档的头、修订历史、引用登记和工作会话指针。类型载荷存放在内容修订里（`creative_content_revisions`），不在这些表中。
- **存储**：PostgreSQL。扩展 `creative_documents`，新增 `creative_document_revisions`、`creative_document_content_refs`、`creative_document_conversations`，迁移 `0072_creative_document_foundation`（合入 develop 时与 `0069`–`0071` 的生成路由迁移撞号，改号为 0072）。
- **角色**：
  - 文档行：权威主数据（当前修订、标题、单元修订号、回收站状态）。
  - 修订表：历史的唯一顺序来源。
  - 引用表：GC 持有登记。
  - 会话表：指针，不参与版本。
- **所有权**：只有 `creativedocument` 写入；`creativereference` 只读修订表和引用表来判定持有。

### 结构与身份

`creative_documents`（扩展，原有列不变）：

| 列 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `created_at` / `updated_at` | timestamptz | 是 | `clock_timestamp()`；每次写命令、进出回收站都更新 `updated_at` |
| `trashed_at` | timestamptz | 否 | null 表示不在回收站 |
| `unit_revs` | jsonb | 是 | 对象，键为单元名，值为正整数修订号；必须含 `meta`；键 ≤ 4096；CHECK `jsonb_typeof='object'` |

索引：`(account_id, type_key, updated_at DESC, id DESC)`、`(account_id, updated_at DESC, id DESC)`。

`creative_document_revisions`：

| 列 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `account_id`, `document_id`, `revision` | text, text, bigint > 0 | 是 | 主键 `(account_id, document_id, revision)` |
| `content_revision_id` | text | 是 | 该修订的快照；GC 持有方 |
| `title` | text ≤ 200 | 是 | 该修订时的标题，供整份恢复 |
| `actor_kind` | text | 是 | `photographer` / `agent` / `system`（CHECK） |
| `run_id` | text | 否 | `actor_kind = 'agent'` 时必填（CHECK） |
| `restored_from` | bigint | 否 | 由恢复产生时为来源修订号 |
| `changed_units` | text[] | 是 | 本修订推进了修订号的单元；整份恢复记为 `{'*'}`；新建记为 `{'*'}` |
| `annotated_units` | text[] | 是 | 本修订只做标注、未推进修订号的单元（如确认），缺省 `{}` |
| `removed_units` | text[] | 是 | 本修订删除的单元，缺省 `{}`；整份恢复记为空，由 `changed_units = {'*'}` 表示全部重置（实施审计补充） |
| `unit_revs` | jsonb | 是 | 本修订写入**之后**的完整单元修订号映射（与当时的文档头一致）；供旧版本读取、恢复钩子判断「当时是否已确认」等使用 |
| `created_at` | timestamptz | 是 | — |

索引：`(account_id, document_id, run_id, revision DESC)`；`(account_id, content_revision_id)`（GC 按快照判定持有时用）。

`creative_document_content_refs`：主键 `(account_id, document_id, revision, content_revision_id)`，索引 `(account_id, content_revision_id)`。每个修订登记一次它快照引用的全部内容修订。

`creative_document_conversations`：主键 `(account_id, document_id, canvas_id)`，列 `conversation_id text not null`、`updated_at`。

### 访问模式与保证
- **写命令**：锁文档行 → 版本检查 → 写内容修订、修订行、引用行 → 更新文档头，在一个事务里完成（design「一次命令的处理顺序」）。
- **读当前**：按 ID 读文档行和当前快照。
- **列表**：按 `(updated_at, id)` 倒序加过滤。
- **历史**：按 `revision` 倒序，可按 `run_id` 过滤。
- **保证方**：
  - 由数据库保证：修订号主键唯一、写入者 CHECK、`agent` 必带 `run_id`、`unit_revs` 为对象。
  - 由应用保证：每个修订行的 `unit_revs` 等于该修订提交时文档头的 `unit_revs`。
  - 由应用保证：修订号连续递增、单元修订号规则（D-I2）、回收站只读、文档行和最新修订行一致（D-I1）。

### 生命周期与演进
- **回填**：迁移为每份存量文档补一行修订（`actor_kind='system'`，`changed_units='{*}'`，标题和快照取当前值），并把文档头与这一行的 `unit_revs` 设为 `{"meta": revision}`。`internal.document` 再补 `body`，与 `meta` 相同。
- **彻底删除**：删除会话指针、引用行、修订行和文档行。快照内容修订（`text`、`plan`、`scene` 等非媒体类型）不在现有媒体 GC 的回收范围内（`backend/internal/creativemedia/gc.go` 的 `unheldMediaRevision` 只处理 `image` / `video` / `audio`），首版保留为无入口数据，**不承诺物理回收**。被快照引用的媒体在失去全部持有方后，照常由媒体 GC 回收。
- **回滚**：存在 `system` 以外写入者的修订行，或存在 `trashed_at` 时拒绝回滚，前向修复。
- **GC**：`LiveOwnerCondition` 增加修订表的 `content_revision_id` 和引用表两行，只扩大持有范围。

## 场景文档

### 用途与存储角色
- **用途**：保存 3D 场景的编排，包括对象树、变换、环境、渲染设置和单元版本。不保存几何（几何在模型内容里）、GLB 字节、生成代码和导出图。
- **存储**：PostgreSQL。
  - 通用文档层：`creative_documents` 行，`type_key = 'scene'`。
  - 修订：`creative_document_revisions`。
  - 引用：`creative_document_content_refs`。
  - 每个修订的全文快照：一条 `creative_content_revisions`，内容类型 `scene`。
  - 通用文档的表（文档行、修订、引用、工作会话）由通用文档底座 change 交付，本条目只定义 `scene` 类型的载荷、单元和引用登记。
- **角色**：权威主数据。`creative_documents.content_revision_id` 指向当前快照；修订表是历史的唯一顺序来源。
- **所有权**：只有 `creativescene` 写入，摄影师和助手都经场景命令；画布命令层只在新建 `core.scene` 节点时同事务创建文档。读取方是场景接口、画布节点视图和助手工具。

### 结构与身份
- 文档 ID 形如 `cwdoc_<uuid>`，账号内唯一。对象 ID 形如 `sobj_<21位 nanoid>`，由客户端生成，场景内唯一，删除后不复用。不复用由客户端随机生成来保证；服务端只拒绝与当前快照重复的 ID，不查历史。
- 快照载荷（`creative_content_revisions.payload`，`schema_version = 1`，≤ 512 KiB）：

| 路径 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `scene.environment.sky.kind` | `"procedural"` \| `"hdri"` \| `"color"` | 是 | 新建场景为 `procedural` |
| `scene.environment.sky.sun_elevation_deg` | number −10…90 | `procedural` 时 | 新建为 35 |
| `scene.environment.sky.sun_azimuth_deg` | number 0…360 | `procedural` 时 | 新建为 135 |
| `scene.environment.sky.turbidity` | number 1…20 | `procedural` 时 | 新建为 3 |
| `scene.environment.sky.hdri` | string，内置 HDRI ID（`^[a-z0-9_]+@[1-9][0-9]*$`） | `hdri` 时 | — |
| `scene.environment.sky.color` | `#rrggbb` | `color` 时 | — |
| `scene.environment.ambient` | `{color:#rrggbb, intensity:0…10}` | 是 | `{#ffffff, 0.4}` |
| `scene.environment.fog` | `{color:#rrggbb, near_m>0, far_m>near_m}` 或 null | 是 | null |
| `scene.render.mode` | `"clay"` \| `"lit"` | 是 | `clay`；只影响编辑器默认显示，导出时另行指定 |
| `scene.objects[]` | 对象数组，≤ 2000 | 是 | 新建为一个内置地面 |
| `objects[].id` | string | 是 | 见身份规则 |
| `objects[].kind` | `"primitive"` \| `"model_instance"` \| `"light"` \| `"camera"` \| `"group"` | 是 | `model_instance` 自 C2 起可写入 |
| `objects[].name` | string ≤ 80 字符 | 是 | 可为空串 |
| `objects[].parent_id` | string 或 null | 是 | null 表示根；父对象必须是 `group`；不能成环 |
| `objects[].order` | integer ≥ 0 | 是 | 同一父对象下的兄弟顺序，兄弟之间不重复 |
| `objects[].transform` | `{position:[3], rotation:[4]（单位四元数 x,y,z,w）, scale:[3]（每项 0.001…1000）}` | 是 | 数值有限；位置每轴 ±100000 m |
| `objects[].visible` / `locked` | boolean | 是 | `true` / `false` |
| `objects[].primitive` | `{builtin:"<name>@<ver>", params:{…}}` | `kind=primitive` 时 | `builtin` 必须在服务端登记的内置清单里（C1 交付清单）；`params` 外形同模型参数值 |
| `objects[].model` | `{content_revision_id}` | `kind=model_instance` 时 | 必须是本账号 `model` 类型、`ready` 且可展示的内容修订 |
| `objects[].material_slots` | `{<slot_id>:{preset, params}}` | 否 | 缺省 `{}`；`preset` 匹配 `^[a-z0-9_]+@[1-9][0-9]*$`；`params` 是平铺对象，≤ 32 键，值为 number/string(≤80)/boolean |
| `objects[].light` | `{type:"directional"\|"point"\|"spot", color, intensity:0…1000, range_m:null\|>0, angle_deg:null\|1…179, penumbra:null\|0…1, cast_shadow:boolean}` | `kind=light` 时 | `spot` 时 `angle_deg` 必填 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near_m>0, far_m>near_m, label≤40}` | `kind=camera` 时 | 新建为 `{35,[36,24],"3:2",0.1,1000,""}` |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型 `content_revision_id`。

### 访问模式与保证
- **读当前**：按文档 ID 读文档行和当前快照；同时批量读引用的模型修订摘要（manifest、生成参数、名称提示，不含代码和地址）。GLB 和缩略图由编辑器按模型修订另取媒体读取票据（C2 修改）。
- **写**：在文档行锁内（`SELECT … FOR UPDATE`）检查单元版本 → 应用 → 校验全部不变量 → 写新快照、修订行（`actor_kind`、`run_id`、`changed_units`、`restored_from`）和引用行 → 更新文档行的 `revision` 和 `content_revision_id`。整个过程在一个事务里，失败则不产生任何修订。
- **不变量的保证方**：
  - 由应用保证：对象 ID 唯一、父子无环、`order` 不重复、引用的模型可用、单元版本单调。
  - 由数据库保证：修订号主键唯一、`account_id` 限定、内容类型 CHECK。
- **历史**：按 `revision DESC` 分页列出修订；`changed_units` 直接读，不比对快照。

### 生命周期与演进
- 进回收站与彻底删除沿用通用文档层。彻底删除时删除修订行和引用行，之后模型修订失去持有方，可被 GC 回收。
- 内容类型 CHECK 加 `scene`。回滚时如已有 `scene` 内容则拒绝，前向修复。
- 快照 `schema_version` 升级时，读取端兼容旧版本，写入只写新版本；不做批量回填。

## 场景内置基础体

### 用途与存储角色
- **用途**：服务端校验 `objects[].primitive` 的依据，也是前端几何生成和参数面板的参数来源。
- **存储**：代码仓库中的一份 JSON，`backend/internal/creativescene/builtins.json`，经 Go `embed` 读入。不进数据库。
- **派生**：`make generate` 原样复制到 `frontend/src/creative-scene/builtins.gen.json`，`make generate-check` 比对漂移。前端不得手写另一份。

### 结构与身份
- 每项的身份是 `<name>@<version>`。版本一经发布，参数和几何语义就不再改变；需要改动时发布新版本（如 `box@2`），旧版本继续可读、可渲染。
- 每项字段：`builtin`、`label`、`mount`（`floor` / `wall` / `ceiling` / `free`）、`params`。`params` 的格式同 `patterns: 几何模块契约` 的参数定义：`number` 必带 `default`、`min`、`max`，可带 `step`、`unit`；`integer` 同 `number`；还有 `boolean`、`enum`、`color`；每项可带 `label`。
- 首批清单（单位为米，Y 轴向上，正面朝 +Z）：

| builtin | mount | 参数（默认值，范围） |
|---|---|---|
| `ground@1` | floor | `size`（40，1…1000） |
| `box@1` | floor | `width`、`height`、`depth`（各 1，0.01…1000） |
| `cylinder@1` | floor | `radius_top`（0.5，0…500）、`radius_bottom`（0.5，0.001…500）、`height`（1，0.01…1000）、`radial_segments`（integer 32，3…128） |
| `sphere@1` | floor | `radius`（0.5，0.01…500）、`segments`（integer 32，8…128） |
| `plane@1` | wall | `width`、`height`（各 2，0.01…1000） |
| `stairs@1` | floor | `width`（1.2，0.1…100）、`step_count`（integer 8，1…100）、`step_height`（0.18，0.01…2）、`step_depth`（0.28，0.01…5） |
| `arch@1` | floor | `width`（2，0.2…100）、`height`（3，0.2…100）、`depth`（0.5，0.05…20）、`opening_width`（1.2，0.1…100）、`opening_height`（2.2，0.1…100） |

### 访问模式与保证
- **服务端校验**：`builtin` 必须在清单中；`params` 的键必须属于该项，类型和单项范围必须符合；缺省的键在写入时补成默认值，所以快照里的参数总是完整的。跨参数约束（如门洞开口不超过外框）不由服务端校验，由几何代码收缩处理。
- **前端**：每个清单项都有一个几何函数。前端测试断言清单与几何函数一一对应。

### 生命周期与演进
- 新增基础体或新版本只改这份 JSON 和前端几何代码，不需要迁移。
- 删除或修改已发布版本属于破坏性变更，不允许。

## 模型内容

### 用途与存储角色
- **用途**：可复用的几何物件，由场景实例和资产库资产共同引用。
- **存储**：
  - `creative_contents.kind = 'model'`。
  - 每个版本一条 `creative_content_revisions`（`schema_version = 1`）。
  - GLB 存为 blob，经 `creative_content_objects` 以 `role='original'` 绑定；缩略图 PNG 以 `role='thumbnail'` 绑定。
  - 上传走 `creative_uploads`，`declared_kind` 加 `model`，校验格式加 `model/gltf-binary`（`.glb`），上限 16 MiB，只允许暂存目标。
  - **两种形态**：
    - 暂存形态：worker 校验 GLB 通过后写入，载荷只有 `{format, manifest}`，manifest 由服务端从 GLB 算出；由暂存候选持有，只供 `commit_model` 消费，不能被场景引用，也不能被采用到资产或画布节点。候选被消费后（`adopted_target_kind = 'document'`）失去持有方，按 GC 回收。
    - 完整形态：`commit_model` 写入，载荷为下表结构；GLB 与缩略图对象复用暂存修订的 blob，不复制字节。
  - **写入口**：`model` 不能经通用写入口（`creativecontent.Validate` / `WriteAndRetain`）写入，资产、画布 `replace_content`、生成输出都写不进来。暂存形态只由 `WriteMediaAndRetain`（worker）写，完整形态只由 `WriteModelAndRetain`（`commit_model`）写。
- **角色**：权威主数据，内容修订不可变。
- **所有权**：由 `creativescene` 的 `commit_model` 写入（消费暂存上传）；`creativelibrary` 存入资产库时只引用同一修订，不复制字节（复制语义由「不可变 + 编辑时分叉」实现，见 I4、I7）。

### 结构与身份

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `model.format` | `"glb"` | 是 | — |
| `model.generator.language` | `"three-module"` | 是 | 外部导入（后续）将使用其他值 |
| `model.generator.contract_version` | integer | 是 | 当前为 1 |
| `model.generator.lib_version` | integer | 是 | 当前为 1 |
| `model.generator.code` | string | 是 | ≤ 64 KiB，UTF-8 |
| `model.generator.code_sha256` | `sha256-<64 hex>` | 是 | 服务端重算并核对 |
| `model.generator.params_schema` | object | 是 | 按几何模块契约的参数定义 |
| `model.generator.params` | object | 是 | 每个键都在 schema 中，值在范围内 |
| `model.generator.mount` | `floor`/`wall`/`ceiling`/`free` | 是 | — |
| `model.manifest` | 见几何模块契约「产物」 | 是 | `triangles` ≤ 300000；`slots` ≤ 32；`parts` ≤ 200 |
| `model.name_hint` | string ≤ 80 | 否 | 助手或摄影师起的名字 |

### 访问模式与保证
- **接受的 GLB 子集**（worker 的 verify 阶段用 Go 实现，编辑器宿主用同一算法预检；不执行代码，文件里的任何声明都不直接采信；不在子集内的一律拒绝）：
  - 容器：magic 为 `glTF`，version 为 2，总长度与实际一致；JSON 块加至多一个 BIN 块，4 字节对齐。
  - 缓冲：至多一个 `buffer`，没有 `uri`。
  - 场景树：恰好一个 `scene`；节点下标全部有效，每个节点至多一个父节点，无环；变换只用 TRS 或 `matrix` 之一，数值有限。
  - 网格：primitive 的 `mode` 为 4；属性只允许 `POSITION`（`FLOAT VEC3`，必有）、`NORMAL`、`TANGENT`、`TEXCOORD_0/1`、`COLOR_0`；不允许 `targets`、`JOINTS_*`、`WEIGHTS_*`。
  - 访问器：不用 `sparse`；按组件对齐；`byteStride` ≥ 元素大小；全部数据落在 `bufferView` 和 BIN 内；索引值小于顶点数；索引数或非索引顶点数是 3 的倍数。
  - 不允许 `images`、`textures`、`samplers`、`skins`、`animations`、`cameras`；`extensionsRequired` 为空，`extensionsUsed` 只允许 `KHR_materials_emissive_strength`。
  - 预算：节点 ≤ 10000，遍历到的 primitive 实例 ≤ 10000，三角形 ≤ 300000，字节 ≤ 16 MiB；读取的 POSITION 顶点总数（每个访问器计一次）≤ BIN 块字节数 / 12。访问器可以指向同一段数据，少了这一项，各项计数都在预算内的文件也能让校验读上数十亿个顶点；访问器互不重叠的文件不会超出。
  - 不合规时上传以 `failed` 结束，失败码沿用现有的 `media_unsupported`。
- **manifest 的算法**：三角形数按场景树遍历累加；每个 primitive 的局部范围**从 POSITION 实际数值算出**，并与访问器声明的 `min` / `max` 逐分量核对（必须存在且相等，否则拒绝）；包围盒取局部范围的 8 个角点经节点世界矩阵变换后合并（与 three.js `Box3.setFromObject` 非精确模式一致）；世界矩阵、包围盒与挂点坐标必须是有限值，各节点数值有限但相乘溢出时拒绝；槽位取材质名 `slot:<id>` 与 `extras.hint`，部件取节点名 `part:<id>`，挂点取节点名 `anchor:<id>` 及其世界坐标，名称取 JSON 原文；槽位 ≤ 32、部件 ≤ 200、挂点 ≤ 200。
- **提交时**：manifest 必须与暂存形态中服务端从 GLB 算出的一致，不一致则拒绝。比对规则：`slots`、`parts`、`anchors` 先按 `id` 排序；`id`、`hint`、`triangles` 精确相等；`bounds` 与挂点坐标容差 1e-4 m。名称取 GLB JSON 原文（three.js 加载后会删掉节点名里的 `:`）。写入完整形态时使用服务端的值。
- **读取**：GLB 和缩略图走现有媒体读取票据（`IssueTicket` / `Open`），角色分别为 `original` 和 `thumbnail`（票据角色加 `thumbnail`）。

### 生命周期与演进
- 持有方：场景修订引用表、资产、画布节点（后续若有）。三者均登记进 `creativereference.LiveOwnerCondition` 和守卫测试。没有持有方的修订按媒体 GC 规则回收。媒体 GC 的类型范围（`creativemedia/gc.go` 的 `unheldMediaRevision`）目前只含 image / video / audio，C2 必须把 `model` 加进去并登记守卫测试；上线时先升级 creative-worker，再开放模型写入。
- 外部 GLB 导入（下一版）会加 `generator.language = "imported"`，且没有 `code`。本版读取端遇到未知 `language` 时只能显示，不能调参数。

## 场景客户端任务

### 用途与存储角色
- **用途**：服务端派给创意空间页面（渲染端）执行的工作（烘焙模型、回看截图、导出），以及它的认领、租约、结果和关联的助手运行。
- **存储**：新表 `creative_scene_jobs`（PostgreSQL）。
- **角色**：权威主数据（任务状态）。产物本身在暂存上传或内容里，不在本表。
- **所有权**：`creativescene` 创建、过期、取消；渲染端经认领、续租、完成接口推进；`creativeagent` 经 `creativescene` 的终态端口读取任务结果，并在任务终态时继续运行。

### 结构与身份

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `id` | text | 是 | `csjob_<uuid>`，主键 |
| `account_id` | text | 是 | 账号隔离 |
| `document_id` | text | 是 | 目标场景 |
| `kind` | text | 是 | `bake_model` / `render_view` / `export_shots`（CHECK） |
| `request` | jsonb | 是 | 按 kind 定义，见下 |
| `state` | text | 是 | `queued`/`claimed`/`succeeded`/`failed`/`expired`/`cancelled`（CHECK） |
| `attempt` | integer | 是 | 从 0 开始，每次租约过期回到 queued 时加 1；达到 3 时不再回队，直接转 `failed`（`host_error`） |
| `claim_token` | text | 认领时 | 随机令牌，过期或终态时清空 |
| `editor_session_id` | text | 认领时 | 认领者标签页 |
| `lease_until` | timestamptz | 认领时 | 租约到期时间 |
| `deadline` | timestamptz | 是 | 创建时间 + 30 分钟 |
| `run_id` / `tool_call_id` | text | 助手创建时 | 关联运行与工具调用；摄影师发起的导出为 null |
| `result` | jsonb | 终态时 | 见下 |
| `created_at` / `updated_at` | timestamptz | 是 | — |

`request`：
- `bake_model`：`{code, params, lib_version, name_hint, target, expected}`。
  - `target` 为 `{op:"add", object:{…}}`（新物件的名称、变换、父级等，字段同 `add_objects` 的条目，不含 `model`），或 `{op:"replace", object_id, scope:"this"|"all_instances", object_ids}`；`all_instances` 时 `object_ids` 是建任务时共用同一模型修订的全部实例，此后不再展开。
  - `expected`：建任务时冻结的替换目标内容单元版本（`{"object:<id>:content": rev}`）；`add` 为空对象，完成时按当时的 `objects` 版本写入。
- `render_view`：三选一，加上 `mode`（C3 只有 `clay`）：
  - `{camera_object_id}`：按场景里的摄像机，画幅取摄像机的比例；
  - `{viewpoint:{position, target, focal_mm}}`：临时视点，4:3；
  - `{preset:"overview"|"top"|"front"|"side"}`：按全部可见物件的包围盒自动取景，4:3（总览为斜上方 3/4 视角）。
- `export_shots`：`{camera_object_ids, passes, canvas_id}`（C4）。

`result`：
- 成功：`bake_model` 为 `{content_id, content_revision_id, object_ids, revision}`；`render_view` 为 `{image_upload_id, content_revision_id, scene_revision}`（暂存截图及其实际渲染的场景修订）；`export_shots` 见 C4。
- 失败：`{code, message}`。`code` 取完成接口的错误码，或服务端写入的 `conflict`（场景单元冲突）、`invalid_result`（结果不合规，例如 manifest 与 GLB 不符、物件超出上限、父级不存在）、`host_error`（第 3 次租约过期）、`expired`、`cancelled`。`conflict` 时另带 `conflicts`（单元、预期与当前版本、改动者）和 `bake`（`{glb_upload_id, thumbnail_upload_id, module, manifest}`），助手重试时复用这份烘焙产物，不重新编译。

索引：`(account_id, state, created_at)`，用于账号级认领；`(account_id, document_id, state, created_at)`，用于按场景认领；`(state, deadline)`，用于过期扫描；`(run_id)`，用于取消运行时找到它的任务。

### 访问模式与保证
- **认领**：取最早的 `queued` 任务（指定场景时只在该场景内），用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。没有任务时只走索引、不加锁，渲染端在每次失效通知时都会调用。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。两者都经终态端口让关联运行继续。
- **完成**：经终态端口复查关联运行（与助手写工具同一套准入：能力、deadline、Skill、consent）→ 校验令牌与租约 → 落产物 → 改终态 → 让运行继续，在同一事务里提交（design「客户端任务」）。复查不通过时运行按现有规则结束，任务随之取消。
- **取消**：运行被取消时，在取消运行的同一事务里把它的非终态任务改为 `cancelled`，清空令牌。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。
