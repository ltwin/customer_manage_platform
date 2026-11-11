# Design

## 设计上下文与关键决策

需求和范围见 proposal.md。没有单独的 brainstorm：讨论在 2026-11-10 的会话里完成，拍板记在 proposal「决策记录」。相关的已有设计：

- 资产库的模型资产、材质资产（`data: 模型资产`、`data: 材质资产`）：「一种资产类型 + 一份只属于资产库的内容 + 持有关系表引用图片」。本次照这个做法加 `character`，写入口对照 `backend/internal/creativecontent/content.go:WriteMaterialAndRetain`。
- 画布的生成参考（`backend/internal/creativecanvas/generation_prompt.go`）：参考是上游节点加连线，受理时按 `orderedGenerationRefs` 排序，`@[节点 ID]` 编译成「图 N」。
- `photo.shoot_plan` 的设计草稿里有策划自己的人设章节。策划还没实现；以后由 `cosplay-pipeline` 改成引用角色卡，本次不碰。

方案分三类：

- **采用**：下文各节。
- **待验证假设**：参考图几张、哪些视角、外貌描述怎么写对一致性最有效，双角色同框会不会串。后端开工前由「一致性调查」按事先定好的判据回答（`discovery/research/reference-consistency.md`，见「未决事项」）。
- **后续增强**：助手建草稿与设定图模板（后续 Feature）、3D 模型表示、策划引用与过时提示、分享与导入、市场。

### 角色卡做成资产类型

| 方案 | 结论 | 理由 |
|---|---|---|
| **资产类型 `character`** | 采用 | 资产库的分组、标签、评分、回收站、筛选、查看浮窗、助手的 `search_assets` / `read_asset`、外发授权都直接沿用；模型资产和材质资产已经验证过这条路 |
| 文档类型（`creativedocument`） | 不采用 | 文档类型端口的约定写明「不适用于不挂在画布节点上的业务对象（例如资产库素材）」（`patterns: 文档类型端口`）；文档要求一个画布节点类型映射，也没有不经画布新建文档的入口。单元级并发和逐单元恢复对角色卡是过度设计 |
| 资产库之外的独立表 | 不采用 | 要重做一遍分组、标签、评分、回收站、搜索和助手读取，形成两套素材管理 |

与材质资产的差别：材质资产存入后不再追加修订；角色卡要改，所以「存新版本」在同一份内容上追加修订，资产行改指过去。版本历史就是内容的修订序列（`creative_content_revisions.sequence`），不另建版本表。

### IP：内置目录 + 自建取值，不建 IP 表

| 方案 | 结论 | 理由 |
|---|---|---|
| **内置目录（静态数据）+ 自建 IP 即角色卡里用过的名字** | 采用 | 「选而不是填」由联想解决：内置目录给常见 IP 的标准名和别名，自建 IP 第一次输入后就成为候选。载荷只存名字和全局 `key`，不破坏可导出 |
| 账号 IP 表 + IP 管理页 | 暂不采用 | 改名或合并要牵动所有引用它的角色卡；载荷引用账号记录 ID 会破坏可导出。自建 IP 多到需要管理时再加，已有取值可以迁移过去 |
| 自由文本 | 不采用 | 「原神 / Genshin」分裂，助手按 IP 找不全；也分不开同 IP 下的不同作品 |
| 用资产库标签表示 IP | 不采用 | 标签是账号自己的整理方式，不进版本、不随导出；IP 是角色身份的一部分 |

内置目录做法同内置贴图库：随代码发布、经代码评审维护，不进数据库。作品也带别名（「FGO」），写入时归一到目录作品名，过滤时按别名解析。

### 不做版权与授权限制

二创是摄影师的常见需求，主流模型一般也不拒绝 IP 类创作；加一层版权确认只会打击创作热情（owner 2026-11-10）。所以：

- 来源字段（`origin`、`attribution`、`source_urls`）全部可选，只是备注；没有「作品衍生必须填 IP」之类的规则，服务端不核实。
- 不新增任何按用途的确认、选图提示或授权步骤。建卡时对 `views` 调用生成受理本来就有的检查（`creativecontent.RequireGenerationReferenceRow`），只是把同一检查提前，保证不会出现「建卡成功、生成时才失败」。创意空间上传的图默认是摄影师自有素材（`creativecontent.RightsDeclarationInput.OrDefault`），界面不问来源，所以这一检查对摄影师不可见。
- 「仅限虚构角色」是范围决定，不是版权限制，保留为界面上的一句说明。

### 一致性只靠参考图和外貌描述

- 不做 LoRA：当前主力模型 `gpt-image-2.5` 系列和 `nano-banana` 都不支持；训练需要额外的算力和工作流，与「不引入 ComfyUI」一致。
- 参考图上限沿用生图路由的策略：aicodewith 和 RunningHub 目前都是 9 张（`backend/internal/platform/llmgateway/generation_aicodewith_policy.go`、`generation_runninghub_policy.go`）。展开后按当前所选模型的上限判断，不写死 9。

## 领域模型与业务规则

### 概念

| 概念 | 说明 |
|---|---|
| 角色卡 | 一个虚构角色可复用的视觉身份。资产类型 `character`，有标题、分组、标签、评分、回收站，与其他资产相同 |
| 角色卡版本 | 角色卡内容的一个修订（`ccrv_…`），不可变。资产行指向当前版本 |
| 参考图 `views` | 生成时真正交给模型的图，1–4 张，有顺序，每张标视角。第一张是封面 |
| 原始素材 `sources` | 建卡时依据的原图，0–12 张，只用于追溯和以后重新生成参考图，**不交给生图模型** |
| 外貌描述 `appearance` | 稳定外貌特征的文字，生成时附在提示词后面 |
| 身份字段 | 角色名 `name`、别名 `aliases`、版本说明 `variant`、所属 IP `ip`、作品 `work`。决定「挑哪张卡」，给人和助手看；不交给生图模型（角色名除外，它在提示词里指代参考图） |
| 所属 IP | 角色所属的系列。来自内置目录时带稳定 `key`，自建时只有名字 |
| 作品 | 同一 IP 下的具体作品。同名角色在不同作品里设定不同，靠它区分 |
| 版本说明 | 同一角色不同造型的标签（常服、第三灵基），不是层级 |
| 来源备注 | `origin`（原创 / 作品衍生）、来源说明、来源页面，全部可选，只做展示 |
| 内置角色 IP 目录 | 随代码发布的常见 IP 名单（名称、别名、作品），账号不能修改 |
| 角色卡节点 | 画布节点 `core.character`，内容是某个角色卡版本，只能作为生成节点的参考 |

字段的约束、取值范围和示例见 `contracts.md`「data: 角色卡」。

### 不变量

- **I1 版本不可变**：版本写入后不再修改。修改角色卡 = 在同一份内容上追加修订，资产行改指新修订。
- **I2 可导出**：版本载荷里只有文字、本账号的图片修订 ID（`kind = image`）和全局 IP 目录 `key`，不含画布、节点、项目、文档、资产 ID。图片修订和 blob 一次写入不再改，所以「载荷 + 图片原图」就是一个版本的全部内容。
- **I3 参考图可用于生成**：`views` 里的每张图在建卡和存新版本时都通过生成参考检查，生成受理和派发时照常再复核。来源字段只是备注，不参与任何校验。
- **I4 IP 与作品写入时归一**：写入时把 IP 名字归一到内置目录（命中名称或别名就补 `key`、改写为显示名），有 `key` 时作品按该 IP 的作品别名归一。载荷存名字快照加可选 `key`，之后目录改名或扩充不回写角色卡，过滤靠名字兼容规则覆盖旧卡。
- **I5 只有虚构角色**：`origin` 没有真人选项，界面写明「仅限虚构角色」。服务端无法判断图片是不是真人，靠声明和界面约束。
- **I6 一份内容只对应一个资产行**：只有建卡接口为 `character` 内容建资产行；按修订存入资产库的通用入口（`SaveRevisionInTx`、`CreateFromRevisionInTx`）拒绝 `character`。资产行持有当前版本，旧版本由引用它的画布节点、生成记录各自持有，没人持有时按 GC 回收。
- **I7 节点停在版本上**：角色卡节点的内容是一个具体版本；角色卡出了新版本，节点不跟着变，只提示，由摄影师更新。
- **I8 账号隔离**（ADR-001）：角色卡、版本、持有关系都带 `account_id`，查询由 repository 基座限定。

## 模块职责与协作

沿用现有分层和依赖方向（`httpapi` → 领域包 → `creativecontent` / `platform/store`），不新增包。

| 模块 | 增量职责 | 不职责 |
|---|---|---|
| `creativecontent` | `character` 内容种类；写入口 `WriteCharacterAndRetain`：校验载荷、复核每张图片（本账号、`image`、`ready`；`views` 内的过生成参考检查，`sources` 内的可展示即可）、写修订和持有关系行（`role = character`），同一事务 | 资产库的元数据和归类 |
| `creativelibrary` | 新建角色卡（建内容 + 资产行）、存新版本（追加修订 + 资产改指 + `revision` +1）、标题由角色名和版本说明生成、搜索投影与 IP / 作品过滤；内置 IP 目录、IP 归一、IP 与作品联想 | 校验图片、生成 |
| `creativecanvas` | 节点类型 `core.character`；读画布时算「有新版本」；带事务的展开步骤 `expandCharacterRefsInTx`，被预检读草稿、锁内受理、提示词编译共用；执行输入关系写 `role = character` 行，派发复核和衍生权利跳过它；溯源快照写 `characters[]` | 资产库写入 |
| `creativereference` | 登记 `role = character` 的持有关系，GC 按 `material` 的规则处理 | — |
| `creativeagent` | `search_assets` / `read_asset` 的角色卡要点和过滤；`read_asset` 多图附件（内置 `runReadAccess` 与外部 `ExternalTools.readAsset` 都要支持）；`edit_canvas` 放置角色卡节点；`content_ref` 引用角色卡 | 角色卡的存储规则、授权模型（沿用） |
| `creativegeneration` | 无代码改动：`prepareImage` 经 `GenerationDraftInTx` 拿到的已是展开后的图片，路由策略照常判断张数、大小、提示词长度 | — |
| `creativemcp` | 无代码改动：外部只读工具共用内置工具的 `Info()` | — |
| `platform/httpapi` | 三个新接口的适配和错误映射 | 业务规则 |
| 前端 `creative-canvas/assets/` | 资产库「角色卡」视图、类型和 IP / 作品筛选、角色卡卡片、新建与编辑对话框（IP、作品联想）、多选图片「创建角色卡」 | — |
| 前端 `creative-canvas/viewer/` | 查看浮窗的角色卡版面（参考图带视角、外貌描述、IP · 作品 · 版本说明、来源备注、原始素材） | — |
| 前端 `creative-canvas/editor/` | 角色卡节点、新版本提示与更新、画布图片节点的「创建角色卡」、@ 菜单和连线对角色卡的支持 | — |

主流程：

```
建卡
  资产库 / 画布 ──POST /creative/assets/characters──▶ httpapi
    ──▶ creativelibrary.CreateCharacter                         （同一事务）
          ├─ IP 与作品归一（内置目录）
          ├─ creativecontent.WriteCharacterAndRetain  views 过生成参考检查；内容 + 修订 + 图片持有，retain = 新资产行
          └─ 资产行 + 归类 + 搜索投影 + library_revision+1

生成
  路由预检  creativegeneration.prepareImage
    └─ creativecanvas.GenerationDraftInTx ─┐
  锁内受理  creativecanvas（generation.go）  ├─ orderedGenerationRefs（纯图）→ expandCharacterRefsInTx（读角色卡版本）
  提示词编译 CompileGenerationPromptInTx   ─┘     → 有序图片 + 编译后的提示词 + 角色卡清单（三处相同）
  预检：路由策略判断张数、大小、提示词长度
  受理：逐项比对预检结果；执行输入关系 = 图片（按发送顺序）+ 每个角色卡一行 role=character
  派发：复核图片（跳过 role=character）
  发布：输入关系整体复制到结果版本 → 结果版本持有角色卡版本
```

失败责任：建卡和存新版本在一个事务里完成，任何一步失败都不留下内容、修订或资产行。展开失败（超限、图片不可用）时生成不受理，不产生生成记录和计费。

## 关键机制

### 存新版本

```
摄影师保存（expected_revision = 资产行 revision）
  锁资产行（FOR UPDATE）
  ├─ 在回收站 → 409 creative_asset_trashed
  ├─ revision 不符 → 409 creative_revision_conflict
  ├─ IP 归一后，新载荷与当前版本 JSON 规范化相等 → 不写，返回当前版本，reused = true
  └─ WriteCharacterAndRetain(content_id = 资产的内容)  追加修订 + 图片持有
     资产行 content_revision_id = 新修订，title = 角色名 · 版本说明，revision + 1
     搜索投影更新，library_revision + 1
```

旧版本的资产持有随资产行改指而解除；它的图片持有关系行随旧修订存在，旧修订被 GC 回收时级联删除（`data: 材质资产`「GC」，不递归）。

### 角色卡节点的新版本提示

读画布时，对画布里全部 `core.character` 节点批量查：用节点内容修订所属的 `content_id` 找不在回收站的 `character` 资产；资产的 `content_revision_id` 与节点不同，就在节点视图里给出 `latest_revision_id`。找不到资产（在回收站或已彻底删除）就不提示。更新 = 画布命令 `update_character` 换节点内容修订，产生节点版本，可撤销。I6 保证按 `content_id` 最多找到一个资产行。

### 展开为生成参考

规则在 `patterns: 角色卡展开为生成参考`。设计要点：

- **在哪里展开**：`orderedGenerationRefs` 是纯图计算、没有事务，读不到角色卡载荷，所以展开不能放进它。新增 `expandCharacterRefsInTx(ctx, tx, refs)`：对 `kind = character` 的参考读版本载荷，就地换成 `views`，返回展开后的参考和角色卡清单（节点、版本、名字、对应的图片区间）。`GenerationDraftInTx`、锁内受理、`CompileGenerationPromptInTx` 三处都在 `orderedGenerationRefs` 之后调用它，再交给 `compileGraphPrompt`。
- **提示词编译**：`CompileGenerationPromptInTx` 和 `compileGraphPrompt` 现在没有 `@` 时直接返回原文，要改成「没有 `@` 但有角色卡时」仍追加「角色设定」段；不带角色卡时保持原样提前返回，编译结果与改动前逐字节相同。编号沿用现有的 `[图N]` 和「参考编号说明」。
- **为什么预检之前就要展开**：预检用草稿里的参考读图片事实、调路由策略；如果草稿里还是角色卡修订，策略拿到的是一个没有图片对象的修订，张数和大小都判断不了。展开后，张数、大小、提示词长度由实际路由策略判断，不在 `creativecanvas` 里写死上限。
- **拒绝的定位**：策略拒绝或图片不可用时，用角色卡清单把出问题的图片映射回「哪张角色卡的第几张图」，写进错误详情和日志。
- **读集**：仍记原来的角色卡节点（它的 `data_revision` 变化会让受理冲突），不把展开出的图片当成节点。

### 角色卡版本的持有

现有 GC 只认关系行（`creativereference.LiveOwnerCondition`），JSON 快照里的 ID 不算持有；发布结果时只把执行的输入关系复制给结果版本（`creativecanvas/publish.go`）。所以：

| 阶段 | 持有方 | 持有什么 |
|---|---|---|
| 画布上 | 角色卡节点 | 角色卡版本（节点已在 `LiveOwnerCondition` 里） |
| 执行中、执行保留期内 | 执行输入关系 `creative_execution_refs` | 展开后的图片 + `role = character` 的角色卡版本 |
| 结果发布后 | 结果版本的输入关系 `creative_node_version_input_refs` | 同上，整体复制 |

- `role = character` 的行不发给模型；派发前的图片复核（`generation.go` 里按输入关系逐个复核）和衍生权利计算（`generationSourceIdentities`）都跳过它，图片已经逐张在列。
- 现有「输入最多 50 项」只算图片；角色卡行另计，最多 8 行。读输入关系的分页上限相应放大。
- 验证路径：生成 → 删除角色卡节点 → 彻底删除角色卡资产 → 执行 GC → 结果版本的溯源仍能读出角色卡版本载荷和图片。

### IP 归一与联想

```
写入（建卡 / 存新版本）
  ip.key 有值 ──▶ 在目录里？ ──否──▶ 400 validation_failed
                      └─是──▶ name := 目录显示名
  ip.key 无值 ──▶ 规范化 name（NFKC、小写、去首尾空白、空白合一）
                  ├─ 唯一命中目录名称或别名 ──▶ 补 key，name := 显示名
                  └─ 未命中或命中多个 ──▶ 自建，name 原样
  有 key 且有 work ──▶ 规范化后命中该 IP 的作品名或别名 ──▶ work := 目录作品名

过滤（资产列表 / search_assets）
  character_ip 参数 ──按写入规则解析──▶ 内置 K：key = K 或 原名 ∈ {K 的显示名, K 的别名}
                                     └─ 自建：原名相等
  character_work 参数 ──规范化──▶ 有内置 IP：按它的作品别名解析
                               └─ 无 IP：在全目录作品里找，唯一命中就解析，多个命中返回候选
                      ──▶ 与投影作品名相等

联想（GET /creative/character-ips）
  候选 = 目录 IP ∪ 本账号角色卡当前版本里出现过的 IP（按归一结果去重）
  排序 = 本账号用过的按张数降序在前，其余按目录顺序
  作品候选 = 目录作品 ∪ 本账号该 IP 下用过的作品
```

过滤（资产列表、`search_assets`）读搜索投影里从当前版本派生的 IP 和作品值，不扫载荷；投影与资产 `revision` 在同一事务里更新。

### 可导出（为分享与市场预留）

- 导出一个版本 = 载荷 + `views` 与 `sources` 的原图。`ip.key` 是全局目录标识，导入方照常能解析。
- 导入时（远期）为每张图写本账号的新图片修订，替换载荷里的 ID，再按建卡写新内容；跨账号永远是复制，不共享修订，ADR-001 不开例外。
- 本次不建任何导出、导入、发布相关的表、字段或接口；载荷的 `schema_version` 留出以后加字段的余地。

## 接口与数据契约

- 契约状态：扩展。
- 契约引用与变更：全部定义在本 Feature `contracts.md`。
  - 新接口：`api: 新建角色卡`、`api: 角色卡存新版本`（沿用资产库公共请求外层、`Idempotency-Key`、`CreativeAssetCreateResult`），`api: 查找角色 IP`（只读）。
  - 已有资产接口不新增路径：`kind` 枚举加 `character`，资产列表加 `character_ip`、`character_work` 过滤；元数据接口对角色卡不接受 `title`（标题由角色名和版本说明生成）。
  - 助手侧是工具契约扩展：`api: 助手读取与使用角色卡`。
  - 数据：`data: 角色卡`、`data: 内置角色 IP 目录`、`data: 角色卡节点`；规则：`patterns: 角色卡展开为生成参考`。
  - OpenAPI 改动后运行 `make generate`，提交 Go / TS 两份生成物。

### 数据与迁移

- 迁移 `0082`（以实施时最新号为准）：`creative_contents.kind`、`creative_assets.kind` 的 CHECK 加 `character`；`creative_content_revision_refs.role` 的 CHECK 加 `character`。节点类型 `core.character` 在 `creativecanvas/definitions.go` 注册，`creative_nodes.type_key` 没有数据库约束（0040 已去掉），不需要迁移。
- 不建新表。版本是内容修订；图片持有复用 `creative_content_revision_refs`；IP 与作品过滤值放进现有的 `creative_asset_search`（这张表没有 `kind` 列，`kind` 仍在 `creative_assets` 上过滤）：加三列，都可为 null，只有角色卡有值：
  - `character_ip_key text`：内置目录 `key`，自建 IP 为 null。
  - `character_ip_name text`：规范化后的 IP 名（载荷里的 `ip.name`）。
  - `character_work text`：规范化后的作品名。
  - 索引：`(account_id, character_ip_key) WHERE character_ip_key IS NOT NULL`、`(account_id, character_ip_name) WHERE character_ip_name IS NOT NULL`、`(account_id, character_work) WHERE character_work IS NOT NULL`。
  - 存原名是为了目录扩充：自建 IP 后来被目录收录时，查询按新目录解析成 `key`，同时用 `key` 的显示名和别名去匹配 `character_ip_name`，旧卡不用重建投影也能命中。
- GC：`character` 加入可回收的内容种类，规则同 `material`。
- 回滚：已有 `character` 内容、资产或持有关系行时拒绝回滚，前向修复（同材质资产）。

## 挂载点

| 位置 | 动作 | 说明 |
|---|---|---|
| `backend/internal/platform/store/migrations/0082_creative_character_cards.up.sql` | 新增 | 三处 CHECK 加 `character`；搜索投影加三列和按非空条件的部分索引 |
| `backend/internal/platform/store/migrations/0082_creative_character_cards.down.sql` | 新增 | 有角色卡数据时拒绝回滚 |
| `backend/internal/creativecontent/content.go` | 修改 | `character` 内容种类、`WriteCharacterAndRetain` |
| `backend/internal/creativecontent/character.go` | 新增 | 角色卡载荷类型、校验、规范化比较 |
| `backend/internal/creativelibrary/character.go` | 新增 | `CreateCharacter`、`SaveCharacterRevision`、标题生成、搜索投影写入 |
| `backend/internal/creativelibrary/character_ips.go` | 新增 | 内置目录加载、IP 归一、联想 |
| `backend/internal/creativelibrary/character_ips.json` | 新增 | 内置角色 IP 目录（首批名单 owner 过目） |
| `backend/internal/creativelibrary/service.go:CreateFromRevisionInTx` | 修改 | 拒绝 `character` |
| `backend/internal/creativelibrary/service.go:SaveRevisionInTx` | 修改 | 拒绝 `character` |
| `backend/internal/creativelibrary/list.go` | 修改 | `character_ip`、`character_work` 过滤，列表返回角色卡载荷要点 |
| `backend/internal/creativereference/projection.go` | 修改 | 登记 `role = character` |
| `backend/internal/creativecanvas/definitions.go` | 修改 | 注册 `core.character` |
| `backend/internal/creativecanvas/character_refs.go` | 新增 | `expandCharacterRefsInTx`：展开、角色卡清单、拒绝时的定位 |
| `backend/internal/creativecanvas/generation_prompt.go:GenerationDraftInTx` | 修改 | 预检读草稿时展开 |
| `backend/internal/creativecanvas/generation_prompt.go:CompileGenerationPromptInTx` | 修改 | 展开；没有 `@` 但有角色卡时也编译 |
| `backend/internal/creativecanvas/generation_prompt.go:compileGraphPrompt` | 修改 | 角色卡的 `@` 编译、「角色设定」段 |
| `backend/internal/creativecanvas/generation.go` | 修改 | 锁内受理展开后比对；执行输入关系写 `role = character` 行；溯源 `characters[]`；派发复核与 `generationSourceIdentities` 跳过 `role = character` |
| `backend/internal/creativecanvas/execution.go` | 修改 | 读取执行快照时带出 `characters[]` |
| `backend/internal/creativeagent/library_tools.go` | 修改 | `search_assets` 过滤与要点 |
| `backend/internal/creativeagent/context.go:readAssetResult` | 修改 | 读角色卡版本，返回多张 `views` 图片 |
| `backend/internal/creativeagent/external_read.go:readAsset` | 修改 | 外部多图附件、逐张省略 |
| `backend/internal/creativeagent/canvas_tools.go` | 修改 | `add_node` 放置角色卡节点 |
| `backend/internal/platform/httpapi/creative_media.go` | 修改 | 注册三个新路由 |
| `backend/internal/platform/httpapi/creative_characters.go` | 新增 | 三个接口的适配与错误映射 |
| `api/openapi.yaml` | 修改 | 新接口、`kind` 枚举、列表过滤 |
| `frontend/src/creative-canvas/assets/AssetBrowser.tsx` | 修改 | 角色卡视图（标题下显示 IP · 作品 · 版本说明）、类型与 IP / 作品筛选、多选图片「创建角色卡」 |
| `frontend/src/creative-canvas/assets/CharacterDialog.tsx` | 新增 | 新建与编辑角色卡对话框、IP / 作品联想 |
| `frontend/src/creative-canvas/viewer/AssetInfoPanel.tsx` | 修改 | 角色卡版面 |
| `frontend/src/creative-canvas/editor/CanvasView.tsx` | 修改 | 角色卡节点渲染、新版本提示与更新 |
| `frontend/src/creative-canvas/editor/CanvasContextMenu.tsx` | 修改 | 图片节点「创建角色卡」 |
| `frontend/src/creative-canvas/editor/assetPlacement.ts` | 修改 | 角色卡放入画布、选参考 |
| `frontend/src/creative-canvas/editor/agentReferences.ts` | 修改 | @ 菜单引用角色卡 |

## 验证与交付

| 验证对象 | 方法 | 完成判据 |
|---|---|---|
| I2 可导出、图片复核 | `creativecontent` 单测 | 拒绝非图片、他账号、未就绪的修订 ID，错误指出哪一张；载荷里没有图片以外的 ID |
| I3 参考图可用于生成 | `creativecontent` 单测 | 不能用作生成参考的图放进 `views` 被拒绝并指出是哪张；同一张图放进 `sources` 可以保存 |
| I4 IP 与作品归一 | `creativelibrary` 单测 + 目录构建期测试 | 别名、大小写、全半角归到同一 `key`；命中两个 IP 时不补 `key`；「FGO」归一为 Fate/Grand Order；目录 `key` 唯一、退役不删、名称与别名无冲突 |
| 过滤与目录扩充 | `creativelibrary` 测试 | 作品简称、全称都能筛到；简称在多个 IP 下都有时返回候选；先以自建 IP 建卡、再把该名字加进目录，用同一个名字筛选前后结果相同 |
| I6 唯一资产行 | `creativelibrary` 单测 | `SaveRevisionInTx`、`CreateFromRevisionInTx` 拒绝 `character` |
| 存新版本 | `creativelibrary` + httpapi 测试 | 并发保存一个 409；无变化 `reused = true`；回收站中 409 |
| I7 新版本提示 | `creativecanvas` 测试 | 存新版本后节点内容不变，读画布给出 `latest_revision_id`；`update_character` 可撤销 |
| 展开 | `creativecanvas` + `creativegeneration` 测试 | 预检、锁内受理、编译三处得到相同的有序图片和提示词；没有 `@` 时也追加「角色设定」；路由策略超限拒绝时详情指到角色卡；**不带角色卡时编译结果与改动前相同** |
| 持有与 GC | `creativereference` 守卫测试 `projection_roles_test.go` + GC 测试 | 资产改指后旧版本被节点持有时不回收；生成 → 删除节点和资产 → GC → 结果溯源仍能读出角色卡载荷；派发复核跳过 `role = character` |
| 助手工具 | `creativeagent` 测试 | 过滤参数校验与作品歧义候选、要点字段、`read_asset` 按 `views` 顺序附多图且只接受该版本的图、单张压缩失败只省略这一张、`add_node` 建 `core.character` |
| 端到端 | 按 proposal 验收走一遍 | 生成相关的用一个真实模型、少量张数；e2e 一次一个用例，换端口避开 owner 的 Vite |

门禁：日常 `make check-go PKG=...`、`make check-frontend`；合回 `develop` 用 `make check-merge`。

交付顺序：

1. 一致性调查（不写产品代码）。
2. 后端存储与接口。
3. 资产库前端。
4. 画布节点与生成展开。
5. 助手工具。

前后端同一版本上线，没有半成品入口。

观测：生成受理拒绝（超限、图片不可用）的日志带角色卡内容 ID 和展开张数。

## 未决事项

| 问题 / 假设 | 影响 | 解决方式 | 最晚解决时点 |
|---|---|---|---|
| 参考图几张、哪些视角、单张拼版还是分开的图最稳；外貌描述怎么写有效；两个角色同框会不会串 | `data: 角色卡` 的 `views` 上限与视角枚举、`patterns: 角色卡展开为生成参考` 第 3、4 条 | 一致性调查，按 `discovery/research/reference-consistency.md` 事先定好的基线和采用判据；结论与本方案不一致时 `lti revise` 改设计和契约 | 交付顺序第 1 步，后端存储开工前 |
| 内置 IP 目录首批收哪些 | `character_ips.json` 的内容，不影响结构 | 实施时整理名单，owner 过目 | 验收前 |
| 自建 IP 多到需要改名、合并 | 是否加 IP 管理页 | 观察使用；需要时另立 change | 不阻塞本次 |
| 真人照片冒充虚构角色 | I5 | 首版只做界面说明；做分享或市场时再评估 | 分享与导入立项时 |
