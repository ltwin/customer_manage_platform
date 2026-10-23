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
| `scene.environment.sky.sun_elevation_deg` | number −10…90 | `procedural` 时 | 新建为 35；0° 为地平线，90° 为天顶 |
| `scene.environment.sky.sun_azimuth_deg` | number 0…360 | `procedural` 时 | 新建为 135；0° 指向 +Z，90° 指向 +X（与 three.js Sky 示例的 `azimuth` 相同） |
| `scene.environment.sky.turbidity` | number 1…20 | `procedural` 时 | 新建为 3 |
| `scene.environment.sky.hdri` | string，内置 HDRI ID（`^[a-z0-9_]+@[1-9][0-9]*$`） | `hdri` 时 | — |
| `scene.environment.sky.color` | `#rrggbb` | `color` 时 | — |
| `scene.environment.ambient` | `{color:#rrggbb, intensity:0…10}` | 是 | `{#ffffff, 0.4}` |
| `scene.environment.fog` | `{color:#rrggbb, near>0, far>near}`（three.js `Fog`，米）或 null | 是 | null |
| `scene.render.mode` | `"clay"` \| `"lit"` | 是 | `clay`；只影响编辑器默认显示，导出时另行指定 |
| `scene.render.exposure_ev` | number −10…10（曝光补偿，档） | 否 | 0；只作用于 `lit`（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）：渲染时 three.js `toneMappingExposure = 2^exposure_ev`。白模、线稿和材质预览不受影响（修订 v10） |
| `scene.render.engine` | `"workbench"` \| `"realtime_gi"`（渲染器） | 否 | `workbench`；只决定最终渲染怎么出图（编辑器的最终渲染、`lit` 导出、`render_view` 的 `lit`）。`workbench` 是光栅化渲染；`realtime_gi` 在其上加入屏幕空间的间接光、反射与接触阴影，视口停止操作后收敛，导出和 `render_view` 多帧累积（`scene-webgpu-renderer` design「渲染器与显示模式」）。白模、线稿、线框和材质预览不受影响；正交视图按 `workbench` 显示。只计入 `lit` 的渲染指纹，值为 `workbench` 时不计入。`path_tracing` 由后续 Epic 加入，现在写入返回 400 |
| `scene.render.resolution` | `"1k"` \| `"2k"` \| `"4k"` \| `"8k"`（输出分辨率） | 否 | `2k`；导出图（`clay`、`line`、`lit`）的长边分别为 1024、2048、4096、8192 px，短边按摄像机的画幅比例。不影响视口、`render_view`（JPEG，长边 1024）和缩略图。不计入渲染指纹 |
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
| `objects[].light` | 按 `type` 区分，字段与 three.js 灯光类同名同义，见下方「灯光」 | `kind=light` 时 | 只有 `type` 必填，其余缺省取 three.js 默认值，存储时补齐 |
| `objects[].camera` | `{focal_mm:8…800, sensor_mm:[w,h]（各 1…100）, aspect:"<w>:<h>"（各 1…32 的整数）, near>0, far>near（米，同 three.js `PerspectiveCamera`）, label≤40, depth_of_field:bool, f_stop:0.5…64, focus>0（米，同 three.js `PerspectiveCamera.focus`）, focus_object_id:物件 ID 或 ""}` | `kind=camera` 时 | 各字段可省略，缺省 `{35,[36,24],"3:2",0.1,1000,"",false,2.8,10,""}`，存储时补齐；景深四个字段为修订 v8 新增，旧快照在读取时补齐。朝局部 −Z 看（three.js 摄像机约定）；视场角按画面长边对应传感器长边算出：`tan(fov/2) = max(sensor_mm) / (2·focal_mm) / max(画幅宽高比, 1)`，`fov` 为 three.js 的垂直视场角（修订 v8），不写 three.js 的 `fov`。`focus_object_id` 非空时按该物件原点沿视线方向的深度对焦：写入时必须指向修改后场景里的 `primitive`、`model_instance` 或 `group`，且不是摄像机自己；之后目标被删除不算违反契约，按 `focus` 对焦。写空字符串取消对焦物件（按键合并下 `null` 等于没写）。`depth_of_field = true` 时，白模导出按光圈和对焦虚化（design「渲染模式与导出」） |
| 单元（存于底座的 `creative_documents.unit_revs`，不在载荷里） | — | — | 场景的单元为 `environment`、`render`、`objects`，以及每个对象的两个单元：摆放 `object:<id>`（`name`、`transform`、`visible`、`locked`）和内容 `object:<id>:content`（`primitive`、`model`、`material_slots`、`light`、`camera`）；`meta` 由底座保留。`parent_id` 和 `order` 由 `objects` 管：它负责对象的存在性、层级和兄弟顺序 |

- **引用表**：`creative_document_content_refs` 记录每个修订引用的所有模型 `content_revision_id`。
- **灯光**（字段与 three.js 同名同义，渲染时原样赋给对应的灯光类）：

| `type` | three.js | 字段与缺省 |
|---|---|---|
| `directional` | `DirectionalLight` | `color`（#rrggbb，#ffffff）、`intensity`（0…1000，1）、`cast_shadow`（false） |
| `point` | `PointLight` | 以上三项，加 `distance`（米，≥ 0，0 表示不截断，缺省 0）、`decay`（0…10，2） |
| `spot` | `SpotLight` | `point` 的全部字段，加 `angle`（弧度，光锥中心到边缘的半角，0 < angle ≤ π/2，缺省 π/3）、`penumbra`（0…1，0） |

  - 写了该类型没有的字段返回 400 并指出字段（例如点光写了 `angle`）。
  - `intensity` 的含义同 three.js 默认的物理光照：点光和聚光为坎德拉（cd），平行光为 three.js 的强度值。
  - 方向：平行光和聚光沿物件局部 −Z 照射，与 three.js 摄像机、Blender 灯光一致。three.js 的 `target` 不进格式，渲染时由旋转算出；平行光的位置只影响阴影范围。
- **命名原则**（修订 v7）：助手要写的字段与 three.js 同名同义；three.js 没有对应字段的，保留带单位的名字（`sun_elevation_deg`、`focal_mm`、`sensor_mm`）。不接受别名，写错名字返回 400 并指出字段。变换的 `rotation` 保持四元数：three.js 里它对应 `quaternion`（three.js 的 `rotation` 是欧拉角），但两者长度不同，写错会被拒绝而不会悄悄出错，C3 试用中助手也全部写对。

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
- 例外：修订 v7 的改名（雾和摄像机的 `near_m`/`far_m` → `near`/`far`，灯光 `range_m` → `distance`、`angle_deg` → `angle` 并补 `decay`）发生在上线之前，生产库没有场景数据。由一次迁移改写已有的场景快照，`schema_version` 仍为 1，不保留旧字段的读取兼容。旧的 `angle_deg` 没有定义是整锥角还是半角，也从未渲染过，迁移按整锥角换算（`angle = angle_deg / 2`，换成弧度）；2026-10-18 开发库里没有灯光物件。
- 修订 v8（C4 起草时）：摄像机视场角改为画面长边对应传感器长边（原为固定按 `sensor_mm[1]` 算垂直视场角，竖幅画面比同焦距的全画幅相机窄约 1.5 倍）。只改渲染端的换算，字段外形和存储不变，不迁移；默认传感器下 3:2 横幅的画面不变，助手已建的非 3:2 摄像机和 `render_view` 临时视点的取景随之改变。
- 修订 v8（C4 起草时）：摄像机新增 `depth_of_field`、`f_stop`、`focus`、`focus_object_id`。不迁移：读取时补齐默认值（服务端解码时补齐，读取场景的响应里也带齐），之后的写入按完整字段存储。「空物体」是没有子物件的 `group`，不新增 `kind`。
- 修订 v10（C5 起草时）：`render` 新增 `exposure_ev`。灯光按 three.js 的物理单位（坎德拉），室外有太阳和只有几盏灯的室内亮度差几个数量级，固定曝光无法同时照顾两者，所以由场景自己定曝光，与摄影里按场景定曝光一致；用「档」而不是倍数，与相机的曝光补偿、Blender 色彩管理的曝光相同。不迁移：读取时补 0，读取场景的响应里也带齐。
- `scene-webgpu-renderer` R2：`render` 新增 `engine` 和 `resolution`。两者是场景的渲染设置而不是本机偏好，这样编辑器、`render_view` 和导出看到同样的画面，摄影师、其他标签页和助手也一致。不迁移：读取时补 `workbench`、`2k`，读取场景的响应里也带齐。回滚到不认识这两个字段的后端会读取失败，只做前向修复。

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
- `render_view`：三选一，加上 `mode`（`clay` 或 `lit`，缺省 `clay`；`lit` 自修订 v10 起，与 `lit` 导出同一套渲染：材质、灯光物件、天空、阴影和 AO）：
  - `{camera_object_id}`：按场景里的摄像机，画幅取摄像机的比例；
  - `{viewpoint:{position, target, focal_mm}}`：临时视点，4:3；
  - `{preset:"overview"|"top"|"front"|"side"}`：按全部可见物件的包围盒自动取景，4:3（总览为斜上方 3/4 视角）。
  - `depth_of_field`（修订 v8，可选，默认 `false`）：`true` 时按这台摄像机的光圈和对焦渲染虚化，画面与白模导出同一套渲染（含 AO），长边仍为 1024。只能与 `camera_object_id` 一起用，并且建任务时这台摄像机的 `depth_of_field` 必须为 `true`，否则工具直接返回错误、不建任务；渲染时按当时快照里的光圈和对焦，不再检查开关。缺省时全清晰。修订 v9：截图与编辑器视口、白模导出同一套渲染，带 AO；`depth_of_field` 只决定是否虚化。
- `export_shots`：`{camera_object_ids, passes, canvas_id, scene_node_id}`（C4）。
  - `camera_object_ids`：建任务时当前快照里的摄像机（工具未指定时为全部摄像机，按物件顺序）；`passes`：`clay` / `line` / `lit` 的子集，不重复。图片总数 ≤ 48（`api: 导出分镜` 的上限）。
  - `canvas_id`、`scene_node_id`：运行所在画布，以及该画布上这个场景的节点（有多个时取最早创建的）；建任务时确定，场景不在该画布上时不建任务。
  - 渲染端出图上传后，把图片清单随完成任务提交（`api: 完成场景客户端任务` 的 `export`），节点在完成事务里创建。任务没有成功，画布上就没有节点；同一任务最多一组节点。

`result`：
- 成功：`bake_model` 为 `{content_id, content_revision_id, object_ids, revision}`；`render_view` 为 `{image_upload_id, content_revision_id, scene_revision}`（暂存截图及其实际渲染的场景修订）；`export_shots` 与 `api: 导出分镜` 的成功响应相同：`{change_id, scene_revision, nodes:[{camera_object_id, pass, node_id, content_revision_id, lens}]}`（`lens` 见 `data: 导出图来源`，修订 v9）。
- 失败：`{code, message}`。`code` 取完成接口的错误码，或服务端写入的 `conflict`（场景单元冲突）、`invalid_result`（结果不合规，例如 manifest 与 GLB 不符、物件超出上限、父级不存在；导出无法放置，例如画布已归档、场景节点已不在画布）、`host_error`（第 3 次租约过期）、`expired`、`cancelled`。`conflict` 时另带 `conflicts`（单元、预期与当前版本、改动者）和 `bake`（`{glb_upload_id, thumbnail_upload_id, module, manifest}`），助手重试时复用这份烘焙产物，不重新编译。

索引：`(account_id, state, created_at)`，用于账号级认领；`(account_id, document_id, state, created_at)`，用于按场景认领；`(state, deadline)`，用于过期扫描；`(run_id)`，用于取消运行时找到它的任务。

### 访问模式与保证
- **认领**：取最早的 `queued` 任务（指定场景时只在该场景内），用 `FOR UPDATE SKIP LOCKED` 改为 `claimed`，写入令牌、会话和租约。由数据库行锁保证同一任务只被一个会话认领（I5）。没有任务时只走索引、不加锁，渲染端在每次失效通知时都会调用。
- **租约回收**：worker 周期扫描（复用 creative-worker 的维护循环），把 `claimed` 且租约已过期的任务改回 `queued`，`attempt` 加 1，清空令牌；`attempt` 达到 3 时改为 `failed`（`host_error`）。把超过 `deadline` 的非终态任务改为 `expired`。两者都经终态端口让关联运行继续。
- **完成**：经终态端口复查关联运行（与助手写工具同一套准入：能力、deadline、Skill、consent）→ 校验令牌与租约 → 落产物 → 改终态 → 让运行继续，在同一事务里提交（design「客户端任务」）。复查不通过时运行按现有规则结束，任务随之取消。
- **取消**：运行被取消时，在取消运行的同一事务里把它的非终态任务改为 `cancelled`，清空令牌。
- **通知**：表上安装提交后通知触发器，走账号级失效通道（`notify_creative_canvas_changed('account')`）。只在新建、回到 `queued` 和进入终态时通知（触发器 `WHEN` 条件），认领、续租和删除不通知。

### 生命周期与演进
- 终态任务保留 30 天后由维护任务删除；只删任务行，不影响产物。
- 新增 `kind` 时扩展 CHECK，并在 `request` 定义新外形；渲染端只认领自己能执行的 kind（认领请求的 `kinds`）。C3 实现 `bake_model`、`render_view`，`export_shots` 由 C4 实现。修订 v10：`render_view` 的 `mode` 增加 `lit`（C5），请求外形不变，旧任务缺省按 `clay`。

## 导出图来源

- **用途**：记录画布图片节点由哪个场景、哪个修订、哪台摄像机、哪种导出方式生成，用于提示过时。
- **存储**：`creative_nodes.config` 内的 `based_on` 对象，只对导出产生的 `core.image` 节点写入：`{scene_document_id, scene_revision: CreativeRevision, camera_object_id, pass:"clay"|"line"|"lit", content_revision_id, fingerprint, lens}`。`content_revision_id` 是导出时放进节点的那份图片内容修订；`fingerprint` 是 `scene_revision` 的快照对这台摄像机、这种导出方式的渲染指纹（带版本前缀，如 `v1:`），客户端只当不透明字符串。
- **镜头快照 `lens`**（修订 v9）：导出时这台摄像机的镜头，`{focal_mm, depth_of_field, f_stop, focus_m, focus_object_name}`。`focus_m` 是实际对焦距离（米）：有对焦物件时按物件原点沿视线的深度算出，否则为 `focus`，并夹在 `near`、`far` 之间；`focus_object_name` 是对焦物件的名称，按距离对焦或对焦物件已删除时为空字符串。景深关闭时也照样记录。`lens` 只说明这张图是怎么拍的，不参与过时判断（指纹已计入这些参数）；没有 `lens` 的节点按未知处理。
- **角色**：派生信息，只用于提示，不参与权限和内容。
- **写入**：只由导出的放置流程经画布命令层写入，入口是「导出分镜」接口和 `export_shots` 的完成事务；画布命令的 `add_node` 不接受 `config`，客户端无法写入。写入后不再修改：节点当前内容不是 `content_revision_id`（换了图、切了版本）时，来源信息不生效，所以改图的操作不需要逐一清除它。复制节点时随 `config` 一起复制，内容相同，判断照样成立。
- **读取**：画布节点快照新增可选只读字段 `based_on_state`，只出现在 `based_on` 生效的节点上（不生效时不返回）：
  - `unavailable`：来源场景不存在、在回收站或快照不可展示，不显示过时；
  - `current`：场景之后没有与这张图相关的修改；
  - `stale`：有，前端显示「场景已更新」。
- **助手读取**（修订 v9）：`read_canvas` 返回的节点里，`based_on` 生效的导出图带 `export_source: {scene_document_id, scene_revision, camera_object_id, pass, lens, state}`，`state` 即 `based_on_state`。画布的 HTTP 节点快照不变，仍只返回 `based_on_state`。
- **过时判断**：读取时计算，不落库。对场景当前快照算同一台摄像机、同一种导出方式的渲染指纹，与 `fingerprint` 比较，不同即 `stale`；这台摄像机已不在当前快照里也算 `stale`。指纹计入哪些内容见 design「渲染模式与导出」，按导出方式区分。改动后又改回原样不算过时。指纹版本与当前不同时算 `stale`，不读旧快照重算（修订 v9，读画布不增加查询；指纹版本只在渲染规则变化时升级，届时旧图本来就该重新导出）。

## 模型资产

- **用途**：资产库可以存放 `model` 资产：一份模型（几何、代码、参数）加它的材质分配，相当于 Blender 的物件资产。位置、旋转、缩放是场景物件的变换，不随资产保存（修订 v12，owner 2026-10-20）。
- **存储**：
  - `creative_assets.kind` 的 CHECK 加 `model`。资产库现有能力（分组、标签、评分、回收站、筛选）对 `model` 一视同仁；按画面的筛选（主色、画幅）只作用于图片，模型自然不命中。
  - **库副本**（修订 v12）：存入时新建一个模型内容，写一个完整形态的修订，资产引用它。它是场景里那个模型修订的复制品：
    - 载荷：`format`、`generator`（含代码）、`manifest`、`name_hint` 与来源修订相同，另加两个字段：

      | 路径 | 类型 | 必须 | 约束 |
      |---|---|---|---|
      | `model.materials` | object | 库副本必有 | 槽位到材质的分配，结构同场景对象的 `material_slots`（`{<slot_id>: {preset, params}}`），只保留 manifest 里存在的槽位，可以是空对象 |
      | `model.source_revision_id` | string | 库副本必有 | 复制自哪个模型修订。来源本身是库副本时取它的 `source_revision_id`，所以总指向场景里烘焙出的修订 |

      场景命令写出的修订没有这两个字段；有 `source_revision_id` 就是库副本。
    - 对象：`original` 复用来源修订的 GLB blob，不复制字节；`thumbnail` 是存入时上传的 PNG，按所配材质渲染（未分配的槽位按 C5 的规则取提示或默认材质）。
    - 写入口：只由「模型存入资产库」写入；`commit_model` 的操作结构里没有这两个字段，场景命令写不出库副本。约束由应用保证。
    - 修订 v12 之前直接引用场景修订的模型资产只存在于开发环境，照常可读，视为没有材质分配，不迁移。
- **查重**（修订 v12，替代 v11 的按模型修订查重）：按「来源修订 + 材质分配」。来源修订相同、材质分配完全相同（JSON 相等）且不在回收站的模型资产已存在时复用，只追加归类；材质不同就是另一份资产。不按 GLB 的 sha256 合并：GLB 相同而代码或参数不同的两个修订是两份资产，各自保留自己的代码。
- **持有**：资产是库副本修订的 GC 持有方，经现有的 `creativereference.LiveOwnerCondition`（按 `content_revision_id` 与 `kind` 匹配）。与来源修订共用的 GLB、存入时上传的缩略图随之保留；不新增引用列。
- **库副本只属于资产库**（修订 v12）：`commit_model` 的 `base_content_id` 指向库副本的内容时，服务端新建内容，不在库副本上追加修订，响应的 `models` 照常返回新内容的 ID。这样放进场景后的修改，不论来自编辑器还是助手、页面是否刷新过，总是分叉出场景自己的模型，库副本只有存入时的那一个修订。这条补充 `api: 场景模型命令` 的分叉规则（I4），由本条交付。
- **读取**（修订 v11）：
  - 资产列表、资产读取返回的模型内容去掉 `generator.code`，与场景读取一致；需要代码时按内容修订读取（`GET /creative/content-revisions/{id}`）。`materials`、`source_revision_id` 照常返回（修订 v12）。
  - 助手（修订 v11）：
    - `search_assets` 对模型项附模型要点 `model`：名称、尺寸 `size_m`（米）、`mount`、面数、材质槽、参数名和材质分配 `materials`（修订 v12）。不附缩略图的宽高，因为 256 × 256 不是模型尺寸。
    - `read_asset` 读模型时返回完整模型信息：在要点之外加部件、挂点、参数定义和当前值。字段名与 `write_model` 回报的一致，不含代码。超出单次工具结果上限时依次省略部件与挂点、参数定义，`omitted` 列出省略项（修订 v12）。
    - 缩略图作为图片附件交给模型，需要模型能看图且摄影师允许图片预览。条件不满足，或之后因图片上限省略、读不到时，只保留模型信息，在 `thumbnail_omitted` 里说明原因，不报错。
    - 模型信息按 `media_metadata` 外发，与资产库元数据相同。
  - 卡片用 `thumbnail` 角色的 PNG，查看浮窗按 `original` 角色读 GLB，都走现有媒体读取票据；查看浮窗的「材质」模式按 `materials` 着色（修订 v12）。
- **放置**：模型资产只能放进场景（`add_objects` 的 `model.content_revision_id`），不能放到画布：没有 `core.model` 节点类型，画布的放入、填充和参考选择都不接受它。放入时实例的 `material_slots` 写资产的 `materials`：编辑器自动写，助手按 `edit_scene` 的说明写。之后在场景里换材质只改实例（修订 v12）。
- **OpenAPI**（修订 v12）：`CreativeModelPayload` 与只用于响应的 `CreativeModelPreviewPayload` 的 `model` 加可选的 `materials`、`source_revision_id`。`CreativeModelPayload` 也被请求体引用，但模型不能经通用写入口写入，放宽不产生新的写法。
- **生命周期**：回滚时如已有 `model` 资产则拒绝，前向修复。资产库不记录资产被哪个场景使用过（I7）。

## 物理天空

### 用途与存储角色
- **用途**：扩展 `data: 场景文档` 的 `scene.environment.sky`，新增天空模式 `physical`。太阳由地点和时间推算或手动指定，天空和光照由物理大气算出，场景按物理单位渲染。
- **存储**：与场景文档相同，位于快照载荷（`schema_version = 1`）的 `environment` 单元内。不新增表、列或单元。
- **角色**：权威主数据。天体方向、大气 LUT 和曝光都是渲染端的派生量，不落库。
- **所有权**：
  - 只由 `creativescene` 经 `set_environment` 写入（环境选项卡、助手 `edit_scene`）。
  - 读取方：渲染端、环境选项卡、助手 `read_scene`、渲染指纹。

### 结构与身份
`sky.kind = "physical"` 时的字段如下。除 `kind` 和 `date` 外都可省略，存储时补齐缺省值。读取场景的响应里总是带齐。

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `sky.kind` | 新增取值 `"physical"`（与 `procedural`、`hdri`、`color` 并列） | — | 新建场景仍为 `procedural` |
| `sky.sun_mode` | `"location"` \| `"manual"` | `location` | 选择太阳由地点和时间推算，还是用下面的手动角度。两套字段都保存 |
| `sky.latitude_deg` | number −90…90（北纬为正） | 31.2304 | 与 `longitude_deg`、`timezone` 要么都写，要么都省略；都省略时为上海。只写一部分返回 400 |
| `sky.longitude_deg` | number −180…180（东经为正） | 121.4737 | 同上 |
| `sky.timezone` | IANA 时区名，区分大小写：服务端 `time.LoadLocation` 能加载，写法与时区数据里的名字完全一致（`asia/shanghai` 拒绝），且不是 `Local` 或空串（`LoadLocation("")` 会返回 UTC） | `Asia/Shanghai` | 同上；`local_time` 是这个时区的当地时间，夏令时由时区库处理 |
| `sky.place` | string ≤ 80 | `""` | 地点的显示名（例如「成都」），只用于界面和助手阅读，不参与计算 |
| `sky.date` | `"YYYY-MM-DD"`，1900-01-01…2100-12-31，必须是存在的日期 | 无，**必填** | 当地日期。`set_environment` 整体替换，缺省值会在每次省略时重置日期，所以不设缺省；编辑器切到物理天空时写入本机的当天 |
| `sky.local_time` | `"HH:MM"`，00:00…23:59 | `"12:00"` | 当地时刻，精确到分钟 |
| `sky.north_azimuth_deg` | number 0…360 | 180 | 正北在文档坐标中的方位，约定同 `sun_azimuth_deg`（0° 指向 +Z，90° 指向 +X）；180° 即正北指向 −Z（视图空间 +Y，同 Blender 的 +Y 为北） |
| `sky.sun_elevation_deg` | number −10…90 | 35 | `sun_mode = manual` 时的太阳高度，含义同程序化天空 |
| `sky.sun_azimuth_deg` | number 0…360 | 135 | `sun_mode = manual` 时的太阳方位，含义同程序化天空 |
| `sky.altitude_m` | number 0…20000 | 0 | 场景 Y = 0 处的海拔（米） |
| `sky.air_density` | number 0…10 | 1 | 空气（瑞利散射）密度倍率，同 Blender Nishita 天空的 Air |
| `sky.dust_density` | number 0…50 | 1 | 尘埃与气溶胶（米氏散射）密度倍率，同 Blender 的 Dust；基准与 Blender 同量级。上限比 Blender 的 10 大，是为了做出浓雾霾和沙尘（修订 v2） |
| `sky.ozone_density` | number 0…10 | 1 | 臭氧吸收倍率，同 Blender 的 Ozone |
| `sky.sun_disc` | boolean | true | 是否画太阳盘；不影响太阳光 |
| `sky.sun_size_deg` | number 0.1…10 | 0.545 | 太阳盘的视直径（度） |
| `sky.sun_strength` | number 0…10 | 1 | 太阳的亮度倍率：太阳光、太阳盘和它照亮的天空一起乘（修订 v2） |
| `sky.sun_temperature_k` | number 2000…12000 | 6500 | 太阳的色温（开尔文），按黑体颜色相对 6500 K 换算；6500 为中性，调低更暖、调高更冷；同样作用于太阳光和天空（修订 v2） |

- `physical` 时不得出现 `turbidity`、`color`、`hdri`，写了返回 400 并指出字段，与「天空只带自己类型的字段」一致。
- 反过来，`procedural`、`hdri`、`color` 时不得出现上表中物理天空独有的字段（除 `kind`、`sun_elevation_deg`、`sun_azimuth_deg` 以外的各行），写了同样返回 400 并指出字段。否则这些字段会进入 `lit` 指纹，已有导出误报过时。
- `sky.night`、`sky.aerial_perspective` 由 `data: 夜空`、`data: 空气透视` 定义，交付之前写入返回 400。
- `sky.clouds` 和 `environment.weather` 保留给远期的云层和天气，现在写入返回 400。
- 预设（晴朗、雾霾、高原、沙尘等）不是字段：选预设等于写入一组 `altitude_m`、`air_density`、`dust_density`、`ozone_density`。各预设的取值由 W2 定，列在助手工具说明和编辑器里。预设不改太阳的亮度和色温。

**物理天空下其他字段的解释**（只改解释，不改存储）：

| 字段 | 物理天空下 | 其他天空模式下 |
|---|---|---|
| 平行光 `intensity` | 照度（lux） | 不变（three.js 的强度值，按 `litLook` 标定） |
| 点光、聚光 `intensity` | 坎德拉（cd） | 坎德拉（cd） |
| `environment.ambient` | 不生效（字段保留） | 不变 |
| `environment.fog` | 雾色视为介质的反照率，亮度随太阳直射与天空漫射的水平照度变化 | 不变 |
| 材质的自发光（`emissive` × `emissive_intensity`，模型的 `KHR_materials_emissive_strength`） | 亮度（cd/m²），乘以预曝光（修订 v2） | 不变 |
| `render.exposure_ev` | 不生效（字段保留） | 场景曝光补偿，`toneMappingExposure = 2^exposure_ev` |
| `render.exposure_ev100`、摄像机曝光字段 | 生效（`data: 摄像机曝光`） | 保存但不生效 |

切换天空模式时不改写任何曝光字段。

已知限制：灯光的 `intensity` 上限仍是 1000（`data: 场景文档`「灯光」）。物理天空下按 lux 解释时，场景里的平行光最多约为正午阳光的 1%，只能用来补光，不能当第二个太阳。需要放宽时另行修订灯光的取值范围。

### 访问模式与保证
- **写**：`set_environment` 整体替换 `environment`。服务端在同一事务里校验上表的约束（沿用场景文档的写入流程），补齐缺省值后存储。
- **读**：随场景快照一起读。
- **保证方**：取值范围、日期有效、三项地点字段成组、时区名可加载，都由应用（`creativescene`）保证。数据库不感知这些字段。
- **渲染指纹**：`lit` 计入整个 `sky`。天空是 `physical` 时，`lit` 不计入 `render.exposure_ev`，因为它在物理天空下不生效。`clay`、`line` 不计入 `environment`（`scene-world-environment` design「渲染指纹」）。

### 生命周期与演进
- 新增取值，不迁移：已有快照不含 `physical`，读取不受影响。
- 回滚到不认识 `physical` 的后端时，这些场景会读取失败，只做前向修复（沿用 R2 的做法）。
- 示例（`set_environment` 的 `environment`）：

```json
{"sky": {"kind": "physical", "sun_mode": "location",
         "latitude_deg": 30.5728, "longitude_deg": 104.0668, "timezone": "Asia/Shanghai", "place": "成都",
         "date": "2026-10-01", "local_time": "17:40", "north_azimuth_deg": 180,
         "altitude_m": 500, "air_density": 1, "dust_density": 3, "ozone_density": 1},
 "ambient": {"color": "#ffffff", "intensity": 0.4},
 "fog": null}
```

存储后，`sun_elevation_deg`、`sun_azimuth_deg`、`sun_disc`、`sun_size_deg`、`sun_strength`、`sun_temperature_k` 都按缺省补齐。

## 摄像机曝光

### 用途与存储角色
- **用途**：
  - 扩展 `data: 场景文档` 的 `objects[].camera`：曝光模式（环境测光或手动）、环境测光的补偿、手动曝光的 ISO 和快门（光圈沿用现有的 `f_stop`）。
  - 扩展 `scene.render`：默认摄像机（视口自由视角、`render_view` 的临时视点）的曝光模式、补偿，以及手动时的默认曝光值。
  - 曝光只在物理天空下生效。W2 交付手动曝光；修订 v5（owner 2026-10-23）加入环境测光，新建摄像机和默认摄像机缺省环境测光。
- **存储**：与场景文档相同。摄像机的字段位于摄像机对象的内容单元 `object:<id>:content` 内，默认摄像机的字段位于 `render` 单元内。
- **角色**：权威主数据。实际的 EV100、环境测光的测光值和预曝光系数是渲染端的派生量，不落库。
- **所有权**：
  - 摄像机的字段由 `creativescene` 经 `add_objects`、`update_objects` 写入（摄像机属性、助手）。
  - 默认摄像机的字段经 `set_render` 写入（渲染选项卡、助手）。
  - 读取方：渲染端、渲染指纹。

### 结构与身份
新增的摄像机字段与现有摄像机字段平级，`update_objects` 里按键合并；`render` 的新字段在 `set_render` 里按键合并。各字段都可省略，存储时补齐缺省值。

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `camera.exposure_mode` | `ambient` \| `manual` | 写入时 `ambient`；读取没有这个字段的已存储摄像机时 `manual` | 曝光模式（修订 v5） |
| `camera.exposure_compensation` | number −5…5（档） | 0 | 环境测光的补偿，正值变亮；手动时保存但不生效（修订 v5） |
| `camera.iso` | number 25…102400 | 100 | 感光度，只在手动时生效 |
| `camera.shutter_s` | number 0.000125…30（秒，即 1/8000 s…30 s） | 0.00025（1/4000 s） | 快门，只在手动时生效 |
| `render.exposure_mode` | `ambient` \| `manual` | `ambient` | 默认摄像机的曝光模式（修订 v5） |
| `render.exposure_compensation` | number −5…5（档） | 0 | 默认摄像机环境测光的补偿（修订 v5） |
| `render.exposure_ev100` | number −10…20 | 15 | 默认摄像机手动时的曝光值（EV100）。15 约为晴天正午 |

- 手动：摄像机的 EV100 = log₂(`f_stop`² / `shutter_s`) − log₂(`iso` / 100)。用现有的 `f_stop`，与 `depth_of_field` 是否打开无关。缺省值（ISO 100、1/4000 s、f/2.8）约为 EV100 15。默认摄像机的 EV100 = `render.exposure_ev100`。
- 环境测光：EV100 = EV_m + max(D(EV_m), L) − 补偿。EV_m 是场景原点水平照度的入射式测光值，D 是夜间压暗，L 是低太阳压暗（Epic design「物理单位与曝光」，修订 v6），都由渲染端从场景算出，不落库。`f_stop` 只决定景深。
- 编辑器在物理天空下添加摄像机时，沿用默认摄像机的曝光设置，新摄像机的画面与视口一样亮：
  - 默认摄像机为环境测光时，写 `exposure_mode: ambient` 和同样的补偿；
  - 默认摄像机为手动时，写 `exposure_mode: manual`，按 `render.exposure_ev100` 换算：先按 ISO 100 取最接近的 1/3 档快门，快门越界时取边界快门，再调 `iso`。缺省光圈 f/2.8 下可达 EV100 约 −11.9…17.9，超出时新摄像机偏暗（已知局限）。
- 缺省值只用于没写曝光字段的写入，例如助手不带曝光地添加摄像机：环境测光，补偿 0。
- 天空不是 `physical` 时，这些字段保存但不生效；切换天空模式时不改写它们。

### 访问模式与保证
- **写**：按现有摄像机字段和渲染字段的规则校验和合并。取值范围由应用保证。`update_objects` 的按键合并在补齐后的旧值上进行：只改 ISO 的旧摄像机仍是手动。
- **读**：旧快照里的摄像机和 `render` 可能缺这些字段，服务端解码时补齐，读取场景的响应里也带齐（与修订 v8 的景深字段、修订 v10 的 `exposure_ev` 相同）：
  - 摄像机缺 `exposure_mode` 时补 `manual`：W2 时期的摄像机都按手动曝光拍，画面和导出不变；
  - `render` 缺 `exposure_mode` 时补 `ambient`：默认摄像机不进导出，没有导出因此过时；
  - 摄像机或 `render` 的 `exposure_mode` 为 `auto`（修订 v5 的取值）时读作 `ambient`（修订 v6 改名）；写入只接受 `ambient`、`manual`；
  - 其余字段补缺省值。
- **渲染指纹**：
  - 摄像机的曝光字段只在 `lit` 且天空为 `physical` 时计入：
    - 手动：计入 `iso`、`shutter_s`；即使景深关闭也计入 `f_stop`，因为光圈决定亮度。不计入 `exposure_mode`（`manual`）和 `exposure_compensation`，所以 W2 时期的摄像机补齐为手动后，哈希输入不变。
    - 环境测光：计入 `exposure_mode`、`exposure_compensation`；不计入 `iso`、`shutter_s`；`f_stop` 按景深的现有规则。
  - `clay`、`line`，以及非物理天空的 `lit`，都清除摄像机的曝光字段；
  - 任何导出方式都不计入 `render` 的曝光字段（`exposure_mode`、`exposure_compensation`、`exposure_ev100`）：导出总是通过场景摄像机。
  - 环境测光的测光值随环境变化，环境已经计入指纹，不另计。
  - 因此改一台摄像机的曝光，只让这台摄像机在物理天空下的 `lit` 导出过时；改默认摄像机的曝光不让任何导出过时。
- 不进导出图来源的 `lens` 快照（`data: 导出图来源` 不变）。

### 生命周期与演进
- 不迁移，读取时补齐（规则见上）。
- 回滚到不认识这些字段的后端时，带这些字段的快照会读取失败，只做前向修复。
- 修订 v5 取代 W2 时「将来加自动曝光时缺省为手动」的约定：新写入的摄像机缺省环境测光；已存储的摄像机读作手动，已有导出不变；默认摄像机缺省环境测光，视口的画面会变，但不影响导出。

## 外部助手连接与 OAuth

### 用途与存储角色
- **用途**：外部助手的授权与身份：连接、OAuth 客户端、授权码、refresh token。
- **存储**：PostgreSQL，新表 `external_agent_connections`、`oauth_clients`、`oauth_authorization_codes`、`oauth_refresh_tokens`。
- **角色**：连接是权威主数据；客户端、授权码、refresh token 是授权服务器内部数据。
- **所有权**：只有 `platform/oauth` 读写。业务模块只拿到连接 ID 和展示名（经助手主体传入），不读这些表。

### 结构与身份

`external_agent_connections`：

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `id` | text | 是 | `cxcon_<uuid>`，主键 |
| `account_id` | text | 是 | 账号隔离；外键到账号 |
| `client_id` | text | 是 | OAuth 客户端标识：CIMD 时是元数据 URL，DCR 时是 `oclient_<uuid>` |
| `client_name` | text ≤ 100 | 是 | 授权时客户端声明的名称，原样保存，用于确认页和审计 |
| `host_kind` | text | 是 | `claude`（claude.ai 连接器，含共用它的 Claude Code）/ `claude_code`（本机添加的 Claude Code）/ `codex` / `chatgpt` / `other`（CHECK）；授权时按客户端元数据（客户端名、回调地址主机、CIMD 地址）识别，识别规则在 C1 实测后固定 |
| `display_name` | text ≤ 100 | 是 | 界面署名：「Claude」「Claude Code」「Codex」「ChatGPT」，`other` 取 `client_name` |
| `scopes` | text[] | 是 | 当前只有 `{creative}` |
| `created_at` | timestamptz | 是 | 授权时间 |
| `last_used_at` | timestamptz | 否 | 最近一次工具调用；每分钟最多更新一次 |
| `revoked_at` | timestamptz | 否 | null 表示有效 |
| `revoked_reason` | text | 否 | `user`（摄影师在接入页断开）/ `refresh_replay`（检测到已轮换的 refresh token 被重放）/ `account_disabled`（CHECK）；`revoked_at` 非空时必填 |

索引：`(account_id, revoked_at, last_used_at DESC)`。

`oauth_clients`：

| 列 | 类型 | 必须 | 约束与含义 |
|---|---|---|---|
| `client_id` | text | 是 | 主键 |
| `registration` | text | 是 | `cimd` / `dcr`（CHECK） |
| `client_name` | text ≤ 100 | 是 | — |
| `redirect_uris` | text[] | 是 | 1–5 个；每个都是回环地址（`http://127.0.0.1:*`、`http://localhost:*`，命令行宿主）或 https（网页端宿主的服务端回调，例如 claude.ai、ChatGPT） |
| `token_endpoint_auth_method` | text | 是 | `none`（公共客户端）/ `client_secret_post` / `client_secret_basic`（机密客户端，网页端宿主）（CHECK）；CIMD 客户端只能是 `none` |
| `client_secret_hash` | text | 机密客户端时 | 注册时签发的密钥的 SHA-256；明文只在注册响应里返回一次 |
| `metadata` | jsonb | 是 | 注册时或 CIMD 拉取到的原始元数据（≤ 5 KB） |
| `fetched_at` | timestamptz | CIMD 时 | 上次拉取时间；超过缓存期（≤ 24 小时）再用时重新拉取 |
| `created_at` | timestamptz | 是 | — |

客户端不属于任何账号：同一个客户端（例如 claude.ai 的连接器）可被多个账号授权。DCR 客户端 90 天没有被任何连接使用时由维护任务删除。

`oauth_authorization_codes`：主键 `code_hash`（SHA-256），列 `client_id`、`account_id`、`connection_id`、`redirect_uri`、`code_challenge`（S256）、`resource`、`expires_at`（签发后 60 秒）、`used_at`。只能用一次；第二次使用时断开对应连接。过期一天后删除。

`oauth_refresh_tokens`：主键 `token_hash`（SHA-256），列 `connection_id`、`account_id`、`state`（`current` / `rotated` / `revoked`，CHECK）、`created_at`、`last_used_at`、`absolute_expires_at`（连接内第一张 refresh token 签发后 90 天）。每个连接同时最多一行 `current`（部分唯一索引）。空闲超过 30 天视为失效。

### 访问模式与保证
- **授权确认**：同一事务建连接、写授权码。同一账号对同一 `client_id` 再次授权时新建一个连接，不复用旧连接（旧连接保持原状，摄影师可以在接入页断开）。
- **换令牌**：锁授权码行 → 校验未用、未过期、`redirect_uri`、PKCE、`resource` → 标记已用 → 写第一张 refresh token。
- **refresh**：锁 refresh 行 → `current` 则改为 `rotated` 并写新的 `current`；`rotated` 则断开连接（`refresh_replay`）、把该连接所有行改为 `revoked`。
- **每次 MCP 请求**：按 `id` 读连接，`revoked_at` 为 null 才放行。
- **断开**：同一事务写 `revoked_at`、作废 refresh 链，并通过 `creativescene` 的端口取消该连接非终态的场景客户端任务（C3 起）；提交后发账号失效通知。
- **保证方**：
  - 由数据库保证：主键唯一、CHECK、每连接一行 `current`。
  - 由应用保证：授权码一次性、PKCE 与 `resource` 校验、断开的级联。

### 生命周期与演进
- 连接永不物理删除。业务表用它的 ID 做署名，已断开的连接仍是署名来源。
- 账号停用时，维护任务断开它的全部连接（`account_disabled`）。
- 以后做细粒度授权时扩展 `scopes`，存量连接保持 `{creative}`。

## 夜空

### 用途与存储角色
- **用途**：扩展物理天空，控制月亮、星星和银河的绘制。
- **存储**：`scene.environment.sky.night`，位于 `environment` 单元内。
- **角色**：权威主数据。月亮的位置和月相、星星的位置都是渲染端由地点和时间算出的派生量。
- **所有权**：同 `data: 物理天空`。

### 结构与身份

| 路径 | 类型 | 缺省 | 说明 |
|---|---|---|---|
| `sky.night` | object | 下列缺省 | 只在 `sky.kind = physical` 时允许，可省略 |
| `sky.night.moon` | boolean | true | 月亮（月盘、月光） |
| `sky.night.stars` | boolean | true | 星表中的亮星 |
| `sky.night.milky_way` | boolean | true | 银河（渲染端程序化生成） |
| `sky.night.moon_strength` | number | 1 | 月亮的亮度倍率，0…10：月盘、月光和月光照亮的天空一起变化 |
| `sky.night.moon_temperature_k` | number | 6500 | 月亮的色温，2000…12000 K；6500 为物理计算的颜色，越低越暖 |
| `sky.night.stars_strength` | number | 1 | 星空的亮度倍率，0…10：星星、银河和夜天光一起变化 |
| `sky.night.stars_temperature_k` | number | 6500 | 星空的色温，2000…12000 K，含义同上 |
| `sky.night.night_look` | number | 1 | 夜色强度，0…1：入夜后画面偏蓝、降饱和的程度；0 为纯物理颜色（修订 v5） |

- 夜空只在 `sun_mode = location` 时绘制；手动模式下字段保存但不生效。`night_look` 例外：夜色只取决于场景照度，两种太阳模式下都生效（Epic design「夜空」）。
- 亮度倍率和色温与太阳的 `sun_strength`、`sun_temperature_k` 同义（owner 2026-10-22，修订 v4）。
- `stars` 同时控制星星和夜天光（无月晴夜天空本身的微光）。
- 天亮后夜空的亮度自然被天空盖过，不需要另外设开关。
- 其他天空模式下写 `night` 返回 400。

### 访问模式与保证
- **写**：随 `set_environment` 一起写，读取时补齐缺省值。
- **渲染指纹**：随 `sky` 计入 `lit`，不计入 `clay`、`line`。
- **助手的月亮参考**（修订 v4）：物理天空的地点模式下，`read_scene` 和写入了环境的 `edit_scene` 结果里在 `sun` 旁边带 `moon`：当前月亮的视高度和方位（含场景坐标的方位）、月相名称与照亮比例、当天的月出月落、前后最近的满月和新月日期（当地日期）。服务端计算，不存储，不进指纹；`night.moon` 关闭时照样给出（它描述的是天上，不是画面）。

### 生命周期与演进
- 不迁移。W2 交付之后、W3 交付之前存储的物理天空没有 `night`，读取时补齐为缺省值（开关全部 `true`，倍率 1，色温 6500，夜色 1）；只写了部分字段的 `night` 同样逐项补齐。W3 上线后，这些场景的 `lit` 导出各提示一次「场景已更新」（指纹按补齐后的快照计算），与 `data: 空气透视` 相同。
- 回滚同 `data: 物理天空`。

## 空气透视

### 用途与存储角色
- **用途**：扩展物理天空，控制空气透视：远处景物按大气的散射和透射变蓝变淡。
- **存储**：`scene.environment.sky.aerial_perspective`，位于 `environment` 单元内。
- **角色**：权威主数据。空气透视的体积是渲染端的派生量。
- **所有权**：同 `data: 物理天空`。

### 结构与身份

| 路径 | 类型 / 约束 | 缺省 | 说明 |
|---|---|---|---|
| `sky.aerial_perspective` | object | 下列缺省 | 只在 `sky.kind = physical` 时允许，可省略 |
| `sky.aerial_perspective.enabled` | boolean | true | 开关 |
| `sky.aerial_perspective.distance_scale` | number 0.1…1000 | 1 | 计算空气透视时，摄像机到表面的距离乘以这个倍率；调大后小尺度的场景也能看出效果。天空背景不受影响 |

- 与全局雾、体积物件叠加，不互相替代（`scene-world-environment` design「合成顺序」）。
- 其他天空模式下写这个字段返回 400。

### 访问模式与保证
- **写**：随 `set_environment` 一起写，读取时补齐缺省值。
- **渲染指纹**：随 `sky` 计入 `lit`，不计入 `clay`、`line`。

### 生命周期与演进
- 不迁移。W4 交付之前存储的物理天空没有这个字段，读取时补齐为缺省值（开启，倍率 1）。W4 上线后，这些场景出现空气透视；渲染指纹按补齐后的快照计算，因此它们的 `lit` 导出各提示一次「场景已更新」。这是预期行为：画面确实变了。
- 回滚同 `data: 物理天空`。
