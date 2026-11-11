<!-- 本 Feature 的目标契约，批准前为草稿，不代表项目当前已实现。 -->

## glossary: ADD 角色卡

- 定义：一个虚构角色可复用的视觉身份，存在资产库里。由 1–4 张参考图和一段外貌描述组成，可以附上所属 IP、作品和来源备注，生成图片时作为稳定参考。有版本，修改即产生新版本。
- 不是：一张普通图片资产（角色卡引用图片，自己不是图片）；不是策划文档里的「人设」（策划的人设以后引用角色卡）；不是真人（只支持虚构角色）；不是造型（同一角色换装是另一张角色卡）。
- 边界场景：同一角色的常服和战斗服是两张角色卡，用「版本说明」区分（「芙宁娜 · 常服」「芙宁娜 · 泳装」），不是一张卡下的两个造型。
- 拼写：代码 `character`；资产 `kind = "character"`；节点 `core.character`；界面「角色卡」；字段 `views`（界面「参考图」）、`sources`（界面「原始素材」）、`appearance`（界面「外貌描述」）、`ip`（界面「所属 IP」）、`work`（界面「作品」）、`variant`（界面「版本说明」）、`aliases`（界面「别名」）。

## glossary: ADD 角色 IP

- 定义：角色所属的知识产权系列，如「原神」「Fate」「崩坏」。用于按系列查找、筛选角色卡。可以来自内置目录，也可以是账号自己输入的名字（例如摄影师自己的原创系列）。
- 不是：作品。一个 IP 可以有多部作品（Fate 下有 stay night、Zero、FGO），同名角色在不同作品里设定可能不同，靠「作品」区分；IP 和作品相同时（原神）不重复填作品。也不是账号里的一条可管理记录：首版没有 IP 管理页，账号自建的 IP 就是角色卡里用过的名字。
- 边界场景：「FGO 里的阿尔托莉雅，第三灵基」= IP「Fate」、作品「Fate/Grand Order」（「FGO」是它的作品别名）、版本说明「第三灵基」。「原神」「Genshin」「Genshin Impact」都匹配内置目录里的同一个 IP。
- 拼写：代码与载荷 `ip`（`{name, key?}`），内置目录的稳定标识 `key`（如 `genshin-impact`）；界面「所属 IP」。

## architecture: ADD 角色卡

### 职责与边界
- 角色卡是资产库的一种资产（`kind = character`），做法同模型资产、材质资产：资产行 + 一份只属于资产库的 `character` 内容 + `creative_content_revision_refs` 持有其中的图片。与材质资产不同，角色卡可以追加修订，资产行指向当前版本。
- `backend/internal/creativecontent`：`character` 内容种类、写入口 `WriteCharacterAndRetain`（校验载荷、复核图片、写修订和持有关系，同一事务）。
- `backend/internal/creativelibrary`：新建角色卡、存新版本、标题跟随角色名与版本说明、搜索索引；内置角色 IP 目录（随代码发布的静态数据）、IP 匹配与联想。按修订存入的通用入口（`SaveRevisionInTx`、`CreateFromRevisionInTx`）不接受 `character`。
- `backend/internal/creativecanvas`：节点类型 `core.character`、新版本提示、生成受理时展开（`patterns: 角色卡展开为生成参考`）。
- `backend/internal/creativeagent`：角色卡的读取和放置只经上述领域入口，不直接写表；首版助手不写角色卡。
- 不职责：不做版权或授权限制，来源字段只是备注，服务端不核实；不判断图片里是不是真人；不提供导出、导入、发布、跨账号读取。
- 依赖方向不变：`httpapi` → `creativelibrary` / `creativecanvas` / `creativeagent` → `creativecontent` → `platform/store`。

### 关键数据流
- 建卡：资产库或画布 → `POST /creative/assets/characters` → `creativelibrary` → `WriteCharacterAndRetain`（retain = 新资产行）→ 资产行、归类、搜索索引 → `library_revision` +1，经现有账号失效通道通知其他页面。
- 存新版本：`POST /creative/assets/{id}/character-revisions` → 锁资产行 → 追加修订 → 资产改指 → 同上通知。

### 长期约束
- 角色卡版本的载荷只含文字、本账号的图片修订 ID 和全局的内置目录 `key`（可导出不变量）。以后的分享、导入、市场都以「复制进本账号」实现，不跨账号共享修订或资产。

## data: ADD 角色卡

### 用途与存储角色
- **用途**：资产库里的角色卡及其版本历史。存角色名、外貌描述、参考图、原始素材、身份字段和来源备注；不存造型层级、3D 模型、使用记录（资产库不记录被哪个画布用过，沿用资产库现有约定）。
- **存储**：PostgreSQL，关系型 + 修订载荷 jsonb。不建新表：
  - `creative_contents.kind`、`creative_assets.kind` 的 CHECK 加 `character`。
  - `creative_content_revision_refs.role` 的 CHECK 加 `character`。
- **角色**：内容修订载荷是权威主数据；资产行是它在资产库的入口，`content_revision_id` 指向当前版本；持有关系行和搜索索引是从载荷派生的，与修订同一事务写入。
- **所有权**：只有 `api: 新建角色卡`、`api: 角色卡存新版本` 经 `creativecontent.WriteCharacterAndRetain` 写角色卡内容和资产。场景命令、画布命令、通用内容写入口、按修订存入资产库的入口都写不出 `character`。

### 结构与身份
- 内容 `ccnt_…`、修订 `ccrv_…`、资产 `ccas_…`，沿用现有前缀。一份角色卡 = 一个内容 + 一个资产行；版本 = 该内容的修订，按 `sequence` 递增。
- 修订载荷（`creative_content_revisions.payload`，`schema_version = 1`）：

| 路径 | 类型 | 必须 | 缺省 / 约束 |
|---|---|---|---|
| `character.name` | string | 是 | 1–60 个 Unicode 码点，去掉首尾空白后非空。用于提示词和资产标题 |
| `character.appearance` | string | 是 | 1–2000 码点。稳定的外貌特征，生成时附在提示词后 |
| `character.views` | array | 是 | 1–4 项，有顺序，第一项是封面。元素见下表；同一图片修订不能出现两次 |
| `character.sources` | array | 是 | 0–12 项，元素 `{content_revision_id}`；可以为空数组。不交给生图模型 |
| `character.aliases` | string[] | 否 | ≤ 10 项，每项 1–60 码点，规范化后互不相同，也不等于 `name`。只用于搜索和助手识别，不进提示词。缺省等同空数组 |
| `character.variant` | string | 否 | 版本说明，1–40 码点，例如「常服」「第三灵基」。缺省表示没有 |
| `character.origin` | string | 否 | `original`（原创）或 `derivative`（作品衍生），只用于展示和以后的市场分类，不影响任何校验。没有真人选项。缺省表示未说明 |
| `character.ip` | object | 否 | 所属 IP，原创和衍生都可以填（原创时是自己的系列名）。结构见下表 |
| `character.work` | string | 否 | 作品，1–100 码点。有 `ip` 时才允许；IP 和作品相同时不填。写入时按「作品归一」处理 |
| `character.attribution` | string | 否 | ≤ 2000 码点，来源备注（出处、画师、授权说明等），服务端只保存和显示。缺省表示没有填写 |
| `character.source_urls` | string[] | 否 | ≤ 10 项，每项 ≤ 2048，只允许 `http`、`https`，作用同来源备注。缺省等同空数组 |

`character.views[]` 元素：

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `content_revision_id` | string `ccrv_…` | 是 | 本账号、`kind = image`、`state = ready`，并且能用作生成参考的图片修订：建卡时用生成受理同一个检查（`creativecontent.RequireGenerationReferenceRow`），保证「建卡成功、生成时才发现这张图不能用」不会发生。创意空间上传的图默认就满足，摄影师看不到任何额外步骤 |
| `view` | string | 是 | `front` 正面、`three_quarter` 四分之三侧、`side` 侧面、`back` 背面、`face` 脸部特写、`full_body` 全身、`detail` 细节、`other` 其他。枚举可能随一致性调查调整（design「未决事项」） |
| `note` | string | 否 | ≤ 200 码点，例如「左手的手环」 |

`character.ip`：

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `name` | string | 是 | 1–60 码点。总是存名字本身，导出后不依赖任何目录也能看懂 |
| `key` | string | 否 | 内置角色 IP 目录的稳定标识（`data: 内置角色 IP 目录`）。有 `key` 时 `name` 是目录里该 IP 的显示名 |

**IP 归一**（建卡、存新版本时由服务端执行，结果写进载荷）：
1. 请求带 `key`：`key` 必须在目录里，服务端把 `name` 改写为目录显示名。
2. 请求不带 `key`：`name` 规范化后（NFKC、转小写、去首尾空白、连续空白合一）与目录某个 IP 的显示名或别名相同，就补上 `key` 并改写 `name`；同时命中多个 IP 时不补，按自建处理。
3. 都不命中：按自建 IP 保存，`name` 原样（只去首尾空白）。
4. **作品归一**：有 `ip.key`、`work` 规范化后与该 IP 在目录里某部作品的名称或别名相同时，改写为目录里的作品名（「FGO」→「Fate/Grand Order」）；否则原样保存（只去首尾空白）。联想由 `api: 查找角色 IP` 提供。

**载荷不引用账号里的任何记录**：`key` 是随代码发布、全局稳定的目录标识，不是账号数据，导出到其他账号仍然有效（可导出不变量）。

- 对象：角色卡修订自己没有 `creative_content_objects`。封面、缩略图都取 `views[0]` 那张图片的对象，经现有媒体读取票据读取。
- 持有关系：写修订的同一事务里，`views` 和 `sources` 中每张不同的图片各写一行 `creative_content_revision_refs`（`holder_revision_id` = 角色卡修订，`role = character`）。同一张图同时在 `views` 和 `sources` 里只写一行。
- 资产行：`kind = character`，`title` 由当前版本生成（建卡和存新版本时同步）：有 `variant` 时为 `<name> · <variant>`，否则为 `<name>`。标题保持简短，**不靠标题区分同名角色**：资产列表、选参考的候选、助手的候选都另外显示 IP、作品和版本说明（见下方「列表与筛选」的 `character` 要点）。`description`、收藏、评分、分组、标签照常。`source_url` 不使用（来源页面在载荷的 `source_urls` 里），保持 null。

### 访问模式与保证
- **建卡**：一个事务内校验载荷 → 复核每张图片 → 写内容和第 1 个修订 → 写持有关系 → 建资产行、归类、搜索索引 → `library_revision` +1。任一步失败全部不生效。不查重：内容相同的两次建卡是两张角色卡（同导入模型）。
- **存新版本**：锁资产行 → 检查回收站和 `revision` → 新载荷与当前版本规范化后 JSON 相等则不写 → 否则在同一内容上追加修订、写持有关系 → 资产行改指、`title` 同步、`revision` +1 → 搜索索引、`library_revision` +1。
- **列表与筛选**：资产列表的 `kind` 过滤加 `character`；按画面的筛选（主色、画幅）只作用于图片，角色卡不命中。角色卡另有两个过滤：
  - `character_ip`：参数先按「IP 归一」解析。解析成内置 `key` K 时，命中「投影的 key 等于 K」**或**「投影里规范化后的 IP 名等于 K 的显示名或任一别名」——后一条让目录后来收录的自建 IP 不用重建投影也能筛到旧卡。解析不到目录时，按规范化后的 IP 名相等匹配。
  - `character_work`：参数规范化后，如果给了 `character_ip` 且解析到内置 IP，再按该 IP 的作品别名解析成作品名；没给 `character_ip` 时，在全部内置 IP 的作品名和别名里找，唯一命中就解析，命中多部作品时返回 400 `validation_failed`，`details.candidates` 列出候选 `{ip, work}`。最后与投影里规范化后的作品名比较，同样兼容目录收录前写入的旧值。
  - 匹配用的值从当前版本载荷派生，存进资产的搜索投影（`creative_asset_search`，与资产 `revision` 同步更新），不扫载荷。投影列的形状见 design.md「数据与迁移」。
  - 资产列表的每个角色卡项附只读要点 `character: {name, variant?, ip?, work?, view_count}`，界面用它在标题下面显示「IP · 作品 · 版本说明」。
- **搜索**：资产搜索索引收录 `name`、`aliases`、`variant`、`ip.name`（内置 IP 再加目录别名）、`work`（内置作品再加作品别名）、`appearance`。
- **读取**：资产读取返回当前版本的完整载荷；旧版本按内容修订读取（`GET /creative/content-revisions/{id}`）。资产库首版不提供版本历史界面。
- **GC**：
  - 资产行是当前版本的持有方（`LiveOwnerCondition` 已有的资产条件，含回收站中的资产）。
  - 持有关系行在持有方修订 `state` 不是 `deleted` 时持有图片（`data: 材质资产` 已加的条件，不递归）。
  - `character` 加入可回收的内容种类。旧版本没有任何持有方（资产已改指、没有节点或记录引用）时回收，持有关系行级联删除，下一轮再回收不再被持有的图片。
  - 守卫测试 `projection_roles_test.go` 登记 `role = character`。
- **保证方**：
  - 数据库：账号范围、主键唯一、两处 kind CHECK、`role` CHECK、持有关系外键。
  - 应用：载荷形状、图片可用性、持有关系与载荷一致、IP 与作品归一、资产 `title` 与当前版本一致、搜索投影与当前版本一致、一份角色卡内容只有一个资产行。
- **隔离**：所有读写经 repository 基座限定 `account_id`（ADR-001）。图片复核要求本账号，所以载荷里不会出现其他账号的图片。
- **助手读取**：见 `api: 助手读取与使用角色卡`。

### 生命周期与演进
- 回收站、彻底删除沿用资产库：进回收站只标资产行；彻底删除只删资产行和归类，内容留给 GC。画布节点持有的版本不受影响。
- 迁移（接在当前最新迁移之后，起草时为 `0081`）：两处 kind CHECK 和 `role` CHECK 加值；`creative_asset_search` 加 IP、作品两列及部分索引。首次建模，无存量数据，不需要回填。回滚时如已有 `character` 内容、资产或 `role = character` 的持有关系行则拒绝，前向修复。
- 演进：载荷靠 `schema_version` 演进。以后的导入可以加 `imported_from` 之类字段，3D 表示可以加模型引用，都在对应立项时定义。
- OpenAPI：新增 `CreativeCharacterPayload`（结构同上表，`views` 元素为 `CreativeCharacterView`）；资产相关的 `kind` 枚举（资产列表筛选、`CreativeLibraryAsset.kind`、`CreativeAssetFacets.kinds`）加 `character`，内容载荷的联合类型加 `character`。

### 表达与样例
```json
{
  "character": {
    "name": "芙宁娜",
    "appearance": "白发及肩，发尾挑染浅蓝；异色瞳，左蓝右浅蓝；深蓝礼帽带白色羽饰；蓝白燕尾短外套，白色短裤，黑白条纹长袜。",
    "views": [
      {"content_revision_id": "ccrv_0f3a1c52-7d0e-4c8b-9a41-2e6f5b7c8d90", "view": "front"},
      {"content_revision_id": "ccrv_6b2e9d14-3a7f-4e15-8c02-91d4a6b3f7e8", "view": "side"},
      {"content_revision_id": "ccrv_c4d81a7e-52b9-4f60-a3e7-0b9f2d6c1e54", "view": "face", "note": "眼睛颜色以这张为准"}
    ],
    "sources": [{"content_revision_id": "ccrv_9a7e3b21-0c4d-4f8e-b615-7d2c9e0a3f46"}],
    "aliases": ["芙卡洛斯", "Furina", "水神"],
    "variant": "常服",
    "origin": "derivative",
    "ip": {"name": "原神", "key": "genshin-impact"},
    "attribution": "参考官方立绘",
    "source_urls": ["https://example.com/furina"]
  }
}
```

同 IP 多作品的例子（只列身份字段）：`{"name": "阿尔托莉雅·潘德拉贡", "aliases": ["Saber", "阿尔托莉雅"], "variant": "第三灵基", "origin": "derivative", "ip": {"name": "Fate", "key": "fate"}, "work": "Fate/Grand Order"}`。

访问样例：列出原神的角色卡，按最近更新排序：`GET /api/v1/creative/assets?kind=character&character_ip=genshin-impact`，返回的每一项 `kind = character`、`title = 芙宁娜 · 常服`，封面取第一张参考图。用名字 `character_ip=Genshin` 查询结果相同（命中目录别名）。

## data: ADD 内置角色 IP 目录

### 用途与存储角色
- **用途**：常见 IP 的标准名称、别名和作品列表，让建卡时「选」而不是「填」，并把「原神 / Genshin / Genshin Impact」归到同一个 IP。不含角色、图片或任何官方素材。
- **存储**：随后端代码发布的静态数据文件，编译时嵌入（`embed`），不进数据库。文件为 `backend/internal/creativelibrary/character_ips.json`。
- **角色**：内置 IP 身份（`key`）的权威来源。角色卡载荷里的 `ip.name` 是写入时的快照，之后目录改名不回写角色卡。
- **所有权**：开发者维护，经代码评审合入；账号不能修改。

### 结构与身份

| 路径 | 类型 | 必须 | 约束 |
|---|---|---|---|
| `ips[].key` | string | 是 | `[a-z0-9-]`，1–60，全目录唯一；**一经发布不改、不复用** |
| `ips[].name` | string | 是 | 显示名（中文优先），1–60 码点 |
| `ips[].aliases` | string[] | 否 | 英文名、简称、旧译名；规范化后在全目录（含所有 `name`）唯一，否则构建期测试失败 |
| `ips[].works` | array | 否 | 旗下作品，只有一个 IP 对应多部作品时才填（如 Fate）；用于作品联想和作品归一 |
| `ips[].works[].name` | string | 是 | 作品名，1–100 码点，同一 IP 下规范化后唯一 |
| `ips[].works[].aliases` | string[] | 否 | 简称、英文名（如「FGO」「Fate/GO」）；同一 IP 下与所有作品名、别名规范化后唯一。不同 IP 之间允许重复，重复时由 `character_work` 的歧义规则处理 |

首批 50–100 个，覆盖常见二游和动漫 IP；名单在实施时整理，owner 过目后合入。

### 访问模式与保证
- 进程启动时载入内存；IP 归一和 `api: 查找角色 IP` 只读内存。
- 构建期测试保证：`key` 格式与唯一、IP 名称和别名规范化后无冲突、同一 IP 下作品名称和别名无冲突、与上一次发布相比没有删除或改名的 `key`（删除改为标记 `retired: true`，不再出现在联想里，但已有 `key` 仍能解析）。

### 生命周期与演进
- 随版本发布增补。退役的 IP 保留条目；已写进角色卡的 `key` 永远可解析。
- 目录改显示名时，已有角色卡的 `ip.name` 不变，按 `key` 过滤和联想不受影响；查看浮窗显示载荷里的名字。
- 目录新收录一个账号早已在用的自建 IP 名（或新增别名）时，旧卡的载荷和投影不改写：过滤按「列表与筛选」的名字兼容规则仍能命中；旧卡下次存新版本时自然归一到 `key`。
- 以后做市场按 IP 浏览，以 `key` 对齐不同账号的角色卡；自建 IP 没有 `key`，只按名字。

## data: ADD 角色卡节点

### 用途与存储角色
- **用途**：画布上的角色卡。作为生成节点的参考，生成时展开（`patterns: 角色卡展开为生成参考`）。
- **存储**：沿用 `creative_nodes`。节点类型 `core.character` 在 `backend/internal/creativecanvas/definitions.go` 注册（`creative_nodes.type_key` 没有数据库约束），不需要迁移。
- **角色**：节点的 `content_revision_id` 是它使用的角色卡版本，是 GC 持有方（节点已在 `LiveOwnerCondition` 里）。
- **所有权**：画布命令层。

### 结构与身份
- 定义：`{TypeKey: "core.character", SchemaVersion: 1, CreationSupported: false, Ports: ["output"], Actions: ["move", "resize", "rename", "duplicate", "remove", "reference", "update_character"]}`。不能生成、不能编辑内容、不能「存入资产库」（它本来就来自资产库）。具体字段名实施时按 `definitions.go` 的现有结构核对。
- 内容：一个 `kind = character` 的内容修订。
- 节点视图附加只读字段 `character`：

| 字段 | 类型 | 出现条件 | 说明 |
|---|---|---|---|
| `character.name` | string | 总是 | 节点版本的角色名 |
| `character.cover_revision_id` | string | 总是 | `views[0]` 的图片修订，用来显示封面 |
| `character.view_count` | integer | 总是 | 参考图张数，用于提示「将展开为 N 张参考图」 |
| `character.asset_id` | string | 有对应资产时 | 同一 `content_id`、不在回收站的角色卡资产 |
| `character.latest_revision_id` | string | 资产的当前版本与节点不同时 | 有新版本 |

### 访问模式与保证
- **建节点**：
  - 从资产库放入、拖入、「选参考」：`add_node` 带角色卡资产，内容为资产的当前版本。「选参考」时照常连到目标生成节点的 `reference` 端口。
  - 助手：`edit_canvas` 的 `add_node` 接受角色卡资产（`api: 助手读取与使用角色卡`）。
  - 画布的「放入」「填充」不接受角色卡（填充是替换媒体内容，不适用）。
- **连线**：只能作为参考连到生成节点（`target_port = reference`）。连到非生成节点、作为 `data` 输入时，画布命令以现有的连线校验错误拒绝。
- **更新到新版本**：动作 `update_character` 把节点内容换成 `latest_revision_id`，产生节点版本，可撤销；与当前相同时不产生版本。
- **新版本提示**：读画布时按节点内容所属的 `content_id` 查资产，规则见上表；查询按画布里的角色卡节点批量进行，不逐个查。

### 生命周期与演进
- 删除节点即解除持有。角色卡资产进回收站或彻底删除，不影响节点已持有的版本，节点照常可以生成，只是不再有 `asset_id` 和新版本提示。
- 新节点类型，没有存量，不需要回填。

## patterns: ADD 角色卡展开为生成参考

- **约定**：角色卡在生成参考里**先展开、再做任何检查**。`creativecanvas` 提供一个带事务的展开步骤，接在 `orderedGenerationRefs`（纯图计算）之后，被三处共用：路由预检读草稿（`GenerationDraftInTx`，由 `creativegeneration` 的 `prepareImage` 调用）、锁内受理（`generation.go` 中逐项核对参考的那段）、提示词编译（`CompileGenerationPromptInTx`）。三处拿到的有序图片和编译后的提示词完全相同，锁内受理照常逐项比对。规则：
  1. 参考来源的内容修订是 `kind = character` 时，在排序中的位置不变，替换成它的 `views`，按 `views` 的顺序依次排列，每张的 `role` 取原连线的 `role`。读集仍记原来的角色卡节点。
  2. 展开后的图片按现有规则编号 `[图1]`、`[图2]`…，并照常附「参考编号说明」。
  3. 提示词中的 `@[角色卡节点 ID]` 编译成 `「<name>」（[图a]–[图b]）`，只有一张图时为 `「<name>」（[图a]）`。
  4. 只要参考里有角色卡，提示词末尾就追加下面这段（提示词里没有 `@` 时也追加），每个角色卡一行，按首次出现的顺序：
     ```
     角色设定：
     - 「<name>」（[图a]–[图b]）：<appearance>
     ```
  5. 参考张数、单张大小、提示词长度仍由实际路由的策略判断（预检时）。展开后超限时拒绝，不丢图、不截断；错误详情给出上限、展开后的总数和每张角色卡贡献的张数。
  6. 展开出的每张图片照常经生成参考检查；某张不可用时拒绝，错误详情指出是哪张角色卡的哪张图。
  7. **持有与溯源**：执行的输入关系（`creative_execution_refs`，`direction = input`）按发送顺序写展开后的图片（role 取连线的 role），另外每个角色卡版本写一行 `role = character`。`role = character` 的行不发给模型，派发前的图片复核和衍生权利计算跳过它（图片已经逐张在列）；发布结果时和其他输入关系一起复制到结果版本的输入关系（`creative_node_version_input_refs`）。这样执行期间由执行记录持有、之后由结果版本持有，GC 不会回收被结果引用的角色卡版本。现有「输入最多 50 项」的上限只算发给模型的图片，`role = character` 的行另计，最多 8 行。
  8. 溯源快照加 `characters: [{node_id, content_id, content_revision_id, name, images: [a, b]}]`，用于界面显示「用了哪张角色卡的哪个版本、对应第几张到第几张图」；持有靠第 7 条的关系行，不靠快照。
- **为什么**：摄影师和助手只需要连一个节点；展开集中在一处，预检、受理、编译不会各算一套。拒绝而不截断，是因为悄悄少几张参考图会让一致性下降，又看不出原因。
- **待验证**：第 3、4 条的写法、`views` 是否按视角重排，由一致性调查确认（design「未决事项」）；结论不同时先修订本条，再实施展开。
- **例子**：生成节点连了角色卡「芙宁娜」（3 张参考图）和一张图片「场景参考」，提示词「@[n_char] 站在 @[n_bg] 的台阶上」→ 发送的图片依次为芙宁娜的 3 张和场景参考，编译为「「芙宁娜」（[图1]–[图3]）站在 [图4] 的台阶上\n\n参考编号说明：[图1] 对应附件第 1 项；……\n\n角色设定：\n- 「芙宁娜」（[图1]–[图3]）：白发及肩……」。执行输入关系 5 行：4 张图片加 1 行 `role = character`。

## api: ADD 新建角色卡

### 用途与调用
- **用途**：新建一张角色卡，写第 1 个版本并存入资产库。
- **消费者**：资产库「新建角色卡」、资产库多选图片的「创建角色卡」、画布图片节点的「创建角色卡」。以后助手建的草稿存入资产库也走这个接口（复制草稿载荷）。
- **调用**：`POST /api/v1/creative/assets/characters`，`Content-Type: application/json`，`Idempotency-Key` 必填，外层为公共约定 `{operation_id, client_created_at, payload}`，`Idempotency-Key` 等于 `operation_id`。
- **权限**：网页登录态；账号有创意空间写入权限（同 `/creative/assets/from-canvas-node`）。所有图片必须属于当前账号。

### 请求

| 字段路径 | 位置 | 类型 | 必传 | 可为 null | 缺省行为 | 约束与含义 |
|---|---|---|---|---|---|---|
| `Idempotency-Key` | header | uuid | 是 | 否 | 缺失报 400 | 等于 `operation_id` |
| `operation_id` | body | uuid | 是 | 否 | — | 公共约定 |
| `client_created_at` | body | date-time | 是 | 否 | — | 公共约定 |
| `payload.character` | body | `CreativeCharacterPayload` | 是 | 否 | — | 结构与约束见 `data: 角色卡`「结构与身份」 |
| `payload.target.description` | body | string | 否 | 否 | 空 | ≤ 1000 码点 |
| `payload.target.group_ids` | body | string[] | 否 | 否 | 不归入分组 | 同 `CreativeUploadAssetTarget` |
| `payload.target.tag_ids` | body | string[] | 否 | 否 | 无标签 | 同上 |
| `payload.target.new_tags` | body | array | 否 | 否 | 无 | 同上 |
| `payload.target.is_favorite` | body | boolean | 否 | 否 | false | 同上 |

`payload.target` 是新的 `CreativeCharacterTarget`：等于 `CreativeUploadAssetTarget` 去掉 `title`（标题由角色名和版本说明生成，见 `data: 角色卡`）。`payload.target` 本身可以省略，等同空对象。

`payload.character.ip` 按 `data: 角色卡`「IP 归一」处理，响应不回显载荷；需要看归一结果时读资产。

### 成功响应
- 201，`CreativeAssetCreateResult`：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `asset_id` | string | 是 | 新资产 `ccas_…` |
| `revision` | CreativeRevision | 是 | 资产行修订，新建为 `"1"` |
| `content_revision_id` | string | 是 | 角色卡第 1 个版本 `ccrv_…` |
| `library_revision` | CreativeRevision | 是 | 资产库修订 |
| `tag_mapping` | object | 是 | 同现有语义 |
| `reused` | boolean | 是 | 总是 `false`（角色卡不查重） |
| `title` / `description` | string | 是 | `title` 为 `<name> · <variant>` 或 `<name>` |
| `tags_created` / `tags_reused` | array | 有新标签时 | 同现有语义 |

随幂等回执重放。

### 失败响应
错误体为公共 `ErrorEnvelope`。

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 载荷字段缺失或越界；`views` 为空或超过 4 张；`views` 内图片重复；没有 `ip` 却带了 `work`；`ip.key` 不在内置目录里；`aliases` 重复或与 `name` 相同；`source_urls` 协议不对。`details.fields` 列出出错的字段路径 | 修正后重试 |
| 400 | `creative_image_unavailable` | 某张图片不属于本账号、不是图片、未就绪，或不能用作生成参考（`views` 内）/ 不可展示（`sources` 内）；`details.content_revision_ids` 列出这些图片 | 换图后重试 |
| 404 | `not_found` | `target` 引用的分组或标签不存在 | 重读资产库 |
| 409 | `idempotency_conflict` | 同一 `operation_id` 用于不同请求 | 公共约定 |
| 403 | `forbidden` | 创意空间当前不可用 | 公共约定 |

### 交互规则
- 写操作：一个事务内建内容、修订、持有关系、资产行和归类，不会部分成功。重复提交同一 `operation_id` 返回首次结果，不建第二张。
- 并发：新建没有并发冲突。

### 调用示例
```http
POST /api/v1/creative/assets/characters
Idempotency-Key: 3b1c2d4e-5f60-4718-9a2b-3c4d5e6f7081
Content-Type: application/json

{"operation_id": "3b1c2d4e-5f60-4718-9a2b-3c4d5e6f7081", "client_created_at": "2026-11-12T08:00:00Z",
 "payload": {
   "character": {
     "name": "芙宁娜",
     "appearance": "白发及肩，发尾挑染浅蓝；异色瞳；深蓝礼帽带白色羽饰；蓝白燕尾短外套。",
     "views": [
       {"content_revision_id": "ccrv_0f3a1c52-7d0e-4c8b-9a41-2e6f5b7c8d90", "view": "front"},
       {"content_revision_id": "ccrv_6b2e9d14-3a7f-4e15-8c02-91d4a6b3f7e8", "view": "side"}
     ],
     "sources": [],
     "aliases": ["Furina"],
     "variant": "常服",
     "origin": "derivative",
     "ip": {"name": "Genshin"}
   },
   "target": {"group_ids": ["ccgr_5a1e"], "is_favorite": true}
 }}
```
201（`ip` 命中目录别名，保存为 `{"name": "原神", "key": "genshin-impact"}`）：
```json
{"asset_id": "ccas_7c9d0e1f-2a3b-4c5d-8e6f-708192a3b4c5", "revision": "1", "content_revision_id": "ccrv_d1e2f3a4-b5c6-4d7e-8f90-a1b2c3d4e5f6", "library_revision": "212", "tag_mapping": {}, "reused": false, "title": "芙宁娜 · 常服", "description": ""}
```
400（第二张图已被彻底删除，不可用）：
```json
{"error": {"code": "creative_image_unavailable", "message": "有图片已不可用", "details": {"content_revision_ids": ["ccrv_6b2e9d14-3a7f-4e15-8c02-91d4a6b3f7e8"]}}}
```

### 兼容与变更
首次定义，无已有消费者。资产 `kind` 枚举加 `character` 是兼容扩展：旧前端遇到不认识的 kind 按现有兜底显示；前后端同一版本上线。元数据接口（`POST /creative/assets/{id}/metadata`）对角色卡传 `title` 时返回 400 `validation_failed`（标题由角色名和版本说明生成，改名要存新版本），其余字段照常。资产列表新增可选查询参数 `character_ip`、`character_work`（`data: 角色卡`「列表与筛选」），只在 `kind=character` 时生效，与其他 kind 一起给时返回 400 `validation_failed`。

## api: ADD 角色卡存新版本

### 用途与调用
- **用途**：修改角色卡：把一份完整的新载荷写成新版本，资产改指新版本。
- **消费者**：查看浮窗的「编辑角色卡」。以后助手草稿的「更新原角色卡」也走这个接口。
- **调用**：`POST /api/v1/creative/assets/{id}/character-revisions`，`Idempotency-Key` 必填，公共请求外层。
- **权限**：资产属于当前账号，账号有创意空间写入权限。

### 请求

| 字段路径 | 位置 | 类型 | 必传 | 可为 null | 缺省行为 | 约束与含义 |
|---|---|---|---|---|---|---|
| `id` | path | string | 是 | 否 | — | 角色卡资产 `ccas_…` |
| `payload.expected_revision` | body | CreativeRevision | 是 | 否 | — | 资产行当前 `revision`，防止覆盖别人刚存的版本 |
| `payload.character` | body | `CreativeCharacterPayload` | 是 | 否 | — | **完整**新载荷，不是补丁；约束同新建 |

### 成功响应
- 200，`CreativeAssetCreateResult`：`asset_id` 为路径里的资产；`revision` 为资产行新修订；`content_revision_id` 为当前版本；`reused = false`；`title` 按新版本生成；`tag_mapping` 为空对象。
- 新载荷与当前版本规范化后相等时：不写新版本，`revision`、`content_revision_id` 为当前值，`reused = true`。

### 失败响应

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | 载荷不合法（同新建）；资产不是角色卡 | 修正 |
| 400 | `creative_image_unavailable` | 同新建 | 换图 |
| 404 | `not_found` | 资产不存在或不属于本账号 | 重读 |
| 409 | `creative_revision_conflict` | `expected_revision` 与资产行当前不符 | 重读后决定是否覆盖 |
| 409 | `creative_asset_trashed` | 资产在回收站 | 先恢复 |
| 409 | `idempotency_conflict` | 公共约定 | — |

### 交互规则
- 写操作：追加修订、写持有关系、资产改指、索引更新在一个事务里；不会出现资产指向不存在的版本。
- 并发：以资产行 `revision` 做乐观并发；修改分组、标签、评分、收藏也会推进资产行 `revision`，所以摄影师在两个窗口里一边改归类、一边改角色卡时，后保存的一方会冲突并重读。
- 已放在画布上的角色卡节点不变（`data: 角色卡节点`）。

### 调用示例
```http
POST /api/v1/creative/assets/ccas_7c9d0e1f-2a3b-4c5d-8e6f-708192a3b4c5/character-revisions
Idempotency-Key: 8d9e0f1a-2b3c-4d5e-9f60-718293a4b5c6
Content-Type: application/json

{"operation_id": "8d9e0f1a-2b3c-4d5e-9f60-718293a4b5c6", "client_created_at": "2026-11-12T09:30:00Z",
 "payload": {
   "expected_revision": "1",
   "character": {
     "name": "芙宁娜",
     "appearance": "白发及肩，发尾挑染浅蓝；异色瞳，左深蓝右浅蓝；深蓝礼帽带白色羽饰；蓝白燕尾短外套。",
     "views": [
       {"content_revision_id": "ccrv_0f3a1c52-7d0e-4c8b-9a41-2e6f5b7c8d90", "view": "front"},
       {"content_revision_id": "ccrv_6b2e9d14-3a7f-4e15-8c02-91d4a6b3f7e8", "view": "side"},
       {"content_revision_id": "ccrv_c4d81a7e-52b9-4f60-a3e7-0b9f2d6c1e54", "view": "back"}
     ],
     "sources": [],
     "aliases": ["Furina", "芙卡洛斯"],
     "variant": "常服",
     "origin": "derivative",
     "ip": {"name": "原神", "key": "genshin-impact"}
   }
 }}
```
200：
```json
{"asset_id": "ccas_7c9d0e1f-2a3b-4c5d-8e6f-708192a3b4c5", "revision": "2", "content_revision_id": "ccrv_e7f8a9b0-c1d2-4e3f-8a4b-5c6d7e8f9012", "library_revision": "213", "tag_mapping": {}, "reused": false, "title": "芙宁娜 · 常服", "description": ""}
```
409（另一个窗口刚给它加了标签，资产行已是修订 2）：
```json
{"error": {"code": "creative_revision_conflict", "message": "内容已被其他窗口修改，本机这次修改未保存"}}
```

### 兼容与变更
首次定义，无已有消费者迁移。

## api: ADD 查找角色 IP

### 用途与调用
- **用途**：建卡和编辑角色卡时，给「所属 IP」和「作品」输入框提供候选，让摄影师选而不是手填。
- **消费者**：资产库的新建、编辑角色卡对话框；资产库角色卡视图的 IP 筛选。
- **调用**：`GET /api/v1/creative/character-ips`，响应 `application/json`。
- **权限**：网页登录态；账号有创意空间读取权限。自建 IP 只来自本账号的角色卡。

### 请求

| 字段 | 位置 | 类型 | 必传 | 缺省行为 | 约束与含义 |
|---|---|---|---|---|---|
| `query` | query | string | 否 | 不过滤，按下方排序返回前 `limit` 个 | ≤ 60 码点；按规范化规则做前缀和包含匹配，内置 IP 的别名也参与匹配 |
| `limit` | query | integer | 否 | 20 | 1–50 |

### 成功响应
200：

| 字段 | 类型 | 必返 | 说明 |
|---|---|---|---|
| `items[]` | array | 是 | 候选 IP，可以为空数组 |
| `items[].name` | string | 是 | 显示名 |
| `items[].key` | string | 内置 IP 时 | 目录 `key`；自建 IP 没有 |
| `items[].builtin` | boolean | 是 | 是否来自内置目录 |
| `items[].character_count` | integer | 是 | 本账号不在回收站的角色卡里，当前版本属于这个 IP 的张数 |
| `items[].works` | string[] | 是 | 作品候选：内置目录的作品，加本账号这个 IP 下角色卡用过的作品，去重；可以为空数组 |

- 排序：本账号用过的（`character_count > 0`）在前，按张数降序；其余按目录顺序。同名的内置 IP 和自建 IP 不会同时出现：自建取值命中目录时在写入时已经归一。
- 退役的内置 IP（`retired`）只在本账号有角色卡用它时出现。

### 失败响应

| 状态码 | 错误码 | 触发条件 | 调用方处理 |
|---|---|---|---|
| 400 | `validation_failed` | `query` 过长；`limit` 越界 | 修正 |
| 403 | `forbidden` | 创意空间当前不可用 | 公共约定 |

### 交互规则
- 只读，无副作用。不分页：候选只用于输入联想，`limit` 封顶。
- 「新建 IP」就是在输入框里输入一个没有候选的名字并保存角色卡，不需要单独的接口。

### 调用示例
```http
GET /api/v1/creative/character-ips?query=fa&limit=5
```
200：
```json
{"items": [
  {"name": "Fate", "key": "fate", "builtin": true, "character_count": 2, "works": ["Fate/stay night", "Fate/Zero", "Fate/Grand Order"]},
  {"name": "Fallen Star 原创", "builtin": false, "character_count": 1, "works": []}
]}
```
400：
```json
{"error": {"code": "validation_failed", "message": "limit 超出范围"}}
```

### 兼容与变更
首次定义，无已有消费者。

## api: ADD 助手读取与使用角色卡

### 用途与调用
- **用途**：让助手（内置助手和外部助手）找到、读取角色卡，放到画布上连到生成节点；让摄影师在对话输入框里引用角色卡。
- **消费者**：内置创作助手；外部助手经 MCP 的只读工具（`search_assets`、`read_asset`，共用同一个 `Info()`）。
- **调用**：现有工具的扩展，不新增工具：
  - `search_assets`：`kind` 过滤接受 `character`。
  - `read_asset`：读角色卡版本。
  - `edit_canvas` 的 `add_node`：接受角色卡资产。
  - 对话指令 `instruction_segments[]` 的 `content_ref`：`content_revision_id` 可以是角色卡版本。
- **权限**：沿用现有的读取准入，不为角色卡新增授权：
  - 内置助手：角色卡的文字按 `media_metadata` 外发；参考图作为图片附件需要模型能看图、且本会话授权含 `image_preview`，不满足时只给文字。
  - 外部助手：沿用现有连接模型，连接时授权的是整个账号的全部数据类别，由外部宿主自己决定是否看图，所以参考图总是附上。首版不引入连接级的图片授权。

### 请求
- `search_assets`：已有参数不变，`kind` 的合法值加 `character`；新增可选参数 `character_ip`、`character_work`，解析和匹配规则同资产列表的同名过滤（`data: 角色卡`「列表与筛选」，含作品别名和歧义），只在 `kind = character` 时允许。`query` 匹配角色名、别名、版本说明、IP、作品和外貌描述。
- `read_asset`：已有参数不变。`content_revision_id` 给角色卡版本（`search_assets` 结果里的 `content_revision_id`），读的就是这个版本。
- `edit_canvas` 的 `add_node`：已有的资产放置参数不变；资产为角色卡时建 `core.character` 节点，内容为资产的当前版本。
- `content_ref`：结构不变 `{type: "content_ref", content_revision_id}`。

### 成功响应
- `search_assets` 每个角色卡项附要点 `character`：`name`、`aliases`、`variant`（有时）、`origin`（有时）、`ip`（有时）、`work`（有时）、`view_count`、`appearance` 前 200 码点。不附图片。助手据 `ip`、`work`、`variant` 向摄影师区分同名角色。
- `read_asset` 读角色卡版本：返回完整载荷（`name`、`aliases`、`variant`、`appearance`、`views[{content_revision_id, view, note}]`、`sources[{content_revision_id}]`、`origin`、`ip`、`work`、`attribution`、`source_urls`）。
  - **图片**：按 `views` 的顺序附图片，最多 4 张（即 `views` 全部）。附上的每张图都必须是这个版本 `views` 里的图片修订，读取时逐张校验，不接受调用方另指图片。
  - `read_asset` 按版本读取：资产进回收站或彻底删除，不影响画布节点已持有的版本（`data: 角色卡节点`），内置和外部助手一致；回收站只影响 `search_assets` 的结果。
  - 某张图无法压缩到可发送的大小或格式时，单独省略这一张，其余照附；`images_omitted: [{content_revision_id, reason}]` 列出被省略的图和原因。内置助手因模型不能看图或缺 `image_preview` 而全部不附时，`images_omitted` 同样列出全部并给出原因。
  - `sources` 只给 ID、宽高等元数据，不附像素。
- `edit_canvas`：与放置其他资产相同的结果，节点类型为 `core.character`。
- `content_ref` 引用角色卡：消息块按 `read_asset` 的形状给出角色卡内容，图片附件规则相同。

### 失败响应

| 信号 | 错误 | 触发条件 | 处理 |
|---|---|---|---|
| 工具错误 | `invalid_arguments` | `kind` 不合法；给了 `character_ip` / `character_work` 但 `kind` 不是 `character`；`character_work` 的别名命中多部作品（结果里列出候选） | 助手修正参数，或把候选给摄影师选 |
| 工具错误 | `not_found` | 资产或版本不存在、不属于本账号 | 重新搜索 |
| 工具错误 | 现有连线校验错误 | 把角色卡节点连到非生成节点，或作为 `data` 输入 | 按 `data: 角色卡节点` 改连 |
| 生成受理失败 | 见 `patterns: 角色卡展开为生成参考` 第 5、6 条 | 展开后超限或图片不可用 | 减少角色卡或参考图 |

### 交互规则
- 只读工具无副作用。`edit_canvas` 的写入语义、撤销、本轮改动展示与放置其他资产相同。
- 助手说明（工具 `Info()`）写明：角色卡在生成时自动展开，连一个角色卡节点即可，不要再逐张连它的参考图；同框角色不要超过模型参考上限；摄影师提到 IP 或作品（含简称，如「FGO」）时用 `character_ip` / `character_work` 缩小范围，仍有多张符合时列出来（带 IP、作品、版本说明）让摄影师选，不要自行挑一张。

### 调用示例
`search_assets` 调用：
```json
{"query": "水神", "kind": "character", "character_ip": "原神"}
```
结果中的一项（「水神」命中别名，「原神」按名字匹配到内置 IP）：
```json
{"asset_id": "ccas_7c9d0e1f-2a3b-4c5d-8e6f-708192a3b4c5", "kind": "character", "title": "芙宁娜 · 常服", "content_revision_id": "ccrv_e7f8a9b0-c1d2-4e3f-8a4b-5c6d7e8f9012",
 "character": {"name": "芙宁娜", "aliases": ["Furina", "芙卡洛斯"], "variant": "常服", "origin": "derivative", "ip": {"name": "原神", "key": "genshin-impact"}, "view_count": 3, "appearance": "白发及肩，发尾挑染浅蓝；异色瞳，左深蓝右浅蓝；深蓝礼帽带白色羽饰；蓝白燕尾短外套。"}}
```
用作品简称缩小范围（「FGO」按 Fate 的作品别名解析为 Fate/Grand Order）：`{"query": "Saber", "kind": "character", "character_ip": "fate", "character_work": "FGO"}`。
`read_asset` 读上面那张卡：`{"content_revision_id": "ccrv_e7f8a9b0-c1d2-4e3f-8a4b-5c6d7e8f9012"}`，结果附 3 张图片，顺序同 `views`。
失败（IP 过滤用在非角色卡上）：
```json
{"error": "invalid_arguments", "message": "character_ip 只能与 kind=character 一起使用"}
```

### 兼容与变更
兼容扩展：参数只加枚举值和两个可选参数，旧的调用方式不变。`read_asset` 读角色卡时可以附多张图片，内置和外部的图片适配都要支持多图（现有只处理单张 `image_revision_id`）。外部助手的工具说明随 `Info()` 更新。
