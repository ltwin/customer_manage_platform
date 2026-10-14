---
doc_type: feature-design
feature: 2026-10-13-agent-asset-lookup
requirement: V6-035（助手按需查找资产库素材）、V6-077（助手找回会话中的多模态内容）
status: draft
date: 2026-10-13
---

# 助手查找资产库与找回会话内容

## 0. 结论

两条同批设计，因为它们共用读取规则（`read_asset`）和授权改动。

| 决定 | 内容 | 来源 |
|---|---|---|
| 资产库授权 | 改写首次外发说明、升级说明版本；之后每次发送都授权助手按需查找资产库，不另设开关 | owner 2026-10-13 |
| 会话内容读取 | 服务端按对话判定：本对话里出现过的内容，在本对话后续轮次都能再读；前端不再拼「最近 20 条」清单 | owner 2026-10-13 |
| 会话与资产库分开 | 两组工具、两条读取规则；会话工具只看本对话，资产库工具只看资产库 | owner 2026-10-13 |
| 生成结果进会话 | `generate_node` 采用后的结果、`compress_image` 采用后的结果，记为本条工具消息的 `result` 引用 | 本设计 |
| 放上画布 | `edit_canvas` 的 `add_node` 可带资产库素材，或带本对话中出现过的内容；另加 `fill_node` 填空节点 | 本设计 |
| 存入资产库 | `save_to_library` 把画布节点当前内容存为素材，只能选已有分组和标签；新建标签归 V6-078 | 本设计 |
| 检索性质 | 精确检索，复用 `creativelibrary.SearchAssets`；工具契约不暴露检索方式，以后接语义检索不改契约 | V6-035 AC-04、ARCH-05 |

本期不做：

- 跨对话找内容
- 资产库整理（V6-078）
- 语义检索
- 外发授权的撤销入口（V6-029 已定不做）

## 1. 现状

2026-10-13 核对，代码基线 `70dc039`。

| 能力 | 位置 | 本设计怎么用 |
|---|---|---|
| 每次发送建一条授权：`selected_revisions` 模式，清单是「最近 20 条消息的引用 + 本条引用」 | `frontend/src/creative-canvas/editor/AgentPanel.tsx` 的 `historyIDs`、`agentState.ts` 的 `messageSteps` | 改为 `account_library` 模式，清单只放本条引用 |
| `account_library` 模式：数据库和 OpenAPI 已有；建授权时不能带清单；`coversRun` 遇到带引用的运行一律拒绝 | `creativeagent/consents.go`、`runs.go` 的 `coversRun` | 放开：可带本条清单，引用必须在清单里 |
| 可读判定：授权清单里的，或当前画布节点上的 | `creativeagent/context.go` 的 `readableInTx` | 增加两个分支（§2.2） |
| 消息引用表 `creative_message_content_refs`，角色分 input / attachment / result；同时是内容保留根 | 迁移 0044；`creativereference/projection.go:44` | V6-077 的索引来源；生成结果写入这张表 |
| 工具消息只有 `read_asset` 记 `result` 引用 | `context.go` 的 `persistToolMessage` | 推广到 `generate_node`、`compress_image` |
| `read_history` 倒序分页，只给引用标识，不给类型和来源 | `context.go` 的 `readHistory` | 保留，用来读对话文字 |
| 资产库检索：关键词、类型、分组、标签、评分、主色、画幅、排序、游标 | `creativelibrary.SearchAssets` | 直接复用 |
| 画布已支持带素材建节点（`add_node` + `asset`）和填空节点（`fill_from_asset`），乐观校验素材修订 | `creativecanvas/graph_commands.go` | 助手工具暴露这两项 |
| 画布不能用一个已有的内容修订建节点 | `graph_model.go` 的 `Action` | 新增 `content_revision_id`（§3.5） |
| 存入资产库命令 | `creativecanvas.SaveNodeToLibrary` | 包成工具 |
| 说明版本 `egress-2026-10-1`；说明文案写着「资产库里没有被引用的素材不会发送」 | `service.go`、`agentState.ts`、`EgressNotice.tsx`、`PrivacyPage.tsx` | 改写并升级到 `egress-2026-10-2` |

## 2. 授权与读取规则

### 2.1 授权

前端每次发送建的授权改为：

- `mode: account_library`
- `data_classes`：在现有 `text`、`canvas_metadata`、按模型能力加的 `image_preview` 之外，加上 `media_metadata`
- `selected_revision_ids`：只放本条消息自己引用的修订，不再带历史消息的引用

后端改动：

- `GrantConsent`：`account_library` 也允许带清单，上限同 `selected_revisions`。
- `coversRun`：两种模式下，运行带的引用都必须在清单里。删掉「有引用就必须是 `selected_revisions`」那一条。
- `policyVersion` 升到 `egress-2026-10-2`，前端 `egressNoticeVersion` 同步。
- `selected_revisions` 模式继续支持：已有授权、旧页面发出的请求照常工作，只是拿不到资产库工具（§3.1）。

### 2.2 读取判定

`readableInTx(run, consent, revision)` 按顺序判定，任一成立即可读：

1. 在本次授权清单里（现有）。
2. 是本次运行所在画布上某个节点的当前内容（现有）。
3. **本对话出现过**（V6-077）：在本对话某条消息的 `creative_message_content_refs` 里，并且本对话没有哪条**已撤销**的授权清单列过它。
4. **资产库素材**（V6-035）：授权是 `account_library` 模式，并且它是本账号某个未进回收站素材的当前内容修订。

判定成立后照旧走 `creativecontent.RequireUsable(..., "display")`，图片照旧要求 `image_preview`。所以：

- 已删除、权利被撤销、不可展示的内容，任何分支都读不到。
- 对话授权本身被撤销，或说明版本过期，整条运行先在 `coversRun` 被拦下。

第 3 条的依据：这些内容此前已在本对话中合法发送或产生过。

- 摄影师发的内容，当时在那条消息的授权清单里。
- 助手读过的内容，当时通过了读取判定。
- 生成结果是模型服务商返回的。

撤销那条检查是为了让「撤销某条授权」继续表示「不再发送它列过的内容」，哪怕将来才做撤销入口。

第 4 条只认素材的**当前**修订，进了回收站就不认了。但助手读过它之后，它已经作为 `result` 引用进了对话，之后按第 3 条仍可读。这符合「本对话出现过」的定义，不另做例外。

### 2.3 工具层的检查

| 工具 | 要求 |
|---|---|
| `search_assets`、`read_library` | 授权是 `account_library` 模式，且有 `media_metadata` |
| `list_conversation_assets` | 无额外要求，只返回本对话的引用 |
| `read_asset` | §2.2 |
| `edit_canvas` 带 `asset_id` | 授权是 `account_library` 模式 |
| `edit_canvas` 带 `content_revision_id` | 该修订按 §2.2 可读 |
| `save_to_library` | 不涉及外发，只检查节点 |

工具结果进入模型前，现有的派发检查（`admitDispatchInTx`）会对附带的图片再做一次 `RequireUsable`，不变。

## 3. 工具契约

新工具都登记在 `runtimeToolEntries`，展示名直接出现在对话的工具块里。分页参数和拒绝的写法沿用现有工具：参数错就拒绝并说明原因；查不到时如实返回空列表。

### 3.1 `search_assets`（V6-035，只读）

- **展示名**：「查找资产库」
- **何时提供**：仅在授权是 `account_library` 模式时；旧授权不提供。

参数，全部可选，`limit` 除外：

| 参数 | 说明 |
|---|---|
| `query` | 关键词，匹配标题、描述、正文和标签名 |
| `kind` | `image` / `video` / `audio` / `text` / `link` |
| `group_id`、`include_subgroups` | 分组，ID 来自 `read_library` |
| `tag_ids`、`tag_mode` | 标签 ID，最多 50 个；`all` 或 `any` |
| `min_rating`、`unrated` | 评分筛选 |
| `color`、`shape` | 主色（十六进制）、画幅（`landscape` / `square` / `portrait`） |
| `view` | `all` / `recent` / `favorites` / `unclassified`；不开放 `trash` |
| `sort` | `recent` / `oldest` / `name` / `rating` |
| `limit` | 1 到 20 |
| `cursor` | 上一页返回的 `next_cursor` |

每条结果返回：

- 身份：`asset_id`、`content_revision_id`
- 内容：`title`、`description`（截到 200 字）、`kind`
- 归类：`tags` 和 `groups`，都是名称，同时带 ID
- `rating`、`is_favorite`
- `width` / `height`（图片、视频有原图尺寸时）
- `unavailable`：素材不可展示时为 true，此时没有可读内容

整页另外返回 `total_count`、`next_cursor`、`has_more`。

不返回图片。要看图用 `read_asset`，一张一张读，受现有的图片张数和字节预算约束。

工具说明里写明三件事：

- 这是精确检索，按文字和归类匹配，不按画面相似度。
- 没有文字信息的素材可能搜不到，可以换条件，或按分组、标签浏览。
- 查不到就如实告诉摄影师，不要编造素材。

### 3.2 `read_library`（V6-035，只读）

- **展示名**：「读取资产库结构」
- **何时提供**：同 `search_assets`。
- **参数**：无。

返回：

- 分组树：`id`、`name`、`parent_id`、素材数。
- 分类与标签：每个分类下的标签，以及不属于任何分类的标签；都带 `id`、`name`、素材数。
- 总数超过 500 项时，截断并标出 `truncated`。

### 3.3 `list_conversation_assets`（V6-077，只读）

- **展示名**：「找回对话内容」
- **何时提供**：总是提供。
- **范围**：本对话中，本次运行触发消息之前的完成消息，加上本次运行自己产生的消息。

参数：

| 参数 | 说明 |
|---|---|
| `kind` | 可选，同上 |
| `source` | 可选。`photographer` 表示摄影师发的（input / attachment）；`assistant` 表示助手读过或产生的（result） |
| `query` | 可选，匹配所在消息的文字，不区分大小写 |
| `before_ordinal` | 分页，首次填 0 |
| `limit` | 1 到 20 |

每条返回：

- `content_revision_id`、`kind`
- `source`、`role`
- `tool`：result 来自哪个工具，例如 `read_asset`、`generate_node`
- `message_ordinal`
- `excerpt`：所在消息的文字摘录，截到 200 字
- `created_at`

同一修订多次出现时，只保留最新的一次。

`query` 的实现：在本对话范围内，对消息正文的 JSON 文本做 `ILIKE`。范围被对话限住，不需要新索引。工具结果消息的正文是 JSON，摘录的可读性有限；工具说明建议优先按 `kind` 和 `source` 筛选。

`read_history` 保留，用来读对话文字。两个工具的分工：

- `read_history`：「当时说了什么」
- `list_conversation_assets`：「当时出现过哪些内容」

### 3.4 `read_asset`（共用）

参数和返回不变，读取判定换成 §2.2。说明改为：

> 读取一项内容：画布节点、本次引用、本对话出现过的内容，或资产库素材。ID 取自 `read_canvas`、`list_conversation_assets`、`search_assets` 或 `<<<...>>>` 引用。

读成功后，仍按现状把它记为 `result` 引用。

### 3.5 `edit_canvas` 扩展

`add_node` 增加两个互斥的可选参数：

- `asset_id` + `content_revision_id`：放一个资产库素材。工具先读素材的当前修订，`content_revision_id` 对不上就拒绝，提示「素材已更新，重新查找」；对得上就组装 `creativelibrary.AssetReference`，走画布现有的 `asset` 分支。
- 只有 `content_revision_id`：放一个本对话出现过的内容（§2.2 第 1～3 条）。

新增动作 `fill_node`（`node_id` + `asset_id` + `content_revision_id`）：把素材填进同类型的空节点，对应画布现有的 `fill_from_asset`。

画布侧为第二种 `add_node` 增加 `Action.ContentRevisionID`：

- 节点内容指向这个已有修订，版本来源记 `conversation`。
- 校验沿用 `asset` 分支：`RequireUsable(display)`、类型一致、`fitMediaSize`。
- 与 `asset`、`content` 三者互斥。
- 这是画布的通用能力，助手工具负责在调用前做 §2.2 的读取判定；画布本身只校验可展示。

节点进入画布历史，可撤销，与现有 `add_node` 一致。

### 3.6 `save_to_library`（V6-035，写）

- **展示名**：「存入资产库」
- **何时提供**：总是提供。

参数：

| 参数 | 说明 |
|---|---|
| `node_id` | 必填 |
| `title` | 必填，最多 200 字 |
| `description` | 可选 |
| `group_ids`、`tag_ids` | 可选，只能用已有的 ID |

行为：

- 工具读节点当前的数据修订，组装 `SaveNodeInput`，通过 `applyOnce` 调 `creativecanvas.SaveNodeToLibrary`。
- 操作 ID 由工具调用派生，重放返回同一结果。
- 节点被改动时自动重读重试，与 `edit_canvas` 一样最多重试 `conflictRetries` 次。
- 同一修订已在库里时，`SaveRevisionInTx` 只追加归类，结果标明 `reused`。

返回 `asset_id`、`content_revision_id`、`reused`。

不能新建标签。传 `new_tags` 会被拒绝，并提示「新建标签属于整理资产库」（V6-078）。

### 3.7 生成结果记为会话引用

- `generate_node` 返回 `apply_state = applied` 时，把返回的 `content_revision_id` 作为本条工具消息的 `result` 引用。
- `compress_image` 返回 `applied` 时同样处理。

实现：把 `persistToolMessage` 里只认 `read_asset` 的特判改成按工具结果取 `content_revision_id`，覆盖这三个工具。只在成功采用时记录，被拒绝或未采用的结果不记。

保留影响：`creative_message_content_refs` 是内容保留根（`creativereference/projection.go:44`），被记录的生成结果会随对话一起保留。这些结果本来就是节点版本，额外占用有限；与 `read_asset` 读过的内容现状一致。

## 4. 前端

| 位置 | 改动 |
|---|---|
| `agentState.ts` 的 `messageSteps` | 改用 `account_library`、加上 `media_metadata`、清单只放本条引用；`egressNoticeVersion` 改为 `egress-2026-10-2` |
| `AgentPanel.tsx` | 删掉 `historyIDs`；「超过 200 个引用」的提示只看本条 |
| `home/startProject.ts` | 同 `messageSteps`（首页本就共用） |
| `EgressNotice.tsx` | 改写说明（见下） |
| `PrivacyPage.tsx` | 同步改写资产库相关的一句 |

说明正文改为：

> 这条消息会发送给平台签约的模型服务商，用来生成助手的回复。详见隐私说明。
>
> 发送的内容：
> - 你写的文字，以及选择的 Skill
> - 你附带或 `@` 引用的图片（压缩后的预览图）
> - 助手完成任务时读取的当前画布内容：节点标题、文字、生成提示词和参数
> - 助手按需查找的资产库内容：素材标题、描述、标签和分组，以及它查看的图片预览
> - 本次对话里之前出现过的内容，助手需要时会再次读取
>
> 说明有变化时会再告诉你。

工具块不需要前端改动：展示名来自后端目录。

## 5. 影响面

**必须修改**

- `creativeagent`：`consents.go`、`runs.go` 的 `coversRun`、`context.go`（`readableInTx`、`read_asset` 说明、`persistToolMessage`）、`runtime.go`（工具目录、`allowed`、`configure`）、`canvas_tools.go`（`add_node` / `fill_node`）。
- 新文件 `library_tools.go`：`search_assets`、`read_library`、`save_to_library`。新文件 `conversation_assets.go`：`list_conversation_assets`。
- `creativecanvas`：`Action.ContentRevisionID` 与 `add_node` 分支。
- `service.go` 的 `policyVersion`。
- 前端：`agentState.ts`、`AgentPanel.tsx`、`home/startProject.ts`、`EgressNotice.tsx`、`PrivacyPage.tsx`。
- 文档：`docs/dev/creative-agent.md`（授权与读取规则、工具清单）、`docs/product/creative-canvas-system/modules/harness.md` §2 的 `SearchAssets` 行（改为指向本设计）。

**需要验证**

- 说明版本升级的影响：派发检查（`admitDispatchInTx` → `coversRun`）会核对说明版本。上线时还在运行或等待补充的运行，下一次派发会返回需要授权，摄影师重新发送即可。这是升级说明版本的代价，验证时实测一次并记入验证记录。
- 资产库搜索在助手路径的性能：复用资产库页同一查询，每页 ≤ 20，沿用现有基准。
- 首页「一句话开始」与画布助手两个入口都走新授权。
- OpenAPI：`account_library` 已在契约中；授权允许带清单只是放宽校验，契约描述需要同步更新并 `make generate`。

**仍待调查**：无。

## 6. 验收映射与测试

测试先行：后端读取判定与工具都用 dbtest（真实 PostgreSQL），前端用现有单测，模型真实性用获准供应商留存调用记录。

| AC | 证据 |
|---|---|
| 035 AC-01 | dbtest：各类条件查到的素材与资产库页同一查询一致，结果带资产 ID 与内容修订 ID |
| 035 AC-02 | dbtest：无匹配返回空列表；`limit` 越界被拒；游标翻页不重不漏 |
| 035 AC-03 | dbtest：`selected_revisions` 授权下不提供、调用也被拒；回收站中的素材读不到；权利撤销的素材读不到；对话授权撤销后整条运行被拦 |
| 035 AC-04 | dbtest：素材放上画布、填空节点、修订过期被拒；节点存入资产库、重放不重复、已有修订只追加归类、`new_tags` 被拒 |
| 035 AC-05 | 真实模型：一次创作任务里查库、读图、放上画布并连为参考，保留工具调用记录 |
| 035 AC-06 | 代码检查：没有新增任何「被哪个画布或对话使用」的记录；资产库接口不变 |
| 077 AC-01 | dbtest：按类型、来源、文字筛选；同一修订去重；生成与压缩结果出现在列表里 |
| 077 AC-02 | dbtest：超过 20 条消息之前的附件，在后续运行仍可读；被撤销授权列过的修订读不到；删除的内容读不到 |
| 077 AC-03 | dbtest：另一对话、另一账号的引用不出现、读不到 |
| 077 AC-04 | dbtest：无匹配返回空；分页有界 |
| 077 AC-05 | 真实模型：多轮对话中找回较早发的参考图，放上画布并用于生成，保留工具调用记录 |

前端：单测覆盖 `messageSteps` 的新授权形状；浏览器验证说明弹窗新文案（升级版本后再弹一次）。

## 7. 取舍与风险

- **默认允许查库**：最符合按需取材，代价是所有账号会再看到一次说明。助手能看到整个资产库的文字信息，回收站除外。owner 已选择。
- **会话内容不再按条授权**：授权粒度从「每条消息」变为「每个对话」。这扩大了单次运行能读的范围，但只限本对话内此前合法出现过的内容。撤销仍然生效（§2.2）。这一变化涉及信任边界，实现后做一轮独立 change review，重点审 §2.2 的四个分支和撤销条件。
- **`query` 用 JSON 文本匹配**：简单，被对话范围限住。工具结果消息的摘录可读性一般；以后若需要，再给消息加纯文字列。
- **画布新增 `content_revision_id` 建节点**：通用能力，目前只有助手用。手工界面以后要做「把对话里的图拖进画布」时可以直接复用。

## 8. 待 owner 确认

无。两项取舍已在 2026-10-13 拍板。
